package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// createTempConfigFile 创建临时配置文件
func createTempConfigFile(t *testing.T, content string) string {
	t.Helper()
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	err := os.WriteFile(configPath, []byte(content), 0644)
	require.NoError(t, err)
	return configPath
}

func TestLoad_ValidConfig(t *testing.T) {
	content := `
gateway:
  base_url: "http://localhost:8080"
  auth_token: "test-token"
  timeout_seconds: 60
shell_mcp:
  server_url: "http://localhost:3000"
  transport: "sse"
  auth_token: "mcp-token"
llm:
  light:
    provider: "openai"
    base_url: "https://api.openai.com"
    api_key: "light-api-key"
    model: "gpt-3.5-turbo"
    temperature: 0.7
    max_tokens: 1000
  power:
    provider: "deepseek"
    base_url: "https://api.deepseek.com"
    api_key: "power-api-key"
    model: "deepseek-chat"
    temperature: 0.5
    max_tokens: 2000
store:
  type: "memory"
  redis:
    host: "localhost"
    port: 6379
    password: ""
    db: 0
agent:
  max_iterations: 15
  compress_threshold: 5
  output_max_lines: 100
  output_max_chars: 5000
  finding_ttl_hours: 2
monitor:
  api_port: 18080
  trace_dir: "data/traces"
log:
  level: "info"
  file_path: "/var/log/app.log"
  max_size_mb: 100
  max_backups: 5
`
	configPath := createTempConfigFile(t, content)
	cfg, err := Load(configPath)

	require.NoError(t, err)
	assert.NotNil(t, cfg)

	// 验证 Gateway 配置
	assert.Equal(t, "http://localhost:8080", cfg.Gateway.BaseURL)
	assert.Equal(t, "test-token", cfg.Gateway.AuthToken)
	assert.Equal(t, 60, cfg.Gateway.TimeoutSeconds)

	// 验证 ShellMCP 配置
	assert.Equal(t, "http://localhost:3000", cfg.ShellMCP.ServerURL)
	assert.Equal(t, "sse", cfg.ShellMCP.Transport)
	assert.Equal(t, "mcp-token", cfg.ShellMCP.AuthToken)

	// 验证 LLM Light 配置
	assert.Equal(t, "openai", cfg.LLM.Light.Provider)
	assert.Equal(t, "https://api.openai.com", cfg.LLM.Light.BaseURL)
	assert.Equal(t, "light-api-key", cfg.LLM.Light.APIKey)
	assert.Equal(t, "gpt-3.5-turbo", cfg.LLM.Light.Model)
	assert.Equal(t, 0.7, cfg.LLM.Light.Temperature)
	assert.Equal(t, 1000, cfg.LLM.Light.MaxTokens)

	// 验证 LLM Power 配置
	assert.Equal(t, "deepseek", cfg.LLM.Power.Provider)
	assert.Equal(t, "https://api.deepseek.com", cfg.LLM.Power.BaseURL)
	assert.Equal(t, "power-api-key", cfg.LLM.Power.APIKey)
	assert.Equal(t, "deepseek-chat", cfg.LLM.Power.Model)
	assert.Equal(t, 0.5, cfg.LLM.Power.Temperature)
	assert.Equal(t, 2000, cfg.LLM.Power.MaxTokens)

	// 验证 Store 配置
	assert.Equal(t, "memory", cfg.Store.Type)
	assert.Equal(t, "localhost", cfg.Store.Redis.Host)
	assert.Equal(t, 6379, cfg.Store.Redis.Port)

	// 验证 Agent 配置
	assert.Equal(t, 15, cfg.Agent.MaxIterations)
	assert.Equal(t, 5, cfg.Agent.CompressThreshold)
	assert.Equal(t, 100, cfg.Agent.OutputMaxLines)
	assert.Equal(t, 5000, cfg.Agent.OutputMaxChars)
	assert.Equal(t, 2, cfg.Agent.FindingTTLHours)

	// 验证 Monitor 配置
	assert.Equal(t, 18080, cfg.Monitor.APIPort)
	assert.Equal(t, "data/traces", cfg.Monitor.TraceDir)

	// 验证 Log 配置
	assert.Equal(t, "info", cfg.Log.Level)
	assert.Equal(t, "/var/log/app.log", cfg.Log.FilePath)
	assert.Equal(t, 100, cfg.Log.MaxSizeMB)
	assert.Equal(t, 5, cfg.Log.MaxBackups)
}

