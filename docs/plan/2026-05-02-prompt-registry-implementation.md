# Prompt Registry 完整实施方案（v2 - 含评审修复）

> **状态**：待实施
> **创建日期**：2026-05-02
> **版本**：v2（修复评审发现的 12 个问题）
> **评审依据**：基于 `.sisyphus/plans/prompt-registry-refactor.md` 的代码审查结果
> **目标**：将 `internal/llm/prompts.go` 中的硬编码 prompt 模板重构为 Prompt Registry 架构

---

## TL;DR

本方案在 v1 基础上，修复了评审发现的所有 12 个问题：

| 问题 | 修复措施 |
|------|---------|
| P0: 缺失 reactSystem/safety/verify/skill 模板 | 补全所有模板完整内容 |
| P0: decision 模板关键指导语缺失 | 逐段对比 prompts.go，确保 100% 覆盖 |
| P1: Safety/ReAct Registry 集成路径 | 扩展 RenderContext，新增 Command/Reason 字段 |
| P1: NewAgent 签名变更 | 改用 `promptDir string` 参数 |
| P1: GetRecentSteps(2) vs (3) | 统一为 3 |
| P2: 验证阶段 AbnormalPods 格式 | 新增 AbnormalPodsVerify 字段 |
| P2: verify decision 值差异 | 文档明确 continue/report |
| P2: Executable 字段缺失 | 文档说明（现有问题，非本次引入） |

---

## 0. Prompt 分层设计原则

所有 prompt 模板必须遵循以下 10 层分层原则。固定内容通过 blocks 引用放置在 Cache Boundary 之上，动态内容放置在 Cache Boundary 之下。

```
┌─────────────────────────────────────────────────────────────┐
│  [1] System Prompt（固定）                                   │
│      角色定义、工作模式、基础约束                               │
│                                                             │
│  [2] Tool Definitions（固定）                                │
│      可用工具列表、参数说明、典型命令                            │
│                                                             │
│  [3] Role Constraints（固定）                                │
│      决策规则、工具选择原则、自检规则、严格约束                    │
│                                                             │
│  [4] Few-shot Examples（固定）                               │
│      诊断思路参考表、问题类型→工具组合映射                        │
├─────────────────────────────────────────────────────────────┤  ← Cache Boundary
│                                                             │     ↑↑↑↑↑↑↑↑↑
│  [5] Long-term Memory Summary（低频变化）                    │     固定块在此之上
│      历史推理摘要（CompressedSummary）                        │     内容稳定不变
│                                                             │     SHA256 可缓存
│  [6] Conversation Summary（低频变化）                        │
│      用户查询、集群状态、异常资源、节点列表                       │
├─────────────────────────────────────────────────────────────┤  ← 会话边界
│                                                             │     ↑↑↑↑↑↑↑↑↑
│  [7] Latest User Input（高频变化）                           │     动态内容在此之下
│      当前轮用户输入（通常包含在 [6] 中）                        │     每轮变化
│                                                             │
│  [8] Tool Results（高频变化）                                │
│      已执行工具摘要、验证查询结果                               │
│                                                             │
│  [9] Scratchpad（高频变化）                                  │
│      最近推理步骤（RecentSteps）、思考过程                      │
│                                                             │
│  [10] Planner Thoughts（高频变化）                           │
│      迭代进度、进度警告、输出格式要求、决策规则                    │
└─────────────────────────────────────────────────────────────┘
```

### 各层在 Registry 中的映射

| 层次 | 内容 | Registry 实现 | 变化频率 |
|------|------|--------------|---------|
| [1] System Prompt | 角色、工作方式、注意事项 | `blocks/system_base.md` | 固定 |
| [2] Tool Definitions | K8s 工具 + 主机工具 | `blocks/tools_definition.md` | 固定 |
| [3] Role Constraints | 决策/验证约束、工具选择原则 | `blocks/constraints_decision.md` / `constraints_verify.md` | 固定 |
| [4] Few-shot Examples | 诊断思路参考表 | `blocks/reference_patterns.md` | 固定 |
| [5] Long-term Memory | CompressedSummary | `{{.CompressedSummary}}` | 低频（压缩后更新） |
| [6] Conversation Summary | UserQuery, ResourceSummary, AbnormalPods | `{{.UserQuery}}` 等 | 低频（会话内稳定） |
| [7] Latest User Input | 当前轮输入 | 包含在 [6] 中 | 高频 |
| [8] Tool Results | ToolSummary, VerifyExecutions | `{{.ToolSummary}}` 等 | 高频（每轮变化） |
| [9] Scratchpad | RecentSteps | `{{.RecentSteps}}` | 高频（每轮追加） |
| [10] Planner Thoughts | 进度、输出格式 | 模板中的固定文本 | 高频（迭代计数） |

### Cache Boundary 的作用

**Cache Boundary 之上的内容**（[1]-[4]）：
- 通过 `registry.yaml` 的 `blocks` 字段引用
- 在 `Build()` 函数中直接拼接，**不经过 `text/template` 渲染**
- SHA256 哈希稳定不变
- Provider 可据此识别可缓存前缀

**Cache Boundary 之下的内容**（[5]-[10]）：
- 通过 `{{.Field}}` 占位符在主模板中引用
- 每轮由 `Render()` 函数动态渲染
- 内容随诊断进展变化

### 示例：decision prompt 的分层

```
┌─────────────────────────────────────────────────────────────┐
│ blocks/system_base.md                                       │
│   "你是 Kubernetes 集群诊断专家..."                            │
│                                                             │
│ blocks/tools_definition.md                                  │
│   "## 可用工具\n### K8s 资源查询\n- list_pods: ..."           │
│                                                             │
│ blocks/constraints_decision.md                              │
│   "## 决策规则\n- execute_plan: ..."                         │
│                                                             │
│ blocks/reference_patterns.md                                │
│   "## 诊断思路参考\n| 问题类型 | 推荐工具 | ..."               │
├─────────────────────────────────────────────────────────────┤  ← Cache Boundary
│                                                             │
│ decision/default.md                                         │
│   "## 用户查询\n{{.UserQuery}}"                              │
│   "## 集群状态\n{{.ResourceSummary}}"                        │
│   "### 异常资源\n{{.AbnormalPods}}"                          │
│   "{{if .CompressedSummary}}## 历史推理摘要..."               │
│   "{{.ToolSummary}}"                                        │
│   "## 已执行的步骤\n{{.RecentSteps}}"                         │
│   "## 进度\n第 {{.Iteration}}/{{.MaxIterations}} 轮..."      │
│   "## 输出格式\n{ ... }"                                     │
└─────────────────────────────────────────────────────────────┘
```

---

## 1. 现状分析

### 1.1 当前问题

`internal/llm/prompts.go`（731 行）将所有 prompt 模板以 `const` 字符串方式硬编码：

| 问题 | 影响 |
|------|------|
| 模板与代码强耦合 | 改 prompt 必须改 Go 代码、编译、发版 |
| 跨阶段重复定义 | decision/verify/skill/report 各自维护相似内容 |
| 无版本概念 | 无法回答"这次用的哪个 prompt" |
| 固定块与动态内容混排 | 无法利用 prompt caching |

### 1.2 目标

1. **外部化**：prompt 模板移到文件，代码只负责渲染
2. **版本化**：每个 prompt 有 name + version，可独立管理
3. **接口化**：通过 Registry 抽象，Provider 可替换
4. **可观测**：trace 记录 prompt 身份，可归因影响
5. **缓存边界**：固定块前置，动态内容后置

---

## 2. 总体架构

### 2.1 三层分离

```
模板层（.md 文件）→ Registry 层（Go 结构体）→ 渲染层（text/template）→ LLM Provider
```

### 2.2 目录结构

```
internal/llm/
├── promptregistry/               ← 新增
│   ├── registry.go              ← 核心：PromptRegistry + Build() + Render()
│   ├── types.go                 ← 类型：PromptSpec, RenderContext
│   ├── provider.go              ← Provider：运行时文件加载（os.ReadFile）
│   ├── helpers.go               ← 通用工具函数（formatList, truncate）
│   └── registry_test.go        ← 测试
├── prompts.go                   ← 保留：Build* 函数变为适配层
└── ...

prompts/                         ← 新增：项目根目录下（不嵌入二进制）
├── registry.yaml
├── blocks/
│   ├── system_base.md
│   ├── tools_definition.md
│   ├── constraints_decision.md
│   ├── constraints_verify.md
│   └── reference_patterns.md
├── decision/default.md
├── verify/default.md
├── skill/default.md
├── report/default.md
└── safety/default.md
```

