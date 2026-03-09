package sync

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	gh "github.com/google/go-github/v60/github"
	"github.com/your-org/dashboard/internal/pipeline"
	"github.com/your-org/dashboard/internal/store"
)

// teamSyncData holds source content fetched for a single team's incremental sync.
type teamSyncData struct {
	currentPlanText string
	goalsDocText    string
	openIssues      []*gh.Issue
	mergedPRs       []*gh.PullRequest
	commits         []*gh.RepositoryCommit
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
	syncStart := time.Now()
	log.Printf("INFO  sync [run %d scope=%s]: started", runID, scope)

	switch scope {
	case "org":
		e.syncOrg(ctx, errs)
	default: // "team"
		if teamID == nil {
			_ = e.store.UpdateSyncRun(ctx, runID, "error", sql.NullString{
				String: "team scope requires non-nil teamID", Valid: true,
			})
			return
		}
		td := e.fetchTeamData(ctx, *teamID, errs)
		e.runTeamPipelines(ctx, *teamID, td, errs)
	}

	if len(errs) > 0 {
		b, _ := json.Marshal(errs)
		log.Printf("INFO  sync [run %d]: done in %s with errors: %s", runID, time.Since(syncStart).Round(time.Millisecond), string(b))
		_ = e.store.UpdateSyncRun(ctx, runID, "error", sql.NullString{String: string(b), Valid: true})
	} else {
		log.Printf("INFO  sync [run %d]: done in %s", runID, time.Since(syncStart).Round(time.Millisecond))
		_ = e.store.UpdateSyncRun(ctx, runID, "done", sql.NullString{})
	}
}

