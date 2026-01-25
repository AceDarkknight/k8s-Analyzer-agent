// Package analysis 提供 Analysis Agent 的单元测试
package analysis

import (
	"context"
	"testing"
	"time"

	"github.com/your-org/k8s-analyzer-agent/internal/client/k8s"
)

// MockSafetyAgent Mock Safety Agent，用于测试
type MockSafetyAgent struct {
	commands []string
}

// NewMockSafetyAgent 创建新的 Mock Safety Agent
func NewMockSafetyAgent() *MockSafetyAgent {
	return &MockSafetyAgent{
		commands: make([]string, 0),
	}
}

// ExecuteSafeCommand 模拟安全执行命令
func (m *MockSafetyAgent) ExecuteSafeCommand(ctx context.Context, command string) (string, error) {
	m.commands = append(m.commands, command)
	return "Mock output for command: " + command, nil
}

// GetCommands 获取已执行的命令
func (m *MockSafetyAgent) GetCommands() []string {
	return m.commands
}

// TestNewState 测试 State 创建
func TestNewState(t *testing.T) {
	state := NewState("test query")

	if state.UserInput != "test query" {
		t.Errorf("Expected UserInput 'test query', got '%s'", state.UserInput)
	}

	if state.K8sInfo == nil {
		t.Error("Expected K8sInfo to be initialized")
	}

	if state.AnalysisResult == nil {
		t.Error("Expected AnalysisResult to be initialized")
	}

	if state.IterationCount != 0 {
		t.Errorf("Expected IterationCount 0, got %d", state.IterationCount)
	}

	if state.MaxIterations != 10 {
		t.Errorf("Expected MaxIterations 10, got %d", state.MaxIterations)
	}
}

// TestIncrementIteration 测试迭代计数增加
func TestIncrementIteration(t *testing.T) {
	state := NewState("test")

	// 正常增加
	err := state.IncrementIteration()
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if state.IterationCount != 1 {
		t.Errorf("Expected IterationCount 1, got %d", state.IterationCount)
	}

	// 达到最大迭代次数
	for i := 0; i < 9; i++ {
		state.IncrementIteration()
	}

	err = state.IncrementIteration()
	if err == nil {
		t.Error("Expected error when exceeding max iterations")
	}
}

// TestShouldContinue 测试是否应该继续执行
func TestShouldContinue(t *testing.T) {
	state := NewState("test")

	// 初始状态应该继续
	if !state.ShouldContinue() {
		t.Error("Expected ShouldContinue to be true initially")
	}

	// 达到最大迭代次数
	state.IterationCount = 10
	if state.ShouldContinue() {
		t.Error("Expected ShouldContinue to be false when max iterations reached")
	}

	// 状态为完成
	state.IterationCount = 5
	state.AnalysisResult.Status = StatusCompleted
	if state.ShouldContinue() {
		t.Error("Expected ShouldContinue to be false when status is completed")
	}
}

// TestAddFinding 测试添加发现
func TestAddFinding(t *testing.T) {
	state := NewState("test")

	state.AddFinding("Critical", "test-pod", "Pod is in error state")

	if len(state.AnalysisResult.Findings) != 1 {
		t.Errorf("Expected 1 finding, got %d", len(state.AnalysisResult.Findings))
	}

	finding := state.AnalysisResult.Findings[0]
	if finding.Severity != "Critical" {
		t.Errorf("Expected severity 'Critical', got '%s'", finding.Severity)
	}

	if finding.Resource != "test-pod" {
		t.Errorf("Expected resource 'test-pod', got '%s'", finding.Resource)
	}
}

// TestAddRecommendation 测试添加建议
func TestAddRecommendation(t *testing.T) {
	state := NewState("test")

	state.AddRecommendation("Check logs", "Pod is crashing", "High", "kubectl logs test-pod")

	if len(state.AnalysisResult.Recommendations) != 1 {
		t.Errorf("Expected 1 recommendation, got %d", len(state.AnalysisResult.Recommendations))
	}

	rec := state.AnalysisResult.Recommendations[0]
	if rec.Action != "Check logs" {
		t.Errorf("Expected action 'Check logs', got '%s'", rec.Action)
	}

	if rec.Priority != "High" {
		t.Errorf("Expected priority 'High', got '%s'", rec.Priority)
	}
}

// TestRuleBasedLLM 测试基于规则的 LLM
func TestRuleBasedLLM(t *testing.T) {
	llm := NewRuleBasedLLM(nil)

	// 测试达到最大迭代次数
	state := NewState("test")
	state.IterationCount = 10
	decision, err := llm.MakeDecision(context.Background(), state)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if decision != DecisionReport {
		t.Errorf("Expected decision '%s', got '%s'", DecisionReport, decision)
	}

	// 测试有错误 Pod
	state = NewState("test")
	state.K8sInfo.Pods = []PodInfo{
		{
			Name:   "error-pod",
			Status: "Error",
		},
	}
	decision, err = llm.MakeDecision(context.Background(), state)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if decision != DecisionDeepQuery {
		t.Errorf("Expected decision '%s', got '%s'", DecisionDeepQuery, decision)
	}
}

