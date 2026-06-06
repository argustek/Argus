package ai

import (
	"context"
	"encoding/json"
	"fmt"
	htmlpkg "html"
	"io"
	"math"
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
- semantic_search: 语义搜索代码库，按功能意图查找（如"认证逻辑在哪"、"数据库查询"），理解代码含义
- search_snippet: 搜索代码片段库，快速获取常用模板（HTTP server、CRUD API、认证中间件、数据库连接等）
- list_files: 递归列出项目文件，支持pattern过滤（如**/*.go），用于了解项目结构
- glob: 按模式查找文件（如 **/*.go, src/**/*.py）
- read_file: 读取文件内容，支持offset/limit读取大文件的指定行范围
- write_file: 创建或覆写文件
- edit_file: 精确替换文件中的文本
- show_diff: 预览编辑效果，对比修改前后差异（不实际修改文件）
- search_files: 搜索文件内容（支持正则和文件类型过滤）
- exec: 执行命令（编译、运行、测试等），每次独立进程
- debug_run: 调试运行（自动加-v -race等flag，panic/trace结构化展示）
- auto_debug: 自动调试（跑测试→AI分析错误→定位根因→生成修复→重跑，循环直到通过或达上限）
- analyze_code: 静态代码分析，检测潜在隐患（nil panic、越界、资源泄漏、并发安全、弱加密等），支持按分类和严重程度过滤
- exec_session: 在持久化shell中执行命令（保持cd/env状态，支持多步构建）
- web_search: 查询网络获取最新文档和技术资料
- git_operation: Git操作（status/diff/commit/push/pull/log等）
- run_tests: 运行测试
- delete_file: 删除文件
- complete_task: 标记任务完成

工作流程：
1. 【探索】用 semantic_search 理解项目架构（如"用户认证在哪实现"），或用 list_files/glob 查看结构
2. 【定位】用 semantic_search 查找相关代码（按功能而非文件名），找到需要修改的位置
3. 【查阅】用 read_file（可指定行范围）查看关键文件
4. 【查询】遇到不熟悉的API用 web_search 查文档
5. 【编写】write_file/edit_file 编写代码
6. 【验证】exec 编译运行 → read_file 确认结果
7. 验证失败 → 分析错误 → edit_file 修复 → exec 再测试
8. 全部通过 → complete_task 标记完成

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
	indexer       *CodeIndexer
	snippetStore  *SnippetStore
	debugLog      func(string) // 调试日志回调（写入conversation.log）
}

func NewSEProcessor(client *Client, workDir string) *SEProcessor {
	dataDir := filepath.Join(workDir, ".argus")
	return &SEProcessor{
		client:       client,
		workDir:      workDir,
		systemPrompt: fmt.Sprintf(SEPrompt, workDir),
		history:      []Message{},
		snippetStore: NewSnippetStore(dataDir),
	}
}

// EnsureIndexer 确保索引器已初始化并生成语义概念
func (s *SEProcessor) EnsureIndexer() error {
	if s.indexer != nil {
		return nil
	}
	s.indexer = NewCodeIndexer(s.workDir)
	s.indexer.SetClient(s.client)
	fmt.Printf("[SemSearch] 开始索引 %s ...\n", s.workDir)
	if err := s.indexer.IndexProject(); err != nil {
		return err
	}
	fmt.Printf("[SemSearch] 索引完成: %s\n", s.indexer.Stats())

	// 异步生成语义概念（不阻塞任务执行）
	go func() {
		if err := s.indexer.GenerateConcepts(context.Background()); err != nil {
			fmt.Printf("[SemSearch] 概念生成: %v\n", err)
		}
	}()

	return nil
}

func (s *SEProcessor) SetEnvMemory(summary string) {
	s.envMemory = summary
}

func (s *SEProcessor) GetIndexer() *CodeIndexer {
	return s.indexer
}

func (s *SEProcessor) GetSnippetStore() *SnippetStore {
	return s.snippetStore
}

