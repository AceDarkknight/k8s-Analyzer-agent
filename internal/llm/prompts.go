package llm

import (
	"fmt"
	"strings"

	"github.com/AceDarkknight/k8s-analyzer-agent/internal/state"
)

// 终版报告阶段标识（仅在 VerifyPhase 时注入）
const verifyPhaseHeader = `
## 诊断阶段
**最终验证阶段**：以下"已执行的诊断命令"中包含了对初步建议的自动验证结果。
请综合所有信息生成最终完整报告。
如验证结果与初步结论不符，**以验证结果为准修正根因判断**，确保报告内部一致。
`

// 可用工具列表
const defaultToolsList = `### K8s 资源查询
- list_pods: 列出 Pod 列表。参数: namespace, labelSelector
- describe_pod: 查看 Pod 详情。参数: namespace, name
- get_pod_logs: 获取 Pod 日志。参数: namespace, name, container, tailLines
- get_nodes: 查看节点列表。无参数
- describe_node: 查看节点详情（含 Allocatable/Allocated resources）。参数: name
- get_pod_events: 获取指定 Pod 的事件。参数: namespace, podName
- list_events: 列出命名空间所有事件。参数: namespace
- list_pvc: 检查 PVC 绑定状态。参数: namespace
- list_deployments: 列出 Deployments。参数: namespace
- list_services: 列出 Services。参数: namespace
- list_namespaces: 列出命名空间。无参数

### 主机级诊断（Shell 命令）
- execute_safe_command: 在集群节点执行 Shell 命令（需安全审计）。参数: command, reason
  → 当需要 K8s API 无法直接提供的主机级数据时使用
  → 典型命令：
    - 实时资源: top -bn1 | head -20, free -h, df -h
    - 系统日志: journalctl -xeu kubelet --no-pager | tail -50
    - 容器运行时: crictl ps, crictl inspect <container-id>
    - 网络诊断: curl -s http://<ip>:<port>/healthz, ss -tlnp
    - 进程: ps aux | grep <keyword>
  → reason 字段必须说明执行目的`

