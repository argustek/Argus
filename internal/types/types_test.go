package types

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetDefaultDecisionConfig(t *testing.T) {
	cfg := GetDefaultDecisionConfig()
	assert.Equal(t, 1, cfg.Version)
	assert.Len(t, cfg.Rules, 9)
	assert.False(t, cfg.UpdatedAt.IsZero())
}

func TestGetDecisionMode(t *testing.T) {
	cfg := GetDefaultDecisionConfig()

	mode := cfg.GetDecisionMode(DecisionFileRead)
	assert.Equal(t, DecisionAuto, mode)

	mode = cfg.GetDecisionMode(DecisionFileWrite)
	assert.Equal(t, DecisionAuto, mode)

	mode = cfg.GetDecisionMode("unknown_type")
	assert.Equal(t, DecisionManual, mode)
}

func TestGetDefaultPermissionConfig(t *testing.T) {
	cfg := GetDefaultPermissionConfig("/work")
	assert.Equal(t, 1, cfg.Version)
	assert.Equal(t, PermFullAccess, cfg.DefaultPermission)
	assert.True(t, len(cfg.Rules) > 0)
}

func TestGetDefaultCommandPolicy(t *testing.T) {
	cp := GetDefaultCommandPolicy()
	assert.Equal(t, 1, cp.Version)
	assert.True(t, len(cp.Rules) > 0)
}

func TestCheckCommand_DenyBlocked(t *testing.T) {
	cp := GetDefaultCommandPolicy()

	level, desc := cp.CheckCommand("format D:")
	assert.Equal(t, CmdBlockDeny, level)
	assert.Contains(t, desc, "格式化")

	level, desc = cp.CheckCommand("diskpart list disk")
	assert.Equal(t, CmdBlockDeny, level)

	level, desc = cp.CheckCommand("bcdedit /set")
	assert.Equal(t, CmdBlockDeny, level)

	level, desc = cp.CheckCommand("net user add bob")
	assert.Equal(t, CmdBlockDeny, level)
}

func TestCheckCommand_AllowSafe(t *testing.T) {
	cp := GetDefaultCommandPolicy()

	level, desc := cp.CheckCommand("go build")
	assert.Equal(t, CmdBlockAllow, level)
	assert.Empty(t, desc)

	level, desc = cp.CheckCommand("npm install")
	assert.Equal(t, CmdBlockAllow, level)

	level, desc = cp.CheckCommand("git commit -m 'fix'")
	assert.Equal(t, CmdBlockAllow, level)
}

func TestMatchCommandPattern(t *testing.T) {
	assert.True(t, matchCommandPattern("format *", "format D:"))
	assert.True(t, matchCommandPattern("format *", "format /q E:"))
	assert.False(t, matchCommandPattern("format *", "go format"))

	assert.True(t, matchCommandPattern("diskpart *", "diskpart list disk"))
	assert.True(t, matchCommandPattern("diskpart *", "diskpart list disk"))

	assert.True(t, matchCommandPattern("reg delete HKCR *", "reg delete HKCR /f"))
	assert.False(t, matchCommandPattern("reg delete HKCR *", "reg query HKCR"))
}

func TestDefaultEnvMemory(t *testing.T) {
	mem := GetDefaultEnvMemory()
	assert.Equal(t, 1, mem.Version)
	assert.Empty(t, mem.Tools)
	assert.Empty(t, mem.Configs)
}

func TestStateConstants(t *testing.T) {
	assert.Equal(t, 0, StatusIdle)
	assert.Equal(t, 1, StatusRunning)
	assert.Equal(t, 2, StatusCompleted)
	assert.Equal(t, 3, StatusError)

	assert.Equal(t, 0, ProjectStateIdle)
	assert.Equal(t, 1, ProjectStateRunning)
	assert.Equal(t, 2, ProjectStateDone)
	assert.Equal(t, 3, ProjectStateApproved)
	assert.Equal(t, 4, ProjectStateError)
}

func TestBoardDefaultValues(t *testing.T) {
	b := Board{
		TaskID:      "test-1",
		Description: "Test task",
		StatusCode:  StatusRunning,
		Status:      "running",
		AssignedTo:  "se",
	}
	assert.Equal(t, "test-1", b.TaskID)
	assert.Equal(t, "running", b.Status)
	assert.Equal(t, "se", b.AssignedTo)
}

func TestMessageStructure(t *testing.T) {
	now := time.Now()
	m := Message{
		ID:        "msg-1",
		Role:      "user",
		Content:   "hello",
		Timestamp: now,
	}
	assert.Equal(t, "msg-1", m.ID)
	assert.Equal(t, "user", m.Role)
	assert.Equal(t, "hello", m.Content)
}

func TestGlobalTaskLifecycle(t *testing.T) {
	now := time.Now()
	task := &GlobalTask{
		ID:          "task-1",
		Description: "write hello.go",
		Role:        "SE",
		Status:      "pending",
		CreatedAt:   now,
	}

	assert.Equal(t, "pending", task.Status)
	assert.Nil(t, task.CompletedAt)

	completedAt := time.Now()
	task.Status = "done"
	task.CompletedAt = &completedAt
	assert.Equal(t, "done", task.Status)
	require.NotNil(t, task.CompletedAt)
	assert.True(t, task.CompletedAt.Equal(completedAt))
}

func TestAPStatusConstants(t *testing.T) {
	assert.Equal(t, "ap_idle", APStatusIdle)
	assert.Equal(t, "ap_reviewing", APStatusReviewing)
	assert.Equal(t, "ap_approved", APStatusApproved)
	assert.Equal(t, "ap_rejected", APStatusRejected)
}

func TestRoleStatusConstants(t *testing.T) {
	assert.Equal(t, "busy", RoleStatusBusy)
	assert.Equal(t, "idle", RoleStatusIdle)
}

func TestDecisionTypeConstants(t *testing.T) {
	assert.Equal(t, DecisionType("file_read"), DecisionFileRead)
	assert.Equal(t, DecisionType("file_write"), DecisionFileWrite)
	assert.Equal(t, DecisionType("file_modify"), DecisionFileModify)
	assert.Equal(t, DecisionType("cmd_execute"), DecisionCmdExecute)
	assert.Equal(t, DecisionType("git_operate"), DecisionGitOperate)
}

func TestPermissionLevelConstants(t *testing.T) {
	assert.Equal(t, PermissionLevel("full"), PermFullAccess)
	assert.Equal(t, PermissionLevel("readwrite"), PermReadWrite)
	assert.Equal(t, PermissionLevel("readonly"), PermReadOnly)
	assert.Equal(t, PermissionLevel("none"), PermNoAccess)
	assert.Equal(t, PermissionLevel("protected"), PermProtected)
}

func TestCommandBlockLevelConstants(t *testing.T) {
	assert.Equal(t, CommandBlockLevel("deny"), CmdBlockDeny)
	assert.Equal(t, CommandBlockLevel("ask"), CmdBlockAsk)
	assert.Equal(t, CommandBlockLevel("allow"), CmdBlockAllow)
}

func TestToolInfoDefaults(t *testing.T) {
	info := ToolInfo{
		Path:      "/usr/bin/go",
		FirstSeen: time.Now(),
		Source:    "learned",
	}
	assert.Equal(t, "/usr/bin/go", info.Path)
	assert.Equal(t, "learned", info.Source)
	assert.Equal(t, 0, info.UseCount)
}
