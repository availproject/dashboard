# PRD: Product Status Dashboard

## Introduction

A TUI-first, AI-assisted product status dashboard for a startup head of product managing ~4 teams and 25–50 people. The system provides a real-time view of team progress, resource allocation, risks, and business metrics, with an AI feedback loop that the user can refine over time.

The system consists of two binaries:
- **`dashboard-server`** — Go backend exposing a REST API; handles source connectors, AI pipelines, sync orchestration, and persistent state (SQLite).
- **`dashboard-tui`** — Go Bubble Tea TUI client; connects to the server via HTTP on localhost and renders all views.

This PRD is written for an AI coding agent. Be explicit, follow each acceptance criterion exactly, and implement only what is specified. Do not add features, helpers, or abstractions beyond what is described.

---

## Goals

- Provide a head of product with a single pane of glass over team sprint status, goals, workload, velocity, and business metrics.
- Automate extraction of status from unstructured sources (Notion, GitHub) using Claude AI.
- Allow the user to correct AI assessments through an annotation system that feeds back into subsequent AI runs.
- Keep all credentials out of the database; keep all sync on-demand.
- Design the REST API so a future web frontend can use it without backend changes.

---

## User Stories

### US-001: Project Scaffolding
**Description:** As an AI coding agent, I need a runnable Go repo with config loading, `.env` loading, and placeholder server and TUI binaries so that subsequent phases have a clean foundation to build on.

**Acceptance Criteria:**
- [ ] `go mod init github.com/your-org/dashboard` executed; `go.mod` present.
- [ ] Directory tree created exactly as specified:
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
- [ ] `internal/config/config.go` defines `Config`, `ServerConfig`, `StorageConfig`, `AuthConfig`, `AIConfig` structs matching the `config.yaml` shape below. `Load(path string) (*Config, error)` uses `gopkg.in/yaml.v3`.
- [ ] `config.example.yaml` present with all fields documented:
  ```yaml
  server:
    port: 8080
  storage:
    path: ./data/dashboard.db
  auth:
    jwt_secret: "changeme"
    admin_username: "admin"
    admin_password_hash: "$2a$..."
  ai:
    provider: "claude-code"   # "anthropic" | "claude-code"
    model: "claude-sonnet-4-6"
    api_key: ""
    binary_path: "claude"
  ```
- [ ] `.env` loading implemented in `internal/config/env.go`: reads `.env` via `os.ReadFile` + manual `key=value` parse (no third-party library). Exposes typed accessors: `GitHubToken()`, `NotionToken()`, `GrafanaToken()`, `GrafanaBaseURL()`, `PostHogAPIKey()`, `PostHogHost()`, `SignozAPIKey()`, `SignozBaseURL()`. All credential reads in connectors go through these accessors — no direct `os.Getenv` elsewhere.
- [ ] `cmd/server/main.go`: parses `--config` flag (default `./config.yaml`), loads config + `.env`, prints masked config summary, exits cleanly.
- [ ] `cmd/tui/main.go`: parses `--server` flag (default `http://localhost:8080`), prints server address, exits cleanly.
- [ ] All dependencies added:
  ```
  github.com/go-chi/chi/v5
  github.com/charmbracelet/bubbletea
  github.com/charmbracelet/lipgloss
  modernc.org/sqlite
  github.com/golang-migrate/migrate/v4
  github.com/golang-jwt/jwt/v5
  golang.org/x/crypto
  gopkg.in/yaml.v3
  github.com/google/go-github/v60
  github.com/jomei/notionapi
  ```
- [ ] `go build ./...` succeeds with no errors.
- [ ] Both binaries run and exit cleanly (exit code 0).

---

### US-002: Database Layer
**Description:** As an AI coding agent, I need all SQLite tables, migrations, and typed query methods so that all subsequent phases can persist and retrieve data.

