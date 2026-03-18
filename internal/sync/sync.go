package sync

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	gh "github.com/google/go-github/v60/github"
	"github.com/your-org/dashboard/internal/pipeline"
	"github.com/your-org/dashboard/internal/store"
	githubconn "github.com/your-org/dashboard/internal/connector/github"
	notionconn "github.com/your-org/dashboard/internal/connector/notion"
)

// teamSyncData holds source content fetched for a single team's incremental sync.
type teamSyncData struct {
	currentPlanText    string
	goalsDocText       string
	boardItems         []githubconn.BoardItem // current-sprint filtered (workload, velocity, activity snapshot)
	allBoardItems      []githubconn.BoardItem // team-area filtered, all sprints (team_status, auto_close)
	mergedPRs          []*gh.PullRequest
	commits            []*gh.RepositoryCommit
	marketingCampaigns []notionconn.MarketingCampaign
}

// Sync runs an incremental sync for the given scope and optional teamID.
// If a sync is already running for the same scope+teamID, the existing syncRunID is returned.
func (e *Engine) Sync(ctx context.Context, scope string, teamID *int64) (int64, error) {
	var nullTeamID sql.NullInt64
	if teamID != nil {
		nullTeamID = sql.NullInt64{Int64: *teamID, Valid: true}
	}

	// Return existing run if one is already in progress.
	existing, err := e.store.GetRunningSyncRun(ctx, scope, nullTeamID)
	if err == nil {
		return existing.ID, nil
	}
	if err != sql.ErrNoRows {
		return 0, fmt.Errorf("sync: check running: %w", err)
	}

	run, err := e.store.CreateSyncRun(ctx, nullTeamID, scope)
	if err != nil {
		return 0, fmt.Errorf("sync: create run: %w", err)
	}

	go e.syncBackground(run.ID, scope, teamID)
	return run.ID, nil
}

func (e *Engine) syncBackground(runID int64, scope string, teamID *int64) {
	ctx := context.Background()
	errs := make(map[string]string)
	timings := newSyncTimings()
	syncStart := time.Now()
	log.Printf("INFO  sync [run %d scope=%s]: started", runID, scope)

	switch scope {
	case "org":
		e.syncOrg(ctx, errs, timings)
	default: // "team"
		if teamID == nil {
			_ = e.store.UpdateSyncRun(ctx, runID, "error", sql.NullString{
				String: "team scope requires non-nil teamID", Valid: true,
			})
			return
		}
		td := e.fetchTeamData(ctx, *teamID, errs, timings, "")
		e.runTeamPipelines(ctx, *teamID, td, errs, timings, "")
	}

	timings.record("total_ms", syncStart)
	_ = e.store.SaveSyncRunTimings(ctx, runID, timings.toJSON())

	if len(errs) > 0 {
		b, _ := json.Marshal(errs)
		log.Printf("INFO  sync [run %d]: done in %s with errors: %s", runID, time.Since(syncStart).Round(time.Millisecond), string(b))
		_ = e.store.UpdateSyncRun(ctx, runID, "error", sql.NullString{String: string(b), Valid: true})
	} else {
		log.Printf("INFO  sync [run %d]: done in %s", runID, time.Since(syncStart).Round(time.Millisecond))
		_ = e.store.UpdateSyncRun(ctx, runID, "done", sql.NullString{})
	}
}

func (e *Engine) syncOrg(ctx context.Context, errs map[string]string, timings *syncTimings) {
	teams, err := e.store.ListTeams(ctx)
	if err != nil {
		errs["list_teams"] = err.Error()
		return
	}

	teamGoals := make(map[int64][]string)
	for _, team := range teams {
		prefix := fmt.Sprintf("team_%d:", team.ID)
		td := e.fetchTeamData(ctx, team.ID, errs, timings, prefix)
		goals := e.runTeamPipelines(ctx, team.ID, td, errs, timings, prefix)
		if len(goals) > 0 {
			teamGoals[team.ID] = goals
		}
	}

	orgGoalsText := e.fetchOrgGoalsText(ctx, errs)
	_, _ = e.pipeline.RunGoalAlignment(ctx, orgGoalsText, teamGoals)
}

// fetchOrgGoalsText loads org-level (team_id IS NULL) notion sources for org_goals/org_milestones.
func (e *Engine) fetchOrgGoalsText(ctx context.Context, errs map[string]string) string {
	configs, err := e.store.GetSourceConfigsForScope(ctx, sql.NullInt64{})
	if err != nil {
		return ""
	}

	var sb strings.Builder
	for _, cfg := range configs {
		if cfg.Purpose != "org_goals" && cfg.Purpose != "org_milestones" {
			continue
		}
		item, err := e.store.GetCatalogueItem(ctx, cfg.CatalogueID)
		if err != nil {
			continue
		}
		content, fetchErr := e.fetchNotionContent(ctx, item)
		if fetchErr != nil {
			errs[fmt.Sprintf("%s:%s", item.SourceType, item.ExternalID)] = fetchErr.Error()
			continue
		}
		sb.WriteString(content)
		sb.WriteString("\n")
		_ = e.store.TouchCatalogueItem(ctx, item.ID)
	}
	return sb.String()
}

// notionFetchWork holds everything needed to do one Notion/github_file fetch in a goroutine.
type notionFetchWork struct {
	cfg  *store.SourceConfig
	item *store.SourceCatalogue
}

// notionFetchResult carries the result back from a goroutine.
type notionFetchResult struct {
	cfg     *store.SourceConfig
	item    *store.SourceCatalogue
	content string
	err     error
	elapsed time.Duration
}

