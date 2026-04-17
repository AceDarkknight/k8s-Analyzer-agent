package api

import (
	"fmt"
	"strings"

	"github.com/AceDarkknight/k8s-analyzer-agent/internal/state"
	trc "github.com/AceDarkknight/k8s-analyzer-agent/internal/trace"
)

type taskListData struct {
	Items []taskIndexRecord `json:"items"`
	Total int               `json:"total"`
	Page  int               `json:"page"`
	Size  int               `json:"size"`
}

type taskStatsData struct {
	TotalTasks        int               `json:"total_tasks"`
	SuccessTasks      int               `json:"success_tasks"`
	FailedTasks       int               `json:"failed_tasks"`
	SuccessRate       float64           `json:"success_rate"`
	TotalTokens       int               `json:"total_tokens"`
	PromptTokens      int               `json:"prompt_tokens"`
	CompletionTokens  int               `json:"completion_tokens"`
	AverageDurationMs int64             `json:"average_duration_ms"`
	Trend             []taskTrendPoint  `json:"trend"`
	ToolUsage         []toolUsageRecord `json:"tool_usage"`
}

type taskTrendPoint struct {
	Date    string `json:"date"`
	Success int    `json:"success"`
	Failed  int    `json:"failed"`
}

type toolUsageRecord struct {
	ToolName string `json:"tool_name"`
	Success  int    `json:"success"`
	Failed   int    `json:"failed"`
}

type taskIndexRecord struct {
	TaskID           string `json:"task_id"`
	Timestamp        string `json:"timestamp"`
	UserInput        string `json:"user_input"`
	Status           string `json:"status"`
	TotalDurationMs  int64  `json:"total_duration_ms"`
	TotalTokens      int    `json:"total_tokens"`
	PromptTokens     int    `json:"prompt_tokens"`
	CompletionTokens int    `json:"completion_tokens"`
}

type toolCallDTO struct {
	ToolName   string                 `json:"tool_name"`
	Args       map[string]interface{} `json:"args,omitempty"`
	Success    bool                   `json:"success"`
	Output     string                 `json:"output,omitempty"`
	DurationMs int64                  `json:"duration_ms"`
	Timestamp  string                 `json:"timestamp,omitempty"`
	Cached     bool                   `json:"cached"`
}

type reasoningStepDTO struct {
	Iteration   int           `json:"iteration"`
	Thought     string        `json:"thought"`
	Decision    string        `json:"decision"`
	ToolCalls   []toolCallDTO `json:"tool_calls,omitempty"`
	Observation string        `json:"observation,omitempty"`
	DurationMs  int64         `json:"duration_ms"`
	TokensUsed  int           `json:"tokens_used"`
}

type taskTraceDTO struct {
	TaskID           string             `json:"task_id"`
	Timestamp        string             `json:"timestamp"`
	UserInput        string             `json:"user_input"`
	Status           string             `json:"status"`
	TotalDurationMs  int64              `json:"total_duration_ms"`
	TokenUsage       trc.TokenUsage     `json:"token_usage"`
	K8sInfo          *state.K8sInfo     `json:"k8s_info,omitempty"`
	ReasoningHistory []reasoningStepDTO `json:"reasoning_history,omitempty"`
	ToolExecutions   []toolCallDTO      `json:"tool_executions,omitempty"`
	AnalysisResult   string             `json:"analysis_result,omitempty"`
	Error            string             `json:"error,omitempty"`
	ActiveSkillName  string             `json:"active_skill_name,omitempty"`
}

func normalizeStatus(status string) string {
	switch strings.ToLower(status) {
	case "completed", "success":
		return "success"
	case "partial", "failed", "error":
		return "failed"
	default:
		return status
	}
}

func toTaskListData(records []trc.TraceIndexRecord, total, page, size int) taskListData {
	items := make([]taskIndexRecord, 0, len(records))
	for _, rec := range records {
		items = append(items, taskIndexRecord{
			TaskID:           rec.TaskID,
			Timestamp:        rec.Timestamp,
			UserInput:        rec.UserInput,
			Status:           normalizeStatus(rec.Status),
			TotalDurationMs:  rec.TotalDurationMs,
			TotalTokens:      rec.TotalTokens,
			PromptTokens:     rec.PromptTokens,
			CompletionTokens: rec.CompletionTokens,
		})
	}
	return taskListData{Items: items, Total: total, Page: page, Size: size}
}

