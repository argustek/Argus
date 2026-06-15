## Summary

v0.9.4 — Hierarchical document tree memory system for project knowledge management + Prompt refactoring.

## New Features

### Document Tree Memory System
- **Document tree system**: Project documents organized in a hierarchical tree under `.argus/tree/` with YAML frontmatter (owner, status, code_ref, dirty flags, exports)
- **CLI commands**: `--tree` (print doc tree), `--rebuild-tree` (scan project and rebuild), `--check-impact <doc_id>` (find impacted docs)
- **SE tools**: `update_doc` (update doc with role permission check), `log_change` (append to CHANGELOG.md), `get_impacted_docs` (query impact), `sync_doc_exports` (auto-sync Go exports)
- **AP tools**: `verify_doc_exports` (bidirectional code-vs-doc comparison), `check_impact` (impact analysis during audit)
- **Dirty propagation**: `complete_task` marks affected docs dirty; `ClearDirty` runs after AP approval
- **Export extraction**: Go AST-based extraction of exported symbols, synced to doc frontmatter

### Prompt Refactoring
- **Unified English PM core prompt** (~50 lines): decision tree first, principles second, covers all task types (code, system, query, doc, chat)
- **4-level task weight system**: Featherweight ⚡ (PM direct), Lightweight (PM→SE), Medium (full PM→SE→PM→AP), Heavy (Medium + impact analysis)
- **PMRules reference layer**: tool table, task norms, error handling, QA process, AP resolution, TODO management — appended to context without cluttering core prompt
- **Removed hardcoded heuristics**: keyword-based task classification (hello world, fibonacci, create+run, 单文件) deleted from `argus.go`; PM decision tree handles all cases
- **ChatWithTools language injection**: `replyLanguage` parameter added, calls `GetLanguageInstruction` like other chat methods
- **Fixed seExecutionSatisfied**: removed `exit status` / `command failed` hard reject that broke system commands returning non-zero exits
- **pmDirectExecute retry prompt**: generalized from coding-only to cover all task types

## System Requirements

- Windows 10 (version 19044+) / Windows 11 x64
- **WebView2 Runtime** (most systems already have it)
  - If the app fails to start, install from: https://go.microsoft.com/fwlink/p/?LinkId=2124703

## Usage

1. Download `argus-desktop-v0.9.4.exe`
2. Double-click to run (no installation needed)
3. If Windows SmartScreen blocks it, click "More info" → "Run anyway"
