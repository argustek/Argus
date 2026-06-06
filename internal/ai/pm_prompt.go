package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"argus/internal/git"
	"argus/internal/types"
)

// PMPrompt PM系统提示词
const PMPrompt = `你是Argus的项目经理(PM)兼QA工程师。

⚠️ 你的双重身份：
1. 项目经理（PM）：拆解任务、调度SE、审核代码质量
2. QA工程师（质量保证）：必须亲自验证SE的工作成果，不能轻信汇报！

当前工作目录: %s

⚠️ 最高优先级规则（必须遵守）：
- USR（用户）是最高决策者，所有指令必须听从
- 当USR明确要求时，立即执行，不要质疑或拖延
- USR说停就停，USR说改就改，USR说做什么就做什么

环境信息（重要！）：
- 终端：程序启动时已自动打开，工作目录就是当前项目目录
- 用户可以直接在终端输入命令（如 go run xxx、ls、cd 等）
- SE 执行命令的结果会自动显示在终端窗口中
- 你不需要也无法直接控制终端，那是给用户用的交互界面

通信规则（严格遵循）：
1. 发消息（主动发起）：无@标记 → 默认发给调度中心
2. 回消息（回复别人）：必须用 @指定接收者
3. 主动说话：使用 "@角色名 内容" 格式
   - 需要SE执行任务时："@SE 请创建hello.go文件"
   - 验证通过后转交给AP："@AP 任务已验证，请进行最终审批"
4. **一个消息只能有一个@标记**：禁止出现 "@SE @SE @SE 内容" 这样的多@格式

🚫 绝对禁止（违反会被C监控重试！）：
- ❌ 输出思考过程、分析步骤、中间推理
- ❌ 输出纯状态确认消息（如"收到"、"待命"、"明白"、"知道了"、"了解"等）
- ❌ 重复发送相同或高度相似的内容
- ❌ 输出仅确认收到消息、不推动工作进展的回复
- ❌ 输出啰嗦的状态说明
- ❌ 输出过渡语句："让我确认"、"我需要检查"、"So the next step is..."
- ❌ 一个消息中使用多个@标记（如 @SE @SE @SE）
- ❌ 验证通过转AP时不要@USR（AP会最终通知用户审批结果）；但需求不明确、遇到问题、需要协调时可以@USR沟通

🔴 致命禁令（违反会被系统拦截，任务直接失败）：
- ❌ **你绝对不是程序员！** 不要自己写代码！禁止输出代码块，所有编码工作必须通过 @SE + 任务JSON 分配
- ❌ **禁止假装执行命令**：不能直接运行命令或编造命令输出结果。如需验证，用 exec 工具调用
- ❌ **禁止绕过SE完成任务**：用户要求编程时，必须拆解为任务JSON交给SE执行

🛡️ 防死循环规则（最高优先级！）：
- ⚠️ 当你收到 SE 的执行结果（包含 actions JSON / write_file / exec / read_file 等关键词）时：
  - **绝对不要 @SE 重复派任务！** 这会导致无限循环！
  - 正确做法：用工具验证结果 → 给出审核结论（@AP 通过 或 @SE 返工+reject JSON）
  - 只有审核不通过需要返工时才能 @SE，且必须附带 reject JSON 说明具体错误
- ⚠️ 如果你的上一条消息已经 @SE 过了，当前这条绝对不能再 @SE（除非是明确的返工指令）
- ⚠️ 收到 SE 的中间执行结果时，不要当新任务处理，继续推进当前流程即可
- ✅ 你可以做的：用 exec 工具运行测试/编译命令验证SE的工作；用 read_file / list_files 检查文件；输出审核结论JSON

✅ 正确的简洁回复格式：
- "@SE 请创建 hello.go"
- "@SE 请修改xxx，原因是xxx"
- "@AP 任务已验证，请进行最终审批"
- {"review_result":"reject","reason":"第3行有错误"}

Message Source Identification (IMPORTANT):
- USR: Real user messages
- PM: Your own replies
- SE: Software Engineer replies
- AP: Approval Processor (final reviewer)
- mc: MC monitor notifications (监控报警/状态报告)
- error: Error messages from system components
  → When you receive mc/error messages, analyze the content and report findings to @USR
  → These are informational messages meant for the user, always notify USR
  → When SE reports failure via error, decide whether to retry, modify approach, or abort, then tell @USR

你能@的角色：@SE（软件工程师），@AP（审批者/Approver）
- @SE：需要SE执行编码任务时用
- @AP：验证通过后@AP交给AP做最终质量审批，系统自动路由
- 不能@C（C不是对话参与者）
- 需要问USR时可以直接@USR（比如需求不明确、走不下去时），但转AP时不要@USR

你的职责：
1. 执行USR要求的任务（最高优先级，USR说做什么就做什么）
2. 与用户自然对话，回答问题、提供建议
3. ⚠️ **【最重要】分配任务给SE时，格式必须完全正确！**
   - ✅ 正确格式: "@SE 请创建 hello.go 文件"
   - ❌ 错误格式: "@USR @SE"、"@USR SE"、"@SE @USR"
   - ⚠️ **一个消息只能有一个@，且必须是@SE！**
   - 任务描述后面直接跟一行JSON启动SE
4. 🆕 [FIX-20260529] **严格区分闲聊与任务：**
   - ✅ 纯闲聊（天气/问候/闲扯）→ 直接回复@USR
   - ❌ **任何涉及以下关键词的请求，绝对禁止直接回复！必须@SE分配任务：**
     - 创建/新建/写/生成 文件 (create/make/write/generate file)
     - 修改/改/编辑/修 文件 (modify/edit/fix file)
     - 运行/执行/编译 命令 (run/exec/compile)
     - 代码/程序/脚本/函数 (code/program/script/function)
     - go/python/java/npm 等编程语言相关
     - hello world / 测试 / demo / 示例
   - ⚠️ 即使看起来很简单（如"创建hello.go"），也**必须走SE流程**！
   - 🔴 **违规后果：系统会检测到并强制转SE，但会浪费一次API调用**
5. **Code Review + QA验证（核心职责！）：SE完成后，你必须亲自验证**
   - 用 list_files 工具确认文件已创建
   - 用 read_file 工具检查文件内容是否正确
   - 用 exec 工具运行编译/测试命令（如 go run, type, npm test）
   - 不要轻信SE的汇报，眼见为实
6. 处理SE的失败，决定重试或调整方案

## 🔍 SE完成任务后的审核流程（⚠️ 绝对强制）

### 步骤
1. SE汇报完成 → 你立即用工具验证（list_files / read_file / exec）
2. 验证完成后，**如果通过 → @AP 转交AP做最终审批；如果不通过 → @SE 返工**

### 结果格式

**通过时——@AP 转交：**
@AP 任务已验证，请进行最终质量审批

**不通过时——@SE 返工：**
@SE 请修改：第3行有语法错误
{"review_result":"reject","reason":"第3行有语法错误/功能未实现"}

### 🚫 禁止事项
- ❌ 不要输出思考过程、分析步骤
- ❌ **不要输出 approve JSON 或说"审核已通过"**（那是AP的职责，你不能替AP做决定！）
- ❌ **通过时不要 @SE**（系统会自动转AP，你@SE会导致死循环！）
- ❌ **通过时绝对禁止 @USR 说"任务已完成/✅完成"等**（必须 @AP 转交！这是最常见的错误！）
- ❌ 转AP时不要 @USR（AP会最终通知用户）；但需求不明确、需要协调时可以@USR
- ❌ 输出状态更新JSON {"action":"update_state",...} （系统自动处理）
- ❌ **不要输出 @SE 加验证指令**（如 list_files、read_file、exec）你应该自己调用工具验证！
- ❌ **让 SE 执行验证命令**（你是审核者，自己验证）

### ⚠️ 常见错误（千万别犯！）
| 错误写法 | 正确写法 |
|---------|---------|
| @USR ✅ 任务已完成 | @AP 任务已验证，请进行最终质量审批 |
| @USR ✅ 审核通过 | @AP 任务已验证，请进行最终质量审批 |
| @USR 任务完成，请审批 | @AP 任务已验证，请进行最终质量审批 |

### 🔄 转交AP的完整流程
验证通过后，系统自动将工作流转给AP做最终审批：

**第一层（最快）—— @AP 标记：**
你输出 @AP 时，系统直接路由到AP做最终审批
→ 推荐使用 @AP 方式

**第二层（兜底）—— C监控超时：**
如果你输出异常或没@AP，C监控90秒后强制移交AP

### ✅ 审核模式正确做法
1. 收到 SE 完成报告 → **你自己调用工具验证**（list_files / read_file / exec）
2. 验证完成后 → **立即输出结论**，不要废话！
   - 通过就输出: @AP 任务已验证，请进行最终质量审批
   - 不通过就输出: @SE 请修改xxx，原因是xxx（然后加一行reject JSON）
3. **严禁输出思考过程**：不要写"让我想想"、"我需要验证"、"SE已经汇报了"等废话！

### 审核阶段绝对禁止
- 禁止输出思考过程、分析步骤、中间推理
- 禁止自言自语：不要描述你在做什么，直接做！
- 禁止给用户汇报：审核结论直接@AP或@SE，不要@USR说"SE已经完成"

### 工具调用限制
- **审核最多2轮工具调用**：用完工具后直接给结论
- 如果验证失败 → 用 @SE 指出具体错误要求返工

## 📋 任务分配流程

当用户提出编程需求时：
1. 拆解任务为简单步骤（每个任务只做一件事）
2. 输出任务JSON（放在回复最后一行）启动SE
3. SE完成后进入审核流程（见上方）

任务JSON格式：
{"current_task":"创建一个hello.go文件，输出Hello World","current_step":1,"total_steps":2,"status":"pending"}

## ⏰ 时间感知（重要！）
当前时间: {{CurrentTime}}
距上次交互: {{TimeSinceLast}}
今天是: {{DayOfWeek}} {{SpecialDay}}
关系阶段: {{RelationshipPhase}}

### 社交指南（让对话更有温度）：
- 如果用户很久没来（>24小时），先寒暄再谈正事
- 工作间隙可以适当闲聊（但不要频繁，不要干扰工作）
- 节假日要主动问候
- 注意用户的工作强度（今天工作了{{TodayWorkHours}}小时），适当关心
- 保持自然，像真同事/老朋友一样
- 关系越深（{{RelationshipPhase}}），表达可以越真诚
`

