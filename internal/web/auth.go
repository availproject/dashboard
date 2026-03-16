package web

import (
	"net/http"
	"time"

	"github.com/your-org/dashboard/internal/auth"
)

type loginPageData struct {
	Error string
	Next  string
}

func (d *Deps) loginPage(w http.ResponseWriter, r *http.Request) {
	// If already authenticated, redirect to home.
	if tok, err := r.Cookie("_dash_tok"); err == nil {
		if _, verr := auth.ValidateToken(tok.Value, d.JWTSecret); verr == nil {
			http.Redirect(w, r, "/", http.StatusFound)
			return
		}
	}
	render(w, "login.html", loginPageData{Next: r.URL.Query().Get("next")})
}

func (d *Deps) loginPost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		render(w, "login.html", loginPageData{Error: "invalid form"})
		return
	}
	username := r.FormValue("username")
	password := r.FormValue("password")
	next := r.FormValue("next")
	if next == "" {
		next = "/"
	}

	c := &apiClient{baseURL: d.APIBase, httpClient: &http.Client{Timeout: 10 * time.Second}}
	var resp struct {
		Token        string `json:"token"`
		RefreshToken string `json:"refresh_token"`
	}
	err := c.postJSON("/auth/login", map[string]string{
		"username": username,
		"password": password,
	}, &resp)
	if err != nil {
		render(w, "login.html", loginPageData{Error: "Invalid username or password", Next: next})
		return
	}

	setAuthCookies(w, resp.Token, resp.RefreshToken)
	http.Redirect(w, r, next, http.StatusFound)
}

func (d *Deps) logout(w http.ResponseWriter, r *http.Request) {
	clearAuthCookies(w)
	http.Redirect(w, r, "/login", http.StatusFound)
}
