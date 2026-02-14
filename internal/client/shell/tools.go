// Package shell 提供 Shell Executor MCP 工具的便捷封装方法
package shell

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ErrKubectlBlocked kubectl 命令被阻止的错误
var ErrKubectlBlocked = fmt.Errorf("kubectl commands are not allowed via Shell MCP; please use K8s MCP instead")

// isKubectlCommand 检查命令是否为 kubectl 命令
func isKubectlCommand(command string) bool {
	// 转换为小写进行检测
	lowerCmd := strings.ToLower(strings.TrimSpace(command))

	// 检测各种 kubectl 变体
	kubectlVariants := []string{
		"kubectl",
		"kubectl.exe",
		"k8s",
	}

	for _, variant := range kubectlVariants {
		// 检查命令是否以 kubectl 变体开头
		if strings.HasPrefix(lowerCmd, variant+" ") || lowerCmd == variant {
			return true
		}
		// 检查命令中是否包含 kubectl（处理 sudo kubectl 等情况）
		if strings.Contains(lowerCmd, " "+variant+" ") || strings.HasPrefix(lowerCmd, variant+" ") {
			return true
		}
	}

	return false
}

// ExecuteResult 命令执行结果
type ExecuteResult struct {
	// Summary 执行摘要
	Summary string `json:"summary"`

	// Groups 按状态分组的结果
	Groups []ExecuteGroup `json:"groups"`
}

// ExecuteGroup 执行分组结果
type ExecuteGroup struct {
	// Count 该组中的节点数量
	Count int `json:"count"`

	// Status 执行状态（success/failed）
	Status string `json:"status"`

	// Output 命令输出
	Output string `json:"output"`

	// Nodes 该组包含的节点列表
	Nodes []string `json:"nodes"`
}

// ExecuteCommand 在服务器集群上执行 Shell 命令
func (c *Client) ExecuteCommand(ctx context.Context, command string) (*ExecuteResult, error) {
	if command == "" {
		return nil, fmt.Errorf("command is required")
	}

	// 检查是否为 kubectl 命令
	if isKubectlCommand(command) {
		fmt.Printf("[Shell MCP] 拒绝执行 kubectl 命令: %s\n", command)
		return nil, ErrKubectlBlocked
	}

	result, err := c.CallTool(ctx, "execute_command", map[string]interface{}{
		"command": command,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to execute command: %w", err)
	}

	if len(result.Content) == 0 {
		return nil, fmt.Errorf("empty response")
	}

	var execResult ExecuteResult
	if textContent, ok := result.Content[0].(*mcp.TextContent); ok {
		if err := json.Unmarshal([]byte(textContent.Text), &execResult); err != nil {
			return nil, fmt.Errorf("failed to parse execute result: %w", err)
		}
	} else {
		return nil, fmt.Errorf("unexpected content type")
	}

	return &execResult, nil
}

// ExecuteCommandWithTimeout 在服务器集群上执行 Shell 命令（带超时）
func (c *Client) ExecuteCommandWithTimeout(ctx context.Context, command string, timeout int) (*ExecuteResult, error) {
	if command == "" {
		return nil, fmt.Errorf("command is required")
	}

	// 检查是否为 kubectl 命令
	if isKubectlCommand(command) {
		fmt.Printf("[Shell MCP] 拒绝执行 kubectl 命令: %s\n", command)
		return nil, ErrKubectlBlocked
	}

	args := map[string]interface{}{
		"command": command,
	}

	if timeout > 0 {
		args["timeout"] = timeout
	}

	result, err := c.CallTool(ctx, "execute_command", args)
	if err != nil {
		return nil, fmt.Errorf("failed to execute command: %w", err)
	}

	if len(result.Content) == 0 {
		return nil, fmt.Errorf("empty response")
	}

	var execResult ExecuteResult
	if textContent, ok := result.Content[0].(*mcp.TextContent); ok {
		if err := json.Unmarshal([]byte(textContent.Text), &execResult); err != nil {
			return nil, fmt.Errorf("failed to parse execute result: %w", err)
		}
	} else {
		return nil, fmt.Errorf("unexpected content type")
	}

	return &execResult, nil
}

// GetSuccessfulNodes 获取执行成功的节点列表
func (r *ExecuteResult) GetSuccessfulNodes() []string {
	var nodes []string
	for _, group := range r.Groups {
		if group.Status == "success" {
			nodes = append(nodes, group.Nodes...)
		}
	}
	return nodes
}

// GetFailedNodes 获取执行失败的节点列表
func (r *ExecuteResult) GetFailedNodes() []string {
	var nodes []string
	for _, group := range r.Groups {
		if group.Status == "failed" {
			nodes = append(nodes, group.Nodes...)
		}
	}
	return nodes
}

// GetSuccessOutput 获取成功节点的输出
func (r *ExecuteResult) GetSuccessOutput() []string {
	var outputs []string
	for _, group := range r.Groups {
		if group.Status == "success" {
			outputs = append(outputs, group.Output)
		}
	}
	return outputs
}

// GetFailureOutput 获取失败节点的错误输出
func (r *ExecuteResult) GetFailureOutput() []string {
	var outputs []string
	for _, group := range r.Groups {
		if group.Status == "failed" {
			outputs = append(outputs, group.Output)
		}
	}
	return outputs
}

// IsAllSuccess 检查是否所有节点都执行成功
func (r *ExecuteResult) IsAllSuccess() bool {
	for _, group := range r.Groups {
		if group.Status != "success" {
			return false
		}
	}
	return true
}

// IsAnySuccess 检查是否有节点执行成功
func (r *ExecuteResult) IsAnySuccess() bool {
	for _, group := range r.Groups {
		if group.Status == "success" {
			return true
		}
	}
	return false
}

// GetTotalNodes 获取总节点数
func (r *ExecuteResult) GetTotalNodes() int {
	total := 0
	for _, group := range r.Groups {
		total += group.Count
	}
	return total
}

// GetSuccessCount 获取成功节点数
func (r *ExecuteResult) GetSuccessCount() int {
	count := 0
	for _, group := range r.Groups {
		if group.Status == "success" {
			count += group.Count
		}
	}
	return count
}

// GetFailureCount 获取失败节点数
func (r *ExecuteResult) GetFailureCount() int {
	count := 0
	for _, group := range r.Groups {
		if group.Status == "failed" {
			count += group.Count
		}
	}
	return count
}

// FormatSummary 格式化执行摘要
func (r *ExecuteResult) FormatSummary() string {
	if r.Summary != "" {
		return r.Summary
	}

	total := r.GetTotalNodes()
	success := r.GetSuccessCount()
	failure := r.GetFailureCount()

	return fmt.Sprintf("Executed on %d nodes: %d success, %d failed", total, success, failure)
}

// ParseToolResult 解析工具调用结果为指定类型
func ParseToolResult[T any](result *mcp.CallToolResult) (T, error) {
	var zero T

	if result == nil || len(result.Content) == 0 {
		return zero, fmt.Errorf("empty result")
	}

	var value T
	if textContent, ok := result.Content[0].(*mcp.TextContent); ok {
		if err := json.Unmarshal([]byte(textContent.Text), &value); err != nil {
			return zero, fmt.Errorf("failed to parse result: %w", err)
		}
	} else {
		return zero, fmt.Errorf("unexpected content type")
	}

	return value, nil
}
