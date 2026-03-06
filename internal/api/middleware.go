package api

import (
	"context"
	"net/http"
	"strings"

	"github.com/your-org/dashboard/internal/auth"
)

type contextKey string

const (
	contextKeyUsername contextKey = "username"
	contextKeyRole     contextKey = "role"
)

// RequireAuth returns middleware that validates Authorization: Bearer <JWT>.
// On success it sets username and role in the request context.
// Returns 401 on missing or invalid token.
func RequireAuth(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := r.Header.Get("Authorization")
			if !strings.HasPrefix(header, "Bearer ") {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}
			tokenStr := strings.TrimPrefix(header, "Bearer ")
			claims, err := auth.ValidateToken(tokenStr, secret)
			if err != nil {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}
			ctx := context.WithValue(r.Context(), contextKeyUsername, claims.Sub)
			ctx = context.WithValue(ctx, contextKeyRole, claims.Role)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireRole returns middleware that checks the role stored in context.
// 'edit' satisfies any role requirement; 'view' only satisfies 'view'.
// Returns 403 if the role is insufficient.
func RequireRole(role string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			contextRole, _ := r.Context().Value(contextKeyRole).(string)
			if !roleAllowed(contextRole, role) {
				http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// roleAllowed reports whether contextRole satisfies the required role.
func roleAllowed(contextRole, required string) bool {
	if contextRole == "edit" {
		return true
	}
	return contextRole == required
}