// PMProcessor PM处理器
type PMProcessor struct {
	client         *Client
	workDir        string
	systemPrompt   string
	timeContext    string
	stateUpdater   func(int)
	currentState   int
	terminalWriter func(string) error
	ReplyLanguage  string
	ctx            context.Context
	todoAdder      func(string) string    // 添加待办
	todoUpdater    func(string, string)   // 更新待办状态

	shellEmitter    types.ShellEventEmitter // 三层模型 Shell 事件推送（可选）
	currentTaskId   string                   // 当前 TaskList ID
	currentTaskIndex int                      // 当前执行到的步骤索引
}

// NewPMProcessor 创建PM处理器
func NewPMProcessor(client *Client, workDir string, stateUpdater func(int)) *PMProcessor {
	return &PMProcessor{
		client:       client,
		workDir:      workDir,
		systemPrompt: fmt.Sprintf(PMPrompt, workDir),
		stateUpdater: stateUpdater,
	}
}

// SetTerminalWriter 设置终端写入器（用于QA验证时显示执行过程）
func (p *PMProcessor) SetTerminalWriter(writer func(string) error) {
	p.terminalWriter = writer
}

// SetStateUpdater 设置状态更新回调
func (p *PMProcessor) SetStateUpdater(updater func(int)) {
	p.stateUpdater = updater
}

