// Package safety 提供命令安全执行 Agent
package safety

import (
	"context"
	"fmt"
	"strings"

	"github.com/AceDarkknight/k8s-analyzer-agent/internal/client/shell"
	"github.com/AceDarkknight/k8s-analyzer-agent/internal/logger"
)

// ShellClient Shell 客户端接口
type ShellClient interface {
	ExecuteCommand(ctx context.Context, command string) (*shell.ExecuteResult, error)
}

// Agent Safety Agent，负责安全执行命令
type Agent struct {
	validator *Validator
	client    ShellClient
}

// NewAgent 创建新的 Safety Agent
func NewAgent(client ShellClient, configPath string, llmAuditor LLMAuditor) (*Agent, error) {
	// 创建验证器（传入 LLM 审计器）
	validator, err := NewValidator(configPath, llmAuditor)
	if err != nil {
		return nil, fmt.Errorf("failed to create validator: %w", err)
	}

	return &Agent{
		validator: validator,
		client:    client,
	}, nil
}

// NewAgentWithValidator 使用自定义验证器创建 Safety Agent
func NewAgentWithValidator(client ShellClient, validator *Validator) *Agent {
	return &Agent{
		validator: validator,
		client:    client,
	}
}

// ExecuteSafeCommand 安全执行命令
// 如果命令通过安全验证，则执行并返回输出
// 如果命令不安全，返回 UnsafeCommandError
func (a *Agent) ExecuteSafeCommand(ctx context.Context, command string) (string, error) {
	//1. 验证命令安全性
	if err := a.validator.ValidateCommand(command); err != nil {
		logger.Warn("[Safety] Command rejected", logger.String("command", command), logger.Err(err))
		return "", err
	}

	logger.Info("[Safety] Command approved", logger.String("command", command))

	//2. 执行命令
	result, err := a.client.ExecuteCommand(ctx, command)
	if err != nil {
		logger.Error("[Safety] Command execution failed", logger.String("command", command), logger.Err(err))
		return "", fmt.Errorf("failed to execute command: %w", err)
	}

	//3. 格式化输出
	output := a.formatOutput(result)
	logger.Info("[Safety] Command executed successfully", logger.String("command", command))

	return output, nil
}

// ExecuteSafeCommandWithAudit 安全执行命令（带 LLM 审计）
// 执行完整的审计流程：规则验证 -> LLM 审计 -> MCP 客户端执行
// 返回格式化的输出，包含审计结果和执行结果
func (a *Agent) ExecuteSafeCommandWithAudit(ctx context.Context, command string, contextInfo map[string]interface{}) (string, error) {
	// 1. 执行规则验证和 LLM 审计
	auditResult, err := a.validator.ValidateCommandWithAudit(ctx, command, contextInfo)
	if err != nil {
		// 审计拒绝（包括规则验证失败和 LLM 审计拒绝）
		if IsUnsafeCommand(err) {
			logger.Warn("[Safety] Command rejected by audit", logger.String("command", command), logger.Err(err))
			return "", err
		}
		// 其他错误（如 LLM 审计失败）
		logger.Error("[Safety] Audit error", logger.String("command", command), logger.Err(err))
		return "", err
	}

	// 2. 审计通过，记录审计结果
	if auditResult != nil {
		switch auditResult.SafetyLevel {
		case SafetyLevelSafe:
			logger.Info("[Safety] Audit passed: Safe", logger.String("command", command))
		case SafetyLevelWarning:
			logger.Info("[Safety] Audit passed: Warning", logger.String("command", command), logger.String("reason", auditResult.Reason))
		case SafetyLevelDangerous:
			logger.Warn("[Safety] Audit passed: Dangerous but allowed", logger.String("command", command), logger.String("reason", auditResult.Reason))
		}
	}

	// 3. 调用 MCP 客户端执行命令
	logger.Info("[Safety] Executing command via MCP client", logger.String("command", command))
	result, err := a.client.ExecuteCommand(ctx, command)
	if err != nil {
		logger.Error("[Safety] MCP execution failed", logger.String("command", command), logger.Err(err))
		return "", fmt.Errorf("MCP execution failed: %w", err)
	}

	// 4. 格式化输出（包含审计信息和执行结果）
	output := a.formatOutputWithAudit(result, auditResult)
	logger.Info("[Safety] Command executed successfully via MCP", logger.String("command", command))

	return output, nil
}

