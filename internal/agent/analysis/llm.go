// Package analysis provides LLM interfaces and implementations
package analysis

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/AceDarkknight/k8s-analyzer-agent/internal/client"
	"github.com/AceDarkknight/k8s-analyzer-agent/internal/logger"
)

// SystemPrompt 系统提示词常量
const SystemPrompt = `## 系统角色

你是一个 Kubernetes 集群诊断专家 Agent。你的职责是诊断和分析 Kubernetes 集群中的问题。

## 核心职责

1. **全命名空間诊断**: 你必须诊断整个集群的所有命名空间，不要局限于单个命名空間。
2. **全面分析**: 检查所有命名空间中的 Pod、Service、Deployment、StatefulSet、Event 等资源。
3. **问题发现**: 主动发现集群中的异常状态，如 CrashLoopBackOff、Pending、Error、错误事件等。
4. **根因分析**: 分析问题的根本原因并提供解决建议。

## ⚠️ 重要约束 - 必须严格遵守

### 数据使用约束（最高优先级）
- **检查已有数据**: 在生成任何查询命令之前，必须先检查 Context 部分是否已包含所需数据。
- **避免重复查询**: 如果 Context 中已有 Pod/Deployment/StatefulSet 列表，不要再次生生成命的命令获取它们。
- **直接分析**: 当已有足够数据时，直接进入分析阶段，生成 "report" 决策。
- **数据充足判断**: 如果已收集到任何 Pod 或 Event 数据，应优先分析这些数据而非继续收集。

### 工具使用约束
- **禁止生成 shell 命令**: 绝对不要生成类似 'kubectl get pods' 的 shell 命令。
- **必须使用 K8s MCP 工具**: 你必须使用提供的 K8s MCP 工具来获取集群信息，不要使用 kubectl 命令。
- **🚫 禁止使用 Shell MCP 执行 kubectl 命令**: 
  - **绝对禁止**使用 Shell MCP 的 execute_command 工具来运行 kubectl 命令。
  - **所有 Kubernetes 操作必须使用 K8s MCP 工具**。
  - 违反此规则将导致任务被系统拒绝。
- **⚡ 严格遵守工具使用说明**: 下方 "可用工具列表" 中每个工具都有特定的使用约束和警告说明，**必须严格遵守**。特别关注带有 "⚠️" 标记的重要提示。

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
- 'list_deployments'（参数: namespace=<命名空间名称>）
- 'list_statefulsets'（参数: namespace=<命名空间名称>）
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
	// 返回 DecisionResult 包含决策、推理和工具调用
	MakeDecision(ctx context.Context, state *State) (*DecisionResult, error)

	// Analyze 分析 K8s 信息并返回分析结果
	Analyze(ctx context.Context, state *State) (string, error)

	// GenerateReport 生成报告摘要
	GenerateReport(ctx context.Context, state *State) (string, error)

	// SynthesizeReport 生成高质量的综合报告
	// 根据用户输入、发现的问题、执行的命令和 K8s 资源摘要生成结构化报告
	SynthesizeReport(ctx context.Context, userInput string, findings []Finding, commands []CommandExecution, k8sSummary string) (string, error)

	// SetTools 设置可用的工具列表
	// 用于动态注入工具信息到 LLM Prompt 中
	SetTools(tools []client.Tool)

	// AnalyzeError 针对特定错误上下文进行深入分析
	// 使用此方法可以获取更具体的修复建议
	// 当启用真实 LLM 时，使用 Eino Chain 进行分析
	AnalyzeError(ctx context.Context, errorContext ErrorContext) (AnalysisResult, error)
}

// DecisionResult LLM 决策结果结构
// 包含决策类型、推理过程和工具调用列表
type DecisionResult struct {
	Decision  Decision   `json:"decision"`   // 决策结果 (continue, report)
	Reasoning string     `json:"reasoning"`  // LLM 的思考过程
	ToolCalls []ToolCall `json:"tool_calls"` // 具体的工具调用列表
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
func (m *MockLLM) MakeDecision(ctx context.Context, state *State) (*DecisionResult, error) {
	logger.Debug("[MockLLM] Making decision", logger.Int("iteration", state.IterationCount))

	// 简单的模拟逻辑
	if state.IterationCount >= state.MaxIterations {
		return &DecisionResult{
			Decision:  DecisionReport,
			Reasoning: "已达到最大迭代次数",
			ToolCalls: nil,
		}, nil
	}

	if state.IterationCount == 0 {
		return &DecisionResult{
			Decision:  DecisionDeepQuery,
			Reasoning: "第一次迭代，需要收集集群信息",
			ToolCalls: nil,
		}, nil
	}

	// 检查是否有错误 Pod
	pods := state.K8sInfo.Resources["Pods"]
	for _, pod := range pods {
		podInfo, ok := pod.(PodInfo)
		if !ok {
			continue
		}
		if strings.Contains(podInfo.Status, "Error") || strings.Contains(podInfo.Status, "Crash") {
			return &DecisionResult{
				Decision:  DecisionDeepQuery,
				Reasoning: fmt.Sprintf("发现异常 Pod: %s, 状态: %s, 需要深入分析", podInfo.Name, podInfo.Status),
				ToolCalls: []ToolCall{
					{
						Tool: "get_pod_logs",
						Args: map[string]interface{}{
							"pod_name":  podInfo.Name,
							"namespace": podInfo.Namespace,
						},
						Type:    "k8s",
						Command: fmt.Sprintf("get_pod_logs pod_name=%s namespace=%s", podInfo.Name, podInfo.Namespace),
					},
				},
			}, nil
		}
	}

	return &DecisionResult{
		Decision:  DecisionReport,
		Reasoning: "已收集足够信息，可以生成报告",
		ToolCalls: nil,
	}, nil
}

// Analyze 模拟分析
func (m *MockLLM) Analyze(ctx context.Context, state *State) (string, error) {
	return "模拟分析结果：集群状态正常", nil
}

// GenerateReport 模拟报告生成
func (m *MockLLM) GenerateReport(ctx context.Context, state *State) (string, error) {
	return fmt.Sprintf("模拟报告：分析了 %d 个 Pod，执行了 %d 条命令",
		len(state.K8sInfo.Resources["Pods"]), len(state.AnalysisResult.ExecutedCommands)), nil
}

// SetTools 设置可用的工具列表
func (m *MockLLM) SetTools(tools []client.Tool) {
	m.tools = tools
	logger.Debug("[MockLLM] Tools set", logger.Int("count", len(tools)))
}

// AnalyzeError 模拟错误分析
func (m *MockLLM) AnalyzeError(ctx context.Context, errorContext ErrorContext) (AnalysisResult, error) {
	logger.Debug("[MockLLM] Analyzing error",
		logger.String("pod", errorContext.PodName),
		logger.String("status", errorContext.Status))

	var result AnalysisResult

	// 模拟分析结果
	result.Findings = append(result.Findings, Finding{
		Severity:  "High",
		Resource:  errorContext.PodName,
		Message:   fmt.Sprintf("模拟分析: Pod 状态异常 - %s", errorContext.Status),
		Timestamp: time.Now(),
	})

	result.Recommendations = append(result.Recommendations, Recommendation{
		Action:   "模拟修复建议",
		Reason:   "这是一个模拟的分析结果",
		Priority: "Medium",
		Command:  "",
	})

	return result, nil
}

// SynthesizeReport 模拟生成高质量综合报告
func (m *MockLLM) SynthesizeReport(ctx context.Context, userInput string, findings []Finding, commands []CommandExecution, k8sSummary string) (string, error) {
	logger.Debug("[MockLLM] Synthesizing report",
		logger.String("userInput", userInput),
		logger.Int("findings", len(findings)),
		logger.Int("commands", len(commands)))

	var sb strings.Builder

	sb.WriteString("# Kubernetes 集群诊断报告\n\n")

	// 执行摘要
	sb.WriteString("## 1. Summary (执行摘要)\n")
	sb.WriteString("模拟诊断完成。根据收集的信息，发现了一些潜在问题并提供了相应的修复建议。\n\n")

	// 详细发现
	sb.WriteString("## 2. Findings (详细发现)\n")
	if len(findings) > 0 {
		for _, f := range findings {
			sb.WriteString(fmt.Sprintf("- **[%s]** %s: %s\n", f.Severity, f.Resource, f.Message))
		}
	} else {
		sb.WriteString("- 未发现明显问题\n")
	}
	sb.WriteString("\n")

	// 修复建议
	sb.WriteString("## 3. Recommendations (修复建议)\n")
	sb.WriteString("- 建议定期检查集群状态\n")
	sb.WriteString("- 关注异常 Pod 的日志信息\n")
	sb.WriteString("- 根据实际情况调整资源配置\n")

	return sb.String(), nil
}

// ToolCall K8s MCP 工具调用结构
// 用于表示要执行的 K8s MCP 工具及其参数
type ToolCall struct {
	Tool    string                 `json:"tool"`    // 工具名称，如 "get_pod_logs"
	Args    map[string]interface{} `json:"args"`    // 工具参数
	Type    string                 `json:"type"`    // 调用类型："k8s" 或 "shell"
	Command string                 `json:"command"` // 原始命令字符串（用于兼容和日志）
}

// ErrorContext 错误上下文结构
// 用于传递错误 Pod 的详细信息给 LLM 进行深入分析
type ErrorContext struct {
	// PodName Pod 名称
	PodName string

	// Namespace 命名空间
	Namespace string

	// Status Pod 状态
	Status string

	// Restarts 重启次数
	Restarts int32

	// Logs Pod 日志内容
	Logs string

	// Events 相关事件列表
	Events []string
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
