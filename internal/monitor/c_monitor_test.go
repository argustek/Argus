package monitor

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"argus/internal/types"
)

func newTestCMonitor() *CMonitor {
	return &CMonitor{
		stateFile:     "test_config/state.json",
		alertFunc:     func(string) {},
		messageSender: func(string) {},
		notifyPM:      func(string) {},
		pmPingChan:    make(chan bool, 1),
		stopChan:      make(chan struct{}),
	}
}

func TestHandleProjectError_ResetsDoubleIdle(t *testing.T) {
	c := newTestCMonitor()
	c.doubleIdleStartTime = 12345
	c.idleAlertedTime = 67890

	c.handleProjectError()

	if c.doubleIdleStartTime != 0 {
		t.Errorf("doubleIdleStartTime = %d, want 0", c.doubleIdleStartTime)
	}
	if c.idleAlertedTime != 0 {
		t.Errorf("idleAlertedTime = %d, want 0", c.idleAlertedTime)
	}
}

func TestHandleProjectError_OnlyResetsOnce(t *testing.T) {
	c := newTestCMonitor()
	c.doubleIdleStartTime = 100
	c.idleAlertedTime = 200

	c.handleProjectError()
	c.handleProjectError()

	if c.doubleIdleStartTime != 0 {
		t.Errorf("doubleIdleStartTime = %d, want 0 (second call with alertedError=true shouldn't change fields)", c.doubleIdleStartTime)
	}
}

func TestHandleProjectRunning_PMBusy_ResetsDoubleIdleViaElse(t *testing.T) {
	c := newTestCMonitor()
	c.doubleIdleStartTime = 12345
	c.idleAlertedTime = 67890

	state := types.State{
		ProjectState: types.ProjectStateRunning,
		PmStatus:     types.RoleStatusBusy,
		SeStatus:     types.RoleStatusIdle,
	}
	c.handleProjectRunning(state, time.Now().Unix())

	if c.doubleIdleStartTime != 0 {
		t.Errorf("doubleIdleStartTime = %d, want 0 (should reset via condition4 else-branch)", c.doubleIdleStartTime)
	}
	if c.idleAlertedTime != 67890 {
		t.Errorf("idleAlertedTime = %d, want 67890 (should NOT be touched when PM is busy)", c.idleAlertedTime)
	}
}

func TestHandleProjectRunning_ThenBothIdle_StartsNewTimer(t *testing.T) {
	c := newTestCMonitor()
	c.doubleIdleStartTime = 0
	c.idleAlertedTime = 0

	now := time.Now().Unix()
	state := types.State{
		ProjectState: types.ProjectStateRunning,
		PmStatus:     types.RoleStatusIdle,
		SeStatus:     types.RoleStatusIdle,
	}

	c.handleProjectRunning(state, now)

	if c.doubleIdleStartTime == 0 {
		t.Fatal("doubleIdleStartTime should be set after both-idle detection")
	}
	if c.doubleIdleStartTime != now {
		t.Errorf("doubleIdleStartTime = %d, want %d", c.doubleIdleStartTime, now)
	}
}

func TestResetSessionState_ResetsDoubleIdle(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "cmonitor-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	c := newTestCMonitor()
	c.stateFile = tmpDir + "/state.json"
	c.doubleIdleStartTime = 12345
	c.idleAlertedTime = 67890

	if err := c.ResetSessionState(); err != nil {
		t.Fatal(err)
	}

	if c.doubleIdleStartTime != 0 {
		t.Errorf("doubleIdleStartTime = %d, want 0", c.doubleIdleStartTime)
	}
	if c.idleAlertedTime != 0 {
		t.Errorf("idleAlertedTime = %d, want 0", c.idleAlertedTime)
	}
}

func TestDoubleIdleAlert_TriggersAfter60s(t *testing.T) {
	c := newTestCMonitor()
	alerted := false
	c.alertFunc = func(msg string) {}
	c.notifyPM = func(msg string) { alerted = true }

	now := int64(1000000)
	state := types.State{
		ProjectState: types.ProjectStateRunning,
		PmStatus:     types.RoleStatusIdle,
		SeStatus:     types.RoleStatusIdle,
	}

	c.handleProjectRunning(state, now)

	if c.doubleIdleStartTime != now {
		t.Fatalf("doubleIdleStartTime = %d, want %d", c.doubleIdleStartTime, now)
	}

	c.handleProjectRunning(state, now+61)

	if !alerted {
		t.Error("expected alert when both idle >60s")
	}
}

// ---------------------------------------------------------------------------
// Weight detection tests
// ---------------------------------------------------------------------------

