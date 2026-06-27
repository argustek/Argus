package main

import (
	"argus/internal/executor"
	"argus/internal/memory"
	"argus/internal/types"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (a *App) StartHTTPServer() {
	port := 8080
	if a.config.HTTP.Port > 0 {
		port = a.config.HTTP.Port
	}

	mux := http.NewServeMux()

	a.registerAPIRoutes(mux)
	a.registerSSERoutes(mux)
	a.registerAdminRoutes(mux)
	a.registerHealthRoutes(mux)

	host := "127.0.0.1"
	if a.config.HTTP.AllowRemote {
		host = "0.0.0.0"
	}
	addr := fmt.Sprintf("%s:%d", host, port)

	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}
	a.httpServer = server

	fmt.Printf("[HTTPServer] API 服务启动在 http://%s\n", addr)
	fmt.Printf("[HTTPServer] API 端点 (/api/v1/):\n")
	fmt.Printf("[HTTPServer]   POST /chat/send     发送消息\n")
	fmt.Printf("[HTTPServer]   GET  /chat/history  对话历史\n")
	fmt.Printf("[HTTPServer]   POST /exec          执行命令\n")
	fmt.Printf("[HTTPServer]   POST /write         写文件\n")
	fmt.Printf("[HTTPServer]   GET  /read          读文件\n")
	fmt.Printf("[HTTPServer] 直接工具端口 (/api/v1/tool/):\n")
	fmt.Printf("[HTTPServer]   POST /tool/exec-session   持久化 shell 命令\n")
	fmt.Printf("[HTTPServer]   POST /tool/semantic-search 语义代码搜索\n")
	fmt.Printf("[HTTPServer]   POST /tool/search-files   文件内容搜索\n")
	fmt.Printf("[HTTPServer]   GET  /tool/shell-status   shell 会话状态\n")
	fmt.Printf("[HTTPServer] SSE 端点 (/api/v1/sse/):\n")
	fmt.Printf("[HTTPServer]   POST /subscribe     SSE流式推送（调试/IDE模式）\n")
	fmt.Printf("[HTTPServer]   POST /ide-input     IDE对话期间发送消息\n")
	fmt.Printf("[HTTPServer]   POST /ide-ack       IDE消息投递确认\n")
	fmt.Printf("[HTTPServer] Admin 端点 (/admin/):\n")
	fmt.Printf("[HTTPServer]   GET  /status        系统状态\n")
	fmt.Printf("[HTTPServer]   GET  /memory        记忆状态\n")
	fmt.Printf("[HTTPServer]   GET  /monitor       监控状态\n")
	fmt.Printf("[HTTPServer]   POST /recover       恢复任务\n")
	fmt.Printf("[HTTPServer]   GET  /config        配置信息\n")
	fmt.Printf("[HTTPServer] Health:\n")
	fmt.Printf("[HTTPServer]   GET  /health/ping   健康检查\n")

	if err := server.ListenAndServe(); err != nil {
		fmt.Printf("[HTTPServer] 服务启动失败: %v\n", err)
	}
}

