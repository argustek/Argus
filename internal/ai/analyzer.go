package ai

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// IssueSeverity 问题严重程度
type IssueSeverity string

const (
	SeverityCritical IssueSeverity = "critical" // 必须修复（如 nil panic、数据竞争）
	SeverityWarning  IssueSeverity = "warning"  // 建议修复（如未检查 error、资源泄漏）
	SeverityInfo     IssueSeverity = "info"     // 提示（如代码风格、可读性）
	SeverityHint     IssueSeverity = "hint"     // 建议（如可简化的写法）
)

// IssueCategory 问题分类
type IssueCategory string

const (
	CatNilSafety      IssueCategory = "nil_safety"       // 空指针安全
	CatBounds         IssueCategory = "bounds"           // 边界检查
	CatResource       IssueCategory = "resource"         // 资源管理
	CatConcurrency    IssueCategory = "concurrency"      // 并发安全
	CatErrorHandling  IssueCategory = "error_handling"   // 错误处理
	CatLogic          IssueCategory = "logic"            // 逻辑问题
	CatSecurity       IssueCategory = "security"         // 安全隐患
	CatPerformance    IssueCategory = "performance"      // 性能问题
	CatStyle          IssueCategory = "style"            // 代码风格
)

// AnalysisIssue 分析发现的问题
type AnalysisIssue struct {
	File        string         `json:"file"`         // 文件路径（相对）
	Line        int            `json:"line"`         // 行号（1-based）
	EndLine     int            `json:"end_line"`     // 结束行号
	Column      int            `json:"column"`       // 列号
	Severity    IssueSeverity  `json:"severity"`      // 严重程度
	Category    IssueCategory  `json:"category"`      // 分类
	RuleID      string         `json:"rule_id"`      // 规则ID（如 NIL001）
	Title       string         `json:"title"`        // 简短标题
	Description string         `json:"description"`  // 详细描述
	CodeSnippet string         `json:"code_snippet"` // 问题代码片段
	Suggestion  string         `json:"suggestion"`   // 修复建议
}

// AnalyzeOptions 分析选项
type AnalyzeOptions struct {
	Path      string   `json:"path"`       // 文件或目录路径
	Categories []string `json:"categories"` // 要检查的分类（空=全部）
	MinLevel   string   `json:"min_level"`  // 最低严重程度（critical/warning/info/hint）
	MaxIssues  int      `json:"max_issues"` // 最大返回数量（0=不限）
}

// AnalyzeResult 分析结果
type AnalyzeResult struct {
	Summary    SummaryStats   `json:"summary"`    // 统计摘要
	Issues     []AnalysisIssue `json:"issues"`     // 问题列表
	FileCount  int            `json:"file_count"` // 扫描文件数
	DurationMs int64          `json:"duration_ms"`
}

// SummaryStats 统计摘要
type SummaryStats struct {
	Total      int            `json:"total"`
	Critical   int            `json:"critical"`
	Warning    int            `json:"warning"`
	Info       int            `json:"info"`
	Hint       int            `json:"hint"`
	Categories map[string]int `json:"categories"` // 各分类的问题数
}

// CodeAnalyzer 静态代码分析器
type CodeAnalyzer struct {
	workDir string
	fset    *token.FileSet
}

// NewCodeAnalyzer 创建分析器
func NewCodeAnalyzer(workDir string) *CodeAnalyzer {
	return &CodeAnalyzer{
		workDir: workDir,
		fset:    token.NewFileSet(),
	}
}

