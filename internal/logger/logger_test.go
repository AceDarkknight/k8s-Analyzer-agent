package logger

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInit(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *LogConfig
		wantErr bool
	}{
		{
			name: "正常初始化",
			cfg: &LogConfig{
				Level:      "debug",
				FilePath:   filepath.Join(os.TempDir(), "test.log"),
				MaxSizeMB:  10,
				MaxBackups: 3,
			},
			wantErr: false,
		},
		{
			name: "使用 nil 配置",
			cfg:  nil,
			wantErr: false,
		},
		{
			name: "使用不同日志级别",
			cfg: &LogConfig{
				Level:      "error",
				FilePath:   filepath.Join(os.TempDir(), "test_error.log"),
				MaxSizeMB:  100,
				MaxBackups: 5,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Init(tt.cfg)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, globalLogger)
			}
			// 清理
			if tt.cfg != nil && tt.cfg.FilePath != "" {
				_ = os.Remove(tt.cfg.FilePath)
			}
		})
	}
}

func TestLogLevels(t *testing.T) {
	// 初始化 logger
	cfg := &LogConfig{
		Level:      "debug",
		FilePath:   filepath.Join(os.TempDir(), "test_levels.log"),
		MaxSizeMB:  10,
		MaxBackups: 3,
	}
	err := Init(cfg)
	require.NoError(t, err)
	defer os.Remove(cfg.FilePath)

	// 测试各级别日志不 panic
	assert.NotPanics(t, func() {
		Debug("debug message", String("key", "value"))
	})
	assert.NotPanics(t, func() {
		Info("info message", Int("count", 42))
	})
	assert.NotPanics(t, func() {
		Warn("warn message", Err(errors.New("test error")))
	})
	assert.NotPanics(t, func() {
		Error("error message", Any("data", map[string]string{"foo": "bar"}))
	})
}

func TestGetLogger(t *testing.T) {
	// 测试未初始化时返回 nil
	globalLogger = nil
	assert.Nil(t, GetLogger())

	// 测试初始化后返回非 nil
	cfg := &LogConfig{
		Level:      "info",
		FilePath:   filepath.Join(os.TempDir(), "test_get.log"),
		MaxSizeMB:  10,
		MaxBackups: 3,
	}
	err := Init(cfg)
	require.NoError(t, err)
	defer os.Remove(cfg.FilePath)

	assert.NotNil(t, GetLogger())
}

func TestFieldHelpers(t *testing.T) {
	// 测试字段辅助函数
	field := String("key", "value")
	assert.Equal(t, "key", field.Key)
	assert.Equal(t, "value", field.String)

	field = Int("count", 42)
	assert.Equal(t, "count", field.Key)
	assert.Equal(t, int64(42), field.Integer)

	testErr := errors.New("test error")
	field = Err(testErr)
	assert.Equal(t, "error", field.Key)
	assert.Equal(t, testErr, field.Interface)

	// Any 函数 - 验证返回的字段有正确的 key
	field = Any("data", "test")
	assert.Equal(t, "data", field.Key)
	// Any 可能使用不同的内部存储方式，这里只验证不 panic 和 key 正确
}

func TestSync(t *testing.T) {
	// 测试未初始化时不报错
	globalLogger = nil
	assert.NoError(t, Sync())

	// 测试初始化后可以正常 sync
	cfg := &LogConfig{
		Level:      "info",
		FilePath:   filepath.Join(os.TempDir(), "test_sync.log"),
		MaxSizeMB:  10,
		MaxBackups: 3,
	}
	err := Init(cfg)
	require.NoError(t, err)
	defer os.Remove(cfg.FilePath)

	// Sync 可能会返回错误，但不应该 panic
	_ = Sync()
}
