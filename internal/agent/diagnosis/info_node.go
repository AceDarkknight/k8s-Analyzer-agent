package diagnosis

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/AceDarkknight/k8s-analyzer-agent/internal/client/gateway"
	"github.com/AceDarkknight/k8s-analyzer-agent/internal/logger"
	"github.com/AceDarkknight/k8s-analyzer-agent/internal/state"
)

// InfoNode 信息收集节点
type InfoNode struct {
	gateway       *gateway.GatewayClient
	maxNamespaces int // 动态 namespace 上限，0 表示使用动态计算
}

// NewInfoNode 创建新的信息收集节点
// maxNamespaces: 配置的 namespace 上限，0 表示使用动态计算
func NewInfoNode(gw *gateway.GatewayClient, maxNamespaces int) *InfoNode {
	return &InfoNode{
		gateway:       gw,
		maxNamespaces: maxNamespaces,
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
		// 动态计算 namespace 扫描上限
		limit := n.calcNamespaceLimit(len(namespaces))
		targetNamespaces = n.prioritizeNamespaces(namespaces)
		if len(targetNamespaces) > limit {
			targetNamespaces = targetNamespaces[:limit]
			logger.Info("InfoNode: limiting namespaces dynamically",
				logger.Int("total", len(namespaces)),
				logger.Int("limit", limit))
		}
	}

	// 4. 初始化 K8sInfo
	k8sInfo := &state.K8sInfo{
		Namespaces: targetNamespaces,
		Resources:  make(map[string][]interface{}),
	}

	// 5. 并发收集各命名空间的 Pods、Deployments 和 Services
	var wg sync.WaitGroup
	var mu sync.Mutex

	for _, ns := range targetNamespaces {
		wg.Add(1)
		go func(namespace string) {
			defer wg.Done()

			// 获取 Pod 列表
			if pods, err := n.collectPods(ctx, namespace); err != nil {
				logger.Warn("InfoNode: failed to collect pods",
					logger.String("namespace", namespace),
					logger.Err(err))
			} else {
				mu.Lock()
				k8sInfo.Resources["Pods"] = append(k8sInfo.Resources["Pods"], pods...)
				mu.Unlock()
			}

			// 获取 Deployment 列表
			if deployments, err := n.collectDeployments(ctx, namespace); err != nil {
				logger.Warn("InfoNode: failed to collect deployments",
					logger.String("namespace", namespace),
					logger.Err(err))
			} else {
				mu.Lock()
				k8sInfo.Resources["Deployments"] = append(k8sInfo.Resources["Deployments"], deployments...)
				mu.Unlock()
			}

			// 获取 Services 列表
			if services, err := n.collectServices(ctx, namespace); err != nil {
				logger.Warn("InfoNode: failed to collect services",
					logger.String("namespace", namespace),
					logger.Err(err))
			} else {
				mu.Lock()
				k8sInfo.Resources["Services"] = append(k8sInfo.Resources["Services"], services...)
				mu.Unlock()
			}
		}(ns)
	}
	wg.Wait()

	// 6. 收集节点状态和集群事件（全局资源）
	if nodes, err := n.collectNodes(ctx); err != nil {
		logger.Warn("InfoNode: failed to collect nodes", logger.Err(err))
	} else {
		k8sInfo.Resources["Nodes"] = nodes
	}

	if events, err := n.collectClusterEvents(ctx); err != nil {
		logger.Warn("InfoNode: failed to collect events", logger.Err(err))
	} else {
		k8sInfo.Resources["Events"] = events
	}

	// 7. 更新 state
	s.SetK8sInfo(k8sInfo)

	logger.Info("InfoNode: information collection completed",
		logger.Int("namespaces", len(targetNamespaces)),
		logger.Int("pods", len(k8sInfo.Resources["Pods"])),
		logger.Int("deployments", len(k8sInfo.Resources["Deployments"])))

	return s, nil
}

