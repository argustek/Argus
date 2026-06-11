# v0.8.5 — PM Prompt Overhaul & Tool Expansion

Release date: 2026-06-12

## Summary

v0.8.5 is a major overhaul of the PM (Project Manager) AI system prompt and toolset. The PM now behaves more like a real AI assistant — proactively using tools instead of just chatting, directly executing featherweight tasks instead of routing everything to SE, and handling ToolCalls=0 edge cases gracefully.

## Key Changes

### PM Prompt Rewrite
- **New First Principle**: "Always use tools, never just reply with text" — directly addresses the ToolCalls=0 problem at the prompt level
- **Resolved contradiction**: The old prompt simultaneously said "all coding requests must go to @SE" AND "featherweight tasks do it yourself". Now unified with a clear decision tree
- **Decision tree structure**: greeting → clarify → featherweight (direct execution) → heavyweight (@SE) → docs (@SE)
- Removed redundant sections, trimmed from ~270 lines to ~220 lines

### New PM Tools
- **write_file** — Create/overwrite files with auto parent directory creation
- **edit_file** — String replacement in existing files
- **delete_file** — Delete files or empty directories

These enable PM to directly execute featherweight tasks without needing @SE.

### ToolCalls=0 Retry Mechanism
- Added a retry/nag mechanism in ProcessStream: if the model returns ToolCalls=0 on the first call, a system reminder is injected asking it to reconsider
- On second attempt, if still no tools, gracefully degrades to text response
- Prevents silent "just chatting" behavior on task requests

### Other Fixes
- Exec command timeout increased from 30s to 60s (PM exec tool)
- Fixed duplicate condition bug in argus.go (`userLevel == "short" || userLevel == "short"` → `"short" || "featherweight"`)

## Technical Details

### Files Changed
| File | Changes |
|------|---------|
| `internal/ai/pm_prompt.go` | Rewrite system prompt; add write_file/edit_file/delete_file tools; add ToolCalls=0 retry; exec timeout 30s→60s |
| `internal/core/argus.go` | Fix duplicate condition bug in featherweight level detection |
| `docs/pm-prompt-polish-guide.md` | New guide documenting prompt engineering methodology |

### New PM Toolset (13 tools)
update_project_state, read_file, list_files, exec, add_todo, update_todo, web_search, fetch_url, grep_content, find_files, **write_file**, **edit_file**, **delete_file**

## Upgrade Notes
- No config changes required
- PM behavior change: featherweight tasks (single file, <100 lines) now executed directly by PM instead of being routed to @SE
- If you observe PM not responding to code tasks, check conversation.log for ToolCalls=0 entries
