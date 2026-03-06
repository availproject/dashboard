package posthog

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/your-org/dashboard/internal/connector"
)

// Client is a PostHog HTTP API client.
type Client struct {
	host   string
	apiKey string
	http   *http.Client
}

// InsightResult holds the raw result from a single PostHog insight query.
type InsightResult struct {
	ID     int
	Name   string
	Result json.RawMessage
}

// New creates a PostHog Client. If host or apiKey is empty, methods will
// return an error.
func New(host, apiKey string) *Client {
	return &Client{
		host:   strings.TrimRight(host, "/"),
		apiKey: apiKey,
		http:   &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Client) checkCredentials() error {
	if c.host == "" {
		return fmt.Errorf("posthog: POSTHOG_HOST credential is missing")
	}
	if c.apiKey == "" {
		return fmt.Errorf("posthog: POSTHOG_API_KEY credential is missing")
	}
	return nil
}

// doGet performs an authenticated GET request and decodes the JSON response.
func (c *Client) doGet(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.host+path, nil)
	if err != nil {
		return fmt.Errorf("posthog: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("posthog: GET %s: %w", path, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("posthog: read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("posthog: GET %s: status %d: %s", path, resp.StatusCode, body)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("posthog: decode response from %s: %w", path, err)
	}
	return nil
}

// Discover enumerates dashboards for a project and returns one DiscoveredItem
// per insight/tile via GET /api/projects/:id/dashboards/.
func (c *Client) Discover(ctx context.Context, projectID string) ([]connector.DiscoveredItem, error) {
	if err := c.checkCredentials(); err != nil {
		return nil, err
	}

	type tile struct {
		ID      int `json:"id"`
		Insight *struct {
			ID          int    `json:"id"`
			Name        string `json:"name"`
			Description string `json:"description"`
			ShortID     string `json:"short_id"`
		} `json:"insight"`
	}
	type dashboard struct {
		ID    int    `json:"id"`
		Name  string `json:"name"`
		Tiles []tile `json:"tiles"`
	}
	type listResp struct {
		Results []dashboard `json:"results"`
		Next    *string     `json:"next"`
	}

	path := fmt.Sprintf("/api/projects/%s/dashboards/", projectID)
	var items []connector.DiscoveredItem

	for path != "" {
		var resp listResp
		if err := c.doGet(ctx, path, &resp); err != nil {
			return nil, err
		}

		for _, dash := range resp.Results {
			for _, t := range dash.Tiles {
				if t.Insight == nil {
					continue
				}
				insightURL := fmt.Sprintf("%s/insights/%s", c.host, t.Insight.ShortID)
				items = append(items, connector.DiscoveredItem{
					SourceType: "posthog_insight",
					ExternalID: fmt.Sprintf("%s/insights/%d", projectID, t.Insight.ID),
					Title:      fmt.Sprintf("%s / %s", dash.Name, t.Insight.Name),
					URL:        insightURL,
					Excerpt:    t.Insight.Description,
					SourceMeta: map[string]any{
						"project_id":    projectID,
						"dashboard_id":  fmt.Sprintf("%d", dash.ID),
						"dashboard_name": dash.Name,
						"insight_id":    fmt.Sprintf("%d", t.Insight.ID),
						"short_id":      t.Insight.ShortID,
					},
				})
			}
		}

		// Follow pagination: next is an absolute URL; extract the path portion.
		if resp.Next == nil || *resp.Next == "" {
			break
		}
		nextPath := *resp.Next
		if strings.HasPrefix(nextPath, c.host) {
			nextPath = strings.TrimPrefix(nextPath, c.host)
		}
		path = nextPath
	}

	return items, nil
}

// FetchInsight retrieves the result for a single insight via
// GET /api/projects/:id/insights/:id/.
func (c *Client) FetchInsight(ctx context.Context, projectID, insightID string) (InsightResult, error) {
	if err := c.checkCredentials(); err != nil {
		return InsightResult{}, err
	}

	path := fmt.Sprintf("/api/projects/%s/insights/%s/", projectID, insightID)

	var resp struct {
		ID     int             `json:"id"`
		Name   string          `json:"name"`
		Result json.RawMessage `json:"result"`
	}
	if err := c.doGet(ctx, path, &resp); err != nil {
		return InsightResult{}, err
	}

	return InsightResult{
		ID:     resp.ID,
		Name:   resp.Name,
		Result: resp.Result,
	}, nil
}
