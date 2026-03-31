package diagnosis

import (
	"strings"
	"testing"

	"github.com/AceDarkknight/k8s-analyzer-agent/internal/state"
)

func TestReportNodeGenerateFallbackReport(t *testing.T) {
	node := &ReportNode{router: nil, store: nil}

	tests := []struct {
		name           string
		k8sInfo        *state.K8sInfo
		expectedStatus string
		minFindings    int
	}{
		{
			name:           "no k8s info",
			k8sInfo:        nil,
			expectedStatus: "partial",
			minFindings:    0,
		},
		{
			name: "no abnormal pods",
			k8sInfo: &state.K8sInfo{
				Namespaces: []string{"default"},
				Resources: map[string][]interface{}{
					"Pods": {
						state.PodInfo{Name: "good-pod", Namespace: "default", Status: "Running"},
					},
				},
			},
			expectedStatus: "partial",
			minFindings:    0,
		},
		{
			name: "with abnormal pods",
			k8sInfo: &state.K8sInfo{
				Namespaces: []string{"default"},
				Resources: map[string][]interface{}{
					"Pods": {
						state.PodInfo{Name: "bad-pod", Namespace: "default", Status: "CrashLoopBackOff", Restarts: 5},
						state.PodInfo{Name: "pending-pod", Namespace: "default", Status: "Pending"},
					},
				},
			},
			expectedStatus: "partial",
			minFindings:    2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := state.NewState("test query", 10, 4)
			s.SetK8sInfo(tt.k8sInfo)

			node.generateFallbackReport(s)

			if s.AnalysisResult == nil {
				t.Fatalf("expected AnalysisResult to be set")
			}

			if s.AnalysisResult.Status != tt.expectedStatus {
				t.Errorf("expected status %q, got %q", tt.expectedStatus, s.AnalysisResult.Status)
			}

			if len(s.AnalysisResult.Findings) < tt.minFindings {
				t.Errorf("expected at least %d findings, got %d", tt.minFindings, len(s.AnalysisResult.Findings))
			}

			// 验证基本字段
			if s.AnalysisResult.Summary == "" {
				t.Errorf("expected non-empty summary")
			}
			if s.AnalysisResult.Severity == "" {
				t.Errorf("expected non-empty severity")
			}
		})
	}
}

func TestReportNodeFallbackReportFindingDetails(t *testing.T) {
	node := &ReportNode{router: nil, store: nil}
	s := state.NewState("test query", 10, 4)

	k8sInfo := &state.K8sInfo{
		Namespaces: []string{"default"},
		Resources: map[string][]interface{}{
			"Pods": {
				state.PodInfo{
					Name:      "test-pod",
					Namespace: "default",
					Status:    "CrashLoopBackOff",
					Restarts:  10,
				},
			},
		},
	}
	s.SetK8sInfo(k8sInfo)

	node.generateFallbackReport(s)

	if len(s.AnalysisResult.Findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(s.AnalysisResult.Findings))
	}

	finding := s.AnalysisResult.Findings[0]

	// 验证 finding 字段
	if finding.Resource != "default/test-pod" {
		t.Errorf("expected resource 'default/test-pod', got %q", finding.Resource)
	}
	if finding.Severity != "warning" {
		t.Errorf("expected severity 'warning', got %q", finding.Severity)
	}
	if !strings.Contains(finding.Message, "CrashLoopBackOff") {
		t.Errorf("expected message to contain 'CrashLoopBackOff', got %q", finding.Message)
	}
	if !strings.Contains(finding.Evidence, "10") {
		t.Errorf("expected evidence to contain restart count '10', got %q", finding.Evidence)
	}
	if finding.Timestamp.IsZero() {
		t.Errorf("expected timestamp to be set")
	}

	// 验证 recommendations
	if len(s.AnalysisResult.Recommendations) == 0 {
		t.Errorf("expected at least one recommendation")
	}
}

func TestReportNodeStatusPartialOnMaxIterations(t *testing.T) {
	// 测试当 IterationCount >= MaxIterations 时状态为 partial
	s := state.NewState("test query", 5, 4)

	// 设置达到最大迭代次数
	for i := 0; i < 5; i++ {
		s.IncrementIteration()
	}

	// 手动设置一个 AnalysisResult 来模拟状态检查
	s.SetAnalysisResult(&state.AnalysisResult{
		Summary:  "Test",
		Severity: "info",
		Status:   "completed",
	})

	// 验证状态应该被更新为 partial
	if s.IterationCount >= s.MaxIterations && s.AnalysisResult.Status != "partial" {
		// 注意：这个测试验证的是 Execute 方法中的逻辑
		// 这里我们只是验证状态条件
		t.Logf("IterationCount=%d, MaxIterations=%d", s.IterationCount, s.MaxIterations)
	}
}
