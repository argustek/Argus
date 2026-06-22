package chat

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// ============================================================
// 审计测试：MessageBus 架构合规性
//
// 规则：
//   1. runtime.EventsEmit 只允许在 message_bus.go 内部调用
//   2. writeDebugLog 对 PM/SE/AP/USER 内容只允许在 Ack() 内写入
//   3. 所有 emitToFrontend 调用的 MessagePath 必须是可追踪的（≠PathCoreOutput）
//   4. 所有 MessagePath 的 shouldTrack 配置必须正确
// ============================================================

// ---------- 单元测试：路径追踪配置 ----------

func TestAllPathsHaveCorrectTracking(t *testing.T) {
	mb := NewMessageBus(nil)
	mb.SetFrontendReady()

	tests := []struct {
		path        MessagePath
		wantTrack   bool
		description string
	}{
		{PathPMToUser, true, "PM 回复 → 必须追踪、ACK 写 log"},
		{PathSEToUser, true, "SE 回复 → 必须追踪、ACK 写 log"},
		{PathAPToUser, true, "AP 审批 → 必须追踪、ACK 写 log"},
		{PathUserInput, true, "用户输入 → 必须追踪、ACK 写 USER log"},
		{PathSystem, true, "系统消息 → 追踪（用于丢失检测）"},
		{PathSEExec, true, "SE 执行事件 → 追踪"},
		{PathPMStream, true, "PM 流 → 追踪（1/10 采样）"},
		{PathSEStream, true, "SE 流 → 追踪（1/10 采样）"},
		{PathCoreOutput, false, "核心输出 → 不追踪（旧兼容路径）"},
		{PathStatus, true, "状态 → [v0.9.0] 启用追踪，bounded queue 兜底"},
	}

	for _, tc := range tests {
		got := mb.shouldTrack(tc.path)
		if got != tc.wantTrack {
			t.Errorf("shouldTrack(%s) = %v, 期望 %v — %s", tc.path, got, tc.wantTrack, tc.description)
		}
	}
}

func TestAckWritesLogForTrackedPaths(t *testing.T) {
	var logBuf strings.Builder
	var mu sync.Mutex

	mb := NewMessageBus(nil)
	mb.SetFrontendReady()
	mb.SetDebugLogWriter(func(s string) {
		mu.Lock()
		logBuf.WriteString(s + "\n")
		mu.Unlock()
	})

	// 测试所有可追踪路径：Send + Ack → log 包含内容
	paths := []struct {
		path    MessagePath
		role    string
		content string
		wantLog string
	}{
		{PathPMToUser, "pm", "PM analysis result", "PM: PM analysis result"},
		{PathSEToUser, "se", "SE execution result", "SE: SE execution result"},
		{PathAPToUser, "ap", "AP approved", "AP: AP approved"},
		{PathUserInput, "user", "user message", "USER: user message"},
	}

	for _, tc := range paths {
		logBuf.Reset()

		msgId := mb.Send(tc.role, tc.content, tc.role+"_message", tc.path, "test", map[string]interface{}{"content": tc.content})
		if msgId == "" {
			t.Fatalf("Send(%s) returned empty msgId", tc.path)
		}

		ok := mb.Ack(msgId)
		if !ok {
			t.Fatalf("Ack(%s) failed for path=%s", msgId, tc.path)
		}

		logContent := logBuf.String()
		if !strings.Contains(logContent, tc.wantLog) {
			t.Errorf("Ack log for path=%s:\n  期望包含: %q\n  实际: %q", tc.path, tc.wantLog, logContent)
		}
	}
}