func (s *SEProcessor) GetClient() *Client {
	return s.client
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

// SetDebugLog 设置调试日志回调（由manager传入writeRouteLog）
func (s *SEProcessor) SetDebugLog(fn func(string)) {
	s.debugLog = fn
}

// seLog 同时输出到终端和日志文件
func (s *SEProcessor) seLog(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	fmt.Print(msg)
	if s.debugLog != nil {
		s.debugLog(msg)
	}
}

func (s *SEProcessor) getCtx() context.Context {
	if s.ctx == nil {
		return context.Background()
	}
	return s.ctx
}

func (s *SEProcessor) ProcessTaskWithTools(taskDesc string, onChunk func(delta string)) (*SEResponse, error) {
	s.seLog("[SE Tools] Starting task: %s\n", taskDesc[:min(60, len(taskDesc))])

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
			s.seLog("[SE Tools] Round %d: no tool calls, ending\n", round)
			break
		}

		s.history = append(s.history, msg)

		for _, tc := range msg.ToolCalls {
			action := s.toolCallToSEAction(tc)
			allActions = append(allActions, action)

			if tc.Function.Name == "complete_task" {
				formatCompleteFromAction(action, completeResult)
				s.seLog("[SE Tools] complete_task called: files=%v summary=%s\n",
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

// repairJSONArgs 修复LLM生成的畸形JSON参数
// 常见损坏模式: "pathfilename"→"path":"filename", importfmt"→import "fmt", 缺失引号等
func repairJSONArgs(raw string) string {
	repaired := raw

	// 模式1: 键值之间缺失 ": "  如 "pathfilename.go"→ 找到已知键名后修复
	keyPatterns := []string{"path", "content", "command", "old_str", "new_str", "pattern"}
	for _, key := range keyPatterns {
		// "keyvalue" → "key": "value" (键后直接跟值)
		re := regexp.MustCompile(`"` + regexp.QuoteMeta(key) + `([a-zA-Z_./\\])`)
		repaired = re.ReplaceAllString(repaired, `"`+key+`": "$1`)
	}

	// 模式2: Go代码中常见修复 - importfmt" → import "fmt"
	repaired = strings.ReplaceAll(repaired, `importfmt"`, `import "fmt"`)
	repaired = strings.ReplaceAll(repaired, `import"`, `import "`)
	repaired = strings.ReplaceAll(repaired, `package"fmt"`, `package main"\n\nimport "fmt"`)
	repaired = strings.ReplaceAll(repaired, `package "fmt"`, `package main\n\nimport "fmt"`)

	// 模式3: 缺失闭引号 - import "fmt  后面直接换行或(
	// "import \"fmt\n → "import \"fmt\"\n
	repaired = regexp.MustCompile(`import\s*"([^"]*?)\n`).ReplaceAllString(repaired, `import "$1"`+"\n")
	repaired = regexp.MustCompile(`import\s*"([^"]*?)$`).ReplaceAllString(repaired, `import "$1"`)

	// 模式4: func main() { ... "Beta"n} → "Beta\"\n}"
	// 反斜杠+字母n 在Go字符串末尾应该是 \n
	repaired = regexp.MustCompile(`"(\w)"n}`).ReplaceAllString(repaired, `$1\"\\n}"`)
	repaired = regexp.MustCompile(`"(\w)"n$`).ReplaceAllString(repaired, `$1\"\\n"`)

	// 模式5: fmt"n → fmt"\n
	repaired = strings.ReplaceAll(repaired, `fmt"n`, `fmt"\n`)
	repaired = strings.ReplaceAll(repaired, `"fmt"n`, `"fmt"\n`)

	// 模式6: command中 go run_xxx → go run xxx (下划线变空格)
	repaired = regexp.MustCompile(`go run_([a-z])`).ReplaceAllString(repaired, `go run $1`)

	if repaired != raw {
		fmt.Printf("[SE Tools] JSON repaired: %d → %d chars\n", len(raw), len(repaired))
	}
	return repaired
}

func (s *SEProcessor) toolCallToSEAction(tc ToolCall) SEAction {
	action := SEAction{Type: tc.Function.Name}

	argsStr := tc.Function.Arguments

	// [G-DEBUG] 记录原始ToolCall参数，排查JSON解析问题
	s.seLog("[SE-RAW] tool=%q | args_len=%d | raw=%s\n", tc.Function.Name, len(argsStr),
		func() string {
			if len(argsStr) > 300 { return argsStr[:300] + "..." }
			return argsStr
		}())

	var args map[string]interface{}
	if err := json.Unmarshal([]byte(argsStr), &args); err != nil {
		s.seLog("[SE Tools] failed to parse tool args: %v, attempting repair...\n", err)
		// 尝试修复后再解析
		repaired := repairJSONArgs(argsStr)
		if err2 := json.Unmarshal([]byte(repaired), &args); err2 != nil {
			s.seLog("[SE Tools] repair also failed: %v\n", err2)
			return action
		}
		s.seLog("[SE Tools] ✅ JSON repair succeeded\n")
	}

	switch tc.Function.Name {
	case "write_file":
		if v, ok := args["path"].(string); ok {
			action.Path = v
		}
		if v, ok := args["content"].(string); ok {
			action.Content = v
		}
		// [G-DEBUG] 显示解析结果
		s.seLog("[SE-PARSED] write_file → path=%q (%d chars) | content=%d chars\n",
			action.Path, len(action.Path), len(action.Content))
		// [TRUNCATION-DETECT] 检测内容是否被异常截断
		// 代码文件(.go/.py/.js/.ts等)内容少于30字节几乎肯定是截断
		if len(action.Content) > 0 && len(action.Content) < 30 {
			ext := strings.ToLower(filepath.Ext(action.Path))
			codeExts := map[string]bool{".go": true, ".py": true, ".js": true, ".ts": true, ".java": true, ".c": true, ".cpp": true, ".rs": true, ".rb": true, ".php": true}
			if codeExts[ext] {
				s.seLog("[SE-WARN] ⚠️ 可疑截断: write_file %s content仅 %d bytes! raw_args前200字符: %s\n",
					action.Path, len(action.Content), func() string {
						if len(argsStr) > 200 { return argsStr[:200] + "..." }
						return argsStr
					}())
			}
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
	case "fetch_url":
		if v, ok := args["url"].(string); ok {
			action.Command = v // 复用 Command 字段存 url
		}
		action.Type = "fetch_url"
	case "delete_file":
		if v, ok := args["path"].(string); ok {
			action.Path = v
		}
		action.Type = "delete_file"
	case "exec_session":
		if v, ok := args["command"].(string); ok {
			action.Command = v
		}
		action.Type = "exec_session"
	case "undo_file":
		if v, ok := args["path"].(string); ok {
			action.Path = v
		}
		action.Type = "undo_file"
	case "list_changes":
		action.Type = "list_changes"
	case "go_to_definition":
		if v, ok := args["path"].(string); ok {
			action.Path = v
		}
		if v, ok := args["line"].(float64); ok {
			action.Line = int(v)
		}
		if v, ok := args["column"].(float64); ok {
			action.Column = int(v)
		}
		action.Type = "go_to_definition"
	case "find_references":
		if v, ok := args["path"].(string); ok {
			action.Path = v
		}
		if v, ok := args["line"].(float64); ok {
			action.Line = int(v)
		}
		if v, ok := args["column"].(float64); ok {
			action.Column = int(v)
		}
		action.Type = "find_references"
	case "hover_info":
		if v, ok := args["path"].(string); ok {
			action.Path = v
		}
		if v, ok := args["line"].(float64); ok {
			action.Line = int(v)
		}
		if v, ok := args["column"].(float64); ok {
			action.Column = int(v)
		}
		action.Type = "hover_info"
	case "diagnostics":
		if v, ok := args["path"].(string); ok {
			action.Path = v
		}
		action.Type = "diagnostics"
	case "rename_symbol":
		if v, ok := args["path"].(string); ok {
			action.Path = v
		}
		if v, ok := args["line"].(float64); ok {
			action.Line = int(v)
		}
		if v, ok := args["column"].(float64); ok {
			action.Column = int(v)
		}
		if v, ok := args["new_name"].(string); ok {
			action.Command = v // 复用 Command 存 new_name
		}
		action.Type = "rename_symbol"
	case "analyze_image":
		if v, ok := args["path"].(string); ok {
			action.Path = v
		}
		if v, ok := args["prompt"].(string); ok {
			action.Command = v // 复用 Command 存 prompt
		}
		action.Type = "analyze_image"
	case "analyze_code":
		if v, ok := args["path"].(string); ok {
			action.Path = v
		}
		// 将 categories/min_level/max_issues 序列化到 Extra
		extra := make(map[string]interface{})
		if v, ok := args["categories"].([]interface{}); ok && len(v) > 0 {
			cats := make([]string, 0, len(v))
			for _, c := range v {
				if s, ok := c.(string); ok {
					cats = append(cats, s)
				}
			}
			extra["categories"] = cats
		}
		if v, ok := args["min_level"].(string); ok && v != "" {
			extra["min_level"] = v
		}
		if v, ok := args["max_issues"].(float64); ok {
			extra["max_issues"] = int(v)
		}
		if len(extra) > 0 {
			if data, err := json.Marshal(extra); err == nil {
				action.Extra = string(data)
			}
		}
		action.Type = "analyze_code"
	case "auto_debug":
		if v, ok := args["test_command"].(string); ok && v != "" {
			action.Command = v
		}
		if v, ok := args["target"].(string); ok && v != "" {
			action.Path = v
		}
		// 将 max_iterations, mode 序列化到 Extra
		extra := make(map[string]interface{})
		if v, ok := args["max_iterations"].(float64); ok {
			extra["max_iterations"] = int(v)
		}
		if v, ok := args["mode"].(string); ok && v != "" {
			extra["mode"] = v
		}
		if len(extra) > 0 {
			if data, err := json.Marshal(extra); err == nil {
				action.Extra = string(data)
			}
		}
		action.Type = "auto_debug"
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

	case "fetch_url":
		var args struct {
			URL string `json:"url"`
		}
		json.Unmarshal([]byte(argsJSON), &args)
		if args.URL == "" {
			return "错误: url参数为空"
		}
		return s.webFetch(args.URL)

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

	case "semantic_search":
		var args struct {
			Query string `json:"query"`
		}
		json.Unmarshal([]byte(argsJSON), &args)
		if args.Query == "" {
			return "错误: query参数为空"
		}
		if s.indexer == nil {
			if err := s.EnsureIndexer(); err != nil {
				return fmt.Sprintf("语义搜索索引构建失败: %v\n回退到普通搜索请用 search_files", err)
			}
		}
		return s.indexer.SemSearch(args.Query)

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

// searchEngine 定义搜索引擎接口
type searchEngine struct {
	Name string
	URL  func(query string) string
}

var searchEngines = []searchEngine{
	{
		Name: "DuckDuckGo",
		URL:  func(q string) string { return fmt.Sprintf("https://html.duckduckgo.com/html/?q=%s", url.QueryEscape(q)) },
	},
	{
		Name: "Bing",
		URL:  func(q string) string { return fmt.Sprintf("https://www.bing.com/search?q=%s", url.QueryEscape(q)) },
	},
	{
		Name: "Google",
		URL:  func(q string) string { return fmt.Sprintf("https://www.google.com/search?q=%s&hl=en", url.QueryEscape(q)) },
	},
}

// webSearch 并行搜索：同时查询所有引擎，取最先返回的结果
func (s *SEProcessor) webSearch(query string) string {
	client := &http.Client{Timeout: 15 * time.Second}
	maxResults := 5

	type engineResult struct {
		engineName string
		results    []searchResult
	}
	resultCh := make(chan engineResult, len(searchEngines))

	// 并行启动所有引擎
	for _, engine := range searchEngines {
		go func(eng searchEngine) {
			results, err := s.searchWithEngine(client, eng, query, maxResults)
			if err == nil && len(results) > 0 {
				resultCh <- engineResult{eng.Name, results}
			} else {
				fmt.Printf("[webSearch] ⚠️ %s 失败: %v\n", eng.Name, err)
			}
		}(engine)
	}

	// 等待第一个成功的结果，或全部失败
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()

	failedEngines := []string{}
	for range searchEngines {
		select {
		case r := <-resultCh:
			output := []string{fmt.Sprintf("🔍 搜索: %s (via %s)\n", query, r.engineName)}
			for i, res := range r.results {
				output = append(output, fmt.Sprintf("%d. %s\n   %s\n   📎 %s\n", i+1, res.Title, res.Snippet, res.URL))
			}
			return strings.Join(output, "\n")
		case <-timer.C:
			return fmt.Sprintf("搜索超时: 所有搜索引擎均无响应（DuckDuckGo / Bing / Google）。\n提示: 可尝试更具体的关键词，或使用 fetch_url 直接访问文档URL")
		}
	}
	_ = failedEngines

	return fmt.Sprintf("未找到 '%s' 的相关结果（已尝试 DuckDuckGo / Bing / Google）。\n提示: 可尝试更具体的关键词，或使用 fetch_url 直接访问文档URL", query)
}

type searchResult struct {
	Title   string
	Snippet string
	URL     string
}

// searchWithEngine 使用指定搜索引擎执行查询并解析结果
func (s *SEProcessor) searchWithEngine(client *http.Client, engine searchEngine, query string, maxResults int) ([]searchResult, error) {
	req, _ := http.NewRequest("GET", engine.URL(query), nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; ArgusSE/1.0)")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	html := string(body)

	var results []searchResult
	switch engine.Name {
	case "DuckDuckGo":
		results = s.parseDuckDuckGo(html, maxResults)
	case "Bing":
		results = s.parseBing(html, maxResults)
	case "Google":
		results = s.parseGoogle(html, maxResults)
	}

	if len(results) == 0 {
		return nil, fmt.Errorf("no results from %s", engine.Name)
	}
	return results, nil
}

// parseDuckDuckGo 解析 DuckDuckGo HTML 结果（含 URL）
func (s *SEProcessor) parseDuckDuckGo(html string, max int) []searchResult {
	var results []searchResult

	snippetRegex := regexp.MustCompile(`<a[^>]*class="result__snippet"[^>]*>(.*?)</a>`)
	titleRegex := regexp.MustCompile(`<a[^>]*class="result__a"[^>]*href="([^"]*)"[^>]*>(.*?)</a>`)

	snippets := snippetRegex.FindAllStringSubmatch(html, max)
	titles := titleRegex.FindAllStringSubmatch(html, max)

	count := len(titles)
	if count > len(snippets) {
		count = len(snippets)
	}

	for i := 0; i < count; i++ {
		results = append(results, searchResult{
			Title:   stripHTML(titles[i][2]),
			Snippet: stripHTML(snippets[i][1]),
			URL:     titles[i][1],
		})
	}
	return results
}

// parseBing 解析 Bing HTML 结果（含 URL）
func (s *SEProcessor) parseBing(html string, max int) []searchResult {
	var results []searchResult

	b_algoRegex := regexp.MustCompile(`(?si)<li[^>]*class="b_algo"[^>]*>(.*?)</li>`)
	algoBlocks := b_algoRegex.FindAllStringSubmatch(html, max)

	titleRegex := regexp.MustCompile(`(?si)<h2[^>]*>\s*<a[^>]*href="([^"]*)"[^>]*>(.*?)</a>\s*</h2>`)
	pRegex := regexp.MustCompile(`(?si)<p[^>]*>(.*?)</p>`)

	for _, block := range algoBlocks {
		if len(results) >= max {
			break
		}
		content := block[1]

		titleMatch := titleRegex.FindStringSubmatch(content)
		pMatch := pRegex.FindStringSubmatch(content)

		if titleMatch != nil {
			result := searchResult{
				Title:   stripHTML(titleMatch[2]),
				URL:     titleMatch[1],
			}
			if pMatch != nil {
				result.Snippet = stripHTML(pMatch[1])
				if len(result.Snippet) > 200 {
					result.Snippet = result.Snippet[:200] + "..."
				}
			}
			results = append(results, result)
		}
	}
	return results
}

// parseGoogle 解析 Google HTML 搜索结果
func (s *SEProcessor) parseGoogle(html string, max int) []searchResult {
	var results []searchResult

	// Google uses <h3> for titles with <a> inside, and various div patterns for snippets
	// Match result blocks: <a href="URL"><h3>Title</h3></a>...<div>Snippet</div>
	linkRegex := regexp.MustCompile(`(?si)<a[^>]*href="(/url\?q=[^"&]*)[^"]*"[^>]*>\s*<h3[^>]*>(.*?)</h3>\s*</a>`)
	snippetRegex := regexp.MustCompile(`(?si)<div[^>]*class="[^"]*BNeawe[^"]*s3v9rd[^"]*AP7Wnd[^"]*"[^>]*>(.*?)</div>`)

	links := linkRegex.FindAllStringSubmatch(html, -1)
	snippets := snippetRegex.FindAllStringSubmatch(html, -1)

	for i := 0; i < len(links) && len(results) < max; i++ {
		rawURL := strings.TrimPrefix(links[i][1], "/url?q=")
		// Strip Google tracking params
		if idx := strings.Index(rawURL, "&"); idx > 0 {
			rawURL = rawURL[:idx]
		}
		cleanURL, _ := url.QueryUnescape(rawURL)
		if !strings.HasPrefix(cleanURL, "http") {
			continue
		}

		result := searchResult{
			Title: stripHTML(links[i][2]),
			URL:   cleanURL,
		}
		if i < len(snippets) {
			result.Snippet = stripHTML(snippets[i][1])
			if len(result.Snippet) > 200 {
				result.Snippet = result.Snippet[:200] + "..."
			}
		}
		results = append(results, result)
	}

	// Fallback: try simpler parsing if the class-based regex didn't match
	if len(results) == 0 {
		simpleTitle := regexp.MustCompile(`(?si)<h3[^>]*>(.*?)</h3>`)
		simpleLink := regexp.MustCompile(`(?si)href="(https?://[^"]*)"`)
		titles := simpleTitle.FindAllStringSubmatch(html, max)
		allLinks := simpleLink.FindAllStringSubmatch(html, max)
		for i := 0; i < len(titles) && i < len(allLinks) && len(results) < max; i++ {
			results = append(results, searchResult{
				Title:   stripHTML(titles[i][1]),
				URL:     allLinks[i][1],
				Snippet: "",
			})
		}
	}

	return results
}

// stripHTML 移除HTML标签
func stripHTML(s string) string {
	re := regexp.MustCompile(`<[^>]*>`)
	return re.ReplaceAllString(s, "")
}

// webFetch 抓取网页内容并提取正文
func (s *SEProcessor) webFetch(urlStr string) string {
	if !strings.HasPrefix(urlStr, "http://") && !strings.HasPrefix(urlStr, "https://") {
		return "错误: URL必须以 http:// 或 https:// 开头"
	}

	client := &http.Client{Timeout: 20 * time.Second}
	req, err := http.NewRequest("GET", urlStr, nil)
	if err != nil {
		return fmt.Sprintf("请求创建失败: %v", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; ArgusSE/1.0)")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Sprintf("请求失败: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Sprintf("HTTP错误 %d: %s", resp.StatusCode, resp.Status)
	}

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024)) // 限制 2MB
	html := string(body)

	// 移除 script/style/noscript 标签及其内容
	for _, tag := range []string{"script", "style", "noscript", "head"} {
		re := regexp.MustCompile(`(?is)<` + tag + `[^>]*>.*?</` + tag + `>`)
		html = re.ReplaceAllString(html, "")
	}

	// 移除所有 HTML 标签
	html = stripHTML(html)

	// 解码 HTML 实体
	html = htmlpkg.UnescapeString(html)
	html = htmlpkg.UnescapeString(html) // double-unescape for &amp;lt; etc

	// 压缩空白
	html = regexp.MustCompile(`\s+`).ReplaceAllString(html, " ")

	// 截断
	const maxLen = 8000
	if len(html) > maxLen {
		html = html[:maxLen] + fmt.Sprintf("\n\n... (截断，原文共%d字符)", len(html))
	}

	return fmt.Sprintf("📄 抓取 %s (%d字符):\n\n%s", urlStr, len(html), strings.TrimSpace(html))
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

// compressHistory 分层智能压缩历史消息
// 升级前: 固定留15条 + 简单拼接摘要 → 长任务丢失关键决策上下文
// 升级后: 分层保留 → 指令层完整保留 / 工具结果压缩为1行 / 最近3轮完整对话 / token计数控制总长度
func (s *SEProcessor) compressHistory() {
	if len(s.history) <= 24 {
		return // 提升阈值：24条以下不压缩（原20）
	}

	// --- Layer 0: 统计 ---
	totalMsgs := len(s.history)
	var toolResultCount, userInstrCount, assistantCount int
	for _, msg := range s.history {
		switch msg.Role {
		case "tool":
			toolResultCount++
		case "user", "system":
			userInstrCount++
		case "assistant":
			assistantCount++
		}
	}

	// --- Layer 1: 保留最近3轮完整对话（6条: 3×assistant+tool）---
	keepRecentRounds := 3
	recentSize := keepRecentRounds * 2 // 每轮 = assistant + tool
	if recentSize > len(s.history) {
		recentSize = len(s.history)
	}
	recentMessages := s.history[len(s.history)-recentSize:]

	// --- Layer 2: 从旧消息中提取并分类 ---
	oldMessages := s.history[:len(s.history)-recentSize]

	// 2a. 用户/系统指令 — 完整保留关键词（这些是决策依据，不能丢）
	var keyInstructions []string
	for _, msg := range oldMessages {
		if msg.Role == "user" || msg.Role == "system" {
			content := strings.TrimSpace(msg.Content)
			// 过滤掉自动注入的 AddResult 反馈和工具结果
			if strings.HasPrefix(content, "✅") || strings.HasPrefix(content, "❌") ||
				strings.HasPrefix(content, "[文件:") || strings.HasPrefix(content, "[搜索]") ||
				strings.HasPrefix(content, "[上下文摘要]") || len(content) < 5 {
				continue
			}
			// 截断但保留完整意图
			if len(content) > 120 {
				content = content[:120] + "..."
			}
			keyInstructions = append(keyInstructions, content)
		}
	}
	// 只保留最后5条关键指令（最早的丢弃）
	if len(keyInstructions) > 5 {
		keyInstructions = keyInstructions[len(keyInstructions)-5:]
	}

	// 2b. 工具操作记录 — 压缩为1行摘要
	var actionSummaries []string
	var lastToolName string
	actionCount := 0
	errorCount := 0
	filesModified := make(map[string]bool) // 去重

	for _, msg := range oldMessages {
		switch msg.Role {
		case "assistant":
			if len(msg.ToolCalls) > 0 {
				for _, tc := range msg.ToolCalls {
					lastToolName = tc.Function.Name
					actionCount++
				}
			}
		case "tool":
			preview := strings.TrimSpace(msg.Content)
			// 提取文件路径和操作结果
			if strings.Contains(preview, "写入成功") || strings.Contains(preview, "written") {
				// 提取文件名
				if idx := strings.Index(preview, ":"); idx >= 0 && idx < len(preview)-1 {
					fname := strings.TrimSpace(preview[idx+1 : idx+min(60, len(preview)-idx)])
					if !filesModified[fname] {
						filesModified[fname] = true
						actionSummaries = append(actionSummaries, "写:"+fname)
					}
				}
			} else if strings.Contains(preview, "错误") || strings.Contains(preview, "error") ||
				strings.Contains(preview, "失败") || strings.Contains(preview, "failed") {
				errorCount++
				if len(preview) > 60 {
					preview = preview[:60]
				}
				actionSummaries = append(actionSummaries, "错:"+preview)
			} else if lastToolName == "exec" || lastToolName == "exec_session" {
				if len(preview) > 50 {
					preview = preview[:50]
				}
				actionSummaries = append(actionSummaries, "exec:"+preview)
			}
		}
	}

	// --- Layer 3: 构建结构化摘要 ---
	summaryParts := []string{}
	summaryParts = append(summaryParts, fmt.Sprintf("共%d轮/%d个工具调用", totalMsgs/2, actionCount))

	if len(filesModified) > 0 {
		var fList []string
		for f := range filesModified {
			fList = append(fList, f)
		}
		summaryParts = append(summaryParts, fmt.Sprintf("修改:%s", strings.Join(fList, ",")))
	}
	if errorCount > 0 {
		summaryParts = append(summaryParts, fmt.Sprintf("错误:%d次", errorCount))
	}
	if len(actionSummaries) > 8 {
		actionSummaries = actionSummaries[len(actionSummaries)-8:] // 只保留最近8条
	}
	if len(actionSummaries) > 0 {
		summaryParts = append(summaryParts, strings.Join(actionSummaries, "|"))
	}
	if len(keyInstructions) > 0 {
		summaryParts = append(summaryParts, fmt.Sprintf("指令:[%s]", strings.Join(keyInstructions, "; ")))
	}

	summary := fmt.Sprintf("[上下文摘要 v2] %s", strings.Join(summaryParts, " | "))

	// --- Layer 4: 组装新 history ---
	newHistory := make([]Message, 0, recentSize+len(keyInstructions)+2)

	// 4a. 摘要作为 system 消息插入头部
	newHistory = append(newHistory, Message{Role: "system", Content: summary})

	// 4b. 关键指令（如果不多的话也插入）
	for _, instr := range keyInstructions {
		newHistory = append(newHistory, Message{Role: "user", Content: instr})
	}

	// 4c. 最近N轮完整对话
	newHistory = append(newHistory, recentMessages...)

	s.history = newHistory

	// --- Token 估算与保护 ---
	estimatedTokens := s.estimateTokens()
	maxTokens := 32000 // 安全上限（大多数模型上下文窗口的 80% 左右）
	if estimatedTokens > maxTokens {
		// 二次压缩：只保留最近2轮 + 摘要
		s.history = append([]Message{s.history[0]}, s.history[len(s.history)-4:]...)
		fmt.Printf("[compressHistory] ⚠️ 二次压缩: %d tokens → 估计 ~%d tokens\n", estimatedTokens, s.estimateTokens())
	}
}

// estimateTokens 粗略估算当前 history 的 token 数量
// 规则: 中文≈2字符/token, 英文≈4字符/token, 平均≈3.5字符/token
func (s *SEProcessor) estimateTokens() int {
	totalChars := 0
	for _, msg := range s.history {
		totalChars += len(msg.Content)
		for _, tc := range msg.ToolCalls {
			totalChars += len(tc.Function.Name) + len(tc.Function.Arguments)
		}
	}
	// 粗略估算: ~3.5 chars per token (混合中英文)
	return totalChars * 10 / 35 // ≈ totalChars / 3.5
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

	// [P0-1] LSP 位置参数（行号和列号，0-based）
	Line   int `json:"line,omitempty"`
	Column int `json:"column,omitempty"`

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
	Extra           string   `json:"extra,omitempty"` // 额外JSON数据（如tags/code等）
}

type SECompletion struct {
	TechnicalNotes string `json:"technical_notes"`
	ChangelogDraft string `json:"changelog_draft"`
	Status         string `json:"status"`
}

// SemanticCheckResult 智能终止检测结果（多维度）
type SemanticCheckResult struct {
	IsComplete   bool    // 是否判定为完成
	Confidence  float64 // 置信度 0.0-1.0
	Reason       string  // 判定原因
	ActionScore  int     // 最近N轮动作有效性得分
}

// CheckSemanticComplete 智能终止检测：多维度评估SE是否真的完成了任务
// 升级前: 仅关键词匹配("完成"/"done") → 误判率高
// 升级后: 关键词 + 动作收敛检测 + complete_task 验证 + 内容质量评估 → 置信度评分
func (s *SEProcessor) CheckSemanticComplete(response string) *SemanticCheckResult {
	result := &SemanticCheckResult{Confidence: 0, ActionScore: 0}
	lower := strings.ToLower(response)

	// --- 维度1: 关键词信号 (基础权重 40%) ---
	strongKeywords := []string{
		"任务完成", "已完成", "task completed", "done", "finished",
	}
	weakKeywords := []string{
		"全部通过", "编译成功", "测试通过",
	}
	kwFound := 0
	hasStrongKW := false
	for _, kw := range strongKeywords {
		if strings.Contains(lower, kw) {
			kwFound++
			hasStrongKW = true
			result.Reason += fmt.Sprintf("[%s]", kw)
		}
	}
	for _, kw := range weakKeywords {
		if strings.Contains(lower, kw) {
			kwFound++
			result.Reason += fmt.Sprintf("[%s]", kw)
		}
	}
	if kwFound > 0 {
	 // 强关键词直接给高基础分，弱关键词补充
		base := 0.40
		if hasStrongKW {
			base = 0.50 // "任务完成"/"done" 等强关键词 → 基础0.5
		}
		bonus := math.Min(0.20, float64(kwFound)*0.05) // 每多一个关键词+0.05，上限+0.20
		result.Confidence += base + bonus
	}

	// --- 维度2: complete_task 动作验证 (权重 35%) ---
	// 检查最近几轮是否有 complete_task 调用
	hasCompleteTask := false
	recentRounds := 5
	startIdx := len(s.history) - recentRounds*2 // 每轮=assistant+tool
	if startIdx < 0 {
		startIdx = 0
	}
	for i := startIdx; i < len(s.history); i++ {
		msg := s.history[i]
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			for _, tc := range msg.ToolCalls {
				if tc.Function.Name == "complete_task" || strings.Contains(tc.Function.Name, "complete") {
					hasCompleteTask = true
					result.Reason += "[complete_task]"
				}
			}
		}
	}
	if hasCompleteTask {
		result.Confidence += 0.35
	}

	// --- 维度3: 动作收敛检测 (权重 25%) ---
	// 最近N轮如果只有 read/search/list/glob（只读操作）而无 write/edit/exec，说明在收尾
	readOnlyActions := 0
	writeOrExecActions := 0
	totalRecentActions := 0
	convergenceWindow := 8 // 检查最近8个工具调用
	actionCount := 0
	for i := len(s.history) - 1; i >= 0 && actionCount < convergenceWindow; i-- {
		msg := s.history[i]
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			for _, tc := range msg.ToolCalls {
				name := tc.Function.Name
				totalRecentActions++
				switch name {
				case "read_file", "search_files", "list_files", "glob", "semantic_search", "web_search":
					readOnlyActions++
				case "write_file", "edit_file", "exec", "exec_session", "delete_file":
					writeOrExecActions++
				case "complete_task", "run_tests", "git_operation":
					// 中性操作，不算收敛也不算发散
				}
			}
			actionCount++
		}
	}

	if totalRecentActions > 0 {
	 readOnlyRatio := float64(readOnlyActions) / float64(totalRecentActions)
	 // 只读比例高 + 有写操作历史 = 收尾阶段
	 if readOnlyRatio >= 0.7 && writeOrExecActions > 0 {
		 result.Confidence += 0.25
		 result.Reason += fmt.Sprintf("[收敛:%d/%d只读]", readOnlyActions, totalRecentActions)
	 } else if readOnlyRatio >= 0.9 && totalRecentActions >= 3 {
		 // 纯只读但无写操作 = 可能还没开始干，降低置信度
		 result.Confidence -= 0.15
		 result.Reason += "[纯只读-未执行]"
	 }
	 result.ActionScore = writeOrExecActions*10 + readOnlyActions
	}

	// --- 维度4: 内容质量/长度信号 (权重 10%) ---
	// 完成响应通常包含技术总结或变更说明，不是空话
	if len(response) > 50 {
		hasTechnicalContent := strings.Contains(lower, "function") ||
			strings.Contains(lower, "file") ||
			strings.Contains(lower, "test") ||
			strings.Contains(lower, "error") ||
			strings.Contains(lower, "fix") ||
			strings.Contains(lower, "implement") ||
			strings.Contains(lower, "修改") ||
			strings.Contains(lower, "实现") ||
			strings.Contains(lower, "修复")
		if hasTechnicalContent {
			result.Confidence += 0.10
			result.Reason += "[有技术内容]"
		}
	}

	// --- 判定阈值 ---
	// ≥0.6 高置信完成，0.4-0.6 边界（需要PM审核），<0.4 未完成
	if result.Confidence >= 0.60 {
		result.IsComplete = true
	} else if result.Confidence >= 0.40 {
	 // 边界情况：有关键词但动作不够收敛 → 标记为可能完成，让PM决定
	 result.IsComplete = true
	 result.Reason += "[边界-需PM确认]"
	} else {
		result.IsComplete = false
		if kwFound > 0 {
		 result.Reason += "[假阳性-关键词匹配但无实质动作]"
		} else {
		 result.Reason += "[未完成]"
		}
	}

	// 置信度钳制到 [0, 1]
	if result.Confidence < 0 {
		result.Confidence = 0
	}
	if result.Confidence > 1 {
		result.Confidence = 1
	}

	return result
}

func (s *SEProcessor) ResetHistory() {
	s.history = []Message{}
}

var SETools = []Tool{
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "semantic_search",
			Description: "语义搜索代码库。按功能意图查找代码（如'认证逻辑在哪'、'数据库连接怎么实现'），支持函数名/类型名/注释/document的语义匹配，返回排名结果和上下文片段。首次调用会自动构建项目索引。",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{
						"type":        "string",
						"description": "自然语言查询（如 '用户认证实现'、'JWT token生成'、'REST API路由定义'）",
					},
				},
				"required": []string{"query"},
			},
		},
	},
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
			Description: "在工作目录下执行命令。用于编译、运行程序、测试等。超时30秒。每次调用是独立进程，不保留状态。",
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
			Name:        "exec_session",
			Description: "在持久化shell中执行命令。保持工作目录(cd)和环境变量(set/export)状态。用于多步操作：cd到子目录后make、设置环境变量后编译、连续shell操作。超时60秒。",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"command": map[string]interface{}{
						"type":        "string",
						"description": "要执行的命令（如 cd build、set GOOS=linux、make、go build ./...）",
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
			Name:        "fetch_url",
			Description: "抓取单个网页内容并提取正文。用于获取文档、API参考、技术文章的完整内容。返回纯文本内容（去除HTML标签、脚本、样式），自动截断到8000字符。",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"url": map[string]interface{}{
						"type":        "string",
						"description": "要抓取的网页URL（如 'https://pkg.go.dev/net/http'）",
					},
				},
				"required": []string{"url"},
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
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "undo_file",
			Description: "撤销对指定文件的最近一次编辑，恢复到编辑前的内容。当编辑导致编译错误或逻辑问题时使用。每次调用撤销一步（类似Ctrl+Z）。",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "要撤销编辑的文件路径（相对于工作目录）",
					},
				},
				"required": []string{"path"},
			},
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "list_changes",
			Description: "列出所有可撤销的文件变更记录。显示每个文件的编辑历史（操作类型、时间、大小），帮助判断哪些修改可以回滚。",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{},
			},
		},
	},
	// ========== [P0-1] LSP 工具（gopls 驱动的精确代码理解） ==========
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "go_to_definition",
			Description: "跳转到符号的定义位置。传入文件路径和光标位置（行号从0开始，列号从0开始）。返回定义所在的文件、行号。用于理解类型/函数/变量的来源。",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "文件路径（相对于工作目录）",
					},
					"line": map[string]interface{}{
						"type":        "integer",
						"description": "行号（0-based）",
					},
					"column": map[string]interface{}{
						"type":        "integer",
						"description": "列号（0-based）",
					},
				},
				"required": []string{"path", "line", "column"},
			},
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "find_references",
			Description: "查找符号在项目中的所有引用位置。返回每个引用的文件和行号。用于理解函数被调用情况、变量使用范围等。",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "文件路径（相对于工作目录）",
					},
					"line": map[string]interface{}{
						"type":        "integer",
						"description": "行号（0-based）",
					},
					"column": map[string]interface{}{
						"type":        "integer",
						"description": "列号（0-based）",
					},
				},
				"required": []string{"path", "line", "column"},
			},
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "hover_info",
			Description: "获取光标位置的类型信息、签名和文档注释。相当于IDE的悬停提示。用于了解变量类型、函数签名、结构体字段等。",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "文件路径（相对于工作目录）",
					},
					"line": map[string]interface{}{
						"type":        "integer",
						"description": "行号（0-based）",
					},
					"column": map[string]interface{}{
						"type":        "integer",
						"description": "列号（0-based）",
					},
				},
				"required": []string{"path", "line", "column"},
			},
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "diagnostics",
			Description: "获取文件的编译诊断信息（错误、警告、提示）。包括编译错误、类型不匹配、未使用的导入等问题。用于在编辑后验证代码正确性。",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "要检查的文件路径（相对于工作目录）",
					},
				},
				"required": []string{"path"},
			},
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "rename_symbol",
			Description: "安全重命名符号（跨文件）。会自动更新所有引用该符号的位置。比手动搜索替换更安全可靠。适用于重命名函数、变量、类型等。",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "文件路径（相对于工作目录）",
					},
					"line": map[string]interface{}{
						"type":        "integer",
						"description": "行号（0-based，指向要重命名的符号）",
					},
					"column": map[string]interface{}{
						"type":        "integer",
						"description": "列号（0-based，指向要重命名的符号）",
					},
					"new_name": map[string]interface{}{
						"type":        "string",
						"description": "新的符号名称",
					},
				},
				"required": []string{"path", "line", "column", "new_name"},
			},
		},
	},
	// ========== [P0-3] 多模态工具 ==========
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "analyze_image",
			Description: "分析图片/截图/UI设计稿。支持 PNG/JPG/GIF/WebP 格式。可识别UI布局、错误截图、设计稿等，并生成对应代码或分析报告。需要 LLM 支持 vision 能力（如 GPT-4o/Claude-3/Gemini）。",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "图片路径（相对于工作目录）或URL（http://开头）",
					},
					"prompt": map[string]interface{}{
						"type":        "string",
						"description": "分析提示词（如'将这个UI转为React代码'、'分析这个错误截图'）",
					},
				},
				"required": []string{"path", "prompt"},
			},
		},
	},
	// ========== 代码片段库工具 ==========
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "search_snippet",
			Description: "搜索代码片段库。按关键词、语言、标签查找常用代码模板（HTTP server、CRUD API、认证中间件、数据库连接等）。支持模糊匹配和评分排序。",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{
						"type":        "string",
						"description": "搜索关键词（如 'http server', 'crud api', 'auth middleware'）",
					},
					"language": map[string]interface{}{
						"type":        "string",
						"description": "语言过滤（如 Go, Python, TypeScript），可选",
					},
					"tags": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "string"},
						"description": "标签过滤（如 ['web', 'api']），可选，任一匹配即可",
					},
				},
				"required": []string{"query"},
			},
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "add_snippet",
			Description: "添加自定义代码片段到片段库。片段会持久化保存到本地，重启后仍可使用。",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{
						"type":        "string",
						"description": "片段名称（如 'Go gRPC Server'）",
					},
					"language": map[string]interface{}{
						"type":        "string",
						"description": "编程语言（如 Go, Python, TypeScript）",
					},
					"description": map[string]interface{}{
						"type":        "string",
						"description": "片段描述（如 '基于gRPC的微服务模板'）",
					},
					"tags": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "string"},
						"description": "标签（如 ['grpc', 'microservice', 'server']），用于搜索匹配",
					},
					"code": map[string]interface{}{
						"type":        "string",
						"description": "代码内容（完整的可运行代码）",
					},
				},
				"required": []string{"name", "language", "description", "tags", "code"},
			},
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "list_snippets",
			Description: "列出代码片段库中的所有片段。支持按语言过滤，返回简要列表（名称、语言、描述、标签）。",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"language": map[string]interface{}{
						"type":        "string",
						"description": "语言过滤（如 Go, Python），可选，为空则列出全部",
					},
				},
			},
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "delete_snippet",
			Description: "删除自定义代码片段。只能删除用户添加的片段，内置模板不可删除。",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"id": map[string]interface{}{
						"type":        "string",
						"description": "要删除的片段ID（从 list_snippets 或 search_snippet 结果中获取）",
					},
				},
				"required": []string{"id"},
			},
		},
	},
	// ========== 代码分析工具 ==========
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "analyze_code",
			Description: "静态分析 Go 代码，检测潜在隐患和反模式。支持 AST 精确分析（nil 安全、错误处理、资源泄漏、并发安全、循环问题）+ 正则模式匹配（弱加密、命令注入风险、性能问题等）。返回结构化的问题报告，包含行号、严重程度、分类、修复建议。",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "要分析的文件或目录路径（如 main.go 或 ./internal/ai）",
					},
					"categories": map[string]interface{}{
						"type": "array",
						"items": map[string]interface{}{"type": "string"},
						"description": "检查的分类过滤，可选值: nil_safety, bounds, resource, concurrency, error_handling, logic, security, performance, style。不传则检查全部",
					},
					"min_level": map[string]interface{}{
						"type":        "string",
						"description": "最低严重程度: critical(仅严重), warning(严重+警告), info(含提示), hint(全部)。默认 hint",
					},
					"max_issues": map[string]interface{}{
						"type":        "number",
						"description": "最大返回问题数（0=不限），默认 50",
					},
				},
				"required": []string{"path"},
			},
		},
	},
	// ========== 自动调试工具 ==========
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "auto_debug",
			Description: "自动调试：运行测试→如果失败则AI分析错误原因→定位根因→生成修复代码→重新测试，循环直到通过或达到最大迭代次数。支持指定测试范围和最大循环次数。",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"test_command": map[string]interface{}{
						"type":        "string",
						"description": "测试命令（默认 'go test -v -count=1'）",
					},
					"target": map[string]interface{}{
						"type":        "string",
						"description": "测试目标包路径（如 './internal/ai/...' 或 './...'），默认当前目录",
					},
					"max_iterations": map[string]interface{}{
						"type":        "number",
						"description": "最大调试循环次数（1-5），默认 3",
					},
					"mode": map[string]interface{}{
						"type":        "string",
						"description": "模式: 'full'（跑测试+自动修复循环）或 'analyze'（仅分析，不修改代码），默认 full",
					},
				},
			},
		},
	},
}
