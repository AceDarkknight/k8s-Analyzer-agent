package shellmcp

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/AceDarkknight/k8s-analyzer-agent/internal/logger"
	"github.com/stretchr/testify/assert"
)

func TestMain(m *testing.M) {
	_ = logger.Init(nil)
	os.Exit(m.Run())
}

func TestNewShellMCPClient(t *testing.T) {
	client := NewShellMCPClient("http://localhost:8080/sse", "test-token", 30)

	assert.NotNil(t, client)
	assert.Equal(t, "http://localhost:8080/sse", client.serverURL)
	assert.Equal(t, "test-token", client.authToken)
	assert.False(t, client.connected)
	assert.False(t, client.IsConnected())
}

func TestShellMCPClient_ExecuteCommand_NotConnected(t *testing.T) {
	client := NewShellMCPClient("http://localhost:8080/sse", "test-token", 30)

	ctx := context.Background()
	result, err := client.ExecuteCommand(ctx, "ls -la")

	assert.Nil(t, result)
	assert.Error(t, err)
	// 懒连接模式下会自动尝试连接，连接失败会返回 lazy connect failed
	assert.Contains(t, err.Error(), "lazy connect failed")
}

func TestIsConnectionError(t *testing.T) {
	assert.True(t, isConnectionError(fmt.Errorf("read tcp: wsarecv: A connection attempt failed")))
	assert.True(t, isConnectionError(fmt.Errorf("EOF")))
	assert.True(t, isConnectionError(fmt.Errorf("connection reset by peer")))
	assert.True(t, isConnectionError(fmt.Errorf("i/o timeout")))
	assert.True(t, isConnectionError(fmt.Errorf("failed to respond")))
	assert.False(t, isConnectionError(fmt.Errorf("command not found")))
	assert.False(t, isConnectionError(nil))
}

func TestShellMCPClient_ListTools_NotConnected(t *testing.T) {
	client := NewShellMCPClient("http://localhost:8080/sse", "test-token", 30)

	ctx := context.Background()
	tools, err := client.ListTools(ctx)

	assert.NoError(t, err)
	assert.Len(t, tools, 1)
	assert.Equal(t, "execute_command", tools[0].Name)
	assert.Contains(t, tools[0].Description, "Shell 命令")
}

func TestShellMCPClient_IsConnected(t *testing.T) {
	client := NewShellMCPClient("http://localhost:8080/sse", "test-token", 30)

	// 初始状态应该未连接
	assert.False(t, client.IsConnected())
}

func TestShellMCPClient_Close_NotConnected(t *testing.T) {
	client := NewShellMCPClient("http://localhost:8080/sse", "test-token", 30)

	// 关闭未连接的客户端应该不返回错误
	err := client.Close()
	assert.NoError(t, err)
}
