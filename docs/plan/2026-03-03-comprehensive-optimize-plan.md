# 通用 LLM 自动化诊断与环境适配闭环计划 (2026-03-03)

## 1. 背景与目标
在 K8s 巡检中，LLM 往往仅停留在获取 K8s 资源摘要的层面。当遇到深度故障（如组件连接失败、性能瓶颈、配置冲突）时，缺乏主动利用底层工具进行验证和修复的通用倾向。
本计划旨在构建一套**通用推理框架**，使 LLM 能够针对**任何**诊断场景，自动实现从"云原生视图"到"宿主机/网络视图"的深度联动与闭环。

## 2. 通用核心优化原则

### 2.1 闭环验证原则 (Closed-loop Verification)
**通用逻辑**：禁止在有验证手段时提供仅基于推测的建议。
- **实现方式**：在 ReAct System Prompt 中注入"修复即验证"协议。如果 LLM 计划提出建议 A，它必须检索工具列表，若存在能验证 A 是否生效或存在的工具 B，则**必须执行 B** 后才能生成报告。
- **效果**：不仅针对 `connection refused`，也适用于如"Pod 挂载卷满"、"内核参数不匹配"等所有场景。

### 2.2 多维环境感知 (Multi-dimensional Awareness)
**通用逻辑**：主动探测并适配不同的基础设施层级。
- **实现方式**：
  - **工具描述增强**：在 `execute_safe_command` 中定义"探测优先级"。
  - **分层探测模型**：引导 LLM 按照"K8s MCP 工具 -> 容器运行时 (Docker/Crictl) -> 系统工具 (ip, ps, df, lsof) -> 系统服务 (systemd)"的层级进行降级探测。
- **效果**：无论集群组件是容器化运行还是二进制运行，LLM 都能通过探测找到正确的排查入口。

### 2.3 容错与路径切换 (Resilient Path Switching)
**通用逻辑**：将"工具失败"视为新的信息来源，而非终点。
- **实现方式**：
  - **错误反馈循环**：在 Prompt 中明确定义 `command not found` 或 `permission denied` 的处理逻辑——要求 LLM 分析错误原因并立即切换到备选工具链。
  - **关联工具索引**：在 Prompt 提示 LLM：如果 MCP 工具 `get_pod_logs` 失败，应考虑 `docker logs`；如果 `nc` 失败，应考虑 `telnet` 或 `/dev/tcp`。

### 2.4 动作导向语义 (Action-oriented Semantics)
**通用逻辑**：将工具描述从"功能说明"提升为"场景契约"。
- **实现方式**：更新工具元数据，明确标注工具在"验证阶段"和"修复阶段"的不同权重。

## 3. 具体修改内容

### 3.1 增强工具适配器语义 (internal/agent/analysis/tools.go)
修改 `SafetyAgentToolAdapter.Info`：
- **新描述**：`【通用底层探测与修复工具】当 K8s 标准 API 无法提供足够信息或返回错误时，必须使用此工具。
  通用策略：
  1. 探测基础设施：使用 which/ls/ps 确认环境工具链。
  2. 跨层验证：若 API 层显示异常，须在宿主机层（进程/容器/网络）获取佐证。
  3. 执行闭环：支持安全范围内的状态修复与二次验证。`

- **实现方式**：直接修改 `tools.go` 第 130-135 行中的 `Info` 方法返回值，更新 `Desc` 字段。

### 3.2 重构 ReAct 系统提示词 (internal/agent/analysis/react_llm.go)
更新 `getReActSystemPrompt`，注入"通用诊断协议"：
- **强制思维链**：要求 `thought` 必须包含对"可验证性"的评估。
- **降级路径说明**：明确提供"MCP 工具调用故障 -> 运行时探测 -> 系统工具探测"的通用降级指引。

- **实现方式**：修改 `react_llm.go` 第 456-510 行的 `getReActSystemPrompt` 函数，在 JSON Schema 注释后添加新的诊断协议说明。

