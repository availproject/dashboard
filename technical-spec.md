# Product Status Dashboard — Technical Specification

## 1. System Architecture

```
┌─────────────────────────────────────────┐
│            TUI Client (Bubble Tea)      │
│            cmd/dashboard-tui/           │
│  Connects to backend via HTTP on        │
│  localhost                              │
└────────────────┬────────────────────────┘
                 │ REST API (JWT auth)
┌────────────────▼────────────────────────┐
│        Backend Server (Go)              │
│        cmd/dashboard-server/            │
│                                         │
│  ┌─────────────┐  ┌──────────────────┐  │
│  │  API Layer  │  │   Sync Engine    │  │
│  │  (net/http  │  │  (orchestrator)  │  │
│  │  + chi)     │  └────────┬─────────┘  │
│  └─────────────┘           │            │
│  ┌─────────────────────────▼─────────┐  │
│  │         Source Connectors         │  │
│  │  GitHub | Notion | PostHog |      │  │
│  │  SigNoz  | Grafana                │  │
│  └───────────────────────────────────┘  │
│  ┌───────────────────────────────────┐  │
│  │          AI Pipeline              │  │
│  │  Generator interface              │  │
│  │  providers: anthropic | claude-   │  │
│  │  code (CLI subprocess)            │  │
│  └───────────────────────────────────┘  │
│  ┌───────────────────────────────────┐  │
│  │  SQLite (modernc.org/sqlite)      │  │
│  │  config | cache | annotations |   │  │
│  │  users | sync state               │  │
│  └───────────────────────────────────┘  │
└─────────────────────────────────────────┘
```

- The TUI and backend are separate binaries in the same repo; TUI speaks HTTP to the backend on localhost.
- This separation allows a future web frontend to use the same API without changes to the backend.
- Single-tenant: one server process per org instance.
- Deployment target: local Mac (for now).

---

## 2. Repository Structure

```
dashboard/
├── cmd/
│   ├── server/          # backend binary entrypoint
│   └── tui/             # Bubble Tea TUI binary entrypoint
├── internal/
│   ├── api/             # HTTP handlers, middleware, router
│   ├── auth/            # JWT generation/validation, password hashing
│   ├── config/          # config.yaml loading (following engram pattern)
│   ├── connector/
│   │   ├── github/
│   │   ├── notion/
│   │   ├── grafana/
│   │   ├── posthog/
│   │   └── signoz/
│   ├── ai/              # Generator interface + provider implementations
│   ├── pipeline/        # AI pipelines (goals, concerns, workload, etc.)
│   ├── store/           # SQLite DB layer (migrations, queries)
│   ├── sync/            # Sync orchestration logic
│   └── tui/             # Bubble Tea models and views
├── config.yaml          # user config (gitignored if sensitive)
├── config.example.yaml
├── .env                 # source credentials (gitignored)
└── go.mod
```

---

## 3. Tech Stack

| Layer | Choice | Notes |
|---|---|---|
| Language | Go | Single binary deployment, strong stdlib |
| TUI framework | Bubble Tea + Lip Gloss | `github.com/charmbracelet/bubbletea` |
| HTTP router | chi | Lightweight, idiomatic |
| Database | SQLite via `modernc.org/sqlite` | Pure Go, no CGo |
| DB migrations | `golang-migrate/migrate` | SQL migration files |
| GitHub client | `google/go-github` | Official-ish SDK |
| Notion client | `jomei/notionapi` | Community SDK |
| JWT | `golang-jwt/jwt/v5` | |
| Password hashing | `golang.org/x/crypto/bcrypt` | |
| Config | `gopkg.in/yaml.v3` | Same as engram |
| AI | `Generator` interface (see §7) | Pluggable: anthropic or claude-code |

---

## 4. Configuration

### `config.yaml`

```yaml
server:
  port: 8080

storage:
  path: ./data/dashboard.db

auth:
  jwt_secret: "changeme"
  admin_username: "admin"
  admin_password_hash: "$2a$..."  # bcrypt hash; set on first run

ai:
  provider: "claude-code"   # "anthropic" | "claude-code"
  model: "claude-sonnet-4-6"
  api_key: ""               # only needed for provider: anthropic
  binary_path: "claude"     # only for provider: claude-code
```

