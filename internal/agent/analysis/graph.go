// Package analysis 提供 Eino Graph 编排实现
package analysis

import (
	"context"
	"fmt"

	"github.com/AceDarkknight/k8s-analyzer-agent/internal/client"
	"github.com/AceDarkknight/k8s-analyzer-agent/internal/client/k8s"
	"github.com/AceDarkknight/k8s-analyzer-agent/internal/config"
	"github.com/AceDarkknight/k8s-analyzer-agent/internal/logger"
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
	reactLLM    *ReActLLM     // ReAct LLM 用于错误分析
	tools       []client.Tool // 动态加载的工具列表
	store       FindingStore  // Finding 存储（去重）
}

// NewAgent 创建新的 Analysis Agent
func NewAgent(k8sClient K8sClient, safetyAgent SafetyAgent, llmConfig *config.LLMConfig) (*Agent, error) {
	// 初始化 FindingStore（Redis 优先，失败则回退到内存存储）
	store := initFindingStore(llmConfig)

	// 必须创建 ReActLLM，不再降级
	if llmConfig.Provider == "rule-based" || llmConfig.APIKey == "" {
		logger.Fatal("[Agent] ReActLLM requires valid LLM configuration (provider != 'rule-based' and APIKey provided)")
		return nil, fmt.Errorf("invalid LLM configuration: rule-based provider or missing APIKey not supported")
	}

	agent := &Agent{
		k8sClient:   k8sClient,
		safetyAgent: safetyAgent,
		store:       store,
	}

	// 加载工具列表（严格启动检查）
	// 注意：这里先不设置 LLM，LoadTools 中需要用到 llm，先创建一个 MockLLM 临时使用
	agent.llm = NewMockLLM()

	ctx := context.Background()
	if err := agent.LoadTools(ctx); err != nil {
		logger.Fatal("Failed to load tools during agent initialization", logger.Err(err))
		return nil, fmt.Errorf("failed to load tools: %w", err)
	}

	logger.Info("[Agent] Creating ReAct LLM for dynamic tool calling")
	reactLLM, err := NewReActLLM(ctx, k8sClient, safetyAgent, llmConfig)
	if err != nil {
		logger.Fatal("[Agent] Failed to create ReAct LLM, exiting",
			logger.Err(err))
		return nil, fmt.Errorf("failed to create ReAct LLM: %w", err)
	}
	agent.reactLLM = reactLLM
	agent.llm = reactLLM // 使用 ReActLLM 替换临时 MockLLM
	logger.Info("[Agent] ReAct LLM created successfully")

	// 构建 Graph
	if err := agent.buildGraph(); err != nil {
		return nil, fmt.Errorf("failed to build graph: %w", err)
	}

	logger.Info("Analysis Agent initialized successfully")
	return agent, nil
}

// NewAgentWithLLM 使用自定义 LLM 创建 Agent
func NewAgentWithLLM(k8sClient K8sClient, safetyAgent SafetyAgent, llm LLM, store FindingStore) (*Agent, error) {
	agent := &Agent{
		k8sClient:   k8sClient,
		safetyAgent: safetyAgent,
		llm:         llm,
		store:       store,
	}

	// 加载工具列表（严格启动检查）
	ctx := context.Background()
	if err := agent.LoadTools(ctx); err != nil {
		logger.Fatal("Failed to load tools during agent initialization", logger.Err(err))
		return nil, fmt.Errorf("failed to load tools: %w", err)
	}

	// 构建 Graph
	if err := agent.buildGraph(); err != nil {
		return nil, fmt.Errorf("failed to build graph: %w", err)
	}

	logger.Info("Analysis Agent initialized successfully")
	return agent, nil
}

