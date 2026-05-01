# 任务详情页执行链、展开收起与缓存显示修复方案

## 1. 背景

基于样例 Trace `data/traces/f5fce6e1-8670-402b-bd9c-aaa976c4cc5b.json`，当前监控面板任务详情页存在以下三个问题：

1. **执行轮次显示不一致**：顶部摘要显示“执行轮次 5 轮”，但执行链时间线展示为“第 3 轮 / 第 5 轮 / 第 8 轮 / 第 9 轮 / 第 10 轮”，用户感知上与“总共执行了 5 轮”不一致。
2. **文本块只能展开不能收起**：执行链中的“输出摘要”和“观察”区域展开后无法可靠收起。
3. **LLM 调用中的缓存命中看起来为空**：需要确认是后端未返回缓存字段，还是前端展示方式让“未命中”看起来像“没有数据”。

本方案目标是先给出**最小可落地修复路径**，优先解决用户感知错误，再决定是否追加后端语义统一。

---

## 2. 现状与证据

### 2.1 样例 Trace 证据

样例文件：`data/traces/f5fce6e1-8670-402b-bd9c-aaa976c4cc5b.json`

- `reasoning_history` 中实际记录了 **5 个推理步骤**。
- 这 5 个步骤的 `iteration` 不是连续值，而是：**3、5、8、9、10**。
- `llm_calls[*].cache_hit` 字段存在，样例值均为 `false`。
- `reasoning_history[*].tool_calls[*].cached` 字段也存在，样例值均为 `false`。

结论：

- “执行轮次数量”与“原始 iteration 编号”是两套不同语义。
- 缓存字段**不是没返回**，而是**返回了 false**。

### 2.2 前端现状证据

文件：`web/src/pages/TaskDetail/index.tsx`

#### 问题 1：执行轮次显示不一致

- 顶部摘要卡片中，“执行轮次”直接取 `(trace.reasoning_history ?? []).length` 显示（约第 617-619 行）。
- 时间线头部中，每个步骤标题直接显示 `第 {step.iteration} 轮`（约第 92-94 行）。

这意味着：

- 顶部展示的是**当前可见步骤数**。
- 执行链展示的是**后端写入的原始 iteration 值**。

两者语义不一致，因此会出现“顶部是 5 轮，但链路里是第 3/5/8/9/10 轮”的冲突感。

#### 问题 2：展开后无法收起

- 工具调用“输出摘要”使用：`Paragraph copyable ellipsis={{ rows: 5, expandable: 'collapsible' }}`（约第 176-179 行）。
- “观察”区域使用：`Paragraph copyable ellipsis={{ rows: 8, expandable: 'collapsible' }}`（约第 209-212 行）。

当前实现完全依赖 Ant Design `Typography.Paragraph` 的内置折叠状态，没有显式受控状态，也没有统一的自定义切换器。同时，这两个文本块都把 `copyable` 与 `ellipsis.expandable='collapsible'` 叠加在同一个组件上，而项目当前使用的 Ant Design 版本为 `^5.29.3`，该组合本身存在已知交互冲突与行为不稳定的潜在风险。

#### 问题 3：缓存命中显示为空

- LLM 调用表格的“缓存”列直接读取 `cache_hit`（约第 323-328 行）。
- 当前渲染逻辑是：
  - `true` → 显示 `命中`
  - `false` → 显示 `—`
- 统计摘要中，`cacheHitCount > 0` 才显示“缓存命中”卡片（约第 366、384 行）。

因此当所有值都是 `false` 时：

- 表格中看起来像“空白占位符”；
- 统计区完全不显示缓存命中卡片；
- 用户很容易误判为“没有返回这个字段”。

### 2.3 后端现状证据

文件：`internal/trace/types.go`

- `LLMCallRecord.CacheHit bool  \`json:"cache_hit"\``：后端 Trace 结构已定义 `cache_hit`，类型为 `bool`，且**没有** `omitempty`。
- `TraceToolCallDetail.Cached bool  \`json:"cached"\``：推理步骤内工具调用也已定义 `cached`。
- `TraceReasoningStep.Iteration int  \`json:"iteration"\``：后端明确保存原始迭代编号。
- `decision_node.go`、`report_node.go`、`react_llm.go` 在创建 `LLMCallRecord` 时当前均**未主动设置** `CacheHit`，因此在现阶段后端约束下该字段会稳定输出为 `false`。

#### OpenRouter Prompt Caching 证据

经查阅 OpenRouter 官方文档，OpenRouter 提供两种缓存机制：

1. **Response Caching**（响应级缓存）：通过 HTTP 响应头 `X-OpenRouter-Cache-Status: HIT/MISS` 标识，命中时所有 token 计数归零。需要客户端发送 `X-OpenRouter-Cache: true` 头部显式启用。
2. **Prompt Caching**（提示词级缓存）：返回在 `usage.prompt_tokens_details` 嵌套字段中，包含 `cached_tokens`（缓存命中 token 数）和 `cache_write_tokens`（写入缓存 token 数）。**DeepSeek 模型自动启用，无需额外配置。**

