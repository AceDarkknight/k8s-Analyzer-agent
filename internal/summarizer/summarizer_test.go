package summarizer

import (
	"strings"
	"testing"
)

func TestNewOutputSummarizer(t *testing.T) {
	tests := []struct {
		name     string
		maxLines int
		maxChars int
		wantLines int
		wantChars int
	}{
		{
			name:     "使用自定义值",
			maxLines: 10,
			maxChars: 100,
			wantLines: 10,
			wantChars: 100,
		},
		{
			name:     "maxLines为0时使用默认值",
			maxLines: 0,
			maxChars: 100,
			wantLines: 50,
			wantChars: 100,
		},
		{
			name:     "maxChars为0时使用默认值",
			maxLines: 10,
			maxChars: 0,
			wantLines: 10,
			wantChars: 3000,
		},
		{
			name:     "都为0时使用默认值",
			maxLines: 0,
			maxChars: 0,
			wantLines: 50,
			wantChars: 3000,
		},
		{
			name:     "负数时使用默认值",
			maxLines: -5,
			maxChars: -100,
			wantLines: 50,
			wantChars: 3000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewOutputSummarizer(tt.maxLines, tt.maxChars)
			if s.MaxLines != tt.wantLines {
				t.Errorf("MaxLines = %d, want %d", s.MaxLines, tt.wantLines)
			}
			if s.MaxChars != tt.wantChars {
				t.Errorf("MaxChars = %d, want %d", s.MaxChars, tt.wantChars)
			}
		})
	}
}

func TestOutputSummarizer_Summarize_ShortOutput(t *testing.T) {
	// 短输出不触发摘要（原样返回）
	s := NewOutputSummarizer(10, 100)
	input := "line1\nline2\nline3"
	
	result := s.Summarize(input)
	
	if result != input {
		t.Errorf("短输出应该原样返回，got:\n%s\nwant:\n%s", result, input)
	}
}

func TestOutputSummarizer_Summarize_ExceedMaxLines(t *testing.T) {
	// 超过 MaxLines 时截断
	s := NewOutputSummarizer(5, 1000)
	
	// 生成10行输出
	var lines []string
	for i := 1; i <= 10; i++ {
		lines = append(lines, "Line "+string(rune('0'+i)))
	}
	input := strings.Join(lines, "\n")
	
	result := s.Summarize(input)
	
	// 检查结果应该包含摘要标记
	if !strings.Contains(result, "[输出已摘要") {
		t.Errorf("超过MaxLines时应该包含摘要标记，got:\n%s", result)
	}
	
	// 检查行数不超过MaxLines（加上摘要标记行）
	resultLines := strings.Split(result, "\n")
	if len(resultLines) > s.MaxLines+1 {
		t.Errorf("结果行数 %d 超过了限制 %d", len(resultLines), s.MaxLines+1)
	}
}

func TestOutputSummarizer_Summarize_ExceedMaxChars(t *testing.T) {
	// 超过 MaxChars 时截断
	s := NewOutputSummarizer(50, 50)
	
	input := "This is a very long line that definitely exceeds fifty characters limit"
	
	result := s.Summarize(input)
	
	// 检查结果应该包含摘要标记
	if !strings.Contains(result, "[输出已摘要") {
		t.Errorf("超过MaxChars时应该包含摘要标记，got:\n%s", result)
	}
	
	// 检查长度不超过MaxChars（加上摘要标记的空间）
	if len(result) > s.MaxChars+50 {
		t.Errorf("结果长度 %d 超过了合理范围", len(result))
	}
}

func TestOutputSummarizer_Summarize_PriorityLines(t *testing.T) {
	// 优先保留 ERROR/WARN 行
	s := NewOutputSummarizer(3, 1000)
	
	input := `Normal line 1
ERROR: something went wrong
Normal line 2
WARN: this is a warning
Normal line 3
Normal line 4`
	
	result := s.Summarize(input)
	
	// 检查结果应该包含 ERROR 和 WARN 行
	if !strings.Contains(result, "ERROR:") {
		t.Errorf("结果应该包含 ERROR 行，got:\n%s", result)
	}
	if !strings.Contains(result, "WARN:") {
		t.Errorf("结果应该包含 WARN 行，got:\n%s", result)
	}
	
	// 由于 MaxLines=3，优先行有2个(ERROR和WARN)，只能再加1个普通行
	// 所以不应该包含 Normal line 4
	if strings.Contains(result, "Normal line 4") {
		t.Errorf("结果不应该包含超出限制的普通行，got:\n%s", result)
	}
}

