// Package k8s 提供与 K8s MCP Server 通信的 Client 实现
package k8s

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// Config 定义 K8s MCP Client 的配置
type Config struct {
	// ServerURL MCP Server 的地址（如 "https://localhost:8443"）
	ServerURL string `json:"server_url"`

	// Token 认证 Token
	Token string `json:"token"`

	// Insecure 是否跳过 TLS 验证（仅用于开发环境）
	Insecure bool `json:"insecure"`

	// Timeout 请求超时时间
	Timeout time.Duration `json:"timeout"`

	// RetryConfig 重试配置
	RetryConfig RetryConfig `json:"retry_config"`

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
		c.Timeout = 30 * time.Second // 默认 30 秒
	}

	if c.RetryConfig.MaxAttempts <= 0 {
		c.RetryConfig = DefaultRetryConfig()
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
	ListTools(ctx context.Context) ([]Tool, error)
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

// Tool 工具定义
type Tool struct {
	Name        string
	Description string
}

// RetryConfig 重试配置
type RetryConfig struct {
	MaxAttempts int           `json:"max_attempts"` // 最大重试次数
	InitialWait time.Duration `json:"initial_wait"` // 初始等待时间
	MaxWait     time.Duration `json:"max_wait"`     // 最大等待时间
}

// DefaultRetryConfig 返回默认的重试配置
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts: 3,
		InitialWait: 1 * time.Second,
		MaxWait:     10 * time.Second,
	}
}

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
// 注意：当前版本使用 MockClient 实现
func NewClient(config Config) (Client, error) {
	// 验证配置
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	// 返回 MockClient
	return NewMockClient(config), nil
}

// NewClientFromFile 从配置文件创建 K8s MCP Client
func NewClientFromFile(configPath string) (Client, error) {
	config := Config{
		ServerURL: "https://localhost:8443",
		Token:     "mock-token",
		Insecure:  true,
		Timeout:   30 * time.Second,
		RetryConfig: RetryConfig{
			MaxAttempts: 3,
			InitialWait: 1 * time.Second,
			MaxWait:     10 * time.Second,
		},
		SSEPath: "/sse",
	}

	return NewClient(config)
}

// NewRealClient 创建真实的 K8s MCP Client
// TODO: 实现真实的 MCP 连接逻辑
func NewRealClient(config Config) (Client, error) {
	_ = config
	// TODO: 实现真实的 MCP 连接逻辑
	// 当前返回 MockClient 作为占位符
	return NewMockClient(config), nil
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
