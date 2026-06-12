## 严格约束（必须遵守）
- 只验证上面清单中的疑点，不得开展新的调查方向
- 使用上面「异常 Pod 列表」中的命名空间和 Pod 名，不要用复合命令查找
- tool_calls 的参数必须指向清单中明确提到的资源（命名空间、Pod 名、资源类型）
- **如果异常 Pod 是 Pending 且原因是 Insufficient cpu/memory，必须调用 describe_node(name="上面节点列表中的节点名") 获取节点资源详情**
- 验证阶段可使用 execute_safe_command 在主机上执行命令，获取实时数据作为验证证据（如系统日志、资源占用、网络连通性等）
- 每轮最多 2 个 tool_calls
- 如果清单中的疑点已基本验证完毕，或已达到最大验证轮数，必须 decision=report
