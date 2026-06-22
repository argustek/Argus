# Doc Tree lifecycle plan

## Weight-aware activation

| Weight | Typical size | DocTree behavior |
|--------|-------------|------------------|
| Featherweight ⚡ | < 5 files, depth ≤ 2 | Hidden |
| Lightweight | 5-20 files | Visible but empty |
| Medium+ | > 20 files or depth ≥ 3 | **Auto-activated** |

## User actions

### New Project
1. Input dialog: project name + description
2. Create dir + `.argus/` skeleton (L0 + default L1 stubs)
3. DocTree auto-activated

### Set WorkDir (open existing)
1. Has `.argus/` → activate directly
2. No `.argus/` → detect weight:
   - Medium+: banner "Enable doc management?" → [Enable] generates `.argus/` + scans `.md` files into tree → [Ignore] remembers preference
   - Lightweight/Featherweight: silent, no banner

### Adding docs (once DocTree active)

| Action | Entry point |
|--------|-------------|
| New doc | DocTree right-click → select parent + template |
| Promote file | FileTree right-click `.md` → "Add to doc tree" |
| Drag & drop | Drag `.md` from FileTree into DocTree panel |

## Skeleton generation

```
.argus/
├── PROJECT_PLAN.md          (L0)
└── tree/
    ├── requirements.md      (L1-1, template=PRD)
    ├── design.md            (L1-2, template=design)
    └── schedule.md          (L1-3, template=schedule)
```

Each template has pre-filled frontmatter (`node_id`, `node_title`, `owner_role`).

## Open questions

1. Should "Enable doc management" banner appear once per project or per workDir?
2. Scan existing `.md` — what if there are 50+ files? Bulk import dialog?
3. Drag & drop implementation — native HTML5 DnD or custom?
4. Weight detection — static analysis (file count, dir depth) or AI-assisted?
