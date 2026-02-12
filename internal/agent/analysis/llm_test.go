// Package analysis 测试 LLM 接口和实现
package analysis

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/AceDarkknight/k8s-analyzer-agent/internal/client"
	"github.com/AceDarkknight/k8s-analyzer-agent/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRuleBasedLLM_FormatToolsPrompt 测试工具列表格式化功能
func TestRuleBasedLLM_FormatToolsPrompt(t *testing.T) {
	tests := []struct {
		name           string
		tools          []client.Tool
		expectedInText []string // 期望包含的文本片段
		notExpected    []string // 不期望包含的文本片段
	}{
		{
			name:  "empty tools",
			tools: []client.Tool{},
			expectedInText: []string{
				"当前没有可用的工具",
			},
			notExpected: []string{
				"可用工具列表",
			},
		},
		{
			name: "single tool with schema",
			tools: []client.Tool{
				{
					Name:        "get_pods",
					Description: "获取 Pod 列表",
					InputSchema: json.RawMessage(`{
						"type": "object",
						"properties": {
							"namespace": {
								"type": "string",
								"description": "命名空间"
							}
						},
						"required": ["namespace"]
					}`),
				},
			},
			expectedInText: []string{
				"## 可用工具列表",
				"1. get_pods",
				"获取 Pod 列表",
				"**参数要求**:",
				"namespace",
				"命名空间",
				"**必需参数**:",
			},
			notExpected: []string{
				"2. ", // 不应该有第二个工具
			},
		},
		{
			name: "multiple tools",
			tools: []client.Tool{
				{
					Name:        "get_pods",
					Description: "获取 Pod 列表",
					InputSchema: json.RawMessage(`{
						"type": "object",
						"properties": {
							"namespace": {"type": "string", "description": "命名空间"}
						}
					}`),
				},
				{
					Name:        "get_logs",
					Description: "获取 Pod 日志",
					InputSchema: json.RawMessage(`{
						"type": "object",
						"properties": {
							"pod_name": {"type": "string", "description": "Pod 名称"},
							"container": {"type": "string", "description": "容器名称"}
						},
						"required": ["pod_name"]
					}`),
				},
			},
			expectedInText: []string{
				"## 可用工具列表",
				"1. get_pods",
				"2. get_logs",
				"获取 Pod 列表",
				"获取 Pod 日志",
				"namespace",
				"pod_name",
				"container",
			},
			notExpected: []string{
				"3. ", // 不应该有第三个工具
			},
		},
		{
			name: "tool without schema",
			tools: []client.Tool{
				{
					Name:        "simple_tool",
					Description: "简单工具",
					InputSchema: nil,
				},
			},
			expectedInText: []string{
				"## 可用工具列表",
				"1. simple_tool",
				"简单工具",
			},
			notExpected: []string{
				"**参数要求**:",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 创建 RuleBasedLLM
			llm := NewRuleBasedLLM(&config.LLMConfig{
				Provider: "test",
				Model:    "test-model",
			})

			// 设置工具
			llm.SetTools(tt.tools)

			// 格式化工具提示
			prompt := llm.FormatToolsPrompt()

			// 验证期望包含的文本
			for _, expected := range tt.expectedInText {
				assert.Contains(t, prompt, expected,
					"提示应该包含: %s", expected)
			}

			// 验证不期望包含的文本
			for _, notExpected := range tt.notExpected {
				assert.NotContains(t, prompt, notExpected,
					"提示不应该包含: %s", notExpected)
			}

			// 验证提示不为空（除了空工具列表的情况）
			assert.NotEmpty(t, prompt, "提示不应为空")
		})
	}
}

// TestRuleBasedLLM_SetTools 测试设置工具功能
func TestRuleBasedLLM_SetTools(t *testing.T) {
	llm := NewRuleBasedLLM(&config.LLMConfig{
		Provider: "test",
		Model:    "test-model",
	})

	// 初始状态：没有工具
	assert.Empty(t, llm.tools, "初始工具列表应为空")

	// 设置工具
	tools := []client.Tool{
		{
			Name:        "tool1",
			Description: "第一个工具",
		},
		{
			Name:        "tool2",
			Description: "第二个工具",
		},
	}
	llm.SetTools(tools)

	// 验证工具已设置
	assert.Len(t, llm.tools, 2, "应该有 2 个工具")
	assert.Equal(t, "tool1", llm.tools[0].Name)
	assert.Equal(t, "tool2", llm.tools[1].Name)

	// 验证格式化后的提示包含工具名称
	prompt := llm.FormatToolsPrompt()
	assert.Contains(t, prompt, "tool1")
	assert.Contains(t, prompt, "tool2")
	assert.Contains(t, prompt, "第一个工具")
	assert.Contains(t, prompt, "第二个工具")
}

