// Package analysis 提供 Graph 节点实现
package analysis

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/AceDarkknight/k8s-analyzer-agent/internal/client"
	"github.com/AceDarkknight/k8s-analyzer-agent/internal/client/k8s"
	"github.com/AceDarkknight/k8s-analyzer-agent/internal/logger"
)

// K8sClient K8s 客户端接口
type K8sClient interface {
	CallTool(ctx context.Context, name string, args map[string]interface{}) (*k8s.CallToolResult, error)
	ListTools(ctx context.Context) ([]client.Tool, error)
}

// SafetyAgent 安全 Agent 接口
type SafetyAgent interface {
	ExecuteSafeCommand(ctx context.Context, command string) (string, error)
	ExecuteSafeCommandWithAudit(ctx context.Context, command string, contextInfo map[string]interface{}) (string, error)
}

// InfoNode 信息收集节点
// 调用 K8s Client 获取集群/资源信息
type InfoNode struct {
	k8sClient      K8sClient
	availableTools map[string]bool // 缓存可用工具列表
}

// NewInfoNode 创建新的 InfoNode
func NewInfoNode(k8sClient K8sClient) *InfoNode {
	return &InfoNode{
		k8sClient:      k8sClient,
		availableTools: make(map[string]bool),
	}
}

// hasTool 检查工具是否可用
func (n *InfoNode) hasTool(ctx context.Context, toolName string) bool {
	// 如果缓存为空，先获取工具列表
	if len(n.availableTools) == 0 {
		tools, err := n.k8sClient.ListTools(ctx)
		if err != nil {
			logger.Warn("[InfoNode] Failed to list tools from MCP server", logger.Err(err))
			return false
		}
		for _, tool := range tools {
			n.availableTools[tool.Name] = true
		}
		logger.Debug("[InfoNode] Cached available tools", logger.Int("count", len(n.availableTools)))
	}
	return n.availableTools[toolName]
}

// Execute 执行信息收集
// 首先获取所有命名空间，然后遍历每个命名空间收集资源
func (n *InfoNode) Execute(ctx context.Context, state *State) (*State, error) {
	logger.Info("[InfoNode] Starting information collection", logger.String("userInput", state.UserInput))

	// 步骤 1: 获取所有命名空间
	namespaces, usedFallback, err := n.collectNamespaces(ctx)
	if err != nil {
		logger.Warn("[InfoNode] Failed to list namespaces, falling back to default", logger.Err(err))
		// 回退到 default 命名空间，但不设置 LastError，因为这只是一个警告
		namespaces = []string{"default"}
		usedFallback = true
	}

	// 如果使用了回退列表，添加发现
	if usedFallback {
		state.AddFinding("Warning", "Cluster", "无法获取完整命名空间列表，已回退至部分扫描模式（仅检查 default 和 kube-system）。请检查 K8s MCP Server 连接状态。")
	}

	// 检查用户是否指定了特定命名空间
	specifiedNamespace := n.extractNamespace(state.UserInput)
	if specifiedNamespace != "" && specifiedNamespace != "default" {
		// 用户指定了特定命名空间，只收集该命名空间
		logger.Info("[InfoNode] User specified namespace, collecting from single namespace",
			logger.String("namespace", specifiedNamespace))
		namespaces = []string{specifiedNamespace}
	}

	logger.Info("[InfoNode] Collecting resources from namespaces",
		logger.Any("namespaces", namespaces),
		logger.Int("count", len(namespaces)))

	// 记录所有命名空间到状态
	state.K8sInfo.Namespace = strings.Join(namespaces, ", ")

	// 步骤 2: 遍历每个命名空间收集资源
	allPods := make([]PodInfo, 0)
	allDeployments := make([]DeploymentInfo, 0)
	// allEvents := make([]EventInfo, 0)

	for _, ns := range namespaces {
		logger.Debug("[InfoNode] Collecting resources from namespace", logger.String("namespace", ns))

		// 收集 Pod 信息
		pods, err := n.collectPods(ctx, ns)
		if err != nil {
			logger.Warn("[InfoNode] Failed to collect pods from namespace",
				logger.String("namespace", ns),
				logger.Err(err))
		} else {
			allPods = append(allPods, pods...)
		}

		// 收集 Deployment 信息
		deployments, err := n.collectDeployments(ctx, ns)
		if err != nil {
			logger.Warn("[InfoNode] Failed to collect deployments from namespace",
				logger.String("namespace", ns),
				logger.Err(err))
		} else {
			allDeployments = append(allDeployments, deployments...)
		}
	}

	// 更新状态
	// 将特定类型切片转换为 []any
	podsAny := make([]any, len(allPods))
	for i, pod := range allPods {
		podsAny[i] = pod
	}
	deploymentsAny := make([]any, len(allDeployments))
	for i, d := range allDeployments {
		deploymentsAny[i] = d
	}
	state.K8sInfo.SetResources("Pods", podsAny...)
	state.K8sInfo.SetResources("Deployments", deploymentsAny...)

	logger.Info("[InfoNode] Collected information from all namespaces",
		logger.Int("namespaces", len(namespaces)),
		logger.Int("pods", len(allPods)),
		logger.Int("deployments", len(allDeployments)),
	)

	return state, nil
}

