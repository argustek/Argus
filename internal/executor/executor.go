package executor

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"argus/internal/board"
	"argus/internal/types"
	"golang.org/x/text/encoding/simplifiedchinese"
)

type Executor struct {
	workDir        string
	boardManager   *board.Manager
	terminalOutput func(string)
	onFileWritten  func(path string)
	shellSession   *ShellSession
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
	// 重置 shell session（工作目录变了）
	if e.shellSession != nil {
		e.shellSession.Close()
		e.shellSession = nil
	}
}

// GetShellSession 获取或创建持久化 shell 会话
func (e *Executor) GetShellSession() (*ShellSession, error) {
	if e.shellSession != nil && e.shellSession.IsRunning() {
		return e.shellSession, nil
	}
	ss, err := NewShellSession(e.workDir)
	if err != nil {
		return nil, err
	}
	e.shellSession = ss
	return ss, nil
}

// ExecWithSession 在持久化 shell 中执行命令（保持 cd/env 状态）
func (e *Executor) ExecWithSession(command string, timeout time.Duration) (string, error) {
	ss, err := e.GetShellSession()
	if err != nil {
		return "", err
	}
	return ss.Exec(command, timeout)
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

	if isCodeFile(path) {
		cleaned := cleanCodeTrailingGarbage(content)
		if cleaned != content {
			fmt.Printf("[Executor] Code trailing garbage cleaned: %s\n", path)
			content = cleaned
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

func isCodeFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	codeExts := map[string]bool{
		".go": true, ".py": true, ".js": true, ".ts": true, ".java": true,
		".c": true, ".cpp": true, ".h": true, ".cs": true, ".rs": true,
		".rb": true, ".php": true, ".swift": true, ".kt": true, ".scala": true,
	}
	return codeExts[ext]
}

func cleanCodeTrailingGarbage(content string) string {
	lines := strings.Split(content, "\n")
	lastBraceIdx := -1
	for i := len(lines) - 1; i >= 0; i-- {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "}" {
			lastBraceIdx = i
			break
		}
	}
	if lastBraceIdx >= 0 && lastBraceIdx < len(lines)-1 {
		trailing := strings.Join(lines[lastBraceIdx+1:], "\n")
		if strings.TrimSpace(trailing) != "" {
			fmt.Printf("[Executor] 🧹 清理代码末尾垃圾: %q\n", trailing)
			content = strings.Join(lines[:lastBraceIdx+1], "\n")
		}
	}

	lines = strings.Split(content, "\n")
	var cleaned []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == `"` || trimmed == `'` || trimmed == `,"` || trimmed == `"'` {
			fmt.Printf("[Executor] 🧹 清理孤立引号行: %q\n", trimmed)
			continue
		}
		if trimmed == `}` && strings.Count(line, `}`) != 1 {
			cleaned = append(cleaned, line)
			continue
		}
		cleaned = append(cleaned, line)
	}
	return strings.Join(cleaned, "\n")
}

func (e *Executor) checkPythonSyntax(filePath string) string {
	cmd := exec.Command("python", "-m", "py_compile", filePath)
	cmd.Dir = e.workDir
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}

	output, err := cmd.CombinedOutput()
	if err != nil {
		lines := strings.Split(toUTF8(output), "\n")
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

	if e.workDir == "" {
		e.workDir = "."
	}

	command = e.sanitizeCommandPath(command)

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
	outputStr := toUTF8(output)
	fmt.Printf("[Executor] Output length: %d, err: %v\n", len(outputStr), err)

	if e.terminalOutput != nil {
		e.terminalOutput(fmt.Sprintf("[%s] > %s\n%s", e.workDir, command, outputStr))
	}

	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return outputStr, fmt.Errorf("command timeout after %v", timeout)
		}
		return outputStr, fmt.Errorf("command failed: %v", err)
	}

	return outputStr, nil
}

// Pre-compiled regexes for sanitizeCommandPath (avoid recompiling on every call)
var (
	reCDPath      = regexp.MustCompile(`(?i)^cd\s+["']?([^"'\s]+)["']?`)
	reGoTestFile  = regexp.MustCompile(`(?i)go\s+test\s+(?:-v\s+)?(?:-run\s+\S+\s+)?(\S+\.go)`)
	reGoRun       = regexp.MustCompile(`(?i)(go run|python|node)\s+(.+)$`)
	reBadPath     = regexp.MustCompile(`(?i)["']?(F:\\\\GithubArgus|F:/GithubArgus|C:\\\\GithubArgus|C:/GithubArgus)`)

	// Known file extensions for run commands
	runExts = map[string]bool{".go": true, ".py": true, ".js": true, ".ts": true}
)