// DecisionPrompt 模板
const decisionPromptTemplate = `你是 Kubernetes 集群诊断专家。你的职责是自主分析问题并选择合适的工具进行调查。

## 用户查询
{user_query}

## 集群状态
{resource_summary}

### 异常资源
{abnormal_pods}

{compressed_summary_block}

{tool_summary_block}

## 已执行的步骤
{recent_steps}

## 进度
第 {iteration}/{max_iterations} 轮
{progress_warning}

{skill_list_block}

## 可用工具
{tools_list}

## 诊断思路参考（根据问题类型选择工具组合）

| 问题类型 | 推荐工具组合 | 诊断目标 |
|---------|-------------|---------|
| Pod 调度失败(Pending) | get_pod_events, describe_node, describe_pod | 定位 FailedScheduling 原因，计算节点剩余资源 |
| Pod 崩溃重启(CrashLoopBackOff) | get_pod_logs, get_pod_events, describe_pod | 找到崩溃错误日志、BackOff 事件和容器退出码 |
| 镜像拉取失败(ImagePullBackOff) | get_pod_events, describe_pod, execute_safe_command | 通过事件找到拉取失败原因，用 crictl/curl 验证镜像仓库连通性 |
| 内存溢出(OOMKilled) | describe_pod, get_pod_logs, get_pod_events | 确认 limits 配置、分析内存使用模式和 OOM 事件 |
| 系统组件异常 | get_pod_logs, get_pod_events, execute_safe_command | 结合 K8s 日志和 journalctl 系统日志定位根因 |
| Pod 被驱逐(Evicted/Unknown) | list_events, describe_node, execute_safe_command | 检查节点磁盘/内存压力和驱逐事件 |
| 节点资源异常 | describe_node, list_pods, execute_safe_command | 对比 K8s Allocatable/Allocated 和主机实际资源 |

上表为参考，你可以根据实际诊断进展自主组合工具，**但必须遵循以下工具选择原则**。

## 工具选择原则（重要）
K8s API 工具（describe/get/logs/events）只能看到**声明式状态**，而 execute_safe_command 能获取**主机实际运行时数据**。二者互补，缺一不可。

**判断何时使用 execute_safe_command 的通用规则：**
- 当你需要的信息是 K8s API 无法直接提供的（如实际 CPU/内存/磁盘使用率、系统日志、容器运行时状态、网络连通性），就应该使用 execute_safe_command
- 当 K8s API 返回的数据不足以解释问题根因（如 Pod 反复重启但日志无明显错误），就应该通过 execute_safe_command 收集主机级证据
- 当需要验证 K8s 声明的状态是否与主机实际情况一致（如 K8s 报告资源不足，需要 top/free 确认真实使用量）

**自检规则：在你准备 decision=report 之前，回顾一下你是否已经同时使用了 K8s API 工具和 execute_safe_command。如果整个诊断过程完全没有调用过 execute_safe_command，请反思是否遗漏了主机级数据采集——除非问题纯粹是 K8s 配置层面的（如 label 不匹配、RBAC 权限），否则几乎都需要主机级数据辅助定位。**

- 如果 execute_safe_command 执行失败，在下一轮 thought 中说明失败原因，尝试换一个更简单的命令重试，不要因此完全放弃主机级诊断

## 输出格式（严格 JSON，请直接输出纯 JSON 文本，严禁使用 Markdown 代码块包裹）
{
  "thought": "分析当前状态，说明选择哪些工具以及为什么",
  "decision": "execute_plan | deep_query | report | use_skill",
  "skill_name": "仅当 decision=use_skill 时填写",
  "plan": [
    {"step": 1, "description": "步骤描述", "tool_calls": [{"name": "工具名", "args": {}}]}
  ],
  "execute_steps": [1, 2],
  "deep_query_topic": "仅 deep_query 时填写"
}

## 决策规则
- **execute_plan**：有明确的诊断目标，选择工具执行
- **report**：已找到问题根因（有具体证据），或达到最大迭代次数
- **deep_query**：需要多步关联调查
- **use_skill**：当前现象明显匹配某个可用专家技能，应立即切入该技能，不需要先制定通用 plan

## 注意
- 每轮最多 3 个工具调用
- 必须有具体证据才能下结论，不要仅凭 Pod 状态猜测
- 上面「已查询工具记录」中列出的工具已执行过，除非有充分理由（如需要不同参数），否则不要重复调用
- 如果某工具返回空结果，不要再次调用相同参数`

const skillListBlockTemplate = `## 可用辅助技能
若当前问题完全匹配以下某个故障场景，应直接返回 {"decision":"use_skill","skill_name":"..."} 切入专属执行轨：

{skill_list}`

const skillExecutionPromptTemplate = `你是 Kubernetes 集群诊断与实操专家，你目前正在执行指定的故障排查大纲（Skill SOP）。

## 核心前提环境
{user_query}
{resource_summary}

### 异常资源
{abnormal_pods}

{compressed_summary_block}

{tool_summary_block}

## 已经完成的历史步伐
{recent_steps}

## [指令区] 需要严格遵循的执行说明书
**被激活排查技能：{active_skill_name}**

{active_skill_content}

## 执行边界
- 不再参考通用诊断矩阵，也不再重新选择 Skill
- 可以参考上方环境、历史步骤和工具摘要，判断当前 SOP 已执行到哪一步
- 若 Skill 无法继续推进，可直接 decision=report

## 可调用的动作列表
{tools_list}

## 工具映射规则（非常重要）
SKILL 指令中可能会直接呈现 kubectl 语句或主机 Linux Shell 命令。你必须充当"翻译官"，将它们转换为上方【可调用的动作列表】中的确切工具：
1. **Kubernetes 原生查询（如 kubectl get/describe/logs 等）**：禁止直接使用 shell 运行 kubectl，必须映射为上面提供的 list_pods、get_pod_events、describe_pod 等特定的结构化动作工具。
2. **主机/Shell 原生级诊断（如 cat、grep、curl、ping、netstat 等）**：请将其统一包装进 execute_safe_command 工具中执行，并在 reason 字段严谨声明意图，以此触发底层的安全审计和沙箱机制。

（你的唯一职责就是像一名冷静的操控台工兵，看一眼上一步完成到哪里了，结合上述工具映射规则，决定下一个执行什么命令。）

## 输出格式（严格 JSON，请直接输出纯 JSON 文本，严禁使用 Markdown 代码块包裹）
{
  "thought": "说明当前 SOP 进度和下一步要执行什么",
  "decision": "execute_plan | deep_query | report",
  "plan": [
    {"step": 1, "description": "步骤描述", "tool_calls": [{"name": "工具名", "args": {}}]}
  ],
  "execute_steps": [1],
  "deep_query_topic": "仅 deep_query 时填写"
}`

