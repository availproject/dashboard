package auth

import (
	"context"
	"fmt"

	"github.com/your-org/dashboard/internal/config"
	"github.com/your-org/dashboard/internal/store"
)

// Bootstrap seeds the admin user if the users table is empty.
// The password hash is stored directly from cfg.Auth.AdminPasswordHash without re-hashing.
func Bootstrap(st *store.Store, cfg *config.Config) error {
	ctx := context.Background()
	users, err := st.ListUsers(ctx)
	if err != nil {
		return fmt.Errorf("bootstrap list users: %w", err)
	}
	if len(users) > 0 {
		return nil
	}
	_, err = st.CreateUser(ctx, cfg.Auth.AdminUsername, cfg.Auth.AdminPasswordHash, "edit")
	if err != nil {
		return fmt.Errorf("bootstrap create admin: %w", err)
	}
	return nil
}
