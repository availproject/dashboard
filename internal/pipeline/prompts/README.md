# Pipeline Prompts

Every LLM call shares the same wrapper structure, built by `buildPrompt` in `shared.go`:

```
{prompt_wrapper.txt} <schema>{*.schema.json}</schema> [<instructions>{*.instructions.txt}</instructions>] [<annotations>[...]</annotations>] <inputs>{...}</inputs>
```

- **`prompt_wrapper.txt`** — the opening instruction (editable)
- **`*.schema.json`** — the JSON shape the model must produce for this pipeline
- **`*.instructions.txt`** — natural-language rules for this pipeline; rendered as plain text in `<instructions>`, not JSON-escaped; omitted for pipelines with no instructions file
- **`<annotations>`** — active team annotations from the dashboard UI; omitted when empty
- **`<inputs>`** — JSON object with all data for this call (never contains `instructions`)

Results are cached by SHA-256 of the full prompt. A sync where source data is unchanged makes zero model calls.

---

## LLM calls

### Team sync (`r` in team view)

**Phase 1** — parallel

| Files | Pipeline key | Runs when | Inputs |
|-------|--------------|-----------|--------|
| `team_sync-p1-sprint_parse.*` | `sprint_parse` | current plan doc was fetched | `sprint_plan_text`, `today`, `instructions` |
| `team_sync-p1-velocity.*` | `velocity_analysis` | always | `sprints` (closed issues, merged PR count, commit count) |

**Phase 2** — parallel, after phase 1

| Files | Pipeline key | Runs when | Inputs |
|-------|--------------|-----------|--------|
| `team_sync-p2-team_status.*` | `team_status` | any plan or goals doc present | `today`, `goals_doc_text`, `sprint_plan_text`, `sprint_meta`, `sprint_timing`, `open_issues`, `merged_prs`, `instructions`; optionally `marketing_campaigns` |
| `team_sync-p2-workload.*` | `workload_estimation` | team has members configured | `members` (per-person open issues / merged PRs / commits), `sprint_window`, `standard_sprint_days` |

### Org sync (`r` from org overview)

Runs a full team sync for every team, then one additional call:

| Files | Pipeline key | Inputs |
|-------|--------------|--------|
| `org_sync-goal_alignment.*` | `goal_alignment` | `org_goals_text`, `team_goals` (map of team_id → business goal texts) |

### Discovery (source setup flow)

| Files | Pipeline key | Inputs |
|-------|--------------|--------|
| `discovery-homepage_extract.*` | `homepage_extract` | `homepage_text`, `today`, `instructions` |
| `discovery-discovery_suggestion.*` | `discovery_suggestion` | `title`, `excerpt` (first ~500 chars of content) |

### Autotag (`t` in team view)

| Files | Pipeline key | Inputs |
|-------|--------------|--------|
| `autotag-label_match.*` | `label_match` | `teams` (list of team names), `labels` (list of `{id, name}`) |

### Not in active sync (`team_sync-concerns.*`)

`concerns` was a predecessor to `team_status`. The schema and Runner method still exist but the pipeline is not called during sync. The `team_status` pipeline now covers concerns as part of its output.

---

## Example prompt — `team_status`

This is representative of what the model receives. Instructions are plain text in their own block; data inputs are JSON. The model responds with JSON only.

```
You are analyzing a software team's status data. Respond ONLY with valid JSON matching this schema: <schema>{"business_goals":[{"text":"string","status":"on_track|at_risk|behind","note":"string"}],"sprint_goals":[{"text":"string","status":"likely_done|at_risk|unclear","note":"string"}],"sprint_forecast":"string","concerns":[{"key":"string","summary":"string","explanation":"string","severity":"high|medium|low","scope":"strategic|sprint"}]}</schema> <instructions>Assess the likelihood of each business goal and sprint objective being completed on time... [full contents of team_sync-p2-team_status.instructions.txt]</instructions> <inputs>{"goals_doc_text":"## Alpha Launch Goals\n- Ship mainnet bridge by end of Sprint 9\n- Complete security audit before public launch\n- Onboard first 3 external integrators","merged_prs":[{"number":1701,"title":"fix: sequencer config for mainnet","url":"https://github.com/acme/bridge/pull/1701"}],"open_issues":[{"number":1688,"title":"Deploy bridge contract to mainnet","state":"open","url":"https://github.com/acme/bridge/issues/1688"},{"number":1689,"title":"Configure sequencer and solver for mainnet","state":"open","url":"https://github.com/acme/bridge/issues/1689"},{"number":1676,"title":"Deposit bug: funds stuck on L1","state":"open","url":"https://github.com/acme/bridge/issues/1676"}],"sprint_meta":{"start_date":"2026-03-09","sprint_number":1,"total_sprints":4},"sprint_plan_text":"# Sprint 6 (Week 1 of 4)\nGoals:\n- Fix Sprint 5 bugs and stabilize testnet\n- Start mainnet migration (deploy contracts, configure solver/sequencer)\n- Lock designs for escrow, account pool, and fees\n- Kick off whitepaper, marketing, compliance, and ops workstreams","sprint_timing":{"available":true,"current_sprint_week":1,"days_elapsed_in_plan":1,"days_into_sprint_week":1,"plan_start_date":"2026-03-09","sprint_week_progress_pct":25},"today":"2026-03-10"}</inputs>
```

Expected response shape:

```json
{
  "business_goals": [
    {"text": "Ship mainnet bridge by end of Sprint 9", "status": "on_track", "note": "Day 1 of sprint. Mainnet config PR merged; contract deploy issues are open and ready to start."}
  ],
  "sprint_goals": [
    {"text": "Fix Sprint 5 bugs and stabilize testnet", "status": "likely_done", "note": "Testnet issues closed. Deposit bug #1676 open but tracked for Sprint 6."},
    {"text": "Start mainnet migration", "status": "unclear", "note": "Issues created and ready; no work started yet. Expected given day 1 of sprint."},
    {"text": "Lock designs for escrow, account pool, and fees", "status": "unclear", "note": "Design issues open. Too early to assess."},
    {"text": "Kick off non-engineering workstreams", "status": "unclear", "note": "Issues created. Day 1 — no progress expected yet."}
  ],
  "sprint_forecast": "Day 1 of Sprint 6. All workstreams have issues created. Testnet is stable. Execution risk is the engineering scope across 4 sprints.",
  "concerns": [
    {"key": "deposit_bug_open", "summary": "Deposit bug #1676 unresolved", "explanation": "Open issue tracking funds stuck on L1. Needs resolution before mainnet launch.", "severity": "high", "scope": "strategic"}
  ]
}
```
