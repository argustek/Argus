package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// SEPrompt SE系统提示词
const SEPrompt = `你是Argus的软件工程师(SE)，负责执行具体的编码任务。

当前工作目录: %s
所有文件路径都基于此目录。写文件时 path 字段使用相对于此目录的路径（如 "main.go"）或绝对路径。

⚠️ 最高优先级规则（必须遵守）：
- USR（用户）是最高决策者，USR的指令必须无条件执行
- 当收到USR的直接指令时（通过PM转达），立即执行
- 如果PM转达的USR要求有疑问，先执行再@PM说明情况
- 不要质疑或拒绝USR的需求，只关注如何实现

通信规则（严格遵循）：
1. **中间执行过程（写文件、编译、运行等）：不要带@标记，直接输出 actions JSON**
   - 系统会自动将你的回复路由给PM，不需要你手动@PM
   - 带了@PM反而会导致死循环！
2. **只在以下情况使用 @PM**：
   - ✅ 任务完成时："@PM ✅ 任务完成" + completed JSON
   - ❌ 遇到错误需要帮助时："@PM 编译失败，需要协助"
   - ⚠️ 需要确认危险操作时："@PM git reset --hard 危险操作请确认"
3. **一个消息只能有一个@标记**：禁止出现 "@PM @PM @PM 内容" 这样的多@格式

🚫 绝对禁止（违反会扣分）：
- ❌ 中间步骤带@PM（会导致PM重复派任务→死循环）
- ❌ 使用 "📊 任务进行中: [任务描述]" 格式
- ❌ 输出超过2行的状态描述
- ❌ 一个消息中使用多个@标记（如 @PM @PM @PM）

✅ 正确的回复格式：
- 中间步骤（写文件/编译/运行）：直接输出 actions JSON，不带任何@
  {"actions":[{"type":"write_file","path":"hello.go","content":"..."}]}
- 完成时："@PM ✅ 任务完成"
  @PM ✅ 任务完成
  {"task_status":"completed","files":["main.go"],"verified":true,"summary":"..."}
- 出错时："@PM 错误描述..."

你能@的角色：@PM（项目经理）
你不能@C（C不是对话参与者）

你的职责：
1. 执行USR要求的任务（通过PM转达，无条件执行）
2. 高质量完成编码任务
3. **自我测试（核心职责！）**：写完代码后必须亲自验证！
   - 写完代码 → exec 编译/运行 → 确认输出正确
   - 修改代码 → exec 测试 → 确认修复有效
   - 不要等PM来验证，**你自己就是第一道质量关**
4. 遇到问题时主动@PM汇报

你的工作流程（严格遵循）：
1. 分析任务，编写代码
2. 直接执行操作（写文件、执行命令等）→ 输出 actions JSON
3. **自我验证（必须！）**：
   - exec "go run xxx.go" 验证Go程序
   - exec "python xxx.py" 验证Python脚本
   - exec "npm test" 运行测试
   - exec "type xxx.txt" 查看文件内容
4. 根据验证结果决定下一步：
   - 如果成功：继续下一步或输出完成
   - 如果失败：分析错误，修复代码，重试
5. **只有确认所有操作都通过验证后**，才输出完成JSON

⚠️ 自我测试规则（绝对不能跳过！）：
- 写了Go文件 → 必须 exec "go run xxx.go" 或 "go build ./..."
- **⚠️ Go 命令格式：用 "go run 文件名.go" 运行，不是 "go 文件名.go"！**
- 写了Python文件 → 必须 exec "python xxx.py"
- 修改了配置文件 → 必须 exec "type xxx.conf" 或检查确认
- **没有exec验证的操作 = 没有真正完成，禁止输出completed！**
- **汇报时附上验证输出**："@PM ✅ 完成。验证输出: Hello World"

执行操作格式（每次回复必须包含）：
{"actions":[{"type":"write_file","path":"main.go","content":"代码内容"},{"type":"exec","command":"go run main.go"}]}

⚠️ **exec 命令必须完整可执行**：
- ✅ 正确: "go run hello.go", "npm test", "python script.py", "dir"
- ❌ 错误: "go hello.go", "hello.go", "./build" (缺少 run/build 等子命令)

可用的 action type（已更新）：
- write_file: 写文件（整体创建新文件或完全覆写），需要 path 和 content
- **edit_file**: 精确编辑文件（推荐用于修改现有代码）⭐ [P0新增]
  - path: 文件路径（相对路径）
  - old_str: 要替换的文本（必须唯一匹配，建议至少20个字符）
  - new_str: 替换为的文本
  - 示例: {"type":"edit_file","path":"main.go","old_str":"func login() {","new_str":"func login(user User) *User {"}
  - ✅ 优势：最小化修改范围，自动生成diff，降低误改风险
- **search_files**: 全局搜索文件内容 ⭐ [P1新增]
  - pattern: 搜索关键词或正则表达式
  - file_pattern: 可选，文件过滤（如 "*.go", "*.py"）
  - is_regex: 可选，是否使用正则（默认 false）
  - path: 可选，搜索子目录（默认整个工作目录）
  - case_insensitive: 可选，是否忽略大小写
  - 示例: {"type":"search_files","pattern":"func login","file_pattern":"*.go"}
  - 正则示例: {"type":"search_files","pattern":"func \\w+\\(.*\\)","is_regex":true,"file_pattern":"*.go"}
  - ✅ 用途：跨文件查找引用、定位函数定义、搜索变量使用
  - 返回：匹配的文件列表、行号、列号、上下文代码
- **git_operation**: Git 版本控制操作 ⭐ [P1新增]
  - git_action: 操作类型（status/diff/commit/push/pull/log/branch/show）
  - git_message: 可选，提交信息（commit 时必填）
  - git_args: 可选，额外参数（如 push 时的 "origin main"）
  - 示例: {"type":"git_operation","git_action":"status"}
  - 提交示例: {"type":"git_operation","git_action":"commit","git_message":"feat: add login feature"}
  - 推送示例: {"type":"git_operation","git_action":"push","git_args":["origin","main"]}
  - Diff示例: {"type":"git_operation","git_action":"diff","git_args":["--stat"]}
  - 日志示例: {"type":"git_operation","git_action":"log","git_args":["-5"]}
  - ⚠️ commit 前先 git status 确认，push 前先确认远程地址
- **run_tests**: 运行测试 ⭐ [P1新增]
  - test_pattern: 可选，测试匹配模式（默认 "./..." 全量测试）
  - test_coverage: 可选，是否生成覆盖率报告
  - test_verbose: 可选，是否详细输出
  - 示例: {"type":"run_tests","test_pattern":"./internal/executor/","test_verbose":true}
  - 覆盖率示例: {"type":"run_tests","test_coverage":true}
  - ✅ 用途：修改代码后自动验证、CI/CD 集成
  - 返回：通过/失败数、各用例详情、耗时、覆盖率
- read_file: 读文件，需要 path。返回文件内容，用于审核代码或查看结果
- exec: 执行命令，需要 command。会在工作目录下执行
- check_env: 检查环境，需要 tool（如 go, python）

⚠️ 编辑规则（重要）：
- **优先使用 edit_file**（而不是 write_file）修改现有代码，更安全！
- **修改前先用 search_files 查找所有引用点**，确保不遗漏
- old_str 必须足够长以确保唯一匹配（至少 20 个字符）
- 一次只修改一处（不要在一个 edit_file 中改多处）
- 修改后立即 exec 验证（go build / go test 等）
- 如果文件不存在或 old_str 未找到，系统会报错提示

重要：
- **尽量一步到位**：一个任务尽量在一次回复中完成所有actions（写文件+编译+运行），不要拆成多轮
- 执行 go run 或运行程序时，直接输出命令如 "go run hello.go"
- 工作目录内的操作直接执行，不需要经过C
- 目录外操作（如安装软件）必须通过 @PM 让PM调用C执行
- 危险操作（如git reset --hard）必须先 @PM 询问确认

项目初始化规则（必须遵守）：
1. 先用 exec "dir" 或 check_env 检查当前目录状态
2. 如果 go.mod 已存在，不要执行 "go mod init"，直接写代码即可
3. 只有在确定是新项目时才初始化

完成时输出（必须同时满足以下条件才能输出）：
@PM ✅ 任务完成
{"task_status":"completed","files":["文件1","文件2"...],"verified":true,"summary":"简短描述做了什么"}

⚠️ 完成的严格条件（缺一不可）：
1. **必须执行过验证**：代码必须通过 go run/go build/python 等命令验证成功
2. **files 必须列出实际创建/修改的文件**
3. **verified 必须为 true**（只有验证通过才能说完成）
4. **summary 简洁说明**：一句话概括实现了什么（不要复制任务描述）

❌ 错误示例（禁止）：
- summary: "已完成创建main.go的任务" ← 这是复制任务描述
- files: [] ← 不能为空（除非真的没创建文件）
- verified: false ← 未验证不能说完成

✅ 正确示例：
@PM ✅ 任务完成：已创建Hello World程序
{"task_status":"completed","files":["main.go"],"verified":true,"summary":"使用fmt.Println实现Hello World输出，通过go run验证"}

⚠️ 发送前检查（每次回复前必须确认）：
□ 中间步骤：是否没有带@PM？（中间步骤带@PM会导致死循环）
□ 完成时：是否带了 @PM + completed JSON？
□ 是否只有一个 @PM 标记？（不能有多个）
□ 如果是完成状态，是否附带了验证结果？`