**Acceptance Criteria:**
- [ ] Migration infrastructure in `internal/store/migrations/`. SQL files embedded via `//go:embed`. `golang-migrate` wired with `modernc.org/sqlite` driver. `store.Migrate(db *sql.DB) error` applies all pending migrations on startup.
- [ ] Migration files numbered `0001`–`0008`, each with an `.up.sql` and `.down.sql`:
  - `0001_users.up.sql`:
    ```sql
    CREATE TABLE users (
        id            INTEGER PRIMARY KEY,
        username      TEXT    NOT NULL UNIQUE,
        password_hash TEXT    NOT NULL,
        role          TEXT    NOT NULL CHECK(role IN ('view','edit')),
        created_at    INTEGER NOT NULL
    );
    ```
  - `0002_teams.up.sql`:
    ```sql
    CREATE TABLE teams (
        id         INTEGER PRIMARY KEY,
        name       TEXT    NOT NULL,
        created_at INTEGER NOT NULL
    );
    CREATE TABLE team_members (
        id              INTEGER PRIMARY KEY,
        team_id         INTEGER NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
        display_name    TEXT    NOT NULL,
        github_username TEXT,
        notion_user_id  TEXT,
        created_at      INTEGER NOT NULL
    );
    ```
  - `0003_source_catalogue.up.sql`:
    ```sql
    CREATE TABLE source_catalogue (
        id                   INTEGER PRIMARY KEY,
        source_type          TEXT    NOT NULL,
        external_id          TEXT    NOT NULL,
        title                TEXT    NOT NULL,
        url                  TEXT,
        source_meta          TEXT,
        ai_suggested_purpose TEXT,
        status               TEXT    NOT NULL DEFAULT 'untagged',
        created_at           INTEGER NOT NULL,
        updated_at           INTEGER NOT NULL,
        UNIQUE(source_type, external_id)
    );
    ```
    Valid `source_type` values: `github_repo`, `notion_page`, `notion_db`, `grafana_panel`, `posthog_panel`, `signoz_panel`.
    Valid `status` values: `untagged`, `configured`, `ignored`.
  - `0004_source_configs.up.sql`:
    ```sql
    CREATE TABLE source_configs (
        id           INTEGER PRIMARY KEY,
        catalogue_id INTEGER NOT NULL REFERENCES source_catalogue(id) ON DELETE CASCADE,
        team_id      INTEGER REFERENCES teams(id) ON DELETE CASCADE,
        purpose      TEXT    NOT NULL,
        config_meta  TEXT,
        created_at   INTEGER NOT NULL,
        updated_at   INTEGER NOT NULL,
        UNIQUE(catalogue_id, team_id, purpose)
    );
    ```
    Valid `purpose` values: `current_plan`, `next_plan`, `goals`, `metrics_panel`, `org_goals`, `org_milestones`.
  - `0005_annotations.up.sql`:
    ```sql
    CREATE TABLE annotations (
        id         INTEGER PRIMARY KEY,
        tier       TEXT    NOT NULL CHECK(tier IN ('item','team')),
        team_id    INTEGER REFERENCES teams(id) ON DELETE CASCADE,
        item_ref   TEXT,
        content    TEXT    NOT NULL,
        archived   INTEGER NOT NULL DEFAULT 0,
        created_at INTEGER NOT NULL,
        updated_at INTEGER NOT NULL
    );
    ```
  - `0006_ai_cache.up.sql`:
    ```sql
    CREATE TABLE ai_cache (
        id         INTEGER PRIMARY KEY,
        input_hash TEXT    NOT NULL,
        pipeline   TEXT    NOT NULL,
        team_id    INTEGER REFERENCES teams(id) ON DELETE CASCADE,
        output     TEXT    NOT NULL,
        created_at INTEGER NOT NULL,
        UNIQUE(input_hash, pipeline, team_id)
    );
    ```
    Valid `pipeline` values: `sprint_parse`, `concerns`, `goal_extraction`, `workload`, `velocity`, `alignment`, `discovery_suggestion`.
  - `0007_sync_runs_sprint_meta.up.sql`:
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
    CREATE TABLE sprint_meta (
        id                  INTEGER PRIMARY KEY,
        team_id             INTEGER NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
        plan_type           TEXT    NOT NULL CHECK(plan_type IN ('current','next')),
        start_date          TEXT,
        total_sprints       INTEGER,
        current_sprint      INTEGER,
        source_catalogue_id INTEGER REFERENCES source_catalogue(id),
        detected_at         INTEGER NOT NULL,
        UNIQUE(team_id, plan_type)
    );
    ```
  - `0008_refresh_tokens.up.sql`:
    ```sql
    CREATE TABLE refresh_tokens (
        id         INTEGER PRIMARY KEY,
        user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
        token_hash TEXT    NOT NULL UNIQUE,
        expires_at INTEGER NOT NULL,
        created_at INTEGER NOT NULL
    );
    ```
- [ ] `internal/store/store.go`: `Store` wraps `*sql.DB`. `New(path string) (*Store, error)` opens the DB, enables WAL mode (`PRAGMA journal_mode=WAL`), enables foreign keys (`PRAGMA foreign_keys=ON`), runs migrations.
- [ ] `internal/store/models.go`: one Go struct per table, field names match column names exactly, using plain `sql.Scan`.
- [ ] Query method files on `*Store`:
  - `users.go`: `CreateUser`, `GetUserByUsername`, `GetUserByID`, `UpdateUser`, `DeleteUser`, `ListUsers`.
  - `teams.go`: `CreateTeam`, `UpdateTeam`, `DeleteTeam`, `ListTeams`, `AddMember`, `UpdateMember`, `DeleteMember`, `GetTeamMembers`.
  - `catalogue.go`: `UpsertCatalogueItem`, `ListCatalogue`, `GetCatalogueItem`, `UpdateCatalogueStatus`.
  - `source_configs.go`: `UpsertSourceConfig`, `ListSourceConfigs`, `DeleteSourceConfig`, `GetSourceConfigsForScope`.
  - `annotations.go`: `CreateAnnotation`, `UpdateAnnotation`, `DeleteAnnotation`, `ListAnnotations`, `ArchiveItemAnnotationsForPlan`.
  - `ai_cache.go`: `GetCacheEntry`, `SetCacheEntry`, `PruneStaleCache(olderThan time.Duration)`.
  - `sync_runs.go`: `CreateSyncRun`, `UpdateSyncRun`, `GetSyncRun`.
  - `sprint_meta.go`: `UpsertSprintMeta`, `GetSprintMeta`.
  - `refresh_tokens.go`: `CreateRefreshToken`, `GetRefreshTokenByHash`, `DeleteRefreshToken`, `DeleteExpiredRefreshTokens`.
- [ ] `go test ./internal/store/...` passes with an in-memory SQLite DB (`:memory:`), exercising basic CRUD round-trips for each table.

---

### US-003: Auth System
**Description:** As an AI coding agent, I need JWT issuance/validation, bcrypt password utilities, refresh token flow, role-enforcement middleware, and admin bootstrap so that the server can authenticate and authorize users.

**Acceptance Criteria:**
- [ ] `internal/auth/password.go`:
  - `HashPassword(plain string) (string, error)` — bcrypt cost 12.
  - `CheckPassword(hash, plain string) bool`.
- [ ] `internal/auth/jwt.go`:
  - Claims struct: `{ Sub string, Role string, exp }`.
  - `IssueToken(username, role, secret string) (string, error)` — 1-hour expiry.
  - `IssueRefreshToken() (rawToken string, hash string, err error)` — random 32 bytes, returns raw token + SHA-256 hex hash.
  - `ValidateToken(tokenStr, secret string) (*Claims, error)`.
- [ ] `internal/auth/bootstrap.go`: `Bootstrap(store *store.Store, cfg *config.Config) error`. If `users` table is empty: insert admin user with `username=cfg.Auth.AdminUsername`, `password_hash=cfg.Auth.AdminPasswordHash` (already bcrypt — store directly, do not re-hash), `role='edit'`.
- [ ] `cmd/server/main.go` supports `hash-password` subcommand: reads password from stdin, prints bcrypt hash (cost 12) to stdout, exits. Check `os.Args[1]` before normal server startup.
- [ ] `internal/api/middleware.go`:
  - `RequireAuth(secret string) func(http.Handler) http.Handler` — validates `Authorization: Bearer <token>`, sets `username` and `role` in request context (use typed context key). Returns 401 on invalid/missing token.
  - `RequireRole(role string) func(http.Handler) http.Handler` — checks context role, returns 403 if insufficient. `"edit"` role is required for mutations; `"view"` is sufficient for reads.
- [ ] Unit tests in `internal/auth/` covering: JWT issue + validate round-trip, expired token rejection, password hash + check, refresh token generation uniqueness.

---

### US-004: Backend API Skeleton
**Description:** As an AI coding agent, I need an HTTP server with a chi router, auth endpoints implemented, and all other routes returning 501 so that the API surface is established and auth can be tested end-to-end.

**Acceptance Criteria:**
- [ ] `internal/api/router.go`: `NewRouter(deps *Deps) http.Handler`. `Deps` struct carries: `Store *store.Store`, `Config *config.Config`, `Engine SyncEngine` (interface), `Pipeline PipelineRunner` (interface). Uses chi. Public routes: `POST /auth/login`, `POST /auth/refresh`. All other routes under `RequireAuth` middleware. All mutation routes additionally under `RequireRole("edit")`.
- [ ] `internal/api/response.go`:
  - `writeJSON(w http.ResponseWriter, status int, v any)` — marshal + set `Content-Type: application/json`.
  - `writeError(w http.ResponseWriter, status int, message string)` — writes `{"error": "message"}`.
  - `readJSON(r *http.Request, v any) error` — decode with 1 MB size limit.
- [ ] `internal/api/auth.go`:
  - `POST /auth/login` — reads `{username, password}`, looks up user, checks password with `auth.CheckPassword`, issues access token + refresh token, stores refresh token hash in DB, returns `{token, refresh_token}`. Returns 401 on bad credentials.
  - `POST /auth/refresh` — reads `{refresh_token}`, hashes it, looks up by hash, validates expiry, issues new access token, rotates refresh token (delete old, insert new), returns `{token, refresh_token}`. Returns 401 on invalid/expired token.
- [ ] Stub handler files (each route returns `writeError(w, 501, "not implemented")`):
  - `api/org.go` — `GET /org/overview`
  - `api/teams.go` — `GET /teams`, `GET /teams/{id}/sprint`, `GET /teams/{id}/goals`, `GET /teams/{id}/workload`, `GET /teams/{id}/velocity`, `GET /teams/{id}/metrics`
  - `api/config_teams.go` — `POST /config/teams`, `PUT /config/teams/{id}`, `DELETE /config/teams/{id}`, `POST /config/teams/{id}/members`, `PUT /config/members/{id}`, `DELETE /config/members/{id}`
  - `api/config_sources.go` — `GET /config/sources`, `POST /config/sources/discover`, `PUT /config/sources/{id}`
  - `api/config_annotations.go` — `GET /config/annotations`, `POST /config/annotations`, `PUT /config/annotations/{id}`, `DELETE /config/annotations/{id}`
  - `api/config_users.go` — `GET /config/users`, `POST /config/users`, `PUT /config/users/{id}`, `DELETE /config/users/{id}`
  - `api/sync.go` — `POST /sync`, `GET /sync/{run_id}`
  - `api/annotations.go` — `POST /annotations`, `PUT /annotations/{id}`, `DELETE /annotations/{id}`
- [ ] `cmd/server/main.go` updated: opens store, runs migrations, runs bootstrap, wires `Deps`, starts HTTP server on configured port.
- [ ] `POST /auth/login` with bootstrap admin credentials returns HTTP 200 with a valid JWT. Verified manually with curl.
- [ ] All other routes return 501.

---

### US-005: Source Connectors
**Description:** As an AI coding agent, I need all five source connectors (GitHub, Notion, Grafana, PostHog, SigNoz) implementing a shared `Discoverer` interface plus typed fetch methods so that the sync engine can retrieve data from external sources.

**Acceptance Criteria:**

**Shared connector interface** (`internal/connector/connector.go`):
```go
type DiscoveredItem struct {
    SourceType string
    ExternalID string
    Title      string
    URL        string
    SourceMeta map[string]any
    Excerpt    string // first ~500 chars of content, for AI suggestion
}

