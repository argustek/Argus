package chat

import (
	"fmt"
	"sync"
	"time"

	"argus/internal/types"
)

type RichMessageBuilder struct {
	mu       sync.Mutex
	current  *richMessageInternal
	emitFunc func(eventType string, data interface{})
}

type richMessageInternal struct {
	taskList *types.TaskList
	shells   []types.ShellBlock
	result   *types.ResultBlock
}

func NewRichMessageBuilder(emitFunc func(string, interface{})) *RichMessageBuilder {
	return &RichMessageBuilder{emitFunc: emitFunc}
}

func (b *RichMessageBuilder) StartTaskList(role, title string, taskDefs []types.TaskItemDef) string {
	b.mu.Lock()
	defer b.mu.Unlock()

	taskId := fmt.Sprintf("%s_%d", role, time.Now().UnixNano()%100000)
	items := make([]types.TaskItem, len(taskDefs))
	for i, t := range taskDefs {
		items[i] = types.TaskItem{
			ID:   fmt.Sprintf("t%d", i+1),
			Text: t.Text,
			Status: "pending",
		}
	}

	b.current = &richMessageInternal{
		taskList: &types.TaskList{
			ID:        taskId,
			Role:      role,
			Title:     title,
			Tasks:     items,
			Status:    "running",
			StartedAt: time.Now().Unix(),
		},
		shells: make([]types.ShellBlock, 0),
	}

	tasksData := make([]map[string]interface{}, len(items))
	for i, item := range items {
		tasksData[i] = map[string]interface{}{
			"id":     item.ID,
			"text":   item.Text,
			"status": item.Status,
		}
	}
	b.emitFunc("tasklist_start", map[string]interface{}{
		"roleId": role, "taskId": taskId, "title": title,
		"tasks": tasksData,
	})
	return taskId
}

func (b *RichMessageBuilder) UpdateTask(taskId string, index int, status string, detail ...string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.current == nil || b.current.taskList == nil || index >= len(b.current.taskList.Tasks) {
		return
	}
	item := &b.current.taskList.Tasks[index]
	item.Status = status
	if len(detail) > 0 && detail[0] != "" {
		item.Detail = detail[0]
	}
	now := time.Now().Unix()
	switch status {
	case "running":
		item.StartedAt = now
	case "done", "error":
		if item.StartedAt > 0 {
			item.CompletedAt = now
			item.Duration = types.FormatDuration(item.StartedAt, item.CompletedAt)
		}
	}

	updateData := map[string]interface{}{"taskId": taskId, "taskIndex": index, "status": status}
	if item.Duration != "" {
		updateData["duration"] = item.Duration
	}
	if item.Error != "" {
		updateData["error"] = item.Error
	}
	if item.Detail != "" {
		updateData["detail"] = item.Detail
	}
	b.emitFunc("tasklist_update", updateData)
}

func (b *RichMessageBuilder) PushShellStart(role, taskId string, taskIndex int, cmdType, command string, extra map[string]string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.current == nil {
		return
	}

	shell := types.ShellBlock{
		TaskID:    taskId,
		Type:      cmdType,
		Command:   command,
		Output:    "",
		Status:    "running",
		Timestamp: time.Now().Unix(),
		Extra:     extra,
	}
	b.current.shells = append(b.current.shells, shell)

	data := map[string]interface{}{
		"roleId": role, "taskId": taskId, "taskIndex": taskIndex,
		"type": cmdType, "command": command,
	}
	if extra != nil {
		data["extra"] = extra
	}
	b.emitFunc("shell_start", data)
}

func (b *RichMessageBuilder) PushShellOutput(taskId, output string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.current == nil || len(b.current.shells) == 0 {
		return
	}
	last := &b.current.shells[len(b.current.shells)-1]
	last.Output += output

	role := ""
	if b.current.taskList != nil {
		role = b.current.taskList.Role
	}
	b.emitFunc("shell_output", map[string]interface{}{
		"roleId": role, "taskId": taskId, "output": output,
	})
}

func (b *RichMessageBuilder) PushShellDone(role, taskId string, exitCode int, duration string, status string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	fmt.Printf("[RichBuilder] PushShellDone role=%s taskId=%s status=%s shells=%d\n", role, taskId, status, len(b.current.shells))
	if b.current == nil || len(b.current.shells) == 0 {
		fmt.Printf("[RichBuilder] ⚠️ PushShellDone 跳过: current=nil或shells为空\n")
		return
	}
	last := &b.current.shells[len(b.current.shells)-1]
	last.ExitCode = exitCode
	last.Duration = duration
	last.Status = status

	b.emitFunc("shell_done", map[string]interface{}{
		"roleId": role, "taskId": taskId,
		"exitCode": exitCode, "duration": duration, "status": status,
	})
}

func (b *RichMessageBuilder) CompleteTaskList(taskId, status string, result *types.ResultBlock) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.current == nil || b.current.taskList == nil {
		return
	}
	for i := range b.current.shells {
		if b.current.shells[i].Status == "running" {
			b.current.shells[i].Status = "done"
		}
	}
	b.current.taskList.Status = status
	b.current.taskList.EndedAt = time.Now().Unix()
	b.current.result = result

	completeData := map[string]interface{}{
		"taskId": taskId, "status": status,
	}
	if result != nil {
		completeData["result"] = result
	}
	b.emitFunc("tasklist_complete", completeData)
}

func (b *RichMessageBuilder) GetCurrentTaskID() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.current == nil || b.current.taskList == nil {
		return ""
	}
	return b.current.taskList.ID
}

func (b *RichMessageBuilder) GetLastShellTimestamp() int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.current == nil || len(b.current.shells) == 0 {
		return 0
	}
	return b.current.shells[len(b.current.shells)-1].Timestamp
}

func (b *RichMessageBuilder) ReplaceTaskList(taskId string, taskDefs []types.TaskItemDef) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.current == nil || b.current.taskList == nil || b.current.taskList.ID != taskId {
		return
	}
	items := make([]types.TaskItem, len(taskDefs))
	for i, t := range taskDefs {
		items[i] = types.TaskItem{
			ID:     fmt.Sprintf("t%d", i+1),
			Text:   t.Text,
			Status: "pending",
		}
	}
	b.current.taskList.Tasks = items
	tasksData := make([]map[string]interface{}, len(items))
	for i, item := range items {
		tasksData[i] = map[string]interface{}{
			"id": item.ID, "text": item.Text, "status": item.Status,
		}
	}
	b.emitFunc("tasklist_replace", map[string]interface{}{
		"taskId": taskId, "tasks": tasksData,
	})
}

func (b *RichMessageBuilder) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.current = nil
}