// SEProcessor SE处理器
type SEProcessor struct {
	client        *Client
	workDir       string
	systemPrompt  string
	history       []Message
	ReplyLanguage string
	ctx           context.Context
	envMemory     string
}

// NewSEProcessor 创建SE处理器
func NewSEProcessor(client *Client, workDir string) *SEProcessor {
	return &SEProcessor{
		client:       client,
		workDir:      workDir,
		systemPrompt: fmt.Sprintf(SEPrompt, workDir),
		history:      []Message{},
	}
}

// SetEnvMemory 设置环境记忆（动态注入到Prompt）
func (s *SEProcessor) SetEnvMemory(summary string) {
	s.envMemory = summary
}

func (s *SEProcessor) getSystemPrompt() string {
	if s.envMemory != "" {
		return s.systemPrompt + "\n\n" + s.envMemory
	}
	return s.systemPrompt
}

// SetContext 设置上下文（用于取消AI调用）
func (s *SEProcessor) SetContext(ctx context.Context) {
	s.ctx = ctx
}

// getCtx 获取上下文，nil 时返回 Background
func (s *SEProcessor) getCtx() context.Context {
	if s.ctx == nil {
		return context.Background()
	}
	return s.ctx
}

// ProcessTask 处理任务
func (s *SEProcessor) ProcessTask(taskDesc string) (*SEResponse, error) {
	fmt.Printf("[SE Debug] Starting task: %s\n", taskDesc)
	response, err := s.client.ChatWithHistory(s.getCtx(), s.getSystemPrompt(), s.history, taskDesc, s.ReplyLanguage)
	if err != nil {
		fmt.Printf("[SE Debug] AI call failed: %v\n", err)
		return nil, err
	}
	fmt.Printf("[SE Debug] AI response received, length: %d\n", len(response))

	// 添加到历史
	s.history = append(s.history, Message{Role: "user", Content: taskDesc})
	s.history = append(s.history, Message{Role: "assistant", Content: response})

	// 限制历史长度（保留最近10轮）
	if len(s.history) > 20 {
		s.history = s.history[len(s.history)-20:]
	}

	// 提取actions
	actions := s.extractActions(response)

	// 提取完成标记
	completed := s.extractCompletion(response)

	// 检查是否需要PM帮助（包含特定关键词）
	needHelp := s.checkNeedHelp(response)

	return &SEResponse{
		Content:   response,
		Actions:   actions,
		Completed: completed,
		NeedHelp:  needHelp,
	}, nil
}

