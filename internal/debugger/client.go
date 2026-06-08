package debugger

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"

	"argus/internal/executor"
)

// DAPClient DAP 协议客户端，与 delve 通信
type DAPClient struct {
	mu           sync.RWMutex
	cmd          *exec.Cmd
	stdin        io.WriteCloser
	stdout       io.ReadCloser
	stdoutReader *bufio.Reader // 使用 Reader 以支持 ReadString 和 Read
	seq          int

	running      bool
	stopped      bool // 是否在断点处暂停

	// 事件回调
	onStopped    OnStoppedHandler
	onOutput     OnOutputHandler
	onExited     OnExitedHandler
	onError      ErrorHandler

	// 事件通道（用于前端推送）
	eventCh      chan *Event

	// 当前状态
	breakpoints  map[int]*Breakpoint // ID -> Breakpoint
	bpSeq        int                 // 断点ID自增

	// 程序信息
	program      string
	mode         string // "debug" or "test"
	workDir      string

	executorRef  *executor.Executor // 用于执行辅助命令
}

// NewDAPClient 创建 DAP 客户端
func NewDAPClient() *DAPClient {
	return &DAPClient{
		seq:         0,
		breakpoints: make(map[int]*Breakpoint),
		eventCh:     make(chan *Event, 256),
	}
}

// SetExecutor 设置 executor 引用
func (c *DAPClient) SetExecutor(ex *executor.Executor) {
	c.executorRef = ex
}

// SetEventHandlers 设置事件回调
func (c *DAPClient) SetEventHandlers(onStopped OnStoppedHandler, onOutput OnOutputHandler, onExited OnExitedHandler, onError ErrorHandler) {
	c.onStopped = onStopped
	c.onOutput = onOutput
	c.onExited = onExited
	c.onError = onError
}

// EventChannel 返回事件通道，用于 SSE 推送到前端
func (c *DAPClient) EventChannel() <-chan *Event {
	return c.eventCh
}

// IsRunning 调试会话是否运行中
func (c *DAPClient) IsRunning() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.running
}

// IsStopped 是否在断点处暂停
func (c *DAPClient) IsStopped() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.stopped
}

// ---- 核心生命周期 ----

// Launch 启动调试会话
func (c *DAPClient) Launch(program string, mode string, workDir string, args []string, stopOnEntry bool) error {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return fmt.Errorf("debug session already running")
	}
	c.program = program
	c.mode = mode
	c.workDir = workDir
	c.running = true
	c.stopped = false
	c.mu.Unlock()

	// 启动 delve DAP server（使用 stdio 模式通信）
	c.cmd = exec.Command("dlv", "dap")
	c.cmd.Dir = workDir

	var err error
	c.stdin, err = c.cmd.StdinPipe()
	if err != nil {
		c.running = false
		return fmt.Errorf("create stdin pipe: %w", err)
	}
	c.stdout, err = c.cmd.StdoutPipe()
	if err != nil {
		c.running = false
		return fmt.Errorf("create stdout pipe: %w", err)
	}
	c.cmd.Stderr = nil // delve stderr 合并到 stdout 或单独处理

	if err := c.cmd.Start(); err != nil {
		c.running = false
		return fmt.Errorf("start dlv dap: %w (is delve installed? run: go install github.com/go-delve/delve/cmd/dlv@latest)", err)
	}

	c.stdoutReader = bufio.NewReader(c.stdout)

	// 启动事件读取 goroutine
	go c.readLoop()

	// Step 1: initialize
	initResp, err := c.sendRequest("initialize", InitializeRequest{
		ClientName:    "argus-debugger",
		AdapterID:    "go",
		PathFormat:    "path",
		LinesStartAt1: true,
		ColumnsStartAt1: true,
		SupportsVariableType: true,
		SupportsVariablePaging: true,
		SupportsMemoryReferences: true,
	})
	if err != nil {
		c.Stop()
		return fmt.Errorf("initialize: %w", err)
	}
	if !initResp.Success {
		c.Stop()
		return fmt.Errorf("initialize failed: %s", initResp.Message)
	}

	// Step 2: launch
	launchArgs := LaunchArguments{
		Mode:        mode,
		Program:     program,
		NoDebug:     false,
		StopOnEntry: stopOnEntry,
		Cwd:         workDir,
		Args:        args,
	}
	launchResp, err := c.sendRequest("launch", launchArgs)
	if err != nil {
		c.Stop()
		return fmt.Errorf("launch: %w", err)
	}
	if !launchResp.Success {
		c.Stop()
		return fmt.Errorf("launch failed: %s", launchResp.Message)
	}

	// Step 3: configurationDone
	configDoneResp, err := c.sendRequest("configurationDone", nil)
	if err != nil {
		c.Stop()
		return fmt.Errorf("configurationDone: %w", err)
	}
	if !configDoneResp.Success {
		// configurationDone 失败不致命，有些 adapter 不支持
		if c.onError != nil {
			c.onError(fmt.Errorf("configurationDone warning: %s", configDoneResp.Message))
		}
	}

	return nil
}

