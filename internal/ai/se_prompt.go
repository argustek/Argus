package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const SEPrompt = `你是Argus的软件工程师(SE)，负责执行具体的编码任务。

当前工作目录: %s
所有文件路径使用相对于此目录的路径（如 main.go、src/utils.go）。

核心职责：
1. 执行USR要求的编码任务（通过PM转达，无条件执行）
2. 写完代码后自我验证：编译→运行→确认输出正确
3. 验证失败时分析错误、修复代码、重试
4. 全部通过后调用 complete_task 工具标记完成

可用工具：
- list_files: 递归列出项目文件，支持pattern过滤（如**/*.go），用于了解项目结构
- glob: 按模式查找文件（如 **/*.go, src/**/*.py）
- read_file: 读取文件内容，支持offset/limit读取大文件的指定行范围
- write_file: 创建或覆写文件
- edit_file: 精确替换文件中的文本
- search_files: 搜索文件内容（支持正则和文件类型过滤）
- exec: 执行命令（编译、运行、测试等）
- web_search: 查询网络获取最新文档和技术资料
- git_operation: Git操作（status/diff/commit/push/pull/log等）
- run_tests: 运行测试
- delete_file: 删除文件
- complete_task: 标记任务完成

工作流程：
1. 【探索】先用 list_files 或 glob 了解项目结构和现有代码
2. 【查阅】用 read_file（可指定行范围）查看关键文件
3. 【查询】遇到不熟悉的API用 web_search 查文档
4. 【编写】write_file/edit_file 编写代码
5. 【验证】exec 编译运行 → read_file 确认结果
6. 验证失败 → 分析错误 → edit_file 修复 → exec 再测试
7. 全部通过 → complete_task 标记完成

注意事项：
- 工作目录内的操作直接执行，目录外操作（安装软件等）需告知PM
- 危险操作（git reset --hard等）需先确认
- 如果 go.mod 已存在，不要执行 go mod init
- 读大文件时使用 offset/limit 参数避免撑爆上下文
- 尽量一步到位，减少反复`

type SEProcessor struct {
	client        *Client
	workDir       string
	systemPrompt  string
	history       []Message
	ReplyLanguage string
	ctx           context.Context
	envMemory     string
}

func NewSEProcessor(client *Client, workDir string) *SEProcessor {
	return &SEProcessor{
		client:       client,
		workDir:      workDir,
		systemPrompt: fmt.Sprintf(SEPrompt, workDir),
		history:      []Message{},
	}
}

func (s *SEProcessor) SetEnvMemory(summary string) {
	s.envMemory = summary
}

func (s *SEProcessor) getSystemPrompt() string {
	if s.envMemory != "" {
		return s.systemPrompt + "\n\n" + s.envMemory
	}
	return s.systemPrompt
}

func (s *SEProcessor) SetContext(ctx context.Context) {
	s.ctx = ctx
}

func (s *SEProcessor) getCtx() context.Context {
	if s.ctx == nil {
		return context.Background()
	}
	return s.ctx
}

