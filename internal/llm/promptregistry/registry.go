package promptregistry

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"text/template"
	"time"
)

type PromptRegistry struct {
	mu     sync.RWMutex
	specs  map[PromptID]map[PromptVersion]*PromptSpec
	blocks map[PromptID]*PromptSpec
	stats  registryStats
}

type registryStats struct {
	mu          sync.Mutex
	renderCount int64
}

var promptFuncMap = template.FuncMap{
	"defaultVal": func(val, def string) string {
		if val == "" {
			return def
		}
		return val
	},
	"truncate": func(s string, max int) string {
		if len(s) <= max {
			return s
		}
		return s[:max] + "..."
	},
	"div": func(a, b int) int {
		if b == 0 {
			return 0
		}
		return a / b
	},
	"sub": func(a, b int) int {
		return a - b
	},
}

func NewPromptRegistry() *PromptRegistry {
	return &PromptRegistry{
		specs:  make(map[PromptID]map[PromptVersion]*PromptSpec),
		blocks: make(map[PromptID]*PromptSpec),
	}
}

// Register 注册一个 PromptSpec。同 name+version 重复注册会覆盖（热更新）。
func (r *PromptRegistry) Register(spec PromptSpec) error {
	if spec.Name == "" {
		return fmt.Errorf("prompt name required")
	}
	if spec.Version == "" {
		spec.Version = VersionDefault
	}
	if spec.Template == "" && len(spec.Blocks) == 0 && spec.SystemPrompt == "" {
		return fmt.Errorf("template or blocks or system_prompt required")
	}

	if spec.Template != "" {
		if err := r.ValidateTemplate(spec.Template); err != nil {
			return fmt.Errorf("invalid template syntax: %w", err)
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.specs[spec.Name]; !ok {
		r.specs[spec.Name] = make(map[PromptVersion]*PromptSpec)
	}

	spec.UpdatedAt = time.Now()
	if spec.CreatedAt.IsZero() {
		spec.CreatedAt = spec.UpdatedAt
	}
	if spec.Role == "" {
		spec.Role = RoleUser
	}

	r.specs[spec.Name][spec.Version] = &spec
	return nil
}

// RegisterBlock 注册固定块。
func (r *PromptRegistry) RegisterBlock(spec PromptSpec) error {
	if spec.Name == "" {
		return fmt.Errorf("block name required")
	}
	if spec.Template == "" {
		return fmt.Errorf("block template required")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.blocks[spec.Name] = &spec
	return nil
}

// Get 获取 prompt spec。
func (r *PromptRegistry) Get(name PromptID, version PromptVersion) (*PromptSpec, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	versions, ok := r.specs[name]
	if !ok {
		return nil, fmt.Errorf("prompt %s not found", name)
	}

	if version == "" || version == VersionLatest {
		version = VersionDefault
	}

	spec, ok := versions[version]
	if !ok {
		spec, ok = versions[VersionDefault]
		if !ok {
			return nil, fmt.Errorf("prompt %s v=%s and default not found", name, version)
		}
	}

	return spec, nil
}

func (r *PromptRegistry) BuildDecision(ctx context.Context, name PromptID, version PromptVersion, data *DecisionPromptContext) (string, error) {
	if err := data.Validate(); err != nil {
		return "", err
	}
	return r.build(ctx, name, version, data)
}

func (r *PromptRegistry) BuildVerify(ctx context.Context, name PromptID, version PromptVersion, data *VerifyPromptContext) (string, error) {
	if err := data.Validate(); err != nil {
		return "", err
	}
	return r.build(ctx, name, version, data)
}

func (r *PromptRegistry) BuildSkill(ctx context.Context, name PromptID, version PromptVersion, data *SkillPromptContext) (string, error) {
	if err := data.Validate(); err != nil {
		return "", err
	}
	return r.build(ctx, name, version, data)
}

func (r *PromptRegistry) BuildReport(ctx context.Context, name PromptID, version PromptVersion, data *ReportPromptContext) (string, error) {
	if err := data.Validate(); err != nil {
		return "", err
	}
	return r.build(ctx, name, version, data)
}

func (r *PromptRegistry) BuildSafety(ctx context.Context, name PromptID, version PromptVersion, data *SafetyPromptContext) (string, error) {
	if err := data.Validate(); err != nil {
		return "", err
	}
	return r.build(ctx, name, version, data)
}

func (r *PromptRegistry) BuildSystemPrompt(ctx context.Context, name PromptID, version PromptVersion) (string, error) {
	return r.build(ctx, name, version, struct{}{})
}

// build 构建并渲染 prompt。
func (r *PromptRegistry) build(ctx context.Context, name PromptID, version PromptVersion, data any) (string, error) {
	_ = ctx

	spec, err := r.Get(name, version)
	if err != nil {
		return "", err
	}

	parts := make([]string, 0, len(spec.Blocks)+2)

	for _, blockName := range spec.Blocks {
		r.mu.RLock()
		block, ok := r.blocks[blockName]
		r.mu.RUnlock()
		if !ok {
			return "", fmt.Errorf("block %s not found for prompt %s", blockName, name)
		}
		parts = append(parts, block.Template)
	}

	if spec.SystemPrompt != "" {
		parts = append(parts, spec.SystemPrompt)
	}

	if spec.Template != "" {
		rendered, renderErr := r.Render(spec.Template, data)
		if renderErr != nil {
			return "", fmt.Errorf("render %s/%s: %w", name, version, renderErr)
		}
		parts = append(parts, rendered)
	}

	final := strings.Join(parts, "\n\n")

	r.stats.mu.Lock()
	r.stats.renderCount++
	r.stats.mu.Unlock()

	return final, nil
}

// Render 渲染模板字符串。
func (r *PromptRegistry) Render(tmpl string, data any) (string, error) {
	t, err := template.New("prompt").Funcs(promptFuncMap).
		Option("missingkey=zero").Parse(tmpl)
	if err != nil {
		return "", fmt.Errorf("parse: %w", err)
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("execute: %w", err)
	}

	return buf.String(), nil
}

// ValidateTemplate 验证模板语法。
func (r *PromptRegistry) ValidateTemplate(tmpl string) error {
	_, err := template.New("validate").Funcs(promptFuncMap).Parse(tmpl)
	return err
}

// Hash 计算 prompt 内容的 SHA256。
func (r *PromptRegistry) Hash(content string) string {
	h := sha256.Sum256([]byte(content))
	return hex.EncodeToString(h[:])[:16]
}

// Stats 返回运行统计。
func (r *PromptRegistry) Stats() (renderCount int64, activeSpecs int, totalBlocks int) {
	r.mu.RLock()
	specCount := 0
	for _, versions := range r.specs {
		specCount += len(versions)
	}
	blockCount := len(r.blocks)
	r.mu.RUnlock()

	r.stats.mu.Lock()
	renderCount = r.stats.renderCount
	r.stats.mu.Unlock()

	return renderCount, specCount, blockCount
}

// Has 检查指定 prompt 是否存在。
func (r *PromptRegistry) Has(name PromptID, version PromptVersion) bool {
	_, err := r.Get(name, version)
	return err == nil
}