// extractNamespace 从用户输入中提取命名空间
func (n *InfoNode) extractNamespace(input string) string {
	// 简单的命名空间提取逻辑
	// 查找 "namespace:" 或 "ns:" 模式
	lower := strings.ToLower(input)
	if strings.Contains(lower, "namespace:") {
		parts := strings.Split(lower, "namespace:")
		if len(parts) > 1 {
			ns := strings.TrimSpace(strings.Split(parts[1], " ")[0])
			if ns != "" {
				return ns
			}
		}
	}
	if strings.Contains(lower, "ns:") {
		parts := strings.Split(lower, "ns:")
		if len(parts) > 1 {
			ns := strings.TrimSpace(strings.Split(parts[1], " ")[0])
			if ns != "" {
				return ns
			}
		}
	}
	return "default" // 默认命名空间
}

// collectNamespaces 收集所有命名空间
// 返回值：命名空间列表和是否使用了回退列表
func (n *InfoNode) collectNamespaces(ctx context.Context) ([]string, bool, error) {
	// 先检查 list_namespaces 工具是否可用
	if !n.hasTool(ctx, "list_namespaces") {
		logger.Info("[InfoNode] list_namespaces tool not available on MCP server, using hardcoded fallback list")
		// 减少命名空间数量用于测试，只保留 default 和 kube-system
		return []string{"default", "kube-system"}, true, nil
	}

	// 调用 K8s MCP 工具获取命名空间列表
	result, err := n.k8sClient.CallTool(ctx, "list_namespaces", nil)
	if err != nil {
		logger.Warn("[InfoNode] list_namespaces tool call failed, using hardcoded fallback list", logger.Err(err))
		// K8s MCP 服务器不支持 list_namespaces，返回硬编码的常见命名空间列表
		return []string{"default", "kube-system"}, true, nil
	}

	// 解析结果
	namespaces := make([]string, 0)
	if result != nil {
		// 解析为 k8s-mcp 的 Namespace 类型切片
		k8sNamespaces, err := k8s.ParseToolResult[[]k8s.Namespace](result, "list_namespaces")
		if err != nil {
			logger.Warn("[InfoNode] Failed to parse namespaces result", logger.Err(err))
			return []string{"default", "kube-system"}, true, nil
		}
		// 转换为字符串切片
		for _, ns := range k8sNamespaces {
			namespaces = append(namespaces, ns.Name)
		}
	}

	// 如果没有获取到命名空间，使用硬编码列表作为回退
	if len(namespaces) == 0 {
		logger.Info("[InfoNode] No namespaces returned from MCP, using hardcoded fallback list")
		return []string{"default", "kube-system"}, true, nil
	}

	logger.Debug("[InfoNode] Collected namespaces", logger.Any("namespaces", namespaces))
	return namespaces, false, nil
}

// collectPods 收集 Pod 信息
func (n *InfoNode) collectPods(ctx context.Context, namespace string) ([]PodInfo, error) {
	// 调用 K8s MCP 工具获取 Pod 列表
	args := map[string]interface{}{
		"namespace": namespace,
	}

	result, err := n.k8sClient.CallTool(ctx, "list_pods", args)
	if err != nil {
		return nil, err
	}

	// 解析结果
	pods := make([]PodInfo, 0)
	if result != nil {
		// 解析为 k8s-mcp 的 Pod 类型切片
		k8sPods, err := k8s.ParseToolResult[[]k8s.Pod](result, "list_pods")
		if err != nil {
			logger.Warn("[InfoNode] Failed to parse pods result", logger.Err(err))
			return pods, nil
		}
		// 转换为内部 PodInfo 类型
		for _, p := range k8sPods {
			podInfo := PodInfo{
				Name:      p.Name,
				Namespace: p.Namespace,
				Status:    p.Status,
				Restarts:  int32(p.Restarts),
				Labels:    p.Labels,
			}
			pods = append(pods, podInfo)
		}
	}

	logger.Debug("[InfoNode] Collected pods from namespace", logger.String("namespace", namespace), logger.Int("count", len(pods)))
	return pods, nil
}

//collectServices 收集 Service 信息
//func (n *InfoNode) collectServices(ctx context.Context, namespace string) ([]ServiceInfo, error) {
//	args := map[string]interface{} {
//		"namespace": namespace,
//	}

