package api

import (
	"database/sql"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/your-org/dashboard/internal/store"
)

type annotationResponse struct {
	ID        int64     `json:"id"`
	TeamID    *int64    `json:"team_id"`
	ItemRef   *string   `json:"item_ref"`
	Tier      string    `json:"tier"`
	Content   string    `json:"content"`
	Archived  bool      `json:"archived"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func annotationToResponse(a *store.Annotation) annotationResponse {
	resp := annotationResponse{
		ID:        a.ID,
		Tier:      a.Tier,
		Content:   a.Content,
		Archived:  a.Archived != 0,
		CreatedAt: a.CreatedAt,
		UpdatedAt: a.UpdatedAt,
	}
	if a.TeamID.Valid {
		v := a.TeamID.Int64
		resp.TeamID = &v
	}
	if a.ItemRef.Valid {
		resp.ItemRef = &a.ItemRef.String
	}
	return resp
}

type groupedAnnotationsResponse struct {
	Item []annotationResponse `json:"item"`
	Team []annotationResponse `json:"team"`
}

// --- GET /config/annotations ---

func (d *Deps) handleListConfigAnnotations(w http.ResponseWriter, r *http.Request) {
	annotations, err := d.Store.ListAllAnnotations(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list annotations: "+err.Error())
		return
	}

	result := groupedAnnotationsResponse{
		Item: []annotationResponse{},
		Team: []annotationResponse{},
	}
	for _, a := range annotations {
		resp := annotationToResponse(a)
		if a.Tier == "item" {
			result.Item = append(result.Item, resp)
		} else {
			result.Team = append(result.Team, resp)
		}
	}

	writeJSON(w, http.StatusOK, result)
}

// --- POST /config/annotations ---

type createAnnotationRequest struct {
	Tier    string  `json:"tier"`
	TeamID  *int64  `json:"team_id"`
	ItemRef *string `json:"item_ref"`
	Content string  `json:"content"`
}

func (d *Deps) handleCreateConfigAnnotation(w http.ResponseWriter, r *http.Request) {
	sharedCreateAnnotation(d, w, r)
}

// --- PUT /config/annotations/{id} ---

func (d *Deps) handleUpdateConfigAnnotation(w http.ResponseWriter, r *http.Request) {
	sharedUpdateAnnotation(d, w, r)
}

// --- DELETE /config/annotations/{id} ---

func (d *Deps) handleDeleteConfigAnnotation(w http.ResponseWriter, r *http.Request) {
	sharedDeleteAnnotation(d, w, r)
}

// Shared annotation mutation helpers used by both /config/annotations and /annotations routes.

func sharedCreateAnnotation(d *Deps, w http.ResponseWriter, r *http.Request) {
	var req createAnnotationRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Tier == "" {
		writeError(w, http.StatusBadRequest, "tier is required")
		return
	}
	if req.Content == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}

	teamID := sql.NullInt64{}
	if req.TeamID != nil {
		teamID = sql.NullInt64{Int64: *req.TeamID, Valid: true}
	}
	itemRef := sql.NullString{}
	if req.ItemRef != nil {
		itemRef = sql.NullString{String: *req.ItemRef, Valid: true}
	}

	a, err := d.Store.CreateAnnotation(r.Context(), teamID, itemRef, req.Tier, req.Content)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "create annotation: "+err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, annotationToResponse(a))
}

func sharedUpdateAnnotation(d *Deps, w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid annotation id")
		return
	}

	var req struct {
		Content string `json:"content"`
	}
	if err := readJSON(r, &req); err != nil || req.Content == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}

	if err := d.Store.UpdateAnnotation(r.Context(), id, req.Content); err != nil {
		if err == sql.ErrNoRows {
			writeError(w, http.StatusNotFound, "annotation not found")
		} else {
			writeError(w, http.StatusInternalServerError, "update annotation: "+err.Error())
		}
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func sharedDeleteAnnotation(d *Deps, w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid annotation id")
		return
	}

	if err := d.Store.DeleteAnnotation(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, "delete annotation: "+err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