// fetchTeamData loads source_configs for a team, fetches each source incrementally, and
// returns the aggregated data. Per-source errors are recorded in errs; the run is not aborted.
// Notion/github_file fetches run in parallel to reduce latency.
// prefix is prepended to all timing keys (used to namespace per-team in org syncs).
func (e *Engine) fetchTeamData(ctx context.Context, teamID int64, errs map[string]string, timings *syncTimings, prefix string) teamSyncData {
	var td teamSyncData

	configs, err := e.store.GetSourceConfigsForScope(ctx, sql.NullInt64{Int64: teamID, Valid: true})
	if err != nil {
		errs[fmt.Sprintf("team_%d_configs", teamID)] = err.Error()
		return td
	}

	// Determine marketing parameters (marketing_label + marketing_calendar DB ID).
	// Done upfront so we can launch the marketing DB query in parallel with content fetches.
	type marketingParams struct {
		label string
		dbID  string
	}
	var mkt *marketingParams
	if team, err := e.store.GetTeam(ctx, teamID); err == nil && team.MarketingLabel.Valid && team.MarketingLabel.String != "" {
		for _, cfg := range configs {
			if cfg.Purpose == "marketing_calendar" {
				if item, err := e.store.GetCatalogueItem(ctx, cfg.CatalogueID); err == nil {
					mkt = &marketingParams{label: team.MarketingLabel.String, dbID: item.ExternalID}
					break
				}
			}
		}
		if mkt == nil {
			log.Printf("INFO  sync [team %d]: marketing_label set to %q but no marketing_calendar source configured; skipping",
				teamID, team.MarketingLabel.String)
		}
	}

	// Separate configs into content sources (fetch in parallel) and GitHub sources (sequential).
	var contentWork []notionFetchWork
	var githubConfigs []*store.SourceConfig
	githubItems := map[int64]*store.SourceCatalogue{}

	// Build set of catalogue IDs that serve as the project homepage — they should
	// never be fetched as content sources even if they carry an old purpose tag.
	homepageIDs := map[int64]bool{}
	for _, cfg := range configs {
		if cfg.Purpose == "project_homepage" {
			homepageIDs[cfg.CatalogueID] = true
		}
	}

	// Track seen catalogue IDs to avoid fetching the same item twice when old
	// and new purpose configs both exist for the same underlying page.
	seenContentIDs := map[int64]bool{}

	for _, cfg := range configs {
		if cfg.Purpose == "project_homepage" || cfg.Purpose == "metrics_panel" {
			continue
		}
		item, err := e.store.GetCatalogueItem(ctx, cfg.CatalogueID)
		if err != nil {
			continue
		}
		if homepageIDs[item.ID] {
			continue // skip items that are the homepage regardless of purpose
		}
		switch item.SourceType {
		case "notion_page", "notion_db", "github_file":
			// Only fetch if this purpose produces sync content.
			switch cfg.Purpose {
			case "current_plan", "sprint_doc", "goals", "goals_doc":
				if !seenContentIDs[item.ID] {
					seenContentIDs[item.ID] = true
					contentWork = append(contentWork, notionFetchWork{cfg: cfg, item: item})
				}
			}
		case "github_repo", "github_project":
			githubConfigs = append(githubConfigs, cfg)
			githubItems[cfg.ID] = item
		}
	}

	if !hasGithubProject(githubConfigs, githubItems) {
		log.Printf("WARN  sync [team %d]: no github_project source configured; board items will be empty", teamID)
	}

	// Launch marketing DB query in parallel with content fetches.
	// FetchMarketingCampaignsMeta is a single Notion API call; running it concurrently
	// with the content fetch goroutines hides most of its latency.
	type mktMetaResult struct {
		metas []notionconn.CampaignMeta
		err   error
		ms    int64
	}
	mktMetaCh := make(chan mktMetaResult, 1)
	if mkt != nil {
		go func() {
			t0 := time.Now()
			metas, err := e.notion.FetchMarketingCampaignsMeta(ctx, mkt.dbID, mkt.label)
			mktMetaCh <- mktMetaResult{metas: metas, err: err, ms: time.Since(t0).Milliseconds()}
		}()
	} else {
		mktMetaCh <- mktMetaResult{}
	}

	// Fetch Notion/file content in parallel.
	fetchStart := time.Now()
	log.Printf("INFO  sync [team %d]: fetching %d content source(s) in parallel", teamID, len(contentWork))
	results := make(chan notionFetchResult, len(contentWork))
	var wg sync.WaitGroup
	for _, w := range contentWork {
		wg.Add(1)
		go func(w notionFetchWork) {
			defer wg.Done()
			t0 := time.Now()
			var content string
			var err error
			if w.item.SourceType == "github_file" {
				content, err = e.fetchGithubFileContent(ctx, w.item)
			} else {
				content, err = e.fetchNotionContent(ctx, w.item)
			}
			elapsed := time.Since(t0)
			cached := w.item.NotionLastEdited.Valid && err == nil && elapsed < 500*time.Millisecond
			cacheLabel := "fresh"
			if cached {
				cacheLabel = "cached"
			}
			log.Printf("INFO  sync [team %d]: fetched %s:%s (%d chars, %s) in %s",
				teamID, w.item.SourceType, w.item.ExternalID, len(content), cacheLabel, elapsed.Round(time.Millisecond))
			results <- notionFetchResult{cfg: w.cfg, item: w.item, content: content, err: err, elapsed: elapsed}
		}(w)
	}
	go func() { wg.Wait(); close(results) }()

	// sprint_doc(current) takes priority over current_plan.
	var currentPlanFallback string
	for r := range results {
		key := fmt.Sprintf("%s:%s", r.item.SourceType, r.item.ExternalID)
		timings.set(prefix+"content:"+key+"_ms", r.elapsed.Milliseconds())
		if r.err != nil {
			errs[key] = r.err.Error()
			continue
		}
		_ = e.store.TouchCatalogueItem(ctx, r.item.ID)
		switch r.cfg.Purpose {
		case "current_plan":
			currentPlanFallback = r.content
		case "sprint_doc":
			meta := parseJSONMeta(r.cfg.ConfigMeta)
			if status, _ := meta["sprint_status"].(string); status == "current" {
				td.currentPlanText = r.content
			}
		case "goals", "goals_doc":
			td.goalsDocText = r.content
		}
	}
	if td.currentPlanText == "" && currentPlanFallback != "" {
		td.currentPlanText = currentPlanFallback
	}

	timings.record(prefix+"content_fetch_ms", fetchStart)
	log.Printf("INFO  sync [team %d]: content fetch done in %s", teamID, time.Since(fetchStart).Round(time.Millisecond))

	// Collect marketing meta result (was running in parallel with content fetch).
	mktMeta := <-mktMetaCh
	if mkt != nil {
		timings.set(prefix+"marketing_meta_ms", mktMeta.ms)
		if mktMeta.err != nil {
			errs["marketing_meta"] = mktMeta.err.Error()
			log.Printf("WARN  sync [team %d]: marketing meta fetch: %v", teamID, mktMeta.err)
		}
	}

	// Start marketing task-page fetches in a background goroutine.
	// These are independent of GitHub and run in parallel with github_fetch.
	type mktTasksResult struct {
		campaigns []notionconn.MarketingCampaign
		ms        int64
	}
	mktTasksCh := make(chan mktTasksResult, 1)
	if mkt != nil && mktMeta.err == nil && len(mktMeta.metas) > 0 {
		go func() {
			t0 := time.Now()
			campaigns := e.fetchMarketingTasksCached(ctx, teamID, mkt.label, mktMeta.metas, errs)
			mktTasksCh <- mktTasksResult{campaigns: campaigns, ms: time.Since(t0).Milliseconds()}
		}()
	} else {
		mktTasksCh <- mktTasksResult{}
	}

	// GitHub sources: fetch all repos and the project board in parallel.
	// PRs, commits, and board items are independent across sources.
	log.Printf("INFO  sync [team %d]: fetching %d github source(s) in parallel", teamID, len(githubConfigs))
	existingMembers, _ := e.store.GetTeamMembers(ctx, teamID)
	knownLogins := map[string]bool{}
	for _, m := range existingMembers {
		if m.GithubLogin.Valid && m.GithubLogin.String != "" {
			knownLogins[strings.ToLower(m.GithubLogin.String)] = true
		}
	}
	log.Printf("INFO  sync [team %d]: %d existing members with github logins", teamID, len(knownLogins))

	var ghMu sync.Mutex // protects td.mergedPRs/commits/boardItems, errs, knownLogins
	var ghWg sync.WaitGroup
	githubStart := time.Now()

	for _, cfg := range githubConfigs {
		item := githubItems[cfg.ID]
		since := item.UpdatedAt
		key := fmt.Sprintf("%s:%s", item.SourceType, item.ExternalID)

		switch item.SourceType {
		case "github_repo":
			owner, repo := parseOwnerRepo(item)
			if owner == "" || repo == "" {
				errs[key+":no_owner_repo"] = "missing owner/repo in source_meta or external_id"
				continue
			}
			log.Printf("INFO  sync [team %d]: github_repo %s/%s (since %s)", teamID, owner, repo, since.Format("2006-01-02"))

			// PRs fetch
			ghWg.Add(1)
			go func(key, owner, repo string, since time.Time) {
				defer ghWg.Done()
				t0 := time.Now()
				prs, fetchErr := e.github.FetchMergedPRs(ctx, owner, repo, since)
				timings.record(prefix+"github:"+key+":prs_ms", t0)
				if fetchErr != nil {
					ghMu.Lock()
					errs[key+":prs"] = fetchErr.Error()
					ghMu.Unlock()
					log.Printf("WARN  sync [team %d]: %s fetch PRs: %v", teamID, key, fetchErr)
					return
				}
				log.Printf("INFO  sync [team %d]: %s: %d merged PRs", teamID, key, len(prs))
				ghMu.Lock()
				td.mergedPRs = append(td.mergedPRs, prs...)
				ghMu.Unlock()
			}(key, owner, repo, since)

			// Commits fetch (also handles member discovery and TouchCatalogueItem)
			ghWg.Add(1)
			go func(key, owner, repo string, since time.Time, itemID int64) {
				defer ghWg.Done()
				commitSince := since
				if floor := time.Now().AddDate(0, 0, -90); since.After(floor) {
					commitSince = floor
				}
				t0 := time.Now()
				commits, fetchErr := e.github.FetchCommits(ctx, owner, repo, commitSince, time.Now(), nil)
				timings.record(prefix+"github:"+key+":commits_ms", t0)
				if fetchErr != nil {
					ghMu.Lock()
					errs[key+":commits"] = fetchErr.Error()
					ghMu.Unlock()
					log.Printf("WARN  sync [team %d]: %s fetch commits: %v", teamID, key, fetchErr)
					return
				}

				// Collect new members under the lock, then write to DB outside it.
				type newMember struct{ login, name string }
				var newMembers []newMember
				ghMu.Lock()
				for _, commit := range commits {
					if commit.Author == nil {
						continue
					}
					login := strings.ToLower(commit.Author.GetLogin())
					if login == "" || knownLogins[login] {
						continue
					}
					name := login
					if c := commit.GetCommit(); c != nil {
						if n := c.GetAuthor().GetName(); n != "" {
							name = n
						}
					}
					knownLogins[login] = true // mark before releasing lock
					newMembers = append(newMembers, newMember{login, name})
				}
				td.commits = append(td.commits, commits...)
				ghMu.Unlock()

				for _, nm := range newMembers {
					_ = e.store.UpsertMemberByGithubLogin(ctx, teamID, nm.login, nm.name)
					log.Printf("INFO  sync [team %d]: auto-added member %q (%s) from commits", teamID, nm.name, nm.login)
				}
				log.Printf("INFO  sync [team %d]: %s: %d commits, %d new members discovered", teamID, key, len(commits), len(newMembers))
				_ = e.store.TouchCatalogueItem(ctx, itemID)
			}(key, owner, repo, since, item.ID)

		case "github_project":
			meta := parseJSONMeta(item.SourceMeta)
			pid, _ := meta["project_id"].(string)
			if pid == "" {
				errs[key+":no_project_id"] = "missing project_id in source_meta"
				continue
			}
			ghWg.Add(1)
			go func(key, pid string, cfg *store.SourceConfig, itemID int64) {
				defer ghWg.Done()
				t0 := time.Now()
				items, fetchErr := e.github.FetchProjectItems(ctx, pid)
				timings.record(prefix+"github:"+key+":board_ms", t0)
				if fetchErr != nil {
					ghMu.Lock()
					errs[key+":board"] = fetchErr.Error()
					ghMu.Unlock()
					log.Printf("WARN  sync [team %d]: %s fetch board: %v", teamID, key, fetchErr)
					return
				}
				bcfg := parseBoardConfig(cfg.ConfigMeta)
				// allItems: team-area filter only — all sprints visible to team_status and auto_close.
				// currentItems: also sprint-window filtered — used for workload, velocity, activity snapshot.
				allItems := applyAreaFilter(items, bcfg)
				currentItems := applySprintFilter(allItems, bcfg, time.Now())
				log.Printf("INFO  sync [team %d]: %s: %d board items (%d in team area, %d in current sprint) in %s",
					teamID, key, len(items), len(allItems), len(currentItems), time.Since(t0).Round(time.Millisecond))
				ghMu.Lock()
				td.allBoardItems = append(td.allBoardItems, allItems...)
				td.boardItems = append(td.boardItems, currentItems...)
				ghMu.Unlock()
				_ = e.store.TouchCatalogueItem(ctx, itemID)
			}(key, pid, cfg, item.ID)
		}
	}
	ghWg.Wait()
	timings.record(prefix+"github_fetch_ms", githubStart)

	// Consolidate permission-denied repos into a single log line and a dedicated
	// error key so the UI can surface them clearly.
	permDeniedSet := map[string]bool{}
	for _, errMsg := range errs {
		if strings.HasPrefix(errMsg, "permission denied: ") {
			permDeniedSet[strings.TrimPrefix(errMsg, "permission denied: ")] = true
		}
	}
	if len(permDeniedSet) > 0 {
		repos := make([]string, 0, len(permDeniedSet))
		for r := range permDeniedSet {
			repos = append(repos, r)
		}
		sort.Strings(repos)
		log.Printf("WARN  sync [team %d]: %d repo(s) inaccessible due to token permissions: %s",
			teamID, len(repos), strings.Join(repos, ", "))
		errs["github:perm_denied"] = strings.Join(repos, ",")
	}

	// Auto-close board items that are terminal-status but still open on GitHub.
	// Uses allBoardItems so items from any sprint (not just current) are caught.
	autoCloseStart := time.Now()
	terminalStatuses := map[string]bool{"Done": true, "Won't Do": true, "Not Complete": true}
	for _, bi := range td.allBoardItems {
		if bi.State != "open" || !terminalStatuses[bi.Status] {
			continue
		}
		if bi.Owner == "" || bi.Repo == "" {
			log.Printf("WARN  sync [team %d]: issue #%d %q project_status=%q but no repo; skipping close", teamID, bi.Number, bi.Title, bi.Status)
			continue
		}
		if err := e.github.CloseIssue(ctx, bi.Owner, bi.Repo, bi.Number); err != nil {
			log.Printf("WARN  sync [team %d]: close issue #%d: %v", teamID, bi.Number, err)
		} else {
			log.Printf("INFO  sync [team %d]: closed issue #%d %q (project_status=%q)", teamID, bi.Number, bi.Title, bi.Status)
		}
	}
	timings.record(prefix+"auto_close_ms", autoCloseStart)

	// Collect marketing tasks result (was running in parallel with github_fetch).
	mktResult := <-mktTasksCh
	if mkt != nil {
		timings.set(prefix+"marketing_tasks_ms", mktResult.ms)
		td.marketingCampaigns = mktResult.campaigns
	}

	// Persist activity and marketing snapshots for the API.
	e.saveActivitySnapshot(ctx, teamID, &td)
	e.saveMarketingSnapshot(ctx, teamID, td.marketingCampaigns)

	return td
}

