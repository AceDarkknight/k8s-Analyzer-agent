package diagnosis

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cloudwego/eino/schema"

	"github.com/AceDarkknight/k8s-analyzer-agent/internal/llm"
	"github.com/AceDarkknight/k8s-analyzer-agent/internal/llm/promptregistry"
	"github.com/AceDarkknight/k8s-analyzer-agent/internal/logger"
	skillpkg "github.com/AceDarkknight/k8s-analyzer-agent/internal/skill"
	"github.com/AceDarkknight/k8s-analyzer-agent/internal/state"
	trc "github.com/AceDarkknight/k8s-analyzer-agent/internal/trace"
)

// DecisionNode 决策节点
type DecisionNode struct {
	router      *llm.LLMRouter
	skillLoader *skillpkg.Loader
	recorder    *trc.TaskRecorder
	promptReg   *promptregistry.PromptRegistry
}

// DecisionOutput 决策输出
type DecisionOutput struct {
	Decision       string
	SkillName      string
	Thought        string
	ToolCalls      []state.ToolCall // 兼容旧模式
	Plan           []state.PlanStep // 新模式：完整计划
	ExecuteSteps   []int            // 本轮要执行的步骤编号
	DeepQueryTopic string
}

// NewDecisionNode 创建新的决策节点
func NewDecisionNode(router *llm.LLMRouter, skillLoader *skillpkg.Loader, recorder *trc.TaskRecorder, promptReg *promptregistry.PromptRegistry) *DecisionNode {
	return &DecisionNode{
		router:      router,
		skillLoader: skillLoader,
		recorder:    recorder,
		promptReg:   promptReg,
	}
}

