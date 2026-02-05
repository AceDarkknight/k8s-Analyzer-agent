// Package shell 提供与 Shell Executor MCP Server 通信的 Client 实现
package shell

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/AceDarkknight/k8s-analyzer-agent/internal/client"
	"github.com/AceDarkknight/k8s-analyzer-agent/internal/logger"
	"github.com/AceDarkknight/shell-executor-mcp/pkg/configs"
	"github.com/AceDarkknight/shell-executor-mcp/pkg/mcpclient"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ServerConfig 定义单个 Shell Executor Server 的配置
type ServerConfig struct {
	// Name 服务器名称（用于标识）
	Name string `json:"name"`

	// URL 服务器地址（如 "http://localhost:8080"）
	URL string `json:"url"`

	// Token 认证 Token（可选）
	Token string `json:"token,omitempty"`
}

// Config 定义 Shell Executor MCP Client 的配置
type Config struct {
	// Servers 服务器列表（支持故障转移）
	Servers []ServerConfig `json:"servers"`

	// Timeout 请求超时时间（秒）
	Timeout int `json:"timeout"`

	// RetryConfig 重试配置
	RetryConfig client.RetryConfig `json:"retry_config"`

	// SSEPath SSE 端点路径（默认为 "/sse"）
	SSEPath string `json:"sse_path"`

	// EnableFailover 是否启用故障转移
	EnableFailover bool `json:"enable_failover"`
}

// Validate 验证配置是否有效
func (c *Config) Validate() error {
	if len(c.Servers) == 0 {
		return fmt.Errorf("at least one server is required")
	}

	for i, server := range c.Servers {
		if server.Name == "" {
			return fmt.Errorf("server[%d]: name is required", i)
		}
		if server.URL == "" {
			return fmt.Errorf("server[%d]: url is required", i)
		}
		// 验证 URL 格式
		if _, err := url.Parse(server.URL); err != nil {
			return fmt.Errorf("server[%d]: invalid url: %w", i, err)
		}
	}

	if c.Timeout <= 0 {
		c.Timeout = 30 // 默认 30 秒
	}

	if c.RetryConfig.MaxAttempts <= 0 {
		c.RetryConfig = client.DefaultRetryConfig()
	}

	if c.SSEPath == "" {
		c.SSEPath = "/sse" // 默认 SSE 路径
	}

	return nil
}

// Client Shell Executor MCP Client 实现
type Client struct {
	config       Config
	mcpClient    *mcpclient.Client
	mcpSession   *mcp.ClientSession
	httpClient   *http.Client
	connected    bool
	currentIndex int // 当前使用的服务器索引
}

// NewClient 创建新的 Shell Executor MCP Client
func NewClient(config Config) (*Client, error) {
	return NewRealClient(&config)
}

// NewRealClient 创建真实的 Shell Executor MCP Client
// 使用 shell-executor-mcp/pkg/mcpclient 提供的客户端实现
func NewRealClient(config *Config) (*Client, error) {
	// 验证配置
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	// 创建 mcpclient.Config
	servers := make([]configs.ServerConfig, len(config.Servers))
	for i, s := range config.Servers {
		servers[i] = configs.ServerConfig{Name: s.Name, URL: s.URL}
	}
	mcpConfig := &configs.ClientConfig{
		Servers: servers,
		Log: configs.LogConfig{
			Level:  "info",
			LogDir: "logs/shell",
		},
	}

	// 创建 mcpclient.Client
	mcpClient, err := mcpclient.NewClient(mcpConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create mcp client: %w", err)
	}

	// 创建 RealClient
	return &Client{
		config:    *config,
		mcpClient: mcpClient,
		connected: false,
	}, nil
}

// RealClient 真实的 Shell Executor MCP Client 实现
type RealClient struct {
	config    Config
	mcpClient *mcpclient.Client
	connected bool
}

