## Summary

v0.9.0 — Branding polish, bug fixes, and DX improvements.

## What's New

- **Branding**: Custom app icon (taskbar/title bar), product name "Argus · 驭码"
- **File Tree Context Menu**: Right-click files for "Open in Explorer", "Copy Path", "Run", "Add to Chat"
- **Explorer Integration**: "Open in Explorer" for both work directory and individual files

## Bug Fixes

- **Explorer crash**: Fixed app freeze when opening file location (cmd.Run → cmd.Start)
- **Error dialog crash**: Caught unhandled promise rejections from Wails bindings
- **Editor highlighting**: Syntax highlighting now works on first file open
- **Startup flash**: Suppressed console windows for background processes (gopls, MCP, env scan)
- **Dead code**: Removed unused TitleBar component

## System Requirements

- Windows 10 (version 19044+) / Windows 11 x64
- **WebView2 Runtime** (most systems already have it)
  - If the app fails to start, install from: https://go.microsoft.com/fwlink/p/?LinkId=2124703

## Usage

1. Download `argus-desktop-v0.9.0.exe`
2. Double-click to run (no installation needed)
3. If Windows SmartScreen blocks it, click "More info" → "Run anyway"
