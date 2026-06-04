package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// CodeIndexer 项目代码语义索引
// 通过解析代码结构（函数/类型/注释）+ 关键词匹配实现语义搜索
// 不依赖外部 embedding API，本地运行
type CodeIndexer struct {
	mu       sync.RWMutex
	workDir  string
	indexed  bool
	entries  []*CodeEntry
	// 倒排索引：token → entry indices
	invertedIndex map[string][]int
	// AI client 用于语义概念提取
	client *Client
	// 是否已生成语义概念
	conceptsGenerated bool
}

// CodeEntry 代码条目
type CodeEntry struct {
	FilePath   string   // 相对路径
	Symbol     string   // 函数名/类型名/包名
	Kind       string   // function/type/package/comment
	Line       int      // 起始行
	LineEnd    int      // 结束行
	Signature  string   // 函数签名或类型定义
	Comment    string   // 关联注释
	Content    string   // 完整代码片段
	Keywords   []string // 分词结果
	Importance float64  // 重要性评分
	// AI 语义指纹（延迟生成）
	SemanticConcepts []string // AI 提取的语义概念（如 "authentication", "REST API", "database query"）
}

// SemSearchResult 语义搜索结果
type SemSearchResult struct {
	Score    float64
	Entry    *CodeEntry
	Snippet  string   // 带行号的上下文片段
	MatchOn  []string // 匹配的关键词/概念
}

// NewCodeIndexer 创建索引器
func NewCodeIndexer(workDir string) *CodeIndexer {
	return &CodeIndexer{
		workDir:       workDir,
		invertedIndex: make(map[string][]int),
	}
}

// SetClient 设置 AI client，用于语义概念生成
func (ci *CodeIndexer) SetClient(client *Client) {
	ci.client = client
}

// tokenize 分词：拆分驼峰命名、下划线、提取关键词
func tokenize(s string) []string {
	s = strings.ToLower(s)
	// 按驼峰拆分
	re := regexp.MustCompile(`[a-z]+|[A-Z][a-z]*|[0-9]+|_+`)
	parts := re.FindAllString(s, -1)
	var result []string
	seen := map[string]bool{}
	for _, p := range parts {
		p = strings.Trim(p, "_")
		if len(p) >= 2 && !isStopWord(p) {
			if !seen[p] {
				result = append(result, p)
				seen[p] = true
			}
		}
	}
	return result
}

// common stop words
var stopWords = map[string]bool{
	"the": true, "and": true, "for": true, "func": true, "type": true,
	"var": true, "int": true, "str": true, "nil": true, "err": true,
	"main": true, "import": true, "package": true, "return": true,
	"if": true, "else": true, "range": true, "case": true, "default": true,
	"this": true, "that": true, "with": true, "from": true, "has": true,
}

func isStopWord(w string) bool {
	return stopWords[w]
}

// IndexProject 扫描项目并建立索引
func (ci *CodeIndexer) IndexProject() error {
	ci.mu.Lock()
	defer ci.mu.Unlock()

	ci.entries = nil
	ci.invertedIndex = make(map[string][]int)

	skipDirs := map[string]bool{
		".git": true, "node_modules": true, "vendor": true,
		".idea": true, ".vscode": true, "__pycache__": true,
		"dist": true, "build": true, ".next": true,
		".cache": true, "coverage": true, ".argus": true,
	}

	err := filepath.Walk(ci.workDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			if skipDirs[info.Name()] {
				return filepath.SkipDir
			}
			return nil
		}

		relPath, _ := filepath.Rel(ci.workDir, path)
		ext := filepath.Ext(path)

		// 只索引导入的代码文件
		supported := map[string]bool{
			".go": true, ".py": true, ".js": true, ".ts": true,
			".tsx": true, ".jsx": true, ".java": true, ".rs": true,
			".c": true, ".h": true, ".cpp": true, ".hpp": true,
			".cs": true, ".rb": true, ".php": true, ".swift": true,
			".kt": true, ".scala": true, ".vue": true, ".svelte": true,
		}
		if !supported[ext] {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		ci.parseFile(relPath, string(content), ext)
		return nil
	})

	ci.indexed = err == nil
	return err
}

