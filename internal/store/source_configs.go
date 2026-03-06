package store

import (
	"context"
	"database/sql"
	"fmt"
)

// UpsertSourceConfig inserts a source config if none exists for the given
// (catalogue_id, team_id, purpose) triple, or updates config_meta on conflict.
func (s *Store) UpsertSourceConfig(ctx context.Context, catalogueID int64, teamID sql.NullInt64, purpose string, configMeta sql.NullString) (*SourceConfig, error) {
	existing, err := s.findSourceConfig(ctx, catalogueID, teamID, purpose)
	if err == nil {
		// Update config_meta if it has changed.
		if existing.ConfigMeta != configMeta {
			if _, err2 := s.db.ExecContext(ctx,
				`UPDATE source_configs SET config_meta = ? WHERE id = ?`,
				configMeta, existing.ID,
			); err2 != nil {
				return nil, fmt.Errorf("update source config meta: %w", err2)
			}
			existing.ConfigMeta = configMeta
		}
		return existing, nil
	}
	if err != sql.ErrNoRows {
		return nil, err
	}

	res, err := s.db.ExecContext(ctx,
		`INSERT INTO source_configs (catalogue_id, team_id, purpose, config_meta) VALUES (?, ?, ?, ?)`,
		catalogueID, teamID, purpose, configMeta,
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
	const q = `SELECT id, catalogue_id, team_id, purpose, config_meta, created_at FROM source_configs ORDER BY id`
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
			`SELECT id, catalogue_id, team_id, purpose, config_meta, created_at FROM source_configs WHERE team_id = ? ORDER BY id`,
			teamID.Int64,
		)
	} else {
		rows, err = s.db.QueryContext(ctx,
			`SELECT id, catalogue_id, team_id, purpose, config_meta, created_at FROM source_configs WHERE team_id IS NULL ORDER BY id`,
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

// GetSourceConfigsByItemID returns all source configs for the given catalogue item id.
func (s *Store) GetSourceConfigsByItemID(ctx context.Context, catalogueID int64) ([]*SourceConfig, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, catalogue_id, team_id, purpose, config_meta, created_at FROM source_configs WHERE catalogue_id = ? ORDER BY id`,
		catalogueID,
	)
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

// FindCurrentPlanForTeam finds the source_config with purpose='current_plan' for the given team,
// or sql.ErrNoRows if none exists.
func (s *Store) FindCurrentPlanForTeam(ctx context.Context, teamID int64) (*SourceConfig, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, catalogue_id, team_id, purpose, config_meta, created_at FROM source_configs WHERE team_id = ? AND purpose = 'current_plan' LIMIT 1`,
		teamID,
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

func (s *Store) findSourceConfig(ctx context.Context, catalogueID int64, teamID sql.NullInt64, purpose string) (*SourceConfig, error) {
	var (
		rows *sql.Rows
		err  error
	)
	if teamID.Valid {
		rows, err = s.db.QueryContext(ctx,
			`SELECT id, catalogue_id, team_id, purpose, config_meta, created_at FROM source_configs WHERE catalogue_id=? AND team_id=? AND purpose=?`,
			catalogueID, teamID.Int64, purpose,
		)
	} else {
		rows, err = s.db.QueryContext(ctx,
			`SELECT id, catalogue_id, team_id, purpose, config_meta, created_at FROM source_configs WHERE catalogue_id=? AND team_id IS NULL AND purpose=?`,
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
		`SELECT id, catalogue_id, team_id, purpose, config_meta, created_at FROM source_configs WHERE id = ?`, id,
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
	if err := rows.Scan(&sc.ID, &sc.CatalogueID, &sc.TeamID, &sc.Purpose, &sc.ConfigMeta, &sc.CreatedAt); err != nil {
		return nil, err
	}
	return &sc, nil
}
