package logger

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"go.uber.org/zap/zapcore"
)

// TestNewDefaultConfig 测试默认配置
func TestNewDefaultConfig(t *testing.T) {
	cfg := NewDefaultConfig()

	if cfg.Level != DebugLevel {
		t.Errorf("Expected default level to be DebugLevel, got %v", cfg.Level)
	}

	if cfg.Filename != "logs/app.log" {
		t.Errorf("Expected default filename to be 'logs/app.log', got %v", cfg.Filename)
	}

	if cfg.MaxSize != 100 {
		t.Errorf("Expected default MaxSize to be 100, got %v", cfg.MaxSize)
	}

	if cfg.MaxBackups != 30 {
		t.Errorf("Expected default MaxBackups to be 30, got %v", cfg.MaxBackups)
	}

	if cfg.OutputToConsole != true {
		t.Errorf("Expected default OutputToConsole to be true, got %v", cfg.OutputToConsole)
	}
}

// TestConfigValidate 测试配置验证
func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *Config
		wantErr bool
	}{
		{
			name:    "Valid default config",
			cfg:     NewDefaultConfig(),
			wantErr: false,
		},
		{
			name: "Invalid level",
			cfg: &Config{
				Level:    Level("invalid"),
				Filename: "test.log",
				MaxSize:  100,
			},
			wantErr: true,
		},
		{
			name: "Empty filename",
			cfg: &Config{
				Level:    InfoLevel,
				Filename: "",
				MaxSize:  100,
			},
			wantErr: true,
		},
		{
			name: "Negative MaxSize",
			cfg: &Config{
				Level:    InfoLevel,
				Filename: "test.log",
				MaxSize:  -1,
			},
			wantErr: true,
		},
		{
			name: "Negative MaxBackups",
			cfg: &Config{
				Level:      InfoLevel,
				Filename:   "test.log",
				MaxSize:    100,
				MaxBackups: -1,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestEnsureLogDir 测试日志目录创建
func TestEnsureLogDir(t *testing.T) {
	// 创建临时目录
	tmpDir := t.TempDir()

	tests := []struct {
		name    string
		cfg     *Config
		wantErr bool
	}{
		{
			name: "Create nested directory",
			cfg: &Config{
				Filename: filepath.Join(tmpDir, "logs", "nested", "app.log"),
			},
			wantErr: false,
		},
		{
			name: "Current directory",
			cfg: &Config{
				Filename: "app.log",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.EnsureLogDir()
			if (err != nil) != tt.wantErr {
				t.Errorf("EnsureLogDir() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr {
				// 检查目录是否创建
				dir := filepath.Dir(tt.cfg.Filename)
				if dir != "." {
					if _, err := os.Stat(dir); os.IsNotExist(err) {
						t.Errorf("Directory was not created: %s", dir)
					}
				}
			}
		})
	}
}

// TestParseLevel 测试日志级别解析
func TestParseLevel(t *testing.T) {
	tests := []struct {
		input    string
		expected Level
		wantErr  bool
	}{
		{"debug", DebugLevel, false},
		{"info", InfoLevel, false},
		{"warn", WarnLevel, false},
		{"error", ErrorLevel, false},
		{"fatal", FatalLevel, false},
		{"DEBUG", DebugLevel, false}, // 大写也应该可以
		{"INFO", InfoLevel, false},
		{"invalid", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			level, err := ParseLevel(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseLevel() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && level != tt.expected {
				t.Errorf("ParseLevel() = %v, want %v", level, tt.expected)
			}
		})
	}
}

// TestInit 测试日志初始化
func TestInit(t *testing.T) {
	// 重置全局 logger（仅用于测试）
	globalLogger = nil
	sugarLogger = nil
	once = sync.Once{}

	// 使用临时目录
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "test.log")

	// 初始化日志
	cfg := &Config{
		Level:           DebugLevel,
		Filename:        logFile,
		MaxSize:         10,
		MaxBackups:      3,
		OutputToConsole: false, // 测试时不输出到控制台
	}

	err := InitWithConfig(cfg)
	if err != nil {
		t.Fatalf("InitWithConfig() error = %v", err)
	}

	// 测试日志记录
	logger := GetLogger()
	logger.Info("test message", String("key", "value"))

	// 测试 SugaredLogger
	sugar := GetSugar()
	sugar.Infof("formatted message: %s", "test")

	// 检查日志文件是否存在
	if _, err := os.Stat(logFile); os.IsNotExist(err) {
		t.Errorf("Log file was not created: %s", logFile)
	}

	// 测试 Sync
	err = Sync()
	if err != nil {
		t.Errorf("Sync() error = %v", err)
	}
}

// TestInitWithInvalidConfig 测试使用无效配置初始化
func TestInitWithInvalidConfig(t *testing.T) {
	// 重置全局 logger
	globalLogger = nil
	sugarLogger = nil
	once = sync.Once{}

	cfg := &Config{
		Level:    Level("invalid"),
		Filename: "test.log",
	}

	err := InitWithConfig(cfg)
	if err == nil {
		t.Error("Expected error for invalid config, got nil")
	}
}

// TestConcurrentLogging 测试并发日志记录
func TestConcurrentLogging(t *testing.T) {
	// 重置全局 logger
	globalLogger = nil
	sugarLogger = nil
	once = sync.Once{}

	// 使用临时目录
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "concurrent.log")

	cfg := &Config{
		Level:           DebugLevel,
		Filename:        logFile,
		MaxSize:         10,
		MaxBackups:      3,
		OutputToConsole: false,
	}

	err := InitWithConfig(cfg)
	if err != nil {
		t.Fatalf("InitWithConfig() error = %v", err)
	}

	// 并发写入日志
	done := make(chan bool)
	for i := 0; i < 100; i++ {
		go func(id int) {
			logger := GetLogger()
			logger.Info("concurrent message", Int("id", id))
			done <- true
		}(i)
	}

	// 等待所有 goroutine 完成
	for i := 0; i < 100; i++ {
		<-done
	}

	// 确保没有 panic
	_ = Sync()
}

// TestNewWriter 测试 NewWriter
func TestNewWriter(t *testing.T) {
	// 重置全局 logger
	globalLogger = nil
	sugarLogger = nil
	once = sync.Once{}

	// 使用临时目录
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "writer.log")

	cfg := &Config{
		Level:           DebugLevel,
		Filename:        logFile,
		MaxSize:         10,
		MaxBackups:      3,
		OutputToConsole: false,
	}

	err := InitWithConfig(cfg)
	if err != nil {
		t.Fatalf("InitWithConfig() error = %v", err)
	}

	// 创建 writer
	writer := NewWriter(zapcore.InfoLevel)
	_, err = writer.Write([]byte("test message via writer\n"))
	if err != nil {
		t.Errorf("Writer.Write() error = %v", err)
	}

	_ = Sync()
}

// TestLogLevels 测试不同日志级别
func TestLogLevels(t *testing.T) {
	// 重置全局 logger
	globalLogger = nil
	sugarLogger = nil
	once = sync.Once{}

	// 使用临时目录
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "levels.log")

	cfg := &Config{
		Level:           DebugLevel,
		Filename:        logFile,
		MaxSize:         10,
		MaxBackups:      3,
		OutputToConsole: false,
	}

	err := InitWithConfig(cfg)
	if err != nil {
		t.Fatalf("InitWithConfig() error = %v", err)
	}

	logger := GetLogger()

	// 测试各个级别
	logger.Debug("debug message")
	logger.Info("info message")
	logger.Warn("warn message")
	logger.Error("error message")

	// Fatal 会调用 os.Exit，所以不能直接测试
	// logger.Fatal("fatal message")

	_ = Sync()
}

