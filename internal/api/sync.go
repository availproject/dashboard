package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
)

func (d *Deps) handleListSyncRuns(w http.ResponseWriter, r *http.Request) {
	runs, err := d.Store.ListSyncRuns(r.Context(), 50)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Build team name map for enrichment.
	teams, _ := d.Store.ListTeams(r.Context())
	teamNames := make(map[int64]string, len(teams))
	for _, t := range teams {
		teamNames[t.ID] = t.Name
	}

	type item struct {
		ID         int64            `json:"ID"`
		Scope      string           `json:"Scope"`
		TeamID     *int64           `json:"TeamID,omitempty"`
		TeamName   *string          `json:"TeamName,omitempty"`
		Status     string           `json:"Status"`
		Error      *string          `json:"Error,omitempty"`
		StartedAt  time.Time        `json:"StartedAt"`
		FinishedAt *time.Time       `json:"FinishedAt,omitempty"`
		DurationMs *int64           `json:"DurationMs,omitempty"`
		Timings    map[string]int64 `json:"Timings,omitempty"`
	}

	out := make([]item, 0, len(runs))
	for _, run := range runs {
		it := item{
			ID:        run.ID,
			Scope:     run.Scope,
			Status:    run.Status,
			StartedAt: run.StartedAt,
		}
		if run.TeamID.Valid {
			id := run.TeamID.Int64
			it.TeamID = &id
			if name, ok := teamNames[id]; ok {
				it.TeamName = &name
			}
		}
		if run.Error.Valid {
			it.Error = &run.Error.String
		}
		if run.FinishedAt.Valid {
			t := run.FinishedAt.Time
			it.FinishedAt = &t
			ms := t.Sub(run.StartedAt).Milliseconds()
			it.DurationMs = &ms
		}
		if run.Timings.Valid && run.Timings.String != "" {
			var timings map[string]int64
			if err := json.Unmarshal([]byte(run.Timings.String), &timings); err == nil {
				it.Timings = timings
			}
		}
		out = append(out, it)
	}

	writeJSON(w, http.StatusOK, out)
}

func (d *Deps) handlePostSync(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Scope  string  `json:"scope"`
		TeamID *int64  `json:"team_id"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.Scope != "team" && body.Scope != "org" {
		writeError(w, http.StatusBadRequest, "scope must be 'team' or 'org'")
		return
	}
	if body.Scope == "team" && body.TeamID == nil {
		writeError(w, http.StatusBadRequest, "team_id required when scope is 'team'")
		return
	}

	var nullTeamID sql.NullInt64
	if body.TeamID != nil {
		nullTeamID = sql.NullInt64{Int64: *body.TeamID, Valid: true}
	}

	if _, err := d.Store.GetRunningSyncRun(r.Context(), body.Scope, nullTeamID); err == nil {
		writeError(w, http.StatusConflict, "sync already running for this scope")
		return
	}

	syncRunID, err := d.Engine.Sync(r.Context(), body.Scope, body.TeamID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]int64{"sync_run_id": syncRunID})
}

func (d *Deps) handleGetSyncRun(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "run_id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid run_id")
		return
	}

	run, err := d.Store.GetSyncRun(r.Context(), id)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "sync run not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	resp := struct {
		ID      int64              `json:"ID"`
		Status  string             `json:"Status"`
		Scope   string             `json:"Scope"`
		Error   *string            `json:"Error"`
		Timings map[string]int64   `json:"Timings,omitempty"`
	}{
		ID:     run.ID,
		Status: run.Status,
		Scope:  run.Scope,
	}
	if run.Error.Valid {
		resp.Error = &run.Error.String
	}
	if run.Timings.Valid && run.Timings.String != "" {
		var t map[string]int64
		if err := json.Unmarshal([]byte(run.Timings.String), &t); err == nil {
			resp.Timings = t
		}
	}
	writeJSON(w, http.StatusOK, resp)
}
