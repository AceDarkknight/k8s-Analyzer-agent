// Package safety 提供命令安全执行 Agent
package safety

import (
	"context"
	"fmt"
	"log"

	"github.com/AceDarkknight/k8s-analyzer-agent/internal/client/shell"
)

// ShellClient Shell 客户端接口
type ShellClient interface {
	ExecuteCommand(ctx context.Context, command string) (*shell.ExecuteResult, error)
}

// Agent Safety Agent，负责安全执行命令
type Agent struct {
	validator *Validator
	client    ShellClient
	logger    *log.Logger
}

// NewAgent 创建新的 Safety Agent
func NewAgent(client ShellClient, configPath string) (*Agent, error) {
	// 创建验证器
	validator, err := NewValidator(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create validator: %w", err)
	}

	return &Agent{
		validator: validator,
		client:    client,
		logger:    log.Default(),
	}, nil
}

// NewAgentWithValidator 使用自定义验证器创建 Safety Agent
func NewAgentWithValidator(client ShellClient, validator *Validator) *Agent {
	return &Agent{
		validator: validator,
		client:    client,
		logger:    log.Default(),
	}
}

// NewAgentWithLogger 创建带自定义日志的 Safety Agent
func NewAgentWithLogger(client ShellClient, configPath string, logger *log.Logger) (*Agent, error) {
	// 创建验证器
	validator, err := NewValidator(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create validator: %w", err)
	}

	return &Agent{
		validator: validator,
		client:    client,
		logger:    logger,
	}, nil
}

// ExecuteSafeCommand 安全执行命令
// 如果命令通过安全验证，则执行并返回输出
// 如果命令不安全，返回 UnsafeCommandError
func (a *Agent) ExecuteSafeCommand(ctx context.Context, command string) (string, error) {
	// 1. 验证命令安全性
	if err := a.validator.ValidateCommand(command); err != nil {
		a.logger.Printf("[Safety] Command rejected: %s - %v", command, err)
		return "", err
	}

	a.logger.Printf("[Safety] Command approved: %s", command)

	// 2. 执行命令
	result, err := a.client.ExecuteCommand(ctx, command)
	if err != nil {
		a.logger.Printf("[Safety] Command execution failed: %s - %v", command, err)
		return "", fmt.Errorf("failed to execute command: %w", err)
	}

	// 3. 格式化输出
	output := a.formatOutput(result)
	a.logger.Printf("[Safety] Command executed successfully: %s", command)

	return output, nil
}

// ExecuteSafeCommandWithTimeout 安全执行命令（带超时）
func (a *Agent) ExecuteSafeCommandWithTimeout(ctx context.Context, command string, timeout int) (string, error) {
	// 1. 验证命令安全性
	if err := a.validator.ValidateCommand(command); err != nil {
		a.logger.Printf("[Safety] Command rejected: %s - %v", command, err)
		return "", err
	}

	a.logger.Printf("[Safety] Command approved: %s", command)

	// 2. 执行命令（带超时）
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
		a.logger.Printf("[Safety] Command execution failed: %s - %v", command, err)
		return "", fmt.Errorf("failed to execute command: %w", err)
	}

	// 3. 格式化输出
	output := a.formatOutput(result)
	a.logger.Printf("[Safety] Command executed successfully: %s", command)

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