// Execute 执行决策
func (n *DecisionNode) Execute(ctx context.Context, s *state.State) (*DecisionOutput, error) {
	logger.Info("DecisionNode: starting decision making",
		logger.Int("iteration", s.GetIterationCount()),
		logger.Int("max_iterations", s.GetMaxIterations()))

	// 验证阶段：自增计数，超限则强制 report
	if s.VerifyPhase {
		exceeded := s.IncrementVerifyIteration()
		if exceeded {
			logger.Info("DecisionNode: verify iterations exhausted, forcing report",
				logger.Int("verify_iter", s.VerifyIterationCount),
				logger.Int("max_verify_iter", s.MaxVerifyIterations))
			return &DecisionOutput{
				Decision:  "report",
				Thought:   fmt.Sprintf("验证迭代已达上限 %d 轮，强制生成终版报告", s.MaxVerifyIterations),
				ToolCalls: []state.ToolCall{},
			}, nil
		}
		// 验证阶段使用专用 Prompt（BuildDecisionPrompt 内部自动分发）
	}

	// 主诊断阶段：检查是否达到 MaxIterations
	if !s.VerifyPhase && s.GetIterationCount() >= s.GetMaxIterations() {
		logger.Info("DecisionNode: max iterations reached, forcing report")
		return &DecisionOutput{
			Decision:  "report",
			Thought:   "已达到最大迭代次数，需要生成报告",
			ToolCalls: []state.ToolCall{},
		}, nil
	}

	// 主诊断阶段：检查缓存命中率（连续 2 轮 100% 命中 → 无新信息，提前终止）
	if !s.VerifyPhase && s.GetIterationCount() >= 2 {
		allCacheHit := true
		for i := s.GetIterationCount() - 1; i >= s.GetIterationCount()-2 && i >= 1; i-- {
			stats := s.GetRoundCacheStats(i)
			if stats == nil || stats.TotalCalls == 0 || stats.CacheHits < stats.TotalCalls {
				allCacheHit = false
				break
			}
		}
		if allCacheHit {
			logger.Info("DecisionNode: consecutive rounds all cache hits, no new info, forcing report")
			return &DecisionOutput{
				Decision:  "report",
				Thought:   "连续 2 轮工具调用全部命中缓存，没有新信息，基于已有数据生成报告",
				ToolCalls: []state.ToolCall{},
			}, nil
		}
	}

	// 主诊断阶段：增加迭代计数
	if !s.VerifyPhase {
		s.IncrementIteration()
	}

	// 3. 构建 prompt（优先 Registry，失败回退 legacy）
	prompt := n.buildPrompt(ctx, s)
	if prompt == "" {
		logger.Warn("DecisionNode: empty prompt generated")
		return n.fallbackDecision(s), nil
	}

	// 4. 调用 LLM
	messages := []*schema.Message{
		schema.UserMessage(prompt),
	}

	llmStart := time.Now()
	response, usage, err := n.router.GenerateWithLight(ctx, messages)
	llmDuration := time.Since(llmStart)
	if err != nil {
		logger.Error("DecisionNode: LLM generation failed", logger.Err(err))
		return n.fallbackDecision(s), nil
	}
	if usage != nil {
		s.AccumulateTokenUsage(usage)
	}

	if response == nil || response.Content == "" {
		logger.Warn("DecisionNode: empty LLM response")
		return n.fallbackDecision(s), nil
	}

	// 5. 解析响应
	result, err := llm.ParseDecisionResponse(response.Content)
	if err != nil {
		logger.Error("DecisionNode: failed to parse decision response", logger.Err(err))
		return n.fallbackDecision(s), nil
	}

	// 6. 构建 ToolCalls（从 execute_steps 中提取）
	var toolCalls []state.ToolCall
	if result.Decision == "execute_plan" && len(result.ExecuteSteps) > 0 {
		for _, stepNum := range result.ExecuteSteps {
			for _, planStep := range result.Plan {
				if planStep.Step == stepNum {
					toolCalls = append(toolCalls, planStep.ToolCalls...)
					break
				}
			}
		}
	} else {
		toolCalls = result.ToolCalls
	}

	// 7. 添加 ReasoningStep 到 state
	step := state.ReasoningStep{
		Iteration:      s.GetIterationCount(),
		Timestamp:      time.Now(),
		Thought:        result.Thought,
		Decision:       result.Decision,
		DeepQueryTopic: result.DeepQueryTopic,
		ToolCalls:      toolCalls,
	}
	if usage != nil {
		step.TokensUsed = usage.TotalTokens
	}
	s.AddReasoningStep(step)
	if n.recorder != nil {
		n.recorder.Emit(trc.ReasoningStepUpdatedEvent{Step: step})
		if usage != nil {
			n.recorder.Emit(trc.LLMCallEvent{Call: trc.LLMCallRecord{
				ModelType:        "light",
				ModelName:        n.router.LightModelName(),
				Source:           "decision",
				PromptTokens:     usage.PromptTokens,
				CompletionTokens: usage.CompletionTokens,
				TotalTokens:      usage.TotalTokens,
				DurationMs:       llmDuration.Milliseconds(),
				Timestamp:        time.Now().Format(time.RFC3339),
				Input:            prompt,
				Output:           response.Content,
			}})
		}
	}

	logger.Info("DecisionNode: decision made",
		logger.String("decision", result.Decision),
		logger.Int("tool_calls", len(toolCalls)),
		logger.Int("plan_steps", len(result.Plan)))

	// 转换 Plan
	var plan []state.PlanStep
	for _, ps := range result.Plan {
		plan = append(plan, state.PlanStep{
			Step:        ps.Step,
			Description: ps.Description,
			ToolCalls:   ps.ToolCalls,
		})
	}

	return &DecisionOutput{
		Decision:       result.Decision,
		SkillName:      result.SkillName,
		Thought:        result.Thought,
		ToolCalls:      toolCalls,
		Plan:           plan,
		ExecuteSteps:   result.ExecuteSteps,
		DeepQueryTopic: result.DeepQueryTopic,
	}, nil
}

func (n *DecisionNode) buildPrompt(ctx context.Context, s *state.State) string {
	if n.promptReg != nil {
		prompt, err := n.buildRegistryPrompt(ctx, s)
		if err == nil && strings.TrimSpace(prompt) != "" {
			return prompt
		}
		if err != nil {
			logger.Warn("DecisionNode: prompt registry build failed, fallback to legacy", logger.Err(err))
		}
	}

	return n.buildPromptLegacy(s)
}

func (n *DecisionNode) buildRegistryPrompt(ctx context.Context, s *state.State) (string, error) {
	if s.VerifyPhase {
		return n.promptReg.BuildVerify(ctx, "verify", promptregistry.VersionDefault, n.buildVerifyContext(s))
	}
	if s.HasActiveSkill() {
		return n.promptReg.BuildSkill(ctx, "skill", promptregistry.VersionDefault, n.buildSkillContext(s))
	}
	return n.promptReg.BuildDecision(ctx, "decision", promptregistry.VersionDefault, n.buildDecisionContext(s))
}

func (n *DecisionNode) buildPromptLegacy(s *state.State) string {
	if s.VerifyPhase {
		return llm.BuildVerifyDecisionPrompt(s)
	}
	if s.HasActiveSkill() {
		return llm.BuildSkillExecutionPrompt(s)
	}
	skillSummary := ""
	if n.skillLoader != nil {
		skillSummary = n.skillLoader.BuildSkillSummary()
	}
	return llm.BuildDecisionPrompt(s, skillSummary)
}

