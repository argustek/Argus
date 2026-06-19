package main

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"gopkg.in/yaml.v3"

	"argus/internal/chat"
	"argus/internal/core"
	"argus/internal/debugger"
	"argus/internal/dingtalk"
	"argus/internal/git"
	"argus/internal/i18n"
	"argus/internal/mcp"
	"argus/internal/memory"
	"argus/internal/types"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// Windows API 常量
const (
	SPI_GETWORKAREA = 0x0030
)

// RECT 结构体
type RECT struct {
	Left   int32
	Top    int32
	Right  int32
	Bottom int32
}

// MonitorStatus 监控状态
type MonitorStatus struct {
	IsRunning     bool      `json:"isRunning"`
	PMStatus      string    `json:"pmStatus"`
	SEStatus      string    `json:"seStatus"`
	CStatus       string    `json:"cStatus"`
	LastCheckTime time.Time `json:"lastCheckTime"`
	AlertMessage  string    `json:"alertMessage"`
	ProjectState  string    `json:"projectState"` // 项目状态：idle/running/done/error
}

// APIConfig API 配置结构
type APIConfig struct {
	ID                 string `json:"id"`
	Name               string `json:"name"`
	Provider           string `json:"provider"`
	BaseURL            string `json:"baseUrl"`
	APIKey             string `json:"apiKey"`
	ModelName          string `json:"modelName"`
	IsDefault          bool   `json:"isDefault"`
	SupportsMultimodal bool   `json:"supportsMultimodal"`
	TestPassed         bool   `json:"testPassed"`
}

// DingTalkConfig 钉钉企业内部机器人配置
type DingTalkConfig struct {
	Enabled      bool   `json:"enabled"`
	Name         string `json:"name"`
	ClientID     string `json:"clientId"`
	ClientSecret string `json:"clientSecret"`
	RobotCode    string `json:"robotCode"`
	APIUrl       string `json:"apiUrl"`
	Mode         string `json:"mode"`
	Encrypt      bool   `json:"encrypt"`
	DefaultReply string `json:"defaultReply"`
	PollInterval int    `json:"pollInterval"`
}

type HTTPConfig struct {
	Enabled     bool   `json:"enabled"`
	Port        int    `json:"port"`
	APIToken    string `json:"apiToken"`
	AllowRemote bool   `json:"allowRemote"`
}

// IMConfig IM 集成配置（支持钉钉、企业微信、飞书）
type IMConfig struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Provider string `json:"provider"` // dingtalk, wecom, feishu
	Enabled  bool   `json:"enabled"`
	// 钉钉字段
	ClientID     string `json:"clientId,omitempty"`
	ClientSecret string `json:"clientSecret,omitempty"`
	RobotCode    string `json:"robotCode,omitempty"`
	Mode         string `json:"mode,omitempty"` // stream, webhook
	APIUrl       string `json:"apiUrl,omitempty"`
	// 企业微信字段（预留）
	CorpID     string `json:"corpId,omitempty"`
	CorpSecret string `json:"corpSecret,omitempty"`
	AgentID    string `json:"agentId,omitempty"`
	// 飞书字段（预留）
	AppID     string `json:"appId,omitempty"`
	AppSecret string `json:"appSecret,omitempty"`
}

// Config 配置结构
type Config struct {
	APIConfigs        []APIConfig             `json:"apiConfigs"`
	IMConfigs         []IMConfig              `json:"imConfigs"`
	ShowCodeBlocks    bool                    `json:"showCodeBlocks"`
	ShowThinking      bool                    `json:"showThinking"`
	PmDecisionAlert   bool                    `json:"pmDecisionAlert"`
	WorkDir           string                  `json:"workDir"`
	RecentProjects    []string                `json:"recentProjects"`
	DingTalk          DingTalkConfig          `json:"dingtalk,omitempty"`
	HTTP              HTTPConfig              `json:"http"`
	APEnabled         bool                    `json:"apEnabled"`
	APConfig          *APIConfig              `json:"apConfig,omitempty"`   // [deprecated] 旧AP独立配置对象，v1.0.22后改用APConfigID
	PMConfigID        string                  `json:"pmConfigId,omitempty"` // PM绑定的模型ID（空=用默认）
	SEConfigID        string                  `json:"seConfigId,omitempty"` // SE绑定的模型ID（空=用默认）
	APConfigID        string                  `json:"apConfigId,omitempty"` // AP绑定的模型ID（空=用默认）
	UseSeparateModels bool                    `json:"useSeparateModels"`    // 是否各角色使用不同模型
	MCPServers        []types.MCPServerConfig `json:"mcpServers,omitempty"` // [v0.7.1] MCP Server 配置列表
}

// ChatMessage 聊天消息
type ChatMessage struct {
	ID          int64       `json:"id"`
	Role        string      `json:"role"`
	Content     string      `json:"content"`
	Summary     string      `json:"summary,omitempty"`
	Description string      `json:"description,omitempty"`
	Changes     []Change    `json:"changes,omitempty"`
	CodeBlocks  []CodeBlock `json:"codeBlocks,omitempty"`
	Error       string      `json:"error,omitempty"`
	Timestamp   int64       `json:"timestamp,omitempty"`
}

// Change 文件改动
type Change struct {
	Type string `json:"type"`
	File string `json:"file"`
}

// CodeBlock 代码块
type CodeBlock struct {
	Language string `json:"language"`
	Code     string `json:"code"`
	File     string `json:"file,omitempty"`
}

func (a *App) newChatMessage(role, content string) ChatMessage {
	return ChatMessage{
		ID:        time.Now().UnixNano(),
		Role:      role,
		Content:   content,
		Timestamp: time.Now().Unix(),
	}
}

// ProjectConfig 项目配置结构（第15章 15.4）
type ProjectConfig struct {
	Language     string                   `yaml:"language" json:"language"`
	Build        string                   `yaml:"build" json:"build"`
	Run          string                   `yaml:"run" json:"run"`
	Test         string                   `yaml:"test" json:"test"`
	Requirements map[string]string        `yaml:"requirements" json:"requirements"`
	Services     map[string]ServiceConfig `yaml:"services" json:"services"`
}

// ServiceConfig 服务配置
type ServiceConfig struct {
	Image string `yaml:"image" json:"image"`
	Port  int    `yaml:"port" json:"port"`
}

// CheckResult 环境检测结果
type CheckResult struct {
	Installed      bool   `json:"installed"`
	Version        string `json:"version,omitempty"`
	Message        string `json:"message,omitempty"`
	CanAutoInstall bool   `json:"can_auto_install,omitempty"`
	InstallCmd     string `json:"install_cmd,omitempty"`
}

// App 应用结构
type App struct {
	ctx        context.Context
	config     Config
	status     MonitorStatus
	messages   []ChatMessage
	aiThinking bool

	// 日志
	logs []string

	// 终端管理器
	terminalManager *TerminalManager

	// 记忆系统
	memoryManager  *memory.MemoryManager
	contextBuilder *memory.ContextBuilder
	compressor     *memory.Compressor
	contextWindow  *memory.ContextWindow // [v0.7.2] 智能上下文窗口管理

	// C 守护进程相关
	cRunning  bool
	cStopChan chan bool

	// 初始化同步（确保 --send 等待 ChatManager 就绪）
	readyChan chan struct{}

	// CLI 模式标志（使用当前工作目录而非 exe 目录）
	useCWD bool

	// 改动历史
	changeHistory []ChangeRecord

	// Chat Manager（V2: Bridge → ArgusCore）
	chatManager *chat.Manager
	bridge      *chat.Bridge

	// HTTP 服务器（支持优雅停止）
	httpServer *http.Server

	// 文件变更检测（替代轮询，纯标准库）
	fileWatcherStop chan struct{}
	fileSnapshot    map[string]int64 // path → modTime.Unix

	// 消息去重（防止前端重复显示）
	msgIDCounter  int64
	emittedMsgIDs map[int64]bool

	// SendMessage 并发保护
	sendMu     sync.Mutex
	isSending  bool
	sendTaskID int64 // 每次新消息自增，用于拦截旧goroutine的陈旧回调

	// [v0.7.1] MCP Manager
	mcpManager *mcp.Manager

	// [v0.7.2] Debugger Session Manager
	debuggerMgr *debugger.DebugSessionManager
}

// ChangeRecord 改动记录
type ChangeRecord struct {
	Time    string   `json:"time"`
	Title   string   `json:"title"`
	Changes []Change `json:"changes"`
}

// NewApp 创建新应用
func NewApp() *App {
	return &App{
		config: Config{
			APIConfigs: []APIConfig{
				{
					ID:                 "1",
					Name:               "阿里通义千问",
					Provider:           "qwen",
					BaseURL:            "https://dashscope.aliyuncs.com/compatible-mode/v1",
					APIKey:             "",
					ModelName:          "qwen-turbo",
					IsDefault:          true,
					SupportsMultimodal: false,
					TestPassed:         false,
				},
			},
			ShowCodeBlocks: true,
			ShowThinking:   true,
			HTTP: HTTPConfig{
				Enabled:     true,
				Port:        8080,
				APIToken:    "",
				AllowRemote: true,
			},
		},
		status: MonitorStatus{
			IsRunning: false,
			PMStatus:  "stopped",
			SEStatus:  "stopped",
			CStatus:   "stopped",
		},
		messages:      make([]ChatMessage, 0),
		aiThinking:    false,
		logs:          []string{"Argus Vibe Coding Platform 已启动"},
		changeHistory: make([]ChangeRecord, 0),
		readyChan:     make(chan struct{}), // 初始化同步通道
	}
}

// Startup 应用启动时调用
func (a *App) Startup(ctx context.Context) {
	a.ctx = ctx
	a.addLog("【Startup】开始加载配置")
	a.loadConfig()
	a.addLog("【Startup】配置加载完成")

	// [v1.0.22] 旧配置自动迁移保存
	if a.config.PMConfigID == "" && a.config.SEConfigID == "" && a.config.APConfigID == "" {
		fmt.Println("[Startup] 🔄 执行旧配置格式自动迁移...")
		a.saveConfigToFile()
	}

	a.addLog("应用启动完成")

	a.initMemorySystem()

	// 确保窗口不会置顶，避免覆盖任务栏
	runtime.WindowSetAlwaysOnTop(ctx, false)

	// 获取屏幕工作区域（排除任务栏）
	user32 := syscall.NewLazyDLL("user32.dll")
	var rect RECT
	ret, _, _ := user32.NewProc("SystemParametersInfoW").Call(
		uintptr(SPI_GETWORKAREA),
		0,
		uintptr(unsafe.Pointer(&rect)),
		0,
	)

	if ret != 0 {
		// 计算工作区域宽高
		workWidth := int(rect.Right - rect.Left)
		workHeight := int(rect.Bottom - rect.Top)

		// 获取窗口大小
		winWidth, winHeight := runtime.WindowGetSize(a.ctx)

		// 计算居中位置
		x := int(rect.Left) + (workWidth-winWidth)/2
		y := int(rect.Top) + (workHeight-winHeight)/2

		runtime.WindowSetPosition(a.ctx, x, y)
		a.addLog(fmt.Sprintf("窗口居中到工作区域: %dx%d", workWidth, workHeight))
	} else {
		runtime.WindowCenter(a.ctx)
	}

	// C守护进程在initChatManager中启动（确保chatManager已初始化）

	// 初始化钉钉机器人
	a.initDingTalk()

	if a.config.HTTP.Enabled {
		go a.StartHTTPServer()
	}

	go func() {
		defer func() {
			if r := recover(); r != nil {
				fmt.Printf("[initChatManager] 💥 panic recovered: %v\n", r)
			}
		}()
		a.initChatManager()
	}()
}

// OnWindowDidShow 窗口显示时触发（包括最小化恢复）
func (a *App) OnWindowDidShow(ctx context.Context) {
	a.checkAndFixPosition()
}

// FixPosition 前端调用的位置修正（最小化恢复时触发）
func (a *App) FixPosition() {
	a.checkAndFixPosition()
}

// checkAndFixPosition 检查并修正窗口位置（最小化恢复时也生效）
func (a *App) checkAndFixPosition() {
	user32 := syscall.NewLazyDLL("user32.dll")
	var workArea RECT
	ret, _, _ := user32.NewProc("SystemParametersInfoW").Call(
		uintptr(SPI_GETWORKAREA),
		0,
		uintptr(unsafe.Pointer(&workArea)),
		0,
	)

	if ret != 0 && a.ctx != nil {
		currentX, currentY := runtime.WindowGetPosition(a.ctx)
		winW, winH := runtime.WindowGetSize(a.ctx)

		screenLeft := int(workArea.Left)
		screenTop := int(workArea.Top)
		screenRight := int(workArea.Right)
		screenBottom := int(workArea.Bottom)

		safeX := currentX
		safeY := currentY

		if currentX+winW < screenLeft || currentX > screenRight {
			safeX = screenLeft + (screenRight-screenLeft-winW)/2
		}
		if currentY+winH < screenTop || currentY > screenBottom {
			safeY = screenTop + (screenBottom-screenTop-winH)/2
		}

		if safeX != currentX || safeY != currentY {
			runtime.WindowSetPosition(a.ctx, safeX, safeY)
		}
	}
}

// HandleBeforeClose 处理窗口关闭事件（隐藏而非真正关闭）
func (a *App) HandleBeforeClose(ctx context.Context) {
	a.addLog("【窗口】收到关闭请求，隐藏窗口（进程仍在运行）")
	writeExitLog(fmt.Sprintf("[%s] [HIDE] 窗口隐藏，进程继续运行\n", time.Now().Format("2006-01-02 15:04:05")))
	runtime.WindowHide(ctx)
}

// HandleBeforeCloseSmart 智能关闭处理（根据状态决定是否退出）
func (a *App) HandleBeforeCloseSmart(ctx context.Context) bool {
	hasActiveTask := a.HasActiveTask()
	cMonitorRunning := a.IsCMonitorRunning()

	timestamp := time.Now().Format("2006-01-02 15:04:05")

	if hasActiveTask || cMonitorRunning {
		reason := ""
		if hasActiveTask && cMonitorRunning {
			reason = "有任务运行中 + C监控活跃"
		} else if hasActiveTask {
			reason = "有任务运行中"
		} else {
			reason = "C监控活跃"
		}

		a.addLog(fmt.Sprintf("【窗口】智能退出：%s → 最小化到托盘", reason))
		writeExitLog(fmt.Sprintf("[%s] [TRAY] %s → 隐藏到托盘\n", timestamp, reason))

		runtime.WindowHide(ctx)
		return true
	}

	a.addLog("【窗口】智能退出：无活动任务 → 允许退出")
	writeExitLog(fmt.Sprintf("[%s] [EXIT] 无活动任务，允许退出\n", timestamp))
	return false
}

// HasActiveTask 检查是否有活动任务
func (a *App) HasActiveTask() bool {
	if a.chatManager == nil {
		return false
	}
	return a.chatManager.HasActiveTask()
}

// IsCMonitorRunning 检查 C 监控是否运行中
func (a *App) IsCMonitorRunning() bool {
	if a.chatManager == nil {
		return false
	}
	return a.cRunning || a.chatManager.IsCMonitorActive()
}

// ShowWindow 安全显示窗口（带位置校验，解决多屏最小化后无法还原的问题）
func (a *App) ShowWindow() {
	if a.ctx == nil {
		return
	}
	a.safeShowWindow()
}

// safeShowWindow 带位置安全检查的显示窗口
func (a *App) safeShowWindow() {
	a.checkAndFixPosition()
	runtime.WindowShow(a.ctx)
	a.addLog("【窗口】已显示")
}

// ForceQuit 强制退出程序（绕过 OnBeforeClose）
func (a *App) ForceQuit() {
	a.addLog("【窗口】强制退出")
	writeExitLog(fmt.Sprintf("[%s] [FORCE_QUIT] 用户强制退出\n", time.Now().Format("2006-01-02 15:04:05")))
	runtime.Quit(a.ctx)
}

// Shutdown 应用关闭前的状态清理
func (a *App) Shutdown() {
	a.addLog("【Shutdown】清理会话状态...")
	if a.chatManager != nil {
		a.chatManager.Shutdown()
	}
	a.StopHTTPServer()
	a.stopDingTalk()
	a.stopCGuardian()
	a.addLog("【Shutdown】完成")
}

func writeExitLog(msg string) {
	dir, err := os.UserHomeDir()
	if err != nil {
		return
	}
	logPath := filepath.Join(dir, ".argus", "exit.log")
	os.MkdirAll(filepath.Dir(logPath), 0755)
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	f.WriteString(msg)
}

