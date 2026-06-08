package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"sync/atomic"
)

// Client MCP JSON-RPC 2.0 客户端（stdio 传输）
type Client struct {
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	stdout    io.Reader
	reader    *bufio.Reader // 用 Reader 而非 Scanner，支持 ReadBytes('\n')
	mu        sync.Mutex
	requestID atomic.Int64
	initialized bool

	// 响应通道：requestID → response channel
	pending   map[uint64]chan *JSONRPCResponse
	pendingMu sync.Mutex

	// 读取 goroutine 控制
	stopCh chan struct{}
	doneCh chan struct{}
}

// NewClient 创建 MCP Client，启动子进程并通过 stdio 通信
func NewClient(config MCPServerConfig) (*Client, error) {
	c := &Client{
		pending: make(map[uint64]chan *JSONRPCResponse),
		stopCh: make(chan struct{}),
		doneCh: make(chan struct{}),
	}

	// 构建命令
	c.cmd = exec.Command(config.Command, config.Args...)
	if len(config.Env) > 0 {
		env := append([]string{}, c.cmd.Env...) // 复制当前环境
		for k, v := range config.Env {
			env = append(env, k+"="+v)
		}
		c.cmd.Env = env
	}

	var err error
	c.stdin, err = c.cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}

	stdoutPipe, err := c.cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	c.stdout = stdoutPipe

	// stderr 合并到当前进程（方便调试）
	c.cmd.Stderr = nil // 默认继承父进程 stderr

	if err := c.cmd.Start(); err != nil {
		return nil, fmt.Errorf("start MCP server '%s': %w", config.Name, err)
	}

	// 启动响应读取 goroutine
	c.reader = bufio.NewReaderSize(c.stdout, 4*1024*1024) // 4MB buffer
	go c.readLoop()

	fmt.Printf("[MCP] ✅ Server '%s' started (PID %d): %s %v\n",
		config.Name, c.cmd.Process.Pid, config.Command, config.Args)

	return c, nil
}

// Initialize 发送 initialize 握手
func (c *Client) Initialize(serverName string) (*InitializeResult, error) {
	params := InitializeParams{
		ProtocolVersion: "2024-11-05",
		Capabilities: ClientCapabilities{},
		ClientInfo: ImplementationInfo{
			Name:    "argus-desktop",
			Version: "0.7.1",
		},
	}

	var result InitializeResult
	if err := c.call("initialize", params, &result); err != nil {
		return nil, fmt.Errorf("initialize '%s': %w", serverName, err)
	}

	// 发送 initialized 通知（无响应）
	c.notify("notifications/initialized", nil)
	c.initialized = true

	fmt.Printf("[MCP] ✅ Server '%s' initialized: protocol=%s, server=%s %s\n",
		serverName, result.ProtocolVersion,
		result.ServerInfo.Name, result.ServerInfo.Version)

	return &result, nil
}

// ListTools 获取工具列表
func (c *Client) ListTools() (*ListToolsResult, error) {
	var result ListToolsResult
	err := c.call("tools/list", map[string]any{"cursor": ""}, &result)
	return &result, err
}

// CallTool 调用工具
func (c *Client) CallTool(name string, args map[string]interface{}) (*CallToolResult, error) {
	params := CallToolParams{
		Name:      name,
		Arguments: args,
	}
	var result CallToolResult
	err := c.call("tools/call", params, &result)
	return &result, err
}

// ========== JSON-RPC 核心方法 ==========

// call 发送请求并等待响应
func (c *Client) call(method string, params any, result any) error {
	id := uint64(c.requestID.Add(1))

	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      json.RawMessage(fmt.Sprintf(`%d`, id)),
		Method:  method,
	}

	if params != nil {
		paramsBytes, err := json.Marshal(params)
		if err != nil {
			return fmt.Errorf("marshal params: %w", err)
		}
		req.Params = json.RawMessage(paramsBytes)
	}

	// 注册响应通道
	respCh := make(chan *JSONRPCResponse, 1)
	c.pendingMu.Lock()
	c.pending[id] = respCh
	c.pendingMu.Unlock()

	defer func() {
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
	}()

	// 发送请求
	if err := c.sendRequest(req); err != nil {
		return err
	}

	// 等待响应
	select {
	case resp := <-respCh:
		if resp.Error != nil {
			return fmt.Errorf("MCP RPC error [%d]: %s", resp.Error.Code, resp.Error.Message)
		}
		if result != nil && len(resp.Result) > 0 {
			if err := json.Unmarshal(resp.Result, result); err != nil {
				return fmt.Errorf("unmarshal result: %w", err)
			}
		}
		return nil
	case <-c.stopCh:
		return fmt.Errorf("MCP client stopped")
	}
}

