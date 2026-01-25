# Mocks 模块

## 概述

Mocks 模块提供了 MCP Client 的 Mock 实现，用于单元测试和集成测试。这些 Mock 实现模拟了真实 MCP Server 的行为，使测试更加可靠和高效。

## 目录结构

```
internal/client/mocks/
└── README.md          # 本文件
└── client_mock.go     # Mock Client 实现
```

## Mock 实现

### MockClient

`MockClient` 是 `MCPClient` 接口的通用 Mock 实现，提供了以下功能：

- **连接状态模拟**: 可以模拟已连接和未连接状态
- **工具列表模拟**: 可以设置可用的工具列表
- **工具调用结果模拟**: 可以为每个工具设置预定义的返回结果
- **自定义函数**: 可以注入自定义的连接、关闭、工具调用等函数
- **错误模拟**: 可以模拟各种错误场景

#### 基本用法

```go
import "github.com/your-org/k8s-analyzer-agent/internal/client/mocks"

// 创建 Mock Client
mockClient := mocks.NewMockClient()

// 设置可用工具
mockClient.SetTools([]mcp.Tool{
    mocks.NewMockTool("test_tool", "Test tool description", nil),
})

// 设置工具调用结果
mockClient.SetToolResult("test_tool", mocks.NewMockToolResult(`{"status": "success"}`))

// 连接
err := mockClient.Connect(context.Background())
assert.NoError(t, err)

// 调用工具
result, err := mockClient.CallTool(context.Background(), "test_tool", nil)
assert.NoError(t, err)
assert.Equal(t, `{"status": "success"}`, result.Content[0].Text)

// 关闭连接
err = mockClient.Close()
assert.NoError(t, err)
```

#### 使用自定义函数

```go
// 创建 Mock Client
mockClient := mocks.NewMockClient()

// 设置自定义工具调用函数
mockClient.CallToolFunc = func(ctx context.Context, name string, args map[string]interface{}) (*mcp.CallToolResult, error) {
    if name == "failing_tool" {
        return nil, fmt.Errorf("tool failed")
    }
    return mocks.NewMockToolResult(`{"result": "ok"}`), nil
}

// 调用工具
_, err := mockClient.CallTool(context.Background(), "failing_tool", nil)
assert.Error(t, err)
```

#### 模拟错误

```go
// 创建 Mock Client
mockClient := mocks.NewMockClient()

// 设置全局错误
mockClient.SetError(fmt.Errorf("connection failed"))

// 连接会失败
err := mockClient.Connect(context.Background())
assert.Error(t, err)
```

### MockK8sClient

`MockK8sClient` 是 K8s Client 的专用 Mock 实现，扩展了 `MockClient` 并添加了 K8s 特定的模拟数据：

- `ClusterStatus`: 模拟集群状态
- `Pods`: 模拟 Pod 列表
- `Services`: 模拟 Service 列表
- `Deployments`: 模拟 Deployment 列表
- `Nodes`: 模拟节点列表
- `Events`: 模拟事件列表
- `Logs`: 模拟日志
- `RBACPermission`: 模拟 RBAC 权限

#### 基本用法

```go
import "github.com/your-org/k8s-analyzer-agent/internal/client/mocks"

// 创建 Mock K8s Client
mockK8sClient := mocks.NewMockK8sClient()

// 设置集群状态
mockK8sClient.ClusterStatus = map[string]interface{}{
    "version":         "v1.28.0",
    "node_count":      3,
    "namespace_count": 5,
}

// 设置 Pod 列表
mockK8sClient.Pods = []map[string]interface{}{
    {
        "name":      "nginx-pod",
        "namespace": "default",
        "status":    "Running",
    },
}

// 连接
err := mockK8sClient.Connect(context.Background())
assert.NoError(t, err)
```

### MockShellClient

`MockShellClient` 是 Shell Client 的专用 Mock 实现，扩展了 `MockClient` 并添加了 Shell 特定的模拟数据：

- `ExecuteResult`: 模拟命令执行结果

#### 基本用法

```go
import "github.com/your-org/k8s-analyzer-agent/internal/client/mocks"

// 创建 Mock Shell Client
mockShellClient := mocks.NewMockShellClient()

// 设置命令执行结果
mockShellClient.ExecuteResult = map[string]interface{}{
    "summary": "Executed on 3 nodes, 2 groups found",
    "groups": []map[string]interface{}{
        {
            "count":  2,
            "status": "success",
            "output": "output1",
            "nodes":  []string{"node1", "node2"},
        },
    },
}

// 连接
err := mockShellClient.Connect(context.Background())
assert.NoError(t, err)
```

## 辅助函数

### NewMockToolResult

创建 Mock 工具调用结果：

```go
result := mocks.NewMockToolResult(`{"status": "success"}`)
```

### NewMockTool

创建 Mock 工具：

```go
tool := mocks.NewMockTool("tool_name", "Tool description", inputSchema)
```

### NewMockToolWithError

创建带有错误的 Mock 工具调用结果：

```go
result := mocks.NewMockToolWithError("tool execution failed")
```

## 最佳实践

1. **隔离测试**: 每个测试用例都应该创建新的 Mock 实例，避免测试之间的相互影响
2. **重置状态**: 在测试之间使用 `Reset()` 方法重置 Mock 状态
3. **明确预期**: 为每个工具调用设置明确的预期结果
4. **测试错误场景**: 使用 Mock 模拟各种错误场景，测试错误处理逻辑
5. **使用自定义函数**: 对于复杂的测试逻辑，使用自定义函数而不是预定义结果

## 示例

### 完整的测试示例

```go
func TestMyFunction(t *testing.T) {
    // 创建 Mock Client
    mockClient := mocks.NewMockClient()

    // 设置工具
    mockClient.SetTools([]mcp.Tool{
        mocks.NewMockTool("list_pods", "List pods", nil),
    })

    // 设置工具结果
    mockClient.SetToolResult("list_pods", mocks.NewMockToolResult(`[
        {"name": "pod1", "status": "Running"},
        {"name": "pod2", "status": "Pending"}
    ]`))

    // 连接
    err := mockClient.Connect(context.Background())
    require.NoError(t, err)
    defer mockClient.Close()

    // 调用被测试的函数
    pods, err := ListPods(mockClient, "default")
    require.NoError(t, err)
    assert.Len(t, pods, 2)
}
```

## 注意事项

1. Mock 实现仅用于测试，不应在生产代码中使用
2. 确保测试覆盖了正常路径和错误路径
3. 定期更新 Mock 实现以保持与真实 API 的同步
4. 对于集成测试，考虑使用真实的 MCP Server 而不是 Mock
