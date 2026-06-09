package memory

import (
	"fmt"
	"strings"
)

type Compressor struct {
	mm *MemoryManager
}

func NewCompressor(mm *MemoryManager) *Compressor {
	return &Compressor{mm: mm}
}

func (c *Compressor) CompressConversations(taskID string, keepLastN int) (int, error) {
	conversations, err := c.mm.GetConversations(taskID, 1000)
	if err != nil {
		return 0, fmt.Errorf("获取对话记录失败：%v", err)
	}

	if len(conversations) <= keepLastN {
		return 0, nil  // 无需压缩，返回0
	}

	toCompress := conversations[:len(conversations)-keepLastN]
	compressedCount := 0

	for _, conv := range toCompress {
		if conv.IsCompressed {
			continue
		}

		summary := c.generateSummary(conv.Content)

		_, err := c.mm.db.Exec(`
			UPDATE conversations
			SET summary = ?, is_compressed = TRUE
			WHERE id = ?
		`, summary, conv.ID)

		if err != nil {
			return compressedCount, fmt.Errorf("压缩对话记录失败：%v", err)
		}

		compressedCount++
	}

	return compressedCount, nil  // 返回实际压缩数量
}

func (c *Compressor) generateSummary(content string) string {
	lines := strings.Split(content, "\n")
	
	var summaryParts []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		
		if strings.HasPrefix(line, "#") || strings.HasPrefix(line, "##") || strings.HasPrefix(line, "###") {
			summaryParts = append(summaryParts, line)
		}
		
		if strings.Contains(line, "完成") || strings.Contains(line, "成功") || strings.Contains(line, "错误") || strings.Contains(line, "失败") {
			if len(line) > 100 {
				line = line[:100] + "..."
			}
			summaryParts = append(summaryParts, "- "+line)
		}
	}
	
	if len(summaryParts) == 0 {
		if len(content) > 200 {
			return content[:200] + "..."
		}
		return content
	}
	
	return strings.Join(summaryParts, "\n")
}

func (c *Compressor) EstimateTokens(text string) int {
	return len([]rune(text)) / 2
}

func (c *Compressor) ShouldCompress(taskID string, maxTokens int) (bool, error) {
	conversations, err := c.mm.GetConversations(taskID, 1000)
	if err != nil {
		return false, err
	}
	
	totalTokens := 0
	for _, conv := range conversations {
		totalTokens += c.EstimateTokens(conv.Content)
	}
	
	return totalTokens > maxTokens, nil
}

func (c *Compressor) CompressIfNeeded(taskID string, maxTokens int, keepLastN int) (int, error) {
	shouldCompress, err := c.ShouldCompress(taskID, maxTokens)
	if err != nil {
		return 0, err
	}

	if shouldCompress {
		return c.CompressConversations(taskID, keepLastN)
	}

	return 0, nil
}
