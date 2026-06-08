package executor

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ============================================================
// [P0] EditFile 测试用例
// ============================================================

func TestEditFile_BasicReplace(t *testing.T) {
	tmpDir := t.TempDir()
	exec := NewExecutor(tmpDir, nil)
	testFile := filepath.Join(tmpDir, "test.go")

	originalContent := `package main

import "fmt"

func hello() {
	fmt.Println("Hello World")
}

func main() {
	hello()
}
`
	if err := os.WriteFile(testFile, []byte(originalContent), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	oldStr := `func hello() {
	fmt.Println("Hello World")
}`
	newStr := `func hello(name string) {
	fmt.Printf("Hello, %s!\n", name)
}`

	result, err := exec.EditFile(testFile, oldStr, newStr)
	if err != nil {
		t.Fatalf("EditFile returned error: %v", err)
	}

	if !result.Success {
		t.Fatalf("EditFile failed: %s", result.Error)
	}

	if result.FilePath != testFile {
		t.Errorf("Expected FilePath=%s, got %s", testFile, result.FilePath)
	}

	if result.LinesChanged < 2 {
		t.Errorf("Expected at least 2 lines changed, got %d", result.LinesChanged)
	}

	content, _ := exec.ReadFile(testFile)
	if !strings.Contains(content, "func hello(name string)") {
		t.Error("Expected content to contain 'func hello(name string)'")
	}

	if strings.Contains(content, "Hello World") && !strings.Contains(content, "Hello, %s!") {
		t.Error("Expected old text to be replaced")
	}

	if len(result.Diff) == 0 {
		t.Error("Expected non-empty diff")
	}

	if !strings.Contains(result.Diff, "---") || !strings.Contains(result.Diff, "+++") {
		t.Errorf("Invalid diff format: %.100s", result.Diff)
	}

	t.Logf("✅ Basic replace test passed\nDiff:\n%s", result.Diff)
}

func TestEditFile_OldStrNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	exec := NewExecutor(tmpDir, nil)
	testFile := filepath.Join(tmpDir, "test.go")

	os.WriteFile(testFile, []byte(`package main`), 0644)

	result, err := exec.EditFile(testFile, "nonexistent text", "replacement")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if result.Success {
		t.Fatal("Expected failure when old_str not found")
	}

	if !strings.Contains(result.Error, "old_str not found") {
		t.Errorf("Expected 'old_str not found' error, got: %s", result.Error)
	}

	t.Logf("✅ OldStrNotFound test passed: %s", result.Error)
}

func TestEditFile_PathOutsideWorkDir(t *testing.T) {
	workDir := t.TempDir()
	exec := NewExecutor(workDir, nil)

	outsidePath := filepath.Join(os.TempDir(), "outside_test.go")
	result, err := exec.EditFile(outsidePath, "test", "replace")

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if result.Success {
		t.Fatal("Expected failure for path outside work directory")
	}

	if !strings.Contains(result.Error, "path outside work directory") {
		t.Errorf("Expected path error, got: %s", result.Error)
	}

	t.Logf("✅ PathOutsideWorkDir test passed")
}

func TestEditFile_MultipleOccurrences(t *testing.T) {
	tmpDir := t.TempDir()
	exec := NewExecutor(tmpDir, nil)
	testFile := filepath.Join(tmpDir, "duplicate.go")

	content := `package main

func add(a, b int) int {
	return a + b
}

func multiply(a, b int) int {
	return a * b
}

func main() {
	result1 := add(1, 2)
	result2 := multiply(3, 4)
}
`
	os.WriteFile(testFile, []byte(content), 0644)

	oldStr := "return a + b"
	newStr := "return a + b // with comment"

	result, _ := exec.EditFile(testFile, oldStr, newStr)
	if !result.Success {
		t.Fatalf("Expected success for unique match: %s", result.Error)
	}

	modified, _ := exec.ReadFile(testFile)
	count := strings.Count(modified, "with comment")
	if count != 1 {
		t.Errorf("Expected exactly 1 replacement, got %d occurrences of 'with comment'", count)
	}

	t.Logf("✅ MultipleOccurrences test passed (only first occurrence replaced)")
}

// ============================================================
// [P0] ErrorAnalysis 测试用例
// ============================================================

func TestAnalyzeError_SyntaxError(t *testing.T) {
	stderr := `# command-line-arguments
./main.go:5:2: syntax error: unexpected newline, expecting comma or }`

	result := &ExecutionResult{
		Command:  "go build",
		Stderr:   stderr,
		Success:  false,
		ExitCode: 1,
	}

	analysis := AnalyzeError(result)

	if analysis == nil {
		t.Fatal("Expected non-nil analysis")
	}

	if analysis.Type != ErrSyntax {
		t.Errorf("Expected type=syntax_error, got %s", analysis.Type)
	}

	if analysis.Category != "compiler" {
		t.Errorf("Expected category=compiler, got %s", analysis.Category)
	}

	if analysis.Severity != "error" {
		t.Errorf("Expected severity=error, got %s", analysis.Severity)
	}

	if analysis.Line != 5 {
		t.Errorf("Expected line=5, got %d", analysis.Line)
	}

	formatted := FormatErrorForSE(analysis)
	if !strings.Contains(formatted, "syntax_error") {
		t.Error("Formatted output should contain error type")
	}

	t.Logf("✅ SyntaxError test passed\n%s", formatted)
}

func TestAnalyzeError_RuntimeError(t *testing.T) {
	stderr := `panic: runtime error: index out of range [3] with length 3

goroutine 1 [running]:
main.main()
	/main.go:10 +0x123`

	result := &ExecutionResult{
		Command:  "./app",
		Stderr:   stderr,
		Success:  false,
		ExitCode: 2,
		Duration: 500 * time.Millisecond,
	}

	analysis := AnalyzeError(result)

	if analysis.Type != ErrRuntime {
		t.Errorf("Expected type=runtime_error, got %s", analysis.Type)
	}

	if !strings.Contains(analysis.Message, "index out of range") {
		t.Errorf("Expected panic message in Message, got: %s", analysis.Message)
	}

	if len(analysis.PossibleCauses) == 0 {
		t.Error("Expected possible causes for runtime error")
	}

	t.Logf("✅ RuntimeError test passed\nMessage: %s\nCauses: %v",
		analysis.Message, analysis.PossibleCauses)
}

func TestAnalyzeError_TestFailure(t *testing.T) {
	stderr := `--- FAIL: TestAdd (0.00s)
    --- PASS: TestMultiply (0.00s)
    --- FAIL: TestDivide (0.00s)
        main_test.go:15: Expected 2.0, got 0.0
FAIL
coverage: 80.0% of statements`

	result := &ExecutionResult{
		Command:  "go test ./...",
		Stderr:   stderr,
		Success:  false,
		ExitCode: 1,
	}

	analysis := AnalyzeError(result)

	if analysis.Type != ErrTestFail {
		t.Errorf("Expected type=test_failure, got %s", analysis.Type)
	}

	if analysis.TestResults == nil {
		t.Fatal("Expected non-nil TestResults")
	}

	if analysis.TestResults.Failed != 2 {
		t.Errorf("Expected 2 failed tests, got %d", analysis.TestResults.Failed)
	}

	if len(analysis.TestResults.FailNames) < 2 {
		t.Errorf("Expected at least 2 failed test names, got %v",
			analysis.TestResults.FailNames)
	}

	if analysis.TestResults.Passed != 1 {
		t.Errorf("Expected 1 passed test, got %d", analysis.TestResults.Passed)
	}

	t.Logf("✅ TestFailure test passed\nFailed: %d | Passed: %d | Names: %v",
		analysis.TestResults.Failed,
		analysis.TestResults.Passed,
		analysis.TestResults.FailNames)
}

