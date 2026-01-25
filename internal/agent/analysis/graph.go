// Package analysis 提供 Eino Graph 编排实现
package analysis

import (
	"context"
	"fmt"
	"log"

	"github.com/cloudwego/eino/compose"
)

const (
	// Node 节点名称常量
	NodeStart    = "start"
	NodeInfo     = "info"
	NodeDecision = "decision"
	NodeAction   = "action"
	NodeReport   = "report"
	NodeEnd      = "end"
)

// Agent 主分析 Agent
// 基于 Eino Graph 实现 OODA 循环编排
type Agent struct {
	graph       compose.Runnable[*State, *AnalysisResult]
	k8sClient   K8sClient
	safetyAgent SafetyAgent
	llm         LLM
	logger      *log.Logger
}

// NewAgent 创建新的 Analysis Agent
func NewAgent(k8sClient K8sClient, safetyAgent SafetyAgent, logger *log.Logger) (*Agent, error) {
	if logger == nil {
		logger = log.Default()
	}

	// 创建基于规则的 LLM（默认）
	llm := NewRuleBasedLLM(logger)

	agent := &Agent{
		k8sClient:   k8sClient,
		safetyAgent: safetyAgent,
		llm:         llm,
		logger:      logger,
	}

	// 构建 Graph
	if err := agent.buildGraph(); err != nil {
		return nil, fmt.Errorf("failed to build graph: %w", err)
	}

	logger.Printf("[Agent] Analysis Agent initialized successfully")
	return agent, nil
}

// NewAgentWithLLM 使用自定义 LLM 创建 Agent
func NewAgentWithLLM(k8sClient K8sClient, safetyAgent SafetyAgent, llm LLM, logger *log.Logger) (*Agent, error) {
	if logger == nil {
		logger = log.Default()
	}

	agent := &Agent{
		k8sClient:   k8sClient,
		safetyAgent: safetyAgent,
		llm:         llm,
		logger:      logger,
	}

	// 构建 Graph
	if err := agent.buildGraph(); err != nil {
		return nil, fmt.Errorf("failed to build graph: %w", err)
	}

	logger.Printf("[Agent] Analysis Agent initialized successfully")
	return agent, nil
}