//	result, err := n.k8sClient.CallTool(ctx, "list_services", args)
//	if err != nil {
//		return nil, err
//	}

//	services := make([]ServiceInfo, 0)
//	if result != nil {
//		// 解析为 k8s-mcp 的 Service 类型切片
//		k8sServices, err := k8s.ParseToolResult[[]k8s.Service](result, "list_services")
//		if err != nil {
//			logger.Warn("[InfoNode] Failed to parse services result", logger.Err(err))
//			return services, nil
//		}
//		// 转换为内部 ServiceInfo 类型
//		for _, s := range k8sServices {
//			serviceInfo := ServiceInfo{
//				Name:      s.Name,
//				Namespace: s.Namespace,
//				Type:      s.Type,
//				ClusterIP: s.ClusterIP,
//			}
//			services = append(services, serviceInfo)
//		}
//	}

//	logger.Debug("[InfoNode] Collected services from namespace", logger.String("namespace", namespace), logger.Int("count", len(services)))
//	return services, nil
// }

// collectDeployments 收集 Deployment 信息
func (n *InfoNode) collectDeployments(ctx context.Context, namespace string) ([]DeploymentInfo, error) {
	args := map[string]interface{}{
		"namespace": namespace,
	}

	result, err := n.k8sClient.CallTool(ctx, "list_deployments", args)
	if err != nil {
		return nil, err
	}

	deployments := make([]DeploymentInfo, 0)
	if result != nil {
		// 解析为 k8s-mcp 的 Deployment 类型切片
		k8sDeployments, err := k8s.ParseToolResult[[]k8s.Deployment](result, "list_deployments")
		if err != nil {
			logger.Warn("[InfoNode] Failed to parse deployments result", logger.Err(err))
			return deployments, nil
		}
		// 转换为内部 DeploymentInfo 类型
		for _, d := range k8sDeployments {
			deploymentInfo := DeploymentInfo{
				Name:      d.Name,
				Namespace: d.Namespace,
			}
			// 解析 Ready 字段格式 "x/y"
			if strings.Contains(d.Ready, "/") {
				parts := strings.Split(d.Ready, "/")
				if len(parts) == 2 {
					var ready, total int32
					fmt.Sscanf(parts[0], "%d", &ready)
					fmt.Sscanf(parts[1], "%d", &total)
					deploymentInfo.ReadyReplicas = ready
					deploymentInfo.Replicas = total
				}
			}
			deployments = append(deployments, deploymentInfo)
		}
	}

	logger.Debug("[InfoNode] Collected deployments from namespace", logger.String("namespace", namespace), logger.Int("count", len(deployments)))
	return deployments, nil
}

// filterExecutedCommands 过滤已执行的命令
// 过滤掉失败的命令（如 invalid params, unknown tool 等），并去重/折叠连续失败的相同工具调用
func filterExecutedCommands(commands []CommandExecution) []CommandExecution {
	// 定义需要过滤的错误关键词
	errorKeywords := []string{
		"invalid params",
		"unknown tool",
		"tool not found",
		"invalid arguments",
		"missing required parameter",
		"parameter error",
		"invalid tool",
	}

	// 第一步：过滤掉失败的命令（包含错误关键词）
	var filtered []CommandExecution
	for _, cmd := range commands {
		if cmd.Success {
			filtered = append(filtered, cmd)
			continue
		}

		// 检查输出是否包含错误关键词
		isErrorCmd := false
		outputLower := strings.ToLower(cmd.Output)
		for _, keyword := range errorKeywords {
			if strings.Contains(outputLower, strings.ToLower(keyword)) {
				isErrorCmd = true
				break
			}
		}

		// 如果不是错误命令，保留
		if !isErrorCmd {
			filtered = append(filtered, cmd)
		}
	}

	// 第二步：去重/折叠连续失败的相同工具调用
	// 我们只保留最后一个连续失败的工具调用（如果有多个连续失败的话）
	var result []CommandExecution
	var lastFailedTool string

	for _, cmd := range filtered {
		// 提取工具名称（取命令的第一个单词）
		parts := strings.Fields(cmd.Command)
		var toolName string
		if len(parts) > 0 {
			toolName = parts[0]
		} else {
			toolName = cmd.Command
		}

		// 检查是否是连续失败
		if !cmd.Success && toolName == lastFailedTool {
			// 连续失败，替换最后一个（跳过当前，保留最后一个）
			if len(result) > 0 {
				result[len(result)-1] = cmd
			}
		} else {
			// 不是连续失败，添加
			result = append(result, cmd)
		}

		// 更新最后失败的工具名称
		if !cmd.Success {
			lastFailedTool = toolName
		} else {
			lastFailedTool = ""
		}
	}

	return result
}