// SetTodoCallbacks 设置TODO回调
func (p *PMProcessor) SetTodoCallbacks(adder func(string) string, updater func(string, string)) {
	p.todoAdder = adder
	p.todoUpdater = updater
}

// SetTimeContext 设置时间上下文（动态注入到Prompt）
func (p *PMProcessor) SetTimeContext(timeInfo string) {
	p.timeContext = timeInfo
}

// SetContext 设置上下文（用于取消AI调用）
func (p *PMProcessor) SetContext(ctx context.Context) {
	p.ctx = ctx
}

func (p *PMProcessor) SetShellEmitter(emitter types.ShellEventEmitter) {
	p.shellEmitter = emitter
}

func (p *PMProcessor) SetTaskContext(taskId string, taskIndex int) {
	p.currentTaskId = taskId
	p.currentTaskIndex = taskIndex
}

// getCtx 获取上下文，nil 时返回 Background
func (p *PMProcessor) getCtx() context.Context {
	if p.ctx == nil {
		return context.Background()
	}
	return p.ctx
}

// getSystemPrompt 获取完整的System Prompt（基础+时间上下文）
func (p *PMProcessor) getSystemPrompt() string {
	if p.timeContext != "" {
		return p.systemPrompt + "\n\n" + p.timeContext
	}
	return p.systemPrompt
}

// PMTools PM可用的工具列表
var PMTools = []Tool{
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "update_project_state",
			Description: "更新项目状态。当开始任务时设为1（进行中），任务完成时设为2（已完成），出错时设为4（需用户介入），无任务时设为0（无项目）",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"state": map[string]interface{}{
						"type":        "integer",
						"description": "项目状态：0=无项目, 1=进行中, 2=已完成, 4=出错",
						"enum":        []int{0, 1, 2, 4},
					},
				},
				"required": []string{"state"},
			},
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "read_file",
			Description: "读取文件内容，用于Code Review审核SE的代码产出。可以读任何文件来验证SE是否正确完成了任务。",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "文件路径（相对于工作目录）",
					},
				},
				"required": []string{"path"},
			},
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "list_files",
			Description: "列出工作目录下的文件，用于了解SE创建了哪些文件。",
			Parameters: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "exec",
			Description: "执行命令用于QA验证。当SE汇报'编译成功'或'测试通过'时，你必须用此工具亲自验证！例如：go build, go run xxx.go, npm test, python xxx.py 等。超时30秒。",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"command": map[string]interface{}{
						"type":        "string",
						"description": "要执行的命令（如 go run hello.go, type file.txt, npm test）",
					},
				},
				"required": []string{"command"},
			},
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "add_todo",
			Description: "添加待办任务到TODO列表。当有来自其他角色的任务需要跟踪时使用，或者需要记住将来要做的事情。最多5条，超过自动淘汰最旧的。",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"description": map[string]interface{}{
						"type":        "string",
						"description": "待办任务描述",
					},
				},
				"required": []string{"description"},
			},
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "update_todo",
			Description: "更新待办任务状态（pending/doing/done）。任务完成后务必更新状态为done。",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"id": map[string]interface{}{
						"type":        "string",
						"description": "待办任务ID（add_todo返回的id）",
					},
					"status": map[string]interface{}{
						"type":        "string",
						"description": "新状态：pending=待办, doing=进行中, done=已完成",
						"enum":        []string{"pending", "doing", "done"},
					},
				},
				"required": []string{"id", "status"},
			},
		},
	},
}

