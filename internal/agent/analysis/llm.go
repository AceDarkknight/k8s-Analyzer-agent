// Package analysis 提供 LLM 接口和实现
package analysis

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/AceDarkknight/k8s-analyzer-agent/internal/client"
	"github.com/AceDarkknight/k8s-analyzer-agent/internal/config"
	"github.com/AceDarkknight/k8s-analyzer-agent/internal/logger"
)

// SystemPrompt 系统提示词常量
const SystemPrompt = `## 系统角色

你是一个 Kubernetes 集群诊断专家 Agent。你的职责是诊断和分析 Kubernetes 集群中的问题。

## 核心职责

1. **全命名空间诊断**: 你必须诊断整个集群的所有命名空间，不要局限于单个命名空间。
2. **全面分析**: 检查所有命名空间中的 Pod、Service、Deployment、Event 等资源。
3. **问题发现**: 主动发现集群中的异常状态，如 CrashLoopBackOff、Pending、错误事件等。
4. **根因分析**: 分析问题的根本原因并提供解决建议。

## ⚠️ 重要约束 - 必须严格遵守

### 数据使用约束（最高优先级）
- **检查已有数据**: 在生成任何查询命令之前，必须先检查 Context 部分是否已包含所需数据。
- **避免重复查询**: 如果 Context 中已有 Pod/Service/Deployment 列表，不要再次生成命令获取它们。
- **直接分析**: 当已有足够数据时，直接进入分析阶段，生成 "report" 决策。
- **数据充足判断**: 如果已收集到任何 Pod、Service 或 Event 数据，应优先分析这些数据而非继续收集。

### 工具使用约束
- **禁止生成 shell 命令**: 绝对不要生成类似 'kubectl get pods' 的 shell 命令。
- **必须使用 K8s MCP 工具**: 你必须使用提供的 K8s MCP 工具（如 'list_pods', 'list_services', 'list_namespaces' 等）来获取集群信息。
- **工具名称对照**:
  - 获取命名空间列表: 使用 'list_namespaces' 工具
  - 获取 Pod 列表: 使用 'list_pods' 工具（需要 namespace 参数）
  - 获取 Service 列表: 使用 'list_services' 工具（需要 namespace 参数）
  - 获取 Deployment 列表: 使用 'list_deployments' 工具（需要 namespace 参数）
  - 获取事件: 使用 'get_events' 工具（需要 namespace 参数）

### 命名空间范围约束
- **不要假设只有 'default' 命名空间**: 集群中可能存在多个命名空间，你必须检查所有命名空间。
- **第一步必须获取命名空间**: 在收集任何资源之前，必须先调用 'list_namespaces' 获取所有命名空间列表。
- **遍历所有命名空间**: 对于获取到的每个命名空间，都要调用相应的资源列表工具。

## 诊断策略（按顺序执行）

### 步骤 1: 检查已有数据
首先检查 Context 部分是否已包含集群资源数据。如果已有数据，跳过收集步骤，直接进入分析。

### 步骤 2: 获取所有命名空间（仅在需要收集数据时）
如果需要收集数据，首先调用 'list_namespaces' 工具获取集群中所有命名空间的列表。
注意：如果 list_namespaces 不可用，系统会自动使用硬编码的命名空间列表。

### 步骤 3: 遍历每个命名空间收集资源
对于步骤 2 返回的每个命名空间，依次调用：
- 'list_pods'（参数: namespace=<命名空间名称>）
- 'list_services'（参数: namespace=<命名空间名称>）
- 'list_deployments'（参数: namespace=<命名空间名称>）
- 'get_events'（参数: namespace=<命名空间名称>）

### 步骤 4: 分析问题
重点关注：
- 非正常状态的 Pod（Error、CrashLoopBackOff、Pending）
- 高重启次数的 Pod
- Warning 类型的事件
- 资源限制和配额问题
- 网络连接问题

## 注意事项

- 使用 K8s MCP 工具执行查询，不要使用 Shell MCP 执行 kubectl 命令
- 确保诊断覆盖所有命名空间，不遗漏任何潜在问题
- 如果某个命名空间的工具调用失败，记录错误并继续处理其他命名空间
- **优先分析已有数据，而非继续收集新数据**

`

