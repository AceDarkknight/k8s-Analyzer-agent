# 诊断循环优化计划

## 背景

对真实 K8s 集群执行全局诊断，总耗时约 3 分钟，主诊断阶段用满 10 轮迭代才生成报告，验证阶段也超出 max_verify_iterations 限制才强制停止。日志分析发现大量重复工具调用和无效迭代，以下为具体问题与优化方案。

---

## P0-1：工具调用重复执行，无缓存/去重机制

### 问题

从日志统计，同一次诊断流程中：

| 工具调用 | 重复次数 | 每次返回数据量 |
|---------|---------|-------------|
| `describe_node <node>` | ~8 次 | ~5KB |
| `describe_pod <ns>/<pod>` | ~7 次 | ~4KB |
| `get_pod_events <ns>` | ~8 次 | 0 bytes（每次都为空） |
| `get_nodes` | ~5 次 | ~110 bytes |

相同参数的请求被反复发送到 Gateway，产生了约 **30+ 次无效网络请求**。

### 原因

`ActionNode.executeToolCall()` 每次收到 `ToolCall` 后直接调用 Gateway，没有任何缓存层。LLM 在每轮决策中因看不到之前的完整结果（见 P0-2），不断生成相同的工具调用指令。

相关代码位置：
- `internal/agent/diagnosis/action_node.go` — `executeToolCall()` 方法（第 190-268 行）

### 优化方案

#### 1. 扩展 store 模块，新增 `ToolCacheStore` 接口

现有 `internal/store` 模块已有 `FindingStore` 接口（`HasFinding`/`SaveFinding`，bool 类型），以及 `MemoryStore`（基于 `ttlcache`）和 `RedisStore`（基于 `go-redis`）两个实现。但 `FindingStore` 只支持 bool 类型存取，无法存储工具调用的 string 结果，因此需要新增一个 `ToolCacheStore` 接口。

底层基础设施可复用：`MemoryStore` 的 `ttlcache` 库和 `RedisStore` 的 `redis.Client` 连接。

```go
// internal/store/tool_cache_store.go

// ToolCacheStore 工具调用结果缓存接口
type ToolCacheStore interface {
    // Get 获取缓存的工具调用结果，不存在返回 ("", false, nil)
    Get(ctx context.Context, key string) (string, bool, error)
    // Set 缓存工具调用结果，带 TTL
    Set(ctx context.Context, key string, value string, ttl time.Duration) error
    // Close 关闭存储
    Close() error
}
```

#### 2. 内存实现：复用 ttlcache

```go
// internal/store/memory_tool_cache.go

type MemoryToolCache struct {
    cache *ttlcache.Cache[string, string]
}

func NewMemoryToolCache() *MemoryToolCache {
    cache := ttlcache.New[string, string](
        ttlcache.WithTTL[string, string](30 * time.Minute),
    )
    go cache.Start()
    return &MemoryToolCache{cache: cache}
}

func (c *MemoryToolCache) Get(ctx context.Context, key string) (string, bool, error) {
    item := c.cache.Get(key)
    if item == nil {
        return "", false, nil
    }
    return item.Value(), true, nil
}

func (c *MemoryToolCache) Set(ctx context.Context, key string, value string, ttl time.Duration) error {
    c.cache.Set(key, value, ttl)
    return nil
}

func (c *MemoryToolCache) Close() error {
    c.cache.Stop()
    return nil
}
```

#### 3. Redis 实现：复用 redis.Client

```go
// internal/store/redis_tool_cache.go

type RedisToolCache struct {
    client *redis.Client
    prefix string
}

func NewRedisToolCache(client *redis.Client) *RedisToolCache {
    return &RedisToolCache{
        client: client,
        prefix: "k8s-analyzer:tool-cache:",
    }
}

func (c *RedisToolCache) Get(ctx context.Context, key string) (string, bool, error) {
    val, err := c.client.Get(ctx, c.prefix+key).Result()
    if err == redis.Nil {
        return "", false, nil
    }
    if err != nil {
        return "", false, err
    }
    return val, true, nil
}

func (c *RedisToolCache) Set(ctx context.Context, key string, value string, ttl time.Duration) error {
    return c.client.Set(ctx, c.prefix+key, value, ttl).Err()
}

func (c *RedisToolCache) Close() error {
    return nil // redis.Client 生命周期由外部管理
}
```

