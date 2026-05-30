package core

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"argus/internal/ai"
	"argus/internal/executor"
)

// RoleState LabVIEW式角色状态（后面板控件值，前面板只读投影）
type RoleState struct {
	Phase     string `json:"phase"`               // idle, pm, se, ap, review, done, error
	PM        string `json:"pm"`                  // idle, busy
	SE        string `json:"se"`                  // idle, busy
	AP        string `json:"ap"`                  // idle, busy
	MC        bool   `json:"mc"`                  // C监控运行中
	Task      string `json:"task,omitempty"`      // 当前任务描述
	Progress  string `json:"progress,omitempty"`  // 进度信息
	UpdatedAt int64  `json:"updated_at"`          // 时间戳
}

type AICaller interface {
	ChatStream(ctx context.Context, systemPrompt string, history []ai.Message, userContent string, replyLanguage string, onChunk func(delta string)) (string, error)
}

type Phase int

const (
	PhaseAnalyze Phase = iota
	PhaseExecute
	PhaseReview
	PhaseApprove
)

type ProcessResult struct {
	Success  bool
	Actions  []ai.SEAction
	Outputs  []string
	Error    error
	Duration time.Duration
	Phases   []PhaseResult
}

type PhaseResult struct {
	Phase    Phase
	Role     Role
	Input    string
	Output   string
	Raw      string
	Duration time.Duration
}

type ReviewResult struct {
	Approved    bool
	Rejected    bool
	Reason      string
	DisplayText string
}

type APResult struct {
	Approved    bool
	Rejected    bool
	Reason      string
	DisplayText string
}

type ArgusCore struct {
	mu       sync.RWMutex
	client   AICaller
	executor *executor.Executor
	memory   *SharedMemory
	prompts  *PromptKit

	workDir  string
	language string

	onMessage      func(source, content string)
	onChunk        func(delta string)
	onStateChange  func(RoleState)

	ctx    context.Context
	cancel context.CancelFunc

	maxRetries int
	timeout    time.Duration

	state RoleState
}

func NewArgusCore(client AICaller, exec *executor.Executor, workDir string) *ArgusCore {
	ctx, cancel := context.WithCancel(context.Background())
	return &ArgusCore{
		client:     client,
		executor:   exec,
		memory:     NewSharedMemory(100),
		prompts:     NewPromptKit(workDir),
		workDir:    workDir,
		language:   "zh",
		ctx:        ctx,
		cancel:     cancel,
		maxRetries: 3,
		timeout:    120 * time.Second,
	}
}

func (c *ArgusCore) SetLanguage(lang string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.language = lang
}

func (c *ArgusCore) SetOnMessage(fn func(source, content string)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onMessage = fn
}

func (c *ArgusCore) SetOnChunk(fn func(delta string)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onChunk = fn
}

func (c *ArgusCore) SetContext(ctx context.Context) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ctx = ctx
}

func (c *ArgusCore) Cancel() {
	if c.cancel != nil {
		c.cancel()
	}
}

func (c *ArgusCore) emit(source, content string) {
	if c.onMessage != nil {
		c.onMessage(source, content)
	}
}

func (c *ArgusCore) emitStatus(phase, role, status string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.state.Phase = phase
	c.state.Task = phase
	switch role {
	case "pm":
		c.state.PM = status
	case "se":
		c.state.SE = status
	case "ap":
		c.state.AP = status
	}

	statusStr := fmt.Sprintf("phase:%s|role:%s|status:%s", phase, role, status)
	if c.onMessage != nil {
		c.onMessage("status", statusStr)
	}
	if c.onStateChange != nil {
		c.onStateChange(c.state)
	}
}

func (c *ArgusCore) SetOnStateChange(fn func(RoleState)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onStateChange = fn
}

func (c *ArgusCore) emitChunk(delta string) {
	if c.onChunk != nil {
		c.onChunk(delta)
	}
}

func (c *ArgusCore) callAI(role Role, prompt string, memoryContext string) (string, error) {
	systemPrompt := c.prompts.Get(role)
	return c.callAIWithPrompt(systemPrompt, string(role), prompt, memoryContext)
}

