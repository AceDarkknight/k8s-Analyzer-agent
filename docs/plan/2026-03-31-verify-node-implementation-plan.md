# 优化：诊断报告中可执行建议的自动跟进

> 创建日期：2026-03-31
> 最后更新：2026-03-31（明确终版报告重新生成策略）
> 关联架构：[architecture-v2.md](../architecture-v2.md)
> 关联计划：[implementation-plan-v2.md](./implementation-plan-v2.md)

## 问题分析

当前诊断报告（如 fileserver CrashLoopBackOff 案例）中包含了 LLM 生成的 `recommendations`，
其中部分建议附有 **可立即执行的诊断命令**（如 `kubectl get pod -o yaml`、`kubectl exec ... printenv`）。
但当前系统在 `ReportNode` 生成报告后直接结束，这些命令被打印出来但从未被执行，
导致 **诊断停留在"建议阶段"而非"验证阶段"**。

### 当前流程问题点

```
DecisionNode → report → ReportNode → 输出报告 → 结束
                                      ↑
                           报告中含有可执行但未执行的建议命令
```

### 根本原因

1. **DecisionNode 过早结束**：当 LLM 判断"信息充足可以出报告"时，实际上只是"有了初步结论"，还有进一步验证的空间。
2. **Recommendation.Command 从未被执行**：`command` 字段仅作为输出展示，没有闭环回到执行链路。
3. **报告中的"限制"(limitations)字段**暗示了未完成的探查，但系统没有利用这个信息继续工作。

---

## 优化策略

### 核心思路：两阶段诊断 + 建议验证循环

将诊断拆分为两个阶段：

**阶段一（当前已有）：初步诊断**
收集基础信息 → 确定根因假设 → 生成初步报告（含待验证建议）

**阶段二（新增）：建议验证**
对初步报告中标记为"可执行验证"的建议命令，由 agent 自动执行，
将结果合并回诊断上下文，最终生成**终版报告**。

---

## 设计方案

### 方案对比

| 方案 | 描述 | 优点 | 缺点 |
|------|------|------|------|
| **A. Recommendation 分级 + 自动跟进（推荐）** | 在 Recommendation 中增加 `executable` 标记，`ReportNode` 后新增 `VerifyNode` 执行可验证命令 | 架构清晰，与现有 Graph 模型契合 | 需要新增节点和类型字段 |
| B. DecisionNode 增加"verify"决策 | 扩展 decision 类型为 `continue/deep_query/report/verify` | 最小改动 | 混淆了"调查"和"验证"的语义边界 |
| C. 报告后重新启动新的诊断循环 | 将 recommendations 转化为新的 userInput 再次运行 | 完全复用现有逻辑 | 浪费 token，上下文丢失 |

**采用方案 A**：最契合现有架构，语义清晰。

---

## 优化后的 Graph 流程

```
现在：
  ... → DecisionNode → report → ReportNode → 输出（含未执行建议）→ 结束

优化后：
  ... → DecisionNode → report → ReportNode(初步报告)
                                      ↓ 有 executable=true 的建议？
                              NO  → END
                              YES → VerifyNode（执行只读验证命令）
                                      ↓
                              ┌── 轻量判断：验证结果是否包含新信息？
                              │
                              ├── 无新信息（验证全部失败 / 结果与初步结论高度吻合）
                              │       → 结构化补充 VerifyResult 字段 → END（不调 LLM）
                              │
                              └── 有新信息（挂载/路径/权限等与初步结论不同）
                                      → ReportNode(终版报告，Power LLM 完整重新生成)
                                              ↓
                                           END
```

> **终版报告设计决策**：第二次 `ReportNode` 调用让 Power LLM **完整重新生成报告**（而非拼接追加），
> 原因是验证结果可能推翻初步根因判断（例如：初步认为"需要创建目录"，
> 验证后发现"PVC 存在但 volumeMount 路径错误"），完整重新生成才能保证报告内部一致性。
> 当验证结果无新信息时，跳过 LLM 调用以节省成本。

---

## Proposed Changes

### 1. State & Types 层

#### [MODIFY] `internal/state/types.go`

扩展 `Recommendation` 结构，增加可执行性标记：

