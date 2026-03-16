# Web Frontend Plan

## Overview

A server-side rendered web frontend for the dashboard, providing feature parity with the TUI.
Built with Go `html/template` + htmx. Served at `/` from the existing server binary. The existing
JSON API moves to `/api/` prefix. No new runtime dependencies.

---

## Architecture

```
Browser ──► Go HTTP server
              ├── /api/*      existing JSON API (unchanged except prefix)
              ├── /static/*   embedded CSS + htmx.min.js
              └── /*          web frontend (html/template + htmx)
```

The web handlers run in the same process as the API. For data, they call the JSON API via
HTTP loopback (`http://localhost:{port}/api/...`), forwarding the user's JWT as a Bearer token.
This avoids any code duplication and keeps the API as the single source of truth.

**Auth:** httpOnly cookies (`_dash_tok` JWT, `_dash_ref` refresh token). Middleware validates
JWT locally using the same secret; on expiry, transparently refreshes using the refresh token cookie.

---

## File Structure

```
internal/web/
  handler.go               Deps struct, NewRouter, go:embed declarations
  middleware.go            RequireWebAuth (cookie → context), role helpers
  auth.go                  GET/POST /login, POST /logout
  api_client.go            Thin HTTP client: forwards JWT to /api/...
  helpers.go               Template funcs (formatDate, riskClass, workloadClass, etc.)
  org.go                   GET / — org overview
  team.go                  GET /teams/{id} — team dashboard
  config.go                All /config/* page handlers

  templates/
    base.html              Layout: topnav (team links, config, logout) + content block
    login.html             Login form
    org.html               Org overview: team cards + workload + goal alignment + calendar
    team.html              Team dashboard: 8 collapsible sections + sync button
    config_root.html       Config landing (links to sub-sections)
    config_sources.html    Source wiring: list, discover, classify, update, delete
    config_teams.html      Teams + members CRUD
    config_users.html      User management CRUD
    config_annotations.html Annotations CRUD (team-tier + item-tier)
    config_admin.html      Admin: autotag + clear AI cache
    partials/
      sync_status.html     htmx polling fragment for sync run status
      annotation_list.html htmx-swappable annotation list for a team section

  static/
    style.css              Dark dashboard theme (custom, no external deps)
    htmx.min.js            htmx 2.0.4 (embedded)
```

---

## Routing Table

| Method | Path | Handler | Role |
|--------|------|---------|------|
| GET | /login | LoginPage | public |
| POST | /login | LoginPost | public |
| POST | /logout | Logout | any |
| GET | / | OrgOverview | view |
| POST | /sync | PostSync | edit |
| GET | /sync/{id}/status | SyncStatus (htmx) | view |
| GET | /teams/{id} | TeamDashboard | view |
| POST | /teams/{id}/sync | PostTeamSync | edit |
| GET | /teams/{id}/annotations/form | AnnotationNewForm (htmx) | edit |
| POST | /annotations | CreateAnnotation (htmx) | edit |
| PUT | /annotations/{id} | UpdateAnnotation (htmx) | edit |
| DELETE | /annotations/{id} | DeleteAnnotation (htmx) | edit |
| GET | /config | ConfigRoot | view |
| GET | /config/sources | ConfigSources | view |
| POST | /config/sources/discover | ConfigSourcesDiscover (htmx) | edit |
| POST | /config/sources/classify | ConfigSourcesClassify (htmx) | edit |
| PUT | /config/sources/{id} | ConfigSourceUpdate (htmx) | edit |
| DELETE | /config/sources/{id}/config/{cid} | ConfigSourceDeleteConfig (htmx) | edit |
| GET | /config/teams | ConfigTeams | view |
| POST | /config/teams | ConfigTeamCreate (htmx) | edit |
| PUT | /config/teams/{id} | ConfigTeamUpdate (htmx) | edit |
| DELETE | /config/teams/{id} | ConfigTeamDelete (htmx) | edit |
| POST | /config/teams/{id}/members | ConfigMemberAdd (htmx) | edit |
| PUT | /config/members/{id} | ConfigMemberUpdate (htmx) | edit |
| DELETE | /config/members/{id} | ConfigMemberDelete (htmx) | edit |
| GET | /config/users | ConfigUsers | edit |
| POST | /config/users | ConfigUserCreate (htmx) | edit |
| PUT | /config/users/{id} | ConfigUserUpdate (htmx) | edit |
| DELETE | /config/users/{id} | ConfigUserDelete (htmx) | edit |
| GET | /config/annotations | ConfigAnnotations | view |
| POST | /config/annotations | ConfigAnnotationCreate (htmx) | edit |
| PUT | /config/annotations/{id} | ConfigAnnotationUpdate (htmx) | edit |
| DELETE | /config/annotations/{id} | ConfigAnnotationDelete (htmx) | edit |
| GET | /config/admin | ConfigAdmin | edit |
| POST | /config/admin/autotag | ConfigAdminAutotag (htmx) | edit |
| DELETE | /config/admin/ai-cache | ConfigAdminClearCache (htmx) | edit |