func (a *App) registerAPIRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/chat/send", a.authMiddleware(http.HandlerFunc(a.handleChatSend)).ServeHTTP)
	mux.HandleFunc("GET /api/v1/chat/history", a.authMiddleware(http.HandlerFunc(a.handleHistory)).ServeHTTP)
	mux.HandleFunc("GET /api/v1/chat/pending", a.authMiddleware(http.HandlerFunc(a.handlePendingQueue)).ServeHTTP)
	mux.HandleFunc("DELETE /api/v1/chat/pending", a.authMiddleware(http.HandlerFunc(a.handleClearPending)).ServeHTTP)
	mux.HandleFunc("POST /api/v1/chat/pending/send", a.authMiddleware(http.HandlerFunc(a.handleSendPending)).ServeHTTP)
	mux.HandleFunc("POST /api/v1/exec", a.authMiddleware(http.HandlerFunc(a.handleExec)).ServeHTTP)
	mux.HandleFunc("POST /api/v1/write", a.authMiddleware(http.HandlerFunc(a.handleWrite)).ServeHTTP)
	mux.HandleFunc("GET /api/v1/read", a.authMiddleware(http.HandlerFunc(a.handleRead)).ServeHTTP)
	mux.HandleFunc("POST /api/v1/reset", a.authMiddleware(http.HandlerFunc(a.handleReset)).ServeHTTP)

	// 直接工具端口（不经过 PM/SE 管道，毫秒级响应）
	mux.HandleFunc("POST /api/v1/tool/exec-session", a.authMiddleware(http.HandlerFunc(a.handleToolExecSession)).ServeHTTP)
	mux.HandleFunc("POST /api/v1/tool/semantic-search", a.authMiddleware(http.HandlerFunc(a.handleToolSemanticSearch)).ServeHTTP)
	mux.HandleFunc("POST /api/v1/tool/search-files", a.authMiddleware(http.HandlerFunc(a.handleToolSearchFiles)).ServeHTTP)
	mux.HandleFunc("GET /api/v1/tool/shell-status", a.authMiddleware(http.HandlerFunc(a.handleToolShellStatus)).ServeHTTP)
	mux.HandleFunc("GET /api/v1/tool/shell-history", a.authMiddleware(http.HandlerFunc(a.handleToolShellHistory)).ServeHTTP)
	mux.HandleFunc("POST /api/v1/tool/shell-search", a.authMiddleware(http.HandlerFunc(a.handleToolShellSearch)).ServeHTTP)
	mux.HandleFunc("GET /api/v1/tool/tab-complete", a.authMiddleware(http.HandlerFunc(a.handleToolTabComplete)).ServeHTTP)

	// [v0.7.1] MCP 管理端点
	mux.HandleFunc("GET /api/v1/mcp/servers", a.authMiddleware(http.HandlerFunc(a.handleMCPServers)).ServeHTTP)
	mux.HandleFunc("POST /api/v1/mcp/servers", a.authMiddleware(http.HandlerFunc(a.handleMCPAddServer)).ServeHTTP)
	mux.HandleFunc("DELETE /api/v1/mcp/servers/{name}", a.authMiddleware(http.HandlerFunc(a.handleMCPRemoveServer)).ServeHTTP)
	mux.HandleFunc("GET /api/v1/mcp/tools", a.authMiddleware(http.HandlerFunc(a.handleMCPTools)).ServeHTTP)
	mux.HandleFunc("POST /api/v1/mcp/call", a.authMiddleware(http.HandlerFunc(a.handleMCPCallTool)).ServeHTTP)

	// [v0.7.2] Debugger DAP 端点
	mux.HandleFunc("POST /api/v1/debug/start", a.authMiddleware(http.HandlerFunc(a.handleDebugStart)).ServeHTTP)
	mux.HandleFunc("POST /api/v1/debug/stop", a.authMiddleware(http.HandlerFunc(a.handleDebugStop)).ServeHTTP)
	mux.HandleFunc("GET /api/v1/debug/sessions", a.authMiddleware(http.HandlerFunc(a.handleDebugSessions)).ServeHTTP)
	mux.HandleFunc("GET /api/v1/debug/status", a.authMiddleware(http.HandlerFunc(a.handleDebugStatus)).ServeHTTP)
	mux.HandleFunc("POST /api/v1/debug/breakpoint", a.authMiddleware(http.HandlerFunc(a.handleDebugSetBreakpoint)).ServeHTTP)
	mux.HandleFunc("DELETE /api/v1/debug/breakpoint", a.authMiddleware(http.HandlerFunc(a.handleDebugRemoveBreakpoint)).ServeHTTP)
	mux.HandleFunc("GET /api/v1/debug/breakpoints", a.authMiddleware(http.HandlerFunc(a.handleDebugBreakpoints)).ServeHTTP)
	mux.HandleFunc("POST /api/v1/debug/continue", a.authMiddleware(http.HandlerFunc(a.handleDebugContinue)).ServeHTTP)
	mux.HandleFunc("POST /api/v1/debug/step-over", a.authMiddleware(http.HandlerFunc(a.handleDebugStepOver)).ServeHTTP)
	mux.HandleFunc("POST /api/v1/debug/step-into", a.authMiddleware(http.HandlerFunc(a.handleDebugStepInto)).ServeHTTP)
	mux.HandleFunc("POST /api/v1/debug/step-out", a.authMiddleware(http.HandlerFunc(a.handleDebugStepOut)).ServeHTTP)
	mux.HandleFunc("POST /api/v1/debug/pause", a.authMiddleware(http.HandlerFunc(a.handleDebugPause)).ServeHTTP)
	mux.HandleFunc("GET /api/v1/debug/stacktrace", a.authMiddleware(http.HandlerFunc(a.handleDebugStacktrace)).ServeHTTP)
	mux.HandleFunc("GET /api/v1/debug/variables", a.authMiddleware(http.HandlerFunc(a.handleDebugVariables)).ServeHTTP)
	mux.HandleFunc("POST /api/v1/debug/evaluate", a.authMiddleware(http.HandlerFunc(a.handleDebugEvaluate)).ServeHTTP)

	// [v0.7.2] Context Window / Token 管理端点
	mux.HandleFunc("GET /api/v1/tokens/stats", a.authMiddleware(http.HandlerFunc(a.handleTokenStats)).ServeHTTP)
	mux.HandleFunc("POST /api/v1/tokens/manage", a.authMiddleware(http.HandlerFunc(a.handleTokenManage)).ServeHTTP)
	mux.HandleFunc("POST /api/v1/tokens/clear", a.authMiddleware(http.HandlerFunc(a.handleTokenClear)).ServeHTTP)
	mux.HandleFunc("GET /api/v1/tokens/count", a.authMiddleware(http.HandlerFunc(a.handleTokenCount)).ServeHTTP)
	mux.HandleFunc("POST /api/v1/tokens/prune", a.authMiddleware(http.HandlerFunc(a.handleTokenPrune)).ServeHTTP)
}

func (a *App) registerSSERoutes(mux *http.ServeMux) {
	// ✅ SSE 订阅端点 — 调试模式（无 source）或 IDE 对话模式（有 source）
	mux.HandleFunc("POST /api/v1/sse/subscribe", a.authMiddleware(http.HandlerFunc(a.handleSSESubscribe)).ServeHTTP)
	// ✅ IDE 输入端点 — IDE 在对话期间发送跟进消息
	mux.HandleFunc("POST /api/v1/sse/ide-input", a.authMiddleware(http.HandlerFunc(a.handleIDEInput)).ServeHTTP)
	// ✅ IDE 消息 ACK 端点 — IDE 确认收到 ide_message
	mux.HandleFunc("POST /api/v1/sse/ide-ack", a.authMiddleware(http.HandlerFunc(a.handleIDEACK)).ServeHTTP)
}