```go
// Recommendation 修复建议
type Recommendation struct {
    Priority     string // high / medium / low
    Action       string
    Command      string
    Risk         string
    // 新增字段
    Executable   bool   // 是否为只读/安全可执行的验证命令（由 LLM 生成时标注）
    Verified     bool   // 是否已被 agent 验证执行过
    VerifyResult string // 验证执行结果摘要
}
```

#### [MODIFY] `internal/state/state.go`

新增字段和辅助方法：

```go
type State struct {
    // ... 现有字段不变 ...
    VerifyPhase bool // 是否处于建议验证阶段（防止二次循环进入 VerifyNode）
}

// HasExecutableRecommendations 判断是否有待验证的可执行建议
func (s *State) HasExecutableRecommendations() bool {
    if s.AnalysisResult == nil {
        return false
    }
    for _, r := range s.AnalysisResult.Recommendations {
        if r.Executable && !r.Verified {
            return true
        }
    }
    return false
}
```

---

### 2. LLM 层

#### [MODIFY] `internal/llm/prompts.go`

**修改 `synthesizePromptTemplate`**，在 recommendations 的 JSON Schema 中增加 `executable` 字段，
并在报告规则中明确区分"可执行验证命令"和"需人工操作的修复命令"：

```json
"recommendations": [
  {
    "priority": "high / medium / low",
    "action": "建议的操作描述",
    "command": "具体命令（如有）",
    "risk": "操作风险说明",
    "executable": true
  }
]
```

在**报告规则**中新增：

```
- executable 判断规则：
  - true（可执行验证）：
      kubectl get/describe/logs [-o yaml]（只读 K8s 查询，Gateway 动词白名单内）
      kubectl -n X get rs/pvc/pv/configmap/events 等只读资源查询
      df -h / du -sh / cat / grep / free / ps / netstat 等纯只读 Shell 命令
  - false（需人工，VerifyNode 无法执行）：
      kubectl exec（Gateway 动词黑名单明确禁止，MCP 也无法运行 kubectl）
      kubectl edit/patch/apply/delete（写操作）
      mkdir / chmod / mount / umount / systemctl（系统变更）
      任何含管道 | 且目标为 sh/bash 的命令
- command 字段为空时，executable 必须为 false
```

**`BuildSynthesizePrompt` 在 `VerifyPhase=true` 时新增 prompt 段落**：

```go
// 终版报告阶段标识（仅在 VerifyPhase 时注入）
const verifyPhaseHeader = `
## 诊断阶段
**最终验证阶段**：以下"已执行的诊断命令"中包含了对初步建议的自动验证结果。
请综合所有信息生成最终完整报告。
如验证结果与初步结论不符，**以验证结果为准修正根因判断**，确保报告内部一致。
`
```

#### [MODIFY] `internal/llm/parser.go`

同步更新 `recommendationJSON` 结构体，解析新增的 `executable` 字段：

```go
type recommendationJSON struct {
    Priority   string `json:"priority"`
    Action     string `json:"action"`
    Command    string `json:"command"`
    Risk       string `json:"risk"`
    Executable bool   `json:"executable"` // 新增
}
```

---

### 3. Graph 新增 VerifyNode

#### [NEW] `internal/agent/diagnosis/verify_node.go`

