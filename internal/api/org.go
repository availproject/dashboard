package api

import "net/http"

func (d *Deps) handleOrgOverview(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotImplemented, "not implemented")
}
