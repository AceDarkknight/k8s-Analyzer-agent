// Package shell 测试 Shell 工具方法
package shell

import (
	"context"
	"os"
	"testing"

	mcpConfig "github.com/AceDarkknight/shell-executor-mcp/pkg/configs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExecuteResult_GetSuccessfulNodes(t *testing.T) {
	tests := []struct {
		name     string
		result   ExecuteResult
		expected []string
	}{
		{
			name: "all success",
			result: ExecuteResult{
				Groups: []ExecuteGroup{
					{Status: "success", Nodes: []string{"node1", "node2"}},
				},
			},
			expected: []string{"node1", "node2"},
		},
		{
			name: "mixed status",
			result: ExecuteResult{
				Groups: []ExecuteGroup{
					{Status: "success", Nodes: []string{"node1"}},
					{Status: "failed", Nodes: []string{"node2"}},
					{Status: "success", Nodes: []string{"node3"}},
				},
			},
			expected: []string{"node1", "node3"},
		},
		{
			name: "all failed",
			result: ExecuteResult{
				Groups: []ExecuteGroup{
					{Status: "failed", Nodes: []string{"node1", "node2"}},
				},
			},
			expected: []string(nil),
		},
		{
			name:     "empty groups",
			result:   ExecuteResult{Groups: []ExecuteGroup{}},
			expected: []string(nil),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nodes := tt.result.GetSuccessfulNodes()
			assert.Equal(t, tt.expected, nodes)
		})
	}
}

func TestExecuteResult_GetFailedNodes(t *testing.T) {
	tests := []struct {
		name     string
		result   ExecuteResult
		expected []string
	}{
		{
			name: "all failed",
			result: ExecuteResult{
				Groups: []ExecuteGroup{
					{Status: "failed", Nodes: []string{"node1", "node2"}},
				},
			},
			expected: []string{"node1", "node2"},
		},
		{
			name: "mixed status",
			result: ExecuteResult{
				Groups: []ExecuteGroup{
					{Status: "success", Nodes: []string{"node1"}},
					{Status: "failed", Nodes: []string{"node2"}},
					{Status: "success", Nodes: []string{"node3"}},
				},
			},
			expected: []string{"node2"},
		},
		{
			name: "all success",
			result: ExecuteResult{
				Groups: []ExecuteGroup{
					{Status: "success", Nodes: []string{"node1", "node2"}},
				},
			},
			expected: []string(nil),
		},
		{
			name:     "empty groups",
			result:   ExecuteResult{Groups: []ExecuteGroup{}},
			expected: []string(nil),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nodes := tt.result.GetFailedNodes()
			assert.Equal(t, tt.expected, nodes)
		})
	}
}

func TestExecuteResult_GetSuccessOutput(t *testing.T) {
	result := ExecuteResult{
		Groups: []ExecuteGroup{
			{Status: "success", Output: "output1"},
			{Status: "failed", Output: "error1"},
			{Status: "success", Output: "output2"},
		},
	}

	outputs := result.GetSuccessOutput()
	assert.Equal(t, []string{"output1", "output2"}, outputs)
}

func TestExecuteResult_GetFailureOutput(t *testing.T) {
	result := ExecuteResult{
		Groups: []ExecuteGroup{
			{Status: "success", Output: "output1"},
			{Status: "failed", Output: "error1"},
			{Status: "failed", Output: "error2"},
		},
	}

	outputs := result.GetFailureOutput()
	assert.Equal(t, []string{"error1", "error2"}, outputs)
}

