package ai

import (
	"fmt"
	"strings"
)

// Snippet 代码片段定义
type Snippet struct {
	Name        string   `json:"name"`        // 片段名（如 "Go HTTP server"）
	Language    string   `json:"language"`    // 语言
	Description string   `json:"description"` // 描述
	Tags        []string `json:"tags"`        // 标签（用于搜索匹配）
	Code        string   `json:"code"`        // 代码内容
}

// SnippetStore 代码片段库
type SnippetStore struct {
	snippets []Snippet
}

// NewSnippetStore 创建带预设片段的片段库
func NewSnippetStore() *SnippetStore {
	return &SnippetStore{snippets: defaultSnippets()}
}

// Search 按关键词搜索片段
func (s *SnippetStore) Search(query string) []Snippet {
	ql := strings.ToLower(query)
	var matched []Snippet
	for _, sn := range s.snippets {
		score := 0
		if strings.Contains(strings.ToLower(sn.Name), ql) {
			score += 3
		}
		if strings.Contains(strings.ToLower(sn.Description), ql) {
			score += 2
		}
		for _, tag := range sn.Tags {
			if strings.Contains(strings.ToLower(tag), ql) {
				score += 2
			}
		}
		if score > 0 {
			matched = append(matched, sn)
		}
	}
	return matched
}

// FormatResults 格式化搜索结果为文本
func (s *SnippetStore) FormatResults(snippets []Snippet) string {
	if len(snippets) == 0 {
		return "未找到匹配的代码片段"
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("找到 %d 个匹配的代码片段:\n\n", len(snippets)))
	for i, sn := range snippets {
		if i > 0 {
			sb.WriteString("\n---\n\n")
		}
		sb.WriteString(fmt.Sprintf("【%s】(%s)\n%s\n\n```%s\n%s\n```\n",
			sn.Name, sn.Language, sn.Description,
			strings.ToLower(sn.Language), sn.Code))
	}
	return sb.String()
}

// Count 返回片段总数
func (s *SnippetStore) Count() int {
	return len(s.snippets)
}

