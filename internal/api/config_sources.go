package api

import (
	"database/sql"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/your-org/dashboard/internal/store"
)

// --- GET /config/sources ---

type sourceConfigResponse struct {
	ID         int64   `json:"id"`
	TeamID     *int64  `json:"team_id"`
	Purpose    string  `json:"purpose"`
	ConfigMeta *string `json:"config_meta,omitempty"`
}

type sourceItemResponse struct {
	ID                 int64                  `json:"id"`
	SourceType         string                 `json:"source_type"`
	ExternalID         string                 `json:"external_id"`
	Title              string                 `json:"title"`
	URL                *string                `json:"url,omitempty"`
	SourceMeta         *string                `json:"source_meta,omitempty"`
	ParentID           *int64                 `json:"parent_id,omitempty"`
	AISuggestedPurpose *string                `json:"ai_suggested_purpose,omitempty"`
	Status             string                 `json:"status"`
	Configs            []sourceConfigResponse `json:"configs"`
}

func (d *Deps) handleListSources(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	items, err := d.Store.ListCatalogue(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list catalogue: "+err.Error())
		return
	}

	// Optional source_type filter (comma-separated).
	sourceTypeFilter := r.URL.Query().Get("source_type")
	var allowedTypes map[string]bool
	if sourceTypeFilter != "" {
		allowedTypes = make(map[string]bool)
		for _, t := range strings.Split(sourceTypeFilter, ",") {
			t = strings.TrimSpace(t)
			if t != "" {
				allowedTypes[t] = true
			}
		}
	}

	configs, err := d.Store.ListSourceConfigs(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list source configs: "+err.Error())
		return
	}

	// Index configs by catalogue_id.
	configsByCatalogueID := make(map[int64][]sourceConfigResponse)
	for _, sc := range configs {
		resp := sourceConfigResponse{
			ID:      sc.ID,
			Purpose: sc.Purpose,
		}
		if sc.TeamID.Valid {
			v := sc.TeamID.Int64
			resp.TeamID = &v
		}
		if sc.ConfigMeta.Valid {
			resp.ConfigMeta = &sc.ConfigMeta.String
		}
		configsByCatalogueID[sc.CatalogueID] = append(configsByCatalogueID[sc.CatalogueID], resp)
	}

	result := make([]sourceItemResponse, 0, len(items))
	for _, item := range items {
		if allowedTypes != nil && !allowedTypes[item.SourceType] {
			continue
		}
		resp := sourceItemResponse{
			ID:         item.ID,
			SourceType: item.SourceType,
			ExternalID: item.ExternalID,
			Title:      item.Title,
			Status:     item.Status,
			Configs:    configsByCatalogueID[item.ID],
		}
		if resp.Configs == nil {
			resp.Configs = []sourceConfigResponse{}
		}
		if item.URL.Valid {
			resp.URL = &item.URL.String
		}
		if item.SourceMeta.Valid {
			resp.SourceMeta = &item.SourceMeta.String
		}
		if item.ParentID.Valid {
			v := item.ParentID.Int64
			resp.ParentID = &v
		}
		if item.AISuggestion.Valid {
			resp.AISuggestedPurpose = &item.AISuggestion.String
		}
		result = append(result, resp)
	}

	writeJSON(w, http.StatusOK, result)
}

// --- DELETE /config/sources/{id}/config/{config_id} ---

