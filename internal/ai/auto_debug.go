package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// AutoDebugResult 自动调试结果
type AutoDebugResult struct {
	Success      bool           `json:"success"`       // 最终是否通过
	Iterations   int            `json:"iterations"`    // 循环次数
	TotalFixes   int            `json:"total_fixes"`   // 总修复次数
	InitialError string         `json:"initial_error"`  // 首次错误摘要
	FinalOutput  string         `json:"final_output"`  // 最终测试输出
	FixHistory   []DebugFix     `json:"fix_history"`  // 修复历史
	DurationMs   int64          `json:"duration_ms"`  // 总耗时(ms)
}

// DebugFix 单次修复记录
type DebugFix struct {
	Iteration   int    `json:"iteration"`
	ErrorSnip   string `json:"error_snip"`   // 错误片段
	Analysis    string `json:"analysis"`      // AI 分析
	Action       string `json:"action"`        // 采取的行动描述
	FilePath    string `json:"file_path"`     // 修改的文件
	OldCode     string `json:"old_code"`      // 修改前的代码
	NewCode     string `json:"new_code"`      // 修改后的代码
	Result      string `json:"result"`        // 修复后结果: pass/fail
}

// AutoDebugConfig 自动调试配置
type AutoDebugConfig struct {
	MaxIterations int           `json:"max_iterations"` // 最大循环次数（默认3）
	TestTimeout   time.Duration `json:"test_timeout"`    // 单次测试超时（默认60s）
	WorkDir       string        `json:"work_dir"`        // 工作目录
	TestCommand   string        `json:"test_command"`    // 测试命令（默认 go test -v -count=1 ./...）
	SpecificTests  string        `json:"specific_tests"`   // 指定测试（如 ./internal/ai/...）
}

// DefaultAutoDebugConfig 返回默认配置
func DefaultAutoDebugConfig(workDir string) AutoDebugConfig {
	return AutoDebugConfig{
		MaxIterations: 3,
		TestTimeout:   60 * time.Second,
		WorkDir:       workDir,
		TestCommand:   "go test -v -count=1",
	}
}

// AutoDebugger 自动调试器：跑测试 → 分析错误 → AI修复 → 重跑（循环直到通过）
type AutoDebugger struct {
	config AutoDebugConfig
	client *Client
	execFn func(command string, timeout time.Duration) (string, error)
}

// NewAutoDebugger 创建自动调试器
// execFn 是执行命令的回调（由 Manager 注入，避免循环依赖）
func NewAutoDebugger(config AutoDebugConfig, client *Client, execFn func(string, time.Duration) (string, error)) *AutoDebugger {
	return &AutoDebugger{
		config: config,
		client: client,
		execFn: execFn,
	}
}

// Run 执行自动调试循环
func (d *AutoDebugger) Run(ctx context.Context) (*AutoDebugResult, error) {
	start := time.Now()
	result := &AutoDebugResult{
		FixHistory: make([]DebugFix, 0),
	}

	// 构建完整测试命令
	testCmd := d.config.TestCommand
	if d.config.SpecificTests != "" {
		testCmd = fmt.Sprintf("%s %s", testCmd, d.config.SpecificTests)
	}

	for i := 0; i < d.config.MaxIterations; i++ {
		result.Iterations = i + 1

		// 1. 跑测试
		output, err := d.execFn(testCmd, d.config.TestTimeout)
		if err != nil {
			output = fmt.Sprintf("%s\n_ERROR: %v", output, err)
		}

		// 2. 检查是否通过
		if isTestPassed(output) {
			result.Success = true
			result.FinalOutput = TruncateOutput(output, 2000)
			result.DurationMs = time.Since(start).Milliseconds()
			return result, nil
		}

		// 3. 首次失败记录错误
		if i == 0 {
			result.InitialError = extractErrorSummary(output)
		}

		// 4. AI 分析错误并生成修复方案
		analysis, fix, analyzeErr := d.analyzeAndFix(ctx, output, i+1)
		if analyzeErr != nil {
			result.FinalOutput = fmt.Sprintf("AI 分析失败: %v\n\n原始输出:\n%s", analyzeErr, TruncateOutput(output, 2000))
			result.DurationMs = time.Since(start).Milliseconds()
			return result, nil
		}

		// 5. 记录修复历史
		fix.Iteration = i + 1
		result.FixHistory = append(result.FixHistory, *fix)
		result.TotalFixes++

		// 6. 没有可行修复方案
		if fix.Action == "no_fix" {
			result.FinalOutput = fmt.Sprintf("循环 %d: AI 无法生成修复方案\n\n分析: %s\n\n输出:\n%s",
				i+1, analysis, TruncateOutput(output, 2000))
			result.DurationMs = time.Since(start).Milliseconds()
			return result, nil
		}

		// 7. 执行修复（通过返回结果让 Manager 层执行 edit_file）
		// 注意：这里只生成修复方案，实际修改由 Manager 层完成
		// 因为 auto_debug 需要和 SE 的 edit_file/write_file 协同工作
		// 返回当前状态，让上层决定是否继续
		result.FinalOutput = TruncateOutput(output, 2000)
	}

	result.DurationMs = time.Since(start).Milliseconds()
	return result, nil
}

