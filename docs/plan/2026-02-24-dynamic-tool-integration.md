# 计划：在 LLM 分析中启用动态工具调用（ReAct 模式）

## 目标
去除基于规则的 LLM (`RuleBasedLLM`)，全面启用基于 Eino 框架的 ReAct Agent。
主 Agent 将使用 `ReActLLM` 进行分析和工具调用。
**关键变更**：如果 `ReActLLM` 初始化失败，**不再降级**到规则引擎，而是直接报错退出（Fatal）。

## 架构变更

### 1. 工作流程设计

**新流程**:
1.  **InfoNode**: 先收集所有基础数据（Namespaces, Pods, Events, Logs 等）整合到 `State`。
2.  **ReportNode**: 直接调用 `ReActLLM.AnalyzeError`。
3.  **ReActLLM**:
    *   接收已收集的数据作为初始上下文。
    *   使用 Eino ReAct Agent 进行推理和行动。
    *   **如果需要更多信息**: 动态调用 K8S MCP 工具或 Safety Agent 工具。
    *   **如果不需要/分析完成**: 生成最终 JSON 分析结果。

**注意**: 原有的 `DecisionNode` 和 `ActionNode` 构成的外部循环将被移除，因为 ReAct Agent 内部已经包含了推理-行动循环。

### 2. 组件变更

#### 删除 RuleBasedLLM
*   删除 `internal/agent/analysis/llm.go` 中的 `RuleBasedLLM` 结构体及其相关方法。
*   删除 `internal/agent/analysis/llm_test.go`。
*   保留 `LLM` 接口定义（如果需要）或重构接口以适应新模式。

#### ReActLLM (核心)
*   位置: `internal/agent/analysis/react_llm.go`
*   功能: 封装 `github.com/cloudwego/eino/flow/agent/react`。
*   工具集成: 使用适配器模式将 `K8sClient` (MCP) 和 `SafetyAgent` 包装为 Eino Tools。

#### Graph (编排)
*   位置: `internal/agent/analysis/graph.go`
*   简化 Graph 结构: `Start -> Info -> Report -> End`。
*   移除 `DecisionNode` 和 `ActionNode`。

### 3. 工具封装（适配器模式）
我们需要将现有的 `k8sClient` 和 `safetyAgent` 适配为 Eino 兼容的工具。

*   **接口**: `github.com/cloudwego/eino/components/tool.BaseTool`。
*   **实现**:
    *   **K8sToolAdapter**: 动态适配器，将 `k8sClient.ListTools()` 的结果转换为 Eino 工具。
    *   **SafetyAgentToolAdapter**: 将 `safetyAgent` 包装为一个工具 ("execute_safe_command")。

## 实施步骤

### 步骤 1：定义工具适配器
创建或更新 `internal/agent/analysis/tools.go`。

```go
// WrapK8sTools 将 MCP 工具转换为 Eino 工具
// 注意：如果从 MCP 列出工具失败，应该直接退出程序（Fatal 错误）
func WrapK8sTools(k8sClient K8sClient) ([]tool.BaseTool, error) {
    // ... 实现同前 ...
}

// WrapSafetyAgent 将 SafetyAgent 包装为 Eino 工具
func WrapSafetyAgent(safetyAgent SafetyAgent) tool.BaseTool {
    // ... 实现同前 ...
}
```

### 步骤 2：实现 ReActLLM
创建或更新 `internal/agent/analysis/react_llm.go`。

```go
// ReActLLM 基于 Eino ReAct Agent 构建的分析器
type ReActLLM struct {
    agent      *react.Agent
    // ...
}

// NewReActLLM 初始化
func NewReActLLM(ctx context.Context, chatModel model.ChatModel, k8sClient K8sClient, safetyAgent SafetyAgent) (*ReActLLM, error) {
    // 1. 封装工具
    // 2. 创建 ReAct Agent
    // ...
}

// AnalyzeError 执行分析
func (llm *ReActLLM) AnalyzeError(ctx context.Context, errorContext ErrorContext) (AnalysisResult, error) {
    // ... 构建 Prompt，调用 Agent，解析结果 ...
}
```

### 步骤 3：重构 Graph 和移除旧代码
1.  **修改 `internal/agent/analysis/graph.go`**:
    *   移除 `NewRuleBasedLLM` 调用。
    *   在 `NewAgent` 中，必须成功初始化 `ReActLLM`，否则返回 Error 或 Fatal。
    *   重新构建 Graph，只包含 `InfoNode` 和 `ReportNode`。
    *   移除 `DecisionNode` 和 `ActionNode` 的相关代码和引用。

2.  **修改 `internal/agent/analysis/nodes.go`**:
    *   `ReportNode` 直接使用 `ReActLLM` (或通过接口)。
    *   可以移除 `DecisionNode` 和 `ActionNode` 结构体及其方法。

3.  **清理文件**:
    *   删除 `internal/agent/analysis/llm.go` 中关于 `RuleBasedLLM` 的代码。
    *   删除 `internal/agent/analysis/llm_test.go`。

### 步骤 4：主程序入口调整
检查 `cmd/k8s-analyzer/main.go` (或 `graph.go` 中的初始化逻辑)，确保在 LLM 初始化失败时直接退出。

## 风险与缓解措施
*   **ReAct 循环控制**: 确保 `MaxStep` 设置合理（如 10），防止死循环。
*   **工具执行失败**: ReAct Agent 能够感知工具执行错误并尝试自我修正或报告错误。
*   **Prompt 优化**: 需要精心设计 System Prompt 以充分利用 InfoNode 已收集的数据，避免重复查询。
