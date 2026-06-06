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

// ThoughtEvent AI思考链事件（用于前端Dashboard展示）
type ThoughtEvent struct {
	Type      string                 `json:"type"`               // "thinking" | "step" | "tool_start" | "tool_done"
	Role      string                 `json:"role"`               // "pm" | "se" | "ap"
	Content   string                 `json:"content,omitempty"`  // 思考内容/步骤描述/工具输出
	Timestamp int64                  `json:"timestamp"`          // Unix秒
	Metadata  map[string]interface{} `json:"meta,omitempty"`      // 扩展(工具名/步骤号/耗时等)
}

type AICaller interface {
	ChatStream(ctx context.Context, systemPrompt string, history []ai.Message, userContent string, replyLanguage string, onChunk func(delta string), onThought func(evt map[string]interface{})) (string, error)
	ChatWithTools(ctx context.Context, systemPrompt string, history []ai.Message, userContent string, tools []ai.Tool) (*ai.ChatResponse, error)
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
	todo     *TodoManager  // 动态任务列表管理器

	workDir  string
	language string

	onMessage      func(source, content string)
	onChunk        func(delta string)
	onThought      func(evt map[string]interface{}) // 思考链回调（Dashboard可视化）
	onStateChange  func(RoleState)
	onActionEvent  func(eventName string, data interface{})

	ctx    context.Context
	cancel context.CancelFunc

	maxRetries int
	timeout    time.Duration

	state RoleState
}

func NewArgusCore(client AICaller, exec *executor.Executor, workDir string) *ArgusCore {
	ctx, cancel := context.WithCancel(context.Background())
	core := &ArgusCore{
		client:     client,
		executor:   exec,
		memory:     NewSharedMemory(100),
		prompts:     NewPromptKit(workDir),
		todo:       NewTodoManager(),
		workDir:    workDir,
		language:   "zh",
		ctx:        ctx,
		cancel:     cancel,
		maxRetries: 3,
		timeout:    120 * time.Second,
	}

	return core
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

func (c *ArgusCore) SetOnThought(fn func(evt map[string]interface{})) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onThought = fn
}

