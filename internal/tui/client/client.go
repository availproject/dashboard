package client

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ErrUnauthenticated is returned when the server returns 401 and the refresh fails.
var ErrUnauthenticated = errors.New("unauthenticated: please log in")

// tokenFile is the JSON structure stored in ~/.dashboard/token.
type tokenFile struct {
	Token        string `json:"token"`
	RefreshToken string `json:"refresh_token"`
}

func tokenFilePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".dashboard", "token"), nil
}

// Client is the typed HTTP client for the dashboard server.
type Client struct {
	serverAddr   string
	httpClient   *http.Client
	token        string
	refreshToken string
}

// New creates a new Client pointing at the given server address.
// The API is now mounted at /api on the server, so we append that prefix here
// so all existing URL constructions in this file continue to work unchanged.
func New(serverAddr string) *Client {
	// Strip any trailing slash, then append /api so paths like
	// c.serverAddr + "/org/overview" resolve to /api/org/overview.
	base := strings.TrimRight(serverAddr, "/")
	return &Client{
		serverAddr: base + "/api",
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// HasToken returns true if the client has a token in memory.
func (c *Client) HasToken() bool {
	return c.token != ""
}

// IsTokenExpired returns true if the in-memory token is empty or its JWT exp
// claim is in the past. It does not verify the token signature.
func (c *Client) IsTokenExpired() bool {
	if c.token == "" {
		return true
	}
	parts := strings.Split(c.token, ".")
	if len(parts) != 3 {
		return true
	}
	// JWT uses raw base64url (no padding); add padding before decoding.
	payload := parts[1]
	for len(payload)%4 != 0 {
		payload += "="
	}
	data, err := base64.URLEncoding.DecodeString(payload)
	if err != nil {
		return true
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if err := json.Unmarshal(data, &claims); err != nil {
		return true
	}
	return time.Now().Unix() >= claims.Exp
}

// LoadToken reads token+refresh_token from ~/.dashboard/token into memory.
func (c *Client) LoadToken() error {
	path, err := tokenFilePath()
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var tf tokenFile
	if err := json.Unmarshal(data, &tf); err != nil {
		return err
	}
	c.token = tf.Token
	c.refreshToken = tf.RefreshToken
	return nil
}

// SaveToken writes token+refresh_token to ~/.dashboard/token and stores them in memory.
func (c *Client) SaveToken(token, refreshToken string) error {
	c.token = token
	c.refreshToken = refreshToken
	path, err := tokenFilePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.Marshal(tokenFile{Token: token, RefreshToken: refreshToken})
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

// ClearToken removes the stored token from memory and disk.
func (c *Client) ClearToken() {
	c.token = ""
	c.refreshToken = ""
	if path, err := tokenFilePath(); err == nil {
		_ = os.Remove(path)
	}
}

// doRequest executes an HTTP request with the current token, retrying once after
// a successful token refresh if the server returns 401.
func (c *Client) doRequest(method, url string, body []byte) (*http.Response, error) {
	resp, err := c.send(method, url, body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusUnauthorized {
		return resp, nil
	}
	resp.Body.Close()

	// Try to refresh the token.
	if err := c.tryRefresh(); err != nil {
		return nil, err
	}

	// Retry the original request with the new token.
	return c.send(method, url, body)
}

func (c *Client) send(method, url string, body []byte) (*http.Response, error) {
	var bodyReader io.Reader
	if len(body) > 0 {
		bodyReader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		return nil, err
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.httpClient.Do(req)
}

func (c *Client) tryRefresh() error {
	if c.refreshToken == "" {
		c.ClearToken()
		return ErrUnauthenticated
	}
	body, _ := json.Marshal(map[string]string{"refresh_token": c.refreshToken})
	req, err := http.NewRequest("POST", c.serverAddr+"/auth/refresh", bytes.NewReader(body))
	if err != nil {
		c.ClearToken()
		return ErrUnauthenticated
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.ClearToken()
		return ErrUnauthenticated
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		c.ClearToken()
		return ErrUnauthenticated
	}
	var auth struct {
		Token        string `json:"token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&auth); err != nil {
		c.ClearToken()
		return ErrUnauthenticated
	}
	return c.SaveToken(auth.Token, auth.RefreshToken)
}

// decodeJSON reads the response body into v, closing the body afterwards.
func decodeJSON(resp *http.Response, v any) error {
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

// checkStatus reads any error body and returns a formatted error if status is not in accepted.
func checkStatus(resp *http.Response, accepted ...int) error {
	for _, code := range accepted {
		if resp.StatusCode == code {
			return nil
		}
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return fmt.Errorf("server returned %d: %s", resp.StatusCode, string(body))
}

// ---- Response types ----

// AuthResponse is returned by Login.
type AuthResponse struct {
	Token        string `json:"token"`
	RefreshToken string `json:"refresh_token"`
}

// OrgTeamItem is a single team entry in OrgOverviewResponse.
type OrgTeamItem struct {
	ID            int64   `json:"id"`
	Name          string  `json:"name"`
	CurrentSprint int     `json:"current_sprint"`
	TotalSprints  int     `json:"total_sprints"`
	RiskLevel     string  `json:"risk_level"`
	Focus         string  `json:"focus"`
	LastSyncedAt  *string `json:"last_synced_at"`
}

// OrgWorkloadItem is an aggregate workload entry in OrgOverviewResponse.
type OrgWorkloadItem struct {
	Name      string             `json:"name"`
	TotalDays float64            `json:"total_days"`
	Label     string             `json:"label"`
	Breakdown map[string]float64 `json:"breakdown"`
}

// OrgOverviewResponse is returned by GetOrgOverview.
type OrgOverviewResponse struct {
	Teams        []OrgTeamItem    `json:"teams"`
	Workload     []OrgWorkloadItem `json:"workload"`
	LastSyncedAt *string          `json:"last_synced_at"`
}

// TeamMemberItem is a member entry in TeamItem.
type TeamMemberItem struct {
	ID             int64   `json:"id"`
	DisplayName    string  `json:"display_name"`
	GithubUsername *string `json:"github_username"`
	NotionUserID   *string `json:"notion_user_id"`
}

// TeamItem is returned as part of GetTeams.
type TeamItem struct {
	ID             int64            `json:"id"`
	Name           string           `json:"name"`
	MarketingLabel *string          `json:"marketing_label,omitempty"`
	Members        []TeamMemberItem `json:"members"`
}

// SprintResponse is returned by GetSprint.
type SprintResponse struct {
	PlanType          string   `json:"plan_type"`
	PlanTitle         string   `json:"plan_title"`
	PlanURL           string   `json:"plan_url"`
	StartDate         *string  `json:"start_date"`
	CurrentSprint     int      `json:"current_sprint"`
	TotalSprints      int      `json:"total_sprints"`
	StartDateMissing  bool     `json:"start_date_missing"`
	NextPlanStartRisk bool     `json:"next_plan_start_risk"`
	Goals             []string `json:"goals"`
	LastSyncedAt      *string  `json:"last_synced_at"`
}

// BusinessGoalItem is a business-level goal with a status assessment.
type BusinessGoalItem struct {
	Text   string `json:"text"`
	Status string `json:"status"` // on_track|at_risk|behind|unclear
	Note   string `json:"note"`
}

// SprintGoalItem is a sprint-level goal with a completion forecast.
type SprintGoalItem struct {
	Text   string `json:"text"`
	Status string `json:"status"` // on_track|at_risk|unclear
	Note   string `json:"note"`
}

// ConcernItem is a single concern in GoalsResponse.
type ConcernItem struct {
	Key         string `json:"key"`
	Summary     string `json:"summary"`
	Explanation string `json:"explanation"`
	Severity    string `json:"severity"`
	Scope       string `json:"scope"` // strategic|sprint
}

// SectionAnnotation is one annotation belonging to a named section.
type SectionAnnotation struct {
	ID      int64  `json:"id"`
	Content string `json:"content"`
}

// GoalsResponse is returned by GetGoals.
type GoalsResponse struct {
	BusinessGoals      []BusinessGoalItem            `json:"business_goals"`
	SprintGoals        []SprintGoalItem              `json:"sprint_goals"`
	SprintForecast     string                        `json:"sprint_forecast"`
	Concerns           []ConcernItem                 `json:"concerns"`
	SectionAnnotations map[string][]SectionAnnotation `json:"section_annotations"`
	LastSyncedAt       *string                       `json:"last_synced_at"`
}

// WorkloadMember is a member entry in WorkloadResponse.
type WorkloadMember struct {
	Name          string  `json:"name"`
	EstimatedDays float64 `json:"estimated_days"`
	Label         string  `json:"label"`
}

// WorkloadResponse is returned by GetWorkload.
type WorkloadResponse struct {
	Members      []WorkloadMember `json:"members"`
	LastSyncedAt *string          `json:"last_synced_at"`
}

// VelocityBreakdown holds the per-sprint breakdown in VelocityResponse.
type VelocityBreakdown struct {
	Issues  float64 `json:"issues"`
	PRs     float64 `json:"prs"`
	Commits float64 `json:"commits"`
}

// VelocitySprint is a single sprint entry in VelocityResponse.
type VelocitySprint struct {
	Label     string            `json:"label"`
	Score     float64           `json:"score"`
	Breakdown VelocityBreakdown `json:"breakdown"`
}

// VelocityResponse is returned by GetVelocity.
type VelocityResponse struct {
	Sprints      []VelocitySprint `json:"sprints"`
	LastSyncedAt *string          `json:"last_synced_at"`
}

// MetricsPanel is a single panel in MetricsResponse.
type MetricsPanel struct {
	Title   string  `json:"title"`
	Value   *string `json:"value"`
	PanelID string  `json:"panel_id"`
}

// MetricsResponse is returned by GetMetrics.
type MetricsResponse struct {
	Panels       []MetricsPanel `json:"panels"`
	LastSyncedAt *string        `json:"last_synced_at"`
}

// ActivityCommit is a single commit entry in ActivityResponse.
type ActivityCommit struct {
	SHA     string `json:"sha"`
	Author  string `json:"author"`
	Message string `json:"message"`
	Repo    string `json:"repo"`
	Date    string `json:"date"`
}

// ActivityIssue is a single open issue in ActivityResponse.
type ActivityIssue struct {
	Number        int    `json:"number"`
	Title         string `json:"title"`
	Assignee      string `json:"assignee,omitempty"`
	ProjectStatus string `json:"project_status,omitempty"`
}

// ActivityPR is a single merged PR in ActivityResponse.
type ActivityPR struct {
	Number   int    `json:"number"`
	Title    string `json:"title"`
	Author   string `json:"author"`
	MergedAt string `json:"merged_at"`
}

// ActivityResponse is returned by GetActivity.
type ActivityResponse struct {
	RecentCommits []ActivityCommit `json:"recent_commits"`
	OpenIssues    []ActivityIssue  `json:"open_issues"`
	MergedPRs     []ActivityPR     `json:"merged_prs"`
	LastSyncedAt  *string          `json:"last_synced_at"`
}

// MarketingTaskItem is a single task in a MarketingCampaignItem.
type MarketingTaskItem struct {
	Title    string `json:"title"`
	Status   string `json:"status"`
	Assignee string `json:"assignee,omitempty"`
}

// MarketingCampaignItem is a single campaign in MarketingResponse.
type MarketingCampaignItem struct {
	Title     string              `json:"title"`
	Status    string              `json:"status"`
	DateStart *string             `json:"date_start,omitempty"`
	DateEnd   *string             `json:"date_end,omitempty"`
	Tasks     []MarketingTaskItem `json:"tasks"`
}

// MarketingResponse is returned by GetMarketing.
type MarketingResponse struct {
	Campaigns    []MarketingCampaignItem `json:"campaigns"`
	LastSyncedAt *string                 `json:"last_synced_at"`
}

// OrgCalendarEvent is a single event in the org-level calendar, enriched with team info.
type OrgCalendarEvent struct {
	TeamID         int64  `json:"team_id"`
	TeamName       string `json:"team_name"`
	Date           string `json:"date,omitempty"`
	EndDate        string `json:"end_date,omitempty"`
	Title          string `json:"title"`
	EventType      string `json:"event_type"`
	DateConfidence string `json:"date_confidence"`
	HasFlags       bool   `json:"has_flags"`
	NeedsDate      bool   `json:"needs_date"`
}

// OrgCalendarResponse is returned by GetOrgCalendar.
type OrgCalendarResponse struct {
	Events  []OrgCalendarEvent `json:"events"`
	Undated []OrgCalendarEvent `json:"undated"`
}

// CalendarEventItem is a single event in the calendar response.
type CalendarEventItem struct {
	EventKey       string `json:"event_key"`
	Title          string `json:"title"`
	EventType      string `json:"event_type"`
	SourceClass    string `json:"source_class"`
	Date           string `json:"date,omitempty"`
	DateConfidence string `json:"date_confidence"`
	EndDate        string `json:"end_date,omitempty"`
	Sources        any    `json:"sources,omitempty"`
	Flags          any    `json:"flags,omitempty"`
	NeedsDate      bool   `json:"needs_date"`
}

// CalendarResponse is returned by GetCalendar.
type CalendarResponse struct {
	Events  []CalendarEventItem `json:"events"`
	Undated []CalendarEventItem `json:"undated"`
}

// SyncRunResponse is returned by GetSyncRun.
type SyncRunResponse struct {
	ID      int64            `json:"ID"`
	Status  string           `json:"Status"`
	Scope   string           `json:"Scope"`
	Error   *string          `json:"Error"`
	Timings map[string]int64 `json:"Timings,omitempty"`
}

// SyncRunListItem is a single entry in the list returned by ListSyncRuns.
type SyncRunListItem struct {
	ID         int64            `json:"ID"`
	Scope      string           `json:"Scope"`
	TeamID     *int64           `json:"TeamID,omitempty"`
	TeamName   *string          `json:"TeamName,omitempty"`
	Status     string           `json:"Status"`
	Error      *string          `json:"Error,omitempty"`
	StartedAt  string           `json:"StartedAt"`
	FinishedAt *string          `json:"FinishedAt,omitempty"`
	DurationMs *int64           `json:"DurationMs,omitempty"`
	Timings    map[string]int64 `json:"Timings,omitempty"`
}

// AnnotationResponse is returned by PostAnnotation.
type AnnotationResponse struct {
	ID        int64     `json:"id"`
	TeamID    *int64    `json:"team_id"`
	ItemRef   *string   `json:"item_ref"`
	Tier      string    `json:"tier"`
	Content   string    `json:"content"`
	Archived  bool      `json:"archived"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// SourceConfigResponse is a config entry within SourceItemResponse.
type SourceConfigResponse struct {
	ID         int64   `json:"id"`
	TeamID     *int64  `json:"team_id"`
	Purpose    string  `json:"purpose"`
	ConfigMeta *string `json:"config_meta"`
}

// SourceItemResponse is a catalogue item with its configs.
type SourceItemResponse struct {
	ID                 int64                  `json:"id"`
	SourceType         string                 `json:"source_type"`
	ExternalID         string                 `json:"external_id"`
	Title              string                 `json:"title"`
	URL                *string                `json:"url"`
	ParentID           *int64                 `json:"parent_id"`
	AISuggestedPurpose *string                `json:"ai_suggested_purpose"`
	Status             string                 `json:"status"`
	Provenance         string                 `json:"provenance"`
	Configs            []SourceConfigResponse `json:"configs"`
}

// ProjectField represents a field on a GitHub ProjectV2 board.
type ProjectField struct {
	Name    string   `json:"name"`
	Type    string   `json:"type"` // "single_select", "iteration", "text"
	Options []string `json:"options"`
}

// BoardConfigMeta holds the user-configurable filter fields for a github_project source.
type BoardConfigMeta struct {
	TeamAreaField string `json:"team_area_field"`
	TeamAreaValue string `json:"team_area_value"`
	SprintField   string `json:"sprint_field"`
}

// TeamConfigSlotItem represents a single configured source in a slot.
type TeamConfigSlotItem struct {
	ID           int64            `json:"id"`
	CatalogueID  int64            `json:"catalogue_id"`
	Title        string           `json:"title"`
	SourceType   string           `json:"source_type"`
	URL          *string          `json:"url,omitempty"`
	Provenance   string           `json:"provenance"`
	SprintStatus *string          `json:"sprint_status,omitempty"`
	BoardConfig  *BoardConfigMeta `json:"board_config,omitempty"`
}

// TeamConfigSlotsResponse mirrors the API response for GET /teams/{id}/config.
type TeamConfigSlotsResponse struct {
	TeamID           int64                          `json:"team_id"`
	TeamName         string                         `json:"team_name"`
	MarketingLabel   *string                        `json:"marketing_label,omitempty"`
	ExtractionStatus string                         `json:"extraction_status"`
	Slots            map[string][]TeamConfigSlotItem `json:"slots"`
}

// GroupedAnnotationsResponse is returned by GetConfigAnnotations.
type GroupedAnnotationsResponse struct {
	Item []AnnotationResponse `json:"item"`
	Team []AnnotationResponse `json:"team"`
}

// UserResponse is returned by GetConfigUsers and PostConfigUser.
type UserResponse struct {
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	Role      string `json:"role"`
	CreatedAt string `json:"created_at"`
}

// ---- Client methods ----

// Login authenticates and saves the token to disk.
func (c *Client) Login(username, password string) error {
	body, err := json.Marshal(map[string]string{"username": username, "password": password})
	if err != nil {
		return err
	}
	req, err := http.NewRequest("POST", c.serverAddr+"/auth/login", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("invalid credentials")
	}
	if err := checkStatus(resp, http.StatusOK); err != nil {
		return err
	}
	var auth AuthResponse
	if err := json.NewDecoder(resp.Body).Decode(&auth); err != nil {
		return fmt.Errorf("decode login response: %w", err)
	}
	return c.SaveToken(auth.Token, auth.RefreshToken)
}

// GetOrgOverview returns the org-level overview.
func (c *Client) GetOrgOverview() (*OrgOverviewResponse, error) {
	resp, err := c.doRequest("GET", c.serverAddr+"/org/overview", nil)
	if err != nil {
		return nil, err
	}
	if err := checkStatus(resp, http.StatusOK); err != nil {
		return nil, err
	}
	var result OrgOverviewResponse
	return &result, decodeJSON(resp, &result)
}

// GetTeams returns all teams with their members.
func (c *Client) GetTeams() ([]TeamItem, error) {
	resp, err := c.doRequest("GET", c.serverAddr+"/teams", nil)
	if err != nil {
		return nil, err
	}
	if err := checkStatus(resp, http.StatusOK); err != nil {
		return nil, err
	}
	var result []TeamItem
	return result, decodeJSON(resp, &result)
}

// GetSprint returns the current sprint status for the given team.
func (c *Client) GetSprint(teamID int64) (*SprintResponse, error) {
	resp, err := c.doRequest("GET", fmt.Sprintf("%s/teams/%d/sprint", c.serverAddr, teamID), nil)
	if err != nil {
		return nil, err
	}
	if err := checkStatus(resp, http.StatusOK); err != nil {
		return nil, err
	}
	var result SprintResponse
	return &result, decodeJSON(resp, &result)
}

// GetGoals returns goals and concerns for the given team.
func (c *Client) GetGoals(teamID int64) (*GoalsResponse, error) {
	resp, err := c.doRequest("GET", fmt.Sprintf("%s/teams/%d/goals", c.serverAddr, teamID), nil)
	if err != nil {
		return nil, err
	}
	if err := checkStatus(resp, http.StatusOK); err != nil {
		return nil, err
	}
	var result GoalsResponse
	return &result, decodeJSON(resp, &result)
}

// GetWorkload returns workload estimates for the given team.
func (c *Client) GetWorkload(teamID int64) (*WorkloadResponse, error) {
	resp, err := c.doRequest("GET", fmt.Sprintf("%s/teams/%d/workload", c.serverAddr, teamID), nil)
	if err != nil {
		return nil, err
	}
	if err := checkStatus(resp, http.StatusOK); err != nil {
		return nil, err
	}
	var result WorkloadResponse
	return &result, decodeJSON(resp, &result)
}

// GetVelocity returns velocity data for the given team.
func (c *Client) GetVelocity(teamID int64) (*VelocityResponse, error) {
	resp, err := c.doRequest("GET", fmt.Sprintf("%s/teams/%d/velocity", c.serverAddr, teamID), nil)
	if err != nil {
		return nil, err
	}
	if err := checkStatus(resp, http.StatusOK); err != nil {
		return nil, err
	}
	var result VelocityResponse
	return &result, decodeJSON(resp, &result)
}

// GetMetrics returns metrics panels for the given team.
func (c *Client) GetMetrics(teamID int64) (*MetricsResponse, error) {
	resp, err := c.doRequest("GET", fmt.Sprintf("%s/teams/%d/metrics", c.serverAddr, teamID), nil)
	if err != nil {
		return nil, err
	}
	if err := checkStatus(resp, http.StatusOK); err != nil {
		return nil, err
	}
	var result MetricsResponse
	return &result, decodeJSON(resp, &result)
}

// GetActivity returns engineering activity for the given team.
func (c *Client) GetActivity(teamID int64) (*ActivityResponse, error) {
	resp, err := c.doRequest("GET", fmt.Sprintf("%s/teams/%d/activity", c.serverAddr, teamID), nil)
	if err != nil {
		return nil, err
	}
	if err := checkStatus(resp, http.StatusOK); err != nil {
		return nil, err
	}
	var result ActivityResponse
	return &result, decodeJSON(resp, &result)
}

// GetMarketing returns marketing campaign data for the given team.
func (c *Client) GetMarketing(teamID int64) (*MarketingResponse, error) {
	resp, err := c.doRequest("GET", fmt.Sprintf("%s/teams/%d/marketing", c.serverAddr, teamID), nil)
	if err != nil {
		return nil, err
	}
	if err := checkStatus(resp, http.StatusOK); err != nil {
		return nil, err
	}
	var result MarketingResponse
	return &result, decodeJSON(resp, &result)
}

// GetOrgCalendar returns calendar events across all teams.
// from and to are optional YYYY-MM-DD date range filters; when empty all events are returned.
func (c *Client) GetOrgCalendar(from, to string) (*OrgCalendarResponse, error) {
	u := c.serverAddr + "/org/calendar"
	if from != "" && to != "" {
		u += fmt.Sprintf("?from=%s&to=%s", from, to)
	}
	resp, err := c.doRequest("GET", u, nil)
	if err != nil {
		return nil, err
	}
	if err := checkStatus(resp, http.StatusOK); err != nil {
		return nil, err
	}
	var result OrgCalendarResponse
	return &result, decodeJSON(resp, &result)
}

// GetCalendar returns the calendar events for a team.
// from and to are optional YYYY-MM-DD date range filters.
func (c *Client) GetCalendar(teamID int64, from, to string) (*CalendarResponse, error) {
	url := fmt.Sprintf("%s/teams/%d/calendar", c.serverAddr, teamID)
	if from != "" && to != "" {
		url += fmt.Sprintf("?from=%s&to=%s", from, to)
	}
	resp, err := c.doRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	if err := checkStatus(resp, http.StatusOK); err != nil {
		return nil, err
	}
	var result CalendarResponse
	return &result, decodeJSON(resp, &result)
}

// PostSync triggers a sync for the given scope.
// PostAutotag starts the autotag job on the server and returns the sync run ID for polling.
func (c *Client) PostAutotag() (int64, error) {
	resp, err := c.doRequest("GET", c.serverAddr+"/admin/autotag", nil)
	if err != nil {
		return 0, err
	}
	if err := checkStatus(resp, http.StatusAccepted); err != nil {
		return 0, err
	}
	var result struct {
		SyncRunID int64 `json:"sync_run_id"`
	}
	return result.SyncRunID, decodeJSON(resp, &result)
}

func (c *Client) PostSync(scope string, teamID *int64) (int64, error) {
	body, err := json.Marshal(map[string]any{"scope": scope, "team_id": teamID})
	if err != nil {
		return 0, err
	}
	resp, err := c.doRequest("POST", c.serverAddr+"/sync", body)
	if err != nil {
		return 0, err
	}
	if err := checkStatus(resp, http.StatusAccepted); err != nil {
		return 0, err
	}
	var result struct {
		SyncRunID int64 `json:"sync_run_id"`
	}
	return result.SyncRunID, decodeJSON(resp, &result)
}

// GetSyncRun returns the status of a sync run.
func (c *Client) GetSyncRun(runID int64) (*SyncRunResponse, error) {
	resp, err := c.doRequest("GET", fmt.Sprintf("%s/sync/%d", c.serverAddr, runID), nil)
	if err != nil {
		return nil, err
	}
	if err := checkStatus(resp, http.StatusOK); err != nil {
		return nil, err
	}
	var result SyncRunResponse
	return &result, decodeJSON(resp, &result)
}

// ListSyncRuns returns the most recent sync runs.
func (c *Client) ListSyncRuns() ([]SyncRunListItem, error) {
	resp, err := c.doRequest("GET", c.serverAddr+"/sync", nil)
	if err != nil {
		return nil, err
	}
	if err := checkStatus(resp, http.StatusOK); err != nil {
		return nil, err
	}
	var result []SyncRunListItem
	return result, decodeJSON(resp, &result)
}

// PostAnnotation creates a new annotation.
func (c *Client) PostAnnotation(tier string, teamID *int64, itemRef *string, content string) (*AnnotationResponse, error) {
	body, err := json.Marshal(map[string]any{
		"tier":     tier,
		"team_id":  teamID,
		"item_ref": itemRef,
		"content":  content,
	})
	if err != nil {
		return nil, err
	}
	resp, err := c.doRequest("POST", c.serverAddr+"/annotations", body)
	if err != nil {
		return nil, err
	}
	if err := checkStatus(resp, http.StatusCreated); err != nil {
		return nil, err
	}
	var result AnnotationResponse
	return &result, decodeJSON(resp, &result)
}

// PutAnnotation updates an annotation's content.
func (c *Client) PutAnnotation(id int64, content string) error {
	body, err := json.Marshal(map[string]string{"content": content})
	if err != nil {
		return err
	}
	resp, err := c.doRequest("PUT", fmt.Sprintf("%s/annotations/%d", c.serverAddr, id), body)
	if err != nil {
		return err
	}
	return checkStatus(resp, http.StatusNoContent)
}

// DeleteAnnotation removes an annotation.
func (c *Client) DeleteAnnotation(id int64) error {
	resp, err := c.doRequest("DELETE", fmt.Sprintf("%s/annotations/%d", c.serverAddr, id), nil)
	if err != nil {
		return err
	}
	return checkStatus(resp, http.StatusNoContent)
}

// GetConfigSources returns all catalogue items with their source configs.
// Optional sourceTypes filters items by source_type (comma-separated in query param).
func (c *Client) GetConfigSources(sourceTypes ...string) ([]SourceItemResponse, error) {
	url := c.serverAddr + "/config/sources"
	if len(sourceTypes) > 0 {
		url += "?source_type=" + strings.Join(sourceTypes, ",")
	}
	resp, err := c.doRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	if err := checkStatus(resp, http.StatusOK); err != nil {
		return nil, err
	}
	var result []SourceItemResponse
	return result, decodeJSON(resp, &result)
}

// DeleteSourceConfig removes a specific source config entry.
func (c *Client) DeleteSourceConfig(catalogueID, configID int64) error {
	resp, err := c.doRequest("DELETE",
		fmt.Sprintf("%s/config/sources/%d/config/%d", c.serverAddr, catalogueID, configID), nil)
	if err != nil {
		return err
	}
	return checkStatus(resp, http.StatusNoContent)
}

// GetTeamConfig returns the slot config for a team.
func (c *Client) GetTeamConfig(teamID int64) (*TeamConfigSlotsResponse, error) {
	resp, err := c.doRequest("GET", fmt.Sprintf("%s/teams/%d/config", c.serverAddr, teamID), nil)
	if err != nil {
		return nil, err
	}
	if err := checkStatus(resp, http.StatusOK); err != nil {
		return nil, err
	}
	var result TeamConfigSlotsResponse
	return &result, decodeJSON(resp, &result)
}

// PostTeamHomepage sets the homepage for a team and triggers extraction.
// Returns the sync_run_id.
func (c *Client) PostTeamHomepage(teamID int64, catalogueID int64) (int64, error) {
	body, err := json.Marshal(map[string]int64{"catalogue_id": catalogueID})
	if err != nil {
		return 0, err
	}
	resp, err := c.doRequest("POST", fmt.Sprintf("%s/teams/%d/homepage", c.serverAddr, teamID), body)
	if err != nil {
		return 0, err
	}
	if err := checkStatus(resp, http.StatusOK); err != nil {
		return 0, err
	}
	var result struct {
		SyncRunID int64 `json:"sync_run_id"`
	}
	return result.SyncRunID, decodeJSON(resp, &result)
}

// PostTeamReextract re-runs homepage extraction for a team.
// Returns the sync_run_id.
func (c *Client) PostTeamReextract(teamID int64) (int64, error) {
	resp, err := c.doRequest("POST", fmt.Sprintf("%s/teams/%d/config/reextract", c.serverAddr, teamID), nil)
	if err != nil {
		return 0, err
	}
	if err := checkStatus(resp, http.StatusOK); err != nil {
		return 0, err
	}
	var result struct {
		SyncRunID int64 `json:"sync_run_id"`
	}
	return result.SyncRunID, decodeJSON(resp, &result)
}

// PutConfigSource updates a source config entry.
func (c *Client) PutConfigSource(id int64, status string, teamID *int64, purpose, configMeta string) error {
	body, err := json.Marshal(map[string]any{
		"status":      status,
		"team_id":     teamID,
		"purpose":     purpose,
		"config_meta": configMeta,
	})
	if err != nil {
		return err
	}
	resp, err := c.doRequest("PUT", fmt.Sprintf("%s/config/sources/%d", c.serverAddr, id), body)
	if err != nil {
		return err
	}
	return checkStatus(resp, http.StatusOK, http.StatusNoContent)
}

// PostClassify triggers an AI classification run for the given catalogue item IDs.
func (c *Client) PostClassify(itemIDs []int64) (int64, error) {
	body, err := json.Marshal(map[string]any{"item_ids": itemIDs})
	if err != nil {
		return 0, err
	}
	resp, err := c.doRequest("POST", c.serverAddr+"/config/sources/classify", body)
	if err != nil {
		return 0, err
	}
	if err := checkStatus(resp, http.StatusAccepted); err != nil {
		return 0, err
	}
	var result struct {
		SyncRunID int64 `json:"sync_run_id"`
	}
	return result.SyncRunID, decodeJSON(resp, &result)
}

// PostDiscover triggers a discovery run.
func (c *Client) PostDiscover(scope, target string) (int64, error) {
	body, err := json.Marshal(map[string]string{"scope": scope, "target": target})
	if err != nil {
		return 0, err
	}
	resp, err := c.doRequest("POST", c.serverAddr+"/config/sources/discover", body)
	if err != nil {
		return 0, err
	}
	if err := checkStatus(resp, http.StatusAccepted); err != nil {
		return 0, err
	}
	var result struct {
		SyncRunID int64 `json:"sync_run_id"`
	}
	return result.SyncRunID, decodeJSON(resp, &result)
}

// GetConfigAnnotations returns all annotations grouped by tier.
func (c *Client) GetConfigAnnotations() (*GroupedAnnotationsResponse, error) {
	resp, err := c.doRequest("GET", c.serverAddr+"/config/annotations", nil)
	if err != nil {
		return nil, err
	}
	if err := checkStatus(resp, http.StatusOK); err != nil {
		return nil, err
	}
	var result GroupedAnnotationsResponse
	return &result, decodeJSON(resp, &result)
}

// GetConfigUsers returns all users.
func (c *Client) GetConfigUsers() ([]UserResponse, error) {
	resp, err := c.doRequest("GET", c.serverAddr+"/config/users", nil)
	if err != nil {
		return nil, err
	}
	if err := checkStatus(resp, http.StatusOK); err != nil {
		return nil, err
	}
	var result []UserResponse
	return result, decodeJSON(resp, &result)
}

// PostConfigUser creates a new user.
func (c *Client) PostConfigUser(username, password, role string) (*UserResponse, error) {
	body, err := json.Marshal(map[string]string{
		"username": username,
		"password": password,
		"role":     role,
	})
	if err != nil {
		return nil, err
	}
	resp, err := c.doRequest("POST", c.serverAddr+"/config/users", body)
	if err != nil {
		return nil, err
	}
	if err := checkStatus(resp, http.StatusCreated); err != nil {
		return nil, err
	}
	var result UserResponse
	return &result, decodeJSON(resp, &result)
}

// PutConfigUser updates a user's role and/or password.
func (c *Client) PutConfigUser(id int64, role, password string) error {
	body, err := json.Marshal(map[string]string{"role": role, "password": password})
	if err != nil {
		return err
	}
	resp, err := c.doRequest("PUT", fmt.Sprintf("%s/config/users/%d", c.serverAddr, id), body)
	if err != nil {
		return err
	}
	return checkStatus(resp, http.StatusOK, http.StatusNoContent)
}

// DeleteConfigUser deletes a user.
func (c *Client) DeleteConfigUser(id int64) error {
	resp, err := c.doRequest("DELETE", fmt.Sprintf("%s/config/users/%d", c.serverAddr, id), nil)
	if err != nil {
		return err
	}
	return checkStatus(resp, http.StatusNoContent)
}

// TeamConfigResponse is returned by PostConfigTeam and PutConfigTeam.
type TeamConfigResponse struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// MemberConfigResponse is returned by PostConfigMember.
type MemberConfigResponse struct {
	ID             int64   `json:"id"`
	TeamID         int64   `json:"team_id"`
	DisplayName    string  `json:"display_name"`
	GithubUsername *string `json:"github_username"`
	NotionUserID   *string `json:"notion_user_id"`
}

// PostConfigTeam creates a new team.
func (c *Client) PostConfigTeam(name string) (*TeamConfigResponse, error) {
	body, err := json.Marshal(map[string]string{"name": name})
	if err != nil {
		return nil, err
	}
	resp, err := c.doRequest("POST", c.serverAddr+"/config/teams", body)
	if err != nil {
		return nil, err
	}
	if err := checkStatus(resp, http.StatusCreated); err != nil {
		return nil, err
	}
	var result TeamConfigResponse
	return &result, decodeJSON(resp, &result)
}

// PutConfigTeam renames a team.
func (c *Client) PutConfigTeam(id int64, name string) (*TeamConfigResponse, error) {
	body, err := json.Marshal(map[string]string{"name": name})
	if err != nil {
		return nil, err
	}
	resp, err := c.doRequest("PUT", fmt.Sprintf("%s/config/teams/%d", c.serverAddr, id), body)
	if err != nil {
		return nil, err
	}
	if err := checkStatus(resp, http.StatusOK); err != nil {
		return nil, err
	}
	var result TeamConfigResponse
	return &result, decodeJSON(resp, &result)
}

// GetBoardFields returns the fields (with options) for the team's configured
// github_project board, for use in the config UI picker.
func (c *Client) GetBoardFields(teamID int64) ([]ProjectField, error) {
	resp, err := c.doRequest("GET", fmt.Sprintf("%s/teams/%d/board-fields", c.serverAddr, teamID), nil)
	if err != nil {
		return nil, err
	}
	if err := checkStatus(resp, http.StatusOK); err != nil {
		return nil, err
	}
	var result struct {
		Fields []ProjectField `json:"fields"`
	}
	return result.Fields, decodeJSON(resp, &result)
}

// GetTeamMarketingLabels returns the available project label options from the
// team's configured marketing calendar Notion database.
func (c *Client) GetTeamMarketingLabels(teamID int64) ([]string, error) {
	resp, err := c.doRequest("GET", fmt.Sprintf("%s/teams/%d/marketing-labels", c.serverAddr, teamID), nil)
	if err != nil {
		return nil, err
	}
	if err := checkStatus(resp, http.StatusOK); err != nil {
		return nil, err
	}
	var result struct {
		Labels []string `json:"labels"`
	}
	return result.Labels, decodeJSON(resp, &result)
}

// PutTeamMarketingLabel sets or clears the marketing label for a team.
// Pass an empty string to clear the label.
func (c *Client) PutTeamMarketingLabel(id int64, label string) error {
	var labelPtr *string
	if label != "" {
		labelPtr = &label
	}
	body, err := json.Marshal(map[string]any{"label": labelPtr})
	if err != nil {
		return err
	}
	resp, err := c.doRequest("PUT", fmt.Sprintf("%s/config/teams/%d/marketing-label", c.serverAddr, id), body)
	if err != nil {
		return err
	}
	return checkStatus(resp, http.StatusOK)
}

// DeleteConfigTeam deletes a team.
func (c *Client) DeleteConfigTeam(id int64) error {
	resp, err := c.doRequest("DELETE", fmt.Sprintf("%s/config/teams/%d", c.serverAddr, id), nil)
	if err != nil {
		return err
	}
	return checkStatus(resp, http.StatusNoContent)
}

// PostConfigMember adds a member to a team.
func (c *Client) PostConfigMember(teamID int64, displayName string, githubUsername, notionUserID *string) (*MemberConfigResponse, error) {
	body, err := json.Marshal(map[string]any{
		"display_name":    displayName,
		"github_username": githubUsername,
		"notion_user_id":  notionUserID,
	})
	if err != nil {
		return nil, err
	}
	resp, err := c.doRequest("POST", fmt.Sprintf("%s/config/teams/%d/members", c.serverAddr, teamID), body)
	if err != nil {
		return nil, err
	}
	if err := checkStatus(resp, http.StatusCreated); err != nil {
		return nil, err
	}
	var result MemberConfigResponse
	return &result, decodeJSON(resp, &result)
}

// PutConfigMember updates a team member.
func (c *Client) PutConfigMember(id int64, displayName string, githubUsername, notionUserID *string) error {
	body, err := json.Marshal(map[string]any{
		"display_name":    displayName,
		"github_username": githubUsername,
		"notion_user_id":  notionUserID,
	})
	if err != nil {
		return err
	}
	resp, err := c.doRequest("PUT", fmt.Sprintf("%s/config/members/%d", c.serverAddr, id), body)
	if err != nil {
		return err
	}
	return checkStatus(resp, http.StatusNoContent)
}

// DeleteConfigMember removes a team member.
func (c *Client) DeleteConfigMember(id int64) error {
	resp, err := c.doRequest("DELETE", fmt.Sprintf("%s/config/members/%d", c.serverAddr, id), nil)
	if err != nil {
		return err
	}
	return checkStatus(resp, http.StatusNoContent)
}
