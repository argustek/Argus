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

// PMPrompt is the PM agent's core behavioral prompt.
// Keep this short — the model reads it every turn.
// Language is injected at runtime by GetLanguageInstruction().
const PMPrompt = `You are Argus PM — an autonomous project manager that uses tools to get things done.

Working directory: %s

=== IDENTITY ===
You are both Project Manager and QA engineer. Your job: understand what the user wants and get it done efficiently. You have two modes: execute simple tasks directly yourself, or delegate complex work to the Software Engineer (SE).

=== FIRST PRINCIPLES ===
1. Always use tools — never respond with just text unless it's a greeting. Every turn must call at least one tool.
2. Search before asking — if something is unclear, use list_files/grep/find_files/web_search to gather context first. Only @USR as last resort, with specific options.
3. Concise and direct — report results, don't add suggestions unless asked. Don't explain trivial code. Don't ask "shall I continue".

=== DECISION TREE ===
User message
  ├─ greeting/chat/thanks → @USR <reply>
  │
  ├─ unclear/ambiguous → use tools to investigate (list_files/grep/search),
  │     then @USR <question with options> if still unclear
  │
  ├─ simple task (you can finish in one round of tool calls) → EXECUTE DIRECTLY
  │   Examples:
  │   • write/edit code → write_file/edit_file + exec to verify
  │   • clean up / organize files → delete_file / exec rm / list_files
  │   • system operations (disk, process, network checks) → exec
  │   • search information → grep / web_search / read_file
  │   • document conversion / processing → appropriate tool
  │   Result format: ✅ tool_name args (summary), no extra commentary
  │
  └─ complex task (multi-step, needs analysis) → @SE <task breakdown>
       After SE completes → verify with tools → @AP for final approval

=== COMMUNICATION ===
@SE <task> — assign work to Software Engineer
@AP <result> — submit for final approval after verification
@USR <message> — talk to the user (questions, status, results)
One @ per message maximum.

=== ANTI-LOOP ===
- If SE completes a task, do not re-assign the same task to SE
- If a tool errors twice on the same input, try a different approach, not a retry
- If you can't make progress after 3 attempts, @USR <what happened + what you tried>
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
	todoAdder      func(string) string  // 添加待办
	todoUpdater    func(string, string) // 更新待办状态
	todoClearer    func()               // 清空待办（replace=true时）

	shellEmitter     types.ShellEventEmitter // 三层模型 Shell 事件推送（可选）
	currentTaskId    string                  // 当前 TaskList ID
	currentTaskIndex int                     // 当前执行到的步骤索引
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

// getSystemPrompt 获取完整的System Prompt（核心 + 执行规则 + 时间上下文）
func (p *PMProcessor) getSystemPrompt() string {
	base := p.systemPrompt + "\n\n" + PMRules
	if p.timeContext != "" {
		return base + "\n\n" + p.timeContext
	}
	return base
}

// pmIsReadTool 判断PM工具是否只读（可并行执行）
func pmIsReadTool(name string) bool {
	switch name {
	case "read_file", "list_files", "web_search", "fetch_url",
		"grep_content", "find_files":
		return true
	}
	return false
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
			Description: "执行命令用于QA验证。当SE汇报'编译成功'或'测试通过'时，你必须用此工具亲自验证！例如：go build, go run xxx.go, npm test, python xxx.py 等。超时60秒。",
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
			Description: "按文件名模式查找文件（支持通配符如 **/*.go, src/**/*.ts, **/hello.go）。当 read_file 找不到文件或路径不确定时，先用此工具搜索！可用于了解项目中有哪些文件。",
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
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "write_file",
			Description: "创建或覆写文件。用于 PM 直接执行任务时写代码/文档。会创建不存在的父目录。",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "文件路径（相对于工作目录）",
					},
					"content": map[string]interface{}{
						"type":        "string",
						"description": "文件内容",
					},
				},
				"required": []string{"path", "content"},
			},
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "edit_file",
			Description: "编辑已存在的文件，用新字符串替换旧字符串。如果 oldString 出现多次，可通过 occurrence 参数指定替换第几次出现的（从1开始）。不传 occurrence 则替换所有。",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "文件路径（相对于工作目录）",
					},
					"old_string": map[string]interface{}{
						"type":        "string",
						"description": "要被替换的旧字符串（必须唯一匹配，否则报错）",
					},
					"new_string": map[string]interface{}{
						"type":        "string",
						"description": "替换后的新字符串",
					},
				},
				"required": []string{"path", "old_string", "new_string"},
			},
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "delete_file",
			Description: "删除文件或空目录。注意：只能删除文件或空目录，不能递归删除非空目录。",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "要删除的文件或空目录路径（相对于工作目录）",
					},
				},
				"required": []string{"path"},
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
	maxToolRounds := 8
	var finalContent string
	var hasToolCalls bool // [v0.8] 记录是否有ToolCalls
	toolNagCount := 0     // [v0.8.4] 记录连续ToolCalls=0次数

	for round := 0; round < maxToolRounds; round++ {
		callCtx, callCancel := context.WithTimeout(p.getCtx(), 120*time.Second)
		resp, err := p.client.ChatWithTools(callCtx, p.getSystemPrompt(), aiHistory, userInput, PMTools, p.ReplyLanguage)
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

		// [v0.8.4] 没有工具调用 → 判断是否应结束
		// 如果之前已经有 ToolCalls 执行过，说明任务已完成，直接结束
		// 如果从未有过 ToolCalls，可能 LLM 在纯文本回复，提醒一次
		if len(msg.ToolCalls) == 0 {
			if !hasToolCalls && toolNagCount == 0 {
				toolNagCount++
				nagMsg := "[系统提示] ⚠️ 你没有调用任何工具就直接回复了。用户请求可能需要你调用工具来处理（write_file/exec/read_file等）。请重新分析：这是否属于可以直接执行的任务？如果是，请直接调用工具完成；如果不是，请 @SE 分配。"
				aiHistory = append(aiHistory, Message{Role: "user", Content: userInput})
				aiHistory = append(aiHistory, msg)
				aiHistory = append(aiHistory, Message{Role: "user", Content: nagMsg, ToolCallID: "tool_nag_1"})
				userInput = "[请分析是否需要调用工具执行任务]"
				continue
			}
			// 已有 ToolCalls（任务已执行）或已提醒过 → 结束
			break
		}

		// [v0.8] 记录PM是否有ToolCalls（用于Featherweight分流判断）
		hasToolCalls = true

		// [v0.8.5] 读工具并行执行，写工具串行执行
		aiHistory = append(aiHistory, Message{Role: "user", Content: userInput})
		aiHistory = append(aiHistory, msg)

		var readTools, writeTools []ToolCall
		for _, tc := range msg.ToolCalls {
			if pmIsReadTool(tc.Function.Name) {
				readTools = append(readTools, tc)
			} else {
				writeTools = append(writeTools, tc)
			}
		}

		if len(readTools) > 0 {
			type readRes struct {
				index int
				tc    ToolCall
				res   string
			}
			ch := make(chan readRes, len(readTools))
			for i, tc := range readTools {
				go func(i int, tc ToolCall) {
					r := p.executeTool(tc.Function.Name, tc.Function.Arguments)
					ch <- readRes{i, tc, r}
				}(i, tc)
			}
			results := make([]readRes, len(readTools))
			for range readTools {
				r := <-ch
				results[r.index] = r
			}
			for _, r := range results {
				if onChunk != nil {
					onChunk(fmt.Sprintf("🔧 **调用工具**: `%s`\n", r.tc.Function.Name))
				}
				aiHistory = append(aiHistory, Message{
					Role: "tool", Content: r.res, ToolCallID: r.tc.ID,
				})
			}
		}

		for _, tc := range writeTools {
			if onChunk != nil {
				onChunk(fmt.Sprintf("🔧 **调用工具**: `%s`\n", tc.Function.Name))
			}
			toolResult := p.executeTool(tc.Function.Name, tc.Function.Arguments)
			aiHistory = append(aiHistory, Message{
				Role: "tool", Content: toolResult, ToolCallID: tc.ID,
			})
		}

		// 收集工具执行结果，追加到 finalContent，不再循环调 LLM 总结
		// 工具结果本身已包含 ✅/❌ 标记，直接展示即可
		var toolOutputs []string
		for j := len(aiHistory) - 1; j >= 0; j-- {
			if aiHistory[j].Role == "tool" {
				toolOutputs = append(toolOutputs, aiHistory[j].Content)
			}
		}
		// 反转成时间顺序
		for i, j := 0, len(toolOutputs)-1; i < j; i, j = i+1, j-1 {
			toolOutputs[i], toolOutputs[j] = toolOutputs[j], toolOutputs[i]
		}
		if len(toolOutputs) > 0 {
			resultsText := strings.Join(toolOutputs, "\n")
			if finalContent != "" {
				finalContent += "\n" + resultsText
			} else {
				finalContent = resultsText
			}
		}
		hasToolCalls = true
		// 不循环回 LLM — 工具结果已可直接展示
		// 对于需要多轮工具调用的复杂任务，由 ProcessStream 调用方在上下文里处理
		break
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
		resp, err := p.client.ChatWithTools(callCtx, p.getSystemPrompt(), aiHistory, reviewMsg, PMTools, p.ReplyLanguage)
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

		// [v0.8.5] 读工具并行，写工具串行
		var readTools, writeTools []ToolCall
		for _, tc := range msg.ToolCalls {
			if pmIsReadTool(tc.Function.Name) {
				readTools = append(readTools, tc)
			} else {
				writeTools = append(writeTools, tc)
			}
		}

		if len(readTools) > 0 {
			type readRes struct {
				index int
				tc    ToolCall
				res   string
			}
			ch := make(chan readRes, len(readTools))
			for i, tc := range readTools {
				go func(i int, tc ToolCall) {
					ch <- readRes{i, tc, p.executeTool(tc.Function.Name, tc.Function.Arguments)}
				}(i, tc)
			}
			results := make([]readRes, len(readTools))
			for range readTools {
				r := <-ch
				results[r.index] = r
			}
			for _, r := range results {
				if onChunk != nil {
					onChunk(fmt.Sprintf("🔧 **调用工具**: `%s`\n", r.tc.Function.Name))
				}
				if onChunk != nil {
					preview := r.res
					if len(preview) > 200 {
						preview = preview[:200] + "..."
					}
					onChunk(fmt.Sprintf("✅ **工具结果** (%d bytes):\n```\n%s\n```\n\n", len(r.res), preview))
				}
				aiHistory = append(aiHistory, Message{
					Role: "tool", Content: r.res, ToolCallID: r.tc.ID,
				})
			}
		}

		for _, tc := range writeTools {
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
	// [FIX-v0.8.1] 工具执行 panic 保护 — 防止 web_search 等工具崩溃导致进程闪崩
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("[executeTool] ⚠️ 工具 %s 执行 panic: %v\n", name, r)
		}
	}()

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
				name := e.Name()
				if e.IsDir() {
					name += "/"
				}
				result = append(result, name)
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
			name := e.Name()
			if e.IsDir() {
				name += "/"
			}
			result = append(result, name)
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
		case <-time.After(60 * time.Second):
			cmd.Process.Kill()
			result := fmt.Sprintf("命令执行超时(60秒): %s\n已终止进程", args.Command)
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
	case "write_file":
		var args struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		json.Unmarshal([]byte(argsJSON), &args)
		fullPath := filepath.Join(p.workDir, args.Path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			return fmt.Sprintf("创建目录失败: %v", err)
		}
		if err := os.WriteFile(fullPath, []byte(args.Content), 0644); err != nil {
			return fmt.Sprintf("写文件失败: %v", err)
		}
		return fmt.Sprintf("✅ 文件已创建: %s (%d bytes)", args.Path, len(args.Content))
	case "edit_file":
		var args struct {
			Path      string `json:"path"`
			OldString string `json:"old_string"`
			NewString string `json:"new_string"`
		}
		json.Unmarshal([]byte(argsJSON), &args)
		fullPath := filepath.Join(p.workDir, args.Path)
		content, err := os.ReadFile(fullPath)
		if err != nil {
			return fmt.Sprintf("读取文件失败: %v", err)
		}
		newContent := strings.ReplaceAll(string(content), args.OldString, args.NewString)
		if string(content) == newContent {
			return fmt.Sprintf("编辑文件失败: 未找到匹配的旧字符串")
		}
		if err := os.WriteFile(fullPath, []byte(newContent), 0644); err != nil {
			return fmt.Sprintf("写文件失败: %v", err)
		}
		return fmt.Sprintf("✅ 文件已编辑: %s", args.Path)
	case "delete_file":
		var args struct {
			Path string `json:"path"`
		}
		json.Unmarshal([]byte(argsJSON), &args)
		fullPath := filepath.Join(p.workDir, args.Path)
		if err := os.Remove(fullPath); err != nil {
			return fmt.Sprintf("删除失败: %v", err)
		}
		return fmt.Sprintf("✅ 已删除: %s", args.Path)
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
			defer func() {
				if r := recover(); r != nil {
					fmt.Printf("[web_search] ⚠️ %s 搜索 panic: %v\n", name, r)
				}
			}()
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