// emitThought 发送思考链事件到前端Dashboard
func (c *ArgusCore) emitThought(evtType, role, content string, meta map[string]interface{}) {
	if c.onThought == nil {
		return
	}
	evt := map[string]interface{}{
		"type":      evtType,
		"role":      role,
		"content":   content,
		"timestamp": time.Now().Unix(),
	}
	if meta != nil {
		for k, v := range meta {
			evt[k] = v
		}
	}
	c.onThought(evt)
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

func (c *ArgusCore) SetOnActionEvent(fn func(eventName string, data interface{})) {
	c.onActionEvent = fn
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

func (c *ArgusCore) SetOnTodoUpdate(fn func(TodoEvent)) {
	c.todo.SetOnUpdate(fn)
}

func (c *ArgusCore) GetTodoManager() *TodoManager {
	return c.todo
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

	// 发送步骤开始事件到Dashboard
	c.emitThought("step", roleLabel, fmt.Sprintf("开始 %s 分析...", strings.ToUpper(roleLabel)), map[string]interface{}{"phase": roleLabel})

	start := time.Now()

	var response string
	var err error
	// reasoning_content 聚合缓冲区（避免逐chunk高频发射导致前端疯狂滚屏）
	var thinkingBuf strings.Builder
	var lastThinkEmit time.Time

	callCtx, callCancel := context.WithTimeout(c.ctx, c.timeout)
	response, err = c.client.ChatStream(callCtx, systemPrompt, history, fullPrompt, c.language,
		func(delta string) {
			c.emitChunk(delta)
		},
		func(evt map[string]interface{}) {
			evtType, _ := evt["type"].(string)
			evtContent, _ := evt["content"].(string)

			if evtType == "thinking" && evtContent != "" {
				// 聚合 reasoning_content，每 3 秒或 step 结束时批量发一次
				thinkingBuf.WriteString(evtContent)
				if time.Since(lastThinkEmit) > 3*time.Second {
					c.emitThought("thinking", roleLabel, thinkingBuf.String(), nil)
					thinkingBuf.Reset()
					lastThinkEmit = time.Now()
				}
			} else if evtType == "step" {
				// step 事件直接转发（低频）
				c.emitThought(evtType, roleLabel, evtContent, nil)
			}
		})
	callCancel()

	// flush 剩余的 thinking 缓冲
	if thinkingBuf.Len() > 0 {
		c.emitThought("thinking", roleLabel, thinkingBuf.String(), nil)
	}

	duration := time.Since(start)
	fmt.Printf("[Core:%s] AI call completed in %v (len=%d, err=%v)\n", roleLabel, duration, len(response), err)

	// 发送步骤完成事件
	status := "done"
	if err != nil {
		status = "error"
	}
	c.emitThought("step", roleLabel, fmt.Sprintf("%s %s (%v, %d chars)",
		map[string]string{"done": "✅", "error": "❌"}[status],
		strings.ToUpper(roleLabel), duration, len(response)),
		map[string]interface{}{"phase": roleLabel, "duration_ms": duration.Milliseconds(), "chars": len(response)})

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

// callSEWithTools 使用Tool Call方式调用SE（替代callAI的纯文本模式）
// 返回JSON格actions字符串，与parseSEResponse兼容
func (c *ArgusCore) callSEWithTools(taskDesc, memoryContext string) (string, error) {
	if c.client == nil {
		return "", fmt.Errorf("AICaller is nil")
	}

	systemPrompt := c.prompts.Get(RoleSE)
	history := buildHistoryFromMemory(c.memory.GetAll())

	fullPrompt := taskDesc
	if memoryContext != "" {
		fullPrompt = fmt.Sprintf("[Context from previous phases]\n%s\n\n[Current Task]\n%s", memoryContext, taskDesc)
	}

	start := time.Now()
	callCtx, callCancel := context.WithTimeout(c.ctx, c.timeout)

	resp, err := c.client.ChatWithTools(callCtx, systemPrompt, history, fullPrompt, ai.SETools)
	callCancel()

	duration := time.Since(start)
	fmt.Printf("[Core:SE-TOOL] Tool Call completed in %v (err=%v)\n", duration, err)

	if err != nil {
		return "", err
	}

	// 将ToolCalls转换为JSON actions格式（与parseSEResponse兼容）
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("no response from AI")
	}

	msg := resp.Choices[0].Message
	fmt.Printf("[Core:SE-TOOL] ToolCalls=%d ContentLen=%d\n", len(msg.ToolCalls), len(msg.Content))

	if len(msg.ToolCalls) == 0 {
		// 没有ToolCall，返回content（可能LLM直接返回了JSON）
		return msg.Content, nil
	}

	// 构建actions数组
	var actions []map[string]interface{}
	for _, tc := range msg.ToolCalls {
		var args map[string]interface{}
		if json.Unmarshal([]byte(tc.Function.Arguments), &args) != nil {
			args = map[string]interface{}{"raw": tc.Function.Arguments}
		}
		args["type"] = tc.Function.Name
		actions = append(actions, args)
	}

	result, _ := json.Marshal(map[string]interface{}{"actions": actions})
	return string(result), nil
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
		c.emitStatus("done", "none", "idle")
		result.Success = true
		return result
	}

	// 📋 TODO: PM分析完成，设置初始任务列表
	c.todo.Clear()
	c.todo.SetTasks([]string{
		"PM Analysis: Analyze user requirements",
		"SE Execution: Write code and run verification",
		"PM Code Review: Quality check and verification",
		"AP Final Approval: Security and compliance check",
	}, "pipeline")
	c.todo.UpdateByPhase("pipeline", TodoDone) // PM已完成
	c.todo.MarkCurrentDoing() // 标记SE为doing

	c.emit("pm_to_se", taskDesc)
	c.emitStatus("execute", "se", "busy")

	seCtx := c.memory.FormatForPrompt()
	seResponse, seErr := c.callAI(RoleSE, taskDesc, seCtx) // [REVERT] 恢复文本模式，Tool Call模式待完善
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
		c.emit("se_to_pm", fmt.Sprintf("@USR SE执行失败: %v", seErr))
		return result
	}

	c.memory.Add(RoleSE, seResponse)

	actions, completed := c.parseSEResponse(seResponse)
	actions = c.ensureExecAction(actions)

	if completed {
		c.emit("se_to_pm", fmt.Sprintf("@PM ✅ %s", c.extractCompletedSummary(seResponse)))
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
			actions = c.ensureExecAction(actions)
			if len(actions) > 0 || completed {
				break
			}
		}
	}

	if len(actions) == 0 && !completed {
		result.Error = fmt.Errorf("SE returned no valid actions after retries")
		c.emit("se_to_pm", "@USR SE无法生成有效操作")
		return result
	}

	// 执行操作
	execResults, execErr := c.executeActions(actions)

	maxSelfFix := 5
	for selfAttempt := 0; selfAttempt <= maxSelfFix; selfAttempt++ {
		if execErr == nil && c.seExecutionSatisfied(execResults) {
			if selfAttempt > 0 {
				// self-fix 成功，继续执行
			}
			break
		}

		if selfAttempt >= maxSelfFix {
			if execErr != nil {
				result.Error = fmt.Errorf("execution failed after %d attempts: %w", maxSelfFix, execErr)
			} else {
				result.Error = fmt.Errorf("SE execution incomplete after %d attempts: missing verification output", maxSelfFix)
			}
			break
		}

		feedbackErr := "<no error>"
		if execErr != nil {
			feedbackErr = execErr.Error()
		}
		c.emit("se_to_pm", fmt.Sprintf("🔄 SE Self-Fix #%d/%d: %s", selfAttempt+1, maxSelfFix, feedbackErr))

		var feedbackPrompt string
		switch selfAttempt {
		case 0:
			feedbackPrompt = fmt.Sprintf(`⚠️ EXECUTION FAILED - FIX REQUIRED

Error: %s

Your Actions:
%v

Results:
%v

Task: %s

COMMON FIXES:
- Syntax error? Rewrite the COMPLETE file with correct Go syntax
- Missing exec? Add {"type":"exec","command":"go run filename.go"}
- Wrong path? Use relative path only (just filename, not full path)

Output corrected JSON:
{"actions":[...]}`, feedbackErr, actions, execResults, userMsg)
		case 1:
			feedbackPrompt = fmt.Sprintf(`❌ STILL FAILING - LOOK CAREFULLY AT THE ERROR

Error: %s

Your LAST attempt FAILED with same/similar error.
Read the error message carefully and fix EXACTLY what it says.

Actions you tried:
%v

Execution output:
%v

Task: %s

SPECIFIC GUIDANCE:
- "cd:" error → Use "cd " (with space) or just use "go run file.go" directly
- "go test x.go" on non-_test.go file → Use "go run x.go" instead
- "path not found" → Use ONLY filename, no directory path
- "syntax error" → Your code has a bug. Write COMPLETE correct code.

Working directory is already set. Just use: go run filename.go

Output CORRECTED JSON:
{"actions":[{"type":"write_file","path":"FILENAME.go","content":"package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"Hello World\")\n}"},{"type":"exec","command":"go run FILENAME.go"}]}`,
				feedbackErr, actions, execResults, userMsg)
		default:
			errorAnalysis := c.analyzeExecError(feedbackErr)
			feedbackPrompt = fmt.Sprintf(`🔴 REPEATED FAILURE #%d - FOLLOW THE EXACT FIX BELOW

Error: %s

Analysis: %s

You have tried %d times and KEEP MAKING THE SAME MISTAKE.
Stop guessing. Follow these EXACT instructions:

1. For write_file: ALWAYS include complete valid code
   package main
   import "fmt"
   func main() { fmt.Println("Hello World") }

2. For exec: ALWAYS use format: go run filename.go
   NEVER use: cd anything, F:\GithubArgus, go test on non-test files

3. Working dir is already set. Use RELATIVE filenames only.

Task: %s

Output ONLY this exact JSON structure:
{"actions":[{"type":"write_file","path":"FILENAME.go","content":"COMPLETE VALID GO CODE"},{"type":"exec","command":"go run FILENAME.go"}]}`,
				selfAttempt+1, feedbackErr, errorAnalysis, selfAttempt+1, userMsg)
		}

		seResponse, seErr = c.callAI(RoleSE, feedbackPrompt, c.memory.FormatForPrompt())
		if seErr != nil {
			c.emit("se_to_pm", fmt.Sprintf("⚠️ Self-fix call failed: %v", seErr))
			continue
		}
		c.memory.Add(RoleSE, fmt.Sprintf("SE self-fix #%d", selfAttempt+1))

		actions, _ = c.parseSEResponse(seResponse)
		actions = c.ensureExecAction(actions)
		if len(actions) == 0 {
			c.emit("se_to_pm", "⚠️ SE returned no actions on self-fix")
			continue
		}

		// self-fix 后重新执行
		execResults, execErr = c.executeActions(actions)
	}

	result.Outputs = execResults
	result.Actions = actions

	seDisplay := c.extractDisplayText(seResponse)
	c.emit("se_to_pm", seDisplay)

	if result.Error != nil {
		c.emitStatus("error", "se", "idle")
		return result
	}

	c.memory.Add(RoleSE, fmt.Sprintf("SE completed. Actions: %d, Results: %v", len(actions), execResults))

	// 📋 TODO: SE执行完成，标记Review为doing
	c.todo.CompleteCurrent() // SE done
	c.todo.MarkCurrentDoing() // Review start

	// --- Phase 2-3 Loop: SE Execution + PM Review with Retry ---
	maxReviewRetries := 2
	var reviewResult ReviewResult

	for reviewAttempt := 0; reviewAttempt < maxReviewRetries; reviewAttempt++ {
		if reviewAttempt > 0 {
			c.emitStatus("se", "se", "busy")
			c.emit("se_to_pm", fmt.Sprintf("🔄 SE Retry #%d (PM Feedback): %s", reviewAttempt, reviewResult.Reason))

			// 📋 TODO: 添加重试任务
			c.todo.AddTask(fmt.Sprintf("SE Retry #%d: Fix PM feedback", reviewAttempt+1), "se_retry", 1)
			c.todo.MarkCurrentDoing()

			retryPrompt := fmt.Sprintf(`PM rejected your previous work with this reason:
%s

Please fix ALL the issues mentioned above and re-execute the task from scratch.
Make sure to:
1. Use correct filenames as specified in the original request
2. Include complete file content (not truncated)
3. Actually execute verification commands (run, test, etc.)
4. Verify the output matches expectations

Original user request: %s`, reviewResult.Reason, userMsg)

			seResponse, seErr = c.callAI(RoleSE, retryPrompt, c.memory.FormatForPrompt())
			if seErr != nil {
				c.emit("se_to_pm", fmt.Sprintf("@USR SE retry failed: %v", seErr))
				break
			}
			c.memory.Add(RoleSE, fmt.Sprintf("SE retry #%d response", reviewAttempt+1))

			actions, _ = c.parseSEResponse(seResponse)
			actions = c.ensureExecAction(actions)
			if len(actions) == 0 {
				c.emit("se_to_pm", "@USR ⚠️ SE returned no actions on retry - marking as failed")
				reviewResult.Rejected = true
				reviewResult.Reason = "SE returned empty actions - unable to fix"
				break
			}

			// PM feedback retry: 重新执行
			execResults, execErr = c.executeActions(actions)
			result.Outputs = execResults
			result.Actions = actions
			if execErr != nil {
				c.emit("se_to_pm", fmt.Sprintf("⚠️ Retry execution error: %v", execErr))
				continue
			}
			// retry execution done, continue to PM review
		}

		// --- Phase 3: PM Code Review ---
		c.emitStatus("review", "pm", "busy")

		reviewCtx := fmt.Sprintf("[User Request] %s\n[SE Actions] %v\n[SE Results] %v\n[SE Response] %s\n[Retry Attempt] %d/%d",
			userMsg, actions, execResults, seResponse, reviewAttempt+1, maxReviewRetries)

		reviewPrompt := fmt.Sprintf(`Review the SE's work above. Check code quality, correctness, and completeness.

Files changed: %v

=== EXECUTION RESULTS ===
%s

=== REVIEW RULES ===
1. If user asked to RUN/EXECUTE a program, SE MUST include exec action with actual run command
2. If any execution shows error/failed, you MUST REJECT
3. Check that outputs match expectations (e.g., "Hello World" should appear)
4. Incomplete work (write only without run) must be REJECTED when user requested execution

Decide: approve or reject with specific reasons.`,
			getFilePaths(actions), func() string {
				if execErr != nil {
					return fmt.Sprintf("❌ EXECUTION FAILED: %v\nOutputs:\n%v", execErr, execResults)
				}
				return fmt.Sprintf("✅ Outputs:\n%v", execResults)
			}())

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
			reviewResult = ReviewResult{Approved: true, DisplayText: "@USR 📋 PM Code Review ✅ AUTO-APPROVED (error)"}
			break
		} else {
			c.memory.Add(RolePM, pmReviewResponse)
		}

		reviewResult = c.parseReviewResponse(pmReviewResponse)
		c.emit("pm_review", reviewResult.DisplayText)

		if !reviewResult.Rejected {
			// PM approved, proceed to AP
			c.todo.CompleteCurrent() // Review done
			c.todo.MarkCurrentDoing() // AP start

			break
		}

		c.emit("pm_review", fmt.Sprintf("@SE ❌ PM rejected (attempt %d/%d): %s", reviewAttempt+1, maxReviewRetries, reviewResult.Reason))

		if reviewAttempt == maxReviewRetries-1 {
			c.emit("error", fmt.Sprintf("V2 Error: PM review rejected after %d attempts: %s", maxReviewRetries, reviewResult.Reason))
			result.Error = fmt.Errorf("PM review rejected after %d attempts: %s", maxReviewRetries, reviewResult.Reason)
			c.emitStatus("error", "pm", "idle")
			return result
		}
	}

	// GUARD: PM rejected all attempts → must NOT proceed to AP
	if reviewResult.Rejected {
		c.emit("error", fmt.Sprintf("V2 Error: PM review rejected after %d attempts: %s", maxReviewRetries, reviewResult.Reason))
		result.Error = fmt.Errorf("PM review rejected after %d attempts: %s", maxReviewRetries, reviewResult.Reason)
		c.emitStatus("error", "pm", "idle")
		return result
	}

	// --- Phase 4: AP Approval (OA) ---
	execSummary := strings.Join(execResults, "\n")
	if execErr != nil {
		execSummary += fmt.Sprintf("\n❌ EXECUTION ERROR: %v", execErr)
	} else {
		execSummary += "\n✅ All actions executed successfully"
	}

	c.emitStatus("approve", "ap", "busy")

	apCtx := c.memory.FormatForPrompt()
	pmStatus := "approved"
	if reviewResult.Rejected {
		pmStatus = "REJECTED - " + reviewResult.Reason
	}

	apPrompt := fmt.Sprintf(`Final approval for task: %s

SE executed %d actions.
PM review: %s

=== REAL EXECUTION RESULTS ===
%s

=== ACTION DETAILS ===
%v

IMPORTANT: Check the execution results above carefully!
- If PM review was REJECTED above, you MUST ALSO REJECT unless SE has clearly fixed the issues
- If any action shows "error" or "failed", you MUST REJECT
- If execution output shows errors (syntax error, command not found, etc.), you MUST REJECT
- Only approve if ALL actions succeeded AND outputs are correct AND PM approved

Perform final quality and security check. Approve or reject with reasons.`,
		userMsg, len(actions), pmStatus, execSummary, actions)

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

	maxAPRetries := 1
	for apAttempt := 0; apAttempt <= maxAPRetries; apAttempt++ {
		if apResult.Rejected {
			if apAttempt >= maxAPRetries {
				c.emit("ap_result", fmt.Sprintf("@SE ❌ AP rejected (final): %s", apResult.Reason))
				c.emitStatus("error", "ap", "idle")
				result.Error = fmt.Errorf("AP rejected: %s", apResult.Reason)
				return result
			}

			c.emit("ap_result", fmt.Sprintf("@SE ❌ AP rejected #%d/%d: %s", apAttempt+1, maxAPRetries, apResult.Reason))
			c.emitStatus("se", "se", "busy")
			c.emit("se_to_pm", fmt.Sprintf("🔄 SE AP-Fix #%d: %s", apAttempt+1, apResult.Reason))

			apFixPrompt := fmt.Sprintf(`AP FINAL APPROVAL REJECTED - CRITICAL FIX REQUIRED

Rejection Reason:
%s

Your previous work was reviewed by PM (approved) but rejected by AP.
This means your code has security, quality, or compliance issues.

Previous Actions:
%v

Execution Results:
%v

Original User Request: %s

FIX THE SPECIFIC ISSUE MENTIONED IN REJECTION:
- If syntax/compile error: rewrite file completely with correct code
- If missing verification: add exec action and verify output
- If quality issue: improve code structure and completeness

CRITICAL RULES:
1. Complete file content with ALL imports (package main, import "fmt")
2. Correct filename from original request
3. Include exec action to RUN and VERIFY
4. Output must show expected result

Generate corrected actions JSON:
{"actions":[{"type":"write_file","path":"CORRECT_FILENAME","content":"COMPLETE CORRECT CODE"},{"type":"exec","command":"run verification"}]}`, apResult.Reason, actions, execResults, userMsg)

			seResponse, seErr = c.callAI(RoleSE, apFixPrompt, c.memory.FormatForPrompt())
			if seErr != nil {
				c.emit("se_to_pm", fmt.Sprintf("@USR SE AP-fix failed: %v", seErr))
				continue
			}
			c.memory.Add(RoleSE, fmt.Sprintf("SE AP-fix #%d response", apAttempt+1))

			actions, _ = c.parseSEResponse(seResponse)
			actions = c.ensureExecAction(actions)
			if len(actions) == 0 {
				c.emit("se_to_pm", "@USR ⚠️ SE returned no actions on AP-fix - marking as failed")
				apResult.Rejected = true
				apResult.Reason = "SE returned empty actions - unable to fix AP feedback"
				break
			}

			// AP-fix: 执行修复操作
			execResults, execErr = c.executeActions(actions)
			result.Outputs = execResults
			result.Actions = actions
			if execErr != nil {
				c.emit("se_to_pm", fmt.Sprintf("⚠️ AP-fix execution error: %v", execErr))
				continue
			}
			// AP-fix execution done, continue to PM re-review

			c.emitStatus("review", "pm", "busy")

			reviewCtx := fmt.Sprintf("[User Request] %s\n[SE Actions] %v\n[SE Results] %v\n[SE Response] %s\n[AP Rejection] %s\n[Retry Attempt] AP-fix #%d/%d",
				userMsg, actions, execResults, seResponse, apResult.Reason, apAttempt+1, maxAPRetries)
			pmReviewResponse, pmReviewErr := c.callAIWithPrompt(c.prompts.PMReview, "pm_review", reviewCtx, c.memory.FormatForPrompt())
			if pmReviewErr != nil {
				c.emit("pm_review", fmt.Sprintf("@USR PM re-review failed: %v", pmReviewErr))
				continue
			}
			reviewResult = c.parseReviewResponse(pmReviewResponse)
			c.emit("pm_review", reviewResult.DisplayText)

			if !reviewResult.Approved {
				c.emit("pm_review", fmt.Sprintf("@SE ❌ PM re-rejected (AP-fix #%d): %s", apAttempt+1, reviewResult.Reason))
				continue
			}
			// PM re-review approved, proceed to AP re-eval

			c.emitStatus("approve", "ap", "busy")
			apPrompt := fmt.Sprintf(`Final approval for task: %s

SE executed %d actions (AP-fix re-evaluation #%d).
PM review approved after AP rejection fix.

=== REAL EXECUTION RESULTS ===
%s

=== ACTION DETAILS ===
%v

Previous AP Rejection: %s

IMPORTANT: Check the execution results above carefully!
- If any action shows "error" or "failed", you MUST REJECT
- If execution output shows errors (syntax error, command not found, etc.), you MUST REJECT
- Only approve if ALL actions succeeded AND outputs are correct

Perform final quality and security check. Approve or reject with reasons.`,
				userMsg, len(actions), apAttempt+1, execSummary, actions, apResult.Reason)
			apResponse, apErr = c.callAI(RoleAP, apPrompt, c.memory.FormatForPrompt())
			if apErr != nil {
				c.emit("ap_result", fmt.Sprintf("@USR AP re-eval error: %v (keeping rejected)", apErr))
				apResult.Rejected = true
				apResult.Reason = fmt.Sprintf("AP re-evaluation call failed: %v", apErr)
				continue
			} else {
				c.memory.Add(RoleAP, apResponse)
			}
			apResult = c.parseAPResponse(apResponse)
			c.emit("ap_result", apResult.DisplayText)
		}
	}

	if apResult.Rejected {
		c.emit("ap_result", fmt.Sprintf("@USR ❌ 任务最终失败: %s", apResult.Reason))
		c.emitStatus("error", "none", "idle")
		result.Error = fmt.Errorf("AP final rejection: %s", apResult.Reason)
		return result
	}

	c.emitStatus("done", "none", "idle")

	// 📋 TODO: 全部完成
	c.todo.CompleteCurrent() // AP done - all tasks completed

	result.Success = true
	return result
}

