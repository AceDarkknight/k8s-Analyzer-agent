package state

import (
	"strings"
	"testing"
	"time"
)

// TestNewStateDefaults 测试 NewState 默认值
func TestNewStateDefaults(t *testing.T) {
	// 测试自定义值
	s := NewState("test input", 5, 3)
	if s.UserInput != "test input" {
		t.Errorf("expected UserInput to be 'test input', got %s", s.UserInput)
	}
	if s.MaxIterations != 5 {
		t.Errorf("expected MaxIterations to be 5, got %d", s.MaxIterations)
	}
	if s.CompressThreshold != 3 {
		t.Errorf("expected CompressThreshold to be 3, got %d", s.CompressThreshold)
	}
	if s.IterationCount != 0 {
		t.Errorf("expected IterationCount to be 0, got %d", s.IterationCount)
	}

	// 测试默认值
	s2 := NewState("test", 0, 0)
	if s2.MaxIterations != 10 {
		t.Errorf("expected default MaxIterations to be 10, got %d", s2.MaxIterations)
	}
	if s2.CompressThreshold != 4 {
		t.Errorf("expected default CompressThreshold to be 4, got %d", s2.CompressThreshold)
	}
}

// TestAddReasoningStep 测试 AddReasoningStep
func TestAddReasoningStep(t *testing.T) {
	s := NewState("test", 10, 4)

	step1 := ReasoningStep{
		Iteration: 0,
		Thought:   "First thought",
		Decision:  "continue",
	}
	s.AddReasoningStep(step1)

	if len(s.ReasoningHistory) != 1 {
		t.Errorf("expected 1 reasoning step, got %d", len(s.ReasoningHistory))
	}

	step2 := ReasoningStep{
		Iteration: 1,
		Thought:   "Second thought",
		Decision:  "deep_query",
	}
	s.AddReasoningStep(step2)

	if len(s.ReasoningHistory) != 2 {
		t.Errorf("expected 2 reasoning steps, got %d", len(s.ReasoningHistory))
	}

	if s.ReasoningHistory[0].Thought != "First thought" {
		t.Errorf("expected first thought to be 'First thought', got %s", s.ReasoningHistory[0].Thought)
	}

	if s.ReasoningHistory[1].Decision != "deep_query" {
		t.Errorf("expected second decision to be 'deep_query', got %s", s.ReasoningHistory[1].Decision)
	}
}

// TestShouldContinue 测试 ShouldContinue
func TestShouldContinue(t *testing.T) {
	// 正常情况：未达最大迭代次数且没有结果
	s := NewState("test", 3, 4)
	if !s.ShouldContinue() {
		t.Error("expected ShouldContinue to return true for new state")
	}

	// 达到 MaxIterations
	s.IncrementIteration()
	s.IncrementIteration()
	s.IncrementIteration()
	if s.ShouldContinue() {
		t.Error("expected ShouldContinue to return false when MaxIterations reached")
	}

	// 已有 AnalysisResult
	s2 := NewState("test", 10, 4)
	s2.AnalysisResult = &AnalysisResult{
		Summary:  "Test result",
		Severity: "info",
	}
	if s2.ShouldContinue() {
		t.Error("expected ShouldContinue to return false when AnalysisResult exists")
	}
}

// TestGetRecentSteps 测试 GetRecentSteps
func TestGetRecentSteps(t *testing.T) {
	s := NewState("test", 10, 4)

	// 添加 5 个步骤
	for i := 0; i < 5; i++ {
		s.AddReasoningStep(ReasoningStep{
			Iteration: i,
			Thought:   "Thought " + string(rune('A'+i)),
		})
	}

	// 获取最近 3 步
	recent := s.GetRecentSteps(3)
	if len(recent) != 3 {
		t.Errorf("expected 3 recent steps, got %d", len(recent))
	}

	// 验证获取的是最后 3 个
	if recent[0].Iteration != 2 {
		t.Errorf("expected first recent step iteration to be 2, got %d", recent[0].Iteration)
	}
	if recent[2].Iteration != 4 {
		t.Errorf("expected last recent step iteration to be 4, got %d", recent[2].Iteration)
	}

	// 获取超过总数的步骤
	all := s.GetRecentSteps(10)
	if len(all) != 5 {
		t.Errorf("expected 5 steps when requesting more than available, got %d", len(all))
	}

	// 获取 0 步
	none := s.GetRecentSteps(0)
	if none != nil {
		t.Error("expected nil when requesting 0 steps")
	}
}

