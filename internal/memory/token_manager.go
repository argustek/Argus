package memory

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// ========== Token 估算（近似 tiktoken cl100k_base）==========

// TokenCounter token 计数器，基于字符/词频近似估算
type TokenCounter struct {
	mu sync.RWMutex
	cache map[string]int // 缓存已计算文本的 token 数
}

// NewTokenCounter 创建 token 计数器
func NewTokenCounter() *TokenCounter {
	return &TokenCounter{
		cache: make(map[string]int),
	}
}

// CountTokens 估算文本 token 数（近似 cl100k_base 编码）
// 算法：英文 ~4 字符/token，中文 ~1.5 字符/token，标点/空格 合并计算
func (tc *TokenCounter) CountTokens(text string) int {
	if text == "" {
		return 0
	}

	tc.mu.RLock()
	if cached, ok := tc.cache[text]; ok {
		tc.mu.RUnlock()
		return cached
	}
	tc.mu.RUnlock()

	count := estimateTokens(text)

	tc.mu.Lock()
	// 缓存限制：最多 5000 条，防止内存膨胀
	if len(tc.cache) < 5000 {
		tc.cache[text] = count
	}
	tc.mu.Unlock()

	return count
}

// ClearCache 清除缓存
func (tc *TokenCounter) ClearCache() {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	tc.cache = make(map[string]int)
}

// estimateTokens 核心估算算法
func estimateTokens(text string) int {
	runes := []rune(text)
	totalRunes := len(runes)

	if totalRunes == 0 {
		return 0
	}

	// 快速路径：纯 ASCII 文本
	isASCII := true
	cnCharCount := 0
	for _, r := range runes {
		if r > 127 {
			isASCII = false
			cnCharCount++
		}
	}

	if isASCII {
		// 英文文本：~4 字符/token（cl100k_base 平均值）
		// 但短单词和常见模式会压缩更好
		// 使用更精细的估算：按空白分词
		words := strings.Fields(text)
		wordCount := len(words)

		if wordCount == 0 {
			return totalRunes / 4 // 纯符号/空格
		}

		// 平均每个单词 ~1.3 tokens（考虑子词切分）
		tokens := int(float64(wordCount) * 1.3)

		// 加上标点和特殊字符的开销
		specialChars := countPatternTokens(text)

		return tokens + specialChars
	}

	// 中英混合文本
	// 中文：~1.5 字符/token（大部分汉字是 1-2 tokens）
	// 英文部分：~4 字符/token
	enRunes := totalRunes - cnCharCount
	cnTokens := int(float64(cnCharCount) / 1.5)
	enTokens := int(float64(enRunes) / 3.5)

	total := cnTokens + enTokens

	// 加上结构化开销（换行、缩进等）
	lineCount := strings.Count(text, "\n")
	total += lineCount / 10 // 每 10 行约 1 个额外 token

	if total < 1 {
		total = 1
	}
	return total
}

// countPatternTokens 计算正则模式匹配的额外 token 开销
func countPatternTokens(text string) int {
	count := 0
	// 代码相关模式通常产生更多 tokens
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`[{}()\[\];,]`),       // 代码括号
		regexp.MustCompile("[\"'`]"),             // 引号
		regexp.MustCompile(`\s{2,}`),             // 多空格
		regexp.MustCompile("\t"),                 // Tab
		regexp.MustCompile(`//.*$|/\*.*\*/`),     // 注释
	}
	for _, p := range patterns {
		matches := p.FindAllStringIndex(text, -1)
		count += len(matches) / 4 // 每 4 个模式匹配 ≈ 1 额外 token
	}
	return count
}

// ========== Context Window 管理器 ==========

// ContextRole 消息角色
type ContextRole string

const (
	RoleSystem    ContextRole = "system"
	RoleUser      ContextRole = "user"
	RoleAssistant ContextRole = "assistant"
	RoleTool      ContextRole = "tool"
)

