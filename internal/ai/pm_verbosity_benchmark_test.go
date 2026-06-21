package ai

import (
	"strings"
	"testing"
)

// ======================================================================
// PM 废话程度 Benchmark
// 依据: docs/徒弟培养方案.md - Iteration methodology
// 目的: 确保 PM prompt 不诱导废话，任务响应简洁
// ======================================================================

// 冗余模式列表：prompt 中不该出现的诱导废话的指令
var verbosityPatterns = []struct {
	name    string
	pattern string
	why     string
}{
	{"主动建议", "主动给 1-2 个下一步建议", "让 PM 在任务完成后画蛇添足"},
	{"影响扫描", "影响范围扫描", "让 PM 做不必要的 grep 搜索"},
	{"不止步", "不止步于完成", "让 PM 觉得 [已完成] 不够，必须加废话"},
	{"多走一步", "多走一步", "同上，诱导多余输出"},
	{"用户期待", "用户期待你多走一步", "同上"},
	{"要不要我", "要不要我重新创建", "具体例子诱导追问"},
	{"要一起改", "要一起改吗", "具体例子诱导追问"},
	{"下一步建议", "下一步建议", "明确要求 PM 提建议"},
	{"随时告诉我", "随时告诉我", "对话末尾常见废话结尾"},
	{"如需其他修改", "如需其他修改", "同上"},
}

// 简洁指令列表：prompt 中应该存在的约束废话的指令
var concisenessDirectives = []struct {
	name    string
	pattern string
	why     string
}{
	{"不废话", "Concise and direct", "第一原则，要求简洁直接"},
	{"不加建议", "don't add suggestions", "明确禁止主动建议"},
	{"不解释trivial", "Don't explain trivial", "禁止解释简单代码"},
	{"不主动问", "shall I continue", "禁止追问'要不要继续'"},
}

// TestPMPrompt_NoVerbosityPatterns 验证 prompt 已删除所有诱导废话的指令
// 注意：否定式指令（如"不影响范围扫描"）包含子串"影响范围扫描"但不算诱导
func TestPMPrompt_NoVerbosityPatterns(t *testing.T) {
	prompt := formatPMPromptForTest(".")

	for _, vp := range verbosityPatterns {
		if strings.Contains(prompt, vp.pattern) {
			// 检查是否在否定语境中（"不"+"pattern"）
			idx := strings.Index(prompt, vp.pattern)
			if idx > 0 && prompt[idx-3:idx] == "不" {
				continue // 否定语境，不算诱导
			}
			t.Errorf("❌ 废话模式仍然存在: [%s] 内容=%q\n    原因: %s",
				vp.name, vp.pattern, vp.why)
		}
	}
}

// TestPMPrompt_HasConcisenessDirectives 验证 prompt 包含简洁指令
func TestPMPrompt_HasConcisenessDirectives(t *testing.T) {
	prompt := formatPMPromptForTest(".")

	for _, cd := range concisenessDirectives {
		if !strings.Contains(prompt, cd.pattern) {
			t.Errorf("❌ 缺少简洁指令: [%s] 内容=%q\n    原因: %s",
				cd.name, cd.pattern, cd.why)
		}
	}
}

// Benchmark tasks defined in 徒弟培养方案 doc
// Each task defines: input, expected concise behavior, verbosity red flags
type VerbosityBenchmarkTask struct {
	Name       string
	Level      string // L1-L5
	Input      string
	MinLines   int      // 期望响应最少行数（含工具调用）
	MaxLines   int      // 期望响应最多行数（含工具调用）
	RedFlags   []string // 响应中不该出现的废话模式
	GreenFlags []string // 响应中应该出现的简洁特征
}

func getVerbosityBenchmarkTasks() []VerbosityBenchmarkTask {
	return []VerbosityBenchmarkTask{
		{
			Name:     "文件内容追加",
			Level:    "L1",
			Input:    `在 hello.go 末尾加一行 fmt.Println("done")`,
			MinLines: 1,
			MaxLines: 6,
			RedFlags: []string{
				"我来", "让我", "首先", "先看看",
				"如果您想", "随时告诉我", "下一步建议",
				"要不要", "是否需要",
			},
			GreenFlags: []string{
				"edit_file", "read_file",
			},
		},
		{
			Name:     "跨文件改名",
			Level:    "L2",
			Input:    `把项目中的 getCwd 函数改名为 getCurrentDir`,
			MinLines: 1,
			MaxLines: 10,
			RedFlags: []string{
				"如果您想", "随时告诉我", "要我下一步",
				"已完成", "让我们",
			},
			GreenFlags: []string{
				"grep_content", "edit_file",
			},
		},
		{
			Name:     "运行 hello.go",
			Level:    "L1",
			Input:    `运行 hello.go`,
			MinLines: 1,
			MaxLines: 5,
			RedFlags: []string{
				"这是一个", "结构清晰", "最基础的",
				"如果您想", "随时告诉我",
				"package main", "import", "func main", // 不需要解释代码
				"下一步", "建议",
			},
			GreenFlags: []string{
				"exec", "go run",
			},
		},
		{
			Name:     "再次运行",
			Level:    "L1",
			Input:    `在运行一次`,
			MinLines: 1,
			MaxLines: 4,
			RedFlags: []string{
				"已完成", "已完成", "创建了",
				"如果您想", "随时告诉我",
				"write_file", // 用户没让写文件，只需运行
			},
			GreenFlags: []string{
				"exec",
			},
		},
		{
			Name:     "多次运行后问结果",
			Level:    "L2",
			Input:    `没看到运行啊`,
			MinLines: 1,
			MaxLines: 8,
			RedFlags: []string{
				"要不要清理", "要不要我", "是否需要",
				"下一步建议", "随时告诉我",
			},
			GreenFlags: []string{
				"exec",
			},
		},
	}
}

