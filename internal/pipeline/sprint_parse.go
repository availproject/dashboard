package pipeline

// SprintParsePipeline is the pipeline name stored in ai_cache.
const SprintParsePipeline = "sprint_parse"

// SprintParseResult is the structured output of the sprint_parse pipeline.
type SprintParseResult struct {
	StartDate     *string             `json:"start_date"`
	TotalSprints  int                 `json:"total_sprints"`
	CurrentSprint int                 `json:"current_sprint"`
	Goals         []string            `json:"goals"`
	Members       []SprintMemberEntry `json:"members"`
}

// SprintMemberEntry is a team member mentioned in a sprint plan.
type SprintMemberEntry struct {
	Name string  `json:"name"`
	Role *string `json:"role"`
}