#### 4. 文件实现：适用于无 Redis 且需持久化的场景

基于本地文件系统的缓存实现，每个 key 对应一个文件，通过文件修改时间判断过期：

```go
// internal/store/file_tool_cache.go

type FileToolCache struct {
    dir string // 缓存目录，如 /tmp/k8s-analyzer-cache/
}

func NewFileToolCache(dir string) (*FileToolCache, error) {
    if err := os.MkdirAll(dir, 0755); err != nil {
        return nil, fmt.Errorf("failed to create cache dir: %w", err)
    }
    return &FileToolCache{dir: dir}, nil
}

func (c *FileToolCache) keyToPath(key string) string {
    // 对 key 做 SHA256 哈希，避免文件名非法字符
    h := sha256.Sum256([]byte(key))
    return filepath.Join(c.dir, hex.EncodeToString(h[:])+".cache")
}

func (c *FileToolCache) Get(ctx context.Context, key string) (string, bool, error) {
    path := c.keyToPath(key)
    info, err := os.Stat(path)
    if os.IsNotExist(err) {
        return "", false, nil
    }
    if err != nil {
        return "", false, err
    }

    // 读取文件内容：前 8 字节为 TTL（Unix 纳秒），后续为 value
    data, err := os.ReadFile(path)
    if err != nil {
        return "", false, err
    }
    if len(data) < 8 {
        return "", false, nil
    }

    expireAt := int64(binary.BigEndian.Uint64(data[:8]))
    if time.Now().UnixNano() > expireAt {
        // 已过期，删除文件
        _ = os.Remove(path)
        return "", false, nil
    }
    _ = info // suppress unused warning
    return string(data[8:]), true, nil
}

func (c *FileToolCache) Set(ctx context.Context, key string, value string, ttl time.Duration) error {
    path := c.keyToPath(key)
    expireAt := time.Now().Add(ttl).UnixNano()

    buf := make([]byte, 8+len(value))
    binary.BigEndian.PutUint64(buf[:8], uint64(expireAt))
    copy(buf[8:], value)

    return os.WriteFile(path, buf, 0644)
}

func (c *FileToolCache) Close() error {
    return nil // 文件缓存无需特殊清理
}
```

#### 5. 配置驱动选择

在 `configs/config.yaml` 中增加缓存后端配置：

```yaml
agent:
  tool_cache:
    backend: "memory"   # 可选 "memory" | "redis" | "file"
    ttl: "10m"          # 缓存过期时间
    file_dir: "/tmp/k8s-analyzer-cache"  # backend=file 时的缓存目录
```

在 `config.go` 中解析并在初始化时根据 `backend` 创建对应实现。当配置为 `redis` 时，复用已有的 Redis 连接（与 `FindingStore` 共享同一个 `redis.Client`）；当配置为 `file` 时，创建 `FileToolCache` 实例。

#### 6. ActionNode 集成

```go
type ActionNode struct {
    gateway    *gateway.GatewayClient
    safety     *safety.SafetyAgent
    reactLLM   *llm.ReActLLM
    summarizer *summarizer.OutputSummarizer
    toolCache  store.ToolCacheStore // 通过接口注入，支持内存/Redis
}

func (n *ActionNode) executeToolCall(ctx context.Context, s *state.State, tc state.ToolCall) (string, error) {
    cacheKey := buildCacheKey(tc) // tool_name + 排序后的 args JSON

    // 检查缓存
    if cached, ok, _ := n.toolCache.Get(ctx, cacheKey); ok {
        logger.Info("ActionNode: cache hit, skipping gateway call",
            logger.String("tool", tc.Name))
        return cached, nil
    }

    // ... 原有执行逻辑 ...

    // 写入缓存
    _ = n.toolCache.Set(ctx, cacheKey, summary, 10*time.Minute)

    return summary, nil
}
```

