package pipeline

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/your-org/dashboard/internal/ai"
	"github.com/your-org/dashboard/internal/store"
)

// SprintParsePipeline is the pipeline name stored in ai_cache.
const SprintParsePipeline = "sprint_parse"

const sprintParseSchema = `{"start_date":"YYYY-MM-DD or null","total_sprints":0,"current_sprint":0,"goals":["string"],"members":[{"name":"string","role":"string or null"}]}`

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

// RunSprintParse runs the sprint_parse pipeline: sends sprintPlanText to the AI,
// parses the result, and upserts sprint metadata into the store.
func RunSprintParse(ctx context.Context, gen *ai.CachedGenerator, s *store.Store, teamID int64, sprintPlanText string, annotations []store.Annotation) (*SprintParseResult, error) {
	rawInputs := map[string]any{"sprint_plan_text": sprintPlanText}
	prompt := buildPrompt(sprintParseSchema, rawInputs, annotations)
	inputs := map[string]any{"prompt": prompt}

	output, err := gen.Generate(ctx, SprintParsePipeline, &teamID, inputs, nil)
	if err != nil {
		return nil, fmt.Errorf("sprint_parse: generate: %w", err)
	}

	var result SprintParseResult
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		return nil, fmt.Errorf("sprint_parse: parse output: %w", err)
	}

	sprintNumber := sql.NullInt64{}
	if result.CurrentSprint > 0 {
		sprintNumber = sql.NullInt64{Int64: int64(result.CurrentSprint), Valid: true}
	}
	startDate := sql.NullString{}
	if result.StartDate != nil {
		startDate = sql.NullString{String: *result.StartDate, Valid: true}
	}
	rawContent := sql.NullString{String: sprintPlanText, Valid: true}

	if _, err := s.UpsertSprintMeta(ctx, teamID, "current", sprintNumber, startDate, sql.NullString{}, rawContent); err != nil {
		return nil, fmt.Errorf("sprint_parse: upsert sprint meta: %w", err)
	}

	return &result, nil
}
