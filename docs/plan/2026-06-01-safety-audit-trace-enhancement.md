# SafetyAgent 安全审计 Trace 增强实施方案

> **状态**：待实施  
> **创建日期**：2026-06-01  
> **最后更新**：2026-06-01（评审修正版）  
> **目标**：补齐 `execute_safe_command` 安全审计链路在 Trace 中的可观测性，明确记录 SafetyAgent 是否调用 LLM、LLM 审计输入输出、规则审计结论与命令执行结果。

---

## 1. 背景

当前 `execute_safe_command` 存在**两条执行路径**：

**路径 A — ActionNode（主诊断流程）**：
```text
ActionNode.executeSafeCommand
  -> SafetyAgent.ExecuteSafeCommand（返回 *CommandResult，含 AuditInfo）
  -> RuleEngine / LLMAuditor
  -> Shell MCP ExecuteCommand
```

**路径 B — ReActLLM DeepQuery（深度调查流程）**：
```text
ReActLLM.buildTools 闭包
  -> SafeCommandExecutor.ExecuteSafeCommand（返回 string，AuditInfo 丢失）
  -> safetyAgentAdapter -> SafetyAgent.ExecuteSimple
  -> SafetyAgent.ExecuteSafeCommand
  -> RuleEngine / LLMAuditor
  -> Shell MCP ExecuteCommand
```

但是最新 trace 中只能看到主诊断 Agent 的 LLM 调用：

- `source = decision`
- `source = report`
- `source = deep_query`

看不到 SafetyAgent 审计阶段的 LLM 调用，也看不到结构化的审计结论。导致排查时无法区分：

1. 命令是否进入了 SafetyAgent。
2. 命令是被规则白名单放行、黑名单拒绝，还是进入了 LLM 审计。
3. LLM 审计输入、输出、耗时与 token 消耗是多少。
4. `execute_safe_command` 的 trace 结果是否来自真实执行、审计拒绝，还是审计失败。

本方案针对以下两个问题改造：

- **原因 3**：SafetyAgent 的 LLM 审计调用未写入当前 trace。
- **原因 4**：当前 trace 只记录主 Agent 的 `decision/report/deep_query` LLM，不记录 `safety audit` LLM。

---

## 2. 现状分析

### 2.1 当前 Trace LLM 记录点

当前 `LLMCallEvent` 只在以下路径 emit：

| 文件 | Source | 说明 |
|------|--------|------|
| `internal/agent/diagnosis/decision_node.go` | `decision` | 每轮决策 LLM |
| `internal/agent/diagnosis/report_node.go` | `report` | 最终报告 LLM |
| `internal/llm/react_llm.go` | `deep_query` | ReAct deep query 内部 LLM |

`internal/agent/safety/llm_auditor.go` 直接调用：

```go
response, err := a.llm.Generate(ctx, messages)
```

但没有 recorder，也没有 emit `LLMCallEvent`。

### 2.2 当前 Tool Trace 的信息缺口

`TraceToolExecution` 只记录：tool name、args、success、output、duration、cached、command。

未记录 `SafetyAgent` 的结构化审计信息：

```go
type AuditInfo struct {
    Allowed     bool
    SafetyLevel string
    Reason      string
    Advice      string
    Method      string // rule / llm
}
```

**路径 A（ActionNode）**：`ExecuteSafeCommand` 返回 `*CommandResult`（含 `AuditInfo`），但 `action_node.go:381` emit `ToolExecutedEvent` 时未将 `AuditInfo` 写入 trace。

**路径 B（ReActLLM）**：`SafeCommandExecutor` 接口只返回 `(string, error)`，adapter 调用 `ExecuteSimple` 丢弃了 `AuditInfo`，且 ReAct 框架内部执行工具时没有 emit `ToolExecutedEvent`。

### 2.3 关键代码引用

- `AuditInfo` 结构体：`internal/agent/safety/agent.go:32-38`
- `CommandResult`（含 AuditInfo）：`internal/agent/safety/agent.go:22-29`
- `ExecuteSimple`（丢弃 AuditInfo）：`internal/agent/safety/agent.go:276-300`
- `LLMAuditor.callLLM`（无 trace emit）：`internal/agent/safety/llm_auditor.go:89-105`
- `SafeCommandExecutor` 接口（string-only）：`internal/llm/react_llm.go:23-26`
- `ActionNode.executeSafeCommand`：`internal/agent/diagnosis/action_node.go:331-386`
- `ReActLLM.buildTools` 闭包：`internal/llm/react_llm.go:229-235`
- eino token usage 获取方式：`internal/llm/react_llm.go:313-316`（`msg.ResponseMeta.Usage`）

