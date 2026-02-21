// Package analysis 提供 Graph 节点实现
package analysis

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/AceDarkknight/k8s-analyzer-agent/internal/agent/safety"
	"github.com/AceDarkknight/k8s-analyzer-agent/internal/client"
	"github.com/AceDarkknight/k8s-analyzer-agent/internal/client/k8s"
	"github.com/AceDarkknight/k8s-analyzer-agent/internal/logger"
)

// quietContextKey 用于在 Context 中传递安静模式标志
var quietContextKey = struct{}{}

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
	namespaces, err := n.collectNamespaces(ctx)
	if err != nil {
		logger.Warn("[InfoNode] Failed to list namespaces, falling back to default", logger.Err(err))
		// 回退到 default 命名空间，但不设置 LastError，因为这只是一个警告
		namespaces = []string{"default"}
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
	allServices := make([]ServiceInfo, 0)
	allDeployments := make([]DeploymentInfo, 0)
	allEvents := make([]EventInfo, 0)

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

		// 收集 Service 信息
		services, err := n.collectServices(ctx, ns)
		if err != nil {
			logger.Warn("[InfoNode] Failed to collect services from namespace",
				logger.String("namespace", ns),
				logger.Err(err))
		} else {
			allServices = append(allServices, services...)
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

		// 收集事件信息
		events, err := n.collectEvents(ctx, ns)
		if err != nil {
			logger.Warn("[InfoNode] Failed to collect events from namespace",
				logger.String("namespace", ns),
				logger.Err(err))
		} else {
			allEvents = append(allEvents, events...)
		}
	}

	// 更新状态
	state.K8sInfo.Pods = allPods
	state.K8sInfo.Services = allServices
	state.K8sInfo.Deployments = allDeployments
	state.K8sInfo.Events = allEvents

	logger.Info("[InfoNode] Collected information from all namespaces",
		logger.Int("namespaces", len(namespaces)),
		logger.Int("pods", len(allPods)),
		logger.Int("services", len(allServices)),
		logger.Int("deployments", len(allDeployments)),
		logger.Int("events", len(allEvents)))

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
func (n *InfoNode) collectNamespaces(ctx context.Context) ([]string, error) {
	// 先检查 list_namespaces 工具是否可用
	if !n.hasTool(ctx, "list_namespaces") {
		logger.Info("[InfoNode] list_namespaces tool not available on MCP server, using hardcoded fallback list")
		// 减少命名空间数量用于测试，只保留 default 和 kube-system
		return []string{"default", "kube-system"}, nil
	}

	// 调用 K8s MCP 工具获取命名空间列表
	result, err := n.k8sClient.CallTool(ctx, "list_namespaces", nil)
	if err != nil {
		logger.Warn("[InfoNode] list_namespaces tool call failed, using hardcoded fallback list", logger.Err(err))
		// K8s MCP 服务器不支持 list_namespaces，返回硬编码的常见命名空间列表
		return []string{"default", "kube-system"}, nil
	}

	// 解析结果
	namespaces := make([]string, 0)
	if result != nil {
		// 解析为 k8s-mcp 的 Namespace 类型切片
		k8sNamespaces, err := k8s.ParseToolResult[[]k8s.Namespace](result, "list_namespaces")
		if err != nil {
			logger.Warn("[InfoNode] Failed to parse namespaces result", logger.Err(err))
			return []string{"default", "kube-system"}, nil
		}
		// 转换为字符串切片
		for _, ns := range k8sNamespaces {
			namespaces = append(namespaces, ns.Name)
		}
	}

	// 如果没有获取到命名空间，使用硬编码列表作为回退
	if len(namespaces) == 0 {
		logger.Info("[InfoNode] No namespaces returned from MCP, using hardcoded fallback list")
		return []string{"default", "kube-system"}, nil
	}

	logger.Debug("[InfoNode] Collected namespaces", logger.Any("namespaces", namespaces))
	return namespaces, nil
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

// collectServices 收集 Service 信息
func (n *InfoNode) collectServices(ctx context.Context, namespace string) ([]ServiceInfo, error) {
	args := map[string]interface{}{
		"namespace": namespace,
	}

	result, err := n.k8sClient.CallTool(ctx, "list_services", args)
	if err != nil {
		return nil, err
	}

	services := make([]ServiceInfo, 0)
	if result != nil {
		// 解析为 k8s-mcp 的 Service 类型切片
		k8sServices, err := k8s.ParseToolResult[[]k8s.Service](result, "list_services")
		if err != nil {
			logger.Warn("[InfoNode] Failed to parse services result", logger.Err(err))
			return services, nil
		}
		// 转换为内部 ServiceInfo 类型
		for _, s := range k8sServices {
			serviceInfo := ServiceInfo{
				Name:      s.Name,
				Namespace: s.Namespace,
				Type:      s.Type,
				ClusterIP: s.ClusterIP,
			}
			services = append(services, serviceInfo)
		}
	}

	logger.Debug("[InfoNode] Collected services from namespace", logger.String("namespace", namespace), logger.Int("count", len(services)))
	return services, nil
}

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

// collectEvents 收集事件信息
func (n *InfoNode) collectEvents(ctx context.Context, namespace string) ([]EventInfo, error) {
	args := map[string]interface{}{
		"namespace": namespace,
	}

	result, err := n.k8sClient.CallTool(ctx, "get_events", args)
	if err != nil {
		return nil, err
	}

	events := make([]EventInfo, 0)
	if result != nil {
		// 解析为 k8s-mcp 的 Event 类型切片
		k8sEvents, err := k8s.ParseToolResult[[]k8s.Event](result, "get_events")
		if err != nil {
			logger.Warn("[InfoNode] Failed to parse events result", logger.Err(err))
			return events, nil
		}
		// 转换为内部 EventInfo 类型
		for _, e := range k8sEvents {
			eventInfo := EventInfo{
				Type:      e.Type,
				Reason:    e.Reason,
				Message:   e.Message,
				Component: e.Source,
			}
			events = append(events, eventInfo)
		}
	}

	logger.Debug("[InfoNode] Collected events from namespace", logger.String("namespace", namespace), logger.Int("count", len(events)))
	return events, nil
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
	decision, err := n.llm.MakeDecision(ctx, state)
	if err != nil {
		logger.Error("[DecisionNode] LLM decision failed", logger.Err(err))
		// 降级到规则引擎
		return n.fallbackDecision(state), nil
	}

	logger.Debug("[DecisionNode] Decision made", logger.String("decision", string(decision)))
	return decision, nil
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
	for _, pod := range state.K8sInfo.Pods {
		if pod.Status == "Error" || pod.Status == "CrashLoopBackOff" {
			// Pod 有问题，建议查看日志
			logger.Debug("[DecisionNode] Found problematic pod", logger.String("pod", pod.Name), logger.String("status", pod.Status))
			return DecisionDeepQuery
		}
		if pod.Restarts > 5 {
			// Pod 重启次数过多
			logger.Debug("[DecisionNode] Pod has many restarts", logger.String("pod", pod.Name), logger.Int32("restarts", pod.Restarts))
			return DecisionDeepQuery
		}
	}

	// 检查是否有事件
	for _, event := range state.K8sInfo.Events {
		if event.Type == "Warning" {
			logger.Debug("[DecisionNode] Found warning event", logger.String("reason", event.Reason), logger.String("message", event.Message))
			return DecisionDeepQuery
		}
	}

	// 如果已经收集了一些信息，可以生成报告
	if state.IterationCount > 0 && (len(state.K8sInfo.Pods) > 0 || len(state.AnalysisResult.ExecutedCommands) > 0) {
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
// 支持两种命令类型：K8s MCP 工具调用 和 Shell 命令
func (n *ActionNode) Execute(ctx context.Context, state *State, toolCall *ToolCall) (*State, error) {
	logger.Info("[ActionNode] Executing command", logger.Any("toolCall", toolCall))

	// 如果命令为空，检查是否有错误
	if toolCall == nil {
		quiet := ctx.Value(quietContextKey).(bool)
		if state.LastError != nil {
			logger.Warn("[ActionNode] No command to execute, but there is an error", logger.Err(state.LastError))
			// 保持错误状态，让决策节点处理
			return state, nil
		}
		if !quiet {
			logger.Debug("[ActionNode] No command to execute, skipping")
		}
		return state, nil
	}

	// 根据命令类型执行不同的逻辑
	if toolCall.Type == "k8s" {
		// 执行 K8s MCP 工具调用
		return n.executeK8sTool(ctx, state, toolCall)
	}

	// 否则，使用 Safety Agent 执行 Shell 命令（兼容旧逻辑）
	return n.executeShellCommand(ctx, state, toolCall.Command)
}

// executeK8sTool 执行 K8s MCP 工具调用
func (n *ActionNode) executeK8sTool(ctx context.Context, state *State, toolCall *ToolCall) (*State, error) {
	logger.Info("[ActionNode] Executing K8s MCP tool", logger.String("tool", toolCall.Tool))

	// 调用 K8s MCP 工具
	result, err := n.k8sClient.CallTool(ctx, toolCall.Tool, toolCall.Args)
	if err != nil {
		logger.Error("[ActionNode] K8s tool execution failed", logger.Err(err), logger.String("tool", toolCall.Tool))

		state.LastError = err
		state.AddCommandExecution(toolCall.Command, err.Error(), false)
		return state, nil // 不返回错误，让决策节点处理
	}

	// 解析结果为字符串
	output, err := k8s.ParseToolResultAsString(result)
	if err != nil {
		logger.Warn("[ActionNode] Failed to parse K8s tool result", logger.Err(err))
		output = fmt.Sprintf("Tool executed but failed to parse result: %v", err)
	}

	logger.Info("[ActionNode] K8s tool executed successfully", logger.String("tool", toolCall.Tool))
	state.AddCommandExecution(toolCall.Command, output, true)
	state.LastAction = toolCall.Command

	// 清除 LastError，因为命令执行成功
	state.LastError = nil

	// 解析输出并更新状态
	n.parseCommandOutput(state, toolCall.Command, output)

	return state, nil
}

// executeShellCommand 执行 Shell 命令（兼容旧逻辑）
func (n *ActionNode) executeShellCommand(ctx context.Context, state *State, command string) (*State, error) {
	logger.Info("[ActionNode] Executing shell command", logger.String("command", command))

	// 调用 Safety Agent 执行命令
	output, err := n.safetyAgent.ExecuteSafeCommand(ctx, command)
	if err != nil {
		logger.Error("[ActionNode] Command execution failed", logger.Err(err), logger.String("command", command))

		// 检查是否是安全拒绝错误
		if _, ok := err.(*safety.UnsafeCommandError); ok {
			state.AddFinding("High", "Security", fmt.Sprintf("Command rejected by safety check: %s", command))
		}

		state.LastError = err
		state.AddCommandExecution(command, err.Error(), false)
		return state, nil // 不返回错误，让决策节点处理
	}

	logger.Info("[ActionNode] Command executed successfully", logger.String("command", command))
	state.AddCommandExecution(command, output, true)
	state.LastAction = command

	// 清除 LastError，因为命令执行成功
	state.LastError = nil

	// 解析输出并更新状态
	n.parseCommandOutput(state, command, output)

	return state, nil
}

// parseCommandOutput 解析命令输出
func (n *ActionNode) parseCommandOutput(state *State, command, output string) {
	// 根据命令类型解析输出
	if strings.Contains(command, "logs") {
		// 日志输出
		logInfo := LogInfo{
			Message:   output,
			Timestamp: time.Now(),
		}
		state.K8sInfo.Logs = append(state.K8sInfo.Logs, logInfo)
	} else if strings.Contains(command, "curl") || strings.Contains(command, "ping") {
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
}

// NewReportNode 创建新的 ReportNode
func NewReportNode() *ReportNode {
	return &ReportNode{}
}

// Execute 生成报告
func (n *ReportNode) Execute(ctx context.Context, state *State) (*State, error) {
	logger.Info("[ReportNode] Generating analysis report")

	// 生成摘要
	state.AnalysisResult.Summary = n.generateSummary(state)

	// 分析发现的问题
	n.analyzeFindings(state)

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

// generateSummary 生成摘要
func (n *ReportNode) generateSummary(state *State) string {
	var sb strings.Builder

	sb.WriteString("## 分析报告\n\n")
	sb.WriteString(fmt.Sprintf("**用户查询**: %s\n\n", state.UserInput))
	sb.WriteString(fmt.Sprintf("**命名空间**: %s\n\n", state.K8sInfo.Namespace))
	sb.WriteString(fmt.Sprintf("**迭代次数**: %d/%d\n\n", state.IterationCount, state.MaxIterations))
	sb.WriteString(fmt.Sprintf("**收集的 Pod 数量**: %d\n\n", len(state.K8sInfo.Pods)))
	sb.WriteString(fmt.Sprintf("**收集的 Service 数量**: %d\n\n", len(state.K8sInfo.Services)))
	sb.WriteString(fmt.Sprintf("**执行的命令数量**: %d\n\n", len(state.AnalysisResult.ExecutedCommands)))

	return sb.String()
}

// analyzeFindings 分析发现的问题
func (n *ReportNode) analyzeFindings(state *State) {
	// 检查 Pod 状态
	for _, pod := range state.K8sInfo.Pods {
		switch pod.Status {
		case "Error", "CrashLoopBackOff":
			state.AddFinding("Critical", pod.Name, fmt.Sprintf("Pod 状态异常: %s", pod.Status))
		case "Pending":
			state.AddFinding("Medium", pod.Name, "Pod 处于 Pending 状态")
		}
		if pod.Restarts > 3 {
			state.AddFinding("High", pod.Name, fmt.Sprintf("Pod 重启次数过多: %d", pod.Restarts))
		}
	}

	// 检查事件
	for _, event := range state.K8sInfo.Events {
		if event.Type == "Warning" {
			severity := "Medium"
			if strings.Contains(event.Reason, "Failed") || strings.Contains(event.Reason, "Error") {
				severity = "High"
			}
			state.AddFinding(severity, event.Component, fmt.Sprintf("%s: %s", event.Reason, event.Message))
		}
	}

	// 检查网络连通性
	if state.K8sInfo.NetworkInfo != nil {
		for _, conn := range state.K8sInfo.NetworkInfo.Connectivity {
			if !conn.Success {
				state.AddFinding("High", "Network", fmt.Sprintf("网络连接失败: %s", conn.Output))
			}
		}
	}
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
