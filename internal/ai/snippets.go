package ai

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Snippet 代码片段定义
type Snippet struct {
	ID          string   `json:"id"`           // 唯一ID（UUID或用户指定）
	Name        string   `json:"name"`         // 片段名（如 "Go HTTP server"）
	Language    string   `json:"language"`      // 语言
	Description string   `json:"description"`   // 描述
	Tags        []string `json:"tags"`          // 标签（用于搜索匹配）
	Code        string   `json:"code"`          // 代码内容
	CreatedAt   string   `json:"created_at"`    // 创建时间
	UpdatedAt   string   `json:"updated_at"`    // 更新时间
	IsBuiltin   bool     `json:"is_builtin"`    // 是否内置模板（不可删除）
}

// SnippetStore 代码片段库（持久化 + CRUD）
type SnippetStore struct {
	mu       sync.RWMutex
	snippets map[string]Snippet // key = ID
	filePath string             // 持久化文件路径
}

// NewSnippetStore 创建片段库，从JSON文件加载 + 内置模板
func NewSnippetStore(dataDir string) *SnippetStore {
	fp := filepath.Join(dataDir, "snippets.json")
	s := &SnippetStore{
		snippets: make(map[string]Snippet),
		filePath: fp,
	}
	s.load()      // 从文件加载自定义片段
	s.addBuiltins() // 合并内置模板（不覆盖同名）
	return s
}

// load 从JSON文件加载自定义片段
func (s *SnippetStore) load() {
	data, err := os.ReadFile(s.filePath)
	if err != nil {
		if !os.IsNotExist(err) {
			fmt.Printf("[SnippetStore] 读取文件失败: %v\n", err)
		}
		return
	}
	var custom []Snippet
	if err := json.Unmarshal(data, &custom); err != nil {
		fmt.Printf("[SnippetStore] 解析文件失败: %v\n", err)
		return
	}
	for _, sn := range custom {
		s.snippets[sn.ID] = sn
	}
	fmt.Printf("[SnippetStore] 加载 %d 个自定义片段\n", len(custom))
}

// save 保存到JSON文件（只保存非内置的）
func (s *SnippetStore) save() error {
	var custom []Snippet
	for _, sn := range s.snippets {
		if !sn.IsBuiltin {
			custom = append(custom, sn)
		}
	}
	data, err := json.MarshalIndent(custom, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化失败: %w", err)
	}
	dir := filepath.Dir(s.filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("创建目录失败: %w", err)
	}
	if err := os.WriteFile(s.filePath, data, 0644); err != nil {
		return fmt.Errorf("写入文件失败: %w", err)
	}
	return nil
}

// addBuiltins 合并内置模板（已有同ID的不覆盖）
func (s *SnippetStore) addBuiltins() {
	for _, sn := range defaultSnippets() {
		sn.IsBuiltin = true
		if _, exists := s.snippets[sn.ID]; !exists {
			if sn.CreatedAt == "" {
				sn.CreatedAt = time.Now().Format(time.RFC3339)
				sn.UpdatedAt = sn.CreatedAt
			}
			s.snippets[sn.ID] = sn
		}
	}
}

// ========== CRUD 操作 ==========

// Add 添加新片段
func (s *SnippetStore) Add(snippet Snippet) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if snippet.ID == "" {
		snippet.ID = generateSnippetID()
	}
	if snippet.Name == "" {
		return fmt.Errorf("片段名不能为空")
	}
	now := time.Now().Format(time.RFC3339)
	if snippet.CreatedAt == "" {
		snippet.CreatedAt = now
	}
	snippet.UpdatedAt = now
	snippet.IsBuiltin = false

	s.snippets[snippet.ID] = snippet
	if err := s.save(); err != nil {
		delete(s.snippets, snippet.ID)
		return err
	}
	return nil
}

// Update 更新片段
func (s *SnippetStore) Update(id string, snippet Snippet) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	existing, exists := s.snippets[id]
	if !exists {
		return fmt.Errorf("片段不存在: %s", id)
	}
	if existing.IsBuiltin {
		return fmt.Errorf("内置模板不可修改: %s", id)
	}
	snippet.ID = id
	snippet.CreatedAt = existing.CreatedAt // 保持创建时间
	snippet.UpdatedAt = time.Now().Format(time.RFC3339)
	snippet.IsBuiltin = false

	s.snippets[id] = snippet
	return s.save()
}

// Delete 删除片段（内置不可删）
func (s *SnippetStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	sn, exists := s.snippets[id]
	if !exists {
		return fmt.Errorf("片段不存在: %s", id)
	}
	if sn.IsBuiltin {
		return fmt.Errorf("内置模板不可删除: %s (%s)", id, sn.Name)
	}
	delete(s.snippets, id)
	return s.save()
}

// GetByID 根据ID获取单个片段
func (s *SnippetStore) GetByID(id string) (Snippet, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sn, ok := s.snippets[id]
	return sn, ok
}

// List 列出所有片段（支持按语言过滤）
func (s *SnippetStore) List(language string) []Snippet {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []Snippet
	for _, sn := range s.snippets {
		if language != "" && !strings.EqualFold(sn.Language, language) {
			continue
		}
		result = append(result, sn)
	}
	// 按名称排序
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result
}

