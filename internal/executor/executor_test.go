package executor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"argus/internal/board"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func readFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	return string(data), err
}

func writeFile(path, content string) {
	os.WriteFile(path, []byte(content), 0644)
}

func newTestExecutor(t *testing.T) (*Executor, string) {
	t.Helper()
	tmpDir := t.TempDir()
	bm := board.NewManager(filepath.Join(tmpDir, "board.json"))
	e := NewExecutor(tmpDir, bm)
	return e, tmpDir
}

func TestWriteFile_RelativePath(t *testing.T) {
	e, tmpDir := newTestExecutor(t)

	err := e.WriteFile("test.txt", "hello world")
	assert.NoError(t, err)

	data, err := readFile(filepath.Join(tmpDir, "test.txt"))
	assert.NoError(t, err)
	assert.Equal(t, "hello world", data)
}

func TestWriteFile_AbsolutePath_WithinWorkDir(t *testing.T) {
	e, tmpDir := newTestExecutor(t)

	absPath := filepath.Join(tmpDir, "subdir", "abs.txt")
	err := e.WriteFile(absPath, "absolute content")
	assert.NoError(t, err)

	data, err := readFile(absPath)
	assert.NoError(t, err)
	assert.Equal(t, "absolute content", data)
}

func TestWriteFile_AbsolutePath_OutsideWorkDir_Rejected(t *testing.T) {
	e, _ := newTestExecutor(t)

	err := e.WriteFile(`C:\Windows\temp\outside.txt`, "should fail")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "path outside work directory")
}

func TestWriteFile_CreateSubdir(t *testing.T) {
	e, tmpDir := newTestExecutor(t)

	err := e.WriteFile("deep/nested/file.txt", "nested")
	assert.NoError(t, err)

	data, err := readFile(filepath.Join(tmpDir, "deep", "nested", "file.txt"))
	assert.NoError(t, err)
	assert.Equal(t, "nested", data)
}

func TestReadFile_Exists(t *testing.T) {
	e, tmpDir := newTestExecutor(t)
	writeFile(filepath.Join(tmpDir, "existing.txt"), "read me")

	content, err := e.ReadFile("existing.txt")
	assert.NoError(t, err)
	assert.Equal(t, "read me", content)
}

func TestReadFile_AbsolutePath(t *testing.T) {
	e, tmpDir := newTestExecutor(t)
	absPath := filepath.Join(tmpDir, "abs_read.txt")
	writeFile(absPath, "abs read")

	content, err := e.ReadFile(absPath)
	assert.NoError(t, err)
	assert.Equal(t, "abs read", content)
}

func TestReadFile_NotFound(t *testing.T) {
	e, _ := newTestExecutor(t)

	_, err := e.ReadFile("nonexistent.txt")
	assert.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "find")
}

func TestExec_SimpleCommand(t *testing.T) {
	e, _ := newTestExecutor(t)

	output, err := e.Exec("echo hello_exec", 10*time.Second)
	assert.NoError(t, err)
	assert.Contains(t, output, "hello_exec")
}

func TestExec_WorkingDirectory(t *testing.T) {
	e, tmpDir := newTestExecutor(t)

	output, err := e.Exec("cd", 5*time.Second)
	assert.NoError(t, err)
	assert.Contains(t, strings.ToLower(output), strings.ToLower(tmpDir))
}

func TestExec_Timeout(t *testing.T) {
	e, _ := newTestExecutor(t)

	start := time.Now()
	_, err := e.Exec("ping -n 10 127.0.0.1", 2*time.Second)
	elapsed := time.Since(start)

	assert.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "timeout")
	t.Logf("timeout test took %v (expected ~2s+overhead)", elapsed)
}

func TestExec_FailingCommand(t *testing.T) {
	e, _ := newTestExecutor(t)

	_, err := e.Exec("exit /b 1", 5*time.Second)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "command failed")
}

func TestExec_MultiLineOutput(t *testing.T) {
	e, _ := newTestExecutor(t)

	output, err := e.Exec("echo line1 && echo line2 && echo line3", 5*time.Second)
	assert.NoError(t, err)
	assert.Contains(t, output, "line1")
	assert.Contains(t, output, "line2")
	assert.Contains(t, output, "line3")
}

func TestDeleteFile(t *testing.T) {
	e, tmpDir := newTestExecutor(t)
	writeFile(filepath.Join(tmpDir, "to_delete.txt"), "delete me")

	err := e.DeleteFile("to_delete.txt")
	assert.NoError(t, err)

	_, err = e.ReadFile("to_delete.txt")
	assert.Error(t, err)
}

func TestListFiles(t *testing.T) {
	e, tmpDir := newTestExecutor(t)
	writeFile(filepath.Join(tmpDir, "a.go"), "// a")
	writeFile(filepath.Join(tmpDir, "b.py"), "# b")

	files, err := e.ListFiles()
	require.NoError(t, err)
	assert.Len(t, files, 2)
}

func TestOnFileWritten_Callback(t *testing.T) {
	e, _ := newTestExecutor(t)

	var capturedPath string
	e.SetOnFileWritten(func(path string) { capturedPath = path })

	err := e.WriteFile("callback_test.txt", "data")
	assert.NoError(t, err)
	assert.Equal(t, "callback_test.txt", capturedPath)
}

func TestE2E_HelloWorld_Scenario(t *testing.T) {
	e, _ := newTestExecutor(t)

	helloCode := "package main\nimport \"fmt\"\nfunc main() {\n\tfmt.Println(\"Hello, Argus!\")\n}\n"

	err := e.WriteFile("main.go", helloCode)
	assert.NoError(t, err, "[E2E] 步骤1: 写入文件")

	content, err := e.ReadFile("main.go")
	assert.NoError(t, err, "[E2E] 步骤2: 读取验证")
	assert.Contains(t, content, "Hello, Argus!")

	output, err := e.Exec("echo Hello, Argus!", 5*time.Second)
	assert.NoError(t, err, "[E2E] 步骤3: 命令执行")
	assert.Contains(t, output, "Hello, Argus!", "[E2E] 输出验证")

	files, err := e.ListFiles()
	assert.NoError(t, err)
	found := false
	for _, f := range files {
		if f.Path == "main.go" {
			found = true
			break
		}
	}
	assert.True(t, found, "main.go 应该在文件列表中")

	err = e.DeleteFile("main.go")
	assert.NoError(t, err, "[E2E] 步骤4: 清理")
}
