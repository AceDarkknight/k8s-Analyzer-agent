// Package analysis 提供 ReAct LLM 实现
// 基于 Eino ReAct Agent，支持动态工具调用
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

// ReActLLM 基于 Eino ReAct Agent
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
// 使用 Eino ReAct Agent，支持动态工具调用
func NewReActLLM(ctx context.Context, k8sClient K8sClient, safetyAgent SafetyAgent, llmConfig *config.LLMConfig) (*ReActLLM, error) {
	logger.Info("[ReActLLM] Initializing ReAct LLM",
		logger.String("model", llmConfig.Model),
		logger.String("provider", llmConfig.Provider))

	// 1. Create ChatModel
	chatModel, err := createChatModel(ctx, llmConfig)
	if err != nil {
		logger.Error("[ReActLLM] Failed to create chat model", logger.Err(err))
		return nil, fmt.Errorf("failed to create chat model: %w", err)
	}

	// 2. Wrap K8s tools
	k8sTools, err := WrapK8sTools(ctx, k8sClient)
	if err != nil {
		logger.Error("[ReActLLM] Failed to wrap K8s tools", logger.Err(err))
		return nil, fmt.Errorf("failed to wrap K8s tools: %w", err)
	}

	// 3. Wrap SafetyAgent tool
	safetyTool := WrapSafetyAgent(safetyAgent)
	allTools := append(k8sTools, safetyTool)

	logger.Info("[ReActLLM] Tools prepared",
		logger.Int("k8s_tools", len(k8sTools)),
		logger.Int("total_tools", len(allTools)))

	// 4. Bind tools to model (using WithTools)
	toolCallingModel, err := chatModel.WithTools(convertToToolInfo(allTools))
	if err != nil {
		logger.Error("[ReActLLM] Failed to bind tools to model", logger.Err(err))
		return nil, fmt.Errorf("failed to bind tools to model: %w", err)
	}

	// 5. Build ReAct agent
	agent, err := react.NewAgent(ctx, &react.AgentConfig{
		ToolCallingModel: toolCallingModel,
		ToolsConfig: compose.ToolsNodeConfig{
			Tools: allTools,
		},
		MaxStep:         10, // Prevent infinite loops
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
// 包含重试逻辑：最多重试 3 次，延迟分别为 1s, 2s, 4s
func (llm *ReActLLM) AnalyzeError(ctx context.Context, errorContext ErrorContext) (AnalysisResult, error) {
	logger.Info("[ReActLLM] Analyzing error context",
		logger.String("pod", errorContext.PodName),
		logger.String("namespace", errorContext.Namespace),
		logger.String("status", errorContext.Status))

	// Adding diagnostic log: check model configuration
	logger.Debug("[ReActLLM] Model config for analysis",
		logger.String("model", llm.config.Model),
		logger.String("base_url", llm.config.BaseURL),
		logger.String("has_api_key", fmt.Sprintf("%t", llm.config.APIKey != "")))

	// Build prompt with collected data
	prompt := buildReActPrompt(errorContext)

	// Construct initial message
	message := schema.UserMessage(prompt)

	logger.Debug("[ReActLLM] Starting ReAct agent generation",
		logger.String("prompt_length", fmt.Sprintf("%d", len(prompt))))

	// Record LLM request log
	logger.Info("[ReActLLM] Request (AnalyzeError)",
		logger.String("role", string(message.Role)),
		logger.String("content", truncateForLog(prompt, 2000)))

	// Retry configuration
	maxRetries := 3
	retryDelays := []time.Duration{
		1 * time.Second, 2 * time.Second, 4 * time.Second}

	// Attempt generation, with retry
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
			// Record LLM response log
			logLLMResponse("AnalyzeError", finalMsg)
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

	// Parse response to AnalysisResult
	return parseReActResponse(finalMsg.Content)
}

// MakeDecision 使用 LLM 根据当前状态做出决策
// 分析状态并选择下一个行动
// 返回包含决策、推理和工具调用的 DecisionResult
func (llm *ReActLLM) MakeDecision(ctx context.Context, state *State) (*DecisionResult, error) {
	logger.Info("[ReActLLM] Making decision", logger.Int("iteration", state.IterationCount))

	// Precheck: if max iterations reached, force report generation to avoid infinite loop
	// Uses state.MaxIterations as limit (default 10)
	if state.IterationCount >= state.MaxIterations {
		logger.Info("[ReActLLM] Max iterations reached, forcing report generation",
			logger.Int("iteration", state.IterationCount),
			logger.Int("max", state.MaxIterations))
		return &DecisionResult{
			Decision:  DecisionReport,
			Reasoning: "已达最大迭代次数",
			ToolCalls: nil,
		}, nil
	}

	// Build decision prompt
	prompt := buildDecisionPrompt(state)

	// Construct messages
	messages := []*schema.Message{
		schema.UserMessage(prompt),
	}

	// Record LLM request log
	logger.Info("[ReActLLM] Request (MakeDecision)",
		logger.Int("message_count", len(messages)),
		logger.String("content", truncateForLog(prompt, 2000)))

	// Call LLM to generate decision
	resp, err := llm.chatModel.Generate(ctx, messages)
	if err != nil {
		logger.Error("[ReActLLM] LLM决策生成失败", logger.Err(err))
		// Fallback to rule-based decision
		return &DecisionResult{
			Decision:  DecisionReport,
			Reasoning: "LLM调用失败，降级到报告决策",
			ToolCalls: nil,
		}, nil
	}

	// Record LLM response log
	logLLMResponse("MakeDecision", resp)

	// Parse response to get decision
	decisionResult, err := parseDecisionResponseToResult(resp.Content)
	if err != nil {
		logger.Warn("[ReActLLM] Failed to parse decision response, using default", logger.Err(err))
		return &DecisionResult{
			Decision:  DecisionReport,
			Reasoning: "解析决策响应失败，使用默认报告决策",
			ToolCalls: nil,
		}, nil
	}

	logger.Info("[ReActLLM] Decision made", logger.String("decision", string(decisionResult.Decision)))
	return decisionResult, nil
}

// Analyze 使用 ReAct Agent 进行深度分析
// 将 State 转换为 ErrorContext 并使用 ReAct Agent 进行分析
func (llm *ReActLLM) Analyze(ctx context.Context, state *State) (string, error) {
	logger.Info("[ReActLLM] Performing deep analysis",
		logger.Int("iteration", state.IterationCount),
		logger.Int("pods", len(state.K8sInfo.Resources["Pods"])))

	// If there are error pods, prioritize analyzing them
	pods := state.K8sInfo.Resources["Pods"]
	for _, pod := range pods {
		podInfo, ok := pod.(PodInfo)
		if !ok {
			continue
		}
		if podInfo.Status == "Error" || podInfo.Status == "CrashLoopBackOff" {
			errorContext := ErrorContext{
				PodName:   podInfo.Name,
				Namespace: podInfo.Namespace,
				Status:    podInfo.Status,
				Restarts:  podInfo.Restarts,
				Logs:      extractPodLogs(state, podInfo.Name),
				Events:    []string{}, // Events need to be obtained through tools
			}

			// Call AnalyzeError for deep analysis
			analysisResult, err := llm.AnalyzeError(ctx, errorContext)
			if err != nil {
				logger.Warn("[ReActLLM] Deep analysis failed", logger.Err(err))
				continue
			}

			// Return the analysis result as a string
			return formatAnalysisResult(analysisResult), nil
		}
	}

	// If no error pods, analyze the whole state
	errorContext := ErrorContext{
		PodName:   "general",
		Namespace: state.K8sInfo.Namespace,
		Status:    "analyzing",
		Restarts:  0,
		Logs:      "",
		Events:    []string{}, // Events need to be obtained through tools
	}

	// Call AnalyzeError for analysis
	analysisResult, err := llm.AnalyzeError(ctx, errorContext)
	if err != nil {
		logger.Warn("[ReActLLM] General analysis failed", logger.Err(err))
		return "", err
	}

	return formatAnalysisResult(analysisResult), nil
}

// GenerateReport 生成 Markdown 格式的分析报告
// 集成 state.AnalysisResult 中的发现和建议
func (llm *ReActLLM) GenerateReport(ctx context.Context, state *State) (string, error) {
	logger.Info("[ReActLLM] Generating report",
		logger.Int("findings", len(state.AnalysisResult.Findings)),
		logger.Int("recommendations", len(state.AnalysisResult.Recommendations)))

	// Build base report
	report := buildBasicReport(state)

	// Optional: Use LLM to polish report content
	// If there are enough findings and recommendations, call LLM for refinement
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

// SetTools implements LLM interface (no operation, because ReActLLM is initialized with tools)
func (llm *ReActLLM) SetTools(tools []client.Tool) {
	// ReActLLM is initialized with tools, so this is a no-op
	logger.Debug("[ReActLLM] SetTools called but ignored (tools set at initialization)")
}

// SynthesizeReport 生成高质量的综合报告
// 使用结构化 Prompt 根据用户输入、发现的问题、执行的命令和 K8s 资源摘要生成报告
func (llm *ReActLLM) SynthesizeReport(ctx context.Context, userInput string, findings []Finding, commands []CommandExecution, k8sSummary string) (string, error) {
	logger.Info("[ReActLLM] Synthesizing report",
		logger.String("userInput", userInput),
		logger.Int("findings", len(findings)),
		logger.Int("commands", len(commands)))

	// 构建结构化 Prompt，使用计划文档 4.2.3 节中的模板
	prompt := buildSynthesizePrompt(userInput, findings, commands, k8sSummary)

	// 构造消息
	messages := []*schema.Message{
		(schema.UserMessage)(prompt),
	}

	// 记录 LLM 请求日志
	logger.Info("[ReActLLM] Request (SynthesizeReport)",
		logger.Int("message_count", len(messages)),
		logger.String("content", truncateForLog(prompt, 2000)))

	// 调用 LLM 生成报告
	resp, err := llm.chatModel.Generate(ctx, messages)
	if err != nil {
		logger.Error("[ReActLLM] SynthesizeReport failed", logger.Err(err))
		return "", err
	}

	// 记录 LLM 响应日志
	logLLMResponse("SynthesizeReport", resp)

	return resp.Content, nil
}

// buildSynthesizePrompt 构建用于生成综合报告的结构化 Prompt
// 使用计划文档 4.2.3 节中的模板
func buildSynthesizePrompt(userInput string, findings []Finding, commands []CommandExecution, k8sSummary string) string {
	var sb strings.Builder

	// Role
	sb.WriteString("# Role\n")
	sb.WriteString("你是一个资深的 Kubernetes 运维专家，负责根据排查过程生成最终诊断报告。\n\n")

	// Input Data
	sb.WriteString("# Input Data\n")

	// 用户原始查询
	sb.WriteString(fmt.Sprintf("- 用户原始查询: %s\n", userInput))

	// 关键发现 (Findings)
	sb.WriteString("- 关键发现 (Findings):\n")
	if len(findings) > 0 {
		for _, f := range findings {
			sb.WriteString(fmt.Sprintf("  - [%s] %s: %s (时间: %s)\n",
				f.Severity, f.Resource, f.Message, f.Timestamp.Format("2006-01-02 15:04:05")))
		}
	} else {
		sb.WriteString("  - 无发现\n")
	}

	// 核心执行步骤 (Filtered Commands)
	sb.WriteString("- 核心执行步骤 (Filtered Commands):\n")
	if len(commands) > 0 {
		// 只显示成功的命令或关键的失败命令
		for _, cmd := range commands {
			status := "成功"
			if !cmd.Success {
				status = "失败"
			}
			sb.WriteString(fmt.Sprintf("  - [%s] %s\n", status, cmd.Command))
			if cmd.Output != "" && len(cmd.Output) < 200 {
				sb.WriteString(fmt.Sprintf("    输出: %s\n", cmd.Output))
			}
		}
	} else {
		sb.WriteString("  - 无执行命令\n")
	}

	// 资源状态摘要
	sb.WriteString(fmt.Sprintf("- 资源状态摘要: %s\n\n", k8sSummary))

	// Output Format (Strict Markdown)
	sb.WriteString("# Output Format (Strict Markdown)\n")
	sb.WriteString("请按以下结构输出报告：\n\n")
	sb.WriteString("## 2. Findings (详细发现)\n")
	sb.WriteString("[按严重程度(Critical/High/Medium)列出所有技术发现，需引用具体的资源名称和错误信息]\n")
	sb.WriteString("**验证状态标注规则**：\n")
	sb.WriteString("- 如果发现是基于成功执行的命令（CommandExecution.Success=true）得出的结论，标注为 ✅ Verified（已验证事实）\n")
	sb.WriteString("- 如果发现仅基于 K8s MCP 工具数据或推测，标注为 ⚠️ Inferred（推测性发现）\n")
	sb.WriteString("- 在每个发现后面加上验证状态标记，例如：- **[Critical] ✅ Verified** 或 **⚠️ Inferred**\n\n")

	return sb.String()
}

// createChatModel creates ChatModel
func createChatModel(ctx context.Context, llmConfig *config.LLMConfig) (*openai.ChatModel, error) {
	// Adding diagnostic log: check the provided configuration
	logger.Debug("[ReActLLM] Creating chat model",
		logger.String("model", llmConfig.Model),
		logger.String("base_url", llmConfig.BaseURL),
		logger.String("provider", llmConfig.Provider),
		logger.String("has_api_key", fmt.Sprintf("%t", llmConfig.APIKey != "")))

	// Use eino-ext's openai ChatModel
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
你采用动态获取数据的模式进行分析。初始阶段只会收集基础的 Pod 信息。
如果你需要更多信息（如 Services、Events、Logs、ConfigMaps 等），请使用可用的工具主动获取。

## 工具使用规范（重要）
你必须严格遵守以下规则来调用工具：

1. **严格遵循 JSON Schema**: 每个工具都有对应的 JSON Schema 定义参数格式。你必须严格按照该 Schema 提供参数。
2. **始终检查当前可用工具列表**: 每次调用工具前，请查看系统提供的工具列表，使用准确的工具名称。
3. **不要猜测工具名称或参数键**: 不同工具的参数名称可能不同（例如 pod_name vs name，namespace vs ns）。必须根据 Schema 中定义的参数名称来提供参数。
4. **参数名称和必填字段必须与 Schema 完全匹配**: 在调用工具前，请仔细阅读工具的 Schema，确保所有必填参数都已提供，且参数名称完全正确。
5. **错误处理和重试策略**: 如果工具调用返回错误或空结果，请分析原因（例如工具不存在、权限不足或参数错误），并尝试使用不同的工具或调整参数再次尝试，不要陷入重复调用同一错误命令的死循环。

## 输出格式要求
你的响应必须严格遵守以下 JSON Schema：
{
  "thought": "分析当前情况、历史记录并决定下一步行动的推理过程。",
  "decision": "continue" 或 "report",
  "tool_calls": [
    {
      "name": "工具名称（必须与可用工具列表中的名称完全一致）",
      "arguments": {
        // 参数必须严格遵循工具的 JSON Schema 定义
      }
    }
  ]
}

## 通用诊断协议（强制执行）
你必须严格遵守以下诊断协议：

### 1. 强制思维链 - 可验证性评估
- 每次生成 "thought" 时，必须包含对"可验证性"的评估
- 评估内容：你提出的分析结论或建议是否可以通过现有工具进行验证？
- 如果可以验证，必须在下一步调用相应的验证工具
- 如果无法验证（如缺少必要工具），请明确标注为"推测性发现"

### 2. 降级探测指引（当 MCP 工具失败或信息不足时）
采用以下分层降级策略：
- **第一层：MCP 服务/工具调用** - 使用 K8s MCP 工具（如 list_pods, describe_pod, get_pod_logs）
- **第二层：运行时探测** - 当 MCP 工具失败或返回错误时，使用容器运行时工具（docker/crictl）查看容器实体的存活状态与真实日志
- **第三层：系统工具探测** - 当运行时探测也无法解决问题时，使用宿主机系统工具（ps, netstat, df, dmesg, ip, ss）

**降级条件**：
- MCP 服务不可用或返回错误
- 状态显示为 Unknown
- 权限问题导致无法获取信息
- 信息不足以定位根因

**降级路径示例**：
步骤 1: 尝试 K8s MCP 工具调用
↓ (若失败或信息不足)
步骤 2: 降级到运行时探测 (docker/crictl logs)
↓ (若仍无法解决)
步骤 3: 降级到系统工具 (ps, netstat, df, dmesg)

### 3. 修复即验证原则
- 如果你计划提出修复建议 A，必须检索工具列表
- 若存在能验证 A 是否生效的工具 B，必须执行 B 后才能生成报告
- 禁止在有验证手段时提供仅基于推测的建议

## 决策规则
- decision 为 "continue" 时：需要调用工具获取更多信息
- decision 为 "report" 时：已收集足够信息，可以生成分析报告
- tool_calls：列出需要执行的工具调用（仅当 decision 为 "continue" 时需要）
- 避免重复调用已执行过的工具，参考推理历史做出决策

## 分析要求
1. 首先分析已提供的基础数据
2. 如果需要更多信息，使用工具主动获取（必须根据 Schema 提供正确参数）
3. 如果已有足够信息进行诊断，直接提供分析结果
4. 最终回复必须是符合上述 JSON 格式的分析结果：
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

	// Add Pod information
	sb.WriteString("**Pod 信息**:\n")
	sb.WriteString(fmt.Sprintf("- Pod 名称: %s\n", errorContext.PodName))
	sb.WriteString(fmt.Sprintf("- 命名空间: %s\n", errorContext.Namespace))
	sb.WriteString(fmt.Sprintf("- 状态: %s\n", errorContext.Status))
	sb.WriteString(fmt.Sprintf("- 重启次数: %d\n\n", errorContext.Restarts))

	// Add log information
	sb.WriteString("**Pod 日志**:\n")
	if errorContext.Logs != "" {
		sb.WriteString("```\n")
		sb.WriteString(errorContext.Logs)
		sb.WriteString("\n```\n\n")
	} else {
		sb.WriteString("*无可用日志*\n\n")
	}

	// Add event information
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

// parseReActResponse parses ReAct Agent's response
func parseReActResponse(content string) (AnalysisResult, error) {
	// 1. Extract JSON content (handling possible Markdown code block markers)
	jsonContent := extractJSON(content)

	// 2. Parse JSON
	var result AnalysisResult
	if err := json.Unmarshal([]byte(jsonContent), &result); err != nil {
		// If parsing fails, try to build a basic error result
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

// extractJSON extracts JSON portion from a string
func extractJSON(s string) string {
	// Try to find JSON block start and end
	// Supports ```json ... ``` format
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start != -1 && end != -1 && end > start {
		return s[start : end+1]
	}

	// Try to find array format
	start = strings.Index(s, "[")
	end = strings.LastIndex(s, "]")
	if start != -1 && end != -1 && end > start {
		return s[start : end+1]
	}

	return s
}

// buildDecisionPrompt 构建决策提示词
// 描述当前状态并要求 LLM 选择下一个行动
// 使用计划文档第 5 节中定义的模板格式
func buildDecisionPrompt(state *State) string {
	var sb strings.Builder

	// Add system role description
	sb.WriteString("你是一个 Kubernetes 诊断代理。你将收到当前状态和你之前的行动历史。根据此历史记录做出下一个决定，以避免循环和冗余检查。\n\n")

	// Context section
	sb.WriteString("## 上下文\n")
	sb.WriteString(fmt.Sprintf("用户查询: %s\n\n", state.UserInput))

	// Reasoning history section - using the template format in the plan document
	sb.WriteString("## 推理历史 (Reasoning History)\n")
	if len(state.ReasoningHistory) > 0 {
		for _, step := range state.ReasoningHistory {
			sb.WriteString(fmt.Sprintf("步骤 %d:\n", step.Iteration))
			sb.WriteString(fmt.Sprintf("思考: %s\n", step.Thought))
			sb.WriteString(fmt.Sprintf("决策: %s\n", step.Decision))
			if len(step.ToolCalls) > 0 {
				sb.WriteString("工具调用: ")
				for _, tc := range step.ToolCalls {
					sb.WriteString(fmt.Sprintf("%s ", tc.Tool))
				}
				sb.WriteString("\n")
			}
			if step.Observation != "" {
				// 检查观察结果是否包含错误关键字，如果是则突出显示
				lowerObs := strings.ToLower(step.Observation)
				if strings.Contains(lowerObs, "error") || strings.Contains(lowerObs, "failed") || strings.Contains(lowerObs, "失败") || strings.Contains(lowerObs, "错误") {
					sb.WriteString(fmt.Sprintf("观察结果: ⚠️ %s\n", step.Observation))
				} else {
					sb.WriteString(fmt.Sprintf("观察结果: %s\n", step.Observation))
				}
			}
			sb.WriteString("\n")
		}
	} else {
		sb.WriteString("*这是第一次迭代，没有推理历史*\n\n")
	}

	// Current resource section
	sb.WriteString("## 当前资源\n")
	sb.WriteString(fmt.Sprintf("Pods: %d\n", len(state.K8sInfo.Resources["Pods"])))
	sb.WriteString(fmt.Sprintf("Deployments: %d\n", len(state.K8sInfo.Resources["Deployments"])))
	sb.WriteString(fmt.Sprintf("当前迭代: %d/%d\n\n", state.IterationCount, state.MaxIterations))

	// Abnormal pod information
	var abnormalPods []string
	for _, pod := range state.K8sInfo.Resources["Pods"] {
		podInfo, ok := pod.(PodInfo)
		if !ok {
			continue
		}
		if podInfo.Status == "Error" || podInfo.Status == "CrashLoopBackOff" || podInfo.Status == "Pending" {
			abnormalPods = append(abnormalPods, fmt.Sprintf("%s (%s, 重启: %d)", podInfo.Name, podInfo.Status, podInfo.Restarts))
		}
	}
	if len(abnormalPods) > 0 {
		sb.WriteString("异常 Pod:\n")
		for _, p := range abnormalPods {
			sb.WriteString(fmt.Sprintf("- %s\n", p))
		}
		sb.WriteString("\n")
	}

	// Task section - clearly require JSON output
	sb.WriteString("## 任务\n")

	// 动态任务指令：根据 LastError 存在与否调整任务重心
	if state.LastError != nil {
		// 存在错误，偏向底层探测而非资源列举
		sb.WriteString("⚠️ 检测到错误状态：\n")
		sb.WriteString(fmt.Sprintf("错误信息: %s\n\n", state.LastError.Error()))
		sb.WriteString("**当前任务优先级**：\n")
		sb.WriteString("1. 优先分析失败原因，而不是重复列举资源\n")
		sb.WriteString("2. 考虑使用底层探测工具（execute_safe_command）获取更多信息\n")
		sb.WriteString("3. 尝试降级策略：运行时探测(docker/crictl) -> 系统工具(ps/netstat/df)\n")
		sb.WriteString("4. 如果需要获取更多 K8s 资源信息，可以使用工具\n\n")
	} else {
		sb.WriteString("决定下一步。\n\n")
	}

	sb.WriteString("返回一个 JSON 对象，必须严格遵守以下 Schema:\n")
	sb.WriteString("```json\n")
	sb.WriteString("{\n")
	sb.WriteString("  \"thought\": \"分析当前情况、历史记录并决定下一步行动的推理过程。如果之前的命令执行失败，请在 thought 中分析失败原因，并在 tool_calls 中提出替代方案或修复后的命令。\",\n")
	sb.WriteString("  \"decision\": \"continue\" | \"report\",\n")
	sb.WriteString("  \"tool_calls\": [ { \"name\": \"...\", \"arguments\": { ... } } ]\n")
	sb.WriteString("}\n")
	sb.WriteString("```\n\n")

	// Additional explanation
	sb.WriteString("## 决策说明\n")
	sb.WriteString("- continue: 需要继续调查或调用工具获取更多信息\n")
	sb.WriteString("- report: 已收集足够信息，可以生成分析报告\n")
	sb.WriteString("- tool_calls: 如果 decision 是 continue，列出需要执行的工具调用\n")
	sb.WriteString("- 可用的 K8s 工具包括: list_pods, get_pod_logs, list_events, list_services 等\n")
	sb.WriteString("- 如需执行非 K8s 操作（如网络探测），可使用: execute_safe_command\n")

	return sb.String()
}

// parseDecisionResponseToResult parses LLM's decision response to DecisionResult
// Parses JSON formatted response, extracts thought, decision, tool_calls
func parseDecisionResponseToResult(content string) (*DecisionResult, error) {
	// Clean response content
	content = strings.TrimSpace(content)
	content = strings.ToLower(content)

	// Extract JSON content (handling possible Markdown code block markers)
	jsonContent := extractJSON(content)

	// Define intermediate structure for JSON parsing
	var rawResponse struct {
		Thought   string `json:"thought"`
		Decision  string `json:"decision"`
		ToolCalls []struct {
			Name      string                 `json:"name"`
			Arguments map[string]interface{} `json:"arguments"`
		} `json:"tool_calls"`
	}

	// Try to unmarshal JSON
	if err := json.Unmarshal([]byte(jsonContent), &rawResponse); err != nil {
		logger.Warn("[ReActLLM] Failed to parse JSON response, falling back to text parsing",
			logger.Err(err))

		// JSON parsing failed, fallback to text matching
		decisionStr := parseDecisionString(content)

		return &DecisionResult{
			Decision:  decisionStr,
			Reasoning: "JSON 解析失败，使用文本匹配",
			ToolCalls: nil,
		}, nil
	}

	// Parse decision
	decision := parseDecisionString(rawResponse.Decision)

	// Convert tool calls
	var toolCalls []ToolCall
	for _, tc := range rawResponse.ToolCalls {
		toolCalls = append(toolCalls, ToolCall{
			Tool: tc.Name,
			Args: tc.Arguments,
			Type: "k8s", // Default type
		})
	}

	// Build return result
	result := &DecisionResult{
		Decision:  decision,
		Reasoning: rawResponse.Thought,
		ToolCalls: toolCalls,
	}

	logger.Debug("[ReActLLM] Parsed decision result",
		logger.String("decision", string(result.Decision)),
		logger.String("thought", truncateForLog(result.Reasoning, 200)),
		logger.Int("tool_calls", len(result.ToolCalls)))

	return result, nil
}

// parseDecisionString 解析决策字符串
// 处理各种可能的决策值
func parseDecisionString(decisionStr string) Decision {
	decisionStr = strings.ToLower(strings.TrimSpace(decisionStr))

	if decisionStr == "report" {
		return DecisionReport
	}
	if decisionStr == "continue" {
		return DecisionContinue
	}
	if decisionStr == "deep_query" || decisionStr == "deep" {
		return DecisionDeepQuery
	}

	// Default to report decision
	return DecisionReport
}

// extractPodLogs 提取指定 Pod 的日志
func extractPodLogs(state *State, podName string) string {
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
	return ""
}

// formatAnalysisResult 将 AnalysisResult 格式化为字符串
func formatAnalysisResult(result AnalysisResult) string {
	var sb strings.Builder

	sb.WriteString("## 分析结果\n\n")

	// Found issues
	if len(result.Findings) > 0 {
		sb.WriteString("### 发现的问题\n")
		for _, finding := range result.Findings {
			sb.WriteString(fmt.Sprintf("- **[%s]** %s: %s\n", finding.Severity, finding.Resource, finding.Message))
		}
		sb.WriteString("\n")
	}

	// Recommendations
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

// buildBasicReport constructs a basic Markdown report
func buildBasicReport(state *State) string {
	var sb strings.Builder

	sb.WriteString("# Kubernetes 集群分析报告\n\n")

	// Basic information
	sb.WriteString("## 基本信息\n\n")
	sb.WriteString(fmt.Sprintf("- **用户查询**: %s\n", state.UserInput))
	sb.WriteString(fmt.Sprintf("- **命名空间**: %s\n", state.K8sInfo.Namespace))
	sb.WriteString(fmt.Sprintf("- **迭代次数**: %d/%d\n\n", state.IterationCount, state.MaxIterations))

	// Resource statistics
	sb.WriteString("## 资源统计\n\n")
	sb.WriteString(fmt.Sprintf("- Pod 数量: %d\n", len(state.K8sInfo.Resources["Pods"])))
	sb.WriteString(fmt.Sprintf("- Deployment 数量: %d\n", len(state.K8sInfo.Resources["Deployments"])))
	sb.WriteString(fmt.Sprintf("- 已执行命令: %d\n\n", len(state.AnalysisResult.ExecutedCommands)))

	// Found issues
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

	// Recommendations
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

	// Command execution history
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
// 优化已经格式化好的报告
func (llm *ReActLLM) polishReport(ctx context.Context, basicReport string, state *State) (string, error) {
	var sb strings.Builder

	sb.WriteString("请优化以下 Kubernetes 分析报告，使其更加清晰、专业。\n\n")
	sb.WriteString("## 当前报告\n")
	sb.WriteString(basicReport)
	sb.WriteString("\n\n请直接输出优化后的报告内容，不要添加解释。\n")

	prompt := sb.String()

	// Call LLM to generate the polished report
	messages := []*schema.Message{
		schema.UserMessage(prompt),
	}

	// Record LLM request log
	logger.Info("[ReActLLM] Request (polishReport)",
		logger.Int("message_count", len(messages)),
		logger.String("content", truncateForLog(prompt, 2000)))

	resp, err := llm.chatModel.Generate(ctx, messages)
	if err != nil {
		logger.Error("[ReActLLM] polishReport failed", logger.Err(err))
		return "", err
	}

	// Record LLM response log
	logLLMResponse("polishReport", resp)

	return resp.Content, nil
}

// logLLMResponse 记录 LLM 响应日志
// 包括主要内容和推理内容（如果有）
func logLLMResponse(methodName string, msg *schema.Message) {
	if msg == nil {
		logger.Warn("[ReActLLM] Response is nil", logger.String("method", methodName))
		return
	}

	// Record main content
	content := msg.Content
	if content == "" {
		content = "<empty>"
	}

	// Check for reasoning/thought content
	reasoning := ""
	if msg.ReasoningContent != "" {
		reasoning = msg.ReasoningContent
	}

	if reasoning != "" {
		logger.Info(fmt.Sprintf("[ReActLLM] Response (%s)", methodName),
			logger.String("reasoning", truncateForLog(reasoning, 4000)),
			logger.String("content", truncateForLog(content, 4000)))
	} else {
		logger.Info(fmt.Sprintf("[ReActLLM] Response (%s)", methodName),
			logger.String("content", truncateForLog(content, 4000)))
	}
}

// truncateForLog 截断日志内容，避免过长
// 如果内容超过 maxLength，将被截断并添加截断提示
func truncateForLog(content string, maxLength int) string {
	if len(content) <= maxLength {
		return content
	}
	return content[:maxLength] + fmt.Sprintf("... [truncated, total length: %d]", len(content))
}