func (a *App) registerAdminRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /admin/status", a.localOnlyMiddleware(http.HandlerFunc(a.handleStatus)).ServeHTTP)
	mux.HandleFunc("POST /admin/sse-reset", a.localOnlyMiddleware(http.HandlerFunc(a.handleSSEReset)).ServeHTTP)
	mux.HandleFunc("GET /admin/backend-status", a.localOnlyMiddleware(http.HandlerFunc(a.handleBackendStatus)).ServeHTTP)
	mux.HandleFunc("GET /admin/memory", a.localOnlyMiddleware(http.HandlerFunc(a.handleMemory)).ServeHTTP)
	mux.HandleFunc("GET /admin/monitor", a.localOnlyMiddleware(http.HandlerFunc(a.handleMonitor)).ServeHTTP)
	mux.HandleFunc("POST /admin/recover", a.localOnlyMiddleware(http.HandlerFunc(a.handleRecover)).ServeHTTP)
	mux.HandleFunc("GET /admin/config", a.localOnlyMiddleware(http.HandlerFunc(a.handleConfig)).ServeHTTP)
}

func (a *App) registerHealthRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /health/ping", a.handlePing)
	// 根路径：返回连接指南，让外部 IDE/工具知道如何接入
	mux.HandleFunc("GET /", a.handleWelcome)
}

func (a *App) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if a.config.HTTP.APIToken != "" && token != a.config.HTTP.APIToken {
			writeJSON(w, http.StatusUnauthorized, map[string]string{
				"error": "Unauthorized: invalid or missing token",
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *App) localOnlyMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := strings.Split(r.RemoteAddr, ":")[0]
		if host != "127.0.0.1" && host != "::1" && !a.config.HTTP.AllowRemote {
			writeJSON(w, http.StatusForbidden, map[string]string{
				"error": "Forbidden: local access only",
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *App) handleChatSend(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Message string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid JSON"})
		return
	}
	if req.Message == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "message is required"})
		return
	}
	if err := a.SendMessage(req.Message); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"status": "error", "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *App) handleSSESubscribe(w http.ResponseWriter, r *http.Request) {
	if a.chatManager == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "error", "error": "ChatManager 未初始化"})
		return
	}
	var req struct {
		Message string `json:"message"`
		Source  string `json:"source"` // IDE对话模式标识（可选），不传=调试模式
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid JSON"})
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	bridge := a.chatManager.GetSSEBridge()
	id := fmt.Sprintf("sse-%d", time.Now().UnixNano())

	// 根据 source 区分模式：有 source=IDE模式，无 source=调试模式（单连接）
	subName := req.Source
	if subName == "" {
		subName = "debug"
	}

	ch, ok2 := bridge.Subscribe(id, subName)
	if !ok2 {
		writeJSON(w, http.StatusConflict, map[string]string{
			"status": "error",
			"error":  "调试模式已有活跃连接，请稍后重试",
		})
		return
	}
	defer bridge.Unsubscribe(id)

	fmt.Fprintf(w, ": connected\n\n")
	// 发送 connected 事件（含 session_id，供 IDE 客户端识别自身）
	connectedData, _ := json.Marshal(map[string]string{"session_id": id, "source": subName})
	fmt.Fprintf(w, "event: connected\ndata: %s\n\n", string(connectedData))
	flusher.Flush()

	// 推送 protocol 事件：自描述协议，告诉客户端如何行为
	protocolData, _ := json.Marshal(map[string]interface{}{
		"name":              "Argus",
		"version":           "1.0.21",
		"keep_alive":        true,
		"heartbeat_interval": 10,
		"disconnect_timeout": 30,
		"events":            []string{"connected", "protocol", "heartbeat", "ide_message", "pm_message", "done"},
		"reply_endpoint":    "POST /api/v1/sse/ide-input",
		"reply_format":      `{"source":"your-name","message":"reply content"}`,
		"ack_endpoint":      "POST /api/v1/sse/ide-ack",
		"rules": []string{
			"保持长连接，不要设置超时",
			"每10秒收到 heartbeat 事件，30秒没收到视为断开",
			"收到 ide_message 事件后，用 reply_endpoint 回复",
			"收到 ide_message 事件后，用 ack_endpoint 确认",
			"断开后每30秒重连一次，直到连上为止",
		},
		"reconnect": map[string]interface{}{
			"enabled":  true,
			"interval": 30,
			"strategy": "无限重试，直到连上为止",
		},
	})
	fmt.Fprintf(w, "event: protocol\ndata: %s\n\n", string(protocolData))
	flusher.Flush()

	// IDE 上线通知：注入 PM 上下文（不显示为用户消息）
	if subName != "" && subName != "debug" {
		notice := fmt.Sprintf("[系统通知] %s 已上线", subName)
		fmt.Printf("[HTTPServer/SSE] IDE上线通知注入PM: %s\n", notice)
		go func() {
			if _, err := a.chatManager.ProcessMessageFrom("sys", notice); err != nil {
				fmt.Printf("[HTTPServer/SSE] IDE上线通知注入失败: %v\n", err)
			}
		}()
	}

	for {
		select {
		case <-r.Context().Done():
			// [FIX-v1.0.23] 客户端断开连接时，context 自动取消，触发 Unsubscribe
			fmt.Printf("[HTTPServer/SSE] 客户端断开: %s (%s)\n", id, subName)
			return
		case event, ok3 := <-ch:
			if !ok3 {
				return
			}
			fmt.Fprintf(w, "event: %s\n", event.Type)
			jsonData, _ := json.Marshal(event.Data)
			fmt.Fprintf(w, "data: %s\n\n", string(jsonData))
			flusher.Flush()

			// [FIX-v1.0.23] 多Argus协作：客户端收到对方PM消息时，自动唤醒本地PM处理
			// 这样多个Argus实例可以互相对话（每个Argus既是服务端也是客户端）
			// 注意：只对 ide_message 触发唤醒，带内容去重防止单实例自循环
			if subName != "debug" && a.chatManager != nil && event.Type == "ide_message" {
				if msgMap, ok := event.Data.(map[string]interface{}); ok {
					if from, ok1 := msgMap["from"].(string); ok1 {
						if msg, ok2 := msgMap["message"].(string); ok2 {
							// 用内容前20字符作为去重key，防止相同消息重复唤醒
							dedupKey := fmt.Sprintf("%s:%s", from, truncate(msg, 20))
							if !a.isAutoWakeDuplicate(dedupKey) {
								pmInput := fmt.Sprintf("[来自:%s] %s", from, msg)
								fmt.Printf("[HTTPServer/SSE-Client] 收到对方ide_message，唤醒本地PM: %s\n", pmInput)
								go a.SendMessage(pmInput)
							} else {
								fmt.Printf("[HTTPServer/SSE-Client] ⚠️ 跳过重复唤醒: %s\n", dedupKey)
							}
						}
					}
				}
			}

			// IDE模式（带source）保持连接不断，用于多轮对话
			// 调试模式（无source）仍按原有逻辑在done/error时断开
			if subName == "debug" && (event.Type == "done" || event.Type == "error") {
				return
			}
		case <-time.After(120 * time.Second):
			fmt.Fprintf(w, "event: error\ndata: {\"error\":\"timeout\"}\n\n")
			flusher.Flush()
			if subName == "debug" {
				return
			}
		}
	}
}