// AnalyzeOnly 只分析错误，不执行修复（供 SE 工具调用）
func (d *AutoDebugger) AnalyzeOnly(ctx context.Context, testOutput string) (analysis string, fix *DebugFix, err error) {
	return d.analyzeAndFix(ctx, testOutput, 0)
}

// analyzeAndFix AI分析错误 + 生成修复方案
func (d *AutoDebugger) analyzeAndFix(ctx context.Context, testOutput string, iteration int) (string, *DebugFix, error) {
	prompt := fmt.Sprintf(`你是一个Go代码调试专家。以下是测试失败输出，请分析根因并给出修复方案。

测试输出（截取前3000字符）:
%s

请严格按以下JSON格式回复（不要包含markdown标记）:
{
  "root_cause": "根因分析（一句话）",
  "error_type": "错误类型（panic/assertion/compile/race/other）",
  "file_path": "需要修改的文件路径（相对路径）",
  "old_code": "需要替换的原始代码片段",
  "new_code": "修复后的代码片段",
  "action": "fix/no_fix",
  "confidence": 0.0-1.0
}

如果无法确定修复方案，将 action 设为 "no_fix"。`, TruncateOutput(testOutput, 3000))

	resp, err := d.client.Chat(ctx,
		"你是Go代码调试专家，专注分析测试失败根因并生成精确修复。",
		prompt, "zh")
	if err != nil {
		return "", nil, fmt.Errorf("AI分析调用失败: %w", err)
	}

	// 解析 AI 响应
	analysis, fix := parseAIDebugEnabledResponse(resp)
	return analysis, fix, nil
}

// ========== 错误检测辅助函数 ==========

// isTestPassed 检查测试输出是否表示通过
func isTestPassed(output string) bool {
	// Go test 通过的标志
	passPatterns := []string{
		"PASS\n",          // 标准通过
		"ok  \t",         // ok  \tpackage\t0.001s
		"PASS (",         // 某些格式
	}
	// 不能只看 PASS，FAIL 也可能包含 PASS
	if strings.Contains(output, "FAIL") {
		return false
	}
	for _, p := range passPatterns {
		if strings.Contains(output, p) {
			return true
		}
	}
	return false
}

// extractErrorSummary 提取错误摘要
func extractErrorSummary(output string) string {
	lines := strings.Split(output, "\n")

	// 优先查找 panic
	for _, line := range lines {
		if strings.Contains(line, "panic:") {
			return strings.TrimSpace(line)
		}
	}

	// 查找 FAIL 行
	for _, line := range lines {
		if strings.Contains(line, "FAIL") && !strings.Contains(line, "ok") {
			return strings.TrimSpace(line)
		}
	}

	// 查找 error 行
	for _, line := range lines {
		if strings.Contains(line, "Error") || strings.Contains(line, "error") {
			return strings.TrimSpace(line)
		}
	}

	// 返回最后 3 行
	if len(lines) > 3 {
		return strings.Join(lines[len(lines)-3:], "\n")
	}
	return TruncateOutput(output, 500)
}

