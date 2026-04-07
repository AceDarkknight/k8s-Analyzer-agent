// Package main 提供 k8s-analyzer 的 CLI 入口程序
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/AceDarkknight/k8s-analyzer-agent/internal/agent/diagnosis"
	"github.com/AceDarkknight/k8s-analyzer-agent/internal/agent/safety"
	"github.com/AceDarkknight/k8s-analyzer-agent/internal/client/gateway"
	"github.com/AceDarkknight/k8s-analyzer-agent/internal/client/shellmcp"
	"github.com/AceDarkknight/k8s-analyzer-agent/internal/config"
	"github.com/AceDarkknight/k8s-analyzer-agent/internal/llm"
	"github.com/AceDarkknight/k8s-analyzer-agent/internal/logger"
	"github.com/AceDarkknight/k8s-analyzer-agent/internal/state"
	"github.com/AceDarkknight/k8s-analyzer-agent/internal/store"
)

func main() {
	// 1. 解析命令行参数
	configPath := flag.String("config", "configs/config.yaml", "配置文件路径")
	safetyRulesPath := flag.String("safety-rules", "configs/safety_rules.yaml", "安全规则配置文件路径")
	flag.Parse()

	// 用户查询从剩余参数获取
	args := flag.Args()
	if len(args) == 0 {
		fmt.Println("用法: k8s-analyzer [--config path] [--safety-rules path] <查询内容>")
		fmt.Println("示例: k8s-analyzer \"分析 default 命名空间下 Pod 异常的原因\"")
		os.Exit(1)
	}
	userQuery := strings.Join(args, " ")

	// 2. 加载配置
	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "加载配置失败: %v\n", err)
		os.Exit(1)
	}

	// 3. 初始化 Logger
	logCfg := &logger.LogConfig{
		Level:      cfg.Log.Level,
		FilePath:   cfg.Log.FilePath,
		MaxSizeMB:  cfg.Log.MaxSizeMB,
		MaxBackups: cfg.Log.MaxBackups,
	}
	if err := logger.Init(logCfg); err != nil {
		fmt.Fprintf(os.Stderr, "初始化日志失败: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync()

	logger.Info("K8s Analyzer Agent 启动",
		logger.String("config", *configPath),
		logger.String("safety_rules", *safetyRulesPath),
	)

	// 4. Context with cancel (支持 Ctrl+C)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		logger.Info("接收到退出信号，正在关闭...")
		cancel()
	}()

	// 5. 初始化 Gateway Client（Fatal if 失败）
	gwClient, err := gateway.NewGatewayClient(cfg.Gateway.BaseURL, cfg.Gateway.AuthToken, cfg.Gateway.TimeoutSeconds)
	if err != nil {
		logger.Fatal("Gateway Client 初始化失败", logger.Err(err))
	}
	logger.Info("Gateway Client 初始化成功")

	// 6. 初始化 Shell MCP Client（Warn if 失败，降级模式）
	var mcpClient *shellmcp.ShellMCPClient
	mcpClient = shellmcp.NewShellMCPClient(cfg.ShellMCP.ServerURL, cfg.ShellMCP.AuthToken)

	// 使用带超时的连接
	connectDone := make(chan error, 1)
	go func() {
		connectDone <- mcpClient.Connect(ctx)
	}()

	select {
	case err := <-connectDone:
		if err != nil {
			logger.Warn("Shell MCP Client 连接失败，将以降级模式运行", logger.Err(err))
			mcpClient = nil // 降级：设为 nil
		} else {
			logger.Info("Shell MCP Client 连接成功")
			defer mcpClient.Close()
		}
	case <-time.After(10 * time.Second):
		logger.Warn("Shell MCP Client 连接超时，将以降级模式运行")
		mcpClient = nil // 降级：设为 nil
	}

	// 7. 初始化 LLM Router
	llmRouter, err := llm.NewLLMRouter(ctx, &cfg.LLM)
	if err != nil {
		logger.Fatal("LLM Router 初始化失败", logger.Err(err))
	}
	logger.Info("LLM Router 初始化成功")

	// 8. 初始化 Safety Agent
	ruleEngine, err := safety.NewRuleEngine(*safetyRulesPath)
	if err != nil {
		logger.Fatal("Rule Engine 初始化失败", logger.Err(err))
	}
	logger.Info("Rule Engine 初始化成功")

	var auditor safety.Auditor
	auditor = safety.NewLLMAuditor(llmRouter.Light())

	safetyAgent := safety.NewSafetyAgent(ruleEngine, auditor, mcpClient)
	logger.Info("Safety Agent 初始化成功")

	// 9. 初始化 ReAct LLM
	// 创建适配器将 SafetyAgent 适配为 SafeCommandExecutor 接口
	safeExecutor := &safetyAgentAdapter{safetyAgent: safetyAgent}
	reactLLM := llm.NewReActLLM(llmRouter, gwClient, safeExecutor)
	logger.Info("ReAct LLM 初始化成功")

	// 10. 初始化 FindingStore
	var findingStore store.FindingStore
	if cfg.Store.Type == "redis" {
		findingStore, err = store.NewRedisStore(cfg.Store.Redis.Host, cfg.Store.Redis.Port, cfg.Store.Redis.Password, cfg.Store.Redis.DB)
		if err != nil {
			logger.Warn("Redis Store 初始化失败，使用内存 Store", logger.Err(err))
			findingStore = store.NewMemoryStore()
		} else {
			logger.Info("Redis Store 初始化成功")
		}
	} else {
		findingStore = store.NewMemoryStore()
		logger.Info("内存 Store 初始化成功")
	}
	defer findingStore.Close()

	// 10.5 初始化 ToolCacheStore
	var toolCache store.ToolCacheStore
	cacheTTL, _ := time.ParseDuration(cfg.Agent.ToolCache.TTL)
	if cacheTTL <= 0 {
		cacheTTL = 10 * time.Minute
	}
	switch cfg.Agent.ToolCache.Backend {
	case "redis":
		if cfg.Store.Type == "redis" {
			// 复用 FindingStore 的 Redis 连接配置
			toolCache = store.NewRedisToolCache(nil) // 需要单独创建 redis client
			logger.Info("ToolCache: Redis 后端（降级为内存）")
			toolCache = store.NewMemoryToolCache(cacheTTL)
		} else {
			logger.Warn("ToolCache: Redis 后端配置但 Store 不是 Redis，降级为内存")
			toolCache = store.NewMemoryToolCache(cacheTTL)
		}
	case "file":
		fileCache, fcErr := store.NewFileToolCache(cfg.Agent.ToolCache.FileDir)
		if fcErr != nil {
			logger.Warn("ToolCache: 文件后端初始化失败，降级为内存", logger.Err(fcErr))
			toolCache = store.NewMemoryToolCache(cacheTTL)
		} else {
			toolCache = fileCache
			logger.Info("ToolCache: 文件后端初始化成功", logger.String("dir", cfg.Agent.ToolCache.FileDir))
		}
	default:
		toolCache = store.NewMemoryToolCache(cacheTTL)
		logger.Info("ToolCache: 内存后端初始化成功")
	}
	defer toolCache.Close()

	// 11. 初始化 Main Agent
	agent := diagnosis.NewAgent(gwClient, safetyAgent, llmRouter, reactLLM, findingStore, toolCache, &cfg.Agent)
	logger.Info("Main Agent 初始化成功")

	// 12. 执行诊断
	fmt.Printf("开始诊断: %s\n\n", userQuery)
	result, err := agent.Run(ctx, userQuery)
	if err != nil {
		fmt.Fprintf(os.Stderr, "诊断失败: %v\n", err)
		os.Exit(1)
	}

	// 13. 输出报告
	if result != nil {
		printReport(result)
	} else {
		fmt.Println("未能生成诊断报告")
	}
}