type Discoverer interface {
    Discover(ctx context.Context, target string) ([]DiscoveredItem, error)
}
```

- [ ] **GitHub connector** (`internal/connector/github/`):
  - `New(token string) *Client` using `google/go-github` with token-authenticated transport. GraphQL client: plain `net/http` with `Authorization: Bearer` hitting `https://api.github.com/graphql`.
  - `Discover(ctx, target string) ([]DiscoveredItem, error)` — target is `"owner/repo"`. Enumerates: labels, GitHub Projects (GraphQL `ProjectV2`), `.md` files (tree API filtered to `.md`). Returns one `DiscoveredItem` per label, project, and `.md` file.
  - `FetchIssues(ctx, owner, repo, label string, since time.Time) ([]*github.Issue, error)` — search API: `q=repo:{owner}/{repo}+label:{label}+updated:>{since}`.
  - `FetchDraftIssues(ctx, owner, repo, projectID, teamAreaValue string) ([]DraftIssue, error)` — GraphQL `ProjectV2Items` filtered by `Team/Area` field value and `contentType: DraftIssue`.
  - `FetchMergedPRs(ctx, owner, repo string, since time.Time) ([]*github.PullRequest, error)` — list closed PRs, filter client-side by `merged_at > since`.
  - `FetchCommits(ctx, owner, repo string, since, until time.Time, authors []string) ([]*github.RepositoryCommit, error)` — commits API with `since`/`until`, filter client-side by author login.
  - `FetchMarkdownFile(ctx, owner, repo, path string) (content string, sha string, err error)`.
  - `AutoTagIssues(ctx, owner, repo, projectID string, teamLabelMap map[string]string) error` — GraphQL pages all project items; for each item with `Team/Area` set but missing the matching label on the linked issue, applies the label via REST.

- [ ] **Notion connector** (`internal/connector/notion/`):
  - `New(token string) *Client` wrapping `jomei/notionapi`.
  - `Discover(ctx, workspaceHint string) ([]DiscoveredItem, error)` — `POST /search` with empty query; returns one item per page (`notion_page`) and database (`notion_db`).
  - `FetchPage(ctx, pageID string) (content string, lastEditedTime time.Time, err error)` — fetches blocks recursively, converts to plain text: headings → `# text`, bullets → `- text`, paragraphs → plain text, toggles → `> text`.
  - `FetchDatabase(ctx, dbID string, updatedAfter time.Time) ([]DatabaseRow, error)` — queries with `filter: {timestamp: last_edited_time, after: updatedAfter}`. Each `DatabaseRow` has title property + linked page content fetched recursively.

- [ ] **Grafana connector** (`internal/connector/grafana/`):
  - `New(baseURL, token string) *Client`.
  - `Discover(ctx, dashboardURL string) ([]DiscoveredItem, error)` — extract dashboard UID from URL path, call `GET /api/dashboards/uid/:uid`, return one `DiscoveredItem` per panel.
  - `FetchPanel(ctx, dashboardUID, panelID string, from, to time.Time) (PanelData, error)` — uses `/api/ds/query`.

- [ ] **PostHog connector** (`internal/connector/posthog/`):
  - `New(host, apiKey string) *Client`.
  - `Discover(ctx, projectID string) ([]DiscoveredItem, error)` — `GET /api/projects/:id/dashboards/`, returns one item per insight/tile.
  - `FetchInsight(ctx, projectID, insightID string) (InsightResult, error)` — `GET /api/projects/:id/insights/:id/`.

- [ ] **SigNoz connector** (`internal/connector/signoz/`):
  - `New(baseURL, apiKey string) *Client`.
  - `Discover(ctx, target string) ([]DiscoveredItem, error)` — enumerate dashboards via SigNoz dashboard list API.
  - `FetchPanel(ctx, dashboardID, panelID string) (PanelData, error)` — fetch panel query results.

