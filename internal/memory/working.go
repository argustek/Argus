package memory

import (
	"sync"
	"time"
)

type WorkingMemory struct {
	mu sync.RWMutex
	
	CurrentFiles    map[string]string
	RecentTurns     []TurnRecord
	CurrentErrors   []string
	TempVariables   map[string]interface{}
	LastUpdated     time.Time
}

type TurnRecord struct {
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
}

func NewWorkingMemory() *WorkingMemory {
	return &WorkingMemory{
		CurrentFiles:  make(map[string]string),
		RecentTurns:   make([]TurnRecord, 0, 3),
		CurrentErrors: make([]string, 0),
		TempVariables: make(map[string]interface{}),
		LastUpdated:   time.Now(),
	}
}

func (wm *WorkingMemory) SetFileContent(path, content string) {
	wm.mu.Lock()
	defer wm.mu.Unlock()
	wm.CurrentFiles[path] = content
	wm.LastUpdated = time.Now()
}

func (wm *WorkingMemory) GetFileContent(path string) (string, bool) {
	wm.mu.RLock()
	defer wm.mu.RUnlock()
	content, ok := wm.CurrentFiles[path]
	return content, ok
}

func (wm *WorkingMemory) AddTurn(role, content string) {
	wm.mu.Lock()
	defer wm.mu.Unlock()
	
	wm.RecentTurns = append(wm.RecentTurns, TurnRecord{
		Role:      role,
		Content:   content,
		Timestamp: time.Now(),
	})
	
	if len(wm.RecentTurns) > 3 {
		wm.RecentTurns = wm.RecentTurns[len(wm.RecentTurns)-3:]
	}
	
	wm.LastUpdated = time.Now()
}

func (wm *WorkingMemory) GetRecentTurns() []TurnRecord {
	wm.mu.RLock()
	defer wm.mu.RUnlock()
	result := make([]TurnRecord, len(wm.RecentTurns))
	copy(result, wm.RecentTurns)
	return result
}

func (wm *WorkingMemory) SetErrors(errors []string) {
	wm.mu.Lock()
	defer wm.mu.Unlock()
	wm.CurrentErrors = errors
	wm.LastUpdated = time.Now()
}

func (wm *WorkingMemory) GetErrors() []string {
	wm.mu.RLock()
	defer wm.mu.RUnlock()
	result := make([]string, len(wm.CurrentErrors))
	copy(result, wm.CurrentErrors)
	return result
}

func (wm *WorkingMemory) SetTempVar(key string, value interface{}) {
	wm.mu.Lock()
	defer wm.mu.Unlock()
	wm.TempVariables[key] = value
	wm.LastUpdated = time.Now()
}

func (wm *WorkingMemory) GetTempVar(key string) (interface{}, bool) {
	wm.mu.RLock()
	defer wm.mu.RUnlock()
	value, ok := wm.TempVariables[key]
	return value, ok
}

func (wm *WorkingMemory) Clear() {
	wm.mu.Lock()
	defer wm.mu.Unlock()
	wm.CurrentFiles = make(map[string]string)
	wm.RecentTurns = make([]TurnRecord, 0, 3)
	wm.CurrentErrors = make([]string, 0)
	wm.TempVariables = make(map[string]interface{})
	wm.LastUpdated = time.Now()
}
