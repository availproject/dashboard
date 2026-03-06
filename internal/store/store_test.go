package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

// newTestStore creates a file-based SQLite store in a temp dir for testing.
// Using a temp file avoids `:memory:` connection-sharing issues with golang-migrate.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	s, err := New(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("newTestStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// ---- Users ----

func TestUserCRUD(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	// CreateUser
	u, err := s.CreateUser(ctx, "alice", "hash1", "edit")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if u.ID == 0 {
		t.Fatal("expected non-zero ID")
	}
	if u.Username != "alice" {
		t.Errorf("username: got %q, want %q", u.Username, "alice")
	}
	if u.Role != "edit" {
		t.Errorf("role: got %q, want %q", u.Role, "edit")
	}

	// GetUserByUsername
	u2, err := s.GetUserByUsername(ctx, "alice")
	if err != nil {
		t.Fatalf("GetUserByUsername: %v", err)
	}
	if u2.ID != u.ID {
		t.Errorf("id mismatch: got %d, want %d", u2.ID, u.ID)
	}

	// GetUserByID
	u3, err := s.GetUserByID(ctx, u.ID)
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}
	if u3.Username != "alice" {
		t.Errorf("username: got %q, want %q", u3.Username, "alice")
	}

	// UpdateUser
	if err := s.UpdateUser(ctx, u.ID, "newhash", "view"); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	u4, _ := s.GetUserByID(ctx, u.ID)
	if u4.PasswordHash != "newhash" {
		t.Errorf("password_hash: got %q, want %q", u4.PasswordHash, "newhash")
	}
	if u4.Role != "view" {
		t.Errorf("role: got %q, want %q", u4.Role, "view")
	}

	// ListUsers
	_, _ = s.CreateUser(ctx, "bob", "hash2", "view")
	users, err := s.ListUsers(ctx)
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(users) != 2 {
		t.Errorf("ListUsers: got %d users, want 2", len(users))
	}

	// DeleteUser
	if err := s.DeleteUser(ctx, u.ID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	_, err = s.GetUserByID(ctx, u.ID)
	if err != sql.ErrNoRows {
		t.Errorf("after delete: expected sql.ErrNoRows, got %v", err)
	}
}

func TestUpdateUser_NotFound(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	err := s.UpdateUser(ctx, 999, "hash", "view")
	if err != sql.ErrNoRows {
		t.Errorf("expected sql.ErrNoRows, got %v", err)
	}
}

// ---- Teams ----

func TestTeamCRUD(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	// CreateTeam
	team, err := s.CreateTeam(ctx, "Platform")
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	if team.ID == 0 {
		t.Fatal("expected non-zero team ID")
	}
	if team.Name != "Platform" {
		t.Errorf("name: got %q, want %q", team.Name, "Platform")
	}

	// UpdateTeam
	if err := s.UpdateTeam(ctx, team.ID, "Platform Team"); err != nil {
		t.Fatalf("UpdateTeam: %v", err)
	}

	// ListTeams
	_, _ = s.CreateTeam(ctx, "Growth")
	teams, err := s.ListTeams(ctx)
	if err != nil {
		t.Fatalf("ListTeams: %v", err)
	}
	if len(teams) != 2 {
		t.Errorf("ListTeams: got %d teams, want 2", len(teams))
	}

	// AddMember
	m, err := s.AddMember(ctx, team.ID, "Alice", sql.NullString{String: "alice-gh", Valid: true}, sql.NullString{}, sql.NullString{String: "engineer", Valid: true})
	if err != nil {
		t.Fatalf("AddMember: %v", err)
	}
	if m.ID == 0 {
		t.Fatal("expected non-zero member ID")
	}
	if m.TeamID != team.ID {
		t.Errorf("team_id: got %d, want %d", m.TeamID, team.ID)
	}
	if m.GithubLogin.String != "alice-gh" {
		t.Errorf("github_login: got %q, want %q", m.GithubLogin.String, "alice-gh")
	}

	// AddMember without optional fields
	m2, err := s.AddMember(ctx, team.ID, "Bob", sql.NullString{}, sql.NullString{}, sql.NullString{})
	if err != nil {
		t.Fatalf("AddMember (no optional): %v", err)
	}
	if m2.GithubLogin.Valid {
		t.Error("expected github_login to be NULL")
	}

	// UpdateMember
	if err := s.UpdateMember(ctx, m.ID, "Alice Smith", sql.NullString{String: "asmith", Valid: true}, sql.NullString{}, sql.NullString{String: "lead", Valid: true}); err != nil {
		t.Fatalf("UpdateMember: %v", err)
	}

	// GetTeamMembers
	members, err := s.GetTeamMembers(ctx, team.ID)
	if err != nil {
		t.Fatalf("GetTeamMembers: %v", err)
	}
	if len(members) != 2 {
		t.Errorf("GetTeamMembers: got %d members, want 2", len(members))
	}
	if members[0].Name != "Alice Smith" {
		t.Errorf("member name: got %q, want %q", members[0].Name, "Alice Smith")
	}

	// DeleteMember
	if err := s.DeleteMember(ctx, m.ID); err != nil {
		t.Fatalf("DeleteMember: %v", err)
	}
	members, _ = s.GetTeamMembers(ctx, team.ID)
	if len(members) != 1 {
		t.Errorf("after DeleteMember: got %d members, want 1", len(members))
	}

	// DeleteTeam (cascades to members)
	if err := s.DeleteTeam(ctx, team.ID); err != nil {
		t.Fatalf("DeleteTeam: %v", err)
	}
	teams, _ = s.ListTeams(ctx)
	if len(teams) != 1 {
		t.Errorf("after DeleteTeam: got %d teams, want 1", len(teams))
	}
}

