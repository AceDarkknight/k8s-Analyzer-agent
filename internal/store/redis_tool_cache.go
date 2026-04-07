package store

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisToolCache 基于 Redis 的工具缓存实现
type RedisToolCache struct {
	client *redis.Client
	prefix string
}

// NewRedisToolCache 创建 Redis 工具缓存实例，复用已有的 redis.Client
func NewRedisToolCache(client *redis.Client) *RedisToolCache {
	return &RedisToolCache{
		client: client,
		prefix: "k8s-analyzer:tool-cache:",
	}
}

// Get 获取缓存
func (c *RedisToolCache) Get(ctx context.Context, key string) (string, bool, error) {
	val, err := c.client.Get(ctx, c.prefix+key).Result()
	if err == redis.Nil {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return val, true, nil
}

// Set 设置缓存
func (c *RedisToolCache) Set(ctx context.Context, key string, value string, ttl time.Duration) error {
	return c.client.Set(ctx, c.prefix+key, value, ttl).Err()
}

// Close redis.Client 生命周期由外部管理
func (c *RedisToolCache) Close() error {
	return nil
}
