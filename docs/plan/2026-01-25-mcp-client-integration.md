# MCP Client 集成实现计划

## 任务概述

开发 MCP Client 模块，用于连接和调用在 `bin/` 下部署的两个 MCP Server：
- **K8s MCP Server** (端口 8443): Kubernetes 集群管理和资源查看
- **Shell Executor MCP Server** (端口 8080): 分布式 Shell 命令执行

## 实现目标

1. 创建 Client 模块基础代码
2. 实现 SSE 连接与认证逻辑
3. 封装工具调用接口
4. 实现错误重试机制（指数退避）
5. 编写单元测试

## 详细实现步骤

### 步骤 1: 创建项目目录结构

**目标**: 建立 Client 模块的目录结构

**操作**:
- 创建 `internal/client` 目录
- 创建 `internal/client/README.md` 说明文件
- 创建 `internal/client/k8s` 子目录（K8s MCP Client）
- 创建 `internal/client/shell` 子目录（Shell Executor MCP Client）
- 创建 `internal/client/mocks` 子目录（用于单元测试）

**预期效果**:
```
internal/client/
├── README.md
├── client.go          # 通用 MCP Client 接口定义
├── config.go          # 配置加载与管理
├── retry.go           # 重试机制实现
├── k8s/
│   ├── client.go      # K8s MCP Client 实现
│   └── tools.go       # K8s 工具封装方法
├── shell/
│   ├── client.go      # Shell Executor MCP Client 实现
│   └── tools.go       # Shell 工具封装方法
└── mocks/
    └── client_mock.go # Mock 实现（用于测试）
```

### 步骤 2: 实现通用 MCP Client 接口

**目标**: 定义 MCP Client 的核心接口和基础实现

**操作**:
- 在 `client.go` 中定义 `MCPClient` 接口
- 定义 `ClientConfig` 配置结构体
- 实现 `BaseClient` 结构体，包含通用功能
- 实现指数退避重试机制（最大重试 3 次，初始间隔 1s）

**关键接口**:
```go
type MCPClient interface {
    Connect(ctx context.Context) error
    Close() error
    CallTool(ctx context.Context, name string, args map[string]interface{}) (*mcp.CallToolResult, error)
    ListTools(ctx context.Context) ([]mcp.Tool, error)
}
```

**预期效果**:
- 定义清晰的接口契约
- 实现可复用的重试逻辑
- 提供统一的错误处理机制

### 步骤 3: 实现 K8s MCP Client

**目标**: 实现连接 K8s MCP Server 的 Client

**操作**:
- 在 `k8s/client.go` 中实现 `K8sClient` 结构体
- 实现 SSE 连接逻辑，支持 HTTPS（可配置 insecure 模式）
- 实现 Bearer Token 认证（从配置读取 token）
- 实现 `Connect`、`Close`、`CallTool`、`ListTools` 方法
- 添加连接健康检查机制

**配置来源**: `bin/k8s_config.json`
- 端口: 8443
- Token: "k8s-analyzer-token"
- Insecure: true

**预期效果**:
- Client 可以成功连接到 K8s MCP Server
- 支持认证和安全连接
- 自动重试连接失败的情况

### 步骤 4: 实现 Shell Executor MCP Client

**目标**: 实现连接 Shell Executor MCP Server 的 Client

**操作**:
- 在 `shell/client.go` 中实现 `ShellClient` 结构体
- 实现 SSE 连接逻辑（HTTP）
- 实现多服务器故障转移（支持配置多个 server URL）
- 实现 `Connect`、`Close`、`CallTool`、`ListTools` 方法

**配置来源**: `bin/shell_config.json`
- 端口: 8080
- 支持故障转移到 peers

**预期效果**:
- Client 可以成功连接到 Shell Executor MCP Server
- 支持故障转移机制
- 当主服务器不可用时自动切换到备用服务器

### 步骤 5: 封装 K8s 工具调用方法

**目标**: 为 K8s MCP 提供类型安全的便捷方法