// fetchMarketingTasksCached fetches all task pages for the given campaign metas
// in parallel, using per-page caching to skip re-fetching block content for
// unchanged pages. Returns fully-assembled MarketingCampaign values.
func (e *Engine) fetchMarketingTasksCached(
	ctx context.Context,
	teamID int64,
	label string,
	metas []notionconn.CampaignMeta,
	errs map[string]string,
) []notionconn.MarketingCampaign {
	// Collect unique task page IDs with their campaign index.
	type taskRef struct {
		taskID      string
		campaignIdx int
	}
	var refs []taskRef
	seen := map[string]bool{}
	for i, m := range metas {
		for _, id := range m.TaskIDs {
			if !seen[id] {
				seen[id] = true
				refs = append(refs, taskRef{taskID: id, campaignIdx: i})
			}
		}
	}

	type taskResult struct {
		campaignIdx int
		task        notionconn.MarketingTask
	}
	resultCh := make(chan taskResult, len(refs))
	var wg sync.WaitGroup
	for _, ref := range refs {
		wg.Add(1)
		go func(r taskRef) {
			defer wg.Done()
			cached, _ := e.store.GetMarketingPageCache(ctx, r.taskID)
			knownLastEdited := ""
			if cached != nil {
				knownLastEdited = cached.LastEdited
			}
			task, lastEdited, changed, err := e.notion.FetchMarketingTaskCached(ctx, r.taskID, knownLastEdited)
			if err != nil {
				log.Printf("WARN  sync [team %d]: fetch marketing task %s: %v", teamID, r.taskID, err)
				return
			}
			if !changed && cached != nil {
				// Page unchanged — use cached block content.
				task.Body = cached.Body
			} else if changed {
				// Store updated content in cache.
				_ = e.store.UpsertMarketingPageCache(ctx, store.MarketingPageCache{
					PageID:     r.taskID,
					LastEdited: lastEdited,
					Title:      task.Title,
					Status:     task.Status,
					Assignee:   task.Assignee,
					Body:       task.Body,
				})
			}
			resultCh <- taskResult{campaignIdx: r.campaignIdx, task: task}
		}(ref)
	}
	go func() { wg.Wait(); close(resultCh) }()

	// Assemble campaign structs from metadata + fetched tasks.
	campaigns := make([]notionconn.MarketingCampaign, len(metas))
	for i, m := range metas {
		campaigns[i] = notionconn.MarketingCampaign{
			PageID:    m.PageID,
			Title:     m.Title,
			Status:    m.Status,
			DateStart: m.DateStart,
			DateEnd:   m.DateEnd,
		}
	}
	for tr := range resultCh {
		campaigns[tr.campaignIdx].Tasks = append(campaigns[tr.campaignIdx].Tasks, tr.task)
	}

	log.Printf("INFO  sync [team %d]: assembled %d marketing campaign(s) with %d task page(s) (label=%q)",
		teamID, len(campaigns), len(refs), label)
	return campaigns
}