func TestAckDoesNotWriteLogForUntrackedPaths(t *testing.T) {
	var logBuf strings.Builder

	mb := NewMessageBus(nil)
	mb.SetFrontendReady()
	mb.SetDebugLogWriter(func(s string) {
		logBuf.WriteString(s + "\n")
	})

	// PathCoreOutput → shouldTrack=false → Ack 不应写 log
	msgId := mb.Send("pm", "some content", "pm_message", PathCoreOutput, "test", nil)
	if msgId == "" {
		t.Fatal("Send returned empty msgId")
	}

	// 不应有 pending entry
	pending := mb.CheckPending()
	if len(pending) != 0 {
		t.Fatalf("PathCoreOutput 不应追踪，但 pending 有 %d 条", len(pending))
	}

	// Ack 也不应该写 log（因为没追踪）
	ok := mb.Ack(msgId)
	if ok {
		t.Error("Ack 对未追踪的消息应返回 false")
	}
	if logBuf.Len() > 0 {
		t.Errorf("Ack 不应为未追踪路径写 log，实际: %s", logBuf.String())
	}
}

// ---------- 静态分析：源代码合规性检查 ----------

// 已知可接受的直接 runtime.EventsEmit 调用（白名单）
var knownEventsEmitAllowlist = map[string]map[int]string{
	filepath.Join("internal", "chat", "message_bus.go"): {
		212: "emitToFrontend — MessageBus 单一前向出口",
		532: "backgroundChecker — 消息丢失告警（非用户内容）",
	},
	filepath.Join("app.go"): {
		1003: "emitToFrontend fallback — msgBus=nil 时降级，初始化阶段使用",
		3952: "file-tree-dirty — 文件树脏标记（非消息通道）",
	},
}

func TestNoDirectEventsEmitOutsideMessageBus(t *testing.T) {
	projectRoot := findProjectRoot(t)
	if projectRoot == "" {
		t.Skip("无法定位项目根目录")
	}

	violations := []string{}
	filepath.Walk(projectRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		if strings.Contains(path, "vendor") || strings.Contains(path, "node_modules") ||
			strings.Contains(path, "messagebus_audit_test.go") {
			return nil
		}

		relPath, _ := filepath.Rel(projectRoot, path)
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		content := string(data)
		lines := strings.Split(content, "\n")
		for i, line := range lines {
			trimmed := strings.TrimSpace(line)
			if !strings.Contains(trimmed, "runtime.EventsEmit") {
				continue
			}
			// 跳过 import、注释、fmt.Sprintf 中的引用
			if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "*") ||
				strings.Contains(trimmed, `"runtime"`) || strings.Contains(trimmed, "fmt.Sprintf") {
				continue
			}
			// 检查白名单
			if fileAllowlist, ok := knownEventsEmitAllowlist[relPath]; ok {
				if _, allowed := fileAllowlist[i+1]; allowed {
					continue
				}
			}
			violations = append(violations, fmt.Sprintf("%s:%d: %s", relPath, i+1, trimmed))
		}
		return nil
	})

	if len(violations) > 0 {
		t.Errorf("发现 %d 处 runtime.EventsEmit 调用不在白名单中:\n%s",
			len(violations), strings.Join(violations, "\n"))
	}
}

func TestNoDirectUserContentWriteDebugLog(t *testing.T) {
	projectRoot := findProjectRoot(t)
	if projectRoot == "" {
		t.Skip("无法定位项目根目录")
	}

	// 只在 Ack() 中允许写 PM:/SE:/AP:/USER: 日志
	// message_bus.go 的 Ack 函数是唯一允许的地方
	allowedPaths := []string{
		filepath.Join("internal", "chat", "message_bus.go"),
	}

	forbiddenPrefixes := []string{`"PM:`, `"SE:`, `"AP:`, `"USER:`}

	violations := []string{}
	filepath.Walk(projectRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		if strings.Contains(path, "vendor") || strings.Contains(path, "node_modules") ||
			strings.Contains(path, "messagebus_audit_test.go") {
			return nil
		}

		relPath, _ := filepath.Rel(projectRoot, path)
		for _, a := range allowedPaths {
			if relPath == a {
				return nil
			}
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		content := string(data)
		lines := strings.Split(content, "\n")
		for i, line := range lines {
			trimmed := strings.TrimSpace(line)
			if !strings.Contains(trimmed, "writeDebugLog") {
				continue
			}
			if strings.HasPrefix(trimmed, "//") {
				continue
			}
			for _, prefix := range forbiddenPrefixes {
				if strings.Contains(trimmed, prefix) {
					violations = append(violations, fmt.Sprintf("%s:%d: %s", relPath, i+1, trimmed))
					break
				}
			}
		}
		return nil
	})

	if len(violations) > 0 {
		t.Errorf("发现 %d 处直接 writeDebugLog(PM/SE/AP/USER:) — 只应在 Ack 中写入:\n%s",
			len(violations), strings.Join(violations, "\n"))
	}
}

