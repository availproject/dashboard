package pipeline

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/your-org/dashboard/internal/ai"
	"github.com/your-org/dashboard/internal/store"
)

// ErrInvalidAIOutput is returned when the AI response cannot be parsed as JSON.
var ErrInvalidAIOutput = errors.New("invalid AI output: response is not valid JSON")

// ConcernsInput holds the inputs for the concerns pipeline.
type ConcernsInput struct {
	OpenIssues     any    `json:"open_issues"`
	MergedPRs      any    `json:"merged_prs"`
	SprintPlanText string `json:"sprint_plan_text"`
	ExtractedGoals any    `json:"extracted_goals"`
	SprintMeta     any    `json:"sprint_meta"`
}

// WorkloadInput holds the inputs for the workload_estimation pipeline.
type WorkloadInput struct {
	Members            []WorkloadMember `json:"members"`
	SprintWindow       SprintWindow     `json:"sprint_window"`
	StandardSprintDays int              `json:"standard_sprint_days"`
}

// VelocityInput holds the inputs for the velocity_analysis pipeline.
type VelocityInput struct {
	Sprints []VelocitySprint `json:"sprints"`
}

// Runner wraps a CachedGenerator and Store and exposes one typed method per pipeline.
type Runner struct {
	gen   *ai.CachedGenerator
	store *store.Store
}

// New returns a new Runner backed by the given CachedGenerator and Store.
func New(gen *ai.CachedGenerator, s *store.Store) *Runner {
	return &Runner{gen: gen, store: s}
}

// activeAnnotations returns non-archived annotations for the given teamID scope
// (pass nil for org-level pipelines).
func (r *Runner) activeAnnotations(ctx context.Context, teamID *int64) ([]store.Annotation, error) {
	var nullID sql.NullInt64
	if teamID != nil {
		nullID = sql.NullInt64{Int64: *teamID, Valid: true}
	}
	all, err := r.store.ListAnnotations(ctx, nullID)
	if err != nil {
		return nil, err
	}
	var active []store.Annotation
	for _, a := range all {
		if a.Archived == 0 {
			active = append(active, *a)
		}
	}
	return active, nil
}

// generate is a helper that builds the prompt, calls the generator, and
// unmarshals the result into dst. Returns ErrInvalidAIOutput when the JSON
// response cannot be unmarshaled.
func (r *Runner) generate(ctx context.Context, pipeline, schema string, teamID *int64, rawInputs map[string]any, annotations []store.Annotation, dst any) error {
	if r.gen == nil {
		return fmt.Errorf("%s: AI generator not configured", pipeline)
	}

	prompt := buildPrompt(schema, rawInputs, annotations)
	inputs := map[string]any{"prompt": prompt}

	output, err := r.gen.Generate(ctx, pipeline, teamID, inputs, nil)
	if err != nil {
		return fmt.Errorf("%s: generate: %w", pipeline, err)
	}

	if err := json.Unmarshal([]byte(output), dst); err != nil {
		log.Printf("ERROR pipeline %s: invalid AI output (raw): %s", pipeline, output)
		return ErrInvalidAIOutput
	}
	return nil
}

