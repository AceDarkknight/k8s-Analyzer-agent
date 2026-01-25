// Package safety 提供命令安全验证功能
package safety

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// SecurityConfig 安全配置
type SecurityConfig struct {
	AllowReadOnly       bool     `json:"allow_read_only"`
	CommandWhitelist    []string `json:"command_whitelist"`
	BlacklistedCommands []string `json:"blacklisted_commands"`
	DangerousArgsRegex  []string `json:"dangerous_args_regex"`
}

// Validator 命令安全验证器
type Validator struct {
	config          *SecurityConfig
	compiledRegexes []*regexp.Regexp
}

// NewValidator 创建新的安全验证器
func NewValidator(configPath string) (*Validator, error) {
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
	}, nil
}

// NewValidatorWithConfig 使用配置对象创建验证器
func NewValidatorWithConfig(config *SecurityConfig) (*Validator, error) {
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