### 3.3 优化决策提示词 (internal/agent/analysis/react_llm.go)
修改 `buildDecisionPrompt`：
- **动态任务指令**：根据 `State.LastError` 的存在与否，动态调整任务重心。如果存在错误，指令将自动偏向"底层探测"而非"资源列举"。

- **实现方式**：修改 `react_llm.go` 第 597-684 行的 `buildDecisionPrompt` 函数，在"任务"部分添加基于 `LastError` 的条件判断逻辑。

### 3.4 增强报告节点 - 报告标注自动化 (internal/agent/analysis/react_llm.go)
修改 `buildSynthesizePrompt`：
- **验证事实标注**：通过增强提示词，引导 LLM 根据 `CommandExecution` 的成功状态自动识别并标注 `Verified Findings`。
- **实现方式**：修改 `react_llm.go` 第 350-411 行的 `buildSynthesizePrompt` 函数，在 "Output Format" 部分添加验证状态标注指引：
  - 在"Findings"部分，要求 LLM 根据执行历史中的 `CommandExecution.Success` 字段判断发现是否为"已验证事实"(Verified Finding)还是"推测性发现"(Inferred Finding)。
  - 对于成功执行的命令对应的发现，标注为 `✅ Verified`；对于仅基于 K8s MCP 工具数据的推测，标注为 `⚠️ Inferred`。

### 3.5 错误状态同步 (internal/agent/analysis/nodes.go)
修改 `ActionNode.Execute` 逻辑：
- **问题描述**：当前 `execute_safe_command` 执行失败时，未同步更新 `state.LastError`，导致决策节点无法感知底层命令执行错误。
- **实现方式**：修改 `nodes.go` 第 542-617 行的 `executeToolCall` 函数，在 `case "execute_safe_command":` 分支的错误处理中（第 570-575 行），添加 `state.LastError = err` 语句，与 K8s 工具调用保持一致。

## 4. 降级探测指引详解

> 本节详细解释"MCP 工具调用故障 -> 运行时探测 -> 系统工具探测"降级指引的含义，这是一种**分层降级排查策略**。

### 4.1 策略含义与目的

当 LLM 进行 Kubernetes 故障诊断时，通常首先依赖 K8s MCP 工具（如 `list_pods`, `describe_pod`）获取资源状态。然而，这种高层抽象可能存在以下局限性：

- **MCP 服务不可用或返回错误**：MCP 服务器连接失败或工具调用返回错误
- **状态显示为 Unknown**：Kubelet 无法向 API Server 报告准确的节点或 Pod 状态
- **权限问题**：RBAC 限制导致 MCP 服务无法获取某些资源信息
- **信息不足以定位根因**：K8s 层仅展示"是什么"，而非"为什么"

**分层降级策略**的核心思想是：当高层抽象失效或信息不足时，引导 LLM 逐步深入底层获取真实状态，确保诊断的深度和准确性。这是一种**自顶向下的排查方法**，每降一级都意味着更接近问题的本质。

### 4.2 层级定义

#### 4.2.1 MCP 服务/工具调用故障（第一层：K8s MCP 服务层）

**定义**：通过 `k8s-mcp` 服务提供的工具（如 `list_pods`, `list_namespaces`, `describe_pod`, `get_pod_logs`）无法获取数据，或 MCP 服务本身返回错误。注意：本项目**不直接调用 Kubernetes 原生 API 或 kubectl**，而是通过 MCP 服务的 `k8sClient.CallTool` 接口获取集群数据。

**典型场景**：
- `list_namespaces` 工具不可用或返回错误
- MCP 服务器连接失败（如网络超时、服务未启动）
- `CallTool` 返回 `Unknown tool` 错误（工具不存在）
- `list_pods` 返回空结果或解析失败
- 权限不足导致 MCP 服务无法访问某些资源

