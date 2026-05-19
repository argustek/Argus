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
	"syscall"
	"time"
	"unsafe"

	"gopkg.in/yaml.v3"

	"argus/internal/chat"
	"argus/internal/dingtalk"
	"argus/internal/git"
	"argus/internal/i18n"
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
	APIConfigs      []APIConfig    `json:"apiConfigs"`
	IMConfigs       []IMConfig     `json:"imConfigs"`
	ShowCodeBlocks  bool           `json:"showCodeBlocks"`
	ShowThinking    bool           `json:"showThinking"`
	PmDecisionAlert bool           `json:"pmDecisionAlert"`
	WorkDir         string         `json:"workDir"`
	RecentProjects  []string       `json:"recentProjects"`
	DingTalk        DingTalkConfig `json:"dingtalk,omitempty"`
	HTTP            HTTPConfig     `json:"http,omitempty"`
	APEnabled       bool           `json:"apEnabled"`
	APConfig        *APIConfig     `json:"apConfig,omitempty"`
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

	// C 守护进程相关
	cRunning  bool
	cStopChan chan bool

	// 初始化同步（确保 --send 等待 ChatManager 就绪）
	readyChan chan struct{}

	// CLI 模式标志（使用当前工作目录而非 exe 目录）
	useCWD bool

	// 改动历史
	changeHistory []ChangeRecord

	// Chat Manager（新的对话管理器）
	chatManager *chat.Manager

	// HTTP 服务器（支持优雅停止）
	httpServer *http.Server

	// 消息去重（防止前端重复显示）
	msgIDCounter  int64
	emittedMsgIDs map[int64]bool
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
	a.addLog("【ChatManager】初始化...")

	// 获取项目目录（避免监控Argus自己）
	projectDir := a.getProjectDir()
	a.addLog(fmt.Sprintf("【ChatManager】项目目录: %s", projectDir))

	// 构造配置
	config := types.Config{
		WorkDir:         projectDir,
		CommitInterval:  5,
		APIConfig:       types.APIConfig{},
		PmDecisionAlert: a.config.PmDecisionAlert,
	}

	// 从 app config 转换 API 配置（优先使用默认配置）
	var selectedConfig *APIConfig
	for i := range a.config.APIConfigs {
		if a.config.APIConfigs[i].IsDefault {
			selectedConfig = &a.config.APIConfigs[i]
			break
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

	chatManager, err := chat.NewManager(config, projectDir)
	if err != nil {
		a.addLog(fmt.Sprintf("【ChatManager】初始化失败: %v", err))
		return
	}

	a.chatManager = chatManager
	a.chatManager.SetDingTalkEnabled(a.isDingTalkEnabled())
	// 设置Wails context（供C监控弹框使用）
	if a.ctx != nil {
		a.chatManager.SetContext(a.ctx)
	}
	// 初始化AP配置
	if a.config.APEnabled {
		if a.config.APConfig != nil && a.config.APConfig.BaseURL != "" && a.config.APConfig.APIKey != "" {
			// AP使用独立API
			a.chatManager.UpdateAPConfig(types.APIConfig{
				Provider: a.config.APConfig.Provider,
				BaseURL:  a.config.APConfig.BaseURL,
				APIKey:   a.config.APConfig.APIKey,
				Model:    a.config.APConfig.ModelName,
			})
			a.addLog(fmt.Sprintf("【ChatManager】AP已启用，使用独立API: %s", a.config.APConfig.BaseURL))
		} else {
			a.chatManager.UpdateAPConfig(types.APIConfig{})
			a.addLog("【ChatManager】AP已启用，使用PM的API配置（共用模式）")
		}
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
		if a.ctx != nil {
			runtime.EventsEmit(a.ctx, "se-file-written", path)
		}
	})
	// 设置项目状态变更回调
	a.chatManager.SetOnProjectStateChanged(func(state string) {
		a.addLog(fmt.Sprintf("[OnProjectStateChanged] 状态变更: %s", state))
		a.status.ProjectState = state
		runtime.EventsEmit(a.ctx, "project-state-changed", state)
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
			if msg.Source != "pm_to_user" && msg.Source != "pm_to_se" {
				runtime.EventsEmit(a.ctx, "new-message", chatMsg)
				a.writeDebugLog(fmt.Sprintf("[OnMsgAdded] EMIT #%d role=%s content=%s", msgID, msg.Role, truncate(msg.Content, 40)))
			} else {
				a.writeDebugLog(fmt.Sprintf("[OnMsgAdded] SILENT #%d role=%s source=%s (流式已显示)", msgID, msg.Role, msg.Source))
			}
		} else {
			a.writeDebugLog(fmt.Sprintf("[OnMsgAdded] SKIP_DUP #%d role=%s", msgID, msg.Role))
		}

		if msg.Role == "se" {
			a.sendToDingTalk(fmt.Sprintf("[SE] %s", msg.Content))
		}
	})

	// 初始化C监控（在SetOnMessageAdded之后，确保消息能正确推送到前端）
	a.chatManager.InitCMonitor()

	// 监听前端语言切换事件
	runtime.EventsOn(a.ctx, "set-reply-language", func(optionalData ...interface{}) {
		if len(optionalData) > 0 {
			if lang, ok := optionalData[0].(string); ok {
				a.chatManager.SetReplyLanguage(lang)
			}
		}
	})

	// ✅ 通知初始化完成（允许 --send 发送消息）
	close(a.readyChan)
	fmt.Println("[initChatManager] ✅ 初始化完成，已关闭 readyChan")

	a.addLog("【ChatManager】初始化完成")

	// 启动C守护进程（此时chatManager已初始化，startCGuardian会跳过旧cMonitorLoop）
	go a.startCGuardian()
}

