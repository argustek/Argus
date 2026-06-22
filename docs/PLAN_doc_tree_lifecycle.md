# Doc Tree lifecycle — decisions

## Status: DECIDED (no UI, AI-driven)

After implementing the DocTree frontend (v0.9.8), we reviewed the lifecycle plan.
All manual UI prompts and user-facing "enable doc management" flows are rejected.
The system relies entirely on AI (PM agent) weight classification.

---

## Three scenarios when setting WorkDir

| Scenario | .argus state | Current behavior | Future (AI-driven) |
|----------|-------------|------------------|-------------------|
| **Empty** | No `.argus/` | DocTree: "📭 No documents found" | PM detects weight → if Medium+, auto-create `.argus/` skeleton + scan `.md` files |
| **Has doc tree** | `.argus/` + `tree/*.md` | DocTree renders fully | Same — no change needed |
| **Has `.argus` no tree** | `.argus/` exists but no `tree/` | DocTree: "📭 No documents found" | PM detects weight → if Medium+, create `tree/` + populate from `.md` files |

## Weight classification (project-level, from `pm_rules.go`)

| Weight | Criteria | DocTree |
|--------|----------|---------|
| Featherweight ⚡ | < 5 files, depth ≤ 2 | Hidden — no doc management |
| Lightweight | 5-20 files | Visible only if PM decides |
| Medium+ | > 20 files or depth ≥ 3 | Auto-activated by PM |

Note: task-level weight (Featherweight/Lightweight/Medium/Heavy in `pm_rules.go`)
is separate from project-level weight. Project weight uses file count + directory depth.

## What was rejected (no UI)

| Proposal | Reason |
|----------|--------|
| "Enable doc management" banner | No banners/prompts — AI decides silently |
| New project dialog (name + description) | Over-engineered; user creates projects externally |
| Drag & drop `.md` into DocTree | No manual interaction; AI handles imports |
| Bulk import dialog for 50+ `.md` files | AI scans and imports automatically |
| Manual enable/disable toggle | AI-driven, no user toggle needed |

## How it works (current + planned)

### Current (v0.9.8)
- Empty workDir → DocTree disabled (shows "No documents found")
- Existing `.argus/tree/` → DocTree renders full hierarchy
- No AI involvement in doc tree lifecycle yet

### Planned
1. User sets workDir
2. System detects no `.argus/` → notifies PM agent
3. PM evaluates project weight (static analysis: file count + depth)
4. If Medium+: PM calls tool to create `.argus/` skeleton, scans `.md` files, populates tree
5. If Featherweight/Lightweight: silent — no doc tree
6. User can always override via `/level` command if PM misjudges

## Questions resolved

1. ~~"Enable doc management" banner per-project or per-workDir?~~ **No banner at all**
2. ~~Bulk import for 50+ files?~~ **AI handles silently**
3. ~~Drag & drop implementation?~~ **Not needed**
4. ~~Weight detection: static or AI-assisted?~~ **Static (file count + depth) as first pass, PM refines**
