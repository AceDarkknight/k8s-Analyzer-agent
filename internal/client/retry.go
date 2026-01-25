// Package client 提供重试机制实现
package client

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"time"
)

// RetryConfig 定义重试配置
type RetryConfig struct {
	// MaxAttempts 最大重试次数（包括首次尝试）
	MaxAttempts int

	// InitialDelay 初始延迟时间
	InitialDelay time.Duration

	// MaxDelay 最大延迟时间
	MaxDelay time.Duration

	// Multiplier 延迟倍增因子（指数退避）
	Multiplier float64

	// Jitter 是否添加随机抖动
	Jitter bool
}

// DefaultRetryConfig 返回默认的重试配置
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts:  3, // 最大重试 3 次
		InitialDelay: 1 * time.Second,
		MaxDelay:     10 * time.Second,
		Multiplier:   2.0,  // 指数增长
		Jitter:       true, // 添加随机抖动避免惊群效应
	}
}

// RetryFunc 定义需要重试的函数类型
type RetryFunc func() error

// RetryWithBackoff 使用指数退避策略执行重试
func RetryWithBackoff(ctx context.Context, config RetryConfig, fn RetryFunc) error {
	if config.MaxAttempts <= 0 {
		config.MaxAttempts = DefaultRetryConfig().MaxAttempts
	}
	if config.InitialDelay <= 0 {
		config.InitialDelay = DefaultRetryConfig().InitialDelay
	}
	if config.Multiplier <= 0 {
		config.Multiplier = DefaultRetryConfig().Multiplier
	}

	var lastErr error
	delay := config.InitialDelay

	for attempt := 0; attempt < config.MaxAttempts; attempt++ {
		// 执行函数
		err := fn()
		if err == nil {
			// 成功，直接返回
			return nil
		}

		// 记录最后一次错误
		lastErr = err

		// 检查是否为可重试的错误
		if !IsRetryableError(err) {
			// 不可重试，直接返回错误
			return fmt.Errorf("non-retryable error on attempt %d: %w", attempt+1, err)
		}

		// 如果是最后一次尝试，不再等待
		if attempt == config.MaxAttempts-1 {
			break
		}

		// 计算延迟时间
		waitTime := delay

		// 添加随机抖动
		if config.Jitter {
			// 抖动范围：±25%
			jitter := time.Duration(float64(waitTime) * 0.25 * (rand.Float64()*2 - 1))
			waitTime += jitter
		}

		// 限制最大延迟
		if waitTime > config.MaxDelay {
			waitTime = config.MaxDelay
		}

		// 确保等待时间为正数
		if waitTime < 0 {
			waitTime = delay
		}

		// 等待或检查上下文取消
		select {
		case <-ctx.Done():
			return fmt.Errorf("retry cancelled: %w", ctx.Err())
		case <-time.After(waitTime):
			// 继续下一次重试
		}

		// 计算下一次的延迟时间（指数退避）
		delay = time.Duration(math.Min(
			float64(delay)*config.Multiplier,
			float64(config.MaxDelay),
		))
	}

	// 所有重试都失败
	return fmt.Errorf("failed after %d attempts: %w", config.MaxAttempts, lastErr)
}

// RetryWithContext 使用指数退避策略执行带上下文的函数
func RetryWithContext(ctx context.Context, config RetryConfig, fn func(context.Context) error) error {
	if config.MaxAttempts <= 0 {
		config.MaxAttempts = DefaultRetryConfig().MaxAttempts
	}
	if config.InitialDelay <= 0 {
		config.InitialDelay = DefaultRetryConfig().InitialDelay
	}
	if config.Multiplier <= 0 {
		config.Multiplier = DefaultRetryConfig().Multiplier
	}

	var lastErr error
	delay := config.InitialDelay

	for attempt := 0; attempt < config.MaxAttempts; attempt++ {
		// 执行函数
		err := fn(ctx)
		if err == nil {
			// 成功，直接返回
			return nil
		}

		// 记录最后一次错误
		lastErr = err

		// 检查是否为可重试的错误
		if !IsRetryableError(err) {
			// 不可重试，直接返回错误
			return fmt.Errorf("non-retryable error on attempt %d: %w", attempt+1, err)
		}

		// 如果是最后一次尝试，不再等待
		if attempt == config.MaxAttempts-1 {
			break
		}

		// 计算延迟时间
		waitTime := delay

		// 添加随机抖动
		if config.Jitter {
			jitter := time.Duration(float64(waitTime) * 0.25 * (rand.Float64()*2 - 1))
			waitTime += jitter
		}

		// 限制最大延迟
		if waitTime > config.MaxDelay {
			waitTime = config.MaxDelay
		}

		// 确保等待时间为正数
		if waitTime < 0 {
			waitTime = delay
		}

		// 等待或检查上下文取消
		select {
		case <-ctx.Done():
			return fmt.Errorf("retry cancelled: %w", ctx.Err())
		case <-time.After(waitTime):
			// 继续下一次重试
		}

		// 计算下一次的延迟时间（指数退避）
		delay = time.Duration(math.Min(
			float64(delay)*config.Multiplier,
			float64(config.MaxDelay),
		))
	}

	// 所有重试都失败
	return fmt.Errorf("failed after %d attempts: %w", config.MaxAttempts, lastErr)
}

// RetryWithResult 使用指数退避策略执行带返回结果的函数
func RetryWithResult[T any](ctx context.Context, config RetryConfig, fn func() (T, error)) (T, error) {
	var zero T

	if config.MaxAttempts <= 0 {
		config.MaxAttempts = DefaultRetryConfig().MaxAttempts
	}
	if config.InitialDelay <= 0 {
		config.InitialDelay = DefaultRetryConfig().InitialDelay
	}
	if config.Multiplier <= 0 {
		config.Multiplier = DefaultRetryConfig().Multiplier
	}

	var lastResult T
	var lastErr error
	delay := config.InitialDelay

	for attempt := 0; attempt < config.MaxAttempts; attempt++ {
		// 执行函数
		result, err := fn()
		if err == nil {
			// 成功，直接返回结果
			return result, nil
		}

		// 记录最后一次结果和错误
		lastResult = result
		lastErr = err

		// 检查是否为可重试的错误
		if !IsRetryableError(err) {
			// 不可重试，直接返回错误
			return zero, fmt.Errorf("non-retryable error on attempt %d: %w", attempt+1, err)
		}

		// 如果是最后一次尝试，不再等待
		if attempt == config.MaxAttempts-1 {
			break
		}

		// 计算延迟时间
		waitTime := delay

		// 添加随机抖动
		if config.Jitter {
			jitter := time.Duration(float64(waitTime) * 0.25 * (rand.Float64()*2 - 1))
			waitTime += jitter
		}

		// 限制最大延迟
		if waitTime > config.MaxDelay {
			waitTime = config.MaxDelay
		}

		// 确保等待时间为正数
		if waitTime < 0 {
			waitTime = delay
		}

		// 等待或检查上下文取消
		select {
		case <-ctx.Done():
			return zero, fmt.Errorf("retry cancelled: %w", ctx.Err())
		case <-time.After(waitTime):
			// 继续下一次重试
		}

		// 计算下一次的延迟时间（指数退避）
		delay = time.Duration(math.Min(
			float64(delay)*config.Multiplier,
			float64(config.MaxDelay),
		))
	}

	// 所有重试都失败
	return lastResult, fmt.Errorf("failed after %d attempts: %w", config.MaxAttempts, lastErr)
}
