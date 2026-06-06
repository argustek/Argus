package ai

import "strings"

// StreamEventType 统一流事件类型
// 参考 Claude Code 的 block_type 设计 + Codex CLI 的 Responses API 事件模型
type StreamEventType int

const (
	// StreamEventThinking 推理/思考内容（只进日志，不返回给上层调用方）
	StreamEventThinking StreamEventType = iota
	// StreamEventContent 正式内容（返回给上层调用方）
	StreamEventContent
)

// StreamEvent 统一流事件
// 上层只看到这个类型，不知道底层模型的 SSE 格式差异
type StreamEvent struct {
	Type StreamEventType
	Data string
}

// deepseekParse DeepSeek 模型族 delta 解析策略
// DeepSeek thinking 模式下 SSE delta 同时包含 reasoning_content 和 content 两个字段
func deepseekParse(delta Delta) []StreamEvent {
	var events []StreamEvent
	if delta.ReasoningContent != "" {
		events = append(events, StreamEvent{Type: StreamEventThinking, Data: delta.ReasoningContent})
	}
	if delta.Content != "" {
		events = append(events, StreamEvent{Type: StreamEventContent, Data: delta.Content})
	}
	return events
}

// defaultParse 默认 delta 解析策略（GLM 及其他无特殊推理字段的模型）
func defaultParse(delta Delta) []StreamEvent {
	if delta.Content == "" {
		return nil
	}
	return []StreamEvent{{Type: StreamEventContent, Data: delta.Content}}
}

// parseDelta 根据当前模型选择对应的 delta 解析策略
// 新增模型只需在这里加一个 case 分支
func (c *Client) parseDelta(delta Delta) []StreamEvent {
	modelLower := strings.ToLower(c.config.Model)
	switch {
	case strings.Contains(modelLower, "deepseek"):
		return deepseekParse(delta)
	// 未来扩展：
	// case strings.Contains(modelLower, "claude"):
	//     return claudeParse(delta)
	// case strings.Contains(modelLower, "gemini"):
	//     return geminiParse(delta)
	default:
		return defaultParse(delta)
	}
}
