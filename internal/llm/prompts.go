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
const defaultToolsList = `- list_pods: 列出 Pod 列表。参数: namespace, labelSelector
- describe_pod: 查看 Pod 详情。参数: namespace, name
- get_pod_logs: 获取 Pod 日志。参数: namespace, name, container, tailLines
- list_events: 列出命名空间内所有 Events。参数: namespace
- get_pod_events: 专门获取某个 Pod 的 Events（重要：用于获取 FailedScheduling 完整原因）。参数: namespace, podName
- list_pvc: 检查 PVC 绑定状态。参数: namespace
- list_deployments: 列出 Deployments。参数: namespace
- list_services: 列出 Services。参数: namespace
- get_nodes: 查看节点状态。无参数
- list_namespaces: 列出命名空间。无参数
- execute_safe_command: 在集群节点上执行 Shell 命令（需通过安全审计）。参数: command, reason`

// DecisionPrompt 模板
const decisionPromptTemplate = `你是一个 Kubernetes 集群诊断专家。你的任务是分析集群状态，制定完整诊断计划。

## 用户查询
{user_query}

## 当前集群状态
{resource_summary}

### 异常资源
{abnormal_pods}

{compressed_summary_block}

## 最近推理步骤
{recent_steps}

## 当前进度
第 {iteration}/{max_iterations} 轮迭代。

## 可用工具
{tools_list}

## Thought 格式要求
你的 thought 必须包含以下三部分：
1. **当前认知**：基于已有信息，目前了解到什么？有哪些异常？
2. **完整诊断计划**：针对每个异常资源，列出需要执行的所有诊断步骤（describe、logs、events等）
3. **本轮执行**：说明本轮要执行计划中的哪些步骤

注意：如果之前有命令被安全审计拒绝，必须参考拒绝建议调整命令。

## 关键诊断约束（必须遵守）
- **Pending Pod 必须找到具体原因**：如果有 Pod 处于 Pending 状态，必须通过 describe_pod 获取 Events 中的 FailedScheduling 具体原因（如节点资源不足、亲和性不匹配、PVC 未绑定等）
- **CrashLoopBackOff 必须获取日志**：通过 get_pod_logs 找到崩溃的具体错误信息
- **不能仅凭 Pod 状态下结论**：必须有具体的错误/日志/事件证据

## 输出格式（严格 JSON，不要包含其他内容）
{
  "thought": "你的完整推理过程",
  "decision": "execute_plan 或 deep_query 或 report",
  "plan": [
    {
      "step": 1,
      "description": "步骤描述",
      "tool_calls": [{"name": "工具名", "args": {}}]
    }
  ],
  "execute_steps": [1, 2],
  "deep_query_topic": "仅当 decision=deep_query 时填写"
}

决策规则：
- 如果能制定完整诊断计划 → decision = "execute_plan"，填写 plan 和 execute_steps
- 如果需要多步关联调查，无法预先确定步骤 → decision = "deep_query"
- 如果已收集到足够信息可以给出诊断 → decision = "report"，plan 为空数组
- 如果已达到第 {max_iterations} 轮 → 必须 decision = "report"
- 每轮 execute_steps 最多包含 3 个步骤，但 plan 可以包含完整计划`

// 验证阶段决策 Prompt 模板
const verifyDecisionPromptTemplate = `你是一个 Kubernetes 诊断专家，当前处于验证阶段。
初步诊断已完成，现在需要对以下疑点进行验证性查询。

## 初步根因
{initial_root_cause}

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
- tool_calls 的参数必须指向清单中明确提到的资源（命名空间、Pod 名、资源类型）
- 每轮最多 2 个 tool_calls
- 如果清单中的疑点已基本验证完毕，或已达到最大验证轮数，必须 decision=report

## 输出格式（严格 JSON，不要包含其他内容）
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

## 报告输出格式（严格 JSON）
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

## 输出格式（严格 JSON，不要包含其他内容）
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

## 诊断思路引导

### Pod 异常排查
- CrashLoopBackOff → 查看 Pod 日志（get_pod_logs）→ 分析错误信息
- ImagePullBackOff → 检查镜像名称/仓库访问 → describe_pod 查看 Events
- **Pending → 重要！必须使用 get_pod_events 获取该 Pod 的专属事件，找到 FailedScheduling 的具体原因**：
  - 节点资源不足 (Insufficient cpu/memory)
  - 亲和性/节点选择器不匹配 (node(s) didn't match)
  - 污点/容忍不匹配 (node(s) had taint)
  - PVC 未绑定 (unbound PersistentVolumeClaim)
  - 端口冲突/资源配额限制
- OOMKilled → 查看容器内存限制 → 分析内存使用（execute_safe_command: free -m）
- Evicted → 查看节点状态 → 检查磁盘空间（execute_safe_command: df -h）

### 网络问题排查
- Service 不可达 → list_services → list_endpoints → 检查 Pod 标签是否匹配
- DNS 解析失败 → execute_safe_command: nslookup {service} → 检查 CoreDNS Pod

### 节点问题排查
- NotReady → get_nodes → describe 节点 → 检查 kubelet 日志

## 注意事项
- 所有输出使用中文
- 使用 execute_safe_command 时必须提供 reason（为什么要执行这个命令）
- execute_safe_command 可能被安全审计拒绝，这是正常行为，请根据拒绝建议调整命令
- 查看日志时务必限制行数（tailLines ≤ 200），避免输出过长
- 每次最多调用 3 个工具
- 如果连续 2 次未获得新信息，应停止调查并基于已有信息给出结论`

// BuildDecisionPrompt 构建决策 Prompt
func BuildDecisionPrompt(s *state.State) string {
	if s == nil {
		return ""
	}

	// 验证阶段使用专用 Prompt
	if s.VerifyPhase {
		return BuildVerifyDecisionPrompt(s)
	}

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
			if len(observation) > 200 {
				observation = observation[:200] + "..."
			}
			stepStrs = append(stepStrs, fmt.Sprintf("步骤 %d:\n  思考: %s\n  决策: %s\n  观察: %s",
				i+1, step.Thought, step.Decision, observation))
		}
		recentSteps = strings.Join(stepStrs, "\n")
	}

	// 构建迭代信息
	iteration := s.GetIterationCount()
	maxIterations := s.GetMaxIterations()

	replacer := strings.NewReplacer(
		"{user_query}", s.UserInput,
		"{resource_summary}", resourceSummary,
		"{abnormal_pods}", abnormalPods,
		"{compressed_summary_block}", compressedSummaryBlock,
		"{recent_steps}", recentSteps,
		"{iteration}", fmt.Sprintf("%d", iteration),
		"{max_iterations}", fmt.Sprintf("%d", maxIterations),
		"{tools_list}", defaultToolsList,
	)

	return replacer.Replace(decisionPromptTemplate)
}

// BuildVerifyDecisionPrompt 构建验证阶段决策 Prompt
func BuildVerifyDecisionPrompt(s *state.State) string {
	if s == nil || s.AnalysisResult == nil {
		return ""
	}

	// 构建待验证清单
	var checklistItems []string
	for i, rec := range s.AnalysisResult.Recommendations {
		status := "尚未验证"
		if rec.Verified {
			status = "已验证"
		}
		checklistItems = append(checklistItems,
			fmt.Sprintf("%d. [%s] %s", i+1, status, rec.Action))
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
			if len(outputPreview) > 500 {
				outputPreview = outputPreview[:500] + "...[截断]"
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
