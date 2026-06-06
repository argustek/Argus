# Argus v0.6.5 Release Notes

> Release date: 2026-06-03  
> Tag: `v0.6.5`  
> Since: `v0.6.0`

---

## Table of Contents

- [Architecture](#architecture)
- [SE Engine](#se-engine)
- [PM / AP Review Pipeline](#pm--ap-review-pipeline)
- [Crash & Stability Fixes](#crash--stability-fixes)
- [Frontend & Message Bus](#frontend--message-bus)
- [Executor & Command Handling](#executor--command-handling)
- [Regression Test Results](#regression-test-results)

---

## Architecture

### Function Calling Architecture (`68e2c31`)
- **SE migrates from ChatStream to ChatWithTools** — full function calling architecture
- SE now uses structured tool calls (`write_file`, `exec`, `edit_file`, `search_files`) instead of free-form chat output
- PM and AP prompts aligned with the new structured action format
- SE prompt rewritten (`internal/ai/se_prompt.go`) — 1014 lines removed, 257 lines added (net -75%)
- New `parseSEResponse()` parser for structured JSON action responses
- New `ensureExecAction()` guard — auto-appends missing `exec` commands for code files (.go, .py, .js, .ts)

### Dynamic Todo List Sync (`a061d64`)
- Real-time todo list synchronization between backend and frontend via MessageBus
- Frontend reflects current phase (PM → SE → Review → Approve) in real-time
- Task completion tracking per phase

---

## SE Engine

### Tool Result Feedback Loop (`5a54be4`)
- When execution fails, SE receives the actual error output (syntax errors, exit codes, command output) and self-fixes
- Up to 5 self-fix attempts with progressive error detail:
  - Attempt 0: General guidance with common fixes
  - Attempt 1: Specific guidance for common mistakes (cd:, go test on non-test files, absolute paths)
  - Attempt 2+: Exact template with complete working code example
- SE only outputs error to user when truly unable to fix after all attempts

### AP → SE Fallback (`5a54be4`)
- When AP rejects, SE gets up to 2 repair chances
- Full PM Review + AP Approval re-run after each repair attempt
- Progressive repair prompts that include the exact rejection reason

### SE Retry After PM Rejection (`8a32e4a`, `fff0f78`)
- SE retry loop when PM rejects work (max 3 attempts originally, now 2)
- Each retry includes the specific PM rejection reason in the prompt
- Empty-action detection: if SE returns no actions on retry, marked as failed

### SE Auto-Fix Enhancement (`6854266`, `83e18df`)
- Self-check mechanism: SE attempts to verify its own work before submitting
- Auto-detection of common issues: missing imports, syntax errors, incomplete code

### Strict Exec Enforcement (`82807be`)
- SE must include `exec` action after any code file write
- PM review tightened to require execution verification output
- Missing exec = automatic rejection at PM level

### Path Validation (`487165a`)
- `ensureExecAction()` now validates paths before execution:
  - Rejects empty paths, `"content"`, absolute paths, paths without extensions
  - Blocks garbage data from reaching the executor
- Prevents LLM hallucinations like `path: "content"` or `F:\GithubArgus\...` from executing

---

## PM / AP Review Pipeline

### PM Reject Guard (`487165a`)
- **Critical safety net**: When PM rejects after all retry attempts, process stops immediately
- Previously, rejected work could leak into AP approval (AP prompt was hardcoded "PM approved")
- AP prompt now receives actual PM status ("approved" or "REJECTED - reason")
- AP instructed to also reject if PM rejected unless SE clearly fixed issues

### Strict Execution Validation (`487165a`)
- `seExecutionSatisfied()` rewritten:
  - Old: any non-empty output = satisfied (write_file "content" counted as success)
  - New: requires at least one successful `exec` result AND no exec failures/syntax errors
- Prevents false-positive "success" when SE only writes files without running them

### PM/AP Review Process (`6486947`, `7f94656`)
- Primary path: AI structured JSON decision for approve/reject
- Fallback: keyword matching (English) if JSON parsing fails
- AP API failure: returns error instead of auto-approving
- Terminal prompt displays working directory correctly
- Residual log files in working directory cleaned up automatically

### Reduced Retries (`5a54be4`)
- PM retry: reduced to 2 (was higher)
- AP retry: reduced to 1 (was higher)
- Rationale: with Tool Result Feedback, fewer retries needed

### SE Slacking Detection (`5a54be4`)
- Empty actions after PM feedback now triggers immediate rejection
- Previously, empty actions would silently pass through to AP

---

## Crash & Stability Fixes

### Critical: sanitizeCommandPath Crash Fix (`5a54be4`)
**Root cause analysis:**
- Every `exec` command triggered a panic in `sanitizeCommandPath()`
- Three contributing factors:
  1. **Per-call regex compilation**: 4 `regexp.MustCompile()` calls on every exec (should be pre-compiled)
  2. **`$` backref conflict**: `ReplaceAllString("$1 "+path)` where path characters collided with regex backreferences
  3. **Unsupported `\1` backreference**: Go RE2 does not support capture group backreferences

**Fix:**
- All regexes pre-compiled as package-level `var` constants
- Path replacement changed from `ReplaceAllString` to safe `strings.Fields` + manual join
- Removed `\1` syntax, simplified capture patterns

**Result:** Regression test 10/10 pass, 0 crashes (was crashing on every exec within ~15 seconds)

### Process Stability (`1d6ae99`, `60e6236`)
- `Bridge.isProcessing` lock reset to prevent task processing deadlock
- `isSending` lock reset to prevent persistent "Send failed" state after Reset

### Startup Message Loss Fix (`2070350`, `50f7b81`)
- DOM-ready synchronization resolves startup message loss
- Terminal `output` events cached until frontend DOM is ready
- Messages no longer lost during initial page load

### Duplicate Event Fix (`d3dbe62`)
- Removed duplicate MessageBus calls for system events (`messages-cleared`, `reset-completed`)

---

## Frontend & Message Bus

### Complete Message Tracing (`6158581`)
- Full message tracing for all backend→frontend communications
- V2 bridge debug logging for all MessageBus events

### MessageBus ACK Coverage (`158689d`, `e63618a`)
- ACK mechanism for `reset-completed` and `task-clarify` events
- Complete ACK coverage across all event types
- Terminal output metadata filter to reduce noise

### Path Status Tracking (`9f7fdfd`)
- Deadlock resolved in PathStatus tracking
- V2 bridge debug log added
- Frontend data format fixed

### GBK Encoding Fix (`83e18df`)
- Chinese character encoding issue resolved for Windows console output
- Proper UTF-8/GBK conversion for terminal display

### Emit/Frontend Bridge (`cff6175`)
- `emitToFrontend` unified message handling
- Bridge message handling cleaned up

### SE Prompt Optimization (`cc56364`)
- Slimmed SE prompt by removing 54 lines of noise
- Core rules preserved, redundancy eliminated

---

## Executor & Command Handling

### Command Sanitization (`5a54be4`, `6854266`)
- `sanitizeCommandPath()` handles common LLM-generated command errors:
  - `cd:` → `cd ` (missing space)
  - `go test file.go` on non-_test.go → `go run file.go`
  - Absolute paths in run commands → relative paths
  - Hallucinated paths (`F:/GithubArgus`) → working directory
  - Non-existent cd targets → fall back to working directory

### Command Safety
- All commands executed through `cmd /c` with timeout protection
- Server commands (http.ListenAndServe, :8080, etc.) detected and handled separately
- Working directory enforced for all executions

---

## Docs & Chores

- [`9317f12`] Updated README for v0.6.0 V2 architecture release
- [`05c6c47`] Fixed workflow diagram to show correct 5-phase USR→PM→SE→PM→AP pipeline
- [`3ce65b5`] Added Git operation rules documentation (fetch before pull, multi-device collaboration)
- [`4eabb5a`] Added SE bugfix and MessageBus ACK fix summaries
- [`134bfa9`] Untracked `.trae` directory (IDE config, not for public repo)

---

## Regression Test Results

| Run | PASS | FAIL | CRASH | Notes |
|-----|------|------|-------|-------|
| Pre-fix | 0/10 | 0 | 10/10 | Crashed on every exec (~15s each) |
| Post-sanitizeCommandPath fix | 10/10 | 0 | 0 | All pass, stable |
| Post-validation guards | 8/10 | 2 | 0 | 2 fails = LLM quality issues (correctly caught by PM/AP) |
| Final (all fixes) | 7/10 | 3 | 0 | 3 fails = correct rejections (PM guard working) |

**Note:** Failures in final run are expected behavior — LLM occasionally generates malformed code (syntax errors, wrong filenames), which is correctly caught by PM/AP review. No crashes, no flow violations.