#### 缓存策略说明

- 通过 `ToolCacheStore` 接口抽象，`ActionNode` 不关心具体后端
- 默认使用内存缓存（单次诊断场景足够）；需要跨进程共享时切换 Redis；无 Redis 但需持久化时使用文件缓存
- `execute_safe_command` 工具不缓存（每次执行可能有副作用）
- `get_pod_logs` 可选缓存（日志可能随时间变化，但同一次诊断中通常一致）
- 缓存命中时仍记录 `CommandExecution`，确保报告生成不受影响

预期收益：减少 **60%+** 的 Gateway 请求。

---

## P0-2：DecisionPrompt 缺少已查询记录，LLM 无法感知历史操作

### 问题

LLM 在每轮决策时不知道之前已经执行过哪些工具、获取了什么结果，因此反复生成相同的工具调用计划。例如连续 6 轮都调用 `describe_node`，每次都认为需要"获取节点资源详情"。

### 原因

`BuildDecisionPrompt()` 构建的 Prompt 中，`已执行的步骤` section 只展示最近 3 步的 `thought + decision + observation`，而 **observation 被截断到 200 字符**（`prompts.go` 第 338-339 行）：

```go
observation := step.Observation
if len(observation) > 200 {
    observation = observation[:200] + "..."
}
```

`describe_node` 返回约 5KB，`describe_pod` 返回约 4KB，200 字符截断后 LLM 几乎看不到任何有效信息。加上更早期的步骤被 CompressNode 压缩后丢失了工具调用细节（见 P1-2），LLM 完全不知道自己已经获取过这些数据。

> **说明**：此问题仅涉及本项目 `prompts.go` 中的截断逻辑，不涉及外部 CLI 工具或 Gateway 端的修改。observation 数据已经通过 `OutputSummarizer` 做了一次摘要，瓶颈在于 Prompt 构建时的二次截断过于激进。

相关代码位置：
- `internal/llm/prompts.go` — `BuildDecisionPrompt()` 第 336-339 行
- `internal/llm/prompts.go` — `decisionPromptTemplate` 中 `{recent_steps}` 部分

### 优化方案

#### 方案 A：在 Prompt 中注入"已执行工具摘要表"

在 `decisionPromptTemplate` 中增加一个新的 section：

```
## 已查询工具记录（勿重复调用）
{executed_tools_summary}
```

在 `BuildDecisionPrompt()` 中构建这个 section：

```go
// 构建已执行工具摘要
executedToolsSummary := "无"
execs := s.GetCommandExecutions()
if len(execs) > 0 {
    seen := make(map[string]bool)
    var toolStrs []string
    for _, e := range execs {
        if seen[e.Command] {
            continue
        }
        seen[e.Command] = true
        status := "✓"
        if !e.Success {
            status = "✗"
        }
        output := e.Output
        if len(output) > 150 {
            output = output[:150] + "..."
        }
        toolStrs = append(toolStrs, fmt.Sprintf("- %s %s → %s", status, e.Command, output))
    }
    executedToolsSummary = strings.Join(toolStrs, "\n")
}
```

#### 方案 B：增大 observation 截断长度

将 `BuildDecisionPrompt()` 中的 observation 截断从 200 提升到 **800** 字符：

```go
if len(observation) > 800 {
    observation = observation[:800] + "..."
}
```

#### 建议

**A + B 同时实施**。方案 A 从宏观层面告知 LLM 已执行过的操作，方案 B 让 LLM 在微观层面看到更多具体结果，两者互补。

#### Prompt 规则强化

在 `decisionPromptTemplate` 的 `## 注意` section 追加：

