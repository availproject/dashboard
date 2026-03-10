package pipeline

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/your-org/dashboard/internal/ai"
	"github.com/your-org/dashboard/internal/store"
)

// VelocityPipeline is the pipeline name stored in ai_cache.
const VelocityPipeline = "velocity"

// VelocityResult is the structured output of the velocity pipeline.
type VelocityResult struct {
	Sprints []SprintVelocity `json:"sprints"`
}

// SprintVelocity is the velocity score for a single sprint.
type SprintVelocity struct {
	Label     string            `json:"label"`
	Score     float64           `json:"score"`
	Breakdown VelocityBreakdown `json:"breakdown"`
}

// VelocityBreakdown is the per-signal contribution to a sprint's velocity score.
type VelocityBreakdown struct {
	Issues  float64 `json:"issues"`
	PRs     float64 `json:"prs"`
	Commits float64 `json:"commits"`
}

// VelocitySprint is the input shape for a single sprint's raw metrics.
type VelocitySprint struct {
	Label        string `json:"label"`
	ClosedIssues int    `json:"closed_issues"`
	MergedPRs    int    `json:"merged_prs"`
	CommitCount  int    `json:"commit_count"`
}

// RunVelocity runs the velocity pipeline: normalizes sprint metrics into comparable scores.
func RunVelocity(ctx context.Context, gen *ai.CachedGenerator, teamID int64, sprints []VelocitySprint, annotations []store.Annotation) (*VelocityResult, error) {
	rawInputs := map[string]any{
		"sprints": sprints,
	}
	prompt := buildPrompt(velocitySchema, rawInputs, annotations)
	inputs := map[string]any{"prompt": prompt}

	output, err := gen.Generate(ctx, VelocityPipeline, &teamID, inputs, nil)
	if err != nil {
		return nil, fmt.Errorf("velocity: generate: %w", err)
	}

	var result VelocityResult
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		return nil, fmt.Errorf("velocity: parse output: %w", err)
	}

	return &result, nil
}