// LLM 定义大语言模型接口
// 用于决策和推理
type LLM interface {
	// MakeDecision 根据当前状态做出决策
	MakeDecision(ctx context.Context, state *State) (Decision, error)

	// Analyze 分析 K8s 信息并返回分析结果
	Analyze(ctx context.Context, state *State) (string, error)

	// GenerateReport 生成报告摘要
	GenerateReport(ctx context.Context, state *State) (string, error)

	// SetTools 设置可用的工具列表
	// 用于动态注入工具信息到 LLM Prompt 中
	SetTools(tools []client.Tool)
}

// RuleBasedLLM 基于规则的 LLM 实现
// 使用预定义规则进行决策，不需要真实的 LLM API
type RuleBasedLLM struct {
	rules     []DecisionRule
	modelName string        // 模型名称（从配置中传入）
	tools     []client.Tool // 可用的工具列表
}

// DecisionRule 决策规则
type DecisionRule struct {
	// Name 规则名称
	Name string

	// Condition 判断条件函数
	Condition func(*State) bool

	// Action 决策动作
	Action Decision

	// Priority 优先级（数字越小优先级越高）
	Priority int
}

// NewRuleBasedLLM 创建基于规则的 LLM
func NewRuleBasedLLM(llmConfig *config.LLMConfig) *RuleBasedLLM {
	llm := &RuleBasedLLM{
		rules:     make([]DecisionRule, 0),
		modelName: llmConfig.Model,
	}

	// 初始化默认规则
	llm.initDefaultRules()

	// 记录使用的模型
	logger.Info("[Analysis] RuleBasedLLM initialized",
		logger.String("model", llm.modelName),
		logger.String("provider", llmConfig.Provider))

	return llm
}

// initDefaultRules 初始化默认决策规则
func (llm *RuleBasedLLM) initDefaultRules() {
	llm.rules = []DecisionRule{
		{
			Name: "max_iterations_reached",
			Condition: func(s *State) bool {
				return s.IterationCount >= s.MaxIterations
			},
			Action:   DecisionReport,
			Priority: 1,
		},
		{
			Name: "no_command_to_execute",
			Condition: func(s *State) bool {
				return s.LastAction == "no_command"
			},
			Action:   DecisionReport,
			Priority: 2,
		},
		{
			Name: "pod_error_detected",
			Condition: func(s *State) bool {
				for _, pod := range s.K8sInfo.Pods {
					if pod.Status == "Error" || pod.Status == "CrashLoopBackOff" {
						return true
					}
				}
				return false
			},
			Action:   DecisionDeepQuery,
			Priority: 3,
		},
		{
			Name: "pod_high_restarts",
			Condition: func(s *State) bool {
				for _, pod := range s.K8sInfo.Pods {
					if pod.Restarts > 5 {
						return true
					}
				}
				return false
			},
			Action:   DecisionDeepQuery,
			Priority: 4,
		},
		{
			Name: "warning_events_detected",
			Condition: func(s *State) bool {
				for _, event := range s.K8sInfo.Events {
					if event.Type == "Warning" {
						return true
					}
				}
				return false
			},
			Action:   DecisionDeepQuery,
			Priority: 5,
		},
		{
			Name: "error_occurred",
			Condition: func(s *State) bool {
				return s.LastError != nil
			},
			Action:   DecisionReport,
			Priority: 6,
		},
		{
			Name: "has_enough_info",
			Condition: func(s *State) bool {
				// 如果已经收集了足够的信息（至少一次迭代，且有数据）
				return s.IterationCount > 0 &&
					(len(s.K8sInfo.Pods) > 0 || len(s.K8sInfo.Services) > 0)
			},
			Action:   DecisionReport,
			Priority: 7,
		},
		{
			Name: "default_continue",
			Condition: func(s *State) bool {
				return true // 默认规则，总是匹配
			},
			Action:   DecisionDeepQuery,
			Priority: 10,
		},
	}
}