func (n *DecisionNode) buildDecisionContext(s *state.State) *promptregistry.DecisionPromptContext {
	rc := &promptregistry.DecisionPromptContext{
		UserQuery:         s.UserInput,
		Iteration:         s.GetIterationCount(),
		MaxIterations:     s.GetMaxIterations(),
		CompressedSummary: s.CompressedSummary,
	}

	if s.K8sInfo != nil {
		rc.ResourceSummary = s.K8sInfo.GetSummary()
		pods := s.K8sInfo.GetAbnormalPods()
		mainLines := make([]string, 0, len(pods))
		for _, p := range pods {
			mainLines = append(mainLines, fmt.Sprintf("- %s/%s (状态: %s, 重启: %d)", p.Namespace, p.Name, p.Status, p.Restarts))
		}
		if len(mainLines) == 0 {
			rc.AbnormalPods = "无"
		} else {
			rc.AbnormalPods = strings.Join(mainLines, "\n")
		}
	}
	if rc.ResourceSummary == "" {
		rc.ResourceSummary = "未获取"
	}

	steps := s.GetRecentSteps(3)
	if len(steps) == 0 {
		rc.RecentSteps = "无"
	} else {
		lines := make([]string, 0, len(steps))
		for i, step := range steps {
			obs := step.Observation
			if len(obs) > 800 {
				obs = obs[:800] + "..."
			}
			lines = append(lines, fmt.Sprintf("步骤 %d:\n  思考: %s\n  决策: %s\n  观察: %s", i+1, step.Thought, step.Decision, obs))
		}
		rc.RecentSteps = strings.Join(lines, "\n")
	}

	execs := s.GetCommandExecutions()
	if len(execs) > 0 {
		lines := []string{"## 已执行工具摘要", "| # | 命令 | 结果 |", "|---|------|------|"}
		for i, e := range execs {
			status := "✓"
			if !e.Success {
				status = "✗"
			}
			cmd := e.Command
			if len(cmd) > 60 {
				cmd = cmd[:60] + "..."
			}
			lines = append(lines, fmt.Sprintf("| %d | %s | %s |", i+1, cmd, status))
		}
		rc.ToolSummary = strings.Join(lines, "\n")
	}

	if n.skillLoader != nil && !s.HasActiveSkill() {
		rc.SkillList = n.skillLoader.BuildSkillSummary()
	}

	return rc
}

func (n *DecisionNode) buildVerifyContext(s *state.State) *promptregistry.VerifyPromptContext {
	rc := &promptregistry.VerifyPromptContext{}

	if s.K8sInfo != nil {
		pods := s.K8sInfo.GetAbnormalPods()
		verifyLines := make([]string, 0, len(pods))
		for _, p := range pods {
			verifyLines = append(verifyLines, fmt.Sprintf("- 命名空间: %s, Pod名: %s, 状态: %s", p.Namespace, p.Name, p.Status))
		}
		if len(verifyLines) == 0 {
			rc.AbnormalPodsVerify = "无"
		} else {
			rc.AbnormalPodsVerify = strings.Join(verifyLines, "\n")
		}

		nodes := s.K8sInfo.GetNodes()
		nodeLines := make([]string, 0, len(nodes))
		for _, node := range nodes {
			nodeLines = append(nodeLines, fmt.Sprintf("- 节点名: %s, 状态: %s", node.Name, node.Status))
		}
		if len(nodeLines) == 0 {
			rc.NodeList = "无"
		} else {
			rc.NodeList = strings.Join(nodeLines, "\n")
		}
	}

	if s.VerifyPhase && s.AnalysisResult != nil {
		rc.InitialRootCause = s.AnalysisResult.RootCause
		rc.VerifyIter = s.VerifyIterationCount
		rc.MaxVerifyIter = s.MaxVerifyIterations

		items := make([]string, 0, len(s.AnalysisResult.Recommendations))
		for _, rec := range s.AnalysisResult.Recommendations {
			if rec.Command == "" {
				continue
			}
			status := "尚未验证"
			if rec.Verified {
				status = "已验证"
			}
			items = append(items, fmt.Sprintf("%d. [%s] %s", len(items)+1, status, rec.Action))
		}
		if len(items) == 0 {
			rc.RecommendationsChecklist = "无"
		} else {
			rc.RecommendationsChecklist = strings.Join(items, "\n")
		}

		verifyExecs := s.GetVerifyPhaseExecutions()
		if len(verifyExecs) == 0 {
			rc.VerifyExecutions = "无"
		} else {
			lines := make([]string, 0, len(verifyExecs))
			for _, e := range verifyExecs {
				status := "成功"
				if !e.Success {
					status = "失败"
				}
				out := e.Output
				if len(out) > 300 {
					out = out[:300] + "..."
				}
				lines = append(lines, fmt.Sprintf("- %s (%s)\n  输出: %s", e.Command, status, out))
			}
			rc.VerifyExecutions = strings.Join(lines, "\n")
		}
	}

	if rc.InitialRootCause == "" {
		rc.InitialRootCause = "未提供"
	}
	if rc.AbnormalPodsVerify == "" {
		rc.AbnormalPodsVerify = "无"
	}
	if rc.NodeList == "" {
		rc.NodeList = "无"
	}
	if rc.RecommendationsChecklist == "" {
		rc.RecommendationsChecklist = "无"
	}
	if rc.VerifyExecutions == "" {
		rc.VerifyExecutions = "无"
	}

	return rc
}

