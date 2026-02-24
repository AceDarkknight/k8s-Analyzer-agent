// Package config 提供 LLM 配置结构
// 支持为不同的 Agent 配置不同的 LLM 模型
package config

// LLMConfig 定义单个 LLM 的配置
type LLMConfig struct {
	// Provider LLM 提供商，例如 "openai", "azure", "deepseek", "rule-based"
	Provider string `json:"provider"`

	// BaseURL API 基础地址（对于真实 LLM 服务）
	BaseURL string `json:"base_url"`

	// APIKey API Key（对于真实 LLM 服务，建议从环境变量读取）
	APIKey string `json:"api_key"`

	// Model 模型名称
	Model string `json:"model"`

	// Temperature 温度参数（控制输出的随机性）
	Temperature float64 `json:"temperature"`

	// MaxTokens 最大 Token 数
	MaxTokens int `json:"max_tokens"`

	// Redis Redis 配置（可选）
	Redis *RedisConfig `json:"redis"`
}

// AgentLLMConfig 定义所有 Agent 的 LLM 配置
type AgentLLMConfig struct {
	// Analysis Analysis Agent 的 LLM 配置
	Analysis LLMConfig `json:"analysis"`

	// Safety Safety Agent 的 LLM 配置
	Safety LLMConfig `json:"safety"`
}

// DefaultAgentLLMConfig 返回默认的 LLM 配置
func DefaultAgentLLMConfig() *AgentLLMConfig {
	return &AgentLLMConfig{
		Analysis: LLMConfig{
			Provider:    "rule-based",
			BaseURL:     "",
			APIKey:      "",
			Model:       "gpt-4",
			Temperature: 0.7,
			MaxTokens:   2000,
		},
		Safety: LLMConfig{
			Provider:    "rule-based",
			BaseURL:     "",
			APIKey:      "",
			Model:       "gpt-3.5-turbo",
			Temperature: 0.0,
			MaxTokens:   500,
		},
	}
}