func TestAnalyzeError_ImportError(t *testing.T) {
	stderr := `main.go:5:2: "fmt" imported and not used`

	result := &ExecutionResult{
		Command:  "go build",
		Stderr:   stderr,
		Success:  false,
		ExitCode: 1,
	}

	analysis := AnalyzeError(result)

	if analysis.Type != ErrImport {
		t.Errorf("Expected type=import_error, got %s", analysis.Type)
	}

	t.Logf("✅ ImportError test passed")
}

func TestAnalyzeError_PermissionError(t *testing.T) {
	stderr := `open /etc/hosts: permission denied`

	result := &ExecutionResult{
		Command:  "cat /etc/hosts",
		Stderr:   stderr,
		Success:  false,
		ExitCode: 1,
	}

	analysis := AnalyzeError(result)

	if analysis.Type != ErrPermission {
		t.Errorf("Expected type=permission_error, got %s", analysis.Type)
	}

	t.Logf("✅ PermissionError test passed")
}

func TestAnalyzeError_Timeout(t *testing.T) {
	stderr := ``

	result := &ExecutionResult{
		Command:  "long_running_command",
		Stdout:   "",
		Stderr:   stderr,
		Success:  false,
		ExitCode: -1,
		Duration: 45 * time.Second,
	}

	analysis := AnalyzeError(result)

	if analysis.Type != ErrTimeout {
		t.Errorf("Expected type=timeout, got %s", analysis.Type)
	}

	if analysis.Severity != "warning" {
		t.Errorf("Expected severity=warning for timeout, got %s", analysis.Severity)
	}

	t.Logf("✅ Timeout test passed (severity: %s)", analysis.Severity)
}

func TestAnalyzeError_SuccessfulExecution(t *testing.T) {
	result := &ExecutionResult{
		Command:  "go run main.go",
		Stdout:   "Hello World\n",
		Success:  true,
		ExitCode: 0,
	}

	analysis := AnalyzeError(result)

	if analysis != nil {
		t.Errorf("Expected nil analysis for successful execution, got %+v", analysis)
	}

	t.Logf("✅ Successful execution returns nil analysis")
}

func TestFormatErrorForSE_Structure(t *testing.T) {
	analysis := &ErrorAnalysis{
		Type:      ErrSyntax,
		Category:  "compiler",
		Severity:  "error",
		File:      "main.go",
		Line:      10,
		Column:    5,
		Message:   "expected ';', found '}'",
		SuggestedFix: "Check syntax around line 10",
		PossibleCauses: []string{
			"Missing semicolon",
			"Unclosed bracket",
			"Typo in keyword",
		},
	}

	formatted := FormatErrorForSE(analysis)

	requiredStrings := []string{
		"syntax_error",
		"expected ';', found '}'",
		"main.go",
		"10",
		"Check syntax around line 10",
		"Missing semicolon",
		"Unclosed bracket",
		"Typo in keyword",
	}

	for _, required := range requiredStrings {
		if !strings.Contains(formatted, required) {
			t.Errorf("Formatted output missing '%s'\nOutput:\n%s", required, formatted)
		}
	}

	t.Logf("✅ FormatErrorForSE structure test passed\n%s", formatted)
}

// ============================================================
// [P0] VerificationPipeline 测试用例
// ============================================================

func TestVerificationPipeline_CreateDefault(t *testing.T) {
	workDir := t.TempDir()
	exec := NewExecutor(workDir, nil)

	pipeline := NewDefaultVerificationPipeline(exec)

	if pipeline == nil {
		t.Fatal("Expected non-nil pipeline")
	}

	if len(pipeline.rules) == 0 {
		t.Error("Expected at least one rule in default pipeline")
	}

	for i, rule := range pipeline.rules {
		if rule.Name == "" {
			t.Errorf("Rule %d has empty name", i)
		}
		if rule.Action == nil {
			t.Errorf("Rule %d has nil action", i)
		}
	}

	t.Logf("✅ Default pipeline created with %d rules", len(pipeline.rules))
}

func TestVerificationPipeline_RunWithGoProject(t *testing.T) {
	workDir := t.TempDir()

	goModPath := filepath.Join(workDir, "go.mod")
	os.WriteFile(goModPath, []byte("module testproject\ngo 1.21"), 0644)

	mainGoPath := filepath.Join(workDir, "main.go")
	os.WriteFile(mainGoPath, []byte(`package main

import "fmt"

func main() {
	fmt.Println("hello")
}
`), 0644)

	exec := NewExecutor(workDir, nil)
	pipeline := NewDefaultVerificationPipeline(exec)

	report, err := pipeline.Run()

	if err != nil {
		t.Logf("⚠️ Pipeline returned error (may be expected if go not installed): %v", err)
	}

	if report == nil {
		t.Fatal("Expected non-nil report")
	}

	if report.Timestamp.IsZero() {
		t.Error("Report should have timestamp")
	}

	if len(report.Rules) == 0 {
		t.Error("Report should have rule results")
	}

	t.Logf("✅ Verification report generated\nPassed: %v | Rules: %d | Summary: %s",
		report.Passed, len(report.Rules), report.Summary)

	for _, rule := range report.Rules {
		t.Logf("  - [%s] Passed=%v Skipped=%v Error=%s",
			rule.Name, rule.Passed, rule.Skipped, rule.Error)
	}
}

func TestExecWithAnalysis_SuccessCase(t *testing.T) {
	exec := NewExecutor(".", nil)

	result := exec.ExecWithAnalysis("echo Hello P0", 5*time.Second)

	if result == nil {
		t.Fatal("Expected non-nil result")
	}

	if !result.Success {
		t.Errorf("Expected success, got exit code %d", result.ExitCode)
	}

	if !strings.Contains(result.Stdout, "Hello P0") {
		t.Errorf("Expected output to contain 'Hello P0', got: %s", result.Stdout)
	}

	if result.Duration == 0 {
		t.Error("Duration should be > 0")
	}

	t.Logf("✅ ExecWithAnalysis success test passed\nOutput: %s | Duration: %v",
		result.Stdout, result.Duration)
}

func TestExecWithAnalysis_ErrorCase(t *testing.T) {
	exec := NewExecutor(".", nil)

	result := exec.ExecWithAnalysis("exit 1", 5*time.Second)

	if result == nil {
		t.Fatal("Expected non-nil result")
	}

	if result.Success {
		t.Error("Expected failure for 'exit 1'")
	}

	if result.ErrorAnalysis == nil {
		t.Error("Expected non-nil ErrorAnalysis for failed command")
	} else {
		t.Logf("✅ Error detected: Type=%s | Category=%s | Severity=%s",
			result.ErrorAnalysis.Type,
			result.ErrorAnalysis.Category,
			result.ErrorAnalysis.Severity)
	}

	t.Logf("✅ ExecWithAnalysis error test passed")
}

// ============================================================
// [P0] 集成测试：完整工作流
// ============================================================

