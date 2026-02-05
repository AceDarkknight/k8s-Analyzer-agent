// Package client 测试重试机制
package client

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultRetryConfig(t *testing.T) {
	config := DefaultRetryConfig()

	assert.Equal(t, 3, config.MaxAttempts, "默认最大重试次数应为 3")
	assert.Equal(t, 1, config.InitialDelay, "默认初始延迟应为 1 秒")
	assert.Equal(t, 10, config.MaxDelay, "默认最大延迟应为 10 秒")
	assert.Equal(t, 2.0, config.Multiplier, "默认倍增因子应为 2.0")
	assert.True(t, config.Jitter, "默认应启用抖动")
}

func TestRetryWithBackoff_Success(t *testing.T) {
	ctx := context.Background()
	config := DefaultRetryConfig()

	attempts := 0
	fn := func() error {
		attempts++
		if attempts < 2 {
			return &ConnectionError{Reason: "temporary error"}
		}
		return nil
	}

	err := RetryWithBackoff(ctx, config, fn)

	assert.NoError(t, err, "应该成功")
	assert.Equal(t, 2, attempts, "应该重试 1 次")
}

func TestRetryWithBackoff_AllFail(t *testing.T) {
	ctx := context.Background()
	config := DefaultRetryConfig()

	attempts := 0
	fn := func() error {
		attempts++
		return &ConnectionError{Reason: "persistent error"}
	}

	err := RetryWithBackoff(ctx, config, fn)

	assert.Error(t, err, "应该失败")
	assert.Equal(t, 3, attempts, "应该尝试 3 次")
	assert.Contains(t, err.Error(), "failed after 3 attempts", "错误信息应包含重试次数")
}

func TestRetryWithBackoff_NonRetryableError(t *testing.T) {
	ctx := context.Background()
	config := DefaultRetryConfig()

	attempts := 0
	fn := func() error {
		attempts++
		return errors.New("non-retryable error")
	}

	err := RetryWithBackoff(ctx, config, fn)

	assert.Error(t, err, "应该失败")
	assert.Equal(t, 1, attempts, "对于不可重试的错误，应该只尝试 1 次")
	assert.Contains(t, err.Error(), "non-retryable error", "错误信息应包含不可重试标记")
}

func TestRetryWithBackoff_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	config := DefaultRetryConfig()

	attempts := 0
	fn := func() error {
		attempts++
		if attempts == 1 {
			cancel() // 取消上下文
		}
		return &ConnectionError{Reason: "error"}
	}

	err := RetryWithBackoff(ctx, config, fn)

	assert.Error(t, err, "应该失败")
	assert.Contains(t, err.Error(), "retry cancelled", "错误信息应包含取消标记")
}

func TestRetryWithResult_Success(t *testing.T) {
	ctx := context.Background()
	config := DefaultRetryConfig()

	attempts := 0
	fn := func() (string, error) {
		attempts++
		if attempts < 2 {
			return "", &ConnectionError{Reason: "temporary error"}
		}
		return "success", nil
	}

	result, err := RetryWithResult[string](ctx, config, fn)

	assert.NoError(t, err, "应该成功")
	assert.Equal(t, "success", result, "结果应该正确")
	assert.Equal(t, 2, attempts, "应该重试 1 次")
}

func TestRetryWithResult_AllFail(t *testing.T) {
	ctx := context.Background()
	config := DefaultRetryConfig()

	attempts := 0
	fn := func() (string, error) {
		attempts++
		return "", &ConnectionError{Reason: "persistent error"}
	}

	result, err := RetryWithResult[string](ctx, config, fn)

	assert.Error(t, err, "应该失败")
	assert.Equal(t, "", result, "结果应为空")
	assert.Equal(t, 3, attempts, "应该尝试 3 次")
}

func TestRetryWithContext_Success(t *testing.T) {
	ctx := context.Background()
	config := DefaultRetryConfig()

	attempts := 0
	fn := func(ctx context.Context) error {
		attempts++
		if attempts < 2 {
			return &ConnectionError{Reason: "temporary error"}
		}
		return nil
	}

	err := RetryWithContext(ctx, config, fn)

	assert.NoError(t, err, "应该成功")
	assert.Equal(t, 2, attempts, "应该重试 1 次")
}

