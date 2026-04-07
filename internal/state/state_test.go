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

	s.AddCommandExecution("kubectl get pods", true, "pod1\npod2", false)

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
	s.AddCommandExecution("", false, "", false)
	s.AddBlockedCommand(BlockedCommand{})
	s.IncrementIteration()

	if s.ShouldContinue() {
		t.Error("ShouldContinue should return false for nil state")
	}

	if s.GetRecentSteps(5) != nil {
		t.Error("GetRecentSteps should return nil for nil state")
	}
}

// TestRecordCacheHitAndMiss 测试缓存命中/未命中记录
func TestRecordCacheHitAndMiss(t *testing.T) {
	s := NewState("test", 10, 4)

	// 初始状态无统计
	stats := s.GetRoundCacheStats(1)
	if stats != nil {
		t.Error("expected nil stats for unrecorded iteration")
	}

	// 记录迭代 1 的命中和未命中
	s.RecordCacheHit(1)
	s.RecordCacheHit(1)
	s.RecordCacheMiss(1)

	stats = s.GetRoundCacheStats(1)
	if stats == nil {
		t.Fatal("expected non-nil stats for iteration 1")
	}
	if stats.TotalCalls != 3 {
		t.Errorf("expected TotalCalls=3, got %d", stats.TotalCalls)
	}
	if stats.CacheHits != 2 {
		t.Errorf("expected CacheHits=2, got %d", stats.CacheHits)
	}

	// 迭代 2 应该独立
	s.RecordCacheMiss(2)
	stats2 := s.GetRoundCacheStats(2)
	if stats2 == nil {
		t.Fatal("expected non-nil stats for iteration 2")
	}
	if stats2.TotalCalls != 1 {
		t.Errorf("expected TotalCalls=1 for iter 2, got %d", stats2.TotalCalls)
	}
	if stats2.CacheHits != 0 {
		t.Errorf("expected CacheHits=0 for iter 2, got %d", stats2.CacheHits)
	}

	// 迭代 1 不受影响
	stats1 := s.GetRoundCacheStats(1)
	if stats1.TotalCalls != 3 {
		t.Errorf("iter 1 stats changed unexpectedly: TotalCalls=%d", stats1.TotalCalls)
	}
}

// TestRecordCacheWithNilState 测试 nil State 不 panic
func TestRecordCacheWithNilState(t *testing.T) {
	var s *State
	// 不应 panic
	s.RecordCacheHit(1)
	s.RecordCacheMiss(1)
	if s.GetRoundCacheStats(1) != nil {
		t.Error("expected nil from nil state")
	}
}

// TestCacheStatsAllHit 测试全部命中场景
func TestCacheStatsAllHit(t *testing.T) {
	s := NewState("test", 10, 4)

	// 全部命中
	s.RecordCacheHit(1)
	s.RecordCacheHit(1)
	s.RecordCacheHit(1)

	stats := s.GetRoundCacheStats(1)
	if stats.TotalCalls != 3 || stats.CacheHits != 3 {
		t.Errorf("expected all hits: Total=%d, Hits=%d", stats.TotalCalls, stats.CacheHits)
	}
}
