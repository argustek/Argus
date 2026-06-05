package types

import (
	"strings"
	"time"
)

// Board 任务状态看板
type Board struct {
	TaskID      string    `json:"task_id"`     // 任务ID
	Description string    `json:"description"` // 任务描述
	StatusCode  int       `json:"status_code"` // 0=idle, 1=running, 2=completed, 3=error
	Status      string    `json:"status"`      // 状态描述
	Result      string    `json:"result"`      // 执行结果或错误信息
	AssignedTo  string    `json:"assigned_to"` // 分配给谁（se）
	UpdatedAt   int64     `json:"updated_at"`  // 更新时间戳
	LastChange  time.Time `json:"last_change"` // 最后变更时间
	// 新增字段（board.go使用）
	CurrentTask string `json:"current_task"` // 当前任务
	CurrentStep int    `json:"current_step"` // 当前步骤
	TotalSteps  int    `json:"total_steps"`  // 总步骤
}

// 状态码常量 (Board用)
const (
	StatusIdle      = 0 // 空闲
	StatusRunning   = 1 // SE运行中
	StatusCompleted = 2 // 已完成
	StatusError     = 3 // 出错
)

// State 项目状态（C监控用，按txt文档）
type State struct {
	ProjectState        int    `json:"project_state"`         // 0=无项目, 1=进行中, 2=完成, 4=出错
	PmStatus            string `json:"pm_status"`             // busy/idle
	SeStatus            string `json:"se_status"`             // busy/idle
	LastChange          int64  `json:"last_change"`           // 最后变更时间戳
	LastUserMessage     string `json:"last_user_message"`     // 最后一条有效用户消息（用于智能恢复）
	LastInteractionTime int64  `json:"last_interaction_time"` // 最后交互时间戳（Unix时间，用于时间感知+社交）
}

// 项目状态常量
const (
	ProjectStateIdle     = 0 // 无项目
	ProjectStateRunning  = 1 // 项目进行中
	ProjectStateDone     = 2 // PM审核通过，等待AP审批
	ProjectStateApproved = 3 // AP最终批准，项目正式完成
	ProjectStateError    = 4 // 出错/需用户介入
)

// 角色状态常量
const (
	RoleStatusBusy = "busy"
	RoleStatusIdle = "idle"
)

// AP 审批状态常量
const (
	APStatusIdle      = "ap_idle"      // AP空闲，未开始审核
	APStatusReviewing = "ap_reviewing" // AP正在审核
	APStatusApproved  = "ap_approved"  // AP审批通过
	APStatusRejected  = "ap_rejected"  // AP审批拒绝
)

// Message 聊天消息
type Message struct {
	ID         string    `json:"id"`
	Role       string    `json:"role"` // user, pm, se
	Content    string    `json:"content"`
	Timestamp  time.Time `json:"timestamp"`
	ReplyTo    string    `json:"reply_to,omitempty"` // 引用消息ID
	RichTaskID string    `json:"_richTaskId,omitempty"` // 三层模型任务ID（用于前端渲染RichMessage）
}

// GlobalTask 全局任务（用于底部任务栏追踪）
type GlobalTask struct {
	ID              string                 `json:"id"`
	Description     string                 `json:"description"` // 具体任务描述（如"创建 hello.go"、"执行 go run"）
	Role            string                 `json:"role"`         // PM | SE | AP | USR
	Status          string                 `json:"status"`       // pending | doing | done | failed
	Progress        string                 `json:"progress,omitempty"`       // 进度文本（如"3/5"）
	ProgressPercent int                    `json:"progressPercent,omitempty"` // 百分比 0-100
	MessageID       string                 `json:"messageId,omitempty"`      // 关联消息ID（用于跳转）
	ParentID        string                 `json:"parentId,omitempty"`       // 父任务ID
	CreatedAt       time.Time              `json:"createdAt"`
	UpdatedAt       time.Time              `json:"updatedAt"`
	CompletedAt     *time.Time             `json:"completedAt,omitempty"`
	Metadata        map[string]interface{} `json:"metadata,omitempty"`
}