// LaunchTest 以测试模式启动调试
func (c *DAPClient) LaunchTest(testPattern string, workDir string, stopOnEntry bool) error {
	return c.Launch(testPattern, "test", workDir, nil, stopOnEntry)
}

// Stop 停止调试会话
func (c *DAPClient) Stop() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.running {
		return nil
	}

	// 发送 disconnect
	_, _ = c.sendRequestUnsafe("disconnect", map[string]interface{}{
		"terminate": true,
	})

	c.running = false
	c.stopped = false

	// 关闭 stdin 触发进程退出
	if c.stdin != nil {
		c.stdin.Close()
	}

	// 等待进程退出
	if c.cmd != nil && c.cmd.Process != nil {
		done := make(chan error, 1)
		go func() {
			done <- c.cmd.Wait()
		}()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			c.cmd.Process.Kill()
		}
	}

	close(c.eventCh)
	c.eventCh = make(chan *Event, 256) // 重置通道
	return nil
}

// ---- 断点管理 ----

// SetBreakpoint 设置断点
func (c *DAPClient) SetBreakpoint(filePath string, line int, condition string) (*Breakpoint, error) {
	c.mu.RLock()
	if !c.running {
		c.mu.RUnlock()
		return nil, fmt.Errorf("debug session not running")
	}
	c.mu.RUnlock()

	args := SetBreakpointsArguments{
		Source: Source{Path: filePath},
		Breakpoints: []SourceBreakpoint{
			{Line: line, Condition: condition},
		},
	}

	resp, err := c.sendRequest("setBreakpoints", args)
	if err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, fmt.Errorf("setBreakpoints failed: %s", resp.Message)
	}

	var body SetBreakpointsResponseBody
	if resp.Body != nil {
		json.Unmarshal(resp.Body, &body)
	}

	if len(body.Breakpoints) > 0 {
		bp := &body.Breakpoints[0]
		c.mu.Lock()
		c.breakpoints[bp.ID] = bp
		c.mu.Unlock()
		return bp, nil
	}

	return &Breakpoint{Verified: false, Line: line}, nil
}

// RemoveBreakpoint 移除断点（通过重新设置空列表实现）
func (c *DAPClient) RemoveBreakpoint(filePath string, line int) error {
	c.mu.RLock()
	if !c.running {
		c.mu.RUnlock()
		return fmt.Errorf("debug session not running")
	}
	c.mu.RUnlock()

	args := SetBreakpointsArguments{
		Source: Source{Path: filePath},
		Breakpoints: []SourceBreakpoint{}, // 空列表清除该文件所有断点
	}

	resp, err := c.sendRequest("setBreakpoints", args)
	if err != nil {
		return err
	}
	if !resp.Success {
		return fmt.Errorf("remove breakpoint failed: %s", resp.Message)
	}

	// 清理本地缓存
	c.mu.Lock()
	for id, bp := range c.breakpoints {
		if bp.Source.Path == filePath && bp.Line == line {
			delete(c.breakpoints, id)
		}
	}
	c.mu.Unlock()

	return nil
}

// GetBreakpoints 获取当前所有断点
func (c *DAPClient) GetBreakpoints() []*Breakpoint {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := make([]*Breakpoint, 0, len(c.breakpoints))
	for _, bp := range c.breakpoints {
		result = append(result, bp)
	}
	return result
}

// ---- 执行控制 ----

// Continue 继续执行（从断点处恢复）
func (c *DAPClient) Continue(threadID int) error {
	c.mu.Lock()
	c.stopped = false
	c.mu.Unlock()

	resp, err := c.sendRequest("continue", map[string]interface{}{"threadId": threadID})
	if err != nil {
		return err
	}
	if !resp.Success {
		return fmt.Errorf("continue failed: %s", resp.Message)
	}
	return nil
}

// Next 单步跳过 (Step Over)
func (c *DAPClient) Next(threadID int) error {
	c.mu.Lock()
	c.stopped = false
	c.mu.Unlock()

	resp, err := c.sendRequest("next", map[string]interface{}{"threadId": threadID})
	if err != nil {
		return err
	}
	if !resp.Success {
		return fmt.Errorf("next failed: %s", resp.Message)
	}
	return nil
}

// StepIn 单步进入 (Step Into)
func (c *DAPClient) StepIn(threadID int) error {
	c.mu.Lock()
	c.stopped = false
	c.mu.Unlock()

	resp, err := c.sendRequest("stepIn", map[string]interface{}{"threadId": threadID})
	if err != nil {
		return err
	}
	if !resp.Success {
		return fmt.Errorf("stepIn failed: %s", resp.Message)
	}
	return nil
}

