package mcp

import (
	"encoding/json"
	"fmt"
	"sync"

	"argus/internal/types"
)

// ========== MCP Protocol Types (Model Context Protocol 2024-11-05) ==========

// JSONRPCRequest JSON-RPC 2.0 请求
type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// JSONRPCResponse JSON-RPC 2.0 响应
type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

// JSONRPCError JSON-RPC 错误
type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// InitializeParams 初始化参数
type InitializeParams struct {
	ProtocolVersion string           `json:"protocolVersion"`
	Capabilities    ClientCapabilities `json:"capabilities"`
	ClientInfo      ImplementationInfo `json:"clientInfo"`
}

// ClientCapabilities 客户端能力
type ClientCapabilities struct {
	Roots map[string]any `json:"roots,omitempty"`
}

// ImplementationInfo 实现信息
type ImplementationInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// InitializeResult 初始化结果
type InitializeResult struct {
	ProtocolVersion     string             `json:"protocolVersion"`
	Capabilities        ServerCapabilities  `json:"capabilities"`
	ServerInfo          ImplementationInfo  `json:"serverInfo"`
	Instructions       string              `json:"instructions,omitempty"`
}

// ServerCapabilities 服务端能力
type ServerCapabilities struct {
	Logging any                        `json:"logging,omitempty"`
	Prompting any                      `json:"prompting,omitempty"`
	Resources ResourceCapabilities      `json:"resources,omitempty"`
	Tools ToolCapabilities             `json:"tools,omitempty"`
}

// ResourceCapabilities 资源能力
type ResourceCapabilities struct {
	Subscribe bool `json:"subscribe,omitempty"`
	ListChanged bool `json:"listChanged,omitempty"`
}

// ToolCapabilities 工具能力
type ToolCapabilities struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

// ========== Tool 相关类型 ==========

// ListToolsResult 工具列表结果
type ListToolsResult struct {
	Tools []ToolInfo `json:"tools"`
	NextCursor string `json:"nextCursor,omitempty"`
}

// ToolInfo 工具信息
type ToolInfo struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	InputSchema map[string]interface{} `json:"inputSchema"`
}

// CallToolParams 调用工具参数
type CallToolParams struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments,omitempty"`
}

// CallToolResult 工具调用结果
type CallToolResult struct {
	Content []ContentBlock `json:"content"`
	IsError bool           `json:"isError,omitempty"`
}

// ContentBlock 内容块
type ContentBlock struct {
	Type string `json:"type"` // "text" | "image" | "resource"
	Text string `json:"text,omitempty"`
	// Image fields omitted for now
	// Resource fields omitted for now
}

// ========== Server 配置 ==========

// MCPServerConfig 复用 types 包定义（YAML 反序列化用）
type MCPServerConfig = types.MCPServerConfig

// ConnectedServer 已连接的 MCP Server 运行时状态
type ConnectedServer struct {
	Config  MCPServerConfig
	Client  *Client
	Tools   []ToolInfo
	mu      sync.RWMutex
}

// GetTools 线程安全获取工具列表
func (s *ConnectedServer) GetTools() []ToolInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Tools
}

// SetTools 线程安全设置工具列表
func (s *ConnectedServer) SetTools(tools []ToolInfo) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Tools = tools
}

// ========== 错误定义 ==========

var (
	ErrNotInitialized = fmt.Errorf("MCP server not initialized")
	ErrServerNotFound = fmt.Errorf("MCP server not found")
	ErrToolNotFound   = fmt.Errorf("MCP tool not found")
)

// ToolCallError 工具调用错误（包含服务端错误信息）
type ToolCallError struct {
	ServerName string
	ToolName   string
	Message    string
}

func (e *ToolCallError) Error() string {
	return fmt.Sprintf("[MCP:%s] tool '%s' failed: %s", e.ServerName, e.ToolName, e.Message)
}