func TestUpdateTeam_NotFound(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	err := s.UpdateTeam(ctx, 999, "nope")
	if err != sql.ErrNoRows {
		t.Errorf("expected sql.ErrNoRows, got %v", err)
	}
}

// ---- Source Catalogue ----

func TestCatalogueCRUD(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	// UpsertCatalogueItem (insert)
	item, err := s.UpsertCatalogueItem(ctx, "github_repo", "org/repo1", "Repo One",
		sql.NullString{String: "https://github.com/org/repo1", Valid: true},
		sql.NullString{})
	if err != nil {
		t.Fatalf("UpsertCatalogueItem: %v", err)
	}
	if item.ID == 0 {
		t.Fatal("expected non-zero ID")
	}
	if item.SourceType != "github_repo" {
		t.Errorf("source_type: got %q, want %q", item.SourceType, "github_repo")
	}
	if item.Status != "untagged" {
		t.Errorf("status default: got %q, want %q", item.Status, "untagged")
	}

	// UpsertCatalogueItem (update on conflict)
	item2, err := s.UpsertCatalogueItem(ctx, "github_repo", "org/repo1", "Repo One Updated",
		sql.NullString{String: "https://github.com/org/repo1", Valid: true},
		sql.NullString{})
	if err != nil {
		t.Fatalf("UpsertCatalogueItem (update): %v", err)
	}
	if item2.ID != item.ID {
		t.Errorf("upsert should return same ID: got %d, want %d", item2.ID, item.ID)
	}
	if item2.Title != "Repo One Updated" {
		t.Errorf("title after upsert: got %q, want %q", item2.Title, "Repo One Updated")
	}

	// GetCatalogueItem
	got, err := s.GetCatalogueItem(ctx, item.ID)
	if err != nil {
		t.Fatalf("GetCatalogueItem: %v", err)
	}
	if got.Title != "Repo One Updated" {
		t.Errorf("GetCatalogueItem title: got %q", got.Title)
	}

	// ListCatalogue
	_, _ = s.UpsertCatalogueItem(ctx, "notion_page", "page-abc", "Page ABC",
		sql.NullString{}, sql.NullString{})
	items, err := s.ListCatalogue(ctx)
	if err != nil {
		t.Fatalf("ListCatalogue: %v", err)
	}
	if len(items) != 2 {
		t.Errorf("ListCatalogue: got %d items, want 2", len(items))
	}

	// UpdateCatalogueStatus
	if err := s.UpdateCatalogueStatus(ctx, item.ID, "configured"); err != nil {
		t.Fatalf("UpdateCatalogueStatus: %v", err)
	}
	got2, _ := s.GetCatalogueItem(ctx, item.ID)
	if got2.Status != "configured" {
		t.Errorf("status after update: got %q, want %q", got2.Status, "configured")
	}

	// UpdateCatalogueAISuggestion
	if err := s.UpdateCatalogueAISuggestion(ctx, item.ID, "current_plan"); err != nil {
		t.Fatalf("UpdateCatalogueAISuggestion: %v", err)
	}
	got3, _ := s.GetCatalogueItem(ctx, item.ID)
	if !got3.AISuggestion.Valid || got3.AISuggestion.String != "current_plan" {
		t.Errorf("ai_suggestion after update: got %v", got3.AISuggestion)
	}
}

