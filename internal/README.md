# Internal

本目录包含项目的私有应用代码库，这些代码仅供本项目内部使用，不作为公共 API 导出。

## 目录结构

*   **`agent/`**: 包含 Agent 的核心逻辑。
    *   **`analysis/`**: 主 Agent 的分析逻辑，负责任务编排和报告生成。
    *   **`safety/`**: 安全子 Agent，负责对 Shell 命令进行安全审计。
*   **`client/`**: 包含与外部 MCP Server 交互的客户端实现。
    *   **`k8s/`**: Kubernetes MCP Client。
    *   **`shell/`**: Shell Executor MCP Client。
