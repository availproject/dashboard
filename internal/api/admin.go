package api

import (
	"context"
	"database/sql"
	"log"
	"net/http"
)

// DELETE /admin/ai-cache — clears all AI cache entries.
// Optional query param ?pipeline=<name> to limit to one pipeline.
func (d *Deps) handleAdminClearAICache(w http.ResponseWriter, r *http.Request) {
	pipeline := r.URL.Query().Get("pipeline")
	n, err := d.Store.ClearAICache(r.Context(), pipeline)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "clear cache: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": n})
}

// GET /admin/autotag — starts AutoTag in the background and returns a sync run ID for polling.
func (d *Deps) handleAdminAutotag(w http.ResponseWriter, r *http.Request) {
	if d.Engine == nil {
		writeError(w, http.StatusServiceUnavailable, "engine not configured")
		return
	}
	run, err := d.Store.CreateSyncRun(r.Context(), sql.NullInt64{}, "autotag")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "create run: "+err.Error())
		return
	}
	go func() {
		ctx := context.Background()
		if err := d.Engine.AutoTag(ctx); err != nil {
			log.Printf("ERROR autotag: %v", err)
			_ = d.Store.UpdateSyncRun(ctx, run.ID, "error", sql.NullString{String: err.Error(), Valid: true})
			return
		}
		_ = d.Store.UpdateSyncRun(ctx, run.ID, "done", sql.NullString{})
	}()
	writeJSON(w, http.StatusAccepted, map[string]any{"sync_run_id": run.ID})
}