// AddRule 添加自定义规则
func (llm *RuleBasedLLM) AddRule(rule DecisionRule) {
	llm.rules = append(llm.rules, rule)
}

// SetTools 设置可用的工具列表
func (llm *RuleBasedLLM) SetTools(tools []client.Tool) {
	llm.tools = tools
	logger.Info("[RuleBasedLLM] Tools set",
		logger.Int("count", len(tools)),
		logger.String("model", llm.modelName))

	// 记录格式化后的工具提示（用于验证）
	if len(tools) > 0 {
		toolsPrompt := llm.FormatToolsPrompt()
		logger.Debug("[RuleBasedLLM] Tools formatted for prompt",
			logger.String("prompt_preview", toolsPrompt))
	}
}

// FormatToolsPrompt 格式化工具列表为 LLM Prompt
// 返回包含所有工具描述的字符串，可注入到 System Prompt 中
func (llm *RuleBasedLLM) FormatToolsPrompt() string {
	var prompt strings.Builder

	// 添加系统提示词
	prompt.WriteString(SystemPrompt)

	if len(llm.tools) == 0 {
		prompt.WriteString("## 可用工具列表\n\n当前没有可用的工具。\n")
		return prompt.String()
	}

	prompt.WriteString("## 可用工具列表\n\n")
	prompt.WriteString("以下是您可以使用的工具，每个工具都有特定的功能和参数要求：\n\n")

	for i, tool := range llm.tools {
		prompt.WriteString(fmt.Sprintf("### %d. %s\n\n", i+1, tool.Name))
		prompt.WriteString(fmt.Sprintf("**描述**: %s\n\n", tool.Description))

		// 格式化 InputSchema
		if len(tool.InputSchema) > 0 {
			prompt.WriteString("**参数要求**:\n")

			// 反序列化 json.RawMessage 为 map
			var schemaMap map[string]interface{}
			if err := json.Unmarshal(tool.InputSchema, &schemaMap); err == nil {
				// 提取 properties
				if props, ok := schemaMap["properties"].(map[string]interface{}); ok {
					for paramName, paramDetails := range props {
						if detailMap, ok := paramDetails.(map[string]interface{}); ok {
							paramType := detailMap["type"]
							paramDesc := detailMap["description"]
							prompt.WriteString(fmt.Sprintf("  - `%s` (%v): %v\n", paramName, paramType, paramDesc))
						}
					}
				}
				// 提取 required 字段
				if required, ok := schemaMap["required"].([]interface{}); ok && len(required) > 0 {
					prompt.WriteString(fmt.Sprintf("  - **必需参数**: %v\n", required))
				}
			} else {
				// 如果解析失败，记录原始 Schema
				logger.Debug("[RuleBasedLLM] Failed to parse InputSchema",
					logger.String("tool", tool.Name),
					logger.Err(err))
			}
			prompt.WriteString("\n")
		}
	}

	return prompt.String()
}

