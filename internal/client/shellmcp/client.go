package shellmcp

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/AceDarkknight/k8s-analyzer-agent/internal/logger"
	"github.com/AceDarkknight/shell-executor-mcp/pkg/configs"
	"github.com/AceDarkknight/shell-executor-mcp/pkg/mcpclient"
)

// ShellMCPClient Shell MCP 客户端（懒连接模式）
type ShellMCPClient struct {
	mu             sync.Mutex
	client         *mcpclient.Client
	serverURL      string
	authToken      string
	connected      bool
	timeoutSeconds int // 命令执行超时（秒）
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

// NewShellMCPClient 创建 ShellMCPClient 实例（不立即连接，首次执行命令时自动连接）
func NewShellMCPClient(serverURL, authToken string, timeoutSeconds int) *ShellMCPClient {
	if timeoutSeconds <= 0 {
		timeoutSeconds = 30
	}
	return &ShellMCPClient{
		serverURL:      serverURL,
		authToken:      authToken,
		connected:      false,
		timeoutSeconds: timeoutSeconds,
	}
}

// Connect 建立到 MCP 服务器的连接
func (c *ShellMCPClient) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.connectLocked(ctx)
}

// connectLocked 在持有锁的情况下建立连接（内部方法）
func (c *ShellMCPClient) connectLocked(ctx context.Context) error {
	if c.connected && c.client != nil {
		return nil
	}

	// 先关闭旧连接（如果有）
	if c.client != nil {
		c.client.Close()
		c.client = nil
		c.connected = false
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

	// 使用带超时的 context 连接
	connectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if err := client.Connect(connectCtx); err != nil {
		logger.Error("failed to connect to shell MCP server", logger.Err(err))
		return fmt.Errorf("failed to connect to shell MCP server: %w", err)
	}

	c.client = client
	c.connected = true
	logger.Info("connected to shell MCP server", logger.String("url", c.serverURL))
	return nil
}

// reconnect 关闭旧连接并重新建立连接
func (c *ShellMCPClient) reconnect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 强制关闭旧连接
	if c.client != nil {
		c.client.Close()
		c.client = nil
		c.connected = false
	}

	logger.Info("reconnecting to shell MCP server", logger.String("url", c.serverURL))
	return c.connectLocked(ctx)
}

// Close 关闭 MCP 连接
func (c *ShellMCPClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected || c.client == nil {
		return nil
	}

	c.client.Close()
	c.client = nil
	c.connected = false
	logger.Info("closed shell MCP connection")
	return nil
}

// IsConnected 返回连接状态
func (c *ShellMCPClient) IsConnected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.connected
}

// connectionErrorPatterns 连接相关错误的匹配模式（包级常量，避免每次调用分配）
var connectionErrorPatterns = []string{
	"eof", "broken pipe", "connection reset", "connection refused",
	"connection closed", "connection attempt failed", "wsarecv",
	"stream closed", "i/o timeout", "timeout", "context deadline exceeded",
	"session is closed", "not connected", "failed to respond",
}

// isConnectionError 检查是否为连接相关错误
func isConnectionError(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())
	for _, p := range connectionErrorPatterns {
		if strings.Contains(errStr, p) {
			return true
		}
	}
	return false
}

// ExecuteCommand 执行命令（自动懒连接 + 失败重连）
func (c *ShellMCPClient) ExecuteCommand(ctx context.Context, command string) (*ExecuteResult, error) {
	// 懒连接：首次调用时自动建立连接
	if err := c.Connect(ctx); err != nil {
		return nil, fmt.Errorf("lazy connect failed: %w", err)
	}

	// 为命令执行设置超时
	execCtx, cancel := context.WithTimeout(ctx, time.Duration(c.timeoutSeconds)*time.Second)
	defer cancel()

	result, err := c.executeWithRetry(execCtx, command)
	if err != nil {
		if execCtx.Err() == context.DeadlineExceeded {
			logger.Error("command execution timed out",
				logger.String("command", command),
				logger.Int("timeout_seconds", c.timeoutSeconds))
			return nil, fmt.Errorf("command execution timed out after %ds: %s", c.timeoutSeconds, command)
		}
		return nil, err
	}

	// 获取文本内容
	contents := result.GetTextContents()
	output := ""
	for _, content := range contents {
		output += content + "\n"
	}
	if strings.TrimSpace(output) == "" {
		fallback := strings.TrimSpace(result.String())
		if fallback != "" {
			output = fallback + "\n"
		}
	}

	return &ExecuteResult{
		Summary: result.String(),
		Output:  output,
		IsError: result.IsError,
	}, nil
}

// executeWithRetry 执行命令，连接错误时主动重连重试一次
func (c *ShellMCPClient) executeWithRetry(ctx context.Context, command string) (*mcpclient.Result, error) {
	c.mu.Lock()
	client := c.client
	c.mu.Unlock()

	if client == nil {
		return nil, fmt.Errorf("not connected to shell MCP server")
	}

	result, err := client.ExecuteCommand(ctx, command)
	if err == nil {
		return result, nil
	}

	// 检查是否为连接错误，如果是则重连重试
	if !isConnectionError(err) {
		logger.Error("failed to execute command (non-connection error)",
			logger.Err(err), logger.String("command", command))
		return nil, fmt.Errorf("failed to execute command: %w", err)
	}

	logger.Warn("command failed with connection error, attempting reconnect",
		logger.Err(err), logger.String("command", command))

	// 重连：使用独立的 context，不受原命令超时限制
	reconnCtx, reconnCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer reconnCancel()
	if reconnErr := c.reconnect(reconnCtx); reconnErr != nil {
		logger.Error("reconnect failed", logger.Err(reconnErr))
		return nil, fmt.Errorf("reconnect failed after connection error: %w (original: %v)", reconnErr, err)
	}

	// 重连成功，用新的超时 context 重试命令
	retryCtx, retryCancel := context.WithTimeout(context.Background(), time.Duration(c.timeoutSeconds)*time.Second)
	defer retryCancel()

	c.mu.Lock()
	client = c.client
	c.mu.Unlock()

	result, err = client.ExecuteCommand(retryCtx, command)
	if err != nil {
		logger.Error("failed to execute command after reconnect",
			logger.Err(err), logger.String("command", command))
		return nil, fmt.Errorf("failed to execute command after reconnect: %w", err)
	}

	return result, nil
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
