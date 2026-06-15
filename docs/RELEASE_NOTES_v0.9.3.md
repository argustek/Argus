## Summary

v0.9.3 — Critical PM output fix, SSE push stability improvements.

## Bug Fixes

- **PM message missing**: Fixed PM bubble not appearing in normal SE flow — now emits `pm_to_user` at all `ProcessStream` exit points, ensuring conversation.log always has a PM entry
- **SSE push race condition**: Wired `PushSSEEvent` for real-time PM message push so frontend receives PM text without polling
- **Frontend PM accumulation**: Fixed `pm_message` event handling to append to existing PM message instead of replacing, preserving streaming output
- **HTTP ack path**: Fixed message routing for HTTP API clients (no-frontend mode)

## Other

- Updated README with Discussions link, v0.9.x badge, solo maintainer note
- Added prompt refactoring plan documentation (3-layer architecture)

## System Requirements

- Windows 10 (version 19044+) / Windows 11 x64
- **WebView2 Runtime** (most systems already have it)
  - If the app fails to start, install from: https://go.microsoft.com/fwlink/p/?LinkId=2124703

## Usage

1. Download `argus-desktop-v0.9.3.exe`
2. Double-click to run (no installation needed)
3. If Windows SmartScreen blocks it, click "More info" → "Run anyway"
