package diagnosis

import (
	"testing"

	"github.com/AceDarkknight/k8s-analyzer-agent/internal/state"
)

func TestToolMapping(t *testing.T) {
	// 验证 toolMapping 是否正确配置
	tests := []struct {
		toolName         string
		expectedVerb     string
		expectedResource string
		shouldExist      bool
	}{
		{"list_pods", "get", "pods", true},
		{"describe_pod", "describe", "pod", true},
		{"get_pod_logs", "logs", "", true},
		{"list_events", "get", "events", true},
		{"list_deployments", "get", "deployments", true},
		{"list_services", "get", "services", true},
		{"get_nodes", "get", "nodes", true},
		{"list_namespaces", "get", "namespaces", true},
		{"unknown_tool", "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.toolName, func(t *testing.T) {
			mapping, exists := toolMapping[tt.toolName]
			if exists != tt.shouldExist {
				t.Errorf("tool %q: expected exists=%v, got %v", tt.toolName, tt.shouldExist, exists)
				return
			}
			if !tt.shouldExist {
				return
			}
			if mapping.Verb != tt.expectedVerb {
				t.Errorf("tool %q: expected verb %q, got %q", tt.toolName, tt.expectedVerb, mapping.Verb)
			}
			if mapping.Resource != tt.expectedResource {
				t.Errorf("tool %q: expected resource %q, got %q", tt.toolName, tt.expectedResource, mapping.Resource)
			}
		})
	}
}

func TestToolMappingCompleteness(t *testing.T) {
	// 确保所有需要的工具都在映射中
	requiredTools := []string{
		"list_pods",
		"describe_pod",
		"get_pod_logs",
		"list_events",
		"list_deployments",
		"list_services",
		"get_nodes",
		"list_namespaces",
	}

	for _, tool := range requiredTools {
		if _, exists := toolMapping[tool]; !exists {
			t.Errorf("required tool %q is missing from toolMapping", tool)
		}
	}
}

func TestBuildCacheKey(t *testing.T) {
	tc1 := state.ToolCall{
		Name: "list_pods",
		Args: map[string]interface{}{"namespace": "default"},
	}
	tc2 := state.ToolCall{
		Name: "list_pods",
		Args: map[string]interface{}{"namespace": "default"},
	}
	tc3 := state.ToolCall{
		Name: "list_pods",
		Args: map[string]interface{}{"namespace": "kube-system"},
	}
	tc4 := state.ToolCall{
		Name: "describe_pod",
		Args: map[string]interface{}{"namespace": "default"},
	}

	// 相同工具+参数 → 相同 key
	key1 := buildCacheKey(tc1)
	key2 := buildCacheKey(tc2)
	if key1 != key2 {
		t.Errorf("same tool call should produce same key: %q vs %q", key1, key2)
	}

	// 不同参数 → 不同 key
	key3 := buildCacheKey(tc3)
	if key1 == key3 {
		t.Error("different args should produce different keys")
	}

	// 不同工具 → 不同 key
	key4 := buildCacheKey(tc4)
	if key1 == key4 {
		t.Error("different tool names should produce different keys")
	}

	// 空参数也应该正常
	tc5 := state.ToolCall{Name: "get_nodes", Args: nil}
	key5 := buildCacheKey(tc5)
	if key5 == "" {
		t.Error("cache key should not be empty")
	}
}

func TestSortedArgs(t *testing.T) {
	// nil args
	result := sortedArgs(nil)
	if result != nil {
		t.Error("nil args should return nil")
	}

	// empty args
	result = sortedArgs(map[string]interface{}{})
	if len(result) != 0 {
		t.Error("empty args should return empty map")
	}

	// 有值的 args 应该有相同的内容
	args := map[string]interface{}{"z": "1", "a": "2", "m": "3"}
	result = sortedArgs(args)
	if len(result) != 3 {
		t.Errorf("expected 3 entries, got %d", len(result))
	}
	if result["z"] != "1" || result["a"] != "2" || result["m"] != "3" {
		t.Error("sorted args should preserve values")
	}
}

func TestBuildCmdStr(t *testing.T) {
	tests := []struct {
		name     string
		tc       state.ToolCall
		expected string
	}{
		{
			name:     "list_pods with namespace",
			tc:       state.ToolCall{Name: "list_pods", Args: map[string]interface{}{"namespace": "default"}},
			expected: "kubectl -n default get pods",
		},
		{
			name:     "describe_pod with namespace and name",
			tc:       state.ToolCall{Name: "describe_pod", Args: map[string]interface{}{"namespace": "prod", "name": "nginx"}},
			expected: "kubectl -n prod describe pod nginx",
		},
		{
			name:     "get_nodes no args",
			tc:       state.ToolCall{Name: "get_nodes", Args: map[string]interface{}{}},
			expected: "kubectl get nodes",
		},
		{
			name:     "unknown tool",
			tc:       state.ToolCall{Name: "unknown", Args: map[string]interface{}{}},
			expected: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildCmdStr(tt.tc)
			if result != tt.expected {
				t.Errorf("buildCmdStr() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestDefaultOutputForVerb(t *testing.T) {
	tests := []struct {
		name     string
		verb     string
		expected string
	}{
		{
			name:     "get uses json output",
			verb:     "get",
			expected: "json",
		},
		{
			name:     "describe omits output",
			verb:     "describe",
			expected: "",
		},
		{
			name:     "logs omits output",
			verb:     "logs",
			expected: "",
		},
		{
			name:     "empty verb omits output",
			verb:     "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := defaultOutputForVerb(tt.verb); got != tt.expected {
				t.Errorf("defaultOutputForVerb(%q) = %q, want %q", tt.verb, got, tt.expected)
			}
		})
	}
}

func TestEnsureCommandObservation(t *testing.T) {
	tests := []struct {
		name    string
		summary string
		success bool
		want    string
	}{
		{
			name:    "preserve non-empty summary",
			summary: "Mem: 1Gi used",
			success: true,
			want:    "Mem: 1Gi used",
		},
		{
			name:    "success fallback when empty",
			summary: "   ",
			success: true,
			want:    "命令已执行成功，但未返回可展示的文本输出",
		},
		{
			name:    "failure fallback when empty",
			summary: "",
			success: false,
			want:    "命令执行失败，但未返回可展示的文本输出",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ensureCommandObservation(tt.summary, tt.success); got != tt.want {
				t.Fatalf("ensureCommandObservation() = %q, want %q", got, tt.want)
			}
		})
	}
}
