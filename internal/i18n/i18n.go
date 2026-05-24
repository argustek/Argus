package i18n

import (
	"fmt"
	"sync"
)

var (
	currentLang = "zh-CN"
	mu          sync.RWMutex
)

type LangMap map[string]string

var translations = map[string]LangMap{
	"zh-CN": {
		"err.se_failed":              "❌ SE执行失败: %v",
		"err.se_recover_failed":      "❌ SE执行失败且无法恢复: %v (原始错误: %v)",
		"err.pm_review_failed":       "❌ PM审核失败: %v",
		"err.task_cannot_continue":   "任务无法继续，请检查后重试。",
		"err.pm_api_network":        "请检查API配置或网络连接后重试。",
		"err.memory_serialize":       "序列化任务记忆失败：%v",
		"err.memory_mkdir":           "创建目录失败：%v",
		"err.memory_write":           "写入任务记忆文件失败：%v",
		"err.memory_read":            "读取任务记忆文件失败：%v",
		"err.memory_parse":           "解析任务记忆文件失败：%v",
		"err.memory_delete":          "删除任务记忆文件失败：%v",
		"err.git_log_failed":         "获取日志失败: %v",
		"err.git_head_failed":        "获取HEAD失败: %v",
		"err.git_branch_failed":      "获取分支失败: %v",
		"err.git_open_repo":          "打开仓库失败: %v",
		"err.git_worktree":           "获取工作区失败: %v",
		"err.git_checkout_failed":    "切换分支失败: %v",
		"err.git_create_branch":      "创建分支失败: %v",
		"err.dingtalk_config":        "钉钉配置未加载",
		"err.dingtalk_token":         "获取 AccessToken 失败: %v",
		"err.dingtalk_read_resp":     "读取响应失败: %v",
		"err.dingtalk_parse_resp":    "解析响应失败: %v",
		"err.dingtalk_token_err":     "获取 AccessToken 错误: %s",
		"err.dingtalk_build_req":     "构建请求体失败: %v",
		"err.dingtalk_create_req":    "创建请求失败: %v",
		"err.dingtalk_send_req":      "发送消息请求失败: %v",
		"err.dingtalk_api_error":     "钉钉错误: errcode=%d, errmsg=%s",
		"err.dingtalk_http_error":    "发送消息失败: HTTP %d - %s",
		"err.api_retry_failed":       "API调用失败(已重试%d次): %w",
		"err.git_add_remote":         "添加远程失败: %v",
		"err.git_remove_remote":      "删除远程失败: %v",
		"err.git_not_repo":           "当前目录不是 Git 仓库，请先初始化",
		"err.git_no_branch":          "无法获取当前分支名，请先创建提交",
		"err.git_push_failed":        "推送失败: %v",
		"err.git_pull_failed":        "拉取失败: %v",
		"err.git_fetch_remote":       "获取远程引用失败: %v",
		"err.git_merge_failed":       "合并失败: %v",
		"err.git_stage_failed":       "暂存失败: %v",
		"err.git_unstage_failed":     "取消暂存失败: %v",
		"err.git_commit_failed":      "提交失败: %v",
		"err.git_restore_failed":     "恢复文件失败: %v",
		"err.git_discard_failed":     "丢弃所有更改失败: %v",
		"err.git_init_failed":        "初始化仓库失败: %v",
		"err.git_remote_not_exist":   "远程仓库 %s 不存在: %v",
		"err.tool_record_failed":    "工具记录失败: %s",
		"msg.se_needs_help":         "⚠️ SE需要帮助，已转交PM处理: %s",
		"msg.ap_approved":           "✅ AP审批通过",
		"msg.ap_retry_exceeded":     "⚠️ AP与PM沟通次数过多，项目强制结束",
		"msg.ap_rework_exceeded":    "⚠️ AP多次要求返工，请人工介入",
		"msg.monitor_force_approve": "🔴⚠️ C强制通过（非正常AP审批结果）",
		"msg.task_complete":          "✅ 任务已完成",
		"msg.task_failed":            "❌ 任务执行失败",
		"msg.task_se_recover_failed": "❌ SE恢复失败",
		"msg.review_timeout":         "⚠️ 审核轮次过多，请手动确认任务是否完成",
		"msg.se_progress":            "⚙️ 已执行操作: %d 个",
	},
	"en-US": {
		"err.se_failed":              "❌ SE execution failed: %v",
		"err.se_recover_failed":      "❌ SE failed and cannot recover: %v (original error: %v)",
		"err.pm_review_failed":       "❌ PM review failed: %v",
		"err.task_cannot_continue":   "Task cannot continue, please check and retry.",
		"err.pm_api_network":        "Please check API configuration or network connection.",
		"err.memory_serialize":       "Failed to serialize task memory: %v",
		"err.memory_mkdir":           "Failed to create directory: %v",
		"err.memory_write":           "Failed to write task memory file: %v",
		"err.memory_read":            "Failed to read task memory file: %v",
		"err.memory_parse":           "Failed to parse task memory file: %v",
		"err.memory_delete":          "Failed to delete task memory file: %v",
		"err.git_log_failed":         "Failed to get log: %v",
		"err.git_head_failed":        "Failed to get HEAD: %v",
		"err.git_branch_failed":      "Failed to get branch: %v",
		"err.git_open_repo":          "Failed to open repo: %v",
		"err.git_worktree":           "Failed to get worktree: %v",
		"err.git_checkout_failed":    "Failed to checkout: %v",
		"err.git_create_branch":      "Failed to create branch: %v",
		"err.dingtalk_config":        "DingTalk config not loaded",
		"err.dingtalk_token":         "Failed to get AccessToken: %v",
		"err.dingtalk_read_resp":     "Failed to read response: %v",
		"err.dingtalk_parse_resp":    "Failed to parse response: %v",
		"err.dingtalk_token_err":     "AccessToken error: %s",
		"err.dingtalk_build_req":     "Failed to build request: %v",
		"err.dingtalk_create_req":    "Failed to create request: %v",
		"err.dingtalk_send_req":      "Failed to send request: %v",
		"err.dingtalk_api_error":     "DingTalk error: errcode=%d, errmsg=%s",
		"err.dingtalk_http_error":    "Send message failed: HTTP %d - %s",
		"err.api_retry_failed":       "API call failed (retried %d times): %w",
		"err.git_add_remote":         "Failed to add remote: %v",
		"err.git_remove_remote":      "Failed to remove remote: %v",
		"err.git_not_repo":           "Not a git repository, please initialize first",
		"err.git_no_branch":          "Cannot get current branch name, please create a commit first",
		"err.git_push_failed":        "Push failed: %v",
		"err.git_pull_failed":        "Pull failed: %v",
		"err.git_fetch_remote":       "Failed to fetch remote refs: %v",
		"err.git_merge_failed":       "Merge failed: %v",
		"err.git_stage_failed":       "Stage failed: %v",
		"err.git_unstage_failed":     "Unstage failed: %v",
		"err.git_commit_failed":      "Commit failed: %v",
		"err.git_restore_failed":     "Restore file failed: %v",
		"err.git_discard_failed":     "Discard all changes failed: %v",
		"err.git_init_failed":        "Init repository failed: %v",
		"err.git_remote_not_exist":   "Remote %s does not exist: %v",
		"err.tool_record_failed":    "Tool record failed: %s",
		"msg.se_needs_help":         "⚠️ SE needs help, forwarded to PM: %s",
		"msg.ap_approved":           "✅ AP approved",
		"msg.ap_retry_exceeded":     "⚠️ Too many AP-PM communications, project force-ended",
		"msg.ap_rework_exceeded":    "⚠️ AP requested rework too many times, manual intervention needed",
		"msg.monitor_force_approve": "🔴⚠️ C forced approval (abnormal AP result)",
		"msg.task_complete":          "✅ Task completed",
		"msg.task_failed":            "❌ Task execution failed",
		"msg.task_se_recover_failed": "❌ SE recovery failed",
		"msg.review_timeout":         "⚠️ Too many review rounds, please manually confirm task status",
		"msg.se_progress":            "⚙️ Executed %d operations",
	},
}

func SetLang(lang string) {
	mu.Lock()
	defer mu.Unlock()
	if _, ok := translations[lang]; ok {
		currentLang = lang
	}
}

func GetLang() string {
	mu.RLock()
	defer mu.RUnlock()
	return currentLang
}

func T(key string, args ...interface{}) string {
	mu.RLock()
	defer mu.RUnlock()

	if langMap, ok := translations[currentLang]; ok {
		if msg, exists := langMap[key]; exists {
			return fmt.Sprintf(msg, args...)
		}
	}

	if langMap, ok := translations["zh-CN"]; ok {
		if msg, exists := langMap[key]; exists {
			return fmt.Sprintf(msg, args...)
		}
	}

	return key
}
