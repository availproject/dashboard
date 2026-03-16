package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

// teamConfigSlotItem represents a single configured source in a slot.
type teamConfigSlotItem struct {
	ID           int64             `json:"id"`
	CatalogueID  int64             `json:"catalogue_id"`
	Title        string            `json:"title"`
	SourceType   string            `json:"source_type"`
	URL          *string           `json:"url,omitempty"`
	Provenance   string            `json:"provenance"`
	SprintStatus *string           `json:"sprint_status,omitempty"`
	BoardConfig  *boardConfigMeta  `json:"board_config,omitempty"`
}

// boardConfigMeta holds the user-configurable filter fields for a github_project source.
type boardConfigMeta struct {
	TeamAreaField string `json:"team_area_field"`
	TeamAreaValue string `json:"team_area_value"`
	SprintField   string `json:"sprint_field"`
}

// teamConfigSlots is the response for GET /teams/{id}/config.
type teamConfigSlots struct {
	TeamID           int64                          `json:"team_id"`
	TeamName         string                         `json:"team_name"`
	MarketingLabel   *string                        `json:"marketing_label,omitempty"`
	ExtractionStatus string                         `json:"extraction_status"` // "none","running","done"
	Slots            map[string][]teamConfigSlotItem `json:"slots"`
}

var teamSlotKeys = []string{
	"project_homepage",
	"goals_doc",
	"sprint_doc",
	"github_repo",
	"github_project",
	"metrics_panel",
	"marketing_calendar",
}

// handleGetTeamConfig returns slot config grouped by slot for a team.
func (d *Deps) handleGetTeamConfig(w http.ResponseWriter, r *http.Request) {
	teamID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid team id")
		return
	}
	ctx := r.Context()

	team, err := d.Store.GetTeam(ctx, teamID)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "team not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "get team: "+err.Error())
		return
	}

	nullTeamID := sql.NullInt64{Int64: teamID, Valid: true}

	configs, err := d.Store.GetSourceConfigsForScope(ctx, nullTeamID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "get configs: "+err.Error())
		return
	}

	// Build slots map (always include all keys, even if empty).
	slots := make(map[string][]teamConfigSlotItem)
	for _, key := range teamSlotKeys {
		slots[key] = []teamConfigSlotItem{}
	}

	for _, cfg := range configs {
		// Only include slot purposes.
		found := false
		for _, key := range teamSlotKeys {
			if cfg.Purpose == key {
				found = true
				break
			}
		}
		if !found {
			continue
		}

		item, err := d.Store.GetCatalogueItem(ctx, cfg.CatalogueID)
		if err != nil {
			continue
		}

		slotItem := teamConfigSlotItem{
			ID:          cfg.ID,
			CatalogueID: cfg.CatalogueID,
			Title:       item.Title,
			SourceType:  item.SourceType,
			Provenance:  cfg.Provenance,
		}
		if item.URL.Valid {
			slotItem.URL = &item.URL.String
		}

		// For sprint_doc, parse sprint_status from config_meta.
		if cfg.Purpose == "sprint_doc" && cfg.ConfigMeta.Valid && cfg.ConfigMeta.String != "" {
			var meta map[string]string
			if err := json.Unmarshal([]byte(cfg.ConfigMeta.String), &meta); err == nil {
				if ss, ok := meta["sprint_status"]; ok && ss != "" {
					slotItem.SprintStatus = &ss
				}
			}
		}

		// For github_project, parse board filter config from config_meta.
		if cfg.Purpose == "github_project" && cfg.ConfigMeta.Valid && cfg.ConfigMeta.String != "" {
			var bc boardConfigMeta
			if err := json.Unmarshal([]byte(cfg.ConfigMeta.String), &bc); err == nil {
				slotItem.BoardConfig = &bc
			}
		}

		slots[cfg.Purpose] = append(slots[cfg.Purpose], slotItem)
	}

	// Determine extraction_status.
	extractionStatus := "none"
	// Check if homepage is configured.
	if len(slots["project_homepage"]) > 0 {
		extractionStatus = "done"
	}
	// Check if a run is currently running.
	if _, err := d.Store.GetRunningSyncRun(ctx, "homepage_extract", nullTeamID); err == nil {
		extractionStatus = "running"
	}

	resp := teamConfigSlots{
		TeamID:           teamID,
		TeamName:         team.Name,
		ExtractionStatus: extractionStatus,
		Slots:            slots,
	}
	if team.MarketingLabel.Valid && team.MarketingLabel.String != "" {
		resp.MarketingLabel = &team.MarketingLabel.String
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleGetMarketingLabels returns available project label options from the
// team's configured marketing calendar Notion database.
func (d *Deps) handleGetMarketingLabels(w http.ResponseWriter, r *http.Request) {
	teamID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid team id")
		return
	}
	labels, err := d.Engine.GetMarketingLabels(r.Context(), teamID)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string][]string{"labels": labels})
}

// handleGetBoardFields returns the fields (with options) for the team's configured
// github_project board, for use in the config UI picker.
func (d *Deps) handleGetBoardFields(w http.ResponseWriter, r *http.Request) {
	teamID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid team id")
		return
	}
	fields, err := d.Engine.GetBoardFields(r.Context(), teamID)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"fields": fields})
}

// handleSetTeamHomepage sets the homepage for a team and triggers extraction.
func (d *Deps) handleSetTeamHomepage(w http.ResponseWriter, r *http.Request) {
	teamID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid team id")
		return
	}
	ctx := r.Context()

	var req struct {
		CatalogueID int64 `json:"catalogue_id"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.CatalogueID == 0 {
		writeError(w, http.StatusBadRequest, "catalogue_id is required")
		return
	}

	nullTeamID := sql.NullInt64{Int64: teamID, Valid: true}

	// Delete any existing project_homepage configs for this team.
	existingHomepages, err := d.Store.GetConfigsByPurpose(ctx, nullTeamID, "project_homepage")
	if err == nil {
		for _, cfg := range existingHomepages {
			_ = d.Store.DeleteSourceConfig(ctx, cfg.ID)
		}
	}

	// Upsert new homepage config.
	if _, err := d.Store.UpsertSourceConfig(ctx, req.CatalogueID, nullTeamID, "project_homepage",
		sql.NullString{}, "manual"); err != nil {
		writeError(w, http.StatusInternalServerError, "upsert homepage config: "+err.Error())
		return
	}

	// Start extraction.
	runID, err := d.Engine.HomepageExtract(ctx, teamID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "start extraction: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]int64{"sync_run_id": runID})
}

// handleTeamReextract re-runs homepage extraction for a team.
func (d *Deps) handleTeamReextract(w http.ResponseWriter, r *http.Request) {
	teamID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid team id")
		return
	}
	ctx := r.Context()

	nullTeamID := sql.NullInt64{Int64: teamID, Valid: true}

	// Check that homepage exists.
	homepageConfigs, err := d.Store.GetConfigsByPurpose(ctx, nullTeamID, "project_homepage")
	if err != nil || len(homepageConfigs) == 0 {
		writeError(w, http.StatusBadRequest, "no homepage configured for this team")
		return
	}

	runID, err := d.Engine.HomepageExtract(ctx, teamID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "start extraction: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]int64{"sync_run_id": runID})
}