func (e *Executor) sanitizeCommandPath(command string) string {
	originalCmd := command

	// Fix "cd:" → "cd "
	command = strings.ReplaceAll(command, "cd:", "cd ")
	command = strings.ReplaceAll(command, "CD:", "CD ")

	// Fix cd to non-existent path
	if m := reCDPath.FindStringSubmatch(command); len(m) >= 2 {
		target := strings.ReplaceAll(m[1], "/", "\\")
		if !filepath.IsAbs(target) {
			target = filepath.Join(e.workDir, target)
		}
		if abs, err := filepath.Abs(target); err == nil {
			if _, statErr := os.Stat(abs); os.IsNotExist(statErr) {
				fmt.Printf("[Executor] cd path not found: %s -> using workDir\n", target)
				command = "cd " + e.workDir
			}
		}
	}

	// Fix "go test file.go" → "go run file.go"
	if m := reGoTestFile.FindStringSubmatch(command); len(m) >= 2 {
		if !strings.HasSuffix(m[1], "_test.go") {
			fmt.Printf("[Executor] go test on non-test file %s -> go run\n", m[1])
			command = strings.Replace(command, "go test", "go run", 1)
		}
	}

	// Fix absolute paths in run commands → relative
	if m := reGoRun.FindStringSubmatch(command); len(m) >= 3 {
		args := m[2]
		fields := strings.Fields(args)
		for i, f := range fields {
			ext := filepath.Ext(f)
			if runExts[ext] && filepath.IsAbs(f) {
				base := filepath.Base(f)
				correct := filepath.Join(e.workDir, base)
				if f != correct {
					fmt.Printf("[Executor] Absolute path corrected: %s -> %s\n", f, correct)
					fields[i] = correct
				}
			}
		}
		if m[1]+" "+strings.Join(fields, " ") != command {
			command = m[1] + " " + strings.Join(fields, " ")
		}
	}

	// Fix hallucinated paths like F:/GithubArgus
	if reBadPath.MatchString(command) {
		fmt.Printf("[Executor] Hallucinated path detected! Replacing with workDir\n")
		command = reBadPath.ReplaceAllString(command, e.workDir)
	}

	if command != originalCmd {
		fmt.Printf("[Executor] Command sanitized:\n  BEFORE: %s\n  AFTER:  %s\n", originalCmd, command)
	}

	return command
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
		return fmt.Sprintf("Server started successfully on port 8080\n%s", toUTF8(output)), nil
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
	result.Output = strings.TrimSpace(toUTF8(output))

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
	report.Output = strings.TrimSpace(toUTF8(output))

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

// [P1] 智能重试策略

type ErrorCategory string

const (
	CategoryTransient ErrorCategory = "transient" // 网络超时/限流/临时故障 → 可重试
	CategoryFixable   ErrorCategory = "fixable"    // 语法/编译/导入错误 → SE可修复
	CategoryPermanent ErrorCategory = "permanent"  // 权限/致命错误 → 不可重试
)

type RetryConfig struct {
	MaxRetries    int           `json:"max_retries"`     // 最大重试次数（默认3）
	InitialDelay  time.Duration `json:"initial_delay"`   // 初始延迟（默认1s）
	MaxDelay      time.Duration `json:"max_delay"`       // 最大延迟（默认30s）
	Multiplier    float64       `json:"multiplier"`      // 延迟倍数（默认2.0）
	Jitter        bool          `json:"jitter"`          // 是否添加随机抖动
	RetryOnFixable bool         `json:"retry_on_fixable"` // 是否对 fixable 错误重试（默认false）
}

type RetryAttempt struct {
	Attempt int           `json:"attempt"`            // 第几次尝试（从1开始）
	Error   string        `json:"error,omitempty"`    // 错误信息
	Category ErrorCategory `json:"category,omitempty"` // 错误分类
	Delay   time.Duration `json:"delay,omitempty"`     // 本次延迟
	Output  string        `json:"output,omitempty"`    // 输出内容
}

type RetryResult struct {
	Success     bool            `json:"success"`       // 最终是否成功
	TotalAttempts int           `json:"total_attempts"` // 总尝试次数
	FinalOutput  string         `json:"final_output"`  // 最终输出
	FinalError   string         `json:"final_error"`   // 最终错误
	Category     ErrorCategory  `json:"category"`      // 最终错误分类
	Attempts     []RetryAttempt `json:"attempts"`      // 所有尝试记录
	TotalDelay   time.Duration  `json:"total_delay"`   // 总耗时
}

func (e *Executor) ClassifyError(stderr string) ErrorCategory {
	lower := strings.ToLower(stderr)

	if isPermissionError(stderr) || strings.Contains(lower, "fatal") ||
		strings.Contains(lower, "operation not permitted") ||
		strings.Contains(lower, "access denied") {
		return CategoryPermanent
	}

	if isSyntaxOrCompileError(stderr) || isImportError(stderr) {
		return CategoryFixable
	}

	if strings.Contains(lower, "timeout") || strings.Contains(lower, "deadline exceeded") ||
		strings.Contains(lower, "connection refused") || strings.Contains(lower, "reset by peer") ||
		strings.Contains(lower, "429 too many requests") || strings.Contains(lower, "503 service unavailable") ||
		strings.Contains(lower, "temporary failure") || strings.Contains(lower, "resource temporarily unavailable") ||
		strings.Contains(lower, "i/o timeout") || strings.Contains(lower, "context deadline") {
		return CategoryTransient
	}

	if isRuntimeError(stderr) || isTestFailure(stderr) {
		return CategoryFixable
	}

	return CategoryPermanent
}

func (e *Executor) ExecuteWithRetry(name string, args []string, config *RetryConfig) (*RetryResult, error) {
	if config == nil {
		config = &RetryConfig{
			MaxRetries:   3,
			InitialDelay: 1 * time.Second,
			MaxDelay:     30 * time.Second,
			Multiplier:   2.0,
			Jitter:       true,
		}
	}
	if config.MaxRetries == 0 {
		config.MaxRetries = 3
	}
	if config.InitialDelay == 0 {
		config.InitialDelay = 1 * time.Second
	}
	if config.MaxDelay == 0 {
		config.MaxDelay = 30 * time.Second
	}
	if config.Multiplier == 0 {
		config.Multiplier = 2.0
	}

	result := &RetryResult{
		Attempts: make([]RetryAttempt, 0),
	}

	delay := config.InitialDelay

	for attempt := 1; attempt <= config.MaxRetries+1; attempt++ {
		cmd := exec.Command(name, args...)
		cmd.Dir = e.workDir
		output, err := cmd.CombinedOutput()
		outputStr := strings.TrimSpace(toUTF8(output))

		attemptRecord := RetryAttempt{
			Attempt: attempt,
			Output:  outputStr,
		}

		if err != nil {
			attemptRecord.Error = err.Error()
			attemptRecord.Category = e.ClassifyError(outputStr)
			result.FinalError = outputStr
			result.Category = attemptRecord.Category
		} else {
			result.Success = true
			result.FinalOutput = outputStr
			result.Category = CategoryTransient
			attemptRecord.Delay = 0
			result.Attempts = append(result.Attempts, attemptRecord)
			break
		}

		result.Attempts = append(result.Attempts, attemptRecord)

		if attempt > config.MaxRetries {
			break
		}

		switch attemptRecord.Category {
		case CategoryPermanent:
			result.TotalAttempts = attempt
			result.TotalDelay = delay
			return result, nil
		case CategoryFixable:
			if !config.RetryOnFixable {
				result.TotalAttempts = attempt
				result.TotalDelay = delay
				return result, nil
			}
		}

		if config.Jitter {
			jitter := time.Duration(rand.Int63n(int64(delay) / 2))
			delay += jitter
		}
		if delay > config.MaxDelay {
			delay = config.MaxDelay
		}

		time.Sleep(delay)
		result.TotalDelay += delay
		delay = time.Duration(float64(delay) * config.Multiplier)
	}

	result.TotalAttempts = len(result.Attempts)
	return result, nil
}

// [P1] AST 级别代码修改

type ASTEditTarget struct {
	Type     string `json:"type"`               // "function" | "struct" | "var" | "const" | "import" | "method"
	Name     string `json:"name,omitempty"`      // 目标名称（如函数名、结构体名）
	File     string `json:"file,omitempty"`      // 文件路径（相对于工作目录）
	Package  string `json:"package,omitempty"`   // 包名（可选，用于跨文件定位）
	LineNum  int    `json:"line_num,omitempty"`  // 行号（备选定位方式）
}

type ASTEditOperation struct {
	Action    string          `json:"action"`             // "replace" | "insert_before" | "insert_after" | "delete"
	Target    *ASTEditTarget  `json:"target"`             // 编辑目标
	NewCode   string          `json:"new_code,omitempty"` // 新代码（replace/insert 时需要）
	OldName   string          `json:"old_name,omitempty"` // 旧名称（rename 时需要）
	NewName   string          `json:"new_name,omitempty"` // 新名称（rename 时需要）
}

type ASTEditResult struct {
	Success      bool   `json:"success"`
	Error        string `json:"error,omitempty"`
	File         string `json:"file,omitempty"`
	Action       string `json:"action,omitempty"`
	TargetType   string `json:"target_type,omitempty"`
	TargetName   string `json:"target_name,omitempty"`
	LinesChanged int    `json:"lines_changed"`
	Diff         string `json:"diff,omitempty"`
	OriginalCode string `json:"original_code,omitempty"`
	ModifiedCode string `json:"modified_code,omitempty"`
	AstValid     bool   `json:"ast_valid"`
}

type GoFileInfo struct {
	Package    string          `json:"package"`
	Imports    []string        `json:"imports"`
	Functions  []FunctionInfo  `json:"functions"`
	Structs    []StructInfo    `json:"structs"`
	Interfaces []InterfaceInfo `json:"interfaces"`
	Vars       []VarInfo       `json:"vars"`
	Constants  []ConstInfo     `json:"constants"`
	Types      []TypeInfo      `json:"types"`
	TotalLines int             `json:"total_lines"`
}

type FunctionInfo struct {
	Name       string `json:"name"`
	Receiver   string `json:"receiver,omitempty"`
	StartLine  int    `json:"start_line"`
	EndLine    int    `json:"end_line"`
	Signature  string `json:"signature"`
	IsExported bool   `json:"is_exported"`
	DocComment string `json:"doc_comment,omitempty"`
}

type StructInfo struct {
	Name      string      `json:"name"`
	StartLine int         `json:"start_line"`
	EndLine   int         `json:"end_line"`
	Fields    []FieldInfo `json:"fields"`
}

type FieldInfo struct {
	Name string `json:"name"`
	Type string `json:"type"`
	Tags string `json:"tags,omitempty"`
}

type InterfaceInfo struct {
	Name      string   `json:"name"`
	StartLine int      `json:"start_line"`
	EndLine   int      `json:"end_line"`
	Methods   []string `json:"methods"`
}

type VarInfo struct {
	Name       string `json:"name"`
	Type       string `json:"type,omitempty"`
	StartLine  int    `json:"start_line"`
	IsExported bool   `json:"is_exported"`
}

type ConstInfo struct {
	Name      string `json:"name"`
	Type      string `json:"type,omitempty"`
	Value     string `json:"value,omitempty"`
	StartLine int    `json:"start_line"`
}

type TypeInfo struct {
	Name      string `json:"name"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	Kind      string `json:"kind"`
}

func (e *Executor) ParseGoFile(filePath string) (*GoFileInfo, error) {
	var fullPath string
	if filepath.IsAbs(filePath) {
		fullPath = filePath
	} else {
		fullPath = filepath.Join(e.workDir, filePath)
	}
	if !isPathInDir(fullPath, e.workDir) {
		return nil, fmt.Errorf("path outside work directory: %s", filePath)
	}
	content, err := os.ReadFile(fullPath)
	if err != nil {
		return nil, fmt.Errorf("read file failed: %v", err)
	}
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, "", content, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse failed: %v", err)
	}
	info := &GoFileInfo{
		Package:    node.Name.Name,
		Imports:    make([]string, 0),
		Functions:  make([]FunctionInfo, 0),
		Structs:    make([]StructInfo, 0),
		Interfaces: make([]InterfaceInfo, 0),
		Vars:       make([]VarInfo, 0),
		Constants:  make([]ConstInfo, 0),
		Types:      make([]TypeInfo, 0),
	}
	lines := strings.Split(string(content), "\n")
	info.TotalLines = len(lines)
	ast.Inspect(node, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.ImportSpec:
			importPath := ""
			if x.Path != nil {
				importPath = strings.Trim(x.Path.Value, `"`)
			}
			info.Imports = append(info.Imports, importPath)
		case *ast.FuncDecl:
			fi := FunctionInfo{
				Name:      x.Name.Name,
				StartLine: fset.Position(x.Pos()).Line,
				EndLine:   fset.Position(x.End()).Line,
				IsExported: ast.IsExported(x.Name.Name),
			}
			if x.Doc != nil {
				fi.DocComment = x.Doc.Text()
			}
			if x.Recv != nil && len(x.Recv.List) > 0 {
				if se, ok := x.Recv.List[0].Type.(*ast.Ident); ok {
					fi.Receiver = se.Name
				} else if se, ok := x.Recv.List[0].Type.(*ast.StarExpr); ok {
					if ident, ok := se.X.(*ast.Ident); ok {
						fi.Receiver = "*" + ident.Name
					}
				}
			}
			fi.Signature = extractFuncSignature(x)
			info.Functions = append(info.Functions, fi)
		case *ast.GenDecl:
			if x.Tok == token.TYPE {
				for _, spec := range x.Specs {
					if ts, ok := spec.(*ast.TypeSpec); ok {
						ti := TypeInfo{Name: ts.Name.Name, StartLine: fset.Position(ts.Pos()).Line, EndLine: fset.Position(ts.End()).Line}
						switch ts.Type.(type) {
						case *ast.StructType:
							ti.Kind = "struct"
							si := StructInfo{Name: ts.Name.Name, StartLine: ti.StartLine, EndLine: ti.EndLine}
							if st, ok := ts.Type.(*ast.StructType); ok && st.Fields != nil {
								for _, field := range st.Fields.List {
									fld := FieldInfo{}
									if len(field.Names) > 0 {
										fld.Name = field.Names[0].Name
									}
									fld.Type = exprToString(field.Type)
									if field.Tag != nil {
										fld.Tags = strings.Trim(field.Tag.Value, "`")
									}
									si.Fields = append(si.Fields, fld)
								}
							}
							info.Structs = append(info.Structs, si)
						case *ast.InterfaceType:
							ti.Kind = "interface"
							ii := InterfaceInfo{Name: ts.Name.Name, StartLine: ti.StartLine, EndLine: ti.EndLine}
							if it, ok := ts.Type.(*ast.InterfaceType); ok && it.Methods != nil {
								for _, method := range it.Methods.List {
									if len(method.Names) > 0 {
										ii.Methods = append(ii.Methods, method.Names[0].Name)
									}
								}
							}
							info.Interfaces = append(info.Interfaces, ii)
						default:
							ti.Kind = "alias"
						}
						info.Types = append(info.Types, ti)
					}
				}
			} else if x.Tok == token.VAR {
				for _, spec := range x.Specs {
					if vs, ok := spec.(*ast.ValueSpec); ok {
						for _, name := range vs.Names {
							vi := VarInfo{Name: name.Name, StartLine: fset.Position(name.Pos()).Line, IsExported: ast.IsExported(name.Name)}
							if vs.Type != nil {
								vi.Type = exprToString(vs.Type)
							}
							info.Vars = append(info.Vars, vi)
						}
					}
				}
			} else if x.Tok == token.CONST {
				for _, spec := range x.Specs {
					if cs, ok := spec.(*ast.ValueSpec); ok {
						for i, name := range cs.Names {
							ci := ConstInfo{Name: name.Name, StartLine: fset.Position(name.Pos()).Line}
							if cs.Type != nil {
								ci.Type = exprToString(cs.Type)
							}
							if i < len(cs.Values) && cs.Values[i] != nil {
								if bl, ok := cs.Values[i].(*ast.BasicLit); ok {
									ci.Value = bl.Value
								}
							}
							info.Constants = append(info.Constants, ci)
						}
					}
				}
			}
		}
		return true
	})
	fmt.Printf("[Executor] ParseGoAST ✅ %s: pkg=%s funcs=%d structs=%d\n", filePath, info.Package, len(info.Functions), len(info.Structs))
	return info, nil
}