```go
// VerifyNode 建议验证节点
// 触发条件：初步报告生成后，存在 Executable=true 且 Verified=false 的建议命令
type VerifyNode struct {
    gateway    *gateway.GatewayClient
    safety     *safety.SafetyAgent
    summarizer *summarizer.OutputSummarizer
    maxVerify  int  // 最多验证的建议条数，默认 3
    fullRegen  bool // 有新信息时是否触发终版 LLM 重新生成，来自 config
}

func NewVerifyNode(gw *gateway.GatewayClient, sa *safety.SafetyAgent,
    sum *summarizer.OutputSummarizer, maxVerify int, fullRegen bool) *VerifyNode

func (n *VerifyNode) Execute(ctx context.Context, s *state.State) (*state.State, error) {
    // 1. 遍历 AnalysisResult.Recommendations，筛选 Executable=true 且 Verified=false 的命令
    // 2. 对每条命令（最多 maxVerify 条），按路由优先级处理：
    //    a. 前置过滤：含 "kubectl exec" → 直接跳过（Gateway 黑名单禁止，MCP 无法运行 kubectl）
    //    b. 尝试解析为 Gateway 结构化请求（verb: get/describe/logs + resource + ns + name）
    //       → 成功：调用 GatewayClient.Execute()
    //    c. 识别为纯 Shell 命令（不含 kubectl，如 df/du/cat/grep）
    //       → 走 SafetyAgent.ExecuteSafeCommand()
    //    d. 无法归类 → 跳过，Verified=false
    // 3. 执行成功：写入 Recommendation.VerifyResult，标记 Verified=true
    // 4. 将验证结果追加到 state.CommandExecutions
    // 5. 调用 needsFullRegeneration() 判断是否需要终版 LLM
    // 6. 设置 state.VerifyPhase = true（表示验证已执行）
    // 7. 设置 state.NeedsFullRegeneration（Graph 分支路由依据）
    // 8. 返回更新后的 state
}

// parseCommandToGatewayRequest 将 kubectl 命令文本解析为 KubectlRequest
// 支持: kubectl [-n ns] get/describe/logs <resource> [name] [-o yaml]
func parseCommandToGatewayRequest(command string) (*gateway.KubectlRequest, bool)

// needsFullRegeneration 纯字符串判断：验证结果是否包含初步报告未覆盖的新信息
func needsFullRegeneration(initialResult *state.AnalysisResult, verifyOutputs []string) bool {
    // 情况1：所有验证命令均执行失败（输出为空）→ 不重新生成
    if allEmpty(verifyOutputs) {
        return false
    }
    // 情况2：提取初步 root_cause + findings.message 关键词，
    // 检查验证输出中是否出现初步报告未提及的新实体
    // （如新的路径、配置键名、PVC名称、错误信息等）
    initialKeywords := extractKeywords(initialResult)
    for _, output := range verifyOutputs {
        if containsNewKeywords(output, initialKeywords) {
            return true
        }
    }
    return false
}
```

**命令routing决策表**：

| 命令模式 | 执行路径 | 说明 |
|---------|---------|------|
| `kubectl -n <ns> get pod <name> -o yaml` | ✅ Gateway | `{verb:get, resource:pods, namespace, name, output:yaml}` |
| `kubectl -n <ns> describe pod <name>` | ✅ Gateway | `{verb:describe, resource:pod, namespace, name}` |
| `kubectl -n <ns> logs <pod>` | ✅ Gateway | `{verb:logs, resource:pod, namespace, name}` |
| `kubectl -n <ns> get rs/pvc/pv/cm/events` | ✅ Gateway | `{verb:get, resource:..., namespace}` |
| `df -h` / `du -sh` / `cat` / `grep` / `free` | ✅ SafetyAgent→MCP | 纯只读 Shell 命令，走安全审计后在节点执行 |
| `kubectl exec ...` | ❌ 强制跳过 | Gateway 动词黑名单；MCP 无法运行 kubectl 命令 |
| `kubectl edit/patch/apply/delete` | ❌ 强制跳过 | 写操作，不在 executable=true 范围内 |
| 管道含 `sh`/`bash` / 命令替换 `$()` | ❌ 强制跳过 | 安全风险，即使 executable=true 也拒绝 |
| 其他无法解析 | ❌ 跳过 | `Verified=false`，保留原始命令在报告中 |

**两种后续行为**：

| 判断结果 | 后续路由 | LLM 调用 |
|---------|---------|---------|
| 有新信息（且 `fullRegen=true`） | `state.NeedsFullRegeneration=true` → graph 路由到 `report` | ✅ 1次 Power LLM |
| 无新信息 / `fullRegen=false` | `state.NeedsFullRegeneration=false` → graph 路由到 `END` | ❌ 不调 LLM |

---

### 4. Graph 编排修改

#### [MODIFY] `internal/agent/diagnosis/graph.go`

在现有 Graph 中插入 VerifyNode，并为 `verify` 后的路由增加两条分支：