---

## 3. 核心类型实现

### 3.1 `internal/llm/promptregistry/types.go`

```go
package promptregistry

import "time"

type PromptID string
type PromptVersion string

const (
    VersionDefault PromptVersion = "default"
    VersionLatest  PromptVersion = "latest"
)

type PromptRole string
const (
    RoleSystem PromptRole = "system"
    RoleUser   PromptRole = "user"
)

// PromptSpec 描述一个完整的 prompt 资产
type PromptSpec struct {
    Name         PromptID        `json:"name" yaml:"name"`
    Version      PromptVersion   `json:"version" yaml:"version"`
    Label        string          `json:"label" yaml:"label"`
    Template     string          `json:"template" yaml:"template"`
    SystemPrompt string          `json:"system_prompt" yaml:"system_prompt"`
    Blocks       []PromptID      `json:"blocks" yaml:"blocks"`
    Role         PromptRole      `json:"role" yaml:"role"`
    ModelHint    string          `json:"model_hint" yaml:"model_hint"`
    Metadata     PromptMetadata  `json:"metadata" yaml:"metadata"`
    IsActive     bool            `json:"is_active" yaml:"is_active"`
    CreatedAt    time.Time       `json:"created_at" yaml:"created_at"`
    UpdatedAt    time.Time       `json:"updated_at" yaml:"updated_at"`
}

type PromptMetadata struct {
    Description string   `json:"description" yaml:"description"`
    Tags        []string `json:"tags" yaml:"tags"`
    Environment string   `json:"environment" yaml:"environment"`
}

// RenderContext prompt 渲染时的变量上下文
type RenderContext struct {
    // State 数据（高频变化）
    UserQuery                string
    Iteration                int
    MaxIterations            int
    RecentSteps              string
    ToolSummary              string

    // K8s 上下文（中频变化）
    ResourceSummary          string
    AbnormalPods             string
    AbnormalPodsVerify       string  // ← 新增：验证阶段的异常 Pod 格式（不同格式）
    NodeList                 string

    // 记忆（低频变化）
    CompressedSummary        string

    // 阶段特有数据
    InitialRootCause         string
    RecommendationsChecklist string
    VerifyExecutions         string
    VerifyIter               int
    MaxVerifyIter            int
    ActiveSkillName          string
    ActiveSkillContent       string
    SkillList                string

    // 报告阶段
    Status                   string
    Findings                 string
    CommandSummary           string
    BlockedCommands           string
    ReasoningChain           string
    IsVerifyPhase            bool

    // Safety Audit 专用（从 Extra 移到顶层）  ← 新增
    Command                  string
    Reason                   string

    // 自定义扩展
    Extra                    map[string]interface{}
}
```

### 3.2 `internal/llm/promptregistry/registry.go`

```go
package promptregistry

import (
    "context"
    "crypto/sha256"
    "encoding/hex"
    "fmt"
    "strings"
    "sync"
    "time"
)

type PromptRegistry struct {
    mu       sync.RWMutex
    specs    map[PromptID]map[PromptVersion]*PromptSpec
    blocks   map[PromptID]*PromptSpec
    stats    registryStats
}

type registryStats struct {
    mu          sync.Mutex
    renderCount int64
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
    if spec.Template == "" && len(spec.Blocks) == 0 {
        return fmt.Errorf("template or blocks required")
    }

    // 验证模板语法
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
    r.specs[spec.Name][spec.Version] = &spec
    return nil
}

// RegisterBlock 注册固定块
func (r *PromptRegistry) RegisterBlock(spec PromptSpec) error {
    if spec.Template == "" {
        return fmt.Errorf("block template required")
    }
    r.mu.Lock()
    defer r.mu.Unlock()
    r.blocks[spec.Name] = &spec
    return nil
}

// Get 获取 prompt spec
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

// Build 构建并渲染 prompt
func (r *PromptRegistry) Build(ctx context.Context, name PromptID, version PromptVersion, data *RenderContext) (string, error) {
    if data == nil {
        data = &RenderContext{}
    }
    spec, err := r.Get(name, version)
    if err != nil {
        return "", err
    }
    var parts []string
    // Blocks（Cache Boundary 之上）
    for _, bn := range spec.Blocks {
        if b, ok := r.blocks[bn]; ok {
            parts = append(parts, b.Template)
        }
    }
    // System prompt
    if spec.SystemPrompt != "" {
        parts = append(parts, spec.SystemPrompt)
    }
    // 主模板渲染
    if spec.Template != "" {
        rendered, err := r.Render(spec.Template, data)
        if err != nil {
            return "", fmt.Errorf("render %s/%s: %w", name, version, err)
        }
        parts = append(parts, rendered)
    }
    final := strings.Join(parts, "\n\n")
    r.stats.mu.Lock()
    r.stats.renderCount++
    r.stats.mu.Unlock()
    return final, nil
}

// Hash 计算 prompt 内容的 SHA256
func (r *PromptRegistry) Hash(content string) string {
    h := sha256.Sum256([]byte(content))
    return hex.EncodeToString(h[:])[:16]
}

// Stats 返回运行统计
func (r *PromptRegistry) Stats() (renderCount int64, activeSpecs int, totalBlocks int) {
    r.mu.RLock()
    c := 0
    for _, vs := range r.specs { c += len(vs) }
    bc := len(r.blocks)
    r.mu.RUnlock()
    r.stats.mu.Lock()
    rc := r.stats.renderCount
    r.stats.mu.Unlock()
    return rc, c, bc
}

// Has 检查指定 prompt 是否存在
func (r *PromptRegistry) Has(name PromptID, version PromptVersion) bool {
    _, err := r.Get(name, version)
    return err == nil
}
```

### 3.3 渲染逻辑（内联到 Registry）

不再使用独立的 `renderer.go` / `templateRenderer` 结构体。渲染所需的 `funcMap` 和 `Render`/`Validate` 方法直接放在 `registry.go` 的 `PromptRegistry` 上：

```go
// 在 registry.go 中补充

import (
    "bytes"
    "text/template"
)

// funcMap 供模板使用的自定义函数
var promptFuncMap = template.FuncMap{
    "defaultVal": func(val, def string) string {
        if val == "" { return def }
        return val
    },
    "truncate": func(s string, max int) string {
        if len(s) <= max { return s }
        return s[:max] + "..."
    },
    "div": func(a, b int) int { if b == 0 { return 0 }; return a / b },
    "sub": func(a, b int) int { return a - b },
}

// Render 渲染模板字符串
func (r *PromptRegistry) Render(tmpl string, data *RenderContext) (string, error) {
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

// ValidateTemplate 验证模板语法
func (r *PromptRegistry) ValidateTemplate(tmpl string) error {
    _, err := template.New("validate").Funcs(promptFuncMap).Parse(tmpl)
    return err
}
```

> **变更**：删除 `renderer.go` 文件和 `templateRenderer` 结构体。`ge`/`gt`/`le`/`lt` 等比较函数是 Go `text/template` 内置的，无需重复定义。`mul`/`mod` 当前模板未使用，不添加。

### 3.4 `internal/llm/promptregistry/provider.go`

