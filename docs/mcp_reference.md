# MCP Reference

本文档汇总了 `temp_mcp` 中两个 MCP 项目的关键信息，包括接口定义、配置方法和使用说明。这些信息用于后续开发 Client 时参考。

## 目录

- [K8s MCP](#k8s-mcp)
    - [Tools](#tools)
    - [Resources](#resources)
    - [配置](#配置)
- [Shell Executor MCP](#shell-executor-mcp)
    - [Tools](#tools-1)
    - [配置](#配置-1)
    - [Client 连接](#client-连接)

---

## K8s MCP

用于 Kubernetes 集群管理和资源查看的 MCP 服务器。

### Tools

服务器提供以下工具，AI 可以自动调用：

| 工具名称 | 描述 | 参数 |
| :--- | :--- | :--- |
| `get_cluster_status` | 获取集群状态信息（版本、节点数、命名空间数） | 无 |
| `list_pods` | 列出指定命名空间中的 Pod | `namespace` (string, 必需) |
| `list_services` | 列出指定命名空间中的 Service | `namespace` (string, 必需) |
| `list_deployments` | 列出指定命名空间中的 Deployment | `namespace` (string, 必需) |
| `list_nodes` | 列出集群中的所有节点 | 无 |
| `get_resource` | 获取特定资源的详细信息（JSON 格式）。Secret 数据将被脱敏。 | `resource_type` (string, 必需), `name` (string, 必需), `namespace` (string, 必需) |
| `get_resource_yaml` | 获取资源的完整 YAML 定义。Secret 数据将被脱敏。 | `resource_type` (string, 必需), `name` (string, 必需), `namespace` (string, 必需) |
| `get_events` | 获取集群事件 | `namespace` (string, 必需) |
| `get_pod_logs` | 获取 Pod 日志。默认 tail_lines=100，最大 1MB。 | `pod_name` (string, 必需), `namespace` (string, 必需), `container_name` (string, 可选), `tail_lines` (int, 可选), `previous` (bool, 可选), `cluster_name` (string, 可选) |
| `check_rbac_permission` | 检查当前用户是否有权限执行某个操作 | `verb` (string, 必需), `resource` (string, 必需), `namespace` (string, 必需) |

### Resources

支持的 `resource_type` 参数值：

- `pods`, `pod`
- `services`, `service`
- `deployments`, `deployment`
- `configmaps`, `configmap`
- `secrets`, `secret` (数据自动脱敏)
- `namespaces`, `namespace`
- `nodes`, `node`
- `events`, `event`

### 配置

#### 服务器启动参数

| 参数 | 环境变量 | 默认值 | 说明 |
| :--- | :--- | :--- | :--- |
| `--port` | `MCP_PORT` | 8443 | 监听端口 |
| `--cert` | `MCP_CERT` | | TLS 证书文件路径（HTTPS 模式必需） |
| `--key` | `MCP_KEY` | | TLS 密钥文件路径（HTTPS 模式必需） |
| `--insecure` | `MCP_INSECURE` | false | 使用不安全的 HTTP 模式（默认为 HTTPS） |
| `--token` | `MCP_TOKEN` | | 认证 Token（必需） |
| `--kubeconfig` | `MCP_KUBECONFIG` | | kubeconfig 文件路径（可选） |

#### 认证

所有请求必须包含 HTTP Header：
`Authorization: Bearer <token>`

---

## Shell Executor MCP

基于 MCP 的分布式 Shell 命令执行系统，支持集群分发和安全控制。

### Tools

| 工具名称 | 描述 | 参数 (JSON Schema) |
| :--- | :--- | :--- |
| `execute_command` | 在服务器集群上执行 Shell 命令 | `command` (string, 必需): 需要执行的 Shell 命令。禁止包含高危操作。 |

**`execute_command` 输出示例:**

```json
{
  "summary": "Executed on 3 nodes, 2 groups found",
  "groups": [
    {
      "count": 2,
      "status": "success",
      "output": "v1.0.0",
      "nodes": ["node-01", "node-02"]
    },
    {
      "count": 1,
      "status": "failed",
      "output": "command not found",
      "nodes": ["node-03"]
    }
  ]
}
```

### 配置

#### Server 配置 (`server_config.json`)

```json
{
  "port": 8080,
  "node_name": "node-01",
  "peers": [
    "http://localhost:8081",
    "http://localhost:8082"
  ],
  "cluster_token": "your-cluster-token",
  "security": {
    "blacklisted_commands": ["rm", "mkfs", "shutdown", "reboot"],
    "dangerous_args_regex": [
      "rm\\s+-[a-zA-Z]*r[a-zA-Z]*\\s+/"
    ]
  },
  "log": {
    "level": "info",
    "log_dir": ".",
    "max_size": 100,
    "max_backups": 3,
    "max_age": 28,
    "compress": false
  }
}
```

#### Client 配置 (`client_config.json`)

Client 支持故障转移，按顺序尝试连接服务器列表。

```json
{
  "servers": [
    {
      "name": "primary-01",
      "url": "http://localhost:8080"
    },
    {
      "name": "backup-02",
      "url": "http://localhost:8081"
    }
  ]
}
```

### Client 连接

- **协议**: MCP over HTTP (SSE)
- **URL**: `http://<server_ip>:<port>/sse` (注意：标准 MCP SDK 通常暴露 `/sse` 端点，或者根路径。需根据 `main.go` 中实际路由确认，通常 SDK 默认可能是 `/sse` 或者直接挂载在 `/`)
    - *校对注：根据 `internal/mcp/server.go` (K8s) 和 Shell Executor 的 `main.go` (未直接读取但推测)，Go SDK `mcp.NewStreamableHTTPHandler` 返回的 handler 需要被挂载。Shell Executor 文档中示例 URL 为 `/sse`。*
- **Auth**: Shell Executor MCP 的 Client-Server 通信目前未强制要求 Token (仅集群内部节点通信使用 `cluster_token`)，但建议检查具体部署配置。
