# 根因分析与修复计划 (Fix Tool Calls)

## 1. 故障现象 (Symptoms)
在 ReAct LLM 动态工具调用过程中，发生了大量工具调用失败：
- `list_pods`, `list_deployments`, `list_services` 因使用了错误的参数（如 `all_namespaces`，或缺失必须的 `namespace` 参数）而失败。
- `get_pod_logs` 因为提供了无法识别的参数（如 `name`, `pod`, `container`, `tail` 等）而一直报错。根据 k8s-mcp 官方文档，它实际需要 `pod_name` 和 `namespace` 等特定参数。
- LLM 多次尝试调用 `list_events`、`list_namespaced_events`、`describe_pod` 等未知/不存在的工具。实际上 k8s-mcp 提供的是 `get_events`、`get_resource` 和 `get_resource_yaml`。

## 2. 根本原因 (Root Cause)
通过分析代码和日志，根本原因如下：
1. **工具 Schema 丢失**：在 `internal/agent/analysis/tools.go` 的 `K8sToolAdapter.Info` 方法中，将 `ParamsOneOf` 显式设置为 `nil`。尽管 MCP 协议传递了精确的 `t.inputSchema`（包含了正确的必填参数和可选参数信息），但是适配层没有将其传递给 Eino ReAct 框架。这导致 LLM 在调用工具时处于“盲目”状态，完全不知道每个工具所需的准确参数。
2. **系统提示词误导**：`internal/agent/analysis/react_llm.go` 中的 `getReActSystemPrompt` 提供了硬编码或容易引起误解的示例，没有让 LLM 明确遵循 k8s-mcp 的实际工具列表（如 `get_cluster_status`, `list_namespaces`, `list_pods`, `get_resource`, `get_events`, `get_pod_logs` 等）。LLM 盲目照抄历史习惯或错误的结构，调用了不存在的工具或使用了错误的参数键名（比如用 `name` 而不是 `pod_name`）。

## 3. 修复计划 (Fix Plan)
为了让 LLM 能够准确了解并构造合规的工具调用参数，我们需要将 `K8sClient` (MCP) 的 Input Schema 透明地传递给 LLM 框架，并同步优化系统提示词：

### 3.1 动态适配 MCP 升级 (Dynamic Adaptation to MCP Upgrades)
如果 k8s-mcp 后续升级，新增了接口或修改了参数，本项目无需修改代码即可动态感知并正确调用，其核心原理如下：

1. **动态工具发现 (Dynamic Tool Discovery)**：MCP 协议原生支持 `tools/list` 端点。在 `WrapK8sTools` 初始化阶段，系统会动态调用此接口获取当前 MCP Server 支持的最新工具列表及其原始描述，而非在代码中硬编码工具列表。
2. **JSON Schema 动态解析 (Dynamic Schema Parsing)**：获取到工具列表后，通过 `schema.NewParamsOneOfByJSONSchema(string(t.inputSchema))` 将 MCP 返回的 JSON Schema 实时解析为 LLM 框架所需的参数定义对象（赋值给 `ParamsOneOf`）。这意味着无论 MCP Server 增加了新参数、修改了必填项还是更改了参数类型，LLM 都能在本次会话中接收到最准确的最新参数规范。
3. **消除提示词硬编码 (Eliminating Hardcoded Prompts)**：在修复系统提示词时，彻底移除了针对特定工具参数名称（如 `pod_name` 或 `namespace`）的硬编码示例。这种做法强制 LLM 放弃对历史格式的依赖，转而**完全依赖动态注入的 Schema**进行思考和调用。如果 MCP 工具接口发生变更，LLM 会直接读取新的 Schema 并自行调整调用参数，从而实现对底层服务升级的完全无缝适配。