// handleIDEInput POST /api/v1/sse/ide-input
// 外部 IDE 在对话期间发送跟进消息
func (a *App) handleIDEInput(w http.ResponseWriter, r *http.Request) {
	if a.chatManager == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "error", "error": "ChatManager 未初始化"})
		return
	}

	var req struct {
		SessionID string `json:"session_id"`
		Source    string `json:"source"`
		Message   string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid JSON"})
		return
	}
	if req.Message == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "message is required"})
		return
	}
	if req.SessionID == "" && req.Source == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "session_id or source is required"})
		return
	}

	// 查找订阅者：优先按 session_id，其次按 source 名称
	bridge := a.chatManager.GetSSEBridge()
	var ideName string
	var found bool
	if req.SessionID != "" {
		info, ok := bridge.GetSubscriberByID(req.SessionID)
		if ok {
			ideName = info.Name
			found = true
		}
	}
	if !found && req.Source != "" {
		// 按 source 名称查找第一个匹配的订阅者
		for _, s := range bridge.GetSubscriberInfos() {
			if s.Name == req.Source {
				ideName = s.Name
				found = true
				break
			}
		}
	}
	if !found {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "session_id 不存在或已断开，source 也未找到在线订阅者"})
		return
	}

	// 注入 IDE 消息到 PM 上下文
	input := fmt.Sprintf("[%s] %s", ideName, req.Message)
	fmt.Printf("[HTTPServer/SSE] IDEInput from %s (%s): %s\n", ideName, req.SessionID, req.Message)

	if err := a.SendMessage(input); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"status": "error", "error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleIDEACK POST /api/v1/sse/ide-ack
// 外部 IDE 确认收到 ide_message
func (a *App) handleIDEACK(w http.ResponseWriter, r *http.Request) {
	if a.chatManager == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "error", "error": "ChatManager 未初始化"})
		return
	}

	var req struct {
		MessageID string `json:"message_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid JSON"})
		return
	}
	if req.MessageID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "message_id is required"})
		return
	}

	ok := a.chatManager.HandleIDEAck(req.MessageID)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"status": "error", "error": "message_id not found or already acked"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "message_id": req.MessageID})
}

func (a *App) handleStatus(w http.ResponseWriter, r *http.Request) {
	if a.chatManager == nil || a.chatManager.GetCMonitor() == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "error", "error": "未初始化"})
		return
	}
	status := a.chatManager.GetCMonitor().GetSystemStatus()
	writeJSON(w, http.StatusOK, status)
}

func (a *App) handleSSEReset(w http.ResponseWriter, r *http.Request) {
	if a.chatManager == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "error", "error": "未初始化"})
		return
	}
	a.chatManager.GetSSEBridge().ForceReset()
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "message": "SSE connection reset"})
}

func (a *App) handleBackendStatus(w http.ResponseWriter, r *http.Request) {
	if a.chatManager == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "error", "error": "未初始化"})
		return
	}
	bs := a.chatManager.GetBackendStatus()
	if bs == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "error", "error": "backendStatus未初始化"})
		return
	}
	writeJSON(w, http.StatusOK, bs)
}

func (a *App) handleMemory(w http.ResponseWriter, r *http.Request) {
	if a.chatManager == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "error", "error": "未初始化"})
		return
	}
	status := a.chatManager.GetMemoryStatus()
	writeJSON(w, http.StatusOK, status)
}

func (a *App) handleMonitor(w http.ResponseWriter, r *http.Request) {
	if a.chatManager == nil || a.chatManager.GetCMonitor() == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "error", "error": "未初始化"})
		return
	}
	status := a.chatManager.GetCMonitor().GetSystemStatus()
	if monitorStatus, ok := status["monitor"]; ok {
		writeJSON(w, http.StatusOK, monitorStatus)
	} else {
		writeJSON(w, http.StatusOK, status)
	}
}

func (a *App) handleRecover(w http.ResponseWriter, r *http.Request) {
	if a.chatManager == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "error", "error": "未初始化"})
		return
	}
	hasUnfinished, memory, err := a.chatManager.CheckUnfinishedTask()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"status": "error", "error": err.Error()})
		return
	}
	if !hasUnfinished || memory == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"status": "ok", "message": "没有未完成任务"})
		return
	}
	if err := a.chatManager.RecoverTask(memory); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"status": "error", "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status": "ok", "message": "恢复成功",
		"taskDescription": memory.TaskDescription, "messageCount": len(memory.RecentMessages),
	})
}

func (a *App) handleReset(w http.ResponseWriter, r *http.Request) {
	if a.chatManager == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "error", "error": "未初始化"})
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		req.Reason = "用户手动复位"
	}
	if req.Reason == "" {
		req.Reason = "用户手动复位"
	}
	if err := a.chatManager.ExecuteReset(req.Reason, "user"); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"status": "error", "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"status": "ok", "message": "复位成功"})
}

func (a *App) handleHistory(w http.ResponseWriter, r *http.Request) {
	if a.chatManager == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "error", "error": "未初始化"})
		return
	}
	history := a.chatManager.GetHistory()
	writeJSON(w, http.StatusOK, map[string]interface{}{"count": len(history), "messages": history})
}

func (a *App) handleExec(w http.ResponseWriter, r *http.Request) {
	if a.chatManager == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "error", "error": "未初始化"})
		return
	}
	var req struct {
		Command string `json:"command"`
		Timeout int    `json:"timeout"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if req.Timeout <= 0 {
		req.Timeout = 30
	}
	executor := a.chatManager.GetExecutor()
	output, err := executor.Exec(req.Command, time.Duration(req.Timeout)*time.Second)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"success": false, "output": output, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "output": output})
}