```go
// 添加节点
g.AddLambdaNode("info",    infoNode.Execute)
g.AddLambdaNode("decision", decisionNode.Execute)
g.AddLambdaNode("action",   actionNode.Execute)
g.AddLambdaNode("compress", compressNode.Execute)
g.AddLambdaNode("report",   reportNode.Execute)
g.AddLambdaNode("verify",   verifyNode.Execute) // 新增

// 路由1：初步 report 后，检查是否需要进入验证
g.AddBranch("report", func(ctx context.Context, s *state.State) (string, error) {
    if s.HasExecutableRecommendations() && !s.VerifyPhase {
        return "verify", nil
    }
    return compose.END, nil
})

// 路由2：verify 后，根据 NeedsFullRegeneration 决定是否再次 report
g.AddBranch("verify", func(ctx context.Context, s *state.State) (string, error) {
    if s.NeedsFullRegeneration {
        return "report", nil // 触发终版 Power LLM 重新生成
    }
    return compose.END, nil  // 验证结果已结构化补充，直接结束
})
```

同步在 `State` 中新增 `NeedsFullRegeneration bool` 字段供路由判断。

---

### 5. ReportNode 修改

#### [MODIFY] `internal/agent/diagnosis/report_node.go`

终版报告（`VerifyPhase=true`）时，仅需确保 `BuildSynthesizePrompt` 注入验证阶段标识。
验证命令的执行结果已经由 VerifyNode 写入 `state.CommandExecutions`，
会自动出现在 `{command_summary}` 中，无需额外传递。

```go
func (n *ReportNode) Execute(ctx context.Context, s *state.State) (*state.State, error) {
    // 现有逻辑不变，BuildSynthesizePrompt 内部根据 s.VerifyPhase 决定是否注入 verifyPhaseHeader
    prompt := llm.BuildSynthesizePrompt(s)
    // ... 后续调用 Power LLM，解析结果 ...
}
```

`BuildSynthesizePrompt` 伪代码：

```go
func BuildSynthesizePrompt(s *state.State) string {
    // 新增：VerifyPhase 时在 prompt 顶部插入验证阶段标识
    verifyHeader := ""
    if s.VerifyPhase {
        verifyHeader = verifyPhaseHeader // 引导 LLM 以验证结果修正根因
    }
    // ... 其余变量注入与现有逻辑一致 ...
}
```

---

### 6. 配置扩展

#### [MODIFY] `internal/config/config.go`

```go
type AgentConfig struct {
    MaxIterations          int  `yaml:"max_iterations"`
    CompressThreshold      int  `yaml:"compress_threshold"`
    OutputMaxLines         int  `yaml:"output_max_lines"`
    OutputMaxChars         int  `yaml:"output_max_chars"`
    FindingTTLHours        int  `yaml:"finding_ttl_hours"`
    VerifyRecommendations  bool `yaml:"verify_recommendations"`  // 新增，默认 true
    MaxVerifyCommands      int  `yaml:"max_verify_commands"`     // 新增，默认 3
    VerifyFullRegeneration bool `yaml:"verify_full_regeneration"` // 新增，默认 true
}
```

`configs/config.yaml` 增加：

```yaml
agent:
  verify_recommendations: true    # 是否自动执行可验证建议
  max_verify_commands: 3          # 每次最多验证几条建议命令
  verify_full_regeneration: true  # 有新信息时是否完整重新生成终版报告（false=仅结构化补充）
```

---

### 7. 输出展示修改

#### [MODIFY] `cmd/k8s-analyzer/main.go`

在 `printReport` 中区分三类建议展示：

```
建议 (4 项):
  1. [high] ✅ 已验证 - 确认并修复 /kddata 的挂载配置
     验证结果: volumes 配置中未找到 /kddata 对应的 PVC 挂载...
     原始命令: kubectl -n ierp-cluster get pod ... -o yaml (已执行)

  2. [high] ⚠️  需人工操作 - 在容器启动前创建 /kddata
     命令: kubectl edit deploy fileserver ... (请运维人员手动执行)
     风险: 修改 Deployment 可能影响应用启动顺序

  3. [medium] ✅ 已验证 - 检查 disk_url 环境变量配置
     验证结果: disk_url=/kddata，确认为硬编码路径，非环境变量可覆盖
     原始命令: kubectl exec ... printenv (已执行)

  4. [low] 💡 建议优化 - 补充告警与健康检查
     说明: 属于优化类工作，需研发/运维协同调整
```

---

## 关键设计边界

