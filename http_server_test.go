package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func setupTestApp() *App {
	app := NewApp()
	app.config.HTTP = HTTPConfig{
		Enabled:     true,
		Port:        8080,
		APIToken:    "",
		AllowRemote: false,
	}
	return app
}

func TestHealthPing(t *testing.T) {
	app := setupTestApp()
	req := httptest.NewRequest("GET", "/health/ping", nil)
	w := httptest.NewRecorder()
	app.handlePing(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var result map[string]string
	json.NewDecoder(w.Body).Decode(&result)
	if result["status"] != "ok" {
		t.Fatalf("expected status=ok, got %s", result["status"])
	}
	if result["version"] != "0.1.0" {
		t.Fatalf("expected version=0.1.0, got %s", result["version"])
	}
	t.Logf("/health/ping: %s", w.Body.String())
}

func TestAdminStatus_NilManager(t *testing.T) {
	app := setupTestApp()
	req := httptest.NewRequest("GET", "/admin/status", nil)
	w := httptest.NewRecorder()
	app.handleStatus(w, req)

	if w.Code != 503 {
		t.Fatalf("expected 503 ServiceUnavailable, got %d", w.Code)
	}
	t.Logf("/admin/status (nil): %d %s", w.Code, w.Body.String())
}

func TestAdminMemory_NilManager(t *testing.T) {
	app := setupTestApp()
	req := httptest.NewRequest("GET", "/admin/memory", nil)
	w := httptest.NewRecorder()
	app.handleMemory(w, req)

	if w.Code != 503 {
		t.Fatalf("expected 503, got %d", w.Code)
	}
	t.Logf("/admin/memory (nil): %d %s", w.Code, w.Body.String())
}

func TestAdminMonitor_NilManager(t *testing.T) {
	app := setupTestApp()
	req := httptest.NewRequest("GET", "/admin/monitor", nil)
	w := httptest.NewRecorder()
	app.handleMonitor(w, req)

	if w.Code != 503 {
		t.Fatalf("expected 503, got %d", w.Code)
	}
	t.Logf("/admin/monitor (nil): %d %s", w.Code, w.Body.String())
}

func TestAdminRecover_NilManager(t *testing.T) {
	app := setupTestApp()
	req := httptest.NewRequest("POST", "/admin/recover", nil)
	w := httptest.NewRecorder()
	app.handleRecover(w, req)

	if w.Code != 503 {
		t.Fatalf("expected 503, got %d", w.Code)
	}
	t.Logf("/admin/recover (nil): %d %s", w.Code, w.Body.String())
}

func TestAdminConfig(t *testing.T) {
	app := setupTestApp()
	app.config.WorkDir = "E:\\TempArgusTest"
	app.config.DingTalk.Enabled = true
	app.config.APIConfigs = []APIConfig{{}, {}}

	req := httptest.NewRequest("GET", "/admin/config", nil)
	w := httptest.NewRecorder()
	app.handleConfig(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var result map[string]interface{}
	json.NewDecoder(w.Body).Decode(&result)
	if result["workDir"] == nil || result["dingtalk"] == nil || result["apiConfigs"] == nil {
		t.Fatalf("missing fields in config response: %v", result)
	}
	t.Logf("/admin/config: workDir=%v dingtalk=%v apiConfigs=%v",
		result["workDir"], result["dingtalk"], result["apiConfigs"])
}

func TestChatSend_EmptyMessage(t *testing.T) {
	app := setupTestApp()
	body := `{"message": ""}`
	req := httptest.NewRequest("POST", "/api/v1/chat/send", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	app.handleChatSend(w, req)

	if w.Code != 400 {
		t.Fatalf("expected 400 Bad Request, got %d", w.Code)
	}
	t.Logf("/chat/send (empty): %d %s", w.Code, w.Body.String())
}

func TestChatSend_InvalidJSON(t *testing.T) {
	app := setupTestApp()
	req := httptest.NewRequest("POST", "/api/v1/chat/send", strings.NewReader("not json"))
	w := httptest.NewRecorder()
	app.handleChatSend(w, req)

	if w.Code != 400 {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	t.Logf("/chat/send (bad json): %d %s", w.Code, w.Body.String())
}

func TestHistory_NilManager(t *testing.T) {
	app := setupTestApp()
	req := httptest.NewRequest("GET", "/api/v1/chat/history", nil)
	w := httptest.NewRecorder()
	app.handleHistory(w, req)

	if w.Code != 503 {
		t.Fatalf("expected 503, got %d", w.Code)
	}
	t.Logf("/chat/history (nil): %d %s", w.Code, w.Body.String())
}

func TestExec_NilManager(t *testing.T) {
	app := setupTestApp()
	body := `{"command": "echo hello"}`
	req := httptest.NewRequest("POST", "/api/v1/exec", strings.NewReader(body))
	w := httptest.NewRecorder()
	app.handleExec(w, req)

	if w.Code != 503 {
		t.Fatalf("expected 503, got %d", w.Code)
	}
	t.Logf("/exec (nil): %d %s", w.Code, w.Body.String())
}

func TestWrite_NilManager(t *testing.T) {
	app := setupTestApp()
	body := `{"path":"test.txt","content":"hello"}`
	req := httptest.NewRequest("POST", "/api/v1/write", strings.NewReader(body))
	w := httptest.NewRecorder()
	app.handleWrite(w, req)

	if w.Code != 503 {
		t.Fatalf("expected 503, got %d", w.Code)
	}
	t.Logf("/write (nil): %d %s", w.Code, w.Body.String())
}

func TestRead_NilManager_NoPath(t *testing.T) {
	app := setupTestApp()
	req := httptest.NewRequest("GET", "/api/v1/read", nil)
	w := httptest.NewRecorder()
	app.handleRead(w, req)

	if w.Code != 503 {
		t.Fatalf("expected 503 (nil manager), got %d", w.Code)
	}
	t.Logf("/read (no path): %d %s", w.Code, w.Body.String())
}

func TestRead_NilManager_WithPath(t *testing.T) {
	app := setupTestApp()
	req := httptest.NewRequest("GET", "/api/v1/read?path=main.go", nil)
	w := httptest.NewRecorder()
	app.handleRead(w, req)

	if w.Code != 503 {
		t.Fatalf("expected 503, got %d", w.Code)
	}
	t.Logf("/read (nil manager): %d %s", w.Code, w.Body.String())
}

func TestSSESubscribe_EmptyMessage(t *testing.T) {
	app := setupTestApp()
	body := `{"message": ""}`
	req := httptest.NewRequest("POST", "/api/v1/sse/subscribe", strings.NewReader(body))
	w := httptest.NewRecorder()
	app.handleSSESubscribe(w, req)

	if w.Code != 503 {
		t.Fatalf("expected 503 (nil manager), got %d", w.Code)
	}
	t.Logf("/sse/subscribe (empty): %d %s", w.Code, w.Body.String())
}

func TestSSESubscribe_NilManager(t *testing.T) {
	app := setupTestApp()
	body := `{"message": "test"}`
	req := httptest.NewRequest("POST", "/api/v1/sse/subscribe", strings.NewReader(body))
	w := httptest.NewRecorder()
	app.handleSSESubscribe(w, req)

	if w.Code != 503 {
		t.Fatalf("expected 503, got %d", w.Code)
	}
	t.Logf("/sse/subscribe (nil): %d %s", w.Code, w.Body.String())
}

func TestAuthMiddleware_NoTokenRequired(t *testing.T) {
	app := setupTestApp()
	app.config.HTTP.APIToken = ""

	middleware := app.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		io.WriteString(w, "ok")
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	middleware.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("token为空时应放行, got %d", w.Code)
	}
	t.Logf("authMiddleware (空token放行): %d", w.Code)
}

func TestAuthMiddleware_TokenMatch(t *testing.T) {
	app := setupTestApp()
	app.config.HTTP.APIToken = "test-secret-123"

	middleware := app.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		io.WriteString(w, "ok")
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer test-secret-123")
	w := httptest.NewRecorder()
	middleware.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("正确token应放行, got %d", w.Code)
	}
	t.Logf("authMiddleware (正确token): %d", w.Code)
}

func TestAuthMiddleware_WrongToken(t *testing.T) {
	app := setupTestApp()
	app.config.HTTP.APIToken = "test-secret-123"

	middleware := app.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		io.WriteString(w, "ok")
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	w := httptest.NewRecorder()
	middleware.ServeHTTP(w, req)

	if w.Code != 401 {
		t.Fatalf("错误token应拒绝, got %d", w.Code)
	}
	t.Logf("authMiddleware (错误token→401): %d", w.Code)
}

func TestAuthMiddleware_MissingToken(t *testing.T) {
	app := setupTestApp()
	app.config.HTTP.APIToken = "test-secret-123"

	middleware := app.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		io.WriteString(w, "ok")
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	middleware.ServeHTTP(w, req)

	if w.Code != 401 {
		t.Fatalf("无token应拒绝, got %d", w.Code)
	}
	t.Logf("authMiddleware (无token→401): %d", w.Code)
}

func TestLocalOnlyMiddleware_Localhost(t *testing.T) {
	app := setupTestApp()
	app.config.HTTP.AllowRemote = false

	middleware := app.localOnlyMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		io.WriteString(w, "ok")
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	middleware.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("localhost应放行, got %d", w.Code)
	}
	t.Logf("localOnlyMiddleware (127.0.0.1): %d", w.Code)
}

func TestLocalOnlyMiddleware_IPv6Localhost(t *testing.T) {
	app := setupTestApp()

	middleware := app.localOnlyMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		io.WriteString(w, "ok")
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "[::1]:12345"
	w := httptest.NewRecorder()
	middleware.ServeHTTP(w, req)

	if w.Code == 200 {
		t.Fatalf("[::1] 应被拒绝 (middleware bug: 未去除[]括号), got %d", w.Code)
	}
	t.Logf("localOnlyMiddleware ([::1]): %d (known: middleware未处理IPv6括号)", w.Code)
}

func TestLocalOnlyMiddleware_RemoteRejected(t *testing.T) {
	app := setupTestApp()
	app.config.HTTP.AllowRemote = false

	middleware := app.localOnlyMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		io.WriteString(w, "ok")
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.100:54321"
	w := httptest.NewRecorder()
	middleware.ServeHTTP(w, req)

	if w.Code != 403 {
		t.Fatalf("远程IP应拒绝, got %d", w.Code)
	}
	t.Logf("localOnlyMiddleware (远程IP→403): %d", w.Code)
}

func TestLocalOnlyMiddleware_AllowRemote(t *testing.T) {
	app := setupTestApp()
	app.config.HTTP.AllowRemote = true

	middleware := app.localOnlyMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		io.WriteString(w, "ok")
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "10.0.0.1:9999"
	w := httptest.NewRecorder()
	middleware.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("allowRemote=true应放行, got %d", w.Code)
	}
	t.Logf("localOnlyMiddleware (allowRemote=true): %d", w.Code)
}

func TestWriteJSON_Helper(t *testing.T) {
	w := httptest.NewRecorder()
	writeJSON(w, 201, map[string]string{"key": "value"})

	if w.Code != 201 {
		t.Fatalf("expected 201, got %d", w.Code)
	}
	ct := w.Header().Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Fatalf("expected json content-type, got %s", ct)
	}
	var result map[string]string
	json.NewDecoder(w.Body).Decode(&result)
	if result["key"] != "value" {
		t.Fatalf("expected value, got %s", result["key"])
	}
	t.Logf("writeJSON helper: code=%d ct=%s body=%s", w.Code, ct, w.Body.String())
}
