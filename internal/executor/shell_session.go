package executor

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

// ShellSession 持久化 shell 会话，保持工作目录和环境变量跨命令
type ShellSession struct {
	mu           sync.Mutex
	workDir      string
	cmd          *exec.Cmd
	stdin        io.WriteCloser
	stdout       io.ReadCloser
	stdoutBuf    *bufio.Scanner
	envVars      map[string]string
	cwd          string
	outputEnd    string // 输出结束标记
	running      bool
	stopCh       chan struct{}
	lastActive   time.Time // 最后活跃时间，用于空闲清理
	idleTimeout  time.Duration // 空闲超时，默认60秒
}

// NewShellSession 创建持久化 shell 会话
func NewShellSession(workDir string) (*ShellSession, error) {
	ss := &ShellSession{
		workDir:     workDir,
		envVars:     make(map[string]string),
		outputEnd:   "___ARGUS_CMD_END___",
		stopCh:      make(chan struct{}),
		idleTimeout: 60 * time.Second,
		lastActive:  time.Now(),
	}

	if err := ss.start(); err != nil {
		return nil, fmt.Errorf("启动 shell 失败: %w", err)
	}

	// 启动空闲清理 goroutine
	go ss.idleChecker()

	return ss, nil
}

// start 启动持久的 cmd.exe 进程
func (ss *ShellSession) start() error {
	ss.cmd = exec.Command("cmd.exe")
	ss.cmd.Dir = ss.workDir
	ss.cwd = ss.workDir
	ss.cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow: true,
	}

	// 设置环境
	ss.cmd.Env = os.Environ()
	for k, v := range ss.envVars {
		ss.cmd.Env = append(ss.cmd.Env, k+"="+v)
	}

	var err error
	ss.stdin, err = ss.cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("stdin pipe: %w", err)
	}
	ss.stdout, err = ss.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}
	// stderr 合并到 stdout
	ss.cmd.Stderr = ss.cmd.Stdout

	if err := ss.cmd.Start(); err != nil {
		return fmt.Errorf("启动 cmd.exe: %w", err)
	}

	ss.stdoutBuf = bufio.NewScanner(ss.stdout)
	// 加大 buffer 避免长输出截断
	buf := make([]byte, 0, 1024*1024)
	ss.stdoutBuf.Buffer(buf, 10*1024*1024)

	ss.running = true

	// 初始设置：关闭 echo，设置代码页
	ss.execRaw("@echo off")
	ss.execRaw("chcp 65001 >nul 2>&1")
	// 切换到工作目录
	ss.execRaw(fmt.Sprintf("cd /d %s", ss.workDir))

	fmt.Printf("[ShellSession] ✅ 持久化 shell 启动，工作目录: %s\n", ss.workDir)
	return nil
}

// idleChecker 空闲清理 goroutine：超过 idleTimeout 无操作则自动关闭
func (ss *ShellSession) idleChecker() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ss.stopCh:
			return
		case <-ticker.C:
			ss.mu.Lock()
			idle := time.Since(ss.lastActive)
			isRunning := ss.running
			ss.mu.Unlock()

			if isRunning && idle > ss.idleTimeout {
				fmt.Printf("[ShellSession] 🧹 空闲超时 (%v > %v)，自动关闭\n", idle, ss.idleTimeout)
				ss.Close()
				return
			}
		}
	}
}

