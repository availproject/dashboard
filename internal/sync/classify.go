package sync

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/your-org/dashboard/internal/pipeline"
)

// Classify runs AI classification on the given catalogue item IDs.
// It creates a sync_run with scope='classify' and launches a background
// goroutine to perform the actual work. Returns the syncRunID immediately.
func (e *Engine) Classify(ctx context.Context, itemIDs []int64) (int64, error) {
	run, err := e.store.CreateSyncRun(ctx, sql.NullInt64{}, "classify")
	if err != nil {
		return 0, fmt.Errorf("classify: create sync run: %w", err)
	}
	go e.classifyBackground(run.ID, itemIDs)
	return run.ID, nil
}

func (e *Engine) classifyBackground(runID int64, itemIDs []int64) {
	ctx := context.Background()

	// Separate github_label items (classified in one batch) from everything else.
	var labelInfos []pipeline.LabelInfo
	var otherIDs []int64

	for _, id := range itemIDs {
		item, err := e.store.GetCatalogueItem(ctx, id)
		if err != nil {
			_ = e.store.UpdateSyncRun(ctx, runID, "failed", sql.NullString{
				String: fmt.Sprintf("get item %d: %v", id, err), Valid: true,
			})
			return
		}
		if item.SourceType == "github_label" {
			labelInfos = append(labelInfos, pipeline.LabelInfo{ID: id, Name: item.Title})
		} else {
			otherIDs = append(otherIDs, id)
		}
	}

	// Batch-classify labels: one AI call that knows all teams and all labels.
	if len(labelInfos) > 0 {
		teams, err := e.store.ListTeams(ctx)
		if err != nil {
			_ = e.store.UpdateSyncRun(ctx, runID, "failed", sql.NullString{
				String: "list teams: " + err.Error(), Valid: true,
			})
			return
		}
		teamNames := make([]string, len(teams))
		for i, t := range teams {
			teamNames[i] = t.Name
		}
		matches, err := e.pipeline.RunLabelMatch(ctx, teamNames, labelInfos)
		if err != nil {
			_ = e.store.UpdateSyncRun(ctx, runID, "failed", sql.NullString{
				String: err.Error(), Valid: true,
			})
			return
		}
		for _, m := range matches {
			_ = e.store.UpdateCatalogueAISuggestion(ctx, m.ID, m.TeamName)
		}
	}

	// Classify everything else individually by purpose.
	for _, id := range otherIDs {
		item, err := e.store.GetCatalogueItem(ctx, id)
		if err != nil {
			_ = e.store.UpdateSyncRun(ctx, runID, "failed", sql.NullString{
				String: fmt.Sprintf("get item %d: %v", id, err), Valid: true,
			})
			return
		}
		result, err := e.pipeline.RunDiscoverySuggestion(ctx, item.Title, "")
		if err != nil {
			_ = e.store.UpdateSyncRun(ctx, runID, "failed", sql.NullString{
				String: err.Error(), Valid: true,
			})
			return
		}
		if result != nil {
			_ = e.store.UpdateCatalogueAISuggestion(ctx, id, result.SuggestedPurpose)
		}
	}

	_ = e.store.UpdateSyncRun(ctx, runID, "completed", sql.NullString{})
}
