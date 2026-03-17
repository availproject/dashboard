# Dashboard

A terminal-based product status dashboard for heads of product. Provides a real-time, AI-assisted view of team progress, workload, sprint health, and business metrics — aggregated from GitHub, Notion, Grafana, PostHog, and SigNoz.

## Architecture

Two binaries communicate over a local HTTP API:

- **`cmd/server`** — REST API server. Manages the SQLite database, syncs data from external sources, runs AI analysis pipelines, and serves all data endpoints.
- **`cmd/tui`** — Bubble Tea terminal UI. Connects to the server and renders the dashboard views.

```
cmd/server      →  :8081 (REST API + MCP endpoint)
cmd/tui         →  connects to localhost:8081
```

## Features

- **Org overview** — cross-team sprint health, workload, and AI-generated concerns
- **Team view** — sprint details, goals, workload by member (in work-days), velocity, and business metrics
- **On-demand sync** — pull latest data from all sources on demand
- **Discovery** — scan connected sources and tag items to teams/purposes
- **AI analysis** — sprint concerns, goal alignment, and workload summaries via Claude (cached by input hash)
- **Annotations** — per-item and per-team notes that persist across sprints
- **Config TUI** — manage teams, members, sources, annotations, and users from the terminal
- **MCP server** — expose all dashboard data to Claude Desktop or any MCP client via `/mcp`

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
  port: 8081

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

mcp:
  api_key: ""                # static Bearer token for /mcp; leave empty to disable auth
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

The project uses a `Makefile` for common tasks. Run `make help` to see all targets:

```
  build        Build the server binary
  run          Build and run locally (foreground, no pm2)
  deploy       Build and (re)start via pm2
  stop         Stop the pm2 process
  restart      Restart pm2 process without rebuilding
  logs         Tail pm2 logs
  status       Show pm2 status
  clean        Remove built binary
```

**Quick start (foreground):**

```bash
make run
# In another terminal, start the TUI
go run ./cmd/tui
```

**Persistent server via pm2:**

```bash
make deploy    # builds and starts (or restarts) the server under pm2
make logs      # tail logs
make status    # check process health
```

The pm2 config lives in `ecosystem.config.cjs`. The server auto-restarts on crash (up to 10 retries with a 3s delay). Logs go to `./logs/`.

## First Run

1. Log in with your admin credentials in the TUI login screen.
2. Go to **Config > Teams** and create your teams and add members.
3. Go to **Config > Sources** and run **Discover** to scan your GitHub repos and Notion pages.
4. Tag discovered sources to teams and purposes.
5. Trigger a **Sync** to pull the latest data.
6. Browse the org overview and team dashboards.

## MCP Server

The server exposes a [Model Context Protocol](https://modelcontextprotocol.io) endpoint at `/mcp` (Streamable HTTP transport). This lets Claude Desktop — or any MCP-compatible client — query your dashboard data conversationally.

### Available tools

| Tool | Args | Description |
|------|------|-------------|
| `list_teams` | — | All teams with IDs and member counts. Start here. |
| `get_org_snapshot` | — | Cross-team sprint progress, risk levels, workload, and goal alignment. |
| `get_team_status` | `team_id` | Full status for one team: sprint, goals, concerns, workload, velocity, metrics. |
| `get_team_members` | `team_id` | Roster with roles, GitHub usernames, and Notion user IDs. |
| `search_annotations` | `team_id?`, `query?` | Search manual annotations and flags across the system. |
| `get_sync_status` | `team_id?` | When data was last refreshed per team. |
| `trigger_sync` | `scope`, `team_id?` | Start a data refresh. Returns a `sync_run_id` to poll. |

### Connecting Claude Desktop

The server must be reachable from the machine running Claude Desktop. On the same machine, use `localhost`. Over a private network (e.g. Tailscale), use the hostname directly — HTTP is fine since Tailscale encrypts traffic at the network layer.

Add to `~/Library/Application Support/Claude/claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "dashboard": {
      "command": "npx",
      "args": [
        "mcp-remote",
        "http://<host>:8081/mcp",
        "--header",
        "Authorization:Bearer <your-mcp-api-key>",
        "--allow-http"
      ]
    }
  }
}
```

Replace `<host>` with `localhost` or your server's hostname (e.g. a Tailscale name), and `<your-mcp-api-key>` with the value from `config.yaml mcp.api_key`. The `--allow-http` flag is required for non-localhost URLs; omit it if connecting to localhost.

Restart Claude Desktop after editing the config. The tools appear in **Settings → Developer**.

### Authentication

Set `mcp.api_key` in `config.yaml` to a random string. The endpoint requires `Authorization: Bearer <key>` on every request. Leave the key empty to disable authentication (only appropriate on a fully trusted network).

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
  discover/     diagnostic CLI for inspecting discovered sources
internal/
  api/          HTTP handlers, router, and MCP server
  ai/           AI generator interface (Anthropic + Claude Code providers)
  auth/         JWT, bcrypt, bootstrap
  config/       YAML config and .env loading
  connector/    External source clients (GitHub, Notion, Grafana, PostHog, SigNoz)
  pipeline/     AI analysis pipelines (concerns, goals, workload, velocity, alignment, team status)
  store/        SQLite store and migrations
  sync/         Sync engine, discovery, classification, and homepage extraction
  tui/          Bubble Tea app, views, and HTTP client
```

## Authentication

- JWT access tokens (1-hour expiry) + refresh tokens (30-day expiry)
- Bcrypt password hashing
- All API endpoints except `/auth/login` and `/auth/refresh` require a valid JWT
- `/mcp` uses a separate static API key (see above)

## Development

```bash
# Run tests
go test ./...

# Run with race detector
go test -race ./...
```
