package diagnosis

import (
	"context"
	"strings"
	"time"

	"github.com/AceDarkknight/k8s-analyzer-agent/internal/agent/safety"
	"github.com/AceDarkknight/k8s-analyzer-agent/internal/client/gateway"
	"github.com/AceDarkknight/k8s-analyzer-agent/internal/logger"
	"github.com/AceDarkknight/k8s-analyzer-agent/internal/state"
	"github.com/AceDarkknight/k8s-analyzer-agent/internal/summarizer"
	"go.uber.org/zap"
)

// VerifyNode 建议验证节点
// 触发条件：初步报告生成后，存在 Executable=true 且 Verified=false 的建议命令
type VerifyNode struct {
	gateway    *gateway.GatewayClient
	safety     *safety.SafetyAgent
	summarizer *summarizer.OutputSummarizer
	maxVerify  int  // 最多验证的建议条数，默认 3
	fullRegen  bool // 有新信息时是否触发终版 LLM 重新生成
}

// NewVerifyNode 创建新的验证节点
func NewVerifyNode(
	gw *gateway.GatewayClient,
	sa *safety.SafetyAgent,
	sum *summarizer.OutputSummarizer,
	maxVerify int,
	fullRegen bool,
) *VerifyNode {
	if maxVerify <= 0 {
		maxVerify = 3
	}
	return &VerifyNode{
		gateway:    gw,
		safety:     sa,
		summarizer: sum,
		maxVerify:  maxVerify,
		fullRegen:  fullRegen,
	}
}

// Execute 执行验证
func (n *VerifyNode) Execute(ctx context.Context, s *state.State) (*state.State, error) {
	logger.Info("VerifyNode: starting verification",
		logger.Int("max_verify", n.maxVerify),
		zap.Bool("full_regen", n.fullRegen))

	if s.AnalysisResult == nil {
		logger.Warn("VerifyNode: no analysis result, skipping")
		s.VerifyPhase = true
		s.NeedsFullRegeneration = false
		return s, nil
	}

	// 收集需要验证的建议
	var toVerify []*state.Recommendation
	for i := range s.AnalysisResult.Recommendations {
		rec := &s.AnalysisResult.Recommendations[i]
		if rec.Executable && !rec.Verified {
			toVerify = append(toVerify, rec)
			if len(toVerify) >= n.maxVerify {
				break
			}
		}
	}

	if len(toVerify) == 0 {
		logger.Info("VerifyNode: no executable recommendations to verify")
		s.VerifyPhase = true
		s.NeedsFullRegeneration = false
		return s, nil
	}

	logger.Info("VerifyNode: found recommendations to verify",
		logger.Int("count", len(toVerify)))

	// 执行验证并收集输出
	var verifyOutputs []string

	for _, rec := range toVerify {
		output := n.verifyRecommendation(ctx, s, rec)
		if output != "" {
			verifyOutputs = append(verifyOutputs, output)
		}
	}

	// 判断是否需要完整重新生成
	needsRegen := n.fullRegen && needsFullRegeneration(s.AnalysisResult, verifyOutputs)

	// 设置状态
	s.VerifyPhase = true
	s.NeedsFullRegeneration = needsRegen

	logger.Info("VerifyNode: verification completed",
		zap.Bool("needs_full_regeneration", needsRegen),
		logger.Int("verified_count", len(verifyOutputs)))

	return s, nil
}

// verifyRecommendation 验证单条建议
// 返回验证输出摘要（空字符串表示未执行或执行失败）
func (n *VerifyNode) verifyRecommendation(ctx context.Context, s *state.State, rec *state.Recommendation) string {
	command := rec.Command
	if command == "" {
		logger.Warn("VerifyNode: empty command, skipping",
			logger.String("action", rec.Action))
		return ""
	}

	logger.Info("VerifyNode: verifying recommendation",
		logger.String("command", command),
		logger.String("action", rec.Action))

	// 前置过滤检查
	if shouldSkipCommand(command) {
		logger.Warn("VerifyNode: command filtered, skipping",
			logger.String("command", command))
		return ""
	}

	// 尝试 Gateway 路由
	if req, ok := parseCommandToGatewayRequest(command); ok {
		output := n.executeViaGateway(ctx, s, command, req)
		if output != "" {
			rec.VerifyResult = output
			rec.Verified = true
			return output
		}
		return ""
	}

	// 尝试 SafetyAgent 路由（纯 Shell 命令）
	if isPureShellCommand(command) {
		output := n.executeViaSafetyAgent(ctx, s, command)
		if output != "" {
			rec.VerifyResult = output
			rec.Verified = true
			return output
		}
		return ""
	}

	// 无法归类，跳过
	logger.Warn("VerifyNode: command cannot be categorized, skipping",
		logger.String("command", command))
	return ""
}

