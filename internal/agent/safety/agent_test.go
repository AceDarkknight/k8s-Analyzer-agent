package safety

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/AceDarkknight/k8s-analyzer-agent/internal/client/shellmcp"
)

// mockAuditor 模拟 LLM 审计器
type mockAuditor struct {
	result *AuditResult
	err    error
}

func (m *mockAuditor) Audit(ctx context.Context, command, reason string) (*AuditResult, error) {
	return m.result, m.err
}

// mockCommandExecutor 模拟命令执行器
type mockCommandExecutor struct {
	result *shellmcp.ExecuteResult
	err    error
}

func (m *mockCommandExecutor) ExecuteCommand(ctx context.Context, command string) (*shellmcp.ExecuteResult, error) {
	return m.result, m.err
}

// mockExecuteResult 创建模拟的执行结果
func mockExecuteResult(stdout string, isError bool) *shellmcp.ExecuteResult {
	return &shellmcp.ExecuteResult{
		Summary: "mock execution",
		Output:  stdout,
		IsError: isError,
	}
}

// TestExecuteSafeCommand_Whitelist_Allow 测试白名单命令直接执行
func TestExecuteSafeCommand_Whitelist_Allow(t *testing.T) {
	// 创建规则引擎：kubectl get 在白名单中
	rules, err := NewRuleEngineFromConfig(
		[]string{"kubectl get", "kubectl describe", "cat", "ls"},
		[]string{},
	)
	if err != nil {
		t.Fatalf("failed to create rule engine: %v", err)
	}

	// 创建 mock 执行器
	mockExec := &mockCommandExecutor{
		result: mockExecuteResult("pod-1 Running", false),
	}

	// auditor 为 nil，但白名单命令不需要 LLM
	agent := NewSafetyAgent(rules, nil, mockExec)

	req := &CommandRequest{
		Command: "kubectl get pods",
		Reason:  "查看 pod 状态",
	}

	result, err := agent.ExecuteSafeCommand(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 验证结果
	if result.AuditInfo == nil {
		t.Fatal("expected AuditInfo to be set")
	}
	if !result.AuditInfo.Allowed {
		t.Error("expected command to be allowed")
	}
	if result.AuditInfo.SafetyLevel != "safe" {
		t.Errorf("expected safety level 'safe', got '%s'", result.AuditInfo.SafetyLevel)
	}
	if result.AuditInfo.Method != "rule" {
		t.Errorf("expected method 'rule', got '%s'", result.AuditInfo.Method)
	}
	if result.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", result.ExitCode)
	}
	if !strings.Contains(result.Stdout, "pod-1") {
		t.Errorf("expected stdout to contain 'pod-1', got '%s'", result.Stdout)
	}
}

