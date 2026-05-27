package executor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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

	var fullPath string
	if filepath.IsAbs(path) {
		fullPath = path
	} else {
		fullPath = filepath.Join(e.workDir, path)
	}

	baseName := filepath.Base(fullPath)
	for _, protected := range protectedFiles {
		if strings.EqualFold(baseName, protected) {
			if isPathInDir(fullPath, e.workDir) {
				fmt.Printf("[Executor] ✅ 允许写入工作目录内的受保护文件: %s\n", fullPath)
			} else {
				fmt.Printf("[Executor] 🛡️ SECURITY: 拒绝写入受保护文件 %s (可能是项目源代码)\n", path)
				return fmt.Errorf("security: 禁止写入受保护文件 '%s'（防止覆盖项目源代码）", path)
			}
		}
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

// [P0] EditResult 编辑结果
type EditResult struct {
	Success      bool   `json:"success"`
	Error        string `json:"error,omitempty"`
	Diff         string `json:"diff"`                   // Unified diff 格式
	LinesChanged int    `json:"lines_changed"`          // 修改的行数
	FilePath     string `json:"file_path,omitempty"`    // 文件路径
}

// [P0] SearchFilesMatch 单个文件匹配结果
type SearchFilesMatch struct {
	File      string   `json:"file"`                // 文件路径（相对）
	Line      int      `json:"line"`                // 匹配行号
	Column    int      `json:"column"`              // 匹配列号
	Content   string   `json:"content"`             // 匹配行的内容
	ContextBefore []string `json:"context_before,omitempty"` // 前2行上下文
	ContextAfter  []string `json:"context_after,omitempty"`  // 后2行上下文
}

// [P0] SearchFilesResult 搜索结果
type SearchFilesResult struct {
	Pattern     string             `json:"pattern"`       // 搜索模式
	IsRegex     bool               `json:"is_regex"`      // 是否正则
	TotalMatches int               `json:"total_matches"` // 总匹配数
	FilesSearched int              `json:"files_searched"` // 搜索的文件数
	Matches     []SearchFilesMatch `json:"matches"`       // 匹配列表
	Error       string             `json:"error,omitempty"`
}

// [P0] SearchFiles 全局搜索文件内容（支持正则和字符串匹配）
func (e *Executor) SearchFiles(pattern string, opts ...SearchOption) (*SearchFilesResult, error) {
	options := defaultSearchOptions()
	for _, opt := range opts {
		opt(&options)
	}

	result := &SearchFilesResult{
		Pattern: pattern,
		IsRegex: options.IsRegex,
		Matches: make([]SearchFilesMatch, 0),
	}

	var regex *regexp.Regexp
	var err error
	if options.IsRegex {
		regex, err = regexp.Compile(pattern)
		if err != nil {
			result.Error = fmt.Sprintf("invalid regex: %v", err)
			return result, nil
		}
	}

	searchDir := e.workDir
	if options.Path != "" {
		if filepath.IsAbs(options.Path) {
			searchDir = options.Path
		} else {
			searchDir = filepath.Join(e.workDir, options.Path)
		}
	}

	if !isPathInDir(searchDir, e.workDir) {
		return &SearchFilesResult{Error: "path outside work directory"}, nil
	}

	err = filepath.Walk(searchDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil || info.IsDir() {
			return nil
		}

		relPath, _ := filepath.Rel(e.workDir, path)
		relPath = filepath.ToSlash(relPath)

		if e.shouldSkipFile(relPath, options) {
			return nil
		}

		result.FilesSearched++

		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}

		lines := strings.Split(string(content), "\n")
		e.searchInLines(lines, relPath, pattern, regex, result, options)

		if options.MaxResults > 0 && len(result.Matches) >= options.MaxResults {
			return filepath.SkipAll
		}
		return nil
	})

	if err != nil {
		result.Error = fmt.Sprintf("search error: %v", err)
	}

	result.TotalMatches = len(result.Matches)
	fmt.Printf("[Executor] SearchFiles: '%s' → %d matches in %d files\n",
		pattern, result.TotalMatches, result.FilesSearched)

	return result, nil
}

// [P0] SearchOption 搜索选项函数类型
type SearchOption func(*SearchOptions)

// [P0] SearchOptions 搜索配置
type SearchOptions struct {
	Path        string   // 搜索子目录（相对路径）
	FilePattern string   // glob 文件过滤（如 "*.go"）
	IsRegex     bool     // 是否使用正则表达式
	CaseInsensitive bool // 是否忽略大小写
	MaxResults  int      // 最大返回数量（0=不限）
	ContextLines int     // 上下文行数（默认2）
}