当前项目使用的 `deepseek/deepseek-v4-flash` **天然支持 Prompt Caching**，OpenRouter 会在每次响应的 `usage` 中返回：

```json
{
  "usage": {
    "prompt_tokens": 10339,
    "completion_tokens": 60,
    "total_tokens": 10399,
    "prompt_tokens_details": {
      "cached_tokens": 10318,
      "cache_write_tokens": 0
    }
  }
}
```

#### Eino SDK 已支持解析缓存字段

项目使用的 CloudWego Eino SDK（`v0.8.0`）的 `schema.TokenUsage` 已包含缓存相关字段：

```go
type TokenUsage struct {
    PromptTokens            int                    `json:"prompt_tokens"`
    PromptTokenDetails      PromptTokenDetails     `json:"prompt_token_details"`
    CompletionTokens        int                    `json:"completion_tokens"`
    TotalTokens             int                    `json:"total_tokens"`
    CompletionTokensDetails CompletionTokensDetails `json:"completion_token_details"`
}

type PromptTokenDetails struct {
    CachedTokens int `json:"cached_tokens"`
}
```

`router.go` 中的 `extractTokenUsage()` 已完整复制 `schema.TokenUsage`（包括 `PromptTokenDetails.CachedTokens`），因此**从 OpenRouter API 到 Eino SDK 这一层数据没有丢失**。

#### 数据断链的真正位置

数据断链发生在 3 个 `LLMCallEvent` 的 Emit 点——它们构造 `LLMCallRecord` 时只复制了 `PromptTokens`、`CompletionTokens`、`TotalTokens`，**没有读取 `usage.PromptTokenDetails.CachedTokens`**：

| Emit 点 | 文件 | 行号 | `CacheHit` / `CachedTokens` 赋值 |
|---------|------|------|----------------------------------|
| 决策调用 | `decision_node.go` | 178-189 | **未设置** |
| 报告生成 | `report_node.go` | 67-78 | **未设置** |
| 深度调查 | `react_llm.go` | 304-314 | **未设置** |

#### 多 LLM 提供商的缓存字段兼容性分析

项目即将从 OpenRouter 切换到 DeepSeek 和 Claude 官方 API，不同提供商的缓存字段格式**完全不同**：

| 提供商 | API 兼容格式 | 缓存字段 JSON 路径 | Eino SDK 映射情况 |
|--------|------------|-------------------|------------------|
| **OpenRouter / OpenAI** | OpenAI 兼容 | `usage.prompt_tokens_details.cached_tokens` | Eino `toEinoTokenUsage()` 已正确映射到 `PromptTokenDetails.CachedTokens` |
| **DeepSeek 官方** | OpenAI 兼容（扩展字段） | `usage.prompt_cache_hit_tokens` (顶层) | **不兼容**：底层 `go-openai` 的 `Usage` 结构无此字段定义，落入 `ExtraFields`；Eino `toEinoTokenUsage()` 不读取 `ExtraFields`，`CachedTokens` 恒为 0 |
| **Claude / Anthropic** | Anthropic 原生 | `usage.cache_read_input_tokens` | 需引入 `eino-ext/components/model/anthropic` adapter（当前未引入），映射方式待确认 |

**底层 SDK 依赖链详细分析**（以 DeepSeek 官方 API 为例）：

1. DeepSeek 官方 API 返回 JSON：
   ```json
   {
     "usage": {
       "prompt_tokens": 1000,
       "completion_tokens": 50,
       "total_tokens": 1050,
       "prompt_cache_hit_tokens": 800,
       "prompt_cache_miss_tokens": 200
     }
   }
   ```
2. `meguminnnnnnnnn/go-openai@v0.1.1` 的 `Usage` 结构定义：
   ```go
   type Usage struct {
       PromptTokens            int                        `json:"prompt_tokens"`
       CompletionTokens        int                        `json:"completion_tokens"`
       TotalTokens             int                        `json:"total_tokens"`
       PromptTokensDetails     *PromptTokensDetails       `json:"prompt_tokens_details"`
       CompletionTokensDetails *CompletionTokensDetails   `json:"completion_tokens_details"`
       ExtraFields             map[string]json.RawMessage `json:"-"`
   }
   ```
   - `prompt_cache_hit_tokens` 没有对应的 Go 字段，被自定义 `UnmarshalJSON` 捕获到 `ExtraFields["prompt_cache_hit_tokens"]` 中。
   - `PromptTokensDetails` 为 `nil`（DeepSeek 不返回 `prompt_tokens_details`）。

3. `eino-ext/libs/acl/openai@v0.1.13` 的 `toEinoTokenUsage()`：
   ```go
   if usage.PromptTokensDetails != nil {
       promptTokenDetails.CachedTokens = usage.PromptTokensDetails.CachedTokens
   }
   ```
   - 由于 `PromptTokensDetails == nil`，`CachedTokens` 保持为 0。
   - **`ExtraFields` 完全被忽略**。

4. **结论**：切换到 DeepSeek 官方 API 后，即使后端 Emit 点正确读取 `usage.PromptTokenDetails.CachedTokens`，值也始终为 0。

结论：

