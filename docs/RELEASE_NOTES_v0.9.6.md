## Summary

v0.9.6 — PM agent tool-calling reliability, file tree refresh fix, and SSE exposure.

## What's New

- **SSE Event Exposure**: `se_message`, `review_result`, `ap_result`, `error` events exposed via SSE for external consumers
- **`--send` CLI Flag**: Send a chat message from command line; results logged to `conversation.log`
- **Automated Regression Test**: 3-round "hello world" test via chat send, verified by `conversation.log`

## Bug Fixes

- **PM multi-round tool calling**: PM can now call `list_files`, see results, and call `delete_file` in follow-up rounds (no longer breaks after first tool call)
- **PM subdirectory cleanup**: PM now deletes empty directories after removing files
- **PM `complete_task` crash**: Fixed "unknown action type: complete_task" error that caused retry exhaustion and app crash
- **PM error deadlock**: SE execution errors are now routed back to PM for analysis and retry instead of getting stuck
- **File tree refresh button**: LeftPanel.vue and FileTree.vue refresh buttons now work (silent refresh, no loading flash)
- **Terminal output alert**: Fixed `message_lost` alert when TerminalWindow is closed
- **File change detection**: Added 2s polling + `EventsOn('file-tree-dirty')` for silent auto-refresh
- **Git state tracking**: `config/state.json` removed from git tracking via `.gitignore`

## System Requirements

- Windows 10 (version 19044+) / Windows 11 x64
- **WebView2 Runtime** (most systems already have it)
  - If the app fails to start, install from: https://go.microsoft.com/fwlink/p/?LinkId=2124703

## Usage

1. Download `argus-desktop-v0.9.6.exe`
2. Double-click to run (no installation needed)
3. If Windows SmartScreen blocks it, click "More info" → "Run anyway"
