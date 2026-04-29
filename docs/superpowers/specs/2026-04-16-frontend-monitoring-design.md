# K8s Analyzer Agent 监控与前端集成设计规范

## 1. 目标

为 K8s Analyzer Agent 引入一个基于 React 的前端监控面板，以及配套的后端指标(Metrics)和任务追踪(Trace) API。实现对 Agent 每次执行任务的详细信息（输入输出、Token 用量、MCP 工具调用情况）的监控和历史查询，并支持对接到 Prometheus。

## 2. 整体架构

系统将引入两个新的模块：

1.  **Frontend (Web UI)**: 放置于 `web/` 目录下的 React + Vite 单页应用。
2.  **Monitor API Server**: 独立于 CLI 诊断流程的后端 HTTP Server（基于 Gin 框架），用于：
    *   暴露 `/metrics` 接口供 Prometheus 抓取。
    *   暴露 `/api/v1/*` 接口供前端查询任务历史数据。

### 2.1 运行模式与目录结构

项目通过 `cmd/` 下的独立目录区分不同的可执行入口：

```
cmd/
├── k8s-analyzer/       # 现有 CLI 诊断工具
│   └── main.go
└── k8s-monitor/        # 新增 Monitor API Server
    └── main.go
```

*   **`k8s-analyzer <query>`** (现有行为)：执行一次性 CLI 诊断，完成后退出。诊断结束时将 Trace 写入文件存储。
*   **`k8s-monitor [--port 9090] [--config configs/config.yaml]`** (新增)：独立启动 Monitor API Server，持续运行，提供 REST API 和 Prometheus `/metrics`。读取历史 Trace 文件供前端查询。

> **设计原则**：两个可执行程序完全独立编译和运行。`k8s-analyzer` 保持纯 CLI 工具特性（执行完退出），`k8s-monitor` 专注于数据展示。两者通过共享的文件存储（Trace 文件）解耦。

### 2.2 Token 用量收集方案

已验证 Eino v0.8.0 的 `schema.Message` 结构体包含 `ResponseMeta *ResponseMeta` 字段，其中 `ResponseMeta.Usage *TokenUsage` 提供完整的 Token 拆解：

```go
// Eino v0.8.0 schema/message.go
type TokenUsage struct {
    PromptTokens     int
    CompletionTokens int
    TotalTokens      int
}
```

**收集策略**：
*   **直接调用**（DecisionNode / ReportNode / LLMAuditor）：每次调用 `Generate()` 后，检查 `response.ResponseMeta.Usage`，累加到 `state.State` 的 Token 统计字段。
*   **ReAct DeepQuery**：Eino `react.Agent` 内部多轮 LLM 调用，最终 `Generate()` 返回的 Message 仅含最后一次 Token。必须实现**精确逐轮统计**。使用 Eino 官方 `react.WithMessageFuture()` API，在调用 `agent.Generate()` 时传入该 Option，并发遍历 `MessageFuture.GetMessages()` Iterator，从每条 Assistant Message 的 `ResponseMeta.Usage` 中累加 Token，实现精确的逐轮 Token 收集。实现时需补齐以下保护措施：
    *   单独封装 Token 收集辅助函数，避免把并发控制散落在业务流程里。
    *   对 `msg`、`msg.ResponseMeta`、`msg.ResponseMeta.Usage` 做完整 nil 检查。
    *   仅统计 `role=assistant` 的消息，避免重复计数。
    *   `Generate()` 返回后必须等待 Iterator 消费完成，再汇总返回 Token 统计结果。
    *   当 Future/Iterator 异常时，记录告警并返回已成功采集到的部分统计，不能影响主诊断结果生成。
*   `DeepQuery` 返回值扩展为 `(string, *schema.TokenUsage, error)`，将累计 Token 返回给上层。

### 2.3 任务级异步记录模型（TaskRecorder）

为避免在主执行热点路径中分散进行重度 Trace 组装、脱敏和序列化，监控能力统一采用**任务级异步记录器**模型。

**核心原则**：
*   执行阶段：各节点只负责采集事件并异步投递到 `TaskRecorder`，不直接组装最终 Trace，也不直接写文件。
*   收敛阶段：任务结束后，由 `TaskRecorder` 汇总内存中的事件，统一完成脱敏、Trace 组装和同步落盘。
*   退出语义：CLI 必须等待 `TaskRecorder` 收敛完成且 Trace 写入结束后才能退出。

**建议结构**：
*   新增 `internal/trace/recorder.go`。
*   每次 `Agent.Run()` 开始时创建一个 `TaskRecorder` 实例，并贯穿整次任务生命周期。
*   `TaskRecorder` 内部维护：
    *   一个事件 channel
    *   一个后台聚合 goroutine
    *   一个内存态 `TaskTraceDraft`
    *   `Wait()` / `Close()` / `Emit()` 等控制方法

**统一事件模型**：
*   `TaskStarted`
*   `LLMTokenUsed`
*   `ToolExecuted`
*   `BlockedCommand`
*   `ReasoningStepUpdated`
*   `TaskFinished`