---

## 3. 目标

### 3.1 功能目标

1. 在 trace 的 `llm_calls` 中记录 SafetyAgent LLM 审计调用。
2. 新增 `source = "safety_audit"`，区分主诊断 LLM 与安全审计 LLM。
3. 在 `execute_safe_command` 的工具 trace 中记录结构化 `audit_info`。
4. 对规则审计和 LLM 审计统一留痕：
   - 白名单 allow：记录 `method=rule`。
   - 黑名单 deny：记录 `method=rule`。
   - unknown -> LLM audit：记录 `method=llm`，并记录对应 `llm_calls`。
5. 两条执行路径（ActionNode、ReActLLM DeepQuery）均写入 `audit_info`。
6. 保持现有安全行为不变：本次只增强可观测性，不改变 allow / deny 策略。

### 3.2 非目标

1. 不修改安全规则语义。
2. 不把所有白名单命令强制改为 LLM 审计。
3. 不改变 Shell MCP 执行协议。
4. 不重构完整 trace 存储层。
5. 不在 trace 中保存密钥、token、环境变量等敏感信息。
6. 不在 reasoning step 的 `TraceToolCallDetail` 中写入 `audit_info`（reasoning step 是决策阶段记录，审计结果在执行阶段才产生，强行同步需改 state 生命周期，收益低）。

---

## 4. 设计方案

### 4.1 扩展 Trace 数据结构

在 `internal/trace/types.go` 新增安全审计 trace 类型：

```go
type TraceAuditInfo struct {
    Allowed     bool   `json:"allowed"`
    SafetyLevel string `json:"safety_level"`
    Reason      string `json:"reason"`
    Advice      string `json:"advice,omitempty"`
    Method      string `json:"method"` // rule | llm
}
```

扩展 `TraceToolExecution`：

```go
type TraceToolExecution struct {
    ToolName   string                 `json:"tool_name"`
    Iteration  int                    `json:"iteration"`
    Args       map[string]interface{} `json:"args,omitempty"`
    Success    bool                   `json:"success"`
    Output     string                 `json:"output"`
    DurationMs int64                  `json:"duration_ms"`
    Timestamp  string                 `json:"timestamp"`
    Cached     bool                   `json:"cached"`
    Command    string                 `json:"command,omitempty"`
    AuditInfo  *TraceAuditInfo        `json:"audit_info,omitempty"`
}
```

`TraceToolCallDetail` **不修改**。reasoning step 记录的是"Agent 决定调用什么工具"（决策阶段），此时还没有执行命令，没有审计结果。审计信息只写入 `TraceToolExecution`（执行阶段记录）。

新增转换函数：

```go
func ConvertAuditInfo(info *safety.AuditInfo) *TraceAuditInfo {
    if info == nil {
        return nil
    }
    return &TraceAuditInfo{
        Allowed:     info.Allowed,
        SafetyLevel: info.SafetyLevel,
        Reason:      info.Reason,
        Advice:      info.Advice,
        Method:      info.Method,
    }
}
```

说明：

- 使用 `omitempty` 保持旧 trace 兼容。
- 只对 `execute_safe_command` 写入 `audit_info`。
- 其他工具不受影响。

### 4.2 扩展 LLMCallRecord source 约定

更新 `LLMCallRecord.Source` 注释与使用约定：

```go
// Source: "decision" | "report" | "deep_query" | "safety_audit"
```

`safety_audit` 记录字段：

| 字段 | 内容 |
|------|------|
| `model_type` | `light` |
| `model_name` | light 模型名 |
| `source` | `safety_audit` |
| `input` | Safety audit prompt，脱敏后 |
| `output` | Safety audit LLM 原始输出，脱敏后 |
| `prompt_tokens` | 如果 provider 返回 usage，则记录；否则为 0 |
| `completion_tokens` | 如果 provider 返回 usage，则记录；否则为 0 |
| `total_tokens` | 如果 provider 返回 usage，则记录；否则为 0 |
| `duration_ms` | LLM 审计耗时 |

### 4.3 为 LLMAuditor 注入 Recorder 与模型名

当前 `LLMAuditor` 只有：

```go
type LLMAuditor struct {
    llm       model.ChatModel
    promptReg *promptregistry.PromptRegistry
}
```

改为：

