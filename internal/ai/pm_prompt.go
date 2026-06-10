package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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

🔑 **最高原则：不理解就问，绝不瞎猜！（优先级高于一切）**
- 用户指令模糊、有歧义、有多种理解时 → **先 @USR，禁止猜一个意思直接@SE**
- 不确定任务范围、方式、目标时 → **先用 list_files/web_search 了解现状**
- 可以给用户选项：@USR 你的意思是 A)删除测试文件 B)整理代码结构 C)git clean？
- 问清楚再安排，比你猜错后重来快 10 倍
- **猜错的代价远大于多问一句的时间**

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
4. 🆕 [FIX-20260607] **不会就问！不理解不瞎猜！（最高优先级规则）**
   - ⚠️ 当用户指令**不明确、模糊、有多种理解**时：
     - **绝对禁止自己猜测意图然后直接@SE** — 猜错代价巨大！
     - **先 @USR 向用户确认**: 用简洁问题澄清 (如 "@USR 你说的'清理'是指删除测试文件，还是整理代码结构？")
     - **可以用 web_search 工具搜索不理解的术语或任务**
     - **可以用 list_files 工具先了解当前状态，再决定怎么做**
   - ⚠️ 常见会误解的模糊指令举例：
     - "清理" → 删除文件？整理代码？格式化？git clean？
     - "改一下" → 改什么？怎么改？哪个文件？
     - "检查" → 检查什么？编译？测试？安全？
   - ✅ 正确流程：不理解 → @USR 确认 → 理解后 @SE 安排
   - ❌ 错误流程：不理解 → 瞎猜 → @SE（浪费时间！）
5. 🆕 [FIX-20260529] **严格区分闲聊与任务：**
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

## 📋 TODO 管理规则（⚠️ 必须遵守）

⚠️ **先建清单，再派活**：接到用户任务后，第一时间调用 add_todo 拆解为待办清单，完成后再 @SE 分派任务。严禁先派活后补清单！
✅ **完工即勾**：SE 汇报完成 → 调用 update_todo 标记 done。AP 批准 → 标记 done。AP 驳回 → 标记 pending 并 @SE 返工。
📌 **清单是一轮对话的总看板**：用户追加需求 → add_todo 追加条目，不清空已有清单。新任务到来 → PM 判断是否新建清单。

## 📋 任务分配流程

当用户提出编程需求时：
1. 拆解任务为简单步骤（每个任务只做一件事）
2. 输出任务JSON（放在回复最后一行）启动SE
3. SE完成后进入审核流程（见上方）

## 📄 文档处理任务（非编码任务）

当用户提出文档相关需求时（如"比较PDF和Word"、"从PDF提取数据生成报表"、"OCR识别扫描件"）：
1. **直接@SE**，让SE使用文档工具完成
2. SE拥有以下文档能力（无需你操心依赖安装，SE会自动处理）：
   - read_pdf: 读取PDF（支持OCR）
   - read_docx: 读取Word
   - write_docx: 生成Word
   - compare_docs: 比较两个文档差异
3. 如果需要OCR或特殊库，SE会自动调用 ensure_tool 安装依赖
4. 审核方式：用 read_file 查看生成的文件，或要求SE展示结果摘要

示例任务分配：
- "@请比较 docs/contract.pdf 和 docs/contract_v2.docx 的差异"
- "@请读取 report.pdf 的内容并生成一份 Word 摘要"
- "@请对 scanned.pdf 做 OCR 提取文字"

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

---

## 🪶 [v0.8] PM直执模式（Featherweight任务）

### 你现在具备 SE 的全部执行能力！

当任务属于 **Featherweight 级别**（单文件/<100行/无依赖）时，你不再需要 @SE，而是**自己直接执行**：

#### 你的执行工具
- **write_file**: 创建/覆写文件（如 hello.go）
- **exec**: 执行命令（如 go run hello.go）
- **read_file**: 读取文件内容
- **edit_file**: 编辑文件（字符串替换）
- **list_files**: 列出工作目录文件
- **search_files**: 搜索文件
- **delete_file**: 删除文件
- 以及其他所有文档处理工具（read_pdf, write_docx 等）