// shouldSkipCommand 检查命令是否应该被跳过
func shouldSkipCommand(command string) bool {
	lowerCmd := strings.ToLower(command)

	// 检查管道到 sh/bash
	if strings.Contains(lowerCmd, "|") && (strings.Contains(lowerCmd, " sh") || strings.Contains(lowerCmd, " bash")) {
		return true
	}

	// 检查命令替换 $() 或反引号
	if strings.Contains(command, "$(") || strings.Contains(command, "`") {
		return true
	}

	// 如果不是 kubectl 命令，就不需要进一步检查 kubectl 相关过滤
	if !strings.Contains(lowerCmd, "kubectl") {
		return false
	}

	// 诶词解析 kubectl 命令，找到动词
	fields := strings.Fields(lowerCmd)
	blacklistVerbs := map[string]bool{
		"exec":     true,
		"edit":     true,
		"patch":    true,
		"apply":    true,
		"delete":   true,
		"create":   true,
		"replace":  true,
		"run":      true,
		"expose":   true,
		"label":    true,
		"annotate": true,
		"scale":    true,
		"rollout":  true,
		"set":      true,
		"taint":    true,
		"cordon":   true,
		"uncordon": true,
		"drain":    true,
	}

	for _, field := range fields {
		if blacklistVerbs[field] {
			return true
		}
	}

	return false
}

// isPureShellCommand 检查是否为纯 Shell 命令（不含 kubectl）
func isPureShellCommand(command string) bool {
	lowerCmd := strings.ToLower(strings.TrimSpace(command))

	// 禁止包含重定向的命令走只读通道
	if strings.ContainsAny(lowerCmd, "><") {
		return false
	}

	// 如果包含 kubectl，不是纯 Shell 命令
	if strings.Contains(lowerCmd, "kubectl") {
		return false
	}

	// 检查是否为已知的只读 Shell 命令
	readOnlyCommands := []string{"df", "du", "cat", "grep", "free", "ps", "netstat", "ls", "pwd", "head", "tail", "wc"}
	for _, cmd := range readOnlyCommands {
		if strings.HasPrefix(lowerCmd, cmd+" ") || lowerCmd == cmd {
			return true
		}
	}

	return false
}

// executeViaGateway 通过 Gateway 执行命令
func (n *VerifyNode) executeViaGateway(ctx context.Context, s *state.State, command string, req *gateway.KubectlRequest) string {
	if n.gateway == nil {
		logger.Warn("VerifyNode: gateway client is nil, skipping execution",
			logger.String("command", command))
		return ""
	}

	logger.Info("VerifyNode: executing via Gateway",
		logger.String("verb", req.Verb),
		logger.String("resource", req.Resource),
		logger.String("namespace", req.Namespace),
		logger.String("name", req.Name))

	resp, err := n.gateway.Execute(ctx, req)
	if err != nil {
		logger.Error("VerifyNode: gateway execution failed",
			logger.String("command", command),
			logger.Err(err))
		return ""
	}

	// 提取输出
	output := resp.Stdout
	if output == "" {
		output = resp.Stderr
	}

	// 摘要输出
	summary := n.summarizer.Summarize(output)

	// 记录命令执行
	exec := state.CommandExecution{
		Command:   command,
		Success:   resp.Status == "success",
		Output:    summary,
		Timestamp: time.Now(),
	}
	s.AddCommandExecution(exec)

	logger.Info("VerifyNode: gateway execution completed",
		logger.String("status", resp.Status),
		logger.Int("exit_code", resp.ExitCode))

	return summary
}

// executeViaSafetyAgent 通过 SafetyAgent 执行命令
func (n *VerifyNode) executeViaSafetyAgent(ctx context.Context, s *state.State, command string) string {
	if n.safety == nil {
		logger.Warn("VerifyNode: safety agent is nil, skipping execution",
			logger.String("command", command))
		return ""
	}

	logger.Info("VerifyNode: executing via SafetyAgent",
		logger.String("command", command))

	req := &safety.CommandRequest{
		Command:   command,
		Reason:    "VerifyNode 自动验证建议命令",
		Source:    "VerifyNode",
		Iteration: 0,
	}

	result, err := n.safety.ExecuteSafeCommand(ctx, req)
	if err != nil {
		logger.Error("VerifyNode: safety agent execution failed",
			logger.String("command", command),
			logger.Err(err))
		return ""
	}

	// 检查是否被安全审计拒绝
	if !result.AuditInfo.Allowed {
		logger.Warn("VerifyNode: command rejected by safety audit",
			logger.String("command", command),
			logger.String("reason", result.AuditInfo.Reason))
		return ""
	}

	// 提取输出
	output := result.Stdout
	if output == "" {
		output = result.Stderr
	}

	// 摘要输出
	summary := n.summarizer.Summarize(output)

	// 记录命令执行
	exec := state.CommandExecution{
		Command:   command,
		Success:   result.ExitCode == 0,
		Output:    summary,
		Timestamp: time.Now(),
	}
	s.AddCommandExecution(exec)

	logger.Info("VerifyNode: safety agent execution completed",
		logger.String("command", command),
		logger.Int("exit_code", result.ExitCode))

	return summary
}

