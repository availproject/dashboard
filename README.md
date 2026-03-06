# Dashboard

A terminal-based product status dashboard for heads of product. Provides a real-time, AI-assisted view of team progress, workload, sprint health, and business metrics — aggregated from GitHub, Notion, Grafana, PostHog, and SigNoz.

## Architecture

Two binaries communicate over a local HTTP API:

- **`cmd/server`** — REST API server. Manages the SQLite database, syncs data from external sources, runs AI analysis pipelines, and serves all data endpoints.
- **`cmd/tui`** — Bubble Tea terminal UI. Connects to the server and renders the dashboard views.

```
cmd/server      →  :8080 (REST API)
cmd/tui         →  connects to localhost:8080
```

## Features

- **Org overview** — cross-team sprint health, workload, and AI-generated concerns
- **Team view** — sprint details, goals, workload by member (in work-days), velocity, and business metrics
- **On-demand sync** — pull latest data from all sources on demand
- **Discovery** — scan connected sources and tag items to teams/purposes
- **AI analysis** — sprint concerns, goal alignment, and workload summaries via Claude (cached by input hash)
- **Annotations** — per-item and per-team notes that persist across sprints
- **Config TUI** — manage teams, members, sources, annotations, and users from the terminal

## Prerequisites

- Go 1.21+
- GitHub personal access token (for GitHub connector)
- Notion integration token (for Notion connector)
- Anthropic API key **or** `claude` CLI binary (for AI analysis)
- Optional: Grafana, PostHog, SigNoz tokens for metrics

## Setup

### 1. Configure credentials

Create a `.env` file in the project root:

```env
GITHUB_TOKEN=ghp_...
NOTION_TOKEN=secret_...

# Optional metrics sources
GRAFANA_TOKEN=...
GRAFANA_BASE_URL=https://your-grafana.example.com
POSTHOG_API_KEY=phc_...
POSTHOG_HOST=https://app.posthog.com
SIGNOZ_API_KEY=...
SIGNOZ_BASE_URL=https://your-signoz.example.com
```

### 2. Configure the server

Copy and edit the example config:

```bash
cp config.example.yaml config.yaml
```

Edit `config.yaml`:

```yaml
server:
  port: 8080

storage:
  path: ./dashboard.db

auth:
  jwt_secret: <random string>
  admin_username: admin
  admin_password_hash: ""   # see below

ai:
  provider: anthropic        # or: claude-code
  model: claude-opus-4-6
  api_key: ""                # required for anthropic provider
  binary_path: claude        # required for claude-code provider
```

**AI providers:**
- `anthropic` — direct API calls using `api_key`
- `claude-code` — invokes the `claude` CLI subprocess (no API key needed)

### 3. Set the admin password

```bash
go run ./cmd/server hash-password
# Enter password at prompt, then paste the output into config.yaml auth.admin_password_hash
```

### 4. Build and run

```bash
# Build both binaries
go build -o bin/dashboard-server ./cmd/server
go build -o bin/dashboard-tui ./cmd/tui

# Start the server
./bin/dashboard-server -config config.yaml

# In another terminal, start the TUI
./bin/dashboard-tui
```

Or run directly without building:

```bash
go run ./cmd/server -config config.yaml
go run ./cmd/tui
```

## First Run

1. Log in with your admin credentials in the TUI login screen.
2. Go to **Config > Teams** and create your teams and add members.
3. Go to **Config > Sources** and run **Discover** to scan your GitHub repos and Notion pages.
4. Tag discovered sources to teams and purposes.
5. Trigger a **Sync** to pull the latest data.
6. Browse the org overview and team dashboards.

## User Management

Users are managed by admins in the **Config > Users** TUI section. Two roles are available:

- `view` — read-only access to all dashboards
- `edit` — full access including sync, config, and annotations

There is no self-registration.

## Data Sources

| Source   | What it provides                        | Env vars                              |
|----------|-----------------------------------------|---------------------------------------|
| GitHub   | Issues, PRs, sprint workload            | `GITHUB_TOKEN`                        |
| Notion   | Sprint docs, goals, team annotations    | `NOTION_TOKEN`                        |
| Grafana  | Infrastructure/product metrics          | `GRAFANA_TOKEN`, `GRAFANA_BASE_URL`   |
| PostHog  | Product analytics                       | `POSTHOG_API_KEY`, `POSTHOG_HOST`     |
| SigNoz   | Observability metrics                   | `SIGNOZ_API_KEY`, `SIGNOZ_BASE_URL`   |

## Project Structure

```
cmd/
  server/       main entry point for the API server
  tui/          main entry point for the terminal UI
internal/
  api/          HTTP handlers and router
  ai/           AI generator interface (Anthropic + Claude Code providers)
  auth/         JWT, bcrypt, bootstrap
  config/       YAML config and .env loading
  connector/    External source clients (GitHub, Notion, Grafana, PostHog, SigNoz)
  pipeline/     AI analysis pipelines (concerns, goals, workload, velocity, alignment)
  store/        SQLite store and migrations
  sync/         Sync engine and auto-tag logic
  tui/          Bubble Tea app, views, and HTTP client
```

## Authentication

- JWT access tokens (1-hour expiry) + refresh tokens (30-day expiry)
- Bcrypt password hashing
- All API endpoints except `/auth/login` and `/auth/refresh` require a valid JWT

## Development

```bash
# Run tests
go test ./...

# Run with race detector
go test -race ./...
```