- **问题 3 的根因是后端 Emit 点未从 SDK 已解析到的 `usage.PromptTokenDetails.CachedTokens` 中提取缓存信息，而非前端展示错误。**
- 前端把 `false` 渲染成 `—` 的问题**仍然存在**，但只是表象；即使修正前端展示，在后端未赋值的情况下也只会显示"未命中"。
- **问题 1 的直接冲突发生在前端展示层，但根本上也涉及后端字段语义没有在 UI 中被区分。**
- **切换 API 提供商后，缓存字段的兼容性是一个独立风险点，需要额外的适配层处理。**

### 2.4 `stepModelMap` 关联风险证据

文件：`web/src/pages/TaskDetail/index.tsx`

- `decisionCalls` 通过 `llmCalls.filter(c => c.source === 'decision')` 得到（约第 59 行）。
- `steps.forEach((step, idx) => { stepModelMap[step.iteration] = decisionCalls[idx] })`（约第 61-64 行）。
- 渲染时通过 `const matchedCall = stepModelMap[step.iteration]` 取值（约第 75 行）。

这说明当前“步骤 ↔ 决策调用”的真实对应关系是**按数组索引 `idx` 建立**，只是最终临时借用了 `step.iteration` 作为 map key。

结论：

- 一旦后续为了展示而把 `step.iteration` 直接重写为连续编号，`stepModelMap` 的查找语义就可能被一起污染。
- 因此本轮实现应**直接取消对 `stepModelMap` 的依赖**，回到代码真实语义：**按 `idx` 关联步骤与 decision 调用，展示轮次独立计算**。

---

## 3. 根因分析

### 3.1 执行轮次问题的根因

根因不是单一“数据错了”，而是**两个不同概念被同时命名为“轮次”**：

1. `reasoning_history.length` 表示“当前记录到的推理步骤数”；
2. `step.iteration` 表示“后端状态机运行时的原始迭代编号”。

当后端存在跳步、过滤、压缩、只保留部分步骤或从某一轮开始写入 Trace 时，`iteration` 就可能天然不连续。

因此前端当前的命名与展示方式会让用户误以为这两者应该严格相等。

### 3.2 展开/收起问题的根因

当前实现把“是否展开”的交互完全交给 `Paragraph ellipsis` 内部状态处理，缺少以下能力：

- 没有显式 `expanded` 状态；
- 没有统一的 `onToggle` 控制；
- 没有为每个文本块建立稳定的唯一 key；
- 在 `Collapse`、`Timeline`、表格展开行等嵌套场景下，组件内置折叠行为不够可控。

除此之外，当前实现还把 `copyable` 与 `ellipsis.expandable='collapsible'` 同时绑定在同一个 `Paragraph` 上。结合 Ant Design 社区已知 issue，这个组合存在 tooltip/hover/展开交互互相干扰的风险。在本项目当前使用的 `antd ^5.29.3` 下，不应继续把“展开/收起是否可靠”建立在这组默认组合行为上。

因此即使库组件理论上支持 `collapsible`，在当前页面场景中仍可能表现为“只能展开，无法可靠收起”。

### 3.3 缓存命中问题的根因

问题的根因分**两层**：

**第一层（后端数据断链）**：OpenRouter 通过 `usage.prompt_tokens_details.cached_tokens` 返回了 Prompt Caching 命中数据，Eino SDK 已正确解析到 `schema.TokenUsage.PromptTokenDetails.CachedTokens`，但后端 3 个 Emit 点在构造 `LLMCallRecord` 时没有读取该字段，导致 `CacheHit` 始终为 Go 零值 `false`。

**第二层（前端展示语义）**：前端把 `false` 渲染成 `—`（空占位符），`0 次命中` 被直接隐藏，用户自然理解为"数据没返回"。

因此这**不仅仅是展示问题，也是后端数据传递断链问题**。两层都需要修复。

---

## 4. 修复原则

1. **优先前端最小改动**：先修复用户可见问题，不先动 Trace 存储结构。
2. **保留后端原始语义**：原始 `iteration` 不删除、不篡改，避免影响追踪与排障。
3. **后端补齐缓存数据传递**：从 SDK 已解析的 `usage.PromptTokenDetails.CachedTokens` 提取数据，传递到 `LLMCallRecord`，实现真实缓存命中记录。
4. **前端同步修正缓存展示**：把"未命中"从"空占位"中解耦，支持展示 `cached_tokens` 数量。
5. **交互受控化**：展开/收起不能继续依赖不可控的组件内部状态。

---

## 5. 详细修复方案

### 5.1 问题一：执行轮次与执行链显示不一致

#### 方案目标

让用户在页面上看到的“轮次”语义保持一致，同时保留后端原始迭代信息用于调试。

#### 推荐方案（本次优先采用）

前端将“展示轮次”和“原始迭代编号”拆开：

1. 顶部摘要区不再把 `(reasoning_history.length)` 与原始 `iteration` 混为同一概念。
2. 执行链标题使用**连续展示序号**（按时间线顺序重新编号为第 1、2、3、4、5 轮）。
3. 原始 `step.iteration` 作为辅助信息保留，例如：
   - 副标签：`原始迭代 3`
   - Tooltip：hover 查看原始编号
   - 次级灰字：`原始 iteration=3`

