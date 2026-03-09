# Source Configuration Redesign

## Overview

The current source configuration workflow is a flat list of all discovered items across all teams, requiring users to manually tag each one with a purpose and team. The new design is team-centric and slot-driven: you navigate into a team, set a project homepage, and the system uses AI to automatically extract and wire up the rest. Every auto-configured slot can be reviewed and overridden.

**Goals:**
- Make it obvious what configuration a team needs (slots as a checklist)
- Eliminate most of the manual tagging work via homepage extraction
- Integrate discovery inline so you never need to leave the config flow
- Give a clear at-a-glance view of whether a team is fully configured

---

## Slots

Each slot has a cardinality (single or multi), a set of compatible source types, and a flag for whether it can be auto-populated from homepage extraction.

**Per-team slots:**

| Slot | Purpose key | Cardinality | Compatible source types | Auto-extracted |
|---|---|---|---|---|
| Project Homepage | `project_homepage` | single | notion_page, github_file | — |
| Goals Doc | `goals_doc` | single | notion_page, github_file | yes |
| Sprint Plans | `sprint_doc` | multi | notion_page, github_file | yes |
| GitHub Repos | `github_repo` | multi | github_repo | yes |
| Metrics | `metrics_panel` | multi | grafana, posthog, signoz | yes |
| Task Label | `task_label` | single | github_label | no |

**Org-level slots** (team_id = NULL):

| Slot | Purpose key | Cardinality | Compatible source types | Auto-extracted |
|---|---|---|---|---|
| Project Homepage | `project_homepage` | single | notion_page, github_file | — |
| Org Goals | `org_goals` | single | notion_page, github_file | yes |
| Org Milestones | `org_milestones` | multi | notion_page, github_project | yes |
| Metrics | `metrics_panel` | multi | grafana, posthog, signoz | yes |

**Sprint status.** Sprint docs have an additional `sprint_status` field stored in `config_meta` JSON: `"current"`, `"next"`, or `"archived"`. This is set by the homepage extraction AI and updated on re-extraction. It replaces the old `current_plan`/`next_plan` purpose split.

**Provenance.** Every `source_config` row carries a `provenance` field: `"manual"` or `"ai_extracted"`. This controls what gets refreshed on re-extraction and drives the `ai` badge in the TUI.

---

## Homepage Extraction Pipeline

**Pipeline name:** `homepage_extract`

**Input:** full text content of the homepage document.

**Task:** Given the page content, identify and extract links or references to: the goals or OKR document, sprint planning documents (indicate which is current, next, or historical), GitHub repositories, and metrics dashboards. Ignore navigation boilerplate, team member lists, and anything not matching those categories.

**Output schema:**
```json
{
  "goals_doc": "https://notion.so/...",
  "sprint_plans": [
    { "url": "https://notion.so/...", "title": "Phase 2B", "sprint_status": "current" },
    { "url": "https://notion.so/...", "title": "Phase 2A", "sprint_status": "next" },
    { "url": "https://notion.so/...", "title": "Phase 1",  "sprint_status": "archived" }
  ],
  "repos": [
    "https://github.com/co/nightshade"
  ],
  "metrics": [
    "https://app.posthog.com/..."
  ]
}
```

Any field can be null or empty if not found. The AI must not hallucinate URLs.

**Post-extraction steps** (run by sync engine after AI call):
1. For each extracted URL, run discovery if not already in catalogue
2. Upsert a `source_config` row with `provenance = 'ai_extracted'` and the appropriate purpose
3. For sprint docs, set `config_meta = {"sprint_status": "current|next|archived"}`
4. Store raw extraction result in `ai_cache` keyed by `(homepage_catalogue_id, 'homepage_extract', team_id)`

**Re-extraction behavior:** Only rows with `provenance = 'ai_extracted'` are replaced. Rows with `provenance = 'manual'` are preserved. Newly extracted items are added; previously extracted items no longer in the output are removed.

---

## Sprint System Redesign

**Old system:** `sprint_parse` reads the `current_plan` source. Sprint week derived from `start_date` extracted from the sprint doc itself.

**New system:** `sprint_parse` reads the `sprint_doc` with `config_meta.sprint_status = "current"`. The determination of which sprint is current is made by `homepage_extract` and stored in `config_meta`. The sprint doc is parsed only for content: goals, total sprints, sprint number.

**Backward compatibility:** The pipeline prefers `sprint_doc` + `sprint_status` if present, falls back to `current_plan`/`next_plan` if not. Existing configurations continue to work without migration.

Same pattern for `goal_extraction`: prefers `goals_doc` purpose, falls back to `goals`.

---

## Data Model Changes

### `source_configs` — new columns

```sql
-- Migration 0018
ALTER TABLE source_configs ADD COLUMN provenance TEXT NOT NULL DEFAULT 'manual'
  CHECK(provenance IN ('manual', 'ai_extracted'));
```

### `source_configs` — updated purpose CHECK constraint