func (e *Engine) syncOrg(ctx context.Context, errs map[string]string) {
	teams, err := e.store.ListTeams(ctx)
	if err != nil {
		errs["list_teams"] = err.Error()
		return
	}

	teamGoals := make(map[int64][]string)
	for _, team := range teams {
		td := e.fetchTeamData(ctx, team.ID, errs)
		goals := e.runTeamPipelines(ctx, team.ID, td, errs)
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
}

// fetchTeamData loads source_configs for a team, fetches each source incrementally, and
// returns the aggregated data. Per-source errors are recorded in errs; the run is not aborted.
// Notion/github_file fetches run in parallel to reduce latency.
func (e *Engine) fetchTeamData(ctx context.Context, teamID int64, errs map[string]string) teamSyncData {
	var td teamSyncData

	configs, err := e.store.GetSourceConfigsForScope(ctx, sql.NullInt64{Int64: teamID, Valid: true})
	if err != nil {
		errs[fmt.Sprintf("team_%d_configs", teamID)] = err.Error()
		return td
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
		case "github_label", "github_repo":
			githubConfigs = append(githubConfigs, cfg)
			githubItems[cfg.ID] = item
		}
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
			cached := w.item.NotionLastEdited.Valid && err == nil && time.Since(t0) < 500*time.Millisecond
			cacheLabel := "fresh"
			if cached {
				cacheLabel = "cached"
			}
			log.Printf("INFO  sync [team %d]: fetched %s:%s (%d chars, %s) in %s",
				teamID, w.item.SourceType, w.item.ExternalID, len(content), cacheLabel, time.Since(t0).Round(time.Millisecond))
			results <- notionFetchResult{cfg: w.cfg, item: w.item, content: content, err: err}
		}(w)
	}
	go func() { wg.Wait(); close(results) }()

	for r := range results {
		key := fmt.Sprintf("%s:%s", r.item.SourceType, r.item.ExternalID)
		if r.err != nil {
			errs[key] = r.err.Error()
			continue
		}
		_ = e.store.TouchCatalogueItem(ctx, r.item.ID)
		switch r.cfg.Purpose {
		case "current_plan":
			td.currentPlanText = r.content
		case "sprint_doc":
			meta := parseJSONMeta(r.cfg.ConfigMeta)
			if status, _ := meta["sprint_status"].(string); status == "current" {
				td.currentPlanText = r.content
			}
		case "goals", "goals_doc":
			td.goalsDocText = r.content
		}
	}

	log.Printf("INFO  sync [team %d]: content fetch done in %s", teamID, time.Since(fetchStart).Round(time.Millisecond))

	// GitHub sources: fetch sequentially (rate-limit friendly).
	// Fetch all commits without a login filter so we can discover new contributors.
	log.Printf("INFO  sync [team %d]: fetching %d github source(s)", teamID, len(githubConfigs))
	existingMembers, _ := e.store.GetTeamMembers(ctx, teamID)
	knownLogins := map[string]bool{}
	for _, m := range existingMembers {
		if m.GithubLogin.Valid && m.GithubLogin.String != "" {
			knownLogins[strings.ToLower(m.GithubLogin.String)] = true
		}
	}
	log.Printf("INFO  sync [team %d]: %d existing members with github logins", teamID, len(knownLogins))

	for _, cfg := range githubConfigs {
		item := githubItems[cfg.ID]
		since := item.UpdatedAt
		key := fmt.Sprintf("%s:%s", item.SourceType, item.ExternalID)

		switch item.SourceType {
		case "github_label":
			meta := parseJSONMeta(item.SourceMeta)
			owner, _ := meta["owner"].(string)
			repo, _ := meta["repo"].(string)
			if owner == "" || repo == "" {
				errs[key+":no_owner_repo"] = "missing owner/repo in source_meta"
				continue
			}
			issues, fetchErr := e.github.FetchIssues(ctx, owner, repo, item.Title, since)
			if fetchErr != nil {
				errs[key+":issues"] = fetchErr.Error()
				log.Printf("WARN  sync [team %d]: %s fetch issues: %v", teamID, key, fetchErr)
			} else {
				log.Printf("INFO  sync [team %d]: %s: %d issues", teamID, key, len(issues))
				td.openIssues = append(td.openIssues, issues...)
			}
			_ = e.store.TouchCatalogueItem(ctx, item.ID)

		case "github_repo":
			owner, repo := parseOwnerRepo(item)
			if owner == "" || repo == "" {
				errs[key+":no_owner_repo"] = "missing owner/repo in source_meta or external_id"
				continue
			}
			log.Printf("INFO  sync [team %d]: github_repo %s/%s (since %s)", teamID, owner, repo, since.Format("2006-01-02"))

			prs, fetchErr := e.github.FetchMergedPRs(ctx, owner, repo, since)
			if fetchErr != nil {
				errs[key+":prs"] = fetchErr.Error()
				log.Printf("WARN  sync [team %d]: %s fetch PRs: %v", teamID, key, fetchErr)
			} else {
				log.Printf("INFO  sync [team %d]: %s: %d merged PRs", teamID, key, len(prs))
				td.mergedPRs = append(td.mergedPRs, prs...)
			}

			meta := parseJSONMeta(item.SourceMeta)
			if label, _ := meta["label"].(string); label != "" {
				issues, fetchErr := e.github.FetchIssues(ctx, owner, repo, label, since)
				if fetchErr != nil {
					errs[key+":issues"] = fetchErr.Error()
					log.Printf("WARN  sync [team %d]: %s fetch issues: %v", teamID, key, fetchErr)
				} else {
					log.Printf("INFO  sync [team %d]: %s: %d issues (label=%s)", teamID, key, len(issues), label)
					td.openIssues = append(td.openIssues, issues...)
				}
			}

			// Fetch all commits; auto-add new contributors as team members.
			// Use at least a 90-day window so newly-added repos discover historical contributors.
			commitSince := since
			if floor := time.Now().AddDate(0, 0, -90); since.After(floor) {
				commitSince = floor
			}
			commits, fetchErr := e.github.FetchCommits(ctx, owner, repo, commitSince, time.Now(), nil)
			if fetchErr != nil {
				errs[key+":commits"] = fetchErr.Error()
				log.Printf("WARN  sync [team %d]: %s fetch commits: %v", teamID, key, fetchErr)
			} else {
				newMembers := 0
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
					_ = e.store.UpsertMemberByGithubLogin(ctx, teamID, login, name)
					knownLogins[login] = true
					newMembers++
					log.Printf("INFO  sync [team %d]: auto-added member %q (%s) from commits", teamID, name, login)
				}
				log.Printf("INFO  sync [team %d]: %s: %d commits, %d new members discovered", teamID, key, len(commits), newMembers)
				td.commits = append(td.commits, commits...)
			}

			_ = e.store.TouchCatalogueItem(ctx, item.ID)
		}
	}
	return td
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

