package executor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"argus/internal/board"
	"argus/internal/types"
)

type Executor struct {
	workDir        string
	boardManager   *board.Manager
	terminalOutput func(string)
	onFileWritten  func(path string)
}

func NewExecutor(workDir string, boardManager *board.Manager) *Executor {
	return &Executor{
		workDir:      workDir,
		boardManager: boardManager,
	}
}

func (e *Executor) SetTerminalOutput(callback func(string)) {
	e.terminalOutput = callback
}

func (e *Executor) SetOnFileWritten(callback func(path string)) {
	e.onFileWritten = callback
}

func (e *Executor) SetWorkDir(workDir string) {
	e.workDir = workDir
}

func (e *Executor) WriteFile(path, content string) error {
	protectedFiles := []string{
		"main.go",
		"app.go",
		"router.go",
		"manager.go",
	}
	
	baseName := filepath.Base(path)
	for _, protected := range protectedFiles {
		if strings.EqualFold(baseName, protected) {
			fmt.Printf("[Executor] 🛡️ SECURITY: 拒绝写入受保护文件 %s (可能是项目源代码)\n", path)
			return fmt.Errorf("security: 禁止写入受保护文件 '%s'（防止覆盖项目源代码）", path)
		}
	}

	var fullPath string
	if filepath.IsAbs(path) {
		fullPath = path
	} else {
		fullPath = filepath.Join(e.workDir, path)
	}
	if !isPathInDir(fullPath, e.workDir) {
		return fmt.Errorf("path outside work directory: %s", path)
	}

	fmt.Printf("[Executor] WriteFile: %s -> %s (%d bytes)\n", path, fullPath, len(content))

	if isPythonFile(path) {
		fixed := fixPythonIndentation(content)
		if fixed != content {
			fmt.Printf("[Executor] Python indentation fixed: %s\n", path)
			content = fixed
		}
	}

	dir := filepath.Dir(fullPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %v", err)
	}

	if isPythonFile(path) {
		tmpFile := filepath.Join(os.TempDir(), fmt.Sprintf("argus_pycheck_%d.py", time.Now().UnixNano()))
		if err := os.WriteFile(tmpFile, []byte(content), 0644); err == nil {
			if syntaxErr := e.checkPythonSyntax(tmpFile); syntaxErr != "" {
				os.Remove(tmpFile)
				fmt.Printf("[Executor] Python syntax error in %s: %s\n", path, syntaxErr)
				return fmt.Errorf("Python语法错误（缩进问题）: %s\n请确保使用4空格缩进，方法体比方法定义多4个空格", syntaxErr)
			}
			os.Remove(tmpFile)
		}
	}

	if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write file: %v", err)
	}

	if e.onFileWritten != nil {
		e.onFileWritten(path)
	}

	return nil
}

func isPythonFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".py" || ext == ".pyw"
}

func (e *Executor) checkPythonSyntax(filePath string) string {
	cmd := exec.Command("python", "-m", "py_compile", filePath)
	cmd.Dir = e.workDir
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}

	output, err := cmd.CombinedOutput()
	if err != nil {
		lines := strings.Split(string(output), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "py_compile") {
				return line
			}
		}
		return err.Error()
	}
	return ""
}

func fixPythonIndentation(content string) string {
	lines := strings.Split(content, "\n")
	if len(lines) <= 1 {
		return content
	}

	indentSizes := make(map[int]int)
	for _, line := range lines {
		trimmed := strings.TrimLeft(line, " \t")
		if trimmed == "" {
			continue
		}
		spaces := len(line) - len(trimmed)
		if spaces > 0 {
			indentSizes[spaces]++
		}
	}

	if len(indentSizes) == 0 {
		return content
	}

	minIndent := -1
	for spaces := range indentSizes {
		if minIndent == -1 || spaces < minIndent {
			minIndent = spaces
		}
	}

	if minIndent <= 0 {
		return content
	}

	has4Space := indentSizes[4] > 0
	has1Space := indentSizes[1] > 0
	if has1Space && !has4Space && minIndent == 1 {
		var fixed []string
		for _, line := range lines {
			trimmed := strings.TrimLeft(line, " \t")
			if trimmed == "" {
				fixed = append(fixed, line)
				continue
			}
			spaces := len(line) - len(trimmed)
			newSpaces := spaces * 4
			fixed = append(fixed, strings.Repeat(" ", newSpaces)+trimmed)
		}
		return strings.Join(fixed, "\n")
	}

	return content
}

func (e *Executor) ReadFile(path string) (string, error) {
	var fullPath string
	if filepath.IsAbs(path) {
		fullPath = path
	} else {
		fullPath = filepath.Join(e.workDir, path)
	}
	if !isPathInDir(fullPath, e.workDir) {
		return "", fmt.Errorf("path outside work directory: %s", path)
	}

	data, err := os.ReadFile(fullPath)
	if err != nil {
		return "", fmt.Errorf("failed to read file: %v", err)
	}

	return string(data), nil
}

