package debugger

import (
	"encoding/json"
	"testing"
)

// ---- 序列化测试 ----

func TestBaseRequest_Marshal(t *testing.T) {
	req := BaseRequest{
		Seq:     1,
		Type:    "request",
		Command: "initialize",
		Arguments: InitializeRequest{
			ClientName:    "argus-debugger",
			AdapterID:    "go",
			LinesStartAt1: true,
			ColumnsStartAt1: true,
		},
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if decoded["type"] != "request" {
		t.Errorf("expected type=request, got %v", decoded["type"])
	}
	if decoded["command"] != "initialize" {
		t.Errorf("expected command=initialize, got %v", decoded["command"])
	}
	t.Logf("✅ BaseRequest 序列化成功: %d bytes", len(data))
}

func TestBaseResponse_Unmarshal(t *testing.T) {
	raw := `{"seq":1,"type":"response","request_seq":1,"success":true,"command":"initialize","body":{}}`
	var resp BaseResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if !resp.Success {
		t.Error("expected success=true")
	}
	if resp.Command != "initialize" {
		t.Errorf("expected command=initialize, got %s", resp.Command)
	}
	t.Logf("✅ BaseResponse 反序列化成功")
}

func TestEvent_MarshalUnmarshal(t *testing.T) {
	evt := Event{
		Seq:   2,
		Type:  "event",
		Event: "stopped",
		Body:  json.RawMessage(`{"reason":"breakpoint","threadId":1}`),
	}

	data, err := json.Marshal(evt)
	if err != nil {
		t.Fatalf("marshal event failed: %v", err)
	}

	var evt2 Event
	if err := json.Unmarshal(data, &evt2); err != nil {
		t.Fatalf("unmarshal event failed: %v", err)
	}
	if evt2.Event != "stopped" {
		t.Errorf("expected event=stopped, got %s", evt2.Event)
	}

	var body StoppedEventBody
	if evt2.Body != nil {
		json.Unmarshal(evt2.Body, &body)
	}
	if body.Reason != "breakpoint" {
		t.Errorf("expected reason=breakpoint, got %s", body.Reason)
	}
	t.Logf("✅ Event 序列化/反序列化成功, reason=%s threadId=%d", body.Reason, body.ThreadID)
}

// ---- 断点相关类型 ----

func TestSetBreakpointsArguments_Marshal(t *testing.T) {
	args := SetBreakpointsArguments{
		Source: Source{Name: "main.go", Path: "/tmp/main.go"},
		Breakpoints: []SourceBreakpoint{
			{Line: 23, Condition: "x > 0"},
			{Line: 45},
		},
	}

	data, _ := json.Marshal(args)
	var m map[string]interface{}
	json.Unmarshal(data, &m)

	src := m["source"].(map[string]interface{})
	if src["name"] != "main.go" {
		t.Errorf("source name mismatch")
	}
	bps := m["breakpoints"].([]interface{})
	if len(bps) != 2 {
		t.Errorf("expected 2 breakpoints, got %d", len(bps))
	}
	t.Logf("✅ SetBreakpointsArguments 序列化成功, %d 断点", len(bps))
}

func TestBreakpoint_Verified(t *testing.T) {
	bp := Breakpoint{
		ID:       1,
		Verified: true,
		Line:     23,
		Source:   Source{Name: "main.go", Path: "/tmp/main.go"},
	}

	data, _ := json.Marshal(bp)
	var bp2 Breakpoint
	json.Unmarshal(data, &bp2)

	if !bp2.Verified {
		t.Error("expected verified=true")
	}
	if bp2.Line != 23 {
		t.Errorf("expected line=23, got %d", bp2.Line)
	}
	if bp2.Source.Name != "main.go" {
		t.Error("source info lost")
	}
	t.Logf("✅ Breakpoint 序列化/反序列化成功 id=%d verified=%v line=%d", bp2.ID, bp2.Verified, bp2.Line)
}

// ---- StackFrame / Scope / Variable ----

func TestStackFrame_FullRoundTrip(t *testing.T) {
	frame := StackFrame{
		ID:        1,
		Name:      "main.main",
		Source:    &Source{Name: "main.go", Path: "/tmp/main.go"},
		Line:      23,
		Column:    5,
		CanRestart: false,
	}

	data, _ := json.Marshal(frame)
	var f2 StackFrame
	json.Unmarshal(data, &f2)

	if f2.Name != "main.main" {
		t.Errorf("name mismatch: %s", f2.Name)
	}
	if f2.Line != 23 {
		t.Errorf("line mismatch: %d", f2.Line)
	}
	t.Logf("✅ StackFrame round-trip OK: %s:%d", f2.Name, f2.Line)
}

func TestScopeAndVariable(t *testing.T) {
	scope := Scope{
		Name:               "Locals",
		VariablesReference: 1001,
		NamedVariables:     3,
		Expensive:          false,
	}

	v := Variable{
		Name:               "x",
		Value:              "42",
		Type:               "int",
		VariablesReference: 0,
		EvaluateName:       "x",
	}

	sData, _ := json.Marshal(scope)
	vData, _ := json.Marshal(v)

	var s2 Scope
	var v2 Variable
	json.Unmarshal(sData, &s2)
	json.Unmarshal(vData, &v2)

	if s2.Name != "Locals" {
		t.Errorf("scope name: %s", s2.Name)
	}
	if v2.Value != "42" {
		t.Errorf("variable value: %s", v2.Value)
	}
	t.Logf("✅ Scope(%s vr=%d) + Variable(%s=%s) round-trip OK", s2.Name, s2.VariablesReference, v2.Name, v2.Value)
}

// ---- Seq 生成器 ----

func TestNextSeq(t *testing.T) {
	// 重置计数器测试（注意：全局状态，顺序执行）
	seq1 := nextSeq()
	seq2 := nextSeq()
	seq3 := nextSeq()

	if seq2 <= seq1 {
		t.Errorf("seq should increment: seq1=%d seq2=%d", seq1, seq2)
	}
	if seq3 <= seq2 {
		t.Errorf("seq should increment: seq2=%d seq3=%d", seq2, seq3)
	}
	t.Logf("✅ Seq 生成器正常: %d -> %d -> %d", seq1, seq2, seq3)
}

// ---- LaunchArguments ----

func TestLaunchArguments_DebugMode(t *testing.T) {
	args := LaunchArguments{
		Mode:        "debug",
		Program:     "./cmd/app",
		StopOnEntry: true,
		Cwd:         "/workspace",
		Args:        []string{"--port", "8080"},
		Env:         map[string]string{"GOOS": "linux"},
	}

	data, _ := json.Marshal(args)
	var args2 LaunchArguments
	json.Unmarshal(data, &args2)

	if args2.Mode != "debug" {
		t.Errorf("mode: %s", args2.Mode)
	}
	if !args2.StopOnEntry {
		t.Error("stopOnEntry should be true")
	}
	if len(args2.Args) != 2 {
		t.Errorf("args count: %d", len(args2.Args))
	}
	t.Logf("✅ LaunchArguments debug mode OK: program=%s stopOnEntry=%v", args2.Program, args2.StopOnEntry)
}

// ---- OutputEventBody 多 category ----

func TestOutputEventBody_Categories(t *testing.T) {
	categories := []struct {
		cat string
		body OutputEventBody
	}{
		{"stdout", OutputEventBody{Output: "hello world", Category: "stdout"}},
		{"stderr", OutputEventBody{Output: "error!", Category: "stderr"}},
		{"console", OutputEventBody{Output: "log msg", Category: "console"}},
		{"empty", OutputEventBody{Output: "just output"}},
	}

	for _, tc := range categories {
		data, _ := json.Marshal(tc.body)
		var b2 OutputEventBody
		json.Unmarshal(data, &b2)
		if b2.Output != tc.body.Output {
			t.Errorf("[%s] output mismatch", tc.cat)
		}
		if tc.body.Category != "" && b2.Category != tc.body.Category {
			t.Errorf("[%s] category: expected=%s got=%s", tc.cat, tc.body.Category, b2.Category)
		}
	}
	t.Logf("✅ OutputEventBody 4 categories round-trip OK")
}
