package diagnosis

import (
	"context"
	"fmt"
	"strings"

	"github.com/AceDarkknight/k8s-analyzer-agent/internal/agent/safety"
	"github.com/AceDarkknight/k8s-analyzer-agent/internal/client/gateway"
	"github.com/AceDarkknight/k8s-analyzer-agent/internal/llm"
	"github.com/AceDarkknight/k8s-analyzer-agent/internal/logger"
	"github.com/AceDarkknight/k8s-analyzer-agent/internal/state"
	"github.com/AceDarkknight/k8s-analyzer-agent/internal/summarizer"
)

// ActionNode 执行节点
type ActionNode struct {
	gateway    *gateway.GatewayClient
	safety     *safety.SafetyAgent
	reactLLM   *llm.ReActLLM
	summarizer *summarizer.OutputSummarizer
}

// toolMapping 工具名到 Gateway 请求映射
var toolMapping = map[string]struct {
	Verb     string
	Resource string
}{
	"list_pods":        {"get", "pods"},
	"describe_pod":     {"describe", "pod"},
	"get_pod_logs":     {"logs", "pod"},
	"list_events":      {"get", "events"},
	"list_deployments": {"get", "deployments"},
	"list_services":    {"get", "services"},
	"get_nodes":        {"get", "nodes"},
	"list_namespaces":  {"get", "namespaces"},
}

// NewActionNode 创建新的执行节点
func NewActionNode(
	gw *gateway.GatewayClient,
	sa *safety.SafetyAgent,
	react *llm.ReActLLM,
	sum *summarizer.OutputSummarizer,
) *ActionNode {
	return &ActionNode{
		gateway:    gw,
		safety:     sa,
		reactLLM:   react,
		summarizer: sum,
	}
}

// Execute 执行动作
func (n *ActionNode) Execute(ctx context.Context, s *state.State, decision *DecisionOutput) (*state.State, error) {
	logger.Info("ActionNode: executing action",
		logger.String("decision", decision.Decision))

	switch decision.Decision {
	case "continue":
		return n.executeContinue(ctx, s, decision)
	case "deep_query":
		return n.executeDeepQuery(ctx, s, decision)
	case "report":
		// report 模式下不执行任何动作
		logger.Info("ActionNode: report decision, no action needed")
		return s, nil
	default:
		logger.Warn("ActionNode: unknown decision", logger.String("decision", decision.Decision))
		return s, nil
	}
}

// executeContinue 执行 continue 模式
func (n *ActionNode) executeContinue(ctx context.Context, s *state.State, decision *DecisionOutput) (*state.State, error) {
	if len(decision.ToolCalls) == 0 {
		logger.Warn("ActionNode: no tool calls in continue mode")
		return s, nil
	}

	var observations []string

	// 遍历 ToolCalls
	for _, tc := range decision.ToolCalls {
		obs, err := n.executeToolCall(ctx, s, tc)
		if err != nil {
			logger.Error("ActionNode: tool call failed",
				logger.String("tool", tc.Name),
				logger.Err(err))
			obs = fmt.Sprintf("工具 %s 执行失败: %v", tc.Name, err)
		}
		observations = append(observations, fmt.Sprintf("[%s]\n%s", tc.Name, obs))
	}

	// 合并所有观察结果
	mergedObservation := strings.Join(observations, "\n\n")

	// 更新最后一个 ReasoningStep 的 Observation
	history := s.GetReasoningHistory()
	if len(history) > 0 {
		lastStep := &history[len(history)-1]
		lastStep.Observation = mergedObservation
	}

	logger.Info("ActionNode: continue mode completed",
		logger.Int("tool_calls", len(decision.ToolCalls)))

	return s, nil
}

