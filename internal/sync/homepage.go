package sync

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/your-org/dashboard/internal/connector"
)

// HomepageExtract creates a sync_run for homepage extraction and starts it in the background.
// Returns the sync run ID immediately.
func (e *Engine) HomepageExtract(ctx context.Context, teamID int64) (int64, error) {
	nullTeamID := sql.NullInt64{Int64: teamID, Valid: true}

	// Return existing run if one is already in progress.
	existing, err := e.store.GetRunningSyncRun(ctx, "homepage_extract", nullTeamID)
	if err == nil {
		return existing.ID, nil
	}
	if err != sql.ErrNoRows {
		return 0, fmt.Errorf("homepage_extract: check running: %w", err)
	}

	run, err := e.store.CreateSyncRun(ctx, nullTeamID, "homepage_extract")
	if err != nil {
		return 0, fmt.Errorf("homepage_extract: create run: %w", err)
	}

	go e.homepageExtractBackground(run.ID, teamID)
	return run.ID, nil
}

func (e *Engine) homepageExtractBackground(runID int64, teamID int64) {
	ctx := context.Background()
	nullTeamID := sql.NullInt64{Int64: teamID, Valid: true}

	// 1. Find the project_homepage config for this team.
	homepageConfigs, err := e.store.GetConfigsByPurpose(ctx, nullTeamID, "project_homepage")
	if err != nil || len(homepageConfigs) == 0 {
		msg := "no homepage configured"
		if err != nil {
			msg = err.Error()
		}
		_ = e.store.UpdateSyncRun(ctx, runID, "error", sql.NullString{String: msg, Valid: true})
		return
	}

	homepageCfg := homepageConfigs[0]

	// 2. Get the catalogue item for the homepage.
	item, err := e.store.GetCatalogueItem(ctx, homepageCfg.CatalogueID)
	if err != nil {
		_ = e.store.UpdateSyncRun(ctx, runID, "error", sql.NullString{
			String: "get homepage catalogue item: " + err.Error(), Valid: true,
		})
		return
	}

	// 3. Fetch homepage content.
	log.Printf("INFO  homepage_extract [run %d team %d]: fetching content from %s (type=%s)", runID, teamID, item.ExternalID, item.SourceType)
	content, err := e.fetchSourceContent(ctx, item)
	if err != nil {
		log.Printf("ERROR homepage_extract [run %d team %d]: fetch failed: %v", runID, teamID, err)
		_ = e.store.UpdateSyncRun(ctx, runID, "error", sql.NullString{
			String: "fetch homepage content: " + err.Error(), Valid: true,
		})
		return
	}
	log.Printf("INFO  homepage_extract [run %d team %d]: fetched %d chars", runID, teamID, len(content))

	// 4. Run the homepage extraction pipeline.
	log.Printf("INFO  homepage_extract [run %d team %d]: running AI pipeline", runID, teamID)
	result, err := e.pipeline.RunHomepageExtract(ctx, teamID, content)
	if err != nil {
		log.Printf("ERROR homepage_extract [run %d team %d]: pipeline failed: %v", runID, teamID, err)
		_ = e.store.UpdateSyncRun(ctx, runID, "error", sql.NullString{
			String: "pipeline: " + err.Error(), Valid: true,
		})
		return
	}
	goalsURL := "<nil>"
	if result.GoalsDoc != nil {
		goalsURL = *result.GoalsDoc
	}
	log.Printf("INFO  homepage_extract [run %d team %d]: AI result — goals_doc=%q sprint_plans=%d repos=%d metrics=%d",
		runID, teamID, goalsURL, len(result.SprintPlans), len(result.Repos), len(result.Metrics))
	for i, sp := range result.SprintPlans {
		log.Printf("INFO  homepage_extract [run %d team %d]:   sprint[%d] status=%q title=%q url=%q", runID, teamID, i, sp.SprintStatus, sp.Title, sp.URL)
	}
	for i, r := range result.Repos {
		log.Printf("INFO  homepage_extract [run %d team %d]:   repo[%d] url=%q", runID, teamID, i, r)
	}

	// 5. Delete existing ai_extracted configs for this team.
	if err := e.store.DeleteAIExtractedConfigsForTeam(ctx, nullTeamID); err != nil {
		_ = e.store.UpdateSyncRun(ctx, runID, "error", sql.NullString{
			String: "delete ai_extracted configs: " + err.Error(), Valid: true,
		})
		return
	}

	// 6. Upsert goals_doc.
	if result.GoalsDoc != nil && *result.GoalsDoc != "" {
		log.Printf("INFO  homepage_extract [run %d team %d]: discovering goals_doc %q", runID, teamID, *result.GoalsDoc)
		catID, err := e.discoverSingleURL(ctx, *result.GoalsDoc)
		if err != nil {
			log.Printf("WARN  homepage_extract [run %d team %d]: goals_doc discover failed: %v", runID, teamID, err)
		} else {
			log.Printf("INFO  homepage_extract [run %d team %d]: goals_doc → catalogue id %d", runID, teamID, catID)
			_, _ = e.store.UpsertSourceConfig(ctx, catID, nullTeamID, "goals_doc",
				sql.NullString{}, "ai_extracted")
		}
	}

	// 7. Upsert sprint plans.
	for _, sp := range result.SprintPlans {
		if sp.URL == "" {
			continue
		}
		log.Printf("INFO  homepage_extract [run %d team %d]: discovering sprint_doc %q (status=%s)", runID, teamID, sp.URL, sp.SprintStatus)
		catID, err := e.discoverSingleURL(ctx, sp.URL)
		if err != nil {
			log.Printf("WARN  homepage_extract [run %d team %d]: sprint_doc discover failed: %v", runID, teamID, err)
			continue
		}
		log.Printf("INFO  homepage_extract [run %d team %d]: sprint_doc → catalogue id %d", runID, teamID, catID)
		metaJSON, _ := json.Marshal(map[string]string{"sprint_status": sp.SprintStatus})
		_, _ = e.store.UpsertSourceConfig(ctx, catID, nullTeamID, "sprint_doc",
			sql.NullString{String: string(metaJSON), Valid: true}, "ai_extracted")

		// Store sprint dates in sprint_meta so the team view shows the correct week.
		if sp.SprintStatus == "current" && (sp.StartDate != nil || sp.EndDate != nil) {
			startDate := sql.NullString{}
			if sp.StartDate != nil && *sp.StartDate != "" {
				startDate = sql.NullString{String: *sp.StartDate, Valid: true}
			}
			endDate := sql.NullString{}
			if sp.EndDate != nil && *sp.EndDate != "" {
				endDate = sql.NullString{String: *sp.EndDate, Valid: true}
			}
			_, _ = e.store.UpsertSprintMeta(ctx, teamID, "current",
				sql.NullInt64{}, startDate, endDate, sql.NullString{})
		}
	}

	// 8. Upsert GitHub repos.
	for _, repoURL := range result.Repos {
		if repoURL == "" {
			continue
		}
		log.Printf("INFO  homepage_extract [run %d team %d]: discovering github_repo %q", runID, teamID, repoURL)
		catID, err := e.discoverSingleURL(ctx, repoURL)
		if err != nil {
			log.Printf("WARN  homepage_extract [run %d team %d]: github_repo discover failed: %v", runID, teamID, err)
			continue
		}
		log.Printf("INFO  homepage_extract [run %d team %d]: github_repo → catalogue id %d", runID, teamID, catID)
		_, _ = e.store.UpsertSourceConfig(ctx, catID, nullTeamID, "github_repo",
			sql.NullString{}, "ai_extracted")
	}

	// 9. Upsert metrics panels.
	for _, metricsURL := range result.Metrics {
		if metricsURL == "" {
			continue
		}
		log.Printf("INFO  homepage_extract [run %d team %d]: discovering metrics_panel %q", runID, teamID, metricsURL)
		catID, err := e.discoverSingleURL(ctx, metricsURL)
		if err != nil {
			log.Printf("WARN  homepage_extract [run %d team %d]: metrics_panel discover failed: %v", runID, teamID, err)
			continue
		}
		_, _ = e.store.UpsertSourceConfig(ctx, catID, nullTeamID, "metrics_panel",
			sql.NullString{}, "ai_extracted")
	}

	// 10. Mark done.
	log.Printf("INFO  homepage_extract [run %d team %d]: done", runID, teamID)
	_ = e.store.UpdateSyncRun(ctx, runID, "done", sql.NullString{})
}

