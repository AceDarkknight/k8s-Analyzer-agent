package logger

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Level 定义日志级别类型
type Level string

const (
	// DebugLevel 调试级别
	DebugLevel Level = "debug"
	// InfoLevel 信息级别
	InfoLevel Level = "info"
	// WarnLevel 警告级别
	WarnLevel Level = "warn"
	// ErrorLevel 错误级别
	ErrorLevel Level = "error"
	// FatalLevel 致命错误级别
	FatalLevel Level = "fatal"
)

// Config 定义日志配置
type Config struct {
	// Level 日志级别，默认为 debug
	Level Level
	// Filename 日志文件路径，默认为 logs/app.log
	Filename string
	// MaxSize 单个日志文件最大尺寸（MB），默认为 100MB
	MaxSize int
	// MaxBackups 保留旧日志文件的最大个数，默认为 30
	MaxBackups int
	// MaxAge 保留旧日志文件的最大天数，默认为 0（不限制）
	MaxAge int
	// Compress 是否压缩旧日志文件，默认为 false
	Compress bool
	// LocalTime 是否使用本地时间创建备份文件名，默认为 true
	LocalTime bool
	// OutputToConsole 是否同时输出到控制台，默认为 true
	OutputToConsole bool
}

// NewDefaultConfig 创建默认配置
func NewDefaultConfig() *Config {
	return &Config{
		Level:           DebugLevel,
		Filename:        "logs/app.log",
		MaxSize:         100, // 100MB
		MaxBackups:      30,
		MaxAge:          0,
		Compress:        false,
		LocalTime:       true,
		OutputToConsole: true,
	}
}

// Validate 验证配置的有效性
func (c *Config) Validate() error {
	// 验证日志级别
	if !isValidLevel(c.Level) {
		return fmt.Errorf("invalid log level: %s", c.Level)
	}

	// 验证文件名
	if c.Filename == "" {
		return fmt.Errorf("log filename cannot be empty")
	}

	// 验证 MaxSize
	if c.MaxSize <= 0 {
		return fmt.Errorf("MaxSize must be positive")
	}

	// 验证 MaxBackups
	if c.MaxBackups < 0 {
		return fmt.Errorf("MaxBackups cannot be negative")
	}

	// 验证 MaxAge
	if c.MaxAge < 0 {
		return fmt.Errorf("MaxAge cannot be negative")
	}

	return nil
}

// EnsureLogDir 确保日志目录存在
func (c *Config) EnsureLogDir() error {
	dir := filepath.Dir(c.Filename)
	if dir == "" || dir == "." {
		return nil
	}

	// 创建目录（如果不存在）
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create log directory: %w", err)
	}

	return nil
}

// isValidLevel 检查日志级别是否有效
func isValidLevel(level Level) bool {
	switch level {
	case DebugLevel, InfoLevel, WarnLevel, ErrorLevel, FatalLevel:
		return true
	default:
		return false
	}
}

// String 返回日志级别的字符串表示
func (l Level) String() string {
	return string(l)
}

// ParseLevel 从字符串解析日志级别
func ParseLevel(level string) (Level, error) {
	l := Level(strings.ToLower(level))
	if !isValidLevel(l) {
		return "", fmt.Errorf("invalid log level: %s", level)
	}
	return l, nil
}
