package debugger

import (
	"encoding/json"
	"sync"
)

// Seq 序列号生成器
var seqMu sync.Mutex
var seqCounter int = 1

func nextSeq() int {
	seqMu.Lock()
	defer seqMu.Unlock()
	seqCounter++
	return seqCounter
}

// ---- DAP 协议消息类型 ----

// BaseRequest 所有请求的基类
type BaseRequest struct {
	Seq  int    `json:"seq"`
	Type string `json:"type"` // "request"
	Command string `json:"command"`
	Arguments interface{} `json:"arguments,omitempty"`
}

// BaseResponse 所有响应的基类
type BaseResponse struct {
	Seq           int             `json:"seq"`
	Type          string          `json:"type"` // "response"
	RequestSeq    int             `json:"request_seq"`
	Success       bool            `json:"success"`
	Command       string          `json:"command"`
	Message       string          `json:"message,omitempty"`
	Body          json.RawMessage `json:"body,omitempty"`
}

// Event DAP 事件
type Event struct {
	Seq     int             `json:"seq"`
	Type    string          `json:"type"` // "event"
	Event   string          `json:"event"`
	Body    json.RawMessage `json:"body,omitempty"`
}

// InitializeRequest 初始化请求
type InitializeRequest struct {
	ClientID                    string `json:"clientID,omitempty"`
	ClientName                  string `json:"clientName"`
	AdapterID                   string `json:"adapterID"`
	PathFormat                  string `json:"pathFormat,omitempty"` // "path" or "uri"
	LinesStartAt1               bool   `json:"linesStartAt1"`
	ColumnsStartAt1             bool   `json:"columnsStartAt1"`
	SupportsVariableType        bool   `json:"supportsVariableType"`
	SupportsVariablePaging      bool   `json:"supportsVariablePaging"`
	SupportsRunInTerminalRequest bool  `json:"supportsRunInTerminalRequest"`
	SupportsMemoryReferences    bool   `json:"supportsMemoryReferences"`
	SupportsProgressReporting   bool   `json:"supportsProgressReporting"`
	SupportsInvalidatedEvent    bool   `json:"supportsInvalidatedEvent"`
	SendsExecutionSummary       bool   `json:"sendsExecutionSummary"`
}

// LaunchArguments 启动参数（Go/delve）
type LaunchArguments struct {
	Mode         string `json:"mode"`                   // "debug" or "test"
	Program      string `json:"program"`                // 可执行文件或测试包
	NoDebug      bool   `json:"noDebug,omitempty"`      // 不加断点直接跑
	StopOnEntry  bool   `json:"stopOnEntry,omitempty"`   // 入口暂停
	Cwd          string `json:"cwd,omitempty"`           // 工作目录
	Args         []string `json:"args,omitempty"`        // 程序参数
	Env          map[string]string `json:"env,omitempty"` // 环境变量
	BuildFlags   string `json:"buildFlags,omitempty"`    // go build flags
	Init         string `json:"init,omitempty"`           // init 命令
	RemotePath   string `json:"remotePath,omitempty"`    // 远程路径
}

// SetBreakpointsArguments 设置断点参数
type SetBreakpointsArguments struct {
	Source Source `json:"source"`
	Breakpoints []SourceBreakpoint `json:"breakpoints,omitempty"`
	Lines []int `json:"lines,omitempty"` // 兼容旧版
	SourceModified bool `json:"sourceModified,omitempty"`
}

type Source struct {
	Name string `json:"name,omitempty"`
	Path string `json:"path,omitempty"`
}

type SourceBreakpoint struct {
	Line     int    `json:"line"`
	Column   int    `json:"column,omitempty"`
	Condition string `json:"condition,omitempty"`
	HitCondition string `json:"hitCondition,omitempty"`
	LogMessage string `json:"logMessage,omitempty"`
}

// SetBreakpointsResponseBody 断点设置结果
type SetBreakpointsResponseBody struct {
	Breakpoints []Breakpoint `json:"breakpoints"`
}

