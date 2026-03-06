package api

import (
	"database/sql"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/your-org/dashboard/internal/auth"
)

type userResponse struct {
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	Role      string `json:"role"`
	CreatedAt string `json:"created_at"`
}

// --- GET /config/users ---

func (d *Deps) handleListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := d.Store.ListUsers(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list users: "+err.Error())
		return
	}

	resp := make([]userResponse, 0, len(users))
	for _, u := range users {
		resp = append(resp, userResponse{
			ID:        u.ID,
			Username:  u.Username,
			Role:      u.Role,
			CreatedAt: u.CreatedAt.Format("2006-01-02T15:04:05Z"),
		})
	}
	writeJSON(w, http.StatusOK, resp)
}

// --- POST /config/users ---

type createUserRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

func (d *Deps) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var req createUserRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Username == "" || req.Password == "" || req.Role == "" {
		writeError(w, http.StatusBadRequest, "username, password, and role are required")
		return
	}
	if req.Role != "view" && req.Role != "edit" {
		writeError(w, http.StatusBadRequest, "role must be 'view' or 'edit'")
		return
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "hash password: "+err.Error())
		return
	}

	u, err := d.Store.CreateUser(r.Context(), req.Username, hash, req.Role)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "create user: "+err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, userResponse{
		ID:        u.ID,
		Username:  u.Username,
		Role:      u.Role,
		CreatedAt: u.CreatedAt.Format("2006-01-02T15:04:05Z"),
	})
}

// --- PUT /config/users/{id} ---

type updateUserRequest struct {
	Role     string `json:"role"`
	Password string `json:"password"`
}

func (d *Deps) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return
	}

	var req updateUserRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Role != "" && req.Role != "view" && req.Role != "edit" {
		writeError(w, http.StatusBadRequest, "role must be 'view' or 'edit'")
		return
	}

	ctx := r.Context()
	existing, err := d.Store.GetUserByID(ctx, id)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "get user: "+err.Error())
		return
	}

	role := existing.Role
	if req.Role != "" {
		role = req.Role
	}

	passwordHash := existing.PasswordHash
	if req.Password != "" {
		passwordHash, err = auth.HashPassword(req.Password)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "hash password: "+err.Error())
			return
		}
	}

	if err := d.Store.UpdateUser(ctx, id, passwordHash, role); err != nil {
		if err == sql.ErrNoRows {
			writeError(w, http.StatusNotFound, "user not found")
		} else {
			writeError(w, http.StatusInternalServerError, "update user: "+err.Error())
		}
		return
	}

	u, err := d.Store.GetUserByID(ctx, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "get user: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, userResponse{
		ID:        u.ID,
		Username:  u.Username,
		Role:      u.Role,
		CreatedAt: u.CreatedAt.Format("2006-01-02T15:04:05Z"),
	})
}

// --- DELETE /config/users/{id} ---

func (d *Deps) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return
	}

	ctx := r.Context()

	// Verify user exists and check role before deletion.
	target, err := d.Store.GetUserByID(ctx, id)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "get user: "+err.Error())
		return
	}

	// Refuse to delete the last edit-role user.
	if target.Role == "edit" {
		count, err := d.Store.CountUsersByRole(ctx, "edit")
		if err != nil {
			writeError(w, http.StatusInternalServerError, "count users: "+err.Error())
			return
		}
		if count <= 1 {
			writeError(w, http.StatusConflict, "cannot delete the last edit-role user")
			return
		}
	}

	if err := d.Store.DeleteUser(ctx, id); err != nil {
		writeError(w, http.StatusInternalServerError, "delete user: "+err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
