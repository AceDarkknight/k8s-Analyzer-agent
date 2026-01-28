# Logger 模块

本模块基于 Uber Zap 和 Lumberjack 实现了高性能、并发安全的日志记录功能。

## 功能特性

- **高性能日志记录**: 基于 Uber Zap，专为高性能场景设计
- **日志轮转**: 使用 Lumberjack 实现日志文件轮转
- **多种日志级别**: 支持 debug、info、warn、error、fatal 五种级别
- **并发安全**: 支持多个 goroutine 同时调用
- **JSON 格式输出**: 结构化日志，便于日志分析和处理
- **结构化字段**: 支持添加自定义字段
- **自动调用者信息**: 自动记录日志调用者的文件名和行号
- **自动时间戳**: 自动添加 ISO8601 格式的时间戳
- **双重输出**: 默认同时输出到文件和控制台

## 使用方法

### 快速开始

使用默认配置初始化日志：

```go
import "github.com/AceDarkknight/k8s-analyzer-agent/internal/logger"

// 使用默认配置初始化
err := logger.Init()
if err != nil {
    panic(err)
}

// 记录日志
logger.Info("Application started")
logger.Debug("Debug information", logger.With(logger.String("key", "value")))
```

### 自定义配置

使用自定义配置初始化日志：

```go
import (
    "github.com/AceDarkknight/k8s-analyzer-agent/internal/logger"
)

cfg := &logger.Config{
    Level:           logger.InfoLevel,
    Filename:        "logs/myapp.log",
    MaxSize:         100,    // 100MB
    MaxBackups:      30,     // 保留30个备份
    MaxAge:          0,      // 不限制天数
    Compress:        false,  // 不压缩
    LocalTime:       true,   // 使用本地时间
    OutputToConsole: true,   // 输出到控制台
}

err := logger.InitWithConfig(cfg)
if err != nil {
    panic(err)
}
```

### 日志记录

#### 使用 Zap Logger

```go
import (
    "go.uber.org/zap"
)

// 获取 logger
log := logger.GetLogger()

// 记录日志
log.Info("User logged in",
    zap.String("username", "test"),
    zap.Int("userId", 123),
)

// 使用 With 创建子 logger
childLog := log.With(zap.String("service", "auth"))
childLog.Info("Authentication successful")
```

#### 使用 Sugared Logger

```go
import (
    "github.com/AceDarkknight/k8s-analyzer-agent/internal/logger"
)

// 获取 sugared logger
sugar := logger.GetSugar()

// 格式化日志
sugar.Infof("User %s logged in at %v", "test", time.Now())

// 结构化日志
sugar.Infow("User logged in",
    "username", "test",
    "userId", 123,
)
```

#### 使用便捷函数

```go
import (
    "github.com/AceDarkknight/k8s-analyzer-agent/internal/logger"
    "go.uber.org/zap"
)

// 不同级别的日志
logger.Debug("Debug message")
logger.Info("Info message")
logger.Warn("Warning message")
logger.Error("Error message")
logger.Fatal("Fatal message") // 会调用 os.Exit(1)

// 带字段的日志
logger.Info("User action",
    zap.String("action", "login"),
    zap.Bool("success", true),
)

// 格式化日志
logger.Infof("Processing item %d", 42)

// 结构化日志
logger.Infow("Processing item",
    "id", 42,
    "status", "processing",
)
```

### 程序退出

在程序退出前调用 `Sync()` 以确保所有日志都被写入：

```go
import (
    "os"
    "os/signal"
    "syscall"
)

func main() {
    // 初始化日志
    logger.Init()

    // ... 你的代码 ...

    // 优雅退出
    sigChan := make(chan os.Signal, 1)
    signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
    <-sigChan

    // 刷新日志
    logger.Sync()
}
```

### 重定向标准库日志

可以将标准库的日志（如 `fmt.Println`）重定向到 zap：

```go
import (
    "go.uber.org/zap/zapcore"
)

// 创建 writer
writer := logger.NewWriter(zapcore.InfoLevel)

// 重定向标准输出
// 注意：这会替换标准输出，谨慎使用
// os.Stdout = writer
```

## 配置说明

### Config 结构体

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| Level | Level | DebugLevel | 日志级别 |
| Filename | string | "logs/app.log" | 日志文件路径 |
| MaxSize | int | 100 | 单个日志文件最大尺寸（MB） |
| MaxBackups | int | 30 | 保留旧日志文件的最大个数 |
| MaxAge | int | 0 | 保留旧日志文件的最大天数（0表示不限制） |
| Compress | bool | false | 是否压缩旧日志文件 |
| LocalTime | bool | true | 是否使用本地时间创建备份文件名 |
| OutputToConsole | bool | true | 是否同时输出到控制台 |

### 日志级别

- `DebugLevel`: 调试级别，最详细的日志
- `InfoLevel`: 信息级别，常规信息
- `WarnLevel`: 警告级别，潜在问题
- `ErrorLevel`: 错误级别，错误但不影响程序运行
- `FatalLevel`: 致命错误级别，记录后会调用 `os.Exit(1)`

## 日志轮转

日志轮转由 Lumberjack 自动管理：

- 当日志文件大小达到 `MaxSize` 时，会自动创建新文件
- 旧文件会重命名为 `app-2024-01-27T15-30-00.log` 格式
- 最多保留 `MaxBackups` 个旧文件
- 超过 `MaxAge` 天的旧文件会被删除（如果设置了）

## 并发安全

本模块完全支持并发调用，可以在多个 goroutine 中安全地使用。

```go
var wg sync.WaitGroup
for i := 0; i < 100; i++ {
    wg.Add(1)
    go func(id int) {
        defer wg.Done()
        logger.Info("Concurrent log", zap.Int("id", id))
    }(i)
}
wg.Wait()
```

## 注意事项

1. **初始化**: `Init()` 或 `InitWithConfig()` 只能调用一次，多次调用会返回错误
2. **程序退出**: 在程序退出前务必调用 `Sync()` 以确保所有日志都被写入
3. **Fatal 级别**: `Fatal` 级别的日志会调用 `os.Exit(1)`，程序会立即退出
4. **日志目录**: 如果日志目录不存在，模块会自动创建
5. **性能**: 对于性能敏感的场景，推荐使用 `GetLogger()` 而不是 `GetSugar()`

## 测试

运行单元测试：

```bash
go test ./internal/logger/... -v
```

## 示例

完整示例请参考 `logger_test.go` 文件。