func (e *Executor) Exec(command string, timeout time.Duration) (string, error) {
	if isServerCommand(command) {
		return e.execServerCommand(command)
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	fmt.Printf("[Executor] Running: %s in %s (timeout: %v)\n", command, e.workDir, timeout)

	cmd := exec.CommandContext(ctx, "cmd", "/c", command)
	cmd.Dir = e.workDir
	cmd.Env = os.Environ()
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow: true,
	}

	output, err := cmd.CombinedOutput()
	outputStr := string(output)
	fmt.Printf("[Executor] Output length: %d, err: %v\n", len(outputStr), err)

	if e.terminalOutput != nil {
		e.terminalOutput(fmt.Sprintf("> %s\n%s", command, outputStr))
	}

	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return outputStr, fmt.Errorf("command timeout after %v", timeout)
		}
		return outputStr, fmt.Errorf("command failed: %v", err)
	}

	return outputStr, nil
}

func isServerCommand(command string) bool {
	serverKeywords := []string{"http.ListenAndServe", ":8080", ":3000", "server.go"}
	for _, kw := range serverKeywords {
		if strings.Contains(command, kw) {
			return true
		}
	}
	return false
}

func (e *Executor) execServerCommand(command string) (string, error) {
	fmt.Printf("[Executor] Server command detected: %s\n", command)

	cmd := exec.Command("cmd", "/c", "start", "/b", command)
	cmd.Dir = e.workDir
	cmd.Env = os.Environ()
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow: true,
	}

	err := cmd.Start()
	if err != nil {
		return "", fmt.Errorf("failed to start server: %v", err)
	}

	time.Sleep(2 * time.Second)

	checkCmd := exec.Command("cmd", "/c", "netstat -ano | findstr :8080")
	checkCmd.Dir = e.workDir
	output, _ := checkCmd.CombinedOutput()

	if len(output) > 0 {
		return fmt.Sprintf("Server started successfully on port 8080\n%s", string(output)), nil
	}

	return "Server started (verification pending)", nil
}

func (e *Executor) ExecInteractive(command string) error {
	cmdLine := command + " && pause"
	psCmd := fmt.Sprintf("Start-Process -FilePath 'cmd.exe' -ArgumentList '/k', '%s'", cmdLine)

	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", psCmd)
	cmd.Dir = e.workDir

	return cmd.Run()
}

func (e *Executor) DeleteFile(path string) error {
	fullPath := filepath.Join(e.workDir, path)
	if !isPathInDir(fullPath, e.workDir) {
		return fmt.Errorf("path outside work directory: %s", path)
	}

	return os.Remove(fullPath)
}

func (e *Executor) ListFiles() ([]FileInfo, error) {
	return e.listFilesRecursive(e.workDir, "")
}

type FileInfo struct {
	Path    string `json:"path"`
	Size    int64  `json:"size"`
	ModTime string `json:"mod_time"`
	IsDir   bool   `json:"is_dir"`
}

func (e *Executor) listFilesRecursive(dir, prefix string) ([]FileInfo, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var files []FileInfo
	for _, entry := range entries {
		if entry.Name()[0] == '.' {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		path := filepath.Join(prefix, entry.Name())
		files = append(files, FileInfo{
			Path:    path,
			Size:    info.Size(),
			ModTime: info.ModTime().Format("2006-01-02 15:04:05"),
			IsDir:   entry.IsDir(),
		})

		if entry.IsDir() {
			subFiles, err := e.listFilesRecursive(
				filepath.Join(dir, entry.Name()),
				path,
			)
			if err == nil {
				files = append(files, subFiles...)
			}
		}
	}

	return files, nil
}

func (e *Executor) UpdateBoardStep(step int) error {
	return e.boardManager.UpdateStep(step)
}

func (e *Executor) MarkBoardDone() error {
	return e.boardManager.MarkDone()
}

func (e *Executor) MarkBoardError() error {
	return e.boardManager.MarkError()
}

func (e *Executor) GetBoardStatus() types.Board {
	return e.boardManager.Get()
}

func (e *Executor) UpdateTask(task string, totalSteps int) error {
	return e.boardManager.UpdateTask(task, totalSteps)
}

func (e *Executor) MarkDone() error {
	return e.boardManager.MarkDone()
}

func (e *Executor) MarkError() error {
	return e.boardManager.MarkError()
}

func (e *Executor) IsTaskDone() bool {
	return e.boardManager.IsDone()
}

func (e *Executor) AssignTask(taskID, title, description string, dependencies []string) error {
	return e.boardManager.UpdateTask(title, 1)
}

func isPathInDir(path, dir string) bool {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	if len(absPath) < len(absDir) {
		return false
	}
	prefix := absPath[:len(absDir)]
	if prefix != absDir {
		return false
	}
	if len(absPath) == len(absDir) {
		return true
	}
	return os.IsPathSeparator(absPath[len(absDir)])
}