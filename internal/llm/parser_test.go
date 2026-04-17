package llm

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractJSON(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "纯 JSON",
			input:    `{"key": "value", "number": 123}`,
			expected: `{"key": "value", "number": 123}`,
		},
		{
			name:     "Markdown json 代码块",
			input:    "```json\n{\"key\": \"value\"}\n```",
			expected: `{"key": "value"}`,
		},
		{
			name:     "Markdown 无语言代码块",
			input:    "```\n{\"key\": \"value\"}\n```",
			expected: `{"key": "value"}`,
		},
		{
			name:     "混合文本 - 前面有说明",
			input:    "这是一些说明文字\n```json\n{\"key\": \"value\"}\n```\n后面还有内容",
			expected: `{"key": "value"}`,
		},
		{
			name:     "无代码块，从文本中提取",
			input:    "开始文字{\"key\": \"value\"}结束文字",
			expected: `{"key": "value"}`,
		},
		{
			name:     "空字符串",
			input:    "",
			expected: "",
		},
		{
			name:     "无 JSON 内容",
			input:    "这是纯文本，没有 JSON",
			expected: "这是纯文本，没有 JSON",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractJSON(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestParseDecisionResponse(t *testing.T) {
	tests := []struct {
		name             string
		input            string
		expectedThought  string
		expectedDecision string
		expectError      bool
	}{
		{
			name: "正常 JSON 响应",
			input: `{
				"thought": "我发现了异常 Pod",
				"decision": "continue",
				"tool_calls": [{"name": "get_pod_logs", "args": {"namespace": "default", "name": "test-pod"}}],
				"deep_query_topic": ""
			}`,
			expectedThought:  "我发现了异常 Pod",
			expectedDecision: "execute_plan",
			expectError:      false,
		},
		{
			name: "Markdown 包裹的 JSON",
			input: "```json\n" + `{
				"thought": "需要深入调查",
				"decision": "deep_query",
				"tool_calls": [],
				"deep_query_topic": "网络连接问题"
			}` + "\n```",
			expectedThought:  "需要深入调查",
			expectedDecision: "deep_query",
			expectError:      false,
		},
		{
			name:             "空内容",
			input:            "",
			expectedThought:  "",
			expectedDecision: "",
			expectError:      true,
		},
		{
			name:             "降级文本匹配 - report",
			input:            "我认为已经收集到足够的信息，可以生成报告了。decision: report",
			expectedThought:  "我认为已经收集到足够的信息，可以生成报告了。decision: report",
			expectedDecision: "report",
			expectError:      false,
		},
		{
			name:             "降级文本匹配 - deep_query",
			input:            "这个问题比较复杂，需要深入调查。decision: deep_query",
			expectedThought:  "这个问题比较复杂，需要深入调查。decision: deep_query",
			expectedDecision: "deep_query",
			expectError:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseDecisionResponse(tt.input)
			if tt.expectError {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.expectedThought, result.Thought)
			assert.Equal(t, tt.expectedDecision, result.Decision)
		})
	}
}

func TestParseDecisionResponse_UseSkill(t *testing.T) {
	input := `{
        "thought": "准备执行技能分析",
        "decision": "use_skill",
        "skill_name": "deploy_app_skill",
        "tool_calls": []
    }`
	result, err := ParseDecisionResponse(input)
	require.NoError(t, err)
	assert.Equal(t, "use_skill", result.Decision)
	assert.Equal(t, "deploy_app_skill", result.SkillName)
}

func TestParseDecisionResponse_ToolCalls(t *testing.T) {
	input := `{
		"thought": "查看 Pod 日志",
		"decision": "continue",
		"tool_calls": [
			{"name": "get_pod_logs", "args": {"namespace": "default", "name": "test-pod", "tailLines": 100}},
			{"name": "describe_pod", "args": {"namespace": "default", "name": "test-pod"}}
		]
	}`

	result, err := ParseDecisionResponse(input)
	require.NoError(t, err)
	assert.Equal(t, "execute_plan", result.Decision)
	assert.Len(t, result.ToolCalls, 2)
	assert.Equal(t, "get_pod_logs", result.ToolCalls[0].Name)
	assert.Equal(t, "default", result.ToolCalls[0].Args["namespace"])
	assert.Equal(t, "describe_pod", result.ToolCalls[1].Name)
}

func TestParseAuditResponse(t *testing.T) {
	tests := []struct {
		name           string
		input          string
		expectedLevel  string
		expectedReason string
		expectedAdvice string
		expectError    bool
	}{
		{
			name: "safe 级别",
			input: `{
				"safety_level": "safe",
				"reason": "这是一个只读命令",
				"advice": ""
			}`,
			expectedLevel:  "safe",
			expectedReason: "这是一个只读命令",
			expectedAdvice: "",
			expectError:    false,
		},
		{
			name: "dangerous 级别",
			input: `{
				"safety_level": "dangerous",
				"reason": "会删除数据",
				"advice": "使用 ls 代替 rm"
			}`,
			expectedLevel:  "dangerous",
			expectedReason: "会删除数据",
			expectedAdvice: "使用 ls 代替 rm",
			expectError:    false,
		},
		{
			name: "Markdown 包裹",
			input: "```json\n" + `{
				"safety_level": "warning",
				"reason": "可能影响系统",
				"advice": ""
			}` + "\n```",
			expectedLevel:  "warning",
			expectedReason: "可能影响系统",
			expectedAdvice: "",
			expectError:    false,
		},
		{
			name:        "空内容",
			input:       "",
			expectError: true,
		},
		{
			name:        "无效的 safety_level",
			input:       `{"safety_level": "invalid", "reason": "test"}`,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseAuditResponse(tt.input)
			if tt.expectError {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.expectedLevel, result.SafetyLevel)
			assert.Equal(t, tt.expectedReason, result.Reason)
			assert.Equal(t, tt.expectedAdvice, result.Advice)
		})
	}
}