// Exec 在持久化 shell 中执行命令，返回输出
func (ss *ShellSession) Exec(command string, timeout time.Duration) (string, error) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	ss.lastActive = time.Now() // 更新活跃时间，阻止空闲清理

	if !ss.running {
		if err := ss.start(); err != nil {
			return "", fmt.Errorf("shell 重启失败: %w", err)
		}
	}

	command = strings.TrimSpace(command)
	if command == "" {
		return "", nil
	}

	// 跟踪 cd 命令更新工作目录
	if strings.HasPrefix(strings.ToLower(command), "cd ") || strings.HasPrefix(strings.ToLower(command), "cd/") || strings.HasPrefix(strings.ToLower(command), "chdir ") {
		parts := strings.Fields(command)
		if len(parts) >= 2 {
			newDir := parts[1]
			newDir = strings.Trim(newDir, "\"'")
			// 尝试解析新路径
			if strings.HasPrefix(newDir, "/d ") {
				newDir = strings.TrimPrefix(newDir, "/d ")
				newDir = strings.Trim(newDir, "\"'")
			}
			ss.cwd = newDir
		}
	}

	// 注入标记：echo 结束标记
	fullCmd := fmt.Sprintf("(%s) & echo %s", command, ss.outputEnd)

	// 写命令到 stdin
	if _, err := io.WriteString(ss.stdin, fullCmd+"\r\n"); err != nil {
		ss.restart()
		return "", fmt.Errorf("写入命令失败: %w", err)
	}

	// 读取输出直到结束标记
	var output strings.Builder
	deadline := time.After(timeout)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for ss.stdoutBuf.Scan() {
			line := ss.stdoutBuf.Text()
			// 检查是否是结束标记
			if strings.TrimSpace(line) == ss.outputEnd {
				return
			}
			output.WriteString(line)
			output.WriteString("\n")
		}
	}()

	select {
	case <-done:
		result := strings.TrimSpace(output.String())
		// 回显命令本身的行（cmd 会回显命令），尝试去掉
		lines := strings.Split(result, "\n")
		if len(lines) > 0 && strings.Contains(lines[0], command) {
			result = strings.Join(lines[1:], "\n")
		}
		result = strings.TrimSpace(result)

		// 如果命令是 cd，刷新 cwd
		if strings.HasPrefix(strings.ToLower(command), "cd ") || strings.HasPrefix(strings.ToLower(command), "cd/") {
			// 用 echo %cd% 获取真实路径
			realCwd := ss.execRaw("echo %cd%")
			realCwd = strings.TrimSpace(realCwd)
			if realCwd != "" {
				ss.cwd = realCwd
			}
		}

		return result, nil

	case <-deadline:
		// 超时：杀掉当前命令但不杀 shell
		fmt.Printf("[ShellSession] ⚠️ 命令超时 (%v): %s\n", timeout, command[:min(60, len(command))])
		// 发送 Ctrl+C
		io.WriteString(ss.stdin, "\x03\r\n")
		// 消费残留输出
		ss.drainOutput()
		return "", fmt.Errorf("命令超时 (%v)", timeout)

	case <-ss.stopCh:
		return "", fmt.Errorf("shell 已关闭")
	}
}

// execRaw 内部使用，不跟踪 cd
func (ss *ShellSession) execRaw(command string) string {
	fullCmd := fmt.Sprintf("%s & echo %s", command, ss.outputEnd)
	io.WriteString(ss.stdin, fullCmd+"\r\n")

	var output strings.Builder
	done := make(chan struct{})
	go func() {
		defer close(done)
		for ss.stdoutBuf.Scan() {
			line := ss.stdoutBuf.Text()
			if strings.TrimSpace(line) == ss.outputEnd {
				return
			}
			output.WriteString(line)
			output.WriteString("\n")
		}
	}()

	select {
	case <-done:
		result := strings.TrimSpace(output.String())
		lines := strings.Split(result, "\n")
		if len(lines) > 0 && strings.Contains(lines[0], command) {
			result = strings.Join(lines[1:], "\n")
		}
		return strings.TrimSpace(result)
	case <-time.After(5 * time.Second):
		return ""
	}
}

// drainOutput 消费所有待读输出
func (ss *ShellSession) drainOutput() {
	for {
		select {
		case <-time.After(100 * time.Millisecond):
			return
		default:
			if !ss.stdoutBuf.Scan() {
				return
			}
			if strings.TrimSpace(ss.stdoutBuf.Text()) == ss.outputEnd {
				return
			}
		}
	}
}

// restart 重启 shell
func (ss *ShellSession) restart() {
	fmt.Println("[ShellSession] 🔄 重启 shell...")
	ss.Close()
	ss.start()
}

// Close 关闭 shell 会话
func (ss *ShellSession) Close() {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	if !ss.running {
		return
	}

	ss.running = false
	close(ss.stopCh)

	// 发送 exit
	io.WriteString(ss.stdin, "exit\r\n")
	time.Sleep(100 * time.Millisecond)

	if ss.cmd != nil && ss.cmd.Process != nil {
		ss.cmd.Process.Kill()
	}
	ss.cmd = nil
}

// CWD 返回当前工作目录
func (ss *ShellSession) CWD() string {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	return ss.cwd
}

// SetEnv 设置环境变量
func (ss *ShellSession) SetEnv(key, value string) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	ss.envVars[key] = value
	// 在当前 shell 中也设置
	ss.execRaw(fmt.Sprintf("set %s=%s", key, value))
}

// IsRunning 检查是否在运行
func (ss *ShellSession) IsRunning() bool {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	return ss.running
}

// SetIdleTimeout 设置空闲超时时间
func (ss *ShellSession) SetIdleTimeout(d time.Duration) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	ss.idleTimeout = d
}