```
- 上面「已查询工具记录」中列出的工具已执行过，除非有充分理由（如需要不同参数），否则不要重复调用
- 如果某工具返回空结果，不要再次调用相同参数
```

预期收益：LLM 决策质量显著提升，迭代次数从 10 轮降至 4-5 轮。

---

## P1-1：连续无新信息时未强制终止，迭代用满上限

### 问题

主诊断阶段从迭代 0 到迭代 9（达到 max_iterations=10 上限），总共 10 轮。实际上在迭代 3 左右就已经获得了所有关键信息：
- 迭代 1：获取了 Pending Pod 事件（FailedScheduling: Insufficient cpu）
- 迭代 2：获取了 CrashLoopBackOff Pod 日志、node 信息
- 迭代 3：获取了 describe_node 的资源分配

迭代 4-9 全部在重复获取相同信息，无任何增量。

### 原因

`DecisionNode` 和 `Graph` 的终止条件只有两个：
1. `s.IterationCount >= s.MaxIterations`（达上限）
2. LLM 主动返回 `decision: "report"`

没有基于"是否获取到新信息"的自动终止机制。LLM 因为看不到完整历史（P0-2），每轮都认为还需要更多信息。

相关代码位置：
- `internal/agent/diagnosis/decision_node.go` — `Execute()` 第 58-66 行
- `internal/agent/diagnosis/graph.go` — 主循环第 66-165 行

### 优化方案

#### 方案 A：结合 P0-1 缓存命中率检测"无新信息"

P0-1 引入了 `ToolCacheStore` 缓存层后，每轮工具调用都会产生"命中/未命中"记录。可以利用这个信息来判断是否还有增量数据，而不需要单独做文本相似度比较。

在 `ActionNode` 中记录每轮的缓存命中统计：

```go
// ActionNode 增加每轮缓存统计
type roundCacheStats struct {
    totalCalls int
    cacheHits  int
}
```

在 `DecisionNode.Execute()` 中，检查上一轮的缓存命中率：

```go
// 在调用 LLM 前检测：如果上一轮所有工具调用全部命中缓存，说明没有新信息
if !s.VerifyPhase && s.GetIterationCount() >= 3 {
    lastStats := s.GetLastRoundCacheStats()
    if lastStats != nil && lastStats.TotalCalls > 0 && lastStats.CacheHits == lastStats.TotalCalls {
        // 连续检查：如果最近两轮都是全缓存命中，强制终止
        prevStats := s.GetRoundCacheStats(s.GetIterationCount() - 2)
        if prevStats != nil && prevStats.TotalCalls > 0 && prevStats.CacheHits == prevStats.TotalCalls {
            logger.Info("DecisionNode: consecutive rounds all cache hits, forcing report")
            return &DecisionOutput{
                Decision:  "report",
                Thought:   "连续两轮所有工具调用均命中缓存，无增量数据，生成报告",
                ToolCalls: []state.ToolCall{},
            }, nil
        }
    }
}
```

这样 P1-1 与 P0-1 形成联动：P0-1 的缓存层既减少了 Gateway 请求，又为 P1-1 提供了"是否有新信息"的判断依据，无需额外的文本相似度算法。

#### 方案 B：在 Prompt 中加入进度提示

当迭代数超过一半时（如 iteration >= max_iterations/2），在 prompt 中加入提示：

```
⚠️ 已执行 {iteration}/{max_iterations} 轮，请尽快归纳已有证据并 decision=report。
如果关键信息已收集完毕（Pending 原因、CrashLoop 日志、节点资源），应立即生成报告。
```

#### 建议

**A + B 同时实施**。方案 A 作为硬保底（代码层面强制终止），方案 B 作为软引导（引导 LLM 更早收敛）。

预期收益：主诊断迭代从 10 轮降至 **4-5 轮**，总耗时缩短 50%+。

---

## P1-2：CompressNode 摘要丢失关键结构化信息