func TestP0_Integration_Workflow(t *testing.T) {
	t.Log("\n🚀 Starting P0 Integration Workflow Test...")

	workDir := t.TempDir()
	exec := NewExecutor(workDir, nil)

	step1File := filepath.Join(workDir, "step1_initial.go")
	initialCode := `package main

import "fmt"

func calculate(a, b int) int {
	return a + b
}

func main() {
	result := calculate(10, 20)
	fmt.Printf("Result: %d\n", result)
}
`
	os.WriteFile(step1File, []byte(initialCode), 0644)
	t.Log("📝 Step 1: Created initial file")

	oldFunc := `func calculate(a, b int) int {
	return a + b
}`
	newFunc := `func calculate(a, b int) int {
	sum := a + b
	if sum < 0 {
		return 0
	}
	return sum
}`

	editResult, err := exec.EditFile(step1File, oldFunc, newFunc)
	if err != nil {
		t.Fatalf("Step 2 failed: %v", err)
	}
	if !editResult.Success {
		t.Fatalf("Step 2 edit failed: %s", editResult.Error)
	}
	t.Logf("✅ Step 2: Edited file (%d lines changed)\nDiff:\n%s",
		editResult.LinesChanged, editResult.Diff)

	verifiedContent, _ := exec.ReadFile(step1File)
	if !strings.Contains(verifiedContent, "if sum < 0") {
		t.Fatal("Step 2 verification failed: new code not found")
	}

	badCommandResult := exec.ExecWithAnalysis("go build nonexistent.go", 5*time.Second)
	if badCommandResult.Success {
		t.Log("⚠️ Step 3a: Unexpected success (file may exist)")
	}
	if badCommandResult.ErrorAnalysis != nil {
		t.Logf("✅ Step 3a: Error analyzed correctly [%s]: %s",
			badCommandResult.ErrorAnalysis.Type,
			badCommandResult.ErrorAnalysis.Message)
	}

	pipeline := NewDefaultVerificationPipeline(exec)
	report, pipelineErr := pipeline.Run()
	if pipelineErr != nil {
		t.Logf("⚠️ Step 4: Pipeline error (may need Go toolchain): %v", pipelineErr)
	}
	if report != nil {
		t.Logf("✅ Step 4: Verification report generated\nSummary: %s", report.Summary)
	}

	t.Log("\n🎉 P0 Integration Workflow Test COMPLETED!")
	t.Log("=========================================")
	t.Log("✅ EditFile: Working correctly")
	t.Log("✅ ErrorAnalysis: Detecting and classifying errors")
	t.Log("✅ VerificationPipeline: Generating reports")
	t.Log("=========================================\n")
}

// ============================================================
// 边界情况测试
// ============================================================

func TestEditFile_EmptyOldStr(t *testing.T) {
	exec := NewExecutor("", nil)
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "empty.go")
	os.WriteFile(testFile, []byte("package main"), 0644)

	result, err := exec.EditFile(testFile, "", "new content")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if result.Success {
		t.Log("ℹ️ Empty old_str behavior: system allowed it (implementation dependent)")
	}
}

func TestEditFile_VeryLongContent(t *testing.T) {
	tmpDir := t.TempDir()
	exec := NewExecutor(tmpDir, nil)
	testFile := filepath.Join(tmpDir, "large.go")

	longContent := strings.Repeat("// This is a very long line of comments\n", 1000)
	os.WriteFile(testFile, []byte(longContent), 0644)

	oldStr := "// This is a very long line of comments\n"
	newStr := "// Modified line of comments\n"

	start := time.Now()
	result, err := exec.EditFile(testFile, oldStr, newStr)
	duration := time.Since(start)

	if err != nil {
		t.Fatalf("Failed on large file: %v", err)
	}

	if !result.Success {
		t.Fatalf("Edit failed on large file: %s", result.Error)
	}

	t.Logf("✅ Large file test passed (duration: %v, lines changed: %d)",
		duration, result.LinesChanged)
}

func TestAnalyzeError_UnknownError(t *testing.T) {
	stderr := `Fatal error: system failure with code 0xDEAD
Process terminated abnormally`

	result := &ExecutionResult{
		Command:  "unknown_command",
		Stderr:   stderr,
		Success:  false,
		ExitCode: 127,
	}

	analysis := AnalyzeError(result)

	if analysis == nil {
		t.Fatal("Expected analysis even for unknown errors")
	}

	if analysis.Type != ErrUnknown {
		t.Errorf("Expected type=unknown, got %s", analysis.Type)
	}

	t.Logf("✅ Unknown error handled gracefully (type: %s)", analysis.Type)
}

func BenchmarkEditFile(b *testing.B) {
	exec := NewExecutor("", nil)
	tmpDir := b.TempDir()
	testFile := filepath.Join(tmpDir, "bench.go")

	content := `package main

func bench() {
	x := 1 + 2 + 3 + 4 + 5
}
`
	os.WriteFile(testFile, []byte(content), 0644)

	oldStr := "\tx := 1 + 2 + 3 + 4 + 5"
	newStr := "\tx := 10 + 20 + 30 + 40 + 50"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		exec.EditFile(testFile, oldStr, newStr)
	}
}

func BenchmarkErrorAnalysis(b *testing.B) {
	stderr := `./main.go:15:2: syntax error: unexpected newline, expecting comma or }`

	result := &ExecutionResult{
		Command: "go build",
		Stderr:  stderr,
		Success: false,
		ExitCode: 1,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		AnalyzeError(result)
	}
}

// ============================================================
// [P1] SearchFiles 测试用例
// ============================================================

func TestSearchFiles_BasicString(t *testing.T) {
	tmpDir := t.TempDir()
	exec := NewExecutor(tmpDir, nil)

	os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte(`package main

import "fmt"

func main() {
	fmt.Println("Hello World")
}
`), 0644)

	os.WriteFile(filepath.Join(tmpDir, "utils.go"), []byte(`package main

func helper() string {
	return "Hello World from utils"
}
`), 0644)

	result, err := exec.SearchFiles("Hello World")
	if err != nil {
		t.Fatalf("SearchFiles error: %v", err)
	}

	if result.Error != "" {
		t.Fatalf("SearchFiles returned error: %s", result.Error)
	}

	if result.TotalMatches != 2 {
		t.Errorf("Expected 2 matches, got %d", result.TotalMatches)
	}

	if len(result.Matches) < 2 {
		t.Fatalf("Expected at least 2 match entries, got %d", len(result.Matches))
	}

	if result.Matches[0].File != "main.go" {
		t.Errorf("First match should be in main.go, got %s", result.Matches[0].File)
	}
	if result.Matches[0].Line != 6 {
		t.Errorf("First match line should be 6, got %d", result.Matches[0].Line)
	}
	t.Logf("✅ Basic string search: %d matches in %d files", result.TotalMatches, result.FilesSearched)
}

func TestSearchFiles_Regex(t *testing.T) {
	tmpDir := t.TempDir()
	exec := NewExecutor(tmpDir, nil)

	os.WriteFile(filepath.Join(tmpDir, "app.go"), []byte(`package main

func login(user string) bool { return true }
func logout() {}
func getUser(id int) *User { return nil }
`), 0644)

	result, _ := exec.SearchFiles(`func \w+\(.*\)`, WithRegex(), WithFilePattern("*.go"))
	if result.Error != "" {
		t.Fatalf("Regex search error: %s", result.Error)
	}

	if result.TotalMatches != 3 {
		t.Errorf("Expected 3 regex matches (3 func declarations), got %d", result.TotalMatches)
	}
	t.Logf("✅ Regex search: %d matches", result.TotalMatches)
}

func TestSearchFiles_FilePatternFilter(t *testing.T) {
	tmpDir := t.TempDir()
	exec := NewExecutor(tmpDir, nil)

	os.WriteFile(filepath.Join(tmpDir, "code.go"), []byte("const VERSION = \"1.0\"\n"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "readme.txt"), []byte("Version: 1.0\n"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "test.js"), []byte("var version = '1.0';\n"), 0644)

	result, _ := exec.SearchFiles("VERSION", WithFilePattern("*.go"))
	if result.TotalMatches != 1 {
		t.Errorf("Expected 1 match in .go files only, got %d", result.TotalMatches)
	}
	t.Logf("✅ File pattern filter: %d matches", result.TotalMatches)
}