// Connect 建立连接
func (c *RealClient) Connect(ctx context.Context) error {
	if err := c.mcpClient.Connect(ctx); err != nil {
		return &client.ConnectionError{
			Reason: "failed to connect to MCP server",
			Err:    err,
		}
	}
	c.connected = true
	return nil
}

// Close 关闭连接
func (c *RealClient) Close() error {
	c.connected = false
	return c.mcpClient.Close()
}

// CallTool 执行 Shell Executor MCP Server 上的特定工具
func (c *RealClient) CallTool(ctx context.Context, name string, args map[string]interface{}) (*mcp.CallToolResult, error) {
	if !c.connected {
		return nil, &client.ToolExecutionError{
			ToolName: name,
			Reason:   "client not connected",
		}
	}

	result, err := c.mcpClient.GetSession().CallTool(ctx, &mcp.CallToolParams{
		Name:      name,
		Arguments: args,
	})
	if err != nil {
		return nil, &client.ToolExecutionError{
			ToolName: name,
			Reason:   "failed to call tool",
			Err:      err,
		}
	}

	return result, nil
}

// ListTools 获取工具列表
func (c *RealClient) ListTools(ctx context.Context) ([]*mcp.Tool, error) {
	if !c.connected {
		return nil, fmt.Errorf("client not connected")
	}

	tools, err := c.mcpClient.GetSession().ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		return nil, fmt.Errorf("failed to list tools: %w", err)
	}

	return tools.Tools, nil
}

// IsConnected 检查是否已连接
func (c *RealClient) IsConnected() bool {
	return c.connected
}

// HealthCheck 健康检查
func (c *RealClient) HealthCheck(ctx context.Context) error {
	if !c.connected {
		return fmt.Errorf("client not connected")
	}
	// 尝试列出工具作为健康检查
	_, err := c.mcpClient.GetSession().ListTools(ctx, &mcp.ListToolsParams{})
	return err
}

// GetConfig 获取配置
func (c *RealClient) GetConfig() Config {
	return c.config
}

// UpdateConfig 更新配置
func (c *RealClient) UpdateConfig(config Config) error {
	if err := config.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}
	c.config = config
	return nil
}

