package chat

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"argus/internal/types"
	"argus/internal/i18n"
)

const (
	memoryFileName    = "task_memory.json"
	maxRecentMessages = 20 // 保存最近20条消息
)

// MemoryManager 任务记忆管理器
type MemoryManager struct {
	workDir     string
	memoryPath  string
	mu          sync.RWMutex
	lastMemory  *types.TaskMemory
	stopped     bool // 用户主动停止标志（防止autosave重新创建文件）
	stopCh      chan struct{} // goroutine 停止信号
}

// NewMemoryManager 创建记忆管理器
func NewMemoryManager(workDir string) *MemoryManager {
	return &MemoryManager{
		workDir:    workDir,
		memoryPath: filepath.Join(workDir, ".argus", memoryFileName),
		stopCh:     make(chan struct{}),
	}
}

// SaveState 保存当前状态
func (mm *MemoryManager) SaveState(userRequest, currentState, currentRole, taskDescription string, messages []types.Message) error {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	recentMessages := messages
	if len(recentMessages) > maxRecentMessages {
		recentMessages = messages[len(messages)-maxRecentMessages:]
	}

	// 提取任务ID：使用第一条用户消息的ID
	taskID := extractTaskID(messages)

	// 检测是否是新任务（与已保存的记忆对比）
	oldTaskID := ""
	if mm.lastMemory != nil && mm.lastMemory.TaskID != "" {
		oldTaskID = mm.lastMemory.TaskID
	}

	if oldTaskID != "" && taskID != "" && oldTaskID != taskID {
		fmt.Printf("[Memory] 🔄 检测到新任务: 旧任务=%s → 新任务=%s\n", oldTaskID, taskID)
	}

	memory := &types.TaskMemory{
		TaskID:          taskID,
		UserRequest:     userRequest,
		CurrentState:    currentState,
		CurrentRole:     currentRole,
		TaskDescription: taskDescription,
		RecentMessages:  recentMessages,
		LastActiveTime:  time.Now(),
		MessageCount:    len(messages),
	}

	mm.trimToLastTasks(memory, 1)

	data, err := json.MarshalIndent(memory, "", "  ")
	if err != nil {
		return fmt.Errorf(i18n.T("err.memory_serialize"), err)
	}

	dir := filepath.Dir(mm.memoryPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf(i18n.T("err.memory_mkdir"), err)
	}

	if err := os.WriteFile(mm.memoryPath, data, 0644); err != nil {
		return fmt.Errorf(i18n.T("err.memory_write"), err)
	}

	mm.lastMemory = memory
	fmt.Printf("[Memory] 状态已保存: state=%s, role=%s, messages=%d\n", currentState, currentRole, len(recentMessages))
	return nil
}

// LoadState 加载状态
func (mm *MemoryManager) LoadState() (*types.TaskMemory, error) {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	data, err := os.ReadFile(mm.memoryPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // 没有记忆文件，不是错误
		}
		return nil, fmt.Errorf(i18n.T("err.memory_read"), err)
	}

	var memory types.TaskMemory
	if err := json.Unmarshal(data, &memory); err != nil {
		return nil, fmt.Errorf(i18n.T("err.memory_parse"), err)
	}

	mm.lastMemory = &memory
	taskIDLog := ""
	if memory.TaskID != "" {
		taskIDLog = fmt.Sprintf(", taskID=%s", memory.TaskID)
	}
	fmt.Printf("[Memory] 状态已加载: state=%s, role=%s, request=%s%s\n",
		memory.CurrentState, memory.CurrentRole, memory.UserRequest[:min(50, len(memory.UserRequest))], taskIDLog)

	return &memory, nil
}

// HasUnfinishedTask 检查是否有未完成任务
func (mm *MemoryManager) HasUnfinishedTask() (bool, *types.TaskMemory, error) {
	memory, err := mm.LoadState()
	if err != nil {
		return false, nil, err
	}
	
	if memory == nil {
		return false, nil, nil
	}

	// 判断条件1: 状态是 working 或 waiting
	isUnfinished := memory.CurrentState == "working" || memory.CurrentState == "waiting"

	// ⚠️ G点35修复：增加时间窗口保护，但排除已完成状态
	// 防止 PM 回复后状态被覆盖为 idle 导致误判
	if !isUnfinished {
		timeSinceLastActive := time.Since(memory.LastActiveTime)
		timeWindow := 3 * time.Minute // 从10分钟改为3分钟（G点35修复）

		// 只在非终态时才使用时间窗口保护
		finishedStates := map[string]bool{"idle": true, "done": true, "error": true, "approved": true}
		if !finishedStates[memory.CurrentState] && timeSinceLastActive < timeWindow && memory.MessageCount > 0 {
			isUnfinished = true
			fmt.Printf("[Memory] ⚠️ 状态=%s 但 %.1f 分钟内有活动，视为未完成任务\n",
				memory.CurrentState, timeSinceLastActive.Minutes())
		}
	}
	
	if isUnfinished {
		timeSinceLastActive := time.Since(memory.LastActiveTime)
		taskIDInfo := ""
		if memory.TaskID != "" {
			taskIDInfo = fmt.Sprintf("[TaskID: %s]", memory.TaskID)
		}
		fmt.Printf("[Memory] ✅ 发现未完成任务: %s %s (距上次活跃: %.0f分钟)\n",
			memory.TaskDescription[:min(50, len(memory.TaskDescription))], taskIDInfo, timeSinceLastActive.Minutes())
	} else {
		fmt.Printf("[Memory] ℹ️ 没有未完成任务 (state=%s, 上次活跃: %s)\n", 
			memory.CurrentState, memory.LastActiveTime.Format("15:04:05"))
	}
	
	return isUnfinished, memory, nil
}

