package api

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"net/http"
	"time"

	"github.com/your-org/dashboard/internal/auth"
)

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type authResponse struct {
	Token        string `json:"token"`
	RefreshToken string `json:"refresh_token"`
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

func (d *Deps) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	ctx := context.Background()
	user, err := d.Store.GetUserByUsername(ctx, req.Username)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	if !auth.CheckPassword(user.PasswordHash, req.Password) {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	token, err := auth.IssueToken(user.Username, user.Role, d.Config.Auth.JWTSecret)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to issue token")
		return
	}

	rawRefresh, refreshHash, err := auth.IssueRefreshToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to issue refresh token")
		return
	}

	expiresAt := time.Now().Add(30 * 24 * time.Hour)
	if _, err := d.Store.CreateRefreshToken(ctx, user.ID, refreshHash, expiresAt); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to store refresh token")
		return
	}

	writeJSON(w, http.StatusOK, authResponse{Token: token, RefreshToken: rawRefresh})
}

func (d *Deps) handleRefresh(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	sum := sha256.Sum256([]byte(req.RefreshToken))
	tokenHash := hex.EncodeToString(sum[:])

	ctx := context.Background()
	stored, err := d.Store.GetRefreshTokenByHash(ctx, tokenHash)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusUnauthorized, "invalid refresh token")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "token lookup failed")
		return
	}

	if time.Now().After(stored.ExpiresAt) {
		_ = d.Store.DeleteRefreshToken(ctx, stored.ID)
		writeError(w, http.StatusUnauthorized, "refresh token expired")
		return
	}

	user, err := d.Store.GetUserByID(ctx, stored.UserID)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "user not found")
		return
	}

	// Rotate: delete old token
	if err := d.Store.DeleteRefreshToken(ctx, stored.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to rotate refresh token")
		return
	}

	token, err := auth.IssueToken(user.Username, user.Role, d.Config.Auth.JWTSecret)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to issue token")
		return
	}

	rawRefresh, refreshHash, err := auth.IssueRefreshToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to issue refresh token")
		return
	}

	expiresAt := time.Now().Add(30 * 24 * time.Hour)
	if _, err := d.Store.CreateRefreshToken(ctx, user.ID, refreshHash, expiresAt); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to store refresh token")
		return
	}

	writeJSON(w, http.StatusOK, authResponse{Token: token, RefreshToken: rawRefresh})
}