#### 实现保护要求

本轮实现方式直接定为：

1. **删除对 `stepModelMap` 的依赖**。
2. `matchedCall` 直接按 `decisionCalls[idx]` 获取。
3. **不要直接改写 `step.iteration` 的原始值**。
4. 展示用连续轮次使用 `displayRound = idx + 1` 之类的独立变量。
5. 原始 `step.iteration` 只作为辅助信息展示，不再承担前端关联 key 的职责。

#### 页面语义定稿

本轮直接定为：

- 顶部字段名保留为：`执行轮次`
- 顶部值显示为：`5 轮`
- 时间线主标题显示为连续轮次：`第 1 轮`、`第 2 轮`、`第 3 轮`...
- 原始 `step.iteration` 作为辅助信息展示，例如：`原始迭代 3 / 5 / 8 / 9 / 10`

这样可以保证用户看到的“执行轮次”始终是连续、可理解的业务展示语义，而原始 iteration 仍保留给研发排障使用。

#### 涉及文件

- `web/src/pages/TaskDetail/index.tsx`

#### 是否需要后端改动

本轮**不必强制后端改动**。

但建议作为后续优化候选项，在 Trace 中显式补充：

- `display_iteration` / `sequence_no` / `step_index`

这样前端无需自行推导展示序号，语义会更稳定。

---

### 5.2 问题二：输出摘要、观察区域只能展开不能收起

#### 方案目标

让每个文本块具备稳定、可重复、可验证的展开/收起行为。

#### 推荐方案

将当前依赖 `Paragraph ellipsis` 的隐式交互，改为**显式受控展开状态**。

同时，本次应明确把 `copyable + ellipsis.expandable` 视为潜在干扰因素，而不是单纯假设是“组件库自身的 collapsible 不稳定”。

核心思路：

1. 为每一个可折叠文本块建立唯一标识，例如：
   - `tool-output-${step.iteration}-${idx}`
   - `observation-${step.iteration}`
2. 在组件中维护一个 `expandedMap`（或拆成多个 map）。
3. 通过显式点击按钮控制：
   - 默认折叠显示固定行数/固定高度
   - 点击“展开”后显示全文
   - 点击“收起”后恢复折叠态
4. 文案不再依赖组件库默认生成，而是统一使用自定义按钮：`展开` / `收起`。
5. `copyable` 不再与 `ellipsis.expandable` 绑定在同一个 `Paragraph` 行为上，可改为外置复制按钮或在自定义组件中分离交互职责。

#### 实现方案（本轮唯一方案）

本轮直接抽一个**仅供 TaskDetail 页面使用**的轻量 `ExpandableText` 组件，统一处理：

- 文本截断
- 复制按钮
- 展开/收起
- 多行预览
- `pre-wrap`/长文本换行

这样做的原因：

- 逻辑集中；
- 后续可复用于工具输出、观察、LLM 输入/输出、错误信息等区域；
- 页面代码更清晰；
- 可以从根上拆开 `copyable` 与 `ellipsis.expandable` 的交互职责，避免继续依赖它们叠加后的默认行为。

组件范围约束：

- 只服务于 `TaskDetail` 页面；
- 不抽成全局公共组件；
- 复制按钮与“展开 / 收起”按钮必须分离。

#### 涉及文件

- `web/src/pages/TaskDetail/index.tsx`
- `web/src/pages/TaskDetail/components/ExpandableText.tsx`（新建，必做）

#### 注意点

- 每个文本块的展开状态必须互不影响。
- 切换 Tab、展开/收起外层 `Collapse` 后，内部状态不能异常串联。
- 长文本、空文本、仅一两行文本都要覆盖。
- 需要显式验证 `copyable` 与“展开/收起”按钮互不抢占 hover/click 行为。

---

### 5.3 问题三：LLM 调用缓存命中显示为空

#### 方案目标

实现真实的 LLM Prompt Caching 命中记录和展示，让用户能看到每次 LLM 调用的缓存命中 token 数。

#### 后端修复（本轮必做）

##### 5.3.1 扩展 `LLMCallRecord` 结构

在 `internal/trace/types.go` 中为 `LLMCallRecord` 增加 `CachedTokens` 字段：

```go
type LLMCallRecord struct {
    // ... 现有字段 ...
    CacheHit     bool `json:"cache_hit"`      // 是否命中缓存（从 CachedTokens > 0 推导）
    CachedTokens int  `json:"cached_tokens"`  // Prompt Caching 命中的 token 数
}
```

`CacheHit` 保留以兼容现有前端逻辑，但改为从 `CachedTokens > 0` 推导赋值。

##### 5.3.2 基于 Eino Callback 机制的多提供商缓存适配方案

不同 LLM 提供商的缓存字段格式完全不同（参见 2.3 节兼容性分析），需要一个**提供商无关的统一适配层**。经过对 Eino SDK 源码的深入分析，发现 Eino OpenAI adapter 提供了 `WithResponseMessageModifier` 机制，可以在 SDK 层面拦截原始 HTTP 响应，将各提供商的非标准缓存字段统一回填到 `schema.TokenUsage.PromptTokenDetails.CachedTokens`。