// parseFile 解析单个文件的代码结构
func (ci *CodeIndexer) parseFile(filePath, content, ext string) {
	lines := strings.Split(content, "\n")
	baseName := filepath.Base(filePath)

	// 添加文件级条目
	entry := &CodeEntry{
		FilePath:  filePath,
		Symbol:    baseName,
		Kind:      "file",
		Line:      1,
		LineEnd:   len(lines),
		Content:   content[:min(500, len(content))],
		Importance: 0.1,
	}
	entry.Keywords = tokenize(baseName + " " + filePath + " " + content[:min(200, len(content))])
	ci.addEntry(entry)

	// 提取包名/模块名
	if ext == ".go" {
		for _, line := range lines[:min(5, len(lines))] {
			if strings.HasPrefix(line, "package ") {
				pkgName := strings.TrimPrefix(line, "package ")
				e := &CodeEntry{
					FilePath:  filePath,
					Symbol:    pkgName,
					Kind:      "package",
					Line:      1,
					LineEnd:   1,
					Importance: 0.2,
				}
				e.Keywords = tokenize(pkgName)
				ci.addEntry(e)
				break
			}
		}
	}

	// 通用函数/方法提取（跨语言）
	funcPattern := regexp.MustCompile(`(?:func|def|function|fn|public|private|static)\s+(\w+)\s*\(`)
	matches := funcPattern.FindAllStringSubmatchIndex(content, -1)

	for _, m := range matches {
		if len(m) < 4 {
			continue
		}
		// 计算行号
		pos := m[0]
		lineNo := 1
		charCount := 0
		for i, l := range lines {
			charCount += len(l) + 1
			if charCount > pos {
				lineNo = i + 1
				break
			}
		}

		funcName := content[m[2]:m[3]]

		// 跳过太短的函数名
		if len(funcName) < 2 {
			continue
		}

		// 提取函数注释（前面的行）
		comment := ""
		for i := lineNo - 2; i >= max(0, lineNo-10); i-- {
			trimmed := strings.TrimSpace(lines[i])
			if trimmed == "" {
				continue
			}
			if strings.HasPrefix(trimmed, "//") {
				comment = strings.TrimPrefix(trimmed, "//") + "; " + comment
			} else if strings.HasPrefix(trimmed, "#") {
				comment = strings.TrimPrefix(trimmed, "#") + "; " + comment
			} else {
				break
			}
		}

		// 提取函数签名
		sigEnd := strings.Index(content[m[3]:], "{")
		sig := funcName + "("
		if sigEnd > 0 {
			sig = strings.TrimSpace(content[m[2]:m[3]+sigEnd]) + "{...}"
		}

		// 提取函数体（前几行作为 snippet）
		bodyEnd := min(lineNo+20, len(lines))
		body := strings.Join(lines[lineNo:bodyEnd], "\n")

		e := &CodeEntry{
			FilePath:  filePath,
			Symbol:    funcName,
			Kind:      "function",
			Line:      lineNo,
			LineEnd:   lineNo + 20,
			Signature: sig,
			Comment:   strings.TrimSpace(comment),
			Content:   body,
			Importance: 0.8,
		}
		e.Keywords = tokenize(funcName + " " + comment + " " + body[:min(200, len(body))])
		ci.addEntry(e)
	}

	// 类型/结构体提取
	typePattern := regexp.MustCompile(`(?:type|class|struct|interface|enum)\s+(\w+)`)
	for _, m := range typePattern.FindAllStringSubmatch(content, -1) {
		if len(m) < 2 || len(m[1]) < 2 {
			continue
		}

		typeName := m[1]
		pos := strings.Index(content, m[0])
		lineNo := 1
		charCount := 0
		for i, l := range lines {
			charCount += len(l) + 1
			if charCount > pos {
				lineNo = i + 1
				break
			}
		}

		e := &CodeEntry{
			FilePath:   filePath,
			Symbol:     typeName,
			Kind:       "type",
			Line:       lineNo,
			LineEnd:    min(lineNo+15, len(lines)),
			Content:    strings.Join(lines[lineNo-1:min(lineNo+15, len(lines))], "\n"),
			Importance: 0.6,
		}
		e.Keywords = tokenize(typeName + " " + e.Content[:min(200, len(e.Content))])
		ci.addEntry(e)
	}

	// 重要注释/docstring 提取
	commentPattern := regexp.MustCompile(`(?://|#)\s*(TODO|FIXME|HACK|XXX|NOTE|IMPORTANT|核心|认证|auth|route|路由|middleware|中间件|config|配置|数据库|database|API|handler)\s*(.*)`)
	for _, m := range commentPattern.FindAllStringSubmatch(content, -1) {
		if len(m) < 3 {
			continue
		}
		commentText := strings.TrimSpace(m[2])
		if len(commentText) < 5 {
			continue
		}

		pos := strings.Index(content, m[0])
		lineNo := 1
		charCount := 0
		for i, l := range lines {
			charCount += len(l) + 1
			if charCount > pos {
				lineNo = i + 1
				break
			}
		}

		e := &CodeEntry{
			FilePath:   filePath,
			Symbol:     m[1],
			Kind:       "comment",
			Line:       lineNo,
			LineEnd:    lineNo,
			Comment:    commentText,
			Content:    commentText,
			Importance: 0.4,
		}
		e.Keywords = tokenize(m[1] + " " + commentText)
		ci.addEntry(e)
	}

	// 结构体字段标签（Go）
	if ext == ".go" {
		tagPattern := regexp.MustCompile(`(\w+)\s+\w+\s+\x60json:"([^"]*)"[^` + "`" + `]*\x60`)
		for _, m := range tagPattern.FindAllStringSubmatch(content, -1) {
			if len(m) < 3 {
				continue
			}
			fieldName := m[1]
			jsonTag := m[2]

			pos := strings.Index(content, m[0])
			lineNo := 1
			charCount := 0
			for i, l := range lines {
				charCount += len(l) + 1
				if charCount > pos {
					lineNo = i + 1
					break
				}
			}

			e := &CodeEntry{
				FilePath:   filePath,
				Symbol:     fieldName,
				Kind:       "field",
				Line:       lineNo,
				LineEnd:    lineNo,
				Content:    fmt.Sprintf("JSON tag: %s", jsonTag),
				Importance: 0.3,
			}
			e.Keywords = tokenize(fieldName + " " + jsonTag)
			ci.addEntry(e)
		}
	}
}