func (s *SEProcessor) ProcessTaskWithTools(taskDesc string, onChunk func(delta string)) (*SEResponse, error) {
	fmt.Printf("[SE Tools] Starting task: %s\n", taskDesc[:min(60, len(taskDesc))])

	if s.client == nil {
		return nil, fmt.Errorf("SEProcessor.client is nil")
	}

	s.history = append(s.history, Message{Role: "user", Content: taskDesc})

	var allActions []SEAction
	var finalContent string
	completeResult := &SECompletion{Status: "completed"}
	maxRounds := 10

	for round := 0; round < maxRounds; round++ {
		callCtx, callCancel := context.WithTimeout(s.getCtx(), 120*time.Second)
		resp, err := s.client.ChatWithTools(callCtx, s.getSystemPrompt(), s.history, taskDesc, SETools)
		callCancel()
		if err != nil {
			return nil, fmt.Errorf("SE ChatWithTools failed: %w", err)
		}
		if len(resp.Choices) == 0 {
			return nil, fmt.Errorf("SE: empty response from AI")
		}

		msg := resp.Choices[0].Message
		finalContent = msg.Content

		if onChunk != nil {
			if msg.Content != "" {
				onChunk(msg.Content)
			}
			for _, tc := range msg.ToolCalls {
				onChunk(fmt.Sprintf("\n🔧 **%s**\n", tc.Function.Name))
			}
		}

		if len(msg.ToolCalls) == 0 {
			fmt.Printf("[SE Tools] Round %d: no tool calls, ending\n", round)
			break
		}

		s.history = append(s.history, msg)

		for _, tc := range msg.ToolCalls {
			action := s.toolCallToSEAction(tc)
			allActions = append(allActions, action)

			if tc.Function.Name == "complete_task" {
				formatCompleteFromAction(action, completeResult)
				fmt.Printf("[SE Tools] complete_task called: files=%v summary=%s\n",
					completeFilesFromAction(action), completeSummaryFromAction(action))
				goto done
			}

			toolResult := s.executeSETool(tc.Function.Name, tc.Function.Arguments)
			if onChunk != nil {
				preview := toolResult
				if len(preview) > 300 {
					preview = preview[:300] + "..."
				}
				onChunk(fmt.Sprintf("```\n%s\n```\n", preview))
			}
			s.history = append(s.history, Message{
				Role:       "tool",
				Content:    toolResult,
				ToolCallID: tc.ID,
			})
		}

		taskDesc = "[继续执行下一步操作]"
	}

done:
	result := &SEResponse{
		Content:   finalContent,
		Actions:   allActions,
		Completed: completeResult,
		NeedHelp:  false,
	}

	// 智能上下文压缩：保留最近消息，将旧消息摘要化
	if len(s.history) > 20 {
		s.compressHistory()
	}

	return result, nil
}

func (s *SEProcessor) toolCallToSEAction(tc ToolCall) SEAction {
	action := SEAction{Type: tc.Function.Name}

	var args map[string]interface{}
	if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
		fmt.Printf("[SE Tools] failed to parse tool args: %v\n", err)
		return action
	}

	switch tc.Function.Name {
	case "write_file":
		if v, ok := args["path"].(string); ok {
			action.Path = v
		}
		if v, ok := args["content"].(string); ok {
			action.Content = v
		}
	case "exec":
		if v, ok := args["command"].(string); ok {
			action.Command = v
		}
	case "read_file":
		if v, ok := args["path"].(string); ok {
			action.Path = v
		}
		if v, ok := args["offset"].(float64); ok {
			action.Offset = int(v)
		}
		if v, ok := args["limit"].(float64); ok {
			action.Limit = int(v)
		}
	case "edit_file":
		if v, ok := args["path"].(string); ok {
			action.Path = v
		}
		if v, ok := args["old_str"].(string); ok {
			action.OldStr = v
		}
		if v, ok := args["new_str"].(string); ok {
			action.NewStr = v
		}
	case "search_files":
		if v, ok := args["pattern"].(string); ok {
			action.Pattern = v
		}
		if v, ok := args["file_pattern"].(string); ok {
			action.FilePattern = v
		}
		if v, ok := args["is_regex"].(bool); ok {
			action.IsRegex = v
		}
		if v, ok := args["case_insensitive"].(bool); ok {
			action.CaseInsensitive = v
		}
	case "git_operation":
		if v, ok := args["git_action"].(string); ok {
			action.GitAction = v
		}
		if v, ok := args["git_message"].(string); ok {
			action.GitMessage = v
		}
		if v, ok := args["git_args"].([]interface{}); ok {
			for _, a := range v {
				if s, ok := a.(string); ok {
					action.GitArgs = append(action.GitArgs, s)
				}
			}
		}
	case "run_tests":
		if v, ok := args["test_pattern"].(string); ok {
			action.TestPattern = v
		}
		if v, ok := args["test_coverage"].(bool); ok {
			action.TestCoverage = v
		}
		if v, ok := args["test_verbose"].(bool); ok {
			action.TestVerbose = v
		}
	case "complete_task":
		if v, ok := args["files"].([]interface{}); ok {
			for _, f := range v {
				if s, ok := f.(string); ok {
					action.Content += s + ","
				}
			}
		}
		if v, ok := args["summary"].(string); ok {
			action.Command = v
		}
	case "list_files":
		if v, ok := args["pattern"].(string); ok {
			action.Pattern = v
		}
		if v, ok := args["recursive"].(bool); ok {
			action.IsRegex = v // 复用 IsRegex 字段存 recursive 标志
		}
	case "glob":
		if v, ok := args["pattern"].(string); ok {
			action.Pattern = v
		}
		action.Type = "glob"
	case "web_search":
		if v, ok := args["query"].(string); ok {
			action.Command = v // 复用 Command 字段存 query
		}
		action.Type = "web_search"
	case "delete_file":
		if v, ok := args["path"].(string); ok {
			action.Path = v
		}
		action.Type = "delete_file"
	}

	return action
}