1. **修改 `internal/agent/analysis/tools.go`**
   - 移除 `ParamsOneOf: nil`。
   - 实现或调用对应的反序列化方法，将 `t.inputSchema` (JSON Schema 格式) 解析为 `schema.ToolInfo` 要求的格式（如使用其注释中暗示的 `schema.NewParamsOneOfByJSONSchema(string(t.inputSchema))` 或等效的 Eino 函数）。
   - 确保 `K8sToolAdapter` 能正确将基于 MCP 的 JSON Schema 映射给底层 LLM 提供商，从而使 LLM 在发起 Tool Call 前便可知晓哪些参数是 `required` 以及参数的具体键名。

2. **更新 ReAct 提示词 (`internal/agent/analysis/react_llm.go`)**
   - 去除所有硬编码的工具名称和参数示例（如 `{"namespace": "...", "pod_name": "..."}`），避免误导 LLM 依赖历史经验。
   - 增加关于“动态工具 Schema”的强约束指导：
     - **强制查阅 Schema**：要求 LLM 在调用任何工具前，必须仔细检查当前注入的可用工具列表及其对应的 JSON Schema。
     - **严禁猜测**：警告 LLM 绝不能凭经验捏造或猜测工具名称（如不要盲目使用 `list_events` 或 `describe_pod`，因为它们可能不存在于当前 MCP Server 提供的列表中）。
     - **精确传参**：强调必须 100% 遵守对应工具 JSON Schema 中定义的必需参数 (required) 和参数键名，不能随意增删或使用错误的键名（如把 `pod_name` 错写成 `name`）。

3. **测试验证**
   - 重新运行 `go test` 或集成测试。
   - 观察 `app.log`，确认 LLM 调用 `get_pod_logs` 时参数键名正确（使用 `pod_name`）、不再报错，且不再尝试调用不存在的 `list_events` 或 `describe_pod`。

## 4. 报告生成优化 (Report Generation Optimization)

### 4.1 当前报告生成机制的不足 (Current Shortcomings)
- **缺乏自然语言总结**：`internal/agent/analysis/nodes.go` 中的 `ReportNode.generateSummary` 只是简单地拼接了基本指标（如 Pod 数量、执行的命令数量），并没有综合分析整个排查过程的逻辑，缺乏可读性强的人类可读结论。
- **包含大量失败和冗余命令**：`state.AnalysisResult.ExecutedCommands` 或推理历史被完整记录并可能暴露在最终输出中，包含了大量由于试错或工具参数错误导致的失败原始命令，严重增加了噪音。
- **结构化不足**：目前虽然定义了 Findings 和 Recommendations，但最后只是简单追加，没有一个统一的由 LLM 驱动的全局综合报告。

### 4.2 优化建议 (Proposed Improvements)

#### 4.2.1 LLM Synthesis 实现细节
1. **扩展 LLM 接口**:
   - 在 `internal/agent/analysis/llm.go` 的 `LLM`接口中添加 `SynthesizeReport(ctx context.Context, state *State) (string, error)` 方法。
   - 在 `MockLLM` 中实现该方法，返回预设的结构化 Markdown。
   - 在 `EinoLLM` (或实际 LLM 实现) 中，构建一个新的 Chain/Prompt 来专门负责综合。
2. **优化 `ReportNode.Execute`**:
   - 修改 `internal/agent/analysis/nodes.go` 中的 `ReportNode.Execute`。
   - 在执行 `analyzeFindings` 之后，调用 `llm.SynthesizeReport`。
   - 将生成的综合报告赋值给 `state.AnalysisResult.Summary`，替代目前的简单拼接逻辑。

#### 4.2.2 噪音命令过滤算法 (Noisy Command Filtering)

**实现位置 (Implementation Location)**:
在 `internal/agent/analysis/nodes.go` 中新增辅助函数 `filterExecutedCommands(commands []CommandExecution) []CommandExecution`。此函数将在 `ReportNode.Execute` 中被调用，用于在生成最终报告前清理冗余数据。

**算法逻辑 (Algorithm Logic)**:

