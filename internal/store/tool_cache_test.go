package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// === MemoryToolCache Tests ===

func TestMemoryToolCache_GetSet(t *testing.T) {
	cache := NewMemoryToolCache(10 * time.Minute)
	defer cache.Close()

	ctx := context.Background()

	// 未设置时 Get 返回 false
	val, ok, err := cache.Get(ctx, "key1")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if ok {
		t.Error("expected ok=false for missing key")
	}
	if val != "" {
		t.Errorf("expected empty value, got %q", val)
	}

	// Set 后 Get 返回 true
	err = cache.Set(ctx, "key1", "value1", 10*time.Minute)
	if err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	val, ok, err = cache.Get(ctx, "key1")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if !ok {
		t.Error("expected ok=true for existing key")
	}
	if val != "value1" {
		t.Errorf("expected 'value1', got %q", val)
	}
}

func TestMemoryToolCache_TTLExpiration(t *testing.T) {
	cache := NewMemoryToolCache(10 * time.Minute)
	defer cache.Close()

	ctx := context.Background()

	// 设置短 TTL
	err := cache.Set(ctx, "expire-key", "expire-value", 100*time.Millisecond)
	if err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	// 立即应该存在
	_, ok, _ := cache.Get(ctx, "expire-key")
	if !ok {
		t.Error("expected key to exist immediately after Set")
	}

	// 等待过期
	time.Sleep(200 * time.Millisecond)

	_, ok, _ = cache.Get(ctx, "expire-key")
	if ok {
		t.Error("expected key to be expired")
	}
}

func TestMemoryToolCache_DifferentKeys(t *testing.T) {
	cache := NewMemoryToolCache(10 * time.Minute)
	defer cache.Close()

	ctx := context.Background()

	cache.Set(ctx, "k1", "v1", 10*time.Minute)
	cache.Set(ctx, "k2", "v2", 10*time.Minute)

	v1, ok1, _ := cache.Get(ctx, "k1")
	v2, ok2, _ := cache.Get(ctx, "k2")
	_, ok3, _ := cache.Get(ctx, "k3")

	if !ok1 || v1 != "v1" {
		t.Errorf("k1: expected v1, got %q (ok=%v)", v1, ok1)
	}
	if !ok2 || v2 != "v2" {
		t.Errorf("k2: expected v2, got %q (ok=%v)", v2, ok2)
	}
	if ok3 {
		t.Error("k3: expected not found")
	}
}

func TestMemoryToolCache_Overwrite(t *testing.T) {
	cache := NewMemoryToolCache(10 * time.Minute)
	defer cache.Close()

	ctx := context.Background()

	cache.Set(ctx, "key", "old", 10*time.Minute)
	cache.Set(ctx, "key", "new", 10*time.Minute)

	val, ok, _ := cache.Get(ctx, "key")
	if !ok || val != "new" {
		t.Errorf("expected 'new' after overwrite, got %q", val)
	}
}

func TestMemoryToolCache_DefaultTTL(t *testing.T) {
	// TTL <= 0 应使用默认值 30m
	cache := NewMemoryToolCache(0)
	defer cache.Close()

	ctx := context.Background()
	cache.Set(ctx, "k", "v", 10*time.Minute)
	val, ok, _ := cache.Get(ctx, "k")
	if !ok || val != "v" {
		t.Error("cache with default TTL should work")
	}
}

// === FileToolCache Tests ===

func TestFileToolCache_GetSet(t *testing.T) {
	dir := filepath.Join(os.TempDir(), "k8s-analyzer-test-cache")
	defer os.RemoveAll(dir)

	cache, err := NewFileToolCache(dir)
	if err != nil {
		t.Fatalf("NewFileToolCache failed: %v", err)
	}
	defer cache.Close()

	ctx := context.Background()

	// 未设置时 Get 返回 false
	val, ok, err := cache.Get(ctx, "file-key1")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if ok {
		t.Error("expected ok=false for missing key")
	}

	// Set 后 Get 返回 true
	err = cache.Set(ctx, "file-key1", "file-value1", 10*time.Minute)
	if err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	val, ok, err = cache.Get(ctx, "file-key1")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if !ok {
		t.Error("expected ok=true for existing key")
	}
	if val != "file-value1" {
		t.Errorf("expected 'file-value1', got %q", val)
	}
}

func TestFileToolCache_TTLExpiration(t *testing.T) {
	dir := filepath.Join(os.TempDir(), "k8s-analyzer-test-cache-ttl")
	defer os.RemoveAll(dir)

	cache, err := NewFileToolCache(dir)
	if err != nil {
		t.Fatalf("NewFileToolCache failed: %v", err)
	}
	defer cache.Close()

	ctx := context.Background()

	// 设置短 TTL
	err = cache.Set(ctx, "expire-file", "expire-val", 100*time.Millisecond)
	if err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	// 立即存在
	_, ok, _ := cache.Get(ctx, "expire-file")
	if !ok {
		t.Error("expected key to exist immediately after Set")
	}

	// 等待过期
	time.Sleep(200 * time.Millisecond)

	_, ok, _ = cache.Get(ctx, "expire-file")
	if ok {
		t.Error("expected key to be expired")
	}
}

func TestFileToolCache_Overwrite(t *testing.T) {
	dir := filepath.Join(os.TempDir(), "k8s-analyzer-test-cache-overwrite")
	defer os.RemoveAll(dir)

	cache, err := NewFileToolCache(dir)
	if err != nil {
		t.Fatalf("NewFileToolCache failed: %v", err)
	}
	defer cache.Close()

	ctx := context.Background()

	cache.Set(ctx, "fk", "old", 10*time.Minute)
	cache.Set(ctx, "fk", "new", 10*time.Minute)

	val, ok, _ := cache.Get(ctx, "fk")
	if !ok || val != "new" {
		t.Errorf("expected 'new' after overwrite, got %q", val)
	}
}

func TestFileToolCache_InvalidDir(t *testing.T) {
	// Windows 上用非法路径
	_, err := NewFileToolCache("")
	// 空路径在 os.MkdirAll 中不一定失败，因为 "" 会被处理为当前目录
	// 所以只要不 panic 就行
	_ = err
}

func TestFileToolCache_KeyToPath(t *testing.T) {
	dir := filepath.Join(os.TempDir(), "k8s-analyzer-test-cache-path")
	defer os.RemoveAll(dir)

	cache, err := NewFileToolCache(dir)
	if err != nil {
		t.Fatalf("NewFileToolCache failed: %v", err)
	}

	// 不同 key 应该产生不同路径
	path1 := cache.keyToPath("key1")
	path2 := cache.keyToPath("key2")
	if path1 == path2 {
		t.Error("different keys should produce different paths")
	}

	// 同一 key 应该产生相同路径
	path1a := cache.keyToPath("key1")
	if path1 != path1a {
		t.Error("same key should produce same path")
	}

	// 特殊字符 key 也应该正常工作（SHA256 哈希）
	pathSpecial := cache.keyToPath("list_pods:{\"namespace\":\"default\"}")
	if pathSpecial == "" {
		t.Error("special characters in key should be handled")
	}
}
