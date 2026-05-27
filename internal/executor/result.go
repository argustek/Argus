package executor

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// [P0] ErrorType 错误类型枚举
type ErrorType string

const (
	ErrSyntax    ErrorType = "syntax_error"       // 语法错误
	ErrRuntime   ErrorType = "runtime_error"      // 运行时错误
	ErrTestFail  ErrorType = "test_failure"       // 测试失败
	ErrImport    ErrorType = "import_error"       // 导入错误
	ErrType      ErrorType = "type_error"         // 类型错误
	ErrPermission ErrorType = "permission_error"  // 权限错误
	ErrTimeout   ErrorType = "timeout"            // 超时
	ErrCompile   ErrorType = "compile_error"      // 编译错误
	ErrUnknown   ErrorType = "unknown"            // 未知错误
)

// [P0] TestResults 测试结果（用于测试命令）
type TestResults struct {
	Total     int      `json:"total"`
	Passed    int      `json:"passed"`
	Failed    int      `json:"failed"`
	Skipped   int      `json:"skipped"`
	FailNames []string `json:"fail_names,omitempty"` // 失败的测试名称
	Output    string   `json:"output,omitempty"`
}

// [P0] ErrorAnalysis 错误分析结果
type ErrorAnalysis struct {
	Type           ErrorType `json:"type"`
	Category       string    `json:"category"`     // "compiler" | "runtime" | "test" | "system"
	Severity       string    `json:"severity"`     // "error" | "warning" | "info"

	File        string   `json:"file,omitempty"`
	Line        int      `json:"line,omitempty"`
	Column      int      `json:"column,omitempty"`

	Message      string   `json:"message"`
	CodeContext  string   `json:"code_context,omitempty"` // 出错行的前后代码

	SuggestedFix    string   `json:"suggested_fix,omitempty"`
	PossibleCauses  []string `json:"possible_causes,omitempty"`
	ExampleFix      string   `json:"example_fix,omitempty"`

	TestResults     *TestResults `json:"test_results,omitempty"` // 测试失败时的详细结果
}

// [P0] ExecutionResult 结构化执行结果
type ExecutionResult struct {
	Success bool   `json:"success"`
	ExitCode int   `json:"exit_code"`
	Command string `json:"command"`

	Stdout  string `json:"stdout"`
	Stderr  string `json:"stderr"`

	ErrorAnalysis *ErrorAnalysis `json:"error_analysis,omitempty"`
	TestResults   *TestResults   `json:"test_results,omitempty"`

	Duration time.Duration `json:"duration_ms"`
	MemoryMB float64       `json:"memory_mb,omitempty"`
}