// Analyze 执行分析（入口）
func (a *CodeAnalyzer) Analyze(opts AnalyzeOptions) (*AnalyzeResult, error) {
	result := &AnalyzeResult{
		Issues: make([]AnalysisIssue, 0),
		Summary: SummaryStats{
			Categories: make(map[string]int),
		},
	}

	absPath := filepath.Join(a.workDir, opts.Path)
	info, err := os.Stat(absPath)
	if err != nil {
		return nil, fmt.Errorf("路径不存在: %s: %w", opts.Path, err)
	}

	minSev := parseSeverity(opts.MinLevel)

	if info.IsDir() {
		// 目录：递归扫描 .go 文件
		err = a.walkDir(absPath, opts.Path, minSev, opts.Categories, result)
	} else {
		// 单文件
		err = a.analyzeFile(absPath, opts.Path, minSev, opts.Categories, result)
	}
	if err != nil {
		return nil, err
	}

	// 截断
	if opts.MaxIssues > 0 && len(result.Issues) > opts.MaxIssues {
		result.Issues = result.Issues[:opts.MaxIssues]
	}

	// 统计
	for _, issue := range result.Issues {
		result.Summary.Total++
		switch issue.Severity {
		case SeverityCritical:
			result.Summary.Critical++
		case SeverityWarning:
			result.Summary.Warning++
		case SeverityInfo:
			result.Summary.Info++
		case SeverityHint:
			result.Summary.Hint++
		}
		result.Summary.Categories[string(issue.Category)]++
	}

	return result, nil
}

// walkDir 递归扫描目录
func (a *CodeAnalyzer) walkDir(absDir, relDir string, minSev IssueSeverity, categories []string, result *AnalyzeResult) error {
	entries, err := os.ReadDir(absDir)
	if err != nil {
		return nil // 跳过无权限目录
	}

	for _, entry := range entries {
		name := entry.Name()
		// 跳过隐藏目录和 vendor
		if strings.HasPrefix(name, ".") || name == "vendor" || name == "node_modules" {
			continue
		}

		fullPath := filepath.Join(absDir, name)
		relPath := filepath.Join(relDir, name)

		if entry.IsDir() {
			a.walkDir(fullPath, relPath, minSev, categories, result)
		} else if strings.HasSuffix(name, ".go") {
			// 跳过测试文件和生成文件
			if strings.HasSuffix(name, "_test.go") || strings.HasSuffix(name, "_gen.go") {
				continue
			}
			a.analyzeFile(fullPath, relPath, minSev, categories, result)
			result.FileCount++
		}
	}
	return nil
}

// analyzeFile 分析单个 Go 文件
func (a *CodeAnalyzer) analyzeFile(absPath, relPath string, minSev IssueSeverity, categories []string, result *AnalyzeResult) error {
	src, err := os.ReadFile(absPath)
	if err != nil {
		return nil
	}

	// 1. AST 分析（精确的语法级检查）
	astIssues := a.analyzeAST(relPath, src)

	// 2. 正则模式匹配（补充 AST 无法覆盖的模式）
	regexIssues := a.analyzePatterns(relPath, string(src))

	// 合并去重 + 过滤
	allIssues := append(astIssues, regexIssues...)
	for _, issue := range allIssues {
		if issue.Severity < minSev {
			continue
		}
		if len(categories) > 0 && !containsCat(categories, string(issue.Category)) {
			continue
		}
		result.Issues = append(result.Issues, issue)
	}

	return nil
}

// analyzeAST 基于 Go AST 的精确分析
func (a *CodeAnalyzer) analyzeAST(relPath string, src []byte) []AnalysisIssue {
	var issues []AnalysisIssue

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, parser.ParseComments|parser.AllErrors)
	if err != nil {
		// 解析失败，跳过 AST 分析
		return nil
	}

	ast.Inspect(f, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.FuncDecl:
			issues = append(issues, a.checkFuncDecl(fset, relPath, node)...)

		case *ast.SelectorExpr:
			issues = append(issues, a.checkSelectorExpr(fset, relPath, node)...)

		case *ast.CallExpr:
			issues = append(issues, a.checkCallExpr(fset, relPath, node)...)

		case *ast.AssignStmt:
			issues = append(issues, a.checkAssignStmt(fset, relPath, node)...)

		case *ast.IfStmt:
			issues = append(issues, a.checkIfStmt(fset, relPath, node)...)

		case *ast.ForStmt, *ast.RangeStmt:
			issues = append(issues, a.checkLoop(fset, relPath, n)...)

		case *ast.GoStmt:
			issues = append(issues, a.checkGoStmt(fset, relPath, node)...)

		case *ast.DeferStmt:
			issues = append(issues, a.checkDeferStmt(fset, relPath, node)...)

		case *ast.ReturnStmt:
			issues = append(issues, a.checkReturnStmt(fset, relPath, node)...)

		case *ast.BranchStmt:
			issues = append(issues, a.checkBranchStmt(fset, relPath, node)...)
		}
		return true
	})

	return issues
}

