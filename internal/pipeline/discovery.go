package pipeline

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/your-org/dashboard/internal/ai"
)

// DiscoverySuggestionPipeline is the pipeline name stored in ai_cache.
const DiscoverySuggestionPipeline = "discovery_suggestion"

const discoverySchema = `{"suggested_purpose":"current_plan|next_plan|goals|metrics_panel|org_goals|org_milestones|unknown","confidence":"high|medium|low","reasoning":"string"}`

// DiscoverySuggestionResult is the structured output of the discovery_suggestion pipeline.
type DiscoverySuggestionResult struct {
	SuggestedPurpose string `json:"suggested_purpose"`
	Confidence       string `json:"confidence"`
	Reasoning        string `json:"reasoning"`
}

// RunDiscoverySuggestion runs the discovery_suggestion pipeline: suggests a purpose for a newly discovered source item.
// excerpt should be the first 500 characters of the item's content.
// No teamID is associated (item is not yet tagged to a team).
func RunDiscoverySuggestion(ctx context.Context, gen *ai.CachedGenerator, title, excerpt string) (*DiscoverySuggestionResult, error) {
	rawInputs := map[string]any{
		"title":   title,
		"excerpt": excerpt,
	}
	prompt := buildPrompt(discoverySchema, rawInputs, nil)
	inputs := map[string]any{"prompt": prompt}

	output, err := gen.Generate(ctx, DiscoverySuggestionPipeline, nil, inputs, nil)
	if err != nil {
		return nil, fmt.Errorf("discovery_suggestion: generate: %w", err)
	}

	var result DiscoverySuggestionResult
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		return nil, fmt.Errorf("discovery_suggestion: parse output: %w", err)
	}

	return &result, nil
}
