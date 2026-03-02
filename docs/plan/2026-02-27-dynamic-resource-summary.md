# 2026-02-27 重构 K8s 资源追踪和摘要生成逻辑 (统一切片存储方案)

## 背景
用户建议将 `Resources` 统一为切片存储，而不是同时支持单个对象和切片。这种统一简化了节点逻辑和报告生成的消费过程，使行为更加可预测。

## 目标
1. `K8sInfo` 在 `internal/agent/analysis/state.go` 中使用 `map[string][]any` 统一存储 K8s 资源。
2. 即使是单个对象（如单条日志），在存入时也封装为长度为 1 的切片。
3. 简化 `GetSummary()` 逻辑，直接使用切片长度（`len`）进行计数，移除复杂的反射检测。
4. 确保增加新资源类型时，系统依然保持零配置自动化汇总。

## 修改方案

### 1. 状态结构重构 (`internal/agent/analysis/state.go`)

- **K8sInfo 结构体更新**:
  废弃/替换原有的固定资源字段，引入通用的 **切片映射** 存储。
  ```go
  type K8sInfo struct {
      Namespace string
      // Resources 统一存储为切片。Key 是资源类型（如 "Pods", "Deployments"）。
      // 即使是单个对象也会被封装为长度为 1 的切片。
      Resources map[string][]any
  }
  ```

- **添加状态辅助方法**:
  - `SetResources(resourceType string, items ...any)`:
    - 直接将资源类型设置为提供的 `items` 切片。
    - 这里的 `...any` 变长参数使得调用方可以传入多个对象，或者使用 `slice...` 语法传入一个切片。
  - `AppendResource(resourceType string, items ...any)`:
    - 向 `Resources[resourceType]` 切片中追加一个或多个 `items`。
    - 负责切片的初始化。
  - `GetSummary() string`: 核心动态生成方法。
    - 获取 `Resources` 的所有 Key 并排序以保证输出顺序。
    - 遍历每个资源，直接使用 `len(slice)` 获取计数。
    - 拼接成最终摘要，例如：`"命名空间: default, Pods: 12, Deployments: 2, Logs: 100"`。

### 2. 节点逻辑重构 (`internal/agent/analysis/nodes.go`)

- **InfoNode**:
  - 调用 `state.K8sInfo.SetResources("Pods", allPods...)`。通过 `...` 语法将切片展开传入。
  
- **ActionNode**:
  - 当工具返回单个资源（如特定 Pod 的详情或某条日志）时，调用 `state.K8sInfo.AppendResource("Logs", logInfo)`。
  - 由于使用了变长参数，单个对象会被自动包装在切片中传入，内部无需再做复杂的反射判断。

- **ReportNode**:
  - 调用 `state.K8sInfo.GetSummary()` 获取由切片长度动态计算出的汇总信息。

### 3. 统一存储的优势 (Rationale)
- **改善开发体验 (DX)**: 使用变长参数 (`...any`) 是 Go 语言中处理集合或可选参数的惯用方式（idiomatic Go），提高了代码的易读性和灵活性。
- **简化消费逻辑**: 消费方（如报告生成或后续分析节点）可以确信每个资源类型对应的总是一个集合，无需通过反射区分“单数”还是“复数”。
- **性能与可靠性**: `SetResources` 和 `AppendResource` 不再需要使用反射（reflection）来探测输入是否为切片，因为变长参数确保了输入始终是一个切片。`GetSummary` 同样直接读取切片长度，逻辑更清晰且效率更高。
- **一致性**: 无论资源是批量拉取的还是增量追加的，其在内存中的物理结构保持一致。

## 架构图
```mermaid
graph TD
    A[InfoNode/ActionNode] -->|Set/Append| B[K8sInfo.Resources map - map string []any]
    B -->|len| C{K8sInfo.GetSummary}
    C -->|Dynamic String| D[ReportNode]
    D -->|Synthesize| E[Final Report]
```

## 预期效果
- **极高的可扩展性**: 增加新资源支持仅需修改收集或解析逻辑，展示层完全自动化。
- **预测性**: 统一的数据结构减少了运行时类型错误的可能性。

## 验证计划
- **单元测试**:
  - 在 `state_test.go` 中测试 `SetResources` 的自动包装功能（输入单对象 -> 输出长度为 1 的切片）。
  - 测试 `AppendResource` 在多次调用后的切片增长。
  - 验证 `GetSummary` 输出的计数与切片长度精确匹配。
- **手动验证**:
  - 模拟一次 API 调用返回单个 Ingress 对象，确认摘要中显示 `Ingress: 1`。