// safetyAgentAdapter 将 SafetyAgent 适配为 llm.SafeCommandExecutor 接口
type safetyAgentAdapter struct {
	safetyAgent *safety.SafetyAgent
}

// ExecuteSafeCommand 实现 llm.SafeCommandExecutor 接口
func (a *safetyAgentAdapter) ExecuteSafeCommand(ctx context.Context, command, reason string) (string, error) {
	return a.safetyAgent.ExecuteSimple(ctx, command, reason)
}

func printReport(result *state.AnalysisResult) {
	fmt.Println(strings.Repeat("=", 60))
	fmt.Printf("诊断报告 [%s]\n", result.Status)
	fmt.Println(strings.Repeat("=", 60))
	fmt.Printf("\n概要: %s\n", result.Summary)
	fmt.Printf("严重程度: %s\n", result.Severity)
	if result.RootCause != "" {
		fmt.Printf("根因: %s\n", result.RootCause)
	}
	// 打印 Findings
	if len(result.Findings) > 0 {
		fmt.Printf("\n发现 (%d 项):\n", len(result.Findings))
		for i, f := range result.Findings {
			fmt.Printf("  %d. [%s] %s: %s\n", i+1, f.Severity, f.Resource, f.Message)
			if f.Evidence != "" {
				fmt.Printf("     证据: %s\n", f.Evidence)
			}
		}
	}
	// 打印 Recommendations
	if len(result.Recommendations) > 0 {
		fmt.Printf("\n建议 (%d 项):\n", len(result.Recommendations))
		for i, r := range result.Recommendations {
			// 确定图标和标签
			var icon, label string
			switch {
			case r.Verified:
				icon, label = "✅", "已验证"
			case r.Command != "":
				icon, label = "⚠️ ", "需人工操作"
			default:
				icon, label = "💡", "建议优化"
			}

			fmt.Printf("  %d. [%s] %s %s - %s\n", i+1, r.Priority, icon, label, r.Action)

			if r.Verified && r.VerifyResult != "" {
				fmt.Printf("     验证结果: %s\n", r.VerifyResult)
			}
			if r.Command != "" {
				if r.Verified {
					fmt.Printf("     命令: %s (已执行)\n", r.Command)
				} else {
					fmt.Printf("     命令: %s\n", r.Command)
				}
			}
			if r.Risk != "" && !r.Verified {
				fmt.Printf("     风险: %s\n", r.Risk)
			}
		}
	}
	if result.Limitations != "" {
		fmt.Printf("\n限制: %s\n", result.Limitations)
	}
	fmt.Println(strings.Repeat("=", 60))
}
