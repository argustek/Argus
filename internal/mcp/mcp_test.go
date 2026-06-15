package mcp

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToolCallError(t *testing.T) {
	err := &ToolCallError{
		ServerName: "test-server",
		ToolName:   "test-tool",
		Message:    "something went wrong",
	}
	assert.Contains(t, err.Error(), "test-server")
	assert.Contains(t, err.Error(), "test-tool")
	assert.Contains(t, err.Error(), "something went wrong")
}

func TestMCPToolInfoFullyQualifiedName(t *testing.T) {
	info := MCPToolInfo{
		ServerName:  "playwright",
		Name:        "click",
		Description: "Click an element",
	}
	assert.Equal(t, "playwright__click", info.FullyQualifiedName())
}

func TestConnectedServerGetSetTools(t *testing.T) {
	s := &ConnectedServer{
		Tools: make([]ToolInfo, 0),
	}

	tools := s.GetTools()
	assert.Empty(t, tools)

	inputTools := []ToolInfo{
		{Name: "tool1", Description: "first tool"},
		{Name: "tool2", Description: "second tool"},
	}
	s.SetTools(inputTools)

	result := s.GetTools()
	assert.Len(t, result, 2)
	assert.Equal(t, "tool1", result[0].Name)
}

func TestJSONRPCRequest(t *testing.T) {
	req := JSONRPCRequest{
		JSONRPC: "2.0",
		Method:  "initialize",
	}
	assert.Equal(t, "2.0", req.JSONRPC)
	assert.Equal(t, "initialize", req.Method)
	assert.Empty(t, req.Params)
}

func TestJSONRPCResponse(t *testing.T) {
	resp := JSONRPCResponse{
		JSONRPC: "2.0",
	}
	assert.Equal(t, "2.0", resp.JSONRPC)
	assert.Nil(t, resp.Error)
}

func TestJSONRPCError(t *testing.T) {
	err := &JSONRPCError{
		Code:    -32601,
		Message: "Method not found",
	}
	assert.Equal(t, -32601, err.Code)
	assert.Equal(t, "Method not found", err.Message)
}

func TestInitializeParams(t *testing.T) {
	params := InitializeParams{
		ProtocolVersion: "2024-11-05",
		ClientInfo: ImplementationInfo{
			Name:    "argus-desktop",
			Version: "0.7.1",
		},
	}
	assert.Equal(t, "2024-11-05", params.ProtocolVersion)
	assert.Equal(t, "argus-desktop", params.ClientInfo.Name)
}

func TestInitializeResult(t *testing.T) {
	result := InitializeResult{
		ProtocolVersion: "2024-11-05",
		ServerInfo: ImplementationInfo{
			Name:    "test-server",
			Version: "1.0.0",
		},
	}
	assert.Equal(t, "1.0.0", result.ServerInfo.Version)
}

func TestListToolsResult(t *testing.T) {
	result := ListToolsResult{
		Tools: []ToolInfo{
			{Name: "tool1"},
			{Name: "tool2"},
		},
	}
	assert.Len(t, result.Tools, 2)
}

func TestCallToolParams(t *testing.T) {
	params := CallToolParams{
		Name: "execute",
		Arguments: map[string]interface{}{
			"cmd": "go build",
		},
	}
	assert.Equal(t, "execute", params.Name)
	assert.Equal(t, "go build", params.Arguments["cmd"])
}

func TestCallToolResult(t *testing.T) {
	result := CallToolResult{
		Content: []ContentBlock{
			{Type: "text", Text: "hello"},
		},
		IsError: false,
	}
	assert.False(t, result.IsError)
	assert.Len(t, result.Content, 1)
}

func TestContentBlock(t *testing.T) {
	block := ContentBlock{
		Type: "text",
		Text: "output",
	}
	assert.Equal(t, "text", block.Type)
	assert.Equal(t, "output", block.Text)
}

