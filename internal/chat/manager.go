package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"argus/internal/ai"
	"argus/internal/board"
	"argus/internal/dingtalk"
	"argus/internal/executor"
	"argus/internal/i18n"
	"argus/internal/mcp"
	"argus/internal/memory"
	"argus/internal/monitor"
	"argus/internal/task"
	"argus/internal/types"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// filterDuplicateMentions 过滤掉重复的@标记，只保留第一个
func filterDuplicateMentions(content string) string {
	// 匹配 @XXX 格式（@后面跟着字母数字下划线）
	re := regexp.MustCompile(`@\w+`)
	mentions := re.FindAllString(content, -1)

	if len(mentions) <= 1 {
		return content
	}

	// 只保留第一个@，移除后面的所有@
	seen := make(map[string]bool)
	var firstMention string
	for _, m := range mentions {
		if !seen[m] {
			seen[m] = true
			if firstMention == "" {
				firstMention = m
			}
		}
	}

	// 替换所有@为占位符，然后只还原第一个
	temp := re.ReplaceAllString(content, "«MENTION»")
	// 只替换第一个占位符为第一个@
	temp = strings.Replace(temp, "«MENTION»", firstMention, 1)
	// 移除剩余的占位符及其后面的空格
	temp = regexp.MustCompile(`«MENTION»\s*`).ReplaceAllString(temp, "")

	return strings.TrimSpace(temp)
}

// isStatusOnlyMessage 判断PM回复是否为纯状态确认消息（不推动工作进展）
func isStatusOnlyMessage(content string) bool {
	cleaned := strings.TrimSpace(content)
	cleaned = strings.TrimPrefix(cleaned, "@SE")
	cleaned = strings.TrimPrefix(cleaned, "@PM")
	cleaned = strings.TrimPrefix(cleaned, "@USR")
	cleaned = strings.TrimSpace(cleaned)
	if cleaned == "" {
		return true
	}
	statusWords := []string{"收到", "待命", "明白", "知道了", "了解", "好的", "ok", "okay"}
	lower := strings.ToLower(cleaned)
	for _, word := range statusWords {
		if lower == word {
			return true
		}
	}
	return false
}

func (m *Manager) isDingTalkEnabled() bool {
	return m.dingTalkEnabled
}

func (m *Manager) SetDingTalkEnabled(enabled bool) {
	fmt.Fprintln(os.Stderr, "[dingtalk] SetDingTalkEnabled:", m.dingTalkEnabled, "->", enabled)
	m.dingTalkEnabled = enabled
}

// [v0.7.1] SetMCPManager 设置 MCP Manager（供 App 初始化后注入）
func (m *Manager) SetMCPManager(mgr *mcp.Manager) {
	m.mcpManager = mgr
}

// [v0.7.2] SetContextManagement 设置上下文管理三个组件（由 App 初始化后注入）
func (m *Manager) SetContextManagement(cw *memory.ContextWindow, cb *memory.ContextBuilder, c *memory.Compressor) {
	m.contextWindow = cw
	m.contextBuilder = cb
	m.compressor = c
}

// [v0.7.2] pushTokenStats 通过 MessageBus 推送 Token 统计数据到前端 TokenMonitor
func (m *Manager) pushTokenStats() {
	if m.contextWindow == nil {
		m.WriteDebugLog("[ContextBridge] ⚠ pushTokenStats 跳过: contextWindow=nil（SetContextManagement 未调用？）")
		return
	}
	if m.msgBus == nil {
		m.WriteDebugLog("[ContextBridge] ⚠ pushTokenStats 跳过: msgBus=nil")
		return
	}
	stats := m.contextWindow.TokenStats()
	msgId := m.msgBusSend("system", "", "token_stats", PathSystem, "ContextBridge:pushTokenStats", stats)
	m.WriteDebugLog(fmt.Sprintf("[ContextBridge] ✅ token_stats 已推送 msgId=%s total_tokens=%v", msgId, stats["total_tokens"]))
}

func (m *Manager) sendToDingTalk(msg string) {
	fmt.Fprintln(os.Stderr, "[dingtalk] sendToDingTalk called, enabled=", m.dingTalkEnabled)
	if !m.isDingTalkEnabled() {
		return
	}
	go func() {
		defer func() {
			if r := recover(); r != nil {
				fmt.Printf("[DingTalk] 💥 panic recovered: %v\n", r)
			}
		}()
		if err := dingtalk.SendMessageToLastSender(msg); err != nil {
			fmt.Fprintln(os.Stderr, "[dingtalk] 发送失败:", err)
		} else {
			fmt.Fprintln(os.Stderr, "[dingtalk] 发送成功:", msg[:min(50, len(msg))])
		}
	}()
}

// MessageCallback 消息回调函数类型
type MessageCallback func(msg Message)

// [G52] 送水审计条目（用于前后端一致性校验）
type StreamAuditEntry struct {
	Timestamp time.Time
	Role      string
	MessageID string
	Delta     string
	DeltaLen  int
}

// [G60] 收水审计条目（前端回传，用于对比校验）
type ReceiveAuditEntry struct {
	Timestamp time.Time
	Role      string
	MessageID string
	Content   string
	ContentLen int
	Source    string // 事件来源: ai-stream-chunk/pm_message/ap_message/new-message/exec_start
}

// Manager 对话管理器
type Manager struct {
	router      *Router
	aiClient    *ai.Client // 默认AI客户端（所有角色共用，除非有独立配置）
	pmClient    *ai.Client // PM专用客户端（nil时用aiClient）
	seClient    *ai.Client // SE专用客户端（nil时用aiClient）
	apClient    *ai.Client // AP专用客户端（nil时用aiClient）
	pmProcessor *ai.PMProcessor
	seProcessor *ai.SEProcessor
	apProcessor *ai.APProcessor
	pmExecutor      *executor.Executor
	seExecutor      *executor.Executor
	fileTracker     *executor.FileChangeTracker // 文件变更追踪（快照/回滚/冲突检测）
	lspClient       *ai.LSPClient             // [P0-1] LSP 客户端（gopls daemon）
	boardManager    *board.Manager
	cMonitor        *monitor.CMonitor
	memoryManager   *MemoryManager
	configManager   *ConfigManager
	envMemory       *EnvMemory
	terminalWriter  func(string) error
	sseBridge       *SSEBridge
	taskManager     *task.TaskManager
	backendStatus   *BackendStatus
	ReplyLanguage   string
	dingTalkEnabled bool
	stopCh          chan struct{} // goroutine 停止信号

	history []Message

	msgCounter int64             // 消息ID计数器（原子递增）
	lastMsgIDs map[string]string // 每个角色的最后一条消息ID (role -> msgID)
	streamingMsgIDs map[string]string // 流式消息ID追踪 (role -> messageId) [G49: 前后端一致性]
	streamAuditLog []StreamAuditEntry // [G52] 送水审计日志（用于前后端一致性校验）
	receiveAuditLog []ReceiveAuditEntry // [G60] 收水审计日志（前端回传，用于对比校验）

	reviewCount   int // PM审核轮次计数（防死循环）
	apReviewCount int // AP审核轮次计数（防死循环）
	mu            sync.RWMutex

	resetMu sync.RWMutex

	processingMu    sync.Mutex         // 消息处理互斥锁（防止并发消息导致SE循环）
	isProcessing    bool               // 是否正在处理消息
	processingStartTime time.Time      // 处理开始时间（用于超时检测）
	pendingQueue    []string           // 等待处理的消息队列
	pendingMu       sync.Mutex         // 队列互斥锁
	cancelFunc      context.CancelFunc // 取消当前AI调用的函数
	resetGeneration int64              // 复位世代号，每次复位+1，用于拦截复位后返回的幽灵调用

	currentRole           string        // 当前正在处理的角色
	currentSETask         string        // [FIX-20260529] SE当前正在执行的任务描述（互斥检查用）
	seContinueCount       int           // SE连续继续次数（防无限循环）
	seAskPMCount          int           // SE连续问PM次数（防needHelp死循环）
	seReportedComplete    bool          // SE是否已报告完成（防重复报告）
	seEmptyActionCount    int           // SE连续空actions次数（防空响应死循环）[FIX-20260528]
	apMode                RoleMode      // AP当前模式（AP管，C可超时强制改）
	handover              HandoverState // 交接状态（C监控用）
	pmReviewCycles        int           // PM→SE 验证循环次数，超过上限强制审批
	pmWaitingForUserSince int64         // PM等待USR决策的时间戳(0=未等待)
	pmWaitingNudgeCount   int           // 已催促USR的次数
	userStopped           bool          // 用户主动停止标志（防止C无限催促）
	seLastMessage         string        // SE上一条消息内容（用于检测重复）
	seRepeatCount         int           // SE重复消息计数（防死循环）
	pmSeLoopCount         int           // PM→SE 循环次数（防死循环，上限3次）
	lastMessageFrom       string        // 上一条消息来源角色（用于循环检测）
	lastMessageContent    string        // 上一条消息内容（用于循环检测）
	isRecovering          bool          // 恢复模式标志（防止重复addHistory）

	// PM健康状态追踪
	pmConsecutiveFailures int           // PM连续API失败次数
	pmLastFailureTime     time.Time     // PM最后一次失败时间
	pmUnhealthySince      time.Time     // PM进入不健康状态的时间点
	workDir               string
	configDir             string // IDE系统目录（调试日志存放位置）
	config                types.Config
	ctx                   context.Context // Wails context

	// 消息回调（用于同步到前端）
	onMessageAdded        MessageCallback
	onProjectStateChanged func(string)            // 项目状态变更回调
	onTaskRecovered       func(*types.TaskMemory) // 任务恢复回调（用于通知前端）

	// PM Todo List 队列（最大5个）
	todoList    []TodoItem
	todoMaxSize int
	todoMu      sync.RWMutex

	richBuilder *RichMessageBuilder
	msgBus      *MessageBus // [G63] MessageBus双向消息总线（前后一致性保障）

	// [v0.7.1] MCP Manager（SE 工具桥接用）
	mcpManager interface {
		CallTool(serverName, toolName string, args map[string]interface{}) (*mcp.CallToolResult, error)
	}

	// [v0.7.2] Context Management Bridge（上下文管理桥接）
	contextWindow   *memory.ContextWindow // Token 监控 + 窗口管理（TokenMonitor 面板数据源）
	contextBuilder *memory.ContextBuilder // 任务上下文组装器（注入 PM/SE system prompt）
	compressor     *memory.Compressor    // 对话压缩器（自动裁剪旧对话）
}

type TodoItem struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Status      string `json:"status"` // pending/doing/done
	CreatedAt   int64  `json:"created_at"`
	UpdatedAt   int64  `json:"updated_at"`
}

type BackendStatus struct {
	Stage        string `json:"stage"`
	PMStatus     string `json:"pm_status"`
	SEStatus     string `json:"se_status"`
	ProjectState int    `json:"project_state"`
	CurrentRole  string `json:"current_role"`
	CurrentSETask string `json:"current_se_task"` // [FIX-20260529] SE当前任务描述
	LastEvent    string `json:"last_event"`
	MessageCount int    `json:"message_count"`
	UpdatedAt    int64  `json:"updated_at"`
}

type RoleMode string

const (
	RoleModeIdle      RoleMode = ""           // 空闲
	RoleModeAPApprove RoleMode = "ap_approve" // AP审批中
)

type HandoverStep string

const (
	HandoverSEToPM   HandoverStep = "se_to_pm"   // SE→PM交接
	HandoverPMToAP   HandoverStep = "pm_to_ap"   // PM→AP交接
	HandoverAPToDone HandoverStep = "ap_to_done" // AP→最终状态(只记录不强制)
)

type HandoverState struct {
	Step       HandoverStep `json:"step"`        // 当前待交接步骤
	Pending    bool         `json:"pending"`     // 是否有待办
	Since      int64        `json:"since"`       // 待办开始时间戳
	NudgeCount int          `json:"nudge_count"` // 已催促次数
	Forced     bool         `json:"forced"`      // 是否已被强制执行
}

// NewManager 创建对话管理器
func NewManager(config types.Config, workDir string, configDir string) (*Manager, error) {
	// 创建AI客户端 — 支持每角色独立模型配置
	aiClient := ai.NewClient(config.APIConfig)
	var pmClient, seClient, apClient *ai.Client
	if config.PMConfig.BaseURL != "" && config.PMConfig.APIKey != "" {
		pmClient = ai.NewClient(config.PMConfig)
		fmt.Printf("[Manager] PM使用独立模型: %s\n", config.PMConfig.Model)
	}
	if config.SEConfig.BaseURL != "" && config.SEConfig.APIKey != "" {
		seClient = ai.NewClient(config.SEConfig)
		fmt.Printf("[Manager] SE使用独立模型: %s\n", config.SEConfig.Model)
	}
	if config.APConfig.BaseURL != "" && config.APConfig.APIKey != "" {
		apClient = ai.NewClient(config.APConfig)
		fmt.Printf("[Manager] AP使用独立模型: %s\n", config.APConfig.Model)
	}

	// 每个Processor用各自的Client（nil回退到aiClient）
	pmActual := aiClient
	if pmClient != nil {
		pmActual = pmClient
	}
	seActual := aiClient
	if seClient != nil {
		seActual = seClient
	}
	apActual := aiClient
	if apClient != nil {
		apActual = apClient
	}

	// 初始化看板
	boardManager := board.NewManager(".argus/board.json")
	if err := boardManager.Load(); err != nil {
		return nil, fmt.Errorf("load board failed: %v", err)
	}

	// 初始化执行器
	pmExecutor := executor.NewExecutor(workDir, boardManager)
	seExecutor := executor.NewExecutor(workDir, boardManager)

	// 初始化文件变更追踪器
	fileTracker := executor.NewFileChangeTracker(workDir, 20)

	// [P0-1] 初始化 LSP 客户端（gopls daemon，可选）
	var lspClient *ai.LSPClient
	if lc, err := ai.NewLSPClient(workDir); err != nil {
		fmt.Printf("[Manager] ⚠️ LSP 不可用: %v（gopls 未安装或启动失败，非致命错误）\n", err)
		lspClient = nil
	} else {
		lspClient = lc
	}

	// 初始化AI处理器
	pmProcessor := ai.NewPMProcessor(pmActual, workDir, nil)
	seProcessor := ai.NewSEProcessor(seActual, workDir)
	apProcessor := ai.NewAPProcessor(apActual, workDir)

	manager := &Manager{
		router:      NewRouter(),
		aiClient:    aiClient,
		pmClient:    pmClient,
		seClient:    seClient,
		apClient:    apClient,
		pmProcessor: pmProcessor,
		seProcessor:   seProcessor,
		apProcessor:   apProcessor,
		pmExecutor:    pmExecutor,
		seExecutor:    seExecutor,
		fileTracker:   fileTracker,
		lspClient:     lspClient,
		boardManager:  boardManager,
		memoryManager: NewMemoryManager(workDir),
		sseBridge:     NewSSEBridge(),
		backendStatus: &BackendStatus{Stage: "idle", UpdatedAt: time.Now().Unix()},
		history:       []Message{},
		lastMsgIDs:    make(map[string]string),
		currentRole:   "user",
		workDir:       workDir,
		configDir:     configDir,
		config:        config,
		taskManager:   task.NewTaskManager(nil),
		ReplyLanguage: "zh",
		stopCh:        make(chan struct{}),
		todoMaxSize:   5,
		todoList:      []TodoItem{},
	}

	// 同步 ReplyLanguage 到 PM/SE 处理器
	pmProcessor.ReplyLanguage = manager.ReplyLanguage
	seProcessor.ReplyLanguage = manager.ReplyLanguage

	// 设置PM的TODO回调
	pmProcessor.SetTodoCallbacks(
		func(desc string) string { return manager.AddTodo(desc) },
		func(id, status string) { manager.UpdateTodoStatus(id, status) },
		func() { manager.ClearTodoList() },
	)

	// 初始化三层模型 Builder（用于 PM/SE 可视化）
	manager.richBuilder = NewRichMessageBuilder(manager.emitWailsEvent)
	pmProcessor.SetShellEmitter(manager.richBuilder)

	// [G63] 初始化MessageBus双向消息总线（前后一致性保障）
	// 注意：ctx在SetContext时设置，这里先创建，ctx后续注入
	manager.msgBus = NewMessageBus(nil)

	// 初始化配置管理器（决策 + 权限）
	configManager, err := NewConfigManager(workDir)
	if err != nil {
		fmt.Printf("[Manager] ⚠️ 配置管理器初始化失败: %v\n", err)
	} else {
		manager.configManager = configManager
		fmt.Printf("[Manager] ✅ 配置管理器已初始化\n")
	}

	envMemory, err := NewEnvMemory(workDir)
	if err != nil {
		fmt.Printf("[Manager] ⚠️ 环境记忆初始化失败: %v\n", err)
	} else {
		manager.envMemory = envMemory
		fmt.Printf("[Manager] ✅ 环境记忆已初始化 (%d 个工具)\n", envMemory.ToolCount())
	}

	// 注意：C监控在SetOnMessageAdded之后初始化，确保消息能正确推送到前端
	// manager.initCMonitor() 移到外部调用
	// manager.recoverState() 也移到 InitCMonitor() 中调用

	// 启动后台对话日志监控循环（永不退出）
	go manager.startConversationMonitor()

	// [关键] 设置所有Client+SE的debugLog回调（首次初始化必须，否则G-DEBUG/SE-RAW日志丢失）
	manager.aiClient.SetDebugLog(manager.seDebugLog)
	if manager.pmClient != nil { manager.pmClient.SetDebugLog(manager.seDebugLog) }
	if manager.seClient != nil { manager.seClient.SetDebugLog(manager.seDebugLog) }
	if manager.apClient != nil { manager.apClient.SetDebugLog(manager.seDebugLog) }
	manager.seProcessor.SetDebugLog(manager.seDebugLog)
	manager.seDebugLog("[INIT] ALL debugLog OK, configDir=" + configDir)

	// 启动自动定期保存（15秒间隔，类似C监控的兜底机制）
	if manager.memoryManager != nil {
		go manager.memoryManager.StartAutoSave(func() (string, string, string, []types.Message) {
			manager.mu.RLock()
			defer manager.mu.RUnlock()

			messages := make([]Message, len(manager.history))
			copy(messages, manager.history)

			var typedMessages []types.Message
			for _, msg := range messages {
				typedMessages = append(typedMessages, types.Message{
					Role:      msg.Role,
					Content:   msg.Content,
					Timestamp: msg.Timestamp,
				})
			}

			return "", manager.getCurrentState(), manager.currentRole, typedMessages
		}, 15*time.Second)

		fmt.Println("[Manager] ✅ 自动定期保存已启动（15秒间隔）")
	}

	return manager, nil
}

// InitCMonitor 初始化C监控（在SetOnMessageAdded之后调用）
func (m *Manager) InitCMonitor() {
	m.initCMonitor()
	// C监控初始化完成后，再恢复状态
	m.recoverState()
}

// UpdateAPIConfig 更新API配置并重建AI客户端（支持每角色独立模型）
func (m *Manager) UpdateAPIConfig(apiConfig types.APIConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.config.APIConfig = apiConfig
	m.aiClient = ai.NewClient(apiConfig)
	m.aiClient.SetDebugLog(m.seDebugLog) // Client层G-DEBUG日志写入conversation.log

	// 重建各角色独立客户端（如果配置了）
	m.rebuildRoleClients()
	// 重建后必须重新设置debugLog（新Client对象）
	if m.pmClient != nil { m.pmClient.SetDebugLog(m.seDebugLog) }
	if m.seClient != nil { m.seClient.SetDebugLog(m.seDebugLog) }
	if m.apClient != nil { m.apClient.SetDebugLog(m.seDebugLog) }

	// PM
	m.pmProcessor = ai.NewPMProcessor(m.getPMClient(), m.workDir, func(state int) {
		fmt.Printf("[PM] 通过Function Call更新项目状态: %d\n", state)
		if m.cMonitor != nil {
			m.cMonitor.UpdateProjectState(state)
		} else {
			fmt.Printf("[PM] ⚠️ cMonitor 未初始化，跳过状态更新\n")
		}
	})
	// SE
	m.seProcessor = ai.NewSEProcessor(m.getSEClient(), m.workDir)
	m.seProcessor.SetDebugLog(m.seDebugLog) // 关键日志写入conversation.log
	m.seDebugLog("[INIT] SE debugLog connected, configDir=" + m.configDir) // 验证回调是否生效
	// AP
	m.apProcessor = ai.NewAPProcessor(m.getAPClient(), m.workDir)

	m.pmProcessor.ReplyLanguage = m.ReplyLanguage
	m.seProcessor.ReplyLanguage = m.ReplyLanguage

	// 恢复TODO回调
	m.pmProcessor.SetTodoCallbacks(
		func(desc string) string { return m.AddTodo(desc) },
		func(id, status string) { m.UpdateTodoStatus(id, status) },
		func() { m.ClearTodoList() },
	)

	fmt.Printf("[Manager] API配置已更新: BaseURL=%s, Model=%s\n", apiConfig.BaseURL, apiConfig.Model)
}

// getPMClient 返回PM专用客户端（nil时回退到默认aiClient）
func (m *Manager) getPMClient() *ai.Client {
	if m.pmClient != nil {
		return m.pmClient
	}
	return m.aiClient
}

// getSEClient 返回SE专用客户端（nil时回退到默认aiClient）
func (m *Manager) getSEClient() *ai.Client {
	if m.seClient != nil {
		return m.seClient
	}
	return m.aiClient
}

// getAPClient 返回AP专用客户端（nil时回退到默认aiClient）
func (m *Manager) getAPClient() *ai.Client {
	if m.apClient != nil {
		return m.apClient
	}
	return m.aiClient
}

// rebuildRoleClients 从config重建各角色独立客户端
func (m *Manager) rebuildRoleClients() {
	if m.config.PMConfig.BaseURL != "" && m.config.PMConfig.APIKey != "" {
		m.pmClient = ai.NewClient(m.config.PMConfig)
		fmt.Printf("[Manager] PM使用独立模型: %s\n", m.config.PMConfig.Model)
	} else {
		m.pmClient = nil
	}
	if m.config.SEConfig.BaseURL != "" && m.config.SEConfig.APIKey != "" {
		m.seClient = ai.NewClient(m.config.SEConfig)
		fmt.Printf("[Manager] SE使用独立模型: %s\n", m.config.SEConfig.Model)
	} else {
		m.seClient = nil
	}
	if m.config.APConfig.BaseURL != "" && m.config.APConfig.APIKey != "" {
		m.apClient = ai.NewClient(m.config.APConfig)
		fmt.Printf("[Manager] AP使用独立模型: %s\n", m.config.APConfig.Model)
	} else {
		m.apClient = nil
	}
}

// UpdatePMConfig 动态更新PM的独立模型配置
func (m *Manager) UpdatePMConfig(pmConfig types.APIConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.config.PMConfig = pmConfig
	m.rebuildRoleClients()
	m.pmProcessor = ai.NewPMProcessor(m.getPMClient(), m.workDir, func(state int) {
		fmt.Printf("[PM] 通过Function Call更新项目状态: %d\n", state)
		if m.cMonitor != nil {
			m.cMonitor.UpdateProjectState(state)
		}
	})
	m.pmProcessor.ReplyLanguage = m.ReplyLanguage
	m.pmProcessor.SetTodoCallbacks(
		func(desc string) string { return m.AddTodo(desc) },
		func(id, status string) { m.UpdateTodoStatus(id, status) },
		func() { m.ClearTodoList() },
	)
	fmt.Printf("[Manager] PM配置已更新: Model=%s\n", pmConfig.Model)
}

// UpdateSEConfig 动态更新SE的独立模型配置
func (m *Manager) UpdateSEConfig(seConfig types.APIConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.config.SEConfig = seConfig
	m.rebuildRoleClients()
	m.seProcessor = ai.NewSEProcessor(m.getSEClient(), m.workDir)
	m.seProcessor.SetDebugLog(m.seDebugLog)
	m.seProcessor.ReplyLanguage = m.ReplyLanguage
	fmt.Printf("[Manager] SE配置已更新: Model=%s\n", seConfig.Model)
}

// UpdateAPConfig 更新AP的独立API配置（如果为空则复用PM的）
func (m *Manager) UpdateAPConfig(apConfig types.APIConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if apConfig.BaseURL == "" || apConfig.APIKey == "" {
		fmt.Println("[Manager] AP未配置独立API，复用PM的AI客户端")
		m.apClient = nil
		m.apProcessor = ai.NewAPProcessor(m.getAPClient(), m.workDir)
	} else {
		m.config.APConfig = apConfig
		m.apClient = ai.NewClient(apConfig)
		m.apProcessor = ai.NewAPProcessor(m.apClient, m.workDir)
		fmt.Printf("[Manager] AP使用独立API: BaseURL=%s, Model=%s\n", apConfig.BaseURL, apConfig.Model)
	}
}

// StopGoroutines 停止所有后台 goroutine
func (m *Manager) StopGoroutines() {
	select {
	case <-m.stopCh:
		return // 已关闭
	default:
		close(m.stopCh)
	}
	if m.memoryManager != nil {
		select {
		case <-m.memoryManager.stopCh:
			return
		default:
			close(m.memoryManager.stopCh)
		}
	}
}

// startConversationMonitor 后台监控对话日志，永不退出
func (m *Manager) startConversationMonitor() {
	fmt.Println("[Monitor] 启动对话日志监控循环")

	lastReadPos := int64(0)
	logDir := filepath.Join(m.configDir, "..", "logs")
	logPath := filepath.Join(logDir, "conversation.log")
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopCh:
			fmt.Println("[Monitor] 对话日志监控已停止")
			return
		case <-ticker.C:
			newContent, newPos, err := m.readNewContent(logPath, lastReadPos)
			if err != nil {
				fmt.Printf("[Monitor] 读取日志失败: %v\n", err)
				continue
			}
			if newContent == "" {
				continue
			}
			fmt.Printf("[Monitor] 检测到新对话内容 (%d 字节):\n%s\n", len(newContent), newContent)
			lastReadPos = newPos
			m.analyzeAndExecute(newContent)
		}
	}
}

// readNewContent 从上次位置读取新内容
func (m *Manager) readNewContent(logPath string, lastPos int64) (string, int64, error) {
	f, err := os.Open(logPath)
	if err != nil {
		return "", lastPos, nil // 文件不存在，不算错误
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return "", lastPos, err
	}

	if stat.Size() <= lastPos {
		return "", lastPos, nil // 没有新内容
	}

	// 从上次位置读取
	f.Seek(lastPos, 0)
	data, err := io.ReadAll(f)
	if err != nil {
		return "", lastPos, err
	}

	return string(data), stat.Size(), nil
}

// analyzeAndExecute 分析对话内容并执行任务
func (m *Manager) analyzeAndExecute(content string) {
	// 1. 检测用户反馈"没有输出" → 检查SE执行结果并可能重试
	if strings.Contains(content, "没有输出") {
		fmt.Println("[Monitor] 检测到用户反馈'没有输出'")
		m.handleNoOutputFeedback()
	}

	// 2. 检测SE执行失败/超时
	if strings.Contains(content, "timeout") || strings.Contains(content, "超时") {
		fmt.Println("[Monitor] 检测到SE执行超时")
		m.handleSETTimeout()
	}

	// 注意：Monitor 只做监控（催促、检测异常），不处理消息！
	// 消息处理由 processPMRequest / SendMessage 统一入口处理
}

// handleNoOutputFeedback 处理"没有输出"反馈
func (m *Manager) handleNoOutputFeedback() {
	// 检查最近SE执行的文件是否存在
	projectDir := m.workDir

	// 检查hello.go是否存在
	helloPath := filepath.Join(projectDir, "hello.go")
	if _, err := os.Stat(helloPath); err == nil {
		fmt.Println("[Monitor] hello.go 存在，尝试直接运行")
		// 文件存在但没输出，可能是执行问题
		// 这里可以触发重试
	} else {
		fmt.Println("[Monitor] hello.go 不存在，可能需要重新创建")
	}
}

// handleSETTimeout 处理SE执行超时
func (m *Manager) handleSETTimeout() {
	fmt.Println("[Monitor] SE执行超时，可能需要调整执行策略")
	// 可以增加超时时间或改用其他执行方式
}

// clearSessionState 复位所有会话状态（统一清理入口）
func (m *Manager) clearSessionState() {
	m.mu.Lock()
	m.history = nil
	m.mu.Unlock()

	m.cMonitor.UpdateProjectState(types.ProjectStateIdle)
	m.cMonitor.UpdatePmStatus(types.RoleStatusIdle)
	m.syncBackendStatus("idle", "PM处理完成，等待用户输入")
	m.cMonitor.UpdateSeStatus(types.RoleStatusIdle)
	m.cMonitor.ClearLastUserMessage()
	if m.memoryManager != nil {
		m.memoryManager.ClearState()
	}

	// ⚠️ G点36修复：AP approved 时通知前端清空 messages
	// 防止下次启动时显示已完成的旧任务
	m.msgBusSend("system", "", "project_approved", PathSystem, "approveProject:project_approved", map[string]interface{}{
		"timestamp": time.Now().Unix(),
		"action":    "clear_messages",
	})
	fmt.Println("[TRACE-AP] ✅ 发送 project_approved 事件（清空messages）")
	m.boardManager.Reset()
	m.currentRole = ""
	m.pmWaitingForUserSince = 0
	m.pmWaitingNudgeCount = 0
	m.reviewCount = 0
	m.seContinueCount = 0
	m.seAskPMCount = 0
	m.seReportedComplete = false
	m.pmConsecutiveFailures = 0
	m.pmLastFailureTime = time.Time{}
	m.pmUnhealthySince = time.Time{}
	m.pmSeLoopCount = 0
	m.lastMessageFrom = ""
	m.lastMessageContent = ""
	fmt.Println("[Recover] ✅ 会话状态已完全清理")
}

// recoverState 启动时恢复/复位任务状态
// 每次启动都清理旧状态，不自动恢复（防止旧消息污染新会话）
func (m *Manager) recoverState() {
	state, err := m.cMonitor.ReadState()
	if err != nil {
		fmt.Printf("[Recover] 读取状态失败: %v\n", err)
		return
	}

	fmt.Printf("[Recover] 启动时状态: project_state=%d, pm=%s, se=%s\n",
		state.ProjectState, state.PmStatus, state.SeStatus)

	if state.ProjectState == types.ProjectStateRunning {
		fmt.Println("[Recover] 上次任务未完成，清理旧状态，等待用户手动操作")
	}
	m.clearSessionState()
}

// initCMonitor 初始化C监控
func (m *Manager) initCMonitor() {
	// 设置PM的状态更新回调（通过Function Call）
	m.pmProcessor.SetStateUpdater(func(state int) {
		fmt.Printf("[PM] 通过Function Call更新项目状态: %d\n", state)
		m.cMonitor.UpdateProjectState(state)

		// ✅ 同步更新角色状态
		if state == 2 || state == 0 {
			m.cMonitor.UpdatePmStatus(types.RoleStatusIdle)
			m.cMonitor.UpdateSeStatus(types.RoleStatusIdle)
			fmt.Printf("[PM] 状态%d，重置PM/SE为idle\n", state)
		}

		// ✅ 项目完成时清除记忆，防止C监控重复触发恢复
		if state == 2 && m.memoryManager != nil {
			if err := m.memoryManager.ClearState(); err != nil {
				fmt.Printf("[PM] ⚠️ 清除任务记忆失败: %v\n", err)
			} else {
				fmt.Println("[PM] ✅ 任务记忆已清除")
			}
		}

		if m.onProjectStateChanged != nil {
			stateStr := "idle"
			switch state {
			case 0:
				stateStr = "idle"
			case 1:
				stateStr = "running"
			case 2:
				stateStr = "done"
			case 4:
				stateStr = "error"
			}
			m.onProjectStateChanged(stateStr)
		}
	})

	// PM重启函数
	pmRestarter := func() error {
		fmt.Println("[PM] 正在重启...")
		// 重建AI客户端（支持每角色独立模型）
		m.aiClient = ai.NewClient(m.config.APIConfig)
		m.aiClient.SetDebugLog(m.seDebugLog)
		m.rebuildRoleClients()
		// 重新初始化PM处理器
		m.pmProcessor = ai.NewPMProcessor(m.getPMClient(), m.workDir, func(state int) {
			fmt.Printf("[PM] 通过Function Call更新项目状态: %d\n", state)
			m.cMonitor.UpdateProjectState(state)
		})
		// 恢复TODO回调
		m.pmProcessor.SetTodoCallbacks(
			func(desc string) string { return m.AddTodo(desc) },
			func(id, status string) { m.UpdateTodoStatus(id, status) },
			func() { m.ClearTodoList() },
		)
		// 重置状态
		m.cMonitor.UpdatePmStatus(types.RoleStatusIdle)
		return nil
	}

	// 消息发送函数（发给PM，通过钉钉）
	messageSender := func(msg string) {
		// 打印日志
		fmt.Printf("[Argus-MC→PM] %s\n", msg)
		// 添加到Argus消息历史（MC发给PM的消息）
		m.addHistory(Message{
			From:    "mc",
			To:      "pm",
			Role:    "mc",
			Content: msg,
			Source:  "c_monitor",
		})
		// 触发PM处理MC消息（同步调用，防止并发）
		if err := m.handleToPM(msg); err != nil {
			fmt.Printf("[Argus-MC] PM处理失败: %v\n", err)
		}
		// 发送钉钉消息（过滤重复@）
		filteredMsg := filterDuplicateMentions(msg)
		m.sendToDingTalk(fmt.Sprintf("[Argus-MC] %s", filteredMsg))
	}

	// 弹框提醒函数（使用Wails系统弹框）
	alertFunc := func(msg string) {
		fmt.Printf("[C监控] %s\n", msg)
		// 只有启用了 PmDecisionAlert 才弹框
		if m.ctx != nil && m.config.PmDecisionAlert {
			runtime.MessageDialog(m.ctx, runtime.MessageDialogOptions{
				Type:    runtime.WarningDialog,
				Title:   "Argus 监控提醒",
				Message: msg,
			})
		}
	}

	m.cMonitor = monitor.NewCMonitor(m.workDir, pmRestarter, messageSender, alertFunc)

	m.cMonitor.SetOnStateChange(func(state int) {
		if m.onProjectStateChanged != nil {
			stateStr := "idle"
			switch state {
			case 0:
				stateStr = "idle"
			case 1:
				stateStr = "running"
			case 2:
				stateStr = "done"
			case 4:
				stateStr = "error"
			}
			m.onProjectStateChanged(stateStr)
		}
	})

	m.cMonitor.SetPMWaitingCallbacks(
		m.GetPMWaitingForUser,
		m.IncrementPMWaitingNudge,
	)

	m.cMonitor.SetNotifyPM(func(msg string) {
		m.addHistory(Message{
			From:      "sys_c",
			Content:   msg,
			Timestamp: time.Now(),
			Source:    "c_monitor_notify",
		})
	})

	m.cMonitor.SetRetryCallback(func() error {
		fmt.Println("[C] 自动重试：取消API调用、清除记忆、复位看板、重置SE状态")

		m.mu.Lock()
		if m.cancelFunc != nil {
			fmt.Println("[C] 🛑 取消正在进行的AI调用（防止幽灵返回）")
			m.cancelFunc()
			m.cancelFunc = nil
		}
		m.mu.Unlock()

		m.processingMu.Lock()
		if m.isProcessing {
			fmt.Println("[C] 🛑 重置处理状态")
			m.isProcessing = false
		}
		m.processingMu.Unlock()

		m.pendingMu.Lock()
		if len(m.pendingQueue) > 0 {
			fmt.Printf("[C] 🧹 清空待处理消息队列 (%d条)\n", len(m.pendingQueue))
			m.pendingQueue = nil
		}
		m.pendingMu.Unlock()

		m.resetMu.Lock()
		m.resetGeneration++
		m.resetMu.Unlock()

		if m.memoryManager != nil {
			m.memoryManager.ClearState()
		}
		m.boardManager.Reset()
		m.reviewCount = 0
		m.seContinueCount = 0
		m.seAskPMCount = 0
		m.seEmptyActionCount = 0
		m.seReportedComplete = false
		m.currentRole = ""
		m.cMonitor.UpdateSeStatus(types.RoleStatusIdle)
		m.cMonitor.UpdatePmStatus(types.RoleStatusIdle)
		fmt.Println("[C] ✅ SE/PM状态已强制重置为IDLE（含API取消+generation递增）")
		return nil
	})

	m.cMonitor.SetHandoverCallbacks(
		func() interface{} { return m.GetHandoverState() },
		func() { m.ClearHandover() },
		func() int { return m.IncrementHandoverNudge() },
		func() { m.MarkHandoverForced() },
		func(role string) string { return m.GetLastRoleMessage(role) },
		m.handleForceHandover,
	)

	// ✅ 设置状态查询依赖（C 的核心能力：实时监控所有模块）
	m.cMonitor.SetStatusProviders(
		m.GetChatManagerStatus,
		m.GetMemoryStatus,
	)
	fmt.Println("[Manager] ✅ C 监控状态查询能力已启用")

	// [FIX-20260528-D] 设置SE完成状态检测器
	m.cMonitor.SetSECompletedChecker(m.IsSETaskCompleted)
	m.cMonitor.SetWorkDirChecker(func() string { return m.workDir })
	fmt.Println("[Manager] ✅ C监控SE完成状态+工作目录检测已启用")

	// 启动C监控
	m.cMonitor.Start()
}