// NewAgentWithReActLLM 使用 ReAct LLM 创建 Agent
func NewAgentWithReActLLM(k8sClient K8sClient, safetyAgent SafetyAgent, reactLLM *ReActLLM, store FindingStore) (*Agent, error) {
	// ReActLLM 已实现 LLM 接口，无需额外的基础 LLM
	agent := &Agent{
		k8sClient:   k8sClient,
		safetyAgent: safetyAgent,
		llm:         reactLLM, // 直接使用 ReActLLM 作为 LLM（实现 LLM 接口）
		reactLLM:    reactLLM,
		store:       store,
	}

	// 加载工具列表（严格启动检查）
	ctx := context.Background()
	if err := agent.LoadTools(ctx); err != nil {
		logger.Fatal("Failed to load tools during agent initialization", logger.Err(err))
		return nil, fmt.Errorf("failed to load tools: %w", err)
	}

	// 构建 Graph
	if err := agent.buildGraph(); err != nil {
		return nil, fmt.Errorf("failed to build graph: %w", err)
	}

	logger.Info("Analysis Agent initialized successfully with ReAct LLM")
	return agent, nil
}

// initFindingStore 初始化 FindingStore
// Redis 优先，失败则回退到内存存储
func initFindingStore(cfg *config.LLMConfig) FindingStore {
	// 检查是否配置了 Redis
	if cfg != nil && cfg.Redis != nil && cfg.Redis.IsValid() {
		logger.Info("Initializing Redis store for findings",
			logger.String("host", cfg.Redis.Host),
			logger.Int("port", cfg.Redis.Port))

		store, err := NewRedisStore(cfg.Redis)
		if err != nil {
			logger.Warn("Failed to connect to Redis, falling back to in-memory store",
				logger.Err(err))
			return NewMemoryStore()
		}
		return store
	}

	logger.Warn("Redis not configured, using in-memory store for findings")
	return NewMemoryStore()
}

// LoadTools 加载 K8s MCP Server 的工具列表
// 该方法在 Agent 初始化时调用，确保工具列表在启动时加载成功
func (a *Agent) LoadTools(ctx context.Context) error {
	logger.Info("Loading tools from K8s MCP Server...")

	// 调用 K8sClient.ListTools 获取工具列表
	mcpTools, err := a.k8sClient.ListTools(ctx)
	if err != nil {
		// 严格启动检查：工具加载失败时Fatal
		logger.Error("Failed to list tools from K8s MCP Server", logger.Err(err))
		return fmt.Errorf("failed to list tools from K8s MCP Server: %w", err)
	}

	// 转换 MCP Tools 为 client.Tool 格式
	tools := make([]client.Tool, 0, len(mcpTools))
	for _, mcpTool := range mcpTools {
		// 序列化 InputSchema
		inputSchema, err := mcpTool.InputSchema.MarshalJSON()
		if err != nil {
			logger.Warn("Failed to marshal input schema for tool",
				logger.String("tool", mcpTool.Name),
				logger.Err(err))
			continue
		}

		tools = append(tools, client.Tool{
			Name:        mcpTool.Name,
			Description: mcpTool.Description,
			InputSchema: inputSchema,
		})
	}

	// 增强工具描述，添加使用指导
	tools = k8s.EnhanceToolDescriptions(tools)

	// 存储工具列表
	a.tools = tools

	// 将工具列表注入到 LLM
	a.llm.SetTools(tools)

	logger.Info("Tools loaded successfully",
		logger.Int("tool_count", len(tools)))

	return nil
}

