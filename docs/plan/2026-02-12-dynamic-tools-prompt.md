# 2026-02-12 动态工具获取与 Prompt 优化计划

## 1. 目标
重构 `AnalysisAgent` 和 `SafetyAgent`，使其在启动时分别从 `K8sClient` 和 `ShellClient` 动态获取可用工具列表，并将这些工具信息注入到 LLM 的 Prompt 中，以提高 Agent 对工具理解的准确性和灵活性。

同时，完善 "Downgrade Protection" (降级保护) 机制，确保在工具执行失败时具有运行时重试能力，提高系统的健壮性。**注意：** "Downgrade Protection" 特指运行时的弹性（如重试），与启动时的严格检查（Strict Startup Check）是不同的概念。启动时必须严格检查工具列表获取是否成功，而运行时工具执行失败则应尝试重试。

## 2. 涉及文件
- `internal/client/client.go` (新增 shared Tool definition)
- `internal/client/k8s/client.go` (使用 shared Tool, 检查 ListTools, 实现 CallTool 重试)
- `internal/client/shell/client.go` (使用 shared Tool, 检查 ListTools, 确认 CallTool 重试)
- `internal/agent/analysis/llm.go` (使用 shared Tool, 更新 LLM 接口)
- `internal/agent/analysis/graph.go` (更新 Agent 初始化)
- `internal/agent/safety/validator.go` (更新 LLMAuditor 接口)
- `internal/agent/safety/agent.go` (更新 Agent 初始化)
- `cmd/k8s-analyzer/main.go` (集成)

## 3. 实施步骤

### 阶段 1: 接口与客户端更新 (Phase 1: Interface & Client Updates)

1.  **统一工具定义**:
    - 在 `internal/client/client.go` 中定义通用的 `Tool` 结构体，作为所有 Client 和 Agent 交互的标准类型：
      ```go
      type Tool struct {
          Name        string          `json:"name"`
          Description string          `json:"description"`
          InputSchema json.RawMessage `json:"input_schema"` // 使用 json.RawMessage 优化 Prompt 生成性能
      }
      ```
    - **Refactor**:
        - 修改 `internal/client/k8s/client.go` 使用 `client.Tool` 替代本地定义的 `Tool`。
        - 修改 `internal/agent/analysis/llm.go` 使用 `client.Tool` 替代本地定义的 `ToolDefinition`。
        - 确保 `internal/client/shell/client.go` 的 `ListTools` 返回 `[]client.Tool` (需要从 SDK 的 `mcp.Tool` 转换)。

2.  **实现 Downgrade Protection (运行时重试)**:
    - **Shell Client**: 检查 `internal/client/shell/client.go`，确认 `CallTool` 方法已使用 `client.RetryWithResult` 实现重试逻辑。(已确认实现)
    - **K8s Client**: 修改 `internal/client/k8s/client.go` 中的 `CallTool` 方法，引入 `client.RetryWithResult`，使其具备与 Shell Client 相同的重试能力。这实现了对工具执行的 "Downgrade Protection"。

3.  **更新 LLM 接口**:
    - 修改 `internal/agent/analysis/llm.go` 中的 `LLM` 接口，增加 `SetTools(tools []client.Tool)` 方法。

4.  **更新 LLMAuditor 接口**:
    - 修改 `internal/agent/safety/validator.go` 中的 `LLMAuditor` 接口。
    - 增加 `SetTools(tools []client.Tool)` 方法。

### 阶段 2: Agent 初始化逻辑调整 (Phase 2: Agent Initialization)

1.  **AnalysisAgent 更新**:
    - 修改 `internal/agent/analysis/graph.go` 中的 `NewAgent` 函数。
    - **Strict Startup Check**: 在初始化阶段（或 `LoadTools` 中），必须严格检查 `ListTools` 的返回错误。
    - 如果 `ListTools` 返回错误，记录 Fatal 错误并退出程序 (`log.Fatalf` 或 `os.Exit(1)`)，因为没有工具 Agent 无法正常工作。这与运行时的 Downgrade Protection (重试) 形成互补：启动失败直接退出，运行失败尝试重试。
    - 调用 `llm.SetTools(tools)` 将工具注入 LLM。

2.  **SafetyAgent 更新**:
    - 修改 `internal/agent/safety/agent.go` 中的 `NewAgent` 或提供一个新的初始化方法。
    - 调用 `shellClient.ListTools(ctx)`。
    - **Strict Startup Check**: 如果 `ListTools` 失败，同样需要严格处理，因为 SafetyAgent 依赖工具列表来审计命令。
    - 将 Shell 工具列表传递给 `Validator` 或 `LLMAuditor`。

### 阶段 3: Prompt 工程 (Phase 3: Prompt Engineering)

1.  **AnalysisAgent Prompt**:
    - 在 `internal/agent/analysis/llm.go` (具体实现中，如 `OpenAILLM` 或 `RuleBasedLLM` 的模拟部分) 更新 System Prompt 构建逻辑。
    - 动态生成工具描述部分：遍历工具列表，生成 "Tool Name: Description" 格式的文本或 JSON Schema。
    - 确保 Prompt 明确指示 LLM 可以使用这些工具。

2.  **SafetyAgent Prompt**:
    - 更新 `LLMAuditor` 的 Prompt 构建逻辑。
    - 将从 Shell Client 获取的 `tools` 列表格式化（例如：列出所有可用命令及其参数说明），并嵌入到 System Prompt 中。
    - 指示 Auditor：这些是合法的 Shell 操作集合，用于辅助判断用户意图和潜在风险。