func defaultSnippets() []Snippet {
	return []Snippet{
		{
			Name:        "Go HTTP Server",
			Language:    "Go",
			Description: "基础 HTTP 服务器，带路由和中间件",
			Tags:        []string{"http", "server", "web", "api", "router"},
			Code: `package main

import (
	"fmt"
	"log"
	"net/http"
)

func loggingMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		log.Printf("%s %s", r.Method, r.URL.Path)
		next(w, r)
	}
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, ` + `"` + `{"status":"ok"}` + `"` + `)
	})
	mux.HandleFunc("/api/hello", loggingMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, ` + `"` + `{"message":"Hello, World!"}` + `"` + `)
	}))

	log.Println("Server starting on :8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}`,
		},
		{
			Name:        "Go CRUD API (标准库)",
			Language:    "Go",
			Description: "RESTful CRUD API 示例，内存存储",
			Tags:        []string{"crud", "api", "rest", "http", "database"},
			Code: `package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
)

type Item struct {
	ID    string ` + "`" + `json:"id"` + "`" + `
	Name  string ` + "`" + `json:"name"` + "`" + `
	Value string ` + "`" + `json:"value"` + "`" + `
}

type Store struct {
	mu    sync.RWMutex
	items map[string]Item
}

var store = &Store{items: make(map[string]Item)}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/items", handleItems)
	mux.HandleFunc("/api/items/", handleItem)
	log.Println("CRUD API on :8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}

func handleItems(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		store.mu.RLock()
		defer store.mu.RUnlock()
		items := make([]Item, 0, len(store.items))
		for _, v := range store.items {
			items = append(items, v)
		}
		writeJSON(w, http.StatusOK, items)
	case http.MethodPost:
		var item Item
		if err := json.NewDecoder(r.Body).Decode(&item); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}
		store.mu.Lock()
		store.items[item.ID] = item
		store.mu.Unlock()
		writeJSON(w, http.StatusCreated, item)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, nil)
	}
}

func handleItem(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/items/")
	switch r.Method {
	case http.MethodGet:
		store.mu.RLock()
		item, ok := store.items[id]
		store.mu.RUnlock()
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		writeJSON(w, http.StatusOK, item)
	case http.MethodPut:
		var item Item
		if err := json.NewDecoder(r.Body).Decode(&item); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}
		item.ID = id
		store.mu.Lock()
		store.items[id] = item
		store.mu.Unlock()
		writeJSON(w, http.StatusOK, item)
	case http.MethodDelete:
		store.mu.Lock()
		delete(store.items, id)
		store.mu.Unlock()
		writeJSON(w, http.StatusNoContent, nil)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, nil)
	}
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v != nil {
		json.NewEncoder(w).Encode(v)
	}
}`,
		},
		{
			Name:        "Go Middleware (认证)",
			Language:    "Go",
			Description: "Bearer Token 认证中间件",
			Tags:        []string{"middleware", "auth", "authentication", "token", "jwt"},
			Code: `package main

import (
	"context"
	"fmt"
	"net/http"
	"strings"
)

// 从请求中提取 Bearer token
func extractToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		return ""
	}
	return strings.TrimPrefix(auth, "Bearer ")
}

// AuthMiddleware 认证中间件
func AuthMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := extractToken(r)
		if token == "" {
			http.Error(w, ` + "`" + `{"error":"missing authorization"}` + "`" + `, http.StatusUnauthorized)
			return
		}
		// TODO: 验证 token（JWT 解析 / 数据库查询等）
		// 将用户信息注入 context
		ctx := context.WithValue(r.Context(), "user_token", token)
		next(w, r.WithContext(ctx))
	}
}

// 使用示例
func secureHandler(w http.ResponseWriter, r *http.Request) {
	token := r.Context().Value("user_token").(string)
	fmt.Fprintf(w, ` + "`" + `{"message":"secure data","token":"%s"}` + "`" + `, token)
}`,
		},
		{
			Name:        "Go Database (SQLite)",
			Language:    "Go",
			Description: "SQLite 数据库初始化 + CRUD 封装",
			Tags:        []string{"database", "sqlite", "db", "sql", "storage"},
			Code: `package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"

	_ "github.com/mattn/go-sqlite3"
)

func initDB(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	db.SetMaxOpenConns(1) // SQLite 单写

	if _, err := db.Exec(` + "`" + `CREATE TABLE IF NOT EXISTS items (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		value TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)` + "`" + `); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return db, nil
}

func getItem(db *sql.DB, id string) (idOut, name, value string, err error) {
	err = db.QueryRow("SELECT id, name, value FROM items WHERE id = ?", id).
		Scan(&idOut, &name, &value)
	return
}

func insertItem(db *sql.DB, id, name, value string) error {
	_, err := db.Exec("INSERT OR REPLACE INTO items (id, name, value) VALUES (?, ?, ?)", id, name, value)
	return err
}

func deleteItem(db *sql.DB, id string) error {
	_, err := db.Exec("DELETE FROM items WHERE id = ?", id)
	return err
}`,
		},
		{
			Name:        "Go 单元测试模板",
			Language:    "Go",
			Description: "表驱动测试 + 子测试模板",
			Tags:        []string{"test", "testing", "unit test", "table-driven"},
			Code: `package mypackage

import "testing"

func TestMyFunc(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{"normal case", "hello", "HELLO", false},
		{"empty string", "", "", false},
		{"special chars", "a+b", "A+B", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := MyFunc(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("MyFunc() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("MyFunc() = %v, want %v", got, tt.want)
			}
		})
	}
}`,
		},
		{
			Name:        "Go 并发模式 (Worker Pool)",
			Language:    "Go",
			Description: "固定大小的 worker pool 并发处理",
			Tags:        []string{"concurrency", "goroutine", "worker", "pool", "channel"},
			Code: `package main

import (
	"fmt"
	"sync"
)

type Job struct {
	ID  int
	Data string
}

type Result struct {
	JobID int
	Output string
}

func workerPool(workers int, jobs []Job) []Result {
	jobCh := make(chan Job, len(jobs))
	resultCh := make(chan Result, len(jobs))

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobCh {
				// 处理 job
				output := fmt.Sprintf("processed: %s", job.Data)
				resultCh <- Result{JobID: job.ID, Output: output}
			}
		}()
	}

	for _, j := range jobs {
		jobCh <- j
	}
	close(jobCh)

	wg.Wait()
	close(resultCh)

	var results []Result
	for r := range resultCh {
		results = append(results, r)
	}
	return results
}`,
		},
		{
			Name:        "Go 配置加载 (JSON/YAML)",
			Language:    "Go",
			Description: "从 JSON 文件加载配置结构体",
			Tags:        []string{"config", "configuration", "json", "yaml", "env"},
			Code: `package main

import (
	"encoding/json"
	"fmt"
	"os"
)

type Config struct {
	Server   ServerConfig   ` + "`" + `json:"server"` + "`" + `
	Database DatabaseConfig ` + "`" + `json:"database"` + "`" + `
	LogLevel string         ` + "`" + `json:"log_level"` + "`" + `
}

type ServerConfig struct {
	Host string ` + "`" + `json:"host"` + "`" + `
	Port int    ` + "`" + `json:"port"` + "`" + `
}

type DatabaseConfig struct {
	DSN      string ` + "`" + `json:"dsn"` + "`" + `
	MaxConns int    ` + "`" + `json:"max_conns"` + "`" + `
}

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	// 默认值
	if cfg.Server.Port == 0 {
		cfg.Server.Port = 8080
	}
	if cfg.LogLevel == "" {
		cfg.LogLevel = "info"
	}
	return &cfg, nil
}`,
		},
		{
			Name:        "Go CLI 工具 (flag 解析)",
			Language:    "Go",
			Description: "带 flag 的命令行工具模板",
			Tags:        []string{"cli", "command", "flag", "args", "tool"},
			Code: `package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	var (
		verbose = flag.Bool("v", false, "详细输出")
		output  = flag.String("o", "", "输出文件路径")
		help    = flag.Bool("h", false, "显示帮助")
	)
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "用法: %s [选项] <输入>\n", os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()

	if *help || flag.NArg() == 0 {
		flag.Usage()
		os.Exit(0)
	}

	input := flag.Arg(0)
	if *verbose {
		fmt.Printf("处理输入: %s\n", input)
	}
	result := process(input)

	if *output != "" {
		if err := os.WriteFile(*output, []byte(result), 0644); err != nil {
			fmt.Fprintf(os.Stderr, "写文件失败: %v\n", err)
			os.Exit(1)
		}
	} else {
		fmt.Println(result)
	}
}

func process(input string) string {
	return fmt.Sprintf("处理结果: %s", input)
}`,
		},
	}
}