// initChatManager 初始化对话管理器
func (a *App) initChatManager() {
	if a.chatManager != nil {
		return
	}

	a.addLog("【ChatManager】初始化...")

	// 获取项目目录（避免监控Argus自己）
	projectDir := a.getProjectDir()
	if projectDir == "" {
		a.addLog("【ChatManager】❌ work_dir 未配置，跳过初始化")
		close(a.readyChan)
		return
	}
	a.addLog(fmt.Sprintf("【ChatManager】项目目录: %s", projectDir))

	// 构造配置
	config := types.Config{
		WorkDir:           projectDir,
		CommitInterval:    5,
		APIConfig:         types.APIConfig{},
		PmDecisionAlert:   a.config.PmDecisionAlert,
		UseSeparateModels: a.config.UseSeparateModels,
	}

	// 从 app config 转换 API 配置（共享模式优先用pmConfigId，否则用isDefault）
	var selectedConfig *APIConfig
	if !a.config.UseSeparateModels && a.config.PMConfigID != "" {
		// 共享模式：用 All roles share 选的模型
		for i := range a.config.APIConfigs {
			if a.config.APIConfigs[i].ID == a.config.PMConfigID {
				selectedConfig = &a.config.APIConfigs[i]
				break
			}
		}
	}
	if selectedConfig == nil {
		// 回退：找 isDefault
		for i := range a.config.APIConfigs {
			if a.config.APIConfigs[i].IsDefault {
				selectedConfig = &a.config.APIConfigs[i]
				break
			}
		}
	}
	if selectedConfig == nil && len(a.config.APIConfigs) > 0 {
		selectedConfig = &a.config.APIConfigs[0]
	}
	if selectedConfig != nil {
		config.APIConfig = types.APIConfig{
			Provider: selectedConfig.Provider,
			BaseURL:  selectedConfig.BaseURL,
			APIKey:   selectedConfig.APIKey,
			Model:    selectedConfig.ModelName,
		}
	}

	chatManager, err := chat.NewManager(config, projectDir, a.getConfigDir())
	if err != nil {
		a.addLog(fmt.Sprintf("【ChatManager】初始化失败: %v", err))
		return
	}

	a.chatManager = chatManager
	a.chatManager.SetDingTalkEnabled(a.isDingTalkEnabled())
	dingtalk.SetLogDir(filepath.Join(a.getConfigDir(), "..", "logs"))
	// 设置Wails context（供C监控弹框使用）
	if a.ctx != nil {
		a.chatManager.SetContext(a.ctx)
	}
	// 初始化各角色独立模型配置（与 SaveConfig 热更新逻辑保持一致）
	if a.config.UseSeparateModels {
		// 独立模式：按 ConfigID 分别设置
		if pmCfg := a.findAPIConfigByID(a.config.PMConfigID); pmCfg != nil {
			chatManager.UpdatePMConfig(types.APIConfig{
				Provider: pmCfg.Provider, BaseURL: pmCfg.BaseURL,
				APIKey: pmCfg.APIKey, Model: pmCfg.ModelName,
			})
			a.addLog(fmt.Sprintf("【ChatManager】PM独立模型: %s (%s)", pmCfg.ModelName, pmCfg.BaseURL))
		}
		if seCfg := a.findAPIConfigByID(a.config.SEConfigID); seCfg != nil {
			chatManager.UpdateSEConfig(types.APIConfig{
				Provider: seCfg.Provider, BaseURL: seCfg.BaseURL,
				APIKey: seCfg.APIKey, Model: seCfg.ModelName,
			})
		}
		if a.config.APEnabled {
			if apCfg := a.findAPIConfigByID(a.config.APConfigID); apCfg != nil {
				chatManager.UpdateAPConfig(types.APIConfig{
					Provider: apCfg.Provider, BaseURL: apCfg.BaseURL,
					APIKey: apCfg.APIKey, Model: apCfg.ModelName,
				})
				a.addLog(fmt.Sprintf("【ChatManager】AP独立模型: %s (%s)", apCfg.ModelName, apCfg.BaseURL))
			} else {
				chatManager.UpdateAPConfig(types.APIConfig{})
				a.addLog("【ChatManager】AP已启用，使用PM的API配置（共用模式）")
			}
		}
	} else if a.config.APEnabled {
		// 共享模式：AP跟随共享配置
		chatManager.UpdateAPConfig(types.APIConfig{})
		a.addLog("【ChatManager】AP已启用，使用PM的API配置（共用模式）")
	} else {
		a.addLog("【ChatManager】AP未启用")
	}
	// 设置终端输出回调，将SE命令输出显示到终端窗口
	a.chatManager.SetTerminalOutput(func(output string) {
		a.emitTerminalOutput(output)
	})
	// 设置PM/SE的终端写入器（用于QA验证时显示执行过程到终端）
	a.chatManager.SetTerminalWriter(a.WriteToTerminal)
	a.chatManager.SetOnFileWritten(func(path string) {
		a.emitToFrontend("se-file-written", path, "SE:FileWritten", chat.PathSEExec)
	})
	// 设置项目状态变更回调
	a.chatManager.SetOnProjectStateChanged(func(state string) {
		fmt.Printf(">>> [DEBUG-PROJ-STATE] onProjectStateChanged called! state=%s\n", state)
		a.addLog(fmt.Sprintf("[OnProjectStateChanged] 状态变更: %s", state))
		a.status.ProjectState = state
		a.emitToFrontend("project-state-changed", state, "Project:StateChange", chat.PathSystem)
	})
	// 设置消息回调，将消息推送到前端
	a.chatManager.SetOnMessageAdded(func(msg chat.Message) {
		a.msgIDCounter++
		msgID := a.msgIDCounter

		chatMsg := a.newChatMessage(msg.Role, msg.Content)
		chatMsg.ID = msgID
		a.messages = append(a.messages, chatMsg)
		a.saveMessages()

		// ✅ 后端去重：确保每条消息只发送一次事件
		if a.emittedMsgIDs == nil {
			a.emittedMsgIDs = make(map[int64]bool)
		}
		if !a.emittedMsgIDs[msgID] {
			a.emittedMsgIDs[msgID] = true
			a.emitToFrontend("new-message", chatMsg, fmt.Sprintf("V1OnMsg:%s", msg.Source), chat.PathSEToUser)
			a.writeDebugLog(fmt.Sprintf("[OnMsgAdded] EMIT #%d role=%s source=%s", msgID, msg.Role, msg.Source))
		} else {
			a.writeDebugLog(fmt.Sprintf("[OnMsgAdded] SKIP_DUP #%d role=%s", msgID, msg.Role))
		}

		if msg.Role == "se" {
			a.sendToDingTalk(fmt.Sprintf("[SE] %s", msg.Content))
		}
	})

	// 初始化C监控（在SetOnMessageAdded之后，确保消息能正确推送到前端）
	a.chatManager.InitCMonitor()

	// [v0.7.1] 初始化 MCP Manager（启动配置中的所有 MCP Server）
	a.initMCPManager(projectDir)

	// 监听前端语言切换事件
	runtime.EventsOn(a.ctx, "set-reply-language", func(optionalData ...interface{}) {
		if len(optionalData) > 0 {
			if lang, ok := optionalData[0].(string); ok {
				a.chatManager.SetReplyLanguage(lang)
			}
		}
	})

	// Git 操作走事件总线（非阻塞）：前端 EventsEmit 请求 → 后端 goroutine 执行 → EventsEmit 推送结果
	// 解决：await GetGitStatus() 阻塞 JS 主线程 → 消息确认超时误报"消息丢失"
	runtime.EventsOn(a.ctx, "git:request-status", func(optionalData ...interface{}) {
		go func() {
			data := git.GetStatus(a.getProjectDir())
			jsonData, _ := json.Marshal(data)
			a.emitToFrontend("git:status", string(jsonData), "GitStatusHandler", chat.PathSystem)
		}()
	})
	runtime.EventsOn(a.ctx, "git:request-repo-info", func(optionalData ...interface{}) {
		go func() {
			info := git.GetRepoInfo(a.getProjectDir())
			jsonData, _ := json.Marshal(info)
			a.emitToFrontend("git:repo-info", string(jsonData), "GitRepoInfoHandler", chat.PathSystem)
		}()
	})

	// ✅ 通知初始化完成（允许 --send 发送消息）
	close(a.readyChan)
	fmt.Println("[initChatManager] ✅ 初始化完成，已关闭 readyChan")

	a.addLog("【ChatManager】初始化完成")

	if a.chatManager != nil && a.chatManager.GetExecutor() != nil {
		aiClient := a.chatManager.GetAIClient()
		if aiClient != nil {
			a.bridge = chat.NewBridge(aiClient, a.chatManager.GetExecutor(), projectDir)
			a.bridge.SetContext(a.ctx)
			// [v0.8.6] 设置Bridge繁忙检测，C Monitor调用handleToPM前检查
			a.chatManager.SetBridgeBusyChecker(a.bridge.IsProcessing)

			if a.chatManager.GetMessageBus() != nil {
				a.chatManager.GetMessageBus().SetContext(a.ctx)
				a.chatManager.GetMessageBus().SetDebugLogWriter(a.chatManager.WriteDebugLog) // [v0.7.2] 对话框与log一致
				a.bridge.SetMessageBus(a.chatManager.GetMessageBus())
				a.bridge.SetDebugLogWriter(a.chatManager.WriteDebugLog)
				a.bridge.SetPushSSEEvent(func(eventType string, data interface{}) {
					if a.chatManager != nil {
						a.chatManager.PushSSEEvent(eventType, data)
					}
				})
				// [v0.7.2] 注入 ContextWindow 到 Bridge（真正的消息处理入口）
				if a.contextWindow != nil {
					a.bridge.SetContextWindow(a.contextWindow)
				}
				// [v0.7.2] 注入 ContextBuilder 和 Compressor 到 Bridge
				if a.contextBuilder != nil {
					a.bridge.SetContextBuilder(a.contextBuilder)
				}
				if a.compressor != nil {
					a.bridge.SetCompressor(a.compressor)
				}
				// [FIX-v0.8.1] Bridge 项目状态回调 → CMonitor → 前端
				// 注意：V2 Bridge 处理的是 short/Featherweight 任务（PM直执，无SE/AP）
				// 所以 success 时用 approved(3) 而非 done(2)，避免 CMonitor 强制触发 AP 审批
				a.bridge.SetOnProjectStateChange(func(state string) {
					if cm := a.chatManager.GetCMonitor(); cm != nil {
						switch state {
						case "running":
							cm.UpdateProjectState(types.ProjectStateRunning)
							cm.UpdatePmStatus(types.RoleStatusBusy)
						case "done":
							// short 任务无 AP 流程，直接 approved（跳过 CMonitor.handleProjectDone）
							cm.UpdateProjectState(types.ProjectStateApproved)
							cm.UpdateSeStatus(types.RoleStatusIdle)
						case "error":
							cm.UpdateProjectState(types.ProjectStateError)
						}
					}
				})
				a.chatManager.GetMessageBus().SetOnStateChange(func(state core.RoleState) {
					a.emitToFrontend("role-state", state, "MessageBus:State", chat.PathStatus)
				})
				// [FIX-v1.0.22] 绑定 IDE 消息推送回调到 Bridge 的 PMProcessor（Bridge 独立创建的实例）
				if bridgePM := a.bridge.GetPMProcessor(); bridgePM != nil {
					a.chatManager.SetupIDEMessageEmitterFor(bridgePM)
				}
			}

			a.bridge.SetOnMessage(func(msg *chat.Message) {
				if msg == nil || msg.Content == "" {
					return
				}

				if msg.Role == "status" || msg.From == "status" {
					fmt.Printf("[Bridge-Status] %s\n", msg.Content)
					if strings.Contains(msg.Content, "status:busy") {
						a.aiThinking = true
					} else {
						a.aiThinking = false
					}
					a.emitToFrontend("role-status", msg.Content, "Bridge:Status", chat.PathStatus)
					return
				}

				// [v0.8.1] 项目级别指示器（short/normal/full）
				if msg.Role == "project_level" {
					a.emitToFrontend("project-level", msg.Content, "Bridge:ProjectLevel", chat.PathStatus)
					return
				}

				a.msgIDCounter++
				chatMsg := a.newChatMessage(msg.Role, msg.Content)
				chatMsg.ID = a.msgIDCounter
				a.messages = append(a.messages, chatMsg)
				a.saveMessages()

				switch msg.Role {
				case "pm":
					a.emitToFrontend("new-message", chatMsg, fmt.Sprintf("Bridge:%s", msg.Role), chat.PathPMToUser)
				case "se":
					a.emitToFrontend("new-message", chatMsg, fmt.Sprintf("Bridge:%s", msg.Role), chat.PathSEToUser)
				case "ap":
					a.emitToFrontend("new-message", chatMsg, fmt.Sprintf("Bridge:%s", msg.Role), chat.PathAPToUser)
				default:
					a.emitToFrontend("new-message", chatMsg, fmt.Sprintf("Bridge:%s", msg.Role), chat.PathSystem)
				}
			})

			a.addLog("【V2 Bridge】✅ ArgusCore 已初始化 (全链路走MessageBus+校验)")
		}
	}

	// ⚠️ G点36修复：启动时如果任务已完成，清空旧消息防止显示历史任务
	if a.chatManager != nil {
		state := a.chatManager.GetProjectState()
		a.addLog(fmt.Sprintf("[G36-FIX] 检查: state=%s, messages=%d", state, len(a.messages)))
		if state == "done" || state == "approved" || state == "idle" {
			if len(a.messages) > 0 {
				a.addLog(fmt.Sprintf("[G36-FIX] ✅ 状态=%s，清空 %d 条旧消息", state, len(a.messages)))
				a.messages = make([]ChatMessage, 0)
				a.saveMessages()
				if a.ctx != nil {
					a.emitToFrontend("messages-cleared", nil, "G36Fix", chat.PathSystem)
				}
			} else {
				a.addLog(fmt.Sprintf("[G36-FIX] ℹ️ 状态=%s，但无旧消息需要清空", state))
			}
		} else {
			a.addLog(fmt.Sprintf("[G36-FIX] ⚠️ 状态=%s，不清空（非终态）", state))
		}
	} else {
		a.addLog("[G36-FIX] ⚠️ chatManager 为 nil，跳过检查")
	}

	// 启动C守护进程（此时chatManager已初始化，startCGuardian会跳过旧cMonitorLoop）
	go a.startCGuardian()
}

// initChatManagerCLI initializes ChatManager without Wails GUI dependencies
func (a *App) initChatManagerCLI() {
	if a.chatManager != nil {
		return
	}

	projectDir := a.getProjectDir()
	if projectDir == "" {
		fmt.Println("❌ work_dir 未配置，请在 config/config.json 中设置 workDir 或通过 GUI 选择")
		return
	}
	fmt.Printf("[CLI] 项目目录: %s\n", projectDir)

	config := types.Config{
		WorkDir:           projectDir,
		CommitInterval:    5,
		APIConfig:         types.APIConfig{},
		UseSeparateModels: a.config.UseSeparateModels,
	}

	var selectedConfig *APIConfig
	if !a.config.UseSeparateModels && a.config.PMConfigID != "" {
		for i := range a.config.APIConfigs {
			if a.config.APIConfigs[i].ID == a.config.PMConfigID {
				selectedConfig = &a.config.APIConfigs[i]
				break
			}
		}
	}
	if selectedConfig == nil {
		for i := range a.config.APIConfigs {
			if a.config.APIConfigs[i].IsDefault {
				selectedConfig = &a.config.APIConfigs[i]
				break
			}
		}
	}
	if selectedConfig == nil && len(a.config.APIConfigs) > 0 {
		selectedConfig = &a.config.APIConfigs[0]
	}
	if selectedConfig != nil {
		config.APIConfig = types.APIConfig{
			Provider: selectedConfig.Provider,
			BaseURL:  selectedConfig.BaseURL,
			APIKey:   selectedConfig.APIKey,
			Model:    selectedConfig.ModelName,
		}
	}

	chatManager, err := chat.NewManager(config, projectDir, a.getConfigDir())
	if err != nil {
		fmt.Printf("[CLI] ChatManager初始化失败: %v\n", err)
		return
	}

	a.chatManager = chatManager
	a.chatManager.SetDingTalkEnabled(a.isDingTalkEnabled())
	if a.ctx != nil {
		a.chatManager.SetContext(a.ctx)
	}
	// 初始化各角色独立模型配置（与 SaveConfig 热更新逻辑保持一致）
	if a.config.UseSeparateModels {
		if pmCfg := a.findAPIConfigByID(a.config.PMConfigID); pmCfg != nil {
			chatManager.UpdatePMConfig(types.APIConfig{
				Provider: pmCfg.Provider, BaseURL: pmCfg.BaseURL,
				APIKey: pmCfg.APIKey, Model: pmCfg.ModelName,
			})
			fmt.Printf("[CLI] PM独立模型: %s (%s)\n", pmCfg.ModelName, pmCfg.BaseURL)
		}
		if seCfg := a.findAPIConfigByID(a.config.SEConfigID); seCfg != nil {
			chatManager.UpdateSEConfig(types.APIConfig{
				Provider: seCfg.Provider, BaseURL: seCfg.BaseURL,
				APIKey: seCfg.APIKey, Model: seCfg.ModelName,
			})
		}
		if a.config.APEnabled {
			if apCfg := a.findAPIConfigByID(a.config.APConfigID); apCfg != nil {
				chatManager.UpdateAPConfig(types.APIConfig{
					Provider: apCfg.Provider, BaseURL: apCfg.BaseURL,
					APIKey: apCfg.APIKey, Model: apCfg.ModelName,
				})
				fmt.Printf("[CLI] AP独立模型: %s (%s)\n", apCfg.ModelName, apCfg.BaseURL)
			} else {
				chatManager.UpdateAPConfig(types.APIConfig{})
				fmt.Println("[CLI] AP已启用，使用PM的API配置（共用模式）")
			}
		}
	} else if a.config.APEnabled {
		chatManager.UpdateAPConfig(types.APIConfig{})
		fmt.Println("[CLI] AP已启用，使用PM的API配置（共用模式）")
	} else {
		fmt.Println("[CLI] AP未启用")
	}
	chatManager.InitCMonitor()

	// ✅ 通知初始化完成
	close(a.readyChan)

	fmt.Println("[CLI] ChatManager初始化完成")
}

// ==================== V2 统一消息总线 (LabVIEW模式) ====================
// 所有前后端通讯必须经过此函数，自动走MessageBus+校验码+ACK追踪