func (ci *CodeIndexer) addEntry(e *CodeEntry) {
	idx := len(ci.entries)
	ci.entries = append(ci.entries, e)
	for _, kw := range e.Keywords {
		ci.invertedIndex[kw] = append(ci.invertedIndex[kw], idx)
	}
}

// Search 语义搜索（关键词 + 概念匹配）
func (ci *CodeIndexer) Search(query string, maxResults int) []SemSearchResult {
	ci.mu.RLock()
	defer ci.mu.RUnlock()

	if !ci.indexed {
		return nil
	}

	queryTokens := tokenize(query)
	if len(queryTokens) == 0 {
		return nil
	}

	// 计算每个 entry 的相关性分数（关键词 + 概念双通道）
	scores := make(map[int]float64)

	for _, qt := range queryTokens {
		indices, ok := ci.invertedIndex[qt]
		if !ok {
			// 前缀匹配
			for kw, idxs := range ci.invertedIndex {
				if strings.HasPrefix(kw, qt) || strings.HasPrefix(qt, kw) {
					for _, idx := range idxs {
						scores[idx] += 0.3 // 前缀匹配权重
					}
				}
			}
			continue
		}
		for _, idx := range indices {
			scores[idx] += 1.0 // 精确关键词匹配
		}
	}

	// 语义概念匹配通道（AI 提取的概念）
	if ci.conceptsGenerated {
		// 将查询本身也做 tokenize（用于概念匹配）
		queryLower := strings.ToLower(query)
		for idx, entry := range ci.entries {
			if len(entry.SemanticConcepts) == 0 {
				continue
			}
			conceptScore := 0.0
			for _, concept := range entry.SemanticConcepts {
				conceptLower := strings.ToLower(concept)
				// 查询包含该概念
				if strings.Contains(queryLower, conceptLower) {
					conceptScore += 2.0
				}
				// 概念的 token 匹配查询 token
				for _, qt := range queryTokens {
					if strings.Contains(conceptLower, qt) {
						conceptScore += 1.5
					}
				}
			}
			if conceptScore > 0 {
				scores[idx] += conceptScore
			}
		}
	}

	// 按分数排序
	type scored struct {
		idx   int
		score float64
	}
	var ranked []scored
	for idx, score := range scores {
		ranked = append(ranked, scored{idx, score})
	}
	sort.Slice(ranked, func(i, j int) bool {
		return ranked[i].score > ranked[j].score
	})

	// 构造结果
	if maxResults <= 0 {
		maxResults = 10
	}
	if maxResults > len(ranked) {
		maxResults = len(ranked)
	}

	var results []SemSearchResult
	for i := 0; i < maxResults; i++ {
		entry := ci.entries[ranked[i].idx]
		score := ranked[i].score * entry.Importance

		// 构造匹配信息
		var matchOn []string
		for _, kw := range entry.Keywords {
			for _, qt := range queryTokens {
				if strings.Contains(kw, qt) || strings.Contains(qt, kw) {
					matchOn = append(matchOn, kw)
					break
				}
			}
		}
		// 附加匹配的概念
		for _, c := range entry.SemanticConcepts {
			if strings.Contains(strings.ToLower(query), strings.ToLower(c)) {
				matchOn = append(matchOn, "["+c+"]")
			}
		}

		results = append(results, SemSearchResult{
			Score:   score,
			Entry:   entry,
			Snippet: formatSnippet(entry),
			MatchOn: matchOn,
		})
	}

	return results
}

