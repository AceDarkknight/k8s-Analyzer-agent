package diagnosis

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/schema"

	"github.com/AceDarkknight/k8s-analyzer-agent/internal/llm"
	"github.com/AceDarkknight/k8s-analyzer-agent/internal/logger"
	"github.com/AceDarkknight/k8s-analyzer-agent/internal/state"
)

// DecisionNode 决策节点
type DecisionNode struct {
	router *llm.LLMRouter
}

// DecisionOutput 决策输出
type DecisionOutput struct {
	Decision       string
	Thought        string
	ToolCalls      []state.ToolCall // 兼容旧模式
	Plan           []state.PlanStep // 新模式：完整计划
	ExecuteSteps   []int            // 本轮要执行的步骤编号
	DeepQueryTopic string
}

// NewDecisionNode 创建新的决策节点
func NewDecisionNode(router *llm.LLMRouter) *DecisionNode {
	return &DecisionNode{
		router: router,
	}
}

// Execute 执行决策
func (n *DecisionNode) Execute(ctx context.Context, s *state.State) (*DecisionOutput, error) {
	logger.Info("DecisionNode: starting decision making",
		logger.Int("iteration", s.GetIterationCount()),
		logger.Int("max_iterations", s.GetMaxIterations()))

	// 验证阶段：自增计数，超限则强制 report
	if s.VerifyPhase {
		exceeded := s.IncrementVerifyIteration()
		if exceeded {
			logger.Info("DecisionNode: verify iterations exhausted, forcing report",
				logger.Int("verify_iter", s.VerifyIterationCount),
				logger.Int("max_verify_iter", s.MaxVerifyIterations))
			return &DecisionOutput{
				Decision:  "report",
				Thought:   fmt.Sprintf("验证迭代已达上限 %d 轮，强制生成终版报告", s.MaxVerifyIterations),
				ToolCalls: []state.ToolCall{},
			}, nil
		}
		// 验证阶段使用专用 Prompt（BuildDecisionPrompt 内部自动分发）
	}

	// 主诊断阶段：检查是否达到 MaxIterations
	if !s.VerifyPhase && s.GetIterationCount() >= s.GetMaxIterations() {
		logger.Info("DecisionNode: max iterations reached, forcing report")
		return &DecisionOutput{
			Decision:  "report",
			Thought:   "已达到最大迭代次数，需要生成报告",
			ToolCalls: []state.ToolCall{},
		}, nil
	}

	// 主诊断阶段：增加迭代计数
	if !s.VerifyPhase {
		s.IncrementIteration()
	}

	// 3. 构建 prompt
	prompt := llm.BuildDecisionPrompt(s)
	if prompt == "" {
		logger.Warn("DecisionNode: empty prompt generated")
		return n.fallbackDecision(s), nil
	}

	// 4. 调用 LLM
	messages := []*schema.Message{
		schema.UserMessage(prompt),
	}

	response, err := n.router.GenerateWithLight(ctx, messages)
	if err != nil {
		logger.Error("DecisionNode: LLM generation failed", logger.Err(err))
		return n.fallbackDecision(s), nil
	}

	if response == nil || response.Content == "" {
		logger.Warn("DecisionNode: empty LLM response")
		return n.fallbackDecision(s), nil
	}

	// 5. 解析响应
	result, err := llm.ParseDecisionResponse(response.Content)
	if err != nil {
		logger.Error("DecisionNode: failed to parse decision response", logger.Err(err))
		return n.fallbackDecision(s), nil
	}

	// 6. 构建 ToolCalls（从 execute_steps 中提取）
	var toolCalls []state.ToolCall
	if result.Decision == "execute_plan" && len(result.ExecuteSteps) > 0 {
		for _, stepNum := range result.ExecuteSteps {
			for _, planStep := range result.Plan {
				if planStep.Step == stepNum {
					toolCalls = append(toolCalls, planStep.ToolCalls...)
					break
				}
			}
		}
	} else {
		toolCalls = result.ToolCalls
	}

	// 7. 添加 ReasoningStep 到 state
	step := state.ReasoningStep{
		Iteration:      s.GetIterationCount(),
		Thought:        result.Thought,
		Decision:       result.Decision,
		DeepQueryTopic: result.DeepQueryTopic,
		ToolCalls:      toolCalls,
	}
	s.AddReasoningStep(step)

	logger.Info("DecisionNode: decision made",
		logger.String("decision", result.Decision),
		logger.Int("tool_calls", len(toolCalls)),
		logger.Int("plan_steps", len(result.Plan)))

	// 转换 Plan
	var plan []state.PlanStep
	for _, ps := range result.Plan {
		plan = append(plan, state.PlanStep{
			Step:        ps.Step,
			Description: ps.Description,
			ToolCalls:   ps.ToolCalls,
		})
	}

	return &DecisionOutput{
		Decision:       result.Decision,
		Thought:        result.Thought,
		ToolCalls:      toolCalls,
		Plan:           plan,
		ExecuteSteps:   result.ExecuteSteps,
		DeepQueryTopic: result.DeepQueryTopic,
	}, nil
}

// fallbackDecision 降级决策处理（仅在 LLM 失败时使用）
func (n *DecisionNode) fallbackDecision(s *state.State) *DecisionOutput {
	// 简单保底：如果还有迭代次数且有异常 Pod，让 LLM 重试
	if s.K8sInfo != nil && s.GetIterationCount() < s.GetMaxIterations()-1 {
		abnormalPods := s.K8sInfo.GetAbnormalPods()
		if len(abnormalPods) > 0 {
			logger.Info("DecisionNode: fallback - abnormal pods exist, will retry LLM")
			// 返回空的 execute_plan，让下一轮重新调用 LLM
			return &DecisionOutput{
				Decision:  "execute_plan",
				Thought:   "LLM 调用失败，等待下一轮重试",
				ToolCalls: []state.ToolCall{},
			}
		}
	}

	// 否则直接生成报告
	logger.Info("DecisionNode: fallback to report")
	return &DecisionOutput{
		Decision:  "report",
		Thought:   "LLM 调用失败，基于已有信息生成报告",
		ToolCalls: []state.ToolCall{},
	}
}
