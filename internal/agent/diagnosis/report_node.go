package diagnosis

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cloudwego/eino/schema"

	"github.com/AceDarkknight/k8s-analyzer-agent/internal/llm"
	"github.com/AceDarkknight/k8s-analyzer-agent/internal/logger"
	"github.com/AceDarkknight/k8s-analyzer-agent/internal/state"
	"github.com/AceDarkknight/k8s-analyzer-agent/internal/store"
	trc "github.com/AceDarkknight/k8s-analyzer-agent/internal/trace"
)

// ReportNode 报告节点
type ReportNode struct {
	router   *llm.LLMRouter
	store    store.FindingStore
	recorder *trc.TaskRecorder
}

// NewReportNode 创建新的报告节点
func NewReportNode(router *llm.LLMRouter, store store.FindingStore, recorder *trc.TaskRecorder) *ReportNode {
	return &ReportNode{
		router:   router,
		store:    store,
		recorder: recorder,
	}
}

// Execute 执行报告生成
func (n *ReportNode) Execute(ctx context.Context, s *state.State) (*state.State, error) {
	logger.Info("ReportNode: starting report generation")

	// 如果是终版报告（VerifyPhase=true），先做验证结果匹配
	if s.VerifyPhase {
		matchVerifyResults(s)
	}

	// 1. 构建 prompt
	prompt := llm.BuildSynthesizePrompt(s)
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
            cached := trc.ExtractCachedTokens(usage)
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
                CachedTokens:     cached,
                CacheHit:         cached > 0,
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
