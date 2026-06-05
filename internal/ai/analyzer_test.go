package ai

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAnalyzeNilSafety 测试 nil 安全和错误处理检测
func TestAnalyzeNilSafety(t *testing.T) {
	tmpDir := t.TempDir()

	// 写入包含多种问题的测试文件（用小写函数名避免 STYLE001 干扰）
	code := `package test

import (
	"os"
	"encoding/json"
)

func badErrorHandling() {
	f, _ := os.Open("test.txt") // 错误被 _ 丢弃
	data, _ := json.Marshal(f)   // 错误被 _ 丢弃
	_ = data
}

func goodCode() int {
	return 1 + 1
}
`
	writeTestFile(t, tmpDir, "nil_test.go", code)

	analyzer := NewCodeAnalyzer(tmpDir)
	result, err := analyzer.Analyze(AnalyzeOptions{Path: "nil_test.go"})
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	if len(result.Issues) == 0 {
		t.Fatal("Expected at least 1 issue, got 0")
	}

	// 检查是否有 error_handling 类别（_ = err 模式）
	found := false
	for _, iss := range result.Issues {
		if iss.Category == CatErrorHandling || iss.RuleID == "ERR004" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected error_handling issue (ERR004), got categories: %+v", extractCategories(result.Issues))
	}
}

// TestAnalyzePanicInCode 测试 panic 检测（正则模式）
func TestAnalyzePanicInCode(t *testing.T) {
	tmpDir := t.TempDir()

	code := `package test

func BadPanic(x int) {
	if x < 0 {
		panic("negative value")
	}
}
`
	writeTestFile(t, tmpDir, "panic_test.go", code)

	analyzer := NewCodeAnalyzer(tmpDir)
	result, err := analyzer.Analyze(AnalyzeOptions{Path: "panic_test.go", MinLevel: "warning"})
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	found := false
	for _, iss := range result.Issues {
		if strings.Contains(iss.RuleID, "ERR005") || strings.Contains(iss.Title, "panic") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected panic detection issue, got %d issues: %s", len(result.Issues), result.FormatResults())
	}
}

// TestAnalyzeWeakCrypto 测试弱加密检测
func TestAnalyzeWeakCrypto(t *testing.T) {
	tmpDir := t.TempDir()

	code := `package test

import (
	"crypto/md5"
	"encoding/hex"
)

func WeakHash(data []byte) string {
	h := md5.New()
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}
`
	writeTestFile(t, tmpDir, "crypto_test.go", code)

	analyzer := NewCodeAnalyzer(tmpDir)
	result, err := analyzer.Analyze(AnalyzeOptions{Path: "crypto_test.go"})
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	found := false
	for _, iss := range result.Issues {
		if iss.Category == CatSecurity && strings.Contains(iss.RuleID, "SEC001") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected weak crypto issue (SEC001), got issues: %s", extractRuleIDs(result.Issues))
	}
}

// TestAnalyzeGoroutine 测试 goroutine 安全检测
func TestAnalyzeGoroutine(t *testing.T) {
	tmpDir := t.TempDir()

	code := `package test

import "time"

func SpawnWorker() {
	go func() {
		time.Sleep(time.Second)
	}()
}
`
	writeTestFile(t, tmpDir, "goroutine_test.go", code)

	analyzer := NewCodeAnalyzer(tmpDir)
	result, err := analyzer.Analyze(AnalyzeOptions{Path: "goroutine_test.go", MinLevel: "warning"})
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	found := false
	for _, iss := range result.Issues {
		if iss.Category == CatConcurrency && strings.Contains(iss.RuleID, "GOR001") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected goroutine safety issue (GOR001), got: %s", extractRuleIDs(result.Issues))
	}
}

// TestAnalyzeHTTPNoTimeout 测试 HTTP 无超时检测
func TestAnalyzeHTTPNoTimeout(t *testing.T) {
	tmpDir := t.TempDir()

	code := `package test

import "net/http"

func FetchData() (*http.Response, error) {
	return http.Get("https://example.com")
}
`
	writeTestFile(t, tmpDir, "http_test.go", code)

	analyzer := NewCodeAnalyzer(tmpDir)
	result, err := analyzer.Analyze(AnalyzeOptions{Path: "http_test.go", MinLevel: "warning"})
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	found := false
	for _, iss := range result.Issues {
		if strings.Contains(iss.RuleID, "RES004") || (iss.Category == CatResource && strings.Contains(iss.Title, "HTTP")) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected HTTP timeout issue (RES004), got: %s", extractRuleIDs(result.Issues))
	}
}

// TestAnalyzeDeferInLoop 测试循环内 defer 检测
func TestAnalyzeDeferInLoop(t *testing.T) {
	tmpDir := t.TempDir()

	code := `package test

import "os"

func ProcessFiles(files []string) error {
	for _, f := range files {
		fd, err := os.Open(f)
		if err != nil {
			return err
		}
		defer fd.Close() // BUG: defer in loop!
		// process fd...
	}
	return nil
}
`
	writeTestFile(t, tmpDir, "defer_loop.go", code)

	analyzer := NewCodeAnalyzer(tmpDir)
	result, err := analyzer.Analyze(AnalyzeOptions{Path: "defer_loop.go", MinLevel: "critical"})
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	found := false
	for _, iss := range result.Issues {
		if strings.Contains(iss.RuleID, "RES001") || (iss.Severity == SeverityCritical && strings.Contains(iss.Title, "defer")) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected defer-in-loop critical issue (RES001), got: %s", extractRuleIDs(result.Issues))
	}
}

