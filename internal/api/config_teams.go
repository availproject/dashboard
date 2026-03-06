package api

import (
	"database/sql"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/your-org/dashboard/internal/store"
)

// --- POST /config/teams ---

type createTeamRequest struct {
	Name string `json:"name"`
}

type teamResponse struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

func (d *Deps) handleCreateTeam(w http.ResponseWriter, r *http.Request) {
	var req createTeamRequest
	if err := readJSON(r, &req); err != nil || req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	team, err := d.Store.CreateTeam(r.Context(), req.Name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "create team: "+err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, teamResponse{ID: team.ID, Name: team.Name})
}

// --- PUT /config/teams/{id} ---

func (d *Deps) handleUpdateTeam(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid team id")
		return
	}

	var req createTeamRequest
	if err := readJSON(r, &req); err != nil || req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	ctx := r.Context()
	if err := d.Store.UpdateTeam(ctx, id, req.Name); err != nil {
		if err == sql.ErrNoRows {
			writeError(w, http.StatusNotFound, "team not found")
		} else {
			writeError(w, http.StatusInternalServerError, "update team: "+err.Error())
		}
		return
	}

	team, err := d.Store.GetTeam(ctx, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "get team: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, teamResponse{ID: team.ID, Name: team.Name})
}

// --- DELETE /config/teams/{id} ---

func (d *Deps) handleDeleteTeam(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid team id")
		return
	}

	if err := d.Store.DeleteTeam(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, "delete team: "+err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// --- POST /config/teams/{id}/members ---

type memberRequest struct {
	DisplayName    string  `json:"display_name"`
	GithubUsername *string `json:"github_username"`
	NotionUserID   *string `json:"notion_user_id"`
}

type memberResponse struct {
	ID             int64   `json:"id"`
	TeamID         int64   `json:"team_id"`
	DisplayName    string  `json:"display_name"`
	GithubUsername *string `json:"github_username"`
	NotionUserID   *string `json:"notion_user_id"`
}

func memberToResponse(m *store.TeamMember) memberResponse {
	resp := memberResponse{
		ID:          m.ID,
		TeamID:      m.TeamID,
		DisplayName: m.Name,
	}
	if m.GithubLogin.Valid {
		resp.GithubUsername = &m.GithubLogin.String
	}
	if m.NotionUserID.Valid {
		resp.NotionUserID = &m.NotionUserID.String
	}
	return resp
}

func (d *Deps) handleAddMember(w http.ResponseWriter, r *http.Request) {
	teamID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid team id")
		return
	}

	var req memberRequest
	if err := readJSON(r, &req); err != nil || req.DisplayName == "" {
		writeError(w, http.StatusBadRequest, "display_name is required")
		return
	}

	githubLogin := sql.NullString{}
	if req.GithubUsername != nil {
		githubLogin = sql.NullString{String: *req.GithubUsername, Valid: true}
	}
	notionUserID := sql.NullString{}
	if req.NotionUserID != nil {
		notionUserID = sql.NullString{String: *req.NotionUserID, Valid: true}
	}

	m, err := d.Store.AddMember(r.Context(), teamID, req.DisplayName, githubLogin, notionUserID, sql.NullString{})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "add member: "+err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, memberToResponse(m))
}

// --- PUT /config/members/{id} ---

func (d *Deps) handleUpdateMember(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid member id")
		return
	}

	var req memberRequest
	if err := readJSON(r, &req); err != nil || req.DisplayName == "" {
		writeError(w, http.StatusBadRequest, "display_name is required")
		return
	}

	githubLogin := sql.NullString{}
	if req.GithubUsername != nil {
		githubLogin = sql.NullString{String: *req.GithubUsername, Valid: true}
	}
	notionUserID := sql.NullString{}
	if req.NotionUserID != nil {
		notionUserID = sql.NullString{String: *req.NotionUserID, Valid: true}
	}

	if err := d.Store.UpdateMember(r.Context(), id, req.DisplayName, githubLogin, notionUserID, sql.NullString{}); err != nil {
		if err == sql.ErrNoRows {
			writeError(w, http.StatusNotFound, "member not found")
		} else {
			writeError(w, http.StatusInternalServerError, "update member: "+err.Error())
		}
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// --- DELETE /config/members/{id} ---

func (d *Deps) handleDeleteMember(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid member id")
		return
	}

	if err := d.Store.DeleteMember(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, "delete member: "+err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
