# PM Prompt Refactoring Plan

## 1. Current State Analysis

### Two conflicting PM prompts exist

| File | Lang | Lines | Used By |
|------|------|-------|---------|
| `internal/core/prompts.go:6` | English | 27 | `pmDirectExecute` via `PromptKit.Get(RolePM)` |
| `internal/ai/pm_prompt.go:25` | Chinese | 173 | `ProcessStream` via `PMProcessor.getSystemPrompt()` |

### Contradictions (per pm-polish-guide.md §1)

- `prompts.go` says: "NEVER write code yourself - always delegate to SE"
- `pm_prompt.go` says: "Featherweight tasks → execute directly"
- Result: model behavior is non-deterministic, depends on which prompt loads

### Redundancy (per pm-polish-guide.md §4)

- Tool table appears only in pm_prompt.go (lines 74-87), missing from prompts.go
- Error handling rules duplicated across principles (§4), execution norms (§5), and QA section (§6)
- Tool list in SEPrompt (prompts.go:42-48) duplicates PM's tool table

### Missing coverage

- No task type covers "system operations" (cleanup, disk check, process management)
- Error handling assumes coding tasks only (undefined symbol, missing import, type mismatch)
- Non-zero exit code treated as failure with no nuance

### Length issues (per pm-polish-guide.md §8)

- 173 lines → model only retains first ~60 lines reliably
- Detailed rules at the bottom are effectively dead code
- Rules in the middle (lines 60-110) get partial attention

---

## 2. Industry Research (June 2026)

### All major competitors use: short core prompt + external rules

| Tool | Core Prompt | External Rules |
|------|-------------|----------------|
| **OpenCode** | ~50 lines, provider-specific (anthropic.txt / beast.txt etc.) | `AGENTS.md` (project) + `~/.config/opencode/AGENTS.md` (global) + `.opencode/agents/*.md` |
| **Cursor** | IDE built-in | `.cursor/rules/*.mdc` (scoped by glob pattern) |
| **Claude Code** | ~2,896 tokens baked-in | `CLAUDE.md` (project) + plugin skills |
| **GitHub Copilot** | Built-in | `.github/copilot-instructions.md` |

### Key patterns across all competitors

1. **Core prompt is lean** (30-60 lines) — identity, first principles, decision tree, communication rules
2. **Detailed rules are external** — in markdown files, version-controlled, per-project
3. **Rules are scoped** — glob patterns control when each rule file is injected
4. **Tool permission > prompt text** — removing a tool is more effective than telling the LLM not to use it
5. **No one crams everything into system prompt** — it degrades model attention on what matters

### OpenCode's architecture (most relevant)

```
LLM request assembly:
  1. Provider base prompt (anthropic.txt, ~50 lines)
  2. + User's AGENTS.md (project rules, loaded from file)
  3. + Global AGENTS.md (user's global preferences)
  4. + Agent-specific prompt (from .opencode/agents/*.md)
  5. + Runtime context (cwd, git status, date)
  ─────────────────────────────────────
  Final system prompt (~200 lines total, but most is reference not behavioral)
```

Note: the 200+ lines include tool schemas, task descriptions, and examples that act as **reference** — the behavioral core is only ~50 lines.

---

## 3. Proposed Architecture

### Split into three layers

```
Layer 1: Core Prompt (~50 lines, code-embedded)
  Purpose: Identity + behavior + decision tree
  Location: internal/ai/pm_prompt.go (PMPrompt constant)
  Audience: All PM LLM calls (ProcessStream + pmDirectExecute)

Layer 2: Execution Rules (~50 lines, loaded at runtime)
  Purpose: Tool reference + task-type norms + error handling
  Location: internal/ai/pm_rules.go (PMRules constant, appended by code)
  Audience: ProcessStream only (pmDirectExecute doesn't need detailed rules)

Layer 3: Project Rules (AGENTS.md, optional, loaded from disk)
  Purpose: Project-specific conventions
  Location: project root / .argus/AGENTS.md
  Audience: Both paths, injected via read_file or memory context
```

### Why three layers (not two)

- **Core prompt** must be in code — it defines the agent's fundamental behavior and can't be overridden by users
- **Execution rules** are semi-stable — they change when we add tools or task types, not per project
- **Project rules** are user-managed — build commands, coding conventions, non-obvious patterns

---

## 4. Core Prompt Design (~50 lines)

### Structure (per pm-polish-guide.md §5)

```
// PMPrompt defines the PM agent's core identity and behavior.
// Keep this short — the model reads it every turn.
const PMPrompt = `You are Argus PM — an autonomous project manager that uses tools to get things done.

[Identity — 1 line]
[First Principle — 3 lines]
[Decision Tree — the only behavioral rules, ~20 lines]
[Communication Rules — 5 lines]
[Anti-loop Protection — 3 lines]
`
```

### Decision Tree — must cover ALL input types with no overlap

```
User message
  ├─ greeting/chat/thanks → @USR <reply>
  │
  ├─ unclear/ambiguous → use tools (list_files/grep/search) to gather context,
  │     then @USR <question with options> if still unclear
  │
  ├─ simple task (single step, clear outcome) → execute directly with tools
  │   Examples:
  │   - write a file → write_file + (exec to verify if code)
  │   - clean up files → exec / delete_file
  │   - check system state → exec (disk/process/network)
  │   - search information → grep / web_search / read_file
  │   - convert a document → appropriate tool
  │   Simple means: you can finish it in one round of tool calls
  │
  └─ complex task (multi-step, needs analysis) → @SE <task breakdown>
      Then after SE completes, verify and @AP for approval
```

### Communication Rules