func TestIsRetryableError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "connection error",
			err:      &ConnectionError{Reason: "connection failed"},
			expected: true,
		},
		{
			name:     "generic error",
			err:      errors.New("generic error"),
			expected: false,
		},
		{
			name:     "timeout error",
			err:      context.DeadlineExceeded,
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsRetryableError(tt.err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestRetryWithBackoff_Delay(t *testing.T) {
	ctx := context.Background()
	config := RetryConfig{
		MaxAttempts:  3,
		InitialDelay: 1, // 1 秒
		MaxDelay:     5, // 5 秒
		Multiplier:   2.0,
		Jitter:       false, // 禁用抖动以便测试
	}

	var timestamps []time.Time
	fn := func() error {
		timestamps = append(timestamps, time.Now())
		return &ConnectionError{Reason: "error"}
	}

	_ = RetryWithBackoff(ctx, config, fn)

	require.Len(t, timestamps, 3, "应该有 3 次尝试")

	// 检查延迟
	delay1 := timestamps[1].Sub(timestamps[0])
	delay2 := timestamps[2].Sub(timestamps[1])

	// 第一次延迟应该约为 1s
	assert.GreaterOrEqual(t, delay1, 900*time.Millisecond, "第一次延迟应该约为 1s")
	assert.LessOrEqual(t, delay1, 1100*time.Millisecond, "第一次延迟应该约为 1s")

	// 第二次延迟应该约为 2s（指数退避）
	assert.GreaterOrEqual(t, delay2, 1900*time.Millisecond, "第二次延迟应该约为 2s")
	assert.LessOrEqual(t, delay2, 2100*time.Millisecond, "第二次延迟应该约为 2s")
}

func TestRetryWithBackoff_MaxDelay(t *testing.T) {
	ctx := context.Background()
	config := RetryConfig{
		MaxAttempts:  5,
		InitialDelay: 1,    // 1 秒
		MaxDelay:     2,    // 2 秒
		Multiplier:   10.0, // 大倍增因子以快速达到最大延迟
		Jitter:       false,
	}

	var timestamps []time.Time
	fn := func() error {
		timestamps = append(timestamps, time.Now())
		return &ConnectionError{Reason: "error"}
	}

	_ = RetryWithBackoff(ctx, config, fn)

	require.Len(t, timestamps, 5, "应该有 5 次尝试")

	// 所有后续延迟都不应超过 MaxDelay (2s)
	for i := 1; i < len(timestamps); i++ {
		delay := timestamps[i].Sub(timestamps[i-1])
		assert.LessOrEqual(t, delay, 2100*time.Millisecond, "延迟不应超过 MaxDelay")
	}
}

func TestCustomErrors(t *testing.T) {
	t.Run("ToolNotFoundError", func(t *testing.T) {
		err := &ToolNotFoundError{ToolName: "test_tool"}
		assert.Equal(t, "tool 'test_tool' not found", err.Error())
	})

	t.Run("ConnectionError", func(t *testing.T) {
		innerErr := errors.New("inner error")
		err := &ConnectionError{Reason: "test reason", Err: innerErr}
		assert.Equal(t, "connection failed: test reason: inner error", err.Error())
		assert.Equal(t, innerErr, err.Unwrap())
	})

	t.Run("AuthenticationError", func(t *testing.T) {
		err := &AuthenticationError{Reason: "invalid token"}
		assert.Equal(t, "authentication failed: invalid token", err.Error())
	})

	t.Run("ToolExecutionError", func(t *testing.T) {
		innerErr := errors.New("inner error")
		err := &ToolExecutionError{ToolName: "test_tool", Reason: "execution failed", Err: innerErr}
		assert.Equal(t, "tool 'test_tool' execution failed: execution failed: inner error", err.Error())
		assert.Equal(t, innerErr, err.Unwrap())
	})
}