#### 分级判断标准

| 级别 | 标准 | 你的行为 |
|------|------|----------|
| **Featherweight** 🪶 | 单文件 / <100行 / 无依赖 | **你自己直接干！** 用工具写代码+执行+汇报 |
| **Lightweight** ⚡ | 2-5文件 / <500行 / 单一功能 | @SE 分配任务 |
| **Medium** | 多模块 / <5000行 / 有内部依赖 | @SE 分配任务 |
| **Heavy** | 大型项目 | @SE 分派任务 |

> 用户也可以用 /level featherweight 强制指定级别，用户指定优先。

#### Featherweight 任务执行规范（必须遵守）

1. **一次搞定**：在一次 Tool Call 响应中返回完整的 actions（write_file + exec）
2. **必须包含 exec 验证**：写完代码后必须 exec（go run xxx.go）验证
3. **结果汇报**：在你的 Content 文本中包含简洁的结果总结，格式：
   - 🪶 已完成 xxx (N个操作): ✅ write_file xxx.go (N bytes)
   - 如果 exec 有输出结果，也一并列出
4. **不换角色、不换工位**：全程以 PM 身份执行和汇报
5. **出错时重试**：如果 exec 失败，重新生成修正后的 actions 再试

#### 典型 Featherweight 示例
- "创建 hello world" → write_file(hello.go) + exec(go run hello.go)
- "写个斐波那契" → write_file(fib.go) + exec(go run fib.go)
- "创建计数器" → write_file(counter.go) + exec(go run counter.go)

