# Product Status Dashboard — Specification

## Overview

A product status dashboard for a startup head of product managing ~4 teams and 25–50 people. The dashboard provides a real-time, AI-assisted view of team progress, resource allocation, risks, and business metrics — with a feedback loop that lets the user refine the AI's understanding over time. The initial interface is a TUI (terminal UI); a web frontend may follow.

---

## Users & Access

- **Intended users:** The head of product and team leads.
- **Two access levels:**
  - **Edit:** Can configure sources, provide annotations and context, dismiss/snooze AI concerns.
  - **View:** Read-only access to the dashboard.
- No per-team access control. Access level is global.

---

## Org Structure Assumptions

- ~4 teams, 25–50 people total.
- Employees contribute to multiple teams simultaneously.
- Each team runs its own 4-week plan broken into 1-week sprints.
- Teams may add a 5th (or more) sprint to complete original plan goals — the tool must handle this gracefully and flag downstream schedule risk when it occurs.
- The tool tracks the **current** and **next** 4-week plan per team. Previous plans are archived in Notion and ignored by the tool.

---

## Data Sources

### GitHub
- Issues and GitHub Projects for sprint tasks (issues are tagged with product area labels).
- Pull requests and commits for developer activity and velocity.
- Some documentation (Markdown files in repos, e.g., sprint plans).

### Notion
- Goals, sprint plans, and other documents — mostly unstructured text, not databases.
- Exception: marketing uses a Notion database where rows are campaigns/sprints with task sub-documents.
- Org-level goals and milestones will live in a dedicated Notion page.
- Next 4-week plans are separate documents from current plans.

### Business Metrics
- **SigNoz**, **PostHog**, **Grafana** — flexible per team/product.
- Configuration: user pastes a dashboard URL, selects panels to display. The tool should enumerate available panels from the URL.
- API credentials for these services are configured via `.env` file on the server — not stored in the database or managed through the TUI.

### Future / Not Yet Scoped
- Inter-team dependencies are not explicitly tracked anywhere and are deprioritized for now.
- Velocity metrics for non-developer teams (e.g., marketing) are deprioritized unless a clear signal exists.

---

## Discovery & Configuration

### Discovery Step
Before regular sync, a one-time (or on-demand) discovery pass analyzes each configured source:
- For a Notion workspace: enumerates pages and databases.
- For a GitHub repo: enumerates issues, projects, Markdown files, and labels.
- For Grafana/PostHog/SigNoz: enumerates dashboard panels from a pasted URL.

Discovered items are catalogued and presented in the **Configuration** section of the TUI, where the user tags each item with its purpose (e.g., "Team A's current sprint plan", "Org-level goals", "Team B's Notion task database"). This is a persistent config section — not a one-time wizard.

Re-running discovery merges new findings into the existing config — it does not reset it. Previously tagged items retain their tags; previously ignored items remain ignored. Newly discovered items are analyzed by AI to infer a suggested purpose and are surfaced for the user to confirm or override.

### Configuration Sections
Everything is configurable; nothing is hardcoded. Config lives in a dedicated TUI section with subsections:

1. **Teams** — team names, members, GitHub usernames, Notion workspace links, relevant repos, relevant Notion databases.
2. **Org-level** — org goals document location, org milestone document location.
3. **Sources per team** — which discovered documents/databases serve which dashboard section (e.g., document A → goals input, document B → sprint plan input). Which dashboard sections are enabled per team.
4. **Business metrics** — per-team panel selections from SigNoz/PostHog/Grafana.
5. **Annotations** — aggregated view of all user-provided context/annotations (see below). Edit or remove from this single location.

---

## Sync & Caching

### On-Demand Sync
- No scheduled background sync. Sync is triggered manually.
- Sync can target a single team or the full org.
- The dashboard always shows the latest cached state immediately on load, with a timestamp.
- While a sync is running, a prominent banner (or modal) indicates sync in progress.

