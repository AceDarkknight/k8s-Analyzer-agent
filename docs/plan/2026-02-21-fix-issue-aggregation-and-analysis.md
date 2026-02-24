# 2026-02-21 修复 Kubernetes 分析器问题聚合与解决方案建议逻辑

## 1. 背景 (Context)

用户反馈 K8s Analyzer Agent 在分析 Pod 问题时存在两个主要缺陷：
1.  **重复报告问题**：对于同一个 Pod，可能会输出多个重复或相似的问题（例如同时报告 Critical 和 High 级别的相同根因问题），导致报告冗余。
2.  **解决方案过于通用**：即使日志中已经明确指出了错误原因（如 "directory does not exist"），Agent 仍然只提供通用的建议（如 "check logs"），未能利用已收集的日志信息生成针对性的修复建议。
3.  **LLM 工具感知不足**：`internal/agent/analysis/llm.go` 中的 `FormatToolsPrompt` 方法未被调用，导致 LLM 不清楚有哪些工具可用，无法给出准确的工具调用建议。
4.  **去重机制缺失**：目前缺乏跨周期的去重机制，需要支持内存和 Redis 两种模式以适应不同部署环境。

## 2. 问题分析 (Problem Analysis)

### 2.1 诊断流程过于线性 (Linear Diagnosis Workflow)
当前 `InfoNode` 试图一次性收集所有信息，效率低下。新工作流应遵循：发现 -> 过滤 -> 按需深挖 -> LLM 决策。

### 2.2 重复问题报告与持久化 (Duplicate Issues & Persistence)
当前代码 (`State.AddFinding`) 缺乏查重机制。需要引入一个持久化层（Store）来记录已发现的问题，防止重复报告。

### 2.3 解决方案建议不足 (Generic Solutions)
需要将不健康 Pod 的完整上下文（状态 + 事件 + 日志）通过 Prompt 传递给 LLM，要求其输出具体的修复建议。

### 2.4 LLM 上下文缺失 (Missing LLM Context)
LLM 需要知道当前可用的工具列表，以便在分析时建议正确的下一步操作或工具调用。

## 3. 方案设计 (Proposed Changes)

### 3.1 优化诊断工作流 (Optimized Diagnosis Workflow)

**修改 `InfoNode` 和 `ReportNode` 的职责：**

1.  **增强 `InfoNode`**:
    -   保留 `list_namespaces` 和 `list_pods`。
    -   **Loop & Filter**: 遍历 Pods，识别异常 Pod (Status != Running/Succeeded 或 Restarts > 0)。
    -   **Conditional Fetch**: 仅对异常 Pod 调用 `get_events` 和 `get_pod_logs`。
    -   **Store Context**: 将详细信息存入 `State` 中的 `UnhealthyPodContext` 列表。

### 3.2 引入 FindingStore 进行去重 (FindingStore for De-duplication)

创建一个新的接口 `FindingStore` 用于管理发现结果的去重和持久化。

1.  **接口定义 (`internal/agent/analysis/store.go`)**:
    ```go
    type FindingStore interface {
        // HasFinding 检查是否已存在相同的 Finding
        // key: 唯一标识符 (例如 "cluster:ns:pod:issue_type")
        HasFinding(ctx context.Context, key string) (bool, error)

        // SaveFinding 保存 Finding 记录
        // key: 唯一标识符
        // ttl: 过期时间
        SaveFinding(ctx context.Context, key string, ttl time.Duration) error
    }
    ```

2.  **配置与实现**:
    -   **新增配置**: 在 `internal/config` 包中创建 `store_config.go`，定义 `RedisConfig` 结构体（包含 Host, Port, Password, DB）。
    -   **`MemoryStore`**: 使用 `github.com/jellydator/ttlcache/v3` 实现，作为默认回退选项。这是为了支持对去重键（deduplication keys）的 TTL 过期机制，同时利用其泛型支持提供类型安全。
    -   **`RedisStore`**: 使用 `github.com/redis/go-redis/v9` 实现。

3.  **初始化与回退逻辑**:
    -   **位置**: `internal/agent/analysis/graph.go` 中的 `NewAgent` 函数（Graph 构建处）。
    -   **逻辑**:
        -   读取配置中的 Redis 配置项。
        -   **Check**: 如果 Redis 配置为空或无效（nil/empty）：
            -   **Fallback**: 初始化 `MemoryStore`。
            -   **Log**: 输出 WARN 日志 "Redis not configured, using in-memory store"。
        -   **Else**: 初始化 `RedisStore`。
        -   将初始化的 `store` 实例传递给 `ReportNode`。

    ```go
    // 伪代码示例
    var store FindingStore
    if config.Redis == nil || config.Redis.Host == "" {
        logger.Warn("Redis not configured, using in-memory store")
        store = NewMemoryStore()
    } else {
        store = NewRedisStore(config.Redis)
    }
    // 注入 ReportNode
    reportNode := NewReportNode(store, agent.llm)
    ```

