package safety

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/AceDarkknight/k8s-analyzer-agent/internal/logger"
)

// AuditResult LLM 审计结果
type AuditResult struct {
	SafetyLevel string `json:"safety_level"` // safe / warning / dangerous
	Reason      string `json:"reason"`
	Advice      string `json:"advice"`
}

// LLMAuditor LLM 语义审计器
type LLMAuditor struct {
	llm model.ChatModel // 使用 Light 模型
}

// NewLLMAuditor 创建 LLM 审计器
func NewLLMAuditor(llm model.ChatModel) *LLMAuditor {
	return &LLMAuditor{
		llm: llm,
	}
}

// Audit 审计命令安全性
func (a *LLMAuditor) Audit(ctx context.Context, command, reason string) (*AuditResult, error) {
	logger.Info("开始 LLM 安全审计",
		logger.String("command", command),
		logger.String("reason", reason),
	)

	// 构建审计 Prompt
	prompt := buildAuditPrompt(command, reason)

	// 调用 LLM
	result, err := a.callLLM(ctx, prompt)
	if err != nil {
		// 检查是否是 context 取消或超时
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			logger.Warn("LLM 审计因 context 取消/超时",
				logger.String("command", command),
				logger.Err(err),
			)
			return nil, nil
		}
		logger.Error("LLM 审计失败",
			logger.String("command", command),
			logger.Err(err),
		)
		return nil, err
	}

	logger.Info("LLM 审计完成",
		logger.String("command", command),
		logger.String("safety_level", result.SafetyLevel),
	)

	return result, nil
}

// callLLM 调用 LLM 并解析结果
func (a *LLMAuditor) callLLM(ctx context.Context, prompt string) (*AuditResult, error) {
	messages := []*schema.Message{
		{
			Role:    schema.User,
			Content: prompt,
		},
	}

	response, err := a.llm.Generate(ctx, messages)
	if err != nil {
		return nil, fmt.Errorf("LLM generate failed: %w", err)
	}

	// 解析 JSON 响应
	result, err := parseAuditResult(response.Content)
	if err != nil {
		// 重试一次，在 prompt 中附加解析错误信息
		logger.Warn("LLM 响应解析失败，准备重试",
			logger.Err(err),
			logger.String("response", response.Content),
		)

		retryPrompt := fmt.Sprintf("%s\n\n注意：之前的响应解析失败，错误：%s。请确保返回严格的 JSON 格式，不要包含 markdown 代码块或其他内容。", prompt, err.Error())
		messages = []*schema.Message{
			{
				Role:    schema.User,
				Content: retryPrompt,
			},
		}

		response, err = a.llm.Generate(ctx, messages)
		if err != nil {
			return nil, fmt.Errorf("LLM retry generate failed: %w", err)
		}

		result, err = parseAuditResult(response.Content)
		if err != nil {
			return nil, fmt.Errorf("failed to parse audit result after retry: %w", err)
		}
	}

	return result, nil
}

// buildAuditPrompt 构建审计 Prompt
func buildAuditPrompt(command, reason string) string {
	return fmt.Sprintf(`你是一个 Linux 命令安全审计专家。请评估以下命令在 Kubernetes 集群节点上执行的安全性。

## 待审计命令
%s

## 执行原因
%s

## 安全评估标准
### Safe（安全）
只读操作：cat, head, tail, grep, df, du, free, uptime, top, ps, netstat, ss, ip addr/route, ping, dig, nslookup, crictl ps/logs, docker ps/logs, dmesg, journalctl

### Warning（警告）
可控操作：systemctl status, docker inspect, lsof, strace -p

### Dangerous（危险）
修改/删除/停服务/改权限/远程执行：rm -rf, mkfs, dd, shutdown, kill, chmod 777, iptables -F, curl|sh, eval, exec, $()

## 输出格式（严格 JSON，不要包含其他内容）
{"safety_level": "safe/warning/dangerous", "reason": "判断理由", "advice": "如果 dangerous，建议替代命令；否则为空"}`, command, reason)
}

// parseAuditResult 解析审计结果
func parseAuditResult(content string) (*AuditResult, error) {
	// 提取 JSON
	jsonStr := extractJSON(content)

	var result AuditResult
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal audit result: %w", err)
	}

	// 验证 safety_level 是否有效
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

// extractJSON 从可能包含 markdown 的字符串中提取 JSON
func extractJSON(s string) string {
	s = strings.TrimSpace(s)

	// 尝试匹配 ```json\n...\n```
	if strings.HasPrefix(s, "```json") {
		start := strings.Index(s, "\n")
		if start != -1 {
			end := strings.LastIndex(s, "```")
			if end != -1 && end > start {
				return strings.TrimSpace(s[start:end])
			}
		}
	}

	// 尝试匹配 ```\n...\n```
	if strings.HasPrefix(s, "```") {
		start := strings.Index(s, "\n")
		if start != -1 {
			end := strings.LastIndex(s, "```")
			if end != -1 && end > start {
				return strings.TrimSpace(s[start:end])
			}
		}
	}

	// 如果没有代码块，找第一个 { 和最后一个 }
	start := strings.Index(s, "{")
	if start == -1 {
		return s
	}
	end := strings.LastIndex(s, "}")
	if end == -1 || end <= start {
		return s
	}

	return s[start : end+1]
}
