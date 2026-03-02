// Package analysis provides tests for ReportNode
package analysis

import (
	"context"
	"testing"
	"time"

	"github.com/AceDarkknight/k8s-analyzer-agent/internal/client"
)

// MockStore mocks FindingStore
type MockStore struct {
	findings map[string]bool
}

func NewMockStore() *MockStore {
	return &MockStore{
		findings: make(map[string]bool),
	}
}

func (m *MockStore) HasFinding(ctx context.Context, key string) (bool, error) {
	_, exists := m.findings[key]
	return exists, nil
}

func (m *MockStore) SaveFinding(ctx context.Context, key string, ttl time.Duration) error {
	m.findings[key] = true
	return nil
}

func (m *MockStore) DeleteFinding(ctx context.Context, key string) error {
	delete(m.findings, key)
	return nil
}

func (m *MockStore) Close() error {
	return nil
}

// MockAnalysisLLM mocks LLM for analysis
type MockAnalysisLLM struct {
	called bool
}

func NewMockAnalysisLLM() *MockAnalysisLLM {
	return &MockAnalysisLLM{}
}

// MakeDecision 模拟决策 - 返回 DecisionResult
func (m *MockAnalysisLLM) MakeDecision(ctx context.Context, state *State) (*DecisionResult, error) {
	return &DecisionResult{
		Decision:  DecisionReport,
		Reasoning: "Mock decision for testing",
		ToolCalls: nil,
	}, nil
}

func (m *MockAnalysisLLM) Analyze(ctx context.Context, state *State) (string, error) {
	return "analysis result", nil
}

func (m *MockAnalysisLLM) GenerateReport(ctx context.Context, state *State) (string, error) {
	return "report", nil
}

func (m *MockAnalysisLLM) SetTools(tools []client.Tool) {
	// no-op
}

func (m *MockAnalysisLLM) AnalyzeError(ctx context.Context, errorContext ErrorContext) (AnalysisResult, error) {
	m.called = true
	return AnalysisResult{
		Findings: []Finding{
			{
				Severity: "Critical",
				Resource: errorContext.PodName,
				Message:  "LLM Analysis Result",
			},
		},
		Recommendations: []Recommendation{
			{
				Action:   "Check logs",
				Reason:   "Error detected",
				Priority: "High",
				Command:  "kubectl logs",
			},
		},
	}, nil
}

// SynthesizeReport 模拟生成综合报告
func (m *MockAnalysisLLM) SynthesizeReport(ctx context.Context, userInput string, findings []Finding, commands []CommandExecution, k8sSummary string) (string, error) {
	return "Synthesized report from mock LLM", nil
}

func TestReportNode_Execute_NewFinding(t *testing.T) {
	store := NewMockStore()
	llm := NewMockAnalysisLLM()
	node := NewReportNode(store, llm)

	state := NewState("test query")
	state.K8sInfo.Namespace = "default"
	// 使用 SetResources 设置 Pods
	state.K8sInfo.SetResources("Pods", PodInfo{
		Name:      "error-pod",
		Namespace: "default",
		Status:    "Error",
	})

	_, err := node.Execute(context.Background(), state)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Verify LLM called
	if !llm.called {
		t.Error("Expected LLM analysis to be called for new finding")
	}

	// Verify Store updated
	has, _ := store.HasFinding(context.Background(), "finding:default:error-pod:Error")
	if !has {
		t.Error("Expected finding to be saved to store")
	}

	// Verify Report contains finding
	if len(state.AnalysisResult.Findings) != 1 {
		t.Errorf("Expected 1 finding, got %d", len(state.AnalysisResult.Findings))
	} else if state.AnalysisResult.Findings[0].Message != "LLM Analysis Result" {
		t.Errorf("Expected LLM finding message, got %s", state.AnalysisResult.Findings[0].Message)
	}
}

func TestReportNode_Execute_DuplicateFinding(t *testing.T) {
	store := NewMockStore()
	// Pre-populate store
	store.SaveFinding(context.Background(), "finding:default:error-pod:Error", time.Hour)

	llm := NewMockAnalysisLLM()
	node := NewReportNode(store, llm)

	state := NewState("test query")
	state.K8sInfo.Namespace = "default"
	// 使用 SetResources 设置 Pods
	state.K8sInfo.SetResources("Pods", PodInfo{
		Name:      "error-pod",
		Namespace: "default",
		Status:    "Error",
	})

	_, err := node.Execute(context.Background(), state)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Verify LLM NOT called
	if llm.called {
		t.Error("Expected LLM analysis NOT to be called for duplicate finding")
	}

	// Verify Report is empty (skipped)
	if len(state.AnalysisResult.Findings) != 0 {
		t.Errorf("Expected 0 findings, got %d", len(state.AnalysisResult.Findings))
	}
}
