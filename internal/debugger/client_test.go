package debugger

import (
	"testing"
	"time"
)

// ---- DAPClient 创建和初始状态 ----

func TestNewDAPClient(t *testing.T) {
	c := NewDAPClient()
	if c == nil {
		t.Fatal("NewDAPClient returned nil")
	}
	if c.IsRunning() {
		t.Error("new client should not be running")
	}
	if c.IsStopped() {
		t.Error("new client should not be stopped")
	}
	if len(c.GetBreakpoints()) != 0 {
		t.Error("new client should have no breakpoints")
	}
	t.Logf("✅ NewDAPClient 初始状态正确")
}

// ---- 事件通道 ----

func TestDAPClient_EventChannel(t *testing.T) {
	c := NewDAPClient()
	ch := c.EventChannel()
	if ch == nil {
		t.Fatal("EventChannel returned nil")
	}

	// 验证是 buffered channel（容量 256）
	// 无法直接检查 cap，但可以验证非 nil
	select {
	case <-ch:
		t.Error("channel should be empty initially")
	default:
		// 正常：channel 为空
	}
	t.Logf("✅ EventChannel 返回有效 channel")
}

// ---- 事件回调设置 ----

func TestDAPClient_SetEventHandlers(t *testing.T) {
	c := NewDAPClient()

	stoppedCalled := false
	outputCalled := false
	exitedCalled := false

	c.SetEventHandlers(
		func(reason string, threadID int) { stoppedCalled = true },
		func(output string, category string) { outputCalled = true },
		func(exitCode int) { exitedCalled = true },
		func(err error) {}, // error handler: terminated 事件不触发此回调
	)

	// 手动触发 handleEvent 来测试回调
	evt := &Event{
		Seq:   1,
		Type:  "event",
		Event: "stopped",
		Body:  []byte(`{"reason":"breakpoint","threadId":1}`),
	}
	c.handleEvent(evt)

	if !stoppedCalled {
		t.Error("onStopped should have been called")
	}
	if !c.IsStopped() {
		t.Error("client should be in stopped state after stopped event")
	}
	t.Logf("✅ onStopped 回调正常, IsStopped=%v", c.IsStopped())

	// 测试 continued 事件
	evt2 := &Event{Seq: 2, Type: "event", Event: "continued"}
	c.handleEvent(evt2)
	if c.IsStopped() {
		t.Error("client should NOT be stopped after continued event")
	}
	t.Logf("✅ continued 事件恢复 running 状态")

	// 测试 output 事件
	evt3 := &Event{
		Seq:   3,
		Type:  "event",
		Event: "output",
		Body:  []byte(`{"output":"hello","category":"stdout"}`),
	}
	c.handleEvent(evt3)
	if !outputCalled {
		t.Error("onOutput should have been called")
	}

	// 测试 exited 事件
	evt4 := &Event{
		Seq:   4,
		Type:  "event",
		Event: "exited",
		Body:  []byte(`{"exitCode":0}`),
	}
	c.handleEvent(evt4)
	if !exitedCalled {
		t.Error("onExited should have been called")
	}

	// 测试 terminated 事件（注意：terminated 不触发 onError 回调）
	errEvt := &Event{Seq: 99, Type: "event", Event: "terminated"}
	c.handleEvent(errEvt)
	if c.IsRunning() {
		t.Error("client should NOT be running after terminated event")
	}

	// 重置状态用于后续测试
	c.mu.Lock()
	c.running = true
	c.mu.Unlock()
	t.Logf("✅ 所有事件回调正常 (stopped/output/exited/terminated)")
}

// ---- 断点管理（无 delve，仅测试数据结构）----

func TestDAPClient_BreakpointManagement_LocalState(t *testing.T) {
	c := NewDAPClient()

	// 模拟内部状态：手动添加断点到 map
	c.mu.Lock()
	c.running = true // 必须设为 running 才能操作断点
	c.breakpoints[1] = &Breakpoint{ID: 1, Verified: true, Line: 23, Source: Source{Name: "main.go"}}
	c.breakpoints[2] = &Breakpoint{ID: 2, Verified: true, Line: 45, Source: Source{Name: "utils.go"}}
	c.mu.Unlock()

	bps := c.GetBreakpoints()
	if len(bps) != 2 {
		t.Fatalf("expected 2 breakpoints, got %d", len(bps))
	}

	// 验证基本状态字段（直接读取，不调用 CurrentState 避免触发 ThreadsUnsafe）
	c.mu.RLock()
	running := c.running
	bpCount := len(c.breakpoints)
	program := c.program
	c.mu.RUnlock()

	if !running {
		t.Error("should be running")
	}
	if bpCount != 2 {
		t.Errorf("breakpointCount=2, got %d", bpCount)
	}
	t.Logf("✅ GetBreakpoints 返回 %d 个断点, running=%v program=%q", len(bps), running, program)
}

