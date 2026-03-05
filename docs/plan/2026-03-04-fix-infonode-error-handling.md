# 计划文档：优化 InfoNode 错误处理机制

## 背景
在当前的实现中，`InfoNode` 调用 `collectNamespaces` 时，如果发生错误（如网络连接失败或 MCP 返回错误内容），会直接返回错误并中断整个 Graph 的执行。这导致在某些命名空间不可访问或临时网络抖动时，分析任务无法继续。

为了提高系统的鲁棒性，需要优化 `InfoNode` 的错误处理逻辑，使其能够区分错误类型，并在非致命错误下保持流程运行。

## 方案目标
1. **区分错误类型**：能够从 `CallTool` 的返回结果中准确区分基础设施级错误与业务逻辑级错误。
2. **非阻塞执行**：在 `InfoNode` 中捕获业务逻辑级错误，记录状态后允许 Graph 继续执行。对于基础设施级错误，维持中断行为。
3. **增强 LLM 感知**：将业务逻辑错误信息透传至 `state`，确保后续节点（尤其是 LLM 决策节点）能感知并决定后续行动（跳过或重试）。

## 详细设计方案

### 1. 错误类型细化

#### 1.1 基础设施级错误 (Infrastructure Errors)
- **定义**：MCP 客户端本身无法完成对 Server 的请求，或者底层网络发生故障。这意味着基础设施不可用，后续尝试通常无效。
- **识别方式**：`k8sClient.CallTool` 直接返回非空的 `error` 对象。
- **常见子类**：
    - `ConnectionError`：连接被拒、网络不可达。
    - `ClientNotConnected`：MCP 客户端未就绪或已断开。
    - `TimeoutError`：上下文 (Context) 超时。
    - `ToolExecutionError`：底层重试机制耗尽后仍失败。
- **处理策略**：**阻塞式中断**。Graph 直接返回 `err` 并停止执行。

#### 1.2 业务逻辑级错误 (Business/Logic Errors)
- **定义**：MCP 客户端成功与 Server 通信，但工具执行逻辑出错，或返回内容无法解析。这类错误通常是局部的或权限相关的。
- **识别方式**：
    - `k8sClient.CallTool` 返回的 `error` 为 `nil`。
    - 但 `CallToolResult.ToolHasError` 为 `true`。
    - 或者后续 `ParseToolResult` 解析 JSON 失败（例如返回了纯文本错误信息）。
- **常见子类**：
    - **权限不足 (RBAC Forbidden)**：当前 ServiceAccount 无权访问某些资源。
    - **参数非法**：传递给 MCP Tool 的参数不符合要求。
    - **资源不存在**：指定的命名空间不存在。
    - **解析失败**：Server 返回了非预期的格式。
- **处理策略**：**非阻塞记录**。
    - 将失败记录到 `state.AnalysisResult.ExecutedCommands`，状态设为 `failed`。
    - 在 `state.Findings` 中添加一条 `Warning` 级别的记录。
    - 将错误详情注入 Prompt，交给 LLM 决定是跳过还是对特定命名空间重试。

### 2. 核心结构体修正

在 `internal/client/k8s/client.go` 中，当前的 `CallToolResult` 结构体缺少 `ToolHasError` 字段，导致无法感知 MCP 协议层面的业务错误（即 Server 返回了 200 OK 但内容标记为错误）。

需要进行如下修改：

1.  **修改 `CallToolResult` 结构体**：
    ```go
    // CallToolResult 工具调用结果
    type CallToolResult struct {
        Content []Content
        ToolHasError bool // 新增字段：标识工具执行是否发生业务错误
    }
    ```

2.  **更新 `CallTool` 方法**：
    在 `internal/client/k8s/client.go` 的 `CallTool` 方法中，将 SDK 返回的 `IsError` 字段透传给 `CallToolResult`。
    - 在调用 SDK 的 `CallTool` 后，获取返回的 `mcp.CallToolResult`。
    - 将 `mcp.CallToolResult.IsError` 的值赋给 `CallToolResult.ToolHasError`。

