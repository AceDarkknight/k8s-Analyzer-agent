// Package k8s 提供 K8s MCP 工具的便捷封装方法
package k8s

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/AceDarkknight/k8s-mcp/pkg/mcpclient"
	"github.com/AceDarkknight/k8s-mcp/pkg/types"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ClusterStatus 集群状态信息
type ClusterStatus struct {
	Version        string `json:"version"`
	NodeCount      int    `json:"node_count"`
	NamespaceCount int    `json:"namespace_count"`
}

// 使用 k8s-mcp 包中的类型定义
// Pod, Service, Deployment, Node, Event, ConfigMap, StatefulSet, RBACPermission, PodLogOptions
// 请参考 github.com/AceDarkknight/k8s-mcp/pkg/types

// resourceKeyMapping 定义资源名称到 JSON 键名的映射关系
// 用于处理 k8s-mcp 返回的嵌套 JSON 格式，如 {"pods": "[..."}
var resourceKeyMapping = map[string]string{
	"list_pods":         "pods",
	"list_services":     "services",
	"list_deployments":  "deployments",
	"list_nodes":        "nodes",
	"list_events":       "events",
	"list_configmaps":   "configmaps",
	"list_statefulsets": "statefulsets",
	"list_namespaces":   "namespaces",
	// "list_persistentvolumeclaims": "persistentvolumeclaims",
	// "list_secrets":                "secrets",
	// "list_ingresses":              "ingresses",
}

// CallToolResultToMCP 将自定义 CallToolResult 转换为 mcp.CallToolResult
// 用于与 k8s-mcp 的 DecodeResult 方法兼容
func CallToolResultToMcp(result *CallToolResult) *mcp.CallToolResult {
	if result == nil {
		return nil
	}

	// 将 Content 转换为 mcp.Content
	mcpContents := make([]mcp.Content, 0, len(result.Content))
	for _, c := range result.Content {
		// 尝试将 string 转换为 *mcp.TextContent
		if str, ok := c.(string); ok {
			mcpContents = append(mcpContents, &mcp.TextContent{
				Text: str,
			})
		} else if tc, ok := c.(*mcp.TextContent); ok {
			mcpContents = append(mcpContents, tc)
		} else {
			// 其他类型尝试序列化
			mcpContents = append(mcpContents, &mcp.TextContent{
				Text: fmt.Sprintf("%v", c),
			})
		}
	}

	return &mcp.CallToolResult{
		Content: mcpContents,
		IsError: false,
	}
}

// DecodeResult 将 MCP 工具调用结果解码为指定的结构体
// 封装 k8s-mcp 提供的 DecodeResult 方法，支持自定义 CallToolResult 类型
// 如果解码失败，会返回错误信息
func DecodeResult[T any](result *CallToolResult) (*T, error) {
	// 将自定义类型转换为 mcp.CallToolResult
	mcpResult := CallToolResultToMcp(result)
	if mcpResult == nil {
		return nil, fmt.Errorf("result is nil")
	}

	// 调用 k8s-mcp 的 DecodeResult 方法
	return mcpclient.DecodeResult[T](mcpResult)
}

// ParseToolResult 解析工具调用结果为指定类型
// 优先使用 k8s-mcp 的 DecodeResult 方法进行解析
// 支持嵌套 JSON 格式: {"resource_name": "[..."}
func ParseToolResult[T any](result *CallToolResult, toolName string) (T, error) {
	var zero T

	if result == nil || len(result.Content) == 0 {
		return zero, fmt.Errorf("empty result")
	}

	// 第一步：优先尝试使用 DecodeResult 解析
	decoded, err := DecodeResult[T](result)
	if err == nil && decoded != nil {
		return *decoded, nil
	}

	// 第二步：手动解析，提取文本内容
	textData, err := extractTextContent(result.Content[0])
	if err != nil {
		return zero, fmt.Errorf("failed to extract text content: %w", err)
	}

	// 去除可能存在的空白字符
	textData = strings.TrimSpace(textData)

	// 尝试直接解析为目标类型
	var value T
	if err := json.Unmarshal([]byte(textData), &value); err == nil {
		return value, nil
	}

	// 如果直接解析失败，尝试解析嵌套 JSON 格式
	// 例如: {"pods": "[...]" 或 {"pods": {...}}
	toolKey := resourceKeyMapping[toolName]
	if toolKey == "" {
		// 如果没有匹配的映射，尝试从 JSON 内容中提取键名
		toolKey = extractResourceKey(textData)
	}

	if toolKey != "" {
		// 尝试使用指定的键名解析
		if parsed, ok := parseNestedJSON[T](textData, toolKey); ok {
			return parsed, nil
		}
	}

	// 增强容错性：如果仍然无法解析，遍历 JSON 对象的所有键
	// 找到第一个值是数组的键进行解析
	if parsed, ok := parseNestedJSONWithFallback[T](textData); ok {
		return parsed, nil
	}

	// 所有解析尝试都失败
	return zero, fmt.Errorf("failed to parse result for tool %s: text content: %.200s", toolName, textData)
}

