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

func TestBuildTaskTrace_EnrichStepsWithExecutions_MatchesSameToolByArgs(t *testing.T) {
	draft := &TaskTraceDraft{
		TaskID:    "task-2",
		StartedAt: time.Unix(200, 0),
		FinishedAt: time.Unix(260, 0),
		UserInput: "诊断一下这个集群",
		ToolExecutions: []TraceToolExecution{
			{
				ToolName:  "execute_safe_command",
				Iteration: 1,
				Args: map[string]interface{}{
					"command": "df -h",
				},
				Success:   true,
				Output:    "disk output",
				Timestamp: "2026-04-27T11:00:01+08:00",
			},
			{
				ToolName:  "execute_safe_command",
				Iteration: 1,
				Args: map[string]interface{}{
					"command": "free -h",
				},
				Success:   true,
				Output:    "memory output",
				Timestamp: "2026-04-27T11:00:00+08:00",
			},
		},
		ReasoningSteps: map[int]TraceReasoningStep{
			1: {
				Iteration: 1,
				Timestamp: "2026-04-27T11:00:00+08:00",
				Thought:   "检查资源",
				Decision:  "execute_plan",
				ToolCalls: []TraceToolCallDetail{
					{
						ToolName: "execute_safe_command",
						Args: map[string]interface{}{
							"command": "free -h",
						},
					},
					{
						ToolName: "execute_safe_command",
						Args: map[string]interface{}{
							"command": "df -h",
						},
					},
				},
			},
		},
	}

	trace := BuildTaskTrace(draft, nil)
	if trace == nil {
		t.Fatal("expected trace")
	}
	if len(trace.ReasoningHistory) != 1 {
		t.Fatalf("unexpected reasoning history length: %d", len(trace.ReasoningHistory))
	}
	toolCalls := trace.ReasoningHistory[0].ToolCalls
	if len(toolCalls) != 2 {
		t.Fatalf("unexpected tool call length: %d", len(toolCalls))
	}
	if toolCalls[0].Output != "memory output" {
		t.Fatalf("expected first tool call output to match free -h, got %q", toolCalls[0].Output)
	}
	if toolCalls[1].Output != "disk output" {
		t.Fatalf("expected second tool call output to match df -h, got %q", toolCalls[1].Output)
	}
}