func (a *App) emitToFrontend(eventType string, payload interface{}, sourceLoc string, msgPath chat.MessagePath) {
	if a.ctx == nil {
		fmt.Printf("[emitToFrontend] ⚠️ ctx=nil, 跳过 %s from %s\n", eventType, sourceLoc)
		return
	}

	payloadJSON, _ := json.Marshal(payload)
	payloadStr := string(payloadJSON)

	if a.chatManager == nil {
		fmt.Printf("[emitToFrontend] ⚠️ chatManager=nil, 跳过 %s from %s\n", eventType, sourceLoc)
		return
	}
	msgBus := a.chatManager.GetMessageBus()
	if msgBus != nil {
		checksum := fmt.Sprintf("%d:%s:%s", len(payloadStr),
			func() string {
				if len(payloadStr) > 0 {
					return string(payloadStr[0])
				}
				return ""
			}(),
			func() string {
				if len(payloadStr) > 1 {
					return string(payloadStr[len(payloadStr)-1])
				}
				return ""
			}())

		msgBus.Send(eventType, payloadStr, eventType, msgPath, "App:emitToFrontend", func() map[string]interface{} {
			merged := map[string]interface{}{
				"event":    eventType,
				"checksum": checksum,
				"source":   sourceLoc,
			}
			var parsed map[string]interface{}
			if json.Unmarshal([]byte(payloadStr), &parsed) == nil && len(parsed) > 0 {
				for k, v := range parsed {
					merged[k] = v
				}
			} else {
				merged["data"] = payload
			}
			return merged
		}())
	} else {
		var finalPayload interface{}
		json.Unmarshal([]byte(payloadStr), &finalPayload)
		if finalPayload == nil {
			finalPayload = payload
		}
		runtime.EventsEmit(a.ctx, eventType, finalPayload)
	}

	fmt.Printf("[emit→Frontend] ✅ %s | path=%s | src=%s | size=%d\n",
		eventType, msgPath, sourceLoc, len(payloadStr))
}

// ==================== 日志相关 ====================

func (a *App) addLog(message string) {
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	logEntry := fmt.Sprintf("[%s] %s", timestamp, message)
	a.logs = append(a.logs, logEntry)

	if len(a.logs) > 1000 {
		a.logs = a.logs[len(a.logs)-1000:]
	}

	// 同时输出到控制台和文件
	fmt.Println(logEntry)

	// 同时写入文件，方便调试
	logFile := filepath.Join(a.getConfigDir(), "..", "logs", "argus.log")
	os.MkdirAll(filepath.Dir(logFile), 0755)
	f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Printf("[addLog Error] 无法打开日志文件: %v\n", err)
		return
	}
	defer f.Close()
	f.WriteString(logEntry + "\n")
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func (a *App) writeDebugLog(message string) {
	debugFile := filepath.Join(a.getConfigDir(), "debug_events.log")
	f, err := os.OpenFile(debugFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	f.WriteString(time.Now().Format("15:04:05.000") + " " + message + "\n")
}

func (a *App) GetLogs() []string {
	return a.logs
}

func (a *App) GetChangeHistory() []ChangeRecord {
	return a.changeHistory
}

// ==================== 配置相关 ====================

func (a *App) GetConfig() Config {
	return a.config
}

// [G60] 前端调用：记录收到消息（用于前后端一致性校验）
func (a *App) RecordReceive(role, messageID, content, source string) {
	if a.chatManager != nil {
		a.chatManager.RecordReceive(role, messageID, content, source)
	}
}

// Ready marks the app as frontend-ready, enabling message ACK tracking
func (a *App) Ready() {
	if a.chatManager != nil && a.chatManager.GetMessageBus() != nil {
		a.chatManager.GetMessageBus().SetFrontendReady()
	}
	if dir := a.getProjectDir(); dir != "" {
		a.startFileWatcher(dir)
	}
}

// [G63] MessageBus: 前端ACK确认收到消息
func (a *App) AckMessage(msgId string) bool {
	if a.chatManager == nil || a.chatManager.GetMessageBus() == nil {
		return false
	}
	return a.chatManager.GetMessageBus().Ack(msgId)
}

// ackPendingMessages 自动确认所有待处理消息（用于 HTTP API / --send 等无前端的场景）
// 确保 conversation.log 写入，不依赖前端 ACK
func (a *App) ackPendingMessages() {
	if a.chatManager == nil || a.chatManager.GetMessageBus() == nil {
		return
	}
	msgBus := a.chatManager.GetMessageBus()
	pending := msgBus.CheckPending()
	if a.chatManager.WriteDebugLog != nil {
		a.chatManager.WriteDebugLog(fmt.Sprintf("[ackPendingMessages] pending count=%d", len(pending)))
	}
	for _, p := range pending {
		if msgId, ok := p["msgId"].(string); ok && msgId != "" {
			if a.chatManager.WriteDebugLog != nil {
				a.chatManager.WriteDebugLog(fmt.Sprintf("[ackPendingMessages] Acking msgId=%s path=%v", msgId, p["path"]))
			}
			msgBus.Ack(msgId)
		}
	}
	if a.chatManager.WriteDebugLog != nil {
		a.chatManager.WriteDebugLog(fmt.Sprintf("[ackPendingMessages] done"))
	}
}

// [G63] MessageBus: 获取待确认消息列表
func (a *App) GetMessageBusPending() []map[string]interface{} {
	if a.chatManager == nil || a.chatManager.GetMessageBus() == nil {
		return []map[string]interface{}{}
	}
	return a.chatManager.GetMessageBus().CheckPending()
}

// [G63] MessageBus: 获取丢失消息历史
func (a *App) GetMessageBusLost() []map[string]interface{} {
	if a.chatManager == nil || a.chatManager.GetMessageBus() == nil {
		return []map[string]interface{}{}
	}
	return a.chatManager.GetMessageBus().GetLostMessages()
}

// [G63] MessageBus: 获取统计信息
func (a *App) GetMessageBusStats() map[string]interface{} {
	if a.chatManager == nil || a.chatManager.GetMessageBus() == nil {
		return map[string]interface{}{"error": "MessageBus未初始化"}
	}
	return a.chatManager.GetMessageBus().GetStats()
}

// [G63] MessageBus: 用户发送消息（双向同步 - 前端→后端）
func (a *App) UserSendMessage(content string) string {
	if a.chatManager == nil || a.chatManager.GetMessageBus() == nil {
		return ""
	}
	msgId := a.chatManager.GetMessageBus().Send("user", content, "user_send", chat.PathUserInput, "UserSendMessage", map[string]interface{}{
		"role":    "user",
		"content": content,
	})
	return msgId
}

// [G60] 获取一致性报告（前端可调用显示）
func (a *App) GetConsistencyReport() string {
	if a.chatManager == nil {
		return "ChatManager未初始化"
	}
	a.chatManager.PrintConsistencyReport()
	return "已输出到终端"
}

func (a *App) SaveConfig(config Config) error {
	oldDingEnabled := a.isDingTalkEnabled()
	oldHttpEnabled := a.config.HTTP.Enabled

	fmt.Printf("[SaveConfig] HTTP配置保存: enabled=%v, port=%d, apiToken=%s, allowRemote=%v\n",
		config.HTTP.Enabled, config.HTTP.Port,
		func() string {
			if config.HTTP.APIToken != "" {
				return "***"
			}
			return ""
		}(),
		config.HTTP.AllowRemote)

	a.config = config

	// [DEBUG] SaveConfig 热生效诊断 - 写conversation.log
	if a.chatManager != nil {
		a.chatManager.LogDebug(fmt.Sprintf("[SaveConfig] PMConfigID=%s, SEConfigID=%s, APConfigID=%s, useSeparate=%v",
			config.PMConfigID, config.SEConfigID, config.APConfigID, config.UseSeparateModels))
		for i, cfg := range config.APIConfigs {
			a.chatManager.LogDebug(fmt.Sprintf("[SaveConfig] apiConfigs[%d]: id=%s name=%s baseUrl=%s model=%s",
				i, cfg.ID, cfg.Name, cfg.BaseURL, cfg.ModelName))
		}
	}

	// [v1.0.22] 每角色独立模型配置
	if a.chatManager != nil {
		// 先同步UseSeparateModels到Manager（UpdateAPIConfig依赖此字段判断是否清空角色配置）
		a.chatManager.UpdateUseSeparateModels(config.UseSeparateModels)

		// 1. 默认/共享模型 → 更新主 aiClient（共享模式下的实际工作客户端）
		var sharedCfg *APIConfig
		if !config.UseSeparateModels && config.PMConfigID != "" {
			// 共享模式：用 All roles share 选的模型更新 aiClient
			sharedCfg = a.findAPIConfigByID(config.PMConfigID)
		}
		if sharedCfg == nil {
			sharedCfg = a.getDefaultAPIConfig()
		}
		if sharedCfg != nil {
			a.chatManager.LogDebug(fmt.Sprintf("[SaveConfig] aiClient-Update → id=%s name=%s model=%s (useSeparate=%v)",
				sharedCfg.ID, sharedCfg.Name, sharedCfg.ModelName, config.UseSeparateModels))
			a.chatManager.UpdateAPIConfig(types.APIConfig{
				Provider: sharedCfg.Provider,
				BaseURL:  sharedCfg.BaseURL,
				APIKey:   sharedCfg.APIKey,
				Model:    sharedCfg.ModelName,
			})
			// [HOTFIX] 同步更新 Bridge/ArgusCore 的 client（pmDirectExecute 路径走的是 c.client）
			if a.bridge != nil {
				a.bridge.UpdateClient(a.chatManager.GetAIClient(), a.config.WorkDir)
			}
		}

		// 2. PM独立模型
		pmCfg := a.findAPIConfigByID(config.PMConfigID)
		if pmCfg != nil {
			a.chatManager.LogDebug(fmt.Sprintf("[SaveConfig] PM-Update → id=%s name=%s baseUrl=%s model=%s",
				pmCfg.ID, pmCfg.Name, pmCfg.BaseURL, pmCfg.ModelName))
			a.chatManager.UpdatePMConfig(types.APIConfig{
				Provider: pmCfg.Provider,
				BaseURL:  pmCfg.BaseURL,
				APIKey:   pmCfg.APIKey,
				Model:    pmCfg.ModelName,
			})
		} else {
			a.chatManager.LogDebug(fmt.Sprintf("[SaveConfig] PM → config NOT FOUND for id=%s", config.PMConfigID))
		}

		// 3. SE独立模型
		seCfg := a.findAPIConfigByID(config.SEConfigID)
		if seCfg != nil {
			a.chatManager.LogDebug(fmt.Sprintf("[SaveConfig] SE-Update → id=%s name=%s baseUrl=%s model=%s",
				seCfg.ID, seCfg.Name, seCfg.BaseURL, seCfg.ModelName))
			a.chatManager.UpdateSEConfig(types.APIConfig{
				Provider: seCfg.Provider,
				BaseURL:  seCfg.BaseURL,
				APIKey:   seCfg.APIKey,
				Model:    seCfg.ModelName,
			})
		} else {
			a.chatManager.LogDebug(fmt.Sprintf("[SaveConfig] SE → config NOT FOUND for id=%s", config.SEConfigID))
		}

		// 4. AP模型
		if config.APEnabled {
			apCfg := a.findAPIConfigByID(config.APConfigID)
			if apCfg != nil {
				a.chatManager.UpdateAPConfig(types.APIConfig{
					Provider: apCfg.Provider,
					BaseURL:  apCfg.BaseURL,
					APIKey:   apCfg.APIKey,
					Model:    apCfg.ModelName,
				})
			} else {
				a.chatManager.UpdateAPConfig(types.APIConfig{})
			}
		} else {
			a.chatManager.UpdateAPConfig(types.APIConfig{})
		}
	}

	if err := a.saveConfigToFile(); err != nil {
		return err
	}

	// 钉钉开关变更时动态启停
	newDingEnabled := a.isDingTalkEnabled()
	fmt.Printf("[dingtalk] SaveConfig: oldDing=%v, newDing=%v\n", oldDingEnabled, newDingEnabled)
	if a.chatManager != nil {
		a.chatManager.SetDingTalkEnabled(newDingEnabled)
	}
	if newDingEnabled && !oldDingEnabled {
		a.addLog("【配置变更】钉钉已启用，正在启动...")
		a.initDingTalk()
	} else if !newDingEnabled && oldDingEnabled {
		a.addLog("【配置变更】钉钉已禁用，正在停止...")
		a.stopDingTalk()
	}

	// HTTP 服务开关变更时动态启停
	newHttpEnabled := config.HTTP.Enabled
	if newHttpEnabled && !oldHttpEnabled {
		a.addLog("【配置变更】HTTP 服务已启用，正在启动...")
		go a.StartHTTPServer()
	} else if !newHttpEnabled && oldHttpEnabled {
		a.addLog("【配置变更】HTTP 服务已禁用，正在停止...")
		a.StopHTTPServer()
	}

	return nil
}

func (a *App) isDingTalkEnabled() bool {
	for _, im := range a.config.IMConfigs {
		if im.Provider == "dingtalk" {
			return im.Enabled && im.ClientID != "" && im.ClientSecret != ""
		}
	}
	if a.config.DingTalk.Enabled && a.config.DingTalk.ClientID != "" && a.config.DingTalk.ClientSecret != "" {
		return true
	}
	return false
}

func (a *App) sendToDingTalk(msg string) {
	fmt.Printf("[dingtalk] app.sendToDingTalk called, isDingTalkEnabled=%v\n", a.isDingTalkEnabled())
	if !a.isDingTalkEnabled() {
		return
	}
	go func() {
		defer func() {
			if r := recover(); r != nil {
				fmt.Printf("[DingTalk-Send] 💥 panic recovered: %v\n", r)
			}
		}()
		if err := dingtalk.SendMessageToLastSender(msg); err != nil {
			a.addLog(fmt.Sprintf("【钉钉发送】失败: %v", err))
		}
	}()
}

func (a *App) stopDingTalk() {
	dingtalk.StopStream()
	a.addLog("钉钉 Stream 已停止")
}

func (a *App) StopHTTPServer() {
	if a.httpServer != nil {
		a.httpServer.Close()
		a.httpServer = nil
		a.addLog("HTTP 服务已停止")
	}
}

func (a *App) GetStatus() MonitorStatus {
	return a.status
}

// ========== 配置管理 API（决策 + 权限）==========

// GetDecisionConfig 获取决策配置（供前端显示）
func (a *App) GetDecisionConfig() *types.DecisionConfig {
	if a.chatManager == nil {
		return nil
	}
	return a.chatManager.GetConfigManager().GetDecisionConfig()
}

// UpdateDecisionRule 更新单个决策规则（前端调用）
func (a *App) UpdateDecisionRule(decisionType string, mode string) error {
	if a.chatManager == nil {
		return fmt.Errorf("ChatManager 未初始化")
	}

	err := a.chatManager.GetConfigManager().UpdateDecisionRule(
		types.DecisionType(decisionType),
		types.DecisionMode(mode),
	)
	if err != nil {
		return err
	}

	// 保存到文件
	return a.chatManager.GetConfigManager().SaveDecisionConfig()
}

// ResetDecisionToDefault 重置决策配置为缺省值
func (a *App) ResetDecisionToDefault() error {
	if a.chatManager == nil {
		return fmt.Errorf("ChatManager 未初始化")
	}

	a.chatManager.GetConfigManager().ResetDecisionToDefault()
	return a.chatManager.GetConfigManager().SaveDecisionConfig()
}

// GetPermissionConfig 获取权限配置（供前端显示）
func (a *App) GetPermissionConfig() *types.PermissionConfig {
	if a.chatManager == nil {
		return nil
	}
	return a.chatManager.GetConfigManager().GetPermissionConfig()
}

// AddPermissionRule 添加权限规则（前端调用）
func (a *App) AddPermissionRule(pathPattern, permission, description string, isDirectory bool, priority int) error {
	if a.chatManager == nil {
		return fmt.Errorf("ChatManager 未初始化")
	}

	rule := types.PathRule{
		PathPattern: pathPattern,
		Permission:  types.PermissionLevel(permission),
		Description: description,
		IsDirectory: isDirectory,
		Priority:    priority,
	}

	err := a.chatManager.GetConfigManager().AddPathRule(rule)
	if err != nil {
		return err
	}

	return a.chatManager.GetConfigManager().SavePermissionConfig()
}

// RemovePermissionRule 删除权限规则（前端调用）
func (a *App) RemovePermissionRule(pathPattern string) error {
	if a.chatManager == nil {
		return fmt.Errorf("ChatManager 未初始化")
	}

	err := a.chatManager.GetConfigManager().RemovePathRule(pathPattern)
	if err != nil {
		return err
	}

	return a.chatManager.GetConfigManager().SavePermissionConfig()
}

// ResetPermissionToDefault 重置权限配置为缺省值
func (a *App) ResetPermissionToDefault() error {
	if a.chatManager == nil {
		return fmt.Errorf("ChatManager 未初始化")
	}

	a.chatManager.GetConfigManager().ResetPermissionToDefault()
	return a.chatManager.GetConfigManager().SavePermissionConfig()
}

// CheckDecisionForOperation 检查操作是否需要人工确认（前端/SE 调用）
func (a *App) CheckDecisionForOperation(operationType string) (map[string]interface{}, error) {
	if a.chatManager == nil {
		return nil, fmt.Errorf("ChatManager 未初始化")
	}

	isAuto, desc, err := a.chatManager.CheckDecision(types.DecisionType(operationType))
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"operation": operationType,
		"isAuto":    isAuto,
		"mode": func() string {
			if isAuto {
				return "auto"
			} else {
				return "manual"
			}
		}(),
		"description": desc,
	}, nil
}

// CheckFilePermission 检查文件权限（前端/SE 调用）
func (a *App) CheckFilePermission(operation, filePath string) (map[string]interface{}, error) {
	if a.chatManager == nil {
		return nil, fmt.Errorf("ChatManager 未初始化")
	}

	level, rule, allowed := a.chatManager.CheckPermission(operation, filePath)

	return map[string]interface{}{
		"operation":   operation,
		"filePath":    filePath,
		"permission":  level,
		"matchedRule": rule,
		"isAllowed":   allowed,
	}, nil
}

func (a *App) GetEnvMemory() *types.EnvMemory {
	if a.chatManager == nil {
		return nil
	}
	return a.chatManager.GetEnvMemory()
}

func (a *App) LearnTool(name, path string) error {
	if a.chatManager == nil {
		return fmt.Errorf("ChatManager 未初始化")
	}
	return a.chatManager.LearnTool(name, path)
}

func (a *App) GetToolPath(name string) (string, bool) {
	if a.chatManager == nil {
		return "", false
	}
	return a.chatManager.GetToolPath(name)
}