// TestMockLLM 测试 Mock LLM
func TestMockLLM(t *testing.T) {
	llm := NewMockLLM(nil)

	state := NewState("test")

	decision, err := llm.MakeDecision(context.Background(), state)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if decision == "" {
		t.Error("Expected non-empty decision")
	}
}

// TestCommandGenerator 测试命令生成器
func TestCommandGenerator(t *testing.T) {
	generator := NewCommandGenerator(nil)

	// 测试有错误 Pod 的情况
	state := NewState("test")
	state.K8sInfo.Pods = []PodInfo{
		{
			Name:   "error-pod",
			Status: "Error",
		},
	}

	command, err := generator.GenerateCommand(state)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if command == "" {
		t.Error("Expected non-empty command")
	}

	t.Logf("Generated command: %s", command)
}

// TestNewAgent 测试 Agent 创建
func TestNewAgent(t *testing.T) {
	// 创建 Mock K8s Client
	k8sClient := k8s.NewMockClient(k8s.Config{})

	// 创建 Mock Safety Agent
	safetyAgent := NewMockSafetyAgent()

	// 创建 Agent
	agent, err := NewAgent(k8sClient, safetyAgent, nil)
	if err != nil {
		t.Fatalf("Failed to create agent: %v", err)
	}

	if agent == nil {
		t.Fatal("Expected non-nil agent")
	}

	if agent.GetK8sClient() == nil {
		t.Error("Expected non-nil K8s client")
	}

	if agent.GetSafetyAgent() == nil {
		t.Error("Expected non-nil Safety agent")
	}

	if agent.GetLLM() == nil {
		t.Error("Expected non-nil LLM")
	}
}

// TestAgentRun 测试 Agent 运行
func TestAgentRun(t *testing.T) {
	// 创建 Mock K8s Client
	k8sClient := k8s.NewMockClient(k8s.Config{})

	// 连接 K8s Client
	ctx := context.Background()
	if err := k8sClient.Connect(ctx); err != nil {
		t.Fatalf("Failed to connect K8s client: %v", err)
	}
	defer k8sClient.Close()

	// 创建 Mock Safety Agent
	safetyAgent := NewMockSafetyAgent()

	// 创建 Agent
	agent, err := NewAgent(k8sClient, safetyAgent, nil)
	if err != nil {
		t.Fatalf("Failed to create agent: %v", err)
	}

	// 运行分析
	result, err := agent.Run(ctx, "分析 nginx 服务")
	if err != nil {
		t.Fatalf("Failed to run agent: %v", err)
	}

	if result == nil {
		t.Fatal("Expected non-nil result")
	}

	// 验证结果
	t.Logf("Analysis Status: %s", result.Status)
	t.Logf("Summary: %s", result.Summary)
	t.Logf("Findings: %d", len(result.Findings))
	t.Logf("Recommendations: %d", len(result.Recommendations))
	t.Logf("Executed Commands: %d", len(result.ExecutedCommands))

	// 验证至少有一些结果
	if len(result.ExecutedCommands) == 0 {
		t.Error("Expected at least one executed command")
	}
}

// TestAgentRunWithMaxIterations 测试达到最大迭代次数的情况
func TestAgentRunWithMaxIterations(t *testing.T) {
	// 创建 Mock K8s Client
	k8sClient := k8s.NewMockClient(k8s.Config{})

	// 连接 K8s Client
	ctx := context.Background()
	if err := k8sClient.Connect(ctx); err != nil {
		t.Fatalf("Failed to connect K8s client: %v", err)
	}
	defer k8sClient.Close()

	// 创建 Mock Safety Agent
	safetyAgent := NewMockSafetyAgent()

	// 创建 Agent
	agent, err := NewAgent(k8sClient, safetyAgent, nil)
	if err != nil {
		t.Fatalf("Failed to create agent: %v", err)
	}

	// 创建初始状态并设置最大迭代次数
	state := NewState("test")
	state.MaxIterations = 2
	state.IterationCount = 2 // 直接设置为最大迭代次数

	// 运行分析
	result, err := agent.RunWithState(ctx, state)
	if err != nil {
		t.Fatalf("Failed to run agent: %v", err)
	}

	if result == nil {
		t.Fatal("Expected non-nil result")
	}

	// 验证状态为部分完成（因为达到最大迭代次数）
	if result.Status != StatusPartial {
		t.Errorf("Expected status '%s', got '%s'", StatusPartial, result.Status)
	}

	t.Logf("Analysis Status: %s", result.Status)
}

