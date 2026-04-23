package store

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"

	trc "github.com/AceDarkknight/k8s-analyzer-agent/internal/trace"
)

type TraceStore interface {
	// SaveTrace 保存完整 trace 并将索引记录写在 traces_index.jsonl 最前面
	SaveTrace(ctx context.Context, trace *trc.TaskTrace) error
	// CheckpointTrace 仅写入 trace 详情文件（不更新索引），用于每轮决策后的增量持久化
	CheckpointTrace(ctx context.Context, trace *trc.TaskTrace) error
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

// SaveTrace 写入 trace 详情文件，并将索引记录 prepend 到 traces_index.jsonl 最前面
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

	// 写 detail 文件
	if err := s.writeDetail(trace); err != nil {
		return err
	}

	// 将索引记录写在文件最前面
	indexRecord := trc.BuildTraceIndex(trace)
	line, err := json.Marshal(indexRecord)
	if err != nil {
		return fmt.Errorf("marshal trace index: %w", err)
	}
	return s.prependIndex(line)
}

// CheckpointTrace 仅写入 trace 详情文件，不更新索引（用于每轮 DecisionNode 后的增量持久化）
func (s *FileTraceStore) CheckpointTrace(ctx context.Context, trace *trc.TaskTrace) error {
	if trace == nil || trace.TaskID == "" {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writeDetail(trace)
}

// writeDetail 将 trace 序列化并写入 {taskID}.json（在持锁状态下调用）
func (s *FileTraceStore) writeDetail(trace *trc.TaskTrace) error {
	detailBytes, err := json.MarshalIndent(trace, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal trace: %w", err)
	}
	detailPath := filepath.Join(s.baseDir, trace.TaskID+".json")
	if err := os.WriteFile(detailPath, detailBytes, 0o644); err != nil {
		return fmt.Errorf("write trace detail: %w", err)
	}
	return nil
}

// prependIndex 将新行写在 traces_index.jsonl 文件最前面（在持锁状态下调用）
func (s *FileTraceStore) prependIndex(line []byte) error {
	existing, err := os.ReadFile(s.indexFile)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read trace index: %w", err)
	}
	// 新内容 = 新行 + 换行符 + 原有内容
	newContent := make([]byte, 0, len(line)+1+len(existing))
	newContent = append(newContent, line...)
	newContent = append(newContent, '\n')
	newContent = append(newContent, existing...)
	if err := os.WriteFile(s.indexFile, newContent, 0o644); err != nil {
		return fmt.Errorf("write trace index: %w", err)
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

// ListTraces 返回按时间戳降序排列的分页记录（兼容新旧两种索引文件格式）
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

	// 按时间戳降序排列，兼容旧格式（升序）和新格式（降序）混合文件
	sort.Slice(records, func(i, j int) bool {
		return records[i].Timestamp > records[j].Timestamp
	})

	total := len(records)
	start := (page - 1) * size
	if start >= total {
		return []trc.TraceIndexRecord{}, total, nil
	}
	end := start + size
	if end > total {
		end = total
	}
	return records[start:end], total, nil
}

func (s *FileTraceStore) Close() error { return nil }
