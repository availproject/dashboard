package api

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/your-org/dashboard/internal/config"
	githubconn "github.com/your-org/dashboard/internal/connector/github"
	"github.com/your-org/dashboard/internal/store"
)

// SyncEngine is the interface the API layer uses to trigger syncs.
type SyncEngine interface {
	Sync(ctx context.Context, scope string, teamID *int64) (int64, error)
	Discover(ctx context.Context, scope, target string) (int64, error)
	Classify(ctx context.Context, itemIDs []int64) (int64, error)
	HomepageExtract(ctx context.Context, teamID int64) (int64, error)
	GetMarketingLabels(ctx context.Context, teamID int64) ([]string, error)
	GetBoardFields(ctx context.Context, teamID int64) ([]githubconn.ProjectField, error)
}

// PipelineRunner is the interface the API layer uses to invoke pipelines.
type PipelineRunner interface{}

// Deps holds the dependencies required by the API handlers.
type Deps struct {
	Store    *store.Store
	Config   *config.Config
	Engine   SyncEngine
	Pipeline PipelineRunner
}

// NewRouter builds and returns the chi router with all routes registered.
func NewRouter(deps *Deps) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)

	// Public auth routes
	// MCP endpoint — authenticated via static API key (see config.yaml mcp.api_key)
	r.Mount("/mcp", deps.buildMCPHandler())

	r.Post("/auth/login", deps.handleLogin)
	r.Post("/auth/refresh", deps.handleRefresh)

	// Protected routes
	r.Group(func(r chi.Router) {
		r.Use(RequireAuth(deps.Config.Auth.JWTSecret))

		// Read-only endpoints (view role sufficient)
		r.Get("/org/overview", deps.handleOrgOverview)
		r.Get("/org/calendar", deps.handleGetOrgCalendar)
		r.Get("/teams", deps.handleListTeams)
		r.Get("/teams/{id}/sprint", deps.handleTeamSprint)
		r.Get("/teams/{id}/goals", deps.handleTeamGoals)
		r.Get("/teams/{id}/workload", deps.handleTeamWorkload)
		r.Get("/teams/{id}/velocity", deps.handleTeamVelocity)
		r.Get("/teams/{id}/metrics", deps.handleTeamMetrics)
		r.Get("/teams/{id}/activity", deps.handleGetTeamActivity)
		r.Get("/teams/{id}/marketing", deps.handleGetTeamMarketing)
		r.Get("/teams/{id}/calendar", deps.handleGetTeamCalendar)
		r.Get("/teams/{id}/config", deps.handleGetTeamConfig)
		r.Get("/teams/{id}/marketing-labels", deps.handleGetMarketingLabels)
		r.Get("/teams/{id}/board-fields", deps.handleGetBoardFields)
		r.Get("/sync", deps.handleListSyncRuns)
		r.Get("/sync/{run_id}", deps.handleGetSyncRun)
		r.Get("/config/sources", deps.handleListSources)
		r.Get("/config/annotations", deps.handleListConfigAnnotations)

		// Mutation endpoints (edit role required)
		r.Group(func(r chi.Router) {
			r.Use(RequireRole("edit"))

			r.Post("/sync", deps.handlePostSync)
			r.Post("/config/sources/discover", deps.handleDiscover)
			r.Post("/config/sources/classify", deps.handleClassify)
			r.Put("/config/sources/{id}", deps.handleUpdateSource)
			r.Delete("/config/sources/{id}/config/{config_id}", deps.handleDeleteSourceConfig)

			r.Post("/teams/{id}/homepage", deps.handleSetTeamHomepage)
			r.Post("/teams/{id}/config/reextract", deps.handleTeamReextract)

			r.Post("/config/annotations", deps.handleCreateConfigAnnotation)
			r.Put("/config/annotations/{id}", deps.handleUpdateConfigAnnotation)
			r.Delete("/config/annotations/{id}", deps.handleDeleteConfigAnnotation)

			r.Get("/config/users", deps.handleListUsers)
			r.Post("/config/users", deps.handleCreateUser)
			r.Put("/config/users/{id}", deps.handleUpdateUser)
			r.Delete("/config/users/{id}", deps.handleDeleteUser)

			r.Post("/config/teams", deps.handleCreateTeam)
			r.Put("/config/teams/{id}", deps.handleUpdateTeam)
			r.Put("/config/teams/{id}/marketing-label", deps.handleSetTeamMarketingLabel)
			r.Delete("/config/teams/{id}", deps.handleDeleteTeam)
			r.Post("/config/teams/{id}/members", deps.handleAddMember)
			r.Put("/config/members/{id}", deps.handleUpdateMember)
			r.Delete("/config/members/{id}", deps.handleDeleteMember)

			r.Post("/annotations", deps.handleCreateAnnotation)
			r.Put("/annotations/{id}", deps.handleUpdateAnnotation)
			r.Delete("/annotations/{id}", deps.handleDeleteAnnotation)

			r.Delete("/admin/ai-cache", deps.handleAdminClearAICache)
		})
	})

	return r
}
