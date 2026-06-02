package ai

import (
	"context"
	"encoding/json"
	"fmt"
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

工作流程：
1. 分析任务 → write_file 写代码 → exec 编译/运行 → read_file 确认结果
2. 验证失败 → 分析错误 → edit_file 修复 → exec 再测试
3. 全部通过 → complete_task 标记完成

注意事项：
- 工作目录内的操作直接执行，目录外操作（安装软件等）需告知PM
- 危险操作（git reset --hard等）需先确认
- 如果 go.mod 已存在，不要执行 go mod init
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

	if len(s.history) > 20 {
		s.history = s.history[len(s.history)-20:]
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
			Path string `json:"path"`
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
		return string(content)

	case "list_files":
		entries, err := os.ReadDir(s.workDir)
		if err != nil {
			return fmt.Sprintf("读取目录失败: %v", err)
		}
		var names []string
		for _, e := range entries {
			if e.IsDir() {
				names = append(names, e.Name()+"/")
			} else {
				names = append(names, e.Name())
			}
		}
		return strings.Join(names, "\n")

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
		return s.searchFiles(args.Pattern, args.FilePattern, args.IsRegex, args.CaseInsensitive)

	default:
		return fmt.Sprintf("工具 %s: 由executor执行", name)
	}
}

func (s *SEProcessor) searchFiles(pattern, filePattern string, isRegex, caseInsensitive bool) string {
	globPattern := filePattern
	if globPattern == "" {
		globPattern = "*"
	}

	matches, err := filepath.Glob(filepath.Join(s.workDir, globPattern))
	if err != nil {
		return fmt.Sprintf("搜索失败: %v", err)
	}

	var results []string
	for _, filePath := range matches {
		content, err := os.ReadFile(filePath)
		if err != nil {
			continue
		}
		lines := strings.Split(string(content), "\n")
		for i, line := range lines {
			haystack := line
			needle := pattern
			if caseInsensitive {
				haystack = strings.ToLower(haystack)
				needle = strings.ToLower(needle)
			}
			if isRegex {
				matched, _ := regexp.MatchString(needle, haystack)
				if matched {
					relPath, _ := filepath.Rel(s.workDir, filePath)
					results = append(results, fmt.Sprintf("%s:%d: %s", relPath, i+1, strings.TrimSpace(line)))
				}
			} else {
				if strings.Contains(haystack, needle) {
					relPath, _ := filepath.Rel(s.workDir, filePath)
					results = append(results, fmt.Sprintf("%s:%d: %s", relPath, i+1, strings.TrimSpace(line)))
				}
			}
		}
	}

	if len(results) == 0 {
		return "未找到匹配内容"
	}
	return strings.Join(results, "\n")
}

func (s *SEProcessor) AddResult(result string) {
	s.history = append(s.history, Message{Role: "user", Content: result})
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
			Description: "读取文件内容，用于查看和审核代码。",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "文件路径（相对于工作目录）",
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
			Description: "列出工作目录下的文件，用于了解当前项目结构。",
			Parameters: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
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
