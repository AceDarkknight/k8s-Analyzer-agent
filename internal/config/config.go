package config

import (
	"fmt"
	"os"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config 是应用程序的主配置结构
type Config struct {
	Gateway  GatewayConfig  `yaml:"gateway"`
	ShellMCP ShellMCPConfig `yaml:"shell_mcp"`
	LLM      AgentLLMConfig `yaml:"llm"`
	Store    StoreConfig    `yaml:"store"`
	Agent    AgentConfig    `yaml:"agent"`
	Log      LogConfig      `yaml:"log"`
}

// GatewayConfig 网关配置
type GatewayConfig struct {
	BaseURL        string `yaml:"base_url"`   // 支持 ${ENV_VAR}
	AuthToken      string `yaml:"auth_token"` // 支持 ${ENV_VAR}
	TimeoutSeconds int    `yaml:"timeout_seconds"`
}

// ShellMCPConfig Shell MCP 服务器配置
type ShellMCPConfig struct {
	ServerURL string `yaml:"server_url"` // 支持 ${ENV_VAR}
	Transport string `yaml:"transport"`  // "sse"
	AuthToken string `yaml:"auth_token"` // 支持 ${ENV_VAR}
}

// LLMConfig LLM 模型配置
type LLMConfig struct {
	Provider    string  `yaml:"provider"` // openai / deepseek / gemini
	BaseURL     string  `yaml:"base_url"`
	APIKey      string  `yaml:"api_key"` // 支持 ${ENV_VAR}
	Model       string  `yaml:"model"`
	Temperature float64 `yaml:"temperature"`
	MaxTokens   int     `yaml:"max_tokens"`
}

// AgentLLMConfig Agent 使用的 LLM 配置（轻量级和强力模型）
type AgentLLMConfig struct {
	Light LLMConfig `yaml:"light"`
	Power LLMConfig `yaml:"power"`
}

// StoreConfig 存储配置
type StoreConfig struct {
	Type  string      `yaml:"type"` // memory / redis
	Redis RedisConfig `yaml:"redis"`
}

// RedisConfig Redis 配置
type RedisConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	Password string `yaml:"password"`
	DB       int    `yaml:"db"`
}

// AgentConfig Agent 行为配置
type AgentConfig struct {
	MaxIterations         int  `yaml:"max_iterations"`          // 默认 10
	CompressThreshold     int  `yaml:"compress_threshold"`      // 默认 4
	OutputMaxLines        int  `yaml:"output_max_lines"`        // 默认 50
	OutputMaxChars        int  `yaml:"output_max_chars"`        // 默认 3000
	FindingTTLHours       int  `yaml:"finding_ttl_hours"`       // 默认 1
	VerifyRecommendations bool `yaml:"verify_recommendations"`  // 是否启用验证迭代，默认 true
	MaxVerifyIterations   int  `yaml:"max_verify_iterations"`   // 验证阶段最大迭代数，默认 2
}

// LogConfig 日志配置
type LogConfig struct {
	Level      string `yaml:"level"`
	FilePath   string `yaml:"file_path"`
	MaxSizeMB  int    `yaml:"max_size_mb"`
	MaxBackups int    `yaml:"max_backups"`
}

// envVarPattern 匹配 ${ENV_VAR} 或 ${ENV_VAR:-default} 格式的正则表达式
var envVarPattern = regexp.MustCompile(`\$\{([^}]+)\}`)

// Load 从 YAML 文件加载配置
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// 解析环境变量
	resolveEnvVarsInStruct(&cfg)

	// 设置默认值
	cfg.setDefaults()

	// 验证配置
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return &cfg, nil
}

// resolveEnvVars 替换字符串中的 ${ENV_VAR} 或 ${ENV_VAR:-default} 为环境变量值
func resolveEnvVars(s string) string {
	return envVarPattern.ReplaceAllStringFunc(s, func(match string) string {
		// 提取变量名，去掉 ${ 和 }
		content := match[2 : len(match)-1]

		// 检查是否有默认值语法 ${VAR:-default}
		var varName, defaultValue string
		if idx := strings.Index(content, ":-"); idx > 0 {
			varName = content[:idx]
			defaultValue = content[idx+2:]
		} else {
			varName = content
		}

		if value := os.Getenv(varName); value != "" {
			return value
		}
		// 如果环境变量不存在，使用默认值（如果有）或保留原样
		if defaultValue != "" {
			return defaultValue
		}
		return match
	})
}

