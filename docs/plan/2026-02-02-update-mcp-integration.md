# MCP 客户端集成计划

**日期：** 2026-02-02
**状态：** 已完成

## 1. 目标

将 `github.com/AceDarkknight/k8s-mcp` 和 `github.com/AceDarkknight/shell-executor-mcp` 集成到 `k8s-analyzer-agent` 中，以替换并增强 `internal/client` 中现有的自定义 MCP 客户端实现。

## 2. 背景

目前，`internal/client/k8s` 使用的是模拟客户端（mock client），缺乏真实的实现。`internal/client/shell` 使用 `modelcontextprotocol/go-sdk` 进行了自定义实现。目标是通过利用 MCP 服务端仓库提供的 SDK (`pkg/mcpclient`)，来标准化客户端实现，减少重复代码并确保与服务端的最佳兼容性。

## 3. 实现步骤

### 第一阶段：依赖管理

1.  **更新 `go.mod`**：
    *   添加 `github.com/AceDarkknight/k8s-mcp`
    *   添加 `github.com/AceDarkknight/shell-executor-mcp`
    *   运行 `go mod tidy` 以解析依赖。

### 第二阶段：重构 `internal/client/k8s`

1.  **移除模拟实现**：
    *   重构 `internal/client/k8s/client.go`，移除旧的模拟实现逻辑。
    *   保留 `MockClient` 仅用于测试目的。

2.  **集成 K8s MCP SDK**：
    *   在 `internal/client/k8s/client.go` 中，引入 `github.com/AceDarkknight/k8s-mcp/pkg/mcpclient`。
    *   **初始化客户端**（参考 SDK Basic Usage）：
        ```go
        // 示例初始化逻辑
        import (
            "github.com/AceDarkknight/k8s-mcp/pkg/mcpclient"
        )

        // 创建配置
        config := mcpclient.Config{
            ServerURL:          "https://localhost:8443", // 应从 agent 配置文件读取
            AuthToken:          "your-token",             // 应从 agent 配置文件读取
            InsecureSkipVerify: true,                     // 根据配置决定
        }

        // 创建客户端
        client, err := mcpclient.NewClient(config)
        if err != nil {
            return nil, err
        }
        
        // 连接服务器
        // 注意：连接管理可能需要根据 agent 的生命周期进行调整
        if err := client.Connect(ctx); err != nil {
            client.Close()
            return nil, err
        }
        ```
    *   **封装方法**：
        *   实现 `internal/client/client.go` 中定义的接口方法（如 `CallTool`）。
        *   直接委托给 `mcpclient` 的 `ListTools` 和 `CallTool` 方法。

### 第三阶段：重构 `internal/client/shell`

1.  **移除旧实现**：
    *   移除 `internal/client/shell` 中基于 raw `go-sdk` 的自定义实现。

2.  **集成 Shell Executor MCP SDK**：
    *   在 `internal/client/shell/client.go` 中，引入 `github.com/AceDarkknight/shell-executor-mcp/pkg/mcpclient`。
    *   **初始化客户端**（参考 SDK Basic Usage）：
        ```go
        // 示例初始化逻辑
        import (
            "github.com/AceDarkknight/shell-executor-mcp/pkg/configs"
            "github.com/AceDarkknight/shell-executor-mcp/pkg/mcpclient"
        )

        // 构造配置 (可以直接构造 struct 或从文件加载)
        cfg := &configs.ClientConfig{
            Servers: []configs.ServerConfig{
                {
                    Name: "primary",
                    URL:  "http://localhost:8080", // 应从 agent 配置文件读取
                },
            },
            Log: configs.LogConfig{
                Level: "info",
            },
        }

        // 创建客户端
        // 可选：使用 mcpclient.WithLogger 适配 agent 自身的 logger
        client, err := mcpclient.NewClient(cfg) 
        if err != nil {
            return nil, err
        }

        // 连接服务器
        if err := client.Connect(ctx); err != nil {
            client.Close()
            return nil, err
        }
        ```
    *   **封装方法**：
        *   使用 SDK 提供的 `ExecuteCommand` 方法来实现 Shell 命令执行。
        *   注意：SDK 的 `ExecuteCommand` 返回 `*Result` 结构体，需要将其转换为 agent 期望的格式。

### 第四阶段：通用逻辑与测试

1.  **统一接口适配**：
    *   确保 `internal/client/k8s` 和 `internal/client/shell` 的新实现都严格遵守 `internal/client/client.go` 中的 `MCPClient` 接口。
    *   如果接口定义与 SDK 方法签名有差异，需要在 wrapper 层进行转换。

2.  **集成测试**：
    *   更新集成测试以使用真实的 SDK 初始化流程（可能需要 mock SDK 的底层连接或使用 SDK 提供的 mock 机制，如果存在）。
    *   验证 `k8s-mcp` 的工具调用和 `shell-executor-mcp` 的命令执行是否正常工作。

## 4. 测试策略

*   **单元测试**：
    *   由于引入了第三方 SDK，单元测试重点将转向测试“我们对 SDK 的封装逻辑”以及“配置转换逻辑”。
    *   尝试 mock SDK 的接口（如果它是接口类型）或使用 Go 的 interface wrapper 来方便 mock。
*   **集成测试**：
    *   需要运行真实的 `k8s-mcp` 和 `shell-executor-mcp` 服务端进行端到端测试。

## 5. 文档更新

*   更新 `docs/requirements.md` 以反映对 `k8s-mcp` 和 `shell-executor-mcp` SDK 的强依赖。
*   更新 `docs/architecture.md` 说明不再维护自定义 MCP 协议实现，而是使用官方/第三方 SDK。
