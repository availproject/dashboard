package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// UpsertSnapshot inserts or replaces a JSON snapshot for the given team and type.
func (s *Store) UpsertSnapshot(ctx context.Context, teamID int64, snapshotType, data string) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO team_snapshots (team_id, snapshot_type, data, updated_at)
VALUES (?, ?, ?, datetime('now'))
ON CONFLICT(team_id, snapshot_type) DO UPDATE SET
    data       = excluded.data,
    updated_at = excluded.updated_at`,
		teamID, snapshotType, data,
	)
	if err != nil {
		return fmt.Errorf("upsert snapshot: %w", err)
	}
	return nil
}

// GetSnapshot returns the JSON data and update time for the given team snapshot,
// or sql.ErrNoRows if none exists.
func (s *Store) GetSnapshot(ctx context.Context, teamID int64, snapshotType string) (data string, updatedAt time.Time, err error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT data, updated_at FROM team_snapshots WHERE team_id = ? AND snapshot_type = ?`,
		teamID, snapshotType,
	)
	var updStr string
	if err = row.Scan(&data, &updStr); err != nil {
		if err == sql.ErrNoRows {
			return "", time.Time{}, sql.ErrNoRows
		}
		return "", time.Time{}, fmt.Errorf("get snapshot: %w", err)
	}
	updatedAt, _ = time.Parse("2006-01-02T15:04:05Z", updStr)
	if updatedAt.IsZero() {
		updatedAt, _ = time.Parse("2006-01-02 15:04:05", updStr)
	}
	return data, updatedAt, nil
}