func TestToolInfo(t *testing.T) {
	info := ToolInfo{
		Name:        "exec",
		Description: "Run a shell command",
		InputSchema: map[string]interface{}{
			"type": "object",
		},
	}
	assert.Equal(t, "exec", info.Name)
	assert.NotNil(t, info.InputSchema)
}

func TestServerCapabilities(t *testing.T) {
	cap := ServerCapabilities{
		Tools: ToolCapabilities{ListChanged: true},
	}
	assert.True(t, cap.Tools.ListChanged)
}

func TestErrorVariables(t *testing.T) {
	assert.Contains(t, ErrNotInitialized.Error(), "not initialized")
	assert.Contains(t, ErrServerNotFound.Error(), "not found")
	assert.Contains(t, ErrToolNotFound.Error(), "not found")
}

func TestTrimLine(t *testing.T) {
	assert.Equal(t, []byte("hello"), trimLine([]byte("hello\n")))
	assert.Equal(t, []byte("hello"), trimLine([]byte("hello\r\n")))
	assert.Equal(t, []byte(""), trimLine([]byte("\n")))
	assert.Equal(t, []byte(""), trimLine([]byte("  ")))
	assert.Equal(t, []byte("text"), trimLine([]byte("\ttext\n")))
}

func TestTrimLineBOM(t *testing.T) {
	bom := []byte{0xEF, 0xBB, 0xBF}
	input := append(bom, []byte("hello\n")...)
	assert.Equal(t, []byte("hello"), trimLine(input))
}

func TestToolNames(t *testing.T) {
	tools := []ToolInfo{
		{Name: "read"},
		{Name: "write"},
	}
	names := toolNames(tools)
	assert.Equal(t, []string{"read", "write"}, names)
}

func TestTruncate(t *testing.T) {
	assert.Equal(t, "hello", truncate("hello", 10))
	assert.Equal(t, "hel...", truncate("hello world", 3))
	assert.Equal(t, "", truncate("", 5))
}

func TestNewManager(t *testing.T) {
	m := NewManager("/test/workdir")
	require.NotNil(t, m)
	assert.Empty(t, m.ListServers())
}

func TestManagerListServers_Empty(t *testing.T) {
	m := NewManager("/tmp")
	servers := m.ListServers()
	assert.Empty(t, servers)
}

func TestManagerGetServer_NotFound(t *testing.T) {
	m := NewManager("/tmp")
	_, err := m.GetServer("nonexistent")
	assert.Equal(t, ErrServerNotFound, err)
}

func TestManagerGetAllTools_Empty(t *testing.T) {
	m := NewManager("/tmp")
	tools := m.GetAllTools()
	assert.Empty(t, tools)
}

func TestManagerCallTool_NotFound(t *testing.T) {
	m := NewManager("/tmp")
	_, err := m.CallTool("nonexistent", "tool", nil)
	assert.Equal(t, ErrServerNotFound, err)
}

func TestManagerClose_Empty(t *testing.T) {
	m := NewManager("/tmp")
	m.Close()
}

func TestMCPServerStatus(t *testing.T) {
	status := MCPServerStatus{
		Name:        "playwright",
		Description: "Browser automation",
		Command:     "npx playwright",
		ToolCount:   5,
		Initialized: true,
		Enabled:     true,
	}
	assert.Equal(t, "playwright", status.Name)
	assert.True(t, status.Initialized)
	assert.Equal(t, 5, status.ToolCount)
}

func TestCallToolErrorFormat(t *testing.T) {
	err := &ToolCallError{ServerName: "srv", ToolName: "t", Message: "failed"}
	assert.Equal(t, "[MCP:srv] tool 't' failed: failed", err.Error())
}

func TestConnectedServerConfig(t *testing.T) {
	s := &ConnectedServer{
		Config: MCPServerConfig{
			Name:    "test",
			Command: "echo",
			Args:    []string{"hello"},
		},
	}
	assert.Equal(t, "test", s.Config.Name)
	assert.Equal(t, "echo", s.Config.Command)
}