func (e *Executor) EditFileWithAST(op *ASTEditOperation) (*ASTEditResult, error) {
	result := &ASTEditResult{Action: op.Action}
	if op.Target == nil {
		result.Error = "target is required"
		return result, nil
	}
	result.TargetType = op.Target.Type
	result.TargetName = op.Target.Name
	result.File = op.Target.File
	var fullPath string
	if filepath.IsAbs(op.Target.File) {
		fullPath = op.Target.File
	} else {
		fullPath = filepath.Join(e.workDir, op.Target.File)
	}
	if !isPathInDir(fullPath, e.workDir) {
		result.Error = fmt.Sprintf("path outside work directory: %s", op.Target.File)
		return result, nil
	}
	content, err := os.ReadFile(fullPath)
	if err != nil {
		result.Error = fmt.Sprintf("read file failed: %v", err)
		return result, nil
	}
	original := string(content)
	lines := strings.Split(original, "\n")
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, "", content, parser.ParseComments)
	if err != nil {
		result.Error = fmt.Sprintf("parse failed (fallback to text mode): %v", err)
		return e.fallbackTextEdit(op, original, fullPath, result)
	}
	targetPos, targetEnd, found := findASTNode(fset, node, op.Target)
	if !found {
		result.Error = fmt.Sprintf("target not found: type=%s name=%s", op.Target.Type, op.Target.Name)
		return result, nil
	}
	startLine := fset.Position(targetPos).Line - 1
	endLine := fset.Position(targetEnd).Line
	result.OriginalCode = strings.Join(lines[startLine:endLine], "\n")
	newContent := original
	switch op.Action {
	case "replace":
		before := lines[:startLine]
		after := lines[endLine:]
		newLines := append(before, strings.Split(op.NewCode, "\n")...)
		newLines = append(newLines, after...)
		newContent = strings.Join(newLines, "\n")
		result.ModifiedCode = op.NewCode
	case "delete":
		before := lines[:startLine]
		after := lines[endLine:]
		newContent = strings.Join(append(before, after...), "\n")
		result.ModifiedCode = ""
	case "insert_before":
		before := lines[:startLine]
		after := lines[startLine:]
		insertLines := strings.Split(op.NewCode, "\n")
		newContent = strings.Join(append(append(before, insertLines...), after...), "\n")
	case "insert_after":
		before := lines[:endLine]
		after := lines[endLine:]
		insertLines := strings.Split(op.NewCode, "\n")
		newContent = strings.Join(append(append(before, insertLines...), after...), "\n")
	default:
		result.Error = fmt.Sprintf("unsupported action: %s", op.Action)
		return result, nil
	}
	diff := generateDiff(original, newContent, op.Target.File)
	result.Diff = diff
	result.LinesChanged = countASTLinesChanged(original, newContent)
	if err := os.WriteFile(fullPath, []byte(newContent), 0644); err != nil {
		result.Error = fmt.Sprintf("write file failed: %v", err)
		return result, nil
	}
	fset2 := token.NewFileSet()
	_, parseErr := parser.ParseFile(fset2, "", []byte(newContent), parser.ParseComments)
	result.AstValid = (parseErr == nil)
	if parseErr != nil {
		fmt.Printf("[Executor] EditFileWithAST ⚠️ syntax check failed: %v\n", parseErr)
	} else {
		fmt.Printf("[Executor] EditFileWithAST ✅ action=%s target=%s/%s lines=%d\n", op.Action, op.Target.Type, op.Target.Name, result.LinesChanged)
	}
	result.Success = true
	return result, nil
}