// buildGraph 构建 Eino Graph
func (a *Agent) buildGraph() error {
	a.logger.Printf("[Agent] Building graph...")

	// 创建 Graph
	g := compose.NewGraph[*State, *AnalysisResult]()

	// 创建节点
	infoNode := NewInfoNode(a.k8sClient, a.logger)
	decisionNode := NewDecisionNode(a.llm, a.logger)
	actionNode := NewActionNode(a.safetyAgent, a.logger)
	reportNode := NewReportNode(a.logger)
	commandGenerator := NewCommandGenerator(a.logger)

	// 添加 Info 节点
	if err := g.AddLambdaNode(NodeInfo, compose.InvokableLambda(
		func(ctx context.Context, state *State) (*State, error) {
			a.logger.Printf("[Graph] Executing InfoNode")
			return infoNode.Execute(ctx, state)
		},
	)); err != nil {
		return fmt.Errorf("failed to add info node: %w", err)
	}

	// 添加 Decision 节点
	if err := g.AddLambdaNode(NodeDecision, compose.InvokableLambda(
		func(ctx context.Context, state *State) (*State, error) {
			a.logger.Printf("[Graph] Executing DecisionNode")
			decision, err := decisionNode.Execute(ctx, state)
			if err != nil {
				state.LastError = err
				return state, err
			}

			// 将决策结果存储在状态中
			state.LastAction = string(decision)
			return state, nil
		},
	)); err != nil {
		return fmt.Errorf("failed to add decision node: %w", err)
	}

	// 添加 Action 节点
	if err := g.AddLambdaNode(NodeAction, compose.InvokableLambda(
		func(ctx context.Context, state *State) (*State, error) {
			a.logger.Printf("[Graph] Executing ActionNode")

			// 生成要执行的命令
			command, err := commandGenerator.GenerateCommand(state)
			if err != nil {
				a.logger.Printf("[Graph] Failed to generate command: %v", err)
				// 设置错误状态，让决策节点处理
				state.LastError = err
				return state, nil
			}

			// 执行命令
			return actionNode.Execute(ctx, state, command)
		},
	)); err != nil {
		return fmt.Errorf("failed to add action node: %w", err)
	}

	// 添加 Report 节点
	if err := g.AddLambdaNode(NodeReport, compose.InvokableLambda(
		func(ctx context.Context, state *State) (*AnalysisResult, error) {
			a.logger.Printf("[Graph] Executing ReportNode")
			// 先执行 ReportNode 生成报告
			_, err := reportNode.Execute(ctx, state)
			if err != nil {
				return nil, err
			}
			// 返回分析结果
			return state.AnalysisResult, nil
		},
	)); err != nil {
		return fmt.Errorf("failed to add report node: %w", err)
	}

	// 添加边（Edge）
	// Start -> Info
	if err := g.AddEdge(compose.START, NodeInfo); err != nil {
		return fmt.Errorf("failed to add edge START->Info: %w", err)
	}

	// Info -> Decision
	if err := g.AddEdge(NodeInfo, NodeDecision); err != nil {
		return fmt.Errorf("failed to add edge Info->Decision: %w", err)
	}

	// Decision -> Action (条件分支)
	if err := g.AddBranch(NodeDecision, compose.NewGraphBranch(
		func(ctx context.Context, state *State) (string, error) {
			decision := Decision(state.LastAction)
			a.logger.Printf("[Graph] Branch from Decision: %s", decision)

			switch decision {
			case DecisionContinue, DecisionDeepQuery:
				return NodeAction, nil
			case DecisionReport:
				return NodeReport, nil
			case DecisionError:
				return NodeReport, nil
			default:
				return NodeReport, nil
			}
		},
		map[string]bool{
			NodeAction: true,
			NodeReport: true,
		},
	)); err != nil {
		return fmt.Errorf("failed to add branch from Decision: %w", err)
	}

	// Action -> Decision (循环)
	if err := g.AddEdge(NodeAction, NodeDecision); err != nil {
		return fmt.Errorf("failed to add edge Action->Decision: %w", err)
	}

	// Report -> End
	if err := g.AddEdge(NodeReport, compose.END); err != nil {
		return fmt.Errorf("failed to add edge Report->END: %w", err)
	}

	// 编译 Graph
	compiledGraph, err := g.Compile(context.Background(), compose.WithMaxRunSteps(10))
	if err != nil {
		return fmt.Errorf("failed to compile graph: %w", err)
	}

	a.graph = compiledGraph
	a.logger.Printf("[Agent] Graph built and compiled successfully")
	return nil
}

// Run 执行分析任务
func (a *Agent) Run(ctx context.Context, userInput string) (*AnalysisResult, error) {
	a.logger.Printf("[Agent] Starting analysis for: %s", userInput)

	// 创建初始状态
	state := NewState(userInput)

	// 执行 Graph
	result, err := a.graph.Invoke(ctx, state)
	if err != nil {
		a.logger.Printf("[Agent] Graph execution failed: %v", err)
		return nil, fmt.Errorf("graph execution failed: %w", err)
	}

	// 返回分析结果
	return result, nil
}

// RunWithState 使用指定状态执行分析任务
func (a *Agent) RunWithState(ctx context.Context, state *State) (*AnalysisResult, error) {
	a.logger.Printf("[Agent] Running with state (iteration %d)", state.IterationCount)

	// 执行 Graph
	result, err := a.graph.Invoke(ctx, state)
	if err != nil {
		a.logger.Printf("[Agent] Graph execution failed: %v", err)
		return nil, fmt.Errorf("graph execution failed: %w", err)
	}

	// 返回分析结果
	return result, nil
}

// GetGraph 获取编译后的 Graph（用于调试和可视化）
func (a *Agent) GetGraph() compose.Runnable[*State, *AnalysisResult] {
	return a.graph
}

// GetK8sClient 获取 K8s 客户端
func (a *Agent) GetK8sClient() K8sClient {
	return a.k8sClient
}

// GetSafetyAgent 获取 Safety Agent
func (a *Agent) GetSafetyAgent() SafetyAgent {
	return a.safetyAgent
}

// GetLLM 获取 LLM
func (a *Agent) GetLLM() LLM {
	return a.llm
}

// SetLLM 设置 LLM
func (a *Agent) SetLLM(llm LLM) {
	a.llm = llm
	a.logger.Printf("[Agent] LLM updated")
}