// ========== 各类检查规则实现 ==========

// checkFuncDecl 函数声明检查
func (a *CodeAnalyzer) checkFuncDecl(fset *token.FileSet, relPath string, fn *ast.FuncDecl) []AnalysisIssue {
	var issues []AnalysisIssue

	// 规则: 公开函数缺少注释
	if fn.Name.IsExported() && fn.Doc == nil {
		pos := fset.Position(fn.Pos())
		issues = append(issues, AnalysisIssue{
			File: relPath, Line: pos.Line, Column: pos.Column,
			Severity: SeverityHint, Category: CatStyle,
			RuleID: "STYLE001",
			Title:       "公开函数缺少文档注释",
			Description: fmt.Sprintf("函数 %s 是导出的（首字母大写），但缺少文档注释", fn.Name.Name),
			CodeSnippet: fn.Name.Name,
			Suggestion:  fmt.Sprintf("// %s 做了什么...", fn.Name.Name),
		})
	}

	// 规则: 函数体过长 (>80 行)
	if fn.Body != nil {
		endPos := fset.Position(fn.Body.Rbrace)
		startPos := fset.Position(fn.Body.Lbrace)
		if endPos.Line-startPos.Line > 80 {
			issues = append(issues, AnalysisIssue{
				File: relPath, Line: startPos.Line,
				Severity: SeverityInfo, Category: CatStyle,
				RuleID: "STYLE002",
				Title:       "函数体过长",
				Description: fmt.Sprintf("函数 %s 有 %d 行，建议拆分为更小的函数", fn.Name.Name, endPos.Line-startPos.Line),
				Suggestion:  "考虑将部分逻辑提取为独立函数",
			})
		}
	}

	return issues
}

// checkSelectorExpr 选择器表达式检查（如 x.Y）
func (a *CodeAnalyzer) checkSelectorExpr(fset *token.FileSet, relPath string, expr *ast.SelectorExpr) []AnalysisIssue {
	var issues []AnalysisIssue

	// 检查是否是 .Err() 或 .Error() 的忽略模式
	if ident, ok := expr.X.(*ast.Ident); ok {
		selName := expr.Sel.Name

		// 规则: _ = err 或 err 忽略错误
		if selName == "Err" || selName == "Error" {
			pos := fset.Position(expr.Pos())
			issues = append(issues, AnalysisIssue{
				File: relPath, Line: pos.Line, Column: pos.Column,
				Severity: SeverityWarning, Category: CatErrorHandling,
				RuleID: "ERR001",
				Title:       "可能的错误被忽略",
				Description: fmt.Sprintf("变量 %s 的 Error/Err 属性被访问但可能未被处理", ident.Name),
				Suggestion:  "请确认错误已被正确处理（log/return/wrap）",
			})
		}
	}

	return issues
}

