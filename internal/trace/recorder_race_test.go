package trace

import (
	"sync"
	"testing"
	"time"
)

func TestTaskRecorder_ConcurrentEmitAndClose(t *testing.T) {
	for i := 0; i < 100; i++ {
		r := NewTaskRecorder(1)
		var wg sync.WaitGroup
		wg.Add(2)

		go func() {
			defer wg.Done()
			r.Emit(TaskStartedEvent{TaskID: "task", StartedAt: time.Unix(1, 0), UserInput: "中文"})
		}()

		go func() {
			defer wg.Done()
			r.Close()
		}()

		wg.Wait()
		r.Wait()
	}
}
