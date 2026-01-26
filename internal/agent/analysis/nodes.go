// Package analysis 提供 Graph 节点实现
package analysis

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/AceDarkknight/k8s-analyzer-agent/internal/agent/safety"
	"github.com/AceDarkknight/k8s-analyzer-agent/internal/client/k8s"
)

// K8sClient K8s 客户端接口
type K8sClient interface {
	CallTool(ctx context.Context, name string, args map[string]interface{}) (*k8s.CallToolResult, error)
	ListTools(ctx context.Context) ([]k8s.Tool, error)
}

// SafetyAgent 安全 Agent 接口
type SafetyAgent interface {
	ExecuteSafeCommand(ctx context.Context, command string) (string, error)
}

// InfoNode 信息收集节点
// 调用 K8s Client 获取集群/资源信息
type InfoNode struct {
	k8sClient K8sClient
	logger    *log.Logger
}

// NewInfoNode 创建新的 InfoNode
func NewInfoNode(k8sClient K8sClient, logger *log.Logger) *InfoNode {
	if logger == nil {
		logger = log.Default()
	}
	return &InfoNode{
		k8sClient: k8sClient,
		logger:    logger,
	}
}

// Execute 执行信息收集
func (n *InfoNode) Execute(ctx context.Context, state *State) (*State, error) {
	n.logger.Printf("[InfoNode] Starting information collection for: %s", state.UserInput)

	// 根据用户输入决定收集哪些信息
	namespace := n.extractNamespace(state.UserInput)

	// 收集 Pod 信息
	pods, err := n.collectPods(ctx, namespace)
	if err != nil {
		n.logger.Printf("[InfoNode] Failed to collect pods: %v", err)
		state.LastError = fmt.Errorf("failed to collect pods: %w", err)
		return state, nil // 即使失败也继续，让决策节点处理
	}
	state.K8sInfo.Pods = pods
	state.K8sInfo.Namespace = namespace

	// 收集 Service 信息
	services, err := n.collectServices(ctx, namespace)
	if err != nil {
		n.logger.Printf("[InfoNode] Failed to collect services: %v", err)
		// 不设置错误，继续执行
	}
	state.K8sInfo.Services = services

	// 收集 Deployment 信息
	deployments, err := n.collectDeployments(ctx, namespace)
	if err != nil {
		n.logger.Printf("[InfoNode] Failed to collect deployments: %v", err)
	}
	state.K8sInfo.Deployments = deployments

	// 收集事件信息
	events, err := n.collectEvents(ctx, namespace)
	if err != nil {
		n.logger.Printf("[InfoNode] Failed to collect events: %v", err)
	}
	state.K8sInfo.Events = events

	n.logger.Printf("[InfoNode] Collected %d pods, %d services, %d deployments, %d events",
		len(pods), len(services), len(deployments), len(events))

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

	// 解析结果（这里简化处理，实际需要解析 MCP 返回的数据）
	// 在实际实现中，需要根据 MCP Server 返回的格式进行解析
	pods := make([]PodInfo, 0)

	// 模拟数据（实际应该从 result 中解析）
	// 这里只是为了演示，实际需要根据真实的 MCP 响应格式解析
	if result != nil {
		// 解析逻辑...
		_ = result
	}

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
	_ = result // 实际需要解析

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
	_ = result // 实际需要解析

	return deployments, nil
}

// collectEvents 收集事件信息
func (n *InfoNode) collectEvents(ctx context.Context, namespace string) ([]EventInfo, error) {
	args := map[string]interface{}{
		"namespace": namespace,
	}

	result, err := n.k8sClient.CallTool(ctx, "list_events", args)
	if err != nil {
		return nil, err
	}

	events := make([]EventInfo, 0)
	_ = result // 实际需要解析

	return events, nil
}

// DecisionNode 决策节点
// 分析当前信息，决定下一步行动
type DecisionNode struct {
	llm    LLM
	logger *log.Logger
}

// NewDecisionNode 创建新的 DecisionNode
func NewDecisionNode(llm LLM, logger *log.Logger) *DecisionNode {
	if logger == nil {
		logger = log.Default()
	}
	return &DecisionNode{
		llm:    llm,
		logger: logger,
	}
}

