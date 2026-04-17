# Skill 中间件接入计划

## 背景

### 问题与动机

当前诊断 Agent 的所有诊断策略固化在 `prompts.go` 的 `decisionPromptTemplate` 中：LLM 每次决策都面对同一套通用工具列表和诊断指引，无法针对不同故障场景（CrashLoopBackOff、OOMKilled、资源调度失败、网络不通等）提供专属的、结构化的诊断流程。

这带来几个具体问题：

- **诊断路径不稳定**：相同症状不同轮次的诊断流程差异大，依赖 LLM 临场发挥
- **缺乏领域经验沉淀**：已积累的 K8s 运维经验无法以可复用的形式固化
- **新场景添加成本高**：每次扩展新的诊断能力都需要修改核心 Prompt，改动风险大
- **无法模块化测试**：单个故障场景的诊断逻辑无法独立验证和优化

### 目标

引入 Skill 系统，将不同 K8s 故障场景的诊断流程模块化为独立的 `SKILL.md` 文件：
- 启动时自动扫描并注册可用技能，LLM 决策时感知到 Skill 列表
- 当问题场景命中某个 Skill 时，加载完整诊断指令并**切换大模型进入专属执行状态**。
- Skills 目录为空时，系统行为与改动前完全一致（完整降级）。
- `VerifyPhase` 保持现有专属验证轨优先级，不受 Skill 执行轨干扰。

---

## 方案选型

**只复用 Eino 官方提供的 `skill.Backend` 数据层**（包含 `skill.NewBackendFromFilesystem` 与文件解析），**自行实现 Skill 隔离执行的逻辑结构**。

选此方案的核心理由：SKILL.md 格式和 Backend 接口与官方 `agentskills.io` 规范严格保持一致，使得它完全兼容社区生态，而不需要受到 `ChatModelAgent` 的框架约束，完美贴合我们自己定制的 StateGraph！

---

## 架构设计

### 逻辑流转（单独的技能读取与执行机制）
为解决“通用矩阵与 Skill 指令互相干扰”的核心痛点，我们将在大模型层进行**“主诊断双轨 + 验证轨保留”** 的状态隔离：主诊断阶段切分为“通用评估轨 / Skill 执行轨”，验证阶段继续复用现有 `VerifyPhase` 专属 Prompt，不引入 Skill 分流。

```text
1. 启动阶段：
  skill.NewBackendFromFilesystem → 读取 Skills 目录预热元数据。

2. 第一轨：【通用评估与选型】（主诊断阶段、无 Skill 状态）
   - 大模型使用带“可用 Skill 列表”的 `decisionPromptTemplate`
   - LLM 通过分析现象，决定此时应该使用技能：返回 {decision: "use_skill", skill_name: "xxx"}
   - `use_skill` **仅允许出现在主诊断阶段**，验证阶段禁止返回该决策。

3. 中间截断与技能状态激活：
   - Graph主循环识别到 use_skill。
   - Loader 读取 SKILL.md 正文 → 在 State 中激活该 Skill。
   - 插入 Observation：“成功激活技能 xxx，后续请严格遵循...”
   - continue 开启下一层循环，重新执行 DecisionNode 读取。
   - 若 Skill 加载失败，仅记录 warn 日志并继续停留在第一轨，不中断主流程。

4. 第二轨：【专业执行层】（DecisionNode 感知到 Skill 状态）
   - 探测到 State 带有 Skill 标识后，不再注入通用诊断矩阵与 Skill 列表，但**保留环境上下文、历史步骤、压缩摘要、已执行工具摘要**，避免执行轨“失忆”。
   - 读取并使用专注的 `SkillExecutionPrompt`。此时主要遵循 SKILL.md 内的操作手顺（SOP），但仍可利用已有上下文判断当前执行进度。
   - 大模型将手顺转化为系统的工具调用（ToolCalls）或者报告（Report）。

5. 第三轨：【最终验证层】（沿用现有 VerifyPhase）
   - 一旦进入 `VerifyPhase`，Prompt 路由优先级最高，始终使用现有 `BuildVerifyDecisionPrompt`。
   - 即便此前已经激活 Skill，验证阶段也**不再触发 use_skill，也不走 SkillExecutionPrompt**。
```

