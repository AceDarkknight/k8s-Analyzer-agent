# bin 目录

本目录用于存放项目编译后的二进制文件和配置文件。

## 文件说明

### 二进制文件

- `k8s-analyzer.exe` / `k8s-analyzer` - K8s 分析 Agent 主程序
  - 智能分析 Agent 的主入口点
  - 集成 K8s MCP Client 和 Shell MCP Client
  - 负责执行具体的分析任务，如 Pod 状态检查、日志分析等
  - 依赖 `k8s_config.json` 和 `shell_config.json` 进行配置

- `k8s-mcp.exe` / `k8s-mcp` - Kubernetes MCP 服务器二进制文件
  - 用于 Kubernetes 集群管理和资源查看
  - 支持 Pod、Service、Deployment 等资源的查询和操作
  - 需要配置 kubeconfig 或使用集群内配置

- `shell-executor-mcp.exe` / `shell-executor-mcp` - Shell 执行器 MCP 服务器二进制文件
  - 用于在服务器集群上执行 Shell 命令
  - 支持命令白名单和安全控制
  - 支持集群分发和故障转移

### 配置文件

- `k8s_config.json` - K8s MCP 配置文件
  - `port`: 监听端口（默认 8443）
  - `insecure`: 是否使用不安全的 HTTP 模式（开发环境设为 true）
  - `token`: 认证 Token
  - `kubeconfig`: kubeconfig 文件路径（可选，默认使用集群内配置）
  - `log_level`: 日志级别（info/debug/error）

- `shell_config.json` - Shell Executor MCP 配置文件
  - `port`: 监听端口（默认 8080）
  - `node_name`: 节点名称
  - `peers`: 对等节点列表（用于集群通信）
  - `cluster_token`: 集群认证 Token
  - `security.allow_read_only`: 是否允许只读命令（true）
  - `security.command_whitelist`: 允许执行的命令白名单
  - `security.blacklisted_commands`: 禁止执行的命令黑名单
  - `security.dangerous_args_regex`: 危险参数正则表达式
  - `log`: 日志配置

## 使用说明

### 启动 K8s Analyzer

```bash
# Windows
k8s-analyzer.exe

# Linux/Mac
./k8s-analyzer
```

### 启动 K8s MCP

```bash
# Windows
k8s-mcp.exe --config k8s_config.json

# Linux/Mac
./k8s-mcp --config k8s_config.json
```

### 启动 Shell Executor MCP

```bash
# Windows
shell-executor-mcp.exe --config shell_config.json

# Linux/Mac
./shell-executor-mcp --config shell_config.json
```

## 安全说明

- Shell Executor MCP 配置了只读模式，仅允许执行安全的只读命令
- 命令白名单包含常用只读命令：ls, cat, kubectl, grep 等
- 危险命令和参数已被列入黑名单
- 生产环境请使用 HTTPS 和强密码