// DecisionNode 决策节点
// 分析当前信息，决定下一步行动
type DecisionNode struct {
	llm LLM
}

// NewDecisionNode 创建新的 DecisionNode
func NewDecisionNode(llm LLM) *DecisionNode {
	return &DecisionNode{
		llm: llm,
	}
}

// Execute 执行决策逻辑
func (n *DecisionNode) Execute(ctx context.Context, state *State) (Decision, error) {
	logger.Debug("[DecisionNode] Making decision", logger.Int("iteration", state.IterationCount))

	// 检查是否达到最大迭代次数
	if state.IterationCount >= state.MaxIterations {
		logger.Info("[DecisionNode] Max iterations reached, generating report")
		return DecisionReport, nil
	}

	// 调用 LLM 进行决策
	decisionResult, err := n.llm.MakeDecision(ctx, state)
	if err != nil {
		logger.Error("[DecisionNode] LLM decision failed", logger.Err(err))
		// 降级到规则引擎
		return n.fallbackDecision(state), nil
	}

	// 将 LLM 的决策结果添加到推理历史
	if decisionResult != nil {
		state.AddReasoningStep(decisionResult.Reasoning, string(decisionResult.Decision), decisionResult.ToolCalls)
		logger.Debug("[DecisionNode] Decision made",
			logger.String("decision", string(decisionResult.Decision)),
			logger.String("reasoning", decisionResult.Reasoning))
		return decisionResult.Decision, nil
	}

	// 如果结果为空，使用降级决策
	return n.fallbackDecision(state), nil
}

// fallbackDecision 降级决策（规则引擎）
func (n *DecisionNode) fallbackDecision(state *State) Decision {
	logger.Debug("[DecisionNode] Using fallback rule-based decision")

	// 检查是否有错误
	if state.LastError != nil {
		// 如果有错误，直接生成报告
		return DecisionReport
	}

	// 检查是否有问题 Pod
	for _, pod := range state.K8sInfo.Resources["Pods"] {
		podInfo, ok := pod.(PodInfo)
		if !ok {
			continue
		}
		if podInfo.Status == "Error" || podInfo.Status == "CrashLoopBackOff" {
			// Pod 有问题，建议查看日志
			logger.Debug("[DecisionNode] Found problematic pod", logger.String("pod", podInfo.Name), logger.String("status", podInfo.Status))
			return DecisionDeepQuery
		}
		if podInfo.Restarts > 5 {
			// Pod 重启次数过多
			logger.Debug("[DecisionNode] Pod has many restarts", logger.String("pod", podInfo.Name), logger.Int32("restarts", podInfo.Restarts))
			return DecisionDeepQuery
		}
	}

	// 如果已经收集了一些信息，可以生成报告
	podsLen := len(state.K8sInfo.Resources["Pods"])
	if state.IterationCount > 0 && (podsLen > 0 || len(state.AnalysisResult.ExecutedCommands) > 0) {
		return DecisionReport
	}

	// 默认继续收集信息
	return DecisionDeepQuery
}

// ActionNode 行动节点
// 调用 Safety Agent 执行命令
type ActionNode struct {
	safetyAgent SafetyAgent
	k8sClient   K8sClient // K8s MCP 客户端，用于执行 K8s 工具调用
}

// NewActionNode 创建新的 ActionNode
func NewActionNode(safetyAgent SafetyAgent, k8sClient K8sClient) *ActionNode {
	return &ActionNode{
		safetyAgent: safetyAgent,
		k8sClient:   k8sClient,
	}
}

// Execute 执行命令
// 从 state.ReasoningHistory 中获取最新的工具调用列表并执行
func (n *ActionNode) Execute(ctx context.Context, state *State) (*State, error) {
	logger.Info("[ActionNode] Executing commands from ReasoningHistory", logger.Int("iteration", state.IterationCount))

	// 获取最新的推理步骤
	lastStep := state.GetLastReasoningStep()
	if lastStep == nil {
		logger.Warn("[ActionNode] No reasoning step found, skipping action execution")
		return state, nil
	}

	// 检查是否有工具调用
	if len(lastStep.ToolCalls) == 0 {
		logger.Debug("[ActionNode] No tool calls in the reasoning step, skipping")
		return state, nil
	}

	// 执行所有工具调用并收集结果
	var observations []string
	for _, toolCall := range lastStep.ToolCalls {
		observation, err := n.executeToolCall(ctx, state, &toolCall)
		if err != nil {
			logger.Warn("[ActionNode] Tool call execution returned error", logger.Err(err))
			observations = append(observations, fmt.Sprintf("工具 %s 执行失败: %v", toolCall.Tool, err))
		} else {
			observations = append(observations, observation)
		}
	}

	// 将所有观察结果合并为一个字符串
	combinedObservation := strings.Join(observations, "\n---\n")
	state.UpdateLastStepObservation(combinedObservation)

	logger.Info("[ActionNode] All tool calls executed", logger.Int("count", len(lastStep.ToolCalls)))
	return state, nil
}