### Skill 生命周期
因为 `State` 本身随单次用户请求的诊断会话存在，Skill 一旦激活，将陪伴整个主诊断会话直至最终 Report 生成。**第一版不提供 Clear，也不允许中途切换到另一个 Skill**；若首次命中有误，也继续按当前会话的单一 Skill 执行到结束，避免状态机复杂化。

### Skill 选择边界
- `use_skill` 只允许在主诊断阶段出现。
- 单次诊断会话至多激活一个 Skill。
- 已激活 Skill 后，后续轮次不再重新选择或覆盖其它 Skill。

### 安全约束（不变）
即便在大模型处于受 SKILL 指导的“专业执行区”，它规划出来的所有命令都还必须穿过原有的 `ActionNode` → `SafetyAgent` 的重重安全网，核心隔离栅栏安然无恙。

### 降级约定
- `enabled=false`：完全跳过 Skill 初始化，保持现有逻辑。
- `enabled=true` 且 `dir` 不存在：记录 warn 日志后降级为“无可用 Skill”，不阻断启动。
- `enabled=true` 且目录存在但没有任何 `SKILL.md`：视为正常空集，系统继续按现有主流程运行。
- Skill 载入或单个 Skill 获取失败：仅跳过本次 Skill 激活，继续走原有诊断逻辑。

---

## 实施计划

### 变更文件清单

| # | 文件 | 操作 | 核心改动 |
|---|------|----|---------|
| 1 | `go.mod` | MODIFY | 升级 eino 到 v0.8.0 挂载官方包 |
| 2 | `internal/skill/loader.go` | **NEW** | 包装 Eino API 的专属 Loader |
| 3 | `config.go` & `.yaml` | MODIFY | Skill 总开关与目录配置 |
| 4 | `state.go` | MODIFY | 支持 ActiveSkill 上层记录 |
| 5 | `parser.go` (含 _test.go) | MODIFY | 解析支持 use_skill 和修复遗留测试 |
| 6 | `prompts.go` | MODIFY | 增设双轨并行的重构引擎逻辑 |
| 7 | `decision_node.go` | MODIFY | 基于状态决定走哪条“提示词轨” |
| 8 | `graph.go` | MODIFY | 截停 use_skill 后开启下一重轨道 |
| 9 | `agent.go` & `main.go` | MODIFY | CLI 装载系统并层层向内注入 |


---

### Phase 1：升级依赖
```bash
go get github.com/cloudwego/eino@v0.8.0
go get github.com/cloudwego/eino-ext/adk/backend/local@<固定兼容版本>
go mod tidy
```

> 注意：`eino-ext` 不使用 `latest`，而是与 `eino v0.8.0` 一并锁定到明确兼容版本，避免依赖漂移。