// ContextMessage 上下文窗口中的一条消息
type ContextMessage struct {
	Role       ContextRole `json:"role"`
	Content    string      `json:"content"`
	TokenCount int         `json:"token_count"`
	Priority   int         `json:"priority"`   // 0=最低, 10=最高(不可裁剪)
	Timestamp  time.Time   `json:"timestamp"`
	Tag        string      `json:"tag,omitempty"` // 分类标签: "system"/"code"/"error"/"output"
	Compressed bool        `json:"compressed"`
}

// ContextBudget 上下文窗口预算配置
type ContextBudget struct {
	MaxTotalTokens     int `json:"max_total_tokens"`     // 总上限（如 128K）
	SystemReserve      int `json:"system_reserve"`       // system prompt 预留
	HistoryMinKeep     int `json:"history_min_keep"`     // 历史消息最少保留条数
	OutputReserve      int `json:"output_reserve"`       // 输出预留空间
	SafetyMargin       int `json:"safety_margin"`        // 安全边距
	CompressionTrigger float64 `json:"compression_trigger"` // 触发压缩的使用率阈值 (0.8 = 80%)
}

// DefaultContextBudget 默认预算（适配 GPT-4o / Claude 3.5）
func DefaultContextBudget() *ContextBudget {
	return &ContextBudget{
		MaxTotalTokens:     128000,
		SystemReserve:      4000,
		HistoryMinKeep:     5,
		OutputReserve:      16000,
		SafetyMargin:       2000,
		CompressionTrigger: 0.80,
	}
}

// CompactContextBudget 紧凑预算（适配 GPT-4o-mini / 小模型）
func CompactContextBudget() *ContextBudget {
	return &ContextBudget{
		MaxTotalTokens:     32000,
		SystemReserve:      2000,
		HistoryMinKeep:     3,
		OutputReserve:      4000,
		SafetyMargin:       1000,
		CompressionTrigger: 0.75,
	}
}

// ContextWindow 上下文窗口管理器
type ContextWindow struct {
	mu           sync.RWMutex
	messages     []*ContextMessage
	budget       *ContextBudget
	counter      *TokenCounter
	systemPrompt string

	// 统计
	totalAdded   int
	totalPruned  int
	totalCompressed int
	lastPruneTime time.Time
}

// NewContextWindow 创建上下文窗口管理器
func NewContextWindow(budget *ContextBudget) *ContextWindow {
	if budget == nil {
		budget = DefaultContextBudget()
	}
	return &ContextWindow{
		messages: make([]*ContextMessage, 0, 50),
		budget:   budget,
		counter:  NewTokenCounter(),
	}
}

// SetSystemPrompt 设置系统提示（高优先级，不参与裁剪）
func (cw *ContextWindow) SetSystemPrompt(prompt string) {
	cw.mu.Lock()
	defer cw.mu.Unlock()

	cw.systemPrompt = prompt
	newTokens := cw.counter.CountTokens(prompt)

	// 更新或创建 system 消息
	found := false
	for _, msg := range cw.messages {
		if msg.Role == RoleSystem {
			msg.Content = prompt
			msg.TokenCount = newTokens
			msg.Priority = 10 // 最高优先级
			found = true
			break
		}
	}
	if !found && prompt != "" {
		cw.messages = append([]*ContextMessage{{
			Role:       RoleSystem,
			Content:    prompt,
			TokenCount: newTokens,
			Priority:   10,
			Timestamp:  time.Now(),
			Tag:        "system",
		}}, cw.messages...)
	}
}

// AddMessage 添加消息到上下文窗口
func (cw *ContextWindow) AddMessage(role ContextRole, content string, priority int, tag string) error {
	cw.mu.Lock()
	defer cw.mu.Unlock()

	tokenCount := cw.counter.CountTokens(content)

	msg := &ContextMessage{
		Role:       role,
		Content:    content,
		TokenCount: tokenCount,
		Priority:   priority,
		Timestamp:  time.Now(),
		Tag:        tag,
	}

	cw.messages = append(cw.messages, msg)
	cw.totalAdded++

	return nil
}

