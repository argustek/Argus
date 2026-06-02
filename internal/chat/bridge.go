package chat

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"argus/internal/ai"
	"argus/internal/core"
	"argus/internal/executor"
)

type Bridge struct {
	mu       sync.RWMutex
	argus    *core.ArgusCore
	executor *executor.Executor
	msgBus   *MessageBus

	onMessage func(msg *Message)
	onChunk   func(delta string)
	ctx       context.Context
	cancel    context.CancelFunc

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

	result := b.argus.Process(userMsg)

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
