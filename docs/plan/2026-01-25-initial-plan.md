# 项目实施计划 (2026-01-25)

## 1. 计划概述
本计划旨在明确 "K8s Analyzer Agent" 项目的初始开发阶段任务。重点在于搭建基础开发环境，集成核心的 MCP 工具，并实现主 Agent 与安全命令子 Agent 的基本交互流程。

**目标**：完成 MVP (Minimum Viable Product) 版本，实现基础的 K8s 信息查询和安全的命令执行诊断。

## 2. 详细步骤拆解

### 阶段一：环境准备与项目初始化

#### 1.1 初始化 Golang 项目
- **操作**：
    - 创建项目根目录 `k8s-analyzer-agent`（如尚未存在）。
    - 运行 `go mod init` 初始化模块。
    - 建立基础目录结构：`cmd/`, `internal/agent/`, `internal/mcp/`, `pkg/utils/`。
- **验证**：项目可编译，目录结构清晰。

#### 1.2 Eino 框架引入
- **操作**：
    - 引入 Eino 框架依赖。
    - 搭建一个最简单的 Eino Graph 示例，确保框架运行正常。

#### 1.3 安装与配置 MCP Servers
- **操作**：
    - **K8s MCP**:
        - 进入 `temp_mcp/k8s-mcp`，编译 `cmd/server` 生成 `k8s-mcp-server` 二进制文件。
        - 准备 kubeconfig 文件（或使用默认）。
        - 生成测试用的 TLS 证书（`cert.pem`, `key.pem`）。
    - **Shell Executor MCP**:
        - 进入 `temp_mcp/shell-executor-mcp`，编译 `cmd/server` 生成 `shell-mcp-server` 二进制文件。
        - 创建 `server_config.json`，配置监听端口和安全策略（如禁止 `rm` 命令）。
- **验证**：手动启动两个 Server，确保端口监听正常且无报错。

### 阶段二：MCP 工具集成 (Infrastructure Layer)

#### 2.1 集成 K8s MCP
- **操作**：
    - 参考 `temp_mcp/k8s-mcp/cmd/client/` 中的源码，了解连接建立和工具调用的实现细节。
    - 编写 `internal/mcp/k8s_client.go`。
    - 实现与 `k8s-mcp` Server 的连接配置。
    - 封装基础工具函数：`ListPods`, `GetNodeStatus`。
- **验证**：编写单元测试，模拟 MCP Server 响应，确认能正确解析 K8s 数据。

#### 2.2 集成 Shell Executor MCP
- **操作**：
    - 参考 `temp_mcp/shell-executor-mcp/cmd/client/` 中的源码，特别是 Transport 层和连接管理的实现。
    - 编写 `internal/mcp/shell_client.go`。
    - 实现与 `shell-executor-mcp` Server 的连接配置。
    - 封装基础工具函数：`ExecuteCommand`。
- **验证**：编写单元测试，确认能发送命令并接收输出。

### 阶段三：Agent 核心逻辑开发 (Core Logic Layer)

#### 3.1 开发安全命令执行子 Agent
- **操作**：
    - 定义子 Agent 的 Input/Output 结构体。
    - 实现 `SecurityEvaluator` 模块：
        - 编写正则表达式或规则集，定义“白名单”命令（如 `curl`, `nslookup`, `cat`, `grep`）。
        - 定义“黑名单”操作（如 `rm`, `mv`, `>`）。
    - 编排子 Agent 流程：接收请求 -> 安全评估 -> (通过)调用 Shell MCP -> 返回结果。
- **验证**：
    - 测试用例覆盖安全场景：确认 `curl` 被允许，`rm -rf /` 被拒绝。

#### 3.2 开发主分析 Agent
- **操作**：
    - 定义主 Agent 的 Graph 结构。
    - 实现 `Planner` 节点：解析用户 Query，生成执行计划。
    - 集成 K8s MCP Tool 和 子 Agent Tool 到主 Agent 的 ToolSet 中。
    - 实现 `Reporter` 节点：将收集到的数据拼接成 Prompt，调用 LLM 生成 Markdown 报告。

### 阶段四：集成测试与优化

#### 4.1 联调测试
- **操作**：
    - 启动本地 Mock 的 MCP Servers (或连接真实测试环境)。
    - 运行完整的 Agent 流程。
    - 测试场景：
        1. "查询 default 命名空间的所有 Pod" (仅 K8s MCP)。
        2. "检查 Service A 的 ClusterIP 是否可通" (K8s MCP + Shell Agent)。
- **验证**：系统能正确输出包含真实数据的 Markdown 报告。

#### 4.2 文档完善
- **操作**：
    - 更新 `README.md`，补充运行指南。
    - 记录开发过程中遇到的问题和解决方案。

#### 4.3 清理临时资源
- **操作**：
    - 删除 `temp_mcp` 目录及其下的所有内容，保持项目仓库的整洁。
- **验证**：确认 `temp_mcp` 目录已移除，且项目核心功能不受影响（必要的代码应已集成到 `internal/mcp` 或其他模块中）。

## 3. 风险评估
- **MCP 连接稳定性**：需考虑 MCP Server 连接断开的重连机制。
- **安全规则完备性**：正则表达式可能无法覆盖所有恶意 Shell 注入变种，初期需采取严格白名单策略。

