// Package api implements LangDeck's HTTP API: the management API used by the
// UI and automation, and the agent API used by the SDK.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/rojinebrahimi/langdeck/internal/auth"
	"github.com/rojinebrahimi/langdeck/internal/config"
	"github.com/rojinebrahimi/langdeck/internal/domain"
	"github.com/rojinebrahimi/langdeck/internal/events"
	"github.com/rojinebrahimi/langdeck/internal/server"
	"github.com/rojinebrahimi/langdeck/internal/store"
)

// API wires handlers to their dependencies.
type API struct {
	Store  store.Store
	Broker events.Broker
	Auth   *auth.Authenticator
	OIDC   *auth.OIDC // nil unless AuthMode is "oidc"
	Cfg    *config.Config
	Log    *slog.Logger
	UI     http.Handler // optional single-page app
	Now    func() time.Time

	// PollInterval is how often long-polls re-check the store when no
	// notification arrives. Tests shorten it.
	PollInterval time.Duration

	loginLimiter *server.RateLimiter
	apiLimiter   *server.RateLimiter
}

// Handler builds the full router.
func (a *API) Handler(ctx context.Context) http.Handler {
	if a.Now == nil {
		a.Now = func() time.Time { return time.Now().UTC() }
	}
	if a.PollInterval == 0 {
		a.PollInterval = 2 * time.Second
	}
	a.loginLimiter = server.NewRateLimiter(a.Cfg.LoginRateLimit, 10)
	a.apiLimiter = server.NewRateLimiter(a.Cfg.APIRateLimit, int(a.Cfg.APIRateLimit*2)+10)
	go a.loginLimiter.Janitor(ctx)
	go a.apiLimiter.Janitor(ctx)

	r := chi.NewRouter()
	r.Use(server.WithRequestID, server.RealIP(a.Cfg.TrustProxyHeaders), server.Observe(a.Log), server.Recover(a.Log),
		server.SecurityHeaders(a.Cfg.SecureCookies()), server.CORS(a.Cfg.CORSOrigins), server.LimitBody(a.Cfg.MaxBodyBytes))

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, 200, map[string]string{"status": "ok"}) })
	r.Get("/readyz", a.ready)

	r.Route("/auth", func(r chi.Router) {
		r.Use(server.LimitByIP(a.loginLimiter))
		r.Get("/login", a.login)
		r.Get("/callback", a.callback)
		r.Post("/dev-login", a.devLogin)
		r.Post("/password-login", a.passwordLogin)
		r.Post("/logout", a.logout)
	})

	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/auth/info", a.authInfo)

		// Agent API: SDKs only.
		r.Route("/agent", func(r chi.Router) {
			r.Use(a.authenticate, a.requireRole(domain.RoleAgent))
			r.Post("/register", a.agentRegister)
			r.Post("/heartbeat", a.agentHeartbeat)
			r.Post("/events", a.agentEvents)
			r.Get("/commands", a.agentPollCommands)
			r.Post("/commands/{id}/ack", a.agentAckCommand)
			r.Post("/interrupts", a.agentCreateInterrupt)
			r.Get("/interrupts/{id}", a.agentWaitInterrupt)
		})

		// Management API: humans (session) and automation (API keys with human roles).
		r.Group(func(r chi.Router) {
			r.Use(a.authenticate, a.requireRole(domain.RoleViewer), a.csrf, a.passwordGate)

			r.Get("/me", a.me)
			r.Post("/me/password", a.changePassword)
			r.Get("/stats", a.stats)
			r.Get("/stream", a.stream)
			r.Get("/agents", a.listAgents)
			r.Get("/agents/{id}", a.getAgent)
			r.Get("/agents/{id}/configs", a.listConfigs)
			r.Get("/agents/{id}/commands", a.listCommands)
			r.Get("/runs", a.listRuns)
			r.Get("/runs/{id}", a.getRun)
			r.Get("/runs/{id}/events", a.listEvents)
			r.Get("/interrupts", a.listInterrupts)

			r.Group(func(r chi.Router) {
				r.Use(a.requireRole(domain.RoleOperator))
				r.Post("/agents/{id}/pause", a.pauseAgent)
				r.Post("/agents/{id}/resume", a.resumeAgent)
				r.Post("/agents/{id}/runs", a.startRun)
				r.Post("/agents/{id}/configs", a.createConfig)
				r.Post("/runs/{id}/cancel", a.cancelRun)
				r.Post("/interrupts/{id}/resolve", a.resolveInterrupt)
			})

			r.Group(func(r chi.Router) {
				r.Use(a.requireRole(domain.RoleAdmin))
				r.Delete("/agents/{id}", a.deleteAgent)
				r.Get("/api-keys", a.listKeys)
				r.Post("/api-keys", a.createKey)
				r.Delete("/api-keys/{id}", a.revokeKey)
				r.Get("/users", a.listUsers)
				r.Post("/users", a.createLocalUser)
				r.Patch("/users/{id}", a.updateUser)
				r.Get("/audit", a.listAudit)
			})
		})
	})

	if a.UI != nil {
		r.Handle("/*", a.UI)
	}
	return r
}

