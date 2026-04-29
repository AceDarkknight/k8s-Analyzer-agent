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

func TestParsePods_WideFormat(t *testing.T) {
	stdout := `NAMESPACE NAME READY STATUS RESTARTS AGE IP NODE NOMINATED NODE READINESS GATES
default pod-wide-1 1/1 Running 1 (32d ago) 5m 10.1.2.3 node-1 <none> <none>`
	pods, err := parsePods(stdout, "default")
	if err != nil {
		t.Fatalf("parsePods wide failed: %v", err)
	}
	if len(pods) != 1 {
		t.Fatalf("expected 1 pod, got %d", len(pods))
	}
	p, ok := pods[0].(state.PodInfo)
	if !ok {
		t.Fatalf("expected PodInfo type, got %T", pods[0])
	}
	if p.Namespace != "default" {
		t.Errorf("expected namespace 'default', got %q", p.Namespace)
	}
	if p.Name != "pod-wide-1" {
		t.Errorf("expected name 'pod-wide-1', got %q", p.Name)
	}
	if p.Status != "Running" {
		t.Errorf("expected status 'Running', got %q", p.Status)
	}
	if p.Restarts != 1 {
		t.Errorf("expected restarts 1, got %d", p.Restarts)
	}
	if p.NodeName != "node-1" {
		t.Errorf("expected node 'node-1', got %q", p.NodeName)
	}
	if p.Age != "5m" {
		t.Errorf("expected age '5m', got %q", p.Age)
	}
}

func TestParseDeployments_WideFormat(t *testing.T) {
	stdout := `NAME READY UP-TO-DATE AVAILABLE AGE
nginx 1/1 1 1 5m`
	deps, err := parseDeployments(stdout)
	if err != nil {
		t.Fatalf("parseDeployments wide failed: %v", err)
	}
	if len(deps) != 1 {
		t.Fatalf("expected 1 deployment, got %d", len(deps))
	}
	d, ok := deps[0].(state.DeploymentInfo)
	if !ok {
		t.Fatalf("expected DeploymentInfo type, got %T", deps[0])
	}
	if d.Name != "nginx" {
		t.Errorf("expected name 'nginx', got %q", d.Name)
	}
	if d.Replicas != 1 {
		t.Errorf("expected replicas 1, got %d", d.Replicas)
	}
	if d.ReadyReplicas != 1 {
		t.Errorf("expected readyReplicas 1, got %d", d.ReadyReplicas)
	}
	if d.UpdatedReplicas != 1 {
		t.Errorf("expected updatedReplicas 1, got %d", d.UpdatedReplicas)
	}
	if d.AvailableReplicas != 1 {
		t.Errorf("expected availableReplicas 1, got %d", d.AvailableReplicas)
	}
}

func TestParseServices_WideFormat(t *testing.T) {
	stdout := `NAME TYPE CLUSTER-IP EXTERNAL-IP PORT(S) AGE
my-service ClusterIP 10.0.0.1 <none> 80/TCP 3m`
	svcs, err := parseServices(stdout)
	if err != nil {
		t.Fatalf("parseServices wide failed: %v", err)
	}
	if len(svcs) != 1 {
		t.Fatalf("expected 1 service, got %d", len(svcs))
	}
	s, ok := svcs[0].(state.ServiceInfo)
	if !ok {
		t.Fatalf("expected ServiceInfo type, got %T", svcs[0])
	}
	if s.Name != "my-service" {
		t.Errorf("expected name 'my-service', got %q", s.Name)
	}
	if s.Type != "ClusterIP" {
		t.Errorf("expected type 'ClusterIP', got %q", s.Type)
	}
	if s.ClusterIP != "10.0.0.1" {
		t.Errorf("expected clusterIP '10.0.0.1', got %q", s.ClusterIP)
	}
	if s.Ports != "80/TCP" {
		t.Errorf("expected ports '80/TCP', got %q", s.Ports)
	}
}

func TestApplyNamespaceToScopedResources(t *testing.T) {
	resources := []interface{}{
		state.DeploymentInfo{Name: "dep-1"},
		state.ServiceInfo{Name: "svc-1"},
		state.PodInfo{Name: "pod-1", Namespace: "default"},
	}

	result := applyNamespaceToScopedResources(resources, "kube-system")

	dep := result[0].(state.DeploymentInfo)
	if dep.Namespace != "kube-system" {
		t.Fatalf("expected deployment namespace kube-system, got %q", dep.Namespace)
	}

	svc := result[1].(state.ServiceInfo)
	if svc.Namespace != "kube-system" {
		t.Fatalf("expected service namespace kube-system, got %q", svc.Namespace)
	}

	pod := result[2].(state.PodInfo)
	if pod.Namespace != "default" {
		t.Fatalf("expected existing pod namespace to stay default, got %q", pod.Namespace)
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

func TestCalcNamespaceLimit(t *testing.T) {
	tests := []struct {
		name          string
		maxNamespaces int
		total         int
		expected      int
	}{
		{"config override", 20, 100, 20},
		{"total<=8 all scan", 0, 5, 5},
		{"total=8 boundary", 0, 8, 8},
		{"total=9 use 10", 0, 9, 10},
		{"total=15", 0, 15, 10},
		{"total=20 boundary", 0, 20, 10},
		{"total=21 use 15", 0, 21, 15},
		{"total=100", 0, 100, 15},
		{"total=0", 0, 0, 0},
		{"total=1", 0, 1, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := NewInfoNode(nil, tt.maxNamespaces)
			result := node.calcNamespaceLimit(tt.total)
			if result != tt.expected {
				t.Errorf("calcNamespaceLimit(%d) = %d, want %d", tt.total, result, tt.expected)
			}
		})
	}
}

func TestPrioritizeNamespaces(t *testing.T) {
	node := NewInfoNode(nil, 0)

	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "mixed namespaces",
			input:    []string{"kube-system", "default", "production", "kube-public", "staging"},
			expected: []string{"production", "staging", "default", "kube-system", "kube-public"},
		},
		{
			name:     "only system",
			input:    []string{"kube-system", "kube-public"},
			expected: []string{"kube-system", "kube-public"},
		},
		{
			name:     "only business",
			input:    []string{"app1", "app2"},
			expected: []string{"app1", "app2"},
		},
		{
			name:     "only default",
			input:    []string{"default"},
			expected: []string{"default"},
		},
		{
			name:     "empty",
			input:    []string{},
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := node.prioritizeNamespaces(tt.input)
			if len(result) != len(tt.expected) {
				t.Fatalf("length mismatch: got %d, want %d", len(result), len(tt.expected))
			}
			for i, ns := range result {
				if ns != tt.expected[i] {
					t.Errorf("index %d: got %q, want %q", i, ns, tt.expected[i])
				}
			}
		})
	}
}
