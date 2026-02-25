// Package analysis 提供 ReAct LLM 实现
// 基于 Eino ReAct Agent 构建，支持动态工具调用
package analysis

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/AceDarkknight/k8s-analyzer-agent/internal/client"
	"github.com/AceDarkknight/k8s-analyzer-agent/internal/config"
	"github.com/AceDarkknight/k8s-analyzer-agent/internal/logger"
	openai "github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/flow/agent/react"
	"github.com/cloudwego/eino/schema"
)

// ReActLLM 基于 Eino ReAct Agent 构建的分析器
// 支持动态工具调用，可以主动调查问题
type ReActLLM struct {
	agent            *react.Agent
	toolCallingModel model.ToolCallingChatModel
	chatModel        interface {
		Generate(ctx context.Context, in []*schema.Message, opts ...model.Option) (*schema.Message, error)
	}
	k8sClient   K8sClient
	safetyAgent SafetyAgent
	tools       []tool.BaseTool
	config      *config.LLMConfig
}

// NewReActLLM 创建新的 ReActLLM
// 使用 Eino ReAct Agent 构建，支持动态工具调用
func NewReActLLM(ctx context.Context, k8sClient K8sClient, safetyAgent SafetyAgent, llmConfig *config.LLMConfig) (*ReActLLM, error) {
	logger.Info("[ReActLLM] Initializing ReAct LLM",
		logger.String("model", llmConfig.Model),
		logger.String("provider", llmConfig.Provider))

	// 1. 创建 ChatModel
	chatModel, err := createChatModel(ctx, llmConfig)
	if err != nil {
		logger.Error("[ReActLLM] Failed to create chat model", logger.Err(err))
		return nil, fmt.Errorf("failed to create chat model: %w", err)
	}

	// 2. 封装 K8s 工具
	k8sTools, err := WrapK8sTools(ctx, k8sClient)
	if err != nil {
		logger.Error("[ReActLLM] Failed to wrap K8s tools", logger.Err(err))
		return nil, fmt.Errorf("failed to wrap K8s tools: %w", err)
	}

	// 3. 封装 SafetyAgent 工具
	safetyTool := WrapSafetyAgent(safetyAgent)
	allTools := append(k8sTools, safetyTool)

	logger.Info("[ReActLLM] Tools prepared",
		logger.Int("k8s_tools", len(k8sTools)),
		logger.Int("total_tools", len(allTools)))

	// 4. 绑定工具到模型（使用 WithTools）
	toolCallingModel, err := chatModel.WithTools(convertToToolInfo(allTools))
	if err != nil {
		logger.Error("[ReActLLM] Failed to bind tools to model", logger.Err(err))
		return nil, fmt.Errorf("failed to bind tools to model: %w", err)
	}

	// 5. 构建 ReAct Agent
	agent, err := react.NewAgent(ctx, &react.AgentConfig{
		ToolCallingModel: toolCallingModel,
		ToolsConfig: compose.ToolsNodeConfig{
			Tools: allTools,
		},
		MaxStep:         10, // 防止无限循环
		MessageModifier: getReActMessageModifier(),
	})
	if err != nil {
		logger.Error("[ReActLLM] Failed to create ReAct agent", logger.Err(err))
		return nil, fmt.Errorf("failed to create ReAct agent: %w", err)
	}

	logger.Info("[ReActLLM] ReAct LLM initialized successfully")

	return &ReActLLM{
		agent:            agent,
		toolCallingModel: toolCallingModel,
		chatModel:        chatModel,
		k8sClient:        k8sClient,
		safetyAgent:      safetyAgent,
		tools:            allTools,
		config:           llmConfig,
	}, nil
}