// checkCallExpr 函数调用检查
func (a *CodeAnalyzer) checkCallExpr(fset *token.FileSet, relPath string, call *ast.CallExpr) []AnalysisIssue {
	var issues []AnalysisIssue

	// 获取调用函数名
	funcName := getCallFuncName(call)
	pos := fset.Position(call.Pos())

	// 规则: os.Open / os.Create 未检查错误
	if (funcName == "os.Open" || funcName == "os.Create" || funcName == "os.OpenFile") &&
		!isErrorChecked(call) {
		issues = append(issues, AnalysisIssue{
			File: relPath, Line: pos.Line, Column: pos.Column,
			Severity: SeverityCritical, Category: CatErrorHandling,
			RuleID: "ERR002",
			Title:       "文件操作未检查错误",
			Description: fmt.Sprintf("%s 返回值可能包含错误，但未看到错误检查", funcName),
			CodeSnippet: funcName,
			Suggestion:  "添加 if err != nil { return err } 检查",
		})
	}

	// 规则: json.Unmarshal 未检查错误
	if funcName == "json.Unmarshal" && !isErrorChecked(call) {
		issues = append(issues, AnalysisIssue{
			File: relPath, Line: pos.Line, Column: pos.Column,
			Severity: SeverityWarning, Category: CatErrorHandling,
			RuleID: "ERR003",
			Title:       "JSON 反序列化未检查错误",
			Description: "json.Unmarshal 可能因格式错误失败，建议检查返回的错误",
			Suggestion:  "if err := json.Unmarshal(...); err != nil { ... }",
		})
	}

	// 规则: fmt.Sprintf 在循环内（性能）
	if funcName == "fmt.Sprintf" {
		issues = append(issues, AnalysisIssue{
			File: relPath, Line: pos.Line, Column: pos.Column,
			Severity: SeverityHint, Category: CatPerformance,
			RuleID: "PERF001",
			Title:       "fmt.Sprintf 性能提示",
			Description: "频繁调用 fmt.Sprintf 可能有性能开销，考虑使用 fmt.Fprintf 或 strings.Builder",
			Suggestion:  "高频场景下可用 strings.Builder 替代",
		})
	}

	// 规则: log.Fatal / os.Exit 在非 main 函数中
	if funcName == "log.Fatal" || funcName == "log.Fatalf" || funcName == "os.Exit" {
		issues = append(issues, AnalysisIssue{
			File: relPath, Line: pos.Line, Column: pos.Column,
			Severity: SeverityWarning, Category: CatLogic,
			RuleID: "LOG001",
			Title:       "使用 Fatal/Exit 终止程序",
			Description: fmt.Sprintf("%s 会直接终止进程（不执行 defer），在库代码中应避免", funcName),
			Suggestion:  "改为 return error 让调用者决定如何处理",
		})
	}

	return issues
}

// checkAssignStmt 赋值语句检查
func (a *CodeAnalyzer) checkAssignStmt(fset *token.FileSet, relPath string, stmt *ast.AssignStmt) []AnalysisIssue {
	var issues []AnalysisIssue

	// 遍历 LHS（左侧变量），处理多值赋值场景（f, _ := os.Open()）
	for i, lhs := range stmt.Lhs {
		if id, ok := lhs.(*ast.Ident); ok && id.Name == "_" {
			// _ = something → 可能丢弃了重要值
			// 获取对应的 RHS（如果 RHS 长度不够则取最后一个）
			rhsIdx := i
			if rhsIdx >= len(stmt.Rhs) {
				rhsIdx = len(stmt.Rhs) - 1
			}
			if rhsIdx >= 0 {
				if call, ok := stmt.Rhs[rhsIdx].(*ast.CallExpr); ok {
					funcName := getCallFuncName(call)
					if isErrorReturning(funcName) {
						pos := fset.Position(stmt.Pos())
						issues = append(issues, AnalysisIssue{
							File: relPath, Line: pos.Line, Column: pos.Column,
							Severity: SeverityWarning, Category: CatErrorHandling,
							RuleID: "ERR004",
							Title:       "错误值被丢弃",
							Description: fmt.Sprintf("%s 返回的错误被 _ 丢弃", funcName),
							Suggestion:  "应该处理或向上传播这个错误",
						})
					}
				}
			}
		}
	}

	return issues
}

