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
	"github.com/AceDarkknight/k8s-analyzer-agent/internal/state"
	"github.com/AceDarkknight/k8s-analyzer-agent/internal/store"
	trc "github.com/AceDarkknight/k8s-analyzer-agent/internal/trace"
)

// ReportNode 报告节点
type ReportNode struct {
	router    *llm.LLMRouter
	store     store.FindingStore
	recorder  *trc.TaskRecorder
	promptReg *promptregistry.PromptRegistry
}

// NewReportNode 创建新的报告节点
func NewReportNode(router *llm.LLMRouter, store store.FindingStore, recorder *trc.TaskRecorder, promptReg *promptregistry.PromptRegistry) *ReportNode {
	return &ReportNode{
		router:    router,
		store:     store,
		recorder:  recorder,
		promptReg: promptReg,
	}
}

// Execute 执行报告生成
func (n *ReportNode) Execute(ctx context.Context, s *state.State) (*state.State, error) {
	logger.Info("ReportNode: starting report generation")

	// 如果是终版报告（VerifyPhase=true），先做验证结果匹配
	if s.VerifyPhase {
		matchVerifyResults(s)
	}

	// 1. 构建 prompt（优先 Registry，失败回退 legacy）
	prompt := n.buildPrompt(ctx, s)
	if prompt == "" {
		logger.Warn("ReportNode: empty prompt generated")
		n.generateFallbackReport(s)
		return s, nil
	}

	// 2. 调用 LLM (使用 Power 模型)
	messages := []*schema.Message{
		schema.UserMessage(prompt),
	}

	llmStart := time.Now()
	response, usage, err := n.router.GenerateWithPower(ctx, messages)
	llmDuration := time.Since(llmStart)
	if err != nil {
		logger.Error("ReportNode: LLM generation failed", logger.Err(err))
		n.generateFallbackReport(s)
		return s, nil
	}
	if usage != nil {
		s.AccumulateTokenUsage(usage)
		if n.recorder != nil {
			n.recorder.Emit(trc.LLMCallEvent{Call: trc.LLMCallRecord{
				ModelType:        "power",
				ModelName:        n.router.PowerModelName(),
				Source:           "report",
				PromptTokens:     usage.PromptTokens,
				CompletionTokens: usage.CompletionTokens,
				TotalTokens:      usage.TotalTokens,
				DurationMs:       llmDuration.Milliseconds(),
				Timestamp:        time.Now().Format(time.RFC3339),
				Input:            prompt,
				Output:           response.Content,
				CacheHit:         usage.PromptTokenDetails.CachedTokens > 0,
				CachedTokens:     usage.PromptTokenDetails.CachedTokens,
			}})
		}
	}

	if response == nil || response.Content == "" {
		logger.Warn("ReportNode: empty LLM response")
		n.generateFallbackReport(s)
		return s, nil
	}

	// 3. 解析响应
	result, err := llm.ParseAnalysisResponse(response.Content)
	if err != nil {
		logger.Error("ReportNode: failed to parse analysis response", logger.Err(err))
		n.generateFallbackReport(s)
		return s, nil
	}

	// 4. 设置状态
	if s.IterationCount >= s.MaxIterations {
		result.Status = "partial"
	} else {
		result.Status = "completed"
	}

	s.SetAnalysisResult(result)

	// 5. 如果 store 不为 nil，对 Findings 做去重
	if n.store != nil && len(result.Findings) > 0 {
		n.deduplicateFindings(ctx, s, result)
	}

	logger.Info("ReportNode: report generation completed",
		logger.String("status", result.Status),
		logger.Int("findings", len(result.Findings)))

	return s, nil
}

func (n *ReportNode) buildPrompt(ctx context.Context, s *state.State) string {
	if n.promptReg != nil {
		prompt, err := n.promptReg.BuildReport(ctx, "report", promptregistry.VersionDefault, n.buildRenderContext(s))
		if err == nil && strings.TrimSpace(prompt) != "" {
			return prompt
		}
		if err != nil {
			logger.Warn("ReportNode: prompt registry build failed, fallback to legacy", logger.Err(err))
		}
	}
	return llm.BuildSynthesizePrompt(s)
}