// SetTerminalWriter 设置终端写入器（用于PM/SE验证时显示执行过程）
func (m *Manager) SetTerminalWriter(writer func(string) error) {
	m.terminalWriter = writer
	if m.pmProcessor != nil {
		m.pmProcessor.SetTerminalWriter(writer)
	}
}

// SetContext 设置Wails context（供App调用）
func (m *Manager) SetContext(ctx context.Context) {
	m.ctx = ctx
	m.taskManager.SetEmitFn(m.emitWailsEvent)
	// [G63] 注入ctx到MessageBus
	if m.msgBus != nil {
		m.msgBus.ctx = ctx
	}
}

func (m *Manager) GetAllTasks() []*types.GlobalTask {
	if m.taskManager == nil {
		return nil
	}
	return m.taskManager.GetAllTasks()
}

// WriteDebugLog 写调试日志到 conversation.log
func (m *Manager) WriteDebugLog(content string) {
	m.writeConversationLog(Message{
		From:    "DEBUG",
		To:      "debug",
		Role:    "debug",
		Content: content,
		Raw:     content,
		Source:  "debug",
		Timestamp: time.Now(),
	})
}

// StopCMonitor 停止ChatManager中的C监控
func (m *Manager) StopCMonitor() {
	if m.cMonitor != nil {
		m.cMonitor.Stop()
		fmt.Println("[Manager] C监控已停止")
	}
}

// StartCMonitor 启动ChatManager中的C监控
func (m *Manager) StartCMonitor() {
	if m.cMonitor != nil {
		m.cMonitor.Start()
		fmt.Println("[Manager] C监控已启动")
	}
}

// IsCMonitorRunning 检查C监控是否在运行
func (m *Manager) IsCMonitorRunning() bool {
	if m.cMonitor != nil {
		return m.cMonitor.IsRunning()
	}
	return false
}

// IsAPReviewing 检查AP是否正在审核
func (m *Manager) IsAPReviewing() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.currentRole == "ap"
}

// SetTerminalOutput 设置终端输出回调（供App调用）
func (m *Manager) SetTerminalOutput(callback func(string)) {
	m.seExecutor.SetTerminalOutput(callback)
}

// SetReplyLanguage 设置AI回复语言（供前端调用）
func (m *Manager) SetReplyLanguage(lang string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.ReplyLanguage = lang
	if m.pmProcessor != nil {
		m.pmProcessor.ReplyLanguage = lang
	}
	if m.seProcessor != nil {
		m.seProcessor.ReplyLanguage = lang
	}
	fmt.Printf("[Manager] 回复语言已设置为: %s\n", lang)
}

func (m *Manager) SetOnFileWritten(callback func(path string)) {
	m.seExecutor.SetOnFileWritten(callback)
}

// HandleUserInput 处理用户输入（无返回值，用于旧接口）
func (m *Manager) HandleUserInput(input string) error {
	_, err := m.ProcessMessage(input)
	return err
}

// injectTimeContext 注入时间上下文到PM/SE的Prompt中
func (m *Manager) injectTimeContext() {
	if m.cMonitor == nil {
		return
	}

	lastTime, err := m.cMonitor.GetLastInteractionTime()
	if err != nil || lastTime == 0 {
		return
	}

	firstTime, _ := m.cMonitor.GetFirstInteractionTime() // 可能为 0（首次交互时）

	ctx := monitor.GenerateTimeContext(lastTime, firstTime)

	timeInfo := fmt.Sprintf(`## ⏰ 时间感知（重要！）
当前时间: %s
距上次交互: %s
今天是: %s %s
关系阶段: %s

### 社交指南（让对话更有温度）：
- 如果用户很久没来（>24小时），先寒暄再谈正事
- 工作间隙可以适当闲聊（但不要频繁，不要干扰工作）
- 节假日要主动问候
- 注意用户的工作强度（今天工作了%.1f小时），适当关心
- 保持自然，像真同事/老朋友一样
- 关系越深（%s），表达可以越真诚`,
		ctx.CurrentTime,
		ctx.TimeSinceLast,
		ctx.DayOfWeek,
		ctx.SpecialDay,
		ctx.RelationshipPhase,
		ctx.TodayWorkHours,
		ctx.RelationshipPhase,
	)

	if m.pmProcessor != nil {
		m.pmProcessor.SetTimeContext(timeInfo)
	}
}

func (m *Manager) injectEnvMemory() {
	if m.envMemory == nil {
		return
	}
	summary := m.envMemory.Summary()
	if summary == "" {
		return
	}
	if m.seProcessor != nil {
		m.seProcessor.SetEnvMemory(summary)
		fmt.Printf("[EnvMemory] 🧠 已注入SE环境记忆 (%d 个工具)\n", m.envMemory.ToolCount())
	}
}

// ProcessMessage 处理用户输入并返回响应
func (m *Manager) ProcessMessage(input string) (string, error) {
	fmt.Println("[ProcessMessage] ==================== 开始处理消息 ====================")
	fmt.Printf("[ProcessMessage] 收到消息: %s\n", input)
	fmt.Printf("[ProcessMessage] pmProcessor: %v, router: %v\n", m.pmProcessor != nil, m.router != nil)

	m.clearStreamMessageIDs() // [G50] 每个新任务清理旧messageId，防止复用

	trimmedInput := strings.TrimSpace(input)

	if trimmedInput == "" || strings.ToLower(trimmedInput) == "stop" || strings.Contains(trimmedInput, "停止") {
		fmt.Println("[ProcessMessage] ⛔ 检测到停止命令，设置 userStopped 标志")
		m.SetUserStopped(true)
		m.StopCurrentTask()
		return "✅ 已停止任务执行", nil
	}

	// 🔴 先解析并添加用户消息到历史（即使后续排队，用户也能看到自己的消息）
	msg := m.router.Parse("user", input)
	msg.Role = "user"
	msg.Source = "user_input"

	fmt.Printf("[ProcessMessage] 🔍 解析结果: To='%s' Content='%s' | RawInput='%s'\n", msg.To, msg.Content, input)

	// 🔴 [FIX] 恢复模式：跳过重复addHistory（消息已在历史中）
	if !m.isRecovering {
		fmt.Printf("[ProcessMessage] ✅ 调用 addHistory (user_msg) | history_len=%d\n", len(m.history))
		m.addHistory(msg)
		fmt.Printf("[ProcessMessage] ✅ addHistory 完成 | history_len=%d\n", len(m.history))
	} else {
		fmt.Println("[ProcessMessage] ⏭️ 恢复模式，跳过addHistory")
	}

	m.processingMu.Lock()
	if m.isProcessing {
		if time.Since(m.processingStartTime) > 60*time.Second {
			m.mu.Lock()
			wasInSE := m.currentRole == "se"
			fmt.Printf("[ProcessMessage] ⚠️ isProcessing 超时(%.0f秒)，强制清理旧任务! currentRole=%s wasInSE=%v\n", time.Since(m.processingStartTime).Seconds(), m.currentRole, wasInSE)
			if m.cancelFunc != nil {
				m.cancelFunc()
				m.cancelFunc = nil
			}
			if !wasInSE {
				m.currentRole = ""
			}
			m.isRecovering = false
			m.mu.Unlock()
			if m.cMonitor != nil {
				m.cMonitor.UpdatePmStatus(types.RoleStatusIdle)
				if !wasInSE {
					m.cMonitor.UpdateSeStatus(types.RoleStatusIdle)
				}
			}
			if wasInSE {
				m.isProcessing = false
				m.processingMu.Unlock()
				m.pendingMu.Lock()
				m.pendingQueue = append(m.pendingQueue, input)
				queueLen := len(m.pendingQueue)
				m.pendingMu.Unlock()
				fmt.Printf("[ProcessMessage] 📥 SE正在执行中，新消息排队 (队列长度=%d)\n", queueLen)
				return "", nil
			}
			m.isProcessing = false
		} else {
			m.processingMu.Unlock()
			m.pendingMu.Lock()
			m.pendingQueue = append(m.pendingQueue, input)
			queueLen := len(m.pendingQueue)
			m.pendingMu.Unlock()
			fmt.Printf("[ProcessMessage] 📥 消息暂存到队列 (队列长度=%d): %s\n", queueLen, input)
			// 添加排队提示到聊天（让用户看到消息已暂存）
			queueNote := Message{
				From:      "system",
				To:        "user",
				Role:      "system",
				Content:   fmt.Sprintf("📥 消息已暂存 (队列中第%d条)，当前消息处理完成后自动发送", queueLen),
				Raw:       fmt.Sprintf("📥 消息已暂存 (队列中第%d条)，当前消息处理完成后自动发送", queueLen),
				Source:    "system",
				Timestamp: time.Now(),
			}
			m.addHistory(queueNote)
			return "", nil
		}
	}
	m.isProcessing = true
	m.processingStartTime = time.Now()
	m.processingMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	m.mu.Lock()
	m.cancelFunc = cancel
	m.mu.Unlock()

	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("[ProcessMessage] 💥 panic recovered: %v\n", r)
		}
		m.processingMu.Lock()
		m.isProcessing = false
		m.processingMu.Unlock()
		m.mu.Lock()
		// ⚠️ 不在这里 cancel()，让 context 保持活跃直到 StopCurrentTask 被调用
		// 这样 PM/SE/AP 的多轮 AI 调用不会被中断
		m.cancelFunc = nil
		m.isRecovering = false
		m.mu.Unlock()

		// 📤 处理完成后，检查队列中是否有待处理消息
		m.pendingMu.Lock()
		if len(m.pendingQueue) > 0 {
			nextInput := m.pendingQueue[0]
			m.pendingQueue = m.pendingQueue[1:]
			queueLen := len(m.pendingQueue)
			m.pendingMu.Unlock()
			fmt.Printf("[ProcessMessage] 📤 从队列取出下一条消息 (剩余%d条): %s\n", queueLen, nextInput)
			go func() {
				defer func() {
					if r := recover(); r != nil {
						fmt.Printf("[ProcessMessage-Queue] 💥 panic recovered: %v\n", r)
						m.processingMu.Lock()
						m.isProcessing = false
						m.processingMu.Unlock()
						if m.cMonitor != nil {
							m.cMonitor.UpdatePmStatus(types.RoleStatusIdle)
							m.cMonitor.UpdateSeStatus(types.RoleStatusIdle)
						}
					}
				}()
				m.ProcessMessage(nextInput)
			}()
		} else {
			m.pendingMu.Unlock()
		}
	}()

	if m.pmProcessor != nil {
		m.pmProcessor.SetContext(ctx)
	}
	if m.seProcessor != nil {
		m.seProcessor.SetContext(ctx)
	}
	if m.apProcessor != nil {
		m.apProcessor.SetContext(ctx)
	}

	// ⏰ 注入时间上下文（让AI感知时间+社交）
	m.injectTimeContext()
	m.injectEnvMemory()

	m.mu.Lock()
	m.reviewCount = 0
	m.pmReviewCycles = 0
	if m.pmWaitingForUserSince > 0 {
		fmt.Printf("[ProcessMessage] USR已回复, 重置等待状态 (等待了%ds)\n", time.Now().Unix()-m.pmWaitingForUserSince)
		m.pmWaitingForUserSince = 0
		m.pmWaitingNudgeCount = 0
	}
	m.mu.Unlock()

	// 💾 保存最后用户消息（用于智能恢复）
	if m.cMonitor != nil {
		m.cMonitor.SaveLastUserMessage(input)
	}

	// 🔄 新任务开始，重置C的自动重试标记
	if m.cMonitor != nil {
		m.cMonitor.ResetRetryFlag()
	}

	// ⏰ 保存最后交互时间（用于时间感知+社交）
	if m.cMonitor != nil {
		m.cMonitor.SaveLastInteractionTime(time.Now().Unix())
	}

	// 立即保存任务状态（更新TaskID，不等PM/SE处理完成）
	m.saveTaskMemoryImmediate(input)

	m.writeRouteLog(fmt.Sprintf("[ROUTE-DEBUG] To='%s' | Content='%s' | Raw='%s'", msg.To, msg.Content, input))

	// 🔄 用户发消息时重置轮转状态（用户触发不算角色连续发言）
	if msg.From == "user" {
		m.router.ForceClearLastSpoken()
		m.router.ForceClear()
		m.pmSeLoopCount = 0
		m.lastMessageFrom = ""
		m.lastMessageContent = ""
	}

	defer m.saveTaskMemory(input)

	switch msg.To {
	case "pm":
		fmt.Printf("[ROUTE] → PM | raw=%s\n", input)
		fmt.Println("[ProcessMessage] 消息发送给 PM")
		err := m.handleToPM(msg.Content)
		if err != nil {
			fmt.Printf("[ProcessMessage] handleToPM 失败: %v\n", err)
			return "", err
		}
		// 返回最后一条消息
		fmt.Println("[ProcessMessage] 获取历史消息")
		history := m.GetHistory()
		if len(history) > 0 {
			lastMsg := history[len(history)-1]
			fmt.Printf("[ProcessMessage] 返回消息: %s\n", lastMsg.Content)
			return lastMsg.Content, nil
		}
		return "", nil
	case "se":
		fmt.Printf("[ROUTE] → SE | raw=%s To=%s\n", input, msg.To)
		err := m.handleUserDirectToSE(msg.Content)
		if err != nil {
			return "", err
		}
		history := m.GetHistory()
		if len(history) > 0 {
			lastMsg := history[len(history)-1]
			return lastMsg.Content, nil
		}
		return "", nil
	case "ap":
		fmt.Printf("[ROUTE] → AP (直接对话) | raw=%s\n", input)
		return m.handleAPDirectChat(input)
	default:
		fmt.Printf("[ROUTE] → DEFAULT(PM) | raw=%s To=%s\n", input, msg.To)
		err := m.handleToPM(msg.Content)
		if err != nil {
			return "", err
		}
		// 返回最后一条消息
		history := m.GetHistory()
		if len(history) > 0 {
			lastMsg := history[len(history)-1]
			return lastMsg.Content, nil
		}
		return "", nil
	}
}

// ProcessMessageFrom 从指定角色发送消息（用于 SE/C 等内部组件）
// 所有角色直接用原始 role，由 PM 统一处理并决定回复给谁
func (m *Manager) ProcessMessageFrom(fromRole, input string) (string, error) {
	fmt.Printf("[ProcessMessageFrom] %s → PM: %s [FROM=%s]\n", fromRole, input[:min(100, len(input))], fromRole)
	m.syncBackendStatus("routing", fromRole+"→PM: "+input[:min(40, len(input))])

	m.lastMessageFrom = fromRole
	m.lastMessageContent = input

	msg := m.router.Parse(fromRole, input)
	msg.Role = fromRole
	msg.Source = fromRole + "_to_pm"

	fmt.Printf("[ProcessMessageFrom] Router解析: From=%q To=%q Content_head=%q\n",
		msg.From, msg.To, msg.Content[:min(80, len(msg.Content))])

	m.addHistory(msg)

	// 实时保存：每条内部消息处理后都保存（防止崩溃丢失）
	defer m.saveTaskMemory(fromRole + ": " + input)

	// 🔑 内部路由（如 SE→PM）时，父函数的 MarkProcessingStart 尚未释放
	// 临时释放 Router processing 锁，让 handleToPM 的 CheckTurn 能正常放行 PM
	restoreRouteLock := m.router.TempReleaseForHandover(fromRole)
	err := m.handleToPM(msg.Content)
	restoreRouteLock()
	if err != nil {
		return "", err
	}

	history := m.GetHistory()
	if len(history) > 0 {
		return history[len(history)-1].Content, nil
	}
	return "", nil
}

// isPMUnhealthy 检测PM是否处于不健康状态（连续API失败/僵尸）
// 条件：连续失败>=3次 且 最后一次失败在60秒内
func (m *Manager) isPMUnhealthy() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.pmConsecutiveFailures >= 3 && !m.pmLastFailureTime.IsZero() {
		if time.Since(m.pmLastFailureTime) < 60*time.Second {
			return true
		}
	}
	return false
}

// resetPMHealth 重置PM健康状态（PM成功调用后调用）
func (m *Manager) resetPMHealth() {
	m.mu.Lock()
	m.pmConsecutiveFailures = 0
	m.pmLastFailureTime = time.Time{}
	m.pmUnhealthySince = time.Time{}
	m.mu.Unlock()
}

// recordPMFailure 记录PM API失败
func (m *Manager) recordPMFailure() {
	m.mu.Lock()
	m.pmConsecutiveFailures++
	now := time.Now()
	if m.pmConsecutiveFailures == 1 {
		m.pmUnhealthySince = now
	}
	m.pmLastFailureTime = now
	failCount := m.pmConsecutiveFailures
	unhealthyDur := ""
	if !m.pmUnhealthySince.IsZero() {
		unhealthyDur = fmt.Sprintf(", 不健康持续%.0fs", now.Sub(m.pmUnhealthySince).Seconds())
	}
	m.mu.Unlock()
	fmt.Printf("[PM Health] ❌ 连续失败 %d 次%s\n", failCount, unhealthyDur)
}

// isSEZombie 检测SE是否处于僵尸状态（busy超时未恢复）
// 条件：SE状态为busy 且 最后状态变更超过5分钟
func (m *Manager) isSEZombie() bool {
	state, err := m.cMonitor.ReadState()
	if err != nil {
		return false
	}
	if state.SeStatus == types.RoleStatusBusy && state.LastChange > 0 {
		lastChange := time.Unix(state.LastChange, 0)
		if time.Since(lastChange) > 5*time.Minute {
			return true
		}
	}
	return false
}

var seResultKeywords = []string{
	`{"actions"`, `"type":"write_file"`, `"type":"exec"`, `"type":"read_file"`,
	`"type":"write_file"`, `"type":"exec"`, `"type":"read_file"`,
	"task_status", "verified", "technical_notes",
}

func (m *Manager) isSEResult(content string) bool {
	lower := strings.ToLower(content)
	for _, kw := range seResultKeywords {
		if strings.Contains(lower, strings.ToLower(kw)) {
			return true
		}
	}
	return false
}

func (m *Manager) checkLoopBlock(fromRole, content string) bool {
	parsed := m.router.Parse(fromRole, content)
	if fromRole == "pm" && parsed.To == "se" && m.lastMessageFrom == "se" {
		m.pmSeLoopCount++
		fmt.Printf("[🛡️ 防循环] 检测到 PM→SE 循环! 上一条来自SE, 当前第%d次 (上限3)\n", m.pmSeLoopCount)
		m.syncBackendStatus("loop_detected", fmt.Sprintf("PM→SE循环第%d次", m.pmSeLoopCount))
		if m.pmSeLoopCount >= 3 {
			fmt.Println("[🛡️ 防循环] 🔴 达到循环上限(3次)，强制终止！")
			m.syncBackendStatus("loop_blocked", "强制终止死循环")
			m.addPMToUserMsg("⚠️ 系统检测到任务执行出现循环，已自动终止。可能需要人工介入。")
			return true
		}
		if m.isSEResult(m.lastMessageContent) {
			fmt.Println("[🛡️ 防循环] ⚠️ 上一条是SE的actions结果，拦截本次PM→SE")
			m.syncBackendStatus("loop_blocked", "拦截: SE结果后禁止重复@SE")
			return true
		}
	}
	if fromRole == "pm" && parsed.To == "se" {
		m.pmSeLoopCount++
	} else if fromRole == "pm" && (parsed.To == "ap" || parsed.To == "usr") {
		m.pmSeLoopCount = 0
	}
	return false
}

// handleToPM 处理发给PM的消息
func (m *Manager) handleToPM(content string) (err error) {
	m.syncBackendStatus("pm_processing", "PM开始处理: "+content[:min(40, len(content))])
	if allowed, _ := m.router.CheckTurn("pm", "handleToPM"); !allowed {
		return nil
	}

	if m.isPMUnhealthy() {
		fmt.Println("[handleToPM] 🚨 PM处于不健康状态，自动清理旧状态后重新处理")
		m.clearSessionState()
		m.resetPMHealth()
	}

	if m.isSEZombie() {
		fmt.Println("[handleToPM] 🚨 SE处于僵尸状态(busy超时)，强制复位")
		m.cMonitor.UpdateSeStatus(types.RoleStatusIdle)
	}

	m.router.MarkProcessingStart("pm")
	defer func() {
		m.router.MarkProcessingEnd("pm")
		if r := recover(); r != nil {
			fmt.Printf("[handleToPM] panic recovered: %v\n", r)
			err = fmt.Errorf("panic: %v", r)
		}
	}()

	// 🔴 安全保证：确保 PM/SE/AP 处理器有有效 ctx（C监控回调等路径可能未设置）
	safeCtx := context.Background()
	m.mu.RLock()
	if m.ctx != nil {
		safeCtx = m.ctx
	}
	m.mu.RUnlock()
	// ⏰ PM超时120秒（ChatWithTools需要多轮工具调用，NVIDIA API响应较慢）
	pmCtx, pmCancel := context.WithTimeout(safeCtx, 120*time.Second)
	defer pmCancel()
	if m.pmProcessor != nil {
		m.pmProcessor.SetContext(pmCtx)
	}
	if m.seProcessor != nil {
		m.seProcessor.SetContext(pmCtx)
	}

	fmt.Printf("[handleToPM] 开始处理消息: %s (时间: %s)\n", content, time.Now().Format("15:04:05"))

	m.currentRole = "pm"

	// === 检测是否为 SE 审核交接场景 ===
	isReviewScenario := strings.Contains(content, "已完成") ||
		strings.Contains(content, "审核") ||
		strings.Contains(content, "SE已完成")
	fmt.Printf("[PROBE-handleToPM] 🔍 审核场景检测: isReview=%v seReportedComplete=%v content_preview=%q (时间:%s)\n",
		isReviewScenario, m.seReportedComplete, content[:min(60, len(content))], time.Now().Format("15:04:05.000"))

	if isReviewScenario && m.richBuilder != nil {
		fmt.Println("[handleToPM] 🔄 审核场景: 用ProcessReview(支持ChatWithTools工具调用验证)")
		m.richBuilder.StartTaskList("pm", "PM 代码审核", []types.TaskItemDef{
			{Text: "接收 SE 完成报告"},
			{Text: "审核 SE 执行结果"},
			{Text: "给出审核结论"},
		})
		pmTaskId := m.richBuilder.GetCurrentTaskID()
		m.richBuilder.UpdateTask(pmTaskId, 0, "running")
		defer func() {
			if pmTaskId != "" && m.richBuilder != nil {
				m.richBuilder.CompleteTaskList(pmTaskId, "completed", nil)
				m.richBuilder.Reset()
			}
		}()
	}

	if m.richBuilder != nil && !isReviewScenario {
		taskPreview := content
		if len(taskPreview) > 80 {
			taskPreview = taskPreview[:80] + "..."
		}
		isLikelyChat := len(content) < 20 || (!strings.Contains(content, "@SE") &&
			!strings.Contains(content, "创建") && !strings.Contains(content, "执行") &&
			!strings.Contains(content, "编写") && !strings.Contains(content, "实现") &&
			!strings.Contains(content, "修改") && !strings.Contains(content, "修复"))
		if !isLikelyChat {
			m.richBuilder.StartTaskList("pm", "PM 分配任务", []types.TaskItemDef{
				{Text: "分析用户需求"},
				{Text: "分配 SE 任务"},
				{Text: "审核 SE 结果"},
			})
			m.richBuilder.UpdateTask(m.richBuilder.GetCurrentTaskID(), 0, "running", taskPreview)
		} else {
			fmt.Printf("[handleToPM] 💬 检测到闲聊模式(短消息/无任务关键词)，跳过RichMessage\n")
		}
	}

	// 🔴 Wails事件: PM开始处理（前端用 runtime.EventsOn 监听）
	if m.ctx != nil {
		m.msgBusSend("pm", "", "pm_started", PathPMStream, "ProcessMessage:pm_started", map[string]string{})
	}

	// 更新PM状态为busy + 项目状态
	// 🔴 [FIX-20260530] 审核场景保持Done状态，防止PM再次@SE分配任务
	if m.seReportedComplete || isReviewScenario {
		fmt.Printf("[handleToPM] 🛡️ 审核模式: seReportedComplete=%v isReview=%v, 保持Done状态\n", m.seReportedComplete, isReviewScenario)
	} else {
		m.cMonitor.UpdateProjectState(types.ProjectStateRunning)
	}
	m.cMonitor.UpdatePmStatus(types.RoleStatusBusy)
	// 注意：API配置错误时不改变项目状态，只有真正的项目执行错误才改变

	// 转换历史为PM处理器需要的格式
	fmt.Printf("[handleToPM] 准备调用 GetHistory (时间: %s)\n", time.Now().Format("15:04:05"))
	history := m.GetHistory()
	fmt.Printf("[handleToPM] GetHistory 完成，历史消息数量: %d (时间: %s)\n", len(history), time.Now().Format("15:04:05"))
	pmHistory := make([]ai.ChatMessage, len(history))
	for i, msg := range history {
		pmHistory[i] = ai.ChatMessage{Role: msg.Role, Content: msg.Content}
	}

	fmt.Printf("[handleToPM] 调用 PMProcessor (isReview=%v)\n", isReviewScenario)

	var resp *ai.PMResponse
	if m.contextWindow != nil {
		m.contextWindow.AddMessage(memory.RoleUser, content, 0, "")
		m.pushTokenStats()
		m.WriteDebugLog("[ContextBridge] ✅ 用户消息已写入 ContextWindow + TokenStats 已推送")
	}

	aiGen := m.getResetGeneration()
	if isReviewScenario {
		fmt.Printf("[handleToPM] 🔄 审核场景用ProcessReview(支持工具调用验证)\n")
		resp, err = m.pmProcessor.ProcessReview(content, pmHistory, func(delta string) {
			m.emitStreamChunk("pm", delta)
		})
	} else {
		resp, err = m.pmProcessor.ProcessStream(content, pmHistory, func(delta string) {
			m.emitStreamChunk("pm", delta)
		})
	}
	if err != nil {
		fmt.Printf("[handleToPM] PMProcessor调用失败: %v (isReview=%v)\n", err, isReviewScenario)
	} else {
		fmt.Printf("[handleToPM] PMProcessor调用成功，响应长度: %d (isReview=%v)\n", len(resp.Content), isReviewScenario)
		hasAP := strings.Contains(strings.ToLower(resp.Content), "@ap")
		m.writeRouteLog(fmt.Sprintf("[TRACE-AP-PM-RESP] hasAP=%v content_preview=%q", hasAP, resp.Content[:min(120, len(resp.Content))]))
		fmt.Printf("[TRACE-AP-PM-RESP] hasAP=%v content_len=%d\n", hasAP, len(resp.Content))
	}
	if m.isGhostCall(aiGen) {
		fmt.Printf("[handleToPM] ⚠️ 检测到复位后的幽灵PM调用，丢弃结果\n")
		m.cMonitor.UpdatePmStatus(types.RoleStatusIdle)
		return nil
	}
	if err != nil {
		m.recordPMFailure()
		m.cMonitor.UpdatePmStatus(types.RoleStatusIdle)
		m.syncBackendStatus("error", "PM AI调用失败: "+err.Error())

		if m.ctx != nil {
			m.msgBusSend("system", err.Error(), "error", PathSystem, "ProcessMessage:pm_error", map[string]interface{}{
				"error": err.Error(),
				"stage": "pm",
			})
		}

		errContent := fmt.Sprintf("[错误] AI处理失败: %v", err)
		m.addPMToUserMsg(errContent)

		// 发送错误到钉钉（过滤重复@）
		go func() {
			filteredErr := filterDuplicateMentions(errContent)
			m.sendToDingTalk(fmt.Sprintf("[PM] %s", filteredErr))
		}()

		if m.richBuilder != nil {
			pmTaskId := m.richBuilder.GetCurrentTaskID()
			if pmTaskId != "" {
				m.richBuilder.CompleteTaskList(pmTaskId, "error", nil)
				m.richBuilder.Reset()
			}
		}

		return fmt.Errorf("PM process failed: %v", err)
	}

	m.resetPMHealth()

	// [v0.7.2] ContextWindow: 记录 PM 响应
	if m.contextWindow != nil && resp != nil {
		m.contextWindow.AddMessage(memory.RoleAssistant, resp.Content, 0, "")
		m.pushTokenStats()
		m.WriteDebugLog("[ContextBridge] ✅ PM 响应已写入 ContextWindow + TokenStats 已推送")
	}

	fmt.Printf("[DEBUG-FLOW] ProcessStream返回: content=%q len=%d\n", resp.Content[:min(80, len(resp.Content))], len(resp.Content))

	// 🔬 探针：记录 AI 原始输出（用于排查双@问题）
	if strings.Contains(resp.Content, "@") {
		fmt.Printf("\n🔬 [PROBE-PM-AI] ========== AI原始输出 ==========\n")
		fmt.Printf("🔬 [PROBE-PM-AI] 完整内容:\n%s\n", resp.Content)
		fmt.Printf("🔬 [PROBE-PM-AI] 前200字符: %q\n", resp.Content[:min(200, len(resp.Content))])
		fmt.Printf("🔬 [PROBE-PM-AI] 包含@数量: %d\n", strings.Count(resp.Content, "@"))
		fmt.Printf("🔬 [PROBE-PM-AI] =========================================\n\n")
	}

	// ✅ 接收者清除handover（下个人清除上个人的交接状态）
	// 只清除 SE→PM 交接，PM→AP 交接保留由 AP 清除
	m.mu.Lock()
	shouldClearHandover := m.handover.Pending && m.handover.Step == HandoverSEToPM
	if shouldClearHandover {
		m.handover = HandoverState{}
		fmt.Println("[Handover] ✅ PM已接手SE→PM交接")
	}
	m.mu.Unlock()

	cleanContent := strings.TrimSpace(resp.Content)
	if cleanContent == "" || cleanContent == "@USR" || cleanContent == "@USR " {
		fmt.Printf("[handleToPM] ⚠️ PM回复为空或只有@标记，跳过发送\n")
		m.cMonitor.UpdatePmStatus(types.RoleStatusIdle)
		return nil
	}
	if isStatusOnlyMessage(cleanContent) {
		fmt.Printf("[handleToPM] ⚠️ PM回复为纯状态确认消息(收到/待命/明白)，过滤跳过: %q\n", cleanContent)
		m.cMonitor.UpdatePmStatus(types.RoleStatusIdle)
		return nil
	}

	// 🔥🔥🔥 [方案1-核心修复] @SE 检测：最高优先级！必须在 @AP 和审批关键词之前！
	// 根因：之前 @SE 检测在第1587行（@AP和审批关键词之后），导致 PM 的任务描述被误判为审批结论
	// 证据：route.log 显示 [TRACE-AP-CHECK] 之后直接到 AP审批，缺失 [SE-TASK] 记录
	fmt.Printf("[PROBE-handleToPM] 📋 PM路由决策: content_preview=%q (时间:%s)\n",
		resp.Content[:min(80, len(resp.Content))], time.Now().Format("15:04:05.000"))
	parsedMsg := m.router.Parse("pm", resp.Content)
	m.writeRouteLog(fmt.Sprintf("[DEBUG-PARSE-1ST] to=%q content_head=%q", parsedMsg.To, resp.Content[:min(80, len(resp.Content))]))

	if parsedMsg.To == "se" {
		fmt.Printf("[SE-DEBUG] ✅ [方案1] @SE最高优先级命中: content=%q\n", resp.Content[:min(100, len(resp.Content))])
		if m.checkLoopBlock("pm", resp.Content) {
			fmt.Println("[🛡️ 防循环] [方案1] PM→SE 被拦截，不走 startSETask")
			m.cMonitor.UpdatePmStatus(types.RoleStatusIdle)
			return nil
		}

		finalTask := parsedMsg.Content
		for {
			idx := strings.LastIndex(finalTask, "\n{")
			if idx < 0 {
				break
			}
			remainder := finalTask[idx+1:]
			if strings.Contains(remainder, "current_task") ||
				strings.Contains(remainder, "total_steps") ||
				strings.Contains(remainder, "action") ||
				strings.Contains(remainder, "state") {
				finalTask = strings.TrimSpace(finalTask[:idx])
			} else {
				break
			}
		}

		if isStatusOnlyMessage(finalTask) {
			fmt.Printf("[SE-DEBUG] ⚠️ [方案1] 纯状态消息拦截: %q\n", finalTask)
			m.cMonitor.UpdatePmStatus(types.RoleStatusIdle)
			return nil
		}
		fmt.Printf("[SE-DEBUG] ✅ [方案1] 通过状态消息检查\n")

		currentState := m.cMonitor.GetProjectState()
		if currentState == types.ProjectStateDone || currentState == types.ProjectStateError || m.seReportedComplete {
			fmt.Printf("[SE-DEBUG] 🛡️ [方案1] 审核模式拦截@SE(state=%d seReportedComplete=%v)\n", currentState, m.seReportedComplete)

			m.reviewCount++
			if m.reviewCount >= 3 {
				fmt.Printf("[handleToPM] 🔴 [方案1] 审核违规@SE达%d次，强制流转AP\n", m.reviewCount)
				m.currentRole = ""
				m.cMonitor.UpdatePmStatus(types.RoleStatusIdle)
				m.SetHandoverPending(HandoverPMToAP)
				if m.apProcessor != nil {
					return m.handleAPReview("⚠️ PM多次违规@SE，系统强制移交AP审批")
				}
				m.forceProjectApproved()
				return nil
			}

			fmt.Printf("[handleToPM] ⚠️ [方案1] 审核违规@SE(%d次)，流转AP\n", m.reviewCount)
			m.currentRole = ""
			m.cMonitor.UpdatePmStatus(types.RoleStatusIdle)
			m.SetHandoverPending(HandoverPMToAP)
			if m.apProcessor != nil {
				return m.handleAPReview("PM在审核模式下@SE，系统转交AP审批")
			}
			m.forceProjectApproved()
			return nil
		} else {
			m.cMonitor.UpdateProjectState(types.ProjectStateRunning)
		}

		m.addPMToSEMsg(finalTask)

		pmTaskId := ""
		if m.richBuilder != nil {
			pmTaskId = m.richBuilder.GetCurrentTaskID()
			if pmTaskId != "" {
				m.richBuilder.UpdateTask(pmTaskId, 0, "done")
				m.richBuilder.UpdateTask(pmTaskId, 1, "running")
			}
		}

		taskPreview := finalTask
		if len(taskPreview) > 80 {
			taskPreview = taskPreview[:80] + "..."
		}
		_ = m.boardManager.UpdateTask(taskPreview, 0)

		m.cMonitor.UpdatePmStatus(types.RoleStatusIdle)

		go func() {
			filteredContent := filterDuplicateMentions(resp.Content)
			m.sendToDingTalk(fmt.Sprintf("[PM→SE] %s", filteredContent))
		}()

		fmt.Printf("[SE-DEBUG] 🚀 [方案1] 即将调用 startSETaskWithFrom: task=%q (时间:%s)\n",
			finalTask[:min(60, len(finalTask))], time.Now().Format("15:04:05.000"))
		err := m.startSETaskWithFrom(finalTask, "pm")
		fmt.Printf("[SE-DEBUG] ✅ [方案1] startSETaskWithFrom 返回: err=%v (时间:%s)\n",
			err, time.Now().Format("15:04:05.000"))

		if pmTaskId != "" && m.richBuilder != nil {
			if err != nil {
				m.richBuilder.UpdateTask(pmTaskId, 1, "error")
				m.richBuilder.CompleteTaskList(pmTaskId, "error", nil)
			} else {
				m.richBuilder.UpdateTask(pmTaskId, 2, "running")
			}
		}

		return err
	}

	// [方案1] HasTasks 检测：第二优先级（如果没有 @SE 但有任务JSON）
	if resp.HasTasks {
		if m.seReportedComplete {
			fmt.Printf("[handleToPM] 🛡️ HasTasks审核拦截: seReportedComplete=true, PM不应再分配任务, 流转AP\n")
			m.reviewCount++
			m.currentRole = ""
			m.cMonitor.UpdatePmStatus(types.RoleStatusIdle)
			m.SetHandoverPending(HandoverPMToAP)
			if m.apProcessor != nil {
				return m.handleAPReview("PM在审核模式下通过HasTasks分配任务，系统转交AP审批")
			}
			m.forceProjectApproved()
			return nil
		}
		fmt.Println("[System] [方案1] HasTasks命中 (无@SE但有任务JSON), starting SE...")
		m.writeRouteLog("[SE-TASK-HASTASKS] from=pm")

		m.addPMToSEMsg(resp.Content)

		_ = m.boardManager.UpdateTask(resp.Tasks.CurrentTask, resp.Tasks.TotalSteps)

		m.cMonitor.UpdatePmStatus(types.RoleStatusIdle)

		go func() {
			filteredTask := filterDuplicateMentions(resp.Tasks.CurrentTask)
			m.sendToDingTalk(fmt.Sprintf("[PM→SE 任务] %s", filteredTask))
		}()

		m.cMonitor.UpdateProjectState(types.ProjectStateRunning)

	return m.startSETask(resp.Tasks.CurrentTask)
	}

	// 🆕 @AP 检测：第三优先级（在 @SE 和 HasTasks 之后）
	hasAP := strings.Contains(strings.ToLower(resp.Content), "@ap")
	fmt.Printf("[TRACE-AP-ROUTE] [方案1] @AP检测(第三优先级): hasAP=%v content_preview=%q\n",
		hasAP, resp.Content[:min(120, len(resp.Content))])
	m.writeRouteLog(fmt.Sprintf("[TRACE-AP-CHECK] hasAP=%v", hasAP))
	if hasAP {
		// 剔除 @AP 标签，保存干净的PM审核结论
		cleanPMContent := strings.Replace(resp.Content, "@AP", "", -1)
		cleanPMContent = strings.Replace(cleanPMContent, "@ap", "", -1)
		cleanPMContent = strings.TrimSpace(cleanPMContent)
		m.addPMToUserMsg(cleanPMContent)

		// 检查SE状态：非 busy 才能路由到AP
		seStatus := m.cMonitor.GetSeStatus()
		fmt.Printf("[TRACE-AP-ROUTE] @AP命中，seStatus=%v\n", seStatus)
		if seStatus == types.RoleStatusBusy {
			fmt.Println("[System] ⚠️ SE仍在忙碌中，等待SE完成后再路由AP [TAG-AP-BLOCK]")
			m.writeRouteLog("[TRACE-AP-BLOCKED] SE仍在忙碌中，忽略@AP")
		} else {
			fmt.Println("[System] PM @AP detected, 路由到AP审批...")
			m.writeRouteLog("[TRACE-AP-ROUTE] @AP命中，即将调用handleAPReview")
			m.currentRole = ""
			m.cMonitor.UpdatePmStatus(types.RoleStatusIdle)
			m.SetHandoverPending(HandoverPMToAP)
			if m.apProcessor != nil {
				fmt.Printf("[TRACE-AP-CALL] 即将调用handleAPReview\n")
				m.writeRouteLog("[TRACE-AP-CALL] 调用handleAPReview")
				return m.handleAPReview(cleanPMContent)
			}
			fmt.Println("[TRACE-AP-CALL] ⚠️ apProcessor为空，走forceProjectApproved")
			m.writeRouteLog("[TRACE-AP-EMPTY] apProcessor为空，forceProjectApproved")
			m.forceProjectApproved()
			return nil
		}
	} else {
		lowerResp := strings.ToLower(resp.Content)
		fmt.Printf("[DEBUG-3RD] ProcessStream路径: content=%q hasAP=%v len=%d\n", lowerResp[:min(100, len(lowerResp))], hasAP, len(resp.Content))
		hasApprovalKeywords := strings.Contains(lowerResp, "已验证") ||
			strings.Contains(lowerResp, "审核通过") ||
			strings.Contains(lowerResp, "验证通过") ||
			strings.Contains(lowerResp, "任务完成") ||
			strings.Contains(lowerResp, "已完成") ||
			strings.Contains(lowerResp, "approved") ||
			strings.Contains(lowerResp, "通过")
		if hasApprovalKeywords && !hasAP {
			fmt.Println("[handleToPM] ⚠️ 第三层保护(ProcessStream路径): PM输出含审批关键词但缺@AP，自动补全并转AP")
			resp.Content = "@AP " + resp.Content
			m.addPMToUserMsg(strings.Replace(strings.Replace(resp.Content, "@AP", "", 1), "@ap", "", 1))
			m.currentRole = ""
			m.cMonitor.UpdatePmStatus(types.RoleStatusIdle)
			m.SetHandoverPending(HandoverPMToAP)
			if m.apProcessor != nil {
				return m.handleAPReview(strings.TrimSpace(strings.Replace(resp.Content, "@AP", "", 1)))
			}
			m.forceProjectApproved()
			return nil
		}
	}

	state := m.cMonitor.GetProjectState()
	debugInfo := fmt.Sprintf("[DEBUG-REVIEW] state=%v content_head=%q",
		state, resp.Content[:min(80, len(resp.Content))])
	fmt.Println(debugInfo)
	m.writeRouteLog(debugInfo)

	fmt.Printf("[DEBUG-AP-ROUTE] state=%v hasAP=%v hasApprove=%v content_preview=%q\n",
		state,
		strings.Contains(strings.ToLower(resp.Content), "@ap"),
		m.extractReviewResult(resp.Content).Found,
		resp.Content[:min(120, len(resp.Content))])

	// 非审核模式兜底：检测是否包含 approve JSON（兼容旧逻辑）
	fallbackReview := m.extractReviewResult(resp.Content)
	fmt.Printf("[System] 📋 extractReviewResult: found=%v result=%v reason=%s\n",
		fallbackReview.Found, fallbackReview.Result, fallbackReview.Reason)
	if fallbackReview.Found && fallbackReview.Result == "approve" {
		seStatus := m.cMonitor.GetSeStatus()
		if seStatus == types.RoleStatusBusy {
			fmt.Println("[System] 🚨 SE仍在执行中！PM的approve JSON无效 [TAG-AP-BLOCK-3]")
			fmt.Printf("[System] seStatus=%v 忽略approve，走正常流程\n", seStatus)
		} else {
			fmt.Println("[System] ⚠️ 非审核模式但检测到 approve JSON → 补触发 AP 审批 [TAG-D1]")
			m.SetHandoverPending(HandoverPMToAP)
			m.cMonitor.UpdateProjectState(types.ProjectStateDone)
			if m.apProcessor != nil {
				return m.handleAPReview("请AP进行最终质量审批")
			}
			m.forceProjectApproved()
			return nil
		}
	}

	// 🆕 智能修正：PM输出@USR但包含任务JSON → 强制路由到SE
	if resp.HasTasks && (strings.Contains(strings.ToLower(resp.Content), "@usr") || !strings.Contains(strings.ToLower(resp.Content), "@se")) {
		fmt.Println("[TRACE-PM-ROUTE] 🔧 智能修正: PM输出含任务JSON但无@SE, 强制路由到SE!")
		m.writeRouteLog("[SMART-FIX] HasTasks但走闲聊分支, 强制转SE")

		m.addPMToSEMsg(resp.Content)
		_ = m.boardManager.UpdateTask(resp.Tasks.CurrentTask, resp.Tasks.TotalSteps)
		m.cMonitor.UpdatePmStatus(types.RoleStatusIdle)

		go func() {
			filteredTask := filterDuplicateMentions(resp.Tasks.CurrentTask)
			m.sendToDingTalk(fmt.Sprintf("[PM→SE 任务] %s", filteredTask))
		}()

		m.cMonitor.UpdateProjectState(types.ProjectStateRunning)
		return m.startSETask(resp.Tasks.CurrentTask)
	}

	// [方案1-已移除] 旧的 @SE 和 HasTasks 检测已移至文件前面（最高优先级）

	// 🆕 [FIX-20260529] 精准兜底检测：仅对明确编程信号强制转SE（避免误判闲聊）
	originalUserMsg := strings.ToLower(parsedMsg.Content)
	strongProgrammingSignals := []string{
		".go", ".py", ".js", ".ts", ".java", ".cpp", ".c ", ".h ",
		"go run", "npm run", "python ", "javac ",
		"hello world", "hello.go", "main()",
		"fmt.Println", "console.log", "print(",
		"func main", "def main", "public static void",
	}
	isClearProgrammingTask := false
	for _, signal := range strongProgrammingSignals {
		if strings.Contains(originalUserMsg, signal) {
			isClearProgrammingTask = true
			break
		}
	}

	if isClearProgrammingTask && !resp.HasTasks && !strings.Contains(strings.ToLower(resp.Content), "@se") && !m.seReportedComplete && m.currentRole != "se" {
		fmt.Println("[TRACE-PM-ROUTE] 🔧 精准兜底: 检测到明确编程信号但PM未@SE, 强制转SE!")
		m.writeRouteLog("[FALLBACK-FIX] 明确编程请求走闲聊分支, 强制转SE")

		taskDesc := fmt.Sprintf("请%s", parsedMsg.Content)
		m.addPMToSEMsg(taskDesc)
		m.cMonitor.UpdatePmStatus(types.RoleStatusIdle)
		m.cMonitor.UpdateProjectState(types.ProjectStateRunning)

		go func() {
			filteredTask := filterDuplicateMentions(taskDesc)
			m.sendToDingTalk(fmt.Sprintf("[PM→SE 任务(兜底)] %s", filteredTask))
		}()

		return m.startSETask(taskDesc)
	}

	// 没有@SE/没有任务 = 普通对话，PM回复用户
	fmt.Printf("[TRACE-PM-ROUTE] ⚠️ PM走【闲聊分支】! to=%q hasTasks=%v content_head=%q | AP不会启动!\n",
		parsedMsg.To, resp.HasTasks, resp.Content[:min(100, len(resp.Content))])

	m.addPMToUserMsg(resp.Content)

	if m.richBuilder != nil {
		pmTaskId := m.richBuilder.GetCurrentTaskID()
		if pmTaskId != "" {
			m.richBuilder.UpdateTask(pmTaskId, 0, "done")
			m.richBuilder.UpdateTask(pmTaskId, 1, "done")
			m.richBuilder.UpdateTask(pmTaskId, 2, "done")
			m.richBuilder.CompleteTaskList(pmTaskId, "done", &types.ResultBlock{
				Text: resp.Content,
			})
			m.richBuilder.Reset()
		}
	}

	m.cMonitor.UpdatePmStatus(types.RoleStatusIdle)
	m.syncBackendStatus("pm_chat", "PM闲聊回复")

	go func() {
		defer func() {
			if r := recover(); r != nil {
				fmt.Printf("[PM-DingTalk] 💥 panic recovered: %v\n", r)
			}
		}()
		filteredContent := filterDuplicateMentions(resp.Content)
		m.sendToDingTalk(fmt.Sprintf("[PM→USR] %s", filteredContent))
	}()

	return nil
}