// executeToolCall 执行单个工具调用
// 根据工具名称分发到不同的执行器
func (n *ActionNode) executeToolCall(ctx context.Context, state *State, toolCall *ToolCall) (string, error) {
	logger.Info("[ActionNode] Executing tool call", logger.String("tool", toolCall.Tool))

	var output string
	var err error

	// 根据工具名称进行分发
	switch toolCall.Tool {
	case "execute_safe_command":
		// 路由到 SafetyAgent
		// 从参数中提取 command 和 reason
		command, _ := toolCall.Args["command"].(string)
		reason, _ := toolCall.Args["reason"].(string)

		if command == "" {
			return "", fmt.Errorf("missing required parameter: command")
		}

		// 构造上下文信息用于审计
		contextInfo := map[string]interface{}{
			"reason": reason,
			"source": "ActionNode",
		}

		// 调用 SafetyAgent 执行安全命令
		output, err = n.safetyAgent.ExecuteSafeCommandWithAudit(ctx, command, contextInfo)
		if err != nil {
			logger.Error("[ActionNode] SafetyAgent command execution failed",
				logger.Err(err),
				logger.String("command", command))
			state.LastError = err
			// 记录失败的命令
			state.AddCommandExecution(command, err.Error(), false)
			return "", err
		}

		// 记录命令执行
		state.AddCommandExecution(command, output, true)
		state.LastAction = command

	default:
		// 默认路由到 K8sClient (MCP)
		// 包括: list_pods, get_pod_logs, describe_pod, list_events 等
		result, err := n.k8sClient.CallTool(ctx, toolCall.Tool, toolCall.Args)
		if err != nil {
			logger.Error("[ActionNode] K8s tool execution failed",
				logger.Err(err),
				logger.String("tool", toolCall.Tool))

			state.LastError = err
			// 记录失败的命令
			state.AddCommandExecution(toolCall.Command, err.Error(), false)
			return "", err
		}

		// 解析结果为字符串
		output, err = k8s.ParseToolResultAsString(result)
		if err != nil {
			logger.Warn("[ActionNode] Failed to parse K8s tool result", logger.Err(err))
			output = fmt.Sprintf("Tool executed but failed to parse result: %v", err)
		}

		// 记录命令执行
		commandStr := fmt.Sprintf("%s %v", toolCall.Tool, toolCall.Args)
		state.AddCommandExecution(commandStr, output, true)
		state.LastAction = toolCall.Tool

		// 清除 LastError，因为命令执行成功
		state.LastError = nil

		// 解析输出并更新状态（用于后续分析）
		n.parseToolOutput(state, toolCall.Tool, output)
	}

	logger.Info("[ActionNode] Tool executed successfully", logger.String("tool", toolCall.Tool))
	return output, nil
}

// parseToolOutput 解析工具输出并更新状态
// 根据工具类型解析输出并存储到相应的状态字段
func (n *ActionNode) parseToolOutput(state *State, toolName, output string) {
	// 根据命令类型解析输出
	if strings.Contains(toolName, "logs") {
		// 日志输出
		logInfo := LogInfo{
			Message:   output,
			Timestamp: time.Now(),
		}
		state.K8sInfo.AppendResource("Logs", logInfo)
	} else if strings.Contains(toolName, "curl") || strings.Contains(toolName, "ping") {
		// 网络测试输出
		if state.K8sInfo.NetworkInfo == nil {
			state.K8sInfo.NetworkInfo = &NetworkInfo{
				Connectivity: make([]ConnectivityInfo, 0),
			}
		}
		connInfo := ConnectivityInfo{
			Success: strings.Contains(output, "200") || strings.Contains(output, "time="),
			Output:  output,
		}
		state.K8sInfo.NetworkInfo.Connectivity = append(state.K8sInfo.NetworkInfo.Connectivity, connInfo)
	}
}

// ReportNode 报告生成节点
// 汇总信息生成最终报告
type ReportNode struct {
	store FindingStore // Finding 存储（去重）
	llm   LLM          // LLM（用于深入分析）
}

// NewReportNode 创建新的 ReportNode
// store: Finding 存储实例，用于去重
// llm: LLM 实例，用于深入分析
func NewReportNode(store FindingStore, llm LLM) *ReportNode {
	return &ReportNode{
		store: store,
		llm:   llm,
	}
}

