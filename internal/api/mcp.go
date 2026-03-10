package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/your-org/dashboard/internal/pipeline"
)

// mcpJSON marshals v to a JSON text result. Claude Desktop does not support
// structured_content yet, so we return plain text JSON for compatibility.
func mcpJSON(v any) (*mcp.CallToolResult, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return mcp.NewToolResultError("marshal result: " + err.Error()), nil
	}
	return mcp.NewToolResultText(string(b)), nil
}

// buildMCPHandler creates the MCP server and returns it as an http.Handler.
// All tools call the store directly — no internal HTTP round-trips.
func (d *Deps) buildMCPHandler() http.Handler {
	s := server.NewMCPServer(
		"dashboard",
		"1.0.0",
		server.WithToolCapabilities(false),
	)

	s.AddTool(toolListTeams(), d.mcpListTeams)
	s.AddTool(toolGetOrgSnapshot(), d.mcpGetOrgSnapshot)
	s.AddTool(toolGetTeamStatus(), d.mcpGetTeamStatus)
	s.AddTool(toolGetTeamMembers(), d.mcpGetTeamMembers)
	s.AddTool(toolSearchAnnotations(), d.mcpSearchAnnotations)
	s.AddTool(toolGetSyncStatus(), d.mcpGetSyncStatus)
	s.AddTool(toolTriggerSync(), d.mcpTriggerSync)

	httpSrv := server.NewStreamableHTTPServer(s)
	return mcpAPIKeyMiddleware(d.Config.MCP.APIKey, httpSrv)
}

