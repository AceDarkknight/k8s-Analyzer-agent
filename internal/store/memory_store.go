package store

import (
	"context"
	"time"

	"github.com/jellydator/ttlcache/v3"
)

// MemoryStore 基于 ttlcache 的内存存储实现
type MemoryStore struct {
	cache *ttlcache.Cache[string, bool]
}

// NewMemoryStore 创建一个新的内存存储实例
func NewMemoryStore() *MemoryStore {
	cache := ttlcache.New[string, bool](
		ttlcache.WithTTL[string, bool](time.Hour), // 默认 TTL，实际使用时会被覆盖
	)

	// 启动自动清理 goroutine
	go cache.Start()

	return &MemoryStore{
		cache: cache,
	}
}

// HasFinding 检查 cache 中是否存在 key
func (s *MemoryStore) HasFinding(ctx context.Context, key string) (bool, error) {
	item := s.cache.Get(key)
	return item != nil, nil
}

// SaveFinding 设置 key=true，带 TTL
func (s *MemoryStore) SaveFinding(ctx context.Context, key string, ttl time.Duration) error {
	s.cache.Set(key, true, ttl)
	return nil
}

// Close 停止 cache
func (s *MemoryStore) Close() error {
	s.cache.Stop()
	return nil
}