func completeFilesFromAction(a SEAction) []string {
	return strings.Split(strings.TrimRight(a.Content, ","), ",")
}

func completeSummaryFromAction(a SEAction) string {
	return a.Command
}

func formatCompleteFromAction(a SEAction, c *SECompletion) {
	c.Status = "completed"
	c.TechnicalNotes = a.Command
}

func (s *SEProcessor) executeSETool(name, argsJSON string) string {
	switch name {
	case "read_file":
		var args struct {
			Path   string `json:"path"`
			Offset int    `json:"offset"`
			Limit  int    `json:"limit"`
		}
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			return fmt.Sprintf("参数解析失败: %v", err)
		}
		if args.Path == "" {
			return "错误: path参数为空"
		}
		var fullPath string
		if filepath.IsAbs(args.Path) {
			fullPath = args.Path
		} else {
			fullPath = filepath.Join(s.workDir, args.Path)
		}
		content, err := os.ReadFile(fullPath)
		if err != nil {
			return fmt.Sprintf("读取文件失败: %v", err)
		}
		lines := strings.Split(string(content), "\n")
		offset := args.Offset
		limit := args.Limit
		if offset < 1 {
			offset = 1
		}
		if limit < 1 {
			limit = 100
		}
		if offset > len(lines) {
			return fmt.Sprintf("文件共%d行，offset=%d超出范围", len(lines), offset)
		}
		end := offset + limit - 1
		if end > len(lines) {
			end = len(lines)
		}
		selected := lines[offset-1 : end]
		result := strings.Join(selected, "\n")
		return fmt.Sprintf("[文件: %s | 行 %d-%d / 共%d行]\n%s", args.Path, offset, end, len(lines), result)

	case "list_files":
		var args struct {
			Pattern   string `json:"pattern"`
			Recursive bool   `json:"recursive"`
		}
		json.Unmarshal([]byte(argsJSON), &args)
		if !args.Recursive {
			args.Recursive = true // 默认递归
		}

		pattern := args.Pattern
		if pattern == "" || pattern == "*" {
			pattern = "*"
		}

		var results []string
		if args.Recursive {
			// 递归遍历，支持 **/*.go 等模式
			baseDir := s.workDir
			// 将 ** 转换为递归匹配
			globPattern := pattern
			if !filepath.IsAbs(globPattern) {
				globPattern = filepath.Join(baseDir, globPattern)
			}
			matches, _ := filepath.Glob(globPattern)
			for _, m := range matches {
				rel, _ := filepath.Rel(baseDir, m)
				results = append(results, rel)
			}
			// 如果没匹配到且pattern含**，尝试双星展开
			if len(results) == 0 && strings.Contains(pattern, "**") {
				s.walkMatch(baseDir, pattern, baseDir, &results)
			}
		} else {
			// 非递归：只列顶层
			entries, _ := os.ReadDir(s.workDir)
			for _, e := range entries {
				if e.IsDir() {
					results = append(results, e.Name()+"/")
				} else {
					results = append(results, e.Name())
				}
			}
		}
		if len(results) == 0 {
			return "未找到匹配文件"
		}
		return strings.Join(results, "\n")

	case "glob":
		var args struct {
			Pattern string `json:"pattern"`
		}
		json.Unmarshal([]byte(argsJSON), &args)
		if args.Pattern == "" {
			return "错误: pattern参数为空"
		}
		var results []string
		globPattern := filepath.Join(s.workDir, args.Pattern)
		matches, _ := filepath.Glob(globPattern)
		for _, m := range matches {
			rel, _ := filepath.Rel(s.workDir, m)
			results = append(results, rel)
		}
		// 双星展开
		if len(results) == 0 && strings.Contains(args.Pattern, "**") {
			s.walkMatch(s.workDir, args.Pattern, s.workDir, &results)
		}
		if len(results) == 0 {
			return "未找到匹配文件"
		}
		return strings.Join(results, "\n")

	case "web_search":
		var args struct {
			Query string `json:"query"`
		}
		json.Unmarshal([]byte(argsJSON), &args)
		if args.Query == "" {
			return "错误: query参数为空"
		}
		return s.webSearch(args.Query)

	case "delete_file":
		var args struct {
			Path string `json:"path"`
		}
		json.Unmarshal([]byte(argsJSON), &args)
		if args.Path == "" {
			return "错误: path参数为空"
		}
		fullPath := args.Path
		if !filepath.IsAbs(fullPath) {
			fullPath = filepath.Join(s.workDir, args.Path)
		}
		// 安全检查：不允许删除工作目录本身或上级目录
		absWork, _ := filepath.Abs(s.workDir)
		absPath, _ := filepath.Abs(fullPath)
		if absPath == absWork || !strings.HasPrefix(absPath, absWork+string(filepath.Separator)) {
			return fmt.Sprintf("安全拒绝: 不允许删除路径 %s（超出工作目录范围）", args.Path)
		}
		if err := os.Remove(fullPath); err != nil {
			return fmt.Sprintf("删除文件失败: %v", err)
		}
		return fmt.Sprintf("已删除: %s", args.Path)

	case "search_files":
		var args struct {
			Pattern         string `json:"pattern"`
			FilePattern     string `json:"file_pattern"`
			IsRegex         bool   `json:"is_regex"`
			CaseInsensitive bool   `json:"case_insensitive"`
		}
		json.Unmarshal([]byte(argsJSON), &args)
		if args.Pattern == "" {
			return "错误: pattern参数为空"
		}
		// 使用增强版搜索（Walk + .gitignore跳过 + 上下文行）
		return s.searchFilesEnhanced(args.Pattern, args.FilePattern, args.IsRegex, args.CaseInsensitive)

	default:
		return fmt.Sprintf("工具 %s: 由executor执行", name)
	}
}

