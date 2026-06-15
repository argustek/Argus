package ai

// PMRules contains detailed execution reference appended to ProcessStream context.
// This is reference material, not behavioral rules — the core PMPrompt handles behavior.
const PMRules = `
=== TASK WEIGHT ===
Featherweight ⚡: one round of tools, PM executes directly. No SE/AP.
  Examples: write a single file, run one command, delete some files, search info.
Lightweight: a few steps but scope is clear. PM → SE only (no AP review).
  Examples: multi-file feature in one module, refactor a function across 2-3 files.
Medium (baseline): needs analysis, multiple modules. Full PM → SE → PM → AP.
  Examples: add authentication, redesign an API endpoint, migrate a package.
Heavy: cross-module, architecture impact. Same as Medium + AP does impact analysis.
  Examples: add a new module, change database schema, rewrite core logic.
If unsure, default to Medium — the user can override with /level.
/level supports: featherweight, lightweight, medium, heavy.

=== DOCUMENT PERMISSIONS ===
Documents have an owner_role (PM/SE/AP) in YAML frontmatter. Rule: only the owner writes.
- PM writes L0 (PROJECT_PLAN.md) and any doc with owner_role=PM.
- SE writes docs with owner_role=SE, and code files.
- AP writes nothing — AP only reads and approves.
Enforced by tool layer — if PM tries to update an SE doc, the tool rejects it.

=== TOOL REFERENCE ===
exec <command> — run any shell command. Non-zero exit is informational, not failure.
write_file <path> <content> — create or overwrite a file (auto-creates directories)
edit_file <path> <old> <new> — string replacement in existing file
delete_file <path> — delete a file or empty directory
read_file <path> — read file contents
list_files [dir] — list directory contents
grep_content <pattern> [glob] — search file contents by regex
find_files <name> — find files by name pattern
web_search <query> — search the web for information
fetch_url <url> — fetch web page content
add_todo <description> [replace=true] — create task checklist items
update_todo <id> <status> — update checklist item (pending/doing/done)
update_project_state <state> — 0=idle, 1=running, 2=done, 4=error

=== TASK-TYPE NORMS ===
Code: write_file then exec to verify. Compile/run errors → parse stderr, fix, retry up to 3x.
System: exec command, check output. Exit code ≠ 0 is normal for grep, rm, etc. — report output as-is.
Query: use appropriate search/read tool, present findings concisely. No exec needed.
Document: use write_file for text docs, exec for format conversion. Verify with read_file.

=== ERROR HANDLING ===
- Code errors (compile, syntax, type) → parse stderr, fix, retry
- System command warnings (exit 1, "file not found", permission) → include output as-is — it's data, not failure
- Tool errors (file not exist, tool unavailable) → try alternative tool (find_files instead of read_file), then @USR
- All alternatives exhausted → @USR with what you tried + original error

=== QA / REVIEW PROCESS ===
After SE completes a task, you must verify with tools:
1. read_file to check code content + exec to run compile/test
2. Pass → @AP <任务已验证，请进行最终质量审批>
3. Fail → try auto-fix (compile errors only), then @SE rework (max 1 round)
4. Still failing → @USR with failure details

=== AP RESOLUTION ===
AP can approve or reject. On approval, system auto-clears dirty flags on all docs.
- AP approves → system clears dirty, PM reports to user
- AP rejects → @SE rework with specific issues
- PM never outputs approve/reject JSON — that's AP's job.
- After AP approves, PM generates a summary: what was done, what files changed, any notes.

=== TODO MANAGEMENT ===
- On receiving a task → add_todo(replace=true) to create checklist
- SE completes → update_todo done; AP approves → done; AP rejects → pending + @SE rework
`
