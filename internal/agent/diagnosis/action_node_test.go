package diagnosis

import (
	"testing"
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
