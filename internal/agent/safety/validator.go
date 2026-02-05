// Package safety 提供命令安全验证功能
package safety

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/AceDarkknight/k8s-analyzer-agent/internal/config"
	"github.com/AceDarkknight/k8s-analyzer-agent/internal/logger"
)

// SafetyLevel 安全级别定义
type SafetyLevel int

const (
	// SafetyLevelSafe 安全级别：安全
	SafetyLevelSafe SafetyLevel = iota
	// SafetyLevelWarning 安全级别：警告
	SafetyLevelWarning
	// SafetyLevelDangerous 安全级别：危险
	SafetyLevelDangerous
)

// AuditResult 审计结果
// 包含命令的安全级别、拒绝理由和建议
type AuditResult struct {
	// SafetyLevel 安全级别
	SafetyLevel SafetyLevel

	// Reason 拒绝或警告的理由（当 SafetyLevel 为 Warning 或 Dangerous 时）
	Reason string

	// Advice 改进建议（可选）
	Advice string

	// IsAllowed 是否允许执行该命令
	IsAllowed bool
}

// LLMAuditor LLM 审计器接口
// 用于对命令进行语义级别的安全审计
type LLMAuditor interface {
	// AuditCommand 审计命令的安全性
	AuditCommand(ctx context.Context, command string, contextInfo map[string]interface{}) (*AuditResult, error)
}

// SecurityConfig 安全配置
type SecurityConfig struct {
	AllowReadOnly       bool     `json:"allow_read_only"`
	CommandWhitelist    []string `json:"command_whitelist"`
	BlacklistedCommands []string `json:"blacklisted_commands"`
	DangerousArgsRegex  []string `json:"dangerous_args_regex"`

	// EnableLLMAudit 是否启用 LLM 审计
	EnableLLMAudit bool `json:"enable_llm_audit"`

	// AuditRiskThreshold 审计风险阈值（0-10），超过此值的命令将被拒绝
	// 0-3: 安全，4-6: 警告但允许，7-10: 危险拒绝
	AuditRiskThreshold int `json:"audit_risk_threshold"`
}

// Validator 命令安全验证器
type Validator struct {
	config          *SecurityConfig
	compiledRegexes []*regexp.Regexp
	llmAuditor      LLMAuditor // LLM 审计器
}

// NewValidator 创建新的安全验证器
func NewValidator(configPath string, llmAuditor LLMAuditor) (*Validator, error) {
	// 读取配置文件
	configData, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// 解析配置
	var config struct {
		Security SecurityConfig `json:"security"`
	}
	if err := json.Unmarshal(configData, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	// 编译正则表达式
	compiledRegexes := make([]*regexp.Regexp, 0, len(config.Security.DangerousArgsRegex))
	for _, pattern := range config.Security.DangerousArgsRegex {
		re, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("failed to compile regex pattern '%s': %w", pattern, err)
		}
		compiledRegexes = append(compiledRegexes, re)
	}

	return &Validator{
		config:          &config.Security,
		compiledRegexes: compiledRegexes,
		llmAuditor:      llmAuditor,
	}, nil
}

// NewValidatorWithConfig 使用配置对象创建验证器
func NewValidatorWithConfig(config *SecurityConfig, llmAuditor LLMAuditor) (*Validator, error) {
	// 编译正则表达式
	compiledRegexes := make([]*regexp.Regexp, 0, len(config.DangerousArgsRegex))
	for _, pattern := range config.DangerousArgsRegex {
		re, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("failed to compile regex pattern '%s': %w", pattern, err)
		}
		compiledRegexes = append(compiledRegexes, re)
	}

	return &Validator{
		config:          config,
		compiledRegexes: compiledRegexes,
		llmAuditor:      llmAuditor,
	}, nil
}

// ValidateCommand 验证命令是否安全
func (v *Validator) ValidateCommand(command string) error {
	if command == "" {
		return fmt.Errorf("command is empty")
	}

	// 提取命令名称（第一个单词）
	commandName := v.extractCommandName(command)

	// 1. 检查黑名单
	if v.isBlacklisted(commandName) {
		return &UnsafeCommandError{
			Command: command,
			Reason:  fmt.Sprintf("command '%s' is in blacklist", commandName),
		}
	}

	// 2. 检查白名单（如果配置了白名单）
	if len(v.config.CommandWhitelist) > 0 {
		if !v.isWhitelisted(commandName) {
			return &UnsafeCommandError{
				Command: command,
				Reason:  fmt.Sprintf("command '%s' is not in whitelist", commandName),
			}
		}
	}

	// 3. 检查危险参数模式
	if v.hasDangerousPattern(command) {
		return &UnsafeCommandError{
			Command: command,
			Reason:  "command contains dangerous argument pattern",
		}
	}

	return nil
}

