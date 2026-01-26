# Kubernetes MCP Client

本包 (`internal/client/k8s`) 提供了与 Kubernetes MCP Server 交互的客户端实现。

## 功能

*   **MCP 连接**: 负责建立和维护与 K8s MCP Server 的连接。
*   **工具调用**: 封装了 MCP 协议的 Tool Call，允许上层 Agent 方便地调用 K8s 相关的工具（如查询 Pod、Service 状态）。
*   **资源发现**: 支持通过 MCP Server 获取集群内的资源信息。

## 主要组件

*   `client.go`: 客户端核心逻辑实现。
*   `tools.go`: 定义了可用的 K8s 工具集。