**排查入口**：
- 检查 MCP 服务器 (`k8s-mcp`) 是否正常运行
- 使用 `ListTools` 查看可用的 MCP 工具列表
- 验证 MCP 服务配置是否正确（见 `bin/k8s_config.json`）
- 考虑降级到运行时探测获取底层信息

#### 4.2.2 运行时探测（第二层：容器运行时）

**定义**：跳过 Kubernetes 控制平面，直接在节点上使用容器运行时工具查看容器实体的存活状态与真实日志。

**典型工具**：
- `docker ps -a`、`docker logs <container_id>`
- `crictl ps -a`、`crictl logs <container_id>`
- `crictl inspect <container_id>`（获取容器详细信息）

**典型场景**：
- Pod 状态显示为 `Running` 但应用实际已崩溃
- 容器反复重启，需要查看真实的重启原因
- K8s MCP 工具显示的日志不完整或被截断
- 需要查看容器内部的文件系统状态

**排查入口**：
- 检查容器实际运行状态（而非 K8s 报告的状态）
- 查看容器启动日志和标准错误输出
- 检查容器进程树和资源使用

#### 4.2.3 系统工具探测（第三层：宿主机层面）

**定义**：通过操作系统层面的原生工具分析进程、网络连接、磁盘、内核参数等宿主机指标。

**典型工具**：
- `ps aux`：查看进程状态
- `netstat -tulpn` / `ss -tulpn`：查看网络连接和端口监听
- `ip addr` / `ip link`：查看网络接口配置
- `df -h`：查看磁盘使用情况
- `lsof -i :<port>`：查看端口占用
- `top` / `htop`：查看实时资源使用
- `dmesg`：查看内核日志
- `sysctl -a`：查看内核参数

**典型场景**：
- 容器无法启动，需要检查宿主机资源是否耗尽
- 网络连接失败，需要验证宿主机网络配置
- 进程消失但无日志，需要检查 OOM Kill 或系统限制
- 磁盘满导致写入失败

**排查入口**：
- 验证宿主机硬件资源是否充足
- 检查系统服务状态（如 kubelet、containerd）
- 分析网络连通性和防火墙规则

### 4.3 降级路径示例

```
┌─────────────────────────────────────────────────────────────────┐
│  步骤 1: 尝试 K8s MCP 工具调用 (list_pods, describe_pod)       │
│  ↓ (若失败或信息不足)                                            │
│  步骤 2: 降级到运行时探测 (docker/crictl logs)                    │
│  ↓ (若仍无法解决)                                                │
│  步骤 3: 降级到系统工具 (ps, netstat, df, dmesg)                 │
└─────────────────────────────────────────────────────────────────┘
```

### 4.4 在 Prompt 中的应用

在 ReAct System Prompt 中，LLM 应被明确告知：
- 当 K8s MCP 工具调用失败时（如 `list_pods` 返回错误），**必须**考虑使用 `docker` 或 `crictl` 进行验证
- 当容器运行时也无法提供信息时，**必须**降级到宿主机系统工具
- 每一次降级都应在 `thought` 中记录原因和预期收获

## 5. 预期效果
- **通用性**：该方案不依赖于具体的错误字符串，而是通过"可验证性"这一通用逻辑驱动 LLM。
- **深度**：巡检报告将不仅包含 K8s 对象状态，还将包含来自容器运行时和宿主机的深层验证数据。
- **健壮性**：系统能自动适配不同 Linux 发行版和 Kubernetes 部署工具（Kubeadm/二进制等）产生的环境差异。
- **可追溯性**：通过验证状态标注，用户可以清晰区分"已通过工具确认的事实"与"推测性发现"。

## 6. 实施顺序
1. 先修改 `tools.go` 更新工具描述（步骤 3.1）
2. 修改 `react_llm.go` 更新提示词（步骤 3.2、3.3、3.4）
3. 修改 `nodes.go` 修复错误状态同步（步骤 3.5）
4. 可选：修改 `state.go` 添加 `IsVerified` 字段（步骤 3.6 方案 B）