func TestLoad_EnvVarSubstitution(t *testing.T) {
	// 设置环境变量
	os.Setenv("TEST_GATEWAY_URL", "http://gateway.example.com")
	os.Setenv("TEST_LIGHT_API_KEY", "env-light-key")
	os.Setenv("TEST_POWER_API_KEY", "env-power-key")
	os.Setenv("TEST_AUTH_TOKEN", "env-auth-token")
	defer func() {
		os.Unsetenv("TEST_GATEWAY_URL")
		os.Unsetenv("TEST_LIGHT_API_KEY")
		os.Unsetenv("TEST_POWER_API_KEY")
		os.Unsetenv("TEST_AUTH_TOKEN")
	}()

	content := `
gateway:
  base_url: "${TEST_GATEWAY_URL}"
  auth_token: "${TEST_AUTH_TOKEN}"
  timeout_seconds: 30
llm:
  light:
    provider: "openai"
    api_key: "${TEST_LIGHT_API_KEY}"
    model: "gpt-3.5-turbo"
  power:
    provider: "deepseek"
    api_key: "${TEST_POWER_API_KEY}"
    model: "deepseek-chat"
`
	configPath := createTempConfigFile(t, content)
	cfg, err := Load(configPath)

	require.NoError(t, err)
	assert.Equal(t, "http://gateway.example.com", cfg.Gateway.BaseURL)
	assert.Equal(t, "env-auth-token", cfg.Gateway.AuthToken)
	assert.Equal(t, "env-light-key", cfg.LLM.Light.APIKey)
	assert.Equal(t, "env-power-key", cfg.LLM.Power.APIKey)
}

func TestLoad_EnvVarNotSet_KeepsOriginal(t *testing.T) {
	// 确保环境变量不存在
	os.Unsetenv("NON_EXISTENT_VAR")

	content := `
gateway:
  base_url: "http://localhost:8080"
  auth_token: "${NON_EXISTENT_VAR}"
  timeout_seconds: 30
llm:
  light:
    provider: "openai"
    api_key: "light-key"
    model: "gpt-3.5-turbo"
  power:
    provider: "deepseek"
    api_key: "power-key"
    model: "deepseek-chat"
`
	configPath := createTempConfigFile(t, content)
	cfg, err := Load(configPath)

	require.NoError(t, err)
	// 环境变量不存在时，保持原样
	assert.Equal(t, "${NON_EXISTENT_VAR}", cfg.Gateway.AuthToken)
}

