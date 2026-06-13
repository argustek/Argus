package ai

import (
	"bufio"
	"context"
	"encoding/json"
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

// LSPClient LSP 客户端，管理 LSP daemon 通信
// 支持 Go/TypeScript/Python/Rust/C/C++ 多种语言
type LSPClient struct {
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	stdout   *bufio.Reader
	ctx      context.Context
	cancel   context.CancelFunc
	mu       sync.RWMutex
	rootURI  string
	initDone bool
	reqID    int64
	pending  map[int64]chan *LSPResponse
	workDir  string
}

// LSPPosition 文件中的位置（0-based）
type LSPPosition struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

// LSPLocation LSP 返回的位置信息
type LSPLocation struct {
	URI   string    `json:"uri"`
	Range LSPLRange `json:"range"`
}

// LSPLRange 范围
type LSPLRange struct {
	Start LSPPosition `json:"start"`
	End   LSPPosition `json:"end"`
}

// LSPResponse 通用响应
type LSPResponse struct {
	ID     int64           `json:"id,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// HoverResult hover 响应
type HoverResult struct {
	Contents interface{} `json:"contents"` // string | MarkupContent
	Range    *LSPLRange  `json:"range,omitempty"`
}

// LocationList 位置列表
type LocationList []LSPLocation

// DiagnosticInfo 诊断信息
type DiagnosticInfo struct {
	URI         string       `json:"uri"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

// Diagnostic 单个诊断项
type Diagnostic struct {
	Range    LSPLRange `json:"range"`
	Severity int       `json:"severity"` // 1=Error, 2=Warning, 3=Info, 4=Hint
	Code     string    `json:"code,omitempty"`
	Source   string    `json:"source,omitempty"`
	Message  string    `json:"message"`
}

// TextEdit 文本编辑
type TextEdit struct {
	Range   LSPLRange `json:"range"`
	NewText string    `json:"newText"`
}

// WorkspaceEdit 工作区编辑（rename 用）
type WorkspaceEdit struct {
	Changes map[string][]TextEdit `json:"changes,omitempty"` // URI -> edits
}

// LSPServerConfig LSP 服务器配置
type LSPServerConfig struct {
	Name    string   // 语言服务器名称
	Command string   // 启动命令
	Args    []string // 启动参数
}

// lspServerMap 语言 → LSP 服务器配置
var lspServerMap = map[string]LSPServerConfig{
	"go":         {Name: "gopls", Command: "gopls", Args: []string{"serve"}},
	"typescript": {Name: "typescript-language-server", Command: "typescript-language-server", Args: []string{"--stdio"}},
	"javascript": {Name: "typescript-language-server", Command: "typescript-language-server", Args: []string{"--stdio"}},
	"python":     {Name: "pylsp", Command: "pylsp", Args: nil},
	"rust":       {Name: "rust-analyzer", Command: "rust-analyzer", Args: nil},
	"c":          {Name: "clangd", Command: "clangd", Args: nil},
	"cpp":        {Name: "clangd", Command: "clangd", Args: nil},
}

// DetectCodeLanguage 从工作目录检测主要编程语言
func DetectCodeLanguage(workDir string) string {
	extCount := map[string]int{}
	_ = filepath.WalkDir(workDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext != "" {
			extCount[ext]++
		}
		return nil
	})

	langByExt := map[string]string{
		".go": "go", ".ts": "typescript", ".tsx": "typescript",
		".js": "javascript", ".jsx": "javascript",
		".py": "python", ".rs": "rust",
		".c": "c", ".cpp": "cpp", ".cc": "cpp", ".h": "c", ".hpp": "cpp",
	}

	bestLang := "go" // 默认 Go
	bestCount := 0
	for ext, count := range extCount {
		if lang, ok := langByExt[ext]; ok && count > bestCount {
			bestCount = count
			bestLang = lang
		}
	}
	return bestLang
}

// SupportedLanguages 返回支持的 LSP 语言列表
func SupportedLanguages() []string {
	return []string{"go", "typescript", "javascript", "python", "rust", "c", "cpp"}
}

// NewLSPClient 创建 LSP 客户端（自动检测语言）
func NewLSPClient(workDir string) (*LSPClient, error) {
	lang := DetectCodeLanguage(workDir)
	return NewLSPClientForLang(workDir, lang)
}

// NewLSPClientForLang 按指定语言创建 LSP 客户端
func NewLSPClientForLang(workDir, lang string) (*LSPClient, error) {
	config, ok := lspServerMap[lang]
	if !ok {
		return nil, fmt.Errorf("unsupported language: %s (supported: %v)", lang, SupportedLanguages())
	}

	ctx, cancel := context.WithCancel(context.Background())

	absDir, err := filepath.Abs(workDir)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("abs path: %w", err)
	}

	// 检查 LSP 服务器是否可用
	if _, err := exec.LookPath(config.Command); err != nil {
		cancel()
		return nil, fmt.Errorf("%s not found in PATH: %w (install: go install %s@latest)", config.Name, err, installHint(config.Name))
	}

	client := &LSPClient{
		ctx:     ctx,
		cancel:  cancel,
		rootURI: fileToURI(absDir),
		workDir: absDir,
		pending: make(map[int64]chan *LSPResponse),
	}

	// 启动 LSP 服务器
	client.cmd = exec.CommandContext(ctx, config.Command, config.Args...)
	client.cmd.Dir = absDir
	client.cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}

	stdin, err := client.cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	client.stdin = stdin

	stdout, err := client.cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	client.stdout = bufio.NewReader(stdout)

	if err := client.cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start %s: %w", config.Name, err)
	}

	// 启动读循环
	go client.readLoop()

	// 初始化 LSP
	if err := client.initialize(); err != nil {
		client.Close()
		return nil, fmt.Errorf("init: %w", err)
	}

	fmt.Printf("[LSP] ✅ %s daemon started (lang=%s, root=%s)\n", config.Name, lang, absDir)
	return client, nil
}