func (a *App) EnvMemorySummary() string {
	if a.chatManager == nil {
		return ""
	}
	return a.chatManager.EnvMemorySummary()
}

func (a *App) GetCommandPolicy() *types.CommandPolicy {
	if a.chatManager == nil || a.chatManager.GetConfigManager() == nil {
		return nil
	}
	return a.chatManager.GetConfigManager().GetCommandPolicy()
}

func (a *App) AddCommandRule(pattern, level, description string) error {
	if a.chatManager == nil || a.chatManager.GetConfigManager() == nil {
		return fmt.Errorf("ConfigManager 未初始化")
	}
	err := a.chatManager.GetConfigManager().AddCommandRule(types.CommandRule{
		Pattern:     pattern,
		Level:       types.CommandBlockLevel(level),
		Description: description,
	})
	if err != nil {
		return err
	}
	return a.chatManager.GetConfigManager().SaveCommandPolicy()
}

func (a *App) RemoveCommandRule(pattern string) error {
	if a.chatManager == nil || a.chatManager.GetConfigManager() == nil {
		return fmt.Errorf("ConfigManager 未初始化")
	}
	err := a.chatManager.GetConfigManager().RemoveCommandRule(pattern)
	if err != nil {
		return err
	}
	return a.chatManager.GetConfigManager().SaveCommandPolicy()
}

func (a *App) ResetCommandPolicyToDefault() error {
	if a.chatManager == nil || a.chatManager.GetConfigManager() == nil {
		return fmt.Errorf("ConfigManager 未初始化")
	}
	err := a.chatManager.GetConfigManager().ResetCommandPolicyToDefault()
	if err != nil {
		return err
	}
	return a.chatManager.GetConfigManager().SaveCommandPolicy()
}

func (a *App) CheckCommandSafety(command string) map[string]interface{} {
	if a.chatManager == nil || a.chatManager.GetConfigManager() == nil {
		return map[string]interface{}{"level": "allow", "description": ""}
	}
	level, desc := a.chatManager.GetConfigManager().CheckCommand(command)
	return map[string]interface{}{
		"command":     command,
		"level":       string(level),
		"description": desc,
	}
}

func (a *App) getConfigDir() string {
	// 配置文件应该放在程序目录下，而不是工作目录
	// 工作目录是用户用来编程的目录，不应该存放程序配置

	// 使用项目根目录下的 config（而不是可执行文件目录）
	// 项目根目录通过查找包含 config 目录的位置确定
	exePath, err := os.Executable()
	if err != nil {
		return "."
	}

	// 尝试多个可能的项目根目录（exe所在目录优先，避免工作目录污染）
	possibleRoots := []string{
		// 可执行文件所在目录的父目录（dev 模式：build/bin/ -> build/ -> 项目根）
		filepath.Join(filepath.Dir(exePath), "..", ".."),
		// 可执行文件所在目录（生产模式）
		filepath.Dir(exePath),
		// 当前目录（开发时，仅作为 fallback）
		".",
	}

	for _, root := range possibleRoots {
		configDir := filepath.Join(root, "config")
		// 检查这个目录是否存在 dingtalk.json 或 config.json
		if _, err := os.Stat(filepath.Join(configDir, "dingtalk.json")); err == nil {
			return configDir
		}
		if _, err := os.Stat(filepath.Join(configDir, "config.json")); err == nil {
			return configDir
		}
	}

	// 默认使用当前目录
	argusDir := filepath.Join(".", "config")
	os.MkdirAll(argusDir, 0755)
	return argusDir
}

func (a *App) loadConfig() {
	configPath := filepath.Join(a.getConfigDir(), "config.json")
	a.addLog(fmt.Sprintf("[loadConfig] ConfigPath: %s", configPath))
	data, err := os.ReadFile(configPath)
	if err != nil {
		a.addLog(fmt.Sprintf("读取主配置失败: %v", err))
	} else {
		var loadedConfig Config
		if err := json.Unmarshal(data, &loadedConfig); err == nil {
			a.config = loadedConfig
			for i := range a.config.APIConfigs {
				a.config.APIConfigs[i].APIKey = decryptAPIKey(a.config.APIConfigs[i].APIKey)
			}
			// 解密AP独立配置的API Key
			if a.config.APConfig != nil {
				a.config.APConfig.APIKey = decryptAPIKey(a.config.APConfig.APIKey)
			}
		}
	}

	// [FIX-20260528-F] 改进HTTP默认值逻辑：只在端口未设置时才使用默认值
	if a.config.HTTP.Port == 0 {
		fmt.Printf("[loadConfig] HTTP端口未设置，使用默认值8080 (原配置: enabled=%v, port=%d)\n",
			a.config.HTTP.Enabled, a.config.HTTP.Port)
		// 保留用户的enabled设置，只设置默认端口
		if a.config.HTTP.APIToken == "" {
			a.config.HTTP.APIToken = ""
		}
		if a.config.HTTP.Port == 0 {
			a.config.HTTP.Port = 8080
		}
	} else {
		fmt.Printf("[loadConfig] HTTP配置已加载: enabled=%v, port=%d, allowRemote=%v\n",
			a.config.HTTP.Enabled, a.config.HTTP.Port, a.config.HTTP.AllowRemote)
	}

	if len(a.config.APIConfigs) == 0 {
		a.config.APIConfigs = []APIConfig{{
			ID:                 "1",
			Name:               "阿里通义千问",
			Provider:           "qwen",
			BaseURL:            "https://dashscope.aliyuncs.com/compatible-mode/v1",
			APIKey:             "",
			ModelName:          "qwen-turbo",
			IsDefault:          true,
			SupportsMultimodal: false,
			TestPassed:         false,
		}}
	}

	// [v1.0.22] 自动迁移：旧配置无角色模型ID → 用默认模型填充
	defaultCfg := a.getDefaultAPIConfig()
	if defaultCfg != nil {
		if a.config.PMConfigID == "" {
			a.config.PMConfigID = defaultCfg.ID
		}
		if a.config.SEConfigID == "" {
			a.config.SEConfigID = defaultCfg.ID
		}
	}
	// 迁移旧 apConfig 对象 → APConfigID
	if a.config.APConfigID == "" && a.config.APConfig != nil {
		for _, cfg := range a.config.APIConfigs {
			if cfg.BaseURL == a.config.APConfig.BaseURL && cfg.ModelName == a.config.APConfig.ModelName {
				a.config.APConfigID = cfg.ID
				break
			}
		}
		if a.config.APConfigID == "" && defaultCfg != nil {
			a.config.APConfigID = defaultCfg.ID
		}
	}
	needsMigration := a.config.PMConfigID == "" && a.config.SEConfigID == "" && a.config.APConfigID == ""
	if needsMigration {
		fmt.Println("[loadConfig] 🔄 检测到旧配置格式，将在Setup阶段自动迁移")
	}

	// 加载钉钉配置（无论主配置是否存在）
	a.loadDingTalkConfig()

	// 加载历史消息
	a.loadMessages()
}

// loadDingTalkConfig 加载钉钉配置（迁移旧配置到 IMConfigs）
func (a *App) loadDingTalkConfig() {
	a.addLog("【loadDingTalkConfig】开始加载")
	dingtalkPath := filepath.Join(a.getConfigDir(), "dingtalk.json")
	a.addLog(fmt.Sprintf("【loadDingTalkConfig】配置文件路径: %s", dingtalkPath))

	data, err := os.ReadFile(dingtalkPath)
	if err != nil {
		a.addLog(fmt.Sprintf("【loadDingTalkConfig】读取失败: %v", err))
		return
	}
	a.addLog(fmt.Sprintf("【loadDingTalkConfig】读取成功，数据长度: %d", len(data)))

	var dingtalkConfig DingTalkConfig
	if err := json.Unmarshal(data, &dingtalkConfig); err == nil {
		a.config.DingTalk = dingtalkConfig
		a.addLog(fmt.Sprintf("【loadDingTalkConfig】解析成功: enabled=%v, clientId=%s", dingtalkConfig.Enabled, dingtalkConfig.ClientID))

		// 迁移到 IMConfigs（如果 IMConfigs 为空且 DingTalk 有配置）
		if len(a.config.IMConfigs) == 0 && dingtalkConfig.ClientID != "" {
			imConfig := IMConfig{
				ID:           "1",
				Name:         dingtalkConfig.Name,
				Provider:     "dingtalk",
				Enabled:      dingtalkConfig.Enabled,
				ClientID:     dingtalkConfig.ClientID,
				ClientSecret: dingtalkConfig.ClientSecret,
				RobotCode:    dingtalkConfig.RobotCode,
				Mode:         dingtalkConfig.Mode,
				APIUrl:       dingtalkConfig.APIUrl,
			}
			a.config.IMConfigs = []IMConfig{imConfig}
			a.addLog("【loadDingTalkConfig】已迁移旧配置到 IMConfigs")
		}
	} else {
		a.addLog(fmt.Sprintf("【loadDingTalkConfig】解析失败: %v，使用默认值", err))
		if a.config.DingTalk.ClientID == "" {
			a.config.DingTalk = DingTalkConfig{
				Enabled: false,
				Mode:    "stream",
			}
		}
	}
	if a.config.DingTalk.ClientID == "" && a.config.DingTalk.Enabled {
		a.config.DingTalk.Enabled = false
	}
}

// GetDingTalkConfig 获取钉钉配置
func (a *App) GetDingTalkConfig() DingTalkConfig {
	return a.config.DingTalk
}

// SaveDingTalkConfig 保存钉钉配置
func (a *App) SaveDingTalkConfig(config DingTalkConfig) error {
	a.config.DingTalk = config
	dingtalkPath := filepath.Join(a.getConfigDir(), "dingtalk.json")
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(dingtalkPath, data, 0644)
}

func (a *App) loadMessages() {
	workDir := a.GetWorkDir()
	if workDir == "" {
		return
	}
	messagesPath := filepath.Join(workDir, ".argus", "messages.json")
	a.loadMessagesFromPath(messagesPath)
}

func (a *App) loadMessagesFromPath(messagesPath string) {
	data, err := os.ReadFile(messagesPath)
	if err != nil {
		return
	}

	var msgs []ChatMessage
	if err := json.Unmarshal(data, &msgs); err == nil {
		a.messages = msgs
		a.addLog(fmt.Sprintf("加载了 %d 条历史消息 (来源: %s)", len(msgs), messagesPath))
	}
}

func (a *App) saveMessages() {
	workDir := a.GetWorkDir()
	if workDir == "" {
		return
	}

	messagesPath := filepath.Join(workDir, ".argus", "messages.json")
	data, err := json.MarshalIndent(a.messages, "", "  ")
	if err != nil {
		return
	}
	os.MkdirAll(filepath.Dir(messagesPath), 0755)
	os.WriteFile(messagesPath, data, 0644)
}

func (a *App) ResetRoleStatus() {
	a.messages = make([]ChatMessage, 0)
	a.saveMessages()
	a.emitToFrontend("messages-cleared", nil, "ResetRoleStatus", chat.PathSystem)

	if a.chatManager != nil {
		a.chatManager.ResetRoleStatus()
	}

	a.addLog("✅ 已重置角色工作状态并清空消息")
}

func (a *App) ExecuteReset(reason string) error {
	if a.chatManager == nil {
		return fmt.Errorf("chatManager 未初始化")
	}
	if reason == "" {
		reason = "用户手动复位"
	}
	err := a.chatManager.ExecuteReset(reason, "user")
	if err != nil {
		return err
	}
	a.messages = make([]ChatMessage, 0)
	a.saveMessages()
	a.emitToFrontend("messages-cleared", nil, "ExecuteReset", chat.PathSystem)
	a.emitToFrontend("reset-completed", map[string]string{"reason": reason}, "ExecuteReset", chat.PathSystem)
	a.addLog("✅ 已执行复位: " + reason)
	return nil
}

func (a *App) saveConfigToFile() error {
	configPath := filepath.Join(a.getConfigDir(), "config.json")

	// 深拷贝（json序列化/反序列化）以避免加密污染内存中的原始key
	data, err := json.Marshal(a.config)
	if err != nil {
		return err
	}
	var configCopy Config
	if err := json.Unmarshal(data, &configCopy); err != nil {
		return err
	}
	encryptAPIKeys(&configCopy)

	data, err = json.MarshalIndent(configCopy, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configPath, data, 0644)
}

func getEncryptionKey() []byte {
	homeDir, _ := os.UserHomeDir()
	hash := sha256.Sum256([]byte("argus-encryption-key-v1:" + homeDir))
	return hash[:32]
}

func encryptAPIKeys(config *Config) {
	key := getEncryptionKey()
	block, _ := aes.NewCipher(key)
	gcm, _ := cipher.NewGCM(block)
	nonce := make([]byte, gcm.NonceSize())
	rand.Read(nonce)

	for i := range config.APIConfigs {
		if config.APIConfigs[i].APIKey != "" && !isEncrypted(config.APIConfigs[i].APIKey) {
			plaintext := []byte(config.APIConfigs[i].APIKey)
			ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)
			config.APIConfigs[i].APIKey = "enc:" + base64.StdEncoding.EncodeToString(ciphertext)
		}
	}

	// 也加密AP独立配置的API Key
	if config.APConfig != nil && config.APConfig.APIKey != "" && !isEncrypted(config.APConfig.APIKey) {
		plaintext := []byte(config.APConfig.APIKey)
		ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)
		config.APConfig.APIKey = "enc:" + base64.StdEncoding.EncodeToString(ciphertext)
	}
}

func decryptAPIKey(encrypted string) string {
	if !isEncrypted(encrypted) {
		return encrypted
	}
	key := getEncryptionKey()
	data, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(encrypted, "enc:"))
	if err != nil {
		return encrypted
	}
	block, _ := aes.NewCipher(key)
	gcm, _ := cipher.NewGCM(block)
	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return encrypted
	}
	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return encrypted
	}
	return string(plaintext)
}

func isEncrypted(s string) bool {
	return strings.HasPrefix(s, "enc:")
}

// ==================== 项目目录管理 ====================

func (a *App) GetWorkDir() string {
	// 如果配置了工作目录，返回配置的值
	// 否则返回空字符串，让前端显示"工作目录"
	return a.config.WorkDir
}

func (a *App) SetWorkDir(dir string) error {
	if dir == "" {
		return fmt.Errorf("目录不能为空")
	}

	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("目录不存在: %s", dir)
	}
	if !info.IsDir() {
		return fmt.Errorf("路径不是目录: %s", dir)
	}

	a.config.WorkDir = dir
	a.startFileWatcher(dir)

	// 添加到最近项目
	a.addRecentProject(dir)

	// 更新ChatManager的工作目录
	if a.chatManager != nil {
		a.chatManager.SetWorkDir(dir)
		a.addLog(fmt.Sprintf("【SetWorkDir】更新ChatManager工作目录: %s", dir))
	} else {
		a.addLog(fmt.Sprintf("【SetWorkDir】首次设置工作目录: %s，初始化 ChatManager...", dir))
		go func() {
			defer func() {
				if r := recover(); r != nil {
					fmt.Printf("[SetWorkDir] 💥 initChatManager panic: %v\n", r)
				}
			}()
			a.initChatManager()
		}()
	}

	return a.saveConfigToFile()
}

func (a *App) GetRecentProjects() []string {
	return a.config.RecentProjects
}

// ClearWorkDir 清除工作目录设置
func (a *App) ClearWorkDir() error {
	a.config.WorkDir = ""
	return a.saveConfigToFile()
}

func (a *App) OpenFolderDialog() (string, error) {
	options := runtime.OpenDialogOptions{
		Title:           "选择项目文件夹",
		ShowHiddenFiles: true,
	}

	result, err := runtime.OpenDirectoryDialog(a.ctx, options)
	if err != nil {
		return "", err
	}

	if result == "" {
		return "", nil
	}

	return result, nil
}

func (a *App) addRecentProject(dir string) {
	// 移除已存在的项目
	for i, p := range a.config.RecentProjects {
		if p == dir {
			a.config.RecentProjects = append(a.config.RecentProjects[:i], a.config.RecentProjects[i+1:]...)
			break
		}
	}

	// 添加到开头
	a.config.RecentProjects = append([]string{dir}, a.config.RecentProjects...)

	// 最多保留 10 个
	if len(a.config.RecentProjects) > 10 {
		a.config.RecentProjects = a.config.RecentProjects[:10]
	}
}

// ==================== C 守护进程实现 ====================

func (a *App) startCGuardian() {
	if a.cRunning {
		return
	}

	a.cRunning = true
	a.cStopChan = make(chan bool)
	a.status.CStatus = "running"
	a.addLog("C守护进程启动")
	a.addLog("C监控由ChatManager接管")
}

func (a *App) stopCGuardian() {
	if !a.cRunning {
		return
	}

	a.cRunning = false
	a.status.CStatus = "stopped"
	a.addLog("C守护进程停止")
}

// StartCMonitor 公开方法：启动C监控
func (a *App) StartCMonitor() error {
	if a.chatManager != nil && a.chatManager.IsCMonitorRunning() {
		return fmt.Errorf("C监控已在运行")
	}
	if a.chatManager != nil {
		a.chatManager.StartCMonitor()
	}
	a.startCGuardian()
	return nil
}

// StopCMonitor 公开方法：停止C监控
func (a *App) StopCMonitor() error {
	if a.chatManager != nil {
		a.chatManager.StopCMonitor()
	}
	a.stopCGuardian()
	return nil
}

// ==================== 文件备份机制 ====================

