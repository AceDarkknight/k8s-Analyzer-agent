# 添加 MCP Client 环境变量支持计划

## 背景
用户希望除了通过配置文件外，还可以通过环境变量配置 Shell 和 K8s MCP Client 的 URL 和 Token。
通过分析代码发现：
1. `internal/client/shell/client.go` 中的 `NewClientFromFile` 已经实现了从文件加载配置，但需要添加环境变量覆盖逻辑。
2. `internal/client/k8s/client.go` 中的 `NewClientFromFile` 目前使用硬编码的默认值，忽略了配置文件路径，这是一个需要修复的问题，同时需要添加环境变量支持。

## 目标
1. 修复 K8s Client 忽略配置文件的问题。
2. 为 Shell Client 添加 `SHELL_MCP_URL` 和 `SHELL_MCP_TOKEN` 环境变量支持。
3. 为 K8s Client 添加 `K8S_MCP_URL` 和 `K8S_MCP_TOKEN` 环境变量支持。
4. 确保环境变量优先级高于配置文件。

## 环境变量定义

| Client | 变量名 | 对应配置项 | 说明 |
|--------|--------|------------|------|
| Shell | `SHELL_MCP_URL` | `Servers[0].URL` | 覆盖第一个 Server 的 URL |
| Shell | `SHELL_MCP_TOKEN` | `Servers[0].Token` | 覆盖第一个 Server 的 Token |
| K8s | `K8S_MCP_URL` | `ServerURL` | 覆盖 K8s MCP Server 地址 |
| K8s | `K8S_MCP_TOKEN` | `Token` | 覆盖 K8s MCP 认证 Token |

## 实施步骤

### 1. 修改 Shell Client (`internal/client/shell/client.go`)

在 `NewClientFromFile` 函数中：
1. 保持现有的 `client.LoadConfig` 逻辑不变。
2. 读取 `os.Getenv("SHELL_MCP_URL")`。如果存在且非空，更新 `config.Servers[0].URL`。
   - 注意：确保 `config.Servers` 至少有一个元素（现有代码已有默认值处理逻辑，需确保在应用环境变量前执行）。
3. 读取 `os.Getenv("SHELL_MCP_TOKEN")`。如果存在且非空，更新 `config.Servers[0].Token`。

### 2. 修改 K8s Client (`internal/client/k8s/client.go`)

在 `NewClientFromFile` 函数中：
1. **修复 Bug**：移除硬编码的配置初始化，改为使用 `client.LoadConfig[Config](configPath)` 从文件加载配置。
2. 读取 `os.Getenv("K8S_MCP_URL")`。如果存在且非空，更新 `config.ServerURL`。
3. 读取 `os.Getenv("K8S_MCP_TOKEN")`。如果存在且非空，更新 `config.Token`。

## 验证计划

1. **Shell Client 测试**：
   - 场景 1：仅配置文件。验证使用配置文件中的值。
   - 场景 2：配置文件 + 环境变量。验证优先使用环境变量中的 URL 和 Token。
   - 场景 3：无配置文件（如果不通过 LoadConfig 加载则会报错，此次修改不涉及改变 LoadConfig 的错误处理，假设配置文件存在）。

2. **K8s Client 测试**：
   - 场景 1：仅配置文件。验证修复后能否正确读取配置文件。
   - 场景 2：配置文件 + 环境变量。验证优先使用环境变量。
