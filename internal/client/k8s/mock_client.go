// Package k8s 提供 Mock K8s Client，用于测试和演示
package k8s

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// MockClient Mock K8s Client 实现
// 用于测试和演示，不需要真实的 MCP Server
type MockClient struct {
	config    Config
	connected bool
}

// NewMockClient 创建新的 Mock K8s Client
func NewMockClient(config Config) *MockClient {
	return &MockClient{
		config:    config,
		connected: false,
	}
}

// NewMockClientFromFile 从配置文件创建 Mock K8s Client
func NewMockClientFromFile(configPath string) (*MockClient, error) {
	config := Config{
		ServerURL: "https://localhost:8443",
		Token:     "mock-token",
		Insecure:  true,
		Timeout:   30 * time.Second,
		RetryConfig: RetryConfig{
			MaxAttempts: 3,
			InitialWait: 1 * time.Second,
			MaxWait:     10 * time.Second,
		},
		SSEPath: "/sse",
	}

	return NewMockClient(config), nil
}

// Connect 建立与 K8s MCP Server 的连接（模拟）
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

// Close 终止与 K8s MCP Server 的连接（模拟）
func (c *MockClient) Close() error {
	c.connected = false
	return nil
}

// CallTool 执行 K8s MCP Server 上的特定工具（模拟）
func (c *MockClient) CallTool(ctx context.Context, name string, args map[string]interface{}) (*CallToolResult, error) {
	if !c.connected {
		return nil, &ConnectionError{Reason: "client is not connected"}
	}

	// 根据工具名称返回模拟数据
	var textData string
	switch name {
	case "list_pods":
		textData = c.mockListPodsJSON(args)
	case "list_services":
		textData = c.mockListServicesJSON(args)
	case "list_deployments":
		textData = c.mockListDeploymentsJSON(args)
	case "list_events":
		textData = c.mockListEventsJSON(args)
	case "get_pod_logs":
		textData = c.mockGetPodLogsJSON(args)
	default:
		textData = fmt.Sprintf("Mock response for tool: %s", name)
	}

	return &CallToolResult{
		Content: []Content{textData},
	}, nil
}

// ListTools 获取 K8s MCP Server 上可用的工具列表（模拟）
func (c *MockClient) ListTools(ctx context.Context) ([]Tool, error) {
	if !c.connected {
		return nil, &ConnectionError{Reason: "client is not connected"}
	}

	return []Tool{
		{
			Name:        "list_pods",
			Description: "List pods in a namespace",
		},
		{
			Name:        "list_services",
			Description: "List services in a namespace",
		},
		{
			Name:        "list_deployments",
			Description: "List deployments in a namespace",
		},
		{
			Name:        "list_events",
			Description: "List events in a namespace",
		},
		{
			Name:        "get_pod_logs",
			Description: "Get logs for a pod",
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

// mockListPodsJSON 模拟列出 Pod（返回 JSON）
func (c *MockClient) mockListPodsJSON(args map[string]interface{}) string {
	namespace := "default"
	if ns, ok := args["namespace"].(string); ok {
		namespace = ns
	}

	pods := []Pod{
		{
			Name:      "nginx-pod-1",
			Namespace: namespace,
			Status:    "Running",
			Ready:     "1/1",
			Restarts:  0,
			Age:       "2d",
		},
		{
			Name:      "nginx-pod-2",
			Namespace: namespace,
			Status:    "Running",
			Ready:     "1/1",
			Restarts:  0,
			Age:       "2d",
		},
		{
			Name:      "error-pod",
			Namespace: namespace,
			Status:    "Error",
			Ready:     "0/1",
			Restarts:  5,
			Age:       "1d",
		},
	}

	data, _ := json.Marshal(pods)
	return string(data)
}

// mockListServicesJSON 模拟列出 Service（返回 JSON）
func (c *MockClient) mockListServicesJSON(args map[string]interface{}) string {
	namespace := "default"
	if ns, ok := args["namespace"].(string); ok {
		namespace = ns
	}

	services := []Service{
		{
			Name:      "nginx-service",
			Namespace: namespace,
			Type:      "ClusterIP",
			ClusterIP: "10.96.0.1",
			Ports:     "80/TCP",
			Age:       "2d",
		},
	}

	data, _ := json.Marshal(services)
	return string(data)
}

// mockListDeploymentsJSON 模拟列出 Deployment（返回 JSON）
func (c *MockClient) mockListDeploymentsJSON(args map[string]interface{}) string {
	namespace := "default"
	if ns, ok := args["namespace"].(string); ok {
		namespace = ns
	}

	deployments := []Deployment{
		{
			Name:      "nginx-deployment",
			Namespace: namespace,
			Ready:     "2/2",
			UpToDate:  "2",
			Available: "2",
			Age:       "2d",
		},
	}

	data, _ := json.Marshal(deployments)
	return string(data)
}

// mockListEventsJSON 模拟列出事件（返回 JSON）
func (c *MockClient) mockListEventsJSON(args map[string]interface{}) string {
	_ = args // 使用变量避免编译警告

	events := []Event{
		{
			Type:      "Warning",
			Reason:    "FailedScheduling",
			Message:   "0/3 nodes are available: 3 Insufficient cpu.",
			Source:    "scheduler",
			Count:     2,
			FirstSeen: "2024-01-25T10:00:00Z",
			LastSeen:  "2024-01-25T10:05:00Z",
		},
		{
			Type:      "Normal",
			Reason:    "Scheduled",
			Message:   "Successfully assigned default/pod-1 to node-1",
			Source:    "scheduler",
			Count:     1,
			FirstSeen: "2024-01-25T10:10:00Z",
			LastSeen:  "2024-01-25T10:10:00Z",
		},
	}

	data, _ := json.Marshal(events)
	return string(data)
}

// mockGetPodLogsJSON 模拟获取 Pod 日志（返回 JSON）
func (c *MockClient) mockGetPodLogsJSON(args map[string]interface{}) string {
	podName := "unknown"
	if name, ok := args["pod_name"].(string); ok {
		podName = name
	}

	logs := fmt.Sprintf("Mock logs for pod %s:\n2024-01-25T10:00:00Z INFO Starting application...\n2024-01-25T10:00:01Z ERROR Failed to connect to database\n2024-01-25T10:00:02Z INFO Retrying connection...\n2024-01-25T10:00:05Z ERROR Connection timeout", podName)

	return logs
}
