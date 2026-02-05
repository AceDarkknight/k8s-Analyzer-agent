# Shell Executor MCP Client

本包 (`internal/client/shell`) 提供了与 Shell Executor MCP Server 交互的客户端实现。

## 功能

*   **MCP 连接**: 负责建立和维护与 Shell Executor MCP Server 的连接。
*   **命令执行**: 允许上层 Agent 通过 MCP 协议请求执行 Shell 命令。
*   **安全集成**: 与 `internal/agent/safety` 配合，确保只有通过安全验证的命令才会被发送到执行器。
*   **环境变量配置**: 支持通过环境变量覆盖配置文件中的设置。

## 配置

客户端支持通过配置文件和环境变量进行配置。环境变量的优先级高于配置文件。

### 环境变量

| 环境变量 | 描述 | 示例 |
|---------|------|------|
| `SHELL_MCP_URL` | Shell Executor MCP Server 的地址 | `http://localhost:8080` |
| `SHELL_MCP_TOKEN` | 认证 Token（可选） | `your-auth-token` |

### 配置优先级

1. 环境变量（如果设置）
2. 配置文件中的值
3. 代码中的默认值

## 主要组件

*   `client.go`: 客户端核心逻辑实现。
*   `tools.go`: 定义了 Shell 执行相关的工具接口。
