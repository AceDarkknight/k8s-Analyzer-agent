// Package safety 测试命令安全验证功能
package safety

import (
	"strings"
	"testing"

	"github.com/AceDarkknight/k8s-analyzer-agent/internal/client"
	"github.com/AceDarkknight/k8s-analyzer-agent/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRuleBasedAuditor_FormatToolsPrompt 测试工具列表格式化功能
func TestRuleBasedAuditor_FormatToolsPrompt(t *testing.T) {
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
				"当前没有可用的 Shell 工具",
				"所有命令均被禁止",
			},
			notExpected: []string{
				"允许的 Shell 命令列表",
			},
		},
		{
			name: "single shell tool",
			tools: []client.Tool{
				{
					Name:        "ls",
					Description: "列出目录内容",
				},
			},
			expectedInText: []string{
				"## 允许的 Shell 命令列表",
				"1. **ls**: 列出目录内容",
				"**审计指南**:",
				"只允许执行上述列表中的命令",
			},
			notExpected: []string{
				"2. ", // 不应该有第二个工具
			},
		},
		{
			name: "multiple shell tools",
			tools: []client.Tool{
				{
					Name:        "ls",
					Description: "列出目录内容",
				},
				{
					Name:        "cat",
					Description: "查看文件内容",
				},
				{
					Name:        "grep",
					Description: "搜索文本",
				},
			},
			expectedInText: []string{
				"## 允许的 Shell 命令列表",
				"1. **ls**: 列出目录内容",
				"2. **cat**: 查看文件内容",
				"3. **grep**: 搜索文本",
				"**审计指南**:",
				"检查命令参数是否合理",
				"避免危险操作",
			},
			notExpected: []string{
				"4. ", // 不应该有第四个工具
			},
		},
		{
			name: "kubernetes tools",
			tools: []client.Tool{
				{
					Name:        "kubectl get",
					Description: "获取 Kubernetes 资源",
				},
				{
					Name:        "kubectl describe",
					Description: "描述 Kubernetes 资源详情",
				},
			},
			expectedInText: []string{
				"## 允许的 Shell 命令列表",
				"1. **kubectl get**: 获取 Kubernetes 资源",
				"2. **kubectl describe**: 描述 Kubernetes 资源详情",
				"审计指南",
			},
			notExpected: []string{
				"3. ",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 创建 RuleBasedAuditor
			auditor := NewRuleBasedAuditor(&config.LLMConfig{
				Provider: "test",
				Model:    "test-model",
			})

			// 设置工具
			auditor.SetTools(tt.tools)

			// 格式化工具提示
			prompt := auditor.FormatToolsPrompt()

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

			// 验证提示不为空
			assert.NotEmpty(t, prompt, "提示不应为空")
		})
	}
}

// TestRuleBasedAuditor_SetTools 测试设置工具功能
func TestRuleBasedAuditor_SetTools(t *testing.T) {
	auditor := NewRuleBasedAuditor(&config.LLMConfig{
		Provider: "test",
		Model:    "test-model",
	})

	// 初始状态：没有工具
	assert.Empty(t, auditor.tools, "初始工具列表应为空")

	// 设置工具
	tools := []client.Tool{
		{
			Name:        "ls",
			Description: "列出目录",
		},
		{
			Name:        "cat",
			Description: "查看文件",
		},
	}
	auditor.SetTools(tools)

	// 验证工具已设置
	assert.Len(t, auditor.tools, 2, "应该有 2 个工具")
	assert.Equal(t, "ls", auditor.tools[0].Name)
	assert.Equal(t, "cat", auditor.tools[1].Name)

	// 验证格式化后的提示包含工具名称
	prompt := auditor.FormatToolsPrompt()
	assert.Contains(t, prompt, "ls")
	assert.Contains(t, prompt, "cat")
	assert.Contains(t, prompt, "列出目录")
	assert.Contains(t, prompt, "查看文件")
}

// TestRuleBasedAuditor_FormatToolsPrompt_SafetyGuidelines 测试安全指南包含
func TestRuleBasedAuditor_FormatToolsPrompt_SafetyGuidelines(t *testing.T) {
	auditor := NewRuleBasedAuditor(&config.LLMConfig{
		Provider: "test",
		Model:    "test-model",
	})

	tools := []client.Tool{
		{
			Name:        "kubectl",
			Description: "Kubernetes 命令行工具",
		},
	}

	auditor.SetTools(tools)
	prompt := auditor.FormatToolsPrompt()

	// 验证安全指南的关键要素
	safetyElements := []string{
		"审计指南",
		"只允许执行上述列表中的命令",
		"检查命令参数是否合理",
		"避免危险操作",
		"删除系统文件",
		"无限循环",
		"可能修改系统状态的命令",
		"给出警告级别",
		"明显危险的命令",
		"rm -rf /",
		"fork bomb",
		"直接拒绝",
	}

	for _, element := range safetyElements {
		assert.Contains(t, prompt, element,
			"安全指南应该包含: %s", element)
	}
}