// StepOut 单步跳出 (Step Out)
func (c *DAPClient) StepOut(threadID int) error {
	c.mu.Lock()
	c.stopped = false
	c.mu.Unlock()

	resp, err := c.sendRequest("stepOut", map[string]interface{}{"threadId": threadID})
	if err != nil {
		return err
	}
	if !resp.Success {
		return fmt.Errorf("stepOut failed: %s", resp.Message)
	}
	return nil
}

// Pause 暂停执行
func (c *DAPClient) Pause(threadID int) error {
	resp, err := c.sendRequest("pause", map[string]interface{}{"threadId": threadID})
	if err != nil {
		return err
	}
	if !resp.Success {
		return fmt.Errorf("pause failed: %s", resp.Message)
	}
	return nil
}

// ---- 信息查询 ----

// Threads 获取线程列表
func (c *DAPClient) Threads() ([]Thread, error) {
	resp, err := c.sendRequest("threads", nil)
	if err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, fmt.Errorf("threads failed: %s", resp.Message)
	}

	var body struct {
		Threads []Thread `json:"threads"`
	}
	if resp.Body != nil {
		json.Unmarshal(resp.Body, &body)
	}
	return body.Threads, nil
}

// StackTrace 获取调用栈
func (c *DAPClient) StackTrace(threadID int, startFrame int, levels int) ([]StackFrame, error) {
	args := map[string]interface{}{
		"threadId":   threadID,
		"startFrame": startFrame,
		"levels":     levels,
	}
	if levels <= 0 {
		args["levels"] = 20 // 默认获取20帧
	}

	resp, err := c.sendRequest("stackTrace", args)
	if err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, fmt.Errorf("stackTrace failed: %s", resp.Message)
	}

	var body struct {
		StackFrames []StackFrame `json:"stackFrames"`
		TotalFrames int          `json:"totalFrames"`
	}
	if resp.Body != nil {
		json.Unmarshal(resp.Body, &body)
	}
	return body.StackFrames, nil
}

// Scopes 获取作用域（局部变量、全局变量等）
func (c *DAPClient) Scopes(frameID int) ([]Scope, error) {
	resp, err := c.sendRequest("scopes", map[string]interface{}{"frameId": frameID})
	if err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, fmt.Errorf("scopes failed: %s", resp.Message)
	}

	var body struct {
		Scopes []Scope `json:"scopes"`
	}
	if resp.Body != nil {
		json.Unmarshal(resp.Body, &body)
	}
	return body.Scopes, nil
}

// Variables 获取变量详情
func (c *DAPClient) Variables(variablesReference int, start int, count int) ([]Variable, error) {
	args := map[string]interface{}{
		"variablesReference": variablesReference,
		"start":              start,
		"count":              count,
	}
	if count <= 0 {
		args["count"] = 100
	}

	resp, err := c.sendRequest("variables", args)
	if err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, fmt.Errorf("variables failed: %s", resp.Message)
	}

	var body struct {
		Variables []Variable `json:"variables"`
	}
	if resp.Body != nil {
		json.Unmarshal(resp.Body, &body)
	}
	return body.Variables, nil
}

// Evaluate 计算表达式
func (c *DAPClient) Evaluate(expression string, frameID int, context string) (*Variable, error) {
	args := map[string]interface{}{
		"expression": expression,
		"frameId":    frameID,
		"context":    context, // "watch", "repl", "hover", etc.
	}

	resp, err := c.sendRequest("evaluate", args)
	if err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, fmt.Errorf("evaluate failed: %s", resp.Message)
	}

	var body Variable
	if resp.Body != nil {
		json.Unmarshal(resp.Body, &body)
	}
	return &body, nil
}

// ---- 内部通信方法 ----

// sendRequest 发送请求并等待响应（带锁）
func (c *DAPClient) sendRequest(command string, arguments interface{}) (*BaseResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sendRequestUnsafe(command, arguments)
}

// sendRequestUnsafe 不加锁的发送（内部已持有锁时调用）
func (c *DAPClient) sendRequestUnsafe(command string, arguments interface{}) (*BaseResponse, error) {
	c.seq++
	seq := c.seq

	req := BaseRequest{
		Seq:       seq,
		Type:      "request",
		Command:   command,
		Arguments: arguments,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	// DAP 协议格式：Content-Length: {length}\r\n\r\n{json}
	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(data))
	if _, err := c.stdin.Write([]byte(header)); err != nil {
		return nil, fmt.Errorf("write header: %w", err)
	}
	if _, err := c.stdin.Write(data); err != nil {
		return nil, fmt.Errorf("write body: %w", err)
	}

	// 读取响应（带超时）
	return c.readResponse(seq, 30*time.Second)
}