// AnalyzeError 使用 ReAct Agent 分析错误上下文
// 可以动态调用工具获取更多信息
// 包含重试逻辑：最多重试 3 次，间隔分别为 1s, 2s, 4s
func (llm *ReActLLM) AnalyzeError(ctx context.Context, errorContext ErrorContext) (AnalysisResult, error) {
	logger.Info("[ReActLLM] Analyzing error context",
		logger.String("pod", errorContext.PodName),
		logger.String("namespace", errorContext.Namespace),
		logger.String("status", errorContext.Status))

	// 添加诊断日志：检查模型配置
	logger.Debug("[ReActLLM] Model config for analysis",
		logger.String("model", llm.config.Model),
		logger.String("base_url", llm.config.BaseURL),
		logger.String("has_api_key", fmt.Sprintf("%t", llm.config.APIKey != "")))

	// 构建包含已收集数据的提示词
	prompt := buildReActPrompt(errorContext)

	// 构造初始消息
	message := schema.UserMessage(prompt)

	logger.Debug("[ReActLLM] Starting ReAct agent generation",
		logger.String("prompt_length", fmt.Sprintf("%d", len(prompt))))

	// 重试配置
	maxRetries := 3
	retryDelays := []time.Duration{1 * time.Second, 2 * time.Second, 4 * time.Second}

	// 尝试生成，带重试
	var finalMsg *schema.Message
	var err error
	for i := 0; i < maxRetries; i++ {
		if i > 0 {
			logger.Info("[ReActLLM] Retrying LLM generation",
				logger.Int("attempt", i+1),
				logger.String("pod", errorContext.PodName),
				logger.String("delay", retryDelays[i-1].String()))
			time.Sleep(retryDelays[i-1])
		}

		finalMsg, err = llm.agent.Generate(ctx, []*schema.Message{message})
		if err == nil {
			break
		}

		logger.Warn("[ReActLLM] LLM generation failed, will retry",
			logger.Err(err),
			logger.String("pod", errorContext.PodName),
			logger.Int("attempt", i+1))
	}

	if err != nil {
		logger.Error("[ReActLLM] ReAct agent failed after retries", logger.Err(err))
		return AnalysisResult{}, err
	}

	// 解析响应为 AnalysisResult
	return parseReActResponse(finalMsg.Content)
}

// MakeDecision 使用 LLM 根据当前状态做出决策
// 通过 chatModel 分析状态并选择下一步行动
func (llm *ReActLLM) MakeDecision(ctx context.Context, state *State) (Decision, error) {
	logger.Info("[ReActLLM] Making decision", logger.Int("iteration", state.IterationCount))

	// 预检查1：如果上次动作是 "no_command"，说明 CommandGenerator 没有产生新的命令
	// 直接返回报告决策，避免无效的 LLM 调用
	if state.LastAction == "no_command" {
		logger.Info("[ReActLLM] Last action was 'no_command', skipping LLM decision and returning report")
		return DecisionReport, nil
	}

	// 预检查2：如果已达到最大迭代次数，强制生成报告以避免无限循环
	// 使用 state.MaxIterations 作为限制（默认为10）
	if state.IterationCount >= state.MaxIterations {
		logger.Info("[ReActLLM] Max iterations reached, forcing report generation",
			logger.Int("iteration", state.IterationCount),
			logger.Int("max", state.MaxIterations))
		return DecisionReport, nil
	}

	// 构建决策提示词
	prompt := buildDecisionPrompt(state)

	// 构造消息
	messages := []*schema.Message{
		schema.UserMessage(prompt),
	}

	// 调用 LLM 生成决策
	resp, err := llm.chatModel.Generate(ctx, messages)
	if err != nil {
		logger.Error("[ReActLLM] LLM decision generation failed", logger.Err(err))
		// 降级到规则决策
		return DecisionReport, nil
	}

	// 解析响应获取决策
	decision, err := parseDecisionResponse(resp.Content)
	if err != nil {
		logger.Warn("[ReActLLM] Failed to parse decision response, using default", logger.Err(err))
		return DecisionReport, nil
	}

	logger.Info("[ReActLLM] Decision made", logger.String("decision", string(decision)))
	return decision, nil
}

