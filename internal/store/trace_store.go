package store

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	trc "github.com/AceDarkknight/k8s-analyzer-agent/internal/trace"
)

type TraceStore interface {
	SaveTrace(ctx context.Context, trace *trc.TaskTrace) error
	GetTrace(ctx context.Context, taskID string) (*trc.TaskTrace, error)
	ListTraces(ctx context.Context, page, size int) ([]trc.TraceIndexRecord, int, error)
	Close() error
}

type FileTraceStore struct {
	baseDir   string
	indexFile string
	mu        sync.Mutex
}

func NewFileTraceStore(baseDir string) (*FileTraceStore, error) {
	if baseDir == "" {
		return nil, fmt.Errorf("trace dir is required")
	}
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return nil, fmt.Errorf("create trace dir: %w", err)
	}
	return &FileTraceStore{
		baseDir:   baseDir,
		indexFile: filepath.Join(baseDir, "traces_index.jsonl"),
	}, nil
}

func (s *FileTraceStore) SaveTrace(ctx context.Context, trace *trc.TaskTrace) error {
	if trace == nil {
		return fmt.Errorf("trace is nil")
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	detailPath := filepath.Join(s.baseDir, trace.TaskID+".json")
	detailBytes, err := json.MarshalIndent(trace, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal trace: %w", err)
	}
	if err := os.WriteFile(detailPath, detailBytes, 0o644); err != nil {
		return fmt.Errorf("write trace detail: %w", err)
	}
	indexRecord := trc.BuildTraceIndex(trace)
	line, err := json.Marshal(indexRecord)
	if err != nil {
		return fmt.Errorf("marshal trace index: %w", err)
	}
	f, err := os.OpenFile(s.indexFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open trace index: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("append trace index: %w", err)
	}
	return nil
}

func (s *FileTraceStore) GetTrace(ctx context.Context, taskID string) (*trc.TaskTrace, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	data, err := os.ReadFile(filepath.Join(s.baseDir, taskID+".json"))
	if err != nil {
		return nil, err
	}
	var trace trc.TaskTrace
	if err := json.Unmarshal(data, &trace); err != nil {
		return nil, fmt.Errorf("unmarshal trace: %w", err)
	}
	return &trace, nil
}

func (s *FileTraceStore) ListTraces(ctx context.Context, page, size int) ([]trc.TraceIndexRecord, int, error) {
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 20
	}
	select {
	case <-ctx.Done():
		return nil, 0, ctx.Err()
	default:
	}
	f, err := os.Open(s.indexFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []trc.TraceIndexRecord{}, 0, nil
		}
		return nil, 0, err
	}
	defer f.Close()
	var records []trc.TraceIndexRecord
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec trc.TraceIndexRecord
		if err := json.Unmarshal(line, &rec); err == nil {
			records = append(records, rec)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, 0, err
	}
	total := len(records)
	start := total - (page * size)
	end := total - ((page - 1) * size)
	if start < 0 {
		start = 0
	}
	if end < 0 {
		end = 0
	}
	if start > total {
		start = total
	}
	if end > total {
		end = total
	}
	result := make([]trc.TraceIndexRecord, 0, end-start)
	for i := end - 1; i >= start; i-- {
		result = append(result, records[i])
	}
	return result, total, nil
}

func (s *FileTraceStore) Close() error { return nil }
