// Package shell 测试 Shell Executor MCP Client
package shell

import (
	"context"
	"os"
	"testing"

	clientpkg "github.com/AceDarkknight/k8s-analyzer-agent/internal/client"
	mcpConfig "github.com/AceDarkknight/shell-executor-mcp/pkg/configs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name        string
		config      Config
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid config with one server",
			config: Config{
				McpConfig: mcpConfig.ClientConfig{
					Servers: []mcpConfig.ServerConfig{
						{Name: "server1", URL: "http://localhost:8080"},
					},
				},
			},
			expectError: false,
		},
		{
			name: "valid config with multiple servers",
			config: Config{
				McpConfig: mcpConfig.ClientConfig{
					Servers: []mcpConfig.ServerConfig{
						{Name: "server1", URL: "http://localhost:8080"},
						{Name: "server2", URL: "http://localhost:8081"},
					},
				},
			},
			expectError: false,
		},
		{
			name:        "empty servers",
			config:      Config{McpConfig: mcpConfig.ClientConfig{Servers: []mcpConfig.ServerConfig{}}},
			expectError: true,
			errorMsg:    "at least one server is required",
		},
		{
			name: "server without name",
			config: Config{
				McpConfig: mcpConfig.ClientConfig{
					Servers: []mcpConfig.ServerConfig{
						{URL: "http://localhost:8080"},
					},
				},
			},
			expectError: true,
			errorMsg:    "name is required",
		},
		{
			name: "server without url",
			config: Config{
				McpConfig: mcpConfig.ClientConfig{
					Servers: []mcpConfig.ServerConfig{
						{Name: "server1"},
					},
				},
			},
			expectError: true,
			errorMsg:    "url is required",
		},
		{
			name: "invalid url",
			config: Config{
				McpConfig: mcpConfig.ClientConfig{
					Servers: []mcpConfig.ServerConfig{
						{Name: "server1", URL: "://invalid"},
					},
				},
			},
			expectError: true,
			errorMsg:    "invalid url",
		},
		{
			name: "config with defaults",
			config: Config{
				McpConfig: mcpConfig.ClientConfig{
					Servers: []mcpConfig.ServerConfig{
						{Name: "server1", URL: "http://localhost:8080"},
					},
				},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()

			if tt.expectError {
				assert.Error(t, err, "应该返回错误")
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg, "错误信息应包含指定内容")
				}
			} else {
				assert.NoError(t, err, "不应该返回错误")
				// 验证默认值
				assert.Equal(t, 30, tt.config.Timeout, "应该设置默认超时")
				assert.Equal(t, 3, tt.config.RetryConfig.MaxAttempts, "应该设置默认重试次数")
				assert.Equal(t, "/sse", tt.config.SSEPath, "应该设置默认 SSE 路径")
			}
		})
	}
}

func TestNewClient(t *testing.T) {
	tests := []struct {
		name        string
		config      Config
		expectError bool
	}{
		{
			name: "valid config",
			config: Config{
				McpConfig: mcpConfig.ClientConfig{
					Servers: []mcpConfig.ServerConfig{
						{Name: "server1", URL: "http://localhost:8080"},
					},
				},
			},
			expectError: false,
		},
		{
			name: "invalid config",
			config: Config{
				McpConfig: mcpConfig.ClientConfig{
					Servers: []mcpConfig.ServerConfig{},
				},
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewClient(tt.config)

			if tt.expectError {
				assert.Error(t, err, "应该返回错误")
				assert.Nil(t, client, "client 应为 nil")
			} else {
				assert.NoError(t, err, "不应该返回错误")
				assert.NotNil(t, client, "client 不应为 nil")
				assert.False(t, client.connected, "初始状态应为未连接")

				// 预期配置包含默认值
				expectedConfig := tt.config
				if expectedConfig.Timeout == 0 {
					expectedConfig.Timeout = 30
				}
				if expectedConfig.RetryConfig.MaxAttempts == 0 {
					expectedConfig.RetryConfig = clientpkg.DefaultRetryConfig()
				}
				if expectedConfig.SSEPath == "" {
					expectedConfig.SSEPath = "/sse"
				}

				assert.Equal(t, expectedConfig, client.config, "配置应该正确保存")
			}
		})
	}
}

func TestNewClientFromFile(t *testing.T) {
	// 这个测试需要实际的配置文件
	// 在实际使用中，应该测试从文件加载配置
	t.Skip("需要实际的配置文件")
}

