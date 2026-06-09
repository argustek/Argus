package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"argus/internal/ai"
	"argus/internal/core"
	"argus/internal/executor"
	"argus/internal/memory"
)

type Bridge struct {
	mu            sync.RWMutex
	argus         *core.ArgusCore
	executor      *executor.Executor
	msgBus        *MessageBus
	contextWindow *memory.ContextWindow // [v0.7.2] Token 监控 + 窗口管理
	contextBuilder *memory.ContextBuilder // [v0.7.2] 任务上下文组装器
	compressor     *memory.Compressor     // [v0.7.2] 对话压缩器

	onMessage func(msg *Message)
	onChunk   func(delta string)
	ctx       context.Context
	cancel    func()

	isProcessing bool
	writeDebugLog func(content string)
}

func NewBridge(aiClient *ai.Client, exec *executor.Executor, workDir string) *Bridge {
	ctx, cancel := context.WithCancel(context.Background())

	argusCore := core.NewArgusCore(aiClient, exec, workDir)

	b := &Bridge{
		argus:     argusCore,
		executor:  exec,
		ctx:       ctx,
		cancel:    cancel,
	}

	b.argus.SetContext(ctx)
	b.argus.SetOnMessage(b.onCoreMessage)
	b.argus.SetOnChunk(b.onCoreChunk)
	b.argus.SetOnThought(b.onCoreThought) // 思考链 → MessageBus → 前端Dashboard

	return b
}

func (b *Bridge) SetMessageBus(bus *MessageBus) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.msgBus = bus
	b.argus.SetOnStateChange(func(state core.RoleState) {
		if bus != nil {
			bus.EmitState(state)
		}
	})

	// 📋 TODO: 连接TodoManager到MessageBus
	b.argus.SetOnTodoUpdate(func(event core.TodoEvent) {
		if bus != nil && b.ctx != nil {
			bus.Send("system", "todo_update", "todo_update", PathSystem, "Bridge:todo", event)
		}
	})

	// Action events (exec_start/done/output/completed) → MessageBus
	b.argus.SetOnActionEvent(func(eventName string, data interface{}) {
		if bus != nil && b.ctx != nil {
			bus.Send("se", eventName, eventName, PathSEExec, "Bridge:action:"+eventName, data)
		}
	})
}

func (b *Bridge) SetOnMessage(fn func(msg *Message)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.onMessage = fn
}

func (b *Bridge) SetOnChunk(fn func(delta string)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.onChunk = fn
}

func (b *Bridge) SetDebugLogWriter(fn func(content string)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.writeDebugLog = fn
}

// [v0.7.2] SetContextWindow 注入 Token 监控窗口
func (b *Bridge) SetContextWindow(cw *memory.ContextWindow) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.contextWindow = cw
}

// [v0.7.2] SetContextBuilder 注入任务上下文组装器
func (b *Bridge) SetContextBuilder(cb *memory.ContextBuilder) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.contextBuilder = cb
}

// [v0.7.2] SetCompressor 注入对话压缩器
func (b *Bridge) SetCompressor(c *memory.Compressor) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.compressor = c
}

// [v0.7.2] pushTokenStats 通过 MessageBus 推送 token_stats 到前端 TokenMonitor
func (b *Bridge) pushTokenStats() {
	if b.contextWindow == nil {
		if b.writeDebugLog != nil {
			b.writeDebugLog("[Bridge-CTX] ⚠ pushTokenStats 跳过: contextWindow=nil")
		}
		return
	}
	if b.msgBus == nil {
		if b.writeDebugLog != nil {
			b.writeDebugLog("[Bridge-CTX] ⚠ pushTokenStats 跳过: msgBus=nil")
		}
		return
	}
	stats := b.contextWindow.TokenStats()
	msgId := b.msgBus.Send("system", "", "token_stats", PathSystem, "Bridge:pushTokenStats", stats)
	if b.writeDebugLog != nil {
		b.writeDebugLog(fmt.Sprintf("[Bridge-CTX] ✅ token_stats 已推送 msgId=%s total_tokens=%v", msgId, stats["total_tokens"]))
	}
}

