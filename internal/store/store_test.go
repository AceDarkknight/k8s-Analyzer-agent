package store

import (
	"context"
	"testing"
	"time"
)

func TestMemoryStore_SaveAndHasFinding(t *testing.T) {
	store := NewMemoryStore()
	defer store.Close()

	ctx := context.Background()
	key := "test-finding-1"

	// 未保存的 key HasFinding 返回 false
	exists, err := store.HasFinding(ctx, key)
	if err != nil {
		t.Fatalf("HasFinding failed: %v", err)
	}
	if exists {
		t.Errorf("Expected key to not exist, but it does")
	}

	// SaveFinding 后 HasFinding 返回 true
	ttl := time.Hour
	err = store.SaveFinding(ctx, key, ttl)
	if err != nil {
		t.Fatalf("SaveFinding failed: %v", err)
	}

	exists, err = store.HasFinding(ctx, key)
	if err != nil {
		t.Fatalf("HasFinding failed: %v", err)
	}
	if !exists {
		t.Errorf("Expected key to exist, but it doesn't")
	}
}

func TestMemoryStore_TTLExpiration(t *testing.T) {
	store := NewMemoryStore()
	defer store.Close()

	ctx := context.Background()
	key := "test-finding-ttl"

	// 使用短 TTL 保存 key
	ttl := 100 * time.Millisecond
	err := store.SaveFinding(ctx, key, ttl)
	if err != nil {
		t.Fatalf("SaveFinding failed: %v", err)
	}

	// 立即检查，应该存在
	exists, err := store.HasFinding(ctx, key)
	if err != nil {
		t.Fatalf("HasFinding failed: %v", err)
	}
	if !exists {
		t.Errorf("Expected key to exist immediately after save, but it doesn't")
	}

	// 等待 TTL 过期
	time.Sleep(200 * time.Millisecond)

	// TTL 过期后 HasFinding 返回 false
	exists, err = store.HasFinding(ctx, key)
	if err != nil {
		t.Fatalf("HasFinding failed: %v", err)
	}
	if exists {
		t.Errorf("Expected key to be expired, but it still exists")
	}
}

func TestMemoryStore_DifferentKeys(t *testing.T) {
	store := NewMemoryStore()
	defer store.Close()

	ctx := context.Background()
	key1 := "test-finding-1"
	key2 := "test-finding-2"

	// 只保存 key1
	err := store.SaveFinding(ctx, key1, time.Hour)
	if err != nil {
		t.Fatalf("SaveFinding failed: %v", err)
	}

	// key1 应该存在
	exists, err := store.HasFinding(ctx, key1)
	if err != nil {
		t.Fatalf("HasFinding failed: %v", err)
	}
	if !exists {
		t.Errorf("Expected key1 to exist, but it doesn't")
	}

	// key2 不应该存在
	exists, err = store.HasFinding(ctx, key2)
	if err != nil {
		t.Fatalf("HasFinding failed: %v", err)
	}
	if exists {
		t.Errorf("Expected key2 to not exist, but it does")
	}
}
