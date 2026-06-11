package main

import (
	"database/sql"
	"fmt"
	_ "github.com/mattn/go-sqlite3"
)

func main() {
	db, err := sql.Open("sqlite3", "E:/ArgusTek/Argus/.argus/memory.db")
	if err != nil {
		fmt.Println("打开数据库失败:", err)
		return
	}
	defer db.Close()

	result, err := db.Exec(`INSERT OR IGNORE INTO tasks (id, goal, description, status) VALUES (?, ?, ?, ?)`,
		"current",
		"测试ContextBuilder和Compressor集成",
		"强制激活测试：验证三个组件的数据流",
		"in_progress")
	if err != nil {
		fmt.Println("插入任务失败:", err)
		return
	}

	rowsAffected, _ := result.RowsAffected()
	fmt.Printf("✅ 插入任务: %d 行\n", rowsAffected)

	testConversations := []struct {
		turn    int
		role    string
		content string
	}{
		{1, "user", "请帮我修复登录页面的bug"},
		{2, "assistant", "好的，我来分析一下登录页面的问题..."},
		{3, "user", "错误信息是 'token expired'"},
		{4, "assistant", "明白了，这是Token过期的问题，需要检查刷新机制..."},
	}

	for _, conv := range testConversations {
		result, err = db.Exec(`INSERT INTO conversations (task_id, turn_number, role, content) VALUES (?, ?, ?, ?)`,
			"current", conv.turn, conv.role, conv.content)
		if err != nil {
			fmt.Printf("❌ 插入对话 %d 失败: %v\n", conv.turn, err)
			continue
		}
		rows, _ := result.RowsAffected()
		fmt.Printf("✅ 插入对话 turn=%d: %d 行\n", conv.turn, rows)
	}

	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM tasks WHERE id='current'").Scan(&count)
	if err == nil {
		fmt.Printf("\n📊 验证: tasks表中有 %d 条记录\n", count)
	}

	err = db.QueryRow("SELECT COUNT(*) FROM conversations WHERE task_id='current'").Scan(&count)
	if err == nil {
		fmt.Printf("📊 验证: conversations表中有 %d 条记录\n", count)
	}

	// 查看所有对话记录
	fmt.Println("\n📋 当前conversations表内容:")
	rows, err := db.Query(`SELECT id, turn_number, role, substr(content, 1, 50), is_compressed FROM conversations WHERE task_id='current' ORDER BY turn_number`)
	if err != nil {
		fmt.Println("查询失败:", err)
		return
	}
	defer rows.Close()

	hasData := false
	for rows.Next() {
		var id int
		var turn int
		var role string
		var content string
		var compressed bool
		err = rows.Scan(&id, &turn, &role, &content, &compressed)
		if err != nil {
			continue
		}
		hasData = true
		fmt.Printf("  ID=%d | Turn=%d | Role=%-10s | Content=%s... | Compressed=%v\n", id, turn, role, content, compressed)
	}

	if !hasData {
		fmt.Println("  ⚠️ 对话记录为空！Compressor无法压缩任何内容")
	}
}