// walkMatch 递归匹配 **/*.xxx 模式
func (s *SEProcessor) walkMatch(baseDir, pattern, currentDir string, results *[]string) {
	entries, err := os.ReadDir(currentDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		fullPath := filepath.Join(currentDir, e.Name())
		relPath, _ := filepath.Rel(baseDir, fullPath)
		// 跳过隐藏目录和常见忽略目录
		if e.IsDir() && (strings.HasPrefix(e.Name(), ".") || e.Name() == "node_modules" || e.Name() == "vendor" || e.Name() == ".git") {
			continue
		}
		// 将pattern中的**转换为当前路径进行匹配
		globPart := strings.Replace(pattern, "**", relPath, 1)
		matched, _ := filepath.Match(globPart, relPath)
		if matched && !e.IsDir() {
			*results = append(*results, relPath)
		}
		if e.IsDir() {
			s.walkMatch(baseDir, pattern, fullPath, results)
		}
	}
}

// webSearch 执行网络搜索，返回搜索结果摘要
func (s *SEProcessor) webSearch(query string) string {
	client := &http.Client{Timeout: 15 * time.Second}
	// 使用 DuckDuckGo HTML 版本获取即时答案
	url := fmt.Sprintf("https://html.duckduckgo.com/html/?q=%s", url.QueryEscape(query))
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; ArgusSE/1.0)")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Sprintf("网络搜索失败（网络不可用）: %v\n建议: 使用 exec 工具执行 curl 命令获取信息", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	html := string(body)

	// 提取结果片段：查找 <a class="result__a" 和 <a class="result__snippet"
	var results []string
	// 简单提取 result__snippet 内容
	snippetRegex := regexp.MustCompile(`<a[^>]*class="result__snippet"[^>]*>(.*?)</a>`)
	titleRegex := regexp.MustCompile(`<a[^>]*class="result__a"[^>]*>(.*?)</a>`)

	snippets := snippetRegex.FindAllStringSubmatch(html, 5)
	titles := titleRegex.FindAllStringSubmatch(html, 5)

	maxResults := len(titles)
	if maxResults > len(snippets) {
		maxResults = len(snippets)
	}
	if maxResults == 0 {
		return fmt.Sprintf("未找到 '%s' 的相关结果。\n提示: 可尝试更具体的关键词，或使用 exec 执行 curl 命令直接访问文档URL", query)
	}

	results = append(results, fmt.Sprintf("🔍 搜索: %s\n", query))
	for i := 0; i < maxResults; i++ {
		title := stripHTML(titles[i][1])
		snippet := stripHTML(snippets[i][1])
		results = append(results, fmt.Sprintf("%d. %s\n   %s\n", i+1, title, snippet))
	}
	return strings.Join(results, "\n")
}

