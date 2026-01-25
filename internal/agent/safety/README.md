# Safety Agent

## 概述

Safety Agent 是一个命令安全评估和执行 Agent，负责在执行 Shell 命令前进行安全性评估。它通过白名单、黑名单和危险模式匹配来防止执行危险命令。

## 功能特性

1. **白名单验证**：只允许执行配置文件中白名单内的命令
2. **黑名单过滤**：拦截黑名单中的危险命令
3. **危险模式检测**：使用正则表达式检测危险参数模式（如 `rm -rf /`）
4. **安全执行**：只有通过安全评估的命令才会被执行
5. **详细日志**：记录所有安全评估和命令执行过程

## 目录结构

```
internal/agent/safety/
├── README.md           # 本文件
├── validator.go        # 安全验证器实现
├── agent.go            # Safety Agent 主实现
└── agent_test.go       # 单元测试
```

## 核心组件

### Validator（验证器）

负责命令的安全评估，包括：
- 白名单检查
- 黑名单检查
- 危险模式匹配

### Agent（Agent）

负责命令的安全执行，包括：
- 调用验证器进行安全评估
- 调用 Shell Client 执行命令
- 返回执行结果或错误

## 配置

Safety Agent 使用 `bin/shell_config.json` 中的安全配置：

```json
{
  "security": {
    "allow_read_only": true,
    "command_whitelist": ["ls", "cat", "kubectl", ...],
    "blacklisted_commands": ["rm", "mkfs", "shutdown", ...],
    "dangerous_args_regex": ["rm\\s+-[a-zA-Z]*r[a-zA-Z]*\\s+/", ...]
  }
}
```

## 接口

### ExecuteSafeCommand

```go
func (a *Agent) ExecuteSafeCommand(ctx context.Context, command string) (string, error)
```

安全执行命令的接口，如果命令不安全则返回错误。

## 使用示例

```go
// 创建 Safety Agent
agent, err := safety.NewAgent(shellClient, configPath)
if err != nil {
    log.Fatal(err)
}

// 安全执行命令
output, err := agent.ExecuteSafeCommand(ctx, "ls -la")
if err != nil {
    log.Printf("命令执行失败: %v", err)
    return
}

fmt.Println(output)
```

## 安全策略

1. **白名单优先**：只有白名单中的命令才能执行
2. **黑名单拦截**：黑名单中的命令直接拒绝
3. **模式匹配**：危险参数模式会被拦截
4. **只读模式**：可配置为只读模式，禁止写操作

## 测试

运行单元测试：

```bash
go test ./internal/agent/safety/...
```

测试覆盖：
- 安全命令执行（白名单内）
- 不安全命令拦截（黑名单）
- 危险模式拦截（正则表达式）
- 错误处理
