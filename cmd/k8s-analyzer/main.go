// Package main 提供 k8s-analyzer 的 CLI 入口程序
// 串联所有模块进行集成测试与验收
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/your-org/k8s-analyzer-agent/internal/agent/analysis"
	"github.com/your-org/k8s-analyzer-agent/internal/agent/safety"
	"github.com/your-org/k8s-analyzer-agent/internal/client/k8s"
	"github.com/your-org/k8s-analyzer-agent/internal/client/shell"
)

const (
	// 默认配置文件路径
	defaultK8sConfigPath   = "bin/k8s_config.json"
	defaultShellConfigPath = "bin/shell_config.json"
)

func main() {
	logger := log.New(os.Stdout, "[K8s-Analyzer] ", log.LstdFlags|log.Lshortfile)
	logger.Println("=== K8s Analyzer Agent 集成测试与验收 ===")

	ctx := context.Background()

	// 1. 读取配置文件
	logger.Println("步骤 1: 读取配置文件...")
	k8sConfigPath, err := getConfigPath(defaultK8sConfigPath)
	if err != nil {
		logger.Printf("警告: 无法获取 K8s 配置路径: %v，使用默认配置", err)
	}
	shellConfigPath, err := getConfigPath(defaultShellConfigPath)
	if err != nil {
		logger.Printf("警告: 无法获取 Shell 配置路径: %v，使用默认配置", err)
	}

	// 2. 初始化 K8s Client
	logger.Println("步骤 2: 初始化 K8s Client...")
	k8sClient, err := k8s.NewClientFromFile(k8sConfigPath)
	if err != nil {
		logger.Printf("K8s Client 初始化失败: %v", err)
	} else {
		logger.Println("K8s Client 初始化成功")
	}

	// 3. 初始化 Shell Client
	logger.Println("步骤 3: 初始化 Shell Client...")
	shellClient, err := shell.NewClientFromFile(shellConfigPath)
	if err != nil {
		logger.Printf("Shell Client 初始化失败: %v", err)
	} else {
		logger.Println("Shell Client 初始化成功")
	}

	// 如果 Shell Client 初始化失败，无法继续
	if shellClient == nil {
		logger.Println("错误: Shell Client 未初始化，无法继续")
		os.Exit(1)
	}

	// 4. 初始化 Safety Agent
	logger.Println("步骤 4: 初始化 Safety Agent...")
	safetyAgent, err := safety.NewAgent(shellClient, shellConfigPath)
	if err != nil {
		logger.Printf("Safety Agent 初始化失败: %v", err)
		os.Exit(1)
	}
	logger.Println("Safety Agent 初始化成功")

	// 5. 初始化 Analysis Agent
	logger.Println("步骤 5: 初始化 Analysis Agent...")
	analysisAgent, err := analysis.NewAgent(k8sClient, safetyAgent, logger)
	if err != nil {
		logger.Printf("Analysis Agent 初始化失败: %v", err)
		os.Exit(1)
	}
	logger.Println("Analysis Agent 初始化成功")

	// 6. 尝试连接 K8s Client（预期会失败，因为没有真实的 MCP Server）
	logger.Println("步骤 6: 尝试连接 K8s Client...")
	if k8sClient != nil {
		if err := k8sClient.Connect(ctx); err != nil {
			logger.Printf("K8s Client 连接失败 (预期行为): %v", err)
		} else {
			logger.Println("K8s Client 连接成功")
		}
		defer func() {
			if err := k8sClient.Close(); err != nil {
				logger.Printf("K8s Client 关闭失败: %v", err)
			}
		}()
	}

	// 7. 尝试连接 Shell Client（预期会失败，因为没有真实的 MCP Server）
	logger.Println("步骤 7: 尝试连接 Shell Client...")
	if err := shellClient.Connect(ctx); err != nil {
		logger.Printf("Shell Client 连接失败 (预期行为): %v", err)
		logger.Println("注意: 这是因为没有真实的 MCP Server 运行")
	} else {
		logger.Println("Shell Client 连接成功")
	}
	defer func() {
		if err := shellClient.Close(); err != nil {
			logger.Printf("Shell Client 关闭失败: %v", err)
		}
	}()

	// 8. 启动 Agent 并传入示例参数
	logger.Println("步骤 8: 启动 Analysis Agent 进行分析...")
	userInput := "分析 default 命名空间中的 Pod 状态"
	logger.Printf("用户输入: %s", userInput)

	result, err := analysisAgent.Run(ctx, userInput)
	if err != nil {
		logger.Printf("Analysis Agent 执行失败: %v", err)
		// 不退出，继续打印部分结果
	}

	// 9. 打印最终报告
	logger.Println("步骤 9: 打印分析报告...")
	printReport(result, logger)

	logger.Println("=== 集成测试与验收完成 ===")
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

// printReport 打印分析报告
func printReport(result *analysis.AnalysisResult, logger *log.Logger) {
	if result == nil {
		logger.Println("无分析结果")
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
