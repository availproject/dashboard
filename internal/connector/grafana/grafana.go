package grafana

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/your-org/dashboard/internal/connector"
)

// Client is a Grafana HTTP API client.
type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

// PanelData holds the raw query response from /api/ds/query for a single panel.
type PanelData struct {
	DashboardUID string
	PanelID      string
	Results      json.RawMessage
}

// New creates a Grafana Client. If baseURL or token is empty, methods will
// return an error.
func New(baseURL, token string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Client) checkCredentials() error {
	if c.baseURL == "" {
		return connector.NewErrCredentialsMissing("GRAFANA_BASE_URL")
	}
	if c.token == "" {
		return connector.NewErrCredentialsMissing("GRAFANA_TOKEN")
	}
	return nil
}

// extractUID parses the dashboard UID from a Grafana URL.
// Grafana dashboard URLs follow the pattern: /d/{uid}/{slug}
func extractUID(dashboardURL string) (string, error) {
	u, err := url.Parse(dashboardURL)
	if err != nil {
		return "", fmt.Errorf("grafana: invalid dashboard URL %q: %w", dashboardURL, err)
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	// Look for /d/{uid}/... pattern
	for i, p := range parts {
		if p == "d" && i+1 < len(parts) {
			uid := parts[i+1]
			if uid != "" {
				return uid, nil
			}
		}
	}
	return "", fmt.Errorf("grafana: could not extract UID from URL %q (expected /d/{uid}/{slug})", dashboardURL)
}

// doGet performs an authenticated GET request and decodes the JSON response.
func (c *Client) doGet(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("grafana: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("grafana: GET %s: %w", path, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("grafana: read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("grafana: GET %s: status %d: %s", path, resp.StatusCode, body)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("grafana: decode response from %s: %w", path, err)
	}
	return nil
}

// Discover extracts the dashboard UID from dashboardURL, fetches its metadata,
// and returns one DiscoveredItem per panel.
func (c *Client) Discover(ctx context.Context, dashboardURL string) ([]connector.DiscoveredItem, error) {
	if err := c.checkCredentials(); err != nil {
		return nil, err
	}

	uid, err := extractUID(dashboardURL)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Dashboard struct {
			UID    string `json:"uid"`
			Title  string `json:"title"`
			Panels []struct {
				ID    int    `json:"id"`
				Title string `json:"title"`
				Type  string `json:"type"`
			} `json:"panels"`
		} `json:"dashboard"`
		Meta struct {
			URL string `json:"url"`
		} `json:"meta"`
	}
	if err := c.doGet(ctx, "/api/dashboards/uid/"+uid, &resp); err != nil {
		return nil, err
	}

	dashURL := c.baseURL + resp.Meta.URL
	if resp.Meta.URL == "" {
		dashURL = dashboardURL
	}

	var items []connector.DiscoveredItem
	for _, panel := range resp.Dashboard.Panels {
		panelID := fmt.Sprintf("%d", panel.ID)
		items = append(items, connector.DiscoveredItem{
			SourceType: "grafana_panel",
			ExternalID: fmt.Sprintf("%s/panel/%s", uid, panelID),
			Title:      fmt.Sprintf("%s / %s", resp.Dashboard.Title, panel.Title),
			URL:        fmt.Sprintf("%s?viewPanel=%s", dashURL, panelID),
			SourceMeta: map[string]any{
				"dashboard_uid":   uid,
				"dashboard_title": resp.Dashboard.Title,
				"panel_id":        panelID,
				"panel_type":      panel.Type,
			},
		})
	}
	return items, nil
}

// FetchPanel queries panel data via /api/ds/query for the given time range.
// It re-fetches the dashboard to obtain the panel's datasource and query targets.
func (c *Client) FetchPanel(ctx context.Context, dashboardUID, panelID string, from, to time.Time) (PanelData, error) {
	if err := c.checkCredentials(); err != nil {
		return PanelData{}, err
	}

	// Re-fetch dashboard to get panel targets and datasource.
	var dashResp struct {
		Dashboard struct {
			Panels []struct {
				ID         int             `json:"id"`
				Datasource json.RawMessage `json:"datasource"`
				Targets    []json.RawMessage `json:"targets"`
			} `json:"panels"`
		} `json:"dashboard"`
	}
	if err := c.doGet(ctx, "/api/dashboards/uid/"+dashboardUID, &dashResp); err != nil {
		return PanelData{}, err
	}

	// Find the panel.
	var datasource json.RawMessage
	var targets []json.RawMessage
	for _, p := range dashResp.Dashboard.Panels {
		if fmt.Sprintf("%d", p.ID) == panelID {
			datasource = p.Datasource
			targets = p.Targets
			break
		}
	}
	if len(targets) == 0 {
		return PanelData{}, fmt.Errorf("grafana: panel %s not found in dashboard %s or has no targets", panelID, dashboardUID)
	}

	// Build /api/ds/query request.
	// Inject datasource and from/to into each target.
	type query struct {
		RefID      string          `json:"refId,omitempty"`
		Datasource json.RawMessage `json:"datasource,omitempty"`
	}
	type queryReq struct {
		From    string            `json:"from"`
		To      string            `json:"to"`
		Queries []json.RawMessage `json:"queries"`
	}

	fromMs := fmt.Sprintf("%d", from.UnixMilli())
	toMs := fmt.Sprintf("%d", to.UnixMilli())

	// Merge datasource into each target.
	var mergedQueries []json.RawMessage
	for _, t := range targets {
		var m map[string]json.RawMessage
		if err := json.Unmarshal(t, &m); err != nil {
			return PanelData{}, fmt.Errorf("grafana: unmarshal target: %w", err)
		}
		if datasource != nil {
			m["datasource"] = datasource
		}
		merged, err := json.Marshal(m)
		if err != nil {
			return PanelData{}, fmt.Errorf("grafana: marshal merged target: %w", err)
		}
		mergedQueries = append(mergedQueries, merged)
	}

	reqBody := queryReq{From: fromMs, To: toMs, Queries: mergedQueries}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return PanelData{}, fmt.Errorf("grafana: marshal query request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/ds/query", bytes.NewReader(bodyBytes))
	if err != nil {
		return PanelData{}, fmt.Errorf("grafana: build query request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return PanelData{}, fmt.Errorf("grafana: /api/ds/query: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return PanelData{}, fmt.Errorf("grafana: read query response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return PanelData{}, fmt.Errorf("grafana: /api/ds/query: status %d: %s", resp.StatusCode, respBody)
	}

	return PanelData{
		DashboardUID: dashboardUID,
		PanelID:      panelID,
		Results:      json.RawMessage(respBody),
	}, nil
}