func installHint(name string) string {
	switch name {
	case "gopls":
		return "golang.org/x/tools/gopls"
	case "typescript-language-server":
		return "npm: typescript-language-server"
	case "pylsp":
		return "pip: python-lsp-server"
	case "rust-analyzer":
		return "rustup: rust-analyzer"
	case "clangd":
		return "apt/brew: clangd"
	}
	return name
}

// fileToURI 将文件路径转为 file:// URI
func fileToURI(path string) string {
	path = filepath.ToSlash(path)
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return "file://" + path
}

// initialize 发送 initialize 请求
func (c *LSPClient) initialize() error {
	params := map[string]interface{}{
		"processId": nil,
		"rootUri":   c.rootURI,
		"capabilities": map[string]interface{}{
			"textDocument": map[string]interface{}{
				"hover":          map[string]interface{}{"contentFormat": []string{"plaintext", "markdown"}},
				"definition":     map[string]interface{}{},
				"references":     map[string]interface{}{},
				"rename":         map[string]interface{}{"prepareSupport": true},
				"documentSymbol": map[string]interface{}{},
			},
			"workspace": map[string]interface{}{
				"workspaceFolders": []map[string]string{
					{"uri": c.rootURI, "name": filepath.Base(c.workDir)},
				},
			},
		},
	}

	_, err := c.call("initialize", params)
	if err != nil {
		return err
	}

	// 发送 initialized 通知
	c.notify("initialized", map[string]interface{}{})
	c.initDone = true
	return nil
}

// call 发送请求并等待响应
func (c *LSPClient) call(method string, params interface{}) (*LSPResponse, error) {
	c.mu.Lock()
	c.reqID++
	id := c.reqID
	ch := make(chan *LSPResponse, 1)
	c.pending[id] = ch
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}()

	// 构造 JSON-RPC 请求
	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	}
	data, _ := json.Marshal(req)

	// 写入 Content-Length header + body
	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(data))
	if _, err := c.stdin.Write([]byte(header)); err != nil {
		return nil, fmt.Errorf("write header: %w", err)
	}
	if _, err := c.stdin.Write(data); err != nil {
		return nil, fmt.Errorf("write body: %w", err)
	}

	// 等待响应（30s超时）
	select {
	case resp := <-ch:
		if resp.Error != nil {
			return nil, fmt.Errorf("LSP error [%d]: %s", resp.Error.Code, resp.Error.Message)
		}
		return resp, nil
	case <-time.After(30 * time.Second):
		return nil, fmt.Errorf("timeout waiting for %s response", method)
	case <-c.ctx.Done():
		return nil, fmt.Errorf("context cancelled")
	}
}

// notify 发送通知（不需要响应）
func (c *LSPClient) notify(method string, params interface{}) error {
	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
	}
	data, _ := json.Marshal(req)
	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(data))
	if _, err := c.stdin.Write([]byte(header)); err != nil {
		return err
	}
	_, err := c.stdin.Write(data)
	return err
}