func (d *Deps) handleDeleteSourceConfig(w http.ResponseWriter, r *http.Request) {
	configID, err := strconv.ParseInt(chi.URLParam(r, "config_id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid config_id")
		return
	}
	if err := d.Store.DeleteSourceConfig(r.Context(), configID); err != nil {
		writeError(w, http.StatusInternalServerError, "delete source config: "+err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- PUT /config/sources/{id} ---

type updateSourceRequest struct {
	Status     string  `json:"status"`
	Purpose    string  `json:"purpose"`
	TeamID     *int64  `json:"team_id"`
	ConfigMeta *string `json:"config_meta"`
}

func (d *Deps) handleUpdateSource(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	var req updateSourceRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	ctx := r.Context()

	// Update catalogue status if provided.
	if req.Status != "" {
		if err := d.Store.UpdateCatalogueStatus(ctx, id, req.Status); err != nil {
			if err == sql.ErrNoRows {
				writeError(w, http.StatusNotFound, "source not found")
			} else {
				writeError(w, http.StatusInternalServerError, "update status: "+err.Error())
			}
			return
		}
	}

	// Update source config if purpose provided.
	if req.Purpose != "" {
		teamID := sql.NullInt64{}
		if req.TeamID != nil {
			teamID = sql.NullInt64{Int64: *req.TeamID, Valid: true}
		}
		configMeta := sql.NullString{}
		if req.ConfigMeta != nil {
			configMeta = sql.NullString{String: *req.ConfigMeta, Valid: true}
		}

		// Handle rollover: if purpose='current_plan' and a different current_plan already exists for the team.
		if req.Purpose == "current_plan" && teamID.Valid {
			existing, err := d.Store.FindCurrentPlanForTeam(ctx, teamID.Int64)
			if err == nil && existing.CatalogueID != id {
				// Rollover: archive item annotations, delete old config.
				if err := d.Store.ArchiveItemAnnotationsForPlan(ctx, teamID.Int64); err != nil {
					writeError(w, http.StatusInternalServerError, "archive annotations: "+err.Error())
					return
				}
				if err := d.Store.DeleteSourceConfig(ctx, existing.ID); err != nil {
					writeError(w, http.StatusInternalServerError, "delete old config: "+err.Error())
					return
				}
			}
		}

		if _, err := d.Store.UpsertSourceConfig(ctx, id, teamID, req.Purpose, configMeta, "manual"); err != nil {
			writeError(w, http.StatusInternalServerError, "upsert source config: "+err.Error())
			return
		}
	}

	// Return updated item.
	item, err := d.Store.GetCatalogueItem(ctx, id)
	if err != nil {
		if err == sql.ErrNoRows {
			writeError(w, http.StatusNotFound, "source not found")
		} else {
			writeError(w, http.StatusInternalServerError, "get source: "+err.Error())
		}
		return
	}

	configs, err := d.Store.GetSourceConfigsByItemID(ctx, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "get configs: "+err.Error())
		return
	}

	resp := catalogueItemToResponse(item, configs)
	writeJSON(w, http.StatusOK, resp)
}

func catalogueItemToResponse(item *store.SourceCatalogue, configs []*store.SourceConfig) sourceItemResponse {
	resp := sourceItemResponse{
		ID:         item.ID,
		SourceType: item.SourceType,
		ExternalID: item.ExternalID,
		Title:      item.Title,
		Status:     item.Status,
		Configs:    make([]sourceConfigResponse, 0, len(configs)),
	}
	if item.URL.Valid {
		resp.URL = &item.URL.String
	}
	if item.SourceMeta.Valid {
		resp.SourceMeta = &item.SourceMeta.String
	}
	if item.ParentID.Valid {
		v := item.ParentID.Int64
		resp.ParentID = &v
	}
	if item.AISuggestion.Valid {
		resp.AISuggestedPurpose = &item.AISuggestion.String
	}
	for _, sc := range configs {
		cr := sourceConfigResponse{ID: sc.ID, Purpose: sc.Purpose}
		if sc.TeamID.Valid {
			v := sc.TeamID.Int64
			cr.TeamID = &v
		}
		if sc.ConfigMeta.Valid {
			cr.ConfigMeta = &sc.ConfigMeta.String
		}
		resp.Configs = append(resp.Configs, cr)
	}
	return resp
}

func (d *Deps) handleClassify(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ItemIDs []int64 `json:"item_ids"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(body.ItemIDs) == 0 {
		writeError(w, http.StatusBadRequest, "item_ids is required")
		return
	}

	syncRunID, err := d.Engine.Classify(r.Context(), body.ItemIDs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]int64{"sync_run_id": syncRunID})
}

func (d *Deps) handleDiscover(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Scope  string `json:"scope"`
		Target string `json:"target"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.Target == "" {
		writeError(w, http.StatusBadRequest, "target is required")
		return
	}

	syncRunID, err := d.Engine.Discover(r.Context(), body.Scope, body.Target)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]int64{"sync_run_id": syncRunID})
}
