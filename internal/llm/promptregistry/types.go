package promptregistry

import (
	"fmt"
	"strings"
	"time"
)

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

// PromptSpec 描述一个完整的 prompt 资产。
type PromptSpec struct {
	Name         PromptID       `json:"name" yaml:"name"`
	Version      PromptVersion  `json:"version" yaml:"version"`
	Label        string         `json:"label" yaml:"label"`
	Template     string         `json:"template" yaml:"template"`
	SystemPrompt string         `json:"system_prompt" yaml:"system_prompt"`
	Blocks       []PromptID     `json:"blocks" yaml:"blocks"`
	Role         PromptRole     `json:"role" yaml:"role"`
	ModelHint    string         `json:"model_hint" yaml:"model_hint"`
	Metadata     PromptMetadata `json:"metadata" yaml:"metadata"`
	IsActive     bool           `json:"is_active" yaml:"is_active"`
	CreatedAt    time.Time      `json:"created_at" yaml:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at" yaml:"updated_at"`
}

type PromptMetadata struct {
	Description string   `json:"description" yaml:"description"`
	Tags        []string `json:"tags" yaml:"tags"`
	Environment string   `json:"environment" yaml:"environment"`
}

type DecisionPromptContext struct {
	UserQuery         string
	ResourceSummary   string
	AbnormalPods      string
	CompressedSummary string
	ToolSummary       string
	RecentSteps       string
	Iteration         int
	MaxIterations     int
	SkillList         string
}

func (c *DecisionPromptContext) Validate() error {
	if c == nil {
		return fmt.Errorf("DecisionPromptContext is nil")
	}
	if strings.TrimSpace(c.UserQuery) == "" {
		return fmt.Errorf("DecisionPromptContext.UserQuery is required")
	}
	if strings.TrimSpace(c.ResourceSummary) == "" {
		return fmt.Errorf("DecisionPromptContext.ResourceSummary is required")
	}
	if strings.TrimSpace(c.AbnormalPods) == "" {
		return fmt.Errorf("DecisionPromptContext.AbnormalPods is required")
	}
	if strings.TrimSpace(c.RecentSteps) == "" {
		return fmt.Errorf("DecisionPromptContext.RecentSteps is required")
	}
	if c.MaxIterations <= 0 {
		return fmt.Errorf("DecisionPromptContext.MaxIterations must be positive")
	}
	if c.Iteration < 0 {
		return fmt.Errorf("DecisionPromptContext.Iteration must be non-negative")
	}
	return nil
}

type VerifyPromptContext struct {
	InitialRootCause         string
	AbnormalPodsVerify       string
	NodeList                 string
	RecommendationsChecklist string
	VerifyExecutions         string
	VerifyIter               int
	MaxVerifyIter            int
}

func (c *VerifyPromptContext) Validate() error {
	if c == nil {
		return fmt.Errorf("VerifyPromptContext is nil")
	}
	if strings.TrimSpace(c.InitialRootCause) == "" {
		return fmt.Errorf("VerifyPromptContext.InitialRootCause is required")
	}
	if strings.TrimSpace(c.AbnormalPodsVerify) == "" {
		return fmt.Errorf("VerifyPromptContext.AbnormalPodsVerify is required")
	}
	if strings.TrimSpace(c.NodeList) == "" {
		return fmt.Errorf("VerifyPromptContext.NodeList is required")
	}
	if strings.TrimSpace(c.RecommendationsChecklist) == "" {
		return fmt.Errorf("VerifyPromptContext.RecommendationsChecklist is required")
	}
	if strings.TrimSpace(c.VerifyExecutions) == "" {
		return fmt.Errorf("VerifyPromptContext.VerifyExecutions is required")
	}
	if c.MaxVerifyIter <= 0 {
		return fmt.Errorf("VerifyPromptContext.MaxVerifyIter must be positive")
	}
	if c.VerifyIter <= 0 {
		return fmt.Errorf("VerifyPromptContext.VerifyIter must be positive")
	}
	return nil
}

type SkillPromptContext struct {
	UserQuery          string
	ResourceSummary    string
	AbnormalPods       string
	CompressedSummary  string
	ToolSummary        string
	RecentSteps        string
	ActiveSkillName    string
	ActiveSkillContent string
}

func (c *SkillPromptContext) Validate() error {
	if c == nil {
		return fmt.Errorf("SkillPromptContext is nil")
	}
	if strings.TrimSpace(c.UserQuery) == "" {
		return fmt.Errorf("SkillPromptContext.UserQuery is required")
	}
	if strings.TrimSpace(c.ResourceSummary) == "" {
		return fmt.Errorf("SkillPromptContext.ResourceSummary is required")
	}
	if strings.TrimSpace(c.AbnormalPods) == "" {
		return fmt.Errorf("SkillPromptContext.AbnormalPods is required")
	}
	if strings.TrimSpace(c.RecentSteps) == "" {
		return fmt.Errorf("SkillPromptContext.RecentSteps is required")
	}
	if strings.TrimSpace(c.ActiveSkillName) == "" {
		return fmt.Errorf("SkillPromptContext.ActiveSkillName is required")
	}
	if strings.TrimSpace(c.ActiveSkillContent) == "" {
		return fmt.Errorf("SkillPromptContext.ActiveSkillContent is required")
	}
	return nil
}

type ReportPromptContext struct {
	UserQuery       string
	Status          string
	ResourceSummary string
	Findings        string
	CommandSummary  string
	BlockedCommands string
	ReasoningChain  string
	IsVerifyPhase   bool
}

func (c *ReportPromptContext) Validate() error {
	if c == nil {
		return fmt.Errorf("ReportPromptContext is nil")
	}
	if strings.TrimSpace(c.UserQuery) == "" {
		return fmt.Errorf("ReportPromptContext.UserQuery is required")
	}
	if strings.TrimSpace(c.Status) == "" {
		return fmt.Errorf("ReportPromptContext.Status is required")
	}
	if strings.TrimSpace(c.ResourceSummary) == "" {
		return fmt.Errorf("ReportPromptContext.ResourceSummary is required")
	}
	if strings.TrimSpace(c.Findings) == "" {
		return fmt.Errorf("ReportPromptContext.Findings is required")
	}
	if strings.TrimSpace(c.CommandSummary) == "" {
		return fmt.Errorf("ReportPromptContext.CommandSummary is required")
	}
	return nil
}

type SafetyPromptContext struct {
	Command string
	Reason  string
}

func (c *SafetyPromptContext) Validate() error {
	if c == nil {
		return fmt.Errorf("SafetyPromptContext is nil")
	}
	if strings.TrimSpace(c.Command) == "" {
		return fmt.Errorf("SafetyPromptContext.Command is required")
	}
	if strings.TrimSpace(c.Reason) == "" {
		return fmt.Errorf("SafetyPromptContext.Reason is required")
	}
	return nil
}
