# Implementation Plan — Product Status Dashboard

Phases build sequentially. Each phase produces working, testable code before the next begins.

---

## Phase 1 — Project Scaffolding

**Goal:** Runnable repo with config loading, .env loading, and a "hello world" server and TUI binary.

### Steps

1. **Initialize module**
   - `go mod init github.com/your-org/dashboard`
   - Create directory tree per technical-spec §2:
     ```
     cmd/server/main.go
     cmd/tui/main.go
     internal/api/
     internal/auth/
     internal/config/
     internal/connector/github/
     internal/connector/notion/
     internal/connector/grafana/
     internal/connector/posthog/
     internal/connector/signoz/
     internal/ai/
     internal/pipeline/
     internal/store/
     internal/sync/
     internal/tui/
     ```

2. **Config loading** (`internal/config/config.go`)
   - Define `Config` struct matching `config.yaml` shape:
     ```go
     type Config struct {
         Server  ServerConfig
         Storage StorageConfig
         Auth    AuthConfig
         AI      AIConfig
     }
     ```
   - `Load(path string) (*Config, error)` using `gopkg.in/yaml.v3`.
   - `config.example.yaml` with all fields documented and placeholder values.

3. **Environment loading**
   - On server startup, load `.env` using `os.ReadFile` + manual key=value parse (no library needed).
   - Expose typed accessors: `GitHubToken()`, `NotionToken()`, `GrafanaToken()`, etc.
   - All credential reads go through this; no direct `os.Getenv` scattered in connectors.

4. **Server binary** (`cmd/server/main.go`)
   - Parse `--config` flag (default `./config.yaml`).
   - Load config + .env.
   - Print loaded config summary (masked secrets) and exit cleanly.

5. **TUI binary** (`cmd/tui/main.go`)
   - Parse `--server` flag (default `http://localhost:8080`).
   - Print server address and exit cleanly.

6. **Add dependencies**
   ```
   go get github.com/go-chi/chi/v5
   go get github.com/charmbracelet/bubbletea
   go get github.com/charmbracelet/lipgloss
   go get modernc.org/sqlite
   go get github.com/golang-migrate/migrate/v4
   go get github.com/golang-jwt/jwt/v5
   go get golang.org/x/crypto
   go get gopkg.in/yaml.v3
   go get github.com/google/go-github/v60
   go get github.com/jomei/notionapi
   ```

**Deliverable:** `go build ./...` succeeds. Both binaries run and exit cleanly.

---

## Phase 2 — Database Layer

**Goal:** SQLite database with all tables, migrations, and typed query functions.

### Steps

1. **Migration infrastructure** (`internal/store/migrations/`)
   - Embed SQL files using `//go:embed`.
   - Wire `golang-migrate` with the `modernc.org/sqlite` driver.
   - `store.Migrate(db *sql.DB) error` applies all pending migrations at startup.

2. **Migration files** (numbered `0001_` … `0007_`)
   - `0001_users.sql` — `users` table.
   - `0002_teams.sql` — `teams` + `team_members`.
   - `0003_source_catalogue.sql` — `source_catalogue`.
   - `0004_source_configs.sql` — `source_configs`.
   - `0005_annotations.sql` — `annotations`.
   - `0006_ai_cache.sql` — `ai_cache`.
   - `0007_sync_runs_sprint_meta.sql` — `sync_runs` + `sprint_meta`.
   - Exact schemas from technical-spec §5, including all CHECK constraints and UNIQUE constraints.

3. **Store struct** (`internal/store/store.go`)
   - `Store` wraps `*sql.DB`.
   - `New(path string) (*Store, error)` — opens DB, enables WAL mode and foreign keys, runs migrations.

4. **Query methods** — one file per domain, all on `*Store`:
   - `users.go`: `CreateUser`, `GetUserByUsername`, `UpdateUser`, `DeleteUser`, `ListUsers`.
   - `teams.go`: `CreateTeam`, `UpdateTeam`, `DeleteTeam`, `ListTeams`, `AddMember`, `UpdateMember`, `DeleteMember`, `GetTeamMembers`.
   - `catalogue.go`: `UpsertCatalogueItem`, `ListCatalogue`, `GetCatalogueItem`, `UpdateCatalogueStatus`.
   - `source_configs.go`: `UpsertSourceConfig`, `ListSourceConfigs`, `DeleteSourceConfig`, `GetSourceConfigsForScope`.
   - `annotations.go`: `CreateAnnotation`, `UpdateAnnotation`, `DeleteAnnotation`, `ListAnnotations`, `ArchiveItemAnnotationsForPlan`.
   - `ai_cache.go`: `GetCacheEntry`, `SetCacheEntry`, `PruneStaleCache` (delete entries older than 30 days).
   - `sync_runs.go`: `CreateSyncRun`, `UpdateSyncRun`, `GetSyncRun`.
   - `sprint_meta.go`: `UpsertSprintMeta`, `GetSprintMeta`.

