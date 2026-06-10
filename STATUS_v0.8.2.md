# Argus v0.8.2 Status Summary

> Date: 2026-06-11
> Branch: main
> Incremental fixes since v0.8.1

## Fixes

### 1. web_search Crash
- **File**: `internal/ai/pm_prompt.go`
- **Issue**: Uncaught panic in web_search goroutine crashed the process
- **Fix**: Added `defer recover` in both `executeTool()` and each search goroutine

### 2. PM stuck "busy" after reset
- **File**: `internal/chat/manager.go`
- **Issue**: `ExecuteReset` cleared backend state but didn't notify frontend
- **Fix**: Push `phase:reset|role:none|status:idle` via MessageBus on reset

### 3. Ghost output after reset
- **File**: `internal/chat/manager.go`
- **Issue**: In-flight LLM stream continued pushing deltas after reset
- **Fix**: PM/SE stream callbacks check `isGhostCall(aiGen)` and discard stale deltas

### 4. Hardcoded paths
- **Files**: `internal/monitor/c_monitor.go`, `internal/chat/pdca_test.go`
- **Fix**: `.argus/state.json` → `config/state.json`

## Known Issues

### P0 - DeepSeek tool fidelity
- DeepSeek `deepseek-ai/deepseek-v4-pro` calls list_files then claims "cleanup done" without actually deleting anything (ToolCalls includes list_files but no delete_file)
- Root cause: LLM comprehension limitation, not prompt-fixable

### P1 - CMonitor timer race
- False "project running but idle" alert fires 13s after reset while PM is still processing
- Root cause: CMonitor ticker runs immediately on startup; ProjectState persists from old session
- Suggestion: Reset ProjectState to Idle on reset, or delay CMonitor initial check

### P2 - Qwen weak tool use
- `qwen/qwen3-next-80b-a3b-instruct` never calls tools (ToolCalls=0), only asks A/B/C questions
- Strategy: switched to DeepSeek

## Current State
- ✅ No crash on web_search
- ✅ Status correctly resets to idle
- ✅ No ghost output after reset
- ✅ `logs/conversation.log` is the correct log path
- ❌ DeepSeek lies about completing tasks
- ❌ CMonitor false alarm on timer race