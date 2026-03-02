// Package analysis 提供 ReAct Agent 的工具适配器实现
// 将 K8sClient 和 SafetyAgent 适配为 Eino 兼容的工具接口
package analysis

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/AceDarkknight/k8s-analyzer-agent/internal/client/k8s"
	"github.com/AceDarkknight/k8s-analyzer-agent/internal/logger"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/eino-contrib/jsonschema"
)

// WrapK8sTools 将 MCP 工具转换为 Eino 工具
// 注意：如果从 MCP 列出工具失败，应该直接退出程序（Fatal 错误）
func WrapK8sTools(ctx context.Context, k8sClient K8sClient) ([]tool.BaseTool, error) {
	// 1. 从 MCP 列出工具
	mcpTools, err := k8sClient.ListTools(ctx)
	if err != nil {
		logger.Fatal("[WrapK8sTools] Failed to list tools from K8s MCP Server", logger.Err(err))
		return nil, fmt.Errorf("failed to list tools from K8s MCP Server: %w", err)
	}

	// 2. 转换为 tool.BaseTool
	tools := make([]tool.BaseTool, 0, len(mcpTools))
	for _, t := range mcpTools {
		tools = append(tools, &K8sToolAdapter{
			name:        t.Name,
			desc:        t.Description,
			inputSchema: t.InputSchema,
			k8sClient:   k8sClient,
		})
	}

	logger.Info("[WrapK8sTools] K8s tools wrapped successfully",
		logger.Int("count", len(tools)))

	return tools, nil
}

// K8sToolAdapter K8s 工具适配器
// 将 K8s MCP 工具包装为 Eino tool.BaseTool 接口
type K8sToolAdapter struct {
	name        string
	desc        string
	inputSchema json.RawMessage
	k8sClient   K8sClient
}

// Info 返回工具信息
func (t *K8sToolAdapter) Info(ctx context.Context) (*schema.ToolInfo, error) {
	var paramsOneOf *schema.ParamsOneOf

	// 解析 JSON Schema 字符串为 *jsonschema.Schema
	if len(t.inputSchema) > 0 {
		var js *jsonschema.Schema
		if err := json.Unmarshal(t.inputSchema, &js); err != nil {
			logger.Warn("[K8sToolAdapter] Failed to parse input schema JSON",
				logger.String("tool", t.name),
				logger.Err(err))
		} else {
			paramsOneOf = schema.NewParamsOneOfByJSONSchema(js)
		}
	}

	return &schema.ToolInfo{
		Name:        t.name,
		Desc:        t.desc,
		ParamsOneOf: paramsOneOf,
	}, nil
}

// InvokableRun 执行工具调用
func (t *K8sToolAdapter) InvokableRun(ctx context.Context, argsJSON string, opts ...tool.Option) (string, error) {
	// 解析参数
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("failed to unmarshal arguments: %w", err)
	}

	logger.Info("[K8sToolAdapter] Executing tool",
		logger.String("tool", t.name),
		logger.Any("args", args))

	// 调用实际的 MCP 工具
	result, err := t.k8sClient.CallTool(ctx, t.name, args)
	if err != nil {
		logger.Error("[K8sToolAdapter] Tool execution failed",
			logger.String("tool", t.name),
			logger.Err(err))
		return "", err
	}

	// 处理结果 - 将 result.Content 转换为字符串返回
	output, err := convertToolResultToString(result)
	if err != nil {
		logger.Warn("[K8sToolAdapter] Failed to convert result to string",
			logger.String("tool", t.name),
			logger.Err(err))
		return "", err
	}

	logger.Info("[K8sToolAdapter] Tool executed successfully",
		logger.String("tool", t.name),
		logger.Int("output_length", len(output)))

	return output, nil
}

// WrapSafetyAgent 将 SafetyAgent 包装为 Eino 工具
// 注意：SafetyAgent 本身不需要修改，只需要通过适配器包装即可
func WrapSafetyAgent(safetyAgent SafetyAgent) tool.BaseTool {
	return &SafetyAgentToolAdapter{
		safetyAgent: safetyAgent,
	}
}

// SafetyAgentToolAdapter SafetyAgent 工具适配器
// 将 SafetyAgent 包装为 Eino tool.BaseTool 接口
type SafetyAgentToolAdapter struct {
	safetyAgent SafetyAgent
}

// Info 返回工具信息
func (t *SafetyAgentToolAdapter) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name:        "execute_safe_command",
		Desc:        "执行安全的 Shell 命令（通过 SafetyAgent 验证）。该工具会先验证命令的安全性，然后执行命令。参数：command (string, required) - 要执行的Shell命令; reason (string, required) - 执行该命令的原因（用于审计）",
		ParamsOneOf: nil,
	}, nil
}

// InvokableRun 执行工具调用
func (t *SafetyAgentToolAdapter) InvokableRun(ctx context.Context, argsJSON string, opts ...tool.Option) (string, error) {
	// 解析参数
	var args struct {
		Command string `json:"command"`
		Reason  string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("failed to unmarshal arguments: %w", err)
	}

	logger.Info("[SafetyAgentToolAdapter] Executing safe command",
		logger.String("command", args.Command),
		logger.String("reason", args.Reason))

	// 构造上下文信息用于审计
	contextInfo := map[string]interface{}{
		"reason": args.Reason,
		"source": "ReActAgent",
	}

	// 直接调用 SafetyAgent 的 ExecuteSafeCommandWithAudit 方法
	// 该方法内部会处理验证和审计逻辑
	output, err := t.safetyAgent.ExecuteSafeCommandWithAudit(ctx, args.Command, contextInfo)
	if err != nil {
		logger.Warn("[SafetyAgentToolAdapter] Command execution returned error",
			logger.String("command", args.Command),
			logger.Err(err))
		return fmt.Sprintf("执行失败: %s", err.Error()), nil
	}

	logger.Info("[SafetyAgentToolAdapter] Command executed successfully",
		logger.String("command", args.Command),
		logger.Int("output_length", len(output)))

	return output, nil
}

// convertToolResultToString 将工具调用结果转换为字符串
func convertToolResultToString(result *k8s.CallToolResult) (string, error) {
	if result == nil {
		return "", nil
	}

	// 尝试解析 content 内容
	if len(result.Content) > 0 {
		var sb strings.Builder
		for _, c := range result.Content {
			sb.WriteString(fmt.Sprintf("%v", c))
			sb.WriteString("\n")
		}
		return sb.String(), nil
	}

	return "", nil
}
