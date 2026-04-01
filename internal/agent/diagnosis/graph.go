package diagnosis

import (
	"context"
	"fmt"

	"github.com/AceDarkknight/k8s-analyzer-agent/internal/logger"
	"github.com/AceDarkknight/k8s-analyzer-agent/internal/state"
)

// ToolCall 是 state.ToolCall 的别名，用于本地使用
type ToolCall = state.ToolCall

// Graph 诊断流程编排
type Graph struct {
	infoNode            *InfoNode
	decisionNode        *DecisionNode
	actionNode          *ActionNode
	compressNode        *CompressNode
	reportNode          *ReportNode
	verifyEnabled       bool
	maxVerifyIterations int
}

// NewGraph 创建 Graph
func NewGraph(info *InfoNode, decision *DecisionNode, action *ActionNode, compress *CompressNode, report *ReportNode, verifyEnabled bool, maxVerifyIterations int) *Graph {
	return &Graph{
		infoNode:            info,
		decisionNode:        decision,
		actionNode:          action,
		compressNode:        compress,
		reportNode:          report,
		verifyEnabled:       verifyEnabled,
		maxVerifyIterations: maxVerifyIterations,
	}
}

// Run 执行诊断流程
func (g *Graph) Run(ctx context.Context, s *state.State) (*state.State, error) {
	logger.Info("Graph: starting diagnosis workflow")

	// defer-recover 防 panic
	defer func() {
		if r := recover(); r != nil {
			logger.Error("Graph: panic recovered", logger.Any("panic", r))
		}
	}()

	// 1. InfoNode.Execute(ctx, state) — 收集基础信息
	logger.Info("Graph: executing InfoNode")
	state, err := g.infoNode.Execute(ctx, s)
	if err != nil {
		logger.Error("Graph: InfoNode failed", logger.Err(err))
		// InfoNode 失败直接返回
		return state, fmt.Errorf("info node failed: %w", err)
	}
	logger.Info("Graph: InfoNode completed")

	// 2. 循环执行决策-动作-压缩流程
	for {
		// 检查是否应该继续
		if !state.ShouldContinue() {
			logger.Info("Graph: should not continue, breaking loop")
			break
		}

		// a. DecisionNode.Execute(ctx, state) → DecisionOutput
		logger.Info("Graph: executing DecisionNode")
		decisionOutput, err := g.decisionNode.Execute(ctx, state)
		if err != nil {
			logger.Error("Graph: DecisionNode failed", logger.Err(err))
			// 记录错误但尝试继续（降级处理）
			decisionOutput = &DecisionOutput{
				Decision:  "report",
				Thought:   "决策节点出错，进入报告模式",
				ToolCalls: []ToolCall{},
			}
		}
		logger.Info("Graph: DecisionNode completed", logger.String("decision", decisionOutput.Decision))

		// b. 如果 decision == "report" → 跳出循环
		if decisionOutput.Decision == "report" {
			logger.Info("Graph: decision is report, breaking loop")
			break
		}

		// c. ActionNode.Execute(ctx, state, decisionOutput) — 执行工具调用
		logger.Info("Graph: executing ActionNode")
		state, err = g.actionNode.Execute(ctx, state, decisionOutput)
		if err != nil {
			logger.Error("Graph: ActionNode failed", logger.Err(err))
			// 记录错误但继续
		}
		logger.Info("Graph: ActionNode completed")

		// d. CompressNode.Execute(ctx, state) — 条件压缩
		logger.Info("Graph: executing CompressNode")
		state, err = g.compressNode.Execute(ctx, state)
		if err != nil {
			logger.Error("Graph: CompressNode failed", logger.Err(err))
			// 记录错误但继续
		}
		logger.Info("Graph: CompressNode completed")

		// e. 检查 state.ShouldContinue()，如果 false → 跳出循环
		if !state.ShouldContinue() {
			logger.Info("Graph: state.ShouldContinue() is false, breaking loop")
			break
		}
	}

	// 3. ReportNode.Execute(ctx, state) — 生成初步报告
	logger.Info("Graph: executing ReportNode")
	state, err = g.reportNode.Execute(ctx, state)
	if err != nil {
		logger.Error("Graph: ReportNode failed", logger.Err(err))
		// 记录错误但尝试返回已有状态
	}
	logger.Info("Graph: ReportNode completed")

	// 4. 验证阶段：检查是否需要进入验证迭代
	if g.verifyEnabled && state.AnalysisResult != nil && len(state.AnalysisResult.Recommendations) > 0 && !state.VerifyPhase {
		logger.Info("Graph: entering verify phase")
		state.EnterVerifyPhase(g.maxVerifyIterations)

		// 验证迭代循环
		for {
			// a. DecisionNode（验证模式）
			logger.Info("Graph: executing DecisionNode (verify phase)")
			decisionOutput, err := g.decisionNode.Execute(ctx, state)
			if err != nil {
				logger.Error("Graph: DecisionNode (verify) failed", logger.Err(err))
				break
			}
			logger.Info("Graph: DecisionNode (verify) completed", logger.String("decision", decisionOutput.Decision))

			// b. 如果 decision == "report" → 跳出验证循环
			if decisionOutput.Decision == "report" {
				logger.Info("Graph: verify phase decision is report, breaking loop")
				break
			}

			// c. ActionNode（验证模式）
			logger.Info("Graph: executing ActionNode (verify phase)")
			state, err = g.actionNode.Execute(ctx, state, decisionOutput)
			if err != nil {
				logger.Error("Graph: ActionNode (verify) failed", logger.Err(err))
			}
			logger.Info("Graph: ActionNode (verify) completed")

			// d. CompressNode（可选）
			logger.Info("Graph: executing CompressNode (verify phase)")
			state, err = g.compressNode.Execute(ctx, state)
			if err != nil {
				logger.Error("Graph: CompressNode (verify) failed", logger.Err(err))
			}
			logger.Info("Graph: CompressNode (verify) completed")
		}

		// 5. 生成终版报告
		logger.Info("Graph: executing ReportNode for final report")
		state, err = g.reportNode.Execute(ctx, state)
		if err != nil {
			logger.Error("Graph: final ReportNode failed", logger.Err(err))
		}
		logger.Info("Graph: final ReportNode completed")
	}

	logger.Info("Graph: diagnosis workflow completed")
	return state, nil
}