###### 核心思路

Eino 的 `eino-ext/libs/acl/openai` adapter 在 Generate 完成时的执行链为：

```
HTTP 响应 → openai.Usage 解析 → toEinoTokenUsage() → ResponseMeta.Usage
                                                          ↓
                                          ResponseMessageModifier(ctx, msg, rawBody)
                                                          ↓
                                          callbacks.OnEnd(CallbackOutput)
                                                          ↓
                                          返回 *schema.Message 给调用方
```

关键点：

- `WithResponseMessageModifier(fn)` 注册的函数会在 `toEinoTokenUsage()` 之后、`callbacks.OnEnd()` 之前被调用。
- 函数签名：`func(ctx context.Context, msg *schema.Message, rawBody []byte) (*schema.Message, error)`
- **`rawBody` 就是原始 HTTP JSON 响应体**，包含各提供商返回的所有字段（包括 DeepSeek 的 `prompt_cache_hit_tokens`）。
- `msg.ResponseMeta.Usage` 此时已被 Eino 填充（但 DeepSeek 的非标准字段丢失了）。
- 我们可以在 modifier 中解析 `rawBody`，将缓存信息**回填到已有的 `msg.ResponseMeta.Usage.PromptTokenDetails.CachedTokens`**。

这样，**下游所有消费 `schema.TokenUsage` 的代码（包括 3 个 Emit 点）都无需关心提供商差异**。

###### 实现方案

**步骤 1：在 `internal/llm/router.go` 中定义缓存适配器**

```go
import (
    "context"
    "encoding/json"

    openaimodel "github.com/cloudwego/eino-ext/components/model/openai"
    "github.com/cloudwego/eino/schema"
)

// cacheTokensModifier 是一个 ResponseMessageModifier，
// 从原始 HTTP 响应体中提取各提供商的缓存 token 字段，
// 统一回填到 msg.ResponseMeta.Usage.PromptTokenDetails.CachedTokens。
func cacheTokensModifier(ctx context.Context, msg *schema.Message, rawBody []byte) (*schema.Message, error) {
    if msg == nil || msg.ResponseMeta == nil || msg.ResponseMeta.Usage == nil {
        return msg, nil
    }

    // 如果 Eino 标准路径已经解析到缓存数据（OpenAI/OpenRouter），直接返回
    if msg.ResponseMeta.Usage.PromptTokenDetails.CachedTokens > 0 {
        return msg, nil
    }

    // 尝试从原始响应体解析 DeepSeek 的非标准字段
    var raw struct {
        Usage struct {
            PromptCacheHitTokens  int `json:"prompt_cache_hit_tokens"`
            PromptCacheMissTokens int `json:"prompt_cache_miss_tokens"`
        } `json:"usage"`
    }
    if err := json.Unmarshal(rawBody, &raw); err != nil {
        // 解析失败不影响正常流程，安全降级
        return msg, nil
    }

    if raw.Usage.PromptCacheHitTokens > 0 {
        msg.ResponseMeta.Usage.PromptTokenDetails.CachedTokens = raw.Usage.PromptCacheHitTokens
    }

    return msg, nil
}
```

**步骤 2：在 `createChatModel()` 中注入 modifier**

```go
func createChatModel(ctx context.Context, cfg *config.LLMConfig) (model.ChatModel, error) {
    // ... 现有参数转换逻辑 ...

    chatModel, err := openaimodel.NewChatModel(ctx, &openaimodel.ChatModelConfig{
        BaseURL:     cfg.BaseURL,
        APIKey:      cfg.APIKey,
        Model:       cfg.Model,
        Temperature: tempPtr,
        MaxTokens:   maxTokensPtr,
    })
    if err != nil {
        return nil, err
    }

    // 注入缓存适配 modifier：从原始 HTTP 响应中提取各提供商的缓存字段
    // 对于 OpenAI/OpenRouter：Eino 已标准解析，modifier 直接透传
    // 对于 DeepSeek 官方：从 rawBody 解析 prompt_cache_hit_tokens 回填到 CachedTokens
    wrappedModel := wrapWithCacheModifier(chatModel)

    return wrappedModel, nil
}
```

**步骤 3：模型包装器实现**

由于 `WithResponseMessageModifier` 是 per-call 的 `model.Option`，需要在每次 Generate 调用时注入。有两种注入方式：

**方式 A（推荐）：包装 ChatModel 接口**

```go
// cachedChatModel 包装 ChatModel，自动注入缓存适配 modifier
type cachedChatModel struct {
    inner model.ChatModel
}

func wrapWithCacheModifier(m model.ChatModel) model.ChatModel {
    return &cachedChatModel{inner: m}
}

func (c *cachedChatModel) Generate(ctx context.Context, in []*schema.Message, opts ...model.Option) (*schema.Message, error) {
    // 在每次调用时注入 cacheTokensModifier
    opts = append(opts, openaimodel.WithResponseMessageModifier(cacheTokensModifier))
    return c.inner.Generate(ctx, in, opts...)
}

// 如果 inner 实现了 ToolCallingChatModel，需要同时转发
func (c *cachedChatModel) BindTools(tools []*schema.ToolInfo) error {
    if tc, ok := c.inner.(model.ToolCallingChatModel); ok {
        return tc.BindTools(tools)
    }
    return nil
}
```

