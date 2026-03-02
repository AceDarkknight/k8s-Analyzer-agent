// Shell MCP Server 连接测试程序
// 直接使用 github.com/AceDarkknight/shell-executor-mcp/pkg/mcpclient 进行测试
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/AceDarkknight/k8s-analyzer-agent/internal/logger"
	"github.com/AceDarkknight/shell-executor-mcp/pkg/configs"
	"github.com/AceDarkknight/shell-executor-mcp/pkg/mcpclient"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// 配置路径
const (
	configPath = "bin/shell_config.json"
)

// ServerConfig 从配置文件读取的服务器配置
type ServerConfig struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// SimpleConfig 简化的配置结构
type SimpleConfig struct {
	Servers []ServerConfig `json:"servers"`
	Token   string         `json:"token"`
}

func main() {
	fmt.Println("========================================")
	fmt.Println("Shell MCP Client 测试程序")
	fmt.Println("直接使用 mcpclient 包")
	fmt.Println("========================================")
	fmt.Println()

	// 显示环境变量信息
	showEnvInfo()

	// 步骤 1: 加载配置
	fmt.Println("[步骤 1] 加载配置...")

	// 读取配置文件
	configData, err := os.ReadFile(configPath)
	if err != nil {
		fmt.Printf("❌ 读取配置文件失败: %v\n", err)
		os.Exit(1)
	}

	var simpleConfig SimpleConfig
	if err := json.Unmarshal(configData, &simpleConfig); err != nil {
		fmt.Printf("❌ 解析配置文件失败: %v\n", err)
		os.Exit(1)
	}

	// 从环境变量覆盖配置
	mcpURL := os.Getenv("SHELL_MCP_URL")
	mcpToken := os.Getenv("SHELL_MCP_TOKEN")

	if mcpURL != "" {
		if len(simpleConfig.Servers) == 0 {
			simpleConfig.Servers = append(simpleConfig.Servers, ServerConfig{Name: "default"})
		}
		simpleConfig.Servers[0].URL = mcpURL
		fmt.Printf("   使用环境变量 SHELL_MCP_URL: %s\n", mcpURL)
	} else if len(simpleConfig.Servers) > 0 && simpleConfig.Servers[0].URL != "" {
		fmt.Printf("   使用配置文件 URL: %s\n", simpleConfig.Servers[0].URL)
	} else {
		// 使用默认地址
		if len(simpleConfig.Servers) == 0 {
			simpleConfig.Servers = append(simpleConfig.Servers, ServerConfig{Name: "default"})
		}
		simpleConfig.Servers[0].URL = "http://172.25.1.75:8018"
		fmt.Printf("   使用默认地址: %s\n", simpleConfig.Servers[0].URL)
	}

	if mcpToken != "" {
		simpleConfig.Token = mcpToken
		fmt.Printf("   使用环境变量 SHELL_MCP_TOKEN\n")
	} else if simpleConfig.Token != "" {
		fmt.Printf("   使用配置文件 Token\n")
	} else {
		fmt.Printf("   警告: Token 为空\n")
	}

	// 步骤 2: 创建 MCP 客户端
	fmt.Println("[步骤 2] 创建 MCP 客户端...")

	// 转换配置
	serverConfigs := make([]configs.ServerConfig, len(simpleConfig.Servers))
	for i, s := range simpleConfig.Servers {
		serverConfigs[i] = configs.ServerConfig{Name: s.Name, URL: s.URL}
	}

	mcpConfig := &configs.ClientConfig{
		Servers: serverConfigs,
		Token:   simpleConfig.Token,
	}

	// 创建客户端选项
	opts := []mcpclient.Option{
		mcpclient.WithHeader("X-Cluster-Token", simpleConfig.Token),
		mcpclient.WithTimeout(15 * time.Second), // 15秒超时
		mcpclient.WithLogger(logger.GetSugar()),
	}

	mcpClient, err := mcpclient.NewClient(mcpConfig, opts...)
	if err != nil {
		fmt.Printf("❌ 创建 MCP 客户端失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("✅ MCP 客户端创建成功")

	// 步骤 3: 建立连接
	fmt.Println("[步骤 3] 连接到 Shell MCP Server...")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	connectStart := time.Now()
	err = mcpClient.Connect(ctx)
	connectDuration := time.Since(connectStart)

	if err != nil {
		fmt.Printf("❌ 连接失败: %v\n", err)
		fmt.Printf("   连接耗时: %v\n", connectDuration)
		analyzeError(err)
		os.Exit(1)
	}
	fmt.Printf("✅ 连接成功! (耗时: %v)\n", connectDuration)
	fmt.Println()

	// 获取 session
	session := mcpClient.GetSession()
	if session == nil {
		fmt.Println("❌ 获取 Session 失败")
		os.Exit(1)
	}
	fmt.Println("✅ Session 获取成功")
	fmt.Println()

	// 步骤 4: 列出可用工具
	fmt.Println("[步骤 4] 获取可用工具列表...")
	listToolsStart := time.Now()
	toolsResult, err := session.ListTools(ctx, &mcp.ListToolsParams{})
	listToolsDuration := time.Since(listToolsStart)

	if err != nil {
		fmt.Printf("❌ 获取工具列表失败: %v\n", err)
		fmt.Printf("   耗时: %v\n", listToolsDuration)
	} else {
		fmt.Printf("✅ 获取到 %d 个工具 (耗时: %v):\n", len(toolsResult.Tools), listToolsDuration)
		for i, tool := range toolsResult.Tools {
			fmt.Printf("   [%d] %s\n", i+1, tool.Name)
			if tool.Description != "" {
				desc := tool.Description
				if len(desc) > 80 {
					desc = desc[:80] + "..."
				}
				fmt.Printf("       描述: %s\n", desc)
			}
		}
	}
	fmt.Println()

	// 步骤 5: 执行测试命令
	fmt.Println("[步骤 5] 执行测试命令...")

	// 先测试一个简单的命令
	testCommands := []struct {
		name    string
		command string
	}{
		{"简单 echo", "echo hello"},
		{"日期时间", "date"},
		{"主机名", "hostname"},
		{"当前目录", "pwd"},
		{"ping 本地", "ping -n 1 127.0.0.1"},
		{"ping 目标", "ping -c 3 172.25.1.75"},
	}

	for _, tc := range testCommands {
		fmt.Printf("   测试命令: %s\n", tc.name)
		fmt.Printf("   命令: %s\n", tc.command)

		// 创建新的 context 以避免之前的超时影响
		cmdCtx, cmdCancel := context.WithTimeout(context.Background(), 30*time.Second)

		execStart := time.Now()
		result, err := session.CallTool(cmdCtx, &mcp.CallToolParams{
			Name: "execute_command",
			Arguments: map[string]interface{}{
				"command": tc.command,
			},
		})
		execDuration := time.Since(execStart)
		cmdCancel()

		if err != nil {
			fmt.Printf("   ❌ 命令执行失败: %v\n", err)
			fmt.Printf("   执行耗时: %v\n", execDuration)
		} else {
			fmt.Printf("   ✅ 命令执行成功! (耗时: %v)\n", execDuration)

			if result != nil && len(result.Content) > 0 {
				fmt.Println("   执行结果:")
				for i, content := range result.Content {
					if jsonBytes, err := json.Marshal(content); err == nil {
						text := string(jsonBytes)
						if len(text) > 300 {
							text = text[:300] + "..."
						}
						fmt.Printf("   --- Content[%d] ---\n%s\n", i, text)
					}
				}
			} else {
				fmt.Println("   无返回内容")
			}
		}
		fmt.Println()
	}

	// 步骤 6: 关闭连接
	fmt.Println("[步骤 6] 关闭连接...")
	if err := mcpClient.Close(); err != nil {
		fmt.Printf("❌ 关闭连接失败: %v\n", err)
	} else {
		fmt.Println("✅ 连接已关闭")
	}

	fmt.Println()
	fmt.Println("========================================")
	fmt.Println("测试完成")
	fmt.Println("========================================")
}

// showEnvInfo 显示环境变量信息
func showEnvInfo() {
	fmt.Println("环境变量配置:")
	mcpURL := os.Getenv("SHELL_MCP_URL")
	mcpToken := os.Getenv("SHELL_MCP_TOKEN")

	if mcpURL != "" {
		fmt.Printf("   SHELL_MCP_URL: %s\n", mcpURL)
	} else {
		fmt.Println("   SHELL_MCP_URL: (未设置)")
	}

	if mcpToken != "" {
		masked := mcpToken
		if len(masked) > 8 {
			masked = masked[:4] + "****" + masked[len(masked)-4:]
		}
		fmt.Printf("   SHELL_MCP_TOKEN: %s\n", masked)
	} else {
		fmt.Println("   SHELL_MCP_TOKEN: (未设置)")
	}
	fmt.Println()
}

// analyzeError 分析错误类型
func analyzeError(err error) {
	errStr := err.Error()

	fmt.Println("   错误分析:")
	if errStr == "" {
		return
	}

	if contains(errStr, "connection refused") {
		fmt.Println("   → 连接被拒绝 - 服务器可能未运行或端口错误")
	} else if contains(errStr, "i/o timeout") || contains(errStr, "timeout") {
		fmt.Println("   → 连接超时 - 服务器可能不可达或网络问题")
	} else if contains(errStr, "no such host") || contains(errStr, "server misbehaving") {
		fmt.Println("   → DNS 解析失败 - 服务器地址可能错误")
	} else if contains(errStr, "connection reset") {
		fmt.Println("   → 连接被重置 - 服务器可能拒绝连接")
	} else if contains(errStr, "wsarecv") {
		fmt.Println("   → Windows 网络错误 - 可能是 WebSocket 连接失败")
	} else if contains(errStr, "405") || contains(errStr, "Method Not Allowed") {
		fmt.Println("   → 方法不允许 - 端点可能需要 POST 请求或不同的协议")
	} else if contains(errStr, "400") || contains(errStr, "Bad Request") {
		fmt.Println("   → 请求格式错误 - 检查请求头和请求体")
	} else if contains(errStr, "401") || contains(errStr, "Unauthorized") {
		fmt.Println("   → 未授权 - Token 可能无效或缺失")
	} else if contains(errStr, "403") || contains(errStr, "Forbidden") {
		fmt.Println("   → 禁止访问 - 权限不足")
	} else if contains(errStr, "context deadline exceeded") {
		fmt.Println("   → 操作超时 - 服务器响应时间过长")
	} else if contains(errStr, "context canceled") {
		fmt.Println("   → 操作被取消 - 可能是由于超时或其他原因")
	}
}

// contains 简单的字符串包含检查
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsAt(s, substr))
}

func containsAt(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