// TestK8sInfoGetSummary 测试 K8sInfo.GetSummary
func TestK8sInfoGetSummary(t *testing.T) {
	// 测试 nil
	var nilInfo *K8sInfo
	if nilInfo.GetSummary() != "K8sInfo: nil" {
		t.Errorf("expected 'K8sInfo: nil' for nil, got %s", nilInfo.GetSummary())
	}

	// 测试空 K8sInfo
	k := &K8sInfo{
		Namespaces: []string{},
		Resources:  make(map[string][]interface{}),
	}
	summary := k.GetSummary()
	if !strings.Contains(summary, "命名空间: []") {
		t.Errorf("expected summary to contain empty namespaces, got %s", summary)
	}
	if !strings.Contains(summary, "Pods: 0 个 (0 个异常)") {
		t.Errorf("expected summary to show 0 pods, got %s", summary)
	}

	// 测试有数据的 K8sInfo
	k2 := &K8sInfo{
		Namespaces: []string{"default", "kube-system"},
		Resources: map[string][]interface{}{
			"Pods": {
				PodInfo{Name: "pod1", Status: "Running"},
				PodInfo{Name: "pod2", Status: "Pending"},
				PodInfo{Name: "pod3", Status: "Failed"},
			},
			"Deployments": {
				DeploymentInfo{Name: "deploy1"},
				DeploymentInfo{Name: "deploy2"},
			},
			"Services": {
				ServiceInfo{Name: "svc1"},
			},
		},
	}
	summary2 := k2.GetSummary()
	if !strings.Contains(summary2, "命名空间: [default, kube-system]") {
		t.Errorf("expected summary to contain namespaces, got %s", summary2)
	}
	if !strings.Contains(summary2, "Pods: 3 个 (2 个异常)") {
		t.Errorf("expected summary to show 3 pods with 2 abnormal, got %s", summary2)
	}
	if !strings.Contains(summary2, "Deployments: 2 个") {
		t.Errorf("expected summary to show 2 deployments, got %s", summary2)
	}
	if !strings.Contains(summary2, "Services: 1 个") {
		t.Errorf("expected summary to show 1 service, got %s", summary2)
	}
}

// TestK8sInfoGetAbnormalPods 测试 K8sInfo.GetAbnormalPods
func TestK8sInfoGetAbnormalPods(t *testing.T) {
	// 测试 nil
	var nilInfo *K8sInfo
	if nilInfo.GetAbnormalPods() != nil {
		t.Error("expected nil for nil K8sInfo")
	}

	// 测试 nil Resources
	k := &K8sInfo{Resources: nil}
	if k.GetAbnormalPods() != nil {
		t.Error("expected nil for nil Resources")
	}

	// 测试没有 Pods
	k2 := &K8sInfo{
		Resources: map[string][]interface{}{
			"Deployments": {DeploymentInfo{Name: "deploy1"}},
		},
	}
	if k2.GetAbnormalPods() != nil {
		t.Error("expected nil when no pods in resources")
	}

	// 测试有正常和异常 Pod
	k3 := &K8sInfo{
		Resources: map[string][]interface{}{
			"Pods": {
				PodInfo{Name: "pod1", Status: "Running"},
				PodInfo{Name: "pod2", Status: "Succeeded"},
				PodInfo{Name: "pod3", Status: "Pending"},
				PodInfo{Name: "pod4", Status: "Failed"},
				PodInfo{Name: "pod5", Status: "CrashLoopBackOff"},
				PodInfo{Name: "pod6", Status: "Unknown"},
			},
		},
	}
	abnormal := k3.GetAbnormalPods()
	if len(abnormal) != 4 {
		t.Errorf("expected 4 abnormal pods, got %d", len(abnormal))
	}

	// 验证异常 Pod 名称
	expectedAbnormal := map[string]bool{"pod3": true, "pod4": true, "pod5": true, "pod6": true}
	for _, pod := range abnormal {
		if !expectedAbnormal[pod.Name] {
			t.Errorf("unexpected abnormal pod: %s", pod.Name)
		}
	}
}

// TestAddFinding 测试 AddFinding
func TestAddFinding(t *testing.T) {
	s := NewState("test", 10, 4)
	s.AnalysisResult = &AnalysisResult{
		Findings: make([]Finding, 0),
	}

	finding := Finding{
		Severity:  "critical",
		Resource:  "pod/test",
		Message:   "Pod is crashing",
		Timestamp: time.Now(),
	}
	s.AddFinding(finding)

	if len(s.AnalysisResult.Findings) != 1 {
		t.Errorf("expected 1 finding, got %d", len(s.AnalysisResult.Findings))
	}

	if s.AnalysisResult.Findings[0].Severity != "critical" {
		t.Errorf("expected finding severity to be 'critical', got %s", s.AnalysisResult.Findings[0].Severity)
	}
}

// TestAddCommandExecution 测试 AddCommandExecution
func TestAddCommandExecution(t *testing.T) {
	s := NewState("test", 10, 4)

	exec := CommandExecution{
		Command:   "kubectl get pods",
		Success:   true,
		Output:    "pod1\npod2",
		Timestamp: time.Now(),
	}
	s.AddCommandExecution(exec)

	if len(s.CommandExecutions) != 1 {
		t.Errorf("expected 1 command execution, got %d", len(s.CommandExecutions))
	}

	if s.CommandExecutions[0].Command != "kubectl get pods" {
		t.Errorf("expected command to be 'kubectl get pods', got %s", s.CommandExecutions[0].Command)
	}
}