// handleSEAskPM 处理SE向PM提问
func (m *Manager) handleSEAskPM(seQuestion string) (err error) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("[handleSEAskPM] 💥 panic recovered: %v\n", r)
			err = fmt.Errorf("panic: %v", r)
			m.cMonitor.UpdatePmStatus(types.RoleStatusIdle)
		}
	}()
	m.seAskPMCount++
	if m.seAskPMCount > 3 {
		fmt.Printf("[handleSEAskPM] ⚠️ SE问PM次数超限(%d)，停止循环\n", m.seAskPMCount)
		m.currentRole = ""
		m.cMonitor.UpdateSeStatus(types.RoleStatusIdle)
		errMsg := "⚠️ SE多次需要帮助，可能任务描述不清晰，请USR重新给出明确指令"
		m.addSEToPMMsg(errMsg)
		return fmt.Errorf("SE ask PM limit exceeded")
	}

	if allowed, reason := m.router.CheckTurnInternal("se", "se_ask_pm", true); !allowed {
		fmt.Printf("[handleSEAskPM] ⚠️ 轮换拦截: %s\n", reason)
		return nil
	}
	m.router.MarkProcessingStart("se")
	defer m.router.MarkProcessingEnd("se")

	m.currentRole = "pm"

	// SE向PM提问（自动加@PM）
	seQuestion = m.ensureSEToPM(seQuestion)
	fmt.Printf("[SE→PM] %s\n", seQuestion)

	pmHistoryRaw := m.GetHistory()
	pmHistory := make([]ai.ChatMessage, len(pmHistoryRaw))
	for i, msg := range pmHistoryRaw {
		pmHistory[i] = ai.ChatMessage{Role: msg.Role, Content: msg.Content}
	}

	fmt.Println("[handleSEAskPM] 🔍 使用ProcessReview（带工具验证）进行PM审核")
	resp, err := m.pmProcessor.ProcessReview(seQuestion, pmHistory, func(delta string) {
			m.emitStreamChunk("pm", delta)
		})
		if err != nil {
		errMsg := fmt.Sprintf("❌ PM审核失败: %v", err)
		m.PrintStreamAuditReport()
		m.addPMToUserMsg(errMsg)
			return fmt.Errorf("PM review failed: %w", err)
		}

		parsedResp := m.router.Parse("pm", resp.Content)
		if parsedResp.To == "se" {
			currentState := m.cMonitor.GetProjectState()
			if currentState == types.ProjectStateDone || currentState == types.ProjectStateError {
				fmt.Printf("[handleSEAskPM] 🛡️ 审核模式拦截@SE(state=%d)\n", currentState)
				m.currentRole = ""
				m.cMonitor.UpdatePmStatus(types.RoleStatusIdle)
				m.SetHandoverPending(HandoverPMToAP)
				if m.apProcessor != nil {
					return m.handleAPReview("PM在审核模式下@SE，系统转交AP审批")
				}
				m.forceProjectApproved()
				return nil
			}

			finalTask := parsedResp.Content
			idx := strings.LastIndex(finalTask, "\n{")
			if idx > 0 {
				remainder := finalTask[idx+1:]
				if strings.Contains(remainder, "current_task") || strings.Contains(remainder, "total_steps") {
					finalTask = strings.TrimSpace(finalTask[:idx])
				}
			}

			if isStatusOnlyMessage(finalTask) {
				m.cMonitor.UpdatePmStatus(types.RoleStatusIdle)
				return nil
			}

			m.addPMToSEMsg(finalTask)
			return m.startSETaskWithFrom(finalTask, "pm")
		}

		hasAP := strings.Contains(strings.ToLower(resp.Content), "@ap")
		if hasAP {
			cleanPMContent := strings.Replace(resp.Content, "@AP", "", -1)
			cleanPMContent = strings.Replace(cleanPMContent, "@ap", "", -1)
			cleanPMContent = strings.TrimSpace(cleanPMContent)

			fmt.Println("[handleSEAskPM] 🛡️ G48: PM审核已通过onChunk流式输出，跳过addPMToUserMsg避免重复")

			seStatus := m.cMonitor.GetSeStatus()
			if seStatus != types.RoleStatusBusy {
				m.currentRole = ""
				m.cMonitor.UpdatePmStatus(types.RoleStatusIdle)
				m.SetHandoverPending(HandoverPMToAP)
				if m.apProcessor != nil {
					return m.handleAPReview(cleanPMContent)
				}
				m.forceProjectApproved()
				return nil
			}
		} else {
			hasUSR := strings.Contains(strings.ToUpper(resp.Content), "@USR")
			if hasUSR {
				fmt.Println("[handleSEAskPM] 🛡️ G46第三层保护：检测到@USR，强制转AP审批")
				cleanContent := strings.TrimPrefix(strings.TrimSpace(resp.Content), "@USR")
				cleanContent = strings.TrimPrefix(cleanContent, "@usr")
				cleanContent = strings.TrimSpace(cleanContent)

				if cleanContent == "" || len(cleanContent) < 5 {
					cleanContent = "任务已验证，请进行最终质量审批"
				}

				fmt.Println("[handleSEAskPM] 🛡️ G48: PM审核已通过onChunk流式输出，跳过addPMToUserMsg避免重复")

				m.currentRole = ""
				m.cMonitor.UpdatePmStatus(types.RoleStatusIdle)
				m.SetHandoverPending(HandoverPMToAP)
				if m.apProcessor != nil {
					return m.handleAPReview(cleanContent)
				}
				m.forceProjectApproved()
				return nil
			}

			fmt.Println("[handleSEAskPM] 🛡️ G48: PM审核已通过onChunk流式输出，跳过addPMToUserMsg避免重复")
		}
		return nil
	}

// handleUserDirectToSE 用户直接@SE
func (m *Manager) handleUserDirectToSE(content string) error {
	return m.startSETaskWithFrom(content, "user")
}

// startSETask 启动SE任务（默认来源是PM）
func (m *Manager) startSETask(taskDesc string) error {
	return m.startSETaskWithFrom(taskDesc, "pm")
}

func (m *Manager) emitStreamChunk(role string, delta string) {
	if m.ctx != nil {
		msgId := m.getOrCreateStreamID(role)
		fmt.Printf("[SSE-AUDIT] 💧 送水: role=%s messageId=%s delta=%q (len=%d)\n", role, msgId, delta, len(delta))

		m.mu.Lock()
		if m.streamAuditLog == nil {
			m.streamAuditLog = make([]StreamAuditEntry, 0)
		}
		m.streamAuditLog = append(m.streamAuditLog, StreamAuditEntry{
			Timestamp: time.Now(),
			Role:      role,
			MessageID: msgId,
			Delta:     delta,
			DeltaLen:  len(delta),
		})
		m.mu.Unlock()

		path := PathPMStream
		if role == "se" {
			path = PathSEStream
		}
		m.msgBusSend(role, delta, "ai-stream-chunk", path, "emitStreamChunk", map[string]interface{}{
			"role":      role,
			"delta":     delta,
			"messageId": msgId,
		})
	}
}

func (m *Manager) PrintStreamAuditReport() {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.streamAuditLog) == 0 {
		fmt.Println("[SSE-AUDIT-REPORT] 📊 送水报告: 无送水记录")
		return
	}

	fmt.Println("\n═════════════════════════════════════════════════════")
	fmt.Println("📊 [SSE-AUDIT-REPORT] 后端送水审计报告")
	fmt.Println("═════════════════════════════════════════════════════")

	roleStats := make(map[string]int)
	totalBytes := 0

	for i, entry := range m.streamAuditLog {
		roleStats[entry.Role]++
		totalBytes += entry.DeltaLen
		preview := entry.Delta
		if len(preview) > 50 {
			preview = preview[:50] + "..."
		}
		fmt.Printf("  #%03d [%s] role=%-5s id=%-40s | %s\n",
			i+1, entry.Timestamp.Format("15:04:05.000"), entry.Role, entry.MessageID, preview)
	}

	fmt.Println("───────────────────────────────────────────────────────")
	fmt.Printf("📈 统计: 总送水%d次 | 总字节数:%d\n", len(m.streamAuditLog), totalBytes)
	fmt.Println("📦 按角色:")
	for role, count := range roleStats {
		fmt.Printf("   %-5s: %d次\n", role, count)
	}
	fmt.Println("═════════════════════════════════════════════════════")
}

// [G60] 前端调用：记录收到消息（用于前后端一致性校验）
func (m *Manager) RecordReceive(role, messageID, content, source string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.receiveAuditLog == nil {
		m.receiveAuditLog = make([]ReceiveAuditEntry, 0)
	}
	preview := content
	if len(preview) > 80 {
		preview = preview[:80] + "..."
	}
	fmt.Printf("[G60-RECEIVE] 🚰 收水: role=%s id=%s source=%s content=%q (len=%d)\n", role, messageID, source, preview, len(content))

	m.receiveAuditLog = append(m.receiveAuditLog, ReceiveAuditEntry{
		Timestamp:   time.Now(),
		Role:        role,
		MessageID:   messageID,
		Content:     content,
		ContentLen:  len(content),
		Source:      source,
	})
}

// [G60] 输出前后端一致性对比报告
func (m *Manager) PrintConsistencyReport() {
	m.mu.RLock()
	defer m.mu.RUnlock()

	fmt.Println("\n╔══════════════════════════════════════════════════════╗")
	fmt.Println("║ [G60] 🔄 前后端消息一致性校验报告                      ║")
	fmt.Println("╚══════════════════════════════════════════════════════╝")

	sendCount := len(m.streamAuditLog)
	receiveCount := len(m.receiveAuditLog)

	fmt.Printf("\n📤 后端送水: %d 条\n", sendCount)
	fmt.Printf("📥 前端收水: %d 条\n", receiveCount)

	if sendCount == 0 && receiveCount == 0 {
		fmt.Println("✅ 无数据（可能任务未开始或已清理）")
		return
	}

	sendByRole := make(map[string]int)
	receiveByRole := make(map[string]int)

	for _, entry := range m.streamAuditLog {
		sendByRole[entry.Role]++
	}
	for _, entry := range m.receiveAuditLog {
		receiveByRole[entry.Role]++
	}

	fmt.Println("\n┌─────────────────────────────────────────────────────┐")
	fmt.Println("│ 📊 按角色对比                                       │")
	fmt.Println("├──────────┬──────────┬──────────┬────────────────────┤")
	fmt.Println("│ 角色     │ 送水次数 │ 收水次数 │ 差异               │")
	fmt.Println("├──────────┼──────────┼──────────┼────────────────────┤")

	allRoles := make(map[string]bool)
	for role := range sendByRole {
		allRoles[role] = true
	}
	for role := range receiveByRole {
		allRoles[role] = true
	}

	hasMismatch := false
	for role := range allRoles {
		s := sendByRole[role]
		r := receiveByRole[role]
		diff := r - s
		status := "✅"
		if diff != 0 {
			status = "❌"
			hasMismatch = true
		}
		fmt.Printf("│ %-8s │ %8d │ %8d │ %4d (%s)          │\n", role, s, r, diff, status)
	}

	fmt.Println("└──────────┴──────────┴──────────┴────────────────────┘")

	if hasMismatch {
		fmt.Println("\n⚠️ 发现不一致！详细差异:")
		for role := range allRoles {
			s := sendByRole[role]
			r := receiveByRole[role]
			if s != r {
				if r < s {
					fmt.Printf("   ❌ %s: 送水%d条 > 收水%d条 (丢失%d条)\n", role, s, r, s-r)
				} else {
					fmt.Printf("   ⚠️ %s: 收水%d条 > 送水%d条 (多余%d条)\n", role, r, s, r-s)
				}
			}
		}
	} else {
		fmt.Println("\n✅ 前后端完全一致！")
	}

	if receiveCount > 0 {
		fmt.Println("\n┌─────────────────────────────────────────────────────┐")
		fmt.Println("│ 📥 前端收水明细                                     │")
		fmt.Println("├──────────┬─────────────────────────────────────────┤")
		for i, entry := range m.receiveAuditLog {
			preview := entry.Content
			if len(preview) > 40 {
				preview = preview[:40] + "..."
			}
			fmt.Printf("│ #%03d [%-12s] %-20s | %s\n",
				i+1, entry.Source, entry.Role, preview)
		}
		fmt.Println("└──────────┴─────────────────────────────────────────┘")
	}

	fmt.Println("╔══════════════════════════════════════════════════════╗")
}

// [G60] 清理审计日志（新任务开始时调用）
func (m *Manager) ClearAuditLogs() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.streamAuditLog = nil
	m.receiveAuditLog = nil
	fmt.Println("[G60] 审计日志已清理")
}

func (m *Manager) getOrCreateStreamID(role string) string {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.streamingMsgIDs == nil {
		m.streamingMsgIDs = make(map[string]string)
	}

	if id, exists := m.streamingMsgIDs[role]; exists {
		return id
	}

	id := fmt.Sprintf("%s_%d_%d", role, time.Now().UnixNano(), len(m.history))
	m.streamingMsgIDs[role] = id
	fmt.Printf("[SSE-AUDIT] 📤 送水: role=%s messageId=%s (第1次创建)\n", role, id)
	return id
}

func (m *Manager) clearStreamMessageIDs() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.streamingMsgIDs) > 0 {
		fmt.Printf("[SSE-AUDIT] 🗑️ 清理旧messageIds: %d个角色\n", len(m.streamingMsgIDs))
		m.streamingMsgIDs = make(map[string]string)
	}
}

func (m *Manager) cleanSEJSONContent(jsonStr string) string {
	fmt.Printf("[G51-FIX] 开始清理SE JSON内容, 原始长度: %d\n", len(jsonStr))

	var data map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &data); err != nil {
		fmt.Printf("[G51-FIX] JSON解析失败: %v, 返回原始内容\n", err)
		return ""
	}

	var actions []map[string]interface{}
	if actionsRaw, ok := data["actions"].([]interface{}); ok {
		for _, a := range actionsRaw {
			if actionMap, ok := a.(map[string]interface{}); ok {
				actions = append(actions, actionMap)
			}
		}
	}

	if len(actions) == 0 {
		fmt.Printf("[G51-FIX] 未找到actions数组\n")
		return ""
	}

	var actionDescs []string
	for _, action := range actions {
		actionType, _ := action["type"].(string)

		switch actionType {
		case "write_file":
			if path, ok := action["path"].(string); ok {
				actionDescs = append(actionDescs, fmt.Sprintf("创建文件: %s", path))
			}
		case "edit_file":
			if path, ok := action["path"].(string); ok {
				actionDescs = append(actionDescs, fmt.Sprintf("修改文件: %s", path))
			}
		case "exec":
			if cmd, ok := action["command"].(string); ok {
				actionDescs = append(actionDescs, fmt.Sprintf("执行命令: %s", cmd))
			}
		case "read_file":
			if path, ok := action["path"].(string); ok {
				actionDescs = append(actionDescs, fmt.Sprintf("读取文件: %s", path))
			}
		case "search_files":
			if pattern, ok := action["pattern"].(string); ok {
				actionDescs = append(actionDescs, fmt.Sprintf("搜索文件: %s", pattern))
			}
		case "git_operation":
			if gitAction, ok := action["git_action"].(string); ok {
				actionDescs = append(actionDescs, fmt.Sprintf("Git操作: %s", gitAction))
			} else {
				actionDescs = append(actionDescs, "Git操作")
			}
		case "run_tests":
			actionDescs = append(actionDescs, fmt.Sprintf("运行测试: %s", func() string {
				if p, ok := action["test_pattern"].(string); ok && p != "" { return p }
				return "./..."
			}()))
		default:
			actionDescs = append(actionDescs, fmt.Sprintf("操作: %s", actionType))
		}
	}

	result := "✅ SE已完成任务执行，请审核结果"
	if len(actionDescs) > 0 {
		result += "\n\n**执行的操作**:\n"
		for _, desc := range actionDescs {
			result += fmt.Sprintf("- %s\n", desc)
		}
	}

	if returnVal, ok := data["return"].(bool); ok && returnVal {
		result += "\n\n📋 **需要验证**: 请用工具检查上述操作结果"
	}

	fmt.Printf("[G51-FIX] 清理完成, 新内容长度: %d\n", len(result))
	return result
}

