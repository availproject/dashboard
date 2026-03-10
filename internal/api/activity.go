package api

import (
	"database/sql"
	"net/http"
	"strconv"

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

func (d *Deps) handleGetTeamActivity(w http.ResponseWriter, r *http.Request) {
	teamID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid team id")
		return
	}

	data, _, err := d.Store.GetSnapshot(r.Context(), teamID, "activity")
	if err == sql.ErrNoRows {
		writeJSON(w, http.StatusOK, activityResponse{})
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "get activity snapshot: "+err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(data))
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

func (d *Deps) handleGetTeamMarketing(w http.ResponseWriter, r *http.Request) {
	teamID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid team id")
		return
	}

	data, _, err := d.Store.GetSnapshot(r.Context(), teamID, "marketing")
	if err == sql.ErrNoRows {
		writeJSON(w, http.StatusOK, marketingResponse{})
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "get marketing snapshot: "+err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(data))
}