// collectPods 收集 Pod 信息
func (n *InfoNode) collectPods(ctx context.Context, ns string) ([]interface{}, error) {
	resp, err := n.gateway.ListPods(ctx, ns, "")
	if err != nil {
		return nil, err
	}

	pods, err := parsePods(resp.Stdout, ns)
	if err != nil {
		return nil, err
	}

	return pods, nil
}

// collectDeployments 收集 Deployment 信息
func (n *InfoNode) collectDeployments(ctx context.Context, ns string) ([]interface{}, error) {
	resp, err := n.gateway.ListDeployments(ctx, ns)
	if err != nil {
		return nil, err
	}

	deployments, err := parseDeployments(resp.Stdout)
	if err != nil {
		return nil, err
	}

	deployments = applyNamespaceToScopedResources(deployments, ns)

	return deployments, nil
}

// collectServices 收集 Service 信息
func (n *InfoNode) collectServices(ctx context.Context, ns string) ([]interface{}, error) {
	resp, err := n.gateway.ListServices(ctx, ns)
	if err != nil {
		return nil, err
	}

	services, err := parseServices(resp.Stdout)
	if err != nil {
		return nil, err
	}

	services = applyNamespaceToScopedResources(services, ns)

	return services, nil
}

func applyNamespaceToScopedResources(resources []interface{}, namespace string) []interface{} {
	if namespace == "" {
		return resources
	}

	for i, resource := range resources {
		switch v := resource.(type) {
		case state.PodInfo:
			if v.Namespace == "" {
				v.Namespace = namespace
				resources[i] = v
			}
		case state.DeploymentInfo:
			if v.Namespace == "" {
				v.Namespace = namespace
				resources[i] = v
			}
		case state.ServiceInfo:
			if v.Namespace == "" {
				v.Namespace = namespace
				resources[i] = v
			}
		}
	}

	return resources
}

// collectNodes 收集节点信息
func (n *InfoNode) collectNodes(ctx context.Context) ([]interface{}, error) {
	resp, err := n.gateway.GetNodes(ctx)
	if err != nil {
		return nil, err
	}

	nodes, err := parseNodes(resp.Stdout)
	if err != nil {
		return nil, err
	}

	return nodes, nil
}

// collectClusterEvents 收集集群事件（Warning 级别）
func (n *InfoNode) collectClusterEvents(ctx context.Context) ([]interface{}, error) {
	resp, err := n.gateway.ListEvents(ctx, "")
	if err != nil {
		return nil, err
	}

	events, err := parseEvents(resp.Stdout)
	if err != nil {
		return nil, err
	}

	return events, nil
}

