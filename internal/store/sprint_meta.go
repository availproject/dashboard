package store

import (
	"context"
	"database/sql"
	"fmt"
)

// UpsertSprintMeta inserts or updates sprint metadata for a team and plan type.
// Returns the resulting record.
func (s *Store) UpsertSprintMeta(ctx context.Context, teamID int64, planType string, sprintNumber sql.NullInt64, startDate, endDate, rawContent sql.NullString) (*SprintMeta, error) {
	const q = `
INSERT INTO sprint_meta (team_id, plan_type, sprint_number, start_date, end_date, raw_content)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(team_id, plan_type) DO UPDATE SET
  sprint_number = excluded.sprint_number,
  start_date    = excluded.start_date,
  end_date      = excluded.end_date,
  raw_content   = excluded.raw_content,
  updated_at    = CURRENT_TIMESTAMP`
	_, err := s.db.ExecContext(ctx, q, teamID, planType, sprintNumber, startDate, endDate, rawContent)
	if err != nil {
		return nil, fmt.Errorf("upsert sprint meta: %w", err)
	}
	return s.GetSprintMeta(ctx, teamID, planType)
}

// GetSprintMeta returns the sprint metadata for the given team and plan type, or sql.ErrNoRows.
func (s *Store) GetSprintMeta(ctx context.Context, teamID int64, planType string) (*SprintMeta, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, team_id, plan_type, sprint_number, start_date, end_date, raw_content, created_at, updated_at FROM sprint_meta WHERE team_id = ? AND plan_type = ?`,
		teamID, planType,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, sql.ErrNoRows
	}
	return scanSprintMeta(rows)
}

func scanSprintMeta(rows *sql.Rows) (*SprintMeta, error) {
	var m SprintMeta
	if err := rows.Scan(&m.ID, &m.TeamID, &m.PlanType, &m.SprintNumber, &m.StartDate, &m.EndDate, &m.RawContent, &m.CreatedAt, &m.UpdatedAt); err != nil {
		return nil, err
	}
	return &m, nil
}
