package core

import (
	"testing"
)

func TestSharedMemory_AddAndGet(t *testing.T) {
	m := NewSharedMemory(10)

	m.Add(RoleUser, "hello")
	m.Add(RolePM, "this is programming task")

	if m.Len() != 2 {
		t.Fatalf("expected 2 entries, got %d", m.Len())
	}

	all := m.GetAll()
	if len(all) != 2 {
		t.Fatalf("expected 2 entries in GetAll, got %d", len(all))
	}

	if all[0].Role != RoleUser || all[0].Content != "hello" {
		t.Errorf("first entry mismatch: role=%s content=%s", all[0].Role, all[0].Content)
	}
}

func TestSharedMemory_GetByRole(t *testing.T) {
	m := NewSharedMemory(10)

	m.Add(RoleUser, "msg1")
	m.Add(RolePM, "msg2")
	m.Add(RoleSE, "msg3")
	m.Add(RolePM, "msg4")

	pmMsgs := m.GetByRole(RolePM)
	if len(pmMsgs) != 2 {
		t.Fatalf("expected 2 PM messages, got %d", len(pmMsgs))
	}
}

func TestSharedMemory_MaxLen(t *testing.T) {
	m := NewSharedMemory(3)

	for i := 0; i < 5; i++ {
		m.Add(RoleUser, string(rune('a'+i)))
	}

	if m.Len() != 3 {
		t.Fatalf("expected max 3 entries, got %d", m.Len())
	}

	all := m.GetAll()
	if all[0].Content != "c" {
		t.Errorf("oldest entry should be 'c', got '%s'", all[0].Content)
	}
}

func TestSharedMemory_Clear(t *testing.T) {
	m := NewSharedMemory(10)
	m.Add(RoleUser, "test")
	m.Clear()

	if m.Len() != 0 {
		t.Fatalf("expected 0 after Clear, got %d", m.Len())
	}
}

func TestSharedMemory_LastUserMsg(t *testing.T) {
	m := NewSharedMemory(10)
	m.Add(RoleUser, "first")
	m.Add(RolePM, "pm msg")
	m.Add(RoleUser, "second last")

	last := m.LastUserMsg()
	if last != "second last" {
		t.Errorf("expected 'second last', got '%s'", last)
	}
}

func TestSharedMemory_FormatForPrompt(t *testing.T) {
	m := NewSharedMemory(10)
	m.Add(RoleUser, "create hello.go")
	m.Add(RolePM, "programming task detected")

	formatted := m.FormatForPrompt()
	if len(formatted) == 0 {
		t.Fatal("FormatForPrompt returned empty")
	}
}

func TestPromptKit_Get(t *testing.T) {
	pk := NewPromptKit("/test/workdir")

	pm := pk.Get(RolePM)
	if pm == "" {
		t.Fatal("PM prompt should not be empty")
	}

	se := pk.Get(RoleSE)
	if se == "" {
		t.Fatal("SE prompt should not be empty")
	}

	ap := pk.Get(RoleAP)
	if ap == "" {
		t.Fatal("AP prompt should not be empty")
	}

	unknown := pk.Get(RoleSys)
	if unknown != "" {
		t.Error("unknown role should return empty prompt")
	}
}

func TestPromptKit_GetFix(t *testing.T) {
	pk := NewPromptKit("/test/workdir")

	fix := pk.GetFix("file not found", "write_file hello.go")
	if fix == "" {
		t.Fatal("Fix prompt should not be empty")
	}

	if !contains(fix, "file not found") || !contains(fix, "write_file hello.go") {
		t.Errorf("Fix prompt should contain error details: %s", fix)
	}
}

func TestParsePMResponse_ProgrammingTask(t *testing.T) {
	core := &ArgusCore{}

	isProg, task := core.parsePMResponse(`{"is_programming":true,"task":"create hello.go","files":["hello.go"]}`)
	if !isProg {
		t.Error("should detect programming task from JSON")
	}
	if task == "" {
		t.Error("task should not be empty")
	}
}

func TestParsePMResponse_KeywordDetection(t *testing.T) {
	core := &ArgusCore{}

	tests := []struct {
		input    string
		expected bool
	}{
		{"创建一个文件", true},
		{"写一个程序", true},
		{"implement feature", true},
		{"create hello.go", true},
		{"今天天气怎么样", false},
		{"你好", false},
	}

	for _, tt := range tests {
		isProg, _ := core.parsePMResponse(tt.input)
		if isProg != tt.expected {
			t.Errorf("parsePMResponse(%q) = %v, want %v", tt.input, isProg, tt.expected)
		}
	}
}

func TestParseSEResponse_Completed(t *testing.T) {
	core := &ArgusCore{}

	response := `{"task_status":"completed","files":["hello.go"],"verified":true,"summary":"done"}`
	actions, completed := core.parseSEResponse(response)

	if !completed {
		t.Error("should detect completed status")
	}
	if len(actions) != 0 {
		t.Errorf("completed response should have no actions, got %d", len(actions))
	}
}

func TestFindMatchingBracket(t *testing.T) {
	tests := []struct {
		input    string
		expected int
	}{
		{"[]", 1},
		{"[{}]", 3},
		{"[[], {}]", 7},
		{"{[]}", 3},
	}

	for _, tt := range tests {
		result := findMatchingBracket(tt.input)
		if result != tt.expected {
			t.Errorf("findMatchingBracket(%q) = %d, want %d", tt.input, result, tt.expected)
		}
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
