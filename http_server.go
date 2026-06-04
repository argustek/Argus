package main

import (
	"argus/internal/executor"
	"encoding/json"
	"fmt"
	"net/http"
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
	fmt.Printf("[HTTPServer]   POST /subscribe     SSE流式推送\n")
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
}

func (a *App) registerSSERoutes(mux *http.ServeMux) {
	// ✅ SSE 订阅端点已启用，用于调试
	mux.HandleFunc("POST /api/v1/sse/subscribe", a.authMiddleware(http.HandlerFunc(a.handleSSESubscribe)).ServeHTTP)
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
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid JSON"})
		return
	}
	if req.Message == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "message is required"})
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
	ch, ok2 := bridge.Subscribe(id)
	if !ok2 {
		writeJSON(w, http.StatusConflict, map[string]string{"status": "error", "error": "已有一个活跃的SSE连接，请稍后重试"})
		return
	}
	defer bridge.Unsubscribe(id)

	fmt.Fprintf(w, ": connected\n\n")
	flusher.Flush()

	fmt.Printf("[HTTPServer/SSE] SendMessage: %s\n", req.Message)
	if err := a.SendMessage(req.Message); err != nil {
		fmt.Fprintf(w, "event: error\ndata: {\"error\":\"%s\",\"stage\":\"system\"}\n\n", err.Error())
		flusher.Flush()
		return
	}

	for {
		select {
		case event, ok3 := <-ch:
			if !ok3 {
				return
			}
			fmt.Fprintf(w, "event: %s\n", event.Type)
			jsonData, _ := json.Marshal(event.Data)
			fmt.Fprintf(w, "data: %s\n\n", string(jsonData))
			flusher.Flush()
			if event.Type == "done" || event.Type == "error" {
				return
			}
		case <-time.After(120 * time.Second):
			fmt.Fprintf(w, "event: error\ndata: {\"error\":\"timeout\"}\n\n")
			flusher.Flush()
			return
		}
	}
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
		"success":      true,
		"pattern":      req.Pattern,
		"count":        len(matches),
		"total_scanned": result.FilesSearched,
		"matches":      matches,
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
			"active":  false,
			"error":   err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"active": ss.IsRunning(),
		"cwd":    ss.CWD(),
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
	writeJSON(w, http.StatusOK, map[string]interface{}{"status": "ok", "message": "消息已发送", "content": msg})
}

func writeJSON(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(data)
}
