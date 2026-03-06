package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
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
	m, err := s.AddMember(ctx, team.ID, "Alice", sql.NullString{String: "alice-gh", Valid: true}, sql.NullString{String: "engineer", Valid: true})
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
	m2, err := s.AddMember(ctx, team.ID, "Bob", sql.NullString{}, sql.NullString{})
	if err != nil {
		t.Fatalf("AddMember (no optional): %v", err)
	}
	if m2.GithubLogin.Valid {
		t.Error("expected github_login to be NULL")
	}

	// UpdateMember
	if err := s.UpdateMember(ctx, m.ID, "Alice Smith", sql.NullString{String: "asmith", Valid: true}, sql.NullString{String: "lead", Valid: true}); err != nil {
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