func (c *ArgusCore) ensureExecAction(actions []ai.SEAction) []ai.SEAction {
	if len(actions) == 0 {
		return actions
	}

	// Validate and fix garbage paths (e.g., "content", "", absolute paths, no extension)
	validExts := map[string]bool{".go": true, ".py": true, ".js": true, ".ts": true, ".html": true, ".css": true, ".json": true, ".md": true, ".txt": true, ".yaml": true, ".yml": true}
	for i := range actions {
		a := &actions[i]
		if a.Type == "write_file" || a.Type == "edit_file" {
			// Reject obviously invalid paths
			if a.Path == "" || a.Path == "content" || strings.ToLower(a.Path) == "content" ||
				filepath.IsAbs(a.Path) || !strings.Contains(a.Path, ".") {
				fmt.Printf("[Core] ⚠️ Invalid path detected: %q → dropping action\n", a.Path)
				a.Type = "_invalid_" // mark for removal
				continue
			}
			ext := filepath.Ext(a.Path)
			if ext != "" && !validExts[ext] && a.Type == "write_file" {
				fmt.Printf("[Core] ⚠️ Unusual extension %q on path %q - keeping but noting\n", ext, a.Path)
			}
		}
	}

	// Remove marked-invalid actions
	validActions := make([]ai.SEAction, 0, len(actions))
	for _, a := range actions {
		if a.Type != "_invalid_" {
			validActions = append(validActions, a)
		}
	}
	actions = validActions

	if len(actions) == 0 {
		return actions
	}

	hasExec := false
	var lastGoFile string
	for _, a := range actions {
		if a.Type == "exec" {
			hasExec = true
			break
		}
		if a.Type == "write_file" && (strings.HasSuffix(a.Path, ".go") || strings.HasSuffix(a.Path, ".py") || strings.HasSuffix(a.Path, ".js") || strings.HasSuffix(a.Path, ".ts")) {
			lastGoFile = a.Path
		}
	}
	if hasExec || lastGoFile == "" {
		return actions
	}
	var execCmd string
	switch {
	case strings.HasSuffix(lastGoFile, ".go"):
		if strings.HasSuffix(lastGoFile, "_test.go") {
			base := strings.TrimSuffix(filepath.Base(lastGoFile), "_test.go")
			tmpFile := base + "_tmp.go"
			execCmd = fmt.Sprintf("copy /y %s %s && go run %s && del %s", lastGoFile, tmpFile, tmpFile, tmpFile)
		} else {
			execCmd = fmt.Sprintf("go run %s", lastGoFile)
		}
	case strings.HasSuffix(lastGoFile, ".py"):
		execCmd = fmt.Sprintf("python %s", lastGoFile)
	case strings.HasSuffix(lastGoFile, ".js"), strings.HasSuffix(lastGoFile, ".ts"):
		execCmd = fmt.Sprintf("node %s", lastGoFile)
	default:
		return actions
	}
	c.emit("se_to_pm", fmt.Sprintf("🔧 自动追加 exec: %s (SE遗漏执行命令)", execCmd))
	actions = append(actions, ai.SEAction{Type: "exec", Command: execCmd})
	return actions
}

