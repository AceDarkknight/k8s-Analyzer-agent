# K8s Analyzer Agent 监控前端与指标集成实施计划

## 1. 概述

本计划旨在指导在 K8s Analyzer Agent 项目中实现 React 前端监控面板，以及配套的 Golang 后端 Metrics (Prometheus) 和任务历史追踪 (Trace) API。

**关键架构决策**：
- `k8s-analyzer` CLI 保持现有"执行→退出"行为，在 `cmd/` 下新增 `k8s-monitor` 目录作为独立可执行的 HTTP Server。
- HTTP Server 使用 Gin 框架（`github.com/gin-gonic/gin`）。
- Token 用量通过 Eino `schema.Message.ResponseMeta.Usage` 获取（已验证可行）。
- Trace 数据结构独立于 `state.State`（后者含 `sync.Mutex` 无法直接序列化）。
- API 统一响应格式 `{code, message, data}`，错误码与消息在 `internal/api/errors.go` 中集中定义。
- `k8s-analyzer` 必须在 Trace 和索引文件写入完成后再退出，不采用异步落盘。
- 当前存储模型按单写者策略设计，不处理多个写入进程同时写同一 Trace 目录的并发场景。
- Trace 中只保存摘要输出，不保存工具原始输出；写入前必须进行敏感字段脱敏。
- DeepQuery 必须支持精确逐轮 Token 统计，但实现上要把并发收集逻辑封装到独立辅助层，避免污染主执行流。
- 监控记录统一采用任务级 `TaskRecorder`：执行阶段异步聚合到内存，任务结束时统一脱敏、组装并同步落盘。

## 2. 实施阶段拆解

### 阶段一：State 扩展与 Token 收集 (Token Tracking)

**目标:** 在现有诊断流程中收集 Token 用量数据，为后续 Trace 和 Metrics 提供数据源。

*   **1.1 扩展 State 的 Token 统计字段:**
    *   在 `internal/state/state.go` 的 `State` 结构中新增：
        ```go
        TotalPromptTokens     int
        TotalCompletionTokens int
        TotalTokens           int
        ```
    *   提供 `AccumulateTokenUsage(usage *schema.TokenUsage)` 方法，累加单次 LLM 调用的 Token 消耗。

*   **1.2 在 LLMRouter 返回后收集 Token:**
    *   修改 `GenerateWithLight` / `GenerateWithPower` 的返回值，额外返回 `*schema.TokenUsage`：
        ```go
        func (r *LLMRouter) GenerateWithLight(ctx context.Context, messages []*schema.Message) (*schema.Message, *schema.TokenUsage, error) {
            resp, err := r.light.Generate(ctx, messages)
            if err != nil { return nil, nil, err }
            var usage *schema.TokenUsage
            if resp.ResponseMeta != nil {
                usage = resp.ResponseMeta.Usage
            }
            return resp, usage, nil
        }
        ```
    *   在 `DecisionNode.Execute()` 和 `ReportNode.Execute()` 中调用后，将 usage 累加到 State。

*   **1.3 ReAct DeepQuery 的 Token 收集:**
    *   `react.Agent` 内部多轮 LLM 调用，最终 `Generate()` 返回的 Message 仅含最后一次 Token，直接读取不可靠。
    *   **方案**：使用 Eino 官方 `react.WithMessageFuture()` API，启动独立 goroutine 在 Generate 执行期间异步消费 Iterator 累加 Token；但这部分逻辑必须封装到 `internal/llm` 内部辅助函数中，例如 `collectDeepQueryTokenUsage(...)`，业务层只接收聚合后的结果：
        ```go
        // 1. 创建 MessageFuture
        opt, future := react.WithMessageFuture()
        
        // 2. 启动 goroutine 异步消费 Iterator（与 Generate 并发执行）
        var totalUsage schema.TokenUsage
        var wg sync.WaitGroup
        wg.Add(1)
        go func() {
            defer wg.Done()
            iter := future.GetMessages()
            for {
                msg, ok, iterErr := iter.Next()
                if !ok || iterErr != nil { break }
                if msg.Role == schema.Assistant && msg.ResponseMeta != nil && msg.ResponseMeta.Usage != nil {
                    totalUsage.PromptTokens += msg.ResponseMeta.Usage.PromptTokens
                    totalUsage.CompletionTokens += msg.ResponseMeta.Usage.CompletionTokens
                    totalUsage.TotalTokens += msg.ResponseMeta.Usage.TotalTokens
                }
            }
        }()
        
        // 3. 调用 Generate（内部每轮 LLM 完成后会推送 Message 到 Iterator）
        response, err := agent.Generate(ctx, messages, opt)
        
        // 4. 等待异步消费完成
        wg.Wait()
        // totalUsage 即为本次 DeepQuery 的累计 Token
        ```
    *   保护要求：
        *   仅统计 `assistant` 消息。
        *   完整 nil 检查，避免 Future 消费阶段 panic。
        *   `Generate()` 返回后必须等待消费协程退出。
        *   Token 统计失败不能中断 DeepQuery 主逻辑，需降级为“返回内容成功，但 Token 统计部分缺失/部分成功”。
    *   `DeepQuery` 返回值扩展为 `(string, *schema.TokenUsage, error)`，将累计的 TokenUsage 返回给上层 `ActionNode`，由其累加到 State。

