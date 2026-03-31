package safety

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/assert"
)

// mockChatModel 实现 model.ChatModel 接口
type mockChatModel struct {
	generateFunc func(ctx context.Context, messages []*schema.Message, opts ...model.Option) (*schema.Message, error)
}

func (m *mockChatModel) Generate(ctx context.Context, messages []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	if m.generateFunc != nil {
		return m.generateFunc(ctx, messages, opts...)
	}
	return nil, errors.New("generateFunc not set")
}

func (m *mockChatModel) Stream(ctx context.Context, messages []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, errors.New("not implemented")
}

func (m *mockChatModel) BindTools(tools []*schema.ToolInfo) error {
	return nil
}

func TestLLMAuditor_Audit_Safe(t *testing.T) {
	mock := &mockChatModel{
		generateFunc: func(ctx context.Context, messages []*schema.Message, opts ...model.Option) (*schema.Message, error) {
			return &schema.Message{
				Role:    schema.Assistant,
				Content: `{"safety_level": "safe", "reason": "这是一个只读命令", "advice": ""}`,
			}, nil
		},
	}

	auditor := NewLLMAuditor(mock)
	result, err := auditor.Audit(context.Background(), "cat /var/log/messages", "查看日志")

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "safe", result.SafetyLevel)
	assert.Equal(t, "这是一个只读命令", result.Reason)
	assert.Empty(t, result.Advice)
}

func TestLLMAuditor_Audit_Dangerous(t *testing.T) {
	mock := &mockChatModel{
		generateFunc: func(ctx context.Context, messages []*schema.Message, opts ...model.Option) (*schema.Message, error) {
			return &schema.Message{
				Role:    schema.Assistant,
				Content: `{"safety_level": "dangerous", "reason": "rm -rf 会删除文件", "advice": "使用 rm -i 或先备份"}`,
			}, nil
		},
	}

	auditor := NewLLMAuditor(mock)
	result, err := auditor.Audit(context.Background(), "rm -rf /tmp/*", "清理临时文件")

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "dangerous", result.SafetyLevel)
	assert.Equal(t, "rm -rf 会删除文件", result.Reason)
	assert.Equal(t, "使用 rm -i 或先备份", result.Advice)
}

func TestLLMAuditor_Audit_InvalidJSON_RetrySuccess(t *testing.T) {
	callCount := 0
	mock := &mockChatModel{
		generateFunc: func(ctx context.Context, messages []*schema.Message, opts ...model.Option) (*schema.Message, error) {
			callCount++
			if callCount == 1 {
				// 第一次返回无效 JSON
				return &schema.Message{
					Role:    schema.Assistant,
					Content: `invalid json`,
				}, nil
			}
			// 第二次返回有效 JSON
			return &schema.Message{
				Role:    schema.Assistant,
				Content: `{"safety_level": "safe", "reason": "重试成功", "advice": ""}`,
			}, nil
		},
	}

	auditor := NewLLMAuditor(mock)
	result, err := auditor.Audit(context.Background(), "ps aux", "查看进程")

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "safe", result.SafetyLevel)
	assert.Equal(t, 2, callCount)
}

func TestLLMAuditor_Audit_ContextTimeout(t *testing.T) {
	mock := &mockChatModel{
		generateFunc: func(ctx context.Context, messages []*schema.Message, opts ...model.Option) (*schema.Message, error) {
			return nil, context.DeadlineExceeded
		},
	}

	auditor := NewLLMAuditor(mock)
	result, err := auditor.Audit(context.Background(), "ls -la", "列出文件")

	assert.NoError(t, err)
	assert.Nil(t, result)
}

func TestLLMAuditor_Audit_ContextCanceled(t *testing.T) {
	mock := &mockChatModel{
		generateFunc: func(ctx context.Context, messages []*schema.Message, opts ...model.Option) (*schema.Message, error) {
			return nil, context.Canceled
		},
	}

	auditor := NewLLMAuditor(mock)
	result, err := auditor.Audit(context.Background(), "ls -la", "列出文件")

	assert.NoError(t, err)
	assert.Nil(t, result)
}

func TestLLMAuditor_Audit_MarkdownJSON(t *testing.T) {
	mock := &mockChatModel{
		generateFunc: func(ctx context.Context, messages []*schema.Message, opts ...model.Option) (*schema.Message, error) {
			return &schema.Message{
				Role:    schema.Assistant,
				Content: "```json\n{\"safety_level\": \"warning\", \"reason\": \"markdown 包裹\", \"advice\": \"小心使用\"}\n```",
			}, nil
		},
	}

	auditor := NewLLMAuditor(mock)
	result, err := auditor.Audit(context.Background(), "systemctl status nginx", "检查服务状态")

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "warning", result.SafetyLevel)
	assert.Equal(t, "markdown 包裹", result.Reason)
	assert.Equal(t, "小心使用", result.Advice)
}