- [ ] Each connector compiles (`go build ./...` passes).
- [ ] All connectors return a clear error if required credentials (from `.env` accessors) are missing — they do not panic.

---

### US-006: AI Layer
**Description:** As an AI coding agent, I need the `Generator` interface, both provider implementations (Anthropic REST and ClaudeCode subprocess), a provider factory, and a caching wrapper so that all AI pipeline calls are deduplicated and the correct provider is used at runtime.

**Acceptance Criteria:**
- [ ] `internal/ai/generator.go`:
  ```go
  type Generator interface {
      Generate(ctx context.Context, prompt string) (string, error)
  }
  ```
- [ ] `internal/ai/anthropic.go`: `AnthropicProvider` with fields `apiKey string`, `model string`.
  - `Generate` — `POST https://api.anthropic.com/v1/messages` with body `{"model": model, "max_tokens": 4096, "messages": [{"role": "user", "content": prompt}]}`. Headers: `x-api-key: apiKey`, `anthropic-version: 2023-06-01`, `content-type: application/json`. Parse response, return `content[0].text`. If `ANTHROPIC_API_KEY` env var is set, it overrides the config `api_key`.
- [ ] `internal/ai/claude_code.go`: `ClaudeCodeProvider` with fields `binaryPath string`, `model string`.
  - `Generate(ctx, prompt string) (string, error)`:
    - Runs: `claude --print --dangerously-skip-permissions --output-format stream-json [--model MODEL] PROMPT`
    - Streams stdout line by line; parses each line as JSON.
    - Collects text from events where `type` is `"result"` or `"content_block_delta"` (field `delta.text`).
    - Returns concatenated text.
    - On non-zero exit: returns stderr content as error.
- [ ] `internal/ai/factory.go`: `New(cfg config.AIConfig) (Generator, error)` — returns `ClaudeCodeProvider` if `cfg.Provider == "claude-code"`, `AnthropicProvider` if `cfg.Provider == "anthropic"`, error otherwise.
- [ ] `internal/ai/cached_generator.go`: `CachedGenerator` wraps a `Generator` and a `*store.Store`.
  - `Generate(ctx context.Context, pipeline string, teamID *int64, inputs map[string]any, annotations []store.Annotation) (string, error)`:
    1. Serialize `inputs` + `annotations` to canonical JSON (keys sorted alphabetically at every level).
    2. Compute `SHA-256` hex of the canonical JSON → `inputHash`.
    3. Call `store.GetCacheEntry(inputHash, pipeline, teamID)` — on hit, return stored output immediately.
    4. Build final prompt: pipeline template (from `internal/pipeline/`) + serialized inputs. (At this layer, just accept a pre-built prompt string as additional parameter OR let the pipeline layer call this with the full prompt already assembled — choose one approach and be consistent.)
    5. Call inner `Generator.Generate(ctx, fullPrompt)`.
    6. Store result via `store.SetCacheEntry(inputHash, pipeline, teamID, result)`.
    7. Return result.
- [ ] Unit tests with a mock `Generator`. Verify: cache hit returns stored value without calling mock; cache miss calls mock and stores result; different inputs produce different hashes.

---

### US-007: AI Pipelines
**Description:** As an AI coding agent, I need all seven AI pipelines implemented with full prompts, input collection, JSON schema output, and typed result structs so that the sync engine can extract structured intelligence from raw source data.

**Acceptance Criteria:**

Each pipeline lives in `internal/pipeline/<name>.go`. All use this shared prompt wrapper:

```
You are analyzing a software team's status data. Respond ONLY with valid JSON matching this schema:
<schema>
{SCHEMA}
</schema>

{IF ANNOTATIONS:
The user has provided the following context. Take it fully into account. Do not re-flag a concern that the user has already addressed with an explanation, unless you have new evidence that contradicts the user's context.
<annotations>
{ANNOTATIONS_JSON}
</annotations>
}

<inputs>
{INPUTS_JSON}
</inputs>
```

- [ ] **`sprint_parse`** (`pipeline/sprint_parse.go`):
  - Input: `{ "sprint_plan_text": "..." }`.
  - Prompt instructs Claude to extract the sprint start date, total number of sprints planned, which sprint week is currently active, and the list of high-level plan goals.
  - Output schema:
    ```json
    {
      "start_date": "2024-01-15",
      "total_sprints": 4,
      "current_sprint": 2,
      "goals": ["Goal A", "Goal B"]
    }
    ```
  - `start_date` may be `null` if not found in the document.
  - After successful run: call `store.UpsertSprintMeta(...)` with parsed values.

- [ ] **`goal_extraction`** (`pipeline/goal_extraction.go`):
  - Input: `{ "goals_doc_text": "...", "sprint_plan_text": "..." }`.
  - Output schema:
    ```json
    {
      "goals": [
        { "text": "...", "source": "goals_doc" }
      ]
    }
    ```
  - `source` is `"goals_doc"` or `"sprint_plan"`.

- [ ] **`concerns`** (`pipeline/concerns.go`):
  - Input: `{ "open_issues": [...], "merged_prs": [...], "sprint_plan_text": "...", "extracted_goals": [...], "sprint_meta": {...} }`. Active annotations (both tiers, non-archived) for the team are appended in the annotations block.
  - Prompt instructs Claude to identify blocked items, resource mismatches, plan omissions, and schedule risks. Each concern must have a stable `key` derived from the concern subject (not its position).
  - Output schema:
    ```json
    {
      "concerns": [
        {
          "key": "unique-stable-slug",
          "summary": "Short summary",
          "explanation": "Plain-language explanation of why this was flagged.",
          "severity": "high"
        }
      ]
    }
    ```
  - Valid `severity` values: `"high"`, `"medium"`, `"low"`.

- [ ] **`workload_estimation`** (`pipeline/workload.go`):
  - Input: `{ "members": [{ "name": "Alice", "open_issues": [...], "open_prs": [...], "recent_commits": 12 }], "sprint_window": { "start": "2024-01-15", "end": "2024-01-19" }, "standard_sprint_days": 5 }`.
  - Output schema:
    ```json
    {
      "members": [
        { "name": "Alice", "estimated_days": 3.5, "label": "NORMAL" }
      ]
    }
    ```
  - Label thresholds: `LOW` < 3, `NORMAL` 3–5, `HIGH` > 5.

- [ ] **`velocity_analysis`** (`pipeline/velocity.go`):
  - Input: `{ "sprints": [{ "label": "Week of 2024-01-01", "closed_issues": [...], "merged_prs": [...], "commit_count": 42 }] }`. Up to N sprints (default 4).
  - Output schema:
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
  - Prompt instructs Claude to weight each item by estimated difficulty to normalize the score.

