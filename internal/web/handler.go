package web

import (
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

//go:embed templates static
var content embed.FS

// Deps holds dependencies for the web handlers.
type Deps struct {
	JWTSecret string
	APIBase   string // e.g. "http://localhost:8080/api"
}

// pageTemplates holds one isolated template set per page so that each page's
// {{define "title"}} / {{define "content"}} blocks don't clobber each other.
var (
	pageTemplates    map[string]*template.Template
	partialTemplates *template.Template
)

var funcMap = template.FuncMap{
	"riskClass":     riskClass,
	"workloadClass": workloadClass,
	"statusIcon":    statusIcon,
	"statusClass":   statusClass,
	"severityClass": severityClass,
	"add":           func(a, b int) int { return a + b },
	"sub":           func(a, b int) int { return a - b },
	"mul":           func(a, b float64) float64 { return a * b },
	"pct":           pct,
	"truncate":      truncate,
	"deref": func(s *string) string {
		if s == nil {
			return ""
		}
		return *s
	},
	"sprintBar":  sprintBar,
	"sprintDayBar": func() template.HTML { return sprintDayBar() },
	"sprintPips": sprintPips,
	"safeHTML":  func(s string) template.HTML { return template.HTML(s) },
}

func init() {
	pageTemplates = make(map[string]*template.Template)

	// Pages that use the base layout (base.html + partials + page file).
	layoutPages := []string{
		"org", "team",
		"config_root", "config_sources", "config_teams",
		"config_users", "config_annotations", "config_admin",
	}
	for _, name := range layoutPages {
		t, err := template.New("base").Funcs(funcMap).ParseFS(content,
			"templates/base.html",
			"templates/partials/*.html",
			"templates/"+name+".html",
		)
		if err != nil {
			panic(fmt.Sprintf("template parse error for %s: %v", name, err))
		}
		pageTemplates[name+".html"] = t
	}

	// Login is a standalone page (no nav).
	t, err := template.New("login.html").Funcs(funcMap).ParseFS(content, "templates/login.html")
	if err != nil {
		panic("template parse error for login.html: " + err.Error())
	}
	pageTemplates["login.html"] = t

	// Partials only (for htmx fragments).
	partialTemplates = template.Must(
		template.New("").Funcs(funcMap).ParseFS(content, "templates/partials/*.html"),
	)
}

// staticHandler serves files from the embedded static directory.
func staticHandler() http.Handler {
	sub, err := fs.Sub(content, "static")
	if err != nil {
		panic(err)
	}
	return http.FileServer(http.FS(sub))
}

// NewRouter returns the chi router for the web frontend.
func NewRouter(deps *Deps) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)

	// Static assets
	r.Handle("/static/*", http.StripPrefix("/static/", staticHandler()))

	// Public
	r.Get("/login", deps.loginPage)
	r.Post("/login", deps.loginPost)
	r.Post("/logout", deps.logout)

	// Protected
	r.Group(func(r chi.Router) {
		r.Use(deps.requireAuth)

		// Dashboard
		r.Get("/", deps.orgOverview)
		r.Post("/sync", deps.postSync)
		r.Get("/sync/{id}/status", deps.syncStatus)

		// Team
		r.Get("/teams/{id}", deps.teamDashboard)
		r.Post("/teams/{id}/sync", deps.postTeamSync)
		r.Get("/teams/{teamID}/annotations/form", deps.annotationNewForm)

		// Annotations (htmx mutations)
		r.Post("/annotations", deps.createAnnotation)
		r.Put("/annotations/{id}", deps.updateAnnotation)
		r.Delete("/annotations/{id}", deps.deleteAnnotation)

		// Config (view)
		r.Get("/config", deps.configRoot)
		r.Get("/config/sources", deps.configSources)
		r.Get("/config/teams", deps.configTeams)
		r.Get("/config/annotations", deps.configAnnotations)

		// Config mutations (edit role enforced per-handler)
		r.Post("/config/sources/discover", deps.configSourcesDiscover)
		r.Post("/config/sources/classify", deps.configSourcesClassify)
		r.Put("/config/sources/{id}", deps.configSourceUpdate)
		r.Delete("/config/sources/{id}/config/{cid}", deps.configSourceDeleteConfig)

		r.Post("/config/teams", deps.configTeamCreate)
		r.Put("/config/teams/{id}", deps.configTeamUpdate)
		r.Delete("/config/teams/{id}", deps.configTeamDelete)
		r.Post("/config/teams/{id}/members", deps.configMemberAdd)
		r.Put("/config/members/{id}", deps.configMemberUpdate)
		r.Delete("/config/members/{id}", deps.configMemberDelete)

		r.Get("/config/users", deps.configUsers)
		r.Post("/config/users", deps.configUserCreate)
		r.Put("/config/users/{id}", deps.configUserUpdate)
		r.Delete("/config/users/{id}", deps.configUserDelete)

		r.Post("/config/annotations", deps.configAnnotationCreate)
		r.Put("/config/annotations/{id}", deps.configAnnotationUpdate)
		r.Delete("/config/annotations/{id}", deps.configAnnotationDelete)

		r.Get("/config/admin", deps.configAdmin)
		r.Post("/config/admin/autotag", deps.configAdminAutotag)
		r.Delete("/config/admin/ai-cache", deps.configAdminClearCache)
	})

	return r
}

// render executes a full page template. Layout pages execute "base"; login
// executes "login.html" directly (it has its own <html> wrapper).
func render(w http.ResponseWriter, name string, data any) {
	t, ok := pageTemplates[name]
	if !ok {
		http.Error(w, "template not found: "+name, http.StatusInternalServerError)
		return
	}
	execName := "base"
	if name == "login.html" {
		execName = "login.html"
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := t.ExecuteTemplate(w, execName, data); err != nil {
		http.Error(w, "template error: "+err.Error(), http.StatusInternalServerError)
	}
}

// renderPartial executes a named htmx partial fragment.
func renderPartial(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := partialTemplates.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, "partial error: "+err.Error(), http.StatusInternalServerError)
	}
}