```go
type LLMAuditor struct {
    llm       model.ChatModel
    promptReg *promptregistry.PromptRegistry
    modelName string            // 新增：模型名称，用于 trace 记录
    recorder  *trace.TaskRecorder // 新增：临时注入，不持久持有
}
```

新增方法（不改构造函数签名，保持 `NewLLMAuditor(llm, promptReg)` 不变，现有 10 处测试调用零改动）：

```go
// WithModelName 设置模型名称，用于 trace 记录。返回自身以支持链式调用。
func (a *LLMAuditor) WithModelName(name string) *LLMAuditor {
    a.modelName = name
    return a
}

// WithRecorder 返回一个带 recorder 的 auditor 浅拷贝，用于单次审计调用。
// 不修改原 auditor，避免并发任务串扰。
func (a *LLMAuditor) WithRecorder(recorder *trace.TaskRecorder) *LLMAuditor {
    return &LLMAuditor{
        llm:       a.llm,
        promptReg: a.promptReg,
        modelName: a.modelName,
        recorder:  recorder,
    }
}
```

`main.go` 中初始化时设置模型名：

```go
auditor = safety.NewLLMAuditor(llmRouter.Light(), promptReg).WithModelName("deepseek-v4-flash")
```

### 4.4 在 Safety LLM 调用处 emit LLMCallEvent

修改 `internal/agent/safety/llm_auditor.go` 的 `callLLM`。

改造要点：

1. 在调用前记录 `start := time.Now()`。
2. 调用 `a.llm.Generate(ctx, messages)`。
3. 从 `response.ResponseMeta.Usage` 中读取 token（nil-safe 方式，参照 `react_llm.go:313-316` 的模式）。
4. 无论解析 JSON 是否成功，只要 LLM 有响应，就 emit `LLMCallEvent`。
5. 对 input/output 复用 `sanitizeTraceText` 脱敏函数。

伪代码：

```go
func (a *LLMAuditor) callLLM(ctx context.Context, prompt string) (*AuditResult, error) {
    messages := []*schema.Message{
        {Role: schema.User, Content: prompt},
    }

    start := time.Now()
    response, err := a.llm.Generate(ctx, messages)
    duration := time.Since(start)

    // emit LLMCallEvent（即使后续 JSON 解析失败也要记录）
    if a.recorder != nil && response != nil {
        var promptTokens, completionTokens, totalTokens int
        if response.ResponseMeta != nil && response.ResponseMeta.Usage != nil {
            usage := response.ResponseMeta.Usage
            promptTokens = usage.PromptTokens
            completionTokens = usage.CompletionTokens
            totalTokens = usage.TotalTokens
        }
        a.recorder.Emit(trace.LLMCallEvent{Call: trace.LLMCallRecord{
            ModelType:        "light",
            ModelName:        a.modelName,
            Source:           "safety_audit",
            DurationMs:       duration.Milliseconds(),
            Timestamp:        time.Now().Format(time.RFC3339),
            Input:            sanitizeTraceText(prompt),
            Output:           sanitizeTraceText(response.Content),
            PromptTokens:     promptTokens,
            CompletionTokens: completionTokens,
            TotalTokens:      totalTokens,
        }})
    }

    if err != nil {
        return nil, fmt.Errorf("LLM generate failed: %w", err)
    }

    // ... 原有 JSON 解析逻辑不变
}
```

注意：

- 如果 `Generate` 返回 error 且没有 response，不记录 LLMCall，只通过日志记录错误。
- 如果 provider 不返回 usage，token 字段保持 0，但仍记录 input/output/duration。
- Safety 审计 token 计入任务总 token，因为 `LLMCallEvent.apply` 会累加 token，代表真实成本。
- `sanitizeTraceText` 脱敏函数实现见 4.8 节。

### 4.5 两条执行路径的改造

#### 4.5.1 路径 A：ActionNode（已有 recorder + CommandResult）

`action_node.go:331-386` 已经有 `n.recorder` 和 `result.AuditInfo`，只需在 emit `ToolExecutedEvent` 时填入 `AuditInfo`：

```go
// action_node.go:381 附近，修改 emit
if n.recorder != nil {
    n.recorder.Emit(trc.ToolExecutedEvent{Execution: trc.TraceToolExecution{
        ToolName:  tc.Name,
        Iteration: s.GetIterationCount(),
        Args:      tc.Args,
        Success:   execRecord.Success,
        Output:    displayOutput,
        DurationMs: execRecord.DurationMs,
        Timestamp: execRecord.Timestamp.Format(time.RFC3339),
        Cached:    false,
        Command:   command,
        AuditInfo: trace.ConvertAuditInfo(result.AuditInfo), // 新增
    }})
}
```

