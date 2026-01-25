# Analysis Agent

基于 Eino 框架的 K8s 集群分析 Agent，使用 Graph 编排协调 K8s Client 和 Safety Agent。

## 目录结构

```
internal/agent/analysis/
├── state.go          # Graph 状态定义
├── nodes.go          # Graph 节点实现
├── llm.go            # LLM 接口和实现
├── graph.go          # Graph 编排逻辑
├── agent_test.go     # 单元测试
└── README.md         # 本文档
```

## 核心组件

### 1. State (state.go)

定义 Graph 的状态结构，包含以下字段：

```go
type State struct {
    UserInput string              // 用户输入
    K8sInfo *K8sInfo          // K8s 集群信息
    AnalysisResult *AnalysisResult // 分析结果
    IterationCount int           // 当前迭代次数
    MaxIterations int            // 最大迭代次数（默认 10）
    LastAction string            // 最后执行的操作
    LastError error             // 最后的错误
}
```

**关键方法**：
- `IncrementIteration()`: 增加迭代计数
- `ShouldContinue()`: 判断是否应该继续执行
- `AddFinding()`: 添加发现的问题
- `AddRecommendation()`: 添加建议
- `AddCommandExecution()`: 记录命令执行

### 2. Graph Nodes (nodes.go)

实现四个核心节点：

#### InfoNode（信息收集节点）
- 调用 K8s Client 获取集群/资源信息
- 收集 Pod、Service、Deployment、Event 信息
- 从用户输入中提取命名空间

#### DecisionNode（决策节点）
- 分析当前信息，决定下一步行动
- 使用 LLM 进行决策
- 支持规则引擎降级

#### ActionNode（行动节点）
- 调用 Safety Agent 执行命令
- 解析命令输出并更新状态
- 处理命令执行失败的情况

#### ReportNode（报告生成节点）
- 汇总信息生成最终报告
- 分析发现的问题
- 生成建议

### 3. LLM (llm.go)

#### LLM 接口
```go
type LLM interface {
    MakeDecision(ctx context.Context, state *State) (Decision, error)
    Analyze(ctx context.Context, state *State) (string, error)
    GenerateReport(ctx context.Context, state *State) (string, error)
}
```

#### RuleBasedLLM（基于规则的 LLM）
- 使用预定义规则进行决策
- 支持优先级排序的规则匹配
- 默认规则包括：
  1. `max_iterations_reached`: 达到最大迭代次数 → Report
  2. `pod_error_detected`: 检测到错误 Pod → DeepQuery
  3. `pod_high_restarts`: Pod 重启次数过多 → DeepQuery
  4. `warning_events_detected`: 检测到警告事件 → DeepQuery
  5. `error_occurred`: 发生错误 → Report
  6. `has_enough_info`: 收集到足够信息 → Report
  7. `default_continue`: 默认继续 → DeepQuery

#### MockLLM（模拟 LLM）
- 用于测试和演示
- 返回预设的响应

#### CommandGenerator（命令生成器）
- 根据当前状态生成要执行的命令
- 支持查看日志、描述 Pod、网络测试等命令

### 4. Graph Orchestration (graph.go)

#### Graph 流程
```
START → Info → Decision → Action → Decision → ... → Report → END
                    ↓         ↓
                  Report    (循环)
```

#### Graph 构建
```go
func (a *Agent) buildGraph() error {
    // 创建 Graph
    g := compose.NewGraph[*State, *AnalysisResult]()

    // 添加节点
    g.AddLambdaNode(NodeInfo, ...)
    g.AddLambdaNode(NodeDecision, ...)
    g.AddLambdaNode(NodeAction, ...)
    g.AddLambdaNode(NodeReport, ...)

    // 添加边
    g.AddEdge(compose.START, NodeInfo)
    g.AddEdge(NodeInfo, NodeDecision)
    g.AddEdge(NodeAction, NodeDecision)
    g.AddEdge(NodeReport, compose.END)

    // 添加分支
    g.AddBranch(NodeDecision, compose.NewGraphBranch(
        func(ctx context.Context, state *State) (string, error) {
            decision := Decision(state.LastAction)
            switch decision {
            case DecisionContinue, DecisionDeepQuery:
                return NodeAction, nil
            case DecisionReport, DecisionError:
                return NodeReport, nil
            }
        },
        map[string]bool{NodeAction: true, NodeReport: true},
    ))

    // 编译 Graph
    compiledGraph, err := g.Compile(context.Background(), compose.WithMaxRunSteps(10))
}
```

#### Decision 类型
```go
const (
    DecisionContinue  Decision = "continue"   // 继续执行命令
    DecisionDeepQuery Decision = "deep_query" // 深入查询
    DecisionReport   Decision = "report"     // 生成报告
    DecisionError    Decision = "error"      // 发生错误
)
```

### 5. 单元测试 (agent_test.go)

包含以下测试：
- `TestNewState`: 测试 State 创建
- `TestIncrementIteration`: 测试迭代计数增加
- `TestShouldContinue`: 测试是否应该继续执行
- `TestAddFinding`: 测试添加发现
- `TestAddRecommendation`: 测试添加建议
- `TestRuleBasedLLM`: 测试基于规则的 LLM
- `TestMockLLM`: 测试 Mock LLM
- `TestCommandGenerator`: 测试命令生成器
- `TestNewAgent`: 测试 Agent 创建
- `TestAgentRun`: 测试 Agent 运行
- `TestAgentRunWithMaxIterations`: 测试达到最大迭代次数
- `TestParseUserQuery`: 测试用户查询解析
- `TestGraphFlow`: 测试 Graph 流转逻辑
- `TestStatePersistence`: 测试状态持久化
- `TestTimeoutHandling`: 测试超时处理

## 使用示例

```go
// 创建 Agent
k8sClient := k8s.NewMockClient(k8s.Config{})
safetyAgent := NewMockSafetyAgent()
agent, err := NewAgent(k8sClient, safetyAgent, nil)

// 运行分析
result, err := agent.Run(ctx, "分析 nginx 服务")

// 查看结果
fmt.Printf("Status: %s\n", result.Status)
fmt.Printf("Summary: %s\n", result.Summary)
fmt.Printf("Findings: %d\n", len(result.Findings))
fmt.Printf("Recommendations: %d\n", len(result.Recommendations))
```

## 设计原则

1. **OODA 循环**: 遵循 Observe-Orient-Decide-Act 循环模式
2. **状态管理**: 通过 State 结构体在节点间传递状态
3. **决策分离**: 决策逻辑封装在 LLM 接口中
4. **错误处理**: 支持规则引擎降级，确保系统稳定性
5. **可测试性**: 使用接口和 Mock 便于单元测试
6. **可扩展性**: 支持自定义 LLM 实现和决策规则

## 依赖

- `github.com/cloudwego/eino`: Eino 框架，用于 Graph 编排
- `github.com/your-org/k8s-analyzer-agent/internal/client/k8s`: K8s 客户端
- `github.com/your-org/k8s-analyzer-agent/internal/agent/safety`: 安全 Agent