// checkIfStmt 条件语句检查
func (a *CodeAnalyzer) checkIfStmt(fset *token.FileSet, relPath string, stmt *ast.IfStmt) []AnalysisIssue {
	var issues []AnalysisIssue

	// 规则: if err != nil { return } 后面紧跟相同变量的重复检查
	// （这个需要跨语句分析，简化版先做基本检查）

	// 规则: if x == nil 但 x 可能不是指针类型（简化启发式）
	if stmt.Init != nil {
		pos := fset.Position(stmt.Init.Pos())
		issues = append(issues, AnalysisIssue{
			File: relPath, Line: pos.Line, Column: pos.Column,
			Severity: SeverityInfo, Category: CatNilSafety,
			RuleID: "NIL001",
			Title:       "if 语句有初始化子句",
			Description: "if 初始化子句中的变量作用域仅限于 if 块，确保这是预期行为",
			Suggestion:  "如果后续需要该变量，考虑将初始化移到 if 外部",
		})
	}

	return issues
}

// checkLoop 循环检查
func (a *CodeAnalyzer) checkLoop(fset *token.FileSet, relPath string, n ast.Node) []AnalysisIssue {
	var issues []AnalysisIssue
	var body *ast.BlockStmt
	var pos token.Pos

	switch loop := n.(type) {
	case *ast.ForStmt:
		body = loop.Body
		pos = loop.Pos()
	case *ast.RangeStmt:
		body = loop.Body
		pos = loop.Pos()
	default:
		return nil
	}

	startPos := fset.Position(pos)

	// 规则: 循环体内 defer（defer 不会在每次迭代结束时执行！）
	for _, stmt := range body.List {
		if _, ok := stmt.(*ast.DeferStmt); ok {
			issues = append(issues, AnalysisIssue{
				File: relPath, Line: startPos.Line, Column: startPos.Column,
				Severity: SeverityCritical, Category: CatResource,
				RuleID: "RES001",
				Title:       "循环体内的 defer",
				Description: "defer 在循环中不会按预期工作——它会延迟到函数返回时才执行，而非每次迭代结束",
				Suggestion:  "将需要关闭的资源管理移到单独的函数中，或在循环内显式关闭",
			})
			break // 只报一次
		}
	}

	return issues
}

// checkGoStmt goroutine 检查
func (a *CodeAnalyzer) checkGoStmt(fset *token.FileSet, relPath string, stmt *ast.GoStmt) []AnalysisIssue {
	var issues []AnalysisIssue
	pos := fset.Position(stmt.Pos())

	// 规则: goroutine 内的 panic 无法被外层 recover 捕获
	issues = append(issues, AnalysisIssue{
		File: relPath, Line: pos.Line, Column: pos.Column,
		Severity: SeverityWarning, Category: CatConcurrency,
		RuleID: "GOR001",
		Title:       "goroutine 未受保护",
		Description: "新启动的 goroutine 如果发生 panic，会导致整个程序崩溃（除非 main 有 recover）",
		Suggestion:  "在 goroutine 入口添加 defer recover()，或使用 errgroup 管理生命周期",
	})

	return issues
}

// checkDeferStmt defer 语句检查
func (a *CodeAnalyzer) checkDeferStmt(fset *token.FileSet, relPath string, stmt *ast.DeferStmt) []AnalysisIssue {
	var issues []AnalysisIssue
	pos := fset.Position(stmt.Pos())

	funcName := getCallFuncName(stmt.Call)
	// 规则: defer Close() 但不确定是否为 nil-safe
	if funcName == "Close" {
		issues = append(issues, AnalysisIssue{
			File: relPath, Line: pos.Line, Column: pos.Column,
			Severity: SeverityInfo, Category: CatResource,
			RuleID: "RES002",
			Title:       "defer Close() 安全性提醒",
			Description: "如果变量可能为 nil，Close() 调用会 panic。建议包装为安全的 close 函数。",
			Suggestion:  "defer safeClose(&f) 其中 safeClose 检查 nil 后再 Close",
		})
	}

	return issues
}

