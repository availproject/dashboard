package sync

import (
	"context"
	"encoding/json"
	"log"
	"strings"
)

// AutoTag iterates all active github_project source configs and calls
// github.AutoTagIssues for each configured repo+project.
func (e *Engine) AutoTag(ctx context.Context) error {
	// Load all catalogue items.
	items, err := e.store.ListCatalogue(ctx)
	if err != nil {
		return err
	}

	// Load all teams to build a label map (team name → team name as label).
	teams, err := e.store.ListTeams(ctx)
	if err != nil {
		return err
	}
	teamLabelMap := make(map[string]string, len(teams))
	for _, t := range teams {
		teamLabelMap[t.Name] = t.Name
	}

	for _, item := range items {
		if item.SourceType != "github_project" {
			continue
		}
		if item.Status != "active" {
			continue
		}
		if !item.SourceMeta.Valid || item.SourceMeta.String == "" {
			continue
		}

		var meta struct {
			Owner     string `json:"owner"`
			Repo      string `json:"repo"`
			ProjectID string `json:"project_id"`
		}
		if err := json.Unmarshal([]byte(item.SourceMeta.String), &meta); err != nil {
			log.Printf("autotag: skip %q: parse meta: %v", item.Title, err)
			continue
		}
		if meta.Owner == "" || meta.Repo == "" || meta.ProjectID == "" {
			continue
		}

		// Strip leading slash from repo if owner/repo combined in Owner.
		owner, repo := meta.Owner, meta.Repo
		if strings.Contains(owner, "/") {
			parts := strings.SplitN(owner, "/", 2)
			owner = parts[0]
			repo = parts[1]
		}

		if err := e.github.AutoTagIssues(ctx, owner, repo, meta.ProjectID, teamLabelMap); err != nil {
			log.Printf("autotag: %s/%s project %s: %v", owner, repo, meta.ProjectID, err)
		}
	}

	return nil
}
