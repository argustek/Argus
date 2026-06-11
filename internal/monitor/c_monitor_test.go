package monitor

import (
	"os"
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
