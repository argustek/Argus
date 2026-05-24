# G54/G55 PM Empty Message Fix - Status Report

## Date: 2025-05-25
## Branch: `fix/pm-empty-message-wip`
## Commit: `ddc0ede`

---

## Problem Statement

### Phenomenon
| Role | Frontend Display | Backend Log |
|------|-----------------|-------------|
| PM (Task Assignment) | ❌ Empty | ✅ Has content |
| PM (Review) | ❌ Empty | ✅ Has content |
| SE (Execution) | ⚠️ Card only, no text | ✅ Has content |
| AP (Approval) | ✅ Normal | ✅ Has content |

### User Observation
> "PM开始显示调用工具，后来显示消失了"
> (PM showed tool calls initially, then disappeared)

---

## Root Cause Analysis (G-Point)

### Timeline of the Bug
```
1. ai-stream-chunk creates PM message + accumulates content (tool call process)
   → User SEES content ✅

2. pm_message fires → lastPmMsg.content = data.delta  
   → OVERWRITES with short conclusion

3. If short conclusion gets "stolen" by RichMessage matching
   → Message becomes EMPTY ❌
```

### Code Path
**Backend sends PM message twice:**
1. **ai-stream-chunk** - During streaming (rich content with tool calls)
   - File: [manager.go](internal/chat/manager.go) - `emitStreamChunk()`
   
2. **pm_message** - After completion (short final conclusion)
   - File: [manager.go:3179](internal/chat/manager.go#L3179)
   ```go
   runtime.EventsEmit(m.ctx, "pm_message", map[string]string{"delta": content})
   ```

**Frontend was overwriting:**
- File: [App.vue:422](frontend/src/App.vue#L422) (BEFORE fix)
  ```javascript
  lastPmMsg.content = data.delta  // ❌ Direct overwrite!
  ```

---

## Fix Applied

### Change 1: App.vue - pm_message handler
```javascript
// BEFORE: Always overwrite
lastPmMsg.content = data.delta

// AFTER: Only fill if empty, preserve accumulated content
if (!lastPmMsg.content || lastPmMsg.content.length < 10) {
    lastPmMsg.content = data.delta
} else {
    // Keep ai-stream-chunk accumulated content
}
```

### Change 2: ChatPanel.vue - Debug logs
Added G54 debug logging for PM message matching trace.

---

## Files Modified
| File | Changes |
|------|---------|
| `frontend/src/App.vue` | pm_message logic refactored |
| `frontend/src/components/ChatPanel.vue` | Added G54 debug logs |

---

## Verification Checklist
- [ ] PM task assignment shows "@SE 请创建..."
- [ ] PM review shows review process or "@AP 任务已验证"
- [ ] SE operation card displays correctly
- [ ] DingTalk messages unaffected
- [ ] No regression in AP messages

---

## Related Issues (Historical)

| ID | Issue | Status |
|----|-------|--------|
| G49 | MessageID tracking system | ✅ Completed |
| G50 | messageId reuse bug | ✅ Fixed |
| G51 | SE JSON output cleanup | ✅ Fixed |
| G52 | Stream audit log system | ✅ Completed |
| G53 | exec_completed duplicate | ✅ Fixed |
| **G54** | **PM empty message root cause** | 🔄 WIP |
| G55 | PM review missing | 🔜 Pending |

---

## Design Flaw Identified

The current architecture has **inconsistent message handling per role**:

| Role | Send Method | Problem |
|------|------------|---------|
| PM | ai-stream-chunk + pm_message | pm_message overwrites |
| SE | exec_start (empty) + _execData | Content always empty |
| AP | ap_message + ai-stream-chunk | Works correctly |

**Ideal solution:** Unify all roles to use single event channel (like AP).

---

## Next Steps
1. Test current fix on clean environment
2. If PM still empty, investigate RichMessage "stealing" content
3. Consider unifying message handling architecture
4. Remove debug logs after verification