// ProcessTaskStream 流式处理任务，每收到文本片段调用 onChunk
func (s *SEProcessor) ProcessTaskStream(taskDesc string, onChunk func(delta string)) (*SEResponse, error) {
	fmt.Printf("[SE Stream] Starting task: %s\n", taskDesc)
	response, err := s.client.ChatStream(s.getCtx(), s.getSystemPrompt(), s.history, taskDesc, s.ReplyLanguage, onChunk)
	if err != nil {
		fmt.Printf("[SE Stream] AI call failed: %v\n", err)
		return nil, err
	}
	fmt.Printf("[SE Stream] AI response completed, length: %d\n", len(response))

	s.history = append(s.history, Message{Role: "user", Content: taskDesc})
	s.history = append(s.history, Message{Role: "assistant", Content: response})

	if len(s.history) > 20 {
		s.history = s.history[len(s.history)-20:]
	}

	actions := s.extractActions(response)
	completed := s.extractCompletion(response)
	needHelp := s.checkNeedHelp(response)

	// 🆕 [DIAG-20260529] 诊断日志：追踪completed识别情况
	if completed != nil {
		fmt.Printf("[SE Debug] ✅ extractCompletion SUCCESS! status=%q notes=%q\n",
			completed.Status, completed.TechnicalNotes)
	} else if strings.Contains(response, "completed") || strings.Contains(response, "task_status") {
		fmt.Printf("[SE Debug] ⚠️ extractCompletion FAILED but response contains 'completed' keyword!\n")
		fmt.Printf("[SE Debug] Response preview (first_300): %s\n", truncate(response, 300))
	}

	return &SEResponse{
		Content:   response,
		Actions:   actions,
		Completed: completed,
		NeedHelp:  needHelp,
	}, nil
}

// AddResult 添加执行结果到历史
func (s *SEProcessor) AddResult(result string) {
	s.history = append(s.history, Message{Role: "user", Content: result})
}

// SEResponse SE响应
type SEResponse struct {
	Content   string
	Actions   []SEAction
	Completed *SECompletion
	NeedHelp  bool // 是否需要PM帮助
}