### 阶段 4: 集成与验证 (Phase 4: Integration)

1.  **Main 函数集成**:
    - 更新 `cmd/k8s-analyzer/main.go`。
    - 确保 Client 连接成功后再初始化 Agent（因为需要调用 ListTools），或者在 Agent 内部处理延迟初始化。
    - **建议方案**: 在 `main.go` 中先 Connect Client，成功后获取 Tools，然后传入 `NewAgent`。或者 `NewAgent` 内部 Connect (目前逻辑是传入已创建的 Client)。
    - 鉴于 `ListTools` 需要连接，建议在 `main.go` 中：
        1. Init Clients
        2. Connect Clients
        3. List Tools
        4. Init Agents (passing tools)
    - 或者保持 `NewAgent` 接收 Client，在 `NewAgent` 内部调用 `ListTools` (前提是 Client 已连接或自动连接)。当前 `main.go` 是先 NewAgent 再 Connect，这会导致 `NewAgent` 中调用 `ListTools` 失败。
    - **调整计划**: 需要调整 `main.go` 顺序，先 Connect 再 NewAgent，或者为 Agent 增加 `Init(ctx)` 方法在 Connect 后调用。
    - 考虑到代码改动最小原则，建议为 Agent 增加 `Init(ctx)` 方法，或者在 `Run` 方法首次执行时加载工具（Lazy Loading）。**Lazy Loading** 可能是改动最小的方案，避免大幅修改 `main.go` 结构。

2.  **单元测试**:
    - 为 `AnalysisAgent` 添加测试，验证 `ListTools` 被调用且 Prompt 包含工具信息。
    - 为 `SafetyAgent` 添加测试。
    - 验证 K8s Client 的 `CallTool` 重试逻辑（使用 Mock Server 模拟失败）。

## 4. 详细 Todo List

- [ ] **Phase 1: Interfaces & Clients**
    - [ ] `internal/client/client.go`: 定义 shared `Tool` 结构体，使用 `json.RawMessage` 作为 `InputSchema`。
    - [ ] `internal/client/k8s/client.go`: **Refactor**: 使用 `client.Tool` 替代本地定义的 `Tool`，并在 `ListTools` 中进行正确转换。
    - [ ] `internal/client/shell/client.go`: **Refactor**: 修改 `ListTools` 返回 `[]client.Tool` 并进行正确转换。
    - [ ] `internal/agent/analysis/llm.go`: **Refactor**: 使用 `client.Tool` 替代 `ToolDefinition`。
    - [ ] `internal/client/k8s/client.go`: **Downgrade Protection**: 修改 `CallTool` 方法，使用 `client.RetryWithResult` 实现运行时重试。
    - [ ] `internal/agent/analysis/llm.go`: `LLM` 接口增加 `SetTools` 方法。
    - [ ] `internal/agent/safety/validator.go`: `LLMAuditor` 接口增加 `SetTools` 方法。

- [ ] **Phase 2: Agent Logic**
    - [ ] `internal/agent/analysis/graph.go`: 修改 `Agent` 结构体，增加 `tools []client.Tool` 字段。
    - [ ] `internal/agent/analysis/graph.go`: 实现 `LoadTools(ctx)` 方法。
        - [ ] 从 Client 获取工具列表 (已经统一为 `[]client.Tool`)。
        - [ ] 调用 `llm.SetTools`。
    - [ ] **Critical**: 在 `LoadTools` 中增加错误检查，如果 `ListTools` 失败，调用 `log.Fatalf` 退出 (Strict Startup Check)。
    - [ ] `internal/agent/analysis/graph.go`: 在 `Run` 方法开头检查是否已加载工具，未加载则调用 `LoadTools`。
    - [ ] `internal/agent/safety/agent.go`: 在 SafetyAgent 初始化或运行前，同样实现 `LoadTools` 逻辑。

- [ ] **Phase 3: Prompt Construction**
    - [ ] `internal/agent/analysis/llm.go`: 实现 `FormatToolsPrompt` 逻辑。
    - [ ] 更新具体的 LLM 实现（如 `RuleBasedLLM` 或未来集成的真实 LLM），使其在生成 Prompt 时包含工具列表。
    - [ ] `internal/agent/safety/validator.go`: 实现将工具列表格式化并注入 `LLMAuditor` System Prompt 的逻辑。

- [ ] **Phase 4: Integration**
    - [ ] `cmd/k8s-analyzer/main.go`: 确保 Client 连接后，Agent 能正常工作（通过 Lazy Loading 机制）。
    - [ ] 验证日志中包含 "Loaded X tools" 等信息。

## 5. 验证计划

1.  运行 `cmd/k8s-analyzer/main.go`。
2.  查看日志，确认 `AnalysisAgent` 输出了 "Successfully listed tools" 且数量正确。
3.  (如果是 Mock Client) 确认获取到了 Mock 的工具列表。
4.  检查生成的 Prompt (通过日志 DEBUG 级别) 是否包含了工具描述。
5.  **验证 Downgrade Protection**: 
    - 暂时修改 K8s Client 配置，指向错误的地址或模拟网络不稳定。
    - 触发工具调用，观察日志中是否有 "retrying..." 或类似的重试信息。
    - 确认在重试次数耗尽前如果恢复连接，调用能成功。