// executeToolCall 执行单个工具调用
func (n *ActionNode) executeToolCall(ctx context.Context, s *state.State, tc state.ToolCall) (string, error) {
	logger.Info("ActionNode: executing tool call",
		logger.String("tool", tc.Name))

	// 处理 execute_safe_command 特殊工具
	if tc.Name == "execute_safe_command" {
		return n.executeSafeCommand(ctx, s, tc)
	}

	// 查找工具映射
	mapping, ok := toolMapping[tc.Name]
	if !ok {
		return "", fmt.Errorf("unknown tool: %s", tc.Name)
	}

	// 构建 Gateway 请求
	req := &gateway.KubectlRequest{
		Verb:     mapping.Verb,
		Resource: mapping.Resource,
		Output:   "json",
		Mode:     "structured",
	}

	// 从 Args 中提取参数
	if ns, ok := tc.Args["namespace"].(string); ok && ns != "" {
		req.Namespace = ns
	}
	if name, ok := tc.Args["name"].(string); ok && name != "" {
		req.Name = name
	}

	// 处理 Options
	req.Options = &gateway.KubectlOptions{}
	if labelSelector, ok := tc.Args["labelSelector"].(string); ok && labelSelector != "" {
		req.Options.LabelSelector = labelSelector
	}
	if container, ok := tc.Args["container"].(string); ok && container != "" {
		req.Options.Container = container
	}
	if tailLines, ok := tc.Args["tailLines"].(float64); ok && tailLines > 0 {
		req.Options.TailLines = int(tailLines)
	}

	// 调用 Gateway
	resp, err := n.gateway.Execute(ctx, req)
	if err != nil {
		return "", err
	}

	// 使用 summarizer 摘要输出
	output := resp.Stdout
	if output == "" {
		output = resp.Stderr
	}

	return n.summarizer.Summarize(output), nil
}

// executeSafeCommand 执行安全命令
func (n *ActionNode) executeSafeCommand(ctx context.Context, s *state.State, tc state.ToolCall) (string, error) {
	command, _ := tc.Args["command"].(string)
	reason, _ := tc.Args["reason"].(string)

	if command == "" {
		return "", fmt.Errorf("command is empty")
	}

	req := &safety.CommandRequest{
		Command:   command,
		Reason:    reason,
		Source:    "ActionNode",
		Iteration: s.GetIterationCount(),
	}

	result, err := n.safety.ExecuteSafeCommand(ctx, req)
	if err != nil {
		return "", err
	}

	// 检查是否被安全审计拒绝
	if !result.AuditInfo.Allowed {
		// 记录被阻止的命令
		blockedCmd := state.BlockedCommand{
			Command: command,
			Reason:  result.AuditInfo.Reason,
			Advice:  result.AuditInfo.Advice,
		}
		s.AddBlockedCommand(blockedCmd)

		return fmt.Sprintf("命令被安全审计拒绝。原因: %s。建议: %s",
			result.AuditInfo.Reason, result.AuditInfo.Advice), nil
	}

	// 记录命令执行
	exec := state.CommandExecution{
		Command: command,
		Success: result.ExitCode == 0,
		Output:  result.Stdout,
	}
	s.AddCommandExecution(exec)

	// 摘要输出
	output := result.Stdout
	if output == "" {
		output = result.Stderr
	}

	return n.summarizer.Summarize(output), nil
}

// executeDeepQuery 执行 deep_query 模式
func (n *ActionNode) executeDeepQuery(ctx context.Context, s *state.State, decision *DecisionOutput) (*state.State, error) {
	if decision.DeepQueryTopic == "" {
		logger.Warn("ActionNode: deep_query mode but no topic provided")
		return s, nil
	}

	logger.Info("ActionNode: starting deep query",
		logger.String("topic", decision.DeepQueryTopic))

	// 调用 ReAct LLM 进行深度调查
	result, err := n.reactLLM.DeepQuery(ctx, decision.DeepQueryTopic, s)
	if err != nil {
		logger.Error("ActionNode: deep query failed", logger.Err(err))
		result = fmt.Sprintf("深度调查执行失败: %v", err)
	}

	// 更新最后一个 ReasoningStep 的 Observation
	history := s.GetReasoningHistory()
	if len(history) > 0 {
		lastStep := &history[len(history)-1]
		lastStep.Observation = result
	}

	logger.Info("ActionNode: deep query completed")

	return s, nil
}