// GetMessages 获取当前所有消息（用于构建 API 请求）
func (cw *ContextWindow) GetMessages() []map[string]string {
	cw.mu.RLock()
	defer cw.mu.RUnlock()

	result := make([]map[string]string, 0, len(cw.messages))
	for _, msg := range cw.messages {
		if msg.Compressed && strings.HasPrefix(msg.Content, "[SUMMARY]") {
			result = append(result, map[string]string{
				"role":    string(msg.Role),
				"content": msg.Content,
			})
		} else if !msg.Compressed {
			result = append(result, map[string]string{
				"role":    string(msg.Role),
				"content": msg.Content,
			})
		}
	}
	return result
}

// CurrentUsage 当前使用情况
func (cw *ContextWindow) CurrentUsage() (used, available int, ratio float64) {
	cw.mu.RLock()
	defer cw.mu.RUnlock()

	used = 0
	for _, msg := range cw.messages {
		used += msg.TokenCount
	}
	available = cw.budget.MaxTotalTokens - used - cw.budget.OutputReserve - cw.budget.SafetyMargin
	if available < 0 {
		available = 0
	}
	ratio = float64(used) / float64(cw.budget.MaxTotalTokens-cw.budget.OutputReserve)
	return
}

// TokenStats 详细统计
func (cw *ContextWindow) TokenStats() map[string]interface{} {
	cw.mu.RLock()
	defer cw.mu.RUnlock()

	byRole := make(map[string]int)
	byTag := make(map[string]int)
	total := 0
	compressedSaved := 0

	for _, msg := range cw.messages {
		byRole[string(msg.Role)] += msg.TokenCount
		byTag[msg.Tag] += msg.TokenCount
		total += msg.TokenCount
		if msg.Compressed {
			compressedSaved += msg.TokenCount // 原始大小（已被压缩后的大小替代）
		}
	}

	used, available, ratio := cw.currentUsageLocked()

	return map[string]interface{}{
		"total_tokens":        total,
		"used":                used,
		"available":           available,
		"ratio":               fmt.Sprintf("%.1f%%", ratio*100),
		"max_tokens":          cw.budget.MaxTotalTokens,
		"message_count":       len(cw.messages),
		"by_role":             byRole,
		"by_tag":              byTag,
		"total_added":         cw.totalAdded,
		"total_pruned":        cw.totalPruned,
		"total_compressed":    cw.totalCompressed,
		"needs_management":    ratio > cw.budget.CompressionTrigger,
	}
}

// ManageIfNeeded 自动管理：检查是否需要裁剪或压缩
func (cw *ContextWindow) ManageIfNeeded() (actionTaken bool, detail string) {
	cw.mu.Lock()
	defer cw.mu.Unlock()

	_, _, ratio := cw.currentUsageLocked()

	if ratio < cw.budget.CompressionTrigger {
		return false, fmt.Sprintf("usage %.1f%% below threshold %.0f%%", ratio*100, cw.budget.CompressionTrigger*100)
	}

	// 第一步：尝试裁剪低优先级消息
	pruned := cw.pruneLowPriority()
	if pruned > 0 {
		cw.totalPruned += pruned
		cw.lastPruneTime = time.Now()
		_, _, newRatio := cw.currentUsageLocked()
		if newRatio < cw.budget.CompressionTrigger {
			return true, fmt.Sprintf("pruned %d messages, ratio now %.1f%%", pruned, newRatio*100)
		}
	}

	// 第二步：压缩旧消息
	compressed := cw.CompressOldMessages()
	if compressed > 0 {
		cw.totalCompressed += compressed
		_, _, newRatio := cw.currentUsageLocked()
		return true, fmt.Sprintf("pruned %d, compressed %d messages, ratio now %.1f%%", pruned, compressed, newRatio*100)
	}

	return true, fmt.Sprintf("pruned %d, compressed %d (still at %.1f%%)", pruned, compressed, ratio*100)
}