func (c *ArgusCore) callAIWithPrompt(systemPrompt, roleLabel, prompt, memoryContext string) (string, error) {
	if c.client == nil {
		return "", fmt.Errorf("AICaller is nil")
	}

	history := buildHistoryFromMemory(c.memory.GetAll())

	fullPrompt := prompt
	if memoryContext != "" {
		fullPrompt = fmt.Sprintf("[Context from previous phases]\n%s\n\n[Current Task]\n%s", memoryContext, prompt)
	}

	start := time.Now()

	var response string
	var err error

	callCtx, callCancel := context.WithTimeout(c.ctx, c.timeout)
	response, err = c.client.ChatStream(callCtx, systemPrompt, history, fullPrompt, c.language, func(delta string) {
		c.emitChunk(delta)
	})
	callCancel()

	duration := time.Since(start)
	fmt.Printf("[Core:%s] AI call completed in %v (len=%d, err=%v)\n", roleLabel, duration, len(response), err)

	return response, err
}

func buildHistoryFromMemory(entries []MemoryEntry) []ai.Message {
	msgs := make([]ai.Message, 0, len(entries))
	for _, e := range entries {
		role := mapRoleToStandard(e.Role)
		msgs = append(msgs, ai.Message{
			Role:    role,
			Content: e.Content,
		})
	}
	return msgs
}

func mapRoleToStandard(role Role) string {
	switch role {
	case RoleUser:
		return "user"
	case RolePM, RoleSE, RoleAP:
		return "assistant"
	case RoleSys:
		return "system"
	default:
		return "user"
	}
}

