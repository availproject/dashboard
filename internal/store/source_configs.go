package store

import (
	"context"
	"database/sql"
	"fmt"
)

// UpsertSourceConfig inserts a source config if none exists for the given
// (catalogue_id, team_id, purpose) triple, or returns the existing one.
func (s *Store) UpsertSourceConfig(ctx context.Context, catalogueID int64, teamID sql.NullInt64, purpose string) (*SourceConfig, error) {
	// Check for existing row first (handles NULL team_id correctly).
	existing, err := s.findSourceConfig(ctx, catalogueID, teamID, purpose)
	if err == nil {
		return existing, nil
	}
	if err != sql.ErrNoRows {
		return nil, err
	}

	res, err := s.db.ExecContext(ctx,
		`INSERT INTO source_configs (catalogue_id, team_id, purpose) VALUES (?, ?, ?)`,
		catalogueID, teamID, purpose,
	)
	if err != nil {
		return nil, fmt.Errorf("upsert source config: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("last insert id: %w", err)
	}
	return s.getSourceConfigByID(ctx, id)
}

// ListSourceConfigs returns all source configs ordered by id.
func (s *Store) ListSourceConfigs(ctx context.Context) ([]*SourceConfig, error) {
	const q = `SELECT id, catalogue_id, team_id, purpose, created_at FROM source_configs ORDER BY id`
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var configs []*SourceConfig
	for rows.Next() {
		sc, err := scanSourceConfig(rows)
		if err != nil {
			return nil, err
		}
		configs = append(configs, sc)
	}
	return configs, rows.Err()
}

// DeleteSourceConfig removes the source config with the given id.
func (s *Store) DeleteSourceConfig(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM source_configs WHERE id = ?`, id)
	return err
}

// GetSourceConfigsForScope returns configs for a given team (teamID.Valid=true)
// or org-level configs (teamID.Valid=false, i.e. team_id IS NULL).
func (s *Store) GetSourceConfigsForScope(ctx context.Context, teamID sql.NullInt64) ([]*SourceConfig, error) {
	var (
		rows *sql.Rows
		err  error
	)
	if teamID.Valid {
		rows, err = s.db.QueryContext(ctx,
			`SELECT id, catalogue_id, team_id, purpose, created_at FROM source_configs WHERE team_id = ? ORDER BY id`,
			teamID.Int64,
		)
	} else {
		rows, err = s.db.QueryContext(ctx,
			`SELECT id, catalogue_id, team_id, purpose, created_at FROM source_configs WHERE team_id IS NULL ORDER BY id`,
		)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var configs []*SourceConfig
	for rows.Next() {
		sc, err := scanSourceConfig(rows)
		if err != nil {
			return nil, err
		}
		configs = append(configs, sc)
	}
	return configs, rows.Err()
}

func (s *Store) findSourceConfig(ctx context.Context, catalogueID int64, teamID sql.NullInt64, purpose string) (*SourceConfig, error) {
	var (
		rows *sql.Rows
		err  error
	)
	if teamID.Valid {
		rows, err = s.db.QueryContext(ctx,
			`SELECT id, catalogue_id, team_id, purpose, created_at FROM source_configs WHERE catalogue_id=? AND team_id=? AND purpose=?`,
			catalogueID, teamID.Int64, purpose,
		)
	} else {
		rows, err = s.db.QueryContext(ctx,
			`SELECT id, catalogue_id, team_id, purpose, created_at FROM source_configs WHERE catalogue_id=? AND team_id IS NULL AND purpose=?`,
			catalogueID, purpose,
		)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, sql.ErrNoRows
	}
	return scanSourceConfig(rows)
}

func (s *Store) getSourceConfigByID(ctx context.Context, id int64) (*SourceConfig, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, catalogue_id, team_id, purpose, created_at FROM source_configs WHERE id = ?`, id,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, sql.ErrNoRows
	}
	return scanSourceConfig(rows)
}

func scanSourceConfig(rows *sql.Rows) (*SourceConfig, error) {
	var sc SourceConfig
	if err := rows.Scan(&sc.ID, &sc.CatalogueID, &sc.TeamID, &sc.Purpose, &sc.CreatedAt); err != nil {
		return nil, err
	}
	return &sc, nil
}
