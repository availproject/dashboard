package pipeline

import (
	"context"
	"time"
)

// HomepageExtractPipeline is the pipeline name for homepage extraction.
const HomepageExtractPipeline = "homepage_extract"

const homepageExtractSchema = `{
  "goals_doc": "url or null",
  "sprint_plans": [{"url":"string","title":"string","sprint_status":"current|next|archived","active_sprint_week":"number (1-based position of the active sprint within this plan, e.g. 1 for the first sprint of this plan) or null","sprint_start_date":"YYYY-MM-DD (start date of the active sprint week) or null","total_weeks_in_plan":"number (total sprint weeks in this plan) or null"}],
  "repos": ["url"],
  "metrics": ["url"]
}`

// HomepageExtractResult holds the structured output of the homepage_extract pipeline.
type HomepageExtractResult struct {
	GoalsDoc    *string              `json:"goals_doc"`
	SprintPlans []ExtractedSprintDoc `json:"sprint_plans"`
	Repos       []string             `json:"repos"`
	Metrics     []string             `json:"metrics"`
}

// ExtractedSprintDoc is one sprint plan doc extracted from the homepage.
type ExtractedSprintDoc struct {
	URL               string  `json:"url"`
	Title             string  `json:"title"`
	SprintStatus      string  `json:"sprint_status"`      // "current", "next", "archived"
	ActiveSprintWeek  *int    `json:"active_sprint_week"` // 1-based position within this plan
	SprintStartDate   *string `json:"sprint_start_date"`  // start date of the active sprint week
	TotalWeeksInPlan  *int    `json:"total_weeks_in_plan"`
}

const homepageExtractInstructions = `For each sprint plan, extract:
- active_sprint_week: the 1-based position of the currently active sprint WITHIN THIS PLAN (not an absolute sprint number). Example: if a plan covers "Sprints 6-9" and Sprint 6 is active, active_sprint_week=1. If Sprint 7 is active, active_sprint_week=2. If the text says "first of 4 sprints", active_sprint_week=1.
- sprint_start_date: the calendar start date of the sprint identified by active_sprint_week (i.e. when that specific sprint week began). A date shown in context like "Where we are (March 9): Sprint 6" means Sprint 6 started on March 9 — use that. If no date for the sprint is mentioned, use today's date (provided in inputs).
- total_weeks_in_plan: total number of sprint weeks in this plan. Example: a plan labeled "Sprints 6-9" has total_weeks_in_plan=4; a plan labeled "4 sprints" also has total_weeks_in_plan=4.
These three fields are used to compute the plan start date (sprint_start_date minus (active_sprint_week-1) weeks) and track current position over time.`

// RunHomepageExtract runs the homepage_extract pipeline for the given team and homepage text.
func (r *Runner) RunHomepageExtract(ctx context.Context, teamID int64, homepageText string) (*HomepageExtractResult, error) {
	var result HomepageExtractResult
	if err := r.generate(ctx, HomepageExtractPipeline, homepageExtractSchema, &teamID,
		map[string]any{
			"homepage_text": homepageText,
			"today":         time.Now().Format("2006-01-02"),
			"instructions":  homepageExtractInstructions,
		},
		nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}
