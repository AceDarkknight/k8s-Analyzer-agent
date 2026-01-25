// Package k8s 提供 K8s MCP 工具的便捷封装方法
package k8s

import (
	"encoding/json"
	"fmt"
)

// ClusterStatus 集群状态信息
type ClusterStatus struct {
	Version        string `json:"version"`
	NodeCount      int    `json:"node_count"`
	NamespaceCount int    `json:"namespace_count"`
}

// Pod Pod 信息
type Pod struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Status    string `json:"status"`
	Ready     string `json:"ready"`
	Restarts  int    `json:"restarts"`
	Age       string `json:"age"`
}

// Service Service 信息
type Service struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Type      string `json:"type"`
	ClusterIP string `json:"cluster_ip"`
	Ports     string `json:"ports"`
	Age       string `json:"age"`
}

// Deployment Deployment 信息
type Deployment struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Ready     string `json:"ready"`
	UpToDate  string `json:"up_to_date"`
	Available string `json:"available"`
	Age       string `json:"age"`
}

// Node 节点信息
type Node struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Roles   string `json:"roles"`
	Version string `json:"version"`
	Age     string `json:"age"`
}

// Event 事件信息
type Event struct {
	Type      string `json:"type"`
	Reason    string `json:"reason"`
	Message   string `json:"message"`
	Source    string `json:"source"`
	Count     int    `json:"count"`
	FirstSeen string `json:"first_seen"`
	LastSeen  string `json:"last_seen"`
}

// RBACPermission RBAC 权限检查结果
type RBACPermission struct {
	Allowed bool   `json:"allowed"`
	Reason  string `json:"reason,omitempty"`
}

// PodLogOptions Pod 日志选项
type PodLogOptions struct {
	ContainerName string `json:"container_name,omitempty"`
	TailLines     int    `json:"tail_lines,omitempty"`
	Previous      bool   `json:"previous,omitempty"`
	ClusterName   string `json:"cluster_name,omitempty"`
}

// ParseToolResult 解析工具调用结果为指定类型
func ParseToolResult[T any](result *CallToolResult) (T, error) {
	var zero T

	if result == nil || len(result.Content) == 0 {
		return zero, fmt.Errorf("empty result")
	}

	// 获取第一个内容
	textData, ok := result.Content[0].(string)
	if !ok {
		return zero, fmt.Errorf("result content is not string")
	}

	var value T
	if err := json.Unmarshal([]byte(textData), &value); err != nil {
		return zero, fmt.Errorf("failed to parse result: %w", err)
	}

	return value, nil
}

// ParseToolResultAsString 解析工具调用结果为字符串
func ParseToolResultAsString(result *CallToolResult) (string, error) {
	if result == nil || len(result.Content) == 0 {
		return "", fmt.Errorf("empty result")
	}

	// 获取第一个内容
	textData, ok := result.Content[0].(string)
	if !ok {
		return "", fmt.Errorf("result content is not string")
	}

	return textData, nil
}