// [P0] AnalyzeError 智能错误分析器
func AnalyzeError(result *ExecutionResult) *ErrorAnalysis {
	if result.Success {
		return nil
	}

	analysis := &ErrorAnalysis{
		Message: result.Stderr,
	}

	stderr := result.Stderr

	switch {
	case isImportError(stderr):
		analysis.Type = ErrImport
		analysis.Category = "compiler"
		analysis.Severity = "error"
		analysis.SuggestedFix = "检查 import 语句和包名"
		analysis.PossibleCauses = []string{
			"包名拼写错误",
			"模块路径不正确",
			"循环依赖",
			"包未安装",
		}
		extractLineInfo(stderr, analysis)

	case isSyntaxOrCompileError(stderr):
		analysis.Type = ErrSyntax
		analysis.Category = "compiler"
		analysis.Severity = "error"
		extractLineInfo(stderr, analysis)
		analysis.SuggestedFix = fmt.Sprintf("检查第 %d 行附近的语法", analysis.Line)
		analysis.PossibleCauses = []string{
			"缺少分号或括号",
			"关键字拼写错误",
			"类型不匹配",
			"未闭合的字符串或注释",
		}

	case isRuntimeError(stderr):
		analysis.Type = ErrRuntime
		analysis.Category = "runtime"
		analysis.Severity = "error"
		if panicMsg := extractPanicMessage(stderr); panicMsg != "" {
			analysis.Message = panicMsg
		}
		analysis.SuggestedFix = "添加空指针检查或边界验证"
		analysis.PossibleCauses = []string{
			"未初始化的变量",
			"数组越界访问",
			"类型断言失败",
			"nil 指针解引用",
		}
		extractLineInfo(stderr, analysis)

	case isTestFailure(stderr):
		analysis.Type = ErrTestFail
		analysis.Category = "test"
		analysis.Severity = "error"
		analysis.TestResults = parseTestOutput(stderr)
		analysis.SuggestedFix = "检查断言条件和期望值"
		analysis.PossibleCauses = []string{
			"期望值与实际值不匹配",
			"测试数据不正确",
			"逻辑错误",
			"边界条件未处理",
		}

	case isPermissionError(stderr):
		analysis.Type = ErrPermission
		analysis.Category = "system"
		analysis.Severity = "error"
		analysis.SuggestedFix = "检查文件权限或以管理员身份运行"
		analysis.PossibleCauses = []string{
			"文件被其他进程锁定",
			"没有写入权限",
			"磁盘空间不足",
			"只读文件系统",
		}

	case result.Duration > 30*time.Second:
		analysis.Type = ErrTimeout
		analysis.Category = "runtime"
		analysis.Severity = "warning"
		analysis.SuggestedFix = "优化算法或增加超时时间"
		analysis.PossibleCauses = []string{
			"死循环",
			"网络请求阻塞",
			"I/O 操作缓慢",
			"计算量过大",
		}

	default:
		analysis.Type = ErrUnknown
		analysis.Category = "unknown"
		analysis.Severity = "error"
		analysis.SuggestedFix = "请检查命令输出并手动分析"
		analysis.PossibleCauses = []string{
			"未知错误类型",
			"环境配置问题",
			"依赖缺失",
		}
	}

	if analysis.File != "" && analysis.Line > 0 {
		analysis.CodeContext = fmt.Sprintf("（第 %d 行附近代码需手动查看）", analysis.Line)
	}

	return analysis
}

// [P0] 错误检测辅助函数

func isSyntaxOrCompileError(stderr string) bool {
	keywords := []string{
		"syntax error",
		"expected ",
		"unexpected ",
		"parse error",
		"cannot find package",
		"undefined: ",
		"declared but not used",
		"imported and not used",
	}
	for _, kw := range keywords {
		if strings.Contains(stderr, kw) {
			return true
		}
	}
	return false
}

func isRuntimeError(stderr string) bool {
	keywords := []string{
		"panic:",
		"runtime error",
		"nil pointer",
		"index out of range",
		"invalid memory address",
		"slice bounds out of range",
		"map access",
		"interface conversion",
		"type assertion",
	}
	for _, kw := range keywords {
		if strings.Contains(stderr, kw) {
			return true
		}
	}
	return false
}

func isTestFailure(stderr string) bool {
	keywords := []string{
		"--- FAIL:",
		"Error: Test failed",
		"FAIL\t",
		"AssertionError",
		"assertion failed",
		"Expected.*but got",
	}
	for _, kw := range keywords {
		if strings.Contains(stderr, kw) {
			return true
		}
	}
	return false
}

func isImportError(stderr string) bool {
	keywords := []string{
		"undefined:",
		"imported and not used",
		"cannot find package",
		"no such file or directory",
		"module not found",
	}
	for _, kw := range keywords {
		if strings.Contains(stderr, kw) {
			return true
		}
	}
	return false
}

func isPermissionError(stderr string) bool {
	keywords := []string{
		"permission denied",
		"access denied",
		"PermissionError",
		"AccessDeniedError",
		"read-only file system",
		"text file busy",
	}
	for _, kw := range keywords {
		if strings.Contains(stderr, kw) {
			return true
		}
	}
	return false
}