### Incremental Sync
After initial discovery and configuration, regular syncs are fast and limited to configured sources:
- Only fetch documents and data sources that have been tagged for use.
- For Notion: only check configured documents for changes.
- For GitHub: use the issues search API with configured labels/project filters to fetch only relevant issues incrementally.
- For business metrics: pull only configured panels.

### AI Processing Cache
AI outputs are cached per input hash. The same inputs (source content + annotations) should not trigger re-processing. Cache is invalidated when source content changes or annotations are modified.

---

## Dashboard Structure & Navigation

The TUI uses a **drill-down** navigation model:

```
Org Overview
├── Team A
│   ├── Sprint & Plan Status
│   ├── Goals & Concerns
│   ├── Resource / Workload
│   ├── Velocity
│   └── Business Metrics
├── Team B
│   └── ...
├── ...
└── Config  ← press `c` from Org Overview
    ├── Teams & Members
    ├── Sources
    ├── Business Metrics
    └── Annotations
```

From the Org Overview, press `c` to open Config. Press `Esc` or `Backspace` to go back at any level.

---

## Team-Level Dashboard Sections

### 1. Sprint & Plan Status
- Current 4-week plan: week N of M (M may exceed 4 if sprints were added).
- The current sprint week is derived from the start date specified in the Notion sprint plan document. If no start date is found, the tool displays a warning and prompts the user to add it to the document or annotate it in Config.
- Current sprint goals and overall plan goals.
- On-track vs. at-risk items, with AI-generated explanations for any at-risk flags.
- Awareness of the **next** 4-week plan document; flags if current plan delay pushes out the next plan's start.

### 2. Goals & Concerns
- AI-extracted goals from the configured sprint plan and goals documents.
- Concerns flagged by AI: blocked items, resource mismatches, plan omissions, potential risks.
- Each concern includes a plain-language explanation of why it was flagged.
- User can inline-annotate any item (goal or concern): dismiss, snooze, or add clarifying context.
- Annotations are passed to the AI on subsequent syncs with an explicit instruction to re-evaluate in light of the context. The same concern should not resurface if the annotation addresses it.

### 3. Resource & Workload
- Estimated workload per team member for the current and upcoming sprint.
- Derived entirely from signals: open/assigned GitHub issues, future-sprint tasks, PRs, commit activity.
- Output format: estimated work-days per person for the sprint window (e.g., "Alice: 3.5 days") plus a qualitative label (high/normal/low), where a standard sprint = 5 work-days.
- Team-level view shows only work assigned through that team's sources (GitHub project, Notion tasks).
- Org-level view aggregates across all teams (e.g., "Alice: 5 days total across Team A and Team B").
- Flags overloaded contributors, including projected overload in future sprints based on assigned tasks.
- This is a planning tool, not a performance evaluation tool.

### 4. Velocity (Developers)
- Per-sprint velocity estimated from GitHub signals: issues closed, PRs merged, commits — each weighted by estimated difficulty.
- AI estimates task/PR/commit difficulty to normalize effort across items.
- Trend chart across recent sprints.
- Used to inform capacity planning for future sprints.
- For non-developer teams: velocity is out of scope for the initial build.

### 5. Business Metrics
- Panels pulled from configured SigNoz/PostHog/Grafana dashboards.
- Flexible per team — no fixed metric set.
- Summarized inline; raw numbers from configured panels.

---

## Org-Level Dashboard

### Team Status Summary
- One card per team: current focus, next milestone, risk summary.
- Flags any team needing attention.

### Employee Workload Overview
- Aggregate view of each employee's load across all teams.
- Same estimation logic as team-level workload, but cross-team.
- Flags anyone overloaded now or projected to be overloaded in upcoming sprints.