```go
package promptregistry

import (
    "fmt"
    "os"
    "path/filepath"

    "gopkg.in/yaml.v3"
)

type PromptManifest struct {
    Version string      `yaml:"version"`
    Blocks  []BlockDef  `yaml:"blocks"`
    Prompts []PromptDef `yaml:"prompts"`
}

type BlockDef struct {
    Name        string `yaml:"name"`
    File        string `yaml:"file"`
    Description string `yaml:"description"`
}

type PromptDef struct {
    Name         string   `yaml:"name"`
    Version      string   `yaml:"version"`
    Label        string   `yaml:"label"`
    File         string   `yaml:"file"`
    SystemPrompt string   `yaml:"system_prompt"`
    Blocks       []string `yaml:"blocks"`
    ModelHint    string   `yaml:"model_hint"`
    Environment  string   `yaml:"environment"`
    Description  string   `yaml:"description"`
    Tags         []string `yaml:"tags"`
    IsActive     bool     `yaml:"is_active"`
}

// Load 从指定目录加载所有 prompt 和 block
func (r *PromptRegistry) Load(promptsDir string) error {
    // 解析相对路径
    if !filepath.IsAbs(promptsDir) {
        cwd, err := os.Getwd()
        if err != nil {
            return fmt.Errorf("get working directory: %w", err)
        }
        promptsDir = filepath.Join(cwd, promptsDir)
    }

    // 验证目录存在
    if _, err := os.Stat(promptsDir); os.IsNotExist(err) {
        return fmt.Errorf("prompts directory not found: %s", promptsDir)
    }

    // 加载 registry.yaml
    manifestPath := filepath.Join(promptsDir, "registry.yaml")
    manifestData, err := os.ReadFile(manifestPath)
    if err != nil {
        return fmt.Errorf("read manifest %s: %w", manifestPath, err)
    }
    var m PromptManifest
    if err := yaml.Unmarshal(manifestData, &m); err != nil {
        return fmt.Errorf("parse manifest: %w", err)
    }

    // 加载 blocks
    for _, b := range m.Blocks {
        blockPath := filepath.Join(promptsDir, b.File)
        c, err := os.ReadFile(blockPath)
        if err != nil {
            return fmt.Errorf("read block %s (%s): %w", b.Name, blockPath, err)
        }
        r.RegisterBlock(PromptSpec{
            Name:     PromptID(b.Name),
            Template: string(c),
            Metadata: PromptMetadata{Description: b.Description},
        })
    }

    // 加载 prompts
    for _, p := range m.Prompts {
        promptPath := filepath.Join(promptsDir, p.File)
        t, err := os.ReadFile(promptPath)
        if err != nil {
            return fmt.Errorf("read prompt %s (%s): %w", p.Name, promptPath, err)
        }
        var refs []PromptID
        for _, b := range p.Blocks {
            refs = append(refs, PromptID(b))
        }
        r.Register(PromptSpec{
            Name:         PromptID(p.Name),
            Version:      PromptVersion(p.Version),
            Label:        p.Label,
            Template:     string(t),
            SystemPrompt: p.SystemPrompt,
            Blocks:       refs,
            Role:         RoleUser,
            ModelHint:    p.ModelHint,
            IsActive:     p.IsActive,
            Metadata: PromptMetadata{
                Description: p.Description,
                Environment: p.Environment,
                Tags:        p.Tags,
            },
        })
    }
    return nil
}
```

---

## 4. Config 结构体扩展

### 4.1 新增 `PromptConfig` 类型

在 `internal/config/config.go` 中添加：

```go
// PromptConfig Prompt 模板配置
type PromptConfig struct {
    TemplateDir string `yaml:"template_dir"` // 模板目录，默认 "./prompts"
}
```

### 4.2 在 `Config` 结构体中添加字段

```go
type Config struct {
    Gateway  GatewayConfig  `yaml:"gateway"`
    ShellMCP ShellMCPConfig `yaml:"shell_mcp"`
    LLM      AgentLLMConfig `yaml:"llm"`
    Store    StoreConfig    `yaml:"store"`
    Agent    AgentConfig    `yaml:"agent"`
    Monitor  MonitorConfig  `yaml:"monitor"`
    Log      LogConfig      `yaml:"log"`
    Skill    SkillConfig    `yaml:"skill"`
    Prompt   PromptConfig   `yaml:"prompt"` // ← 新增
}
```

### 4.3 在 `setDefaults()` 中设置默认值

```go
func (c *Config) setDefaults() {
    // ...现有默认值设置...
    
    // Prompt 默认值
    if c.Prompt.TemplateDir == "" {
        c.Prompt.TemplateDir = "./prompts"
    }
}
```

### 4.4 在 `validate()` 中添加验证

```go
func (c *Config) validate() error {
    // ...现有验证...
    
    // Prompt 验证（可选：检查目录是否存在）
    // 注意：不在此处验证，而是在 Registry.Load() 时验证
    // 这样可以支持启动时目录不存在但后续创建的场景
    
    return nil
}
```

### 4.5 配置文件示例

```yaml
# configs/config.yaml
prompt:
  template_dir: "./prompts"  # 相对于工作目录

# 其他配置...
gateway:
  base_url: "https://localhost:8080"
  # ...
```

---

## 5. 辅助函数实现

### 5.1 精简设计

不再为每种格式化单独导出函数。大部分格式化逻辑是一次性的，直接内联到 `buildRenderContext` 中（见 §6.2 和 §6.3）。`helpers.go` 只保留真正被多处复用的通用函数：

```go
package promptregistry

import (
    "fmt"
    "strings"
)

// formatList 通用列表格式化：将元素数组格式化为 "- xxx\n- yyy" 形式
func formatList[T any](items []T, empty string, fn func(T) string) string {
    if len(items) == 0 {
        return empty
    }
    var lines []string
    for _, item := range items {
        lines = append(lines, fn(item))
    }
    return strings.Join(lines, "\n")
}

// truncate 截断字符串
func truncate(s string, max int) string {
    if len(s) <= max {
        return s
    }
    return s[:max] + "..."
}

// formatExecStatus 将执行成功/失败转为中文
func formatExecStatus(success bool) string {
    if success {
        return "成功"
    }
    return "失败"
}
```

> **变更**：原 helpers.go 中 12 个导出函数精简为 3 个通用工具函数。具体的格式化逻辑内联到各节点的 `buildRenderContext` 方法中（见 §6.2 / §6.3）。

---

## 6. 依赖注入实现

### 6.1 修改 `agent.go`

```go
package diagnosis

import (
    // ...现有 import...
    "github.com/AceDarkknight/k8s-analyzer-agent/internal/llm/promptregistry"
)

func NewAgent(
    gw *gateway.GatewayClient,
    sa *safety.SafetyAgent,
    router *llm.LLMRouter,
    reactLLM *llm.ReActLLM,
    findingStore store.FindingStore,
    toolCache store.ToolCacheStore,
    skillLoader *skillpkg.Loader,
    traceStore store.TraceStore,
    cfg *config.AgentConfig,
    promptDir string,  // ← 新增参数（而非改 cfg 类型）
) *Agent {
    // ...现有逻辑...
    
    // 初始化 Prompt Registry
    reg := promptregistry.NewPromptRegistry()
    if promptDir != "" {
        if err := reg.Load(promptDir); err != nil {
            logger.Warn("failed to load prompt templates, will use fallback",
                logger.String("dir", promptDir),
                logger.Err(err))
            // 注意：不在此处返回错误，而是降级到旧 Build*() 函数
        } else {
            rc, specs, blocks := reg.Stats()
            logger.Info("prompt registry initialized",
                logger.Int64("render_count", rc),
                logger.Int("specs", specs),
                logger.Int("blocks", blocks))
        }
    }
    
    // 创建各节点，传入 Registry
    decisionNode := NewDecisionNode(router, skillLoader, recorder, reg)
    reportNode := NewReportNode(router, findingStore, recorder, reg)
    
    // ...后续逻辑...
}
```

**调用方修改**（`cmd/k8s-analyzer/main.go`）：
```go
// 修改前
agent := diagnosis.NewAgent(gw, sa, router, reactLLM, findingStore, toolCache, skillLoader, traceStore, &cfg.Agent)

// 修改后
agent := diagnosis.NewAgent(gw, sa, router, reactLLM, findingStore, toolCache, skillLoader, traceStore, &cfg.Agent, cfg.Prompt.TemplateDir)
```

### 6.2 修改 `DecisionNode`

