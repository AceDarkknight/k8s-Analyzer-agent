# 计划：支持 Main Agent 和 Sub Agent 使用不同的 LLM 模型

## 1. 背景
目前 `k8s-analyzer-agent` 项目中的 Analysis Agent (Main Agent) 和 Safety Agent (Sub Agent) 均使用基于规则或 Mock 的 LLM 实现 (`RuleBasedLLM`, `RuleBasedAuditor`)。为了提升分析能力和安全性，我们需要支持接入真实的 LLM 服务（如 OpenAI 兼容接口），并且允许为不同的 Agent 配置不同的 LLM 模型（例如，Analysis Agent 使用推理能力更强的模型，Safety Agent 使用更快速或专门的安全模型）。

## 2. 目标
- 定义通用的 LLM 配置结构。
- 实现一个通用的 LLM 客户端（支持 OpenAI 兼容接口）。
- 在 `Analysis Agent` 和 `Safety Agent` 中集成该通用 LLM 客户端。
- 修改程序入口 `main.go`，支持从配置文件加载 LLM 配置，并分别为两个 Agent 实例化不同的 LLM 客户端。

## 3. 设计方案

### 3.1 配置结构设计
在 `internal/config` 包（需新建或在现有 `internal/client/config.go` 基础上扩展）中定义 LLM 配置。

```go
// internal/config/llm_config.go

type LLMConfig struct {
    Provider    string  `json:"provider"`     // 例如 "openai", "azure", "deepseek"
    BaseURL     string  `json:"base_url"`     // API 基础地址
    APIKey      string  `json:"api_key"`      // API Key (建议支持从环境变量读取)
    Model       string  `json:"model"`        // 模型名称
    Temperature float64 `json:"temperature"`  // 温度
    MaxTokens   int     `json:"max_tokens"`   // 最大 Token 数
}

type AgentLLMConfig struct {
    Analysis LLMConfig `json:"analysis"` // Analysis Agent 的 LLM 配置
    Safety   LLMConfig `json:"safety"`   // Safety Agent 的 LLM 配置
}
```

配置文件 `bin/llm_config.json` 示例：
```json
{
  "analysis": {
    "provider": "openai",
    "base_url": "https://api.openai.com/v1",
    "api_key": "sk-...",
    "model": "gpt-4-turbo",
    "temperature": 0.7,
    "max_tokens": 2000
  },
  "safety": {
    "provider": "openai",
    "base_url": "https://api.openai.com/v1",
    "api_key": "sk-...",
    "model": "gpt-3.5-turbo",
    "temperature": 0.0,
    "max_tokens": 500
  }
}
```

### 3.2 通用 LLM 客户端实现
在 `internal/llm` 包中实现通用的 LLM 客户端。

- **接口定义**:
  ```go
  type Client interface {
      ChatCompletion(ctx context.Context, messages []Message) (string, error)
  }
  ```

- **OpenAI 实现**:
  使用 `github.com/sashabaranov/go-openai` 或标准 HTTP 库实现。

### 3.3 Agent 集成

#### Analysis Agent (`internal/agent/analysis`)
- 修改 `LLM` 接口的具体实现（新建 `OpenAILLM` 结构体实现 `LLM` 接口）。
- `OpenAILLM` 内部持有 `internal/llm.Client`。
- `Analyze`, `MakeDecision`, `GenerateReport` 方法将通过 Prompt 调用 LLM。

#### Safety Agent (`internal/agent/safety`)
- 修改 `LLMAuditor` 接口的具体实现（新建 `OpenAIAuditor` 结构体实现 `LLMAuditor` 接口）。
- `OpenAIAuditor` 内部持有 `internal/llm.Client`。
- `AuditCommand` 方法将构造专门的 Prompt 进行安全审计。

## 4. 实施步骤

### 阶段一：基础架构准备
1.  **创建配置包**: 在 `internal/config` 中定义 `LLMConfig` 和 `AgentLLMConfig` 结构体，并实现加载逻辑。
2.  **实现通用 LLM 客户端**: 创建 `internal/llm` 包，实现基于 OpenAI 接口的客户端。

### 阶段二：Agent 改造
3.  **改造 Analysis Agent**:
    - 在 `internal/agent/analysis/llm.go` 中添加 `OpenAILLM` 实现。
    - 编写 Prompt 模板用于 `MakeDecision`, `Analyze`, `GenerateReport`。
4.  **改造 Safety Agent**:
    - 在 `internal/agent/safety/validator.go` (或新建 `audit_llm.go`) 中添加 `OpenAIAuditor` 实现。
    - 编写 Prompt 模板用于 `AuditCommand`。

### 阶段三：集成与配置
5.  **更新入口文件 `main.go`**:
    - 读取 `bin/llm_config.json`。
    - 根据配置分别初始化 Analysis Agent 和 Safety Agent 的 LLM 客户端。
    - 将 LLM 实例注入到 Agent 中。

### 阶段四：测试验证
6.  **单元测试**: 为新的 Config 加载、LLM 客户端、Prompt 生成逻辑编写单元测试。
7.  **集成测试**: 配置 Mock Server 或使用真实 Key 验证完整流程。

## 5. 验证计划
- 运行单元测试确保代码逻辑正确。
- 创建 `bin/llm_config.json` 配置文件。
- 运行 `k8s-analyzer`，观察日志，确认 Analysis Agent 和 Safety Agent 分别使用了配置的模型进行交互。
