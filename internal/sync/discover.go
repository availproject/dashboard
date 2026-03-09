package sync

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/your-org/dashboard/internal/connector"
)

// detectScope infers the discovery scope and normalises the target from a URL
// or short-form string. It is used when the caller passes an empty scope.
//
// Supported inputs:
//   - GitHub project URL:  https://github.com/orgs/{org}/projects/{n}[/...]
//   - GitHub repo URL:     https://github.com/{owner}/{repo}
//   - Notion URL:          https://www.notion.so/... or https://notion.so/...
//   - Any other https URL: treated as metrics_url
//   - org/N (N is integer): github_project, target normalised to "org/N"
//   - owner/repo:           github_repo
func detectScope(target string) (scope, resolved string, err error) {
	if strings.HasPrefix(target, "https://github.com/orgs/") {
		u, parseErr := url.Parse(target)
		if parseErr == nil {
			parts := strings.Split(strings.Trim(u.Path, "/"), "/")
			// path: orgs/{org}/projects/{number}[/...]
			if len(parts) >= 4 && parts[0] == "orgs" && parts[2] == "projects" {
				return "github_project", parts[1] + "/" + parts[3], nil
			}
		}
	}

	if strings.HasPrefix(target, "https://github.com/") {
		u, parseErr := url.Parse(target)
		if parseErr == nil {
			parts := strings.Split(strings.Trim(u.Path, "/"), "/")
			if len(parts) >= 2 && parts[0] != "" && parts[1] != "" {
				return "github_repo", parts[0] + "/" + parts[1], nil
			}
		}
	}

	if strings.HasPrefix(target, "https://www.notion.so/") || strings.HasPrefix(target, "https://notion.so/") {
		return "notion_workspace", target, nil
	}

	if strings.HasPrefix(target, "https://") || strings.HasPrefix(target, "http://") {
		return "metrics_url", target, nil
	}

	// Plain "a/b" — if b is a pure integer, treat as org/project-number.
	if parts := strings.SplitN(target, "/", 2); len(parts) == 2 {
		if _, numErr := strconv.Atoi(parts[1]); numErr == nil {
			return "github_project", target, nil
		}
		return "github_repo", target, nil
	}

	return "", "", fmt.Errorf("cannot detect source type from %q — paste a URL or use owner/repo format", target)
}

// Discover runs a discovery pass for the given scope and target.
// If scope is empty, it is auto-detected from the target URL or short form.
// It creates a sync_run record (status='running') and launches a background
// goroutine to perform the actual work. Returns the syncRunID immediately.
func (e *Engine) Discover(ctx context.Context, scope, target string) (int64, error) {
	if scope == "" {
		detected, resolved, err := detectScope(target)
		if err != nil {
			return 0, fmt.Errorf("discover: %w", err)
		}
		scope = detected
		target = resolved
	}
	run, err := e.store.CreateSyncRun(ctx, sql.NullInt64{}, scope)
	if err != nil {
		return 0, fmt.Errorf("discover: create sync run: %w", err)
	}
	go e.discoverBackground(run.ID, scope, target)
	return run.ID, nil
}

func (e *Engine) discoverBackground(runID int64, scope, target string) {
	ctx := context.Background()

	var items []connector.DiscoveredItem
	var discoverErr error

	switch scope {
	case "notion_workspace":
		items, discoverErr = e.notion.Discover(ctx, target)
	case "github_repo":
		items, discoverErr = e.github.Discover(ctx, target)
	case "github_project":
		items, discoverErr = e.github.DiscoverProject(ctx, target)
	case "metrics_url":
		items, discoverErr = e.discoverMetrics(ctx, target)
	default:
		discoverErr = fmt.Errorf("unknown scope: %s", scope)
	}

	if discoverErr != nil {
		_ = e.store.UpdateSyncRun(ctx, runID, "failed", sql.NullString{
			String: discoverErr.Error(), Valid: true,
		})
		return
	}

	// idMap resolves (sourceType+externalID) → catalogue ID so children can
	// reference their parent's DB id. Items must be ordered parents-first.
	idMap := make(map[string]int64)

	for _, item := range items {
		metaStr := sql.NullString{}
		if item.SourceMeta != nil {
			if b, err := json.Marshal(item.SourceMeta); err == nil {
				metaStr = sql.NullString{String: string(b), Valid: true}
			}
		}

		var parentID sql.NullInt64
		if item.ParentExternalID != "" && item.ParentSourceType != "" {
			key := item.ParentSourceType + "\x00" + item.ParentExternalID
			if pid, ok := idMap[key]; ok {
				parentID = sql.NullInt64{Int64: pid, Valid: true}
			}
		}

		result, err := e.store.UpsertCatalogueItem(ctx,
			item.SourceType, item.ExternalID, item.Title,
			sql.NullString{String: item.URL, Valid: item.URL != ""},
			metaStr,
			parentID,
		)
		if err != nil {
			_ = e.store.UpdateSyncRun(ctx, runID, "failed", sql.NullString{
				String: err.Error(), Valid: true,
			})
			return
		}
		idMap[item.SourceType+"\x00"+item.ExternalID] = result.ID
	}

	_ = e.store.UpdateSyncRun(ctx, runID, "completed", sql.NullString{})
}

// discoverMetrics runs discovery against all metrics connectors (grafana, posthog,
// signoz) and merges the results. Individual connector errors are silently ignored
// so that connectors not applicable to the target URL do not fail the run.
func (e *Engine) discoverMetrics(ctx context.Context, target string) ([]connector.DiscoveredItem, error) {
	var all []connector.DiscoveredItem
	if items, err := e.grafana.Discover(ctx, target); err == nil {
		all = append(all, items...)
	}
	if items, err := e.posthog.Discover(ctx, target); err == nil {
		all = append(all, items...)
	}
	if items, err := e.signoz.Discover(ctx, target); err == nil {
		all = append(all, items...)
	}
	return all, nil
}
