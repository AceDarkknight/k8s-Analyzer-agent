package trace

import (
	"time"

	"github.com/AceDarkknight/k8s-analyzer-agent/internal/state"
)

// TraceEvent 追踪事件接口
type TraceEvent interface {
	apply(draft *TaskTraceDraft)
}

type TaskStartedEvent struct {
	TaskID    string
	StartedAt time.Time
	UserInput string
}

func (e TaskStartedEvent) apply(draft *TaskTraceDraft) {
	draft.TaskID = e.TaskID
	draft.StartedAt = e.StartedAt
	draft.UserInput = e.UserInput
}

type LLMTokenUsedEvent struct {
	Source           string
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

func (e LLMTokenUsedEvent) apply(draft *TaskTraceDraft) {
	draft.TokenUsage.PromptTokens += e.PromptTokens
	draft.TokenUsage.CompletionTokens += e.CompletionTokens
	draft.TokenUsage.TotalTokens += e.TotalTokens
}

type ToolExecutedEvent struct {
	Execution TraceToolExecution
}

func (e ToolExecutedEvent) apply(draft *TaskTraceDraft) {
	draft.ToolExecutions = append(draft.ToolExecutions, e.Execution)
}

type BlockedCommandEvent struct {
	Command state.BlockedCommand
}

func (e BlockedCommandEvent) apply(draft *TaskTraceDraft) {
	draft.BlockedCommands = append(draft.BlockedCommands, e.Command)
}

type ReasoningStepUpdatedEvent struct {
	Step state.ReasoningStep
}

func (e ReasoningStepUpdatedEvent) apply(draft *TaskTraceDraft) {
	draft.ReasoningSteps[e.Step.Iteration] = convertReasoningStep(e.Step)
}

type TaskFinishedEvent struct {
	FinishedAt time.Time
	Status     string
	Err        string
}

func (e TaskFinishedEvent) apply(draft *TaskTraceDraft) {
	draft.FinishedAt = e.FinishedAt
	if e.Status != "" {
		draft.Status = e.Status
	}
	if e.Err != "" {
		draft.Error = e.Err
	}
}