func TestParseAnalysisResponse(t *testing.T) {
	tests := []struct {
		name             string
		input            string
		expectedSummary  string
		expectedSeverity string
		expectError      bool
	}{
		{
			name: "完整的分析报告",
			input: `{
				"summary": "发现内存泄漏问题",
				"severity": "critical",
				"root_cause": "应用程序未正确释放内存",
				"findings": [
					{
						"resource": "pod/app-123",
						"severity": "critical",
						"message": "内存使用超过限制",
						"evidence": "日志显示 OOMKilled"
					}
				],
				"recommendations": [
					{
						"priority": "high",
						"action": "重启 Pod",
						"command": "kubectl delete pod app-123",
						"risk": "短暂服务中断"
					}
				],
				"limitations": ""
			}`,
			expectedSummary:  "发现内存泄漏问题",
			expectedSeverity: "critical",
			expectError:      false,
		},
		{
			name: "Markdown 包裹",
			input: "```json\n" + `{
				"summary": "网络连接正常",
				"severity": "info",
				"root_cause": "",
				"findings": [],
				"recommendations": [],
				"limitations": ""
			}` + "\n```",
			expectedSummary:  "网络连接正常",
			expectedSeverity: "info",
			expectError:      false,
		},
		{
			name:        "空内容",
			input:       "",
			expectError: true,
		},
		{
			name:        "无效 JSON",
			input:       "这不是有效的 JSON",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseAnalysisResponse(tt.input)
			if tt.expectError {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.expectedSummary, result.Summary)
			assert.Equal(t, tt.expectedSeverity, result.Severity)
			assert.Equal(t, "completed", result.Status)
		})
	}
}

func TestParseAnalysisResponse_FullStructure(t *testing.T) {
	input := `{
		"summary": "集群诊断完成",
		"severity": "warning",
		"root_cause": "资源不足",
		"findings": [
			{
				"resource": "node/worker-1",
				"severity": "warning",
				"message": "磁盘空间不足",
				"evidence": "df -h 显示 /var 使用 95%"
			},
			{
				"resource": "pod/nginx-abc",
				"severity": "info",
				"message": "重启次数较多",
				"evidence": "重启 5 次"
			}
		],
		"recommendations": [
			{
				"priority": "high",
				"action": "清理磁盘",
				"command": "kubectl exec ...",
				"risk": "可能删除重要日志",
				"executable": false
			},
			{
				"priority": "medium",
				"action": "检查 Pod 配置",
				"command": "",
				"risk": "",
				"executable": false
			}
		],
		"limitations": "部分命令被拒绝执行"
	}`

	result, err := ParseAnalysisResponse(input)
	require.NoError(t, err)

	assert.Equal(t, "集群诊断完成", result.Summary)
	assert.Equal(t, "warning", result.Severity)
	assert.Equal(t, "资源不足", result.RootCause)
	assert.Equal(t, "部分命令被拒绝执行", result.Limitations)

	// 验证 findings
	require.Len(t, result.Findings, 2)
	assert.Equal(t, "node/worker-1", result.Findings[0].Resource)
	assert.Equal(t, "warning", result.Findings[0].Severity)
	assert.Equal(t, "磁盘空间不足", result.Findings[0].Message)
	assert.Equal(t, "df -h 显示 /var 使用 95%", result.Findings[0].Evidence)

	// 验证 recommendations
	require.Len(t, result.Recommendations, 2)
	assert.Equal(t, "high", result.Recommendations[0].Priority)
	assert.Equal(t, "清理磁盘", result.Recommendations[0].Action)
	assert.Equal(t, "kubectl exec ...", result.Recommendations[0].Command)
	assert.Equal(t, "可能删除重要日志", result.Recommendations[0].Risk)
}

func TestExtractJSON_Nested(t *testing.T) {
	// 测试嵌套 JSON
	input := `{"outer": {"inner": "value"}}`
	result := ExtractJSON(input)
	assert.Equal(t, `{"outer": {"inner": "value"}}`, result)
}

func TestExtractJSON_MultipleCodeBlocks(t *testing.T) {
	// 测试多个代码块，应该提取第一个
	input := "```json\n{\"first\": 1}\n```\n一些文字\n```json\n{\"second\": 2}\n```"
	result := ExtractJSON(input)
	assert.Equal(t, `{"first": 1}`, result)
}

// TestParseDecisionResponse_InvalidJSON 测试无效 JSON 但包含有效 decision 关键词的情况
func TestParseDecisionResponse_InvalidJSONWithDecisionKeyword(t *testing.T) {
	input := `这不是 JSON，但包含 decision: report 关键词`
	result, err := ParseDecisionResponse(input)
	require.NoError(t, err)
	assert.Equal(t, "report", result.Decision)
	assert.Equal(t, input, result.Thought)
}

// TestParseDecisionResponse_ContinueFallback 测试降级到 continue
func TestParseDecisionResponse_ContinueFallback(t *testing.T) {
	input := `无法识别的响应格式`
	result, err := ParseDecisionResponse(input)
	require.NoError(t, err)
	assert.Equal(t, "continue", result.Decision)
	assert.Equal(t, input, result.Thought)
}
