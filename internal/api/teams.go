package api

import (
	"database/sql"
	"encoding/json"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/your-org/dashboard/internal/pipeline"
)

type listTeamsMemberItem struct {
	ID             int64   `json:"id"`
	DisplayName    string  `json:"display_name"`
	GithubUsername *string `json:"github_username"`
	NotionUserID   *string `json:"notion_user_id"`
}

type listTeamsItem struct {
	ID      int64                 `json:"id"`
	Name    string                `json:"name"`
	Members []listTeamsMemberItem `json:"members"`
}

func (d *Deps) handleListTeams(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	teams, err := d.Store.ListTeams(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list teams: "+err.Error())
		return
	}

	result := make([]listTeamsItem, 0, len(teams))
	for _, t := range teams {
		members, err := d.Store.GetTeamMembers(ctx, t.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "get members: "+err.Error())
			return
		}

		memberItems := make([]listTeamsMemberItem, 0, len(members))
		for _, m := range members {
			item := listTeamsMemberItem{
				ID:          m.ID,
				DisplayName: m.Name,
			}
			if m.GithubLogin.Valid {
				item.GithubUsername = &m.GithubLogin.String
			}
			if m.NotionUserID.Valid {
				item.NotionUserID = &m.NotionUserID.String
			}
			memberItems = append(memberItems, item)
		}

		result = append(result, listTeamsItem{
			ID:      t.ID,
			Name:    t.Name,
			Members: memberItems,
		})
	}

	writeJSON(w, http.StatusOK, result)
}

// parseTeamID extracts and parses the {id} URL parameter.
func parseTeamID(r *http.Request) (int64, error) {
	return strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
}

// teamLastSyncedAt returns the last completed "team"-scope sync run's
// finished_at as an RFC3339 string, or nil if none exists.
func (d *Deps) teamLastSyncedAt(r *http.Request, teamID int64) *string {
	ctx := r.Context()
	run, err := d.Store.GetLastCompletedSyncRun(ctx, "team", sql.NullInt64{Int64: teamID, Valid: true})
	if err != nil || !run.FinishedAt.Valid {
		return nil
	}
	s := run.FinishedAt.Time.Format(time.RFC3339)
	return &s
}

// --- GET /teams/{id}/sprint ---

type teamSprintResponse struct {
	PlanType           string   `json:"plan_type"`
	StartDate          *string  `json:"start_date"`
	CurrentSprint      int      `json:"current_sprint"`
	TotalSprints       int      `json:"total_sprints"`
	StartDateMissing   bool     `json:"start_date_missing"`
	NextPlanStartRisk  bool     `json:"next_plan_start_risk"`
	Goals              []string `json:"goals"`
	LastSyncedAt       *string  `json:"last_synced_at"`
}

func (d *Deps) handleTeamSprint(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	teamID, err := parseTeamID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid team id")
		return
	}

	resp := teamSprintResponse{
		PlanType:         "current",
		Goals:            []string{},
		StartDateMissing: true, // default true; cleared when a valid start_date is found
	}

	// Load sprint_meta for plan_type="current"
	sprintMeta, err := d.Store.GetSprintMeta(ctx, teamID, "current")
	if err == nil {
		resp.PlanType = sprintMeta.PlanType
		if sprintMeta.StartDate.Valid {
			resp.StartDate = &sprintMeta.StartDate.String
			resp.StartDateMissing = false
		}
	}

	// Load sprint_parse pipeline cache for total_sprints, current_sprint, goals
	teamNullID := sql.NullInt64{Int64: teamID, Valid: true}
	sprintCache, err := d.Store.GetLatestCacheByPipeline(ctx, pipeline.SprintParsePipeline, teamNullID)
	if err == nil {
		var sp pipeline.SprintParseResult
		if json.Unmarshal([]byte(sprintCache.Output), &sp) == nil {
			resp.TotalSprints = sp.TotalSprints
			if sp.Goals != nil {
				resp.Goals = sp.Goals
			}

			// Compute current_sprint from start_date if available; fall back to AI count.
			if sp.StartDate != nil && *sp.StartDate != "" {
				t, parseErr := time.Parse("2006-01-02", *sp.StartDate)
				if parseErr == nil {
					daysElapsed := time.Since(t).Hours() / 24
					resp.CurrentSprint = int(math.Floor(daysElapsed/7)) + 1
					if resp.CurrentSprint < 1 {
						resp.CurrentSprint = 1
					}
					resp.StartDateMissing = false
				} else {
					resp.CurrentSprint = sp.CurrentSprint
				}
			} else {
				resp.CurrentSprint = sp.CurrentSprint
			}
			// If the AI successfully identified a sprint number, suppress the missing-date
			// warning — teams that track sprints by number don't need calendar dates.
			if sp.CurrentSprint > 0 {
				resp.StartDateMissing = false
			}
		}
	}

	// Check next_plan_start_risk: total_sprints > 4 AND next-plan source_config exists
	if resp.TotalSprints > 4 {
		configs, err := d.Store.GetSourceConfigsForScope(ctx, teamNullID)
		if err == nil {
			for _, sc := range configs {
				if sc.Purpose == "next_plan" {
					resp.NextPlanStartRisk = true
					break
				}
			}
		}
	}

	resp.LastSyncedAt = d.teamLastSyncedAt(r, teamID)

	writeJSON(w, http.StatusOK, resp)
}