```sql
-- Migration 0019: recreate table with expanded CHECK
purpose TEXT NOT NULL CHECK(purpose IN (
  -- new
  'project_homepage',
  'goals_doc',
  'sprint_doc',
  'github_repo',
  -- existing
  'task_label',
  'metrics_panel',
  'org_goals',
  'org_milestones',
  -- legacy (kept for backward compat)
  'current_plan',
  'next_plan',
  'goals'
))
```

### Optional backfill

```sql
-- Migration 0020 (optional, run after confirming new system works)
UPDATE source_configs SET purpose = 'goals_doc',
  config_meta = '{"sprint_status":"current"}' WHERE purpose = 'current_plan';
UPDATE source_configs SET purpose = 'sprint_doc',
  config_meta = '{"sprint_status":"next"}'    WHERE purpose = 'next_plan';
UPDATE source_configs SET purpose = 'goals_doc' WHERE purpose = 'goals';
```

---

## API Changes

### `POST /teams/{id}/homepage`

Sets the project homepage and triggers async extraction.

Request: `{ "catalogue_id": 123 }`
Response (202): `{ "sync_run_id": 456 }`

Replaces any existing `project_homepage` config for the team. The sync run result includes a summary of slot changes in `sync_runs.result_meta`.

### `POST /teams/{id}/config/reextract`

Re-runs homepage extraction. Refreshes `ai_extracted` rows; preserves `manual` rows.

Response (202): `{ "sync_run_id": 789 }`

### `GET /teams/{id}/config`

Returns the full slot configuration grouped by slot. Used by the TUI overview screen.

Response:
```json
{
  "team_id": 1,
  "team_name": "Nightshade",
  "extraction_status": "done",
  "slots": {
    "project_homepage": {
      "items": [{ "id": 10, "title": "...", "source_type": "notion_page", "url": "...", "provenance": "manual" }]
    },
    "goals_doc": {
      "items": [{ "id": 20, "title": "...", "source_type": "github_file", "url": "...", "provenance": "ai_extracted" }]
    },
    "sprint_doc": {
      "items": [
        { "id": 30, "title": "Phase 2B", "source_type": "notion_page", "provenance": "ai_extracted", "sprint_status": "current" },
        { "id": 31, "title": "Phase 2A", "source_type": "notion_page", "provenance": "ai_extracted", "sprint_status": "next" }
      ]
    },
    "github_repo":   { "items": [...] },
    "metrics_panel": { "items": [] },
    "task_label":    { "items": [] }
  }
}
```

`extraction_status` is `"none"` (no homepage), `"running"` (sync run in flight), or `"done"`.

### `GET /config/sources` (updated)

Gains optional `?source_type=` and `?team_id=` query params so the source picker can filter to compatible items.

### `PUT /config/sources/{id}` (unchanged)

When called from the new TUI, sets `provenance = 'manual'` on the upserted config.

---

## TUI Screens

### Entry points

- `OrgOverviewView`: `c` on selected team → `ConfigTeamSlotsView`
- `ConfigRootView`: "Teams" → team list → `ConfigTeamSlotsView`
- `ConfigRootView`: "Org" → `ConfigOrgSlotsView` (new)
- `ConfigRootView`: "All Sources" → existing `ConfigSourcesView` (kept for power users)

### `ConfigRootView` (updated)

```
  Config

> Teams
  Org
  All Sources
  Users
```

### `ConfigTeamSlotsView`

**No homepage set:**
```
  Configure — Nightshade

> Project Homepage   (not set — start here)                         ○

  ─── Will be auto-configured from homepage ─────────────────────────
  Goals Doc          (set homepage first)                           ○
  Sprint Plans       (set homepage first)                           ○
  GitHub Repos       (set homepage first)                           ○
  Metrics            (set homepage first)                           ○

  ─── Manual ─────────────────────────────────────────────────────────
  Task Label         (none)                                         ○

  j/k navigate  ·  Enter edit  ·  Esc back
```

**Extraction in progress:**
```
  Configure — Nightshade

  Project Homepage   Nightshade Project Home                        ✓

  ─── Analyzing homepage… ────────────────────────────────────────────
  Goals Doc          …
  Sprint Plans       …
  GitHub Repos       …
  Metrics            …

  ─── Manual ─────────────────────────────────────────────────────────
  Task Label         (none)                                         ○

  j/k navigate  ·  Enter edit  ·  Esc back
```

**Extraction done:**
```
  Configure — Nightshade

  Project Homepage   Nightshade Project Home                        ✓

  ─── Auto-configured from homepage ──────────────────────────────────
  Goals Doc          Nightshade Goals & OKRs.md                 ✓  ai
  Sprint Plans       Phase 2B (current), Phase 2A (+1 more)     ✓  ai
  GitHub Repos       github.com/co/nightshade (+1 more)         ✓  ai
  Metrics            (none found — add manually)                ○  ai

  ─── Manual ──────────────────────────────────────────────────────────
  Task Label         Nightshade                                 ✓

  j/k navigate  ·  Enter edit  ·  e re-extract  ·  Esc back
```

### `ConfigHomepageSlotView`