func defaultSearchOptions() SearchOptions {
	return SearchOptions{
		IsRegex:          false,
		CaseInsensitive:  false,
		MaxResults:       100,
		ContextLines:     2,
	}
}

func WithPath(p string) SearchOption {
	return func(o *SearchOptions) { o.Path = p }
}
func WithFilePattern(fp string) SearchOption {
	return func(o *SearchOptions) { o.FilePattern = fp }
}
func WithRegex() SearchOption {
	return func(o *SearchOptions) { o.IsRegex = true }
}
func WithCaseInsensitive() SearchOption {
	return func(o *SearchOptions) { o.CaseInsensitive = true }
}
func WithMaxResults(n int) SearchOption {
	return func(o *SearchOptions) { o.MaxResults = n }
}
func WithContextLines(n int) SearchOption {
	return func(o *SearchOptions) { o.ContextLines = n }
}

func (e *Executor) shouldSkipFile(relPath string, opts SearchOptions) bool {
	skipDirs := map[string]bool{
		".git": true, "node_modules": true, "vendor": true,
		".idea": true, "__pycache__": true, ".next": true,
		"dist": true, "build": true, ".cache": true,
	}
	dir := filepath.Dir(relPath)
	for d := dir; d != "." && d != ""; {
		if skipDirs[d] {
			return true
		}
		d = filepath.Dir(d)
	}
	if opts.FilePattern != "" {
		base := filepath.Base(relPath)
		matched, _ := filepath.Match(opts.FilePattern, base)
		if !matched {
			return true
		}
	}
	skipExts := map[string]bool{
		".exe": true, ".dll": true, ".so": true, ".dylib": true,
		".png": true, ".jpg": true, ".gif": true, ".ico": true,
		".pdf": true, ".zip": true, ".tar.gz": true, ".bin": true,
		".woff": true, ".ttf": true, ".lock": true,
	}
	ext := strings.ToLower(filepath.Ext(relPath))
	if skipExts[ext] {
		return true
	}
	return false
}

func (e *Executor) searchInLines(lines []string, relPath, pattern string, regex *regexp.Regexp, result *SearchFilesResult, opts SearchOptions) {
	for i, line := range lines {
		lineNum := i + 1
		var matched bool
		var col int
		searchStr := line
		if opts.CaseInsensitive {
			searchStr = strings.ToLower(line)
			patternLower := strings.ToLower(pattern)
			col = strings.Index(searchStr, patternLower)
			matched = col >= 0
		} else if regex != nil {
			loc := regex.FindStringIndex(line)
			matched = loc != nil
			if matched {
				col = loc[0]
			}
		} else {
			col = strings.Index(line, pattern)
			matched = col >= 0
		}
		if matched {
			match := SearchFilesMatch{
				File:    relPath,
				Line:    lineNum,
				Column:  col + 1,
				Content: strings.TrimSpace(line),
			}
			ctxLines := opts.ContextLines
			if ctxLines > 0 {
				for j := lineNum - 1 - ctxLines; j >= 0 && j < lineNum-1; j++ {
					if j < len(lines) {
						match.ContextBefore = append([]string{strings.TrimSpace(lines[j])}, match.ContextBefore...)
					}
				}
				for j := lineNum; j < lineNum+ctxLines && j < len(lines); j++ {
					match.ContextAfter = append(match.ContextAfter, strings.TrimSpace(lines[j]))
				}
			}
			result.Matches = append(result.Matches, match)
		}
	}
}

func (e *Executor) EditFile(path, oldStr, newStr string) (*EditResult, error) {
	var fullPath string
	if filepath.IsAbs(path) {
		fullPath = path
	} else {
		fullPath = filepath.Join(e.workDir, path)
	}

	if !isPathInDir(fullPath, e.workDir) {
		return &EditResult{
			Success:  false,
			Error:    fmt.Sprintf("path outside work directory: %s", path),
			FilePath: path,
		}, nil
	}

	fmt.Printf("[Executor] EditFile: %s (old_str length: %d, new_str length: %d)\n",
		path, len(oldStr), len(newStr))

	content, err := os.ReadFile(fullPath)
	if err != nil {
		return &EditResult{
			Success:  false,
			Error:    fmt.Sprintf("read file failed: %v", err),
			FilePath: path,
		}, nil
	}

	original := string(content)

	if !strings.Contains(original, oldStr) {
		return &EditResult{
			Success:  false,
			Error:    fmt.Sprintf("old_str not found in %s (searched for: %.50s...)", path, oldStr),
			Diff:     "",
			FilePath: path,
		}, nil
	}

	newContent := strings.Replace(original, oldStr, newStr, 1)

	diff := generateUnifiedDiff(original, newContent, path)

	if err := os.WriteFile(fullPath, []byte(newContent), 0644); err != nil {
		return &EditResult{
			Success:  false,
			Error:    fmt.Sprintf("write file failed: %v", err),
			FilePath: path,
		}, nil
	}

	linesChanged := countLinesChanged(oldStr, newStr)

	result := &EditResult{
		Success:      true,
		Diff:         diff,
		LinesChanged: linesChanged,
		FilePath:     path,
	}

	fmt.Printf("[Executor] ✅ EditFile success: %s (%d lines changed)\n", path, linesChanged)

	if e.onFileWritten != nil {
		e.onFileWritten(path)
	}

	return result, nil
}

