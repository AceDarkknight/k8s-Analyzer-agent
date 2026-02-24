# 计划：使用 Eino 重构 LLM 集成

## 目标
重构 `RuleBasedLLM` 以使用 Eino 框架实现真实的 LLM 能力，特别是使用 `eino-ext/components/model/openai`。这包括实现提示词模板、输出解析器、链式构建和并发分析。

## 优化分析

### 1. Eino 链式集成
*   **当前状态**: `RuleBasedLLM` 使用硬编码字符串和"模拟"逻辑。
*   **优化**: 使用 `compose.NewChain` 构建一个健壮的处理流水线：`Prompt -> ChatModel -> OutputParser`。
*   **优势**: 模块化、更易于测试和更好的错误处理。

### 2. 提示词管理
*   **当前状态**: 在 `AnalyzeError` 中使用字符串拼接。
*   **优化**: 使用 `eino` 的 `PromptTemplate`。
*   **优势**: 代码更清晰，支持少样本示例和动态变量注入。

### 3. 结构化输出解析
*   **当前状态**: `AnalysisResult` 结构体已存在，但没有真正的解析逻辑（它是硬编码的模拟）。
*   **优化**: 使用 `schema.NewOutputParser`（或类似的 Eino 功能）为 `AnalysisResult` 定义预期的 JSON 模式，并自动解析 LLM 的响应。
*   **优势**: 保证 LLM 输出与 Go 结构体结构匹配，减少运行时错误。

### 4. 报告中的并发处理
*   **当前状态**: `ReportNode.analyzeFindings` 顺序遍历 Pod。
*   **优化**: 使用工作池或 `errgroup` 并行分析多个不健康的 Pod。
*   **优势**: 当多个 Pod 失败时，显著减少生成报告的总时间。

## 实施步骤

### 步骤 1：添加依赖
*   将 `github.com/cloudwego/eino` 和 `github.com/cloudwego/eino-ext` 添加到 `go.mod`。

### 步骤 2：定义输出模式和解析器
*   在 `internal/agent/analysis/llm.go` 中，为 `AnalysisResult` 定义 JSON 结构体标签。
*   创建一个针对 `AnalysisResult` 的 Eino `OutputParser`。

### 步骤 3：创建提示词模板
*   为错误分析定义一个 `prompt.ChatTemplate`。
*   包含系统指令（你是 K8s 专家...）和用户指令（分析这个 Pod...）。
*   使用 `pod_name`、`logs`、`events` 等占位符。

### 步骤 4：重构 `RuleBasedLLM`
*   将 `RuleBasedLLM` 重命名为 `EinoLLM`（或保留名称但更改内部实现） - *决策：暂时保留名称 `RuleBasedLLM` 以最小化接口更改，但注意它现在调用真实 LLM。*
*   向 `RuleBasedLLM` 添加字段以保存编译后的 Eino 图或链。
*   在 `NewRuleBasedLLM` 中初始化链：
    ```go
    // 伪代码
    chain := compose.NewChain[ErrorContext, AnalysisResult](
        compose.Downstream(promptNode),
        compose.Downstream(modelNode),
        compose.Downstream(parserNode),
    )
    ```

### 步骤 5：使用链实现 `AnalyzeError`
*   将 `AnalyzeError` 中的硬编码逻辑替换为：
    ```go
    result, err := llm.chain.Invoke(ctx, errorContext)
    ```

### 步骤 6：更新 `ReportNode` 以支持并发
*   修改 `internal/agent/analysis/nodes.go` 中的 `analyzeFindings`。
*   使用信号量或工作池（限制并发数，例如 5）同时为多个 Pod 调用 `AnalyzeError`。
*   线程安全地收集结果。

### 步骤 7：配置
*   确保 `internal/config/llm_config.go` 正确映射到 Eino 的 OpenAI 配置（Key、BaseURL、Model）。

## 验证计划
1.  **单元测试**: 更新 `llm_test.go` 以测试链的构建（如果可能，模拟模型）。
2.  **集成测试**: 在已知有故障 Pod 的集群上运行 Agent，验证 LLM 生成有效的 `AnalysisResult`。