// saveFinding 保存 Finding 记录到 Store
// 封装了 TTL 获取和错误处理逻辑
func (n *ReportNode) saveFinding(ctx context.Context, key string) {
	if n.store != nil {
		defaultTTL := time.Hour
		if err := n.store.SaveFinding(ctx, key, defaultTTL); err != nil {
			logger.Warn("[ReportNode] Failed to save finding", logger.Err(err))
		}
	}
}

// Execute 生成报告
func (n *ReportNode) Execute(ctx context.Context, state *State) (*State, error) {
	logger.Info("[ReportNode] Generating analysis report")

	// 过滤已执行的命令，去除噪声命令
	filteredCommands := filterExecutedCommands(state.AnalysisResult.ExecutedCommands)

	// 生成 K8s 资源摘要
	k8sSummary := state.K8sInfo.GetSummary()

	// 使用 LLM 生成综合报告摘要
	summary, err := n.llm.SynthesizeReport(ctx, state.UserInput, state.AnalysisResult.Findings, filteredCommands, k8sSummary)
	if err != nil {
		logger.Warn("[ReportNode] Failed to synthesize report with LLM, using fallback", logger.Err(err))
		// 如果 LLM 调用失败，使用简单的摘要作为回退
		state.AnalysisResult.Summary = n.generateSummary(state, filteredCommands)
	} else {
		state.AnalysisResult.Summary = summary
	}

	// 分析发现的问题（传递 ctx 用于去重）
	n.analyzeFindings(ctx, state)

	// 生成建议
	n.generateRecommendations(state)

	// 设置状态
	if state.IterationCount >= state.MaxIterations {
		state.SetStatus(StatusPartial)
	} else {
		state.SetStatus(StatusCompleted)
	}

	logger.Info("[ReportNode] Report generated",
		logger.Int("findings", len(state.AnalysisResult.Findings)),
		logger.Int("recommendations", len(state.AnalysisResult.Recommendations)))

	return state, nil
}

// generateSummary 生成摘要（回退方案，当 LLM 不可用时使用）
func (n *ReportNode) generateSummary(state *State, filteredCommands []CommandExecution) string {
	var sb strings.Builder

	sb.WriteString("## 分析报告\n\n")
	sb.WriteString(fmt.Sprintf("**用户查询**: %s\n\n", state.UserInput))
	sb.WriteString(fmt.Sprintf("**命名空间**: %s\n\n", state.K8sInfo.Namespace))
	sb.WriteString(fmt.Sprintf("**迭代次数**: %d/%d\n\n", state.IterationCount, state.MaxIterations))
	sb.WriteString(fmt.Sprintf("**收集的资源信息**: %s\n\n", state.K8sInfo.GetSummary()))
	sb.WriteString(fmt.Sprintf("**执行的命令数量**: %d\n\n", len(filteredCommands)))

	// 添加发现的问题摘要
	if len(state.AnalysisResult.Findings) > 0 {
		sb.WriteString("**发现的问题**:\n\n")
		for _, f := range state.AnalysisResult.Findings {
			sb.WriteString(fmt.Sprintf("- [%s] %s: %s\n", f.Severity, f.Resource, f.Message))
		}
		sb.WriteString("\n")
	}

	// 添加建议摘要
	if len(state.AnalysisResult.Recommendations) > 0 {
		sb.WriteString("**建议**:\n\n")
		for _, r := range state.AnalysisResult.Recommendations {
			sb.WriteString(fmt.Sprintf("- [%s] %s\n", r.Priority, r.Action))
		}
	}

	return sb.String()
}