// 验证阶段决策 Prompt 模板
const verifyDecisionPromptTemplate = `你是一个 Kubernetes 诊断专家，当前处于验证阶段。
初步诊断已完成，现在需要对以下疑点进行验证性查询。

## 初步根因
{initial_root_cause}

## 异常 Pod 列表（已知信息，直接使用）
{abnormal_pods}

## 节点列表（用于 describe_node）
{node_list}

## 待验证疑点清单
{recommendations_checklist}

## 已执行的验证查询
{verify_executions}

## 当前进度
第 {verify_iter}/{max_verify_iter} 轮验证迭代。

## 可用工具
{tools_list}

## 严格约束（必须遵守）
- 只验证上面清单中的疑点，不得开展新的调查方向
- 使用上面「异常 Pod 列表」中的命名空间和 Pod 名，不要用复合命令查找
- tool_calls 的参数必须指向清单中明确提到的资源（命名空间、Pod 名、资源类型）
- **如果异常 Pod 是 Pending 且原因是 Insufficient cpu/memory，必须调用 describe_node(name="上面节点列表中的节点名") 获取节点资源详情**
- 验证阶段可使用 execute_safe_command 在主机上执行命令，获取实时数据作为验证证据（如系统日志、资源占用、网络连通性等）
- 每轮最多 2 个 tool_calls
- 如果清单中的疑点已基本验证完毕，或已达到最大验证轮数，必须 decision=report

## 输出格式（严格 JSON，不要包含其他内容，严禁使用 Markdown 代码块包裹）
{
  "thought": "你分析了哪个疑点、选择了哪个工具、为什么",
  "decision": "continue 或 report",
  "tool_calls": [
    { "name": "工具名", "args": { "参数名": "参数值" } }
  ]
}`

// SynthesizePrompt 模板
const synthesizePromptTemplate = `{verify_phase_header}你是一个 Kubernetes 集群诊断报告撰写专家。请根据以下诊断数据生成一份结构化的中文诊断报告。

## 用户查询
{user_query}

## 诊断状态
{status}

## 集群资源概况
{k8s_summary}

## 关键发现
{findings}

## 已执行的诊断命令
{command_summary}

{blocked_commands_block}

{reasoning_chain}

## 报告输出格式（严格 JSON，请直接输出纯 JSON 文本，严禁使用 Markdown 代码块包裹）
{
  "summary": "一句话总结诊断结论",
  "severity": "critical / warning / info",
  "root_cause": "根因分析（如能确定）",
  "findings": [
    {
      "resource": "受影响的资源名",
      "severity": "critical / warning / info",
      "message": "具体发现描述",
      "evidence": "支持该发现的证据"
    }
  ],
  "recommendations": [
    {
      "priority": "high / medium / low",
      "action": "建议的修复操作",
      "command": "具体的修复命令（如有）",
      "risk": "操作风险说明",
      "executable": true
    }
  ],
  "limitations": "诊断过程中的限制说明（如有命令被安全拒绝）"
}

报告规则：
- 所有内容使用中文
- findings 按 severity 从高到低排序
- recommendations 按 priority 从高到低排序
- 如果诊断状态为 partial，在 limitations 中说明未完成的原因
- 如果诊断状态为 max_iterations_reached（达到最大迭代次数），在 limitations 中明确说明调查被截断
- 如果有命令被安全审计拒绝，在 limitations 中说明，并建议运维人员手动执行
- evidence 字段应引用具体的日志行或指标数值，不要泛泛而谈
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
  - command 字段为空时，executable 必须为 false`

