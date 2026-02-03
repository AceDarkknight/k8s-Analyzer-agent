# Client 模块

## 概述

Client 模块负责与 MCP (Model Context Protocol) Server 建立连接、发送指令并解析响应。该模块为 K8s Analyzer Agent 提供与外部 MCP Server 通信的能力。

目前，本模块集成了以下 SDK 作为底层实现，以确保与标准 MCP Server 的最佳兼容性：
- `github.com/AceDarkknight/k8s-mcp`
- `github.com/AceDarkknight/shell-executor-mcp`

## 目录结构

```
internal/client/
├── README.md          # 本文件
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

## 核心接口

### MCPClient

MCPClient 接口定义了与 MCP Server 交互的核心方法：

```go
type MCPClient interface {
    // Connect 建立与 MCP Server 的连接
    Connect(ctx context.Context) error

    // Close 终止连接
    Close() error

    // CallTool 执行 MCP Server 上的特定工具
    CallTool(ctx context.Context, name string, args map[string]interface{}) (*mcp.CallToolResult, error)

    // ListTools 获取 Server 上可用的工具列表
    ListTools(ctx context.Context) ([]mcp.Tool, error)
}
```

## 支持的 MCP Server

### K8s MCP Server

- **端口**: 8443 (默认)
- **协议**: SSE (Server-Sent Events) over HTTPS/HTTP
- **认证**: Bearer Token
- **配置文件**: `bin/k8s_config.json`

**提供的工具**:
- `get_cluster_status` - 获取集群状态
- `list_pods` - 列出 Pod
- `list_services` - 列出 Service
- `list_deployments` - 列出 Deployment
- `list_nodes` - 列出节点
- `get_resource` - 获取资源详情
- `get_resource_yaml` - 获取资源 YAML
- `get_events` - 获取事件
- `get_pod_logs` - 获取 Pod 日志
- `check_rbac_permission` - 检查权限

### Shell Executor MCP Server

- **端口**: 8080 (默认)
- **协议**: SSE (Server-Sent Events) over HTTP
- **认证**: 可选
- **配置文件**: `bin/shell_config.json`

**提供的工具**:
- `execute_command` - 执行 Shell 命令

## 使用示例

### K8s Client

```go
import (
    "context"
    "log"
    "github.com/AceDarkknight/k8s-analyzer-agent/internal/client/k8s"
)

func main() {
    // 创建 K8s Client
    client, err := k8s.NewClient(k8s.Config{
        ServerURL: "https://localhost:8443",
        Token:     "your-token",
        Insecure:  true, // 开发环境可跳过 TLS 验证
    })
    if err != nil {
        log.Fatal(err)
    }
    defer client.Close()

    // 连接到 Server
    if err := client.Connect(context.Background()); err != nil {
        log.Fatal(err)
    }

    // 获取集群状态
    status, err := client.GetClusterStatus(context.Background())
    if err != nil {
        log.Fatal(err)
    }
    log.Printf("Cluster version: %s", status.Version)

    // 列出 default 命名空间中的 Pod
    pods, err := client.ListPods(context.Background(), "default")
    if err != nil {
        log.Fatal(err)
    }
    for _, pod := range pods {
        log.Printf("Pod: %s, Status: %s", pod.Name, pod.Status)
    }
}
```

### Shell Client

```go
import (
    "context"
    "log"
    "github.com/AceDarkknight/k8s-analyzer-agent/internal/client/shell"
)

func main() {
    // 创建 Shell Client
    client, err := shell.NewClient(shell.Config{
        Servers: []shell.ServerConfig{
            {Name: "primary", URL: "http://localhost:8080"},
            {Name: "backup", URL: "http://localhost:8081"},
        },
    })
    if err != nil {
        log.Fatal(err)
    }
    defer client.Close()

    // 连接到 Server
    if err := client.Connect(context.Background()); err != nil {
        log.Fatal(err)
    }

    // 执行命令
    result, err := client.ExecuteCommand(context.Background(), "ls -la /tmp")
    if err != nil {
        log.Fatal(err)
    }
    log.Printf("Command executed on %d nodes", len(result.Groups))
}
```

## 重试机制

Client 实现了指数退避重试机制，用于处理临时性网络错误：

- **最大重试次数**: 3
- **初始间隔**: 1 秒
- **退避策略**: 1s, 2s, 4s (指数增长)
- **适用场景**: 网络错误、连接超时、服务暂时不可用

## 错误处理

Client 提供了详细的错误信息，包括：

- 连接失败原因
- 工具调用失败详情
- 认证错误
- 超时错误

## 配置管理

Client 支持从 JSON 文件加载配置，并支持环境变量覆盖：

```go
// 从文件加载配置
config, err := client.LoadConfig("bin/k8s_config.json")
if err != nil {
    log.Fatal(err)
}

// 环境变量覆盖（优先级更高）
if token := os.Getenv("MCP_TOKEN"); token != "" {
    config.Token = token
}
```

## 测试

运行单元测试：

```bash
go test ./internal/client/...
```

运行测试并查看覆盖率：

```bash
go test -cover ./internal/client/...
```

## 注意事项

1. **安全性**: 生产环境中请使用 HTTPS 并配置有效的 TLS 证书
2. **Token 管理**: 请妥善保管认证 Token，避免泄露
3. **超时设置**: 建议为所有工具调用设置合理的超时时间
4. **错误处理**: 请妥善处理所有可能的错误情况

## 依赖

- `github.com/modelcontextprotocol/go-sdk/mcp` - MCP SDK
- `github.com/AceDarkknight/k8s-mcp` - K8s MCP SDK
- `github.com/AceDarkknight/shell-executor-mcp` - Shell Executor MCP SDK
- `github.com/stretchr/testify` - 测试框架

## 更新日志

- 2026-02-03: 集成 `k8s-mcp` 和 `shell-executor-mcp` SDK 作为底层实现
- 2026-01-25: 初始版本，实现 K8s MCP 和 Shell Executor MCP Client
