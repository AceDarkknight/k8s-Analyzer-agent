package api

import (
	"testing"

	"github.com/AceDarkknight/k8s-analyzer-agent/internal/state"
	trc "github.com/AceDarkknight/k8s-analyzer-agent/internal/trace"
	"github.com/stretchr/testify/require"
)

func TestNormalizeStatus(t *testing.T) {
	require.Equal(t, "success", normalizeStatus("completed"))
	require.Equal(t, "failed", normalizeStatus("error"))
	require.Equal(t, "failed", normalizeStatus("failed"))
	require.Equal(t, "pending", normalizeStatus("pending"))
}

func TestToTaskListData(t *testing.T) {
	records := []trc.TraceIndexRecord{{
		TaskID:           "task-1",
		Timestamp:        "2026-04-16T18:00:00+08:00",
		UserInput:        "检查 default 命名空间",
		Status:           "completed",
		TotalDurationMs:  1234,
		TotalTokens:      30,
		PromptTokens:     10,
		CompletionTokens: 20,
	}}
	data := toTaskListData(records, 1, 1, 20)
	require.Equal(t, 1, data.Total)
	require.Equal(t, "success", data.Items[0].Status)
	require.Equal(t, "检查 default 命名空间", data.Items[0].UserInput)
}

func TestEnrichReasoningSteps(t *testing.T) {
	// 已由 BuildTaskTrace 回填的推理步骤（工具调用已含执行结果）
	steps := []trc.TraceReasoningStep{{
		Iteration:   1,
		Thought:     "先看 Pod",
		Decision:    "continue",
		Observation: "已看到 1 个 Pod",
		DurationMs:  100,
		TokensUsed:  5,
		ToolCalls: []trc.TraceToolCallDetail{{
			ToolName:   "list_pods",
			Args:       map[string]interface{}{"namespace": "default"},
			Success:    true,
			Output:     "pod-a",
			DurationMs: 88,
			Timestamp:  "2026-04-16T18:00:00+08:00",
		}},
	}}
	got := enrichReasoningSteps(steps)
	require.Len(t, got, 1)
	require.Len(t, got[0].ToolCalls, 1)
	require.Equal(t, "pod-a", got[0].ToolCalls[0].Output)
	require.Equal(t, int64(88), got[0].ToolCalls[0].DurationMs)
	require.True(t, got[0].ToolCalls[0].Success)
}

func TestRenderAnalysisResult(t *testing.T) {
	result := &state.AnalysisResult{
		Summary:         "总结",
		Severity:        "info",
		RootCause:       "根因",
		Findings:        []state.Finding{{Resource: "pod/a", Severity: "warning", Message: "异常"}},
		Recommendations: []state.Recommendation{{Priority: "high", Action: "处理"}},
		Limitations:     "限制",
	}
	markdown := renderAnalysisResult(result)
	require.Contains(t, markdown, "## 概要")
	require.Contains(t, markdown, "## 根因")
	require.Contains(t, markdown, "## 发现")
	require.Contains(t, markdown, "## 建议")
	require.Contains(t, markdown, "## 限制")
}
