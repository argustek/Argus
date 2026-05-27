package chat

import (
	"path/filepath"
	"testing"

	"argus/internal/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestConfigManager(t *testing.T) (*ConfigManager, func()) {
	t.Helper()
	tmpDir := t.TempDir()
	cm, err := NewConfigManager(tmpDir)
	require.NoError(t, err)
	return cm, func() {}
}

func TestCheckDecision_DefaultAuto_FileRead(t *testing.T) {
	cm, cleanup := newTestConfigManager(t)
	defer cleanup()

	auto, desc, err := cm.CheckDecision(types.DecisionFileRead)
	require.NoError(t, err)
	assert.True(t, auto, "file_read should be auto by default")
	assert.NotEmpty(t, desc)
}

func TestCheckDecision_DefaultManual_FileDelete(t *testing.T) {
	cm, cleanup := newTestConfigManager(t)
	defer cleanup()

	auto, desc, err := cm.CheckDecision(types.DecisionFileDelete)
	require.NoError(t, err)
	assert.False(t, auto, "file_delete should require manual confirmation")
	assert.NotEmpty(t, desc)
}

func TestCheckDecision_DefaultAuto_CmdExecute(t *testing.T) {
	cm, cleanup := newTestConfigManager(t)
	defer cleanup()

	auto, _, err := cm.CheckDecision(types.DecisionCmdExecute)
	require.NoError(t, err)
	assert.True(t, auto, "cmd_execute is auto by default")
}

func TestCheckDecision_DefaultAuto_GitOperate(t *testing.T) {
	cm, cleanup := newTestConfigManager(t)
	defer cleanup()

	auto, _, err := cm.CheckDecision(types.DecisionGitOperate)
	require.NoError(t, err)
	assert.True(t, auto, "git_operate is auto by default")
}

func TestCheckPermission_DefaultFullAccess_WorkDir(t *testing.T) {
	cm, cleanup := newTestConfigManager(t)
	defer cleanup()

	level, _, allowed := cm.CheckPermission("write", filepath.Join(cm.workDir, "main.go"))
	assert.Equal(t, types.PermFullAccess, level)
	assert.True(t, allowed)
}

func TestPermission_GitDirectory_ReadWrite_DeleteBlocked(t *testing.T) {
	cm, cleanup := newTestConfigManager(t)
	defer cleanup()

	level, _, allowed := cm.CheckPermission("delete", filepath.Join(cm.workDir, ".git", "config"))
	assert.Equal(t, types.PermReadWrite, level)
	assert.False(t, allowed, ".git delete should be blocked (readwrite allows read/write but not delete)")
}

func TestPermission_Protected_GitDirectory_ReadAllowed(t *testing.T) {
	cm, cleanup := newTestConfigManager(t)
	defer cleanup()

	_, _, allowed := cm.CheckPermission("read", filepath.Join(cm.workDir, ".git", "HEAD"))
	assert.True(t, allowed, ".git read should be allowed")
}

func TestUpdateDecisionRule(t *testing.T) {
	cm, cleanup := newTestConfigManager(t)
	defer cleanup()

	err := cm.UpdateDecisionRule(types.DecisionCmdExecute, types.DecisionAuto)
	require.NoError(t, err)

	auto, _, err := cm.CheckDecision(types.DecisionCmdExecute)
	require.NoError(t, err)
	assert.True(t, auto, "cmd_execute now auto after update")
}

func TestAddPathRule_CustomProtection(t *testing.T) {
	cm, cleanup := newTestConfigManager(t)
	defer cleanup()

	rule := types.PathRule{
		PathPattern: "*.env",
		Permission:  types.PermProtected,
		Priority:    1,
	}
	err := cm.AddPathRule(rule)
	require.NoError(t, err)

	level, _, allowed := cm.CheckPermission("write", filepath.Join(cm.workDir, ".env"))
	assert.Equal(t, types.PermProtected, level)
	assert.False(t, allowed, ".env write should be blocked after adding protected rule")
}

func TestRemovePathRule(t *testing.T) {
	cm, cleanup := newTestConfigManager(t)
	defer cleanup()

	rule := types.PathRule{
		PathPattern: "secrets/**",
		Permission:  types.PermNoAccess,
		Priority:    0,
	}
	cm.AddPathRule(rule)

	err := cm.RemovePathRule("secrets/**")
	require.NoError(t, err)

	level, _, _ := cm.CheckPermission("write", filepath.Join(cm.workDir, "secrets", "key.txt"))
	assert.NotEqual(t, types.PermNoAccess, level)
}

func TestGetConfigStatus(t *testing.T) {
	cm, cleanup := newTestConfigManager(t)
	defer cleanup()

	status := cm.GetConfigStatus()
	assert.True(t, status["initialized"].(bool))
	assert.NotNil(t, status["decision"])
	assert.NotNil(t, status["permission"])
}

func TestSaveAndReload_DecisionConfig(t *testing.T) {
	tmpDir := t.TempDir()
	cm, err := NewConfigManager(tmpDir)
	require.NoError(t, err)

	err = cm.UpdateDecisionRule(types.DecisionFileWrite, types.DecisionAuto)
	require.NoError(t, err)

	configPath := filepath.Join(tmpDir, ".argus", "decision_config.json")
	assert.FileExists(t, configPath)

	cm2, err := NewConfigManager(tmpDir)
	require.NoError(t, err)

	auto, _, err := cm2.CheckDecision(types.DecisionFileWrite)
	require.NoError(t, err)
	assert.True(t, auto, "persisted decision should survive reload")
}

func TestMatchPathPattern_WildcardGo(t *testing.T) {
	cm, cleanup := newTestConfigManager(t)
	defer cleanup()

	assert.True(t, cm.matchPathPattern("*.go", "main.go"))
	assert.True(t, cm.matchPathPattern("*.go", "utils/helper.go"))
	assert.False(t, cm.matchPathPattern("*.go", "main.py"))
	assert.True(t, cm.matchPathPattern("*.go", "MAIN.GO"))
}

func TestMatchPathPattern_DoubleStar(t *testing.T) {
	cm, cleanup := newTestConfigManager(t)
	defer cleanup()

	assert.True(t, cm.matchPathPattern(".git/**", ".git/config"))
	assert.True(t, cm.matchPathPattern(".git/**", ".git/objects/pack/file"))
	assert.False(t, cm.matchPathPattern(".git/**", "src/main.go"))
}

func TestResetDecisionToDefault(t *testing.T) {
	cm, cleanup := newTestConfigManager(t)
	defer cleanup()

	cm.UpdateDecisionRule(types.DecisionFileDelete, types.DecisionManual)

	err := cm.ResetDecisionToDefault()
	require.NoError(t, err)

	auto, _, err := cm.CheckDecision(types.DecisionFileDelete)
	require.NoError(t, err)
	assert.True(t, auto, "file_delete should be auto (default) after reset")
}