func TestUpdateCatalogueStatus_NotFound(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	err := s.UpdateCatalogueStatus(ctx, 999, "ignored")
	if err != sql.ErrNoRows {
		t.Errorf("expected sql.ErrNoRows, got %v", err)
	}
}

// ---- Source Configs ----

func TestSourceConfigCRUD(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	// Need a catalogue item and a team first.
	item, err := s.UpsertCatalogueItem(ctx, "notion_page", "page-1", "Sprint Plan",
		sql.NullString{}, sql.NullString{})
	if err != nil {
		t.Fatalf("UpsertCatalogueItem: %v", err)
	}
	team, err := s.CreateTeam(ctx, "Alpha")
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}

	teamID := sql.NullInt64{Int64: team.ID, Valid: true}

	// UpsertSourceConfig (insert)
	sc, err := s.UpsertSourceConfig(ctx, item.ID, teamID, "current_plan", sql.NullString{})
	if err != nil {
		t.Fatalf("UpsertSourceConfig: %v", err)
	}
	if sc.ID == 0 {
		t.Fatal("expected non-zero ID")
	}
	if sc.CatalogueID != item.ID {
		t.Errorf("catalogue_id: got %d, want %d", sc.CatalogueID, item.ID)
	}
	if sc.Purpose != "current_plan" {
		t.Errorf("purpose: got %q, want %q", sc.Purpose, "current_plan")
	}

	// UpsertSourceConfig (idempotent - returns existing)
	sc2, err := s.UpsertSourceConfig(ctx, item.ID, teamID, "current_plan", sql.NullString{})
	if err != nil {
		t.Fatalf("UpsertSourceConfig (idempotent): %v", err)
	}
	if sc2.ID != sc.ID {
		t.Errorf("idempotent upsert returned different ID: got %d, want %d", sc2.ID, sc.ID)
	}

	// UpsertSourceConfig (org-level, team_id IS NULL)
	orgConfig, err := s.UpsertSourceConfig(ctx, item.ID, sql.NullInt64{}, "org_goals", sql.NullString{})
	if err != nil {
		t.Fatalf("UpsertSourceConfig (org): %v", err)
	}
	if orgConfig.TeamID.Valid {
		t.Error("expected team_id to be NULL for org config")
	}

	// ListSourceConfigs
	configs, err := s.ListSourceConfigs(ctx)
	if err != nil {
		t.Fatalf("ListSourceConfigs: %v", err)
	}
	if len(configs) != 2 {
		t.Errorf("ListSourceConfigs: got %d configs, want 2", len(configs))
	}

	// GetSourceConfigsForScope (team)
	teamConfigs, err := s.GetSourceConfigsForScope(ctx, teamID)
	if err != nil {
		t.Fatalf("GetSourceConfigsForScope (team): %v", err)
	}
	if len(teamConfigs) != 1 {
		t.Errorf("GetSourceConfigsForScope (team): got %d, want 1", len(teamConfigs))
	}

	// GetSourceConfigsForScope (org)
	orgConfigs, err := s.GetSourceConfigsForScope(ctx, sql.NullInt64{})
	if err != nil {
		t.Fatalf("GetSourceConfigsForScope (org): %v", err)
	}
	if len(orgConfigs) != 1 {
		t.Errorf("GetSourceConfigsForScope (org): got %d, want 1", len(orgConfigs))
	}

	// DeleteSourceConfig
	if err := s.DeleteSourceConfig(ctx, sc.ID); err != nil {
		t.Fatalf("DeleteSourceConfig: %v", err)
	}
	configs, _ = s.ListSourceConfigs(ctx)
	if len(configs) != 1 {
		t.Errorf("after DeleteSourceConfig: got %d configs, want 1", len(configs))
	}
}