// SEAction SE动作
type SEAction struct {
	Type    string `json:"type"`
	Path    string `json:"path,omitempty"`
	Content string `json:"content,omitempty"`
	Command string `json:"command,omitempty"`
	Tool    string `json:"tool,omitempty"`

	// [P0] 精确编辑支持（用于 edit_file）
	OldStr  string `json:"old_str,omitempty"`  // 要搜索的文本（必须唯一匹配）
	NewStr  string `json:"new_str,omitempty"`  // 替换为的文本

	// [P1] 全局搜索支持（用于 search_files）
	Pattern        string `json:"pattern,omitempty"`         // 搜索关键词或正则
	FilePattern    string `json:"file_pattern,omitempty"`     // 文件过滤（如 *.go）
	IsRegex        bool   `json:"is_regex,omitempty"`        // 是否正则
	CaseInsensitive bool   `json:"case_insensitive,omitempty"` // 忽略大小写

	// [P1] Git 操作支持（用于 git_operation）
	GitAction string   `json:"git_action,omitempty"` // status/diff/commit/push/pull/log/branch/show
	GitMessage string  `json:"git_message,omitempty"` // commit message
	GitArgs   []string `json:"git_args,omitempty"`    // 额外参数

	// [P1] 测试运行支持（用于 run_tests）
	TestPattern string `json:"test_pattern,omitempty"` // 测试匹配模式
	TestCoverage bool  `json:"test_coverage,omitempty"` // 是否生成覆盖率
	TestVerbose  bool  `json:"test_verbose,omitempty"`  // 详细输出
}

// SECompletion SE完成信息
type SECompletion struct {
	TechnicalNotes string `json:"technical_notes"`
	ChangelogDraft string `json:"changelog_draft"`
	Status         string `json:"status"`
}

