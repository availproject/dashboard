package pipeline

import (
	"context"
	"time"
)

// HomepageExtractPipeline is the pipeline name for homepage extraction.
const HomepageExtractPipeline = "homepage_extract"

// HomepageExtractResult holds the structured output of the homepage_extract pipeline.
type HomepageExtractResult struct {
	GoalsDoc      *string              `json:"goals_doc"`
	SprintPlans   []ExtractedSprintDoc `json:"sprint_plans"`
	Repos         []string             `json:"repos"`
	ProjectBoards []string             `json:"project_boards"`
	Metrics       []string             `json:"metrics"`
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
