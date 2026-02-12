// Package analysis 提供 Analysis Agent 的单元测试
package analysis

import (
	"context"
	"testing"
	"time"

	"github.com/AceDarkknight/k8s-analyzer-agent/internal/client/k8s"
	"github.com/AceDarkknight/k8s-analyzer-agent/internal/config"
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

// ExecuteSafeCommandWithAudit 模拟带审计的安全执行命令
func (m *MockSafetyAgent) ExecuteSafeCommandWithAudit(ctx context.Context, command string, contextInfo map[string]interface{}) (string, error) {
	m.commands = append(m.commands, command)
	return "Mock audit output for command: " + command, nil
}

// GetCommands 获取已执行的命令
func (m *MockSafetyAgent) GetCommands() []string {
	return m.commands
}

// getTestLLMConfig 返回测试用的 LLM 配置
func getTestLLMConfig() *config.LLMConfig {
	return &config.LLMConfig{
		Provider:    "rule-based",
		BaseURL:     "",
		APIKey:      "",
		Model:       "gpt-4",
		Temperature: 0.7,
		MaxTokens:   2000,
	}
}

// TestNewAgent 测试 Agent 创建
func TestNewAgent(t *testing.T) {
	// 创建 Mock K8s Client
	k8sClient := k8s.NewMockClient(k8s.Config{})

	// 连接 K8s Client (mock client 需要连接才能使用)
	ctx := context.Background()
	if err := k8sClient.Connect(ctx); err != nil {
		t.Fatalf("Failed to connect K8s client: %v", err)
	}
	defer k8sClient.Close()

	// 创建 Mock Safety Agent
	safetyAgent := NewMockSafetyAgent()

	// 创建 Agent
	agent, err := NewAgent(k8sClient, safetyAgent, getTestLLMConfig())
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
	agent, err := NewAgent(k8sClient, safetyAgent, getTestLLMConfig())
	if err != nil {
		t.Fatalf("Failed to create agent: %v", err)
	}

	// 运行分析
	result, err := agent.Run(ctx, "测试查询")
	if err != nil {
		t.Fatalf("Failed to run agent: %v", err)
	}

	if result == nil {
		t.Fatal("Expected non-nil result")
	}

	// 验证结果
	t.Logf("Analysis Status: %s", result.Status)
	t.Logf("Findings: %d", len(result.Findings))
	t.Logf("Recommendations: %d", len(result.Recommendations))
	t.Logf("Executed Commands: %d", len(result.ExecutedCommands))
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
	state.AnalysisResult.Status = StatusCompleted

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
	llm := NewRuleBasedLLM(getTestLLMConfig())

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

	// 测试有错误 Pod 的情况
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
	llm := NewMockLLM()

	state := NewState("test")

	// 测试达到最大迭代次数
	state.IterationCount = 10

	decision, err := llm.MakeDecision(context.Background(), state)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if decision != DecisionReport {
		t.Errorf("Expected decision '%s', got '%s'", DecisionReport, decision)
	}

	// 测试有错误 Pod 的情况
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

	if decision == "" {
		t.Errorf("Expected non-empty decision")
	}
}

// TestCommandGenerator 测试命令生成器
func TestCommandGenerator(t *testing.T) {
	generator := NewCommandGenerator()

	state := NewState("test")

	// 测试有错误 Pod 的情况
	state.K8sInfo.Pods = []PodInfo{
		{
			Name:   "error-pod",
			Status: "Error",
		},
	}

	command, err := generator.GenerateCommand(state)
	if err != nil {
		t.Errorf("Expected error when generating command for error pod")
	}

	if command == "" {
		t.Errorf("Expected non-empty command for error pod")
	}

	// 测试有重启次数过多的 Pod
	state = NewState("test")
	state.K8sInfo.Pods = []PodInfo{
		{
			Name:     "restart-pod",
			Status:   "Running",
			Restarts: 6,
		},
	}

	command, err = generator.GenerateCommand(state)
	if err != nil {
		t.Errorf("Unexpected error when generating command for restart pod")
	}

	if command == "" {
		t.Errorf("Expected non-empty command for restart pod")
	}

	// 测试有 Service 的情况
	state = NewState("test")
	state.K8sInfo.Services = []ServiceInfo{
		{
			Name:      "nginx-service",
			ClusterIP: "10.0.0.1",
		},
	}

	command, err = generator.GenerateCommand(state)
	if err != nil {
		t.Errorf("Expected error when generating command for service")
	}

	if command == "" {
		t.Errorf("Expected non-empty command for service")
	}

	// 测试有 Pod 的情况
	state = NewState("test")
	state.K8sInfo.Pods = []PodInfo{
		{
			Name:   "normal-pod",
			Status: "Running",
		},
	}

	command, err = generator.GenerateCommand(state)
	if err != nil {
		t.Errorf("Expected error when generating command for normal pod")
	}

	// 测试已经执行了命令的情况
	state = NewState("test")
	state.AnalysisResult.ExecutedCommands = []CommandExecution{
		{
			Command: "kubectl get pods",
			Success: true,
		},
	}

	command, err = generator.GenerateCommand(state)
	if err == nil {
		t.Errorf("Expected error when command already executed")
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
	agent, err := NewAgent(k8sClient, safetyAgent, getTestLLMConfig())
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

	// 验证状态为部分完成
	if result.Status != StatusPartial {
		t.Errorf("Expected status '%s', got '%s'", StatusPartial, result.Status)
	}

	// 验证有结果
	t.Logf("Analysis Status: %s", result.Status)
	t.Logf("Findings: %d", len(result.Findings))
	t.Logf("Recommendations: %d", len(result.Recommendations))
	t.Logf("Executed Commands: %d", len(result.ExecutedCommands))
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
	agent, err := NewAgent(k8sClient, safetyAgent, getTestLLMConfig())
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

	finding := state.AnalysisResult.Findings[0]
	if finding.Severity != "High" {
		t.Errorf("Expected severity 'High', got '%s'", finding.Severity)
	}

	rec := state.AnalysisResult.Recommendations[0]
	if rec.Action != "Check logs" {
		t.Errorf("Expected action 'Check logs', got '%s'", rec.Action)
	}
}

// TestTimeoutHandling 测试超时处理
func TestTimeoutHandling(t *testing.T) {
	// 创建 Mock K8s Client
	k8sClient := k8s.NewMockClient(k8s.Config{})

	// 连接 K8s Client (使用较长的超时时间以确保连接成功)
	connectCtx, connectCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer connectCancel()

	if err := k8sClient.Connect(connectCtx); err != nil {
		t.Fatalf("Failed to connect K8s client: %v", err)
	}
	defer k8sClient.Close()

	// 创建 Mock Safety Agent
	safetyAgent := NewMockSafetyAgent()

	// 创建 Agent
	agent, err := NewAgent(k8sClient, safetyAgent, getTestLLMConfig())
	if err != nil {
		t.Fatalf("Failed to create agent: %v", err)
	}

	// 运行分析（设置极短的超时时间，确保触发超时）
	// 注意：由于 Eino 框架内部处理可能很快，1ns 可能不够触发超时
	// 这里使用 context.WithCancel 手动取消，模拟超时效果
	runCtx, runCancel := context.WithCancel(context.Background())
	runCancel() // 立即取消

	_, err = agent.Run(runCtx, "测试超时")
	if err == nil {
		t.Error("Expected error when timeout")
	}

	// 验证 Agent 能正常运行
	t.Logf("Agent completed with error: %v", err)
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
	agent, err := NewAgent(k8sClient, safetyAgent, getTestLLMConfig())
	if err != nil {
		b.Fatalf("Failed to create agent: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = agent.Run(ctx, "测试查询")
	}
	b.StopTimer()
}