func (a *API) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := a.Store.Ping(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
		return
	}
	writeJSON(w, 200, map[string]string{"status": "ready"})
}

// ---- middleware ----

func (a *API) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, err := a.Auth.Authenticate(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		if !a.apiLimiter.Allow(string(p.Kind) + ":" + p.ID) {
			w.Header().Set("Retry-After", "1")
			writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
			return
		}
		next.ServeHTTP(w, r.WithContext(auth.WithPrincipal(r.Context(), p)))
	})
}

func (a *API) requireRole(min domain.Role) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if p := auth.FromContext(r.Context()); p == nil || !p.Role.AtLeast(min) {
				writeError(w, http.StatusForbidden, "insufficient role")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func (a *API) csrf(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !auth.CheckCSRF(r, auth.FromContext(r.Context())) {
			writeError(w, http.StatusForbidden, "missing or invalid CSRF token")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ---- helpers ----

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// fail maps domain errors to status codes. Unknown errors are logged and
// reported generically so internals never leak to clients.
func (a *API) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		writeError(w, http.StatusNotFound, "not found")
	case errors.Is(err, domain.ErrConflict):
		writeError(w, http.StatusConflict, "conflict")
	case errors.Is(err, domain.ErrInvalid):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, domain.ErrForbidden):
		writeError(w, http.StatusForbidden, "forbidden")
	case errors.Is(err, domain.ErrUnauthorized):
		writeError(w, http.StatusUnauthorized, "unauthorized")
	default:
		a.Log.Error("request failed", "err", err, "path", r.URL.Path, "request_id", server.RequestID(r.Context()))
		writeError(w, http.StatusInternalServerError, "internal error")
	}
}

func invalid(msg string) error { return errors.Join(domain.ErrInvalid, errors.New(msg)) }

func decode(r *http.Request, v any) error {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			return invalid("request body too large")
		}
		return invalid("invalid JSON body")
	}
	return nil
}

// audit appends an audit entry. Failures are logged but never fail the request:
// the action already happened.
func (a *API) audit(r *http.Request, action, targetType, targetID string, details any) {
	p := auth.FromContext(r.Context())
	actor := "anonymous"
	if p != nil {
		actor = p.Actor()
	}
	var raw json.RawMessage
	if details != nil {
		raw, _ = json.Marshal(details)
	}
	e := &domain.AuditEntry{Actor: actor, Action: action, TargetType: targetType, TargetID: targetID,
		Details: raw, IP: server.ClientIP(r.Context()), CreatedAt: a.Now()}
	if err := a.Store.AppendAudit(context.WithoutCancel(r.Context()), e); err != nil {
		a.Log.Error("audit write failed", "err", err, "action", action)
	}
}

func (a *API) publish(r *http.Request, e events.Event) {
	a.Broker.Publish(context.WithoutCancel(r.Context()), e)
}
