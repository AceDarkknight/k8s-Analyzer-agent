package shellmcp

import (
	"context"
	"fmt"

	"github.com/AceDarkknight/k8s-analyzer-agent/internal/logger"
	"github.com/AceDarkknight/shell-executor-mcp/pkg/configs"
	"github.com/AceDarkknight/shell-executor-mcp/pkg/mcpclient"
)

// ShellMCPClient Shell MCP 客户端
type ShellMCPClient struct {
	client    *mcpclient.Client
	serverURL string
	authToken string
	connected bool
}

// ExecuteResult 执行结果
type ExecuteResult struct {
	Summary string
	Output  string
	IsError bool
}

// NodeResult 单节点执行结果（兼容旧代码）
type NodeResult struct {
	NodeID   string
	Stdout   string
	Stderr   string
	ExitCode int
	Success  bool
}

// ToolInfo 工具信息
type ToolInfo struct {
	Name        string
	Description string
}

// NewShellMCPClient 创建 ShellMCPClient 实例（不立即连接）
func NewShellMCPClient(serverURL, authToken string) *ShellMCPClient {
	return &ShellMCPClient{
		serverURL: serverURL,
		authToken: authToken,
		connected: false,
	}
}

// Connect 建立到 MCP 服务器的连接
func (c *ShellMCPClient) Connect(ctx context.Context) error {
	if c.connected {
		return nil
	}

	// 创建客户端配置
	cfg := &configs.ClientConfig{
		Token: c.authToken,
		Servers: []configs.ServerConfig{
			{
				Name: "shell-mcp-server",
				URL:  c.serverURL,
			},
		},
	}

	// 创建客户端
	client, err := mcpclient.NewClient(cfg, mcpclient.WithLogger(logger.GetLogger().Sugar()))
	if err != nil {
		logger.Error("failed to create shell MCP client", logger.Err(err))
		return fmt.Errorf("failed to create shell MCP client: %w", err)
	}

	// 连接服务器
	if err := client.Connect(ctx); err != nil {
		logger.Error("failed to connect to shell MCP server", logger.Err(err))
		return fmt.Errorf("failed to connect to shell MCP server: %w", err)
	}

	c.client = client
	c.connected = true
	logger.Info("connected to shell MCP server", logger.String("url", c.serverURL))
	return nil
}

// Close 关闭 MCP 连接
func (c *ShellMCPClient) Close() error {
	if !c.connected || c.client == nil {
		return nil
	}

	c.client.Close()
	c.connected = false
	logger.Info("closed shell MCP connection")
	return nil
}

// IsConnected 返回连接状态
func (c *ShellMCPClient) IsConnected() bool {
	return c.connected
}

// ExecuteCommand 执行命令
func (c *ShellMCPClient) ExecuteCommand(ctx context.Context, command string) (*ExecuteResult, error) {
	if !c.connected || c.client == nil {
		return nil, fmt.Errorf("not connected to shell MCP server")
	}

	result, err := c.client.ExecuteCommand(ctx, command)
	if err != nil {
		logger.Error("failed to execute command", logger.Err(err), logger.String("command", command))
		return nil, fmt.Errorf("failed to execute command: %w", err)
	}

	// 获取文本内容
	contents := result.GetTextContents()
	output := ""
	for _, content := range contents {
		output += content + "\n"
	}

	return &ExecuteResult{
		Summary: result.String(),
		Output:  output,
		IsError: result.IsError,
	}, nil
}

// ListTools 获取可用工具列表
func (c *ShellMCPClient) ListTools(ctx context.Context) ([]ToolInfo, error) {
	// shell-executor-mcp 的客户端没有直接提供 ListTools 方法
	// 返回一个固定的工具信息
	return []ToolInfo{
		{
			Name:        "execute_command",
			Description: "在服务器集群上执行 Shell 命令",
		},
	}, nil
}
