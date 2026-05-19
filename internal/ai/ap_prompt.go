package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"argus/internal/executor"
	"argus/internal/git"
)

// APPrompt AP系统提示词
const APPrompt = `你是Argus的审批者(Approver/AP)，负责对已完成的任务进行最终质量把关。

⚠️ 你的身份：独立的 QA 工程师 + Code Reviewer
- 你是最终质量关卡，拥有否决权
- 你需要独立审查，不受PM或SE意见影响
- 你的否决权是最终的：如果你说不行，项目就不能结束

当前工作目录: %s

🎯 核心职责（必须遵守）：

1. **Code Review（代码审查）**：
   - 检查代码正确性、安全性、可维护性
   - 检查是否满足用户需求
   - 检查是否有潜在的bug或边界问题
   - 检查代码风格和最佳实践

2. **QA验证（质量保证）**：
   - 亲自运行测试、编译，验证功能
   - 不轻信PM或SE的报告，**眼见为实**
   - 用工具验证所有关键点

3. **审批决策**：
   - ✅ 通过：代码质量达标，需求满足，测试通过
   - ❌ 不通过：存在问题需要SE修改

🚫 绝对禁止：
- ❌ 不调用任何工具就直接说"通过"
- ❌ 只看文字汇报不做实际验证
- ❌ 因为小问题就反复拒绝（区分致命问题和优化建议）

✅ 审批流程（强制执行）：

第一步：用 list_files 查看SE创建了哪些文件
第二步：用 read_file 读取关键文件内容，检查代码质量
第三步：用 exec 运行编译/测试命令验证功能
第四步：综合分析后给出审批结论

⚠️ 审批结论格式（必须输出JSON）：

如果通过：
@USR ✅ 项目审批通过
{"approval_result":"approve","reason":"代码质量符合要求，测试验证通过","issues_found":0,"files_reviewed":["文件1","文件2"]}

如果不通过：
@SE 请修改以下问题：[具体问题描述]
{"approval_result":"reject","reason":"具体问题描述","issues_found":1,"critical_issues":["问题1详情"]}

你有这些工具可用：read_file(读文件)、list_files(列文件)、exec(执行命令)

重要规则：
- 必须使用工具验证后才能给出结论
- 如果发现致命问题（如功能错误、安全漏洞），必须拒绝
- 对于非致命问题（如命名风格、注释缺失），可以指出但不应阻塞项目
- 审核最多3轮工具调用
- 保持客观专业，不要与PM的意见对立，而是补充和加强
`

// APProcessor AP处理器
type APProcessor struct {
	client         *Client
	workDir        string
	systemPrompt   string
	terminalWriter func(string) error
	executor       *executor.Executor
	ReplyLanguage  string
	ctx            context.Context
}

// NewAPProcessor 创建AP处理器
func NewAPProcessor(client *Client, workDir string) *APProcessor {
	return &APProcessor{
		client:       client,
		workDir:      workDir,
		systemPrompt: fmt.Sprintf(APPrompt, workDir),
		executor:     executor.NewExecutor(workDir, nil),
	}
}

// SetTerminalWriter 设置终端写入器
func (p *APProcessor) SetTerminalWriter(writer func(string) error) {
	p.terminalWriter = writer
}

// SetContext 设置上下文
func (p *APProcessor) SetContext(ctx context.Context) {
	p.ctx = ctx
}

// getCtx 获取上下文，nil 时返回 Background
func (p *APProcessor) getCtx() context.Context {
	if p.ctx == nil {
		return context.Background()
	}
	return p.ctx
}

func (p *APProcessor) getSystemPrompt() string {
	return p.systemPrompt
}

// APTools AP可用的工具列表
var APTools = []Tool{
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "read_file",
			Description: "读取文件内容，用于Code Review审核代码质量。可以读任何文件来验证SE产出的代码是否正确。",
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
				"type": "object",
				"properties": map[string]interface{}{},
			},
		},
	},
	{
		Type: "function",
		Function: ToolFunction{
			Name:        "exec",
			Description: "执行命令用于QA验证。运行编译、测试等命令来验证代码是否正确工作。超时30秒。",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"command": map[string]interface{}{
						"type":        "string",
						"description": "要执行的命令（如 go build, npm test, python test.py 等）",
					},
				},
				"required": []string{"command"},
			},
		},
	},
}