// [v0.7.2] pushContextBuilt 通过 MessageBus 推送任务上下文到前端
func (b *Bridge) pushContextBuilt(taskID string, contextStr string) {
	if b.msgBus == nil {
		return
	}
	data := map[string]interface{}{
		"task_id":   taskID,
		"context":    contextStr,
		"timestamp":  time.Now().Unix(),
	}
	msgId := b.msgBus.Send("system", "", "context_built", PathSystem, "Bridge:pushContextBuilt", data)
	if b.writeDebugLog != nil {
		b.writeDebugLog(fmt.Sprintf("[Bridge-CTX] ✅ context_built 已推送 msgId=%s taskID=%s len=%d", msgId, taskID, len(contextStr)))
	}
}

// [v0.7.2] pushCompressDone 通过 MessageBus 推送压缩结果到前端
func (b *Bridge) pushCompressDone(taskID string, compressedCount int) {
	if b.msgBus == nil {
		return
	}
	data := map[string]interface{}{
		"task_id":         taskID,
		"compressed_count": compressedCount,
		"timestamp":       time.Now().Unix(),
	}
	msgId := b.msgBus.Send("system", "", "compress_done", PathSystem, "Bridge:pushCompressDone", data)
	if b.writeDebugLog != nil {
		b.writeDebugLog(fmt.Sprintf("[Bridge-CTX] ✅ compress_done 已推送 msgId=%s taskID=%s compressed=%d", msgId, taskID, compressedCount))
	}
}

func (b *Bridge) emitStatus(status string) {
	if b.onMessage != nil {
		msg := &Message{
			From:      "system",
			To:        "frontend",
			Role:      "status",
			Content:   status,
			Timestamp: time.Now(),
		}
		b.onMessage(msg)
	}
}

// [v0.7.2] emitSystemMsg 在聊天历史中插入一条系统消息（用户可见，类似 Trae IDE 的 "正在压缩对话..."）
func (b *Bridge) emitSystemMsg(content string) {
	if b.onMessage != nil {
		msg := &Message{
			From:      "system",
			To:        "frontend",
			Role:      "system",
			Content:   content,
			Timestamp: time.Now(),
		}
		b.onMessage(msg)
	}
}

func (b *Bridge) onCoreMessage(source, content string) {
	if b.writeDebugLog != nil && content != "" {
		role := b.roleFromSource(source)
		b.writeDebugLog(fmt.Sprintf("%s: %s", strings.ToUpper(role), content))
	}
	if b.onMessage != nil && content != "" {
		parts := strings.Split(source, "_to_")
		from := parts[0]
		to := ""
		if len(parts) > 1 {
			to = parts[1]
		}
		msg := &Message{
			From:      from,
			To:        to,
			Role:      b.roleFromSource(source),
			Content:   content,
			Timestamp: time.Now(),
		}
		b.onMessage(msg)
	}
}

func (b *Bridge) onCoreChunk(delta string) {
	if b.onChunk != nil && delta != "" {
		b.onChunk(delta)
	}
}

// onCoreThought 思考链回调 → MessageBus → 前端Dashboard
// 用 PathSystem（可追踪）而非 PathCoreOutput（高频NO_TRACK，追踪会卡死前端）
func (b *Bridge) onCoreThought(evt map[string]interface{}) {
	if b.msgBus == nil || b.ctx == nil {
		return
	}
	dataJSON, err := json.Marshal(evt)
	if err != nil {
		return
	}
	b.msgBus.Send("system", string(dataJSON), "agent-thought", PathSystem, "Bridge:onCoreThought", evt)
}

func (b *Bridge) roleFromSource(source string) string {
	switch source {
	case "pm_to_user", "pm_to_se", "review_start", "pm_review":
		return "pm"
	case "se_to_user", "se_to_pm":
		return "se"
	case "ap_to_user", "ap_to_pm", "ap_start", "ap_result":
		return "ap"
	default:
		return "system"
	}
}

