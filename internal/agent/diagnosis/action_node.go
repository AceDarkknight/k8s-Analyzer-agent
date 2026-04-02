package diagnosis

import (
	"context"
	"fmt"
	"strings"
	"sync"

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
	"get_pod_logs":     {"logs", ""},
	"list_events":      {"get", "events"},
	"get_pod_events":   {"get", "events"}, // 专门获取某个 Pod 的事件
	"list_deployments": {"get", "deployments"},
	"list_services":    {"get", "services"},
	"get_nodes":        {"get", "nodes"},
	"list_namespaces":  {"get", "namespaces"},
	"list_pvc":         {"get", "pvc"}, // 检查 PVC 绑定状态
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
	case "execute_plan":
		// execute_plan 模式：从 Plan 中提取工具调用执行
		return n.executePlan(ctx, s, decision)
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

	// 并发执行所有 ToolCalls
	var wg sync.WaitGroup
	results := make([]string, len(decision.ToolCalls))
	var mu sync.Mutex

	for i, tc := range decision.ToolCalls {
		wg.Add(1)
		go func(index int, toolCall state.ToolCall) {
			defer wg.Done()

			obs, err := n.executeToolCall(ctx, s, toolCall)
			if err != nil {
				logger.Error("ActionNode: tool call failed",
					logger.String("tool", toolCall.Name),
					logger.Err(err))
				obs = fmt.Sprintf("工具 %s 执行失败: %v", toolCall.Name, err)
			}

			mu.Lock()
			results[index] = fmt.Sprintf("[%s]\n%s", toolCall.Name, obs)
			mu.Unlock()
		}(i, tc)
	}
	wg.Wait()

	// 合并所有观察结果
	mergedObservation := strings.Join(results, "\n\n")

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

// executePlan 执行计划模式
func (n *ActionNode) executePlan(ctx context.Context, s *state.State, decision *DecisionOutput) (*state.State, error) {
	// 优先使用直接传入的 ToolCalls（Graph 执行单步时）
	// 否则从完整 Plan 中提取
	var allToolCalls []state.ToolCall
	if len(decision.ToolCalls) > 0 {
		allToolCalls = decision.ToolCalls
	} else {
		for _, step := range decision.Plan {
			allToolCalls = append(allToolCalls, step.ToolCalls...)
		}
	}

	if len(allToolCalls) == 0 {
		logger.Warn("ActionNode: no tool calls in execute_plan mode")
		return s, nil
	}

	logger.Info("ActionNode: executing plan",
		logger.Int("steps", len(decision.Plan)),
		logger.Int("total_tool_calls", len(allToolCalls)))

	// 并发执行所有 ToolCalls
	var wg sync.WaitGroup
	results := make([]string, len(allToolCalls))
	var mu sync.Mutex

	for i, tc := range allToolCalls {
		wg.Add(1)
		go func(index int, toolCall state.ToolCall) {
			defer wg.Done()

			obs, err := n.executeToolCall(ctx, s, toolCall)
			if err != nil {
				logger.Error("ActionNode: tool call failed",
					logger.String("tool", toolCall.Name),
					logger.Err(err))
				obs = fmt.Sprintf("工具 %s 执行失败: %v", toolCall.Name, err)
			}

			mu.Lock()
			results[index] = fmt.Sprintf("[%s]\n%s", toolCall.Name, obs)
			mu.Unlock()
		}(i, tc)
	}
	wg.Wait()

	// 合并所有观察结果
	mergedObservation := strings.Join(results, "\n\n")

	// 更新最后一个 ReasoningStep 的 Observation
	history := s.GetReasoningHistory()
	if len(history) > 0 {
		lastStep := &history[len(history)-1]
		lastStep.Observation = mergedObservation
	}

	logger.Info("ActionNode: execute_plan mode completed",
		logger.Int("tool_calls", len(allToolCalls)))

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
	if fieldSelector, ok := tc.Args["fieldSelector"].(string); ok && fieldSelector != "" {
		req.Options.FieldSelector = fieldSelector
	}
	if container, ok := tc.Args["container"].(string); ok && container != "" {
		req.Options.Container = container
	}
	if tailLines, ok := tc.Args["tailLines"].(float64); ok && tailLines > 0 {
		req.Options.TailLines = int(tailLines)
	}

	// 特殊处理：get_pod_events 自动构建 fieldSelector
	if tc.Name == "get_pod_events" {
		if podName, ok := tc.Args["podName"].(string); ok && podName != "" {
			req.Options.FieldSelector = fmt.Sprintf("involvedObject.name=%s", podName)
		}
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

	summary := n.summarizer.Summarize(output)

	// 记录命令执行（构建命令字符串用于显示）
	cmdStr := fmt.Sprintf("kubectl %s %s", mapping.Verb, mapping.Resource)
	if req.Namespace != "" {
		cmdStr = fmt.Sprintf("kubectl -n %s %s %s", req.Namespace, mapping.Verb, mapping.Resource)
	}
	if req.Name != "" {
		cmdStr += " " + req.Name
	}
	s.AddCommandExecution(cmdStr, resp.Status == "success", summary, s.VerifyPhase)

	return summary, nil
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
	s.AddCommandExecution(command, result.ExitCode == 0, result.Stdout, s.VerifyPhase)

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