func (c *ArgusCore) Process(userMsg string) *ProcessResult {
	totalStart := time.Now()
	result := &ProcessResult{
		Phases: make([]PhaseResult, 0, 3),
	}

	defer func() {
		result.Duration = time.Since(totalStart)
	}()

	c.emitStatus("start", "pm", "busy")

	c.memory.Add(RoleUser, userMsg)

	pmResponse, pmErr := c.callAI(RolePM, userMsg, "")
	phasePM := PhaseResult{
		Phase:    PhaseAnalyze,
		Role:     RolePM,
		Input:    userMsg,
		Output:   pmResponse,
		Raw:      pmResponse,
		Duration: 0,
	}
	result.Phases = append(result.Phases, phasePM)

	if pmErr != nil {
		result.Error = fmt.Errorf("PM analysis failed: %w", pmErr)
		c.emit("pm_to_user", fmt.Sprintf("@USR PM分析失败: %v", pmErr))
		return result
	}

	isProgramming, taskDesc := c.parsePMResponse(pmResponse)
	c.memory.Add(RolePM, pmResponse)

	displayText := c.extractDisplayText(pmResponse)
	c.emit("pm_to_user", displayText)

	if !isProgramming {
		c.emit("pm_to_user", displayText)
		c.emitStatus("done", "none", "idle")
		result.Success = true
		return result
	}

	c.emit("pm_to_se", taskDesc)
	c.emitStatus("execute", "se", "busy")

	seCtx := c.memory.FormatForPrompt()
	seResponse, seErr := c.callAI(RoleSE, taskDesc, seCtx)
	phaseSE := PhaseResult{
		Phase:    PhaseExecute,
		Role:     RoleSE,
		Input:    taskDesc,
		Output:   seResponse,
		Raw:      seResponse,
		Duration: 0,
	}
	result.Phases = append(result.Phases, phaseSE)

	if seErr != nil {
		result.Error = fmt.Errorf("SE execution failed: %w", seErr)
		c.emit("se_to_user", fmt.Sprintf("@USR SE执行失败: %v", seErr))
		return result
	}

	c.memory.Add(RoleSE, seResponse)

	actions, completed := c.parseSEResponse(seResponse)

	if completed {
		c.emit("se_to_user", fmt.Sprintf("@PM ✅ %s", c.extractCompletedSummary(seResponse)))
		c.emitStatus("done", "none", "idle")
		result.Success = true
		return result
	}

	if len(actions) == 0 {
		for attempt := 1; attempt <= c.maxRetries; attempt++ {
			fmt.Printf("[Core:SE] Empty actions retry #%d\n", attempt)
			fixPrompt := c.prompts.GetFix("Empty actions returned", userMsg)
			seResponse, seErr = c.callAI(RoleSE, fixPrompt, seCtx)
			if seErr != nil {
				continue
			}
			actions, completed = c.parseSEResponse(seResponse)
			if len(actions) > 0 || completed {
				break
			}
		}
	}

	if len(actions) == 0 && !completed {
		result.Error = fmt.Errorf("SE returned no valid actions after retries")
		c.emit("se_to_user", "@USR SE无法生成有效操作")
		return result
	}

	c.emit("se_to_user", fmt.Sprintf("🔧 执行 %d 个操作...", len(actions)))

	execResults, execErr := c.executeActions(actions)
	if execErr != nil {
		result.Error = fmt.Errorf("execution failed: %w", execErr)

		fixPrompt := c.prompts.GetFix(execErr.Error(), seResponse)
		fixResponse, fixErr := c.callAI(RoleSE, fixPrompt, c.memory.FormatForPrompt())
		if fixErr == nil {
			fixActions, _ := c.parseSEResponse(fixResponse)
			if len(fixActions) > 0 {
				execResults, execErr = c.executeActions(fixActions)
				if execErr == nil {
					result.Error = nil
				}
			}
		}
	}

	result.Outputs = execResults
	result.Actions = actions

	seDisplay := c.extractDisplayText(seResponse)
	c.emit("se_to_user", seDisplay)

	if result.Error != nil {
		c.emitStatus("error", "se", "idle")
		return result
	}

	c.emit("se_to_user", "✅ SE execution completed, submitting for PM review...")
	c.memory.Add(RoleSE, fmt.Sprintf("SE completed. Actions: %d, Results: %v", len(actions), execResults))

	// --- Phase 3: PM Code Review ---
	c.emitStatus("review", "pm", "busy")
	c.emit("review_start", "📋 PM reviewing SE's work...")

	reviewCtx := fmt.Sprintf("[User Request] %s\n[SE Actions] %v\n[SE Results] %v\n[SE Response] %s",
		userMsg, actions, execResults, seResponse)

	reviewPrompt := fmt.Sprintf("Review the SE's work above. Check code quality, correctness, and completeness.\n\nFiles changed: %v\nExecution results: %v\n\nDecide: approve or reject with specific reasons.",
		getFilePaths(actions), execResults)

	pmReviewResponse, reviewErr := c.callAIWithPrompt(c.prompts.PMReview, "pm_review", reviewPrompt, reviewCtx)
	phaseReview := PhaseResult{
		Phase:  PhaseReview,
		Role:   RolePM,
		Input:  reviewPrompt,
		Output: pmReviewResponse,
		Raw:    pmReviewResponse,
	}
	result.Phases = append(result.Phases, phaseReview)

	if reviewErr != nil {
		c.emit("pm_review", fmt.Sprintf("@USR PM review error: %v (auto-approve)", reviewErr))
		pmReviewResponse = "auto-approved"
	} else {
		c.memory.Add(RolePM, pmReviewResponse)
	}

	reviewResult := c.parseReviewResponse(pmReviewResponse)
	c.emit("pm_review", reviewResult.DisplayText)

	if reviewResult.Rejected {
		c.emit("pm_review", fmt.Sprintf("@SE ❌ PM rejected: %s", reviewResult.Reason))
		c.emitStatus("error", "pm", "idle")
		result.Error = fmt.Errorf("PM review rejected: %s", reviewResult.Reason)
		return result
	}

	// --- Phase 4: AP Approval (OA) ---
	c.emitStatus("approve", "ap", "busy")
	c.emit("ap_start", "🔒 AP final approval...")

	apCtx := c.memory.FormatForPrompt()
	apPrompt := fmt.Sprintf("Final approval for task: %s\nSE executed %d actions successfully.\nPM review approved.\n\nPerform final quality and security check. Approve or reject.",
		userMsg, len(actions))

	apResponse, apErr := c.callAI(RoleAP, apPrompt, apCtx)
	phaseAP := PhaseResult{
		Phase:  PhaseApprove,
		Role:   RoleAP,
		Input:  apPrompt,
		Output: apResponse,
		Raw:    apResponse,
	}
	result.Phases = append(result.Phases, phaseAP)

	if apErr != nil {
		c.emit("ap_result", fmt.Sprintf("@USR AP error: %v (auto-approve)", apErr))
		apResponse = "auto-approved"
	} else {
		c.memory.Add(RoleAP, apResponse)
	}

	apResult := c.parseAPResponse(apResponse)
	c.emit("ap_result", apResult.DisplayText)

	if apResult.Rejected {
		c.emit("ap_result", fmt.Sprintf("@SE ❌ AP rejected: %s", apResult.Reason))
		c.emitStatus("error", "ap", "idle")
		result.Error = fmt.Errorf("AP rejected: %s", apResult.Reason)
		return result
	}

	c.emit("ap_result", "✅ 交付完成！PM Review + AP Approval 全部通过")
	c.emitStatus("done", "none", "idle")
	result.Success = true
	return result
}

