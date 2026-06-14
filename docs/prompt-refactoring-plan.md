# PM Prompt Refactoring Plan

## Language Strategy

### Decision: Unified English prompt

Argus is open source. Using English as the base prompt language ensures:

- **i18n-friendly**: Language-specific instructions are injected at runtime via `GetLanguageInstruction()`, not baked into the prompt
- **Community accessible**: English is the common language for open source projects
- **Consistent**: One prompt, one source of truth — no more "which prompt loaded?" ambiguity

### How language control works after refactoring

```
Core prompt (English, ~50 lines) ← the only prompt in code
    │
    └─ Appended at LLM call time:
       GetLanguageInstruction(replyLanguage, userMessage)
       → "You MUST reply in English." / "你必须用中文回复。"
```

This is already how `ChatStream`/`Chat` work (`client.go:216-218`). The refactoring adds the same to `ChatWithTools` (currently missing it). User's input language determines reply language — prompt stays English.

### What this means for users

- Chinese user types "写一个hello.go" → PM replies in Chinese (detected by `DetectLanguage`)
- English user types "write hello.go" → PM replies in English
- The prompt itself is always English — it's the model's behavioral instructions, not its output

---

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

## 2. Transplant Strategy: Not Delete, Reorganize

### Core principle

The current Chinese prompt (`pm_prompt.go:25-173`) has been benchmark-tested and produces coding output comparable to opencode. **We are not rewriting from scratch** — we are reorganizing the same tested content into a new architecture.

### Content mapping: old → new

| Old Section (Chinese, 173 lines) | Lines | Goes To | Rationale |
|------|-------|---------|-----------|
| Identity + 5 Principles | 25-65 | **Core Prompt** (Section 4) | Behavioral rules, keep in core |
| Featherweight tool table | 74-87 | **Execution Rules** (Section 5) | Reference material, not behavioral |
| Lightweight+ desc | 89-91 | **Core Prompt** → Decision Tree | One line in the decision tree |
| Decision Tree | 96-112 | **Core Prompt** → Decision Tree | Core behavioral logic, expand to cover all task types |
| Communication Rules | 116-122 | **Core Prompt** → Communication | Unchanged, translate to English |
| QA/Review Process | 126-143 | **Execution Rules** (Section 5) | Only relevant for @SE flow, reference |
| TODO Management | 147-150 | **Execution Rules** (Section 5) | Reference |
| Anti-loop | 154-158 | **Core Prompt** → Anti-loop | Behavioral, keep in core |
| Time/Social | 162-172 | **Remove** | Not useful for code generation |

### Why this works

- Every tested rule is preserved, just relocated
- Nothing is deleted — only structural reorganization
- The model still sees the same content (core + rules appended = comparable total length)
- The difference: behavioral rules are now front-loaded, reference material is after

### Risk: what might change

| Change | Mitigation |
|--------|-----------|
| Chinese → English phrasing | Use same semantic meaning, verified by output comparison |
| Content reordering | Behavioral rules first = model pays more attention to them = likely improvement |
| Rules appended vs inline | Same total content, same model consumption |

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

### Language: all English

Core prompt and execution rules are written in English. Language-specific instructions are injected at runtime by `GetLanguageInstruction()` in the LLM call layer. This is already the pattern for `ChatStream`/`Chat` — the refactoring adds it to `ChatWithTools`.

---

## 4. Core Prompt Design (~50 lines)

### Source material

Every line in the new core prompt is translated/adapted from the existing Chinese prompt (`pm_prompt.go:25-65` + `96-122` + `154-158`). Nothing is invented — we are transplanting tested content into a new structure.

### Structure (per pm-polish-guide.md §5)

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

### Source material

Transplanted from:
- Tool table (`pm_prompt.go:74-87`)
- Featherweight execution norms (`pm_prompt.go:106-112`)
- QA/Review process (`pm_prompt.go:126-143`)
- TODO management (`pm_prompt.go:147-150`)

Plus newly added: system/query/document task norms (previously missing coverage).

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

### 6.1 Unify `internal/core/prompts.go` and `internal/ai/pm_prompt.go`

Both PM prompts (English 27 lines from `prompts.go` + Chinese 173 lines from `pm_prompt.go`) become one unified English prompt in `pm_prompt.go`. The `PromptKit.Get(RolePM)` returns the same core prompt.

Impact:
- `argus.go:1813` — `c.prompts.Get(RolePM)` in pmDirectExecute now returns the unified core prompt (English, ~50 lines)
- `PMProcessor.getSystemPrompt()` in ProcessStream returns core + PMRules (English, ~100 lines total)
- Both paths get identical behavioral rules — no more "which prompt loaded?" ambiguity

