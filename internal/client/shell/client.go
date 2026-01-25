// Package shell 提供与 Shell Executor MCP Server 通信的 Client 实现
package shell

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/your-org/k8s-analyzer-agent/internal/client"
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

	// Timeout 请求超时时间
	Timeout time.Duration `json:"timeout"`

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
		c.Timeout = 30 * time.Second // 默认 30 秒
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
	mcpClient    *mcp.Client
	mcpSession   *mcp.ClientSession
	httpClient   *http.Client
	connected    bool
	currentIndex int // 当前使用的服务器索引
}

// NewClient 创建新的 Shell Executor MCP Client
func NewClient(config Config) (*Client, error) {
	// 验证配置
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	// 创建 HTTP Client
	httpClient := &http.Client{
		Timeout: config.Timeout,
	}

	return &Client{
		config:     config,
		httpClient: httpClient,
		connected:  false,
	}, nil
}

// NewClientFromFile 从配置文件创建 Shell Executor MCP Client
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

	return NewClient(*config)
}

// Connect 建立与 Shell Executor MCP Server 的连接
func (c *Client) Connect(ctx context.Context) error {
	if c.connected {
		return nil // 已经连接
	}

	// 尝试连接到第一个可用的服务器
	for i := 0; i < len(c.config.Servers); i++ {
		server := c.config.Servers[i]

		// 构建 SSE URL
		sseURL, err := url.Parse(server.URL)
		if err != nil {
			continue // 跳过无效的 URL
		}

		// 确保 URL 路径以 /sse 结尾
		if sseURL.Path == "" || sseURL.Path == "/" {
			sseURL.Path = c.config.SSEPath
		}

		// 创建 HTTP Headers
		headers := make(map[string]string)
		if server.Token != "" {
			headers["Authorization"] = fmt.Sprintf("Bearer %s", server.Token)
		}

		// 创建 MCP Client
		impl := &mcp.Implementation{}
		mcpClient := mcp.NewClient(impl, &mcp.ClientOptions{})

		// 创建 SSE Transport
		transport := &mcp.SSEClientTransport{
			Endpoint:   sseURL.String(),
			HTTPClient: c.httpClient,
		}

		// 初始化连接
		session, err := mcpClient.Connect(ctx, transport, &mcp.ClientSessionOptions{})
		if err != nil {
			continue // 跳过初始化失败的客户端
		}

		// 连接成功
		c.mcpClient = mcpClient
		c.mcpSession = session
		c.connected = true
		c.currentIndex = i
		return nil
	}

	// 所有服务器都连接失败
	return &client.ConnectionError{
		Reason: "failed to connect to any server",
	}
}

// Close 终止与 Shell Executor MCP Server 的连接
func (c *Client) Close() error {
	if !c.connected || c.mcpSession == nil {
		return nil
	}

	err := c.mcpSession.Close()
	c.connected = false
	c.mcpClient = nil
	c.mcpSession = nil

	return err
}

// CallTool 执行 Shell Executor MCP Server 上的特定工具
func (c *Client) CallTool(ctx context.Context, name string, args map[string]interface{}) (*mcp.CallToolResult, error) {
	if !c.connected {
		return nil, &client.ConnectionError{Reason: "client is not connected"}
	}

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
		// 如果启用了故障转移且当前服务器不可用，尝试切换到备用服务器
		if c.config.EnableFailover && c.isConnectionError(err) {
			if failoverErr := c.failover(ctx); failoverErr == nil {
				// 故障转移成功，重试工具调用
				result, err := c.mcpSession.CallTool(ctx, &mcp.CallToolParams{
					Name:      name,
					Arguments: args,
				})
				if err != nil {
					return nil, err
				}
				return result, nil
			}
		}

		return nil, &client.ToolExecutionError{
			ToolName: name,
			Reason:   "tool execution failed",
			Err:      err,
		}
	}

	return result, nil
}

// ListTools 获取 Shell Executor MCP Server 上可用的工具列表
func (c *Client) ListTools(ctx context.Context) ([]*mcp.Tool, error) {
	if !c.connected {
		return nil, &client.ConnectionError{Reason: "client is not connected"}
	}

	result, err := c.mcpSession.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		return nil, &client.ToolExecutionError{
			Reason: "failed to list tools",
			Err:    err,
		}
	}

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

// failover 切换到备用服务器
func (c *Client) failover(ctx context.Context) error {
	// 关闭当前连接
	if c.mcpSession != nil {
		c.mcpSession.Close()
	}
	c.connected = false
	c.mcpClient = nil
	c.mcpSession = nil

	// 尝试连接到下一个服务器
	for i := 1; i < len(c.config.Servers); i++ {
		nextIndex := (c.currentIndex + i) % len(c.config.Servers)
		server := c.config.Servers[nextIndex]

		// 构建 SSE URL
		sseURL, err := url.Parse(server.URL)
		if err != nil {
			continue
		}

		// 确保 URL 路径以 /sse 结尾
		if sseURL.Path == "" || sseURL.Path == "/" {
			sseURL.Path = c.config.SSEPath
		}

		// 创建 MCP Client
		impl := &mcp.Implementation{}
		mcpClient := mcp.NewClient(impl, &mcp.ClientOptions{})

		// 创建 SSE Transport
		transport := &mcp.SSEClientTransport{
			Endpoint:   sseURL.String(),
			HTTPClient: c.httpClient,
		}

		// 初始化连接
		session, err := mcpClient.Connect(ctx, transport, &mcp.ClientSessionOptions{})
		if err != nil {
			continue
		}

		// 连接成功
		c.mcpClient = mcpClient
		c.mcpSession = session
		c.connected = true
		c.currentIndex = nextIndex
		return nil
	}

	// 所有服务器都连接失败
	return &client.ConnectionError{Reason: "failover failed: no available servers"}
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
		Timeout: config.Timeout,
	}

	return nil
}