func (c *ArgusCore) parsePMResponse(response string) (bool, string) {
	lower := strings.ToLower(response)

	jsonIdx := strings.Index(lower, `{"is_programming":true`)
	if jsonIdx != -1 {
		endIdx := strings.Index(response[jsonIdx:], "}")
		if endIdx != -1 {
			jsonStr := response[jsonIdx : jsonIdx+endIdx+1]
			var task struct {
				Task        string `json:"task"`
				IsProgramming bool  `json:"is_programming"`
				Files       []string `json:"files"`
			}
			if json.Unmarshal([]byte(jsonStr), &task) == nil && task.IsProgramming {
				return true, task.Task
			}
		}
	}

	keywords := []string{"创建", "写一个", "implement", "create", "write", "build", "开发", "编程", ".go", ".py", ".js", ".ts"}
	for _, kw := range keywords {
		if strings.Contains(lower, kw) {
			return true, response
		}
	}

	return false, response
}

func (c *ArgusCore) parseSEResponse(response string) ([]ai.SEAction, bool) {
	fmt.Printf("[parseSE] raw_len=%d first_400=%q\n", len(response), func() string {
		if len(response) > 400 { return response[:400] }
		return response
	}())
	jsonIdx := strings.Index(response, `"actions"`)
	if jsonIdx == -1 {
		jsonIdx = strings.Index(response, `"task_status"`)
	}

	if jsonIdx != -1 {
		bracketStart := strings.Index(response[jsonIdx:], "[")
		if bracketStart == -1 {
			bracketStart = strings.Index(response[jsonIdx:], "{")
		}
		if bracketStart != -1 {
			searchFrom := response[jsonIdx+bracketStart:]
			bracketEnd := findMatchingBracket(searchFrom)
			if bracketEnd > 0 {
				jsonStr := searchFrom[:bracketEnd+1]
				var actionList []map[string]interface{}
				if err := json.Unmarshal([]byte(jsonStr), &actionList); err == nil {
					actions := make([]ai.SEAction, 0, len(actionList))
					for _, a := range actionList {
						action := ai.SEAction{
							Type: fmt.Sprintf("%v", a["type"]),
						}
						if v, ok := a["path"].(string); ok {
							action.Path = v
						}
						if v, ok := a["content"].(string); ok {
							action.Content = v
						}
						if v, ok := a["command"].(string); ok {
							action.Command = v
						}
						if v, ok := a["old_str"].(string); ok {
							action.OldStr = v
						}
						if v, ok := a["new_str"].(string); ok {
							action.NewStr = v
						}
						actions = append(actions, action)
					}
					if len(actions) > 0 {
						return actions, false
					}
				} else {
					fixedJSON := fixMalformedSEJSON(jsonStr)
					var parseErr error
					if parseErr = json.Unmarshal([]byte(fixedJSON), &actionList); parseErr == nil {
						actions := make([]ai.SEAction, 0, len(actionList))
						for _, a := range actionList {
							action := ai.SEAction{Type: fmt.Sprintf("%v", a["type"])}
							if v, ok := a["path"].(string); ok { action.Path = v }
							if v, ok := a["content"].(string); ok { action.Content = v }
							if v, ok := a["command"].(string); ok { action.Command = v }
							if v, ok := a["old_str"].(string); ok { action.OldStr = v }
							if v, ok := a["new_str"].(string); ok { action.NewStr = v }
							actions = append(actions, action)
						}
						if len(actions) > 0 {
							fmt.Printf("[parseSE] ✅ JSON fix success! %d actions\n", len(actions))
							return actions, false
						}
					}
					fmt.Printf("[parseSE] JSON parse failed: %v | fixed: %v\n", err, parseErr)
				}
			}
		}
	}

	completedIdx := strings.Index(strings.ToLower(response), `"task_status":"completed"`)
	if completedIdx != -1 {
		return nil, true
	}
	completedIdx2 := strings.Index(strings.ToLower(response), `"status":"completed"`)
	if completedIdx2 != -1 {
		return nil, true
	}

	if fallbackActions := extractActionsFromText(response); len(fallbackActions) > 0 {
		fmt.Printf("[parseSE] ✅ Fallback text extraction found %d actions\n", len(fallbackActions))
		return fallbackActions, false
	}

	return nil, false
}

