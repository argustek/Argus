package chat

import (
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"argus/internal/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestManager(t *testing.T) (*Manager, func()) {
	t.Helper()
	tmpDir := t.TempDir()
	config := types.Config{
		APIConfig: types.APIConfig{
			Provider: "test",
			BaseURL:  "http://localhost:0",
			APIKey:   "test-key",
			Model:    "test-model",
		},
	}
	m, err := NewManager(config, tmpDir)
	require.NoError(t, err)
	return m, func() {}
}

func TestProcessMessage_ConcurrentRejection(t *testing.T) {
	m, cleanup := newTestManager(t)
	defer cleanup()

	m.isProcessing = true

	result, err := m.ProcessMessage("hello")
	assert.NoError(t, err)
	assert.Contains(t, result, "仍在处理中")
}

func TestProcessMessage_StopCommand(t *testing.T) {
	m, cleanup := newTestManager(t)
	defer cleanup()

	result, err := m.ProcessMessage("stop")
	assert.NoError(t, err)
	assert.Contains(t, result, "已停止")
	assert.True(t, m.IsUserStopped())
}

func TestProcessMessage_EmptyStops(t *testing.T) {
	m, cleanup := newTestManager(t)
	defer cleanup()

	result, err := m.ProcessMessage("")
	assert.NoError(t, err)
	assert.Contains(t, result, "已停止")
	assert.True(t, m.IsUserStopped())
}

func TestProcessMessage_ChineseStop(t *testing.T) {
	m, cleanup := newTestManager(t)
	defer cleanup()

	result, err := m.ProcessMessage("停止")
	assert.NoError(t, err)
	assert.Contains(t, result, "已停止")
	assert.True(t, m.IsUserStopped())
}

func TestStopCurrentTask_ResetsState(t *testing.T) {
	m, cleanup := newTestManager(t)
	defer cleanup()

	m.isProcessing = true
	m.currentRole = "se"
	m.seContinueCount = 5
	m.seReportedComplete = false

	m.StopCurrentTask()

	assert.False(t, m.isProcessing)
	assert.Equal(t, "", m.currentRole)
	assert.Equal(t, 0, m.seContinueCount)
	assert.False(t, m.seReportedComplete)
}

func TestStopCurrentTask_WithCancelFunc(t *testing.T) {
	m, cleanup := newTestManager(t)
	defer cleanup()

	cancelled := false
	m.mu.Lock()
	m.cancelFunc = func() {
		cancelled = true
	}
	m.mu.Unlock()

	m.StopCurrentTask()

	assert.True(t, cancelled, "cancelFunc should have been called")
	m.mu.RLock()
	nilFunc := m.cancelFunc == nil
	m.mu.RUnlock()
	assert.True(t, nilFunc, "cancelFunc should be nil after stop")
}

func TestProcessingLock_PreventsConcurrentMessages(t *testing.T) {
	m, cleanup := newTestManager(t)
	defer cleanup()

	var wg sync.WaitGroup
	results := make(chan string, 2)

	m.isProcessing = true

	wg.Add(1)
	go func() {
		defer wg.Done()
		result, _ := m.ProcessMessage("first")
		results <- result
	}()

	time.Sleep(50 * time.Millisecond)

	wg.Add(1)
	go func() {
		defer wg.Done()
		result, _ := m.ProcessMessage("second")
		results <- result
	}()

	wg.Wait()
	close(results)

	rejectionCount := 0
	for r := range results {
		if strings.Contains(r, "仍在处理中") {
			rejectionCount++
		}
	}
	assert.Equal(t, 2, rejectionCount, "both concurrent messages should be rejected when processing")
}

func TestSetUserStopped_True(t *testing.T) {
	m, cleanup := newTestManager(t)
	defer cleanup()

	m.SetUserStopped(true)
	assert.True(t, m.IsUserStopped())
}

func TestSetUserStopped_False(t *testing.T) {
	m, cleanup := newTestManager(t)
	defer cleanup()

	m.SetUserStopped(true)
	m.SetUserStopped(false)
	assert.False(t, m.IsUserStopped())
}

func TestGetExecutionStatus(t *testing.T) {
	m, cleanup := newTestManager(t)
	defer cleanup()

	pmBusy, seRunning := m.GetExecutionStatus()
	assert.False(t, pmBusy)
	assert.False(t, seRunning)

	m.currentRole = "pm"
	pmBusy, seRunning = m.GetExecutionStatus()
	assert.True(t, pmBusy)
	assert.False(t, seRunning)

	m.currentRole = "se"
	pmBusy, seRunning = m.GetExecutionStatus()
	assert.False(t, pmBusy)
	assert.True(t, seRunning)
}

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s[1:], substr))
}

func TestNewManager_ValidConfig(t *testing.T) {
	tmpDir := t.TempDir()
	config := types.Config{
		APIConfig: types.APIConfig{
			Provider: "test",
			BaseURL:  "http://localhost:0",
			APIKey:   "test-key",
			Model:    "test-model",
		},
	}
	m, err := NewManager(config, tmpDir)
	require.NoError(t, err)
	require.NotNil(t, m)
	assert.Equal(t, tmpDir, m.workDir)
	assert.False(t, m.isProcessing)
	assert.False(t, m.userStopped)
}

func TestNewManager_BoardFileCreated(t *testing.T) {
	tmpDir := t.TempDir()
	config := types.Config{
		APIConfig: types.APIConfig{
			Provider: "test",
			BaseURL:  "http://localhost:0",
			APIKey:   "test-key",
			Model:    "test-model",
		},
	}
	_, err := NewManager(config, tmpDir)
	require.NoError(t, err)

	boardPath := filepath.Join(tmpDir, ".argus", "board.json")
	_, err = filepath.Glob(boardPath)
	assert.NoError(t, err)
}
