package diagnosis

import (
	"context"
	"fmt"
	"strings"

	"github.com/AceDarkknight/k8s-analyzer-agent/internal/logger"
	"github.com/AceDarkknight/k8s-analyzer-agent/internal/state"
)

// CompressNode 压缩节点
type CompressNode struct {
	threshold  int // 默认 4
	recentKeep int // 默认 3
}

// NewCompressNode 创建新的压缩节点
func NewCompressNode(threshold, recentKeep int) *CompressNode {
	if threshold <= 0 {
		threshold = 4
	}
	if recentKeep <= 0 {
		recentKeep = 3
	}
	return &CompressNode{
		threshold:  threshold,
		recentKeep: recentKeep,
	}
}

// Execute 执行压缩
func (n *CompressNode) Execute(ctx context.Context, s *state.State) (*state.State, error) {
	logger.Info("CompressNode: checking if compression needed",
		logger.Int("history_length", len(s.ReasoningHistory)),
		logger.Int("threshold", n.threshold))

	// 1. 如果历史长度 <= threshold，直接返回
	if len(s.ReasoningHistory) <= n.threshold {
		logger.Info("CompressNode: no compression needed")
		return s, nil
	}

	// 2. 计算需要压缩的早期步骤
	historyLen := len(s.ReasoningHistory)
	earlyStepsCount := historyLen - n.recentKeep

	if earlyStepsCount <= 0 {
		logger.Info("CompressNode: not enough steps to compress")
		return s, nil
	}

	earlySteps := s.ReasoningHistory[:earlyStepsCount]
	recentSteps := s.ReasoningHistory[earlyStepsCount:]

	// 3. 压缩早期步骤
	summary := n.ruleSummarize(earlySteps)

	// 4. 更新 CompressedSummary
	if s.CompressedSummary != "" {
		s.CompressedSummary = s.CompressedSummary + "\n" + summary
	} else {
		s.CompressedSummary = summary
	}

	// 5. 保留最近 recentKeep 轮
	s.ReasoningHistory = recentSteps

	logger.Info("CompressNode: compression completed",
		logger.Int("compressed_steps", earlyStepsCount),
		logger.Int("remaining_steps", len(s.ReasoningHistory)))

	return s, nil
}

// ruleSummarize 对步骤进行摘要（保留工具调用结构化信息）
func (n *CompressNode) ruleSummarize(steps []state.ReasoningStep) string {
	var summaries []string
	for _, step := range steps {
		// 构建工具调用摘要
		toolCallSummary := ""
		if len(step.ToolCalls) > 0 {
			var tcStrs []string
			for _, tc := range step.ToolCalls {
				argStr := ""
				if ns, ok := tc.Args["namespace"].(string); ok && ns != "" {
					argStr += "ns=" + ns
				}
				if name, ok := tc.Args["name"].(string); ok && name != "" {
					if argStr != "" {
						argStr += ","
					}
					argStr += "name=" + name
				}
				if argStr != "" {
					tcStrs = append(tcStrs, fmt.Sprintf("%s(%s)", tc.Name, argStr))
				} else {
					tcStrs = append(tcStrs, tc.Name)
				}
			}
			toolCallSummary = " [工具: " + strings.Join(tcStrs, ", ") + "]"
		}
		keyFinding := n.extractKeyFinding(step.Observation)
		summary := fmt.Sprintf("迭代%d: %s%s → %s", step.Iteration, step.Decision, toolCallSummary, keyFinding)
		summaries = append(summaries, summary)
	}
	return strings.Join(summaries, "\n")
}

// extractKeyFinding 从 Observation 中提取关键发现
func (n *CompressNode) extractKeyFinding(observation string) string {
	if observation == "" {
		return "无观察结果"
	}

	// 定义关键词列表
	keywords := []string{
		"ERROR",
		"错误",
		"异常",
		"失败",
		"CrashLoop",
		"OOMKilled",
		"ImagePullBackOff",
		"Pending",
		"Evicted",
		"Failed",
		"NotReady",
		// 新增资源相关关键词
		"Allocatable",
		"Allocated",
		"Insufficient",
		"requests",
		"limits",
		"cpu",
		"memory",
	}

	lines := strings.Split(observation, "\n")
	var keyLines []string

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		upperLine := strings.ToUpper(line)
		for _, keyword := range keywords {
			if strings.Contains(upperLine, strings.ToUpper(keyword)) {
				keyLines = append(keyLines, line)
				break
			}
		}

		// 最多提取 5 行
		if len(keyLines) >= 5 {
			break
		}
	}

	if len(keyLines) == 0 {
		// 如果没有关键词，返回前 100 个字符
		if len(observation) > 100 {
			return observation[:100] + "..."
		}
		return observation
	}

	return strings.Join(keyLines, "; ")
}