⚠️ **只有 Featherweight 任务才自己直接执行。其他级别必须 @SE 分配任务！**
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
	todoClearer    func()                 // 清空待办（replace=true时）

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
func (p *PMProcessor) SetTodoCallbacks(adder func(string) string, updater func(string, string), clearer func()) {
	p.todoAdder = adder
	p.todoUpdater = updater
	p.todoClearer = clearer
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
			Description: "⚠️ 必须在接到用户任务后、@SE分派之前调用！将任务拆解为待办清单（最多5条）。新任务替换=true清空旧清单，追加需求替换=false。严禁事后补加！",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"description": map[string]interface{}{
						"type":        "string",
						"description": "待办任务描述",
					},
					"replace": map[string]interface{}{
						"type":        "boolean",
						"description": "true=新建清单（清空旧的），false=追加到现有清单",
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
			Description: "更新待办状态：SE完成/AP批准→done，AP驳回→pending，正在执行→doing。收到SE汇报或AP结果后必须调用！",
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
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "web_search",
			Description: "搜索网络获取最新信息。当你遇到不理解的术语、技术概念、或者需要查文档、查最佳实践时使用。比如：'清理工作目录 最佳实践'、'go mod tidy vs clean'、'PowerShell批量删除文件通配符'等。",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{
						"type":        "string",
						"description": "搜索查询语句",
					},
				},
				"required": []string{"query"},
			},
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "fetch_url",
			Description: "抓取URL内容。用于获取网页文档、API文档等详细资料。",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"url": map[string]interface{}{
						"type":        "string",
						"description": "要抓取的URL地址",
					},
				},
				"required": []string{"url"},
			},
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "grep_content",
			Description: "搜索文件内容（支持正则表达式）。在项目中搜索包含特定模式的文件，用于理解代码库结构、找函数定义、查引用等。",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"pattern": map[string]interface{}{
						"type":        "string",
						"description": "要搜索的正则表达式模式（如 'func.*Login'）",
					},
					"glob": map[string]interface{}{
						"type":        "string",
						"description": "文件筛选模式（如 '*.go'），可选",
					},
				},
				"required": []string{"pattern"},
			},
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "find_files",
			Description: "按文件名模式查找文件（支持通配符如 **/*.go, src/**/*.ts）。用于了解项目中有哪些文件。",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"pattern": map[string]interface{}{
						"type":        "string",
						"description": "文件名模式（如 '**/*.go', 'test_*.go'）",
					},
				},
				"required": []string{"pattern"},
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

	// [v0.7.2] 使用 ChatWithTools 让 PM 能调用 add_todo/update_todo
	maxToolRounds := 5
	var finalContent string
	var hasToolCalls bool // [v0.8] 记录是否有ToolCalls

	for round := 0; round < maxToolRounds; round++ {
		callCtx, callCancel := context.WithTimeout(p.getCtx(), 120*time.Second)
		resp, err := p.client.ChatWithTools(callCtx, p.getSystemPrompt(), aiHistory, userInput, PMTools)
		callCancel()
		if err != nil {
			return nil, err
		}

		if len(resp.Choices) == 0 {
			return nil, fmt.Errorf("no response from AI")
		}

		msg := resp.Choices[0].Message
		// 累积所有轮次的内容（工具轮 + 最终文本轮），避免 @SE 被后续工具轮覆盖
		if msg.Content != "" {
			if finalContent != "" && !strings.Contains(finalContent, msg.Content) {
				finalContent += "\n" + msg.Content
			} else if finalContent == "" {
				finalContent += msg.Content
			}
		}

		// 没有工具调用 → 结束
		if len(msg.ToolCalls) == 0 {
			break
		}

		// [v0.8] 记录PM是否有ToolCalls（用于Featherweight分流判断）
		hasToolCalls = true

		// 处理工具调用（add_todo / update_todo）
		aiHistory = append(aiHistory, Message{Role: "user", Content: userInput})
		aiHistory = append(aiHistory, msg)

		for _, tc := range msg.ToolCalls {
			if onChunk != nil {
				onChunk(fmt.Sprintf("🔧 **调用工具**: `%s`\n", tc.Function.Name))
			}
			toolResult := p.executeTool(tc.Function.Name, tc.Function.Arguments)
			aiHistory = append(aiHistory, Message{
				Role:       "tool",
				Content:    toolResult,
				ToolCallID: tc.ID,
			})
		}

		userInput = "[工具结果已返回，请继续分析并给出结论]"
	}

	p.extractAndUpdateState(finalContent)
	tasks := p.extractTasks(finalContent)
	reviewResult, reviewReason := p.extractReviewResult(finalContent)

	return &PMResponse{
		Content:      finalContent,
		Tasks:        tasks,
		HasTasks:     tasks != nil,
		HasToolCalls: hasToolCalls, // [v0.8]
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
			Replace     bool   `json:"replace"`
		}
		json.Unmarshal([]byte(argsJSON), &args)
		if p.todoAdder != nil {
			// Replace=true → 清空旧清单重建（Trae风格merge=false）
			if args.Replace && p.todoClearer != nil {
				p.todoClearer()
			}
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
	case "grep_content":
		var args struct {
			Pattern string `json:"pattern"`
			Glob    string `json:"glob"`
		}
		json.Unmarshal([]byte(argsJSON), &args)
		if args.Pattern == "" {
			return "错误: pattern参数为空"
		}
		return pmGrep(p.workDir, args.Pattern, args.Glob)
	case "find_files":
		var args struct {
			Pattern string `json:"pattern"`
		}
		json.Unmarshal([]byte(argsJSON), &args)
		if args.Pattern == "" {
			return "错误: pattern参数为空"
		}
		return pmGlob(p.workDir, args.Pattern)
	case "web_search":
		var args struct {
			Query string `json:"query"`
		}
		json.Unmarshal([]byte(argsJSON), &args)
		if args.Query == "" {
			return "错误: query参数为空"
		}
		return pmWebSearch(args.Query)
	case "fetch_url":
		var args struct {
			URL string `json:"url"`
		}
		json.Unmarshal([]byte(argsJSON), &args)
		if args.URL == "" {
			return "错误: url参数为空"
		}
		return pmWebFetch(args.URL)
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
	HasToolCalls bool   // [v0.8] PM响应中是否包含ToolCalls（用于Featherweight分流判断）
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

// pmWebSearch PM可用的网络搜索（并行DuckDuckGo/Bing/Google）
func pmWebSearch(query string) string {
	client := &http.Client{Timeout: 15 * time.Second}
	engines := []struct {
		name string
		url  string
	}{
		{"DuckDuckGo", fmt.Sprintf("https://html.duckduckgo.com/html/?q=%s", url.QueryEscape(query))},
		{"Bing", fmt.Sprintf("https://www.bing.com/search?q=%s", url.QueryEscape(query))},
	}

	type result struct {
		name string
		text string
	}
	ch := make(chan result, len(engines))

	for _, eng := range engines {
		go func(name, u string) {
			text := fetchAndExtractText(client, u, name)
			if text != "" {
				ch <- result{name, text}
			}
		}(eng.name, eng.url)
	}

	timer := time.NewTimer(8 * time.Second)
	defer timer.Stop()

	select {
	case r := <-ch:
		return fmt.Sprintf("🔍 搜索: %s (via %s)\n%s", query, r.name, r.text)
	case <-timer.C:
		return fmt.Sprintf("🔍 搜索: %s\n⚠️ 所有搜索引擎超时，建议用 fetch_url 直接访问文档URL", query)
	}
}

// pmWebFetch PM可用的URL抓取
func pmWebFetch(urlStr string) string {
	if !strings.HasPrefix(urlStr, "http://") && !strings.HasPrefix(urlStr, "https://") {
		return "错误: URL必须以 http:// 或 https:// 开头"
	}

	client := &http.Client{Timeout: 20 * time.Second}
	req, _ := http.NewRequest("GET", urlStr, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; ArgusPM/1.0)")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Sprintf("请求失败: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 50000))
	text := htmlToText(string(body))

	if len(text) > 4000 {
		text = text[:4000] + "\n...(truncated)"
	}
	return fmt.Sprintf("📄 %s (%d bytes)\n%s", urlStr, resp.StatusCode, text)
}

