// Package analysis 提供 Finding 存储接口和实现
// 用于问题去重和跨周期持久化
package analysis

import (
	"context"
	"fmt"
	"time"

	"github.com/AceDarkknight/k8s-analyzer-agent/internal/config"
	"github.com/AceDarkknight/k8s-analyzer-agent/internal/logger"
	"github.com/jellydator/ttlcache/v3"
	"github.com/redis/go-redis/v9"
)

// FindingStore 定义 Finding 存储接口
// 用于管理发现结果的去重和持久化
type FindingStore interface {
	// HasFinding 检查是否已存在相同的 Finding
	// key: 唯一标识符 (例如 "cluster:ns:pod:issue_type")
	HasFinding(ctx context.Context, key string) (bool, error)

	// SaveFinding 保存 Finding 记录
	// key: 唯一标识符
	// ttl: 过期时间
	SaveFinding(ctx context.Context, key string, ttl time.Duration) error

	// Close 关闭存储连接
	Close() error
}

// MemoryStore 基于内存的 Finding 存储实现
// 使用 ttlcache/v3 实现，支持 TTL 过期
type MemoryStore struct {
	cache *ttlcache.Cache[string, bool]
}

// NewMemoryStore 创建新的 MemoryStore
func NewMemoryStore() *MemoryStore {
	// 创建 TTL 缓存，默认 TTL 为 1 小时
	cache := ttlcache.New(
		ttlcache.WithTTL[string, bool](time.Hour),
		ttlcache.WithCapacity[string, bool](10000), // 最多缓存 10000 条
	)

	// 启动后台清理 goroutine
	go cache.Start()

	return &MemoryStore{
		cache: cache,
	}
}

// HasFinding 检查是否已存在相同的 Finding
func (s *MemoryStore) HasFinding(ctx context.Context, key string) (bool, error) {
	item := s.cache.Get(key)
	if item != nil {
		return true, nil
	}
	return false, nil
}

// SaveFinding 保存 Finding 记录
func (s *MemoryStore) SaveFinding(ctx context.Context, key string, ttl time.Duration) error {
	// 设置 TTL
	s.cache.Set(key, true, ttl)
	logger.Debug("[MemoryStore] Saved finding", logger.String("key", key), logger.String("ttl", ttl.String()))
	return nil
}

// Close 关闭存储连接
func (s *MemoryStore) Close() error {
	s.cache.Stop()
	return nil
}

// RedisStore 基于 Redis 的 Finding 存储实现
type RedisStore struct {
	client *redis.Client
	ttl    time.Duration
}

// NewRedisStore 创建新的 RedisStore
func NewRedisStore(cfg *config.RedisConfig) (*RedisStore, error) {
	// 设置默认 TTL
	ttl := time.Duration(cfg.TTL) * time.Second
	if ttl == 0 {
		ttl = 24 * time.Hour
	}

	client := redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		Password: cfg.Password,
		DB:       cfg.DB,
	})

	// 测试连接
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		logger.Error("[RedisStore] Failed to connect to Redis", logger.Err(err))
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	logger.Info("[RedisStore] Connected to Redis",
		logger.String("host", cfg.Host),
		logger.Int("port", cfg.Port))

	return &RedisStore{
		client: client,
		ttl:    ttl,
	}, nil
}

// HasFinding 检查是否已存在相同的 Finding
func (s *RedisStore) HasFinding(ctx context.Context, key string) (bool, error) {
	result, err := s.client.Exists(ctx, key).Result()
	if err != nil {
		logger.Error("[RedisStore] Failed to check finding", logger.Err(err), logger.String("key", key))
		return false, err
	}
	return result > 0, nil
}

// SaveFinding 保存 Finding 记录
func (s *RedisStore) SaveFinding(ctx context.Context, key string, ttl time.Duration) error {
	// 使用 SETEX 设置过期时间
	err := s.client.SetEx(ctx, key, "1", ttl).Err()
	if err != nil {
		logger.Error("[RedisStore] Failed to save finding", logger.Err(err), logger.String("key", key))
		return err
	}
	logger.Debug("[RedisStore] Saved finding", logger.String("key", key), logger.String("ttl", ttl.String()))
	return nil
}

// Close 关闭存储连接
func (s *RedisStore) Close() error {
	return s.client.Close()
}