func (b *Bridge) Process(userMsg string) (*core.ProcessResult, error) {
	b.mu.Lock()
	if b.isProcessing {
		b.mu.Unlock()
		return nil, fmt.Errorf("busy processing another task")
	}
	b.isProcessing = true
	b.mu.Unlock()

	defer func() {
		b.mu.Lock()
		b.isProcessing = false
		b.mu.Unlock()
		if r := recover(); r != nil {
			fmt.Printf("[Bridge-PANIC] recover: %v\n", r)
		}
	}()

	b.emitStatus("phase:pm|role:pm|status:busy")

	if b.writeDebugLog != nil {
		b.writeDebugLog(fmt.Sprintf("USER: %s", userMsg))
	}

	// [v0.7.2] ContextWindow: 记录用户消息 + 推送 Token 统计
	if b.contextWindow != nil {
		b.contextWindow.AddMessage(memory.RoleUser, userMsg, 0, "")
		b.pushTokenStats()
		if b.writeDebugLog != nil {
			b.writeDebugLog("[Bridge-CTX] ✅ 用户消息已写入 ContextWindow")
		}
	}

	// [v0.7.2] ContextBuilder: 构建任务上下文并推送到前端（如果可用）
	if b.contextBuilder != nil {
		taskID := "current" // 使用固定taskID表示当前会话
		contextStr, err := b.contextBuilder.BuildContextForTask(taskID, 8000)
		if err != nil {
			if b.writeDebugLog != nil {
				b.writeDebugLog(fmt.Sprintf("[Bridge-CTX] ⚠ ContextBuilder 失败: %v", err))
			}
			// ContextBuilder失败不影响主流程，继续执行
		} else {
			b.pushContextBuilt(taskID, contextStr)
			if b.writeDebugLog != nil {
				b.writeDebugLog("[Bridge-CTX] ✅ 任务上下文已构建并推送")
			}
		}
	}

	result := b.argus.Process(userMsg)

	// [v0.7.2] ContextWindow: 记录 PM 响应 + 推送 Token 统计
	if b.contextWindow != nil && result.Success && len(result.Phases) > 0 {
		pmOutput := result.Phases[0].Output // PhaseAnalyze = PM phase
		if pmOutput != "" {
			b.contextWindow.AddMessage(memory.RoleAssistant, pmOutput, 0, "")
			b.pushTokenStats()
			if b.writeDebugLog != nil {
				b.writeDebugLog("[Bridge-CTX] ✅ PM 响应已写入 ContextWindow")
			}
		}
	}

	// [v0.7.2] Compressor: 检查是否需要压缩对话（保留最近2条，便于测试）
	if b.compressor != nil {
		taskID := "current"
		compressedCount, err := b.compressor.CompressConversations(taskID, 2)
		if err != nil {
			if b.writeDebugLog != nil {
				b.writeDebugLog(fmt.Sprintf("[Bridge-CTX] ⚠ Compressor 失败: %v", err))
			}
			// Compressor失败不影响主流程，继续执行
		} else if compressedCount > 0 {
			// 只有真的压缩了才通知前端
			b.pushCompressDone(taskID, compressedCount)
			if b.writeDebugLog != nil {
				b.writeDebugLog(fmt.Sprintf("[Bridge-CTX] ✅ 对话压缩完成: 压缩了 %d 条旧消息", compressedCount))
			}
		} else if b.writeDebugLog != nil {
			b.writeDebugLog("[Bridge-CTX] ℹ️ 对话未超过阈值，无需压缩")
		}
	}

	if b.writeDebugLog != nil {
		if result.Success {
			b.writeDebugLog(fmt.Sprintf("SYS_C: V2-Done success=%v actions=%d", result.Success, len(result.Actions)))
		} else if result.Error != nil {
			b.writeDebugLog(fmt.Sprintf("SYS_C: V2-Error %v", result.Error))
		}
	}

	if result.Success {
		b.emitStatus("phase:done|role:none|status:idle")
	} else {
		b.emitStatus("phase:error|role:none|status:error")
	}

	return result, result.Error
}

func (b *Bridge) Cancel() {
	if b.cancel != nil {
		b.cancel()
	}
	b.argus.Cancel()
}

func (b *Bridge) IsProcessing() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.isProcessing
}

func (b *Bridge) GetMemory() *core.SharedMemory {
	return b.argus.GetMemory()
}

func (b *Bridge) ClearMemory() {
	b.argus.ClearMemory()
}

func (b *Bridge) Stats() map[string]interface{} {
	return b.argus.Stats()
}

func (b *Bridge) SetLanguage(lang string) {
	b.argus.SetLanguage(lang)
}

func (b *Bridge) SetContext(ctx context.Context) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.ctx = ctx
	b.argus.SetContext(ctx)
}