// ExecuteSafeCommandWithTimeout 安全执行命令（带超时）
func (a *Agent) ExecuteSafeCommandWithTimeout(ctx context.Context, command string, timeout int) (string, error) {
	//1. 验证命令安全性
	if err := a.validator.ValidateCommand(command); err != nil {
		logger.Warn("[Safety] Command rejected", logger.String("command", command), logger.Err(err))
		return "", err
	}

	logger.Info("[Safety] Command approved", logger.String("command", command))

	//2. 执行命令（带超时）
	var result *shell.ExecuteResult
	var err error

	if timeout > 0 {
		// 如果客户端支持超时，使用 ExecuteCommandWithTimeout
		if client, ok := a.client.(interface {
			ExecuteCommandWithTimeout(ctx context.Context, command string, timeout int) (*shell.ExecuteResult, error)
		}); ok {
			result, err = client.ExecuteCommandWithTimeout(ctx, command, timeout)
		} else {
			// 回退到普通执行
			result, err = a.client.ExecuteCommand(ctx, command)
		}
	} else {
		result, err = a.client.ExecuteCommand(ctx, command)
	}

	if err != nil {
		logger.Error("[Safety] Command execution failed", logger.String("command", command), logger.Err(err))
		return "", fmt.Errorf("failed to execute command: %w", err)
	}

	//3. 格式化输出
	output := a.formatOutput(result)
	logger.Info("[Safety] Command executed successfully", logger.String("command", command))

	return output, nil
}

// ValidateCommand 仅验证命令安全性，不执行
func (a *Agent) ValidateCommand(command string) error {
	return a.validator.ValidateCommand(command)
}

// formatOutput 格式化命令执行结果
func (a *Agent) formatOutput(result *shell.ExecuteResult) string {
	if result == nil {
		return ""
	}

	// 使用摘要作为输出
	output := result.FormatSummary()

	// 如果有成功的输出，追加到结果中
	successOutputs := result.GetSuccessOutput()
	if len(successOutputs) > 0 {
		output += "\n\nOutput:\n"
		for _, out := range successOutputs {
			output += out + "\n"
		}
	}

	// 如果有失败的输出，追加到结果中
	failureOutputs := result.GetFailureOutput()
	if len(failureOutputs) > 0 {
		output += "\n\nErrors:\n"
		for _, out := range failureOutputs {
			output += out + "\n"
		}
	}

	return output
}

// formatOutputWithAudit 格式化命令执行结果（包含审计信息）
func (a *Agent) formatOutputWithAudit(result *shell.ExecuteResult, auditResult *AuditResult) string {
	if result == nil {
		return ""
	}

	var output strings.Builder

	// 1. 添加审计结果
	if auditResult != nil {
		output.WriteString("## 审计结果\n\n")
		switch auditResult.SafetyLevel {
		case SafetyLevelSafe:
			output.WriteString("✅ 安全级别: 安全\n")
		case SafetyLevelWarning:
			output.WriteString("⚠️ 安全级别: 警告\n")
		case SafetyLevelDangerous:
			output.WriteString("🔴 安全级别: 危险（但已允许）\n")
		}

		if auditResult.Reason != "" {
			output.WriteString(fmt.Sprintf("审计理由: %s\n", auditResult.Reason))
		}

		if auditResult.Advice != "" {
			output.WriteString(fmt.Sprintf("建议: %s\n", auditResult.Advice))
		}

		output.WriteString("\n")
	}

	// 2. 添加执行结果摘要
	output.WriteString("## 执行结果\n\n")
	output.WriteString(result.FormatSummary())
	output.WriteString("\n")

	// 3. 如果有成功的输出，追加到结果中
	successOutputs := result.GetSuccessOutput()
	if len(successOutputs) > 0 {
		output.WriteString("\n### 输出\n\n")
		for _, out := range successOutputs {
			output.WriteString(out + "\n")
		}
	}

	// 4. 如果有失败的输出，追加到结果中
	failureOutputs := result.GetFailureOutput()
	if len(failureOutputs) > 0 {
		output.WriteString("\n### 错误\n\n")
		for _, out := range failureOutputs {
			output.WriteString(out + "\n")
		}
	}

	return output.String()
}

// GetValidator 获取验证器
func (a *Agent) GetValidator() *Validator {
	return a.validator
}

// GetClient 获取 Shell 客户端
func (a *Agent) GetClient() ShellClient {
	return a.client
}

// GetConfig 获取安全配置
func (a *Agent) GetConfig() *SecurityConfig {
	return a.validator.GetConfig()
}