func TestExecuteResult_IsAllSuccess(t *testing.T) {
	tests := []struct {
		name     string
		result   ExecuteResult
		expected bool
	}{
		{
			name: "all success",
			result: ExecuteResult{
				Groups: []ExecuteGroup{
					{Status: "success", Count: 2},
				},
			},
			expected: true,
		},
		{
			name: "mixed status",
			result: ExecuteResult{
				Groups: []ExecuteGroup{
					{Status: "success", Count: 1},
					{Status: "failed", Count: 1},
				},
			},
			expected: false,
		},
		{
			name: "all failed",
			result: ExecuteResult{
				Groups: []ExecuteGroup{
					{Status: "failed", Count: 2},
				},
			},
			expected: false,
		},
		{
			name:     "empty groups",
			result:   ExecuteResult{Groups: []ExecuteGroup{}},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.result.IsAllSuccess()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExecuteResult_IsAnySuccess(t *testing.T) {
	tests := []struct {
		name     string
		result   ExecuteResult
		expected bool
	}{
		{
			name: "all success",
			result: ExecuteResult{
				Groups: []ExecuteGroup{
					{Status: "success", Count: 2},
				},
			},
			expected: true,
		},
		{
			name: "mixed status",
			result: ExecuteResult{
				Groups: []ExecuteGroup{
					{Status: "success", Count: 1},
					{Status: "failed", Count: 1},
				},
			},
			expected: true,
		},
		{
			name: "all failed",
			result: ExecuteResult{
				Groups: []ExecuteGroup{
					{Status: "failed", Count: 2},
				},
			},
			expected: false,
		},
		{
			name:     "empty groups",
			result:   ExecuteResult{Groups: []ExecuteGroup{}},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.result.IsAnySuccess()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExecuteResult_GetTotalNodes(t *testing.T) {
	tests := []struct {
		name     string
		result   ExecuteResult
		expected int
	}{
		{
			name: "multiple groups",
			result: ExecuteResult{
				Groups: []ExecuteGroup{
					{Count: 2},
					{Count: 3},
					{Count: 1},
				},
			},
			expected: 6,
		},
		{
			name:     "single group",
			result:   ExecuteResult{Groups: []ExecuteGroup{{Count: 5}}},
			expected: 5,
		},
		{
			name:     "empty groups",
			result:   ExecuteResult{Groups: []ExecuteGroup{}},
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			total := tt.result.GetTotalNodes()
			assert.Equal(t, tt.expected, total)
		})
	}
}

func TestExecuteResult_GetSuccessCount(t *testing.T) {
	tests := []struct {
		name     string
		result   ExecuteResult
		expected int
	}{
		{
			name: "all success",
			result: ExecuteResult{
				Groups: []ExecuteGroup{
					{Status: "success", Count: 3},
					{Status: "success", Count: 2},
				},
			},
			expected: 5,
		},
		{
			name: "mixed status",
			result: ExecuteResult{
				Groups: []ExecuteGroup{
					{Status: "success", Count: 2},
					{Status: "failed", Count: 1},
				},
			},
			expected: 2,
		},
		{
			name: "all failed",
			result: ExecuteResult{
				Groups: []ExecuteGroup{
					{Status: "failed", Count: 3},
				},
			},
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			count := tt.result.GetSuccessCount()
			assert.Equal(t, tt.expected, count)
		})
	}
}

func TestExecuteResult_GetFailureCount(t *testing.T) {
	tests := []struct {
		name     string
		result   ExecuteResult
		expected int
	}{
		{
			name: "all failed",
			result: ExecuteResult{
				Groups: []ExecuteGroup{
					{Status: "failed", Count: 3},
					{Status: "failed", Count: 2},
				},
			},
			expected: 5,
		},
		{
			name: "mixed status",
			result: ExecuteResult{
				Groups: []ExecuteGroup{
					{Status: "success", Count: 2},
					{Status: "failed", Count: 1},
				},
			},
			expected: 1,
		},
		{
			name: "all success",
			result: ExecuteResult{
				Groups: []ExecuteGroup{
					{Status: "success", Count: 3},
				},
			},
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			count := tt.result.GetFailureCount()
			assert.Equal(t, tt.expected, count)
		})
	}
}

func TestExecuteResult_FormatSummary(t *testing.T) {
	tests := []struct {
		name     string
		result   ExecuteResult
		expected string
	}{
		{
			name: "with custom summary",
			result: ExecuteResult{
				Summary: "Executed on 3 nodes, 2 groups found",
			},
			expected: "Executed on 3 nodes, 2 groups found",
		},
		{
			name: "auto-generated summary",
			result: ExecuteResult{
				Groups: []ExecuteGroup{
					{Status: "success", Count: 2},
					{Status: "failed", Count: 1},
				},
			},
			expected: "Executed on 3 nodes: 2 success, 1 failed",
		},
		{
			name:     "empty result",
			result:   ExecuteResult{Groups: []ExecuteGroup{}},
			expected: "Executed on 0 nodes: 0 success, 0 failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			summary := tt.result.FormatSummary()
			assert.Equal(t, tt.expected, summary)
		})
	}
}

func TestClient_ExecuteCommand_EmptyCommand(t *testing.T) {
	config := Config{
		McpConfig: mcpConfig.ClientConfig{
			Token: "test-token",
			Servers: []mcpConfig.ServerConfig{
				{Name: "server1", URL: "http://localhost:8080"},
			},
		},
	}

	client, err := NewClient(config)
	require.NoError(t, err, "创建 client 不应失败")

	ctx := context.Background()
	_, err = client.ExecuteCommand(ctx, "")
	assert.Error(t, err, "空命令应该返回错误")
	assert.Contains(t, err.Error(), "command is required", "错误信息应包含命令为空的提示")
}

func TestClient_ExecuteCommand_NotConnected(t *testing.T) {
	config := Config{
		McpConfig: mcpConfig.ClientConfig{
			Token: "test-token",
			Servers: []mcpConfig.ServerConfig{
				{Name: "server1", URL: "http://localhost:8080"},
			},
		},
	}

	client, err := NewClient(config)
	require.NoError(t, err, "创建 client 不应失败")

	ctx := context.Background()
	_, err = client.ExecuteCommand(ctx, "ls -la")
	assert.Error(t, err, "未连接时执行命令应该失败")
	assert.Contains(t, err.Error(), "not connected", "错误信息应包含未连接标记")
}

func TestClient_ExecuteCommandWithTimeout_EmptyCommand(t *testing.T) {
	config := Config{
		McpConfig: mcpConfig.ClientConfig{
			Token: "test-token",
			Servers: []mcpConfig.ServerConfig{
				{Name: "server1", URL: "http://localhost:8080"},
			},
		},
	}

	client, err := NewClient(config)
	require.NoError(t, err, "创建 client 不应失败")

	ctx := context.Background()
	_, err = client.ExecuteCommandWithTimeout(ctx, "", 10)
	assert.Error(t, err, "空命令应该返回错误")
	assert.Contains(t, err.Error(), "command is required", "错误信息应包含命令为空的提示")
}

func TestParseToolResult(t *testing.T) {
	tests := []struct {
		name        string
		result      *ExecuteResult
		expectError bool
	}{
		{
			name: "valid result",
			result: &ExecuteResult{
				Summary: "test summary",
				Groups: []ExecuteGroup{
					{Status: "success", Count: 1},
				},
			},
			expectError: false,
		},
		{
			name:        "nil result",
			result:      nil,
			expectError: true,
		},
		{
			name:        "empty content",
			result:      &ExecuteResult{},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 注意：ParseToolResult 需要实际的 mcp.CallToolResult
			// 这里只是测试函数签名，实际测试需要 mock MCP SDK
			t.Skip("需要 mock MCP SDK")
		})
	}
}

// 集成测试（需要真实的 MCP Server 运行）
func TestClient_ExecuteCommand_Integration(t *testing.T) {
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
	}

	client, err := NewClient(config)
	require.NoError(t, err, "创建 client 不应失败")

	ctx := context.Background()
	err = client.Connect(ctx)
	if err != nil {
		t.Skipf("无法连接到 MCP Server: %v", err)
	}
	defer client.Close()

	// 测试执行命令
	result, err := client.ExecuteCommand(ctx, "ls -la /tmp")
	require.NoError(t, err, "执行命令不应失败")
	assert.NotNil(t, result, "结果不应为 nil")
	assert.NotEmpty(t, result.Summary, "摘要不应为空")
}

