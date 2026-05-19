package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"argus/internal/i18n"
	"argus/internal/limiter"
	"argus/internal/types"
)

const apiCallOpType = "api_call"

const (
	ReplyLangAuto = "auto"
	ReplyLangZh   = "zh"
	ReplyLangEn   = "en"
)

func DetectLanguage(text string) string {
	if text == "" {
		return ReplyLangAuto
	}

	total := 0
	chineseChars := 0
	for _, r := range text {
		total++
		if r >= 0x4e00 && r <= 0x9fff {
			chineseChars++
		}
	}
	if total == 0 {
		return ReplyLangAuto
	}

	ratio := float64(chineseChars) / float64(total)
	if ratio > 0.3 {
		return ReplyLangZh
	}
	return ReplyLangEn
}

func GetLanguageInstruction(lang, userMessage string) string {
	switch lang {
	case ReplyLangZh:
		return "\n\n⚠️ 语言规则：你必须使用中文回复。不要使用英文或其他语言。" +
			"\nIgnore the language pattern in the conversation history, only follow this language instruction."
	case ReplyLangEn:
		return "\n\n⚠️ Language Rule: You MUST reply in English only." +
			"\nWARNING: Do not output any Chinese characters (including punctuation). Output only English." +
			"\nIgnore the language pattern in the conversation history, only follow this language instruction."
	case ReplyLangAuto:
		detected := DetectLanguage(userMessage)
		if detected == ReplyLangZh {
			return "\n\n⚠️ 语言规则：用户使用中文，你必须用中文回复。"
		}
		return "\n\n⚠️ Language Rule: User is using English, you MUST reply in English only." +
			"\nWARNING: Do not output any Chinese characters (including punctuation). Output only English."
	default:
		return ""
	}
}

// Client AI客户端
type Client struct {
	config         types.APIConfig
	client         *http.Client
	circuitBreaker *limiter.CircuitBreaker
	rateLimiter    *limiter.RateLimiter
	mu             sync.Mutex
}

// NewClient 创建AI客户端
func NewClient(config types.APIConfig) *Client {
	cbConfig := types.CircuitBreakerConfig{
		Exec: types.CircuitBreaker{
			FailureThreshold: 3,
			TimeoutSeconds:   30,
		},
	}
	rlConfig := types.RateLimitConfig{
		Exec: types.RateLimit{
			MaxPerMinute: 20,
		},
	}

	return &Client{
		config: config,
		client: &http.Client{
			Timeout: 60 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        10,
				MaxConnsPerHost:     5,
				IdleConnTimeout:     30 * time.Second,
				TLSHandshakeTimeout: 10 * time.Second,
				ResponseHeaderTimeout: 30 * time.Second,
				ExpectContinueTimeout: 1 * time.Second,
			},
		},
		circuitBreaker: limiter.NewCircuitBreaker(cbConfig),
		rateLimiter:    limiter.NewRateLimiter(rlConfig),
	}
}

func (c *Client) checkBeforeCall() error {
	if err := c.circuitBreaker.Check(apiCallOpType); err != nil {
		return fmt.Errorf("circuit breaker: %w", err)
	}
	if err := c.rateLimiter.Check(apiCallOpType); err != nil {
		return fmt.Errorf("rate limit: %w", err)
	}
	return nil
}

func (c *Client) recordSuccess() {
	c.circuitBreaker.RecordSuccess(apiCallOpType)
}

func (c *Client) recordFailure() {
	c.circuitBreaker.RecordFailure(apiCallOpType)
}

// Message 消息结构
type Message struct {
	Role             string     `json:"role"`
	Content          string     `json:"content"`
	ReasoningContent string     `json:"reasoning_content,omitempty"`
	ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID       string     `json:"tool_call_id,omitempty"`
}

// ToolCall 工具调用
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

// FunctionCall 函数调用
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// Tool 工具定义
type Tool struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

// ToolFunction 工具函数定义
type ToolFunction struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

// ChatRequest 聊天请求
type ChatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Stream   bool      `json:"stream"`
	Tools    []Tool    `json:"tools,omitempty"`
}