func extractActionsFromText(response string) []ai.SEAction {
	var actions []ai.SEAction

	rePath := regexp.MustCompile(`"path"\s*:\s*"([^"]+)"`)
	reContent := regexp.MustCompile(`"content"\s*:\s*"((?:[^"\\]|\\.)*)"`)
	reCommand := regexp.MustCompile(`"command"\s*:\s*"([^"]+)"`)
	reType := regexp.MustCompile(`"type"\s*:\s*"([^"]+)"`)
	reGoFile := regexp.MustCompile(`"(\w+\.go)"|(\w+\.go)`)
	reExecCmd := regexp.MustCompile(`(?:go run |python |npm )([^\s"]+)`)

	paths := rePath.FindAllStringSubmatch(response, -1)
	contents := reContent.FindAllStringSubmatch(response, -1)
	commands := reCommand.FindAllStringSubmatch(response, -1)
	types := reType.FindAllStringSubmatch(response, -1)

	maxItems := max(len(paths), len(contents), len(commands))
	if maxItems == 0 {
		goFiles := reGoFile.FindAllStringSubmatch(response, -1)
		execMatches := reExecCmd.FindAllStringSubmatch(response, -1)

		for _, gf := range goFiles {
			filename := gf[1]
			if filename == "" {
				filename = gf[2]
			}
			actions = append(actions, ai.SEAction{Type: "write_file", Path: filename})
		}
		for _, em := range execMatches {
			cmd := em[0]
			if !strings.Contains(cmd, "run ") {
				cmd = "go run " + cmd
			}
			actions = append(actions, ai.SEAction{Type: "exec", Command: cmd})
		}

		if len(actions) > 0 {
			return inferMissingFields(actions, response)
		}
		return nil
	}

	for i := 0; i < maxItems; i++ {
		action := ai.SEAction{}
		if i < len(types) && types[i][1] != "" {
			action.Type = types[i][1]
		}
		if i < len(paths) && paths[i][1] != "" {
			action.Path = paths[i][1]
		}
		if i < len(contents) && contents[i][1] != "" {
			action.Content = unescapeJSONString(contents[i][1])
		}
		if i < len(commands) && commands[i][1] != "" {
			action.Command = commands[i][1]
		}

		if action.Type == "" && action.Path != "" {
			action.Type = "write_file"
		}
		if action.Type == "" && action.Command != "" {
			action.Type = "exec"
		}

		if action.Type != "" {
			actions = append(actions, action)
		}
	}

	return inferMissingFields(actions, response)
}

func inferMissingFields(actions []ai.SEAction, rawResponse string) []ai.SEAction {
	hasCode := strings.Contains(rawResponse, "package main") ||
		strings.Contains(rawResponse, "import \"fmt\"") ||
		strings.Contains(rawResponse, "func main")

	for i := range actions {
		a := &actions[i]

		if a.Type == "write_file" && a.Content == "" && hasCode {
			reCode := regexp.MustCompile(`package main[\s\S]*?(?:\n\s*\})?`)
			codeMatch := reCode.FindString(rawResponse)
			if codeMatch != "" {
				a.Content = cleanExtractedCode(codeMatch)
			} else {
				a.Content = `package main

import "fmt"

func main() {
    fmt.Println("Hello, World!")
}`
			}
		}

		if a.Type == "exec" && a.Command == "" && a.Path != "" {
			ext := filepath.Ext(a.Path)
			switch ext {
			case ".go":
				a.Command = "go run " + a.Path
			case ".py":
				a.Command = "python " + a.Path
			case ".js":
				a.Command = "node " + a.Path
			default:
				a.Command = "type " + a.Path
			}
		}

		if a.Type == "" || (a.Type == "write_file" && a.Path == "") {
			continue
		}
	}

	validActions := make([]ai.SEAction, 0, len(actions))
	for _, a := range actions {
		if a.Type != "" && ((a.Type == "write_file" && a.Path != "") || (a.Type == "exec" && a.Command != "")) {
			validActions = append(validActions, a)
		}
	}

	return validActions
}

