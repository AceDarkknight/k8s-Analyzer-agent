// Package main 提供 k8s-analyzer 的 CLI 入口程序
// 串联所有模块进行集成测试与验收
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/AceDarkknight/k8s-analyzer-agent/internal/agent/analysis"
	"github.com/AceDarkknight/k8s-analyzer-agent/internal/agent/safety"
	"github.com/AceDarkknight/k8s-analyzer-agent/internal/client/k8s"
	"github.com/AceDarkknight/k8s-analyzer-agent/internal/client/shell"
	"github.com/AceDarkknight/k8s-analyzer-agent/internal/config"
	"github.com/AceDarkknight/k8s-analyzer-agent/internal/logger"
)

const (
	// 默认配置文件路径
	defaultK8sConfigPath   = "bin/k8s_config.json"
	defaultShellConfigPath = "bin/shell_config.json"
	defaultLLMConfigPath   = "bin/llm_config.json"
)

func main() {
	// 初始化日志系统
	if err := logger.InitWithConfig(logger.NewDefaultConfig()); err != nil {
		fmt.Printf("日志初始化失败: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync()

	logger.Info("=== K8s Analyzer Agent 集成测试与验收 ===")

	ctx := context.Background()

	// 设置优雅退出
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		logger.Info("接收到退出信号，正在关闭...")
		logger.Sync()
		os.Exit(0)
	}()

	// 1. 读取配置文件
	logger.Info("步骤 1: 读取配置文件...")
	k8sConfigPath, err := getConfigPath(defaultK8sConfigPath)
	if err != nil {
		logger.Warn("无法获取 K8s 配置路径，使用默认配置", logger.Err(err))
	}
	shellConfigPath, err := getConfigPath(defaultShellConfigPath)
	if err != nil {
		logger.Warn("无法获取 Shell 配置路径，使用默认配置", logger.Err(err))
	}

	// 读取 LLM 配置
	llmConfig, err := loadLLMConfig(defaultLLMConfigPath)
	if err != nil {
		logger.Warn("无法读取 LLM 配置，使用默认配置", logger.Err(err))
		llmConfig = config.DefaultAgentLLMConfig()
	}
	logger.Info("LLM 配置加载成功",
		logger.String("analysis_model", llmConfig.Analysis.Model),
		logger.String("safety_model", llmConfig.Safety.Model))

	// 2. 初始化 K8s Client
	logger.Info("步骤 2: 初始化 K8s Client...")
	k8sClient, err := k8s.NewClientFromFile(k8sConfigPath)
	if err != nil {
		logger.Error("K8s Client 初始化失败", logger.Err(err))
	} else {
		logger.Info("K8s Client 初始化成功")
	}

	// 3. 初始化 Shell Client
	logger.Info("步骤 3: 初始化 Shell Client...")
	shellClient, err := shell.NewClientFromFile(shellConfigPath)
	if err != nil {
		logger.Fatal("创建 Shell Client 失败", logger.Err(err))
	} else {
		logger.Info("创建 Shell Client 成功")
	}

	// 4. 初始化 Safety Agent
	logger.Info("步骤 4: 初始化 Safety Agent...")
	// 创建 LLM 审计器（使用基于规则的审计器，传入 Safety 配置）
	llmAuditor := safety.NewRuleBasedAuditor(&llmConfig.Safety)
	safetyAgent, err := safety.NewAgent(shellClient, shellConfigPath, llmAuditor)
	if err != nil {
		logger.Fatal("Safety Agent 初始化失败", logger.Err(err))
	}
	logger.Info("Safety Agent 初始化成功")

	// 5. 初始化 Analysis Agent
	logger.Info("步骤 5: 初始化 Analysis Agent...")
	analysisAgent, err := analysis.NewAgent(k8sClient, safetyAgent, &llmConfig.Analysis)
	if err != nil {
		logger.Fatal("Analysis Agent 初始化失败", logger.Err(err))
	}
	logger.Info("Analysis Agent 初始化成功")

	// 6. 尝试连接 K8s Client（预期会失败，因为没有真实的 MCP Server）
	logger.Info("步骤 6: 尝试连接 K8s Client...")
	if k8sClient != nil {
		if err := k8sClient.Connect(ctx); err != nil {
			logger.Fatal("K8s Client 连接失败", logger.Err(err))
		}

		logger.Info("K8s Client 连接成功")
		defer func() {
			if err := k8sClient.Close(); err != nil {
				logger.Error("K8s Client 关闭失败", logger.Err(err))
			}
		}()
	}

	// 7. 跳过 Shell Client 连接（预期会失败，因为没有真实的 MCP Server）
	logger.Info("步骤 7: 尝试连接 Shell Client...")

	// 创建带超时的 context (10秒超时)
	connectCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	if err := shellClient.Connect(connectCtx); err != nil {
		logger.Fatal("Shell Client 连接失败", logger.Err(err))
	} else {
		logger.Info("Shell Client 连接成功")
		defer func() {
			// 清理已连接的资源
			if err := shellClient.Close(); err != nil {
				logger.Error("Shell Client 关闭失败", logger.Err(err))
			}
		}()
	}

	// 8. 启动 Agent 并传入示例参数
	logger.Info("步骤 8: 启动 Analysis Agent 进行分析...")
	userInput := "分析所有命名空间中的 Pod 状态"
	logger.Info("用户输入", logger.String("input", userInput))

	result, err := analysisAgent.Run(ctx, userInput)
	if err != nil {
		logger.Error("Analysis Agent 执行失败", logger.Err(err))
		// 不退出，继续打印部分结果
	}

	// 9. 打印最终报告
	logger.Info("步骤 9: 打印分析报告...")
	printReport(result)

	logger.Info("=== 集成测试与验收完成 ===")
}

// getConfigPath 获取配置文件的绝对路径
func getConfigPath(defaultPath string) (string, error) {
	// 尝试从当前工作目录获取相对路径
	absPath, err := filepath.Abs(defaultPath)
	if err != nil {
		return "", err
	}

	// 检查文件是否存在
	if _, err := os.Stat(absPath); err != nil {
		return "", err
	}

	return absPath, nil
}

// loadLLMConfig 加载 LLM 配置文件
// 配置优先级：环境变量 > 配置文件 > 默认值
func loadLLMConfig(configPath string) (*config.AgentLLMConfig, error) {
	// 尝试获取绝对路径
	absPath, err := filepath.Abs(configPath)
	if err != nil {
		return nil, fmt.Errorf("无法获取配置文件绝对路径: %w", err)
	}

	// 读取配置文件
	configData, err := os.ReadFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("无法读取配置文件: %w", err)
	}

	// 解析配置
	var llmConfig config.AgentLLMConfig
	if err := json.Unmarshal(configData, &llmConfig); err != nil {
		return nil, fmt.Errorf("无法解析配置文件: %w", err)
	}

	// 环境变量覆盖配置文件中的值
	// 优先级：环境变量 > 配置文件

	// Analysis Agent 配置覆盖
	if apiKey := os.Getenv("ANALYSIS_AGENT_API_KEY"); apiKey != "" {
		llmConfig.Analysis.APIKey = apiKey
	}
	if baseURL := os.Getenv("ANALYSIS_AGENT_BASE_URL"); baseURL != "" {
		llmConfig.Analysis.BaseURL = baseURL
	}

	// Safety Agent 配置覆盖
	if apiKey := os.Getenv("SAFETY_AGENT_API_KEY"); apiKey != "" {
		llmConfig.Safety.APIKey = apiKey
	}
	if baseURL := os.Getenv("SAFETY_AGENT_BASE_URL"); baseURL != "" {
		llmConfig.Safety.BaseURL = baseURL
	}

	return &llmConfig, nil
}