func (e *Executor) fallbackTextEdit(op *ASTEditOperation, original, fullPath string, result *ASTEditResult) (*ASTEditResult, error) {
	if op.Action == "replace" && op.Target.Name != "" {
		pattern := op.Target.Name
		switch op.Target.Type {
		case "function":
			pattern = "func " + op.Target.Name
		case "struct":
			pattern = "type " + op.Target.Name + " struct"
		case "interface":
			pattern = "type " + op.Target.Name + " interface"
		}
		if strings.Contains(original, pattern) {
			idx := strings.Index(original, pattern)
			before := original[:idx]
			after := original[idx:]
			endIdx := findBlockEnd(after)
			afterBlock := after[endIdx:]
			newContent := before + op.NewCode + afterBlock
			diff := generateDiff(original, newContent, op.Target.File)
			result.Diff = diff
			result.LinesChanged = countASTLinesChanged(original, newContent)
			if err := os.WriteFile(fullPath, []byte(newContent), 0644); err != nil {
				result.Error = fmt.Sprintf("write file failed: %v", err)
				return result, nil
			}
			result.Success = true
			result.AstValid = false
			return result, nil
		}
	}
	result.Error = "text fallback also failed: cannot find target"
	return result, nil
}

func findASTNode(fset *token.FileSet, node ast.Node, target *ASTEditTarget) (token.Pos, token.Pos, bool) {
	var foundPos, foundEnd token.Pos
	found := false
	ast.Inspect(node, func(n ast.Node) bool {
		if found {
			return false
		}
		match := false
		switch target.Type {
		case "function":
			if fn, ok := n.(*ast.FuncDecl); ok && fn.Name.Name == target.Name {
				match = true
			}
		case "method":
			if fn, ok := n.(*ast.FuncDecl); ok && fn.Recv != nil && len(fn.Recv.List) > 0 && fn.Name.Name == target.Name {
				match = true
			}
		case "struct":
			if ts, ok := n.(*ast.TypeSpec); ok && ts.Name.Name == target.Name {
				if _, isStruct := ts.Type.(*ast.StructType); isStruct {
					match = true
				}
			}
		case "interface":
			if ts, ok := n.(*ast.TypeSpec); ok && ts.Name.Name == target.Name {
				if _, isIface := ts.Type.(*ast.InterfaceType); isIface {
					match = true
				}
			}
		case "var":
			if gs, ok := n.(*ast.GenDecl); ok && gs.Tok == token.VAR {
				for _, spec := range gs.Specs {
					if vs, ok := spec.(*ast.ValueSpec); ok {
						for _, name := range vs.Names {
							if name.Name == target.Name { match = true; break }
						}
					}
				}
			}
		case "const":
			if gs, ok := n.(*ast.GenDecl); ok && gs.Tok == token.CONST {
				for _, spec := range gs.Specs {
					if cs, ok := spec.(*ast.ValueSpec); ok {
						for _, name := range cs.Names {
							if name.Name == target.Name { match = true; break }
						}
					}
				}
			}
		case "import":
			if is, ok := n.(*ast.ImportSpec); ok {
				path := ""
				if is.Path != nil { path = strings.Trim(is.Path.Value, `"`) }
				if path == target.Name || strings.HasSuffix(path, target.Name) {
					match = true
				}
			}
		case "type":
			if ts, ok := n.(*ast.TypeSpec); ok && ts.Name.Name == target.Name {
				match = true
			}
		}
		if match {
			foundPos = n.Pos()
			foundEnd = n.End()
			found = true
			return false
		}
		return true
	})
	return foundPos, foundEnd, found
}

