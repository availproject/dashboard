package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/your-org/dashboard/internal/auth"
	"github.com/your-org/dashboard/internal/config"
	"github.com/your-org/dashboard/internal/store"
)

// mockEngine is a minimal SyncEngine for testing.
type mockEngine struct {
	syncRunID int64
	syncErr   error
}

func (m *mockEngine) Sync(_ context.Context, _ string, _ *int64) (int64, error) {
	return m.syncRunID, m.syncErr
}
func (m *mockEngine) Discover(_ context.Context, _, _ string) (int64, error) { return 0, nil }
func (m *mockEngine) AutoTag(_ context.Context) error                        { return nil }

func newTestDeps(t *testing.T) (*Deps, *store.Store, string) {
	t.Helper()
	st, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	cfg := &config.Config{
		Auth: config.AuthConfig{JWTSecret: "test-secret"},
	}

	deps := &Deps{
		Store:  st,
		Config: cfg,
		Engine: &mockEngine{syncRunID: 1},
	}

	// Issue an edit-role token for authenticated requests.
	token, err := auth.IssueToken("admin", "edit", cfg.Auth.JWTSecret)
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}
	return deps, st, token
}

// TestTeamSprintMissingStartDate verifies that GET /teams/{id}/sprint returns
// start_date_missing: true when no sprint_meta exists, without panicking or 500.
func TestTeamSprintMissingStartDate(t *testing.T) {
	deps, st, token := newTestDeps(t)

	team, err := st.CreateTeam(context.Background(), "Team A")
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}

	router := NewRouter(deps)
	req := httptest.NewRequest(http.MethodGet, "/teams/"+fmt.Sprintf("%d", team.ID)+"/sprint", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — body: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		StartDateMissing bool `json:"start_date_missing"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !resp.StartDateMissing {
		t.Error("expected start_date_missing: true when no sprint meta exists")
	}
}

// TestPostSyncConflict verifies that POST /sync returns 409 when a sync run is
// already running for the same scope.
func TestPostSyncConflict(t *testing.T) {
	deps, st, token := newTestDeps(t)

	ctx := context.Background()

	// Create a running sync run.
	_, err := st.CreateSyncRun(ctx, sql.NullInt64{}, "org")
	if err != nil {
		t.Fatalf("CreateSyncRun: %v", err)
	}

	body, _ := json.Marshal(map[string]string{"scope": "org"})
	req := httptest.NewRequest(http.MethodPost, "/sync", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router := NewRouter(deps)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Errorf("expected 409 Conflict, got %d — body: %s", rec.Code, rec.Body.String())
	}
	// Verify error message.
	var errResp struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&errResp); err == nil {
		if errResp.Error != "sync already running for this scope" {
			t.Errorf("unexpected error message: %q", errResp.Error)
		}
	}
}

// TestFirstRunEndpointsReturn200 verifies that data endpoints return HTTP 200
// with empty/null fields before any sync has been completed.
func TestFirstRunEndpointsReturn200(t *testing.T) {
	deps, st, token := newTestDeps(t)

	team, err := st.CreateTeam(context.Background(), "Team A")
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}

	router := NewRouter(deps)

	endpoints := []string{
		fmt.Sprintf("/teams/%d/sprint", team.ID),
		fmt.Sprintf("/teams/%d/goals", team.ID),
		fmt.Sprintf("/teams/%d/workload", team.ID),
		fmt.Sprintf("/teams/%d/velocity", team.ID),
		fmt.Sprintf("/teams/%d/metrics", team.ID),
	}

	for _, ep := range endpoints {
		req := httptest.NewRequest(http.MethodGet, ep, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s: expected 200, got %d (body: %s)", ep, rec.Code, rec.Body.String())
		}
	}
}

