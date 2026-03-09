package pipeline

import "context"

// HomepageExtractPipeline is the pipeline name for homepage extraction.
const HomepageExtractPipeline = "homepage_extract"

const homepageExtractSchema = `{
  "goals_doc": "url or null",
  "sprint_plans": [{"url":"string","title":"string","sprint_status":"current|next|archived","start_date":"YYYY-MM-DD or null","end_date":"YYYY-MM-DD or null"}],
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
	URL          string  `json:"url"`
	Title        string  `json:"title"`
	SprintStatus string  `json:"sprint_status"` // "current", "next", "archived"
	StartDate    *string `json:"start_date"`
	EndDate      *string `json:"end_date"`
}

// RunHomepageExtract runs the homepage_extract pipeline for the given team and homepage text.
func (r *Runner) RunHomepageExtract(ctx context.Context, teamID int64, homepageText string) (*HomepageExtractResult, error) {
	var result HomepageExtractResult
	if err := r.generate(ctx, HomepageExtractPipeline, homepageExtractSchema, &teamID,
		map[string]any{"homepage_text": homepageText},
		nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}