// notify 发送通知（无需响应）
func (c *Client) notify(method string, params any) error {
	req := JSONRPCRequest{
		JSONRPC: "2.0",
		Method:  method,
	}
	if params != nil {
		paramsBytes, err := json.Marshal(params)
		if err != nil {
			return err
		}
		req.Params = json.RawMessage(paramsBytes)
	}
	return c.sendRequest(req)
}

// sendRequest 序列化并发送 JSON-RPC 请求（每行一个 JSON 对象）
func (c *Client) sendRequest(req JSONRPCRequest) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	data, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	data = append(data, '\n')
	n, err := c.stdin.Write(data)
	if err != nil {
		return fmt.Errorf("write stdin: %w", err)
	}
	if n != len(data) {
		return fmt.Errorf("short write: %d/%d", n, len(data))
	}

	return nil
}

// readLoop 持续读取 stdout 的 JSON-RPC 响应
func (c *Client) readLoop() {
	defer close(c.doneCh)

	for {
		select {
		case <-c.stopCh:
			return
		default:
		}

		line, err := c.reader.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				fmt.Printf("[MCP] 📴 Server EOF (process exited)\n")
			} else {
				fmt.Printf("[MCP] 📴 Read error: %v\n", err)
			}
			// 通知所有 pending 请求失败
			c.failAllPending(fmt.Errorf("server disconnected"))
			return
		}

		line = trimLine(line)
		if len(line) == 0 {
			continue
		}

		var resp JSONRPCResponse
		if err := json.Unmarshal(line, &resp); err != nil {
			fmt.Printf("[MCP] ⚠️ Invalid JSON: %s\n", string(line[:min(200, len(line))]))
			continue
		}

		// 通知没有 ID，忽略
		if len(resp.ID) == 0 || string(resp.ID) == "null" {
			continue
		}

		// 解析 ID
		var id uint64
		if err := json.Unmarshal(resp.ID, &id); err != nil {
			continue
		}

		// 投递到对应通道
		c.pendingMu.Lock()
		ch, ok := c.pending[id]
		c.pendingMu.Unlock()
		if ok {
			ch <- &resp
		}
	}
}

// failAllPending 所有待处理请求返回错误
func (c *Client) failAllPending(err error) {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	for id, ch := range c.pending {
		ch <- &JSONRPCResponse{Error: &JSONRPCError{
			Code: -32000, Message: err.Error(),
		}}
		delete(c.pending, id)
	}
}

// Close 关闭 MCP Client
func (c *Client) Close() error {
	close(c.stopCh)

	// 等待读循环结束
	select {
	case <-c.doneCh:
	case <-make(chan struct{}):
	}

	if c.stdin != nil {
		c.stdin.Close()
	}

	if c.cmd != nil && c.cmd.Process != nil {
		c.cmd.Process.Kill()
		c.cmd.Wait() // 回收僵尸进程
	}

	fmt.Printf("[MCP] 🔌 Client closed\n")
	return nil
}

// IsInitialized 是否已完成握手
func (c *Client) IsInitialized() bool {
	return c.initialized
}

// trimLine 去除行首尾空白和 BOM
func trimLine(b []byte) []byte {
	// 去除 BOM
	if len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF {
		b = b[3:]
	}
	// 去除 \r\n \n
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r') {
		b = b[:len(b)-1]
	}
	for len(b) > 0 && (b[0] == ' ' || b[0] == '\t') {
		b = b[1:]
	}
	return b
}