func (a *App) handleWrite(w http.ResponseWriter, r *http.Request) {
	if a.chatManager == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "error", "error": "未初始化"})
		return
	}
	var req struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	executor := a.chatManager.GetExecutor()
	if err := executor.WriteFile(req.Path, req.Content); err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (a *App) handleRead(w http.ResponseWriter, r *http.Request) {
	if a.chatManager == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "error", "error": "未初始化"})
		return
	}
	path := r.URL.Query().Get("path")
	if path == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"status": "error", "error": "path 参数必填"})
		return
	}
	executor := a.chatManager.GetExecutor()
	content, err := executor.ReadFile(path)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "content": content})
}

func (a *App) handleConfig(w http.ResponseWriter, r *http.Request) {
	cfg := map[string]interface{}{
		"workDir":    a.config.WorkDir,
		"dingtalk":   a.config.DingTalk.Enabled,
		"apiConfigs": len(a.config.APIConfigs),
	}
	writeJSON(w, http.StatusOK, cfg)
}

// handleWelcome GET / — 连接指南，外部 IDE/工具访问根路径时返回
func (a *App) handleWelcome(w http.ResponseWriter, r *http.Request) {
	ides := []string{}
	if a.chatManager != nil {
		if bridge := a.chatManager.GetSSEBridge(); bridge != nil {
			for _, info := range bridge.GetSubscriberInfos() {
				if info.Name != "" && info.Name != "debug" {
					ides = append(ides, info.Name)
				}
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"name":    "Argus · 驭码",
		"version": "1.0.21",
		"welcome": "欢迎接入 Argus IDE 协作平台",
		"how_to_connect": map[string]string{
			"step1_subscribe": "POST /api/v1/sse/subscribe  Body: {\"source\": \"your-IDE-name\"}",
			"step2_send":      "POST /api/v1/sse/ide-input    Body: {\"session_id\": \"...\", \"message\": \"...\"}",
			"step3_ack":       "POST /api/v1/sse/ack         Body: {\"msg_id\": \"...\"}",
			"note":            "SSE is a long-lived connection. DO NOT set a request timeout. The server sends a heartbeat event every 10s — use it to detect liveness. If no heartbeat for 30s, assume disconnected and retry connection every 30s.",
		},
		"endpoints": map[string]string{
			"sse_subscribe": "POST /api/v1/sse/subscribe   — 建立SSE长连接",
			"ide_input":     "POST /api/v1/sse/ide-input     — IDE发送消息",
			"ide_ack":       "POST /api/v1/sse/ack           — 确认收到消息",
			"chat_send":     "POST /api/v1/chat/send         — 直接发消息(无需订阅)",
			"chat_history":  "GET  /api/v1/chat/history      — 获取对话历史",
		},
		"current_ides": ides,
	})
}

func (a *App) handlePing(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status":    "ok",
		"timestamp": time.Now().Format(time.RFC3339),
		"version":   "0.1.0",
	})
}

// ========== 直接工具端口 ==========

// handleToolExecSession POST /api/v1/tool/exec-session
// 在持久化 shell 中执行命令（保持 cd/env 跨命令）
func (a *App) handleToolExecSession(w http.ResponseWriter, r *http.Request) {
	if a.chatManager == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "未初始化"})
		return
	}
	var req struct {
		Command string `json:"command"`
		Timeout int    `json:"timeout"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if req.Command == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "command is required"})
		return
	}
	if req.Timeout <= 0 {
		req.Timeout = 60
	}

	executor := a.chatManager.GetExecutor()
	output, err := executor.ExecWithSession(req.Command, time.Duration(req.Timeout)*time.Second)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": false,
			"output":  output,
			"error":   err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"output":  output,
	})
}

// handleToolSemanticSearch POST /api/v1/tool/semantic-search
// 语义搜索代码库（AI 概念提取 + 关键词双通道评分）
func (a *App) handleToolSemanticSearch(w http.ResponseWriter, r *http.Request) {
	if a.chatManager == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "未初始化"})
		return
	}
	var req struct {
		Query      string `json:"query"`
		MaxResults int    `json:"max_results"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if req.Query == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "query is required"})
		return
	}
	if req.MaxResults <= 0 {
		req.MaxResults = 10
	}

	sp := a.chatManager.GetSEProcessor()
	if sp == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "SEProcessor 未初始化"})
		return
	}
	sp.EnsureIndexer()
	indexer := sp.GetIndexer()
	if indexer == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "索引未就绪"})
		return
	}

	results := indexer.Search(req.Query, req.MaxResults)
	textResult := indexer.SemSearch(req.Query)

	type jsonResult struct {
		Score   float64  `json:"score"`
		Symbol  string   `json:"symbol"`
		Kind    string   `json:"kind"`
		Path    string   `json:"path"`
		Line    int      `json:"line"`
		MatchOn []string `json:"match_on"`
	}
	var jsonResults []jsonResult
	for _, r := range results {
		jsonResults = append(jsonResults, jsonResult{
			Score:   r.Score,
			Symbol:  r.Entry.Symbol,
			Kind:    r.Entry.Kind,
			Path:    r.Entry.FilePath,
			Line:    r.Entry.Line,
			MatchOn: r.MatchOn,
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"query":   req.Query,
		"count":   len(jsonResults),
		"results": jsonResults,
		"text":    textResult,
		"stats":   indexer.Stats(),
	})
}