// extractSpecifiedNamespace 从用户输入中提取指定的命名空间
func extractSpecifiedNamespace(input string) string {
	input = strings.ToLower(input)

	// 匹配 "namespace:" 或 "命名空间" 关键词以及常见命令行参数
	patterns := []string{"namespace:", "namespace：", "命名空间", "ns:", "-n ", "--namespace "}
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

// parsePodTableFormat 解析 Pod 表格格式（支持 wide/non-wide 头部）
func parsePodTableFormat(stdout string, namespace string) ([]interface{}, error) {
	lines := strings.Split(stdout, "\n")
	var pods []interface{}
	headerFound := false
	hasNamespace := false
	hasReady := false
	hasStatus := false
	hasRestarts := false
	hasAge := false
	hasIP := false
	hasNode := false
	hasNominatedNode := false
	hasReadinessGates := false

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		upper := strings.ToUpper(line)
		if strings.Contains(upper, "NAME") && strings.Contains(upper, "STATUS") && !headerFound {
			headerFound = true
			hasNamespace = strings.Contains(upper, "NAMESPACE")
			hasReady = strings.Contains(upper, "READY")
			hasStatus = strings.Contains(upper, "STATUS")
			hasRestarts = strings.Contains(upper, "RESTARTS")
			hasAge = strings.Contains(upper, "AGE")
			hasIP = strings.Contains(upper, " IP ") || strings.HasSuffix(upper, " IP") || strings.Contains(upper, " IP ")
			hasNode = strings.Contains(upper, " NODE")
			hasNominatedNode = strings.Contains(upper, "NOMINATED NODE")
			hasReadinessGates = strings.Contains(upper, "READINESS GATES")
			continue
		}

		if !headerFound {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}

		pod := state.PodInfo{Namespace: namespace}
		left := 0

		if hasNamespace && left < len(fields) {
			pod.Namespace = fields[left]
			left++
		}

		if left < len(fields) {
			pod.Name = fields[left]
			left++
		}

		if hasReady && left < len(fields) {
			left++
		}

		if hasStatus && left < len(fields) {
			pod.Status = fields[left]
			left++
		}

		right := len(fields)

		if hasReadinessGates && right > left {
			right--
		}
		if hasNominatedNode && right > left {
			right--
		}
		if hasNode && right > left {
			pod.NodeName = fields[right-1]
			right--
		}
		if hasIP && right > left {
			right--
		}
		if hasAge && right > left {
			pod.Age = fields[right-1]
			right--
		}

		if hasRestarts && left < right {
			restartField := strings.Join(fields[left:right], " ")
			var restarts int32
			fmt.Sscanf(restartField, "%d", &restarts)
			pod.Restarts = restarts
		}

		pods = append(pods, pod)
	}

	if !headerFound {
		return nil, fmt.Errorf("failed to parse pod table format: header not found")
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
	if stdout != "" {
		if deployments, err := parseDeploymentTableFormat(stdout); err == nil {
			return deployments, nil
		}
	}

	names, err := parseTableFormat(stdout, "NAME")
	if err != nil {
		return nil, fmt.Errorf("failed to parse deployments: %w", err)
	}

	deployments := make([]interface{}, 0, len(names))
	for _, name := range names {
		deployments = append(deployments, state.DeploymentInfo{Name: name})
	}
	return deployments, nil
}

// parseDeploymentTableFormat 解析 Deployment 的 wide 表格格式
func parseDeploymentTableFormat(stdout string) ([]interface{}, error) {
	lines := strings.Split(stdout, "\n")
	var deployments []interface{}
	headerFound := false
	idx := map[string]int{}

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		upper := strings.ToUpper(line)
		if strings.Contains(upper, "NAME") && strings.Contains(upper, "READY") && !headerFound {
			headerFound = true
			fields := strings.Fields(line)
			for i, h := range fields {
				key := strings.ToLower(h)
				switch key {
				case "name":
					idx["name"] = i
				case "ready":
					idx["ready"] = i
				case "up-to-date":
					idx["up_to_date"] = i
				case "updated":
					idx["updated"] = i
				case "available":
					idx["available"] = i
				case "age":
					idx["age"] = i
				}
			}
			continue
		}
		if headerFound {
			fields := strings.Fields(line)
			var name string
			if v, ok := idx["name"]; ok && v < len(fields) {
				name = fields[v]
			} else if len(fields) > 0 {
				name = fields[0]
			}
			dep := state.DeploymentInfo{Name: name}

			if v, ok := idx["ready"]; ok && v < len(fields) {
				ready := fields[v]
				if parts := strings.Split(ready, "/"); len(parts) == 2 {
					var readyNum int32
					var repTotal int32
					fmt.Sscanf(parts[0], "%d", &readyNum)
					fmt.Sscanf(parts[1], "%d", &repTotal)
					dep.ReadyReplicas = readyNum
					dep.Replicas = repTotal
				}
			}

			if v, ok := idx["up_to_date"]; ok && v < len(fields) {
				var upd int32
				fmt.Sscanf(fields[v], "%d", &upd)
				dep.UpdatedReplicas = upd
			} else if v, ok := idx["updated"]; ok && v < len(fields) {
				var upd int32
				fmt.Sscanf(fields[v], "%d", &upd)
				dep.UpdatedReplicas = upd
			}

			if v, ok := idx["available"]; ok && v < len(fields) {
				var av int32
				fmt.Sscanf(fields[v], "%d", &av)
				dep.AvailableReplicas = av
			}

			deployments = append(deployments, dep)
		}
	}

	if !headerFound {
		return nil, fmt.Errorf("failed to parse deployment table format: header not found")
	}

	return deployments, nil
}

