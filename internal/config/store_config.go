// Package config 提供存储配置结构
// 支持 Redis 和内存存储两种模式
package config

// RedisConfig 定义 Redis 连接配置
type RedisConfig struct {
	// Host Redis 主机地址
	Host string `json:"host"`

	// Port Redis 端口
	Port int `json:"port"`

	// Password Redis 密码（可选）
	Password string `json:"password"`

	// DB Redis 数据库编号（默认 0）
	DB int `json:"db"`

	// TTL 过期时间（秒），用于 Finding 去重键的 TTL
	// 默认 24 小时
	TTL int `json:"ttl"`
}

// DefaultRedisConfig 返回默认的 Redis 配置
func DefaultRedisConfig() *RedisConfig {
	return &RedisConfig{
		Host:     "",
		Port:     6379,
		Password: "",
		DB:       0,
		TTL:      86400, // 24小时
	}
}

// IsValid 检查 Redis 配置是否有效
func (r *RedisConfig) IsValid() bool {
	return r.Host != "" && r.Port > 0
}
