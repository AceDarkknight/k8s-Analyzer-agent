package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/AceDarkknight/k8s-analyzer-agent/internal/logger"
)

func init() {
	// 初始化 logger 用于测试
	_ = logger.Init(nil)
}

func TestNewGatewayClient(t *testing.T) {
	tests := []struct {
		name           string
		baseURL        string
		authToken      string
		timeoutSeconds int
		wantErr        bool
	}{
		{
			name:           "valid client",
			baseURL:        "http://localhost:8080",
			authToken:      "test-token",
			timeoutSeconds: 30,
			wantErr:        false,
		},
		{
			name:           "empty baseURL",
			baseURL:        "",
			authToken:      "test-token",
			timeoutSeconds: 30,
			wantErr:        true,
		},
		{
			name:           "zero timeout uses default",
			baseURL:        "http://localhost:8080",
			authToken:      "test-token",
			timeoutSeconds: 0,
			wantErr:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewGatewayClient(tt.baseURL, tt.authToken, tt.timeoutSeconds)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewGatewayClient() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && client == nil {
				t.Error("NewGatewayClient() returned nil client without error")
			}
		})
	}
}

func TestGatewayClient_Execute(t *testing.T) {
	// 创建 mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 验证请求路径
		if r.URL.Path != "/execute" {
			t.Errorf("expected path /execute, got %s", r.URL.Path)
		}

		// 验证请求方法
		if r.Method != http.MethodPost {
			t.Errorf("expected POST method, got %s", r.Method)
		}

		// 验证 Content-Type
		contentType := r.Header.Get("Content-Type")
		if contentType != "application/json" {
			t.Errorf("expected Content-Type application/json, got %s", contentType)
		}

		// 验证 Authorization Header
		authHeader := r.Header.Get("Authorization")
		if !strings.HasPrefix(authHeader, "Bearer ") {
			t.Errorf("expected Authorization header to start with 'Bearer ', got %s", authHeader)
		}

		// 解析请求体
		var req KubectlRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request body: %v", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		// 返回成功响应
		resp := KubectlResponse{
			RequestID:         "test-request-id",
			Status:            "success",
			ExitCode:          0,
			Stdout:            `{"items":[]}`,
			Stderr:            "",
			Truncated:         false,
			DurationMs:        100,
			ResponseSizeBytes: 12,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, err := NewGatewayClient(server.URL, "test-token", 30)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	req := &KubectlRequest{
		Verb:      "get",
		Resource:  "pods",
		Namespace: "default",
		Output:    "json",
		Mode:      "structured",
	}

	resp, err := client.Execute(context.Background(), req)
	if err != nil {
		t.Errorf("Execute() unexpected error = %v", err)
		return
	}

	if resp.Status != "success" {
		t.Errorf("expected status success, got %s", resp.Status)
	}
	if resp.RequestID != "test-request-id" {
		t.Errorf("expected request_id test-request-id, got %s", resp.RequestID)
	}
}

func TestGatewayClient_Execute_Error(t *testing.T) {
	// 创建返回错误响应的 mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := KubectlResponse{
			RequestID:     "test-request-id",
			Status:        "error",
			ExitCode:      1,
			Stderr:        "Error from server (NotFound): pods \"nonexistent\" not found",
			DurationMs:    50,
			BlockedReason: "",
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, err := NewGatewayClient(server.URL, "test-token", 30)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	req := &KubectlRequest{
		Verb:      "get",
		Resource:  "pod",
		Namespace: "default",
		Name:      "nonexistent",
		Mode:      "structured",
	}

	resp, err := client.Execute(context.Background(), req)
	if err == nil {
		t.Error("Execute() expected error for error response, got nil")
		return
	}

	if resp == nil {
		t.Error("Execute() should return response even on error")
		return
	}

	if resp.Status != "error" {
		t.Errorf("expected status error, got %s", resp.Status)
	}
}

func TestGatewayClient_Execute_Blocked(t *testing.T) {
	// 创建返回被阻止响应的 mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := KubectlResponse{
			RequestID:     "test-request-id",
			Status:        "error",
			ExitCode:      1,
			Stderr:        "",
			DurationMs:    10,
			BlockedReason: "command not allowed: delete",
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, err := NewGatewayClient(server.URL, "test-token", 30)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	req := &KubectlRequest{
		Verb:      "delete",
		Resource:  "pod",
		Namespace: "default",
		Name:      "test-pod",
		Mode:      "structured",
	}

	resp, err := client.Execute(context.Background(), req)
	if err == nil {
		t.Error("Execute() expected error for blocked response, got nil")
		return
	}

	if resp == nil {
		t.Error("Execute() should return response even when blocked")
		return
	}

	if !strings.Contains(err.Error(), "blocked_reason") {
		t.Errorf("expected error to contain blocked_reason, got: %s", err.Error())
	}
}

func TestGatewayClient_ListPods(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req KubectlRequest
		json.NewDecoder(r.Body).Decode(&req)

		// 验证 ListPods 构造的请求
		if req.Verb != "get" {
			t.Errorf("expected verb get, got %s", req.Verb)
		}
		if req.Resource != "pods" {
			t.Errorf("expected resource pods, got %s", req.Resource)
		}
		if req.Output != "json" {
			t.Errorf("expected output json, got %s", req.Output)
		}
		if req.Mode != "structured" {
			t.Errorf("expected mode structured, got %s", req.Mode)
		}

		resp := KubectlResponse{
			RequestID: "test-id",
			Status:    "success",
			ExitCode:  0,
			Stdout:    `{"items":[{"metadata":{"name":"pod1"}}]}`,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, _ := NewGatewayClient(server.URL, "test-token", 30)

	// 测试不带 label selector
	resp, err := client.ListPods(context.Background(), "default", "")
	if err != nil {
		t.Errorf("ListPods() unexpected error = %v", err)
	}
	if resp.Status != "success" {
		t.Errorf("expected status success, got %s", resp.Status)
	}

	// 测试带 label selector
	server2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req KubectlRequest
		json.NewDecoder(r.Body).Decode(&req)

		if req.Options == nil || req.Options.LabelSelector != "app=test" {
			t.Errorf("expected labelSelector app=test, got %v", req.Options)
		}

		resp := KubectlResponse{
			RequestID: "test-id",
			Status:    "success",
			ExitCode:  0,
			Stdout:    `{"items":[]}`,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server2.Close()

	client2, _ := NewGatewayClient(server2.URL, "test-token", 30)
	resp, err = client2.ListPods(context.Background(), "default", "app=test")
	if err != nil {
		t.Errorf("ListPods() with labelSelector unexpected error = %v", err)
	}
	if resp.Status != "success" {
		t.Errorf("expected status success, got %s", resp.Status)
	}
}

func TestGatewayClient_AuthHeader(t *testing.T) {
	expectedToken := "my-secret-token"
	var receivedAuth string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")

		resp := KubectlResponse{
			RequestID: "test-id",
			Status:    "success",
			ExitCode:  0,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, _ := NewGatewayClient(server.URL, expectedToken, 30)
	client.Execute(context.Background(), &KubectlRequest{
		Verb:     "get",
		Resource: "pods",
		Mode:     "structured",
	})

	expectedAuth := "Bearer " + expectedToken
	if receivedAuth != expectedAuth {
		t.Errorf("expected Authorization header %s, got %s", expectedAuth, receivedAuth)
	}
}

func TestGatewayClient_Timeout(t *testing.T) {
	// 创建延迟响应的 server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		resp := KubectlResponse{
			RequestID: "test-id",
			Status:    "success",
			ExitCode:  0,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// 使用 50ms 超时创建客户端
	client, _ := NewGatewayClient(server.URL, "test-token", 0)
	client.httpClient.Timeout = 50 * time.Millisecond

	ctx := context.Background()
	_, err := client.Execute(ctx, &KubectlRequest{
		Verb:     "get",
		Resource: "pods",
		Mode:     "structured",
	})

	if err == nil {
		t.Error("expected timeout error, got nil")
	}

	if !strings.Contains(err.Error(), "Client.Timeout") && !strings.Contains(err.Error(), "context deadline exceeded") {
		t.Errorf("expected timeout error, got: %v", err)
	}
}

func TestGatewayClient_ConvenienceMethods(t *testing.T) {
	tests := []struct {
		name         string
		method       func(*GatewayClient, context.Context) (*KubectlResponse, error)
		expectedVerb string
		expectedRes  string
		expectedOut  string
	}{
		{
			name: "DescribePod",
			method: func(c *GatewayClient, ctx context.Context) (*KubectlResponse, error) {
				return c.DescribePod(ctx, "default", "test-pod")
			},
			expectedVerb: "describe",
			expectedRes:  "pod",
			expectedOut:  "",
		},
		{
			name: "ListEvents",
			method: func(c *GatewayClient, ctx context.Context) (*KubectlResponse, error) {
				return c.ListEvents(ctx, "default")
			},
			expectedVerb: "get",
			expectedRes:  "events",
			expectedOut:  "json",
		},
		{
			name: "ListDeployments",
			method: func(c *GatewayClient, ctx context.Context) (*KubectlResponse, error) {
				return c.ListDeployments(ctx, "default")
			},
			expectedVerb: "get",
			expectedRes:  "deployments",
			expectedOut:  "json",
		},
		{
			name: "ListNamespaces",
			method: func(c *GatewayClient, ctx context.Context) (*KubectlResponse, error) {
				return c.ListNamespaces(ctx)
			},
			expectedVerb: "get",
			expectedRes:  "namespaces",
			expectedOut:  "json",
		},
		{
			name: "GetNodes",
			method: func(c *GatewayClient, ctx context.Context) (*KubectlResponse, error) {
				return c.GetNodes(ctx)
			},
			expectedVerb: "get",
			expectedRes:  "nodes",
			expectedOut:  "json",
		},
		{
			name: "ListServices",
			method: func(c *GatewayClient, ctx context.Context) (*KubectlResponse, error) {
				return c.ListServices(ctx, "default")
			},
			expectedVerb: "get",
			expectedRes:  "services",
			expectedOut:  "json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedReq KubectlRequest

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				json.NewDecoder(r.Body).Decode(&capturedReq)

				resp := KubectlResponse{
					RequestID: "test-id",
					Status:    "success",
					ExitCode:  0,
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(resp)
			}))
			defer server.Close()

			client, _ := NewGatewayClient(server.URL, "test-token", 30)
			_, err := tt.method(client, context.Background())
			if err != nil {
				t.Errorf("%s() unexpected error = %v", tt.name, err)
				return
			}

			if capturedReq.Verb != tt.expectedVerb {
				t.Errorf("expected verb %s, got %s", tt.expectedVerb, capturedReq.Verb)
			}
			if capturedReq.Resource != tt.expectedRes {
				t.Errorf("expected resource %s, got %s", tt.expectedRes, capturedReq.Resource)
			}
			if capturedReq.Output != tt.expectedOut {
				t.Errorf("expected output %s, got %s", tt.expectedOut, capturedReq.Output)
			}
			if capturedReq.Mode != "structured" {
				t.Errorf("expected mode structured, got %s", capturedReq.Mode)
			}
		})
	}
}

func TestGatewayClient_GetLogs(t *testing.T) {
	tests := []struct {
		name          string
		container     string
		tailLines     int
		expectOpts    bool
		expectCont    string
		expectTail    int
	}{
		{
			name:       "no options",
			container:  "",
			tailLines:  0,
			expectOpts: false,
		},
		{
			name:       "with container",
			container:  "main",
			tailLines:  0,
			expectOpts: true,
			expectCont: "main",
			expectTail: 0,
		},
		{
			name:       "with tailLines",
			container:  "",
			tailLines:  100,
			expectOpts: true,
			expectCont: "",
			expectTail: 100,
		},
		{
			name:       "with both options",
			container:  "app",
			tailLines:  50,
			expectOpts: true,
			expectCont: "app",
			expectTail: 50,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedReq KubectlRequest

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				json.NewDecoder(r.Body).Decode(&capturedReq)

				resp := KubectlResponse{
					RequestID: "test-id",
					Status:    "success",
					ExitCode:  0,
					Stdout:    "log line 1\nlog line 2",
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(resp)
			}))
			defer server.Close()

			client, _ := NewGatewayClient(server.URL, "test-token", 30)
			_, err := client.GetLogs(context.Background(), "default", "test-pod", tt.container, tt.tailLines)
			if err != nil {
				t.Errorf("GetLogs() unexpected error = %v", err)
				return
			}

			if capturedReq.Verb != "logs" {
				t.Errorf("expected verb logs, got %s", capturedReq.Verb)
			}
			if capturedReq.Resource != "pod" {
				t.Errorf("expected resource pod, got %s", capturedReq.Resource)
			}

			if tt.expectOpts {
				if capturedReq.Options == nil {
					t.Error("expected Options to be set")
				} else {
					if capturedReq.Options.Container != tt.expectCont {
						t.Errorf("expected container %s, got %s", tt.expectCont, capturedReq.Options.Container)
					}
					if capturedReq.Options.TailLines != tt.expectTail {
						t.Errorf("expected tailLines %d, got %d", tt.expectTail, capturedReq.Options.TailLines)
					}
				}
			} else {
				// 当没有选项时，仍然创建了空的 Options
				if capturedReq.Options == nil {
					t.Error("expected Options to be created even when empty")
				}
			}
		})
	}
}

