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
			iterationCount: 5,
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
