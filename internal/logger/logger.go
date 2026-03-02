package logger

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

// Field 是 zap.Field 的类型别名，用于结构化日志字段
type Field = zap.Field

var (
	globalLogger   *zap.Logger
	sugarLogger    *zap.SugaredLogger
	lumberjackSink *lumberjack.Logger
	once           sync.Once
)

// Init 初始化全局日志实例
// 使用默认配置初始化日志
func Init() error {
	return InitWithConfig(NewDefaultConfig())
}

// InitWithConfig 使用指定配置初始化全局日志实例
// 注意：此函数只能调用一次，多次调用会返回错误
func InitWithConfig(cfg *Config) error {
	var initErr error
	once.Do(func() {
		// 验证配置
		if err := cfg.Validate(); err != nil {
			initErr = fmt.Errorf("invalid config: %w", err)
			return
		}

		// 确保日志目录存在
		if err := cfg.EnsureLogDir(); err != nil {
			initErr = fmt.Errorf("failed to ensure log dir: %w", err)
			return
		}

		// 创建 lumberjack 日志轮转器
		lumberjackSink = &lumberjack.Logger{
			Filename:   cfg.Filename,
			MaxSize:    cfg.MaxSize,
			MaxBackups: cfg.MaxBackups,
			MaxAge:     cfg.MaxAge,
			Compress:   cfg.Compress,
			LocalTime:  cfg.LocalTime,
		}

		// 配置 zap 编码器
		encoderConfig := zapcore.EncoderConfig{
			TimeKey:        "time",
			LevelKey:       "level",
			NameKey:        "logger",
			CallerKey:      "caller",
			FunctionKey:    zapcore.OmitKey,
			MessageKey:     "msg",
			StacktraceKey:  "stacktrace",
			LineEnding:     zapcore.DefaultLineEnding,
			EncodeLevel:    zapcore.LowercaseLevelEncoder,
			EncodeTime:     zapcore.ISO8601TimeEncoder,
			EncodeDuration: zapcore.SecondsDurationEncoder,
			EncodeCaller:   zapcore.ShortCallerEncoder,
		}

		// 解析日志级别
		zapLevel, err := parseZapLevel(cfg.Level)
		if err != nil {
			initErr = fmt.Errorf("failed to parse log level: %w", err)
			return
		}

		// 创建核心
		// 如果配置了输出到控制台，则同时输出到文件和控制台
		var core zapcore.Core
		if cfg.OutputToConsole {
			// 文件输出核心
			fileCore := zapcore.NewCore(
				zapcore.NewJSONEncoder(encoderConfig),
				zapcore.AddSync(lumberjackSink),
				zapLevel,
			)

			// 控制台输出核心
			consoleCore := zapcore.NewCore(
				zapcore.NewConsoleEncoder(encoderConfig),
				zapcore.AddSync(os.Stdout),
				zapLevel,
			)

			// 组合核心（同时输出到文件和控制台）
			core = zapcore.NewTee(fileCore, consoleCore)
		} else {
			// 仅输出到文件
			core = zapcore.NewCore(
				zapcore.NewJSONEncoder(encoderConfig),
				zapcore.AddSync(lumberjackSink),
				zapLevel,
			)
		}

		// 创建 logger
		globalLogger = zap.New(core, zap.AddCaller(), zap.AddCallerSkip(1), zap.AddStacktrace(zapcore.ErrorLevel))
		sugarLogger = globalLogger.Sugar()
	})

	return initErr
}

// GetLogger 获取全局日志实例
// 如果未初始化，会使用默认配置自动初始化
func GetLogger() *zap.Logger {
	if globalLogger == nil {
		_ = Init() // 忽略错误，因为使用默认配置
	}
	return globalLogger
}

// GetSugar 获取全局 SugaredLogger 实例
// SugaredLogger 提供了更友好的 API，支持格式化字符串
func GetSugar() *zap.SugaredLogger {
	if sugarLogger == nil {
		_ = Init() // 忽略错误，因为使用默认配置
	}
	return sugarLogger
}

// Sync 刷新日志缓冲区
// 在程序退出前调用此函数以确保所有日志都被写入
func Sync() error {
	var err error
	if globalLogger != nil {
		err = globalLogger.Sync()
	}
	if sugarLogger != nil {
		err = sugarLogger.Sync()
	}

	if lumberjackSink != nil {
		lumberjackSink.Close()
		lumberjackSink = nil
	}
	return err
}

// parseZapLevel 将自定义 Level 转换为 zapcore.Level
func parseZapLevel(level Level) (zapcore.Level, error) {
	switch level {
	case DebugLevel:
		return zapcore.DebugLevel, nil
	case InfoLevel:
		return zapcore.InfoLevel, nil
	case WarnLevel:
		return zapcore.WarnLevel, nil
	case ErrorLevel:
		return zapcore.ErrorLevel, nil
	case FatalLevel:
		return zapcore.FatalLevel, nil
	default:
		return zapcore.InfoLevel, fmt.Errorf("unknown log level: %s", level)
	}
}

// With 创建一个带有额外字段的子 logger
// 支持结构化日志字段
func With(fields ...zap.Field) *zap.Logger {
	return GetLogger().With(fields...)
}

// Debug 记录调试级别日志
func Debug(msg string, fields ...zap.Field) {
	GetLogger().Debug(msg, fields...)
}

// Info 记录信息级别日志
func Info(msg string, fields ...zap.Field) {
	GetLogger().Info(msg, fields...)
}

// Warn 记录警告级别日志
func Warn(msg string, fields ...zap.Field) {
	GetLogger().Warn(msg, fields...)
}

// Error 记录错误级别日志
func Error(msg string, fields ...zap.Field) {
	GetLogger().Error(msg, fields...)
}