// --- GET /teams/{id}/goals ---

type teamBusinessGoalItem struct {
	Text   string `json:"text"`
	Status string `json:"status"`
	Note   string `json:"note"`
}

type teamSprintGoalItem struct {
	Text   string `json:"text"`
	Status string `json:"status"`
	Note   string `json:"note"`
}

type teamConcernItem struct {
	Key          string  `json:"key"`
	Summary      string  `json:"summary"`
	Explanation  string  `json:"explanation"`
	Severity     string  `json:"severity"`
	Scope        string  `json:"scope"`
	AnnotationID *int64  `json:"annotation_id"`
}

type teamGoalsResponse struct {
	BusinessGoals  []teamBusinessGoalItem `json:"business_goals"`
	SprintGoals    []teamSprintGoalItem   `json:"sprint_goals"`
	SprintForecast string                 `json:"sprint_forecast"`
	Concerns       []teamConcernItem      `json:"concerns"`
	LastSyncedAt   *string                `json:"last_synced_at"`
}

func (d *Deps) handleTeamGoals(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	teamID, err := parseTeamID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid team id")
		return
	}

	resp := teamGoalsResponse{
		BusinessGoals: []teamBusinessGoalItem{},
		SprintGoals:   []teamSprintGoalItem{},
		Concerns:      []teamConcernItem{},
	}

	teamNullID := sql.NullInt64{Int64: teamID, Valid: true}

	// Load team_status cache
	cache, err := d.Store.GetLatestCacheByPipeline(ctx, pipeline.TeamStatusPipeline, teamNullID)
	if err == nil {
		var ts pipeline.TeamStatusResult
		if json.Unmarshal([]byte(cache.Output), &ts) == nil {
			for _, g := range ts.BusinessGoals {
				resp.BusinessGoals = append(resp.BusinessGoals, teamBusinessGoalItem{
					Text:   g.Text,
					Status: g.Status,
					Note:   g.Note,
				})
			}
			for _, g := range ts.SprintGoals {
				resp.SprintGoals = append(resp.SprintGoals, teamSprintGoalItem{
					Text:   g.Text,
					Status: g.Status,
					Note:   g.Note,
				})
			}
			resp.SprintForecast = ts.SprintForecast

			// Build item_ref → annotation_id map for concern linking
			annotations, _ := d.Store.ListAnnotations(ctx, teamNullID)
			annotationByRef := map[string]int64{}
			for _, a := range annotations {
				if a.ItemRef.Valid && a.Archived == 0 {
					annotationByRef[a.ItemRef.String] = a.ID
				}
			}
			for _, c := range ts.Concerns {
				item := teamConcernItem{
					Key:         c.Key,
					Summary:     c.Summary,
					Explanation: c.Explanation,
					Severity:    c.Severity,
					Scope:       c.Scope,
				}
				if id, ok := annotationByRef[c.Key]; ok {
					item.AnnotationID = &id
				}
				resp.Concerns = append(resp.Concerns, item)
			}
		}
	}

	resp.LastSyncedAt = d.teamLastSyncedAt(r, teamID)

	writeJSON(w, http.StatusOK, resp)
}

