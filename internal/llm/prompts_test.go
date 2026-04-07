package llm

import (
	"strings"
	"testing"

	"github.com/AceDarkknight/k8s-analyzer-agent/internal/state"
)

func TestBuildDecisionPrompt_ToolSummaryBlock(t *testing.T) {
	s := state.NewState("test query", 10, 4)
	s.SetK8sInfo(&state.K8sInfo{
		Namespaces: []string{"default"},
		Resources:  map[string][]interface{}{"Pods": {}},
	})

	// 无命令执行时不应包含工具摘要表
	prompt := BuildDecisionPrompt(s)
	if strings.Contains(prompt, "已执行工具摘要") {
		t.Error("should not contain tool summary when no commands executed")
	}

	// 添加命令执行记录
	s.AddCommandExecution("kubectl get pods", true, "output1", false)
	s.AddCommandExecution("kubectl describe pod nginx", false, "error", false)

	prompt = BuildDecisionPrompt(s)
	if !strings.Contains(prompt, "已执行工具摘要") {
		t.Error("should contain tool summary header")
	}
	if !strings.Contains(prompt, "kubectl get pods") {
		t.Error("should contain executed command")
	}
	if !strings.Contains(prompt, "✓") {
		t.Error("should contain success marker")
	}
	if !strings.Contains(prompt, "✗") {
		t.Error("should contain failure marker")
	}
}

func TestBuildDecisionPrompt_ObservationTruncation(t *testing.T) {
	s := state.NewState("test query", 10, 4)
	s.SetK8sInfo(&state.K8sInfo{
		Namespaces: []string{"default"},
		Resources:  map[string][]interface{}{"Pods": {}},
	})

	// 添加一个带长 observation 的 step
	longObs := strings.Repeat("x", 1000)
	s.AddReasoningStep(state.ReasoningStep{
		Iteration:   1,
		Thought:     "test thought",
		Decision:    "continue",
		Observation: longObs,
	})

	prompt := BuildDecisionPrompt(s)
	// 确保 observation 被截断到 800 而不是 200
	// prompt 中应包含 "..." 表示截断
	if !strings.Contains(prompt, "...") {
		t.Error("long observation should be truncated")
	}
	// 不应包含完整的 1000 字符
	if strings.Contains(prompt, longObs) {
		t.Error("observation should not be fully included when > 800 chars")
	}
}

func TestBuildDecisionPrompt_NilState(t *testing.T) {
	prompt := BuildDecisionPrompt(nil)
	if prompt != "" {
		t.Errorf("expected empty string for nil state, got %q", prompt)
	}
}

func TestBuildDecisionPrompt_BasicContent(t *testing.T) {
	s := state.NewState("check pods in default namespace", 10, 4)
	s.SetK8sInfo(&state.K8sInfo{
		Namespaces: []string{"default"},
		Resources: map[string][]interface{}{
			"Pods": {
				state.PodInfo{Name: "nginx", Namespace: "default", Status: "Pending", Restarts: 0},
			},
		},
	})

	prompt := BuildDecisionPrompt(s)

	// 应包含用户查询
	if !strings.Contains(prompt, "check pods in default namespace") {
		t.Error("prompt should contain user query")
	}
	// 应包含异常 Pod
	if !strings.Contains(prompt, "nginx") {
		t.Error("prompt should contain abnormal pod name")
	}
	// 应包含工具列表
	if !strings.Contains(prompt, "list_pods") {
		t.Error("prompt should contain tools list")
	}
	// 应包含迭代进度
	if !strings.Contains(prompt, "第 0/10 轮") {
		t.Error("prompt should contain iteration progress")
	}
}