// checkReturnStmt 返回语句检查
func (a *CodeAnalyzer) checkReturnStmt(fset *token.FileSet, relPath string, stmt *ast.ReturnStmt) []AnalysisIssue {
	var issues []AnalysisIssue

	// 规则: naked return（裸返回）降低可读性
	if len(stmt.Results) == 0 {
		pos := fset.Position(stmt.Pos())
		issues = append(issues, AnalysisIssue{
			File: relPath, Line: pos.Line, Column: pos.Column,
			Severity: SeverityHint, Category: CatStyle,
			RuleID: "STYLE003",
			Title:       "裸返回",
			Description: "使用裸返回（return 无参数）会降低代码可读性，尤其是多返回值时",
			Suggestion:  "明确写出返回值以提高可读性",
		})
	}

	return issues
}

// checkBranchStmt 分支语句检查
func (a *CodeAnalyzer) checkBranchStmt(fset *token.FileSet, relPath string, stmt *ast.BranchStmt) []AnalysisIssue {
	var issues []AnalysisIssue

	// 规则: 标签化的 break/continue（goto 替代品）
	if stmt.Label != nil {
		pos := fset.Position(stmt.Pos())
		issues = append(issues, AnalysisIssue{
			File: relPath, Line: pos.Line, Column: pos.Column,
			Severity: SeverityInfo, Category: CatStyle,
			RuleID: "STYLE004",
			Title:       "标签化分支",
			Description: fmt.Sprintf("标签化的 %s 会增加控制流复杂度", stmt.Tok.String()),
			Suggestion:  "考虑重构为函数调用或提取方法来替代标签跳转",
		})
	}

	return issues
}

// ========== 正则模式匹配（补充 AST 无法覆盖的模式） ==========