### 问题

CompressNode 压缩早期步骤时，`extractKeyFinding()` 只提取包含 ERROR/Failed/Pending 等关键词的行（最多 3 行），完全丢弃了：
- `describe_node` 的 Allocatable/Allocated 资源信息
- `describe_pod` 的调度条件、QoS Class、requests/limits
- 工具名和参数（LLM 不知道之前调了什么工具）

压缩后的摘要示例：
```
迭代2: execute_plan → Pending; FailedScheduling; Insufficient cpu
```

LLM 看到这个摘要后不知道 describe_node 已经调过了，于是又调一次。

### 原因

`CompressNode.ruleSummarize()` 和 `extractKeyFinding()` 的设计过于简单，只做关键词行提取，没有保留工具调用的结构化信息。

相关代码位置：
- `internal/agent/diagnosis/compress_node.go` — `ruleSummarize()` 第 77-84 行
- `internal/agent/diagnosis/compress_node.go` — `extractKeyFinding()` 第 88-140 行

### 优化方案

改进 `ruleSummarize()` 使其保留工具调用信息：

```go
func (n *CompressNode) ruleSummarize(steps []state.ReasoningStep) string {
    var summaries []string
    for _, step := range steps {
        // 保留工具调用列表
        var toolNames []string
        for _, tc := range step.ToolCalls {
            toolStr := tc.Name
            if ns, ok := tc.Args["namespace"].(string); ok && ns != "" {
                toolStr += "(" + ns + ")"
            }
            if name, ok := tc.Args["name"].(string); ok && name != "" {
                toolStr += "[" + name + "]"
            }
            toolNames = append(toolNames, toolStr)
        }
        toolsStr := strings.Join(toolNames, ", ")

        // 提取关键发现
        keyFinding := n.extractKeyFinding(step.Observation)

        summary := fmt.Sprintf("迭代%d: %s | 工具: %s | 发现: %s",
            step.Iteration, step.Decision, toolsStr, keyFinding)
        summaries = append(summaries, summary)
    }
    return strings.Join(summaries, "\n")
}
```

改进后的摘要示例：
```
迭代2: execute_plan | 工具: describe_node[<node>], get_pod_events(<ns>), describe_pod(<ns>)[<pod>] | 发现: FailedScheduling; Insufficient cpu; CPU Allocated 81%
```

同时将 `extractKeyFinding` 的最大提取行数从 3 行增加到 5 行，并增加资源相关关键词：

```go
keywords := []string{
    "ERROR", "错误", "异常", "失败",
    "CrashLoop", "OOMKilled", "ImagePullBackOff",
    "Pending", "Evicted", "Failed", "NotReady",
    // 新增资源相关关键词
    "Allocatable", "Allocated", "Insufficient",
    "requests", "limits", "cpu", "memory",
}
```

预期收益：压缩后 LLM 仍能知道之前调用过哪些工具及关键结果，减少重复调用。

---

## P2-1：Namespace 优先级策略不合理

### 问题

当集群 namespace 较多时，InfoNode 限制最多 5 个，直接截取前 5 个，可能导致有业务意义的 namespace 被跳过。而 `kube-node-lease` 和 `kube-public` 通常没有任何用户资源（pods/deployments/services 全部为空），却占用了名额。

### 原因

`InfoNode.Execute()` 中的 namespace 限制策略是简单的 `targetNamespaces[:5]`，没有优先级排序。

相关代码位置：
- `internal/agent/diagnosis/info_node.go` — 第 80-86 行

### 优化方案

#### 1. 动态调整 namespace 扫描数量

取消硬编码的 `maxNamespaces=5` 限制，改为根据集群实际 namespace 数量动态计算扫描上限：

```go
// dynamicMaxNamespaces 根据集群 namespace 总数动态计算扫描上限
func dynamicMaxNamespaces(total int) int {
    switch {
    case total <= 8:
        return total // 小集群：全部扫描
    case total <= 20:
        return 10    // 中等集群：扫描前 10 个（过滤低优先级后）
    default:
        return 15    // 大集群：扫描前 15 个（过滤低优先级后）
    }
}
```

