package skill

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/AceDarkknight/k8s-analyzer-agent/internal/logger"
	"github.com/cloudwego/eino-ext/adk/backend/local"
	einoSkill "github.com/cloudwego/eino/adk/middlewares/skill"
)

// Loader 提供 Skill 元数据与内容读取能力。
type Loader struct {
	backend einoSkill.Backend
	cached  []einoSkill.FrontMatter
	dir     string
}

// NewLoader 创建 Skill Loader。
// 当目录不存在时记录 warn 并降级为“无可用 Skill”。
func NewLoader(ctx context.Context, skillsDir string) (*Loader, error) {
	if skillsDir == "" {
		skillsDir = "./skills"
	}

	info, err := os.Stat(skillsDir)
	if err != nil {
		if os.IsNotExist(err) {
			logger.Warn("skill: skill directory not found, fallback to normal flow", logger.String("dir", skillsDir))
			return &Loader{dir: skillsDir}, nil
		}
		return nil, fmt.Errorf("skill: failed to stat skills dir: %w", err)
	}
	if !info.IsDir() {
		logger.Warn("skill: configured path is not a directory, fallback to normal flow", logger.String("dir", skillsDir))
		return &Loader{dir: skillsDir}, nil
	}

	be, err := local.NewBackend(ctx, &local.Config{})
	if err != nil {
		return nil, fmt.Errorf("skill: failed to create local backend: %w", err)
	}

	skillBackend, err := einoSkill.NewBackendFromFilesystem(ctx, &einoSkill.BackendFromFilesystemConfig{
		Backend: be,
		BaseDir: skillsDir,
	})
	if err != nil {
		return nil, fmt.Errorf("skill: failed to create skill backend: %w", err)
	}

	matters, err := skillBackend.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("skill: failed to list skills: %w", err)
	}

	return &Loader{backend: skillBackend, cached: matters, dir: skillsDir}, nil
}

// ListSkills 返回缓存的 Skill 元数据。
func (l *Loader) ListSkills() []einoSkill.FrontMatter {
	if l == nil || len(l.cached) == 0 {
		return nil
	}
	result := make([]einoSkill.FrontMatter, len(l.cached))
	copy(result, l.cached)
	return result
}

// GetSkillContent 读取指定 Skill 的正文内容。
func (l *Loader) GetSkillContent(ctx context.Context, name string) (string, error) {
	if l == nil || l.backend == nil {
		return "", fmt.Errorf("skill loader is not initialized")
	}
	s, err := l.backend.Get(ctx, name)
	if err != nil {
		return "", fmt.Errorf("skill [%s] not found: %w", name, err)
	}
	return s.Content, nil
}

// BuildSkillSummary 构建可注入 Prompt 的 Skill 摘要列表。
func (l *Loader) BuildSkillSummary() string {
	if l == nil || len(l.cached) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, m := range l.cached {
		desc := strings.TrimSpace(m.Description)
		if desc == "" {
			desc = "暂无描述"
		}
		sb.WriteString(fmt.Sprintf("- **%s**：%s\n", m.Name, desc))
	}
	return sb.String()
}

// HasSkills 判断当前是否存在可用 Skill。
func (l *Loader) HasSkills() bool {
	return l != nil && len(l.cached) > 0
}