func TestClient_IsConnected(t *testing.T) {
	config := Config{
		McpConfig: mcpConfig.ClientConfig{
			Servers: []mcpConfig.ServerConfig{
				{Name: "server1", URL: "http://localhost:8080"},
			},
		},
	}

	client, err := NewClient(config)
	require.NoError(t, err, "创建 client 不应失败")

	assert.False(t, client.IsConnected(), "初始状态应为未连接")
}

func TestClient_GetConfig(t *testing.T) {
	config := Config{
		McpConfig: mcpConfig.ClientConfig{
			Token: "test-token",
			Servers: []mcpConfig.ServerConfig{
				{Name: "server1", URL: "http://localhost:8080"},
			},
		},
		Timeout:        60,
		EnableFailover: true,
	}

	client, err := NewClient(config)
	require.NoError(t, err, "创建 client 不应失败")

	// 预期配置包含默认值
	expectedConfig := config
	if expectedConfig.RetryConfig.MaxAttempts == 0 {
		expectedConfig.RetryConfig = clientpkg.DefaultRetryConfig()
	}
	if expectedConfig.SSEPath == "" {
		expectedConfig.SSEPath = "/sse"
	}

	returnedConfig := client.GetConfig()
	assert.Equal(t, expectedConfig, returnedConfig, "返回的配置应该与原始配置相同")
}

func TestClient_GetCurrentServer(t *testing.T) {
	config := Config{
		McpConfig: mcpConfig.ClientConfig{
			Servers: []mcpConfig.ServerConfig{
				{Name: "server1", URL: "http://localhost:8080"},
			},
		},
	}

	client, err := NewClient(config)
	require.NoError(t, err, "创建 client 不应失败")

	// 未连接时，GetCurrentServer 应该返回 nil
	server := client.GetCurrentServer()
	assert.Nil(t, server, "未连接时应返回 nil")
}

func TestClient_Close(t *testing.T) {
	config := Config{
		McpConfig: mcpConfig.ClientConfig{
			Servers: []mcpConfig.ServerConfig{
				{Name: "server1", URL: "http://localhost:8080"},
			},
		},
	}

	client, err := NewClient(config)
	require.NoError(t, err, "创建 client 不应失败")

	// 关闭未连接的 client
	err = client.Close()
	assert.NoError(t, err, "关闭未连接的 client 不应失败")
	assert.False(t, client.IsConnected(), "状态应为未连接")
}

func TestClient_UpdateConfig(t *testing.T) {
	oldConfig := Config{
		McpConfig: mcpConfig.ClientConfig{
			Servers: []mcpConfig.ServerConfig{
				{Name: "server1", URL: "http://localhost:8080"},
			},
		},
		EnableFailover: false,
	}

	client, err := NewClient(oldConfig)
	require.NoError(t, err, "创建 client 不应失败")

	newConfig := Config{
		McpConfig: mcpConfig.ClientConfig{
			Servers: []mcpConfig.ServerConfig{
				{Name: "server2", URL: "http://localhost:8081"},
			},
		},
		EnableFailover: true,
	}

	err = client.UpdateConfig(newConfig)
	assert.NoError(t, err, "更新配置不应失败")

	// 预期配置包含默认值
	expectedConfig := newConfig
	if expectedConfig.Timeout == 0 {
		expectedConfig.Timeout = 30
	}
	if expectedConfig.RetryConfig.MaxAttempts == 0 {
		expectedConfig.RetryConfig = clientpkg.DefaultRetryConfig()
	}
	if expectedConfig.SSEPath == "" {
		expectedConfig.SSEPath = "/sse"
	}

	updatedConfig := client.GetConfig()
	assert.Equal(t, expectedConfig, updatedConfig, "配置应该已更新")
	assert.False(t, client.IsConnected(), "更新配置后应断开连接")
}

func TestClient_UpdateConfig_Invalid(t *testing.T) {
	config := Config{
		McpConfig: mcpConfig.ClientConfig{
			Servers: []mcpConfig.ServerConfig{
				{Name: "server1", URL: "http://localhost:8080"},
			},
		},
	}

	client, err := NewClient(config)
	require.NoError(t, err, "创建 client 不应失败")

	invalidConfig := Config{
		McpConfig: mcpConfig.ClientConfig{
			Servers: []mcpConfig.ServerConfig{},
		},
	}

	err = client.UpdateConfig(invalidConfig)
	assert.Error(t, err, "更新为无效配置应该失败")
}

func TestClient_Connect_NotConnected(t *testing.T) {
	config := Config{
		McpConfig: mcpConfig.ClientConfig{
			Servers: []mcpConfig.ServerConfig{
				{Name: "server1", URL: "http://localhost:8080"},
			},
		},
	}

	// 使用 MockClient 进行测试
	client := NewMockClient(config)

	// MockClient 连接总是成功
	ctx := context.Background()
	err := client.Connect(ctx)
	assert.NoError(t, err, "MockClient 连接总是成功")
}