func (n *ReportNode) buildRenderContext(s *state.State) *promptregistry.ReportPromptContext {
	status := "completed"
	if s.LastError != nil {
		status = "partial"
	} else if s.GetIterationCount() >= s.GetMaxIterations() {
		status = "max_iterations_reached"
	}

	resourceSummary := "未获取"
	if s.K8sInfo != nil {
		resourceSummary = s.K8sInfo.GetSummary()
	}

	rc := &promptregistry.ReportPromptContext{
		UserQuery:       s.UserInput,
		Status:          status,
		ResourceSummary: resourceSummary,
		IsVerifyPhase:   s.VerifyPhase,
	}

	if s.AnalysisResult != nil && len(s.AnalysisResult.Findings) > 0 {
		findings := make([]string, 0, len(s.AnalysisResult.Findings))
		for _, f := range s.AnalysisResult.Findings {
			findings = append(findings, fmt.Sprintf("- [%s] %s: %s", f.Severity, f.Resource, f.Message))
		}
		rc.Findings = strings.Join(findings, "\n")
	} else {
		rc.Findings = "无"
	}

	execs := s.GetCommandExecutions()
	if len(execs) > 0 {
		cmds := make([]string, 0, len(execs))
		for _, e := range execs {
			st := "成功"
			if !e.Success {
				st = "失败"
			}
			out := e.Output
			if len(out) > 4000 {
				out = out[:4000] + "...[截断]"
			}
			cmds = append(cmds, fmt.Sprintf("- %s (%s)\n  输出摘要: %s", e.Command, st, out))
		}
		rc.CommandSummary = strings.Join(cmds, "\n")
	} else {
		rc.CommandSummary = "无"
	}

	blocked := s.GetBlockedCommands()
	if len(blocked) > 0 {
		lines := []string{"## 被安全审计拒绝的命令"}
		for _, bc := range blocked {
			lines = append(lines, fmt.Sprintf("- 命令: %s\n  原因: %s\n  建议: %s", bc.Command, bc.Reason, bc.Advice))
		}
		rc.BlockedCommands = strings.Join(lines, "\n")
	}

	if len(s.ReasoningHistory) > 0 {
		lines := []string{"## 完整推理过程"}
		for i, step := range s.ReasoningHistory {
			thought := step.Thought
			if len(thought) > 200 {
				thought = thought[:200] + "..."
			}
			obs := step.Observation
			if len(obs) > 300 {
				obs = obs[:300] + "..."
			}
			lines = append(lines, fmt.Sprintf("轮次%d [%s]:\n思考: %s\n工具结果: %s", i+1, step.Decision, thought, obs))
		}
		rc.ReasoningChain = strings.Join(lines, "\n")
	}

	return rc
}

// deduplicateFindings 对 Findings 进行去重
func (n *ReportNode) deduplicateFindings(ctx context.Context, s *state.State, result *state.AnalysisResult) {
	var uniqueFindings []state.Finding

	for _, finding := range result.Findings {
		// 生成 Finding 的唯一 key
		key := fmt.Sprintf("%s:%s:%s", finding.Resource, finding.Severity, finding.Message)

		exists, err := n.store.HasFinding(ctx, key)
		if err != nil {
			logger.Error("ReportNode: failed to check finding existence",
				logger.String("key", key),
				logger.Err(err))
			// 出错时保留该 finding
			uniqueFindings = append(uniqueFindings, finding)
			continue
		}

		if exists {
			logger.Info("ReportNode: skipping duplicate finding",
				logger.String("resource", finding.Resource))
			continue
		}

		// 保存 finding
		if err := n.store.SaveFinding(ctx, key, 24*time.Hour); err != nil {
			logger.Error("ReportNode: failed to save finding",
				logger.String("key", key),
				logger.Err(err))
		}

		uniqueFindings = append(uniqueFindings, finding)
	}

	result.Findings = uniqueFindings
}

