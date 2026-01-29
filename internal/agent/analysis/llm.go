// Package analysis 提供 LLM 接口和实现
package analysis

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/AceDarkknight/k8s-analyzer-agent/internal/logger"
)

// LLM 定义大语言模型接口
// 用于决策和推理
type LLM interface {
	// MakeDecision 根据当前状态做出决策
	MakeDecision(ctx context.Context, state *State) (Decision, error)

	// Analyze 分析 K8s 信息并返回分析结果
	Analyze(ctx context.Context, state *State) (string, error)

	// GenerateReport 生成报告摘要
	GenerateReport(ctx context.Context, state *State) (string, error)
}

// RuleBasedLLM 基于规则的 LLM 实现
// 使用预定义规则进行决策，不需要真实的 LLM API
type RuleBasedLLM struct {
	rules []DecisionRule
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
func NewRuleBasedLLM() *RuleBasedLLM {
	llm := &RuleBasedLLM{
		rules: make([]DecisionRule, 0),
	}

	// 初始化默认规则
	llm.initDefaultRules()

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
			Priority: 2,
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
			Priority: 3,
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
			Priority: 4,
		},
		{
			Name: "error_occurred",
			Condition: func(s *State) bool {
				return s.LastError != nil
			},
			Action:   DecisionReport,
			Priority: 5,
		},
		{
			Name: "has_enough_info",
			Condition: func(s *State) bool {
				// 如果已经收集了足够的信息（至少一次迭代，且有数据）
				return s.IterationCount > 0 &&
					(len(s.K8sInfo.Pods) > 0 || len(s.K8sInfo.Services) > 0)
			},
			Action:   DecisionReport,
			Priority: 6,
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

	// 检查是否已经执行了命令，避免重复
	if len(state.AnalysisResult.ExecutedCommands) > 0 {
		logger.Debug("[CommandGenerator] Already executed commands, skipping", logger.Int("count", len(state.AnalysisResult.ExecutedCommands)))
		return "", fmt.Errorf("already executed commands, skipping")
	}

	// 检查是否有错误 Pod
	for _, pod := range state.K8sInfo.Pods {
		if pod.Status == "Error" || pod.Status == "CrashLoopBackOff" {
			// 生成查看日志的命令
			cmd := fmt.Sprintf("kubectl logs %s -n %s", pod.Name, pod.Namespace)
			logger.Debug("[CommandGenerator] Generated command", logger.String("command", cmd))
			return cmd, nil
		}
		if pod.Restarts > 3 {
			// Pod 重启次数过多，查看日志
			cmd := fmt.Sprintf("kubectl logs %s -n %s --previous", pod.Name, pod.Namespace)
			logger.Debug("[CommandGenerator] Generated command", logger.String("command", cmd))
			return cmd, nil
		}
	}

	// 检查是否有 Service，生成网络测试命令
	if len(state.K8sInfo.Services) > 0 {
		svc := state.K8sInfo.Services[0]
		if svc.ClusterIP != "" {
			cmd := fmt.Sprintf("curl -I http://%s:%d", svc.ClusterIP, 80)
			logger.Debug("[CommandGenerator] Generated command", logger.String("command", cmd))
			return cmd, nil
		}
	}

	// 默认返回描述 Pod 的命令
	if len(state.K8sInfo.Pods) > 0 {
		pod := state.K8sInfo.Pods[0]
		cmd := fmt.Sprintf("kubectl describe pod %s -n %s", pod.Name, pod.Namespace)
		logger.Debug("[CommandGenerator] Generated command", logger.String("command", cmd))
		return cmd, nil
	}

	// 如果没有足够信息，返回一个默认命令
	cmd := "kubectl get pods -A"
	logger.Debug("[CommandGenerator] Generated default command", logger.String("command", cmd))
	return cmd, nil
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
