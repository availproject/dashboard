package store

import (
	"context"
	"database/sql"
	"fmt"
)

// CreateUser inserts a new user and returns the created record.
func (s *Store) CreateUser(ctx context.Context, username, passwordHash, role string) (*User, error) {
	const q = `INSERT INTO users (username, password_hash, role) VALUES (?, ?, ?)`
	res, err := s.db.ExecContext(ctx, q, username, passwordHash, role)
	if err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("last insert id: %w", err)
	}
	return s.GetUserByID(ctx, id)
}

// GetUserByUsername returns the user with the given username, or sql.ErrNoRows.
func (s *Store) GetUserByUsername(ctx context.Context, username string) (*User, error) {
	const q = `SELECT id, username, password_hash, role, created_at FROM users WHERE username = ?`
	row := s.db.QueryRowContext(ctx, q, username)
	return scanUser(row)
}

// GetUserByID returns the user with the given ID, or sql.ErrNoRows.
func (s *Store) GetUserByID(ctx context.Context, id int64) (*User, error) {
	const q = `SELECT id, username, password_hash, role, created_at FROM users WHERE id = ?`
	row := s.db.QueryRowContext(ctx, q, id)
	return scanUser(row)
}

// UpdateUser updates the password_hash and role for the user with the given ID.
func (s *Store) UpdateUser(ctx context.Context, id int64, passwordHash, role string) error {
	const q = `UPDATE users SET password_hash = ?, role = ? WHERE id = ?`
	res, err := s.db.ExecContext(ctx, q, passwordHash, role, id)
	if err != nil {
		return fmt.Errorf("update user: %w", err)
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

// DeleteUser removes the user with the given ID.
func (s *Store) DeleteUser(ctx context.Context, id int64) error {
	const q = `DELETE FROM users WHERE id = ?`
	_, err := s.db.ExecContext(ctx, q, id)
	return err
}

// CountUsersByRole returns the number of users with the given role.
func (s *Store) CountUsersByRole(ctx context.Context, role string) (int, error) {
	const q = `SELECT COUNT(*) FROM users WHERE role = ?`
	var n int
	if err := s.db.QueryRowContext(ctx, q, role).Scan(&n); err != nil {
		return 0, fmt.Errorf("count users by role: %w", err)
	}
	return n, nil
}

// ListUsers returns all users ordered by id.
func (s *Store) ListUsers(ctx context.Context) ([]*User, error) {
	const q = `SELECT id, username, password_hash, role, created_at FROM users ORDER BY id`
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []*User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.CreatedAt); err != nil {
			return nil, err
		}
		users = append(users, &u)
	}
	return users, rows.Err()
}

func scanUser(row *sql.Row) (*User, error) {
	var u User
	if err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.CreatedAt); err != nil {
		return nil, err
	}
	return &u, nil
}
