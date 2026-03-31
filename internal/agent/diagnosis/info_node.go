package diagnosis

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/AceDarkknight/k8s-analyzer-agent/internal/client/gateway"
	"github.com/AceDarkknight/k8s-analyzer-agent/internal/logger"
	"github.com/AceDarkknight/k8s-analyzer-agent/internal/state"
)

// InfoNode 信息收集节点
type InfoNode struct {
	gateway *gateway.GatewayClient
}

// NewInfoNode 创建新的信息收集节点
func NewInfoNode(gw *gateway.GatewayClient) *InfoNode {
	return &InfoNode{
		gateway: gw,
	}
}

// Execute 执行信息收集
func (n *InfoNode) Execute(ctx context.Context, s *state.State) (*state.State, error) {
	logger.Info("InfoNode: starting information collection")

	// 1. 获取命名空间列表
	resp, err := n.gateway.ListNamespaces(ctx)
	if err != nil {
		logger.Warn("InfoNode: failed to list namespaces", logger.Err(err))
		// 不终止流程，继续执行
		s.SetK8sInfo(&state.K8sInfo{
			Namespaces: []string{},
			Resources:  make(map[string][]interface{}),
		})
		return s, nil
	}

	// 调试：记录原始响应
	stdoutPreview := resp.Stdout
	if len(stdoutPreview) > 200 {
		stdoutPreview = stdoutPreview[:200] + "..."
	}
	logger.Info("InfoNode: received gateway response",
		logger.String("stdout_preview", stdoutPreview),
		logger.Int("stdout_length", len(resp.Stdout)))

	// 解析命名空间列表
	namespaces, err := parseNamespaces(resp.Stdout)
	if err != nil {
		logger.Warn("InfoNode: failed to parse namespaces", logger.Err(err))
		namespaces = []string{}
	}

	// 2. 检查用户输入是否指定了命名空间
	specifiedNS := extractSpecifiedNamespace(s.UserInput)

	// 3. 确定要查询的命名空间列表
	var targetNamespaces []string
	if specifiedNS != "" {
		// 用户指定了命名空间，检查是否存在
		found := false
		for _, ns := range namespaces {
			if ns == specifiedNS {
				found = true
				break
			}
		}
		if found {
			targetNamespaces = []string{specifiedNS}
		} else {
			logger.Warn("InfoNode: specified namespace not found", logger.String("namespace", specifiedNS))
			targetNamespaces = []string{"default"}
		}
	} else {
		// 使用全部命名空间，但限制最多5个
		targetNamespaces = namespaces
		if len(targetNamespaces) > 5 {
			targetNamespaces = targetNamespaces[:5]
			logger.Info("InfoNode: limiting namespaces to 5", logger.Int("total", len(namespaces)))
		}
	}

	// 4. 初始化 K8sInfo
	k8sInfo := &state.K8sInfo{
		Namespaces: targetNamespaces,
		Resources:  make(map[string][]interface{}),
	}

	// 5. 遍历命名空间收集 Pod 和 Deployment 信息
	for _, ns := range targetNamespaces {
		// 获取 Pod 列表
		if err := n.collectPods(ctx, k8sInfo, ns); err != nil {
			logger.Warn("InfoNode: failed to collect pods",
				logger.String("namespace", ns),
				logger.Err(err))
		}

		// 获取 Deployment 列表
		if err := n.collectDeployments(ctx, k8sInfo, ns); err != nil {
			logger.Warn("InfoNode: failed to collect deployments",
				logger.String("namespace", ns),
				logger.Err(err))
		}
	}

	// 6. 更新 state
	s.SetK8sInfo(k8sInfo)

	logger.Info("InfoNode: information collection completed",
		logger.Int("namespaces", len(targetNamespaces)),
		logger.Int("pods", len(k8sInfo.Resources["Pods"])),
		logger.Int("deployments", len(k8sInfo.Resources["Deployments"])))

	return s, nil
}