// parseServices 解析 Service 列表（支持 JSON 和表格格式）
func parseServices(stdout string) ([]interface{}, error) {
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
				Type      string `json:"type"`
				ClusterIP string `json:"clusterIP"`
				Ports     []struct {
					Port     int32  `json:"port"`
					Protocol string `json:"protocol"`
				} `json:"ports"`
			} `json:"spec"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(stdout), &jsonResult); err == nil {
		services := make([]interface{}, 0, len(jsonResult.Items))
		for _, item := range jsonResult.Items {
			var portStrs []string
			for _, p := range item.Spec.Ports {
				portStrs = append(portStrs, fmt.Sprintf("%d/%s", p.Port, p.Protocol))
			}
			svc := state.ServiceInfo{
				Name:      item.Metadata.Name,
				Namespace: item.Metadata.Namespace,
				Type:      item.Spec.Type,
				ClusterIP: item.Spec.ClusterIP,
				Ports:     strings.Join(portStrs, ","),
			}
			services = append(services, svc)
		}
		return services, nil
	}

	// JSON 解析失败，尝试表格格式
	// 先尝试 wide 表头解析
	if services, err := parseServiceTableFormat(stdout); err == nil {
		return services, nil
	}
	names, err := parseTableFormat(stdout, "NAME")
	if err != nil {
		return nil, fmt.Errorf("failed to parse services: %w", err)
	}
	services := make([]interface{}, 0, len(names))
	for _, name := range names {
		services = append(services, state.ServiceInfo{Name: name})
	}
	return services, nil
}

// parseServiceTableFormat 解析 Service 的 wide 表格格式
func parseServiceTableFormat(stdout string) ([]interface{}, error) {
	lines := strings.Split(stdout, "\n")
	var services []interface{}
	headerFound := false
	idx := map[string]int{}

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		upper := strings.ToUpper(line)
		if strings.Contains(upper, "NAME") && strings.Contains(upper, "TYPE") && !headerFound {
			headerFound = true
			fields := strings.Fields(line)
			for i, h := range fields {
				key := strings.ToLower(h)
				switch key {
				case "name":
					idx["name"] = i
				case "type":
					idx["type"] = i
				case "cluster-ip":
					idx["cluster_ip"] = i
				case "ports", "port(s)":
					idx["ports"] = i
				case "age":
					idx["age"] = i
				}
			}
			continue
		}
		if headerFound {
			fields := strings.Fields(line)
			var name, serviceType string
			if v, ok := idx["name"]; ok && v < len(fields) {
				name = fields[v]
			} else if len(fields) > 0 {
				name = fields[0]
			}
			if v, ok := idx["type"]; ok && v < len(fields) {
				serviceType = fields[v]
			}
			svc := state.ServiceInfo{Name: name, Type: serviceType}
			if v, ok := idx["cluster_ip"]; ok && v < len(fields) {
				svc.ClusterIP = fields[v]
			}
			if v, ok := idx["ports"]; ok && v < len(fields) {
				svc.Ports = fields[v]
			}
			services = append(services, svc)
		}
	}
	if !headerFound {
		return nil, fmt.Errorf("failed to parse service table format: header not found")
	}

	return services, nil
}

// parseNodes 解析节点列表（支持 JSON 和表格格式）
func parseNodes(stdout string) ([]interface{}, error) {
	stdout = strings.TrimSpace(stdout)
	if stdout == "" {
		return []interface{}{}, nil
	}

	// 尝试 JSON 格式解析
	var jsonResult struct {
		Items []struct {
			Metadata struct {
				Name   string            `json:"name"`
				Labels map[string]string `json:"labels"`
			} `json:"metadata"`
			Status struct {
				Conditions []struct {
					Type   string `json:"type"`
					Status string `json:"status"`
				} `json:"conditions"`
			} `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(stdout), &jsonResult); err == nil {
		nodes := make([]interface{}, 0, len(jsonResult.Items))
		for _, item := range jsonResult.Items {
			nodeStatus := "Unknown"
			for _, cond := range item.Status.Conditions {
				if cond.Type == "Ready" {
					if cond.Status == "True" {
						nodeStatus = "Ready"
					} else {
						nodeStatus = "NotReady"
					}
					break
				}
			}
			// 提取角色标签
			role := "worker"
			for k := range item.Metadata.Labels {
				if strings.HasPrefix(k, "node-role.kubernetes.io/") {
					role = strings.TrimPrefix(k, "node-role.kubernetes.io/")
					break
				}
			}
			nodes = append(nodes, state.NodeInfo{
				Name:   item.Metadata.Name,
				Status: nodeStatus,
				Roles:  role,
			})
		}
		return nodes, nil
	}

	// JSON 解析失败，尝试表格格式
	lines := strings.Split(stdout, "\n")
	var nodes []interface{}
	headerFound := false
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.Contains(line, "NAME") && strings.Contains(line, "STATUS") && !headerFound {
			headerFound = true
			continue
		}
		if headerFound {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				nodes = append(nodes, state.NodeInfo{
					Name:   fields[0],
					Status: fields[1],
				})
			}
		}
	}
	return nodes, nil
}