func TestLoad_MissingRequiredFields(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantErr string
	}{
		{
			name: "missing gateway.base_url",
			content: `
llm:
  light:
    provider: "openai"
    api_key: "light-key"
    model: "gpt-3.5-turbo"
  power:
    provider: "deepseek"
    api_key: "power-key"
    model: "deepseek-chat"
`,
			wantErr: "gateway.base_url is required",
		},
		{
			name: "missing llm.light.api_key",
			content: `
gateway:
  base_url: "http://localhost:8080"
llm:
  light:
    provider: "openai"
    model: "gpt-3.5-turbo"
  power:
    provider: "deepseek"
    api_key: "power-key"
    model: "deepseek-chat"
`,
			wantErr: "llm.light.api_key is required",
		},
		{
			name: "missing llm.power.api_key",
			content: `
gateway:
  base_url: "http://localhost:8080"
llm:
  light:
    provider: "openai"
    api_key: "light-key"
    model: "gpt-3.5-turbo"
  power:
    provider: "deepseek"
    model: "deepseek-chat"
`,
			wantErr: "llm.power.api_key is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configPath := createTempConfigFile(t, tt.content)
			_, err := Load(configPath)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestLoad_DefaultValues(t *testing.T) {
	content := `
gateway:
  base_url: "http://localhost:8080"
llm:
  light:
    provider: "openai"
    api_key: "light-key"
    model: "gpt-3.5-turbo"
  power:
    provider: "deepseek"
    api_key: "power-key"
    model: "deepseek-chat"
`
	configPath := createTempConfigFile(t, content)
	cfg, err := Load(configPath)

	require.NoError(t, err)

	// 验证 Agent 配置的默认值
	assert.Equal(t, 10, cfg.Agent.MaxIterations)
	assert.Equal(t, 4, cfg.Agent.CompressThreshold)
	assert.Equal(t, 50, cfg.Agent.OutputMaxLines)
	assert.Equal(t, 3000, cfg.Agent.OutputMaxChars)
	assert.Equal(t, 1, cfg.Agent.FindingTTLHours)
}

func TestLoad_DefaultValues_Override(t *testing.T) {
	content := `
gateway:
  base_url: "http://localhost:8080"
llm:
  light:
    provider: "openai"
    api_key: "light-key"
    model: "gpt-3.5-turbo"
  power:
    provider: "deepseek"
    api_key: "power-key"
    model: "deepseek-chat"
agent:
  max_iterations: 20
  compress_threshold: 8
`
	configPath := createTempConfigFile(t, content)
	cfg, err := Load(configPath)

	require.NoError(t, err)

	// 验证自定义值覆盖默认值
	assert.Equal(t, 20, cfg.Agent.MaxIterations)
	assert.Equal(t, 8, cfg.Agent.CompressThreshold)
	// 未设置的字段仍使用默认值
	assert.Equal(t, 50, cfg.Agent.OutputMaxLines)
	assert.Equal(t, 3000, cfg.Agent.OutputMaxChars)
}

func TestLoad_FileNotFound(t *testing.T) {
	_, err := Load("/nonexistent/path/config.yaml")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read config file")
}

func TestLoad_InvalidYAML(t *testing.T) {
	content := `
invalid yaml content: [:
`
	configPath := createTempConfigFile(t, content)
	_, err := Load(configPath)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to unmarshal config")
}

func TestResolveEnvVars(t *testing.T) {
	os.Setenv("TEST_VAR", "test_value")
	os.Setenv("ANOTHER_VAR", "another_value")
	defer func() {
		os.Unsetenv("TEST_VAR")
		os.Unsetenv("ANOTHER_VAR")
	}()

	tests := []struct {
		input    string
		expected string
	}{
		{
			input:    "${TEST_VAR}",
			expected: "test_value",
		},
		{
			input:    "prefix_${TEST_VAR}_suffix",
			expected: "prefix_test_value_suffix",
		},
		{
			input:    "${TEST_VAR}_${ANOTHER_VAR}",
			expected: "test_value_another_value",
		},
		{
			input:    "no env var",
			expected: "no env var",
		},
		{
			input:    "${NON_EXISTENT}",
			expected: "${NON_EXISTENT}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := resolveEnvVars(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGatewayConfig_GetTimeoutSeconds(t *testing.T) {
	tests := []struct {
		name     string
		config   GatewayConfig
		expected int
	}{
		{
			name:     "positive timeout",
			config:   GatewayConfig{TimeoutSeconds: 60},
			expected: 60,
		},
		{
			name:     "zero timeout returns default",
			config:   GatewayConfig{TimeoutSeconds: 0},
			expected: 30,
		},
		{
			name:     "negative timeout returns default",
			config:   GatewayConfig{TimeoutSeconds: -1},
			expected: 30,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.config.GetTimeoutSeconds()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestRedisConfig_GetRedisAddr(t *testing.T) {
	tests := []struct {
		name     string
		config   RedisConfig
		expected string
	}{
		{
			name:     "with custom port",
			config:   RedisConfig{Host: "localhost", Port: 6380},
			expected: "localhost:6380",
		},
		{
			name:     "with default port",
			config:   RedisConfig{Host: "localhost", Port: 0},
			expected: "localhost:6379",
		},
		{
			name:     "with empty host",
			config:   RedisConfig{Host: "", Port: 6379},
			expected: ":6379",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.config.GetRedisAddr()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConfig_String(t *testing.T) {
	cfg := &Config{
		Gateway: GatewayConfig{
			BaseURL:        "http://localhost:8080",
			TimeoutSeconds: 60,
		},
		LLM: AgentLLMConfig{
			Light: LLMConfig{
				Provider: "openai",
				Model:    "gpt-3.5-turbo",
			},
			Power: LLMConfig{
				Provider: "deepseek",
				Model:    "deepseek-chat",
			},
		},
		Store: StoreConfig{Type: "memory"},
		Agent: AgentConfig{
			MaxIterations:     10,
			CompressThreshold: 4,
			OutputMaxLines:    50,
			OutputMaxChars:    3000,
			FindingTTLHours:   1,
		},
		Log: LogConfig{
			Level:    "info",
			FilePath: "/var/log/app.log",
		},
	}

	str := cfg.String()
	assert.Contains(t, str, "Gateway:")
	assert.Contains(t, str, "LLM:")
	assert.Contains(t, str, "Store:")
	assert.Contains(t, str, "Agent:")
	assert.Contains(t, str, "Log:")
	// 确保敏感信息（API Key）不在 String() 输出中
	assert.NotContains(t, str, "api_key")
}