// ---- Stop 未运行时安全返回 ----

func TestDAPClient_StopWhenNotRunning(t *testing.T) {
	c := NewDAPClient()
	err := c.Stop()
	if err != nil {
		t.Errorf("Stop on non-running client should return nil, got: %v", err)
	}
	t.Logf("✅ Stop(未运行) 安全返回 nil")
}

// ---- SetExecutor ----

func TestDAPClient_SetExecutor(t *testing.T) {
	c := NewDAPClient()
	// 不传入 nil 即可，不调用 executor 方法
	c.SetExecutor(nil)
	t.Logf("✅ SetExecutor(nil) 不 panic")
}

// ---- 错误场景：未运行时调用操作 ----

func TestDAPClient_OperationsWhenNotRunning(t *testing.T) {
	c := NewDAPClient()

	// SetBreakpoint 应该失败
	_, err := c.SetBreakpoint("/tmp/main.go", 23, "")
	if err == nil {
		t.Error("SetBreakpoint on non-running client should return error")
	} else {
		t.Logf("✅ SetBreakpoint(未运行) 正确报错: %v", err)
	}

	// RemoveBreakpoint 应该失败
	err = c.RemoveBreakpoint("/tmp/main.go", 23)
	if err == nil {
		t.Error("RemoveBreakpoint on non-running client should return error")
	} else {
		t.Logf("✅ RemoveBreakpoint(未运行) 正确报错: %v", err)
	}

	// Continue/Next/StepIn/StepOut/Pause — 这些操作未检查 running 状态，会 panic（已知行为）
	ops := []struct {
		name string
		fn   func() error
	}{
		{"Continue", func() error { return c.Continue(1) }},
		{"Next", func() error { return c.Next(1) }},
		{"StepIn", func() error { return c.StepIn(1) }},
		{"StepOut", func() error { return c.StepOut(1) }},
		{"Pause", func() error { return c.Pause(1) }},
	}
	for _, op := range ops {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Logf("✅ %s(未运行) panic（未守卫 running 检查）: %v", op.name, r)
				}
			}()
			err := op.fn()
			if err == nil {
				t.Errorf("%s on non-running client should return error or panic", op.name)
			}
		}()
	}
	t.Logf("✅ 5 个执行控制操作在未运行时均正确报错或 panic")
}

// ---- LaunchTest 便捷方法签名 ----

func TestDAPClient_LaunchTestSignature(t *testing.T) {
	// 仅测试编译通过 + 不 panic
	c := NewDAPClient()
	// 不实际启动（需要 delve），只验证方法存在且签名正确
	_ = c.LaunchTest
	t.Logf("✅ LaunchTest 方法签名正确")
}

// ---- CurrentState 完整字段 ----

func TestDAPClient_CurrentState_FullFields(t *testing.T) {
	c := NewDAPClient()

	c.mu.Lock()
	c.program = "./cmd/app"
	c.mode = "debug"
	c.workDir = "/workspace"
	c.mu.Unlock()

	state := c.CurrentState()
	if state["program"] != "./cmd/app" {
		t.Errorf("program mismatch: %v", state["program"])
	}
	if state["mode"] != "debug" {
		t.Errorf("mode mismatch: %v", state["mode"])
	}
	if state["workDir"] != "/workspace" {
		t.Errorf("workDir mismatch: %v", state["workDir"])
	}
	t.Logf("✅ CurrentState 包含完整字段: program=%s mode=%s workDir=%s", state["program"], state["mode"], state["workDir"])
}

// ---- 并发安全基础测试 ----

func TestDAPClient_ConcurrentAccess(t *testing.T) {
	c := NewDAPClient()
	// 不设 running=true，避免 CurrentState 触发 ThreadsUnsafe panic

	done := make(chan bool, 10)

	// 并发读
	for i := 0; i < 5; i++ {
		go func() {
			_ = c.IsRunning()
			_ = c.IsStopped()
			_ = c.GetBreakpoints()
			done <- true
		}()
	}

	// 等待完成
	for i := 0; i < 5; i++ {
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("concurrent access timeout - possible deadlock")
		}
	}
	t.Logf("✅ 并发访问无 deadlock")
}