// extractTextContent 从 Content 中提取文本内容
// 支持 string 和 *mcp.TextContent 两种类型
func extractTextContent(content Content) (string, error) {
	switch v := content.(type) {
	case string:
		return v, nil
	case *mcp.TextContent:
		if v != nil {
			return v.Text, nil
		}
		return "", fmt.Errorf("TextContent is nil")
	default:
		return "", fmt.Errorf("unsupported content type: %T", content)
	}
}

// parseNestedJSON 尝试使用指定的键名解析嵌套 JSON
func parseNestedJSON[T any](textData, toolKey string) (T, bool) {
	var zero T

	// 解析外层 JSON 获取嵌套的内容
	var outer map[string]json.RawMessage
	if err := json.Unmarshal([]byte(textData), &outer); err != nil {
		return zero, false
	}

	if innerData, ok := outer[toolKey]; ok {
		// 尝试解析内层数据
		innerStr := string(innerData)
		// 去除可能的引号包裹（处理双重编码的情况）
		if strings.HasPrefix(innerStr, `"`) && strings.HasSuffix(innerStr, `"`) {
			var unquoted string
			if err := json.Unmarshal(innerData, &unquoted); err == nil {
				innerStr = unquoted
			}
		}

		if err := json.Unmarshal([]byte(innerStr), &zero); err == nil {

			return zero, true
		}
	}

	return zero, false
}

// parseNestedJSONWithFallback 遍历 JSON 对象的所有键，找到第一个值是数组的键进行解析
// 这是一个增强的容错机制，当无法确定正确的键名时使用
func parseNestedJSONWithFallback[T any](textData string) (T, bool) {
	var zero T

	// 解析外层 JSON 获取所有键
	var outer map[string]json.RawMessage
	if err := json.Unmarshal([]byte(textData), &outer); err != nil {
		return zero, false
	}

	// 遍历所有键，找到第一个值是数组的键
	for _, innerData := range outer {
		// 尝试解析内层数据
		innerStr := string(innerData)
		// 去除可能的引号包裹（处理双重编码的情况）
		if strings.HasPrefix(innerStr, `"`) && strings.HasSuffix(innerStr, `"`) {
			var unquoted string
			if err := json.Unmarshal(innerData, &unquoted); err == nil {
				innerStr = unquoted
			}
		}

		// 尝试解析为数组
		if err := json.Unmarshal([]byte(innerStr), &zero); err == nil {

			return zero, true
		}
	}

	return zero, false
}

// extractResourceKey 从 JSON 文本中提取可能的资源键名
// 优先从已知的资源键名映射中查找，其次遍历所有键找数组值
func extractResourceKey(textData string) string {
	// 首先尝试常见的资源键名精确匹配
	commonKeys := []string{"pods", "services", "deployments", "nodes", "events", "configmaps", "statefulsets", "namespaces", "persistentvolumeclaims", "secrets", "ingresses"}

	for _, key := range commonKeys {
		// 使用更精确的匹配：查找 "key": 格式
		pattern := `"` + key + `":`
		if strings.Contains(textData, pattern) {
			return key
		}
	}

	// 如果精确匹配失败，尝试更宽松的匹配
	for _, key := range commonKeys {
		if strings.Contains(textData, `"`+key+`"`) {
			return key
		}
	}

	// 最后尝试解析 JSON 对象，遍历所有键
	var outer map[string]json.RawMessage
	if err := json.Unmarshal([]byte(textData), &outer); err == nil {
		// 遍历所有键，找到第一个值是数组的键
		for key, value := range outer {
			// 检查值是否以数组开始
			trimmed := strings.TrimSpace(string(value))
			if strings.HasPrefix(trimmed, "[") {
				return key
			}
			// 也检查双重编码的情况
			if strings.HasPrefix(trimmed, `"[`) {
				return key
			}
		}
	}

	return ""
}

// ParseToolResultAsString 解析工具调用结果为字符串
func ParseToolResultAsString(result *CallToolResult) (string, error) {
	if result == nil || len(result.Content) == 0 {
		return "", fmt.Errorf("empty result")
	}

	// 使用 extractTextContent 支持多种内容类型
	return extractTextContent(result.Content[0])
}

// 导出 k8s-mcp 包中的类型，供其他模块使用
type (
	// Pod Pod 信息
	Pod = types.Pod
	// Service Service 信息
	Service = types.Service
	// Deployment Deployment 信息
	Deployment = types.Deployment
	// Node 节点信息
	Node = types.Node
	// Event 事件信息
	Event = types.Event
	// ConfigMap ConfigMap 信息
	ConfigMap = types.ConfigMap
	// StatefulSet StatefulSet 信息
	StatefulSet = types.StatefulSet
	// RBACPermission RBAC 权限检查结果
	RBACPermission = types.RBACPermission
	// PodLogOptions Pod 日志选项
	PodLogOptions = types.PodLogOptions
	// Namespace 命名空间信息
	Namespace = types.Namespace
)
