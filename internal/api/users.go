package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"portguard/internal/store"
)

// ---- user management (owner-only) ----

func (a *App) handleListUsers(w http.ResponseWriter, r *http.Request) {
	admins, err := a.St.ListAdmins()
	if err != nil {
		errJSON(w, err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, admins)
}

func (a *App) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	body.Username = strings.TrimSpace(body.Username)
	if len(body.Username) < 3 {
		errJSON(w, errors.New("username must be 3+ chars"), http.StatusUnprocessableEntity)
		return
	}
	if len(body.Password) < 8 {
		errJSON(w, errors.New("password must be 8+ chars"), http.StatusUnprocessableEntity)
		return
	}
	if !store.ValidRole(body.Role) {
		errJSON(w, errors.New("role must be owner, admin, operator or viewer"), http.StatusUnprocessableEntity)
		return
	}
	// operators cannot be created by non-owners — the route itself is
	// owner-gated, so this is just a shape check
	hash, err := hashPassword(body.Password)
	if err != nil {
		errJSON(w, err, 500)
		return
	}
	id, err := a.St.CreateAdminRole(body.Username, hash, body.Role)
	if err != nil {
		errJSON(w, errors.New("username already exists"), http.StatusConflict)
		return
	}
	a.St.Audit(actorFrom(r.Context()), "user.create", body.Username+" ("+body.Role+")", "ok")
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

func (a *App) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var body struct {
		Role *string `json:"role"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	target, err := a.St.GetAdminByID(id)
	if err != nil {
		errJSON(w, err, http.StatusNotFound)
		return
	}
	if body.Role != nil {
		if !store.ValidRole(*body.Role) {
			errJSON(w, errors.New("role must be owner, admin, operator or viewer"), http.StatusUnprocessableEntity)
			return
		}
		// guard: an owner can only be demoted when another owner exists
		if target.Role == "owner" && *body.Role != "owner" {
			admins, _ := a.St.ListAdmins()
			owners := 0
			for _, u := range admins {
				if u.Role == "owner" {
					owners++
				}
			}
			if owners <= 1 {
				errJSON(w, errors.New("cannot demote the last owner"), http.StatusUnprocessableEntity)
				return
			}
		}
		if err := a.St.SetAdminRole(id, *body.Role); err != nil {
			errJSON(w, err, 500)
			return
		}
	}
	a.St.Audit(actorFrom(r.Context()), "user.update", target.Username+" role → "+deref(body.Role), "ok")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func deref(s *string) string {
	if s == nil {
		return "?"
	}
	return *s
}

func (a *App) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	target, err := a.St.GetAdminByID(id)
	if err != nil {
		errJSON(w, err, http.StatusNotFound)
		return
	}
	if target.Role == "owner" {
		admins, _ := a.St.ListAdmins()
		owners := 0
		for _, u := range admins {
			if u.Role == "owner" {
				owners++
			}
		}
		if owners <= 1 {
			errJSON(w, errors.New("cannot delete the last owner"), http.StatusUnprocessableEntity)
			return
		}
	}
	if err := a.St.DeleteAdmin(id); err != nil {
		errJSON(w, err, 500)
		return
	}
	a.St.Audit(actorFrom(r.Context()), "user.delete", target.Username, "ok")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