var patternRules = []struct {
	re        *regexp.Regexp
	severity  IssueSeverity
	category  IssueCategory
	ruleID    string
	title     string
	descTmpl  string
	suggest   string
}{
	// nil 检测相关
	{regexp.MustCompile(`(?m)^.*\bnil\s*==\s*\w+`), SeverityWarning, CatNilSafety, "NIL002",
		"nil == Yoda 写法", "使用了 Yoda 风格的 nil 比较（nil == x），虽然合法但不惯用",
		"建议改为 x == nil 或 x != nil"},
	{regexp.MustCompile(`(?m)^.*\bmake\(\[\]`), SeverityInfo, CatBounds, "BND001",
		"创建空 slice", "make([]T, 0) 可以简化为 []T{}，除非后续需要 append 且关心容量预分配",
		"如果确定不需要预分配容量，可用 T{} 字面量"},

	// 并发相关
	{regexp.MustCompile(`\bsync\.Mutex\b`), SeverityInfo, CatConcurrency, "CON001",
		"使用互斥锁", "检测到 Mutex 使用，请确保 Lock/Unlock 配对且不会遗漏 Unlock（建议用 defer）",
		"推荐 defer mu.Unlock() 确保 unlock"},
	{regexp.MustCompile(`\batomic\.`), SeverityInfo, CatConcurrency, "CON002",
		"使用原子操作", "检测到 atomic 操作，注意原子操作不能与普通读写混用同一变量",
		"确保对同一变量的所有访问都通过 atomic 包"},

	// 安全相关
	{regexp.MustCompile(`(?i)(md5|sha1)\.(New|Sum)`), SeverityWarning, CatSecurity, "SEC001",
		"弱哈希算法", "MD5 和 SHA1 已被认为不够安全，不适合密码学用途",
		"密码学场景改用 SHA256 或更高；如果只是校验和则可以保留"},
	{regexp.MustCompile(`(?i)rand\.(Int|Intn|Float64)\(`), SeverityWarning, CatSecurity, "SEC002",
		"伪随机数", "math/rand 不是密码学安全的随机数",
		"安全场景使用 crypto/rand，普通场景加 rand.Seed 或用 rand.New"},
	{regexp.MustCompile(`(?i)(exec|os/exec)\.Command`), SeverityInfo, CatSecurity, "SEC003",
		"命令执行", "检测到外部命令执行，注意防范命令注入风险",
		"确保输入经过验证和转义，避免用户输入直接拼接到命令中"},

	// 错误处理
	{regexp.MustCompile(`panic\(`), SeverityWarning, CatErrorHandling, "ERR005",
		"panic 调用", "在生产代码中应尽量避免 panic（除非是真正的不可恢复错误）",
		"库代码中应 return error 而非 panic；只在初始化阶段或不变量违反时使用"},
	{regexp.MustCompile(`recover\(\)`), SeverityInfo, CatErrorHandling, "ERR006",
		"recover 使用", "检测到 recover，确保它在 defer 中使用且能正确捕获 panic",
		"recover 只在 defer 中有效，其他位置调用会返回 nil"},

	// 性能
	{regexp.MustCompile(`\+.*\+.*\+.*\+`), SeverityHint, CatPerformance, "PERF002",
		"字符串拼接链", "多次字符串拼接（+ 链）会产生临时对象，影响性能",
		"循环中使用 strings.Builder 或 fmt.Sprintf 替代"},
	{regexp.MustCompile(`append\([^)]+\)\[0\]`), SeverityWarning, CatPerformance, "PERF003",
		"append 后取索引 0", "append 后立即取 [0] 可能导致不必要的扩容",
		"如果只需要第一个元素，考虑直接访问而不是 append"},

	// 资源管理
	{regexp.MustCompile(`tempFile|TempFile|TempDir|ioutil\.TempFile`), SeverityInfo, CatResource, "RES003",
		"临时文件", "检测到临时文件操作，确保在使用后清理（defer os.Remove）",
		"使用 defer os.Remove(name) 或 t.Cleanup() 确保清理"},
	{regexp.MustCompile(`http\.Get\(|http\.Post\(`), SeverityWarning, CatResource, "RES004",
		"HTTP 客户端", "使用默认 http.Get/Post 不会设置超时，可能导致请求永久挂起",
		"使用自定义 http.Client 并设置 Timeout 字段"},
}

// analyzePatterns 正则模式匹配分析
func (a *CodeAnalyzer) analyzePatterns(relPath, src string) []AnalysisIssue {
	var issues []AnalysisIssue
	lines := strings.Split(src, "\n")

	for _, rule := range patternRules {
		matches := rule.re.FindAllStringIndex(src, -1)
		for _, match := range matches {
			// 计算行号
			lineNum := 1
			colNum := 1
			for _, ch := range src[:match[0]] {
				if ch == '\n' {
					lineNum++
					colNum = 1
				} else {
					colNum++
				}
			}

			// 提取代码片段
			snippet := src[match[0]:match[1]]
			if len(snippet) > 100 {
				snippet = snippet[:100] + "..."
			}
			// 补充上下文行
			if lineNum <= len(lines) {
				contextLine := lines[lineNum-1]
				if len(contextLine) > 120 {
					contextLine = contextLine[:120] + "..."
				}
				snippet = contextLine
			}

			issues = append(issues, AnalysisIssue{
				File: relPath, Line: lineNum, Column: colNum,
				Severity: rule.severity, Category: rule.category,
				RuleID:      rule.ruleID,
				Title:       rule.title,
				Description: rule.descTmpl,
				CodeSnippet: snippet,
				Suggestion:  rule.suggest,
			})
		}
	}

	return issues
}

// ========== 辅助函数 ==========

