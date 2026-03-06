package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"sort"
	"time"

	"github.com/your-org/dashboard/internal/pipeline"
)

type orgTeamItem struct {
	ID            int64   `json:"id"`
	Name          string  `json:"name"`
	CurrentSprint int     `json:"current_sprint"`
	TotalSprints  int     `json:"total_sprints"`
	RiskLevel     string  `json:"risk_level"`
	Focus         string  `json:"focus"`
	LastSyncedAt  *string `json:"last_synced_at"`
}

type orgWorkloadItem struct {
	Name      string             `json:"name"`
	TotalDays float64            `json:"total_days"`
	Label     string             `json:"label"`
	Breakdown map[string]float64 `json:"breakdown"`
}

type orgOverviewResponse struct {
	Teams         []orgTeamItem              `json:"teams"`
	Workload      []orgWorkloadItem          `json:"workload"`
	GoalAlignment *pipeline.AlignmentResult  `json:"goal_alignment"`
	LastSyncedAt  *string                    `json:"last_synced_at"`
}

func (d *Deps) handleOrgOverview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	teams, err := d.Store.ListTeams(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list teams: "+err.Error())
		return
	}

	type memberAgg struct {
		totalDays float64
		breakdown map[string]float64
	}
	workloadAgg := map[string]*memberAgg{}

	teamItems := make([]orgTeamItem, 0, len(teams))

	for _, t := range teams {
		item := orgTeamItem{ID: t.ID, Name: t.Name}

		teamID := sql.NullInt64{Int64: t.ID, Valid: true}

		// sprint_parse cache: current_sprint + total_sprints
		sprintCache, err := d.Store.GetLatestCacheByPipeline(ctx, pipeline.SprintParsePipeline, teamID)
		if err == nil {
			var sp pipeline.SprintParseResult
			if json.Unmarshal([]byte(sprintCache.Output), &sp) == nil {
				item.CurrentSprint = sp.CurrentSprint
				item.TotalSprints = sp.TotalSprints
			}
		}

		// concerns cache: risk_level
		concernsCache, err := d.Store.GetLatestCacheByPipeline(ctx, pipeline.ConcernsPipeline, teamID)
		if err == nil {
			var cr pipeline.ConcernsResult
			if json.Unmarshal([]byte(concernsCache.Output), &cr) == nil {
				item.RiskLevel = highestSeverity(cr.Concerns)
			}
		}

		// goal_extraction cache: focus (first goal text)
		goalsCache, err := d.Store.GetLatestCacheByPipeline(ctx, pipeline.GoalExtractionPipeline, teamID)
		if err == nil {
			var gr pipeline.GoalExtractionResult
			if json.Unmarshal([]byte(goalsCache.Output), &gr) == nil && len(gr.Goals) > 0 {
				item.Focus = gr.Goals[0].Text
			}
		}

		// last completed sync run for this team
		lastRun, err := d.Store.GetLastCompletedSyncRun(ctx, "team", teamID)
		if err == nil && lastRun.FinishedAt.Valid {
			s := lastRun.FinishedAt.Time.Format(time.RFC3339)
			item.LastSyncedAt = &s
		}

		// workload cache: cross-team aggregate
		workloadCache, err := d.Store.GetLatestCacheByPipeline(ctx, pipeline.WorkloadPipeline, teamID)
		if err == nil {
			var wr pipeline.WorkloadResult
			if json.Unmarshal([]byte(workloadCache.Output), &wr) == nil {
				for _, m := range wr.Members {
					if workloadAgg[m.Name] == nil {
						workloadAgg[m.Name] = &memberAgg{breakdown: map[string]float64{}}
					}
					workloadAgg[m.Name].totalDays += m.EstimatedDays
					workloadAgg[m.Name].breakdown[t.Name] += m.EstimatedDays
				}
			}
		}

		teamItems = append(teamItems, item)
	}

	// goal alignment (org-level, no team)
	var goalAlignment *pipeline.AlignmentResult
	alignCache, err := d.Store.GetLatestCacheByPipeline(ctx, pipeline.AlignmentPipeline, sql.NullInt64{})
	if err == nil {
		var ar pipeline.AlignmentResult
		if json.Unmarshal([]byte(alignCache.Output), &ar) == nil {
			goalAlignment = &ar
		}
	}

	// last completed org-scope sync run
	var lastSyncedAt *string
	orgRun, err := d.Store.GetLastCompletedSyncRun(ctx, "org", sql.NullInt64{})
	if err == nil && orgRun.FinishedAt.Valid {
		s := orgRun.FinishedAt.Time.Format(time.RFC3339)
		lastSyncedAt = &s
	}

	// build sorted workload list
	workloadItems := make([]orgWorkloadItem, 0, len(workloadAgg))
	for name, agg := range workloadAgg {
		workloadItems = append(workloadItems, orgWorkloadItem{
			Name:      name,
			TotalDays: agg.totalDays,
			Label:     workloadLabel(agg.totalDays),
			Breakdown: agg.breakdown,
		})
	}
	sort.Slice(workloadItems, func(i, j int) bool {
		return workloadItems[i].Name < workloadItems[j].Name
	})

	writeJSON(w, http.StatusOK, orgOverviewResponse{
		Teams:         teamItems,
		Workload:      workloadItems,
		GoalAlignment: goalAlignment,
		LastSyncedAt:  lastSyncedAt,
	})
}

func highestSeverity(concerns []pipeline.Concern) string {
	for _, c := range concerns {
		if c.Severity == "high" {
			return "high"
		}
	}
	for _, c := range concerns {
		if c.Severity == "medium" {
			return "medium"
		}
	}
	if len(concerns) > 0 {
		return "low"
	}
	return ""
}

func workloadLabel(days float64) string {
	if days < 3 {
		return "LOW"
	}
	if days <= 5 {
		return "NORMAL"
	}
	return "HIGH"
}
