// Package client 提供了与 MCP Server 通信的通用接口和基础实现
package client

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Tool 定义 MCP 工具的统一描述信息
// 用于在 LLM Prompt 中注入可用工具列表
// 该结构体被所有 Client（K8s, Shell）和 Agent 共享使用
type Tool struct {
	// Name 工具名称
	Name string `json:"name"`

	// Description 工具描述
	Description string `json:"description"`

	// InputSchema 工具的输入参数 Schema (JSON Schema 格式)
	// 使用 json.RawMessage 存储预序列化的 JSON，避免频繁的序列化开销
	InputSchema json.RawMessage `json:"input_schema"`
}

// MCPClient 定义了与 MCP Server 交互的核心接口
type MCPClient interface {
	// Connect 建立与 MCP Server 的连接
	// 实现需要处理传输层的初始化（如 SSE、Stdio）
	Connect(ctx context.Context) error

	// Close 终止与 MCP Server 的连接
	Close() error

	// CallTool 执行 MCP Server 上的特定工具
	// name: 工具名称（如 "get_pod_logs", "execute_command"）
	// args: 工具参数的键值对
	CallTool(ctx context.Context, name string, args map[string]interface{}) (*mcp.CallToolResult, error)

	// ListTools 获取 Server 上可用的工具列表
	ListTools(ctx context.Context) ([]mcp.Tool, error)
}

// ToolNotFoundError 表示请求的工具不存在
type ToolNotFoundError struct {
	ToolName string
}

func (e *ToolNotFoundError) Error() string {
	return fmt.Sprintf("tool '%s' not found", e.ToolName)
}

// ConnectionError 表示连接失败
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

// AuthenticationError 表示认证失败
type AuthenticationError struct {
	Reason string
}

func (e *AuthenticationError) Error() string {
	return fmt.Sprintf("authentication failed: %s", e.Reason)
}

// ToolExecutionError 表示工具执行失败
type ToolExecutionError struct {
	ToolName string
	Reason   string
	Err      error
}

func (e *ToolExecutionError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("tool '%s' execution failed: %s: %v", e.ToolName, e.Reason, e.Err)
	}
	return fmt.Sprintf("tool '%s' execution failed: %s", e.ToolName, e.Reason)
}

// Unwrap 返回底层错误
func (e *ToolExecutionError) Unwrap() error {
	return e.Err
}

// IsRetryableError 判断错误是否可重试
func IsRetryableError(err error) bool {
	if err == nil {
		return false
	}

	// 检查是否为连接错误（可重试）
	if _, ok := err.(*ConnectionError); ok {
		return true
	}

	// 检查是否为超时错误（可重试）
	if err == context.DeadlineExceeded {
		return true
	}

	return false
}