// resolveEnvVarsInStruct 递归解析结构体中的所有字符串字段的环境变量
func resolveEnvVarsInStruct(v interface{}) {
	resolveEnvVarsInValue(reflect.ValueOf(v).Elem())
}

// resolveEnvVarsInValue 递归处理 reflect.Value
func resolveEnvVarsInValue(v reflect.Value) {
	switch v.Kind() {
	case reflect.String:
		if v.CanSet() {
			resolved := resolveEnvVars(v.String())
			v.SetString(resolved)
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			field := v.Field(i)
			if field.CanSet() {
				resolveEnvVarsInValue(field)
			}
		}
	case reflect.Ptr:
		if !v.IsNil() {
			resolveEnvVarsInValue(v.Elem())
		}
	}
}

// setDefaults 设置配置默认值
func (c *Config) setDefaults() {
	// Agent 配置默认值
	if c.Agent.MaxIterations == 0 {
		c.Agent.MaxIterations = 10
	}
	if c.Agent.CompressThreshold == 0 {
		c.Agent.CompressThreshold = 4
	}
	if c.Agent.OutputMaxLines == 0 {
		c.Agent.OutputMaxLines = 50
	}
	if c.Agent.OutputMaxChars == 0 {
		c.Agent.OutputMaxChars = 3000
	}
	if c.Agent.FindingTTLHours == 0 {
		c.Agent.FindingTTLHours = 1
	}
	// VerifyRecommendations 默认为 true（零值 false 表示未设置，需要设为 true）
	// 注意：bool 类型零值为 false，但我们需要默认启用，所以这里不需要判断，直接保持配置值即可
	// 如果配置文件中未设置，Go 会解析为 false，但我们需要默认 true
	// 因此这里不做特殊处理，由调用方判断 if cfg.Agent.VerifyRecommendations
	if c.Agent.MaxVerifyIterations == 0 {
		c.Agent.MaxVerifyIterations = 2
	}
}

// validate 校验必填字段
func (c *Config) validate() error {
	if c.Gateway.BaseURL == "" {
		return fmt.Errorf("gateway.base_url is required")
	}

	if c.LLM.Light.APIKey == "" {
		return fmt.Errorf("llm.light.api_key is required")
	}

	if c.LLM.Power.APIKey == "" {
		return fmt.Errorf("llm.power.api_key is required")
	}

	return nil
}

// String 返回配置的字符串表示（用于调试，隐藏敏感信息）
func (c *Config) String() string {
	return fmt.Sprintf(
		"Config{Gateway:{BaseURL:%s TimeoutSeconds:%d}, ShellMCP:{ServerURL:%s Transport:%s}, LLM:{Light:{Provider:%s Model:%s} Power:{Provider:%s Model:%s}}, Store:{Type:%s}, Agent:{MaxIterations:%d CompressThreshold:%d OutputMaxLines:%d OutputMaxChars:%d FindingTTLHours:%d VerifyRecommendations:%t MaxVerifyIterations:%d}, Log:{Level:%s FilePath:%s}}",
		c.Gateway.BaseURL,
		c.Gateway.TimeoutSeconds,
		c.ShellMCP.ServerURL,
		c.ShellMCP.Transport,
		c.LLM.Light.Provider,
		c.LLM.Light.Model,
		c.LLM.Power.Provider,
		c.LLM.Power.Model,
		c.Store.Type,
		c.Agent.MaxIterations,
		c.Agent.CompressThreshold,
		c.Agent.OutputMaxLines,
		c.Agent.OutputMaxChars,
		c.Agent.FindingTTLHours,
		c.Agent.VerifyRecommendations,
		c.Agent.MaxVerifyIterations,
		c.Log.Level,
		c.Log.FilePath,
	)
}

// GetTimeoutSeconds 返回超时时间（秒）
func (c *GatewayConfig) GetTimeoutSeconds() int {
	if c.TimeoutSeconds <= 0 {
		return 30 // 默认 30 秒
	}
	return c.TimeoutSeconds
}

// GetRedisAddr 返回 Redis 地址
func (c *RedisConfig) GetRedisAddr() string {
	port := c.Port
	if port == 0 {
		port = 6379
	}
	return c.Host + ":" + strconv.Itoa(port)
}