// initChatManagerCLI initializes ChatManager without Wails GUI dependencies
func (a *App) initChatManagerCLI() {
	if a.chatManager != nil {
		return
	}

	projectDir := a.getProjectDir()
	fmt.Printf("[CLI] 项目目录: %s\n", projectDir)

	config := types.Config{
		WorkDir:        projectDir,
		CommitInterval: 5,
		APIConfig:      types.APIConfig{},
	}

	var selectedConfig *APIConfig
	for i := range a.config.APIConfigs {
		if a.config.APIConfigs[i].IsDefault {
			selectedConfig = &a.config.APIConfigs[i]
			break
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

	chatManager, err := chat.NewManager(config, projectDir)
	if err != nil {
		fmt.Printf("[CLI] ChatManager初始化失败: %v\n", err)
		return
	}

	a.chatManager = chatManager
	a.chatManager.SetDingTalkEnabled(a.isDingTalkEnabled())
	// 初始化AP配置
	if a.config.APEnabled {
		if a.config.APConfig != nil && a.config.APConfig.BaseURL != "" && a.config.APConfig.APIKey != "" {
			// AP使用独立API
			a.chatManager.UpdateAPConfig(types.APIConfig{
				Provider: a.config.APConfig.Provider,
				BaseURL:  a.config.APConfig.BaseURL,
				APIKey:   a.config.APConfig.APIKey,
				Model:    a.config.APConfig.ModelName,
			})
			fmt.Printf("[CLI] AP已启用，使用独立API: %s\n", a.config.APConfig.BaseURL)
		} else {
			a.chatManager.UpdateAPConfig(types.APIConfig{})
			fmt.Println("[CLI] AP已启用，使用PM的API配置（共用模式）")
		}
	} else {
		fmt.Println("[CLI] AP未启用")
	}
	chatManager.InitCMonitor()

	// ✅ 通知初始化完成
	close(a.readyChan)

	fmt.Println("[CLI] ChatManager初始化完成")
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
	logFile := filepath.Join(a.getConfigDir(), "..", "argus.log")
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

func (a *App) SaveConfig(config Config) error {
	oldDingEnabled := a.isDingTalkEnabled()
	oldHttpEnabled := a.config.HTTP.Enabled

	a.config = config

	// 先用明文 API Key 更新 ChatManager（必须在 saveConfigToFile 加密之前！）
	if a.chatManager != nil {
		selectedConfig := a.getDefaultAPIConfig()
		if selectedConfig != nil {
			a.chatManager.UpdateAPIConfig(types.APIConfig{
				Provider: selectedConfig.Provider,
				BaseURL:  selectedConfig.BaseURL,
				APIKey:   selectedConfig.APIKey,
				Model:    selectedConfig.ModelName,
			})
		}

		// 更新 AP 配置
		if config.APEnabled {
			if config.APConfig != nil && config.APConfig.BaseURL != "" && config.APConfig.APIKey != "" {
				// AP使用独立API
				a.chatManager.UpdateAPConfig(types.APIConfig{
					Provider: config.APConfig.Provider,
					BaseURL:  config.APConfig.BaseURL,
					APIKey:   config.APConfig.APIKey,
					Model:    config.APConfig.ModelName,
				})
				fmt.Printf("[SaveConfig] AP使用独立API: %s\n", config.APConfig.BaseURL)
			} else {
				// AP复用PM的API
				a.chatManager.UpdateAPConfig(types.APIConfig{})
				fmt.Printf("[SaveConfig] AP使用PM的API配置（共用模式）\n")
			}
		} else {
			a.chatManager.UpdateAPConfig(types.APIConfig{})
			fmt.Printf("[SaveConfig] AP未启用\n")
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

// SendCLI sends a message from command line without Wails GUI
func (a *App) SendCLI(message string) {
	a.loadConfig()
	a.initChatManagerCLI()

	if a.chatManager == nil {
		fmt.Println("ChatManager 初始化失败")
		return
	}

	fmt.Printf("发送: %s\n", message)
	response, err := a.chatManager.ProcessMessage(message)
	if err != nil {
		fmt.Printf("错误: %v\n", err)
		return
	}
	fmt.Printf("回复: %s\n", response)
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

	// 尝试多个可能的项目根目录
	possibleRoots := []string{
		// 当前目录（开发时）
		".",
		// 可执行文件所在目录的父目录（dev 模式：build/bin/ -> build/ -> 项目根）
		filepath.Join(filepath.Dir(exePath), "..", ".."),
		// 可执行文件所在目录（生产模式）
		filepath.Dir(exePath),
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

	if !a.config.HTTP.Enabled && a.config.HTTP.Port == 0 {
		a.config.HTTP = HTTPConfig{
			Enabled:     true,
			Port:        8080,
			APIToken:    "",
			AllowRemote: false,
		}
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
		// 没有设置工作目录时，使用旧的全局路径（向后兼容）
		messagesPath := filepath.Join(a.getConfigDir(), "messages.json")
		a.loadMessagesFromPath(messagesPath)
	} else {
		// 使用工作目录下的消息文件
		messagesPath := filepath.Join(workDir, ".argus", "messages.json")
		a.loadMessagesFromPath(messagesPath)
	}
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
	var messagesPath string

	if workDir == "" {
		messagesPath = filepath.Join(a.getConfigDir(), "messages.json")
	} else {
		messagesPath = filepath.Join(workDir, ".argus", "messages.json")
	}

	data, err := json.MarshalIndent(a.messages, "", "  ")
	if err != nil {
		return
	}
	os.MkdirAll(filepath.Dir(messagesPath), 0755)
	os.WriteFile(messagesPath, data, 0644)
}

// ClearMessages 清空聊天记录
func (a *App) ClearMessages() {
	a.messages = make([]ChatMessage, 0)
	a.saveMessages()
	a.addLog("✅ 已清空聊天记录")

	runtime.EventsEmit(a.ctx, "messages-cleared", nil)
}

func (a *App) ResetRoleStatus() {
	a.messages = make([]ChatMessage, 0)
	a.saveMessages()
	runtime.EventsEmit(a.ctx, "messages-cleared", nil)

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
	runtime.EventsEmit(a.ctx, "messages-cleared", nil)
	runtime.EventsEmit(a.ctx, "reset-completed", map[string]string{"reason": reason})
	a.addLog("✅ 已执行复位: " + reason)
	return nil
}

func (a *App) saveConfigToFile() error {
	configPath := filepath.Join(a.getConfigDir(), "config.json")

	encryptAPIKeys(&a.config)

	data, err := json.MarshalIndent(a.config, "", "  ")
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

	// 添加到最近项目
	a.addRecentProject(dir)

	// 更新ChatManager的工作目录
	if a.chatManager != nil {
		a.chatManager.SetWorkDir(dir)
		a.addLog(fmt.Sprintf("【SetWorkDir】更新ChatManager工作目录: %s", dir))
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
		if relPath == "." || relPath == ".argus" || strings.HasPrefix(relPath, ".argus"+string(filepath.Separator)) {
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
		return fmt.Errorf("不支持的操作系统: %s", goruntime.GOOS)
	}

	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd.Run()
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

	err := cmd.Run()
	if err != nil {
		a.addLog(fmt.Sprintf("【资源管理器】打开失败: %v", err))
		return fmt.Errorf("打开资源管理器失败: %v", err)
	}

	a.addLog(fmt.Sprintf("【资源管理器】已成功打开: %s", workDir))
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
		}, fmt.Errorf(errMsg)
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
	if a.chatManager == nil {
		return fmt.Errorf("ChatManager 未初始化")
	}
	a.aiThinking = false
	runtime.EventsEmit(a.ctx, "ai-thinking", false)
	a.chatManager.StopCurrentTask()
	a.chatManager.SetUserStopped(true)
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
	a.writeDebugLog(fmt.Sprintf("[SendMessage] CALLED content=%s", truncate(content, 50)))
	fmt.Printf("[SendMessage] Step 1: 函数开始\n")
	fmt.Printf("[SendMessage] Step 2: 收到消息: %s\n", content)

	if strings.TrimSpace(content) != "" {
		apiCfg := a.getDefaultAPIConfig()
		if apiCfg == nil || strings.TrimSpace(apiCfg.APIKey) == "" {
			errMsg := "⚠️ API Key 未配置，请先在设置中填写 API Key"
			a.addLog(errMsg)
			a.messages = append(a.messages, a.newChatMessage("error", errMsg))
			a.saveMessages()
			if a.ctx != nil {
				lastMsg := a.messages[len(a.messages)-1]
				runtime.EventsEmit(a.ctx, "new-message", lastMsg)
			}
			return fmt.Errorf(errMsg)
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

	// 使用新的 ChatManager 处理消息
	if a.chatManager != nil {
		fmt.Printf("[SendMessage] Step 4: 进入 chatManager 分支\n")
		// 如果是空消息，打断AI工作
		if strings.TrimSpace(content) == "" {
			fmt.Printf("[SendMessage] Step 5: 空消息，停止AI\n")
			a.aiThinking = false
			runtime.EventsEmit(a.ctx, "ai-thinking", false)

			if a.chatManager != nil {
				a.chatManager.SetUserStopped(true)
				fmt.Printf("[SendMessage] Step 6: 已设置 userStopped 标志，清理记忆文件\n")
			}

			return nil
		}

		if !strings.HasPrefix(content, "[钉钉]") {
			a.addLog("【SendMessage】准备发送钉钉消息")
			a.sendToDingTalk(fmt.Sprintf("[USR] %s", content))
		}

		a.addLog("【SendMessage】准备处理消息（同步方式）")
		a.aiThinking = true
		runtime.EventsEmit(a.ctx, "ai-thinking", true)

		fmt.Printf("[SendMessage] 同步调用 ProcessMessage: %s\n", content)
		response, err := a.chatManager.ProcessMessage(content)
		fmt.Printf("[SendMessage] ProcessMessage 返回: err=%v, response_len=%d\n", err, len(response))
		a.addLog(fmt.Sprintf("【SendMessage】ProcessMessage 返回: err=%v, response_len=%d", err, len(response)))

		if err != nil {
			a.addLog(fmt.Sprintf("【SendMessage】处理失败: %v", err))
			errorMsg := a.newChatMessage("error", fmt.Sprintf("错误: %v", err))
			errorMsg.Summary = "System"
			a.messages = append(a.messages, errorMsg)
			a.saveMessages()
			a.writeDebugLog(fmt.Sprintf("[SendMessage] ERROR_EMIT err=%v", err))
			runtime.EventsEmit(a.ctx, "new-message", errorMsg)

			// 发送错误消息到钉钉
			a.sendToDingTalk(fmt.Sprintf("[ERR] %v", err))
		} else {
			// 注意：不在这里添加消息！
			// 消息已通过 chatManager.onMessageAdded callback 添加到 app.messages
			// 这里只负责发送钉钉通知

			// 发送AI回复到钉钉，带着角色
			go func() {
				defer func() {
					if r := recover(); r != nil {
						fmt.Printf("[DingTalk-Notify] 💥 panic recovered: %v\n", r)
					}
				}()
				role := a.chatManager.GetCurrentRole()
				roleLabel := "PM"
				if role == "se" {
					roleLabel = "SE"
				} else if role == "mc" {
					roleLabel = "MC"
				}
				a.sendToDingTalk(fmt.Sprintf("[%s] %s", roleLabel, response))
			}()
		}

		a.aiThinking = false
		runtime.EventsEmit(a.ctx, "ai-thinking", false)

		return nil
	}

	// ChatManager 未初始化，使用旧逻辑
	a.addLog("【SendMessage】ChatManager 未初始化，使用旧逻辑")
	return a.sendMessageLegacy(content)
}

// sendMessageLegacy 旧的 SendMessage 逻辑
func (a *App) sendMessageLegacy(content string) error {
	if a.aiThinking {
		return fmt.Errorf("AI正在思考中，请稍后再试")
	}

	userMsg := a.newChatMessage("user", content)
	a.messages = append(a.messages, userMsg)
	a.saveMessages()

	runtime.EventsEmit(a.ctx, "new-message", userMsg)

	a.aiThinking = true
	if a.chatManager != nil {
		go func() {
			a.chatManager.ProcessMessage(content)
			a.aiThinking = false
		}()
	} else {
		a.aiThinking = false
		a.addErrorMessage("ChatManager未初始化，无法处理消息")
	}

	return nil
}

// 添加PM消息
func (a *App) addPMMessage(content string) {
	aiMsg := a.newChatMessage("pm", content)
	aiMsg.Summary = "PM"
	a.messages = append(a.messages, aiMsg)
	a.saveMessages()

	// 主动推送消息到前端
	runtime.EventsEmit(a.ctx, "new-message", aiMsg)

	// 发送AI回复到钉钉
	a.sendToDingTalk(fmt.Sprintf("[PM] %s", content))
}

// 添加SE消息
func (a *App) addSEMessage(content string) {
	aiMsg := a.newChatMessage("se", content)
	aiMsg.Summary = "SE"
	a.messages = append(a.messages, aiMsg)
	a.saveMessages()

	// 主动推送消息到前端
	runtime.EventsEmit(a.ctx, "new-message", aiMsg)

	// 发送SE回复到钉钉
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
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "new-message", errMsg)
	}
	// 发送错误消息到钉钉
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
	if a.config.WorkDir != "" {
		return a.config.WorkDir
	}

	// CLI 模式：使用当前工作目录
	if a.useCWD {
		cwd, err := os.Getwd()
		if err == nil {
			fmt.Printf("[getProjectDir] CLI模式，使用 cwd: %s\n", cwd)
			return cwd
		}
	}

	exePath, err := os.Executable()
	if err != nil {
		return "."
	}
	exeDir := filepath.Dir(exePath)
	projectDir := filepath.Join(exeDir, "project")

	if err := os.MkdirAll(projectDir, 0755); err != nil {
		return "."
	}

	return projectDir
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

func (a *App) emitTerminalOutput(output string) {
	runtime.EventsEmit(a.ctx, "terminal:output", output)
}