```go
type DecisionNode struct {
    router      *llm.LLMRouter
    skillLoader *skillpkg.Loader
    recorder    *trc.TaskRecorder
    promptReg   *promptregistry.PromptRegistry  // ← 新增
}

func NewDecisionNode(
    router *llm.LLMRouter,
    skillLoader *skillpkg.Loader,
    recorder *trc.TaskRecorder,
    promptReg *promptregistry.PromptRegistry,  // ← 新增参数
) *DecisionNode {
    return &DecisionNode{
        router:      router,
        skillLoader: skillLoader,
        recorder:    recorder,
        promptReg:   promptReg,
    }
}

func (n *DecisionNode) Execute(ctx context.Context, s *state.State) (*DecisionOutput, error) {
    // ...现有逻辑（迭代计数、cache 检查等）...
    
    // 构建 prompt（带 fallback）
    prompt := n.buildPrompt(ctx, s)
    if prompt == "" {
        logger.Warn("DecisionNode: empty prompt generated")
        return n.fallbackDecision(s), nil
    }
    
    // ...后续逻辑不变...
}

func (n *DecisionNode) buildPrompt(ctx context.Context, s *state.State) string {
    // 优先使用 Registry
    if n.promptReg != nil {
        promptName := n.resolvePromptName(s)
        renderCtx := n.buildRenderContext(s)
        prompt, err := n.promptReg.Build(ctx, promptName, promptregistry.VersionDefault, renderCtx)
        if err == nil {
            return prompt
        }
        logger.Warn("prompt registry build failed, falling back to legacy",
            logger.Err(err))
    }
    
    // 降级到旧 Build*() 函数
    return n.buildPromptLegacy(s)
}

func (n *DecisionNode) buildPromptLegacy(s *state.State) string {
    if s.VerifyPhase {
        return llm.BuildVerifyDecisionPrompt(s)
    }
    if s.HasActiveSkill() {
        return llm.BuildSkillExecutionPrompt(s)
    }
    skillSummary := ""
    if n.skillLoader != nil {
        skillSummary = n.skillLoader.BuildSkillSummary()
    }
    return llm.BuildDecisionPrompt(s, skillSummary)
}

func (n *DecisionNode) resolvePromptName(s *state.State) promptregistry.PromptID {
    if s.VerifyPhase {
        return "verify"
    }
    if s.HasActiveSkill() {
        return "skill"
    }
    return "decision"
}

func (n *DecisionNode) buildRenderContext(s *state.State) *promptregistry.RenderContext {
    ctx := &promptregistry.RenderContext{
        UserQuery:     s.UserInput,
        Iteration:     s.GetIterationCount(),
        MaxIterations: s.GetMaxIterations(),
    }
    if s.K8sInfo != nil {
        ctx.ResourceSummary = s.K8sInfo.GetSummary()
        // 主诊断格式："- ns/name (状态: X, 重启: N)"
        pods := s.K8sInfo.GetAbnormalPods()
        var podStrs []string
        for _, p := range pods {
            podStrs = append(podStrs, fmt.Sprintf("- %s/%s (状态: %s, 重启: %d)", p.Namespace, p.Name, p.Status, p.Restarts))
        }
        ctx.AbnormalPods = strings.Join(podStrs, "\n")
        if len(podStrs) == 0 { ctx.AbnormalPods = "无" }
        // 验证阶段格式："- 命名空间: X, Pod名: Y, 状态: Z"
        var verifyStrs []string
        for _, p := range pods {
            verifyStrs = append(verifyStrs, fmt.Sprintf("- 命名空间: %s, Pod名: %s, 状态: %s", p.Namespace, p.Name, p.Status))
        }
        ctx.AbnormalPodsVerify = strings.Join(verifyStrs, "\n")
        if len(verifyStrs) == 0 { ctx.AbnormalPodsVerify = "无" }
    }
    ctx.CompressedSummary = s.CompressedSummary
    // 构建最近推理步骤（与现有代码一致：GetRecentSteps(3), observation 截断 800）
    steps := s.GetRecentSteps(3)
    if len(steps) == 0 {
        ctx.RecentSteps = "无"
    } else {
        var stepStrs []string
        for i, step := range steps {
            obs := step.Observation
            if len(obs) > 800 { obs = obs[:800] + "..." }
            stepStrs = append(stepStrs, fmt.Sprintf("步骤 %d:\n  思考: %s\n  决策: %s\n  观察: %s", i+1, step.Thought, step.Decision, obs))
        }
        ctx.RecentSteps = strings.Join(stepStrs, "\n")
    }
    // 工具摘要表
    execs := s.GetCommandExecutions()
    if len(execs) > 0 {
        var lines []string
        lines = append(lines, "## 已执行工具摘要", "| # | 命令 | 结果 |", "|---|------|------|")
        for i, e := range execs {
            status := "✓"; if !e.Success { status = "✗" }
            cmd := e.Command; if len(cmd) > 60 { cmd = cmd[:60] + "..." }
            lines = append(lines, fmt.Sprintf("| %d | %s | %s |", i+1, cmd, status))
        }
        ctx.ToolSummary = strings.Join(lines, "\n")
    }
    if n.skillLoader != nil && !s.HasActiveSkill() {
        ctx.SkillList = n.skillLoader.BuildSkillSummary()
    }
    if s.VerifyPhase && s.AnalysisResult != nil {
        ctx.InitialRootCause = s.AnalysisResult.RootCause
        ctx.VerifyIter = s.VerifyIterationCount
        ctx.MaxVerifyIter = s.MaxVerifyIterations
        // 构建待验证清单
        var items []string
        for _, rec := range s.AnalysisResult.Recommendations {
            if rec.Command == "" { continue }
            st := "尚未验证"; if rec.Verified { st = "已验证" }
            items = append(items, fmt.Sprintf("%d. [%s] %s", len(items)+1, st, rec.Action))
        }
        if len(items) == 0 { ctx.RecommendationsChecklist = "无" } else { ctx.RecommendationsChecklist = strings.Join(items, "\n") }
        // 构建已执行验证查询
        vExecs := s.GetVerifyPhaseExecutions()
        if len(vExecs) == 0 { ctx.VerifyExecutions = "无" } else {
            var es []string
            for _, e := range vExecs {
                st := "成功"; if !e.Success { st = "失败" }
                out := e.Output; if len(out) > 300 { out = out[:300] + "..." }
                es = append(es, fmt.Sprintf("- %s (%s)\n  输出: %s", e.Command, st, out))
            }
            ctx.VerifyExecutions = strings.Join(es, "\n")
        }
        // 节点列表
        if s.K8sInfo != nil {
            nodes := s.K8sInfo.GetNodes()
            var ns []string
            for _, n := range nodes { ns = append(ns, fmt.Sprintf("- 节点名: %s, 状态: %s", n.Name, n.Status)) }
            if len(ns) == 0 { ctx.NodeList = "无" } else { ctx.NodeList = strings.Join(ns, "\n") }
        }
    }
    if s.HasActiveSkill() {
        ctx.ActiveSkillName = s.ActiveSkillName
        ctx.ActiveSkillContent = s.ActiveSkillContent
    }
    return ctx
}
```

### 6.3 修改 `ReportNode`

```go
type ReportNode struct {
    router    *llm.LLMRouter
    store     store.FindingStore
    recorder  *trc.TaskRecorder
    promptReg *promptregistry.PromptRegistry  // ← 新增
}

func NewReportNode(
    router *llm.LLMRouter,
    store store.FindingStore,
    recorder *trc.TaskRecorder,
    promptReg *promptregistry.PromptRegistry,  // ← 新增参数
) *ReportNode {
    return &ReportNode{
        router:    router,
        store:     store,
        recorder:  recorder,
        promptReg: promptReg,
    }
}

func (n *ReportNode) Execute(ctx context.Context, s *state.State) (*state.State, error) {
    // ...现有验证结果匹配逻辑...
    
    // 构建 prompt
    prompt := n.buildPrompt(ctx, s)
    if prompt == "" {
        logger.Warn("ReportNode: empty prompt generated")
        n.generateFallbackReport(s)
        return s, nil
    }
    
    // ...后续逻辑不变...
}

func (n *ReportNode) buildPrompt(ctx context.Context, s *state.State) string {
    // 优先使用 Registry
    if n.promptReg != nil {
        renderCtx := n.buildRenderContext(s)
        prompt, err := n.promptReg.Build(ctx, "report", promptregistry.VersionDefault, renderCtx)
        if err == nil {
            return prompt
        }
        logger.Warn("prompt registry build failed for report, falling back", logger.Err(err))
    }
    
    // 降级到旧 Build*() 函数
    return llm.BuildSynthesizePrompt(s)
}

func (n *ReportNode) buildRenderContext(s *state.State) *promptregistry.RenderContext {
    status := "completed"
    if s.LastError != nil {
        status = "partial"
    } else if s.GetIterationCount() >= s.GetMaxIterations() {
        status = "max_iterations_reached"
    }
    k8sSummary := "未获取"
    if s.K8sInfo != nil {
        k8sSummary = s.K8sInfo.GetSummary()
    }
    ctx := &promptregistry.RenderContext{
        UserQuery:       s.UserInput,
        Status:          status,
        ResourceSummary: k8sSummary,
        IsVerifyPhase:   s.VerifyPhase,
    }
    // Findings
    if len(s.AnalysisResult.Findings) == 0 {
        ctx.Findings = "无"
    } else {
        var fs []string
        for _, f := range s.AnalysisResult.Findings {
            fs = append(fs, fmt.Sprintf("- [%s] %s: %s", f.Severity, f.Resource, f.Message))
        }
        ctx.Findings = strings.Join(fs, "\n")
    }
    // CommandSummary
    execs := s.GetCommandExecutions()
    if len(execs) == 0 {
        ctx.CommandSummary = "无"
    } else {
        var cs []string
        for _, e := range execs {
            st := "成功"; if !e.Success { st = "失败" }
            out := e.Output; if len(out) > 4000 { out = out[:4000] + "...[截断]" }
            cs = append(cs, fmt.Sprintf("- %s (%s)\n  输出摘要: %s", e.Command, st, out))
        }
        ctx.CommandSummary = strings.Join(cs, "\n")
    }
    // BlockedCommands
    blocked := s.GetBlockedCommands()
    if len(blocked) > 0 {
        var bs []string
        bs = append(bs, "## 被安全审计拒绝的命令")
        for _, bc := range blocked {
            bs = append(bs, fmt.Sprintf("- 命令: %s\n  原因: %s\n  建议: %s", bc.Command, bc.Reason, bc.Advice))
        }
        ctx.BlockedCommands = strings.Join(bs, "\n")
    }
    // ReasoningChain
    if len(s.ReasoningHistory) > 0 {
        var rs []string
        rs = append(rs, "## 完整推理过程")
        for i, step := range s.ReasoningHistory {
            t := step.Thought; if len(t) > 200 { t = t[:200] + "..." }
            o := step.Observation; if len(o) > 300 { o = o[:300] + "..." }
            rs = append(rs, fmt.Sprintf("轮次%d [%s]:\n思考: %s\n工具结果: %s", i+1, step.Decision, t, o))
        }
        ctx.ReasoningChain = strings.Join(rs, "\n")
    }
    return ctx
}
```

