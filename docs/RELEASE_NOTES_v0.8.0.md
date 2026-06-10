# Argus v0.8.0 Release Notes

**Release Date:** 2026-06-10

---

## Summary

v0.8.0 is a **feature + bug-fix release** that introduces **PM DirectExecute (Featherweight mode)** for lightweight tasks, and fixes the remaining **API config hot-reload gap where Bridge/ArgusCore client was not synced on model switch**.

---

## Features

### PM DirectExecute (Featherweight Mode)

PM can now handle lightweight tasks in a single LLM call, bypassing the full SE → Review → AP pipeline. This significantly reduces latency for simple requests like "create a file", "write a function", or "run a quick test".

**How it works:**
- User sends a message; PM analyzes task complexity
- If classified as **Featherweight** (single file, <100 lines, no dependencies), PM calls `pmDirectExecute()`
- PM executes actions directly with its own tool-calling capability
- Result returned in one round-trip instead of 3-4 rounds

### Auto Task Grading

Tasks are automatically graded into three levels:

| Level | Criteria | Flow |
|-------|----------|------|
| **Featherweight** | Single file / <100 lines / no deps | pmDirectExecute (1 call) |
| **Lightweight** | 2-5 files / <500 lines / single feature | PM → SE → AP (fast path) |
| **Medium** | Multi-module / <5000 lines / internal deps | Full pipeline with Review loops |

Users can override auto-grading by including `/level featherweight|lightweight|medium` in their message.

### UseSeparateModels Config

Settings panel now supports two API model modes:
- **Shared mode** (default): All roles use the same model from "All roles share" dropdown
- **Independent mode**: PM/SE/AP each have their own model configuration

---

## Bug Fixes

### Bridge/ArgusCore Client Not Synced on Hot-Reload (Critical)

**Problem:** After v0.7.3 fixed `UpdateAPIConfig()` to properly rebuild Manager's `aiClient`, there was still a **second client path that was stale**: `pmDirectExecute()` in `argus.go` calls `c.client.ChatWithTools()`, where `c.client` is the `ArgusCore.client` set at construction time via `Bridge`. When user switched models via SaveConfig, only `Manager.aiClient` and `Manager.pmProcessor` were updated — `Bridge.ArgusCore.client` kept pointing to the old model.

**Evidence from conversation.log:**
```
[16:49:43] [SaveConfig] PM-Update → model=qwen/qwen3.5-122b-a10b (nvidia)
[16:50:00] [G-DEBUG] model=glm-5  ← WRONG! Still using old opencode model
[16:50:00] client_ptr=0x849570912d0  ← Same pointer as before save!
```

**Root Cause:** Two independent client ownership chains:
1. `Manager.aiClient` / `Manager.pmProcessor` — updated by `SaveConfig` ✅
2. `Bridge.ArgusCore.client` / `Bridge.PMProcessor` — created once at init, never updated ❌

`pmDirectExecute()` uses chain #2, so it always used the startup model regardless of config changes.

**Fix:** Three-layer change:
1. **`internal/core/argus.go`**: Added `SetClient(AICaller)` method to allow hot-swapping the core client
2. **`internal/chat/bridge.go`**: Added `UpdateClient(*ai.Client, workDir)` method that updates both `ArgusCore.client` and rebuilds `PMProcessor` with new client
3. **`app.go`**: After `chatManager.UpdateAPIConfig()`, call `bridge.UpdateClient(chatManager.GetAIClient(), workDir)` to sync

**Verified:** Post-fix, `client_ptr` changes after SaveConfig and actual LLM calls use the correct model URL.

### Startup Init Fix

Unified all role initialization paths to use `findAPIConfigByID(config.XXXConfigID)` instead of reading deprecated `APConfig` field. This ensures consistent behavior between cold start and hot-reload.

---

## Changes

| Component | File | Change |
|-----------|------|--------|
| Core | `internal/core/argus.go` | Add `SetClient(AICaller)` method for hot-swap |
| Chat Bridge | `internal/chat/bridge.go` | Add `UpdateClient()` to sync ArgusCore + PMProcessor on config change |
| App | `app.go` | SaveConfig: call `bridge.UpdateClient()` after `UpdateAPIConfig()` |
| Docs | `docs/phase1-pmdirect-spec.md` | Add Featherweight PM direct execute specification |

**Total: 4 files changed**

---

## Upgrade Notes

- No database migration required
- Existing `config.json` works as-is
- Model switching now takes effect **immediately without restart** for ALL code paths (including pmDirectExecute)
- New `pmDirectExecute` path is active by default for lightweight tasks

---

## Download

| Platform | File |
|----------|------|
| Windows x64 | `argus-desktop-v0.8.0.exe` |

**System Requirements:**
- Windows 10 (version 19044+) / Windows 11 x64
- WebView2 Runtime (most systems already have it)
- If app fails to start, install from: https://go.microsoft.com/fwlink/p/?LinkId=2124703
