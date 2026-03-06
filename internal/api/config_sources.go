package api

import "net/http"

func (d *Deps) handleListSources(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotImplemented, "not implemented")
}

func (d *Deps) handleUpdateSource(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotImplemented, "not implemented")
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
	if body.Scope == "" {
		writeError(w, http.StatusBadRequest, "scope is required")
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
