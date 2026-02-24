// Package analysis 提供 Store 单元测试
package analysis

import (
	"context"
	"testing"
	"time"

	"github.com/AceDarkknight/k8s-analyzer-agent/internal/config"
)

// TestMemoryStore_operations 测试 MemoryStore 的基本操作
func TestMemoryStore_operations(t *testing.T) {
	store := NewMemoryStore()
	defer store.Close()

	ctx := context.Background()
	key := "test:key:123"
	ttl := 1 * time.Hour

	// 测试 HasFinding - 初始应该返回 false
	has, err := store.HasFinding(ctx, key)
	if err != nil {
		t.Fatalf("HasFinding failed: %v", err)
	}
	if has {
		t.Fatal("Expected key to not exist initially")
	}

	// 测试 SaveFinding
	err = store.SaveFinding(ctx, key, ttl)
	if err != nil {
		t.Fatalf("SaveFinding failed: %v", err)
	}

	// 测试 HasFinding - 保存后应该返回 true
	has, err = store.HasFinding(ctx, key)
	if err != nil {
		t.Fatalf("HasFinding failed: %v", err)
	}
	if !has {
		t.Fatal("Expected key to exist after saving")
	}

	t.Log("MemoryStore basic operations test passed")
}

// TestMemoryStore_deduplication 测试 MemoryStore 的去重功能
func TestMemoryStore_deduplication(t *testing.T) {
	store := NewMemoryStore()
	defer store.Close()

	ctx := context.Background()
	key := "test:dedup:key"
	ttl := 1 * time.Hour

	// 第一次保存
	err := store.SaveFinding(ctx, key, ttl)
	if err != nil {
		t.Fatalf("First SaveFinding failed: %v", err)
	}

	// 检查存在
	has, err := store.HasFinding(ctx, key)
	if err != nil {
		t.Fatalf("HasFinding failed: %v", err)
	}
	if !has {
		t.Fatal("Expected key to exist after first save")
	}

	// 尝试重复保存（不应该导致错误）
	err = store.SaveFinding(ctx, key, ttl)
	if err != nil {
		t.Fatalf("Second SaveFinding failed: %v", err)
	}

	t.Log("MemoryStore deduplication test passed")
}

// TestMemoryStore_ttl 测试 MemoryStore 的 TTL 功能
func TestMemoryStore_ttl(t *testing.T) {
	store := NewMemoryStore()
	defer store.Close()

	ctx := context.Background()
	key := "test:ttl:key"
	// 使用很短的 TTL
	ttl := 100 * time.Millisecond

	// 保存
	err := store.SaveFinding(ctx, key, ttl)
	if err != nil {
		t.Fatalf("SaveFinding failed: %v", err)
	}

	// 立即检查 - 应该存在
	has, err := store.HasFinding(ctx, key)
	if err != nil {
		t.Fatalf("HasFinding failed: %v", err)
	}
	if !has {
		t.Fatal("Expected key to exist immediately after saving")
	}

	// 等待 TTL 过期
	time.Sleep(200 * time.Millisecond)

	// 再次检查 - 应该不存在（因为 TTL 已过期）
	has, err = store.HasFinding(ctx, key)
	if err != nil {
		t.Fatalf("HasFinding failed: %v", err)
	}
	if has {
		t.Fatal("Expected key to not exist after TTL expired")
	}

	t.Log("MemoryStore TTL test passed")
}

// TestRedisStore_config_validation 测试 Redis 配置验证
func TestRedisStore_config_validation(t *testing.T) {
	// 测试无效配置
	cfg := &config.RedisConfig{
		Host: "",
		Port: 0,
	}

	if cfg.IsValid() {
		t.Fatal("Expected invalid config to return false")
	}

	// 测试有效配置
	cfg2 := &config.RedisConfig{
		Host: "localhost",
		Port: 6379,
	}

	if !cfg2.IsValid() {
		t.Fatal("Expected valid config to return true")
	}

	t.Log("RedisStore config validation test passed")
}

// TestDefaultRedisConfig 测试默认 Redis 配置
func TestDefaultRedisConfig(t *testing.T) {
	cfg := config.DefaultRedisConfig()

	if cfg.Host != "" {
		t.Errorf("Expected empty host, got %s", cfg.Host)
	}
	if cfg.Port != 6379 {
		t.Errorf("Expected port 6379, got %d", cfg.Port)
	}
	if cfg.TTL != 86400 {
		t.Errorf("Expected TTL 86400, got %d", cfg.TTL)
	}

	t.Log("DefaultRedisConfig test passed")
}

// TestFindingStore_interface 测试 FindingStore 接口
// 确保 MemoryStore 实现了 FindingStore 接口
func TestFindingStore_interface(t *testing.T) {
	var _ FindingStore = (*MemoryStore)(nil)
	t.Log("MemoryStore implements FindingStore interface")
}
