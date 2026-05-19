package ai

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestSEProcessor() *SEProcessor {
	return &SEProcessor{}
}

func TestExtractActions_ValidJSON(t *testing.T) {
	s := newTestSEProcessor()
	raw := `{"actions":[{"type":"write_file","path":"main.go","content":"package main"},{"type":"exec","command":"go run main.go"}]}`

	actions := s.extractActions(raw)
	require.Len(t, actions, 2)
	assert.Equal(t, "write_file", actions[0].Type)
	assert.Equal(t, "main.go", actions[0].Path)
	assert.Equal(t, "exec", actions[1].Type)
	assert.Equal(t, "go run main.go", actions[1].Command)
}

func TestExtractActions_EmptyActions(t *testing.T) {
	s := newTestSEProcessor()
	raw := `{"actions":[]}`

	actions := s.extractActions(raw)
	assert.Empty(t, actions)
}

func TestExtractActions_WithMarkdownFence(t *testing.T) {
	s := newTestSEProcessor()
	raw := "some text\n```json\n{\"actions\":[{\"type\":\"exec\",\"command\":\"ls\"}]}\n```\nmore text"

	actions := s.extractActions(raw)
	require.Len(t, actions, 1)
	assert.Equal(t, "exec", actions[0].Type)
}

func TestExtractActions_NoJSON_ReturnsEmpty(t *testing.T) {
	s := newTestSEProcessor()
	raw := "this is just plain text with no json at all"

	actions := s.extractActions(raw)
	assert.Empty(t, actions)
}

func TestExtractActions_InvalidJSON_Ignores(t *testing.T) {
	s := newTestSEProcessor()
	raw := `{"actions":[{"type":"write_file","path":"main.go"`

	actions := s.extractActions(raw)
	assert.Empty(t, actions)
}

func TestExtractActions_NestedInText(t *testing.T) {
	s := newTestSEProcessor()
	raw := `I'll create the file now:
{"actions":[{"type":"write_file","path":"hello.go","content":"package main"}]}
Done!`

	actions := s.extractActions(raw)
	require.Len(t, actions, 1)
	assert.Equal(t, "hello.go", actions[0].Path)
}

func TestExtractCompletion_Success(t *testing.T) {
	s := newTestSEProcessor()
	raw := `Task done!
{"completed":true,"technical_notes":"All tests pass","changelog_draft":"Added hello world","status":"success"}`

	completion := s.extractCompletion(raw)
	assert.NotNil(t, completion)
	assert.Equal(t, "success", completion.Status)
}

func TestExtractCompletion_NoCompletion(t *testing.T) {
	s := newTestSEProcessor()
	raw := `Still working on it...`

	completion := s.extractCompletion(raw)
	assert.Nil(t, completion)
}

func TestCheckNeedHelp_NeedsHelp(t *testing.T) {
	s := newTestSEProcessor()
	cases := []string{
		"这个需求需要PM确认",
		"我不确定怎么实现",
		"请PM确认一下",
		"需要帮助",
	}

	for _, text := range cases {
		needsHelp := s.checkNeedHelp(text)
		assert.True(t, needsHelp, "should detect need help in: %s", text)
	}
}

func TestCheckNeedHelp_NoHelpNeeded(t *testing.T) {
	s := newTestSEProcessor()
	cases := []string{
		"I will create the file now",
		"All tests are passing",
		"Task completed successfully",
		"Starting implementation",
	}

	for _, text := range cases {
		needsHelp := s.checkNeedHelp(text)
		assert.False(t, needsHelp, "should NOT detect need help in: %s", text)
	}
}

func TestSEAction_RoundTrip(t *testing.T) {
	original := []SEAction{
		{Type: "write_file", Path: "test.go", Content: "package main"},
		{Type: "exec", Command: "go test ./..."},
	}

	data, err := json.Marshal(original)
	require.NoError(t, err)

	var parsed []SEAction
	err = json.Unmarshal(data, &parsed)
	require.NoError(t, err)

	assert.Len(t, parsed, 2)
	assert.Equal(t, original[0].Type, parsed[0].Type)
	assert.Equal(t, original[0].Path, parsed[0].Path)
	assert.Equal(t, original[1].Command, parsed[1].Command)
}

func TestExtractActions_RealWorldExample(t *testing.T) {
	s := newTestSEProcessor()
	response := `I've analyzed your request. Here's my plan: {"actions":[{"type":"write_file","path":"hello.go","content":"package main"},{"type":"exec","command":"go run hello.go"}]}`

	actions := s.extractActions(response)
	require.Len(t, actions, 2)
	assert.Equal(t, "write_file", actions[0].Type)
	assert.Equal(t, "hello.go", actions[0].Path)
	assert.Equal(t, "exec", actions[1].Type)
}

func TestCheckNeedHelp_CaseInsensitive(t *testing.T) {
	s := newTestSEProcessor()
	cases := map[string]bool{
		"我需要帮助":           true,
		"请PM确认":             true,
		"不确定":               true,
		"I'm good to proceed":   false,
		"Starting implementation": false,
	}

	for text, expectNeeds := range cases {
		needsHelp := s.checkNeedHelp(text)
		assert.Equal(t, expectNeeds, needsHelp, "mismatch for: %s", text)
	}
}

func TestExtractActions_PythonWithIndentation(t *testing.T) {
	s := newTestSEProcessor()
	raw := `{"actions":[{"type":"write_file","path":"lru.py","content":"class LRUCache:\n    def __init__(self, capacity):\n        self.capacity = capacity\n        self.cache = {}\n\n    def get(self, key):\n        if key not in self.cache:\n            return -1\n        return self.cache[key]"}]}`

	actions := s.extractActions(raw)
	require.Len(t, actions, 1)
	assert.Equal(t, "write_file", actions[0].Type)
	assert.Equal(t, "lru.py", actions[0].Path)
	assert.Contains(t, actions[0].Content, "    def __init__")
	assert.Contains(t, actions[0].Content, "        self.capacity")
	assert.Contains(t, actions[0].Content, "    def get")
}

func TestExtractActions_FixRealNewlinesInJSON(t *testing.T) {
	s := newTestSEProcessor()
	raw := "{\"actions\":[{\"type\":\"write_file\",\"path\":\"test.py\",\"content\":\"class Foo:\n    def bar(self):\n        return 42\"}]}"

	actions := s.extractActions(raw)
	require.Len(t, actions, 1)
	assert.Contains(t, actions[0].Content, "    def bar")
	assert.Contains(t, actions[0].Content, "        return 42")
}

func TestExtractActions_CodeBlockFallback(t *testing.T) {
	s := newTestSEProcessor()
	raw := "I'll write the code:\n```python\nclass LRUCache:\n    def __init__(self, capacity):\n        self.capacity = capacity\n```\nDone!"

	actions := s.extractActions(raw)
	require.Len(t, actions, 1)
	assert.Equal(t, "write_file", actions[0].Type)
	assert.Equal(t, "main.py", actions[0].Path)
	assert.Contains(t, actions[0].Content, "    def __init__")
	assert.Contains(t, actions[0].Content, "        self.capacity")
}

func TestExtractActions_GoCodeBlockFallback(t *testing.T) {
	s := newTestSEProcessor()
	raw := "```go\npackage main\n\nimport \"fmt\"\n\nfunc main() {\n    fmt.Println(\"hello\")\n}\n```"

	actions := s.extractActions(raw)
	require.Len(t, actions, 1)
	assert.Equal(t, "main.go", actions[0].Path)
	assert.Contains(t, actions[0].Content, "    fmt.Println")
}