> **注意**：包装器需要确保 `model.ToolCallingChatModel` 接口的完整转发，因为 ReAct Agent 使用的是 `ToolCallingChatModel`。

**方式 B（更简洁）：在 LLMRouter 的 Generate 方法中注入**

```go
func (r *LLMRouter) GenerateWithLight(ctx context.Context, messages []*schema.Message) (*schema.Message, *schema.TokenUsage, error) {
    if r.light == nil {
        return nil, nil, fmt.Errorf("light model not initialized")
    }
    // 注入缓存适配 modifier
    msg, err := r.light.Generate(ctx, messages, openaimodel.WithResponseMessageModifier(cacheTokensModifier))
    if err != nil {
        return nil, nil, err
    }
    return msg, extractTokenUsage(msg), nil
}
```

方式 B 的局限是**无法覆盖 ReAct Agent 内部的 LLM 调用**（react.Agent 直接持有 `ToolCallingChatModel` 引用，调用时不经过 LLMRouter）。因此**推荐方式 A**。

**步骤 4：统一提取函数（保持简单）**

由于 `cacheTokensModifier` 已经在 SDK 层面统一了数据，`ExtractCachedTokens` 只需读标准路径：

```go
// ExtractCachedTokens 从 TokenUsage 中提取缓存命中 token 数。
// 由于 cacheTokensModifier 已在 SDK 层面将各提供商的缓存字段
// 统一回填到 PromptTokenDetails.CachedTokens，此函数只需读标准路径。
func ExtractCachedTokens(usage *schema.TokenUsage) int {
    if usage == nil {
        return 0
    }
    return usage.PromptTokenDetails.CachedTokens
}
```

###### Claude/Anthropic 适配扩展

当项目引入 Eino 的 Anthropic adapter（`eino-ext/components/model/anthropic`）时：

1. 如果 Anthropic adapter 已将 `cache_read_input_tokens` 映射到 `PromptTokenDetails.CachedTokens` → 无需额外处理。
2. 如果未映射 → 可在 Anthropic adapter 中注册类似的 Callback OnEnd handler，或在 `cachedChatModel` 中增加 Anthropic 原始响应的解析逻辑。
3. `cacheTokensModifier` 已设计为安全降级（解析失败返回原消息），不会影响其他提供商。

###### 提供商适配矩阵

| 提供商 | 适配路径 | modifier 行为 |
|--------|---------|--------------|
| OpenAI/OpenRouter | Eino 标准路径 | 检测到 `CachedTokens > 0`，直接透传 |
| DeepSeek 官方 | `rawBody` 解析 | 从 JSON 提取 `prompt_cache_hit_tokens`，回填到 `CachedTokens` |
| Claude/Anthropic | 待接入后确认 | 可扩展 modifier 或由 adapter 自行映射 |

###### 关键源码依据

- `eino-ext/libs/acl/openai@v0.1.13/option.go` L42：`ResponseMessageModifier func(ctx, msg, rawBody) (*Message, error)`
- `eino-ext/libs/acl/openai@v0.1.13/chat_model.go` L817-822：Generate 中 modifier 的调用时机（在 `toEinoTokenUsage()` 之后、`callbacks.OnEnd()` 之前）
- `eino-ext/libs/acl/openai@v0.1.13/option.go` L81-85：`WithResponseMessageModifier()` option 注册函数
- `meguminnnnnnnnn/go-openai@v0.1.1/common.go` L14-21：Usage 的 `ExtraFields` 捕获了 DeepSeek 的非标准字段


##### 5.3.3 在 3 个 Emit 点提取缓存数据

由于 `cacheTokensModifier` 已在 SDK 层面将各提供商的缓存字段统一回填到 `PromptTokenDetails.CachedTokens`，Emit 点只需简单读取标准路径：

```go
cachedTokens := llm.ExtractCachedTokens(usage)
// ...
CachedTokens: cachedTokens,
CacheHit:     cachedTokens > 0,
```

涉及文件：

| 文件 | 修改位置 |
|------|---------|
| `internal/trace/types.go` | `LLMCallRecord` 增加 `CachedTokens` 字段 |
| `internal/llm/router.go` | 新增 `cacheTokensModifier`、`cachedChatModel` 包装器、`ExtractCachedTokens()` |
| `internal/agent/diagnosis/decision_node.go` | L178-189 Emit 点 |
| `internal/agent/diagnosis/report_node.go` | L67-78 Emit 点 |
| `internal/llm/react_llm.go` | L304-314 Emit 点 |

#### 前端修复（本轮必做）

##### 5.3.4 更新 TypeScript 类型定义

在 `web/src/api/types.ts` 的 `LLMCallRecord` 中增加：