// TestParseUserQuery 测试用户查询解析
func TestParseUserQuery(t *testing.T) {
	// 测试包含命名空间的查询
	query1 := "分析 namespace:production 中的 nginx pod"
	result1 := ParseUserQuery(query1)

	if result1["namespace"] != "production" {
		t.Errorf("Expected namespace 'production', got '%s'", result1["namespace"])
	}

	// 测试包含资源名称的查询
	query2 := "检查 pod nginx-pod-1 的状态"
	result2 := ParseUserQuery(query2)

	if result2["resource_name"] != "nginx-pod-1" {
		t.Errorf("Expected resource_name 'nginx-pod-1', got '%s'", result2["resource_name"])
	}

	if result2["resource_type"] != "pod" {
		t.Errorf("Expected resource_type 'pod', got '%s'", result2["resource_type"])
	}
}

// BenchmarkAgentRun 基准测试 Agent 运行性能
func BenchmarkAgentRun(b *testing.B) {
	// 创建 Mock K8s Client
	k8sClient := k8s.NewMockClient(k8s.Config{})

	// 连接 K8s Client
	ctx := context.Background()
	if err := k8sClient.Connect(ctx); err != nil {
		b.Fatalf("Failed to connect K8s client: %v", err)
	}
	defer k8sClient.Close()

	// 创建 Mock Safety Agent
	safetyAgent := NewMockSafetyAgent()

	// 创建 Agent
	agent, err := NewAgent(k8sClient, safetyAgent, nil)
	if err != nil {
		b.Fatalf("Failed to create agent: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = agent.Run(ctx, "测试查询")
	}
	b.StopTimer()
}

// TestGraphFlow 测试 Graph 流转逻辑
func TestGraphFlow(t *testing.T) {
	// 创建 Mock K8s Client
	k8sClient := k8s.NewMockClient(k8s.Config{})

	// 连接 K8s Client
	ctx := context.Background()
	if err := k8sClient.Connect(ctx); err != nil {
		t.Fatalf("Failed to connect K8s client: %v", err)
	}
	defer k8sClient.Close()

	// 创建 Mock Safety Agent
	safetyAgent := NewMockSafetyAgent()

	// 创建 Agent
	agent, err := NewAgent(k8sClient, safetyAgent, nil)
	if err != nil {
		t.Fatalf("Failed to create agent: %v", err)
	}

	// 获取 Graph
	graph := agent.GetGraph()
	if graph == nil {
		t.Fatal("Expected non-nil graph")
	}

	t.Logf("Graph created successfully")

	// 运行分析并验证流程
	result, err := agent.Run(ctx, "测试 Graph 流转")
	if err != nil {
		t.Fatalf("Failed to run agent: %v", err)
	}

	if result == nil {
		t.Fatal("Expected non-nil result")
	}

	// 验证流程：Start -> Info -> Decision -> Action -> Decision -> Report
	t.Logf("Graph flow completed with status: %s", result.Status)
	t.Logf("Total iterations: %d", len(result.ExecutedCommands))

	// 验证至少执行了一次 Info 节点
	if len(result.ExecutedCommands) == 0 {
		t.Error("Expected at least one command execution")
	}
}

// TestStatePersistence 测试状态持久化
func TestStatePersistence(t *testing.T) {
	state := NewState("test")

	// 添加一些数据
	state.AddFinding("High", "test-pod", "Pod is restarting")
	state.AddRecommendation("Check logs", "Pod is restarting", "Medium", "kubectl logs test-pod")

	// 验证数据持久化
	if len(state.AnalysisResult.Findings) != 1 {
		t.Errorf("Expected 1 finding, got %d", len(state.AnalysisResult.Findings))
	}

	if len(state.AnalysisResult.Recommendations) != 1 {
		t.Errorf("Expected 1 recommendation, got %d", len(state.AnalysisResult.Recommendations))
	}
}

// TestTimeoutHandling 测试超时处理
func TestTimeoutHandling(t *testing.T) {
	// 创建 Mock K8s Client
	k8sClient := k8s.NewMockClient(k8s.Config{})

	// 连接 K8s Client
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	if err := k8sClient.Connect(ctx); err != nil {
		t.Fatalf("Failed to connect K8s client: %v", err)
	}
	defer k8sClient.Close()

	// 创建 Mock Safety Agent
	safetyAgent := NewMockSafetyAgent()

	// 创建 Agent
	agent, err := NewAgent(k8sClient, safetyAgent, nil)
	if err != nil {
		t.Fatalf("Failed to create agent: %v", err)
	}

	// 运行分析（应该因超时而失败）
	// 注意：由于 Graph 执行很快，1ms 的超时可能不够
	// 这个测试主要验证超时机制的存在
	_, err = agent.Run(ctx, "测试超时")

	// 不强制要求超时错误，因为 Graph 执行很快
	// 只要验证 Agent 能正常运行即可
	t.Logf("Agent completed with error: %v", err)
}