// ---- Annotations ----

func TestAnnotationCRUD(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	team, err := s.CreateTeam(ctx, "Beta")
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	teamID := sql.NullInt64{Int64: team.ID, Valid: true}

	// CreateAnnotation (item tier)
	a, err := s.CreateAnnotation(ctx, teamID, sql.NullString{String: "issue-42", Valid: true}, "item", "This is blocked")
	if err != nil {
		t.Fatalf("CreateAnnotation: %v", err)
	}
	if a.ID == 0 {
		t.Fatal("expected non-zero ID")
	}
	if a.Tier != "item" {
		t.Errorf("tier: got %q, want %q", a.Tier, "item")
	}
	if a.Archived != 0 {
		t.Errorf("archived default: got %d, want 0", a.Archived)
	}

	// CreateAnnotation (team tier)
	a2, err := s.CreateAnnotation(ctx, teamID, sql.NullString{}, "team", "Team is on vacation")
	if err != nil {
		t.Fatalf("CreateAnnotation (team): %v", err)
	}
	if a2.Tier != "team" {
		t.Errorf("tier: got %q, want %q", a2.Tier, "team")
	}
	if a2.ItemRef.Valid {
		t.Error("expected item_ref to be NULL")
	}

	// UpdateAnnotation
	if err := s.UpdateAnnotation(ctx, a.ID, "Updated content"); err != nil {
		t.Fatalf("UpdateAnnotation: %v", err)
	}

	// ListAnnotations (by team)
	anns, err := s.ListAnnotations(ctx, teamID)
	if err != nil {
		t.Fatalf("ListAnnotations: %v", err)
	}
	if len(anns) != 2 {
		t.Errorf("ListAnnotations: got %d, want 2", len(anns))
	}
	if anns[0].Content != "Updated content" {
		t.Errorf("content after update: got %q", anns[0].Content)
	}

	// ArchiveItemAnnotationsForPlan
	if err := s.ArchiveItemAnnotationsForPlan(ctx, team.ID); err != nil {
		t.Fatalf("ArchiveItemAnnotationsForPlan: %v", err)
	}
	anns2, _ := s.ListAnnotations(ctx, teamID)
	archived := 0
	for _, ann := range anns2 {
		if ann.Archived == 1 {
			archived++
		}
	}
	if archived != 1 {
		t.Errorf("expected 1 archived annotation (item tier), got %d", archived)
	}

	// DeleteAnnotation
	if err := s.DeleteAnnotation(ctx, a2.ID); err != nil {
		t.Fatalf("DeleteAnnotation: %v", err)
	}
	anns3, _ := s.ListAnnotations(ctx, teamID)
	if len(anns3) != 1 {
		t.Errorf("after DeleteAnnotation: got %d, want 1", len(anns3))
	}
}

// TestRolloverAnnotations verifies that ArchiveItemAnnotationsForPlan archives
// only item-tier annotations and leaves team-tier annotations untouched.
func TestRolloverAnnotations(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	team, err := s.CreateTeam(ctx, "RolloverTeam")
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	teamID := sql.NullInt64{Int64: team.ID, Valid: true}

	// Create one item-tier annotation.
	itemAnn, err := s.CreateAnnotation(ctx, teamID, sql.NullString{String: "issue-99", Valid: true}, "item", "blocked on dep")
	if err != nil {
		t.Fatalf("CreateAnnotation (item): %v", err)
	}

	// Create one team-tier annotation.
	teamAnn, err := s.CreateAnnotation(ctx, teamID, sql.NullString{}, "team", "team on vacation")
	if err != nil {
		t.Fatalf("CreateAnnotation (team): %v", err)
	}

	// Trigger rollover.
	if err := s.ArchiveItemAnnotationsForPlan(ctx, team.ID); err != nil {
		t.Fatalf("ArchiveItemAnnotationsForPlan: %v", err)
	}

	// Reload and verify item annotation is archived.
	anns, err := s.ListAnnotations(ctx, teamID)
	if err != nil {
		t.Fatalf("ListAnnotations: %v", err)
	}
	for _, a := range anns {
		switch a.ID {
		case itemAnn.ID:
			if a.Archived != 1 {
				t.Errorf("item annotation: want archived=1, got %d", a.Archived)
			}
		case teamAnn.ID:
			if a.Archived != 0 {
				t.Errorf("team annotation: want archived=0, got %d", a.Archived)
			}
		}
	}
}