- [ ] **`goal_alignment`** (`pipeline/alignment.go`):
  - Input: `{ "org_goals_text": "...", "team_goals": { "1": ["Goal A", "Goal B"], "2": ["Goal C"] } }`.
  - Output schema:
    ```json
    {
      "alignments": [
        { "team_id": 1, "aligned": true, "notes": "..." }
      ],
      "flags": ["Team B's work does not appear to contribute to any org goal"]
    }
    ```

- [ ] **`discovery_suggestion`** (`pipeline/discovery.go`):
  - Input: `{ "title": "...", "excerpt": "..." }` (excerpt = first 500 chars of content).
  - Output schema:
    ```json
    {
      "suggested_purpose": "current_plan",
      "confidence": "high",
      "reasoning": "..."
    }
    ```
  - Valid `suggested_purpose` values: `current_plan`, `next_plan`, `goals`, `metrics_panel`, `org_goals`, `org_milestones`, `unknown`.
  - Valid `confidence` values: `high`, `medium`, `low`.

- [ ] **`internal/pipeline/runner.go`**: `Runner` struct holds `*ai.CachedGenerator` and `*store.Store`. One public method per pipeline:
  - `RunSprintParse(ctx, teamID int64, sprintPlanText string) (*SprintParseResult, error)`
  - `RunGoalExtraction(ctx, teamID int64, goalsDocText, sprintPlanText string) (*GoalExtractionResult, error)`
  - `RunConcerns(ctx, teamID int64, input ConcernsInput) (*ConcernsResult, error)`
  - `RunWorkloadEstimation(ctx, teamID int64, input WorkloadInput) (*WorkloadResult, error)`
  - `RunVelocityAnalysis(ctx, teamID int64, input VelocityInput) (*VelocityResult, error)`
  - `RunGoalAlignment(ctx, orgGoalsText string, teamGoals map[int64][]string) (*AlignmentResult, error)`
  - `RunDiscoverySuggestion(ctx, title, excerpt string) (*DiscoverySuggestionResult, error)`
  - Each method: fetches active annotations for the scope from the store, assembles the prompt using the shared wrapper, calls `CachedGenerator.Generate`, unmarshals JSON response into the typed result struct. Returns a structured error if Claude's response is not valid JSON matching the schema.
- [ ] `go build ./...` passes.
- [ ] Integration test: `RunSprintParse` called with a short sample sprint plan document (using `ClaudeCodeProvider`) returns a non-nil result with at least one goal.

---

### US-008: Sync Engine
**Description:** As an AI coding agent, I need the discovery pass and incremental sync engine — both async with sync run tracking — so that the server can fetch fresh data from all configured sources and trigger the correct AI pipelines.

**Acceptance Criteria:**
- [ ] `internal/sync/engine.go`:
  ```go
  type Engine struct {
      store    *store.Store
      github   *github.Client
      notion   *notion.Client
      grafana  *grafana.Client
      posthog  *posthog.Client
      signoz   *signoz.Client
      pipeline *pipeline.Runner
  }
  func New(...) *Engine
  ```
- [ ] `internal/sync/discover.go`: `(e *Engine) Discover(ctx context.Context, scope, target string) (syncRunID int64, err error)`:
  1. Insert `sync_runs` row with `status='running'`, return `syncRunID`.
  2. Launch goroutine:
     a. Call appropriate connector's `Discover(ctx, target)` based on `scope` (`notion_workspace`, `github_repo`, `metrics_url`).
     b. For each `DiscoveredItem`: call `store.UpsertCatalogueItem` (update metadata if exists, insert with `status='untagged'` if new).
     c. For each **new** item: call `pipeline.RunDiscoverySuggestion(ctx, item.Title, item.Excerpt)` → set `ai_suggested_purpose` via `store.UpdateCatalogueAISuggestion`.
     d. `store.UpdateSyncRun(id, 'completed', nil)` on success; `'failed'` with error string on failure.
- [ ] `internal/sync/sync.go`: `(e *Engine) Sync(ctx context.Context, scope string, teamID *int64) (syncRunID int64, err error)`:
  1. Check for existing `running` sync run for the same scope+teamID — return existing `syncRunID` with no new run if already running (no 409 at this layer; the API layer returns 409).
  2. Insert `sync_runs` row with `status='running'`, return `syncRunID`.
  3. Launch goroutine:
     a. Load `source_configs` for scope.
     b. For each configured source: fetch incrementally using the appropriate connector method (use `source_catalogue.updated_at` as `since` timestamp). Update `source_catalogue.updated_at` on success.
     c. Determine affected pipelines: any source change → re-run all pipelines for that team. (The caching layer handles dedup — just always call the pipeline methods; they are no-ops on cache hit.)
     d. Run pipelines in order: `sprint_parse` → `goal_extraction` → `concerns` → `workload_estimation` → `velocity_analysis`.
     e. If `scope == 'org'`: also run `goal_alignment` with all teams' extracted goals + org goals.
     f. `store.UpdateSyncRun(id, 'completed', nil)` on success; `'failed'` with error on failure. Partial source failures are recorded in the error JSON but do not abort the run.
- [ ] `api/sync.go` stubs replaced with real handlers:
  - `POST /sync` — validates `scope` and `team_id` (required when `scope='team'`). Checks for existing running sync run for the same scope; returns `409 {"error": "sync already running"}` if busy. Calls `engine.Sync(...)`, returns `{"sync_run_id": N}`.
  - `GET /sync/{run_id}` — returns the `sync_runs` row as JSON.
  - `POST /config/sources/discover` — validates `scope` + `target`, calls `engine.Discover(...)`, returns `{"sync_run_id": N}`.
- [ ] TUI polls `GET /sync/{run_id}` every 2 seconds; sync banner disappears on `completed` or changes to error on `failed`.

---

### US-009: REST API Implementation
**Description:** As an AI coding agent, I need all 501 stub endpoints replaced with real implementations that read from and write to the store and pipeline cache so that the TUI and future web frontend can retrieve live data.

**Acceptance Criteria:**

- [ ] **`GET /org/overview`**: loads all teams; for each team loads latest `sprint_meta`, cached `concerns` output, cached `workload` output; loads `goal_alignment` output; loads per-member cross-team workload aggregate. Returns:
  ```json
  {
    "teams": [
      {
        "id": 1, "name": "Engineering",
        "current_sprint": 2, "total_sprints": 4,
        "risk_level": "high",
        "focus": "...",
        "last_synced_at": 1710000000
      }
    ],
    "workload": [
      { "name": "Alice", "total_days": 5.0, "label": "HIGH", "breakdown": {"Engineering": 3.5, "Marketing": 1.5} }
    ],
    "goal_alignment": { "alignments": [...], "flags": [...] },
    "last_synced_at": 1710000000
  }
  ```

