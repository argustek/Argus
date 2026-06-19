package core

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"argus/internal/ai"
	"argus/internal/debugger"
	"argus/internal/executor"
)

// RoleState LabVIEW式角色状态（后面板控件值，前面板只读投影）
type RoleState struct {
	Phase     string `json:"phase"`              // idle, pm, se, ap, review, done, error
	PM        string `json:"pm"`                 // idle, busy
	SE        string `json:"se"`                 // idle, busy
	AP        string `json:"ap"`                 // idle, busy
	MC        bool   `json:"mc"`                 // C监控运行中
	Task      string `json:"task,omitempty"`     // 当前任务描述
	Progress  string `json:"progress,omitempty"` // 进度信息
	UpdatedAt int64  `json:"updated_at"`         // 时间戳
}

// ThoughtEvent AI思考链事件（用于前端Dashboard展示）
type ThoughtEvent struct {
	Type      string                 `json:"type"`              // "thinking" | "step" | "tool_start" | "tool_done"
	Role      string                 `json:"role"`              // "pm" | "se" | "ap"
	Content   string                 `json:"content,omitempty"` // 思考内容/步骤描述/工具输出
	Timestamp int64                  `json:"timestamp"`         // Unix秒
	Metadata  map[string]interface{} `json:"meta,omitempty"`    // 扩展(工具名/步骤号/耗时等)
}

type AICaller interface {
	ChatStream(ctx context.Context, systemPrompt string, history []ai.Message, userContent string, replyLanguage string, onChunk func(delta string), onThought func(evt map[string]interface{})) (string, error)
	ChatWithTools(ctx context.Context, systemPrompt string, history []ai.Message, userContent string, tools []ai.Tool, replyLanguage string) (*ai.ChatResponse, error)
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
	Level    string // [v0.8.1] 项目级别: short-process / normal-process / full-process
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
	todo     *TodoManager // 动态任务列表管理器

	pmProcessor *ai.PMProcessor // [v0.7.2] PM处理器（带 add_todo/update_todo Function Call）

	workDir  string
	language string

	debuggerMgr *debugger.DebugSessionManager // DAP 调试会话管理器

	onMessage     func(source, content string)
	onChunk       func(delta string)
	onThought     func(evt map[string]interface{}) // 思考链回调（Dashboard可视化）
	onStateChange func(RoleState)
	onActionEvent func(eventName string, data interface{})

	silent bool // [v0.8] Featherweight静默模式：抑制所有中间emit，只发最终总结

	ctx    context.Context
	cancel context.CancelFunc

	maxRetries int
	timeout    time.Duration

	state         RoleState
	prevTaskLevel string // 上一个完成任务的级别，用于追问继承同一重量
}