func toTaskTraceDTO(trace *trc.TaskTrace) taskTraceDTO {
	toolExecs := make([]toolCallDTO, 0, len(trace.ToolExecutions))
	for _, exec := range trace.ToolExecutions {
		toolExecs = append(toolExecs, toolCallDTO{
			ToolName:   exec.ToolName,
			Args:       exec.Args,
			Success:    exec.Success,
			Output:     exec.Output,
			DurationMs: exec.DurationMs,
			Timestamp:  exec.Timestamp,
			Cached:     exec.Cached,
		})
	}
	steps := enrichReasoningSteps(trace.ReasoningHistory, trace.ToolExecutions)
	return taskTraceDTO{
		TaskID:           trace.TaskID,
		Timestamp:        trace.Timestamp,
		UserInput:        trace.UserInput,
		Status:           normalizeStatus(trace.Status),
		TotalDurationMs:  trace.TotalDurationMs,
		TokenUsage:       trace.TokenUsage,
		K8sInfo:          trace.K8sInfo,
		ReasoningHistory: steps,
		ToolExecutions:   toolExecs,
		AnalysisResult:   renderAnalysisResult(trace.AnalysisResult),
		Error:            trace.Error,
		ActiveSkillName:  trace.ActiveSkillName,
	}
}

func enrichReasoningSteps(steps []trc.TraceReasoningStep, execs []trc.TraceToolExecution) []reasoningStepDTO {
	// 按 (iteration, toolName) 建立索引，同名工具保留有序列表以处理同轮多次调用
	type iterToolKey struct {
		iteration int
		toolName  string
	}
	execIndex := map[iterToolKey][]trc.TraceToolExecution{}
	for _, exec := range execs {
		key := iterToolKey{exec.Iteration, exec.ToolName}
		execIndex[key] = append(execIndex[key], exec)
	}
	// 记录每个 (iteration, toolName) 已消费的偏移量
	usedOffset := map[iterToolKey]int{}

	result := make([]reasoningStepDTO, 0, len(steps))
	for _, step := range steps {
		calls := make([]toolCallDTO, 0, len(step.ToolCalls))
		for _, call := range step.ToolCalls {
			dto := toolCallDTO{ToolName: call.Name, Args: call.Args}
			key := iterToolKey{step.Iteration, call.Name}
			if list, ok := execIndex[key]; ok {
				idx := usedOffset[key]
				if idx < len(list) {
					exec := list[idx]
					usedOffset[key] = idx + 1
					dto.Success = exec.Success
					dto.Output = exec.Output
					dto.DurationMs = exec.DurationMs
					dto.Timestamp = exec.Timestamp
					dto.Cached = exec.Cached
				}
			}
			calls = append(calls, dto)
		}
		result = append(result, reasoningStepDTO{
			Iteration:   step.Iteration,
			Thought:     step.Thought,
			Decision:    step.Decision,
			ToolCalls:   calls,
			Observation: step.Observation,
			DurationMs:  step.DurationMs,
			TokensUsed:  step.TokensUsed,
		})
	}
	return result
}

func renderAnalysisResult(result *state.AnalysisResult) string {
	if result == nil {
		return ""
	}
	var b strings.Builder
	if result.Summary != "" {
		b.WriteString("## 概要\n\n")
		b.WriteString(result.Summary)
		b.WriteString("\n\n")
	}
	if result.RootCause != "" {
		b.WriteString("## 根因\n\n")
		b.WriteString(result.RootCause)
		b.WriteString("\n\n")
	}
	if len(result.Findings) > 0 {
		b.WriteString("## 发现\n\n")
		for _, f := range result.Findings {
			b.WriteString(fmt.Sprintf("- **[%s] %s**：%s\n", f.Severity, f.Resource, f.Message))
			if f.Evidence != "" {
				b.WriteString(fmt.Sprintf("  - 证据：%s\n", f.Evidence))
			}
		}
		b.WriteString("\n")
	}
	if len(result.Recommendations) > 0 {
		b.WriteString("## 建议\n\n")
		for _, r := range result.Recommendations {
			b.WriteString(fmt.Sprintf("- **[%s]** %s\n", r.Priority, r.Action))
			if r.Command != "" {
				b.WriteString(fmt.Sprintf("  - 命令：`%s`\n", r.Command))
			}
			if r.VerifyResult != "" {
				b.WriteString(fmt.Sprintf("  - 验证：%s\n", r.VerifyResult))
			}
		}
		b.WriteString("\n")
	}
	if result.Limitations != "" {
		b.WriteString("## 限制\n\n")
		b.WriteString(result.Limitations)
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}
