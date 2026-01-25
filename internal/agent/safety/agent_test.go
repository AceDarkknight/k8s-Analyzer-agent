// Package safety 测试命令安全验证和执行功能
package safety

import (
	"context"
	"errors"
	"testing"

	"github.com/your-org/k8s-analyzer-agent/internal/client/shell"
)

// MockShellClient 模拟 Shell 客户端
type MockShellClient struct {
	executeFunc func(ctx context.Context, command string) (*shell.ExecuteResult, error)
}

func (m *MockShellClient) ExecuteCommand(ctx context.Context, command string) (*shell.ExecuteResult, error) {
	if m.executeFunc != nil {
		return m.executeFunc(ctx, command)
	}
	return &shell.ExecuteResult{
		Summary: "Mock execution successful",
		Groups: []shell.ExecuteGroup{
			{
				Count:  1,
				Status: "success",
				Output: "mock output",
				Nodes:  []string{"node1"},
			},
		},
	}, nil
}

// TestValidator_Whitelist 测试白名单验证
func TestValidator_Whitelist(t *testing.T) {
	config := &SecurityConfig{
		CommandWhitelist: []string{"ls", "cat", "kubectl"},
	}

	validator, err := NewValidatorWithConfig(config)
	if err != nil {
		t.Fatalf("Failed to create validator: %v", err)
	}

	tests := []struct {
		name    string
		command string
		wantErr bool
	}{
		{
			name:    "safe command in whitelist",
			command: "ls -la",
			wantErr: false,
		},
		{
			name:    "safe command in whitelist with different case",
			command: "LS -la",
			wantErr: false,
		},
		{
			name:    "command not in whitelist",
			command: "rm -rf /",
			wantErr: true,
		},
		{
			name:    "empty command",
			command: "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateCommand(tt.command)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateCommand() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestValidator_Blacklist 测试黑名单验证
func TestValidator_Blacklist(t *testing.T) {
	config := &SecurityConfig{
		BlacklistedCommands: []string{"rm", "mkfs", "shutdown"},
	}

	validator, err := NewValidatorWithConfig(config)
	if err != nil {
		t.Fatalf("Failed to create validator: %v", err)
	}

	tests := []struct {
		name    string
		command string
		wantErr bool
	}{
		{
			name:    "blacklisted command",
			command: "rm -rf /",
			wantErr: true,
		},
		{
			name:    "blacklisted command with different case",
			command: "MKFS /dev/sda1",
			wantErr: true,
		},
		{
			name:    "safe command not blacklisted",
			command: "ls -la",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateCommand(tt.command)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateCommand() error = %v, wantErr %v", err, tt.wantErr)
			}

			if tt.wantErr && !IsUnsafeCommand(err) {
				t.Errorf("Expected UnsafeCommandError, got %T", err)
			}
		})
	}
}

// TestValidator_DangerousPatterns 测试危险模式匹配
func TestValidator_DangerousPatterns(t *testing.T) {
	config := &SecurityConfig{
		CommandWhitelist: []string{"rm", "dd"},
		DangerousArgsRegex: []string{
			`rm\s+-[a-zA-Z]*r[a-zA-Z]*\s+/`,
			`dd\s+.*of=/`,
		},
	}

	validator, err := NewValidatorWithConfig(config)
	if err != nil {
		t.Fatalf("Failed to create validator: %v", err)
	}

	tests := []struct {
		name    string
		command string
		wantErr bool
	}{
		{
			name:    "rm -rf / dangerous pattern",
			command: "rm -rf /",
			wantErr: true,
		},
		{
			name:    "rm -r / dangerous pattern",
			command: "rm -r /",
			wantErr: true,
		},
		{
			name:    "rm -rfv / dangerous pattern",
			command: "rm -rfv /",
			wantErr: true,
		},
		{
			name:    "dd with of=/ dangerous pattern",
			command: "dd if=/dev/zero of=/dev/sda1",
			wantErr: true,
		},
		{
			name:    "safe rm command",
			command: "rm -rf /tmp/test",
			wantErr: true, // rm -rf /tmp/test 匹配 rm\s+-[a-zA-Z]*r[a-zA-Z]*\s+/
		},
		{
			name:    "safe dd command",
			command: "dd if=/dev/zero of=/tmp/test.img",
			wantErr: true, // dd if=/dev/zero of=/tmp/test.img 匹配 dd\s+.*of=/
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateCommand(tt.command)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateCommand() error = %v, wantErr %v", err, tt.wantErr)
			}

			if tt.wantErr && !IsUnsafeCommand(err) {
				t.Errorf("Expected UnsafeCommandError, got %T", err)
			}
		})
	}
}