// readResponse 读取指定 seq 的响应
func (c *DAPClient) readResponse(expectedSeq int, timeout time.Duration) (*BaseResponse, error) {
	deadline := time.After(timeout)
	for {
		select {
		case <-deadline:
			return nil, fmt.Errorf("timeout waiting for response to seq=%d", expectedSeq)
		default:
			line, err := c.stdoutReader.ReadString('\n')
			if err != nil {
				if err == io.EOF {
					return nil, fmt.Errorf("dlv process ended (EOF)")
				}
				continue
			}

			// 解析 Content-Length header
			var contentLength int
			if n, _ := fmt.Sscanf(line, "Content-Length: %d", &contentLength); n == 1 && contentLength > 0 {
				// 读取空行
				c.stdoutReader.ReadString('\n')

				// 读取 JSON body
				body := make([]byte, contentLength)
				io.ReadFull(c.stdoutReader, body)

				// 判断是 response 还是 event
				var msgType struct {
					Type string `json:"type"`
					Seq  int    `json:"seq"`
				}
				json.Unmarshal(body, &msgType)

				if msgType.Type == "response" {
					var resp BaseResponse
					json.Unmarshal(body, &resp)
					if resp.Seq == expectedSeq {
						return &resp, nil
					}
					// 不是我们要的响应，缓存或忽略（理论上不应该发生）
				} else if msgType.Type == "event" {
					var evt Event
					json.Unmarshal(body, &evt)
					c.handleEvent(&evt)
				}
			}
		}
	}
}

// readLoop 持续读取事件（在独立 goroutine 中运行）
func (c *DAPClient) readLoop() {
	for c.IsRunning() {
		line, err := c.stdoutReader.ReadString('\n')
		if err != nil {
			if c.onError != nil {
				c.onError(fmt.Errorf("read loop error: %w", err))
			}
			return
		}

		var contentLength int
		if n, _ := fmt.Sscanf(line, "Content-Length: %d", &contentLength); n == 1 && contentLength > 0 {
			c.stdoutReader.ReadString('\n') // 空行

			body := make([]byte, contentLength)
			_, err := io.ReadFull(c.stdoutReader, body)
			if err != nil {
				continue
			}

			var msgType struct {
				Type string `json:"type"`
			}
			json.Unmarshal(body, &msgType)

			if msgType.Type == "event" {
				var evt Event
				json.Unmarshal(body, &evt)
				c.handleEvent(&evt)

				// 推送到事件通道
				select {
				case c.eventCh <- &evt:
				default:
					// 通道满则丢弃旧事件
				}
			}
		}
	}
}

// handleEvent 处理 DAP 事件
func (c *DAPClient) handleEvent(evt *Event) {
	switch evt.Event {
	case "stopped":
		c.mu.Lock()
		c.stopped = true
		c.mu.Unlock()

		var body StoppedEventBody
		if evt.Body != nil {
			json.Unmarshal(evt.Body, &body)
		}

		if c.onStopped != nil {
			c.onStopped(body.Reason, body.ThreadID)
		}

	case "continued":
		c.mu.Lock()
		c.stopped = false
		c.mu.Unlock()

	case "output":
		var body OutputEventBody
		if evt.Body != nil {
			json.Unmarshal(evt.Body, &body)
		}
		if c.onOutput != nil {
			c.onOutput(body.Output, body.Category)
		}

	case "exited":
		var body struct {
			ExitCode int `json:"exitCode"`
		}
		if evt.Body != nil {
			json.Unmarshal(evt.Body, &body)
		}
		if c.onExited != nil {
			c.onExited(body.ExitCode)
		}

	case "terminated":
		c.mu.Lock()
		c.running = false
		c.stopped = false
		c.mu.Unlock()

	case "thread":
		// 线程创建/退出事件，通常不需要处理

	default:
		// 其他事件：breakpoint, module, process 等
	}
}

// ---- 辅助方法 ----

// CurrentState 返回当前调试状态的摘要
func (c *DAPClient) CurrentState() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	state := map[string]interface{}{
		"running":   c.running,
		"stopped":   c.stopped,
		"program":   c.program,
		"mode":      c.mode,
		"workDir":   c.workDir,
		"breakpointCount": len(c.breakpoints),
	}

	if c.running {
		threads, _ := c.ThreadsUnsafe()
		state["threads"] = threads
	}
	return state
}

// ThreadsUnsafe 不加锁获取线程（内部调用）
func (c *DAPClient) ThreadsUnsafe() ([]Thread, error) {
	resp, err := c.sendRequestUnsafe("threads", nil)
	if err != nil {
		return nil, err
	}
	var body struct {
		Threads []Thread `json:"threads"`
	}
	if resp.Body != nil {
		json.Unmarshal(resp.Body, &body)
	}
	return body.Threads, nil
}