#### 2. 过滤低优先级 namespace

在动态上限之上，增加低优先级 namespace 后置排序，确保空 namespace 不会挤掉有业务意义的 namespace：

```go
func prioritizeNamespaces(namespaces []string) []string {
    // 低优先级 namespace（通常为空或无业务意义）
    lowPriority := map[string]bool{
        "kube-node-lease": true,
        "kube-public":     true,
    }

    var high, low []string
    for _, ns := range namespaces {
        if lowPriority[ns] {
            low = append(low, ns)
        } else {
            high = append(high, ns)
        }
    }
    return append(high, low...)
}
```

#### 3. InfoNode 集成

在 `InfoNode.Execute()` 中替换原有逻辑：

```go
// 动态计算扫描上限
maxNs := dynamicMaxNamespaces(len(namespaces))

// 按优先级排序
targetNamespaces := prioritizeNamespaces(namespaces)

// 截取
if len(targetNamespaces) > maxNs {
    logger.Info("InfoNode: dynamic namespace limit applied",
        logger.Int("total", len(namespaces)),
        logger.Int("maxNs", maxNs),
        logger.Int("selected", maxNs))
    targetNamespaces = targetNamespaces[:maxNs]
}
```

#### 4. 配置可覆盖

在 `configs/config.yaml` 中支持手动覆盖（0 表示使用动态策略）：

```yaml
agent:
  max_namespaces: 0  # 0=动态计算，>0=固定上限
```

预期收益：确保有业务意义的 namespace 不会被低优先级系统 namespace 挤掉，避免漏诊。

---

## P2-2：Verify Phase 对纯建议型 Recommendations 无意义

### 问题

诊断报告的 3 条建议全部是"建议优化"类型（无具体可执行命令），验证阶段仍然进入，做了 3 轮无意义的验证迭代（重复获取 describe_pod、describe_node），浪费约 30 秒。

### 原因

`Graph.Run()` 中进入验证阶段的条件是 `len(state.AnalysisResult.Recommendations) > 0`（第 177 行），没有检查 Recommendations 是否包含可验证的内容。

相关代码位置：
- `internal/agent/diagnosis/graph.go` — 第 177 行

### 优化方案

增加验证阶段的进入条件检查：

```go
// 判断是否有可验证的建议（至少有一条带 Command 的建议）
hasVerifiableRec := false
for _, rec := range state.AnalysisResult.Recommendations {
    if rec.Command != "" {
        hasVerifiableRec = true
        break
    }
}

if g.verifyEnabled && state.AnalysisResult != nil && hasVerifiableRec && !state.VerifyPhase {
    // 进入验证阶段
    ...
}
```

同时在 `BuildVerifyDecisionPrompt` 中过滤掉没有 Command 的建议，避免 LLM 对纯文字建议做"验证"：

```go
// 只展示有 Command 的建议作为待验证清单
for i, rec := range s.AnalysisResult.Recommendations {
    if rec.Command == "" {
        continue // 跳过纯建议
    }
    // ... 构建 checklist
}
```

预期收益：纯建议场景跳过验证阶段，节省 20-30 秒。

---

## 实施顺序建议

1. **P0-1 + P0-2**：工具缓存 + Prompt已查询记录（同时实施，效果叠加最大）
2. **P1-1**：无新信息强制终止（作为缓存之上的二级保底）
3. **P1-2**：CompressNode 改进（与 P0-2 配合，减少信息丢失）
4. **P2-1 + P2-2**：Namespace 优先级 + Verify 跳过（独立优化，互不影响）

全部实施后预期：
- 主诊断迭代：10 轮 → 4-5 轮
- Gateway 请求数：~50 次 → ~20 次
- 总耗时：~3 分钟 → ~1-1.5 分钟
