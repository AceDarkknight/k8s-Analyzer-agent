package summarizer

import (
	"fmt"
	"strings"
)

const (
	defaultMaxLines = 50
	defaultMaxChars = 3000
)

// OutputSummarizer 输出摘要器
// 用于将 Gateway/MCP 返回的大输出在进入 LLM Prompt 前进行摘要
type OutputSummarizer struct {
	MaxLines int // 最大行数，默认 50
	MaxChars int // 最大字符数，默认 3000
}

// NewOutputSummarizer 创建摘要器
// 如果 maxLines <= 0 设为 50，如果 maxChars <= 0 设为 3000
func NewOutputSummarizer(maxLines, maxChars int) *OutputSummarizer {
	if maxLines <= 0 {
		maxLines = defaultMaxLines
	}
	if maxChars <= 0 {
		maxChars = defaultMaxChars
	}
	return &OutputSummarizer{
		MaxLines: maxLines,
		MaxChars: maxChars,
	}
}

// priorityKeywords 定义优先行的关键词（不区分大小写）
var priorityKeywords = []string{
	// 错误类
	"ERROR",
	"WARN",
	"FATAL",
	"panic",
	// Pod 状态异常
	"OOMKilled",
	"CrashLoopBackOff",
	"ImagePullBackOff",
	"ErrImagePull",
	"CreateContainerError",
	// 调度相关（Pending Pod 关键信息）
	"FailedScheduling",
	"Unschedulable",
	"Insufficient",
	"node(s) didn't match",
	"node(s) had taint",
	"PodToleratesNodeTaints",
	"NodeAffinity",
	"NodeSelector",
	// 存储相关
	"FailedMount",
	"FailedAttachVolume",
	"VolumeBinding",
	"PersistentVolumeClaim",
	// 资源相关（用于计算节点剩余资源）
	"Allocatable:",
	"Allocated resources:",
	"Resource           Requests",  // Allocated resources 表头
	"(Total limits",                 // Allocated resources 注释行
	"cpu:",                          // Allocatable 中的 cpu 行
	"memory:",                       // Allocatable 中的 memory 行
	"cpu ",                          // Allocated resources 中的 cpu 行
	"memory ",                       // Allocated resources 中的 memory 行
	// 其他关键信息
	"BackOff",
	"Failed",
	"Evicted",
	"Pending",
}

// isPriorityLine 判断是否为优先行
func (s *OutputSummarizer) isPriorityLine(line string) bool {
	upperLine := strings.ToUpper(line)
	for _, keyword := range priorityKeywords {
		if strings.Contains(upperLine, strings.ToUpper(keyword)) {
			return true
		}
	}
	return false
}

// Summarize 对输出进行摘要
func (s *OutputSummarizer) Summarize(output string) string {
	// 按行分割
	lines := strings.Split(output, "\n")
	totalLines := len(lines)

	// 去除空行并去重（保留原始行内容，但用trim后的内容去重）
	seen := make(map[string]bool)
	var nonEmptyLines []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if !seen[trimmed] {
			seen[trimmed] = true
			nonEmptyLines = append(nonEmptyLines, line)
		}
	}

	_ = totalLines // avoid unused variable warning

	// 构建去重去空后的结果
	cleanedOutput := strings.Join(nonEmptyLines, "\n")

	// 如果处理后长度和行数都在限制内，直接返回处理后的结果
	if len(cleanedOutput) <= s.MaxChars && len(nonEmptyLines) <= s.MaxLines {
		return cleanedOutput
	}

	// 将行分为优先行和普通行
	var priorityLines, normalLines []string
	for _, line := range nonEmptyLines {
		if s.isPriorityLine(line) {
			priorityLines = append(priorityLines, line)
		} else {
			normalLines = append(normalLines, line)
		}
	}

	// 合并结果：优先行 + 普通行，总行数不超过 MaxLines
	var resultLines []string
	resultLines = append(resultLines, priorityLines...)
	remainingLines := s.MaxLines - len(priorityLines)
	if remainingLines > 0 && len(normalLines) > 0 {
		if remainingLines >= len(normalLines) {
			resultLines = append(resultLines, normalLines...)
		} else {
			resultLines = append(resultLines, normalLines[:remainingLines]...)
		}
	}

	// 构建结果字符串
	result := strings.Join(resultLines, "\n")
	displayedLines := len(resultLines)

	// 预留摘要标记的空间
	summaryMarker := fmt.Sprintf("[输出已摘要，原始 %d 行 / 显示 %d 行]", totalLines, displayedLines)
	availableChars := s.MaxChars - len(summaryMarker) - 1 // -1 for newline
	if availableChars < 0 {
		availableChars = 0
	}

	// 截断到可用字符数
	if len(result) > availableChars {
		result = result[:availableChars]
		// 确保不在字符中间截断，找到最后一个完整的行
		lastNewline := strings.LastIndex(result, "\n")
		if lastNewline > 0 {
			result = result[:lastNewline]
			displayedLines = strings.Count(result, "\n") + 1
		} else {
			// 如果截断后没有换行符，说明只有一行被截断
			displayedLines = 1
		}
		// 重新生成摘要标记（因为 displayedLines 可能改变了）
		summaryMarker = fmt.Sprintf("[输出已摘要，原始 %d 行 / 显示 %d 行]", totalLines, displayedLines)
	}

	// 附加摘要标记
	if len(result) > 0 {
		result = result + "\n" + summaryMarker
	} else {
		result = summaryMarker
	}

	return result
}