// startSETaskWithFrom 启动SE任务，指定来源
func (m *Manager) startSETaskWithFrom(taskDesc string, from string) error {
	fmt.Printf("[SE-DEBUG] 🚀 startSETask入口 from=%s task=%q (时间:%s)\n", from, taskDesc[:min(60, len(taskDesc))], time.Now().Format("15:04:05"))
	fmt.Printf("[SE-DEBUG] 当前状态: currentRole=%q, isProcessing=%v, lastSpokenBy=%q\n",
		m.currentRole, m.router.isProcessing, m.router.lastSpokenBy)
	m.writeRouteLog(fmt.Sprintf("[SE-TASK] from=%s task='%s'", from, taskDesc))

	// [FIX-20260529-CONCURRENCY] SE互斥检查：如果SE正在执行，拒绝新调用
	if m.currentRole == "se" {
		currentTaskDesc := m.currentSETask
		if len(currentTaskDesc) > 40 {
			currentTaskDesc = currentTaskDesc[:40] + "..."
		}
		fmt.Printf("[SE-DEBUG] ⚠️ SE正在执行中(currentRole=se)，拒绝新调用 from=%s task=%q\n", from, taskDesc[:min(40, len(taskDesc))])
		m.writeRouteLog(fmt.Sprintf("[SE-TASK] ❌ BLOCKED: SE正在执行中，拒绝新调用 from=%s", from))
		m.msgBusSend("system", fmt.Sprintf("SE正在执行任务中，请等待完成后再发送新指令。(当前任务: %s)", currentTaskDesc), "warning", PathSystem, "startSETask:se_busy", map[string]interface{}{
			"from":        from,
			"blockedTask": taskDesc[:min(60, len(taskDesc))],
		})
		return fmt.Errorf("SE正在执行中，拒绝新调用")
	}

	// 创建PM任务记录（PM分配的任务）
	if from == "pm" && m.taskManager != nil {
		cleanDesc := strings.TrimSpace(taskDesc)
		// 去掉末尾的 JSON 元数据（AI常附加 {"current_task":...}）
		for {
			idx := strings.LastIndex(cleanDesc, "\n{")
			if idx < 0 {
				break
			}
			remainder := cleanDesc[idx+1:]
			if strings.Contains(remainder, "current_task") ||
				strings.Contains(remainder, "total_steps") ||
				strings.Contains(remainder, "action") ||
				strings.Contains(remainder, "state") {
				cleanDesc = strings.TrimSpace(cleanDesc[:idx])
			} else {
				break
			}
		}
		// 截断过长描述
		if len(cleanDesc) > 60 {
			cleanDesc = cleanDesc[:60] + "..."
		}
		m.taskManager.CreateTask("分配："+cleanDesc, "PM")
		m.taskManager.CompleteLastTaskByRole("PM")
	}

	// 防御：过滤纯状态确认消息
	if isStatusOnlyMessage(taskDesc) {
		m.writeRouteLog(fmt.Sprintf("[SE-TASK] ❌ BLOCKED: status-only message '%s'", taskDesc))
		fmt.Printf("[startSETask] ⚠️ 纯状态确认消息，跳过SE启动: %q\n", taskDesc)
		m.currentRole = ""
		m.cMonitor.UpdateSeStatus(types.RoleStatusIdle)
		return nil
	}

	if allowed, reason := m.router.CheckTurnInternal(from, "se_task_"+from, true); !allowed {
		m.writeRouteLog(fmt.Sprintf("[SE-TASK] ❌ BLOCKED by turn: %s", reason))
		fmt.Printf("[startSETask] ⚠️ 轮换拦截: %s\n", reason)
		return nil
	}
	m.writeRouteLog("[SE-TASK] ✅ Turn passed, executing...")
	m.router.MarkProcessingStart("se")
	defer func() {
		if r := recover(); r != nil {
			stack := debug.Stack()
			fmt.Printf("[startSETask] 💥 PANIC recovered: %v\n", r)
			fmt.Printf("[startSETask] 📋 Stack trace:\n%s\n", string(stack))
			func() {
				f, _ := os.OpenFile(filepath.Join(os.TempDir(), "argus_se_panic.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
				if f != nil {
					f.WriteString(fmt.Sprintf("[%s] PANIC: %v\nStack:\n%s\n",
						time.Now().Format("15:04:05.000"), r, string(stack)))
					f.Close()
				}
			}()
			m.cMonitor.UpdateSeStatus(types.RoleStatusIdle)
		}
		m.router.MarkProcessingEnd("se")
		m.currentSETask = ""
		fmt.Printf("[PROBE-SE] ⬅️ startSETask defer执行: currentRole=%q → 清空 | seReportedComplete=%v (时间:%s)\n",
			m.currentRole, m.seReportedComplete, time.Now().Format("15:04:05.000"))
		m.currentRole = ""
	}()

	m.currentRole = "se"
	m.currentSETask = taskDesc // [FIX-20260529] 保存当前任务描述
	m.seContinueCount = 0
	m.seAskPMCount = 0
	m.seReportedComplete = false
	m.seEmptyActionCount = 0

	m.cMonitor.ResetRetryFlag()

	m.cleanupOldArtifacts()

	if m.seProcessor != nil {
		m.seProcessor.ResetHistory()
		fmt.Printf("[SE] ✅ History已重置 (新任务开始)\n")
	}

	// ⚠️ 关键修复：SE任务执行时临时释放isProcessing锁
	// 防止内部调用的handleToPM被ProcessMessage的队列阻塞
	m.processingMu.Lock()
	wasProcessing := m.isProcessing
	m.isProcessing = false
	m.processingMu.Unlock()

	// 在函数退出时恢复锁（所有return路径都经过defer）
	defer func() {
		if wasProcessing {
			m.processingMu.Lock()
			m.isProcessing = true
			m.processingMu.Unlock()
			fmt.Printf("[startSETask] ✅ 恢复isProcessing锁 (原值=true)\n")
		}
	}()

	// 更新SE状态为busy
	m.cMonitor.UpdateSeStatus(types.RoleStatusBusy)
	m.syncBackendStatus("se_processing", "SE开始执行: "+taskDesc[:min(40, len(taskDesc))])

	// 更新看板
	m.boardManager.UpdateTask(taskDesc, 1)

	fmt.Printf("[SE] 开始执行任务: %s\n", taskDesc)

	if m.seProcessor == nil {
		m.cMonitor.UpdateSeStatus(types.RoleStatusIdle)
		return fmt.Errorf("seProcessor not initialized")
	}

	// SE处理任务（内部分步执行，流式）
	aiGen := m.getResetGeneration()

	// ⏰ SE超时45秒 + 最多重试2次
	safeCtx := context.Background()
	m.mu.RLock()
	if m.ctx != nil {
		safeCtx = m.ctx
	}
	m.mu.RUnlock()

	const seTimeout = 120 * time.Second
	const maxRetries = 2

	fmt.Printf("[PROBE-startSETask] 即将调用ProcessTaskWithTools: task=%q timeout=%v (时间:%s)\n",
		taskDesc[:min(60, len(taskDesc))], seTimeout, time.Now().Format("15:04:05.000"))

	var resp *ai.SEResponse
	var err error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			if m.IsUserStopped() {
				fmt.Printf("[SE] ⛔ 用户已停止，放弃重试\n")
				break
			}
			backoff := time.Duration(attempt) * 5 * time.Second
			fmt.Printf("[SE] ⏳ 第%d次重试，等待%v...\n", attempt, backoff)
			m.syncBackendStatus("se_retry", fmt.Sprintf("SE第%d次重试(等待%v)", attempt, backoff))
			time.Sleep(backoff)
		}

		seCtx, seCancel := context.WithTimeout(safeCtx, seTimeout)
		m.seProcessor.SetContext(seCtx)

		m.writeRouteLog(fmt.Sprintf("[SE-API-START] from=%s attempt=%d/%d ctx_err=%v time=%s",
			from, attempt, maxRetries, seCtx.Err(), time.Now().Format("15:04:05")))

		if m.richBuilder != nil && attempt == 0 {
			m.richBuilder.StartTaskList("se", "SE 执行: "+taskDesc[:min(30, len(taskDesc))], []types.TaskItemDef{
				{Text: "⏳ 规划操作..."},
			})
			m.richBuilder.UpdateTask(m.richBuilder.GetCurrentTaskID(), 0, "running")
		}

		if attempt == 0 && m.aiClient != nil {
			m.aiClient.CloseIdleConnections()
			fmt.Println("[SE] 🔧 关闭空闲连接，避免复用PM用过的死连接")
		}

		if attempt == 0 {
			state, _ := m.cMonitor.ReadState()
			func() {
				f, _ := os.OpenFile(filepath.Join(os.TempDir(), "argus_se_state_probe.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
				if f != nil {
					f.WriteString(fmt.Sprintf("[%s] [BEFORE-API] attempt=%d cMonitor.SE=%q cMonitor.PM=%q currentRole=%q seReportedComplete=%v isProcessing=%v aiGen=%d curGen=%d\n",
						time.Now().Format("15:04:05.000"), attempt, state.SeStatus, state.PmStatus, m.currentRole, m.seReportedComplete, m.isProcessing, aiGen, m.getResetGeneration()))
					f.Close()
				}
			}()
		}

		fmt.Printf("[PROBE-CALL] 🚀 即将调用ProcessTaskWithTools (时间:%s)\n", time.Now().Format("15:04:05.000"))

		if attempt == 0 {
			func() {
				f, _ := os.OpenFile(filepath.Join(os.TempDir(), "argus_se_state_probe.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
				if f != nil {
					seProcStatus := "nil"
					if m.seProcessor != nil {
						seProcStatus = "ok"
					}
					f.WriteString(fmt.Sprintf("[%s] [PRE-CALL-PROBE] seProcessor=%s\n",
						time.Now().Format("15:04:05.000"), seProcStatus))
					f.Close()
				}
			}()
			if m.seProcessor == nil {
			errMsg := fmt.Errorf("seProcessor is nil")
			fmt.Printf("[SE] 🔴 FATAL: %v\n", errMsg)
			m.writeRouteLog(fmt.Sprintf("[SE-FATAL] %s", errMsg.Error()))
			return errMsg
		}

		// 首次启动SE任务时异步构建语义搜索索引
		if attempt == 0 {
			go func() {
				if err := m.seProcessor.EnsureIndexer(); err != nil {
					fmt.Printf("[SemSearch] 索引构建警告: %v\n", err)
				}
			}()
		}
		}

		// [v0.7.2] ContextWindow: 记录 SE 任务输入
		if m.contextWindow != nil {
			m.contextWindow.AddMessage(memory.RoleUser, taskDesc, 0, "")
			m.pushTokenStats()
			m.WriteDebugLog("[ContextBridge] ✅ SE 任务输入已写入 ContextWindow + TokenStats 已推送")
		}

		resp, err = m.seProcessor.ProcessTaskWithTools(taskDesc, func(delta string) {
			m.cMonitor.UpdateSeChunkTime()
			m.emitStreamChunk("se", delta)
		})
		fmt.Printf("[PROBE-CALL] ✅ ProcessTaskWithTools已返回 (时间:%s)\n", time.Now().Format("15:04:05.000"))

		if attempt == 0 {
			state, _ := m.cMonitor.ReadState()
			func() {
				f, _ := os.OpenFile(filepath.Join(os.TempDir(), "argus_se_state_probe.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
				if f != nil {
					f.WriteString(fmt.Sprintf("[%s] [AFTER-API] attempt=%d err=%v cMonitor.SE=%q cMonitor.PM=%q currentRole=%q isProcessing=%v aiGen=%d curGen=%d\n",
						time.Now().Format("15:04:05.000"), attempt, err, state.SeStatus, state.PmStatus, m.currentRole, m.isProcessing, aiGen, m.getResetGeneration()))
					f.Close()
				}
			}()
		}

		seCancel()

		respActions := -1
		if resp != nil {
			respActions = len(resp.Actions)
		}
		fmt.Printf("[PROBE-SE] 📡 ProcessTaskWithTools返回: err=%v actions=%d content_len=%d (时间:%s)\n",
			err, respActions, func() int { if resp != nil { return len(resp.Content) }; return 0 }(), time.Now().Format("15:04:05.000"))
		m.writeRouteLog(fmt.Sprintf("[SE-API-CALL] from=%s attempt=%d/%d err=%v actions=%d time=%s",
			from, attempt, maxRetries, err, respActions, time.Now().Format("15:04:05")))

		if err == nil {
			break
		}

		fmt.Printf("[SE] ❌ 第%d/%d次尝试失败: %v\n", attempt+1, maxRetries+1, err)

		if strings.Contains(err.Error(), "context deadline exceeded") ||
			strings.Contains(err.Error(), "timeout") ||
			strings.Contains(err.Error(), "context canceled") ||
			strings.Contains(err.Error(), "connection") ||
			strings.Contains(err.Error(), "forcibly closed") ||
			strings.Contains(err.Error(), "read stream") {
			m.syncBackendStatus("se_retry", fmt.Sprintf("SE网络错误重试(%d/%d): %v", attempt+1, maxRetries+1, err.Error()[:min(60, len(err.Error()))]))
			continue
		}

		break
	}

	fmt.Printf("[PROBE-startSETask] ⏹️ ProcessTaskWithTools循环结束: err=%v actions=%d (时间:%s)\n",
		err, func() int { if resp != nil { return len(resp.Actions) }; return -1 }(), time.Now().Format("15:04:05.000"))

	if m.isGhostCall(aiGen) {
		fmt.Printf("[startSETask] ⚠️ 检测到复位后的幽灵SE调用，丢弃结果\n")
		m.writeRouteLog("[GHOST-CALL] SE响应被丢弃(复位检测)")
		func() {
			f, _ := os.OpenFile(filepath.Join(os.TempDir(), "argus_diag.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			if f != nil {
				f.WriteString(fmt.Sprintf("[%s] GHOST-CALL aiGen=%d currentGen=%d\n", time.Now().Format("15:04:05"), aiGen, m.getResetGeneration()))
				f.Close()
			}
		}()
		m.currentRole = ""
		m.cMonitor.UpdateSeStatus(types.RoleStatusIdle)
		return nil
	}
	func() {
		f, _ := os.OpenFile(filepath.Join(os.TempDir(), "argus_diag.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if f != nil {
			f.WriteString(fmt.Sprintf("[%s] GHOST-PASSED err=%v actions=%d\n", time.Now().Format("15:04:05"), err, len(resp.Actions)))
			f.Close()
		}
	}()
	if err != nil {
		m.currentRole = ""
		m.cMonitor.UpdateSeStatus(types.RoleStatusIdle)

		m.boardManager.UpdateTask(i18n.T("msg.task_failed"), 0)

		if m.ctx != nil {
			m.msgBusSend("system", err.Error(), "error", PathSystem, "startSETaskWithFrom:se_error", map[string]interface{}{
				"error": err.Error(),
				"stage": "se",
			})
		}

		errMsg := fmt.Sprintf("%s\n\n%s", i18n.T("err.se_failed", err), i18n.T("err.task_cannot_continue"))
		m.addSEToPMMsg(errMsg)
		m.syncBackendStatus("error", "SE执行失败: "+err.Error())
		if from != "pm" {
			m.addSEToUserMsg(i18n.T("err.se_failed", err))
		}
		if m.onProjectStateChanged != nil {
			m.onProjectStateChanged("error")
		}
		return fmt.Errorf("SE process failed: %w", err)
	}

	fmt.Printf("[SE Debug] Raw response length: %d\n", len(resp.Content))
	fmt.Printf("[SE Debug] Actions count: %d\n", len(resp.Actions))
	fmt.Printf("[SE Debug] Completed: %v\n", resp.Completed != nil)
	fmt.Printf("[SE Debug] NeedHelp: %v\n", resp.NeedHelp)
	m.writeRouteLog(fmt.Sprintf("[SE-RESP] actions=%d completed=%v needHelp=%v content_len=%d",
		len(resp.Actions), resp.Completed != nil, resp.NeedHelp, len(resp.Content)))

	// [v0.7.2] ContextWindow: 记录 SE 响应
	if m.contextWindow != nil && len(resp.Content) > 0 {
		m.contextWindow.AddMessage(memory.RoleAssistant, resp.Content, 0, "")
		m.pushTokenStats()
		m.WriteDebugLog("[ContextBridge] ✅ SE 响应已写入 ContextWindow + TokenStats 已推送")
	}

	if len(resp.Content) > 0 {
		fmt.Printf("[SE Debug] Response preview: %s\n", resp.Content[:min(500, len(resp.Content))])
	}
	if len(resp.Actions) > 0 {
		for i, a := range resp.Actions {
			fmt.Printf("[SE Debug] Action[%d]: type=%s path=%s\n", i, a.Type, a.Path)
		}
	}

	// 🔴 [FIX-20260528] 防死循环：检测空actions死循环
	if len(resp.Actions) == 0 && resp.Completed == nil && !resp.NeedHelp {
		m.seEmptyActionCount++
		fmt.Printf("[SE-DEBUG] ⚠️ 空actions响应 #%d | content_len=%d (时间:%s)\n",
			m.seEmptyActionCount, len(resp.Content), time.Now().Format("15:04:05"))
		m.writeRouteLog(fmt.Sprintf("[SE-EMPTY-ACTION] count=%d content_len=%d continueCount=%d",
			m.seEmptyActionCount, len(resp.Content), m.seContinueCount))

		// 🆕 [FIX-JSON-FALLBACK] 第1次空actions时，用严格JSON重试
		if m.seEmptyActionCount == 1 && len(resp.Content) > 0 {
			fmt.Println("[SE-DEBUG] 🔄 检测到空actions，用严格JSON提示词重试AI调用...")
			m.writeRouteLog("[SE-RETRY-JSON] 空actions，用JSON-only提示词重试")
			retryPrompt := fmt.Sprintf(
				"Your last response had JSON errors. Output ONLY valid JSON:\n%s",
				`{"actions":[{"type":"write_file","path":"FILENAME","content":"CODE"},{"type":"exec","command":"COMMAND"}]}`,
			)
			retryCtx, retryCancel := context.WithTimeout(safeCtx, seTimeout)
			m.seProcessor.SetContext(retryCtx)
			retryResp, retryErr := m.seProcessor.ProcessTaskWithTools(retryPrompt, func(delta string) {
				m.cMonitor.UpdateSeChunkTime()
				m.emitStreamChunk("se", delta)
			})
			retryCancel()
			if retryErr == nil && len(retryResp.Actions) > 0 {
				fmt.Printf("[SE-DEBUG] ✅ JSON重试成功! actions=%d\n", len(retryResp.Actions))
				m.writeRouteLog(fmt.Sprintf("[SE-RETRY-JSON] ✅ 重试成功 actions=%d", len(retryResp.Actions)))
				m.seEmptyActionCount = 0
				resp = retryResp
			} else {
				fmt.Println("[SE-DEBUG] ❌ JSON重试失败")
				m.writeRouteLog("[SE-RETRY-JSON] ❌ 重试失败")
			}
		}

		if m.seEmptyActionCount >= 3 {
			fmt.Println("[🛡️ 防死循环] SE连续3次返回空actions，强制路由到PM [TAG-E1]")
			m.writeRouteLog("[SE-FORCE-ROUTE] 连续3次空actions，强制路由到PM")
			resp.Content = strings.TrimSpace(resp.Content)
			if resp.Content == "" {
				resp.Content = "✅ SE已完成（无操作动作）"
			}
			m.seReportedComplete = true
			m.currentRole = ""
			m.cMonitor.UpdateSeStatus(types.RoleStatusIdle)
			m.SetHandoverPending(HandoverSEToPM)
			m.cMonitor.UpdateProjectState(types.ProjectStateDone)
			m.syncBackendStatus("done", "SE空actions兜底，强制完成 [TAG-E1]")
			summary := "✅ SE已完成任务执行（无文件操作），请审核结果"
			if from != "pm" {
				m.addSEToUserMsg(summary)
			}
			m.msgBusSend("se", summary, "exec_completed", PathSEExec, "startSETaskWithFrom:empty-action-force", map[string]interface{}{
				"executor":  "se",
				"result":    "completed",
				"status":    "completed",
				"timestamp": time.Now().Unix(),
			})
			_, err := m.ProcessMessageFrom("se", summary+"\\n"+resp.Content)
			return err
		}
	} else {
		m.seEmptyActionCount = 0
	}

	// 执行actions（先执行动作，确保有效操作不被NeedHelp跳过）
	if len(resp.Actions) > 0 {
		func() {
			f, _ := os.OpenFile(filepath.Join(os.TempDir(), "argus_diag.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			if f != nil {
				f.WriteString(fmt.Sprintf("[%s] CALLING-EXECUTE-ACTIONS count=%d\n", time.Now().Format("15:04:05"), len(resp.Actions)))
				f.Close()
			}
		}()
		fmt.Printf("[PROBE-startSETask] 🚀 开始执行actions: count=%d (时间:%s)\n",
			len(resp.Actions), time.Now().Format("15:04:05.000"))
		if err := m.executeSEActions(resp.Actions); err != nil {
			fmt.Printf("[PROBE-startSETask] ❌ executeSEActions失败: %v (耗时:%s)\n",
				err, time.Now().Format("15:04:05.000"))

			maxFixRetries := 3
			var fixErr error = err
			for fixAttempt := 0; fixAttempt < maxFixRetries; fixAttempt++ {
				fmt.Printf("[AUTO-FIX] 🔧 自动修复 #%d/%d: %v\n", fixAttempt+1, maxFixRetries, fixErr)
				m.emitStreamChunk("se", fmt.Sprintf("🔧 **自动修复 #%d/%d**: %v\n", fixAttempt+1, maxFixRetries, fixErr))

				fixPrompt := fmt.Sprintf(`⚠️ EXECUTION FAILED - AUTO-REPAIR MODE (attempt %d/%d)

=== ERROR DETAILS ===
%v

=== YOUR LAST ACTIONS THAT FAILED ===
%v

=== REPAIR INSTRUCTIONS ===
Analyze the error and fix it:

🔴 COMMON ERRORS AND FIXES:
1. "path outside work directory" → You used absolute path (E:\...). NEVER use absolute paths!
   Fix: path field must be ONLY the filename: "main.go", "hello.go", etc.
2. "unknown command" from go → You forgot the "run" keyword.
   Fix: "go run hello.go" NOT "go hello.go"
3. "function main is undeclared" / "undefined" → Missing package main or func main.
   Fix: Write COMPLETE file: package main + import "fmt" + func main() { fmt.Println("Hello") }
4. "syntax error" / "string not terminated" → Code has typos.
   Fix: Rewrite the file with clean, correct code.

CRITICAL RULES:
- path MUST be a simple filename like "main.go" — NEVER "E:\...\main.go"
- command MUST include "run": "go run main.go" — NEVER "go main.go"
- Write COMPLETE file content, not truncated
- Include an exec action to VERIFY your code

Generate corrected actions JSON (use ONLY relative filenames):
{"actions":[{"type":"write_file","path":"main.go","content":"package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"Hello\")\n}\n"},{"type":"exec","command":"go run main.go"}]}`, fixAttempt+1, maxFixRetries, fixErr, resp.Actions)

				retryCtx, retryCancel := context.WithTimeout(safeCtx, seTimeout)
				m.seProcessor.SetContext(retryCtx)

				respFix, fixProcErr := m.seProcessor.ProcessTaskWithTools(fixPrompt, func(delta string) {
					m.cMonitor.UpdateSeChunkTime()
					m.emitStreamChunk("se", delta)
				})
				retryCancel()

				if fixProcErr != nil {
					fmt.Printf("[AUTO-FIX] ⚠️ 修复调用失败: %v\n", fixProcErr)
					m.emitStreamChunk("se", fmt.Sprintf("⚠️ 修复调用失败: %v\n", fixProcErr))
					continue
				}

				if len(respFix.Actions) == 0 {
					fmt.Println("[AUTO-FIX] ⚠️ AI 未返回修复操作")
					m.emitStreamChunk("se", "⚠️ AI 未返回修复操作，尝试让SE自行处理...\n")
					break
				}

				fmt.Printf("[AUTO-FIX] 🔧 执行修复操作 %d 个...\n", len(respFix.Actions))
				m.emitStreamChunk("se", fmt.Sprintf("🔧 执行修复操作 %d 个...\n", len(respFix.Actions)))

				if fixErr = m.executeSEActions(respFix.Actions); fixErr == nil {
					fmt.Println("[AUTO-FIX] ✅ 自动修复成功！")
					m.emitStreamChunk("se", "**✅ 自动修复成功！**\n")

					resp.Actions = append(resp.Actions, respFix.Actions...)
					if respFix.Content != "" {
						resp.Content += "\n" + respFix.Content
					}
					err = nil
					break
				}
			}

			if err != nil {
				failMsg := fmt.Sprintf("执行失败(已尝试%d次自动修复): %v", maxFixRetries, err)
				m.seProcessor.AddResult(failMsg)

				retryCtx, retryCancel := context.WithTimeout(safeCtx, seTimeout)
				m.seProcessor.SetContext(retryCtx)

				resp2, err2 := m.seProcessor.ProcessTaskWithTools(failMsg+"\\n请分析原因并决定下一步", func(delta string) {
					m.cMonitor.UpdateSeChunkTime()
					m.emitStreamChunk("se", delta)
				})
				retryCancel()
				if err2 != nil {
					m.cMonitor.UpdateSeStatus(types.RoleStatusIdle)

					m.boardManager.UpdateTask(i18n.T("msg.task_se_recover_failed"), 0)

					errMsg := fmt.Sprintf(i18n.T("err.se_recover_failed"), err2, err)
					m.addSEToPMMsg(errMsg)
					if m.onProjectStateChanged != nil {
						m.onProjectStateChanged("error")
					}
					return fmt.Errorf("SE process failed after action error: %w", err2)
				}

				// 添加SE回复到历史（自动加@PM）
				m.addSEToPMMsg(resp2.Content)

				// 检查SE是否需要PM帮助
				if resp2.NeedHelp {
					fmt.Println("[System] SE needs help after failure, asking PM...")
					return m.handleSEAskPM(resp2.Content)
				}

				// SE自己继续处理
				return m.continueSETask()
			}
		}

		// 如果执行成功但没有completed
		if resp.Completed == nil {
			fmt.Printf("[PROBE-startSETask] ✅ actions执行成功, Completed=nil, NeedHelp=%v (时间:%s)\n",
				resp.NeedHelp, time.Now().Format("15:04:05.000"))
			if resp.NeedHelp {
				fmt.Println("[System] SE actions done but still needs help, asking PM... [TAG-S1]")
				return m.handleSEAskPM(resp.Content)
			}
			scResult := m.seProcessor.CheckSemanticComplete(resp.Content)
			if scResult.IsComplete {
				fmt.Printf("[🛡️ 语义兜底] SE完成检测: confidence=%.2f reason=%s\n", scResult.Confidence, scResult.Reason)
				m.seReportedComplete = true
				m.currentRole = ""
				m.cMonitor.UpdatePmStatus(types.RoleStatusIdle)
				m.cMonitor.UpdateSeStatus(types.RoleStatusIdle)
				m.SetHandoverPending(HandoverSEToPM)
				m.cMonitor.UpdateProjectState(types.ProjectStateDone)
				m.syncBackendStatus("done", "SE语义完成，兜底路由到PM")
				summary := "✅ SE报告任务完成（语义检测），请审核"
			if from != "pm" {
				m.addSEToUserMsg(summary)
			}
			// [G53] 移除过早的exec_completed！原因：后续可能还有continueSETask()导致第2批SE actions
			// 前端收到此事件会重置所有SE消息的_streaming状态，导致后续SE chunk丢失
			// 正确的exec_completed应在最终完成时发送（TAG-D4路径或executeSEActions末尾）
			// runtime.EventsEmit(m.ctx, "exec_completed", map[string]interface{}{
			// 	"executor":  "se",
			// 	"result":    "",
			// 	"timestamp": time.Now().Unix(),
			// 	"content":   resp.Content,
			// })
			_, err := m.ProcessMessageFrom("se", "@PM ✅ 任务完成(语义检测)\n"+resp.Content) // [TAG-D3-RETURN]
			return err
			}
			fmt.Println("[System] SE未标记完成，继续执行 [TAG-S2]")
			return m.continueSETask()
		}
	}

	// ✅ Completed优先于NeedHelp：如果SE已标记完成，通过@层路由到PM
	if resp.Completed != nil {
		fmt.Printf("[TRACE-SE] ✅ SE标记Completed! → 走@层(Router)→PM | status=%s notes=%q (时间:%s) [TAG-S3]\n",
			resp.Completed.Status, resp.Completed.TechnicalNotes[:min(60, len(resp.Completed.TechnicalNotes))], time.Now().Format("15:04:05"))
		if m.seReportedComplete {
			fmt.Println("[DEBUG] SE已完成报告已发送过，跳过重复报告")
			m.currentRole = ""
			m.cMonitor.UpdateSeStatus(types.RoleStatusIdle)
			return nil
		}
		m.seReportedComplete = true

		m.currentRole = ""
		m.cMonitor.UpdatePmStatus(types.RoleStatusIdle)
		m.cMonitor.UpdateSeStatus(types.RoleStatusIdle)

		fmt.Println("[System] SE task completed, routing to PM via @layer (Router)...")

		m.SetHandoverPending(HandoverSEToPM)
		m.cMonitor.UpdateProjectState(types.ProjectStateDone)
		m.syncBackendStatus("done", "SE任务完成，路由到PM审核 [TAG-D4]")

		summary := "✅ 任务完成，文件在工作目录中，请用 read_file 审查"

		if from != "pm" {
			m.addSEToUserMsg(summary)
			fmt.Printf("[System] → 智能CC: from=%s, 额外通知来源角色\n", from)
		}

		m.msgBusSend("se", summary, "exec_completed", PathSEExec, "startSETaskWithFrom:completed", map[string]interface{}{
			"executor":  "se",
			"result":    "",
			"changelog": "",
			"status":    "completed",
		})

		fmt.Println("[System] → SE回复走 ProcessMessageFrom (@层Router) → PM")
		_, err := m.ProcessMessageFrom("se", summary)
		if err != nil {
			fmt.Printf("[System] ⚠️ SE→PM路由失败: %v\n", err)
			errMsg := fmt.Sprintf("@USR ❌ PM审核失败: %s\n\n请检查API配置或网络连接后重试。", err)
			m.msgBusSend("system", errMsg, "error", PathSystem, "startSETaskWithFrom:pm_error", map[string]interface{}{
				"error": err.Error(),
				"stage": "se_to_pm_routing",
			})
			m.addPMToUserMsg(errMsg)
		}
		return err
	}

	// 检查SE是否需要帮助（对于没有actions的情况）
	if resp.NeedHelp {
		fmt.Println("[System] SE needs help, asking PM...")
		m.syncBackendStatus("se_need_help", "SE请求PM帮助")
		if from != "pm" {
			m.addSEToUserMsg(i18n.T("msg.se_needs_help", resp.Content))
		}
		return m.handleSEAskPM(resp.Content)
	}

	// SE未完成且不需要帮助
	{
		fmt.Printf("[TRACE-SE] 🔄 SE无Completed/NeedHelp | actions=%d content_len=%d (时间:%s)\n",
			len(resp.Actions), len(resp.Content), time.Now().Format("15:04:05"))

		if len(resp.Actions) == 0 && resp.Content != "" {
			fmt.Println("[System] 🔑 SE无新actions但有内容 → 走@层(Router)→PM，不再死循环")
			m.currentRole = ""
			m.cMonitor.UpdateSeStatus(types.RoleStatusIdle)

			m.SetHandoverPending(HandoverSEToPM)
		m.cMonitor.UpdateProjectState(types.ProjectStateDone)
		m.syncBackendStatus("se_content_only", "SE有内容无操作，路由到PM [TAG-D5]")

			seContent := strings.TrimSpace(resp.Content)

			if strings.HasPrefix(seContent, "{") && strings.Contains(seContent, `"actions"`) {
				fmt.Printf("[G51-FIX] 🧹 检测到SE返回JSON格式，清理后发送给PM\n")
				cleanContent := m.cleanSEJSONContent(seContent)
				if cleanContent != "" {
					seContent = cleanContent
				}
			}

			if from != "pm" && len(seContent) > 200 {
				m.addSEToUserMsg(seContent[:200] + "...")
			}

			fmt.Printf("[System] → SE回复走 ProcessMessageFrom (@层Router) → PM | content_head=%q\n",
				resp.Content[:min(80, len(resp.Content))])
			_, err := m.ProcessMessageFrom("se", seContent)
			return err
		}

		if len(resp.Actions) > 0 {
			progressMsg := fmt.Sprintf(i18n.T("msg.se_progress"), len(resp.Actions))
			fmt.Printf("[DEBUG] 发送SE进度消息: %s\n", progressMsg)
			m.addSEToPMMsg(progressMsg)
		}
	}

	// 继续SE任务
	return m.continueSETask()
}

// continueSETask 继续SE任务
func (m *Manager) continueSETask() (err error) {
	fmt.Printf("[PROBE-continueSETask] 🟢 入口: seContinueCount=%d seReportedComplete=%v currentRole=%q (时间:%s)\n",
		m.seContinueCount, m.seReportedComplete, m.currentRole, time.Now().Format("15:04:05.000"))
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("[continueSETask] 💥 panic recovered: %v\n", r)
			err = fmt.Errorf("panic: %v", r)
			m.cMonitor.UpdateSeStatus(types.RoleStatusIdle)
		}
	}()
	m.seContinueCount++
	if m.seContinueCount > 10 {
		fmt.Printf("[System] ⚠️ SE继续次数超限(%d>10)，强制结束任务\n", m.seContinueCount)

		m.currentRole = ""
		m.cMonitor.UpdateSeStatus(types.RoleStatusIdle)

		errMsg := "❌ SE执行超限: 任务无法在20轮内完成，可能需要人工介入"
		m.addSEToPMMsg(errMsg)

		if m.onProjectStateChanged != nil {
			m.onProjectStateChanged("error")
		}
		return fmt.Errorf("SE continue limit exceeded")
	}

	// ⏰ SE继续执行超时45秒 + 最多重试1次
	safeCtx := context.Background()
	m.mu.RLock()
	if m.ctx != nil {
		safeCtx = m.ctx
	}
	m.mu.RUnlock()

	const seContinueTimeout = 90 * time.Second
	seCtx, seCancel := context.WithTimeout(safeCtx, seContinueTimeout)
	defer seCancel()
	m.seProcessor.SetContext(seCtx)

	if m.aiClient != nil {
		m.aiClient.CloseIdleConnections()
		fmt.Println("[continueSETask] 🔧 关闭空闲连接，避免复用死连接")
	}

	fmt.Printf("[PROBE-continueSETask] ⏳ 即将调用ProcessTaskWithTools(继续) (时间:%s)\n",
		time.Now().Format("15:04:05.000"))
	resp, err := m.seProcessor.ProcessTaskWithTools("继续", func(delta string) {
		m.cMonitor.UpdateSeChunkTime()
		m.emitStreamChunk("se", delta)
	})
	fmt.Printf("[PROBE-continueSETask] ⏹️ ProcessTaskWithTools返回: err=%v actions=%d content_len=%d (时间:%s)\n",
		err, func() int { if resp != nil { return len(resp.Actions) }; return 0 }(),
		func() int { if resp != nil { return len(resp.Content) }; return 0 }(),
		time.Now().Format("15:04:05.000"))
	if err != nil {
		m.cMonitor.UpdateSeStatus(types.RoleStatusIdle)
		errMsg := fmt.Sprintf("❌ SE继续执行失败: %v", err)
		m.addSEToPMMsg(errMsg)

		if strings.Contains(err.Error(), "connection") ||
			strings.Contains(err.Error(), "forcibly closed") ||
			strings.Contains(err.Error(), "read stream") {
			fmt.Println("[continueSETask] 🔄 网络错误，等待3秒后重试...")
			time.Sleep(3 * time.Second)
			retryCtx, retryCancel := context.WithTimeout(safeCtx, 90*time.Second)
			defer retryCancel()
			m.seProcessor.SetContext(retryCtx)
			resp, err = m.seProcessor.ProcessTaskWithTools("继续", func(delta string) {
				m.cMonitor.UpdateSeChunkTime()
				m.emitStreamChunk("se", delta)
			})
			if err == nil {
				fmt.Println("[continueSETask] ✅ 重试成功!")
				goto continueProcess
			}
			fmt.Printf("[continueSETask] ❌ 重试也失败: %v\n", err)
		}

		if m.onProjectStateChanged != nil {
			m.onProjectStateChanged("error")
		}
		return fmt.Errorf("SE continue failed: %w", err)
	}

continueProcess:
	// ❌ 删除中间 addHistory（不发"继续"消息，只保留最终结果）
	fmt.Printf("[SE] 继续处理中...\n")

	// 检查SE是否需要PM帮助
	if resp.NeedHelp {
		fmt.Println("[System] SE needs help, asking PM...")
		return m.handleSEAskPM(resp.Content)
	}

	// 执行actions
	if len(resp.Actions) > 0 {
		if err := m.executeSEActions(resp.Actions); err != nil {
			// 执行失败，通知SE，让SE决定如何处理
			failMsg := fmt.Sprintf("执行失败: %v", err)
			m.seProcessor.AddResult(failMsg)

			// SE处理失败情况，可能需要问PM
			resp2, err2 := m.seProcessor.ProcessTaskWithTools("上述执行失败，请分析原因并决定下一步", func(delta string) {
				m.cMonitor.UpdateSeChunkTime()
				m.emitStreamChunk("se", delta)
			})
			if err2 != nil {
				m.cMonitor.UpdateSeStatus(types.RoleStatusIdle)

				m.boardManager.UpdateTask(i18n.T("msg.task_se_recover_failed"), 0)
				errMsg := fmt.Sprintf(i18n.T("err.se_recover_failed"), err2)
				m.addSEToPMMsg(errMsg)
				if m.onProjectStateChanged != nil {
					m.onProjectStateChanged("error")
				}
				return fmt.Errorf("SE process failed after action error: %w", err2)
			}

			// 添加SE回复到历史
			seMsg2 := Message{
				From:    "se",
				To:      "pm",
				Role:    "se",
				Content: resp2.Content,
				Raw:     resp2.Content,
				Source:  "se_to_pm",
			}
			m.addHistory(seMsg2)
			fmt.Printf("[SE→PM] %s\n", resp2.Content)

			// 检查SE是否需要PM帮助
			if resp2.NeedHelp {
				fmt.Println("[System] SE needs help after failure, asking PM...")
				return m.handleSEAskPM(resp2.Content)
			}

			// SE自己继续处理，但不递归调用continueSETask
			return nil
		}

		// ✅ 汇总执行结果（不发中间消息）
		fmt.Printf("[SE] Actions executed successfully\n")

		// SE任务完成，切换到PM进行审核（走@层Router）
		fmt.Printf("[PROBE-continueSETask] 🔀 TAG-C1: actions完成，路由到PM (时间:%s)\n",
			time.Now().Format("15:04:05.000"))
		m.seReportedComplete = true  // [FIX-20260528-E] 补充缺失的状态设置
		m.currentRole = ""
		m.cMonitor.UpdateSeStatus(types.RoleStatusIdle)
		m.SetHandoverPending(HandoverSEToPM)  // [FIX-20260528-E]
		m.cMonitor.UpdateProjectState(types.ProjectStateDone)  // [FIX-20260528-E]
		m.syncBackendStatus("done", "SE任务完成(continue)，路由到PM [TAG-C1]")  // [FIX-20260528-E]

		// [G53] 在最终完成路径发送exec_completed（只发一次！）
		if m.ctx != nil {
			m.msgBusSend("se", "completed", "exec_completed", PathSEExec, "executeSEActions:TAG-C1", map[string]interface{}{
				"executor":  "se",
				"result":    "completed",
				"status":    "completed",
				"timestamp": time.Now().Unix(),
			})
		}

		_, err := m.ProcessMessageFrom("se", "SE已完成任务执行，请审核结果") // [TAG-C1]
		return err
	}

	// 如果完成了 - 通过@层路由发送
	if resp.Completed != nil {
		m.seReportedComplete = true  // [FIX-20260528-E] 补充缺失的状态设置
		m.currentRole = ""

		m.cMonitor.UpdatePmStatus(types.RoleStatusIdle)
		m.cMonitor.UpdateSeStatus(types.RoleStatusIdle)
		m.SetHandoverPending(HandoverSEToPM)  // [FIX-20260528-E]
		m.cMonitor.UpdateProjectState(types.ProjectStateDone)  // [FIX-20260528-E]
		fmt.Printf("[PROBE-continueSETask] 🔀 TAG-C2: Completed分支，路由到PM (时间:%s)\n",
			time.Now().Format("15:04:05.000"))

		summary := "✅ 任务完成\n\n"
		summary += fmt.Sprintf("📝 技术笔记:\n%s\n\n", resp.Completed.TechnicalNotes)
		summary += fmt.Sprintf("📋 变更日志:\n%s", resp.Completed.ChangelogDraft)

		fmt.Println("[System] SE完成(continueSETask) → 走 ProcessMessageFrom (@层Router) → PM")
		_, err := m.ProcessMessageFrom("se", summary)
		return err
	}

	if resp.Content != "" {
		fmt.Printf("[PROBE-continueSETask] 🔀 TAG-C3: 有内容无actions，路由到PM content_len=%d (时间:%s)\n",
			len(resp.Content), time.Now().Format("15:04:05.000"))
		m.seReportedComplete = true  // [FIX-20260528-E] 补充缺失的状态设置
		m.currentRole = ""
		m.cMonitor.UpdateSeStatus(types.RoleStatusIdle)
		m.SetHandoverPending(HandoverSEToPM)  // [FIX-20260528-E]
		m.cMonitor.UpdateProjectState(types.ProjectStateDone)  // [FIX-20260528-E]
		_, err := m.ProcessMessageFrom("se", strings.TrimSpace(resp.Content))
		return err
	}

	fmt.Printf("[PROBE-continueSETask] 🔀 TAG-C4: 回复空且无actions，结束 (时间:%s)\n",
		time.Now().Format("15:04:05.000"))
	m.seReportedComplete = true  // [FIX-20260528-E] 即使空回复也算完成
	m.currentRole = ""
	m.cMonitor.UpdateSeStatus(types.RoleStatusIdle)
	m.SetHandoverPending(HandoverSEToPM)  // [FIX-20260528-E]
	m.cMonitor.UpdateProjectState(types.ProjectStateDone)  // [FIX-20260528-E]
	return nil
}

// [v0.7.2] emitWailsEvent 改为走 MessageBus（不再走破路 runtime.EventsEmit）
func (m *Manager) emitWailsEvent(eventName string, data interface{}) {
	m.msgBusSend("se", "", eventName, PathSEExec, "emitWailsEvent:"+eventName, data)
	m.pushSSEEvent(eventName, data)
}

// [G63] msgBusSend 通过MessageBus发送消息（强制送水+校验）
func (m *Manager) msgBusSend(role, content, eventName string, path MessagePath, sourceLoc string, data interface{}) string {
	fmt.Printf("[G63-DEBUG] msgBusSend: role=%s event=%s path=%s source=%s msgBus=%v enabled=%v mb_ctx=%v mgr_ctx=%v\n",
		role, eventName, path, sourceLoc,
		m.msgBus != nil,
		m.msgBus != nil && m.msgBus.enabled,
		m.msgBus != nil && m.msgBus.ctx != nil,
		m.ctx != nil)

	var msgId string
	if m.msgBus != nil && m.msgBus.enabled {
		msgId = m.msgBus.Send(role, content, eventName, path, sourceLoc, data)
		if m.msgBus.ctx == nil {
			fmt.Printf("[G63-WARN] msgBus.ctx is nil, using manager.ctx fallback for event=%s\n", eventName)
		}
	}

	emitCtx := m.ctx
	if m.msgBus != nil && m.msgBus.ctx != nil {
		emitCtx = m.msgBus.ctx
	}

	if emitCtx != nil {
		enrichedData := map[string]interface{}{
			"_msgId": msgId,
			"_role":  role,
			"_path":  string(path),
		}
		if d, ok := data.(map[string]interface{}); ok {
			for k, v := range d {
				enrichedData[k] = v
			}
		} else if d, ok := data.(map[string]string); ok {
			for k, v := range d {
				enrichedData[k] = v
			}
		} else if data != nil {
			enrichedData["data"] = data
		}
		runtime.EventsEmit(emitCtx, eventName, enrichedData)
		fmt.Printf("[💧MSG-EMIT] event=%s role=%s ctx_ok=%v keys=%v\n", eventName, role, emitCtx != nil, getMapKeys(enrichedData))
	}

	m.pushSSEEvent(eventName, data)
	return msgId
}

// isReadOnlyAction 判断是否为无副作用只读操作（可并行执行）
func isReadOnlyAction(actionType string) bool {
	switch actionType {
	case "read_file", "search_files", "search_snippet", "show_diff":
		return true
	}
	return false
}

// executeReadOnlyBatch 并行执行一批只读操作（read_file / search_files）
func (m *Manager) executeReadOnlyBatch(batch []ai.SEAction, startIdx, totalActions int, seTaskId string) error {
	type batchResult struct {
		idx     int
		content string
		err     error
	}

	var wg sync.WaitGroup
	results := make([]batchResult, len(batch))

	for bi, action := range batch {
		wg.Add(1)
		go func(bi int, action ai.SEAction) {
			defer wg.Done()

			m.emitWailsEvent("exec_start", map[string]interface{}{
				"executor": "se", "index": startIdx + bi + 1, "total": totalActions,
				"type": action.Type, "label": action.Type + " " + action.Path,
				"path": action.Path, "status": "running",
			})

			switch action.Type {
			case "read_file":
				result, err := m.seExecutor.ReadFile(action.Path)
				if err != nil {
					results[bi] = batchResult{idx: bi, err: err}
				} else {
					results[bi] = batchResult{idx: bi, content: result}
				}

			case "search_files":
			var opts []executor.SearchOption
			if action.IsRegex {
				opts = append(opts, executor.WithRegex())
			}
			if action.CaseInsensitive {
				opts = append(opts, executor.WithCaseInsensitive())
			}
			if action.FilePattern != "" {
				opts = append(opts, executor.WithFilePattern(action.FilePattern))
			}
			searchResult, err := m.seExecutor.SearchFiles(action.Pattern, opts...)
			if err != nil {
				results[bi] = batchResult{idx: bi, err: err}
			} else if searchResult.Error != "" {
				results[bi] = batchResult{idx: bi, err: fmt.Errorf("%s", searchResult.Error)}
			} else {
				results[bi] = batchResult{idx: bi, content: formatSearchResult(searchResult)}
			}

			case "search_snippet":
				store := m.seProcessor.GetSnippetStore()
				snippets := store.SearchSimple(action.Pattern)
				results[bi] = batchResult{idx: bi, content: store.FormatResults(snippets)}
			}
		}(bi, action)
	}

	wg.Wait()

	// 按顺序反馈结果（保持 SE context 一致性）
	for bi, r := range results {
		action := batch[bi]
		if r.err != nil {
			errMsg := fmt.Sprintf("❌ %s: %v", action.Type, r.err)
			m.seProcessor.AddResult(errMsg)
			m.emitWailsEvent("exec_done", map[string]interface{}{
				"executor": "se", "index": startIdx + bi + 1, "type": action.Type,
				"label": action.Type + " " + action.Path, "status": "error", "error": errMsg,
			})
		} else {
			if action.Type == "read_file" {
				m.seProcessor.AddResult(fmt.Sprintf("文件内容 [%s]:\n%s", action.Path, r.content))
			} else {
				m.seProcessor.AddResult(fmt.Sprintf("搜索结果:\n%s", r.content))
			}
			m.emitWailsEvent("exec_done", map[string]interface{}{
				"executor": "se", "index": startIdx + bi + 1, "type": action.Type,
				"label": action.Type + " " + action.Path, "status": "done",
			})
		}
	}

	return nil
}

func formatSearchResult(r *executor.SearchFilesResult) string {
	if r == nil || r.TotalMatches == 0 {
		return "未找到匹配内容"
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("找到 %d 处匹配（搜索 %d 个文件）:\n", r.TotalMatches, r.FilesSearched))
	for _, m := range r.Matches {
		sb.WriteString(fmt.Sprintf("  %s:%d: %s\n", m.File, m.Line, m.Content))
	}
	return sb.String()
}

// executeSEActions 执行SE的actions
func (m *Manager) executeSEActions(actions []ai.SEAction) error {
	fmt.Printf("[PROBE-executeSEActions] 🟢 入口: actions=%d (时间:%s)\n",
		len(actions), time.Now().Format("15:04:05.000"))

	// [G-FIX] Action重排序：write_file必须在同文件exec之前执行
	// LLM经常先返回exec再返回write_file，导致文件不存在错误
	actions = m.reorderActions(actions)

	// [G-FIX] Action类型规范化：LLM经常返回错误的tool name
	actions = m.normalizeActionTypes(actions)

	func() {
		f, _ := os.OpenFile(filepath.Join(os.TempDir(), "argus_actions_probe.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if f != nil {
			for i, a := range actions {
				f.WriteString(fmt.Sprintf("[%s] ACTION[%d/%d] type=%q path=%q command=%q content_len=%d tool=%q\n",
					time.Now().Format("15:04:05.000"), i+1, len(actions), a.Type, a.Path, a.Command, len(a.Content), a.Tool))
			}
			f.Close()
		}
	}()
	
	totalActions := len(actions)
	seTaskId := ""
	if m.richBuilder != nil {
		seTaskId = m.richBuilder.GetCurrentTaskID()
		if seTaskId != "" && totalActions > 0 {
			taskDefs := make([]types.TaskItemDef, totalActions)
			for i, a := range actions {
				label := ""
				switch a.Type {
				case "write_file":
					label = "创建 " + a.Path
				case "edit_file":
					label = "修改 " + a.Path
				case "exec":
					label = "执行 " + a.Command
				case "read_file":
					label = "读取 " + a.Path
				case "search_files":
					label = "搜索 " + a.Pattern
				case "git_operation":
					if a.GitAction != "" {
						label = "Git " + a.GitAction
					} else {
						label = "Git 操作"
					}
				case "run_tests":
					label = "运行测试"
				case "undo_file":
					label = "撤销 " + a.Path
				case "list_changes":
					label = "列出变更记录"
				case "go_to_definition":
					label = "跳转定义: " + filepath.Base(a.Path)
				case "find_references":
					label = "查找引用: " + filepath.Base(a.Path)
				case "hover_info":
					label = "悬停信息: " + filepath.Base(a.Path)
				case "diagnostics":
					label = "诊断: " + filepath.Base(a.Path)
				case "rename_symbol":
					label = "重命名: " + a.Command
				case "analyze_code":
					label = "分析代码: " + a.Path
				case "auto_debug":
					label = "自动调试"
				case "search_snippet":
					label = "搜索片段"
				case "add_snippet":
					label = "添加片段"
				case "list_snippets":
					label = "列出片段"
				case "delete_snippet":
					label = "删除片段"
				default:
					label = a.Type
				}
				taskDefs[i] = types.TaskItemDef{Text: label}
			}
			m.richBuilder.ReplaceTaskList(seTaskId, taskDefs)
			m.richBuilder.UpdateTask(seTaskId, 0, "running")
		}
	}

	fmt.Printf("[PROBE-executeSEActions] 🔄 开始action循环: total=%d (时间:%s)\n",
		totalActions, time.Now().Format("15:04:05.000"))
	for i, action := range actions {
		actionLabel := fmt.Sprintf("%s %s", action.Type, func() string {
			switch action.Type {
			case "write_file":
				return action.Path
			case "exec":
				return action.Command
			case "read_file":
				return action.Path
			case "search_files":
				return action.Pattern
			case "git_operation":
				if action.GitAction != "" {
					return action.GitAction
				}
				return ""
			case "run_tests":
				if action.TestPattern != "" {
					return action.TestPattern
				}
				return "./..."
			default:
				return ""
			}
		}())

		fmt.Printf("[PROBE-executeSEActions] ▶️ action[%d/%d]: type=%s label=%q (时间:%s)\n",
			i+1, totalActions, action.Type, actionLabel, time.Now().Format("15:04:05.000"))

		var currentTask *types.GlobalTask
		desc := fmt.Sprintf("%s%s", map[string]string{
			"write_file":    "创建文件：",
			"edit_file":     "修改文件：",
			"exec":          "执行命令：",
			"read_file":     "读取文件：",
			"search_files":  "搜索文件：",
			"git_operation": "Git 操作：",
			"run_tests":     "运行测试：",
		}[action.Type], actionLabel)
		currentTask = m.taskManager.CreateTask(desc, "SE")

		m.emitWailsEvent("exec_start", map[string]interface{}{
			"executor": "se",
			"index":    i + 1,
			"total":    totalActions,
			"type":     action.Type,
			"label":    actionLabel,
			"path":     action.Path,
			"command":  action.Command,
			"status":   "running",
		})

		func() {
			f, _ := os.OpenFile(filepath.Join(os.TempDir(), "argus_actions_probe.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			if f != nil {
				f.WriteString(fmt.Sprintf("[%s] SWITCH-ENTER action[%d/%d] type=%q\n", time.Now().Format("15:04:05.000"), i+1, totalActions, action.Type))
				f.Close()
			}
		}()

		// 🆕 并行批处理：连续只读操作（read_file/search_files/list_files/glob/web_search）并发执行
		if isReadOnlyAction(action.Type) {
			batchSize := 1
			for j := i + 1; j < len(actions) && isReadOnlyAction(actions[j].Type) && batchSize < 5; j++ {
				batchSize++
			}
			if batchSize > 1 {
				fmt.Printf("[PARALLEL] 🚀 批处理 %d 个只读操作: idx %d-%d\n", batchSize, i, i+batchSize-1)
				batch := actions[i : i+batchSize]
				if err := m.executeReadOnlyBatch(batch, i, totalActions, seTaskId); err != nil {
					fmt.Printf("[PARALLEL] ❌ 批处理失败: %v\n", err)
					return err
				}
				i += batchSize - 1 // 跳过已处理项（for 循环会 +1）
				continue
			}
		}

		switch action.Type {
		case "write_file":
			if _, _, allowed := m.CheckPermission("write", action.Path); !allowed {
				errMsg := fmt.Sprintf("权限拒绝: 无权限写入 %s", action.Path)
				fmt.Printf("[Action] 🚫 %s\n", errMsg)
				m.seProcessor.AddResult(fmt.Sprintf("❌ %s", errMsg))
				m.emitWailsEvent("exec_done", map[string]interface{}{
					"executor": "se",
					"index":    i + 1,
					"type":     "write_file",
					"label":    actionLabel,
					"status":   "blocked",
					"error":    errMsg,
				})
				if currentTask != nil {
					m.taskManager.UpdateStatus(currentTask.ID, "failed")
				}
				continue
			}
			// [TRUNCATION-GUARD] 拒绝写入异常短的代码文件（防止LLM输出截断导致无效代码）
			if len(action.Content) > 0 && len(action.Content) < 30 {
				ext := strings.ToLower(filepath.Ext(action.Path))
				codeExts := map[string]bool{".go": true, ".py": true, ".js": true, ".ts": true, ".java": true, ".c": true, ".cpp": true, ".rs": true}
				if codeExts[ext] {
					errMsg := fmt.Sprintf("内容截断检测: %s 内容仅 %d 字节，代码文件不可能这么短！请重新生成完整代码。", action.Path, len(action.Content))
					fmt.Printf("[Action] ⚠️ %s\n", errMsg)
					fmt.Printf("[Action] ⚠️ 截断内容预览: %q\n", action.Content)
					m.seProcessor.AddResult(fmt.Sprintf("⚠️ %s", errMsg))
					m.emitWailsEvent("exec_done", map[string]interface{}{
						"executor": "se",
						"index":    i + 1,
						"type":     "write_file",
						"label":    actionLabel,
						"status":   "truncated",
						"error":    errMsg,
						"content_len": len(action.Content),
					})
					if currentTask != nil {
						m.taskManager.UpdateStatus(currentTask.ID, "failed")
					}
					continue
				}
			}
			// [P1] Diff预览：编辑前计算diff推送到前端
			oldContent, _ := os.ReadFile(filepath.Join(m.workDir, action.Path))
			if diff := executor.ComputeDiff(action.Path, string(oldContent), action.Content); diff != "" {
				m.emitWailsEvent("diff_preview", map[string]interface{}{
					"type":   "write_file",
					"path":   action.Path,
					"diff":   diff,
					"action": "before_write",
				})
			} else if len(oldContent) == 0 {
				m.emitWailsEvent("diff_preview", map[string]interface{}{
					"type":   "write_file",
					"path":   action.Path,
					"diff":   fmt.Sprintf("+++ 新建文件: %s (%d bytes)", action.Path, len(action.Content)),
					"action": "new_file",
				})
			}
			// [P1-2.5] 编辑前快照
			m.fileTracker.Snapshot(action.Path, "write")
			if err := m.seExecutor.WriteFile(action.Path, action.Content); err != nil {
				errMsg := fmt.Sprintf("写入文件 %s 失败: %v", action.Path, err)
				fmt.Printf("[Action] ❌ %s\n", errMsg)
				m.seProcessor.AddResult(fmt.Sprintf("❌ %s", errMsg))
				m.emitWailsEvent("exec_done", map[string]interface{}{
					"executor": "se",
					"index":    i + 1,
					"type":     "write_file",
					"label":    actionLabel,
					"status":   "error",
					"error":    errMsg,
				})
				m.emitWailsEvent("exec_output", map[string]interface{}{
					"executor":  "se",
					"command":   "",
					"output":    errMsg,
					"exit_code": -1,
				})
				if currentTask != nil {
					m.taskManager.UpdateStatus(currentTask.ID, "failed")
				}
				continue
			}
			fmt.Printf("[Action] Write file: %s (%d bytes)\n", action.Path, len(action.Content))

			// [G-FIX] Go文件语法预检：写入后立即验证，避免执行失败
			if strings.HasSuffix(action.Path, ".go") && len(action.Content) > 10 {
				absPath := filepath.Join(m.workDir, action.Path)

				// [G-FIX] 修复 _test.go 文件名（go run 不允许执行 _test.go 文件）
				if strings.Contains(filepath.Base(action.Path), "_test.") || strings.Contains(filepath.Base(action.Path), "_test") {
					newName := strings.Replace(filepath.Base(action.Path), "_test", "_app", 1)
					newPath := filepath.Join(filepath.Dir(action.Path), newName)
					fmt.Printf("[G-FIX] 🔧 重命名 %s → %s (避免_test.go限制)\n", action.Path, newPath)
					action.Path = newPath
					absPath = filepath.Join(m.workDir, action.Path)
				}

				// 先尝试修复常见LLM生成错误
				repaired := m.repairGoContent(action.Content)
				if repaired != action.Content {
					fmt.Printf("[G-FIX] 🔧 Go代码已修复: %s (%d→%d chars)\n", action.Path, len(action.Content), len(repaired))
					// 用修复后的内容重写
					m.seExecutor.WriteFile(action.Path, repaired)
				}

				if syntaxErr := m.validateGoSyntax(absPath); syntaxErr != "" {
					fmt.Printf("[G-FIX] ⚠️ Go语法错误: %s → %s\n", action.Path, syntaxErr)
					m.seProcessor.AddResult(fmt.Sprintf("⚠️ 文件 %s 存在Go语法错误: %s\n建议使用 undo_file 撤销或修复后重写", action.Path, syntaxErr))
					// [P0-2] 自动回滚：语法错误时恢复到编辑前状态
					if m.fileTracker != nil {
						if rolledBack, rbMsg := m.fileTracker.RollbackLast(action.Path); rolledBack {
							fmt.Printf("[Auto-Rollback] ↩️ 已自动回滚 %s: %s\n", action.Path, rbMsg)
							m.seProcessor.AddResult(fmt.Sprintf("🔄 已自动回滚 %s 到编辑前状态，SE可重新编写\n", action.Path))
						}
					}
				} else {
					fmt.Printf("[G-FIX] ✅ Go语法检查通过: %s\n", action.Path)
				}
			}

			m.emitWailsEvent("exec_done", map[string]interface{}{
				"executor": "se",
				"index":    i + 1,
				"type":     "write_file",
				"label":    actionLabel,
				"status":   "done",
			})
			if currentTask != nil {
				m.taskManager.UpdateStatus(currentTask.ID, "done")
			}

		case "edit_file":
			if _, _, allowed := m.CheckPermission("edit", action.Path); !allowed {
				errMsg := fmt.Sprintf("权限拒绝: 无权限编辑 %s", action.Path)
				fmt.Printf("[Action] 🚫 %s\n", errMsg)
				m.seProcessor.AddResult(fmt.Sprintf("❌ %s", errMsg))
				m.emitWailsEvent("exec_done", map[string]interface{}{
					"executor": "se",
					"index":    i + 1,
					"type":     "edit_file",
					"label":    actionLabel,
					"status":   "blocked",
					"error":    errMsg,
				})
				if currentTask != nil {
					m.taskManager.UpdateStatus(currentTask.ID, "failed")
				}
				continue
			}
			// [P1] Diff预览：编辑前计算diff推送到前端
			if oldBytes, err := os.ReadFile(filepath.Join(m.workDir, action.Path)); err == nil {
				newContent := strings.Replace(string(oldBytes), action.OldStr, action.NewStr, 1)
				if diff := executor.ComputeDiff(action.Path, string(oldBytes), newContent); diff != "" {
					m.emitWailsEvent("diff_preview", map[string]interface{}{
						"type":   "edit_file",
						"path":   action.Path,
						"diff":   diff,
						"action": "before_edit",
					})
				}
			}
			// [P1-2.5] 编辑前快照
			m.fileTracker.Snapshot(action.Path, "edit")

			editResult, err := m.seExecutor.EditFile(action.Path, action.OldStr, action.NewStr)
			if err != nil {
				errMsg := fmt.Sprintf("编辑文件 %s 失败: %v", action.Path, err)
				fmt.Printf("[Action] ❌ %s\n", errMsg)
				m.seProcessor.AddResult(fmt.Sprintf("❌ %s", errMsg))
				m.emitWailsEvent("exec_done", map[string]interface{}{
					"executor": "se",
					"index":    i + 1,
					"type":     "edit_file",
					"label":    actionLabel,
					"status":   "error",
					"error":    errMsg,
				})
				if currentTask != nil {
					m.taskManager.UpdateStatus(currentTask.ID, "failed")
				}
				return fmt.Errorf("edit file failed: %v", err)
			}

			if editResult.Success {
				diffPreview := editResult.Diff
				if len(diffPreview) > 300 {
					diffPreview = diffPreview[:300] + "\n... (diff 已截断)"
				}

				resultMsg := fmt.Sprintf("✅ 编辑成功: %s\n📝 修改行数: %d\n%s",
					action.Path,
					editResult.LinesChanged,
					diffPreview)

				fmt.Printf("[Action] ✅ EditFile: %s (%d lines changed)\n", 
					action.Path, editResult.LinesChanged)
				
				m.seProcessor.AddResult(resultMsg)

				m.emitWailsEvent("exec_done", map[string]interface{}{
					"executor":      "se",
					"index":         i + 1,
					"type":          "edit_file",
					"label":         actionLabel,
					"status":        "done",
					"lines_changed": editResult.LinesChanged,
					"diff":          diffPreview,
				})

				m.emitWailsEvent("exec_output", map[string]interface{}{
					"executor":  "se",
					"command":   fmt.Sprintf("edit_file(%s)", action.Path),
					"output":    resultMsg,
					"exit_code": 0,
				})

				if currentTask != nil {
					m.taskManager.UpdateStatus(currentTask.ID, "done")
				}
			} else {
				errMsg := fmt.Sprintf("编辑失败: %s", editResult.Error)
				fmt.Printf("[Action] ❌ EditFile failed: %s\n", editResult.Error)
				m.seProcessor.AddResult(fmt.Sprintf("❌ %s", errMsg))
				// [P0-2] 编辑失败时自动回滚
				if m.fileTracker != nil {
					if rolledBack, rbMsg := m.fileTracker.RollbackLast(action.Path); rolledBack {
						fmt.Printf("[Auto-Rollback] ↩️ 已自动回滚编辑失败的 %s: %s\n", action.Path, rbMsg)
						m.seProcessor.AddResult(fmt.Sprintf("🔄 已自动回滚 %s 到编辑前状态\n", action.Path))
					}
				}

				m.emitWailsEvent("exec_done", map[string]interface{}{
					"executor": "se",
					"index":    i + 1,
					"type":     "edit_file",
					"label":    actionLabel,
					"status":   "error",
					"error":    editResult.Error,
				})

				if currentTask != nil {
					m.taskManager.UpdateStatus(currentTask.ID, "failed")
				}
			}

		case "search_files":
			var searchOpts []executor.SearchOption
			if action.FilePattern != "" {
				searchOpts = append(searchOpts, executor.WithFilePattern(action.FilePattern))
			}
			if action.IsRegex {
				searchOpts = append(searchOpts, executor.WithRegex())
			}
			if action.CaseInsensitive {
				searchOpts = append(searchOpts, executor.WithCaseInsensitive())
			}
			if action.Path != "" {
				searchOpts = append(searchOpts, executor.WithPath(action.Path))
			}

			searchResult, err := m.seExecutor.SearchFiles(action.Pattern, searchOpts...)
			if err != nil {
				errMsg := fmt.Sprintf("搜索失败: %v", err)
				m.seProcessor.AddResult(fmt.Sprintf("❌ %s", errMsg))
				m.emitWailsEvent("exec_done", map[string]interface{}{
					"executor": "se",
					"index":    i + 1,
					"type":     "search_files",
					"label":    actionLabel,
					"status":   "error",
					"error":    errMsg,
				})
				continue
			}

			if searchResult.Error != "" {
				m.seProcessor.AddResult(fmt.Sprintf("❌ 搜索错误: %s", searchResult.Error))
			} else {
				resultMsg := fmt.Sprintf("🔍 搜索 '%s': 找到 %d 个匹配 (搜索了 %d 个文件)\n",
					action.Pattern, searchResult.TotalMatches, searchResult.FilesSearched)
				for _, match := range searchResult.Matches {
					resultMsg += fmt.Sprintf("  → %s:%d:%d  %s\n", match.File, match.Line, match.Column, match.Content)
				}
				m.seProcessor.AddResult(resultMsg)

				m.emitWailsEvent("exec_output", map[string]interface{}{
					"executor":  "se",
					"command":   fmt.Sprintf("search_files('%s')", action.Pattern),
					"output":    resultMsg,
					"exit_code": 0,
				})
			}

			m.emitWailsEvent("exec_done", map[string]interface{}{
				"executor":      "se",
				"index":         i + 1,
				"type":          "search_files",
				"label":         actionLabel,
				"status":        "done",
				"total_matches": searchResult.TotalMatches,
			})

		case "git_operation":
			gitAction := action.GitAction
			if gitAction == "" {
				gitAction = "status"
			}
			gitResult, err := m.seExecutor.GitOperation(gitAction, action.GitMessage, action.GitArgs)
			if err != nil {
				errMsg := fmt.Sprintf("Git 操作失败: %v", err)
				m.seProcessor.AddResult(fmt.Sprintf("❌ %s", errMsg))
				m.emitWailsEvent("exec_done", map[string]interface{}{
					"executor": "se",
					"index":    i + 1,
					"type":     "git_operation",
					"label":    actionLabel,
					"status":   "error",
					"error":    errMsg,
				})
				continue
			}

			if !gitResult.Success || gitResult.Error != "" {
				m.seProcessor.AddResult(fmt.Sprintf("❌ git %s: %s\n", gitAction, gitResult.Error))
				m.emitWailsEvent("exec_done", map[string]interface{}{
					"executor": "se",
					"index":    i + 1,
					"type":     "git_operation",
					"label":    actionLabel,
					"status":   "error",
					"error":    gitResult.Error,
				})
				continue
			}

			resultMsg := fmt.Sprintf("🔀 git %s 成功\n", gitAction)
			switch gitAction {
			case "status":
				if gitResult.Status != nil {
					resultMsg += fmt.Sprintf("  分支: %s\n", gitResult.Status.Branch)
					if !gitResult.Status.IsClean {
						if len(gitResult.Status.Staged) > 0 {
							resultMsg += fmt.Sprintf("  已暂存: %d 个文件\n", len(gitResult.Status.Staged))
						}
						if len(gitResult.Status.Modified) > 0 {
							resultMsg += fmt.Sprintf("  已修改: %d 个文件\n", len(gitResult.Status.Modified))
						}
						if len(gitResult.Status.Untracked) > 0 {
							resultMsg += fmt.Sprintf("  未跟踪: %d 个文件\n", len(gitResult.Status.Untracked))
						}
					} else {
						resultMsg += "  工作区干净 ✅\n"
					}
				}
			case "diff":
				if gitResult.Diff != "" {
					lines := strings.Split(gitResult.Diff, "\n")
					if len(lines) > 50 {
						resultMsg += strings.Join(lines[:50], "\n")
						resultMsg += fmt.Sprintf("\n  ... (共 %d 行，已截断)\n", len(lines))
					} else {
						resultMsg += gitResult.Diff + "\n"
					}
				} else {
					resultMsg += "  (无差异)\n"
				}
			case "log":
				if len(gitResult.Log) > 0 {
					for _, entry := range gitResult.Log {
						resultMsg += fmt.Sprintf("  %s  %s\n", entry.Hash, entry.Message)
					}
				}
			case "commit":
				resultMsg += fmt.Sprintf("  提交: %s\n", action.GitMessage)
			default:
				if gitResult.Output != "" {
					outputLines := strings.Split(gitResult.Output, "\n")
					if len(outputLines) > 10 {
						resultMsg += strings.Join(outputLines[:10], "\n")
						resultMsg += fmt.Sprintf("\n  ... (共 %d 行)\n", len(outputLines))
					} else {
						resultMsg += gitResult.Output + "\n"
					}
				}
			}
			m.seProcessor.AddResult(resultMsg)

			m.emitWailsEvent("exec_output", map[string]interface{}{
				"executor":  "se",
				"command":   fmt.Sprintf("git %s", gitAction),
				"output":    resultMsg,
				"exit_code": 0,
			})

			m.emitWailsEvent("exec_done", map[string]interface{}{
				"executor":  "se",
				"index":     i + 1,
				"type":      "git_operation",
				"label":     actionLabel,
				"status":    "done",
				"git_action": gitAction,
			})

		case "run_tests":
			testConfig := executor.TestConfig{
				Pattern:  action.TestPattern,
				Coverage: action.TestCoverage,
				Verbose:  action.TestVerbose,
			}
			testReport, err := m.seExecutor.RunTests(testConfig)
			if err != nil {
				errMsg := fmt.Sprintf("测试运行失败: %v", err)
				m.seProcessor.AddResult(fmt.Sprintf("❌ %s", errMsg))
				m.emitWailsEvent("exec_done", map[string]interface{}{
					"executor": "se",
					"index":    i + 1,
					"type":     "run_tests",
					"label":    actionLabel,
					"status":   "error",
					"error":    errMsg,
				})
				continue
			}

			resultMsg := fmt.Sprintf("🧪 测试结果: %s\n", func() string {
				if testReport.Success {
					return fmt.Sprintf("✅ 全部通过 (%d/%d)", testReport.Passed, testReport.Total)
				}
				return fmt.Sprintf("❌ 失败 %d/%d (通过 %d, 跳过 %d)", testReport.Failed, testReport.Total, testReport.Passed, testReport.Skipped)
			}())
			if testReport.Duration != "" {
				resultMsg += fmt.Sprintf("  耗时: %s\n", testReport.Duration)
			}
			if testReport.Coverage != "" {
				resultMsg += fmt.Sprintf("  覆盖率: %s\n", testReport.Coverage)
			}

			if len(testReport.Cases) > 0 {
				resultMsg += "\n"
				for _, tc := range testReport.Cases {
					icon := "✅"
					if tc.Status == "fail" || tc.Status == "panic" {
						icon = "❌"
					} else if tc.Status == "skip" {
						icon = "⏭️"
					}
					resultMsg += fmt.Sprintf("  %s %s (%s)", icon, tc.Name, tc.Duration)

					// [v0.7.1] 结构化失败信息
					if (tc.Status == "fail" || tc.Status == "panic") && tc.File != "" {
						resultMsg += fmt.Sprintf(" [%s:%d]", tc.File, tc.Line)
					}
					resultMsg += "\n"

					if tc.Error != "" {
						errLines := strings.Split(tc.Error, "\n")
						if len(errLines) > 3 {
							resultMsg += fmt.Sprintf("      %s\n      ... (共%d行)\n", errLines[0], len(errLines))
						} else {
							for _, el := range errLines {
								resultMsg += fmt.Sprintf("      %s\n", el)
							}
						}
					}

					// [v0.7.1] 显示断言详情
					if tc.AssertionType != "" {
						resultMsg += fmt.Sprintf("      断言: %s", tc.AssertionType)
						if tc.Expected != "" {
							resultMsg += fmt.Sprintf(" | 期望: %s", tc.Expected)
						}
						if tc.Actual != "" {
							resultMsg += fmt.Sprintf(" | 实际: %s", tc.Actual)
						}
						resultMsg += "\n"
					}
				}
			}
			m.seProcessor.AddResult(resultMsg)

			m.emitWailsEvent("exec_output", map[string]interface{}{
				"executor":  "se",
				"command":   fmt.Sprintf("go test %s", action.TestPattern),
				"output":    resultMsg,
				"exit_code": func() int { if testReport.Success { return 0 }; return 1 }(),
				"test_total":   testReport.Total,
				"test_passed":  testReport.Passed,
				"test_failed":  testReport.Failed,
				"test_coverage": testReport.Coverage,
			})

			m.emitWailsEvent("exec_done", map[string]interface{}{
				"executor":     "se",
				"index":        i + 1,
				"type":         "run_tests",
				"label":        actionLabel,
				"status":       func() string { if testReport.Success { return "done" }; return "failed" }(),
				"test_success": testReport.Success,
			})

		case "exec":
			if m.configManager != nil {
				level, desc := m.configManager.CheckCommand(action.Command)
				if level == types.CmdBlockDeny {
					errMsg := fmt.Sprintf("命令被安全策略拒绝: %s (%s)", action.Command, desc)
					fmt.Printf("[Action] 🚫 %s\n", errMsg)
					m.seProcessor.AddResult(fmt.Sprintf("❌ %s", errMsg))
					m.emitWailsEvent("exec_done", map[string]interface{}{
						"executor": "se",
						"index":    i + 1,
						"type":     "exec",
						"label":    actionLabel,
						"status":   "blocked",
						"error":    errMsg,
					})
					m.emitWailsEvent("exec_output", map[string]interface{}{
						"executor":  "se",
						"command":   action.Command,
						"output":    errMsg,
						"exit_code": -1,
						"blocked":   true,
					})
					if currentTask != nil {
						m.taskManager.UpdateStatus(currentTask.ID, "failed")
					}
					continue
				}
				if level == types.CmdBlockAsk {
					needConfirm, ruleDesc, _ := m.CheckDecision(types.DecisionCmdExecute)
					_ = ruleDesc
					if !needConfirm {
						errMsg := fmt.Sprintf("危险命令需人工确认: %s (%s)", action.Command, desc)
						fmt.Printf("[Action] ⚠️ %s\n", errMsg)
						m.seProcessor.AddResult(fmt.Sprintf("⚠️ %s", errMsg))
						m.emitWailsEvent("exec_done", map[string]interface{}{
							"executor": "se",
							"index":    i + 1,
							"type":     "exec",
							"label":    actionLabel,
							"status":   "blocked",
							"error":    errMsg,
						})
						m.emitWailsEvent("exec_output", map[string]interface{}{
							"executor":  "se",
							"command":   action.Command,
							"output":    errMsg,
							"exit_code": -1,
							"blocked":   true,
						})
						if currentTask != nil {
							m.taskManager.UpdateStatus(currentTask.ID, "failed")
						}
						continue
					}
				}
			}

			// 🆕 [FIX-20260529] exec命令预检：自动修复常见错误
			originalCommand := action.Command
			fixedCommand := action.Command

			// 2.1 修复 "go run hello" → "go run hello.go"
			if strings.HasPrefix(fixedCommand, "go run ") && !strings.HasSuffix(fixedCommand, ".go") {
				parts := strings.Fields(fixedCommand)
				if len(parts) >= 3 && !strings.Contains(parts[2], ".") {
					fixedCommand = "go run " + parts[2] + ".go"
					fmt.Printf("[EXEC-PRECHECK] 🔄 修复go命令: %q → %q\n", originalCommand, fixedCommand)
					action.Command = fixedCommand
				}
			}

			// 2.2 修复 "go hello.go" → "go run hello.go"
			if strings.HasPrefix(fixedCommand, "go ") && !strings.HasPrefix(fixedCommand, "go run") && strings.HasSuffix(fixedCommand, ".go") {
				fixedCommand = strings.Replace(fixedCommand, "go ", "go run ", 1)
				fmt.Printf("[EXEC-PRECHECK] 🔄 补全run: %q → %q\n", originalCommand, fixedCommand)
				action.Command = fixedCommand
			}

			if seTaskId != "" && m.richBuilder != nil {
				m.richBuilder.PushShellStart("se", seTaskId, 1, "exec", action.Command, nil)
			}
			output, err := m.seExecutor.Exec(action.Command, 30*time.Second)
			if seTaskId != "" && m.richBuilder != nil {
				m.richBuilder.PushShellOutput(seTaskId, output)
			}
			if err != nil {
				errMsg := fmt.Sprintf("执行失败: %v", err)

				execResult := &executor.ExecutionResult{
					Command:  action.Command,
					Stdout:   "",
					Stderr:   output + "\n" + errMsg,
					Success:  false,
					ExitCode: -1,
				}

				errorAnalysis := executor.AnalyzeError(execResult)
				if errorAnalysis != nil {
					formattedError := executor.FormatErrorForSE(errorAnalysis)
					errMsg = formattedError
					fmt.Printf("[P0-ErrorAnalysis] 🔍 检测到错误 [%s]: %s\n",
						errorAnalysis.Type, errorAnalysis.Message)
				}

				m.emitWailsEvent("exec_done", map[string]interface{}{
					"executor": "se",
					"index":    i + 1,
					"type":     "exec",
					"label":    actionLabel,
					"status":   "error",
					"error":    errMsg,
				})
				m.emitWailsEvent("exec_output", map[string]interface{}{
					"executor":  "se",
					"command":   action.Command,
					"output":    output + "\n" + errMsg,
					"exit_code": -1,
				})
				if currentTask != nil {
					m.taskManager.UpdateStatus(currentTask.ID, "failed")
				}
				return fmt.Errorf("exec failed: %v, output: %s", err, output)
			}
			fmt.Printf("[Action] Exec: %s\nOutput: %s\n", action.Command, output)
			if m.envMemory != nil {
				m.envMemory.LearnFromCommand(action.Command)
			}

			successResult := &executor.ExecutionResult{
				Command:  action.Command,
				Stdout:   output,
				Success:  true,
				ExitCode: 0,
			}
			successAnalysis := executor.AnalyzeError(successResult)
			if successAnalysis == nil {
				m.seProcessor.AddResult(fmt.Sprintf("执行结果:\n%s", output))
			} else {
				fmt.Printf("[P0-ErrorAnalysis] ⚠️ 成功但检测到警告: %s\n", successAnalysis.Type)
				m.seProcessor.AddResult(fmt.Sprintf("执行结果:\n%s\n⚠️ 警告: %s", 
					output, successAnalysis.Message))
			}

			m.emitWailsEvent("exec_done", map[string]interface{}{
				"executor": "se",
				"index":    i + 1,
				"type":     "exec",
				"label":    actionLabel,
				"status":   "done",
			})
			if currentTask != nil {
				m.taskManager.UpdateStatus(currentTask.ID, "done")
			}
			if seTaskId != "" && m.richBuilder != nil {
				m.richBuilder.PushShellDone("se", seTaskId, 0, "", "done")
			}
			m.emitWailsEvent("exec_output", map[string]interface{}{
			"executor":  "se",
			"command":   action.Command,
			"output":    truncateSSEOutput(output, 500),
			"exit_code": 0,
		})

	case "debug_run":
		// 调试运行：自动加调试flag，格式化panic/trace，60s超时
		if m.configManager != nil {
			level, desc := m.configManager.CheckCommand(action.Command)
			if level == types.CmdBlockDeny {
				errMsg := fmt.Sprintf("命令被安全策略拒绝: %s (%s)", action.Command, desc)
				m.seProcessor.AddResult(fmt.Sprintf("❌ %s", errMsg))
				continue
			}
			_ = desc
		}

		// 智能增强Go命令
		cmd := action.Command
		if strings.HasPrefix(cmd, "go test") || strings.Contains(cmd, "go test") {
			if !strings.Contains(cmd, "-v") {
				cmd += " -v"
			}
			if !strings.Contains(cmd, "-count") {
				cmd += " -count=1"
			}
		} else if strings.HasPrefix(cmd, "go run") || strings.HasPrefix(cmd, "go build") {
			if !strings.Contains(cmd, "-race") {
				cmd += " -race"
			}
		}
		fmt.Printf("[DebugRun] 🔍 %s\n", cmd)

		output, err := m.seExecutor.Exec(cmd, 60*time.Second)
		if err != nil {
			m.seProcessor.AddResult(fmt.Sprintf("❌ debug运行失败: %v\n输出: %s", err, output))
			continue
		}

		// 格式化panic/trace为结构化展示
		result := fmt.Sprintf("🔍 Debug输出:\n%s", output)
		if strings.Contains(output, "panic:") {
			result = fmt.Sprintf("🔍 💥 PANIC检测:\n%s\n\n📋 建议: 检查panic行号，注意nil指针、索引越界、类型断言", output)
		} else if strings.Contains(output, "FAIL") || strings.Contains(output, "FAIL") {
			result = fmt.Sprintf("🔍 ❌ 测试失败:\n%s\n\n📋 建议: 查看失败用例输出，检查断言和预期值", output)
		}
		m.seProcessor.AddResult(result)
		m.emitWailsEvent("exec_done", map[string]interface{}{
			"executor": "se", "index": i + 1, "type": "debug_run",
			"label": actionLabel, "status": "done",
		})
		m.emitWailsEvent("exec_output", map[string]interface{}{
			"executor": "se", "command": cmd,
			"output": truncateSSEOutput(output, 500), "exit_code": 0,
		})

	case "exec_session":
		// 持久化 shell 会话执行（保持 cd/env 状态）
		if m.configManager != nil {
			level, desc := m.configManager.CheckCommand(action.Command)
			if level == types.CmdBlockDeny {
				errMsg := fmt.Sprintf("命令被安全策略拒绝: %s (%s)", action.Command, desc)
				m.seProcessor.AddResult(fmt.Sprintf("❌ %s", errMsg))
				m.emitWailsEvent("exec_done", map[string]interface{}{
					"executor": "se", "index": i + 1, "type": "exec_session",
					"label": actionLabel, "status": "blocked", "error": errMsg,
				})
				if currentTask != nil { m.taskManager.UpdateStatus(currentTask.ID, "failed") }
				continue
			}
		}

		sessionOutput, sessionErr := m.seExecutor.ExecWithSession(action.Command, 60*time.Second)
		if sessionErr != nil {
			errMsg := fmt.Sprintf("执行失败: %v", sessionErr)
			fmt.Printf("[Action] ❌ exec_session: %s\n", errMsg)
			m.seProcessor.AddResult(fmt.Sprintf("❌ %s\n输出:\n%s", errMsg, sessionOutput))
			m.emitWailsEvent("exec_done", map[string]interface{}{
				"executor": "se", "index": i + 1, "type": "exec_session",
				"label": actionLabel, "status": "error", "error": errMsg,
			})
			m.emitWailsEvent("exec_output", map[string]interface{}{
				"executor": "se", "command": action.Command,
				"output": truncateSSEOutput(sessionOutput, 500), "exit_code": -1,
			})
			if currentTask != nil { m.taskManager.UpdateStatus(currentTask.ID, "failed") }
			continue
		}

		fmt.Printf("[Action] exec_session: %s → %s\n", action.Command, strings.TrimSpace(sessionOutput)[:min(100, len(strings.TrimSpace(sessionOutput)))])
		m.seProcessor.AddResult(fmt.Sprintf("执行结果:\n%s", sessionOutput))
		m.emitWailsEvent("exec_done", map[string]interface{}{
			"executor": "se", "index": i + 1, "type": "exec_session",
			"label": actionLabel, "status": "done",
		})
		if currentTask != nil { m.taskManager.UpdateStatus(currentTask.ID, "done") }
		m.emitWailsEvent("exec_output", map[string]interface{}{
			"executor": "se", "command": action.Command,
			"output": truncateSSEOutput(sessionOutput, 500), "exit_code": 0,
		})

		case "check_env":
			fmt.Printf("[Action] Check env: %s\n", action.Tool)
			m.seProcessor.AddResult(fmt.Sprintf("环境检查: %s 可用", action.Tool))
			m.emitWailsEvent("exec_done", map[string]interface{}{
				"executor": "se",
				"index":    i + 1,
				"type":     "check_env",
				"label":    action.Tool,
				"status":   "done",
			})

		case "search_snippet":
			query := action.Pattern
			if query == "" {
				query = action.Tool
			}
			store := m.seProcessor.GetSnippetStore()
			snippets := store.SearchSimple(query)
			resultMsg := store.FormatResults(snippets)
			m.seProcessor.AddResult(resultMsg)
			m.emitWailsEvent("exec_done", map[string]interface{}{
				"executor": "se",
				"index":    i + 1,
				"type":     "search_snippet",
				"label":    fmt.Sprintf("search_snippet('%s')", query),
				"status":   "done",
				"matches":  len(snippets),
			})

		case "add_snippet":
			store := m.seProcessor.GetSnippetStore()
			newSn := ai.Snippet{
				Name:        action.Pattern, // name
				Language:    action.Path,    // language
				Description: action.Content, // description
			}
			// Parse tags from extra if available
			if action.Extra != "" {
				var extra struct {
					Tags []string `json:"tags"`
					Code string   `json:"code"`
				}
				if json.Unmarshal([]byte(action.Extra), &extra) == nil {
					newSn.Tags = extra.Tags
					newSn.Code = extra.Code
				}
			}
			if err := store.Add(newSn); err != nil {
				m.seProcessor.AddResult(fmt.Sprintf("❌ 添加片段失败: %v", err))
			} else {
				m.seProcessor.AddResult(fmt.Sprintf("✅ 片段已添加: %s (%s)", newSn.Name, newSn.Language))
			}
			m.emitWailsEvent("exec_done", map[string]interface{}{
				"executor": "se", "index": i + 1, "type": "add_snippet",
				"label": fmt.Sprintf("add_snippet('%s')", newSn.Name), "status": "done",
			})

		case "list_snippets":
			store := m.seProcessor.GetSnippetStore()
			lang := action.Pattern // optional language filter
			list := store.List(lang)
			resultMsg := store.FormatList(list)
			m.seProcessor.AddResult(resultMsg)
			m.emitWailsEvent("exec_done", map[string]interface{}{
				"executor": "se", "index": i + 1, "type": "list_snippets",
				"label": "list_snippets()", "status": "done", "count": len(list),
			})

		case "delete_snippet":
			store := m.seProcessor.GetSnippetStore()
			snippetID := action.Pattern
			if err := store.Delete(snippetID); err != nil {
				m.seProcessor.AddResult(fmt.Sprintf("❌ 删除片段失败: %v", err))
			} else {
				m.seProcessor.AddResult(fmt.Sprintf("✅ 片段已删除: %s", snippetID))
			}
			m.emitWailsEvent("exec_done", map[string]interface{}{
				"executor": "se", "index": i + 1, "type": "delete_snippet",
				"label": fmt.Sprintf("delete_snippet('%s')", snippetID), "status": "done",
			})

		case "analyze_code":
			analyzer := ai.NewCodeAnalyzer(m.workDir)
			opts := ai.AnalyzeOptions{
				Path:     action.Path,
				MaxIssues: 50,
			}
			// 解析 Extra 中的过滤参数
			if action.Extra != "" {
				var extra struct {
					Categories []string `json:"categories"`
					MinLevel   string   `json:"min_level"`
					MaxIssues  int      `json:"max_issues"`
				}
				if json.Unmarshal([]byte(action.Extra), &extra) == nil {
					opts.Categories = extra.Categories
					opts.MinLevel = extra.MinLevel
					if extra.MaxIssues > 0 {
						opts.MaxIssues = extra.MaxIssues
					}
				}
			}
			result, err := analyzer.Analyze(opts)
			if err != nil {
				m.seProcessor.AddResult(fmt.Sprintf("❌ 代码分析失败: %v", err))
			} else {
				report := result.FormatResults()
				m.seProcessor.AddResult(report)
			}
			m.emitWailsEvent("exec_done", map[string]interface{}{
				"executor": "se", "index": i + 1, "type": "analyze_code",
				"label": fmt.Sprintf("analyze_code('%s')", action.Path), "status": "done",
			})

		case "auto_debug":
			// 解析参数
			maxIter := 3
			mode := "full"
			if action.Extra != "" {
				var extra struct {
					MaxIterations int    `json:"max_iterations"`
					Mode          string `json:"mode"`
				}
				if json.Unmarshal([]byte(action.Extra), &extra) == nil {
					if extra.MaxIterations > 0 && extra.MaxIterations <= 5 {
						maxIter = extra.MaxIterations
					}
					if extra.Mode == "analyze" || extra.Mode == "full" {
						mode = extra.Mode
					}
				}
			}
			testCmd := action.Command
			if testCmd == "" {
				testCmd = "go test -v -count=1"
			}
			target := action.Path // 测试目标包路径
			if target != "" {
				testCmd = fmt.Sprintf("%s %s", testCmd, target)
			}

			if mode == "analyze" {
				// 仅分析模式：跑一次测试，AI分析错误但不动代码
				output, err := m.seExecutor.Exec(testCmd, 60*time.Second)
				if err != nil && output == "" {
					m.seProcessor.AddResult(fmt.Sprintf("❌ 测试执行失败: %v", err))
				} else {
					debugger := ai.NewAutoDebugger(ai.DefaultAutoDebugConfig(m.workDir), m.seProcessor.GetClient(), m.seExecutor.Exec)
					analysis, _, analyzeErr := debugger.AnalyzeOnly(context.Background(), output)
					if analyzeErr != nil {
						m.seProcessor.AddResult(fmt.Sprintf("❌ 分析失败: %v\n\n原始输出:\n%s", analyzeErr, ai.TruncateOutput(output, 3000)))
					} else {
						m.seProcessor.AddResult(fmt.Sprintf("📋 测试失败分析:\n%s\n\n原始输出:\n%s", analysis, ai.TruncateOutput(output, 2000)))
					}
				}
				m.emitWailsEvent("exec_done", map[string]interface{}{
					"executor": "se", "index": i + 1, "type": "auto_debug",
					"label": fmt.Sprintf("auto_debug(analyze '%s')", target), "status": "done",
				})
			} else {
				// full 模式：跑测试 + AI分析 + 自动修复循环
				config := ai.DefaultAutoDebugConfig(m.workDir)
				config.MaxIterations = maxIter
				if target != "" {
					config.SpecificTests = target
				}
				config.TestCommand = "go test -v -count=1"
				if testCmd != "" {
					config.TestCommand = strings.TrimSuffix(testCmd, " "+target)
				}

				debugger := ai.NewAutoDebugger(config, m.seProcessor.GetClient(), m.seExecutor.Exec)
				result, err := debugger.Run(context.Background())
				if err != nil {
					m.seProcessor.AddResult(fmt.Sprintf("❌ 自动调试失败: %v", err))
				} else {
					report := result.FormatResults()
					m.seProcessor.AddResult(report)

					// 如果有修复建议，记录到结果
					if len(result.FixHistory) > 0 {
						lastFix := result.FixHistory[len(result.FixHistory)-1]
						if lastFix.FilePath != "" && lastFix.NewCode != "" {
							m.seProcessor.AddResult(fmt.Sprintf("\n💡 最后修复建议 (%s):\n旧代码:\n```\n%s\n```\n新代码:\n```\n%s\n```",
								lastFix.FilePath, lastFix.OldCode, lastFix.NewCode))
						}
					}
				}
				m.emitWailsEvent("exec_done", map[string]interface{}{
					"executor": "se", "index": i + 1, "type": "auto_debug",
					"label": fmt.Sprintf("auto_debug('%s')", target), "status": "done",
					"success": result != nil && result.Success,
					"iterations": func() int { if result != nil { return result.Iterations }; return 0 }(),
				})
			}

		case "show_diff":
			absPath := filepath.Join(m.workDir, action.Path)
			oldBytes, err := os.ReadFile(absPath)
			if err != nil {
				m.seProcessor.AddResult(fmt.Sprintf("❌ 无法读取文件: %v", err))
				continue
			}
			diff := executor.ComputeDiff(action.Path, string(oldBytes), action.Content)
			if diff == "" {
				diff = "无变化"
			}
			m.seProcessor.AddResult(fmt.Sprintf("📊 Diff预览:\n%s", diff))
			m.emitWailsEvent("diff_preview", map[string]interface{}{
				"type": "show_diff",
				"path": action.Path,
				"diff": diff,
			})
			m.emitWailsEvent("exec_done", map[string]interface{}{
				"executor": "se",
				"index":    i + 1,
				"type":     "show_diff",
				"label":    fmt.Sprintf("show_diff(%s)", action.Path),
				"status":   "done",
			})

		case "read_file":
			if _, _, allowed := m.CheckPermission("read", action.Path); !allowed {
				errMsg := fmt.Sprintf("权限拒绝: 无权限读取 %s", action.Path)
				fmt.Printf("[Action] 🚫 %s\n", errMsg)
				m.seProcessor.AddResult(fmt.Sprintf("❌ %s", errMsg))
				m.emitWailsEvent("exec_done", map[string]interface{}{
					"executor": "se",
					"index":    i + 1,
					"type":     "read_file",
					"label":    actionLabel,
					"status":   "blocked",
					"error":    errMsg,
				})
				if currentTask != nil {
					m.taskManager.UpdateStatus(currentTask.ID, "failed")
				}
				continue
			}
			content, err := m.seExecutor.ReadFile(action.Path)
			if err != nil {
				m.emitWailsEvent("exec_done", map[string]interface{}{
					"executor": "se",
					"index":    i + 1,
					"type":     "read_file",
					"label":    actionLabel,
					"status":   "error",
					"error":    err.Error(),
				})
				if currentTask != nil {
					m.taskManager.UpdateStatus(currentTask.ID, "failed")
				}
				return fmt.Errorf("read file failed: %v", err)
			}
			fmt.Printf("[Action] Read file: %s (%d bytes)\n", action.Path, len(content))
			m.seProcessor.AddResult(fmt.Sprintf("文件内容 [%s]:\n%s", action.Path, content))

		case "undo_file":
			// [P0-2] 撤销文件编辑 — 回滚到最近一次快照
			if m.fileTracker == nil {
				m.seProcessor.AddResult("❌ 撤销失败: 文件追踪器未初始化")
				continue
			}
			ok, msg := m.fileTracker.RollbackLast(action.Path)
			if ok {
				m.seProcessor.AddResult(fmt.Sprintf("↩️ 已撤销 %s 的最近一次编辑: %s\n", action.Path, msg))
				fmt.Printf("[Undo] ✅ %s\n", msg)
			} else {
				m.seProcessor.AddResult(fmt.Sprintf("❌ 撤销 %s 失败: %s\n", action.Path, msg))
				fmt.Printf("[Undo] ❌ %s\n", msg)
			}

			m.emitWailsEvent("exec_done", map[string]interface{}{
				"executor": "se",
				"index":    i + 1,
				"type":     "undo_file",
				"label":    "撤销 " + action.Path,
				"status":   func() string { if ok { return "done" }; return "error" }(),
			})

		case "list_changes":
			// [P0-2] 列出所有可撤销的变更记录
			if m.fileTracker == nil {
				m.seProcessor.AddResult("❌ 文件追踪器未初始化")
				continue
			}
			changes := m.fileTracker.GetRecentChanges(20)
			if len(changes) == 0 {
				m.seProcessor.AddResult("📋 当前没有可撤销的变更记录")
			} else {
				result := fmt.Sprintf("📋 变更记录 (共%d条，最多保留%d步):\n", len(changes), m.fileTracker.Stats()["max_stack"])
				for j, c := range changes {
					result += fmt.Sprintf("  #%d [%s] %s (%d bytes, %s)\n",
						j+1, c.Action, c.Path, len(c.Content), c.Timestamp.Format("15:04:05"))
				}
				stats := m.fileTracker.Stats()
				result += fmt.Sprintf("\n统计: %d个文件被追踪\n", stats["files_tracked"])
				m.seProcessor.AddResult(result)
			}

			m.emitWailsEvent("exec_done", map[string]interface{}{
				"executor": "se",
				"index":    i + 1,
				"type":     "list_changes",
				"label":    "列出变更",
				"status":   "done",
			})

		// ========== [P0-1] LSP 工具执行 ==========
		case "go_to_definition":
			if m.lspClient == nil {
				m.seProcessor.AddResult("❌ LSP 不可用（gopls 未启动），无法使用 go_to_definition")
				continue
			}
			locs, err := m.lspClient.GoToDefinition(action.Path, action.Line, action.Column)
			if err != nil {
				m.seProcessor.AddResult(fmt.Sprintf("❌ GoToDefinition 失败: %v\n", err))
			} else {
				result := ai.FormatDefResult(locs)
				m.seProcessor.AddResult(fmt.Sprintf("🔗 GoToDefinition %s:%d:\n%s\n", action.Path, action.Line+1, result))
			}
			m.emitWailsEvent("exec_done", map[string]interface{}{
				"executor": "se", "index": i + 1, "type": "go_to_definition",
				"label": "定义: " + filepath.Base(action.Path),
				"status": func() string { if err != nil { return "error" }; return "done" }(),
			})

		case "find_references":
			if m.lspClient == nil {
				m.seProcessor.AddResult("❌ LSP 不可用，无法使用 find_references")
				continue
			}
			locs, err := m.lspClient.FindReferences(action.Path, action.Line, action.Column)
			if err != nil {
				m.seProcessor.AddResult(fmt.Sprintf("❌ FindReferences 失败: %v\n", err))
			} else {
				result := ai.FormatRefResult(locs)
				m.seProcessor.AddResult(fmt.Sprintf("🔍 FindReferences %s:%d:\n%s\n", action.Path, action.Line+1, result))
			}
			m.emitWailsEvent("exec_done", map[string]interface{}{
				"executor": "se", "index": i + 1, "type": "find_references",
				"label": "引用: " + filepath.Base(action.Path),
				"status": func() string { if err != nil { return "error" }; return "done" }(),
			})

		case "hover_info":
			if m.lspClient == nil {
				m.seProcessor.AddResult("❌ LSP 不可用，无法使用 hover_info")
				continue
			}
			info, err := m.lspClient.Hover(action.Path, action.Line, action.Column)
			if err != nil {
				m.seProcessor.AddResult(fmt.Sprintf("❌ Hover 失败: %v\n", err))
			} else if info == "" {
				m.seProcessor.AddResult("💡 无悬停信息\n")
			} else {
				m.seProcessor.AddResult(fmt.Sprintf("💡 Hover %s:%d:\n%s\n", action.Path, action.Line+1, info))
			}
			m.emitWailsEvent("exec_done", map[string]interface{}{
				"executor": "se", "index": i + 1, "type": "hover_info",
				"label": "Hover: " + filepath.Base(action.Path),
				"status": func() string { if err != nil { return "error" }; return "done" }(),
			})

		case "diagnostics":
			if m.lspClient == nil {
				m.seProcessor.AddResult("❌ LSP 不可用，无法使用 diagnostics")
				continue
			}
			diags, err := m.lspClient.Diagnostics(action.Path)
			if err != nil {
				m.seProcessor.AddResult(fmt.Sprintf("❌ Diagnostics 失败: %v\n", err))
			} else {
				result := ai.FormatDiagResult(diags)
				m.seProcessor.AddResult(fmt.Sprintf("📋 Diagnostics %s:\n%s\n", action.Path, result))
			}
			m.emitWailsEvent("exec_done", map[string]interface{}{
				"executor": "se", "index": i + 1, "type": "diagnostics",
				"label": "诊断: " + filepath.Base(action.Path),
				"status": func() string { if err != nil { return "error" }; return "done" }(),
			})

		case "rename_symbol":
			if m.lspClient == nil {
				m.seProcessor.AddResult("❌ LSP 不可用，无法使用 rename_symbol")
				continue
			}
			newName := action.Command // new_name 存在 Command 字段
			edit, err := m.lspClient.Rename(action.Path, action.Line, action.Column, newName)
			if err != nil {
				m.seProcessor.AddResult(fmt.Sprintf("❌ Rename 失败: %v\n", err))
			} else {
				changedFiles, applyErr := ai.ApplyWorkspaceEdit(edit, m.workDir)
				if applyErr != nil {
					m.seProcessor.AddResult(fmt.Sprintf("❌ Rename 计算成功但应用失败: %v\n", applyErr))
				} else {
					result := fmt.Sprintf("✅ 已重命名 → '%s'，修改了 %d 个文件:\n", newName, len(changedFiles))
					for _, f := range changedFiles {
						result += fmt.Sprintf("  📄 %s\n", f)
					}
					m.seProcessor.AddResult(result)
				}
			}
			m.emitWailsEvent("exec_done", map[string]interface{}{
				"executor": "se", "index": i + 1, "type": "rename_symbol",
				"label": "重命名: " + newName,
				"status": func() string { if err != nil { return "error" }; return "done" }(),
			})

		case "analyze_image":
			// [P0-3] 多模态图片分析
			prompt := action.Command
			imagePath := action.Path

			if !m.seProcessor.IsVisionModel() {
				m.seProcessor.AddResult("⚠️ 当前模型不支持 vision 能力，无法分析图片。\n建议切换到 GPT-4o / Claude-3 / Gemini 等支持视觉的模型。")
				m.emitWailsEvent("exec_done", map[string]interface{}{
					"executor": "se", "index": i + 1, "type": "analyze_image",
					"label": "图片分析 (不支持)",
					"status":  "error",
				})
				continue
			}

			visionResp, err := m.seProcessor.AnalyzeImage(imagePath, prompt)
			if err != nil {
				m.seProcessor.AddResult(fmt.Sprintf("❌ 图片分析失败: %v\n", err))
				m.emitWailsEvent("exec_done", map[string]interface{}{
					"executor": "se", "index": i + 1, "type": "analyze_image",
					"label": "图片分析",
					"status":  "error",
					"error":   err.Error(),
				})
				continue
			}

			if visionResp.Error != "" {
				m.seProcessor.AddResult(fmt.Sprintf("❌ %s\n", visionResp.Error))
			} else {
				resultMsg := fmt.Sprintf("🖼️ 图片分析结果 (%s):\n%s\n", filepath.Base(imagePath), visionResp.Description)
				if visionResp.Code != "" {
					resultMsg += fmt.Sprintf("\n📝 生成的代码:\n```%s```\n", visionResp.Code)
				}
				m.seProcessor.AddResult(resultMsg)
			}
			m.emitWailsEvent("exec_done", map[string]interface{}{
				"executor": "se", "index": i + 1, "type": "analyze_image",
				"label": "图片分析: " + filepath.Base(imagePath),
				"status": func() string { if visionResp.Error != "" || err != nil { return "error" }; return "done" }(),
			})

		default:
			// [v0.7.1] MCP 工具桥接：mcp__serverName__toolName 格式
			if strings.HasPrefix(action.Type, "mcp__") && m.mcpManager != nil {
				parts := strings.SplitN(strings.TrimPrefix(action.Type, "mcp__"), "__", 2)
				if len(parts) == 2 {
					serverName, toolName := parts[0], parts[1]
					// 从 Command 字段解析 JSON 参数
					var args map[string]interface{}
					if action.Command != "" {
						json.Unmarshal([]byte(action.Command), &args)
					}
					mcpResult, mcpErr := m.mcpManager.CallTool(serverName, toolName, args)
					if mcpErr != nil {
						mcpResultMsg := fmt.Sprintf("❌ MCP [%s:%s] 失败: %v\n", serverName, toolName, mcpErr)
						m.seProcessor.AddResult(mcpResultMsg)
						m.emitWailsEvent("exec_done", map[string]interface{}{
							"executor": "se", "index": i + 1, "type": "mcp_call",
							"label": fmt.Sprintf("MCP: %s.%s", serverName, toolName),
							"status": "error",
							"error":   mcpErr.Error(),
						})
					} else {
						var textParts []string
						for _, block := range mcpResult.Content {
							if block.Type == "text" && block.Text != "" {
								textParts = append(textParts, block.Text)
							}
						}
						icon := "✅"
						if mcpResult.IsError {
							icon = "⚠️"
						}
						mcpResultMsg := fmt.Sprintf("%s MCP [%s:%s]\n", icon, serverName, toolName)
						for _, t := range textParts {
							// 截断超长输出
							if len(t) > 2000 {
								t = t[:2000] + "... (truncated)"
							}
							mcpResultMsg += fmt.Sprintf("  %s\n", t)
						}
						m.seProcessor.AddResult(mcpResultMsg)
						m.emitWailsEvent("exec_done", map[string]interface{}{
							"executor": "se", "index": i + 1, "type": "mcp_call",
							"label": fmt.Sprintf("MCP: %s.%s", serverName, toolName),
							"status": func() string { if mcpResult.IsError { return "error" }; return "done" }(),
						})
					}
					break // 跳出 switch，继续下一个 action
				}
			}
			return fmt.Errorf("unknown action type: %s", action.Type)
		}
		
		func() {
			f, _ := os.OpenFile(filepath.Join(os.TempDir(), "argus_actions_probe.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			if f != nil {
				f.WriteString(fmt.Sprintf("[%s] SWITCH-DONE action[%d/%d] type=%q\n", time.Now().Format("15:04:05.000"), i+1, totalActions, action.Type))
				f.Close()
			}
		}()
	}
	if seTaskId != "" && m.richBuilder != nil {
		m.richBuilder.UpdateTask(seTaskId, 2, "done")
		m.richBuilder.CompleteTaskList(seTaskId, "done", nil)
	}

	if m.ctx != nil {
		// [G53] 移除executeSEActions中的exec_completed！
		// 原因：此函数可能被多次调用（如continueSETask场景），每次都发会导致前端过早重置_streaming
		// 正确做法：只在最终完成路径（TAG-D4/TAG-C1）发送一次exec_completed
		fmt.Println("[executeSEActions] ⚠️ [G53] 不在此处发送exec_completed，由调用方负责")
		// runtime.EventsEmit(m.ctx, "exec_completed", map[string]interface{}{
		// 	"executor":  "se",
		// 	"result":    "completed",
		// 	"status":    "completed",
		// 	"timestamp": time.Now().Unix(),
		// })
	} else {
		fmt.Println("[executeSEActions] ⚠️ m.ctx 为 nil，无法发送 exec_completed")
	}

	fmt.Printf("[PROBE-executeSEActions] ⏹️ 函数正常结束 (时间:%s)\n",
		time.Now().Format("15:04:05.000"))
	return nil
}

func truncateSSEOutput(output string, maxLen int) string {
	if len(output) > maxLen {
		return output[:maxLen] + "...(截断)"
	}
	return output
}

// addHistory 添加消息到历史
func (m *Manager) addHistory(msg Message) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if msg.Timestamp.IsZero() {
		msg.Timestamp = time.Now()
	}
	if msg.Source == "" {
		msg.Source = "unknown"
	}

	sourceInfo := ""
	if msg.Source != "" {
		sourceInfo = fmt.Sprintf(", Source=%s", msg.Source)
	}
	contentPreview := msg.Content
	if len(contentPreview) > 60 {
		contentPreview = contentPreview[:60] + "..."
	}
	fmt.Printf("[addHistory%s] #%d Role=%s From=%s To=%s Content='%s'\n",
		sourceInfo, len(m.history)+1, msg.Role, msg.From, msg.To, contentPreview)

	// 自动分配消息ID（如果未设置）
	if msg.ID == "" {
		m.msgCounter++
		msg.ID = fmt.Sprintf("msg_%d", m.msgCounter)
	}

	// 追踪每个角色的最后消息ID
	m.lastMsgIDs[msg.Role] = msg.ID

	m.history = append(m.history, msg)

	if len(m.history) > 200 {
		trimLen := len(m.history) - 200
		m.history = m.history[trimLen:]
	}

	// 写入对话日志文件
	m.writeConversationLog(msg)

	// 触发回调，推送到前端
	if m.onMessageAdded != nil {
		m.onMessageAdded(msg)
	}
}

// getLastMsgID 获取指定角色的最后消息ID（用于设置ReplyTo）
func (m *Manager) getLastMsgID(role string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.lastMsgIDs[role]
}

// getReplyToID 根据当前角色获取应该回复的消息ID
// PM回复SE/USR，SE回复PM，AP回复PM
func (m *Manager) getReplyToID(myRole string) string {
	switch myRole {
	case "pm":
		// PM优先回复SE，其次USR
		if id := m.getLastMsgID("se"); id != "" {
			return id
		}
		return m.getLastMsgID("user")
	case "se":
		// SE回复PM
		return m.getLastMsgID("pm")
	case "ap":
		// AP回复PM
		return m.getLastMsgID("pm")
	default:
		return ""
	}
}

// ensureSEToPM 确保SE消息带@PM前缀（代码层面强制，不依赖AI自觉）
func (m *Manager) ensureSEToPM(content string) string {
	if strings.HasPrefix(content, "@PM") {
		return content
	}
	return "@PM " + content
}

type ReviewResult struct {
	Result string
	Reason string
	Found  bool
}

func (m *Manager) extractReviewResult(content string) ReviewResult {
	lines := strings.Split(content, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if strings.HasPrefix(line, "{") && strings.HasSuffix(line, "}") {
			if strings.Contains(line, `"review_result"`) || strings.Contains(line, `"approval_result"`) {
				var result struct {
					ReviewResult   string `json:"review_result"`
					ApprovalResult string `json:"approval_result"`
					Reason         string `json:"reason"`
				}
				if err := json.Unmarshal([]byte(line), &result); err == nil {
					res := result.ReviewResult
					if res == "" {
						res = result.ApprovalResult
					}
					if res == "approve" || res == "reject" {
						return ReviewResult{Result: res, Reason: result.Reason, Found: true}
					}
				}
			}
		}
	}
	return ReviewResult{Found: false}
}

// aggregateAIMessage 聚合AI消息：过滤思考过程，只保留有效内容
// 检测并移除：
// 1. 英文思考段落（"Let me see", "First I need", "So the next step"等）
// 2. 过时的状态JSON（{"action":"update_state",...}）
// 返回清理后的有效内容
func (m *Manager) aggregateAIMessage(content string, role string) string {
	lines := strings.Split(content, "\n")
	var validLines []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if trimmed == "" {
			continue
		}

		// 跳过过时的状态JSON
		if strings.Contains(trimmed, `"action":"update_state"`) ||
			strings.Contains(trimmed, `{"action":"update_state"`) {
			fmt.Printf("[聚合过滤] 移除状态JSON: %s\n", trimmed[:min(80, len(trimmed))])
			continue
		}

		// 检测英文思考过程（小写开头 + 常见思考词）
		lowerLine := strings.ToLower(trimmed)
		isThinking := false

		thinkingPrefixes := []string{
			"okay, let me", "let me see", "first, i need",
			"so the ", "but according", "therefore,",
			"i need to check", "the assistant should",
			"now i ", "so now ", "but the ",
			"looking at ", "from the previous",
			"considering that ", "based on ",
			"the user is ", "the user says",
			"it seems ", "this suggests",
		}

		for _, prefix := range thinkingPrefixes {
			if strings.HasPrefix(lowerLine, prefix) {
				isThinking = true
				break
			}
		}

		if isThinking {
			fmt.Printf("[聚合过滤] 移除思考过程: %s...\n", trimmed[:min(50, len(trimmed))])
			continue
		}

		validLines = append(validLines, line)
	}

	result := strings.Join(validLines, "\n")
	result = strings.TrimSpace(result)

	return result
}

// addSEToPMMsg 发送SE→PM消息（自动加@PM）
// ✅ 静默模式：只保存到历史记录，不发送 new-message 事件（因为流式已显示）
func (m *Manager) addSEToPMMsg(content string) {
	content = strings.TrimSpace(content)
	if content == "" {
		fmt.Println("[SE→PM] ⚠️ 内容为空，跳过发送")
		return
	}

	re := regexp.MustCompile(`\{[^{}]*"actions"\s*:\s*\[[^\]]*\][^{}]*\}`)
	cleanContent := re.ReplaceAllString(content, "")
	cleanContent = strings.TrimSpace(cleanContent)
	if cleanContent == "" {
		cleanContent = content
	}

	content = m.ensureSEToPM(cleanContent)
	seMsg := Message{
		From:      "se",
		To:        "pm",
		Role:      "se",
		Content:   content,
		Raw:       content,
		Source:    "se_to_pm",
		Timestamp: time.Now(),
	}

	m.addHistory(seMsg)
	fmt.Printf("[SE→PM] %s\n", content)
}

// ensurePMToUSR 确保PM消息带@USR前缀，并清理多余@
func (m *Manager) ensurePMToUSR(content string) string {
	content = strings.TrimSpace(content)

	// 0. 清除开头的裸@ + 空格（AI输出 "@ 请创建..." 模式）
	re0 := regexp.MustCompile(`^@\s+`)
	content = re0.ReplaceAllString(content, "")
	content = strings.TrimSpace(content)

	re3 := regexp.MustCompile(`^@[A-Za-z]+\s+@[A-Z]{2,3}\s+`)
	content = re3.ReplaceAllString(content, "")
	content = strings.TrimSpace(content)

	re1 := regexp.MustCompile(`^@[A-Za-z]+\s+@\s*`)
	content = re1.ReplaceAllString(content, "")
	content = strings.TrimSpace(content)

	re2 := regexp.MustCompile(`^@[A-Za-z]+\s+[A-Z]{2,3}\s+`)
	content = re2.ReplaceAllString(content, "")
	content = strings.TrimSpace(content)

	if strings.HasPrefix(content, "@USR") {
		return content
	}
	return "@USR " + content
}

// buildCompressionSummary 生成压缩对话摘要
func (m *Manager) buildCompressionSummary(msgs []Message) string {
	var lines []string
	for _, msg := range msgs {
		role := msg.Role
		if role == "user" {
			role = "[用户]"
		} else if role == "pm" {
			role = "[PM]"
		} else if role == "se" {
			role = "[SE]"
		} else if role == "ap" {
			role = "[AP]"
		} else {
			role = "[" + role + "]"
		}
		content := msg.Content
		if len(content) > 80 {
			content = content[:80] + "..."
		}
		lines = append(lines, fmt.Sprintf("- %s %s", role, content))
	}
	return strings.Join(lines, "\n")
}

// addPMToUserMsg 发送PM→USR消息（自动加@USR）
// ✅ 静默模式：只保存到历史记录，不发送 new-message 事件（因为流式已显示）
func (m *Manager) addPMToUserMsg(content string) {
	content = strings.TrimSpace(content)
	if content == "" {
		fmt.Println("[PM→USR] ⚠️ 内容为空，跳过发送")
		return
	}

	content = m.ensurePMToUSR(content)

	content = m.aggregateAIMessage(content, "pm")

	pmMsg := Message{
		From:      "pm",
		To:        "user",
		Role:      "pm",
		Content:   content,
		Raw:       content,
		Source:    "pm_to_user",
		Timestamp: time.Now(),
		ReplyTo:   m.getReplyToID("pm"),
	}

	m.mu.Lock()
	m.history = append(m.history, pmMsg)
	if len(m.history) > 200 {
		trimLen := len(m.history) - 200
		m.history = m.history[trimLen:]
	}
	m.mu.Unlock()

	m.writeConversationLog(pmMsg)
	fmt.Printf("[PM→USR] %s\n", content)

	if m.onMessageAdded != nil {
		m.onMessageAdded(pmMsg)
	}

	m.msgBusSend("pm", content, "pm_message", PathPMToUser, "addPMToUserMsg", map[string]string{"delta": content})
	m.msgBusSend("pm", content, "pm_streaming_done", PathPMToUser, "addPMToUserMsg", map[string]interface{}{"content": content})

	isCompletion := strings.Contains(content, "✅") || strings.Contains(content, "完成") || strings.Contains(content, "已完成")
	asksForInput := strings.Contains(content, "?") || strings.Contains(content, "？") ||
		strings.Contains(content, "请确认") || strings.Contains(content, "请选择") ||
		strings.Contains(content, "请决定") || strings.Contains(content, "请回复")
	if asksForInput && !isCompletion {
		m.mu.Lock()
		m.pmWaitingForUserSince = time.Now().Unix()
		m.pmWaitingNudgeCount = 0
		m.mu.Unlock()
		fmt.Printf("[PM→USR] PM询问用户，开始等待USR决策, 时间戳: %d\n", time.Now().Unix())

		// 弹窗提醒用户PM在等回复
		if m.ctx != nil {
			go func() {
				runtime.MessageDialog(m.ctx, runtime.MessageDialogOptions{
					Type:    runtime.InfoDialog,
					Title:   "PM 请您确认",
					Message: "PM 需要您确认或回答，请查看聊天面板。",
				})
			}()
		}
	} else if !isCompletion {
		// 非询问消息（状态通知/信息告知）：清除等待状态，PM不等待USR
		m.mu.Lock()
		if m.pmWaitingForUserSince > 0 {
			fmt.Printf("[PM→USR] PM已回复（不等待），清除之前等待状态\n")
			m.pmWaitingForUserSince = 0
			m.pmWaitingNudgeCount = 0
		}
		m.mu.Unlock()
	} else {
		fmt.Println("[PM→USR] 任务完成消息，不设置等待状态")
		if m.memoryManager != nil {
			if err := m.memoryManager.ClearState(); err != nil {
				fmt.Printf("[PM→USR] ⚠️ 清除任务记忆失败: %v\n", err)
			} else {
				fmt.Println("[PM→USR] ✅ 任务记忆已清除，防止C监控恢复")
			}
		}
	}
	m.syncBackendStatus("pm_reply", "PM回复用户: "+content[:min(40, len(content))])
}

// extractLastSETask 从PM回复中提取最后一个@SE的任务内容
// 处理AI输出多个@SE的情况：取最后一个@SE作为真正任务，前面的部分作为给用户的回复
func extractLastSETask(content string) (taskContent, userReply string) {
	re := regexp.MustCompile(`@SE\s+`)
	matches := re.FindAllStringIndex(content, -1)

	if len(matches) <= 1 {
		re2 := regexp.MustCompile(`@SE\s+`)
		return strings.TrimSpace(re2.ReplaceAllString(content, "")), ""
	}

	lastMatch := matches[len(matches)-1]
	taskContent = strings.TrimSpace(content[lastMatch[0]:])
	taskRe := regexp.MustCompile(`@SE\s+`)
	taskContent = strings.TrimSpace(taskRe.ReplaceAllString(taskContent, ""))

	beforeLastSE := content[:lastMatch[0]]
	userReply = strings.TrimSpace(beforeLastSE)
	atRe := regexp.MustCompile(`@\w+\s+`)
	userReply = atRe.ReplaceAllString(userReply, "")
	userReply = strings.TrimSpace(userReply)

	return taskContent, userReply
}

// ensurePMToSE 确保PM消息带@SE前缀，并清理多余@
func (m *Manager) ensurePMToSE(content string) string {
	content = strings.TrimSpace(content)

	// 0. 清除开头的裸@ + 空格（AI输出 "@ 请创建..." 模式）
	re0 := regexp.MustCompile(`^@\s+`)
	content = re0.ReplaceAllString(content, "")
	content = strings.TrimSpace(content)

	re3 := regexp.MustCompile(`^@[A-Za-z]+\s+@[A-Z]{2,3}\s+`)
	content = re3.ReplaceAllString(content, "")
	content = strings.TrimSpace(content)

	re1 := regexp.MustCompile(`^@[A-Za-z]+\s+@\s*`)
	content = re1.ReplaceAllString(content, "")
	content = strings.TrimSpace(content)

	re2 := regexp.MustCompile(`^@[A-Za-z]+\s+[A-Z]{2,3}\s+`)
	content = re2.ReplaceAllString(content, "")
	content = strings.TrimSpace(content)

	if !strings.HasPrefix(content, "@SE ") && !strings.HasPrefix(content, "@SE\n") {
		if strings.HasPrefix(content, "@SE") {
			return content
		}
		return "@SE " + content
	}
	return content
}

// addPMToSEMsg 发送PM→SE消息（自动加@SE，内部调度）
func (m *Manager) addPMToSEMsg(content string) {
	content = strings.TrimSpace(content)
	if content == "" {
		fmt.Println("[PM→SE] ⚠️ 内容为空，跳过发送")
		return
	}

	content = m.ensurePMToSE(content)
	pmInternalMsg := Message{
		From:    "pm",
		To:      "se",
		Role:    "pm",
		Content: content,
		Raw:     content,
		Source:  "pm_to_se",
	}
	m.addHistory(pmInternalMsg)
	fmt.Printf("[PM→SE] %s\n", content)

	m.msgBusSend("pm", content, "pm_message", PathPMStream, "addPMToSEMsg", map[string]string{"delta": content})
	m.msgBusSend("pm", content, "se_task_assigned", PathPMStream, "addPMToSEMsg", map[string]interface{}{
		"task":  strings.TrimPrefix(content, "@SE "),
		"steps": 0,
	})
}

// ensureSEToUSR 确保SE消息带@USR前缀
func (m *Manager) ensureSEToUSR(content string) string {
	if strings.HasPrefix(content, "@USR") {
		return content
	}
	return "@USR " + content
}

// addSEToUserMsg 发送SE状态通知→USR
// 路由策略: SE的任务内容走 se_to_pm → PM审核后转发USR；
//           但SE的错误/状态/进度通知需要立即触达用户，不经过PM中转。
// 这类消息包括: 执行失败、语义完成报告、需要帮助、达到重试上限等。
// Source统一为 "se_notify" 以区别于任务内容消息。
func (m *Manager) addSEToUserMsg(content string) {
	content = m.ensureSEToUSR(content)
	seMsg := Message{
		From:      "se",
		To:        "user",
		Role:      "se",
		Content:   content,
		Raw:       content,
		Source:    "se_notify", // 状态通知，非任务内容
		Timestamp: time.Now(),
		ReplyTo:   m.getReplyToID("se"),
	}
	m.mu.Lock()
	m.history = append(m.history, seMsg)
	if len(m.history) > 200 {
		trimLen := len(m.history) - 200
		m.history = m.history[trimLen:]
	}
	m.mu.Unlock()
	m.writeConversationLog(seMsg)
	fmt.Printf("[SE→USR] %s\n", content)

	if m.onMessageAdded != nil {
		m.onMessageAdded(seMsg)
	}
}

// validateGoSyntax 检查Go文件语法是否正确，返回错误信息（空=通过）
func (m *Manager) validateGoSyntax(absPath string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "go", "build", "-o", os.DevNull, absPath)
	cmd.Dir = m.workDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		// 提取关键错误信息（通常在最后几行）
		lines := strings.Split(string(output), "\n")
		var keyErrors []string
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "#") {
				keyErrors = append(keyErrors, line)
			}
		}
		if len(keyErrors) == 0 {
			return err.Error()
		}
		return strings.Join(keyErrors[len(keyErrors)-2:], "; ")
	}
	return ""
}

// reorderActions 重排序actions：确保write_file在exec之前
// LLM经常先返回exec再返回write_file，导致文件不存在
func (m *Manager) reorderActions(actions []ai.SEAction) []ai.SEAction {
	if len(actions) <= 1 {
		return actions
	}

	var writes []ai.SEAction
	var others []ai.SEAction

	for i, a := range actions {
		switch a.Type {
		case "write_file", "edit_file":
			writes = append(writes, actions[i])
		default:
			others = append(others, actions[i])
		}
	}

	if len(writes) == 0 {
		return actions
	}

	result := append(writes, others...)
	if len(writes) > 0 && len(others) > 0 {
		fmt.Printf("[reorderActions] 🔄 重排序: %d个write/edit → %d个其他\n", len(writes), len(others))
	}
	return result
}

// repairGoContent 修复LLM生成的常见Go代码错误
// 基于实测错误模式：缺import关键字、缺fmt前缀、引号丢失等
func (m *Manager) repairGoContent(content string) string {
	fixed := content
	changed := false

	// 模式0: 清除尾部JSON泄漏（如 "}},{" , "}," 等）
	// LLM的content字段经常包含JSON结构残留
	re0 := regexp.MustCompile(`[\s]*[",}\]]{2,}\s*$`)
	if re0.MatchString(fixed) {
		fixed = re0.ReplaceAllString(fixed, "")
		changed = true
	}

	// 模式1: 独立的 "fmt" 行（缺import关键字）
	re1 := regexp.MustCompile(`(?m)^\s*"\s*fmt\s*"\s*$`)
	if re1.MatchString(fixed) {
		fixed = re1.ReplaceAllString(fixed, `import "fmt"`)
		changed = true
	}

	// 模式1b: 开头只有 " main" 或 "main" （缺package关键字）
	re1b := regexp.MustCompile(`(?m)^(\s*)main\s*$`)
	if re1b.MatchString(fixed) && !strings.Contains(fixed, "package ") {
		fixed = re1b.ReplaceAllString(fixed, `${1}package main`)
		changed = true
	}

	// 模式2: package "fmt" → package main + import "fmt"
	if strings.Contains(fixed, `package "fmt"`) || strings.Contains(fixed, "package\"fmt\"") {
		fixed = strings.Replace(fixed, `package "fmt"`, "package main\n\nimport \"fmt\"", 1)
		fixed = strings.Replace(fixed, "package\"fmt\"", "package main\n\nimport \"fmt\"", 1)
		changed = true
	}

	// 模式3: .Println( / .Sprintf( （缺fmt前缀）
	re3 := regexp.MustCompile(`(?m)^\s*\.\s*(Println|Sprintf|Printf|Fprintf|Errorf)\s*\(`)
	if re3.MatchString(fixed) {
		fixed = re3.ReplaceAllString(fixed, "fmt.$1(")
		changed = true
	}

	// 模式4: importfmt" / import"fmt" → import "fmt"
	fixed = strings.ReplaceAll(fixed, "importfmt\"", "import \"fmt\"")
	fixed = strings.ReplaceAll(fixed, "import\"fmt\"", "import \"fmt\"")
	if strings.Contains(fixed, "import\"") || strings.Contains(fixed, "importfmt") { changed = true }

	// 模式5: func main { （缺括号）→ func main()
	re5 := regexp.MustCompile(`func\s+main\s*\{`)
	if re5.MatchString(fixed) {
		fixed = re5.ReplaceAllString(fixed, "func main() {")
		changed = true
	}

	// 模式6: 确保有 package main（如果文件有 func main 但没有 package）
	if !regexp.MustCompile(`package\s+\w+`).MatchString(fixed) && regexp.MustCompile(`func\s+main\s*\(`).MatchString(fixed) {
		fixed = "package main\n\n" + fixed
		changed = true
	}

	if changed {
		fmt.Printf("[repairGoContent] 🔧 Go代码已修复 (%d patterns)\n", 1)
	}
	return fixed
}

// normalizeActionTypes 规范化LLM返回的action type
// LLM经常返回错误的tool name（如 write→write_file, 空字符串等）
func (m *Manager) normalizeActionTypes(actions []ai.SEAction) []ai.SEAction {
	// 常见错误映射：LLM实际返回 → 正确的tool name
	typeMap := map[string]string{
		"write":        "write_file",
		"w":            "write_file",
		"create":       "write_file",
		"create_file":  "write_file",
		"edit":         "edit_file",
		"modify":       "edit_file",
		"read":         "read_file",
		"run":          "exec",
		"execute":      "exec",
		"command":      "exec",
		"shell":        "exec",
		"search":       "search_files",
		"find":         "search_files",
		"grep":         "search_files",
		"git":          "git_operation",
		"":             "", // 空字符串特殊处理
	}

	changed := false
	for i := range actions {
		oldType := actions[i].Type
		if mapped, ok := typeMap[oldType]; ok && mapped != "" {
			actions[i].Type = mapped
			if oldType != mapped {
				fmt.Printf("[normalizeAction] %q → %q\n", oldType, mapped)
				changed = true
			}
		} else if oldType == "" {
			// 空type：尝试从content/command推断
			if actions[i].Content != "" && actions[i].Path != "" {
				actions[i].Type = "write_file"
				fmt.Printf("[normalizeAction] \"\" → write_file (inferred from path+content)\n")
				changed = true
			} else if actions[i].Command != "" {
				actions[i].Type = "exec"
				fmt.Printf("[normalizeAction] \"\" → exec (inferred from command)\n")
				changed = true
			}
		} else if !isValidActionType(oldType) {
			// 完全未知的action type，记录但不修改（让switch default处理）
			fmt.Printf("[normalizeAction] ⚠️ 未知action type: %q\n", oldType)
		}
	}

	if changed {
		fmt.Printf("[normalizeActionTypes] 🔄 已规范化%d个actions\n", len(actions))
	}
	return actions
}

func isValidActionType(t string) bool {
	switch t {
	case "write_file", "edit_file", "read_file", "exec", "exec_session", "search_files",
		"git_operation", "run_tests", "semantic_search", "web_search",
		"undo_file", "list_changes",
		"go_to_definition", "find_references", "hover_info", "diagnostics", "rename_symbol",
		"analyze_image", "search_snippet", "show_diff", "debug_run", "spawn_subtask",
		"analyze_code", "auto_debug":
		return true
	default:
		return false
	}
}

// seDebugLog 写SE/Client关键日志到conversation.log（与WriteDebugLog同文件）
func (m *Manager) seDebugLog(msg string) {
	logDir := filepath.Join(m.configDir, "..", "logs")
	logPath := filepath.Join(logDir, "conversation.log")
	os.MkdirAll(logDir, 0755)
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Printf("[seDebugLog] ERROR: open %s failed: %v\n", logPath, err)
		return
	}
	defer f.Close()
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	line := fmt.Sprintf("[%s] DEBUG: %s\n", timestamp, msg)
	f.WriteString(line)
	// 终端也打一份关键标记
	if strings.Contains(msg, "[INIT]") || strings.Contains(msg, "G-DEBUG") || strings.Contains(msg, "SE-RAW") {
		fmt.Printf("[seDebugLog-OK] %s", line)
	}
}

// writeRouteLog 路由调试日志（永久保留，排查消息路由问题用）
func (m *Manager) writeRouteLog(msg string) {
	m.seDebugLog(msg)
}

// writeConversationLog 写入对话日志文件（调试用途，存放在IDE系统目录的logs/下）
func (m *Manager) writeConversationLog(msg Message) {
	logDir := filepath.Join(m.configDir, "..", "logs")
	os.MkdirAll(logDir, 0755)
	logPath := filepath.Join(logDir, "conversation.log")

	// ✅ 自动清理：日志超过500KB时只保留最后2000行（开发调试用）
	if info, err := os.Stat(logPath); err == nil && info.Size() > 512*1024 {
		m.truncateLog(logPath, 2000)
	}

	// 追加写入
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Printf("[ConversationLog] Write failed: %v\n", err)
		return
	}
	defer f.Close()

	timestamp := time.Now().Format("2006-01-02 15:04:05")
	logEntry := fmt.Sprintf("[%s] %s: %s\n", timestamp, strings.ToUpper(msg.From), msg.Raw)

	if _, err := f.WriteString(logEntry); err != nil {
		fmt.Printf("[ConversationLog] Write failed: %v\n", err)
	}
}

// truncateLog 截断日志文件，只保留最后N行
func (m *Manager) truncateLog(logPath string, keepLines int) {
	data, err := os.ReadFile(logPath)
	if err != nil {
		return
	}

	lines := strings.Split(string(data), "\n")
	if len(lines) <= keepLines {
		return
	}

	// 保留最后 N 行
	kept := lines[len(lines)-keepLines:]
	newContent := strings.Join(kept, "\n")

	if err := os.WriteFile(logPath, []byte(newContent), 0644); err == nil {
		fmt.Printf("[ConversationLog] 已清理，保留最后 %d 行（原 %d 行）\n", keepLines, len(lines))
	}
}

// saveTaskMemory 保存任务记忆
func (m *Manager) saveTaskMemory(userRequest string) {
	if m.memoryManager == nil {
		return
	}

	m.mu.RLock()
	currentRole := m.currentRole
	pmWaiting := m.pmWaitingForUserSince > 0
	messages := make([]Message, len(m.history))
	copy(messages, m.history)
	m.mu.RUnlock()

	// 确定当前状态
	var currentState string
	switch {
	case pmWaiting:
		currentState = "waiting"
	case currentRole == "pm" || currentRole == "se":
		currentState = "working"
	default:
		currentState = "idle"
	}

	// 获取任务描述（从最近PM消息中提取）
	taskDescription := extractTaskDescription(messages)

	// 转换消息类型（chat.Message -> types.Message）
	var typedMessages []types.Message
	for _, msg := range messages {
		typedMessages = append(typedMessages, types.Message{
			Role:      msg.Role,
			Content:   msg.Content,
			Timestamp: msg.Timestamp,
		})
	}

	if err := m.memoryManager.SaveState(userRequest, currentState, currentRole, taskDescription, typedMessages); err != nil {
		fmt.Printf("[Manager] 保存任务记忆失败: %v\n", err)
	}
}

// saveTaskMemoryImmediate 立即保存任务记忆（不等PM/SE处理完成，用于快速更新TaskID）
func (m *Manager) saveTaskMemoryImmediate(userRequest string) {
	if m.memoryManager == nil {
		return
	}

	m.mu.RLock()
	currentRole := m.currentRole
	messages := make([]Message, len(m.history))
	copy(messages, m.history)
	m.mu.RUnlock()

	// 获取任务描述
	taskDescription := extractTaskDescription(messages)

	// 转换消息类型
	var typedMessages []types.Message
	for _, msg := range messages {
		typedMessages = append(typedMessages, types.Message{
			Role:      msg.Role,
			Content:   msg.Content,
			Timestamp: msg.Timestamp,
		})
	}

	// 使用 SaveStateImmediate（跳过stopped检查，因为这是用户主动触发的）
	if err := m.memoryManager.SaveStateImmediate(userRequest, "working", currentRole, taskDescription, typedMessages, true); err != nil {
		fmt.Printf("[Manager] 立即保存任务记忆失败: %v\n", err)
	} else {
		fmt.Println("[Manager] ✅ 已立即保存任务状态（TaskID已更新）")
	}
}

// extractTaskDescription 从消息历史中提取任务描述
// getCurrentState 获取当前状态（用于自动保存）
func (m *Manager) getCurrentState() string {
	if m.pmWaitingForUserSince > 0 {
		return "waiting"
	}
	switch m.currentRole {
	case "pm", "se":
		return "working"
	default:
		return "idle"
	}
}

func extractTaskDescription(messages []Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if msg.Role == "pm" && len(msg.Content) > 10 {
			// 截取前100字符作为任务描述
			if len(msg.Content) > 100 {
				return msg.Content[:100] + "..."
			}
			return msg.Content
		}
	}
	return ""
}

// CheckUnfinishedTask 检查是否有未完成任务
func (m *Manager) CheckUnfinishedTask() (bool, *types.TaskMemory, error) {
	if m.memoryManager == nil {
		return false, nil, nil
	}
	return m.memoryManager.HasUnfinishedTask()
}

// RecoverTask 恢复未完成任务
func (m *Manager) RecoverTask(memory *types.TaskMemory) error {
	if m.memoryManager == nil || memory == nil {
		return fmt.Errorf("memory manager or memory is nil")
	}

	fmt.Printf("[Manager] 开始恢复任务: %s\n", memory.UserRequest)

	// 恢复消息历史（types.Message -> chat.Message）
	m.mu.Lock()
	m.history = make([]Message, len(memory.RecentMessages))
	for i, msg := range memory.RecentMessages {
		m.history[i] = Message{
			Role:      msg.Role,
			Content:   msg.Content,
			Timestamp: msg.Timestamp,
			From:      msg.Role,
			To:        "pm",
			Raw:       msg.Content,
		}
	}
	m.currentRole = memory.CurrentRole
	m.mu.Unlock()

	// 通知前端恢复完成
	if m.onTaskRecovered != nil {
		m.onTaskRecovered(memory)
	}

	fmt.Printf("[Manager] 任务恢复完成，恢复了 %d 条消息\n", len(memory.RecentMessages))
	return nil
}

// ClearTaskMemory 清除任务记忆（任务完成时调用）
func (m *Manager) ClearTaskMemory() error {
	if m.memoryManager == nil {
		return nil
	}
	return m.memoryManager.ClearState()
}

// SetOnTaskRecovered 设置任务恢复回调
func (m *Manager) SetOnTaskRecovered(callback func(*types.TaskMemory)) {
	m.onTaskRecovered = callback
}

// GetHistory 获取历史
func (m *Manager) GetHistory() []Message {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]Message, len(m.history))
	copy(result, m.history)
	return result
}

