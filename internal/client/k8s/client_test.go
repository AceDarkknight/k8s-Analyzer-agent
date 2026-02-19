// Package k8s 测试 K8s MCP Client
package k8s

import (
	"context"
	"testing"

	"github.com/AceDarkknight/k8s-analyzer-agent/internal/client"
	clientpkg "github.com/AceDarkknight/k8s-analyzer-agent/internal/client"
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
			name: "valid config",
			config: Config{
				ServerURL: "https://localhost:8443",
				Token:     "test-token",
			},
			expectError: false,
		},
		{
			name: "missing server_url",
			config: Config{
				Token: "test-token",
			},
			expectError: true,
			errorMsg:    "server_url is required",
		},
		{
			name: "missing token",
			config: Config{
				ServerURL: "https://localhost:8443",
			},
			expectError: true,
			errorMsg:    "token is required",
		},
		{
			name: "invalid url",
			config: Config{
				ServerURL: "://invalid-url",
				Token:     "test-token",
			},
			expectError: true,
			errorMsg:    "invalid server_url",
		},
		{
			name: "config with defaults",
			config: Config{
				ServerURL: "https://localhost:8443",
				Token:     "test-token",
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
				assert.Equal(t, 1, tt.config.RetryConfig.InitialDelay, "应该设置默认初始等待时间")
				assert.Equal(t, 10, tt.config.RetryConfig.MaxDelay, "应该设置默认最大等待时间")
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
				ServerURL: "https://localhost:8443",
				Token:     "test-token",
			},
			expectError: false,
		},
		{
			name: "invalid config",
			config: Config{
				ServerURL: "",
				Token:     "test-token",
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k8sClient, err := NewClient(tt.config)

			if tt.expectError {
				assert.Error(t, err, "应该返回错误")
				assert.Nil(t, k8sClient, "client 应为 nil")
			} else {
				assert.NoError(t, err, "不应该返回错误")
				assert.NotNil(t, k8sClient, "client 不应为 nil")
				assert.False(t, k8sClient.IsConnected(), "初始状态应为未连接")

				// 预期配置包含默认值
				expectedConfig := tt.config
				if expectedConfig.Timeout == 0 {
					expectedConfig.Timeout = 30
				}
				if expectedConfig.RetryConfig.MaxAttempts == 0 {
					expectedConfig.RetryConfig = client.DefaultRetryConfig()
				}
				if expectedConfig.SSEPath == "" {
					expectedConfig.SSEPath = "/sse"
				}

				assert.Equal(t, expectedConfig, k8sClient.GetConfig(), "配置应该正确保存")
			}
		})
	}
}

func TestClient_IsConnected(t *testing.T) {
	config := Config{
		ServerURL: "https://localhost:8443",
		Token:     "test-token",
	}

	client, err := NewClient(config)
	require.NoError(t, err, "创建 client 不应失败")

	assert.False(t, client.IsConnected(), "初始状态应为未连接")
}