func TestUpdateAnnotation_NotFound(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	err := s.UpdateAnnotation(ctx, 999, "nope")
	if err != sql.ErrNoRows {
		t.Errorf("expected sql.ErrNoRows, got %v", err)
	}
}

// ---- AI Cache ----

func TestAICacheCRUD(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	team, err := s.CreateTeam(ctx, "Gamma")
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	teamID := sql.NullInt64{Int64: team.ID, Valid: true}

	// GetCacheEntry (miss)
	_, err = s.GetCacheEntry(ctx, "hash1", "concerns", teamID)
	if err != sql.ErrNoRows {
		t.Errorf("expected sql.ErrNoRows on miss, got %v", err)
	}

	// SetCacheEntry (insert)
	entry, err := s.SetCacheEntry(ctx, "hash1", "concerns", teamID, `{"result":"ok"}`)
	if err != nil {
		t.Fatalf("SetCacheEntry: %v", err)
	}
	if entry.ID == 0 {
		t.Fatal("expected non-zero ID")
	}
	if entry.Output != `{"result":"ok"}` {
		t.Errorf("output: got %q", entry.Output)
	}

	// GetCacheEntry (hit)
	got, err := s.GetCacheEntry(ctx, "hash1", "concerns", teamID)
	if err != nil {
		t.Fatalf("GetCacheEntry: %v", err)
	}
	if got.ID != entry.ID {
		t.Errorf("id mismatch: got %d, want %d", got.ID, entry.ID)
	}

	// SetCacheEntry (update existing)
	updated, err := s.SetCacheEntry(ctx, "hash1", "concerns", teamID, `{"result":"updated"}`)
	if err != nil {
		t.Fatalf("SetCacheEntry (update): %v", err)
	}
	if updated.ID != entry.ID {
		t.Errorf("update changed ID: got %d, want %d", updated.ID, entry.ID)
	}
	if updated.Output != `{"result":"updated"}` {
		t.Errorf("updated output: got %q", updated.Output)
	}

	// SetCacheEntry (org-level, team_id IS NULL)
	orgEntry, err := s.SetCacheEntry(ctx, "hash2", "velocity", sql.NullInt64{}, `{"org":"data"}`)
	if err != nil {
		t.Fatalf("SetCacheEntry (org): %v", err)
	}
	if orgEntry.TeamID.Valid {
		t.Error("expected team_id to be NULL for org cache entry")
	}

	// PruneStaleCache (nothing old enough to prune)
	if err := s.PruneStaleCache(ctx, time.Hour); err != nil {
		t.Fatalf("PruneStaleCache: %v", err)
	}
	_, err = s.GetCacheEntry(ctx, "hash1", "concerns", teamID)
	if err != nil {
		t.Errorf("entry should still exist after prune: %v", err)
	}

	// PruneStaleCache with zero duration prunes everything
	if err := s.PruneStaleCache(ctx, 0); err != nil {
		t.Fatalf("PruneStaleCache (zero): %v", err)
	}
	_, err = s.GetCacheEntry(ctx, "hash1", "concerns", teamID)
	if err != sql.ErrNoRows {
		t.Errorf("expected sql.ErrNoRows after prune all, got %v", err)
	}
}

// ---- Sync Runs ----

