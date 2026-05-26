package chat

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// MessagePath 消息路径标签（类似通讯协议的源地址）
type MessagePath string

const (
	PathPMToUser    MessagePath = "pm_to_user"     // PM→用户（addPMToUserMsg）
	PathPMStream    MessagePath = "pm_stream"       // PM流式输出（emitStreamChunk）
	PathSEToUser    MessagePath = "se_to_user"      // SE→用户（addSEToUserMsg）
	PathSEStream    MessagePath = "se_stream"       // SE流式输出（emitStreamChunk）
	PathSEExec      MessagePath = "se_exec"         // SE执行操作（exec_start/done/output）
	PathAPToUser    MessagePath = "ap_to_user"      // AP→用户（AP审批结果）
	PathUserInput   MessagePath = "user_input"      // 用户输入（new-message）
	PathSystem      MessagePath = "system"          // 系统消息（错误/状态）
)

// MessageTag 消息标签（包含路径+校验信息）
type MessageTag struct {
	Path      MessagePath `json:"path"`                // 消息来源路径
	Checksum  string      `json:"checksum"`            // 内容校验码（长度+首尾字符）
	Timestamp int64       `json:"timestamp"`           // 发送时间戳
	SeqNum    int64       `json:"seq_num"`             // 全局序列号
	SourceLoc string      `json:"source_loc"`          // 代码位置（函数名:行号）
}

// PendingMessage 待确认消息
type PendingMessage struct {
	MsgId     string      // 消息ID
	Role      string      // 角色
	Content   string      // 内容
	EventName string      // 事件名称
	Tag       MessageTag  // 标签
	SentAt    time.Time   // 发送时间
	RetryCount int        // 重试次数
}

// ReceivedMessage 前端已接收消息
type ReceivedMessage struct {
	MsgId     string    // 消息ID
	Role      string    // 角色
	Content   string    // 内容
	EventName string    // 事件名称
	ReceivedAt time.Time // 接收时间
	Latency   time.Duration // 网络延迟
}

// MessageBus 统一消息总线组件（LabVIEW式前后一致性保障）
type MessageBus struct {
	ctx           context.Context
	pendingQueue  map[string]*PendingMessage // msgId → 待确认消息
	receivedMap   map[string]*ReceivedMessage // msgId → 已确认消息
	lostMessages  []*PendingMessage         // 丢失消息记录
	mu            sync.RWMutex
	seqNum        int64                    // 全局序列号生成器
	checkInterval time.Duration            // 检查间隔
	timeout       time.Duration            // 确认超时
	enabled       bool                     // 是否启用
}

// NewMessageBus 创建消息总线
func NewMessageBus(ctx context.Context) *MessageBus {
	mb := &MessageBus{
		ctx:           ctx,
		pendingQueue:  make(map[string]*PendingMessage),
		receivedMap:   make(map[string]*ReceivedMessage),
		lostMessages:  make([]*PendingMessage, 0),
		checkInterval: 2 * time.Second,  // 每2秒检查一次
		timeout:       5 * time.Second,   // 5秒超时
		enabled:       true,
	}
	
	go mb.backgroundChecker()
	return mb
}

// generateChecksum 生成内容校验码
func (mb *MessageBus) generateChecksum(content string) string {
	if len(content) == 0 {
		return "empty"
	}
	len := len(content)
	first := content[0]
	last := content[len-1]
	return fmt.Sprintf("%d_%d_%d", len, first, last)
}

// generateMsgId 生成唯一消息ID（含校验信息）
func (mb *MessageBus) generateMsgId(role, eventName string, tag MessageTag) string {
	mb.seqNum++
	return fmt.Sprintf("%s_%s_%d_%d", role, eventName, tag.Timestamp, mb.seqNum)
}

// Send 强制发送消息（替代所有runtime.EventsEmit）
func (mb *MessageBus) Send(role, content, eventName string, path MessagePath, sourceLoc string, data interface{}) string {
	if !mb.enabled {
		return ""
	}

	now := time.Now()
	tag := MessageTag{
		Path:      path,
		Checksum:  mb.generateChecksum(content),
		Timestamp: now.UnixNano(),
		SeqNum:    mb.seqNum,
		SourceLoc: sourceLoc,
	}

	msgId := mb.generateMsgId(role, eventName, tag)

	pending := &PendingMessage{
		MsgId:      msgId,
		Role:       role,
		Content:    content,
		EventName:  eventName,
		Tag:        tag,
		SentAt:     now,
		RetryCount: 0,
	}

	mb.mu.Lock()
	mb.pendingQueue[msgId] = pending
	mb.mu.Unlock()

	if mb.ctx == nil {
		return msgId
	}
	
	enrichedData := map[string]interface{}{
		"_msgId":    msgId,
		"_role":     role,
		"_path":     string(path),
		"_checksum": tag.Checksum,
		"_seqNum":   tag.SeqNum,
		"_sentAt":   tag.Timestamp,
		"_source":   sourceLoc,
	}
	
	if m, ok := data.(map[string]interface{}); ok {
		for k, v := range m {
			enrichedData[k] = v
		}
	} else if data != nil {
		enrichedData["data"] = data
	}
	
	runtime.EventsEmit(mb.ctx, eventName, enrichedData)
	
	fmt.Printf("[💧MSG] 送出 id=%s role=%s event=%s path=%s checksum=%s len=%d source=%s\n",
		msgId, role, eventName, path, tag.Checksum, len(content), sourceLoc)
	
	return msgId
}

