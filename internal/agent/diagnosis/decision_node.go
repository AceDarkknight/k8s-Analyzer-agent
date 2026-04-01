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
	ToolCalls      []state.ToolCall
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

	// 6. 添加 ReasoningStep 到 state
	step := state.ReasoningStep{
		Iteration:      s.GetIterationCount(),
		Thought:        result.Thought,
		Decision:       result.Decision,
		DeepQueryTopic: result.DeepQueryTopic,
		ToolCalls:      result.ToolCalls,
	}
	s.AddReasoningStep(step)

	logger.Info("DecisionNode: decision made",
		logger.String("decision", result.Decision),
		logger.Int("tool_calls", len(result.ToolCalls)))

	return &DecisionOutput{
		Decision:       result.Decision,
		Thought:        result.Thought,
		ToolCalls:      result.ToolCalls,
		DeepQueryTopic: result.DeepQueryTopic,
	}, nil
}

// fallbackDecision 降级决策处理
func (n *DecisionNode) fallbackDecision(s *state.State) *DecisionOutput {
	// 如果 K8sInfo 有异常 Pod 且 IterationCount < 3，返回 continue + 工具调用 describe_pod
	if s.K8sInfo != nil && s.GetIterationCount() < 3 {
		abnormalPods := s.K8sInfo.GetAbnormalPods()
		if len(abnormalPods) > 0 {
			// 选择第一个异常 Pod 进行 describe
			pod := abnormalPods[0]
			logger.Info("DecisionNode: fallback to describe abnormal pod",
				logger.String("pod", fmt.Sprintf("%s/%s", pod.Namespace, pod.Name)))

			return &DecisionOutput{
				Decision: "continue",
				Thought:  "发现异常 Pod，需要查看详情",
				ToolCalls: []state.ToolCall{
					{
						Name: "describe_pod",
						Args: map[string]interface{}{
							"namespace": pod.Namespace,
							"name":      pod.Name,
						},
					},
				},
			}
		}
	}

	// 否则返回 report
	logger.Info("DecisionNode: fallback to report")
	return &DecisionOutput{
		Decision:  "report",
		Thought:   "基于已有信息生成报告",
		ToolCalls: []state.ToolCall{},
	}
}
