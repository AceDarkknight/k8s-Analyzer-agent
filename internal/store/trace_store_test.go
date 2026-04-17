package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	trc "github.com/AceDarkknight/k8s-analyzer-agent/internal/trace"
	"github.com/stretchr/testify/require"
)

func TestFileTraceStore_SaveGetList(t *testing.T) {
	dir := t.TempDir()
	store, err := NewFileTraceStore(dir)
	require.NoError(t, err)
	defer store.Close()

	trace := &trc.TaskTrace{
		TaskID:          "task-1",
		Timestamp:       time.Now().Format(time.RFC3339),
		UserInput:       "检查 default 命名空间中的异常 Pod",
		Status:          "completed",
		TotalDurationMs: 1234,
		TokenUsage:      trc.TokenUsage{PromptTokens: 1, CompletionTokens: 2, TotalTokens: 3},
	}
	require.NoError(t, store.SaveTrace(context.Background(), trace))

	got, err := store.GetTrace(context.Background(), trace.TaskID)
	require.NoError(t, err)
	require.Equal(t, trace.UserInput, got.UserInput)
	require.Equal(t, trace.TokenUsage.TotalTokens, got.TokenUsage.TotalTokens)

	records, total, err := store.ListTraces(context.Background(), 1, 20)
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Len(t, records, 1)
	require.Equal(t, trace.TaskID, records[0].TaskID)
	require.Equal(t, trace.UserInput, records[0].UserInput)

	_, err = filepath.Abs(dir)
	require.NoError(t, err)
}
