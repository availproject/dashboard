package sync

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/your-org/dashboard/internal/connector"
)

// Discover runs a discovery pass for the given scope and target.
// It creates a sync_run record (status='running') and launches a background
// goroutine to perform the actual work. Returns the syncRunID immediately.
func (e *Engine) Discover(ctx context.Context, scope, target string) (int64, error) {
	run, err := e.store.CreateSyncRun(ctx, sql.NullInt64{}, scope)
	if err != nil {
		return 0, fmt.Errorf("discover: create sync run: %w", err)
	}
	go e.discoverBackground(run.ID, scope, target)
	return run.ID, nil
}

func (e *Engine) discoverBackground(runID int64, scope, target string) {
	ctx := context.Background()

	var items []connector.DiscoveredItem
	var discoverErr error

	switch scope {
	case "notion_workspace":
		items, discoverErr = e.notion.Discover(ctx, target)
	case "github_repo":
		items, discoverErr = e.github.Discover(ctx, target)
	case "metrics_url":
		items, discoverErr = e.discoverMetrics(ctx, target)
	default:
		discoverErr = fmt.Errorf("unknown scope: %s", scope)
	}

	if discoverErr != nil {
		_ = e.store.UpdateSyncRun(ctx, runID, "failed", sql.NullString{
			String: discoverErr.Error(), Valid: true,
		})
		return
	}

	for _, item := range items {
		metaStr := sql.NullString{}
		if item.SourceMeta != nil {
			if b, err := json.Marshal(item.SourceMeta); err == nil {
				metaStr = sql.NullString{String: string(b), Valid: true}
			}
		}

		catalogueItem, err := e.store.UpsertCatalogueItem(ctx,
			item.SourceType, item.ExternalID, item.Title,
			sql.NullString{String: item.URL, Valid: item.URL != ""},
			metaStr,
		)
		if err != nil {
			_ = e.store.UpdateSyncRun(ctx, runID, "failed", sql.NullString{
				String: err.Error(), Valid: true,
			})
			return
		}

		// Run discovery suggestion for new items (no AI suggestion yet).
		if !catalogueItem.AISuggestion.Valid || catalogueItem.AISuggestion.String == "" {
			result, err := e.pipeline.RunDiscoverySuggestion(ctx, item.Title, item.Excerpt)
			if err == nil && result != nil {
				_ = e.store.UpdateCatalogueAISuggestion(ctx, catalogueItem.ID, result.SuggestedPurpose)
			}
		}
	}

	_ = e.store.UpdateSyncRun(ctx, runID, "completed", sql.NullString{})
}

// discoverMetrics runs discovery against all metrics connectors (grafana, posthog,
// signoz) and merges the results. Individual connector errors are silently ignored
// so that connectors not applicable to the target URL do not fail the run.
func (e *Engine) discoverMetrics(ctx context.Context, target string) ([]connector.DiscoveredItem, error) {
	var all []connector.DiscoveredItem
	if items, err := e.grafana.Discover(ctx, target); err == nil {
		all = append(all, items...)
	}
	if items, err := e.posthog.Discover(ctx, target); err == nil {
		all = append(all, items...)
	}
	if items, err := e.signoz.Discover(ctx, target); err == nil {
		all = append(all, items...)
	}
	return all, nil
}