**Not set:**
```
  Configure — Nightshade — Project Homepage

  No homepage set.

  Setting the homepage lets the system automatically configure your
  Goals, Sprint Plans, Repos, and Metrics slots by analyzing the page.
  You can review and override everything after.

  Enter to pick a source  ·  Esc back
```

**Set:**
```
  Configure — Nightshade — Project Homepage

  Current:

    Nightshade Project Home
    notion_page  ·  notion.so/company/nightshade-home

  Last extracted:  2026-03-07 14:32

  Enter to change  ·  e re-extract  ·  x clear  ·  Esc back
```

**Confirm change:**
```
  Configure — Nightshade — Project Homepage

  Replace homepage and re-run extraction?

  Current:    Nightshade Project Home
  New:        Nightshade New Home 2026

  AI-extracted slots will be refreshed.
  Manually added sources will be kept.

> Yes, replace and re-extract
  No, cancel
```

### `ConfigSingleSlotView`

**Source set:**
```
  Configure — Nightshade — Goals Doc

  Source:

    Nightshade Goals & OKRs.md                           [ai]
    github_file  ·  github.com/co/nightshade/docs/goals.md

  Enter to replace  ·  x to clear  ·  Esc back
```

**Not set:**
```
  Configure — Nightshade — Goals Doc

  No source set.

  Enter to pick a source  ·  Esc back
```

**Task Label — no labels discovered:**
```
  Configure — Nightshade — Task Label

  No GitHub labels discovered yet.
  Add a repo first to discover its labels.

  a add a repo to discover labels  ·  Esc back
```

### `ConfigMultiSlotView`

**Sprint Plans:**
```
  Configure — Nightshade — Sprint Plans

  From homepage (AI):
  > [notion_page]   Phase 2B — Nightshade Sprint Plan    current
    [notion_page]   Phase 2A — Nightshade Sprint Plan    next
    [notion_page]   Phase 1  — Nightshade Sprint Plan    archived

  Manually added:
    (none)

  a add  ·  x remove  ·  Esc back
```

**GitHub Repos:**
```
  Configure — Nightshade — GitHub Repos

  From homepage (AI):
  > github.com/co/nightshade-api
    github.com/co/nightshade-web

  Manually added:
    (none)

  a add  ·  x remove  ·  Esc back
```

**Metrics (empty):**
```
  Configure — Nightshade — Metrics

  From homepage (AI):
    (none found)

  Manually added:
    (none)

  a add  ·  Esc back
```

### `ConfigSourcePickerView`

Used by all slots when adding a source. Filters to compatible types for the slot by default.

```
  Configure — Nightshade — Sprint Plans — Add Source

  [f: notion_page, github_file ▾]    / search…

    TYPE             TITLE                                    ASSIGNED TO
  > [notion_page]    Phase 2B — Nightshade Sprint Plan       Nightshade
    [notion_page]    Platform Sprint Plan Q1                 Platform
    [github_file]    docs/sprint-2b.md                       (none)

  ──────────────────────────────────────────────────────────────────────
  + Discover a new source…

  j/k navigate  ·  Enter select  ·  f filter  ·  / search  ·  Esc cancel
```

### `ConfigDiscoverInlineView`

**Input:**
```
  Discover New Source

  Accepts: Notion URL  ·  GitHub URL  ·  GitHub owner/repo  ·  owner/repo/path/file.md

  > _______________________________________________________________

  Enter to start  ·  Esc cancel
```

**Single result:**
```
  Discover New Source

  Found:

  > [notion_page]   Phase 3 — Nightshade Sprint Plan

  Enter to add  ·  Esc cancel
```

**Multiple results (e.g. parent page with children):**
```
  Discover New Source

  Found 4 items. Select to add:

  > [notion_page]   Nightshade Sprint Plans (parent)
    [notion_page]   Phase 2B — Nightshade Sprint Plan
    [notion_page]   Phase 2A — Nightshade Sprint Plan
    [notion_page]   Phase 1  — Nightshade Sprint Plan

  j/k navigate  ·  Space select  ·  Enter add selected  ·  Esc cancel
```

### `ConfigOrgSlotsView`

Same structure as `ConfigTeamSlotsView` with org-level slots.

```
  Configure — Org

> Project Homepage   Company Hub                               ✓

  ─── Auto-configured from homepage ──────────────────────────────────
  Goals              Company OKRs 2026                      ✓  ai
  Milestones         Roadmap 2026  (+1 more)                ✓  ai
  Metrics            (none found)                           ○  ai

  j/k navigate  ·  Enter edit  ·  e re-extract  ·  Esc back
```

---

## Migration Plan

1. **Migration 0018** — add `provenance` column to `source_configs`
2. **Migration 0019** — expand `purpose` CHECK constraint with new values
3. **Migration 0020** (optional, after new system confirmed working) — backfill new purposes from old ones
4. Update `sprint_parse` pipeline to prefer `sprint_doc` + `sprint_status`, fall back to `current_plan`
5. Update `goal_extraction` pipeline to prefer `goals_doc`, fall back to `goals`
6. New `homepage_extract` pipeline
7. New API endpoints
8. New TUI views; update `ConfigRootView`