### 6.2 Rewrite `internal/ai/pm_prompt.go` PMPrompt constant

Transplant existing 173 lines (Chinese) → ~50 lines (English):

| Content | Action |
|---------|--------|
| 5 Principles (lines 29-65) | Translate to English, keep in core |
| Decision Tree (lines 96-112) | Translate, expand to cover system/query/docs |
| Communication Rules (lines 116-122) | Translate, unchanged |
| Anti-loop (lines 154-158) | Translate, unchanged |
| Time/Social (lines 162-172) | Remove (not useful for code tasks) |

### 6.3 Add `internal/ai/pm_rules.go`

New file:

```go
package ai

// PMRules contains detailed execution reference appended to ProcessStream context.
// Transplanted from pm_prompt.go tool table (74-87), execution norms (106-112),
// and QA process (126-143).
const PMRules = `...`
```

`ProcessStream` appends it via `getSystemPrompt()`. `pmDirectExecute` does not use it.

### 6.4 Add language instruction to `ChatWithTools`

Currently `ChatWithTools(client.go:602)` does NOT inject language instructions. Add the same `GetLanguageInstruction()` call that `ChatStream` and `Chat` already use.

```go
// In ChatWithTools, before building the request:
langInstruction := GetLanguageInstruction(replyLanguage, userContent)
if langInstruction != "" {
    systemPrompt = systemPrompt + langInstruction
}
```

This ensures the English core prompt produces output in the user's language.

### 6.5 Fix `seExecutionSatisfied` in `argus.go:1381`

Remove the hard reject on `exit status` and `command failed`. These are normal outputs for many system commands. The LLM should decide if a result is acceptable, not a regex check.

```go
// Before:
if strings.Contains(r, "exit status") || strings.Contains(r, "command failed") {
    return false
}

// After:
// Remove these lines — let the LLM interpret exit codes
```

### 6.6 Fix `pmDirectExecute` retry prompt in `argus.go:2017-2029`

Current prompt is hardcoded for coding tasks ("syntax errors", "go run filename.go"). Replace with a generic version:

```
⚠️ Execution did not produce expected results. Review the output and retry.

Error: %s
Actions attempted: %v
Results: %v

Task: %s

Return corrected actions.
```

### 6.7 (Future) AGENTS.md support

When the project has a `.argus/AGENTS.md`, PM reads it on startup and includes it in context. This is for project-specific rules (build commands, code conventions) — not universal PM behavior.

---

## 7. Migration Plan

### Phase 1: Write unified prompt (transplant, not rewrite)

1. **Translate core content** (Chinese → English)
   - Identity + 5 Principles → Core Prompt identity/principles section
   - Decision Tree (lines 96-112) → Core Prompt decision tree (expanded with system/query/docs)
   - Communication Rules → unchanged, translate
   - Anti-loop → unchanged, translate

2. **Write PMRules** (transplant reference content)
   - Tool table (74-87) → translate
   - Execution norms (106-112) → translate, add system/query/docs norms
   - QA process (126-143) → translate
   - TODO (147-150) → translate

3. **Discard**
   - Time/Social (162-172) → remove entirely
   - `prompts.go` PMPrompt (27 lines, "delegate to SE") → replace with unified core

### Phase 2: Code changes

1. Unify `PromptKit.Get(RolePM)` → same core prompt from `pm_prompt.go`
2. Add `ChatWithTools` language injection (same as `ChatStream`/`Chat`)
3. Remove `exit status` check from `seExecutionSatisfied`
4. Fix `pmDirectExecute` retry prompt to be generic (not coding-only)

### Phase 3: Build + verify

1. `npm run build` + `wails build`
2. Test coding task (hello world → write + exec)
3. Test system task (clean directory → exec rm)
4. Test follow-up (再运行一次 → prevTaskLevel inheritance)
5. Test complex task (multi-file → @SE delegation)
6. Check conversation.log for correct PM behavior (language, tool usage)

### Phase 4: AGENTS.md (optional)
1. Add `.argus/AGENTS.md` loading
2. Document format for users

---

## 8. Open Questions

1. Should `PromptKit` be deleted entirely, or kept for SE/AP prompts? (SE/AP prompts are shorter and don't have this complexity)
2. Should `pmDirectExecute` be deleted entirely, or kept as the "no-frills" path for featherweight tasks? (If core prompt is unified, pmDirectExecute gets the same behavioral rules — but it doesn't need the detailed PMRules reference)
3. `ChatWithTools` language injection: should it match the existing pattern (append to systemPrompt) or be handled differently for tool-calling models?
4. The `ReplyLang` vs `c.language` — these are set in two different places (manager.go:413 defaults to "zh", argus.go:450 detects per-request). After unification with English prompt, `ReplyLanguage=auto` might be the right default.