// Analyze 使用 ReAct Agent 进行深入分析
// 将 State 转换为 ErrorContext 并调用 ReAct Agent 进行分析
func (llm *ReActLLM) Analyze(ctx context.Context, state *State) (string, error) {
	logger.Info("[ReActLLM] Performing deep analysis",
		logger.Int("iteration", state.IterationCount),
		logger.Int("pods", len(state.K8sInfo.Pods)))

	// 如果有错误 Pod，优先分析错误 Pod
	for _, pod := range state.K8sInfo.Pods {
		if pod.Status == "Error" || pod.Status == "CrashLoopBackOff" {
			errorContext := ErrorContext{
				PodName:   pod.Name,
				Namespace: pod.Namespace,
				Status:    pod.Status,
				Restarts:  pod.Restarts,
				Logs:      extractPodLogs(state, pod.Name),
				Events:    []string{}, // 事件需要通过工具获取
			}

			// 调用 AnalyzeError 进行深入分析
			analysisResult, err := llm.AnalyzeError(ctx, errorContext)
			if err != nil {
				logger.Warn("[ReActLLM] Deep analysis failed", logger.Err(err))
				continue
			}

			// 返回分析结果的字符串形式
			return formatAnalysisResult(analysisResult), nil
		}
	}

	// 如果没有错误 Pod，对整个状态进行分析
	errorContext := ErrorContext{
		PodName:   "general",
		Namespace: state.K8sInfo.Namespace,
		Status:    "analyzing",
		Restarts:  0,
		Logs:      "",
		Events:    []string{}, // 事件需要通过工具获取
	}

	// 调用 AnalyzeError 进行分析
	analysisResult, err := llm.AnalyzeError(ctx, errorContext)
	if err != nil {
		logger.Warn("[ReActLLM] General analysis failed", logger.Err(err))
		return "", err
	}

	return formatAnalysisResult(analysisResult), nil
}

// GenerateReport 生成 Markdown 格式的分析报告
// 整合 state.AnalysisResult 中的发现和建议
func (llm *ReActLLM) GenerateReport(ctx context.Context, state *State) (string, error) {
	logger.Info("[ReActLLM] Generating report",
		logger.Int("findings", len(state.AnalysisResult.Findings)),
		logger.Int("recommendations", len(state.AnalysisResult.Recommendations)))

	// 构建基础报告
	report := buildBasicReport(state)

	// 可选：使用 LLM 优化报告内容
	// 如果有足够的发现和建议，可以调用 LLM 进行润色
	if len(state.AnalysisResult.Findings) > 0 || len(state.AnalysisResult.Recommendations) > 0 {
		polishedReport, err := llm.polishReport(ctx, report, state)
		if err != nil {
			logger.Warn("[ReActLLM] Report polishing failed, using basic report", logger.Err(err))
			return report, nil
		}
		return polishedReport, nil
	}

	return report, nil
}

// SetTools 实现 LLM 接口（无操作，因为 ReActLLM 初始化时已设置工具）
func (llm *ReActLLM) SetTools(tools []client.Tool) {
	// ReActLLM 在初始化时已经设置了工具，这里不需要处理
	logger.Debug("[ReActLLM] SetTools called but ignored (tools set at initialization)")
}