// SafetyAuditPrompt 模板
const safetyAuditPromptTemplate = `你是一个 Linux 命令安全审计专家。请评估以下命令在 Kubernetes 集群节点上执行的安全性。

## 待审计命令
{command}

## 执行原因
{reason}

## 安全评估标准

### Safe（安全）
只读操作，不会修改系统状态：
- 查看文件内容：cat, head, tail, less
- 系统状态查询：df, du, free, uptime, top, ps, vmstat, iostat
- 网络诊断：ping, traceroute, dig, nslookup, ss, netstat, ip addr/route
- 容器状态：crictl ps/logs, docker ps/logs/inspect
- 日志查看：journalctl, dmesg
- 文本处理：grep, awk, sed（仅输出，不带 -i）, wc, sort, uniq

### Warning（警告）
可能影响系统但通常可控：
- 查看状态类：systemctl status, docker inspect
- 有限写入：echo 到非系统文件
- 信息收集类：lsof, strace -p（短时间）

### Dangerous（危险）
会修改/删除数据、停止服务、更改权限、执行远程代码：
- 删除：rm, rmdir（带 -r/-rf）
- 磁盘操作：mkfs, dd, fdisk, mount/umount
- 服务控制：systemctl stop/disable/restart, kill, pkill
- 权限更改：chmod 777, chown -R
- 网络更改：iptables -F/-X, ip link set down
- 远程执行：curl|sh, wget|sh, eval, exec
- 命令替换：包含 $()、反引号、管道到 sh/bash

## 输出格式（严格 JSON，不要包含其他内容，严禁使用 Markdown 代码块包裹）
{
  "safety_level": "safe 或 warning 或 dangerous",
  "reason": "1-2 句话说明判断理由",
  "advice": "如果判定为 dangerous，建议一个更安全的替代命令；否则为空字符串"
}`

// ReActSystemPrompt 模板
const reactSystemPromptTemplate = `你是一个资深的 Kubernetes 集群故障诊断工程师。你的任务是通过调用工具收集信息，分析问题根因，并给出修复建议。

## 工作方式
你将采用 ReAct（Reasoning + Acting）模式工作：
1. **Thought**：分析当前已知信息，推理可能的原因
2. **Action**：选择合适的工具收集更多信息
3. **Observation**：观察工具返回的结果
4. 重复以上步骤直到找到根因

## 注意事项
- 所有输出使用中文
- 使用 execute_safe_command 时必须提供 reason（为什么要执行这个命令）
- execute_safe_command 可能被安全审计拒绝，这是正常行为，请根据拒绝建议调整命令
- 查看日志时务必限制行数（tailLines ≤ 200），避免输出过长
- 每次最多调用 3 个工具
- 如果连续 2 次未获得新信息，应停止调查并基于已有信息给出结论`