// PruneToLimit 强制裁剪到指定 token 数
func (cw *ContextWindow) PruneToLimit(maxTokens int) int {
	cw.mu.Lock()
	defer cw.mu.Unlock()

	target := maxTokens - cw.budget.SafetyMargin
	current := cw.currentUsageLockedTotal()

	if current <= target {
		return 0
	}

	return cw.pruneToTarget(target)
}

// Clear 清空上下文（保留 system prompt）
func (cw *ContextWindow) Clear() {
	cw.mu.Lock()
	defer cw.mu.Unlock()

	var kept []*ContextMessage
	for _, msg := range cw.messages {
		if msg.Role == RoleSystem && msg.Priority >= 10 {
			kept = append(kept, msg)
		}
	}
	cw.messages = kept
}

// ========== 内部方法 ==========

func (cw *ContextWindow) currentUsageLocked() (used, available int, ratio float64) {
	used = cw.currentUsageLockedTotal()
	available = cw.budget.MaxTotalTokens - used - cw.budget.OutputReserve - cw.budget.SafetyMargin
	if available < 0 {
		available = 0
	}
	ratio = float64(used) / float64(cw.budget.MaxTotalTokens-cw.budget.OutputReserve)
	return
}

func (cw *ContextWindow) currentUsageLockedTotal() int {
	total := 0
	for _, msg := range cw.messages {
		total += msg.TokenCount
	}
	return total
}

// pruneLowPriority 裁剪低优先级消息（跳过高优先级和最近的 N 条）
func (cw *ContextWindow) pruneLowPriority() int {
	if len(cw.messages) <= cw.budget.HistoryMinKeep+1 { // +1 for system
		return 0
	}

	// 保护：system 消息 + 最近 N 条
	protected := make(map[int]bool)
	protectedCount := 0

	// 标记 system 和高优先级消息
	for i, msg := range cw.messages {
		if msg.Role == RoleSystem || msg.Priority >= 8 {
			protected[i] = true
			protectedCount++
		}
	}

	// 标记最近的消息
	fromEnd := 0
	for i := len(cw.messages) - 1; i >= 0 && fromEnd < cw.budget.HistoryMinKeep; i-- {
		if !protected[i] {
			protected[i] = true
			fromEnd++
		}
	}

	// 收集可裁剪的消息（按优先级升序，时间升序）
	type candidate struct {
		index    int
		priority int
		time     time.Time
		tokens   int
	}
	var candidates []candidate
	for i, msg := range cw.messages {
		if !protected[i] && msg.Priority < 8 {
			candidates = append(candidates, candidate{
				index:    i,
				priority: msg.Priority,
				time:     msg.Timestamp,
				tokens:   msg.TokenCount,
			})
		}
	}

	// 排序：低优先级先删，同优先级旧消息先删
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].priority != candidates[j].priority {
			return candidates[i].priority < candidates[j].priority
		}
		return candidates[i].time.Before(candidates[j].time)
	})

	// 裪剪直到使用率低于阈值
	pruned := 0
	_, _, ratio := cw.currentUsageLocked()
	targetRatio := cw.budget.CompressionTrigger - 0.1 // 目标降到阈值以下 10%

	for _, c := range candidates {
		if ratio <= targetRatio {
			break
		}
		cw.messages[c.index] = nil // 标记删除
		pruned++
		// 重新计算比率
		ratio = float64(cw.currentUsageLockedTotal()-c.tokens) /
			float64(cw.budget.MaxTotalTokens-cw.budget.OutputReserve)
	}

	// 压缩切片（移除 nil）
	if pruned > 0 {
		filtered := cw.messages[:0]
		for _, msg := range cw.messages {
			if msg != nil {
				filtered = append(filtered, msg)
			}
		}
		cw.messages = filtered
	}

	return pruned
}