func TestGatewayClient_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		resp := KubectlResponse{
			RequestID: "test-id",
			Status:    "success",
			ExitCode:  0,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, _ := NewGatewayClient(server.URL, "test-token", 30)

	// 创建一个会被立即取消的 context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消

	_, err := client.Execute(ctx, &KubectlRequest{
		Verb:     "get",
		Resource: "pods",
		Mode:     "structured",
	})

	if err == nil {
		t.Error("expected error for cancelled context, got nil")
	}
}

func TestGatewayClient_InvalidJSONResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("invalid json"))
	}))
	defer server.Close()

	client, _ := NewGatewayClient(server.URL, "test-token", 30)

	_, err := client.Execute(context.Background(), &KubectlRequest{
		Verb:     "get",
		Resource: "pods",
		Mode:     "structured",
	})

	if err == nil {
		t.Error("expected error for invalid JSON response, got nil")
	}
}

func ExampleGatewayClient_ListPods() {
	// 创建 mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := KubectlResponse{
			RequestID: "req-123",
			Status:    "success",
			ExitCode:  0,
			Stdout:    `{"items":[{"metadata":{"name":"nginx-pod"}}]}`,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, _ := NewGatewayClient(server.URL, "token", 30)
	resp, err := client.ListPods(context.Background(), "default", "")
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	fmt.Printf("Status: %s\n", resp.Status)
	fmt.Printf("RequestID: %s\n", resp.RequestID)
	// Output:
	// Status: success
	// RequestID: req-123
}