1. **分类判定**: 遍历 `ExecutedCommands` 切片，将命令分为三类：
    - **成功命令 (Success)**: `Success == true`。
    - **试错性失败 (Trial-and-Error Failure)**: `Success == false` 且输出匹配关键词（如 `invalid params`, `unknown tool`, `missing required parameter`）。这通常代表 LLM 在摸索工具参数，属于噪音。
    - **实质性失败 (Real Failure)**: `Success == false` 且输出包含业务错误（如 `Connection Refused`, `Context Deadline Exceeded`, `500 Internal Server Error`）。这些是诊断的关键线索。

2. **去重与折叠 (Deduplication & Folding)**:
    - 维护一个 `map[string]int` 记录每个工具（Tool Name）的连续失败次数。
    - 如果连续出现对同一工具的“试错性失败”：
        - 仅保留**该组最后一次**失败记录。
        - 将其 `Output` 修改为 `[折叠重复失败] 经过多次尝试，最终报错: ` + 原始错误。
    - 如果“试错性失败”之后紧跟着同工具的“成功命令”：
        - 可选：直接剔除该“试错性失败”，因为成功的结果已经覆盖了之前的摸索。

3. **伪代码描述 (Pseudo-code)**:
```go
func filterExecutedCommands(commands []CommandExecution) []CommandExecution {
    filtered := make([]CommandExecution, 0)
    for i := 0; i < len(commands); i++ {
        cmd := commands[i]
        // 1. 保留所有成功命令
        if cmd.Success {
            filtered = append(filtered, cmd)
            continue
        }
        // 2. 识别实质性业务错误，必须保留
        if isBusinessError(cmd.Output) {
            filtered = append(filtered, cmd)
            continue
        }
        // 3. 处理试错性失败：寻找连续同工具失败的终点
        lastIdx := i
        for j := i + 1; j < len(commands); j++ {
            if isSameTool(commands[j], cmd) && isTrialError(commands[j].Output) {
                lastIdx = j
            } else {
                break
            }
        }
        // 如果有折叠，修改最后一次失败的描述
        if lastIdx > i {
            finalFail := commands[lastIdx]
            finalFail.Output = fmt.Sprintf("[经过 %d 次尝试后失败]: %s", lastIdx-i+1, finalFail.Output)
            filtered = append(filtered, finalFail)
        } else {
            filtered = append(filtered, cmd)
        }
        i = lastIdx // 跳过已处理的折叠项
    }
    return filtered
}
```

**数据流 (Data Flow)**:
1. `ReportNode.Execute` 获取 `state.AnalysisResult.ExecutedCommands`。
2. 调用 `filterExecutedCommands` 得到清理后的列表。
3. 将清理后的列表、`Findings` 和 `K8sInfo` 汇总。
4. 调用 `llm.SynthesizeReport(ctx, filteredData)`。
5. 将 LLM 生成的 Markdown 文本回填至 `state.AnalysisResult.Summary`。

#### 4.2.3 结构化输出 Prompt 模板
`SynthesizeReport` 将使用如下 Prompt 结构：

```markdown
# Role
你是一个资深的 Kubernetes 运维专家，负责根据排查过程生成最终诊断报告。

# Input Data
- 用户原始查询: {{.UserInput}}
- 关键发现 (Findings): {{.Findings}}
- 核心执行步骤 (Filtered Commands): {{.Commands}}
- 资源状态摘要: {{.K8sSummary}}

# Output Format (Strict Markdown)
请按以下结构输出报告：

## 1. Summary (执行摘要)
[一句话总结诊断结论，说明问题是否已解决或定位到根因]

## 2. Findings (详细发现)
[按严重程度(Critical/High/Medium)列出所有技术发现，需引用具体的资源名称和错误信息]

## 3. Recommendations (修复建议)
[列出具体的、可执行的建议。如果有具体的修复命令（如 kubectl patch/edit），请提供代码块]
```