// ClearState 清除状态（任务完成时调用）
func (mm *MemoryManager) ClearState() error {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	if err := os.Remove(mm.memoryPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf(i18n.T("err.memory_delete"), err)
	}

	mm.lastMemory = nil
	fmt.Println("[Memory] 状态已清除")
	return nil
}

// ClearTaskMemory 清理任务记忆文件（别名方法，语义更清晰）
func (mm *MemoryManager) ClearTaskMemory() error {
	return mm.ClearState()
}

// SetStopped 设置停止标志（防止autosave重新创建文件）
func (mm *MemoryManager) SetStopped(stopped bool) {
	mm.mu.Lock()
	defer mm.mu.Unlock()
	mm.stopped = stopped
	if stopped {
		fmt.Println("[Memory] ⛔ 已设置停止标志，Autosave将不再保存")
	} else {
		fmt.Println("[Memory] ✅ 停止标志已重置")
	}
}

// trimToLastTasks 裁剪记忆，只保留最近N次用户交互的消息
func (mm *MemoryManager) trimToLastTasks(memory *types.TaskMemory, keepLastN int) {
	msgs := memory.RecentMessages
	if len(msgs) <= 20 {
		return
	}

	userIndices := []int{}
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "user" {
			userIndices = append(userIndices, i)
			if len(userIndices) >= keepLastN {
				break
			}
		}
	}

	if len(userIndices) == 0 {
		return
	}

	cutAt := userIndices[len(userIndices)-1]
	memory.RecentMessages = msgs[cutAt:]
	memory.MessageCount = len(msgs[cutAt:])
	fmt.Printf("[Memory] ✅ 裁剪记忆: %d条 → %d条 (保留最近%d次用户交互)\n", len(msgs), len(memory.RecentMessages), keepLastN)
}

// GetLastMemory 获取最后保存的记忆（不从磁盘读取）
func (mm *MemoryManager) GetLastMemory() *types.TaskMemory {
	mm.mu.RLock()
	defer mm.mu.RUnlock()
	return mm.lastMemory
}

// SaveStateImmediate 实时保存（每条消息处理完后调用）
// force: 是否强制保存（跳过stopped检查，用于用户主动发新消息时）
func (mm *MemoryManager) SaveStateImmediate(userRequest, currentState, currentRole, taskDescription string, messages []types.Message, force ...bool) error {
	// 检查是否强制保存
	shouldForce := len(force) > 0 && force[0]

	mm.mu.RLock()
	isStopped := mm.stopped
	mm.mu.RUnlock()

	if isStopped && !shouldForce {
		fmt.Println("[Memory] ⚠️ 用户已停止，跳过实时保存")
		return nil
	}

	if err := mm.SaveState(userRequest, currentState, currentRole, taskDescription, messages); err != nil {
		fmt.Printf("[Memory] ⚠️ 实时保存失败: %v\n", err)
		return err
	} else {
		fmt.Printf("[Memory] ✅ 实时保存成功: state=%s, messages=%d\n", currentState, len(messages))
		return nil
	}
}

// StartAutoSave 启动自动定期保存（兜底机制）
func (mm *MemoryManager) StartAutoSave(getState func() (string, string, string, []types.Message), interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-mm.stopCh:
			fmt.Println("[Memory] AutoSave 已停止")
			return
		case <-ticker.C:
			mm.mu.RLock()
			isStopped := mm.stopped
			mm.mu.RUnlock()

			if isStopped {
				continue
			}

			userRequest, state, role, messages := getState()
			if len(messages) > 0 && (state == "working" || state == "waiting") {
				taskDescription := extractTaskDescriptionFromTyped(messages)
				mm.SaveStateImmediate(userRequest, state, role, taskDescription, messages)
				fmt.Printf("[Memory] ⏰ 自动定期保存完成 (间隔: %v)\n", interval)
			}
		}
	}
}

// extractTaskID 从消息列表中提取任务ID（使用最新一条用户消息）
func extractTaskID(messages []types.Message) string {
	// 从后往前找，使用最新的用户消息
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if msg.Role == "user" || msg.Role == "usr" {
			if msg.ID != "" {
				return msg.ID
			}
			if !msg.Timestamp.IsZero() {
				return msg.Timestamp.Format("20060102-150405.000")
			}
			if msg.Content != "" {
				hash := simpleHash(msg.Content)
				return fmt.Sprintf("task-%s", hash)
			}
		}
	}
	return ""
}

// simpleHash 生成简单的字符串哈希（用于生成TaskID）
func simpleHash(s string) string {
	if len(s) == 0 {
		return "empty"
	}
	// 使用 CRC32 或简单数值哈希确保唯一性
	hash := uint32(0)
	for i, c := range s {
		hash = hash*31 + uint32(c) * uint32(i+1)
	}
	// 转换为16进制短串
	return fmt.Sprintf("%08x", hash)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// extractTaskDescriptionFromTyped 从 types.Message 中提取任务描述
func extractTaskDescriptionFromTyped(messages []types.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if msg.Role == "pm" && len(msg.Content) > 10 {
			if len(msg.Content) > 100 {
				return msg.Content[:100] + "..."
			}
			return msg.Content
		}
	}
	return ""
}