**无需任何接口变更**，直接在现有 emit 处加一行。

#### 4.5.2 路径 B：ReActLLM DeepQuery（需要扩展接口）

ReActLLM 使用 `SafeCommandExecutor` 接口（只返回 string），AuditInfo 丢失。需要新增可选扩展接口：

```go
// react_llm.go — 新增可选接口，不修改现有 SafeCommandExecutor
type SafeCommandExecutorWithResult interface {
    SafeCommandExecutor
    ExecuteSafeCommandWithResult(ctx context.Context, command, reason string) (*safety.CommandResult, error)
}
```

ReActLLM 结构体新增字段：

```go
type ReActLLM struct {
    // ... 现有字段不变
    safeExecutorWithResult SafeCommandExecutorWithResult // 新增，可选
}
```

在 `SetRecorder` 时做接口断言：

```go
func (r *ReActLLM) SetRecorder(recorder *trc.TaskRecorder) {
    r.recorder = recorder
    if withResult, ok := r.safeExecutor.(SafeCommandExecutorWithResult); ok {
        r.safeExecutorWithResult = withResult
    }
}
```

`buildTools` 中的 execute_safe_command 闭包改为：

```go
executeSafeCommandTool, err := utils.InferTool("execute_safe_command", "...", func(ctx context.Context, input executeSafeCommandInput) (string, error) {
    // 优先走带回结果的路径
    if r.safeExecutorWithResult != nil && r.recorder != nil {
        result, err := r.safeExecutorWithResult.ExecuteSafeCommandWithResult(ctx, input.Command, input.Reason)
        if err != nil {
            return fmt.Sprintf("Error: %v", err), nil
        }
        // emit 工具 trace（含 AuditInfo）
        r.recorder.Emit(trc.ToolExecutedEvent{Execution: trc.TraceToolExecution{
            ToolName:  "execute_safe_command",
            Args:      map[string]interface{}{"command": input.Command, "reason": input.Reason},
            Success:   result.AuditInfo != nil && result.AuditInfo.Allowed,
            Output:    result.Output,
            AuditInfo: trace.ConvertAuditInfo(result.AuditInfo),
            Timestamp: time.Now().Format(time.RFC3339),
        }})
        if result.AuditInfo != nil && !result.AuditInfo.Allowed {
            return fmt.Sprintf("命令被安全审计拒绝。原因: %s。建议: %s",
                result.AuditInfo.Reason, result.AuditInfo.Advice), nil
        }
        return result.Output, nil
    }
    // fallback：旧路径（无 recorder 或 executor 不支持扩展接口）
    output, err := r.safeExecutor.ExecuteSafeCommand(ctx, input.Command, input.Reason)
    if err != nil {
        return fmt.Sprintf("Error: %v", err), nil
    }
    return output, nil
})
```

`safetyAgentAdapter`（`main.go`）实现扩展接口：

```go
// 已有方法，不改
func (a *safetyAgentAdapter) ExecuteSafeCommand(ctx context.Context, command, reason string) (string, error) {
    return a.safetyAgent.ExecuteSimple(ctx, command, reason)
}

// 新增方法
func (a *safetyAgentAdapter) ExecuteSafeCommandWithResult(ctx context.Context, command, reason string) (*safety.CommandResult, error) {
    return a.safetyAgent.ExecuteSafeCommand(ctx, &safety.CommandRequest{
        Command: command,
        Reason:  reason,
        Source:  "react",
    })
}
```

#### 4.5.3 将 TaskRecorder 传入 SafetyAgent / LLMAuditor（仅 LLM audit trace 需要）

对于 LLMCallEvent 的 emit，recorder 需要到达 `LLMAuditor.callLLM`。通过 `CommandRequest.Recorder` 传递：

扩展 `CommandRequest`：

```go
type CommandRequest struct {
    Command     string
    Reason      string
    Source      string
    Iteration   int
    ContextInfo map[string]string
    Recorder    *trace.TaskRecorder // 新增：可选，用于 LLM audit trace
}
```

新增 `TraceAwareAuditor` 接口：

```go
// TraceAwareAuditor 支持 trace 记录的审计器接口
type TraceAwareAuditor interface {
    AuditWithTrace(ctx context.Context, command, reason string, recorder *trace.TaskRecorder) (*AuditResult, error)
}
```