// min 返回两个整数中的较小值
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// MakeDecision 根据当前状态做出决策
func (llm *RuleBasedLLM) MakeDecision(ctx context.Context, state *State) (Decision, error) {
	logger.Debug("[RuleBasedLLM] Making decision", logger.Int("iteration", state.IterationCount))

	// 按优先级排序规则（从小到大）
	sortedRules := make([]DecisionRule, len(llm.rules))
	copy(sortedRules, llm.rules)

	// 简单的冒泡排序
	for i := 0; i < len(sortedRules); i++ {
		for j := i + 1; j < len(sortedRules); j++ {
			if sortedRules[i].Priority > sortedRules[j].Priority {
				sortedRules[i], sortedRules[j] = sortedRules[j], sortedRules[i]
			}
		}
	}

	// 按优先级匹配规则
	for _, rule := range sortedRules {
		if rule.Condition(state) {
			logger.Debug("[RuleBasedLLM] Rule matched", logger.String("rule", rule.Name), logger.String("decision", string(rule.Action)))
			return rule.Action, nil
		}
	}

	// 默认决策
	logger.Debug("[RuleBasedLLM] No rule matched, using default decision")
	return DecisionDeepQuery, nil
}

// Analyze 分析 K8s 信息
func (llm *RuleBasedLLM) Analyze(ctx context.Context, state *State) (string, error) {
	logger.Debug("[RuleBasedLLM] Analyzing K8s information")

	var analysis strings.Builder

	// 分析 Pod 状态
	if len(state.K8sInfo.Pods) > 0 {
		analysis.WriteString("## Pod 状态分析\n\n")
		runningCount := 0
		errorCount := 0
		pendingCount := 0

		for _, pod := range state.K8sInfo.Pods {
			switch pod.Status {
			case "Running":
				runningCount++
			case "Error", "CrashLoopBackOff":
				errorCount++
			case "Pending":
				pendingCount++
			}
		}

		analysis.WriteString(fmt.Sprintf("- 总 Pod 数: %d\n", len(state.K8sInfo.Pods)))
		analysis.WriteString(fmt.Sprintf("- 运行中: %d\n", runningCount))
		analysis.WriteString(fmt.Sprintf("- 错误: %d\n", errorCount))
		analysis.WriteString(fmt.Sprintf("- 等待中: %d\n\n", pendingCount))

		if errorCount > 0 {
			analysis.WriteString("⚠️ 发现异常 Pod，建议查看日志\n\n")
		}
	}

	// 分析事件
	if len(state.K8sInfo.Events) > 0 {
		analysis.WriteString("## 事件分析\n\n")
		warningCount := 0

		for _, event := range state.K8sInfo.Events {
			if event.Type == "Warning" {
				warningCount++
			}
		}

		analysis.WriteString(fmt.Sprintf("- 总事件数: %d\n", len(state.K8sInfo.Events)))
		analysis.WriteString(fmt.Sprintf("- 警告事件: %d\n\n", warningCount))

		if warningCount > 0 {
			analysis.WriteString("⚠️ 发现警告事件，建议关注\n\n")
		}
	}

	// 分析网络
	if state.K8sInfo.NetworkInfo != nil && len(state.K8sInfo.NetworkInfo.Connectivity) > 0 {
		analysis.WriteString("## 网络分析\n\n")
		successCount := 0
		for _, conn := range state.K8sInfo.NetworkInfo.Connectivity {
			if conn.Success {
				successCount++
			}
		}

		analysis.WriteString(fmt.Sprintf("- 总连接测试: %d\n", len(state.K8sInfo.NetworkInfo.Connectivity)))
		analysis.WriteString(fmt.Sprintf("- 成功: %d\n", successCount))
		analysis.WriteString(fmt.Sprintf("- 失败: %d\n\n", len(state.K8sInfo.NetworkInfo.Connectivity)-successCount))

		if successCount < len(state.K8sInfo.NetworkInfo.Connectivity) {
			analysis.WriteString("⚠️ 发现网络连接问题\n\n")
		}
	}

	return analysis.String(), nil
}

