package types

import (
	"fmt"
	"time"
)

type TaskItemDef struct {
	Text string
}

type ShellEventEmitter interface {
	PushShellStart(role, taskId string, taskIndex int, cmdType, command string, extra map[string]string)
	PushShellOutput(taskId, output string)
	PushShellDone(role, taskId string, exitCode int, duration string, status string)
	GetLastShellTimestamp() int64
}

type TaskItem struct {
	ID          string `json:"id"`
	Text        string `json:"text"`
	Status      string `json:"status"`
	StartedAt   int64  `json:"started_at"`
	CompletedAt int64  `json:"completed_at"`
	Duration    string `json:"duration"`
	Error       string `json:"error,omitempty"`
}

type TaskList struct {
	ID        string      `json:"id"`
	Role      string      `json:"role"`
	Title     string      `json:"title"`
	Tasks     []TaskItem  `json:"tasks"`
	Status    string      `json:"status"`
	StartedAt int64       `json:"started_at"`
	EndedAt   int64       `json:"ended_at"`
}

type ShellBlock struct {
	TaskID    string            `json:"task_id"`
	Type      string            `json:"type"`
	Command   string            `json:"command"`
	Output    string            `json:"output"`
	ExitCode  int               `json:"exit_code"`
	Duration  string            `json:"duration"`
	Status    string            `json:"status"`
	Timestamp int64             `json:"timestamp"`
	Extra     map[string]string `json:"extra,omitempty"`
}

type CodeBlock struct {
	Lang     string `json:"lang"`
	Code     string `json:"code"`
	Copyable bool   `json:"copyable"`
}

type ResultBlock struct {
	Text       string      `json:"text"`
	CodeBlocks []CodeBlock  `json:"code_blocks,omitempty"`
	JSONData   interface{} `json:"json_data,omitempty"`
}

func FormatDuration(start, end int64) string {
	d := time.Duration(end - start)
	if d < time.Second {
		return fmt.Sprintf("%.0fms", float64(d)/float64(time.Millisecond))
	}
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", float64(d)/float64(time.Second))
	}
	return d.String()
}
