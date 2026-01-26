# CMD

本目录包含项目的入口点。

## 子目录说明

*   `k8s-analyzer/`: 包含 `main.go`，是 K8s Analyzer Agent 的主程序入口。

## 构建方法

请在项目根目录下执行：

```bash
go build -o bin/k8s-analyzer.exe cmd/k8s-analyzer/main.go
```