**操作**:
- 在 `k8s/tools.go` 中实现以下方法：
  - `GetClusterStatus(ctx)` - 获取集群状态
  - `ListPods(ctx, namespace)` - 列出 Pod
  - `ListServices(ctx, namespace)` - 列出 Service
  - `ListDeployments(ctx, namespace)` - 列出 Deployment
  - `ListNodes(ctx)` - 列出节点
  - `GetResource(ctx, resourceType, name, namespace)` - 获取资源详情
  - `GetResourceYAML(ctx, resourceType, name, namespace)` - 获取资源 YAML
  - `GetEvents(ctx, namespace)` - 获取事件
  - `GetPodLogs(ctx, podName, namespace, options)` - 获取 Pod 日志
  - `CheckRBACPermission(ctx, verb, resource, namespace)` - 检查权限

**预期效果**:
- 提供类型安全的 API
- 简化工具调用
- 统一错误处理

### 步骤 6: 封装 Shell 工具调用方法

**目标**: 为 Shell Executor MCP 提供类型安全的便捷方法

**操作**:
- 在 `shell/tools.go` 中实现以下方法：
  - `ExecuteCommand(ctx, command)` - 执行 Shell 命令
  - 返回结构化的执行结果（包含 summary、groups、nodes）

**预期效果**:
- 简化命令执行调用
- 统一结果解析
- 支持多节点执行结果的聚合

### 步骤 7: 实现配置管理

**目标**: 统一管理 Client 配置

**操作**:
- 在 `config.go` 中定义配置结构体
- 实现从 JSON 文件加载配置
- 实现环境变量覆盖机制
- 提供配置验证功能

**预期效果**:
- 支持从文件加载配置
- 支持环境变量覆盖
- 配置验证确保正确性

### 步骤 8: 编写单元测试

**目标**: 验证 Client 功能的正确性

**操作**:
- 创建 `internal/client/client_test.go`
- 测试通用重试机制
- 创建 `internal/client/k8s/client_test.go`
  - Mock K8s MCP Server
  - 测试连接、认证、工具调用
- 创建 `internal/client/shell/client_test.go`
  - Mock Shell Executor MCP Server
  - 测试连接、故障转移、工具调用
- 测试边界情况和错误处理

**预期效果**:
- 单元测试覆盖率达到 80% 以上
- 所有测试通过
- Mock Server 可用于本地测试

### 步骤 9: 更新文档

**目标**: 确保文档与代码同步

**操作**:
- 更新 `internal/client/README.md`，说明模块用途和使用方法
- 更新 `docs/architecture.md` 中的 MCP Client 部分
- 添加使用示例代码

**预期效果**:
- 文档清晰完整
- 包含使用示例
- 与代码保持同步

## 技术细节

### 依赖库
- `github.com/modelcontextprotocol/go-sdk/mcp` - MCP SDK
- `github.com/stretchr/testify` - 测试框架

### SSE 连接实现
- 使用 `mcp.StreamableClientTransport` 实现 SSE 通讯
- K8s MCP: `https://localhost:8443/sse` (或 `http://` 如果 insecure)
- Shell Executor MCP: `http://localhost:8080/sse`

### 认证机制
- K8s MCP: HTTP Header `Authorization: Bearer <token>`
- Shell Executor MCP: 当前未强制要求 Token（可选）

### 重试策略
- 指数退避：1s, 2s, 4s
- 最大重试次数：3
- 仅对可重试错误（网络错误、超时）进行重试

### 错误处理
- 连接失败：返回详细错误信息
- 工具调用失败：返回 MCP 错误响应
- 超时：使用 context.WithTimeout 设置超时时间

## 预期成果

1. **代码结构清晰**: 模块化设计，职责分明
2. **接口定义完善**: MCPClient 接口提供统一的调用方式
3. **连接稳定可靠**: 支持重试、故障转移、健康检查
4. **使用简单便捷**: 封装的便捷方法简化调用
5. **测试覆盖充分**: 单元测试验证核心功能
6. **文档完整准确**: README 和架构文档同步更新

## 注意事项

1. 所有代码文件行数不超过 600 行
2. 方法需要添加必要的中文注释
3. 每个文件夹下需要有 README.md 文件
4. 代码提交前确保所有单元测试通过
5. 更新代码后及时更新文档

## 后续工作

完成本阶段后，可以进入下一阶段：
- 实现 Agent Core (Graph Orchestration)
- 实现安全命令执行子 Agent
- 实现主分析 Agent
