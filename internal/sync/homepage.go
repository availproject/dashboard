package sync

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"strings"
	"time"

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
	log.Printf("INFO  homepage_extract [run %d team %d]: AI result — goals_doc=%q sprint_plans=%d repos=%d project_boards=%d metrics=%d",
		runID, teamID, goalsURL, len(result.SprintPlans), len(result.Repos), len(result.ProjectBoards), len(result.Metrics))
	for i, sp := range result.SprintPlans {
		log.Printf("INFO  homepage_extract [run %d team %d]:   sprint[%d] status=%q title=%q url=%q", runID, teamID, i, sp.SprintStatus, sp.Title, sp.URL)
	}
	for i, r := range result.Repos {
		log.Printf("INFO  homepage_extract [run %d team %d]:   repo[%d] url=%q", runID, teamID, i, r)
	}
	for i, b := range result.ProjectBoards {
		log.Printf("INFO  homepage_extract [run %d team %d]:   project_board[%d] url=%q", runID, teamID, i, b)
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

		// Compute plan_start_date from the anchor and store it in sprint_meta.
		// Anchor: sprint_start_date is when the active sprint week began;
		// active_sprint_week is its 1-based position within the plan.
		// plan_start_date = sprint_start_date - (active_sprint_week - 1) * 7 days.
		if sp.SprintStatus == "current" {
			planStartDate := sql.NullString{}
			if sp.SprintStartDate != nil && *sp.SprintStartDate != "" && sp.ActiveSprintWeek != nil && *sp.ActiveSprintWeek > 0 {
				if anchor, err := time.Parse("2006-01-02", *sp.SprintStartDate); err == nil {
					planStart := anchor.AddDate(0, 0, -(*sp.ActiveSprintWeek-1)*7)
					planStartDate = sql.NullString{String: planStart.Format("2006-01-02"), Valid: true}
				}
			}
			sprintWeek, sprintStart, totalWeeks := 0, "", 0
			if sp.ActiveSprintWeek != nil {
				sprintWeek = *sp.ActiveSprintWeek
			}
			if sp.SprintStartDate != nil {
				sprintStart = *sp.SprintStartDate
			}
			if sp.TotalWeeksInPlan != nil {
				totalWeeks = *sp.TotalWeeksInPlan
			}
			log.Printf("INFO  homepage_extract [run %d team %d]: sprint anchor: sprint_week=%d start=%q total=%d → plan_start_date=%q",
				runID, teamID, sprintWeek, sprintStart, totalWeeks, planStartDate.String)
			_, _ = e.store.UpsertSprintMeta(ctx, teamID, "current",
				sql.NullInt64{}, planStartDate, sql.NullString{}, sql.NullString{})
		}
	}

	// 8. Upsert GitHub project boards and their linked repos.
	for _, boardURL := range result.ProjectBoards {
		if boardURL == "" {
			continue
		}
		normalizedURL := normalizeGitHubProjectURL(boardURL)
		_, target, err := detectScope(normalizedURL)
		if err != nil {
			log.Printf("WARN  homepage_extract [run %d team %d]: github_project scope detect failed for %q: %v", runID, teamID, normalizedURL, err)
			continue
		}
		log.Printf("INFO  homepage_extract [run %d team %d]: discovering github_project %q", runID, teamID, normalizedURL)
		discovered, err := e.github.DiscoverProject(ctx, target)
		if err != nil {
			log.Printf("WARN  homepage_extract [run %d team %d]: github_project discover failed: %v", runID, teamID, err)
			continue
		}

		// Upsert all discovered items into the catalogue.
		catalogueIDs := map[string]int64{} // "sourceType\x00externalID" → catalogue id
		for _, di := range discovered {
			metaStr := sql.NullString{}
			if di.SourceMeta != nil {
				if b, merr := json.Marshal(di.SourceMeta); merr == nil {
					metaStr = sql.NullString{String: string(b), Valid: true}
				}
			}
			cat, uerr := e.store.UpsertCatalogueItem(ctx,
				di.SourceType, di.ExternalID, di.Title,
				sql.NullString{String: di.URL, Valid: di.URL != ""},
				metaStr, sql.NullInt64{},
			)
			if uerr != nil {
				continue
			}
			catalogueIDs[di.SourceType+"\x00"+di.ExternalID] = cat.ID
		}

		// Configure the project board itself.
		// Check for existing manual/configured boards — if present, skip AI configuration
		// so the user's explicit choice is never overridden.
		existingBoardConfigs, _ := e.store.GetConfigsByPurpose(ctx, nullTeamID, "github_project")
		hasManualBoard := false
		for _, ec := range existingBoardConfigs {
			if ec.Provenance == "manual" || ec.Provenance == "configured" {
				hasManualBoard = true
				break
			}
		}
		for _, di := range discovered {
			if di.SourceType != "github_project" {
				continue
			}
			catID := catalogueIDs["github_project\x00"+di.ExternalID]
			if catID == 0 {
				continue
			}
			if hasManualBoard {
				log.Printf("INFO  homepage_extract [run %d team %d]: skipping github_project catalogue id %d (manual board already set)", runID, teamID, catID)
				continue
			}
			// Remove any stale ai_extracted board configs pointing to a different catalogue item.
			for _, ec := range existingBoardConfigs {
				if ec.Provenance == "ai_extracted" && ec.CatalogueID != catID {
					log.Printf("INFO  homepage_extract [run %d team %d]: replacing stale ai_extracted github_project config %d", runID, teamID, ec.ID)
					_ = e.store.DeleteSourceConfig(ctx, ec.ID)
				}
			}
			log.Printf("INFO  homepage_extract [run %d team %d]: github_project → catalogue id %d", runID, teamID, catID)
			_, _ = e.store.UpsertSourceConfig(ctx, catID, nullTeamID, "github_project",
				sql.NullString{}, "ai_extracted")
		}

		// Configure linked repos so issue fetching works without manual entries.
		for _, di := range discovered {
			if di.SourceType != "github_repo" {
				continue
			}
			catID := catalogueIDs["github_repo\x00"+di.ExternalID]
			if catID == 0 {
				continue
			}
			log.Printf("INFO  homepage_extract [run %d team %d]: github_project linked repo %q → catalogue id %d", runID, teamID, di.ExternalID, catID)
			_, _ = e.store.UpsertSourceConfig(ctx, catID, nullTeamID, "github_repo",
				sql.NullString{}, "ai_extracted")
		}
	}

	// 9. Upsert GitHub repos.
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

	// 10. Upsert metrics panels.
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

	// 11. Mark done.
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
	case "github_project":
		items, err = e.github.DiscoverProject(ctx, target)
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

// normalizeGitHubProjectURL strips view and query params from a GitHub project
// board URL, returning the canonical https://github.com/orgs/{org}/projects/{n} form.
// Returns the input unchanged if it does not match the expected pattern.
func normalizeGitHubProjectURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host != "github.com" {
		return rawURL
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	// Expected: orgs/{org}/projects/{n}[/...]
	if len(parts) >= 4 && parts[0] == "orgs" && parts[2] == "projects" {
		return fmt.Sprintf("https://github.com/orgs/%s/projects/%s", parts[1], parts[3])
	}
	return rawURL
}

