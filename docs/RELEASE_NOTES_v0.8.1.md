# v0.8.1 — MessageBus Unification & Project Level Indicator

Release date: 2026-06-10

## Summary

v0.8.1 completes the MessageBus unification effort (started in v0.7.2) and introduces the Project Level Indicator. All frontend-backend communication now flows exclusively through MessageBus, eliminating the fragmented SSE/stderr event paths that caused silent message drops and status inconsistencies.

## Key Features

### Project Level Indicator (`[short-process]` / `[normal-process]` / `[full-process]`)
- New badge displayed to the left of the status indicator light in TopBar
- Shows the weight of the current task at a glance
- **short-process**: Featherweight/single-file tasks (PM direct execution, no SE/AP)
- **normal-process**: Standard PM→SE workflow
- **full-process**: Reserved for future multi-stage complex workflows
- Badge is color-coded: purple (short), blue (normal), orange (full)
- Data pushed from backend through MessageBus, no polling

## Bug Fixes

### Frontend Status Indicator Reliability
- Fixed project status light stuck on `idle` during standard PM→SE workflow (handleToPM)
- Added `onProjectStateChange` callback to V2 Bridge — PM direct execution now correctly propagates `running`/`done`/`error` states to CMonitor
- CMonitor state mapping now includes `case 3 (approved)` — was missing, causing approved state to be silently mapped to `idle`
- Added auto-fallback in the chat branch: when PM returns without `@AP`, project state auto-transitions from `running` to `done` (prevents CMonitor false alarms)

### CMonitor / AP Interaction
- Short tasks (Featherweight) now set `ProjectStateApproved(3)` instead of `ProjectStateDone(2)`
- This bypasses CMonitor's AP-forcing logic (`handleProjectDone`), eliminating the need for manual AP approval on tasks that skip the AP review phase

### Dead Code Cleanup
- Removed legacy `pm_started` push (replaced by `role-state`)
- Removed `se_task_assigned` push (frontend only acked, never used the data)
- Corresponding frontend `EventsOn` listeners cleaned up

### UI Polish
- Removed hardcoded `⚡` (lightning bolt) prefix from PM direct-execute output messages
- `⚡` was an internal short-process marker leaked to users; now only displayed as `[short-process]` badge when relevant

## Technical Details

### Architecture
- **MessageBus** is now the sole communication channel between Go backend and Vue frontend
- All `runtime.EventsEmit` calls replaced with `m.msgBusSend()` → unified tracking, ACK, and loss detection
- Bridge path (`Bridge.Process()`) and Standard path (`Manager.handleToPM()`) both emit `project-level` events to the same bus channel

### Data Flow (Project Level)
```
ArgusCore.Process() ──→ result.Level ("short-process"/"normal-process")
    │
    ▼
Bridge / handleToPM ──→ MessageBus "project_level"
    │
    ▼
app.go emitToFrontend("project-level")
    │
    ▼
App.vue EventsOn('project-level') ──→ TopBar :projectLevel prop
    │
    ▼
TopBar.vue: [short-process] ● idle
```

### Files Changed
| File | Changes |
|------|---------|
| `internal/core/argus.go` | Added `Level` field to ProcessResult; set by Featherweight detection |
| `internal/chat/bridge.go` | Push `project_level` via onMessage; add onProjectStateChange callback |
| `internal/chat/manager.go` | Add `detectProjectLevel()` function; push level in handleToPM; chat branch auto-fallback; cleanup dead SSE events |
| `app.go` | Intercept `project_level` → emitToFrontend; Bridge callback → CMonitor |
| `frontend/src/App.vue` | Add projectLevel ref + EventsOn listener; remove dead pm_started/se_task_assigned listeners |
| `frontend/src/components/TopBar.vue` | Add badge display + CSS for 3 levels |

## Upgrade Notes

- **No config changes required** — all new features are additive
- **Frontend rebuilt required** — `npm run build` then `wails build` (Vue template changes)
- The `⚡` marker is removed from PM direct-execute responses; tasks are now classified purely through the `[short-process]` badge in TopBar
- If you have scripts parsing PM output for `⚡`, update them to use the new `project-level` event via MessageBus