func formatSnippet(e *CodeEntry) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("[%s] %s:%d", e.Kind, e.FilePath, e.Line))
	if e.Signature != "" {
		sb.WriteString(fmt.Sprintf(" %s", e.Signature))
	}
	if e.Comment != "" {
		commentPreview := e.Comment
		if len(commentPreview) > 120 {
			commentPreview = commentPreview[:120] + "..."
		}
		sb.WriteString(fmt.Sprintf("\n  📝 %s", commentPreview))
	}
	if e.Content != "" && e.Kind != "comment" {
		contentPreview := strings.ReplaceAll(strings.TrimSpace(e.Content), "\n", "\n  ")
		if len(contentPreview) > 300 {
			contentPreview = contentPreview[:300] + "..."
		}
		sb.WriteString(fmt.Sprintf("\n  %s", contentPreview))
	}
	return sb.String()
}

// SemSearch 对外接口
func (ci *CodeIndexer) SemSearch(query string) string {
	results := ci.Search(query, 10)

	if len(results) == 0 {
		return fmt.Sprintf("未找到与 '%s' 语义相关的代码。\n建议：尝试更具体的关键词。", query)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("🔍 语义搜索: %s\n", query))
	sb.WriteString(fmt.Sprintf("找到 %d 条相关结果:\n\n", len(results)))

	for i, r := range results {
		if i > 8 {
			sb.WriteString(fmt.Sprintf("... 共 %d 条结果，显示前 %d 条\n", len(results), 9))
			break
		}
		sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, r.Snippet))
		if i < len(results)-1 {
			sb.WriteString("\n")
		}
	}

	return sb.String()
}