### Phase 2：新建 Skill Loader
新建 `internal/skill/loader.go`（基于 einoSkill 官方接口，并显式支持“缺失目录 warn + 降级”）：
```go
package skill

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/cloudwego/eino-ext/adk/backend/local"
	einoSkill "github.com/cloudwego/eino/adk/middlewares/skill"
)

type Loader struct {
	backend einoSkill.Backend
	cached  []einoSkill.FrontMatter
}

func NewLoader(ctx context.Context, skillsDir string) (*Loader, error) {
	if skillsDir == "" {
		return &Loader{}, nil
	}
	if _, err := os.Stat(skillsDir); err != nil {
		if os.IsNotExist(err) {
			// 目录不存在：记录 warn 后降级，不阻断主流程
			return &Loader{}, nil
		}
		return nil, fmt.Errorf("skill: failed to stat skills dir: %w", err)
	}
	be, err := local.NewBackend(ctx, &local.Config{})
	if err != nil {
		return nil, fmt.Errorf("skill: failed to create local backend: %w", err)
	}
	skillBackend, err := einoSkill.NewBackendFromFilesystem(ctx,
		&einoSkill.BackendFromFilesystemConfig{Backend: be, BaseDir: skillsDir})
	if err != nil {
		return nil, fmt.Errorf("skill: failed to create skill backend: %w", err)
	}
	matters, err := skillBackend.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("skill: failed to list skills: %w", err)
	}
	return &Loader{backend: skillBackend, cached: matters}, nil
}

func (l *Loader) ListSkills() []einoSkill.FrontMatter {
	if l == nil { return nil }
	return l.cached
}

func (l *Loader) GetSkillContent(ctx context.Context, name string) (string, error) {
	if l == nil {
		return "", fmt.Errorf("skill loader is nil")
	}
	s, err := l.backend.Get(ctx, name)
	if err != nil {
		return "", fmt.Errorf("skill [%s] not found: %w", name, err)
	}
	return s.Content, nil
}

func (l *Loader) BuildSkillSummary() string {
	if l == nil || len(l.cached) == 0 { return "" }
	var sb strings.Builder
	for _, m := range l.cached {
		sb.WriteString(fmt.Sprintf("- **%s**：%s\n", m.Name, m.Description))
	}
	return sb.String()
}
```

> 说明：Eino backend 的“空目录”和“缺失目录”语义不同。这里由我们在 Loader 外层先处理“缺失目录降级”，避免把底层文件系统错误误判为“空目录”。

### Phase 3：更新 Config 配置结构
在 `internal/config/config.go` 中的 `AgentConfig` 结构体上方增加结构：
```go
// SkillConfig Skill 系统配置
type SkillConfig struct {
    Enabled bool   `yaml:"enabled"` 
    Dir     string `yaml:"dir"`    
}
```
并在 `Config` 大结构和 `config.yaml` 中分别挂载。由于本次不采用 `*bool`，因此**由默认配置文件显式写出 `enabled: true`** 来表达“默认开启”；代码结构保持 `bool` 即可。

额外约定：
- `dir` 默认值为 `skills`。
- 路径解析规则为**相对当前工作目录**；因此默认扫描 `./skills`。
- `enabled=false` 时不初始化 Loader。

### Phase 4：更新 Graph State 数据仓库
`internal/state/state.go` 新增专属记录区，并支持方法：
```go
// State 结构中挂载:
ActiveSkillName    string
ActiveSkillContent string

// 方法提供外部挂载：
func (s *State) ActivateSkill(name, content string) {
    if s == nil { return }
    s.ActiveSkillName = name
    s.ActiveSkillContent = content
}

func (s *State) HasActiveSkill() bool {
    return s != nil && s.ActiveSkillName != ""
}
// 注意：此次不提供 Clear 方法！贯彻一旦唤醒生命周期随诊结束的原则！
```

补充约束：
- `ActivateSkill` 只允许首次生效；若已有 `ActiveSkillName`，后续调用直接忽略或返回。
- 第一版不支持 Skill 切换，避免会话中途覆盖执行上下文。

### Phase 5：彻底剥离的双规制 Prompt 引擎
此步是解耦的灵魂！进入 `internal/llm/prompts.go`：

**1. 修改现有模板体补充列表占位并且强化 JSON**：
```diff
// decisionPromptTemplate:
+ {skill_list_block}
  ## 可用工具
  {tools_list}
  ## 输出格式（严格 JSON）
  {
+   "skill_name": "（仅 decision为use_skill 时，填写匹配的技能名）",
    "thought": ...

// 决策规则中补充：
+ - **use_skill**：用户现象明显匹配某专家技能，立刻选此项且不需要制定 plan
```

同时明确 Prompt 路由优先级：
1. `VerifyPhase == true` → `BuildVerifyDecisionPrompt`
2. `HasActiveSkill() == true` → `BuildSkillExecutionPrompt`
3. 其他情况 → 带 Skill 列表增强的普通 `BuildDecisionPrompt`

