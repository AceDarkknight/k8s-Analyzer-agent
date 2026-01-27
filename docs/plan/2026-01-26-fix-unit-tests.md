# 修复单元测试编译错误计划

## 1. 目标
修复项目中的单元测试编译错误，确保所有测试可以通过 `go test ./...` 正常运行。

## 2. 问题分析
目前的编译错误主要分为三类：
1.  **Mock 实现错误** (`internal/client/mocks/client_mock.go`): 使用了无效的 `mcp.Content` 复合字面量。
2.  **K8s 客户端测试错误** (`internal/client/k8s/client_test.go`): 访问了接口中不存在的私有字段 (`connected`, `config`) 和方法 (`UpdateConfig`)。
3.  **Shell 客户端测试错误** (`internal/client/shell/client_test.go`): 引用了未导入或未限定包名的错误类型 (`ConnectionError`, `ToolExecutionError`)。

## 3. 实施步骤

### 步骤 1: 修复 Mock 客户端 (`internal/client/mocks/client_mock.go`)

**问题**: `mcp.Content` 是一个接口，不能直接使用 `{Type: "text", ...}` 这种结构体字面量进行初始化。

**解决方案**:
- 将 `mcp.Content` 的字面量初始化替换为具体的实现类型 `mcp.TextContent`。
- 涉及的方法: `NewMockToolResult` 和 `NewMockToolWithError`。

**修改示例**:
```go
// 修改前
Content: []mcp.Content{
    {
        Type: "text",
        Text: text,
    },
},

// 修改后
Content: []mcp.Content{
    mcp.TextContent{
        Type: "text",
        Text: text,
    },
},
```

### 步骤 2: 修复 K8s 客户端测试 (`internal/client/k8s/client_test.go` 和 `internal/client/k8s/client.go`)

**问题 1**: 测试代码直接访问了 `client.connected` 和 `client.config` 私有字段，但 `NewClient` 返回的是 `Client` 接口。
**问题 2**: 测试代码调用了 `client.UpdateConfig`，但该方法未在 `Client` 接口中定义。

**解决方案**:
1.  **修改接口定义** (`internal/client/k8s/client.go`):
    - 在 `Client` 接口中添加 `UpdateConfig(config Config) error` 方法。
    - 确保 `MockClient` 实现了该方法 (如果尚未实现，需要添加或确认 `MockClient` 是否内嵌了支持该方法的结构)。*注意：检查发现 `MockClient` 似乎没有 `UpdateConfig`，需要为其添加一个简单的实现。*

2.  **修改测试代码** (`internal/client/k8s/client_test.go`):
    - 将所有 `client.connected` 访问改为调用 `client.IsConnected()`。
    - 将所有 `client.config` 访问改为调用 `client.GetConfig()`。

**MockClient 补充 (如果需要)**:
如果在 `internal/client/k8s/mock_client.go` 中 `MockClient` 没有 `UpdateConfig`，需要添加：
```go
func (c *MockClient) UpdateConfig(config Config) error {
    c.config = config
    c.connected = false // 更新配置后断开连接
    return nil
}
```

### 步骤 3: 修复 Shell 客户端测试 (`internal/client/shell/client_test.go`)

**问题**: 测试文件中直接使用了 `ConnectionError` 和 `ToolExecutionError`，但这些类型定义在 `internal/client` 包中，且测试文件未导入该包。

**解决方案**:
1.  **添加导入**: 在 `internal/client/shell/client_test.go` 中添加导入 `"github.com/AceDarkknight/k8s-analyzer-agent/internal/client"`。
2.  **限定类型名称**:
    - 将 `&ConnectionError{...}` 替换为 `&client.ConnectionError{...}`。
    - 将 `&ToolExecutionError{...}` 替换为 `&client.ToolExecutionError{...}`。

## 4. 验证

执行以下命令验证修复结果：
```bash
go test ./...
```
确保所有测试通过且无编译错误。