// [P0] generateUnifiedDiff 生成简单的 unified diff
func generateUnifiedDiff(oldContent, newContent, filename string) string {
	oldLines := strings.Split(oldContent, "\n")
	newLines := strings.Split(newContent, "\n")

	var diff strings.Builder
	diff.WriteString(fmt.Sprintf("--- a/%s\n+++ b/%s\n", filename, filename))

	for i := range oldLines {
		if i >= len(newLines) || oldLines[i] != newLines[i] {
			diff.WriteString(fmt.Sprintf("@@ -%d,%d +%d,%d @@\n",
				i+1, len(oldLines)-i, i+1, len(newLines)-i))

			for j := i; j < len(oldLines); j++ {
				if j < len(newLines) && oldLines[j] == newLines[j] {
					break
				}
				diff.WriteString(fmt.Sprintf("-%s\n", oldLines[j]))
			}

			for j := i; j < len(newLines); j++ {
				if j < len(oldLines) && oldLines[j] == newLines[j] {
					break
				}
				diff.WriteString(fmt.Sprintf("+%s\n", newLines[j]))
			}

			break
		}
	}

	return diff.String()
}

// [P0] countLinesChanged 计算修改的行数
func countLinesChanged(oldStr, newStr string) int {
	oldLines := len(strings.Split(oldStr, "\n"))
	newLines := len(strings.Split(newStr, "\n"))

	if oldLines > newLines {
		return oldLines
	}
	return newLines
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

// [P1] GitResult Git 操作结果
type GitResult struct {
	Success   bool   `json:"success"`
	Action    string `json:"action"`              // status/diff/commit/push/log/branch
	Output    string `json:"output"`              // 命令输出
	Error     string `json:"error,omitempty"`     // 错误信息
	ExitCode  int    `json:"exit_code"`           // 退出码

	Diff      string `json:"diff,omitempty"`      // diff 内容（仅 diff 操作）
	Status    *GitStatus `json:"status,omitempty"` // 结构化状态（仅 status 操作）
	Log       []GitLogEntry `json:"log,omitempty"` // 日志条目（仅 log 操作）
}

// [P1] GitStatus 结构化 Git 状态
type GitStatus struct {
	Branch     string            `json:"branch"`               // 当前分支
	Ahead      int               `json:"ahead"`                // 领先远程提交数
	Behind     int               `json:"behind"`               // 落后远程提交数
	Staged     []string          `json:"staged"`               // 已暂存文件
	Modified   []string          `json:"modified"`             // 已修改未暂存
	Untracked  []string          `json:"untracked"`            // 未跟踪文件
	IsClean    bool              `json:"is_clean"`             // 工作区是否干净
}

// [P1] GitLogEntry Git 日志条目
type GitLogEntry struct {
	Hash    string `json:"hash"`
	Author  string `json:"author"`
	Date    string `json:"date"`
	Message string `json:"message"`
}

// [P1] GitOperation 执行 Git 命令
func (e *Executor) GitOperation(action, message string, args []string) (*GitResult, error) {
	result := &GitResult{
		Action: action,
	}

	var cmd *exec.Cmd
	workDir := e.workDir

	switch action {
	case "status":
		cmd = exec.Command("git", "status", "--short", "--branch")
	case "diff":
		diffArgs := []string{"diff"}
		diffArgs = append(diffArgs, args...)
		cmd = exec.Command("git", diffArgs...)
	case "commit":
		if message == "" {
			return &GitResult{Success: false, Action: action, Error: "commit 需要 message"}, nil
		}
		cmd = exec.Command("git", "commit", "-m", message)
	case "push":
		pushArgs := []string{"push"}
		pushArgs = append(pushArgs, args...)
		cmd = exec.Command("git", pushArgs...)
	case "pull":
		pullArgs := []string{"pull"}
		pullArgs = append(pullArgs, args...)
		cmd = exec.Command("git", pullArgs...)
	case "log":
		logArgs := []string{"log", "--oneline", "-20"}
		logArgs = append(logArgs, args...)
		cmd = exec.Command("git", logArgs...)
	case "branch":
		branchArgs := []string{"branch", "-a"}
		branchArgs = append(branchArgs, args...)
		cmd = exec.Command("git", branchArgs...)
	case "show":
		showArgs := []string{"show"}
		showArgs = append(showArgs, args...)
		cmd = exec.Command("git", showArgs...)
	default:
		return &GitResult{Success: false, Action: action, Error: fmt.Sprintf("不支持的 git 操作: %s", action)}, nil
	}

	cmd.Dir = workDir
	output, err := cmd.CombinedOutput()
	result.Output = strings.TrimSpace(string(output))

	if err != nil {
		result.ExitCode = 1
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		}
		result.Error = fmt.Sprintf("git %s 失败: %v", action, err)
		if result.Output != "" {
			result.Error += "\n" + result.Output
		}
		fmt.Printf("[Executor] GitOperation '%s' ❌ exit=%d\n", action, result.ExitCode)
		return result, nil
	}

	result.Success = true
	result.ExitCode = 0

	switch action {
	case "diff":
		result.Diff = result.Output
	case "status":
		result.Status = parseGitStatus(result.Output)
	case "log":
		result.Log = parseGitLog(result.Output)
	}

	fmt.Printf("[Executor] GitOperation '%s' ✅ (output %d chars)\n", action, len(result.Output))
	return result, nil
}

