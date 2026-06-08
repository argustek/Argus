package debugger

import (
	"fmt"
	"sync"
	"time"

	"argus/internal/executor"
)

// DebugSessionManager 调试会话管理器，管理多个调试会话的生命周期
type DebugSessionManager struct {
	mu         sync.RWMutex
	sessions   map[string]*DebugSession // sessionID -> session
	executor   *executor.Executor
	workDir    string
}

// DebugSession 单个调试会话
type DebugSession struct {
	ID          string            `json:"id"`
	Program     string            `json:"program"`
	Mode        string            `json:"mode"`       // "debug" or "test"
	WorkDir     string            `json:"workDir"`
	Status      string            `json:"status"`     // "starting", "running", "stopped", "exited", "error"
	CreatedAt   time.Time         `json:"createdAt"`
	StoppedAt   *time.Time        `json:"stoppedAt,omitempty"`
	Client      *DAPClient        `json:"-"`
	CurrentThreadID int           `json:"currentThreadId"` // 当前暂停的线程ID
	CurrentFrameID  int           `json:"currentFrameId"`  // 当前栈帧ID（用于变量查询）

	// 最后一次查询缓存（减少重复 DAP 调用）
	lastStackCache    []StackFrame
	lastStackTime     time.Time
	lastVarsCache     map[int][]Variable
	lastVarsTime      time.Time
}

// NewDebugSessionManager 创建调试会话管理器
func NewDebugSessionManager(ex *executor.Executor, workDir string) *DebugSessionManager {
	return &DebugSessionManager{
		sessions: make(map[string]*DebugSession),
		executor: ex,
		workDir:  workDir,
	}
}

// SetWorkDir 设置工作目录
func (m *DebugSessionManager) SetWorkDir(dir string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.workDir = dir
}

// ---- 会话生命周期 ----

// StartDebug 启动新的调试会话
func (m *DebugSessionManager) StartDebug(program string, mode string, args []string, stopOnEntry bool) (*DebugSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	session := &DebugSession{
		ID:        generateSessionID(),
		Program:   program,
		Mode:      mode,
		WorkDir:   m.workDir,
		Status:    "starting",
		CreatedAt: time.Now(),
		Client:    NewDAPClient(),
	}

	if m.executor != nil {
		session.Client.SetExecutor(m.executor)
	}

	// 设置事件回调
	session.Client.SetEventHandlers(
		func(reason string, threadID int) {
			session.Status = "stopped"
			session.CurrentThreadID = threadID
			now := time.Now()
			session.StoppedAt = &now
		},
		func(output string, category string) {
			// 程序输出转发到终端
			if m.executor != nil && m.executor.TerminalOutput != nil {
				prefix := ""
				switch category {
				case "stderr":
					prefix = "[DEBUG-ERR] "
				case "stdout":
					prefix = "[DEBUG-OUT] "
				default:
					prefix = "[DEBUG] "
				}
				m.executor.TerminalOutput(prefix + output)
			}
		},
		func(exitCode int) {
			session.Status = "exited"
			now := time.Now()
			session.StoppedAt = &now
		},
		func(err error) {
			session.Status = "error"
		},
	)

	// 启动调试
	err := session.Client.Launch(program, mode, m.workDir, args, stopOnEntry)
	if err != nil {
		session.Status = "error"
		return session, fmt.Errorf("launch failed: %w", err)
	}

	session.Status = "running"
	m.sessions[session.ID] = session

	return session, nil
}

// StartTestDebug 以测试模式启动调试
func (m *DebugSessionManager) StartTestDebug(testPattern string, stopOnEntry bool) (*DebugSession, error) {
	return m.StartDebug(testPattern, "test", nil, stopOnEntry)
}

// StopDebug 停止指定调试会话
func (m *DebugSessionManager) StopDebug(sessionID string) error {
	m.mu.Lock()
	session, ok := m.sessions[sessionID]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("session %s not found", sessionID)
	}
	m.mu.Unlock()

	err := session.Client.Stop()
	session.Status = "exited"

	m.mu.Lock()
	delete(m.sessions, sessionID)
	m.mu.Unlock()

	return err
}

// StopAll 停止所有调试会话
func (m *DebugSessionManager) StopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for id, session := range m.sessions {
		session.Client.Stop()
		session.Status = "exited"
		delete(m.sessions, id)
	}
}

// GetSession 获取指定会话
func (m *DebugSessionManager) GetSession(sessionID string) (*DebugSession, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	session, ok := m.sessions[sessionID]
	if !ok {
		return nil, fmt.Errorf("session %s not found", sessionID)
	}
	return session, nil
}

// GetAllSessions 获取所有活跃会话
func (m *DebugSessionManager) GetAllSessions() []*DebugSession {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*DebugSession, 0, len(m.sessions))
	for _, s := range m.sessions {
		result = append(result, s)
	}
	return result
}

// HasActiveSession 是否有活跃的调试会话
func (m *DebugSessionManager) HasActiveSession() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions) > 0
}

// ---- 断点操作（便捷方法）----