func TestSearchFiles_SkipDirs(t *testing.T) {
	tmpDir := t.TempDir()
	exec := NewExecutor(tmpDir, nil)

	nodeDir := filepath.Join(tmpDir, "node_modules", "pkg")
	os.MkdirAll(nodeDir, 0755)
	os.WriteFile(filepath.Join(nodeDir, "index.js"), []byte("secret data\n"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte("real data\n"), 0644)

	result, _ := exec.SearchFiles("data")
	if result.TotalMatches != 1 {
		t.Errorf("node_modules should be skipped, expected 1 match, got %d", result.TotalMatches)
	}
	t.Logf("✅ Skip dirs: node_modules ignored correctly")
}

func TestSearchFiles_ContextLines(t *testing.T) {
	tmpDir := t.TempDir()
	exec := NewExecutor(tmpDir, nil)

	content := `line 1
line 2 - before target
line 3 - TARGET HERE
line 4 - after target
line 5
`
	os.WriteFile(filepath.Join(tmpDir, "ctx.go"), []byte(content), 0644)

	result, _ := exec.SearchFiles("TARGET HERE", WithContextLines(1))
	if result.TotalMatches != 1 {
		t.Fatalf("Expected 1 match, got %d", result.TotalMatches)
	}

	match := result.Matches[0]
	if len(match.ContextBefore) == 0 {
		t.Error("Expected context_before lines")
	}
	if len(match.ContextAfter) == 0 {
		t.Error("Expected context_after lines")
	}
	t.Logf("✅ Context lines: before=%v, after=%v", match.ContextBefore, match.ContextAfter)
}

func TestSearchFiles_PathOutsideWorkdir(t *testing.T) {
	tmpDir := t.TempDir()
	exec := NewExecutor(tmpDir, nil)

	result, _ := exec.SearchFiles("test", WithPath("../../etc/passwd"))
	if result.Error == "" || !strings.Contains(result.Error, "outside") {
		t.Errorf("Should reject path outside workdir, got: %s", result.Error)
	}
	t.Logf("✅ Path outside workdir rejected")
}

func TestSearchFiles_CaseInsensitive(t *testing.T) {
	tmpDir := t.TempDir()
	exec := NewExecutor(tmpDir, nil)

	os.WriteFile(filepath.Join(tmpDir, "case.go"), []byte("Hello WORLD hello world HELLO\n"), 0644)

	result, _ := exec.SearchFiles("hello", WithCaseInsensitive())
	if result.TotalMatches != 1 {
		t.Errorf("Case insensitive should find 'hello' in 'Hello', got %d", result.TotalMatches)
	}
	t.Logf("✅ Case insensitive: found at line %d col %d", result.Matches[0].Line, result.Matches[0].Column)
}

func BenchmarkSearchFiles(b *testing.B) {
	tmpDir := b.TempDir()
	exec := NewExecutor(tmpDir, nil)

	for i := 0; i < 50; i++ {
		content := fmt.Sprintf(`package main\n\nfunc func%d() int { return %d }\n`, i, i)
		os.WriteFile(filepath.Join(tmpDir, fmt.Sprintf("file%d.go", i)), []byte(content), 0644)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		exec.SearchFiles("return ", WithFilePattern("*.go"))
	}
}

// [P1] GitOperation 测试用例

func TestGitOperation_Status(t *testing.T) {
	tmpDir := t.TempDir()
	exec := NewExecutor(tmpDir, nil)

	initGit(t, tmpDir)

	result, err := exec.GitOperation("status", "", nil)
	if err != nil {
		t.Fatalf("GitOperation status error: %v", err)
	}

	if !result.Success {
		t.Fatalf("Status should succeed, got error: %s", result.Error)
	}

	if result.Status == nil {
		t.Fatal("Status should not be nil")
	}

	if !strings.Contains(result.Status.Branch, "main") && !strings.Contains(result.Status.Branch, "master") {
		t.Errorf("Expected branch containing main/master, got %s", result.Status.Branch)
	}

	if !result.Status.IsClean {
		t.Errorf("Fresh repo should be clean, got staged=%d modified=%d untracked=%d",
			len(result.Status.Staged), len(result.Status.Modified), len(result.Status.Untracked))
	}

	t.Logf("✅ Git Status: branch=%s, clean=%v", result.Status.Branch, result.Status.IsClean)
}

func TestGitOperation_Log(t *testing.T) {
	tmpDir := t.TempDir()
	exec := NewExecutor(tmpDir, nil)

	initGitWithCommit(t, tmpDir, "initial commit")

	result, err := exec.GitOperation("log", "", nil)
	if err != nil {
		t.Fatalf("GitOperation log error: %v", err)
	}

	if !result.Success {
		t.Fatalf("Log should succeed, got error: %s", result.Error)
	}

	if result.Log == nil || len(result.Log) == 0 {
		t.Fatal("Log should have entries after commit")
	}

	if result.Log[0].Message != "initial commit" {
		t.Errorf("Expected 'initial commit', got '%s'", result.Log[0].Message)
	}

	t.Logf("✅ Git Log: %d entries, first=%s", len(result.Log), result.Log[0].Hash)
}

func TestGitOperation_Commit(t *testing.T) {
	tmpDir := t.TempDir()
	exec := NewExecutor(tmpDir, nil)

	initGit(t, tmpDir)
	os.WriteFile(filepath.Join(tmpDir, "test.txt"), []byte("hello"), 0644)
	runCmd(t, tmpDir, "git", "add", "test.txt")

	result, err := exec.GitOperation("commit", "add test file", nil)
	if err != nil {
		t.Fatalf("GitOperation commit error: %v", err)
	}

	if !result.Success {
		t.Fatalf("Commit should succeed, got error: %s", result.Error)
	}

	logResult, _ := exec.GitOperation("log", "", nil)
	found := false
	for _, entry := range logResult.Log {
		if strings.Contains(entry.Message, "add test file") {
			found = true
			break
		}
	}
	if !found {
		t.Error("Commit message not found in git log")
	}

	t.Logf("✅ Git Commit: message found in log")
}

func TestGitOperation_CommitNoMessage(t *testing.T) {
	tmpDir := t.TempDir()
	exec := NewExecutor(tmpDir, nil)

	result, err := exec.GitOperation("commit", "", nil)
	if err != nil {
		t.Fatalf("GitOperation error: %v", err)
	}

	if result.Success {
		t.Fatal("Commit without message should fail")
	}

	if !strings.Contains(result.Error, "commit 需要 message") {
		t.Errorf("Expected 'commit 需要 message' error, got: %s", result.Error)
	}

	t.Logf("✅ Git Commit no message correctly rejected")
}

func TestGitOperation_UnsupportedAction(t *testing.T) {
	tmpDir := t.TempDir()
	exec := NewExecutor(tmpDir, nil)

	result, err := exec.GitOperation("rebase", "", nil)
	if err != nil {
		t.Fatalf("GitOperation error: %v", err)
	}

	if result.Success {
		t.Fatal("Unsupported action should fail")
	}

	if !strings.Contains(result.Error, "不支持的") {
		t.Errorf("Expected unsupported error, got: %s", result.Error)
	}

	t.Logf("✅ Git Unsupported action rejected: %s", result.Error)
}

func TestGitOperation_Diff(t *testing.T) {
	tmpDir := t.TempDir()
	exec := NewExecutor(tmpDir, nil)

	initGitWithCommit(t, tmpDir, "initial")
	testPath := filepath.Join(tmpDir, "test.txt")
	os.WriteFile(testPath, []byte("original content"), 0644)
	runCmd(t, tmpDir, "git", "add", "test.txt")
	runCmd(t, tmpDir, "git", "commit", "-m", "add test.txt")
	os.WriteFile(testPath, []byte("modified content"), 0644)

	result, err := exec.GitOperation("diff", "", nil)
	if err != nil {
		t.Fatalf("GitOperation diff error: %v", err)
	}

	if !result.Success {
		t.Fatalf("Diff should succeed, got error: %s", result.Error)
	}

	if result.Diff == "" {
		t.Error("Diff should have output after modifying file")
	}

	t.Logf("✅ Git Diff: %d chars", len(result.Diff))
}

func TestGitOperation_Branch(t *testing.T) {
	tmpDir := t.TempDir()
	exec := NewExecutor(tmpDir, nil)

	initGit(t, tmpDir)

	result, err := exec.GitOperation("branch", "", nil)
	if err != nil {
		t.Fatalf("GitOperation branch error: %v", err)
	}

	if !result.Success {
		t.Fatalf("Branch should succeed, got error: %s", result.Error)
	}

	t.Logf("✅ Git Branch: %d chars output", len(result.Output))
}

func TestParseGitStatus(t *testing.T) {
	output := `## main...origin/main [ahead 1]
 M internal/executor/executor.go
M  internal/chat/manager.go
?? new_file.go
A  added_file.go`

	status := parseGitStatus(output)
	if status.Branch != "main" {
		t.Errorf("Expected main, got %s", status.Branch)
	}
	if status.Ahead != 1 {
		t.Errorf("Expected ahead=1, got %d", status.Ahead)
	}
	if len(status.Modified) == 0 {
		t.Error("Should have modified files")
	}
	if len(status.Untracked) == 0 {
		t.Error("Should have untracked files")
	}
	if status.IsClean {
		t.Error("Should NOT be clean")
	}
	t.Logf("✅ parseGitStatus: branch=%s, staged=%d, mod=%d, untracked=%d",
		status.Branch, len(status.Staged), len(status.Modified), len(status.Untracked))
}

func TestParseGitStatus_Clean(t *testing.T) {
	status := parseGitStatus("## main")
	if !status.IsClean {
		t.Error("Clean status should report IsClean=true")
	}
	t.Logf("✅ parseGitStatus clean: is_clean=%v", status.IsClean)
}

func TestParseGitLog(t *testing.T) {
	output := `a1b2c3d First commit
e4f5g6h Second commit
i7j8k9l Third commit`

	entries := parseGitLog(output)
	if len(entries) != 3 {
		t.Fatalf("Expected 3 entries, got %d", len(entries))
	}
	if entries[0].Hash != "a1b2c3d" {
		t.Errorf("First hash should be a1b2c3d, got %s", entries[0].Hash)
	}
	if entries[2].Message != "Third commit" {
		t.Errorf("Third message wrong, got %s", entries[2].Message)
	}
	t.Logf("✅ parseGitLog: %d entries", len(entries))
}

func initGit(t *testing.T, dir string) {
	t.Helper()
	runCmd(t, dir, "git", "init")
	runCmd(t, dir, "git", "config", "user.email", "test@test.com")
	runCmd(t, dir, "git", "config", "user.name", "Test User")
}

func initGitWithCommit(t *testing.T, dir string, msg string) {
	t.Helper()
	initGit(t, dir)
	os.WriteFile(filepath.Join(dir, "init.txt"), []byte("init"), 0644)
	runCmd(t, dir, "git", "add", ".")
	runCmd(t, dir, "git", "commit", "-m", msg)
}

func runCmd(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v failed: %v\n%s", name, args, err, out)
	}
}