// collectPods 收集 Pod 信息
func (n *InfoNode) collectPods(ctx context.Context, k8sInfo *state.K8sInfo, ns string) error {
	resp, err := n.gateway.ListPods(ctx, ns, "")
	if err != nil {
		return err
	}

	pods, err := parsePods(resp.Stdout, ns)
	if err != nil {
		return err
	}

	k8sInfo.Resources["Pods"] = append(k8sInfo.Resources["Pods"], pods...)
	return nil
}

// collectDeployments 收集 Deployment 信息
func (n *InfoNode) collectDeployments(ctx context.Context, k8sInfo *state.K8sInfo, ns string) error {
	resp, err := n.gateway.ListDeployments(ctx, ns)
	if err != nil {
		return err
	}

	deployments, err := parseDeployments(resp.Stdout)
	if err != nil {
		return err
	}

	k8sInfo.Resources["Deployments"] = append(k8sInfo.Resources["Deployments"], deployments...)
	return nil
}

// extractSpecifiedNamespace 从用户输入中提取指定的命名空间
func extractSpecifiedNamespace(input string) string {
	input = strings.ToLower(input)

	// 匹配 "namespace:" 或 "命名空间" 关键词
	patterns := []string{"namespace:", "namespace：", "命名空间", "ns:"}
	for _, pattern := range patterns {
		idx := strings.Index(input, pattern)
		if idx != -1 {
			// 提取后面的内容
			start := idx + len(pattern)
			rest := strings.TrimSpace(input[start:])
			// 提取第一个单词作为命名空间名
			parts := strings.Fields(rest)
			if len(parts) > 0 {
				return parts[0]
			}
		}
	}

	return ""
}

// parseNamespaces 解析命名空间列表
func parseNamespaces(stdout string) ([]string, error) {
	stdout = strings.TrimSpace(stdout)
	if stdout == "" {
		return []string{}, nil
	}

	// 尝试 JSON 格式解析
	var jsonResult struct {
		Items []struct {
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(stdout), &jsonResult); err == nil {
		namespaces := make([]string, 0, len(jsonResult.Items))
		for _, item := range jsonResult.Items {
			if item.Metadata.Name != "" {
				namespaces = append(namespaces, item.Metadata.Name)
			}
		}
		return namespaces, nil
	}

	// JSON 解析失败，尝试表格格式解析
	return parseTableFormat(stdout, "NAME")
}

// parseTableFormat 解析 kubectl 表格格式输出
func parseTableFormat(stdout string, headerKeyword string) ([]string, error) {
	lines := strings.Split(stdout, "\n")
	var names []string
	var headerFound bool

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// 跳过表头行
		if strings.Contains(line, headerKeyword) && !headerFound {
			headerFound = true
			continue
		}

		// 解析数据行，第一列是名称
		if headerFound {
			fields := strings.Fields(line)
			if len(fields) > 0 && fields[0] != "" {
				names = append(names, fields[0])
			}
		}
	}

	if len(names) == 0 && !headerFound {
		return nil, fmt.Errorf("unable to parse table format: header not found")
	}

	return names, nil
}