5. **Model types** (`internal/store/models.go`)
   - One Go struct per table matching DB columns exactly.
   - No ORM magic — plain `sql.Scan` into structs.

**Deliverable:** `go test ./internal/store/...` with an in-memory SQLite DB passes basic CRUD round-trips for each table.

---

## Phase 3 — Auth System

**Goal:** JWT issuance and validation, bcrypt passwords, role enforcement middleware.

### Steps

1. **Password utilities** (`internal/auth/password.go`)
   - `HashPassword(plain string) (string, error)` — bcrypt cost 12.
   - `CheckPassword(hash, plain string) bool`.
   - `cmd/server hash-password` subcommand: reads password from stdin, prints bcrypt hash. Used once to set `admin_password_hash` in config.

2. **JWT** (`internal/auth/jwt.go`)
   - Claims struct: `{ sub (username), role, exp }`.
   - `IssueToken(username, role, secret string) (accessToken string, err error)` — 1-hour expiry.
   - `IssueRefreshToken() (token string, hash string, err error)` — random 32-byte token, returns raw token + SHA-256 hash for DB storage.
   - `ValidateToken(tokenStr, secret string) (*Claims, error)`.

3. **Refresh token storage** — add `refresh_tokens` table in a new migration:
   ```sql
   CREATE TABLE refresh_tokens (
       id         INTEGER PRIMARY KEY,
       user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
       token_hash TEXT    NOT NULL UNIQUE,
       expires_at INTEGER NOT NULL,
       created_at INTEGER NOT NULL
   );
   ```
   - `store.go` methods: `CreateRefreshToken`, `GetRefreshTokenByHash`, `DeleteRefreshToken`, `DeleteExpiredRefreshTokens`.

4. **Auth middleware** (`internal/api/middleware.go`)
   - `RequireAuth(secret string)` — validates Bearer token, sets `username` and `role` in request context.
   - `RequireRole(role string)` — checks context role, returns 403 if insufficient.

