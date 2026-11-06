package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/rojinebrahimi/langdeck/internal/auth"
	"github.com/rojinebrahimi/langdeck/internal/domain"
	"github.com/rojinebrahimi/langdeck/internal/server"
	"github.com/rojinebrahimi/langdeck/internal/store"
	"github.com/rojinebrahimi/langdeck/internal/util"
)

// EnsureLocalAdmin creates the initial admin account in "local" auth mode if it
// does not exist yet. It never overwrites an existing account's password.
// It reports whether the well-known default password is in use.
func EnsureLocalAdmin(ctx context.Context, st store.Store, username, password string, forceChange bool) (created bool, err error) {
	if _, err := st.GetUserBySubject(ctx, auth.LocalSubject(username)); err == nil {
		return false, nil
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return false, err
	}
	err = st.CreateUser(ctx, &domain.User{
		ID: util.NewID(), Subject: auth.LocalSubject(username), Name: username, Role: domain.RoleAdmin,
		PasswordHash: hash, MustChangePassword: forceChange, CreatedAt: time.Now().UTC(), Local: true,
	})
	if errors.Is(err, domain.ErrConflict) { // another replica won the race
		return false, nil
	}
	return err == nil, err
}

func (a *API) passwordLogin(w http.ResponseWriter, r *http.Request) {
	if a.Cfg.AuthMode != "local" {
		writeError(w, http.StatusNotFound, "password login is disabled")
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decode(r, &req); err != nil {
		a.fail(w, r, err)
		return
	}
	username := strings.TrimSpace(strings.ToLower(req.Username))
	u, err := a.Store.GetUserBySubject(r.Context(), auth.LocalSubject(username))
	hash := ""
	if err == nil {
		hash = u.PasswordHash
	}
	if !auth.VerifyPassword(hash, req.Password) || len(username) > 64 {
		a.Log.Warn("failed password login", "username", username, "ip", server.ClientIP(r.Context()))
		writeError(w, http.StatusUnauthorized, "invalid username or password")
		return
	}
	if err := a.Auth.StartSession(w, r, u, a.Cfg.SessionTTL, server.ClientIP(r.Context())); err != nil {
		a.fail(w, r, err)
		return
	}
	r = r.WithContext(auth.WithPrincipal(r.Context(), &auth.Principal{Kind: auth.KindUser, ID: u.ID, Name: u.Name}))
	a.audit(r, "auth.login", "user", u.ID, map[string]any{"method": "password"})
	writeJSON(w, 200, u)
}

// changePassword lets a local user set a new password. It is the only action
// (besides /me and logout) open to an account that must change its password.
func (a *API) changePassword(w http.ResponseWriter, r *http.Request) {
	p := auth.FromContext(r.Context())
	var req struct {
		Current string `json:"current_password"`
		New     string `json:"new_password"`
	}
	if err := decode(r, &req); err != nil {
		a.fail(w, r, err)
		return
	}
	u, err := a.Store.GetUser(r.Context(), p.ID)
	if err != nil || p.Kind != auth.KindUser || !u.Local {
		writeError(w, http.StatusBadRequest, "only local accounts have a password")
		return
	}
	if !auth.VerifyPassword(u.PasswordHash, req.Current) {
		writeError(w, http.StatusForbidden, "current password is wrong")
		return
	}
	if err := auth.ValidatePassword(req.New); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.New == req.Current || strings.EqualFold(req.New, u.Name) {
		writeError(w, http.StatusBadRequest, "choose a password different from the current one and your username")
		return
	}
	hash, err := auth.HashPassword(req.New)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	if err := a.Store.SetPassword(r.Context(), u.ID, hash, false); err != nil {
		a.fail(w, r, err)
		return
	}
	a.audit(r, "user.password_change", "user", u.ID, nil)
	w.WriteHeader(http.StatusNoContent)
}

// createLocalUser lets an admin add a username/password account. The new user
// must pick their own password at first sign-in.
func (a *API) createLocalUser(w http.ResponseWriter, r *http.Request) {
	if a.Cfg.AuthMode != "local" {
		writeError(w, http.StatusBadRequest, "users are managed by your identity provider in this auth mode")
		return
	}
	var req struct {
		Username string      `json:"username"`
		Role     domain.Role `json:"role"`
		Password string      `json:"password"`
	}
	if err := decode(r, &req); err != nil {
		a.fail(w, r, err)
		return
	}
	req.Username = strings.TrimSpace(strings.ToLower(req.Username))
	if !domain.ValidAgentName(req.Username) {
		writeError(w, http.StatusBadRequest, "username must be lowercase letters, digits, '-', '_' or '.'")
		return
	}
	if !req.Role.Valid() || req.Role == domain.RoleAgent {
		writeError(w, http.StatusBadRequest, "role must be viewer, operator or admin")
		return
	}
	if err := auth.ValidatePassword(req.Password); err != nil {
		writeError(w, http.StatusBadRequest, "initial "+err.Error())
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	u := &domain.User{ID: util.NewID(), Subject: auth.LocalSubject(req.Username), Name: req.Username, Role: req.Role,
		PasswordHash: hash, MustChangePassword: true, CreatedAt: a.Now(), Local: true}
	if err := a.Store.CreateUser(r.Context(), u); err != nil {
		a.fail(w, r, err)
		return
	}
	a.audit(r, "user.create", "user", u.ID, map[string]any{"username": u.Name, "role": u.Role})
	writeJSON(w, http.StatusCreated, u)
}

// passwordGate blocks everything except /me and the password change for local
// accounts still using an initial password (for example the default admin/admin).
func (a *API) passwordGate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := auth.FromContext(r.Context())
		if p != nil && p.MustChangePassword && r.URL.Path != "/api/v1/me" && r.URL.Path != "/api/v1/me/password" {
			writeError(w, http.StatusForbidden, "password change required")
			return
		}
		next.ServeHTTP(w, r)
	})
}