// Task 任务
type Task struct {
	ID           string   `json:"id"`
	Title        string   `json:"title"`
	Description  string   `json:"description"`
	Status       string   `json:"status"` // pending, in_progress, done, error
	Dependencies []string `json:"dependencies"`
}

// ExecRequest 执行请求
type ExecRequest struct {
	Command string `json:"command"`
	WorkDir string `json:"work_dir,omitempty"`
}

// ExecResponse 执行响应
type ExecResponse struct {
	Success bool   `json:"success"`
	Output  string `json:"output"`
	Error   string `json:"error,omitempty"`
}

// WriteFileRequest 写文件请求
type WriteFileRequest struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// LogEntry 日志条目
type LogEntry struct {
	Time    time.Time `json:"time"`
	Type    string    `json:"type"`    // exec, write_file, install
	Command string    `json:"command"` // 执行的命令或操作
	Caller  string    `json:"caller"`  // 调用者：PM, SE
	Status  string    `json:"status"`  // start, success, failed, rejected
	Output  string    `json:"output,omitempty"`
	Error   string    `json:"error,omitempty"`
	Reason  string    `json:"reason,omitempty"` // rate_limited, circuit_breaker
}

// RateLimitConfig 限流配置
type RateLimitConfig struct {
	Exec         RateLimit `yaml:"exec"`
	Install      RateLimit `yaml:"install"`
	WriteFile    RateLimit `yaml:"write_file"`
	ReadFile     RateLimit `yaml:"read_file"`
	ExecCommand  RateLimit `yaml:"exec_command"`
	GitOperation RateLimit `yaml:"git_operation"`
}

// RateLimit 限流规则
type RateLimit struct {
	MaxPerSecond int `yaml:"max_per_second,omitempty"`
	MaxPerMinute int `yaml:"max_per_minute,omitempty"`
	MaxPerHour   int `yaml:"max_per_hour,omitempty"`
}

// CircuitBreakerConfig 熔断配置
type CircuitBreakerConfig struct {
	Exec    CircuitBreaker `yaml:"exec"`
	Install CircuitBreaker `yaml:"install"`
}

// CircuitBreaker 熔断规则
type CircuitBreaker struct {
	FailureThreshold int `yaml:"failure_threshold"`
	TimeoutSeconds   int `yaml:"timeout_seconds"`
}

// Config 主配置
type Config struct {
	WorkDir          string    `yaml:"work_dir"`
	CheckInterval    int       `yaml:"check_interval"`    // C检查看板间隔（秒）
	CommitInterval   int       `yaml:"commit_interval"`   // 自动commit间隔（分钟）
	HeartbeatTimeout int       `yaml:"heartbeat_timeout"` // 心跳超时（秒）
	APIConfig        APIConfig `yaml:"api_config"`
	PmDecisionAlert  bool      `yaml:"pm_decision_alert"` // PM决策提醒（弹窗）
}

// APIConfig API配置
type APIConfig struct {
	Provider string `yaml:"provider"`
	BaseURL  string `yaml:"base_url"`
	APIKey   string `yaml:"api_key"`
	Model    string `yaml:"model"`
}

// TaskMemory 任务记忆（持久化用）
type TaskMemory struct {
	TaskID          string    `json:"task_id"`          // 任务ID（第一条用户消息的ID）
	UserRequest     string    `json:"user_request"`     // 用户原始请求
	CurrentState    string    `json:"current_state"`    // idle/working/waiting/done
	CurrentRole     string    `json:"current_role"`     // 当前活跃角色 (user/pm/se)
	TaskDescription string    `json:"task_description"` // PM解析的任务描述
	RecentMessages  []Message `json:"recent_messages"`  // 最近20条消息（恢复上下文）
	LastActiveTime  time.Time `json:"last_active_time"` // 最后活跃时间
	MessageCount    int       `json:"message_count"`    // 总消息数
}

// ========== 决策配置系统（全可配置）==========

// DecisionType 操作类型枚举
type DecisionType string