type Breakpoint struct {
	ID         int    `json:"id"`
	Verified   bool   `json:"verified"`
	Message    string `json:"message,omitempty"`
	Line       int    `json:"line"`
	Column     int    `json:"column,omitempty"`
	EndLine    int    `json:"endLine,omitempty"`
	EndColumn  int    `json:"endColumn,omitempty"`
	Source     Source `json:"source,omitempty"`
}

// StackFrame 调用栈帧
type StackFrame struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	Source    *Source `json:"source,omitempty"`
	Line      int    `json:"line"`
	Column    int    `json:"column"`
	EndLine   int    `json:"endLine,omitempty"`
	EndColumn int    `json:"endColumn,omitempty"`
	CanRestart bool  `json:"canRestart,omitempty"`
	PresentationHint *StackFramePresentationHint `json:"presentationHint,omitempty"`
}

type StackFramePresentationHint struct {
	Hint        string `json:"hint,omitempty"`
	Label       string `json:"label,omitempty"`
	Attributes  []string `json:"attributes,omitempty"`
}

// Scope 变量作用域
type Scope struct {
	Name               string `json:"name"`
	VariablesReference int    `json:"variablesReference"`
	NamedVariables     int    `json:"namedVariables"`
	IndexedVariables   int    `json:"indexedVariables"`
	Expensive          bool   `json:"expensive"`
	Source             *Source `json:"source,omitempty"`
	Line               int    `json:"line,omitempty"`
	Column             int    `json:"column,omitempty"`
	EndLine            int    `json:"endLine,omitempty"`
	EndColumn          int    `json:"endColumn,omitempty"`
}

// Variable 变量
type Variable struct {
	Name               string `json:"name"`
	Value              string `json:"value"`
	Type               string `json:"type,omitempty"`
	VariablesReference int    `json:"variablesReference"`
	NamedVariables     int    `json:"namedVariables"`
	IndexedVariables   int    `json:"indexedVariables"`
	EvaluateName       string `json:"evaluateName,omitempty"`
	MemoryReference    string `json:"memoryReference,omitempty"`
	PresentationHint   *VariablePresentationHint `json:"presentationHint,omitempty"`
}

type VariablePresentationHint struct {
	Kind       string   `json:"kind,omitempty"`
	Attributes []string `json:"attributes,omitempty"`
	Visibility string   `json:"visibility,omitempty"`
}

// Thread 线程
type Thread struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// StoppedEventBody 停止事件体
type StoppedEventBody struct {
	Reason     string `json:"reason"` // "breakpoint", "step", "exception", "pause", "entry", etc.
	ThreadID   int    `json:"threadId,omitempty"`
	Description string `json:"description,omitempty"`
	Text       string `json:"text,omitempty"`
	AllThreadsStopped bool `json:"allThreadsStopped,omitempty"`
	HitBreakpointIDs []int `json:"hitBreakpointIds,omitempty"`
}

// ContinuedEventBody 继续事件体
type ContinuedEventBody struct {
	ThreadID          int  `json:"threadId"`
	AllThreadsContinued bool `json:"allThreadsContinued"`
}

// OutputEventBody 输出事件体
type OutputEventBody struct {
	Output   string `json:"output"`
	Category string `json:"category,omitempty"` // "console", "stdout", "stderr", "telemetry"
	Group    string `json:"group,omitempty"`    // "start", "startCollapsed", "end"
	Line     int    `json:"line,omitempty"`
	Source   *Source `json:"source,omitempty"`
	Data     json.RawMessage `json:"data,omitempty"`
}

// ---- 内部事件回调类型 ----

// OnStoppedHandler 断点命中/单步停止回调
type OnStoppedHandler func(reason string, threadID int)

// OnOutputHandler 程序输出回调
type OnOutputHandler func(output string, category string)

// OnExitedHandler 程序退出回调
type OnExitedHandler func(exitCode int)

// ErrorHandler 错误回调
type ErrorHandler func(err error)
