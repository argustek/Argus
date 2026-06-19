package chat

import (
	"context"
	"fmt"
	"sync"
	"time"

	"argus/internal/core"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// MessagePath 消息路径标签（类似通讯协议的源地址）
type MessagePath string

const (
	PathPMToUser   MessagePath = "pm_to_user"  // PM→用户（addPMToUserMsg）
	PathPMStream   MessagePath = "pm_stream"   // PM流式输出（emitStreamChunk）
	PathSEToUser   MessagePath = "se_to_user"  // SE→用户（addSEToUserMsg）
	PathSEStream   MessagePath = "se_stream"   // SE流式输出（emitStreamChunk）
	PathSEExec     MessagePath = "se_exec"     // SE执行操作（exec_start/done/output）
	PathAPToUser   MessagePath = "ap_to_user"  // AP→用户（AP审批结果）
	PathUserInput  MessagePath = "user_input"  // 用户输入（new-message）
	PathSystem     MessagePath = "system"      // 系统消息（错误/状态）
	PathCoreOutput MessagePath = "core_output" // V2 ArgusCore输出（Bridge统一推送）
	PathStatus     MessagePath = "status"      // V2 状态同步（PM/SE/AP灯 + 阶段切换）
	PathIDEEvent   MessagePath = "ide_event"   // IDE连接状态事件（前端TopBar）
)

// MessageTag 消息标签（包含路径+校验信息）
type MessageTag struct {
	Path      MessagePath `json:"path"`       // 消息来源路径
	Checksum  string      `json:"checksum"`   // 内容校验码（长度+首尾字符）
	Timestamp int64       `json:"timestamp"`  // 发送时间戳
	SeqNum    int64       `json:"seq_num"`    // 全局序列号
	SourceLoc string      `json:"source_loc"` // 代码位置（函数名:行号）
}

// PendingMessage 待确认消息
type PendingMessage struct {
	MsgId      string     // 消息ID
	Role       string     // 角色
	Content    string     // 内容
	EventName  string     // 事件名称
	Tag        MessageTag // 标签
	SentAt     time.Time  // 发送时间
	RetryCount int        // 重试次数
}

// ReceivedMessage 前端已接收消息
type ReceivedMessage struct {
	MsgId      string        // 消息ID（批次ID或单条ID）
	Role       string        // 角色
	Content    string        // 内容
	EventName  string        // 事件名称
	ReceivedAt time.Time     // 接收时间
	Latency    time.Duration // 网络延迟
	BatchAck   *BatchAckInfo // 非nil表示这是批量确认（含起止seq范围）
}

// BatchAckInfo 批量确认信息（前端回传，覆盖起止范围）
type BatchAckInfo struct {
	StartSeq int64 `json:"start_seq"` // 该批起始全局序号
	EndSeq   int64 `json:"end_seq"`   // 该批结束全局序号
	AckCount int64 `json:"ack_count"` // 前端确认收到多少条
}

// RoleState = core.RoleState (后面板控件值，前面板只读投影)
type RoleState = core.RoleState

// MessageBus 统一消息总线组件（LabVIEW式前后一致性保障）
type MessageBus struct {
	ctx           context.Context
	pendingQueue  map[string]*PendingMessage  // msgId → 待确认消息
	receivedMap   map[string]*ReceivedMessage // msgId → 已确认消息
	lostMessages  []*PendingMessage           // 丢失消息记录
	mu            sync.RWMutex
	seqNum        int64                // 全局序列号生成器
	lastMsgId     string               // 最后发送的消息ID
	frontendReady bool                 // 前端是否就绪（OnDomReady之前不追踪）
	checkInterval time.Duration        // 检查间隔
	timeout       time.Duration        // 确认超时（普通消息）
	streamTimeout time.Duration        // 流式消息超时（更短，防pendingQueue膨胀）
	enabled       bool                 // 是否启用
	state         RoleState            // 当前角色状态（后面板控件）
	onStateChange func(RoleState)      // 状态变更回调
	writeDebugLog func(content string) // [v0.7.2] 写入 conversation.log（与Bridge一致）
}

// NewMessageBus 创建消息总线
func NewMessageBus(ctx context.Context) *MessageBus {
	mb := &MessageBus{
		ctx:           ctx,
		pendingQueue:  make(map[string]*PendingMessage),
		receivedMap:   make(map[string]*ReceivedMessage),
		lostMessages:  make([]*PendingMessage, 0),
		checkInterval: 2 * time.Second, // 每2秒检查一次
		timeout:       2 * time.Second, // 2秒超时（普通消息）
		streamTimeout: 5 * time.Second, // 5秒超时（流式消息，快速释放防膨胀）
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

func (mb *MessageBus) SetContext(ctx context.Context) {
	mb.mu.Lock()
	defer mb.mu.Unlock()
	mb.ctx = ctx
}

// generateMsgId 生成唯一消息ID（含校验信息）
func (mb *MessageBus) generateMsgId(role, eventName string, tag MessageTag) string {
	mb.seqNum++
	return fmt.Sprintf("%s_%s_%d_%d", role, eventName, tag.Timestamp, mb.seqNum)
}

// Send 强制发送消息（替代所有runtime.EventsEmit）
func (mb *MessageBus) Send(role, content, eventName string, path MessagePath, sourceLoc string, data interface{}) string {
	if !mb.enabled {
		fmt.Printf("[💧MSG] ❌ MessageBus disabled! event=%s\n", eventName)
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

	needTracking := mb.shouldTrack(path)

	if needTracking {
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
	}

	// 发送给前端
	mb.emitToFrontend(eventName, msgId, role, path, tag, sourceLoc, data)

	// [v0.8.0] 注意：日志不在 Send 时写，而在 Ack（收到前端确认）后写
	// 这样才能确保"所见即所得"——只有用户真正在对话框看到的消息才记入 conversation.log
	// 见 Ack() 方法的实现

	// 高频日志已降级（每条消息都打会导致刷屏），需要调试时取消注释
	// trackingMark := "📢(no-track)"
	// if needTracking { trackingMark = "⏳" }
	// fmt.Printf("[💧MSG%s] id=%s role=%s event=%s path=%s len=%d source=%s\n",
	// 	trackingMark, msgId, role, eventName, path, len(content), sourceLoc)

	return msgId
}

// emitToFrontend 实际执行 runtime.EventsEmit（提取为独立方法供批量缓冲复用）
func (mb *MessageBus) emitToFrontend(eventName, msgId, role string, path MessagePath, tag MessageTag, sourceLoc string, data interface{}) {
	if mb.ctx == nil {
		fmt.Printf("[💧MSG] ❌ Context is NULL! event=%s msgId=%s (NOT SENT!)\n", eventName, msgId)
		return
	}

	// 高频日志已降级（每条消息都打会导致刷屏）
	// fmt.Printf("[💧MSG] 📡 Emitting to frontend: event=%s msgId=%s path=%s\n", eventName, msgId, path)

	enrichedData := map[string]interface{}{
		"_msgId":    msgId,
		"_role":     role,
		"_path":     string(path),
		"_checksum": tag.Checksum,
		"_seqNum":   tag.SeqNum,
		"_sentAt":   tag.Timestamp,
		"_source":   sourceLoc,
		"_tracked":  true,
	}

	if m, ok := data.(map[string]interface{}); ok {
		for k, v := range m {
			enrichedData[k] = v
		}
	} else if data != nil {
		enrichedData["data"] = data
	}

	runtime.EventsEmit(mb.ctx, eventName, enrichedData)
}

// GetLastMsgId returns the last sent message ID
func (mb *MessageBus) GetLastMsgId() string {
	mb.mu.RLock()
	defer mb.mu.RUnlock()
	return mb.lastMsgId
}

// shouldTrack 判断该消息是否需要ACK追踪
// 🎯 核心原则：后端→前端的跨进程通讯必须追踪，确保可靠投递
// ⚠️ OnDomReady之前前端未就绪，暂不追踪（等前端就绪后再追踪）
// 分类策略：
//
//	✅ MUST_TRACK: PathSystem, PathUserInput, PathPM/SE/APToUser, PathSEExec
//	❌ NO_TRACK: PathCoreOutput (高频内部通道), PathStatus (状态同步)
//	📊 SAMPLE: PathPMStream, PathSEStream (流式消息采样追踪)
func (mb *MessageBus) shouldTrack(path MessagePath) bool {
	mb.mu.RLock()
	ready := mb.frontendReady
	mb.mu.RUnlock()
	if !ready {
		return false
	}

	switch path {
	case PathCoreOutput:
		return false // ⚠️ TECH-DEBT: 高频通道不能追踪！
		// 2026-06 血的教训：PathStatus 改为 return true 后，
		// pendingQueue 爆炸 → 前端完全卡死，AI 全部不动。
		// 根因：高频事件（status/chunk/thought）每秒几十条进 pendingQueue，
		// CheckPending O(n) 扫描 + 超时检测把 CPU 吃满。
		// TODO: 方案A) pendingQueue 改为 ring buffer + 异步清理
		//       方案B) 高频路径采样追踪（每N条追1条）
		//       方案C) shouldTrack 加频率限制（同路径 >QPS阈值自动降级）
		// 临时方案：重要事件（如 agent-thought）改用 PathSystem 发送

	case PathPMStream, PathSEStream:
		// [FIX-v0.8.4] 流式消息独立追踪（统一架构）
		// v0.8.3 的 batch 机制导致个体 msgId 与 batch msgId 不匹配 → ACK 永远失败
		// 现改为独立追踪 + CheckPending 快速路径（短超时+轻量扫描）解决性能问题
		return true

	case PathUserInput:
		return true

	case PathSystem:
		return true

	case PathStatus, PathIDEEvent:
		return false

	case PathPMToUser, PathSEToUser, PathAPToUser:
		return true

	case PathSEExec:
		return true

	default:
		fmt.Printf("[⚠️MSG] Unknown path %s, tracking for safety\n", path)
		return true
	}
}

// SetFrontendReady marks the frontend as ready, enabling message ACK tracking
func (mb *MessageBus) SetFrontendReady() {
	mb.mu.Lock()
	mb.frontendReady = true
	mb.mu.Unlock()
	fmt.Println("[💧MSG] 🟢 Frontend ready, ACK tracking enabled")
}

// Ack 前端确认收到（支持单条和批量两种模式）
// 单条: Ack("msg_123")
// 批量: Ack("msg_123_batch") + 前端回传 BatchAckInfo（含起止seq范围）
func (mb *MessageBus) Ack(msgId string, batchInfo ...*BatchAckInfo) bool {
	if !mb.enabled {
		return false
	}

	mb.mu.Lock()
	defer mb.mu.Unlock()

	pending, exists := mb.pendingQueue[msgId]
	if !exists {
		// 高频日志已降级
		// fmt.Printf("[✅MSG] ACK重复或未知: id=%s\n", msgId)
		return false
	}

	now := time.Now()
	latency := now.Sub(pending.SentAt)

	// [v0.7.3] Debug: log every ACK to diagnose token_stats/context_built LOST issue
	fmt.Printf("[🥤MSG] ✅ ACK received: id=%s event=%s latency=%.0fms\n",
		msgId, pending.EventName, latency.Seconds()*1000)

	received := &ReceivedMessage{
		MsgId:      msgId,
		Role:       pending.Role,
		Content:    pending.Content,
		EventName:  pending.EventName,
		ReceivedAt: now,
		Latency:    latency,
	}

	// 如果是批量确认，附带起止范围信息
	if len(batchInfo) > 0 && batchInfo[0] != nil {
		received.BatchAck = batchInfo[0]
		info := batchInfo[0]
		expected := info.EndSeq - info.StartSeq + 1
		gap := expected - info.AckCount
		if gap > 0 {
			fmt.Printf("[🥤MSG] 🍚 Batch ACK 缺失! id=%s seq=%d~%d expect=%d got=%d\n",
				msgId, info.StartSeq, info.EndSeq, expected, info.AckCount)
		}
		// 高频日志已降级（正常确认不打印）
		// fmt.Printf("[🥤MSG] 🍚 Batch ACK id=%s seq=%d~%d expect=%d got=%d %s latency=%.0fms\n",
		// 	msgId, info.StartSeq, info.EndSeq, expected, info.AckCount, status, latency.Seconds()*1000)
	} else {
		// 高频日志已降级
		// fmt.Printf("[🥤MSG] 收到确认 id=%s role=%s latency=%.0fms pending=%d\n",
		// 	msgId, pending.Role, latency.Seconds()*1000, len(mb.pendingQueue))
	}

	mb.receivedMap[msgId] = received
	delete(mb.pendingQueue, msgId)

	// [v0.8.0] 所见即所得：前端确认收到后才写 conversation.log
	// 只有用户在对话框里真正能看到的内容才记日志（pm/se/ap回复 + 用户输入）
	// 这样确保日志与前端显示 100% 一致
	if mb.writeDebugLog != nil {
		switch pending.Tag.Path {
		case PathPMToUser:
			mb.writeDebugLog(fmt.Sprintf("PM: %s", pending.Content))
		case PathSEToUser:
			mb.writeDebugLog(fmt.Sprintf("SE: %s", pending.Content))
		case PathAPToUser:
			mb.writeDebugLog(fmt.Sprintf("AP: %s", pending.Content))
		case PathUserInput:
			mb.writeDebugLog(fmt.Sprintf("USER: %s", pending.Content))
		case PathPMStream, PathSEStream:
			// 流式消息的批量确认：记录批次摘要
			if received.BatchAck != nil {
				mb.writeDebugLog(fmt.Sprintf("[STREAM-BATCH] %s: %d msgs confirmed (seq=%d~%d)",
					pending.Role, received.BatchAck.AckCount,
					received.BatchAck.StartSeq, received.BatchAck.EndSeq))
			}
		}
	}

	return true
}

// CheckPending 检查未确认消息（可被前端调用查看状态）
// [v0.8.4] 性能优化：流式消息走快速路径（短超时+轻量扫描+容量上限）
func (mb *MessageBus) CheckPending() []map[string]interface{} {
	if !mb.enabled {
		return []map[string]interface{}{}
	}

	mb.mu.Lock()
	defer mb.mu.Unlock()

	now := time.Now()
	var pendingList []map[string]interface{}
	streamLost := 0 // 流式丢失计数（静默处理，不每条都报警）

	// [v0.8.4] 容量保护：pendingQueue 超过 500 条时，优先清理最老的流式消息
	const maxPending = 500
	if len(mb.pendingQueue) > maxPending {
		var oldestStream string
		oldestTime := now
		for msgId, p := range mb.pendingQueue {
			if p.Tag.Path == PathPMStream || p.Tag.Path == PathSEStream {
				if p.SentAt.Before(oldestTime) {
					oldestTime = p.SentAt
					oldestStream = msgId
				}
			}
		}
		if oldestStream != "" {
			delete(mb.pendingQueue, oldestStream)
		}
	}

	for msgId, pending := range mb.pendingQueue {
		elapsed := now.Sub(pending.SentAt)

		// [v0.8.4] 流式快速路径：短超时 + 轻量扫描
		isStream := pending.Tag.Path == PathPMStream || pending.Tag.Path == PathSEStream
		effTimeout := mb.timeout
		if isStream {
			effTimeout = mb.streamTimeout
		}

		isTimeout := elapsed > effTimeout
		isNewLoss := isTimeout && pending.RetryCount == 0

		// 流式消息：轻量 item（跳过内容截断）
		if isStream {
			item := map[string]interface{}{
				"msgId":      msgId,
				"role":       pending.Role,
				"event":      pending.EventName,
				"path":       string(pending.Tag.Path),
				"elapsedSec": elapsed.Seconds(),
				"isTimeout":  isTimeout,
				"isNewLoss":  isNewLoss,
			}
			pendingList = append(pendingList, item)

			if isNewLoss {
				pending.RetryCount++
				streamLost++
				// 流式丢失静默处理：不写 lostMessages、不打 LOST 日志
				// （单条 chunk 丢失不影响用户体验，前端已累积显示）
				delete(mb.pendingQueue, msgId)
			}
			continue // 流式消息到此为止，不走下面的通用路径
		}

		// === 非流式消息：完整扫描（原有逻辑） ===
		contentPreview := pending.Content
		if len(contentPreview) > 80 {
			contentPreview = contentPreview[:80] + "..."
		}

		item := map[string]interface{}{
			"msgId":          msgId,
			"role":           pending.Role,
			"event":          pending.EventName,
			"path":           pending.Tag.Path,
			"source":         pending.Tag.SourceLoc,
			"direction":      "后端→前端",
			"sendedAt":       pending.SentAt.Format("15:04:05.000"),
			"elapsedSec":     elapsed.Seconds(),
			"contentLen":     len(pending.Content),
			"contentPreview": contentPreview,
			"isTimeout":      isTimeout,
			"isNewLoss":      isNewLoss,
		}
		pendingList = append(pendingList, item)

		if isNewLoss {
			pending.RetryCount++
			fmt.Printf("[🚨MSG] 超时未确认! id=%s role=%s path=%s source=%s 已等待%.1fs\n",
				msgId, pending.Role, pending.Tag.Path, pending.Tag.SourceLoc, elapsed.Seconds())
			mb.lostMessages = append(mb.lostMessages, pending)
			// [v0.7.3] Enhanced LOST alert with data preview for debugging
			contentPreview := pending.Content
			if len(contentPreview) > 200 {
				contentPreview = contentPreview[:200] + "..."
			}
			if mb.writeDebugLog != nil {
				mb.writeDebugLog(fmt.Sprintf("[MessageBus-LOST] 🚨 %s/%s msgId=%s 等待%.1fs | 发送者:%s | 数据: %s",
					pending.EventName, pending.Tag.Path, msgId, elapsed.Seconds(), pending.Tag.SourceLoc, contentPreview))
			}
		}
	}

	// 流式丢失汇总日志（每轮最多打一条）
	if streamLost > 0 {
		fmt.Printf("[💧MSG] 📊 Stream cleanup: %d chunks expired (>5s), silently dropped\n", streamLost)
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
			"msgId":   lost.MsgId,
			"role":    lost.Role,
			"event":   lost.EventName,
			"path":    lost.Tag.Path,
			"source":  lost.Tag.SourceLoc,
			"sentAt":  lost.SentAt.Format("15:04:05.000"),
			"content": lost.Content[:min(100, len(lost.Content))],
		})
	}
	return result
}

// GetStats 获取统计信息
func (mb *MessageBus) GetStats() map[string]interface{} {
	mb.mu.RLock()
	defer mb.mu.RUnlock()

	return map[string]interface{}{
		"pending":   len(mb.pendingQueue),
		"received":  len(mb.receivedMap),
		"lost":      len(mb.lostMessages),
		"totalSent": mb.seqNum,
		"enabled":   mb.enabled,
	}
}

// backgroundChecker 后台定时检查 pendingQueue 中超时的消息
func (mb *MessageBus) backgroundChecker() {
	ticker := time.NewTicker(mb.checkInterval)
	defer ticker.Stop()

	for range ticker.C {
		pending := mb.CheckPending()
		if len(pending) > 0 && mb.ctx != nil {
			for _, p := range pending {
				if isNewLoss, ok := p["isNewLoss"].(bool); ok && isNewLoss {
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

// EmitState 推送角色状态变更（后面板→前面板）
func (mb *MessageBus) EmitState(state RoleState) {
	mb.mu.Lock()
	state.UpdatedAt = time.Now().UnixMilli()
	mb.state = state
	onStateChange := mb.onStateChange
	mb.mu.Unlock()

	if onStateChange != nil {
		onStateChange(state)
	}

	fmt.Printf("[📊STATE] phase=%s pm=%s se=%s ap=%s mc=%v task=%q\n",
		state.Phase, state.PM, state.SE, state.AP, state.MC, state.Task)
}

// GetState 获取当前状态（前面板读取）
func (mb *MessageBus) GetState() RoleState {
	mb.mu.RLock()
	defer mb.mu.RUnlock()
	return mb.state
}

// SetOnStateChange 设置状态变更回调
func (mb *MessageBus) SetOnStateChange(fn func(RoleState)) {
	mb.mu.Lock()
	defer mb.mu.Unlock()
	mb.onStateChange = fn
}

// [v0.7.2] SetDebugLogWriter 注入 conversation.log 写入函数（与Bridge一致）
func (mb *MessageBus) SetDebugLogWriter(fn func(content string)) {
	mb.mu.Lock()
	defer mb.mu.Unlock()
	mb.writeDebugLog = fn
}

// SetEnabled 启用/禁用
func (mb *MessageBus) SetEnabled(enabled bool) {
	mb.mu.Lock()
	defer mb.mu.Unlock()
	mb.enabled = enabled
	fmt.Printf("[⚙️MSG] enabled=%v\n", enabled)
}