// [P0] extractLineInfo 从错误信息中提取行号和文件
func extractLineInfo(stderr string, analysis *ErrorAnalysis) {
	patterns := []string{
		`:(\d+):\d+`,                    // Go: file.go:10:5
		`line (\d+)`,                     // Python: line 10
		`at line (\d+)`,                  // Generic: at line 10
		`\((\d+),(\d+)\)`,               // Python: (file.py, 10)
		`:(\d+):`,                        // Simple: :10:
	}

	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		matches := re.FindStringSubmatch(stderr)
		if len(matches) > 1 {
			lineNum, err := strconv.Atoi(matches[1])
			if err == nil {
				analysis.Line = lineNum
				if len(matches) > 2 {
					colNum, err := strconv.Atoi(matches[2])
					if err == nil {
						analysis.Column = colNum
					}
				}
				break
			}
		}
	}

	filePatterns := []string{
		`(\\?[a-zA-Z]:?[/\\][^\s:]+\.go)\s*:`,
		`(\/[^\s]+\.py)\s*`,
		`([a-zA-Z_]\w+\.\w+)\s*[:(]`,
	}

	for _, pattern := range filePatterns {
		re := regexp.MustCompile(pattern)
		matches := re.FindStringSubmatch(stderr)
		if len(matches) > 1 {
			analysis.File = matches[1]
			break
		}
	}
}

// [P0] extractPanicMessage 提取 panic 信息
func extractPanicMessage(stderr string) string {
	re := regexp.MustCompile(`panic:\s*(.+)`)
	matches := re.FindStringSubmatch(stderr)
	if len(matches) > 1 {
		return matches[1]
	}
	return ""
}

// [P0] parseTestOutput 解析测试输出
func parseTestOutput(stderr string) *TestResults {
	results := &TestResults{}

	failRe := regexp.MustCompile(`--- FAIL:\s+(\S+)`)
	fails := failRe.FindAllStringSubmatch(stderr, -1)
	results.Failed = len(fails)
	for _, f := range fails {
		if len(f) > 1 {
			results.FailNames = append(results.FailNames, f[1])
		}
	}

	passRe := regexp.MustCompile(`--- PASS:\s+(\S+)`)
	passes := passRe.FindAllStringSubmatch(stderr, -1)
	results.Passed = len(passes)

	totalRe := regexp.MustCompile(`(ok|FAIL)\s+\S+\s+([\d.]+)s`)
	totalMatches := totalRe.FindStringSubmatch(stderr)
	if len(totalMatches) > 2 {
		fmt.Sscanf(totalMatches[2], "%f", &results.Output)
	}

	results.Total = results.Passed + results.Failed + results.Skipped
	results.Output = stderr

	return results
}

// [P0] FormatErrorForSE 格式化错误信息给 SE
func FormatErrorForSE(analysis *ErrorAnalysis) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("❌ 执行失败 [%s]\n", analysis.Type))
	sb.WriteString(fmt.Sprintf("   消息: %s\n", analysis.Message))

	if analysis.File != "" {
		sb.WriteString(fmt.Sprintf("   📄 文件: %s\n", analysis.File))
		if analysis.Line > 0 {
			sb.WriteString(fmt.Sprintf("   🔢 行号: %d", analysis.Line))
			if analysis.Column > 0 {
				sb.WriteString(fmt.Sprintf(":%d", analysis.Column))
			}
			sb.WriteString("\n")
		}
	}

	if analysis.CodeContext != "" {
		sb.WriteString(fmt.Sprintf("   💻 上下文: %s\n", analysis.CodeContext))
	}

	sb.WriteString(fmt.Sprintf("   💡 建议: %s\n", analysis.SuggestedFix))

	if len(analysis.PossibleCauses) > 0 {
		sb.WriteString("   🔍 可能原因:\n")
		for i, cause := range analysis.PossibleCauses {
			sb.WriteString(fmt.Sprintf("     %d. %s\n", i+1, cause))
		}
	}

	if analysis.ExampleFix != "" {
		sb.WriteString(fmt.Sprintf("   ✏️ 示例修复:\n%s\n", analysis.ExampleFix))
	}

	return sb.String()
}