// NewClientFromFile 从配置文件创建 Shell Executor MCP Client
// 支持从环境变量读取配置，环境变量优先级高于配置文件
func NewClientFromFile(configPath string) (*Client, error) {
	config, err := client.LoadConfig[Config](configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	// 如果配置文件中没有 servers，使用默认值
	if len(config.Servers) == 0 {
		config.Servers = []ServerConfig{
			{
				Name: "default",
				URL:  "http://localhost:8080",
			},
		}
	}

	// 从环境变量读取配置，覆盖配置文件中的值
	// 优先级：环境变量 > 配置文件
	if envURL := os.Getenv("SHELL_MCP_URL"); envURL != "" {
		// 覆盖第一个服务器的 URL
		config.Servers[0].URL = envURL
		logger.Info("Using SHELL_MCP_URL from environment variable", logger.String("url", envURL))
	}

	if envToken := os.Getenv("SHELL_MCP_TOKEN"); envToken != "" {
		// 覆盖第一个服务器的 Token
		config.Servers[0].Token = envToken
		logger.Info("Using SHELL_MCP_TOKEN from environment variable")
	}

	return NewRealClient(config)
}

// Connect 建立与 Shell Executor MCP Server 的连接
func (c *Client) Connect(ctx context.Context) error {
	if c.connected {
		logger.Debug("Already connected to Shell MCP Server")
		return nil // 已经连接
	}

	logger.Info("Attempting to connect to Shell MCP Server")

	// 使用 mcpclient 连接
	if err := c.mcpClient.Connect(ctx); err != nil {
		return &client.ConnectionError{
			Reason: "failed to connect to MCP server",
			Err:    err,
		}
	}

	// 连接成功
	c.mcpSession = c.mcpClient.GetSession()
	c.connected = true
	logger.Info("Successfully connected to Shell MCP Server")
	return nil
}

// Close 终止与 Shell Executor MCP Server 的连接
func (c *Client) Close() error {
	if !c.connected || c.mcpClient == nil {
		return nil
	}

	err := c.mcpClient.Close()
	c.connected = false
	c.mcpClient = nil
	c.mcpSession = nil

	return err
}

// CallTool 执行 Shell Executor MCP Server 上的特定工具
func (c *Client) CallTool(ctx context.Context, name string, args map[string]interface{}) (*mcp.CallToolResult, error) {
	if !c.connected {
		logger.Error("Cannot call tool: client is not connected", logger.String("tool", name))
		return nil, &client.ConnectionError{Reason: "client is not connected"}
	}

	logger.Debug("Calling tool", logger.String("tool", name), logger.Any("args", args))

	// 使用重试机制执行工具调用
	result, err := client.RetryWithResult(
		ctx,
		c.config.RetryConfig,
		func() (*mcp.CallToolResult, error) {
			result, err := c.mcpSession.CallTool(ctx, &mcp.CallToolParams{
				Name:      name,
				Arguments: args,
			})
			if err != nil {
				return nil, err
			}
			return result, nil
		},
	)

	if err != nil {
		logger.Error("Tool execution failed", logger.String("tool", name), logger.Err(err))
		return nil, &client.ToolExecutionError{
			ToolName: name,
			Reason:   "tool execution failed",
			Err:      err,
		}
	}

	logger.Debug("Tool call succeeded", logger.String("tool", name))
	return result, nil
}

// ListTools 获取 Shell Executor MCP Server 上可用的工具列表
func (c *Client) ListTools(ctx context.Context) ([]*mcp.Tool, error) {
	if !c.connected {
		logger.Error("Cannot list tools: client is not connected")
		return nil, &client.ConnectionError{Reason: "client is not connected"}
	}

	logger.Debug("Listing available tools")
	result, err := c.mcpSession.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		logger.Error("Failed to list tools", logger.Err(err))
		return nil, &client.ToolExecutionError{
			Reason: "failed to list tools",
			Err:    err,
		}
	}

	logger.Debug("Successfully listed tools", logger.Int("count", len(result.Tools)))
	return result.Tools, nil
}

// IsConnected 检查客户端是否已连接
func (c *Client) IsConnected() bool {
	return c.connected
}

// HealthCheck 执行健康检查
func (c *Client) HealthCheck(ctx context.Context) error {
	if !c.connected {
		return &client.ConnectionError{Reason: "client is not connected"}
	}

	// 通过调用 list_tools 来检查连接是否正常
	_, err := c.ListTools(ctx)
	if err != nil {
		return &client.ConnectionError{Reason: "health check failed", Err: err}
	}

	return nil
}

// GetConfig 返回当前配置
func (c *Client) GetConfig() Config {
	return c.config
}

// GetCurrentServer 返回当前连接的服务器配置
func (c *Client) GetCurrentServer() *ServerConfig {
	if !c.connected || c.currentIndex >= len(c.config.Servers) {
		return nil
	}
	return &c.config.Servers[c.currentIndex]
}

// isConnectionError 判断错误是否为连接错误
func (c *Client) isConnectionError(err error) bool {
	if err == nil {
		return false
	}

	// 检查是否为连接错误
	if _, ok := err.(*client.ConnectionError); ok {
		return true
	}

	// 检查是否为超时错误
	if err == context.DeadlineExceeded {
		return true
	}

	return false
}

// UpdateConfig 更新配置（需要重新连接）
func (c *Client) UpdateConfig(config Config) error {
	// 验证新配置
	if err := config.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	// 如果已连接，先关闭连接
	if c.connected {
		if err := c.Close(); err != nil {
			return fmt.Errorf("failed to close existing connection: %w", err)
		}
	}

	// 更新配置
	c.config = config

	// 更新 HTTP Client
	c.httpClient = &http.Client{
		Timeout: time.Duration(config.Timeout) * time.Second,
	}

	return nil
}
