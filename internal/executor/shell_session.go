package executor

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
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

	// [v0.7.1] 命令历史
	history      []string          // 环形缓冲区，最近执行的命令
	historyPos   int               // 写入位置（环形）
	historyCount int               // 已存储的命令总数（用于判断是否填满）
	historyMax   int               // 最大历史条数（默认 500）
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
		historyMax:  500,
		history:     make([]string, 500), // 预分配环形缓冲区
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

	// [v0.7.1] 记录命令历史（跳过纯空行和重复的连续命令）
	ss.appendToHistory(command)

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

// ========== [v0.7.1] 命令历史 ==========

// appendToHistory 追加命令到历史环形缓冲区（需持有锁）
func (ss *ShellSession) appendToHistory(cmd string) {
	// 跳过空命令和与上一条完全相同的连续重复
	if cmd == "" {
		return
	}
	if ss.historyCount > 0 {
		lastIdx := (ss.historyPos - 1 + ss.historyMax) % ss.historyMax
		if ss.history[lastIdx] == cmd {
			return // 跳过连续重复
		}
	}

	ss.history[ss.historyPos] = cmd
	ss.historyPos = (ss.historyPos + 1) % ss.historyMax
	if ss.historyCount < ss.historyMax {
		ss.historyCount++
	}
}

// History 返回最近的 n 条命令（最多 n 条，按时间倒序，最新的在前）
func (ss *ShellSession) History(n int) []string {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	if n <= 0 || ss.historyCount == 0 {
		return []string{}
	}
	if n > ss.historyCount {
		n = ss.historyCount
	}

	result := make([]string, n)
	for i := 0; i < n; i++ {
		// 从最新往回走：historyPos-1 是最近一条，再往前走 i 条
		idx := (ss.historyPos - 1 - i + ss.historyMax) % ss.historyMax
		result[i] = ss.history[idx]
	}
	return result
}

// HistoryCount 返回历史中的总命令数
func (ss *ShellSession) HistoryCount() int {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	return ss.historyCount
}

// SearchHistory 反向搜索历史命令（类似 Ctrl+R）
// 返回匹配 query 的命令列表（倒序，最近的在前），limit 限制返回条数
func (ss *ShellSession) SearchHistory(query string, limit int) []string {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	if query == "" || ss.historyCount == 0 {
		return []string{}
	}
	if limit <= 0 {
		limit = 20
	}

	query = strings.ToLower(query)
	var results []string

	// 从最新向最旧遍历
	for i := 0; i < ss.historyCount; i++ {
		idx := (ss.historyPos - 1 - i + ss.historyMax) % ss.historyMax
		cmd := ss.history[idx]
		if strings.Contains(strings.ToLower(cmd), query) {
			results = append(results, cmd)
			if len(results) >= limit {
				break
			}
		}
	}
	return results
}

// [v0.7.1] TabComplete 根据输入的前缀返回补全候选列表
// 支持: 文件路径补全、命令名补全、目录补全
func (ss *ShellSession) TabComplete(input string) []string {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	input = strings.TrimSpace(input)
	if input == "" {
		return ss.completeCommands("") // 空输入 → 列出常用命令
	}

	// 提取最后一个词作为补全前缀（处理空格分隔）
	lastWord := input
	if idx := strings.LastIndex(input, " "); idx >= 0 {
		lastWord = input[idx+1:]
	}

	// 判断是路径补全还是命令补全
	// 有空格 → 参数位置，优先路径补全
	hasSpace := strings.LastIndex(input, " ") >= 0
	if hasSpace || strings.Contains(lastWord, "\\") || strings.Contains(lastWord, "/") ||
		strings.Contains(lastWord, ".") || lastWord == "" {
		return ss.completePath(input, lastWord)
	}
	return ss.completeCommand(input, lastWord)
}

// completeCommands 返回可用命令列表（无前缀时调用）
func (ss *ShellSession) completeCommands(prefix string) []string {
	var results []string

	// 常用 Windows 命令白名单
	commonCmds := []string{
		"dir", "cd", "cls", "type", "copy", "move", "del", "mkdir", "rmdir",
		"echo", "set", "if", "for", "call", "start", "tasklist", "taskkill",
		"find", "findstr", "more", "sort", "xcopy", "robocopy", "attrib",
		"icacls", "net", "sc", "reg", "wmic", "powershell", "cmd", "where",
		"git", "go", "node", "npm", "npx", "python", "pip", "cargo", "rustc",
		"docker", "docker-compose", "kubectl", "make", "cmake", "gcc", "g++",
		"javac", "java", "ruby", "php", "perl", "bash", "ssh", "scp", "curl",
		"wget", "tar", "zip", "unzip", "grep", "sed", "awk", "cat", "ls",
		"chmod", "chown", "ps", "kill", "top", "htop", "vim", "nano", "less",
	}

	for _, cmd := range commonCmds {
		if prefix == "" || strings.HasPrefix(strings.ToLower(cmd), strings.ToLower(prefix)) {
			results = append(results, cmd)
		}
	}

	// 去重并排序（简单去重）
	seen := make(map[string]bool)
	var unique []string
	for _, r := range results {
		lower := strings.ToLower(r)
		if !seen[lower] {
			seen[lower] = true
			unique = append(unique, r)
		}
	}
	return unique
}

// completeCommand 补全命令名
func (ss *ShellSession) completeCommand(input, prefix string) []string {
	cmds := ss.completeCommands(prefix)
	if len(cmds) == 0 {
		// 回退到路径补全
		return ss.completePath(input, prefix)
	}

	// 构建完整输入: 去掉最后一个词 + 补全结果
	base := ""
	if idx := strings.LastIndex(input, " "); idx >= 0 {
		base = input[:idx+1]
	}

	results := make([]string, 0, len(cmds))
	for _, cmd := range cmds {
		results = append(results, base+cmd)
	}
	return results
}

// completePath 补全文件/目录路径
func (ss *ShellSession) completePath(input, prefix string) []string {
	dir := ss.workDir
	filePrefix := prefix

	// 如果包含路径分隔符，提取目录部分
	if idx := strings.LastIndexAny(prefix, `\/`); idx >= 0 {
		dirPart := prefix[:idx+1]
		if filepath.IsAbs(dirPart) {
			dir = dirPart
		} else {
			dir = filepath.Join(ss.workDir, dirPart)
		}
		filePrefix = prefix[idx+1:]
	}

	// 读取目录内容
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var results []string
	for _, entry := range entries {
		name := entry.Name()
		// 跳过隐藏文件（以 . 开头，Windows 下也过滤）
		if len(name) > 0 && name[0] == '.' && filePrefix != "." && !strings.HasPrefix(filePrefix, ".") {
			continue
		}
		if !strings.EqualFold(name[:cmpLen(name, filePrefix)], filePrefix) {
			continue
		}

		full := name
		if entry.IsDir() {
			full += `\` // 目录加反斜杠
		}

		// 还原原始路径前缀
		base := input
		if idx := strings.LastIndexAny(prefix, `\/`); idx >= 0 {
			base = input[:len(input)-len(prefix)+idx+1]
		} else if idx := strings.LastIndex(input, " "); idx >= 0 {
			base = input[:idx+1]
		} else {
			base = ""
		}
		results = append(results, base+full)

		if len(results) >= 20 { // 限制候选数量
			break
		}
	}
	return results
}

func cmpLen(a, b string) int {
	if len(a) < len(b) {
		return len(a)
	}
	return len(b)
}
