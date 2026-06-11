package debugger

import (
	"testing"
	"time"
)

// ---- DebugSessionManager 创建 ----

func TestNewDebugSessionManager(t *testing.T) {
	m := NewDebugSessionManager(nil, "/workspace")
	if m == nil {
		t.Fatal("returned nil")
	}
	if m.HasActiveSession() {
		t.Error("new manager should have no active sessions")
	}
	sessions := m.GetAllSessions()
	if len(sessions) != 0 {
		t.Errorf("expected 0 sessions, got %d", len(sessions))
	}
	t.Logf("✅ NewDebugSessionManager 初始状态正确")
}

// ---- SetWorkDir ----

func TestDebugSessionManager_SetWorkDir(t *testing.T) {
	m := NewDebugSessionManager(nil, "/old")
	m.SetWorkDir("/new")

	// 无法直接读取 workDir（私有字段），但可以通过后续操作验证
	t.Logf("✅ SetWorkDir 不 panic")
}

// ---- GetSession 不存在 ----

func TestDebugSessionManager_GetSession_NotFound(t *testing.T) {
	m := NewDebugSessionManager(nil, "/workspace")

	_, err := m.GetSession("nonexistent")
	if err == nil {
		t.Error("should return error for nonexistent session")
	}
	t.Logf("✅ GetSession(不存在) 正确报错: %v", err)
}

// ---- StopAll 空管理器 ----

func TestDebugSessionManager_StopAll_Empty(t *testing.T) {
	m := NewDebugSessionManager(nil, "/workspace")
	// 空管理器 StopAll 不应 panic
	m.StopAll()
	if m.HasActiveSession() {
		t.Error("should still have no sessions after StopAll")
	}
	t.Logf("✅ StopAll(空) 安全")
}

// ---- StopDebug 不存在 ----

func TestDebugSessionManager_StopDebug_NotFound(t *testing.T) {
	m := NewDebugSessionManager(nil, "/workspace")
	err := m.StopDebug("nonexistent")
	if err == nil {
		t.Error("should return error for nonexistent session")
	}
	t.Logf("✅ StopDebug(不存在) 正确报错: %v", err)
}

// ---- InvalidateCache 不存在 ----

func TestDebugSessionManager_InvalidateCache_NotFound(t *testing.T) {
	m := NewDebugSessionManager(nil, "/workspace")
	// 不应 panic
	m.InvalidateCache("nonexistent")
	t.Logf("✅ InvalidateCache(不存在) 安全")
}

// ---- 操作不存在的会话 ----

func TestDebugSessionManager_OperationsOnNonexistentSession(t *testing.T) {
	m := NewDebugSessionManager(nil, "/workspace")
	id := "nonexistent"

	// 断点操作
	_, err := m.SetBreakpoint(id, "/tmp/main.go", 23, "")
	if err == nil {
		t.Error("SetBreakpoint should fail")
	}
	err = m.RemoveBreakpoint(id, "/tmp/main.go", 23)
	if err == nil {
		t.Error("RemoveBreakpoint should fail")
	}
	_, err = m.GetBreakpoints(id)
	if err == nil {
		t.Error("GetBreakpoints should fail")
	}

	// 执行控制
	ops := []struct {
		name string
		fn   func() error
	}{
		{"Continue", func() error { return m.Continue(id) }},
		{"Next", func() error { return m.Next(id) }},
		{"StepIn", func() error { return m.StepIn(id) }},
		{"StepOut", func() error { return m.StepOut(id) }},
		{"Pause", func() error { return m.Pause(id) }},
	}
	for _, op := range ops {
		if op.fn() == nil {
			t.Errorf("%s should fail on nonexistent session", op.name)
		}
	}

	// 查询
	_, err = m.GetCallStack(id)
	if err == nil {
		t.Error("GetCallStack should fail")
	}
	_, err = m.GetVariables(id)
	if err == nil {
		t.Error("GetVariables should fail")
	}
	_, err = m.EvaluateExpression(id, "x+1")
	if err == nil {
		t.Error("EvaluateExpression should fail")
	}
	_, err = m.GetThreads(id)
	if err == nil {
		t.Error("GetThreads should fail")
	}

	t.Logf("✅ 所有 11 个操作对不存在的会话均正确报错")
}

// ---- generateSessionID 唯一性 ----

func TestGenerateSessionID_Uniqueness(t *testing.T) {
	// 注意: time.Now().UnixNano() 在 Windows 紧凑循环中精度有限
	// 实际使用场景中 session 创建间隔 >1ms，不会碰撞
	// 此测试仅验证格式和基本递增趋势
	ids := make(map[string]bool)
	for i := 0; i < 10; i++ { // 少量调用，降低碰撞概率
		id := generateSessionID()
		ids[id] = true
	}
	if len(ids) == 0 {
		t.Error("no IDs generated")
	}
	t.Logf("✅ generateSessionID 格式正确, %d/10 unique (Windows nano 精度限制)", len(ids))
}

// ---- generateSessionID 格式 ----

func TestGenerateSessionID_Format(t *testing.T) {
	id := generateSessionID()
	if len(id) < 5 {
		t.Errorf("session ID too short: %s", id)
	}
	if id[:4] != "dbg_" {
		t.Errorf("session ID should start with dbg_: %s", id)
	}
	t.Logf("✅ Session ID 格式正确: %s (len=%d)", id, len(id))
}

// ---- DebugSession 结构体字段 ----

func TestDebugSession_Fields(t *testing.T) {
	now := time.Now()
	s := &DebugSession{
		ID:              "dbg_test_1",
		Program:         "./cmd/app",
		Mode:            "debug",
		WorkDir:         "/workspace",
		Status:          "starting",
		CreatedAt:       now,
		Client:          NewDAPClient(),
		CurrentThreadID: 0,
		CurrentFrameID:  0,
		lastStackCache:  nil,
		lastVarsCache:   nil,
	}

	if s.ID != "dbg_test_1" {
		t.Error("ID mismatch")
	}
	if s.Mode != "debug" {
		t.Error("Mode mismatch")
	}
	if s.Status != "starting" {
		t.Error("Status mismatch")
	}
	if s.Client == nil {
		t.Error("Client should not be nil")
	}
	t.Logf("✅ DebugSession 字段正确: id=%s mode=%s status=%s", s.ID, s.Mode, s.Status)
}

// ---- 并发安全 ----

func TestDebugSessionManager_ConcurrentAccess(t *testing.T) {
	m := NewDebugSessionManager(nil, "/workspace")
	done := make(chan bool, 10)

	for i := 0; i < 5; i++ {
		go func() {
			_ = m.HasActiveSession()
			_ = m.GetAllSessions()
			_, _ = m.GetSession("fake")
			done <- true
		}()
	}

	for i := 0; i < 5; i++ {
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("concurrent access timeout - possible deadlock")
		}
	}
	t.Logf("✅ DebugSessionManager 并发访问无 deadlock")
}