### 6.4 修改 `react_llm.go`（ReAct System Prompt）

`ReActLLM` 新增 `promptReg` 字段，通过 Registry 加载 system prompt，fallback 到旧常量：

```go
// react_llm.go
type ReActLLM struct {
    router       *LLMRouter
    gateway      *gateway.GatewayClient
    safeExecutor SafeCommandExecutor
    recorder     *trc.TaskRecorder
    promptReg    *promptregistry.PromptRegistry  // ← 新增
}

func NewReActLLM(router *LLMRouter, gw *gateway.GatewayClient, safeExecutor SafeCommandExecutor, promptReg *promptregistry.PromptRegistry) *ReActLLM {
    return &ReActLLM{
        router:       router,
        gateway:      gw,
        safeExecutor: safeExecutor,
        promptReg:    promptReg,
    }
}

// GetSystemPrompt 优先从 Registry 加载，fallback 到硬编码常量
func (r *ReActLLM) GetSystemPrompt(ctx context.Context) string {
    if r.promptReg != nil {
        prompt, err := r.promptReg.Build(ctx, "reactSystem", promptregistry.VersionDefault, nil)
        if err == nil {
            return prompt
        }
        logger.Warn("prompt registry build failed for reactSystem, falling back", logger.Err(err))
    }
    return reactSystemPromptTemplate
}
```

**调用方修改**（`DeepQuery` 方法内）：
```go
// 修改前
systemMsg := schema.SystemMessage(BuildReActSystemPrompt())
// 修改后
systemMsg := schema.SystemMessage(r.GetSystemPrompt(ctx))
```

### 6.5 修改 `safety.SafetyAgent`（Safety Audit Prompt）

Safety Prompt 的集成路径：SafetyAgent → PromptRegistry.Build("safety", ...)

```go
// internal/agent/safety/safety_agent.go
type SafetyAgent struct {
    // ...现有字段...
    promptReg *promptregistry.PromptRegistry  // ← 新增
}

func NewSafetyAgent(
    // ...现有参数...
    promptReg *promptregistry.PromptRegistry,  // ← 新增参数
) *SafetyAgent {
    return &SafetyAgent{
        // ...
        promptReg: promptReg,
    }
}

func (a *SafetyAgent) AuditCommand(ctx context.Context, command, reason string) (*AuditResult, error) {
    // 构建 prompt
    prompt := a.buildAuditPrompt(ctx, command, reason)
    
    // ...后续逻辑不变...
}

func (a *SafetyAgent) buildAuditPrompt(ctx context.Context, command, reason string) string {
    // 优先使用 Registry
    if a.promptReg != nil {
        renderCtx := &promptregistry.RenderContext{
            Command: command,
            Reason:  reason,
        }
        prompt, err := a.promptReg.Build(ctx, "safety", promptregistry.VersionDefault, renderCtx)
        if err == nil {
            return prompt
        }
        logger.Warn("prompt registry build failed for safety, falling back", logger.Err(err))
    }
    
    // 降级到旧 Build*() 函数
    return llm.BuildSafetyAuditPrompt(command, reason)
}
```

---

## 7. 模板文件规范

### 7.1 `prompts/registry.yaml`

```yaml
version: "1.0.0"

blocks:
  - name: system_base
    file: blocks/system_base.md
    description: "[1] System Prompt - 基础系统角色（所有阶段共用）"
  - name: tools_definition
    file: blocks/tools_definition.md
    description: "[2] Tool Definitions - 工具列表（K8s + 主机级）"
  - name: constraints_decision
    file: blocks/constraints_decision.md
    description: "[3] Role Constraints - 决策阶段约束"
  - name: constraints_verify
    file: blocks/constraints_verify.md
    description: "[3] Role Constraints - 验证阶段约束"
  - name: reference_patterns
    file: blocks/reference_patterns.md
    description: "[4] Few-shot Examples - 诊断参考表 + 工具原则"

prompts:
  - name: decision
    version: default
    label: "v1.0.0"
    file: decision/default.md
    blocks: [system_base, tools_definition, constraints_decision, reference_patterns]
    model_hint: light
    environment: prod
    description: "[1][2][3][4] + [5-10] 主诊断阶段决策 prompt（ReAct）"
    is_active: true

  - name: verify
    version: default
    label: "v1.0.0"
    file: verify/default.md
    blocks: [system_base, tools_definition, constraints_verify]
    model_hint: light
    environment: prod
    description: "[1][2][3] + [6][8][10] 验证阶段决策 prompt"
    is_active: true

  - name: skill
    version: default
    label: "v1.0.0"
    file: skill/default.md
    blocks: [system_base, tools_definition, constraints_decision]
    model_hint: light
    environment: prod
    description: "[1][2][3] + [5-10] Skill SOP 执行阶段"
    is_active: true

  - name: report
    version: default
    label: "v1.0.0"
    file: report/default.md
    blocks: []
    model_hint: power
    environment: prod
    description: "报告合成 prompt（独立，不引用通用 blocks）"
    is_active: true

  - name: safety
    version: default
    label: "v1.0.0"
    file: safety/default.md
    blocks: []
    model_hint: light
    environment: prod
    description: "Shell 安全审计（独立，不引用通用 blocks）"
    is_active: true

  - name: reactSystem
    version: default
    label: "v1.0.0"
    file: reactSystem/default.md
    blocks: []
    role: system  # ← 特殊：作为 system message
    model_hint: light
    environment: prod
    description: "ReAct 工具链 system prompt（独立，不引用通用 blocks）"
    is_active: true
```

**设计说明**：

| Prompt | Blocks 引用 | 主模板内容 |
|--------|-------------|-----------|
| decision | [1][2][3][4] | [5][6][7][8][9][10] |
| verify | [1][2][3] | [6][8][10] |
| skill | [1][2][3] | [5][6][7][8][9][10] |
| report | 无 | 全部（独立模板） |
| safety | 无 | 全部（独立模板） |
| reactSystem | 无 | 全部（独立固定内容） |

### 7.2 固定块文件（Cache Boundary 之上）

固定块是**纯文本**，不包含 `{{.Field}}` 占位符。它们在 `Build()` 中直接拼接到 prompt 前缀，SHA256 稳定不变。

#### `blocks/system_base.md` — [1] System Prompt

```markdown
你是 Kubernetes 集群诊断专家。你的职责是自主分析问题并选择合适的工具进行调查。

## 工作方式
采用 ReAct（Reasoning + Acting）模式：
1. Thought：分析当前已知信息，推理可能的原因
2. Action：选择合适的工具收集更多信息
3. Observation：观察工具返回的结果
4. 重复以上步骤直到找到根因

## 注意事项
- 所有输出使用中文
- execute_safe_command 必须提供 reason
- 安全审计可能拒绝命令，请根据建议调整
- 日志 tailLines ≤ 200
- 每次最多调用 3 个工具
- 连续 2 次无新信息，停止调查并生成结论
```

