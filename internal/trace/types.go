package trace

import (
	"sort"
	"time"

	"github.com/AceDarkknight/k8s-analyzer-agent/internal/state"
)

type TokenUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type TraceToolExecution struct {
	ToolName   string                 `json:"tool_name"`
	Iteration  int                    `json:"iteration"`
	Args       map[string]interface{} `json:"args,omitempty"`
	Success    bool                   `json:"success"`
	Output     string                 `json:"output"`
	DurationMs int64                  `json:"duration_ms"`
	Timestamp  string                 `json:"timestamp"`
	Cached     bool                   `json:"cached"`
	Command    string                 `json:"command,omitempty"`
}

type TraceReasoningStep struct {
	Iteration      int              `json:"iteration"`
	Timestamp      string           `json:"timestamp"`
	Thought        string           `json:"thought"`
	Decision       string           `json:"decision"`
	DeepQueryTopic string           `json:"deep_query_topic,omitempty"`
	ToolCalls      []state.ToolCall `json:"tool_calls,omitempty"`
	Observation    string           `json:"observation,omitempty"`
	DurationMs     int64            `json:"duration_ms"`
	TokensUsed     int              `json:"tokens_used"`
}

type TaskTraceDraft struct {
	TaskID          string
	StartedAt       time.Time
	FinishedAt      time.Time
	UserInput       string
	Status          string
	Error           string
	TokenUsage      TokenUsage
	ToolExecutions  []TraceToolExecution
	BlockedCommands []state.BlockedCommand
	ReasoningSteps  map[int]TraceReasoningStep
}

type TaskTrace struct {
	TaskID           string                 `json:"task_id"`
	Timestamp        string                 `json:"timestamp"`
	UserInput        string                 `json:"user_input"`
	Status           string                 `json:"status"`
	TotalDurationMs  int64                  `json:"total_duration_ms"`
	TokenUsage       TokenUsage             `json:"token_usage"`
	K8sInfo          *state.K8sInfo         `json:"k8s_info,omitempty"`
	ReasoningHistory []TraceReasoningStep   `json:"reasoning_history,omitempty"`
	ToolExecutions   []TraceToolExecution   `json:"tool_executions,omitempty"`
	BlockedCommands  []state.BlockedCommand `json:"blocked_commands,omitempty"`
	AnalysisResult   *state.AnalysisResult  `json:"analysis_result,omitempty"`
	Error            string                 `json:"error,omitempty"`
	ActiveSkillName  string                 `json:"active_skill_name,omitempty"`
}

type TraceIndexRecord struct {
	TaskID           string `json:"task_id"`
	Timestamp        string `json:"timestamp"`
	UserInput        string `json:"user_input"`
	Status           string `json:"status"`
	TotalDurationMs  int64  `json:"total_duration_ms"`
	TotalTokens      int    `json:"total_tokens"`
	PromptTokens     int    `json:"prompt_tokens"`
	CompletionTokens int    `json:"completion_tokens"`
}

func convertReasoningStep(step state.ReasoningStep) TraceReasoningStep {
	return TraceReasoningStep{
		Iteration:      step.Iteration,
		Timestamp:      step.Timestamp.Format(time.RFC3339),
		Thought:        step.Thought,
		Decision:       step.Decision,
		DeepQueryTopic: step.DeepQueryTopic,
		ToolCalls:      step.ToolCalls,
		Observation:    step.Observation,
		DurationMs:     step.Duration.Milliseconds(),
		TokensUsed:     step.TokensUsed,
	}
}

func BuildTaskTrace(draft *TaskTraceDraft, s *state.State) *TaskTrace {
	if draft == nil {
		return nil
	}
	finishedAt := draft.FinishedAt
	if finishedAt.IsZero() {
		finishedAt = time.Now()
	}
	status := draft.Status
	if status == "" {
		if s != nil && s.AnalysisResult != nil && s.AnalysisResult.Status != "" {
			status = s.AnalysisResult.Status
		} else if draft.Error != "" {
			status = "failed"
		} else {
			status = "completed"
		}
	}
	trace := &TaskTrace{
		TaskID:          draft.TaskID,
		Timestamp:       draft.StartedAt.Format(time.RFC3339),
		UserInput:       draft.UserInput,
		Status:          status,
		TotalDurationMs: finishedAt.Sub(draft.StartedAt).Milliseconds(),
		TokenUsage:      draft.TokenUsage,
		ToolExecutions:  append([]TraceToolExecution(nil), draft.ToolExecutions...),
		BlockedCommands: append([]state.BlockedCommand(nil), draft.BlockedCommands...),
		Error:           draft.Error,
	}
	if s != nil {
		trace.K8sInfo = s.GetK8sInfo()
		trace.AnalysisResult = s.AnalysisResult
		trace.ActiveSkillName = s.ActiveSkillName
	}
	trace.ReasoningHistory = buildReasoningHistory(draft, s)
	return trace
}

func buildReasoningHistory(draft *TaskTraceDraft, s *state.State) []TraceReasoningStep {
	if draft != nil && len(draft.ReasoningSteps) > 0 {
		steps := make([]TraceReasoningStep, 0, len(draft.ReasoningSteps))
		keys := make([]int, 0, len(draft.ReasoningSteps))
		for k := range draft.ReasoningSteps {
			keys = append(keys, k)
		}
		sort.Ints(keys)
		for _, k := range keys {
			steps = append(steps, draft.ReasoningSteps[k])
		}
		return steps
	}
	if s == nil {
		return nil
	}
	history := s.GetReasoningHistory()
	steps := make([]TraceReasoningStep, 0, len(history))
	for _, step := range history {
		steps = append(steps, convertReasoningStep(step))
	}
	return steps
}

func BuildTraceIndex(trace *TaskTrace) TraceIndexRecord {
	if trace == nil {
		return TraceIndexRecord{}
	}
	return TraceIndexRecord{
		TaskID:           trace.TaskID,
		Timestamp:        trace.Timestamp,
		UserInput:        trace.UserInput,
		Status:           trace.Status,
		TotalDurationMs:  trace.TotalDurationMs,
		TotalTokens:      trace.TokenUsage.TotalTokens,
		PromptTokens:     trace.TokenUsage.PromptTokens,
		CompletionTokens: trace.TokenUsage.CompletionTokens,
	}
}
