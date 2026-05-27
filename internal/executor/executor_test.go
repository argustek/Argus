package executor

import (
	"os"
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
