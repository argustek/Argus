# SE Hang Bug Fix — Status Summary
## 2026-05-29

---

## Root Causes Found

### 1. Nested Retry Explosion (Primary)
`ChatStream` inner retry (maxRetries=3) × `startSETaskWithFrom` outer retry (maxRetries=2)
= worst case 12 HTTP calls before giving up.
When one Read() hangs (SSE streaming doesn't respect context deadline),
the chain waits sequentially: ~4 min PM→SE, ~4 more min retry, etc.

**Fix:** Reduced `ChatStream` maxRetries 3→1. Reduced `http.Client.Timeout` 60s→30s.
Reduced stream idle timeout 90s→30s.

### 2. Turn Mechanism Blocking Handoff
`CheckTurnInternal` had hardcoded `"se"` as `from` — when PM delegates to SE,
PM just spoke, so `lastSpokenBy="pm"`, turn check says "pm can't speak again" → SE blocked.

SE→PM handoff had same issue: `ProcessMessageFrom` calls `TempReleaseProcessing`
which releases `isProcessing` but NOT `lastSpokenBy`. When SE finishes and routes
to PM, `lastSpokenBy` is still "pm" → PM blocked.

**Fix:** `CheckTurnInternal(from, ...)` uses actual caller.
Added `TempReleaseForHandover(fromRole)` — releases processing AND sets `lastSpokenBy=fromRole`.
Added `SetLastSpokenBy("pm")` before `handleAPReview` CheckTurn call.

### 3. SE JSON Parser — Missing `{` prefix
SE prompt changes caused model to occasionally output `"actions":[{...}]`
instead of `{"actions":[{...}]}`. `extractActionsFromJSON` only matched `{"actions"`.

**Fix:** Fallback: if `{"actions"` not found, search for `"actions":[`,
wrap with `{` prefix, re-parse. Returns empty actions → SE stuck.

### 4. isProcessing Timeout Kills SE State
When SE is stuck in API call >60s, user sends new message → `ProcessMessage`
timeout handler does `m.currentRole = ""` (clears SE role) + `UpdateSeStatus(Idle)`.
This allows a second SE goroutine to start while the first is still running.

**Fix:** `wasInSE` guard — if SE is current, queue the new message instead of
clearing SE state.

### 5. `currentRole` Not Cleaned on Error
SE `defer` only ran `MarkProcessingEnd("se")`. If `ProcessTaskStream` errored,
`currentRole` stayed "se" forever, blocking all future SE calls.

**Fix:** Added `m.currentRole = ""` to SE defer block.

### 6. Missing Frontend ACK Events
`se_task_assigned` and `error` events had no frontend ACK listener,
causing message_bus to falsely report them as lost.

**Fix:** Added `EventsOn` handlers with `ackMessage()` calls in `App.vue`.

---

## Files Changed

| File | Changes |
|------|---------|
| `internal/ai/se_prompt.go` | JSON parser fallback for malformed `"actions":[` |
| `internal/chat/router.go` | `TempReleaseForHandover`, `SetLastSpokenBy`, CheckTurn fix |
| `internal/chat/manager.go` | wasInSE guard, currentRole defer, context canceled retry |
| `internal/ai/client.go` | isRetryableError +context canceled, reduced timeouts |
| `frontend/src/App.vue` | ACK listeners for se_task_assigned / error events |

---

## Current Status

| Component | Status |
|-----------|--------|
| PM→SE delegation | ✅ Turn passes, API call starts immediately |
| SE API response | ✅ 3-5s when not hung (30s timeout when hung) |
| SE JSON parsing | ✅ Handles both `{"actions"` and malformed `"actions":[` |
| SE→PM handoff | ⚠️ Turn fix deployed, not yet fully verified |
| PM→AP handoff | ⚠️ SetLastSpokenBy fix deployed, not yet verified |
| SE state cleanup | ✅ currentRole cleared on error/exit |

---

## Known Remaining Issue

**SSE streaming `Read()` does not respect context deadline.**
This is a Go `net/http` design limitation: once HTTP response headers are received,
`resp.Body.Read()` blocks indefinitely regardless of context cancellation.
The `select { case <-ctx.Done() }` pattern only works BETWEEN reads, not DURING a read.

**Mitigation applied:** 30s stream idle timeout + reduced retry chain.
If Read() hangs beyond 30s without data, we return partial content and let
the outer retry loop handle it. Maximum total wait: ~90s (30s Read + 30s retry + 30s Read).

**Proper fix (future):** Use `net.Dialer` with `KeepAlive` or implement
a goroutine-based Read wrapper with channel + `resp.Body.Close()` to force-unblock.

---

## Test Results

- 18:08 round: PM→SE 5s return, actions=2 ✅
- 18:11 round: PM→SE hung (Read() block), recovered via user @SE → context cancel
- 18:41 round: PM→SE 3s return, actions=1 ✅ (after Read timeout fix)
- 19:04 round: PM→SE hung again — Read() blocking not fixed by http.Client.Timeout alone

**Overall:** JSON parser fix works reliably. Turn handover fix needs more testing.
SSE Read() blocking remains the outstanding hard problem.