// readLoop 读取 LSP daemon 输出
func (c *LSPClient) readLoop() {
	reader := bufio.NewReader(c.stdout)
	for {
		select {
		case <-c.ctx.Done():
			return
		default:
		}

		// 读取 Content-Length header
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "Content-Length:") {
			continue
		}

		var length int
		fmt.Sscanf(line, "Content-Length: %d", &length)

		// 跳过空行
		reader.ReadString('\n')

		// 读取 body
		body := make([]byte, length)
		io.ReadFull(reader, body)

		var resp LSPResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			continue
		}

		// 分发到等待的 channel
		if resp.ID != 0 {
			c.mu.RLock()
			ch, ok := c.pending[resp.ID]
			c.mu.RUnlock()
			if ok {
				ch <- &resp
			}
		}
		// 通知类消息（如 diagnostics）忽略
	}
}

// Close 关闭 LSP daemon
func (c *LSPClient) Close() {
	c.cancel()
	if c.cmd != nil && c.cmd.Process != nil {
		c.cmd.Process.Kill()
	}
	fmt.Println("[LSP] daemon stopped")
}

// ========== 核心 LSP 操作 ==========

// GoToDefinition 跳转到定义
func (c *LSPClient) GoToDefinition(filePath string, line, col int) ([]LSPLocation, error) {
	uri := fileToURI(filePath)
	pos := LSPPosition{Line: line, Character: col}

	resp, err := c.call("textDocument/definition", map[string]interface{}{
		"textDocument": map[string]string{"uri": uri},
		"position":     pos,
	})
	if err != nil {
		return nil, err
	}

	var result LocationList
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		// 可能是单个 location（不是数组）
		var single LSPLocation
		if err2 := json.Unmarshal(resp.Result, &single); err2 == nil {
			return []LSPLocation{single}, nil
		}
		return nil, fmt.Errorf("parse result: %w", err)
	}
	return result, nil
}

// FindReferences 查找所有引用
func (c *LSPClient) FindReferences(filePath string, line, col int) ([]LSPLocation, error) {
	uri := fileToURI(filePath)
	pos := LSPPosition{Line: line, Character: col}

	resp, err := c.call("textDocument/references", map[string]interface{}{
		"textDocument": map[string]interface{}{
			"uri": uri,
		},
		"position": pos,
		"context": map[string]bool{
			"includeDeclaration": true,
		},
	})
	if err != nil {
		return nil, err
	}

	var result LocationList
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("parse result: %w", err)
	}
	return result, nil
}

// Hover 获取悬停信息（类型签名、文档）
func (c *LSPClient) Hover(filePath string, line, col int) (string, error) {
	uri := fileToURI(filePath)
	pos := LSPPosition{Line: line, Character: col}

	resp, err := c.call("textDocument/hover", map[string]interface{}{
		"textDocument": map[string]string{"uri": uri},
		"position":     pos,
	})
	if err != nil {
		return "", err
	}

	var result HoverResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		// 可能是纯字符串
		var s string
		if err2 := json.Unmarshal(resp.Result, &s); err2 == nil {
			return s, nil
		}
		return "", fmt.Errorf("parse result: %w", err)
	}

	switch v := result.Contents.(type) {
	case string:
		return v, nil
	case map[string]interface{}:
		if value, ok := v["value"].(string); ok {
			return value, nil
		}
	}
	return "", nil
}

// Diagnostics 获取文件诊断（编译错误、类型错误等）
// 注意：diagnostics 通常通过 push 通知发送，这里用 textDocument/codeAction 触发刷新
func (c *LSPClient) Diagnostics(filePath string) ([]Diagnostic, error) {
	// 先打开文件确保 LSP 有缓存
	c.notify("textDocument/didOpen", map[string]interface{}{
		"textDocument": map[string]interface{}{
			"uri":        fileToURI(filePath),
			"languageId": "go",
			"version":    0,
		},
	})

	// 等待诊断推送
	time.Sleep(500 * time.Millisecond)

	// 通过 pull diagnostics API 获取
	resp, err := c.call("textDocument/diagnostic", map[string]interface{}{
		"textDocument": map[string]interface{}{
			"uri": fileToURI(filePath),
		},
	})
	if err != nil {
		// LSP 不支持 pull diagnostics，返回空
		return []Diagnostic{}, nil
	}

	var diagResp struct {
		Kind        string       `json:"kind"`
		ResultID    string       `json:"resultId"`
		Diagnostics []Diagnostic `json:"items"`
	}
	if err := json.Unmarshal(resp.Result, &diagResp); err != nil {
		return nil, fmt.Errorf("parse diagnostics: %w", err)
	}
	return diagResp.Diagnostics, nil
}