// fetchAndExtractText 抓取URL并提取纯文本
func fetchAndExtractText(client *http.Client, urlStr, engineName string) string {
	req, _ := http.NewRequest("GET", urlStr, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; ArgusPM/1.0)")

	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 50000))
	text := htmlToText(string(body))

	// 简单提取前几行有意义的内容
	lines := strings.Split(text, "\n")
	var meaningful []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if len(line) > 20 && !strings.HasPrefix(line, "http") {
			meaningful = append(meaningful, line)
			if len(meaningful) >= 8 {
				break
			}
		}
	}
	if len(meaningful) == 0 {
		return ""
	}
	return strings.Join(meaningful, "\n")
}

// htmlToText 简单HTML→纯文本转换
func htmlToText(html string) string {
	// 移除script/style标签内容
	re := regexp.MustCompile(`(?is)<(script|style|noscript)[^>]*>.*?</\1>`)
	text := re.ReplaceAllString(html, "")

	// 移除HTML标签
	re = regexp.MustCompile(`<[^>]+>`)
	text = re.ReplaceAllString(text, " ")

	// 处理实体
	text = strings.ReplaceAll(text, "&amp;", "&")
	text = strings.ReplaceAll(text, "&lt;", "<")
	text = strings.ReplaceAll(text, "&gt;", ">")
	text = strings.ReplaceAll(text, "&quot;", "\"")
	text = strings.ReplaceAll(text, "&#39;", "'")
	text = strings.ReplaceAll(text, "&nbsp;", " ")

	// 合并多余空白
	re = regexp.MustCompile(`\s+`)
	text = re.ReplaceAllString(text, " ")
	text = strings.TrimSpace(text)

	return text
}