// printReport 打印分析报告
func printReport(result *analysis.AnalysisResult) {
	if result == nil {
		fmt.Println("无分析结果")
		return
	}

	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Println("分析报告")
	fmt.Println(strings.Repeat("=", 60))

	// 打印摘要
	if result.Summary != "" {
		fmt.Println("\n" + result.Summary)
	}

	// 打印发现的问题
	if len(result.Findings) > 0 {
		fmt.Println("\n发现的问题:")
		for i, finding := range result.Findings {
			fmt.Printf("  %d. [%s] %s: %s\n", i+1, finding.Severity, finding.Resource, finding.Message)
		}
	} else {
		fmt.Println("\n未发现问题")
	}

	// 打印建议
	if len(result.Recommendations) > 0 {
		fmt.Println("\n建议:")
		for i, rec := range result.Recommendations {
			fmt.Printf("  %d. %s (优先级: %s)\n", i+1, rec.Action, rec.Priority)
			fmt.Printf("     原因: %s\n", rec.Reason)
			if rec.Command != "" {
				fmt.Printf("     命令: %s\n", rec.Command)
			}
		}
	}

	// 打印执行的命令
	if len(result.ExecutedCommands) > 0 {
		fmt.Println("\n执行的命令:")
		for i, cmd := range result.ExecutedCommands {
			fmt.Printf("  %d. %s\n", i+1, cmd.Command)
			fmt.Printf("     状态: %s\n", getStatusText(cmd.Success))
			if len(cmd.Output) > 0 {
				output := cmd.Output
				if len(output) > 200 {
					output = output[:200] + "..."
				}
				fmt.Printf("     输出: %s\n", output)
			}
		}
	}

	// 打印状态
	fmt.Printf("\n分析状态: %s\n", result.Status)

	fmt.Println(strings.Repeat("=", 60))
}

// getStatusText 获取状态文本
func getStatusText(success bool) string {
	if success {
		return "成功"
	}
	return "失败"
}
