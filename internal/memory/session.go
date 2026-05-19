package memory

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type ConversationRecord struct {
	ID           int64     `json:"id"`
	TaskID       string    `json:"task_id"`
	TurnNumber   int       `json:"turn_number"`
	Role         string    `json:"role"`
	Content      string    `json:"content"`
	Summary      string    `json:"summary,omitempty"`
	KeyDecisions string    `json:"key_decisions,omitempty"`
	FilesMentioned string  `json:"files_mentioned,omitempty"`
	Timestamp    time.Time `json:"timestamp"`
	IsCompressed bool      `json:"is_compressed"`
}

type FileChangeRecord struct {
	ID             int64     `json:"id"`
	TaskID         string    `json:"task_id"`
	ConversationID int64     `json:"conversation_id"`
	FilePath       string    `json:"file_path"`
	ChangeType     string    `json:"change_type"`
	DiffSummary    string    `json:"diff_summary"`
	DiffContent    string    `json:"diff_content"`
	LinesAdded     int       `json:"lines_added"`
	LinesDeleted   int       `json:"lines_deleted"`
	Timestamp      time.Time `json:"timestamp"`
}

type ProjectKnowledge struct {
	ID          int64     `json:"id"`
	TaskID      string    `json:"task_id"`
	Category    string    `json:"category"`
	Title       string    `json:"title"`
	Content     string    `json:"content"`
	Tags        []string  `json:"tags"`
	Confidence  float64   `json:"confidence"`
	SourceType  string    `json:"source_type"`
	SourceID    int64     `json:"source_id"`
	CreatedAt   time.Time `json:"created_at"`
	VerifiedAt  time.Time `json:"verified_at"`
}

type PendingIssue struct {
	ID               int64     `json:"id"`
	TaskID           string    `json:"task_id"`
	ConversationID   int64     `json:"conversation_id"`
	Description      string    `json:"description"`
	Severity         string    `json:"severity"`
	Status           string    `json:"status"`
	RelatedFiles     []string  `json:"related_files"`
	ErrorMessage     string    `json:"error_message"`
	ProposedSolution string    `json:"proposed_solution"`
	ResolvedAt       time.Time `json:"resolved_at"`
	ResolutionNotes  string    `json:"resolution_notes"`
	CreatedAt        time.Time `json:"created_at"`
}

func (mm *MemoryManager) AddConversation(taskID string, turnNumber int, role, content string, keyDecisions map[string]interface{}, filesMentioned []string) error {
	decisionsJSON := ""
	if len(keyDecisions) > 0 {
		b, _ := json.Marshal(keyDecisions)
		decisionsJSON = string(b)
	}
	
	filesJSON := ""
	if len(filesMentioned) > 0 {
		b, _ := json.Marshal(filesMentioned)
		filesJSON = string(b)
	}
	
	_, err := mm.db.Exec(`
		INSERT INTO conversations (task_id, turn_number, role, content, key_decisions, files_mentioned)
		VALUES (?, ?, ?, ?, ?, ?)
	`, taskID, turnNumber, role, content, decisionsJSON, filesJSON)
	
	if err != nil {
		return fmt.Errorf("添加对话记录失败：%v", err)
	}
	
	mm.workingMemory.AddTurn(role, content)
	
	_, _ = mm.db.Exec("UPDATE tasks SET total_turns = total_turns + 1 WHERE id = ?", taskID)
	
	return nil
}