// --- GET /teams/{id}/workload ---

type teamWorkloadMember struct {
	Name          string  `json:"name"`
	EstimatedDays float64 `json:"estimated_days"`
	Label         string  `json:"label"`
}

type teamWorkloadResponse struct {
	Members      []teamWorkloadMember `json:"members"`
	LastSyncedAt *string              `json:"last_synced_at"`
}

func (d *Deps) handleTeamWorkload(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	teamID, err := parseTeamID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid team id")
		return
	}

	resp := teamWorkloadResponse{
		Members: []teamWorkloadMember{},
	}

	teamNullID := sql.NullInt64{Int64: teamID, Valid: true}
	cache, err := d.Store.GetLatestCacheByPipeline(ctx, pipeline.WorkloadPipeline, teamNullID)
	if err == nil {
		var wr pipeline.WorkloadResult
		if json.Unmarshal([]byte(cache.Output), &wr) == nil {
			for _, m := range wr.Members {
				resp.Members = append(resp.Members, teamWorkloadMember{
					Name:          m.Name,
					EstimatedDays: m.EstimatedDays,
					Label:         m.Label,
				})
			}
		}
	}

	resp.LastSyncedAt = d.teamLastSyncedAt(r, teamID)

	writeJSON(w, http.StatusOK, resp)
}

// --- GET /teams/{id}/velocity ---

type teamVelocityBreakdown struct {
	Issues  float64 `json:"issues"`
	PRs     float64 `json:"prs"`
	Commits float64 `json:"commits"`
}

type teamVelocitySprint struct {
	Label     string                `json:"label"`
	Score     float64               `json:"score"`
	Breakdown teamVelocityBreakdown `json:"breakdown"`
}

type teamVelocityResponse struct {
	Sprints      []teamVelocitySprint `json:"sprints"`
	LastSyncedAt *string              `json:"last_synced_at"`
}

func (d *Deps) handleTeamVelocity(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	teamID, err := parseTeamID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid team id")
		return
	}

	resp := teamVelocityResponse{
		Sprints: []teamVelocitySprint{},
	}

	teamNullID := sql.NullInt64{Int64: teamID, Valid: true}
	cache, err := d.Store.GetLatestCacheByPipeline(ctx, pipeline.VelocityPipeline, teamNullID)
	if err == nil {
		var vr pipeline.VelocityResult
		if json.Unmarshal([]byte(cache.Output), &vr) == nil {
			for _, s := range vr.Sprints {
				resp.Sprints = append(resp.Sprints, teamVelocitySprint{
					Label: s.Label,
					Score: s.Score,
					Breakdown: teamVelocityBreakdown{
						Issues:  s.Breakdown.Issues,
						PRs:     s.Breakdown.PRs,
						Commits: s.Breakdown.Commits,
					},
				})
			}
		}
	}

	resp.LastSyncedAt = d.teamLastSyncedAt(r, teamID)

	writeJSON(w, http.StatusOK, resp)
}

// --- GET /teams/{id}/metrics ---

type teamMetricsPanel struct {
	Title   string  `json:"title"`
	Value   *string `json:"value"`
	PanelID string  `json:"panel_id"`
}

type teamMetricsResponse struct {
	Panels       []teamMetricsPanel `json:"panels"`
	LastSyncedAt *string            `json:"last_synced_at"`
}

func (d *Deps) handleTeamMetrics(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	teamID, err := parseTeamID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid team id")
		return
	}

	resp := teamMetricsResponse{
		Panels: []teamMetricsPanel{},
	}

	teamNullID := sql.NullInt64{Int64: teamID, Valid: true}
	configs, err := d.Store.GetSourceConfigsForScope(ctx, teamNullID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list source configs: "+err.Error())
		return
	}

	for _, sc := range configs {
		if sc.Purpose != "metrics_panel" {
			continue
		}
		item, err := d.Store.GetCatalogueItem(ctx, sc.CatalogueID)
		if err != nil {
			continue
		}

		panel := teamMetricsPanel{
			Title: item.Title,
		}

		// Extract panel_id and value from source_meta JSON
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

		resp.Panels = append(resp.Panels, panel)
	}

	resp.LastSyncedAt = d.teamLastSyncedAt(r, teamID)

	writeJSON(w, http.StatusOK, resp)
}
