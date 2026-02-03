// Package shell 提供 Mock Shell Client，用于测试和演示
package shell

import (
	"context"
	"fmt"
	"time"

	clientpkg "github.com/AceDarkknight/k8s-analyzer-agent/internal/client"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// MockClient Mock Shell Client 实现
// 用于测试和演示，不需要真实的 MCP Server
type MockClient struct {
	config    Config
	connected bool
}

// NewMockClient 创建新的 Mock Shell Client
func NewMockClient(config Config) *MockClient {
	return &MockClient{
		config:    config,
		connected: false,
	}
}

// NewMockClientFromFile 从配置文件创建 Mock Shell Client
func NewMockClientFromFile(configPath string) (*MockClient, error) {
	config := Config{
		Servers: []ServerConfig{
			{
				Name: "default",
				URL:  "http://localhost:8080",
			},
		},
		Timeout: 30 * time.Second,
		RetryConfig: clientpkg.RetryConfig{
			MaxAttempts:  3,
			InitialDelay: 1 * time.Second,
			MaxDelay:     10 * time.Second,
		},
		SSEPath: "/sse",
	}

	return NewMockClient(config), nil
}

// Connect 建立与 Shell Executor MCP Server 的连接（模拟）
func (c *MockClient) Connect(ctx context.Context) error {
	// 模拟连接延迟
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(10 * time.Millisecond):
		c.connected = true
		return nil
	}
}

// Close 终止与 Shell Executor MCP Server 的连接（模拟）
func (c *MockClient) Close() error {
	c.connected = false
	return nil
}

// CallTool 执行 Shell Executor MCP Server 上的特定工具（模拟）
func (c *MockClient) CallTool(ctx context.Context, name string, args map[string]interface{}) (*mcp.CallToolResult, error) {
	if !c.connected {
		return nil, &ConnectionError{Reason: "client is not connected"}
	}

	// 根据工具名称返回模拟数据
	var content string
	switch name {
	case "execute_command":
		cmd, _ := args["command"].(string)
		content = fmt.Sprintf("Mock output for command: %s", cmd)
	case "list_files":
		path, _ := args["path"].(string)
		content = fmt.Sprintf("Mock file list for path: %s", path)
	default:
		content = fmt.Sprintf("Mock response for tool: %s", name)
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{
				Text: content,
			},
		},
	}, nil
}

// ListTools 获取 Shell Executor MCP Server 上可用的工具列表（模拟）
func (c *MockClient) ListTools(ctx context.Context) ([]*mcp.Tool, error) {
	if !c.connected {
		return nil, &ConnectionError{Reason: "client is not connected"}
	}

	return []*mcp.Tool{
		{
			Name:        "execute_command",
			Description: "Execute a shell command",
		},
		{
			Name:        "list_files",
			Description: "List files in a directory",
		},
	}, nil
}

// IsConnected 检查客户端是否已连接
func (c *MockClient) IsConnected() bool {
	return c.connected
}

// HealthCheck 执行健康检查（模拟）
func (c *MockClient) HealthCheck(ctx context.Context) error {
	if !c.connected {
		return &ConnectionError{Reason: "client is not connected"}
	}
	return nil
}

// GetConfig 返回当前配置
func (c *MockClient) GetConfig() Config {
	return c.config
}

// UpdateConfig 更新配置
func (c *MockClient) UpdateConfig(config Config) error {
	if err := config.Validate(); err != nil {
		return err
	}
	c.config = config
	c.connected = false // 更新配置后断开连接
	return nil
}

// ConnectionError 连接错误（在 shell 包中定义）
type ConnectionError struct {
	Reason string
	Err    error
}

func (e *ConnectionError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("connection failed: %s: %v", e.Reason, e.Err)
	}
	return fmt.Sprintf("connection failed: %s", e.Reason)
}

// Unwrap 返回底层错误
func (e *ConnectionError) Unwrap() error {
	return e.Err
}