*   **1.4 引入任务级 TaskRecorder（统一异步记录器）:**
    *   新建 `internal/trace/recorder.go`。
    *   定义：
        ```go
        type TaskRecorder interface {
            Emit(event TraceEvent)
            Close()
            Wait() error
            Snapshot() *TaskTraceDraft
        }
        ```
    *   内部实现包含：
        *   `chan TraceEvent`
        *   后台聚合 goroutine
        *   `TaskTraceDraft` 内存草稿对象
        *   `sync.WaitGroup` / `context.Context` 控制退出
    *   约束：
        *   所有监控数据先入 recorder，不直接在热点路径中组装最终 Trace。
        *   recorder 只负责内存聚合，不负责即时落盘。

*   **1.5 统一事件模型:**
    *   新建 `internal/trace/events.go`，定义事件类型：
        *   `TaskStartedEvent`
        *   `LLMTokenUsedEvent`
        *   `ToolExecutedEvent`
        *   `BlockedCommandEvent`
        *   `ReasoningStepUpdatedEvent`
        *   `TaskFinishedEvent`
    *   所有事件包含基础时间戳；需要时附带 `task_id`、iteration、source 字段。
    *   DeepQuery 的 MessageFuture 消费协程通过 `Emit(LLMTokenUsedEvent)` 汇报 Token；`ActionNode` 通过 `Emit(ToolExecutedEvent)` 汇报工具执行；决策/报告节点通过 `Emit(ReasoningStepUpdatedEvent)` 汇报推理链更新。

### 阶段二：Trace 数据结构与存储 (Trace Store)

**目标:** 定义 Trace 数据结构并实现文件系统持久化。

*   **2.1 定义 Trace 数据结构:**
    *   新建 `internal/trace/types.go`，定义：
        *   `TaskTrace`：完整任务追踪（task_id, timestamp, user_input, status, duration, token_usage, reasoning_history, tool_executions, analysis_result, error, active_skill_name）。
        *   `TraceIndexRecord`：索引摘要（task_id, timestamp, user_input, status, duration, token 拆解）。
        *   `TraceToolExecution`：工具执行记录（tool_name, args, success, output, duration_ms, timestamp, cached）。
        *   `TraceReasoningStep`：推理步骤记录（从 `state.ReasoningStep` 转换，不含不可序列化字段）。
    *   提供 `BuildTaskTrace(taskID string, startTime time.Time, s *state.State) *TaskTrace` 转换函数，从 State 提取并映射字段。
    *   新增 `TaskTraceDraft`，作为 recorder 在内存中持续聚合的中间结构；任务结束时再从 Draft 转换为最终 `TaskTrace`。

