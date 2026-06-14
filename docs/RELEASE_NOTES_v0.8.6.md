# v0.8.6 — Three-Station Consistency & Featherweight Refinement

Release date: 2026-06-13

## Summary

v0.8.6 focuses on fixing the frontend ↔ backend ↔ conversation.log three-station consistency issue, unifying GUI/CLI config loading, and refining featherweight direct execution with proper frontend state synchronization.

## Key Changes

### Three-Station Consistency (Critical Fix)
- **PM message flow fix**: Bridge now emits `pm_message` event with `delta` field matching frontend expectations — eliminated all `MessageBus-LOST` errors
- **conversation.log WYSIWYG**: `PM:`/`SE:`/`AP:`/`USER:` entries only written after frontend ACKs receipt
- **Removed stale `config/messages.json` fallback**: When workDir is empty, messages are no longer loaded from/saved to the old global path (prevents stale error messages from appearing)

### workDir Configuration Overhaul
- **No default workDir**: `getProjectDir()` returns `""` instead of creating a `project/` directory
- **Proper initialization flow**: `SetWorkDir` → `initChatManager()` — app starts with TopBar prompt to select folder
- **Nil guards everywhere**: `SendMessage` returns clear error "请先设置工作目录" when chatManager is nil
- **CLI unified**: `cmd/argus/main.go` now reads `config/config.json` (same as GUI) instead of `.argus/config.yaml`

### Featherweight Direct Execution
- **Silent mode**: `pmDirectExecute` wraps `executeActions` with `c.silent = true` to prevent `exec_start`/`exec_done` from creating empty PM bubbles
- **Frontend emit before execution**: PM text response emitted to frontend before featherweight tool execution begins
- **C Monitor guard**: Prevents `handleToPM` from interrupting featherweight execution

### PM "⚡" Marker Removal
- Stripped from PM output in both `bridge.go` and `manager.go:addPMToUserMsg`
- Replaced by TopBar `shortprocess` indicator

### Other Fixes
- **Memory state**: `RecoverTask` now calls `memoryManager.ClearState()` to clear stuck `hasUnfinished` flag
- **Parallel tool execution**: PM and SE read tools (read_file, grep_content, find_files) execute concurrently for performance
- **MessageBus cleanup**: Removed dead batch code (v0.8.4 rendered batch path unused)
- **Go dependency cleanup**: Removed unused `go-sqlite3` dependency

## Technical Details

### Files Changed
| File | Changes |
|------|---------|
| `internal/chat/bridge.go` | `pm_to_user`→`pm_message` event name; `content`→`delta` field; ⚡ removal; dead `sendToMsgBus` removed |
| `internal/chat/manager.go` | ⚡ removal in `addPMToUserMsg`; `ClearState()` in `RecoverTask`; formatting cleanup |
| `internal/core/argus.go` | `c.silent=true` around `executeActions` in `pmDirectExecute` |
| `app.go` | workDir guards, nil chatManager checks, `SetWorkDir`→`initChatManager` flow |
| `cmd/argus/main.go` | Rewritten to read `config/config.json` instead of `.argus/config.yaml` |
| `frontend/src/App.vue` | PM message handler dedup; `browse` folder picker in `handleSelectProject` |
| `config/messages.json` | Removed (stale error messages file) |

### Upgraded Projects (14 commits, v0.8.5→v0.8.6)
139be0c 9b3d58e 169fd7e 308fda6 6011082 927211f 4c447e5 2b9ce63 fc0b59e ef6e022 94a8f2e b98f097 16a682a f04cdfc

## Upgrade Notes
- **workDir change**: If `config/config.json` has an empty `workDir`, the app will prompt you to select a folder on startup (no more automatic `project/` directory creation)
- **Config compatibility**: CLI now uses the same `config/config.json` as GUI — no separate `.argus/config.yaml` needed
- **API configs preserved**: All `apiConfigs`, `pmConfigId`, `seConfigId`, `apConfigId` settings remain in config.json and are not affected
- **PM behavior change**: Featherweight tasks emit PM text first, then execute tools — frontend shows text immediately instead of waiting for tools to complete