// GetCurrentRole 获取当前角色
func (m *Manager) GetCurrentRole() string {
	return m.currentRole
}

// GetExecutionStatus 获取执行状态（pm是否忙碌，se是否运行）
func (m *Manager) GetExecutionStatus() (pmBusy bool, seRunning bool) {
	m.mu.RLock()
	pmBusy = m.currentRole == "pm"
	seRunning = m.currentRole == "se"
	m.mu.RUnlock()

	// 二次检查：如果currentRole已空闲但CMonitor仍记录SE为busy，返回正确状态
	if !seRunning && m.cMonitor != nil {
		state, err := m.cMonitor.ReadState()
		if err == nil && state.SeStatus == types.RoleStatusBusy {
			seRunning = true
		}
	}

	// 二次检查：如果currentRole显示se但CMonitor已空闲，说明有状态残留
	if seRunning && m.cMonitor != nil {
		state, err := m.cMonitor.ReadState()
		if err == nil && state.SeStatus == types.RoleStatusIdle {
			seRunning = false
		}
	}

	return pmBusy, seRunning
}

func (m *Manager) GetPMWaitingForUser() (waiting bool, since int64, nudgeCount int) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	waiting = m.pmWaitingForUserSince > 0
	since = m.pmWaitingForUserSince
	nudgeCount = m.pmWaitingNudgeCount
	return
}

