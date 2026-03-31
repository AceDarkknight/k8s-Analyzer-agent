package store

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisStore 基于 Redis 的存储实现
type RedisStore struct {
	client *redis.Client
	prefix string // key 前缀，如 "k8s-analyzer:finding:"
}

// NewRedisStore 创建一个新的 Redis 存储实例
func NewRedisStore(host string, port int, password string, db int) (*RedisStore, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%d", host, port),
		Password: password,
		DB:       db,
	})

	// 测试连接
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to redis: %w", err)
	}

	return &RedisStore{
		client: client,
		prefix: "k8s-analyzer:finding:",
	}, nil
}

// HasFinding 使用 EXISTS 命令检查 key 是否存在
func (s *RedisStore) HasFinding(ctx context.Context, key string) (bool, error) {
	fullKey := s.prefix + key
	exists, err := s.client.Exists(ctx, fullKey).Result()
	if err != nil {
		return false, fmt.Errorf("failed to check key existence: %w", err)
	}
	return exists > 0, nil
}

// SaveFinding 使用 SET 命令带 TTL 保存 key
func (s *RedisStore) SaveFinding(ctx context.Context, key string, ttl time.Duration) error {
	fullKey := s.prefix + key
	err := s.client.Set(ctx, fullKey, true, ttl).Err()
	if err != nil {
		return fmt.Errorf("failed to save key: %w", err)
	}
	return nil
}

// Close 关闭 Redis 客户端
func (s *RedisStore) Close() error {
	return s.client.Close()
}
