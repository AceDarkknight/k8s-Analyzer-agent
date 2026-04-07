package diagnosis

import (
	"context"
	"testing"

	"github.com/AceDarkknight/k8s-analyzer-agent/internal/state"
)

func TestDecisionNodeFallbackDecision(t *testing.T) {
	// 创建 mock router（实际测试中使用 nil，因为我们只测试 fallback）
	node := &DecisionNode{router: nil}

	tests := []struct {
		name           string
		iterationCount int
		k8sInfo        *state.K8sInfo
		expectedDecision string
		expectedToolCalls int
	}{
		{
			name:           "no abnormal pods",
			iterationCount: 1,
			k8sInfo: &state.K8sInfo{
				Namespaces: []string{"default"},
				Resources: map[string][]interface{}{
					"Pods": {},
				},
			},
			expectedDecision:  "report",
			expectedToolCalls: 0,
		},
		{
			name:           "max iterations reached",
			iterationCount: 9,
			k8sInfo: &state.K8sInfo{
				Namespaces: []string{"default"},
				Resources: map[string][]interface{}{
					"Pods": {
						state.PodInfo{Name: "bad-pod", Namespace: "default", Status: "CrashLoopBackOff"},
					},
				},
			},
			expectedDecision:  "report",
			expectedToolCalls: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := state.NewState("test query", 10, 4)
			for i := 0; i < tt.iterationCount; i++ {
				s.IncrementIteration()
			}
			s.SetK8sInfo(tt.k8sInfo)

			output := node.fallbackDecision(s)

			if output.Decision != tt.expectedDecision {
				t.Errorf("expected decision %q, got %q", tt.expectedDecision, output.Decision)
			}
			if len(output.ToolCalls) != tt.expectedToolCalls {
				t.Errorf("expected %d tool calls, got %d", tt.expectedToolCalls, len(output.ToolCalls))
			}
		})
	}
}

func TestDecisionNodeMaxIterations(t *testing.T) {
	node := &DecisionNode{router: nil}
	s := state.NewState("test query", 3, 4)

	// 设置迭代次数达到最大值
	s.IncrementIteration()
	s.IncrementIteration()
	s.IncrementIteration() // 现在 IterationCount = 3, MaxIterations = 3

	output, err := node.Execute(context.Background(), s)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if output.Decision != "report" {
		t.Errorf("expected decision 'report' when max iterations reached, got %q", output.Decision)
	}
}

func TestDecisionNodeCacheHitEarlyTermination(t *testing.T) {
	node := &DecisionNode{router: nil}

	// 场景1：连续 2 轮全部缓存命中 → 应强制 report
	s := state.NewState("test query", 10, 4)
	s.IncrementIteration() // iter=1
	s.IncrementIteration() // iter=2
	s.IncrementIteration() // iter=3

	// 检查的是 IterationCount-1=2 和 IterationCount-2=1
	s.RecordCacheHit(1)
	s.RecordCacheHit(1)
	s.RecordCacheHit(2)
	s.RecordCacheHit(2)

	// 设置 K8sInfo 以避免 fallback 干扰
	s.SetK8sInfo(&state.K8sInfo{
		Namespaces: []string{"default"},
		Resources:  map[string][]interface{}{"Pods": {}},
	})

	output, err := node.Execute(context.Background(), s)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if output.Decision != "report" {
		t.Errorf("expected 'report' for consecutive cache hits, got %q", output.Decision)
	}
}

func TestDecisionNodeCacheHitNotTriggered(t *testing.T) {
	node := &DecisionNode{router: nil}

	// 场景2：有 cache miss → 不应提前终止
	// 设置 iterCount=maxIter 使其走 max iterations 终止而非 LLM 调用
	s := state.NewState("test query", 3, 4)
	s.IncrementIteration() // iter=1
	s.IncrementIteration() // iter=2
	s.IncrementIteration() // iter=3 = max

	// 迭代 1 有 miss（不满足全命中）
	s.RecordCacheHit(1)
	s.RecordCacheMiss(1)
	// 迭代 2 全命中
	s.RecordCacheHit(2)

	s.SetK8sInfo(&state.K8sInfo{
		Namespaces: []string{"default"},
		Resources: map[string][]interface{}{
			"Pods": {
				state.PodInfo{Name: "bad", Namespace: "default", Status: "CrashLoopBackOff"},
			},
		},
	})

	output, err := node.Execute(context.Background(), s)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	// 应因 max iterations 而终止，而不是因缓存命中
	if output.Thought == "连续 2 轮工具调用全部命中缓存，没有新信息，基于已有数据生成报告" {
		t.Error("should not trigger cache hit early termination when there's a cache miss")
	}
	if output.Decision != "report" {
		t.Errorf("expected 'report' due to max iterations, got %q", output.Decision)
	}
}