func (m *Manager) IncrementPMWaitingNudge() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pmWaitingNudgeCount++
	fmt.Printf("[Manager] USR催促次数: %d\n", m.pmWaitingNudgeCount)
	return m.pmWaitingNudgeCount
}

// cleanupOldArtifacts 清理旧编译产物（新任务开始时调用）
func (m *Manager) cleanupOldArtifacts() {
	workDir := m.workDir
	if workDir == "" {
		return
	}

	// 要清理的文件模式
	patterns := []string{
		"*.exe", // Windows可执行文件
		"*.tmp", // 临时文件
	}

	cleanedCount := 0
	for _, pattern := range patterns {
		matches, err := filepath.Glob(filepath.Join(workDir, pattern))
		if err != nil {
			continue
		}
		for _, match := range matches {
			if err := os.Remove(match); err == nil {
				fmt.Printf("[Cleanup] 删除旧文件: %s\n", filepath.Base(match))
				cleanedCount++
			}
		}
	}

	if cleanedCount > 0 {
		fmt.Printf("[Cleanup] 已清理 %d 个旧文件\n", cleanedCount)
	}
}

func (m *Manager) ResetRoleStatus() {
	m.mu.Lock()
	defer m.mu.Unlock()

	oldRole := m.currentRole
	m.currentRole = ""
	m.pmWaitingForUserSince = 0
	m.pmWaitingNudgeCount = 0
	m.reviewCount = 0
	m.seContinueCount = 0
	m.seAskPMCount = 0
	m.seReportedComplete = false
	m.userStopped = true // 标记用户主动停止，C监控会跳过催促

	// 🧹 清理路由器状态
	m.router.ForceClear()

	if m.cMonitor != nil {
		m.cMonitor.UpdatePmStatus(types.RoleStatusIdle)
		m.cMonitor.UpdateSeStatus(types.RoleStatusIdle)
		m.cMonitor.UpdateProjectState(types.ProjectStateIdle)
		m.cMonitor.ClearLastUserMessage()
	}

	if m.memoryManager != nil {
		_ = m.memoryManager.ClearState()
	}
	_ = m.boardManager.Reset()

	fmt.Printf("[ResetRoleStatus] 重置完成: %s → idle, project_state → idle\n", oldRole)
}