// TestStructuredLogging 测试结构化日志
func TestStructuredLogging(t *testing.T) {
	// 重置全局 logger
	globalLogger = nil
	sugarLogger = nil
	once = sync.Once{}

	// 使用临时目录
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "structured.log")

	cfg := &Config{
		Level:           DebugLevel,
		Filename:        logFile,
		MaxSize:         10,
		MaxBackups:      3,
		OutputToConsole: false,
	}

	err := InitWithConfig(cfg)
	if err != nil {
		t.Fatalf("InitWithConfig() error = %v", err)
	}

	logger := GetLogger()

	// 测试结构化字段
	logger.Info("structured message",
		String("user", "test"),
		Int("count", 42),
		Bool("success", true),
	)

	// 测试 With
	childLogger := logger.With(String("service", "test-service"))
	childLogger.Info("child logger message")

	_ = Sync()
}

// TestSugaredLogger 测试 SugaredLogger
func TestSugaredLogger(t *testing.T) {
	// 重置全局 logger
	globalLogger = nil
	sugarLogger = nil
	once = sync.Once{}

	// 使用临时目录
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "sugar.log")

	cfg := &Config{
		Level:           DebugLevel,
		Filename:        logFile,
		MaxSize:         10,
		MaxBackups:      3,
		OutputToConsole: false,
	}

	err := InitWithConfig(cfg)
	if err != nil {
		t.Fatalf("InitWithConfig() error = %v", err)
	}

	sugar := GetSugar()

	// 测试格式化日志
	sugar.Debugf("debug message: %s", "test")
	sugar.Infof("info message: %d", 123)
	sugar.Warnf("warn message: %v", map[string]int{"a": 1})
	sugar.Errorf("error message: %s", "error")

	// 测试结构化日志
	sugar.Debugw("debug structured", "key", "value", "count", 42)
	sugar.Infow("info structured", "user", "test", "success", true)

	_ = Sync()
}