// Execute 执行决策逻辑
func (n *DecisionNode) Execute(ctx context.Context, state *State) (Decision, error) {
	n.logger.Printf("[DecisionNode] Making decision (iteration %d)", state.IterationCount)

	// 检查是否达到最大迭代次数
	if state.IterationCount >= state.MaxIterations {
		n.logger.Printf("[DecisionNode] Max iterations reached, generating report")
		return DecisionReport, nil
	}

	// 调用 LLM 进行决策
	decision, err := n.llm.MakeDecision(ctx, state)
	if err != nil {
		n.logger.Printf("[DecisionNode] LLM decision failed: %v", err)
		// 降级到规则引擎
		return n.fallbackDecision(state), nil
	}

	n.logger.Printf("[DecisionNode] Decision: %s", decision)
	return decision, nil
}

// fallbackDecision 降级决策（规则引擎）
func (n *DecisionNode) fallbackDecision(state *State) Decision {
	n.logger.Printf("[DecisionNode] Using fallback rule-based decision")

	// 检查是否有错误
	if state.LastError != nil {
		// 如果有错误，直接生成报告
		return DecisionReport
	}

	// 检查是否有问题 Pod
	for _, pod := range state.K8sInfo.Pods {
		if pod.Status == "Error" || pod.Status == "CrashLoopBackOff" {
			// Pod 有问题，建议查看日志
			n.logger.Printf("[DecisionNode] Found problematic pod: %s (status: %s)", pod.Name, pod.Status)
			return DecisionDeepQuery
		}
		if pod.Restarts > 5 {
			// Pod 重启次数过多
			n.logger.Printf("[DecisionNode] Pod %s has %d restarts", pod.Name, pod.Restarts)
			return DecisionDeepQuery
		}
	}

	// 检查是否有事件
	for _, event := range state.K8sInfo.Events {
		if event.Type == "Warning" {
			n.logger.Printf("[DecisionNode] Found warning event: %s - %s", event.Reason, event.Message)
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
	logger      *log.Logger
}

// NewActionNode 创建新的 ActionNode
func NewActionNode(safetyAgent SafetyAgent, logger *log.Logger) *ActionNode {
	if logger == nil {
		logger = log.Default()
	}
	return &ActionNode{
		safetyAgent: safetyAgent,
		logger:      logger,
	}
}

// Execute 执行命令
func (n *ActionNode) Execute(ctx context.Context, state *State, command string) (*State, error) {
	n.logger.Printf("[ActionNode] Executing command: %s", command)

	// 如果命令为空，检查是否有错误
	if command == "" {
		if state.LastError != nil {
			n.logger.Printf("[ActionNode] No command to execute, but there is an error: %v", state.LastError)
			// 保持错误状态，让决策节点处理
			return state, nil
		}
		n.logger.Printf("[ActionNode] No command to execute, skipping")
		return state, nil
	}

	// 调用 Safety Agent 执行命令
	output, err := n.safetyAgent.ExecuteSafeCommand(ctx, command)
	if err != nil {
		n.logger.Printf("[ActionNode] Command execution failed: %v", err)

		// 检查是否是安全拒绝错误
		if _, ok := err.(*safety.UnsafeCommandError); ok {
			state.AddFinding("High", "Security", fmt.Sprintf("Command rejected by safety check: %s", command))
		}

		state.LastError = err
		state.AddCommandExecution(command, err.Error(), false)
		return state, nil // 不返回错误，让决策节点处理
	}

	n.logger.Printf("[ActionNode] Command executed successfully")
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
	logger *log.Logger
}

// NewReportNode 创建新的 ReportNode
func NewReportNode(logger *log.Logger) *ReportNode {
	if logger == nil {
		logger = log.Default()
	}
	return &ReportNode{
		logger: logger,
	}
}

// Execute 生成报告
func (n *ReportNode) Execute(ctx context.Context, state *State) (*State, error) {
	n.logger.Printf("[ReportNode] Generating analysis report")

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

	n.logger.Printf("[ReportNode] Report generated with %d findings and %d recommendations",
		len(state.AnalysisResult.Findings),
		len(state.AnalysisResult.Recommendations))

	return state, nil
}

// generateSummary 生成摘要
func (n *ReportNode) generateSummary(state *State) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("## 分析报告\n\n"))
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