func (a *App) backupFile(filePath string) (string, error) {
	fullPath := filepath.Join(a.getProjectDir(), filePath)
	absPath, _ := filepath.Abs(fullPath)
	absDir, _ := filepath.Abs(a.getProjectDir())
	if !strings.HasPrefix(absPath, absDir+string(os.PathSeparator)) && absPath != absDir {
		return "", fmt.Errorf("路径越界: %s", filePath)
	}

	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		return "", nil
	}

	backupDir := filepath.Join(a.getProjectDir(), ".argus", "backups")
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		return "", fmt.Errorf("创建备份目录失败：%v", err)
	}

	timestamp := time.Now().Format("20060102_150405")
	ext := filepath.Ext(filePath)
	base := strings.TrimSuffix(filepath.Base(filePath), ext)
	dir := filepath.Dir(filePath)

	backupName := fmt.Sprintf("%s_%s%s", base, timestamp, ext)
	backupPath := filepath.Join(backupDir, dir, backupName)

	if err := os.MkdirAll(filepath.Dir(backupPath), 0755); err != nil {
		return "", fmt.Errorf("创建备份子目录失败：%v", err)
	}

	if err := copyFile(fullPath, backupPath); err != nil {
		return "", fmt.Errorf("备份文件失败：%v", err)
	}

	a.addLog("文件已备份: " + filePath + " -> " + backupPath)
	return backupPath, nil
}

func copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	return err
}

// ==================== 项目配置管理 ====================

func (a *App) loadProjectConfig() (*ProjectConfig, error) {
	projectDir := a.getProjectDir()
	configPath := filepath.Join(projectDir, ".argus", "config.yaml")

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return a.detectProjectConfig()
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}

	var config ProjectConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("解析 config.yaml 失败: %v", err)
	}

	return &config, nil
}

func (a *App) detectProjectConfig() (*ProjectConfig, error) {
	projectDir := a.getProjectDir()
	config := &ProjectConfig{}

	if _, err := os.Stat(filepath.Join(projectDir, "go.mod")); err == nil {
		config.Language = "go"
		config.Build = "go build -o app.exe ./..."
		config.Run = ".\\app.exe"
		config.Test = "go test ./..."
		config.Requirements = map[string]string{"go": ">=1.21"}
	} else if _, err := os.Stat(filepath.Join(projectDir, "package.json")); err == nil {
		config.Language = "nodejs"
		config.Build = "npm run build"
		config.Run = "npm start"
		config.Test = "npm test"
		config.Requirements = map[string]string{"node": ">=18.0"}
	} else if _, err := os.Stat(filepath.Join(projectDir, "requirements.txt")); err == nil {
		config.Language = "python"
		config.Build = ""
		config.Run = "python main.py"
		config.Test = "pytest"
		config.Requirements = map[string]string{"python": ">=3.9"}
	}

	return config, nil
}

func (a *App) saveProjectConfig(config *ProjectConfig) error {
	projectDir := a.getProjectDir()
	configDir := filepath.Join(projectDir, ".argus")

	if err := os.MkdirAll(configDir, 0755); err != nil {
		return err
	}

	var content strings.Builder
	content.WriteString("# Argus 项目配置文件\n")
	content.WriteString(fmt.Sprintf("language: %s\n", config.Language))
	content.WriteString(fmt.Sprintf("build: %s\n", config.Build))
	content.WriteString(fmt.Sprintf("run: %s\n", config.Run))
	content.WriteString(fmt.Sprintf("test: %s\n", config.Test))

	if len(config.Requirements) > 0 {
		content.WriteString("\nrequirements:\n")
		for tool, version := range config.Requirements {
			content.WriteString(fmt.Sprintf("  %s: \"%s\"\n", tool, version))
		}
	}

	return os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte(content.String()), 0644)
}

// ==================== 文件操作（增强版，带备份）====================

func (a *App) CreateFile(filePath string, content string) error {
	fullPath := filepath.Join(a.getProjectDir(), filePath)
	dir := filepath.Dir(fullPath)

	if _, err := os.Stat(fullPath); err == nil {
		a.backupFile(filePath)
	}

	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("创建目录失败：%v", err)
	}

	if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("写入文件失败：%v", err)
	}

	return nil
}

func (a *App) ReadFile(filePath string) (string, error) {
	var fullPath string

	if filepath.IsAbs(filePath) {
		fullPath = filePath
	} else {
		fullPath = filepath.Join(a.getProjectDir(), filePath)
	}

	data, err := os.ReadFile(fullPath)
	if err != nil {
		return "", fmt.Errorf("读取文件失败：%v", err)
	}
	return string(data), nil
}

func (a *App) OpenFileDialog() (string, error) {
	workDir := a.getProjectDir()
	dialog := runtime.OpenDialogOptions{
		Title:            "打开文件",
		DefaultDirectory: workDir,
		Filters:          []runtime.FileFilter{},
	}

	selectedFile, err := runtime.OpenFileDialog(a.ctx, dialog)
	if err != nil {
		return "", fmt.Errorf("打开文件对话框失败：%v", err)
	}

	if selectedFile == "" {
		return "", nil
	}

	return selectedFile, nil
}

func (a *App) SaveFile(defaultName string, content string) (string, error) {
	dialog := runtime.SaveDialogOptions{
		Title:           "保存文件",
		DefaultFilename: defaultName,
		Filters: []runtime.FileFilter{
			{DisplayName: "所有文件", Pattern: "*.*"},
		},
	}

	savedPath, err := runtime.SaveFileDialog(a.ctx, dialog)
	if err != nil {
		return "", fmt.Errorf("保存文件对话框失败：%v", err)
	}

	if savedPath == "" {
		return "", nil
	}

	err = os.WriteFile(savedPath, []byte(content), 0644)
	if err != nil {
		return "", fmt.Errorf("写入文件失败：%v", err)
	}

	return savedPath, nil
}

func (a *App) WriteFile(filePath string, content string) error {
	err := os.WriteFile(filePath, []byte(content), 0644)
	if err != nil {
		return fmt.Errorf("写入文件失败：%v", err)
	}
	return nil
}

func (a *App) UpdateFile(filePath string, content string) error {
	return a.CreateFile(filePath, content)
}

func (a *App) DeleteFile(filePath string) error {
	fullPath := filepath.Join(a.getProjectDir(), filePath)

	if _, err := os.Stat(fullPath); err == nil {
		a.backupFile(filePath)
	}

	if err := os.Remove(fullPath); err != nil {
		return fmt.Errorf("删除文件失败：%v", err)
	}
	return nil
}

func (a *App) ListFiles() ([]map[string]interface{}, error) {
	projectDir := a.getProjectDir()
	var files []map[string]interface{}

	err := filepath.Walk(projectDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, _ := filepath.Rel(projectDir, path)
		if relPath == "." {
			return nil
		}
		// Skip .argus/cache and .argus/logs, but show tree/docs/skills
		relPathSlash := strings.ReplaceAll(relPath, "\\", "/")
		if strings.HasPrefix(relPathSlash, ".argus/cache") || strings.HasPrefix(relPathSlash, ".argus/logs") {
			return nil
		}

		files = append(files, map[string]interface{}{
			"name":    info.Name(),
			"path":    relPath,
			"isDir":   info.IsDir(),
			"size":    info.Size(),
			"modTime": info.ModTime().Format("2006-01-02 15:04:05"),
		})

		return nil
	})

	return files, err
}

func (a *App) OpenFileLocation(filePath string) error {
	if filePath == "" {
		return fmt.Errorf("OpenFileLocation: file path is empty")
	}
	fullPath := filepath.Join(a.getProjectDir(), filePath)
	dir := filepath.Dir(fullPath)

	var cmd *exec.Cmd

	switch goruntime.GOOS {
	case "windows":
		cmd = exec.Command("explorer", "/select,", fullPath)
	case "darwin":
		cmd = exec.Command("open", "-R", fullPath)
	case "linux":
		cmd = exec.Command("xdg-open", dir)
	default:
		// 不支持的系统，静默忽略
		return nil
	}

	if err := cmd.Start(); err != nil {
		a.addLog(fmt.Sprintf("【OpenFileLocation】启动失败: %v", err))
		return nil
	}

	go cmd.Wait()
	return nil
}

// RunFile 在新 PowerShell 窗口中运行可执行文件
func (a *App) RunFile(filePath string) error {
	fullPath := filepath.Join(a.getProjectDir(), filePath)

	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		return fmt.Errorf("文件不存在: %s", fullPath)
	}

	var cmd *exec.Cmd

	switch goruntime.GOOS {
	case "windows":
		if strings.EqualFold(filepath.Ext(fullPath), ".ps1") {
			cmd = exec.Command("powershell", "-NoExit", "-ExecutionPolicy", "Bypass", "-File", fullPath)
		} else {
			cmd = exec.Command("powershell", "-NoExit", "-Command", "&", fullPath)
		}
	case "darwin":
		cmd = exec.Command("open", "-a", "Terminal", fullPath)
	case "linux":
		cmd = exec.Command("x-terminal-emulator", "-e", fullPath)
	default:
		return fmt.Errorf("不支持的操作系统: %s", goruntime.GOOS)
	}

	return cmd.Start()
}

func (a *App) OpenWorkDir() error {
	workDir := a.getProjectDir()

	a.addLog(fmt.Sprintf("【资源管理器】尝试打开工作目录: %s", workDir))

	if workDir == "" || workDir == "." {
		return fmt.Errorf("未设置工作目录，请先选择项目")
	}

	if _, err := os.Stat(workDir); os.IsNotExist(err) {
		return fmt.Errorf("工作目录不存在: %s", workDir)
	}

	var cmd *exec.Cmd

	switch goruntime.GOOS {
	case "windows":
		cmd = exec.Command("explorer", workDir)
	case "darwin":
		cmd = exec.Command("open", workDir)
	case "linux":
		cmd = exec.Command("xdg-open", workDir)
	default:
		return fmt.Errorf("不支持的操作系统: %s", goruntime.GOOS)
	}

	err := cmd.Start()
	if err != nil {
		a.addLog(fmt.Sprintf("【资源管理器】启动失败: %v", err))
		return nil
	}

	go func() {
		cmd.Wait()
		a.addLog(fmt.Sprintf("【资源管理器】已打开: %s", workDir))
	}()

	return nil
}

// ==================== 环境检测（第15章 15.7.1）====================

func (a *App) CheckEnv(tool string) bool {
	result := a.checkEnv(tool)
	return result.Installed
}

func (a *App) CheckEnvDetail(tool string) CheckResult {
	return a.checkEnv(tool)
}

func (a *App) InstallEnv(tool string) (map[string]interface{}, error) {
	wingetMap := map[string]string{
		"go":     "GoLang.Go",
		"node":   "OpenJS.NodeJS",
		"npm":    "OpenJS.NodeJS",
		"python": "Python.Python.3.12",
		"git":    "Git.Git",
		"pwsh":   "Microsoft.PowerShell",
		"curl":   "curl.curl",
		"7z":     "7zip.7zip",
	}

	pkg, ok := wingetMap[tool]
	if !ok {
		return nil, fmt.Errorf("不支持自动安装: %s (无 winget 包名映射)", tool)
	}

	if _, err := exec.LookPath("winget"); err != nil {
		return nil, fmt.Errorf("winget 不可用，无法自动安装")
	}

	result := a.checkEnv(tool)
	if result.Installed {
		return map[string]interface{}{
			"tool":    tool,
			"status":  "already_installed",
			"version": result.Version,
		}, nil
	}

	fmt.Printf("[InstallEnv] 正在安装 %s (%s)...\n", tool, pkg)
	cmd := exec.Command("winget", "install", pkg, "--silent", "--accept-package-agreements", "--accept-source-agreements")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	output, err := cmd.CombinedOutput()

	if err != nil {
		errMsg := fmt.Sprintf("安装 %s 失败: %v\n%s", tool, err, string(output))
		fmt.Printf("[InstallEnv] ❌ %s\n", errMsg)
		return map[string]interface{}{
			"tool":   tool,
			"status": "failed",
			"error":  errMsg,
		}, fmt.Errorf("%s", errMsg)
	}

	verifyResult := a.checkEnv(tool)
	if verifyResult.Installed {
		fmt.Printf("[InstallEnv] ✅ %s 安装成功: %s\n", tool, verifyResult.Version)
		return map[string]interface{}{
			"tool":    tool,
			"status":  "installed",
			"version": verifyResult.Version,
		}, nil
	}

	fmt.Printf("[InstallEnv] ⚠️ %s 安装命令已执行，但验证未通过（可能需要重启终端）\n", tool)
	return map[string]interface{}{
		"tool":   tool,
		"status": "installed_need_restart",
		"output": string(output),
	}, nil
}

func (a *App) CheckAllEnv() map[string]interface{} {
	tools := []string{"go", "node", "npm", "python", "git", "pwsh", "curl", "7z"}
	results := make(map[string]interface{})
	missing := 0

	for _, tool := range tools {
		result := a.checkEnv(tool)
		results[tool] = result
		if !result.Installed {
			missing++
		}
	}

	results["_summary"] = map[string]interface{}{
		"total":      len(tools),
		"installed":  len(tools) - missing,
		"missing":    missing,
		"canAutoFix": missing > 0,
	}

	return results
}

func (a *App) checkEnv(tool string) CheckResult {
	wingetMap := map[string]string{
		"go":     "GoLang.Go",
		"node":   "OpenJS.NodeJS",
		"npm":    "OpenJS.NodeJS",
		"python": "Python.Python.3.12",
		"git":    "Git.Git",
		"pwsh":   "Microsoft.PowerShell",
		"curl":   "curl.curl",
		"7z":     "7zip.7zip",
	}

	notInstalled := func(msg string) CheckResult {
		result := CheckResult{Installed: false, Message: msg}
		if pkg, ok := wingetMap[tool]; ok {
			if _, err := exec.LookPath("winget"); err == nil {
				result.CanAutoInstall = true
				result.InstallCmd = "winget install " + pkg + " --silent"
			}
		}
		return result
	}

	switch tool {
	case "go":
		path, err := exec.LookPath("go")
		if err != nil {
			return notInstalled("go 未安装")
		}
		out, err := exec.Command(path, "version").Output()
		if err != nil {
			return CheckResult{Installed: true, Version: "未知"}
		}
		return CheckResult{Installed: true, Version: strings.TrimSpace(string(out))}

	case "node":
		path, err := exec.LookPath("node")
		if err != nil {
			return notInstalled("node 未安装")
		}
		out, err := exec.Command(path, "--version").Output()
		if err != nil {
			return CheckResult{Installed: true, Version: "未知"}
		}
		return CheckResult{Installed: true, Version: strings.TrimSpace(string(out))}

	case "npm":
		path, err := exec.LookPath("npm")
		if err != nil {
			return notInstalled("npm 未安装")
		}
		out, err := exec.Command(path, "--version").Output()
		if err != nil {
			return CheckResult{Installed: true, Version: "未知"}
		}
		return CheckResult{Installed: true, Version: strings.TrimSpace(string(out))}

	case "python":
		path, err := exec.LookPath("python")
		if err != nil {
			path, err = exec.LookPath("python3")
			if err != nil {
				return notInstalled("python 未安装")
			}
		}
		out, err := exec.Command(path, "--version").Output()
		if err != nil {
			return CheckResult{Installed: true, Version: "未知"}
		}
		return CheckResult{Installed: true, Version: strings.TrimSpace(string(out))}

	case "git":
		path, err := exec.LookPath("git")
		if err != nil {
			return notInstalled("git 未安装")
		}
		out, err := exec.Command(path, "--version").Output()
		if err != nil {
			return CheckResult{Installed: true, Version: "未知"}
		}
		return CheckResult{Installed: true, Version: strings.TrimSpace(string(out))}

	case "pwsh":
		path, err := exec.LookPath("pwsh")
		if err != nil {
			return notInstalled("PowerShell 7 未安装")
		}
		out, err := exec.Command(path, "--version").Output()
		if err != nil {
			return CheckResult{Installed: true, Version: "未知"}
		}
		return CheckResult{Installed: true, Version: strings.TrimSpace(string(out))}

	case "curl":
		path, err := exec.LookPath("curl")
		if err != nil {
			return notInstalled("curl 未安装")
		}
		out, err := exec.Command(path, "--version").Output()
		if err != nil {
			return CheckResult{Installed: true, Version: "未知"}
		}
		return CheckResult{Installed: true, Version: strings.TrimSpace(string(out))}

	case "7z":
		path, err := exec.LookPath("7z")
		if err != nil {
			return notInstalled("7-Zip 未安装")
		}
		out, err := exec.Command(path, "--help").Output()
		if err != nil {
			return CheckResult{Installed: true, Version: "未知"}
		}
		lines := strings.Split(string(out), "\n")
		if len(lines) > 0 {
			return CheckResult{Installed: true, Version: strings.TrimSpace(lines[0])}
		}
		return CheckResult{Installed: true, Version: "已安装"}

	default:
		path, err := exec.LookPath(tool)
		if err != nil {
			return CheckResult{Installed: false, Message: tool + " 未安装"}
		}
		return CheckResult{Installed: true, Version: path}
	}
}

// ==================== Git验证（架构文档第十三章）====================

func (a *App) isRepoClean() bool {
	projectDir := a.getProjectDir()

	// 检查是否是Git仓库
	if _, err := os.Stat(filepath.Join(projectDir, ".git")); os.IsNotExist(err) {
		return false
	}

	// 检查是否有未提交的更改
	cmd := exec.Command("git", "-c", "core.quotepath=false", "status", "--porcelain")
	cmd.Dir = projectDir
	output, err := cmd.Output()
	if err != nil {
		return false
	}

	return len(strings.TrimSpace(string(output))) == 0
}

func (a *App) hasTagForTask(taskID string) bool {
	projectDir := a.getProjectDir()
	tagName := fmt.Sprintf("argus-task-%s", taskID)

	cmd := exec.Command("git", "tag", "-l", tagName)
	cmd.Dir = projectDir
	output, err := cmd.Output()
	if err != nil {
		return false
	}

	return strings.TrimSpace(string(output)) == tagName
}

func (a *App) createTaskTag(taskID string) error {
	projectDir := a.getProjectDir()
	tagName := fmt.Sprintf("argus-task-%s", taskID)

	cmd := exec.Command("git", "tag", "-a", tagName, "-m", fmt.Sprintf("Argus task %s completed", taskID))
	cmd.Dir = projectDir
	return cmd.Run()
}