func findBlockEnd(s string) int {
	braceCount := 0
	inString := false
	stringChar := rune(0)
	for i, ch := range s {
		if inString {
			if ch == stringChar { inString = false }
			continue
		}
		if ch == '"' || ch == '`' || ch == '\'' {
			inString = true
			stringChar = ch
			continue
		}
		if ch == '{' { braceCount++ } else if ch == '}' {
			braceCount--
			if braceCount == 0 { return i + 1 }
		}
	}
	return len(s)
}

func generateDiff(old, new, filename string) string {
	oldLines := strings.Split(old, "\n")
	newLines := strings.Split(new, "\n")
	var buf strings.Builder
	buf.WriteString(fmt.Sprintf("--- a/%s\n", filename))
	buf.WriteString(fmt.Sprintf("+++ b/%s\n", filename))
	maxLen := len(oldLines)
	if len(newLines) > maxLen { maxLen = len(newLines) }
	for i := 0; i < maxLen; i++ {
		oldLine := ""
		newLine := ""
		if i < len(oldLines) { oldLine = oldLines[i] }
		if i < len(newLines) { newLine = newLines[i] }
		if oldLine == newLine {
			buf.WriteString(fmt.Sprintf(" %s\n", oldLine))
		} else {
			if oldLine != "" && i < len(oldLines) { buf.WriteString(fmt.Sprintf("-%s\n", oldLine)) }
			if newLine != "" && i < len(newLines) { buf.WriteString(fmt.Sprintf("+%s\n", newLine)) }
		}
	}
	return buf.String()
}

