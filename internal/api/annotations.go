package api

import "net/http"

func (d *Deps) handleCreateAnnotation(w http.ResponseWriter, r *http.Request) {
	sharedCreateAnnotation(d, w, r)
}

func (d *Deps) handleUpdateAnnotation(w http.ResponseWriter, r *http.Request) {
	sharedUpdateAnnotation(d, w, r)
}

func (d *Deps) handleDeleteAnnotation(w http.ResponseWriter, r *http.Request) {
	sharedDeleteAnnotation(d, w, r)
}