// GetMarketingLabels returns the available project label options from the team's
// configured marketing calendar Notion database. Returns an error if no
// marketing_calendar source is configured for the team or the Notion call fails.
func (e *Engine) GetMarketingLabels(ctx context.Context, teamID int64) ([]string, error) {
	configs, err := e.store.GetSourceConfigsForScope(ctx, sql.NullInt64{Int64: teamID, Valid: true})
	if err != nil {
		return nil, fmt.Errorf("get source configs: %w", err)
	}
	var dbID string
	for _, cfg := range configs {
		if cfg.Purpose == "marketing_calendar" {
			item, err := e.store.GetCatalogueItem(ctx, cfg.CatalogueID)
			if err == nil {
				dbID = item.ExternalID
				break
			}
		}
	}
	if dbID == "" {
		return nil, fmt.Errorf("no marketing_calendar source configured for team %d", teamID)
	}
	return e.notion.FetchProjectLabels(ctx, dbID)
}

// GetBoardFields looks up the team's configured github_project source and returns
// the available fields (with options) from the GitHub ProjectV2 board.
func (e *Engine) GetBoardFields(ctx context.Context, teamID int64) ([]githubconn.ProjectField, error) {
	configs, err := e.store.GetSourceConfigsForScope(ctx, sql.NullInt64{Int64: teamID, Valid: true})
	if err != nil {
		return nil, fmt.Errorf("get source configs: %w", err)
	}
	for _, cfg := range configs {
		if cfg.Purpose != "github_project" {
			continue
		}
		item, err := e.store.GetCatalogueItem(ctx, cfg.CatalogueID)
		if err != nil {
			continue
		}
		meta := parseJSONMeta(item.SourceMeta)
		pid, _ := meta["project_id"].(string)
		if pid == "" {
			continue
		}
		return e.github.FetchProjectFields(ctx, pid)
	}
	return nil, fmt.Errorf("no github_project source configured for team %d", teamID)
}

