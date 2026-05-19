package i18n

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestI18n_SwitchLanguage(t *testing.T) {
	tests := []struct {
		lang     string
		key      string
		contains string
	}{
		{"zh-CN", "err.se_failed", "SE执行失败"},
		{"en-US", "err.se_failed", "SE execution failed"},
		{"zh-CN", "msg.task_complete", "任务已完成"},
		{"en-US", "msg.task_complete", "Task completed"},
		{"zh-CN", "msg.review_timeout", "审核轮次过多"},
		{"en-US", "msg.review_timeout", "review rounds"},
		{"zh-CN", "err.dingtalk_token", "AccessToken"},
		{"en-US", "err.dingtalk_token", "AccessToken"},
	}

	for _, tt := range tests {
		t.Run(tt.lang+"_"+tt.key, func(t *testing.T) {
			SetLang(tt.lang)
			result := T(tt.key)
			assert.Contains(t, result, tt.contains)
		})
	}

	SetLang("zh-CN")
}

func TestI18n_DefaultFallback(t *testing.T) {
	result := T("nonexistent.key")
	assert.Equal(t, "nonexistent.key", result, "不存在的 key 应返回 key 本身")

	SetLang("xx-XX")
	result = T("err.se_failed")
	assert.Contains(t, result, "SE执行失败", "未知语言应回退到中文")

	SetLang("zh-CN")
}

func TestI18n_GetSetLang(t *testing.T) {
	SetLang("en-US")
	assert.Equal(t, "en-US", GetLang())

	SetLang("zh-CN")
	assert.Equal(t, "zh-CN", GetLang())
}
