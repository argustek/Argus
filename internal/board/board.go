package board

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"argus/internal/types"
)

var jsonFragmentRe = regexp.MustCompile(`\{[^}]*"current_task"[^}]*\}|\{[^}]*"action"[^}]*\}|\{[^}]*"state"\d*[^}]*\}`)

func sanitizeTask(task string) string {
	result := jsonFragmentRe.ReplaceAllString(task, "")
	result = strings.TrimSpace(result)
	result = regexp.MustCompile(`\n{3,}`).ReplaceAllString(result, "\n\n")
	return result
}

// Manager 看板管理器
type Manager struct {
	mu        sync.RWMutex
	board     types.Board
	boardPath string
}

// NewManager 创建看板管理器
func NewManager(boardPath string) *Manager {
	return &Manager{
		board: types.Board{
			StatusCode: types.StatusIdle,
			Status:     "idle",
			LastChange: time.Now(),
			UpdatedAt:  time.Now().Unix(),
		},
		boardPath: boardPath,
	}
}

// Load 从文件加载看板
func (m *Manager) Load() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	data, err := os.ReadFile(m.boardPath)
	if err != nil {
		if os.IsNotExist(err) {
			// 文件不存在，创建默认看板
			return m.save()
		}
		return err
	}

	return json.Unmarshal(data, &m.board)
}

// Save 保存看板到文件
func (m *Manager) Save() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.save()
}

// save 内部保存（需要加锁后调用）
func (m *Manager) save() error {
	// 确保目录存在
	dir := filepath.Dir(m.boardPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(m.board, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(m.boardPath, data, 0644)
}

// Get 获取看板状态
func (m *Manager) Get() types.Board {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.board
}

// UpdateTask 更新当前任务
func (m *Manager) UpdateTask(task string, totalSteps int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.board.CurrentTask = sanitizeTask(task)
	m.board.CurrentStep = 0
	m.board.TotalSteps = totalSteps
	m.board.Status = "in_progress"
	m.board.LastChange = time.Now()

	return m.save()
}

// UpdateStep 更新当前步骤
func (m *Manager) UpdateStep(step int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.board.CurrentStep = step
	m.board.LastChange = time.Now()

	return m.save()
}

// MarkDone 标记任务完成
func (m *Manager) MarkDone() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.board.Status = "done"
	m.board.LastChange = time.Now()

	return m.save()
}

// MarkError 标记任务错误
func (m *Manager) MarkError() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.board.Status = "error"
	m.board.LastChange = time.Now()

	return m.save()
}

// Reset 重置看板
func (m *Manager) Reset() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.board = types.Board{
		Status:     "pending",
		LastChange: time.Now(),
	}

	return m.save()
}

// IsDone 检查是否完成
func (m *Manager) IsDone() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.board.Status == "done"
}

// String 返回看板字符串表示
func (m *Manager) String() string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return fmt.Sprintf("[%s] %s (%d/%d)",
		m.board.Status,
		m.board.CurrentTask,
		m.board.CurrentStep,
		m.board.TotalSteps,
	)
}
