package limiter

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"argus/internal/types"
)

// Logger 操作日志记录器
type Logger struct {
	mu       sync.Mutex
	logPath  string
	file     *os.File
}

// NewLogger 创建日志记录器
func NewLogger(logDir string) (*Logger, error) {
	logPath := filepath.Join(logDir, "commands.log")
	
	// 确保目录存在
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, err
	}

	// 以追加模式打开文件
	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}

	return &Logger{
		logPath: logPath,
		file:    file,
	}, nil
}

// Close 关闭日志文件
func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.file.Close()
}

// Log 记录日志条目
func (l *Logger) Log(entry types.LogEntry) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	// 设置时间
	if entry.Time.IsZero() {
		entry.Time = time.Now()
	}

	// 序列化为 JSON
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}

	// 写入文件（JSON Lines 格式）
	_, err = l.file.Write(append(data, '\n'))
	return err
}

// LogStart 记录操作开始
func (l *Logger) LogStart(opType, command, caller string) error {
	return l.Log(types.LogEntry{
		Type:    opType,
		Command: command,
		Caller:  caller,
		Status:  "start",
	})
}

// LogSuccess 记录操作成功
func (l *Logger) LogSuccess(opType, command, caller, output string) error {
	return l.Log(types.LogEntry{
		Type:    opType,
		Command: command,
		Caller:  caller,
		Status:  "success",
		Output:  output,
	})
}

// LogFailed 记录操作失败
func (l *Logger) LogFailed(opType, command, caller, errMsg string) error {
	return l.Log(types.LogEntry{
		Type:    opType,
		Command: command,
		Caller:  caller,
		Status:  "failed",
		Error:   errMsg,
	})
}

// LogRejected 记录操作被拒绝（限流或熔断）
func (l *Logger) LogRejected(opType, command, caller, reason string) error {
	return l.Log(types.LogEntry{
		Type:    opType,
		Command: command,
		Caller:  caller,
		Status:  "rejected",
		Reason:  reason,
	})
}

// ReadLogs 读取最近的日志
func (l *Logger) ReadLogs(limit int) ([]types.LogEntry, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	// 先关闭文件以便读取
	l.file.Close()
	defer func() {
		// 重新打开文件
		l.file, _ = os.OpenFile(l.logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	}()

	data, err := os.ReadFile(l.logPath)
	if err != nil {
		return nil, err
	}

	var entries []types.LogEntry
	lines := splitLines(data)
	
	// 从后往前读取
	start := len(lines) - limit
	if start < 0 {
		start = 0
	}

	for i := len(lines) - 1; i >= start; i-- {
		line := lines[i]
		if len(line) == 0 {
			continue
		}
		
		var entry types.LogEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			continue
		}
		entries = append(entries, entry)
	}

	return entries, nil
}

// splitLines 分割字节数组为行
func splitLines(data []byte) [][]byte {
	var lines [][]byte
	start := 0
	for i, b := range data {
		if b == '\n' {
			lines = append(lines, data[start:i])
			start = i + 1
		}
	}
	if start < len(data) {
		lines = append(lines, data[start:])
	}
	return lines
}
