# Argus v0.7.3 Release Notes

**Release Date:** 2026-06-10

---

## Summary

v0.7.3 is a **critical bug-fix release** that fixes two major issues: **API configuration hot-reload not working in shared mode**, and **MessageBus event loss (LOST alerts)** causing token stats and context events to disappear from the frontend panel.

---

## Bug Fixes

### API Configuration Hot-Reload (Critical)

**Problem:** In shared mode ("Use different models for each role" = unchecked), changing the model via "All roles share" dropdown and clicking Save did **not** take effect. The PM continued using the previous model until the application was restarted.

**Root Cause:** `UpdateAPIConfig()` called `rebuildRoleClients()` which used the **stale** `PMConfig` from before the save to create an independent `pmClient`. Since `getPMClient()` returns `pmClient` when non-nil, the new correct `aiClient` was bypassed entirely — the old independent client kept serving requests with the wrong model.

**Fix:** Three-layer fix:
1. Added `UseSeparateModels` flag to `types.Config` so Manager knows whether it's in shared or independent mode
2. `UpdateAPIConfig()` now clears `PMConfig/SEConfig/APConfig` to empty **before** calling `rebuildRoleClients()` when in shared mode, preventing stale independent clients from being created
3. `SaveConfig()` now calls `UpdateUseSeparateModels()` first to sync the mode flag into Manager state

**Verified:** 4/4 round-trip model switching tests passed (glm-5 ↔ deepseek-v4-pro), each producing a new client pointer with the correct model name.

### MessageBus Event Loss

**Problem:** TokenMonitor component had `EventsOff` call that caused `token_stats` and `context_built` events to be lost, triggering LOST alerts in conversation.log.

**Fix:** Removed `EventsOff` from TokenMonitor; enhanced LOST alert message to include data preview for faster diagnosis.

---

## Changes

| Component | File | Change |
|-----------|------|--------|
| Types | `internal/types/types.go` | Add `UseSeparateModels bool` field to Config struct |
| Chat Manager | `internal/chat/manager.go` | `UpdateAPIConfig()`: clear role configs in shared mode; add `UpdateUseSeparateModels()` method |
| App | `app.go` | SaveConfig: sync UseSeparateModels before UpdateAPIConfig; initChatManager: pass UseSeparateModels; initChatManagerCLI: same |
| Frontend | `frontend/src/components/SettingsPanel.vue` | Sync `sharedModelId` → `isDefault` on save for persistence across restarts |
| Frontend | `frontend/src/App.vue` | SaveConfig field name alignment |
| Frontend | `frontend/src/components/TokenMonitor.vue` | Remove EventsOff causing event loss |
| AI Client | `internal/ai/client.go` | G-DEBUG log includes client_ptr for debugging model routing |
| MessageBus | `internal/chat/message_bus.go` | Enhanced LOST alert with data preview |

**Total: 9 files changed, +242 / -134 lines**

---

## Upgrade Notes

- No database migration required
- Existing config.json will work as-is; `UseSeparateModels` defaults to false (shared mode)
- After upgrade, model changes in Settings → API Config → Save will take effect **immediately without restart**
- If you previously experienced model reverting after Save, this is now fixed

---

## Download

| Platform | File |
|----------|------|
| Windows x64 | `argus-desktop.exe` |
