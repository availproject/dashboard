package api

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

// ---- Engineering activity ----

type activityCommit struct {
	SHA     string `json:"sha"`
	Author  string `json:"author"`
	Message string `json:"message"`
	Repo    string `json:"repo"`
	Date    string `json:"date"`
}

type activityIssue struct {
	Number        int    `json:"number"`
	Title         string `json:"title"`
	Assignee      string `json:"assignee,omitempty"`
	ProjectStatus string `json:"project_status,omitempty"`
}

type activityPR struct {
	Number   int    `json:"number"`
	Title    string `json:"title"`
	Author   string `json:"author"`
	MergedAt string `json:"merged_at"`
}

type activityResponse struct {
	RecentCommits []activityCommit `json:"recent_commits"`
	OpenIssues    []activityIssue  `json:"open_issues"`
	MergedPRs     []activityPR     `json:"merged_prs"`
	LastSyncedAt  *string          `json:"last_synced_at"`
}

// handleGetTeamActivity returns engineering activity for a team.
// TODO: replace mock data with real stored activity once sync persists raw GitHub data.
func (d *Deps) handleGetTeamActivity(w http.ResponseWriter, r *http.Request) {
	_ = chi.URLParam(r, "id")

	now := time.Now()
	ts := now.Format(time.RFC3339)
	d1 := now.AddDate(0, 0, -1).Format("2006-01-02")
	d2 := now.AddDate(0, 0, -2).Format("2006-01-02")
	d3 := now.AddDate(0, 0, -3).Format("2006-01-02")

	data := activityResponse{
		LastSyncedAt: &ts,
		RecentCommits: []activityCommit{
			{SHA: "a1b2c3d", Author: "alice", Message: "fix: resolve null pointer in auth handler", Repo: "backend", Date: d1},
			{SHA: "b2c3d4e", Author: "bob", Message: "feat: add rate limiting to public API endpoints", Repo: "backend", Date: d1},
			{SHA: "c3d4e5f", Author: "charlie", Message: "chore: upgrade go-github to v60", Repo: "backend", Date: d2},
			{SHA: "d4e5f6a", Author: "alice", Message: "feat: dashboard mobile layout improvements", Repo: "frontend", Date: d2},
			{SHA: "e5f6a7b", Author: "bob", Message: "fix: correct token refresh race condition", Repo: "backend", Date: d3},
		},
		OpenIssues: []activityIssue{
			{Number: 142, Title: "Rate limiting not applied to refresh endpoint", Assignee: "alice", ProjectStatus: "In Progress"},
			{Number: 138, Title: "Dashboard chart flickers on resize", Assignee: "charlie", ProjectStatus: "In Progress"},
			{Number: 135, Title: "API timeout on large data exports", Assignee: "bob", ProjectStatus: "In Review"},
			{Number: 129, Title: "Add pagination to /teams endpoint", ProjectStatus: "To Do"},
			{Number: 121, Title: "Mobile layout broken on iOS Safari", Assignee: "charlie", ProjectStatus: "To Do"},
		},
		MergedPRs: []activityPR{
			{Number: 151, Title: "feat: add 2FA support via TOTP", Author: "alice", MergedAt: d1},
			{Number: 149, Title: "fix: correct token refresh logic after expiry", Author: "bob", MergedAt: d2},
			{Number: 147, Title: "chore: bump dependencies, patch CVE-2026-1234", Author: "charlie", MergedAt: d3},
		},
	}

	writeJSON(w, http.StatusOK, data)
}

// ---- Marketing campaigns ----

type marketingTaskItem struct {
	Title    string `json:"title"`
	Status   string `json:"status"`
	Assignee string `json:"assignee,omitempty"`
}

type marketingCampaignItem struct {
	Title     string              `json:"title"`
	Status    string              `json:"status"`
	DateStart *string             `json:"date_start,omitempty"`
	DateEnd   *string             `json:"date_end,omitempty"`
	Tasks     []marketingTaskItem `json:"tasks"`
}

type marketingResponse struct {
	Campaigns    []marketingCampaignItem `json:"campaigns"`
	LastSyncedAt *string                 `json:"last_synced_at"`
}

// handleGetTeamMarketing returns marketing campaign data for a team.
// TODO: replace mock data with real stored campaigns once sync persists Notion marketing data.
func (d *Deps) handleGetTeamMarketing(w http.ResponseWriter, r *http.Request) {
	_ = chi.URLParam(r, "id")

	now := time.Now()
	ts := now.Format(time.RFC3339)
	s1 := now.AddDate(0, 0, -10).Format("2006-01-02")
	e1 := now.AddDate(0, 0, 20).Format("2006-01-02")
	s2 := now.AddDate(0, 0, 5).Format("2006-01-02")
	e2 := now.AddDate(0, 0, 15).Format("2006-01-02")

	data := marketingResponse{
		LastSyncedAt: &ts,
		Campaigns: []marketingCampaignItem{
			{
				Title:     "Q1 Product Launch",
				Status:    "In Progress",
				DateStart: &s1,
				DateEnd:   &e1,
				Tasks: []marketingTaskItem{
					{Title: "Launch blog post", Status: "In Progress", Assignee: "Carol"},
					{Title: "Social media asset pack", Status: "In Progress", Assignee: "Dave"},
					{Title: "Email newsletter draft", Status: "Not Started", Assignee: "Carol"},
					{Title: "Press kit and media outreach", Status: "Not Started", Assignee: "Eve"},
				},
			},
			{
				Title:     "Developer Community Meetup",
				Status:    "Not Started",
				DateStart: &s2,
				DateEnd:   &e2,
				Tasks: []marketingTaskItem{
					{Title: "Design event banner", Status: "Not Started", Assignee: "Dave"},
					{Title: "Submit conference application", Status: "Not Started", Assignee: "Eve"},
					{Title: "Invite speakers and panelists", Status: "Not Started", Assignee: "Carol"},
				},
			},
		},
	}

	writeJSON(w, http.StatusOK, data)
}
