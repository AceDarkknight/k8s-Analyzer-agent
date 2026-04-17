package trace

import (
	"testing"
	"time"
)

func TestTaskRecorderLifecycle(t *testing.T) {
	r := NewTaskRecorder(4)
	r.Emit(TaskStartedEvent{TaskID: "task-1", StartedAt: time.Unix(100, 0), UserInput: "检查中文"})
	r.Emit(LLMTokenUsedEvent{Source: "decision", PromptTokens: 1, CompletionTokens: 2, TotalTokens: 3})
	r.Emit(ToolExecutedEvent{Execution: TraceToolExecution{ToolName: "list_pods", Iteration: 1, Success: true, Output: "ok", Timestamp: "2026-04-16T18:00:00+08:00"}})
	r.Emit(TaskFinishedEvent{FinishedAt: time.Unix(160, 0), Status: "completed"})
	r.Close()
	r.Wait()

	snap := r.Snapshot()
	if snap == nil {
		t.Fatal("expected snapshot")
	}
	if snap.TaskID != "task-1" || snap.UserInput != "检查中文" {
		t.Fatalf("unexpected snapshot: %+v", snap)
	}
	if snap.TokenUsage.TotalTokens != 3 {
		t.Fatalf("unexpected token usage: %+v", snap.TokenUsage)
	}
	if len(snap.ToolExecutions) != 1 || snap.ToolExecutions[0].ToolName != "list_pods" {
		t.Fatalf("unexpected tool executions: %+v", snap.ToolExecutions)
	}
	if snap.FinishedAt.Unix() != 160 {
		t.Fatalf("unexpected finished time: %v", snap.FinishedAt)
	}
}
