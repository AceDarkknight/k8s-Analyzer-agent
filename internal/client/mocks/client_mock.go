// Package mocks 提供 MCP Client 的 Mock 实现，用于单元测试
package mocks

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// MockClient 是 MCPClient 接口的 Mock 实现
type MockClient struct {
	// Connected 模拟连接状态
	Connected bool

	// Tools 模拟可用的工具列表
	Tools []mcp.Tool

	// ToolResults 模拟工具调用的结果
	ToolResults map[string]*mcp.CallToolResult

	// CallToolFunc 自定义工具调用函数
	CallToolFunc func(ctx context.Context, name string, args map[string]interface{}) (*mcp.CallToolResult, error)

	// ConnectFunc 自定义连接函数
	ConnectFunc func(ctx context.Context) error

	// CloseFunc 自定义关闭函数
	CloseFunc func() error

	// ListToolsFunc 自定义列出工具函数
	ListToolsFunc func(ctx context.Context) ([]mcp.Tool, error)

	// Error 模拟错误
	Error error
}

// NewMockClient 创建新的 Mock Client
func NewMockClient() *MockClient {
	return &MockClient{
		Connected:    false,
		Tools:        []mcp.Tool{},
		ToolResults:  make(map[string]*mcp.CallToolResult),
		CallToolFunc: nil,
	}
}

// Connect 建立连接
func (m *MockClient) Connect(ctx context.Context) error {
	if m.ConnectFunc != nil {
		return m.ConnectFunc(ctx)
	}
	if m.Error != nil {
		return m.Error
	}
	m.Connected = true
	return nil
}

// Close 关闭连接
func (m *MockClient) Close() error {
	if m.CloseFunc != nil {
		return m.CloseFunc()
	}
	m.Connected = false
	return nil
}

// CallTool 调用工具
func (m *MockClient) CallTool(ctx context.Context, name string, args map[string]interface{}) (*mcp.CallToolResult, error) {
	if m.CallToolFunc != nil {
		return m.CallToolFunc(ctx, name, args)
	}
	if m.Error != nil {
		return nil, m.Error
	}
	if result, ok := m.ToolResults[name]; ok {
		return result, nil
	}
	return nil, fmt.Errorf("tool '%s' not found", name)
}

// ListTools 列出可用工具
func (m *MockClient) ListTools(ctx context.Context) ([]mcp.Tool, error) {
	if m.ListToolsFunc != nil {
		return m.ListToolsFunc(ctx)
	}
	if m.Error != nil {
		return nil, m.Error
	}
	return m.Tools, nil
}

// SetToolResult 设置工具调用结果
func (m *MockClient) SetToolResult(name string, result *mcp.CallToolResult) {
	m.ToolResults[name] = result
}

// SetTools 设置可用工具列表
func (m *MockClient) SetTools(tools []mcp.Tool) {
	m.Tools = tools
}

// SetError 设置错误
func (m *MockClient) SetError(err error) {
	m.Error = err
}

// Reset 重置 Mock 状态
func (m *MockClient) Reset() {
	m.Connected = false
	m.Tools = []mcp.Tool{}
	m.ToolResults = make(map[string]*mcp.CallToolResult)
	m.CallToolFunc = nil
	m.ConnectFunc = nil
	m.CloseFunc = nil
	m.ListToolsFunc = nil
	m.Error = nil
}

// MockK8sClient 是 K8s Client 的 Mock 实现
type MockK8sClient struct {
	*MockClient

	// ClusterStatus 模拟集群状态
	ClusterStatus interface{}

	// Pods 模拟 Pod 列表
	Pods interface{}

	// Services 模拟 Service 列表
	Services interface{}

	// Deployments 模拟 Deployment 列表
	Deployments interface{}

	// Nodes 模拟节点列表
	Nodes interface{}

	// Events 模拟事件列表
	Events interface{}

	// Logs 模拟日志
	Logs interface{}

	// RBACPermission 模拟 RBAC 权限
	RBACPermission interface{}
}

// NewMockK8sClient 创建新的 Mock K8s Client
func NewMockK8sClient() *MockK8sClient {
	return &MockK8sClient{
		MockClient: NewMockClient(),
	}
}

// MockShellClient 是 Shell Client 的 Mock 实现
type MockShellClient struct {
	*MockClient

	// ExecuteResult 模拟命令执行结果
	ExecuteResult interface{}
}

// NewMockShellClient 创建新的 Mock Shell Client
func NewMockShellClient() *MockShellClient {
	return &MockShellClient{
		MockClient: NewMockClient(),
	}
}

// Helper functions for creating mock results

// NewMockToolResult 创建 Mock 工具调用结果
func NewMockToolResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{
				Text: text,
			},
		},
	}
}

// NewMockTool 创建 Mock 工具
func NewMockTool(name, description string, inputSchema interface{}) mcp.Tool {
	return mcp.Tool{
		Name:        name,
		Description: description,
		InputSchema: inputSchema,
	}
}

// NewMockToolWithError 创建带有错误的 Mock 工具调用结果
func NewMockToolWithError(error string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{
				Text: fmt.Sprintf(`{"error": "%s"}`, error),
			},
		},
		IsError: true,
	}
}
