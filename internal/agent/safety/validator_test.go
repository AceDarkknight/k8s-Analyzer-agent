// Package safety 测试命令安全验证功能
package safety

import (
	"testing"

	"github.com/AceDarkknight/k8s-analyzer-agent/internal/client"
	"github.com/AceDarkknight/k8s-analyzer-agent/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestValidator_SetTools 测试 Validator 的 SetTools 方法
func TestValidator_SetTools(t *testing.T) {
	// 创建一个简单的安全配置（无白名单）
	securityConfig := &SecurityConfig{
		AllowReadOnly:       true,
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