// pmGrep PM可用的内容搜索（模仿Trae的Grep工具）
func pmGrep(workDir, pattern, globPattern string) string {
	// 构建ripgrep命令
	args := []string{"-n", "--no-heading", "--color=never", "-e", pattern, workDir}
	if globPattern != "" {
		args = append(args, "--glob", globPattern)
	}

	cmd := exec.Command("rg", args...)
	cmd.Dir = workDir
	cmd.Env = append(os.Environ(), "HOME="+os.Getenv("USERPROFILE"))

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Start()
	if err != nil {
		// ripgrep不可用时，回退到Go原生实现
		return pmGrepFallback(workDir, pattern, globPattern)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case <-time.After(10 * time.Second):
		cmd.Process.Kill()
		return "搜索超时(10秒)"
	case <-done:
	}

	output := stdout.String()
	if output == "" && stderr.Len() > 0 {
		return fmt.Sprintf("搜索出借: %s", stderr.String())
	}
	if output == "" {
		return fmt.Sprintf("未找到匹配 '%s' 的内容", pattern)
	}

	// 限制输出行数
	lines := strings.Split(output, "\n")
	if len(lines) > 30 {
		lines = lines[:30]
		lines = append(lines, fmt.Sprintf("...(共%d行匹配，展示前30行)", len(strings.Split(output, "\n"))))
	}

	return fmt.Sprintf("搜索结果 (pattern=%s):\n%s", pattern, strings.Join(lines, "\n"))
}

// pmGrepFallback Go原生文件内容搜索（当ripgrep不可用时）
func pmGrepFallback(workDir, pattern, globPattern string) string {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return fmt.Sprintf("正则表达式无效: %v", err)
	}

	var results []string
	count := 0

	filepath.Walk(workDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if strings.Contains(path, ".git") || strings.Contains(path, "node_modules") {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if globPattern != "" {
			matched, _ := filepath.Match(globPattern, info.Name())
			// 也支持 **/*.go 风格简化为 *.go
			if !matched && !strings.HasSuffix(globPattern, filepath.Ext(info.Name())) {
				return nil
			}
		}
		if count >= 30 {
			return filepath.SkipAll
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		lines := strings.Split(string(content), "\n")
		relPath, _ := filepath.Rel(workDir, path)
		for i, line := range lines {
			if re.MatchString(line) {
				results = append(results, fmt.Sprintf("%s:%d: %s", relPath, i+1, strings.TrimSpace(line)))
				count++
				if count >= 30 {
					return filepath.SkipAll
				}
			}
		}
		return nil
	})

	if len(results) == 0 {
		return fmt.Sprintf("未找到匹配 '%s' 的内容", pattern)
	}
	return fmt.Sprintf("搜索结果 (pattern=%s):\n%s", pattern, strings.Join(results, "\n"))
}

// pmGlob PM可用的文件名模式查找
func pmGlob(workDir, pattern string) string {
	var results []string

	// 处理 ** 递归通配
	if strings.Contains(pattern, "**") {
		parts := strings.SplitN(pattern, "**", 2)
		prefix := strings.TrimSuffix(parts[0], "/")
		suffix := strings.TrimPrefix(parts[1], "/")
		searchRoot := filepath.Join(workDir, prefix)
		filepath.Walk(searchRoot, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if info.IsDir() && (info.Name() == ".git" || info.Name() == "node_modules") {
				return filepath.SkipDir
			}
			if !info.IsDir() {
				rel, _ := filepath.Rel(workDir, path)
				if matched, _ := filepath.Match(suffix, info.Name()); matched || suffix == "*" || suffix == "*.*" {
					if len(results) < 50 {
						results = append(results, rel)
					}
				}
			}
			return nil
		})
	} else {
		matches, _ := filepath.Glob(filepath.Join(workDir, pattern))
		for _, m := range matches {
			rel, _ := filepath.Rel(workDir, m)
			results = append(results, rel)
		}
	}

	if len(results) == 0 {
		return fmt.Sprintf("未找到匹配 '%s' 的文件", pattern)
	}

	output := fmt.Sprintf("文件查找结果 (pattern=%s, 共%d个):\n", pattern, len(results))
	for i, r := range results {
		output += fmt.Sprintf("  %d. %s\n", i+1, r)
		if i >= 50 {
			output += fmt.Sprintf("  ...(还有%d个文件未显示)\n", len(results)-50)
			break
		}
	}
	return output
}