// buildGraph 构建 Eino Graph
func (a *Agent) buildGraph() error {
	logger.Info("Building graph...")

	// 创建 Graph
	g := compose.NewGraph[*State, *AnalysisResult]()

	// 确定使用哪个 LLM 进行错误分析
	// 优先使用 ReActLLM，如果可用的话
	errorAnalysisLLM := a.llm
	if a.reactLLM != nil {
		logger.Info("[Agent] Using ReAct LLM for error analysis")
		errorAnalysisLLM = a.reactLLM
	}

	// 创建节点
	infoNode := NewInfoNode(a.k8sClient)
	decisionNode := NewDecisionNode(a.llm)
	actionNode := NewActionNode(a.safetyAgent, a.k8sClient)
	reportNode := NewReportNode(a.store, errorAnalysisLLM)
	commandGenerator := NewCommandGenerator()

	// 添加 Info 节点
	if err := g.AddLambdaNode(NodeInfo, compose.InvokableLambda(
		func(ctx context.Context, state *State) (*State, error) {
			logger.Debug("Executing InfoNode", logger.Int("iteration", state.IterationCount))
			return infoNode.Execute(ctx, state)
		},
	)); err != nil {
		return fmt.Errorf("failed to add info node: %w", err)
	}

	// 添加 Decision 节点
	if err := g.AddLambdaNode(NodeDecision, compose.InvokableLambda(
		func(ctx context.Context, state *State) (*State, error) {
			logger.Debug("Executing DecisionNode", logger.Int("iteration", state.IterationCount))
			decision, err := decisionNode.Execute(ctx, state)
			if err != nil {
				state.LastError = err
				return state, err
			}

			// 将决策结果存储在状态中
			state.LastAction = string(decision)
			logger.Debug("Decision made", logger.String("action", string(decision)))
			return state, nil
		},
	)); err != nil {
		return fmt.Errorf("failed to add decision node: %w", err)
	}

	// 添加 Action 节点
	if err := g.AddLambdaNode(NodeAction, compose.InvokableLambda(
		func(ctx context.Context, state *State) (*State, error) {
			logger.Debug("Executing ActionNode", logger.Int("iteration", state.IterationCount))

			// 增加迭代计数
			state.IterationCount++

			// 生成要执行的命令
			toolCall, err := commandGenerator.GenerateCommand(state)
			if err != nil {
				logger.Error("Failed to generate command", logger.Err(err))
				// 设置错误状态，让决策节点处理
				state.LastError = err
				return state, nil
			}

			// 如果没有命令可执行，设置标志让决策节点知道
			if toolCall == nil {
				logger.Debug("No new command to execute, proceeding to decision")
				state.LastAction = "no_command"
				return state, nil
			}

			// 执行命令
			return actionNode.Execute(ctx, state, toolCall)
		},
	)); err != nil {
		return fmt.Errorf("failed to add action node: %w", err)
	}

	// 添加 Report 节点
	if err := g.AddLambdaNode(NodeReport, compose.InvokableLambda(
		func(ctx context.Context, state *State) (*AnalysisResult, error) {
			logger.Info("Executing ReportNode", logger.Int("iteration", state.IterationCount))
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
			logger.Debug("Branch from Decision", logger.String("decision", string(decision)))

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
	compiledGraph, err := g.Compile(context.Background(), compose.WithMaxRunSteps(100))
	if err != nil {
		return fmt.Errorf("failed to compile graph: %w", err)
	}

	a.graph = compiledGraph
	logger.Info("Graph built and compiled successfully")
	return nil
}

// Run 执行分析任务
func (a *Agent) Run(ctx context.Context, userInput string) (*AnalysisResult, error) {
	logger.Info("Starting analysis", logger.String("userInput", userInput))

	// 创建初始状态
	state := NewState(userInput)

	// 执行 Graph
	result, err := a.graph.Invoke(ctx, state)
	if err != nil {
		logger.Error("Graph execution failed", logger.Err(err))
		return nil, fmt.Errorf("graph execution failed: %w", err)
	}

	// 返回分析结果
	logger.Info("Analysis completed successfully", logger.Int("iterations", state.IterationCount))
	return result, nil
}

// RunWithState 使用指定状态执行分析任务
func (a *Agent) RunWithState(ctx context.Context, state *State) (*AnalysisResult, error) {
	logger.Debug("Running with state", logger.Int("iteration", state.IterationCount))

	// 执行 Graph
	result, err := a.graph.Invoke(ctx, state)
	if err != nil {
		logger.Error("Graph execution failed", logger.Err(err))
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
	logger.Info("LLM updated")
}

// GetReActLLM 获取 ReAct LLM
func (a *Agent) GetReActLLM() *ReActLLM {
	return a.reactLLM
}