func parseGitStatus(output string) *GitStatus {
	status := &GitStatus{
		Staged:    make([]string, 0),
		Modified:  make([]string, 0),
		Untracked: make([]string, 0),
	}
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "## ") {
			branchInfo := strings.TrimPrefix(line, "## ")
			parts := strings.Split(branchInfo, "...")
			status.Branch = strings.TrimSpace(parts[0])
			if len(parts) > 1 {
				aheadBehind := parts[1]
				re := regexp.MustCompile(`ahead (\d+)`)
				if matches := re.FindStringSubmatch(aheadBehind); len(matches) > 1 {
					fmt.Sscanf(matches[1], "%d", &status.Ahead)
				}
				re = regexp.MustCompile(`behind (\d+)`)
				if matches := re.FindStringSubmatch(aheadBehind); len(matches) > 1 {
					fmt.Sscanf(matches[1], "%d", &status.Behind)
				}
			}
			continue
		}
		if len(line) < 2 {
			continue
		}
		switch line[0] {
		case ' ', '?':
			status.Untracked = append(status.Untracked, strings.TrimSpace(line[2:]))
		case 'M', 'A', 'D', 'R', 'C':
			status.Staged = append(status.Staged, strings.TrimSpace(line[2:]))
		}
		if len(line) > 1 && line[0] == ' ' && line[1] == 'M' {
			status.Modified = append(status.Modified, strings.TrimSpace(line[2:]))
		} else if len(line) > 1 && line[0] == 'M' && line[1] != ' ' {
			status.Modified = append(status.Modified, strings.TrimSpace(line[1:]))
		}
	}
	status.IsClean = len(status.Staged) == 0 && len(status.Modified) == 0 && len(status.Untracked) == 0
	return status
}

func parseGitLog(output string) []GitLogEntry {
	entries := make([]GitLogEntry, 0)
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, " ", 2)
		if len(parts) >= 2 {
			entries = append(entries, GitLogEntry{
				Hash:    parts[0],
				Message: parts[1],
			})
		}
	}
	return entries
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

// [P1] 测试运行器

type TestConfig struct {
	Pattern  string `json:"pattern"`   // 测试匹配模式 ("./...", "TestLogin")
	Coverage bool   `json:"coverage"`  // 是否生成覆盖率报告
	Verbose  bool   `json:"verbose"`   // 详细输出
	Timeout  int    `json:"timeout"`   // 超时秒数（默认60）
}

type TestCase struct {
	Name     string `json:"name"`     // 测试名（如 TestEditFile_BasicReplace）
	Status   string `json:"status"`   // pass / fail / skip / panic
	Duration string `json:"duration"` // 耗时（如 "0.05s"）
	Error    string `json:"error,omitempty"` // 失败时的错误信息
}