// TestVerbosityBenchmarkTasks 验证 prompt 层面支持简洁执行
// 这测试 prompt 文本是否对每个 benchmark task 有正确的指令约束
func TestVerbosityBenchmarkTasks(t *testing.T) {
	prompt := formatPMPromptForTest(".")

	for _, task := range getVerbosityBenchmarkTasks() {
		t.Run(task.Name, func(t *testing.T) {
			// 检查 prompt 中有没有简洁执行指令
			if !strings.Contains(prompt, "Concise and direct") &&
				!strings.Contains(prompt, "don't add suggestions") {
				t.Error("❌ prompt 缺少简洁执行指令，PM 会继续废话")
			}

			// 检查 prompt 中没有诱导废话的指令
			for _, rf := range task.RedFlags {
				// 检查 prompt 中是否有诱导这些废话的指令
				if strings.Contains(prompt, "主动建议") && rf == "随时告诉我" {
					t.Errorf("❌ prompt 要求主动建议 → PM 必然加'随时告诉我'")
				}
			}

			t.Logf("📋 任务 [%s] (%s): %s", task.Level, task.Name, task.Input)
			t.Logf("   期望行数: %d-%d", task.MinLines, task.MaxLines)
			t.Logf("   禁止模式: %v", task.RedFlags)
			t.Logf("   要求模式: %v", task.GreenFlags)
		})
	}
}

// formatPMPromptForTest 用 dummy 参数格式化 PMPrompt 用于测试
func formatPMPromptForTest(workDir string) string {
	// PMPrompt expects: workDir
	// Remove template placeholders for test
	prompt := PMPrompt
	// If it's a format string, just return it raw for testing
	if strings.Contains(prompt, "%s") {
		prompt = strings.ReplaceAll(prompt, "%s", workDir)
	}
	return prompt
}

// BenchmarkPMPromptVerbosoty 性能测试：测量 prompt 中废话相关的字符占比
// 数值越低越好
func BenchmarkPMPromptVerbosoty(b *testing.B) {
	prompt := formatPMPromptForTest(".")

	fluffWords := []string{
		"主动建议", "影响扫描", "不止步", "多走一步",
		"下一步建议", "随时告诉我", "如需",
		"问候", "寒暄", "节假日", "工作强度",
	}

	for i := 0; i < b.N; i++ {
		fluffCount := 0
		for _, fw := range fluffWords {
			fluffCount += strings.Count(prompt, fw)
		}
		_ = fluffCount
	}
}

// 手动评分工具：用于对 conversation.log 中的 PM 响应打分
// 使用方式：从 conversation.log 复制 PM 响应文本，调用 ScorePMResponse

// PMResponseScore 记录 PM 一条响应的评分
type PMResponseScore struct {
	TaskInput       string
	PMResponse      string
	LineCount       int
	HasUnsolicited  bool // 有无主动建议/追问
	ExplainsTrivial bool // 有无解释 trivial 代码
	HasFluff        bool // 有无废话结尾
	Score           int  // 0-5, 5=最简洁
}

// ScorePMResponse 对 PM 的一条响应打分
func ScorePMResponse(task, response string) PMResponseScore {
	lines := strings.Split(strings.TrimSpace(response), "\n")
	score := 5

	s := PMResponseScore{
		TaskInput:  task,
		PMResponse: response,
		LineCount:  len(lines),
	}

	// 检查主动建议
	suggestions := []string{
		"如果您想", "随时告诉我", "要不要", "是否需要",
		"下一步", "建议", "然后呢",
	}
	for _, sug := range suggestions {
		if strings.Contains(response, sug) {
			s.HasUnsolicited = true
			score -= 2
			break
		}
	}

	// 检查 trivial 代码解释
	trivialPatterns := []string{
		"package main", "import \"fmt\"", "func main()",
		"可执行程序", "入口函数",
	}
	matches := 0
	for _, tp := range trivialPatterns {
		if strings.Contains(response, tp) {
			matches++
		}
	}
	if matches >= 2 {
		s.ExplainsTrivial = true
		score -= 2
	}

	// 检查废话结尾
	fluffEndings := []string{
		"随时告诉我", "如需其他", "如果您想",
	}
	for _, fe := range fluffEndings {
		if strings.Contains(response, fe) {
			s.HasFluff = true
			score -= 1
			break
		}
	}

	// 行数扣分
	if len(lines) > 8 {
		score -= 1
	}
	if len(lines) > 15 {
		score -= 1
	}

	if score < 0 {
		score = 0
	}
	s.Score = score

	return s
}