func TestNoCoreOutputInTrackedPaths(t *testing.T) {
	projectRoot := findProjectRoot(t)
	if projectRoot == "" {
		t.Skip("无法定位项目根目录")
	}

	violations := []string{}
	filepath.Walk(projectRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		if strings.Contains(path, "vendor") || strings.Contains(path, "node_modules") ||
			strings.Contains(path, "message_bus_test.go") || strings.Contains(path, "messagebus_audit_test.go") {
			return nil
		}

		relPath, _ := filepath.Rel(projectRoot, path)
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		content := string(data)
		lines := strings.Split(content, "\n")
		for i, line := range lines {
			trimmed := strings.TrimSpace(line)
			if !strings.Contains(trimmed, "PathCoreOutput") {
				continue
			}
			if strings.HasPrefix(trimmed, "//") {
				continue
			}
			// 跳过常量声明行
			if strings.Contains(trimmed, "PathCoreOutput MessagePath") {
				continue
			}
			if strings.Contains(trimmed, "shouldTrack") || strings.Contains(trimmed, "case PathCoreOutput") {
				continue
			}
			violations = append(violations, fmt.Sprintf("%s:%d: %s", relPath, i+1, trimmed))
		}
		return nil
	})

	if len(violations) > 0 {
		t.Errorf("PathCoreOutput 不应出现在 emitToFrontend 调用中（无追踪无日志）:\n%s",
			strings.Join(violations, "\n"))
	}
}

// ---------- 集成测试：端到端消息流 ----------

func TestPMToUserFullCycle(t *testing.T) {
	var logBuf strings.Builder

	mb := NewMessageBus(nil)
	mb.SetFrontendReady()
	mb.SetDebugLogWriter(func(s string) {
		logBuf.WriteString(s + "\n")
	})

	// 模拟 PM 流式输出：多个 delta 通过 msgBus.Send 发送
	deltas := []string{"写", "一个", "hello.go", "文件"}
	var msgIds []string
	for _, d := range deltas {
		msgId := mb.Send("pm", d, "pm_message", PathPMToUser, "Bridge:pm_to_user",
			map[string]interface{}{"delta": d})
		if msgId == "" {
			t.Fatal("Send 返回空 msgId")
		}
		msgIds = append(msgIds, msgId)
	}

	// 逐个确认
	for i, msgId := range msgIds {
		ok := mb.Ack(msgId)
		if !ok {
			t.Fatalf("Ack #%d (msgId=%s) 失败", i, msgId)
		}
	}

	logContent := logBuf.String()
	for _, d := range deltas {
		if !strings.Contains(logContent, fmt.Sprintf("PM: %s", d)) {
			t.Errorf("log 应包含 PM delta: %q", d)
		}
	}
}

func TestUserInputFullCycle(t *testing.T) {
	var logBuf strings.Builder

	mb := NewMessageBus(nil)
	mb.SetFrontendReady()
	mb.SetDebugLogWriter(func(s string) {
		logBuf.WriteString(s + "\n")
	})

	userMsg := "hello world"
	msgId := mb.Send("user", userMsg, "new-message", PathUserInput,
		"App:SendMessage", map[string]interface{}{"content": userMsg})
	if msgId == "" {
		t.Fatal("Send 返回空 msgId")
	}

	ok := mb.Ack(msgId)
	if !ok {
		t.Fatal("Ack 失败")
	}

	logContent := logBuf.String()
	if !strings.Contains(logContent, fmt.Sprintf("USER: %s", userMsg)) {
		t.Errorf("log 应包含 USER: %s，实际: %s", userMsg, logContent)
	}
}

