// Package auth authenticates requests and enforces RBAC.
//
// Two credential types exist:
//   - Browser sessions (OIDC login, or dev login locally): an opaque random cookie.
//     The store keeps only its SHA-256, so a database leak cannot be replayed.
//     State-changing requests also need a CSRF token bound to the session.
//   - API keys (agents and automation): "ldk_" + 256 bits of randomness, sent as a
//     Bearer token. Only the SHA-256 is stored. High-entropy secrets do not need a
//     slow hash such as bcrypt, and a fast hash keeps per-request auth cheap.
package auth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/rojinebrahimi/langdeck/internal/domain"
	"github.com/rojinebrahimi/langdeck/internal/store"
	"github.com/rojinebrahimi/langdeck/internal/util"
)

// KeyPrefix marks LangDeck API keys, which makes leaked keys easy to detect with secret scanners.
const KeyPrefix = "ldk_"

// PrincipalKind tells how a request was authenticated.
type PrincipalKind string

const (
	KindUser   PrincipalKind = "user"
	KindAPIKey PrincipalKind = "api_key"
)

// Principal is the authenticated caller.
type Principal struct {
	Kind      PrincipalKind `json:"kind"`
	ID        string        `json:"id"`
	Name      string        `json:"name"`
	Email     string        `json:"email,omitempty"`
	Role      domain.Role   `json:"role"`
	AgentName string        `json:"agent_name,omitempty"` // API keys bound to one agent
	SessionID string        `json:"-"`
	CSRFToken string        `json:"-"`
	// MustChangePassword limits a local account to changing its password.
	MustChangePassword bool `json:"must_change_password"`
}

// Actor is the string recorded in audit entries.
func (p *Principal) Actor() string {
	if p.Kind == KindUser {
		if p.Email != "" {
			return "user:" + p.Email
		}
		if p.Name != "" {
			return "user:" + p.Name
		}
		return "user:" + p.ID
	}
	return "key:" + p.Name
}

type ctxKey struct{}

// WithPrincipal stores p in ctx.
func WithPrincipal(ctx context.Context, p *Principal) context.Context {
	return context.WithValue(ctx, ctxKey{}, p)
}

// FromContext returns the principal, or nil for anonymous requests.
func FromContext(ctx context.Context) *Principal {
	p, _ := ctx.Value(ctxKey{}).(*Principal)
	return p
}

// HashSecret returns the hex SHA-256 of a secret.
func HashSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

// NewAPIKey generates a key. The plaintext is returned once and never stored.
func NewAPIKey(name string, role domain.Role, agentName, createdBy string, expires *time.Time) (string, *domain.APIKey) {
	secret := KeyPrefix + util.RandomToken(32)
	return secret, &domain.APIKey{
		ID: util.NewID(), Name: name, Prefix: secret[:12], Hash: HashSecret(secret), Role: role,
		AgentName: agentName, CreatedBy: createdBy, CreatedAt: time.Now().UTC(), ExpiresAt: expires,
	}
}

// Authenticator resolves credentials into principals.
type Authenticator struct {
	Store         store.Store
	SecureCookies bool
	Now           func() time.Time
}

// SessionCookieName uses the __Host- prefix over HTTPS, which forbids Domain
// attributes and forces Secure + Path=/ (cookie-tossing protection).
func (a *Authenticator) SessionCookieName() string {
	if a.SecureCookies {
		return "__Host-langdeck_session"
	}
	return "langdeck_session"
}

var errNoCredentials = errors.New("no credentials")

// Authenticate inspects the Authorization header first, then the session cookie.
func (a *Authenticator) Authenticate(r *http.Request) (*Principal, error) {
	if h := r.Header.Get("Authorization"); h != "" {
		token, ok := strings.CutPrefix(h, "Bearer ")
		if !ok || !strings.HasPrefix(token, KeyPrefix) {
			return nil, domain.ErrUnauthorized
		}
		return a.authenticateKey(r.Context(), token)
	}
	if c, err := r.Cookie(a.SessionCookieName()); err == nil && c.Value != "" {
		return a.authenticateSession(r.Context(), c.Value)
	}
	return nil, errNoCredentials
}