const (
	DecisionFileRead     DecisionType = "file_read"     // 文件读取
	DecisionFileWrite    DecisionType = "file_write"    // 文件写入（创建新文件）
	DecisionFileModify   DecisionType = "file_modify"   // 文件修改（编辑现有文件）
	DecisionFileDelete   DecisionType = "file_delete"   // 文件删除
	DecisionCmdExecute   DecisionType = "cmd_execute"   // 命令执行
	DecisionGitOperate   DecisionType = "git_operate"   // Git操作（commit/push等）
	DecisionTaskStop     DecisionType = "task_stop"     // 任务终止
	DecisionConfigChange DecisionType = "config_change" // 配置修改
	DecisionSensitiveOp  DecisionType = "sensitive_op"  // 敏感操作
)

// DecisionMode 决策模式
type DecisionMode string

const (
	DecisionAuto   DecisionMode = "auto"   // AI自动决策
	DecisionManual DecisionMode = "manual" // 需要人工确认
)

// DecisionRule 单个操作的决策规则
type DecisionRule struct {
	Type        DecisionType `json:"type"`         // 操作类型
	Mode        DecisionMode `json:"mode"`         // 当前设置：auto/manual
	DefaultMode DecisionMode `json:"default_mode"` // 缺省值（建议）
	Description string       `json:"description"`  // 操作说明
	Category    string       `json:"category"`     // 分类：file/cmd/git/system
}

// DecisionConfig 完整的决策配置
type DecisionConfig struct {
	Version   int            `json:"version"`    // 配置版本
	Rules     []DecisionRule `json:"rules"`      // 所有规则列表
	UpdatedAt time.Time      `json:"updated_at"` // 最后更新时间
}

// GetDefaultDecisionConfig 获取缺省配置（我的建议）
func GetDefaultDecisionConfig() DecisionConfig {
	return DecisionConfig{
		Version:   1,
		UpdatedAt: time.Now(),
		Rules: []DecisionRule{
			{Type: DecisionFileRead, Mode: DecisionAuto, DefaultMode: DecisionAuto, Description: "读取文件内容", Category: "file"},
			{Type: DecisionFileWrite, Mode: DecisionAuto, DefaultMode: DecisionAuto, Description: "创建新文件", Category: "file"},
			{Type: DecisionFileModify, Mode: DecisionAuto, DefaultMode: DecisionAuto, Description: "编辑现有文件", Category: "file"},
			{Type: DecisionFileDelete, Mode: DecisionAuto, DefaultMode: DecisionAuto, Description: "删除文件", Category: "file"},
			{Type: DecisionCmdExecute, Mode: DecisionAuto, DefaultMode: DecisionAuto, Description: "执行命令（编译/测试等）", Category: "cmd"},
			{Type: DecisionGitOperate, Mode: DecisionAuto, DefaultMode: DecisionAuto, Description: "Git操作（commit/push等）", Category: "git"},
			{Type: DecisionTaskStop, Mode: DecisionAuto, DefaultMode: DecisionAuto, Description: "终止当前任务", Category: "system"},
			{Type: DecisionConfigChange, Mode: DecisionAuto, DefaultMode: DecisionAuto, Description: "修改系统配置", Category: "system"},
			{Type: DecisionSensitiveOp, Mode: DecisionAuto, DefaultMode: DecisionAuto, Description: "敏感操作（涉及安全）", Category: "system"},
		},
	}
}

// GetDecisionMode 获取指定操作类型的决策模式
func (dc *DecisionConfig) GetDecisionMode(decisionType DecisionType) DecisionMode {
	for _, rule := range dc.Rules {
		if rule.Type == decisionType {
			return rule.Mode
		}
	}
	return DecisionManual // 未找到则默认需要人工确认
}

// ========== 权限配置系统（工作目录权限）==========

// PermissionLevel 权限级别
type PermissionLevel string

const (
	PermFullAccess PermissionLevel = "full"      // 全权限（读写删改执行）
	PermReadWrite  PermissionLevel = "readwrite" // 读写改（不能删除）
	PermReadOnly   PermissionLevel = "readonly"  // 只读
	PermNoAccess   PermissionLevel = "none"      // 禁止访问
	PermProtected  PermissionLevel = "protected" // 受保护（禁止AI直接操作）
)