// HasActiveTask 检查是否有活动任务（用于智能退出判断）
func (m *Manager) HasActiveTask() bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.currentRole != "" {
		return true
	}

	if m.cMonitor != nil {
		state, err := m.cMonitor.ReadState()
		if err == nil && state.ProjectState == types.ProjectStateRunning {
			return true
		}
	}

	return false
}

// IsCMonitorActive 检查 C 监控是否活跃
func (m *Manager) IsCMonitorActive() bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.cMonitor == nil {
		return false
	}

	state, err := m.cMonitor.ReadState()
	if err != nil {
		return false
	}

	return state.ProjectState == types.ProjectStateRunning ||
		state.PmStatus == types.RoleStatusBusy ||
		state.SeStatus == types.RoleStatusBusy
}

// handlePMReview PM审核SE完成的任务
func (m *Manager) handlePMReview(reviewMsg string) error {
	if allowed, reason := m.router.CheckTurn("pm", "pm_review"); !allowed {
		fmt.Printf("[handlePMReview] ⚠️ 轮换拦截: %s\n", reason)
		return nil
	}
	m.router.MarkProcessingStart("pm")
	defer m.router.MarkProcessingEnd("pm")

	m.reviewCount++

	// ✅ 新增：如果项目已经是done状态，不再重复审核
	currentState, err := m.cMonitor.ReadState()
	if err != nil {
		fmt.Printf("[handlePMReview] 读取状态失败: %v\n", err)
	} else if currentState.ProjectState == types.ProjectStateApproved {
		fmt.Printf("[handlePMReview] 项目已最终完成(state=approved)，跳过\n")
		m.addPMToUserMsg(i18n.T("msg.task_complete"))
		return nil
	} else if currentState.ProjectState == types.ProjectStateError {
		fmt.Printf("[handlePMReview] 项目出错(state=error)，跳过审核\n")
		m.addPMToUserMsg(i18n.T("msg.task_complete"))
		return nil
	}

	if m.reviewCount > 10 {
		fmt.Printf("[handlePMReview] 审核轮次超限(%d)，强制结束审核流程\n", m.reviewCount)
		m.addPMToUserMsg(i18n.T("msg.review_timeout"))
		m.currentRole = ""
		m.cMonitor.UpdatePmStatus(types.RoleStatusIdle)

		// ✅ [FIX#3] 审核超限时设置明确的错误状态，防止状态不确定
		m.cMonitor.UpdateProjectState(types.ProjectStateError)
		m.boardManager.UpdateTask(i18n.T("msg.review_timeout"), 0)
		if m.onProjectStateChanged != nil {
			m.onProjectStateChanged("error")
		}

		return nil
	}

	m.currentRole = "pm"

	// === 三层模型：启动 PM 审核 TaskList ===
	pmTaskId := m.richBuilder.StartTaskList("pm", "PM 代码审核", []types.TaskItemDef{
		{Text: "接收 SE 完成报告"},
		{Text: "自动检测代码变更 (git status / list_files)"},
		{Text: "审阅变更详情 (read_file / git diff)"},
		{Text: "运行验证测试 (exec)"},
		{Text: "给出审核结论"},
	})
	var pmReviewResult string
	defer func() {
		status := "done"
		if pmReviewResult == "" {
			pmReviewResult = i18n.T("msg.task_complete")
		}
		m.richBuilder.CompleteTaskList(pmTaskId, status, &types.ResultBlock{
			Text: pmReviewResult,
		})
		m.richBuilder.Reset()
		if m.ctx != nil {
			m.msgBusSend("pm", pmReviewResult, "pm_review_completed", PathPMToUser, "handlePMReviewWithRich:done", map[string]interface{}{
				"taskId": pmTaskId,
				"status": status,
				"result": pmReviewResult,
			})
		}
	}()

	m.richBuilder.UpdateTask(pmTaskId, 0, "running")
	m.richBuilder.UpdateTask(pmTaskId, 0, "done")

	m.pmProcessor.SetTaskContext(pmTaskId, 1)

	// 转换历史
	history := m.GetHistory()
	pmHistory := make([]ai.ChatMessage, len(history))
	for i, msg := range history {
		pmHistory[i] = ai.ChatMessage{Role: msg.Role, Content: msg.Content}
	}

	// PM审核（使用带工具的ProcessReview，PM可读文件做Code Review）
	resp, err := m.pmProcessor.ProcessReview(reviewMsg, pmHistory, func(delta string) {
		m.emitStreamChunk("pm", delta)
	})
	if err != nil {
		m.cMonitor.UpdatePmStatus(types.RoleStatusIdle)
		m.cMonitor.UpdateProjectState(types.ProjectStateError)

		if m.ctx != nil {
			m.msgBusSend("system", err.Error(), "error", PathSystem, "handlePMReviewWithRich:error", map[string]interface{}{
				"error": err.Error(),
				"stage": "pm_review",
			})
		}

		errMsg := fmt.Sprintf("%s\n\n%s", i18n.T("err.pm_review_failed", err), i18n.T("err.pm_api_network"))
		m.addPMToUserMsg(errMsg)
		if m.onProjectStateChanged != nil {
			m.onProjectStateChanged("error")
		}
		return fmt.Errorf("PM review failed: %w", err)
	}

	pmReviewResult = resp.Content

	cleanContent := strings.TrimSpace(resp.Content)
	if cleanContent == "" || cleanContent == "@USR" || cleanContent == "@USR " {
		fmt.Printf("[handlePMReview] ⚠️ PM审核回复为空，强制转AP审批 (G37修复)\n")
		m.currentRole = ""
		m.cMonitor.UpdatePmStatus(types.RoleStatusIdle)
		m.SetHandoverPending(HandoverPMToAP)
		if m.apProcessor != nil {
			return m.handleAPReview("SE任务已完成，请AP进行最终质量审批")
		}
		m.forceProjectApproved()
		return nil
	}

	lowerContent := strings.ToLower(cleanContent)
	hasApprovalKeywords := strings.Contains(lowerContent, "已验证") ||
		strings.Contains(lowerContent, "审核通过") ||
		strings.Contains(lowerContent, "验证通过") ||
		strings.Contains(lowerContent, "任务完成") ||
		strings.Contains(lowerContent, "approved") ||
		strings.Contains(lowerContent, "通过")
	hasAPTag := strings.Contains(cleanContent, "@AP")
	fmt.Printf("[DEBUG-3RD-RICH] RichPM路径: content=%q hasAPTag=%v len=%d\n", cleanContent[:min(100, len(cleanContent))], hasAPTag, len(cleanContent))

	if hasApprovalKeywords && !hasAPTag {
		fmt.Println("[handlePMReview] ⚠️ 第三层保护: PM输出含审批关键词但缺@AP，自动补上")
		cleanContent = strings.TrimPrefix(cleanContent, "@USR ")
		cleanContent = strings.TrimPrefix(cleanContent, "@USR")
		resp.Content = "@AP " + cleanContent
		cleanContent = resp.Content
		fmt.Printf("[handlePMReview] ✅ 补全后内容: %s\n", cleanContent[:min(80, len(cleanContent))])
	}

	// 添加PM回复到历史（自动加@USR）
	m.addPMToUserMsg(resp.Content)

	// === 主路：AI结构化审核结果 ===
	// AI输出JSON {"review_result":"reject"/"approve","reason":"..."}
	// 这是AI的直接判断，优先级最高
	if resp.ReviewResult == "reject" {
		fmt.Printf("[System] PM审核拒绝(AI结构化): %s\n", resp.ReviewReason[:min(80, len(resp.ReviewReason))])
		m.cMonitor.UpdateProjectState(types.ProjectStateRunning)
		return m.startSETaskWithFrom(fmt.Sprintf("PM审核不通过: %s", resp.ReviewReason), "pm")
	}

	if resp.ReviewResult == "approve" {
		fmt.Println("[System] PM审核通过(AI结构化)，触发AP审批...")
		m.currentRole = ""
		m.cMonitor.UpdatePmStatus(types.RoleStatusIdle)
		m.SetHandoverPending(HandoverPMToAP)
		return m.handleAPReview("请AP进行最终质量审批")
	}

	// === 兜底：文本解析（AI没输出JSON时） ===
	parsedMsg := m.router.Parse("pm", resp.Content)

	if parsedMsg.To == "ap" {
		fmt.Println("[System] handlePMReview PM @AP detected, 路由到AP审批...")
		m.currentRole = ""
		m.cMonitor.UpdatePmStatus(types.RoleStatusIdle)
		m.SetHandoverPending(HandoverPMToAP)
		if m.apProcessor != nil {
			return m.handleAPReview(parsedMsg.Content)
		}
		m.forceProjectApproved()
		return nil
	}

	if m.reviewCount >= 3 {
		fmt.Printf("[System] 审核轮次已达%d，强制结束审核流程\n", m.reviewCount)
		m.addPMToUserMsg(i18n.T("msg.review_timeout"))
		m.currentRole = ""
		m.cMonitor.UpdatePmStatus(types.RoleStatusIdle)
		m.cMonitor.UpdateProjectState(types.ProjectStateError)
		m.boardManager.UpdateTask(i18n.T("msg.review_timeout"), 0)
		if m.onProjectStateChanged != nil {
			m.onProjectStateChanged("error")
		}
		return nil
	}

	if parsedMsg.To == "se" {
		lowerContent := strings.ToLower(strings.TrimSpace(parsedMsg.Content))

		isRework := strings.Contains(lowerContent, "不通过") ||
			strings.Contains(lowerContent, "返工") ||
			strings.Contains(lowerContent, "修改") ||
			strings.Contains(lowerContent, "错误") ||
			strings.Contains(lowerContent, "失败") ||
			strings.Contains(lowerContent, "重写") ||
			strings.Contains(lowerContent, "有bug") ||
			strings.Contains(lowerContent, "bug") ||
			strings.Contains(lowerContent, "rejected") ||
			strings.Contains(lowerContent, "reject") ||
			strings.Contains(lowerContent, "failed")

		if isRework {
			fmt.Printf("[System] PM审核要求返工(文本解析)，生成新任务给SE\n")
			m.cMonitor.UpdateProjectState(types.ProjectStateRunning)
			return m.startSETaskWithFrom(parsedMsg.Content, "pm")
		}

		fmt.Printf("[System] PM审核违规@SE(非返工)，拦截不路由: %s\n", parsedMsg.Content[:min(80, len(parsedMsg.Content))])
		m.reviewCount++

		if m.reviewCount >= 3 {
			fmt.Printf("[System] PM审核违规@SE达%d次，强制流转AP\n", m.reviewCount)
		}

		parsedMsg.To = "usr"
	}

	if resp.HasTasks {
		fmt.Println("[System] PM has more tasks, continuing...")
		return m.startSETask(resp.Tasks.CurrentTask)
	}

	fmt.Println("[System] PM审核通过(兜底)，触发AP审批...")
	m.currentRole = ""
	m.cMonitor.UpdatePmStatus(types.RoleStatusIdle)
	m.SetHandoverPending(HandoverPMToAP)

	return m.handleAPReview("请AP进行最终质量审批")
}

// handlePMReviewWithRich 从 handleToPM 调用的 PM 审核（带三层模型可视化）
func (m *Manager) handlePMReviewWithRich(content string, pmCtx context.Context) error {
	fmt.Println("[RichPM] === 启动 PM 三层模型审核 ===")

	// 创建PM审核任务
	if m.taskManager != nil {
		m.taskManager.CreateTask("审核：验证SE执行结果", "PM")
	}

	pmTaskId := m.richBuilder.StartTaskList("pm", "PM 代码审核", []types.TaskItemDef{
		{Text: "接收 SE 完成报告"},
		{Text: "自动检测代码变更 (git status / list_files)"},
		{Text: "审阅变更详情 (read_file / git diff)"},
		{Text: "运行验证测试 (exec)"},
		{Text: "给出审核结论"},
	})
	var pmReviewResult string
	defer func() {
		status := "completed"
		if pmReviewResult == "" {
			status = "error"
			pmReviewResult = i18n.T("err.pm_review_failed", fmt.Errorf("no response"))
		}
		m.richBuilder.CompleteTaskList(pmTaskId, status, &types.ResultBlock{
			Text: pmReviewResult,
		})
		m.richBuilder.Reset()
	}()

	m.richBuilder.UpdateTask(pmTaskId, 0, "running")
	m.richBuilder.UpdateTask(pmTaskId, 0, "done")
	m.pmProcessor.SetTaskContext(pmTaskId, 1)

	if m.ctx != nil {
		m.msgBusSend("pm", "", "pm_started", PathPMStream, "handleToPM:pm_started", map[string]string{})
	}
	m.cMonitor.UpdateProjectState(types.ProjectStateRunning)
	m.cMonitor.UpdatePmStatus(types.RoleStatusBusy)

	history := m.GetHistory()
	pmHistory := make([]ai.ChatMessage, len(history))
	for i, msg := range history {
		pmHistory[i] = ai.ChatMessage{Role: msg.Role, Content: msg.Content}
	}

	m.pmProcessor.SetContext(pmCtx)
	resp, err := m.pmProcessor.ProcessReview(content, pmHistory, func(delta string) {
		m.emitStreamChunk("pm", delta)
	})
	if err != nil {
		m.cMonitor.UpdatePmStatus(types.RoleStatusIdle)
		m.cMonitor.UpdateProjectState(types.ProjectStateError)
		if m.ctx != nil {
			m.msgBusSend("system", err.Error(), "error", PathSystem, "handleToPM:error", map[string]interface{}{
				"error": err.Error(), "stage": "pm_review",
			})
		}
		errMsg := fmt.Sprintf("%s\n\n%s", i18n.T("err.pm_review_failed", err), i18n.T("err.pm_api_network"))
		m.addPMToUserMsg(errMsg)
		return fmt.Errorf("PM review failed: %w", err)
	}

	pmReviewResult = resp.Content
	cleanContent := strings.TrimSpace(resp.Content)
	if cleanContent == "" || cleanContent == "@USR" || cleanContent == "@USR " {
		fmt.Printf("[RichPM] ⚠️ PM审核回复为空，强制转AP审批 (G37修复)\n")
		m.currentRole = ""
		m.cMonitor.UpdatePmStatus(types.RoleStatusIdle)
		m.SetHandoverPending(HandoverPMToAP)
		if m.apProcessor != nil {
			return m.handleAPReview("SE任务已完成，请AP进行最终质量审批")
		}
		m.forceProjectApproved()
		return nil
	}

	lowerContent := strings.ToLower(cleanContent)
	hasApprovalKeywords := strings.Contains(lowerContent, "已验证") ||
		strings.Contains(lowerContent, "审核通过") ||
		strings.Contains(lowerContent, "验证通过") ||
		strings.Contains(lowerContent, "任务完成") ||
		strings.Contains(lowerContent, "approved") ||
		strings.Contains(lowerContent, "通过")
	hasAPTag := strings.Contains(cleanContent, "@AP")

	if hasApprovalKeywords && !hasAPTag {
		fmt.Println("[RichPM] ⚠️ 第三层保护: PM输出含审批关键词但缺@AP，自动补上")
		cleanContent = strings.TrimPrefix(cleanContent, "@USR ")
		cleanContent = strings.TrimPrefix(cleanContent, "@USR")
		resp.Content = "@AP " + cleanContent
		cleanContent = resp.Content
		fmt.Printf("[RichPM] ✅ 补全后内容: %s\n", cleanContent[:min(80, len(cleanContent))])
	}

	hasFakeToolCall := strings.Contains(lowerContent, "list_files") ||
		strings.Contains(lowerContent, "read_file") ||
		strings.Contains(lowerContent, "exec ") ||
		strings.Contains(lowerContent, "write_file")
	isJustToolCall := hasFakeToolCall &&
		!hasApprovalKeywords &&
		!hasAPTag &&
		!strings.Contains(cleanContent, "@SE") &&
		len(cleanContent) < 300

	if isJustToolCall {
		fmt.Println("[RichPM] ⚠️ 检测到PM输出伪工具调用文本(无结论)，强制转AP")
		resp.Content = "@AP [系统自动验证] SE任务已完成，请进行最终质量审批"
		cleanContent = resp.Content
	}

	m.addPMToUserMsg(resp.Content)
	m.resetPMHealth()

	// === 主路：AI结构化审核结果 ===
	if resp.ReviewResult == "reject" {
		fmt.Println("[RichPM] PM审核拒绝(AI结构化) → 返工 [EXIT-REJECT]")
		m.currentRole = ""
		m.cMonitor.UpdatePmStatus(types.RoleStatusIdle)
		return m.startSETaskWithFrom(fmt.Sprintf("PM审核不通过: %s", resp.ReviewReason), "pm")
	}

	if resp.ReviewResult == "approve" {
		fmt.Println("[RichPM] PM审核通过(AI结构化) → 路由到AP审批 [EXIT-APPROVE]")
		m.currentRole = ""
		m.cMonitor.UpdatePmStatus(types.RoleStatusIdle)
		m.SetHandoverPending(HandoverPMToAP)
		if m.taskManager != nil {
			m.taskManager.CompleteLastTaskByRole("PM")
		}
		if m.apProcessor != nil {
			return m.handleAPReview("请AP进行最终质量审批")
		}
		m.forceProjectApproved()
		return nil
	}

	// === 兜底：文本解析 ===
	parsedMsg := m.router.Parse("pm", resp.Content)
	fmt.Printf("[DEBUG-G57] 🔀 Router解析: To=%q | Content_head=%q\n", parsedMsg.To, parsedMsg.Content[:min(80, len(parsedMsg.Content))])
	if parsedMsg.To == "ap" {
		fmt.Println("[RichPM] PM @AP → 路由到AP审批 [EXIT-1]")
		m.currentRole = ""
		m.cMonitor.UpdatePmStatus(types.RoleStatusIdle)
		m.SetHandoverPending(HandoverPMToAP)
		if m.taskManager != nil {
			m.taskManager.CompleteLastTaskByRole("PM")
		}
		if m.apProcessor != nil {
			return m.handleAPReview(parsedMsg.Content)
		}
	}

	if parsedMsg.To == "se" {
		fmt.Println("[RichPM] PM @SE → 返工 [EXIT-2]")
		m.currentRole = ""
		m.cMonitor.UpdatePmStatus(types.RoleStatusIdle)
		return m.startSETaskWithFrom(parsedMsg.Content, "pm")
	}

	m.currentRole = ""
	m.cMonitor.UpdatePmStatus(types.RoleStatusIdle)

	if resp.HasTasks {
		fmt.Println("[RichPM] ⚠️ [G56-FIX] 审核模式忽略HasTasks，PM审核职责是验证不是分配 [EXIT-3-SKIP]")
		fmt.Printf("[RichPM] ℹ️ 残留任务JSON: current_task=%q status=%q\n",
			resp.Tasks.CurrentTask, resp.Tasks.Status)
	}

	fmt.Println("[RichPM] → 走fallback路径 → handleAPReview [EXIT-4]")
	m.SetHandoverPending(HandoverPMToAP)
	if m.taskManager != nil {
		m.taskManager.CompleteLastTaskByRole("PM")
	}
	return m.handleAPReview("请AP进行最终质量审批")
}

// handleAPDirectChat 用户直接@AP对话（测试用）
func (m *Manager) handleAPDirectChat(input string) (string, error) {
	fmt.Printf("[handleAPDirectChat] 开始: input='%s'\n", input[:min(80, len(input))])
	if m.apProcessor == nil {
		fmt.Println("[handleAPDirectChat] ❌ apProcessor=nil")
		errMsg := "❌ AP未配置，请在设置中启用AP并配置API"
		m.addAPToUserMsg(errMsg)
		return "", fmt.Errorf("AP not configured")
	}
	fmt.Println("[handleAPDirectChat] ✅ apProcessor存在，调用ProcessReview...")

	// ⏰ 添加120秒超时，防止AP AI API调用无限期挂起
	safeCtx := context.Background()
	m.mu.RLock()
	if m.ctx != nil {
		safeCtx = m.ctx
	}
	m.mu.RUnlock()
	apCtx, apCancel := context.WithTimeout(safeCtx, 120*time.Second)
	defer apCancel()
	m.apProcessor.SetContext(apCtx)

	history := m.GetHistory()
	apHistory := make([]ai.ChatMessage, 0, len(history))
	for _, msg := range history {
		apHistory = append(apHistory, ai.ChatMessage{Role: msg.Role, Content: msg.Content})
	}

	fmt.Printf("[handleAPDirectChat] 调用ProcessReview, history=%d条\n", len(apHistory))
	resp, err := m.apProcessor.ProcessReview(input, apHistory, func(delta string) {
		m.emitStreamChunk("ap", delta)
	})
	if err != nil {
		fmt.Printf("[handleAPDirectChat] ❌ ProcessReview失败: %v\n", err)
		errMsg := fmt.Sprintf("❌ AP响应失败: %v", err)
		m.addAPToUserMsg(errMsg)
		return "", err
	}
	fmt.Printf("[handleAPDirectChat] ✅ ProcessReview成功, content='%s'\n", resp.Content[:min(100, len(resp.Content))])

	m.addAPToUserMsg(resp.Content)
	return resp.Content, nil
}

