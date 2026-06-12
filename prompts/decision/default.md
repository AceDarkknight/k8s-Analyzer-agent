## 用户查询
{{.UserQuery}}

## 集群状态
{{.ResourceSummary}}

### 异常资源
{{.AbnormalPods}}

{{if .CompressedSummary}}
## 历史推理摘要
{{.CompressedSummary}}
{{end}}

{{.ToolSummary}}

## 已执行的步骤
{{.RecentSteps}}

## 进度
第 {{.Iteration}}/{{.MaxIterations}} 轮
{{if ge .Iteration (div .MaxIterations 2)}}
⚠️ 已执行 {{.Iteration}}/{{.MaxIterations}} 轮，请尽快归纳证据并 decision=report。
如果关键信息已收集完毕（Pending 原因、CrashLoop 日志、节点资源），应立即生成报告。
{{end}}

{{if .SkillList}}
## 可用辅助技能
若当前问题完全匹配以下某个故障场景，应直接返回 {"decision":"use_skill","skill_name":"..."} 切入专属执行轨：

{{.SkillList}}
{{end}}

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

## 注意
- 每轮最多 3 个工具调用
- 必须有具体证据才能下结论，不要仅凭 Pod 状态猜测
- 上面「已查询工具记录」中列出的工具已执行过，除非有充分理由（如需要不同参数），否则不要重复调用
- 如果某工具返回空结果，不要再次调用相同参数