// RunSprintParse runs the sprint_parse pipeline and upserts sprint metadata.
func (r *Runner) RunSprintParse(ctx context.Context, teamID int64, sprintPlanText string) (*SprintParseResult, error) {
	annotations, err := r.activeAnnotations(ctx, &teamID)
	if err != nil {
		return nil, err
	}

	var result SprintParseResult
	if err := r.generate(ctx, SprintParsePipeline, sprintParseSchema, &teamID,
		map[string]any{
			"sprint_plan_text": sprintPlanText,
			"today":            time.Now().Format("2006-01-02"),
			"instructions":     "This document is the CURRENT sprint plan. Count only the sprint weeks defined in this document for total_sprints (e.g. if it defines Sprint 6, 7, 8, 9 then total_sprints=4, not 9). For current_sprint, use today's date and start_date to determine which sprint week we are on within this plan (1 = first week of this plan).",
		},
		annotations, &result); err != nil {
		return nil, err
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

	if _, err := r.store.UpsertSprintMeta(ctx, teamID, "current", sprintNumber, startDate, sql.NullString{}, rawContent); err != nil {
		return nil, fmt.Errorf("sprint_parse: upsert sprint meta: %w", err)
	}

	// Auto-add team members found in the sprint doc.
	for _, m := range result.Members {
		if m.Name == "" {
			continue
		}
		_ = r.store.UpsertMemberByName(ctx, teamID, m.Name)
	}

	return &result, nil
}

// RunGoalExtraction runs the goal_extraction pipeline.
func (r *Runner) RunGoalExtraction(ctx context.Context, teamID int64, goalsDocText, sprintPlanText string) (*GoalExtractionResult, error) {
	annotations, err := r.activeAnnotations(ctx, &teamID)
	if err != nil {
		return nil, err
	}

	var result GoalExtractionResult
	if err := r.generate(ctx, GoalExtractionPipeline, goalExtractionSchema, &teamID,
		map[string]any{
			"goals_doc_text":   goalsDocText,
			"sprint_plan_text": sprintPlanText,
		},
		annotations, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// RunConcerns runs the concerns pipeline.
func (r *Runner) RunConcerns(ctx context.Context, teamID int64, input ConcernsInput) (*ConcernsResult, error) {
	annotations, err := r.activeAnnotations(ctx, &teamID)
	if err != nil {
		return nil, err
	}

	var result ConcernsResult
	if err := r.generate(ctx, ConcernsPipeline, concernsSchema, &teamID,
		map[string]any{
			"today":            time.Now().Format("2006-01-02"),
			"open_issues":      input.OpenIssues,
			"merged_prs":       input.MergedPRs,
			"sprint_plan_text": input.SprintPlanText,
			"extracted_goals":  input.ExtractedGoals,
			"sprint_meta":      input.SprintMeta,
		},
		annotations, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// RunWorkloadEstimation runs the workload_estimation pipeline.
func (r *Runner) RunWorkloadEstimation(ctx context.Context, teamID int64, input WorkloadInput) (*WorkloadResult, error) {
	annotations, err := r.activeAnnotations(ctx, &teamID)
	if err != nil {
		return nil, err
	}

	var result WorkloadResult
	if err := r.generate(ctx, WorkloadPipeline, workloadSchema, &teamID,
		map[string]any{
			"members":              input.Members,
			"sprint_window":        input.SprintWindow,
			"standard_sprint_days": input.StandardSprintDays,
		},
		annotations, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// RunVelocityAnalysis runs the velocity_analysis pipeline.
func (r *Runner) RunVelocityAnalysis(ctx context.Context, teamID int64, input VelocityInput) (*VelocityResult, error) {
	annotations, err := r.activeAnnotations(ctx, &teamID)
	if err != nil {
		return nil, err
	}

	var result VelocityResult
	if err := r.generate(ctx, VelocityPipeline, velocitySchema, &teamID,
		map[string]any{"sprints": input.Sprints},
		annotations, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// RunTeamStatus runs the team_status pipeline, replacing goal_extraction + concerns.
func (r *Runner) RunTeamStatus(ctx context.Context, teamID int64, input TeamStatusInput) (*TeamStatusResult, error) {
	annotations, err := r.activeAnnotations(ctx, &teamID)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	timing := sprintTimingContext(input.SprintMeta, now)
	timingJSON, _ := json.Marshal(timing)
	log.Printf("DEBUG team_status [team %d]: sprint_timing=%s", teamID, timingJSON)

	var result TeamStatusResult
	if err := r.generate(ctx, TeamStatusPipeline, teamStatusSchema, &teamID,
		map[string]any{
			"today":            now.Format("2006-01-02"),
			"goals_doc_text":   input.GoalsDocText,
			"sprint_plan_text": input.SprintPlanText,
			"sprint_meta":      input.SprintMeta,
			"sprint_timing":    timing,
			"open_issues":      input.OpenIssues,
			"merged_prs":       input.MergedPRs,
			"instructions":     teamStatusInstructions,
		},
		annotations, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// RunGoalAlignment runs the goal_alignment pipeline (org-level; no teamID).
func (r *Runner) RunGoalAlignment(ctx context.Context, orgGoalsText string, teamGoals map[int64][]string) (*AlignmentResult, error) {
	annotations, err := r.activeAnnotations(ctx, nil)
	if err != nil {
		return nil, err
	}

	var result AlignmentResult
	if err := r.generate(ctx, AlignmentPipeline, alignmentSchema, nil,
		map[string]any{
			"org_goals_text": orgGoalsText,
			"team_goals":     teamGoals,
		},
		annotations, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// RunDiscoverySuggestion runs the discovery_suggestion pipeline (no teamID).
func (r *Runner) RunDiscoverySuggestion(ctx context.Context, title, excerpt string) (*DiscoverySuggestionResult, error) {
	var result DiscoverySuggestionResult
	if err := r.generate(ctx, DiscoverySuggestionPipeline, discoverySchema, nil,
		map[string]any{
			"title":   title,
			"excerpt": excerpt,
		},
		nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// RunLabelMatch runs the label_match pipeline: given a list of team names and
// label infos, returns one LabelMatchItem per label with the matched team name
// (or "unknown"). All labels are classified in a single AI call.
func (r *Runner) RunLabelMatch(ctx context.Context, teamNames []string, labels []LabelInfo) ([]LabelMatchItem, error) {
	var result struct {
		Matches []LabelMatchItem `json:"matches"`
	}
	if err := r.generate(ctx, LabelMatchPipeline, labelMatchSchema, nil,
		map[string]any{
			"teams":  teamNames,
			"labels": labels,
		},
		nil, &result); err != nil {
		return nil, err
	}
	return result.Matches, nil
}