- [ ] **`GET /teams`**: returns `[{ id, name, members: [{ id, display_name, github_username, notion_user_id }] }]`.

- [ ] **`GET /teams/{id}/sprint`**: computes current sprint week as `floor(days_elapsed / 7) + 1` where `days_elapsed = today - start_date`. Returns:
  ```json
  {
    "plan_type": "current",
    "start_date": "2024-01-15",
    "current_sprint": 2,
    "total_sprints": 4,
    "start_date_missing": false,
    "next_plan_start_risk": false,
    "goals": ["..."],
    "last_synced_at": 1710000000
  }
  ```
  `start_date_missing: true` when `sprint_meta.start_date` is NULL. `next_plan_start_risk: true` when `total_sprints > 4` AND a next-plan `source_config` exists for this team.

- [ ] **`GET /teams/{id}/goals`**: returns `goal_extraction` + `concerns` pipeline outputs from cache with annotation refs. Returns:
  ```json
  {
    "goals": [{ "text": "...", "source": "sprint_plan" }],
    "concerns": [{ "key": "...", "summary": "...", "explanation": "...", "severity": "high", "annotation_id": null }],
    "last_synced_at": 1710000000
  }
  ```

- [ ] **`GET /teams/{id}/workload`**: returns `workload_estimation` output from cache. Returns:
  ```json
  {
    "members": [{ "name": "Alice", "estimated_days": 3.5, "label": "NORMAL" }],
    "last_synced_at": 1710000000
  }
  ```

- [ ] **`GET /teams/{id}/velocity`**: returns `velocity_analysis` output from cache. Returns:
  ```json
  {
    "sprints": [{ "label": "Week of 2024-01-01", "score": 42, "breakdown": { "issues": 20, "prs": 15, "commits": 7 } }],
    "last_synced_at": 1710000000
  }
  ```

- [ ] **`GET /teams/{id}/metrics`**: loads configured `metrics_panel` source configs for team; returns panel data. Returns:
  ```json
  { "panels": [{ "title": "DAU", "value": "1,234", "panel_id": "..." }] }
  ```

- [ ] **Config — Teams CRUD** (`config_teams.go`): standard CRUD delegating to store. `POST /config/teams` accepts `{name}`. `PUT /config/teams/{id}` accepts `{name}`. `DELETE /config/teams/{id}`. `POST /config/teams/{id}/members` accepts `{display_name, github_username, notion_user_id}`. `PUT /config/members/{id}`. `DELETE /config/members/{id}`. All return appropriate JSON.

- [ ] **Config — Sources** (`config_sources.go`):
  - `GET /config/sources` — returns full source catalogue with `status` and `ai_suggested_purpose`.
  - `PUT /config/sources/{id}` — accepts `{status, purpose, team_id, config_meta}`. Updates source config. If `purpose='current_plan'` and a different `current_plan` already exists for the same team: triggers plan rollover (see US-010).

- [ ] **Config — Annotations CRUD** (`config_annotations.go`): `GET` returns all annotations grouped by tier (`item` vs `team`), including archived. `POST` accepts `{tier, team_id, item_ref, content}`. `PUT` updates content. `DELETE` hard-deletes.

- [ ] **Config — Users CRUD** (`config_users.go`): edit-role only. `POST` hashes password before storing. All standard CRUD. Cannot delete the last edit-role user (return 409).

- [ ] **Inline annotations** (`annotations.go`): `POST /annotations`, `PUT /annotations/{id}`, `DELETE /annotations/{id}` — same logic as config annotations, different mount point.

- [ ] All endpoints return `last_synced_at` from the most recent `completed` `sync_runs` row for the relevant scope.
- [ ] Manual curl tests pass for each endpoint.

---

### US-010: Plan Rollover Logic
**Description:** As an AI coding agent, I need plan rollover logic so that when the user re-tags a source as the new current plan, all item-tier annotations for the old plan are automatically archived and sprint metadata is reset.

**Acceptance Criteria:**
- [ ] `PUT /config/sources/{id}` with `purpose='current_plan'`:
  1. Look up the existing `source_configs` row for `(team_id, purpose='current_plan')`.
  2. If it differs from the new one (old plan is being replaced):
     a. Call `store.ArchiveItemAnnotationsForPlan(teamID)`:
        ```sql
        UPDATE annotations SET archived=1, updated_at=? WHERE team_id=? AND tier='item' AND archived=0
        ```
     b. Delete old `source_configs` row for `(team_id, purpose='current_plan')`.
     c. Insert new `source_configs` row.
     d. Trigger `sprint_parse` pipeline for the new document (or mark it for re-parse on next sync).
  3. If no previous current plan exists: simply insert the new `source_configs` row.
- [ ] `team`-tier annotations for the same team are untouched by rollover (SQL `WHERE tier='item'` ensures this).
- [ ] Archived annotations remain visible in `GET /config/annotations` (with `archived: true` field) but are excluded from all pipeline inputs (`pipeline.Runner` methods filter `WHERE archived=0`).
- [ ] Unit test: create team, create item and team annotations, trigger rollover, verify item annotations have `archived=1` and team annotations have `archived=0`.

---

### US-011: TUI
**Description:** As an AI coding agent, I need a full Bubble Tea TUI with drill-down navigation, all views, a sync banner, an annotation panel, and a login screen so that the head of product can interact with the dashboard entirely from the terminal.

**Acceptance Criteria:**

**Architecture:**
- [ ] `internal/tui/app.go`: `App` struct with `views []tea.Model` (stack), `client *client.Client`, `syncPoller *SyncPoller`. `Enter` pushes; `Esc`/`Backspace` pops. `q` quits only when stack depth is 1.
- [ ] `internal/tui/client/client.go`: typed HTTP client. Reads JWT from `~/.dashboard/token`. On 401: attempts token refresh. If refresh fails: presents login screen. Methods: `Login`, `GetOrgOverview`, `GetTeams`, `GetSprint`, `GetGoals`, `GetWorkload`, `GetVelocity`, `GetMetrics`, `PostSync`, `GetSyncRun`, `PostAnnotation`, `PutAnnotation`, `DeleteAnnotation`, `GetConfigSources`, `PutConfigSource`, `PostDiscover`, `GetConfigAnnotations`, `GetConfigUsers`, `PostConfigUser`, `PutConfigUser`, `DeleteConfigUser`.

