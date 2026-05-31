package core

import (
	"fmt"
	"sync"
	"time"
)

// TodoStatus 任务状态
type TodoStatus string

const (
	TodoPending TodoStatus = "pending" // 待处理
	TodoDoing  TodoStatus = "doing"   // 进行中
	TodoDone   TodoStatus = "done"    // 已完成
	TodoError  TodoStatus = "error"   // 出错
)

// TodoItem 单个待办项
type TodoItem struct {
	ID          string      `json:"id"`
	Description string      `json:"description"`
	Status      TodoStatus  `json:"status"`
	Priority    int         `json:"priority"` // 1=高, 2=中, 3=低
	CreatedAt   time.Time   `json:"createdAt"`
	UpdatedAt   time.Time   `json:"updatedAt"`
	Phase       string      `json:"phase"`     // 所属阶段: pm/se/review/ap
	Progress    int         `json:"progress"`  // 进度百分比 0-100
}

// TodoEvent Todo更新事件（发送到前端）
type TodoEvent struct {
	Action string     `json:"action"` // "set"|"update"|"clear"
	Items  []TodoItem `json:"items"`
}

// TodoManager 动态任务列表管理器（Message Bus驱动）
type TodoManager struct {
	mu       sync.RWMutex
	items    []TodoItem
	onUpdate func(TodoEvent) // 回调：通知MessageBus发送事件
	seqNum   int             // 序列号生成器
}

// NewTodoManager 创建Todo管理器
func NewTodoManager() *TodoManager {
	return &TodoManager{
		items: make([]TodoItem, 0),
		seqNum: 0,
	}
}

// SetOnUpdate 设置更新回调（连接到MessageBus）
func (tm *TodoManager) SetOnUpdate(fn func(TodoEvent)) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.onUpdate = fn
}

// generateId 生成唯一ID
func (tm *TodoManager) generateId() string {
	tm.seqNum++
	now := time.Now().Format("150405")
	return fmt.Sprintf("todo_%s_%d", now, tm.seqNum)
}

// SetTasks 设置完整任务列表（PM分解任务时调用）
func (tm *TodoManager) SetTasks(tasks []string, phase string) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	newItems := make([]TodoItem, 0, len(tasks))
	for i, desc := range tasks {
		item := TodoItem{
			ID:          tm.generateId(),
			Description: desc,
			Status:      TodoPending,
			Priority:    i + 1,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
			Phase:       phase,
			Progress:    0,
		}
		newItems = append(newItems, item)
	}

	tm.items = newItems
	tm.emitEvent("set")
	fmt.Printf("[✅TODO] Set %d tasks for phase=%s\n", len(tasks), phase)
}

// UpdateStatus 更新单个任务状态
func (tm *TodoManager) UpdateStatus(id string, status TodoStatus) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	for i := range tm.items {
		if tm.items[i].ID == id {
			oldStatus := tm.items[i].Status
			tm.items[i].Status = status
			tm.items[i].UpdatedAt = time.Now()

			if status == TodoDone {
				tm.items[i].Progress = 100
			} else if status == TodoDoing {
				tm.items[i].Progress = 50
			}

			fmt.Printf("[🔄TODO] id=%s %s → %s\n", id, oldStatus, status)
			break
		}
	}

	tm.emitEvent("update")
}

// UpdateByPhase 按阶段批量更新状态
func (tm *TodoManager) UpdateByPhase(phase string, status TodoStatus) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	count := 0
	for i := range tm.items {
		if tm.items[i].Phase == phase && tm.items[i].Status != TodoDone {
			tm.items[i].Status = status
			tm.items[i].UpdatedAt = time.Now()

			if status == TodoDone {
				tm.items[i].Progress = 100
				count++
			} else if status == TodoDoing {
				tm.items[i].Progress = 50
			}
		}
	}

	if count > 0 {
		fmt.Printf("[🔄TODO] Batch update phase=%s status=%s count=%d\n", phase, status, count)
		tm.emitEvent("update")
	}
}

// MarkCurrentDoing 标记当前阶段第一个pending任务为doing
func (tm *TodoManager) MarkCurrentDoing() {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	for i := range tm.items {
		if tm.items[i].Status == TodoPending {
			tm.items[i].Status = TodoDoing
			tm.items[i].UpdatedAt = time.Now()
			tm.items[i].Progress = 50
			fmt.Printf("[▶️TODO] Start: id=%s desc=%q\n", tm.items[i].ID, tm.items[i].Description)
			tm.emitEvent("update")
			return
		}
	}
}

// CompleteCurrent 完成当前doing任务
func (tm *TodoManager) CompleteCurrent() {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	for i := range tm.items {
		if tm.items[i].Status == TodoDoing {
			tm.items[i].Status = TodoDone
			tm.items[i].UpdatedAt = time.Now()
			tm.items[i].Progress = 100
			fmt.Printf("[✅TODO] Complete: id=%s desc=%q\n", tm.items[i].ID, tm.items[i].Description)
			tm.emitEvent("update")
			return
		}
	}
}

// AddTask 动态添加新任务（SE重试、错误恢复等场景）
func (tm *TodoManager) AddTask(description string, phase string, priority int) string {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	item := TodoItem{
		ID:          tm.generateId(),
		Description: description,
		Status:      TodoPending,
		Priority:    priority,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		Phase:       phase,
		Progress:    0,
	}

	tm.items = append(tm.items, item)
	fmt.Printf("[➕TODO] Add: id=%s desc=%q phase=%s\n", item.ID, description, phase)
	tm.emitEvent("update")

	return item.ID
}

// GetItems 获取当前所有任务（快照）
func (tm *TodoManager) GetItems() []TodoItem {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	items := make([]TodoItem, len(tm.items))
	copy(items, tm.items)
	return items
}

// Clear 清空所有任务（新任务开始时调用）
func (tm *TodoManager) Clear() {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	tm.items = make([]TodoItem, 0)
	tm.emitEvent("clear")
	fmt.Println("[🧹TODO] Cleared all tasks")
}

// GetStats 获取统计信息
func (tm *TodoManager) GetStats() map[string]interface{} {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	pending := 0
	doing := 0
	done := 0
	error := 0

	for _, item := range tm.items {
		switch item.Status {
		case TodoPending:
			pending++
		case TodoDoing:
			doing++
		case TodoDone:
			done++
		case TodoError:
			error++
		}
	}

	return map[string]interface{}{
		"total":   len(tm.items),
		"pending": pending,
		"doing":   doing,
		"done":    done,
		"error":   error,
	}
}

// emitEvent 触发更新事件
func (tm *TodoManager) emitEvent(action string) {
	if tm.onUpdate == nil {
		return
	}

	items := make([]TodoItem, len(tm.items))
	copy(items, tm.items)

	event := TodoEvent{
		Action: action,
		Items:  items,
	}

	go tm.onUpdate(event) // 异步发送，不阻塞主流程
}