`LLMAuditor` 实现此接口：

```go
func (a *LLMAuditor) AuditWithTrace(ctx context.Context, command, reason string, recorder *trace.TaskRecorder) (*AuditResult, error) {
    return a.WithRecorder(recorder).Audit(ctx, command, reason)
}
```

`SafetyAgent.auditAndExecute` 中优先使用 `AuditWithTrace`：

```go
func (a *SafetyAgent) auditAndExecute(ctx context.Context, req *CommandRequest) (*CommandResult, error) {
    // ...
    var auditResult *AuditResult
    var err error

    if req.Recorder != nil {
        if traced, ok := a.auditor.(TraceAwareAuditor); ok {
            auditResult, err = traced.AuditWithTrace(ctx, req.Command, req.Reason, req.Recorder)
        } else {
            auditResult, err = a.auditor.Audit(ctx, req.Command, req.Reason)
        }
    } else {
        auditResult, err = a.auditor.Audit(ctx, req.Command, req.Reason)
    }
    // ... 后续逻辑不变
}
```

**向后兼容性**：

- 现有 `Auditor` 接口不变，`mockAuditor` 只实现 `Audit`，类型断言 `TraceAwareAuditor` 返回 false，走原路径。
- 现有所有 `CommandRequest{...}` 字面量使用命名字段，新增 `Recorder` 字段默认 nil，不影响。
- `ExecuteSimple` 不传 Recorder，走原路径。

### 4.6 ExecuteSimpleWithResult（ReActLLM 路径需要）

当前 `ExecuteSimple` 丢弃了 `AuditInfo`。为了 ReActLLM 路径能拿到完整 `CommandResult`，新增方法：

```go
func (a *SafetyAgent) ExecuteSimpleWithResult(ctx context.Context, command, reason string) (*CommandResult, error) {
    return a.ExecuteSafeCommand(ctx, &CommandRequest{
        Command: command,
        Reason:  reason,
        Source:  "simple",
    })
}
```

旧方法保留：

```go
func (a *SafetyAgent) ExecuteSimple(ctx context.Context, command, reason string) (string, error) {
    result, err := a.ExecuteSimpleWithResult(ctx, command, reason)
    if err != nil {
        return "", err
    }
    if !result.AuditInfo.Allowed {
        msg := fmt.Sprintf("Command rejected: %s\n", command)
        msg += fmt.Sprintf("Reason: %s\n", result.AuditInfo.Reason)
        if result.AuditInfo.Advice != "" {
            msg += fmt.Sprintf("Advice: %s\n", result.AuditInfo.Advice)
        }
        return msg, nil
    }
    return result.Stdout, nil
}
```

`safetyAgentAdapter.ExecuteSafeCommandWithResult` 直接调用 `ExecuteSafeCommand`，不经过 `ExecuteSimple`。

### 4.7 脱敏函数 sanitizeTraceText

新增 `internal/trace/sanitize.go`：

```go
package trace

import (
    "regexp"
)

// sensitivePatterns 匹配可能包含敏感信息的模式
var sensitivePatterns = []*regexp.Regexp{
    regexp.MustCompile(`(?i)(token|password|secret|authorization)\s*[:=]\s*\S+`),
    regexp.MustCompile(`(?i)Bearer\s+\S+`),
    regexp.MustCompile(`-----BEGIN.*?KEY-----[\s\S]*?-----END.*?KEY-----`),
    regexp.MustCompile(`(?i)kubeconfig[\s\S]{0,20}`),
}

// SanitizeTraceText 对写入 trace 的文本进行脱敏。
// 替换匹配到的敏感模式为 [REDACTED]。
func SanitizeTraceText(text string) string {
    result := text
    for _, p := range sensitivePatterns {
        result = p.ReplaceAllString(result, "[REDACTED]")
    }
    return result
}
```

### 4.8 记录效果示例

白名单 allow 示例（`execute_safe_command` 工具 trace）：

```json
{
  "tool_name": "execute_safe_command",
  "args": {
    "command": "journalctl -u kubelet --no-pager | tail -100",
    "reason": "查看 kubelet 日志"
  },
  "success": true,
  "output": "...",
  "cached": false,
  "audit_info": {
    "allowed": true,
    "safety_level": "safe",
    "reason": "命令在白名单中",
    "method": "rule"
  }
}
```

黑名单拒绝示例：