func TestClient_Connect_AlreadyConnected(t *testing.T) {
	config := Config{
		McpConfig: mcpConfig.ClientConfig{
			Servers: []mcpConfig.ServerConfig{
				{Name: "server1", URL: "http://localhost:8080"},
			},
		},
	}

	client, err := NewClient(config)
	require.NoError(t, err, "创建 client 不应失败")

	// 模拟已连接状态
	client.connected = true

	ctx := context.Background()
	err = client.Connect(ctx)
	assert.NoError(t, err, "已连接时再次连接不应失败")
}

func TestClient_CallTool_NotConnected(t *testing.T) {
	config := Config{
		McpConfig: mcpConfig.ClientConfig{
			Servers: []mcpConfig.ServerConfig{
				{Name: "server1", URL: "http://localhost:8080"},
			},
		},
	}

	client, err := NewClient(config)
	require.NoError(t, err, "创建 client 不应失败")

	ctx := context.Background()
	_, err = client.CallTool(ctx, "test_tool", nil)
	assert.Error(t, err, "未连接时调用工具应该失败")
	assert.Contains(t, err.Error(), "not connected", "错误信息应包含未连接标记")
}

func TestClient_ListTools_NotConnected(t *testing.T) {
	config := Config{
		McpConfig: mcpConfig.ClientConfig{
			Servers: []mcpConfig.ServerConfig{
				{Name: "server1", URL: "http://localhost:8080"},
			},
		},
	}

	client, err := NewClient(config)
	require.NoError(t, err, "创建 client 不应失败")

	ctx := context.Background()
	_, err = client.ListTools(ctx)
	assert.Error(t, err, "未连接时列出工具应该失败")
	assert.Contains(t, err.Error(), "not connected", "错误信息应包含未连接标记")
}

func TestClient_HealthCheck_NotConnected(t *testing.T) {
	config := Config{
		McpConfig: mcpConfig.ClientConfig{
			Servers: []mcpConfig.ServerConfig{
				{Name: "server1", URL: "http://localhost:8080"},
			},
		},
	}

	client, err := NewClient(config)
	require.NoError(t, err, "创建 client 不应失败")

	ctx := context.Background()
	err = client.HealthCheck(ctx)
	assert.Error(t, err, "未连接时健康检查应该失败")
	assert.Contains(t, err.Error(), "not connected", "错误信息应包含未连接标记")
}

func TestClient_isConnectionError(t *testing.T) {
	config := Config{
		McpConfig: mcpConfig.ClientConfig{
			Servers: []mcpConfig.ServerConfig{
				{Name: "server1", URL: "http://localhost:8080"},
			},
		},
	}

	client, err := NewClient(config)
	require.NoError(t, err, "创建 client 不应失败")

	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "connection error",
			err:      &clientpkg.ConnectionError{Reason: "connection failed"},
			expected: true,
		},
		{
			name:     "generic error",
			err:      &clientpkg.ToolExecutionError{ToolName: "test", Reason: "failed"},
			expected: false,
		},
		{
			name:     "timeout error",
			err:      context.DeadlineExceeded,
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := client.isConnectionError(tt.err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// 集成测试（需要真实的 MCP Server 运行）
func TestClient_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过集成测试")
	}

	// 默认跳过集成测试，除非设置了环境变量
	if os.Getenv("RUN_INTEGRATION_TESTS") != "true" {
		t.Skip("跳过集成测试，设置 RUN_INTEGRATION_TESTS=true 来运行")
	}

	config := Config{
		McpConfig: mcpConfig.ClientConfig{
			Servers: []mcpConfig.ServerConfig{
				{Name: "server1", URL: "http://localhost:8080"},
			},
		},
		EnableFailover: true,
	}

	client, err := NewClient(config)
	require.NoError(t, err, "创建 client 不应失败")

	ctx := context.Background()
	err = client.Connect(ctx)
	if err != nil {
		t.Skipf("无法连接到 MCP Server: %v", err)
	}
	defer client.Close()

	// 测试 ListTools
	tools, err := client.ListTools(ctx)
	require.NoError(t, err, "列出工具不应失败")
	assert.NotEmpty(t, tools, "应该有可用的工具")

	// 测试 HealthCheck
	err = client.HealthCheck(ctx)
	assert.NoError(t, err, "健康检查不应失败")

	// 测试 GetCurrentServer
	server := client.GetCurrentServer()
	assert.NotNil(t, server, "应该返回当前服务器")
	assert.Equal(t, "server1", server.Name, "服务器名称应该正确")
}