// fetchGithubFileContent retrieves the raw content of a github_file catalogue item.
// The owner, repo, and path are parsed from source_meta.
func (e *Engine) fetchGithubFileContent(ctx context.Context, item *store.SourceCatalogue) (string, error) {
	_ = ctx
	meta := parseJSONMeta(item.SourceMeta)
	_, _ = meta["owner"].(string)
	_, _ = meta["repo"].(string)
	_, _ = meta["path"].(string)
	return "", fmt.Errorf("github_file fetch not yet implemented")
}

// fetchSourceContent retrieves text content from a catalogue item.
// Handles notion_page, notion_db, and github_file.
func (e *Engine) fetchSourceContent(ctx context.Context, item *store.SourceCatalogue) (string, error) {
	switch item.SourceType {
	case "github_file":
		return e.fetchGithubFileContent(ctx, item)
	default:
		return e.fetchNotionContent(ctx, item)
	}
}

// fetchNotionContent retrieves text content from a notion_page or notion_db catalogue item.
// For notion_page, uses cached content when the page hasn't changed since the last fetch.
func (e *Engine) fetchNotionContent(ctx context.Context, item *store.SourceCatalogue) (string, error) {
	switch item.SourceType {
	case "notion_page":
		content, lastEdited, changed, err := e.notion.FetchPageIfChanged(ctx, item.ExternalID, item.NotionLastEdited.String)
		if err != nil {
			return "", err
		}
		if !changed {
			// Page unchanged — return cached content.
			return item.RawContent.String, nil
		}
		// Store new content and last-edited timestamp for next sync.
		_ = e.store.UpdateCatalogueContent(ctx, item.ID, content, lastEdited)
		return content, nil
	case "notion_db":
		rows, err := e.notion.FetchDatabase(ctx, item.ExternalID, item.UpdatedAt)
		if err != nil {
			return "", err
		}
		var parts []string
		for _, row := range rows {
			if row.Content != "" {
				parts = append(parts, row.Content)
			}
		}
		return strings.Join(parts, "\n"), nil
	default:
		return "", fmt.Errorf("fetchNotionContent: unsupported source_type %s", item.SourceType)
	}
}

// runTeamPipelines executes the pipeline chain for a team in two parallel phases
// with a calendar step between them:
//
//	Phase 1 (parallel): sprint_parse, velocity_analysis
//	Calendar step (sequential): structural calendar writer, dates_extract
//	Phase 2 (parallel, after calendar step): team_status, workload_estimation
//
// Returns the extracted business goal texts for use in org-level goal_alignment.
func (e *Engine) runTeamPipelines(ctx context.Context, teamID int64, td teamSyncData, errs map[string]string, timings *syncTimings, prefix string) []string {
	var (
		mu        sync.Mutex
		goalTexts []string
	)

	// Phase 1: independent pipelines run concurrently.
	p1Start := time.Now()
	log.Printf("INFO  sync [team %d]: pipeline phase 1 start (sprint_parse, velocity)", teamID)
	var phase1 sync.WaitGroup

	var sprintParseResult *pipeline.SprintParseResult
	if td.currentPlanText != "" {
		phase1.Add(1)
		go func() {
			defer phase1.Done()
			t0 := time.Now()
			res, err := e.pipeline.RunSprintParse(ctx, teamID, td.currentPlanText)
			timings.record(prefix+"pipeline:sprint_parse_ms", t0)
			if err == nil {
				mu.Lock()
				sprintParseResult = res
				mu.Unlock()
			}
			log.Printf("INFO  sync [team %d]: sprint_parse done in %s", teamID, time.Since(t0).Round(time.Millisecond))
		}()
	}

	closedCount := 0
	for _, bi := range td.boardItems {
		if bi.State == "closed" {
			closedCount++
		}
	}
	phase1.Add(1)
	go func() {
		defer phase1.Done()
		t0 := time.Now()
		_, _ = e.pipeline.RunVelocityAnalysis(ctx, teamID, pipeline.VelocityInput{
			Sprints: []pipeline.VelocitySprint{
				{
					Label:        "current",
					ClosedIssues: closedCount,
					MergedPRs:    len(td.mergedPRs),
					CommitCount:  len(td.commits),
				},
			},
		})
		timings.record(prefix+"pipeline:velocity_analysis_ms", t0)
		log.Printf("INFO  sync [team %d]: velocity_analysis done in %s", teamID, time.Since(t0).Round(time.Millisecond))
	}()

	phase1.Wait()
	timings.record(prefix+"phase1_ms", p1Start)
	log.Printf("INFO  sync [team %d]: pipeline phase 1 done in %s", teamID, time.Since(p1Start).Round(time.Millisecond))

	// Calendar step: write structural events and run dates_extract.
	// Runs sequentially after phase 1 (needs sprint_meta from sprint_parse) and
	// before phase 2 (team_status needs the resulting calendar flags).
	calendarStart := time.Now()
	sprintMeta, _ := e.store.GetSprintMeta(ctx, teamID, "current")
	var calendarFlags []pipeline.CalendarEventFlag

	if err := e.writeStructuralCalendarEvents(ctx, teamID, td.marketingCampaigns); err != nil {
		log.Printf("WARN  sync [team %d]: structural calendar write: %v", teamID, err)
	}

	if td.currentPlanText != "" || td.goalsDocText != "" {
		totalSprints := 4
		if sprintParseResult != nil && sprintParseResult.TotalSprints > 0 {
			totalSprints = sprintParseResult.TotalSprints
		}
		sprintCal := buildSprintCalendar(sprintMeta, totalSprints)
		mktCampaigns := calendarCampaignsFromMarketing(td.marketingCampaigns)

		t0 := time.Now()
		datesResult, err := e.pipeline.RunDatesExtract(ctx, teamID, pipeline.DatesExtractInput{
			GoalsDocText:       td.goalsDocText,
			SprintPlanText:     td.currentPlanText,
			SprintCalendar:     sprintCal,
			MarketingCampaigns: mktCampaigns,
			Today:              time.Now().Format("2006-01-02"),
		})
		timings.record(prefix+"pipeline:dates_extract_ms", t0)
		if err != nil {
			log.Printf("WARN  sync [team %d]: dates_extract: %v", teamID, err)
		} else {
			log.Printf("INFO  sync [team %d]: dates_extract done in %s (%d event(s))", teamID, time.Since(t0).Round(time.Millisecond), len(datesResult.Events))
			synthesized := calendarEventsFromResult(datesResult)
			if err := e.store.ReplaceCalendarEvents(ctx, teamID, "synthesized", synthesized); err != nil {
				log.Printf("WARN  sync [team %d]: replace synthesized calendar events: %v", teamID, err)
			}
			// Collect all flags across events for team_status.
			for _, ev := range datesResult.Events {
				calendarFlags = append(calendarFlags, ev.Flags...)
			}
		}
	}
	timings.record(prefix+"calendar_step_ms", calendarStart)

	// Phase 2: pipelines that depend on phase 1 + calendar results.
	p2Start := time.Now()
	log.Printf("INFO  sync [team %d]: pipeline phase 2 start (team_status, workload)", teamID)
	members, _ := e.store.GetTeamMembers(ctx, teamID)

	var phase2 sync.WaitGroup

	if td.currentPlanText != "" || td.goalsDocText != "" {
		phase2.Add(1)
		go func() {
			defer phase2.Done()
			t0 := time.Now()
			var flagsInput any
			if len(calendarFlags) > 0 {
				flagsInput = calendarFlags
			}
			result, err := e.pipeline.RunTeamStatus(ctx, teamID, pipeline.TeamStatusInput{
				GoalsDocText:       td.goalsDocText,
				SprintPlanText:     td.currentPlanText,
				SprintMeta:         sprintMeta,
				OpenIssues:         boardItemsToAny(td.allBoardItems, time.Now()),
				MergedPRs:          prsToAny(td.mergedPRs),
				MarketingCampaigns: marketingCampaignsToAny(td.marketingCampaigns),
				CalendarFlags:      flagsInput,
			})
			timings.record(prefix+"pipeline:team_status_ms", t0)
			if err != nil {
				log.Printf("ERROR sync [team %d]: team_status: %v", teamID, err)
				mu.Lock()
				errs["team_status"] = err.Error()
				mu.Unlock()
				return
			}
			log.Printf("INFO  sync [team %d]: team_status done in %s", teamID, time.Since(t0).Round(time.Millisecond))
			mu.Lock()
			for _, g := range result.BusinessGoals {
				goalTexts = append(goalTexts, g.Text)
			}
			mu.Unlock()
		}()
	}

	if len(members) > 0 {
		phase2.Add(1)
		go func() {
			defer phase2.Done()
			t0 := time.Now()
			_, _ = e.pipeline.RunWorkloadEstimation(ctx, teamID, pipeline.WorkloadInput{
				Members:            buildWorkloadMembers(members, td.boardItems, td.mergedPRs, td.commits),
				SprintWindow:       sprintWindow(sprintMeta),
				StandardSprintDays: 5,
			})
			timings.record(prefix+"pipeline:workload_estimation_ms", t0)
			log.Printf("INFO  sync [team %d]: workload_estimation done in %s", teamID, time.Since(t0).Round(time.Millisecond))
		}()
	} else {
		log.Printf("INFO  sync [team %d]: skipping workload_estimation (no team members)", teamID)
	}

	phase2.Wait()
	timings.record(prefix+"phase2_ms", p2Start)
	log.Printf("INFO  sync [team %d]: pipeline phase 2 done in %s", teamID, time.Since(p2Start).Round(time.Millisecond))
	return goalTexts
}