func TestSyncRunCRUD(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	team, err := s.CreateTeam(ctx, "Delta")
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	teamID := sql.NullInt64{Int64: team.ID, Valid: true}

	// CreateSyncRun (team scope)
	run, err := s.CreateSyncRun(ctx, teamID, "team")
	if err != nil {
		t.Fatalf("CreateSyncRun: %v", err)
	}
	if run.ID == 0 {
		t.Fatal("expected non-zero ID")
	}
	if run.Status != "running" {
		t.Errorf("status default: got %q, want %q", run.Status, "running")
	}
	if run.FinishedAt.Valid {
		t.Error("expected finished_at to be NULL initially")
	}

	// CreateSyncRun (org scope)
	orgRun, err := s.CreateSyncRun(ctx, sql.NullInt64{}, "org")
	if err != nil {
		t.Fatalf("CreateSyncRun (org): %v", err)
	}
	if orgRun.TeamID.Valid {
		t.Error("expected team_id to be NULL for org sync run")
	}

	// GetSyncRun
	got, err := s.GetSyncRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetSyncRun: %v", err)
	}
	if got.Scope != "team" {
		t.Errorf("scope: got %q, want %q", got.Scope, "team")
	}

	// UpdateSyncRun (done)
	if err := s.UpdateSyncRun(ctx, run.ID, "done", sql.NullString{}); err != nil {
		t.Fatalf("UpdateSyncRun (done): %v", err)
	}
	got2, _ := s.GetSyncRun(ctx, run.ID)
	if got2.Status != "done" {
		t.Errorf("status after update: got %q, want %q", got2.Status, "done")
	}
	if !got2.FinishedAt.Valid {
		t.Error("expected finished_at to be set after update")
	}

	// UpdateSyncRun (error with message)
	if err := s.UpdateSyncRun(ctx, orgRun.ID, "error", sql.NullString{String: "connection refused", Valid: true}); err != nil {
		t.Fatalf("UpdateSyncRun (error): %v", err)
	}
	got3, _ := s.GetSyncRun(ctx, orgRun.ID)
	if got3.Status != "error" {
		t.Errorf("status: got %q, want %q", got3.Status, "error")
	}
	if !got3.Error.Valid || got3.Error.String != "connection refused" {
		t.Errorf("error field: got %v", got3.Error)
	}
}

func TestUpdateSyncRun_NotFound(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	err := s.UpdateSyncRun(ctx, 999, "done", sql.NullString{})
	if err != sql.ErrNoRows {
		t.Errorf("expected sql.ErrNoRows, got %v", err)
	}
}

// ---- Sprint Meta ----

func TestSprintMetaCRUD(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	team, err := s.CreateTeam(ctx, "Epsilon")
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}

	// GetSprintMeta (miss)
	_, err = s.GetSprintMeta(ctx, team.ID, "current")
	if err != sql.ErrNoRows {
		t.Errorf("expected sql.ErrNoRows on miss, got %v", err)
	}

	// UpsertSprintMeta (insert)
	sm, err := s.UpsertSprintMeta(ctx, team.ID, "current",
		sql.NullInt64{Int64: 42, Valid: true},
		sql.NullString{String: "2026-03-01", Valid: true},
		sql.NullString{String: "2026-03-14", Valid: true},
		sql.NullString{String: "raw content here", Valid: true},
	)
	if err != nil {
		t.Fatalf("UpsertSprintMeta: %v", err)
	}
	if sm.ID == 0 {
		t.Fatal("expected non-zero ID")
	}
	if sm.TeamID != team.ID {
		t.Errorf("team_id: got %d, want %d", sm.TeamID, team.ID)
	}
	if sm.PlanType != "current" {
		t.Errorf("plan_type: got %q, want %q", sm.PlanType, "current")
	}
	if !sm.SprintNumber.Valid || sm.SprintNumber.Int64 != 42 {
		t.Errorf("sprint_number: got %v", sm.SprintNumber)
	}

	// UpsertSprintMeta (update on conflict)
	sm2, err := s.UpsertSprintMeta(ctx, team.ID, "current",
		sql.NullInt64{Int64: 43, Valid: true},
		sql.NullString{String: "2026-03-15", Valid: true},
		sql.NullString{String: "2026-03-28", Valid: true},
		sql.NullString{String: "new raw content", Valid: true},
	)
	if err != nil {
		t.Fatalf("UpsertSprintMeta (update): %v", err)
	}
	if sm2.ID != sm.ID {
		t.Errorf("upsert should return same ID: got %d, want %d", sm2.ID, sm.ID)
	}
	if sm2.SprintNumber.Int64 != 43 {
		t.Errorf("sprint_number after update: got %d, want 43", sm2.SprintNumber.Int64)
	}

	// UpsertSprintMeta (next plan)
	next, err := s.UpsertSprintMeta(ctx, team.ID, "next",
		sql.NullInt64{},
		sql.NullString{},
		sql.NullString{},
		sql.NullString{String: "next sprint raw", Valid: true},
	)
	if err != nil {
		t.Fatalf("UpsertSprintMeta (next): %v", err)
	}
	if next.PlanType != "next" {
		t.Errorf("plan_type: got %q, want %q", next.PlanType, "next")
	}
	if next.SprintNumber.Valid {
		t.Error("expected sprint_number to be NULL")
	}

	// GetSprintMeta
	got, err := s.GetSprintMeta(ctx, team.ID, "current")
	if err != nil {
		t.Fatalf("GetSprintMeta: %v", err)
	}
	if got.ID != sm.ID {
		t.Errorf("id mismatch: got %d, want %d", got.ID, sm.ID)
	}
}