func cleanExtractedCode(code string) string {
	code = strings.TrimSpace(code)
	code = regexp.MustCompile(`\\n`).ReplaceAllString(code, "\n")
	code = regexp.MustCompile(`\\"`).ReplaceAllString(code, "\"")
	return code
}

func unescapeJSONString(s string) string {
	s = strings.ReplaceAll(s, "\\n", "\n")
	s = strings.ReplaceAll(s, "\\t", "\t")
	s = strings.ReplaceAll(s, "\\\"", "\"")
	s = strings.ReplaceAll(s, "\\\\", "\\")
	return s
}

func fixMalformedSEJSON(jsonStr string) string {
	fixed := jsonStr

	reUnquotedValue := regexp.MustCompile(`"([a-z_]+)":([a-zA-Z][a-zA-Z0-9_.]*)`)
	fixed = reUnquotedValue.ReplaceAllStringFunc(fixed, func(match string) string {
		parts := reUnquotedValue.FindStringSubmatch(match)
		if len(parts) == 3 {
			return fmt.Sprintf(`"%s":"%s"`, parts[1], parts[2])
		}
		return match
	})

	reStuckKey := regexp.MustCompile(`"(type|path|content|command)([a-zA-Z/_.])`)
	fixed = reStuckKey.ReplaceAllString(fixed, `"$1":"$2`)

	reEmptyKey := regexp.MustCompile(`"":\s*"([^"]{10,})"`)
	fixed = reEmptyKey.ReplaceAllStringFunc(fixed, func(match string) string {
		parts := reEmptyKey.FindStringSubmatch(match)
		if len(parts) >= 2 && (strings.Contains(parts[1], ".go") || strings.Contains(parts[1], ".py") || strings.Contains(parts[1], "package")) {
			return `"path":"` + parts[1] + `"`
		} else if len(parts) >= 2 && (strings.Contains(parts[1], "func ") || strings.Contains(parts[1], "import ") || strings.Contains(parts[1], "fmt.")) {
			return `"content":"` + parts[1] + `"`
		} else if len(parts) >= 2 && (parts[1] == "exec" || strings.Contains(parts[1], "go run") || strings.Contains(parts[1], "python ")) {
			return `"command":"` + parts[1] + `"`
		}
		return match
	})

	fmt.Printf("[fixMalformedSEJSON] input_len=%d output_len=%d\n", len(jsonStr), len(fixed))
	return fixed
}

func findMatchingBracket(s string) int {
	count := 0
	for i, ch := range s {
		switch ch {
		case '[':
			count++
		case ']':
			count--
			if count == 0 {
				return i
			}
		case '{':
			count++
		case '}':
			count--
			if count == 0 {
				return i
			}
		}
	}
	return -1
}

func (c *ArgusCore) executeActions(actions []ai.SEAction) ([]string, error) {
	outputs := make([]string, 0, len(actions))

	for i, action := range actions {
		fmt.Printf("[Core:Exec] Action %d/%d: type=%s\n", i+1, len(actions), action.Type)

		var output string
		var err error

		switch action.Type {
		case "write_file":
			err = c.executor.WriteFile(action.Path, action.Content)
			if err == nil {
				output = fmt.Sprintf("✅ write_file %s (%d bytes)", action.Path, len(action.Content))
			} else {
				output = fmt.Sprintf("❌ write_file %s error: %v", action.Path, err)
			}
		case "exec":
			output, err = c.executor.Exec(action.Command, 60*time.Second)
			if err != nil {
				output = fmt.Sprintf("❌ exec '%s' error: %v\noutput: %s", action.Command, err, output)
			} else {
				output = fmt.Sprintf("✅ exec '%s'\n%s", action.Command, output)
			}
		case "edit_file":
			_, err = c.executor.EditFile(action.Path, action.OldStr, action.NewStr)
			if err == nil {
				output = fmt.Sprintf("✅ edit_file %s", action.Path)
			} else {
				output = fmt.Sprintf("❌ edit_file %s error: %v", action.Path, err)
			}
		case "search_files":
			files, listErr := c.executor.ListFiles()
			if listErr != nil {
				output = fmt.Sprintf("❌ search_files error: %v", listErr)
			} else {
				output = fmt.Sprintf("📁 Found %d files", len(files))
				for _, f := range files {
					output += "\n  - " + f.Path
				}
			}
		default:
			err = fmt.Errorf("unknown action type: %s", action.Type)
			output = fmt.Sprintf("❌ unknown action: %s", action.Type)
		}

		outputs = append(outputs, output)
		c.emit("se_to_user", output)

		if err != nil {
			return outputs, fmt.Errorf("action %d (%s) failed: %w", i, action.Type, err)
		}
	}

	return outputs, nil
}