// PathRule 单个路径/文件的权限规则
type PathRule struct {
	PathPattern string          `json:"path_pattern"` // 路径模式（支持通配符 *.go）
	Permission  PermissionLevel `json:"permission"`   // 权限级别
	Description string          `json:"description"`  // 规则说明
	IsDirectory bool            `json:"is_directory"` // 是否是目录规则
	Priority    int             `json:"priority"`     // 优先级（数字越小优先级越高）
}

// PermissionConfig 完整的权限配置
type PermissionConfig struct {
	Version           int             `json:"version"`            // 配置版本
	Rules             []PathRule      `json:"rules"`              // 所有路径规则
	DefaultPermission PermissionLevel `json:"default_permission"` // 默认权限（未匹配时）
	UpdatedAt         time.Time       `json:"updated_at"`         // 最后更新时间
}

// GetDefaultPermissionConfig 获取缺省配置（工作目录全开放，.git 只限制删除）
func GetDefaultPermissionConfig(workDir string) PermissionConfig {
	return PermissionConfig{
		Version:           1,
		DefaultPermission: PermFullAccess, // 默认：工作目录全开放！
		UpdatedAt:         time.Now(),
		Rules: []PathRule{
			{PathPattern: ".env*", Permission: PermReadOnly, Description: "环境变量文件（AI不可修改）", Priority: 1},
			{PathPattern: "**/credentials*", Permission: PermReadOnly, Description: "凭证文件（AI不可修改）", Priority: 2},
			{PathPattern: "**/secret*", Permission: PermReadOnly, Description: "密钥文件（AI不可修改）", Priority: 2},
			{PathPattern: ".git/**", Permission: PermReadWrite, Description: "Git版本控制目录（可读写，不能删除）", IsDirectory: true, Priority: 3},
			{PathPattern: ".argus/**", Permission: PermProtected, Description: "Argus系统目录（AI不可操作）", IsDirectory: true, Priority: 1},
			{PathPattern: "C:/Windows/**", Permission: PermReadOnly, Description: "Windows系统目录（只读）", IsDirectory: true, Priority: 1},
			{PathPattern: "C:\\Windows\\**", Permission: PermReadOnly, Description: "Windows系统目录（只读）", IsDirectory: true, Priority: 1},
			{PathPattern: "C:/Program Files/**", Permission: PermReadOnly, Description: "Program Files（只读）", IsDirectory: true, Priority: 1},
			{PathPattern: "C:\\Program Files\\**", Permission: PermReadOnly, Description: "Program Files（只读）", IsDirectory: true, Priority: 1},
			{PathPattern: "C:/Program Files (x86)/**", Permission: PermReadOnly, Description: "Program Files (x86)（只读）", IsDirectory: true, Priority: 1},
			{PathPattern: "C:\\Program Files (x86)\\**", Permission: PermReadOnly, Description: "Program Files (x86)（只读）", IsDirectory: true, Priority: 1},
			{PathPattern: "C:/Users/*/AppData/**", Permission: PermReadOnly, Description: "AppData（只读）", IsDirectory: true, Priority: 1},
			{PathPattern: "C:\\Users\\*\\AppData\\**", Permission: PermReadOnly, Description: "AppData（只读）", IsDirectory: true, Priority: 1},
		},
	}
}

// ========== 命令黑名单系统（exec 安全拦截）==========

type CommandBlockLevel string

const (
	CmdBlockDeny  CommandBlockLevel = "deny"  // 绝对拒绝，不可覆盖
	CmdBlockAsk   CommandBlockLevel = "ask"   // 需要用户确认
	CmdBlockAllow CommandBlockLevel = "allow" // 允许执行
)

type CommandRule struct {
	Pattern     string            `json:"pattern"`     // 命令匹配模式（支持通配符）
	Level       CommandBlockLevel `json:"level"`       // 拦截级别
	Description string            `json:"description"` // 规则说明
}

