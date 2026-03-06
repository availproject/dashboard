package store

import (
	"context"
	"database/sql"
	"fmt"
)

// CreateAnnotation inserts a new annotation and returns the resulting record.
func (s *Store) CreateAnnotation(ctx context.Context, teamID sql.NullInt64, itemRef sql.NullString, tier, content string) (*Annotation, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO annotations (team_id, item_ref, tier, content) VALUES (?, ?, ?, ?)`,
		teamID, itemRef, tier, content,
	)
	if err != nil {
		return nil, fmt.Errorf("create annotation: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("last insert id: %w", err)
	}
	return s.getAnnotationByID(ctx, id)
}

// UpdateAnnotation updates the content of the annotation with the given id.
func (s *Store) UpdateAnnotation(ctx context.Context, id int64, content string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE annotations SET content = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		content, id,
	)
	if err != nil {
		return fmt.Errorf("update annotation: %w", err)
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

// DeleteAnnotation removes the annotation with the given id.
func (s *Store) DeleteAnnotation(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM annotations WHERE id = ?`, id)
	return err
}

// ListAnnotations returns all non-archived annotations for the given team (or all
// org-level annotations when teamID.Valid is false), ordered by id.
func (s *Store) ListAnnotations(ctx context.Context, teamID sql.NullInt64) ([]*Annotation, error) {
	var (
		rows *sql.Rows
		err  error
	)
	if teamID.Valid {
		rows, err = s.db.QueryContext(ctx,
			`SELECT id, team_id, item_ref, tier, content, archived, created_at, updated_at FROM annotations WHERE team_id = ? ORDER BY id`,
			teamID.Int64,
		)
	} else {
		rows, err = s.db.QueryContext(ctx,
			`SELECT id, team_id, item_ref, tier, content, archived, created_at, updated_at FROM annotations WHERE team_id IS NULL ORDER BY id`,
		)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var annotations []*Annotation
	for rows.Next() {
		a, err := scanAnnotation(rows)
		if err != nil {
			return nil, err
		}
		annotations = append(annotations, a)
	}
	return annotations, rows.Err()
}

// ArchiveItemAnnotationsForPlan archives all item-tier annotations for the given team.
func (s *Store) ArchiveItemAnnotationsForPlan(ctx context.Context, teamID int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE annotations SET archived = 1, updated_at = CURRENT_TIMESTAMP WHERE team_id = ? AND tier = 'item' AND archived = 0`,
		teamID,
	)
	return err
}

func (s *Store) getAnnotationByID(ctx context.Context, id int64) (*Annotation, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, team_id, item_ref, tier, content, archived, created_at, updated_at FROM annotations WHERE id = ?`, id,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, sql.ErrNoRows
	}
	return scanAnnotation(rows)
}

func scanAnnotation(rows *sql.Rows) (*Annotation, error) {
	var a Annotation
	if err := rows.Scan(&a.ID, &a.TeamID, &a.ItemRef, &a.Tier, &a.Content, &a.Archived, &a.CreatedAt, &a.UpdatedAt); err != nil {
		return nil, err
	}
	return &a, nil
}