type GitStatusEntry struct {
	Status string `json:"status"`
	Path   string `json:"path"`
	Name   string `json:"name"`
	IsDir  bool   `json:"isDir"`
}

func (a *App) GetRepoInfo() (map[string]interface{}, error) {
	info := git.GetRepoInfo(a.getProjectDir())
	if info == nil {
		info = &git.RepoInfo{}
	}
	return map[string]interface{}{
		"is_repo": info.IsRepo, "current_branch": info.CurrentBranch,
		"remote_url": info.RemoteURL, "remote_name": info.RemoteName,
		"ahead": info.Ahead, "behind": info.Behind, "is_clean": info.IsClean,
	}, nil
}

func (a *App) AddRemote(name, url string) error {
	return git.AddRemote(a.getProjectDir(), name, url)
}

func (a *App) RemoveRemote(name string) error {
	return git.RemoveRemote(a.getProjectDir(), name)
}

func (a *App) SetGitCredentials(user, pass string) {
	git.SetCredentials(user, pass)
}

func (a *App) GetFileDiff(path string) (map[string]interface{}, error) {
	result := git.GetFileDiff(a.getProjectDir(), path)
	return map[string]interface{}{
		"path":      result.Path,
		"status":    result.Status,
		"content":   result.Content,
		"additions": result.Additions,
		"deletions": result.Deletions,
	}, nil
}

func (a *App) GetCommitDiff(hash string) (map[string]interface{}, error) {
	result := git.GetCommitDiff(a.getProjectDir(), hash)
	return map[string]interface{}{
		"path":      result.Path,
		"status":    result.Status,
		"content":   result.Content,
		"additions": result.Additions,
		"deletions": result.Deletions,
	}, nil
}

func (a *App) GetCommitLog(limit int) ([]git.CommitLogEntry, error) {
	return git.GetCommitLog(a.getProjectDir(), limit)
}
func (a *App) GetBranches() ([]git.BranchInfo, error) { return git.GetBranches(a.getProjectDir()) }
func (a *App) GetRemotes() ([]git.RemoteInfo, error)  { return git.GetRemotes(a.getProjectDir()) }
func (a *App) GitClone(url, dir, branch string) map[string]interface{} {
	// [FIX-20260528-GIT] 安全检查：防止clone到工作目录导致"女婿上丈母娘的床"
	absDir, _ := filepath.Abs(dir)
	absWorkDir, _ := filepath.Abs(a.config.WorkDir)

	fmt.Printf("[GitClone] Clone请求: url=%s, dir=%s, branch=%s\n", url, absDir, branch)
	fmt.Printf("[GitClone] 工作目录: %s\n", absWorkDir)

	// 检查1: 是否是工作目录本身或其子目录
	if strings.EqualFold(absDir, absWorkDir) || strings.HasPrefix(strings.ToLower(absDir), strings.ToLower(absWorkDir)+string(filepath.Separator)) {
		errMsg := fmt.Sprintf("⚠️ 危险操作：不能Clone到工作目录(%s)！这会导致C监控auto-commit污染主仓库。请选择其他目录。", absWorkDir)
		fmt.Printf("[GitClone] ❌ %s\n", errMsg)
		return map[string]interface{}{
			"success": false,
			"error":   errMsg,
			"output":  "BLOCKED: Cannot clone to work directory",
		}
	}

	// 检查2: 目标目录是否已存在且非空（防止覆盖重要文件）
	if _, err := os.Stat(absDir); err == nil {
		files, _ := os.ReadDir(absDir)
		if len(files) > 0 {
			// 检查是否已有.git目录
			if _, gitErr := os.Stat(filepath.Join(absDir, ".git")); gitErr == nil {
				errMsg := fmt.Sprintf("⚠️ 目标目录%s已是Git仓库，Clone会覆盖现有内容！", absDir)
				fmt.Printf("[GitClone] ❌ %s\n", errMsg)
				return map[string]interface{}{
					"success": false,
					"error":   errMsg,
					"output":  "BLOCKED: Target is already a git repository",
				}
			}
			fmt.Printf("[GitClone] ⚠️ 目标目录非空(%d个文件)，但允许继续\n", len(files))
		}
	}

	fmt.Printf("[GitClone] ✅ 安全检查通过，开始Clone...\n")
	r := git.Clone(url, dir, branch)
	return map[string]interface{}{"success": r.Success, "output": r.Output, "error": r.Error}
}
func (a *App) GitPush(remote, branch string) (string, error) {
	return git.Push(a.getProjectDir(), remote, branch)
}
func (a *App) GitPull(remote, branch string) (string, error) {
	return git.Pull(a.getProjectDir(), remote, branch)
}
func (a *App) SwitchBranch(name string) error { return git.SwitchBranch(a.getProjectDir(), name) }
func (a *App) CreateBranch(name string) error { return git.CreateBranch(a.getProjectDir(), name) }
func (a *App) GitInit() error                 { return git.InitRepo(a.getProjectDir()) }

func (a *App) GetGitStatus() ([]GitStatusEntry, error) {
	rawEntries := git.GetStatus(a.getProjectDir())
	var entries []GitStatusEntry
	for _, e := range rawEntries {
		status, _ := e["status"].(string)
		path, _ := e["path"].(string)
		entries = append(entries, GitStatusEntry{
			Status: status,
			Path:   path,
			Name:   filepath.Base(path),
			IsDir:  false,
		})
	}
	return entries, nil
}

func (a *App) TrackFile(path string) error {
	return git.StageFile(a.getProjectDir(), path)
}

func (a *App) UnTrackFile(path string) error {
	return git.UnstageFile(a.getProjectDir(), path)
}

func (a *App) SaveVersion(message string) error {
	_, err := git.Commit(a.getProjectDir(), message)
	return err
}

func (a *App) RestoreFile(path string) error {
	return git.RestoreFile(a.getProjectDir(), path)
}

type VerificationResult struct {
	Check     string `json:"check"`
	Passed    bool   `json:"passed"`
	Message   string `json:"message"`
	Timestamp string `json:"timestamp"`
}

func (a *App) VerifySEWork(taskID string) ([]VerificationResult, error) {
	var results []VerificationResult
	now := time.Now().Format("2006-01-02 15:04:05")

	results = append(results, a.checkGitClean(taskID, now))
	results = append(results, a.checkGitTag(taskID, now))
	results = append(results, a.checkFileChanges(taskID, now))
	results = append(results, a.checkCompilation(taskID, now))

	if a.memoryManager != nil {
		results = append(results, a.checkMemoryConsistency(taskID, now))
	}

	return results, nil
}

func (a *App) checkGitClean(taskID string, timestamp string) VerificationResult {
	clean := a.isRepoClean()
	return VerificationResult{
		Check:  "工作区清洁",
		Passed: clean,
		Message: func() string {
			if clean {
				return "工作区干净"
			} else {
				return "存在未提交的更改"
			}
		}(),
		Timestamp: timestamp,
	}
}

func (a *App) checkGitTag(taskID string, timestamp string) VerificationResult {
	hasTag := a.hasTagForTask(taskID)
	return VerificationResult{
		Check:  "Git Tag 一致性",
		Passed: hasTag,
		Message: func() string {
			if hasTag {
				return "已找到任务 Tag"
			} else {
				return "未找到任务 Tag"
			}
		}(),
		Timestamp: timestamp,
	}
}

func (a *App) checkFileChanges(taskID string, timestamp string) VerificationResult {
	projectDir := a.getProjectDir()

	cmd := exec.Command("git", "diff", "--name-only", "HEAD")
	cmd.Dir = projectDir
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	output, err := cmd.Output()
	if err != nil {
		return VerificationResult{
			Check:     "文件变更",
			Passed:    false,
			Message:   "检查失败：" + err.Error(),
			Timestamp: timestamp,
		}
	}

	changedFiles := strings.Split(strings.TrimSpace(string(output)), "\n")
	changedCount := 0
	for _, f := range changedFiles {
		if f != "" {
			changedCount++
		}
	}

	return VerificationResult{
		Check:     "文件变更",
		Passed:    changedCount > 0,
		Message:   fmt.Sprintf("共变更 %d 个文件", changedCount),
		Timestamp: timestamp,
	}
}

func (a *App) checkCompilation(taskID string, timestamp string) VerificationResult {
	projectDir := a.getProjectDir()

	var cmd *exec.Cmd
	if _, err := os.Stat(filepath.Join(projectDir, "Makefile")); err == nil {
		cmd = exec.Command("make")
	} else if _, err := os.Stat(filepath.Join(projectDir, "CMakeLists.txt")); err == nil {
		cmd = exec.Command("cmake", "--build", ".")
	} else {
		return VerificationResult{
			Check:     "编译检查",
			Passed:    true,
			Message:   "未找到构建系统，跳过编译检查",
			Timestamp: timestamp,
		}
	}

	cmd.Dir = projectDir
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	output, err := cmd.CombinedOutput()

	if err != nil {
		return VerificationResult{
			Check:     "编译检查",
			Passed:    false,
			Message:   "编译失败：" + string(output),
			Timestamp: timestamp,
		}
	}

	return VerificationResult{
		Check:     "编译检查",
		Passed:    true,
		Message:   "编译成功",
		Timestamp: timestamp,
	}
}

func (a *App) checkMemoryConsistency(taskID string, timestamp string) VerificationResult {
	if a.memoryManager == nil {
		return VerificationResult{
			Check:     "记忆一致性",
			Passed:    true,
			Message:   "记忆系统未初始化",
			Timestamp: timestamp,
		}
	}

	issues, err := a.memoryManager.GetOpenIssues(taskID)
	if err != nil {
		return VerificationResult{
			Check:     "记忆一致性",
			Passed:    false,
			Message:   "检查失败：" + err.Error(),
			Timestamp: timestamp,
		}
	}

	openIssues := 0
	criticalIssues := 0
	for _, issue := range issues {
		if issue.Status == "open" {
			openIssues++
		}
		if issue.Severity == "critical" && issue.Status == "open" {
			criticalIssues++
		}
	}

	if criticalIssues > 0 {
		return VerificationResult{
			Check:     "记忆一致性",
			Passed:    false,
			Message:   fmt.Sprintf("存在 %d 个严重问题未解决", criticalIssues),
			Timestamp: timestamp,
		}
	}

	return VerificationResult{
		Check:     "记忆一致性",
		Passed:    openIssues == 0,
		Message:   fmt.Sprintf("开放问题：%d 个", openIssues),
		Timestamp: timestamp,
	}
}

func (a *App) CheckProcessExists(processName string) bool {
	cmd := exec.Command("tasklist", "/FI", fmt.Sprintf("IMAGENAME eq %s", processName), "/NH")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	output, err := cmd.Output()
	if err != nil {
		return false
	}

	return strings.Contains(strings.ToLower(string(output)), strings.ToLower(processName))
}

func (a *App) CheckFileUpdated(filePath string, sinceSeconds int64) (bool, error) {
	fullPath := filepath.Join(a.getProjectDir(), filePath)

	info, err := os.Stat(fullPath)
	if err != nil {
		return false, err
	}

	modTime := info.ModTime()
	elapsed := time.Since(modTime).Seconds()

	return int64(elapsed) <= sinceSeconds, nil
}

type TestResult struct {
	Passed bool     `json:"passed"`
	Total  int      `json:"total"`
	Failed int      `json:"failed"`
	Errors []string `json:"errors"`
}

func (a *App) ParseTestResult(testOutput string) TestResult {
	result := TestResult{Passed: true}

	lines := strings.Split(testOutput, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)

		if strings.Contains(line, "PASS") && strings.Contains(line, "tests") {
			parts := strings.Fields(line)
			for i, part := range parts {
				if part == "PASS" && i+1 < len(parts) {
					fmt.Sscanf(parts[i+1], "%d", &result.Total)
				}
			}
		}

		if strings.Contains(line, "FAIL") {
			result.Passed = false
			result.Failed++
			result.Errors = append(result.Errors, line)
		}

		if strings.Contains(line, "error") || strings.Contains(line, "Error") {
			result.Passed = false
			result.Errors = append(result.Errors, line)
		}
	}

	return result
}

func (a *App) CheckPortListening(port int) bool {
	addr := fmt.Sprintf(":%d", port)

	cmd := exec.Command("netstat", "-ano")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	output, err := cmd.Output()
	if err != nil {
		return false
	}

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.Contains(line, addr) && strings.Contains(line, "LISTENING") {
			return true
		}
	}

	return false
}

func (a *App) UpdateProjectState(taskID string, summary string) error {
	projectDir := a.getProjectDir()
	statePath := filepath.Join(projectDir, "docs", "project_state.md")

	if err := os.MkdirAll(filepath.Dir(statePath), 0755); err != nil {
		return err
	}

	now := time.Now().Format("2006-01-02 15:04:05")

	var content string
	if existing, err := os.ReadFile(statePath); err == nil {
		content = string(existing)
	}

	newEntry := fmt.Sprintf("## %s - 任务 %s\n\n%s\n\n", now, taskID, summary)

	content = newEntry + content

	return os.WriteFile(statePath, []byte(content), 0644)
}

func (a *App) LogPMAudit(taskID string, action string, details string) error {
	projectDir := a.getProjectDir()
	auditPath := filepath.Join(projectDir, "docs", "pm_audit_log.md")

	if err := os.MkdirAll(filepath.Dir(auditPath), 0755); err != nil {
		return err
	}

	now := time.Now().Format("2006-01-02 15:04:05")

	var content string
	if existing, err := os.ReadFile(auditPath); err == nil {
		content = string(existing)
	}

	newEntry := fmt.Sprintf("### %s - 任务 %s\n\n- **操作**: %s\n- **详情**: %s\n\n", now, taskID, action, details)

	content = newEntry + content

	return os.WriteFile(auditPath, []byte(content), 0644)
}

type PMReviewResult struct {
	TaskID    string               `json:"taskId"`
	Passed    bool                 `json:"passed"`
	Checks    []VerificationResult `json:"checks"`
	Summary   string               `json:"summary"`
	NeedsFix  []string             `json:"needsFix"`
	Timestamp string               `json:"timestamp"`
}

func (a *App) ReviewSEWork(taskID string) PMReviewResult {
	now := time.Now().Format("2006-01-02 15:04:05")

	checks, err := a.VerifySEWork(taskID)
	if err != nil {
		return PMReviewResult{
			TaskID:    taskID,
			Passed:    false,
			Summary:   "验证失败：" + err.Error(),
			Timestamp: now,
		}
	}

	allPassed := true
	var needsFix []string
	var failedChecks []string

	for _, check := range checks {
		if !check.Passed {
			allPassed = false
			failedChecks = append(failedChecks, check.Check)
			needsFix = append(needsFix, check.Message)
		}
	}

	result := PMReviewResult{
		TaskID:    taskID,
		Passed:    allPassed,
		Checks:    checks,
		Timestamp: now,
		NeedsFix:  needsFix,
	}

	if allPassed {
		result.Summary = "SE 工作审核通过，所有验证项均通过"
		_ = a.LogPMAudit(taskID, "审核通过", "所有验证项通过")
		_ = a.UpdateProjectState(taskID, "任务已完成并通过 PM 审核")

		if a.memoryManager != nil {
			_ = a.memoryManager.CompleteTask(taskID)
		}
	} else {
		result.Summary = fmt.Sprintf("SE 工作审核未通过，%d 项验证失败：%s", len(failedChecks), strings.Join(failedChecks, "、"))
		_ = a.LogPMAudit(taskID, "审核未通过", result.Summary)

		if a.memoryManager != nil {
			_ = a.memoryManager.AddIssue(taskID, result.Summary, "critical", nil, strings.Join(needsFix, "; "))
		}
	}

	return result
}

type SEWorker struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Status      string `json:"status"`
	CurrentTask string `json:"currentTask"`
	StartedAt   string `json:"startedAt"`
}

func (a *App) GetSEWorkers() []SEWorker {
	status := "idle"
	currentTask := ""
	if a.chatManager != nil {
		_, seRunning := a.chatManager.GetExecutionStatus()
		if seRunning {
			status = "running"
		}
	}
	return []SEWorker{
		{
			ID:          "se-1",
			Name:        "SE-1 (主工程师)",
			Status:      status,
			CurrentTask: currentTask,
			StartedAt:   "",
		},
	}
}

func (a *App) AddSEWorker(name string) SEWorker {
	worker := SEWorker{
		ID:        fmt.Sprintf("se-%d", time.Now().UnixNano()),
		Name:      name,
		Status:    "idle",
		StartedAt: time.Now().Format("2006-01-02 15:04:05"),
	}

	a.addLog("新增 SE 工程师：" + name)

	return worker
}

func (a *App) AssignTaskToSE(taskID string, workerID string) error {
	a.addLog(fmt.Sprintf("任务 %s 已分配给 %s", taskID, workerID))

	if a.memoryManager != nil {
		_ = a.memoryManager.AddKnowledge(taskID, "任务分配", fmt.Sprintf("任务已分配给 %s", workerID), "", nil, 1.0, "system", 0)
	}

	return nil
}

// ==================== 聊天功能 ====================

func (a *App) GetMessages() []ChatMessage {
	fmt.Printf("[GetMessages] returning app.messages: %d\n", len(a.messages))
	return a.messages
}

// GetPendingQueue 获取当前等待处理的消息队列
func (a *App) GetPendingQueue() []string {
	if a.chatManager == nil {
		return []string{}
	}
	return a.chatManager.GetPendingQueue()
}

// GetGlobalTasks 获取当前所有全局任务（供前端/调试查询）
func (a *App) GetGlobalTasks() string {
	if a.chatManager == nil {
		return "[]"
	}
	tasks := a.chatManager.GetAllTasks()
	data, err := json.Marshal(tasks)
	if err != nil {
		a.addLog("[GetGlobalTasks] json marshal error")
		return "[]"
	}
	return string(data)
}