```json
{
  "tool_name": "execute_safe_command",
  "success": false,
  "output": "命令被安全审计拒绝...",
  "audit_info": {
    "allowed": false,
    "safety_level": "dangerous",
    "reason": "匹配黑名单规则: >\\s*/dev/",
    "advice": "Use read-only commands from the whitelist...",
    "method": "rule"
  }
}
```

LLM 审计示例（工具 trace + llm_calls）：

```json
{
  "tool_name": "execute_safe_command",
  "success": true,
  "audit_info": {
    "allowed": true,
    "safety_level": "warning",
    "reason": "只读诊断命令，风险较低",
    "advice": "限制输出范围",
    "method": "llm"
  }
}
```

```json
{
  "model_type": "light",
  "model_name": "deepseek-v4-flash",
  "source": "safety_audit",
  "input": "...",
  "output": "{\"safety_level\":\"warning\",...}",
  "duration_ms": 1234
}
```

---

## 5. 实施步骤

### 阶段一：Trace 类型扩展

1. 修改 `internal/trace/types.go`：
   - 新增 `TraceAuditInfo`。
   - 为 `TraceToolExecution` 增加 `AuditInfo *TraceAuditInfo`。
   - 更新 `LLMCallRecord.Source` 注释。
   - **不修改** `TraceToolCallDetail`。

2. 新增 `internal/trace/sanitize.go`：
   - 实现 `SanitizeTraceText` 脱敏函数。

3. 新增 `internal/trace/convert.go`：
   - 实现 `ConvertAuditInfo` 转换函数。

4. 修改相关测试：
   - `internal/trace/types_test.go`
   - `internal/trace/recorder_test.go`

### 阶段二：Safety Auditor Trace 记录

1. 修改 `internal/agent/safety/llm_auditor.go`：
   - `LLMAuditor` 增加 `modelName` 和 `recorder` 字段。
   - 新增 `WithModelName` 方法。
   - 新增 `WithRecorder` 方法。
   - 新增 `AuditWithTrace` 方法（实现 `TraceAwareAuditor` 接口）。
   - 在 `callLLM` 中 emit `source=safety_audit` 的 `LLMCallEvent`。
   - 记录 duration、input、output、usage。
   - usage 获取使用 nil-safe 方式：`if response.ResponseMeta != nil && response.ResponseMeta.Usage != nil { ... }`。

2. 修改 `internal/agent/safety/agent.go`：
   - `CommandRequest` 增加 `Recorder *trace.TaskRecorder`。
   - 新增 `TraceAwareAuditor` 接口定义。
   - `auditAndExecute` 优先调用 `TraceAwareAuditor.AuditWithTrace`（当 `req.Recorder != nil` 时）。
   - 新增 `ExecuteSimpleWithResult`，保留旧 `ExecuteSimple` 不变。

3. 修改 `cmd/k8s-analyzer/main.go`：
   - `safetyAgentAdapter` 新增 `ExecuteSafeCommandWithResult` 方法。
   - 初始化 `auditor` 时调用 `.WithModelName("deepseek-v4-flash")`。

4. 修改测试：
   - `internal/agent/safety/llm_auditor_test.go`：新增 `AuditWithTrace` 测试。
   - `internal/agent/safety/agent_test.go`：新增 `ExecuteSimpleWithResult` 测试。现有测试无需修改（`CommandRequest.Recorder` 默认 nil）。

### 阶段三：工具执行层接入结构化审计结果

1. **路径 A（ActionNode）**：修改 `internal/agent/diagnosis/action_node.go`：
   - 在 `executeSafeCommand` 方法的 `ToolExecutedEvent` emit 处，将 `result.AuditInfo` 转换为 `TraceAuditInfo` 并填入。

2. **路径 B（ReActLLM）**：修改 `internal/llm/react_llm.go`：
   - 新增 `SafeCommandExecutorWithResult` 接口定义。
   - `ReActLLM` 增加 `safeExecutorWithResult` 字段。
   - `SetRecorder` 中做接口断言。
   - `buildTools` 中的 execute_safe_command 闭包优先走扩展接口路径，emit `ToolExecutedEvent`（含 AuditInfo）。

3. 对拒绝命令，`success=false`，`output` 保留用户可读拒绝原因，同时 `audit_info` 保留结构化原因。

### 阶段四：前端/API 兼容展示（可选但建议）

1. API 层无需破坏性变更，JSON 会自然携带 `audit_info`。
2. 监控前端任务详情页增加展示：
   - 审计方式：rule / llm
   - 是否允许：allowed
   - 风险等级：safety_level
   - 理由：reason
   - 建议：advice