### Org Goals & Milestones
- Sourced from a dedicated Notion page.
- AI assesses directional alignment between org-level goals and team-level goals/sprint plans.
- Flags misalignment (e.g., a team's work does not appear to contribute to any org goal).

### Aggregate Metrics
- Aggregated business metrics across teams.
- Aggregated velocity trends.

---

## AI Reasoning & Feedback Loop

### Principles
- AI is used for: extracting goals/status from unstructured text, generating concerns with explanations, estimating task difficulty, assessing workload, and flagging goal misalignment.
- AI outputs are deterministic given fixed inputs + annotations (cached by input hash to avoid re-processing and output drift).
- Every AI-generated flag includes a plain-language explanation so the user can evaluate it.

### Annotation System
The primary feedback mechanism. Users can annotate any AI-generated item inline in the TUI:
- **Dismiss with context:** "This is not at risk because X" — passed to the AI as additional context on next sync, preventing the same flag from re-appearing.
- **Add note:** Free-text context to guide the AI.
All annotations are aggregated in a single **Annotations** section in Config, where they can be reviewed, edited, or deleted. This prevents unbounded growth of hidden state.

When the underlying situation changes (e.g., a blocked dependency resolves), the user is expected to manually clean up relevant annotations. Optionally, the AI may flag that an annotation appears stale given new evidence.

### Override & Convergence Workflow
1. User loads dashboard, sees AI assessment.
2. If assessment seems wrong, user adds an annotation with correcting context.
3. User triggers a sync (team or full org).
4. AI re-evaluates with the annotation as additional input, guided by explicit prompt instructions to double-check against user-provided context.
5. Repeat until the user's understanding matches the dashboard output.

---

## Sprint Plan Lifecycle

- **Current plan:** Actively tracked. Tool knows which sprint week is current.
- **Next plan:** Exists as a separate Notion document. User tags it in Config as "next plan." Tool is aware of it to flag schedule risks from current-plan delays.
- **Rollover:** When the next plan becomes current, user updates the Config to re-tag the documents. No automated rollover.
- **Task lifecycle:** GitHub issues are always closed at the end of each sprint as Done, Not Completed, or Won't Do. Issues are never rolled forward. If a task is still wanted in the next sprint, a new issue is created (possibly with updated scope). The tool assumes this discipline and does not attempt to track carried-over tasks.
- **Previous plans:** Not tracked by the tool. Archived in Notion.

### Annotation Lifecycle

Annotations come in two tiers with different lifespans:

- **Item-level annotations** — tied to a specific goal, task, or concern (e.g., "this feature delay is fine because we already have a workaround"). Only relevant while that item exists. At plan rollover, these are automatically archived with the old plan: no longer sent to the AI, but still viewable as history in the Annotations config section.

- **Team-level / general annotations** — observations about a team or its patterns that are not tied to any specific item (e.g., "this team consistently underestimates mobile work", "Alice is on leave through April"). These survive sprint boundaries and persist unchanged into the new plan.

When annotating any item, the user selects the tier (default: item-level, with an option to promote to team-level). In the Annotations config section, the two tiers are displayed in separate groups so it is always clear what standing guidance the AI is operating under. The user is expected to periodically review and prune team-level annotations as circumstances change.

---

## Backend Architecture (High-Level)

- A backend server exposes an API consumed by both the TUI and (eventually) a web frontend.
- The backend handles:
  - Source connectors (Notion, GitHub, SigNoz, PostHog, Grafana).
  - Discovery and incremental sync orchestration.
  - AI pipeline execution and caching.
  - Persistent state: configuration, catalogued sources, annotations, cached AI outputs.
- Auth system with two roles: **view** and **edit**. Only registered users can access data.
- Single-tenant deployment (one org per instance). User accounts are managed by an admin through the Config section of the TUI. Credentials are stored as hashed passwords in the same database used for caching. No self-registration flow.

---

## Out of Scope (for Now)

- Inter-team dependency tracking (implicit or AI-inferred).
- Proactive notifications (the dashboard is passive; you come to it).
- Fine-grained per-team access control.
- Velocity for non-developer teams (future phase; marketing Notion database is a candidate signal).
- Backward-looking performance analysis.