// SetBreakpoint 在指定会话中设置断点
func (m *DebugSessionManager) SetBreakpoint(sessionID, filePath string, line int, condition string) (*Breakpoint, error) {
	session, err := m.GetSession(sessionID)
	if err != nil {
		return nil, err
	}
	return session.Client.SetBreakpoint(filePath, line, condition)
}

// RemoveBreakpoint 移除断点
func (m *DebugSessionManager) RemoveBreakpoint(sessionID, filePath string, line int) error {
	session, err := m.GetSession(sessionID)
	if err != nil {
		return err
	}
	return session.Client.RemoveBreakpoint(filePath, line)
}

// GetBreakpoints 获取断点列表
func (m *DebugSessionManager) GetBreakpoints(sessionID string) ([]*Breakpoint, error) {
	session, err := m.GetSession(sessionID)
	if err != nil {
		return nil, err
	}
	return session.Client.GetBreakpoints(), nil
}

// ---- 执行控制（便捷方法）----

// Continue 继续执行
func (m *DebugSessionManager) Continue(sessionID string) error {
	session, err := m.GetSession(sessionID)
	if err != nil {
		return err
	}
	return session.Client.Continue(session.CurrentThreadID)
}

// Next Step Over
func (m *DebugSessionManager) Next(sessionID string) error {
	session, err := m.GetSession(sessionID)
	if err != nil {
		return err
	}
	return session.Client.Next(session.CurrentThreadID)
}

// StepIn Step Into
func (m *DebugSessionManager) StepIn(sessionID string) error {
	session, err := m.GetSession(sessionID)
	if err != nil {
		return err
	}
	return session.Client.StepIn(session.CurrentThreadID)
}

// StepOut Step Out
func (m *DebugSessionManager) StepOut(sessionID string) error {
	session, err := m.GetSession(sessionID)
	if err != nil {
		return err
	}
	return session.Client.StepOut(session.CurrentThreadID)
}

// Pause 暂停
func (m *DebugSessionManager) Pause(sessionID string) error {
	session, err := m.GetSession(sessionID)
	if err != nil {
		return err
	}
	return session.Client.Pause(session.CurrentThreadID)
}

// ---- 信息查询（带缓存）----

// GetCallStack 获取调用栈（5秒缓存）
func (m *DebugSessionManager) GetCallStack(sessionID string) ([]StackFrame, error) {
	session, err := m.GetSession(sessionID)
	if err != nil {
		return nil, err
	}

	// 缓存 5 秒
	if time.Since(session.lastStackTime) < 5*time.Second && session.lastStackCache != nil {
		return session.lastStackCache, nil
	}

	frames, err := session.Client.StackTrace(session.CurrentThreadID, 0, 20)
	if err != nil {
		return nil, err
	}
	session.lastStackCache = frames
	session.lastStackTime = time.Now()
	return frames, nil
}

// GetVariables 获取当前帧的局部变量（5秒缓存）
func (m *DebugSessionManager) GetVariables(sessionID string) (map[string][]Variable, error) {
	session, err := m.GetSession(sessionID)
	if err != nil {
		return nil, err
	}

	// 先获取 scopes
	scopes, err := session.Client.Scopes(session.CurrentFrameID)
	if err != nil {
		return nil, err
	}

	result := make(map[string][]Variable)
	for _, scope := range scopes {
		// 缓存检查
		cached, ok := session.lastVarsCache[scope.VariablesReference]
		if ok && time.Since(session.lastVarsTime) < 5*time.Second {
			result[scope.Name] = cached
			continue
		}

		vars, err := session.Client.Variables(scope.VariablesReference, 0, 100)
		if err != nil {
			// 单个 scope 失败不阻断其他
			continue
		}
		result[scope.Name] = vars

		if session.lastVarsCache == nil {
			session.lastVarsCache = make(map[int][]Variable)
		}
		session.lastVarsCache[scope.VariablesReference] = vars
		session.lastVarsTime = time.Now()
	}

	return result, nil
}

// EvaluateExpression 计算表达式
func (m *DebugSessionManager) EvaluateExpression(sessionID, expression string) (*Variable, error) {
	session, err := m.GetSession(sessionID)
	if err != nil {
		return nil, err
	}
	return session.Client.Evaluate(expression, session.CurrentFrameID, "repl")
}

// GetThreads 获取线程列表
func (m *DebugSessionManager) GetThreads(sessionID string) ([]Thread, error) {
	session, err := m.GetSession(sessionID)
	if err != nil {
		return nil, err
	}
	return session.Client.Threads()
}

// InvalidateCache 清除查询缓存（在状态变更后调用）
func (m *DebugSessionManager) InvalidateCache(sessionID string) {
	m.mu.RLock()
	session, ok := m.sessions[sessionID]
	m.mu.RUnlock()
	if ok {
		session.lastStackCache = nil
		session.lastVarsCache = nil
	}
}

// ---- 辅助 ----

func generateSessionID() string {
	return fmt.Sprintf("dbg_%d", time.Now().UnixNano())
}