3. `llm_calls` 列表支持显示 `source=safety_audit`。

---

## 6. 验证方法

### 6.1 单元测试验证

#### 6.1.1 Rule allow 不调用 LLM，但写入 audit_info

使用白名单命令（如 `df -h`）构造测试。

期望：

- `AuditInfo.Method = "rule"`
- `AuditInfo.Allowed = true`
- `AuditInfo.SafetyLevel = "safe"`
- mock auditor 未被调用
- trace tool call 包含 `audit_info`

#### 6.1.2 Rule deny 不调用 LLM，但写入 audit_info

使用黑名单命令构造测试。需确认当前规则配置中的黑名单规则存在。

期望：

- `AuditInfo.Method = "rule"`
- `AuditInfo.Allowed = false`
- `AuditInfo.SafetyLevel = "dangerous"`
- mock auditor 未被调用
- trace tool call `success=false`
- `audit_info.reason` 包含拒绝原因

#### 6.1.3 Unknown 命令调用 LLM，并写入 llm_calls

选择一个不在白名单、不在黑名单的只读命令（如 `lsblk -f`）。

期望：

- rule result = `unknown`
- mock LLM auditor 被调用 1 次
- `AuditInfo.Method = "llm"`
- recorder 中出现 `source: "safety_audit"`
- `input` 包含命令和 reason
- `output` 包含 LLM 审计 JSON
- token usage 如果 mock 提供 usage，则累计到 trace token

#### 6.1.4 AuditWithTrace 正确注入 recorder

测试 `LLMAuditor.AuditWithTrace` 方法：

- 传入非 nil recorder → `callLLM` 内 emit `LLMCallEvent`
- 传入 nil recorder → 不 emit，不 panic
- `WithRecorder` 返回新实例，不修改原 auditor

#### 6.1.5 ReActLLM 路径 emit 工具 trace

测试 ReActLLM 闭包中 `SafeCommandExecutorWithResult` 路径：

- mock executor 实现 `SafeCommandExecutorWithResult`
- 闭包执行后 recorder 中出现 `ToolExecutedEvent` 且含 `AuditInfo`
- mock executor 不实现扩展接口时，fallback 到旧路径

运行测试：

```bash
go test ./internal/agent/safety ./internal/trace ./internal/llm
```

### 6.2 集成测试验证

#### 6.2.1 构造触发规则 allow 的实际诊断

运行：

```bash
go run ./cmd/k8s-analyzer --config configs/config.yaml "分析一下这个集群"
```

检查最新 trace JSON 文件，验证 `tool_executions` 中每个 `execute_safe_command` 记录的 `audit_info` 字段。

期望：

- 每个 `execute_safe_command` 的 `tool_executions` 记录都有 `audit_info`
- 白名单命令 `audit_info.method = rule`、`audit_info.allowed = true`

#### 6.2.2 构造触发 LLM audit 的命令

新增一个仅测试使用的 Go 集成测试，直接调用 SafetyAgent 执行未知但只读命令（如 `lsblk -f`）。

期望 trace 中 `llm_calls` 至少出现 1 条 `source: "safety_audit"` 记录。

#### 6.2.3 构造触发黑名单拒绝的命令

在集成测试中使用黑名单命令。

期望：

- `execute_safe_command` 的 `tool_executions.success = false`
- `audit_info.allowed = false`
- `audit_info.method = rule`
- `audit_info.reason` 包含黑名单原因

### 6.3 Trace JSON 验证

运行完成后检查 trace JSON 文件，验证以下内容：

验收标准：

1. `llm_calls` 中允许出现以下 source：
   - `decision`
   - `report`
   - `deep_query`
   - `safety_audit`
2. 当存在 unknown 命令审计时，`safety_audit` 数量大于 0。
3. 所有 `execute_safe_command` 的 `tool_executions` 记录都带 `audit_info`。
4. `audit_info.method` 只允许 `rule` 或 `llm`。
5. `audit_info.allowed=false` 时，必须有 `reason`。
6. 原有 trace 文件仍可被 monitor 正常读取。

### 6.4 Monitor 页面/API 验证

启动 monitor 并访问任务详情 API，验证返回的 JSON 中包含 `audit_info` 字段。

期望：

- 任务详情 API 返回 `audit_info`。
- 老 trace 没有 `audit_info` 时页面不报错（`omitempty` 保证）。