- `provider: "anthropic"` — calls Anthropic REST API directly with `api_key`.
- `provider: "claude-code"` — invokes the local `claude` CLI binary (uses the user's Claude subscription, no separate API key needed). Follows the pattern established in `~/src/engram`.
- Model is passed through to whichever provider is active.
- `ANTHROPIC_API_KEY` environment variable overrides `ai.api_key` if set.

### `.env` (source credentials, never in DB)

```
GITHUB_TOKEN=ghp_...
NOTION_TOKEN=secret_...
GRAFANA_TOKEN=...
GRAFANA_BASE_URL=https://grafana.example.com
POSTHOG_API_KEY=phc_...
POSTHOG_HOST=https://app.posthog.com
SIGNOZ_API_KEY=...
SIGNOZ_BASE_URL=https://signoz.example.com
```

---

## 5. Database Schema

All tables use integer primary keys. Timestamps are stored as Unix seconds (int64).

### `users`
```sql
CREATE TABLE users (
    id           INTEGER PRIMARY KEY,
    username     TEXT    NOT NULL UNIQUE,
    password_hash TEXT   NOT NULL,
    role         TEXT    NOT NULL CHECK(role IN ('view','edit')),
    created_at   INTEGER NOT NULL
);
```

### `teams`
```sql
CREATE TABLE teams (
    id         INTEGER PRIMARY KEY,
    name       TEXT    NOT NULL,
    created_at INTEGER NOT NULL
);
```

### `team_members`
```sql
CREATE TABLE team_members (
    id              INTEGER PRIMARY KEY,
    team_id         INTEGER NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    display_name    TEXT    NOT NULL,
    github_username TEXT,
    notion_user_id  TEXT,
    created_at      INTEGER NOT NULL
);
```

### `source_catalogue`
Discovered items from connected sources. One row per discoverable item.

```sql
CREATE TABLE source_catalogue (
    id                   INTEGER PRIMARY KEY,
    source_type          TEXT    NOT NULL, -- 'github_repo' | 'notion_page' | 'notion_db' | 'grafana_panel' | 'posthog_panel' | 'signoz_panel'
    external_id          TEXT    NOT NULL, -- Notion page ID, GitHub "owner/repo", panel ID, etc.
    title                TEXT    NOT NULL,
    url                  TEXT,
    source_meta          TEXT,             -- JSON: workspace, repo, dashboard URL, etc.
    ai_suggested_purpose TEXT,             -- filled during discovery
    status               TEXT    NOT NULL DEFAULT 'untagged', -- 'untagged' | 'configured' | 'ignored'
    created_at           INTEGER NOT NULL,
    updated_at           INTEGER NOT NULL,
    UNIQUE(source_type, external_id)
);
```

### `source_configs`
User tagging decisions: maps a catalogued source to its purpose for a team (or org).

```sql
CREATE TABLE source_configs (
    id           INTEGER PRIMARY KEY,
    catalogue_id INTEGER NOT NULL REFERENCES source_catalogue(id) ON DELETE CASCADE,
    team_id      INTEGER REFERENCES teams(id) ON DELETE CASCADE, -- NULL = org-level
    purpose      TEXT    NOT NULL,
    -- purposes: 'current_plan' | 'next_plan' | 'goals' | 'metrics_panel' | 'org_goals' | 'org_milestones'
    config_meta  TEXT,   -- JSON: e.g. {"selected_panels": ["panel1","panel2"]}
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL,
    UNIQUE(catalogue_id, team_id, purpose)
);
```

### `annotations`
User-provided context and dismissals for AI-generated items.

```sql
CREATE TABLE annotations (
    id         INTEGER PRIMARY KEY,
    tier       TEXT    NOT NULL CHECK(tier IN ('item','team')),
    team_id    INTEGER REFERENCES teams(id) ON DELETE CASCADE, -- NULL = org-level
    item_ref   TEXT,   -- JSON: {"type":"concern","key":"..."} NULL for team-tier
    content    TEXT    NOT NULL,
    archived   INTEGER NOT NULL DEFAULT 0, -- 1 = auto-archived at plan rollover
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
```

### `ai_cache`
Cached AI pipeline outputs, keyed by a hash of inputs + active annotations.

```sql
CREATE TABLE ai_cache (
    id         INTEGER PRIMARY KEY,
    input_hash TEXT    NOT NULL, -- SHA-256 of canonical(inputs + active_annotations)
    pipeline   TEXT    NOT NULL, -- 'sprint_parse' | 'concerns' | 'goal_extraction' | 'workload' | 'velocity' | 'alignment' | 'discovery_suggestion'
    team_id    INTEGER REFERENCES teams(id) ON DELETE CASCADE, -- NULL = org-level
    output     TEXT    NOT NULL, -- JSON
    created_at INTEGER NOT NULL,
    UNIQUE(input_hash, pipeline, team_id)
);
```

### `sync_runs`
```sql
CREATE TABLE sync_runs (
    id           INTEGER PRIMARY KEY,
    scope        TEXT    NOT NULL CHECK(scope IN ('team','org')),
    team_id      INTEGER REFERENCES teams(id) ON DELETE SET NULL,
    status       TEXT    NOT NULL CHECK(status IN ('running','completed','failed')),
    started_at   INTEGER NOT NULL,
    completed_at INTEGER,
    error        TEXT
);
```

### `sprint_meta`
Parsed sprint metadata per team, updated by the `sprint_parse` pipeline.

```sql
CREATE TABLE sprint_meta (
    id                 INTEGER PRIMARY KEY,
    team_id            INTEGER NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    plan_type          TEXT    NOT NULL CHECK(plan_type IN ('current','next')),
    start_date         TEXT,   -- ISO date, NULL if not found (triggers warning)
    total_sprints      INTEGER,
    current_sprint     INTEGER,
    source_catalogue_id INTEGER REFERENCES source_catalogue(id),
    detected_at        INTEGER NOT NULL,
    UNIQUE(team_id, plan_type)
);
```

---

## 6. REST API

All endpoints except `/auth/login` and `/auth/refresh` require a valid `Authorization: Bearer <token>` header.

Edit-role is required for all mutation endpoints (POST/PUT/DELETE) and for `/sync`, `/discover`, and user management.

### Auth
```
POST /auth/login           { "username": "...", "password": "..." }
                           → { "token": "...", "refresh_token": "..." }
POST /auth/refresh         { "refresh_token": "..." } → { "token": "..." }
```

### Org
```
GET  /org/overview         → { teams: [...summary], workload: [...], goal_alignment: {...}, metrics: [...] }
```

### Teams
```
GET  /teams                → [{ id, name, members: [...] }]
GET  /teams/:id/sprint     → sprint & plan status
GET  /teams/:id/goals      → AI-extracted goals & concerns (with annotation refs)
GET  /teams/:id/workload   → per-member estimated work-days + label
GET  /teams/:id/velocity   → per-sprint velocity trend
GET  /teams/:id/metrics    → configured business metrics panels
```

### Config — Teams
```
POST   /config/teams               { name }
PUT    /config/teams/:id           { name }
DELETE /config/teams/:id
POST   /config/teams/:id/members   { display_name, github_username, notion_user_id }
PUT    /config/members/:id
DELETE /config/members/:id
```

### Config — Sources
```
GET  /config/sources               → source catalogue with status
POST /config/sources/discover      { "scope": "notion_workspace"|"github_repo"|"metrics_url", "target": "..." }
                                   → { "sync_run_id": ... }
PUT  /config/sources/:id           { status, purpose, team_id, config_meta }
```

### Config — Annotations
```
GET    /config/annotations         → all annotations grouped by tier
POST   /config/annotations         { tier, team_id, item_ref, content }
PUT    /config/annotations/:id     { content }
DELETE /config/annotations/:id
```

### Config — Users (edit-role only)
```
GET    /config/users
POST   /config/users               { username, password, role }
PUT    /config/users/:id           { password?, role? }
DELETE /config/users/:id
```

### Sync
```
POST /sync                         { "scope": "team"|"org", "team_id": 1 }
                                   → { "sync_run_id": 42 }
GET  /sync/:run_id                 → { id, status, started_at, completed_at, error }
```

### Annotations (inline from dashboard)
```
POST   /annotations                { tier, team_id, item_ref, content }
PUT    /annotations/:id            { content }
DELETE /annotations/:id
```

---

## 7. AI Layer

### Generator Interface

```go
// Generator is the single interface all AI providers implement.
type Generator interface {
    Generate(ctx context.Context, prompt string) (string, error)
}
```

### Providers

**`AnthropicProvider`** — calls the Anthropic REST API directly using `api_key` from config.

**`ClaudeCodeProvider`** — invokes the `claude` CLI binary as a subprocess (identical pattern to `~/src/engram/internal/consolidate/claude_code_client.go`):
- Runs: `claude --print --dangerously-skip-permissions --output-format stream-json [--model MODEL] PROMPT`
- Streams and parses `stream-json` output, collecting text from `result` and `content_block_delta` events.
- Allows using the user's Claude subscription without a separate API key.

Provider is selected at startup based on `config.yaml ai.provider`.

### Caching

Every pipeline call:
1. Collects inputs (source content snapshots + active annotations for scope).
2. Computes `SHA-256(canonical_json(inputs))`.
3. Checks `ai_cache` for `(input_hash, pipeline, team_id)` — cache hit returns stored output immediately.
4. On miss: calls `Generator.Generate()`, stores result, returns it.

Cache is invalidated implicitly: when source content changes, the input hash changes → new cache entry. Old entries are not explicitly deleted but become unreachable (periodic cleanup can remove stale entries by age).

Annotations are included in the hash: adding or editing an annotation invalidates the cache for all pipelines that include those annotations in their input.

### Pipelines

All pipelines produce structured JSON output. Prompts instruct Claude to respond in JSON and include the schema. Where annotations exist for the scope, they are appended with:

> "The user has provided the following context. Take it fully into account. Do not re-flag a concern that the user has already addressed with an explanation, unless you have new evidence that contradicts the user's context."

| Pipeline | Primary Inputs | Output Shape |
|---|---|---|
| `sprint_parse` | Sprint plan document text | `{ start_date, total_sprints, current_sprint, goals: [...] }` |
| `goal_extraction` | Goals doc text, sprint plan text | `{ goals: [{ text, source }] }` |
| `concerns` | Issues data, PR data, sprint plan, goals, sprint_meta, annotations | `{ concerns: [{ key, summary, explanation, severity }] }` |
| `workload_estimation` | Open issues (assignee, labels, description), PRs, commits per person, sprint window | `{ members: [{ name, estimated_days, label }] }` |
| `velocity_analysis` | Closed issues + PRs + commits per sprint window (multiple sprints) | `{ sprints: [{ label, score, breakdown }] }` |
| `goal_alignment` | Team goals (all teams), org goals | `{ alignments: [{ team_id, aligned: bool, notes }], flags: [...] }` |
| `discovery_suggestion` | Item title + content excerpt | `{ suggested_purpose, confidence, reasoning }` |

---

## 8. Source Connectors

All credentials come from `.env` (loaded at startup into connector configs). No credentials stored in the DB.

### GitHub Connector
- **Auth:** `GITHUB_TOKEN` (Personal Access Token)
- **Library:** `google/go-github` (REST) + GraphQL for Projects API
- **Configuration:** One "roadmap" repo per org (configurable). Issues are tagged with a team label matching the project board's "Team/Area" field value.
- **Discovery:**
  - Enumerate labels, GitHub Projects, and `.md` files in the configured repo.
- **Incremental sync (on every sync run):**
  - **Issues:** `GET /search/issues?q=repo:{owner}/{repo}+label:{team_label}+updated:>{last_sync_time}` — fast, time-filtered, label-scoped.
  - **Draft issues:** GraphQL `ProjectV2Items` query filtered by `Team/Area` field value + `contentType: DraftIssue`. Drafts represent planned work not yet filed as issues; treated as lower-confidence signals in the workload pipeline.
  - **PRs:** `GET /repos/{owner}/{repo}/pulls?state=closed&sort=updated&direction=desc` filtered client-side by `merged_at > last_sync_time`.
  - **Commits:** `GET /repos/{owner}/{repo}/commits?since={sprint_start}&until={now}`, filtered by configured team member GitHub usernames.
  - **`.md` files:** Check `updated_at` of blob; re-fetch only if changed.
- **Auto-tagger (periodic, not on every sync):**
  - Scans the full project board via GraphQL, paging through all items.
  - For each item where the "Team/Area" field is set but the corresponding team label is absent on the linked issue: applies the label via the REST API.
  - Intended to run on a schedule (e.g. every 12–24h via cron or a server-side ticker), not in the critical sync path.
  - Ensures label-based search stays accurate without requiring perfect process discipline from team members.

### Notion Connector
- **Auth:** `NOTION_TOKEN` (Integration Token)
- **Library:** `jomei/notionapi`
- **Discovery:** `POST /search` to enumerate all pages and databases the integration can access.
- **Incremental sync:** For each configured page/database, check `last_edited_time`; fetch full content only if changed since last sync.
- **Content extraction:** Blocks are fetched recursively and converted to plain text for AI input.

### Grafana Connector
- **Auth:** `GRAFANA_TOKEN` (service account), `GRAFANA_BASE_URL`
- **Discovery:** Given a dashboard URL, extract dashboard UID, call `GET /api/dashboards/uid/:uid` to list panels.
- **Sync:** Call `GET /api/ds/query` or panel-specific data source endpoints for configured panels.

### PostHog Connector
- **Auth:** `POSTHOG_API_KEY`, `POSTHOG_HOST`
- **Discovery:** `GET /api/projects/:id/dashboards/` to list dashboards and tiles.
- **Sync:** `GET /api/projects/:id/insights/:id/` for configured insight results.

### SigNoz Connector
- **Auth:** `SIGNOZ_API_KEY`, `SIGNOZ_BASE_URL`
- **Discovery:** Enumerate dashboards via SigNoz API.
- **Sync:** Fetch panel query results for configured panels.

---

## 9. Sync Engine

### Discovery Pass

Triggered by `POST /config/sources/discover`. Creates a sync run with a discovery scope.

1. Connector enumerates available items for the given target.
2. For each item:
   - If already in `source_catalogue` (matched by `source_type + external_id`): update metadata, leave `status` and `purpose` intact.
   - If new: insert with `status = 'untagged'`, run `discovery_suggestion` pipeline → set `ai_suggested_purpose`.
3. New items surface in the Config → Sources section of the TUI for user review.
4. Previously ignored or configured items are not reset.

### Incremental Sync

Triggered by `POST /sync`. Creates a sync run record immediately (returns `sync_run_id`) and runs asynchronously.

1. Collect all `source_configs` for the given scope (team or org).
2. For each configured source, call the relevant connector's incremental fetch (using last sync timestamp stored in `source_catalogue.updated_at`).
3. For each pipeline whose inputs might have changed:
   a. Re-collect inputs (fetched content + active annotations).
   b. Compute `input_hash`.
   c. If hash matches existing `ai_cache` entry → skip (use cached output).
   d. If hash differs → run pipeline, store new cache entry.
4. Update `sync_run` status to `completed` (or `failed` with error).
5. TUI polls `GET /sync/:run_id` and removes the banner on completion.

---

## 10. Annotation Lifecycle

### Tiers

| Tier | `item_ref` | Lifecycle |
|---|---|---|
| `item` | JSON ref to a specific goal/concern/task | Auto-archived (`archived=1`) at plan rollover (when user re-tags current plan doc). Visible in history in Config → Annotations but no longer sent to AI. |
| `team` | NULL | Persists until manually deleted by user. Always included as AI context for that team's pipelines. |

When user annotates inline in the TUI, they are prompted to select a tier (default: `item`).

### Plan Rollover

When the user re-tags documents (new plan becomes current):
1. All `item`-tier annotations where `item_ref` references items from the old plan are set `archived=1`.
2. `team`-tier annotations are unaffected.
3. `sprint_meta` for `plan_type='current'` is replaced with the new plan's parsed data.
4. Old `ai_cache` entries become unreachable (hash changes due to new source content).

No automated rollover detection — the user explicitly changes config tags.

---

## 11. TUI Structure (Bubble Tea)

### Navigation Model

Drill-down using a stack of views. `Enter` drills in, `Esc`/`Backspace` goes up.

```
[Org Overview]
  → [Team: Engineering]
      → [Sprint & Plan Status]
      → [Goals & Concerns]
          → [Concern detail + inline annotate]
      → [Resource / Workload]
      → [Velocity]
      → [Business Metrics]
  → [Team: Marketing]
      → ...
  → [Config]
      → [Teams & Members]
      → [Sources]          (catalogue with status, tagging UI)
      → [Business Metrics] (panel selection per team)
      → [Annotations]      (all annotations, both tiers, grouped)
      → [Users]            (edit-role only)
```

### Key Bindings

| Key | Action |
|---|---|
| `j` / `↓` | Move down |
| `k` / `↑` | Move up |
| `Enter` | Drill in / select |
| `Esc` / `Backspace` | Go up one level |
| `r` | Sync current team |
| `R` | Sync entire org |
| `a` | Annotate selected item (opens annotation panel) |
| `d` | Delete selected annotation (in Annotations config) |
| `q` | Quit (from top level) |

### Sync Banner

While any sync run is `running`, a banner is shown at the top of every view:

```
[ Sync in progress... (Engineering) ]
```

The TUI polls `GET /sync/:run_id` every 2 seconds and removes the banner on `completed` or `failed`. On failure, banner changes to an error message that requires dismissal.

### Workload Display

Team view: `Alice: 3.5 days [HIGH]`
Org view: `Alice: 5.0 days total (Eng: 3.5, Marketing: 1.5) [HIGH]`

Standard sprint = 5 work-days. Labels: `LOW` (<3), `NORMAL` (3–5), `HIGH` (>5).

### Cached State UX

- Dashboard always shows last cached state on load, with a `Last synced: <timestamp>` footer.
- No blank state after first sync; data persists across restarts.

---

## 12. Auth System

- **JWT access tokens:** 1-hour expiry, signed with `auth.jwt_secret` from config.
- **Refresh tokens:** 30-day expiry, stored as a hash in the DB.
- **Roles:** `view` (read-only), `edit` (annotate, configure, sync, manage users).
- **Bootstrap:** On first startup, if no users exist, the server creates an admin user from `auth.admin_username` + `auth.admin_password_hash` in `config.yaml`. The hash is a bcrypt hash the user generates once with a provided helper command: `dashboard-server hash-password`.
- **User management:** Admin creates/modifies users in Config → Users in the TUI. No self-registration.

---

## 13. Sprint Plan Details

### Week Detection

The `sprint_parse` pipeline extracts `start_date` from the Notion sprint plan document text. The backend computes current sprint week as:

```
days_elapsed = today - start_date
current_sprint = floor(days_elapsed / 7) + 1
```

If `start_date` is not found in the document, `sprint_meta.start_date` is NULL and the dashboard displays:

```
Warning: Sprint start date not found. Add it to the plan document or annotate it in Config.
```

### 5th Sprint Handling

`total_sprints` may exceed 4. The dashboard shows `Week N of M` where M comes from `sprint_meta.total_sprints`. If M > 4 and a next-plan document is configured, the concerns pipeline flags:

> "Current plan has extended to sprint M. This delays the next plan's projected start date."

### Task Discipline Assumption

GitHub issues are always closed at sprint end (Done / Not Completed / Won't Do). The tool does not track carry-over; it only reads currently open issues for workload estimation and closed issues (within sprint window) for velocity.

---

## 14. Out of Scope (Initial Build)

- Inter-team dependency tracking.
- Proactive notifications / alerts.
- Marketing velocity (future phase using Notion database signals).
- Web frontend (API is designed for it; just not built yet).
- Stale annotation auto-detection (nice-to-have; manual cleanup expected).
- OAuth flows for GitHub or Notion.
- Multi-tenant deployment.