```typescript
export interface LLMCallRecord {
  // ... 现有字段 ...
  cache_hit: boolean;        // 是否命中缓存（从 cached_tokens > 0 推导）
  cached_tokens: number;     // Prompt Caching 命中的 token 数
}
```

注意：`cache_hit` 从可选改为必需（`boolean` 而非 `boolean?`），因为后端无 `omitempty`，稳定输出。

##### 5.3.5 表格列展示优化

将当前的二态占位改为**直接展示命中 token 数量**：

- LLM 调用表格中的缓存列**固定命名为**：`缓存命中 Tokens`
- `cached_tokens > 0` → 显示格式化后的 token 数，例如 `10,318`
- `cached_tokens === 0` → 明确显示 `0`

`cache_hit` 保留用于内部语义与兼容逻辑，不再作为该列的主展示内容。

##### 5.3.6 统计摘要始终展示缓存卡片

当前逻辑只有 `cacheHitCount > 0` 才显示缓存摘要卡片。改为固定展示：

- 有命中时：`缓存命中 Tokens：30,954`
- 全部未命中时：`缓存命中 Tokens：0`

统计口径固定为：`sum(llm_calls[].cached_tokens)`。

本轮不再把“命中次数”作为缓存主指标，也不再展示“X 次 / Y tokens”这种混合口径。

##### 工具调用缓存标签保持现状

执行链中工具调用已用 `tc.cached && <Tag>缓存</Tag>` 展示，该逻辑本身没有取值错误，本轮不改。

##### 5.3.7 前端展示约束定稿

为避免实现阶段再次出现歧义，本轮前端缓存展示统一遵循以下约束：

1. 用户可见的 LLM 缓存信息**统一以 `cached_tokens` 为准**。
2. 前端 UI **不再单独展示** `cache_hit` 的“命中 / 未命中”文案或 Tag。
3. `cache_hit` 仅用于内部语义、兼容逻辑或衍生判断，不再作为主展示字段。

#### 涉及文件

##### 后端

- `internal/trace/types.go`（`LLMCallRecord` 增字段）
- `internal/agent/diagnosis/decision_node.go`（Emit 点补赋值）
- `internal/agent/diagnosis/report_node.go`（Emit 点补赋值）
- `internal/llm/react_llm.go`（Emit 点补赋值）

##### 前端

- `web/src/api/types.ts`（类型定义更新）
- `web/src/pages/TaskDetail/index.tsx`（展示逻辑更新）

---

## 6. 变更文件清单

### 本轮必改

#### 前端

- `web/src/pages/TaskDetail/index.tsx`
- `web/src/api/types.ts`

#### 后端

- `internal/trace/types.go`（`LLMCallRecord` 增加 `CachedTokens` 字段）
- `internal/llm/router.go`（新增 `cacheTokensModifier`、`cachedChatModel` 包装器、`ExtractCachedTokens()`）
- `internal/agent/diagnosis/decision_node.go`（Emit 点补缓存赋值）
- `internal/agent/diagnosis/report_node.go`（Emit 点补缓存赋值）
- `internal/llm/react_llm.go`（Emit 点补缓存赋值）

### 本轮可选

- 无

### 暂不改动

- `internal/trace/recorder.go`、`internal/trace/events.go`（事件结构无需变更，`LLMCallRecord` 已通过 `LLMCallEvent.Call` 透传）

---

## 7. 实施顺序

### Phase 1：统一执行轮次展示语义

目标：先修复最显眼、最容易误导用户的问题。

输出结果：

- 顶部摘要与时间线主标题语义一致；
- 原始 iteration 仍可查看。

### Phase 2：替换受控展开/收起交互

目标：让“输出摘要”和“观察”区域具备稳定的展开/收起行为。

输出结果：

- 每个文本块都可展开；
- 展开后都可收起；
- 多个文本块互不串状态。

### Phase 3：补齐后端缓存数据 + 修正前端缓存展示

目标：让 `cache_hit` 和 `cached_tokens` 反映真实的 LLM Prompt Caching 命中情况，前端明确展示缓存命中 token 数，未命中固定显示 `0`。**设计为提供商无关，支持后续切换 DeepSeek/Claude 官方 API。**

输出结果：

- 后端 `LLMCallRecord` 新增 `CachedTokens` 字段；
- 新增 `cacheTokensModifier` + `cachedChatModel` 包装器，利用 Eino 的 `WithResponseMessageModifier` 从原始 HTTP 响应解析各提供商缓存字段，统一回填到 `PromptTokenDetails.CachedTokens`；
- 新增 `ExtractCachedTokens()` 简单提取函数供 Emit 点使用；
- `CacheHit` 从 `CachedTokens > 0` 推导；
- 表格列明确展示 `缓存命中 Tokens` 数值；
- 统计摘要始终展示累计 `缓存命中 Tokens`。

> **注意**：当前实现在 OpenRouter 和 DeepSeek 官方 API 下均可工作。切换到 Claude 需先引入 Anthropic adapter 并确认缓存字段映射。

---

## 8. 验证方案

### 8.1 使用样例 Trace 验证执行轮次

验证文件：`data/traces/f5fce6e1-8670-402b-bd9c-aaa976c4cc5b.json`

