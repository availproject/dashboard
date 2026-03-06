package pipeline

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/your-org/dashboard/internal/ai"
	"github.com/your-org/dashboard/internal/store"
)

// AlignmentPipeline is the pipeline name stored in ai_cache.
const AlignmentPipeline = "alignment"

const alignmentSchema = `{"alignments":[{"team_id":0,"aligned":true,"notes":"string"}],"flags":["string"]}`

// AlignmentResult is the structured output of the alignment pipeline.
type AlignmentResult struct {
	Alignments []TeamAlignment `json:"alignments"`
	Flags      []string        `json:"flags"`
}

// TeamAlignment is the alignment assessment for a single team.
type TeamAlignment struct {
	TeamID  int64  `json:"team_id"`
	Aligned bool   `json:"aligned"`
	Notes   string `json:"notes"`
}

// RunAlignment runs the alignment pipeline: assesses how well each team's goals align with org goals.
// This is an org-level pipeline; no teamID is associated with the cache entry.
func RunAlignment(ctx context.Context, gen *ai.CachedGenerator, orgGoalsText string, teamGoals map[int64][]string, annotations []store.Annotation) (*AlignmentResult, error) {
	rawInputs := map[string]any{
		"org_goals_text": orgGoalsText,
		"team_goals":     teamGoals,
	}
	prompt := buildPrompt(alignmentSchema, rawInputs, annotations)
	inputs := map[string]any{"prompt": prompt}

	output, err := gen.Generate(ctx, AlignmentPipeline, nil, inputs, nil)
	if err != nil {
		return nil, fmt.Errorf("alignment: generate: %w", err)
	}

	var result AlignmentResult
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		return nil, fmt.Errorf("alignment: parse output: %w", err)
	}

	return &result, nil
}