// Rename 重命名符号（跨文件安全重命名）
func (c *LSPClient) Rename(filePath string, line, col int, newName string) (*WorkspaceEdit, error) {
	uri := fileToURI(filePath)
	pos := LSPPosition{Line: line, Character: col}

	resp, err := c.call("textDocument/rename", map[string]interface{}{
		"textDocument": map[string]string{"uri": uri},
		"position":     pos,
		"newName":      newName,
	})
	if err != nil {
		return nil, err
	}

	var result WorkspaceEdit
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("parse rename result: %w", err)
	}
	return &result, nil
}

// ApplyWorkspaceEdit 应用重命名编辑结果
func ApplyWorkspaceEdit(edit *WorkspaceEdit, workDir string) ([]string, error) {
	var changedFiles []string
	for uri, textEdits := range edit.Changes {
		// URI -> file path
		filePath := strings.TrimPrefix(uri, "file://")
		filePath = strings.ReplaceAll(filePath, "/", string(filepath.Separator))

		content, err := os.ReadFile(filePath)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", filePath, err)
		}

		lines := strings.Split(string(content), "\n")

		// 从后往前应用 edits（避免位置偏移）
		for i := len(textEdits) - 1; i >= 0; i-- {
			edit := textEdits[i]
			startLine := edit.Range.Start.Line
			endLine := edit.Range.End.Line
			startChar := edit.Range.Start.Character
			endChar := edit.Range.End.Character

			// 替换指定范围
			before := lines[startLine][:startChar]
			middle := edit.NewText
			after := lines[endLine][endChar:]

			newLines := append(lines[:startLine], before+middle+after)
			newLines = append(newLines, lines[endLine+1:]...)
			lines = newLines
		}

		newContent := strings.Join(lines, "\n")
		if err := os.WriteFile(filePath, []byte(newContent), 0644); err != nil {
			return nil, fmt.Errorf("write %s: %w", filePath, err)
		}
		changedFiles = append(changedFiles, filePath)
	}
	return changedFiles, nil
}

// FormatLSPResult 格式化 LSP 结果供 SE 使用
func FormatDefResult(locations []LSPLocation) string {
	if len(locations) == 0 {
		return "未找到定义"
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("找到 %d 个定义:\n", len(locations)))
	for i, loc := range locations {
		filePath := strings.TrimPrefix(loc.URI, "file://")
		sb.WriteString(fmt.Sprintf("  #%d %s:%d:%d\n", i+1, filePath, loc.Range.Start.Line+1, loc.Range.Start.Character+1))
	}
	return sb.String()
}

func FormatRefResult(locations []LSPLocation) string {
	if len(locations) == 0 {
		return "未找到引用"
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("找到 %d 处引用:\n", len(locations)))
	// 按文件分组
	files := make(map[string][]LSPLocation)
	for _, loc := range locations {
		fp := strings.TrimPrefix(loc.URI, "file://")
		files[fp] = append(files[fp], loc)
	}
	for fp, locs := range files {
		sb.WriteString(fmt.Sprintf("\n  📄 %s (%d处):\n", filepath.Base(fp), len(locs)))
		for _, loc := range locs {
			sb.WriteString(fmt.Sprintf("    L%d:C%d\n", loc.Range.Start.Line+1, loc.Range.Start.Character+1))
		}
	}
	return sb.String()
}

func FormatDiagResult(diags []Diagnostic) string {
	if len(diags) == 0 {
		return "✅ 无诊断问题"
	}
	var sb strings.Builder
	errors := 0
	warnings := 0
	for _, d := range diags {
		icon := "ℹ️"
		switch d.Severity {
		case 1:
			icon = "❌"
			errors++
		case 2:
			icon = "⚠️"
			warnings++
		}
		sb.WriteString(fmt.Sprintf("%s L%d: %s\n", icon, d.Range.Start.Line+1, d.Message))
	}
	sb.WriteString(fmt.Sprintf("\n总计: %d 错误, %d 警告, %d 其他\n", errors, warnings, len(diags)-errors-warnings))
	return sb.String()
}