预期：

- 顶部显示：`执行轮次 5 轮`（或等价语义）
- 时间线主标题显示：`第 1 轮` ~ `第 5 轮`
- 每项仍可查看原始 iteration：`3 / 5 / 8 / 9 / 10`

### 8.2 验证展开/收起交互

覆盖场景：

- 工具调用“输出摘要”
- “观察”区域
- 多个步骤同时存在可展开文本

预期：

- 首次点击“展开”能看到全文；
- 再次点击“收起”能恢复折叠；
- 多个块之间状态互不干扰；
- 外层 Collapse 开关后，内部状态表现符合预期。
- `copyable` 仍可正常工作，且不会干扰“展开 / 收起”按钮的点击与 hover。

### 8.3 验证缓存展示语义

预期：

- `cache_hit=true` + `cached_tokens=10318` → 明细列明确显示 `10,318`
- `cache_hit=false` + `cached_tokens=0` → 明细列明确显示 `0`
- 摘要卡片在 0 次命中时仍显示 `缓存命中 Tokens：0`
- 有命中时显示累计 `缓存命中 Tokens：M`

补充校验：

- 重新构建后端并运行一次真实诊断任务，确认 `cached_tokens` 字段在新 Trace 中出现非零值。
- 使用当前样例 Trace（修复前的历史数据，`cached_tokens` 缺失）时，前端应将其视为 `0` 正常展示，不报错。

### 8.4 回归验证

需要确认以下区域未被误伤：

- LLM 调用表格展开行
- 原始 JSON Tab
- 顶部统计信息中的 token / duration / tool count
- 无 `reasoning_history` 的空态页面

---

## 9. 风险与取舍

### 风险 1：前端重编号后，研发人员担心失去真实 iteration

应对：

- 不覆盖原始 `iteration`，仅把它降为辅助信息展示。

### 风险 2：继续依赖组件库内部折叠逻辑，问题可能复现

应对：

- 本次直接改为受控状态，不再把核心交互交给 `Paragraph ellipsis` 默认行为。
- 避免继续在同一 `Paragraph` 上叠加 `copyable + ellipsis.expandable` 作为主交互方案。

### 风险 3：展示轮次改造时误伤模型匹配逻辑

应对：

- 不改写原始 `step.iteration`；
- 展示轮次单独计算；
- `decisionCalls` 与步骤匹配直接按 `idx` 维持，避免把展示编号混入数据关联。

### 风险 4：历史 Trace 缺少 `cached_tokens` 字段

应对：

- 后端新增字段使用 Go 零值（`int` 默认 `0`），JSON 序列化无 `omitempty`，向后兼容。
- 前端对 `cached_tokens` 做 `?? 0` 兜底，确保读取历史 Trace 时不报错。

### 风险 5：切换 LLM 提供商后缓存数据不可用

应对：

- **DeepSeek 官方 API**：已通过 `cacheTokensModifier` 解决。modifier 从 `rawBody` 解析 `prompt_cache_hit_tokens` 并回填到 `PromptTokenDetails.CachedTokens`，与 OpenAI/OpenRouter 的标准路径统一。
- **Claude/Anthropic API**：需引入 Eino 的 Anthropic adapter（`eino-ext/components/model/anthropic`），确认其 `cache_read_input_tokens` 的映射路径。如 adapter 未映射，可扩展 `cacheTokensModifier` 或在 Anthropic adapter 层注册独立的 callback handler。
- **兜底策略**：`cacheTokensModifier` 设计为安全降级——`rawBody` 解析失败时返回原消息不报错，`ExtractCachedTokens()` 对 0 值返回 0，前端正常显示 `0`，不影响功能。

---

## 10. 最终建议

本次建议按以下策略推进：

1. **前端修复**：统一轮次展示语义、修复展开/收起、修正缓存状态展示文案。
2. **后端补齐**：在 `LLMCallRecord` 中新增 `CachedTokens` 字段，利用 Eino 的 `WithResponseMessageModifier` 实现 `cacheTokensModifier` 从原始 HTTP 响应中提取各提供商缓存字段，通过 `cachedChatModel` 包装器自动注入到每次 LLM 调用。3 个 Emit 点通过 `ExtractCachedTokens()` 读取标准路径。`CacheHit` 改为从 `CachedTokens > 0` 推导。
3. **提供商切换适配**：`cacheTokensModifier` 已覆盖 OpenAI/OpenRouter（标准路径透传）和 DeepSeek 官方（`rawBody` 解析 `prompt_cache_hit_tokens` 回填），切换到 Claude 时只需扩展 modifier 或由 Anthropic adapter 自行映射。
4. **前端实现定稿**：执行链模型匹配直接按 `idx` 关联；展开/收起统一由 `ExpandableText` 承担；缓存区统一显示 `cached_tokens` 数值，未命中显示 `0`。
5. **把后端语义统一作为后续增强项**：若后续需要在更多页面复用 iteration 语义，再补充 `display_iteration` 一类字段。

这样能在修正数据源准确性的同时，把前端的可理解性和可用性也修正到位。
