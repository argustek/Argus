package task

import (
	"crypto/rand"
	"fmt"
	"sync"
	"time"

	"argus/internal/types"
)

type EventCallback func(eventName string, data interface{})

type TaskManager struct {
	mu          sync.RWMutex
	tasks       map[string]*types.GlobalTask
	emitFn      EventCallback
	LogFn       func(format string, args ...interface{})
	lastTaskIDs map[string]string // role -> last task id
}

func NewTaskManager(emitFn EventCallback) *TaskManager {
	return &TaskManager{
		tasks:       make(map[string]*types.GlobalTask),
		emitFn:      emitFn,
		lastTaskIDs: make(map[string]string),
	}
}

func (tm *TaskManager) CreateTask(description, role string) *types.GlobalTask {
	id := generateUUID()
	now := time.Now()
	task := &types.GlobalTask{
		ID:          id,
		Description: description,
		Role:        role,
		Status:      "pending",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	tm.mu.Lock()
	tm.tasks[id] = task
	tm.lastTaskIDs[role] = id
	tm.mu.Unlock()
	tm.log("[TASK-MANAGER-1] CreateTask: id=%s desc=%q role=%s", id, description, role)
	tm.emit("task_added", task)
	return task
}

func (tm *TaskManager) SetEmitFn(fn EventCallback) {
	tm.emitFn = fn
}

func (tm *TaskManager) log(format string, args ...interface{}) {
	if tm.LogFn != nil {
		tm.LogFn(format, args...)
	}
}

func (tm *TaskManager) GetTask(id string) (*types.GlobalTask, bool) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	t, ok := tm.tasks[id]
	return t, ok
}

func (tm *TaskManager) GetAllTasks() []*types.GlobalTask {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	result := make([]*types.GlobalTask, 0, len(tm.tasks))
	for _, t := range tm.tasks {
		result = append(result, t)
	}
	return result
}

func (tm *TaskManager) UpdateStatus(id, status string) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	t, ok := tm.tasks[id]
	if !ok {
		tm.log("[TASK-MANAGER-2] UpdateStatus: task not found id=%s status=%s", id, status)
		return
	}
	t.Status = status
	t.UpdatedAt = time.Now()
	if status == "done" || status == "failed" {
		now := time.Now()
		t.CompletedAt = &now
	}
	tm.log("[TASK-MANAGER-2] UpdateStatus: id=%s desc=%q status=%s", id, t.Description, status)
	tm.emit("task_updated", t)
}

func (tm *TaskManager) DeleteTask(id string) {
	tm.mu.Lock()
	delete(tm.tasks, id)
	tm.mu.Unlock()
	tm.emit("task_deleted", map[string]string{"id": id})
}

func (tm *TaskManager) ClearDone() {
	tm.mu.Lock()
	for id, t := range tm.tasks {
		if t.Status == "done" || t.Status == "failed" {
			delete(tm.tasks, id)
		}
	}
	tm.mu.Unlock()
	tm.emit("tasks_cleared", nil)
}

func (tm *TaskManager) ClearTasks() {
	tm.mu.Lock()
	tm.tasks = make(map[string]*types.GlobalTask)
	tm.mu.Unlock()
	tm.log("[TASK-MANAGER] ClearTasks: all tasks cleared")
	tm.emit("tasks_cleared", nil)
}

// CompleteLastTaskByRole 标记指定角色的最后一个任务为完成
func (tm *TaskManager) CompleteLastTaskByRole(role string) {
	tm.mu.Lock()
	id, ok := tm.lastTaskIDs[role]
	if !ok {
		tm.mu.Unlock()
		return
	}
	task, exists := tm.tasks[id]
	if !exists {
		tm.mu.Unlock()
		return
	}
	task.Status = "done"
	now := time.Now()
	task.UpdatedAt = now
	task.CompletedAt = &now
	tm.mu.Unlock()
	tm.log("[TASK-MANAGER] CompleteLastTaskByRole: role=%s id=%s", role, id)
	tm.emit("task_updated", task)
}

func (tm *TaskManager) emit(name string, data interface{}) {
	if tm.emitFn != nil {
		if task, ok := data.(*types.GlobalTask); ok {
			payload := map[string]interface{}{
				"id":          task.ID,
				"description": task.Description,
				"role":        task.Role,
				"status":      task.Status,
				"progress":    task.Progress,
				"createdAt":   task.CreatedAt.Format(time.RFC3339),
				"updatedAt":   task.UpdatedAt.Format(time.RFC3339),
			}
			fmt.Printf("[TASK-MANAGER] 📤 emit %s: id=%s desc=%q role=%s status=%s\n", name, task.ID, task.Description, task.Role, task.Status)
			tm.emitFn(name, payload)
			return
		}
		tm.emitFn(name, data)
	}
}

var ErrTaskNotFound = fmt.Errorf("task not found")

func generateUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}