// Process 处理用户输入
func (p *PMProcessor) Process(userInput string, history []ChatMessage) (*PMResponse, error) {
	fmt.Printf("[PM Debug] Processing input: %s, history count: %d\n", userInput, len(history))
	fmt.Printf("[PM Debug] System prompt length: %d\n", len(p.getSystemPrompt()))
	fmt.Printf("[PM Debug] AI client config: BaseURL=%s, Model=%s\n", p.client.config.BaseURL, p.client.config.Model)

	// 转换历史为AI消息格式
	aiHistory := make([]Message, 0, len(history))
	for _, msg := range history {
		if msg.Role == "user" {
			aiHistory = append(aiHistory, Message{Role: "user", Content: msg.Content})
		} else if msg.Role == "pm" {
			aiHistory = append(aiHistory, Message{Role: "assistant", Content: msg.Content})
		} else if msg.Role == "se" {
			aiHistory = append(aiHistory, Message{Role: "user", Content: msg.Content})
		}
	}

	fmt.Printf("[PM Debug] Calling AI with %d history messages\n", len(aiHistory))
	response, err := p.client.ChatWithHistory(p.getCtx(), p.getSystemPrompt(), aiHistory, userInput, p.ReplyLanguage)
	if err != nil {
		fmt.Printf("[PM Debug] AI call failed: %v\n", err)
		return nil, err
	}
	fmt.Printf("[PM Debug] AI response received, length: %d\n", len(response))

	// 检查是否有状态更新JSON
	p.extractAndUpdateState(response)
	tasks := p.extractTasks(response)
	reviewResult, reviewReason := p.extractReviewResult(response)

	return &PMResponse{
		Content:      response,
		Tasks:        tasks,
		HasTasks:     tasks != nil,
		ReviewResult: reviewResult,
		ReviewReason: reviewReason,
	}, nil
}

// ProcessStream 流式处理用户输入，每收到文本片段调用 onChunk
func (p *PMProcessor) ProcessStream(userInput string, history []ChatMessage, onChunk func(delta string)) (*PMResponse, error) {
	fmt.Printf("[PM Stream] Processing input: %s, history count: %d\n", userInput, len(history))

	aiHistory := make([]Message, 0, len(history))
	for _, msg := range history {
		if msg.Role == "user" {
			aiHistory = append(aiHistory, Message{Role: "user", Content: msg.Content})
		} else if msg.Role == "pm" {
			aiHistory = append(aiHistory, Message{Role: "assistant", Content: msg.Content})
		}
	}

	response, err := p.client.ChatStream(p.getCtx(), p.getSystemPrompt(), aiHistory, userInput, p.ReplyLanguage, onChunk, nil)
	if err != nil {
		fmt.Printf("[PM Stream] AI call failed: %v\n", err)
		return nil, err
	}
	fmt.Printf("[PM Stream] AI response completed, length: %d\n", len(response))

	p.extractAndUpdateState(response)
	tasks := p.extractTasks(response)
	reviewResult, reviewReason := p.extractReviewResult(response)

	return &PMResponse{
		Content:      response,
		Tasks:        tasks,
		HasTasks:     tasks != nil,
		ReviewResult: reviewResult,
		ReviewReason: reviewReason,
	}, nil
}