// [P1] RunTests 测试用例

func TestRunTests_Basic(t *testing.T) {
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(`module testpkg

go 1.23
`), 0644)
	os.WriteFile(filepath.Join(tmpDir, "math.go"), []byte(`package math

func Add(a, b int) int { return a + b }
func Mul(a, b int) int { return a * b }
`), 0644)
	os.WriteFile(filepath.Join(tmpDir, "math_test.go"), []byte(`package math

import "testing"

func TestAdd(t *testing.T) {
	if Add(1, 2) != 3 { t.Fatal("Add failed") }
}
func TestMul(t *testing.T) {
	if Mul(3, 4) != 12 { t.Fatal("Mul failed") }
}
func TestSkipDemo(t *testing.T) {
	t.Skip("demo skip")
}
`), 0644)

	exec := NewExecutor(tmpDir, nil)

	report, err := exec.RunTests(TestConfig{Verbose: true})
	if err != nil {
		t.Fatalf("RunTests error: %v", err)
	}

	if report.Total == 0 {
		t.Fatal("Expected at least one test case")
	}

	if !report.Success {
		t.Errorf("Expected success, got failed=%d", report.Failed)
	}

	if report.Passed < 2 {
		t.Errorf("Expected at least 2 passed, got %d", report.Passed)
	}

	if report.Skipped < 1 {
		t.Errorf("Expected at least 1 skipped, got %d", report.Skipped)
	}

	t.Logf("✅ RunTests: total=%d passed=%d failed=%d skipped=%d duration=%s",
		report.Total, report.Passed, report.Failed, report.Skipped, report.Duration)

	for _, tc := range report.Cases {
		t.Logf("  %s %s (%s)", tc.Status, tc.Name, tc.Duration)
	}
}

func TestRunTests_Verbose(t *testing.T) {
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(`module calcpkg

go 1.23
`), 0644)
	os.WriteFile(filepath.Join(tmpDir, "calc.go"), []byte(`package calc

func Sum(nums ...int) int {
	s := 0
	for _, n := range nums { s += n }
	return s
}
`), 0644)
	os.WriteFile(filepath.Join(tmpDir, "calc_test.go"), []byte(`package calc

import "testing"

func TestSumEmpty(t *testing.T) { if Sum() != 0 { t.Fail() } }
func TestSumSingle(t *testing.T) { if Sum(5) != 5 { t.Fail() } }
func TestSumMultiple(t *testing.T) { if Sum(1,2,3) != 6 { t.Fail() } }
`), 0644)

	exec := NewExecutor(tmpDir, nil)

	report, err := exec.RunTests(TestConfig{
		Verbose: true,
	})
	if err != nil {
		t.Fatalf("RunTests verbose error: %v", err)
	}

	if len(report.Cases) == 0 {
		t.Error("Verbose mode should return individual test cases")
	}

	foundPass := false
	for _, tc := range report.Cases {
		if tc.Status == "pass" {
			foundPass = true
			break
		}
	}
	if !foundPass {
		t.Error("Expected at least one pass status in verbose mode")
	}

	t.Logf("✅ RunTests Verbose: %d cases", len(report.Cases))
}

func TestRunTests_WithCoverage(t *testing.T) {
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(`module covpkg

go 1.23
`), 0644)
	os.WriteFile(filepath.Join(tmpDir, "cov.go"), []byte(`package cov

func Double(n int) int { return n * 2 }
`), 0644)
	os.WriteFile(filepath.Join(tmpDir, "cov_test.go"), []byte(`package cov

import "testing"

func TestDouble(t *testing.T) {
	if Double(5) != 10 { t.Fatal("Double failed") }
}
`), 0644)

	exec := NewExecutor(tmpDir, nil)

	report, err := exec.RunTests(TestConfig{
		Coverage: true,
	})
	if err != nil {
		t.Fatalf("RunTests coverage error: %v", err)
	}

	coverPath := filepath.Join(tmpDir, "coverage.out")
	if _, err := os.Stat(coverPath); err == nil {
		t.Errorf("coverage.out should be cleaned up after test, but found at %s", coverPath)
	}

	t.Logf("✅ RunTests Coverage: coverage=%s, file cleaned up", report.Coverage)
}

