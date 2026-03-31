package logger

import (
	"os"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

// 全局 logger 实例
var globalLogger *zap.Logger

// Init 初始化全局 logger
func Init(cfg *LogConfig) error {
	if cfg == nil {
		cfg = &LogConfig{
			Level:      "info",
			MaxSizeMB:  100,
			MaxBackups: 3,
		}
	}

	// 设置日志级别
	level := zapcore.InfoLevel
	if err := level.UnmarshalText([]byte(cfg.Level)); err != nil {
		level = zapcore.InfoLevel
	}

	// 编码器配置
	encoderConfig := zapcore.EncoderConfig{
		TimeKey:        "timestamp",
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

	// 创建 lumberjack writer 用于日志轮转
	var writers []zapcore.WriteSyncer

	// 如果配置了文件路径，添加文件输出
	if cfg.FilePath != "" {
		lumberjackLogger := &lumberjack.Logger{
			Filename:   cfg.FilePath,
			MaxSize:    cfg.MaxSizeMB,
			MaxBackups: cfg.MaxBackups,
			Compress:   true,
		}
		writers = append(writers, zapcore.AddSync(lumberjackLogger))
	}

	// 同时输出到控制台
	writers = append(writers, zapcore.AddSync(os.Stdout))

	// 创建多输出 writer
	writeSyncer := zapcore.NewMultiWriteSyncer(writers...)

	// 创建 core
	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(encoderConfig),
		writeSyncer,
		level,
	)

	// 创建 logger
	globalLogger = zap.New(core, zap.AddCaller(), zap.AddStacktrace(zapcore.ErrorLevel))

	return nil
}

// Debug 输出 debug 级别日志
func Debug(msg string, fields ...zap.Field) {
	if globalLogger != nil {
		globalLogger.Debug(msg, fields...)
	}
}

// Info 输出 info 级别日志
func Info(msg string, fields ...zap.Field) {
	if globalLogger != nil {
		globalLogger.Info(msg, fields...)
	}
}

// Warn 输出 warn 级别日志
func Warn(msg string, fields ...zap.Field) {
	if globalLogger != nil {
		globalLogger.Warn(msg, fields...)
	}
}

// Error 输出 error 级别日志
func Error(msg string, fields ...zap.Field) {
	if globalLogger != nil {
		globalLogger.Error(msg, fields...)
	}
}

// Fatal 输出 fatal 级别日志
func Fatal(msg string, fields ...zap.Field) {
	if globalLogger != nil {
		globalLogger.Fatal(msg, fields...)
	}
}

// String 创建字符串字段
func String(key, val string) zap.Field {
	return zap.String(key, val)
}

// Int 创建整数字段
func Int(key string, val int) zap.Field {
	return zap.Int(key, val)
}

// Err 创建错误字段
func Err(err error) zap.Field {
	return zap.Error(err)
}

// Any 创建任意类型字段
func Any(key string, val interface{}) zap.Field {
	return zap.Any(key, val)
}

// Sync 刷新日志
func Sync() error {
	if globalLogger != nil {
		return globalLogger.Sync()
	}
	return nil
}

// GetLogger 获取底层 logger 实例
func GetLogger() *zap.Logger {
	return globalLogger
}
