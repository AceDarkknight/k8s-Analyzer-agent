package store

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisToolCache 基于 Redis 的工具缓存实现
type RedisToolCache struct {
	client *redis.Client
	prefix string
}

// NewRedisToolCache 创建 Redis 工具缓存实例
func NewRedisToolCache(host string, port int, password string, db int) (*RedisToolCache, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%d", host, port),
		Password: password,
		DB:       db,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to redis for tool cache: %w", err)
	}

	return &RedisToolCache{
		client: client,
		prefix: "k8s-analyzer:tool-cache:",
	}, nil
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