// mcpAPIKeyMiddleware rejects requests without the correct Bearer token.
// If api_key is empty, the endpoint is open (useful for local dev with no key set).
func mcpAPIKeyMiddleware(apiKey string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if apiKey != "" {
			auth := r.Header.Get("Authorization")
			token := strings.TrimPrefix(auth, "Bearer ")
			if token != apiKey {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// ---------- tool definitions ----------

func toolListTeams() mcp.Tool {
	return mcp.NewTool("list_teams",
		mcp.WithDescription(
			"Returns all teams with their IDs, names, and member counts. "+
				"Start here — use the returned team IDs with get_team_status or get_team_members.",
		),
		mcp.WithReadOnlyHintAnnotation(true),
	)
}

func toolGetOrgSnapshot() mcp.Tool {
	return mcp.NewTool("get_org_snapshot",
		mcp.WithDescription(
			"Cross-team summary: sprint progress, risk level, and focus area for every team, "+
				"plus org-wide workload distribution and goal alignment. "+
				"No arguments required — good entry point when you want a full picture.",
		),
		mcp.WithReadOnlyHintAnnotation(true),
	)
}

func toolGetTeamStatus() mcp.Tool {
	return mcp.NewTool("get_team_status",
		mcp.WithDescription(
			"Full status for one team: sprint progress, business goals, sprint goals, concerns/blockers, "+
				"per-member workload, velocity history, and metrics panels. "+
				"Get team_id from list_teams or get_org_snapshot.",
		),
		mcp.WithNumber("team_id",
			mcp.Required(),
			mcp.Description("ID of the team. Get from list_teams."),
		),
		mcp.WithReadOnlyHintAnnotation(true),
	)
}

func toolGetTeamMembers() mcp.Tool {
	return mcp.NewTool("get_team_members",
		mcp.WithDescription(
			"Roster for one team: names, roles, GitHub usernames, and Notion user IDs. "+
				"Get team_id from list_teams.",
		),
		mcp.WithNumber("team_id",
			mcp.Required(),
			mcp.Description("ID of the team. Get from list_teams."),
		),
		mcp.WithReadOnlyHintAnnotation(true),
	)
}

func toolSearchAnnotations() mcp.Tool {
	return mcp.NewTool("search_annotations",
		mcp.WithDescription(
			"Search manual annotations and flags across the system. "+
				"Omit both arguments to return all active annotations. "+
				"Filter by team_id to scope to one team, or pass a query string for full-text search.",
		),
		mcp.WithNumber("team_id",
			mcp.Description("Optional team ID to filter by. Omit for all teams."),
		),
		mcp.WithString("query",
			mcp.Description("Optional substring to search in annotation content."),
		),
		mcp.WithReadOnlyHintAnnotation(true),
	)
}

func toolGetSyncStatus() mcp.Tool {
	return mcp.NewTool("get_sync_status",
		mcp.WithDescription(
			"Shows when data was last refreshed for each team and the org overall. "+
				"Pass team_id to focus on one team, or omit for all.",
		),
		mcp.WithNumber("team_id",
			mcp.Description("Optional team ID to filter by."),
		),
		mcp.WithReadOnlyHintAnnotation(true),
	)
}

func toolTriggerSync() mcp.Tool {
	return mcp.NewTool("trigger_sync",
		mcp.WithDescription(
			"Kicks off a data refresh from GitHub and Notion. "+
				"Use scope='team' with a team_id for one team, or scope='org' for all teams. "+
				"Returns a sync_run_id you can pass to get_sync_status to wait for completion.",
		),
		mcp.WithString("scope",
			mcp.Required(),
			mcp.Description("Either 'team' (one team) or 'org' (all teams)."),
		),
		mcp.WithNumber("team_id",
			mcp.Description("Required when scope is 'team'. Get from list_teams."),
		),
	)
}

// ---------- tool handlers ----------

type mcpTeamItem struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	MemberCount int    `json:"member_count"`
}

func (d *Deps) mcpListTeams(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	teams, err := d.Store.ListTeams(ctx)
	if err != nil {
		return mcp.NewToolResultError("list teams: " + err.Error()), nil
	}

	items := make([]mcpTeamItem, 0, len(teams))
	for _, t := range teams {
		members, _ := d.Store.GetTeamMembers(ctx, t.ID)
		items = append(items, mcpTeamItem{
			ID:          t.ID,
			Name:        t.Name,
			MemberCount: len(members),
		})
	}
	return mcpJSON(items)
}

func (d *Deps) mcpGetOrgSnapshot(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// Reuse the same logic as handleOrgOverview but return directly.
	teams, err := d.Store.ListTeams(ctx)
	if err != nil {
		return mcp.NewToolResultError("list teams: " + err.Error()), nil
	}

	type teamSnap struct {
		ID            int64   `json:"id"`
		Name          string  `json:"name"`
		CurrentSprint int     `json:"current_sprint"`
		TotalSprints  int     `json:"total_sprints"`
		RiskLevel     string  `json:"risk_level"`
		Focus         string  `json:"focus"`
		LastSyncedAt  *string `json:"last_synced_at"`
	}
	type memberWorkload struct {
		Name      string             `json:"name"`
		TotalDays float64            `json:"total_days"`
		Label     string             `json:"label"`
		Breakdown map[string]float64 `json:"breakdown"`
	}
	type orgSnap struct {
		Teams         []teamSnap              `json:"teams"`
		Workload      []memberWorkload         `json:"workload"`
		GoalAlignment *pipeline.AlignmentResult `json:"goal_alignment"`
		LastSyncedAt  *string                  `json:"last_synced_at"`
	}

	type memberAgg struct {
		totalDays float64
		breakdown map[string]float64
	}
	workloadAgg := map[string]*memberAgg{}
	teamSnaps := make([]teamSnap, 0, len(teams))

	for _, t := range teams {
		snap := teamSnap{ID: t.ID, Name: t.Name}
		tid := sql.NullInt64{Int64: t.ID, Valid: true}

		if c, err := d.Store.GetLatestCacheByPipeline(ctx, pipeline.SprintParsePipeline, tid); err == nil {
			var sp pipeline.SprintParseResult
			if json.Unmarshal([]byte(c.Output), &sp) == nil {
				snap.CurrentSprint = sp.CurrentSprint
				snap.TotalSprints = sp.TotalSprints
			}
		}
		if c, err := d.Store.GetLatestCacheByPipeline(ctx, pipeline.TeamStatusPipeline, tid); err == nil {
			var tsr pipeline.TeamStatusResult
			if json.Unmarshal([]byte(c.Output), &tsr) == nil {
				snap.RiskLevel = highestSeverity(tsr.Concerns)
				if len(tsr.BusinessGoals) > 0 {
					snap.Focus = tsr.BusinessGoals[0].Text
				}
			}
		}
		if run, err := d.Store.GetLastCompletedSyncRun(ctx, "team", tid); err == nil && run.FinishedAt.Valid {
			s := run.FinishedAt.Time.Format(time.RFC3339)
			snap.LastSyncedAt = &s
		}
		if c, err := d.Store.GetLatestCacheByPipeline(ctx, pipeline.WorkloadPipeline, tid); err == nil {
			var wr pipeline.WorkloadResult
			if json.Unmarshal([]byte(c.Output), &wr) == nil {
				for _, m := range wr.Members {
					if workloadAgg[m.Name] == nil {
						workloadAgg[m.Name] = &memberAgg{breakdown: map[string]float64{}}
					}
					workloadAgg[m.Name].totalDays += m.EstimatedDays
					workloadAgg[m.Name].breakdown[t.Name] += m.EstimatedDays
				}
			}
		}
		teamSnaps = append(teamSnaps, snap)
	}

	wl := make([]memberWorkload, 0, len(workloadAgg))
	for name, agg := range workloadAgg {
		wl = append(wl, memberWorkload{
			Name:      name,
			TotalDays: agg.totalDays,
			Label:     workloadLabel(agg.totalDays),
			Breakdown: agg.breakdown,
		})
	}

	var goalAlignment *pipeline.AlignmentResult
	if c, err := d.Store.GetLatestCacheByPipeline(ctx, pipeline.AlignmentPipeline, sql.NullInt64{}); err == nil {
		var ar pipeline.AlignmentResult
		if json.Unmarshal([]byte(c.Output), &ar) == nil {
			goalAlignment = &ar
		}
	}

	var lastSyncedAt *string
	if run, err := d.Store.GetLastCompletedSyncRun(ctx, "org", sql.NullInt64{}); err == nil && run.FinishedAt.Valid {
		s := run.FinishedAt.Time.Format(time.RFC3339)
		lastSyncedAt = &s
	}

	return mcpJSON(orgSnap{
		Teams:         teamSnaps,
		Workload:      wl,
		GoalAlignment: goalAlignment,
		LastSyncedAt:  lastSyncedAt,
	})
}

type mcpTeamStatus struct {
	Sprint    mcpSprintStatus      `json:"sprint"`
	Goals     mcpGoals             `json:"goals"`
	Workload  []mcpWorkloadMember  `json:"workload"`
	Velocity  []mcpVelocitySprint  `json:"velocity"`
	Metrics   []mcpMetricsPanel    `json:"metrics"`
	LastSyncedAt *string           `json:"last_synced_at"`
}

type mcpSprintStatus struct {
	PlanType          string   `json:"plan_type"`
	CurrentSprint     int      `json:"current_sprint"`
	TotalSprints      int      `json:"total_sprints"`
	StartDate         *string  `json:"start_date"`
	Goals             []string `json:"goals"`
	StartDateMissing  bool     `json:"start_date_missing"`
	NextPlanStartRisk bool     `json:"next_plan_start_risk"`
}

type mcpGoals struct {
	BusinessGoals  []teamBusinessGoalItem `json:"business_goals"`
	SprintGoals    []teamSprintGoalItem   `json:"sprint_goals"`
	SprintForecast string                 `json:"sprint_forecast"`
	Concerns       []teamConcernItem      `json:"concerns"`
}

type mcpWorkloadMember struct {
	Name          string  `json:"name"`
	EstimatedDays float64 `json:"estimated_days"`
	Label         string  `json:"label"`
}

type mcpVelocitySprint struct {
	Label string                `json:"label"`
	Score float64               `json:"score"`
	Breakdown teamVelocityBreakdown `json:"breakdown"`
}

type mcpMetricsPanel struct {
	Title   string  `json:"title"`
	Value   *string `json:"value"`
	PanelID string  `json:"panel_id"`
}

func (d *Deps) mcpGetTeamStatus(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	teamID, err := req.RequireInt("team_id")
	if err != nil {
		return mcp.NewToolResultError("team_id is required"), nil
	}
	tid := sql.NullInt64{Int64: int64(teamID), Valid: true}

	var status mcpTeamStatus

	// Sprint
	status.Sprint.PlanType = "current"
	status.Sprint.Goals = []string{}
	status.Sprint.StartDateMissing = true
	if sm, err := d.Store.GetSprintMeta(ctx, int64(teamID), "current"); err == nil {
		status.Sprint.PlanType = sm.PlanType
		if sm.StartDate.Valid {
			status.Sprint.StartDate = &sm.StartDate.String
			status.Sprint.StartDateMissing = false
		}
	}
	if c, err := d.Store.GetLatestCacheByPipeline(ctx, pipeline.SprintParsePipeline, tid); err == nil {
		var sp pipeline.SprintParseResult
		if json.Unmarshal([]byte(c.Output), &sp) == nil {
			status.Sprint.TotalSprints = sp.TotalSprints
			if sp.Goals != nil {
				status.Sprint.Goals = sp.Goals
			}
			status.Sprint.CurrentSprint = sp.CurrentSprint
			if sp.CurrentSprint > 0 {
				status.Sprint.StartDateMissing = false
			}
		}
	}
	if status.Sprint.TotalSprints > 4 {
		if configs, err := d.Store.GetSourceConfigsForScope(ctx, tid); err == nil {
			for _, sc := range configs {
				if sc.Purpose == "next_plan" {
					status.Sprint.NextPlanStartRisk = true
					break
				}
			}
		}
	}

	// Goals and concerns
	status.Goals.BusinessGoals = []teamBusinessGoalItem{}
	status.Goals.SprintGoals = []teamSprintGoalItem{}
	status.Goals.Concerns = []teamConcernItem{}
	if c, err := d.Store.GetLatestCacheByPipeline(ctx, pipeline.TeamStatusPipeline, tid); err == nil {
		var ts pipeline.TeamStatusResult
		if json.Unmarshal([]byte(c.Output), &ts) == nil {
			for _, g := range ts.BusinessGoals {
				status.Goals.BusinessGoals = append(status.Goals.BusinessGoals, teamBusinessGoalItem{
					Text: g.Text, Status: g.Status, Note: g.Note,
				})
			}
			for _, g := range ts.SprintGoals {
				status.Goals.SprintGoals = append(status.Goals.SprintGoals, teamSprintGoalItem{
					Text: g.Text, Status: g.Status, Note: g.Note,
				})
			}
			status.Goals.SprintForecast = ts.SprintForecast
			for _, c := range ts.Concerns {
				status.Goals.Concerns = append(status.Goals.Concerns, teamConcernItem{
					Key: c.Key, Summary: c.Summary,
					Explanation: c.Explanation, Severity: c.Severity, Scope: c.Scope,
				})
			}
		}
	}

	// Workload
	status.Workload = []mcpWorkloadMember{}
	if c, err := d.Store.GetLatestCacheByPipeline(ctx, pipeline.WorkloadPipeline, tid); err == nil {
		var wr pipeline.WorkloadResult
		if json.Unmarshal([]byte(c.Output), &wr) == nil {
			for _, m := range wr.Members {
				status.Workload = append(status.Workload, mcpWorkloadMember{
					Name: m.Name, EstimatedDays: m.EstimatedDays, Label: m.Label,
				})
			}
		}
	}

	// Velocity
	status.Velocity = []mcpVelocitySprint{}
	if c, err := d.Store.GetLatestCacheByPipeline(ctx, pipeline.VelocityPipeline, tid); err == nil {
		var vr pipeline.VelocityResult
		if json.Unmarshal([]byte(c.Output), &vr) == nil {
			for _, s := range vr.Sprints {
				status.Velocity = append(status.Velocity, mcpVelocitySprint{
					Label: s.Label, Score: s.Score,
					Breakdown: teamVelocityBreakdown{Issues: s.Breakdown.Issues, PRs: s.Breakdown.PRs, Commits: s.Breakdown.Commits},
				})
			}
		}
	}

	// Metrics panels
	status.Metrics = []mcpMetricsPanel{}
	if configs, err := d.Store.GetSourceConfigsForScope(ctx, tid); err == nil {
		for _, sc := range configs {
			if sc.Purpose != "metrics_panel" {
				continue
			}
			item, err := d.Store.GetCatalogueItem(ctx, sc.CatalogueID)
			if err != nil {
				continue
			}
			panel := mcpMetricsPanel{Title: item.Title}
			if item.SourceMeta.Valid {
				var meta map[string]any
				if json.Unmarshal([]byte(item.SourceMeta.String), &meta) == nil {
					if pid, ok := meta["panel_id"].(string); ok {
						panel.PanelID = pid
					}
					if val, ok := meta["value"].(string); ok {
						panel.Value = &val
					}
				}
			}
			status.Metrics = append(status.Metrics, panel)
		}
	}

	// Last synced
	if run, err := d.Store.GetLastCompletedSyncRun(ctx, "team", tid); err == nil && run.FinishedAt.Valid {
		s := run.FinishedAt.Time.Format(time.RFC3339)
		status.LastSyncedAt = &s
	}

	return mcpJSON(status)
}

type mcpMember struct {
	ID             int64   `json:"id"`
	Name           string  `json:"name"`
	Role           *string `json:"role"`
	GithubUsername *string `json:"github_username"`
	NotionUserID   *string `json:"notion_user_id"`
}

func (d *Deps) mcpGetTeamMembers(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	teamID, err := req.RequireInt("team_id")
	if err != nil {
		return mcp.NewToolResultError("team_id is required"), nil
	}

	members, err := d.Store.GetTeamMembers(ctx, int64(teamID))
	if err != nil {
		return mcp.NewToolResultError("get members: " + err.Error()), nil
	}

	items := make([]mcpMember, 0, len(members))
	for _, m := range members {
		item := mcpMember{ID: m.ID, Name: m.Name}
		if m.Role.Valid {
			item.Role = &m.Role.String
		}
		if m.GithubLogin.Valid {
			item.GithubUsername = &m.GithubLogin.String
		}
		if m.NotionUserID.Valid {
			item.NotionUserID = &m.NotionUserID.String
		}
		items = append(items, item)
	}
	return mcpJSON(items)
}

type mcpAnnotation struct {
	ID        int64   `json:"id"`
	TeamID    *int64  `json:"team_id"`
	ItemRef   *string `json:"item_ref"`
	Tier      string  `json:"tier"`
	Content   string  `json:"content"`
	CreatedAt string  `json:"created_at"`
}

func (d *Deps) mcpSearchAnnotations(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	teamIDArg := req.GetInt("team_id", 0)
	query := req.GetString("query", "")

	var tid sql.NullInt64
	if teamIDArg != 0 {
		tid = sql.NullInt64{Int64: int64(teamIDArg), Valid: true}
	}

	annotations, err := d.Store.ListAnnotations(ctx, tid)
	if err != nil {
		return mcp.NewToolResultError("list annotations: " + err.Error()), nil
	}

	items := make([]mcpAnnotation, 0)
	for _, a := range annotations {
		if a.Archived != 0 {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(a.Content), strings.ToLower(query)) {
			continue
		}
		item := mcpAnnotation{
			ID:        a.ID,
			Tier:      a.Tier,
			Content:   a.Content,
			CreatedAt: a.CreatedAt.Format(time.RFC3339),
		}
		if a.TeamID.Valid {
			item.TeamID = &a.TeamID.Int64
		}
		if a.ItemRef.Valid {
			item.ItemRef = &a.ItemRef.String
		}
		items = append(items, item)
	}
	return mcpJSON(items)
}

type mcpSyncEntry struct {
	TeamID       *int64  `json:"team_id"`
	TeamName     *string `json:"team_name"`
	Scope        string  `json:"scope"`
	LastSyncedAt *string `json:"last_synced_at"`
	Status       string  `json:"status"`
}

func (d *Deps) mcpGetSyncStatus(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	teamIDArg := req.GetInt("team_id", 0)

	teams, err := d.Store.ListTeams(ctx)
	if err != nil {
		return mcp.NewToolResultError("list teams: " + err.Error()), nil
	}

	teamNames := map[int64]string{}
	for _, t := range teams {
		teamNames[t.ID] = t.Name
	}

	entries := make([]mcpSyncEntry, 0)

	addEntry := func(scope string, tid sql.NullInt64) {
		entry := mcpSyncEntry{Scope: scope}
		if tid.Valid {
			entry.TeamID = &tid.Int64
			if name, ok := teamNames[tid.Int64]; ok {
				entry.TeamName = &name
			}
		}
		if run, err := d.Store.GetLastCompletedSyncRun(ctx, scope, tid); err == nil {
			entry.Status = run.Status
			if run.FinishedAt.Valid {
				s := run.FinishedAt.Time.Format(time.RFC3339)
				entry.LastSyncedAt = &s
			}
		} else {
			entry.Status = "never"
		}
		// Also check if a sync is currently running
		if _, err := d.Store.GetRunningSyncRun(ctx, scope, tid); err == nil {
			entry.Status = "running"
		}
		entries = append(entries, entry)
	}

	if teamIDArg != 0 {
		addEntry("team", sql.NullInt64{Int64: int64(teamIDArg), Valid: true})
	} else {
		for _, t := range teams {
			addEntry("team", sql.NullInt64{Int64: t.ID, Valid: true})
		}
		addEntry("org", sql.NullInt64{})
	}

	return mcpJSON(entries)
}

func (d *Deps) mcpTriggerSync(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	scope, err := req.RequireString("scope")
	if err != nil || (scope != "team" && scope != "org") {
		return mcp.NewToolResultError("scope must be 'team' or 'org'"), nil
	}

	var teamID *int64
	if scope == "team" {
		tid := req.GetInt("team_id", 0)
		if tid == 0 {
			return mcp.NewToolResultError("team_id is required when scope is 'team'"), nil
		}
		id := int64(tid)
		teamID = &id
	}

	if d.Engine == nil {
		return mcp.NewToolResultError("sync engine not configured"), nil
	}

	// Check for already-running sync
	var nullTeamID sql.NullInt64
	if teamID != nil {
		nullTeamID = sql.NullInt64{Int64: *teamID, Valid: true}
	}
	if _, err := d.Store.GetRunningSyncRun(ctx, scope, nullTeamID); err == nil {
		return mcp.NewToolResultError("sync already running for this scope"), nil
	}

	runID, err := d.Engine.Sync(ctx, scope, teamID)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("trigger sync: %s", err)), nil
	}

	type result struct {
		SyncRunID int64  `json:"sync_run_id"`
		Note      string `json:"note"`
	}
	return mcpJSON(result{
		SyncRunID: runID,
		Note:      "Sync started. Poll get_sync_status to wait for completion.",
	})
}