func TestLLMAuditor_Audit_MarkdownNoLangJSON(t *testing.T) {
	mock := &mockChatModel{
		generateFunc: func(ctx context.Context, messages []*schema.Message, opts ...model.Option) (*schema.Message, error) {
			return &schema.Message{
				Role:    schema.Assistant,
				Content: "```\n{\"safety_level\": \"safe\", \"reason\": \"无语言标记\", \"advice\": \"\"}\n```",
			}, nil
		},
	}

	auditor := NewLLMAuditor(mock)
	result, err := auditor.Audit(context.Background(), "df -h", "查看磁盘")

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "safe", result.SafetyLevel)
	assert.Equal(t, "无语言标记", result.Reason)
}

func TestLLMAuditor_Audit_InvalidSafetyLevel(t *testing.T) {
	mock := &mockChatModel{
		generateFunc: func(ctx context.Context, messages []*schema.Message, opts ...model.Option) (*schema.Message, error) {
			return &schema.Message{
				Role:    schema.Assistant,
				Content: `{"safety_level": "invalid", "reason": "无效级别", "advice": ""}`,
			}, nil
		},
	}

	auditor := NewLLMAuditor(mock)
	// 第一次失败，重试
	mock.generateFunc = func(ctx context.Context, messages []*schema.Message, opts ...model.Option) (*schema.Message, error) {
		return &schema.Message{
			Role:    schema.Assistant,
			Content: `{"safety_level": "safe", "reason": "有效级别", "advice": ""}`,
		}, nil
	}

	result, err := auditor.Audit(context.Background(), "cat file", "查看")
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "safe", result.SafetyLevel)
}

func TestExtractJSON(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "plain json",
			input:    `{"safety_level": "safe", "reason": "test"}`,
			expected: `{"safety_level": "safe", "reason": "test"}`,
		},
		{
			name:     "markdown json",
			input:    "```json\n{\"safety_level\": \"safe\"}\n```",
			expected: `{"safety_level": "safe"}`,
		},
		{
			name:     "markdown no lang",
			input:    "```\n{\"safety_level\": \"safe\"}\n```",
			expected: `{"safety_level": "safe"}`,
		},
		{
			name:     "with extra text",
			input:    "Here is the result:\n```json\n{\"safety_level\": \"safe\"}\n```\nDone!",
			expected: `{"safety_level": "safe"}`,
		},
		{
			name:     "no braces",
			input:    "just text",
			expected: "just text",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractJSON(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestParseAuditResult(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expectError bool
		expected    *AuditResult
	}{
		{
			name:        "valid json",
			input:       `{"safety_level": "safe", "reason": "test", "advice": ""}`,
			expectError: false,
			expected: &AuditResult{
				SafetyLevel: "safe",
				Reason:      "test",
				Advice:      "",
			},
		},
		{
			name:        "invalid json",
			input:       `not json`,
			expectError: true,
		},
		{
			name:        "invalid safety level",
			input:       `{"safety_level": "unknown", "reason": "test"}`,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseAuditResult(tt.input)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestLLMAuditor_Audit_RealContextTimeout(t *testing.T) {
	mock := &mockChatModel{
		generateFunc: func(ctx context.Context, messages []*schema.Message, opts ...model.Option) (*schema.Message, error) {
			// 模拟长时间运行
			select {
			case <-time.After(100 * time.Millisecond):
				return &schema.Message{
					Role:    schema.Assistant,
					Content: `{"safety_level": "safe", "reason": "test"}`,
				}, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
	}

	auditor := NewLLMAuditor(mock)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	result, err := auditor.Audit(ctx, "cat file", "查看")

	assert.NoError(t, err)
	assert.Nil(t, result)
}

func TestBuildAuditPrompt(t *testing.T) {
	command := "ls -la"
	reason := "查看文件"

	prompt := buildAuditPrompt(command, reason)

	assert.Contains(t, prompt, command)
	assert.Contains(t, prompt, reason)
	assert.Contains(t, prompt, "Safe（安全）")
	assert.Contains(t, prompt, "Warning（警告）")
	assert.Contains(t, prompt, "Dangerous（危险）")
}