// GenerateConcepts 使用 AI 为关键代码条目提取语义概念
// 只对 function/type 等有价值条目生成（跳过 file/package/field）
func (ci *CodeIndexer) GenerateConcepts(ctx context.Context) error {
	if ci.client == nil {
		return fmt.Errorf("AI client 未设置，无法生成语义概念")
	}

	ci.mu.RLock()
	if ci.conceptsGenerated {
		ci.mu.RUnlock()
		return nil
	}

	// 筛选有价值的条目（function/type，限定最多 30 个）
	var targets []struct {
		idx   int
		entry *CodeEntry
	}
	for i, e := range ci.entries {
		if e.Kind == "function" || e.Kind == "type" {
			targets = append(targets, struct {
				idx   int
				entry *CodeEntry
			}{i, e})
			if len(targets) >= 30 {
				break
			}
		}
	}
	ci.mu.RUnlock()

	if len(targets) == 0 {
		ci.mu.Lock()
		ci.conceptsGenerated = true
		ci.mu.Unlock()
		return nil
	}

	// 构造批量提取 prompt
	var codeList strings.Builder
	for i, t := range targets {
		codeList.WriteString(fmt.Sprintf("%d. [%s] %s:%d %s\n",
			i+1, t.entry.Kind, t.entry.FilePath, t.entry.Line, t.entry.Symbol))
		if t.entry.Comment != "" {
			codeList.WriteString(fmt.Sprintf("   Comment: %s\n", t.entry.Comment[:min(120, len(t.entry.Comment))]))
		}
		contentPreview := t.entry.Content
		if len(contentPreview) > 200 {
			contentPreview = contentPreview[:200]
		}
		codeList.WriteString(fmt.Sprintf("   Code: %s\n", strings.ReplaceAll(contentPreview, "\n", " ")))
	}

	prompt := fmt.Sprintf(`Extract semantic concepts from these code elements. For each, output 2-5 keywords/phrases in Chinese and English (e.g., authentication, JWT验证, database query, 数据库连接).

%s

Output format (JSON array):
[
  {"id": 1, "concepts": ["concept1", "concept2", ...]},
  {"id": 2, "concepts": [...]},
  ...
]

Only output the JSON array, no explanation.`, codeList.String())

	// 调用 AI
	timeoutCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	response, err := ci.client.Chat(timeoutCtx,
		"You extract semantic concepts from code. Output valid JSON array only.",
		prompt, "")
	if err != nil {
		fmt.Printf("[SemSearch] 概念生成失败 (AI): %v, 回退到关键词模式\n", err)
		ci.mu.Lock()
		ci.conceptsGenerated = true // 标记已尝试，不再重试
		ci.mu.Unlock()
		return nil // 不阻断搜索功能
	}

	// 解析 JSON
	response = strings.TrimSpace(response)
	// 去掉可能的 markdown 包裹
	if idx := strings.Index(response, "["); idx > 0 {
		response = response[idx:]
	}
	if idx := strings.LastIndex(response, "]"); idx > 0 {
		response = response[:idx+1]
	}

	type conceptEntry struct {
		ID       int      `json:"id"`
		Concepts []string `json:"concepts"`
	}
	var concepts []conceptEntry
	if err := json.Unmarshal([]byte(response), &concepts); err != nil {
		fmt.Printf("[SemSearch] JSON 解析失败: %v, response: %.200q\n", err, response)
		ci.mu.Lock()
		ci.conceptsGenerated = true
		ci.mu.Unlock()
		return nil
	}

	// 写入概念
	ci.mu.Lock()
	for _, c := range concepts {
		if c.ID >= 1 && c.ID <= len(targets) {
			targets[c.ID-1].entry.SemanticConcepts = c.Concepts
		}
	}
	ci.conceptsGenerated = true
	ci.mu.Unlock()

	fmt.Printf("[SemSearch] ✅ 语义概念生成完成: %d 条目\n", len(concepts))
	return nil
}

// Stats 返回索引统计
func (ci *CodeIndexer) Stats() string {
	ci.mu.RLock()
	defer ci.mu.RUnlock()

	if !ci.indexed {
		return "索引未构建"
	}

	typeCounts := map[string]int{}
	for _, e := range ci.entries {
		typeCounts[e.Kind]++
	}

	return fmt.Sprintf("索引统计: %d 条目 (%s)", len(ci.entries),
		fmt.Sprintf("function:%d type:%d file:%d comment:%d",
			typeCounts["function"], typeCounts["type"],
			typeCounts["file"], typeCounts["comment"]))
}