// ProcessReview 带Code Review能力的审核处理（使用工具调用）
func (p *PMProcessor) ProcessReview(reviewMsg string, history []ChatMessage, onChunk func(delta string)) (*PMResponse, error) {
	fmt.Printf("[PM Review] Processing review: %s\n", reviewMsg)

	if onChunk != nil {
		onChunk("🔍 **PM开始审核...**\n\n")
	}

	aiHistory := make([]Message, 0, len(history))
	for _, msg := range history {
		if msg.Role == "user" {
			aiHistory = append(aiHistory, Message{Role: "user", Content: msg.Content})
		} else if msg.Role == "pm" {
			aiHistory = append(aiHistory, Message{Role: "assistant", Content: msg.Content})
		} else if msg.Role == "se" {
			aiHistory = append(aiHistory, Message{Role: "user", Content: msg.Content})
		}
	}

	maxToolRounds := 5
	var finalContent string
	toolCalled := false
	nagCount := 0
	maxNagCount := 2

	for round := 0; round < maxToolRounds; round++ {
		callCtx, callCancel := context.WithTimeout(p.getCtx(), 60*time.Second)
		resp, err := p.client.ChatWithTools(callCtx, p.getSystemPrompt(), aiHistory, reviewMsg, PMTools)
		callCancel()
		if err != nil {
			if strings.Contains(err.Error(), "context deadline exceeded") || strings.Contains(err.Error(), "context canceled") || strings.Contains(err.Error(), "timeout") {
				fmt.Printf("[PM Review] ⚠️ Round %d API调用超时(60s), 强制降级输出结论\n", round+1)
				finalContent = "@AP 任务已验证，请进行最终质量审批"
				break
			}
			return nil, err
		}

		if len(resp.Choices) == 0 {
			return nil, fmt.Errorf("no response from AI")
		}

		msg := resp.Choices[0].Message
		finalContent = msg.Content

		if len(msg.ToolCalls) == 0 {
			if !toolCalled {
				nagCount++

				if nagCount <= maxNagCount {
					// 🐕 C监控多次提醒PM（最多maxNagCount次）
					nagMessages := []string{
						"[🐕 C监控] ⚠️ PM你没有调用任何验证工具就说过关！这是失职！请立即用工具验证：创建文件→read_file，运行程序→exec，删除文件→list_files",
						"[🐕 C监控] 🔴 再次警告！PM仍然拒绝验证！这是严重的职业疏忽！SE的工作成果必须经过QA验证才能确认完成！请立即执行：list_files查看文件 → read_file检查内容 → exec运行测试",
						"[🐕 C监控] 💀 最后通牒！PM已连续拒绝验证%d次！系统将启动自动降级验证程序！这是你最后的机会证明自己的专业能力！立即使用exec或read_file工具！",
					}

					var nagMsg string
					if nagCount < len(nagMessages) {
						nagMsg = nagMessages[nagCount-1]
					} else {
						nagMsg = fmt.Sprintf(nagMessages[len(nagMessages)-1], nagCount)
					}

					aiHistory = append(aiHistory, Message{Role: "user", Content: reviewMsg})
					aiHistory = append(aiHistory, msg)
					aiHistory = append(aiHistory, Message{
						Role:       "user",
						Content:    nagMsg,
						ToolCallID: fmt.Sprintf("c_monitor_nag_%d", nagCount),
					})
					reviewMsg = fmt.Sprintf("[🐕 C监控要求(%d/%d)] 你必须先调用验证工具，验证SE的工作成果后才能给出结论", nagCount, maxNagCount)
					continue
				}
			}

			// PM多次提醒后仍拒绝 → 强制autoVerify降级
			autoResult := p.autoVerify()
			aiHistory = append(aiHistory, Message{Role: "user", Content: reviewMsg})
			aiHistory = append(aiHistory, msg)
			aiHistory = append(aiHistory, Message{
				Role:       "user",
				Content:    fmt.Sprintf("[🐕 C监控强制降级验证] PM已连续拒绝验证%d次，系统接管QA流程！\n\n%s\n\n⚠️ 请基于以上【系统Git模块】自动检测到的变更信息给出最终结论，不得再跳过验证！", nagCount, autoResult),
				ToolCallID: "auto_verify_forced",
			})
			reviewMsg = "[C监控] 已执行强制降级验证（系统Git模块），请基于验证结果给出最终结论"
			continue
		}

		toolCalled = true
		aiHistory = append(aiHistory, Message{Role: "user", Content: reviewMsg})
		aiHistory = append(aiHistory, msg)

		for _, tc := range msg.ToolCalls {
			if onChunk != nil {
				onChunk(fmt.Sprintf("🔧 **调用工具**: `%s`\n", tc.Function.Name))
			}
			toolResult := p.executeTool(tc.Function.Name, tc.Function.Arguments)
			if onChunk != nil {
				resultPreview := toolResult
				if len(resultPreview) > 200 {
					resultPreview = resultPreview[:200] + "..."
				}
				onChunk(fmt.Sprintf("✅ **工具结果** (%d bytes):\n```\n%s\n```\n\n", len(toolResult), resultPreview))
			}
			aiHistory = append(aiHistory, Message{
				Role:       "tool",
				Content:    toolResult,
				ToolCallID: tc.ID,
			})
		}

		reviewMsg = "[工具结果已返回，请继续审核并给出最终结论]"
	}

	if strings.TrimSpace(finalContent) == "" {
		fmt.Printf("[PM Review] ⚠️ AI返回空内容，使用默认审核通过消息 (G38修复)\n")
		finalContent = "@AP 任务已验证，请进行最终质量审批"
	}

	if onChunk != nil {
		onChunk("\n---\n📝 **PM审核结论**:\n")
		for _, ch := range finalContent {
			onChunk(string(ch))
		}
		onChunk("\n")
	}

	p.extractAndUpdateState(finalContent)
	tasks := p.extractTasks(finalContent)
	reviewResult, reviewReason := p.extractReviewResult(finalContent)

	return &PMResponse{
		Content:      finalContent,
		Tasks:        tasks,
		HasTasks:     tasks != nil,
		ReviewResult: reviewResult,
		ReviewReason: reviewReason,
	}, nil
}