// EmitTaskClarify 发送需求澄清请求到前端（触发 TaskClarify 组件）
func (a *App) EmitTaskClarify(questionsJSON string) {
	a.addLog(fmt.Sprintf("[EmitTaskClarify] 发送澄清请求: %s", questionsJSON))
	a.emitToFrontend("task-clarify", questionsJSON, "EmitTaskClarify", chat.PathPMToUser)
}

// CheckUnfinishedTask 检查是否有未完成任务（前端启动时调用）
func (a *App) CheckUnfinishedTask() (bool, string, string, error) {
	if a.chatManager == nil {
		return false, "", "", nil
	}

	hasUnfinished, memory, err := a.chatManager.CheckUnfinishedTask()
	if err != nil {
		return false, "", "", err
	}

	if !hasUnfinished || memory == nil {
		return false, "", "", nil
	}

	fmt.Printf("[CheckUnfinishedTask] 发现未完成任务: %s\n", memory.TaskDescription)
	return true, memory.UserRequest, memory.TaskDescription, nil
}

// RecoverTask 恢复未完成任务
func (a *App) RecoverTask() ([]ChatMessage, error) {
	if a.chatManager == nil {
		return nil, fmt.Errorf("chat manager not initialized")
	}

	// 先获取记忆
	hasUnfinished, memory, _ := a.chatManager.CheckUnfinishedTask()
	if !hasUnfinished || memory == nil {
		return nil, fmt.Errorf("no unfinished task to recover")
	}

	// 恢复任务到 ChatManager
	if err := a.chatManager.RecoverTask(memory); err != nil {
		return nil, fmt.Errorf("recover task failed: %v", err)
	}

	// 转换消息为前端格式（types.Message -> ChatMessage）
	var messages []ChatMessage
	for i, msg := range memory.RecentMessages {
		messages = append(messages, ChatMessage{
			ID:        int64(i + 1),
			Role:      msg.Role,
			Content:   msg.Content,
			Timestamp: msg.Timestamp.Unix(),
		})
	}

	// 更新 app.messages
	a.messages = messages

	fmt.Printf("[RecoverTask] 任务恢复成功，消息数: %d\n", len(messages))
	return messages, nil
}

func (a *App) IsAIThinking() bool {
	return a.aiThinking
}

func (a *App) StopCurrentTask() error {
	a.aiThinking = false
	a.emitToFrontend("ai-thinking", false, "StopCurrentTask", chat.PathSystem)
	if a.bridge != nil {
		a.bridge.Cancel()
	}
	if a.chatManager != nil {
		a.chatManager.StopCurrentTask()
		a.chatManager.SetUserStopped(true)
	}
	a.addLog("🛑 用户手动停止当前任务")
	return nil
}

func (a *App) IsPMThinking() bool {
	// 优先从 ChatManager 获取状态
	if a.chatManager != nil {
		pmBusy, _ := a.chatManager.GetExecutionStatus()
		return pmBusy
	}
	return a.aiThinking
}

func (a *App) IsCRunning() bool {
	// 检查App的C守护进程状态，或者ChatManager的C监控状态
	if a.cRunning {
		return true
	}
	// 也检查ChatManager的C监控
	if a.chatManager != nil {
		return a.chatManager.IsCMonitorRunning()
	}
	return false
}

func (a *App) IsSERunning() bool {
	if a.chatManager != nil {
		_, seRunning := a.chatManager.GetExecutionStatus()
		return seRunning
	}
	return false
}

func (a *App) IsAPThinking() bool {
	if a.chatManager != nil {
		return a.chatManager.IsAPReviewing()
	}
	return false
}

// SetLang 设置语言（前端切换时调用）
func (a *App) SetLang(lang string) {
	i18n.SetLang(lang)
	a.addLog(fmt.Sprintf("【语言】切换到: %s", lang))
	// 如果 chatManager 已初始化，也同步语言设置
	if a.chatManager != nil {
		a.chatManager.SetReplyLanguage(lang)
	}
}

func (a *App) SendMessage(content string) error {
	a.sendMu.Lock()
	isBusy := a.isSending || a.aiThinking
	if a.bridge != nil && a.bridge.IsProcessing() {
		isBusy = true
	}
	if isBusy {
		a.sendMu.Unlock()
		fmt.Printf("[SendMessage] ⚡ AI繁忙，中断当前任务并立即处理: %s\n", truncate(content, 50))
		if a.bridge != nil {
			a.bridge.Cancel()
		}
		if a.chatManager != nil {
			a.chatManager.SetUserStopped(true)
			a.chatManager.StopCurrentTask()
		}
		time.Sleep(100 * time.Millisecond)
		a.sendMu.Lock()
	}
	a.isSending = true
	a.sendTaskID++
	taskID := a.sendTaskID
	a.sendMu.Unlock()

	defer func() {
		a.sendMu.Lock()
		a.isSending = false
		a.sendMu.Unlock()
	}()

	a.writeDebugLog(fmt.Sprintf("[SendMessage] CALLED content=%s", truncate(content, 50)))
	fmt.Printf("[SendMessage] Step 1: 函数开始\n")
	fmt.Printf("[SendMessage] Step 2: 收到消息: %s\n", content)

	if strings.TrimSpace(content) != "" {
		apiCfg := a.getDefaultAPIConfig()
		// 快速验证API配置完整性，避免进入PM→SE流水线后才超时报错
		if apiCfg == nil || strings.TrimSpace(apiCfg.APIKey) == "" {
			errMsg := "⚠️ API Key 未配置，请先在设置中填写 API Key"
			a.addLog(errMsg)
			a.messages = append(a.messages, a.newChatMessage("error", errMsg))
			a.saveMessages()
			lastMsg := a.messages[len(a.messages)-1]
			a.emitToFrontend("new-message", lastMsg, "SendMessage:NoAPIKey", chat.PathSystem)
			return fmt.Errorf("%s", errMsg)
		}
		if strings.TrimSpace(apiCfg.BaseURL) == "" {
			errMsg := "⚠️ API 地址(BaseURL)未配置，请先在设置中填写"
			a.addLog(errMsg)
			a.messages = append(a.messages, a.newChatMessage("error", errMsg))
			a.saveMessages()
			lastMsg := a.messages[len(a.messages)-1]
			a.emitToFrontend("new-message", lastMsg, "SendMessage:NoBaseURL", chat.PathSystem)
			return fmt.Errorf("%s", errMsg)
		}
		if strings.TrimSpace(apiCfg.ModelName) == "" {
			errMsg := "⚠️ 模型名称(Model)未配置，请先在设置中选择模型"
			a.addLog(errMsg)
			a.messages = append(a.messages, a.newChatMessage("error", errMsg))
			a.saveMessages()
			lastMsg := a.messages[len(a.messages)-1]
			a.emitToFrontend("new-message", lastMsg, "SendMessage:NoModel", chat.PathSystem)
			return fmt.Errorf("%s", errMsg)
		}
	}

	// ✅ 等待 ChatManager 初始化完成（防止竞态条件）
	fmt.Println("[SendMessage] ⏳ 等待 ChatManager 初始化...")
	select {
	case <-a.readyChan:
		fmt.Println("[SendMessage] ✅ ChatManager 已就绪")
	case <-time.After(10 * time.Second):
		fmt.Println("[SendMessage] ⚠️ 等待超时（10秒），继续执行")
	}

	fmt.Printf("[SendMessage] Step 3: chatManager: %v\n", a.chatManager != nil)

	if a.chatManager == nil {
		errMsg := "⚠️ 请先设置工作目录（点击顶部「工作目录」按钮选择项目文件夹）"
		a.addLog(errMsg)
		chatMsg := a.newChatMessage("error", errMsg)
		a.messages = append(a.messages, chatMsg)
		a.saveMessages()
		a.emitToFrontend("new-message", chatMsg, "SendMessage:NoWorkDir", chat.PathSystem)
		return fmt.Errorf("%s", errMsg)
	}

	// 使用新的 ChatManager 处理消息
	if a.chatManager != nil {
		fmt.Printf("[SendMessage] Step 4: 进入 chatManager 分支\n")
		// 如果是空消息，打断AI工作
		if strings.TrimSpace(content) == "" {
			fmt.Printf("[SendMessage] Step 5: 空消息，停止AI\n")
			a.aiThinking = false
			a.emitToFrontend("ai-thinking", false, "SendMessage:Stop", chat.PathSystem)

			if a.bridge != nil {
				a.bridge.Cancel()
			}
			if a.chatManager != nil {
				a.chatManager.SetUserStopped(true)
				a.chatManager.StopCurrentTask()
				fmt.Printf("[SendMessage] Step 6: 已设置 userStopped 标志，清理记忆文件\n")
			}

			return nil
		}

		if !strings.HasPrefix(content, "[钉钉]") {
			a.addLog("【SendMessage】准备发送钉钉消息")
			a.sendToDingTalk(fmt.Sprintf("[USR] %s", content))
		}

		a.addLog("【SendMessage】准备处理消息（V2 ArgusCore）")
		a.aiThinking = true
		a.emitToFrontend("ai-thinking", true, "SendMessage:Start", chat.PathSystem)

		a.msgIDCounter++
		userMsg := a.newChatMessage("user", content)
		userMsg.ID = a.msgIDCounter
		a.messages = append(a.messages, userMsg)
		a.saveMessages()
		a.emitToFrontend("new-message", userMsg, "SendMessage:UserInput", chat.PathUserInput)

		if a.bridge != nil {
			fmt.Printf("[SendMessage] 🚀 V2 Bridge.Process (async): %s\n", content)
			go func() {
				result, err := a.bridge.Process(content)

				a.sendMu.Lock()
				if taskID != a.sendTaskID {
					a.sendMu.Unlock()
					return
				}
				a.sendMu.Unlock()

				if result == nil {
					a.addLog(fmt.Sprintf("【V2-Error】result is nil (可能被中断): %v", err))
					a.aiThinking = false
					a.emitToFrontend("ai-thinking", false, "SendMessage:Done", chat.PathSystem)
					return
				}

				fmt.Printf("[SendMessage] V2 返回: success=%v err=%v phases=%d\n",
					result.Success, err, len(result.Phases))

				if err != nil {
					a.addLog(fmt.Sprintf("【V2-Error】%v", err))
					detail := ""
					if len(result.Outputs) > 0 {
						detail = strings.Join(result.Outputs, "\n")
					}
					if len(result.Phases) > 0 {
						for _, p := range result.Phases {
							if p.Output != "" {
								detail += "\n---\n" + p.Output
							}
						}
					}
					errorMsg := a.newChatMessage("error", fmt.Sprintf("V2 Error: %v\n\n%s", err, detail))
					errorMsg.Summary = "System"
					a.messages = append(a.messages, errorMsg)
					a.saveMessages()
					a.emitToFrontend("new-message", errorMsg, "SendMessage:V2Error", chat.PathSystem)
				} else {
					a.addLog(fmt.Sprintf("【V2-Done】success=%v actions=%d duration=%v",
						result.Success, len(result.Actions), result.Duration))
				}

				a.aiThinking = false
				a.emitToFrontend("ai-thinking", false, "SendMessage:Done", chat.PathSystem)
				// 异步处理完成：自动确认待处理消息（HTTP API 无前端 ACK）
				a.ackPendingMessages()
			}()
		}

		return nil
	}

	// 不可达：Bridge 和 ChatManager 至少一个已初始化
	a.addLog("【SendMessage】⚠️ 既无Bridge也无ChatManager")
	return fmt.Errorf("chat system not initialized")
}

// 添加PM消息
func (a *App) addPMMessage(content string) {
	aiMsg := a.newChatMessage("pm", content)
	aiMsg.Summary = "PM"
	a.messages = append(a.messages, aiMsg)
	a.saveMessages()
	a.emitToFrontend("new-message", aiMsg, "addPMMsg", chat.PathPMToUser)

	a.sendToDingTalk(fmt.Sprintf("[PM] %s", content))
}

// 添加SE消息
func (a *App) addSEMessage(content string) {
	aiMsg := a.newChatMessage("se", content)
	aiMsg.Summary = "SE"
	a.messages = append(a.messages, aiMsg)
	a.saveMessages()
	a.emitToFrontend("new-message", aiMsg, "addSEMsg", chat.PathSEToUser)

	a.sendToDingTalk(fmt.Sprintf("[SE] %s", content))
}

// ==================== AI API调用 ====================

func (a *App) getDefaultAPIConfig() *APIConfig {
	for i := range a.config.APIConfigs {
		if a.config.APIConfigs[i].IsDefault {
			return &a.config.APIConfigs[i]
		}
	}
	if len(a.config.APIConfigs) > 0 {
		return &a.config.APIConfigs[0]
	}
	return nil
}

// findAPIConfigByID 根据ID查找模型配置
func (a *App) findAPIConfigByID(id string) *APIConfig {
	if id == "" {
		return nil
	}
	for i := range a.config.APIConfigs {
		if a.config.APIConfigs[i].ID == id {
			return &a.config.APIConfigs[i]
		}
	}
	return nil
}

// GetCurrentAPIConfigID 返回当前正在使用的API配置ID（供前端显示"当前使用"标识）
func (a *App) GetCurrentAPIConfigID() string {
	config := a.getDefaultAPIConfig()
	if config != nil {
		return config.ID
	}
	return ""
}

// SwitchAPIConfig 切换当前使用的API配置（不重启，立即生效）
func (a *App) SwitchAPIConfig(configID string) error {
	for i := range a.config.APIConfigs {
		if a.config.APIConfigs[i].ID == configID {
			a.config.APIConfigs[i].IsDefault = true
		} else {
			a.config.APIConfigs[i].IsDefault = false
		}
	}
	selectedConfig := a.getDefaultAPIConfig()
	if selectedConfig != nil && a.chatManager != nil {
		a.chatManager.UpdateAPIConfig(types.APIConfig{
			Provider: selectedConfig.Provider,
			BaseURL:  selectedConfig.BaseURL,
			APIKey:   selectedConfig.APIKey,
			Model:    selectedConfig.ModelName,
		})
	}
	a.saveConfigToFile()
	return nil
}

type APITestResult struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

func (a *App) TestAPIConfig(provider, baseUrl, apiKey, modelName string) APITestResult {
	if apiKey == "" {
		return APITestResult{Success: false, Message: "API Key 不能为空"}
	}
	if baseUrl == "" {
		return APITestResult{Success: false, Message: "Base URL 不能为空"}
	}
	if modelName == "" {
		return APITestResult{Success: false, Message: "模型名称不能为空"}
	}
	testConfig := &APIConfig{
		Provider:  provider,
		BaseURL:   strings.TrimRight(baseUrl, "/"),
		APIKey:    apiKey,
		ModelName: modelName,
	}
	messages := []map[string]string{
		{"role": "user", "content": "hi"},
	}
	content, err := a.callAIAPI(testConfig, messages)
	if err != nil {
		return APITestResult{Success: false, Message: err.Error()}
	}
	resp := content
	if len(resp) > 50 {
		resp = resp[:50]
	}
	return APITestResult{Success: true, Message: "连接成功，模型响应: " + resp}
}

