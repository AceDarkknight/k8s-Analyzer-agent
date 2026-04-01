package state

import (
	"fmt"
	"strings"
	"time"
)

// K8sInfo 集群信息
type K8sInfo struct {
	Namespaces []string
	Resources  map[string][]interface{} // "Pods" → []PodInfo, "Deployments" → []DeploymentInfo
}

// GetSummary 返回资源概况摘要字符串
func (k *K8sInfo) GetSummary() string {
	if k == nil {
		return "K8sInfo: nil"
	}

	var parts []string

	// 命名空间
	if len(k.Namespaces) > 0 {
		parts = append(parts, fmt.Sprintf("命名空间: [%s]", strings.Join(k.Namespaces, ", ")))
	} else {
		parts = append(parts, "命名空间: []")
	}

	// Pods
	pods := k.GetAbnormalPods()
	podCount := 0
	if podsList, ok := k.Resources["Pods"]; ok {
		podCount = len(podsList)
	}
	parts = append(parts, fmt.Sprintf("Pods: %d 个 (%d 个异常)", podCount, len(pods)))

	// Deployments
	deploymentCount := 0
	if deploymentList, ok := k.Resources["Deployments"]; ok {
		deploymentCount = len(deploymentList)
	}
	parts = append(parts, fmt.Sprintf("Deployments: %d 个", deploymentCount))

	// Services
	serviceCount := 0
	if serviceList, ok := k.Resources["Services"]; ok {
		serviceCount = len(serviceList)
	}
	parts = append(parts, fmt.Sprintf("Services: %d 个", serviceCount))

	return strings.Join(parts, "\n")
}

// GetAbnormalPods 返回异常 Pod 列表（状态非 Running/Succeeded）
func (k *K8sInfo) GetAbnormalPods() []PodInfo {
	if k == nil || k.Resources == nil {
		return nil
	}

	podsList, ok := k.Resources["Pods"]
	if !ok {
		return nil
	}

	var abnormalPods []PodInfo
	for _, p := range podsList {
		if pod, ok := p.(PodInfo); ok {
			if pod.Status != "Running" && pod.Status != "Succeeded" {
				abnormalPods = append(abnormalPods, pod)
			}
		}
	}

	return abnormalPods
}

type PodInfo struct {
	Name      string
	Namespace string
	Status    string
	NodeName  string
	Restarts  int32
	Labels    map[string]string
	Age       string
}

type DeploymentInfo struct {
	Name              string
	Namespace         string
	Replicas          int32
	ReadyReplicas     int32
	UpdatedReplicas   int32
	AvailableReplicas int32
}

type ServiceInfo struct {
	Name      string
	Namespace string
	Type      string
	ClusterIP string
	Ports     string
}

// Finding 诊断发现
type Finding struct {
	Severity  string // critical / warning / info
	Resource  string
	Message   string
	Evidence  string
	Timestamp time.Time
	Verified  bool
}

// ToolCall 工具调用
type ToolCall struct {
	Name string                 `json:"name"`
	Args map[string]interface{} `json:"args"`
}

// ReasoningStep 推理步骤
type ReasoningStep struct {
	Iteration      int
	Timestamp      time.Time
	Thought        string
	Decision       string // continue / deep_query / report
	DeepQueryTopic string // deep_query 模式的调查主题
	ToolCalls      []ToolCall
	Observation    string
	Duration       time.Duration
	TokensUsed     int
}

// AnalysisResult 分析结果
type AnalysisResult struct {
	Summary         string
	Severity        string // critical / warning / info
	RootCause       string
	Findings        []Finding
	Recommendations []Recommendation
	Limitations     string
	Status          string // completed / partial
}

// Recommendation 修复建议
type Recommendation struct {
	Priority     string // high / medium / low
	Action       string
	Command      string
	Risk         string
	Verified     bool   // 是否已被验证迭代覆盖
	VerifyResult string // 验证执行结果摘要（截取前 200 字）
}

// BlockedCommand 被安全审计拒绝的命令
type BlockedCommand struct {
	Command string
	Reason  string
	Advice  string
}

// CommandExecution 命令执行记录
type CommandExecution struct {
	Command       string
	Success       bool
	Output        string
	Timestamp     time.Time
	IsVerifyPhase bool // 是否属于验证迭代阶段
}
