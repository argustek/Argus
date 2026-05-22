# Work Progress Summary - 2026-05-22

## Issue: PM Message Display Bug (Empty/Flashing)

### Problem Description
When user sends a chat message (e.g., "你好"), PM response:
1. Briefly displays content, then **auto-clears** to empty
2. Shows a popup dialog "PM needs your confirmation" that blocks interaction
3. Task mode works correctly, only chat mode has this issue

### Root Cause Analysis (3-layer investigation)

#### Layer 1: Event Channel Conflict (Initial Hypothesis)
- `ai-stream-chunk("pm")` creates streaming message A
- `pm_message({delta})` creates complete message B
- Two events conflict → content overwritten ❌ (Partial cause)

#### Layer 2: RichMessage Rendering Switch (TRUE ROOT CAUSE)
- [manager.go:1249](internal/chat/manager.go#L1249): Every `handleToPM` call unconditionally starts TaskList via `richBuilder.StartTaskList()`
- [manager.go:1576](internal/chat/manager.go#L1576): Chat mode calls `CompleteTaskList()` at end → emits `tasklist_complete` event
- Frontend receives event → switches from plain text to RichMessage component
- RichMessage component renders with empty result/shells → **shows blank!**

**Why task mode works**: Task mode calls `addPMToSEMsg()` then immediately `startSETask()`, so the RichMessage switch happens later or not at all.

### Fixes Applied

| File | Change | Purpose |
|------|--------|---------|
| [App.vue](frontend/src/App.vue#L383) | Skip PM role in `ai-stream-chunk` handler | Prevent double message creation |
| [App.vue](frontend/src/App.vue#L334) | Skip PM role in `new-message` handler | Prevent overwrite by backend callback |
| [ChatPanel.vue](frontend/src/components/ChatPanel.vue#L249-L254) | Add PM waiting status bar | Replace blocking dialog with inline notification |
| [manager.go](internal/chat/manager.go#L1264-L1277) | Detect chat mode, skip RichMessage for short messages | **Core fix**: Don't trigger RichMessage for casual chat |

### Files Modified
1. `frontend/src/App.vue` - Event handling fixes
2. `frontend/src/components/ChatPanel.vue` - PM status bar UI + styles
3. `internal/chat/manager.go` - Chat mode detection logic
4. `internal/task/manager.go` - Time.Time serialization fix (RFC3339)
5. `frontend/src/components/GlobalTaskBar.vue` - Always show, empty state styling

### Technical Details
- PM has dedicated `pm_message` event channel (unlike SE/AP)
- Backend filters `pm_to_user` source messages from `new-message` event (app.go:585)
- But `ai-stream-chunk` was NOT filtered → caused frontend confusion
- RichMessage component requires `result.text` or `shells` to display content
- When switched to RichMessage mode without proper data → blank display

### Next Steps
- Verify chat mode shows PM response correctly after rebuild
- Test task mode still works (no regression)
- Consider adding debug logging for event flow tracing