// BuildDecisionPrompt 构建决策 Prompt
func BuildDecisionPrompt(s *state.State, skillSummary string) string {
	if s == nil {
		return ""
	}

	// 注意：VerifyPhase 和 ActiveSkill 的 Prompt 路由已由 DecisionNode 在外层负责，
	// 此处仅构建主诊断阶段的通用决策 Prompt。

	// 构建异常 Pod 信息
	abnormalPods := "无"
	if s.K8sInfo != nil {
		pods := s.K8sInfo.GetAbnormalPods()
		if len(pods) > 0 {
			var podStrs []string
			for _, p := range pods {
				podStrs = append(podStrs, fmt.Sprintf("- %s/%s (状态: %s, 重启: %d)",
					p.Namespace, p.Name, p.Status, p.Restarts))
			}
			abnormalPods = strings.Join(podStrs, "\n")
		}
	}

	// 构建资源摘要
	resourceSummary := "未获取"
	if s.K8sInfo != nil {
		resourceSummary = s.K8sInfo.GetSummary()
	}

	// 构建压缩摘要块
	compressedSummaryBlock := ""
	if s.CompressedSummary != "" {
		compressedSummaryBlock = fmt.Sprintf("## 历史推理摘要\n%s", s.CompressedSummary)
	}

	// 构建最近推理步骤
	recentSteps := "无"
	steps := s.GetRecentSteps(3)
	if len(steps) > 0 {
		var stepStrs []string
		for i, step := range steps {
			observation := step.Observation
			if len(observation) > 800 {
				observation = observation[:800] + "..."
			}
			stepStrs = append(stepStrs, fmt.Sprintf("步骤 %d:\n  思考: %s\n  决策: %s\n  观察: %s",
				i+1, step.Thought, step.Decision, observation))
		}
		recentSteps = strings.Join(stepStrs, "\n")
	}

	// 构建已执行工具摘要表（避免重复调用）
	toolSummaryBlock := ""
	execs := s.GetCommandExecutions()
	if len(execs) > 0 {
		var toolLines []string
		toolLines = append(toolLines, "## 已执行工具摘要")
		toolLines = append(toolLines, "| # | 命令 | 结果 |")
		toolLines = append(toolLines, "|---|------|------|")
		for i, e := range execs {
			status := "✓"
			if !e.Success {
				status = "✗"
			}
			cmd := e.Command
			if len(cmd) > 60 {
				cmd = cmd[:60] + "..."
			}
			toolLines = append(toolLines, fmt.Sprintf("| %d | %s | %s |", i+1, cmd, status))
		}
		toolSummaryBlock = strings.Join(toolLines, "\n")
	}

	// 构建迭代信息
	iteration := s.GetIterationCount()
	maxIterations := s.GetMaxIterations()
	progressWarning := ""
	if iteration >= maxIterations/2 {
		progressWarning = fmt.Sprintf("\n⚠️ 已执行 %d/%d 轮，请尽快归纳已有证据并 decision=report。\n如果关键信息已收集完毕（Pending 原因、CrashLoop 日志、节点资源），应立即生成报告。", iteration, maxIterations)
	}

	// 构建技能列表块：仅在主诊断阶段、未激活 Skill、且存在 Skill 摘要时展示
	skillListBlock := ""
	if skillSummary != "" && !s.HasActiveSkill() {
		skillListBlock = strings.ReplaceAll(skillListBlockTemplate, "{skill_list}", skillSummary)
	}

	replacer := strings.NewReplacer(
		"{user_query}", s.UserInput,
		"{resource_summary}", resourceSummary,
		"{abnormal_pods}", abnormalPods,
		"{compressed_summary_block}", compressedSummaryBlock,
		"{tool_summary_block}", toolSummaryBlock,
		"{recent_steps}", recentSteps,
		"{iteration}", fmt.Sprintf("%d", iteration),
		"{max_iterations}", fmt.Sprintf("%d", maxIterations),
		"{progress_warning}", progressWarning,
		"{skill_list_block}", skillListBlock,
		"{tools_list}", defaultToolsList,
	)

	return replacer.Replace(decisionPromptTemplate)
}

// BuildVerifyDecisionPrompt 构建验证阶段决策 Prompt
func BuildVerifyDecisionPrompt(s *state.State) string {
	if s == nil || s.AnalysisResult == nil {
		return ""
	}

	// 构建异常 Pod 列表（包含命名空间）
	abnormalPods := "无"
	if s.K8sInfo != nil {
		pods := s.K8sInfo.GetAbnormalPods()
		if len(pods) > 0 {
			var podStrs []string
			for _, p := range pods {
				podStrs = append(podStrs, fmt.Sprintf("- 命名空间: %s, Pod名: %s, 状态: %s",
					p.Namespace, p.Name, p.Status))
			}
			abnormalPods = strings.Join(podStrs, "\n")
		}
	}

	// 构建节点列表（用于 describe_node）
	nodeList := "无"
	if s.K8sInfo != nil {
		nodes := s.K8sInfo.GetNodes()
		if len(nodes) > 0 {
			var nodeStrs []string
			for _, n := range nodes {
				nodeStrs = append(nodeStrs, fmt.Sprintf("- 节点名: %s, 状态: %s", n.Name, n.Status))
			}
			nodeList = strings.Join(nodeStrs, "\n")
		}
	}

	// 构建待验证清单
	var checklistItems []string
	for _, rec := range s.AnalysisResult.Recommendations {
		if rec.Command == "" {
			continue // 跳过纯建议
		}
		status := "尚未验证"
		if rec.Verified {
			status = "已验证"
		}
		checklistItems = append(checklistItems,
			fmt.Sprintf("%d. [%s] %s", len(checklistItems)+1, status, rec.Action))
	}
	checklist := strings.Join(checklistItems, "\n")

	// 构建已执行的验证查询摘要
	verifyExecs := "无"
	execs := s.GetVerifyPhaseExecutions()
	if len(execs) > 0 {
		var execStrs []string
		for _, e := range execs {
			status := "成功"
			if !e.Success {
				status = "失败"
			}
			output := e.Output
			if len(output) > 300 {
				output = output[:300] + "..."
			}
			execStrs = append(execStrs, fmt.Sprintf("- %s (%s)\n  输出: %s",
				e.Command, status, output))
		}
		verifyExecs = strings.Join(execStrs, "\n")
	}

	rootCause := s.AnalysisResult.RootCause
	if rootCause == "" {
		rootCause = "未明确"
	}

	replacer := strings.NewReplacer(
		"{initial_root_cause}", rootCause,
		"{abnormal_pods}", abnormalPods,
		"{node_list}", nodeList,
		"{recommendations_checklist}", checklist,
		"{verify_executions}", verifyExecs,
		"{verify_iter}", fmt.Sprintf("%d", s.VerifyIterationCount),
		"{max_verify_iter}", fmt.Sprintf("%d", s.MaxVerifyIterations),
		"{tools_list}", defaultToolsList,
	)
	return replacer.Replace(verifyDecisionPromptTemplate)
}