// Ack 前端确认收到
func (mb *MessageBus) Ack(msgId string) bool {
	if !mb.enabled {
		return false
	}
	
	mb.mu.Lock()
	defer mb.mu.Unlock()
	
	pending, exists := mb.pendingQueue[msgId]
	if !exists {
		fmt.Printf("[✅MSG] ACK重复或未知: id=%s\n", msgId)
		return false
	}
	
	now := time.Now()
	latency := now.Sub(pending.SentAt)
	
	received := &ReceivedMessage{
		MsgId:      msgId,
		Role:       pending.Role,
		Content:    pending.Content,
		EventName:  pending.EventName,
		ReceivedAt: now,
		Latency:    latency,
	}
	
	mb.receivedMap[msgId] = received
	delete(mb.pendingQueue, msgId)
	
	fmt.Printf("[🥤MSG] 收到确认 id=%s role=%s latency=%.0fms pending=%d\n",
		msgId, pending.Role, latency.Seconds()*1000, len(mb.pendingQueue))
	
	return true
}

// CheckPending 检查未确认消息（可被前端调用查看状态）
func (mb *MessageBus) CheckPending() []map[string]interface{} {
	if !mb.enabled {
		return []map[string]interface{}{}
	}
	
	mb.mu.RLock()
	defer mb.mu.RUnlock()
	
	now := time.Now()
	var pendingList []map[string]interface{}
	
	for msgId, pending := range mb.pendingQueue {
		elapsed := now.Sub(pending.SentAt)
		
		item := map[string]interface{}{
			"msgId":      msgId,
			"role":       pending.Role,
			"event":      pending.EventName,
			"path":       pending.Tag.Path,
			"source":     pending.Tag.SourceLoc,
			"sendedAt":   pending.SentAt.Format("15:04:05.000"),
			"elapsedSec": elapsed.Seconds(),
			"contentLen": len(pending.Content),
			"isTimeout":  elapsed > mb.timeout,
		}
		pendingList = append(pendingList, item)
		
		if elapsed > mb.timeout && pending.RetryCount == 0 {
			pending.RetryCount++
			fmt.Printf("[🚨MSG] 超时未确认! id=%s role=%s path=%s source=%s 已等待%.1fs\n",
				msgId, pending.Role, pending.Tag.Path, pending.Tag.SourceLoc, elapsed.Seconds())
			
			mb.lostMessages = append(mb.lostMessages, pending)
		}
	}
	
	return pendingList
}

// GetLostMessages 获取丢失消息历史
func (mb *MessageBus) GetLostMessages() []map[string]interface{} {
	mb.mu.RLock()
	defer mb.mu.RUnlock()
	
	result := make([]map[string]interface{}, 0, len(mb.lostMessages))
	for _, lost := range mb.lostMessages {
		result = append(result, map[string]interface{}{
			"msgId":     lost.MsgId,
			"role":      lost.Role,
			"event":     lost.EventName,
			"path":      lost.Tag.Path,
			"source":    lost.Tag.SourceLoc,
			"sentAt":    lost.SentAt.Format("15:04:05.000"),
			"content":   lost.Content[:min(100, len(lost.Content))],
		})
	}
	return result
}

// GetStats 获取统计信息
func (mb *MessageBus) GetStats() map[string]interface{} {
	mb.mu.RLock()
	defer mb.mu.RUnlock()
	
	return map[string]interface{}{
		"pending":    len(mb.pendingQueue),
		"received":   len(mb.receivedMap),
		"lost":       len(mb.lostMessages),
		"totalSent":  mb.seqNum,
		"enabled":    mb.enabled,
	}
}

// backgroundChecker 后台定时检查器
func (mb *MessageBus) backgroundChecker() {
	ticker := time.NewTicker(mb.checkInterval)
	defer ticker.Stop()
	
	for range ticker.C {
		pending := mb.CheckPending()
		if len(pending) > 0 && mb.ctx != nil {
			for _, p := range pending {
				if isTimeout, ok := p["isTimeout"].(bool); ok && isTimeout {
					runtime.EventsEmit(mb.ctx, "message_lost", p)
				}
			}
		}
	}
}

// Clear 清理所有状态（新任务开始时调用）
func (mb *MessageBus) Clear() {
	mb.mu.Lock()
	defer mb.mu.Unlock()
	
	mb.pendingQueue = make(map[string]*PendingMessage)
	mb.receivedMap = make(map[string]*ReceivedMessage)
	mb.lostMessages = make([]*PendingMessage, 0)
	
	fmt.Println("[🧹MSG] 已清理所有状态")
}

// SetEnabled 启用/禁用
func (mb *MessageBus) SetEnabled(enabled bool) {
	mb.mu.Lock()
	defer mb.mu.Unlock()
	mb.enabled = enabled
	fmt.Printf("[⚙️MSG] enabled=%v\n", enabled)
}
