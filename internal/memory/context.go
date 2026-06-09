package memory

import (
	"fmt"
	"strings"
)

type ContextBuilder struct {
	mm *MemoryManager
}

func NewContextBuilder(mm *MemoryManager) *ContextBuilder {
	return &ContextBuilder{mm: mm}
}

func (cb *ContextBuilder) BuildContextForTask(taskID string, maxTokens int) (string, error) {
	var contextParts []string

	// [v0.7.2] 获取任务信息，如果不存在则创建默认上下文
	task, err := cb.getTaskInfo(taskID)
	if err != nil {
		// 数据库中没有任务记录时，返回默认上下文而非错误
		contextParts = append(contextParts, fmt.Sprintf("## 当前会话\n- 任务ID: %s\n- 状态: 活跃会话\n\n", taskID))
	} else {
		contextParts = append(contextParts, fmt.Sprintf("## 任务目标\n%s\n\n", task.Goal))
		if task.Description != "" {
			contextParts = append(contextParts, fmt.Sprintf("## 任务描述\n%s\n\n", task.Description))
		}
	}
	
	contextParts = append(contextParts, "## 待解决问题\n")
	issues, err := cb.mm.GetOpenIssues(taskID)
	if err == nil && len(issues) > 0 {
		for _, issue := range issues {
			severity := map[string]string{
				"critical": "🔴",
				"major":    "🟠",
				"minor":    "🟡",
			}[issue.Severity]
			contextParts = append(contextParts, fmt.Sprintf("- %s [%s] %s\n", severity, issue.Severity, issue.Description))
			if issue.ErrorMessage != "" {
				contextParts = append(contextParts, fmt.Sprintf("  错误信息：%s\n", issue.ErrorMessage))
			}
		}
	} else {
		contextParts = append(contextParts, "- 无\n")
	}
	
	contextParts = append(contextParts, "\n## 项目知识\n")
	knowledge, err := cb.mm.GetKnowledge(taskID, "")
	if err == nil && len(knowledge) > 0 {
		for _, k := range knowledge {
			contextParts = append(contextParts, fmt.Sprintf("- [%s] %s: %s\n", k.Category, k.Title, k.Content))
		}
	} else {
		contextParts = append(contextParts, "- 暂无\n")
	}
	
	contextParts = append(contextParts, "\n## 最近对话历史\n")
	conversations, err := cb.mm.GetConversations(taskID, 5)
	if err == nil && len(conversations) > 0 {
		for i := len(conversations) - 1; i >= 0; i-- {
			c := conversations[i]
			content := c.Content
			if len(content) > 200 {
				content = content[:200] + "..."
			}
			contextParts = append(contextParts, fmt.Sprintf("### 第 %d 轮 - %s\n%s\n\n", c.TurnNumber, c.Role, content))
		}
	} else {
		contextParts = append(contextParts, "- 暂无对话历史\n")
	}
	
	wm := cb.mm.workingMemory
	if len(wm.CurrentFiles) > 0 {
		contextParts = append(contextParts, "## 当前编辑的文件\n")
		for path := range wm.CurrentFiles {
			contextParts = append(contextParts, fmt.Sprintf("- %s\n", path))
		}
		contextParts = append(contextParts, "\n")
	}
	
	errors := wm.GetErrors()
	if len(errors) > 0 {
		contextParts = append(contextParts, "## 当前错误\n")
		for _, e := range errors {
			contextParts = append(contextParts, fmt.Sprintf("- %s\n", e))
		}
		contextParts = append(contextParts, "\n")
	}
	
	fullContext := strings.Join(contextParts, "")
	
	return fullContext, nil
}

type TaskInfo struct {
	ID          string
	Goal        string
	Description string
	Status      string
	TotalTurns  int
	TotalChanges int
}

func (cb *ContextBuilder) getTaskInfo(taskID string) (*TaskInfo, error) {
	var task TaskInfo
	err := cb.mm.db.QueryRow(`
		SELECT id, goal, description, status, total_turns, total_changes
		FROM tasks WHERE id = ?
	`, taskID).Scan(&task.ID, &task.Goal, &task.Description, &task.Status, &task.TotalTurns, &task.TotalChanges)
	
	if err != nil {
		return nil, err
	}
	
	return &task, nil
}
