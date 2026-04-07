package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/AceDarkknight/k8s-analyzer-agent/internal/logger"
)

// GatewayClient Gateway HTTP 客户端
type GatewayClient struct {
	baseURL    string
	httpClient *http.Client
	authToken  string
}

// NewGatewayClient 创建新的 Gateway 客户端
func NewGatewayClient(baseURL, authToken string, timeoutSeconds int) (*GatewayClient, error) {
	if baseURL == "" {
		return nil, fmt.Errorf("baseURL cannot be empty")
	}

	if timeoutSeconds <= 0 {
		timeoutSeconds = 30
	}

	return &GatewayClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: time.Duration(timeoutSeconds) * time.Second,
		},
		authToken: authToken,
	}, nil
}

// Execute 执行 kubectl 请求
func (c *GatewayClient) Execute(ctx context.Context, req *KubectlRequest) (*KubectlResponse, error) {
	url := c.baseURL + "/execute"

	// 序列化请求体
	reqBody, err := json.Marshal(req)
	if err != nil {
		logger.Error("failed to marshal request", logger.Err(err))
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// 创建 HTTP 请求
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(reqBody))
	if err != nil {
		logger.Error("failed to create request", logger.Err(err))
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// 设置请求头
	httpReq.Header.Set("Content-Type", "application/json")
	if c.authToken != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.authToken)
	}

	logger.Info("sending gateway request",
		logger.String("verb", req.Verb),
		logger.String("resource", req.Resource),
		logger.String("namespace", req.Namespace),
		logger.String("name", req.Name),
	)

	// 发送请求
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		logger.Error("failed to send request", logger.Err(err))
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// 读取原始响应体
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.Error("failed to read response body", logger.Err(err))
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// 调试：记录原始响应
	bodyPreview := string(bodyBytes)
	if len(bodyPreview) > 500 {
		bodyPreview = bodyPreview[:500] + "..."
	}
	logger.Info("received gateway raw response",
		logger.String("body_preview", bodyPreview),
		logger.Int("body_length", len(bodyBytes)))

	// 解析响应
	var kubectlResp KubectlResponse
	if err := json.Unmarshal(bodyBytes, &kubectlResp); err != nil {
		logger.Error("failed to decode response", logger.Err(err))
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	logger.Info("received gateway response",
		logger.String("request_id", kubectlResp.RequestID),
		logger.String("status", kubectlResp.Status),
		logger.Int("exit_code", kubectlResp.ExitCode),
		logger.Int("duration_ms", kubectlResp.DurationMs),
		logger.Int("stdout_length", len(kubectlResp.Stdout)),
	)

	// 检查响应状态
	if kubectlResp.Status == "error" {
		errMsg := fmt.Sprintf("gateway request failed: exit_code=%d, stderr=%s", kubectlResp.ExitCode, kubectlResp.Stderr)
		if kubectlResp.BlockedReason != "" {
			errMsg = fmt.Sprintf("%s, blocked_reason=%s", errMsg, kubectlResp.BlockedReason)
		}
		logger.Error("gateway request failed",
			logger.String("request_id", kubectlResp.RequestID),
			logger.Int("exit_code", kubectlResp.ExitCode),
			logger.String("stderr", kubectlResp.Stderr),
			logger.String("blocked_reason", kubectlResp.BlockedReason),
		)
		return &kubectlResp, fmt.Errorf("%s", errMsg)
	}

	return &kubectlResp, nil
}

// ListPods 列出 Pod
func (c *GatewayClient) ListPods(ctx context.Context, ns, labelSelector string) (*KubectlResponse, error) {
	req := &KubectlRequest{
		Verb:      "get",
		Resource:  "pods",
		Namespace: ns,
		Output:    "json",
		Mode:      "structured",
	}

	if labelSelector != "" {
		req.Options = &KubectlOptions{
			LabelSelector: labelSelector,
		}
	}

	return c.Execute(ctx, req)
}

// DescribePod 描述 Pod
func (c *GatewayClient) DescribePod(ctx context.Context, ns, name string) (*KubectlResponse, error) {
	req := &KubectlRequest{
		Verb:      "describe",
		Resource:  "pod",
		Namespace: ns,
		Name:      name,
		Mode:      "structured",
	}

	return c.Execute(ctx, req)
}

// GetLogs 获取 Pod 日志
func (c *GatewayClient) GetLogs(ctx context.Context, ns, pod, container string, tailLines int) (*KubectlResponse, error) {
	req := &KubectlRequest{
		Verb:      "logs",
		Resource:  "pod",
		Namespace: ns,
		Name:      pod,
		Mode:      "structured",
		Options:   &KubectlOptions{},
	}

	if container != "" {
		req.Options.Container = container
	}
	if tailLines > 0 {
		req.Options.TailLines = tailLines
	}

	return c.Execute(ctx, req)
}

// ListEvents 列出事件
func (c *GatewayClient) ListEvents(ctx context.Context, ns string) (*KubectlResponse, error) {
	req := &KubectlRequest{
		Verb:      "get",
		Resource:  "events",
		Namespace: ns,
		Output:    "json",
		Mode:      "structured",
	}

	return c.Execute(ctx, req)
}

// ListDeployments 列出 Deployment
func (c *GatewayClient) ListDeployments(ctx context.Context, ns string) (*KubectlResponse, error) {
	req := &KubectlRequest{
		Verb:      "get",
		Resource:  "deployments",
		Namespace: ns,
		Output:    "json",
		Mode:      "structured",
	}

	return c.Execute(ctx, req)
}

// ListNamespaces 列出命名空间
func (c *GatewayClient) ListNamespaces(ctx context.Context) (*KubectlResponse, error) {
	req := &KubectlRequest{
		Verb:     "get",
		Resource: "namespaces",
		Output:   "json",
		Mode:     "structured",
	}

	return c.Execute(ctx, req)
}

// GetNodes 获取节点列表
func (c *GatewayClient) GetNodes(ctx context.Context) (*KubectlResponse, error) {
	req := &KubectlRequest{
		Verb:     "get",
		Resource: "nodes",
		Output:   "json",
		Mode:     "structured",
	}

	return c.Execute(ctx, req)
}

// ListServices 列出服务
func (c *GatewayClient) ListServices(ctx context.Context, ns string) (*KubectlResponse, error) {
	req := &KubectlRequest{
		Verb:      "get",
		Resource:  "services",
		Namespace: ns,
		Output:    "json",
		Mode:      "structured",
	}

	return c.Execute(ctx, req)
}
