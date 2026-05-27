package executor

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// [P0] VerificationRule 验证规则
type VerificationRule struct {
	Name      string                         `json:"name"`
	Condition func(*ExecutionResult) bool     `json:"-"` // 判断是否应用此规则
	Action    func(*ExecutionResult) error   `json:-`     // 执行验证动作
	Mandatory bool                           `json:"mandatory"` // true = 必须通过
}

// [P0] RuleResult 规则执行结果
type RuleResult struct {
	Name    string `json:"name"`
	Passed  bool   `json:"passed"`
	Skipped bool   `json:"skipped"`
	Error   string `json:"error,omitempty"`
}

// [P0] VerificationReport 验证报告
type VerificationReport struct {
	Timestamp time.Time     `json:"timestamp"`
	Passed    bool          `json:"passed"`
	Rules     []RuleResult  `json:"rules"`
	Actions   []ExecutionResult `json:"actions"`
	Summary   string        `json:"summary"`
}

// [P0] VerificationPipeline 验证流水线
type VerificationPipeline struct {
	executor *Executor
	rules    []VerificationRule
}

// [P0] NewDefaultVerificationPipeline 创建默认验证流水线
func NewDefaultVerificationPipeline(exec *Executor) *VerificationPipeline {
	return &VerificationPipeline{
		executor: exec,
		rules: []VerificationRule{
			{
				Name: "编译检查",
				Condition: func(r *ExecutionResult) bool {
					return r.Command == "" && (strings.HasSuffix(r.Command, ".go") || hasGoFiles(exec.workDir))
				},
				Action: func(r *ExecutionResult) error {
					result := exec.ExecWithAnalysis("go build .", 60*time.Second)
					if !result.Success && result.ErrorAnalysis != nil {
						return fmt.Errorf("编译失败 [%s]: %s", 
							result.ErrorAnalysis.Type, 
							result.ErrorAnalysis.Message)
					}
					return nil
				},
				Mandatory: true,
			},
			{
				Name: "测试检查",
				Condition: func(r *ExecutionResult) bool {
					return hasTestFiles(exec.workDir)
				},
				Action: func(r *ExecutionResult) error {
					result := exec.ExecWithAnalysis("go test ./...", 120*time.Second)
					if !result.Success && result.ErrorAnalysis != nil && 
						result.ErrorAnalysis.Type == ErrTestFail {
						if result.TestResults != nil {
							return fmt.Errorf("测试失败: %d 个测试用例未通过 [%v]", 
								result.TestResults.Failed,
								result.TestResults.FailNames)
						}
						return fmt.Errorf("测试失败")
					}
					return nil
				},
				Mandatory: false, // 测试失败不阻塞，但会警告
			},
			{
				Name: "Lint 检查",
				Condition: func(r *ExecutionResult) bool {
					return hasGoFiles(exec.workDir)
				},
				Action: func(r *ExecutionResult) error {
					output, err := exec.Exec("golint ./...", 30*time.Second)
					if err == nil && output != "" {
						fmt.Printf("[Lint] ⚠️ %d 个 lint 问题\n", strings.Count(output, "\n"))
					}
					return nil // Lint 警告不阻塞
				},
				Mandatory: false,
			},
		},
	}
}

// [P0] Run 运行验证流水线
func (vp *VerificationPipeline) Run() (*VerificationReport, error) {
	report := &VerificationReport{
		Timestamp: time.Now(),
		Rules:     make([]RuleResult, 0, len(vp.rules)),
		Passed:    true,
	}

	for _, rule := range vp.rules {
		ruleResult := RuleResult{Name: rule.Name}

		fmt.Printf("[Verification] 🔍 检查规则: %s\n", rule.Name)

		if rule.Action != nil {
			err := rule.Action(nil)
			if err != nil {
				ruleResult.Error = err.Error()
				ruleResult.Passed = false

				if rule.Mandatory {
					report.Passed = false
					report.Rules = append(report.Rules, ruleResult)
					
					report.Summary = fmt.Sprintf("❌ 强制验证失败 [%s]: %s", rule.Name, err)
					fmt.Printf("[Verification] ❌ %s 失败: %s\n", rule.Name, err)
					
					return report, fmt.Errorf("强制验证失败 [%s]: %s", rule.Name, err)
				}
				
				fmt.Printf("[Verification] ⚠️ %s 警告: %s\n", rule.Name, err)
			} else {
				ruleResult.Passed = true
				fmt.Printf("[Verification] ✅ %s 通过\n", rule.Name)
			}
		} else {
			ruleResult.Skipped = true
			fmt.Printf("[Verification] ⏭️ %s 跳过（无操作）\n", rule.Name)
		}

		report.Rules = append(report.Rules, ruleResult)
	}

	if report.Passed {
		report.Summary = "✅ 所有强制验证通过"
		fmt.Println("[Verification] 🎉 所有验证通过！")
	}

	return report, nil
}