// handleAPReview AP审批处理（第二道质量关卡）
func (m *Manager) handleAPReview(reviewMsg string) error {
	fmt.Printf("[TRACE-AP] 🔥 handleAPReview入口! msg=%q apEnabled=%v state=%v (时间:%s)\n",
		reviewMsg[:min(60, len(reviewMsg))], m.apProcessor != nil, m.cMonitor.GetProjectState(), time.Now().Format("15:04:05"))

	// 创建AP任务记录（AP审核任务）
	if m.taskManager != nil {
		m.taskManager.CreateTask("审核：任务完成情况及质量审批", "AP")
	}

	m.router.SetLastSpokenBy("pm")
	if allowed, reason := m.router.CheckTurnInternal("ap", "ap_review", true); !allowed {
		fmt.Printf("[TRACE-AP] ❌ AP被轮换拦截: %s\n", reason)
		return nil
	}
	fmt.Println("[TRACE-AP] ✅ 轮换通过，开始AP审核...")
	m.router.MarkProcessingStart("ap")
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("[handleAPReview] 💥 panic recovered: %v\n", r)
		}
		m.router.MarkProcessingEnd("ap")
	}()

	// 🔴 安全保证：确保 AP 处理器有有效 ctx
	safeCtx := context.Background()
	m.mu.RLock()
	if m.ctx != nil {
		safeCtx = m.ctx
	}
	m.mu.RUnlock()
	// ⏰ 添加120秒超时，防止AP AI API调用无限期挂起
	apCtx, apCancel := context.WithTimeout(safeCtx, 120*time.Second)
	defer apCancel()
	if m.apProcessor != nil {
		m.apProcessor.SetContext(apCtx)
	}

	// ✅ 接收者清除handover（AP接手PM→AP交接）
	m.mu.Lock()
	if m.handover.Pending && m.handover.Step == HandoverPMToAP {
		m.handover = HandoverState{}
		fmt.Println("[Handover] ✅ AP已接手PM→AP交接")
	}
	m.mu.Unlock()

	m.apMode = RoleModeAPApprove
	m.apReviewCount++
	fmt.Printf("[handleAPReview] 开始AP审批(第%d轮): %s\n", m.apReviewCount, reviewMsg[:min(100, len(reviewMsg))])

	if m.apReviewCount > 10 {
		fmt.Printf("[handleAPReview] AP审核轮次超限(%d)，标记错误\n", m.apReviewCount)
		m.addAPToUserMsg("⚠️ AP审核轮次过多，已暂停自动审批。请人工确认项目状态。")
		m.cMonitor.UpdateProjectState(types.ProjectStateError)
		m.currentRole = ""
		if m.onProjectStateChanged != nil {
			m.onProjectStateChanged("error")
		}
		return nil
	}

	m.currentRole = "ap"

	history := m.GetHistory()
	apHistory := make([]ai.ChatMessage, len(history))
	for i, msg := range history {
		apHistory[i] = ai.ChatMessage{Role: msg.Role, Content: msg.Content}
	}

	maxRetries := 3
	var lastErr error
	var resp *ai.APResponse
	var err error
	for retry := 0; retry < maxRetries; retry++ {
		if retry > 0 {
			retryTimeout := time.Duration(60*(retry+1)) * time.Second
			fmt.Printf("[handleAPReview] 🔄 第%d次重试(超时%ds)...\n", retry, retryTimeout/time.Second)
			m.addAPToUserMsg(fmt.Sprintf("⚠️ AP审批重试中(%d/%d)...", retry, maxRetries-1))
			ctx2, cancel2 := context.WithTimeout(safeCtx, retryTimeout)
			defer cancel2()
			if m.apProcessor != nil {
				m.apProcessor.SetContext(ctx2)
			}
		}
		resp, err = m.apProcessor.ProcessReview(reviewMsg, apHistory, func(delta string) {
			m.emitStreamChunk("ap", delta)
		})
		if err == nil {
			break
		}
		lastErr = err
		fmt.Printf("[handleAPReview] AP审核失败(尝试%d/%d): %v\n", retry+1, maxRetries, err)
	}
	if err != nil {
		err = lastErr
		fmt.Printf("[handleAPReview] AP原始API %d次均失败，尝试降级PM的API...\n", maxRetries)

		errStr := err.Error()
		isNetworkErr := strings.Contains(errStr, "deadline") ||
			strings.Contains(errStr, "timeout") ||
			strings.Contains(errStr, "connection") ||
			strings.Contains(errStr, "no such host") ||
			strings.Contains(errStr, "EOF")

		if isNetworkErr && m.config.APIConfig.BaseURL != "" {
			fmt.Println("[handleAPReview] ⚠️ 切换到PM的API，重试...")
			m.addAPToUserMsg("⚠️ AP API连接失败，切换到PM的API...")

			pmClient := ai.NewClient(m.config.APIConfig)
			fallbackProcessor := ai.NewAPProcessor(pmClient, m.workDir)

			for pmRetry := 0; pmRetry < maxRetries; pmRetry++ {
				if pmRetry > 0 {
					pmTimeout := time.Duration(60*(pmRetry+1)) * time.Second
					fmt.Printf("[handleAPReview] 🔄 PM API第%d次重试(超时%ds)...\n", pmRetry, pmTimeout/time.Second)
					m.addAPToUserMsg(fmt.Sprintf("⚠️ PM API审批重试中(%d/%d)...", pmRetry, maxRetries-1))
					pmCtx, pmCancel := context.WithTimeout(safeCtx, pmTimeout)
					defer pmCancel()
					fallbackProcessor.SetContext(pmCtx)
				}
				resp, err = fallbackProcessor.ProcessReview(reviewMsg, apHistory, func(delta string) {
					m.emitStreamChunk("ap", delta)
				})
				if err == nil {
					fmt.Println("[handleAPReview] ✅ PM API审核成功")
					break
				}
				fmt.Printf("[handleAPReview] PM API失败(尝试%d/%d): %v\n", pmRetry+1, maxRetries, err)
			}
		}

		if err != nil {
			m.currentRole = ""
			errStr = err.Error()
			isToolErr := strings.Contains(errStr, "tool choice") || strings.Contains(errStr, "tool_choice") ||
				strings.Contains(errStr, "enable-auto-tool-choice") || strings.Contains(errStr, "tool-call-parser")

			if isToolErr {
				fmt.Println("[handleAPReview] ⚠️ 模型不支持工具调用，尝试无工具模式...")
				retryResp, retryErr := m.apProcessor.ProcessReviewNoTools(reviewMsg, apHistory, func(delta string) {
					m.emitStreamChunk("ap", delta)
				})
				if retryErr != nil {
					fmt.Printf("[handleAPReview] 无工具模式也失败: %v\n", retryErr)
					m.addAPToUserMsg(fmt.Sprintf("❌ AP审批API调用失败: %v\n\n请联系PM人工审核或检查API配置后重试。", retryErr))
					m.cMonitor.UpdateProjectState(types.ProjectStateError)
					m.currentRole = ""
					if m.onProjectStateChanged != nil {
						m.onProjectStateChanged("error")
					}
					return fmt.Errorf("AP review failed (no-tools fallback also failed): %w", retryErr)
				}
				resp = retryResp
			} else {
				errMsg := fmt.Sprintf("❌ AP审批API调用失败: %v\n\n请联系PM人工审核或检查API配置后重试。", err)
				m.addAPToUserMsg(errMsg)
				m.cMonitor.UpdateProjectState(types.ProjectStateError)
				m.currentRole = ""
				if m.onProjectStateChanged != nil {
					m.onProjectStateChanged("error")
				}
				return fmt.Errorf("AP review failed: %w", err)
			}
		}
	}

	cleanContent := strings.TrimSpace(resp.Content)
	if cleanContent == "" || cleanContent == "@USR" || cleanContent == "@USR " {
		fmt.Printf("[handleAPReview] ⚠️ AP回复为空，视为通过\n")
		m.addAPToUserMsg(i18n.T("msg.ap_approved"))
		m.forceProjectApproved()
		return nil
	}

	// 添加AP回复到历史
	m.addAPToUserMsg(resp.Content)

	// 解析AP是否@了PM（用于来回沟通）
	parsedMsg := m.router.Parse("ap", resp.Content)

	if parsedMsg.To == "pm" {
		fmt.Printf("[System] AP @PM detected，返回给PM处理: %s\n", parsedMsg.Content)
		m.currentRole = ""

		if m.apReviewCount >= 5 {
			fmt.Printf("[System] AP-PM沟通轮次过多(%d)，强制结束\n", m.apReviewCount)
			m.addAPToUserMsg(i18n.T("msg.ap_retry_exceeded"))
			m.forceProjectApproved()
			return nil
		}

		return m.handleToPM(fmt.Sprintf("[AP反馈] %s\n\n请根据AP的意见决定下一步（可以解释、让SE修改、或继续沟通）", parsedMsg.Content))
	}

	if resp.NeedRework && !resp.Approved {
		fmt.Printf("[System] AP不通过，需要返工，返回给PM决策\n")
		m.currentRole = ""

		if m.apReviewCount >= 5 {
			fmt.Printf("[System] AP返工次数过多(%d)，强制结束\n", m.apReviewCount)
			m.addAPToUserMsg(i18n.T("msg.ap_rework_exceeded"))
			m.cMonitor.UpdateProjectState(types.ProjectStateError)
			if m.onProjectStateChanged != nil {
				m.onProjectStateChanged("error")
			}
			return nil
		}

		reworkMsg := fmt.Sprintf("[❌ AP审批未通过]\n\n%s\n\n请决定：可以让SE修改，也可以向AP解释说明。", resp.ReworkMsg)
		return m.handleToPM(reworkMsg)
	}

	// ✅ AP审批通过 → 项目正式结束！
	if resp.Approved {
		fmt.Println("[TRACE-AP] ✅✅ AP审批通过 → forceProjectApproved (done_approved)!")
		m.SetHandoverPending(HandoverAPToDone)
		m.forceProjectApproved()
		return nil
	}

	// ⚠️ AP未给出明确的approve/reject判断，走兜底：检查文本中的拒绝关键词
	lowerContent := strings.ToLower(resp.Content)
	maybeReject := strings.Contains(lowerContent, "不通过") ||
		strings.Contains(lowerContent, "未通过") ||
		strings.Contains(lowerContent, "拒绝") ||
		strings.Contains(lowerContent, "驳") ||
		strings.Contains(lowerContent, "reject") ||
		strings.Contains(lowerContent, "fail")
	if maybeReject {
		fmt.Println("[TRACE-AP] ⚠️ AP输出含拒绝关键词但未解析到结构化判断，强制返工")
		return m.handleToPM(fmt.Sprintf("[❌ AP疑似拒绝]\n%s\n请根据AP意见决定下一步", resp.Content))
	}

	lowerContent = strings.ToLower(resp.Content)
	maybeApprove := strings.Contains(lowerContent, "通过") ||
		strings.Contains(lowerContent, "approved") ||
		strings.Contains(lowerContent, "✅") ||
		strings.Contains(lowerContent, "pass")
	if maybeApprove {
		fmt.Println("[TRACE-AP] ⚠️ AP输出含通过关键词但未解析到结构化判断，视为通过")
		m.SetHandoverPending(HandoverAPToDone)
		m.forceProjectApproved()
		return nil
	}

	fmt.Printf("[TRACE-AP] ⚠️ AP未给出明确判断(Approved=false, NeedRework=false)，兜底拒绝\n")
	return m.handleToPM(fmt.Sprintf("[❌ AP审批结果不明确]\n%s\n\n请PM确认是否通过", resp.Content))
}

// forceProjectApproved AP批准后项目正式完成（状态=done_approved）
func (m *Manager) forceProjectApproved() {
	fmt.Printf("[TRACE-AP] 🏁 forceProjectApproved! 项目正式完成 (时间:%s)\n", time.Now().Format("15:04:05"))
	m.PrintStreamAuditReport()
	m.clearStreamMessageIDs()
	m.ClearHandover()
	m.currentRole = ""
	m.reviewCount = 0
	m.apReviewCount = 0
	m.apMode = RoleModeIdle
	m.pmReviewCycles = 0
	m.seContinueCount = 0
	m.seAskPMCount = 0
	m.seReportedComplete = false

	// 标记AP审核任务为完成
	if m.taskManager != nil {
		m.taskManager.CompleteLastTaskByRole("AP")
	}

	m.cMonitor.UpdateSeStatus(types.RoleStatusIdle)
	m.cMonitor.UpdatePmStatus(types.RoleStatusIdle)
	m.msgBusSend("system", "", "done", PathSystem, "completeTask:done_approved", map[string]string{"status": "approved"})
	m.cMonitor.UpdateProjectState(types.ProjectStateApproved)
	m.boardManager.MarkDone()

	if m.memoryManager != nil {
		m.memoryManager.ClearState()
	}

	if m.onProjectStateChanged != nil {
		m.onProjectStateChanged("approved")
	}

	// 弹窗通知用户项目已完成
	if m.ctx != nil {
		go func() {
			runtime.MessageDialog(m.ctx, runtime.MessageDialogOptions{
				Type:    runtime.InfoDialog,
				Title:   "✅ 项目已完成",
				Message: "AP审批已通过，项目成功完成！",
			})
		}()
	}

	// [v0.7.2] PM 最终总结：AP通过后PM向用户汇报完成情况 + 更新TODO
	go m.PMFinalSummary()
}

// PMFinalSummary AP审批通过后PM向用户做最终汇报（导出供Bridge路径调用）
func (m *Manager) PMFinalSummary() {
	if m.pmProcessor == nil {
		return
	}

	fmt.Println("[PM-Final] 开始最终汇报...")

	msg := "@PM 所有任务已通过AP审批，请向用户做最终汇报摘要，并调用 update_todo 将所有待办标记为 done。"

	// 构建PM历史
	history := m.GetHistory()
	pmHistory := make([]ai.ChatMessage, len(history))
	for i, msg := range history {
		pmHistory[i] = ai.ChatMessage{Role: msg.Role, Content: msg.Content}
	}

	resp, err := m.pmProcessor.ProcessReview(msg, pmHistory, func(delta string) {
		// 最终汇报不需要流式输出
	})
	if err != nil {
		fmt.Printf("[PM-Final] ❌ 最终汇报失败: %v\n", err)
		return
	}

	content := strings.TrimSpace(resp.Content)
	if content != "" {
		m.addPMToUserMsg(content)
		m.WriteDebugLog(fmt.Sprintf("[PM-Final] 最终汇报已发送 (%d chars)", len(content)))
	}
}

// forceProjectDone 强制结束项目（清除状态、通知前端）- 旧版兼容，状态=done
func (m *Manager) forceProjectDone() {
	m.ClearHandover()
	m.currentRole = ""
	m.reviewCount = 0
	m.apReviewCount = 0
	m.apMode = RoleModeIdle

	// 发射角色状态为idle（解决前端状态灯残留问题）
	if m.msgBus != nil {
		m.msgBus.EmitState(RoleState{
			Phase: "done",
			PM:    "idle",
			SE:    "idle",
			AP:    "idle",
		})
	}

	m.msgBusSend("system", "", "done", PathSystem, "completeTask:done_completed", map[string]string{"status": "completed"})
	m.cMonitor.UpdateProjectState(types.ProjectStateDone)
	m.boardManager.MarkDone() // [TAG-D6]

	if m.memoryManager != nil {
		m.memoryManager.ClearState()
	}

	if m.onProjectStateChanged != nil {
		m.onProjectStateChanged("done")
	}

	// 弹窗通知用户项目已完成
	if m.ctx != nil {
		go func() {
			runtime.MessageDialog(m.ctx, runtime.MessageDialogOptions{
				Type:    runtime.InfoDialog,
				Title:   "项目已完成",
				Message: "任务已完成！",
			})
		}()
	}
}

type ResetTrigger string

const (
	ResetTriggerUser     ResetTrigger = "user"
	ResetTriggerPM       ResetTrigger = "pm"
	ResetTriggerCMonitor ResetTrigger = "c_monitor"
)

func (m *Manager) ExecuteReset(reason string, trigger string) error {
	fmt.Printf("[RESET] 开始执行复位: reason=%s, trigger=%s\n", reason, trigger)

	if err := m.archiveResetLog(reason, string(trigger)); err != nil {
		fmt.Printf("[RESET] ⚠️ 归档失败: %v\n", err)
	}

	m.mu.Lock()
	m.history = []Message{}
	m.msgCounter = 0
	m.lastMsgIDs = make(map[string]string)
	m.currentRole = ""
	m.reviewCount = 0
	m.apReviewCount = 0
	m.apMode = RoleModeIdle
	m.pmReviewCycles = 0
	m.seContinueCount = 0
	m.seAskPMCount = 0
	m.seReportedComplete = false
	m.isRecovering = false
	m.userStopped = false
	m.pmWaitingForUserSince = 0
	m.pmWaitingNudgeCount = 0
	m.isProcessing = false // 🔴 关键：复位时重置处理状态
	if m.cancelFunc != nil {
		m.cancelFunc()
		m.cancelFunc = nil
	}
	m.mu.Unlock()

	m.cMonitor.UpdateProjectState(types.ProjectStateIdle)
	m.cMonitor.UpdatePmStatus(types.RoleStatusIdle)
	m.cMonitor.ResetSessionState()
	m.cMonitor.ResetRetryFlag()

	// 递增复位世代号，后续返回的幽灵调用会被拦截
	m.resetMu.Lock()
	m.resetGeneration++
	m.resetMu.Unlock()

	if m.memoryManager != nil {
		m.memoryManager.ClearState()
	}

	// 清空全局任务列表
	if m.taskManager != nil {
		m.taskManager.ClearTasks()
	}

	m.msgBusSend("system", "", "reset", PathSystem, "ResetController:reset", map[string]string{
		"reason":  reason,
		"trigger": string(trigger),
	})

	fmt.Printf("[RESET] ✅ 复位完成\n")
	return nil
}

// ClearGlobalTasks 清空全局任务列表
func (m *Manager) ClearGlobalTasks() {
	if m.taskManager != nil {
		m.taskManager.ClearTasks()
	}
}

func (m *Manager) archiveResetLog(reason, trigger string) error {
	workDir := m.workDir
	if workDir == "" {
		return fmt.Errorf("workDir 未设置，无法归档")
	}

	resetDir := filepath.Join(workDir, ".argus", "resets")
	os.MkdirAll(resetDir, 0755)

	timestamp := time.Now().Format("20060102_150405")
	filename := fmt.Sprintf("reset_%s.json", timestamp)
	filePath := filepath.Join(resetDir, filename)

	entry := map[string]interface{}{
		"timestamp": time.Now().Format(time.RFC3339),
		"reason":    reason,
		"trigger":   trigger,
		"role":      m.currentRole,
		"state":     m.cMonitor.GetProjectState(),
		"history_count_before": func() int {
			m.mu.RLock()
			defer m.mu.RUnlock()
			return len(m.history)
		}(),
	}

	data, _ := json.MarshalIndent(entry, "", "  ")
	return os.WriteFile(filePath, data, 0644)
}

func (m *Manager) AddTodo(description string) string {
	m.todoMu.Lock()
	defer m.todoMu.Unlock()

	if len(m.todoList) >= m.todoMaxSize {
		m.todoList = m.todoList[1:]
	}

	now := time.Now().Unix()
	item := TodoItem{
		ID:          fmt.Sprintf("todo_%d", now),
		Description: description,
		Status:      "pending",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	m.todoList = append(m.todoList, item)

	// SSE推送TODO更新
	m.emitTodoUpdate()
	return item.ID
}

func (m *Manager) UpdateTodoStatus(id, status string) {
	m.todoMu.Lock()
	defer m.todoMu.Unlock()
	for i := range m.todoList {
		if m.todoList[i].ID == id {
			m.todoList[i].Status = status
			m.todoList[i].UpdatedAt = time.Now().Unix()
			break
		}
	}

	// SSE推送TODO更新
	m.todoMu.RLock()
	todoCopy := make([]TodoItem, len(m.todoList))
	copy(todoCopy, m.todoList)
	m.todoMu.RUnlock()
	m.msgBusSend("system", "", "todo_update", PathSystem, "updateTodoList:todo_update", todoCopy)
}

func (m *Manager) GetTodoList() []TodoItem {
	m.todoMu.RLock()
	defer m.todoMu.RUnlock()
	result := make([]TodoItem, len(m.todoList))
	copy(result, m.todoList)
	return result
}

func (m *Manager) ClearTodoList() {
	m.todoMu.Lock()
	defer m.todoMu.Unlock()
	m.todoList = []TodoItem{}

	// SSE推送TODO status clear
	m.msgBusSend("system", "", "todo_update", PathSystem, "ClearTodoList:todo_clear", []TodoItem{})
}

// emitTodoUpdate SSE推送TODO列表变更
func (m *Manager) emitTodoUpdate() {
	m.todoMu.RLock()
	defer m.todoMu.RUnlock()
	todoCopy := make([]TodoItem, len(m.todoList))
	copy(todoCopy, m.todoList)
	m.msgBusSend("system", "", "todo_update", PathSystem, "emitTodoUpdate:todo_update", todoCopy)
}

// addAPToUserMsg 添加AP消息到历史并通知用户
func (m *Manager) addAPToUserMsg(content string) {
	content = strings.TrimSpace(content)
	if content == "" {
		fmt.Println("[AP→USR] ⚠️ 内容为空，跳过发送")
		return
	}

	content = strings.TrimPrefix(content, "@USR ")
	content = strings.TrimPrefix(content, "@USR")

	apMsg := Message{
		From:      "ap",
		To:        "user",
		Role:      "ap",
		Content:   content,
		Raw:       content,
		Source:    "ap_review",
		Timestamp: time.Now(),
		ReplyTo:   m.getReplyToID("ap"),
	}

	m.mu.Lock()
	m.history = append(m.history, apMsg)
	if len(m.history) > 200 {
		trimLen := len(m.history) - 200
		m.history = m.history[trimLen:]
	}
	m.mu.Unlock()

	fmt.Printf("[AP→USR] %s\n", content[:min(200, len(content))])

	m.writeConversationLog(apMsg)

	if m.onMessageAdded != nil {
		m.onMessageAdded(apMsg)
	}

	m.msgBusSend("ap", content, "ap_message", PathAPToUser, "addAPToUserMsg", map[string]string{"delta": content})

	go func() {
		defer func() {
			if r := recover(); r != nil {
				fmt.Printf("[AP-DingTalk] 💥 panic recovered: %v\n", r)
			}
		}()
		filteredContent := filterDuplicateMentions(content)
		m.sendToDingTalk(fmt.Sprintf("[AP→USR] %s", filteredContent))
	}()
}

// Stop 停止管理器
func (m *Manager) Stop() {
	if m.cMonitor != nil {
		m.cMonitor.Stop()
	}
}

// Shutdown 关闭管理器（持久化状态清理）
func (m *Manager) Shutdown() {
	fmt.Println("[Manager] 🛑 Shutdown: 清理会话状态...")
	m.StopGoroutines()
	if m.cMonitor != nil {
		m.cMonitor.Stop()
		_ = m.cMonitor.ResetSessionState()
	}
	if m.memoryManager != nil {
		_ = m.memoryManager.ClearState()
	}
	_ = m.boardManager.Reset()
	fmt.Println("[Manager] ✅ Shutdown 完成")
}

// GetCMonitor 获取C监控（用于外部调用）
func (m *Manager) GetCMonitor() *monitor.CMonitor {
	return m.cMonitor
}

// GetProjectState 获取当前项目状态（用于启动时判断是否清空旧消息）
func (m *Manager) GetProjectState() string {
	state, err := m.cMonitor.ReadState()
	if err != nil {
		return "unknown"
	}
	switch state.ProjectState {
	case types.ProjectStateIdle:
		return "idle"
	case types.ProjectStateRunning:
		return "running"
	case types.ProjectStateDone:
		return "done"
	case types.ProjectStateApproved:
		return "approved"
	default:
		return fmt.Sprintf("%d", state.ProjectState)
	}
}

// GetChatManagerStatus 获取 ChatManager 状态（供 C 监控调用）
func (m *Manager) GetChatManagerStatus() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return map[string]interface{}{
		"initialized":        true,
		"messageCount":       len(m.history),
		"currentRole":        m.currentRole,
		"workDir":            m.workDir,
		"hasMemoryManager":   m.memoryManager != nil,
		"seReportedComplete": m.seReportedComplete,  // [FIX-20260528-D] 暴露SE完成状态
	}
}

// IsSETaskCompleted [FIX-20260528-D] 供C监控判断SE是否已报告任务完成
func (m *Manager) IsSETaskCompleted() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.seReportedComplete
}

func (m *Manager) GetHandoverState() HandoverState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.handover
}

func (m *Manager) SetHandoverPending(step HandoverStep) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.handover.Pending || m.handover.Step != step {
		m.handover = HandoverState{
			Step:    step,
			Pending: true,
			Since:   time.Now().Unix(),
		}
		fmt.Printf("[Handover] 📋 待交接: %s\n", step)
	}
}

func (m *Manager) ClearHandover() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.handover = HandoverState{}
}

func (m *Manager) IncrementHandoverNudge() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.handover.NudgeCount++
	return m.handover.NudgeCount
}

func (m *Manager) MarkHandoverForced() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.handover.Forced = true
}

func (m *Manager) GetLastRoleMessage(role string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for i := len(m.history) - 1; i >= 0; i-- {
		if m.history[i].From == role {
			return m.history[i].Content
		}
	}
	return ""
}

func (m *Manager) handleForceHandover(step string, forced bool) error {
	fmt.Printf("[ForceHandover] C强制执行交接: step=%s, forced=%v\n", step, forced)

	switch HandoverStep(step) {
	case HandoverSEToPM:
		m.ClearHandover()
		return m.handleToPM("⚠️ [C强制] SE已完成，请立即审核 [TAG-F1]")

	case HandoverPMToAP:
		m.ClearHandover()
		if m.apProcessor != nil {
			return m.handleAPReview("⚠️ [C强制] PM已approve但未移交，AP请审批 [TAG-F2]")
		}
		m.forceProjectApproved()
		return nil

	case HandoverAPToDone:
		m.ClearHandover()
		m.apMode = RoleModeIdle
		m.addAPToUserMsg(i18n.T("msg.monitor_force_approve"))
		m.forceProjectApproved()
		return nil // [TAG-F3]

	default:
		return fmt.Errorf("unknown handover step: %s", step)
	}
}

// SetUserStopped 设置用户停止标志（防止C无限催促恢复的任务）
func (m *Manager) SetUserStopped(stopped bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.userStopped = stopped
	if stopped {
		fmt.Println("[Manager] ⛔ 用户主动停止，C将不再催促恢复任务")
		if m.memoryManager != nil {
			m.memoryManager.SetStopped(true) // 防止autosave重新创建文件
			if err := m.memoryManager.ClearTaskMemory(); err != nil {
				fmt.Printf("[Manager] ⚠️ 清理记忆文件失败: %v\n", err)
			} else {
				fmt.Println("[Manager] ✅ 已清理记忆文件")
			}
		}
	} else {
		fmt.Println("[Manager] ✅ 用户停止标志已重置")
		if m.memoryManager != nil {
			m.memoryManager.SetStopped(false)
		}
	}
}

// IsUserStopped 获取用户停止状态
func (m *Manager) IsUserStopped() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.userStopped
}

// StopCurrentTask 停止当前正在执行的任务（取消AI调用 + 重置状态）
func (m *Manager) StopCurrentTask() {
	fmt.Println("[Manager] 🛑 StopCurrentTask 被调用")

	m.mu.Lock()
	if m.cancelFunc != nil {
		fmt.Println("[Manager] 🛑 取消正在进行的AI调用")
		m.cancelFunc()
		m.cancelFunc = nil
	}
	m.mu.Unlock()

	m.processingMu.Lock()
	if m.isProcessing {
		fmt.Println("[Manager] 🛑 重置处理状态")
		m.isProcessing = false
	}
	m.processingMu.Unlock()

	// 🧹 清空待处理消息队列
	m.pendingMu.Lock()
	if len(m.pendingQueue) > 0 {
		fmt.Printf("[Manager] 🧹 清空待处理消息队列 (%d条)\n", len(m.pendingQueue))
		m.pendingQueue = nil
	}
	m.pendingMu.Unlock()

	m.currentRole = ""
	m.seContinueCount = 0
	m.seAskPMCount = 0
	m.seReportedComplete = false

	// 🧹 清理路由器状态（防止router.isProcessing卡住导致PM静默不响应）
	m.router.ForceClear()

	if m.cMonitor != nil {
		m.cMonitor.UpdatePmStatus(types.RoleStatusIdle)
		m.cMonitor.UpdateSeStatus(types.RoleStatusIdle)
		m.cMonitor.UpdateProjectState(types.ProjectStateIdle)
		m.cMonitor.ClearLastUserMessage()
	}

	if m.memoryManager != nil {
		_ = m.memoryManager.ClearState()
	}

	m.msgBusSend("system", "", "done", PathSystem, "stopCurrentTask:done_cancelled", map[string]string{"status": "cancelled"})

	fmt.Println("[Manager] 🛑 当前任务已停止")

	_ = m.boardManager.Reset()
}

// GetMemoryStatus 获取记忆系统状态（供 C 监控调用）
func (m *Manager) GetMemoryStatus() map[string]interface{} {
	if m.memoryManager == nil {
		return map[string]interface{}{
			"initialized": false,
			"error":       "MemoryManager 未初始化",
		}
	}

	// 使用正确的判断方法
	hasUnfinished, memory, err := m.memoryManager.HasUnfinishedTask()
	if err != nil {
		return map[string]interface{}{
			"initialized":   true,
			"hasUnfinished": false,
			"error":         err.Error(),
		}
	}

	result := map[string]interface{}{
		"initialized":   true,
		"hasUnfinished": hasUnfinished,
	}

	if memory != nil {
		result["lastTask"] = map[string]interface{}{
			"userRequest":     memory.UserRequest,
			"currentState":    memory.CurrentState,
			"currentRole":     memory.CurrentRole,
			"taskDescription": memory.TaskDescription,
			"messageCount":    memory.MessageCount,
			"lastActiveTime":  memory.LastActiveTime.Format("2006-01-02 15:04:05"),
		}
	}

	return result
}

// AddMCMessage 添加 C 监控消息（供 App 调用）
func (m *Manager) AddMCMessage(content string) {
	m.addHistory(Message{
		From:    "mc",
		To:      "pm",
		Role:    "mc",
		Content: content,
		Source:  "mc_internal",
	})
}

// ========== 配置管理接口 ==========

// GetConfigManager 获取配置管理器实例
func (m *Manager) GetConfigManager() *ConfigManager {
	return m.configManager
}

// [G63] GetMessageBus 获取MessageBus实例
func (m *Manager) GetMessageBus() *MessageBus {
	return m.msgBus
}

func (m *Manager) GetExecutor() *executor.Executor {
	return m.seExecutor
}

func (m *Manager) GetSEProcessor() *ai.SEProcessor {
	return m.seProcessor
}

func (m *Manager) GetSSEBridge() *SSEBridge {
	return m.sseBridge
}

func (m *Manager) GetBackendStatus() *BackendStatus {
	return m.backendStatus
}

func getMapKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func (m *Manager) pushSSEEvent(eventType string, data interface{}) {
	if m.sseBridge == nil {
		return
	}
	m.sseBridge.PushEvent(eventType, data)
	fmt.Printf("[SSE] → %s | data=%v\n", eventType, data)
}

func (m *Manager) syncBackendStatus(stage string, event string) {
	if m.backendStatus == nil || m.cMonitor == nil {
		return
	}
	m.backendStatus.Stage = stage
	m.backendStatus.LastEvent = event
	m.backendStatus.UpdatedAt = time.Now().Unix()
	m.backendStatus.MessageCount++
	state, _ := m.cMonitor.ReadState()
	m.backendStatus.PMStatus = state.PmStatus
	m.backendStatus.SEStatus = state.SeStatus
	m.backendStatus.ProjectState = state.ProjectState
	m.backendStatus.CurrentRole = m.currentRole
	m.backendStatus.CurrentSETask = m.currentSETask // [FIX-20260529] 同步SE任务描述
	m.pushSSEEvent("status_update", m.backendStatus)
}

// GetConfigStatus 获取配置系统状态（供 C 监控调用）
func (m *Manager) GetConfigStatus() map[string]interface{} {
	if m.configManager == nil {
		return map[string]interface{}{
			"initialized": false,
			"error":       "ConfigManager 未初始化",
		}
	}
	return m.configManager.GetConfigStatus()
}

// CheckDecision 检查操作是否需要人工确认（供 SE 执行前调用）
func (m *Manager) CheckDecision(decisionType types.DecisionType) (bool, string, error) {
	if m.configManager == nil {
		return false, "", fmt.Errorf("ConfigManager 未初始化")
	}
	return m.configManager.CheckDecision(decisionType)
}

// CheckPermission 检查文件路径权限（供 SE 操作文件前调用）
func (m *Manager) CheckPermission(operation string, filePath string) (types.PermissionLevel, string, bool) {
	if m.configManager == nil {
		return types.PermNoAccess, "ConfigManager 未初始化", false
	}
	return m.configManager.CheckPermission(operation, filePath)
}

func (m *Manager) GetEnvMemory() *types.EnvMemory {
	if m.envMemory == nil {
		return nil
	}
	m.envMemory.mu.RLock()
	defer m.envMemory.mu.RUnlock()
	return m.envMemory.data
}

func (m *Manager) LearnTool(name, path string) error {
	if m.envMemory == nil {
		return fmt.Errorf("EnvMemory 未初始化")
	}
	if m.envMemory.LearnTool(name, path, "user") {
		return nil
	}
	return fmt.Errorf("%s", i18n.T("err.tool_record_failed", name))
}

func (m *Manager) GetToolPath(name string) (string, bool) {
	if m.envMemory == nil {
		return "", false
	}
	return m.envMemory.GetTool(name)
}

func (m *Manager) EnvMemorySummary() string {
	if m.envMemory == nil {
		return ""
	}
	return m.envMemory.Summary()
}

// SetWorkDir 更新工作目录（当用户切换项目时调用）
func (m *Manager) SetWorkDir(workDir string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	fmt.Printf("[Manager] 更新工作目录: %s -> %s\n", m.workDir, workDir)
	m.workDir = workDir

	// 更新各组件的工作目录
	m.seExecutor.SetWorkDir(workDir)
	m.pmExecutor.SetWorkDir(workDir)
	m.cMonitor.SetWorkDir(workDir)
}

// SetOnMessageAdded 设置消息添加回调（用于推送到前端）
func (m *Manager) SetOnMessageAdded(callback MessageCallback) {
	m.onMessageAdded = callback
}

// SetOnProjectStateChanged 设置项目状态变更回调
func (m *Manager) SetOnProjectStateChanged(callback func(string)) {
	m.onProjectStateChanged = func(stateStr string) {
		if callback != nil {
			callback(stateStr)
		}

		// 出错时弹窗通知用户
		if stateStr == "error" && m.ctx != nil {
			go func() {
				runtime.MessageDialog(m.ctx, runtime.MessageDialogOptions{
					Type:    runtime.ErrorDialog,
					Title:   "❌ 项目出错",
					Message: "项目执行过程中出现错误，请查看聊天面板了解详情。",
				})
			}()
		}
	}
}

// GetPendingQueue 获取待发送消息队列
func (m *Manager) GetPendingQueue() []string {
	m.pendingMu.Lock()
	defer m.pendingMu.Unlock()
	queue := make([]string, len(m.pendingQueue))
	copy(queue, m.pendingQueue)
	return queue
}

// ClearPendingQueue 清空待发送消息队列
func (m *Manager) ClearPendingQueue() {
	m.pendingMu.Lock()
	defer m.pendingMu.Unlock()
	if len(m.pendingQueue) > 0 {
		fmt.Printf("[Manager] 🧹 清空待发送消息队列 (%d条)\n", len(m.pendingQueue))
		m.pendingQueue = nil
	}
}

// PopAndSendPending 弹出并发送第一条待发送消息
func (m *Manager) PopAndSendPending() string {
	m.pendingMu.Lock()
	defer m.pendingMu.Unlock()
	if len(m.pendingQueue) == 0 {
		return ""
	}
	msg := m.pendingQueue[0]
	m.pendingQueue = m.pendingQueue[1:]
	fmt.Printf("[Manager] 📤 立即发送待处理消息: %s (剩余%d条)\n", msg, len(m.pendingQueue))
	go func() {
		defer func() {
			if r := recover(); r != nil {
				fmt.Printf("[PopAndSendPending] 💥 panic recovered: %v\n", r)
				m.processingMu.Lock()
				m.isProcessing = false
				m.processingMu.Unlock()
			}
		}()
		m.ProcessMessage(msg)
	}()
	return msg
}

// getResetGeneration 获取当前复位世代号
func (m *Manager) getResetGeneration() int64 {
	m.resetMu.RLock()
	defer m.resetMu.RUnlock()
	return m.resetGeneration
}

// isGhostCall 检测是否为复位后返回的幽灵AI调用（复位后generation已递增，旧调用会不匹配）
func (m *Manager) isGhostCall(oldGeneration int64) bool {
	return oldGeneration != m.getResetGeneration()
}

func (m *Manager) GetAIClient() *ai.Client {
	return m.aiClient
}
