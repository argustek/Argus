# Argus v0.7.1 Release Notes

**Release Date:** 2026-06-08

---

## Summary

v0.7.1 focuses on **Settings Panel UX improvements**, **MCP (Model Context Protocol) foundation**, **Tab completion for terminal**, **structured Go test output parsing**, and **memory system enhancements**.

---

## New Features

### Settings Panel Enhancements
- **Draggable window**: Settings panel can now be repositioned by dragging the title bar
- **Resizable panel width**: Drag the right edge handle to adjust panel width (600px ~ window width, default 920px)
- **Resizable table columns**: API config table columns (Provider, URL, Model, API Key) support drag-to-resize
- **All drag states persisted**: Panel position, width, and column widths are saved to localStorage and restored on next open
- **Fixed About page blank issue**: Corrected DOM structure so About tab content renders properly

### MCP (Model Context Protocol) Foundation
- **JSON-RPC stdio Client**: Full implementation of the MCP client protocol (`internal/mcp/client.go`)
- **MCP Manager**: Server lifecycle management with 5 REST API endpoints
- **SE Tool Bridge**: MCP tools are exposed to SE (Software Engineer) role via tool bridge
- **Type system**: Complete type definitions for MCP protocol messages

### Terminal Tab Completion
- **Command completion**: Auto-complete shell commands in terminal input
- **File path completion**: Navigate and complete file/directory paths
- **Directory completion**: Directory name suggestions with trailing slash
- **UI**: Candidate list popup with cycle selection (Tab/Shift+Tab)

### Structured Go Test Output
- **JSON mode parsing**: `go test --json` output is parsed into structured `TestCase` objects
- **Rich test metadata**: Each test case includes File, Line, Expected, Actual fields
- **Graceful fallback**: If JSON parsing fails, falls back to text-based parsing

### Memory System Improvements
- **First interaction timestamp**: `firstInteractionTime` properly propagated through memory context
- **Relationship stages**: Social relationship stage detection now active and effective

### HTTP API Service
- **New HTTP server module** (`http_server.go`): Exposes internal capabilities via REST API
- **Configurable port and auth token**
- **Remote access toggle**

---

## Bug Fixes

- Fixed SE execution producing no natural language summary after action execution (Post-Execution Summary mechanism)
- Fixed circuit breaker causing ineffective retries after timeout
- Fixed About page rendering blank due to nested `v-if` structure
- Fixed API config changes requiring restart to take effect

---

## Technical Details

| Component | Files Changed | Lines |
|-----------|--------------|-------|
| MCP Module | 3 files | +716 |
| Terminal/Shell | 2 files | +321 |
| Executor/Test | 2 files | +313 |
| HTTP Server | 1 file | +244 |
| Chat/Memory | 1 file | +85 |
| Frontend (Settings) | 1 file | +~200 |
| Frontend (Terminal) | 1 file | +82 |
| App/Bindings | 3 files | +114 |

**Total: ~16 files changed, +2077 / -136 lines**

---

## Upgrade Notes

- No database migration required
- Settings panel layout has changed significantly — existing users will see a larger, draggable settings window
- Column widths in API config table are user-adjustable and persisted
- New MCP features require external MCP servers to be configured (foundation only, UI configuration coming in future releases)

---

## Download

| Platform | File |
|----------|------|
| Windows x64 | `argus-desktop.exe` |
