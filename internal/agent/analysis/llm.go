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
	MakeDecision(ctx context.Context, state *State) (Decision, error)

	// Analyze 分析 K8s 信息并返回分析结果
	Analyze(ctx context.Context, state *State) (string, error)

	// GenerateReport 生成报告摘要
	GenerateReport(ctx context.Context, state *State) (string, error)

	// SetTools 设置可用的工具列表
	// 用于动态注入工具信息到 LLM Prompt 中
	SetTools(tools []client.Tool)

	// AnalyzeError 针对特定错误上下文进行深入分析
	// 使用此方法可以获取更具体的修复建议
	// 当启用真实 LLM 时，使用 Eino Chain 进行分析
	AnalyzeError(ctx context.Context, errorContext ErrorContext) (AnalysisResult, error)
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

// CommandGenerator 命令生成器
// 根据当前状态生成要执行的命令
type CommandGenerator struct{}

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

// NewCommandGenerator 创建命令生成器
func NewCommandGenerator() *CommandGenerator {
	return &CommandGenerator{}
}

// GenerateCommand 生成命令
// 返回 K8s MCP 工具调用而不是 shell 命令
func (g *CommandGenerator) GenerateCommand(state *State) (*ToolCall, error) {
	logger.Debug("[CommandGenerator] Generating command", logger.Int("iteration", state.IterationCount))

	// 检查是否已有足够的 K8s 资源数据（采用动态获取模式，不再预收集 Services 和 Events）
	hasK8sResources := len(state.K8sInfo.Pods) > 0 ||
		len(state.K8sInfo.Deployments) > 0

	// 如果已有 K8s 资源数据，不再生成 kubectl 获取命令
	// 只在有特定问题需要深入分析时生成命令
	if hasK8sResources {
		// 检查是否有错误 Pod 需要查看日志
		for _, pod := range state.K8sInfo.Pods {
			if pod.Status == "Error" || pod.Status == "CrashLoopBackOff" {
				// 生成 K8s MCP 工具调用获取日志
				toolCall := &ToolCall{
					Tool: "get_pod_logs",
					Args: map[string]interface{}{
						"pod_name":  pod.Name,
						"namespace": pod.Namespace,
					},
					Type:    "k8s",
					Command: fmt.Sprintf("get_pod_logs pod_name=%s namespace=%s", pod.Name, pod.Namespace),
				}
				// 检查是否已执行过此命令
				if !g.isToolCallExecuted(state, toolCall) {
					logger.Debug("[CommandGenerator] Generated K8s tool call for error pod",
						logger.String("tool", toolCall.Tool),
						logger.String("pod", pod.Name))
					return toolCall, nil
				}
			}
			if pod.Restarts > 3 {
				// Pod 重启次数过多，查看上一次容器日志
				toolCall := &ToolCall{
					Tool: "get_pod_logs",
					Args: map[string]interface{}{
						"pod_name":  pod.Name,
						"namespace": pod.Namespace,
						"previous":  true,
					},
					Type:    "k8s",
					Command: fmt.Sprintf("get_pod_logs pod_name=%s namespace=%s previous=true", pod.Name, pod.Namespace),
				}
				// 检查是否已执行过此命令
				if !g.isToolCallExecuted(state, toolCall) {
					logger.Debug("[CommandGenerator] Generated K8s tool call for high restart pod",
						logger.String("tool", toolCall.Tool),
						logger.String("pod", pod.Name))
					return toolCall, nil
				}
			}
		}

		// 已有数据且无特定问题需要深入分析，返回空命令表示无需更多操作
		logger.Debug("[CommandGenerator] Already have K8s resources data, no need to fetch again")
		return nil, nil
	}

	// 没有足够的 K8s 资源数据时，返回空命令
	// InfoNode 应该已经通过 K8s MCP 工具收集了数据
	logger.Debug("[CommandGenerator] No K8s resources data available, waiting for InfoNode to collect")
	return nil, nil
}

// isToolCallExecuted 检查 K8s 工具调用是否已经执行过
// 通过比较工具名称和参数来判断
func (g *CommandGenerator) isToolCallExecuted(state *State, toolCall *ToolCall) bool {
	for _, executedCmd := range state.AnalysisResult.ExecutedCommands {
		// 如果原始命令匹配，也认为是已执行
		if executedCmd.Command == toolCall.Command {
			return true
		}
		// 如果是 K8s 工具调用，检查工具名称和关键参数
		if toolCall.Type == "k8s" {
			// 尝试解析已执行的命令是否为 K8s 工具调用格式
			// 这里简化处理：如果命令包含相同的工具名称和 Pod/Namespace，则认为已执行
			if strings.Contains(executedCmd.Command, toolCall.Tool) {
				// 检查参数是否匹配（支持 pod_name 和 pod 两种格式）
				if pod, ok := toolCall.Args["pod_name"].(string); ok {
					if strings.Contains(executedCmd.Command, "pod_name="+pod) || strings.Contains(executedCmd.Command, "pod="+pod) || strings.Contains(executedCmd.Command, pod+" ") {
						return true
					}
				}
			}
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
