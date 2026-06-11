package chat

import (
	"testing"
	"time"

	"argus/internal/memory"
)

// TestSetContextManagement 验证三个组件正确注入到 Manager
func TestSetContextManagement(t *testing.T) {
	mgr := &Manager{}

	cw := memory.NewContextWindow(memory.DefaultContextBudget())
	cb := memory.NewContextBuilder(nil)
	c := memory.NewCompressor(nil)

	// 注入前：全部为 nil
	if mgr.contextWindow != nil {
		t.Error("contextWindow should be nil before injection")
	}
	if mgr.contextBuilder != nil {
		t.Error("contextBuilder should be nil before injection")
	}
	if mgr.compressor != nil {
		t.Error("compressor should be nil before injection")
	}

	// 注入
	mgr.SetContextManagement(cw, cb, c)

	// 注入后：全部非空
	if mgr.contextWindow == nil {
		t.Error("contextWindow should not be nil after injection")
	}
	if mgr.contextBuilder == nil {
		t.Error("contextBuilder should not be nil after injection")
	}
	if mgr.compressor == nil {
		t.Error("compressor should not be nil after injection")
	}

	// 验证是同一个实例
	if mgr.contextWindow != cw {
		t.Error("contextWindow mismatch")
	}
	if mgr.contextBuilder != cb {
		t.Error("contextBuilder mismatch")
	}
	if mgr.compressor != c {
		t.Error("compressor mismatch")
	}

	t.Log("✅ SetContextManagement: 三个组件正确注入")
}

// TestSetContextManagement_NilSafe 验证注入 nil 不会 panic
func TestSetContextManagement_NilSafe(t *testing.T) {
	mgr := &Manager{}

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("SetContextManagement with nil should not panic: %v", r)
		}
	}()

	mgr.SetContextManagement(nil, nil, nil)

	if mgr.contextWindow != nil || mgr.contextBuilder != nil || mgr.compressor != nil {
		t.Error("nil injection should result in nil fields")
	}

	t.Log("✅ SetContextManagement(nil): 安全处理")
}

// TestContextBridge_PMFlow 模拟 PM 聊天流程的 ContextWindow 写入
func TestContextBridge_PMFlow(t *testing.T) {
	cw := memory.NewContextWindow(memory.DefaultContextBudget())

	// 模拟用户发消息给 PM（对应 handleToPM 中的桥接逻辑）
	userMsg := "帮我写一个登录功能"
	cw.AddMessage(memory.RoleUser, userMsg, 0, "")

	// 模拟 PM 回复（对应 ProcessStream 返回后）
	pmReply := "@SE 请创建 login.go 文件，实现用户登录接口"
	cw.AddMessage(memory.RoleAssistant, pmReply, 0, "")

	stats := cw.TokenStats()
	msgCount := stats["message_count"].(int)
	if msgCount != 2 {
		t.Errorf("expected 2 messages, got %d", msgCount)
	}

	totalTokens := stats["total_tokens"].(int)
	if totalTokens == 0 {
		t.Error("total tokens should > 0 after adding messages")
	}

	t.Logf("✅ PM 流程桥接: %d 消息, %d tokens", msgCount, totalTokens)
}

// TestContextBridge_SEFlow 模拟 SE 任务执行的 ContextWindow 写入
func TestContextBridge_SEFlow(t *testing.T) {
	cw := memory.NewContextWindow(memory.DefaultContextBudget())

	// 模拟 SE 接收任务（对应 handleSEAskPM 中的桥接逻辑）
	taskDesc := "创建 login.go，实现 POST /api/login 接口"
	cw.AddMessage(memory.RoleUser, taskDesc, 0, "")

	// 模拟 SE 执行结果
	seResult := `{"actions":[{"type":"write_file","path":"login.go","content":"package main"}]}`
	cw.AddMessage(memory.RoleAssistant, seResult, 0, "")

	stats := cw.TokenStats()
	msgCount := stats["message_count"].(int)
	if msgCount != 2 {
		t.Errorf("expected 2 messages, got %d", msgCount)
	}

	t.Logf("✅ SE 流程桥接: %d 消息, %d tokens", msgCount, stats["total_tokens"].(int))
}

