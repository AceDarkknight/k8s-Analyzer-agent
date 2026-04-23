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

// LLMCallRecord 单次 LLM 调用记录
type LLMCallRecord struct {
	ModelType        string `json:"model_type"` // "light" | "power"
	ModelName        string `json:"model_name"` // 实际使用的模型名称
	Source           string `json:"source"`     // "decision" | "report" | "deep_query"
	PromptTokens     int    `json:"prompt_tokens"`
	CompletionTokens int    `json:"completion_tokens"`
	TotalTokens      int    `json:"total_tokens"`
	DurationMs       int64  `json:"duration_ms"`
	Timestamp        string `json:"timestamp"`
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

// TraceToolCallDetail 推理步骤内单次工具调用的完整记录（含执行结果与耗时）
type TraceToolCallDetail struct {
	ToolName   string                 `json:"tool_name"`
	Args       map[string]interface{} `json:"args,omitempty"`
	Success    bool                   `json:"success"`
	Output     string                 `json:"output,omitempty"`
	DurationMs int64                  `json:"duration_ms"`
	Timestamp  string                 `json:"timestamp,omitempty"`
	Cached     bool                   `json:"cached"`
}

type TraceReasoningStep struct {
	Iteration      int                   `json:"iteration"`
	Timestamp      string                `json:"timestamp"`
	Thought        string                `json:"thought"`
	Decision       string                `json:"decision"`
	DeepQueryTopic string                `json:"deep_query_topic,omitempty"`
	ToolCalls      []TraceToolCallDetail `json:"tool_calls,omitempty"`
	Observation    string                `json:"observation,omitempty"`
	DurationMs     int64                 `json:"duration_ms"`
	TokensUsed     int                   `json:"tokens_used"`
}

type TaskTraceDraft struct {
	TaskID          string
	StartedAt       time.Time
	FinishedAt      time.Time
	UserInput       string
	Status          string
	Error           string
	TokenUsage      TokenUsage
	LLMCalls        []LLMCallRecord
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
	LLMCalls         []LLMCallRecord        `json:"llm_calls,omitempty"`
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
	toolCalls := make([]TraceToolCallDetail, 0, len(step.ToolCalls))
	for _, tc := range step.ToolCalls {
		toolCalls = append(toolCalls, TraceToolCallDetail{
			ToolName: tc.Name,
			Args:     tc.Args,
		})
	}
	return TraceReasoningStep{
		Iteration:      step.Iteration,
		Timestamp:      step.Timestamp.Format(time.RFC3339),
		Thought:        step.Thought,
		Decision:       step.Decision,
		DeepQueryTopic: step.DeepQueryTopic,
		ToolCalls:      toolCalls,
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
		LLMCalls:        append([]LLMCallRecord(nil), draft.LLMCalls...),
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
	var steps []TraceReasoningStep

	if draft != nil && len(draft.ReasoningSteps) > 0 {
		keys := make([]int, 0, len(draft.ReasoningSteps))
		for k := range draft.ReasoningSteps {
			keys = append(keys, k)
		}
		sort.Ints(keys)
		for _, k := range keys {
			steps = append(steps, draft.ReasoningSteps[k])
		}
	} else if s != nil {
		history := s.GetReasoningHistory()
		for _, step := range history {
			steps = append(steps, convertReasoningStep(step))
		}
	}

	// 将 tool_executions 的实际执行结果（耗时、成功状态、输出）回填到推理步骤的工具调用中
	if draft != nil && len(draft.ToolExecutions) > 0 && len(steps) > 0 {
		enrichStepsWithExecutions(steps, draft.ToolExecutions)
	}

	return steps
}

// enrichStepsWithExecutions 将顶层 TraceToolExecution 的执行结果回填到推理步骤的工具调用明细中
func enrichStepsWithExecutions(steps []TraceReasoningStep, executions []TraceToolExecution) {
	// 按 iteration 建立索引
	execByIter := make(map[int][]TraceToolExecution)
	for _, exec := range executions {
		execByIter[exec.Iteration] = append(execByIter[exec.Iteration], exec)
	}

	for si := range steps {
		step := &steps[si]
		iterExecs := execByIter[step.Iteration]
		if len(iterExecs) == 0 || len(step.ToolCalls) == 0 {
			continue
		}
		// 按工具名分组，处理同一轮中多次调用同名工具的情况
		execsByName := make(map[string][]TraceToolExecution)
		for _, exec := range iterExecs {
			execsByName[exec.ToolName] = append(execsByName[exec.ToolName], exec)
		}
		usedIdx := make(map[string]int)
		for ti := range step.ToolCalls {
			tc := &step.ToolCalls[ti]
			execs := execsByName[tc.ToolName]
			idx := usedIdx[tc.ToolName]
			if idx < len(execs) {
				exec := execs[idx]
				tc.Success = exec.Success
				tc.DurationMs = exec.DurationMs
				tc.Output = exec.Output
				tc.Timestamp = exec.Timestamp
				tc.Cached = exec.Cached
				usedIdx[tc.ToolName] = idx + 1
			}
		}
	}
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