// TestAddBlockedCommand 测试 AddBlockedCommand
func TestAddBlockedCommand(t *testing.T) {
	s := NewState("test", 10, 4)

	blocked := BlockedCommand{
		Command: "rm -rf /",
		Reason:  "Dangerous command",
		Advice:  "Use safer alternatives",
	}
	s.AddBlockedCommand(blocked)

	if len(s.BlockedCommands) != 1 {
		t.Errorf("expected 1 blocked command, got %d", len(s.BlockedCommands))
	}

	if s.BlockedCommands[0].Command != "rm -rf /" {
		t.Errorf("expected blocked command to be 'rm -rf /', got %s", s.BlockedCommands[0].Command)
	}
}

// TestIncrementIteration 测试 IncrementIteration
func TestIncrementIteration(t *testing.T) {
	s := NewState("test", 10, 4)

	if s.IterationCount != 0 {
		t.Errorf("expected initial IterationCount to be 0, got %d", s.IterationCount)
	}

	s.IncrementIteration()
	if s.IterationCount != 1 {
		t.Errorf("expected IterationCount to be 1, got %d", s.IterationCount)
	}

	s.IncrementIteration()
	s.IncrementIteration()
	if s.IterationCount != 3 {
		t.Errorf("expected IterationCount to be 3, got %d", s.IterationCount)
	}
}

// TestStateMethodsWithNil 测试 nil State 的方法不会 panic
func TestStateMethodsWithNil(t *testing.T) {
	var s *State

	// 这些方法不应该 panic
	s.AddReasoningStep(ReasoningStep{})
	s.AddFinding(Finding{})
	s.AddCommandExecution(CommandExecution{})
	s.AddBlockedCommand(BlockedCommand{})
	s.IncrementIteration()

	if s.ShouldContinue() {
		t.Error("ShouldContinue should return false for nil state")
	}

	if s.GetRecentSteps(5) != nil {
		t.Error("GetRecentSteps should return nil for nil state")
	}
}

// TestHasExecutableRecommendations 测试 HasExecutableRecommendations
func TestHasExecutableRecommendations(t *testing.T) {
	// 场景1: AnalysisResult 为 nil → 返回 false
	s1 := NewState("test", 10, 4)
	if s1.HasExecutableRecommendations() {
		t.Error("expected HasExecutableRecommendations to return false when AnalysisResult is nil")
	}

	// 场景2: Recommendations 为空 → 返回 false
	s2 := NewState("test", 10, 4)
	s2.AnalysisResult = &AnalysisResult{
		Recommendations: []Recommendation{},
	}
	if s2.HasExecutableRecommendations() {
		t.Error("expected HasExecutableRecommendations to return false when Recommendations is empty")
	}

	// 场景3: 所有 Recommendations 的 Executable=false → 返回 false
	s3 := NewState("test", 10, 4)
	s3.AnalysisResult = &AnalysisResult{
		Recommendations: []Recommendation{
			{Priority: "high", Action: "test", Executable: false, Verified: false},
			{Priority: "medium", Action: "test2", Executable: false, Verified: false},
		},
	}
	if s3.HasExecutableRecommendations() {
		t.Error("expected HasExecutableRecommendations to return false when all recommendations are not executable")
	}

	// 场景4: 有 Executable=true 且 Verified=false → 返回 true
	s4 := NewState("test", 10, 4)
	s4.AnalysisResult = &AnalysisResult{
		Recommendations: []Recommendation{
			{Priority: "high", Action: "test", Executable: false, Verified: false},
			{Priority: "high", Action: "verify this", Executable: true, Verified: false},
		},
	}
	if !s4.HasExecutableRecommendations() {
		t.Error("expected HasExecutableRecommendations to return true when there are unverified executable recommendations")
	}

	// 场景5: 有 Executable=true 但 Verified=true → 返回 false（已验证过的不算）
	s5 := NewState("test", 10, 4)
	s5.AnalysisResult = &AnalysisResult{
		Recommendations: []Recommendation{
			{Priority: "high", Action: "test", Executable: false, Verified: false},
			{Priority: "high", Action: "already verified", Executable: true, Verified: true},
		},
	}
	if s5.HasExecutableRecommendations() {
		t.Error("expected HasExecutableRecommendations to return false when all executable recommendations are already verified")
	}

	// 场景6: 混合情况 - 部分已验证，部分未验证 → 返回 true
	s6 := NewState("test", 10, 4)
	s6.AnalysisResult = &AnalysisResult{
		Recommendations: []Recommendation{
			{Priority: "high", Action: "already verified", Executable: true, Verified: true},
			{Priority: "high", Action: "not verified yet", Executable: true, Verified: false},
		},
	}
	if !s6.HasExecutableRecommendations() {
		t.Error("expected HasExecutableRecommendations to return true when there are some unverified executable recommendations")
	}
}
