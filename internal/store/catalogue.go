package store

import (
	"context"
	"database/sql"
	"fmt"
)

// UpsertCatalogueItem inserts a new catalogue item or updates it on conflict
// (source_type, external_id). parentID is set on insert; on conflict it is
// only filled in if currently NULL (preserves an existing parent assignment).
// Returns the resulting record.
func (s *Store) UpsertCatalogueItem(ctx context.Context, sourceType, externalID, title string, url, sourceMeta sql.NullString, parentID sql.NullInt64) (*SourceCatalogue, error) {
	const q = `
INSERT INTO source_catalogue (source_type, external_id, title, url, source_meta, parent_id)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(source_type, external_id) DO UPDATE SET
  title       = excluded.title,
  url         = excluded.url,
  source_meta = excluded.source_meta,
  parent_id   = COALESCE(source_catalogue.parent_id, excluded.parent_id),
  updated_at  = CURRENT_TIMESTAMP`
	_, err := s.db.ExecContext(ctx, q, sourceType, externalID, title, url, sourceMeta, parentID)
	if err != nil {
		return nil, fmt.Errorf("upsert catalogue item: %w", err)
	}
	return s.getCatalogueByKey(ctx, sourceType, externalID)
}

const catalogueColumns = `id, source_type, external_id, title, url, source_meta, parent_id, ai_suggestion, status, created_at, updated_at, raw_content, notion_last_edited`

// ListCatalogue returns all catalogue items ordered by id.
func (s *Store) ListCatalogue(ctx context.Context) ([]*SourceCatalogue, error) {
	q := `SELECT ` + catalogueColumns + ` FROM source_catalogue ORDER BY id`
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []*SourceCatalogue
	for rows.Next() {
		sc, err := scanCatalogue(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, sc)
	}
	return items, rows.Err()
}

// GetCatalogueItem returns the catalogue item with the given id, or sql.ErrNoRows.
func (s *Store) GetCatalogueItem(ctx context.Context, id int64) (*SourceCatalogue, error) {
	q := `SELECT ` + catalogueColumns + ` FROM source_catalogue WHERE id = ?`
	rows, err := s.db.QueryContext(ctx, q, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, sql.ErrNoRows
	}
	return scanCatalogue(rows)
}

// UpdateCatalogueStatus sets the status of the given catalogue item.
func (s *Store) UpdateCatalogueStatus(ctx context.Context, id int64, status string) error {
	const q = `UPDATE source_catalogue SET status = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`
	res, err := s.db.ExecContext(ctx, q, status, id)
	if err != nil {
		return fmt.Errorf("update catalogue status: %w", err)
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

// UpdateCatalogueAISuggestion sets the ai_suggestion of the given catalogue item.
func (s *Store) UpdateCatalogueAISuggestion(ctx context.Context, id int64, suggestion string) error {
	const q = `UPDATE source_catalogue SET ai_suggestion = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`
	res, err := s.db.ExecContext(ctx, q, suggestion, id)
	if err != nil {
		return fmt.Errorf("update catalogue ai suggestion: %w", err)
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

// TouchCatalogueItem updates updated_at to CURRENT_TIMESTAMP for the given catalogue item.
func (s *Store) TouchCatalogueItem(ctx context.Context, id int64) error {
	const q = `UPDATE source_catalogue SET updated_at = CURRENT_TIMESTAMP WHERE id = ?`
	_, err := s.db.ExecContext(ctx, q, id)
	return err
}

// GetCatalogueItemByURL returns the first catalogue item with the given URL, or sql.ErrNoRows.
func (s *Store) GetCatalogueItemByURL(ctx context.Context, url string) (*SourceCatalogue, error) {
	q := `SELECT ` + catalogueColumns + ` FROM source_catalogue WHERE url = ? LIMIT 1`
	rows, err := s.db.QueryContext(ctx, q, url)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, sql.ErrNoRows
	}
	return scanCatalogue(rows)
}

func (s *Store) getCatalogueByKey(ctx context.Context, sourceType, externalID string) (*SourceCatalogue, error) {
	q := `SELECT ` + catalogueColumns + ` FROM source_catalogue WHERE source_type = ? AND external_id = ?`
	rows, err := s.db.QueryContext(ctx, q, sourceType, externalID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, sql.ErrNoRows
	}
	return scanCatalogue(rows)
}

func scanCatalogue(rows *sql.Rows) (*SourceCatalogue, error) {
	var sc SourceCatalogue
	if err := rows.Scan(&sc.ID, &sc.SourceType, &sc.ExternalID, &sc.Title, &sc.URL, &sc.SourceMeta, &sc.ParentID, &sc.AISuggestion, &sc.Status, &sc.CreatedAt, &sc.UpdatedAt, &sc.RawContent, &sc.NotionLastEdited); err != nil {
		return nil, err
	}
	return &sc, nil
}

// UpdateCatalogueContent stores fetched content and the Notion last-edited timestamp.
func (s *Store) UpdateCatalogueContent(ctx context.Context, id int64, content, notionLastEdited string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE source_catalogue SET raw_content = ?, notion_last_edited = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		content, notionLastEdited, id,
	)
	return err
}