**Token 异步统计的统一方式**：
*   DeepQuery 内部现有的 `WithMessageFuture()` 异步消费逻辑保留，但其职责仅限于从 Eino Future 中读取消息并提取 TokenUsage。
*   Token 收集协程不直接操作最终 Trace，也不直接写 State/文件；它只向 `TaskRecorder` 发出 `LLMTokenUsed` 事件。
*   其他节点（如 `ActionNode`、`DecisionNode`、`ReportNode`）同样统一通过事件方式把工具调用、推理步骤更新等信息发送给 `TaskRecorder`。

**落盘时机**：
*   `TaskFinished` 发出后，`TaskRecorder` 停止接收新事件。
*   等待后台聚合协程处理完剩余事件。
*   对 `TaskTraceDraft` 执行统一脱敏。
*   转换为最终 `TaskTrace` 并同步调用 `TraceStore.SaveTrace()`。
*   保存完成后，CLI 才允许退出。

## 3. 数据存储设计 (文件系统/结构化日志)

### 3.1 目录结构

*   默认存储路径：`data/traces/`（可通过 `config.yaml` 的 `monitor.trace_dir` 配置）。
*   路径相对于工作目录解析，与现有 `logs/` 目录同级。

### 3.2 Trace 数据结构

由于 `state.State` 包含 `sync.Mutex` 无法直接 JSON 序列化，需定义独立的 Trace 数据结构（`internal/trace/types.go`）：

*   **`TaskTrace`** (完整追踪)：
    *   `task_id` (string, UUID)
    *   `timestamp` (RFC3339)
    *   `user_input` (string)
    *   `status` ("success" / "failed")
    *   `total_duration_ms` (int64)
    *   `token_usage`: `{ prompt_tokens, completion_tokens, total_tokens }`
    *   `k8s_info`: 集群信息快照
    *   `reasoning_history`: `[]TraceReasoningStep`（含 Iteration、Thought、Decision、ToolCalls、Observation、Duration、TokensUsed）
    *   `tool_executions`: `[]TraceToolExecution`（见下方 3.3）
    *   `analysis_result`: 最终报告
    *   `error`: 错误信息（如有）
    *   `active_skill_name`: 激活的技能名（如有）

*   **`TraceIndexRecord`** (索引摘要)：
    *   `task_id`, `timestamp`, `user_input`, `status`
    *   `total_duration_ms`, `total_tokens`, `prompt_tokens`, `completion_tokens`

### 3.4 工具执行记录结构（新增）

现有 `CommandExecution` 缺少工具名、参数和耗时信息。需新增 `TraceToolExecution` 结构：

```go
type TraceToolExecution struct {
    ToolName   string                 `json:"tool_name"`   // list_pods / describe_pod / execute_safe_command 等
    Args       map[string]interface{} `json:"args"`        // 工具参数
    Success    bool                   `json:"success"`
    Output     string                 `json:"output"`      // 摘要输出（非原始输出）
    DurationMs int64                  `json:"duration_ms"` // 执行耗时
    Timestamp  string                 `json:"timestamp"`
    Cached     bool                   `json:"cached"`      // 是否命中缓存
}
```

**数据来源改造**：在 `ActionNode` 执行工具调用时，记录完整的 `TraceToolExecution` 到 `State` 中（需扩展 `State` 字段或在 Trace 收集时从 ReasoningStep + CommandExecution 合并构造）。

**输出口径约束**：
*   Trace 中保存的是**摘要输出**，不保存原始工具输出全文。
*   不额外做长度截断，摘要长度由现有 summarizer 负责控制。
*   在写入 Trace 前必须对工具参数、摘要输出、最终报告中的敏感字段做脱敏处理（例如 Token、密钥、密码、认证头、可能的凭据片段）。

### 3.5 存储格式 (JSONL)

*   **索引文件 (`traces_index.jsonl`)**：每行一个 `TraceIndexRecord` JSON 对象，按追加写入。
*   **详细追踪文件 (`{task_id}.json`)**：每个任务一个完整 `TaskTrace` JSON 文件。

### 3.6 读写策略

*   **写入**：`k8s-analyzer` 在任务完成后同步写入（非异步），**只有在 Trace 及索引写入完成后 CLI 才退出**，确保本次任务观测数据已经落盘。
*   **单写者假设**：当前部署模型按单写者策略设计，不考虑多进程并发写入同一 Trace 目录的场景，因此本阶段不引入跨进程并发写保护。
*   **读取**：`k8s-monitor` 的 API 层读取文件。
    *   `ListTraces`：顺序读取 `traces_index.jsonl` 全部行，在内存中倒序后分页返回。当前页面为单次使用场景，且生产环境按每个任务独立运行假设设计，因此该方案在当前规模下可接受。
    *   `GetTrace`：直接读取 `{task_id}.json`。

## 4. 后端接口设计 (Golang)

### 4.1 配置扩展

在 `config.yaml` 和 `Config` 结构中新增：

```yaml
monitor:
  api_port: 8080        # k8s-monitor 监听端口
  trace_dir: "data/traces"  # Trace 存储目录
```