// analyzeFindings 分析发现的问题
// 使用并发处理来并行分析多个 Pod 的错误
func (n *ReportNode) analyzeFindings(ctx context.Context, state *State) {
	// 收集需要 LLM 分析的错误 Pod
	type pendingAnalysis struct {
		key          string
		pod          PodInfo
		errorContext ErrorContext
	}

	var pendingAnalyses []pendingAnalysis

	// 检查 Pod 状态并收集需要分析的错误
	pods := state.K8sInfo.Resources["Pods"]
	for _, pod := range pods {
		podInfo, ok := pod.(PodInfo)
		if !ok {
			continue
		}
		switch podInfo.Status {
		case "Error":
			// 生成唯一的 Finding key
			key := fmt.Sprintf("finding:%s:%s:%s", state.K8sInfo.Namespace, podInfo.Name, podInfo.Status)
			// 检查是否已存在
			if n.store != nil {
				has, err := n.store.HasFinding(ctx, key)
				if err != nil {
					logger.Warn("[ReportNode] Failed to check finding", logger.Err(err))
				}
				if has {
					logger.Info("[ReportNode] Skipping duplicate finding", logger.String("key", key))
					continue
				}
			}

			// 收集待分析的错误
			pendingAnalyses = append(pendingAnalyses, pendingAnalysis{
				key: key,
				pod: podInfo,
				errorContext: ErrorContext{
					PodName:   podInfo.Name,
					Namespace: podInfo.Namespace,
					Status:    podInfo.Status,
					Restarts:  podInfo.Restarts,
					Logs:      n.extractPodLogs(state, podInfo.Name),
					Events:    n.extractPodEvents(state, podInfo.Name),
				},
			})

		case "Pending":
			key := fmt.Sprintf("finding:%s:%s:%s", state.K8sInfo.Namespace, podInfo.Name, "Pending")
			if n.store != nil {
				has, err := n.store.HasFinding(ctx, key)
				if err != nil {
					logger.Warn("[ReportNode] Failed to check finding", logger.Err(err))
				}
				if has {
					logger.Info("[ReportNode] Skipping duplicate finding", logger.String("key", key))
					continue
				}
			}
			state.AddFinding("Medium", podInfo.Name, "Pod 处于 Pending 状态")
			n.saveFinding(ctx, key)
		}

		if podInfo.Restarts > 3 {
			key := fmt.Sprintf("finding:%s:%s:high_restarts", state.K8sInfo.Namespace, podInfo.Name)
			if n.store != nil {
				has, err := n.store.HasFinding(ctx, key)
				if err != nil {
					logger.Warn("[ReportNode] Failed to check finding", logger.Err(err))
				}
				if has {
					logger.Info("[ReportNode] Skipping duplicate finding", logger.String("key", key))
					continue
				}
			}
			state.AddFinding("Medium", podInfo.Name, fmt.Sprintf("Pod 重启次数过高: %d", podInfo.Restarts))
			n.saveFinding(ctx, key)
		}
	}

	// 并发执行 LLM 分析，使用 semaphore 限制并发数
	if len(pendingAnalyses) > 0 && n.llm != nil {
		logger.Info("[ReportNode] Starting concurrent LLM analysis",
			logger.Int("count", len(pendingAnalyses)))

		// 使用 WaitGroup 和 Mutex 实现并发分析
		var wg sync.WaitGroup
		var mu sync.Mutex

		// 限制并发数为 1
		semaphore := make(chan struct{}, 1)

		for _, analysis := range pendingAnalyses {
			wg.Add(1)
			go func(a pendingAnalysis) {
				defer wg.Done()

				// 获取信号量
				// 使用 select 允许在 ctx 被取消时退出等待
				select {
				case semaphore <- struct{}{}:
				case <-ctx.Done():
					logger.Info("[ReportNode] Context cancelled, skipping analysis", logger.String("pod", a.pod.Name))
					return
				}

				// 确保释放信号量
				defer func() { <-semaphore }()

				// 再次检查 ctx 是否已取消（可选，但在高并发下有助于提前退出）
				select {
				case <-ctx.Done():
					logger.Info("[ReportNode] Context cancelled before analysis", logger.String("pod", a.pod.Name))
					return
				default:
				}

				// 调用 LLM 进行深入分析
				analysisResult, err := n.llm.AnalyzeError(ctx, a.errorContext)
				mu.Lock()
				defer mu.Unlock()

				if err != nil {
					logger.Fatal("[ReportNode] LLM analysis failed, exiting program",
						logger.Err(err),
						logger.String("pod", a.pod.Name))
					return
				} else {
					// 添加 LLM 分析结果
					for _, finding := range analysisResult.Findings {
						state.AddFinding(finding.Severity, finding.Resource, finding.Message)
					}
					for _, rec := range analysisResult.Recommendations {
						state.AddRecommendation(rec.Action, rec.Reason, rec.Priority, rec.Command)
					}
				}

				// 保存到 store
				n.saveFinding(ctx, a.key)
			}(analysis)
		}

		// 等待所有分析完成
		wg.Wait()
		logger.Info("[ReportNode] Completed concurrent LLM analysis")
	} else if len(pendingAnalyses) > 0 {
		// 没有 LLM，直接添加简单的 Finding
		for _, analysis := range pendingAnalyses {
			state.AddFinding("Critical", analysis.pod.Name, fmt.Sprintf("Pod 状态异常: %s", analysis.pod.Status))
			n.saveFinding(ctx, analysis.key)
		}
	}

	// 检查网络连通性
	if state.K8sInfo.NetworkInfo != nil {
		for _, conn := range state.K8sInfo.NetworkInfo.Connectivity {
			if !conn.Success {
				key := fmt.Sprintf("finding:network:%s:%s", conn.Source, conn.Target)
				if n.store != nil {
					has, err := n.store.HasFinding(ctx, key)
					if err != nil {
						logger.Warn("[ReportNode] Failed to check finding", logger.Err(err))
					}
					if has {
						logger.Info("[ReportNode] Skipping duplicate finding", logger.String("key", key))
						continue
					}
				}
				state.AddFinding("High", "Network", fmt.Sprintf("网络连接失败: %s", conn.Output))
				n.saveFinding(ctx, key)
			}
		}
	}
}