*   **2.2 扩展 State 的工具执行记录:**
    *   当前 `CommandExecution` 缺少工具名、参数、耗时。扩展为：
        ```go
        type CommandExecution struct {
            Command       string
            ToolName      string        // 新增：工具名
            Args          map[string]interface{} // 新增：工具参数
            Success       bool
            Output        string
            DurationMs    int64         // 新增：执行耗时(ms)
            Timestamp     time.Time
            IsVerifyPhase bool
            Cached        bool          // 新增：是否命中缓存
        }
        ```
    *   在 `ActionNode` 执行工具调用时填充新字段。
    *   `Output` 字段统一保存摘要输出，不保存原始 stdout/stderr 全文。
    *   新增脱敏步骤：在写入 `CommandExecution` / `TraceToolExecution` 前，对参数和输出执行敏感信息脱敏。
    *   同时把同一份结构化数据作为 `ToolExecutedEvent` 投递给 `TaskRecorder`，避免后续重复解析。

*   **2.3 实现 Trace Store:**
    *   新建 `internal/store/trace_store.go`，定义接口与文件实现：
        ```go
        type TraceStore interface {
            SaveTrace(ctx context.Context, trace *trace.TaskTrace) error
            GetTrace(ctx context.Context, taskID string) (*trace.TaskTrace, error)
            ListTraces(ctx context.Context, page, size int) ([]trace.TraceIndexRecord, int, error) // 返回记录+总数
            Close() error
        }
        ```
    *   文件实现：索引追加写入 `traces_index.jsonl`，详情写入 `{task_id}.json`。
    *   写入模式采用**同步单写者策略**：只覆盖当前部署假设中的单进程写入，不实现多写者并发控制。
    *   `ListTraces` 实现：读取全部索引行 → 内存倒序 → 分页截取。当前页面为单次使用场景，生产环境按每个任务独立运行假设设计，因此本阶段接受该实现。

*   **2.4 实现统一脱敏与 Draft → Trace 转换:**
    *   新建 `internal/trace/sanitizer.go`，统一处理敏感字段脱敏。
    *   脱敏发生在**任务结束后、正式落盘前**，而不是每次工具调用完成后立即执行。
    *   脱敏对象包括：
        *   Tool args
        *   Tool 摘要输出
        *   用户输入中可能的敏感参数
        *   最终报告中的敏感片段
    *   新建 Draft 转换函数，将 `TaskTraceDraft` 转换为最终 `TaskTrace` 和 `TraceIndexRecord`。

*   **2.5 在 Agent.Run() 中集成 TaskRecorder 与 Trace 写入:**
    *   在 `internal/agent/diagnosis/agent.go` 的 `Run` 方法中：
        *   任务开始时生成 UUID 格式 `TaskID`，记录 `startTime`。
        *   任务开始时创建 `TaskRecorder`，并先发送 `TaskStartedEvent`。
        *   将 recorder 传入需要记录事件的节点或上下文对象。
        *   任务结束后（defer 中）发送 `TaskFinishedEvent`。
        *   调用 `recorder.Close()`，等待 `recorder.Wait()` 完成聚合。
        *   从 recorder 的 `TaskTraceDraft` 构造最终 Trace，经统一脱敏后调用 `TraceStore.SaveTrace()` 同步写入。
        *   `SaveTrace` 成功或失败都必须有明确日志；只有写入流程返回后 CLI 才允许退出。
    *   `Agent` 结构新增 `traceStore store.TraceStore` 依赖，通过 `NewAgent` 注入。

*   **2.6 更新配置:**
    *   `internal/config/config.go` 新增 `MonitorConfig`：
        ```go
        type MonitorConfig struct {
            APIPort  int    `yaml:"api_port"`   // 默认 8080
            TraceDir string `yaml:"trace_dir"`  // 默认 "data/traces"
        }
        ```
    *   `configs/config.yaml` 新增 `monitor` 配置段。

### 阶段三：后端 API & Prometheus Metrics

**目标:** 暴露 REST API 供前端查询，暴露 `/metrics` 供 Prometheus 拉取。

*   **3.1 集成 Prometheus Client:**
    *   `go get github.com/prometheus/client_golang`。
    *   新建 `internal/metrics/collector.go`，定义并注册全局 Metrics：
        *   `agent_task_total` (CounterVec)
        *   `agent_llm_tokens_total` (CounterVec)
        *   `agent_tool_calls_total` (CounterVec)
        *   `agent_task_duration_seconds` (HistogramVec)
    *   提供上层调用函数 `RecordTaskComplete(status, duration)`、`RecordTokenUsage(model, prompt, completion)`、`RecordToolCall(toolName, success)`。
    *   **注意**：仅由 `agent.go`、`decision_node.go` 等上层调用，底层包（`llm/`、`client/`）不 import metrics，避免循环依赖。
    *   Label 范围固定为 `status`、`model`、`type`、`tool_name`，禁止把 `task_id`、用户输入、资源名等高基数字段放入 Prometheus labels。