### 4.2 Prometheus Metrics

引入 `github.com/prometheus/client_golang`。

*   `agent_task_total` (CounterVec, labels: `status`): 任务总数。
*   `agent_llm_tokens_total` (CounterVec, labels: `model`, `type`[prompt/completion]): Token 消耗。
*   `agent_tool_calls_total` (CounterVec, labels: `tool_name`, `status`): 工具调用次数。
*   `agent_task_duration_seconds` (HistogramVec, labels: `status`): 任务耗时分布。

**埋点方式**：定义 `internal/metrics/collector.go` 提供 `RecordTaskComplete()` / `RecordTokenUsage()` / `RecordToolCall()` 等函数。由上层（`agent.go`、`decision_node.go`）调用，避免底层包（`llm/`、`client/`）反向 import 造成循环依赖。

### 4.3 REST API

使用 Gin 框架（`github.com/gin-gonic/gin`）构建 API Server，放置于 `internal/api/` 包中。

*   `GET /api/v1/tasks?page=1&size=20`：获取任务列表（倒序分页）。
*   `GET /api/v1/tasks/{task_id}`：获取任务详情。
*   `GET /metrics`：Prometheus handler。

**统一响应格式**：

```json
{
  "code": 0,
  "message": "ok",
  "data": { ... }
}
```

成功时 `code=0`，`message="ok"`。错误时 `code` 为预定义的业务错误码，`message` 携带错误描述。

**错误码定义**：在 `internal/api/errors.go` 中集中定义所有业务错误码和对应消息：

```go
// internal/api/errors.go
const (
    CodeOK             = 0
    CodeBadRequest     = 40000  // 请求参数错误
    CodeNotFound       = 40400  // 资源不存在
    CodeInternalError  = 50000  // 服务器内部错误
    CodeStoreError     = 50001  // 存储层读写错误
)

var codeMessages = map[int]string{
    CodeOK:            "ok",
    CodeBadRequest:    "invalid request parameters",
    CodeNotFound:      "resource not found",
    CodeInternalError: "internal server error",
    CodeStoreError:    "trace store error",
}
```

**CORS**：通过 Gin CORS 中间件（`github.com/gin-contrib/cors`）配置，开发阶段允许 `localhost:*` 跨域，生产部署可通过 Vite proxy 或 Nginx 反代规避。

### 4.4 Agent.Run() 返回值扩展

当前 `Agent.Run()` 仅返回 `*state.AnalysisResult`。为支持 Trace 收集，需改为返回完整 State 或在 Run 内部直接完成 Trace 写入：

```go
// 方案：在 Agent.Run() 内部完成 Trace 写入（推荐）
func (a *Agent) Run(ctx context.Context, userQuery string) (*state.AnalysisResult, error) {
    taskID := uuid.New().String()
    startTime := time.Now()
    // ... 执行诊断 ...
    // 任务结束后构造 TaskTrace 并写入
    if a.traceStore != nil {
        trace := buildTaskTrace(taskID, startTime, finalState)
        a.traceStore.SaveTrace(ctx, trace)
    }
    return finalState.AnalysisResult, nil
}
```

## 5. 前端设计 (React + TS + Vite)

### 5.1 技术栈

*   框架: React 18, Vite, TypeScript
*   UI 组件库: Ant Design v5 (antd)
*   路由: React Router v6
*   HTTP 客户端: Axios
*   数据可视化: Recharts
*   Markdown 渲染: react-markdown

### 5.2 页面划分

1.  **Dashboard (大盘)**:
    *   统计卡片：总任务数、成功率、总 Token 用量。通过 `/api/v1/tasks` 计算。
    *   (可选) 任务趋势图、工具调用分布图。
2.  **Task Traces (任务追踪)**:
    *   任务列表 (Table)：展示 Task ID、时间、输入、耗时、Token 消耗、状态。
    *   任务详情页：
        *   **基础信息**: 耗时、Token 拆解 (Prompt/Completion)、激活技能。
        *   **执行链 (Traces Tab)**: 按 Iteration 展开 Timeline，每节点含 Thought → Action (折叠面板展示 ToolCalls 参数/输出) → Observation。
        *   **最终报告 (Report Tab)**: Markdown 渲染。
        *   **原生 JSON (Raw Tab)**: 完整 Trace JSON 展示。

### 5.3 错误与空状态处理

*   **API 超时/500**：展示全局错误提示（antd `message.error`）。
*   **任务列表为空**：展示 Empty 组件引导用户执行首次诊断。
*   **详情页 404**：展示 Result 组件 + 返回列表按钮。

### 5.4 部署策略

*   **开发阶段**：`vite.config.ts` 配置 proxy 将 `/api` 代理到 `http://localhost:9090`。
*   **生产部署**：使用 Go `//go:embed` 将 `web/dist/` 嵌入 `k8s-monitor` 二进制中，实现单文件部署。

## 6. 需同步更新的配置

*   `configs/config.yaml`：新增 `monitor` 配置段。
*   `.gitignore`：新增 `web/node_modules/`、`web/dist/`、`data/traces/`。