### 6.5 回归测试

```bash
go test ./...
```

如果存在外部依赖导致的非本次失败，需要记录失败项并确认与本改造无关。

---

## 7. 风险与注意事项

### 7.1 Token 统计变化

Safety LLM 调用写入 `LLMCallEvent` 后会被计入 `TaskTrace.token_usage`。

这是合理的，因为它反映真实 LLM 成本；但会导致新旧 trace 的 token 统计口径变化。

**影响范围**：

- 监控面板的 token 统计图表（如有基于阈值的告警，需调整阈值）
- 用户可见的 token 消耗数字

**缓解措施**：在 changelog 和监控 UI 上说明：

```text
vX.Y.Z 起，total_tokens 包含 safety_audit LLM 消耗。
```

### 7.2 并发任务 recorder 串扰

`LLMAuditor.WithRecorder` 返回新实例（浅拷贝），不修改原 auditor。每个任务的 recorder 只在 `WithRecorder` 创建的临时 auditor 上生效，任务结束后被 GC 回收。

**关键约束**：不要把 `TaskRecorder` 固定保存在全局 `LLMAuditor` 中。

### 7.3 敏感信息脱敏

Safety audit prompt 包含原始命令。写入 trace 前必须通过 `SanitizeTraceText` 脱敏，覆盖：

- token、password、secret、authorization header
- Bearer token
- PEM 私钥内容
- kubeconfig 片段

### 7.4 旧 trace 兼容

新增字段必须使用 `omitempty`，避免旧 trace 缺少字段导致前端或 API 报错。`TraceToolCallDetail` 不修改，确保 reasoning step 的 JSON 结构不变。

---

## 8. 验收标准

1. `execute_safe_command` 的每条 `tool_executions` 记录都能看到 `audit_info`。
2. 白名单命令记录 `method=rule`、`allowed=true`。
3. 黑名单命令记录 `method=rule`、`allowed=false`、拒绝原因。
4. unknown 命令进入 LLM 审计时，`llm_calls` 中出现 `source=safety_audit`。
5. Safety LLM 的 input/output/duration/model/token 能在 trace 中查看。
6. 原有 `decision/report/deep_query` LLM trace 不受影响。
7. `go test ./...` 通过，或仅存在明确记录的外部依赖型既有失败。
8. `k8s-monitor` 能正常读取新旧 trace。

---

## 9. 变更文件清单

| 文件 | 变更类型 | 说明 |
|------|---------|------|
| `internal/trace/types.go` | 修改 | 新增 `TraceAuditInfo`，`TraceToolExecution` 加 `AuditInfo` |
| `internal/trace/sanitize.go` | 新增 | `SanitizeTraceText` 脱敏函数 |
| `internal/trace/convert.go` | 新增 | `ConvertAuditInfo` 转换函数 |
| `internal/agent/safety/llm_auditor.go` | 修改 | 加 `modelName`/`recorder` 字段，`WithModelName`/`WithRecorder`/`AuditWithTrace` 方法，`callLLM` 中 emit trace |
| `internal/agent/safety/agent.go` | 修改 | `CommandRequest` 加 `Recorder`，新增 `TraceAwareAuditor` 接口，`auditAndExecute` 优先走 trace 路径，新增 `ExecuteSimpleWithResult` |
| `internal/agent/diagnosis/action_node.go` | 修改 | `executeSafeCommand` 的 emit 处加 `AuditInfo` |
| `internal/llm/react_llm.go` | 修改 | 新增 `SafeCommandExecutorWithResult` 接口，`ReActLLM` 加字段，`buildTools` 闭包改造 |
| `cmd/k8s-analyzer/main.go` | 修改 | adapter 加 `ExecuteSafeCommandWithResult`，auditor 初始化加 `.WithModelName(...)` |
| 测试文件 | 修改 | 新增 trace 相关测试，现有测试无需修改 |

## 10. 建议实施顺序

1. 先实现 trace 数据结构（阶段一）：`TraceAuditInfo`、`ConvertAuditInfo`、`SanitizeTraceText`。
2. 实现 SafetyAgent trace emit（阶段二）：`LLMAuditor` 改造、`TraceAwareAuditor` 接口、`auditAndExecute` 改造。
3. 改工具执行层（阶段三）：ActionNode 路径 + ReActLLM 路径。
4. 补齐所有测试。
5. 最后视需要更新 monitor 前端展示。

这样可以先保证数据落盘正确，再处理 UI 展示。
