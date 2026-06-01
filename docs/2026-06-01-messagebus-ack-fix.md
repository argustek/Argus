# MessageBus ACK & Terminal Output Fix — 2026-06-01

## Overview

Complete fix for MessageBus ACK tracking gaps and terminal output display issues.
Root cause: `window.__argusAck` was never defined, causing all terminal ACKs to be no-ops.

---

## Problems Fixed (5 issues)

### 1. 🔴 CRITICAL: `window.__argusAck` undefined — terminal:output never ACKed
- **Root cause**: `TerminalWindow.vue` and `BottomPanel.vue` called `window.__argusAck?.(msgId)` but the function was **never assigned**
- **Impact**: ALL `terminal:output` messages triggered "🚨 消息丢失" false alarms every 6 seconds
- **Fix**: [App.vue:273](frontend/src/App.vue#L273) — `;(window as any).__argusAck = ackMessage`

### 2. 🟡 Terminal showing metadata garbage (`_msgId`, `_tracked`, checksum, etc.)
- **Root cause**: [app.go:796-808](app.go#L796-L808) `emitToFrontend` merges payload + MessageBus metadata into one object
- **Fix**: [TerminalWindow.vue:152-165](frontend/src/components/TerminalWindow.vue#L152-L165) + [BottomPanel.vue:165-178](frontend/src/components/BottomPanel.vue#L165-L178)
  - Filter out `_` prefixed fields, `event`, `checksum`, `source`, `path`
  - Only show pure command output to user

### 3. 🟢 V2 Error message too vague ("exit status 1" with no command detail)
- **Before**: `V2 Error: action 1 (exec) failed: command failed: exit status 1`
- **After**: Shows full command, error, and execution output from `result.Outputs` and `result.Phases[].Output`
- **Fix**: [app.go:3132-3139](app.go#L3132-L3139)

### 4. 🟢 11 events missing frontend ACK handlers
Events with listeners but NO `ackMessage()` call:
| Event | File |
|-------|------|
| `se-file-written` | App.vue:763 |
| `pm_review_completed` | App.vue:605 |
| `project_approved` | App.vue:750 |
| `task-recovered` | App.vue:776 |
| `tasklist_start` | App.vue:800 |
| `tasklist_update` | App.vue:814 |
| `tasklist_replace` | App.vue:829 |
| `tasklist_complete` | App.vue:837 |
| `shell_start` | App.vue:849 |
| `shell_output` | App.vue:861 |
| `shell_done` | App.vue:870 |

All now have `ackMessage(data._msgId || '')` as first line in handler.

### 5. 🟢 Stream chunk sampling (from previous session)
- [message_bus.go:220-226](internal/chat/message_bus.go#L220-L226): Every Nth chunk tracked (default N=10)
- Reduces ACK overhead for high-frequency stream events

---

## Files Modified (8 files)

| File | Changes |
|------|---------|
| `app.go` | V2 Error detail output (+8 lines), removed V1 Bridge ai-chunk code |
| `frontend/src/App.vue` | `__argusAck` global mount, 11 event ACK additions, removed dead ai-chunk listener |
| `frontend/src/components/TerminalWindow.vue` | Metadata filter in handleOutput, ACK fix |
| `frontend/src/components/BottomPanel.vue` | Metadata filter in handleTerminalOutput, ACK fix |
| `internal/chat/message_bus.go` | Stream sampling logic |
| `main.go` | Log path consolidation |
| `frontend/wailsjs/go/main/App.d.ts` | Type update for AckMessage |
| `frontend/wailsjs/go/main/App.js` | Bindings update |

---

## Architecture Note: V1 → V2 Cleanup

**V1 Bridge layer `ai-chunk` event has been fully removed.**

The dual-channel problem:
- **V1 path**: `bridge.SetOnChunk` → emit `ai-chunk` event (DELETED)
- **V2 path**: `manager.Process` → emit `ai-stream-chunk` via MessageBus (ACTIVE)

Frontend was confused by receiving both. Now unified on V2 Manager path only.

---

## Testing Status

- ✅ Compilation successful (wails build)
- ✅ `__argusAck` global mount verified
- ✅ Terminal metadata filter implemented
- ✅ 11 events ACK coverage complete
- ⚠️ Terminal output still not visible in UI (needs further investigation — may be xterm rendering issue or data not reaching terminal component)

## Next Steps

1. Investigate why terminal panel shows no output despite `terminalOutput` callback being called
2. Verify hello world full flow (USR→PM→SE→exec→AP approve) end-to-end
3. Consider adding terminal output to conversation log for debugging
