// discover is a diagnostic CLI that runs a discovery pass directly against
// a connector and prints every found item to stdout. It loads credentials
// from .env (same as the server) and does not touch the database.
//
// Usage:
//
//	go run ./cmd/discover notion_workspace https://www.notion.so/workspace/Project-abc123
//	go run ./cmd/discover github_repo owner/repo
//	go run ./cmd/discover metrics_url https://grafana.example.com/d/abc123
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/your-org/dashboard/internal/config"
	ghconn "github.com/your-org/dashboard/internal/connector/github"
	"github.com/your-org/dashboard/internal/connector/grafana"
	notionconn "github.com/your-org/dashboard/internal/connector/notion"
	"github.com/your-org/dashboard/internal/connector/posthog"
	"github.com/your-org/dashboard/internal/connector/signoz"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: discover <scope> [target]")
		fmt.Fprintln(os.Stderr, "  scopes: notion_workspace  github_repo  metrics_url")
		os.Exit(1)
	}
	scope := os.Args[1]
	target := ""
	if len(os.Args) >= 3 {
		target = os.Args[2]
	}

	if err := config.LoadEnv(".env"); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not load .env: %v\n", err)
	}

	ctx := context.Background()

	switch scope {
	case "notion_workspace":
		if target == "" {
			fmt.Fprintln(os.Stderr, "notion_workspace requires a target: Notion page URL")
			os.Exit(1)
		}
		runNotion(ctx, target)
	case "github_repo":
		if target == "" {
			fmt.Fprintln(os.Stderr, "github_repo requires a target: owner/repo")
			os.Exit(1)
		}
		runGitHub(ctx, target)
	case "metrics_url":
		if target == "" {
			fmt.Fprintln(os.Stderr, "metrics_url requires a target URL")
			os.Exit(1)
		}
		runMetrics(ctx, target)
	default:
		fmt.Fprintf(os.Stderr, "unknown scope %q — use notion_workspace, github_repo, or metrics_url\n", scope)
		os.Exit(1)
	}
}

func runNotion(ctx context.Context, target string) {
	token := config.NotionToken()
	fmt.Printf("Notion token present: %v\n\n", token != "")
	c := notionconn.New(token)
	items, err := c.Discover(ctx, target)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Found %d item(s):\n", len(items))
	for _, item := range items {
		fmt.Printf("  [%s] %q\n    url: %s\n    id:  %s\n", item.SourceType, item.Title, item.URL, item.ExternalID)
	}
}

func runGitHub(ctx context.Context, target string) {
	token := config.GitHubToken()
	fmt.Printf("GitHub token present: %v\n\n", token != "")
	c := ghconn.New(token)
	items, err := c.Discover(ctx, target)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Found %d item(s):\n", len(items))
	for _, item := range items {
		fmt.Printf("  [%s] %q\n    url: %s\n    id:  %s\n", item.SourceType, item.Title, item.URL, item.ExternalID)
	}
}

func runMetrics(ctx context.Context, target string) {
	results := map[string]struct {
		count int
		err   error
	}{}

	gf := grafana.New(config.GrafanaToken(), config.GrafanaBaseURL())
	items, err := gf.Discover(ctx, target)
	results["grafana"] = struct {
		count int
		err   error
	}{len(items), err}

	ph := posthog.New(config.PostHogAPIKey(), config.PostHogHost())
	items2, err2 := ph.Discover(ctx, target)
	results["posthog"] = struct {
		count int
		err   error
	}{len(items2), err2}

	sz := signoz.New(config.SignozAPIKey(), config.SignozBaseURL())
	items3, err3 := sz.Discover(ctx, target)
	results["signoz"] = struct {
		count int
		err   error
	}{len(items3), err3}

	allItems := append(append(items, items2...), items3...)
	fmt.Printf("Found %d item(s) across all metrics connectors:\n", len(allItems))
	for name, r := range results {
		if r.err != nil {
			fmt.Printf("  %s: error — %v\n", name, r.err)
		} else {
			fmt.Printf("  %s: %d item(s)\n", name, r.count)
		}
	}
	fmt.Println()
	for _, item := range allItems {
		fmt.Printf("  [%s] %q\n    url: %s\n    id:  %s\n", item.SourceType, item.Title, item.URL, item.ExternalID)
	}
}