// parseCommandToGatewayRequest 将 kubectl 命令文本解析为 KubectlRequest
// 支持格式: kubectl [-n namespace] get/describe/logs <resource> [name] [-o yaml/json] [--tail N]
// 返回 (*gateway.KubectlRequest, bool)，bool 表示是否成功解析
func parseCommandToGatewayRequest(command string) (*gateway.KubectlRequest, bool) {
	// 去除首尾空格
	command = strings.TrimSpace(command)
	if command == "" {
		return nil, false
	}

	// 分词
	fields := strings.Fields(command)
	if len(fields) < 2 {
		return nil, false
	}

	// 检查是否为 kubectl 命令
	if strings.ToLower(fields[0]) != "kubectl" {
		return nil, false
	}

	// 检查是否为支持的动词
	supportedVerbs := map[string]bool{"get": true, "describe": true, "logs": true}

	req := &gateway.KubectlRequest{
		Mode:   "structured",
		Output: "json",
	}

	i := 1
	// 解析 -n namespace
	for i < len(fields) {
		if fields[i] == "-n" || fields[i] == "--namespace" {
			if i+1 < len(fields) {
				req.Namespace = fields[i+1]
				i += 2
				continue
			}
		}
		break
	}

	// 检查动词
	if i >= len(fields) {
		return nil, false
	}
	verb := strings.ToLower(fields[i])
	if !supportedVerbs[verb] {
		return nil, false
	}
	req.Verb = verb
	i++

	// 解析资源类型和名称
	if i >= len(fields) {
		return nil, false
	}

	// 特殊处理 logs 命令：kubectl logs <pod_name>
	// logs 命令没有明确的资源类型参数，直接跟随的是 pod 名称
	if verb == "logs" {
		req.Resource = "pod"
		if req.Options == nil {
			req.Options = &gateway.KubectlOptions{}
		}

		// 扫描剩余 fields，先处理 options，非 option 的第一个 token 作为 Pod 名
		for i < len(fields) {
			tok := fields[i]
			if strings.HasPrefix(tok, "-") {
				switch tok {
				case "-c", "--container":
					if i+1 < len(fields) {
						req.Options.Container = fields[i+1]
						i += 2
						continue
					}
				case "--tail":
					if i+1 < len(fields) {
						// 尝试解析 tail lines，这里简化处理
						req.Options.TailLines = 100 // 默认值
						i += 2
						continue
					}
				case "-n", "--namespace":
					if i+1 < len(fields) {
						req.Namespace = fields[i+1]
						i += 2
						continue
					}
				case "-o", "--output":
					if i+1 < len(fields) {
						req.Output = fields[i+1]
						req.Options.Output = fields[i+1]
						i += 2
						continue
					}
				}
				i++
			} else {
				if req.Name == "" {
					req.Name = tok
				}
				i++
			}
		}

		if req.Name == "" {
			return nil, false
		}
	} else {
		// 资源类型
		req.Resource = fields[i]
		i++

		// 资源名称（可选）
		if i < len(fields) && !strings.HasPrefix(fields[i], "-") {
			req.Name = fields[i]
			i++
		}
	}

	// 解析剩余选项
	if req.Options == nil {
		req.Options = &gateway.KubectlOptions{}
	}
	for i < len(fields) {
		switch fields[i] {
		case "-o", "--output":
			if i+1 < len(fields) {
				req.Output = fields[i+1]
				req.Options.Output = fields[i+1]
				i += 2
				continue
			}
		case "--tail":
			if i+1 < len(fields) {
				// 尝试解析 tail lines，这里简化处理
				req.Options.TailLines = 100 // 默认值
				i += 2
				continue
			}
		case "-c", "--container":
			if i+1 < len(fields) {
				req.Options.Container = fields[i+1]
				i += 2
				continue
			}
		case "-n", "--namespace":
			if i+1 < len(fields) {
				req.Namespace = fields[i+1]
				i += 2
				continue
			}
		}
		i++
	}

	// 标准化资源名称（复数化）
	req.Resource = normalizeResourceName(req.Resource)

	return req, true
}

