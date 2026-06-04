package ai

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// VisionRequest 多模态请求（图片+文字）
type VisionRequest struct {
	ImagePath string // 图片路径或URL
	Prompt    string // 提示词（如"将这个UI截图转为React代码"）
}

// VisionResponse 多模态响应
type VisionResponse struct {
	Description string `json:"description"` // 图片内容描述
	Code        string `json:"code"`        // 生成的代码（如果有）
	Raw         string `json:"raw"`         // 原始LLM响应
	Error       string `json:"error,omitempty"`
}

// ImageContent 用于发送给 vision LLM 的图片内容
type ImageContent struct {
	Type     string           `json:"type"`
	Text     string           `json:"text,omitempty"`
	ImageURL *ImageURL        `json:"image_url,omitempty"`
}

// ImageURL 图片 URL 结构
type ImageURL struct {
	URL string `json:"url"`
}

// AnalyzeImage 分析图片（截图/UI设计稿/PDF等）
// 返回图片描述和可选的代码生成结果
// [P0-3] 多模态输入核心函数
func (c *SEProcessor) AnalyzeImage(imagePath, prompt string) (*VisionResponse, error) {
	// 1. 确定图片来源：本地文件 or URL
	var imageData []byte
	var mimeType string

	if strings.HasPrefix(imagePath, "http://") || strings.HasPrefix(imagePath, "https://") {
		resp, err := http.Get(imagePath)
		if err != nil {
			return nil, fmt.Errorf("download image: %w", err)
		}
		defer resp.Body.Close()
		imageData, err = io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("read image body: %w", err)
		}
		contentType := resp.Header.Get("Content-Type")
		if contentType == "" || contentType == "application/octet-stream" {
			mimeType = detectImageMIME(imagePath)
		} else {
			mimeType = contentType
		}
	} else {
		// 本地文件
		data, err := os.ReadFile(imagePath)
		if err != nil {
			return nil, fmt.Errorf("read image file %s: %w", imagePath, err)
		}
		imageData = data
		mimeType = detectImageMIME(imagePath)
	}

	// 验证是支持的图片格式
	if !isSupportedImage(mimeType) && mimeType != "application/pdf" {
		return &VisionResponse{
			Error: fmt.Sprintf("不支持的图片格式: %s (支持 PNG/JPG/GIF/WebP/PDF)", mimeType),
		}, nil
	}

	// 2. 构造 base64 data URI
	b64 := base64.StdEncoding.EncodeToString(imageData)
	dataURI := fmt.Sprintf("data:%s;base64,%s", mimeType, b64)

	// 3. 构造多模态消息
	userContent := []ImageContent{
		{Type: "text", Text: prompt},
		{Type: "image_url", ImageURL: &ImageURL{URL: dataURI}},
	}

	// 4. 调用 LLM（需要 vision 能力）
	result, err := c.callVisionLLM(userContent)
	if err != nil {
		return nil, fmt.Errorf("vision LLM call failed: %w", err)
	}

	// 5. 解析响应，提取代码块
	description := result
	code := extractCodeBlock(result)

	return &VisionResponse{
		Description: description,
		Code:        code,
		Raw:         result,
	}, nil
}

// callVisionLLM 发送多模态请求到 LLM
func (c *SEProcessor) callVisionLLM(content []ImageContent) (string, error) {
	// 将 content 序列化为 JSON 以便嵌入到消息中
	contentJSON, _ := json.Marshal(content)

	// 构造带图片的 user message
	msgContent := json.RawMessage(contentJSON)

	// 使用现有的 chat completion 接口，但传入多模态 content
	// 注意：这要求底层 LLM 支持 vision（如 GPT-4o、Claude、Gemini 等）
	messages := []map[string]interface{}{
		{
			"role": "system",
			"content": `你是一个专业的视觉分析AI。当用户提供截图、UI设计稿或图片时：
1. 详细描述图片内容和布局结构
2. 如果用户要求生成代码，输出完整的可运行代码
3. 代码使用 markdown code block 包裹，标注语言类型
4. 保持简洁准确，不要过度推测`,
		},
		{
			"role":    "user",
			"content": msgContent,
		},
	}

	reqBody := map[string]interface{}{
		"model":      c.client.config.Model,
		"messages":   messages,
		"max_tokens": 4096,
		"temperature": 0.1,
	}

	reqData, _ := json.Marshal(reqBody)
	apiURL := strings.TrimSuffix(c.client.config.BaseURL, "/") + "/chat/completions"
	httpReq, err := http.NewRequest("POST", apiURL, strings.NewReader(string(reqData)))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.client.config.APIKey)

	resp, err := c.client.client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error,omitempty"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}

	if result.Error != nil {
		return "", fmt.Errorf("API error: %s", result.Error.Message)
	}

	if len(result.Choices) == 0 {
		return "", fmt.Errorf("no response from LLM")
	}

	return result.Choices[0].Message.Content, nil
}

// detectImageMIME 从文件扩展名检测 MIME 类型
func detectImageMIME(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".svg":
		return "image/svg+xml"
	case ".pdf":
		return "application/pdf"
	case ".bmp":
		return "image/bmp"
	default:
		return "application/octet-stream"
	}
}

// isSupportedImage 检查是否为支持的图片格式
func isSupportedImage(mime string) bool {
	switch mime {
	case "image/png", "image/jpeg", "image/gif", "image/webp", "image/svg+xml", "image/bmp":
		return true
	default:
		return false
	}
}

// extractCodeBlock 从响应中提取第一个代码块
func extractCodeBlock(text string) string {
	// 匹配 ```language\n...\n```
	start := strings.Index(text, "```")
	if start == -1 {
		return ""
	}
	start += 3
	// 跳过语言标识
	nl := strings.Index(text[start:], "\n")
	if nl == -1 {
		return ""
	}
	start += nl + 1

	end := strings.Index(text[start:], "```")
	if end == -1 {
		return ""
	}
	return text[start : start+end]
}

// IsVisionModel 检查当前模型是否支持 vision
func (c *SEProcessor) IsVisionModel() bool {
	model := strings.ToLower(c.client.config.Model)
	visionKeywords := []string{"gpt-4o", "gpt-4-vision", "gpt-4-turbo", "claude-3", "gemini", "vision", "qwen-vl", "glm-4v"}
	for _, kw := range visionKeywords {
		if strings.Contains(model, kw) {
			return true
		}
	}
	return false
}

// GetImageAnalysisPrompt 根据场景返回合适的分析提示词
func GetImageAnalysisPrompt(taskType string) string {
	prompts := map[string]string{
		"ui_to_code": `分析这个UI截图，生成对应的代码。要求：
1. 准确还原布局和样式
2. 使用语义化的HTML/CSS或对应框架组件
3. 输出完整可运行的代码`,
		"design_review": `分析这个UI设计稿，给出专业评审意见：
1. 布局合理性
2. 色彩搭配
3. 用户体验建议
4. 可改进的地方`,
		"screenshot_debug": `分析这个错误截图，帮助诊断问题：
1. 描述截图中显示的错误信息
2. 分析可能的原因
3. 给出修复建议`,
		"diagram_parse": `分析这个图表/流程图/架构图，提取其中的信息并转换为文本描述。`,
		"general": `请详细描述这张图片的内容、结构和关键信息。`,
	}
	if p, ok := prompts[taskType]; ok {
		return p
	}
	return prompts["general"]
}
