package trace

import (
	"sync"

	"github.com/AceDarkknight/k8s-analyzer-agent/internal/state"
)

// TaskRecorder 任务级异步记录器
type TaskRecorder struct {
	events    chan TraceEvent
	draft     *TaskTraceDraft
	mu        sync.RWMutex
	stateMu   sync.Mutex
	wg        sync.WaitGroup
	closed    bool
	closeOnce sync.Once
}

func NewTaskRecorder(bufferSize int) *TaskRecorder {
	if bufferSize <= 0 {
		bufferSize = 128
	}
	r := &TaskRecorder{
		events: make(chan TraceEvent, bufferSize),
		draft: &TaskTraceDraft{
			ReasoningSteps: make(map[int]TraceReasoningStep),
		},
	}
	r.wg.Add(1)
	go r.run()
	return r
}

func (r *TaskRecorder) run() {
	defer r.wg.Done()
	for event := range r.events {
		if event == nil {
			continue
		}
		r.mu.Lock()
		event.apply(r.draft)
		r.mu.Unlock()
	}
}

func (r *TaskRecorder) Emit(event TraceEvent) {
	if r == nil || event == nil {
		return
	}
	// 检查 closed 标志时短暂持锁，检查完立即释放。
	// 不能在持锁期间做阻塞 channel 发送——会导致 stateMu 与 channel 背压形成死锁：
	// react_llm goroutine 持 stateMu 阻塞在 send，同时 agent.Generate 因 future 满也阻塞，双向等待。
	r.stateMu.Lock()
	if r.closed {
		r.stateMu.Unlock()
		return
	}
	r.stateMu.Unlock()

	// 释放锁后再发送；用 recover 防止 Close 与 Emit 极端并发时向已关闭 channel 发送引发 panic
	defer func() { recover() }()
	r.events <- event
}

func (r *TaskRecorder) Close() {
	if r == nil {
		return
	}
	r.closeOnce.Do(func() {
		r.stateMu.Lock()
		r.closed = true
		close(r.events)
		r.stateMu.Unlock()
	})
}

func (r *TaskRecorder) Wait() {
	if r == nil {
		return
	}
	r.wg.Wait()
}

func (r *TaskRecorder) Snapshot() *TaskTraceDraft {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	copyDraft := *r.draft
	copyDraft.ToolExecutions = append([]TraceToolExecution(nil), r.draft.ToolExecutions...)
	copyDraft.BlockedCommands = append([]state.BlockedCommand(nil), r.draft.BlockedCommands...)
	copyDraft.ReasoningSteps = make(map[int]TraceReasoningStep, len(r.draft.ReasoningSteps))
	for k, v := range r.draft.ReasoningSteps {
		copyDraft.ReasoningSteps[k] = v
	}
	return &copyDraft
}