// APResponse AP响应
type APResponse struct {
	Content    string // AP的审批意见
	Approved   bool   // 是否通过审批
	NeedRework bool   // 是否需要返工
	ReworkMsg  string // 返工原因（如果需要）
}

// ProcessReview AP审核处理（带工具调用能力）
func (p *APProcessor) ProcessReview(reviewMsg string, history []ChatMessage, onChunk func(delta string)) (*APResponse, error) {
	fmt.Printf("[AP Review] 开始审核: %s\n", reviewMsg[:min(100, len(reviewMsg))])

	aiHistory := make([]Message, 0, len(history))
	for _, msg := range history {
		if msg.Role == "user" {
			aiHistory = append(aiHistory, Message{Role: "user", Content: msg.Content})
		} else if msg.Role == "pm" {
			aiHistory = append(aiHistory, Message{Role: "assistant", Content: msg.Content})
		} else if msg.Role == "se" {
			aiHistory = append(aiHistory, Message{Role: "user", Content: msg.Content})
		} else if msg.Role == "ap" {
			aiHistory = append(aiHistory, Message{Role: "assistant", Content: msg.Content})
		}
	}
	const maxAPHistory = 20
	const maxHistoryChars = 8000
	if len(aiHistory) > maxAPHistory {
		fmt.Printf("[AP Review] ⚠️ history截断 %d→%d条\n", len(aiHistory), maxAPHistory)
		aiHistory = aiHistory[len(aiHistory)-maxAPHistory:]
	}
	totalChars := 0
	cutIdx := len(aiHistory)
	for i := len(aiHistory) - 1; i >= 0; i-- {
		totalChars += len(aiHistory[i].Content)
		if totalChars > maxHistoryChars && i < cutIdx {
			cutIdx = i
		}
	}
	if cutIdx > 0 {
		fmt.Printf("[AP Review] ⚠️ history字符截断 %d→%d条(总字符%d)\n", len(aiHistory), len(aiHistory[cutIdx:]), totalChars)
		aiHistory = aiHistory[cutIdx:]
	}

	maxToolRounds := 3
	var finalContent string
	toolsSupported := true

	for round := 0; round < maxToolRounds; round++ {
		if toolsSupported {
			fmt.Printf("[AP ChatWithTools] 调用 AI，round=%d\n", round+1)
			resp, err := p.client.ChatWithTools(p.getCtx(), p.getSystemPrompt(), aiHistory, reviewMsg, APTools)
			if err != nil {
				errStr := err.Error()
				if strings.Contains(errStr, "tool choice") || strings.Contains(errStr, "tool_choice") ||
					strings.Contains(errStr, "enable-auto-tool-choice") || strings.Contains(errStr, "tool-call-parser") {
					fmt.Printf("[AP ChatWithTools] ⚠️ 模型不支持工具调用，降级为普通Chat: %v\n", err)
					toolsSupported = false
				} else {
					fmt.Printf("[AP ChatWithTools] ❌ error=%v\n", err)
					return nil, fmt.Errorf("AP review failed: %w", err)
				}
			} else {
				if len(resp.Choices) == 0 {
					return nil, fmt.Errorf("empty response from AP AI")
				}
				msg := resp.Choices[0].Message
				finalContent = msg.Content
				if len(msg.ToolCalls) == 0 {
					fmt.Printf("[AP ChatWithTools] ✅ 无工具调用，round=%d 直接出结论\n", round+1)
					break
				}
				fmt.Printf("[AP ChatWithTools] 🔧 调用了 %d 个工具\n", len(msg.ToolCalls))
				aiHistory = append(aiHistory, Message{Role: "user", Content: reviewMsg})
				aiHistory = append(aiHistory, msg)
				for _, tc := range msg.ToolCalls {
					toolResult := p.executeTool(tc.Function.Name, tc.Function.Arguments)
					aiHistory = append(aiHistory, Message{
						Role:       "tool",
						Content:    toolResult,
						ToolCallID: tc.ID,
					})
				}
				reviewMsg = "[工具结果已返回，请继续审核并给出最终结论]"
				continue
			}
		}

		fmt.Printf("[AP ChatStream] 降级模式，round=%d\n", round+1)
		content, err := p.client.ChatStream(p.getCtx(), p.getSystemPrompt(), aiHistory, reviewMsg, p.ReplyLanguage, onChunk)
		if err != nil {
			fmt.Printf("[AP ChatStream] ❌ error=%v\n", err)
			return nil, fmt.Errorf("AP review failed: %w", err)
		}
		finalContent = content
		break
	}

	// 用 onChunk 逐字输出最终结论（前端流式展示）
	if onChunk != nil && finalContent != "" {
		for _, ch := range finalContent {
			onChunk(string(ch))
		}
	}

	result := p.parseApprovalResult(finalContent)
	return result, nil
}

