package store

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// FileToolCache 基于本地文件系统的工具缓存实现
type FileToolCache struct {
	dir string
}

// NewFileToolCache 创建文件工具缓存实例
func NewFileToolCache(dir string) (*FileToolCache, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create cache dir: %w", err)
	}
	return &FileToolCache{dir: dir}, nil
}

// keyToPath 将 key 转为文件路径（SHA256 哈希避免非法字符）
func (c *FileToolCache) keyToPath(key string) string {
	h := sha256.Sum256([]byte(key))
	return filepath.Join(c.dir, hex.EncodeToString(h[:])+".cache")
}

// Get 获取缓存
func (c *FileToolCache) Get(ctx context.Context, key string) (string, bool, error) {
	path := c.keyToPath(key)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if len(data) < 8 {
		return "", false, nil
	}

	expireAt := int64(binary.BigEndian.Uint64(data[:8]))
	if time.Now().UnixNano() > expireAt {
		_ = os.Remove(path)
		return "", false, nil
	}
	return string(data[8:]), true, nil
}

// Set 设置缓存（前 8 字节存过期时间戳）
func (c *FileToolCache) Set(ctx context.Context, key string, value string, ttl time.Duration) error {
	path := c.keyToPath(key)
	expireAt := time.Now().Add(ttl).UnixNano()

	buf := make([]byte, 8+len(value))
	binary.BigEndian.PutUint64(buf[:8], uint64(expireAt))
	copy(buf[8:], value)

	return os.WriteFile(path, buf, 0644)
}

// Close 文件缓存无需特殊清理
func (c *FileToolCache) Close() error {
	return nil
}