**2. 追加全新的独立状态 Prompt `skillExecutionPromptTemplate`**：
```go
// skillListBlockTemplate 依然按旧法预装载
const skillListBlockTemplate = `
## 可用辅助技能
若当前完全匹配以下故障情形，你应直接将 decision 声明为 "use_skill"：

{skill_list}`

// 追加第二轨：隔离执行空间
const skillExecutionPromptTemplate = `你是 Kubernetes 集群诊断与实操专家，你目前正在执行指定的故障排查大纲（Skill SOP）。

## 核心前提环境
{user_query}
{resource_summary}
{abnormal_pods}
{compressed_summary_block}
{tool_summary_block}

## 已经完成的历史步伐
{recent_steps}

## [指令区] 需要严格遵循的执行说明书
**被激活排查技能：{active_skill_name}**

{active_skill_content}

## 执行边界
- 不再参考通用诊断矩阵，也不再重新选择 Skill。
- 允许参考上方环境信息、已执行步骤、工具摘要，判断当前 SOP 进行到了哪一步。
- 若 Skill 无法继续推进，可直接 `decision=report`，不要退回 `use_skill` 或切换其他 Skill。

## 可调用的动作列表
{tools_list}

## 工具映射规则（非常重要）
SKILL 指令中可能会直接呈现 `kubectl` 语句或主机 `Linux Shell` 命令。你必须充当“翻译官”，将它们转换为上方【可调用的动作列表】中的确切工具：
1. **Kubernetes 原生查询（如 kubectl get/describe/logs 等）**：禁止直接使用 shell 运行 kubectl，必须映射为上面提供的 `list_pods`、`get_pod_events`、`describe_pod` 等特定的结构化动作工具。
2. **主机/Shell 原生级诊断（如 cat, grep, curl, ping, netstat 等）**：请将其统一包装进 `execute_safe_command` 工具中执行，并在 `reason` 字段严谨声明意图，以此触发底层的安全审计和沙箱机制。

（你的唯一职责就是像一名冷静的操控台工兵，看一眼上一步完成到哪里了，结合上述工具映射规则，决定下一个执行什么命令。）

## 输出格式（严格 JSON）
{
  "thought": "研判说明书步骤目前处在什么进度，下一步应使用什么命令核对",
  "decision": "execute_plan | deep_query | report",
  "plan": [
    {"step": 1, "description": "操作点描述", "tool_calls": [{"name": "工具名", "args": {}}]}
  ],
  "execute_steps": [1]
}
`
```

**3. 在同一文件扩展 Build 路由逻辑**：
```go
// 修改原有
func BuildDecisionPrompt(s *state.State, skillSummary string) string {
    // VerifyPhase 优先级最高；若处于验证阶段，仍走现有 Verify Prompt。
    // 自动判定仅当未激活且提供摘要时，才显示这块区域防止干扰。
    skillListBlock := ""
    if skillSummary != "" && !s.HasActiveSkill() {
        skillListBlock = strings.ReplaceAll(skillListBlockTemplate, "{skill_list}", skillSummary)
    }
    // ...装载现有的replacer
}

// 追加新的导出方法
func BuildSkillExecutionPrompt(s *state.State) string {
    // 注入全部必需环境以及 SOP 详情。实现执行流隔离调用。
    replacer := strings.NewReplacer(
        "{user_query}", s.UserInput,
        "{resource_summary}", s.GetK8sInfo().GetResourceSummary(),
        // ...(略: 等同基础环境构造)...
        "{active_skill_name}", s.ActiveSkillName,
        "{active_skill_content}", s.ActiveSkillContent,
        "{tools_list}", defaultToolsList,
    )
    return replacer.Replace(skillExecutionPromptTemplate)
}
```

**4. 结构端侧同步支持**：
`internal/llm/parser.go` 加上针对 `use_skill` 的 `json` 反序列及 map 后盾。