// ProcessReviewNoTools AP审核处理（无工具调用，用于不支持工具的模型降级）
func (p *APProcessor) ProcessReviewNoTools(reviewMsg string, history []ChatMessage, onChunk func(delta string)) (*APResponse, error) {
	fmt.Printf("[AP ReviewNoTools] 开始无工具审核: %s\n", reviewMsg[:min(100, len(reviewMsg))])

	aiHistory := make([]Message, 0, len(history))
	for _, msg := range history {
		if msg.Role == "user" {
			aiHistory = append(aiHistory, Message{Role: "user", Content: msg.Content})
		} else if msg.Role == "pm" {
			aiHistory = append(aiHistory, Message{Role: "assistant", Content: msg.Content})
		} else if msg.Role == "se" {
			aiHistory = append(aiHistory, Message{Role: "user", Content: msg.Content})
		} else if msg.Role == "ap" {
			aiHistory = append(aiHistory, Message{Role: "assistant", Content: msg.Content})
		}
	}
	const maxAPHistory = 20
	const maxHistoryChars = 8000
	if len(aiHistory) > maxAPHistory {
		fmt.Printf("[AP ReviewNoTools] ⚠️ history截断 %d→%d条\n", len(aiHistory), maxAPHistory)
		aiHistory = aiHistory[len(aiHistory)-maxAPHistory:]
	}
	totalChars := 0
	cutIdx := len(aiHistory)
	for i := len(aiHistory) - 1; i >= 0; i-- {
		totalChars += len(aiHistory[i].Content)
		if totalChars > maxHistoryChars && i < cutIdx {
			cutIdx = i
		}
	}
	if cutIdx > 0 {
		fmt.Printf("[AP ReviewNoTools] ⚠️ history字符截断 %d→%d条(总字符%d)\n", len(aiHistory), len(aiHistory[cutIdx:]), totalChars)
		aiHistory = aiHistory[cutIdx:]
	}

	fmt.Printf("[AP ReviewNoTools] 调用 ChatStream...\n")
	content, err := p.client.ChatStream(p.getCtx(), p.getSystemPrompt(), aiHistory, reviewMsg, p.ReplyLanguage, onChunk)
	if err != nil {
		fmt.Printf("[AP ReviewNoTools] ❌ error=%v\n", err)
		return nil, fmt.Errorf("AP no-tools review failed: %w", err)
	}
	fmt.Printf("[AP ReviewNoTools] ✅ 完成，长度: %d\n", len(content))

	result := p.parseApprovalResult(content)
	return result, nil
}

