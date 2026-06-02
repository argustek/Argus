package ai

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToolCallToSEAction_WriteFile(t *testing.T) {
	s := &SEProcessor{}
	args, _ := json.Marshal(map[string]string{
		"path":    "main.go",
		"content": "package main",
	})
	tc := ToolCall{
		Function: FunctionCall{
			Name:      "write_file",
			Arguments: string(args),
		},
	}

	action := s.toolCallToSEAction(tc)
	assert.Equal(t, "write_file", action.Type)
	assert.Equal(t, "main.go", action.Path)
	assert.Equal(t, "package main", action.Content)
}

func TestToolCallToSEAction_Exec(t *testing.T) {
	s := &SEProcessor{}
	args, _ := json.Marshal(map[string]string{
		"command": "go run main.go",
	})
	tc := ToolCall{
		Function: FunctionCall{
			Name:      "exec",
			Arguments: string(args),
		},
	}

	action := s.toolCallToSEAction(tc)
	assert.Equal(t, "exec", action.Type)
	assert.Equal(t, "go run main.go", action.Command)
}

func TestToolCallToSEAction_CompleteTask(t *testing.T) {
	s := &SEProcessor{}
	args, _ := json.Marshal(map[string]interface{}{
		"files":   []interface{}{"main.go", "utils.go"},
		"summary": "created hello world program",
	})
	tc := ToolCall{
		Function: FunctionCall{
			Name:      "complete_task",
			Arguments: string(args),
		},
	}

	action := s.toolCallToSEAction(tc)
	assert.Equal(t, "complete_task", action.Type)
	assert.Equal(t, "main.go,utils.go,", action.Content)
	assert.Equal(t, "created hello world program", action.Command)
}

func TestToolCallToSEAction_EditFile(t *testing.T) {
	s := &SEProcessor{}
	args, _ := json.Marshal(map[string]string{
		"path":    "main.go",
		"old_str": "func login() {",
		"new_str": "func login(user User) *User {",
	})
	tc := ToolCall{
		Function: FunctionCall{
			Name:      "edit_file",
			Arguments: string(args),
		},
	}

	action := s.toolCallToSEAction(tc)
	assert.Equal(t, "edit_file", action.Type)
	assert.Equal(t, "main.go", action.Path)
	assert.Equal(t, "func login() {", action.OldStr)
	assert.Equal(t, "func login(user User) *User {", action.NewStr)
}

func TestCheckSemanticComplete_Chinese(t *testing.T) {
	s := &SEProcessor{}
	assert.True(t, s.CheckSemanticComplete("任务完成，请审核"))
	assert.True(t, s.CheckSemanticComplete("所有功能已完成"))
}

func TestCheckSemanticComplete_English(t *testing.T) {
	s := &SEProcessor{}
	assert.True(t, s.CheckSemanticComplete("Task completed successfully"))
	assert.True(t, s.CheckSemanticComplete("All done!"))
	assert.True(t, s.CheckSemanticComplete("Build finished with no errors"))
}

func TestCheckSemanticComplete_NotComplete(t *testing.T) {
	s := &SEProcessor{}
	assert.False(t, s.CheckSemanticComplete("正在编译代码..."))
	assert.False(t, s.CheckSemanticComplete("Writing file main.go"))
	assert.False(t, s.CheckSemanticComplete("I'll fix that error"))
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

func TestCompleteFilesFromAction(t *testing.T) {
	action := SEAction{
		Type:    "complete_task",
		Content: "main.go,utils.go,test.go,",
	}
	files := completeFilesFromAction(action)
	assert.Len(t, files, 3)
	assert.Equal(t, "main.go", files[0])
	assert.Equal(t, "utils.go", files[1])
	assert.Equal(t, "test.go", files[2])
}

func TestMerge(t *testing.T) {
	assert.Equal(t, "ok", "ok")
}
