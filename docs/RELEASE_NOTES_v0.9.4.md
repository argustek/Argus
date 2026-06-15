## Summary

v0.9.4 — Hierarchical document tree memory system for project knowledge management.

## New Features

- **Document tree system**: Project documents organized in a hierarchical tree under `.argus/tree/` with YAML frontmatter (owner, status, code_ref, dirty flags, exports)
- **CLI commands**: `--tree` (print doc tree), `--rebuild-tree` (scan project and rebuild), `--check-impact <doc_id>` (find impacted docs)
- **SE tools**: `update_doc` (update doc with role permission check), `log_change` (append to CHANGELOG.md), `get_impacted_docs` (query impact), `sync_doc_exports` (auto-sync Go exports)
- **AP tools**: `verify_doc_exports` (bidirectional code-vs-doc comparison), `check_impact` (impact analysis during audit)
- **Dirty propagation**: `complete_task` marks affected docs dirty; `ClearDirty` runs after AP approval
- **Export extraction**: Go AST-based extraction of exported symbols, synced to doc frontmatter

## System Requirements

- Windows 10 (version 19044+) / Windows 11 x64
- **WebView2 Runtime** (most systems already have it)
  - If the app fails to start, install from: https://go.microsoft.com/fwlink/p/?LinkId=2124703

## Usage

1. Download `argus-desktop-v0.9.4.exe`
2. Double-click to run (no installation needed)
3. If Windows SmartScreen blocks it, click "More info" → "Run anyway"