#### `blocks/tools_definition.md` — [2] Tool Definitions

```markdown
## 可用工具

### K8s 资源查询
- list_pods: Pod 列表。参数: namespace, labelSelector
- describe_pod: Pod 详情。参数: namespace, name
- get_pod_logs: Pod 日志。参数: namespace, name, container, tailLines
- get_nodes: 节点列表。无参数
- describe_node: 节点详情（含 Allocatable/Allocated）。参数: name
- get_pod_events: Pod 事件。参数: namespace, podName
- list_events: 命名空间事件。参数: namespace
- list_pvc: PVC 状态。参数: namespace
- list_deployments: Deployments。参数: namespace
- list_services: Services。参数: namespace
- list_namespaces: 命名空间。无参数

### 主机级诊断
- execute_safe_command: 执行 Shell 命令（需安全审计）。参数: command, reason
  → 典型命令：top -bn1 | head -20, free -h, df -h
  → 系统日志：journalctl -xeu kubelet --no-pager | tail -50
  → 容器运行时：crictl ps, crictl inspect <id>
  → 网络诊断：curl -s http://<ip>:<port>/healthz, ss -tlnp
  → reason 字段必须说明执行目的
```

#### `blocks/constraints_decision.md` — [3] Role Constraints（决策阶段）

```markdown
## 决策规则
- execute_plan：有明确诊断目标，选择工具执行
- report：已找到根因（有具体证据），或达到最大迭代
- deep_query：需要多步关联调查
- use_skill：当前现象匹配可用专家技能，立即切入

## 工具选择原则（重要）
K8s API 工具（describe/get/logs/events）只能看到**声明式状态**，而 execute_safe_command 能获取**主机实际运行时数据**。二者互补，缺一不可。

**判断何时使用 execute_safe_command 的通用规则：**
- 当你需要的信息是 K8s API 无法直接提供的（如实际 CPU/内存/磁盘使用率、系统日志、容器运行时状态、网络连通性），就应该使用 execute_safe_command
- 当 K8s API 返回的数据不足以解释问题根因（如 Pod 反复重启但日志无明显错误），就应该通过 execute_safe_command 收集主机级证据
- 当需要验证 K8s 声明的状态是否与主机实际情况一致（如 K8s 报告资源不足，需要 top/free 确认真实使用量）

**自检规则：在你准备 decision=report 之前，回顾一下你是否已经同时使用了 K8s API 工具和 execute_safe_command。如果整个诊断过程完全没有调用过 execute_safe_command，请反思是否遗漏了主机级数据采集——除非问题纯粹是 K8s 配置层面的（如 label 不匹配、RBAC 权限），否则几乎都需要主机级数据辅助定位。**

- 如果 execute_safe_command 执行失败，在下一轮 thought 中说明失败原因，尝试换一个更简单的命令重试，不要因此完全放弃主机级诊断

## 严格约束
- 每轮最多 3 个 tool_calls
- 必须有具体证据才能下结论
- 已执行工具无新理由不重复调用
- 空结果不重复调用
```

#### `blocks/constraints_verify.md` — [3] Role Constraints（验证阶段）

```markdown
## 严格约束（必须遵守）
- 只验证上面清单中的疑点，不得开展新的调查方向
- 使用上面「异常 Pod 列表」中的命名空间和 Pod 名，不要用复合命令查找
- tool_calls 的参数必须指向清单中明确提到的资源（命名空间、Pod 名、资源类型）
- **如果异常 Pod 是 Pending 且原因是 Insufficient cpu/memory，必须调用 describe_node(name="上面节点列表中的节点名") 获取节点资源详情**
- 验证阶段可使用 execute_safe_command 在主机上执行命令，获取实时数据作为验证证据（如系统日志、资源占用、网络连通性等）
- 每轮最多 2 个 tool_calls
- 如果清单中的疑点已基本验证完毕，或已达到最大验证轮数，必须 decision=report
```

#### `blocks/reference_patterns.md` — [4] Few-shot Examples

```markdown
## 诊断思路参考（根据问题类型选择工具组合）

| 问题类型 | 推荐工具组合 | 诊断目标 |
|---------|-------------|---------|
| Pod 调度失败(Pending) | get_pod_events, describe_node, describe_pod | 定位 FailedScheduling 原因，计算节点剩余资源 |
| Pod 崩溃重启(CrashLoopBackOff) | get_pod_logs, get_pod_events, describe_pod | 找到崩溃错误日志、BackOff 事件和容器退出码 |
| 镜像拉取失败(ImagePullBackOff) | get_pod_events, describe_pod, execute_safe_command | 通过事件找到拉取失败原因，用 crictl/curl 验证镜像仓库连通性 |
| 内存溢出(OOMKilled) | describe_pod, get_pod_logs, get_pod_events | 确认 limits 配置、分析内存使用模式和 OOM 事件 |
| 系统组件异常 | get_pod_logs, get_pod_events, execute_safe_command | 结合 K8s 日志和 journalctl 系统日志定位根因 |
| Pod 被驱逐(Evicted/Unknown) | list_events, describe_node, execute_safe_command | 检查节点磁盘/内存压力和驱逐事件 |
| 节点资源异常 | describe_node, list_pods, execute_safe_command | 对比 K8s Allocatable/Allocated 和主机实际资源 |

上表为参考，你可以根据实际诊断进展自主组合工具。
```

### 7.3 主模板文件（Cache Boundary 之下）

主模板**只包含动态内容**，通过 `{{.Field}}` 占位符引用 RenderContext 中的数据。固定内容（System Prompt、Tool Definitions、Role Constraints、Few-shot）由 blocks 引用提供，**不在主模板中重复**。

#### `decision/default.md`

```markdown
## 用户查询
{{.UserQuery}}

## 集群状态
{{.ResourceSummary}}

### 异常资源
{{.AbnormalPods}}

{{if .CompressedSummary}}
## 历史推理摘要
{{.CompressedSummary}}
{{end}}

{{.ToolSummary}}

## 已执行的步骤
{{.RecentSteps}}

## 进度
第 {{.Iteration}}/{{.MaxIterations}} 轮
{{if ge .Iteration (div .MaxIterations 2)}}
⚠️ 已执行 {{.Iteration}}/{{.MaxIterations}} 轮，请尽快归纳证据并 decision=report。
如果关键信息已收集完毕（Pending 原因、CrashLoop 日志、节点资源），应立即生成报告。
{{end}}

{{if .SkillList}}
## 可用辅助技能
若当前问题完全匹配以下某个故障场景，应直接返回 {"decision":"use_skill","skill_name":"..."} 切入专属执行轨：

{{.SkillList}}
{{end}}

## 输出格式（严格 JSON，请直接输出纯 JSON 文本，严禁使用 Markdown 代码块包裹）
{
  "thought": "分析当前状态，说明选择哪些工具以及为什么",
  "decision": "execute_plan | deep_query | report | use_skill",
  "skill_name": "仅当 decision=use_skill 时填写",
  "plan": [
    {"step": 1, "description": "步骤描述", "tool_calls": [{"name": "工具名", "args": {}}]}
  ],
  "execute_steps": [1, 2],
  "deep_query_topic": "仅 deep_query 时填写"
}

## 注意
- 每轮最多 3 个工具调用
- 必须有具体证据才能下结论，不要仅凭 Pod 状态猜测
- 上面「已查询工具记录」中列出的工具已执行过，除非有充分理由（如需要不同参数），否则不要重复调用
- 如果某工具返回空结果，不要再次调用相同参数
```

**设计说明**：
- [1] System Prompt → `blocks/system_base.md`（通过 registry.yaml blocks 引用）
- [2] Tool Definitions → `blocks/tools_definition.md`（通过 registry.yaml blocks 引用）
- [3] Role Constraints → `blocks/constraints_decision.md`（通过 registry.yaml blocks 引用）
- [4] Few-shot → `blocks/reference_patterns.md`（通过 registry.yaml blocks 引用）
- [5] Long-term Memory → `{{.CompressedSummary}}`
- [6] Conversation Summary → `{{.UserQuery}}`, `{{.ResourceSummary}}`, `{{.AbnormalPods}}`
- [7] Latest User Input → `{{.UserQuery}}`
- [8] Tool Results → `{{.ToolSummary}}`
- [9] Scratchpad → `{{.RecentSteps}}`
- [10] Planner Thoughts → Progress info + Output Schema

#### `verify/default.md`