// extractPodLogs 提取 Pod 的日志
// 优先从 state.K8sInfo.Resources["Logs"] 中获取，如果未找到则从 ExecutedCommands 中搜索
func (n *ReportNode) extractPodLogs(state *State, podName string) string {
	// 首先从 Resources["Logs"] 中查找
	logs := state.K8sInfo.Resources["Logs"]
	for _, log := range logs {
		logInfo, ok := log.(LogInfo)
		if !ok {
			continue
		}
		if logInfo.PodName == podName {
			return logInfo.Message
		}
	}

	// 如果未找到，从 ExecutedCommands 中搜索 get_pod_logs 调用
	for _, cmd := range state.AnalysisResult.ExecutedCommands {
		// 检查是否是获取日志的命令
		if strings.Contains(cmd.Command, "get_pod_logs") && cmd.Success {
			// 检查命令中是否包含指定的 Pod 名称
			if strings.Contains(cmd.Command, podName) {
				return cmd.Output
			}
		}
	}

	return ""
}

// extractPodEvents 提取 Pod 相关的事件
// 从 ExecutedCommands 中搜索 get_events 调用，查找与指定 Pod 或命名空间相关的事件
func (n *ReportNode) extractPodEvents(state *State, podName string) []string {
	// 从 ExecutedCommands 中搜索 get_events 调用
	for _, cmd := range state.AnalysisResult.ExecutedCommands {
		// 检查是否是获取事件的命令
		if strings.Contains(cmd.Command, "get_events") && cmd.Success {
			// 解析输出为事件列表
			events := parseEventsFromOutput(cmd.Output, podName)
			if len(events) > 0 {
				logger.Debug("[ReportNode] Found events from previous get_events call",
					logger.String("pod", podName),
					logger.Int("count", len(events)))
				return events
			}
		}
	}

	// 没有找到相关事件，返回空切片
	return []string{}
}

// parseEventsFromOutput 解析工具输出中的事件
// 将 get_events 命令的输出解析为事件字符串列表
func parseEventsFromOutput(output, targetPod string) []string {
	if output == "" {
		return []string{}
	}

	var events []string

	// 尝试解析为结构化数据（JSON 格式）
	// 如果输出是 JSON 格式的事件数组，解析它
	if strings.HasPrefix(strings.TrimSpace(output), "[") || strings.HasPrefix(strings.TrimSpace(output), "{") {
		// 尝试解析为通用 JSON，然后提取事件信息
		// 这里使用简单的文本解析方法
		lines := strings.Split(output, "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			// 如果指定了目标 Pod，过滤相关事件
			if targetPod != "" && !strings.Contains(line, targetPod) {
				// 对于 namespace 级别的事件，我们仍然保留
				// 只过滤明显不相关的 Pod 事件
				if strings.Contains(line, "Pod") && !strings.Contains(line, targetPod) {
					continue
				}
			}
			// 过滤掉空行或无效行
			if len(line) > 5 {
				events = append(events, line)
			}
		}
	} else {
		// 文本格式，按行解析
		lines := strings.Split(output, "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			// 如果指定了目标 Pod，过滤相关事件
			if targetPod != "" {
				// 保留包含目标 Pod 名称的行
				if strings.Contains(line, targetPod) {
					events = append(events, line)
				} else if strings.Contains(strings.ToLower(line), "namespace") && !strings.Contains(line, "Pod/") {
					events = append(events, line)
				}
			} else {
				// 没有指定 Pod，保留所有事件
				events = append(events, line)
			}
		}
	}

	return events
}

// generateRecommendations 生成建议
func (n *ReportNode) generateRecommendations(state *State) {
	// 基于 Findings 生成建议
	for _, finding := range state.AnalysisResult.Findings {
		if finding.Severity == "Critical" {
			if strings.Contains(finding.Message, "CrashLoopBackOff") {
				state.AddRecommendation(
					"查看 Pod 日志",
					"Pod 处于 CrashLoopBackOff 状态，需要查看日志定位问题",
					"High",
					fmt.Sprintf("kubectl logs %s -n %s", finding.Resource, state.K8sInfo.Namespace),
				)
			}
		}
	}

	// 如果没有发现，添加一个信息性建议
	if len(state.AnalysisResult.Findings) == 0 {
		state.AddRecommendation(
			"继续监控",
			"当前未发现明显问题，建议继续监控集群状态",
			"Low",
			"",
		)
	}
}