*   **3.2 Metrics 埋点:**
    *   `Agent.Run()` 结束时：调用 `RecordTaskComplete` + `RecordTokenUsage`（汇总）。
    *   `ActionNode.Execute()` 每次工具调用后：调用 `RecordToolCall`。

*   **3.3 实现 REST API Server:**
    *   新建 `internal/api/server.go`，使用 Gin 框架：
        ```go
        func NewServer(port int, traceStore store.TraceStore) *gin.Engine
        ```
    *   路由：
        *   `GET /metrics` → `gin.WrapH(promhttp.Handler())`
        *   `GET /api/v1/tasks` → 分页查询
        *   `GET /api/v1/tasks/:id` → 详情查询
    *   CORS 中间件：使用 `github.com/gin-contrib/cors`，开发阶段允许 `localhost:*` 跨域。

*   **3.4 定义统一响应格式与错误码:**
    *   新建 `internal/api/errors.go`，集中定义业务错误码和消息：
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
    *   新建 `internal/api/response.go`，封装统一响应函数：
        ```go
        // 成功响应
        func Success(c *gin.Context, data interface{}) {
            c.JSON(http.StatusOK, gin.H{"code": CodeOK, "message": "ok", "data": data})
        }
        // 错误响应
        func Error(c *gin.Context, httpStatus int, code int) {
            c.JSON(httpStatus, gin.H{"code": code, "message": codeMessages[code], "data": nil})
        }
        ```

*   **3.5 新增 `cmd/k8s-monitor` 独立入口:**
    *   新建 `cmd/k8s-monitor/main.go`，作为独立可执行程序：
        ```go
        // cmd/k8s-monitor/main.go
        func main() {
            // 解析 --port, --config 参数
            // 加载配置 → 初始化 TraceStore → 创建 Gin Server → 启动 + 优雅停机
        }
        ```
    *   编译产物：`go build -o bin/k8s-monitor ./cmd/k8s-monitor`。
    *   `cmd/k8s-analyzer/main.go` 无需修改（保持现有 CLI 行为）。

### 阶段四：前端基础搭建与 Dashboard (Frontend MVP)

**目标:** 搭建前端基础架构，实现数据概览和任务列表。

*   **4.1 初始化 React 项目:**
    *   在 `web/` 目录下执行 `npm create vite@latest . -- --template react-ts`。
    *   安装依赖：`npm install react-router-dom axios antd@^5 @ant-design/icons recharts react-markdown`。
    *   配置 `vite.config.ts` 设置 proxy，将 `/api` 和 `/metrics` 代理到 `http://localhost:8080`。

*   **4.2 布局与路由:**
    *   实现基础 Admin 布局（侧边栏 + 内容区）。
    *   路由：`/` (Dashboard)、`/tasks` (任务列表)、`/tasks/:id` (任务详情)。

*   **4.3 Dashboard 开发:**
    *   统计卡片：总任务数、成功率、总 Token 用量（从 `/api/v1/tasks` 聚合计算）。当前页面为单次使用场景，因此本阶段不额外引入独立聚合接口。
    *   (可选) Recharts 绘制任务趋势图。

*   **4.4 任务列表页开发:**
    *   antd Table 展示：时间、输入、状态、耗时、Token、操作（查看详情）。
    *   支持分页（调用 `/api/v1/tasks?page=&size=`）。
    *   空状态：antd Empty 组件引导。

### 阶段五：前端任务追踪详情页 (Frontend Task Trace)

**目标:** 详细展示单次 Agent 任务的 Reasoning 和 Tool 调用全过程。

*   **5.1 详情页布局:**
    *   顶部：任务基础信息卡片（ID、输入、状态、耗时、Token 拆解、激活技能）。
    *   底部分 Tab：`执行链 (Traces)`、`最终报告 (Report)`、`原生 JSON (Raw)`。
    *   404 处理：antd Result 组件 + 返回列表按钮。