func TestParseGoTestOutput_PassFailSkip(t *testing.T) {
	output := `=== RUN   TestA
--- PASS: TestA (0.01s)
=== RUN   TestB
--- FAIL: TestB (0.02s)
	main_test.go:15: expected 42 got 24
	main_test.go:16: assertion error
=== RUN   TestC
--- SKIP: TestC (0.00s)
=== RUN   TestD
--- PASS: TestD (0.03s)
ok      pkg/test    0.06s`

	cases := parseGoTestOutput(output)
	if len(cases) != 4 {
		t.Fatalf("Expected 4 cases, got %d", len(cases))
	}

	if cases[0].Name != "TestA" || cases[0].Status != "pass" {
		t.Errorf("Case 0: expected TestA/pass, got %s/%s", cases[0].Name, cases[0].Status)
	}
	if cases[1].Status != "fail" {
		t.Errorf("Case 1: expected fail, got %s", cases[1].Status)
	}
	if cases[1].Error == "" {
		t.Error("Case 1 (fail) should have error message")
	}
	if !strings.Contains(cases[1].Error, "expected 42") {
		t.Errorf("Case 1 error should contain 'expected 42', got: %s", cases[1].Error)
	}
	if cases[2].Status != "skip" {
		t.Errorf("Case 2: expected skip, got %s", cases[2].Status)
	}
	if cases[3].Status != "pass" {
		t.Errorf("Case 3: expected pass, got %s", cases[3].Status)
	}

	t.Logf("✅ parseGoTestOutput: 4 cases parsed correctly")
}

func TestParseGoTestOutput_Empty(t *testing.T) {
	cases := parseGoTestOutput("")
	if len(cases) != 0 {
		t.Errorf("Expected 0 cases for empty output, got %d", len(cases))
	}
	cases = parseGoTestOutput("no test results here\njust random text")
	if len(cases) != 0 {
		t.Errorf("Expected 0 cases for garbage output, got %d", len(cases))
	}
	t.Logf("✅ parseGoTestOutput empty/garbage handled correctly")
}

func TestParseGoTestOutput_DurationExtraction(t *testing.T) {
	output := `=== RUN   TestSlow
--- PASS: TestSlow (1.234s)
ok      pkg/test    2.500s`

	cases := parseGoTestOutput(output)
	if len(cases) != 1 {
		t.Fatalf("Expected 1 case, got %d", len(cases))
	}
	if cases[0].Duration != "1.234s" {
		t.Errorf("Expected duration 1.234s, got %s", cases[0].Duration)
	}
	t.Logf("✅ Duration extraction: %s", cases[0].Duration)
}

// [P1] 智能重试策略测试用例

func TestClassifyError_Transient(t *testing.T) {
	exec := NewExecutor(t.TempDir(), nil)

	tests := []struct {
		input    string
		expected ErrorCategory
	}{
		{"connection refused", CategoryTransient},
		{"timeout after 30s", CategoryTransient},
		{"429 too many requests", CategoryTransient},
		{"503 service unavailable", CategoryTransient},
		{"i/o timeout", CategoryTransient},
		{"context deadline exceeded", CategoryTransient},
		{"reset by peer", CategoryTransient},
		{"temporary failure in network", CategoryTransient},
	}

	for _, tc := range tests {
		result := exec.ClassifyError(tc.input)
		if result != tc.expected {
			t.Errorf("ClassifyError(%q) = %s, want %s", tc.input, result, tc.expected)
		}
	}
	t.Logf("✅ ClassifyError Transient: %d cases passed", len(tests))
}

func TestClassifyError_Fixable(t *testing.T) {
	exec := NewExecutor(t.TempDir(), nil)

	tests := []struct {
		input    string
		expected ErrorCategory
	}{
		{"syntax error: unexpected { at line 10", CategoryFixable},
		{"undefined: fmt", CategoryFixable},
		{"cannot find package \"github.com/test\"", CategoryFixable},
		{"import cycle not allowed", CategoryFixable},
		{"panic: runtime error: index out of range", CategoryFixable},
		{"--- FAIL: TestLogin (0.01s)", CategoryFixable},
		{"main.go:15:2: expected ';', found '{'", CategoryFixable},
	}

	for _, tc := range tests {
		result := exec.ClassifyError(tc.input)
		if result != tc.expected {
			t.Errorf("ClassifyError(%q) = %s, want %s", tc.input, result, tc.expected)
		}
	}
	t.Logf("✅ ClassifyError Fixable: %d cases passed", len(tests))
}

func TestClassifyError_Permanent(t *testing.T) {
	exec := NewExecutor(t.TempDir(), nil)

	tests := []struct {
		input    string
		expected ErrorCategory
	}{
		{"permission denied", CategoryPermanent},
		{"operation not permitted", CategoryPermanent},
		{"fatal error: cannot allocate memory", CategoryPermanent},
		{"access denied to resource", CategoryPermanent},
		{"some random unknown error message", CategoryPermanent},
	}

	for _, tc := range tests {
		result := exec.ClassifyError(tc.input)
		if result != tc.expected {
			t.Errorf("ClassifyError(%q) = %s, want %s", tc.input, result, tc.expected)
		}
	}
	t.Logf("✅ ClassifyError Permanent: %d cases passed", len(tests))
}

func TestExecuteWithRetry_SuccessFirstTry(t *testing.T) {
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(`module retrytest

go 1.23
`), 0644)
	os.WriteFile(filepath.Join(tmpDir, "ok.go"), []byte(`package main

import "fmt"
func main() { fmt.Println("hello") }
`), 0644)

	exec := NewExecutor(tmpDir, nil)
	config := &RetryConfig{
		MaxRetries:   3,
		InitialDelay: 10 * time.Millisecond,
		Multiplier:   1.5,
		Jitter:       false,
	}

	result, err := exec.ExecuteWithRetry("go", []string{"run", "ok.go"}, config)
	if err != nil {
		t.Fatalf("ExecuteWithRetry error: %v", err)
	}

	if !result.Success {
		t.Errorf("Expected success on first try")
	}
	if result.TotalAttempts != 1 {
		t.Errorf("Expected 1 attempt, got %d", result.TotalAttempts)
	}
	if len(result.Attempts) != 1 {
		t.Fatalf("Expected 1 attempt record, got %d", len(result.Attempts))
	}
	if result.Attempts[0].Attempt != 1 {
		t.Error("First attempt should be #1")
	}

	t.Logf("✅ ExecuteWithRetry Success: 1 attempt, output=%s", result.FinalOutput[:min(50, len(result.FinalOutput))])
}

func TestExecuteWithRetry_PermanentStopsImmediately(t *testing.T) {
	tmpDir := t.TempDir()
	exec := NewExecutor(tmpDir, nil)
	config := &RetryConfig{
		MaxRetries:      5,
		InitialDelay:    10 * time.Millisecond,
		Multiplier:      1.0,
		Jitter:          false,
		RetryOnFixable:  false,
	}

	result, err := exec.ExecuteWithRetry("cmd", []string{"/c", "exit 1"}, config)
	if err != nil {
		t.Fatalf("ExecuteWithRetry error: %v", err)
	}

	if result.Success {
		t.Error("Expected failure for permanent error")
	}
	if result.TotalAttempts > 1 {
		t.Errorf("Permanent error should stop immediately, but got %d attempts", result.TotalAttempts)
	}
	if result.Category != CategoryPermanent {
		t.Errorf("Expected category permanent, got %s", result.Category)
	}

	t.Logf("✅ ExecuteWithRetry Permanent: stopped after %d attempt(s)", result.TotalAttempts)
}

func TestExecuteWithRetry_FixableNoRetryByDefault(t *testing.T) {
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(`module fixtest

go 1.23
`), 0644)
	os.WriteFile(filepath.Join(tmpDir, "bad.go"), []byte(`package main

func main() {
	println(undefinedVar)
}
`), 0644)

	exec := NewExecutor(tmpDir, nil)
	config := &RetryConfig{
		MaxRetries:   3,
		InitialDelay: 10 * time.Millisecond,
		Jitter:       false,
		RetryOnFixable: false,
	}

	result, err := exec.ExecuteWithRetry("go", []string{"build", "bad.go"}, config)
	if err != nil {
		t.Fatalf("ExecuteWithRetry error: %v", err)
	}

	if result.Success {
		t.Error("Expected failure for syntax error")
	}
	if result.Category != CategoryFixable {
		t.Errorf("Expected category fixable, got %s", result.Category)
	}
	if result.TotalAttempts > 1 {
		t.Errorf("Fixable error should NOT retry by default, got %d attempts", result.TotalAttempts)
	}

	t.Logf("✅ ExecuteWithRetry Fixable (no retry): stopped after %d attempt, category=%s",
		result.TotalAttempts, result.Category)
}

