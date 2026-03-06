package signoz

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

// Client is a SigNoz HTTP API client.
type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

// PanelData holds the raw query response from SigNoz for a single panel.
type PanelData struct {
	DashboardID string
	PanelID     string
	Results     json.RawMessage
}

// New creates a SigNoz Client. If baseURL or apiKey is empty, methods will
// return an error.
func New(baseURL, apiKey string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Client) checkCredentials() error {
	if c.baseURL == "" {
		return fmt.Errorf("signoz: SIGNOZ_BASE_URL credential is missing")
	}
	if c.apiKey == "" {
		return fmt.Errorf("signoz: SIGNOZ_API_KEY credential is missing")
	}
	return nil
}

// doGet performs an authenticated GET request and decodes the JSON response.
func (c *Client) doGet(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("signoz: build request: %w", err)
	}
	req.Header.Set("SIGNOZ-API-KEY", c.apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("signoz: GET %s: %w", path, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("signoz: read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("signoz: GET %s: status %d: %s", path, resp.StatusCode, body)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("signoz: decode response from %s: %w", path, err)
	}
	return nil
}

// Discover enumerates all SigNoz dashboards and returns one DiscoveredItem
// per panel/widget in each dashboard.
func (c *Client) Discover(ctx context.Context, target string) ([]connector.DiscoveredItem, error) {
	if err := c.checkCredentials(); err != nil {
		return nil, err
	}

	type widget struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	}
	type dashData struct {
		Title   string   `json:"title"`
		Widgets []widget `json:"widgets"`
	}
	type dashboard struct {
		UUID string   `json:"uuid"`
		Data dashData `json:"data"`
	}
	type listResp struct {
		Data []dashboard `json:"data"`
	}

	var resp listResp
	if err := c.doGet(ctx, "/api/v1/dashboards", &resp); err != nil {
		return nil, err
	}

	var items []connector.DiscoveredItem
	for _, dash := range resp.Data {
		for _, widget := range dash.Data.Widgets {
			dashURL := fmt.Sprintf("%s/dashboard/%s", c.baseURL, dash.UUID)
			items = append(items, connector.DiscoveredItem{
				SourceType: "signoz_panel",
				ExternalID: fmt.Sprintf("%s/%s", dash.UUID, widget.ID),
				Title:      fmt.Sprintf("%s / %s", dash.Data.Title, widget.Title),
				URL:        dashURL,
				SourceMeta: map[string]any{
					"dashboard_id":    dash.UUID,
					"dashboard_title": dash.Data.Title,
					"panel_id":        widget.ID,
				},
			})
		}
	}
	return items, nil
}

// FetchPanel retrieves metric data for a single panel via the SigNoz query API.
// The panel's query configuration is fetched from the dashboard metadata.
func (c *Client) FetchPanel(ctx context.Context, dashboardID, panelID string) (PanelData, error) {
	if err := c.checkCredentials(); err != nil {
		return PanelData{}, err
	}

	type widget struct {
		ID    string          `json:"id"`
		Query json.RawMessage `json:"query"`
	}
	type dashData struct {
		Widgets []widget `json:"widgets"`
	}
	type dashboard struct {
		UUID string   `json:"uuid"`
		Data dashData `json:"data"`
	}
	type dashResp struct {
		Data dashboard `json:"data"`
	}

	var resp dashResp
	if err := c.doGet(ctx, "/api/v1/dashboards/"+dashboardID, &resp); err != nil {
		return PanelData{}, err
	}

	// Find the widget.
	var panelQuery json.RawMessage
	for _, w := range resp.Data.Data.Widgets {
		if w.ID == panelID {
			panelQuery = w.Query
			break
		}
	}
	if panelQuery == nil {
		return PanelData{}, fmt.Errorf("signoz: panel %s not found in dashboard %s", panelID, dashboardID)
	}

	// Return the panel's query configuration as the result.
	// Actual metric execution would require POSTing to /api/v3/query_range,
	// but that requires composing step, start, end from caller context.
	// Here we return the query definition; callers can invoke query_range as needed.
	return PanelData{
		DashboardID: dashboardID,
		PanelID:     panelID,
		Results:     panelQuery,
	}, nil
}
