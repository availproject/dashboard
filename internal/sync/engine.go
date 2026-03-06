package sync

import (
	ghconn "github.com/your-org/dashboard/internal/connector/github"
	"github.com/your-org/dashboard/internal/connector/grafana"
	"github.com/your-org/dashboard/internal/connector/notion"
	"github.com/your-org/dashboard/internal/connector/posthog"
	"github.com/your-org/dashboard/internal/connector/signoz"
	"github.com/your-org/dashboard/internal/pipeline"
	"github.com/your-org/dashboard/internal/store"
)

// Engine orchestrates discovery and incremental sync runs.
type Engine struct {
	store    *store.Store
	github   *ghconn.Client
	notion   *notion.Client
	grafana  *grafana.Client
	posthog  *posthog.Client
	signoz   *signoz.Client
	pipeline *pipeline.Runner
}

// New returns a new Engine with the provided dependencies.
func New(
	st *store.Store,
	gh *ghconn.Client,
	no *notion.Client,
	gr *grafana.Client,
	ph *posthog.Client,
	si *signoz.Client,
	pl *pipeline.Runner,
) *Engine {
	return &Engine{
		store:    st,
		github:   gh,
		notion:   no,
		grafana:  gr,
		posthog:  ph,
		signoz:   si,
		pipeline: pl,
	}
}