// ValidateCommandWithAudit 验证命令是否安全，并执行 LLM 审计
// 返回审计结果（如果启用了 LLM 审计）和错误
func (v *Validator) ValidateCommandWithAudit(ctx context.Context, command string, contextInfo map[string]interface{}) (*AuditResult, error) {
	// 先执行规则验证
	if err := v.ValidateCommand(command); err != nil {
		return nil, err
	}

	// 如果未启用 LLM 审计，返回默认的安全结果
	if !v.config.EnableLLMAudit {
		return &AuditResult{
			SafetyLevel: SafetyLevelSafe,
			Reason:      "LLM 审计未启用",
			Advice:      "",
			IsAllowed:   true,
		}, nil
	}

	// 如果没有配置 LLM 审计器，返回警告
	if v.llmAuditor == nil {
		logger.Warn("[Validator] LLM 审计已启用但未配置审计器")
		return &AuditResult{
			SafetyLevel: SafetyLevelWarning,
			Reason:      "LLM 审计器未配置，仅规则验证通过",
			Advice:      "建议配置 LLM 审计器以提高安全性",
			IsAllowed:   true,
		}, nil
	}

	// 执行 LLM 审计
	logger.Info("[Validator] Executing LLM audit", logger.String("command", command))
	auditResult, err := v.llmAuditor.AuditCommand(ctx, command, contextInfo)
	if err != nil {
		logger.Error("[Validator] LLM audit failed", logger.Err(err))
		return nil, fmt.Errorf("LLM audit failed: %w", err)
	}

	// 根据审计结果决定是否允许执行
	if !auditResult.IsAllowed {
		logger.Warn("[Validator] Command rejected by LLM audit", logger.String("reason", auditResult.Reason))
		return auditResult, &UnsafeCommandError{
			Command: command,
			Reason:  fmt.Sprintf("LLM 审计拒绝: %s", auditResult.Reason),
		}
	}

	// 记录审计结果
	switch auditResult.SafetyLevel {
	case SafetyLevelSafe:
		logger.Info("[Validator] LLM audit passed: Safe")
	case SafetyLevelWarning:
		logger.Info("[Validator] LLM audit passed: Warning", logger.String("reason", auditResult.Reason))
	case SafetyLevelDangerous:
		logger.Warn("[Validator] LLM audit result: Dangerous but allowed", logger.String("reason", auditResult.Reason))
	}

	return auditResult, nil
}

// extractCommandName 从命令中提取命令名称
func (v *Validator) extractCommandName(command string) string {
	// 去除前后空格
	command = strings.TrimSpace(command)

	// 查找第一个空格或特殊字符
	for i, r := range command {
		if r == ' ' || r == '\t' || r == ';' || r == '|' || r == '&' || r == '>' || r == '<' {
			return command[:i]
		}
	}

	return command
}

// isBlacklisted 检查命令是否在黑名单中
func (v *Validator) isBlacklisted(commandName string) bool {
	for _, blacklisted := range v.config.BlacklistedCommands {
		if strings.EqualFold(commandName, blacklisted) {
			return true
		}
	}
	return false
}

// isWhitelisted 检查命令是否在白名单中
func (v *Validator) isWhitelisted(commandName string) bool {
	for _, whitelisted := range v.config.CommandWhitelist {
		if strings.EqualFold(commandName, whitelisted) {
			return true
		}
	}
	return false
}

// hasDangerousPattern 检查命令是否包含危险参数模式
func (v *Validator) hasDangerousPattern(command string) bool {
	for _, re := range v.compiledRegexes {
		if re.MatchString(command) {
			return true
		}
	}
	return false
}

// GetConfig 获取当前配置
func (v *Validator) GetConfig() *SecurityConfig {
	return v.config
}

// UnsafeCommandError 不安全命令错误
type UnsafeCommandError struct {
	Command string
	Reason  string
}

// Error 实现 error 接口
func (e *UnsafeCommandError) Error() string {
	return fmt.Sprintf("unsafe command: %s - %s", e.Command, e.Reason)
}

// IsUnsafeCommand 判断错误是否为不安全命令错误
func IsUnsafeCommand(err error) bool {
	_, ok := err.(*UnsafeCommandError)
	return ok
}

// RuleBasedAuditor 基于规则的审计器
// 使用预定义规则进行命令审计
type RuleBasedAuditor struct {
	modelName string // 模型名称（从配置中传入）
}

// NewRuleBasedAuditor 创建基于规则的审计器
func NewRuleBasedAuditor(llmConfig *config.LLMConfig) *RuleBasedAuditor {
	auditor := &RuleBasedAuditor{
		modelName: llmConfig.Model,
	}

	// 记录使用的模型
	logger.Info("[Safety] RuleBasedAuditor initialized",
		logger.String("model", auditor.modelName),
		logger.String("provider", llmConfig.Provider))

	return auditor
}