// TestRuleBasedLLM_FormatToolsPrompt_ComplexSchema 测试复杂 Schema 的格式化
func TestRuleBasedLLM_FormatToolsPrompt_ComplexSchema(t *testing.T) {
	llm := NewRuleBasedLLM(&config.LLMConfig{
		Provider: "test",
		Model:    "test-model",
	})

	// 创建包含复杂 Schema 的工具
	tools := []client.Tool{
		{
			Name:        "execute_command",
			Description: "执行 Shell 命令",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"command": {
						"type": "string",
						"description": "要执行的命令"
					},
					"timeout": {
						"type": "integer",
						"description": "超时时间（秒）"
					},
					"env": {
						"type": "object",
						"description": "环境变量"
					}
				},
				"required": ["command"]
			}`),
		},
	}

	llm.SetTools(tools)
	prompt := llm.FormatToolsPrompt()

	// 验证基本信息
	assert.Contains(t, prompt, "execute_command")
	assert.Contains(t, prompt, "执行 Shell 命令")

	// 验证参数信息
	assert.Contains(t, prompt, "command")
	assert.Contains(t, prompt, "timeout")
	assert.Contains(t, prompt, "env")
	assert.Contains(t, prompt, "要执行的命令")
	assert.Contains(t, prompt, "超时时间")
	assert.Contains(t, prompt, "环境变量")

	// 验证必需参数
	assert.Contains(t, prompt, "必需参数")
	assert.Contains(t, prompt, "command")
}

// TestRuleBasedLLM_FormatToolsPrompt_InvalidSchema 测试无效 Schema 的处理
func TestRuleBasedLLM_FormatToolsPrompt_InvalidSchema(t *testing.T) {
	llm := NewRuleBasedLLM(&config.LLMConfig{
		Provider: "test",
		Model:    "test-model",
	})

	// 创建包含无效 Schema 的工具
	tools := []client.Tool{
		{
			Name:        "invalid_tool",
			Description: "无效 Schema 的工具",
			InputSchema: json.RawMessage(`{invalid json`), // 无效的 JSON
		},
	}

	llm.SetTools(tools)

	// 格式化应该不会崩溃，即使 Schema 无效
	require.NotPanics(t, func() {
		prompt := llm.FormatToolsPrompt()
		// 仍然应该包含工具名称和描述
		assert.Contains(t, prompt, "invalid_tool")
		assert.Contains(t, prompt, "无效 Schema 的工具")
	})
}

// TestRuleBasedLLM_FormatToolsPrompt_RealWorldExample 测试真实场景的工具列表
func TestRuleBasedLLM_FormatToolsPrompt_RealWorldExample(t *testing.T) {
	llm := NewRuleBasedLLM(&config.LLMConfig{
		Provider: "openai",
		Model:    "gpt-4",
	})

	// 模拟真实的 Kubernetes 工具列表
	tools := []client.Tool{
		{
			Name:        "k8s_get_pods",
			Description: "获取 Kubernetes Pod 列表",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"namespace": {"type": "string", "description": "命名空间"},
					"label_selector": {"type": "string", "description": "标签选择器"}
				},
				"required": ["namespace"]
			}`),
		},
		{
			Name:        "k8s_get_logs",
			Description: "获取 Pod 日志",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"pod_name": {"type": "string", "description": "Pod 名称"},
					"namespace": {"type": "string", "description": "命名空间"},
					"container": {"type": "string", "description": "容器名称"},
					"tail_lines": {"type": "integer", "description": "尾部行数"}
				},
				"required": ["pod_name", "namespace"]
			}`),
		},
		{
			Name:        "shell_execute",
			Description: "执行 Shell 命令",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"command": {"type": "string", "description": "Shell 命令"}
				},
				"required": ["command"]
			}`),
		},
	}

	llm.SetTools(tools)
	prompt := llm.FormatToolsPrompt()

	// 验证格式化的提示包含所有关键信息
	expectedElements := []string{
		"## 可用工具列表",
		"1. k8s_get_pods",
		"2. k8s_get_logs",
		"3. shell_execute",
		"获取 Kubernetes Pod 列表",
		"获取 Pod 日志",
		"执行 Shell 命令",
		"namespace",
		"pod_name",
		"command",
	}

	for _, element := range expectedElements {
		assert.Contains(t, prompt, element,
			"提示应该包含: %s", element)
	}

	// 验证提示结构合理
	assert.True(t, strings.HasPrefix(prompt, "## 可用工具列表"),
		"提示应该以标题开头")
	assert.Contains(t, prompt, "**参数要求**",
		"提示应该包含参数要求说明")
}

// TestMin 测试 min 辅助函数
func TestMin(t *testing.T) {
	tests := []struct {
		name     string
		a        int
		b        int
		expected int
	}{
		{"a < b", 5, 10, 5},
		{"a > b", 10, 5, 5},
		{"a == b", 7, 7, 7},
		{"negative numbers", -5, -3, -5},
		{"zero", 0, 5, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := min(tt.a, tt.b)
			assert.Equal(t, tt.expected, result)
		})
	}
}
