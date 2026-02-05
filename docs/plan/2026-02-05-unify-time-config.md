# 计划：统一时间配置为整数秒 (int 类型)

## 背景
用户要求统一时间配置（超时、重试参数）为整数秒，并且明确指示不要使用自定义的 `Duration` 类型，而是直接使用 `int` 类型来表示秒。仅在使用这些配置值时，将其转换为 `time.Duration`。

## 目标
1.  修改 `RetryConfig` 和客户端 `Config` 中的时间字段，将其类型改为 `int`（单位：秒）。
2.  在逻辑代码中，将 `int` 类型的秒转换为 `time.Duration` 进行使用。
3.  移除对自定义 `client.Duration` 类型的依赖（针对这些配置字段）。

## 变更方案

### 1. 修改 `internal/client/retry.go`
*   **修改 `RetryConfig` 结构体**：
    将 `InitialDelay` 和 `MaxDelay` 字段的类型改为 `int`。
    ```go
    type RetryConfig struct {
        // ...
        InitialDelay int `json:"initial_delay"` // 秒
        MaxDelay     int `json:"max_delay"`     // 秒
        // ...
    }
    ```
*   **更新 `DefaultRetryConfig`**：
    将默认值改为整数秒（例如 `1`, `10`）。
*   **更新重试逻辑**：
    在 `RetryWithBackoff`, `RetryWithContext`, `RetryWithResult` 中，使用时将 `int` 转换为 `time.Duration`。
    例如：`delay := time.Duration(config.InitialDelay) * time.Second`。

### 2. 修改 `internal/client/k8s/client.go`
*   **修改 `Config` 结构体**：
    将 `Timeout` 字段类型从 `client.Duration` 改为 `int`。
    ```go
    type Config struct {
        // ...
        Timeout int `json:"timeout"` // 秒
        // ...
    }
    ```
*   **更新 `Validate` 方法**：
    默认值设为整数（例如 `30`）。
    验证逻辑中，检查 `c.Timeout <= 0`。
*   **更新 `NewRealClient` / HTTP Client 创建**：
    在使用 `Config.Timeout` 的地方，将其转换为 `time.Duration`：`time.Duration(config.Timeout) * time.Second`。

### 3. 修改 `internal/client/shell/client.go`
*   **修改 `Config` 结构体**：
    将 `Timeout` 字段类型从 `client.Duration` 改为 `int`。
    ```go
    type Config struct {
        // ...
        Timeout int `json:"timeout"` // 秒
        // ...
    }
    ```
*   **更新 `Validate` 方法**：
    默认值设为整数（例如 `30`）。
*   **更新 `UpdateConfig` 和 HTTP Client 创建**：
    将 `int` 转换为 `time.Duration`。

## 验证计划
1.  运行测试确保逻辑正确。
2.  确认 JSON 配置文件中的整数值（如 `30`）被正确解析并在程序中作为 30 秒生效。
