package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	taskTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "agent_task_total",
		Help: "Total number of agent tasks by status.",
	}, []string{"status"})

	llmTokensTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "agent_llm_tokens_total",
		Help: "Total LLM token usage by model and type.",
	}, []string{"model", "type"})

	toolCallsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "agent_tool_calls_total",
		Help: "Total tool calls by tool name and status.",
	}, []string{"tool_name", "status"})

	taskDurationSeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "agent_task_duration_seconds",
		Help:    "Agent task duration in seconds.",
		Buckets: prometheus.DefBuckets,
	}, []string{"status"})
)

// RecordTaskComplete 记录任务完成（由 Agent.Run 调用）
func RecordTaskComplete(status string, durationSeconds float64) {
	taskTotal.WithLabelValues(status).Inc()
	taskDurationSeconds.WithLabelValues(status).Observe(durationSeconds)
}

// RecordTokenUsage 记录 LLM Token 消耗（由 Agent.Run 调用）
func RecordTokenUsage(model string, promptTokens, completionTokens int) {
	llmTokensTotal.WithLabelValues(model, "prompt").Add(float64(promptTokens))
	llmTokensTotal.WithLabelValues(model, "completion").Add(float64(completionTokens))
}

// RecordToolCall 记录工具调用（由 ActionNode 调用）
func RecordToolCall(toolName string, success bool) {
	status := "success"
	if !success {
		status = "failed"
	}
	toolCallsTotal.WithLabelValues(toolName, status).Inc()
}