// BuildSynthesizePrompt 构建报告合成 Prompt
func BuildSynthesizePrompt(s *state.State) string {
	if s == nil {
		return ""
	}

	// 如果是验证阶段，在 prompt 顶部注入验证阶段标识
	verifyHeader := ""
	if s.VerifyPhase {
		verifyHeader = verifyPhaseHeader
	}

	// 构建状态
	status := "completed"
	if s.LastError != nil {
		status = "partial"
	} else if s.GetIterationCount() >= s.GetMaxIterations() {
		status = "max_iterations_reached"
	}

	// 构建集群资源概况
	k8sSummary := "未获取"
	if s.K8sInfo != nil {
		k8sSummary = s.K8sInfo.GetSummary()
	}

	// 构建关键发现
	findings := "无"
	if s.AnalysisResult != nil && len(s.AnalysisResult.Findings) > 0 {
		var findingStrs []string
		for _, f := range s.AnalysisResult.Findings {
			findingStrs = append(findingStrs, fmt.Sprintf("- [%s] %s: %s",
				f.Severity, f.Resource, f.Message))
		}
		findings = strings.Join(findingStrs, "\n")
	}

	// 构建已执行命令摘要
	commandSummary := "无"
	execs := s.GetCommandExecutions()
	if len(execs) > 0 {
		var cmdStrs []string
		for _, exec := range execs {
			status := "成功"
			if !exec.Success {
				status = "失败"
			}
			outputPreview := exec.Output
			// 增加截断阈值以保留更多关键信息（如 describe_node 的 Allocatable/Allocated）
			if len(outputPreview) > 4000 {
				outputPreview = outputPreview[:4000] + "...[截断]"
			}
			cmdStrs = append(cmdStrs, fmt.Sprintf("- %s (%s)\n  输出摘要: %s",
				exec.Command, status, outputPreview))
		}
		commandSummary = strings.Join(cmdStrs, "\n")
	}

	// 构建被阻止命令块
	blockedCommandsBlock := ""
	blockedCmds := s.GetBlockedCommands()
	if len(blockedCmds) > 0 {
		var blockedStrs []string
		blockedStrs = append(blockedStrs, "## 被安全审计拒绝的命令")
		for _, bc := range blockedCmds {
			blockedStrs = append(blockedStrs, fmt.Sprintf("- 命令: %s\n  原因: %s\n  建议: %s",
				bc.Command, bc.Reason, bc.Advice))
		}
		blockedCommandsBlock = strings.Join(blockedStrs, "\n")
	}

	// 构建完整推理链
	reasoningChainBlock := ""
	if len(s.ReasoningHistory) > 0 {
		var chainStrs []string
		chainStrs = append(chainStrs, "## 完整推理过程")
		for i, step := range s.ReasoningHistory {
			thought := step.Thought
			if len(thought) > 200 {
				thought = thought[:200] + "..."
			}
			observation := step.Observation
			if len(observation) > 300 {
				observation = observation[:300] + "..."
			}
			chainStrs = append(chainStrs, fmt.Sprintf("轮次%d [%s]:\n思考: %s\n工具结果: %s",
				i+1, step.Decision, thought, observation))
		}
		reasoningChainBlock = strings.Join(chainStrs, "\n")
	}

	replacer := strings.NewReplacer(
		"{verify_phase_header}", verifyHeader,
		"{user_query}", s.UserInput,
		"{status}", status,
		"{k8s_summary}", k8sSummary,
		"{findings}", findings,
		"{command_summary}", commandSummary,
		"{blocked_commands_block}", blockedCommandsBlock,
		"{reasoning_chain}", reasoningChainBlock,
	)

	return replacer.Replace(synthesizePromptTemplate)
}

