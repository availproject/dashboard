package api

import (
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
