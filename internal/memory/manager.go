package memory

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

type MemoryManager struct {
	db          *sql.DB
	projectPath string
	mu          sync.RWMutex
	
	workingMemory *WorkingMemory
}

func NewMemoryManager(projectPath string) (*MemoryManager, error) {
	mm := &MemoryManager{
		projectPath: projectPath,
		workingMemory: NewWorkingMemory(),
	}
	
	if err := mm.initDB(); err != nil {
		return nil, fmt.Errorf("初始化数据库失败：%v", err)
	}
	
	return mm, nil
}

func (mm *MemoryManager) initDB() error {
	dbPath := filepath.Join(mm.projectPath, ".argus", "memory.db")
	
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return fmt.Errorf("创建数据库目录失败：%v", err)
	}
	
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return err
	}
	
	db.SetMaxOpenConns(1)
	db.SetConnMaxLifetime(time.Hour)
	
	if err := db.Ping(); err != nil {
		return err
	}
	
	mm.db = db
	return mm.createSchema()
}

func (mm *MemoryManager) createSchema() error {
	schema := `
	CREATE TABLE IF NOT EXISTS tasks (
		id TEXT PRIMARY KEY,
		goal TEXT NOT NULL,
		description TEXT,
		status TEXT NOT NULL DEFAULT 'pending',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		started_at DATETIME,
		completed_at DATETIME,
		failed_at DATETIME,
		failure_reason TEXT,
		total_turns INTEGER DEFAULT 0,
		total_changes INTEGER DEFAULT 0,
		project_path TEXT
	);

	CREATE TABLE IF NOT EXISTS conversations (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		task_id TEXT NOT NULL,
		turn_number INTEGER NOT NULL,
		role TEXT NOT NULL,
		content TEXT NOT NULL,
		summary TEXT,
		key_decisions TEXT,
		files_mentioned TEXT,
		timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
		is_compressed BOOLEAN DEFAULT FALSE,
		FOREIGN KEY (task_id) REFERENCES tasks(id)
	);

	CREATE UNIQUE INDEX IF NOT EXISTS idx_conversations_task_turn 
	ON conversations(task_id, turn_number);

	CREATE VIRTUAL TABLE IF NOT EXISTS conversations_fts 
	USING fts5(content, summary, key_decisions, content_rowid=id);

	CREATE TRIGGER IF NOT EXISTS conversations_ai_insert 
	AFTER INSERT ON conversations BEGIN
		INSERT INTO conversations_fts(rowid, content, summary, key_decisions) 
		VALUES (new.id, new.content, new.summary, new.key_decisions);
	END;

	CREATE TRIGGER IF NOT EXISTS conversations_ai_update 
	AFTER UPDATE ON conversations BEGIN
		UPDATE conversations_fts 
		SET content = new.content, 
			summary = new.summary, 
			key_decisions = new.key_decisions
		WHERE rowid = new.id;
	END;

	CREATE TRIGGER IF NOT EXISTS conversations_ai_delete 
	AFTER DELETE ON conversations BEGIN
		DELETE FROM conversations_fts WHERE rowid = old.id;
	END;

	CREATE TABLE IF NOT EXISTS file_changes (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		task_id TEXT NOT NULL,
		conversation_id INTEGER,
		file_path TEXT NOT NULL,
		change_type TEXT NOT NULL,
		diff_summary TEXT,
		diff_content TEXT,
		lines_added INTEGER,
		lines_deleted INTEGER,
		timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (task_id) REFERENCES tasks(id),
		FOREIGN KEY (conversation_id) REFERENCES conversations(id)
	);

	CREATE INDEX IF NOT EXISTS idx_file_changes_path 
	ON file_changes(file_path);

	CREATE INDEX IF NOT EXISTS idx_file_changes_task 
	ON file_changes(task_id, timestamp);

	CREATE TABLE IF NOT EXISTS project_knowledge (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		task_id TEXT,
		category TEXT NOT NULL,
		title TEXT NOT NULL,
		content TEXT NOT NULL,
		tags TEXT,
		confidence FLOAT DEFAULT 1.0,
		source_type TEXT,
		source_id INTEGER,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		verified_at DATETIME,
		FOREIGN KEY (task_id) REFERENCES tasks(id)
	);

	CREATE INDEX IF NOT EXISTS idx_knowledge_category 
	ON project_knowledge(category, task_id);

	CREATE TABLE IF NOT EXISTS pending_issues (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		task_id TEXT NOT NULL,
		conversation_id INTEGER,
		description TEXT NOT NULL,
		severity TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'open',
		related_files TEXT,
		error_message TEXT,
		proposed_solution TEXT,
		resolved_at DATETIME,
		resolution_notes TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (task_id) REFERENCES tasks(id),
		FOREIGN KEY (conversation_id) REFERENCES conversations(id)
	);

	CREATE INDEX IF NOT EXISTS idx_issues_status 
	ON pending_issues(task_id, status, severity);

	CREATE TABLE IF NOT EXISTS checkpoints (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		task_id TEXT NOT NULL,
		conversation_id INTEGER,
		description TEXT,
		files_snapshot TEXT,
		context_summary TEXT,
		timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (task_id) REFERENCES tasks(id),
		FOREIGN KEY (conversation_id) REFERENCES conversations(id)
	);
	`
	
	_, err := mm.db.Exec(schema)
	return err
}

func (mm *MemoryManager) Close() error {
	if mm.db != nil {
		return mm.db.Close()
	}
	return nil
}

func (mm *MemoryManager) GetWorkingMemory() *WorkingMemory {
	return mm.workingMemory
}
