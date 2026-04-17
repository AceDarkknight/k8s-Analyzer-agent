package store

import (
	"context"
	"time"
)

// ToolCacheStore 工具调用结果缓存接口
type ToolCacheStore interface {
	// Get 获取缓存的工具调用结果，不存在返回 ("", false, nil)
	Get(ctx context.Context, key string) (string, bool, error)
	// Set 缓存工具调用结果，带 TTL
	Set(ctx context.Context, key string, value string, ttl time.Duration) error
	// Close 关闭存储
	Close() error
}
