package diagnosis

import (
	"context"
	"fmt"
	"time"

	"github.com/cloudwego/eino/schema"

	"github.com/AceDarkknight/k8s-analyzer-agent/internal/llm"
	"github.com/AceDarkknight/k8s-analyzer-agent/internal/logger"
	"github.com/AceDarkknight/k8s-analyzer-agent/internal/state"
	"github.com/AceDarkknight/k8s-analyzer-agent/internal/store"
)

// ReportNode 报告节点
type ReportNode struct {
	router *llm.LLMRouter
	store  store.FindingStore
}

// NewReportNode 创建新的报告节点
func NewReportNode(router *llm.LLMRouter, store store.FindingStore) *ReportNode {
	return &ReportNode{
		router: router,
		store:  store,
	}
}

// Execute 执行报告生成
func (n *ReportNode) Execute(ctx context.Context, s *state.State) (*state.State, error) {
	logger.Info("ReportNode: starting report generation")

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

	response, err := n.router.GenerateWithPower(ctx, messages)
	if err != nil {
		logger.Error("ReportNode: LLM generation failed", logger.Err(err))
		n.generateFallbackReport(s)
		return s, nil
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