3.  **更新 `CallToolResultToMcp` 方法**：
    在 `internal/client/k8s/tools.go` 中，更新 `CallToolResultToMcp` 函数。
    - 当前实现可能硬编码了 `IsError: false`。
    - 修改为使用 `result.ToolHasError` 来设置 `mcp.CallToolResult.IsError`。

### 3. collectNamespaces 识别逻辑伪代码

```go
func collectNamespaces(ctx context.Context, k8sClient K8sClient, state *AnalysisState) error {
    // 1. 调用 MCP Tool
    result, err := k8sClient.CallTool(ctx, "list_namespaces", nil)
    
    // 2. 识别基础设施级错误
    if err != nil {
        // 识别为 ConnectionError/TimeoutError 等
        // 处理策略：直接中断
        return fmt.Errorf("infrastructure error: %w", err)
    }

    // 3. 识别业务逻辑级错误 (ToolHasError = true)
    if result.ToolHasError {
        msg := fmt.Sprintf("business logic error from MCP: %s", result.Content)
        // 处理策略：记录到 state，不中断
        recordNonFatalError(state, "list_namespaces", msg)
        return nil 
    }

    // 4. 识别解析解析级错误 (属于业务逻辑错误范畴)
    var namespaces []string
    err = json.Unmarshal([]byte(result.Content), &namespaces)
    if err != nil {
        msg := fmt.Sprintf("parse error: failed to unmarshal result: %v", err)
        // 处理策略：记录到 state，不中断
        recordNonFatalError(state, "list_namespaces", msg)
        return nil
    }

    // 成功获取数据...
    state.Namespaces = namespaces
    return nil
}

func recordNonFatalError(state *AnalysisState, cmd string, errMsg string) {
    // 记录到执行历史
    state.AnalysisResult.ExecutedCommands = append(state.AnalysisResult.ExecutedCommands, ExecutedCommand{
        Command: cmd,
        Status:  "failed",
        Output:  errMsg,
    })
    // 添加到发现项
    state.Findings = append(state.Findings, Finding{
        Type:     "Warning",
        Message:  fmt.Sprintf("Step '%s' failed: %s", cmd, errMsg),
        Severity: "Medium",
    })
}
```

### 3. InfoNode 非阻塞处理流程
修改 `internal/agent/analysis/nodes.go` 中的 `InfoNode` 函数：
- 调用 `collectNamespaces`。
- 如果返回 `err != nil`（即基础设施错误），则 `InfoNode` 返回该错误，Graph 中断。
- 如果返回 `nil`，即使内部发生了业务错误（已记录到 `state`），流程依然走向 `DecisionNode`。

### 4. LLM 感知与决策
在 `react_llm.go` 的 Prompt 构建中，包含 `state.AnalysisResult.ExecutedCommands` 中标记为 `failed` 的条目。
LLM 会看到类似：
> 系统尝试执行 `list_namespaces` 失败，原因：`RBAC: user cannot list namespaces`。

LLM 可以据此调整策略，例如尝试直接访问默认命名空间 `default`。

## 实现步骤
1. **定义错误分类**：在 `internal/client/k8s/client.go` 中明确 `CallTool` 的返回预期。
2. **重构 collectNamespaces**：按照伪代码逻辑实现错误区分。
3. **更新 InfoNode**：确保其能够正确处理 `collectNamespaces` 返回的 `nil`（代表已处理的非致命错误）。
4. **单元测试**：
    - 模拟 `CallTool` 返回底层 `error` -> 验证 Graph 中断。
    - 模拟 `CallTool` 返回 `ToolHasError: true` -> 验证 Graph 继续且 `state` 已更新。

## 预期效果
- 当 MCP Server 连接断开时，系统能立即感知并报错停止。
- 当由于权限问题无法列出所有命名空间时，系统能记录警告并在 LLM 的指导下继续尝试其他操作。
- 最终报告能够准确反映哪些操作由于业务原因未能成功。
