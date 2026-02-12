// Package k8s 提供与 K8s MCP Server 通信的 Client 实现
package k8s

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/AceDarkknight/k8s-analyzer-agent/internal/client"
	"github.com/AceDarkknight/k8s-mcp/pkg/mcpclient"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Config 定义 K8s MCP Client 的配置
type Config struct {
	// ServerURL MCP Server 的地址（如 "https://localhost:8443"）
	ServerURL string `json:"server_url"`

	// Token 认证 Token
	Token string `json:"token"`

	// Insecure 是否跳过 TLS 验证（仅用于开发环境）
	Insecure bool `json:"insecure"`

	// Timeout 请求超时时间（秒）
	Timeout int `json:"timeout"`

	// RetryConfig 重试配置
	RetryConfig client.RetryConfig `json:"retry_config"`

	// SSEPath SSE 端点路径（默认为 "/sse"）
	SSEPath string `json:"sse_path"`
}

// Validate 验证配置是否有效
func (c *Config) Validate() error {
	if c.ServerURL == "" {
		return fmt.Errorf("server_url is required")
	}

	// 验证 URL 格式
	if _, err := url.Parse(c.ServerURL); err != nil {
		return fmt.Errorf("invalid server_url: %w", err)
	}

	if c.Token == "" {
		return fmt.Errorf("token is required")
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

// Client K8s MCP Client 接口
// 使用接口而不是具体实现，方便测试和 Mock
type Client interface {
	Connect(ctx context.Context) error
	Close() error
	CallTool(ctx context.Context, name string, args map[string]interface{}) (*CallToolResult, error)
	ListTools(ctx context.Context) ([]client.Tool, error)
	IsConnected() bool
	HealthCheck(ctx context.Context) error
	GetConfig() Config
	UpdateConfig(config Config) error
}

// CallToolResult 工具调用结果
type CallToolResult struct {
	Content []Content
}

// Content 内容接口
type Content interface{}

// ConnectionError 连接错误
type ConnectionError struct {
	Reason string
	Err    error
}

func (e *ConnectionError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("connection failed: %s: %v", e.Reason, e.Err)
	}
	return fmt.Sprintf("connection failed: %s", e.Reason)
}

// Unwrap 返回底层错误
func (e *ConnectionError) Unwrap() error {
	return e.Err
}

// ToolExecutionError 工具执行错误
type ToolExecutionError struct {
	ToolName string
	Reason   string
	Err      error
}

func (e *ToolExecutionError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("tool '%s' execution failed: %s: %v", e.ToolName, e.Reason, e.Err)
	}
	return fmt.Sprintf("tool '%s' execution failed: %s", e.ToolName, e.Reason)
}

// Unwrap 返回底层错误
func (e *ToolExecutionError) Unwrap() error {
	return e.Err
}

// NewClient 创建新的 K8s MCP Client
// 使用 RealClient 实现
func NewClient(config Config) (Client, error) {
	return NewRealClient(config)
}

// NewClientFromFile 从配置文件创建 K8s MCP Client
func NewClientFromFile(configPath string) (Client, error) {
	config, err := client.LoadConfig[Config](configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	// 从环境变量读取配置，覆盖配置文件中的值
	// 优先级：环境变量 > 配置文件
	if envURL := os.Getenv("K8S_MCP_URL"); envURL != "" {
		config.ServerURL = envURL
		fmt.Printf("Using K8S_MCP_URL from environment variable: %s\n", envURL)
	}

	if envToken := os.Getenv("K8S_MCP_TOKEN"); envToken != "" {
		config.Token = envToken
		fmt.Println("Using K8S_MCP_TOKEN from environment variable")

	}

	return NewClient(*config)
}

// NewRealClient 创建真实的 K8s MCP Client
// 使用 k8s-mcp/pkg/mcpclient 提供的客户端实现
func NewRealClient(config Config) (Client, error) {
	// 验证配置
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	// 创建 mcpclient.Config
	mcpConfig := mcpclient.Config{
		ServerURL:          config.ServerURL,
		AuthToken:          config.Token,
		InsecureSkipVerify: config.Insecure,
		UserAgent:          "k8s-analyzer-agent/1.0.0",
	}

	// 创建 mcpclient.Client
	mcpClient, err := mcpclient.NewClient(mcpConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create mcp client: %w", err)
	}

	// 创建 RealClient
	return &RealClient{
		config:    config,
		mcpClient: mcpClient,
		connected: false,
	}, nil
}

// RealClient 真实的 K8s MCP Client 实现
type RealClient struct {
	config    Config
	mcpClient *mcpclient.Client
	connected bool
}

// Connect 建立连接
func (c *RealClient) Connect(ctx context.Context) error {
	if err := c.mcpClient.Connect(ctx); err != nil {
		return &ConnectionError{
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

// CallTool 调用工具
// 使用重试机制实现降级保护（Downgrade Protection）
func (c *RealClient) CallTool(ctx context.Context, name string, args map[string]interface{}) (*CallToolResult, error) {
	if !c.connected {
		return nil, &ToolExecutionError{
			ToolName: name,
			Reason:   "client not connected",
		}
	}

	// 使用重试机制执行工具调用
	result, err := client.RetryWithResult(
		ctx,
		c.config.RetryConfig,
		func() (*mcp.CallToolResult, error) {
			result, err := c.mcpClient.CallTool(ctx, name, args)
			if err != nil {
				return nil, err
			}
			return result, nil
		},
	)

	if err != nil {
		return nil, &ToolExecutionError{
			ToolName: name,
			Reason:   "failed to call tool after retries",
			Err:      err,
		}
	}

	// 转换 mcp.CallToolResult 为 CallToolResult
	callToolResult := &CallToolResult{
		Content: convertContent(result.Content),
	}

	return callToolResult, nil
}

// ListTools 获取工具列表
func (c *RealClient) ListTools(ctx context.Context) ([]client.Tool, error) {
	if !c.connected {
		return nil, fmt.Errorf("client not connected")
	}

	tools, err := c.mcpClient.ListTools(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list tools: %w", err)
	}

	// 转换 mcp.Tool 为 client.Tool
	result := make([]client.Tool, 0, len(tools))
	for _, tool := range tools {
		// 序列化 InputSchema 为 json.RawMessage
		var inputSchema json.RawMessage
		if tool.InputSchema != nil {
			schemaBytes, err := json.Marshal(tool.InputSchema)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal input schema for tool %s: %w", tool.Name, err)
			}
			inputSchema = schemaBytes
		}

		result = append(result, client.Tool{
			Name:        tool.Name,
			Description: tool.Description,
			InputSchema: inputSchema,
		})
	}

	return result, nil
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
	_, err := c.mcpClient.ListTools(ctx)
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

// convertContent 转换 mcp.Content 为 Content
func convertContent(contents []mcp.Content) []Content {
	result := make([]Content, 0, len(contents))
	for _, content := range contents {
		result = append(result, content)
	}
	return result
}

// HTTPClientConfig HTTP 客户端配置
type HTTPClientConfig struct {
	Timeout            time.Duration
	InsecureSkipVerify bool
}

// NewHTTPClient 创建新的 HTTP 客户端
func NewHTTPClient(config HTTPClientConfig) *http.Client {
	return &http.Client{
		Timeout: config.Timeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: config.InsecureSkipVerify,
			},
		},
	}
}
