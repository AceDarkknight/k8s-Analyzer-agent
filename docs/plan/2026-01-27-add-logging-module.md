# 计划：添加日志模块 (Add Logging Module)

## 1. 目标
构建一个高性能、并发安全且易于使用的日志模块，作为项目的基础设施组件。该模块将基于 Uber Zap 和 Lumberjack 实现，支持结构化日志、日志轮转（Rotation）和灵活的配置。

## 2. 依赖管理
需要引入以下 Go 模块：
- **核心日志库**: `go.uber.org/zap` (高性能日志库)
- **日志轮转**: `gopkg.in/natefinch/lumberjack.v2` (负责日志文件的切割和轮转)

## 3. 设计方案

### 3.1 目录结构
在 `internal` 目录下新建 `logger` 包：
```
internal/
└── logger/
    ├── logger.go       # 核心逻辑与接口封装
    ├── config.go       # 配置结构体定义
    └── logger_test.go  # 单元测试
```

### 3.2 配置设计 (Configuration)
定义 `Config` 结构体，支持以下配置项及默认值：
- **Level**: 日志级别 (Debug, Info, Warn, Error, Fatal)，默认为 `Debug`。
- **Filename**: 日志文件路径，默认为 `logs/app.log` (支持配置)。
- **MaxSize**: 单个日志文件最大尺寸 (MB)，默认为 `100` MB。
- **MaxBackups**: 保留旧日志文件的最大个数，默认为 `30` 个。
- **MaxAge**: 保留旧日志文件的最大天数，默认为 `30` 天。
- **Compress**: 是否压缩旧日志，默认为 `false`。
- **LocalTime**: 备份文件名是否使用本地时间（而非 UTC），默认为 `true`。
- **Console**: 是否同时输出到控制台 (stdout)，默认为 `true` (即默认同时输出到文件和控制台)。

### 3.3 功能特性
- **双重输出**: 默认同时将日志输出到文件 (`logs/app.log`) 和控制台 (stdout)。
- **日志轮转策略**: 
    - **触发条件**: 基于大小轮转，当单个日志文件达到 `100MB` 时自动切割。
    - **备份命名**: 备份文件将包含时间戳信息（使用本地时间）。
- **JSON 格式**: 日志内容默认使用 JSON 格式输出，便于后续解析和分析。
- **结构化字段**: 支持键值对形式的结构化日志记录 (e.g., `logger.Info("msg", zap.String("key", "value"))`)。
- **并发安全**: 确保在多 goroutine 环境下安全使用。

## 4. 实现步骤

### 步骤 1: 添加依赖
- 执行 `go get` 命令安装 `zap` 和 `lumberjack` 依赖。

### 步骤 2: 定义配置结构
- 在 `internal/logger/config.go` 中定义配置结构体及默认值。
- 确保默认配置符合需求：输出到文件+控制台，MaxSize=100MB, MaxBackups=30, LocalTime=true。

### 步骤 3: 封装核心逻辑
- 在 `internal/logger/logger.go` 中实现初始化逻辑。
- 配置 Zap 的 `Encoder` (JSON)。
- 配置 `WriteSyncer`：
    - 文件输出：使用 `lumberjack.Logger`，配置 `Filename`, `MaxSize` (100MB), `MaxBackups` (30), `LocalTime` (true)。
    - 控制台输出：使用 `os.Stdout`。
    - 组合输出：使用 `zapcore.NewMultiWriteSyncer` 将两者结合。
- 封装常用的日志方法 (`Debug`, `Info`, `Warn`, `Error`, `Fatal`)。

### 步骤 4: 单元测试
- 在 `internal/logger/logger_test.go` 中编写测试用例。
- 验证日志文件是否成功创建。
- 验证日志轮转功能（通过临时设置极小的 MaxSize 模拟）。
- 验证控制台和文件是否都有输出。

### 步骤 5: 集成到现有代码 (Integration)
将日志模块集成到核心流程中：

1.  **应用初始化 (Application Initialization)**:
    - 修改 `cmd/k8s-analyzer/main.go`。
    - 在程序启动最开始初始化 Logger。
    - 记录应用启动信息 (e.g., "Application started", "Configuration loaded")。

2.  **Agent 处理流程 (Agent Processing)**:
    - 在 `internal/agent` 相关代码中添加日志。
    - **请求开始**: 收到分析请求时记录 (e.g., "Processing new request", requestID)。
    - **关键节点**: 记录分析步骤的转换或关键决策点。
    - **错误处理**: 在遇到错误时记录详细的 Error 日志。
    - **完成处理**: 处理结束时记录耗时或结果状态。

3.  **Client 交互**:
    - 在 `internal/client` (k8s/shell) 中添加 Debug 级别日志，记录关键的 API 调用或命令执行（注意脱敏）。

### 步骤 6: 文档更新
- 更新 `internal/logger/README.md` 文档，说明配置项含义和使用示例。

## 5. 预期效果
- 默认情况下，开发者可以在终端看到实时日志，同时 `logs/app.log` 会持久化保存日志。
- 日志文件单文件最大 100MB，最多保留 30 个备份，备份文件名带本地时间戳。
- 关键业务流程（Agent分析、应用启动）都有清晰的日志记录，便于排查问题。