// boardConfigMeta holds the filter configuration for a github_project source config.
type boardConfigMeta struct {
	TeamAreaField string `json:"team_area_field"` // field name, e.g. "Team / Area"
	TeamAreaValue string `json:"team_area_value"` // team's value, e.g. "Engineering"
	SprintField   string `json:"sprint_field"`    // iteration field name, e.g. "Sprint"
}

// parseBoardConfig unmarshals a nullable JSON config_meta string into a boardConfigMeta.
func parseBoardConfig(meta sql.NullString) boardConfigMeta {
	var bc boardConfigMeta
	if meta.Valid && meta.String != "" {
		_ = json.Unmarshal([]byte(meta.String), &bc)
	}
	return bc
}

// applyAreaFilter keeps only items whose TeamArea matches the configured value.
// If no TeamAreaValue is configured, all items pass through.
func applyAreaFilter(items []githubconn.BoardItem, cfg boardConfigMeta) []githubconn.BoardItem {
	if cfg.TeamAreaValue == "" {
		return items
	}
	result := items[:0:0]
	for _, bi := range items {
		if bi.TeamArea == cfg.TeamAreaValue {
			result = append(result, bi)
		}
	}
	return result
}

// applySprintFilter keeps only items whose sprint window includes now.
// Items with no sprint assigned are excluded when SprintField is configured.
// If SprintField is empty, all items pass through unchanged.
func applySprintFilter(items []githubconn.BoardItem, cfg boardConfigMeta, now time.Time) []githubconn.BoardItem {
	if cfg.SprintField == "" {
		return items
	}
	result := items[:0:0]
	for _, bi := range items {
		if bi.SprintStart == "" {
			continue
		}
		start, err := time.Parse("2006-01-02", bi.SprintStart)
		if err != nil {
			continue
		}
		days := bi.SprintDays
		if days <= 0 {
			days = 14
		}
		end := start.AddDate(0, 0, days)
		if !now.Before(start) && !now.After(end) {
			result = append(result, bi)
		}
	}
	return result
}

// hasGithubProject returns true if any of the github configs is a github_project source.
func hasGithubProject(configs []*store.SourceConfig, items map[int64]*store.SourceCatalogue) bool {
	for _, cfg := range configs {
		if item, ok := items[cfg.ID]; ok && item.SourceType == "github_project" {
			return true
		}
	}
	return false
}

// parseOwnerRepo extracts owner and repo from a catalogue item's source_meta or external_id.
func parseOwnerRepo(item *store.SourceCatalogue) (string, string) {
	meta := parseJSONMeta(item.SourceMeta)
	if owner, _ := meta["owner"].(string); owner != "" {
		if repo, _ := meta["repo"].(string); repo != "" {
			return owner, repo
		}
	}
	parts := strings.SplitN(item.ExternalID, "/", 2)
	if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
		return parts[0], parts[1]
	}
	return "", ""
}

// parseJSONMeta unmarshals a nullable JSON string into a map.
func parseJSONMeta(meta sql.NullString) map[string]any {
	if !meta.Valid || meta.String == "" {
		return map[string]any{}
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(meta.String), &m); err != nil {
		return map[string]any{}
	}
	return m
}

