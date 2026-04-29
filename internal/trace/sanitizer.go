package trace

import (
	"regexp"
	"strings"

	"github.com/AceDarkknight/k8s-analyzer-agent/internal/state"
)

var sensitivePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(authorization\s*[:=]\s*)(bearer\s+)?[^\s,;]+`),
	regexp.MustCompile(`(?i)(api[_-]?key\s*[:=]\s*)[^\s,;]+`),
	regexp.MustCompile(`(?i)(token\s*[:=]\s*)[^\s,;]+`),
	regexp.MustCompile(`(?i)(password\s*[:=]\s*)[^\s,;]+`),
	regexp.MustCompile(`(?i)(secret\s*[:=]\s*)[^\s,;]+`),
}

func SanitizeTaskTrace(src *TaskTrace) *TaskTrace {
	if src == nil {
		return nil
	}
	cloned := *src
	cloned.UserInput = sanitizeString(src.UserInput)
	cloned.Error = sanitizeString(src.Error)
	if src.AnalysisResult != nil {
		result := *src.AnalysisResult
		result.Summary = sanitizeString(result.Summary)
		result.RootCause = sanitizeString(result.RootCause)
		result.Limitations = sanitizeString(result.Limitations)
		if len(result.Findings) > 0 {
			findings := make([]state.Finding, len(result.Findings))
			for i, f := range result.Findings {
				findings[i] = f
				findings[i].Message = sanitizeString(f.Message)
				findings[i].Evidence = sanitizeString(f.Evidence)
			}
			result.Findings = findings
		}
		if len(result.Recommendations) > 0 {
			recs := make([]state.Recommendation, len(result.Recommendations))
			for i, r := range result.Recommendations {
				recs[i] = r
				recs[i].Action = sanitizeString(r.Action)
				recs[i].Command = sanitizeString(r.Command)
				recs[i].Risk = sanitizeString(r.Risk)
				recs[i].VerifyResult = sanitizeString(r.VerifyResult)
			}
			result.Recommendations = recs
		}
		cloned.AnalysisResult = &result
	}
	if len(src.ToolExecutions) > 0 {
		cloned.ToolExecutions = make([]TraceToolExecution, len(src.ToolExecutions))
		for i, exec := range src.ToolExecutions {
			cloned.ToolExecutions[i] = exec
			cloned.ToolExecutions[i].Output = sanitizeString(exec.Output)
			cloned.ToolExecutions[i].Command = sanitizeString(exec.Command)
			cloned.ToolExecutions[i].Args = sanitizeMap(exec.Args)
		}
	}
	if len(src.ReasoningHistory) > 0 {
		cloned.ReasoningHistory = make([]TraceReasoningStep, len(src.ReasoningHistory))
		for i, step := range src.ReasoningHistory {
			cloned.ReasoningHistory[i] = step
			cloned.ReasoningHistory[i].Thought = sanitizeString(step.Thought)
			cloned.ReasoningHistory[i].Observation = sanitizeString(step.Observation)
			cloned.ReasoningHistory[i].DeepQueryTopic = sanitizeString(step.DeepQueryTopic)
			if len(step.ToolCalls) > 0 {
				tcs := make([]TraceToolCallDetail, len(step.ToolCalls))
				for j, tc := range step.ToolCalls {
					tcs[j] = tc
					tcs[j].Output = sanitizeString(tc.Output)
					tcs[j].Args = sanitizeMap(tc.Args)
				}
				cloned.ReasoningHistory[i].ToolCalls = tcs
			}
		}
	}
	if len(src.BlockedCommands) > 0 {
		cloned.BlockedCommands = make([]state.BlockedCommand, len(src.BlockedCommands))
		for i, cmd := range src.BlockedCommands {
			cloned.BlockedCommands[i] = cmd
			cloned.BlockedCommands[i].Command = sanitizeString(cmd.Command)
			cloned.BlockedCommands[i].Reason = sanitizeString(cmd.Reason)
			cloned.BlockedCommands[i].Advice = sanitizeString(cmd.Advice)
		}
	}
	return &cloned
}

func sanitizeMap(src map[string]interface{}) map[string]interface{} {
	if len(src) == 0 {
		return src
	}
	out := make(map[string]interface{}, len(src))
	for k, v := range src {
		switch val := v.(type) {
		case string:
			if isSensitiveKey(k) {
				out[k] = "***"
			} else {
				out[k] = sanitizeString(val)
			}
		case map[string]interface{}:
			out[k] = sanitizeMap(val)
		default:
			out[k] = v
		}
	}
	return out
}

func isSensitiveKey(key string) bool {
	key = strings.ToLower(key)
	return strings.Contains(key, "token") || strings.Contains(key, "password") || strings.Contains(key, "secret") || strings.Contains(key, "auth") || strings.Contains(key, "key")
}

func sanitizeString(input string) string {
	if input == "" {
		return input
	}
	out := input
	for _, pattern := range sensitivePatterns {
		out = pattern.ReplaceAllString(out, `${1}***`)
	}
	return out
}