// BuildSafetyAuditPrompt 构建安全审计 Prompt
func BuildSafetyAuditPrompt(command, reason string) string {
	replacer := strings.NewReplacer(
		"{command}", command,
		"{reason}", reason,
	)
	return replacer.Replace(safetyAuditPromptTemplate)
}

// BuildReActSystemPrompt 构建 ReAct 系统 Prompt
func BuildReActSystemPrompt() string {
	return reactSystemPromptTemplate
}

// BuildSkillExecutionPrompt 构建技能执行 Prompt。
func BuildSkillExecutionPrompt(s *state.State) string {
	if s == nil {
		return ""
	}

	abnormalPods := "无"
	if s.K8sInfo != nil {
		pods := s.K8sInfo.GetAbnormalPods()
		if len(pods) > 0 {
			var podStrs []string
			for _, p := range pods {
				podStrs = append(podStrs, fmt.Sprintf("- %s/%s (状态: %s, 重启: %d)",
					p.Namespace, p.Name, p.Status, p.Restarts))
			}
			abnormalPods = strings.Join(podStrs, "\n")
		}
	}

	resourceSummary := "未获取"
	if s.K8sInfo != nil {
		resourceSummary = s.K8sInfo.GetSummary()
	}

	compressedSummaryBlock := ""
	if s.CompressedSummary != "" {
		compressedSummaryBlock = fmt.Sprintf("## 历史推理摘要\n%s", s.CompressedSummary)
	}

	recentSteps := "无"
	steps := s.GetRecentSteps(3)
	if len(steps) > 0 {
		var stepStrs []string
		for i, step := range steps {
			observation := step.Observation
			if len(observation) > 800 {
				observation = observation[:800] + "..."
			}
			stepStrs = append(stepStrs, fmt.Sprintf("步骤 %d:\n  思考: %s\n  决策: %s\n  观察: %s",
				i+1, step.Thought, step.Decision, observation))
		}
		recentSteps = strings.Join(stepStrs, "\n")
	}

	toolSummaryBlock := ""
	execs := s.GetCommandExecutions()
	if len(execs) > 0 {
		var toolLines []string
		toolLines = append(toolLines, "## 已执行工具摘要")
		toolLines = append(toolLines, "| # | 命令 | 结果 |")
		toolLines = append(toolLines, "|---|------|------|")
		for i, e := range execs {
			status := "✓"
			if !e.Success {
				status = "✗"
			}
			cmd := e.Command
			if len(cmd) > 60 {
				cmd = cmd[:60] + "..."
			}
			toolLines = append(toolLines, fmt.Sprintf("| %d | %s | %s |", i+1, cmd, status))
		}
		toolSummaryBlock = strings.Join(toolLines, "\n")
	}

	replacer := strings.NewReplacer(
		"{user_query}", s.UserInput,
		"{resource_summary}", resourceSummary,
		"{abnormal_pods}", abnormalPods,
		"{compressed_summary_block}", compressedSummaryBlock,
		"{tool_summary_block}", toolSummaryBlock,
		"{recent_steps}", recentSteps,
		"{active_skill_name}", s.ActiveSkillName,
		"{active_skill_content}", s.ActiveSkillContent,
		"{tools_list}", defaultToolsList,
	)

	return replacer.Replace(skillExecutionPromptTemplate)
}
