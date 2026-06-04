package executor

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// FileSnapshot 文件编辑前快照
type FileSnapshot struct {
	Path      string    // 文件路径（相对）
	Content   string    // 编辑前完整内容
	Mtime     time.Time // 文件修改时间
	Timestamp time.Time // 快照时间
	Action    string    // 触发操作: "write" | "edit"
}

// FileChangeTracker 文件变更追踪器
// 功能:
//   1. 每次 write/edit 前自动快照文件内容
//   2. 维护最近 N 步的 undo stack
//   3. 检测文件冲突（外部修改 vs 内部修改）
//   4. 支持 rollback 到指定快照
type FileChangeTracker struct {
	mu        sync.RWMutex
	snapshots []FileSnapshot // 按时间倒序（最新的在前）
	maxStack  int            // 最大保留快照数
	workDir   string         // 工作目录
}

// NewFileChangeTracker 创建变更追踪器
func NewFileChangeTracker(workDir string, maxStack int) *FileChangeTracker {
	if maxStack <= 0 {
		maxStack = 20 // 默认保留最近20步
	}
	return &FileChangeTracker{
		snapshots: make([]FileSnapshot, 0, maxStack),
		maxStack:  maxStack,
		workDir:   workDir,
	}
}

// Snapshot 在编辑前快照文件
// 返回快照索引（用于后续 rollback），如果文件不存在返回 nil（新文件无需回滚）
func (t *FileChangeTracker) Snapshot(relPath, action string) *FileSnapshot {
	t.mu.Lock()
	defer t.mu.Unlock()

	absPath := filepath.Join(t.workDir, relPath)
	info, err := os.Stat(absPath)
	if err != nil {
		// 文件不存在 → 新建文件，不需要快照
		fmt.Printf("[FileTracker] 📸 新文件 %s (%s)，跳过快照\n", relPath, action)
		return nil
	}

	content, err := os.ReadFile(absPath)
	if err != nil {
		fmt.Printf("[FileTracker] ⚠️ 无法读取 %s: %v\n", relPath, err)
		return nil
	}

	snap := FileSnapshot{
		Path:      relPath,
		Content:   string(content),
		Mtime:     info.ModTime(),
		Timestamp: time.Now(),
		Action:    action,
	}

	// 追加到栈顶
	t.snapshots = append(t.snapshots, snap)

	// 超出上限则裁剪旧快照
	if len(t.snapshots) > t.maxStack {
		t.snapshots = t.snapshots[len(t.snapshots)-t.maxStack:]
	}

	fmt.Printf("[FileTracker] 📸 快照 #%d %s (%s, %d bytes)\n", len(t.snapshots), relPath, action, len(content))
	return &snap
}

// CheckConflict 检测文件是否被外部修改
// 如果文件的当前 mtime 与最后快照的 mtime 不同，说明有外部修改
func (t *FileChangeTracker) CheckConflict(relPath string) (bool, string) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	absPath := filepath.Join(t.workDir, relPath)
	info, err := os.Stat(absPath)
	if err != nil {
		return false, "" // 文件不存在，无冲突
	}

	// 找到该文件最近的快照
	for i := len(t.snapshots) - 1; i >= 0; i-- {
		if t.snapshots[i].Path == relPath {
			if !info.ModTime().Equal(t.snapshots[i].Mtime) && info.ModTime().After(t.snapshots[i].Mtime) {
				return true, fmt.Sprintf("文件 %s 在上次编辑后被外部修改 (快照:%s → 当前:%s)",
					relPath, t.snapshots[i].Mtime.Format("15:04:05"), info.ModTime().Format("15:04:05"))
			}
			break
		}
	}
	return false, ""
}

// RollbackLast 回滚最后一次对指定文件的编辑，恢复到编辑前的状态
// 返回回滚是否成功和原因
func (t *FileChangeTracker) RollbackLast(relPath string) (bool, string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// 从后往前找该文件的最新快照
	for i := len(t.snapshots) - 1; i >= 0; i-- {
		if t.snapshots[i].Path == relPath {
			// [BUGFIX] 回滚到的是"编辑前"的状态，即前一个快照（i-1）
			// 当前快照(i)存的是编辑后的内容，我们需要恢复到编辑前
			if i == 0 {
				// 第一个快照，没有更早的状态可回滚
				return false, fmt.Sprintf("%s 是初始快照，无法再回滚", relPath)
			}

			currentSnap := t.snapshots[i]
			prevSnap := t.snapshots[i-1]
			absPath := filepath.Join(t.workDir, relPath)

			err := os.WriteFile(absPath, []byte(prevSnap.Content), 0644)
			if err != nil {
				return false, fmt.Sprintf("回滚写入失败: %v", err)
			}

			// 只移除当前快照（被撤销的edit），保留前一个作为新的"当前状态"
			t.snapshots = append(t.snapshots[:i], t.snapshots[i+1:]...)

			fmt.Printf("[FileTracker] ↩️ 回滚 %s — 撤销了 %q 操作\n", relPath, currentSnap.Action)
			return true, fmt.Sprintf("已撤销 %s 的 %q 操作（恢复到前一个状态）",
				relPath, currentSnap.Action)
		}
	}
	return false, fmt.Sprintf("没有找到 %s 的快照", relPath)
}

// GetRecentChanges 获取最近的文件变更列表（用于展示给用户或PM审核）
func (t *FileChangeTracker) GetRecentChanges(n int) []FileSnapshot {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if n <= 0 || n > len(t.snapshots) {
		n = len(t.snapshots)
	}
	result := make([]FileSnapshot, n)
	copy(result, t.snapshots[len(t.snapshots)-n:])
	// 反转使最新的在前
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}
	return result
}

// Stats 返回追踪统计信息
func (t *FileChangeTracker) Stats() map[string]interface{} {
	t.mu.RLock()
	defer t.mu.RUnlock()

	fileCount := make(map[string]int)
	actionCount := make(map[string]int)
	for _, s := range t.snapshots {
		fileCount[s.Path]++
		actionCount[s.Action]++
	}
	return map[string]interface{}{
		"total_snapshots": len(t.snapshots),
		"max_stack":       t.maxStack,
		"files_tracked":   len(fileCount),
		"actions":         actionCount,
		"top_files":       fileCount,
	}
}