// ChatResponse 聊天响应
type ChatResponse struct {
	Choices []struct {
		Message      Message `json:"message"`
		FinishReason string  `json:"finish_reason"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// Chat 发送聊天请求
func (c *Client) Chat(ctx context.Context, systemPrompt, userContent string, replyLanguage string) (string, error) {
	if err := c.checkBeforeCall(); err != nil {
		return "", err
	}

	langInstruction := GetLanguageInstruction(replyLanguage, userContent)
	if langInstruction != "" {
		systemPrompt = systemPrompt + langInstruction
	}

	messages := []Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userContent},
	}

	req := ChatRequest{
		Model:    c.config.Model,
		Messages: messages,
		Stream:   false,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("marshal request failed: %v", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.config.BaseURL+"/chat/completions", bytes.NewBuffer(data))
	if err != nil {
		return "", fmt.Errorf("create request failed: %v", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.config.APIKey)

	resp, err := c.client.Do(httpReq)
	if err != nil {
		c.recordFailure()
		return "", fmt.Errorf("send request failed: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		c.recordFailure()
		return "", fmt.Errorf("read response failed: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		c.recordFailure()
		return "", fmt.Errorf("API error: %s", string(body))
	}

	var chatResp ChatResponse
	if err := json.Unmarshal(body, &chatResp); err != nil {
		c.recordFailure()
		return "", fmt.Errorf("unmarshal response failed: %v", err)
	}

	if chatResp.Error != nil {
		c.recordFailure()
		return "", fmt.Errorf("API error: %s", chatResp.Error.Message)
	}

	if len(chatResp.Choices) == 0 {
		c.recordFailure()
		return "", fmt.Errorf("no response from AI")
	}

	msgContent := chatResp.Choices[0].Message.Content
	if msgContent == "" {
		msgContent = chatResp.Choices[0].Message.ReasoningContent
	}
	if msgContent == "" {
		c.recordFailure()
		return "", fmt.Errorf("empty response from AI")
	}

	c.recordSuccess()
	return msgContent, nil
}

// StreamChunk 流式响应片段
type StreamChunk struct {
	Choices []struct {
		Delta struct {
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content,omitempty"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
}

// ChatStream 流式聊天请求，每收到文本片段调用 onChunk，返回累积的完整文本
func (c *Client) ChatStream(ctx context.Context, systemPrompt string, history []Message, userContent string, replyLanguage string, onChunk func(delta string)) (string, error) {
	maxRetries := 3
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			waitTime := time.Duration(attempt*2) * time.Second
			fmt.Printf("[AI Retry] 第%d次重试，等待%v...\n", attempt, waitTime)
			time.Sleep(waitTime)
		}

		result, err := c.chatStreamOnce(ctx, systemPrompt, history, userContent, replyLanguage, onChunk)
		if err == nil {
			c.recordSuccess()
			return result, nil
		}

		lastErr = err

		if !isRetryableError(err) {
			c.recordFailure()
			return "", err
		}

		fmt.Printf("[AI Retry] 可重试错误: %v (尝试 %d/%d)\n", err, attempt+1, maxRetries)
	}

	c.recordFailure()
	return "", fmt.Errorf(i18n.T("err.api_retry_failed"), maxRetries, lastErr)
}

func (c *Client) chatStreamOnce(ctx context.Context, systemPrompt string, history []Message, userContent string, replyLanguage string, onChunk func(delta string)) (string, error) {
	if err := c.checkBeforeCall(); err != nil {
		return "", err
	}

	langInstruction := GetLanguageInstruction(replyLanguage, userContent)
	if langInstruction != "" {
		systemPrompt = systemPrompt + langInstruction
	}

	messages := []Message{
		{Role: "system", Content: systemPrompt},
	}
	messages = append(messages, history...)
	messages = append(messages, Message{Role: "user", Content: userContent})

	req := ChatRequest{
		Model:    c.config.Model,
		Messages: messages,
		Stream:   true,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("marshal request failed: %v", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.config.BaseURL+"/chat/completions", bytes.NewBuffer(data))
	if err != nil {
		return "", fmt.Errorf("create request failed: %v", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.config.APIKey)
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := c.client.Do(httpReq)
	if err != nil {
		c.recordFailure()
		return "", fmt.Errorf("send request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		c.recordFailure()
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("API error: %s", string(body))
	}

	var fullContent string
	reader := io.Reader(resp.Body)
	buf := make([]byte, 4096)
	leftover := ""

	for {
		n, readErr := reader.Read(buf)
		if n > 0 {
			chunk := leftover + string(buf[:n])
			lines := splitLines(chunk)

			for i, line := range lines {
				if i == len(lines)-1 && readErr == nil && !hasLineEnd(chunk, line) {
					leftover = line
					break
				}

				line = trimDataPrefix(line)
				if line == "" || line == "[DONE]" {
					continue
				}

				var streamChunk StreamChunk
				if err := json.Unmarshal([]byte(line), &streamChunk); err != nil {
					continue
				}

				if len(streamChunk.Choices) > 0 {
					delta := streamChunk.Choices[0].Delta.Content
					if delta == "" {
						delta = streamChunk.Choices[0].Delta.ReasoningContent
					}
					if delta != "" {
						fullContent += delta
						if onChunk != nil {
							onChunk(delta)
						}
					}
				}
			}
		}

		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			c.recordFailure()
			return fullContent, fmt.Errorf("read stream failed: %v", readErr)
		}
	}

	if fullContent == "" {
		c.recordFailure()
		return "", fmt.Errorf("empty response from AI")
	}

	c.recordSuccess()
	return fullContent, nil
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		} else if s[i] == '\r' {
			if i+1 < len(s) && s[i+1] == '\n' {
				lines = append(lines, s[start:i])
				start = i + 2
				i++
			} else {
				lines = append(lines, s[start:i])
				start = i + 1
			}
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func hasLineEnd(full, line string) bool {
	idx := 0
	for i := 0; i < len(full); i++ {
		if full[i:] == line {
			idx = i + len(line)
			break
		}
	}
	return idx >= len(full) || full[idx] == '\n' || full[idx] == '\r'
}

func trimDataPrefix(line string) string {
	if len(line) >= 6 && line[:6] == "data: " {
		return line[6:]
	}
	if len(line) >= 5 && line[:5] == "data:" {
		return line[5:]
	}
	return line
}

// ChatWithTools 带工具调用的聊天
func (c *Client) ChatWithTools(ctx context.Context, systemPrompt string, history []Message, userContent string, tools []Tool) (*ChatResponse, error) {
	if err := c.checkBeforeCall(); err != nil {
		return nil, err
	}

	messages := []Message{
		{Role: "system", Content: systemPrompt},
	}
	messages = append(messages, history...)
	messages = append(messages, Message{Role: "user", Content: userContent})

	req := ChatRequest{
		Model:    c.config.Model,
		Messages: messages,
		Stream:   false,
		Tools:    tools,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request failed: %v", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.config.BaseURL+"/chat/completions", bytes.NewBuffer(data))
	if err != nil {
		return nil, fmt.Errorf("create request failed: %v", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.config.APIKey)

	resp, err := c.client.Do(httpReq)
	if err != nil {
		c.recordFailure()
		return nil, fmt.Errorf("send request failed: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		c.recordFailure()
		return nil, fmt.Errorf("read response failed: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		c.recordFailure()
		return nil, fmt.Errorf("API error: %s", string(body))
	}

	var chatResp ChatResponse
	if err := json.Unmarshal(body, &chatResp); err != nil {
		c.recordFailure()
		return nil, fmt.Errorf("unmarshal response failed: %v", err)
	}

	if chatResp.Error != nil {
		c.recordFailure()
		return nil, fmt.Errorf("API error: %s", chatResp.Error.Message)
	}

	if len(chatResp.Choices) == 0 {
		c.recordFailure()
		return nil, fmt.Errorf("no response from AI")
	}

	c.recordSuccess()
	return &chatResp, nil
}

// ChatWithHistory 带历史记录的聊天
func (c *Client) ChatWithHistory(ctx context.Context, systemPrompt string, history []Message, userContent string, replyLanguage string) (string, error) {
	if err := c.checkBeforeCall(); err != nil {
		return "", err
	}

	langInstruction := GetLanguageInstruction(replyLanguage, userContent)
	if langInstruction != "" {
		systemPrompt = systemPrompt + langInstruction
	}

	messages := []Message{
		{Role: "system", Content: systemPrompt},
	}
	messages = append(messages, history...)
	messages = append(messages, Message{Role: "user", Content: userContent})

	req := ChatRequest{
		Model:    c.config.Model,
		Messages: messages,
		Stream:   false,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("marshal request failed: %v", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.config.BaseURL+"/chat/completions", bytes.NewBuffer(data))
	if err != nil {
		return "", fmt.Errorf("create request failed: %v", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.config.APIKey)

	resp, err := c.client.Do(httpReq)
	if err != nil {
		c.recordFailure()
		return "", fmt.Errorf("send request failed: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		c.recordFailure()
		return "", fmt.Errorf("read response failed: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		c.recordFailure()
		return "", fmt.Errorf("API error: %s", string(body))
	}

	var chatResp ChatResponse
	if err := json.Unmarshal(body, &chatResp); err != nil {
		c.recordFailure()
		return "", fmt.Errorf("unmarshal response failed: %v", err)
	}

	if chatResp.Error != nil {
		c.recordFailure()
		return "", fmt.Errorf("API error: %s", chatResp.Error.Message)
	}

	if len(chatResp.Choices) == 0 {
		c.recordFailure()
		return "", fmt.Errorf("no response from AI")
	}

	c.recordSuccess()
	return chatResp.Choices[0].Message.Content, nil
}

func isRetryableError(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())

	retryablePatterns := []string{
		"429",
		"too many requests",
		"rate limit",
		"timeout",
		"connection refused",
		"connection reset",
		"connection aborted",
		"network is unreachable",
		"no such host",
		"dns",
		"eof",
		"502",
		"503",
		"504",
		"temporary",
		"socket",
		"i/o timeout",
		"context deadline exceeded",
		"tls handshake",
	}

	for _, pattern := range retryablePatterns {
		if strings.Contains(errStr, pattern) {
			return true
		}
	}
	return false
}
