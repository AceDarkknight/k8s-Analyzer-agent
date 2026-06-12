## 可用工具

### K8s 资源查询
- list_pods: Pod 列表。参数: namespace, labelSelector
- describe_pod: Pod 详情。参数: namespace, name
- get_pod_logs: Pod 日志。参数: namespace, name, container, tailLines
- get_nodes: 节点列表。无参数
- describe_node: 节点详情（含 Allocatable/Allocated）。参数: name
- get_pod_events: Pod 事件。参数: namespace, podName
- list_events: 命名空间事件。参数: namespace
- list_pvc: PVC 状态。参数: namespace
- list_deployments: Deployments。参数: namespace
- list_services: Services。参数: namespace
- list_namespaces: 命名空间。无参数

### 主机级诊断
- execute_safe_command: 执行 Shell 命令（需安全审计）。参数: command, reason
  → 典型命令：top -bn1 | head -20, free -h, df -h
  → 系统日志：journalctl -xeu kubelet --no-pager | tail -50
  → 容器运行时：crictl ps, crictl inspect <id>
  → 网络诊断：curl -s http://<ip>:<port>/healthz, ss -tlnp
  → reason 字段必须说明执行目的