type CommandPolicy struct {
	Version   int           `json:"version"`
	Rules     []CommandRule `json:"rules"`
	UpdatedAt time.Time     `json:"updated_at"`
}

func GetDefaultCommandPolicy() CommandPolicy {
	return CommandPolicy{
		Version:   1,
		UpdatedAt: time.Now(),
		Rules: []CommandRule{
			{Pattern: "format *", Level: CmdBlockDeny, Description: "格式化磁盘"},
			{Pattern: "cipher /w:*", Level: CmdBlockDeny, Description: "覆写磁盘数据"},
			{Pattern: "diskpart *", Level: CmdBlockDeny, Description: "磁盘分区操作"},
			{Pattern: "bcdedit *", Level: CmdBlockDeny, Description: "修改启动配置"},
			{Pattern: "net user *", Level: CmdBlockDeny, Description: "用户账户操作"},
			{Pattern: "reg delete HKCR *", Level: CmdBlockDeny, Description: "删除文件关联注册表"},
			{Pattern: "reg delete HKLM *", Level: CmdBlockDeny, Description: "删除系统核心注册表"},
		},
	}
}

func (cp *CommandPolicy) CheckCommand(command string) (CommandBlockLevel, string) {
	cmdLower := strings.ToLower(strings.TrimSpace(command))
	for _, rule := range cp.Rules {
		if matchCommandPattern(strings.ToLower(rule.Pattern), cmdLower) {
			return rule.Level, rule.Description
		}
	}
	return CmdBlockAllow, ""
}

func matchCommandPattern(pattern, command string) bool {
	if pattern == command {
		return true
	}
	if strings.Contains(pattern, "*") {
		parts := strings.SplitN(pattern, "*", 2)
		prefix := parts[0]
		suffix := ""
		if len(parts) > 1 {
			suffix = parts[1]
		}
		if prefix != "" && !strings.HasPrefix(command, prefix) {
			return false
		}
		if suffix != "" && !strings.HasSuffix(command, suffix) {
			return false
		}
		return true
	}
	if strings.HasPrefix(command, pattern+" ") {
		return true
	}
	if strings.HasPrefix(command, pattern) {
		return true
	}
	return false
}

// ========== 内部环境记忆系统（EnvMemory）==========

// ToolInfo 工具软件信息
type ToolInfo struct {
	Path      string    `json:"path"`       // 完整路径
	FirstSeen time.Time `json:"first_seen"` // 首次发现时间
	LastUsed  time.Time `json:"last_used"`  // 最后使用时间
	UseCount  int       `json:"use_count"`  // 使用次数
	Source    string    `json:"source"`     // 来源: "learned"=AI自动发现, "user"=用户告知
}

// EnvConfig 开发配置项（非路径类）
type EnvConfig struct {
	Value     string    `json:"value"`
	UpdatedAt time.Time `json:"updated_at"`
	Source    string    `json:"source"`
}

// EnvMemory 内部环境变量（AI 的持久化环境感知）
type EnvMemory struct {
	Version   int                  `json:"version"`
	Tools     map[string]ToolInfo  `json:"tools"`   // 工具名→工具信息 (node, git, python...)
	Configs   map[string]EnvConfig `json:"configs"` // 配置项 (npm_registry, go_proxy...)
	System    SystemInfo           `json:"system"`  // 系统信息摘要
	UpdatedAt time.Time            `json:"updated_at"`
}

// SystemInfo 系统信息
type SystemInfo struct {
	OS      string `json:"os"`
	CPU     string `json:"cpu"`
	RAM     string `json:"ram"`
	GoVer   string `json:"go_version"`
	NodeVer string `json:"node_version"`
}

// GetDefaultEnvMemory 获取默认空的环境记忆
func GetDefaultEnvMemory() *EnvMemory {
	return &EnvMemory{
		Version: 1,
		Tools:   make(map[string]ToolInfo),
		Configs: make(map[string]EnvConfig),
		System:  SystemInfo{},
	}
}