func TestDuplicateAckReturnsFalse(t *testing.T) {
	mb := NewMessageBus(nil)
	mb.SetFrontendReady()

	msgId := mb.Send("pm", "test", "pm_message", PathPMToUser, "test", nil)
	if msgId == "" {
		t.Fatal("Send 返回空 msgId")
	}

	if !mb.Ack(msgId) {
		t.Fatal("第一次 Ack 应成功")
	}
	if mb.Ack(msgId) {
		t.Error("重复 Ack 应返回 false")
	}
}

func TestAckForUnknownMsgIdReturnsFalse(t *testing.T) {
	mb := NewMessageBus(nil)
	if mb.Ack("nonexistent") {
		t.Error("Ack 不存在的 msgId 应返回 false")
	}
	if mb.Ack("") {
		t.Error("Ack 空字符串应返回 false")
	}
}

// ---------- 集成测试：auto-ACK（headless / HTTP API 模式） ----------

func TestAutoAckPendingMessages(t *testing.T) {
	var logBuf strings.Builder
	var mu sync.Mutex

	mb := NewMessageBus(nil)
	// 不调用 SetFrontendReady() — 模拟 HTTP API / headless 模式
	mb.SetDebugLogWriter(func(s string) {
		mu.Lock()
		logBuf.WriteString(s + "\n")
		mu.Unlock()
	})

	// 发送 PM + USER 消息
	pmId := mb.Send("pm", "PM analysis result", "pm_message", PathPMToUser, "test", map[string]interface{}{"delta": "PM analysis result"})
	userId := mb.Send("user", "user message", "new-message", PathUserInput, "test", map[string]interface{}{"content": "user message"})

	// 不应有 pending 条目（frontend not ready → shouldTrack=false）
	pending := mb.CheckPending()
	if len(pending) != 0 {
		t.Fatalf("headless 模式不应追踪消息，但 pending 有 %d 条", len(pending))
	}

	// 模拟 app.ackPendingMessages(): ACK 所有 pending（应该为空操作）
	if pmId != "" {
		mb.Ack(pmId)
	}
	if userId != "" {
		mb.Ack(userId)
	}

	// headless 模式下，log 不应有 PM/USER 内容（未追踪）
	logContent := logBuf.String()
	if strings.Contains(logContent, "PM:") {
		t.Errorf("headless 模式不应有 PM log: %s", logContent)
	}
	if strings.Contains(logContent, "USER:") {
		t.Errorf("headless 模式不应有 USER log: %s", logContent)
	}
}

func TestAutoAckAfterFrontendReady(t *testing.T) {
	var logBuf strings.Builder
	var mu sync.Mutex

	mb := NewMessageBus(nil)
	mb.SetFrontendReady() // 模拟 GUI 模式：前端就绪
	mb.SetDebugLogWriter(func(s string) {
		mu.Lock()
		logBuf.WriteString(s + "\n")
		mu.Unlock()
	})

	// 发送 PM 消息 — 应有 pending 条目
	msgId := mb.Send("pm", "PM content", "pm_message", PathPMToUser, "test", map[string]interface{}{"delta": "PM content"})
	if msgId == "" {
		t.Fatal("Send 返回空 msgId")
	}

	pending := mb.CheckPending()
	if len(pending) != 1 {
		t.Fatalf("应追踪 1 条消息，实际 %d 条", len(pending))
	}

	// 模拟 app.ackPendingMessages(): 遍历 pending 并 ACK
	for _, p := range pending {
		if id, ok := p["msgId"].(string); ok && id != "" {
			mb.Ack(id)
		}
	}

	// 验证 log 有 PM 内容
	logContent := logBuf.String()
	if !strings.Contains(logContent, "PM: PM content") {
		t.Errorf("auto-ACK 后 PM log 应写入，实际 log: %s", logContent)
	}

	// 重复 ACK 安全
	if mb.Ack(msgId) {
		t.Error("重复 ACK 应返回 false")
	}
}