// ======================================================================
// PM 反冗余指令测试
// ======================================================================

func TestPMPrompt_HasAntiRedundancy(t *testing.T) {
	prompt := formatPMPromptForTest(".")
	if !strings.Contains(prompt, "always re-run exec to verify") {
		t.Error("❌ PM prompt ANTI-LOOP 缺少反冗余指令: 'always re-run exec to verify'")
	}
	if !strings.Contains(prompt, "even if the file already exists") {
		t.Error("❌ PM prompt ANTI-LOOP 缺少反冗余指令: 'even if the file already exists'")
	}
}

// ======================================================================
// wantsIDEDelegation 测试
// ======================================================================

func TestWantsIDEDelegation_ChineseSendTo(t *testing.T) {
	cases := []string{
		"给 trae-ide 发消息 写个 hello",
		"通知 trae-ide 跑测试",
		"告诉 cursor 编译项目",
		"转发给 ide 这个消息",
		"发给 trae-ide 做",
		"传话给 windsurf 改代码",
		"发送给 vscode 看看",
	}
	for _, c := range cases {
		if !wantsIDEDelegation(c) {
			t.Errorf("❌ 应识别为IDE委托: %q", c)
		}
	}
}

func TestWantsIDEDelegation_EnglishAskIDE(t *testing.T) {
	cases := []string{
		"ask trae-ide to write hello",
		"tell ide to run tests",
		"forward to cursor please",
		"send to vscode",
		"let trae handle this",
	}
	for _, c := range cases {
		if !wantsIDEDelegation(c) {
			t.Errorf("❌ 应识别为IDE委托: %q", c)
		}
	}
}

func TestWantsIDEDelegation_NoMatch(t *testing.T) {
	cases := []string{
		"写一个 hello world",
		"编译项目",
		"运行测试",
		"这是什么",
	}
	for _, c := range cases {
		if wantsIDEDelegation(c) {
			t.Errorf("❌ 不应识别为IDE委托: %q", c)
		}
	}
}

// ======================================================================
// needsExecution 测试
// ======================================================================

func TestNeedsExecution(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{"运行 hello.go", true},
		{"run tests", true},
		{"执行编译", true},
		{"这是什么功能", false},
		{"帮我看看代码", false},
	}
	for _, c := range cases {
		got := needsExecution(c.input)
		if got != c.want {
			t.Errorf("needsExecution(%q) = %v, want %v", c.input, got, c.want)
		}
	}
}

// TestScorePMResponse_Examples 演示打分逻辑
func TestScorePMResponse_Examples(t *testing.T) {
	tests := []struct {
		name     string
		task     string
		response string
		minScore int
	}{
		{
			name:     "好响应-简洁",
			task:     "运行 hello.go",
			response: "✅ exec 'go run hello.go'\nHello, World!",
			minScore: 5,
		},
		{
			name:     "好响应-带read_file",
			task:     "运行 hello.go",
			response: "✅ read_file hello.go (73 bytes)\n✅ exec 'go run hello.go'\nHello, World!",
			minScore: 5,
		},
		{
			name: "差响应-解释代码",
			task: "运行 hello.go",
			response: "运行完成！\n\n" +
				"```\nHello, World!\n```\n\n" +
				"✅ hello.go 运行成功。这是一个最基础的 Go 程序。\n\n" +
				"- package main — 可执行程序\n" +
				"- import fmt — 导入标准输出库\n" +
				"- func main — 入口函数\n\n" +
				"下一步建议：如果你想在此基础上扩展，随时告诉我！",
			minScore: 0,
		},
		{
			name: "差响应-主动建议",
			task: "运行 hello.go",
			response: "✅ 已运行 hello.go\n\n" +
				"输出: Hello, World!\n\n" +
				"需要我继续做什么？比如修改内容、或者编译成 exe？",
			minScore: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := ScorePMResponse(tt.task, tt.response)
			t.Logf("📊 得分: %d/5", score.Score)
			t.Logf("   行数: %d", score.LineCount)
			t.Logf("   主动建议: %v", score.HasUnsolicited)
			t.Logf("   解释trivial: %v", score.ExplainsTrivial)
			t.Logf("   废话结尾: %v", score.HasFluff)
			if score.Score < tt.minScore {
				t.Errorf("期望 >=%d, 实际 %d", tt.minScore, score.Score)
			}
		})
	}
}