// TestAnalyzeCleanCode 测试干净代码无问题
func TestAnalyzeCleanCode(t *testing.T) {
	tmpDir := t.TempDir()

	code := `package test

// Add adds two integers and returns the result.
func Add(a, b int) int {
	return a + b
}
`
	writeTestFile(t, tmpDir, "clean.go", code)

	analyzer := NewCodeAnalyzer(tmpDir)
	result, err := analyzer.Analyze(AnalyzeOptions{Path: "clean.go", MinLevel: "warning"})
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	// 干净代码在 warning 级别应该没有问题（hint 级别的 style 提示可能有）
	warningCount := 0
	for _, iss := range result.Issues {
		if iss.Severity == SeverityCritical || iss.Severity == SeverityWarning {
			warningCount++
		}
	}
	if warningCount > 0 {
		t.Errorf("Clean code should have 0 warning+ issues, got %d: %s", warningCount, result.FormatResults())
	}
}

// TestAnalyzeDirectory 测试目录扫描
func TestAnalyzeDirectory(t *testing.T) {
	tmpDir := t.TempDir()

	writeTestFile(t, tmpDir, "a.go", `package test

func A() {
	panic("oops")
}`)
	writeTestFile(t, tmpDir, "b.go", `package test

import "crypto/md5"

func B() { _ = md5.New() }`)

	analyzer := NewCodeAnalyzer(tmpDir)
	result, err := analyzer.Analyze(AnalyzeOptions{Path: ".", MaxIssues: 10})
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	if result.FileCount < 2 {
		t.Errorf("Expected at least 2 files scanned, got %d", result.FileCount)
	}
	if len(result.Issues) == 0 {
		t.Error("Expected at least 1 issue from directory scan")
	}
}

// TestAnalyzeCategoryFilter 测试分类过滤
func TestAnalyzeCategoryFilter(t *testing.T) {
	tmpDir := t.TempDir()

	code := `package test

import (
	"crypto/md5"
	"sync"
)

func MixedIssues() {
	_ = md5.New()   // security issue
	_ = sync.Mutex{} // concurrency info
	panic("test")    // error handling warning
}
`
	writeTestFile(t, tmpDir, "mixed.go", code)

	analyzer := NewCodeAnalyzer(tmpDir)

	// 只查 security 分类
	result, err := analyzer.Analyze(AnalyzeOptions{
		Path:      "mixed.go",
		Categories: []string{"security"},
	})
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	// 所有结果都应该是 security 类别
	for _, iss := range result.Issues {
		if iss.Category != CatSecurity {
			t.Errorf("Expected only security issues, got category=%s rule=%s", iss.Category, iss.RuleID)
		}
	}
}

// TestFormatResults 测试输出格式化
func TestFormatResults(t *testing.T) {
	result := &AnalyzeResult{
		Summary: SummaryStats{
			Total:     3,
			Critical:  1,
			Warning:   1,
			Info:      1,
			Categories: map[string]int{"error_handling": 1, "security": 1, "style": 1},
		},
		Issues: []AnalysisIssue{
			{File: "test.go", Line: 10, Severity: SeverityCritical, RuleID: "NIL001", Title: "nil panic risk"},
			{File: "test.go", Line: 20, Severity: SeverityWarning, RuleID: "SEC001", Title: "weak crypto"},
			{File: "test.go", Line: 30, Severity: SeverityInfo, RuleID: "STYLE001", Title: "no comment"},
		},
		FileCount: 1,
	}

	output := result.FormatResults()
	if output == "" {
		t.Fatal("FormatResults returned empty")
	}
	if !strings.Contains(output, "3") {
		t.Errorf("Expected total count 3 in output, got: %s", output)
	}
	if !strings.Contains(output, "NIL001") {
		t.Errorf("Expected NIL001 in output, got: %s", output)
	}
	if !strings.Contains(output, "SEC001") {
		t.Errorf("Expected SEC001 in output, got: %s", output)
	}
}

// TestFormatResultsEmpty 测试空结果格式化
func TestFormatResultsEmpty(t *testing.T) {
	result := &AnalyzeResult{
		Issues:    []AnalysisIssue{},
		FileCount: 0,
	}
	output := result.FormatResults()
	if !strings.Contains(output, "未发现问题") {
		t.Errorf("Expected clean message, got: %s", output)
	}
}

// ========== 辅助函数 ==========

func writeTestFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write test file %s: %v", path, err)
	}
}

func extractCategories(issues []AnalysisIssue) []string {
	cats := make([]string, len(issues))
	for i, iss := range issues {
		cats[i] = string(iss.Category)
	}
	return cats
}

func extractRuleIDs(issues []AnalysisIssue) []string {
	ids := make([]string, len(issues))
	for i, iss := range issues {
		ids[i] = iss.RuleID
	}
	return ids
}
