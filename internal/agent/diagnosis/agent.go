package diagnosis

import (
	"context"
	"time"

	"github.com/AceDarkknight/k8s-analyzer-agent/internal/agent/safety"
	"github.com/AceDarkknight/k8s-analyzer-agent/internal/client/gateway"
	"github.com/AceDarkknight/k8s-analyzer-agent/internal/config"
	"github.com/AceDarkknight/k8s-analyzer-agent/internal/llm"
	"github.com/AceDarkknight/k8s-analyzer-agent/internal/logger"
	"github.com/AceDarkknight/k8s-analyzer-agent/internal/state"
	"github.com/AceDarkknight/k8s-analyzer-agent/internal/store"
	"github.com/AceDarkknight/k8s-analyzer-agent/internal/summarizer"
)

// Agent 主诊断 Agent
type Agent struct {
	graph *Graph
	cfg   *config.AgentConfig
}

// NewAgent 创建主诊断 Agent
func NewAgent(
	gw *gateway.GatewayClient,
	sa *safety.SafetyAgent,
	router *llm.LLMRouter,
	reactLLM *llm.ReActLLM,
	findingStore store.FindingStore,
	toolCache store.ToolCacheStore,
	cfg *config.AgentConfig,
) *Agent {
	// 1. 创建 OutputSummarizer
	sum := summarizer.NewOutputSummarizer(cfg.OutputMaxLines, cfg.OutputMaxChars)

	// 解析缓存 TTL
	cacheTTL, err := time.ParseDuration(cfg.ToolCache.TTL)
	if err != nil {
		cacheTTL = 10 * time.Minute
	}

	// 2. 创建各节点
	infoNode := NewInfoNode(gw, cfg.MaxNamespaces)
	decisionNode := NewDecisionNode(router)
	actionNode := NewActionNode(gw, sa, reactLLM, sum, toolCache, cacheTTL)
	compressNode := NewCompressNode(cfg.CompressThreshold, 3)
	reportNode := NewReportNode(router, findingStore)

	// 3. 创建 Graph（验证迭代逻辑内置于 Graph.Run 中）
	graph := NewGraph(infoNode, decisionNode, actionNode, compressNode, reportNode, cfg.VerifyRecommendations, cfg.MaxVerifyIterations)

	// 4. 返回 Agent
	return &Agent{
		graph: graph,
		cfg:   cfg,
	}
}

// Run 执行诊断
func (a *Agent) Run(ctx context.Context, userQuery string) (*state.AnalysisResult, error) {
	logger.Info("Agent: starting diagnosis", logger.String("query", userQuery))

	// 1. 创建 State
	s := state.NewState(userQuery, a.cfg.MaxIterations, a.cfg.CompressThreshold)

	// 2. 调用 graph.Run(ctx, state)
	finalState, err := a.graph.Run(ctx, s)
	if err != nil {
		logger.Error("Agent: graph execution failed", logger.Err(err))
		return nil, err
	}

	// 3. 返回 state.AnalysisResult
	if finalState.AnalysisResult == nil {
		logger.Warn("Agent: no analysis result generated")
		return &state.AnalysisResult{
			Summary:   "诊断未完成，未生成分析报告",
			Severity:  "unknown",
			RootCause: "未知",
			Status:    "failed",
		}, nil
	}

	logger.Info("Agent: diagnosis completed",
		logger.String("status", finalState.AnalysisResult.Status),
		logger.Int("findings", len(finalState.AnalysisResult.Findings)))

	return finalState.AnalysisResult, nil
}
