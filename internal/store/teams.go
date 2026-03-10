package store

import (
	"context"
	"database/sql"
	"fmt"
)

// CreateTeam inserts a new team and returns the created record.
func (s *Store) CreateTeam(ctx context.Context, name string) (*Team, error) {
	const q = `INSERT INTO teams (name) VALUES (?)`
	res, err := s.db.ExecContext(ctx, q, name)
	if err != nil {
		return nil, fmt.Errorf("create team: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("last insert id: %w", err)
	}
	var t Team
	row := s.db.QueryRowContext(ctx, `SELECT id, name, marketing_label, created_at FROM teams WHERE id = ?`, id)
	if err := row.Scan(&t.ID, &t.Name, &t.MarketingLabel, &t.CreatedAt); err != nil {
		return nil, err
	}
	return &t, nil
}

// UpdateTeam updates the name of the team with the given ID.
func (s *Store) UpdateTeam(ctx context.Context, id int64, name string) error {
	const q = `UPDATE teams SET name = ? WHERE id = ?`
	res, err := s.db.ExecContext(ctx, q, name, id)
	if err != nil {
		return fmt.Errorf("update team: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// DeleteTeam removes the team with the given ID.
func (s *Store) DeleteTeam(ctx context.Context, id int64) error {
	const q = `DELETE FROM teams WHERE id = ?`
	_, err := s.db.ExecContext(ctx, q, id)
	return err
}

// GetTeam returns the team with the given ID.
func (s *Store) GetTeam(ctx context.Context, id int64) (*Team, error) {
	var t Team
	row := s.db.QueryRowContext(ctx, `SELECT id, name, marketing_label, created_at FROM teams WHERE id = ?`, id)
	if err := row.Scan(&t.ID, &t.Name, &t.MarketingLabel, &t.CreatedAt); err != nil {
		return nil, err
	}
	return &t, nil
}

// ListTeams returns all teams ordered by id.
func (s *Store) ListTeams(ctx context.Context) ([]*Team, error) {
	const q = `SELECT id, name, marketing_label, created_at FROM teams ORDER BY id`
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var teams []*Team
	for rows.Next() {
		var t Team
		if err := rows.Scan(&t.ID, &t.Name, &t.MarketingLabel, &t.CreatedAt); err != nil {
			return nil, err
		}
		teams = append(teams, &t)
	}
	return teams, rows.Err()
}

// UpdateTeamMarketingLabel sets or clears the marketing_label for the given team.
func (s *Store) UpdateTeamMarketingLabel(ctx context.Context, id int64, label sql.NullString) error {
	const q = `UPDATE teams SET marketing_label = ? WHERE id = ?`
	res, err := s.db.ExecContext(ctx, q, label, id)
	if err != nil {
		return fmt.Errorf("update marketing label: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// AddMember inserts a new team member and returns the created record.
func (s *Store) AddMember(ctx context.Context, teamID int64, name string, githubLogin, notionUserID, role sql.NullString) (*TeamMember, error) {
	const q = `INSERT INTO team_members (team_id, name, github_login, notion_user_id, role) VALUES (?, ?, ?, ?, ?)`
	res, err := s.db.ExecContext(ctx, q, teamID, name, githubLogin, notionUserID, role)
	if err != nil {
		return nil, fmt.Errorf("add member: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("last insert id: %w", err)
	}
	return s.getMemberByID(ctx, id)
}

// UpdateMember updates a team member's name, github_login, notion_user_id, and role.
func (s *Store) UpdateMember(ctx context.Context, id int64, name string, githubLogin, notionUserID, role sql.NullString) error {
	const q = `UPDATE team_members SET name = ?, github_login = ?, notion_user_id = ?, role = ? WHERE id = ?`
	res, err := s.db.ExecContext(ctx, q, name, githubLogin, notionUserID, role, id)
	if err != nil {
		return fmt.Errorf("update member: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// DeleteMember removes the team member with the given ID.
func (s *Store) DeleteMember(ctx context.Context, id int64) error {
	const q = `DELETE FROM team_members WHERE id = ?`
	_, err := s.db.ExecContext(ctx, q, id)
	return err
}

// UpsertMemberByName adds a team member with the given name if no member with
// that name already exists on the team. It is a no-op otherwise.
func (s *Store) UpsertMemberByName(ctx context.Context, teamID int64, name string) error {
	var count int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM team_members WHERE team_id = ? AND name = ?`,
		teamID, name,
	).Scan(&count); err != nil || count > 0 {
		return err
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO team_members (team_id, name) VALUES (?, ?)`,
		teamID, name,
	)
	return err
}

// UpsertMemberByGithubLogin adds a team member with the given GitHub login if no
// member with that login already exists on the team. It is a no-op otherwise.
func (s *Store) UpsertMemberByGithubLogin(ctx context.Context, teamID int64, login, name string) error {
	var count int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM team_members WHERE team_id = ? AND github_login = ?`,
		teamID, login,
	).Scan(&count); err != nil || count > 0 {
		return err
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO team_members (team_id, name, github_login) VALUES (?, ?, ?)`,
		teamID, name, login,
	)
	return err
}

// GetTeamMembers returns all members of the given team ordered by id.
func (s *Store) GetTeamMembers(ctx context.Context, teamID int64) ([]*TeamMember, error) {
	const q = `SELECT id, team_id, name, github_login, role, notion_user_id, created_at FROM team_members WHERE team_id = ? ORDER BY id`
	rows, err := s.db.QueryContext(ctx, q, teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var members []*TeamMember
	for rows.Next() {
		m, err := scanMember(rows)
		if err != nil {
			return nil, err
		}
		members = append(members, m)
	}
	return members, rows.Err()
}

func (s *Store) getMemberByID(ctx context.Context, id int64) (*TeamMember, error) {
	const q = `SELECT id, team_id, name, github_login, role, notion_user_id, created_at FROM team_members WHERE id = ?`
	rows, err := s.db.QueryContext(ctx, q, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, sql.ErrNoRows
	}
	return scanMember(rows)
}

func scanMember(rows *sql.Rows) (*TeamMember, error) {
	var m TeamMember
	if err := rows.Scan(&m.ID, &m.TeamID, &m.Name, &m.GithubLogin, &m.Role, &m.NotionUserID, &m.CreatedAt); err != nil {
		return nil, err
	}
	return &m, nil
}