// [P0] ExecWithAnalysis 执行命令并返回结构化结果
func (e *Executor) ExecWithAnalysis(command string, timeout time.Duration) *ExecutionResult {
	startTime := time.Now()

	stdout, stderrCombined, exitCode := e.execCommandInternal(command, timeout)

	duration := time.Since(startTime)

	result := &ExecutionResult{
		Command:  command,
		ExitCode: exitCode,
		Duration: duration,
		Success:  exitCode == 0,
	}

	if len(stdout) > 0 {
		result.Stdout = stdout
	}

	if len(stderrCombined) > 0 {
		if exitCode != 0 {
			result.Stderr = stderrCombined
		} else {
			result.Stdout += "\n" + stderrCombined
		}
	}

	if !result.Success {
		result.ErrorAnalysis = AnalyzeError(result)
	}

	if isTestCommand(command) && result.ErrorAnalysis != nil && 
		result.ErrorAnalysis.Type == ErrTestFail {
		result.TestResults = parseTestOutput(result.Stderr)
	}

	return result
}

// [P0] execCommandInternal 内部命令执行（返回分离的 stdout/stderr）
func (e *Executor) execCommandInternal(command string, timeout time.Duration) (stdout, stderr string, exitCode int) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "cmd", "/c", command)
	cmd.Dir = e.workDir

	var stdoutBuf, stderrBuf strings.Builder
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	err := cmd.Run()
	exitCode = 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else if ctx.Err() == context.DeadlineExceeded {
			exitCode = -1
		} else {
			exitCode = 1
		}
	}

	stdout = stdoutBuf.String()
	stderr = stderrBuf.String()

	fmt.Printf("[Executor-Internal] Command: %s | ExitCode: %d | Duration: %v\n",
		command, exitCode, timeout)

	return
}

// [P0] 辅助函数

func hasGoFiles(dir string) bool {
	files, _ := filepath.Glob(filepath.Join(dir, "*.go"))
	return len(files) > 0
}

func hasTestFiles(dir string) bool {
	patterns := []string{"*_test.go", "*.test.js", "*.test.py", "*.spec.js"}
	for _, pattern := range patterns {
		files, _ := filepath.Glob(filepath.Join(dir, pattern))
		if len(files) > 0 {
			return true
		}
	}
	return false
}

func isTestCommand(command string) bool {
	testKeywords := []string{"go test", "npm test", "pytest", "jest", "mocha"}
	for _, kw := range testKeywords {
		if strings.Contains(command, kw) {
			return true
		}
	}
	return false
}

// [P0] FormatVerificationReport 格式化验证报告
func FormatVerificationReport(report *VerificationReport) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("\n📋 **验证报告** (%s)\n", 
		report.Timestamp.Format("15:04:05")))
	
	sb.WriteString(fmt.Sprintf("状态: %s\n\n", map[bool]string{true: "✅ 通过", false: "❌ 未通过"}[report.Passed]))

	sb.WriteString("**规则详情**:\n")
	for _, rule := range report.Rules {
		status := "⏭️ 跳过"
		switch {
		case rule.Passed:
			status = "✅ 通过"
		case rule.Error != "":
			status = fmt.Sprintf("❌ 失败: %s", rule.Error)
		}

		sb.WriteString(fmt.Sprintf("  - [%s] %s\n", rule.Name, status))
	}

	if report.Summary != "" {
		sb.WriteString(fmt.Sprintf("\n%s\n", report.Summary))
	}

	return sb.String()
}
