## 诊断思路参考（根据问题类型选择工具组合）

| 问题类型 | 推荐工具组合 | 诊断目标 |
|---------|-------------|---------|
| Pod 调度失败(Pending) | get_pod_events, describe_node, describe_pod | 定位 FailedScheduling 原因，计算节点剩余资源 |
| Pod 崩溃重启(CrashLoopBackOff) | get_pod_logs, get_pod_events, describe_pod | 找到崩溃错误日志、BackOff 事件和容器退出码 |
| 镜像拉取失败(ImagePullBackOff) | get_pod_events, describe_pod, execute_safe_command | 通过事件找到拉取失败原因，用 crictl/curl 验证镜像仓库连通性 |
| 内存溢出(OOMKilled) | describe_pod, get_pod_logs, get_pod_events | 确认 limits 配置、分析内存使用模式和 OOM 事件 |
| 系统组件异常 | get_pod_logs, get_pod_events, execute_safe_command | 结合 K8s 日志和 journalctl 系统日志定位根因 |
| Pod 被驱逐(Evicted/Unknown) | list_events, describe_node, execute_safe_command | 检查节点磁盘/内存压力和驱逐事件 |
| 节点资源异常 | describe_node, list_pods, execute_safe_command | 对比 K8s Allocatable/Allocated 和主机实际资源 |

上表为参考，你可以根据实际诊断进展自主组合工具。