// TestIsKubectlCommand 测试 kubectl 命令检测
func TestIsKubectlCommand(t *testing.T) {
	tests := []struct {
		name     string
		command  string
		expected bool
	}{
		{
			name:     "kubectl get pods",
			command:  "kubectl get pods",
			expected: true,
		},
		{
			name:     "kubectl get pods -A",
			command:  "kubectl get pods -A",
			expected: true,
		},
		{
			name:     "kubectl describe pod my-pod",
			command:  "kubectl describe pod my-pod",
			expected: true,
		},
		{
			name:     "kubectl logs my-pod -n default",
			command:  "kubectl logs my-pod -n default",
			expected: true,
		},
		{
			name:     "KUBECTL get pods (uppercase)",
			command:  "KUBECTL get pods",
			expected: true,
		},
		{
			name:     "Kubectl get pods (mixed case)",
			command:  "Kubectl get pods",
			expected: true,
		},
		{
			name:     "sudo kubectl get pods",
			command:  "sudo kubectl get pods",
			expected: true,
		},
		{
			name:     "ls -la",
			command:  "ls -la",
			expected: false,
		},
		{
			name:     "curl http://localhost",
			command:  "curl http://localhost",
			expected: false,
		},
		{
			name:     "ping 8.8.8.8",
			command:  "ping 8.8.8.8",
			expected: false,
		},
		{
			name:     "docker ps",
			command:  "docker ps",
			expected: false,
		},
		{
			name:     "echo kubectl",
			command:  "echo kubectl",
			expected: false,
		},
		{
			name:     "empty command",
			command:  "",
			expected: false,
		},
		{
			name:     "kubectl.exe get pods (Windows)",
			command:  "kubectl.exe get pods",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isKubectlCommand(tt.command)
			assert.Equal(t, tt.expected, result, "isKubectlCommand(%q) = %v, want %v", tt.command, result, tt.expected)
		})
	}
}

