package api

// Auth & session handlers: first-run setup, login, me, password change.
// Split out of handlers.go (v2.12 refactor) — same package, same API.

import (
	"errors"
	"net/http"
	"strings"

)

func (a *App) handleSetupStatus(w http.ResponseWriter, r *http.Request) {
	n, err := a.St.AdminCount()
	if err != nil {
		errJSON(w, err, 500)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"setup_required": n == 0})
}

func (a *App) handleSetup(w http.ResponseWriter, r *http.Request) {
	n, err := a.St.AdminCount()
	if err != nil {
		errJSON(w, err, 500)
		return
	}
	if n > 0 {
		errJSON(w, errors.New("setup already completed"), http.StatusForbidden)
		return
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	if len(body.Username) < 3 || len(body.Password) < 8 {
		errJSON(w, errors.New("username must be 3+ chars and password 8+ chars"), http.StatusBadRequest)
		return
	}
	hash, err := hashPassword(body.Password)
	if err != nil {
		errJSON(w, err, 500)
		return
	}
	if _, err := a.St.CreateAdmin(strings.ToLower(strings.TrimSpace(body.Username)), hash); err != nil {
		errJSON(w, err, 500)
		return
	}
	a.St.Audit(body.Username, "setup", "initial admin created", "ok")
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *App) handleLogin(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if !a.Auth.Allow(ip) {
		errJSON(w, errors.New("too many failed attempts, try again in a minute"), http.StatusTooManyRequests)
		return
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	admin, err := a.St.GetAdminByUsername(strings.ToLower(strings.TrimSpace(body.Username)))
	if err != nil {
		a.Auth.Fail(ip)
		errJSON(w, errors.New("invalid credentials"), http.StatusUnauthorized)
		return
	}
	if !checkPassword(admin.PasswordHash, body.Password) {
		a.Auth.Fail(ip)
		errJSON(w, errors.New("invalid credentials"), http.StatusUnauthorized)
		return
	}
	token, err := a.Auth.IssueRole(admin.Username, admin.ID, admin.Role)
	if err != nil {
		errJSON(w, err, 500)
		return
	}
	a.St.TouchAdminLogin(admin.ID)
	a.St.Audit(admin.Username, "login", "logged in from "+ip, "ok")
	writeJSON(w, http.StatusOK, map[string]any{"token": token, "username": admin.Username, "role": admin.Role})
}

func (a *App) handleMe(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"username": actorFrom(r.Context()), "role": roleFrom(r.Context())})
}

// handleEvents authenticates via ?token= (EventSource) or the Authorization header.
func (a *App) handleEvents(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
			token = strings.TrimPrefix(h, "Bearer ")
		}
	}
	if _, err := a.Auth.Parse(token); err != nil {
		unauthorized(w)
		return
	}
	a.Broker.Handler(w, r)
}

func (a *App) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r.Context())
	var body struct {
		Old string `json:"old"`
		New string `json:"new"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	admin, err := a.St.GetAdminByUsername(actor)
	if err != nil {
		errJSON(w, err, 500)
		return
	}
	if !checkPassword(admin.PasswordHash, body.Old) {
		errJSON(w, errors.New("current password is wrong"), http.StatusBadRequest)
		return
	}
	if len(body.New) < 8 {
		errJSON(w, errors.New("new password must be 8+ chars"), http.StatusBadRequest)
		return
	}
	hash, err := hashPassword(body.New)
	if err != nil {
		errJSON(w, err, 500)
		return
	}
	if err := a.St.SetAdminPassword(admin.ID, hash); err != nil {
		errJSON(w, err, 500)
		return
	}
	a.St.Audit(actor, "password", "password changed", "ok")
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