func (a *Authenticator) authenticateKey(ctx context.Context, token string) (*Principal, error) {
	k, err := a.Store.GetAPIKeyByHash(ctx, HashSecret(token))
	if err != nil {
		return nil, domain.ErrUnauthorized
	}
	now := a.Now()
	if !k.Active(now) {
		return nil, domain.ErrUnauthorized
	}
	// Recording every use would turn each request into a write; once a minute is enough.
	if k.LastUsedAt == nil || now.Sub(*k.LastUsedAt) > time.Minute {
		_ = a.Store.TouchAPIKey(ctx, k.ID, now)
	}
	return &Principal{Kind: KindAPIKey, ID: k.ID, Name: k.Name, Role: k.Role, AgentName: k.AgentName}, nil
}

func (a *Authenticator) authenticateSession(ctx context.Context, raw string) (*Principal, error) {
	s, err := a.Store.GetSession(ctx, HashSecret(raw))
	if err != nil {
		return nil, domain.ErrUnauthorized
	}
	if a.Now().After(s.ExpiresAt) {
		_ = a.Store.DeleteSession(ctx, s.ID)
		return nil, domain.ErrUnauthorized
	}
	u, err := a.Store.GetUser(ctx, s.UserID)
	if err != nil {
		return nil, domain.ErrUnauthorized
	}
	return &Principal{Kind: KindUser, ID: u.ID, Name: u.Name, Email: u.Email, Role: u.Role,
		SessionID: s.ID, CSRFToken: s.CSRFToken, MustChangePassword: u.MustChangePassword}, nil
}

// StartSession creates a session for u and sets the cookie.
func (a *Authenticator) StartSession(w http.ResponseWriter, r *http.Request, u *domain.User, ttl time.Duration, ip string) error {
	raw := util.RandomToken(32)
	now := a.Now()
	s := &domain.Session{
		ID: HashSecret(raw), UserID: u.ID, CSRFToken: util.RandomToken(24), CreatedAt: now,
		ExpiresAt: now.Add(ttl), UserAgent: truncate(r.UserAgent(), 256), IP: ip,
	}
	if err := a.Store.CreateSession(r.Context(), s); err != nil {
		return err
	}
	// Secure follows the public URL scheme: it must be off for plain-http local development.
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // G124: Secure is set whenever LANGDECK_PUBLIC_URL is https
		Name: a.SessionCookieName(), Value: raw, Path: "/", Expires: s.ExpiresAt,
		HttpOnly: true, Secure: a.SecureCookies, SameSite: http.SameSiteLaxMode,
	})
	return nil
}

// EndSession deletes the session and clears the cookie.
func (a *Authenticator) EndSession(w http.ResponseWriter, r *http.Request, p *Principal) {
	if p != nil && p.SessionID != "" {
		_ = a.Store.DeleteSession(r.Context(), p.SessionID)
	}
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // G124: see StartSession
		Name: a.SessionCookieName(), Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: a.SecureCookies, SameSite: http.SameSiteLaxMode,
	})
}

// CSRFHeader carries the per-session CSRF token on unsafe requests.
const CSRFHeader = "X-CSRF-Token"

// CheckCSRF enforces the synchronizer token for cookie-authenticated unsafe requests.
// Bearer-authenticated requests are not exposed to CSRF and skip the check.
func CheckCSRF(r *http.Request, p *Principal) bool {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}
	if p.Kind != KindUser {
		return true
	}
	got := r.Header.Get(CSRFHeader)
	return got != "" && subtle.ConstantTimeCompare([]byte(got), []byte(p.CSRFToken)) == 1
}

// ResolveRole maps IdP groups and bootstrap emails to a role.
// It returns "" when nothing matches, meaning "keep the stored role".
func ResolveRole(email string, groups []string, bootstrapAdmins, adminGroups, operatorGroups, viewerGroups []string) domain.Role {
	for _, e := range bootstrapAdmins {
		if strings.EqualFold(e, email) {
			return domain.RoleAdmin
		}
	}
	has := func(set []string) bool {
		for _, g := range groups {
			for _, s := range set {
				if g == s {
					return true
				}
			}
		}
		return false
	}
	switch {
	case has(adminGroups):
		return domain.RoleAdmin
	case has(operatorGroups):
		return domain.RoleOperator
	case has(viewerGroups):
		return domain.RoleViewer
	}
	return ""
}

// EmailAllowed applies the optional domain allow-list.
func EmailAllowed(email string, domains []string) bool {
	if len(domains) == 0 {
		return true
	}
	at := strings.LastIndexByte(email, '@')
	if at < 0 {
		return false
	}
	d := strings.ToLower(email[at+1:])
	for _, allowed := range domains {
		if strings.EqualFold(d, allowed) {
			return true
		}
	}
	return false
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