*   **5.2 执行链可视化 (Traces Tab):**
    *   调用 `GET /api/v1/tasks/:id` 获取 `TaskTrace`。
    *   使用 antd `Timeline` 按 Iteration 展示：
        *   **Thought**：思考过程文本。
        *   **Action**：工具调用折叠面板（Collapse），展开可见：工具名、参数 (JSON)、执行状态、耗时、摘要输出（可折叠）。
        *   **Observation**：执行结果摘要。

*   **5.3 报告预览 (Report Tab):**
    *   使用 `react-markdown` 渲染 `analysis_result` 的 Markdown 内容。

*   **5.4 原生 JSON (Raw Tab):**
    *   展示完整 Trace JSON（可使用 antd Typography.Paragraph copyable）。

## 3. 变更文件清单

| # | 文件 | 操作 | 核心改动 |
|---|------|------|---------|
| 1 | `internal/state/state.go` | MODIFY | 新增 Token 累加字段和方法 |
| 2 | `internal/state/types.go` | MODIFY | 扩展 CommandExecution 字段 |
| 3 | `internal/llm/router.go` | MODIFY | Generate 额外返回 TokenUsage |
| 4 | `internal/agent/diagnosis/decision_node.go` | MODIFY | 收集 Token Usage |
| 5 | `internal/agent/diagnosis/report_node.go` | MODIFY | 收集 Token Usage |
| 6 | `internal/agent/diagnosis/action_node.go` | MODIFY | 填充 CommandExecution 新字段 |
| 7 | `internal/agent/diagnosis/agent.go` | MODIFY | 集成 TraceStore + Trace 写入 |
| 8 | `internal/trace/types.go` | **NEW** | Trace 数据结构定义 + 转换函数 |
| 9 | `internal/trace/recorder.go` | **NEW** | 任务级异步记录器实现 |
| 10 | `internal/trace/events.go` | **NEW** | Trace 事件模型定义 |
| 11 | `internal/trace/sanitizer.go` | **NEW** | 统一敏感字段脱敏 |
| 12 | `internal/store/trace_store.go` | **NEW** | TraceStore 接口与文件实现 |
| 13 | `internal/metrics/collector.go` | **NEW** | Prometheus Metrics 定义与记录函数 |
| 14 | `internal/api/server.go` | **NEW** | Gin REST API Server |
| 15 | `internal/api/errors.go` | **NEW** | 业务错误码与消息集中定义 |
| 16 | `internal/api/response.go` | **NEW** | 统一响应封装函数 |
| 17 | `internal/config/config.go` | MODIFY | 新增 MonitorConfig |
| 18 | `configs/config.yaml` | MODIFY | 新增 monitor 配置段 |
| 19 | `cmd/k8s-monitor/main.go` | **NEW** | 独立 Monitor HTTP Server 入口 |
| 20 | `.gitignore` | MODIFY | 新增 web/node_modules/、web/dist/、data/traces/ |
| 21 | `go.mod` | MODIFY | 新增 gin、gin-contrib/cors、prometheus、uuid 依赖 |
| 22 | `web/` | **NEW** | React 前端项目 |

## 4. 测试与验证

*   执行 `k8s-analyzer "检查 default 命名空间"` 多次（覆盖成功和失败场景）。
*   检查 `data/traces/` 下是否正确生成索引文件和详情 JSON，验证 Token 字段不为零。
*   验证 CLI 在 Trace 写入完成前不会退出；写入完成后索引文件与详情文件同时可见。
*   验证 Trace 中保存的是摘要输出而非原始输出，并验证敏感字段已被脱敏。
*   验证 TaskRecorder 能正确收集 DeepQuery 的逐轮 Token 事件、工具调用事件、任务结束事件，并在 `Close + Wait` 后完整生成 Trace。
*   编译并启动 `go run ./cmd/k8s-monitor`，访问 `http://localhost:8080/metrics` 验证 Prometheus 指标。
*   访问 `http://localhost:8080/api/v1/tasks` 验证 JSON 响应格式（`{code, message, data}`）和分页。
*   验证错误响应：访问不存在的 task_id，确认返回 `{"code": 40400, "message": "resource not found", "data": null}`。
*   启动前端 `cd web && npm run dev`，验证 Dashboard 数据、任务列表、详情页执行链。
