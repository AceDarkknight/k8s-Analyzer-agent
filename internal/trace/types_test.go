package trace

import (
	"testing"
	"time"

	"github.com/AceDarkknight/k8s-analyzer-agent/internal/state"
)

func TestBuildTaskTraceAndIndex(t *testing.T) {
	draft := &TaskTraceDraft{
		TaskID:     "task-1",
		StartedAt:  time.Unix(100, 0),
		FinishedAt: time.Unix(160, 0),
		UserInput:  "检查 default 命名空间中的异常 Pod",
		Status:     "completed",
		TokenUsage: TokenUsage{PromptTokens: 10, CompletionTokens: 20, TotalTokens: 30},
		ToolExecutions: []TraceToolExecution{{
			ToolName:  "list_pods",
			Iteration: 1,
			Success:   true,
			Output:    "pod-a",
			Timestamp: "2026-04-16T18:00:00+08:00",
		}},
		ReasoningSteps: map[int]TraceReasoningStep{
			1: {
				Iteration:   1,
				Timestamp:   "2026-04-16T18:00:00+08:00",
				Thought:     "先看 Pod",
				Decision:    "continue",
				Observation: "已发现 1 个 Pod",
				DurationMs:  123,
				TokensUsed:  30,
			},
		},
	}
	st := &state.State{ActiveSkillName: "skill-a", AnalysisResult: &state.AnalysisResult{Summary: "ok", Status: "completed"}, K8sInfo: &state.K8sInfo{Namespaces: []string{"default"}}}

	trace := BuildTaskTrace(draft, st)
	if trace == nil {
		t.Fatal("expected trace")
	}
	if trace.TaskID != draft.TaskID || trace.UserInput != draft.UserInput {
		t.Fatalf("unexpected trace basic fields: %+v", trace)
	}
	if trace.TotalDurationMs != 60000 {
		t.Fatalf("unexpected duration: %d", trace.TotalDurationMs)
	}
	if trace.TokenUsage.TotalTokens != 30 {
		t.Fatalf("unexpected token usage: %+v", trace.TokenUsage)
	}
	if len(trace.ReasoningHistory) != 1 || trace.ReasoningHistory[0].Thought != "先看 Pod" {
		t.Fatalf("unexpected reasoning history: %+v", trace.ReasoningHistory)
	}
	idx := BuildTraceIndex(trace)
	if idx.TotalTokens != 30 || idx.PromptTokens != 10 || idx.CompletionTokens != 20 {
		t.Fatalf("unexpected index: %+v", idx)
	}
}