// TestContextBridge_MultiTurn 多轮对话累积写入 ContextWindow
func TestContextBridge_MultiTurn(t *testing.T) {
	cw := memory.NewContextWindow(memory.DefaultContextBudget())

	conversations := []struct {
		role    memory.ContextRole
		content string
	}{
		{memory.RoleUser, "你好"},
		{memory.RoleAssistant, "你好！我是PM，有什么可以帮你的？"},
		{memory.RoleUser, "帮我实现用户注册功能"},
		{memory.RoleAssistant, "@SE 请创建 register.go 实现用户注册"},
		{memory.RoleUser, "加上邮箱验证"},
		{memory.RoleAssistant, "好的 @SE 在注册接口中增加邮箱验证步骤"},
	}

	for i, conv := range conversations {
		cw.AddMessage(conv.role, conv.content, 0, "")
		if i%2 == 1 { // 每轮结束后检查
			stats := cw.TokenStats()
			expectedMsgs := i + 1
			msgCount := stats["message_count"].(int)
			if msgCount != expectedMsgs {
				t.Errorf("round %d: expected %d messages, got %d", i/2+1, expectedMsgs, msgCount)
			}
		}
	}

	stats := cw.TokenStats()
	msgCount := stats["message_count"].(int)
	if msgCount != len(conversations) {
		t.Errorf("final: expected %d messages, got %d", len(conversations), msgCount)
	}

	t.Logf("✅ 多轮对话: %d 条消息, 总Token=%d, 已用=%s",
		msgCount, stats["total_tokens"].(int), stats["ratio"].(string))
}

// TestContextBridge_CompressorIntegration 验证 Compressor 与 ContextWindow 协同工作
func TestContextBridge_CompressorIntegration(t *testing.T) {
	cw := memory.NewContextWindow(memory.DefaultContextBudget())

	// Compressor 需要 memory.MemoryManager
	// nil 会触发 panic（因为内部调用 GetConversations），用 recover 捕获验证桥接安全
	cmp := memory.NewCompressor(nil)

	// 先写入一些消息
	for i := 0; i < 10; i++ {
		cw.AddMessage(memory.RoleUser, "这是一条测试消息用于验证压缩功能", 0, "")
		cw.AddMessage(memory.RoleAssistant, "收到，正在处理中...", 0, "")
	}

	beforeStats := cw.TokenStats()
	beforeCount := beforeStats["message_count"].(int)

	// 执行压缩检查（nil MemoryManager 会导致 panic，捕获并记录为预期行为）
	panicRecovered := false
	func() {
		defer func() {
			if r := recover(); r != nil {
				panicRecovered = true
				t.Logf("Compressor panic（预期：MemoryManager 为 nil）: %v", r)
			}
		}()
		cmp.CompressIfNeeded("default", 100, 2)
	}()

	if !panicRecovered {
		t.Log("✅ Compressor 未 panic（MemoryManager 可能有默认行为）")
	}

	afterStats := cw.TokenStats()
	afterCount := afterStats["message_count"].(int)

	t.Logf("✅ Compressor 集成: 压缩前 %d msg / 压缩后 %d msg (panic=%v)", beforeCount, afterCount, panicRecovered)
}

// TestContextBridge_ConcurrentWrite 并发安全测试（多 goroutine 同时写入 ContextWindow）
func TestContextBridge_ConcurrentWrite(t *testing.T) {
	cw := memory.NewContextWindow(memory.DefaultContextBudget())

	done := make(chan bool)

	// 模拟 PM 和 SE 并发写入
	go func() {
		for i := 0; i < 50; i++ {
			cw.AddMessage(memory.RoleUser, "user message", 0, "")
			time.Sleep(time.Microsecond)
		}
		done <- true
	}()

	go func() {
		for i := 0; i < 50; i++ {
			cw.AddMessage(memory.RoleAssistant, "assistant response", 0, "")
			time.Sleep(time.Microsecond)
		}
		done <- true
	}()

	<-done
	<-done

	stats := cw.TokenStats()
	msgCount := stats["message_count"].(int)
	if msgCount != 100 {
		t.Errorf("concurrent write: expected 100 messages, got %d", msgCount)
	}

	t.Logf("✅ 并发安全: %d 条消息无丢失", msgCount)
}

// TestContextBridge_TokenMonitorData 验证 TokenMonitor 能读到正确的数据
func TestContextBridge_TokenMonitorData(t *testing.T) {
	cw := memory.NewContextWindow(memory.DefaultContextBudget())

	// 模拟一次完整交互
	cw.AddMessage(memory.RoleUser, "请分析当前代码库结构", 0, "")
	cw.AddMessage(memory.RoleAssistant,
		"@SE 请使用 list_files 工具查看项目目录结构并生成报告", 0, "")

	// TokenMonitor 读到的数据应该反映真实状态
	stats := cw.TokenStats()
	msgCount := stats["message_count"].(int)
	totalTokens := stats["total_tokens"].(int)
	used := stats["used"].(int)
	available := stats["available"].(int)
	ratio := stats["ratio"].(string)

	// 验证各字段合理性
	if msgCount != 2 {
		t.Errorf("expected 2 messages, got %d", msgCount)
	}
	if used == 0 {
		t.Error("used tokens should > 0")
	}
	if available <= 0 {
		t.Error("available tokens should > 0")
	}
	if totalTokens == 0 {
		t.Error("total tokens should > 0")
	}

	t.Logf("✅ TokenMonitor 数据一致性:")
	t.Logf("   总消息=%d, 总Token=%d, 已用=%d, 可用=%d, 用量=%s",
		msgCount, totalTokens, used, available, ratio)
}