// stripHTML 移除HTML标签
func stripHTML(s string) string {
	re := regexp.MustCompile(`<[^>]*>`)
	return re.ReplaceAllString(s, "")
}

// searchFilesEnhanced 增强版文件搜索：Walk遍历 + 跳过忽略目录 + 支持上下文行
func (s *SEProcessor) searchFilesEnhanced(pattern, filePattern string, isRegex, caseInsensitive bool) string {
	var results []string

	fileGlob := filePattern
	if fileGlob == "" {
		fileGlob = "*"
	}

	skipDirs := map[string]bool{
		".git": true, "node_modules": true, "vendor": true,
		".idea": true, ".vscode": true, "__pycache__": true,
		"dist": true, "build": true, ".next": true,
		".cache": true, "coverage": true,
	}

	filepath.Walk(s.workDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			if info.IsDir() && skipDirs[info.Name()] {
				return filepath.SkipDir
			}
			return nil
		}

		// 文件类型过滤
		if fileGlob != "*" {
			relPath, _ := filepath.Rel(s.workDir, path)
			matched, _ := filepath.Match(fileGlob, info.Name())
			if !matched {
				// 也尝试匹配相对路径
				matched2, _ := filepath.Match(fileGlob, relPath)
				if !matched2 {
					return nil
				}
			}
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		lines := strings.Split(string(content), "\n")
		contextBefore := 1 // 上下文行数
		contextAfter := 1

		for i, line := range lines {
			haystack := line
			needle := pattern
			if caseInsensitive {
				haystack = strings.ToLower(haystack)
				needle = strings.ToLower(needle)
			}

			var matched bool
			if isRegex {
				matched, _ = regexp.MatchString(needle, haystack)
			} else {
				matched = strings.Contains(haystack, needle)
			}

			if matched {
				relPath, _ := filepath.Rel(s.workDir, path)
				// 输出上下文行
				start := i - contextBefore
				if start < 0 {
					start = 0
				}
				end := i + contextAfter + 1
				if end > len(lines) {
					end = len(lines)
				}
				for j := start; j < end; j++ {
					prefix := " "
					if j == i {
						prefix = ">"
					}
					results = append(results, fmt.Sprintf("%s%s:%d: %s", prefix, relPath, j+1, lines[j]))
				}
			}
		}
		return nil
	})

	if len(results) == 0 {
		return "未找到匹配内容"
	}
	if len(results) > 100 {
		results = results[:100]
		results = append([]string{fmt.Sprintf("... 共 %d 条匹配，显示前 100 条 ...", len(results))}, results...)
	}
	return strings.Join(results, "\n")
}

func (s *SEProcessor) AddResult(result string) {
	s.history = append(s.history, Message{Role: "user", Content: result})
}

