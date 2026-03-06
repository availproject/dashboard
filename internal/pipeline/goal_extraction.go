package pipeline

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/your-org/dashboard/internal/ai"
	"github.com/your-org/dashboard/internal/store"
)

// GoalExtractionPipeline is the pipeline name stored in ai_cache.
const GoalExtractionPipeline = "goal_extraction"

const goalExtractionSchema = `{"goals":[{"text":"string","source":"goals_doc|sprint_plan"}]}`

// GoalExtractionResult is the structured output of the goal_extraction pipeline.
type GoalExtractionResult struct {
	Goals []ExtractedGoal `json:"goals"`
}

// ExtractedGoal is a single goal extracted from a document.
type ExtractedGoal struct {
	Text   string `json:"text"`
	Source string `json:"source"`
}

// RunGoalExtraction runs the goal_extraction pipeline: extracts goals from
// goalsDocText and sprintPlanText and returns the structured result.
func RunGoalExtraction(ctx context.Context, gen *ai.CachedGenerator, teamID int64, goalsDocText, sprintPlanText string, annotations []store.Annotation) (*GoalExtractionResult, error) {
	rawInputs := map[string]any{
		"goals_doc_text":  goalsDocText,
		"sprint_plan_text": sprintPlanText,
	}
	prompt := buildPrompt(goalExtractionSchema, rawInputs, annotations)
	inputs := map[string]any{"prompt": prompt}

	output, err := gen.Generate(ctx, GoalExtractionPipeline, &teamID, inputs, nil)
	if err != nil {
		return nil, fmt.Errorf("goal_extraction: generate: %w", err)
	}

	var result GoalExtractionResult
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		return nil, fmt.Errorf("goal_extraction: parse output: %w", err)
	}

	return &result, nil
}
