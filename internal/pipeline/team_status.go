package pipeline

import (
	"time"

	"github.com/your-org/dashboard/internal/store"
)

// TeamStatusPipeline is the pipeline name stored in ai_cache.
const TeamStatusPipeline = "team_status"

// sprintTimingContext computes derived timing fields from a *store.SprintMeta.
// The returned map is included in the team_status prompt inputs so the model
// can reason about how far through the sprint/plan we are.
func sprintTimingContext(meta any, today time.Time) map[string]any {
	sm, ok := meta.(*store.SprintMeta)
	if !ok || sm == nil || !sm.StartDate.Valid || sm.StartDate.String == "" {
		return map[string]any{
			"available":         false,
			"note":              "sprint start date unavailable; timing-based calibration not possible",
		}
	}

	start, err := time.Parse("2006-01-02", sm.StartDate.String)
	if err != nil {
		return map[string]any{
			"available": false,
			"note":      "could not parse sprint start date",
		}
	}

	// Calendar days since plan start (0 = start day, clamp to 0 if future).
	calDays := today.Sub(start).Hours() / 24
	if calDays < 0 {
		calDays = 0
	}

	// 1-based day number within the plan (start day = Day 1).
	daysElapsed := int(calDays) + 1

	// Current sprint week within the plan (1-based).
	currentWeek := int(calDays/7) + 1

	// Day within the current sprint week (1-based, Mon=1 … Fri=5), clamped for weekends.
	daysIntoWeek := int(calDays)%7 + 1
	if daysIntoWeek > 5 {
		daysIntoWeek = 5
	}

	// Percentage of the current sprint week elapsed (0–100).
	weekProgressPct := (daysIntoWeek * 100) / 5

	// Total sprint weeks from sprint_meta (SprintNumber holds current; we don't
	// have total here, so expose what we have and let the model read total from
	// sprint_plan_text).
	result := map[string]any{
		"available":               true,
		"plan_start_date":         sm.StartDate.String,
		"days_elapsed_in_plan":    int(daysElapsed),
		"current_sprint_week":     currentWeek,
		"days_into_sprint_week":   daysIntoWeek,
		"sprint_week_progress_pct": weekProgressPct,
	}
	return result
}

// TeamStatusResult is the structured output of the team_status pipeline.
type TeamStatusResult struct {
	BusinessGoals  []BusinessGoalItem  `json:"business_goals"`
	SprintGoals    []SprintGoalItem    `json:"sprint_goals"`
	SprintForecast string              `json:"sprint_forecast"`
	Concerns       []TeamStatusConcern `json:"concerns"`
}

// BusinessGoalItem is a business-level goal with a status assessment.
type BusinessGoalItem struct {
	Text   string `json:"text"`
	Status string `json:"status"` // on_track|at_risk|behind|unclear
	Note   string `json:"note"`
}

// SprintGoalItem is a sprint-level goal with a completion forecast.
type SprintGoalItem struct {
	Text   string `json:"text"`
	Status string `json:"status"` // on_track|at_risk|unclear
	Note   string `json:"note"`
}

// TeamStatusConcern is a single concern with scope classification.
type TeamStatusConcern struct {
	Key         string `json:"key"`
	Summary     string `json:"summary"`
	Explanation string `json:"explanation"`
	Severity    string `json:"severity"` // high|medium|low
	Scope       string `json:"scope"`    // strategic|sprint
}

// sprintMetaForPrompt extracts only the semantic fields from a *store.SprintMeta,
// dropping ID, timestamps, and RawContent (which is already passed as sprint_plan_text).
// This keeps the cache key stable across syncs that don't change sprint data.
func sprintMetaForPrompt(meta any) map[string]any {
	sm, ok := meta.(*store.SprintMeta)
	if !ok || sm == nil {
		return nil
	}
	return map[string]any{
		"plan_type":     sm.PlanType,
		"sprint_number": sm.SprintNumber,
		"start_date":    sm.StartDate,
		"end_date":      sm.EndDate,
	}
}

// TeamStatusInput holds the inputs for the team_status pipeline.
type TeamStatusInput struct {
	GoalsDocText       string `json:"goals_doc_text"`
	SprintPlanText     string `json:"sprint_plan_text"`
	SprintMeta         any    `json:"sprint_meta"`
	OpenIssues         any    `json:"open_issues"`
	MergedPRs          any    `json:"merged_prs"`
	MarketingCampaigns any    `json:"marketing_campaigns,omitempty"`
	CalendarFlags      any    `json:"calendar_flags,omitempty"` // []CalendarEventFlag from dates_extract
}
