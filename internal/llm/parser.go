package llm

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/AceDarkknight/k8s-analyzer-agent/internal/state"
)

// DecisionResult 决策节点 LLM 响应
type DecisionResult struct {
	Thought        string           `json:"thought"`
	Decision       string           `json:"decision"`   // execute_plan / deep_query / report
	ToolCalls      []state.ToolCall `json:"tool_calls"` // 兼容旧模式
	Plan           []state.PlanStep `json:"plan"`       // 新模式
	ExecuteSteps   []int            `json:"execute_steps"`
	DeepQueryTopic string           `json:"deep_query_topic"`
}

// AuditResponse LLM 审计响应
type AuditResponse struct {
	SafetyLevel string `json:"safety_level"`
	Reason      string `json:"reason"`
	Advice      string `json:"advice"`
}

// ExtractJSON 从可能包含 Markdown 代码块的字符串中提取 JSON
func ExtractJSON(s string) string {
	if s == "" {
		return ""
	}

	// 尝试匹配 ```json ... ``` 或 ``` ... ``` 中的内容
	codeBlockPattern := regexp.MustCompile("(?s)```(?:json)?\\s*(\\{.*?\\})\\s*```")
	matches := codeBlockPattern.FindStringSubmatch(s)
	if len(matches) > 1 {
		return strings.TrimSpace(matches[1])
	}

	// 如果没有代码块，尝试找到第一个 { 和最后一个 } 之间的内容
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start != -1 && end != -1 && end > start {
		return strings.TrimSpace(s[start : end+1])
	}

	// 返回原始字符串
	return strings.TrimSpace(s)
}

// ParseDecisionResponse 解析决策响应
func ParseDecisionResponse(content string) (*DecisionResult, error) {
	if content == "" {
		return nil, fmt.Errorf("empty content")
	}

	// 提取 JSON
	jsonStr := ExtractJSON(content)
	if jsonStr == "" {
		return nil, fmt.Errorf("no JSON found in content")
	}

	// 解析 JSON
	var result DecisionResult
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		// 尝试降级处理：文本匹配 decision 关键词
		return parseDecisionResponseFallback(content)
	}

	// 验证 decision 值（支持新旧两种模式）
	validDecisions := map[string]bool{
		"continue":     true, // 旧模式兼容
		"execute_plan": true, // 新模式
		"deep_query":   true,
		"report":       true,
	}
	if !validDecisions[result.Decision] {
		// 尝试降级处理
		return parseDecisionResponseFallback(content)
	}

	// 旧模式兼容：如果是 continue，转换为 execute_plan 格式
	if result.Decision == "continue" && len(result.ToolCalls) > 0 {
		result.Decision = "execute_plan"
		result.Plan = []state.PlanStep{
			{Step: 1, Description: "诊断步骤", ToolCalls: result.ToolCalls},
		}
		result.ExecuteSteps = []int{1}
	}

	return &result, nil
}

// parseDecisionResponseFallback 降级解析决策响应
func parseDecisionResponseFallback(content string) (*DecisionResult, error) {
	contentLower := strings.ToLower(content)

	result := &DecisionResult{
		Thought:   content,
		Decision:  "continue",
		ToolCalls: make([]state.ToolCall, 0),
	}

	// 尝试匹配 decision 关键词
	if strings.Contains(contentLower, "decision") {
		if strings.Contains(contentLower, "report") {
			result.Decision = "report"
		} else if strings.Contains(contentLower, "deep_query") || strings.Contains(contentLower, "deep query") {
			result.Decision = "deep_query"
		}
	}

	return result, nil
}

// ParseAnalysisResponse 解析报告响应
func ParseAnalysisResponse(content string) (*state.AnalysisResult, error) {
	if content == "" {
		return nil, fmt.Errorf("empty content")
	}

	// 提取 JSON
	jsonStr := ExtractJSON(content)
	if jsonStr == "" {
		return nil, fmt.Errorf("no JSON found in content")
	}

	// 定义解析用的临时结构
	type findingJSON struct {
		Resource string `json:"resource"`
		Severity string `json:"severity"`
		Message  string `json:"message"`
		Evidence string `json:"evidence"`
	}

	type recommendationJSON struct {
		Priority string `json:"priority"`
		Action   string `json:"action"`
		Command  string `json:"command"`
		Risk     string `json:"risk"`
	}

	type analysisJSON struct {
		Summary         string               `json:"summary"`
		Severity        string               `json:"severity"`
		RootCause       string               `json:"root_cause"`
		Findings        []findingJSON        `json:"findings"`
		Recommendations []recommendationJSON `json:"recommendations"`
		Limitations     string               `json:"limitations"`
	}

	var parsed analysisJSON
	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		return nil, fmt.Errorf("failed to unmarshal analysis response: %w", err)
	}

	// 转换为 state.AnalysisResult
	result := &state.AnalysisResult{
		Summary:     parsed.Summary,
		Severity:    parsed.Severity,
		RootCause:   parsed.RootCause,
		Limitations: parsed.Limitations,
		Status:      "completed",
	}

	// 转换 findings
	for _, f := range parsed.Findings {
		result.Findings = append(result.Findings, state.Finding{
			Resource: f.Resource,
			Severity: f.Severity,
			Message:  f.Message,
			Evidence: f.Evidence,
		})
	}

	// 转换 recommendations
	for _, r := range parsed.Recommendations {
		result.Recommendations = append(result.Recommendations, state.Recommendation{
			Priority: r.Priority,
			Action:   r.Action,
			Command:  r.Command,
			Risk:     r.Risk,
		})
	}

	return result, nil
}

// ParseAuditResponse 解析审计响应
func ParseAuditResponse(content string) (*AuditResponse, error) {
	if content == "" {
		return nil, fmt.Errorf("empty content")
	}

	// 提取 JSON
	jsonStr := ExtractJSON(content)
	if jsonStr == "" {
		return nil, fmt.Errorf("no JSON found in content")
	}

	var result AuditResponse
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal audit response: %w", err)
	}

	// 验证 safety_level 值
	validLevels := map[string]bool{
		"safe":      true,
		"warning":   true,
		"dangerous": true,
	}
	if !validLevels[result.SafetyLevel] {
		return nil, fmt.Errorf("invalid safety_level: %s", result.SafetyLevel)
	}

	return &result, nil
}