**Views** (each in `internal/tui/views/`):
- [ ] **Login** (`login.go`): username + password text inputs. Submit with Enter. On success: stores `token` + `refresh_token` to `~/.dashboard/token` (JSON). On failure: shows error message.
- [ ] **Org Overview** (`org_overview.go`): list of team cards — `[Team Name]  Sprint N/M  |  Risk: HIGH  |  Focus: ...`. `j`/`k` to navigate. `Enter` → push Team View. `c` → push Config Root. Bottom row: org-level metrics summary. Footer: `Last synced: <timestamp>`. `R` → trigger full org sync + show banner.
- [ ] **Team View** (`team.go`): sub-menu: `Sprint & Plan Status`, `Goals & Concerns`, `Resource/Workload`, `Velocity`, `Business Metrics`. `j`/`k` + `Enter` to drill in. `r` → team sync.
- [ ] **Sprint & Plan Status** (`sprint.go`): shows `Week N of M`. If `start_date_missing: true`: shows `Warning: Sprint start date not found. Add it to the plan document or annotate it in Config.` in amber. If `next_plan_start_risk: true`: shows red warning `Current plan extended to sprint M. This delays the next plan's start.`. Lists plan goals.
- [ ] **Goals & Concerns** (`goals.go`): two sections separated by a divider: Goals and Concerns. Each concern: `[HIGH] summary` with explanation below. Colors: HIGH=red, MEDIUM=yellow, LOW=gray (using Lip Gloss). `a` on any item → push Annotation Panel.
- [ ] **Annotation Panel** (`annotate.go`): overlay/pushed view. Shows the item being annotated. Tier selector toggled with Tab: `[Item-level]` / `[Team-level]`. Multi-line text input. Submit with `Ctrl+Enter`. On submit: `POST /annotations`, pop view.
- [ ] **Resource/Workload** (`workload.go`): per-member table: `Alice: 3.5 days [HIGH]`. Color-coded labels (same colors as concerns). `a` on a row → annotation panel with team-level tier pre-selected.
- [ ] **Velocity** (`velocity.go`): sparkline-style chart using unicode block characters (`▁▂▃▄▅▆▇█`) across recent sprints. Below: tabular breakdown per sprint.
- [ ] **Business Metrics** (`metrics.go`): list of configured panels with title + latest value.
- [ ] **Config Root** (`config/config_root.go`): sub-menu: Teams & Members, Sources, Business Metrics, Annotations, Users.
- [ ] **Config — Teams & Members** (`config/config_teams.go`): list teams. `Enter` → expand team, show members. `n` → new team (text input popup). `e` → edit. `d` → delete (confirm prompt: `Delete team "X"? [y/N]`). On team expand: list members with `n`/`e`/`d`.
- [ ] **Config — Sources** (`config/config_sources.go`): catalogue table: `[type]  title  |  status  |  purpose`. Filter by status with `f`. `Enter` on item → tagging panel: set purpose, team, show AI suggestion. `D` → run discovery (prompts for target URL/scope).
- [ ] **Config — Annotations** (`config/config_annotations.go`): two groups: Team-level and Item-level. Archived items grayed out, labeled `[Archived]`. `e` → edit content. `d` → hard delete.
- [ ] **Config — Users** (`config/config_users.go`): only visible to edit-role users. List users with role. `n` → create (username + password + role). `e` → edit role/password. `d` → delete.

**Sync Banner** (`tui/components/banner.go`):
- [ ] Rendered at top of every view when a sync run is active: `[ Sync in progress... (Team Name) ]`.
- [ ] `SyncPoller` goroutine polls `GET /sync/{run_id}` every 2 seconds via `time.Ticker`. Sends `SyncDoneMsg` or `SyncFailedMsg` to the Bubble Tea program via `tea.Program.Send`.
- [ ] On `completed`: banner disappears.
- [ ] On `failed`: banner turns red: `[ Sync failed: <error> ]  Press Enter to dismiss`.

**Key bindings:**
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
| `q` | Quit (top level only) |

- [ ] **No data state**: before first sync, each data view shows `No data yet. Press r to sync this team (or R for full org).`
- [ ] Footer on all data views: `Last synced: <timestamp>` (or `Never synced` if no completed sync).

---

### US-012: Wiring & Integration
**Description:** As an AI coding agent, I need all system components wired together with graceful shutdown, cache pruning, auto-tagger scheduling, and a smoke test script so that the full system works end-to-end.

**Acceptance Criteria:**
- [ ] **`hash-password` subcommand** in `cmd/server/main.go`: `if len(os.Args) > 1 && os.Args[1] == "hash-password"` → read password from stdin, print bcrypt hash, exit 0.
- [ ] **Graceful shutdown**: `cmd/server/main.go` uses `signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)`. Server uses `http.Server.Shutdown(ctx)`. In-progress sync goroutines tracked with a `sync.WaitGroup`; shutdown waits up to 30 seconds before forcing exit.
- [ ] **Cache pruning**: on server startup, call `store.PruneStaleCache(30 * 24 * time.Hour)` to delete `ai_cache` entries older than 30 days.
- [ ] **Auto-tagger**: `cmd/server/main.go` starts a `time.Ticker` every 12 hours that calls `engine.AutoTag(ctx)` for all configured GitHub repos. `engine.AutoTag` calls `github.AutoTagIssues` for each configured repo+project.
- [ ] **`GET /admin/autotag`** endpoint (edit-role only): triggers `engine.AutoTag(ctx)` immediately and returns `{"status": "ok"}`.
- [ ] **Integration smoke test script** (`scripts/smoke_test.sh`):
  - Starts server in background.
  - Logs in, saves token.
  - Calls `GET /teams` — expects 200.
  - Calls `GET /org/overview` — expects 200.
  - Calls `POST /sync` with `scope=org` — expects 200 with `sync_run_id`.
  - Polls `GET /sync/{run_id}` until `completed` or timeout (60s).
  - Calls `GET /teams/{id}/sprint` — expects 200.
  - Prints PASS or FAIL with curl output on failure.
- [ ] `go build ./...` succeeds with no errors.
- [ ] Both `cmd/server` and `cmd/tui` binaries produce no race conditions under `-race` flag.

---

### US-013: Hardening & Edge Cases
**Description:** As an AI coding agent, I need all edge cases from the spec handled gracefully so that the system is robust in production use.