// GenerateReport 生成报告摘要
func (llm *RuleBasedLLM) GenerateReport(ctx context.Context, state *State) (string, error) {
	logger.Debug("[RuleBasedLLM] Generating report summary")

	var summary strings.Builder

	summary.WriteString("## 分析摘要\n\n")
	summary.WriteString(fmt.Sprintf("**用户查询**: %s\n\n", state.UserInput))
	summary.WriteString(fmt.Sprintf("**命名空间**: %s\n\n", state.K8sInfo.Namespace))
	summary.WriteString(fmt.Sprintf("**迭代次数**: %d/%d\n\n", state.IterationCount, state.MaxIterations))

	// 统计信息
	summary.WriteString("### 统计信息\n\n")
	summary.WriteString(fmt.Sprintf("- Pod 数量: %d\n", len(state.K8sInfo.Pods)))
	summary.WriteString(fmt.Sprintf("- Service 数量: %d\n", len(state.K8sInfo.Services)))
	summary.WriteString(fmt.Sprintf("- Deployment 数量: %d\n", len(state.K8sInfo.Deployments)))
	summary.WriteString(fmt.Sprintf("- 事件数量: %d\n", len(state.K8sInfo.Events)))
	summary.WriteString(fmt.Sprintf("- 日志条目: %d\n", len(state.K8sInfo.Logs)))
	summary.WriteString(fmt.Sprintf("- 执行命令: %d\n\n", len(state.AnalysisResult.ExecutedCommands)))

	// 状态
	summary.WriteString("### 分析状态\n\n")
	switch state.AnalysisResult.Status {
	case StatusCompleted:
		summary.WriteString("✅ 分析完成\n\n")
	case StatusPartial:
		summary.WriteString("⚠️ 部分完成（达到最大迭代次数）\n\n")
	case StatusFailed:
		summary.WriteString("❌ 分析失败\n\n")
	default:
		summary.WriteString("🔄 分析进行中\n\n")
	}

	return summary.String(), nil
}

// MockLLM 模拟 LLM 实现
// 用于测试和演示，返回预设的响应
type MockLLM struct {
	tools []client.Tool // 可用的工具列表
}

// NewMockLLM 创建 Mock LLM
func NewMockLLM() *MockLLM {
	return &MockLLM{}
}

// MakeDecision 模拟决策
func (m *MockLLM) MakeDecision(ctx context.Context, state *State) (Decision, error) {
	logger.Debug("[MockLLM] Making decision", logger.Int("iteration", state.IterationCount))

	// 简单的模拟逻辑
	if state.IterationCount >= state.MaxIterations {
		return DecisionReport, nil
	}

	if state.IterationCount == 0 {
		return DecisionDeepQuery, nil
	}

	// 检查是否有错误 Pod
	for _, pod := range state.K8sInfo.Pods {
		if strings.Contains(pod.Status, "Error") || strings.Contains(pod.Status, "Crash") {
			return DecisionDeepQuery, nil
		}
	}

	return DecisionReport, nil
}

// Analyze 模拟分析
func (m *MockLLM) Analyze(ctx context.Context, state *State) (string, error) {
	return "模拟分析结果：集群状态正常", nil
}

// GenerateReport 模拟报告生成
func (m *MockLLM) GenerateReport(ctx context.Context, state *State) (string, error) {
	return fmt.Sprintf("模拟报告：分析了 %d 个 Pod，执行了 %d 条命令",
		len(state.K8sInfo.Pods), len(state.AnalysisResult.ExecutedCommands)), nil
}

// SetTools 设置可用的工具列表
func (m *MockLLM) SetTools(tools []client.Tool) {
	m.tools = tools
	logger.Debug("[MockLLM] Tools set", logger.Int("count", len(tools)))
}

// CommandGenerator 命令生成器
// 根据当前状态生成要执行的命令
type CommandGenerator struct {
}

// NewCommandGenerator 创建命令生成器
func NewCommandGenerator() *CommandGenerator {
	return &CommandGenerator{}
}

