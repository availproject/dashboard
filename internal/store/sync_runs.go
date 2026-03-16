package store

import (
	"context"
	"database/sql"
	"fmt"
)

// MarkOrphanedRunsFailed marks any sync_runs still in status='running' as
// 'error'. This is called on startup to clean up runs that were interrupted
// by a server restart.
func (s *Store) MarkOrphanedRunsFailed(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE sync_runs SET status = 'error', error = 'interrupted by server restart', finished_at = CURRENT_TIMESTAMP WHERE status = 'running'`,
	)
	if err != nil {
		return 0, fmt.Errorf("mark orphaned runs failed: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// CreateSyncRun inserts a new sync run with status 'running' and returns it.
func (s *Store) CreateSyncRun(ctx context.Context, teamID sql.NullInt64, scope string) (*SyncRun, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO sync_runs (team_id, scope, status) VALUES (?, ?, 'running')`,
		teamID, scope,
	)
	if err != nil {
		return nil, fmt.Errorf("create sync run: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("last insert id: %w", err)
	}
	return s.GetSyncRun(ctx, id)
}

// UpdateSyncRun updates the status, error, and finished_at of the given sync run.
func (s *Store) UpdateSyncRun(ctx context.Context, id int64, status string, syncErr sql.NullString) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE sync_runs SET status = ?, error = ?, finished_at = CURRENT_TIMESTAMP WHERE id = ?`,
		status, syncErr, id,
	)
	if err != nil {
		return fmt.Errorf("update sync run: %w", err)
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

// SaveSyncRunTimings stores the timings JSON for the given sync run.
func (s *Store) SaveSyncRunTimings(ctx context.Context, id int64, timingsJSON string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE sync_runs SET timings = ? WHERE id = ?`, timingsJSON, id,
	)
	return err
}

// GetSyncRun returns the sync run with the given id, or sql.ErrNoRows.
func (s *Store) GetSyncRun(ctx context.Context, id int64) (*SyncRun, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, team_id, scope, status, error, timings, started_at, finished_at FROM sync_runs WHERE id = ?`, id,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, sql.ErrNoRows
	}
	return scanSyncRun(rows)
}

// GetRunningSyncRun returns the most recent running sync_run for the given scope
// and teamID, or sql.ErrNoRows if none exists.
func (s *Store) GetRunningSyncRun(ctx context.Context, scope string, teamID sql.NullInt64) (*SyncRun, error) {
	var (
		rows *sql.Rows
		err  error
	)
	const base = `SELECT id, team_id, scope, status, error, timings, started_at, finished_at FROM sync_runs WHERE scope = ? AND status = 'running'`
	if teamID.Valid {
		rows, err = s.db.QueryContext(ctx, base+` AND team_id = ? ORDER BY id DESC LIMIT 1`, scope, teamID.Int64)
	} else {
		rows, err = s.db.QueryContext(ctx, base+` AND team_id IS NULL ORDER BY id DESC LIMIT 1`, scope)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, sql.ErrNoRows
	}
	return scanSyncRun(rows)
}

// GetLastCompletedSyncRun returns the most recent sync_run with status='done'
// for the given scope and teamID, or sql.ErrNoRows if none exists.
func (s *Store) GetLastCompletedSyncRun(ctx context.Context, scope string, teamID sql.NullInt64) (*SyncRun, error) {
	const base = `SELECT id, team_id, scope, status, error, timings, started_at, finished_at FROM sync_runs WHERE scope = ? AND status = 'done'`
	var (
		rows *sql.Rows
		err  error
	)
	if teamID.Valid {
		rows, err = s.db.QueryContext(ctx, base+` AND team_id = ? ORDER BY id DESC LIMIT 1`, scope, teamID.Int64)
	} else {
		rows, err = s.db.QueryContext(ctx, base+` AND team_id IS NULL ORDER BY id DESC LIMIT 1`, scope)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, sql.ErrNoRows
	}
	return scanSyncRun(rows)
}

// ListSyncRuns returns the most recent sync runs ordered newest-first.
func (s *Store) ListSyncRuns(ctx context.Context, limit int) ([]*SyncRun, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, team_id, scope, status, error, timings, started_at, finished_at
		   FROM sync_runs ORDER BY id DESC LIMIT ?`, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var runs []*SyncRun
	for rows.Next() {
		r, err := scanSyncRun(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, r)
	}
	return runs, rows.Err()
}

func scanSyncRun(rows *sql.Rows) (*SyncRun, error) {
	var r SyncRun
	if err := rows.Scan(&r.ID, &r.TeamID, &r.Scope, &r.Status, &r.Error, &r.Timings, &r.StartedAt, &r.FinishedAt); err != nil {
		return nil, err
	}
	return &r, nil
}
