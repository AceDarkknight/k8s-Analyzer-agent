# Shell Executor MCP Client

本包 (`internal/client/shell`) 提供了与 Shell Executor MCP Server 交互的客户端实现。

## 功能

*   **MCP 连接**: 负责建立和维护与 Shell Executor MCP Server 的连接。
*   **命令执行**: 允许上层 Agent 通过 MCP 协议请求执行 Shell 命令。
*   **安全集成**: 与 `internal/agent/safety` 配合，确保只有通过安全验证的命令才会被发送到执行器。

## 主要组件

*   `client.go`: 客户端核心逻辑实现。
*   `tools.go`: 定义了 Shell 执行相关的工具接口。