5. **Bootstrap** (`internal/auth/bootstrap.go`)
   - `Bootstrap(store, config) error` — called at server startup.
   - If `users` table is empty: hash `config.Auth.AdminPasswordHash` (it's already a hash from the helper), insert admin user with `role='edit'`.
   - Actually: `AdminPasswordHash` is already bcrypt — store it directly. No re-hashing.

**Deliverable:** Unit tests for JWT round-trip, password hash/check, refresh token flow.

---

## Phase 4 — Backend API Skeleton

**Goal:** HTTP server with auth endpoints, chi router, all route placeholders returning 501.

### Steps

1. **Router** (`internal/api/router.go`)
   - `NewRouter(deps *Deps) http.Handler` — `Deps` carries store, config, sync engine, connectors, AI pipeline runner.
   - Mount all routes from technical-spec §6 using chi groups.
   - Public routes: `POST /auth/login`, `POST /auth/refresh`.
   - Protected routes: all others under `RequireAuth` middleware.
   - Edit-only routes: mutation endpoints wrapped with `RequireRole("edit")`.

2. **Auth handlers** (`internal/api/auth.go`)
   - `POST /auth/login` — look up user by username, check password, issue access + refresh tokens, return both.
   - `POST /auth/refresh` — validate refresh token hash, issue new access token, rotate refresh token.

3. **Stub handlers** — one file per route group:
   - `api/org.go` — `GET /org/overview` → 501.
   - `api/teams.go` — all `/teams/*` routes → 501.
   - `api/config_teams.go` — all `/config/teams/*` routes → 501.
   - `api/config_sources.go` — all `/config/sources/*` routes → 501.
   - `api/config_annotations.go` — all `/config/annotations/*` routes → 501.
   - `api/config_users.go` — all `/config/users/*` routes → 501.
   - `api/sync.go` — `POST /sync`, `GET /sync/:run_id` → 501.
   - `api/annotations.go` — inline annotation endpoints → 501.

4. **Server startup** (`cmd/server/main.go`)
   - Open store, run migrations, run bootstrap.
   - Wire `Deps`, create router.
   - `http.ListenAndServe(":PORT", router)`.

5. **JSON helpers** (`internal/api/response.go`)
   - `writeJSON(w, status, v)` — marshal + set Content-Type.
   - `writeError(w, status, message)`.
   - `readJSON(r, &v) error` — decode with size limit.

**Deliverable:** Server starts, `POST /auth/login` with bootstrap credentials returns a JWT, all other routes return 501.

---

## Phase 5 — Source Connectors

**Goal:** Each connector implements a consistent interface; all can discover and incrementally fetch their data.

### Connector interface (`internal/connector/connector.go`)

```go
type DiscoveredItem struct {
    SourceType string
    ExternalID string
    Title      string
    URL        string
    SourceMeta map[string]any // serialized to JSON in DB
    Excerpt    string         // short content sample for AI suggestion
}

type Discoverer interface {
    Discover(ctx context.Context, target string) ([]DiscoveredItem, error)
}
```

Each connector also has typed fetch methods (not a shared interface — they return domain-specific types used by pipelines).

### GitHub Connector (`internal/connector/github/`)

1. **Client setup**
   - `New(token string) *Client` using `google/go-github` with a token-authenticated transport.
   - GraphQL client for Projects API: plain `net/http` with `Authorization: Bearer` header hitting `https://api.github.com/graphql`.

2. **Discovery** (`discover.go`)
   - `Discover(ctx, target string) ([]DiscoveredItem, error)` — target is `"owner/repo"`.
   - Enumerate: labels (`GET /repos/{owner}/{repo}/labels`), GitHub Projects (`GraphQL ProjectV2`), `.md` files in root and `docs/` (`GET /repos/{owner}/{repo}/git/trees/HEAD?recursive=1` filtered to `.md`).
   - Return one `DiscoveredItem` per label, project, and `.md` file.

3. **Incremental fetch** (`fetch.go`)
   - `FetchIssues(ctx, owner, repo, label string, since time.Time) ([]*github.Issue, error)` — search API with `updated:>since`.
   - `FetchDraftIssues(ctx, owner, repo, projectID, teamAreaValue string) ([]DraftIssue, error)` — GraphQL `ProjectV2Items` filtered by `Team/Area` field.
   - `FetchMergedPRs(ctx, owner, repo string, since time.Time) ([]*github.PullRequest, error)`.
   - `FetchCommits(ctx, owner, repo string, since, until time.Time, authors []string) ([]*github.RepositoryCommit, error)`.
   - `FetchMarkdownFile(ctx, owner, repo, path string) (content string, sha string, err error)`.

4. **Auto-tagger** (`autotag.go`)
   - `AutoTagIssues(ctx, owner, repo, projectID string, teamLabelMap map[string]string) error`.
   - GraphQL page through all project items. For each item with `Team/Area` set but missing the team label on the linked issue: apply label via REST.
   - Called from a separate endpoint or server-side ticker — not in sync path.

### Notion Connector (`internal/connector/notion/`)

1. **Client setup** — `New(token string) *Client` wrapping `jomei/notionapi`.

2. **Discovery** (`discover.go`)
   - `Discover(ctx, workspaceHint string) ([]DiscoveredItem, error)`.
   - `POST /search` with empty query to enumerate all accessible pages and databases.
   - Return one item per page (type `notion_page`) and database (type `notion_db`), with title and URL.

3. **Incremental fetch** (`fetch.go`)
   - `FetchPage(ctx, pageID string) (content string, lastEditedTime time.Time, err error)`.
     - Retrieve page blocks recursively, convert to plain text (headings → `# text`, bullets → `- text`, paragraphs → text, etc.).
   - `FetchDatabase(ctx, dbID string, updatedAfter time.Time) ([]DatabaseRow, error)`.
     - Query database with `filter: {timestamp: last_edited_time, after: updatedAfter}`.
     - Each row: title property + linked page content (fetched recursively).

### Grafana Connector (`internal/connector/grafana/`)

1. **Client setup** — `New(baseURL, token string) *Client`.
2. **Discovery** — given a dashboard URL, extract UID from path, call `GET /api/dashboards/uid/:uid`, return panel list as `DiscoveredItem` per panel.
3. **Fetch** — `FetchPanel(ctx, dashboardUID, panelID string, from, to time.Time) (PanelData, error)` using `/api/ds/query` with panel's datasource config.

### PostHog Connector (`internal/connector/posthog/`)

1. **Client setup** — `New(host, apiKey string) *Client`.
2. **Discovery** — `GET /api/projects/:project_id/dashboards/` list tiles per dashboard.
3. **Fetch** — `FetchInsight(ctx, projectID, insightID string) (InsightResult, error)`.

### SigNoz Connector (`internal/connector/signoz/`)

1. **Client setup** — `New(baseURL, apiKey string) *Client`.
2. **Discovery** — enumerate dashboards via SigNoz dashboard list API.
3. **Fetch** — fetch panel query results for configured panels.

**Deliverable:** Each connector compiles. Discovery functions return plausible stub data against real APIs (manual integration test, not automated).

---

## Phase 6 — AI Layer

**Goal:** `Generator` interface with both providers, plus the caching wrapper.

### Generator interface (`internal/ai/generator.go`)

```go
type Generator interface {
    Generate(ctx context.Context, prompt string) (string, error)
}
```

### Anthropic Provider (`internal/ai/anthropic.go`)

- `AnthropicProvider` with `apiKey`, `model` fields.
- `Generate` — POST to `https://api.anthropic.com/v1/messages` with `model`, `max_tokens: 4096`, single `user` message containing the prompt.
- Parse response JSON, return `content[0].text`.
- Respect `ANTHROPIC_API_KEY` env var override.

### ClaudeCode Provider (`internal/ai/claude_code.go`)

- Copy pattern from `~/src/engram/internal/consolidate/claude_code_client.go`.
- `ClaudeCodeProvider` with `binaryPath`, `model` fields.
- `Generate(ctx, prompt string) (string, error)`:
  - Run: `claude --print --dangerously-skip-permissions --output-format stream-json [--model MODEL] PROMPT`
  - Stream stdout line by line, parse each line as JSON.
  - Collect text from events where type is `result` or `content_block_delta`.
  - Return concatenated text.
  - Return stderr content as error on non-zero exit.

### Provider factory (`internal/ai/factory.go`)

- `New(cfg config.AIConfig) (Generator, error)` — returns the correct provider based on `cfg.Provider`.

### Caching wrapper (`internal/ai/cached_generator.go`)

- `CachedGenerator` wraps a `Generator` and a `*store.Store`.
- `Generate(ctx, pipeline, teamID, inputs map[string]any, annotations []store.Annotation) (string, error)`:
  1. Serialize `inputs + annotations` to canonical JSON (sorted keys).
  2. `SHA-256` of the canonical JSON → `inputHash`.
  3. Check `store.GetCacheEntry(inputHash, pipeline, teamID)` — return stored output on hit.
  4. Build prompt from pipeline template + serialized inputs.
  5. Call inner `Generator.Generate(ctx, prompt)`.
  6. Store result via `store.SetCacheEntry(...)`.
  7. Return result.

**Deliverable:** Unit tests with a mock `Generator`. Cache hit/miss behavior verified.

---

## Phase 7 — AI Pipelines

**Goal:** All seven pipelines implemented with prompts, input collection, and typed output parsing.

Each pipeline lives in `internal/pipeline/<name>.go`. All return strongly typed Go structs (unmarshalled from Claude's JSON response).

### Shared prompt pattern

```
You are analyzing a software team's status data. Respond ONLY with valid JSON matching this schema:
<schema>
...
</schema>

<optional: annotations block>
The user has provided the following context. Take it fully into account. Do not re-flag a concern that the user has already addressed with an explanation, unless you have new evidence that contradicts the user's context.
<annotations>
...
</annotations>

<inputs>
...
</inputs>
```

### `sprint_parse` (`pipeline/sprint_parse.go`)

- **Inputs:** Sprint plan document text (from Notion page).
- **Output schema:**
  ```json
  {
    "start_date": "2024-01-15",
    "total_sprints": 4,
    "current_sprint": 2,
    "goals": ["..."]
  }
  ```
- After successful run: `store.UpsertSprintMeta(...)` with parsed values.
- If `start_date` is null in output → leave `sprint_meta.start_date` NULL → dashboard shows warning.

### `goal_extraction` (`pipeline/goal_extraction.go`)

- **Inputs:** Goals document text + sprint plan text.
- **Output:**
  ```json
  {
    "goals": [
      { "text": "...", "source": "goals_doc|sprint_plan" }
    ]
  }
  ```

### `concerns` (`pipeline/concerns.go`)

- **Inputs:** Open issues JSON, merged PRs JSON, sprint plan text, extracted goals, `sprint_meta`, active annotations (both tiers) for the team.
- **Output:**
  ```json
  {
    "concerns": [
      { "key": "unique-stable-id", "summary": "...", "explanation": "...", "severity": "high|medium|low" }
    ]
  }
  ```
- `key` must be stable across re-runs for the same logical concern (AI instructed to derive key from concern subject, not position).

### `workload_estimation` (`pipeline/workload.go`)

- **Inputs:** Per-member data: open issues (title, labels, description, assignee), open PRs (title, assignee), recent commits (author, message count), sprint window (start/end dates), standard sprint days (5).
- **Output:**
  ```json
  {
    "members": [
      { "name": "Alice", "estimated_days": 3.5, "label": "NORMAL" }
    ]
  }
  ```
- Label thresholds: LOW < 3, NORMAL 3–5, HIGH > 5.

### `velocity_analysis` (`pipeline/velocity.go`)

- **Inputs:** Per-sprint historical data: closed issues (title, labels, closed_at), merged PRs (title, merged_at), commit counts — for the past N sprints (N configurable, default 4).
- **Output:**
  ```json
  {
    "sprints": [
      {
        "label": "Week of 2024-01-01",
        "score": 42,
        "breakdown": { "issues": 20, "prs": 15, "commits": 7 }
      }
    ]
  }
  ```
- AI weights each item by estimated difficulty to normalize score.

### `goal_alignment` (`pipeline/alignment.go`)

- **Inputs:** Org goals text, all teams' extracted goals (keyed by team ID).
- **Output:**
  ```json
  {
    "alignments": [
      { "team_id": 1, "aligned": true, "notes": "..." }
    ],
    "flags": ["Team B's work does not appear to contribute to any org goal"]
  }
  ```

### `discovery_suggestion` (`pipeline/discovery.go`)

- **Inputs:** Item title + content excerpt (first 500 chars of content).
- **Output:**
  ```json
  {
    "suggested_purpose": "current_plan|next_plan|goals|metrics_panel|org_goals|org_milestones|unknown",
    "confidence": "high|medium|low",
    "reasoning": "..."
  }
  ```
- Called during discovery pass for each new `source_catalogue` item.

### Runner (`internal/pipeline/runner.go`)

- `Runner` struct holds `*ai.CachedGenerator` and `*store.Store`.
- One method per pipeline, signature: `RunSprintParse(ctx, teamID, sourceContent string) (*SprintParseResult, error)` etc.
- Each method: gathers active annotations for scope, calls `CachedGenerator.Generate`, unmarshals JSON into typed result struct.

**Deliverable:** Each pipeline function compiles with real prompt. Integration test with `ClaudeCodeProvider` for one pipeline (e.g., `sprint_parse`) against a small sample document.

---

## Phase 8 — Sync Engine

**Goal:** Discovery pass and incremental sync orchestrated correctly, async with status tracking.

### `internal/sync/engine.go`

```go
type Engine struct {
    store      *store.Store
    github     *github.Client
    notion     *notion.Client
    grafana    *grafana.Client
    posthog    *posthog.Client
    signoz     *signoz.Client
    pipeline   *pipeline.Runner
}
```

### Discovery Pass (`sync/discover.go`)

`Discover(ctx, scope, target string) (syncRunID int64, err error)`:
1. Insert `sync_runs` row with `status='running'`.
2. Return `syncRunID` immediately (caller is async goroutine).
3. Call the appropriate connector's `Discover(ctx, target)`.
4. For each `DiscoveredItem`:
   - `store.UpsertCatalogueItem(...)` — update metadata if exists; if new: insert with `status='untagged'`.
   - For new items: call `pipeline.RunDiscoverySuggestion(ctx, item.Title, item.Excerpt)` → set `ai_suggested_purpose`.
5. On completion: `store.UpdateSyncRun(id, 'completed', nil)`.
6. On error: `store.UpdateSyncRun(id, 'failed', err.Error())`.

### Incremental Sync (`sync/sync.go`)

`Sync(ctx, scope string, teamID *int64) (syncRunID int64, err error)`:
1. Insert `sync_runs` row with `status='running'`.
2. Return `syncRunID`; continue asynchronously.
3. Load `source_configs` for the scope.
4. For each configured source, fetch incrementally using the connector (using `source_catalogue.updated_at` as the `since` timestamp).
5. Update `source_catalogue.updated_at` on successful fetch.
6. Determine which pipelines are affected by changed inputs:
   - Any source change → re-run all pipelines for that team.
   - Annotation change (detected by comparing annotation hashes) → re-run all pipelines for the affected team.
7. For each affected pipeline: call `pipeline.Runner` method (cache handles dedup via hash).
8. If scope is `'org'`: also run `goal_alignment` with all teams' extracted goals + org goals.
9. `store.UpdateSyncRun(id, 'completed', nil)` or `'failed'`.

### Async goroutine management

- Discovery and sync both launch a goroutine and return the `sync_run_id`.
- TUI polls `GET /sync/:run_id` every 2 seconds.
- Only one sync per team can run simultaneously — check for existing `running` sync run for the same scope before starting; return 409 if busy.

**Deliverable:** `POST /sync` starts an async job, TUI can poll its status, and sync correctly skips cached AI outputs when inputs haven't changed.

---

## Phase 9 — REST API Implementation

**Goal:** All endpoints fully implemented (replacing 501 stubs).

### Auth (`api/auth.go`) — already done in Phase 4.

### Org Overview (`api/org.go`)

`GET /org/overview`:
- Load all teams.
- For each team: load latest sprint_meta, concerns (from cache), workload summary.
- Load org goal alignment output (from cache).
- Load per-member cross-team workload aggregate.
- Return combined JSON.

### Teams (`api/teams.go`)

- `GET /teams` — list teams with members from store.
- `GET /teams/:id/sprint` — sprint_meta + sprint week calculation (`days_elapsed / 7 + 1`), start_date warning if NULL.
- `GET /teams/:id/goals` — goal_extraction + concerns pipeline outputs from cache, with annotation refs.
- `GET /teams/:id/workload` — workload_estimation output from cache, per-member.
- `GET /teams/:id/velocity` — velocity_analysis output from cache.
- `GET /teams/:id/metrics` — load configured metrics panels, return panel data from cache.

### Config — Teams (`api/config_teams.go`)

Standard CRUD delegating to store methods. No business logic beyond validation.

### Config — Sources (`api/config_sources.go`)

- `GET /config/sources` — return full source catalogue with status and suggested purpose.
- `POST /config/sources/discover` — validate `scope` + `target`, call `engine.Discover(ctx, scope, target)` async, return `{sync_run_id}`.
- `PUT /config/sources/:id` — update `status`, `purpose`, `team_id`, `config_meta`. If `purpose='current_plan'` and item is a Notion page: trigger `sprint_parse` pipeline (or enqueue for next sync).

### Config — Annotations (`api/config_annotations.go`)

- `GET /config/annotations` — list all annotations grouped by tier (`item` vs `team`), include archived status.
- `POST /config/annotations` — create with tier, team_id, item_ref, content. Validate tier.
- `PUT /config/annotations/:id` — update content, bump `updated_at`.
- `DELETE /config/annotations/:id` — hard delete.

### Config — Users (`api/config_users.go`)

- All CRUD. `POST` hashes password before storing. Require edit role.

### Sync (`api/sync.go`)

- `POST /sync` — validate scope/team_id, call `engine.Sync(ctx, scope, teamID)` async, return `{sync_run_id}`.
- `GET /sync/:run_id` — return sync run row.

### Annotations (inline) (`api/annotations.go`)

- Same logic as `config_annotations.go` — same underlying store methods.
- Separate mount point (`/annotations`) for ergonomic TUI calls.

### Sprint week endpoint — included in `GET /teams/:id/sprint` response:
```json
{
  "plan_type": "current",
  "start_date": "2024-01-15",
  "current_sprint": 2,
  "total_sprints": 4,
  "start_date_missing": false,
  "next_plan_start_risk": false,
  "goals": [...]
}
```

**Deliverable:** All API endpoints return real data. Manual curl tests pass for each.

---

## Phase 10 — Plan Rollover Logic

**Goal:** When user re-tags a source as new current plan, item-tier annotations for the old plan are archived.

### Implementation

1. `PUT /config/sources/:id` with `purpose='current_plan'` triggers rollover logic:
   - Look up the previous `source_configs` row for `(team_id, purpose='current_plan')`.
   - If it differs from the new one → old plan is being replaced.
   - Call `store.ArchiveItemAnnotationsForPlan(teamID)` → sets `archived=1` for all `item`-tier annotations for this team.
   - Replace sprint_meta `plan_type='current'` with new document's data (trigger `sprint_parse`).
   - Update `source_configs` to point to new document.

2. `store.ArchiveItemAnnotationsForPlan(teamID int64)`:
   ```sql
   UPDATE annotations SET archived=1, updated_at=?
   WHERE team_id=? AND tier='item' AND archived=0
   ```

3. Archived annotations remain visible in Config → Annotations (clearly labeled "Archived") but are not included in pipeline inputs.

4. `team`-tier annotations are untouched by rollover.

**Deliverable:** Rollover correctly archives item annotations and leaves team annotations intact, verified with a unit test.

---

## Phase 11 — TUI

**Goal:** Full Bubble Tea TUI with drill-down navigation, all views, sync banner, and annotation panel.

### Architecture (`internal/tui/`)

**View stack model** (`tui/app.go`):
```go
type App struct {
    views []tea.Model    // stack; views[len-1] is active
    client *api.Client   // HTTP client for backend
    syncPoll *SyncPoller
}
```
- `Enter` pushes a new view onto the stack.
- `Esc`/`Backspace` pops the stack.
- Top-level `q` quits only when stack depth is 1.

**HTTP client** (`tui/client/client.go`):
- Typed methods for every API endpoint: `GetTeams()`, `GetSprint(teamID)`, `PostAnnotation(...)`, `PostSync(scope, teamID)` etc.
- Reads JWT from a local token file (`~/.dashboard/token`), refreshes automatically on 401.
- Login screen presented if no valid token exists.

### Views

#### Login View (`tui/views/login.go`)
- Username + password text inputs.
- On submit: `POST /auth/login`, store token + refresh token to `~/.dashboard/token`.

#### Org Overview (`tui/views/org_overview.go`)
- List of team cards: `[Team Name] Sprint N/M | Risk: HIGH | Focus: ...`
- One card per team, navigable with `j`/`k`.
- Press `Enter` on a card → push Team View.
- Bottom row: org-level metrics summary.
- `Last synced: <timestamp>` footer.
- `R` → trigger full org sync.

#### Team View (`tui/views/team.go`)
- Sub-menu: Sprint & Plan Status | Goals & Concerns | Resource/Workload | Velocity | Business Metrics.
- `j`/`k` to navigate, `Enter` to drill in.
- `r` → trigger team sync.

#### Sprint & Plan Status View (`tui/views/sprint.go`)
- Shows: `Week N of M` (or warning if no start date).
- Plan goals list.
- If M > 4 and next plan configured: show red warning about next plan delay.

#### Goals & Concerns View (`tui/views/goals.go`)
- Two sections: Goals and Concerns, separated by a divider.
- Each concern shows: `[HIGH] summary` with `explanation` below.
- Severity colors: HIGH=red, MEDIUM=yellow, LOW=gray.
- Press `a` on any item → push Annotation Panel.

#### Annotation Panel (`tui/views/annotate.go`)
- Overlay/pushed view.
- Shows item being annotated.
- Tier selector: `[Item-level] / [Team-level]` (toggle with Tab).
- Multi-line text input for annotation content.
- Submit: `Ctrl+Enter` or a dedicated keybind → `POST /annotations`.

#### Resource/Workload View (`tui/views/workload.go`)
- Per-member table: `Alice: 3.5 days [HIGH]`.
- Color-coded labels.
- Press `a` on member row → annotate (e.g., "Alice is on leave").

#### Velocity View (`tui/views/velocity.go`)
- Sparkline-style chart using unicode block characters across recent sprints.
- Below chart: tabular breakdown per sprint.

#### Business Metrics View (`tui/views/metrics.go`)
- Panel list from configured sources.
- Show panel title + latest value/summary.

#### Config View (`tui/views/config/`)

**Config Root** (`config_root.go`) — sub-menu: Teams & Members | Sources | Business Metrics | Annotations | Users.

**Teams & Members** (`config_teams.go`):
- List teams. `Enter` → expand team, show members.
- `n` → new team (text input popup). `e` → edit. `d` → delete (confirm prompt).
- On team expand: list members with `n`/`e`/`d` bindings.

**Sources** (`config_sources.go`):
- Catalogue table: `[type] title | status | purpose`.
- Filter by status (untagged / configured / ignored).
- Press `Enter` on item → tagging panel: set purpose, team, show AI suggestion.
- `D` → run discovery (text input for target, then async).
- Syncs with sync banner while discovery runs.

**Business Metrics** (`config_metrics.go`):
- Per-team: list configured panels. `a` → add panel (paste URL, fetch panel list, select).

**Annotations** (`config_annotations.go`):
- Two groups: Team-level (persists) and Item-level (may be archived).
- Show: content, team, tier, archived status.
- `e` → edit content. `d` → delete.

**Users** (`config_users.go`):
- Only visible to edit-role users.
- List users. `n` → create. `e` → edit role/password. `d` → delete.

### Sync Banner (`tui/components/banner.go`)

- Component rendered at top of every view when a sync run is active.
- `SyncPoller` goroutine polls `GET /sync/:run_id` every 2s, sends `SyncDoneMsg` or `SyncFailedMsg` to the Bubble Tea program.
- On completion: banner disappears.
- On failure: banner becomes red error with `Press Enter to dismiss`.

### Keybindings (summary, per spec)

| Key | Action |
|-----|--------|
| `j` / `↓` | Move down |
| `k` / `↑` | Move up |
| `Enter` | Drill in / select / submit |
| `Esc` / `Backspace` | Go up one level |
| `r` | Sync current team |
| `R` | Sync entire org |
| `a` | Annotate selected item |
| `d` | Delete selected annotation |
| `q` | Quit (from top level only) |

**Deliverable:** Full interactive TUI. All views render real data from the backend. Annotation flow works end-to-end.

---

## Phase 12 — Wiring & Integration

**Goal:** Full system works end-to-end with real data sources.

### Steps

1. **Integration smoke test script**
   - Shell script: start server, log in, run discovery against a test GitHub repo, tag a source, trigger sync, poll until complete, hit all read endpoints.

2. **GitHub auto-tagger wiring**
   - Add `GET /admin/autotag` endpoint (edit-only) that runs the auto-tagger for all configured repos.
   - Or: add a server-side `time.Ticker` (every 12h) in `cmd/server/main.go` that calls `engine.AutoTag(ctx)`.

3. **`hash-password` subcommand**
   - `cmd/server/main.go` checks `os.Args[1] == "hash-password"` → read from stdin, print bcrypt hash, exit.

4. **Graceful shutdown**
   - Server: `signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)`.
   - Wait for in-progress sync goroutines to finish (with timeout).

5. **Cache pruning**
   - On server startup: `store.PruneStaleCache(30 * 24 * time.Hour)` to delete old unreachable cache entries.

6. **Config → Annotations lifecycle**
   - Ensure archived annotations are visually distinct in TUI (grayed out, labeled "Archived").
   - Ensure they are excluded from pipeline inputs in `pipeline.Runner` (filter `archived=0`).

7. **`Last synced` timestamp**
   - `GET /teams/:id/*` responses include `last_synced_at` from the latest completed `sync_runs` row for that team.
   - TUI footer shows this timestamp on all team views.

---

## Phase 13 — Hardening & Edge Cases

**Goal:** Handle all edge cases described in spec gracefully.

### Edge cases to implement

1. **No start date in sprint plan**
   - `sprint_meta.start_date` is NULL → `GET /teams/:id/sprint` sets `start_date_missing: true`.
   - Sprint & Plan Status view shows: `Warning: Sprint start date not found. Add it to the plan document or annotate it in Config.`

2. **5th (or more) sprint**
   - `sprint_meta.total_sprints > 4` AND next plan is configured → `concerns` pipeline flags delay risk.
   - TUI shows `Week N of M` with M highlighted in amber if M > 4.

3. **Concurrent sync prevention**
   - `POST /sync` returns 409 with message if a sync for the same scope is already running.

4. **AI output is not valid JSON**
   - Pipeline runner: if Claude returns non-JSON or JSON that doesn't match schema, log the raw output, return a structured error.
   - TUI shows `AI processing error — try syncing again.` with a dismiss option.

5. **Source fetch errors**
   - Connector returns error → sync run still completes for other sources; error is noted per-source in `sync_runs.error` JSON.
   - TUI shows which sources failed in the sync error banner.

6. **Missing `.env` credentials**
   - Connector init: if required env var is missing, connector is disabled; discovery/sync attempts for that connector type return a clear error.

7. **Annotation stale warning (optional)**
   - In `concerns` pipeline: include a note in prompt — "If any user annotation appears to be contradicted by the current evidence, flag it with type `stale_annotation`."
   - If output contains `stale_annotation` concerns, TUI highlights them distinctly.

8. **TUI: no data yet (first run)**
   - Before first sync: show "No data. Press R to sync." placeholder in each view.

---

## Implementation Order Summary

| Phase | Effort | Dependency |
|-------|--------|------------|
| 1. Scaffolding | Small | — |
| 2. Database Layer | Medium | 1 |
| 3. Auth System | Small | 2 |
| 4. API Skeleton | Small | 3 |
| 5. Source Connectors | Large | 1 |
| 6. AI Layer | Medium | 1 |
| 7. AI Pipelines | Large | 5, 6 |
| 8. Sync Engine | Medium | 5, 7 |
| 9. REST API | Large | 2, 4, 8 |
| 10. Plan Rollover | Small | 9 |
| 11. TUI | Large | 9 |
| 12. Wiring | Small | 11 |
| 13. Hardening | Medium | 12 |

The critical path is: **1 → 2 → 3 → 4 → 5 → 6 → 7 → 8 → 9 → 11**.

Phases 10, 12, and 13 can be interleaved with Phase 11 once the API layer (Phase 9) is stable.
