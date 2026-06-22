# Doc lifecycle — staggered creation protocol

This doc captures the real complexity of document creation order,
which is **not** a simple "weight → auto-enable" decision.

---

## Core constraint: documents have creation dependencies

```
Phase 0: PROJECT_PLAN.md (L0)
    └─ prerequisite: nothing (project is defined, scope is known)
         │
Phase 1: Feature/requirement docs (L1 nodes)
    └─ prerequisite: PROJECT_PLAN.md exists and is stable
         │  Without a plan, you don't know what features to write
         │
Phase 2: WBS / Schedule (L1 or L2)
    └─ prerequisite: feature docs exist
         │  Without feature breakdown, WBS has no tasks to schedule
         │
Phase 3: Design / implementation details (L2 nodes under features)
    └─ prerequisite: feature doc + WBS exist
         │  Without approved requirements, design is premature
         │
Phase 4: Review / audit docs (L2 nodes)
    └─ prerequisite: implementation is done or in progress
```

Each phase **blocks** the next. A PM cannot schedule before it knows the features.
A PM cannot write features before it has a project plan.

---

## Problem statement

Current code (`BuildTree` / `ScanForDocs`) only **reads existing `.md` files**.
It has zero logic for:

1. **Detecting which phase the project is in** — does PROJECT_PLAN.md exist?
   Are there L1 docs? Is there a WBS?
2. **Knowing what to create next** — given current phase, what doc should PM write?
3. **Ordering creation** — PM could try to write everything at once,
   but that's wrong because doc order matters (plan → features → schedule).
4. **User communication** — should PM auto-proceed to next phase,
   or ask "I've written the plan, shall I decompose into features now?"

---

## Proposed: PM doc state machine

The PM agent gets a new section in its prompt (or a separate protocol):

```
PROJECT DOC PHASES:

currentPhase = detect(rootDir)
  → "empty"           no .argus at all
  → "has_plan"        PROJECT_PLAN.md exists
  → "has_features"    tree/*.md with L1 feature docs exist
  → "has_schedule"    WBS/schedule doc exists
  → "has_design"      L2 design docs exist
  → "complete"        full tree

For each phase, PM knows what to create:

empty:
  1. Write PROJECT_PLAN.md with node_id=L0, parent=""
  2. That's all. Stop. Don't try to write features yet.

has_plan:
  1. Analyze project plan → decompose into features
  2. Write L1 docs (one per feature), each referencing L0 as parent
  3. Stop after features are written.

has_features:
  1. Gather feature docs + plan
  2. Derive task breakdown, estimate effort
  3. Write WBS/schedule doc linking to feature docs
  4. Stop.

has_schedule:
  1. Wait for user to give a build/implementation task
  2. When task arrives, PM may create L2 design docs as needed
  3. Normal PM → SE → AP flow takes over.
```

---

## Key design decisions (unresolved, need discussion)

| Question | Options |
|----------|---------|
| Auto-proceed or ask? | When PM finishes phase 0 (plan), should it auto-start phase 1 (features) or ask user? |
| Phase detection timing | Check on SetWorkDir only? Or periodically? Or when user sends a message? |
| Empty project detection | What if the project has code but no `.argus`? PM should first understand the codebase before writing plan? |
| User override | If PM misjudges the phase, user needs a way to say "skip to phase 3" or "go back to phase 1" |
| Template source | Each doc type needs frontmatter template (node_id naming convention, owner_role, etc.) — where defined? |

---

## What NOT to do (rejected)

- ❌ UI banner "Enable doc management" — no user-facing prompt
- ❌ Manual "New doc" button — AI creates docs
- ❌ Bulk import dialog — AI scans and decides
- ❌ Weight-based auto-enable — weight alone is insufficient; phase is what matters

---

## Relationship to weight

Weight classification (Featherweight/Lightweight/Medium+) still has a role:
it decides **whether the PM enters this state machine at all**.

- Featherweight ⚡: skip entirely. No `.argus/`, no doc lifecycle.
- Lightweight: PM may enter phase 0 (create plan) if the task is non-trivial.
- Medium+: PM must enter phase 0 and follow the full state machine.

But weight is a **gate**, not the **driver**. Phase detection is the driver.