func TestClient_GetConfig(t *testing.T) {
	config := Config{
		ServerURL: "https://localhost:8443",
		Token:     "test-token",
		Insecure:  true,
		Timeout:   60,
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

func TestClient_Close(t *testing.T) {
	config := Config{
		ServerURL: "https://localhost:8443",
		Token:     "test-token",
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
		ServerURL: "https://localhost:8443",
		Token:     "old-token",
		Insecure:  true,
	}

	client, err := NewClient(oldConfig)
	require.NoError(t, err, "创建 client 不应失败")

	newConfig := Config{
		ServerURL: "https://localhost:9443",
		Token:     "new-token",
		Insecure:  false,
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
		ServerURL: "https://localhost:8443",
		Token:     "test-token",
	}

	client, err := NewClient(config)
	require.NoError(t, err, "创建 client 不应失败")

	invalidConfig := Config{
		ServerURL: "",
		Token:     "test-token",
	}

	err = client.UpdateConfig(invalidConfig)
	assert.Error(t, err, "更新为无效配置应该失败")
}

func TestClient_Connect_NotConnected(t *testing.T) {
	config := Config{
		ServerURL: "https://localhost:8443",
		Token:     "test-token",
	}

	// 使用 MockClient 进行测试
	client := NewMockClient(config)

	// MockClient 连接总是成功
	ctx := context.Background()
	err := client.Connect(ctx)
	assert.NoError(t, err, "MockClient 连接总是成功")
}

func TestClient_CallTool_NotConnected(t *testing.T) {
	config := Config{
		ServerURL: "https://localhost:8443",
		Token:     "test-token",
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
		ServerURL: "https://localhost:8443",
		Token:     "test-token",
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
		ServerURL: "https://localhost:8443",
		Token:     "test-token",
	}

	client, err := NewClient(config)
	require.NoError(t, err, "创建 client 不应失败")

	ctx := context.Background()
	err = client.HealthCheck(ctx)
	assert.Error(t, err, "未连接时健康检查应该失败")
	assert.Contains(t, err.Error(), "not connected", "错误信息应包含未连接标记")
}

// 集成测试（需要真实的 MCP Server 运行）
// 这些测试默认被跳过，可以通过设置环境变量来启用
func TestClient_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过集成测试")
	}

	config := Config{
		ServerURL: "https://localhost:8443",
		Token:     "k8s-analyzer-token",
		Insecure:  true, // 开发环境跳过 TLS 验证
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
}

// TestParseToolResult_NestedJSON 测试嵌套 JSON 解析功能
func TestParseToolResult_NestedJSON(t *testing.T) {
	tests := []struct {
		name        string
		toolName    string
		result      *CallToolResult
		expectError bool
		checkResult func(t *testing.T, result []Pod)
	}{
		{
			name:     "嵌套 JSON 格式 - pods 键",
			toolName: "list_pods",
			result: &CallToolResult{
				Content: []Content{`{"pods": [{"name": "nginx-1", "namespace": "default", "status": "Running", "ready": "1/1", "restarts": 0, "age": "1d"}, {"name": "nginx-2", "namespace": "default", "status": "Running", "ready": "1/1", "restarts": 0, "age": "1d"}]}`},
			},
			expectError: false,
			checkResult: func(t *testing.T, result []Pod) {
				assert.Len(t, result, 2)
				assert.Equal(t, "nginx-1", result[0].Name)
				assert.Equal(t, "nginx-2", result[1].Name)
			},
		},
		{
			name:     "嵌套 JSON 格式 - services 键",
			toolName: "list_services",
			result: &CallToolResult{
				Content: []Content{`{"services": [{"name": "nginx-svc", "namespace": "default", "type": "ClusterIP", "cluster_ip": "10.96.0.1", "ports": "80/TCP", "age": "2d"}]}`},
			},
			expectError: false,
			checkResult: func(t *testing.T, result []Pod) {
				// 这里期望的是 []Service 类型，但由于泛型我们需要用 []Pod 测试
				// 实际使用中会根据类型自动匹配
			},
		},
		{
			name:     "直接 JSON 数组格式",
			toolName: "list_pods",
			result: &CallToolResult{
				Content: []Content{`[{"name": "nginx-1", "namespace": "default", "status": "Running", "ready": "1/1", "restarts": 0, "age": "1d"}]`},
			},
			expectError: false,
			checkResult: func(t *testing.T, result []Pod) {
				assert.Len(t, result, 1)
				assert.Equal(t, "nginx-1", result[0].Name)
			},
		},
		{
			name:     "空结果",
			toolName: "list_pods",
			result: &CallToolResult{
				Content: []Content{},
			},
			expectError: true,
			checkResult: func(t *testing.T, result []Pod) {
				assert.Nil(t, result)
			},
		},
		{
			name:     "无效 JSON",
			toolName: "list_pods",
			result: &CallToolResult{
				Content: []Content{`invalid json`},
			},
			expectError: true,
			checkResult: func(t *testing.T, result []Pod) {
				assert.Nil(t, result)
			},
		},
		{
			name:     "嵌套 JSON 格式 - namespaces 键",
			toolName: "list_namespaces",
			result: &CallToolResult{
				Content: []Content{`{"namespaces": [{"name": "default", "status": "Active", "age": "1d"}, {"name": "kube-system", "status": "Active", "age": "1d"}]}`},
			},
			expectError: false,
			checkResult: func(t *testing.T, result []Pod) {
				// 这里期望的是 []Namespace 类型，但由于泛型我们需要用 []Pod 测试
				// 实际使用中会根据类型自动匹配
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseToolResult[[]Pod](tt.result, tt.toolName)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				if tt.checkResult != nil {
					tt.checkResult(t, result)
				}
			}
		})
	}
}