func (a *App) callAIAPI(apiConfig *APIConfig, messages []map[string]string) (string, error) {
	requestBody := map[string]interface{}{
		"model":    apiConfig.ModelName,
		"messages": messages,
		"stream":   false,
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequest("POST", apiConfig.BaseURL+"/chat/completions", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiConfig.APIKey)

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("API返回错误：%d - %s", resp.StatusCode, string(body))
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	choices, ok := result["choices"].([]interface{})
	if !ok || len(choices) == 0 {
		return "", fmt.Errorf("无响应内容")
	}

	choice := choices[0].(map[string]interface{})
	message := choice["message"].(map[string]interface{})
	content, ok := message["content"].(string)
	if !ok || content == "" {
		if rc, hasRC := message["reasoning_content"].(string); hasRC && rc != "" {
			content = rc
		} else if message["content"] == nil {
			return "", fmt.Errorf("模型返回空内容(content=null)，请尝试更换模型")
		} else {
			rawContent := message["content"]
			return "", fmt.Errorf("响应格式错误: content类型=%T, value=%v", rawContent, rawContent)
		}
	}

	return content, nil
}

// ==================== 辅助函数 ====================

func (a *App) addErrorMessage(errorMsg string) {
	errMsg := a.newChatMessage("error", "")
	errMsg.Error = errorMsg
	a.messages = append(a.messages, errMsg)
	a.saveMessages()
	a.emitToFrontend("new-message", errMsg, "addError", chat.PathSystem)
	a.sendToDingTalk(fmt.Sprintf("[ERR] %s", errorMsg))
}

func (a *App) initMemorySystem() {
	projectDir := a.getProjectDir()
	if projectDir == "" || projectDir == "." {
		a.addLog("警告：未设置项目目录，记忆系统未初始化")
		return
	}

	mm, err := memory.NewMemoryManager(projectDir)
	if err != nil {
		a.addLog("记忆系统初始化失败：" + err.Error())
		return
	}

	a.memoryManager = mm
	a.contextBuilder = memory.NewContextBuilder(mm)
	a.compressor = memory.NewCompressor(mm)
	a.contextWindow = memory.NewContextWindow(memory.DefaultContextBudget())

	a.addLog("记忆系统已初始化（SQLite + FTS5）")
}

// initDingTalk 初始化钉钉企业内部机器人（Stream模式）
func (a *App) initDingTalk() {
	var clientID, clientSecret string
	hasIMConfig := false

	for _, im := range a.config.IMConfigs {
		if im.Provider == "dingtalk" {
			hasIMConfig = true
			if im.Enabled {
				clientID = im.ClientID
				clientSecret = im.ClientSecret
			}
			break
		}
	}

	if !hasIMConfig && clientID == "" && a.config.DingTalk.Enabled {
		clientID = a.config.DingTalk.ClientID
		clientSecret = a.config.DingTalk.ClientSecret
	}

	a.addLog(fmt.Sprintf("钉钉配置状态: clientId=%s", clientID))

	if clientID == "" || clientSecret == "" {
		a.addLog("钉钉机器人 ClientID 或 ClientSecret 未配置")
		return
	}

	// 设置消息处理器
	handler := func(content string, sender string) {
		a.addLog(fmt.Sprintf("【App】钉钉消息处理器被调用: %s from %s", content, sender))

		if !a.isDingTalkEnabled() {
			a.addLog("【App】钉钉已禁用，忽略消息")
			return
		}

		a.addLog("【App】调用 SendMessage 统一处理钉钉消息")
		go a.SendMessage("[钉钉] " + content)
	}

	// 初始化 Stream 模式（长连接，无需公网地址）
	a.addLog("正在启动钉钉 Stream 客户端...")
	dingtalk.InitStream(dingtalk.StreamConfig{
		Enabled:      true,
		ClientID:     clientID,
		ClientSecret: clientSecret,
	}, handler)

	a.addLog("钉钉 Stream 机器人已启动（长连接模式）")
}

func (a *App) getProjectDir() string {
	fmt.Printf("[getProjectDir] 开始解析工作目录...\n")
	fmt.Printf("[getProjectDir] config.WorkDir=%q\n", a.config.WorkDir)
	fmt.Printf("[getProjectDir] useCWD=%v\n", a.useCWD)

	if a.config.WorkDir != "" {
		absWorkDir, _ := filepath.Abs(a.config.WorkDir)
		fmt.Printf("[getProjectDir] 使用配置的 WorkDir: %s (abs: %s)\n", a.config.WorkDir, absWorkDir)

		if a.isDangerousWorkDir(absWorkDir) {
			fallback := filepath.Join(os.TempDir(), "argus-workspace")
			os.MkdirAll(fallback, 0755)
			fmt.Printf("[getProjectDir] ⚠️ WorkDir 在危险路径中，强制使用: %s\n", fallback)
			return fallback
		}

		return a.config.WorkDir
	}

	cwd, err := os.Getwd()
	if err == nil {
		fmt.Printf("[getProjectDir] 当前工作目录 cwd=%s\n", cwd)
	}

	if a.useCWD {
		if err == nil {
			absCwd, _ := filepath.Abs(cwd)

			if a.isDangerousWorkDir(absCwd) {
				fallback := filepath.Join(os.TempDir(), "argus-workspace")
				os.MkdirAll(fallback, 0755)
				fmt.Printf("[getProjectDir] ⚠️ CLI模式下 cwd在危险路径中，强制使用: %s\n", fallback)
				return fallback
			}

			fmt.Printf("[getProjectDir] CLI模式，使用 cwd: %s\n", cwd)
			return cwd
		}
	}

	fmt.Printf("[getProjectDir] ⚠️ work_dir 未配置，请通过界面选择工作目录\n")
	return ""
}

// startFileWatcher 启动文件变更检测（2s间隔，纯标准库，无闪烁）
func (a *App) startFileWatcher(dir string) {
	a.stopFileWatcher()
	if dir == "" {
		return
	}
	a.fileWatcherStop = make(chan struct{})
	a.fileSnapshot = make(map[string]int64)
	fmt.Printf("[FileWatcher] start watching: %s\n", dir)

	// 构建初始快照
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		info, err := e.Info()
		if err == nil {
			a.fileSnapshot[e.Name()] = info.ModTime().Unix()
		}
	}

	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				a.pollFileChanges(dir)
			case <-a.fileWatcherStop:
				return
			}
		}
	}()
}

func (a *App) stopFileWatcher() {
	if a.fileWatcherStop != nil {
		close(a.fileWatcherStop)
		a.fileWatcherStop = nil
	}
}

func (a *App) pollFileChanges(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	changed := false
	current := make(map[string]int64)

	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		mt := info.ModTime().Unix()
		current[e.Name()] = mt
		if oldMT, ok := a.fileSnapshot[e.Name()]; !ok || oldMT != mt {
			changed = true
		}
	}

	// 检查是否有文件被删除
	if len(current) != len(a.fileSnapshot) {
		changed = true
	}

	if changed {
		a.fileSnapshot = current
		if a.ctx != nil {
			runtime.EventsEmit(a.ctx, "file-tree-dirty", map[string]interface{}{"dir": dir})
		}
	}
}

// [v0.7.1] initMCPManager 初始化 MCP Manager，启动配置中的所有 Server
func (a *App) initMCPManager(workDir string) {
	servers := a.config.MCPServers
	if len(servers) == 0 {
		fmt.Printf("[MCP] ℹ️ 无 MCP Server 配置，跳过初始化\n")
		return
	}

	a.mcpManager = mcp.NewManager(workDir)

	// Initialize Debugger Manager
	a.debuggerMgr = debugger.NewDebugSessionManager(nil, workDir) // executor set later when available

	for i, srv := range servers {
		if !srv.Enabled {
			fmt.Printf("[MCP] ⏭️ Server '%s' 已禁用，跳过\n", srv.Name)
			continue
		}
		if srv.Command == "" || srv.Name == "" {
			fmt.Printf("[MCP] ⚠️ Server #%d 缺少 name 或 command，跳过\n", i+1)
			continue
		}

		if err := a.mcpManager.AddServer(srv); err != nil {
			a.addLog(fmt.Sprintf("【MCP】启动 Server '%s' 失败: %v", srv.Name, err))
			fmt.Printf("[MCP] ❌ 启动失败 '%s': %v\n", srv.Name, err)
		} else {
			a.addLog(fmt.Sprintf("【MCP】✅ Server '%s' 已连接 (%d tools)", srv.Name, 0))
		}
	}

	totalTools := len(a.mcpManager.GetAllTools())
	fmt.Printf("[MCP] ✅ 初始化完成: %d servers, %d total tools\n",
		len(a.mcpManager.ListServers()), totalTools)

	// 注入到 chatManager（SE 工具桥接）
	if a.chatManager != nil {
		a.chatManager.SetMCPManager(a.mcpManager)
		// [v0.7.2] 注入上下文管理三个组件
		a.chatManager.SetContextManagement(a.contextWindow, a.contextBuilder, a.compressor)
	}
}

// GetMCPManager 获取 MCP Manager（供 HTTP API 和 SE 工具桥接使用）
func (a *App) GetMCPManager() *mcp.Manager {
	return a.mcpManager
}

func (a *App) isDangerousWorkDir(dir string) bool {
	dangerousPatterns := []string{
		"\\ArgusTek\\",
		"\\src\\",
		"\\internal\\",
		"\\frontend\\",
	}

	for _, pattern := range dangerousPatterns {
		if strings.Contains(dir, pattern) {
			return true
		}
	}

	currentExe, _ := os.Executable()
	if currentExe != "" {
		exeDir := filepath.Dir(currentExe)
		absDir, _ := filepath.Abs(dir)
		absExeDir, _ := filepath.Abs(exeDir)

		if strings.HasPrefix(absDir, absExeDir+string(os.PathSeparator)) ||
			absDir == absExeDir {
			return true
		}
	}

	return false
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ==================== 终端相关 ====================

func (a *App) RunCommandWithOutput(command string, args []string, workingDir string) (string, error) {
	a.addLog(fmt.Sprintf("执行命令: %s %v", command, args))

	if workingDir == "" {
		workingDir = a.getProjectDir()
	}

	cmd := exec.Command(command, args...)
	cmd.Dir = workingDir
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow: true,
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	// 合并 stdout 和 stderr
	output := stdout.String()
	if stderr.Len() > 0 {
		if output != "" {
			output += "\n"
		}
		output += stderr.String()
	}

	a.addLog(fmt.Sprintf("命令输出长度: %d", len(output)))
	return output, err
}

func (a *App) RunCommand(command string, args []string, workingDir string) (string, error) {
	return a.RunCommandWithOutput(command, args, workingDir)
}

func (a *App) RunCommandAsync(command string, args []string, workingDir string) error {
	if workingDir == "" {
		workingDir = a.getProjectDir()
	}

	psArgs := append([]string{"-NoProfile", "-NonInteractive", "-Command"},
		fmt.Sprintf("cd '%s'; %s %s", workingDir, command, strings.Join(args, " ")))

	cmd := exec.Command("powershell", psArgs...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow: false,
	}

	return cmd.Start()
}

func (a *App) OpenPowerShell(workingDir string) error {
	if workingDir == "" {
		workingDir = a.getProjectDir()
	}

	cmd := exec.Command("powershell", "-NoExit", "-Command", fmt.Sprintf("cd '%s'", workingDir))
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow: false,
	}

	return cmd.Start()
}

func (a *App) StartTerminal(workingDir string) error {
	if a.terminalManager == nil {
		a.terminalManager = NewTerminalManager(a)
	}

	return a.terminalManager.StartTerminal(workingDir)
}

func (a *App) WriteToTerminal(input string) error {
	if a.terminalManager == nil {
		return fmt.Errorf("终端未启动")
	}
	return a.terminalManager.WriteToTerminal(input)
}

func (a *App) StopTerminal() error {
	if a.terminalManager == nil {
		return nil
	}
	return a.terminalManager.StopTerminal()
}

func (a *App) IsTerminalRunning() bool {
	if a.terminalManager == nil {
		return false
	}
	running, _ := a.terminalManager.IsTerminalRunning()
	return running
}

func (a *App) NewTerminalSession(name string) (string, error) {
	if a.terminalManager == nil {
		a.terminalManager = NewTerminalManager(a)
	}
	err := a.terminalManager.NewSession(name, "")
	if err != nil {
		return "", err
	}
	return a.terminalManager.GetActiveSessionID(), nil
}

func (a *App) SwitchTerminalSession(sessionID string) error {
	if a.terminalManager == nil {
		return fmt.Errorf("终端未启动")
	}
	return a.terminalManager.SwitchSession(sessionID)
}

func (a *App) CloseTerminalSession(sessionID string) error {
	if a.terminalManager == nil {
		return nil
	}
	return a.terminalManager.CloseSession(sessionID)
}

func (a *App) SetTerminalEncoding(enc string) error {
	if a.terminalManager == nil {
		return fmt.Errorf("终端未启动")
	}
	return a.terminalManager.SetTerminalEncoding(enc)
}

// [v0.7.1] TerminalTabComplete Tab 补全（前端调用）
func (a *App) TerminalTabComplete(input string) ([]string, error) {
	exe := a.chatManager.GetExecutor()
	ss, err := exe.GetShellSession()
	if err != nil {
		return nil, err
	}
	return ss.TabComplete(input), nil
}

func (a *App) emitTerminalOutput(output string) {
	a.emitToFrontend("terminal:output", output, "Terminal", chat.PathSEExec)
}

// ========== [v0.7.2] Panel Binding Methods (Message Bus / Direct Call) ==========

// --- Token Monitor ---

func (a *App) TokenStats() (map[string]interface{}, error) {
	if a.contextWindow == nil {
		return nil, fmt.Errorf("context window not initialized")
	}
	return a.contextWindow.TokenStats(), nil
}

func (a *App) TokenManage() (map[string]interface{}, error) {
	if a.contextWindow == nil {
		return nil, fmt.Errorf("context window not initialized")
	}
	actionTaken, detail := a.contextWindow.ManageIfNeeded()
	return map[string]interface{}{"action_taken": actionTaken, "detail": detail}, nil
}

func (a *App) TokenClear() error {
	if a.contextWindow == nil {
		return fmt.Errorf("context window not initialized")
	}
	a.contextWindow.Clear()
	return nil
}

func (a *App) TokenCount(text string) (map[string]interface{}, error) {
	if text == "" {
		return nil, fmt.Errorf("text parameter required")
	}
	counter := memory.NewTokenCounter()
	count := counter.CountTokens(text)
	return map[string]interface{}{
		"text": text, "char_count": len(text),
		"rune_count": len([]rune(text)), "token_count": count,
	}, nil
}

func (a *App) TokenPrune(maxTokens int) (map[string]interface{}, error) {
	if a.contextWindow == nil {
		return nil, fmt.Errorf("context window not initialized")
	}
	if maxTokens <= 0 {
		maxTokens = 100000
	}
	pruned := a.contextWindow.PruneToLimit(maxTokens)
	return map[string]interface{}{"pruned": pruned}, nil
}

// --- MCP Panel ---

func (a *App) MCPServers() (map[string]interface{}, error) {
	if a.mcpManager == nil {
		return map[string]interface{}{"servers": []interface{}{}, "total": 0}, nil
	}
	servers := a.mcpManager.ListServers()
	return map[string]interface{}{"servers": servers, "total": len(servers)}, nil
}

func (a *App) MCPAddServer(name, command string, args []string, env map[string]string) error {
	if a.mcpManager == nil {
		return fmt.Errorf("MCP Manager 未初始化")
	}
	if name == "" || command == "" {
		return fmt.Errorf("name 和 command 必填")
	}
	cfg := types.MCPServerConfig{Name: name, Command: command, Args: args, Env: env, Enabled: true}
	if err := a.mcpManager.AddServer(cfg); err != nil {
		return err
	}
	a.addLog(fmt.Sprintf("【MCP】动态添加 Server '%s'", name))
	return nil
}

func (a *App) MCPRemoveServer(name string) error {
	if a.mcpManager == nil {
		return fmt.Errorf("MCP Manager 未初始化")
	}
	if err := a.mcpManager.RemoveServer(name); err != nil {
		return err
	}
	a.addLog(fmt.Sprintf("【MCP】移除 Server '%s'", name))
	return nil
}

func (a *App) MCPTools() ([]map[string]interface{}, error) {
	if a.mcpManager == nil {
		return []map[string]interface{}{}, nil
	}
	raw := a.mcpManager.GetAllTools()
	result := make([]map[string]interface{}, len(raw))
	for i, t := range raw {
		result[i] = map[string]interface{}{
			"name": t.Name, "description": t.Description,
			"server_name": t.ServerName, "input_schema": t.InputSchema,
		}
	}
	return result, nil
}

func (a *App) MCPCallTool(toolName string, arguments map[string]interface{}) (interface{}, error) {
	if a.mcpManager == nil {
		return nil, fmt.Errorf("MCP Manager 未初始化")
	}
	// 遍历所有服务器查找工具
	servers := a.mcpManager.ListServers()
	for _, s := range servers {
		if !s.Initialized {
			continue
		}
		tools, err := a.mcpManager.RefreshTools(s.Name)
		if err != nil {
			continue
		}
		for _, t := range tools {
			if t.Name == toolName {
				result, err := a.mcpManager.CallTool(s.Name, toolName, arguments)
				if err != nil {
					return nil, err
				}
				return result.Content, nil
			}
		}
	}
	return nil, fmt.Errorf("tool '%s' not found", toolName)
}

// --- Debugger Panel ---

func (a *App) DebugStart(program, mode string, args []string, stopOnEntry bool) (interface{}, error) {
	if mode == "" {
		mode = "test"
	}
	session, err := a.debuggerMgr.StartDebug(program, mode, args, stopOnEntry)
	if err != nil {
		return nil, err
	}
	return session, nil
}

func (a *App) DebugStop(sessionID string) error {
	if sessionID == "" {
		a.debuggerMgr.StopAll()
		return nil
	}
	return a.debuggerMgr.StopDebug(sessionID)
}

func (a *App) DebugSessions() (map[string]interface{}, error) {
	sessions := a.debuggerMgr.GetAllSessions()
	return map[string]interface{}{"sessions": sessions, "count": len(sessions)}, nil
}

func (a *App) DebugStatus(sessionID string) (interface{}, error) {
	if sessionID == "" {
		sessions := a.debuggerMgr.GetAllSessions()
		if len(sessions) == 0 {
			return map[string]bool{"running": false}, nil
		}
		sessionID = sessions[len(sessions)-1].ID
	}
	session, err := a.debuggerMgr.GetSession(sessionID)
	if err != nil {
		return nil, err
	}
	return session.Client.CurrentState(), nil
}

func (a *App) DebugSetBreakpoint(sessionID, file string, line int, condition string) (interface{}, error) {
	bp, err := a.debuggerMgr.SetBreakpoint(sessionOrDefault(sessionID, a), file, line, condition)
	if err != nil {
		return nil, err
	}
	return bp, nil
}

func (a *App) DebugRemoveBreakpoint(sessionID, file string, line int) error {
	return a.debuggerMgr.RemoveBreakpoint(sessionOrDefault(sessionID, a), file, line)
}

func (a *App) DebugBreakpoints(sessionID string) (interface{}, error) {
	bps, err := a.debuggerMgr.GetBreakpoints(sessionOrDefault(sessionID, a))
	if err != nil {
		return nil, err
	}
	return bps, nil
}

func (a *App) DebugContinue(sessionID string) error {
	return a.debuggerMgr.Continue(sessionOrDefault(sessionID, a))
}

func (a *App) DebugStepOver(sessionID string) error {
	return a.debuggerMgr.Next(sessionOrDefault(sessionID, a))
}

func (a *App) DebugStepInto(sessionID string) error {
	return a.debuggerMgr.StepIn(sessionOrDefault(sessionID, a))
}

func (a *App) DebugStepOut(sessionID string) error {
	return a.debuggerMgr.StepOut(sessionOrDefault(sessionID, a))
}

func (a *App) DebugPause(sessionID string) error {
	return a.debuggerMgr.Pause(sessionOrDefault(sessionID, a))
}

func (a *App) DebugStacktrace(sessionID string, depth int) (interface{}, error) {
	if depth <= 0 {
		depth = 20
	}
	frames, err := a.debuggerMgr.GetCallStack(sessionOrDefault(sessionID, a))
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"frames": frames, "count": len(frames)}, nil
}

func (a *App) DebugVariables(sessionID string, scope string) (interface{}, error) {
	vars, err := a.debuggerMgr.GetVariables(sessionOrDefault(sessionID, a))
	if err != nil {
		return nil, err
	}
	return vars, nil
}

func (a *App) DebugEvaluate(sessionID, expression string) (interface{}, error) {
	v, err := a.debuggerMgr.EvaluateExpression(sessionOrDefault(sessionID, a), expression)
	if err != nil {
		return nil, err
	}
	return v, nil
}
