package pipeline

// TeamStatusPipeline is the pipeline name stored in ai_cache.
const TeamStatusPipeline = "team_status"

const teamStatusSchema = `{"business_goals":[{"text":"string","status":"on_track|at_risk|behind","note":"string"}],"sprint_goals":[{"text":"string","status":"likely_done|at_risk|unclear","note":"string"}],"sprint_forecast":"string","concerns":[{"key":"string","summary":"string","explanation":"string","severity":"high|medium|low","scope":"strategic|sprint"}]}`

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
	Status string `json:"status"` // on_track|at_risk|behind
	Note   string `json:"note"`
}

// SprintGoalItem is a sprint-level goal with a completion forecast.
type SprintGoalItem struct {
	Text   string `json:"text"`
	Status string `json:"status"` // likely_done|at_risk|unclear
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

// TeamStatusInput holds the inputs for the team_status pipeline.
type TeamStatusInput struct {
	GoalsDocText   string `json:"goals_doc_text"`
	SprintPlanText string `json:"sprint_plan_text"`
	SprintMeta     any    `json:"sprint_meta"`
	OpenIssues     any    `json:"open_issues"`
	MergedPRs      any    `json:"merged_prs"`
}
