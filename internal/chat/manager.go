package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"argus/internal/ai"
	"argus/internal/board"
	"argus/internal/dingtalk"
	"argus/internal/executor"
	"argus/internal/i18n"
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

// Manager 对话管理器
type Manager struct {
	router          *Router
	aiClient        *ai.Client
	pmProcessor     *ai.PMProcessor
	seProcessor     *ai.SEProcessor
	apProcessor     *ai.APProcessor
	pmExecutor      *executor.Executor
	seExecutor      *executor.Executor
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
	seContinueCount       int           // SE连续继续次数（防无限循环）
	seAskPMCount          int           // SE连续问PM次数（防needHelp死循环）
	seReportedComplete    bool          // SE是否已报告完成（防重复报告）
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
func NewManager(config types.Config, workDir string) (*Manager, error) {
	// 创建AI客户端
	aiClient := ai.NewClient(config.APIConfig)

	// 初始化看板
	boardManager := board.NewManager(".argus/board.json")
	if err := boardManager.Load(); err != nil {
		return nil, fmt.Errorf("load board failed: %v", err)
	}

	// 初始化执行器
	pmExecutor := executor.NewExecutor(workDir, boardManager)
	seExecutor := executor.NewExecutor(workDir, boardManager)

	// 初始化AI处理器
	pmProcessor := ai.NewPMProcessor(aiClient, workDir, nil)
	seProcessor := ai.NewSEProcessor(aiClient, workDir)
	apProcessor := ai.NewAPProcessor(aiClient, workDir)

	manager := &Manager{
		router:        NewRouter(),
		aiClient:      aiClient,
		pmProcessor:   pmProcessor,
		seProcessor:   seProcessor,
		apProcessor:   apProcessor,
		pmExecutor:    pmExecutor,
		seExecutor:    seExecutor,
		boardManager:  boardManager,
		memoryManager: NewMemoryManager(workDir),
		sseBridge:     NewSSEBridge(),
		backendStatus: &BackendStatus{Stage: "idle", UpdatedAt: time.Now().Unix()},
		history:       []Message{},
		lastMsgIDs:    make(map[string]string),
		currentRole:   "user",
		workDir:       workDir,
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
	)

	// 初始化三层模型 Builder（用于 PM/SE 可视化）
	manager.richBuilder = NewRichMessageBuilder(manager.emitWailsEvent)
	pmProcessor.SetShellEmitter(manager.richBuilder)

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

// UpdateAPIConfig 更新API配置并重建AI客户端
func (m *Manager) UpdateAPIConfig(apiConfig types.APIConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.config.APIConfig = apiConfig
	m.aiClient = ai.NewClient(apiConfig)
	m.pmProcessor = ai.NewPMProcessor(m.aiClient, m.workDir, func(state int) {
		fmt.Printf("[PM] 通过Function Call更新项目状态: %d\n", state)
		if m.cMonitor != nil {
			m.cMonitor.UpdateProjectState(state)
		} else {
			fmt.Printf("[PM] ⚠️ cMonitor 未初始化，跳过状态更新\n")
		}
	})
	m.seProcessor = ai.NewSEProcessor(m.aiClient, m.workDir)
	m.apProcessor = ai.NewAPProcessor(m.aiClient, m.workDir)
	m.pmProcessor.ReplyLanguage = m.ReplyLanguage
	m.seProcessor.ReplyLanguage = m.ReplyLanguage

	// 恢复TODO回调
	m.pmProcessor.SetTodoCallbacks(
		func(desc string) string { return m.AddTodo(desc) },
		func(id, status string) { m.UpdateTodoStatus(id, status) },
	)

	fmt.Printf("[Manager] API配置已更新: BaseURL=%s, Model=%s\n", apiConfig.BaseURL, apiConfig.Model)
}

// UpdateAPConfig 更新AP的独立API配置（如果为空则复用PM的）
func (m *Manager) UpdateAPConfig(apConfig types.APIConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if apConfig.BaseURL == "" || apConfig.APIKey == "" {
		fmt.Println("[Manager] AP未配置独立API，复用PM的AI客户端")
		m.apProcessor = ai.NewAPProcessor(m.aiClient, m.workDir)
	} else {
		apClient := ai.NewClient(apConfig)
		m.apProcessor = ai.NewAPProcessor(apClient, m.workDir)
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
	logPath := filepath.Join(m.workDir, ".argus", "conversation.log")
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
	if m.ctx != nil {
		runtime.EventsEmit(m.ctx, "project_approved", map[string]interface{}{
			"timestamp": time.Now().Unix(),
			"action":    "clear_messages",
		})
		fmt.Println("[TRACE-AP] ✅ 发送 project_approved 事件（清空messages）")
	}
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
		// 重新创建AI客户端
		m.aiClient = ai.NewClient(m.config.APIConfig)
		// 重新初始化PM处理器
		m.pmProcessor = ai.NewPMProcessor(m.aiClient, m.workDir, func(state int) {
			fmt.Printf("[PM] 通过Function Call更新项目状态: %d\n", state)
			m.cMonitor.UpdateProjectState(state)
		})
		// 恢复TODO回调
		m.pmProcessor.SetTodoCallbacks(
			func(desc string) string { return m.AddTodo(desc) },
			func(id, status string) { m.UpdateTodoStatus(id, status) },
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
		fmt.Println("[C] 自动重试：清除记忆、复位看板")
		if m.memoryManager != nil {
			m.memoryManager.ClearState()
		}
		m.boardManager.Reset()
		m.reviewCount = 0
		m.seContinueCount = 0
		m.seAskPMCount = 0
		m.seReportedComplete = false
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

	ctx := monitor.GenerateTimeContext(lastTime)

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
			fmt.Printf("[ProcessMessage] ⚠️ isProcessing 超时(%.0f秒)，强制清理旧任务!\n", time.Since(m.processingStartTime).Seconds())
			m.mu.Lock()
			if m.cancelFunc != nil {
				m.cancelFunc()
				m.cancelFunc = nil
			}
			m.currentRole = ""
			m.isRecovering = false
			m.mu.Unlock()
			m.cMonitor.UpdatePmStatus(types.RoleStatusIdle)
			m.cMonitor.UpdateSeStatus(types.RoleStatusIdle)
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
						m.cMonitor.UpdatePmStatus(types.RoleStatusIdle)
						m.cMonitor.UpdateSeStatus(types.RoleStatusIdle)
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
	m.cMonitor.SaveLastUserMessage(input)

	// 🔄 新任务开始，重置C的自动重试标记
	m.cMonitor.ResetRetryFlag()

	// ⏰ 保存最后交互时间（用于时间感知+社交）
	m.cMonitor.SaveLastInteractionTime(time.Now().Unix())

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
	restoreRouteLock := m.router.TempReleaseProcessing()
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
	fmt.Printf("[TRACE-PM] → handleToPM入口 content=%q state=%v currentRole=%q lastMsgFrom=%q (时间:%s)\n",
		content[:min(60, len(content))], m.cMonitor.GetProjectState(), m.currentRole, m.lastMessageFrom, time.Now().Format("15:04:05"))
	if strings.Contains(content, "已完成") || strings.Contains(content, "审核") {
		fmt.Println("[TRACE-PM] 🔍 检测到SE完成/审核关键词! 可能是SE交接消息")
	}
	m.syncBackendStatus("pm_processing", "PM开始处理: "+content[:min(40, len(content))])
	if allowed, reason := m.router.CheckTurn("pm", "handleToPM"); !allowed {
		fmt.Printf("[TRACE-PM] ❌ PM被轮换拦截: %s\n", reason)
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
	// ⏰ PM超时45秒
	pmCtx, pmCancel := context.WithTimeout(safeCtx, 45*time.Second)
	// 🔴 G点探针：handleToPM入口
	os.WriteFile("C:\\tmp\\ap_trace.log", []byte(fmt.Sprintf("[%s] handleToPM ENTRY, state=%d\n",
		time.Now().Format("2006-01-02 15:04:05.000"),
		m.cMonitor.GetProjectState())), 0644)
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

	if isReviewScenario && m.richBuilder != nil {
		return m.handlePMReviewWithRich(content, pmCtx)
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
		runtime.EventsEmit(m.ctx, "pm_started", map[string]string{})
	}

	// 更新PM状态为busy + 项目状态为running
	m.cMonitor.UpdateProjectState(types.ProjectStateRunning)
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

	fmt.Printf("[handleToPM] 调用 PMProcessor.ProcessStream\n")
	aiGen := m.getResetGeneration()
	resp, err := m.pmProcessor.ProcessStream(content, pmHistory, func(delta string) {
		m.emitStreamChunk("pm", delta)
	})
	if err != nil {
		fmt.Printf("[handleToPM] PMProcessor.ProcessStream 失败: %v\n", err)
	} else {
		fmt.Printf("[handleToPM] PMProcessor.ProcessStream 成功，响应长度: %d\n", len(resp.Content))
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
			runtime.EventsEmit(m.ctx, "error", map[string]interface{}{
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
	fmt.Printf("[DEBUG-FLOW] ProcessStream返回: content=%q len=%d\n", resp.Content[:min(80, len(resp.Content))], len(resp.Content))

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
	// 🆕 @AP 检测：必须在 addPMToUserMsg 之前！
	// 防止PM消息带 @AP 标签污染历史（P1修复）
	hasAP := strings.Contains(strings.ToLower(resp.Content), "@ap")
	fmt.Printf("[TRACE-AP-ROUTE] @AP检测: hasAP=%v content_preview=%q\n",
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
		m.addPMToUserMsg(resp.Content)
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

	// 正常路由解析（@AP 已在上面提前处理过了，此处只处理 @SE/@USR）
	parsedMsg := m.router.Parse("pm", resp.Content)

	if parsedMsg.To == "se" {
		if m.checkLoopBlock("pm", resp.Content) {
			fmt.Println("[🛡️ 防循环] PM→SE 被拦截，不走 startSETask")
			m.cMonitor.UpdatePmStatus(types.RoleStatusIdle)
			return nil
		}
		fmt.Printf("[TRACE-PM-ROUTE] ✅ PM走【@SE分支】→ startSETask | task_head=%q\n",
			parsedMsg.Content[:min(80, len(parsedMsg.Content))])

		// 移除 @SE 内容末尾的所有 task JSON（AI 常把 {"current_task":...} / {"action":...} 当 metadata 附加）
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

		// 过滤纯状态确认消息（防止PM循环发送"收到"/"待命"给SE触发死循环）
		if isStatusOnlyMessage(finalTask) {
			fmt.Printf("[handleToPM] ⚠️ PM给SE的消息是纯状态确认，跳过: %q\n", finalTask)
			m.cMonitor.UpdatePmStatus(types.RoleStatusIdle)
			return nil
		}

		currentState := m.cMonitor.GetProjectState()
		if currentState == types.ProjectStateDone || currentState == types.ProjectStateError {
			fmt.Printf("[handleToPM] 🛡️ 审核模式拦截@SE(state=%d): %s\n",
				currentState, finalTask[:min(60, len(finalTask))])

			m.reviewCount++

			if m.reviewCount >= 3 {
				fmt.Printf("[handleToPM] 🔴 审核违规@SE达%d次，强制流转AP\n", m.reviewCount)
				m.currentRole = ""
				m.cMonitor.UpdatePmStatus(types.RoleStatusIdle)
				m.SetHandoverPending(HandoverPMToAP)
				if m.apProcessor != nil {
					return m.handleAPReview("⚠️ PM多次违规@SE，系统强制移交AP审批")
				}
				m.forceProjectApproved()
				return nil
			}

			fmt.Printf("[handleToPM] ⚠️ 审核违规@SE(%d次)，流转AP\n", m.reviewCount)
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

		// 📋 记录 PM→SE 任务分配
		taskPreview := finalTask
		if len(taskPreview) > 80 {
			taskPreview = taskPreview[:80] + "..."
		}
		_ = m.boardManager.UpdateTask(taskPreview, 0)

		m.cMonitor.UpdatePmStatus(types.RoleStatusIdle)

		// 发送PM@SE到钉钉（过滤重复@）
		go func() {
			filteredContent := filterDuplicateMentions(resp.Content)
			m.sendToDingTalk(fmt.Sprintf("[PM→SE] %s", filteredContent))
		}()

		// 启动SE执行任务
		err := m.startSETaskWithFrom(finalTask, "pm")

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

	// 如果有任务JSON，启动SE执行
	if resp.HasTasks {
		fmt.Println("[System] PM created tasks, starting SE...")

		m.addPMToSEMsg(resp.Content)

		// 📋 记录 PM→SE 任务分配
		_ = m.boardManager.UpdateTask(resp.Tasks.CurrentTask, resp.Tasks.TotalSteps)

		m.cMonitor.UpdatePmStatus(types.RoleStatusIdle)

		// 发送PM创建任务到钉钉（过滤重复@）
		go func() {
			filteredTask := filterDuplicateMentions(resp.Tasks.CurrentTask)
			m.sendToDingTalk(fmt.Sprintf("[PM→SE 任务] %s", filteredTask))
		}()

		m.cMonitor.UpdateProjectState(types.ProjectStateRunning)
		return m.startSETask(resp.Tasks.CurrentTask)
	}

	// 没有@SE/没有任务 = 普通对话，PM回复用户（已在上面 addPMToUserMsg）
	fmt.Printf("[TRACE-PM-ROUTE] ⚠️ PM走【闲聊分支】! to=%q hasTasks=%v content_head=%q | AP不会启动!\n",
		parsedMsg.To, resp.HasTasks, resp.Content[:min(100, len(resp.Content))])

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

	resp, err := m.pmProcessor.ProcessStream(seQuestion, nil, func(delta string) {
		m.emitStreamChunk("pm", delta)
	})
	if err != nil {
		errMsg := fmt.Sprintf("❌ PM处理SE提问失败: %v", err)
		m.addPMToUserMsg(errMsg)
		return fmt.Errorf("PM process SE question failed: %w", err)
	}

	parsedResp := m.router.Parse("pm", resp.Content)
	if parsedResp.To == "se" {
		reviewCheck := m.extractReviewResult(parsedResp.Content)
		currentState := m.cMonitor.GetProjectState()

		if reviewCheck.Found && reviewCheck.Result == "approve" {
			fmt.Printf("[handleSEAskPM] ✅ PM回复含approve JSON → 转AP: %s [TAG-D2]\n",
				parsedResp.Content[:min(60, len(parsedResp.Content))])
			m.currentRole = ""
			m.cMonitor.UpdatePmStatus(types.RoleStatusIdle)
			m.cMonitor.UpdateProjectState(types.ProjectStateDone)
			m.SetHandoverPending(HandoverPMToAP)
			if m.apProcessor != nil {
				return m.handleAPReview("请AP进行最终质量审批")
			}
			m.forceProjectApproved()
			return nil
		}

		if currentState == types.ProjectStateDone || currentState == types.ProjectStateError {
			fmt.Printf("[handleSEAskPM] 🛡️ 审核模式拦截@SE(state=%d): %s\n",
				currentState, parsedResp.Content[:min(60, len(parsedResp.Content))])
			m.currentRole = ""
			m.cMonitor.UpdatePmStatus(types.RoleStatusIdle)
			m.SetHandoverPending(HandoverPMToAP)
			if m.apProcessor != nil {
				return m.handleAPReview("PM在审核模式下@SE，系统转交AP审批")
			}
			m.forceProjectApproved()
			return nil
		}

		fmt.Printf("[PM→SE] PM回复SE: %s\n", parsedResp.Content)
		m.addPMToSEMsg(parsedResp.Content)
		return m.startSETaskWithFrom(parsedResp.Content, "pm")
	}

	// 🔴 G点修复：handleSEAskPM 的 else 分支也需要检测 @AP！
	// 当 PM 输出 "@USR @AP 任务已验证" 时，router.Parse 只匹配 @USR，
	// 走到这个 else 分支，但之前完全没有 @AP 路由逻辑
	hasAP := strings.Contains(strings.ToLower(resp.Content), "@ap")
	fmt.Printf("[handleSEAskPM] PM回复非@SE: to=%q hasAP=%v content=%q [TAG-AP-FIX]\n",
		parsedResp.To, hasAP, resp.Content[:min(100, len(resp.Content))])

	if hasAP {
		cleanPMContent := strings.Replace(resp.Content, "@AP", "", -1)
		cleanPMContent = strings.Replace(cleanPMContent, "@ap", "", -1)
		cleanPMContent = strings.TrimSpace(cleanPMContent)
		m.addPMToUserMsg(cleanPMContent)
		fmt.Println("[handleSEAskPM] ✅ @AP命中，路由到AP审批 [TAG-AP-FIX]")
		m.currentRole = ""
		m.cMonitor.UpdatePmStatus(types.RoleStatusIdle)
		m.SetHandoverPending(HandoverPMToAP)
		if m.apProcessor != nil {
			return m.handleAPReview(cleanPMContent)
		}
		m.forceProjectApproved()
		return nil
	}

	lowerResp := strings.ToLower(resp.Content)
	hasFakeToolCall := strings.Contains(lowerResp, "list_files") ||
		strings.Contains(lowerResp, "read_file") ||
		strings.Contains(lowerResp, "exec ") ||
		strings.Contains(lowerResp, "write_file")
	hasApprovalKeywords := strings.Contains(lowerResp, "已验证") ||
		strings.Contains(lowerResp, "审核通过") ||
		strings.Contains(lowerResp, "验证通过") ||
		strings.Contains(lowerResp, "任务完成") ||
		strings.Contains(lowerResp, "已完成")
	isJustToolCall := hasFakeToolCall && !hasApprovalKeywords && !hasAP && len(strings.TrimSpace(resp.Content)) < 300

	if isJustToolCall {
		fmt.Println("[handleSEAskPM] ⚠️ 检测到PM输出伪工具调用文本(无结论)，强制转AP")
		resp.Content = "@AP [系统自动验证] SE任务已完成，请进行最终质量审批"
		m.addPMToUserMsg(strings.Replace(resp.Content, "@AP", "", 1))
		m.currentRole = ""
		m.cMonitor.UpdatePmStatus(types.RoleStatusIdle)
		m.SetHandoverPending(HandoverPMToAP)
		if m.apProcessor != nil {
			return m.handleAPReview("请AP进行最终质量审批")
		}
		m.forceProjectApproved()
		return nil
	}

	if hasApprovalKeywords && !hasAP {
		fmt.Println("[handleSEAskPM] ⚠️ 第三层保护: PM输出含审批关键词但缺@AP，自动补全并转AP")
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

	m.addPMToUserMsg(resp.Content)
	m.cMonitor.UpdatePmStatus(types.RoleStatusIdle)
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
		runtime.EventsEmit(m.ctx, "ai-stream-chunk", map[string]interface{}{
			"role":  role,
			"delta": delta,
		})
	}
}

// startSETaskWithFrom 启动SE任务，指定来源
func (m *Manager) startSETaskWithFrom(taskDesc string, from string) error {
	fmt.Printf("[TRACE-SE] 🚀 startSETask入口 from=%s task=%q (时间:%s)\n", from, taskDesc[:min(60, len(taskDesc))], time.Now().Format("15:04:05"))
	m.writeRouteLog(fmt.Sprintf("[SE-TASK] from=%s task='%s'", from, taskDesc))

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

	if allowed, reason := m.router.CheckTurnInternal("se", "se_task_"+from, true); !allowed {
		m.writeRouteLog(fmt.Sprintf("[SE-TASK] ❌ BLOCKED by turn: %s", reason))
		fmt.Printf("[startSETask] ⚠️ 轮换拦截: %s\n", reason)
		return nil
	}
	m.writeRouteLog("[SE-TASK] ✅ Turn passed, executing...")
	m.router.MarkProcessingStart("se")
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("[startSETask] 💥 panic recovered: %v\n", r)
			m.cMonitor.UpdateSeStatus(types.RoleStatusIdle)
		}
		m.router.MarkProcessingEnd("se")
	}()

	m.currentRole = "se"
	m.seContinueCount = 0
	m.seAskPMCount = 0
	m.seReportedComplete = false

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

	const seTimeout = 45 * time.Second
	const maxRetries = 2

	var resp *ai.SEResponse
	var err error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(attempt) * 5 * time.Second
			fmt.Printf("[SE] ⏳ 第%d次重试，等待%v...\n", attempt, backoff)
			m.syncBackendStatus("se_retry", fmt.Sprintf("SE第%d次重试(等待%v)", attempt, backoff))
			time.Sleep(backoff)
		}

		seCtx, seCancel := context.WithTimeout(safeCtx, seTimeout)
		m.seProcessor.SetContext(seCtx)

		if m.richBuilder != nil && attempt == 0 {
			m.richBuilder.StartTaskList("se", "SE 执行: "+taskDesc[:min(30, len(taskDesc))], []types.TaskItemDef{
				{Text: "⏳ 规划操作..."},
			})
			m.richBuilder.UpdateTask(m.richBuilder.GetCurrentTaskID(), 0, "running")
		}

		resp, err = m.seProcessor.ProcessTaskStream(taskDesc, func(delta string) {
			m.cMonitor.UpdateSeChunkTime()
			m.emitStreamChunk("se", delta)
		})
		seCancel()

		if err == nil {
			break
		}

		fmt.Printf("[SE] ❌ 第%d/%d次尝试失败: %v\n", attempt+1, maxRetries+1, err)

		if strings.Contains(err.Error(), "context deadline exceeded") || strings.Contains(err.Error(), "timeout") {
			m.syncBackendStatus("se_timeout", fmt.Sprintf("SE超时(%d/%d): %v", attempt+1, maxRetries+1, err.Error()[:min(60, len(err.Error()))]))
			continue
		}

		break
	}
	if m.isGhostCall(aiGen) {
		fmt.Printf("[startSETask] ⚠️ 检测到复位后的幽灵SE调用，丢弃结果\n")
		m.currentRole = ""
		m.cMonitor.UpdateSeStatus(types.RoleStatusIdle)
		return nil
	}
	if err != nil {
		m.currentRole = ""
		m.cMonitor.UpdateSeStatus(types.RoleStatusIdle)

		m.boardManager.UpdateTask(i18n.T("msg.task_failed"), 0)

		if m.ctx != nil {
			runtime.EventsEmit(m.ctx, "error", map[string]interface{}{
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
	if len(resp.Content) > 0 {
		fmt.Printf("[SE Debug] Response preview: %s\n", resp.Content[:min(500, len(resp.Content))])
	}
	if len(resp.Actions) > 0 {
		for i, a := range resp.Actions {
			fmt.Printf("[SE Debug] Action[%d]: type=%s path=%s\n", i, a.Type, a.Path)
		}
	}

	// 执行actions（先执行动作，确保有效操作不被NeedHelp跳过）
	if len(resp.Actions) > 0 {
		if err := m.executeSEActions(resp.Actions); err != nil {
			// 执行失败，通知SE，让SE决定如何处理
			failMsg := fmt.Sprintf("执行失败: %v", err)
			m.seProcessor.AddResult(failMsg)

			// SE处理失败情况，可能需要问PM
			resp2, err2 := m.seProcessor.ProcessTaskStream("上述执行失败，请分析原因并决定下一步", func(delta string) {
				m.cMonitor.UpdateSeChunkTime()
				m.emitStreamChunk("se", delta)
			})
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

		// 如果执行成功但没有completed
		if resp.Completed == nil {
			if resp.NeedHelp {
				fmt.Println("[System] SE actions done but still needs help, asking PM... [TAG-S1]")
				return m.handleSEAskPM(resp.Content)
			}
			if m.seProcessor.CheckSemanticComplete(resp.Content) {
				fmt.Println("[🛡️ 语义兜底] SE回复包含完成关键词但无completed JSON，强制路由到PM [TAG-D3]")
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
				runtime.EventsEmit(m.ctx, "exec_completed", map[string]interface{}{
					"executor":  "se",
					"result":    "",
					"timestamp": time.Now().Unix(),
					"content":   resp.Content,
				})
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

		runtime.EventsEmit(m.ctx, "exec_completed", map[string]interface{}{
			"executor":  "se",
			"result":    "",
			"changelog": "",
			"status":    "completed",
		})

		fmt.Println("[System] → SE回复走 ProcessMessageFrom (@层Router) → PM")
		fmt.Printf("[TRACE-AP-SE2PM] 即将调用ProcessMessageFrom(se, %q) time=%s\n",
			summary[:min(80, len(summary))], time.Now().Format("15:04:05"))
		m.writeRouteLog(fmt.Sprintf("[TRACE-AP-SE2PM] 调用ProcessMessageFrom(se) summary=%q", summary[:min(80, len(summary))]))
		// ⚠️ 也写到一个固定路径日志，排除文件句柄问题
		os.WriteFile("C:\\tmp\\ap_trace.log", []byte(fmt.Sprintf("[%s] TRACE-AP-SE2PM 调用ProcessMessageFrom(se) summary=%q\n",
			time.Now().Format("15:04:05"), summary[:min(80, len(summary))])), 0644)
		_, err := m.ProcessMessageFrom("se", summary)
		fmt.Printf("[TRACE-AP-SE2PM] ProcessMessageFrom返回 time=%s err=%v\n",
			time.Now().Format("15:04:05"), err)
		m.writeRouteLog(fmt.Sprintf("[TRACE-AP-SE2PM] ProcessMessageFrom返回 err=%v", err))
		os.WriteFile("C:\\tmp\\ap_trace.log", []byte(fmt.Sprintf("[%s] TRACE-AP-SE2PM ProcessMessageFrom返回 err=%v\n",
			time.Now().Format("15:04:05"), err)), 0644)
		return err
	}

	// 检查SE是否需要帮助（对于没有actions的情况）
	if resp.NeedHelp {
		fmt.Printf("[TRACE-SE] ⚠️ SE走【NeedHelp分支】→ handleSEAskPM | actions=%d (时间:%s)\n", len(resp.Actions), time.Now().Format("15:04:05"))
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
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("[continueSETask] 💥 panic recovered: %v\n", r)
			err = fmt.Errorf("panic: %v", r)
			m.cMonitor.UpdateSeStatus(types.RoleStatusIdle)
		}
	}()
	m.seContinueCount++
	if m.seContinueCount > 19 {
		fmt.Printf("[System] SE继续次数超限(%d)，任务失败\n", m.seContinueCount)

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

	const seContinueTimeout = 45 * time.Second
	seCtx, seCancel := context.WithTimeout(safeCtx, seContinueTimeout)
	defer seCancel()
	m.seProcessor.SetContext(seCtx)

	resp, err := m.seProcessor.ProcessTaskStream("继续", func(delta string) {
		m.cMonitor.UpdateSeChunkTime()
		m.emitStreamChunk("se", delta)
	})
	if err != nil {
		m.cMonitor.UpdateSeStatus(types.RoleStatusIdle)
		errMsg := fmt.Sprintf("❌ SE继续执行失败: %v", err)
		m.addSEToPMMsg(errMsg)
		if m.onProjectStateChanged != nil {
			m.onProjectStateChanged("error")
		}
		return fmt.Errorf("SE continue failed: %w", err)
	}

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
			resp2, err2 := m.seProcessor.ProcessTaskStream("上述执行失败，请分析原因并决定下一步", func(delta string) {
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
		fmt.Println("[System] SE actions completed, routing to PM via @layer (Router)... [TAG-C1]")
		m.currentRole = ""
		m.cMonitor.UpdateSeStatus(types.RoleStatusIdle)
		_, err := m.ProcessMessageFrom("se", "SE已完成任务执行，请审核结果") // [TAG-C1]
		return err
	}

	// 如果完成了 - 通过@层路由发送
	if resp.Completed != nil {
		m.currentRole = ""

		m.cMonitor.UpdatePmStatus(types.RoleStatusIdle)
		m.cMonitor.UpdateSeStatus(types.RoleStatusIdle)
		fmt.Println("[System] SE task completed(continue), routing to PM via @layer (Router)... [TAG-C2]")

		summary := "✅ 任务完成\n\n"
		summary += fmt.Sprintf("📝 技术笔记:\n%s\n\n", resp.Completed.TechnicalNotes)
		summary += fmt.Sprintf("📋 变更日志:\n%s", resp.Completed.ChangelogDraft)

		fmt.Println("[System] SE完成(continueSETask) → 走 ProcessMessageFrom (@层Router) → PM")
		_, err := m.ProcessMessageFrom("se", summary)
		return err
	}

	if resp.Content != "" {
		fmt.Printf("[System] 🔑 continueSETask: SE无actions/Completed但有内容 → 走@层→PM | content_len=%d [TAG-C3]\n", len(resp.Content))
		m.currentRole = ""
		m.cMonitor.UpdateSeStatus(types.RoleStatusIdle)
		_, err := m.ProcessMessageFrom("se", strings.TrimSpace(resp.Content))
		return err
	}

	fmt.Printf("[System] ⚠️ continueSETask: SE回复为空且无actions，结束 [TAG-C4]\n")
	m.currentRole = ""
	m.cMonitor.UpdateSeStatus(types.RoleStatusIdle)
	return nil
}

// emitWailsEvent 安全触发Wails前端事件（ctx为nil时跳过），同时推送到SSE
func (m *Manager) emitWailsEvent(eventName string, data interface{}) {
	if m.ctx != nil {
		runtime.EventsEmit(m.ctx, eventName, data)
	}
	m.pushSSEEvent(eventName, data)
}

// executeSEActions 执行SE的actions
func (m *Manager) executeSEActions(actions []ai.SEAction) error {
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
				default:
					label = a.Type
				}
				taskDefs[i] = types.TaskItemDef{Text: label}
			}
			m.richBuilder.ReplaceTaskList(seTaskId, taskDefs)
			m.richBuilder.UpdateTask(seTaskId, 0, "running")
		}
	}

	for i, action := range actions {
		actionLabel := fmt.Sprintf("%s %s", action.Type, func() string {
			switch action.Type {
			case "write_file":
				return action.Path
			case "exec":
				return action.Command
			case "read_file":
				return action.Path
			default:
				return ""
			}
		}())

		var currentTask *types.GlobalTask
		desc := fmt.Sprintf("%s%s", map[string]string{
			"write_file": "创建文件：",
			"edit_file":   "修改文件：",
			"exec":        "执行命令：",
			"read_file":   "读取文件：",
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
			fmt.Printf("[Action] Write file: %s\n", action.Path)
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

			if seTaskId != "" && m.richBuilder != nil {
				m.richBuilder.PushShellStart("se", seTaskId, 1, "exec", action.Command, nil)
			}
			output, err := m.seExecutor.Exec(action.Command, 30*time.Second)
			if seTaskId != "" && m.richBuilder != nil {
				m.richBuilder.PushShellOutput(seTaskId, output)
			}
			if err != nil {
				errMsg := fmt.Sprintf("执行失败: %v", err)
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
			m.seProcessor.AddResult(fmt.Sprintf("执行结果:\n%s", output))

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

		default:
			return fmt.Errorf("unknown action type: %s", action.Type)
		}
	}
	if seTaskId != "" && m.richBuilder != nil {
		m.richBuilder.UpdateTask(seTaskId, 2, "done")
		m.richBuilder.CompleteTaskList(seTaskId, "done", nil)
	}

	if m.ctx != nil {
		fmt.Println("[executeSEActions] ✅ 发送 exec_completed 事件")
		runtime.EventsEmit(m.ctx, "exec_completed", map[string]interface{}{
			"executor":  "se",
			"result":    "completed",
			"status":    "completed",
			"timestamp": time.Now().Unix(),
		})
	} else {
		fmt.Println("[executeSEActions] ⚠️ m.ctx 为 nil，无法发送 exec_completed")
	}

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

// ensurePMToUSR 确保PM消息带@USR前缀
func (m *Manager) ensurePMToUSR(content string) string {
	if strings.HasPrefix(content, "@USR") {
		return content
	}
	return "@USR " + content
}

// addPMToUserMsg 发送PM→USR消息（自动加@USR）
// ✅ 静默模式：只保存到历史记录，不发送 new-message 事件（因为流式已显示）
func (m *Manager) addPMToUserMsg(content string) {
	content = strings.TrimSpace(content)
	if content == "" {
		fmt.Println("[PM→USR] ⚠️ 内容为空，跳过发送")
		return
	}

	if !strings.HasPrefix(content, "@USR") && !strings.HasPrefix(content, "@SE") && !strings.HasPrefix(content, "@PM") && !strings.HasPrefix(content, "@AP") {
		content = "@USR " + content
	}

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

	if m.ctx != nil {
		runtime.EventsEmit(m.ctx, "pm_message", map[string]string{"delta": content})
		runtime.EventsEmit(m.ctx, "pm_streaming_done", map[string]interface{}{"content": content})
	}

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

// ensurePMToSE 确保PM消息带@SE前缀
func (m *Manager) ensurePMToSE(content string) string {
	if strings.HasPrefix(content, "@SE") {
		return content
	}
	return "@SE " + content
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

	if m.ctx != nil {
		runtime.EventsEmit(m.ctx, "pm_message", map[string]string{"delta": content})
		runtime.EventsEmit(m.ctx, "se_task_assigned", map[string]interface{}{
			"task":  strings.TrimPrefix(content, "@SE "),
			"steps": 0,
		})
	}
}

// ensureSEToUSR 确保SE消息带@USR前缀
func (m *Manager) ensureSEToUSR(content string) string {
	if strings.HasPrefix(content, "@USR") {
		return content
	}
	return "@USR " + content
}

// addSEToUserMsg 发送SE→USR消息（自动加@USR，直接回复）
func (m *Manager) addSEToUserMsg(content string) {
	content = m.ensureSEToUSR(content)
	seMsg := Message{
		From:      "se",
		To:        "user",
		Role:      "se",
		Content:   content,
		Raw:       content,
		Source:    "se_to_user",
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
}

// writeRouteLog 路由调试日志（永久保留，排查消息路由问题用）
// 日志位置: {workDir}/.argus/route.log
// 用途: 追踪 @SE/@PM 消息解析结果、SE任务来源(from)、轮转拦截情况
// 查看: Get-Content {workDir}/.argus/route.log -Tail 30
func (m *Manager) writeRouteLog(msg string) {
	logPath := filepath.Join(m.workDir, ".argus", "route.log")
	os.MkdirAll(filepath.Dir(logPath), 0755)
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	f.WriteString(fmt.Sprintf("[%s] %s\n", timestamp, msg))
}

// writeConversationLog 写入对话日志文件
func (m *Manager) writeConversationLog(msg Message) {
	logPath := filepath.Join(m.workDir, ".argus", "conversation.log")

	// 确保目录存在
	os.MkdirAll(filepath.Dir(logPath), 0755)

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
			runtime.EventsEmit(m.ctx, "pm_review_completed", map[string]interface{}{
				"taskId": pmTaskId,
				"status": status,
				"result":  pmReviewResult,
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
			runtime.EventsEmit(m.ctx, "error", map[string]interface{}{
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

	// 检查PM回复中是否有@SE/@AP标记
	// 使用单人路由：优先路由到目标角色
	parsedMsg := m.router.Parse("pm", resp.Content)

	// 🆕 @AP 路由：PM 审核通过后直接 @AP
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

	// 限制审核轮次，防止死循环
	// 注意：项目状态仍由PM通过Function Call更新，这里只是强制结束审核流程
	if m.reviewCount >= 3 {
		fmt.Printf("[System] 审核轮次已达%d，强制结束审核流程\n", m.reviewCount)
		m.addPMToUserMsg(i18n.T("msg.review_timeout"))
		m.currentRole = ""
		m.cMonitor.UpdatePmStatus(types.RoleStatusIdle)

		// ✅ [FIX#3] 审核超限时设置明确的错误状态
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
			strings.Contains(lowerContent, "bug")

		if isRework {
			fmt.Printf("[System] PM审核要求返工，生成新任务给SE\n")
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

	// PM决定是否完成项目
	if resp.HasTasks {
		// PM认为还有任务，继续执行
		fmt.Println("[System] PM has more tasks, continuing...")
		return m.startSETask(resp.Tasks.CurrentTask)
	}

	// ✅ PM审核通过 → 触发 AP 审批（第二道关卡）
	fmt.Println("[System] PM审核通过，触发AP审批...")
	m.currentRole = ""
	m.cMonitor.UpdatePmStatus(types.RoleStatusIdle)

	// ✅ 设HandoverPending（第三层C监控才能兜底）
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
		runtime.EventsEmit(m.ctx, "pm_started", map[string]string{})
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
			runtime.EventsEmit(m.ctx, "error", map[string]interface{}{
				"error": err.Error(), "stage": "pm_review",
			})
		}
		errMsg := fmt.Sprintf("%s\n\n%s", i18n.T("err.pm_review_failed", err), i18n.T("err.pm_api_network"))
		m.addPMToUserMsg(errMsg)
		return fmt.Errorf("PM review failed: %w", err)
	}

	pmReviewResult = resp.Content
	cleanContent := strings.TrimSpace(resp.Content)
	fmt.Printf("[DEBUG-RICH-FLOW] ProcessReview返回: content=%q len=%d\n", cleanContent[:min(80, len(cleanContent))], len(cleanContent))
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

	parsedMsg := m.router.Parse("pm", resp.Content)
	if parsedMsg.To == "ap" {
		fmt.Println("[RichPM] PM @AP → 路由到AP审批")
		m.currentRole = ""
		m.cMonitor.UpdatePmStatus(types.RoleStatusIdle)
		m.SetHandoverPending(HandoverPMToAP)
		// 标记PM审核任务为完成
		if m.taskManager != nil {
			m.taskManager.CompleteLastTaskByRole("PM")
		}
		if m.apProcessor != nil {
			return m.handleAPReview(parsedMsg.Content)
		}
	}

	if parsedMsg.To == "se" {
		fmt.Println("[RichPM] PM @SE → 返工")
		m.currentRole = ""
		m.cMonitor.UpdatePmStatus(types.RoleStatusIdle)
		return m.startSETaskWithFrom(parsedMsg.Content, "pm")
	}

	m.currentRole = ""
	m.cMonitor.UpdatePmStatus(types.RoleStatusIdle)

	if resp.HasTasks {
		return m.startSETask(resp.Tasks.CurrentTask)
	}

	m.SetHandoverPending(HandoverPMToAP)
	// 标记PM审核任务为完成（fallback路径）
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
		fmt.Printf("[handleAPReview] AP审核轮次超限(%d)，强制结束\n", m.apReviewCount)
		m.addAPToUserMsg(i18n.T("msg.review_timeout"))
		m.forceProjectApproved()
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
					m.addAPToUserMsg(fmt.Sprintf("❌ AP审批失败: %v\n\n项目将强制完成。", retryErr))
					m.forceProjectApproved()
					return fmt.Errorf("AP review failed (no-tools fallback also failed): %w", retryErr)
				}
				resp = retryResp
			} else {
				errMsg := fmt.Sprintf("❌ AP审批失败: %v\n\n项目将强制完成。", err)
				m.addAPToUserMsg(errMsg)
				m.forceProjectApproved()
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
	fmt.Println("[TRACE-AP] ✅✅ AP审批通过 → forceProjectApproved (done_approved)!")
	m.SetHandoverPending(HandoverAPToDone)
	m.forceProjectApproved()

	return nil
}

// forceProjectApproved AP批准后项目正式完成（状态=done_approved）
func (m *Manager) forceProjectApproved() {
	fmt.Printf("[TRACE-AP] 🏁 forceProjectApproved! 项目正式完成 (时间:%s)\n", time.Now().Format("15:04:05"))
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
	if m.ctx != nil {
		runtime.EventsEmit(m.ctx, "done", map[string]string{"status": "approved"})
	}
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
}

// forceProjectDone 强制结束项目（清除状态、通知前端）- 旧版兼容，状态=done
func (m *Manager) forceProjectDone() {
	m.ClearHandover()
	m.currentRole = ""
	m.reviewCount = 0
	m.apReviewCount = 0
	m.apMode = RoleModeIdle

	if m.ctx != nil {
		runtime.EventsEmit(m.ctx, "done", map[string]string{"status": "completed"})
	}
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

	if m.ctx != nil {
		runtime.EventsEmit(m.ctx, "reset", map[string]string{
			"reason":  reason,
			"trigger": string(trigger),
		})
	}

	fmt.Printf("[RESET] ✅ 复位完成\n")
	return nil
}

// ClearGlobalTasks 清空全局任务列表（供 App.ClearMessages 调用）
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
	if m.ctx != nil {
		runtime.EventsEmit(m.ctx, "todo_update", todoCopy)
	}
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
	if m.ctx != nil {
		runtime.EventsEmit(m.ctx, "todo_update", []TodoItem{})
	}
}

// emitTodoUpdate SSE推送TODO列表变更
func (m *Manager) emitTodoUpdate() {
	m.todoMu.RLock()
	defer m.todoMu.RUnlock()
	todoCopy := make([]TodoItem, len(m.todoList))
	copy(todoCopy, m.todoList)
	if m.ctx != nil {
		runtime.EventsEmit(m.ctx, "todo_update", todoCopy)
	}
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

	if m.ctx != nil {
		runtime.EventsEmit(m.ctx, "ap_message", map[string]string{"delta": content})
	}

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
		"initialized":      true,
		"messageCount":     len(m.history),
		"currentRole":      m.currentRole,
		"workDir":          m.workDir,
		"hasMemoryManager": m.memoryManager != nil,
	}
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

	if m.ctx != nil {
		runtime.EventsEmit(m.ctx, "done", map[string]string{"status": "cancelled"})
	}

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

func (m *Manager) GetExecutor() *executor.Executor {
	return m.seExecutor
}

func (m *Manager) GetSSEBridge() *SSEBridge {
	return m.sseBridge
}

func (m *Manager) GetBackendStatus() *BackendStatus {
	return m.backendStatus
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