// GenerateCommand 生成命令
func (g *CommandGenerator) GenerateCommand(state *State) (string, error) {
	logger.Debug("[CommandGenerator] Generating command", logger.Int("iteration", state.IterationCount))

	// 检查是否已有足够的 K8s 资源数据
	hasK8sResources := len(state.K8sInfo.Pods) > 0 ||
		len(state.K8sInfo.Services) > 0 ||
		len(state.K8sInfo.Deployments) > 0 ||
		len(state.K8sInfo.Events) > 0

	// 如果已有 K8s 资源数据，不再生成 kubectl 获取命令
	// 只在有特定问题需要深入分析时生成命令
	if hasK8sResources {
		// 检查是否有错误 Pod 需要查看日志
		for _, pod := range state.K8sInfo.Pods {
			if pod.Status == "Error" || pod.Status == "CrashLoopBackOff" {
				// 生成查看日志的命令
				cmd := fmt.Sprintf("kubectl logs %s -n %s", pod.Name, pod.Namespace)
				// 检查是否已执行过此命令
				if !g.isCommandExecuted(state, cmd) {
					logger.Debug("[CommandGenerator] Generated command for error pod", logger.String("command", cmd))
					return cmd, nil
				}
			}
			if pod.Restarts > 3 {
				// Pod 重启次数过多，查看日志
				cmd := fmt.Sprintf("kubectl logs %s -n %s --previous", pod.Name, pod.Namespace)
				// 检查是否已执行过此命令
				if !g.isCommandExecuted(state, cmd) {
					logger.Debug("[CommandGenerator] Generated command for high restart pod", logger.String("command", cmd))
					return cmd, nil
				}
			}
		}

		// 检查是否有 Service，生成网络测试命令
		if len(state.K8sInfo.Services) > 0 {
			svc := state.K8sInfo.Services[0]
			if svc.ClusterIP != "" {
				cmd := fmt.Sprintf("curl -I http://%s:%d", svc.ClusterIP, 80)
				// 检查是否已执行过此命令
				if !g.isCommandExecuted(state, cmd) {
					logger.Debug("[CommandGenerator] Generated network test command", logger.String("command", cmd))
					return cmd, nil
				}
			}
		}

		// 已有数据且无特定问题需要深入分析，返回空命令表示无需更多操作
		logger.Debug("[CommandGenerator] Already have K8s resources data, no need to fetch again")
		return "", nil
	}

	// 没有足够的 K8s 资源数据时，返回空命令
	// InfoNode 应该已经通过 K8s MCP 工具收集了数据
	logger.Debug("[CommandGenerator] No K8s resources data available, waiting for InfoNode to collect")
	return "", nil
}

// isCommandExecuted 检查命令是否已经执行过
func (g *CommandGenerator) isCommandExecuted(state *State, cmd string) bool {
	for _, executedCmd := range state.AnalysisResult.ExecutedCommands {
		if executedCmd.Command == cmd {
			return true
		}
	}
	return false
}

// ParseUserQuery 解析用户查询
func ParseUserQuery(query string) map[string]string {
	result := make(map[string]string)

	// 提取命名空间
	nsRegex := regexp.MustCompile(`(?:namespace|ns)[:\s]+(\S+)`)
	if matches := nsRegex.FindStringSubmatch(query); len(matches) > 1 {
		result["namespace"] = matches[1]
	}

	// 提取资源名称
	nameRegex := regexp.MustCompile(`(?:pod|service|deployment|svc|deploy)[:\s]+(\S+)`)
	if matches := nameRegex.FindStringSubmatch(query); len(matches) > 1 {
		result["resource_name"] = matches[1]
	}

	// 提取资源类型
	if strings.Contains(strings.ToLower(query), "pod") {
		result["resource_type"] = "pod"
	} else if strings.Contains(strings.ToLower(query), "service") || strings.Contains(strings.ToLower(query), "svc") {
		result["resource_type"] = "service"
	} else if strings.Contains(strings.ToLower(query), "deployment") || strings.Contains(strings.ToLower(query), "deploy") {
		result["resource_type"] = "deployment"
	}

	return result
}
