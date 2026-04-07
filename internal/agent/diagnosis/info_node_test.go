package diagnosis

import (
	"testing"

	"github.com/AceDarkknight/k8s-analyzer-agent/internal/state"
)

func TestExtractSpecifiedNamespace(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty input",
			input:    "",
			expected: "",
		},
		{
			name:     "namespace keyword",
			input:    "check pods in namespace: kube-system",
			expected: "kube-system",
		},
		{
			name:     "namespace with chinese colon",
			input:    "check pods in namespace：default",
			expected: "default",
		},
		{
			name:     "chinese namespace keyword",
			input:    "查看命名空间 default 的 pod",
			expected: "default",
		},
		{
			name:     "ns shorthand",
			input:    "check pods in ns: production",
			expected: "production",
		},
		{
			name:     "no namespace specified",
			input:    "check all pods",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractSpecifiedNamespace(tt.input)
			if result != tt.expected {
				t.Errorf("extractSpecifiedNamespace(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestParseNamespaces(t *testing.T) {
	jsonData := `{
		"items": [
			{"metadata": {"name": "default"}},
			{"metadata": {"name": "kube-system"}},
			{"metadata": {"name": "kube-public"}}
		]
	}`

	namespaces, err := parseNamespaces(jsonData)
	if err != nil {
		t.Fatalf("parseNamespaces failed: %v", err)
	}

	if len(namespaces) != 3 {
		t.Errorf("expected 3 namespaces, got %d", len(namespaces))
	}

	expected := []string{"default", "kube-system", "kube-public"}
	for i, ns := range expected {
		if namespaces[i] != ns {
			t.Errorf("expected namespace %q at index %d, got %q", ns, i, namespaces[i])
		}
	}
}

func TestParsePods(t *testing.T) {
	jsonData := `{
		"items": [
			{
				"metadata": {
					"name": "nginx-pod",
					"namespace": "default",
					"labels": {"app": "nginx"}
				},
				"status": {
					"phase": "Running",
					"containerStatuses": [
						{"restartCount": 2},
						{"restartCount": 1}
					]
				},
				"spec": {
					"nodeName": "node1"
				}
			}
		]
	}`

	pods, err := parsePods(jsonData, "default")
	if err != nil {
		t.Fatalf("parsePods failed: %v", err)
	}

	if len(pods) != 1 {
		t.Fatalf("expected 1 pod, got %d", len(pods))
	}

	pod, ok := pods[0].(state.PodInfo)
	if !ok {
		t.Fatalf("expected PodInfo type, got %T", pods[0])
	}

	if pod.Name != "nginx-pod" {
		t.Errorf("expected name 'nginx-pod', got %q", pod.Name)
	}
	if pod.Namespace != "default" {
		t.Errorf("expected namespace 'default', got %q", pod.Namespace)
	}
	if pod.Status != "Running" {
		t.Errorf("expected status 'Running', got %q", pod.Status)
	}
	if pod.NodeName != "node1" {
		t.Errorf("expected nodeName 'node1', got %q", pod.NodeName)
	}
	if pod.Restarts != 3 {
		t.Errorf("expected restarts 3, got %d", pod.Restarts)
	}
	if pod.Labels["app"] != "nginx" {
		t.Errorf("expected label app=nginx, got %q", pod.Labels["app"])
	}
}

func TestParseDeployments(t *testing.T) {
	jsonData := `{
		"items": [
			{
				"metadata": {
					"name": "nginx-deployment",
					"namespace": "default"
				},
				"spec": {
					"replicas": 3
				},
				"status": {
					"readyReplicas": 2,
					"updatedReplicas": 3,
					"availableReplicas": 2
				}
			}
		]
	}`

	deployments, err := parseDeployments(jsonData)
	if err != nil {
		t.Fatalf("parseDeployments failed: %v", err)
	}

	if len(deployments) != 1 {
		t.Fatalf("expected 1 deployment, got %d", len(deployments))
	}

	dep, ok := deployments[0].(state.DeploymentInfo)
	if !ok {
		t.Fatalf("expected DeploymentInfo type, got %T", deployments[0])
	}

	if dep.Name != "nginx-deployment" {
		t.Errorf("expected name 'nginx-deployment', got %q", dep.Name)
	}
	if dep.Namespace != "default" {
		t.Errorf("expected namespace 'default', got %q", dep.Namespace)
	}
	if dep.Replicas != 3 {
		t.Errorf("expected replicas 3, got %d", dep.Replicas)
	}
	if dep.ReadyReplicas != 2 {
		t.Errorf("expected readyReplicas 2, got %d", dep.ReadyReplicas)
	}
	if dep.UpdatedReplicas != 3 {
		t.Errorf("expected updatedReplicas 3, got %d", dep.UpdatedReplicas)
	}
	if dep.AvailableReplicas != 2 {
		t.Errorf("expected availableReplicas 2, got %d", dep.AvailableReplicas)
	}
}