```markdown
## 初步根因
{{.InitialRootCause}}

## 异常 Pod 列表（已知信息，直接使用）
{{.AbnormalPodsVerify}}

## 节点列表（用于 describe_node）
{{.NodeList}}

## 待验证疑点清单
{{.RecommendationsChecklist}}

## 已执行的验证查询
{{.VerifyExecutions}}

## 当前进度
第 {{.VerifyIter}}/{{.MaxVerifyIter}} 轮验证迭代。

## 输出格式（严格 JSON，不要包含其他内容，严禁使用 Markdown 代码块包裹）
{
  "thought": "你分析了哪个疑点、选择了哪个工具、为什么",
  "decision": "continue 或 report",
  "tool_calls": [
    { "name": "工具名", "args": { "参数名": "参数值" } }
  ]
}
```

**设计说明**：
- [1] System Prompt → `blocks/system_base.md`（通过 registry.yaml blocks 引用）
- [2] Tool Definitions → `blocks/tools_definition.md`（通过 registry.yaml blocks 引用）
- [3] Role Constraints → `blocks/constraints_verify.md`（通过 registry.yaml blocks 引用）
- [4] Few-shot → 不适用（验证阶段不需要诊断参考表）
- [5] Long-term Memory → 不适用（验证阶段无 CompressedSummary）
- [6] Conversation Summary → `{{.InitialRootCause}}`, `{{.AbnormalPodsVerify}}`, `{{.NodeList}}`
- [7] Latest User Input → 不适用（验证阶段无新 UserQuery）
- [8] Tool Results → `{{.VerifyExecutions}}`
- [9] Scratchpad → 不适用（验证阶段无 RecentSteps）
- [10] Planner Thoughts → Progress info + Output Schema

**注意**：验证阶段的 `decision` 值是 `"continue"` 或 `"report"`（不是 `execute_plan`）。

#### `skill/default.md`

```markdown
## 核心前提环境
{{.UserQuery}}
{{.ResourceSummary}}

### 异常资源
{{.AbnormalPods}}

{{if .CompressedSummary}}
## 历史推理摘要
{{.CompressedSummary}}
{{end}}

{{.ToolSummary}}

## 已经完成的历史步伐
{{.RecentSteps}}

## [指令区] 需要严格遵循的执行说明书
**被激活排查技能：{{.ActiveSkillName}}**

{{.ActiveSkillContent}}

## 执行边界
- 不再参考通用诊断矩阵，也不再重新选择 Skill
- 可以参考上方环境、历史步骤和工具摘要，判断当前 SOP 已执行到哪一步
- 若 Skill 无法继续推进，可直接 decision=report

## 工具映射规则（非常重要）
SKILL 指令中可能会直接呈现 kubectl 语句或主机 Linux Shell 命令。你必须充当"翻译官"，将它们转换为上方【可调用的动作列表】中的确切工具：
1. **Kubernetes 原生查询（如 kubectl get/describe/logs 等）**：禁止直接使用 shell 运行 kubectl，必须映射为上面提供的 list_pods、get_pod_events、describe_pod 等特定的结构化动作工具。
2. **主机/Shell 原生级诊断（如 cat、grep、curl、ping、netstat 等）**：请将其统一包装进 execute_safe_command 工具中执行，并在 reason 字段严谨声明意图，以此触发底层的安全审计和沙箱机制。

（你的唯一职责就是像一名冷静的操控台工兵，看一眼上一步完成到哪里了，结合上述工具映射规则，决定下一个执行什么命令。）

## 输出格式（严格 JSON，请直接输出纯 JSON 文本，严禁使用 Markdown 代码块包裹）
{
  "thought": "说明当前 SOP 进度和下一步要执行什么",
  "decision": "execute_plan | deep_query | report",
  "plan": [
    {"step": 1, "description": "步骤描述", "tool_calls": [{"name": "工具名", "args": {}}]}
  ],
  "execute_steps": [1],
  "deep_query_topic": "仅 deep_query 时填写"
}
```

**设计说明**：
- [1] System Prompt → `blocks/system_base.md`（通过 registry.yaml blocks 引用）
- [2] Tool Definitions → `blocks/tools_definition.md`（通过 registry.yaml blocks 引用）
- [3] Role Constraints → `blocks/constraints_decision.md`（通过 registry.yaml blocks 引用）
- [4] Few-shot → 不适用（Skill 阶段由 SOP 指导，不需要通用参考表）
- [5] Long-term Memory → `{{.CompressedSummary}}`
- [6] Conversation Summary → `{{.UserQuery}}`, `{{.ResourceSummary}}`, `{{.AbnormalPods}}`
- [7] Latest User Input → `{{.UserQuery}}`
- [8] Tool Results → `{{.ToolSummary}}`
- [9] Scratchpad → `{{.RecentSteps}}`
- [10] Planner Thoughts → SOP 进度 + Output Schema

#### `safety/default.md`

```markdown
## 待审计命令
{{.Command}}

## 执行原因
{{.Reason}}

## 安全评估标准

### Safe（安全）
只读操作，不会修改系统状态：
- 查看文件内容：cat, head, tail, less
- 系统状态查询：df, du, free, uptime, top, ps, vmstat, iostat
- 网络诊断：ping, traceroute, dig, nslookup, ss, netstat, ip addr/route
- 容器状态：crictl ps/logs, docker ps/logs/inspect
- 日志查看：journalctl, dmesg
- 文本处理：grep, awk, sed（仅输出，不带 -i）, wc, sort, uniq

### Warning（警告）
可能影响系统但通常可控：
- 查看状态类：systemctl status, docker inspect
- 有限写入：echo 到非系统文件
- 信息收集类：lsof, strace -p（短时间）

### Dangerous（危险）
会修改/删除数据、停止服务、更改权限、执行远程代码：
- 删除：rm, rmdir（带 -r/-rf）
- 磁盘操作：mkfs, dd, fdisk, mount/umount
- 服务控制：systemctl stop/disable/restart, kill, pkill
- 权限更改：chmod 777, chown -R
- 网络更改：iptables -F/-X, ip link set down
- 远程执行：curl|sh, wget|sh, eval, exec
- 命令替换：包含 $()、反引号、管道到 sh/bash

## 输出格式（严格 JSON，不要包含其他内容，严禁使用 Markdown 代码块包裹）
{
  "safety_level": "safe 或 warning 或 dangerous",
  "reason": "1-2 句话说明判断理由",
  "advice": "如果判定为 dangerous，建议一个更安全的替代命令；否则为空字符串"
}
```

**设计说明**：Safety prompt 不需要 blocks 引用（无 tools、无 constraints），内容完全自包含。

#### `reactSystem/default.md`

```markdown
你是一个资深的 Kubernetes 集群故障诊断工程师。你的任务是通过调用工具收集信息，分析问题根因，并给出修复建议。

## 工作方式
你将采用 ReAct（Reasoning + Acting）模式工作：
1. **Thought**：分析当前已知信息，推理可能的原因
2. **Action**：选择合适的工具收集更多信息
3. **Observation**：观察工具返回的结果
4. 重复以上步骤直到找到根因

## 注意事项
- 所有输出使用中文
- 使用 execute_safe_command 时必须提供 reason（为什么要执行这个命令）
- execute_safe_command 可能被安全审计拒绝，这是正常行为，请根据拒绝建议调整命令
- 查看日志时务必限制行数（tailLines ≤ 200），避免输出过长
- 每次最多调用 3 个工具
- 如果连续 2 次未获得新信息，应停止调查并基于已有信息给出结论
```

**设计说明**：ReactSystem 是纯固定内容，不需要 blocks 引用，直接作为 system message 传递。

#### `report/default.md`