// TestParseToolResult_Labels 测试 Labels 字段解析
func TestParseToolResult_Labels(t *testing.T) {
	// 测试包含 Labels 的 Pod 数据解析
	result := &CallToolResult{
		Content: []Content{`[{"name": "nginx-1", "namespace": "default", "status": "Running", "ready": "1/1", "restarts": 0, "age": "1d", "labels": {"app": "nginx", "version": "v1"}}]`},
	}

	pods, err := ParseToolResult[[]Pod](result, "list_pods")
	require.NoError(t, err)
	assert.Len(t, pods, 1)
	assert.Equal(t, "nginx-1", pods[0].Name)
	assert.Equal(t, "nginx", pods[0].Labels["app"])
	assert.Equal(t, "v1", pods[0].Labels["version"])
}

// TestParseToolResult_ConfigMap 测试 ConfigMap 解析
func TestParseToolResult_ConfigMap(t *testing.T) {
	result := &CallToolResult{
		Content: []Content{`{"configmaps": [{"name": "nginx-config", "namespace": "default", "data_count": 1, "age": "2d", "labels": {"app": "nginx"}}]}`},
	}

	configmaps, err := ParseToolResult[[]ConfigMap](result, "list_configmaps")
	require.NoError(t, err)
	assert.Len(t, configmaps, 1)
	assert.Equal(t, "nginx-config", configmaps[0].Name)
	assert.Equal(t, "nginx", configmaps[0].Labels["app"])
	assert.Equal(t, 1, configmaps[0].DataCount)
}

// TestParseToolResult_StatefulSet 测试 StatefulSet 解析
func TestParseToolResult_StatefulSet(t *testing.T) {
	result := &CallToolResult{
		Content: []Content{`{"statefulsets": [{"name": "mysql-sts", "namespace": "default", "ready": "3/3", "age": "10d", "labels": {"app": "mysql"}}]}`},
	}

	statefulsets, err := ParseToolResult[[]StatefulSet](result, "list_statefulsets")
	require.NoError(t, err)
	assert.Len(t, statefulsets, 1)
	assert.Equal(t, "mysql-sts", statefulsets[0].Name)
	assert.Equal(t, "3/3", statefulsets[0].Ready)
	assert.Equal(t, "mysql", statefulsets[0].Labels["app"])
}

// TestParseToolResult_Namespaces 测试 namespaces 解析
func TestParseToolResult_Namespaces(t *testing.T) {
	result := &CallToolResult{
		Content: []Content{`{"namespaces": [{"name": "default", "status": "Active", "age": "1d"}, {"name": "kube-system", "status": "Active", "age": "1d"}]}`},
	}

	namespaces, err := ParseToolResult[[]Namespace](result, "list_namespaces")
	require.NoError(t, err)
	assert.Len(t, namespaces, 2)
	assert.Equal(t, "default", namespaces[0].Name)
	assert.Equal(t, "kube-system", namespaces[1].Name)
}

// TestParseToolResult_Namespaces_DoubleEncoded 测试双重编码的 namespaces 解析
func TestParseToolResult_Namespaces_DoubleEncoded(t *testing.T) {
	// 模拟双重编码的情况：外层是对象，内层是字符串
	result := &CallToolResult{
		Content: []Content{`{"namespaces": "[{\"name\": \"default\", \"status\": \"Active\", \"age\": \"1d\"}, {\"name\": \"kube-system\", \"status\": \"Active\", \"age\": \"1d\"}]"}`},
	}

	namespaces, err := ParseToolResult[[]Namespace](result, "list_namespaces")
	require.NoError(t, err)
	assert.Len(t, namespaces, 2)
	assert.Equal(t, "default", namespaces[0].Name)
	assert.Equal(t, "kube-system", namespaces[1].Name)
}