// compressHistory 智能压缩历史消息：保留最近的工具交互，将旧对话摘要化
func (s *SEProcessor) compressHistory() {
	if len(s.history) <= 20 {
		return
	}

	// 保留最近15条消息（当前活跃的上下文）
	recentCount := 15
	oldMessages := s.history[:len(s.history)-recentCount]
	s.history = s.history[len(s.history)-recentCount:]

	// 将旧消息摘要化：提取关键信息（做了什么操作、结果如何）
	var summaryParts []string
	var lastToolName string
	actionCount := 0

	for _, msg := range oldMessages {
		switch msg.Role {
		case "assistant":
			// 记录使用了哪些工具
			if len(msg.ToolCalls) > 0 {
				for _, tc := range msg.ToolCalls {
					summaryParts = append(summaryParts, tc.Function.Name)
					lastToolName = tc.Function.Name
				}
				actionCount++
			}
		case "tool":
			// 记录工具结果的简短信息
			preview := strings.TrimSpace(msg.Content)
			if len(preview) > 80 {
				preview = preview[:80] + "..."
			}
			if lastToolName != "" && !strings.HasPrefix(preview, "错误") && !strings.HasPrefix(preview, "[文件:") {
				summaryParts = append(summaryParts, fmt.Sprintf("→%s", preview))
			}
		case "user":
			// 用户/系统指令保留关键词
			if len(msg.Content) > 60 {
				summaryParts = append(summaryParts, fmt.Sprintf("任务:%s...", msg.Content[:60]))
			} else if msg.Content != "" {
				summaryParts = append(summaryParts, fmt.Sprintf("任务:%s", msg.Content))
			}
		}
	}

	summary := fmt.Sprintf("[上下文摘要] 此前已完成 %d 轮操作: %s",
		actionCount, strings.Join(summaryParts, "; "))
	// 将摘要作为最早的 system/user 消息插入
	s.history = append([]Message{{Role: "system", Content: summary}}, s.history...)
}

type SEResponse struct {
	Content   string
	Actions   []SEAction
	Completed *SECompletion
	NeedHelp  bool
}

type SEAction struct {
	Type    string `json:"type"`
	Path    string `json:"path,omitempty"`
	Content string `json:"content,omitempty"`
	Command string `json:"command,omitempty"`
	Tool    string `json:"tool,omitempty"`

	Offset int `json:"offset,omitempty"`
	Limit  int `json:"limit,omitempty"`

	OldStr          string   `json:"old_str,omitempty"`
	NewStr          string   `json:"new_str,omitempty"`
	Pattern         string   `json:"pattern,omitempty"`
	FilePattern     string   `json:"file_pattern,omitempty"`
	IsRegex         bool     `json:"is_regex,omitempty"`
	CaseInsensitive bool     `json:"case_insensitive,omitempty"`
	GitAction       string   `json:"git_action,omitempty"`
	GitMessage      string   `json:"git_message,omitempty"`
	GitArgs         []string `json:"git_args,omitempty"`
	TestPattern     string   `json:"test_pattern,omitempty"`
	TestCoverage    bool     `json:"test_coverage,omitempty"`
	TestVerbose     bool     `json:"test_verbose,omitempty"`
}

type SECompletion struct {
	TechnicalNotes string `json:"technical_notes"`
	ChangelogDraft string `json:"changelog_draft"`
	Status         string `json:"status"`
}