同时补充约束：
- `use_skill` 进入 `validDecisions`，但仅允许在主诊断阶段消费。
- `DecisionResult` / `DecisionOutput` 增加 `SkillName` 字段。
- 原有 `continue -> execute_plan` 兼容逻辑保留。

### Phase 6：更新 DecisionNode (提示词岔路分道)
`internal/agent/diagnosis/decision_node.go`

```diff
  func (n *DecisionNode) Execute(ctx context.Context, s *state.State) (*DecisionOutput, error) {
      // 3. 构建 prompt分道：
-     prompt := llm.BuildDecisionPrompt(s)
+     var prompt string
+     if s.VerifyPhase {
+         // 优先级最高：验证阶段不走 Skill 轨
+         prompt = llm.BuildVerifyDecisionPrompt(s)
+     } else if s.HasActiveSkill() {
+         // 彻底隔离，进入执行轨道
+         prompt = llm.BuildSkillExecutionPrompt(s)
+     } else {
+         // 继续走寻常规轨道
+         skillSummary := ""
+         if n.skillLoader != nil {
+             skillSummary = n.skillLoader.BuildSkillSummary()
+         }
+         prompt = llm.BuildDecisionPrompt(s, skillSummary)
+     }
```

并补充节点结构改动：
- `DecisionNode` 持有 `skillLoader` 依赖。
- 当 `s.HasActiveSkill()` 为真时，禁止再次接受新的 `use_skill` 作为有效切换结果。

### Phase 7：Graph 流向短路环 
`internal/agent/diagnosis/graph.go` 修改主循环拦截机制：

```go
        // 在解析并得到 decisionOutput 之后
        if !state.VerifyPhase && !state.HasActiveSkill() && decisionOutput.Decision == "use_skill" {
            skillName := decisionOutput.SkillName
            if g.skillLoader != nil && skillName != "" {
                content, err := g.skillLoader.GetSkillContent(ctx, skillName)
                if err != nil {
                    logger.Warn("Graph: fail loading skill, fallback to normal path", logger.Err(err))
                } else {
                    state.ActivateSkill(skillName, content)
                    
                    // 填补反馈，防止复读机制
                    if lastStep := state.GetLastReasoningStep(); lastStep != nil {
                        lastStep.Observation = fmt.Sprintf(
                            "【系统报告】：已成功切换进[%s]技能诊断流水线。请在下一次输出时遵照新技能的指南开始进行逐步的计划执行。", 
                            skillName)
                    }
                    
                    // 中途截断并重新循环，由于携带了 HasActiveSkill() 将自动走第二轨的特殊提示词重造思维！
                    continue 
                }
            }
        }
```

补充约束：
- 仅主诊断阶段拦截 `use_skill`。
- 已激活 Skill 后，Graph 不再接受二次激活。
- Skill 激活失败不报 fatal，只记录 warn 并继续原路径。

### Phase 8：修复 Parser_test 历史错误并组装服务
由于此前 `continue` 自动转化为 `execute_plan` 留下了测试报错，我们在本次顺带着：
- 执行 `go test ./internal/llm -v` 把该文件涉及断言不对的地方全部改平。
- 在最后一步进入 `agent.go` 与 `main.go` 进行总线组网并把 loader 对象丢给 graph，完成全部。
- `main.go` 中补充 Skill 配置解析与默认目录 `./skills` 注入。
- `agent.go` / `graph.go` / `decision_node.go` 贯通 loader 依赖注入。

---

## 降级与兜底设计

*   如果 `enabled=false`：完全跳过 Skill 系统。
*   如果 `enabled=true` 但 `./skills` 不存在：记录 warn 后继续按旧逻辑运行。
*   如果目录存在但没有任何 `SKILL.md`：视为正常空集，不报错。
*   如果因为单个 Skill 文件读取失败（Load失败）：直接退回走“第一轨旧世界逻辑”，无缝降压不报错。
*   如果执行过程中发生了断层：最终达到迭代极限退出给报告一样有基础信息能够依靠。

