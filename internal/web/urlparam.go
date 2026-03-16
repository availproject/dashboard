package web

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// chi_urlparam is a local alias to avoid repeating the import everywhere.
func chi_urlparam(r *http.Request, key string) string {
	return chi.URLParam(r, key)
}