// runTeamPipelines executes the pipeline chain for a team in two parallel phases:
//
//	Phase 1 (parallel): sprint_parse, velocity_analysis
//	Phase 2 (parallel, after phase 1): team_status, workload_estimation
//
// Returns the extracted business goal texts for use in org-level goal_alignment.
func (e *Engine) runTeamPipelines(ctx context.Context, teamID int64, td teamSyncData, errs map[string]string) []string {
	var (
		mu        sync.Mutex
		goalTexts []string
	)

	// Phase 1: independent pipelines run concurrently.
	p1Start := time.Now()
	log.Printf("INFO  sync [team %d]: pipeline phase 1 start (sprint_parse, velocity)", teamID)
	var phase1 sync.WaitGroup

	if td.currentPlanText != "" {
		phase1.Add(1)
		go func() {
			defer phase1.Done()
			t0 := time.Now()
			_, _ = e.pipeline.RunSprintParse(ctx, teamID, td.currentPlanText)
			log.Printf("INFO  sync [team %d]: sprint_parse done in %s", teamID, time.Since(t0).Round(time.Millisecond))
		}()
	}

	closedCount := 0
	for _, issue := range td.openIssues {
		if issue.GetState() == "closed" {
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
		log.Printf("INFO  sync [team %d]: velocity_analysis done in %s", teamID, time.Since(t0).Round(time.Millisecond))
	}()

	phase1.Wait()
	log.Printf("INFO  sync [team %d]: pipeline phase 1 done in %s", teamID, time.Since(p1Start).Round(time.Millisecond))

	// Phase 2: pipelines that depend on phase 1 results.
	p2Start := time.Now()
	log.Printf("INFO  sync [team %d]: pipeline phase 2 start (team_status, workload)", teamID)
	sprintMeta, _ := e.store.GetSprintMeta(ctx, teamID, "current")
	members, _ := e.store.GetTeamMembers(ctx, teamID)

	var phase2 sync.WaitGroup

	if td.currentPlanText != "" || td.goalsDocText != "" {
		phase2.Add(1)
		go func() {
			defer phase2.Done()
			t0 := time.Now()
			result, err := e.pipeline.RunTeamStatus(ctx, teamID, pipeline.TeamStatusInput{
				GoalsDocText:   td.goalsDocText,
				SprintPlanText: td.currentPlanText,
				SprintMeta:     sprintMeta,
				OpenIssues:     issuesToAny(td.openIssues),
				MergedPRs:      prsToAny(td.mergedPRs),
			})
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
				Members:            buildWorkloadMembers(members, td.openIssues, td.mergedPRs, td.commits),
				SprintWindow:       sprintWindow(sprintMeta),
				StandardSprintDays: 5,
			})
			log.Printf("INFO  sync [team %d]: workload_estimation done in %s", teamID, time.Since(t0).Round(time.Millisecond))
		}()
	} else {
		log.Printf("INFO  sync [team %d]: skipping workload_estimation (no team members)", teamID)
	}

	phase2.Wait()
	log.Printf("INFO  sync [team %d]: pipeline phase 2 done in %s", teamID, time.Since(p2Start).Round(time.Millisecond))
	return goalTexts
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

// issuesToAny converts []*gh.Issue to []any with key fields for pipeline prompts.
func issuesToAny(issues []*gh.Issue) []any {
	result := make([]any, len(issues))
	for i, issue := range issues {
		result[i] = map[string]any{
			"number": issue.GetNumber(),
			"title":  issue.GetTitle(),
			"state":  issue.GetState(),
			"url":    issue.GetHTMLURL(),
		}
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
func buildWorkloadMembers(members []*store.TeamMember, issues []*gh.Issue, prs []*gh.PullRequest, commits []*gh.RepositoryCommit) []pipeline.WorkloadMember {
	result := make([]pipeline.WorkloadMember, len(members))
	for i, m := range members {
		login := ""
		if m.GithubLogin.Valid {
			login = strings.ToLower(m.GithubLogin.String)
		}

		var memberIssues []map[string]any
		if login != "" {
			for _, issue := range issues {
				if issue.Assignee != nil && strings.ToLower(issue.Assignee.GetLogin()) == login {
					memberIssues = append(memberIssues, map[string]any{
						"number": issue.GetNumber(),
						"title":  issue.GetTitle(),
						"state":  issue.GetState(),
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

// firstLine returns the first line of a multiline string.
func firstLine(s string) string {
	if idx := strings.IndexByte(s, '\n'); idx >= 0 {
		return s[:idx]
	}
	return s
}
