package board

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"argus/internal/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestBoard(t *testing.T) (*Manager, string) {
	t.Helper()
	boardPath := filepath.Join(t.TempDir(), "board.json")
	m := NewManager(boardPath)
	return m, boardPath
}

func TestNewBoard_InitialIdleState(t *testing.T) {
	m, _ := newTestBoard(t)

	b := m.Get()
	assert.Equal(t, types.StatusIdle, b.StatusCode)
	assert.Equal(t, "idle", b.Status)
	assert.Equal(t, 0, b.CurrentStep)
	assert.Equal(t, 0, b.TotalSteps)
}

func TestUpdateTask_TransitionsToInProgress(t *testing.T) {
	m, boardPath := newTestBoard(t)

	err := m.UpdateTask("install dependencies", 5)
	require.NoError(t, err)

	b := m.Get()
	assert.Equal(t, "in_progress", b.Status)
	assert.Equal(t, "install dependencies", b.CurrentTask)
	assert.Equal(t, 5, b.TotalSteps)
	assert.Equal(t, 0, b.CurrentStep)

	assert.FileExists(t, boardPath)
}

func TestUpdateStep_IncrementsStep(t *testing.T) {
	m, _ := newTestBoard(t)
	m.UpdateTask("task", 3)

	for i := 1; i <= 3; i++ {
		err := m.UpdateStep(i)
		require.NoError(t, err)
		b := m.Get()
		assert.Equal(t, i, b.CurrentStep)
	}
}

func TestMarkDone_CompletedState(t *testing.T) {
	m, _ := newTestBoard(t)
	m.UpdateTask("task", 2)
	m.UpdateStep(2)

	err := m.MarkDone()
	require.NoError(t, err)

	b := m.Get()
	assert.Equal(t, "done", b.Status)
	assert.True(t, m.IsDone())
}

func TestMarkError_ErrorState(t *testing.T) {
	m, _ := newTestBoard(t)
	m.UpdateTask("task", 2)

	err := m.MarkError()
	require.NoError(t, err)

	b := m.Get()
	assert.Equal(t, "error", b.Status)
	assert.False(t, m.IsDone())
}

func TestReset_ClearsState(t *testing.T) {
	m, _ := newTestBoard(t)
	m.UpdateTask("task", 2)
	m.MarkDone()

	err := m.Reset()
	require.NoError(t, err)

	b := m.Get()
	assert.Equal(t, "pending", b.Status)
	assert.Empty(t, b.CurrentTask)
}

func TestString_Format(t *testing.T) {
	m, _ := newTestBoard(t)
	m.UpdateTask("compile project", 4)
	m.UpdateStep(2)

	s := m.String()
	assert.Contains(t, s, "in_progress")
	assert.Contains(t, s, "compile project")
	assert.Contains(t, s, "2/4")
}

func TestLoad_PersistsToFile(t *testing.T) {
	m, boardPath := newTestBoard(t)

	m.UpdateTask("persisted task", 7)
	m.UpdateStep(3)

	m2 := NewManager(boardPath)
	err := m2.Load()
	require.NoError(t, err)

	b := m2.Get()
	assert.Equal(t, "in_progress", b.Status)
	assert.Equal(t, "persisted task", b.CurrentTask)
	assert.Equal(t, 7, b.TotalSteps)
	assert.Equal(t, 3, b.CurrentStep)
}

func TestLoad_CreatesDefaultIfNotExists(t *testing.T) {
	_, boardPath := newTestBoard(t)
	os.Remove(boardPath)

	m := NewManager(boardPath)
	err := m.Load()
	require.NoError(t, err)

	b := m.Get()
	assert.Equal(t, types.StatusIdle, b.StatusCode)
}

func TestConcurrentUpdates_ThreadSafety(t *testing.T) {
	m, _ := newTestBoard(t)
	m.UpdateTask("concurrent task", 100)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(step int) {
			defer wg.Done()
			_ = m.UpdateStep(step)
		}(i + 1)
	}
	wg.Wait()

	b := m.Get()
	assert.Equal(t, "concurrent task", b.CurrentTask)
	assert.Equal(t, 100, b.TotalSteps)
	assert.GreaterOrEqual(t, b.CurrentStep, 1)
}

func TestLastChange_UpdatedOnEachOperation(t *testing.T) {
	m, _ := newTestBoard(t)
	before := m.Get().LastChange

	time.Sleep(10 * time.Millisecond)
	m.UpdateTask("new task", 1)

	after := m.Get().LastChange
	assert.True(t, after.After(before), "LastChange should be updated")
}

func TestBoard_CrossInstancePersistence(t *testing.T) {
	tmpDir := t.TempDir()
	boardPath := filepath.Join(tmpDir, "board.json")

	bm1 := NewManager(boardPath)
	bm1.UpdateTask("跨实例持久化", 10)
	bm1.UpdateStep(5)

	bm2 := NewManager(boardPath)
	_ = bm2.Load()
	b := bm2.Get()
	assert.Equal(t, "跨实例持久化", b.CurrentTask)
	assert.Equal(t, 10, b.TotalSteps)
	assert.Equal(t, 5, b.CurrentStep)
}