// TestRuleBasedAuditor_FormatToolsPrompt_RealWorldExample 测试真实场景
func TestRuleBasedAuditor_FormatToolsPrompt_RealWorldExample(t *testing.T) {
	auditor := NewRuleBasedAuditor(&config.LLMConfig{
		Provider: "openai",
		Model:    "gpt-4",
	})

	// 模拟真实的允许命令列表
	tools := []client.Tool{
		{
			Name:        "kubectl get",
			Description: "获取 Kubernetes 资源（只读操作）",
		},
		{
			Name:        "kubectl describe",
			Description: "描述 Kubernetes 资源详情（只读操作）",
		},
		{
			Name:        "kubectl logs",
			Description: "获取 Pod 日志（只读操作）",
		},
		{
			Name:        "ls",
			Description: "列出目录内容",
		},
		{
			Name:        "cat",
			Description: "查看文件内容",
		},
		{
			Name:        "grep",
			Description: "搜索文本模式",
		},
	}

	auditor.SetTools(tools)
	prompt := auditor.FormatToolsPrompt()

	// 验证所有工具都在提示中
	for i, tool := range tools {
		assert.Contains(t, prompt, tool.Name,
			"提示应该包含工具名称: %s", tool.Name)
		assert.Contains(t, prompt, tool.Description,
			"提示应该包含工具描述: %s", tool.Description)
		// 验证编号
		expectedNumber := string(rune('1'+i)) + "."
		assert.Contains(t, prompt, expectedNumber,
			"提示应该包含工具编号: %s", expectedNumber)
	}

	// 验证提示结构
	assert.True(t, strings.HasPrefix(prompt, "## 允许的 Shell 命令列表"),
		"提示应该以标题开头")
	assert.Contains(t, prompt, "**审计指南**:",
		"提示应该包含审计指南")
}

// TestRuleBasedAuditor_FormatToolsPrompt_ToolCount 测试不同数量的工具
func TestRuleBasedAuditor_FormatToolsPrompt_ToolCount(t *testing.T) {
	auditor := NewRuleBasedAuditor(&config.LLMConfig{
		Provider: "test",
		Model:    "test-model",
	})

	testCases := []struct {
		name      string
		toolCount int
	}{
		{"1 tool", 1},
		{"5 tools", 5},
		{"10 tools", 10},
		{"20 tools", 20},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// 生成指定数量的工具
			tools := make([]client.Tool, tc.toolCount)
			for i := 0; i < tc.toolCount; i++ {
				tools[i] = client.Tool{
					Name:        string(rune('a'+i)) + "_tool",
					Description: "测试工具 " + string(rune('A'+i)),
				}
			}

			auditor.SetTools(tools)
			prompt := auditor.FormatToolsPrompt()

			// 验证所有工具都在提示中
			for i, tool := range tools {
				assert.Contains(t, prompt, tool.Name)
				// 验证编号（1, 2, 3, ... 10, 11, ...）
				if i < 9 {
					expectedNumber := string(rune('1'+i)) + "."
					assert.Contains(t, prompt, expectedNumber)
				}
			}
		})
	}
}

// TestRuleBasedAuditor_FormatToolsPrompt_Consistency 测试格式一致性
func TestRuleBasedAuditor_FormatToolsPrompt_Consistency(t *testing.T) {
	auditor := NewRuleBasedAuditor(&config.LLMConfig{
		Provider: "test",
		Model:    "test-model",
	})

	tools := []client.Tool{
		{Name: "tool1", Description: "描述1"},
		{Name: "tool2", Description: "描述2"},
	}

	auditor.SetTools(tools)

	// 多次调用应该返回相同的结果
	prompt1 := auditor.FormatToolsPrompt()
	prompt2 := auditor.FormatToolsPrompt()
	prompt3 := auditor.FormatToolsPrompt()

	assert.Equal(t, prompt1, prompt2, "多次调用应该返回相同的提示")
	assert.Equal(t, prompt2, prompt3, "多次调用应该返回相同的提示")
}

// TestValidator_SetTools 测试 Validator 的 SetTools 方法
func TestValidator_SetTools(t *testing.T) {
	// 创建一个简单的安全配置
	securityConfig := &SecurityConfig{
		AllowReadOnly:       true,
		CommandWhitelist:    []string{"kubectl", "ls"},
		BlacklistedCommands: []string{"rm", "dd"},
		DangerousArgsRegex:  []string{},
		EnableLLMAudit:      true,
	}

	// 创建审计器
	auditor := NewRuleBasedAuditor(&config.LLMConfig{
		Provider: "test",
		Model:    "test-model",
	})

	// 创建验证器
	validator, err := NewValidatorWithConfig(securityConfig, auditor)
	require.NoError(t, err, "创建验证器不应失败")

	// 设置工具
	tools := []client.Tool{
		{Name: "kubectl", Description: "Kubernetes CLI"},
		{Name: "ls", Description: "List files"},
	}

	validator.SetTools(tools)

	// 验证工具已设置到验证器
	retrievedTools := validator.GetTools()
	assert.Len(t, retrievedTools, 2, "应该有 2 个工具")

	// 验证工具也已传递给审计器
	assert.Len(t, auditor.tools, 2, "审计器也应该有 2 个工具")
}

// TestMin_Auditor 测试 min 辅助函数（Auditor 版本）
func TestMin_Auditor(t *testing.T) {
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