// extractActions 提取actions（多层容错）
func (s *SEProcessor) extractActions(response string) []SEAction {
	// 策略1: 标准JSON解析
	actions := s.extractActionsFromJSON(response)
	if len(actions) > 0 {
		return actions
	}

	// 策略2: 从markdown code block提取Python等代码，构造write_file action
	actions = s.extractActionsFromCodeBlocks(response)
	if len(actions) > 0 {
		if f, err := os.OpenFile(filepath.Join(os.TempDir(), "argus_se_debug.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err == nil {
			f.WriteString(fmt.Sprintf("[%s] Fallback to code block extraction, found %d actions\n", time.Now().Format("15:04:05"), len(actions)))
			f.Close()
		}
		return actions
	}

	// ⚠️ 全部解析失败 — 记录原始响应以便调试
	fmt.Printf("[SE Debug] ⚠️ extractActions FAILED! all strategies returned nil | raw_len=%d first_300=%q\n",
		len(response), truncate(response, 300))
	if f, err := os.OpenFile(filepath.Join(os.TempDir(), "argus_se_debug.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err == nil {
		f.WriteString(fmt.Sprintf("[%s] EXTRACT-ACTIONS-FAILED raw_len=%d first_500=%q\n", time.Now().Format("15:04:05"), len(response), truncate(response, 500)))
		f.Close()
	}

	return nil
}

// extractActionsFromJSON 标准JSON解析（带容错）
func (s *SEProcessor) extractActionsFromJSON(response string) []SEAction {
	start := strings.Index(response, "{\"actions\"")
	if start == -1 {
		return nil
	}

	braceCount := 0
	end := start
	for i := start; i < len(response); i++ {
		if response[i] == '{' {
			braceCount++
		} else if response[i] == '}' {
			braceCount--
			if braceCount == 0 {
				end = i + 1
				break
			}
		}
	}

	jsonStr := response[start:end]

	debugLog := fmt.Sprintf("[%s] extractActions JSON length=%d, first_500=%s\n", time.Now().Format("15:04:05"), len(jsonStr), truncate(jsonStr, 500))
	if f, err := os.OpenFile(filepath.Join(os.TempDir(), "argus_se_debug.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err == nil {
		f.WriteString(debugLog)
		f.Close()
	}

	var result struct {
		Actions []SEAction `json:"actions"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &result); err == nil {
		result.Actions = s.fixActionTypes(result.Actions)
		for i, a := range result.Actions {
			if a.Type == "write_file" {
				fmt.Printf("[SE Debug] write_file action #%d: path=%s, content_len=%d, first_200=%q\n", i, a.Path, len(a.Content), truncate(a.Content, 200))
				if f, err := os.OpenFile(filepath.Join(os.TempDir(), "argus_se_debug.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err == nil {
					f.WriteString(fmt.Sprintf("[%s] write_file #%d: path=%s content_len=%d first_300=%q\n", time.Now().Format("15:04:05"), i, a.Path, len(a.Content), truncate(a.Content, 300)))
					f.Close()
				}
			}
		}
		return result.Actions
	}

	// JSON解析失败，尝试修复：将content中的真实换行转为\n转义
	fixed := s.fixJSONNewlines(jsonStr)

	// 🆕 尝试修复AI常见JSON错误（缺少冒号等）
	fixed = s.fixMalformedJSON(fixed)

	if err := json.Unmarshal([]byte(fixed), &result); err == nil {
		fmt.Printf("[SE Debug] extractActions: 修复换行后解析成功\n")
		result.Actions = s.fixActionTypes(result.Actions)
		if f, err := os.OpenFile(filepath.Join(os.TempDir(), "argus_se_debug.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err == nil {
			f.WriteString(fmt.Sprintf("[%s] fixJSONNewlines SUCCESS\n", time.Now().Format("15:04:05")))
			f.Close()
		}
		for i, a := range result.Actions {
			if a.Type == "write_file" {
				fmt.Printf("[SE Debug] write_file action #%d: path=%s, content_len=%d, first_200=%q\n", i, a.Path, len(a.Content), truncate(a.Content, 200))
			}
		}
		return result.Actions
	}

	fmt.Printf("[SE Debug] extractActions JSON parse failed even after fix, raw snippet: %q\n", truncate(jsonStr, 300))
	return nil
}

// fixActionTypes 修复AI生成的actions中type字段为空或错误的问题
func (s *SEProcessor) fixActionTypes(actions []SEAction) []SEAction {
	for i := range actions {
		a := &actions[i]
		if a.Type == "" || a.Type == "_" || strings.HasPrefix(a.Type, "_") {
			if a.Path != "" && a.Content != "" {
				a.Type = "write_file"
				fmt.Printf("[SE Debug] fixActionTypes: action #%d inferred as write_file (path=%s)\n", i, a.Path)
			} else if a.Command != "" {
				a.Type = "exec"
				fmt.Printf("[SE Debug] fixActionTypes: action #%d inferred as exec (command=%s)\n", i, truncate(a.Command, 100))
			} else if a.Tool != "" {
				a.Type = "check_env"
				fmt.Printf("[SE Debug] fixActionTypes: action #%d inferred as check_env (tool=%s)\n", i, a.Tool)
			} else if a.OldStr != "" && a.NewStr != "" {
				a.Type = "edit_file"
				fmt.Printf("[SE Debug] fixActionTypes: action #%d inferred as edit_file\n", i)
			}
		}
	}
	return actions
}

// fixMalformedJSON 修复AI生成的JSON中常见格式错误：缺少冒号, 键值粘连, 缺失引号
func (s *SEProcessor) fixMalformedJSON(jsonStr string) string {
	totalFixes := 0

	// 1. 修复 "actionstype:" → "actions":[{"type":
	re := regexp.MustCompile(`^\{\s*"actionstype"\s*:`)
	if re.MatchString(jsonStr) {
		jsonStr = re.ReplaceAllString(jsonStr, `{"actions":[{"type":`)
		totalFixes++
		fmt.Println("[SE Debug] fixMalformedJSON: 修复 actionstype -> actions:[{type")
	}

	// 2. 修复 "pathhello.go" → "path":"hello.go" (键值粘连)
	pathRe := regexp.MustCompile(`"path"([^:\s])`)
	if pathRe.MatchString(jsonStr) {
		jsonStr = pathRe.ReplaceAllString(jsonStr, `"path":"$1`)
		totalFixes++
		fmt.Println("[SE Debug] fixMalformedJSON: 修复 path粘连")
	}

	// 3. 修复 "typeexec" / "typewrite_file" → "type":"..."
	typeRe := regexp.MustCompile(`"type"([a-z_])`)
	if typeRe.MatchString(jsonStr) {
		jsonStr = typeRe.ReplaceAllString(jsonStr, `"type":"$1`)
		totalFixes++
		fmt.Println("[SE Debug] fixMalformedJSON: 修复 type粘连")
	}

	// 3.1 🆕 [FIX-20260529] 修复空type字段 {"" :"exec"} → {"type":"exec"}
	emptyTypeRe := regexp.MustCompile(`{""\s*:\s*"([a-z_]+)"`)
	if emptyTypeRe.MatchString(jsonStr) {
		jsonStr = emptyTypeRe.ReplaceAllString(jsonStr, `{"type":"$1"`)
		totalFixes++
		fmt.Println("[SE Debug] fixMalformedJSON: 修复空type字段")
	}

	// 3.2 🆕 [FIX-20260529] 修复完全缺失type字段的action对象
	missingTypeRe := regexp.MustCompile(`\{\s*"(path|command|content)"`)
	if missingTypeRe.MatchString(jsonStr) {
		jsonStr = missingTypeRe.ReplaceAllString(jsonStr, `{"type":"$1","$1`)
		totalFixes++
		fmt.Println("[SE Debug] fixMalformedJSON: 补全缺失的type字段")
	}

	// 4. 修复 "commandgo" → "command":"go"
	cmdRe := regexp.MustCompile(`"command"([^:\s"])`)
	if cmdRe.MatchString(jsonStr) {
		jsonStr = cmdRe.ReplaceAllString(jsonStr, `"command":"$1`)
		totalFixes++
		fmt.Println("[SE Debug] fixMalformedJSON: 修复 command粘连")
	}

	// 5. 修复 "content"粘连
	contentRe := regexp.MustCompile(`"content"([^:\s])`)
	if contentRe.MatchString(jsonStr) {
		jsonStr = contentRe.ReplaceAllString(jsonStr, `"content":"$1`)
		totalFixes++
		fmt.Println("[SE Debug] fixMalformedJSON: 修复 content粘连")
	}

	// 6. 🆕 补全缺失的尾部 ]} (常见: AI只输出了一半)
	trimmed := strings.TrimSpace(jsonStr)
	if !strings.HasSuffix(trimmed, "]}") && strings.HasSuffix(trimmed, "}") {
		// 单action缺少外层 ]
		// 在最后的 } 之后还有内容就不是单action，不处理
	}
	if strings.HasSuffix(trimmed, `"}`) {
		// 结尾像 `{"actions":[...}` 缺 ]
		jsonStr = trimmed + "]}"
		totalFixes++
		fmt.Println("[SE Debug] fixMalformedJSON: 补全尾部 ]}")
	} else if !strings.HasSuffix(trimmed, "]}") && strings.Count(trimmed, "{") > strings.Count(trimmed, "}") {
		// 花括号不配对，补上
		missing := strings.Count(trimmed, "{") - strings.Count(trimmed, "}")
		jsonStr = trimmed + strings.Repeat("}", missing)
		// 也补上外层 ]
		if !strings.Contains(jsonStr[len(jsonStr)-missing*2:], "]") {
			jsonStr = strings.TrimSuffix(jsonStr, strings.Repeat("}", missing)) + "]" + strings.Repeat("}", missing)
		}
		totalFixes++
		fmt.Printf("[SE Debug] fixMalformedJSON: 补全缺失的花括号 (missing=%d)\n", missing)
	}

	// 6.5 🆕 [FIX-20260529] 移除Markdown代码块标记 (AI输出 ```json ... ```)
	markdownRe := regexp.MustCompile("```(?:json)?\\s*\n?")
	if markdownRe.MatchString(jsonStr) {
		jsonStr = markdownRe.ReplaceAllString(jsonStr, "")
		totalFixes++
		fmt.Println("[SE Debug] fixMalformedJSON: 移除Markdown代码块标记")
	}
	// 移除结尾的 ```
	jsonStr = strings.TrimSuffix(jsonStr, "```")
	jsonStr = strings.TrimSpace(jsonStr)

	// 6.6 🆕 [FIX-20260529] 修复缺少开头的 {" (常见: AI直接输出 actions":[...)
	if !strings.HasPrefix(jsonStr, "{") && (strings.HasPrefix(jsonStr, `"actions"`) || strings.Contains(jsonStr, `"actions":[`)) {
		jsonStr = "{" + jsonStr
		totalFixes++
		fmt.Println("[SE Debug] fixMalformedJSON: 补全缺失的开头 {")
	}

	// 6.7 🆕 [FIX-20260529] 修复 "type":"execcommand" 粘连 (更精确的command检测)
	execCmdRe := regexp.MustCompile(`"type"\s*:\s*"exec([a-z]+)command"`)
	if execCmdRe.MatchString(jsonStr) {
		jsonStr = execCmdRe.ReplaceAllString(jsonStr, `"type":"exec","command":"$1`)
		totalFixes++
		fmt.Println("[SE Debug] fixMalformedJSON: 修复 exec+command粘连")
	}

	// 6.8 🆕 [FIX-20260529] 修复 "actions": "type": 格式错误 (缺少数组括号)
	actionsFormatRe := regexp.MustCompile(`"actions"\s*:\s*"type"`)
	if actionsFormatRe.MatchString(jsonStr) {
		jsonStr = actionsFormatRe.ReplaceAllString(jsonStr, `"actions":[{"type"`)
		totalFixes++
		fmt.Println("[SE Debug] fixMalformedJSON: 修复actions数组格式")
	}

	// 7. 🆕 [FIX-20260529] 修复AI生成的Go代码常见语法错误
	// 7.1 修复 "fmt.PrintlnWorld" → fmt.Println("World") (缺少括号和引号)
	printRe := regexp.MustCompile(`fmt\.Println([a-zA-Z]\w*)`)
	if printRe.MatchString(jsonStr) {
		jsonStr = printRe.ReplaceAllString(jsonStr, `fmt.Println("$1")`)
		totalFixes++
		fmt.Println("[SE Debug] fixMalformedJSON: 修复 Println缺少括号")
	}

	// 7.2 修复 "go run hello" (缺.go) → "go run hello.go" (在exec command中)
	execGoRe := regexp.MustCompile(`"command"\s*:\s*"go\s+run\s+(\w+)(?!\.go)"`)
	if execGoRe.MatchString(jsonStr) {
		jsonStr = execGoRe.ReplaceAllString(jsonStr, `"command":"go run $1.go"`)
		totalFixes++
		fmt.Println("[SE Debug] fixMalformedJSON: 修复 go run 缺少.go后缀")
	}

	// 7.3 修复 "go hello.go" (缺run) → "go run hello.go"
	goRunRe := regexp.MustCompile(`"command"\s*:\s*"go\s+([^r]\w*\.go)"`)
	if goRunRe.MatchString(jsonStr) {
		jsonStr = goRunRe.ReplaceAllString(jsonStr, `"command":"go run $1"`)
		totalFixes++
		fmt.Println("[SE Debug] fixMalformedJSON: 修复 go 命令缺少run")
	}

	// 8. 🆕 [FIX-20260529] 修复content中的常见Go错误
	// 8.1 修复 import "\"fmt\"" → import "fmt"
	importRe := regexp.MustCompile(`import\s+"\\?"fmt"\\?"`)
	if importRe.MatchString(jsonStr) {
		jsonStr = importRe.ReplaceAllString(jsonStr, `import "fmt"`)
		totalFixes++
		fmt.Println("[SE Debug] fixMalformedJSON: 修复 import 语句格式错误")
	}

	fmt.Printf("[SE Debug] fixMalformedJSON: %d fixes applied | result first_200=%q\n", totalFixes, truncate(jsonStr, 200))

	// 9. 🆕 [FIX-20260529] 终极验证：尝试解析修复后的JSON
	if totalFixes > 0 {
		var testStruct struct {
			Actions []SEAction `json:"actions"`
		}
		if err := json.Unmarshal([]byte(jsonStr), &testStruct); err != nil {
			fmt.Printf("[SE Debug] ⚠️ 修复后仍无法解析: %v | 尝试激进重组...\n", err)
			jsonStr = s.aggressiveJSONReconstruct(jsonStr)
			totalFixes++
		} else if len(testStruct.Actions) > 0 {
			fmt.Printf("[SE Debug] ✅ 修复成功! 解析出 %d 个actions\n", len(testStruct.Actions))
		}
	}

	return jsonStr
}

// aggressiveJSONReconstruct: Rebuild JSON structure when normal fixes fail
func (s *SEProcessor) aggressiveJSONReconstruct(jsonStr string) string {
	fmt.Println("[SE Debug] aggressiveJSONReconstruct: Starting aggressive restructure...")

	re := regexp.MustCompile(`\{[^{}]*"([^"]+)"\s*:\s*"([^"]*)"[^{}]*\}`)
	matches := re.FindAllString(jsonStr, -1)

	if len(matches) < 2 {
		fmt.Printf("[SE Debug] aggressiveJSONReconstruct: Only %d objects found, abort\n", len(matches))
		return jsonStr
	}

	fmt.Printf("[SE Debug] aggressiveJSONReconstruct: Extracted %d object blocks\n", len(matches))

	var actions []string
	for _, match := range matches {
		if !strings.Contains(match, `"type"`) {
			if strings.Contains(match, `"command"`) {
				match = `{"type":"exec",` + strings.TrimPrefix(match, "{")
			} else if strings.Contains(match, `"path"`) {
				match = `{"type":"write_file",` + strings.TrimPrefix(match, "{")
			}
		}
		actions = append(actions, match)
	}

	reconstructed := `{"actions":[` + strings.Join(actions, ",") + `]}`
	fmt.Printf("[SE Debug] aggressiveJSONReconstruct: Done | len=%d\n", len(reconstructed))
	return reconstructed
}

// fixJSONNewlines 修复JSON中content字段的真实换行为\n转义
func (s *SEProcessor) fixJSONNewlines(jsonStr string) string {
	var b strings.Builder
	inString := false
	escapeNext := false

	for i := 0; i < len(jsonStr); i++ {
		ch := jsonStr[i]

		if escapeNext {
			b.WriteByte(ch)
			escapeNext = false
			continue
		}

		if ch == '\\' {
			b.WriteByte(ch)
			escapeNext = true
			continue
		}

		if ch == '"' {
			inString = !inString
			b.WriteByte(ch)
			continue
		}

		if inString && ch == '\n' {
			b.WriteString("\\n")
			continue
		}

		if inString && ch == '\r' {
			continue
		}

		if inString && ch == '\t' {
			b.WriteString("\\t")
			continue
		}

		b.WriteByte(ch)
	}

	return b.String()
}

// extractActionsFromCodeBlocks 从markdown code block提取代码
func (s *SEProcessor) extractActionsFromCodeBlocks(response string) []SEAction {
	var actions []SEAction

	re := regexp.MustCompile("(?s)```(\\w*)\\s*\\n(.*?)```")
	matches := re.FindAllStringSubmatch(response, -1)

	for i, match := range matches {
		lang := strings.TrimSpace(match[1])
		code := match[2]

		var filename string
		switch lang {
		case "python", "py":
			filename = "main.py"
		case "go":
			filename = "main.go"
		case "javascript", "js":
			filename = "main.js"
		case "typescript", "ts":
			filename = "main.ts"
		case "java":
			filename = "Main.java"
		case "rust":
			filename = "main.rs"
		case "c", "cpp", "c++":
			filename = "main.c"
		default:
			filename = fmt.Sprintf("file%d.txt", i+1)
		}

		if len(code) > 0 {
			actions = append(actions, SEAction{
				Type:    "write_file",
				Path:    filename,
				Content: code,
			})
			fmt.Printf("[SE Debug] 从code block提取: lang=%s, path=%s, content_len=%d\n", lang, filename, len(code))
		}
	}

	return actions
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// extractCompletion 提取完成标记（增强版：支持GLM-5等模型的格式错误）
func (s *SEProcessor) extractCompletion(response string) *SECompletion {
	// 策略1: 标准解析 - 查找独立行的JSON
	lines := strings.Split(response, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if strings.HasPrefix(line, "{") && strings.Contains(line, "status") && strings.Contains(line, "completed") {
			var completion SECompletion
			if err := json.Unmarshal([]byte(line), &completion); err == nil {
				return &completion
			}
		}
	}

	// 策略2: 增强搜索 - 查找响应中任何位置的completed标记（修复GLM-5格式错误）
	if strings.Contains(response, "completed") || strings.Contains(response, "task_status") {
		re := regexp.MustCompile(`(?i)\{[^{}]*"task_status"\s*:\s*"completed"[^{}]*\}`)
		matches := re.FindStringSubmatch(response)
		if len(matches) > 0 {
			var completion SECompletion
			if err := json.Unmarshal([]byte(matches[0]), &completion); err == nil {
				fmt.Printf("[SE Debug] ✅ extractCompletion(策略2): 找到内嵌completed JSON\n")
				return &completion
			}
		}

		// 策略3: 激进修复 - 尝试修复常见格式错误后重新解析
		fixed := s.fixCompletedJSON(response)
		if fixed != "" {
			var completion SECompletion
			if err := json.Unmarshal([]byte(fixed), &completion); err == nil {
				fmt.Printf("[SE Debug] ✅ extractCompletion(策略3): 修复后的completed JSON解析成功\n")
				return &completion
			}
		}
	}

	return nil
}

// fixCompletedJSON 修复AI生成的completed JSON常见格式错误
func (s *SEProcessor) fixCompletedJSON(response string) string {
	start := strings.Index(response, "task_status")
	if start == -1 {
		return ""
	}

	// 向前查找 { 或 @PM 位置
	jsonStart := strings.LastIndex(response[:start], "{")
	if jsonStart == -1 {
		jsonStart = strings.LastIndex(response[:start], "@PM")
		if jsonStart != -1 {
			jsonStart += 4 // 跳过 "@PM "
		} else {
			jsonStart = start
		}
	}

	// 向后查找 } 结束
	jsonEnd := strings.Index(response[start:], "}")
	if jsonEnd == -1 {
		return ""
	}
	jsonEnd = start + jsonEnd + 1

	candidate := response[jsonStart:jsonEnd]

	// 修复常见错误
	candidate = strings.ReplaceAll(candidate, `\"`, `"`)  // 移除多余转义
	candidate = strings.ReplaceAll(candidate, `completed.go`, `"completed"`)  // 修复粘连
	if !strings.HasPrefix(candidate, "{") {
		candidate = "{" + candidate
	}
	if !strings.HasSuffix(candidate, "}") {
		candidate = candidate + "}"
	}

	fmt.Printf("[SE Debug] fixCompletedJSON: fixed=%q\n", truncate(candidate, 200))
	return candidate
}

// checkNeedHelp 检查是否需要PM帮助
func (s *SEProcessor) checkNeedHelp(response string) bool {
	// 如果包含这些关键词，说明需要PM帮助
	helpKeywords := []string{
		"问PM",
		"需要确认",
		"需要PM确认",
		"请PM确认",
		"不确定",
		"需要帮助",
		"@PM",
	}
	lowerResponse := strings.ToLower(response)
	for _, keyword := range helpKeywords {
		if strings.Contains(lowerResponse, strings.ToLower(keyword)) {
			return true
		}
	}
	return false
}

var semanticCompleteKeywords = []string{
	"任务完成", "已完成", "已执行完毕", "执行完毕", "完成执行", "请审核", "请计审核", "审核结果",
	"task completed", "done", "finished", "verification complete",
}

func (s *SEProcessor) CheckSemanticComplete(response string) bool {
	lower := strings.ToLower(response)
	for _, kw := range semanticCompleteKeywords {
		if strings.Contains(lower, strings.ToLower(kw)) {
			return true
		}
	}
	return false
}

// ResetHistory 重置历史
func (s *SEProcessor) ResetHistory() {
	s.history = []Message{}
}