**Acceptance Criteria:**
- [ ] **Missing sprint start date**: `GET /teams/{id}/sprint` returns `start_date_missing: true`. TUI Sprint view shows the warning message in amber. No panic or error response.
- [ ] **Extended sprint (5th+ sprint)**: when `total_sprints > 4` and a next-plan source config exists for the team, `GET /teams/{id}/sprint` returns `next_plan_start_risk: true`. TUI shows `Week N of M` with M highlighted in amber if M > 4.
- [ ] **Concurrent sync prevention**: `POST /sync` returns `409 {"error": "sync already running for this scope"}` if a `running` sync run already exists for the same scope+teamID. Does not start a second goroutine.
- [ ] **AI output not valid JSON**: pipeline runner logs the raw output at ERROR level. Returns a typed error: `ErrInvalidAIOutput`. TUI shows `AI processing error — try syncing again.` with a dismiss option (`d` or `Enter`). Does not crash.
- [ ] **Per-source fetch errors**: connector errors during sync are collected per-source. Sync run still completes for other sources. `sync_runs.error` stores a JSON object `{"source_id": "error message", ...}`. TUI sync error banner lists which sources failed.
- [ ] **Missing `.env` credentials**: each connector checks for required env vars in its constructor. If a required var is missing, the connector method returns `ErrCredentialsMissing` with the var name. Discovery/sync for that connector type surfaces a clear error in the TUI (not a panic or generic 500).
- [ ] **Stale annotation warning (optional)**: `concerns` pipeline prompt includes: `"If any user annotation appears to be contradicted by the current evidence, flag it with type stale_annotation."` If the output contains a concern with `key` starting with `stale_annotation_`, the TUI highlights it with a `[STALE ANNOTATION]` prefix in orange.
- [ ] **First run — no data**: before any sync has completed, all data endpoints (`/teams/{id}/sprint`, `/goals`, `/workload`, `/velocity`, `/metrics`) return `200` with empty arrays/null fields plus `"last_synced_at": null`. TUI shows the no-data placeholder message in each view.
- [ ] **Expired JWT handling**: TUI client automatically attempts refresh on 401. If refresh fails (expired or invalid refresh token), clears `~/.dashboard/token` and presents the login screen. No crash.
- [ ] **`go test -race ./...`** passes with no data races detected.

---

## Functional Requirements

- FR-1: The system must expose a REST API consumed by both the TUI and (in future) a web frontend. No business logic in the TUI.
- FR-2: All source credentials must be loaded from `.env` at startup. No credentials stored in SQLite or returned by the API.
- FR-3: All sync must be triggered manually by the user (no background scheduled sync).
- FR-4: The dashboard must display the last cached state immediately on load, with a `Last synced: <timestamp>` footer.
- FR-5: AI outputs must be cached by `SHA-256(canonical_json(inputs + active_annotations))`. The same inputs must never trigger a re-call to Claude.
- FR-6: Every AI-generated concern must include a plain-language explanation and a stable key.
- FR-7: Annotations come in two tiers: `item` (auto-archived at plan rollover) and `team` (persists indefinitely). Users select the tier when annotating. Default is `item`.
- FR-8: At plan rollover (user re-tags current plan document), all non-archived `item`-tier annotations for that team must be set to `archived=1`. `team`-tier annotations must not be affected.
- FR-9: The system must prevent two concurrent sync runs for the same scope+team.
- FR-10: The workload view must show work-days per person (not percentages). Team view shows only that team's work. Org view aggregates cross-team. Standard sprint = 5 days. Labels: LOW < 3, NORMAL 3–5, HIGH > 5.
- FR-11: Sprint week is computed as `floor((today - start_date).days / 7) + 1`. If `start_date` is not found in the sprint plan document, the system must warn the user rather than crash.
- FR-12: When `total_sprints > 4` and a next-plan document is configured, the system must flag schedule risk for the next plan.
- FR-13: GitHub issues are assumed to be closed at sprint end (Done / Not Completed / Won't Do). The system does not track carryover. Workload uses only **open** issues; velocity uses only **closed** issues within the sprint window.
- FR-14: Users are created and managed by admin in Config → Users. No self-registration. Two roles: `view` (read-only) and `edit` (annotate, configure, sync, manage users).
- FR-15: The `hash-password` subcommand must produce a bcrypt hash (cost 12) from stdin and exit.

---

## Non-Goals

- Web frontend (API is designed for it; not built in this PRD).
- Inter-team dependency tracking.
- Proactive notifications or alerts (dashboard is passive).
- Marketing/non-developer velocity tracking.
- OAuth flows for GitHub or Notion (PAT/integration tokens only).
- Multi-tenant deployment.
- Snooze on annotations (dismiss with context only).
- Automated stale annotation detection (surfaced via AI hint, not auto-removed).
- Fine-grained per-team access control (access is global: view or edit).
- Performance analysis or backward-looking reporting.

---

## Technical Considerations

- **Language:** Go. Single binary per component. No CGo (use `modernc.org/sqlite`).
- **TUI framework:** Bubble Tea + Lip Gloss.
- **HTTP router:** chi v5.
- **Database:** SQLite with WAL mode and foreign keys enabled.
- **Migrations:** `golang-migrate/migrate` with embedded SQL files.
- **AI:** `Generator` interface. Default provider: `claude-code` (subprocess). Alternative: `anthropic` (REST). Provider set in `config.yaml`.
- **Auth:** JWT (1h expiry) + refresh tokens (30d, stored as SHA-256 hash in DB). bcrypt cost 12.
- **Deployment target:** local Mac. No Docker required.
- **Config:** `config.yaml` for app config. `.env` for source credentials. Both gitignored if sensitive.
- **Token storage (TUI):** `~/.dashboard/token` — JSON with `token` and `refresh_token` fields.
- **Concurrency:** sync runs in goroutines. One sync per scope allowed at a time. Use `sync.WaitGroup` for graceful shutdown.
- **AI caching:** input hash computed from canonical JSON (alphabetically sorted keys at all levels). SHA-256 hex string. Stored in `ai_cache` table keyed by `(input_hash, pipeline, team_id)`.

---

## Success Metrics

- `go build ./...` succeeds with no errors.
- `go test ./...` passes with no failures.
- `go test -race ./...` produces no data races.
- After running the smoke test script against a real GitHub repo + Notion workspace: all read endpoints return non-empty data.
- A full org sync completes without error when all `.env` credentials are present.
- An annotation added inline in the TUI suppresses the corresponding concern on the next sync.
- Plan rollover archives item annotations and leaves team annotations intact.

---

## Open Questions

- Should the `velocity_analysis` pipeline's sprint windows align exactly with calendar weeks, or should they use the `start_date` from `sprint_meta` as the anchor? (Assume: use `start_date` as anchor if available, fall back to calendar weeks.)
- Should the org workload aggregate call a single AI pipeline or aggregate the per-team results in Go? (Assume: aggregate in Go — sum `estimated_days` per person across teams.)
- Should the TUI login screen appear only when no valid token file exists, or also when the server returns 401 on startup health-check? (Assume: also on startup 401 — the client checks token validity on launch.)