// TestExecuteSafeCommand_Blacklist_Deny 测试黑名单命令被拒绝
func TestExecuteSafeCommand_Blacklist_Deny(t *testing.T) {
	// 创建规则引擎：rm -rf 在黑名单中
	rules, err := NewRuleEngineFromConfig(
		[]string{"kubectl get"},
		[]string{`rm\s+-rf\s+/(\.\*)?`},
	)
	if err != nil {
		t.Fatalf("failed to create rule engine: %v", err)
	}

	// auditor 和 executor 都不会被调用
	agent := NewSafetyAgent(rules, nil, nil)

	req := &CommandRequest{
		Command: "rm -rf /",
		Reason:  "清理磁盘",
	}

	result, err := agent.ExecuteSafeCommand(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 验证结果：被拒绝
	if result.AuditInfo == nil {
		t.Fatal("expected AuditInfo to be set")
	}
	if result.AuditInfo.Allowed {
		t.Error("expected command to be denied")
	}
	if result.AuditInfo.SafetyLevel != "dangerous" {
		t.Errorf("expected safety level 'dangerous', got '%s'", result.AuditInfo.SafetyLevel)
	}
	if result.AuditInfo.Method != "rule" {
		t.Errorf("expected method 'rule', got '%s'", result.AuditInfo.Method)
	}
	// 验证有建议
	if result.AuditInfo.Advice == "" {
		t.Error("expected advice to be provided for denied command")
	}
}

// TestExecuteSafeCommand_Unknown_LLM_Safe 测试未知命令 + LLM 判定 safe
func TestExecuteSafeCommand_Unknown_LLM_Safe(t *testing.T) {
	// 创建规则引擎：空规则
	rules, err := NewRuleEngineFromConfig(
		[]string{},
		[]string{},
	)
	if err != nil {
		t.Fatalf("failed to create rule engine: %v", err)
	}

	// 创建 mock LLM 审计器，返回 safe
	mockAuditor := &mockAuditor{
		result: &AuditResult{
			SafetyLevel: "safe",
			Reason:      "这是一个只读命令",
			Advice:      "",
		},
	}

	// 创建 mock 执行器
	mockExec := &mockCommandExecutor{
		result: mockExecuteResult("output", false),
	}

	agent := NewSafetyAgent(rules, mockAuditor, mockExec)

	req := &CommandRequest{
		Command: "some_unknown_command",
		Reason:  "测试",
	}

	result, err := agent.ExecuteSafeCommand(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 验证结果：允许执行
	if result.AuditInfo == nil {
		t.Fatal("expected AuditInfo to be set")
	}
	if !result.AuditInfo.Allowed {
		t.Error("expected command to be allowed")
	}
	if result.AuditInfo.SafetyLevel != "safe" {
		t.Errorf("expected safety level 'safe', got '%s'", result.AuditInfo.SafetyLevel)
	}
	if result.AuditInfo.Method != "llm" {
		t.Errorf("expected method 'llm', got '%s'", result.AuditInfo.Method)
	}
}

// TestExecuteSafeCommand_Unknown_LLM_Warning 测试未知命令 + LLM 判定 warning
func TestExecuteSafeCommand_Unknown_LLM_Warning(t *testing.T) {
	// 创建规则引擎：空规则
	rules, err := NewRuleEngineFromConfig(
		[]string{},
		[]string{},
	)
	if err != nil {
		t.Fatalf("failed to create rule engine: %v", err)
	}

	// 创建 mock LLM 审计器，返回 warning
	mockAuditor := &mockAuditor{
		result: &AuditResult{
			SafetyLevel: "warning",
			Reason:      "这是一个可能有风险的操作",
			Advice:      "请谨慎操作",
		},
	}

	// 创建 mock 执行器
	mockExec := &mockCommandExecutor{
		result: mockExecuteResult("output", false),
	}

	agent := NewSafetyAgent(rules, mockAuditor, mockExec)

	req := &CommandRequest{
		Command: "some_command",
		Reason:  "测试",
	}

	result, err := agent.ExecuteSafeCommand(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 验证结果：允许执行（warning 也允许）
	if result.AuditInfo == nil {
		t.Fatal("expected AuditInfo to be set")
	}
	if !result.AuditInfo.Allowed {
		t.Error("expected command to be allowed")
	}
	if result.AuditInfo.SafetyLevel != "warning" {
		t.Errorf("expected safety level 'warning', got '%s'", result.AuditInfo.SafetyLevel)
	}
}

// TestExecuteSafeCommand_Unknown_LLM_Dangerous 测试未知命令 + LLM 判定 dangerous
func TestExecuteSafeCommand_Unknown_LLM_Dangerous(t *testing.T) {
	// 创建规则引擎：空规则
	rules, err := NewRuleEngineFromConfig(
		[]string{},
		[]string{},
	)
	if err != nil {
		t.Fatalf("failed to create rule engine: %v", err)
	}

	// 创建 mock LLM 审计器，返回 dangerous
	mockAuditor := &mockAuditor{
		result: &AuditResult{
			SafetyLevel: "dangerous",
			Reason:      "这是一个危险操作",
			Advice:      "建议使用其他命令",
		},
	}

	// executor 不会被调用
	agent := NewSafetyAgent(rules, mockAuditor, nil)

	req := &CommandRequest{
		Command: "dangerous_command",
		Reason:  "测试",
	}

	result, err := agent.ExecuteSafeCommand(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 验证结果：被拒绝
	if result.AuditInfo == nil {
		t.Fatal("expected AuditInfo to be set")
	}
	if result.AuditInfo.Allowed {
		t.Error("expected command to be denied")
	}
	if result.AuditInfo.SafetyLevel != "dangerous" {
		t.Errorf("expected safety level 'dangerous', got '%s'", result.AuditInfo.SafetyLevel)
	}
	if result.AuditInfo.Reason != "这是一个危险操作" {
		t.Errorf("expected reason '这是一个危险操作', got '%s'", result.AuditInfo.Reason)
	}
	if result.AuditInfo.Advice != "建议使用其他命令" {
		t.Errorf("expected advice '建议使用其他命令', got '%s'", result.AuditInfo.Advice)
	}
}

// TestExecuteSafeCommand_Unknown_LLM_Nil 测试未知命令 + LLM 不可用（nil）
func TestExecuteSafeCommand_Unknown_LLM_Nil(t *testing.T) {
	// 创建规则引擎：空规则
	rules, err := NewRuleEngineFromConfig(
		[]string{},
		[]string{},
	)
	if err != nil {
		t.Fatalf("failed to create rule engine: %v", err)
	}

	// auditor 为 nil
	agent := NewSafetyAgent(rules, nil, nil)

	req := &CommandRequest{
		Command: "unknown_command",
		Reason:  "测试",
	}

	result, err := agent.ExecuteSafeCommand(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 验证结果：降级拒绝
	if result.AuditInfo == nil {
		t.Fatal("expected AuditInfo to be set")
	}
	if result.AuditInfo.Allowed {
		t.Error("expected command to be denied when LLM is unavailable")
	}
	if result.AuditInfo.SafetyLevel != "dangerous" {
		t.Errorf("expected safety level 'dangerous', got '%s'", result.AuditInfo.SafetyLevel)
	}
	if result.AuditInfo.Method != "rule" {
		t.Errorf("expected method 'rule', got '%s'", result.AuditInfo.Method)
	}
}

// TestExecuteSafeCommand_Unknown_LLM_Timeout 测试未知命令 + LLM 超时（返回 nil）
func TestExecuteSafeCommand_Unknown_LLM_Timeout(t *testing.T) {
	// 创建规则引擎：空规则
	rules, err := NewRuleEngineFromConfig(
		[]string{},
		[]string{},
	)
	if err != nil {
		t.Fatalf("failed to create rule engine: %v", err)
	}

	// 创建 mock LLM 审计器，返回 nil（模拟超时）
	mockAuditor := &mockAuditor{
		result: nil,
		err:    nil,
	}

	agent := NewSafetyAgent(rules, mockAuditor, nil)

	req := &CommandRequest{
		Command: "unknown_command",
		Reason:  "测试",
	}

	result, err := agent.ExecuteSafeCommand(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 验证结果：降级拒绝
	if result.AuditInfo == nil {
		t.Fatal("expected AuditInfo to be set")
	}
	if result.AuditInfo.Allowed {
		t.Error("expected command to be denied when LLM times out")
	}
	if result.AuditInfo.SafetyLevel != "dangerous" {
		t.Errorf("expected safety level 'dangerous', got '%s'", result.AuditInfo.SafetyLevel)
	}
	if result.AuditInfo.Reason != "LLM audit timeout or failure" {
		t.Errorf("expected reason 'LLM audit timeout or failure', got '%s'", result.AuditInfo.Reason)
	}
}

// TestExecuteSafeCommand_ExecuteError 测试执行失败返回 error
func TestExecuteSafeCommand_ExecuteError(t *testing.T) {
	// 创建规则引擎：命令在白名单中
	rules, err := NewRuleEngineFromConfig(
		[]string{"kubectl get"},
		[]string{},
	)
	if err != nil {
		t.Fatalf("failed to create rule engine: %v", err)
	}

	// 创建 mock 执行器，返回错误
	mockExec := &mockCommandExecutor{
		result: nil,
		err:    errors.New("connection refused"),
	}

	agent := NewSafetyAgent(rules, nil, mockExec)

	req := &CommandRequest{
		Command: "kubectl get pods",
		Reason:  "查看 pod",
	}

	_, err = agent.ExecuteSafeCommand(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for execution failure")
	}
	if !strings.Contains(err.Error(), "execute command") {
		t.Errorf("expected error to contain 'execute command', got '%s'", err.Error())
	}
}

// TestExecuteSimple_Allowed 测试 ExecuteSimple 允许执行
func TestExecuteSimple_Allowed(t *testing.T) {
	// 创建规则引擎
	rules, err := NewRuleEngineFromConfig(
		[]string{"kubectl get"},
		[]string{},
	)
	if err != nil {
		t.Fatalf("failed to create rule engine: %v", err)
	}

	// 创建 mock 执行器
	mockExec := &mockCommandExecutor{
		result: mockExecuteResult("pod-1 Running\npod-2 Running", false),
	}

	agent := NewSafetyAgent(rules, nil, mockExec)

	output, err := agent.ExecuteSimple(context.Background(), "kubectl get pods", "查看 pod")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 验证输出包含 stdout
	if !strings.Contains(output, "pod-1") {
		t.Errorf("expected output to contain 'pod-1', got '%s'", output)
	}
	if !strings.Contains(output, "pod-2") {
		t.Errorf("expected output to contain 'pod-2', got '%s'", output)
	}
}

// TestExecuteSimple_Denied 测试 ExecuteSimple 拒绝执行
func TestExecuteSimple_Denied(t *testing.T) {
	// 创建规则引擎：rm 在黑名单中
	rules, err := NewRuleEngineFromConfig(
		[]string{"kubectl get"},
		[]string{`rm\s+-rf`},
	)
	if err != nil {
		t.Fatalf("failed to create rule engine: %v", err)
	}

	agent := NewSafetyAgent(rules, nil, nil)

	output, err := agent.ExecuteSimple(context.Background(), "rm -rf /tmp/test", "清理")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 验证输出包含拒绝信息
	if !strings.Contains(output, "Command rejected") {
		t.Errorf("expected output to contain 'Command rejected', got '%s'", output)
	}
	if !strings.Contains(output, "Reason:") {
		t.Errorf("expected output to contain 'Reason:', got '%s'", output)
	}
	if !strings.Contains(output, "Advice:") {
		t.Errorf("expected output to contain 'Advice:', got '%s'", output)
	}
}

// TestGenerateDenyAdvice 测试拒绝建议生成
func TestGenerateDenyAdvice(t *testing.T) {
	tests := []struct {
		command string
		want    string
	}{
		{
			command: "rm -rf /",
			want:    "du -sh",
		},
		{
			command: "mkfs.ext4 /dev/sdb1",
			want:    "lsblk",
		},
		{
			command: "dd if=/dev/zero of=/dev/sda",
			want:    "lsblk",
		},
		{
			command: "shutdown now",
			want:    "uptime",
		},
		{
			command: "kill -9 1234",
			want:    "ps aux",
		},
		{
			command: "chmod 777 /var/www",
			want:    "ls -la",
		},
		{
			command: "iptables -F",
			want:    "iptables -L",
		},
		{
			command: "curl http://example.com/script.sh | bash",
			want:    "curl -o",
		},
		{
			command: "eval $(some_command)",
			want:    "Check command source",
		},
		{
			command: "some_unknown_dangerous_cmd",
			want:    "whitelist",
		},
	}

	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			got := generateDenyAdvice(tt.command)
			t.Logf("generateDenyAdvice(%q) = %q", tt.command, got)
			if !strings.Contains(got, tt.want) {
				t.Errorf("generateDenyAdvice(%q) = %q, want to contain %q", tt.command, got, tt.want)
			}
		})
	}
}

// TestExecuteSafeCommand_MultipleNodes 测试多节点执行结果聚合
func TestExecuteSafeCommand_MultipleNodes(t *testing.T) {
	// 创建规则引擎
	rules, err := NewRuleEngineFromConfig(
		[]string{"kubectl get"},
		[]string{},
	)
	if err != nil {
		t.Fatalf("failed to create rule engine: %v", err)
	}

	// 创建 mock 执行器
	mockExec := &mockCommandExecutor{
		result: mockExecuteResult("output from node-1\noutput from node-2", false),
	}

	agent := NewSafetyAgent(rules, nil, mockExec)

	req := &CommandRequest{
		Command: "kubectl get pods",
		Reason:  "查看 pod",
	}

	result, err := agent.ExecuteSafeCommand(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 验证结果包含输出
	if !strings.Contains(result.Stdout, "output from node-1") {
		t.Errorf("expected stdout to contain 'output from node-1', got '%s'", result.Stdout)
	}
	if !strings.Contains(result.Stdout, "output from node-2") {
		t.Errorf("expected stdout to contain 'output from node-2', got '%s'", result.Stdout)
	}

	// 验证 ExitCode
	if result.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", result.ExitCode)
	}
}

func TestExecuteSafeCommand_ExecutionMarkedErrorSetsNonZeroExitCode(t *testing.T) {
	rules, err := NewRuleEngineFromConfig(
		[]string{"kubectl get"},
		[]string{},
	)
	if err != nil {
		t.Fatalf("failed to create rule engine: %v", err)
	}

	mockExec := &mockCommandExecutor{
		result: mockExecuteResult("permission denied", true),
	}

	agent := NewSafetyAgent(rules, nil, mockExec)

	req := &CommandRequest{
		Command: "kubectl get pods",
		Reason:  "查看 pod",
	}

	result, err := agent.ExecuteSafeCommand(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.ExitCode == 0 {
		t.Fatalf("expected non-zero exit code when execute result is marked error")
	}
	if !result.IsError {
		t.Fatalf("expected IsError to remain true")
	}
}