func (mm *MemoryManager) GetConversations(taskID string, limit int) ([]ConversationRecord, error) {
	query := `
		SELECT id, task_id, turn_number, role, content, summary, key_decisions, files_mentioned, timestamp, is_compressed
		FROM conversations
		WHERE task_id = ?
		ORDER BY turn_number DESC
		LIMIT ?
	`
	
	rows, err := mm.db.Query(query, taskID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	
	var conversations []ConversationRecord
	for rows.Next() {
		var c ConversationRecord
		err := rows.Scan(&c.ID, &c.TaskID, &c.TurnNumber, &c.Role, &c.Content, &c.Summary, &c.KeyDecisions, &c.FilesMentioned, &c.Timestamp, &c.IsCompressed)
		if err != nil {
			return nil, err
		}
		conversations = append(conversations, c)
	}
	
	for i, j := 0, len(conversations)-1; i < j; i, j = i+1, j-1 {
		conversations[i], conversations[j] = conversations[j], conversations[i]
	}
	
	return conversations, nil
}

func (mm *MemoryManager) SearchConversations(query string, limit int) ([]ConversationRecord, error) {
	ftsQuery := strings.ReplaceAll(query, "'", "''")
	
	sql := fmt.Sprintf(`
		SELECT c.id, c.task_id, c.turn_number, c.role, c.content, c.summary, c.key_decisions, c.files_mentioned, c.timestamp, c.is_compressed
		FROM conversations c
		INNER JOIN conversations_fts fts ON c.id = fts.rowid
		WHERE conversations_fts MATCH '%s'
		ORDER BY rank
		LIMIT %d
	`, ftsQuery, limit)
	
	rows, err := mm.db.Query(sql)
	if err != nil {
		return mm.searchFallback(query, limit)
	}
	defer rows.Close()
	
	var conversations []ConversationRecord
	for rows.Next() {
		var c ConversationRecord
		err := rows.Scan(&c.ID, &c.TaskID, &c.TurnNumber, &c.Role, &c.Content, &c.Summary, &c.KeyDecisions, &c.FilesMentioned, &c.Timestamp, &c.IsCompressed)
		if err != nil {
			return nil, err
		}
		conversations = append(conversations, c)
	}
	
	return conversations, nil
}

func (mm *MemoryManager) searchFallback(query string, limit int) ([]ConversationRecord, error) {
	sql := `
		SELECT id, task_id, turn_number, role, content, summary, key_decisions, files_mentioned, timestamp, is_compressed
		FROM conversations
		WHERE content LIKE ? OR summary LIKE ?
		ORDER BY timestamp DESC
		LIMIT ?
	`
	
	pattern := "%" + query + "%"
	rows, err := mm.db.Query(sql, pattern, pattern, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	
	var conversations []ConversationRecord
	for rows.Next() {
		var c ConversationRecord
		err := rows.Scan(&c.ID, &c.TaskID, &c.TurnNumber, &c.Role, &c.Content, &c.Summary, &c.KeyDecisions, &c.FilesMentioned, &c.Timestamp, &c.IsCompressed)
		if err != nil {
			return nil, err
		}
		conversations = append(conversations, c)
	}
	
	return conversations, nil
}

func (mm *MemoryManager) AddFileChange(taskID string, conversationID int64, filePath, changeType, diffSummary string, linesAdded, linesDeleted int) error {
	_, err := mm.db.Exec(`
		INSERT INTO file_changes (task_id, conversation_id, file_path, change_type, diff_summary, lines_added, lines_deleted)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, taskID, conversationID, filePath, changeType, diffSummary, linesAdded, linesDeleted)
	
	if err != nil {
		return fmt.Errorf("添加文件改动记录失败：%v", err)
	}
	
	_, _ = mm.db.Exec("UPDATE tasks SET total_changes = total_changes + 1 WHERE id = ?", taskID)
	
	return nil
}

func (mm *MemoryManager) GetFileChanges(taskID string, filePath string) ([]FileChangeRecord, error) {
	var rows interface{}
	var err error
	
	if filePath != "" {
		rows, err = mm.db.Query(`
			SELECT id, task_id, conversation_id, file_path, change_type, diff_summary, diff_content, lines_added, lines_deleted, timestamp
			FROM file_changes
			WHERE task_id = ? AND file_path = ?
			ORDER BY timestamp DESC
		`, taskID, filePath)
	} else {
		rows, err = mm.db.Query(`
			SELECT id, task_id, conversation_id, file_path, change_type, diff_summary, diff_content, lines_added, lines_deleted, timestamp
			FROM file_changes
			WHERE task_id = ?
			ORDER BY timestamp DESC
		`, taskID)
	}
	
	if err != nil {
		return nil, err
	}
	defer rows.(*sql.Rows).Close()
	
	sqlRows := rows.(*sql.Rows)
	var changes []FileChangeRecord
	for sqlRows.Next() {
		var c FileChangeRecord
		err := sqlRows.Scan(&c.ID, &c.TaskID, &c.ConversationID, &c.FilePath, &c.ChangeType, &c.DiffSummary, &c.DiffContent, &c.LinesAdded, &c.LinesDeleted, &c.Timestamp)
		if err != nil {
			return nil, err
		}
		changes = append(changes, c)
	}
	
	return changes, nil
}

func (mm *MemoryManager) AddKnowledge(taskID, category, title, content string, tags []string, confidence float64, sourceType string, sourceID int64) error {
	tagsJSON := ""
	if len(tags) > 0 {
		b, _ := json.Marshal(tags)
		tagsJSON = string(b)
	}
	
	_, err := mm.db.Exec(`
		INSERT INTO project_knowledge (task_id, category, title, content, tags, confidence, source_type, source_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, taskID, category, title, content, tagsJSON, confidence, sourceType, sourceID)
	
	return err
}

func (mm *MemoryManager) GetKnowledge(taskID, category string) ([]ProjectKnowledge, error) {
	var rows *sql.Rows
	var err error
	
	if category != "" {
		rows, err = mm.db.Query(`
			SELECT id, task_id, category, title, content, tags, confidence, source_type, source_id, created_at, verified_at
			FROM project_knowledge
			WHERE task_id = ? AND category = ?
			ORDER BY confidence DESC, created_at DESC
		`, taskID, category)
	} else {
		rows, err = mm.db.Query(`
			SELECT id, task_id, category, title, content, tags, confidence, source_type, source_id, created_at, verified_at
			FROM project_knowledge
			WHERE task_id = ?
			ORDER BY confidence DESC, created_at DESC
		`, taskID)
	}
	
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	
	var knowledge []ProjectKnowledge
	for rows.Next() {
		var k ProjectKnowledge
		var tagsJSON string
		err := rows.Scan(&k.ID, &k.TaskID, &k.Category, &k.Title, &k.Content, &tagsJSON, &k.Confidence, &k.SourceType, &k.SourceID, &k.CreatedAt, &k.VerifiedAt)
		if err != nil {
			return nil, err
		}
		if tagsJSON != "" {
			json.Unmarshal([]byte(tagsJSON), &k.Tags)
		}
		knowledge = append(knowledge, k)
	}
	
	return knowledge, nil
}

func (mm *MemoryManager) AddIssue(taskID, description, severity string, relatedFiles []string, errorMessage string) error {
	filesJSON := ""
	if len(relatedFiles) > 0 {
		b, _ := json.Marshal(relatedFiles)
		filesJSON = string(b)
	}
	
	_, err := mm.db.Exec(`
		INSERT INTO pending_issues (task_id, description, severity, related_files, error_message)
		VALUES (?, ?, ?, ?, ?)
	`, taskID, description, severity, filesJSON, errorMessage)
	
	return err
}

func (mm *MemoryManager) GetOpenIssues(taskID string) ([]PendingIssue, error) {
	rows, err := mm.db.Query(`
		SELECT id, task_id, conversation_id, description, severity, status, related_files, error_message, proposed_solution, resolved_at, resolution_notes, created_at
		FROM pending_issues
		WHERE task_id = ? AND status IN ('open', 'in_progress')
		ORDER BY 
			CASE severity 
				WHEN 'critical' THEN 1 
				WHEN 'major' THEN 2 
				WHEN 'minor' THEN 3 
				ELSE 4 
			END,
			created_at ASC
	`, taskID)
	
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	
	var issues []PendingIssue
	for rows.Next() {
		var i PendingIssue
		var filesJSON string
		err := rows.Scan(&i.ID, &i.TaskID, &i.ConversationID, &i.Description, &i.Severity, &i.Status, &filesJSON, &i.ErrorMessage, &i.ProposedSolution, &i.ResolvedAt, &i.ResolutionNotes, &i.CreatedAt)
		if err != nil {
			return nil, err
		}
		if filesJSON != "" {
			json.Unmarshal([]byte(filesJSON), &i.RelatedFiles)
		}
		issues = append(issues, i)
	}
	
	return issues, nil
}

func (mm *MemoryManager) ResolveIssue(issueID int64, resolutionNotes string) error {
	_, err := mm.db.Exec(`
		UPDATE pending_issues 
		SET status = 'resolved', resolved_at = CURRENT_TIMESTAMP, resolution_notes = ?
		WHERE id = ?
	`, resolutionNotes, issueID)
	
	return err
}

func (mm *MemoryManager) CreateTask(id, goal, description, projectPath string) error {
	_, err := mm.db.Exec(`
		INSERT INTO tasks (id, goal, description, project_path, status, started_at)
		VALUES (?, ?, ?, ?, 'running', CURRENT_TIMESTAMP)
	`, id, goal, description, projectPath)
	
	return err
}

func (mm *MemoryManager) CompleteTask(taskID string) error {
	_, err := mm.db.Exec(`
		UPDATE tasks 
		SET status = 'completed', completed_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, taskID)
	
	return err
}

func (mm *MemoryManager) FailTask(taskID, reason string) error {
	_, err := mm.db.Exec(`
		UPDATE tasks 
		SET status = 'failed', failed_at = CURRENT_TIMESTAMP, failure_reason = ?
		WHERE id = ?
	`, reason, taskID)
	
	return err
}