// parseAIDebugEnabledResponse 解析 AI 调试分析响应
func parseAIDebugEnabledResponse(resp string) (string, *DebugFix) {
	// 清理可能的 markdown 包裹
	clean := resp
	clean = strings.TrimPrefix(clean, "```json")
	clean = strings.TrimPrefix(clean, "```")
	clean = strings.TrimSuffix(clean, "```")
	clean = strings.TrimSpace(clean)

	// 尝试 JSON 解析
	var result struct {
		RootCause   string  `json:"root_cause"`
		ErrorType   string  `json:"error_type"`
		FilePath    string  `json:"file_path"`
		OldCode     string  `json:"old_code"`
		NewCode     string  `json:"new_code"`
		Action      string  `json:"action"`
		Confidence  float64 `json:"confidence"`
	}

	if err := json.Unmarshal([]byte(clean), &result); err != nil {
		// JSON 解析失败，返回原始分析文本
		return resp, &DebugFix{
			Action:  "no_fix",
			Analysis: resp,
		}
	}

	analysis := fmt.Sprintf("根因: %s (类型: %s, 置信度: %.0f%%)",
		result.RootCause, result.ErrorType, result.Confidence*100)

	fix := &DebugFix{
		Analysis: analysis,
		FilePath: result.FilePath,
		OldCode:  result.OldCode,
		NewCode:  result.NewCode,
		Action:   result.Action,
	}

	// 提取错误片段
	if result.OldCode != "" {
		fix.ErrorSnip = TruncateOutput(result.OldCode, 200)
	}

	return analysis, fix
}

// ========== 输出处理 ==========

// TruncateOutput 截断输出到指定长度（导出供外部调用）
func TruncateOutput(output string, maxLen int) string {
	if len(output) <= maxLen {
		return output
	}
	// 保留开头和结尾
	head := maxLen / 2
	tail := maxLen / 2
	return output[:head] + "\n\n... (truncated) ...\n\n" + output[len(output)-tail:]
}

// FormatResults 格式化自动调试结果
func (r *AutoDebugResult) FormatResults() string {
	if r == nil {
		return "❌ 自动调试未执行"
	}

	var sb strings.Builder

	if r.Success {
		sb.WriteString(fmt.Sprintf("✅ 自动调试成功！共 %d 次迭代，%d 次修复\n\n", r.Iterations, r.TotalFixes))
	} else {
		sb.WriteString(fmt.Sprintf("❌ 自动调试未通过，共 %d 次迭代，%d 次修复\n\n", r.Iterations, r.TotalFixes))
	}

	if r.InitialError != "" {
		sb.WriteString(fmt.Sprintf("**首次错误**: %s\n\n", r.InitialError))
	}

	if len(r.FixHistory) > 0 {
		sb.WriteString("**修复历史**:\n\n")
		for _, fix := range r.FixHistory {
			statusIcon := "❌"
			if fix.Result == "pass" {
				statusIcon = "✅"
			}
			sb.WriteString(fmt.Sprintf("%d. %s %s\n", fix.Iteration, statusIcon, fix.Analysis))
			if fix.FilePath != "" {
				sb.WriteString(fmt.Sprintf("   文件: `%s`\n", fix.FilePath))
			}
			if fix.OldCode != "" {
				old := fix.OldCode
				if len(old) > 100 {
					old = old[:100] + "..."
				}
				sb.WriteString(fmt.Sprintf("   原代码: `%s`\n", old))
			}
			if fix.NewCode != "" {
				new := fix.NewCode
				if len(new) > 100 {
					new = new[:100] + "..."
				}
				sb.WriteString(fmt.Sprintf("   新代码: `%s`\n", new))
			}
			sb.WriteString("\n")
		}
	}

	sb.WriteString(fmt.Sprintf("\n⏱ 耗时: %dms", r.DurationMs))
	return sb.String()
}

// ErrorPattern 用于 classify error 的正则
var (
	rePanic    = regexp.MustCompile(`panic:\s*(.+)`)
	reFailTest = regexp.MustCompile(`FAIL\s+`)
	reRace     = regexp.MustCompile(`DATA RACE`)
	reCompile  = regexp.MustCompile(`(?i)(?:cannot compile|undefined:|syntax error)`)
)