// TestClient_ExecuteCommand_KubectlBlocked 测试 kubectl 命令被阻止
func TestClient_ExecuteCommand_KubectlBlocked(t *testing.T) {
	config := Config{
		McpConfig: mcpConfig.ClientConfig{
			Token: "test-token",
			Servers: []mcpConfig.ServerConfig{
				{Name: "server1", URL: "http://localhost:8080"},
			},
		},
	}

	client, err := NewClient(config)
	require.NoError(t, err, "创建 client 不应失败")

	ctx := context.Background()

	tests := []struct {
		name    string
		command string
	}{
		{"kubectl get pods", "kubectl get pods"},
		{"kubectl get pods -A", "kubectl get pods -A"},
		{"kubectl describe pod", "kubectl describe pod my-pod"},
		{"kubectl logs", "kubectl logs my-pod"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.ExecuteCommand(ctx, tt.command)
			assert.Error(t, err, "kubectl 命令应该被阻止")
			assert.Equal(t, ErrKubectlBlocked, err, "应该返回 ErrKubectlBlocked 错误")
		})
	}
}

// TestClient_ExecuteCommandWithTimeout_KubectlBlocked 测试带超时的 kubectl 命令被阻止
func TestClient_ExecuteCommandWithTimeout_KubectlBlocked(t *testing.T) {
	config := Config{
		McpConfig: mcpConfig.ClientConfig{
			Token: "test-token",
			Servers: []mcpConfig.ServerConfig{
				{Name: "server1", URL: "http://localhost:8080"},
			},
		},
	}

	client, err := NewClient(config)
	require.NoError(t, err, "创建 client 不应失败")

	ctx := context.Background()

	_, err = client.ExecuteCommandWithTimeout(ctx, "kubectl get pods", 10)
	assert.Error(t, err, "kubectl 命令应该被阻止")
	assert.Equal(t, ErrKubectlBlocked, err, "应该返回 ErrKubectlBlocked 错误")
}