// discoverSingleURL ensures the given URL is in the catalogue, returning the catalogue ID.
// If a catalogue item already exists with that URL, it returns its ID.
// Otherwise it runs discovery inline for the URL.
func (e *Engine) discoverSingleURL(ctx context.Context, rawURL string) (int64, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return 0, fmt.Errorf("empty URL")
	}

	// Check if a catalogue item already exists with this URL.
	existing, err := e.store.GetCatalogueItemByURL(ctx, rawURL)
	if err == nil {
		return existing.ID, nil
	}
	if err != sql.ErrNoRows {
		return 0, fmt.Errorf("check catalogue by url: %w", err)
	}

	// Auto-detect scope from the URL.
	scope, target, err := detectScope(rawURL)
	if err != nil {
		return 0, fmt.Errorf("detect scope for %q: %w", rawURL, err)
	}

	// Discover inline.
	var items []connector.DiscoveredItem
	switch scope {
	case "notion_workspace":
		items, err = e.notion.Discover(ctx, target)
	case "github_repo":
		items, err = e.github.Discover(ctx, target)
	default:
		return 0, fmt.Errorf("discoverSingleURL: unsupported scope %s", scope)
	}
	if err != nil {
		return 0, fmt.Errorf("discover %s: %w", scope, err)
	}

	// Upsert all discovered items; find the one matching the original URL.
	var matchedID int64
	for _, item := range items {
		metaStr := sql.NullString{}
		if item.SourceMeta != nil {
			if b, merr := json.Marshal(item.SourceMeta); merr == nil {
				metaStr = sql.NullString{String: string(b), Valid: true}
			}
		}
		result, uerr := e.store.UpsertCatalogueItem(ctx,
			item.SourceType, item.ExternalID, item.Title,
			sql.NullString{String: item.URL, Valid: item.URL != ""},
			metaStr,
			sql.NullInt64{},
		)
		if uerr != nil {
			continue
		}
		if item.URL == rawURL && matchedID == 0 {
			matchedID = result.ID
		}
	}

	if matchedID == 0 {
		// Try looking up by URL again after discovery.
		found, ferr := e.store.GetCatalogueItemByURL(ctx, rawURL)
		if ferr == nil {
			return found.ID, nil
		}
		return 0, fmt.Errorf("discoverSingleURL: item with URL %q not found after discovery", rawURL)
	}
	return matchedID, nil
}

