package diagnosis

import (
	"context"
	"time"

	"github.com/AceDarkknight/k8s-analyzer-agent/internal/agent/safety"
	"github.com/AceDarkknight/k8s-analyzer-agent/internal/client/gateway"
	"github.com/AceDarkknight/k8s-analyzer-agent/internal/config"
	"github.com/AceDarkknight/k8s-analyzer-agent/internal/llm"
	"github.com/AceDarkknight/k8s-analyzer-agent/internal/logger"
	"github.com/AceDarkknight/k8s-analyzer-agent/internal/metrics"
	skillpkg "github.com/AceDarkknight/k8s-analyzer-agent/internal/skill"
	"github.com/AceDarkknight/k8s-analyzer-agent/internal/state"
	"github.com/AceDarkknight/k8s-analyzer-agent/internal/store"
	"github.com/AceDarkknight/k8s-analyzer-agent/internal/summarizer"
	trc "github.com/AceDarkknight/k8s-analyzer-agent/internal/trace"
	"github.com/google/uuid"
)

// Agent 主诊断 Agent
type Agent struct {
	graph      *Graph
	cfg        *config.AgentConfig
	traceStore store.TraceStore
}

// NewAgent 创建主诊断 Agent
func NewAgent(
	gw *gateway.GatewayClient,
	sa *safety.SafetyAgent,
	router *llm.LLMRouter,
	reactLLM *llm.ReActLLM,
	findingStore store.FindingStore,
	toolCache store.ToolCacheStore,
	skillLoader *skillpkg.Loader,
	traceStore store.TraceStore,
	cfg *config.AgentConfig,
) *Agent {
	// 1. 创建 OutputSummarizer
	sum := summarizer.NewOutputSummarizer(cfg.OutputMaxLines, cfg.OutputMaxChars)

	// 解析缓存 TTL
	cacheTTL, err := time.ParseDuration(cfg.ToolCache.TTL)
	if err != nil {
		cacheTTL = 10 * time.Minute
	}

	recorder := trc.NewTaskRecorder(256)
	reactLLM.SetRecorder(recorder)
	// 2. 创建各节点
	infoNode := NewInfoNode(gw, cfg.MaxNamespaces)
	decisionNode := NewDecisionNode(router, skillLoader, recorder)
	actionNode := NewActionNode(gw, sa, reactLLM, sum, toolCache, cacheTTL, recorder)
	compressNode := NewCompressNode(cfg.CompressThreshold, 3)
	reportNode := NewReportNode(router, findingStore, recorder)

	// 3. 创建 Graph（验证迭代逻辑内置于 Graph.Run 中）
	graph := NewGraph(infoNode, decisionNode, actionNode, compressNode, reportNode, skillLoader, cfg.VerifyRecommendations, cfg.MaxVerifyIterations, recorder)

	// 4. 返回 Agent
	return &Agent{
		graph:      graph,
		cfg:        cfg,
		traceStore: traceStore,
	}
}

// Run 执行诊断
func (a *Agent) Run(ctx context.Context, userQuery string) (*state.AnalysisResult, error) {
	logger.Info("Agent: starting diagnosis", logger.String("query", userQuery))
	taskID := uuid.NewString()
	startTime := time.Now()

	// 1. 创建 State
	s := state.NewState(userQuery, a.cfg.MaxIterations, a.cfg.CompressThreshold)
	if recorder := a.graph.Recorder(); recorder != nil {
		recorder.Emit(trc.TaskStartedEvent{TaskID: taskID, StartedAt: startTime, UserInput: userQuery})
		defer func() {
			status := "failed"
			if s.AnalysisResult != nil && s.AnalysisResult.Status != "" {
				status = s.AnalysisResult.Status
			}
			errText := ""
			if s.LastError != nil {
				errText = s.LastError.Error()
			}
			recorder.Emit(trc.TaskFinishedEvent{FinishedAt: time.Now(), Status: status, Err: errText})
			recorder.Close()
			recorder.Wait()
			// 记录 Prometheus 指标
			durationSeconds := time.Since(startTime).Seconds()
			metrics.RecordTaskComplete(status, durationSeconds)
			metrics.RecordTokenUsage("aggregated", s.TotalPromptTokens, s.TotalCompletionTokens)
			if a.traceStore != nil {
				trace := trc.BuildTaskTrace(recorder.Snapshot(), s)
				trace = trc.SanitizeTaskTrace(trace)
				if trace != nil {
					if saveErr := a.traceStore.SaveTrace(ctx, trace); saveErr != nil {
						logger.Error("Agent: failed to save trace", logger.Err(saveErr), logger.String("task_id", taskID))
					} else {
						logger.Info("Agent: trace saved", logger.String("task_id", taskID))
					}
				}
			}
		}()
	}

	// 2. 调用 graph.Run(ctx, state)
	finalState, err := a.graph.Run(ctx, s)
	if err != nil {
		s.SetLastError(err)
		logger.Error("Agent: graph execution failed", logger.Err(err))
		return nil, err
	}

	// 3. 返回 state.AnalysisResult
	if finalState.AnalysisResult == nil {
		logger.Warn("Agent: no analysis result generated")
		finalState.SetAnalysisResult(&state.AnalysisResult{
			Summary:   "诊断未完成，未生成分析报告",
			Severity:  "unknown",
			RootCause: "未知",
			Status:    "failed",
		})
		return finalState.AnalysisResult, nil
	}

	logger.Info("Agent: diagnosis completed",
		logger.String("status", finalState.AnalysisResult.Status),
		logger.Int("findings", len(finalState.AnalysisResult.Findings)))

	return finalState.AnalysisResult, nil
}
