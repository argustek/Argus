# v0.7.0 Release Notes

> **Release Date**: 2026-06-08
> **Tag**: `v0.7.0`
> **Previous**: `v0.6.5`
> **Commits**: 19 | **Lines Added**: ~10,900 (Go)

---

## What's New in v0.7.0

v0.7.0 is a major capability expansion across **document processing, core execution reliability, developer experience, and configuration architecture**.

---

## Document Processing Engine (New)

- **PDF/Word read & write** (`read_pdf`, `read_docx`, `write_docx`) — powered by PyMuPDF & python-docx
- **Document comparison** (`compare_documents`) — diff PDF vs Word, extract changes
- **OCR support** — automatic OCR plugin installation for scanned PDFs
- **Tool self-bootstrap** — auto-install missing Python dependencies (pip install on demand)
- 8 new SE tools registered for document workflows

## Post-Execution Summary (Architecture Change)

- **Two-phase execution mode**: SE executes tools → auto-generates natural language summary → PM Review
- Solves the root cause of PM rejections: when SE returned only JSON actions without human-readable output
- Smart trigger: activates when SE display text < 20 chars AND execution results exist
- Fallback: concatenates exec results if AI summary generation fails
- `extractDisplayText()`: filters JSON blocks, `@SE` directives, preserves `@USR` content

## Multi-Language LSP Integration

- LSP support for **Go, TypeScript, JavaScript, Python, Rust, C, C++**
- Code intelligence: completions, definitions, references, diagnostics
- Integrated into SE toolchain for context-aware code modifications

## Per-Role Model Configuration

- PM, SE, AP can each use **different AI models**
- Configurable via decision config / environment
- Enables cost optimization (e.g., cheap model for SE, strong model for PM review)

## Enhanced SE Toolset (31 tools total)

| Tool | Description |
|------|-------------|
| `debug_run` | Auto debug flags + panic/trace formatting |
| `show_diff` | Diff preview dialog |
| `analyze_code` | Static analysis + suggestions |
| `auto_debug` | Automatic bug diagnosis |
| `snippets` | 9 code templates (Round 1) |
| `parallel_web_search` | 3 search engines racing for results |
| `fetch_url` | Fetch and read web page content |
| `glob` | Fast file pattern matching |
| `delete_file` | Safe file deletion |

## Reliability Improvements

- **SE ToolCall 3-layer defense** against LLM JSON corruption (trim, bracket fix, regex fallback)
- **seExecutionSatisfied expanded** from only recognizing `exec` to 11 tool types (read_file, write_file, list_files, etc.)
- **Circuit breaker detection** in self-fix loop — stops immediately when breaker is open instead of wasting retries
- **Shell session idle cleanup** — auto-terminate stale sessions
- **PathRule protection** for `.env`, credentials, secrets files
- **write_file truncation guard** — prevents oversized file writes

## UI/UX

- **Agent thinking visualization** — real-time PM/SE/AP status indicators
- **Diff preview dialog** — side-by-side change review
- **Multimodal input** — image + text support
- **Git async event bus** — non-blocking git operations
- **MessageBus batch tracking** — improved message delivery reliability

## Regression Tests

- 28+ unit tests passing (core, AI, executor modules)
- 10/10 regression test suite pass rate
- 7 new tests for Post-Execution Summary logic

---

## Upgrade Notes

- Python runtime required for document processing tools (PDF/Word) — auto-installed on first use
- Existing configurations remain compatible; per-role model config is opt-in

---

## Full Changelog (19 commits since v0.6.5)

```
49d6d2b feat: Post-Execution Summary, doc tools, and SE/PM reliability fixes
6d199d1 feat: enhance PM prompt system, expand core argus capabilities
7de0c58 feat: agent thinking visualization, shell session idle cleanup, git async event bus
d3c6124 docs: sync gap_analysis with code audit, add Phase 3.5 shell session cleanup
e5e6ff9 feat: register show_diff and debug_run SE tools (31 tools)
e098cdb fix: revert SE to text mode, SSE pendingJSON [10/10 regression passed]
d5e70f2 feat: analyze_code + auto_debug SE tools, SnippetStore persistence
6f8f981 feat: Diff preview dialog + multi-agent planning doc
9e9fbc0 feat: per-role model config (PM/SE/AP different models)
69b74a6 feat: Round 4 multi-language LSP (Go/TS/JS/Python/Rust/C/C++)
0e7d710 feat: Round 3 debug_run tool (auto debug flags)
b3fbe86 feat: snippets library (9 templates) + diff preview (ComputeDiff)
4e6e6bc feat: .env/credentials/secrets PathRule protection
399cc51 feat: parallel web search (3 engines race) + fetch_url tool
e31f7ac feat: P0 core - LSP integration, undo/rollback, multimodal input
af8c668 fix: SE ToolCall robustness - 3-layer defense vs corruption
18cb840 feat: direct tool endpoints, emit cleanup, semantic search, shell session
f6ed389 feat: SE toolset upgrade (glob/web_search/delete_file)
```