// boardItemsToAny converts []githubconn.BoardItem to []any with key fields for pipeline prompts.
// Sprint context ("past", "current", "future") is computed relative to now so the model
// can distinguish items from different sprints across the whole plan.
func boardItemsToAny(items []githubconn.BoardItem, now time.Time) []any {
	result := make([]any, len(items))
	for i, bi := range items {
		m := map[string]any{
			"number": bi.Number,
			"title":  bi.Title,
			"state":  bi.State,
			"url":    fmt.Sprintf("https://github.com/%s/%s/issues/%d", bi.Owner, bi.Repo, bi.Number),
		}
		if bi.Status != "" {
			m["project_status"] = bi.Status
		}
		if bi.Sprint != "" {
			m["sprint"] = bi.Sprint
			if bi.SprintStart != "" {
				if start, err := time.Parse("2006-01-02", bi.SprintStart); err == nil {
					days := bi.SprintDays
					if days <= 0 {
						days = 14
					}
					end := start.AddDate(0, 0, days)
					switch {
					case now.Before(start):
						m["sprint_context"] = "future"
					case now.After(end):
						m["sprint_context"] = "past"
					default:
						m["sprint_context"] = "current"
					}
				}
			}
		}
		result[i] = m
	}
	return result
}

// prsToAny converts []*gh.PullRequest to []any with key fields for pipeline prompts.
func prsToAny(prs []*gh.PullRequest) []any {
	result := make([]any, len(prs))
	for i, pr := range prs {
		result[i] = map[string]any{
			"number": pr.GetNumber(),
			"title":  pr.GetTitle(),
			"url":    pr.GetHTMLURL(),
		}
	}
	return result
}

// buildWorkloadMembers creates per-member workload inputs by matching issues/PRs/commits
// to team members by GitHub login.
func buildWorkloadMembers(members []*store.TeamMember, boardItems []githubconn.BoardItem, prs []*gh.PullRequest, commits []*gh.RepositoryCommit) []pipeline.WorkloadMember {
	result := make([]pipeline.WorkloadMember, len(members))
	for i, m := range members {
		login := ""
		if m.GithubLogin.Valid {
			login = strings.ToLower(m.GithubLogin.String)
		}

		var memberIssues []map[string]any
		if login != "" {
			for _, bi := range boardItems {
				matched := false
				for _, a := range bi.Assignees {
					if strings.ToLower(a) == login {
						matched = true
						break
					}
				}
				if matched {
					memberIssues = append(memberIssues, map[string]any{
						"number": bi.Number,
						"title":  bi.Title,
						"state":  bi.State,
					})
				}
			}
		}

		var memberPRs []map[string]any
		if login != "" {
			for _, pr := range prs {
				if pr.User != nil && strings.ToLower(pr.User.GetLogin()) == login {
					memberPRs = append(memberPRs, map[string]any{
						"number": pr.GetNumber(),
						"title":  pr.GetTitle(),
					})
				}
			}
		}

		var memberCommits []map[string]any
		if login != "" {
			for _, commit := range commits {
				if commit.Author != nil && strings.ToLower(commit.Author.GetLogin()) == login {
					sha := commit.GetSHA()
					if len(sha) > 7 {
						sha = sha[:7]
					}
					msg := ""
					if c := commit.GetCommit(); c != nil {
						msg = firstLine(c.GetMessage())
					}
					memberCommits = append(memberCommits, map[string]any{
						"sha":     sha,
						"message": msg,
					})
				}
			}
		}

		result[i] = pipeline.WorkloadMember{
			Name:          m.Name,
			OpenIssues:    memberIssues,
			MergedPRs:     memberPRs,
			RecentCommits: memberCommits,
		}
	}
	return result
}

// sprintWindow converts a SprintMeta into a pipeline.SprintWindow.
func sprintWindow(sm *store.SprintMeta) pipeline.SprintWindow {
	if sm == nil {
		return pipeline.SprintWindow{}
	}
	return pipeline.SprintWindow{
		Start: sm.StartDate.String,
		End:   sm.EndDate.String,
	}
}

// marketingCampaignsToAny converts []notionconn.MarketingCampaign to []any for pipeline prompts.
// Returns nil if there are no campaigns, so the AI prompt omits the field entirely.
func marketingCampaignsToAny(campaigns []notionconn.MarketingCampaign) any {
	if len(campaigns) == 0 {
		return nil
	}
	result := make([]any, len(campaigns))
	for i, c := range campaigns {
		tasks := make([]any, len(c.Tasks))
		for j, t := range c.Tasks {
			tasks[j] = map[string]any{
				"title":    t.Title,
				"status":   t.Status,
				"assignee": t.Assignee,
				"body":     t.Body,
			}
		}
		entry := map[string]any{
			"title":  c.Title,
			"status": c.Status,
			"tasks":  tasks,
		}
		if c.DateStart != nil {
			entry["date_start"] = c.DateStart.Format("2006-01-02")
		}
		if c.DateEnd != nil {
			entry["date_end"] = c.DateEnd.Format("2006-01-02")
		}
		result[i] = entry
	}
	return result
}

// calendarCampaignsFromMarketing converts []notionconn.MarketingCampaign to
// []pipeline.CalendarCampaign for use as structured input to dates_extract.
func calendarCampaignsFromMarketing(campaigns []notionconn.MarketingCampaign) []pipeline.CalendarCampaign {
	if len(campaigns) == 0 {
		return nil
	}
	out := make([]pipeline.CalendarCampaign, len(campaigns))
	for i, c := range campaigns {
		cc := pipeline.CalendarCampaign{Name: c.Title}
		if c.DateStart != nil {
			s := c.DateStart.Format("2006-01-02")
			cc.DateStart = &s
		}
		if c.DateEnd != nil {
			e := c.DateEnd.Format("2006-01-02")
			cc.DateEnd = &e
		}
		out[i] = cc
	}
	return out
}

// calendarEventsFromResult converts a DatesExtractResult into []store.CalendarEvent
// ready for ReplaceCalendarEvents with source_class='synthesized'.
func calendarEventsFromResult(r *pipeline.DatesExtractResult) []store.CalendarEvent {
	if r == nil {
		return nil
	}
	events := make([]store.CalendarEvent, 0, len(r.Events))
	for _, e := range r.Events {
		ev := store.CalendarEvent{
			EventKey:       e.EventKey,
			Title:          e.Title,
			EventType:      e.EventType,
			DateConfidence: e.DateConfidence,
		}
		if e.Date != nil {
			ev.Date = sql.NullString{String: *e.Date, Valid: true}
		}
		if e.EndDate != nil {
			ev.EndDate = sql.NullString{String: *e.EndDate, Valid: true}
		}
		if e.NeedsDate {
			ev.NeedsDate = 1
		}
		if len(e.Sources) > 0 {
			if b, err := json.Marshal(e.Sources); err == nil {
				ev.Sources = sql.NullString{String: string(b), Valid: true}
			}
		}
		if len(e.Flags) > 0 {
			if b, err := json.Marshal(e.Flags); err == nil {
				ev.Flags = sql.NullString{String: string(b), Valid: true}
			}
		}
		events = append(events, ev)
	}
	return events
}

