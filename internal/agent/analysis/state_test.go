// Package analysis 提供分析 Agent 的单元测试
package analysis

import (
	"testing"
	"time"
)

// TestNewState_ReasoningHistory 测试 NewState 初始化推理历史
func TestNewState_ReasoningHistory(t *testing.T) {
	state := NewState("test query")

	// 验证 ReasoningHistory 已正确初始化
	if state.ReasoningHistory == nil {
		t.Error("ReasoningHistory should be initialized, got nil")
	}

	if len(state.ReasoningHistory) != 0 {
		t.Errorf("ReasoningHistory should be empty initially, got %d elements", len(state.ReasoningHistory))
	}
}

// TestAddReasoningStep 测试添加推理步骤
func TestAddReasoningStep(t *testing.T) {
	state := NewState("test query")

	// 添加第一个推理步骤
	toolCalls := []ToolCall{
		{
			Tool: "list_pods",
			Args: map[string]interface{}{
				"namespace": "default",
			},
			Type: "k8s",
		},
	}

	state.AddReasoningStep("分析当前状态，需要获取 Pod 列表", "continue", toolCalls)

	// 验证步骤已添加到历史
	if len(state.ReasoningHistory) != 1 {
		t.Errorf("Expected 1 reasoning step, got %d", len(state.ReasoningHistory))
	}

	// 验证步骤字段
	step := state.ReasoningHistory[0]
	if step.Iteration != 1 {
		t.Errorf("Expected Iteration=1, got %d", step.Iteration)
	}

	if step.Thought != "分析当前状态，需要获取 Pod 列表" {
		t.Errorf("Expected Thought='分析当前状态，需要获取 Pod 列表', got '%s'", step.Thought)
	}

	if step.Decision != "continue" {
		t.Errorf("Expected Decision='continue', got '%s'", step.Decision)
	}

	if len(step.ToolCalls) != 1 {
		t.Errorf("Expected 1 tool call, got %d", len(step.ToolCalls))
	}

	if step.ToolCalls[0].Tool != "list_pods" {
		t.Errorf("Expected Tool='list_pods', got '%s'", step.ToolCalls[0].Tool)
	}

	// 验证时间戳已设置
	if step.Timestamp.IsZero() {
		t.Error("Timestamp should be set")
	}
}

// TestAddReasoningStep_Multiple 测试添加多个推理步骤
func TestAddReasoningStep_Multiple(t *testing.T) {
	state := NewState("test query")

	// 添加多个推理步骤
	state.AddReasoningStep("思考1", "continue", nil)
	state.AddReasoningStep("思考2", "continue", nil)
	state.AddReasoningStep("思考3", "report", nil)

	if len(state.ReasoningHistory) != 3 {
		t.Errorf("Expected 3 reasoning steps, got %d", len(state.ReasoningHistory))
	}

	// 验证迭代次数递增
	if state.ReasoningHistory[0].Iteration != 1 {
		t.Errorf("First step should have Iteration=1, got %d", state.ReasoningHistory[0].Iteration)
	}

	if state.ReasoningHistory[1].Iteration != 2 {
		t.Errorf("Second step should have Iteration=2, got %d", state.ReasoningHistory[1].Iteration)
	}

	if state.ReasoningHistory[2].Iteration != 3 {
		t.Errorf("Third step should have Iteration=3, got %d", state.ReasoningHistory[2].Iteration)
	}
}

// TestUpdateLastStepObservation 测试更新最后步骤的观察结果
func TestUpdateLastStepObservation(t *testing.T) {
	state := NewState("test query")

	// 添加推理步骤
	state.AddReasoningStep("思考", "continue", nil)

	// 初始 Observation 为空
	if state.ReasoningHistory[0].Observation != "" {
		t.Errorf("Initial Observation should be empty, got '%s'", state.ReasoningHistory[0].Observation)
	}

	// 更新观察结果
	observation := "获取到 5 个 Pod，其中 2 个处于 Error 状态"
	state.UpdateLastStepObservation(observation)

	// 验证观察结果已更新
	if state.ReasoningHistory[0].Observation != observation {
		t.Errorf("Expected Observation='%s', got '%s'", observation, state.ReasoningHistory[0].Observation)
	}
}

// TestUpdateLastStepObservation_EmptyHistory 测试空历史时的更新
func TestUpdateLastStepObservation_EmptyHistory(t *testing.T) {
	state := NewState("test query")

	// 尝试更新空历史的观察结果（不应该 panic）
	state.UpdateLastStepObservation("test")

	// 验证历史仍为空
	if len(state.ReasoningHistory) != 0 {
		t.Errorf("ReasoningHistory should remain empty, got %d elements", len(state.ReasoningHistory))
	}
}

// TestGetLastReasoningStep 测试获取最后推理步骤
func TestGetLastReasoningStep(t *testing.T) {
	state := NewState("test query")

	// 空历史时返回 nil
	lastStep := state.GetLastReasoningStep()
	if lastStep != nil {
		t.Error("GetLastReasoningStep should return nil for empty history")
	}

	// 添加步骤后获取最后步骤
	state.AddReasoningStep("思考1", "continue", nil)
	state.AddReasoningStep("思考2", "report", nil)

	lastStep = state.GetLastReasoningStep()
	if lastStep == nil {
		t.Fatal("GetLastReasoningStep should not return nil")
	}

	// 验证返回的是最后一步
	if lastStep.Thought != "思考2" {
		t.Errorf("Expected last step Thought='思考2', got '%s'", lastStep.Thought)
	}

	if lastStep.Decision != "report" {
		t.Errorf("Expected last step Decision='report', got '%s'", lastStep.Decision)
	}
}

// TestReasoningStep_ToolCallsNil 测试 ToolCalls 为 nil 的情况
func TestReasoningStep_ToolCallsNil(t *testing.T) {
	state := NewState("test query")

	// 添加不带工具调用的推理步骤
	state.AddReasoningStep("思考", "report", nil)

	if len(state.ReasoningHistory) != 1 {
		t.Errorf("Expected 1 reasoning step, got %d", len(state.ReasoningHistory))
	}

	if state.ReasoningHistory[0].ToolCalls != nil {
		t.Error("ToolCalls should be nil when not provided")
	}
}

// TestReasoningStep_Timestamp 测试时间戳设置
func TestReasoningStep_Timestamp(t *testing.T) {
	state := NewState("test query")

	before := time.Now()
	state.AddReasoningStep("思考", "continue", nil)
	after := time.Now()

	step := state.ReasoningHistory[0]

	// 验证时间戳在合理范围内
	if step.Timestamp.Before(before) || step.Timestamp.After(after) {
		t.Error("Timestamp should be within the expected range")
	}
}