type TestReport struct {
	Success  bool        `json:"success"`       // 全部通过?
	Total    int         `json:"total"`         // 总测试数
	Passed   int         `json:"passed"`        // 通过数
	Failed   int         `json:"failed"`        // 失败数
	Skipped  int         `json:"skipped"`       // 跳过数
	Coverage string      `json:"coverage,omitempty"` // 覆盖率百分比
	Duration string      `json:"duration"`      // 总耗时
	Cases    []TestCase  `json:"cases"`         // 各用例详情
	Output   string      `json:"output"`        // 原始输出
}

func (e *Executor) RunTests(config TestConfig) (*TestReport, error) {
	report := &TestReport{
		Cases: make([]TestCase, 0),
	}

	args := []string{"test"}
	if config.Timeout == 0 {
		config.Timeout = 60
	}
	args = append(args, fmt.Sprintf("-timeout=%ds", config.Timeout))
	if config.Verbose {
		args = append(args, "-v")
	}
	if config.Coverage {
		args = append(args, "-coverprofile=coverage.out")
	}
	if config.Pattern != "" {
		args = append(args, config.Pattern)
	} else {
		args = append(args, "./...")
	}

	cmd := exec.Command("go", args...)
	cmd.Dir = e.workDir
	output, err := cmd.CombinedOutput()
	report.Output = strings.TrimSpace(string(output))

	if err != nil {
		fmt.Printf("[Executor] RunTests ❌ exit=%d\n", 1)
	} else {
		fmt.Printf("[Executor] RunTests ✅ (output %d chars)\n", len(report.Output))
	}

	report.Cases = parseGoTestOutput(report.Output)
	for _, tc := range report.Cases {
		switch tc.Status {
		case "pass":
			report.Passed++
		case "fail", "panic":
			report.Failed++
		case "skip":
			report.Skipped++
		}
	}
	report.Total = len(report.Cases)
	report.Success = report.Failed == 0 && report.Total > 0

	durationRe := regexp.MustCompile(`ok\s+\S+\s+([\d.]+s)`)
	if matches := durationRe.FindStringSubmatch(report.Output); len(matches) > 1 {
		report.Duration = matches[1]
	}

	coverageRe := regexp.MustCompile(`coverage:\s*([\d.]+%)`)
	if matches := coverageRe.FindStringSubmatch(report.Output); len(matches) > 1 {
		report.Coverage = matches[1]
	}

	if config.Coverage {
		coverPath := filepath.Join(e.workDir, "coverage.out")
		if _, err := os.Stat(coverPath); err == nil {
			os.Remove(coverPath)
		}
	}

	return report, nil
}

func parseGoTestOutput(output string) []TestCase {
	cases := make([]TestCase, 0)
	lines := strings.Split(output, "\n")

	passRe := regexp.MustCompile(`^--- PASS:\s+(.+?)\s+\(([\d.]+s)\)`)
	failRe := regexp.MustCompile(`^--- FAIL:\s+(.+?)\s+\(([\d.]+s)\)`)
	skipRe := regexp.MustCompile(`^--- SKIP:\s+(.+?)\s+\(([\d.]+s)\)`)
	panicRe := regexp.MustCompile(`^--- PANIC:\s+(.+?)\s+\(([\d.]+s)\)`)

	for i, line := range lines {
		line = strings.TrimSpace(line)

		var name, duration, status string
		switch {
		case passRe.MatchString(line):
			m := passRe.FindStringSubmatch(line)
			name, duration, status = m[1], m[2], "pass"
		case failRe.MatchString(line):
			m := failRe.FindStringSubmatch(line)
			name, duration, status = m[1], m[2], "fail"
		case skipRe.MatchString(line):
			m := skipRe.FindStringSubmatch(line)
			name, duration, status = m[1], m[2], "skip"
		case panicRe.MatchString(line):
			m := panicRe.FindStringSubmatch(line)
			name, duration, status = m[1], m[2], "panic"
		default:
			continue
		}

		tc := TestCase{
			Name:     name,
			Status:   status,
			Duration: duration,
		}

		if status == "fail" || status == "panic" {
			for j := i + 1; j < len(lines) && j < i+20; j++ {
				nextLine := lines[j]
				if strings.HasPrefix(nextLine, "\t") || strings.HasPrefix(nextLine, "		") {
					tc.Error += strings.TrimSpace(nextLine) + "\n"
				} else if strings.TrimSpace(nextLine) != "" && !strings.HasPrefix(strings.TrimSpace(nextLine), "---") {
					break
				}
			}
			tc.Error = strings.TrimSpace(tc.Error)
		}

		cases = append(cases, tc)
	}

	return cases
}