func countASTLinesChanged(old, new string) int {
	oldLines := strings.Split(old, "\n")
	newLines := strings.Split(new, "\n")
	changes := abs(len(newLines) - len(oldLines))
	minLen := min(len(oldLines), len(newLines))
	for i := 0; i < minLen; i++ {
		if oldLines[i] != newLines[i] { changes++ }
	}
	return changes
}

func extractFuncSignature(fd *ast.FuncDecl) string {
	var buf strings.Builder
	if fd.Recv != nil {
		buf.WriteByte('(')
		for i, field := range fd.Recv.List {
			if i > 0 { buf.WriteString(", ") }
			if len(field.Names) > 0 { buf.WriteString(field.Names[0].Name); buf.WriteByte(' ') }
			buf.WriteString(exprToString(field.Type))
		}
		buf.WriteString(") ")
	}
	buf.WriteString(fd.Name.Name)
	buf.WriteByte('(')
	if fd.Type.Params != nil {
		for i, field := range fd.Type.Params.List {
			if i > 0 { buf.WriteString(", ") }
			if len(field.Names) > 0 {
				for j, name := range field.Names {
					if j > 0 { buf.WriteString(", ") }
					buf.WriteString(name.Name)
				}
				buf.WriteByte(' ')
			}
			buf.WriteString(exprToString(field.Type))
		}
	}
	buf.WriteByte(')')
	if fd.Type.Results != nil && len(fd.Type.Results.List) > 0 {
		buf.WriteByte(' ')
		if len(fd.Type.Results.List) > 1 { buf.WriteByte('(') }
		for i, field := range fd.Type.Results.List {
			if i > 0 { buf.WriteString(", ") }
			if len(field.Names) > 0 {
				for j, name := range field.Names {
					if j > 0 { buf.WriteString(", ") }
					buf.WriteString(name.Name)
					buf.WriteByte(' ')
				}
			}
			buf.WriteString(exprToString(field.Type))
		}
		if len(fd.Type.Results.List) > 1 { buf.WriteByte(')') }
	}
	return buf.String()
}

func exprToString(expr ast.Expr) string {
	if expr == nil { return "" }
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.StarExpr:
		return "*" + exprToString(e.X)
	case *ast.SelectorExpr:
		return fmt.Sprintf("%s.%s", exprToString(e.X), e.Sel.Name)
	case *ast.ArrayType:
		return fmt.Sprintf("[]%s", exprToString(e.Elt))
	case *ast.MapType:
		return fmt.Sprintf("map[%s]%s", exprToString(e.Key), exprToString(e.Value))
	case *ast.ChanType:
		dir := "chan "
		if e.Dir == ast.SEND { dir = "chan<- " } else if e.Dir == ast.RECV { dir = "<-chan " }
		return dir + exprToString(e.Value)
	case *ast.FuncType:
		return extractFuncType(e)
	case *ast.InterfaceType:
		return "interface{}"
	case *ast.Ellipsis:
		return fmt.Sprintf("...%s", exprToString(e.Elt))
	case *ast.ParenExpr:
		return fmt.Sprintf("(%s)", exprToString(e.X))
	default:
		return fmt.Sprintf("%T", expr)[5:]
	}
}