func (s *SEProcessor) CheckSemanticComplete(response string) bool {
	lower := strings.ToLower(response)
	for _, kw := range []string{
		"任务完成", "已完成", "task completed", "done", "finished",
	} {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

func (s *SEProcessor) ResetHistory() {
	s.history = []Message{}
}

var SETools = []Tool{
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "write_file",
			Description: "创建或覆写文件。path为相对于工作目录的路径（如hello.go），content为文件完整内容。",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "文件路径（相对于工作目录，如 main.go、src/utils.go）",
					},
					"content": map[string]interface{}{
						"type":        "string",
						"description": "文件的完整内容",
					},
				},
				"required": []string{"path", "content"},
			},
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "exec",
			Description: "在工作目录下执行命令。用于编译、运行程序、测试等。超时30秒。",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"command": map[string]interface{}{
						"type":        "string",
						"description": "要执行的命令（如 go run hello.go、go build、python main.py、npm test）",
					},
				},
				"required": []string{"command"},
			},
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "read_file",
			Description: "读取文件内容。支持offset和limit参数读取大文件的指定行范围，避免撑爆上下文。",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "文件路径（相对于工作目录）",
					},
					"offset": map[string]interface{}{
						"type":        "number",
						"description": "起始行号（从1开始），默认1",
					},
					"limit": map[string]interface{}{
						"type":        "number",
						"description": "读取行数，默认100，大文件建议设为50-100",
					},
				},
				"required": []string{"path"},
			},
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "edit_file",
			Description: "精确编辑现有文件。old_str为要替换的文本（必须唯一匹配，至少20字符），new_str为替换后的文本。",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "文件路径（相对于工作目录）",
					},
					"old_str": map[string]interface{}{
						"type":        "string",
						"description": "要替换的原文（必须能在文件中唯一匹配）",
					},
					"new_str": map[string]interface{}{
						"type":        "string",
						"description": "替换后的新文本",
					},
				},
				"required": []string{"path", "old_str", "new_str"},
			},
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "search_files",
			Description: "搜索文件内容。pattern为搜索关键词或正则表达式，file_pattern可选过滤文件类型（如*.go）。",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"pattern": map[string]interface{}{
						"type":        "string",
						"description": "搜索关键词或正则表达式",
					},
					"file_pattern": map[string]interface{}{
						"type":        "string",
						"description": "文件过滤（如 *.go, *.py），可选",
					},
					"is_regex": map[string]interface{}{
						"type":        "boolean",
						"description": "是否使用正则表达式，默认false",
					},
					"case_insensitive": map[string]interface{}{
						"type":        "boolean",
						"description": "是否忽略大小写",
					},
				},
				"required": []string{"pattern"},
			},
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "git_operation",
			Description: "Git版本控制操作。git_action为操作类型（status/diff/commit/push/pull/log）。",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"git_action": map[string]interface{}{
						"type":        "string",
						"description": "操作类型: status, diff, commit, push, pull, log, branch, show",
						"enum":        []string{"status", "diff", "commit", "push", "pull", "log", "branch", "show"},
					},
					"git_message": map[string]interface{}{
						"type":        "string",
						"description": "提交信息（commit时必填）",
					},
					"git_args": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "string"},
						"description": "额外参数（如 push 时的 origin main）",
					},
				},
				"required": []string{"git_action"},
			},
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "run_tests",
			Description: "运行测试。test_pattern为测试匹配模式（默认./...），可生成覆盖率报告。",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"test_pattern": map[string]interface{}{
						"type":        "string",
						"description": "测试匹配模式（如 ./internal/...），默认 ./...",
					},
					"test_coverage": map[string]interface{}{
						"type":        "boolean",
						"description": "是否生成覆盖率报告",
					},
					"test_verbose": map[string]interface{}{
						"type":        "boolean",
						"description": "是否详细输出",
					},
				},
			},
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "list_files",
			Description: "递归列出工作目录下的文件和目录，用于了解项目结构。支持pattern过滤（如**/*.go只列Go文件），默认递归列出所有文件。",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"pattern": map[string]interface{}{
						"type":        "string",
						"description": "glob模式过滤（如 **/*.go, src/**, *.py），默认*列出所有",
					},
					"recursive": map[string]interface{}{
						"type":        "boolean",
						"description": "是否递归子目录，默认true",
					},
				},
			},
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "glob",
			Description: "按glob模式递归查找文件名。用于快速定位文件（如查找所有Go文件、某个目录下的配置文件等）。",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"pattern": map[string]interface{}{
						"type":        "string",
						"description": "glob模式（如 **/*.go, src/**/*.py, *.json, **/test*.go）",
					},
				},
				"required": []string{"pattern"},
			},
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "web_search",
			Description: "查询网络获取最新文档、API参考或技术资料。当不熟悉某个API或框架时使用。",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{
						"type":        "string",
						"description": "搜索查询（如 'Go net/http server example', 'React useEffect cleanup'）",
					},
				},
				"required": []string{"query"},
			},
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "delete_file",
			Description: "删除指定路径的文件。谨慎使用，删除前请确认路径正确。",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "要删除的文件路径（相对于工作目录）",
					},
				},
				"required": []string{"path"},
			},
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "complete_task",
			Description: "任务完成时调用此工具。必须在所有操作成功验证后调用。files列出所有创建/修改的文件，summary简洁描述做了什么。",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"files": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "string"},
						"description": "所有创建/修改的文件名列表",
					},
					"summary": map[string]interface{}{
						"type":        "string",
						"description": "一句话概括实现了什么",
					},
				},
				"required": []string{"files", "summary"},
			},
		},
	},
}