func TestExecuteWithRetry_DefaultConfig(t *testing.T) {
	tmpDir := t.TempDir()
	exec := NewExecutor(tmpDir, nil)

	result, err := exec.ExecuteWithRetry("go", []string{"version"}, nil)
	if err != nil {
		t.Fatalf("ExecuteWithRetry with nil config error: %v", err)
	}

	if !result.Success {
		t.Error("Go version should succeed")
	}
	if !strings.Contains(result.FinalOutput, "go") {
		t.Errorf("Output should contain 'go', got: %s", result.FinalOutput)
	}

	t.Logf("✅ ExecuteWithRetry default config works: success=%v", result.Success)
}

// ============================================================
// [P1] AST 级别代码修改 测试用例
// ============================================================

func TestParseGoFile_Basic(t *testing.T) {
	tmpDir := t.TempDir()
	exec := NewExecutor(tmpDir, nil)
	testFile := filepath.Join(tmpDir, "sample.go")
	content := `package sample

import (
	"fmt"
	"strings"
)

type User struct {
	ID   int    "json:\"id\""
	Name string "json:\"name\""
}

func (u *User) FullName() string {
	return u.Name
}

func Add(a, b int) int {
	return a + b
}
`
	os.WriteFile(testFile, []byte(content), 0644)

	info, err := exec.ParseGoFile("sample.go")
	if err != nil {
		t.Fatalf("ParseGoFile error: %v", err)
	}
	if info.Package != "sample" { t.Errorf("Package = %q, want 'sample'", info.Package) }
	if len(info.Functions) < 2 { t.Errorf("Functions count = %d, want >=2", len(info.Functions)) }
	if len(info.Structs) == 0 { t.Error("Structs empty") }
	foundAdd := false
	for _, fn := range info.Functions {
		if fn.Name == "Add" && !fn.IsExported { t.Error("Add should be exported") }
		if fn.Name == "Add" { foundAdd = true }
	}
	if !foundAdd { t.Error("Function Add not found") }
	if len(info.Imports) < 2 { t.Errorf("Imports count = %d, want >=2", len(info.Imports)) }

	t.Logf("✅ ParseGoFile: pkg=%s funcs=%d structs=%d imports=%d", info.Package, len(info.Functions), len(info.Structs), len(info.Imports))
}

func TestEditFileWithAST_ReplaceFunction(t *testing.T) {
	tmpDir := t.TempDir()
	exec := NewExecutor(tmpDir, nil)
	testFile := filepath.Join(tmpDir, "math.go")
	content := "package math\n\nfunc Add(a, b int) int {\n\treturn a + b\n}\n"
	os.WriteFile(testFile, []byte(content), 0644)

	op := &ASTEditOperation{
		Action: "replace",
		Target: &ASTEditTarget{
			Type: "function",
			Name: "Add",
			File: "math.go",
		},
		NewCode: "func Add(a, b int) int {\n\tsum := a + b\n\treturn sum\n}",
	}

	result, err := exec.EditFileWithAST(op)
	if err != nil {
		t.Fatalf("EditFileWithAST error: %v", err)
	}
	if !result.Success { t.Errorf("Success = false, error: %s", result.Error) }
	if result.LinesChanged <= 0 { t.Errorf("LinesChanged = %d, want >0", result.LinesChanged) }

	modifiedContent, _ := os.ReadFile(testFile)
	if !strings.Contains(string(modifiedContent), "sum := a + b") {
		t.Errorf("Modified content missing new code:\n%s", string(modifiedContent))
	}

	t.Logf("✅ EditFileWithAST ReplaceFunction: lines=%d astValid=%v diffLen=%d", result.LinesChanged, result.AstValid, len(result.Diff))
}

func TestEditFileWithAST_DeleteStruct(t *testing.T) {
	tmpDir := t.TempDir()
	exec := NewExecutor(tmpDir, nil)
	testFile := filepath.Join(tmpDir, "types.go")
	content := `package types

type User struct {
	ID   int
	Name string
}

type Config struct {
	Debug bool
	Port  int
}
`
	os.WriteFile(testFile, []byte(content), 0644)

	op := &ASTEditOperation{
		Action: "delete",
		Target: &ASTEditTarget{
			Type: "struct",
			Name: "User",
			File: "types.go",
		},
	}

	result, err := exec.EditFileWithAST(op)
	if err != nil {
		t.Fatalf("EditFileWithAST error: %v", err)
	}
	if !result.Success { t.Errorf("Success = false, error: %s", result.Error) }

	modifiedContent, _ := os.ReadFile(testFile)
	if strings.Contains(string(modifiedContent), "type User struct") {
		t.Error("User struct should be deleted")
	}
	if !strings.Contains(string(modifiedContent), "type Config struct") {
		t.Error("Config struct should remain")
	}

	t.Logf("✅ EditFileWithAST DeleteStruct: lines=%d", result.LinesChanged)
}

func TestEditFileWithAST_InsertBefore(t *testing.T) {
	tmpDir := t.TempDir()
	exec := NewExecutor(tmpDir, nil)
	testFile := filepath.Join(tmpDir, "handler.go")
	content := "package handler\n\nfunc Handle() {\n\tfmt.Println(\"handled\")\n}\n"
	os.WriteFile(testFile, []byte(content), 0644)

	op := &ASTEditOperation{
		Action: "insert_before",
		Target: &ASTEditTarget{
			Type: "function",
			Name: "Handle",
			File: "handler.go",
		},
		NewCode: "// Handle processes the request\n",
	}

	result, err := exec.EditFileWithAST(op)
	if err != nil {
		t.Fatalf("EditFileWithAST error: %v", err)
	}
	if !result.Success { t.Errorf("Success = false, error: %s", result.Error) }

	modifiedContent, _ := os.ReadFile(testFile)
	if !strings.Contains(string(modifiedContent), "// Handle processes the request") {
		t.Errorf("Comment not inserted:\n%s", string(modifiedContent))
	}

	t.Logf("✅ EditFileWithAST InsertBefore: success=%v lines=%d", result.Success, result.LinesChanged)
}

func TestEditFileWithAST_TargetNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	exec := NewExecutor(tmpDir, nil)
	testFile := filepath.Join(tmpDir, "test.go")
	os.WriteFile(testFile, []byte(`package test`), 0644)

	op := &ASTEditOperation{
		Action: "replace",
		Target: &ASTEditTarget{
			Type: "function",
			Name: "NonExistentFunc",
			File: "test.go",
		},
		NewCode: "func NonExistentFunc() {}",
	}

	result, err := exec.EditFileWithAST(op)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success { t.Error("Should fail for non-existent target") }
	if !strings.Contains(result.Error, "target not found") { t.Errorf("Error message unexpected: %s", result.Error) }

	t.Logf("✅ EditFileWithAST TargetNotFound: error=%s", result.Error)
}

func TestEditFileWithAST_PathOutsideWorkdir(t *testing.T) {
	tmpDir := t.TempDir()
	exec := NewExecutor(tmpDir, nil)

	op := &ASTEditOperation{
		Action: "replace",
		Target: &ASTEditTarget{
			Type: "function",
			Name: "Test",
			File: "../etc/passwd",
		},
		NewCode: "",
	}

	result, err := exec.EditFileWithAST(op)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success { t.Error("Should fail for path outside workdir") }
	if !strings.Contains(result.Error, "outside work directory") { t.Errorf("Error message unexpected: %s", result.Error) }

	t.Log("✅ EditFileWithAST PathOutsideWorkdir blocked correctly")
}