// TestValidator_ComplexScenarios 测试复杂场景
func TestValidator_ComplexScenarios(t *testing.T) {
	config := &SecurityConfig{
		CommandWhitelist:    []string{"ls", "cat", "kubectl", "grep"},
		BlacklistedCommands: []string{"rm", "dd"},
		DangerousArgsRegex: []string{
			`rm\s+-[a-zA-Z]*r[a-zA-Z]*\s+/`,
			`dd\s+.*of=/`,
		},
	}

	validator, err := NewValidatorWithConfig(config)
	if err != nil {
		t.Fatalf("Failed to create validator: %v", err)
	}

	tests := []struct {
		name    string
		command string
		wantErr bool
	}{
		{
			name:    "safe ls command",
			command: "ls -la /tmp",
			wantErr: false,
		},
		{
			name:    "safe cat command",
			command: "cat /etc/hosts",
			wantErr: false,
		},
		{
			name:    "safe kubectl command",
			command: "kubectl get pods",
			wantErr: false,
		},
		{
			name:    "safe grep command",
			command: "grep error /var/log/app.log",
			wantErr: false,
		},
		{
			name:    "blacklisted rm command",
			command: "rm /tmp/test",
			wantErr: true,
		},
		{
			name:    "blacklisted dd command",
			command: "dd if=/dev/zero of=/tmp/test",
			wantErr: true,
		},
		{
			name:    "command not in whitelist",
			command: "ps aux",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateCommand(tt.command)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateCommand() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestAgent_ExecuteSafeCommand 测试安全命令执行
func TestAgent_ExecuteSafeCommand(t *testing.T) {
	config := &SecurityConfig{
		CommandWhitelist:    []string{"ls", "cat"},
		BlacklistedCommands: []string{"rm"},
		DangerousArgsRegex: []string{
			`rm\s+-[a-zA-Z]*r[a-zA-Z]*\s+/`,
		},
	}

	validator, err := NewValidatorWithConfig(config)
	if err != nil {
		t.Fatalf("Failed to create validator: %v", err)
	}

	mockClient := &MockShellClient{}
	agent := NewAgentWithValidator(mockClient, validator)

	tests := []struct {
		name    string
		command string
		wantErr bool
	}{
		{
			name:    "execute safe command",
			command: "ls -la",
			wantErr: false,
		},
		{
			name:    "execute another safe command",
			command: "cat /etc/hosts",
			wantErr: false,
		},
		{
			name:    "reject blacklisted command",
			command: "rm -rf /",
			wantErr: true,
		},
		{
			name:    "reject command not in whitelist",
			command: "ps aux",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			output, err := agent.ExecuteSafeCommand(ctx, tt.command)

			if (err != nil) != tt.wantErr {
				t.Errorf("ExecuteSafeCommand() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && output == "" {
				t.Errorf("ExecuteSafeCommand() expected output, got empty string")
			}
		})
	}
}

// TestAgent_ExecuteSafeCommandWithClientError 测试客户端错误处理
func TestAgent_ExecuteSafeCommandWithClientError(t *testing.T) {
	config := &SecurityConfig{
		CommandWhitelist: []string{"ls"},
	}

	validator, err := NewValidatorWithConfig(config)
	if err != nil {
		t.Fatalf("Failed to create validator: %v", err)
	}

	// 创建返回错误的模拟客户端
	mockClient := &MockShellClient{
		executeFunc: func(ctx context.Context, command string) (*shell.ExecuteResult, error) {
			return nil, errors.New("client connection failed")
		},
	}

	agent := NewAgentWithValidator(mockClient, validator)

	ctx := context.Background()
	_, err = agent.ExecuteSafeCommand(ctx, "ls -la")

	if err == nil {
		t.Errorf("Expected error from ExecuteSafeCommand, got nil")
	}

	if IsUnsafeCommand(err) {
		t.Errorf("Expected client error, not UnsafeCommandError")
	}
}

// TestUnsafeCommandError 测试不安全命令错误
func TestUnsafeCommandError(t *testing.T) {
	err := &UnsafeCommandError{
		Command: "rm -rf /",
		Reason:  "command 'rm' is in blacklist",
	}

	expected := "unsafe command: rm -rf / - command 'rm' is in blacklist"
	if err.Error() != expected {
		t.Errorf("UnsafeCommandError.Error() = %v, want %v", err.Error(), expected)
	}

	if !IsUnsafeCommand(err) {
		t.Errorf("IsUnsafeCommand() should return true for UnsafeCommandError")
	}

	otherErr := errors.New("some other error")
	if IsUnsafeCommand(otherErr) {
		t.Errorf("IsUnsafeCommand() should return false for non-UnsafeCommandError")
	}
}

// TestValidator_ExtractCommandName 测试命令名称提取
func TestValidator_ExtractCommandName(t *testing.T) {
	config := &SecurityConfig{}
	validator, err := NewValidatorWithConfig(config)
	if err != nil {
		t.Fatalf("Failed to create validator: %v", err)
	}

	tests := []struct {
		name     string
		command  string
		expected string
	}{
		{
			name:     "simple command",
			command:  "ls",
			expected: "ls",
		},
		{
			name:     "command with arguments",
			command:  "ls -la /tmp",
			expected: "ls",
		},
		{
			name:     "command with pipe",
			command:  "ls | grep test",
			expected: "ls",
		},
		{
			name:     "command with semicolon",
			command:  "ls ; cat file",
			expected: "ls",
		},
		{
			name:     "command with redirection",
			command:  "ls > output.txt",
			expected: "ls",
		},
		{
			name:     "command with tabs",
			command:  "ls\t-la",
			expected: "ls",
		},
		{
			name:     "command with leading/trailing spaces",
			command:  "  ls -la  ",
			expected: "ls",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := validator.extractCommandName(tt.command)
			if result != tt.expected {
				t.Errorf("extractCommandName() = %v, want %v", result, tt.expected)
			}
		})
	}
}

// TestAgent_FormatOutput 测试输出格式化
func TestAgent_FormatOutput(t *testing.T) {
	agent := &Agent{}

	tests := []struct {
		name   string
		result *shell.ExecuteResult
		want   string
	}{
		{
			name:   "nil result",
			result: nil,
			want:   "",
		},
		{
			name: "successful execution",
			result: &shell.ExecuteResult{
				Summary: "Executed on 1 nodes: 1 success, 0 failed",
				Groups: []shell.ExecuteGroup{
					{
						Count:  1,
						Status: "success",
						Output: "file1.txt\nfile2.txt",
						Nodes:  []string{"node1"},
					},
				},
			},
			want: "Executed on 1 nodes: 1 success, 0 failed\n\nOutput:\nfile1.txt\nfile2.txt\n",
		},
		{
			name: "failed execution",
			result: &shell.ExecuteResult{
				Summary: "Executed on 1 nodes: 0 success, 1 failed",
				Groups: []shell.ExecuteGroup{
					{
						Count:  1,
						Status: "failed",
						Output: "command not found",
						Nodes:  []string{"node1"},
					},
				},
			},
			want: "Executed on 1 nodes: 0 success, 1 failed\n\nErrors:\ncommand not found\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := agent.formatOutput(tt.result)
			if result != tt.want {
				t.Errorf("formatOutput() = %v, want %v", result, tt.want)
			}
		})
	}
}

// TestValidator_EmptyWhitelist 测试空白名单行为
func TestValidator_EmptyWhitelist(t *testing.T) {
	config := &SecurityConfig{
		CommandWhitelist:    []string{},
		BlacklistedCommands: []string{"rm"},
	}

	validator, err := NewValidatorWithConfig(config)
	if err != nil {
		t.Fatalf("Failed to create validator: %v", err)
	}

	// 空白名单时，只检查黑名单
	tests := []struct {
		name    string
		command string
		wantErr bool
	}{
		{
			name:    "command not in whitelist but not blacklisted",
			command: "ps aux",
			wantErr: false,
		},
		{
			name:    "blacklisted command",
			command: "rm -rf /",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateCommand(tt.command)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateCommand() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