func (n *DecisionNode) buildSkillContext(s *state.State) *promptregistry.SkillPromptContext {
	rc := &promptregistry.SkillPromptContext{
		UserQuery:          s.UserInput,
		CompressedSummary:  s.CompressedSummary,
		ActiveSkillName:    s.ActiveSkillName,
		ActiveSkillContent: s.ActiveSkillContent,
	}

	if s.K8sInfo != nil {
		rc.ResourceSummary = s.K8sInfo.GetSummary()
		pods := s.K8sInfo.GetAbnormalPods()
		mainLines := make([]string, 0, len(pods))
		for _, p := range pods {
			mainLines = append(mainLines, fmt.Sprintf("- %s/%s (状态: %s, 重启: %d)", p.Namespace, p.Name, p.Status, p.Restarts))
		}
		if len(mainLines) == 0 {
			rc.AbnormalPods = "无"
		} else {
			rc.AbnormalPods = strings.Join(mainLines, "\n")
		}
	}
	if rc.ResourceSummary == "" {
		rc.ResourceSummary = "未获取"
	}

	steps := s.GetRecentSteps(3)
	if len(steps) == 0 {
		rc.RecentSteps = "无"
	} else {
		lines := make([]string, 0, len(steps))
		for i, step := range steps {
			obs := step.Observation
			if len(obs) > 800 {
				obs = obs[:800] + "..."
			}
			lines = append(lines, fmt.Sprintf("步骤 %d:\n  思考: %s\n  决策: %s\n  观察: %s", i+1, step.Thought, step.Decision, obs))
		}
		rc.RecentSteps = strings.Join(lines, "\n")
	}

	execs := s.GetCommandExecutions()
	if len(execs) > 0 {
		lines := []string{"## 已执行工具摘要", "| # | 命令 | 结果 |", "|---|------|------|"}
		for i, e := range execs {
			status := "✓"
			if !e.Success {
				status = "✗"
			}
			cmd := e.Command
			if len(cmd) > 60 {
				cmd = cmd[:60] + "..."
			}
			lines = append(lines, fmt.Sprintf("| %d | %s | %s |", i+1, cmd, status))
		}
		rc.ToolSummary = strings.Join(lines, "\n")
	}

	if rc.AbnormalPods == "" {
		rc.AbnormalPods = "无"
	}

	return rc
}

// fallbackDecision 降级决策处理（仅在 LLM 失败时使用）
func (n *DecisionNode) fallbackDecision(s *state.State) *DecisionOutput {
	// 简单保底：如果还有迭代次数且有异常 Pod，让 LLM 重试
	if s.K8sInfo != nil && s.GetIterationCount() < s.GetMaxIterations()-1 {
		abnormalPods := s.K8sInfo.GetAbnormalPods()
		if len(abnormalPods) > 0 {
			logger.Info("DecisionNode: fallback - abnormal pods exist, will retry LLM")
			// 返回空的 execute_plan，让下一轮重新调用 LLM
			return &DecisionOutput{
				Decision:  "execute_plan",
				Thought:   "LLM 调用失败，等待下一轮重试",
				ToolCalls: []state.ToolCall{},
			}
		}
	}

	// 否则直接生成报告
	logger.Info("DecisionNode: fallback to report")
	return &DecisionOutput{
		Decision:  "report",
		Thought:   "LLM 调用失败，基于已有信息生成报告",
		ToolCalls: []state.ToolCall{},
	}
}