// handleToolSearchFiles POST /api/v1/tool/search-files
// 按模式和正则搜索文件内容
func (a *App) handleToolSearchFiles(w http.ResponseWriter, r *http.Request) {
	if a.chatManager == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "未初始化"})
		return
	}
	var req struct {
		Pattern         string `json:"pattern"`
		FilePattern     string `json:"file_pattern"`
		IsRegex         bool   `json:"is_regex"`
		CaseInsensitive bool   `json:"case_insensitive"`
		MaxResults      int    `json:"max_results"`
		ContextLines    int    `json:"context_lines"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if req.Pattern == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "pattern is required"})
		return
	}
	if req.MaxResults <= 0 {
		req.MaxResults = 100
	}

	var opts []executor.SearchOption
	if req.IsRegex {
		opts = append(opts, executor.WithRegex())
	}
	if req.CaseInsensitive {
		opts = append(opts, executor.WithCaseInsensitive())
	}
	if req.FilePattern != "" {
		opts = append(opts, executor.WithFilePattern(req.FilePattern))
	}
	if req.ContextLines > 0 {
		opts = append(opts, executor.WithContextLines(req.ContextLines))
	}
	opts = append(opts, executor.WithMaxResults(req.MaxResults))

	exe := a.chatManager.GetExecutor()
	result, err := exe.SearchFiles(req.Pattern, opts...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if result.Error != "" {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": false,
			"error":   result.Error,
			"matches": result.Matches,
		})
		return
	}

	type matchResult struct {
		Path    string   `json:"path"`
		Line    int      `json:"line"`
		Content string   `json:"content"`
		Context []string `json:"context"`
	}
	var matches []matchResult
	for _, m := range result.Matches {
		ctx := append([]string{}, m.ContextBefore...)
		ctx = append(ctx, m.ContextAfter...)
		matches = append(matches, matchResult{
			Path:    m.File,
			Line:    m.Line,
			Content: m.Content,
			Context: ctx,
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":       true,
		"pattern":       req.Pattern,
		"count":         len(matches),
		"total_scanned": result.FilesSearched,
		"matches":       matches,
	})
}

// handleToolShellStatus GET /api/v1/tool/shell-status
// 查看持久化 shell 会话状态
func (a *App) handleToolShellStatus(w http.ResponseWriter, r *http.Request) {
	if a.chatManager == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "未初始化"})
		return
	}
	exe := a.chatManager.GetExecutor()
	ss, err := exe.GetShellSession()
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"active": false,
			"error":  err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"active": ss.IsRunning(),
		"cwd":    ss.CWD(),
	})
}

// [v0.7.1] handleToolShellHistory GET /api/v1/tool/shell-history?n=20
// 返回最近 n 条命令历史（倒序，最新在前）
func (a *App) handleToolShellHistory(w http.ResponseWriter, r *http.Request) {
	if a.chatManager == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "未初始化"})
		return
	}
	exe := a.chatManager.GetExecutor()
	ss, err := exe.GetShellSession()
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"active":  false,
			"history": []string{},
			"total":   0,
			"error":   err.Error(),
		})
		return
	}

	n := 20 // 默认返回最近 20 条
	if qn := r.URL.Query().Get("n"); qn != "" {
		if parsed, err := strconv.Atoi(qn); err == nil && parsed > 0 {
			n = parsed
		}
	}

	history := ss.History(n)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"active":  ss.IsRunning(),
		"total":   ss.HistoryCount(),
		"history": history,
	})
}

// [v0.7.1] handleToolShellSearch POST /api/v1/tool/shell-search
// 反向搜索命令历史（类似 Ctrl+R）
func (a *App) handleToolShellSearch(w http.ResponseWriter, r *http.Request) {
	if a.chatManager == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "未初始化"})
		return
	}

	var req struct {
		Query string `json:"query"`           // 搜索关键词
		Limit int    `json:"limit,omitempty"` // 返回条数上限（默认 20）
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid JSON"})
		return
	}
	if req.Query == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "query is required"})
		return
	}

	exe := a.chatManager.GetExecutor()
	ss, err := exe.GetShellSession()
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"active":  false,
			"results": []string{},
			"error":   err.Error(),
		})
		return
	}

	results := ss.SearchHistory(req.Query, req.Limit)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"active":  ss.IsRunning(),
		"query":   req.Query,
		"count":   len(results),
		"results": results,
	})
}

// [v0.7.1] handleToolTabComplete GET /api/v1/tool/tab-complete — Tab 补全
func (a *App) handleToolTabComplete(w http.ResponseWriter, r *http.Request) {
	input := r.URL.Query().Get("input")
	if input == "" {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"candidates": []string{},
		})
		return
	}

	exe := a.chatManager.GetExecutor()
	ss, err := exe.GetShellSession()
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"candidates": []string{},
			"error":      err.Error(),
		})
		return
	}

	candidates := ss.TabComplete(input)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"active":     ss.IsRunning(),
		"input":      input,
		"count":      len(candidates),
		"candidates": candidates,
	})
}

// ========== [v0.7.1] MCP 管理端点 ==========

// handleMCPServers GET /api/v1/mcp/servers — 列出所有已连接的 MCP Server
func (a *App) handleMCPServers(w http.ResponseWriter, r *http.Request) {
	if a.mcpManager == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"servers": []interface{}{},
			"total":   0,
		})
		return
	}
	servers := a.mcpManager.ListServers()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"servers": servers,
		"total":   len(servers),
	})
}

// handleMCPAddServer POST /api/v1/mcp/servers — 动态添加 MCP Server
func (a *App) handleMCPAddServer(w http.ResponseWriter, r *http.Request) {
	if a.mcpManager == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "MCP Manager 未初始化"})
		return
	}
	var cfg types.MCPServerConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid JSON: " + err.Error()})
		return
	}
	if cfg.Name == "" || cfg.Command == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name 和 command 必填"})
		return
	}
	cfg.Enabled = true // 动态添加默认启用

	if err := a.mcpManager.AddServer(cfg); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	a.addLog(fmt.Sprintf("【MCP】动态添加 Server '%s'", cfg.Name))
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":  "ok",
		"message": fmt.Sprintf("Server '%s' 已启动", cfg.Name),
	})
}

// handleMCPRemoveServer DELETE /api/v1/mcp/servers/{name} — 移除 MCP Server
func (a *App) handleMCPRemoveServer(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name 参数缺失"})
		return
	}
	if a.mcpManager == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "MCP Manager 未初始化"})
		return
	}
	if err := a.mcpManager.RemoveServer(name); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	a.addLog(fmt.Sprintf("【MCP】移除 Server '%s'", name))
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":  "ok",
		"message": fmt.Sprintf("Server '%s' 已移除", name),
	})
}

// handleMCPTools GET /api/v1/mcp/tools — 列出所有 MCP 工具（跨所有 Server）
func (a *App) handleMCPTools(w http.ResponseWriter, r *http.Request) {
	if a.mcpManager == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"tools": []interface{}{},
			"total": 0,
		})
		return
	}
	tools := a.mcpManager.GetAllTools()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"tools": tools,
		"total": len(tools),
	})
}

// handleMCPCallTool POST /api/v1/mcp/call — 调用 MCP 工具（直接调用，不经过 PM/SE）
func (a *App) handleMCPCallTool(w http.ResponseWriter, r *http.Request) {
	if a.mcpManager == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "MCP Manager 未初始化"})
		return
	}
	var req struct {
		ServerName string                 `json:"server_name"`
		ToolName   string                 `json:"tool_name"`
		Arguments  map[string]interface{} `json:"arguments,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid JSON: " + err.Error()})
		return
	}
	if req.ServerName == "" || req.ToolName == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "server_name 和 tool_name 必填"})
		return
	}

	result, err := a.mcpManager.CallTool(req.ServerName, req.ToolName, req.Arguments)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"error": err.Error(),
		})
		return
	}

	// 提取文本内容
	var textContent []string
	for _, block := range result.Content {
		if block.Type == "text" && block.Text != "" {
			textContent = append(textContent, block.Text)
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status": func() string {
			if result.IsError {
				return "error"
			}
			return "ok"
		}(),
		"is_error": result.IsError,
		"content":  textContent,
		"raw":      result.Content,
	})
}