## 4. 下一步计划
- 待用户确认本计划后，切换到 Code 模式开始执行“阶段一”。

## 5. 常见问题与决策记录 (2026-01-25)

针对用户提出的核心问题，在此进行统一记录与解答，作为后续开发的指导依据。

### Q1: 为什么采用图（Graph）结构编排 Agent 逻辑？

**A:** 在 Kubernetes 故障排查和复杂系统诊断场景中，Graph (图) 结构相比传统的链式 (Chain) 结构具有显著优势：
1.  **非线性流程支持**：排查过程往往充满回溯和分支（例如：发现 Pod 正常 -> 检查 Service -> 发现 Service 异常 -> 回溯检查 Endpoints）。Graph 允许节点间的任意跳转，完美契合这种探索性过程。
2.  **状态机模型**：每个节点可视为一个独立的状态（如“日志分析状态”、“网络测试状态”），边代表状态流转条件。这使得状态管理更加清晰且类型安全。
3.  **模块化与扩展性**：新增诊断能力只需增加一个图节点，不影响现有主流程。
4.  **框架适配**：Eino 等现代 Agent 框架原生支持 Graph，能提供可视化的调试能力和持久化的状态保存（这对长耗时诊断任务至关重要）。

### Q2: 是否阅读了 k8s-mcp 和 shell-executor-mcp 的相关文档和接口？

**A:** 是的，已深入分析了这两个项目的源码和文档：
*   **k8s-mcp**: 重点分析了 `internal/mcp/server.go` 中注册的工具（如 `get_pod_logs`, `check_rbac_permission`）以及其基于 Token 的认证机制。
*   **shell-executor-mcp**: 重点分析了 `cmd/client/cmd/run.go` 和 `internal/dispatch/dispatcher.go`，理解了其 SSE 连接建立过程、`execute_command` 工具的调用方式以及安全性设计。
*   **结论**: 现有的工具集（Tools）和接口定义完全满足 Agent 的需求。

### Q3: 关于参考现有 MCP Client 实现 Agent Client 的计划

**A:** Agent 的 Client 模块将直接借鉴现有 CLI Client 的成熟实现：
*   **Transport 层**: 复用 `mcp.StreamableClientTransport` (基于 SSE)，确保与现有 Server 的兼容性。
*   **连接管理**: 采用与 `shell-executor-mcp` 客户端一致的多 Server 连接管理策略，支持同时连接 K8s 和 Shell 服务。
*   **接口定义**: 已在 `docs/architecture.md` 中更新了 Golang Interface 定义，该接口是对 SDK `mcp.Client` 的高层封装，旨在统一不同 Server 的调用方式并注入必要的认证信息（如 Bearer Token）。

### Q4: 架构如何支持多次与 MCP 或子 Agent 的交互？

**A:** 架构设计中已明确引入了**循环（Loop）和决策（Decision）机制**来支持多轮交互，而非简单的线性执行：
1.  **Graph 循环设计**: 主 Agent 采用 Eino Graph 的循环模式（`Decision Node` <-> `Tool/Agent Execution`）。每次执行完工具或子 Agent 后，流程会返回到决策节点，携带最新的执行结果作为上下文。
2.  **动态决策**: 决策节点会评估当前上下文：
    *   如果信息不足（例如：获取 Pod 发现 CrashLoopBackOff，但需要查看日志），它会决定再次调用 K8s MCP 获取 Logs。
    *   如果需要验证（例如：怀疑网络不通），它会决定调用 Shell 子 Agent 执行 `curl`。
    *   只有当“信息充足”或“达到最大迭代次数”时，才会流转到报告生成节点。
3.  **死循环防护**: 为了防止 Agent 陷入无限重试，架构中强制设置了 `MaxIterations`（最大迭代次数，如 10 次），确保任务最终能够终止并返回已有结果。

### Q5: 系统的异常处理原则是什么？

**A:** 系统遵循**“鲁棒性优先，优雅降级”**的异常处理原则（详见 `docs/architecture.md` 第7章）：
1.  **分层处理**: 针对 MCP 连接层、命令执行层、K8s API 层和 Agent 内部逻辑层分别制定了处理策略。
2.  **优雅降级**: 当非核心组件（如 Shell Executor）不可用时，系统不会直接崩溃，而是切换到“受限模式”继续提供基础分析服务。
3.  **透明反馈**: 所有的异常（如权限不足、命令被安全拦截）都会转化为清晰的 Observation 或报告内容，明确告知用户当前受限的原因，而非简单的内部报错。

### Q6: 如何应对 Agent 任务意外中断或 LLM 服务不稳定的情况？

**A:** 系统在架构层面（详见文档 7.5 & 7.6 节）引入了双重保障机制：
1.  **Agent 状态恢复**: 利用 Graph Checkpointer 在每个节点执行后持久化上下文。若任务因 Crash 或手动停止而中断，重启后可检测 `ThreadID` 并加载最新的 Checkpoint，实现“断点续传”，避免重复执行耗时步骤。
2.  **LLM 韧性设计**:
    *   **连接容错**: 遇到 5xx 或超时，执行指数退避重试；连续失败则自动切换备用模型（Fallback Model）。
    *   **格式修正**: 针对 JSON 解析失败，Agent 会触发 Self-Correction 流程，将错误信息反馈给 LLM 要求其重新生成合规格式。