// parsePods 解析 Pod 列表（支持 JSON 和表格格式）
func parsePods(stdout string, namespace string) ([]interface{}, error) {
	stdout = strings.TrimSpace(stdout)
	if stdout == "" {
		return []interface{}{}, nil
	}

	// 尝试 JSON 格式解析
	var jsonResult struct {
		Items []struct {
			Metadata struct {
				Name      string            `json:"name"`
				Namespace string            `json:"namespace"`
				Labels    map[string]string `json:"labels"`
			} `json:"metadata"`
			Status struct {
				Phase             string `json:"phase"`
				ContainerStatuses []struct {
					RestartCount int32 `json:"restartCount"`
				} `json:"containerStatuses"`
			} `json:"status"`
			Spec struct {
				NodeName string `json:"nodeName"`
			} `json:"spec"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(stdout), &jsonResult); err == nil {
		pods := make([]interface{}, 0, len(jsonResult.Items))
		for _, item := range jsonResult.Items {
			// 计算总重启次数
			var totalRestarts int32
			for _, cs := range item.Status.ContainerStatuses {
				totalRestarts += cs.RestartCount
			}

			pod := state.PodInfo{
				Name:      item.Metadata.Name,
				Namespace: item.Metadata.Namespace,
				Status:    item.Status.Phase,
				NodeName:  item.Spec.NodeName,
				Restarts:  totalRestarts,
				Labels:    item.Metadata.Labels,
			}
			pods = append(pods, pod)
		}
		return pods, nil
	}

	// JSON 解析失败，尝试表格格式解析
	return parsePodTableFormat(stdout, namespace)
}

// parsePodTableFormat 解析 Pod 表格格式
func parsePodTableFormat(stdout string, namespace string) ([]interface{}, error) {
	lines := strings.Split(stdout, "\n")
	var pods []interface{}
	var headerFound bool

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// 跳过表头行
		if strings.Contains(line, "NAME") && strings.Contains(line, "STATUS") && !headerFound {
			headerFound = true
			continue
		}

		// 解析数据行
		if headerFound {
			fields := strings.Fields(line)
			if len(fields) >= 1 && fields[0] != "" {
				pod := state.PodInfo{
					Name:      fields[0],
					Namespace: namespace,
				}
				// 尝试提取状态（通常在第3列）
				if len(fields) >= 3 {
					pod.Status = fields[2]
				} else {
					pod.Status = "Unknown"
				}
				// 尝试提取重启次数（通常在第4列）
				if len(fields) >= 4 {
					restartStr := fields[3]
					var restarts int32
					fmt.Sscanf(restartStr, "%d", &restarts)
					pod.Restarts = restarts
				}
				pods = append(pods, pod)
			}
		}
	}

	return pods, nil
}

// parseDeployments 解析 Deployment 列表（支持 JSON 和表格格式）
func parseDeployments(stdout string) ([]interface{}, error) {
	stdout = strings.TrimSpace(stdout)
	if stdout == "" {
		return []interface{}{}, nil
	}

	// 尝试 JSON 格式解析
	var jsonResult struct {
		Items []struct {
			Metadata struct {
				Name      string `json:"name"`
				Namespace string `json:"namespace"`
			} `json:"metadata"`
			Spec struct {
				Replicas int32 `json:"replicas"`
			} `json:"spec"`
			Status struct {
				ReadyReplicas     int32 `json:"readyReplicas"`
				UpdatedReplicas   int32 `json:"updatedReplicas"`
				AvailableReplicas int32 `json:"availableReplicas"`
			} `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(stdout), &jsonResult); err == nil {
		deployments := make([]interface{}, 0, len(jsonResult.Items))
		for _, item := range jsonResult.Items {
			deployment := state.DeploymentInfo{
				Name:              item.Metadata.Name,
				Namespace:         item.Metadata.Namespace,
				Replicas:          item.Spec.Replicas,
				ReadyReplicas:     item.Status.ReadyReplicas,
				UpdatedReplicas:   item.Status.UpdatedReplicas,
				AvailableReplicas: item.Status.AvailableReplicas,
			}
			deployments = append(deployments, deployment)
		}
		return deployments, nil
	}

	// JSON 解析失败，尝试表格格式解析
	names, err := parseTableFormat(stdout, "NAME")
	if err != nil {
		return nil, fmt.Errorf("failed to parse deployments: %w", err)
	}

	// 表格格式只能获取名称列表
	deployments := make([]interface{}, 0, len(names))
	for _, name := range names {
		deployments = append(deployments, state.DeploymentInfo{Name: name})
	}
	return deployments, nil
}