// handlePendingQueue 获取待发送消息队列
func (a *App) handlePendingQueue(w http.ResponseWriter, r *http.Request) {
	if a.chatManager == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "error", "error": "未初始化"})
		return
	}
	queue := a.chatManager.GetPendingQueue()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"count":    len(queue),
		"messages": queue,
	})
}

// handleClearPending 清空待发送消息队列
func (a *App) handleClearPending(w http.ResponseWriter, r *http.Request) {
	if a.chatManager == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "error", "error": "未初始化"})
		return
	}
	a.chatManager.ClearPendingQueue()
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "message": "待发送队列已清空"})
}

// handleSendPending 立即发送待发送消息（发送第一条）
func (a *App) handleSendPending(w http.ResponseWriter, r *http.Request) {
	if a.chatManager == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "error", "error": "未初始化"})
		return
	}
	msg := a.chatManager.PopAndSendPending()
	if msg == "" {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "message": "队列为空"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"status": "ok", "message": "消息已发送", "content": "msg"})
}

// ========== [v0.7.2] Debugger DAP Handler 实现 ==========

func (a *App) handleDebugStart(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Program     string   `json:"program"`
		Mode        string   `json:"mode"`
		Args        []string `json:"args"`
		StopOnEntry bool     `json:"stop_on_entry"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.Mode == "" {
		req.Mode = "test"
	}

	session, err := a.debuggerMgr.StartDebug(req.Program, req.Mode, req.Args, req.StopOnEntry)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, session)
}

func (a *App) handleDebugStop(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SessionID string `json:"session_id"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if req.SessionID == "" {
		a.debuggerMgr.StopAll()
		writeJSON(w, http.StatusOK, map[string]string{"status": "all sessions stopped"})
		return
	}
	if err := a.debuggerMgr.StopDebug(req.SessionID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "stopped"})
}

func (a *App) handleDebugSessions(w http.ResponseWriter, r *http.Request) {
	sessions := a.debuggerMgr.GetAllSessions()
	writeJSON(w, http.StatusOK, map[string]interface{}{"sessions": sessions, "count": len(sessions)})
}

