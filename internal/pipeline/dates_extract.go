package pipeline

import "context"

// DatesExtractPipeline is the pipeline name stored in ai_cache.
const DatesExtractPipeline = "dates_extract"

// DatesExtractInput holds the inputs for the dates_extract pipeline.
type DatesExtractInput struct {
	GoalsDocText       string              `json:"goals_doc_text"`
	SprintPlanText     string              `json:"sprint_plan_text"`
	SprintCalendar     []SprintCalEntry    `json:"sprint_calendar"`
	MarketingCampaigns []CalendarCampaign  `json:"marketing_campaigns,omitempty"`
	Today              string              `json:"today"`
}

// SprintCalEntry describes one sprint week's date range.
type SprintCalEntry struct {
	SprintNum int    `json:"sprint_num"`
	StartDate string `json:"start_date"`
	EndDate   string `json:"end_date"`
}

// CalendarCampaign is the marketing campaign view passed to dates_extract.
type CalendarCampaign struct {
	Name      string  `json:"name"`
	DateStart *string `json:"date_start,omitempty"`
	DateEnd   *string `json:"date_end,omitempty"`
}

// DatesExtractResult is the structured output of the dates_extract pipeline.
type DatesExtractResult struct {
	Events []CalendarEventOutput `json:"events"`
}

// CalendarEventOutput is one event produced by the dates_extract pipeline.
type CalendarEventOutput struct {
	EventKey       string                  `json:"event_key"`
	Title          string                  `json:"title"`
	EventType      string                  `json:"event_type"` // release|milestone|deadline
	Date           *string                 `json:"date,omitempty"`
	DateConfidence string                  `json:"date_confidence"` // confirmed|inferred|none
	EndDate        *string                 `json:"end_date,omitempty"`
	NeedsDate      bool                    `json:"needs_date"`
	Sources        []CalendarEventSource   `json:"sources"`
	Flags          []CalendarEventFlag     `json:"flags,omitempty"`
}

// CalendarEventSource records how one source document contributed to an event.
type CalendarEventSource struct {
	Source     string  `json:"source"`     // goals_doc|sprint_doc|marketing
	Mention    string  `json:"mention"`    // verbatim or close paraphrase
	Date       *string `json:"date,omitempty"`
	Confidence string  `json:"confidence"` // confirmed|inferred|none
}

// CalendarEventFlag records a detected contradiction or planning concern.
type CalendarEventFlag struct {
	Type    string `json:"type"`    // date_mismatch|missing_date|missing_sprint_coverage
	Message string `json:"message"`
}

// RunDatesExtract runs the dates_extract pipeline and returns the result.
// It does not write to the store — the caller is responsible for persistence.
func (r *Runner) RunDatesExtract(ctx context.Context, teamID int64, input DatesExtractInput) (*DatesExtractResult, error) {
	annotations, err := r.activeAnnotations(ctx, &teamID)
	if err != nil {
		return nil, err
	}

	rawInputs := map[string]any{
		"today":            input.Today,
		"goals_doc_text":   input.GoalsDocText,
		"sprint_plan_text": input.SprintPlanText,
		"sprint_calendar":  input.SprintCalendar,
		"instructions":     datesExtractInstructions,
	}
	if len(input.MarketingCampaigns) > 0 {
		rawInputs["marketing_campaigns"] = input.MarketingCampaigns
	}

	var result DatesExtractResult
	if err := r.generate(ctx, DatesExtractPipeline, datesExtractSchema, &teamID, rawInputs, annotations, &result); err != nil {
		return nil, err
	}
	return &result, nil
}
