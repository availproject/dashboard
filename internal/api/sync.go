package api

import (
	"database/sql"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

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

	writeJSON(w, http.StatusOK, run)
}
