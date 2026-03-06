package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// CreateRefreshToken inserts a new refresh token and returns it.
func (s *Store) CreateRefreshToken(ctx context.Context, userID int64, tokenHash string, expiresAt time.Time) (*RefreshToken, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO refresh_tokens (user_id, token_hash, expires_at) VALUES (?, ?, ?)`,
		userID, tokenHash, expiresAt.UTC().Format("2006-01-02 15:04:05"),
	)
	if err != nil {
		return nil, fmt.Errorf("create refresh token: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("last insert id: %w", err)
	}
	return s.getRefreshTokenByID(ctx, id)
}

// GetRefreshTokenByHash returns the refresh token with the given hash, or sql.ErrNoRows.
func (s *Store) GetRefreshTokenByHash(ctx context.Context, tokenHash string) (*RefreshToken, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, user_id, token_hash, expires_at, created_at FROM refresh_tokens WHERE token_hash = ?`, tokenHash,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, sql.ErrNoRows
	}
	return scanRefreshToken(rows)
}

// DeleteRefreshToken removes the refresh token with the given id.
func (s *Store) DeleteRefreshToken(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM refresh_tokens WHERE id = ?`, id)
	return err
}

// DeleteExpiredRefreshTokens removes all refresh tokens that have passed their expiry time.
func (s *Store) DeleteExpiredRefreshTokens(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM refresh_tokens WHERE expires_at < CURRENT_TIMESTAMP`)
	return err
}

func (s *Store) getRefreshTokenByID(ctx context.Context, id int64) (*RefreshToken, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, user_id, token_hash, expires_at, created_at FROM refresh_tokens WHERE id = ?`, id,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, sql.ErrNoRows
	}
	return scanRefreshToken(rows)
}

func scanRefreshToken(rows *sql.Rows) (*RefreshToken, error) {
	var t RefreshToken
	if err := rows.Scan(&t.ID, &t.UserID, &t.TokenHash, &t.ExpiresAt, &t.CreatedAt); err != nil {
		return nil, err
	}
	return &t, nil
}
