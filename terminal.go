package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

// TerminalSession 单个终端会话
type TerminalSession struct {
	ID         string
	Name       string
	cmd        *exec.Cmd
	stdin      *bufio.Writer
	stdout     *bufio.Scanner
	workingDir string
	shellType  string // "powershell" | "cmd" | "bash"
	encoding   string // "gbk" | "utf-8" | ...
	running    atomic.Bool
	busy       atomic.Bool
	history    []string
	mu         sync.Mutex
	manager    *TerminalManager
}

// TerminalManager 终端管理器（支持多实例）
type TerminalManager struct {
	sessions  map[string]*TerminalSession
	activeID  string
	sessionMu sync.RWMutex
	app       *App
	counter   int64
}

// NewTerminalManager 创建终端管理器
func NewTerminalManager(app *App) *TerminalManager {
	return &TerminalManager{
		sessions: make(map[string]*TerminalSession),
		app:      app,
	}
}

// StartTerminal 启动默认终端会话
func (tm *TerminalManager) StartTerminal(workingDir string) error {
	return tm.NewSession("Terminal 1", workingDir)
}

// NewSession 创建新的终端会话（支持多Tab）
func (tm *TerminalManager) NewSession(name, workingDir string) error {
	tm.sessionMu.Lock()
	defer tm.sessionMu.Unlock()

	if workingDir == "" {
		workingDir = tm.GetProjectDir()
	}

	atomic.AddInt64(&tm.counter, 1)
	sessionID := fmt.Sprintf("term-%d", tm.counter)

	session := &TerminalSession{
		ID:         sessionID,
		Name:       name,
		workingDir: workingDir,
		shellType:  detectShell(),
		encoding:   "utf-8",
		history:    make([]string, 0, 500),
		manager:    tm,
	}
	session.running.Store(false)
	session.busy.Store(false)

	err := session.start()
	if err != nil {
		return err
	}

	tm.sessions[sessionID] = session
	tm.activeID = sessionID

	tm.app.addLog(fmt.Sprintf("[终端] 新会话启动: %s (%s)", name, sessionID))
	runtime.EventsEmit(tm.app.ctx, "terminal:session-created", map[string]interface{}{
		"id":   sessionID,
		"name": name,
	})

	return nil
}

// start 启动真实Shell进程
func (s *TerminalSession) start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var cmd *exec.Cmd

	switch s.shellType {
	case "powershell":
		cmd = exec.Command("powershell", "-NoProfile", "-NoExit", "-Command",
			fmt.Sprintf("function prompt {'PS ' + (Get-Location).Path + '> '}; chcp 65001 | Out-Null; $OutputEncoding = [System.Text.Encoding]::UTF8; [Console]::OutputEncoding = [System.Text.Encoding]::UTF8; Set-Location '%s'", s.workingDir))
	case "cmd":
		cmd = exec.Command("cmd.exe", "/K", fmt.Sprintf("chcp 65001 && cd /d %s && title Argus Terminal", s.workingDir))
	default:
		cmd = exec.Command("powershell", "-NoProfile", "-NoExit")
	}

	cmd.Dir = s.workingDir
	cmd.Env = append(os.Environ(),
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
		"ARGUS_TERMINAL=true",
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000,
	}

	stdinPipe, _ := cmd.StdinPipe()
	stdoutPipe, _ := cmd.StdoutPipe()
	stderrPipe, _ := cmd.StderrPipe()

	s.cmd = cmd
	s.stdin = bufio.NewWriter(stdinPipe)
	_ = stdoutPipe
	_ = stderrPipe

	err := cmd.Start()
	if err != nil {
		return fmt.Errorf("启动Shell失败: %w", err)
	}

	s.running.Store(true)

	go s.readOutput(stdoutPipe)
	go s.readOutput(stderrPipe)

	return nil
}

// readOutput 实时读取Shell输出
func (s *TerminalSession) readOutput(pipe interface{ Read([]byte) (int, error) }) {
	reader := bufio.NewReader(pipe)
	buf := make([]byte, 4096)

	for s.running.Load() {
		n, err := reader.Read(buf)
		if err != nil || n == 0 {
			continue
		}

		rawBytes := buf[:n]
		var output string
		s.mu.Lock()
		enc := s.encoding
		s.mu.Unlock()

		switch enc {
		case "gbk", "gb2312":
			output, _, _ = transform.String(simplifiedchinese.GBK.NewDecoder(), string(rawBytes))
		default:
			output = string(rawBytes)
		}

		s.manager.app.emitTerminalOutput(output)

		if strings.Contains(output, "\n") || strings.Contains(output, "> ") || strings.Contains(output, "$ ") {
			s.busy.Store(false)
		} else if n > 0 && !strings.HasSuffix(strings.TrimSpace(output), "> ") {
			s.busy.Store(true)
		}
	}
}

