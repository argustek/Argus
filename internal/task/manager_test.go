package task

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestManager() *TaskManager {
	return NewTaskManager(nil)
}

func TestCreateTask(t *testing.T) {
	tm := newTestManager()
	task := tm.CreateTask("write hello.go", "SE")
	require.NotNil(t, task)
	assert.NotEmpty(t, task.ID)
	assert.Equal(t, "write hello.go", task.Description)
	assert.Equal(t, "SE", task.Role)
	assert.Equal(t, "pending", task.Status)
	assert.False(t, task.CreatedAt.IsZero())
}

func TestGetTask(t *testing.T) {
	tm := newTestManager()
	created := tm.CreateTask("test task", "PM")

	found, ok := tm.GetTask(created.ID)
	assert.True(t, ok)
	assert.Equal(t, created.ID, found.ID)
	assert.Equal(t, "test task", found.Description)

	_, ok = tm.GetTask("nonexistent")
	assert.False(t, ok)
}

func TestGetAllTasks(t *testing.T) {
	tm := newTestManager()
	assert.Empty(t, tm.GetAllTasks())

	tm.CreateTask("task 1", "PM")
	tm.CreateTask("task 2", "SE")
	tm.CreateTask("task 3", "AP")

	all := tm.GetAllTasks()
	assert.Len(t, all, 3)
}

func TestUpdateStatus(t *testing.T) {
	tm := newTestManager()
	task := tm.CreateTask("test", "SE")

	tm.UpdateStatus(task.ID, "doing")
	updated, _ := tm.GetTask(task.ID)
	assert.Equal(t, "doing", updated.Status)
	assert.Nil(t, updated.CompletedAt)

	tm.UpdateStatus(task.ID, "done")
	done, _ := tm.GetTask(task.ID)
	assert.Equal(t, "done", done.Status)
	require.NotNil(t, done.CompletedAt)
	assert.False(t, done.CompletedAt.IsZero())
}

func TestUpdateStatus_NonexistentTask(t *testing.T) {
	tm := newTestManager()
	tm.UpdateStatus("bad-id", "done")
}

func TestDeleteTask(t *testing.T) {
	tm := newTestManager()
	task := tm.CreateTask("to delete", "SE")

	_, ok := tm.GetTask(task.ID)
	assert.True(t, ok)

	tm.DeleteTask(task.ID)

	_, ok = tm.GetTask(task.ID)
	assert.False(t, ok)
}

func TestClearDone(t *testing.T) {
	tm := newTestManager()
	t1 := tm.CreateTask("done task", "SE")
	t2 := tm.CreateTask("pending task", "SE")
	t3 := tm.CreateTask("failed task", "SE")

	tm.UpdateStatus(t1.ID, "done")
	tm.UpdateStatus(t3.ID, "failed")

	tm.ClearDone()

	assert.Len(t, tm.GetAllTasks(), 1)
	remaining, ok := tm.GetTask(t2.ID)
	assert.True(t, ok)
	assert.Equal(t, "pending", remaining.Status)
}

func TestClearTasks(t *testing.T) {
	tm := newTestManager()
	tm.CreateTask("task 1", "PM")
	tm.CreateTask("task 2", "SE")

	assert.Len(t, tm.GetAllTasks(), 2)

	tm.ClearTasks()
	assert.Empty(t, tm.GetAllTasks())
}

func TestCompleteLastTaskByRole(t *testing.T) {
	tm := newTestManager()

	tm.CreateTask("first", "PM")
	task := tm.CreateTask("last", "PM")

	tm.CompleteLastTaskByRole("PM")

	completed, ok := tm.GetTask(task.ID)
	require.True(t, ok)
	assert.Equal(t, "done", completed.Status)
}

func TestCompleteLastTaskByRole_NoTask(t *testing.T) {
	tm := newTestManager()
	tm.CompleteLastTaskByRole("AP")
}

func TestEventCallback(t *testing.T) {
	events := make([]string, 0)
	var mu sync.Mutex

	tm := NewTaskManager(func(name string, data interface{}) {
		mu.Lock()
		events = append(events, name)
		mu.Unlock()
	})

	tm.CreateTask("test", "SE")
	tm.UpdateStatus("nonexistent", "done")
	tm.CreateTask("another", "PM")
	tm.DeleteTask("nonexistent")

	mu.Lock()
	assert.NotEmpty(t, events)
	mu.Unlock()
}

func TestSetEmitFn(t *testing.T) {
	called := false
	tm := newTestManager()

	fn := func(name string, data interface{}) {
		called = true
	}

	tm.SetEmitFn(fn)
	tm.CreateTask("test", "SE")

	assert.True(t, called)
}

func TestConcurrentAccess(t *testing.T) {
	tm := newTestManager()
	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			task := tm.CreateTask("concurrent task", "SE")
			tm.UpdateStatus(task.ID, "doing")
			tm.UpdateStatus(task.ID, "done")
		}(i)
	}

	wg.Wait()
	assert.Len(t, tm.GetAllTasks(), 10)
}

func TestUUIDFormat(t *testing.T) {
	id := generateUUID()
	assert.Len(t, id, 36)
	assert.Contains(t, id, "-")
}

func TestErrTaskNotFound(t *testing.T) {
	assert.Equal(t, "task not found", ErrTaskNotFound.Error())
}