// normalizeResourceName 标准化资源名称
func normalizeResourceName(resource string) string {
	lowerRes := strings.ToLower(resource)

	// 资源名称映射表
	singularToPlural := map[string]string{
		"pod":         "pods",
		"deployment":  "deployments",
		"service":     "services",
		"configmap":   "configmaps",
		"secret":      "secrets",
		"node":        "nodes",
		"namespace":   "namespaces",
		"event":       "events",
		"replicaset":  "replicasets",
		"pvc":         "persistentvolumeclaims",
		"pv":          "persistentvolumes",
		"statefulset": "statefulsets",
		"daemonset":   "daemonsets",
		"job":         "jobs",
		"cronjob":     "cronjobs",
		"ingress":     "ingresses",
		"endpoint":    "endpoints",
		"rs":          "replicasets",
	}

	if plural, ok := singularToPlural[lowerRes]; ok {
		return plural
	}

	// 如果已经是复数形式，直接返回
	pluralResources := map[string]bool{
		"pods": true, "deployments": true, "services": true, "configmaps": true,
		"secrets": true, "nodes": true, "namespaces": true, "events": true,
		"replicasets": true, "persistentvolumeclaims": true, "persistentvolumes": true,
		"statefulsets": true, "daemonsets": true, "jobs": true, "cronjobs": true,
		"ingresses": true, "endpoints": true, "rs": true, "pvc": true, "pv": true,
	}

	if pluralResources[lowerRes] {
		return lowerRes
	}

	return lowerRes
}

// needsFullRegeneration 判断是否需要完整重新生成报告
// 纯字符串判断，不调用 LLM
func needsFullRegeneration(initialResult *state.AnalysisResult, verifyOutputs []string) bool {
	// 所有验证输出为空 → 不需要重新生成
	if len(verifyOutputs) == 0 {
		return false
	}

	// 如果没有初步报告，无法判断是否有新信息，默认不重新生成
	if initialResult == nil {
		return false
	}

	// 提取初步报告的关键词
	initialText := extractInitialReportText(initialResult)
	initialKeywords := extractKeywords(initialText)

	// 检查验证输出中是否包含新信息
	for _, output := range verifyOutputs {
		if containsNewInformation(output, initialKeywords) {
			return true
		}
	}

	return false
}

// extractInitialReportText 提取初步报告的文本内容
func extractInitialReportText(result *state.AnalysisResult) string {
	if result == nil {
		return ""
	}

	var parts []string

	// 根因
	if result.RootCause != "" {
		parts = append(parts, result.RootCause)
	}

	// 摘要
	if result.Summary != "" {
		parts = append(parts, result.Summary)
	}

	// 发现的 Message
	for _, finding := range result.Findings {
		if finding.Message != "" {
			parts = append(parts, finding.Message)
		}
	}

	return strings.Join(parts, " ")
}

// extractKeywords 从文本中提取关键词
func extractKeywords(text string) map[string]bool {
	keywords := make(map[string]bool)

	// 转换为小写并分词
	lowerText := strings.ToLower(text)
	// 替换标点符号为空格
	for _, r := range ",.!?;:()[]{}\"'" {
		lowerText = strings.ReplaceAll(lowerText, string(r), " ")
	}

	words := strings.Fields(lowerText)

	// 过滤停用词和短词
	stopWords := map[string]bool{
		"the": true, "a": true, "an": true, "is": true, "are": true, "was": true, "were": true,
		"be": true, "been": true, "being": true, "have": true, "has": true, "had": true,
		"do": true, "does": true, "did": true, "will": true, "would": true, "could": true,
		"should": true, "may": true, "might": true, "must": true, "can": true, "need": true,
		"to": true, "of": true, "in": true, "for": true, "on": true, "with": true, "at": true,
		"by": true, "from": true, "as": true, "into": true, "through": true, "during": true,
		"before": true, "after": true, "above": true, "below": true, "between": true, "under": true,
		"and": true, "or": true, "but": true, "so": true, "if": true, "then": true, "than": true,
		"this": true, "that": true, "these": true, "those": true, "it": true, "its": true,
	}

	for _, word := range words {
		if len(word) >= 3 && !stopWords[word] {
			keywords[word] = true
		}
	}

	return keywords
}

// containsNewInformation 检查输出中是否包含新信息
func containsNewInformation(output string, initialKeywords map[string]bool) bool {
	// 将输出按行分割
	lines := strings.Split(output, "\n")

	newInfoLines := 0
	totalMeaningfulLines := 0

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		totalMeaningfulLines++

		// 提取当前行的关键词
		lineKeywords := extractKeywords(trimmed)

		// 检查是否有新关键词
		hasNewInfo := false
		for keyword := range lineKeywords {
			if !initialKeywords[keyword] {
				// 这是一个新关键词
				hasNewInfo = true
				break
			}
		}

		if hasNewInfo {
			newInfoLines++
		}
	}

	// 如果超过 30% 的行包含新信息，认为需要重新生成
	if totalMeaningfulLines > 0 && float64(newInfoLines)/float64(totalMeaningfulLines) > 0.3 {
		return true
	}

	// 如果有至少 3 行包含新信息，也认为需要重新生成
	if newInfoLines >= 3 {
		return true
	}

	return false
}