---

## 测试与验证方案

为了确保不仅主流程无碍，且“通用能力”与“Skill 特化运行”均正常并隔离，我们需要执行以下验证：

### 1. 单元测试 (Unit Tests)
*   **`internal/llm/parser_test.go`**：
    *   **动作**：修复之前损坏的 `continue` 用例。
    *   **动作**：新增针对 `{"decision": "use_skill", "skill_name": "xxx"}` 的解析测试。
*   **`internal/llm/prompts_test.go` (如需增加)**：
    *   **动作**：测试 `BuildDecisionPrompt` 在传入非空 `skillSummary` 且无 `ActiveSkill` 时，正确渲染出列表。
    *   **动作**：测试 `BuildSkillExecutionPrompt` 能正确组装出含有 `ActiveSkillContent` 的独立 Prompt，并且不包含任何通用排障法则。

### 2. 降级测试 (Fallback Test)
*   **目标**：确保在没有 `skills/` 目录或目录下没有技能时，Agent 功能毫不受损。
*   **动作**：移动或重命名现有的 `skills/` 目录。
*   **执行**：运行普通集群诊断查询（例：`go run ./cmd/k8s-analyzer "检查 default 命名空间 pod 状态"`）。
*   **期望**：Agent 正常调用基础工具，并在日志输出“Skill 未开启”或“Skill 目录不存在，已降级”等 warn/info 信息。

### 3. 手工 CLI 集成验证 (Manual Integration Check)
*   **测试准备**：在根目录创建 `skills/dummy-test/SKILL.md`。
    ```markdown
    ---
    name: dummy-test
    description: 专门用于集成测试的虚拟故障指导。只要用户的提示包含“虚拟测试”，即触发此技能。
    ---
    # 模拟技能测试
    你必须且只能做以下一件事：
    请执行主机 Shell 命令 `echo "hello skill integration"` 并利用 grep 提取 hello。
    ```
*   **执行**：运行 CLI `go run ./cmd/k8s-analyzer "执行虚拟测试"`。
*   **期望观测点与验收标准**：
    1.  **分流校验**：观察日志输出，LLM 的第一次响应必须返回 `decision="use_skill"`。
    2.  **State 切入校验**：观察日志输出 `【系统报告】：已成功切换进[dummy-test]技能诊断流水线` 被记录为一次 Iteration 的反馈中。
    3.  **规则翻译校验**：LLM 收到第二轨 `SkillExecutionPrompt` 后，必须没有随意直接生成未知工具，而是遵循工具映射规则生成如下合法的动作格式：
        `{"name": "execute_safe_command", "args": {"command": "echo \"hello skill integration\" | grep hello", "reason": "执行模拟技能"}}`
    4.  **安全审查打通校验**：终端提示需用户审批命令（如果开启拦截），输入 y 之后被执行。

### 4. 本次不纳入范围
- 暂不建设全自动 E2E 集成测试基建。
- 暂不支持 Skill 在验证阶段继续分流。
- 暂不支持单会话内切换多个 Skill。

（至此，Skill 的架构被彻底重构拆分为：通用判别树 + 无干扰执行通道的双分离范式，实现方案逻辑上的100%圆满。）
## 变更摘要（本次提交要点）
- 支持在主诊断轨中通过 use_skill 进行技能分流，/parser.go 增加 SkillName 字段用于携带技能名，且对 decision 的取值扩展到 use_skill。
- prompts.go 增加 SkillExecutionPrompt 模板，以及 BuildSkillExecutionPrompt 用于执行轨道的技能 SOP；并在普通决策提示中新增 skill_summary_block，以支持技能摘要的输出。引入技能摘要块的可选注入逻辑，保持未激活技能时与现有行为兼容。
- tests 增加了对 use_skill 的解析测试，确保 SkillName 能正确解析并在新字段上可用。
- 兼容性保持：VerifyPhase 仍然作为最高优先级，旧的 continue/execute_plan 流保持向后兼容。