### 3.3 基于 LLM 的智能根因分析与工具集成 (LLM-based RCA & Tool Integration)

1.  **集成 `FormatToolsPrompt`**:
    -   在 `internal/agent/analysis/llm.go` 中，确保 `MakeDecision` 或 `AnalyzeError` 构建 Prompt 时调用 `FormatToolsPrompt()`。

2.  **扩展 `LLM` 接口**:
    增加 `AnalyzeError` 方法，用于针对特定错误上下文进行深入分析。
    ```go
    type ErrorContext struct {
        PodName   string
        Namespace string
        Status    string
        Logs      string
        Events    []string
    }

    type LLM interface {
        // ... existing methods
        AnalyzeError(ctx context.Context, errorContext ErrorContext) (AnalysisResult, error)
    }
    ```

3.  **调用位置与流程 (`ReportNode` 优化)**:
    -   **文件**: `internal/agent/analysis/nodes.go`
    -   **方法**: `ReportNode.Execute` (或其辅助方法 `analyzeFindings`)。
    -   **流程**:
        1.  遍历 `State` 中的异常 Pod。
        2.  **Generate Key**: `fmt.Sprintf("finding:%s:%s:%s", namespace, podName, issueType)`。
        3.  **Check Store**: 调用 `store.HasFinding(ctx, key)`。
            -   如果返回 `true` (已存在) -> **Skip** (记录日志 "Skipping duplicate finding")。
        4.  如果不存在：
            -   构建 `ErrorContext` (从 State 中提取 Logs, Events)。
            -   **Call LLM**: `analysis, err := n.llm.AnalyzeError(ctx, errorContext)`。
            -   **Add Finding**: 将 LLM 返回的分析结果（根因、建议）添加到报告中。
            -   **Save to Store**: 调用 `store.SaveFinding(ctx, key, ttl)`。

## 4. 详细实现步骤 (Implementation Steps)

### 步骤 1: 基础设施 (Infrastructure)
1.  在 `internal/config` 中新建 `store_config.go`，添加 `RedisConfig` 定义。
2.  创建 `internal/agent/analysis/store.go`，定义 `FindingStore` 接口。
3.  实现 `MemoryStore` (基于 `github.com/jellydator/ttlcache/v3`，需先执行 `go get github.com/jellydator/ttlcache/v3`)。
4.  引入 Redis 依赖 `github.com/redis/go-redis/v9` 并实现 `RedisStore`。

### 步骤 2: Agent 初始化更新 (Agent Update)
1.  修改 `internal/agent/analysis/graph.go` 中的 `Agent` 结构体，增加 `store FindingStore` 字段。
2.  修改 `NewAgent` 函数，实现 Redis 配置检查和回退逻辑 (Redis -> Memory + Log)。
3.  修改 `NewReportNode` 签名，接受 `FindingStore` 和 `LLM` 实例。

### 步骤 3: LLM 接口与实现 (LLM Integration)
1.  修改 `internal/agent/analysis/llm.go`，定义 `ErrorContext` 结构体。
2.  在 `LLM` 接口中添加 `AnalyzeError` 方法。
3.  实现 `AnalyzeError`：构建包含工具列表 (System Prompt) 和错误上下文 (User Prompt) 的 Prompt，调用模型并解析 JSON 结果。

### 步骤 4: ReportNode 逻辑重构 (Node Refactoring)
1.  修改 `internal/agent/analysis/nodes.go` 中的 `ReportNode`。
2.  在 `Execute` 方法中实现"去重 -> 分析 -> 存储"的完整闭环。
    -   确保正确调用 `llm.AnalyzeError(ctx, errorContext)`。
    -   确保正确处理 Store 的读写。

### 步骤 5: 验证与测试 (Verification)
1.  编写 `store_test.go` 测试 Memory 和 Redis 实现。
2.  运行 Agent 模拟无 Redis 配置，验证是否回退到 MemoryStore 并打印日志。
3.  模拟重复故障，验证是否不再生成重复报告。

## 5. 验证计划 (Verification Plan)

### 5.1 单元测试
-   测试 `MemoryStore` 和 `RedisStore` 的 `HasFinding` 和 `SaveFinding`。
-   验证 `FormatToolsPrompt` 输出是否包含所有工具及其参数。

### 5.2 集成测试
-   **Fallback 测试**: 不配置 Redis，启动 Agent，检查日志中是否有 "using in-memory store"。
-   **去重测试**: 模拟两次相同的异常 Pod 数据，确认第二次分析时跳过 LLM 调用和 Finding 生成。