> **不自动执行修复命令**：VerifyNode 只执行只读验证命令（`executable=true`）。
> 所有写操作（edit/patch/delete/apply、文件创建、权限变更等）保持在报告中作为建议，
> 必须由运维人员手动执行。这是核心安全边界，不可妥协。

> **与现有安全机制的关系**：VerifyNode 执行命令仍走 GatewayClient + SafetyAgent 两条路径，
> 不绕过任何安全检查。Gateway 的动词白名单天然防止了非只读操作的误执行。

> **防止无限循环**：`state.VerifyPhase = true` 确保 VerifyNode 只执行一次；
> 终版 ReportNode 执行完后，`report` 节点分支判断 `VerifyPhase=true` 直接路由到 `END`。

---

## 修改文件清单

| 文件 | 类型 | 修改内容 |
|------|------|---------|
| `internal/state/types.go` | MODIFY | `Recommendation` 新增 `Executable/Verified/VerifyResult` |
| `internal/state/state.go` | MODIFY | `State` 新增 `VerifyPhase`、`NeedsFullRegeneration`；新增 `HasExecutableRecommendations()` |
| `internal/llm/prompts.go` | MODIFY | synthesizePromptTemplate 增加 `executable` 字段规则；VerifyPhase 时注入 `verifyPhaseHeader` |
| `internal/llm/parser.go` | MODIFY | `recommendationJSON` 增加 `executable` 字段解析 |
| `internal/agent/diagnosis/verify_node.go` | **NEW** | VerifyNode 完整实现，含命令解析、执行、`needsFullRegeneration` 轻量判断 |
| `internal/agent/diagnosis/graph.go` | MODIFY | 插入 VerifyNode；report 后分支路由；verify 后双路由（有新信息→report，无→END） |
| `internal/agent/diagnosis/report_node.go` | MODIFY | VerifyPhase 时在 prompt 中注入验证阶段 header，引导 LLM 以验证结果修正根因 |
| `internal/config/config.go` | MODIFY | `AgentConfig` 增加 `VerifyRecommendations`、`MaxVerifyCommands`、`VerifyFullRegeneration` |
| `configs/config.yaml` | MODIFY | 新增三个 verify 相关配置项 |
| `cmd/k8s-analyzer/main.go` | MODIFY | `printReport` 区分三类建议（已验证/需人工/建议优化）展示 |

---

## 验证计划

### 自动化测试

- `verify_node_test.go`：Mock GatewayClient，测试各类 kubectl 命令解析逻辑；测试 `needsFullRegeneration` 逻辑
- `graph_test.go`：端到端测试两阶段流程（含 VerifyNode 触发/跳过、有无新信息的全部路径）
- `parser_test.go`：验证 `executable` 字段解析正确性
- `prompts_test.go`：验证 synthesize prompt 在 VerifyPhase 时包含 `verifyPhaseHeader`

### 手动验证场景

1. **有新信息的验证**：fileserver 场景中，`kubectl get pod -o yaml` 输出揭示 volumeMount 路径错误（非单纯"目录不存在"），确认终版报告修正了根因判断
2. **无新信息的验证**：`kubectl exec ... printenv` 输出与初步结论一致，确认跳过第二次 LLM 调用，仅结构化补充 VerifyResult
3. **无可执行建议**：所有 recommendations 均为 `executable=false`，流程直接结束（无 VerifyNode 触发）
4. **配置关闭验证**：`verify_recommendations: false` 时，流程退化为当前行为
5. **配置关闭重新生成**：`verify_full_regeneration: false` 时，即便有新信息也仅做结构化补充
6. **命令解析失败**：无法解析的命令跳过执行，`Verified=false`，报告中正常展示为人工建议
7. **所有验证命令执行失败**：`needsFullRegeneration` 返回 false，沿用初步报告，不额外调用 LLM

---

## 预估工作量

| 组件 | 工时 |
|------|------|
| State/Types 扩展 | 0.5 天 |
| Prompt 修改 + Parser | 0.5 天 |
| VerifyNode 实现（含命令解析 + `needsFullRegeneration` 判断） | 1.5 天 |
| Graph 路由修改（含两条终版路径） | 0.5 天 |
| ReportNode 终版 prompt 注入 | 0.5 天 |
| 配置 + 输出展示 | 0.5 天 |
| 测试 | 0.5 天 |
| **合计** | **~4.5 天** |
