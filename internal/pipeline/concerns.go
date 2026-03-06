package pipeline

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/your-org/dashboard/internal/ai"
	"github.com/your-org/dashboard/internal/store"
)

// ConcernsPipeline is the pipeline name stored in ai_cache.
const ConcernsPipeline = "concerns"

const concernsSchema = `{"concerns":[{"key":"string","summary":"string","explanation":"string","severity":"high|medium|low"}]}`

// ConcernsResult is the structured output of the concerns pipeline.
type ConcernsResult struct {
	Concerns []Concern `json:"concerns"`
}

// Concern is a single concern identified from sprint data.
type Concern struct {
	Key         string `json:"key"`
	Summary     string `json:"summary"`
	Explanation string `json:"explanation"`
	Severity    string `json:"severity"`
}

// RunConcerns runs the concerns pipeline: identifies risks from sprint data.
// Active (non-archived) annotations are appended in the annotations block.
func RunConcerns(ctx context.Context, gen *ai.CachedGenerator, teamID int64, openIssues, mergedPRs any, sprintPlanText string, extractedGoals any, sprintMeta any, annotations []store.Annotation) (*ConcernsResult, error) {
	rawInputs := map[string]any{
		"open_issues":     openIssues,
		"merged_prs":      mergedPRs,
		"sprint_plan_text": sprintPlanText,
		"extracted_goals": extractedGoals,
		"sprint_meta":     sprintMeta,
	}
	prompt := buildPrompt(concernsSchema, rawInputs, annotations)
	inputs := map[string]any{"prompt": prompt}

	output, err := gen.Generate(ctx, ConcernsPipeline, &teamID, inputs, nil)
	if err != nil {
		return nil, fmt.Errorf("concerns: generate: %w", err)
	}

	var result ConcernsResult
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		return nil, fmt.Errorf("concerns: parse output: %w", err)
	}

	return &result, nil
}