func extractFuncType(ft *ast.FuncType) string {
	var buf strings.Builder
	buf.WriteString("func(")
	if ft.Params != nil {
		for i, field := range ft.Params.List {
			if i > 0 { buf.WriteString(", ") }
			buf.WriteString(exprToString(field.Type))
		}
	}
	buf.WriteByte(')')
	if ft.Results != nil && len(ft.Results.List) > 0 {
		if len(ft.Results.List) == 1 && ft.Results.List[0].Names == nil {
			buf.WriteByte(' ')
			buf.WriteString(exprToString(ft.Results.List[0].Type))
		} else {
			buf.WriteString(" (")
			for i, field := range ft.Results.List {
				if i > 0 { buf.WriteString(", ") }
				buf.WriteString(exprToString(field.Type))
			}
			buf.WriteByte(')')
		}
	}
	return buf.String()
}

func abs(x int) int {
	if x < 0 { return -x }
	return x
}

// [P1] 多文件上下文理解 - 依赖分析与影响范围

type DependencyInfo struct {
	File        string   `json:"file"`                  // 文件路径（相对于工作目录）
	Package     string   `json:"package"`               // 包名
	Imports     []string `json:"imports"`               // 直接导入的包
	DependedBy  []string `json:"depended_by,omitempty"` // 被哪些文件导入
	Functions   []string `json:"functions,omitempty"`   // 导出的函数
	Types       []string `json:"types,omitempty"`       // 导出的类型
	IsTestFile  bool     `json:"is_test_file"`
	Complexity  int      `json:"complexity"`             // 复杂度估算（行数/100）
}

type ImpactScope struct {
	TargetFile    string          `json:"target_file"`              // 目标文件
	TargetType    string          `json:"target_type"`              // 目标类型 (function/struct/var)
	TargetName    string          `json:"target_name"`              // 目标名称
	DirectImpact  []string        `json:"direct_impact"`            // 直接影响的文件
	IndirectImpact []string       `json:"indirect_impact"`          // 间接影响的文件
	TestFiles     []string        `json:"test_files"`              // 相关测试文件
	RiskLevel     string          `json:"risk_level"`              // low/medium/high/critical
	Reason        string          `json:"reason"`                  // 风险原因
	Suggestions   []string        `json:"suggestions,omitempty"`   // 建议
}

type CodeContext struct {
	Files         []*GoFileInfo    `json:"files"`           // 解析的所有文件
	Dependencies  []*DependencyInfo `json:"dependencies"`    // 依赖关系图
	TotalFiles    int               `json:"total_files"`     // 总文件数
	TotalFuncs    int               `json:"total_funcs"`     // 总函数数
	Packages      map[string]int    `json:"packages"`        // 包分布
}

func (e *Executor) AnalyzeDependencies(pattern string) ([]*DependencyInfo, error) {
	var fullPath string
	if filepath.IsAbs(pattern) {
		fullPath = pattern
	} else {
		fullPath = filepath.Join(e.workDir, pattern)
	}
	matches, err := filepath.Glob(filepath.Join(fullPath, "*.go"))
	if err != nil {
		return nil, fmt.Errorf("glob pattern failed: %v", err)
	}
	deps := make([]*DependencyInfo, 0, len(matches))
	for _, file := range matches {
		relPath, _ := filepath.Rel(e.workDir, file)
		info, parseErr := e.ParseGoFile(relPath)
		if parseErr != nil {
			continue
		}
		exportedFuncs := make([]string, 0)
		for _, fn := range info.Functions {
			if fn.IsExported { exportedFuncs = append(exportedFuncs, fn.Name) }
		}
		exportedTypes := make([]string, 0)
		for _, ti := range info.Types {
			if ast.IsExported(ti.Name) { exportedTypes = append(exportedTypes, ti.Name) }
		}
		dep := &DependencyInfo{
			File:       relPath,
			Package:    info.Package,
			Imports:    info.Imports,
			Functions:  exportedFuncs,
			Types:      exportedTypes,
			IsTestFile: strings.HasSuffix(relPath, "_test.go"),
			Complexity:  info.TotalLines / 100,
		}
		deps = append(deps, dep)
	}
	for i, dep := range deps {
		importers := make([]string, 0)
		pkgPath := filepath.Dir(dep.File)
		for _, other := range deps {
			if other.File == dep.File { continue }
			for _, imp := range other.Imports {
				if strings.HasSuffix(imp, pkgPath) || strings.HasSuffix(imp, dep.Package) {
					importers = append(importers, other.File)
					break
				}
			}
		}
		deps[i].DependedBy = importers
	}
	fmt.Printf("[Executor] AnalyzeDependencies ✅ pattern=%s files=%d\n", pattern, len(deps))
	return deps, nil
}