func TestOutputSummarizer_Summarize_RemoveDuplicates(t *testing.T) {
	// 去重复行
	s := NewOutputSummarizer(10, 1000)
	
	input := `Unique line 1
Duplicate line
Unique line 2
Duplicate line
Unique line 3`
	
	result := s.Summarize(input)
	
	// 统计 "Duplicate line" 出现的次数
	count := strings.Count(result, "Duplicate line")
	if count > 1 {
		t.Errorf("重复行应该被去除，但出现了 %d 次，got:\n%s", count, result)
	}
	
	// 确保所有唯一行都在
	if !strings.Contains(result, "Unique line 1") {
		t.Errorf("结果应该包含 Unique line 1，got:\n%s", result)
	}
	if !strings.Contains(result, "Unique line 2") {
		t.Errorf("结果应该包含 Unique line 2，got:\n%s", result)
	}
	if !strings.Contains(result, "Unique line 3") {
		t.Errorf("结果应该包含 Unique line 3，got:\n%s", result)
	}
}

func TestOutputSummarizer_Summarize_RemoveEmptyLines(t *testing.T) {
	// 去除空行
	s := NewOutputSummarizer(10, 1000)
	
	input := `Line 1

Line 2
   
Line 3
`
	
	result := s.Summarize(input)
	
	// 检查结果不包含连续换行（空行被去除后重新连接）
	if strings.Contains(result, "\n\n") {
		t.Errorf("结果不应该包含空行，got:\n%s", result)
	}
}

func TestOutputSummarizer_Summarize_SummaryMarker(t *testing.T) {
	// 摘要标记附加正确
	s := NewOutputSummarizer(3, 1000)
	
	input := "Line 1\nLine 2\nLine 3\nLine 4\nLine 5"
	
	result := s.Summarize(input)
	
	// 检查摘要标记格式
	if !strings.Contains(result, "[输出已摘要，原始 5 行 / 显示") {
		t.Errorf("摘要标记格式不正确，got:\n%s", result)
	}
}

func TestOutputSummarizer_Summarize_AllPriorityKeywords(t *testing.T) {
	// 测试所有优先关键词
	s := NewOutputSummarizer(10, 1000)
	
	tests := []struct {
		keyword string
		line    string
	}{
		{"ERROR", "ERROR: database connection failed"},
		{"WARN", "WARN: low disk space"},
		{"FATAL", "FATAL: system crash"},
		{"panic", "panic: runtime error"},
		{"OOMKilled", "Container was OOMKilled"},
		{"CrashLoopBackOff", "Pod is in CrashLoopBackOff state"},
		{"ImagePullBackOff", "Failed to pull image: ImagePullBackOff"},
	}
	
	// 构建输入：每个关键词一行，加上一些普通行
	var lines []string
	for _, tt := range tests {
		lines = append(lines, tt.line)
	}
	// 添加超过限制的普通行
	for i := 1; i <= 10; i++ {
		lines = append(lines, "Normal line "+string(rune('0'+i)))
	}
	
	input := strings.Join(lines, "\n")
	result := s.Summarize(input)
	
	// 所有优先行都应该被保留
	for _, tt := range tests {
		if !strings.Contains(result, tt.keyword) {
			t.Errorf("结果应该包含 %s 关键词的行，got:\n%s", tt.keyword, result)
		}
	}
}

func TestOutputSummarizer_Summarize_CaseInsensitivePriority(t *testing.T) {
	// 测试关键词不区分大小写
	s := NewOutputSummarizer(5, 1000)
	
	input := `normal line 1
error: lowercase error
normal line 2
WARN: uppercase warning
normal line 3
Fatal: mixed case fatal`
	
	result := s.Summarize(input)
	
	// 所有不同大小写的关键词行都应该被保留
	if !strings.Contains(result, "error:") {
		t.Errorf("结果应该包含小写的 error 行，got:\n%s", result)
	}
	if !strings.Contains(result, "WARN:") {
		t.Errorf("结果应该包含 WARN 行，got:\n%s", result)
	}
	if !strings.Contains(result, "Fatal:") {
		t.Errorf("结果应该包含 Fatal 行，got:\n%s", result)
	}
}