// createChatModel 创建 ChatModel
func createChatModel(ctx context.Context, llmConfig *config.LLMConfig) (*openai.ChatModel, error) {
	// 添加诊断日志：检查传入的配置
	logger.Debug("[ReActLLM] Creating chat model",
		logger.String("model", llmConfig.Model),
		logger.String("base_url", llmConfig.BaseURL),
		logger.String("provider", llmConfig.Provider),
		logger.String("has_api_key", fmt.Sprintf("%t", llmConfig.APIKey != "")))

	// 使用 eino-ext 的 openai ChatModel
	chatModel, err := openai.NewChatModel(ctx, &openai.ChatModelConfig{
		Model:   llmConfig.Model,
		APIKey:  llmConfig.APIKey,
		BaseURL: llmConfig.BaseURL,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create openai chat model: %w", err)
	}

	return chatModel, nil
}

// convertToToolInfo 将 BaseTool 转换为 schema.ToolInfo
func convertToToolInfo(tools []tool.BaseTool) []*schema.ToolInfo {
	result := make([]*schema.ToolInfo, 0, len(tools))
	for _, t := range tools {
		info, err := t.Info(context.Background())
		if err != nil {
			logger.Warn("[ReActLLM] Failed to get tool info",
				logger.Err(err))
			continue
		}
		result = append(result, info)
	}
	return result
}

// getReActMessageModifier 返回消息修饰器
func getReActMessageModifier() react.MessageModifier {
	return react.NewPersonaModifier(getReActSystemPrompt())
}

// getReActSystemPrompt 返回 ReAct Agent 的系统提示词
func getReActSystemPrompt() string {
	return `你是一个 Kubernetes 故障排查专家。

## 动态获取数据模式
你采用动态获取数据的模式进行分析。初始阶段只会收集基础的 Pod 和 Deployment 信息。
如果你需要更多信息（如 Services、Events、Logs、ConfigMaps 等），请使用可用的工具主动获取。

## 可用工具
你可以使用以下工具来获取所需信息：
- list_services: 列出 Service 信息
- get_events: 获取集群事件
- list_pods: 列出 Pod 信息
- get_pod_logs: 获取 Pod 日志
- list_configmaps: 列出 ConfigMap 信息
- 以及其他可用工具

## 分析要求
1. 首先分析已提供的基础数据
2. 如果需要更多信息（Services、Events 等），使用工具主动获取
3. 如果已有足够信息进行诊断，直接提供分析结果
4. 最终回复必须是符合以下 JSON 格式的分析结果：
   - findings: 发现的问题列表，每个问题包含 severity(严重程度), resource(资源名称), message(问题描述)
   - recommendations: 建议列表，每个建议包含 action(操作), reason(原因), priority(优先级), command(可选的修复命令)

## 输出格式
请以 JSON 格式输出最终分析结果，例如：
{
  "findings": [
    {"severity": "High", "resource": "pod-name", "message": "问题描述"}
  ],
  "recommendations": [
    {"action": "操作描述", "reason": "原因", "priority": "High", "command": "修复命令"}
  ]
}`
}

// buildReActPrompt 构建 ReAct Agent 的提示词
func buildReActPrompt(errorContext ErrorContext) string {
	var sb strings.Builder

	sb.WriteString("请分析以下 Pod 错误：\n\n")

	// 添加 Pod 信息
	sb.WriteString("**Pod 信息**:\n")
	sb.WriteString(fmt.Sprintf("- Pod 名称: %s\n", errorContext.PodName))
	sb.WriteString(fmt.Sprintf("- 命名空间: %s\n", errorContext.Namespace))
	sb.WriteString(fmt.Sprintf("- 状态: %s\n", errorContext.Status))
	sb.WriteString(fmt.Sprintf("- 重启次数: %d\n\n", errorContext.Restarts))

	// 添加日志信息
	sb.WriteString("**Pod 日志**:\n")
	if errorContext.Logs != "" {
		sb.WriteString("```\n")
		sb.WriteString(errorContext.Logs)
		sb.WriteString("\n```\n\n")
	} else {
		sb.WriteString("*无可用日志*\n\n")
	}

	// 添加事件信息
	sb.WriteString("**相关事件**:\n")
	if len(errorContext.Events) > 0 {
		for _, event := range errorContext.Events {
			sb.WriteString(fmt.Sprintf("- %s\n", event))
		}
	} else {
		sb.WriteString("*无相关事件*\n")
	}

	sb.WriteString("\n请分析上述信息，如果需要更多信息，请使用工具获取。\n")

	return sb.String()
}

// parseReActResponse 解析 ReAct Agent 的响应
func parseReActResponse(content string) (AnalysisResult, error) {
	// 1. 提取 JSON 内容（处理可能的 Markdown 代码块标记）
	jsonContent := extractJSON(content)

	// 2. 解析 JSON
	var result AnalysisResult
	if err := json.Unmarshal([]byte(jsonContent), &result); err != nil {
		// 如果解析失败，尝试构建一个基本的错误结果
		logger.Warn("[ReActLLM] Failed to parse analysis result JSON",
			logger.Err(err),
			logger.String("content", content))

		return AnalysisResult{
			Findings: []Finding{
				{
					Severity:  "Medium",
					Message:   "分析完成，但在解析结果时出错。请查看原始输出。",
					Timestamp: time.Now(),
				},
			},
		}, nil
	}

	return result, nil
}

// extractJSON 从字符串中提取 JSON 部分
func extractJSON(s string) string {
	// 尝试查找 JSON 块的开始和结束
	// 支持 ```json ... ``` 格式
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start != -1 && end != -1 && end > start {
		return s[start : end+1]
	}

	// 尝试查找数组格式
	start = strings.Index(s, "[")
	end = strings.LastIndex(s, "]")
	if start != -1 && end != -1 && end > start {
		return s[start : end+1]
	}

	return s
}

// buildDecisionPrompt 构建决策提示词
// 描述当前状态并要求 LLM 选择下一步行动
func buildDecisionPrompt(state *State) string {
	var sb strings.Builder

	sb.WriteString("你是一个 Kubernetes 集群诊断决策专家。根据当前收集的信息，你需要决定下一步的行动。\n\n")

	// 当前迭代信息
	sb.WriteString("## 当前状态\n")
	sb.WriteString(fmt.Sprintf("- 迭代次数: %d/%d\n", state.IterationCount, state.MaxIterations))
	sb.WriteString(fmt.Sprintf("- 用户查询: %s\n\n", state.UserInput))

	// 已收集的资源信息
	sb.WriteString("## 已收集的资源\n")
	sb.WriteString(fmt.Sprintf("- Pod 数量: %d\n", len(state.K8sInfo.Pods)))
	sb.WriteString(fmt.Sprintf("- Deployment 数量: %d\n", len(state.K8sInfo.Deployments)))
	sb.WriteString(fmt.Sprintf("- 已执行命令: %d\n\n", len(state.AnalysisResult.ExecutedCommands)))

	// 当前问题
	sb.WriteString("## 当前问题\n")

	// 检查异常 Pod
	var abnormalPods []string
	for _, pod := range state.K8sInfo.Pods {
		if pod.Status == "Error" || pod.Status == "CrashLoopBackOff" || pod.Status == "Pending" {
			abnormalPods = append(abnormalPods, fmt.Sprintf("%s (%s, 重启: %d)", pod.Name, pod.Status, pod.Restarts))
		}
	}
	if len(abnormalPods) > 0 {
		sb.WriteString("异常 Pod:\n")
		for _, p := range abnormalPods {
			sb.WriteString(fmt.Sprintf("- %s\n", p))
		}
	} else {
		sb.WriteString("- 无明显异常 Pod\n")
	}

	sb.WriteString("\n## 决策选项\n")
	sb.WriteString("请根据当前状态选择以下一种决策：\n")
	sb.WriteString("- `report`: 已收集足够信息，生成分析报告\n")
	sb.WriteString("- `deep_query`: 需要进一步查询更多信息（如果需要获取 Services 或 Events 信息，请使用 list_services 或 get_events 工具）\n")
	sb.WriteString("- `continue`: 继续当前操作\n\n")

	sb.WriteString("请只输出一个决策选项（report/deep_query/continue），不要输出其他内容。\n")

	return sb.String()
}

// parseDecisionResponse 解析 LLM 返回的决策响应
func parseDecisionResponse(content string) (Decision, error) {
	// 清理响应内容
	content = strings.TrimSpace(content)
	content = strings.ToLower(content)

	// 提取决策
	if strings.Contains(content, "report") {
		return DecisionReport, nil
	}
	if strings.Contains(content, "deep_query") || strings.Contains(content, "deep") {
		return DecisionDeepQuery, nil
	}
	if strings.Contains(content, "continue") {
		return DecisionContinue, nil
	}

	// 默认返回报告决策
	return DecisionReport, nil
}

// extractPodLogs 提取指定 Pod 的日志
func extractPodLogs(state *State, podName string) string {
	for _, log := range state.K8sInfo.Logs {
		if log.PodName == podName {
			return log.Message
		}
	}
	return ""
}

// extractPodEvents 提取指定 Pod 相关的事件
// 注意：在新的动态获取模式下，事件需要通过工具获取
func extractPodEvents(state *State, podName string) []string {
	// 在新的动态获取模式下，事件通过 LLM 使用工具获取
	// 此处返回空切片
	return []string{}
}

// formatEventsForAnalysis 格式化事件列表用于分析
// 注意：在新的动态获取模式下，事件通过工具获取
func formatEventsForAnalysis(events []EventInfo) []string {
	// 在新的动态获取模式下，事件通过 LLM 使用工具获取
	// 此处返回空切片
	return []string{}
}

// formatAnalysisResult 将 AnalysisResult 格式化为字符串
func formatAnalysisResult(result AnalysisResult) string {
	var sb strings.Builder

	sb.WriteString("## 分析结果\n\n")

	// 发现的问题
	if len(result.Findings) > 0 {
		sb.WriteString("### 发现的问题\n")
		for _, finding := range result.Findings {
			sb.WriteString(fmt.Sprintf("- **[%s]** %s: %s\n", finding.Severity, finding.Resource, finding.Message))
		}
		sb.WriteString("\n")
	}

	// 建议
	if len(result.Recommendations) > 0 {
		sb.WriteString("### 建议\n")
		for _, rec := range result.Recommendations {
			sb.WriteString(fmt.Sprintf("- **[%s]** %s\n  - 原因: %s\n", rec.Priority, rec.Action, rec.Reason))
			if rec.Command != "" {
				sb.WriteString(fmt.Sprintf("  - 命令: `%s`\n", rec.Command))
			}
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// buildBasicReport 构建基础 Markdown 报告
func buildBasicReport(state *State) string {
	var sb strings.Builder

	sb.WriteString("# Kubernetes 集群分析报告\n\n")

	// 基本信息
	sb.WriteString("## 基本信息\n\n")
	sb.WriteString(fmt.Sprintf("- **用户查询**: %s\n", state.UserInput))
	sb.WriteString(fmt.Sprintf("- **命名空间**: %s\n", state.K8sInfo.Namespace))
	sb.WriteString(fmt.Sprintf("- **迭代次数**: %d/%d\n\n", state.IterationCount, state.MaxIterations))

	// 资源统计
	sb.WriteString("## 资源统计\n\n")
	sb.WriteString(fmt.Sprintf("- Pod 数量: %d\n", len(state.K8sInfo.Pods)))
	sb.WriteString(fmt.Sprintf("- Deployment 数量: %d\n", len(state.K8sInfo.Deployments)))
	sb.WriteString(fmt.Sprintf("- 已执行命令: %d\n\n", len(state.AnalysisResult.ExecutedCommands)))

	// 发现的问题
	sb.WriteString("## 发现的问题\n\n")
	if len(state.AnalysisResult.Findings) > 0 {
		for _, finding := range state.AnalysisResult.Findings {
			sb.WriteString(fmt.Sprintf("### %s\n", finding.Resource))
			sb.WriteString(fmt.Sprintf("- **严重程度**: %s\n", finding.Severity))
			sb.WriteString(fmt.Sprintf("- **描述**: %s\n", finding.Message))
			sb.WriteString(fmt.Sprintf("- **时间**: %s\n\n", finding.Timestamp.Format("2006-01-02 15:04:05")))
		}
	} else {
		sb.WriteString("*未发现明显问题*\n\n")
	}

	// 建议
	sb.WriteString("## 建议\n\n")
	if len(state.AnalysisResult.Recommendations) > 0 {
		for i, rec := range state.AnalysisResult.Recommendations {
			sb.WriteString(fmt.Sprintf("### %d. %s\n", i+1, rec.Action))
			sb.WriteString(fmt.Sprintf("- **优先级**: %s\n", rec.Priority))
			sb.WriteString(fmt.Sprintf("- **原因**: %s\n", rec.Reason))
			if rec.Command != "" {
				sb.WriteString(fmt.Sprintf("- **建议命令**: `%s`\n", rec.Command))
			}
			sb.WriteString("\n")
		}
	} else {
		sb.WriteString("*暂无建议*\n\n")
	}

	// 执行命令历史
	if len(state.AnalysisResult.ExecutedCommands) > 0 {
		sb.WriteString("## 执行命令历史\n\n")
		for _, cmd := range state.AnalysisResult.ExecutedCommands {
			status := "✅ 成功"
			if !cmd.Success {
				status = "❌ 失败"
			}
			sb.WriteString(fmt.Sprintf("### %s\n", status))
			sb.WriteString(fmt.Sprintf("```\n%s\n```\n", cmd.Command))
			if cmd.Output != "" {
				sb.WriteString(fmt.Sprintf("输出:\n```\n%s\n```\n\n", cmd.Output))
			}
		}
	}

	return sb.String()
}

// polishReport 使用 LLM 优化报告内容
// 对已格式化的报告进行润色和改进
func (llm *ReActLLM) polishReport(ctx context.Context, basicReport string, state *State) (string, error) {
	var sb strings.Builder

	sb.WriteString("请优化以下 Kubernetes 分析报告，使其更加清晰、专业。\n\n")
	sb.WriteString("## 当前报告\n")
	sb.WriteString(basicReport)
	sb.WriteString("\n\n请直接输出优化后的报告内容，不要添加解释。\n")

	// 调用 LLM 生成优化后的报告
	messages := []*schema.Message{
		schema.UserMessage(sb.String()),
	}

	resp, err := llm.chatModel.Generate(ctx, messages)
	if err != nil {
		return "", err
	}

	return resp.Content, nil
}
