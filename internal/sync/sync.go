package sync

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
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
		e.runTeamPipelines(ctx, *teamID, td)
	}

	if len(errs) > 0 {
		b, _ := json.Marshal(errs)
		_ = e.store.UpdateSyncRun(ctx, runID, "error", sql.NullString{String: string(b), Valid: true})
	} else {
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
		goals := e.runTeamPipelines(ctx, team.ID, td)
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

// fetchTeamData loads source_configs for a team, fetches each source incrementally, and
// returns the aggregated data. Per-source errors are recorded in errs; the run is not aborted.
func (e *Engine) fetchTeamData(ctx context.Context, teamID int64, errs map[string]string) teamSyncData {
	var td teamSyncData

	configs, err := e.store.GetSourceConfigsForScope(ctx, sql.NullInt64{Int64: teamID, Valid: true})
	if err != nil {
		errs[fmt.Sprintf("team_%d_configs", teamID)] = err.Error()
		return td
	}

	for _, cfg := range configs {
		item, err := e.store.GetCatalogueItem(ctx, cfg.CatalogueID)
		if err != nil {
			continue
		}
		since := item.UpdatedAt
		key := fmt.Sprintf("%s:%s", item.SourceType, item.ExternalID)

		switch item.SourceType {
		case "notion_page", "notion_db":
			content, fetchErr := e.fetchNotionContent(ctx, item)
			if fetchErr != nil {
				errs[key] = fetchErr.Error()
				continue
			}
			switch cfg.Purpose {
			case "current_plan":
				td.currentPlanText = content
			case "goals":
				td.goalsDocText = content
			}
			_ = e.store.TouchCatalogueItem(ctx, item.ID)

		case "github_repo":
			owner, repo := parseOwnerRepo(item)
			if owner == "" || repo == "" {
				errs[key+":no_owner_repo"] = "missing owner/repo in source_meta or external_id"
				continue
			}

			// Merged PRs (always fetch).
			prs, fetchErr := e.github.FetchMergedPRs(ctx, owner, repo, since)
			if fetchErr != nil {
				errs[key+":prs"] = fetchErr.Error()
			} else {
				td.mergedPRs = append(td.mergedPRs, prs...)
			}

			// Issues — only when a label is configured in source_meta.
			meta := parseJSONMeta(item.SourceMeta)
			if label, _ := meta["label"].(string); label != "" {
				issues, fetchErr := e.github.FetchIssues(ctx, owner, repo, label, since)
				if fetchErr != nil {
					errs[key+":issues"] = fetchErr.Error()
				} else {
					td.openIssues = append(td.openIssues, issues...)
				}
			}

			// Commits filtered by team member GitHub logins.
			members, _ := e.store.GetTeamMembers(ctx, teamID)
			var logins []string
			for _, m := range members {
				if m.GithubLogin.Valid && m.GithubLogin.String != "" {
					logins = append(logins, m.GithubLogin.String)
				}
			}
			commits, fetchErr := e.github.FetchCommits(ctx, owner, repo, since, time.Now(), logins)
			if fetchErr != nil {
				errs[key+":commits"] = fetchErr.Error()
			} else {
				td.commits = append(td.commits, commits...)
			}

			_ = e.store.TouchCatalogueItem(ctx, item.ID)
		}
	}
	return td
}

// fetchNotionContent retrieves text content from a notion_page or notion_db catalogue item.
func (e *Engine) fetchNotionContent(ctx context.Context, item *store.SourceCatalogue) (string, error) {
	switch item.SourceType {
	case "notion_page":
		content, _, err := e.notion.FetchPage(ctx, item.ExternalID)
		return content, err
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

// runTeamPipelines executes the full pipeline chain for a team in order:
// sprint_parse → goal_extraction → concerns → workload_estimation → velocity_analysis.
// Returns the extracted goal texts for use in org-level goal_alignment.
func (e *Engine) runTeamPipelines(ctx context.Context, teamID int64, td teamSyncData) []string {
	// 1. sprint_parse
	if td.currentPlanText != "" {
		_, _ = e.pipeline.RunSprintParse(ctx, teamID, td.currentPlanText)
	}

	// 2. goal_extraction
	var goalTexts []string
	if td.currentPlanText != "" || td.goalsDocText != "" {
		result, err := e.pipeline.RunGoalExtraction(ctx, teamID, td.goalsDocText, td.currentPlanText)
		if err == nil && result != nil {
			for _, g := range result.Goals {
				goalTexts = append(goalTexts, g.Text)
			}
		}
	}

	sprintMeta, _ := e.store.GetSprintMeta(ctx, teamID, "current")

	// 3. concerns
	_, _ = e.pipeline.RunConcerns(ctx, teamID, pipeline.ConcernsInput{
		OpenIssues:     issuesToAny(td.openIssues),
		MergedPRs:      prsToAny(td.mergedPRs),
		SprintPlanText: td.currentPlanText,
		ExtractedGoals: goalTexts,
		SprintMeta:     sprintMeta,
	})

	// 4. workload_estimation
	members, _ := e.store.GetTeamMembers(ctx, teamID)
	_, _ = e.pipeline.RunWorkloadEstimation(ctx, teamID, pipeline.WorkloadInput{
		Members:            buildWorkloadMembers(members, td.openIssues, td.mergedPRs, td.commits),
		SprintWindow:       sprintWindow(sprintMeta),
		StandardSprintDays: 5,
	})

	// 5. velocity_analysis
	closedCount := 0
	for _, issue := range td.openIssues {
		if issue.GetState() == "closed" {
			closedCount++
		}
	}
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
