package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// GetCacheEntry returns the AI cache entry for the given key, or sql.ErrNoRows.
func (s *Store) GetCacheEntry(ctx context.Context, inputHash, pipeline string, teamID sql.NullInt64) (*AICache, error) {
	return s.findCacheEntry(ctx, inputHash, pipeline, teamID)
}

// SetCacheEntry inserts or updates the AI cache entry for the given key.
func (s *Store) SetCacheEntry(ctx context.Context, inputHash, pipeline string, teamID sql.NullInt64, output string) (*AICache, error) {
	existing, err := s.findCacheEntry(ctx, inputHash, pipeline, teamID)
	if err == nil {
		// Update existing entry.
		_, err = s.db.ExecContext(ctx,
			`UPDATE ai_cache SET output = ? WHERE id = ?`,
			output, existing.ID,
		)
		if err != nil {
			return nil, fmt.Errorf("update ai cache: %w", err)
		}
		return s.getCacheByID(ctx, existing.ID)
	}
	if err != sql.ErrNoRows {
		return nil, err
	}

	res, err := s.db.ExecContext(ctx,
		`INSERT INTO ai_cache (input_hash, pipeline, team_id, output) VALUES (?, ?, ?, ?)`,
		inputHash, pipeline, teamID, output,
	)
	if err != nil {
		return nil, fmt.Errorf("insert ai cache: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("last insert id: %w", err)
	}
	return s.getCacheByID(ctx, id)
}

// PruneStaleCache deletes cache entries older than the given duration.
func (s *Store) PruneStaleCache(ctx context.Context, olderThan time.Duration) error {
	cutoff := time.Now().UTC().Add(-olderThan).Format("2006-01-02 15:04:05")
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM ai_cache WHERE created_at <= ?`,
		cutoff,
	)
	return err
}

func (s *Store) findCacheEntry(ctx context.Context, inputHash, pipeline string, teamID sql.NullInt64) (*AICache, error) {
	var (
		rows *sql.Rows
		err  error
	)
	if teamID.Valid {
		rows, err = s.db.QueryContext(ctx,
			`SELECT id, input_hash, pipeline, team_id, output, created_at FROM ai_cache WHERE input_hash = ? AND pipeline = ? AND team_id = ?`,
			inputHash, pipeline, teamID.Int64,
		)
	} else {
		rows, err = s.db.QueryContext(ctx,
			`SELECT id, input_hash, pipeline, team_id, output, created_at FROM ai_cache WHERE input_hash = ? AND pipeline = ? AND team_id IS NULL`,
			inputHash, pipeline,
		)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, sql.ErrNoRows
	}
	return scanAICache(rows)
}

func (s *Store) getCacheByID(ctx context.Context, id int64) (*AICache, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, input_hash, pipeline, team_id, output, created_at FROM ai_cache WHERE id = ?`, id,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, sql.ErrNoRows
	}
	return scanAICache(rows)
}

func scanAICache(rows *sql.Rows) (*AICache, error) {
	var c AICache
	if err := rows.Scan(&c.ID, &c.InputHash, &c.Pipeline, &c.TeamID, &c.Output, &c.CreatedAt); err != nil {
		return nil, err
	}
	return &c, nil
}