// firstLine returns the first line of a multiline string.
func firstLine(s string) string {
	if idx := strings.IndexByte(s, '\n'); idx >= 0 {
		return s[:idx]
	}
	return s
}

// ---- Snapshot persistence ----

// saveActivitySnapshot builds and persists the activity snapshot for a team.
func (e *Engine) saveActivitySnapshot(ctx context.Context, teamID int64, td *teamSyncData) {
	type snapshotCommit struct {
		SHA     string `json:"sha"`
		Author  string `json:"author"`
		Message string `json:"message"`
		Repo    string `json:"repo"`
		Date    string `json:"date"`
	}
	type snapshotIssue struct {
		Number        int    `json:"number"`
		Title         string `json:"title"`
		Assignee      string `json:"assignee,omitempty"`
		ProjectStatus string `json:"project_status,omitempty"`
	}
	type snapshotPR struct {
		Number   int    `json:"number"`
		Title    string `json:"title"`
		Author   string `json:"author"`
		MergedAt string `json:"merged_at"`
	}
	type snapshot struct {
		RecentCommits []snapshotCommit `json:"recent_commits"`
		OpenIssues    []snapshotIssue  `json:"open_issues"`
		MergedPRs     []snapshotPR     `json:"merged_prs"`
		LastSyncedAt  string           `json:"last_synced_at"`
	}

	// Sort commits newest-first and cap at 15.
	commits := make([]*gh.RepositoryCommit, len(td.commits))
	copy(commits, td.commits)
	sort.Slice(commits, func(i, j int) bool {
		di := commits[i].GetCommit().GetAuthor().GetDate()
		dj := commits[j].GetCommit().GetAuthor().GetDate()
		return di.After(dj.Time)
	})
	if len(commits) > 15 {
		commits = commits[:15]
	}

	snap := snapshot{LastSyncedAt: time.Now().UTC().Format(time.RFC3339)}

	for _, c := range commits {
		author := c.GetAuthor().GetLogin()
		if author == "" {
			author = c.GetCommit().GetAuthor().GetName()
		}
		repo := repoFromURL(c.GetHTMLURL())
		snap.RecentCommits = append(snap.RecentCommits, snapshotCommit{
			SHA:     c.GetSHA(),
			Author:  author,
			Message: firstLine(c.GetCommit().GetMessage()),
			Repo:    repo,
			Date:    c.GetCommit().GetAuthor().GetDate().Format("2006-01-02"),
		})
	}

	for _, bi := range td.boardItems {
		si := snapshotIssue{
			Number:        bi.Number,
			Title:         bi.Title,
			ProjectStatus: bi.Status,
		}
		if len(bi.Assignees) > 0 {
			si.Assignee = bi.Assignees[0]
		}
		snap.OpenIssues = append(snap.OpenIssues, si)
	}

	prs := td.mergedPRs
	if len(prs) > 15 {
		prs = prs[:15]
	}
	for _, pr := range prs {
		mergedAt := ""
		if t := pr.GetMergedAt(); !t.IsZero() {
			mergedAt = t.Format("2006-01-02")
		}
		snap.MergedPRs = append(snap.MergedPRs, snapshotPR{
			Number:   pr.GetNumber(),
			Title:    pr.GetTitle(),
			Author:   pr.GetUser().GetLogin(),
			MergedAt: mergedAt,
		})
	}

	data, err := json.Marshal(snap)
	if err != nil {
		log.Printf("WARN  sync [team %d]: marshal activity snapshot: %v", teamID, err)
		return
	}
	if err := e.store.UpsertSnapshot(ctx, teamID, "activity", string(data)); err != nil {
		log.Printf("WARN  sync [team %d]: save activity snapshot: %v", teamID, err)
	}
}

// saveMarketingSnapshot persists the marketing campaigns snapshot for a team.
func (e *Engine) saveMarketingSnapshot(ctx context.Context, teamID int64, campaigns []notionconn.MarketingCampaign) {
	type snapshotTask struct {
		Title    string `json:"title"`
		Status   string `json:"status"`
		Assignee string `json:"assignee,omitempty"`
	}
	type snapshotCampaign struct {
		Title     string         `json:"title"`
		Status    string         `json:"status"`
		DateStart *string        `json:"date_start,omitempty"`
		DateEnd   *string        `json:"date_end,omitempty"`
		Tasks     []snapshotTask `json:"tasks"`
	}
	type snapshot struct {
		Campaigns    []snapshotCampaign `json:"campaigns"`
		LastSyncedAt string             `json:"last_synced_at"`
	}

	snap := snapshot{LastSyncedAt: time.Now().UTC().Format(time.RFC3339)}
	for _, c := range campaigns {
		sc := snapshotCampaign{
			Title:  c.Title,
			Status: c.Status,
		}
		if c.DateStart != nil {
			s := c.DateStart.Format("2006-01-02")
			sc.DateStart = &s
		}
		if c.DateEnd != nil {
			s := c.DateEnd.Format("2006-01-02")
			sc.DateEnd = &s
		}
		for _, t := range c.Tasks {
			sc.Tasks = append(sc.Tasks, snapshotTask{
				Title:    t.Title,
				Status:   t.Status,
				Assignee: t.Assignee,
			})
		}
		snap.Campaigns = append(snap.Campaigns, sc)
	}

	data, err := json.Marshal(snap)
	if err != nil {
		log.Printf("WARN  sync [team %d]: marshal marketing snapshot: %v", teamID, err)
		return
	}
	if err := e.store.UpsertSnapshot(ctx, teamID, "marketing", string(data)); err != nil {
		log.Printf("WARN  sync [team %d]: save marketing snapshot: %v", teamID, err)
	}
}

// repoFromURL extracts the repository name from a GitHub HTML URL.
// e.g. https://github.com/owner/repo/commit/sha → "repo"
func repoFromURL(htmlURL string) string {
	parts := strings.Split(htmlURL, "/")
	if len(parts) >= 5 {
		return parts[4]
	}
	return ""
}