---

## Page Designs

### Org Overview (/)
- **Header bar**: "Org Overview" title | last-synced timestamp | "Sync Org" button
- **Team cards grid**: 2-3 columns, each card shows: name, sprint week N/M bar,
  risk badge (HIGH/MEDIUM/LOW/none), focus text (first business goal)
- **Cross-team workload**: table of members, estimated days, label badge, breakdown by team
- **Goal alignment**: rendered if data present — AI-generated alignment summary
- **Calendar**: 2-month grid with team initials as day markers, event list below

### Team Dashboard (/teams/{id})
- **Header bar**: team name | last-synced | "Sync Team" button | annotations mode toggle
- **8 collapsible sections** (all expanded by default, `<details>` element):
  1. **Sprint** — week position bar (N/M filled blocks), plan title link, sprint goals list,
     warnings (start date missing, next-plan risk)
  2. **Goals** — business goals with status icon (✓/✗/~), sprint forecast badge,
     sprint goals list, concerns accordion (severity-colored), inline annotations
  3. **Workload** — member table: name | est. days | label bar | label badge
  4. **Velocity** — CSS bar chart (one bar per sprint, labeled), breakdown table below
  5. **Metrics** — grid of metric panels (title + value)
  6. **Activity** — recent commits list, open issues table, merged PRs table
  7. **Marketing** — campaign cards with status, date range, tasks list
  8. **Calendar** — 2-month grid + event list (same as org but team-scoped)

### Config Sources (/config/sources)
- Filter bar (source type filter, search)
- Table: title | type | status | AI suggestion | configured-as | actions
- Actions per row: assign purpose+team (inline form), delete config
- "Discover" section at top: target input, scope selector, submit button → htmx polling
- "Classify" button on items with unreviewed AI suggestions

### Config Teams (/config/teams)
- "Add team" form at top
- Accordion per team: team name header (edit inline), members table, "Add member" form
- Member row: display name | github | notion | edit | delete
- Delete team: button with hx-confirm

### Config Users (/config/users)
- "Add user" form (username, password, role select)
- Users table: username | role | created | edit role | reset password | delete
- Edit role + reset password as inline expand forms

### Config Annotations (/config/annotations)
- Two sections: "Team annotations" (persist) | "Item annotations" (sprint-scoped)
- Each: sortable by team, show content + team name, edit/delete actions
- "Add annotation" form above each section

### Config Admin (/config/admin)
- AutoTag card: description + "Run AutoTag" button → htmx trigger + poll
- Clear AI Cache card: description + pipeline select (optional) + "Clear" button with confirm

---

## Auth Design

### Cookies
- `_dash_tok`: JWT (httpOnly, SameSite=Strict, MaxAge=3600)
- `_dash_ref`: Refresh token (httpOnly, SameSite=Strict, MaxAge=2592000)

### Middleware Flow
1. Read `_dash_tok` cookie → validate JWT locally with JWTSecret
2. If valid → set `username` + `role` in context → proceed
3. If expired → read `_dash_ref` cookie → POST /api/auth/refresh → update both cookies → proceed
4. If no valid auth → redirect to /login (preserve target URL in query param)