// EventInfo 事件信息
type EventInfo struct {
	Namespace string
	Type      string
	Reason    string
	Object    string
	Message   string
}

// parseEvents 解析事件列表（只保留 Warning 事件）
func parseEvents(stdout string) ([]interface{}, error) {
	stdout = strings.TrimSpace(stdout)
	if stdout == "" {
		return []interface{}{}, nil
	}

	// 尝试 JSON 格式解析
	var jsonResult struct {
		Items []struct {
			Metadata struct {
				Namespace string `json:"namespace"`
			} `json:"metadata"`
			Type           string `json:"type"`
			Reason         string `json:"reason"`
			Message        string `json:"message"`
			InvolvedObject struct {
				Kind string `json:"kind"`
				Name string `json:"name"`
			} `json:"involvedObject"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(stdout), &jsonResult); err == nil {
		events := make([]interface{}, 0)
		for _, item := range jsonResult.Items {
			// 只保留 Warning 事件
			if item.Type != "Warning" {
				continue
			}
			events = append(events, EventInfo{
				Namespace: item.Metadata.Namespace,
				Type:      item.Type,
				Reason:    item.Reason,
				Object:    fmt.Sprintf("%s/%s", item.InvolvedObject.Kind, item.InvolvedObject.Name),
				Message:   item.Message,
			})
		}
		return events, nil
	}

	// JSON 解析失败，返回空列表
	return []interface{}{}, nil
}

// calcNamespaceLimit 根据集群 namespace 总数动态计算扫描上限
// 规则：≤8 全扫、≤20 取 10、>20 取 15；如果配置了 maxNamespaces 则以配置为准
func (n *InfoNode) calcNamespaceLimit(total int) int {
	if n.maxNamespaces > 0 {
		return n.maxNamespaces
	}
	switch {
	case total <= 8:
		return total
	case total <= 20:
		return 10
	default:
		return 15
	}
}

// prioritizeNamespaces 对 namespace 进行优先级排序
// 将业务相关 namespace 排在前面，系统 namespace（kube-*）排在后面
func (n *InfoNode) prioritizeNamespaces(namespaces []string) []string {
	var priority, system, others []string
	for _, ns := range namespaces {
		switch {
		case ns == "default":
			priority = append(priority, ns)
		case strings.HasPrefix(ns, "kube-"):
			system = append(system, ns)
		default:
			others = append(others, ns)
		}
	}
	// 业务 namespace → default → 系统 namespace
	result := make([]string, 0, len(namespaces))
	result = append(result, others...)
	result = append(result, priority...)
	result = append(result, system...)
	return result
}
