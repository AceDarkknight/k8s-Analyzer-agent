# 监控页面最小运行说明

本文档说明如何在本地启动以下三个部分：

1. `k8s-analyzer`：执行一次诊断任务并写入 Trace
2. `k8s-monitor`：读取 Trace，提供 `/api/v1/*`、`/metrics` 以及内嵌前端页面
3. （可选）Vite 开发服务器：前端开发调试时使用

## 1. 准备环境变量

PowerShell 示例：

```powershell
$env:SHELL_MCP_URL=""
$env:SHELL_MCP_TOKEN="***"
$env:GATEWAY_BASE_URL=""
$env:GATEWAY_AUTH_TOKEN="***"
$env:OPENAI_API_KEY="***"
```

如果配置文件中已经引用这些环境变量，则无需额外修改 `configs/config.yaml`。

## 2. 构建前端资源（供 k8s-monitor 内嵌）

```powershell
cd web
npm install
npm run build
cd ..
```

构建完成后会生成 `web/dist/`，`k8s-monitor` 会自动内嵌该目录。

## 3. 启动监控服务

```powershell
go run ./cmd/k8s-monitor --config configs/config.yaml
```

默认监听：

- `http://localhost:9090/`：监控前端页面
- `http://localhost:9090/api/v1/tasks`：任务列表 API
- `http://localhost:9090/api/v1/stats`：Dashboard 聚合统计 API
- `http://localhost:9090/metrics`：Prometheus 文本指标

如果端口冲突，可以显式指定：

```powershell
go run ./cmd/k8s-monitor --config configs/config.yaml --port 19090
```

## 4. 运行一次诊断任务，生成 Trace

```powershell
go run ./cmd/k8s-analyzer --config configs/config.yaml "检查 default 命名空间中异常 Pod 的原因"
```

运行完成后会在 `monitor.trace_dir`（默认 `data/traces/`）下生成：

- `traces_index.jsonl`
- `<task_id>.json`

`k8s-analyzer` 会在 Trace 写入完成后才退出。

## 5. 查看页面

打开浏览器访问：

```text
http://localhost:9090/
```

页面包含：

- Dashboard（调用 `/api/v1/stats`）
- 任务列表（调用 `/api/v1/tasks`）
- 任务详情（调用 `/api/v1/tasks/{id}`）

## 6. 前端开发模式（可选）

如果需要单独调试前端热更新：

```powershell
cd web
npm run dev
```

Vite 会把 `/api` 和 `/metrics` 代理到 `http://localhost:9090`，因此仍然需要先启动 `k8s-monitor`。

## 7. 快速排查

### 页面空白 / 无数据

先确认：

1. `k8s-monitor` 已启动
2. 已至少运行过一次 `k8s-analyzer`
3. `data/traces/` 下确实有 JSON 文件和索引文件

### `/api/v1/tasks` 返回空数组

说明 monitor 已启动，但尚未检测到 Trace 文件。请先执行一次 analyzer。

### 前端页面能打开，但接口失败

确认 `k8s-monitor` 日志，以及端口是否与 Vite 代理目标一致。