func (c *ArgusCore) extractDisplayText(response string) string {
	lines := strings.Split(response, "\n")
	var displayLines []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "{") && strings.HasSuffix(line, "}") {
			continue
		}
		if strings.HasPrefix(line, "@") {
			continue
		}
		displayLines = append(displayLines, line)
		if len(displayLines) >= 3 {
			break
		}
	}
	return strings.Join(displayLines, "\n")
}

func (c *ArgusCore) extractCompletedSummary(response string) string {
	idx := strings.Index(response, `"summary"`)
	if idx != -1 {
		start := strings.Index(response[idx:], `:`) + 1
		end := strings.Index(response[start+idx:], `"`)
		if end > 0 {
			return response[start : start+end]
		}
	}
	return "任务已完成"
}

func (c *ArgusCore) GetMemory() *SharedMemory {
	return c.memory
}

func (c *ArgusCore) ClearMemory() {
	c.memory.Clear()
}

func (c *ArgusCore) Stats() map[string]interface{} {
	return map[string]interface{}{
		"memory_entries": c.memory.Len(),
		"work_dir":       c.workDir,
		"language":       c.language,
	}
}

func (c *ArgusCore) parseReviewResponse(response string) ReviewResult {
	lower := strings.ToLower(response)

	if strings.Contains(lower, `"reject"`) ||
		strings.Contains(lower, `"review_result":"reject"`) ||
		strings.Contains(lower, `"approval":"reject"`) {
		reason := extractJSONValue(response, "reason")
		if reason == "" {
			reason = extractJSONValue(response, "review_comment")
		}
		if reason == "" {
			reason = "PM review rejected"
		}
		return ReviewResult{
			Rejected:    true,
			Reason:      reason,
			DisplayText: fmt.Sprintf("@USR 📋 PM Code Review ❌ REJECTED: %s", reason),
		}
	}

	return ReviewResult{
		Approved:    true,
		DisplayText: "@USR 📋 PM Code Review ✅ APPROVED",
	}
}

func (c *ArgusCore) parseAPResponse(response string) APResult {
	lower := strings.ToLower(response)

	if strings.Contains(lower, `"reject"`) ||
		strings.Contains(lower, `"approval_result":"reject"`) ||
		strings.Contains(lower, `"approval":"reject"`) {
		reason := extractJSONValue(response, "reason")
		if reason == "" {
			reason = extractJSONValue(response, "critical_issues")
		}
		if reason == "" {
			reason = "AP rejected"
		}
		return APResult{
			Rejected:    true,
			Reason:      reason,
			DisplayText: fmt.Sprintf("@USR 🔒 AP Approval ❌ REJECTED: %s", reason),
		}
	}

	return APResult{
		Approved:    true,
		DisplayText: "@USR 🔒 AP Approval ✅ PASSED",
	}
}

func extractJSONValue(jsonStr, key string) string {
	patterns := []string{
		fmt.Sprintf(`"%s"`, key) + `:\s*"`,
		fmt.Sprintf(`"%s"`, key) + `"\s*:\s*"`,
	}
	for _, pat := range patterns {
		re := regexp.MustCompile(pat)
		loc := re.FindStringIndex(jsonStr)
		if loc != nil {
			rest := jsonStr[loc[1]:]
			end := strings.Index(rest, `"`)
			if end > 0 {
				return rest[:end]
			}
		}
	}
	return ""
}

func getFilePaths(actions []ai.SEAction) []string {
	paths := make([]string, 0, len(actions))
	for _, a := range actions {
		if a.Path != "" {
			paths = append(paths, a.Path)
		}
	}
	return paths
}