// WriteToTerminal 写入数据到终端（用户输入）
func (tm *TerminalManager) WriteToTerminal(data string) error {
	session := tm.getActiveSession()
	if session == nil {
		return fmt.Errorf("没有活动的终端会话")
	}

	if data == "\x03" {
		return session.sendSignal("INT")
	}

	session.mu.Lock()
	defer session.mu.Unlock()

	if strings.TrimSpace(data) != "" && !strings.Contains(data, "\r") && !strings.Contains(data, "\n") {
		session.addToHistory(strings.TrimSpace(data))
	}

	_, err := session.stdin.WriteString(data)
	if err == nil {
		err = session.stdin.Flush()
	}

	if strings.Contains(data, "\r") || strings.Contains(data, "\n") {
		session.busy.Store(true)
		go func() {
			time.Sleep(2 * time.Second)
			session.busy.Store(false)
		}()
	}

	return err
}

// sendSignal 发送信号给进程
func (s *TerminalSession) sendSignal(sig string) error {
	if sig == "INT" {
		if s.cmd != nil && s.cmd.Process != nil {
			s.cmd.Process.Signal(os.Interrupt)
			s.busy.Store(false)
			s.manager.app.emitTerminalOutput("^C\r\n")
		}
	}
	return nil
}

// StopTerminal 停止当前活动终端
func (tm *TerminalManager) StopTerminal() error {
	session := tm.getActiveSession()
	if session == nil {
		return nil
	}
	return session.stop()
}

// stop 停止单个会话
func (s *TerminalSession) stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running.Load() {
		return nil
	}

	s.running.Store(false)

	if s.cmd != nil && s.cmd.Process != nil {
		s.cmd.Process.Kill()
		s.cmd.Wait()
	}

	delete(s.manager.sessions, s.ID)

	if s.manager.activeID == s.ID && len(s.manager.sessions) > 0 {
		for id := range s.manager.sessions {
			s.manager.activeID = id
			break
		}
	} else if len(s.manager.sessions) == 0 {
		s.manager.activeID = ""
	}

	s.manager.app.addLog(fmt.Sprintf("[终端] 会话停止: %s", s.Name))
	s.manager.app.emitTerminalOutput("\r\n\x1b[33m⏹ 终端已停止\x1b[0m\r\n")

	runtime.EventsEmit(s.manager.app.ctx, "terminal:session-closed", s.ID)

	return nil
}

// IsTerminalRunning 检查是否有运行中的终端
func (tm *TerminalManager) IsTerminalRunning() (bool, error) {
	session := tm.getActiveSession()
	return session != nil && session.running.Load(), nil
}

// GetTerminalStatus 获取所有终端状态
func (tm *TerminalManager) GetTerminalStatus() []map[string]interface{} {
	tm.sessionMu.RLock()
	defer tm.sessionMu.RUnlock()

	status := make([]map[string]interface{}, 0, len(tm.sessions))

	for _, s := range tm.sessions {
		status = append(status, map[string]interface{}{
			"id":         s.ID,
			"name":       s.Name,
			"running":    s.running.Load(),
			"busy":       s.busy.Load(),
			"shellType":  s.shellType,
			"workingDir": s.workingDir,
			"historyLen": len(s.history),
		})
	}

	return status
}

// SwitchSession 切换活动终端（用于Tab切换）
func (tm *TerminalManager) SwitchSession(sessionID string) error {
	tm.sessionMu.Lock()
	defer tm.sessionMu.Unlock()

	if _, exists := tm.sessions[sessionID]; !exists {
		return fmt.Errorf("终端会话不存在: %s", sessionID)
	}

	tm.activeID = sessionID
	runtime.EventsEmit(tm.app.ctx, "terminal:session-switched", sessionID)
	return nil
}

