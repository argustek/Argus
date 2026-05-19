package chat

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
)

// TurnPriority 消息优先级
type TurnPriority int

const (
	TurnPriorityNormal TurnPriority = iota // PM/SE/AP 普通优先级
	TurnPriorityHigh                      // USR/C 高优先级(可打断)
)

// Message 消息结构
type Message struct {
	ID        string    `json:"id"`                 // 消息唯一ID
	From      string    `json:"from"`               // 发送者: user, pm, se, mc
	To        string    `json:"to"`                 // 接收者: pm, se, user
	Role      string    `json:"role"`               // 角色: user, pm, se, mc（用于前端显示）
	Content   string    `json:"content"`            // 内容（去掉@标记）
	Raw       string    `json:"raw"`                // 原始内容
	Timestamp time.Time `json:"timestamp"`          // 时间戳
	Source    string    `json:"source"`             // 来源: user_input, handleToPM, handlePMReview, handleSEAskPM, c_monitor, system
	ReplyTo   string    `json:"reply_to,omitempty"` // 回复的消息ID（消息线程）
	Priority  TurnPriority `json:"priority"`        // 消息优先级
}

// Router 消息路由器（带轮换管理）
type Router struct {
	mu            sync.RWMutex
	lastSpokenBy  string // 上一个说话的角色
	isProcessing  bool   // 是否正在处理消息
	processingFrom string // 当前正在处理的消息来源
}

// NewRouter 创建路由器
func NewRouter() *Router {
	return &Router{}
}

// GetPriority 根据来源获取优先级
func GetPriority(from, source string) TurnPriority {
	switch from {
	case "user", "usr":
		return TurnPriorityHigh
	case "mc", "sys_c", "system":
		if source == "c_monitor" || source == "c_force" || source == "system" {
			return TurnPriorityHigh
		}
	}
	return TurnPriorityNormal
}

// IsHighPriority 判断是否高优先级（USR/C）
func IsHighPriority(from, source string) bool {
	return GetPriority(from, source) == TurnPriorityHigh
}

func (r *Router) CheckTurn(from, source string) (bool, string) {
	return r.CheckTurnInternal(from, source, false)
}

// CheckTurnInternal 轮换检查（内部版本）
// internal=true 时跳过 isProcessing 检查（用于父函数内部的子调用）
// 返回: allowed(是否允许), reason(拒绝原因)
func (r *Router) CheckTurnInternal(from, source string, internal bool) (bool, string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	priority := GetPriority(from, source)

	if priority == TurnPriorityHigh {
		fmt.Printf("[TurnCheck] ✅ P0放行: from=%s source=%s (当前处理中=%v, last=%s)\n",
			from, source, r.isProcessing, r.lastSpokenBy)
		return true, ""
	}

	if !internal && r.isProcessing {
		fmt.Printf("[TurnCheck] ❌ P1排队: from=%s source=%s (当前处理中=%s)\n",
			from, source, r.processingFrom)
		return false, fmt.Sprintf("当前%s正在处理，请排队", r.processingFrom)
	}

	if r.lastSpokenBy != "" && r.lastSpokenBy == from {
		fmt.Printf("[TurnCheck] ❌ 连续说话拦截: from=%s (上次也是%s)\n", from, r.lastSpokenBy)
		return false, fmt.Sprintf("%s刚说过话，请等待其他角色", from)
	}

	fmt.Printf("[TurnCheck] ✅ P1放行: from=%s source=%s (last=%s, internal=%v)\n", from, source, r.lastSpokenBy, internal)
	return true, ""
}

// MarkProcessingStart 标记开始处理
func (r *Router) MarkProcessingStart(from string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.isProcessing = true
	r.processingFrom = from
}

// MarkProcessingEnd 标记处理结束 + 更新最后说话者
func (r *Router) MarkProcessingEnd(spokenBy string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.isProcessing = false
	r.processingFrom = ""
	if spokenBy != "" {
		r.lastSpokenBy = spokenBy
		fmt.Printf("[TurnCheck] 📝 更新lastSpokenBy=%s\n", spokenBy)
	}
}

// TempReleaseProcessing 临时释放处理中状态，返回恢复函数
// 用于 ProcessMessageFrom 等内部路由场景：释放父函数的 processing 锁，
// 让 handleToPM 中的 CheckTurn 能正常放行 PM
func (r *Router) TempReleaseProcessing() (restore func()) {
	r.mu.Lock()
	wasProcessing := r.isProcessing
	r.isProcessing = false
	r.mu.Unlock()

	fmt.Printf("[TurnCheck] 🔸 临时释放processing锁 (原值=%v)\n", wasProcessing)
	return func() {
		if wasProcessing {
			r.mu.Lock()
			r.isProcessing = true
			r.mu.Unlock()
			fmt.Println("[TurnCheck] 🔹 恢复processing锁")
		}
	}
}

// ForceClear 强制清除处理状态（用于打断）
func (r *Router) ForceClear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.isProcessing = false
	r.processingFrom = ""
	fmt.Println("[TurnCheck] 🔴 强制清除处理状态")
}

// ForceClearLastSpoken 强制清除最后发言者记录（用于用户新消息触发）
func (r *Router) ForceClearLastSpoken() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastSpokenBy = ""
	fmt.Println("[TurnCheck] 🔄 清除lastSpokenBy（用户触发）")
}

// Parse 解析消息（单人路由）
// 规则：有@标记 → 发给该角色（精确投递）；无@ → 默认PM
// 注意：如果有多个@标记，只取第一个有效的；连续@@算作单个@
func (r *Router) Parse(from, content string) Message {
	cleanedContent := strings.ReplaceAll(content, "@@", "@")
	re := regexp.MustCompile(`@(\w+)\s+`)
	matches := re.FindStringSubmatch(cleanedContent)

	ts := time.Now()
	priority := GetPriority(from, "")
	if len(matches) > 1 {
		target := strings.ToLower(matches[1])
		resultContent := re.ReplaceAllString(cleanedContent, "")
		resultContent = strings.TrimSpace(resultContent)
		return Message{
			From:      from,
			To:        target,
			Content:   resultContent,
			Raw:       content,
			Timestamp: ts,
			Priority:  priority,
		}
	}

	return Message{
		From:      from,
		To:        "pm",
		Content:   content,
		Raw:       content,
		Timestamp: ts,
		Priority:  priority,
	}
}

// ParseMultiple 解析消息（支持多目标）
// 当消息中包含多个 @ 标记时，会返回多条消息，每条消息发送给一个角色
func (r *Router) ParseMultiple(from, content string) []Message {
	// 提取所有 @ 标记
	re := regexp.MustCompile(`@(\w+)\s+`)
	allMatches := re.FindAllStringSubmatch(content, -1)

	if len(allMatches) > 0 {
		messages := []Message{}
		seen := make(map[string]bool) // 避免重复

		for _, matches := range allMatches {
			target := strings.ToLower(matches[1])
			if seen[target] {
				continue // 跳过重复的目标
			}
			seen[target] = true

			// 移除所有 @ 标记作为消息内容
			cleanContent := re.ReplaceAllString(content, "")
			cleanContent = strings.TrimSpace(cleanContent)

			messages = append(messages, Message{
				From:    from,
				To:      target,
				Content: cleanContent,
				Raw:     content,
			})
		}

		return messages
	}

	// 没有 @ 标记，默认发给 PM
	return []Message{{
		From:    from,
		To:      "pm",
		Content: content,
		Raw:     content,
	}}
}
