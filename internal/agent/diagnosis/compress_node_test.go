package diagnosis

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/AceDarkknight/k8s-analyzer-agent/internal/state"
)

func TestCompressNodeNoCompressionNeeded(t *testing.T) {
	// 历史长度 <= threshold 时不压缩
	node := NewCompressNode(4, 3)
	s := state.NewState("test query", 10, 4)

	// 添加 3 个步骤（少于 threshold=4）
	for i := 0; i < 3; i++ {
		step := state.ReasoningStep{
			Iteration: i + 1,
			Timestamp: time.Now(),
			Thought:   "thought " + string(rune('A'+i)),
			Decision:  "continue",
			Observation: "normal observation",
		}
		s.AddReasoningStep(step)
	}

	_, err := node.Execute(context.Background(), s)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// 验证历史长度不变
	if len(s.ReasoningHistory) != 3 {
		t.Errorf("expected 3 steps, got %d", len(s.ReasoningHistory))
	}

	// 验证 CompressedSummary 为空
	if s.CompressedSummary != "" {
		t.Errorf("expected empty summary, got %q", s.CompressedSummary)
	}
}

func TestCompressNodeCompressionTriggered(t *testing.T) {
	// 历史长度 > threshold 时正确压缩
	node := NewCompressNode(4, 3)
	s := state.NewState("test query", 10, 4)

	// 添加 6 个步骤（超过 threshold=4）
	for i := 0; i < 6; i++ {
		step := state.ReasoningStep{
			Iteration: i + 1,
			Timestamp: time.Now(),
			Thought:   "thought " + string(rune('A'+i)),
			Decision:  "continue",
			Observation: "observation " + string(rune('1'+i)),
		}
		s.AddReasoningStep(step)
	}

	_, err := node.Execute(context.Background(), s)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// 验证历史长度变为 3（recentKeep）
	if len(s.ReasoningHistory) != 3 {
		t.Errorf("expected 3 steps after compression, got %d", len(s.ReasoningHistory))
	}

	// 验证 CompressedSummary 不为空
	if s.CompressedSummary == "" {
		t.Errorf("expected non-empty summary after compression")
	}

	// 验证保留的是最近的 3 个步骤（第4、5、6步）
	// 原索引 3,4,5 变为 0,1,2
	expectedIterations := []int{4, 5, 6}
	for i, step := range s.ReasoningHistory {
		if step.Iteration != expectedIterations[i] {
			t.Errorf("step %d: expected iteration %d, got %d", i, expectedIterations[i], step.Iteration)
		}
	}
}

func TestCompressNodeExtractKeyFinding(t *testing.T) {
	node := NewCompressNode(4, 3)

	tests := []struct {
		name       string
		observation string
		shouldContain []string
	}{
		{
			name:       "empty observation",
			observation: "",
			shouldContain: []string{"无观察结果"},
		},
		{
			name:       "error keyword",
			observation: "Normal line\nERROR: something went wrong\nAnother line",
			shouldContain: []string{"ERROR"},
		},
		{
			name:       "chinese error keyword",
			observation: "正常输出\n发生错误: 连接失败\n其他信息",
			shouldContain: []string{"错误"},
		},
		{
			name:       "crash loop keyword",
			observation: "Pod status: CrashLoopBackOff\nRestart count: 5",
			shouldContain: []string{"CrashLoop"},
		},
		{
			name:       "no keywords",
			observation: "This is a normal observation without any errors",
			shouldContain: []string{"normal observation"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := node.extractKeyFinding(tt.observation)
			for _, expected := range tt.shouldContain {
				if !strings.Contains(result, expected) {
					t.Errorf("expected result to contain %q, got %q", expected, result)
				}
			}
		})
	}
}

func TestCompressNodeRuleSummarize(t *testing.T) {
	node := NewCompressNode(4, 3)

	steps := []state.ReasoningStep{
		{Decision: "continue", Observation: "Normal output"},
		{Decision: "continue", Observation: "ERROR: something failed"},
		{Decision: "report", Observation: "Final observation"},
	}

	summary := node.ruleSummarize(steps)

	// 验证摘要包含所有步骤
	if !strings.Contains(summary, "步骤1") {
		t.Errorf("summary should contain '步骤1'")
	}
	if !strings.Contains(summary, "步骤2") {
		t.Errorf("summary should contain '步骤2'")
	}
	if !strings.Contains(summary, "步骤3") {
		t.Errorf("summary should contain '步骤3'")
	}

	// 验证包含决策信息
	if !strings.Contains(summary, "continue") {
		t.Errorf("summary should contain 'continue'")
	}
	if !strings.Contains(summary, "report") {
		t.Errorf("summary should contain 'report'")
	}
}

func TestCompressNodeMultipleCompressions(t *testing.T) {
	// 测试多次压缩
	node := NewCompressNode(4, 3)
	s := state.NewState("test query", 10, 4)

	// 第一次压缩：6 步 -> 3 步 + 摘要
	for i := 0; i < 6; i++ {
		step := state.ReasoningStep{
			Iteration:   i + 1,
			Timestamp:   time.Now(),
			Thought:     "thought " + string(rune('A'+i)),
			Decision:    "continue",
			Observation: "observation " + string(rune('1'+i)),
		}
		s.AddReasoningStep(step)
	}

	_, err := node.Execute(context.Background(), s)
	if err != nil {
		t.Fatalf("First Execute failed: %v", err)
	}

	firstSummary := s.CompressedSummary
	if firstSummary == "" {
		t.Errorf("expected non-empty summary after first compression")
	}

	// 添加更多步骤触发第二次压缩
	for i := 0; i < 4; i++ {
		step := state.ReasoningStep{
			Iteration:   i + 7,
			Timestamp:   time.Now(),
			Thought:     "thought batch2 " + string(rune('A'+i)),
			Decision:    "continue",
			Observation: "observation batch2 " + string(rune('1'+i)),
		}
		s.AddReasoningStep(step)
	}

	_, err = node.Execute(context.Background(), s)
	if err != nil {
		t.Fatalf("Second Execute failed: %v", err)
	}

	// 验证摘要累积
	if len(s.CompressedSummary) <= len(firstSummary) {
		t.Errorf("summary should grow after second compression")
	}

	// 验证最终历史长度仍为 3
	if len(s.ReasoningHistory) != 3 {
		t.Errorf("expected 3 steps after second compression, got %d", len(s.ReasoningHistory))
	}
}

func TestNewCompressNodeDefaults(t *testing.T) {
	// 测试默认值
	node := NewCompressNode(0, 0)
	if node.threshold != 4 {
		t.Errorf("expected default threshold 4, got %d", node.threshold)
	}
	if node.recentKeep != 3 {
		t.Errorf("expected default recentKeep 3, got %d", node.recentKeep)
	}

	// 测试自定义值
	node2 := NewCompressNode(5, 2)
	if node2.threshold != 5 {
		t.Errorf("expected threshold 5, got %d", node2.threshold)
	}
	if node2.recentKeep != 2 {
		t.Errorf("expected recentKeep 2, got %d", node2.recentKeep)
	}
}
