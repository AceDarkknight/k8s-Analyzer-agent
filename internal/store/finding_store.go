package store

import (
	"context"
	"time"
)

// FindingStore Finding 去重存储接口
type FindingStore interface {
	// HasFinding 检查是否已存在某个 Finding
	HasFinding(ctx context.Context, key string) (bool, error)
	// SaveFinding 保存 Finding，带 TTL
	SaveFinding(ctx context.Context, key string, ttl time.Duration) error
	// Close 关闭存储
	Close() error
}