// generateFallbackReport 生成降级报告
func (n *ReportNode) generateFallbackReport(s *state.State) {
	logger.Info("ReportNode: generating fallback report")

	result := &state.AnalysisResult{
		Summary:   "由于 LLM 服务异常，生成基础诊断报告",
		Severity:  "warning",
		RootCause: "无法确定具体根因，建议手动检查集群状态",
		Status:    "partial",
	}

	// 基于 K8sInfo 生成基础 Findings
	if s.K8sInfo != nil {
		abnormalPods := s.K8sInfo.GetAbnormalPods()
		for _, pod := range abnormalPods {
			result.Findings = append(result.Findings, state.Finding{
				Severity:  "warning",
				Resource:  fmt.Sprintf("%s/%s", pod.Namespace, pod.Name),
				Message:   fmt.Sprintf("Pod 状态异常: %s", pod.Status),
				Evidence:  fmt.Sprintf("重启次数: %d", pod.Restarts),
				Timestamp: time.Now(),
			})
		}
	}

	// 添加建议
	if len(result.Findings) > 0 {
		result.Recommendations = []state.Recommendation{
			{
				Priority: "high",
				Action:   "检查异常 Pod 的日志和事件",
				Command:  "kubectl describe pod <pod-name> -n <namespace>",
				Risk:     "低风险",
			},
		}
	}

	result.Limitations = "LLM 服务异常，报告内容可能不完整"

	s.SetAnalysisResult(result)
}

// matchVerifyResults 将验证阶段执行结果与建议进行关联（纯字符串匹配，零 LLM）
func matchVerifyResults(s *state.State) {
	if s.AnalysisResult == nil {
		return
	}
	verifyExecs := s.GetVerifyPhaseExecutions()
	if len(verifyExecs) == 0 {
		return
	}

	for i := range s.AnalysisResult.Recommendations {
		rec := &s.AnalysisResult.Recommendations[i]
		if rec.Verified {
			continue
		}
		for _, exec := range verifyExecs {
			if !exec.Success {
				continue
			}
			if commandCoversRecommendation(exec.Command, rec.Action, rec.Command) {
				rec.Verified = true
				output := exec.Output
				if len(output) > 200 {
					output = output[:200] + "..."
				}
				rec.VerifyResult = output

				// 同时更新对应 Finding 的 Evidence
				updateFindingEvidence(s, rec, output)
				break
			}
		}
	}
}

// updateFindingEvidence 根据 Recommendation 更新对应 Finding 的 Evidence
func updateFindingEvidence(s *state.State, rec *state.Recommendation, verifyOutput string) {
	for fi := range s.AnalysisResult.Findings {
		finding := &s.AnalysisResult.Findings[fi]
		// 如果 Finding 的资源名与 Recommendation 的操作描述相关，则更新 Evidence
		if isFindingRelatedToRec(finding, rec) {
			if finding.Evidence == "" {
				finding.Evidence = "验证输出: " + verifyOutput
			} else {
				finding.Evidence += "\n验证输出: " + verifyOutput
			}
		}
	}
}

// isFindingRelatedToRec 判断 Finding 是否与 Recommendation 相关
func isFindingRelatedToRec(finding *state.Finding, rec *state.Recommendation) bool {
	// 提取 Recommendation 中的关键词
	recKeywords := extractEntityKeywords(rec.Action + " " + rec.Command)
	// 检查 Finding 的资源名是否包含这些关键词
	for _, kw := range recKeywords {
		if len(kw) > 3 && strings.Contains(strings.ToLower(finding.Resource), strings.ToLower(kw)) {
			return true
		}
	}
	return false
}

// commandCoversRecommendation 判断执行命令是否覆盖该建议
// 提取建议文本中的关键实体词（命名空间、Pod名、资源类型），至少匹配 2 个
func commandCoversRecommendation(execCmd, recAction, recCommand string) bool {
	keywords := extractEntityKeywords(recAction + " " + recCommand)
	matchCount := 0
	execLower := strings.ToLower(execCmd)
	for _, kw := range keywords {
		if len(kw) > 3 && strings.Contains(execLower, strings.ToLower(kw)) {
			matchCount++
		}
	}
	return matchCount >= 2
}

// extractEntityKeywords 从文本中提取有意义的实体词
func extractEntityKeywords(text string) []string {
	// 忽略常见中文停用词和短词
	stopWords := map[string]bool{
		"确认": true, "检查": true, "查看": true, "修复": true, "并": true,
		"配置": true, "命令": true, "建议": true, "操作": true,
	}
	var keywords []string
	for _, w := range strings.FieldsFunc(text, func(r rune) bool {
		return r == ' ' || r == '/' || r == '-' || r == ',' || r == '：' || r == ':'
	}) {
		w = strings.Trim(w, ".,;:()[]{}\"'（）")
		if len(w) >= 3 && !stopWords[w] {
			keywords = append(keywords, w)
		}
	}
	return keywords
}