func TestAutoAckMultiplePaths(t *testing.T) {
	var logBuf strings.Builder
	var mu sync.Mutex

	mb := NewMessageBus(nil)
	mb.SetFrontendReady()
	mb.SetDebugLogWriter(func(s string) {
		mu.Lock()
		logBuf.WriteString(s + "\n")
		mu.Unlock()
	})

	// 发送不同路径的消息
	msgIds := map[string]string{}
	paths := map[string]struct {
		path   MessagePath
		role   string
		prefix string
	}{
		"pm":   {PathPMToUser, "pm", "PM:"},
		"se":   {PathSEToUser, "se", "SE:"},
		"ap":   {PathAPToUser, "ap", "AP:"},
		"user": {PathUserInput, "user", "USER:"},
	}
	for name, info := range paths {
		id := mb.Send(info.role, name+" content", name+"_msg", info.path, "test", map[string]interface{}{"content": name + " content"})
		if id == "" {
			t.Fatalf("Send %s 返回空 msgId", name)
		}
		msgIds[name] = id
	}

	// 模拟 app.ackPendingMessages(): 遍历 pending 并 ACK
	for _, p := range mb.CheckPending() {
		if id, ok := p["msgId"].(string); ok && id != "" {
			mb.Ack(id)
		}
	}

	logContent := logBuf.String()
	for _, info := range paths {
		if !strings.Contains(logContent, info.prefix) {
			t.Errorf("auto-ACK 后应包含 %s, log: %s", info.prefix, logContent)
		}
	}
}

// ---------- 辅助函数 ----------

func findProjectRoot(t *testing.T) string {
	t.Helper()
	// 从当前目录向上查找 go.mod
	dir, err := os.Getwd()
	if err != nil {
		t.Logf("Getwd 失败: %v", err)
		return ""
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Log("未找到 go.mod，已达根目录")
			return ""
		}
		dir = parent
	}
}

// TestSendRecordsTimestamp 验证 Send 记录时间戳
func TestSendRecordsTimestamp(t *testing.T) {
	mb := NewMessageBus(nil)
	mb.SetFrontendReady()

	before := time.Now()
	msgId := mb.Send("pm", "test", "pm_message", PathPMToUser, "test", nil)
	after := time.Now()

	mb.mu.RLock()
	pending, exists := mb.pendingQueue[msgId]
	mb.mu.RUnlock()

	if !exists {
		t.Fatal("msgId 应在 pendingQueue 中")
	}

	if pending.SentAt.Before(before) || pending.SentAt.After(after) {
		t.Errorf("时间戳不在合理范围内: %v (范围 %v ~ %v)", pending.SentAt, before, after)
	}
}

// TestAllPathsReportedInStats 验证 GetStats 报告所有路径统计
func TestAllPathsReportedInStats(t *testing.T) {
	mb := NewMessageBus(nil)
	mb.SetFrontendReady()

	// 给每个路径发送一条消息
	paths := []MessagePath{PathPMToUser, PathSEToUser, PathAPToUser, PathUserInput, PathSystem, PathSEExec}
	for _, p := range paths {
		mb.Send("test", "content", "test_event", p, "test", nil)
	}

	stats := mb.GetStats()
	if stats == nil {
		t.Fatal("GetStats 返回 nil")
	}
	totalSent, ok := stats["totalSent"].(int64)
	if !ok {
		t.Fatalf("stats['totalSent'] 类型错误: %T", stats["totalSent"])
	}
	if totalSent <= 0 {
		t.Errorf("totalSent 应 >0，实际=%d", totalSent)
	}
}
