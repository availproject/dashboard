package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/your-org/dashboard/internal/auth"
)

func TestRequireAuth(t *testing.T) {
	secret := "test-secret"

	handler := RequireAuth(secret)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, _ := r.Context().Value(contextKeyUsername).(string)
		role, _ := r.Context().Value(contextKeyRole).(string)
		w.Header().Set("X-Username", username)
		w.Header().Set("X-Role", role)
		w.WriteHeader(http.StatusOK)
	}))

	t.Run("missing token returns 401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", rec.Code)
		}
	})

	t.Run("invalid token returns 401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer notavalidtoken")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", rec.Code)
		}
	})

	t.Run("valid token sets context values", func(t *testing.T) {
		token, err := auth.IssueToken("alice", "edit", secret)
		if err != nil {
			t.Fatalf("IssueToken: %v", err)
		}
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rec.Code)
		}
		if got := rec.Header().Get("X-Username"); got != "alice" {
			t.Errorf("X-Username = %q, want %q", got, "alice")
		}
		if got := rec.Header().Get("X-Role"); got != "edit" {
			t.Errorf("X-Role = %q, want %q", got, "edit")
		}
	})
}

func TestRequireRole(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

	withRole := func(role string, h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), contextKeyRole, role)
			h.ServeHTTP(w, r.WithContext(ctx))
		})
	}

	cases := []struct {
		name         string
		contextRole  string
		requiredRole string
		wantCode     int
	}{
		{"edit satisfies edit", "edit", "edit", http.StatusOK},
		{"edit satisfies view", "edit", "view", http.StatusOK},
		{"view satisfies view", "view", "view", http.StatusOK},
		{"view denied for edit", "view", "edit", http.StatusForbidden},
		{"empty role denied for edit", "", "edit", http.StatusForbidden},
		{"empty role denied for view", "", "view", http.StatusForbidden},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := withRole(tc.contextRole, RequireRole(tc.requiredRole)(ok))
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != tc.wantCode {
				t.Errorf("expected %d, got %d", tc.wantCode, rec.Code)
			}
		})
	}
}
