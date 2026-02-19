# 实施计划：整合 k8s-mcp 依赖并补全节点解析逻辑

## 目标
1. 更新 `github.com/AceDarkknight/k8s-mcp` 依赖到最新版本
2. 删除 `internal/client/k8s/tools.go` 中重复的结构体定义，改为引用 `k8s-mcp`
3. 补全 `internal/agent/analysis/nodes.go` 中的 `collectNamespaces`, `collectPods`, `collectServices`, `collectDeployments`, `collectEvents` 等解析逻辑

## 背景
- 当前 `internal/client/k8s/tools.go` 中定义的结构体（Pod, Service, Deployment, Node, Event, ConfigMap, StatefulSet, RBACPermission, PodLogOptions）与 `k8s-mcp` 包中的定义重复
- `internal/agent/analysis/nodes.go` 中的资源收集函数调用了 K8s MCP 工具，但缺少解析 MCP 返回数据的逻辑

## 步骤

### 步骤 1: 删除 `tools.go` 中重复的结构体定义
- 删除以下结构体定义（改为从 `k8s-mcp` 导入）:
  - `Pod`
  - `Service`
  - `Deployment`
  - `Node`
  - `Event`
  - `ConfigMap`
  - `StatefulSet`
  - `RBACPermission`
  - `PodLogOptions`
- 保留 `ClusterStatus`（k8s-mcp 中没有对应定义）
- 保留 `resourceKeyMapping` 映射表
- 保留 `ParseToolResult` 和 `ParseToolResultAsString` 函数
- 添加必要的导入：`github.com/AceDarkknight/k8s-mcp/pkg/types`

### 步骤 2: 更新 `nodes.go` 中的解析逻辑
- 补全 `collectNamespaces` 函数：解析 `list_namespaces` 工具返回的 JSON 数据
- 补全 `collectPods` 函数：解析 `list_pods` 工具返回的 JSON 数据
- 补全 `collectServices` 函数：解析 `list_services` 工具返回的 JSON 数据
- 补全 `collectDeployments` 函数：解析 `list_deployments` 工具返回的 JSON 数据
- 补全 `collectEvents` 函数：解析 `get_events` 工具返回的 JSON 数据

### 步骤 3: 更新 `state.go` 中的 Info 结构体（如果需要）
- 检查 `K8sInfo` 结构体中的 `PodInfo`, `ServiceInfo`, `DeploymentInfo`, `EventInfo` 类型定义
- 确保这些类型与 `k8s-mcp` 中的定义兼容

### 步骤 4: 运行测试验证
- 运行 `go build` 确保代码编译通过
- 运行单元测试确保功能正常

## 预期效果
- 代码中不再有重复的结构体定义
- `nodes.go` 中的资源收集函数能够正确解析 MCP 返回的数据
- 项目能够正常编译和运行