package api

import "net/http"

// GET /admin/autotag — triggers engine.AutoTag immediately.
func (d *Deps) handleAdminAutotag(w http.ResponseWriter, r *http.Request) {
	if d.Engine == nil {
		writeError(w, http.StatusServiceUnavailable, "engine not configured")
		return
	}
	if err := d.Engine.AutoTag(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "autotag: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
