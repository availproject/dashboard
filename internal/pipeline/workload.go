package pipeline

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/your-org/dashboard/internal/ai"
	"github.com/your-org/dashboard/internal/store"
)

// WorkloadPipeline is the pipeline name stored in ai_cache.
const WorkloadPipeline = "workload"

// WorkloadResult is the structured output of the workload pipeline.
type WorkloadResult struct {
	Members []MemberWorkload `json:"members"`
}

// MemberWorkload is the workload estimate for a single team member.
// Label thresholds: LOW < 3 days, NORMAL 3-5 days, HIGH > 5 days.
type MemberWorkload struct {
	Name          string  `json:"name"`
	EstimatedDays float64 `json:"estimated_days"`
	Label         string  `json:"label"`
}

// WorkloadMember is the input shape for a single team member.
type WorkloadMember struct {
	Name          string `json:"name"`
	OpenIssues    any    `json:"open_issues"`
	MergedPRs     any    `json:"merged_prs"`
	RecentCommits any    `json:"recent_commits"`
}

// SprintWindow is the input shape for the sprint time window.
type SprintWindow struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

// RunWorkload runs the workload pipeline: estimates work-days per team member.
func RunWorkload(ctx context.Context, gen *ai.CachedGenerator, teamID int64, members []WorkloadMember, sprintWindow SprintWindow, standardSprintDays int, annotations []store.Annotation) (*WorkloadResult, error) {
	rawInputs := map[string]any{
		"members":              members,
		"sprint_window":        sprintWindow,
		"standard_sprint_days": standardSprintDays,
	}
	prompt := buildPrompt(workloadSchema, rawInputs, annotations)
	inputs := map[string]any{"prompt": prompt}

	output, err := gen.Generate(ctx, WorkloadPipeline, &teamID, inputs, nil)
	if err != nil {
		return nil, fmt.Errorf("workload: generate: %w", err)
	}

	var result WorkloadResult
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		return nil, fmt.Errorf("workload: parse output: %w", err)
	}

	return &result, nil
}
