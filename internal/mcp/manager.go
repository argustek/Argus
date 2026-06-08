package mcp

import (
	"fmt"
	"sync"
)

// Manager 管理多个 MCP Server 连接
type Manager struct {
	mu      sync.RWMutex
	servers map[string]*ConnectedServer // name → ConnectedServer
	workDir string
}

// NewManager 创建 MCP Manager
func NewManager(workDir string) *Manager {
	return &Manager{
		servers: make(map[string]*ConnectedServer),
		workDir: workDir,
	}
}

// AddServer 添加并启动一个 MCP Server
func (m *Manager) AddServer(config MCPServerConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.servers[config.Name]; exists {
		return fmt.Errorf("MCP server '%s' already exists", config.Name)
	}

	client, err := NewClient(config)
	if err != nil {
		return err
	}

	conn := &ConnectedServer{
		Config: config,
		Client: client,
		Tools:  make([]ToolInfo, 0),
	}
	m.servers[config.Name] = conn

	// 初始化握手
	initResult, err := client.Initialize(config.Name)
	if err != nil {
		// 清理
		delete(m.servers, config.Name)
		client.Close()
		return fmt.Errorf("initialize server '%s': %w", config.Name, err)
	}

	// 发现工具
	tools, err := client.ListTools()
	if err != nil {
		fmt.Printf("[MCP] ⚠️ Server '%s' tools/list failed (server running): %v\n", config.Name, err)
	} else {
		conn.SetTools(tools.Tools)
		fmt.Printf("[MCP] ✅ Server '%s' discovered %d tools: %v\n",
			config.Name, len(tools.Tools), toolNames(tools.Tools))
	}

	// 打印服务端说明（如果有）
	if initResult.Instructions != "" {
		fmt.Printf("[MCP] 📋 Server '%s' instructions: %s\n", config.Name, truncate(initResult.Instructions, 200))
	}

	return nil
}

// RemoveServer 移除并关闭一个 MCP Server
func (m *Manager) RemoveServer(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	conn, ok := m.servers[name]
	if !ok {
		return ErrServerNotFound
	}

	if conn.Client != nil {
		conn.Client.Close()
	}
	delete(m.servers, name)

	fmt.Printf("[MCP] 🔌 Server '%s' removed\n", name)
	return nil
}

// GetServer 获取已连接的 Server
func (m *Manager) GetServer(name string) (*ConnectedServer, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	conn, ok := m.servers[name]
	if !ok {
		return nil, ErrServerNotFound
	}
	return conn, nil
}

// ListServers 列出所有已连接的 Server 及其工具数
func (m *Manager) ListServers() []MCPServerStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	statuses := make([]MCPServerStatus, 0, len(m.servers))
	for name, conn := range m.servers {
		statuses = append(statuses, MCPServerStatus{
			Name:        name,
			Description: conn.Config.Description,
			Command:     fmt.Sprintf("%s %v", conn.Config.Command, conn.Config.Args),
			ToolCount:   len(conn.GetTools()),
			Initialized: conn.Client.IsInitialized(),
			Enabled:     conn.Config.Enabled,
		})
	}
	return statuses
}

// GetAllTools 获取所有 Server 的所有工具（扁平化）
func (m *Manager) GetAllTools() []MCPToolInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var all []MCPToolInfo
	for serverName, conn := range m.servers {
		for _, tool := range conn.GetTools() {
			all = append(all, MCPToolInfo{
				ServerName:  serverName,
				Name:        tool.Name,
				Description: tool.Description,
				InputSchema: tool.InputSchema,
			})
		}
	}
	return all
}

// CallTool 调用指定 Server 的工具
func (m *Manager) CallTool(serverName, toolName string, args map[string]interface{}) (*CallToolResult, error) {
	conn, err := m.GetServer(serverName)
	if err != nil {
		return nil, err
	}
	if !conn.Client.IsInitialized() {
		return nil, ErrNotInitialized
	}

	result, err := conn.Client.CallTool(toolName, args)
	if err != nil {
		return nil, &ToolCallError{
			ServerName: serverName,
			ToolName:   toolName,
			Message:    err.Error(),
		}
	}

	return result, nil
}

// RefreshTools 重新发现某个 Server 的工具列表
func (m *Manager) RefreshTools(serverName string) ([]ToolInfo, error) {
	conn, err := m.GetServer(serverName)
	if err != nil {
		return nil, err
	}

	tools, err := conn.Client.ListTools()
	if err != nil {
		return nil, err
	}
	conn.SetTools(tools.Tools)
	return tools.Tools, nil
}

// Close 关闭所有 Server 连接
func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for name, conn := range m.servers {
		if conn.Client != nil {
			conn.Client.Close()
		}
		delete(m.servers, name)
	}
	fmt.Printf("[MCP] 🔌 All servers closed\n")
}

// ========== 辅助类型 ==========

// MCPServerStatus Server 状态（用于 API 返回）
type MCPServerStatus struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Command     string `json:"command"`
	ToolCount   int    `json:"tool_count"`
	Initialized bool   `json:"initialized"`
	Enabled     bool   `json:"enabled"`
}

// MCPToolInfo 工具信息（带 Server 前缀，用于 SE 注册）
type MCPToolInfo struct {
	ServerName  string                 `json:"server_name"`
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	InputSchema map[string]interface{} `json:"input_schema"`
}

// FullyQualifiedName 完整工具名：server_name__tool_name
func (t MCPToolInfo) FullyQualifiedName() string {
	return t.ServerName + "__" + t.Name
}

// ========== 辅助函数 ==========

func toolNames(tools []ToolInfo) []string {
	names := make([]string, len(tools))
	for i, t := range tools {
		names[i] = t.Name
	}
	return names
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
