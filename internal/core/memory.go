package core

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

type Role string

const (
	RoleUser Role = "user"
	RolePM   Role = "pm"
	RoleSE   Role = "se"
	RoleAP   Role = "ap"
	RoleSys  Role = "system"
)

type MemoryEntry struct {
	Role      Role                   `json:"role"`
	Content   string                 `json:"content"`
	Timestamp time.Time              `json:"timestamp"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

type SharedMemory struct {
	mu      sync.RWMutex
	entries []MemoryEntry
	maxLen  int
}

func NewSharedMemory(maxLen int) *SharedMemory {
	if maxLen <= 0 {
		maxLen = 100
	}
	return &SharedMemory{
		entries: make([]MemoryEntry, 0, maxLen),
		maxLen:  maxLen,
	}
}

func (m *SharedMemory) Add(role Role, content string, metadata ...map[string]interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()

	meta := map[string]interface{}{}
	if len(metadata) > 0 && metadata[0] != nil {
		meta = metadata[0]
	}

	entry := MemoryEntry{
		Role:      role,
		Content:   content,
		Timestamp: time.Now(),
		Metadata:  meta,
	}

	m.entries = append(m.entries, entry)
	if len(m.entries) > m.maxLen {
		m.entries = m.entries[len(m.entries)-m.maxLen:]
	}
}

func (m *SharedMemory) GetAll() []MemoryEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]MemoryEntry, len(m.entries))
	copy(result, m.entries)
	return result
}

func (m *SharedMemory) GetByRole(role Role) []MemoryEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []MemoryEntry
	for _, e := range m.entries {
		if e.Role == role {
			result = append(result, e)
		}
	}
	return result
}

func (m *SharedMemory) GetLast(n int) []MemoryEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if n <= 0 || n > len(m.entries) {
		n = len(m.entries)
	}
	result := make([]MemoryEntry, n)
	copy(result, m.entries[len(m.entries)-n:])
	return result
}

func (m *SharedMemory) LastUserMsg() string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for i := len(m.entries) - 1; i >= 0; i-- {
		if m.entries[i].Role == RoleUser {
			return m.entries[i].Content
		}
	}
	return ""
}

func (m *SharedMemory) FormatForPrompt() string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var lines string
	for _, e := range m.entries {
		lines += fmt.Sprintf("[%s] %s\n", e.Role, e.Content)
	}
	return lines
}

func (m *SharedMemory) FormatJSON() string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	data, _ := json.MarshalIndent(m.entries, "", "  ")
	return string(data)
}

func (m *SharedMemory) Len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.entries)
}

func (m *SharedMemory) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries = make([]MemoryEntry, 0, m.maxLen)
}

func (m *SharedMemory) ToMessages() []map[string]string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	msgs := make([]map[string]string, 0, len(m.entries))
	for _, e := range m.entries {
		msgs = append(msgs, map[string]string{
			"role":    string(e.Role),
			"content": e.Content,
		})
	}
	return msgs
}
