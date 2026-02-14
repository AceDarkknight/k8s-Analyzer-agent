# 增强日志和约束执行计划

## 目标

### 目标 1: 增强日志记录
在 MCP 客户端交互中添加详细日志，显示 Agent 调用 MCP 工具的时间和结果。

### 目标 2: 约束执行
确保 `kubectl` 命令只能通过 K8s MCP 执行，Shell MCP 不应执行 `kubectl`。

### 目标 3: 提示词修改
更新 Main Agent 的提示词，明确指示诊断所有命名空间。

## 实现步骤

### 步骤 1: 增强 K8s MCP Client 日志
- **文件**: `internal/client/k8s/client.go`
- **操作**: 在 `CallTool` 方法中添加日志
  - 调用前：记录工具名称和参数
  - 调用后：记录结果或错误
- **预期效果**: 用户可以看到 K8s MCP 工具调用的详细信息

### 步骤 2: 增强 Shell MCP Client 日志
- **文件**: `internal/client/shell/client.go`
- **操作**: 在 `CallTool` 方法中添加更详细的日志
  - 调用前：记录工具名称和参数（已有部分日志，需增强）
  - 调用后：记录完整结果或错误详情
- **预期效果**: 用户可以看到 Shell MCP 工具调用的详细信息

### 步骤 3: 在 Shell MCP 中阻止 kubectl 命令
- **文件**: `internal/client/shell/tools.go`
- **操作**: 在 `ExecuteCommand` 和 `ExecuteCommandWithTimeout` 方法中添加检查
  - 检测命令是否包含 `kubectl`
  - 如果包含，返回错误信息
- **预期效果**: Shell MCP 拒绝执行 kubectl 命令

### 步骤 4: 更新 Main Agent 提示词
- **文件**: `internal/agent/analysis/llm.go`
- **操作**: 修改系统提示词，强调诊断整个集群的所有命名空间
- **预期效果**: Agent 会主动诊断所有命名空间

### 步骤 5: 验证修改
- 运行项目 `go run cmd/k8s-analyzer/main.go`
- 检查日志输出是否符合预期
- 验证 kubectl 约束是否生效

## 注意事项
- 日志需要输出到 stdout/stderr，确保用户可见
- kubectl 检查需要考虑命令变体（如 `kubectl get pods`、`kubectl.exe` 等）
- 提示词修改需要保持与其他提示词的一致性