// CloseSession 关闭指定终端会话
func (tm *TerminalManager) CloseSession(sessionID string) error {
	tm.sessionMu.Lock()
	session, exists := tm.sessions[sessionID]
	tm.sessionMu.Unlock()

	if !exists {
		return nil
	}

	return session.stop()
}

// ExecuteCommandSync 同步执行命令并返回结果（用于SE执行）
func (tm *TerminalManager) ExecuteCommandSync(command string, timeout int) (string, error) {
	session := tm.getActiveSession()

	if session == nil || !session.running.Load() {
		tm.StartTerminal("")
		session = tm.getActiveSession()
		time.Sleep(200 * time.Millisecond)
	}

	if session == nil {
		return "", fmt.Errorf("无法启动终端")
	}

	outputBuf := &strings.Builder{}
	done := make(chan struct{})

	session.manager.app.emitTerminalOutput(fmt.Sprintf(
		"\r\n\x1b[90m[SE] 执行: %s\x1b[0m\r\n",
		command,
	))

	go func() {
		session.manager.WriteToTerminal(command + "\r\n")

		timeoutDuration := time.Duration(timeout) * time.Second
		if timeout <= 0 {
			timeoutDuration = 30 * time.Second
		}

		select {
		case <-time.After(timeoutDuration):
			outputBuf.WriteString("\r\n\x1b[33m[超时] 命令执行超过限制时间\x1b[0m\r\n")
		case <-done:
		}
	}()

	startTime := time.Now()
	for {
		if time.Since(startTime) > time.Duration(timeout)*time.Second+time.Second {
			break
		}
		if !session.busy.Load() && time.Since(startTime) > 500*time.Millisecond {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	close(done)

	result := outputBuf.String()

	if strings.Contains(result, "error") || strings.Contains(result, "Error") ||
		strings.Contains(result, "failed") || strings.Contains(result, "失败") {
		session.manager.app.emitTerminalOutput(
			"\r\n\x1b[31m💡 检测到错误，AI分析中...\x1b[0m\r\n",
		)
	}

	return result, nil
}

// getActiveSession 获取当前活动会话
func (tm *TerminalManager) getActiveSession() *TerminalSession {
	tm.sessionMu.RLock()
	defer tm.sessionMu.RUnlock()
	return tm.sessions[tm.activeID]
}

func (tm *TerminalManager) GetActiveSessionID() string {
	tm.sessionMu.RLock()
	defer tm.sessionMu.RUnlock()
	return tm.activeID
}

func (tm *TerminalManager) SetTerminalEncoding(enc string) error {
	session := tm.getActiveSession()
	if session == nil {
		return fmt.Errorf("没有活动的终端会话")
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	session.encoding = enc
	return nil
}

// addToHistory 添加命令到历史
func (s *TerminalSession) addToHistory(cmd string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.history) > 0 && s.history[len(s.history)-1] == cmd {
		return
	}

	s.history = append(s.history, cmd)
	if len(s.history) > 500 {
		s.history = s.history[len(s.history)-500:]
	}
}

// GetHistory 获取命令历史
func (tm *TerminalManager) GetHistory(sessionID string) []string {
	tm.sessionMu.RLock()
	session := tm.sessions[sessionID]
	tm.sessionMu.RUnlock()

	if session == nil {
		return []string{}
	}

	session.mu.Lock()
	defer session.mu.Unlock()
	return session.history
}

// GetProjectDir 获取项目目录
func (tm *TerminalManager) GetProjectDir() string {
	if tm.app.config.WorkDir != "" {
		return tm.app.config.WorkDir
	}
	dir, _ := os.Getwd()
	for i := 0; i < 5; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		if _, err := os.Stat(filepath.Join(dir, "package.json")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return dir
}

// detectShell 检测可用Shell
func detectShell() string {
	if _, err := exec.LookPath("powershell"); err == nil {
		return "powershell"
	}
	if _, err := exec.LookPath("pwsh"); err == nil {
		return "powershell"
	}
	if _, err := exec.LookPath("cmd"); err == nil {
		return "cmd"
	}
	return "powershell"
}

// ResizeTerminal 调整终端大小（用于前端resize事件）
func (tm *TerminalManager) ResizeTerminal(cols, rows uint16) error {
	session := tm.getActiveSession()
	if session == nil || session.cmd == nil || session.cmd.Process == nil {
		return nil
	}

	tm.app.addLog(fmt.Sprintf("[终端] 大小调整: %dx%d", cols, rows))
	return nil
}