// executeTool 执行AP的工具调用
func (p *APProcessor) executeTool(name string, argsJSON string) string {
	fmt.Printf("[AP Tool] 执行: %s\n", name)

	switch name {
	case "read_file":
		var args struct {
			Path string `json:"path"`
		}
		if err := jsonUnmarshal(argsJSON, &args); err != nil {
			return fmt.Sprintf("参数解析失败: %v", err)
		}
		content, err := p.executor.ReadFile(args.Path)
		if err != nil {
			return fmt.Sprintf("读取文件失败 [%s]: %v", args.Path, err)
		}
		return content

	case "list_files":
		files, err := p.executor.ListFiles()
		if err != nil {
			return fmt.Sprintf("列出文件失败: %v", err)
		}
		var fileNames []string
		for _, f := range files {
			fileNames = append(fileNames, f.Path)
		}
		return strings.Join(fileNames, "\n")

	case "exec":
		var args struct {
			Command string `json:"command"`
		}
		if err := jsonUnmarshal(argsJSON, &args); err != nil {
			return fmt.Sprintf("参数解析失败: %v", err)
		}
		output, err := p.executor.Exec(args.Command, 30*time.Second)
		if err != nil {
			return fmt.Sprintf("执行失败: %v\n输出:\n%s", err, output)
		}
		return fmt.Sprintf("执行成功\n输出:\n%s", output)

	default:
		return fmt.Sprintf("未知工具: %s", name)
	}
}

// parseApprovalResult 解析AP的审批结果（优先JSON，fallback关键词）
func (p *APProcessor) parseApprovalResult(content string) *APResponse {
	result := &APResponse{
		Content: content,
	}

	jsonIdx := strings.Index(content, `{"approval_result"`)
	if jsonIdx != -1 {
		jsonEnd := strings.Index(content[jsonIdx:], "}") + jsonIdx
		if jsonEnd > jsonIdx {
			jsonStr := content[jsonIdx : jsonEnd+1]
			var jsonResult struct {
				Result       string   `json:"approval_result"`
				Reason       string   `json:"reason"`
				IssuesFound  int      `json:"issues_found"`
				CriticalIssues []string `json:"critical_issues"`
			}
			if err := json.Unmarshal([]byte(jsonStr), &jsonResult); err == nil {
				if jsonResult.Result == "approve" {
					result.Approved = true
					result.NeedRework = false
					return result
				} else if jsonResult.Result == "reject" {
					result.Approved = false
					result.NeedRework = true
					result.ReworkMsg = jsonResult.Reason
					return result
				}
			}
		}
	}

	contentLower := strings.ToLower(content)

	checkPatterns := []struct {
		patterns []string
		isReject bool
	}{
		{
			patterns: []string{"不通过", "未通过", "❌", "拒绝", "驳回", "需要修改", "有问题", "缺陷", "错误", "bug", "failed", "reject", "need fix", "need to change"},
			isReject: true,
		},
		{
			patterns: []string{"通过", "✅", "批准", "同意", "认可", "approved", "pass", "ok", "没有问题", "质量达标"},
			isReject: false,
		},
	}

	for _, check := range checkPatterns {
		for _, pattern := range check.patterns {
			if strings.Contains(contentLower, strings.ToLower(pattern)) {
				if check.isReject {
					result.Approved = false
					result.NeedRework = true
					result.ReworkMsg = content
				} else {
					result.Approved = true
					result.NeedRework = false
				}
				return result
			}
		}
	}

	result.Approved = false
	result.NeedRework = false
	return result
}

// jsonUnmarshal 简单的JSON解析辅助函数
func jsonUnmarshal(jsonStr string, target interface{}) error {
	return json.Unmarshal([]byte(jsonStr), target)
}

// autoVerify AP不调工具时的兜底验证
func (p *APProcessor) autoVerify() string {
	statusEntries := git.GetStatus(p.workDir)
	if statusEntries == nil {
		return "无法获取git状态信息"
	}

	if len(statusEntries) == 0 {
		return "git status无变更"
	}

	var result strings.Builder
	result.WriteString("📋 [自动检测] 代码变更信息：\n\n")
	for _, entry := range statusEntries {
		path, _ := entry["path"].(string)
		status, _ := entry["status"].(string)
		result.WriteString(fmt.Sprintf("- %s (%s)\n", path, status))
	}
	return result.String()
}