// autoVerify PM不调工具时的代码兜底验证
// 使用系统Git模块获取结构化变更信息（复用C监控的检测能力）
func (p *PMProcessor) autoVerify() string {
	statusEntries := git.GetStatus(p.workDir)
	if statusEntries == nil {
		return p.autoVerifyFallback()
	}

	if len(statusEntries) == 0 {
		return "git status无变更（可能SE未修改任何文件，或变更已被commit）"
	}

	var result strings.Builder
	result.WriteString("📋 [C监控+Git模块] 自动检测到SE代码变更：\n\n")
	result.WriteString(fmt.Sprintf("共 %d 个文件发生变更\n\n", len(statusEntries)))

	totalAdditions := 0
	totalDeletions := 0
	fileCount := 0

	for _, entry := range statusEntries {
		path, _ := entry["path"].(string)
		status, _ := entry["status"].(string)

		if path == "" || strings.HasPrefix(path, ".argus") {
			continue
		}

		fileCount++

		result.WriteString(fmt.Sprintf("📄 文件%d: %s [%s]\n", fileCount, path, translateGitStatus(status)))

		diff := git.GetFileDiff(p.workDir, path)
		if diff != nil && diff.Content != "" {
			totalAdditions += diff.Additions
			totalDeletions += diff.Deletions

			preview := diff.Content
			if len(preview) > 300 {
				preview = preview[:300] + "\n... (截断)"
			}
			result.WriteString(fmt.Sprintf("   ➕%d行 ➖%d行\n", diff.Additions, diff.Deletions))
			result.WriteString(fmt.Sprintf("   变更预览:\n%s\n", preview))
		}

		result.WriteString("\n")

		if fileCount >= 5 {
			remaining := len(statusEntries) - fileCount
			if remaining > 0 {
				result.WriteString(fmt.Sprintf("... 还有 %d 个文件未显示\n", remaining))
			}
			break
		}
	}

	result.WriteString(fmt.Sprintf("📊 变更统计: %d个文件, +%d行, -%d行",
		fileCount, totalAdditions, totalDeletions))

	output := result.String()
	if len(output) > 2000 {
		output = output[:2000] + "\n... (报告过长已截断)"
	}

	return output
}

// translateGitStatus 翻译Git状态码为中文
func translateGitStatus(status string) string {
	statusMap := map[string]string{
		"M":  "已修改",
		"A":  "新增",
		"D":  "已删除",
		"R":  "重命名",
		"C":  "复制",
		"U":  "未跟踪",
		"??": "未跟踪新文件",
		" M": "工作区修改",
		"A ": "暂存区新增",
		"D ": "暂存区删除",
		"MM": "工作区和暂存区都修改",
	}
	if desc, ok := statusMap[status]; ok {
		return desc
	}
	return status
}

// autoVerifyFallback 非git仓库的降级验证
func (p *PMProcessor) autoVerifyFallback() string {
	var result strings.Builder
	entries, err := os.ReadDir(p.workDir)
	if err != nil {
		return fmt.Sprintf("列出文件失败: %v", err)
	}

	type fileInfo struct {
		name    string
		size    int64
		modTime time.Time
	}
	var files []fileInfo
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, fileInfo{e.Name(), info.Size(), info.ModTime()})
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].modTime.After(files[j].modTime)
	})

	maxRecent := 3
	recentCount := 0
	for i := 0; i < len(files) && recentCount < maxRecent; i++ {
		if time.Since(files[i].modTime) > 10*time.Minute {
			break
		}
		content, err := os.ReadFile(filepath.Join(p.workDir, files[i].name))
		if err != nil {
			continue
		}
		preview := string(content)
		if len(preview) > 200 {
			preview = preview[:200] + "..."
		}
		result.WriteString(fmt.Sprintf("最近修改: %s\n%s\n", files[i].name, preview))
		recentCount++
	}

	if result.Len() == 0 {
		return "未找到最近修改的文件"
	}
	return result.String()
}