// ---- Refresh Tokens ----

func TestRefreshTokenCRUD(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	user, err := s.CreateUser(ctx, "zeta", "hash", "view")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	expiry := time.Now().Add(30 * 24 * time.Hour).UTC().Truncate(time.Second)

	// CreateRefreshToken
	tok, err := s.CreateRefreshToken(ctx, user.ID, "sha256hashvalue1", expiry)
	if err != nil {
		t.Fatalf("CreateRefreshToken: %v", err)
	}
	if tok.ID == 0 {
		t.Fatal("expected non-zero ID")
	}
	if tok.UserID != user.ID {
		t.Errorf("user_id: got %d, want %d", tok.UserID, user.ID)
	}
	if tok.TokenHash != "sha256hashvalue1" {
		t.Errorf("token_hash: got %q, want %q", tok.TokenHash, "sha256hashvalue1")
	}

	// GetRefreshTokenByHash
	got, err := s.GetRefreshTokenByHash(ctx, "sha256hashvalue1")
	if err != nil {
		t.Fatalf("GetRefreshTokenByHash: %v", err)
	}
	if got.ID != tok.ID {
		t.Errorf("id mismatch: got %d, want %d", got.ID, tok.ID)
	}

	// GetRefreshTokenByHash (miss)
	_, err = s.GetRefreshTokenByHash(ctx, "nonexistent")
	if err != sql.ErrNoRows {
		t.Errorf("expected sql.ErrNoRows, got %v", err)
	}

	// CreateRefreshToken (second token)
	tok2, err := s.CreateRefreshToken(ctx, user.ID, "sha256hashvalue2", expiry)
	if err != nil {
		t.Fatalf("CreateRefreshToken (second): %v", err)
	}

	// DeleteRefreshToken
	if err := s.DeleteRefreshToken(ctx, tok.ID); err != nil {
		t.Fatalf("DeleteRefreshToken: %v", err)
	}
	_, err = s.GetRefreshTokenByHash(ctx, "sha256hashvalue1")
	if err != sql.ErrNoRows {
		t.Errorf("after delete: expected sql.ErrNoRows, got %v", err)
	}

	// DeleteExpiredRefreshTokens (tok2 not expired yet - should survive)
	if err := s.DeleteExpiredRefreshTokens(ctx); err != nil {
		t.Fatalf("DeleteExpiredRefreshTokens: %v", err)
	}
	_, err = s.GetRefreshTokenByHash(ctx, "sha256hashvalue2")
	if err != nil {
		t.Errorf("non-expired token should still exist: %v", err)
	}

	// Create an already-expired token and prune it
	pastExpiry := time.Now().Add(-time.Hour)
	_, err = s.CreateRefreshToken(ctx, user.ID, "expiredtoken", pastExpiry)
	if err != nil {
		t.Fatalf("CreateRefreshToken (expired): %v", err)
	}
	if err := s.DeleteExpiredRefreshTokens(ctx); err != nil {
		t.Fatalf("DeleteExpiredRefreshTokens (expired): %v", err)
	}
	_, err = s.GetRefreshTokenByHash(ctx, "expiredtoken")
	if err != sql.ErrNoRows {
		t.Errorf("expired token should be deleted, got %v", err)
	}
	// tok2 still present
	_, err = s.GetRefreshTokenByHash(ctx, "sha256hashvalue2")
	if err != nil {
		t.Errorf("tok2 should still exist after prune: %v", err)
	}
	_ = tok2
}
