package web

import (
	"context"
	"net/http"
	"time"

	"github.com/your-org/dashboard/internal/auth"
)

type contextKey string

const (
	ctxUsername contextKey = "username"
	ctxRole     contextKey = "role"
	ctxToken    contextKey = "token"
)

func (d *Deps) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok, err := r.Cookie("_dash_tok")
		if err == nil {
			claims, verr := auth.ValidateToken(tok.Value, d.JWTSecret)
			if verr == nil {
				// Valid JWT – inject claims into context.
				ctx := context.WithValue(r.Context(), ctxUsername, claims.Sub)
				ctx = context.WithValue(ctx, ctxRole, claims.Role)
				ctx = context.WithValue(ctx, ctxToken, tok.Value)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
		}

		// Try refresh token.
		ref, err := r.Cookie("_dash_ref")
		if err != nil {
			http.Redirect(w, r, "/login?next="+r.URL.Path, http.StatusFound)
			return
		}

		var refreshResp struct {
			Token        string `json:"token"`
			RefreshToken string `json:"refresh_token"`
		}
		c := &apiClient{baseURL: d.APIBase, httpClient: &http.Client{Timeout: 10 * time.Second}}
		if err := c.postJSON("/auth/refresh", map[string]string{"refresh_token": ref.Value}, &refreshResp); err != nil {
			clearAuthCookies(w)
			http.Redirect(w, r, "/login?next="+r.URL.Path, http.StatusFound)
			return
		}

		setAuthCookies(w, refreshResp.Token, refreshResp.RefreshToken)

		claims, verr := auth.ValidateToken(refreshResp.Token, d.JWTSecret)
		if verr != nil {
			clearAuthCookies(w)
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}

		ctx := context.WithValue(r.Context(), ctxUsername, claims.Sub)
		ctx = context.WithValue(ctx, ctxRole, claims.Role)
		ctx = context.WithValue(ctx, ctxToken, refreshResp.Token)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func requireEditRole(w http.ResponseWriter, r *http.Request) bool {
	if role, _ := r.Context().Value(ctxRole).(string); role != "edit" {
		http.Error(w, "forbidden", http.StatusForbidden)
		return false
	}
	return true
}

func ctxUsername_(r *http.Request) string {
	v, _ := r.Context().Value(ctxUsername).(string)
	return v
}

func ctxRole_(r *http.Request) string {
	v, _ := r.Context().Value(ctxRole).(string)
	return v
}

func ctxToken_(r *http.Request) string {
	v, _ := r.Context().Value(ctxToken).(string)
	return v
}

func setAuthCookies(w http.ResponseWriter, token, refreshToken string) {
	http.SetCookie(w, &http.Cookie{
		Name:     "_dash_tok",
		Value:    token,
		Path:     "/",
		MaxAge:   3600,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     "_dash_ref",
		Value:    refreshToken,
		Path:     "/",
		MaxAge:   30 * 24 * 3600,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
}

func clearAuthCookies(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     "_dash_tok",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     "_dash_ref",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
}
