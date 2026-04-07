package store

import (
	"context"
	"time"

	"github.com/jellydator/ttlcache/v3"
)

// MemoryToolCache 基于 ttlcache 的内存工具缓存实现
type MemoryToolCache struct {
	cache *ttlcache.Cache[string, string]
}

// NewMemoryToolCache 创建内存工具缓存实例
func NewMemoryToolCache(defaultTTL time.Duration) *MemoryToolCache {
	if defaultTTL <= 0 {
		defaultTTL = 30 * time.Minute
	}
	cache := ttlcache.New[string, string](
		ttlcache.WithTTL[string, string](defaultTTL),
	)
	go cache.Start()
	return &MemoryToolCache{cache: cache}
}

// Get 获取缓存
func (c *MemoryToolCache) Get(ctx context.Context, key string) (string, bool, error) {
	item := c.cache.Get(key)
	if item == nil {
		return "", false, nil
	}
	return item.Value(), true, nil
}

// Set 设置缓存
func (c *MemoryToolCache) Set(ctx context.Context, key string, value string, ttl time.Duration) error {
	c.cache.Set(key, value, ttl)
	return nil
}

// Close 停止缓存
func (c *MemoryToolCache) Close() error {
	c.cache.Stop()
	return nil
}
