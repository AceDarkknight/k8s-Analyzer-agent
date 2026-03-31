package shellmcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewShellMCPClient(t *testing.T) {
	client := NewShellMCPClient("http://localhost:8080/sse", "test-token")

	assert.NotNil(t, client)
	assert.Equal(t, "http://localhost:8080/sse", client.serverURL)
	assert.Equal(t, "test-token", client.authToken)
	assert.False(t, client.connected)
	assert.False(t, client.IsConnected())
}

func TestShellMCPClient_ExecuteCommand_NotConnected(t *testing.T) {
	client := NewShellMCPClient("http://localhost:8080/sse", "test-token")

	ctx := context.Background()
	result, err := client.ExecuteCommand(ctx, "ls -la")

	assert.Nil(t, result)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not connected")
}

func TestShellMCPClient_ListTools_NotConnected(t *testing.T) {
	client := NewShellMCPClient("http://localhost:8080/sse", "test-token")

	ctx := context.Background()
	tools, err := client.ListTools(ctx)

	assert.Nil(t, tools)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not connected")
}

func TestShellMCPClient_IsConnected(t *testing.T) {
	client := NewShellMCPClient("http://localhost:8080/sse", "test-token")

	// 初始状态应该未连接
	assert.False(t, client.IsConnected())
}

func TestShellMCPClient_Close_NotConnected(t *testing.T) {
	client := NewShellMCPClient("http://localhost:8080/sse", "test-token")

	// 关闭未连接的客户端应该不返回错误
	err := client.Close()
	assert.NoError(t, err)
}