// pruneToTarget 裁剪到目标 token 数
func (cw *ContextWindow) pruneToTarget(target int) int {
	pruned := 0
	for len(cw.messages) > cw.budget.HistoryMinKeep+1 {
		// 找到可裁剪的最低优先级消息
		bestIdx := -1
		bestPriority := 999
		bestTokens := 0

		for i, msg := range cw.messages {
			if msg.Role == RoleSystem || msg.Priority >= 8 {
				continue
			}
			// 保护最后几条
			if i >= len(cw.messages)-cw.budget.HistoryMinKeep {
				continue
			}
			if msg.Priority < bestPriority ||
				(msg.Priority == bestPriority && msg.TokenCount > bestTokens) {
				bestIdx = i
				bestPriority = msg.Priority
				bestTokens = msg.TokenCount
			}
		}

		if bestIdx == -1 {
			break
		}

		cw.messages = append(cw.messages[:bestIdx], cw.messages[bestIdx+1:]...)
		pruned++

		if cw.currentUsageLockedTotal() <= target {
			break
		}
	}

	cw.totalPruned += pruned
	return pruned
}

// compressOldMessages 压缩旧消息为摘要
func (cw *ContextWindow) CompressOldMessages() int {
	if len(cw.messages) <= cw.budget.HistoryMinKeep+2 {
		return 0
	}

	compressed := 0
	// 从第二条消息开始找可压缩的（跳过 system）
	for i := 1; i < len(cw.messages)-cw.budget.HistoryMinKeep; i++ {
		msg := cw.messages[i]
		if msg.Compressed || msg.Priority >= 7 {
			continue
		}

		summary := cw.summarizeMessage(msg)
		if summary != "" {
			msg.Content = "[压缩] " + summary
			msg.TokenCount = cw.counter.CountTokens(summary)
			msg.Compressed = true
			compressed++
		}
	}

	return compressed
}

// summarizeMessage 生成单条消息的摘要
func (cw *ContextWindow) summarizeMessage(msg *ContextMessage) string {
	content := msg.Content
	runes := []rune(content)

	switch msg.Tag {
	case "output":
		// 输出类：只保留前几行和关键信息
		lines := strings.Split(content, "\n")
		var keyLines []string
		for i, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			if i < 5 {
				keyLines = append(keyLines, line)
			} else if strings.Contains(line, "error") ||
				strings.Contains(line, "Error") ||
				strings.Contains(line, "failed") {
				keyLines = append(keyLines, line)
			}
		}
		if len(keyLines) == 0 {
			return ""
		}
		result := strings.Join(keyLines, "\n")
		if len([]rune(result)) > 300 {
			result = string(runes[:300]) + "..."
		}
		return "[SUMMARY output truncated]\n" + result

	case "code":
		// 代码类：保留关键行数信息
		lineCount := strings.Count(content, "\n") + 1
		firstLine := content
		if idx := strings.Index(content, "\n"); idx >= 0 {
			firstLine = content[:idx]
		}
		return fmt.Sprintf("[SUMMARY code: %d lines, starts with: %s]", lineCount, truncate(firstLine, 100))

	default:
		// 通用摘要：按角色生成不同前缀
		prefix := "[摘要]"
		switch msg.Role {
		case "user":
			prefix = "[USR摘要]"
		case "assistant":
			prefix = "[AI摘要]"
		}
		if len(runes) <= 80 {
			return fmt.Sprintf("%s %s", prefix, content)
		}
		if len(runes) <= 200 {
			return fmt.Sprintf("%s %s", prefix, content[:min(150, len(content))]+"...")
		}
		head := content[:100]
		tail := content[len(content)-100:]
		return fmt.Sprintf("[SUMMARY original %d chars]\n%s\n...\n%s",
			len(runes), head, tail)
	}
}

func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}
