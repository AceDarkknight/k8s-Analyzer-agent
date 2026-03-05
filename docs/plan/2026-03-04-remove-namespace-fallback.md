# 移除命名空间回退逻辑修改计划

## 背景
当前 `internal/agent/analysis/nodes.go` 中的 `InfoNode` 在获取命名空间失败时会回退到硬编码的列表（`default`, `kube-system`）。为了增强系统的灵活性并让 LLM 能够更好地处理异常情况，我们需要移除这些回退逻辑。如果无法获取命名空间，系统应将错误返回给上层，由 LLM 决定下一步如何执行。

## 修改目标
1. 移除 `InfoNode.Execute` 中获取命名空间失败时的手动设置回退逻辑。
2. 移除 `InfoNode.collectNamespaces` 中所有硬编码的回退命名空间列表。
3. 清理相关的日志记录和状态更新（如 `state.AddFinding` 中的警告）。

## 详细实现步骤

### 1. 修改 `InfoNode.Execute` (L62-L77)
- 移除对 `collectNamespaces` 返回错误的捕获和回退处理。
- 如果 `collectNamespaces` 返回错误，不再手动设置 `namespaces = []string{"default"}`。
- 直接返回错误：`if err != nil { return state, err }`。
- 移除关于使用回退列表的 `state.AddFinding` 调用。

### 2. 修改 `InfoNode.collectNamespaces` (L174-L213)
- 移除 `!n.hasTool(ctx, "list_namespaces")` 时的回退逻辑，改为返回错误。
- 移除 `n.k8sClient.CallTool` 调用失败后的回退逻辑，直接返回错误。
- 移除解析结果为空 (`len(namespaces) == 0`) 时的回退逻辑，返回相应的错误。
- 虽然方法签名 `([]string, bool, error)` 保持不变以减少对其他地方的影响，但 `bool` 返回值（`usedFallback`）将始终设为 `false`。

### 3. 日志与清理
- 更新相关的 `logger` 调用，移除提及 "fallback" 或 "hardcoded list" 的日志。
- 确保所有错误路径都能清晰地记录失败原因，以便 LLM 或操作员诊断。

## 预期效果
- 当 K8s MCP 服务器无法正常工作或权限不足导致无法获取命名空间列表时，`InfoNode` 将不再静默回退。
- 错误将透传至 Graph，LLM 可以识别到 `list_namespaces` 失败，并根据上下文决定是重试、报错还是尝试直接操作特定的命名空间。
- 系统行为更加透明，减少了隐式的硬编码配置。