// Fatal 记录致命错误级别日志并退出程序
func Fatal(msg string, fields ...zap.Field) {
	GetLogger().Fatal(msg, fields...)
}

// Debugf 使用格式化字符串记录调试级别日志（SugaredLogger）
func Debugf(template string, args ...interface{}) {
	GetSugar().Debugf(template, args...)
}

// Infof 使用格式化字符串记录信息级别日志（SugaredLogger）
func Infof(template string, args ...interface{}) {
	GetSugar().Infof(template, args...)
}

// Warnf 使用格式化字符串记录警告级别日志（SugaredLogger）
func Warnf(template string, args ...interface{}) {
	GetSugar().Warnf(template, args...)
}

// Errorf 使用格式化字符串记录错误级别日志（SugaredLogger）
func Errorf(template string, args ...interface{}) {
	GetSugar().Errorf(template, args...)
}

// Fatalf 使用格式化字符串记录致命错误级别日志并退出程序（SugaredLogger）
func Fatalf(template string, args ...interface{}) {
	GetSugar().Fatalf(template, args...)
}

// Debugw 使用结构化字段记录调试级别日志（SugaredLogger）
func Debugw(msg string, keyvals ...interface{}) {
	GetSugar().Debugw(msg, keyvals...)
}

// Infow 使用结构化字段记录信息级别日志（SugaredLogger）
func Infow(msg string, keyvals ...interface{}) {
	GetSugar().Infow(msg, keyvals...)
}

// Warnw 使用结构化字段记录警告级别日志（SugaredLogger）
func Warnw(msg string, keyvals ...interface{}) {
	GetSugar().Warnw(msg, keyvals...)
}

// Errorw 使用结构化字段记录错误级别日志（SugaredLogger）
func Errorw(msg string, keyvals ...interface{}) {
	GetSugar().Errorw(msg, keyvals...)
}

// Fatalw 使用结构化字段记录致命错误级别日志并退出程序（SugaredLogger）
func Fatalw(msg string, keyvals ...interface{}) {
	GetSugar().Fatalw(msg, keyvals...)
}

// writerAdapter 将 zap.Logger 适配为 io.Writer
type writerAdapter struct {
	logger *zap.Logger
	level  zapcore.Level
}

// Write 实现 io.Writer 接口
func (w *writerAdapter) Write(p []byte) (n int, err error) {
	msg := string(p)
	// 去除末尾的换行符，zap 会自动添加
	if len(msg) > 0 && msg[len(msg)-1] == '\n' {
		msg = msg[:len(msg)-1]
	}
	w.logger.Log(w.level, msg)
	return len(p), nil
}

// NewWriter 创建一个 io.Writer，用于将标准库日志（如 fmt.Println）重定向到 zap
// level 指定日志级别
func NewWriter(level zapcore.Level) io.Writer {
	return &writerAdapter{
		logger: GetLogger(),
		level:  level,
	}
}

// ========== 字段构造函数 ==========
// 以下函数封装了 zap 的字段构造函数，使其他包无需直接导入 zap

// String 构造字符串类型的日志字段
func String(key, val string) Field {
	return zap.String(key, val)
}

// Int 构造整数类型的日志字段
func Int(key string, val int) Field {
	return zap.Int(key, val)
}

// Int8 构造 int8 类型的日志字段
func Int8(key string, val int8) Field {
	return zap.Int8(key, val)
}

// Int16 构造 int16 类型的日志字段
func Int16(key string, val int16) Field {
	return zap.Int16(key, val)
}

// Int32 构造 int32 类型的日志字段
func Int32(key string, val int32) Field {
	return zap.Int32(key, val)
}

// Int64 构造 int64 类型的日志字段
func Int64(key string, val int64) Field {
	return zap.Int64(key, val)
}

// Uint 构造无符号整数类型的日志字段
func Uint(key string, val uint) Field {
	return zap.Uint(key, val)
}

// Uint8 构造 uint8 类型的日志字段
func Uint8(key string, val uint8) Field {
	return zap.Uint8(key, val)
}

// Uint16 构造 uint16 类型的日志字段
func Uint16(key string, val uint16) Field {
	return zap.Uint16(key, val)
}

// Uint32 构造 uint32 类型的日志字段
func Uint32(key string, val uint32) Field {
	return zap.Uint32(key, val)
}

// Uint64 构造 uint64 类型的日志字段
func Uint64(key string, val uint64) Field {
	return zap.Uint64(key, val)
}

// Float32 构造 float32 类型的日志字段
func Float32(key string, val float32) Field {
	return zap.Float32(key, val)
}

// Float64 构造 float64 类型的日志字段
func Float64(key string, val float64) Field {
	return zap.Float64(key, val)
}

// Bool 构造布尔类型的日志字段
func Bool(key string, val bool) Field {
	return zap.Bool(key, val)
}

// Err 构造错误类型的日志字段
func Err(err error) Field {
	return zap.Error(err)
}

// Any 构造任意类型的日志字段
func Any(key string, val interface{}) Field {
	return zap.Any(key, val)
}

// Object 构造对象类型的日志字段
func Object(key string, val zapcore.ObjectMarshaler) Field {
	return zap.Object(key, val)
}

// Namespace 构造命名空间类型的日志字段
func Namespace(key string) Field {
	return zap.Namespace(key)
}

// Duration 构造时间间隔类型的日志字段
func Duration(key string, val time.Duration) Field {
	return zap.Duration(key, val)
}

// Time 构造时间类型的日志字段
func Time(key string, val time.Time) Field {
	return zap.Time(key, val)
}

// Reflect 构造反射类型的日志字段
func Reflect(key string, val interface{}) Field {
	return zap.Reflect(key, val)
}