### Login Flow
1. GET /login → render login form (redirect target in hidden field)
2. POST /login → POST /api/auth/login → on success: set both cookies → redirect to target (or /)
3. POST /logout → clear both cookies → redirect to /login

---

## htmx Interaction Patterns

### Sync
```html
<button hx-post="/sync" hx-target="#sync-status" hx-swap="outerHTML">Sync Org</button>
<div id="sync-status"></div>

<!-- sync_status.html partial: returned by POST /sync and polled -->
<div id="sync-status"
     hx-get="/sync/{{.RunID}}/status"
     hx-trigger="every 2s"
     hx-swap="outerHTML">
  Syncing…
</div>

<!-- When done: returns a static div without hx-trigger (stops polling) -->
<div id="sync-status">Done — <a href="/">Reload</a></div>
```

### Annotations
```html
<!-- section footer -->
<button hx-get="/teams/1/annotations/form?section=goals"
        hx-target="#ann-goals" hx-swap="beforeend">+ Add note</button>
<div id="ann-goals">
  {{range .Goals.Annotations}}...{{end}}
</div>

<!-- form partial returned, submits to: -->
<form hx-post="/annotations" hx-target="#ann-goals" hx-swap="innerHTML">
  <input type="hidden" name="team_id" value="1">
  <input type="hidden" name="section" value="goals">
  <textarea name="content"></textarea>
  <button>Save</button>
</form>
```

### Config mutations (inline forms)
- Create: form at bottom of list → on submit returns updated list fragment
- Edit: "Edit" button → `hx-get` returns edit form inline → save returns updated row
- Delete: `hx-delete` + `hx-confirm="Are you sure?"` + `hx-target="closest tr"` + `hx-swap="outerHTML"`

---

## Visual Design

### Color Palette
```css
--bg:       #0f1117   /* page background */
--surface:  #1a1d27   /* cards, panels */
--surface2: #242736   /* table alt rows, inputs */
--border:   #2d3148   /* borders */
--text:     #e2e8f0   /* primary text */
--muted:    #64748b   /* secondary text, labels */
--cyan:     #22d3ee   /* accent, links, active */
--yellow:   #fbbf24   /* warnings, today */
--green:    #22c55e   /* success, low risk */
--red:      #ef4444   /* errors, high risk */
--amber:    #f59e0b   /* medium risk */
```

### Typography
- UI text: `system-ui, -apple-system, sans-serif`
- Data / code values: `ui-monospace, "JetBrains Mono", monospace`
- Base size: 14px

### Components
- **Card**: `background: var(--surface)`, `border: 1px solid var(--border)`, `border-radius: 6px`, `padding: 16px`
- **Badge**: inline pill, colored by semantic (risk, workload, status)
- **Table**: `width: 100%`, compact rows (28px), alternating row colors
- **Button**: primary (cyan fill), secondary (surface2 fill), ghost (transparent)
- **Nav**: fixed top bar, `background: #0a0c12` (darker than page bg)
- **Section header** (`<summary>` inside `<details>`): bold, border-bottom on open

---

## Changes to Existing Files

### cmd/server/main.go
- Import `internal/web`
- Build a top-level chi router: mount `/api` → api router, `/` → web router
- Pass `web.Deps{JWTSecret: ..., APIBase: "http://localhost:{port}/api"}` to web router

### internal/tui/client/client.go
- In `New()`: change `serverAddr: serverAddr` to `serverAddr: serverAddr + "/api"`
  so existing URL constructions (`c.serverAddr + "/org/overview"`) still resolve correctly

---

## Implementation Sequence

1. **Infrastructure**: handler.go, middleware.go, auth.go, api_client.go, helpers.go
2. **Static**: style.css, htmx.min.js (already downloaded)
3. **Base template**: base.html, login.html
4. **Org overview**: org.go + org.html
5. **Team dashboard**: team.go + team.html
6. **Config pages**: config.go + all config_*.html
7. **Partials**: sync_status.html, annotation_list.html
8. **Wire up**: update server/main.go + tui/client/client.go
9. **Compile & fix**