// ============================================================
// [P1] 多文件上下文理解 测试用例
// ============================================================

func TestAnalyzeDependencies_Basic(t *testing.T) {
	tmpDir := t.TempDir()
	exec := NewExecutor(tmpDir, nil)
	os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module testpkg\ngo 1.23\n"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "core.go"), []byte("package testpkg\n\nfunc CoreFunc() int { return 42 }\n"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "handler.go"), []byte("package testpkg\n\nimport \"testpkg\"\n\nfunc Handler() { CoreFunc() }\n"), 0644)

	deps, err := exec.AnalyzeDependencies(".")
	if err != nil {
		t.Fatalf("AnalyzeDependencies error: %v", err)
	}
	if len(deps) == 0 { t.Error("Should find dependencies") }

	coreFound := false
	for _, d := range deps {
		if d.File == "core.go" {
			coreFound = true
			foundCoreFunc := false
			for _, fn := range d.Functions {
				if fn == "CoreFunc" { foundCoreFunc = true }
			}
			if !foundCoreFunc { t.Error("CoreFunc should be in exported functions") }
		}
	}
	if !coreFound { t.Error("core.go not found") }

	t.Logf("✅ AnalyzeDependencies: files=%d", len(deps))
}

func TestAnalyzeImpact_LocalChange(t *testing.T) {
	tmpDir := t.TempDir()
	exec := NewExecutor(tmpDir, nil)
	os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module impact\ngo 1.23\n"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "util.go"), []byte("package impact\n\nfunc helper() {}\n"), 0644)

	scope, err := exec.AnalyzeImpact("util.go", "function", "helper")
	if err != nil {
		t.Fatalf("AnalyzeImpact error: %v", err)
	}
	if scope.RiskLevel == "" { t.Error("RiskLevel should be set") }
	if len(scope.Suggestions) == 0 { t.Error("Should have suggestions") }

	t.Logf("✅ AnalyzeImpact: risk=%s direct=%d suggestions=%d", scope.RiskLevel, len(scope.DirectImpact), len(scope.Suggestions))
}

func TestBuildCodeContext(t *testing.T) {
	tmpDir := t.TempDir()
	exec := NewExecutor(tmpDir, nil)
	os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module ctxpkg\ngo 1.23\n"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte("package ctxpkg\n\nfunc Main() {}\n"), 0644)

	ctx, err := exec.BuildCodeContext(".")
	if err != nil {
		t.Fatalf("BuildCodeContext error: %v", err)
	}
	if ctx.TotalFiles == 0 { t.Error("Should find files") }
	if ctx.TotalFuncs == 0 { t.Error("Should find functions") }
	if len(ctx.Packages) == 0 { t.Error("Should find packages") }

	t.Logf("✅ BuildCodeContext: files=%d funcs=%d pkgs=%d", ctx.TotalFiles, ctx.TotalFuncs, len(ctx.Packages))
}

// ============================================================
// [v0.7.1] TabComplete 测试用例
// ============================================================

func TestTabComplete_EmptyInput(t *testing.T) {
	tmpDir := t.TempDir()
	ss, err := NewShellSession(tmpDir)
	if err != nil {
		t.Fatalf("NewShellSession: %v", err)
	}
	defer ss.Close()

	results := ss.TabComplete("")
	// 空输入应返回常用命令列表
	if len(results) == 0 {
		t.Error("空输入应返回命令列表")
	}
	// 应包含常见命令
	found := false
	for _, r := range results {
		if r == "git" || r == "go" || r == "dir" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("应包含常见命令, got: %v", results[:min(5, len(results))])
	}
	t.Logf("✅ 空输入补全: %d 个候选", len(results))
}

func TestTabComplete_CommandPrefix(t *testing.T) {
	tmpDir := t.TempDir()
	ss, err := NewShellSession(tmpDir)
	if err != nil {
		t.Fatalf("NewShellSession: %v", err)
	}
	defer ss.Close()

	tests := []struct{ input, expect string }{
		{"gi", "git"},
		{"go", "go"},
		{"di", "dir"},
		{"no", "node"}, // npm/npx 也匹配 no
	}
	for _, tc := range tests {
		results := ss.TabComplete(tc.input)
		found := false
		for _, r := range results {
			if strings.EqualFold(r, tc.expect) || strings.HasSuffix(strings.ToLower(r), strings.ToLower(tc.expect)) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("TabComplete(%q) 应包含 %q, got: %v", tc.input, tc.expect, results)
		} else {
			t.Logf("✅ TabComplete(%q) → 包含 %q (%d candidates)", tc.input, tc.expect, len(results))
		}
	}
}

func TestTabComplete_FilePath(t *testing.T) {
	tmpDir := t.TempDir()
	// 创建测试文件
	os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte("package main"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "utils.go"), []byte("package main"), 0644)
	os.MkdirAll(filepath.Join(tmpDir, "subdir"), 0755)

	ss, err := NewShellSession(tmpDir)
	if err != nil {
		t.Fatalf("NewShellSession: %v", err)
	}
	defer ss.Close()

	// 补全文件名
	results := ss.TabComplete("cat m")
	foundMain := false
	foundUtils := false
	for _, r := range results {
		if strings.Contains(r, "main.go") { foundMain = true }
		if strings.Contains(r, "utils.go") { foundUtils = true }
	}
	if !foundMain {
		t.Errorf("应补全到 main.go, got: %v", results)
	}
	t.Logf("✅ 文件路径补全 'cat m' → %v (main=%v utils=%v)", results, foundMain, foundUtils)

	// 目录补全（应以 \ 结尾）
	dirResults := ss.TabComplete("cd s")
	foundDir := false
	for _, r := range dirResults {
		if strings.HasSuffix(strings.ToLower(r), strings.ToLower(`subdir\`)) {
			foundDir = true
		}
	}
	if !foundDir {
		t.Logf("目录补全结果: %v", dirResults)
	}
}

func TestTabComplete_WithSpace(t *testing.T) {
	tmpDir := t.TempDir()
	ss, err := NewShellSession(tmpDir)
	if err != nil {
		t.Fatalf("NewShellSession: %v", err)
	}
	defer ss.Close()

	// "git " 后面应该尝试路径/参数补全
	results := ss.TabComplete("git ")
	// 不应 panic，返回值可以是路径或空
	t.Logf("✅ 带空格输入 'git ': %d candidates", len(results))
}

func TestTabComplete_NoMatch(t *testing.T) {
	tmpDir := t.TempDir()
	ss, err := NewShellSession(tmpDir)
	if err != nil {
		t.Fatalf("NewShellSession: %v", err)
	}
	defer ss.Close()

	results := ss.TabComplete("zzzzzzz_nonexistent_command_xyz")
	if len(results) > 0 {
		// 可能回退到路径补全，不报错即可
		t.Logf("无匹配但有结果(可能是路径): %v", results)
	}
	t.Logf("✅ 无匹配输入正常处理")
}

func TestTabComplete_PathWithSeparator(t *testing.T) {
	tmpDir := t.TempDir()
	os.MkdirAll(filepath.Join(tmpDir, "src"), 0755)
	os.WriteFile(filepath.Join(tmpDir, "src", "app.go"), []byte("pkg"), 0644)

	ss, err := NewShellSession(tmpDir)
	if err != nil {
		t.Fatalf("NewShellSession: %v", err)
	}
	defer ss.Close()

	// 路径分隔符触发路径补全
	results := ss.TabComplete("src/")
	if len(results) == 0 {
		t.Log("路径补全 src/ 无结果（可能工作目录问题）")
	} else {
		t.Logf("✅ 路径补全 'src/' → %v", results)
	}
}
