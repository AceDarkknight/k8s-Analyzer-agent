# Kubernetes MCP Client

本包 (`internal/client/k8s`) 提供了与 Kubernetes MCP Server 交互的客户端实现。

## 功能

*   **MCP 连接**: 负责建立和维护与 K8s MCP Server 的连接。
*   **工具调用**: 封装了 MCP 协议的 Tool Call，允许上层 Agent 方便地调用 K8s 相关的工具（如查询 Pod、Service 状态）。
*   **资源发现**: 支持通过 MCP Server 获取集群内的资源信息。
*   **环境变量配置**: 支持通过环境变量覆盖配置文件中的设置。

## 配置

客户端支持通过配置文件和环境变量进行配置。环境变量的优先级高于配置文件。

### 环境变量

| 环境变量 | 描述 | 示例 |
|---------|------|------|
| `K8S_MCP_URL` | K8s MCP Server 的地址 | `https://localhost:8443` |
| `K8S_MCP_TOKEN` | 认证 Token | `your-auth-token` |

### 配置优先级

1. 环境变量（如果设置）
2. 配置文件中的值

## 主要组件

*   `client.go`: 客户端核心逻辑实现。
*   `tools.go`: 定义了可用的 K8s 工具集。