// ListByTags 按标签列出片段
func (s *SnippetStore) ListByTags(tags []string) []Snippet {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []Snippet
	for _, sn := range s.snippets {
		matchCount := 0
		for _, t := range tags {
			for _, st := range sn.Tags {
				if strings.EqualFold(st, t) {
					matchCount++
					break
				}
			}
		}
		if matchCount > 0 {
			result = append(result, sn)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result
}

// Count 返回片段总数（可选按语言）
func (s *SnippetStore) Count(language string) int {
	list := s.List(language)
	return len(list)
}

// Languages 返回所有可用语言列表
func (s *SnippetStore) Languages() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	langSet := make(map[string]bool)
	for _, sn := range s.snippets {
		if sn.Language != "" {
			langSet[strings.ToLower(sn.Language)] = true
		}
	}
	var langs []string
	for l := range langSet {
		langs = append(langs, l)
	}
	sort.Strings(langs)
	return langs
}

// Tags 返回所有标签列表
func (s *SnippetStore) Tags() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tagSet := make(map[string]bool)
	for _, sn := range s.snippets {
		for _, t := range sn.Tags {
			tagSet[strings.ToLower(t)] = true
		}
	}
	var tags []string
	for t := range tagSet {
		tags = append(tags, t)
	}
	sort.Strings(tags)
	return tags
}

// ========== 搜索 ==========

// Search 按关键词搜索片段（支持语言/标签过滤）
type SearchOptions struct {
	Query    string   // 搜索关键词
	Language string   // 语言过滤（可选）
	Tags     []string // 标签过滤（可选，任一匹配即可）
	Limit    int      // 最大返回数（0=不限）
}

func (s *SnippetStore) Search(opts SearchOptions) []Snippet {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ql := strings.ToLower(opts.Query)
	type scoredSnippet struct {
		snippet Snippet
		score   int
	}
	var matched []scoredSnippet

	for _, sn := range s.snippets {
		// 语言过滤
		if opts.Language != "" && !strings.EqualFold(sn.Language, opts.Language) {
			continue
		}
		// 标签过滤（如果指定了tags，必须至少匹配一个）
		if len(opts.Tags) > 0 {
			foundTag := false
			for _, optTag := range opts.Tags {
				for _, snTag := range sn.Tags {
					if strings.EqualFold(snTag, optTag) {
						foundTag = true
						break
					}
				}
				if foundTag {
					break
				}
			}
			if !foundTag {
				continue
			}
		}

		// 关键词评分
		if ql == "" {
			matched = append(matched, scoredSnippet{snippet: sn, score: 1})
			continue
		}

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
		if strings.Contains(strings.ToLower(sn.Code), ql) {
			score += 1
		}
		if score > 0 {
			matched = append(matched, scoredSnippet{snippet: sn, score: score})
		}
	}

	// 按分数降序
	sort.Slice(matched, func(i, j int) bool {
		return matched[i].score > matched[j].score
	})

	// 限制数量
	if opts.Limit > 0 && len(matched) > opts.Limit {
		matched = matched[:opts.Limit]
	}

	result := make([]Snippet, len(matched))
	for i, m := range matched {
		result[i] = m.snippet
	}
	return result
}

// Search 兼容旧接口（仅关键词搜索）
func (s *SnippetStore) SearchSimple(query string) []Snippet {
	return s.Search(SearchOptions{Query: query})
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
		builtinMark := ""
		if sn.IsBuiltin {
			builtinMark = " [内置]"
		}
		tagStr := strings.Join(sn.Tags, ", ")
		sb.WriteString(fmt.Sprintf("【%s】(%s)%s\n%s | 标签: [%s]\nID: %s\n\n```%s\n%s\n```\n",
			sn.Name, sn.Language, builtinMark, sn.Description,
			tagStr, sn.ID,
			strings.ToLower(sn.Language), sn.Code))
	}
	return sb.String()
}

// FormatList 格式化列表为简要文本
func (s *SnippetStore) FormatList(snippets []Snippet) string {
	if len(snippets) == 0 {
		return "暂无代码片段"
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("共 %d 个代码片段:\n\n", len(snippets)))
	for _, sn := range snippets {
		builtinMark := ""
		if sn.IsBuiltin {
			builtinMark = "*"
		}
		sb.WriteString(fmt.Sprintf("  %s %-30s [%-8s] %s (ID:%s)\n",
			builtinMark, sn.Name, sn.Language, sn.Description, sn.ID))
	}
	return sb.String()
}

func defaultSnippets() []Snippet {
	return []Snippet{
		{
			ID:          "builtin-go-http-server",
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
			ID:          "builtin-go-crud-api",
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
			ID:          "builtin-go-middleware-auth",
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
			ID:          "builtin-go-database-sqlite",
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
			ID:          "builtin-go-unit-test",
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
			ID:          "builtin-go-worker-pool",
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
			ID:          "builtin-go-config-loader",
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
			ID:          "builtin-go-cli-tool",
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

// generateSnippetID 生成片段ID（简单递增）
var snippetIDCounter int64

func generateSnippetID() string {
	snippetIDCounter++
	return fmt.Sprintf("sn-%d-%d", time.Now().Unix(), snippetIDCounter)
}
