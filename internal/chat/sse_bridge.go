package chat

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

type SSEEvent struct {
	Type string      `json:"type"`
	Data interface{} `json:"data"`
}

type SSEBridge struct {
	subscribers   map[string]chan SSEEvent
	mu            sync.RWMutex
	activeConnID  string
	heartbeatStop chan struct{}
}

func NewSSEBridge() *SSEBridge {
	return &SSEBridge{
		subscribers: make(map[string]chan SSEEvent),
	}
}

func (b *SSEBridge) Subscribe(id string) (chan SSEEvent, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.activeConnID != "" {
		return nil, false
	}

	ch := make(chan SSEEvent, 64)
	b.subscribers[id] = ch
	b.activeConnID = id

	fmt.Printf("[SSEBridge] 订阅者 %s 已连接 (活跃连接: 1)\n", id)
	return ch, true
}

func (b *SSEBridge) Unsubscribe(id string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if ch, ok := b.subscribers[id]; ok {
		close(ch)
		delete(b.subscribers, id)
	}

	if b.activeConnID == id {
		b.activeConnID = ""
	}

	if b.heartbeatStop != nil {
		close(b.heartbeatStop)
		b.heartbeatStop = nil
	}

	fmt.Printf("[SSEBridge] 订阅者 %s 已断开 (活跃连接: 0)\n", id)
}

func (b *SSEBridge) Push(event SSEEvent) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.activeConnID == "" {
		return
	}

	for _, ch := range b.subscribers {
		select {
		case ch <- event:
		default:
			fmt.Printf("[SSEBridge] ⚠️ 订阅者 channel 已满，丢弃事件: %s\n", event.Type)
		}
	}
}

func (b *SSEBridge) HasActiveConnection() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.activeConnID != ""
}

func (b *SSEBridge) PushEvent(eventType string, data interface{}) {
	b.Push(SSEEvent{Type: eventType, Data: data})
}

func (b *SSEBridge) ForceReset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	for id, ch := range b.subscribers {
		close(ch)
		delete(b.subscribers, id)
	}
	b.activeConnID = ""
	if b.heartbeatStop != nil {
		close(b.heartbeatStop)
		b.heartbeatStop = nil
	}
	fmt.Printf("[SSEBridge] ForceReset: 清理所有残留连接\n")
}

func (b *SSEBridge) StartHeartbeat() {
	b.mu.Lock()
	if b.heartbeatStop != nil {
		b.mu.Unlock()
		return
	}
	b.heartbeatStop = make(chan struct{})
	stopCh := b.heartbeatStop
	b.mu.Unlock()

	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				b.PushEvent("heartbeat", map[string]string{
					"pm_status": "idle",
					"se_status": "idle",
				})
			case <-stopCh:
				return
			}
		}
	}()
}

func (b *SSEBridge) stopHeartbeat() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.heartbeatStop != nil {
		close(b.heartbeatStop)
		b.heartbeatStop = nil
	}
}

func (b *SSEBridge) AllSubscriberCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subscribers)
}

func marshalSSEData(data interface{}) string {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return "{}"
	}
	return string(jsonData)
}

func FormatSSE(eventType string, data interface{}) string {
	return fmt.Sprintf("event: %s\ndata: %s\n\n", eventType, marshalSSEData(data))
}