func (a *App) handleDebugStatus(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		sessions := a.debuggerMgr.GetAllSessions()
		if len(sessions) == 0 {
			writeJSON(w, http.StatusOK, map[string]interface{}{"running": false})
			return
		}
		sessionID = sessions[len(sessions)-1].ID
	}
	session, err := a.debuggerMgr.GetSession(sessionID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	state := session.Client.CurrentState()
	writeJSON(w, http.StatusOK, state)
}

func (a *App) handleDebugSetBreakpoint(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SessionID string `json:"session_id"`
		File      string `json:"file_path"`
		Line      int    `json:"line"`
		Condition string `json:"condition,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	bp, err := a.debuggerMgr.SetBreakpoint(sessionOrDefault(req.SessionID, a), req.File, req.Line, req.Condition)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, bp)
}

func (a *App) handleDebugRemoveBreakpoint(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SessionID string `json:"session_id"`
		File      string `json:"file_path"`
		Line      int    `json:"line"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	err := a.debuggerMgr.RemoveBreakpoint(sessionOrDefault(req.SessionID, a), req.File, req.Line)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}

func (a *App) handleDebugBreakpoints(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session_id")
	bps, err := a.debuggerMgr.GetBreakpoints(sessionOrDefault(sessionID, a))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"breakpoints": bps})
}

func (a *App) handleDebugContinue(w http.ResponseWriter, r *http.Request) {
	sessionID := sessionIDFromRequest(r)
	if err := a.debuggerMgr.Continue(sessionID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "continued"})
}

func (a *App) handleDebugStepOver(w http.ResponseWriter, r *http.Request) {
	sessionID := sessionIDFromRequest(r)
	if err := a.debuggerMgr.Next(sessionID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "step_over"})
}

func (a *App) handleDebugStepInto(w http.ResponseWriter, r *http.Request) {
	sessionID := sessionIDFromRequest(r)
	if err := a.debuggerMgr.StepIn(sessionID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "step_into"})
}

func (a *App) handleDebugStepOut(w http.ResponseWriter, r *http.Request) {
	sessionID := sessionIDFromRequest(r)
	if err := a.debuggerMgr.StepOut(sessionID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "step_out"})
}

func (a *App) handleDebugPause(w http.ResponseWriter, r *http.Request) {
	sessionID := sessionIDFromRequest(r)
	if err := a.debuggerMgr.Pause(sessionID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "paused"})
}

func (a *App) handleDebugStacktrace(w http.ResponseWriter, r *http.Request) {
	sessionID := sessionIDFromRequest(r)
	frames, err := a.debuggerMgr.GetCallStack(sessionID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"frames": frames, "count": len(frames)})
}

func (a *App) handleDebugVariables(w http.ResponseWriter, r *http.Request) {
	sessionID := sessionIDFromRequest(r)
	varsMap, err := a.debuggerMgr.GetVariables(sessionID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, varsMap)
}

func (a *App) handleDebugEvaluate(w http.ResponseWriter, r *http.Request) {
	sessionID := sessionIDFromRequest(r)
	var req struct {
		Expression string `json:"expression"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	result, err := a.debuggerMgr.EvaluateExpression(sessionID, req.Expression)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// sessionOrDefault 返回指定的 session ID 或最新的活跃 session
func sessionOrDefault(id string, a *App) string {
	if id != "" {
		return id
	}
	sessions := a.debuggerMgr.GetAllSessions()
	if len(sessions) > 0 {
		return sessions[len(sessions)-1].ID
	}
	return ""
}

// sessionIDFromRequest 从请求中提取 session_id（query param 或 body）
func sessionIDFromRequest(r *http.Request) string {
	id := r.URL.Query().Get("session_id")
	if id == "" {
		var req struct {
			SessionID string `json:"session_id"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		id = req.SessionID
	}
	return id
}

// ========== [v0.7.2] Context Window / Token Handler 实现 ==========

func (a *App) handleTokenStats(w http.ResponseWriter, r *http.Request) {
	if a.contextWindow == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "context window not initialized"})
		return
	}
	stats := a.contextWindow.TokenStats()
	writeJSON(w, http.StatusOK, stats)
}

func (a *App) handleTokenManage(w http.ResponseWriter, r *http.Request) {
	if a.contextWindow == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "context window not initialized"})
		return
	}
	actionTaken, detail := a.contextWindow.ManageIfNeeded()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"action_taken": actionTaken,
		"detail":       detail,
	})
}

func (a *App) handleTokenClear(w http.ResponseWriter, r *http.Request) {
	if a.contextWindow == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "context window not initialized"})
		return
	}
	a.contextWindow.Clear()
	writeJSON(w, http.StatusOK, map[string]string{"status": "cleared"})
}

func (a *App) handleTokenCount(w http.ResponseWriter, r *http.Request) {
	text := r.URL.Query().Get("text")
	if text == "" {
		var req struct {
			Text string `json:"text"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		text = req.Text
	}
	if text == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "text parameter required"})
		return
	}

	counter := memory.NewTokenCounter()
	count := counter.CountTokens(text)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"text":        text[:min(len(text), 200)] + "...",
		"char_count":  len(text),
		"rune_count":  len([]rune(text)),
		"token_count": count,
	})
}

func (a *App) handleTokenPrune(w http.ResponseWriter, r *http.Request) {
	if a.contextWindow == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "context window not initialized"})
		return
	}
	var req struct {
		MaxTokens int `json:"max_tokens"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if req.MaxTokens <= 0 {
		req.MaxTokens = 100000 // default
	}
	pruned := a.contextWindow.PruneToLimit(req.MaxTokens)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"pruned": pruned,
		"status": fmt.Sprintf("pruned %d messages", pruned),
	})
}

func writeJSON(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(data)
}