func (c *ArgusCore) seExecutionSatisfied(results []string) bool {
	if len(results) == 0 {
		return false
	}
	// Must have at least one successful exec result
	hasExecSuccess := false
	for _, r := range results {
		if strings.HasPrefix(r, "✅ exec") {
			hasExecSuccess = true
			break
		}
	}
	if !hasExecSuccess {
		return false
	}
	// Also must not have any exec failures
	for _, r := range results {
		if strings.Contains(r, "❌ exec") || strings.Contains(r, "syntax error") ||
			strings.Contains(r, "exit status") || strings.Contains(r, "command failed") {
			return false
		}
	}
	return true
}

func (c *ArgusCore) analyzeExecError(errMsg string) string {
	errLower := strings.ToLower(errMsg)
	var analysis []string

	if strings.Contains(errLower, "cd:") || strings.Contains(errLower, "cd\\") {
		analysis = append(analysis, "❌ 'cd:' syntax error - missing space after cd")
	}
	if strings.Contains(errLower, "githubargus") || strings.Contains(errLower, "github") {
		analysis = append(analysis, "❌ Hallucinated path 'GithubArgus' - use relative filename only")
	}
	if strings.Contains(errLower, "go test") && !strings.Contains(errLower, "_test.go") {
		analysis = append(analysis, "❌ Using 'go test' on non-test file - use 'go run'")
	}
	if strings.Contains(errLower, "syntax error") || strings.Contains(errLower, "unexpected") {
		analysis = append(analysis, "❌ Code syntax error - rewrite with valid Go code")
	}
	if strings.Contains(errLower, "path not found") || strings.Contains(errLower, "找不到") {
		analysis = append(analysis, "❌ Path does not exist - use relative filename in working directory")
	}
	if strings.Contains(errLower, "unknown action") || strings.Contains(errLower, "invalid action") {
		analysis = append(analysis, "❌ Invalid JSON action format - check your actions structure")
	}

	if len(analysis) == 0 {
		return fmt.Sprintf("Generic execution error: %s", errMsg)
	}
	return strings.Join(analysis, "\n")
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

	c.emitAction("exec_start", map[string]interface{}{
		"executor": "se",
		"total":    len(actions),
	})

	for i, action := range actions {
		fmt.Printf("[Core:Exec] Action %d/%d: type=%s\n", i+1, len(actions), action.Type)

		label := action.Command
		if label == "" {
			label = action.Path
		}
		if label == "" {
			label = action.Type
		}

		c.emitAction("exec_start", map[string]interface{}{
			"executor": "se",
			"index":    i + 1,
			"total":    len(actions),
			"type":     action.Type,
			"label":    label,
		})

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
			c.emitAction("exec_output", map[string]interface{}{
				"executor": "se",
				"command":  action.Command,
				"output":   output,
				"exit_code": 0,
			})
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

		status := "done"
		if err != nil {
			status = "error"
		}

		c.emitAction("exec_done", map[string]interface{}{
			"executor": "se",
			"index":    i + 1,
			"total":    len(actions),
			"type":     action.Type,
			"label":    label,
			"status":   status,
			"error":    func() string { if err != nil { return err.Error() }; return "" }(),
		})

		outputs = append(outputs, output)
		c.emit("se_to_pm", output)

		if err != nil {
			return outputs, fmt.Errorf("action %d (%s) failed: %w", i, action.Type, err)
		}
	}

	c.emitAction("exec_completed", map[string]interface{}{})

	return outputs, nil
}

func (c *ArgusCore) emitAction(eventName string, data interface{}) {
	if c.onActionEvent != nil {
		c.onActionEvent(eventName, data)
	}
}

func (c *ArgusCore) extractDisplayText(response string) string {
	lines := strings.Split(response, "\n")
	var displayLines []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "{") || strings.HasPrefix(line, "```json") || strings.Contains(line, `"is_programming"`) || strings.Contains(line, `"task":`) {
			continue
		}
		if strings.HasPrefix(line, "@") {
			continue
		}
		if strings.HasPrefix(line, "```") {
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