// AuditCommand 审计命令的安全性
func (a *RuleBasedAuditor) AuditCommand(ctx context.Context, command string, contextInfo map[string]interface{}) (*AuditResult, error) {
	logger.Debug("[RuleBasedAuditor] Auditing command", logger.String("command", command))

	// 默认审计结果：安全
	result := &AuditResult{
		SafetyLevel: SafetyLevelSafe,
		Reason:      "",
		Advice:      "",
		IsAllowed:   true,
	}

	// 定义危险命令模式（简化版规则）
	dangerousPatterns := []struct {
		pattern string
		reason  string
		advice  string
	}{
		{
			pattern: `rm\s+-rf\s+/`,
			reason:  "命令包含危险的删除操作，可能删除系统关键文件",
			advice:  "请确认删除的目标路径，避免使用通配符或根目录",
		},
		{
			pattern: `dd\s+if=/dev/zero`,
			reason:  "命令可能破坏磁盘数据",
			advice:  "请确认 dd 命令的目标设备，避免误操作",
		},
		{
			pattern: `:(){ :|:& };:`,
			reason:  "命令包含 fork bomb 模式，可能导致系统资源耗尽",
			advice:  "请避免使用无限递归的 shell 函数",
		},
		{
			pattern: `chmod\s+777\s+/`,
			reason:  "命令将根目录权限设置为 777，存在严重安全风险",
			advice:  "请仅对必要的目录设置适当的权限",
		},
		{
			pattern: `curl.*\|.*sh`,
			reason:  "命令从网络下载并执行脚本，存在代码注入风险",
			advice:  "请先下载脚本，检查内容后再执行",
		},
		{
			pattern: `wget.*\|.*sh`,
			reason:  "命令从网络下载并执行脚本，存在代码注入风险",
			advice:  "请先下载脚本，检查内容后再执行",
		},
		{
			pattern: `kubectl\s+delete\s+.*--all`,
			reason:  "命令使用 --all 参数删除所有资源，风险较高",
			advice:  "请明确指定要删除的资源名称，避免批量删除",
		},
		{
			pattern: `kubectl\s+delete\s+ns\s+\S+`,
			reason:  "命令删除命名空间，会删除该命名空间下的所有资源",
			advice:  "请确认命名空间名称，避免误删",
		},
	}

	// 检查危险模式
	for _, dp := range dangerousPatterns {
		matched, _ := regexp.MatchString(dp.pattern, command)
		if matched {
			result.SafetyLevel = SafetyLevelDangerous
			result.Reason = dp.reason
			result.Advice = dp.advice
			result.IsAllowed = false
			logger.Warn("[RuleBasedAuditor] Command rejected", logger.String("reason", dp.reason))
			return result, nil
		}
	}

	// 检查警告模式（需要谨慎但可以执行）
	warningPatterns := []struct {
		pattern string
		reason  string
		advice  string
	}{
		{
			pattern: `kubectl\s+delete\s+`,
			reason:  "命令删除 K8s 资源，请谨慎操作",
			advice:  "建议先使用 --dry-run 参数预览操作",
		},
		{
			pattern: `kubectl\s+edit\s+`,
			reason:  "命令直接编辑 K8s 资源，请确认修改内容",
			advice:  "建议先备份资源配置",
		},
		{
			pattern: `kubectl\s+apply\s+-f\s+-`,
			reason:  "命令从标准输入应用配置，请确认输入内容",
			advice:  "建议先检查配置文件的正确性",
		},
	}

	// 检查警告模式
	for _, wp := range warningPatterns {
		matched, _ := regexp.MatchString(wp.pattern, command)
		if matched {
			result.SafetyLevel = SafetyLevelWarning
			result.Reason = wp.reason
			result.Advice = wp.advice
			result.IsAllowed = true // 警告级别允许执行
			logger.Info("[RuleBasedAuditor] Command warning", logger.String("reason", wp.reason))
			return result, nil
		}
	}

	logger.Info("[RuleBasedAuditor] Command is safe")
	return result, nil
}

// MockAuditor 模拟审计器
// 用于测试和演示，返回预设的审计结果
type MockAuditor struct{}

// NewMockAuditor 创建模拟审计器
func NewMockAuditor() *MockAuditor {
	return &MockAuditor{}
}

// AuditCommand 模拟命令审计
func (a *MockAuditor) AuditCommand(ctx context.Context, command string, contextInfo map[string]interface{}) (*AuditResult, error) {
	logger.Debug("[MockAuditor] Auditing command", logger.String("command", command))

	// 默认审计结果：安全
	result := &AuditResult{
		SafetyLevel: SafetyLevelSafe,
		Reason:      "",
		Advice:      "",
		IsAllowed:   true,
	}

	// 简单的模拟逻辑：检查是否包含危险关键词
	if strings.Contains(command, "rm -rf") || strings.Contains(command, "delete") {
		result.SafetyLevel = SafetyLevelWarning
		result.Reason = "模拟审计：命令包含删除操作，请谨慎"
		result.Advice = "建议先确认删除目标"
		result.IsAllowed = true // 模拟中允许执行
		logger.Info("[MockAuditor] Command warning", logger.String("reason", result.Reason))
	} else {
		logger.Info("[MockAuditor] Command is safe")
	}

	return result, nil
}