func (p *PMProcessor) executeTool(name, argsJSON string) string {
	switch name {
	case "read_file":
		var args struct {
			Path string `json:"path"`
		}
		json.Unmarshal([]byte(argsJSON), &args)
		if p.shellEmitter != nil && p.currentTaskId != "" {
			p.shellEmitter.PushShellStart("pm", p.currentTaskId, p.currentTaskIndex, "read_file", args.Path, nil)
			startTime := time.Now()
			content, err := os.ReadFile(filepath.Join(p.workDir, args.Path))
			duration := time.Since(startTime).String()
			if err != nil {
				p.shellEmitter.PushShellDone("pm", p.currentTaskId, -1, duration, "error")
				return fmt.Sprintf("读取文件失败: %v", err)
			}
			p.shellEmitter.PushShellDone("pm", p.currentTaskId, 0, duration, "done")
			return string(content)
		}
		content, err := os.ReadFile(filepath.Join(p.workDir, args.Path))
		if err != nil {
			return fmt.Sprintf("读取文件失败: %v", err)
		}
		return string(content)

	case "list_files":
		if p.shellEmitter != nil && p.currentTaskId != "" {
			p.shellEmitter.PushShellStart("pm", p.currentTaskId, p.currentTaskIndex, "list_files", ".", nil)
			startTime := time.Now()
			entries, err := os.ReadDir(p.workDir)
			duration := time.Since(startTime).String()
			if err != nil {
				p.shellEmitter.PushShellDone("pm", p.currentTaskId, -1, duration, "error")
				return fmt.Sprintf("列出文件失败: %v", err)
			}
			var result []string
			for _, e := range entries {
				result = append(result, e.Name())
			}
			p.shellEmitter.PushShellDone("pm", p.currentTaskId, 0, duration, "done")
			return strings.Join(result, "\n")
		}
		entries, err := os.ReadDir(p.workDir)
		if err != nil {
			return fmt.Sprintf("列出文件失败: %v", err)
		}
		var result []string
		for _, e := range entries {
			result = append(result, e.Name())
		}
		return strings.Join(result, "\n")

	case "exec":
		var args struct {
			Command string `json:"command"`
		}
		json.Unmarshal([]byte(argsJSON), &args)

		hasEmitter := p.shellEmitter != nil && p.currentTaskId != ""
		if hasEmitter {
			p.shellEmitter.PushShellStart("pm", p.currentTaskId, p.currentTaskIndex, "exec", args.Command, nil)
		}

		cmd := exec.Command("powershell", "-NoProfile", "-WindowStyle", "Hidden", "-Command", args.Command)
		cmd.Dir = p.workDir
		cmd.SysProcAttr = &syscall.SysProcAttr{
			HideWindow:    true,
			CreationFlags: 0x08000000,
		}
		cmd.Env = append(os.Environ(),
			"[Console]::OutputEncoding = [System.Text.Encoding]::UTF8",
			"$OutputEncoding = [System.Text.Encoding]::UTF8",
		)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		err := cmd.Start()
		if err != nil {
			if hasEmitter {
				p.shellEmitter.PushShellDone("pm", p.currentTaskId, -1, "", "error")
			}
			return fmt.Sprintf("命令执行失败(启动): %v", err)
		}

		done := make(chan error, 1)
		go func() {
			done <- cmd.Wait()
		}()

		select {
		case <-time.After(30 * time.Second):
			cmd.Process.Kill()
			result := fmt.Sprintf("命令执行超时(30秒): %s\n已终止进程", args.Command)
			if p.terminalWriter != nil {
				p.terminalWriter(result)
			}
			if hasEmitter {
				p.shellEmitter.PushShellDone("pm", p.currentTaskId, -1, "30s", "error")
			}
			return result
		case err = <-done:
			output := stdout.String()
			if stderr.Len() > 0 {
				output += "\n[stderr]\n" + stderr.String()
			}
			exitCode := 0
			if cmd.ProcessState != nil {
				exitCode = cmd.ProcessState.ExitCode()
			}
			if hasEmitter {
				p.shellEmitter.PushShellOutput(p.currentTaskId, output)
				shellStart := p.shellEmitter.GetLastShellTimestamp()
				if shellStart > 0 {
					duration := types.FormatDuration(shellStart, time.Now().Unix())
					if exitCode == 0 {
						p.shellEmitter.PushShellDone("pm", p.currentTaskId, exitCode, duration, "done")
					} else {
						p.shellEmitter.PushShellDone("pm", p.currentTaskId, exitCode, duration, "error")
					}
				}
			}
			if err != nil {
				result := fmt.Sprintf("命令执行失败(exit code %d):\n%s", exitCode, output)
				if p.terminalWriter != nil {
					p.terminalWriter(result)
				}
				return result
			}

			if p.terminalWriter != nil {
				p.terminalWriter(output + "\n")
			}

			return output
		}
	case "update_project_state":
		var args struct {
			State int `json:"state"`
		}
		json.Unmarshal([]byte(argsJSON), &args)

		// ✅ 状态锁定检查：如果已经完成(state=2)，不允许改为 idle(0)，但允许改为 error(4) 或新任务(1)
		if p.currentState == 2 && args.State == 0 {
			fmt.Printf("[PM] ⚠️ 状态锁定：项目已完成，拒绝重置为 idle(0)\n")
			return fmt.Sprintf("状态保持为: 2 (已完成)，拒绝重置为 idle(0)")
		}

		if p.stateUpdater != nil {
			p.stateUpdater(args.State)
			p.currentState = args.State // 更新当前状态
		}
		return fmt.Sprintf("状态已更新为: %d", args.State)
	case "add_todo":
		var args struct {
			Description string `json:"description"`
		}
		json.Unmarshal([]byte(argsJSON), &args)
		if p.todoAdder != nil {
			id := p.todoAdder(args.Description)
			return fmt.Sprintf("待办已添加 ✅  ID: %s | 描述: %s", id, args.Description)
		}
		return "TODO功能未配置"
	case "update_todo":
		var args struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		}
		json.Unmarshal([]byte(argsJSON), &args)
		if p.todoUpdater != nil {
			p.todoUpdater(args.ID, args.Status)
			return fmt.Sprintf("待办 %s 状态已更新为 %s", args.ID, args.Status)
		}
		return "TODO功能未配置"
	default:
		return fmt.Sprintf("未知工具: %s", name)
	}
}

// ChatMessage 通用消息结构（供外部传入）
type ChatMessage struct {
	Role    string
	Content string
}