// FormatResults 格式化分析结果供展示
func (r *AnalyzeResult) FormatResults() string {
	if r == nil || len(r.Issues) == 0 {
		return "✅ 未发现问题，代码质量良好！"
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("📋 代码分析报告 — 共发现 **%d** 个问题（扫描 %d 个文件）\n\n", r.Summary.Total, r.FileCount))

	// 按严重程度分组
	bySeverity := map[IssueSeverity][]AnalysisIssue{}
	for _, iss := range r.Issues {
		bySeverity[iss.Severity] = append(bySeverity[iss.Severity], iss)
	}

	order := []IssueSeverity{SeverityCritical, SeverityWarning, SeverityInfo, SeverityHint}
	labels := map[IssueSeverity]string{
		SeverityCritical: "🔴 严重 (必须修复)",
		SeverityWarning:  "🟡 警告 (建议修复)",
		SeverityInfo:     "🔵 提示",
		SeverityHint:     "⚪ 建议",
	}

	for _, sev := range order {
		list, ok := bySeverity[sev]
		if !ok || len(list) == 0 {
			continue
		}
		sb.WriteString(fmt.Sprintf("### %s (%d)\n\n", labels[sev], len(list)))
		for _, iss := range list {
			sb.WriteString(fmt.Sprintf("- **[%s]** `%s:%d` — %s\n", iss.RuleID, iss.File, iss.Line, iss.Title))
			sb.WriteString(fmt.Sprintf("  > %s\n", iss.Description))
			if iss.CodeSnippet != "" {
				cs := iss.CodeSnippet
				if len(cs) > 80 {
					cs = cs[:80] + "..."
				}
				sb.WriteString(fmt.Sprintf("  > 代码: `%s`\n", cs))
			}
			sb.WriteString(fmt.Sprintf("  > 💡 %s\n\n", iss.Suggestion))
		}
	}

	// 底部统计
	sb.WriteString(fmt.Sprintf("---\n**统计**: 🔴%d 🟡%d 🔵%d ⚪%d | ",
		r.Summary.Critical, r.Summary.Warning, r.Summary.Info, r.Summary.Hint))
	if len(r.Summary.Categories) > 0 {
		parts := make([]string, 0, len(r.Summary.Categories))
		for cat, count := range r.Summary.Categories {
			parts = append(parts, fmt.Sprintf("%s:%d", cat, count))
		}
		sb.WriteString(strings.Join(parts, " | "))
	}

	return sb.String()
}

// getCallFuncName 获取调用表达式的函数名
func getCallFuncName(call *ast.CallExpr) string {
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		return fn.Name
	case *ast.SelectorExpr:
		if x, ok := fn.X.(*ast.Ident); ok {
			return x.Name + "." + fn.Sel.Name
		}
	}
	return ""
}

// isErrorChecked 简化判断：调用是否紧跟赋值给 err 变量
// 注意：这是一个启发式检查，不是完整的流分析
func isErrorChecked(call *ast.CallExpr) bool {
	// 这个检查需要在父节点层面进行，这里只做标记
	// 实际的精确检查需要完整的数据流分析
	return false
}

// isErrorReturning 判断函数名是否通常返回 error
func isErrorReturning(funcName string) bool {
	errorProne := map[string]bool{
		"os.Open": true, "os.Create": true, "os.OpenFile": true,
		"io.ReadAll": true, "ioutil.ReadAll": true,
		"json.Marshal": true, "json.Unmarshal": true,
		"http.Get": true, "http.Post": true, "http.Do": true,
		"database/sql.DB.Query": true, "database/sql.DB.Exec": true,
		"exec.Command": true, "exec.LookPath": true,
		"net.Dial": true, "net.Listen": true,
		"encoding/json.Marshal": true, "encoding/json.Unmarshal": true,
	}
	_, ok := errorProne[funcName]
	return ok
}

func parseSeverity(s string) IssueSeverity {
	switch s {
	case "critical":
		return SeverityCritical
	case "warning":
		return SeverityWarning
	case "info":
		return SeverityInfo
	case "hint":
		return SeverityHint
	default:
		return SeverityHint // 默认显示全部
	}
}

func containsCat(categories []string, cat string) bool {
	for _, c := range categories {
		if c == cat {
			return true
		}
	}
	return false
}
