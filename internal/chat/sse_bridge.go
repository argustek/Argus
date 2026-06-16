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

type SubscriberInfo struct {
	ID        string    // 唯一ID (sse-<timestamp>)
	Name      string    // IDE标识 (IDE-A / IDE-B / debug)
	Connected time.Time // 连接时间
}

type SSEBridge struct {
	subscribers     map[string]chan SSEEvent
	subscriberInfo  map[string]SubscriberInfo
	mu              sync.RWMutex
	activeConnID    string // 仅调试模式（无source）使用
	debugConnected  bool
	heartbeatStop   chan struct{}
	onChange        func() // 订阅者变化回调
}

func NewSSEBridge() *SSEBridge {
	return &SSEBridge{
		subscribers:    make(map[string]chan SSEEvent),
		subscriberInfo: make(map[string]SubscriberInfo),
	}
}

// Subscribe 订阅SSE事件流
// name: 调试模式传 "debug"，IDE模式传 IDE标识（如 "IDE-A"）
// 调试模式保持单连接限制，IDE模式允许多连接
func (b *SSEBridge) Subscribe(id, name string) (chan SSEEvent, bool) {
	b.mu.Lock()

	if name == "" || name == "debug" {
		// 调试模式：保持单连接限制
		if b.debugConnected {
			b.mu.Unlock()
			return nil, false
		}
		b.debugConnected = true
	} // else: IDE模式，允许多连接

	ch := make(chan SSEEvent, 64)
	b.subscribers[id] = ch
	b.subscriberInfo[id] = SubscriberInfo{
		ID:        id,
		Name:      name,
		Connected: time.Now(),
	}

	activeCount := len(b.subscribers)
	fmt.Printf("[SSEBridge] 订阅者 %s (%s) 已连接 (活跃连接: %d)\n", id, name, activeCount)

	// 首次连接时启动心跳
	if b.heartbeatStop == nil {
		b.startHeartbeatLocked()
	}
	b.mu.Unlock()

	// 通知状态变化（锁已释放，回调可安全调用 GetSubscriberInfos）
	if b.onChange != nil {
		b.onChange()
	}

	return ch, true
}

func (b *SSEBridge) Unsubscribe(id string) {
	b.mu.Lock()

	if ch, ok := b.subscribers[id]; ok {
		close(ch)
		delete(b.subscribers, id)
	}

	// 在删除 info 前检查是否为 debug 连接
	if info, ok := b.subscriberInfo[id]; ok && (info.Name == "" || info.Name == "debug") {
		b.debugConnected = false
	}
	if id == b.activeConnID {
		b.activeConnID = ""
	}

	delete(b.subscriberInfo, id)

	// 检查是否仅剩IDE连接，如果是则保持心跳
	// 如果没有任何连接了，停止心跳
	if len(b.subscribers) == 0 && b.heartbeatStop != nil {
		close(b.heartbeatStop)
		b.heartbeatStop = nil
	}

	activeCount := len(b.subscribers)
	fmt.Printf("[SSEBridge] 订阅者 %s 已断开 (活跃连接: %d)\n", id, activeCount)
	b.mu.Unlock()

	// 通知状态变化（锁已释放，回调可安全调用 GetSubscriberInfos）
	if b.onChange != nil {
		b.onChange()
	}
}

// PushToAll 广播事件给所有订阅者
func (b *SSEBridge) PushToAll(event SSEEvent) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	for id, ch := range b.subscribers {
		select {
		case ch <- event:
		default:
			fmt.Printf("[SSEBridge] ⚠️ %s channel 已满，丢弃事件: %s\n", id, event.Type)
		}
	}
}

// PushToSubscriber 定向推送事件给指定订阅者
func (b *SSEBridge) PushToSubscriber(id string, event SSEEvent) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	ch, ok := b.subscribers[id]
	if !ok {
		return
	}

	select {
	case ch <- event:
	default:
		fmt.Printf("[SSEBridge] ⚠️ %s channel 已满，丢弃事件: %s\n", id, event.Type)
	}
}

// Push 广播事件（保留原有接口，调用 PushToAll）
func (b *SSEBridge) Push(event SSEEvent) {
	b.PushToAll(event)
}

func (b *SSEBridge) HasActiveConnection() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subscribers) > 0
}

func (b *SSEBridge) PushEvent(eventType string, data interface{}) {
	b.PushToAll(SSEEvent{Type: eventType, Data: data})
}

func (b *SSEBridge) ForceReset() {
	b.mu.Lock()
	for id, ch := range b.subscribers {
		close(ch)
		delete(b.subscribers, id)
	}
	b.subscriberInfo = make(map[string]SubscriberInfo)
	b.activeConnID = ""
	b.debugConnected = false
	if b.heartbeatStop != nil {
		close(b.heartbeatStop)
		b.heartbeatStop = nil
	}
	b.mu.Unlock()
	fmt.Printf("[SSEBridge] ForceReset: 清理所有残留连接\n")
	if b.onChange != nil {
		b.onChange()
	}
}

// SetOnChange 设置订阅者变化回调
func (b *SSEBridge) SetOnChange(cb func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.onChange = cb
}

func (b *SSEBridge) StartHeartbeat() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.heartbeatStop != nil {
		return
	}
	b.startHeartbeatLocked()
}

func (b *SSEBridge) startHeartbeatLocked() {
	b.heartbeatStop = make(chan struct{})
	stopCh := b.heartbeatStop

	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				b.PushToAll(SSEEvent{
					Type: "heartbeat",
					Data: map[string]string{
						"pm_status": "idle",
						"se_status": "idle",
					},
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

// GetSubscriberInfos 返回所有订阅者信息
func (b *SSEBridge) GetSubscriberInfos() []SubscriberInfo {
	b.mu.RLock()
	defer b.mu.RUnlock()
	infos := make([]SubscriberInfo, 0, len(b.subscriberInfo))
	for _, info := range b.subscriberInfo {
		infos = append(infos, info)
	}
	return infos
}

// GetSubscriberByID 通过ID查找订阅者信息
func (b *SSEBridge) GetSubscriberByID(id string) (SubscriberInfo, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	info, ok := b.subscriberInfo[id]
	return info, ok
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