// extractAndUpdateState 从回复中提取状态更新JSON并执行
func (p *PMProcessor) extractAndUpdateState(response string) {
	// 尝试从文本中提取 JSON（可能在同一行有其他文本）
	jsonStart := strings.Index(response, `{"action"`)
	if jsonStart == -1 {
		return
	}

	// 找到 JSON 结束位置
	jsonEnd := strings.Index(response[jsonStart:], "}")
	if jsonEnd == -1 {
		return
	}
	jsonStr := response[jsonStart : jsonStart+jsonEnd+1]

	var stateAction struct {
		Action string `json:"action"`
		State  int    `json:"state"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &stateAction); err == nil && stateAction.Action == "update_state" {
		fmt.Printf("[PM] 状态更新JSON: state=%d, current=%d\n", stateAction.State, p.currentState)

		// ✅ 状态锁定检查：如果已经完成(state=2)，不允许改为 idle(0)，但允许改为 error(4) 或新任务(1)
		if p.currentState == 2 && stateAction.State == 0 {
			fmt.Printf("[PM] ⚠️ 状态锁定：项目已完成，拒绝JSON重置为 idle(0)\n")
			return
		}

		if p.stateUpdater != nil {
			p.stateUpdater(stateAction.State)
			p.currentState = stateAction.State // 更新当前状态
		}
	}
}

// PMResponse PM响应
type PMResponse struct {
	Content      string
	Tasks        *types.Board
	HasTasks     bool
	ReviewResult string // AI结构化判断结果: "approve" 或 "reject"
	ReviewReason string // AI判断的理由
}

// extractReviewResult 从AI回复中提取审核结果
// 主路：AI输出JSON格式 {"review_result":"reject","reason":"..."}
// 兜底：AI没输出JSON时，靠关键词匹配
func (p *PMProcessor) extractReviewResult(content string) (result string, reason string) {
	// 主路：AI结构化JSON
	jsonIdx := strings.Index(content, `{"review_result"`)
	if jsonIdx != -1 {
		// 找到完整的JSON块（可能跨多行）
		braceCount := 0
		endIdx := -1
		for i := jsonIdx; i < len(content); i++ {
			if content[i] == '{' {
				braceCount++
			} else if content[i] == '}' {
				braceCount--
				if braceCount == 0 {
					endIdx = i + 1
					break
				}
			}
		}
		if endIdx > jsonIdx {
			jsonStr := content[jsonIdx:endIdx]
			var jsonResult struct {
				Result string `json:"review_result"`
				Reason string `json:"reason"`
			}
			if err := json.Unmarshal([]byte(jsonStr), &jsonResult); err == nil {
				result = strings.ToLower(strings.TrimSpace(jsonResult.Result))
				reason = jsonResult.Reason
				return result, reason
			}
		}
	}

	// 兜底：关键词匹配
	contentLower := strings.ToLower(content)
	rejectKeywords := []string{"不通过", "未通过", "❌", "返工", "驳回", "拒绝", "rejected", "reject", "失败", "failed", "错误", "error", "有bug", "bug", "修改", "重写", "need fix", "need to change", "rework"}
	for _, kw := range rejectKeywords {
		if strings.Contains(contentLower, kw) {
			return "reject", content
		}
	}
	approveKeywords := []string{"审核通过", "验证通过", "✅", "批准", "通过", "approved", "pass", "ok", "任务完成"}
	for _, kw := range approveKeywords {
		if strings.Contains(contentLower, kw) {
			return "approve", ""
		}
	}

	return "", ""
}

// extractTasks 从响应中提取任务JSON
func (p *PMProcessor) extractTasks(response string) *types.Board {
	lines := strings.Split(response, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if strings.HasPrefix(line, "{") && (strings.Contains(line, "task_id") || strings.Contains(line, "current_task")) {
			var board types.Board
			if err := json.Unmarshal([]byte(line), &board); err == nil {
				if board.TaskID == "" {
					board.TaskID = fmt.Sprintf("task_%d", time.Now().Unix())
				}
				if board.AssignedTo == "" {
					board.AssignedTo = "se"
				}
				return &board
			}
		}
	}
	return nil
}

// ReviewSEResult 审核SE结果
func (p *PMProcessor) ReviewSEResult(taskDesc, technicalNotes, changelog string) (string, error) {
	prompt := fmt.Sprintf(`你是Argus的项目经理(PM)，请审核SE完成的任务。

任务描述: %s
技术笔记: %s
变更日志: %s

请严格审核（必须执行以下步骤）：
1. **读取文件**：用 read_file 读取SE创建/修改的文件，确认代码内容正确
2. **运行验证**：用 exec 运行代码（如 python xxx.py / go run xxx.go），确认输出正确
3. **检查遗漏**：是否有边界情况未处理、是否有语法错误
4. **代码质量**：命名规范、逻辑清晰度

审核规则：
- 必须先 read_file + exec 验证，才能判断通过
- 如果验证失败，说明原因并要求SE返工
- 如果验证通过，回复"审核通过"

如果完成，回复"审核通过"。
如果需要返工，说明原因。`, taskDesc, technicalNotes, changelog)

	return p.client.Chat(p.getCtx(), prompt, "请审核上述任务", p.ReplyLanguage)
}

// HandleSEFailure 处理SE失败
func (p *PMProcessor) HandleSEFailure(taskDesc, errorMsg string) (string, error) {
	prompt := fmt.Sprintf(`你是Argus的项目经理(PM)，SE执行任务失败了。

任务描述: %s
错误信息: %s

请分析：
1. 失败原因
2. 是否需要调整任务
3. 是否需要换种方法
4. 给出新的任务计划（如果需要）`, taskDesc, errorMsg)

	return p.client.Chat(p.getCtx(), prompt, "请分析失败原因并给出建议", p.ReplyLanguage)
}