func TestClassifyWeight(t *testing.T) {
	tests := []struct {
		count int
		want  string
	}{
		{0, types.WeightFeatherweight},
		{1, types.WeightFeatherweight},
		{4, types.WeightFeatherweight},
		{5, types.WeightLightweight},
		{10, types.WeightLightweight},
		{20, types.WeightLightweight},
		{21, types.WeightMedium},
		{100, types.WeightMedium},
		{1000, types.WeightMedium},
	}
	for _, tt := range tests {
		got := classifyWeight(tt.count)
		if got != tt.want {
			t.Errorf("classifyWeight(%d) = %q, want %q", tt.count, got, tt.want)
		}
	}
}

func TestScanSourceFiles(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "doc_weight_test_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create test files
	files := []struct {
		path string
		body string
	}{
		{"main.go", "package main"},
		{"utils.go", "package utils"},
		{".argus/plan.md", "---\nnode_id: L0\n---"},
		{"node_modules/foo.js", "skip"},
		{"vendor/bar.go", "skip"},
		{"config.json", `{"key": "val"}`},
		{"build/output.o", "obj"}, // .o not in skipExt, but build dir is skipped
	}
	for _, f := range files {
		dir := filepath.Dir(f.path)
		if dir != "." {
			os.MkdirAll(filepath.Join(tmpDir, dir), 0755)
		}
		os.WriteFile(filepath.Join(tmpDir, f.path), []byte(f.body), 0644)
	}

	count := ScanSourceFiles(tmpDir)
	// Should count: main.go, utils.go (2). .argus, node_modules, vendor, build dirs all skipped.
	// config.json (.json) skipped.
	if count != 2 {
		t.Errorf("ScanSourceFiles = %d, want 2. Files in tmp: %s", count, tmpDir)
	}
}

func TestDetectDocWeightChange_NoChange(t *testing.T) {
	c := newTestCMonitor()
	dir, _ := os.MkdirTemp("", "detect_test_*")
	defer os.RemoveAll(dir)
	c.workDir = dir
	c.stateFile = filepath.Join(dir, "state.json")

	state := types.State{
		DocWeight:      types.WeightLightweight,
		DocWeightFiles: 10,
	}
	c.writeStateLocked(state)

	changed := c.detectDocWeightChange(&state)
	if changed {
		t.Error("expected no change, but got changed=true")
	}
}

func TestDetectDocWeightChange_CrossThreshold(t *testing.T) {
	c := newTestCMonitor()
	dir, _ := os.MkdirTemp("", "detect_test_*")
	defer os.RemoveAll(dir)
	c.workDir = dir
	c.stateFile = filepath.Join(dir, "state.json")

	// Create 21 go files to cross medium+ threshold
	for i := 0; i < 21; i++ {
		os.WriteFile(filepath.Join(c.workDir, fmt.Sprintf("f%d.go", i)), []byte("package p"), 0644)
	}

	c.writeStateLocked(types.State{
		DocWeight:      types.WeightFeatherweight,
		DocWeightFiles: 1,
	})

	sentMsg := ""
	c.messageSender = func(msg string) { sentMsg = msg }

	state, _ := c.readState()
	changed := c.detectDocWeightChange(&state)
	if !changed {
		t.Fatal("expected change, got false")
	}
	if sentMsg == "" {
		t.Error("expected messageSender to be called")
	}
	if state.DocWeight != types.WeightMedium {
		t.Errorf("DocWeight = %q, want %q", state.DocWeight, types.WeightMedium)
	}
}

func TestHandleDocCommand_On(t *testing.T) {
	c := newTestCMonitor()
	dir, _ := os.MkdirTemp("", "doc_cmd_test_*")
	defer os.RemoveAll(dir)
	c.workDir = dir
	c.stateFile = filepath.Join(dir, "state.json")

	// Initial state
	c.writeStateLocked(types.State{DocEnabled: "auto"})

	result := c.HandleDocCommand("on")
	if result == "" {
		t.Fatal("expected non-empty result")
	}

	state, _ := c.readState()
	if state.DocEnabled != "on" {
		t.Errorf("DocEnabled = %q, want %q", state.DocEnabled, "on")
	}
}

func TestHandleDocCommand_Off(t *testing.T) {
	c := newTestCMonitor()
	dir, _ := os.MkdirTemp("", "doc_cmd_test_*")
	defer os.RemoveAll(dir)
	c.workDir = dir
	c.stateFile = filepath.Join(dir, "state.json")

	c.writeStateLocked(types.State{DocEnabled: "auto"})

	result := c.HandleDocCommand("off")
	if result == "" {
		t.Fatal("expected non-empty result")
	}

	state, _ := c.readState()
	if state.DocEnabled != "off" {
		t.Errorf("DocEnabled = %q, want %q", state.DocEnabled, "off")
	}
}

func TestHandleDocCommand_Invalid(t *testing.T) {
	c := newTestCMonitor()

	result := c.HandleDocCommand("invalid")
	if result == "" {
		t.Fatal("expected error message")
	}
	if len(result) < 10 {
		t.Errorf("result too short: %q", result)
	}
}
