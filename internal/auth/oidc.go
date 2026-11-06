package auth

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/rojinebrahimi/langdeck/internal/util"
)

// OIDC implements the OAuth 2.0 Authorization Code flow with PKCE (S256),
// state and nonce, against any OpenID Connect provider (Keycloak, Okta, Auth0,
// Google, Azure AD/Entra ID, Dex, Authentik, GitLab, ...).
type OIDC struct {
	provider    *oidc.Provider
	verifier    *oidc.IDTokenVerifier
	oauth       *oauth2.Config
	groupsClaim string
	secure      bool
}

// Claims are the identity fields LangDeck uses.
type Claims struct {
	Subject       string
	Email         string
	EmailVerified bool
	Name          string
	Groups        []string
}

// NewOIDC discovers the provider's endpoints from its issuer URL.
func NewOIDC(ctx context.Context, issuer, clientID, clientSecret, publicURL string, scopes []string, groupsClaim string, secure bool) (*OIDC, error) {
	p, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, fmt.Errorf("oidc discovery: %w", err)
	}
	return &OIDC{
		provider: p,
		verifier: p.Verifier(&oidc.Config{ClientID: clientID}),
		oauth: &oauth2.Config{
			ClientID: clientID, ClientSecret: clientSecret, Endpoint: p.Endpoint(),
			RedirectURL: publicURL + "/auth/callback", Scopes: scopes,
		},
		groupsClaim: groupsClaim,
		secure:      secure,
	}, nil
}

const flowCookie = "langdeck_oauth"

// flowCookieTTL bounds how long a login may take at the IdP.
const flowCookieTTL = 10 * time.Minute

// Begin redirects the browser to the provider. state, nonce and the PKCE
// verifier live in short-lived HttpOnly cookies scoped to the callback path.
func (o *OIDC) Begin(w http.ResponseWriter, r *http.Request) {
	state, nonce, verifier := util.RandomToken(24), util.RandomToken(24), oauth2.GenerateVerifier()
	for name, v := range map[string]string{"state": state, "nonce": nonce, "pkce": verifier} {
		http.SetCookie(w, &http.Cookie{ //nolint:gosec // G124: Secure is set whenever the public URL is https
			Name: flowCookie + "_" + name, Value: v, Path: "/auth/callback", MaxAge: int(flowCookieTTL.Seconds()),
			HttpOnly: true, Secure: o.secure, SameSite: http.SameSiteLaxMode,
		})
	}
	url := o.oauth.AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier))
	http.Redirect(w, r, url, http.StatusFound)
}

// ErrLoginFailed hides provider details from the browser; the cause is logged.
var ErrLoginFailed = errors.New("login failed")

// Finish validates the callback, exchanges the code and verifies the ID token.
func (o *OIDC) Finish(w http.ResponseWriter, r *http.Request) (*Claims, error) {
	cookie := func(name string) string {
		c, err := r.Cookie(flowCookie + "_" + name)
		if err != nil {
			return ""
		}
		return c.Value
	}
	state, nonce, verifier := cookie("state"), cookie("nonce"), cookie("pkce")
	for _, name := range []string{"state", "nonce", "pkce"} {
		http.SetCookie(w, &http.Cookie{Name: flowCookie + "_" + name, Path: "/auth/callback", MaxAge: -1, //nolint:gosec // G124: clearing only
			HttpOnly: true, Secure: o.secure, SameSite: http.SameSiteLaxMode})
	}

	q := r.URL.Query()
	if e := q.Get("error"); e != "" {
		return nil, fmt.Errorf("%w: provider error %q", ErrLoginFailed, e)
	}
	if state == "" || subtle.ConstantTimeCompare([]byte(state), []byte(q.Get("state"))) != 1 {
		return nil, fmt.Errorf("%w: state mismatch", ErrLoginFailed)
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	tok, err := o.oauth.Exchange(ctx, q.Get("code"), oauth2.VerifierOption(verifier))
	if err != nil {
		return nil, fmt.Errorf("%w: code exchange: %w", ErrLoginFailed, err)
	}
	rawID, ok := tok.Extra("id_token").(string)
	if !ok {
		return nil, fmt.Errorf("%w: no id_token", ErrLoginFailed)
	}
	idt, err := o.verifier.Verify(ctx, rawID)
	if err != nil {
		return nil, fmt.Errorf("%w: id_token: %w", ErrLoginFailed, err)
	}
	if nonce == "" || subtle.ConstantTimeCompare([]byte(idt.Nonce), []byte(nonce)) != 1 {
		return nil, fmt.Errorf("%w: nonce mismatch", ErrLoginFailed)
	}

	var all map[string]any
	if err := idt.Claims(&all); err != nil {
		return nil, fmt.Errorf("%w: claims: %w", ErrLoginFailed, err)
	}
	c := &Claims{Subject: idt.Subject}
	c.Email, _ = all["email"].(string)
	c.EmailVerified, _ = all["email_verified"].(bool)
	c.Name, _ = all["name"].(string)
	if c.Name == "" {
		c.Name, _ = all["preferred_username"].(string)
	}
	c.Groups = stringList(all[o.groupsClaim])
	return c, nil
}

func stringList(v any) []string {
	switch t := v.(type) {
	case []any:
		out := make([]string, 0, len(t))
		for _, x := range t {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case string:
		return []string{t}
	}
	return nil
}