```
@SE <task> — assign work to Software Engineer
@AP <result> — submit for approval after verification
@USR <message> — talk to the user (questions, status, results)

One @ per message maximum.
```

### Anti-loop Protection

```
- If SE completes a task, do not re-assign the same task to SE
- If a tool errors twice on the same input, try a different approach, not a retry
- If you can't make progress after 3 attempts, @USR <what happened + what you tried>
```

---

## 5. Execution Rules Design (~50 lines)

### Structure

```go
// PMRules contains detailed execution reference appended to ProcessStream context.
// This is reference material, not behavioral rules.
const PMRules = `
=== TOOL REFERENCE ===
exec <command> — run any shell command. Non-zero exit is informational, not failure.
write_file <path> <content> — create or overwrite a file (auto-creates directories)
edit_file <path> <old> <new> — string replacement in existing file
delete_file <path> — delete a file or empty directory
read_file <path> — read file contents
list_files [dir] — list directory contents
grep_content <pattern> [glob] — search file contents
find_files <name> — find files by name
web_search <query> — search the web
fetch_url <url> — fetch web page content
add_todo / update_todo — task tracking for complex work

=== TASK-TYPE NORMS ===
Code: write_file then exec to verify. Compile/run errors → fix and retry up to 3x.
System: exec command, check output. Exit code ≠ 0 is normal for rm, grep, etc.
Query: use appropriate search/read tool, present findings concisely.
Document: use the right tool for the format.

=== ERROR HANDLING ===
- Code errors (compile, syntax, type) → parse stderr, fix, retry
- System command warnings (exit 1, "file not found", permission) → include output as-is, it's data not failure
- Tool errors (file not exist, tool unavailable) → try alternative tool, then @USR
`
```

### Where it's injected

```go
// In ProcessStream, append PMRules after the core prompt
func (p *PMProcessor) getSystemPrompt() string {
    base := p.systemPrompt  // the ~50 line core
    if p.useDetailedRules {
        return base + "\n\n" + PMRules
    }
    return base
}
```

Note: `pmDirectExecute` does NOT need `PMRules` — its use case is simple enough with just the core prompt.

---

## 6. Code Changes Required

### 6.1 Delete `internal/core/prompts.go` PMPrompt (27 lines)

Replace with a reference to the unified PM prompt. The `PromptKit.Get(RolePM)` should return the same core prompt from `pm_prompt.go`.

Impact:
- `argus.go:1813` — `c.prompts.Get(RolePM)` in pmDirectExecute now returns the unified core prompt
- This changes pmDirectExecute behavior from "delegate to SE" to "execute directly" — which is what featherweight should do

### 6.2 Rewrite `internal/ai/pm_prompt.go` PMPrompt constant

From 173 lines to ~50 lines. Remove:
- Redundant examples (keep 1-2 max)
- Tool table (move to PMRules)
- Execution norms (move to PMRules)
- Error handling details (move to PMRules)
- QA verification process for SE flow (keep only as reference)
- Time perception / social guide (remove entirely — doesn't affect code generation)

### 6.3 Add `internal/ai/pm_rules.go`

New file containing `PMRules` constant (~50 lines). `ProcessStream` appends it, `pmDirectExecute` does not.

### 6.4 Fix `seExecutionSatisfied` in `argus.go:1381`

Remove the hard reject on `exit status` and `command failed`. These are normal outputs for many system commands. The LLM should decide if a result is acceptable, not a regex check.

```go
// Before:
if strings.Contains(r, "exit status") || strings.Contains(r, "command failed") {
    return false
}

// After:
// Remove these lines — let the LLM interpret exit codes
```

### 6.5 Fix `pmDirectExecute` retry prompt in `argus.go:2017-2029`

Current prompt is hardcoded for coding tasks ("syntax errors", "go run filename.go"). Replace with a generic version:

```
⚠️ Execution did not produce expected results. Review the output and retry.

Error: %s
Actions attempted: %v
Results: %v

Task: %s

Return corrected actions.
```

### 6.6 (Future) AGENTS.md support

When the project has a `.argus/AGENTS.md`, PM reads it on startup and includes it in context. This is for project-specific rules (build commands, code conventions) — not universal PM behavior.

---

## 7. Migration Plan

### Phase 1: Consolidate prompts
1. Write the unified core prompt (~50 lines)
2. Write PMRules (~50 lines)
3. Delete `prompts.go` PMPrompt, point to new core
4. Make `ProcessStream` append PMRules
5. Make sure `pmDirectExecute` does NOT get PMRules

### Phase 2: Fix execution logic
1. Remove `exit status` check from `seExecutionSatisfied`
2. Fix `pmDirectExecute` retry prompt to be generic

### Phase 3: Verify
1. Test coding task (hello world → write + exec)
2. Test system task (clean directory → exec rm)
3. Test follow-up (再运行一次 → prevTaskLevel inheritance)
4. Test complex task (multi-file → @SE delegation)
5. Check conversation.log for correct PM behavior

### Phase 4: AGENTS.md (optional)
1. Add `.argus/AGENTS.md` loading
2. Document format for users

---

## 8. Open Questions

1. Should `PromptKit` be deleted entirely, or kept for SE/AP prompts? (SE/AP prompts are shorter and don't have this complexity)
2. Should `pmDirectExecute` be deleted entirely, or kept as the "no-frills" path for featherweight tasks? (If core prompt is unified, pmDirectExecute gets the same behavioral rules — but it doesn't need the detailed PMRules reference)
3. How to handle the `seExecutionSatisfied` removal without breaking the normal SE execution path? (SE execution has its own satisfaction check in manager.go)