```markdown
{{if .IsVerifyPhase}}
## 诊断阶段
最终验证阶段：以下诊断命令中包含了对初步建议的自动验证结果。
请综合所有信息生成最终完整报告。
如验证结果与初步结论不符，**以验证结果为准修正根因判断**，确保报告内部一致。
{{end}}

你是 Kubernetes 集群诊断报告撰写专家。请根据以下诊断数据生成结构化的中文诊断报告。

## 用户查询
{{.UserQuery}}

## 诊断状态
{{.Status}}

## 集群资源概况
{{.ResourceSummary}}

## 关键发现
{{.Findings}}

## 已执行的诊断命令
{{.CommandSummary}}

{{if .BlockedCommands}}
## 被安全审计拒绝的命令
{{.BlockedCommands}}
{{end}}

{{if .ReasoningChain}}
## 完整推理过程
{{.ReasoningChain}}
{{end}}

## 报告输出格式（严格 JSON，禁止 Markdown 代码块包裹）
{
  "summary": "一句话总结诊断结论",
  "severity": "critical / warning / info",
  "root_cause": "根因分析",
  "findings": [
    {
      "resource": "受影响的资源名",
      "severity": "critical / warning / info",
      "message": "具体发现描述",
      "evidence": "支持该发现的证据"
    }
  ],
  "recommendations": [
    {
      "priority": "high / medium / low",
      "action": "建议的修复操作",
      "command": "具体的修复命令（如有）",
      "risk": "操作风险说明",
      "executable": true
    }
  ],
  "limitations": "诊断过程中的限制说明"
}

报告规则：
- 所有内容使用中文
- findings 按 severity 从高到低排序
- recommendations 按 priority 从高到低排序
- evidence 字段应引用具体的日志行或指标数值，不要泛泛而谈
```

---

## 8. 迁移计划

### 原则

零停机、可回滚。每一步保持向后兼容。

### Step 1：创建 Registry 包 + 辅助函数（1-2 天）

- 新增 `internal/llm/promptregistry/` 包
- 实现 `registry.go`、`types.go`、`provider.go`、`renderer.go`
- 实现 `helpers.go`（从 `prompts.go` 提取辅助函数）
- 新增 `prompts/` 目录和模板文件
- 单元测试

**验证**：
```bash
go build ./...
go test ./internal/llm/promptregistry/...
```

### Step 2：Config 扩展（0.5 天）

- 在 `config.go` 中新增 `PromptConfig` 类型
- 在 `Config` 结构体中添加 `Prompt` 字段
- 在 `setDefaults()` 中设置默认值

**验证**：
```bash
go build ./...
go test ./internal/config/...
```

### Step 3：Agent + Node 集成（1 天）

- 修改 `agent.go`：初始化 Registry 并传递给节点
- 修改 `DecisionNode`：增加 `promptReg` 字段和 fallback 逻辑
- 修改 `ReportNode`：同上
- 更新构造函数签名

**验证**：
```bash
go build ./...
go test ./internal/agent/diagnosis/...
```

### Step 4：回归测试 + 清理（0.5 天）

- 用 trace 数据回放验证
- 可选删除 `prompts.go` 中的旧 const
- 更新文档

**验证**：
```bash
go test ./...
```

---

## 9. 验证方案

### 9.1 静态验证

```bash
go build ./...
go vet ./...
go test ./internal/llm/...
go test ./internal/llm/promptregistry/...
go test ./internal/agent/diagnosis/...
```

### 9.2 Prompt 一致性回归测试

```go
// registry_test.go
func TestMigrationParity(t *testing.T) {
    state := createTestState()
    
    // V1：旧方式
    oldPrompt := llm.BuildDecisionPrompt(state, "")
    
    // V2：Registry 方式
    reg := NewPromptRegistry()
    reg.Load("./prompts")
    renderCtx := buildTestRenderContext(state)
    newPrompt, _ := reg.Build(ctx, "decision", "default", renderCtx)
    
    // 比对关键段
    assert.Contains(t, newPrompt, "你是 Kubernetes 集群诊断专家")
    assert.Contains(t, newPrompt, "list_pods")
    assert.Contains(t, newPrompt, "execute_plan")
}
```

### 9.3 Trace 回放验证

选取 2-3 个典型 trace，用 trace 中的 state 回放 Registry prompt，确保关键 blocks 齐全。

---

## 10. 风险与缓解

| 风险 | 缓解 |
|------|------|
| 迁移引入文字差异 | 逐段比对 + 回归测试 |
| text/template 转义问题 | `missingkey=zero` + 边界测试 |
| Registry 加载失败 | fallback 到旧 `Build*()` 函数 |
| 模板目录未部署 | 启动时检查目录存在性，失败则用 fallback |
| 跨平台路径兼容 | `filepath.Join` 统一路径分隔符 |

---

## 11. 时间线

| 步骤 | 预估 | 说明 |
|------|------|------|
| Registry 包 + 辅助函数 | 1-2 天 | registry.go, types.go, provider.go, helpers.go |
| 模板文件（全部） | 1 天 | 含 decision/verify/skill/report/safety/reactSystem |
| Config 扩展 | 0.5 天 | PromptConfig + setDefaults |
| Agent + Node 集成 | 1.5 天 | DecisionNode, ReportNode, SafetyAgent, react_llm |
| 回归测试 + 清理 | 0.5 天 | trace 回放 + 旧 const 清理 |
| **总计** | **4.5-5.5 天** | 比 v1 增加 1.5 天（模板补全 + SafetyAgent 集成） |

---

## 12. 后续扩展

1. **版本对比**：记录每次 prompt 变更的 diff，支持一键回滚
2. **A/B 路由**：按流量比例分发 prompt 版本
3. **评测流水线**：自动运行测试集，对比版本质量
4. **外部 Provider**：支持从 Redis/DB 加载 prompt（不改二进制）
5. **热重载**：文件监控 + 自动重新加载模板目录
6. **Trace 集成**：在 trace 中记录 `prompt_name` / `prompt_version` / `prompt_hash`

---

## 附录 A：完整文件清单

```
新增文件：
├── internal/llm/promptregistry/
│   ├── registry.go          ← 含 Render/ValidateTemplate 方法
│   ├── types.go
│   ├── provider.go
│   ├── helpers.go           ← 精简为 3 个通用工具函数
│   └── registry_test.go
├── prompts/
│   ├── registry.yaml
│   ├── blocks/
│   │   ├── system_base.md
│   │   ├── tools_definition.md
│   │   ├── constraints_decision.md
│   │   ├── constraints_verify.md
│   │   └── reference_patterns.md
│   ├── decision/default.md
│   ├── verify/default.md
│   ├── skill/default.md
│   ├── report/default.md
│   ├── safety/default.md
│   └── reactSystem/default.md  ← 新增

修改文件：
├── internal/config/config.go          ← 新增 PromptConfig
├── internal/agent/diagnosis/agent.go  ← 新增 promptDir 参数
├── internal/agent/diagnosis/decision_node.go  ← 集成 Registry
├── internal/agent/diagnosis/report_node.go    ← 集成 Registry
├── internal/agent/safety/safety_agent.go      ← 集成 Registry
├── cmd/k8s-analyzer/main.go                   ← 传递 promptDir
└── configs/config.yaml                ← 新增 prompt 配置段
```

---

## 附录 B：评审问题修复清单

| # | 问题 | 修复措施 | 位置 |
|---|------|---------|------|
| 1 | reactSystem 模板缺失 | 新增 `prompts/reactSystem/default.md` | §7.3 |
| 2 | safety 模板无内容 | 补充完整 safety 模板（含 Command/Reason） | §7.3 |
| 3 | verify 模板缺失 | 补充完整 verify 模板（含约束） | §7.3 |
| 4 | skill 模板缺失 | 补充完整 skill 模板（含工具映射规则） | §7.3 |
| 5 | decision 模板缩水 | 逐段对比 prompts.go，100% 覆盖 | §7.3 |
| 6 | GetRecentSteps(2) | 改为 GetRecentSteps(3) | §6.2 |
| 7 | verify decision 值差异 | 文档明确 "continue 或 report" | §7.3 |
| 8 | NewAgent 签名 | 改用 promptDir string 参数 | §6.1 |
| 9 | Safety Registry 路径 | 新增 Command/Reason 到 RenderContext | §3.1, §6.5 |
| 10 | reactSystem 集成 | 使用方案 B：通过 Registry 加载（带 fallback） | §6.4 |
| 11 | AbnormalPods 格式 | 新增 AbnormalPodsVerify 字段 | §3.1 |
| 12 | Executable 字段 | 文档说明（现有问题，非本次引入） | §7.3 |

---

## 附录 C：已知限制

### Executable 字段

`Recommendation` 结构体当前没有 `Executable` 字段。这是现有代码的限制，不是本次重构引入的问题。建议在后续迭代中补齐：
1. 在 `state.Recommendation` 中新增 `Executable bool`
2. 在 `ParseAnalysisResponse` 中解析该字段
3. 在报告模板中引用

### Ge/Le 等函数

Go 模板内置了 `ge`/`gt`/`le`/`lt`/`eq` 等比较函数，无需在 promptFuncMap 中重复定义。只添加 `div`/`sub` 等内置不支持的数学运算。