func NewArgusCore(client AICaller, exec *executor.Executor, workDir string) *ArgusCore {
	ctx, cancel := context.WithCancel(context.Background())
	core := &ArgusCore{
		client:      client,
		executor:    exec,
		memory:      NewSharedMemory(100),
		prompts:     NewPromptKit(workDir),
		todo:        NewTodoManager(),
		workDir:     workDir,
		language:    "zh",
		ctx:         ctx,
		cancel:      cancel,
		maxRetries:  3,
		timeout:     120 * time.Second,
		debuggerMgr: debugger.NewDebugSessionManager(exec, workDir),
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
	// [v0.8.1] Featherweight静默模式：抑制所有中间消息（只保留最终总结）
	if c.silent {
		return
	}
	if c.onMessage != nil {
		c.onMessage(source, content)
	}
}

func (c *ArgusCore) emitStatus(phase, role, status string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// [v0.8.1] 状态更新始终发出（C监控依赖此判断PM/SE是否busy）
	// silent模式只抑制emit()消息，不抑制状态

	c.state.Phase = phase

	// 🔧 关键修复：done/error阶段强制重置所有角色状态（无论传入的role是什么）
	if phase == "done" || phase == "error" {
		c.state.PM = "idle"
		c.state.SE = "idle"
		c.state.AP = "idle"
	} else {
		// 正常运行阶段：按角色单独设置
		switch role {
		case "pm":
			c.state.PM = status
		case "se":
			c.state.SE = status
		case "ap":
			c.state.AP = status
		}
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

// SetPMProcessor 注入 PM 处理器（带 add_todo/update_todo Function Call）
// 同时连接 TODO 回调：PM 工具调用 → TodoManager → MessageBus → 前端
func (c *ArgusCore) SetPMProcessor(p *ai.PMProcessor) {
	c.pmProcessor = p
	p.SetTodoCallbacks(
		func(desc string) string { return c.todo.AddTask(desc, "ai_todo", 1) },
		func(id, status string) { c.todo.UpdateStatus(id, TodoStatus(status)) },
		func() { c.todo.Clear() },
	)
}

// SetClient 热更新 LLM 客户端（SaveConfig 切换模型时调用）
func (c *ArgusCore) SetClient(client AICaller) {
	c.client = client
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

	resp, err := c.client.ChatWithTools(callCtx, systemPrompt, history, fullPrompt, ai.SETools, c.language)
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
	os.WriteFile("F:\\ArgusTek\\Argus\\debug_argus_process.txt", []byte(fmt.Sprintf("ArgusCore.Process CALLED at %v\nuserMsg=%q\npmProcessor=%v\n", time.Now(), userMsg, c.pmProcessor != nil)), 0644)
	totalStart := time.Now()
	result := &ProcessResult{
		Phases: make([]PhaseResult, 0, 3),
	}

	// [v0.8.1] 自动检测用户消息语言：英文消息→en，中文/其他→zh
	englishChars := 0
	for _, r := range userMsg {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			englishChars++
		}
	}
	if englishChars > len([]rune(userMsg))/2 {
		c.language = "en"
	} else {
		c.language = "zh"
	}

	defer func() {
		result.Duration = time.Since(totalStart)
	}()

	c.emitStatus("start", "pm", "busy")

	c.memory.Add(RoleUser, userMsg)

	// [v0.9.4] 前置分流：只依赖用户 /level 命令和追问继承
	// 不再使用硬编码关键词启发式 — 交给 PM 决策树自然判断
	// 条件A：用户 /level 命令
	userLevel := ""
	if strings.Contains(userMsg, "/level ") {
		parts := strings.SplitN(userMsg, "/level ", 2)
		if len(parts) == 2 {
			userLevel = strings.TrimSpace(strings.Fields(parts[1])[0])
			fmt.Printf("[Core:Level] 用户指定级别: %s\n", userLevel)
		}
	}

	preFeatherweight := userLevel == "short" || userLevel == "featherweight" || userLevel == "⚡" ||
		userLevel == "lightweight" || userLevel == "direct"

	// 追问继承 — 上一个任务如果是 short-process，追问默认继承同一重量
	if !preFeatherweight && c.prevTaskLevel == "short-process" {
		preFeatherweight = true
		fmt.Printf("[Core:Level] ⚡ 追问继承→Featherweight (prevLevel=%s)\n", c.prevTaskLevel)
	}

	// 前置Featherweight → 直接 pmDirectExecute，跳过 PM ProcessReview
	if preFeatherweight {
		result.Level = "short-process"
		c.prevTaskLevel = "short-process"
		fmt.Printf("[Core:分流] ⚡ Featherweight → PM直执（跳过PM分析）\n")
		return c.pmDirectExecute(userMsg, "", result)
	}

	// ===== 非Featherweight：走完整 PM 分析 =====
	var pmResponse string
	var pmErr error
	if c.pmProcessor != nil {
		entries := c.memory.GetAll()
		history := make([]ai.ChatMessage, 0, len(entries))
		for _, e := range entries {
			role := mapRoleToStandard(e.Role)
			history = append(history, ai.ChatMessage{Role: role, Content: e.Content})
		}
		var resp *ai.PMResponse
		resp, pmErr = c.pmProcessor.ProcessStream(userMsg, history, func(delta string) {
			trimmed := strings.TrimSpace(delta)
			if trimmed == "" || strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") || strings.HasPrefix(trimmed, "```") {
				return
			}
			if c.onChunk != nil {
				c.onChunk(delta)
			}
		})
		if pmErr == nil && resp != nil {
			pmResponse = resp.Content
			if resp.HasToolCalls {
				pmResponse += "\n[HAS_TOOL_CALLS]"
			}
		}
	} else {
		pmResponse, pmErr = c.callAI(RolePM, userMsg, "")
	}
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
		c.emit("pm_to_user", fmt.Sprintf("@USR PM analysis failed: %v", pmErr))
		return result
	}

	c.memory.Add(RolePM, pmResponse)

	// 条件B：PM 标记了 short（需 PM 返回后才能判断）
	if strings.Contains(pmResponse, "[HAS_TOOL_CALLS]") {
		// PM 已在 ProcessStream 中执行了工具调用（write_file/exec 等）
		// 任务已完成，直接汇报结果，不再调用 pmDirectExecute 重复执行
		result.Level = "short-process"
		// [FIX-v1.0.22] 不再无条件设置 prevTaskLevel=short-process
		// ide_send 等纯消息工具不应触发追问继承走 SE 路径
		// 只有 PM 明确标记 level:"short"/"featherweight" 才继承
		result.Success = true
		pmText := strings.ReplaceAll(pmResponse, "[HAS_TOOL_CALLS]", "")
		pmText = strings.TrimSpace(pmText)
		if pmText != "" {
			c.emit("pm_to_user", "@USR "+pmText)
		}
		c.emitStatus("done", "none", "idle")
		return result
	}

	if strings.Contains(pmResponse, `"level":"short"`) ||
		strings.Contains(pmResponse, `"level":"featherweight"`) ||
		strings.Contains(pmResponse, "⚡") {
		result.Level = "short-process"
		c.prevTaskLevel = "short-process"
		fmt.Printf("[Core:分流] ⚡ Featherweight → PM直执（PM标记）\n")
		return c.pmDirectExecute(userMsg, pmResponse, result)
	}

	// 非Featherweight：清理内部标记后再展示 PM 原始响应
	result.Level = "normal-process" // [v0.8.1] 默认 normal（PM→SE 标准流程）
	cleanPMResponse := strings.ReplaceAll(pmResponse, "[HAS_TOOL_CALLS]", "")
	cleanPMResponse = strings.TrimSpace(cleanPMResponse)
	displayText := c.extractDisplayText(cleanPMResponse)
	if displayText != "" {
		c.emit("pm_to_user", displayText)
	}

	// 非Featherweight：继续原有流程
	// PM 是聪明人，不是分类器。看 PM 的 @ 指令决定流向
	hasSE := strings.Contains(pmResponse, "@SE")

	if !hasSE {
		if strings.Contains(pmResponse, "@USR") {
			// PM 在问用户问题，别结束，等人回答
			c.emitStatus("question", "none", "idle")
		} else {
			// 纯聊天 / 直接回答，完事
			c.emitStatus("done", "none", "idle")
		}
		result.Success = true
		return result
	}
	// PM 安排了工作，继续走 SE 流程

	// [v0.7.2] 硬编码TODO已禁用，改由 PM 的 AI 工具调用 (add_todo/update_todo) 管理
	// 但每次新任务必须先清空旧 TODO，否则堆积
	c.todo.Clear()

	// [Layer 1] 工具链预检：在SE执行前检测编译器/解释器是否可用
	// 从PM响应中提取目标语言（基于文件扩展名）
	var detectedLang string
	for _, ext := range []string{".go", ".py", ".rs", ".js", ".ts", ".java", ".c", ".cpp"} {
		if strings.Contains(pmResponse, ext) {
			switch ext {
			case ".go":
				detectedLang = "go"
			case ".py":
				detectedLang = "python"
			case ".rs":
				detectedLang = "rust"
			case ".js", ".ts":
				detectedLang = "nodejs"
			case ".java":
				detectedLang = "java"
			case ".c", ".cpp":
				detectedLang = "c/c++"
			}
			break
		}
	}

	if detectedLang != "" {
		available, missing, hints := c.checkToolAvailability(detectedLang)
		if len(missing) > 0 {
			// 🔧 关键改进：检测到环境缺失时，暂停流程并询问用户
			blockMsg := fmt.Sprintf("🛑 **环境阻断**: 目标语言 [%s] 缺少必要工具!\n\n❌ 缺失工具: %s\n✅ 已有工具: %s\n\n**请选择处理方式:**\n1️⃣ 自动安装 (运行: %s)\n2️⃣ 改用其他语言 (如Python/Go)\n3️⃣ 取消任务\n\n回复数字选择或输入新指令。",
				detectedLang,
				strings.Join(missing, ", "),
				strings.Join(available, ", "),
				strings.Join(hints, "\n   或 "))
			c.emit("pm_to_user", blockMsg)
			c.emitStatus("env-blocked", "none", "idle") // 特殊状态：环境阻断
			result.Phases = append(result.Phases, PhaseResult{
				Phase:    PhaseAnalyze,
				Role:     RolePM,
				Input:    userMsg,
				Output:   blockMsg,
				Raw:      blockMsg,
				Duration: time.Since(totalStart),
			})
			return result // 🔑 阻断！不继续执行SE
		} else {
			fmt.Printf("[Core:EnvCheck] ✅ %s toolchain OK: %s\n", detectedLang, strings.Join(available, ", "))
		}
	}

	c.emit("pm_to_se", pmResponse)
	c.emitStatus("execute", "se", "busy")

	seCtx := c.memory.FormatForPrompt()
	seResponse, seErr := c.callAI(RoleSE, pmResponse, seCtx)
	phaseSE := PhaseResult{
		Phase:    PhaseExecute,
		Role:     RoleSE,
		Input:    pmResponse,
		Output:   seResponse,
		Raw:      seResponse,
		Duration: 0,
	}
	result.Phases = append(result.Phases, phaseSE)

	if seErr != nil {
		result.Error = fmt.Errorf("SE execution failed: %w", seErr)
		c.emit("se_to_pm", fmt.Sprintf("@USR SE执行失败: %v", seErr))
		c.emit("pm_to_user", fmt.Sprintf("@USR SE执行失败: %v", seErr))
		return result
	}

	c.memory.Add(RoleSE, seResponse)

	actions, completed := c.parseSEResponse(seResponse)
	actions = c.ensureExecAction(actions)

	if completed {
		seSummary := c.extractCompletedSummary(seResponse)
		c.emit("se_to_pm", fmt.Sprintf("@PM ✅ %s", seSummary))
		if seSummary != "" {
			c.emit("pm_to_user", "@USR ✅ "+seSummary)
		}
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
		// [v0.9.7] 路由到PM：让PM分析SE空action问题并决定下一步
		if c.pmProcessor != nil {
			c.emitStatus("review", "pm", "busy")
			c.emit("pm_review", "@USR 🔄 PM正在分析SE空响应...")
			pmCtx := fmt.Sprintf("⚠️ **SE执行出错** — SE无法生成有效操作。\n\n用户请求: %s\n\nSE最终响应: %s\n\n请分析原因并决定下一步：\n- 如果可修复→@SE给出修正方案\n- 如果需要用户输入→@USR提问\n- 如果无法完成→@USR说明原因",
				userMsg, seResponse)
			entries := c.memory.GetAll()
			history := make([]ai.ChatMessage, 0, len(entries))
			for _, e := range entries {
				history = append(history, ai.ChatMessage{Role: mapRoleToStandard(e.Role), Content: e.Content})
			}
			pmResp, pmErr := c.pmProcessor.ProcessStream(pmCtx, history, func(delta string) {
				trimmed := strings.TrimSpace(delta)
				if trimmed == "" || strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") || strings.HasPrefix(trimmed, "```") {
					return
				}
				c.emitChunk(delta)
			})
			if pmErr == nil && pmResp != nil {
				c.memory.Add(RolePM, pmResp.Content)
				if strings.Contains(pmResp.Content, "@SE") {
					seResponse2, seErr2 := c.callAI(RoleSE, pmResp.Content, c.memory.FormatForPrompt())
					if seErr2 == nil {
						actions, completed = c.parseSEResponse(seResponse2)
						actions = c.ensureExecAction(actions)
					}
				}
			}
		}
		if len(actions) == 0 && !completed {
			errMsg := "SE无法生成有效操作"
			result.Error = fmt.Errorf("SE returned no valid actions after retries")
			c.emit("se_to_pm", "@USR "+errMsg)
			c.emit("pm_to_user", "@USR "+errMsg)
			return result
		}
	}

	// 执行操作
	execResults, execErr := c.executeActions(actions, "se")

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
			// [v0.9.7] 路由到PM：让PM分析SE执行错误并决定下一步
			if c.pmProcessor != nil {
				c.emitStatus("review", "pm", "busy")
				c.emit("pm_review", "@USR 🔄 PM正在分析SE执行错误...")
				pmCtx := fmt.Sprintf("⚠️ **SE执行出错**，经%d次自动修复均失败。\n\n错误: %v\n\n执行的Actions: %v\n执行结果: %v\n\n用户请求: %s\n\n请分析原因并决定下一步：\n- 如果可修复→@SE给出修正方案重新执行\n- 如果需要用户输入→@USR提问\n- 如果无法完成→@USR说明原因",
					maxSelfFix, result.Error, actions, execResults, userMsg)
				entries := c.memory.GetAll()
				history := make([]ai.ChatMessage, 0, len(entries))
				for _, e := range entries {
					history = append(history, ai.ChatMessage{Role: mapRoleToStandard(e.Role), Content: e.Content})
				}
				pmResp, pmErr := c.pmProcessor.ProcessStream(pmCtx, history, func(delta string) {
					trimmed := strings.TrimSpace(delta)
					if trimmed == "" || strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") || strings.HasPrefix(trimmed, "```") {
						return
					}
					c.emitChunk(delta)
				})
				if pmErr == nil && pmResp != nil {
					c.memory.Add(RolePM, pmResp.Content)
					if strings.Contains(pmResp.Content, "@SE") {
						// PM给了修正方案，再试一次
						seResponse2, seErr2 := c.callAI(RoleSE, pmResp.Content, c.memory.FormatForPrompt())
						if seErr2 == nil {
							c.memory.Add(RoleSE, "SE PM-guided retry")
							actions2, _ := c.parseSEResponse(seResponse2)
							actions2 = c.ensureExecAction(actions2)
							if len(actions2) > 0 {
								execResults2, execErr2 := c.executeActions(actions2, "se")
								if execErr2 == nil && c.seExecutionSatisfied(execResults2) {
									execResults = execResults2
									actions = actions2
									result.Error = nil
									selfAttempt = 0 // 重置让循环退出
								}
							}
						}
					}
				}
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
- Syntax error? Rewrite the COMPLETE file with correct syntax
- Missing exec? Add {"type":"exec","command":"go run filename.go"}
- Wrong path? Use relative path only (just filename, not full path)
- **Compiler/Interpreter not found?** Check error message for "不是内部或外部命令" or "command not found"
  - If Rust needed → suggest user install from https://rustup.rs/
  - If Go/Python/Node available → switch language instead of retrying same command
  - NEVER retry exec if compiler is missing (will fail 5 times uselessly)

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
			// 熔断器打开后继续重试无意义，立即终止
			if strings.Contains(seErr.Error(), "circuit breaker") {
				result.Error = fmt.Errorf("circuit breaker open after %d attempts: %w", selfAttempt+1, seErr)
				break
			}
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
		execResults, execErr = c.executeActions(actions, "se")
	}

	result.Outputs = execResults
	result.Actions = actions

	// --- Post-Execution Summary ---
	// 如果 SE 原始响应只有 JSON action 没有自然语言总结，注入执行结果让 AI 补充
	seDisplay := c.extractDisplayText(seResponse)
	if len(strings.TrimSpace(seDisplay)) < 20 && len(execResults) > 0 {
		c.emit("se_to_pm", "📝 SE 正在生成执行结果总结...")
		summaryPrompt := fmt.Sprintf(`你刚刚完成了以下操作，结果如下：

【用户原始请求】%s
【执行的 Actions】%v
【执行结果】%s

请用简洁的自然语言向用户汇报：
1. 你做了什么（文件名/命令）
2. 执行结果（成功/失败/内容摘要）
3. 如果是读取文件类任务，请总结文件要点（不要全文粘贴）

直接输出总结即可，不要输出 JSON。`, userMsg, actions, strings.Join(execResults, "\n"))
		summaryResp, summaryErr := c.callAI(RoleSE, summaryPrompt, c.memory.FormatForPrompt())
		if summaryErr == nil && len(summaryResp) > 10 {
			seDisplay = c.extractDisplayText(summaryResp)
			c.memory.Add(RoleSE, fmt.Sprintf("SE post-execution summary generated (len=%d)", len(seDisplay)))
			fmt.Printf("[Core:SE] Post-execution summary: %d chars\n", len(seDisplay))
		} else {
			fmt.Printf("[Core:SE] Post-execution summary skipped: err=%v\n", summaryErr)
			// fallback: 用执行结果的拼接作为显示文本
			seDisplay = fmt.Sprintf("✅ 执行完成 (%d 个操作):\n%s\n", len(actions), strings.Join(execResults, "\n"))
		}
	}

	c.emit("se_to_pm", seDisplay)

	if result.Error != nil {
		c.emitStatus("error", "se", "idle")
		return result
	}

	c.memory.Add(RoleSE, fmt.Sprintf("SE completed. Actions: %d, Results: %v", len(actions), execResults))

	// 📋 TODO: SE执行完成，标记Review为doing
	c.todo.CompleteCurrent()  // SE done
	c.todo.MarkCurrentDoing() // Review start

	// --- Phase 2-3 Loop: SE Execution + PM Review with Retry ---
	maxReviewRetries := 2
	var reviewResult ReviewResult

	for reviewAttempt := 0; reviewAttempt < maxReviewRetries; reviewAttempt++ {
		if reviewAttempt > 0 {
			c.emitStatus("se", "se", "busy")
			c.emit("se_to_pm", fmt.Sprintf("🔄 SE Retry #%d (PM Feedback): %s", reviewAttempt, reviewResult.Reason))

			// [v0.7.2] 硬编码TODO已禁用
			// c.todo.AddTask(...)
			// c.todo.MarkCurrentDoing()

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
			execResults, execErr = c.executeActions(actions, "se")
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
			// [v0.7.2] 硬编码TODO已禁用
			// c.todo.CompleteCurrent()
			// c.todo.MarkCurrentDoing()

			break
		}

		c.emit("pm_review", fmt.Sprintf("@SE ❌ PM rejected (attempt %d/%d): %s", reviewAttempt+1, maxReviewRetries, reviewResult.Reason))

		if reviewAttempt == maxReviewRetries-1 {
			rejectMsg := fmt.Sprintf("PM审核拒绝（已重试%d次）: %s", maxReviewRetries, reviewResult.Reason)
			c.emit("error", fmt.Sprintf("V2 Error: PM review rejected after %d attempts: %s", maxReviewRetries, reviewResult.Reason))
			result.Error = fmt.Errorf("PM review rejected after %d attempts: %s", maxReviewRetries, reviewResult.Reason)
			c.emit("pm_to_user", "@USR ❌ "+rejectMsg)
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
				c.emit("pm_to_user", fmt.Sprintf("@USR ❌ AP终审拒绝: %s", apResult.Reason))
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
			execResults, execErr = c.executeActions(actions, "se")
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
		c.emit("pm_to_user", fmt.Sprintf("@USR ❌ 任务最终失败: %s", apResult.Reason))
		c.emitStatus("error", "none", "idle")
		result.Error = fmt.Errorf("AP final rejection: %s", apResult.Reason)
		return result
	}

	c.emitStatus("done", "none", "idle")

	// [v0.7.2] 硬编码TODO已禁用

	// [v0.7.2] AP通过后，PM收尾：@USR向用户简要总结（无工具，不带历史目录扫描）
	pmHadFinalSummary := false
	if c.pmProcessor != nil {
		fmt.Println("[Core-PM] 🎯 AP审批通过，请求PM做最终总结...")
		summaryMsg := "当前任务已通过AP最终审批。请@USR向用户做一句话总结（只说本次做了什么，不要列举其他文件）。"
		// 用 callAI 而不是 ProcessStream，不带工具，避免 PM 目录扫描后列出所有历史文件
		summary, err := c.callAI(RolePM, summaryMsg, c.memory.FormatForPrompt())
		if err != nil {
			fmt.Printf("[Core-PM] ⚠️ 最终总结失败: %v\n", err)
		} else if strings.TrimSpace(summary) != "" {
			fmt.Printf("[Core-PM] ✅ 最终总结: %s\n", summary)
			c.emit("pm_to_user", summary)
			c.memory.Add(RolePM, summary)
			pmHadFinalSummary = true
		}
	}
	// 兜底：确保一定有pm_to_user（防止pmProcessor为nil或总结失败）
	if !pmHadFinalSummary {
		fallbackSummary := fmt.Sprintf("✅ 任务完成（执行%d个操作）", len(result.Actions))
		c.emit("pm_to_user", fallbackSummary)
	}

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

// checkToolAvailability 检测工具链是否可用（Layer 1预检）
// 返回：可用工具列表 + 缺失工具列表 + 安装建议
func (c *ArgusCore) checkToolAvailability(language string) (available []string, missing []string, hints []string) {
	// 常见语言→编译器/解释器映射
	toolMap := map[string][]string{
		"go":     {"go"},
		"python": {"python", "python3"},
		"rust":   {"rustc", "cargo"},
		"nodejs": {"node", "npm"},
		"java":   {"javac", "java"},
		"c/c++":  {"gcc", "g++", "clang"},
		"ruby":   {"ruby"},
		"php":    {"php"},
	}

	lowerLang := strings.ToLower(language)
	var tools []string
	if toolsList, ok := toolMap[lowerLang]; ok {
		tools = toolsList
	} else {
		// 未知语言，尝试从常见映射中查找
		for lang, tl := range toolMap {
			if strings.Contains(lowerLang, lang) || strings.Contains(lang, lowerLang) {
				tools = tl
				break
			}
		}
	}

	if len(tools) == 0 {
		// 无法识别的语言，假设用户知道自己在做什么
		return nil, nil, nil
	}

	for _, tool := range tools {
		cmd := exec.Command("where", tool) // Windows
		if runtime.GOOS != "windows" {
			cmd = exec.Command("which", tool)
		}
		err := cmd.Run()
		if err != nil {
			missing = append(missing, tool)

			// 生成安装提示
			switch tool {
			case "rustc", "cargo":
				hints = append(hints, fmt.Sprintf("🔧 Install Rust: https://rustup.rs/ or `winget install Rustlang.Rust.MSVC`"))
			case "go":
				hints = append(hints, fmt.Sprintf("🔧 Install Go: https://go.dev/dl/ or `winget install GoLang.Go`"))
			case "python", "python3":
				hints = append(hints, fmt.Sprintf("🔧 Install Python: https://python.org or `winget install Python.Python.3.12`"))
			case "node", "npm":
				hints = append(hints, fmt.Sprintf("🔧 Install Node.js: https://nodejs.org or `winget install OpenJS.NodeJS`"))
			case "gcc", "g++", "clang":
				hints = append(hints, fmt.Sprintf("🔧 Install C/C++ compiler: Visual Studio Build Tools or MinGW-w64"))
			default:
				hints = append(hints, fmt.Sprintf("🔧 Install %s: check official website", tool))
			}
		} else {
			available = append(available, tool)
		}
	}

	return available, missing, hints
}

func (c *ArgusCore) seExecutionSatisfied(results []string) bool {
	if len(results) == 0 {
		return false
	}
	// Must have at least one successful operation (any tool type)
	successPrefixes := []string{"✅ exec", "✅ read_file", "✅ write_file", "✅ edit_file",
		"✅ read_pdf", "✅ read_docx", "✅ write_docx", "✅ compare_docs",
		"✅ ensure_tool", "✅ install_pkg", "✅ search_code", "✅ list_files", "✅ delete_file",
		"✅ complete_task"}
	hasSuccess := false
	for _, r := range results {
		for _, prefix := range successPrefixes {
			if strings.HasPrefix(r, prefix) {
				hasSuccess = true
				break
			}
		}
		if hasSuccess {
			break
		}
	}
	if !hasSuccess {
		return false
	}
	// Also must not have any hard failures
	for _, r := range results {
		if strings.Contains(r, "❌ exec") || strings.Contains(r, "❌ read_file") ||
			strings.Contains(r, "syntax error") {
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
	// [P0] 环境检测：编译器/解释器未安装
	if strings.Contains(errLower, "不是内部或外部命令") ||
		strings.Contains(errLower, "not recognized") ||
		strings.Contains(errLower, "command not found") ||
		strings.Contains(errLower, "no such file or directory") {

		// 提取缺失的命令名
		cmdName := ""
		if idx := strings.Index(errLower, "'"); idx != -1 {
			endIdx := strings.Index(errLower[idx+1:], "'")
			if endIdx != -1 {
				cmdName = errLower[idx+1 : idx+1+endIdx]
			}
		}

		var installHint string
		switch cmdName {
		case "rustc", "cargo":
			installHint = "🔧 Install Rust: https://rustup.rs/ or `winget install Rustlang.Rust.MSVC`"
		case "go":
			installHint = "🔧 Install Go: https://go.dev/dl/ or `winget install GoLang.Go`"
		case "python", "python3", "pip":
			installHint = "🔧 Install Python: https://python.org or `winget install Python.Python.3.12`"
		case "node", "npm":
			installHint = "🔧 Install Node.js: https://nodejs.org or `winget install OpenJS.NodeJS`"
		case "gcc", "g++", "clang", "make":
			installHint = "🔧 Install C/C++ compiler: Visual Studio Build Tools or MinGW-w64"
		default:
			installHint = fmt.Sprintf("🔧 Install required tool: %s", cmdName)
		}

		analysis = append(analysis, fmt.Sprintf("❌ MISSING COMPILER/RUNTIME: %s\n%s", cmdName, installHint))
		analysis = append(analysis, "💡 Suggestion: Switch to an available language (Go/Python/Node.js) OR install the missing tool")
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
				Task          string   `json:"task"`
				IsProgramming bool     `json:"is_programming"`
				Files         []string `json:"files"`
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
		if len(response) > 400 {
			return response[:400]
		}
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

// pmDirectExecute [v0.8] PM直执模式 — Featherweight任务，PM在自己工位上用SE工具直接执行
// 不换帽子、不换工位、不走SE/Review/AP
// LLM调用：正常1次，出错最多重试3次（共4次）
func (c *ArgusCore) pmDirectExecute(userMsg string, pmResponse string, result *ProcessResult) *ProcessResult {
	fmt.Printf("[Core:Feather] ⚡ PM直执模式启动\n")

	c.emitStatus("execute", "pm", "busy")

	start := time.Now()

	// [FIX-v1.0.22] 合并 PMTools + SETools，让 short-process 也能用 ide_send 等 PM 工具
	featherTools := ai.MergePMAndSETools()
	fmt.Printf("[Core:Feather] ⚡ 合并工具集: PM=%d + SE=%d = 总%d\n",
		len(ai.PMTools), len(ai.SETools), len(featherTools))

	// Step 1: 调用 LLM（PM自己工位 + 全部工具，不换工位）
	systemPrompt := c.prompts.Get(RolePM)
	execPrompt := fmt.Sprintf(`Execute the task directly. No analysis.

User request: %s

Requirements:
1. Return complete actions in one response (write_file + exec)
2. exec must verify the code runs`, userMsg)

	var actions []ai.SEAction
	var displayContent string
	var callErr error

	// 构建记忆历史
	memEntries := c.memory.GetAll()
	memHistory := make([]ai.Message, 0, len(memEntries))
	for _, e := range memEntries {
		memHistory = append(memHistory, ai.Message{Role: mapRoleToStandard(e.Role), Content: e.Content})
	}

	// 第一次 LLM 调用
	callCtx, callCancel := context.WithTimeout(c.ctx, c.timeout)
	var resp *ai.ChatResponse
	resp, callErr = c.client.ChatWithTools(callCtx, systemPrompt, memHistory, execPrompt, featherTools, c.language)
	callCancel()

	if callErr != nil {
		result.Error = fmt.Errorf("pmDirectExecute LLM call failed: %w", callErr)
		c.emit("pm_to_user", fmt.Sprintf("@USR PM直执失败: %v", callErr))
		c.emitStatus("error", "pm", "idle")
		return result
	}

	if len(resp.Choices) == 0 {
		result.Error = fmt.Errorf("pmDirectExecute: no response from AI")
		c.emit("pm_to_user", "@USR 无响应")
		c.emitStatus("error", "pm", "idle")
		return result
	}

	msg := resp.Choices[0].Message
	displayContent = msg.Content

	// 提取 ToolCalls 为 actions
	if len(msg.ToolCalls) > 0 {
		for _, tc := range msg.ToolCalls {
			var args map[string]interface{}
			json.Unmarshal([]byte(tc.Function.Arguments), &args)
			action := ai.SEAction{Type: tc.Function.Name}
			if p, ok := args["path"].(string); ok {
				action.Path = p
			}
			if ct, ok := args["content"].(string); ok {
				action.Content = ct
			}
			if cmd, ok := args["command"].(string); ok {
				action.Command = cmd
			}
			actions = append(actions, action)
		}
	}

	// [v0.8.2] ToolCalls=0 重试：LLM 可能返回纯文本而非工具调用，重试强制使用工具
	maxToolRetries := 2
	for toolAttempt := 0; toolAttempt <= maxToolRetries; toolAttempt++ {
		if len(msg.ToolCalls) > 0 {
			break // 有 tool calls，正常继续
		}
		if toolAttempt == maxToolRetries {
			break // 重试耗尽，走文本回退路径
		}
		fmt.Printf("[Core:Feather] ⚠️ LLM未返回ToolCalls (attempt %d/%d), 重试...\n", toolAttempt+1, maxToolRetries)

		// 强化 prompt 强制使用工具
		forcePrompt := fmt.Sprintf(`【重要】你必须使用工具调用（function call）来完成此任务！

用户请求：%s

你上次返回了纯文本而没有使用工具。这是错误的！
请务必使用以下工具：
1. write_file - 创建代码文件（必须）
2. exec - 执行命令验证（必须）

返回格式示例：
- function_call: write_file(path="hello.go", content="...")
- function_call: exec(command="go run hello.go")

现在请重新执行，直接返回工具调用！`, userMsg)

		forceCtx, forceCancel := context.WithTimeout(c.ctx, c.timeout)
		var resp2 *ai.ChatResponse
		resp2, callErr = c.client.ChatWithTools(forceCtx, systemPrompt, memHistory, forcePrompt, featherTools, c.language)
		forceCancel()

		if callErr != nil || len(resp2.Choices) == 0 {
			continue // 重试
		}
		msg = resp2.Choices[0].Message
		displayContent = msg.Content

		if len(msg.ToolCalls) > 0 {
			actions = nil
			for _, tc := range msg.ToolCalls {
				var args map[string]interface{}
				json.Unmarshal([]byte(tc.Function.Arguments), &args)
				action := ai.SEAction{Type: tc.Function.Name}
				if p, ok := args["path"].(string); ok {
					action.Path = p
				}
				if ct, ok := args["content"].(string); ok {
					action.Content = ct
				}
				if cmd, ok := args["command"].(string); ok {
					action.Command = cmd
				}
				actions = append(actions, action)
			}
			fmt.Printf("[Core:Feather] ✅ ToolCalls重试成功 (attempt %d)\n", toolAttempt+1)
		}
	}

	phaseFE := PhaseResult{Phase: PhaseExecute, Role: RolePM, Input: execPrompt, Output: msg.Content, Raw: msg.Content, Duration: time.Since(start)}
	result.Phases = append(result.Phases, phaseFE)

	// 如果没有 actions（PM 只返回了文本），直接展示
	if len(actions) == 0 {
		dc := displayContent
		if strings.HasPrefix(dc, "@USR") {
			dc = strings.TrimSpace(strings.TrimPrefix(dc, "@USR"))
		}
		c.emit("pm_to_user", fmt.Sprintf("@USR %s", dc))
		c.memory.Add(RolePM, msg.Content)
		c.emitStatus("done", "none", "idle")
		result.Success = true
		return result
	}

	// Step 2: 执行 actions（executor="pm"）
	t2 := time.Now()
	execResults, execErr := c.executeActions(actions, "pm")
	os.WriteFile("timing_log.txt", []byte(fmt.Sprintf("executeActions: %v\n", time.Since(t2))), 0644)
	result.Outputs = execResults
	result.Actions = actions

	// [v0.8.2] 自动补 exec：LLM只写了代码没运行，根据文件类型自动执行（不消耗LLM调用）
	originalHasExec := false
	for _, a := range actions {
		if a.Type == "exec" {
			originalHasExec = true
			break
		}
	}
	if execErr == nil {
		hasWriteFile := false
		var lastPath string
		for _, a := range actions {
			if a.Type == "write_file" || a.Type == "edit_file" {
				hasWriteFile = true
				if a.Path != "" {
					lastPath = a.Path
				}
			}
		}
		if hasWriteFile && !originalHasExec && lastPath != "" {
			cmd := inferExecCommand(lastPath)
			if cmd != "" {
				ta := time.Now()
				autoAction := ai.SEAction{Type: "exec", Command: cmd}
				autoResult, _ := c.executeActions([]ai.SEAction{autoAction}, "pm")
				os.WriteFile("timing_log.txt", []byte(fmt.Sprintf("executeActions: %v\nfirst: %v\nauto-exec: %v\n", time.Since(t2), time.Since(t2), time.Since(ta))), 0644)
				execResults = append(execResults, autoResult...)
				actions = append(actions, autoAction)
				result.Actions = actions
				result.Outputs = execResults
				if len(autoResult) > 0 && strings.HasPrefix(autoResult[0], "✅") {
					execErr = nil
				}
			}
		}
	}

	// Step 3a: 多轮延续 — 工具执行后让 LLM 继续处理（如删除空目录）
	if execErr == nil && len(execResults) > 0 {
		maxFollow := 3
		followHistory := make([]ai.Message, 0, len(memHistory)+10)
		followHistory = append(followHistory, memHistory...)
		followHistory = append(followHistory, ai.Message{Role: "user", Content: execPrompt})
		followHistory = append(followHistory, msg)
		for _, r := range execResults {
			followHistory = append(followHistory, ai.Message{Role: "tool", Content: r})
		}
		for fr := 0; fr < maxFollow; fr++ {
			prompt := "[工具已执行，请继续分析。若需进一步操作（如删除空目录）请继续调用工具；若已完成请回复用户。]"
			followHistory = append(followHistory, ai.Message{Role: "user", Content: prompt})
			fCtx, fCancel := context.WithTimeout(c.ctx, c.timeout)
			fResp, fErr := c.client.ChatWithTools(fCtx, systemPrompt, followHistory, prompt, featherTools, c.language)
			fCancel()
			if fErr != nil || len(fResp.Choices) == 0 {
				break
			}
			fMsg := fResp.Choices[0].Message
			if len(fMsg.Content) > 20 {
				displayContent = fMsg.Content
			}
			if len(fMsg.ToolCalls) == 0 {
				break
			}
			var more []ai.SEAction
			for _, tc := range fMsg.ToolCalls {
				var args map[string]interface{}
				json.Unmarshal([]byte(tc.Function.Arguments), &args)
				a := ai.SEAction{Type: tc.Function.Name}
				if p, ok := args["path"].(string); ok {
					a.Path = p
				}
				if ct, ok := args["content"].(string); ok {
					a.Content = ct
				}
				if cmd, ok := args["command"].(string); ok {
					a.Command = cmd
				}
				more = append(more, a)
			}
			if len(more) == 0 {
				break
			}
			moreResults, moreErr := c.executeActions(more, "pm")
			execResults = append(execResults, moreResults...)
			actions = append(actions, more...)
			result.Outputs = execResults
			result.Actions = actions
			if moreErr != nil {
				execErr = moreErr
				break
			}
			for _, r := range moreResults {
				followHistory = append(followHistory, ai.Message{Role: "tool", Content: r})
			}
		}
	}

	// Step 3b: 检查结果 + 重试（最多3次）
	maxRetries := 3
	for attempt := 1; attempt <= maxRetries; attempt++ {
		// 先检查核心任务是否已完成（有✅结果），即使清理命令报错也视为成功
		if c.seExecutionSatisfied(execResults) {
			execErr = nil
			break
		}
		if execErr == nil {
			break
		}
		if attempt == maxRetries {
			if execErr != nil {
				result.Error = fmt.Errorf("pmDirectExecute failed after %d retries: %w", maxRetries, execErr)
			} else {
				result.Error = fmt.Errorf("pmDirectExecute incomplete after %d retries", maxRetries)
			}
			fmt.Printf("[Core:Feather] ❌ PM直执失败 (重试%d次耗尽)\n", maxRetries)
			break
		}
		feedbackErr := "<no error>"
		if execErr != nil {
			feedbackErr = execErr.Error()
		}
		fmt.Printf("[Core:Feather] 🔄 PM重试 #%d/%d: %s\n", attempt, maxRetries, feedbackErr)
		fixPrompt := fmt.Sprintf(`⚠️ 上次执行结果不符合预期，请修复后重新返回 actions。

错误/输出: %s
你尝试的actions: %v
执行结果: %v

任务: %s

要求：
1. 分析输出/错误，判断问题原因
2. 返回修正后的 actions（write_file/edit_file/exec 等）
3. 如果 exec 非零退出，输出本身可能就是正确数据，不一定需要重试
4. 最多重试 3 次，仍不行就 @USR 汇报`,
			feedbackErr, actions, execResults, userMsg)
		callCtx2, cancel2 := context.WithTimeout(c.ctx, c.timeout)
		resp2, err2 := c.client.ChatWithTools(callCtx2, systemPrompt, memHistory, fixPrompt, featherTools, c.language)
		cancel2()
		if err2 != nil {
			fmt.Printf("[Core:Feather] ⚠️ 重试LLM调用失败: %v\n", err2)
			continue
		}
		if len(resp2.Choices) == 0 {
			continue
		}
		msg2 := resp2.Choices[0].Message
		if len(msg2.Content) > 20 {
			displayContent = msg2.Content
		}
		actions = nil
		for _, tc := range msg2.ToolCalls {
			var args map[string]interface{}
			json.Unmarshal([]byte(tc.Function.Arguments), &args)
			a := ai.SEAction{Type: tc.Function.Name}
			if p, ok := args["path"].(string); ok {
				a.Path = p
			}
			if ct, ok := args["content"].(string); ok {
				a.Content = ct
			}
			if cmd, ok := args["command"].(string); ok {
				a.Command = cmd
			}
			actions = append(actions, a)
		}
		if len(actions) > 0 {
			execResults, execErr = c.executeActions(actions, "pm")
			result.Outputs = execResults
			result.Actions = actions
		}
	}

	// Step 4: 汇报结果（emit到前端 + 存memory供上下文）
	os.WriteFile("F:\\ArgusTek\\Argus\\debug_step4.txt", []byte(fmt.Sprintf("pmDirectExecute Step4 REACHED at %v\ndisplayContent=%q\nexecResults=%v\nlen(execResults)=%d\n", time.Now(), displayContent, execResults, len(execResults))), 0644)
	if result.Error != nil {
		c.emit("pm_to_user", fmt.Sprintf("@USR ❌ %v", result.Error))
		c.memory.Add(RolePM, fmt.Sprintf("❌ %v", result.Error))
	} else if execErr != nil {
		c.emit("pm_to_user", fmt.Sprintf("@USR ❌ %v", execErr))
		c.memory.Add(RolePM, fmt.Sprintf("❌ %v", execErr))
	} else if len(execResults) > 0 {
		execText := strings.Join(execResults, "\n")
		hasExecResult := strings.Contains(execText, "✅ exec") || strings.Contains(execText, "❌ exec")
		// [v1.0.21] 优先使用LLM叙事总结（说人话），降级到原始工具结果
		cleanSummary := c.extractCleanSummary(displayContent)
		if cleanSummary != "" && len(cleanSummary) > 10 {
			c.memory.Add(RolePM, cleanSummary)
			c.emit("pm_to_user", "@USR "+cleanSummary)
		} else if hasExecResult {
			c.memory.Add(RolePM, execText)
			c.emit("pm_to_user", "@USR "+execText)
		} else {
			c.memory.Add(RolePM, execText)
			c.emit("pm_to_user", "@USR "+execText)
		}
	} else if len(strings.TrimSpace(displayContent)) > 0 {
		cleanSummary := c.extractCleanSummary(displayContent)
		if cleanSummary != "" {
			c.emit("pm_to_user", "@USR "+cleanSummary)
			c.memory.Add(RolePM, cleanSummary)
		}
	}
	c.emitStatus("done", "none", "idle")
	result.Success = (result.Error == nil && execErr == nil)
	return result
}

func (c *ArgusCore) executeActions(actions []ai.SEAction, executor string) ([]string, error) {
	outputs := make([]string, 0, len(actions))

	if executor == "" {
		executor = "se"
	}

	c.emitAction("exec_start", map[string]interface{}{
		"executor": executor,
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
			"executor": executor,
			"index":    i + 1,
			"total":    len(actions),
			"type":     action.Type,
			"label":    label,
		})

		var output string
		var err error

		switch action.Type {
		case "read_file":
			absPath := c.resolvePath(action.Path)
			content, readErr := c.executor.ReadFile(absPath)
			if readErr != nil {
				output = fmt.Sprintf("❌ read_file error: %v", readErr)
			} else {
				output = fmt.Sprintf("✅ read_file %s (%d bytes)\n%s",
					action.Path, len(content), truncateContent(content, 8000))
			}
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
				"executor":  executor,
				"command":   action.Command,
				"output":    output,
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
		case "delete_file":
			err = c.executor.DeleteFile(action.Path)
			if err == nil {
				output = fmt.Sprintf("✅ delete_file %s", action.Path)
			} else {
				output = fmt.Sprintf("❌ delete_file %s error: %v", action.Path, err)
			}
		case "glob":
			globPattern := filepath.Join(c.workDir, action.Pattern)
			matches, globErr := filepath.Glob(globPattern)
			if globErr != nil {
				output = fmt.Sprintf("❌ glob '%s' error: %v", action.Pattern, globErr)
			} else if len(matches) == 0 {
				output = fmt.Sprintf("📁 glob '%s' 无匹配", action.Pattern)
			} else {
				var rels []string
				for _, m := range matches {
					rel, _ := filepath.Rel(c.workDir, m)
					rels = append(rels, rel)
				}
				output = fmt.Sprintf("📁 glob '%s' → %d 个文件:\n%s", action.Pattern, len(matches), strings.Join(rels, "\n"))
			}
		case "list_files":
			files, listErr := c.executor.ListFiles()
			if listErr != nil {
				output = fmt.Sprintf("❌ list_files error: %v", listErr)
			} else {
				output = fmt.Sprintf("📁 %d files:\n", len(files))
				for _, f := range files {
					output += fmt.Sprintf("  %s (%d bytes)\n", f.Path, f.Size)
				}
			}
		// ========== 文档处理工具 ==========
		case "read_pdf":
			absPath := c.resolvePath(action.Path)
			result, docErr := ai.ReadPDF(absPath, action.UseOCR)
			if docErr != nil {
				output = fmt.Sprintf("❌ read_pdf error: %v", docErr)
			} else {
				output = fmt.Sprintf("✅ read_pdf %s (pages:%d words:%d)\n%s",
					action.Path, result.Meta.Pages, result.Meta.WordCount,
					truncateContent(result.Content, 8000))
			}
		case "read_docx":
			absPath := c.resolvePath(action.Path)
			result, docErr := ai.ReadDocx(absPath)
			if docErr != nil {
				output = fmt.Sprintf("❌ read_docx error: %v", docErr)
			} else {
				output = fmt.Sprintf("✅ read_docx %s (tables:%d words:%d)\n%s",
					action.Path, result.Meta.Tables, result.Meta.WordCount,
					truncateContent(result.Content, 8000))
			}
		case "write_docx":
			absPath := c.resolvePath(action.Path)
			result, docErr := ai.WriteDocx(absPath, action.DocContent)
			if docErr != nil {
				output = fmt.Sprintf("❌ write_docx error: %v", docErr)
			} else {
				output = fmt.Sprintf("✅ write_docx %s (%d bytes)", action.Path, result.Meta.Size)
			}
		case "compare_docs":
			pathA := c.resolvePath(action.Path)
			pathB := c.resolvePath(action.ComparePathB)
			result, docErr := ai.CompareDocs(pathA, pathB)
			if docErr != nil {
				output = fmt.Sprintf("❌ compare_docs error: %v", docErr)
			} else {
				output = fmt.Sprintf("✅ compare_docs %s vs %s\n%s",
					action.Path, action.ComparePathB,
					truncateContent(result.Content, 10000))
			}
		// ========== 工具自举 ==========
		case "ensure_tool":
			toolName := action.ToolName
			if toolName == "" {
				toolName = "read_pdf"
			}
			ready, missing, _ := ai.EnsureTool(toolName)
			if ready {
				output = fmt.Sprintf("✅ ensure_tool '%s' — 所有依赖已就绪", toolName)
			} else {
				success, installLog := ai.AutoInstallDeps(toolName)
				if success {
					output = fmt.Sprintf("✅ ensure_tool '%s' — 依赖已自动安装:\n%s", toolName, installLog)
				} else {
					output = fmt.Sprintf("⚠️ ensure_tool '%s' — 缺失: %s\n%s\n请手动安装后重试",
						toolName, strings.Join(missing, ", "), installLog)
				}
			}
		case "install_pkg":
			pkgManager := action.PkgManager
			if pkgManager == "" {
				pkgManager = "pip"
			}
			pkgName := action.PkgName
			var installCmd string
			switch pkgManager {
			case "pip":
				installCmd = fmt.Sprintf("pip install %s", pkgName)
			case "npm":
				installCmd = fmt.Sprintf("npm install -g %s", pkgName)
			case "cargo":
				installCmd = fmt.Sprintf("cargo install %s", pkgName)
			case "go":
				installCmd = fmt.Sprintf("go install %s@latest", pkgName)
			default:
				installCmd = fmt.Sprintf("pip install %s", pkgName)
			}
			output, err = c.executor.Exec(installCmd, 120*time.Second)
			if err != nil {
				output = fmt.Sprintf("❌ install_pkg '%s' via %s error: %v\noutput: %s", pkgName, pkgManager, err, output)
			} else {
				output = fmt.Sprintf("✅ install_pkg '%s' via %s succeeded\n%s", pkgName, pkgManager, truncateContent(output, 2000))
			}
		// ========== DAP 断点调试工具 ==========
		case "debug_start":
			mode := action.DebugMode
			if mode == "" {
				mode = "test"
			}
			stopOnEntry := action.DebugStopOnEntry
			session, dbgErr := c.debuggerMgr.StartDebug(action.Program, mode, action.Args, stopOnEntry)
			if dbgErr != nil {
				output = fmt.Sprintf("❌ debug_start error: %v", dbgErr)
				err = dbgErr
			} else {
				output = fmt.Sprintf("🐛 Debug session started [ID:%s] program=%s mode=%s",
					session.ID, session.Program, session.Mode)
			}
		case "debug_set_breakpoint":
			bp, bpErr := c.debuggerMgr.SetBreakpoint(c.getActiveDebugSessionID(), action.FilePath, action.Line, action.Condition)
			if bpErr != nil {
				output = fmt.Sprintf("❌ debug_set_breakpoint error: %v", bpErr)
				err = bpErr
			} else if !bp.Verified {
				output = fmt.Sprintf("⚠️  Breakpoint set at %s:%d (not verified - may not be valid code line)",
					action.FilePath, action.Line)
			} else {
				output = fmt.Sprintf("✅ Breakpoint #%d at %s:%d verified ✓", bp.ID, action.FilePath, bp.Line)
			}
		case "debug_continue":
			if stepErr := c.debuggerMgr.Continue(c.getActiveDebugSessionID()); stepErr != nil {
				output = fmt.Sprintf("❌ debug_continue error: %v", stepErr)
				err = stepErr
			} else {
				output = "▶️  Continued — running until next breakpoint"
				c.debuggerMgr.InvalidateCache(c.getActiveDebugSessionID())
			}
		case "debug_step_over":
			if stepErr := c.debuggerMgr.Next(c.getActiveDebugSessionID()); stepErr != nil {
				output = fmt.Sprintf("❌ debug_step_over error: %v", stepErr)
				err = stepErr
			} else {
				output = "⤵️  Step Over"
				c.debuggerMgr.InvalidateCache(c.getActiveDebugSessionID())
			}
		case "debug_step_into":
			if stepErr := c.debuggerMgr.StepIn(c.getActiveDebugSessionID()); stepErr != nil {
				output = fmt.Sprintf("❌ debug_step_into error: %v", stepErr)
				err = stepErr
			} else {
				output = "⤵️  Step Into"
				c.debuggerMgr.InvalidateCache(c.getActiveDebugSessionID())
			}
		case "debug_step_out":
			if stepErr := c.debuggerMgr.StepOut(c.getActiveDebugSessionID()); stepErr != nil {
				output = fmt.Sprintf("❌ debug_step_out error: %v", stepErr)
				err = stepErr
			} else {
				output = "⤴️  Step Out"
				c.debuggerMgr.InvalidateCache(c.getActiveDebugSessionID())
			}
		case "debug_pause":
			if pauseErr := c.debuggerMgr.Pause(c.getActiveDebugSessionID()); pauseErr != nil {
				output = fmt.Sprintf("❌ debug_pause error: %v", pauseErr)
				err = pauseErr
			} else {
				output = "⏸️  Paused"
			}
		case "debug_stop":
			if stopErr := c.debuggerMgr.StopDebug(c.getActiveDebugSessionID()); stopErr != nil {
				output = fmt.Sprintf("❌ debug_stop error: %v", stopErr)
				err = stopErr
			} else {
				output = "🛑 Debug session stopped"
			}
		case "debug_stacktrace":
			frames, stErr := c.debuggerMgr.GetCallStack(c.getActiveDebugSessionID())
			if stErr != nil {
				output = fmt.Sprintf("❌ debug_stacktrace error: %v", stErr)
				err = stErr
			} else {
				output = fmt.Sprintf("📋 Call Stack (%d frames):\n", len(frames))
				for i, f := range frames {
					src := ""
					if f.Source != nil {
						src = fmt.Sprintf("%s:%d", f.Source.Path, f.Line)
					}
					output += fmt.Sprintf("  #%d %s @ %s\n", i, f.Name, src)
				}
			}
		case "debug_variables":
			varsMap, vErr := c.debuggerMgr.GetVariables(c.getActiveDebugSessionID())
			if vErr != nil {
				output = fmt.Sprintf("❌ debug_variables error: %v", vErr)
				err = vErr
			} else {
				output = "📊 Variables:\n"
				for scopeName, vars := range varsMap {
					output += fmt.Sprintf("  [%s] (%d vars):\n", scopeName, len(vars))
					for _, v := range vars {
						vType := ""
						if v.Type != "" {
							vType = fmt.Sprintf(" [%s]", v.Type)
						}
						refHint := ""
						if v.VariablesReference > 0 {
							refHint = fmt.Sprintf(" (+%d children)", v.NamedVariables+v.IndexedVariables)
						}
						output += fmt.Sprintf("    %s%s = %s%s\n", v.Name, vType, v.Value, refHint)
					}
				}
			}
		case "debug_evaluate":
			result, evalErr := c.debuggerMgr.EvaluateExpression(c.getActiveDebugSessionID(), action.Expression)
			if evalErr != nil {
				output = fmt.Sprintf("❌ debug_evaluate '%s' error: %v", action.Expression, evalErr)
				err = evalErr
			} else {
				vType := ""
				if result.Type != "" {
					vType = fmt.Sprintf(" [%s]", result.Type)
				}
				output = fmt.Sprintf("🔍 eval(%s) = %s%s", action.Expression, result.Value, vType)
			}
		case "complete_task":
			output = "✅ complete_task 任务已完成"
		default:
			err = fmt.Errorf("unknown action type: %s", action.Type)
			output = fmt.Sprintf("❌ unknown action: %s", action.Type)
		}

		status := "done"
		if err != nil {
			status = "error"
		}

		c.emitAction("exec_done", map[string]interface{}{
			"executor": executor,
			"index":    i + 1,
			"total":    len(actions),
			"type":     action.Type,
			"label":    label,
			"status":   status,
			"error": func() string {
				if err != nil {
					return err.Error()
				}
				return ""
			}(),
		})

		outputs = append(outputs, output)
		// [v0.8] PM直执不逐条emit action结果（最终总结一条消息搞定）
		if executor != "pm" {
			c.emit(executor+"_to_user", output)
		}

		if err != nil {
			return outputs, fmt.Errorf("action %d (%s) failed: %w", i, action.Type, err)
		}
	}

	c.emitAction("exec_completed", map[string]interface{}{})

	return outputs, nil
}

func (c *ArgusCore) emitAction(eventName string, data interface{}) {
	// [v0.8] Featherweight静默模式：跳过action事件到前端（避免多余bubble）
	if c.silent {
		return
	}
	if c.onActionEvent != nil {
		c.onActionEvent(eventName, data)
	}
}

// resolvePath 将相对路径解析为绝对路径（基于工作目录）
func (c *ArgusCore) resolvePath(relPath string) string {
	if filepath.IsAbs(relPath) {
		return relPath
	}
	return filepath.Join(c.workDir, relPath)
}

// truncateContent 截断内容到指定长度，避免撑爆上下文
func truncateContent(content string, maxLen int) string {
	if len(content) <= maxLen {
		return content
	}
	return content[:maxLen] + fmt.Sprintf("\n... [截断，共 %d 字符]", len(content))
}

// inferExecCommand 根据文件扩展名推断执行命令
func inferExecCommand(filePath string) string {
	ext := strings.ToLower(filepath.Ext(filepath.Base(filePath)))
	switch ext {
	case ".go":
		return "go run " + filepath.Base(filePath)
	case ".py":
		return "python " + filepath.Base(filePath)
	case ".js":
		return "node " + filepath.Base(filePath)
	case ".ts":
		return "npx ts-node " + filepath.Base(filePath)
	case ".rs":
		return "cargo run"
	case ".sh":
		return "bash " + filepath.Base(filePath)
	case ".bat", ".cmd":
		return filepath.Base(filePath)
	}
	return ""
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
		if strings.HasPrefix(line, "@SE") {
			continue // SE指令不显示给用户
		}
		if strings.HasPrefix(line, "@USR") {
			// 保留 @USR 后面的用户可见内容
			line = strings.TrimSpace(strings.TrimPrefix(line, "@USR"))
		}
		if strings.HasPrefix(line, "```") {
			continue
		}
		displayLines = append(displayLines, line)
	}
	return strings.Join(displayLines, "\n")
}

// extractCleanSummary [v0.8] 从PM直执的LLM响应中提取干净的汇报文本
// 过滤掉：JSON代码块、中间思考过程、工具调用说明
func (c *ArgusCore) extractCleanSummary(content string) string {
	if content == "" {
		return ""
	}

	lines := strings.Split(content, "\n")
	var clean []string
	inJSONBlock := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		// 跳过 JSON 代码块
		if strings.HasPrefix(trimmed, "```") {
			inJSONBlock = !inJSONBlock
			continue
		}
		if inJSONBlock {
			continue
		}
		if strings.HasPrefix(trimmed, "{") && strings.Contains(trimmed, `"type"`) {
			continue
		}
		if strings.HasPrefix(trimmed, "[") {
			continue
		}

		// 跳过中间过程/思考文本
		skipPrefixes := []string{
			// 中文
			"让我执行", "我来执行", "我来直接", "正在执行",
			"写代码 +", "执行验证", "以下是", "包含操作",
			"我将", "我现在", "开始执行",
			"我直接执行", "这个 short", "这个任务",
			"为你创建", "帮你写", "这是",
			// 英文
			"Here are", "Let me", "I will", "I'm going",
			"Executing", "Creating", "Writing",
			"I'll create", "I'll write", "I'll now",
			"I've created", "I've written",
			"This is a", "This task",
			"Here's the", "The program",
			"The code", "The file",
		}
		isSkip := false
		for _, prefix := range skipPrefixes {
			if strings.HasPrefix(strings.ToLower(trimmed), strings.ToLower(prefix)) {
				isSkip = true
				break
			}
		}
		if isSkip {
			continue
		}

		// 保留 ⚡ 标记的行（这是正式汇报）
		clean = append(clean, trimmed)
	}

	result := strings.Join(clean, "\n")
	// 如果结果太长（>200字符），截取最后部分作为总结
	if len(result) > 200 {
		lines := strings.Split(result, "\n")
		if len(lines) > 3 {
			result = strings.Join(lines[len(lines)-3:], "\n")
		}
	}
	return result
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

// getActiveDebugSessionID 获取当前活跃的调试会话ID
// 如果没有活跃会话则返回空字符串（调用方需检查）
func (c *ArgusCore) getActiveDebugSessionID() string {
	sessions := c.debuggerMgr.GetAllSessions()
	if len(sessions) > 0 {
		return sessions[len(sessions)-1].ID // 返回最新的会话
	}
	return ""
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
