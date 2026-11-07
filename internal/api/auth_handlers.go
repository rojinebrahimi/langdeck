package api

import (
	"net/http"
	"strings"

	"github.com/rojinebrahimi/langdeck/internal/auth"
	"github.com/rojinebrahimi/langdeck/internal/domain"
	"github.com/rojinebrahimi/langdeck/internal/server"
	"github.com/rojinebrahimi/langdeck/internal/util"
)

func (a *API) authInfo(w http.ResponseWriter, _ *http.Request) {
	info := map[string]any{"mode": a.Cfg.AuthMode, "demo": a.Cfg.DemoMode}
	if a.Cfg.AuthMode == "local" && a.Cfg.AdminPassword == "admin" {
		// The well-known default is configured; the UI shows a first-login hint.
		// It only works until changed when ForcePasswordChange is on.
		info["default_credentials"] = a.Cfg.AdminUsername + " / admin"
	}
	writeJSON(w, 200, info)
}

func (a *API) login(w http.ResponseWriter, r *http.Request) {
	if a.OIDC == nil {
		writeError(w, http.StatusNotFound, "OIDC login is not enabled")
		return
	}
	a.OIDC.Begin(w, r)
}

func (a *API) callback(w http.ResponseWriter, r *http.Request) {
	if a.OIDC == nil {
		writeError(w, http.StatusNotFound, "OIDC login is not enabled")
		return
	}
	claims, err := a.OIDC.Finish(w, r)
	if err != nil {
		a.Log.Warn("oidc login failed", "err", err, "ip", server.ClientIP(r.Context()))
		http.Redirect(w, r, "/login?error=login_failed", http.StatusFound)
		return
	}
	if !auth.EmailAllowed(claims.Email, a.Cfg.AllowedEmailDomains) {
		a.Log.Warn("login rejected by domain allow-list", "email", claims.Email)
		http.Redirect(w, r, "/login?error=not_allowed", http.StatusFound)
		return
	}

	u, err := a.Store.UpsertUserOnLogin(r.Context(), claims.Subject, claims.Email, claims.Name, domain.Role(a.Cfg.DefaultRole))
	if err != nil {
		a.fail(w, r, err)
		return
	}
	// Bootstrap admin emails only count when the IdP vouches for the address.
	bootstrap := a.Cfg.BootstrapAdmins
	if !claims.EmailVerified {
		bootstrap = nil
	}
	if role := auth.ResolveRole(claims.Email, claims.Groups, bootstrap, a.Cfg.AdminGroups, a.Cfg.OperatorGroups, a.Cfg.ViewerGroups); role != "" && role != u.Role {
		if err := a.Store.SetUserRole(r.Context(), u.ID, role); err != nil {
			a.fail(w, r, err)
			return
		}
		u.Role = role
	}
	if err := a.Auth.StartSession(w, r, u, a.Cfg.SessionTTL, server.ClientIP(r.Context())); err != nil {
		a.fail(w, r, err)
		return
	}
	r = r.WithContext(auth.WithPrincipal(r.Context(), &auth.Principal{Kind: auth.KindUser, ID: u.ID, Email: u.Email}))
	a.audit(r, "auth.login", "user", u.ID, map[string]any{"role": u.Role})
	http.Redirect(w, r, "/", http.StatusFound)
}

// devLogin signs in without an identity provider. It exists for local
// development only; config validation refuses dev mode in production.
func (a *API) devLogin(w http.ResponseWriter, r *http.Request) {
	if a.Cfg.AuthMode != "dev" {
		writeError(w, http.StatusNotFound, "dev login is disabled")
		return
	}
	if a.Cfg.DemoMode {
		a.demoLogin(w, r)
		return
	}
	var body struct {
		Email string `json:"email"`
		Name  string `json:"name"`
	}
	if err := decode(r, &body); err != nil {
		a.fail(w, r, err)
		return
	}
	body.Email = strings.TrimSpace(strings.ToLower(body.Email))
	if body.Email == "" || !strings.Contains(body.Email, "@") || len(body.Email) > 254 {
		writeError(w, http.StatusBadRequest, "a valid email is required")
		return
	}
	if body.Name == "" {
		body.Name = strings.Split(body.Email, "@")[0]
	}
	u, err := a.Store.UpsertUserOnLogin(r.Context(), "dev|"+body.Email, body.Email, body.Name, domain.RoleAdmin)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	if err := a.Auth.StartSession(w, r, u, a.Cfg.SessionTTL, server.ClientIP(r.Context())); err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, 200, u)
}

// demoLogin signs a public-demo visitor in under a random, anonymous identity.
// Nothing from the request is used: whatever a visitor types never becomes visible
// to other visitors, and the operator role keeps them out of users, keys and audit.
func (a *API) demoLogin(w http.ResponseWriter, r *http.Request) {
	tag := util.RandomToken(6)[:6]
	tag = strings.ToLower(strings.NewReplacer("-", "x", "_", "y").Replace(tag))
	u, err := a.Store.UpsertUserOnLogin(r.Context(), "demo|"+util.NewID(), "visitor-"+tag+"@demo.invalid",
		"Visitor "+tag, domain.RoleOperator)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	if err := a.Auth.StartSession(w, r, u, a.Cfg.SessionTTL, ""); err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, 200, u)
}

func (a *API) logout(w http.ResponseWriter, r *http.Request) {
	p, err := a.Auth.Authenticate(r)
	if err == nil && p.Kind == auth.KindUser {
		if !auth.CheckCSRF(r, p) {
			writeError(w, http.StatusForbidden, "missing or invalid CSRF token")
			return
		}
		r = r.WithContext(auth.WithPrincipal(r.Context(), p))
		a.audit(r, "auth.logout", "user", p.ID, nil)
	}
	a.Auth.EndSession(w, r, p)
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) me(w http.ResponseWriter, r *http.Request) {
	p := auth.FromContext(r.Context())
	writeJSON(w, 200, map[string]any{
		"kind": p.Kind, "id": p.ID, "name": p.Name, "email": p.Email, "role": p.Role,
		"csrf_token": p.CSRFToken, "auth_mode": a.Cfg.AuthMode, "must_change_password": p.MustChangePassword,
	})
}