func (e *Executor) AnalyzeImpact(targetFile, targetType, targetName string) (*ImpactScope, error) {
	var relPath string
	if filepath.IsAbs(targetFile) {
		relPath, _ = filepath.Rel(e.workDir, targetFile)
	} else {
		relPath = targetFile	}
	dir := filepath.Dir(relPath)
	deps, err := e.AnalyzeDependencies(dir)
	if err != nil {
		return nil, fmt.Errorf("analyze dependencies failed: %v", err)
	}
	scope := &ImpactScope{
		TargetFile: relPath,
		TargetType: targetType,
		TargetName: targetName,
		DirectImpact:  make([]string, 0),
		IndirectImpact: make([]string, 0),
		TestFiles:      make([]string, 0),
		RiskLevel:     "low",
	}
	targetDep := findDependency(deps, relPath)
	if targetDep != nil {
		scope.DirectImpact = append(scope.DirectImpact, targetDep.DependedBy...)
		for _, importer := range targetDep.DependedBy {
			importerDep := findDependency(deps, importer)
			if importerDep != nil {
				for _, indirect := range importerDep.DependedBy {
					if !containsStr(scope.DirectImpact, indirect) && !containsStr(scope.IndirectImpact, indirect) && indirect != relPath {
						scope.IndirectImpact = append(scope.IndirectImpact, indirect)
					}
				}
			}
		}
	}
	for _, dep := range deps {
		if dep.IsTestFile && (dep.File == relPath+"_test" || strings.HasPrefix(dep.File, dir)) {
			scope.TestFiles = append(scope.TestFiles, dep.File)
		}
	}
	totalAffected := len(scope.DirectImpact) + len(scope.IndirectImpact)
	isExported := ast.IsExported(targetName)
	switch {
	case totalAffected > 10 || (isExported && totalAffected > 5):
		scope.RiskLevel = "critical"
		scope.Reason = "Highly connected exported symbol affecting many files"
	case totalAffected > 5 || isExported:
		scope.RiskLevel = "high"
		scope.Reason = "Exported symbol or moderate impact scope"
	case totalAffected > 2:
		scope.RiskLevel = "medium"
		scope.Reason = "Limited impact on few files"
	default:
		scope.RiskLevel = "low"
		scope.Reason = "Localized change with minimal dependencies"
	}
	switch scope.RiskLevel {
	case "critical", "high":
		scope.Suggestions = append(scope.Suggestions, "Run full test suite before and after changes")
		scope.Suggestions = append(scope.Suggestions, "Consider backward compatibility")
		scope.Suggestions = append(scope.Suggestions, "Review all dependent files")
	case "medium":
		scope.Suggestions = append(scope.Suggestions, "Run affected test files")
		scope.Suggestions = append(scope.Suggestions, "Check direct dependents for breakage")
	default:
		scope.Suggestions = append(scope.Suggestions, "Run local tests")
	}
	fmt.Printf("[Executor] AnalyzeImpact ✅ target=%s risk=%s direct=%d indirect=%d\n", targetName, scope.RiskLevel, len(scope.DirectImpact), len(scope.IndirectImpact))
	return scope, nil
}

func (e *Executor) BuildCodeContext(pattern string) (*CodeContext, error) {
	files, err := filepath.Glob(filepath.Join(e.workDir, pattern, "*.go"))
	if err != nil {
		return nil, fmt.Errorf("glob failed: %v", err)
	}
	ctx := &CodeContext{
		Files:      make([]*GoFileInfo, 0),
		Packages:   make(map[string]int),
	}
	for _, file := range files {
		relPath, _ := filepath.Rel(e.workDir, file)
		info, parseErr := e.ParseGoFile(relPath)
		if parseErr != nil {
			continue
		}
		ctx.Files = append(ctx.Files, info)
		ctx.TotalFuncs += len(info.Functions)
		ctx.Packages[info.Package]++
	}
	ctx.TotalFiles = len(ctx.Files)
	deps, _ := e.AnalyzeDependencies(pattern)
	ctx.Dependencies = deps
	fmt.Printf("[Executor] BuildCodeContext ✅ files=%d funcs=%d pkgs=%d\n", ctx.TotalFiles, ctx.TotalFuncs, len(ctx.Packages))
	return ctx, nil
}

func findDependency(deps []*DependencyInfo, file string) *DependencyInfo {
	for _, d := range deps {
		if d.File == file { return d }
	}
	return nil
}

func containsStr(slice []string, s string) bool {
	for _, item := range slice {
		if item == s { return true }
	}
	return false
}

func toUTF8(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	if isValidUTF8(raw) {
		return string(raw)
	}
	decoder := simplifiedchinese.GBK.NewDecoder()
	result, err := decoder.Bytes(raw)
	if err != nil {
		return string(raw)
	}
	return string(result)
}

func isValidUTF8(b []byte) bool {
	for i := 0; i < len(b); {
		if b[i] < 0x80 {
			i++
			continue
		}
		_, size := decodeUTF8Rune(b[i:])
		if size == 0 {
			return false
		}
		i += size
	}
	return true
}

func decodeUTF8Rune(b []byte) (rune, int) {
	if len(b) == 0 {
		return 0, 0
	}
	lead := b[0]
	switch {
	case lead < 0x80:
		return rune(lead), 1
	case lead < 0xC0:
		return 0, 0
	case lead < 0xE0:
		if len(b) < 2 || b[1]&0xC0 != 0x80 {
			return 0, 0
		}
		return rune(b[0]&0x1F)<<6 | rune(b[1]&0x3F), 2
	case lead < 0xF0:
		if len(b) < 3 || b[1]&0xC0 != 0x80 || b[2]&0xC0 != 0x80 {
			return 0, 0
		}
		return rune(b[0]&0x0F)<<12 | rune(b[1]&0x3F)<<6 | rune(b[2]&0x3F), 3
	default:
		if len(b) < 4 || b[1]&0xC0 != 0x80 || b[2]&0xC0 != 0x80 || b[3]&0xC0 != 0x80 {
			return 0, 0
		}
		return rune(b[0]&0x07)<<18 | rune(b[1]&0x3F)<<12 | rune(b[2]&0x3F)<<6 | rune(b[3]&0x3F), 4
	}
}