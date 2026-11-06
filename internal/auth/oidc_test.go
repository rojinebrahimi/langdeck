package auth_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/rojinebrahimi/langdeck/internal/auth"
	"github.com/rojinebrahimi/langdeck/internal/testutil/oidcfake"
)

func newOIDC(t *testing.T) (*auth.OIDC, *oidcfake.Provider) {
	t.Helper()
	idp := oidcfake.New(t, "langdeck")
	o, err := auth.NewOIDC(context.Background(), idp.URL, "langdeck", "secret", "https://deck.example.com",
		[]string{"openid", "email"}, "groups", true)
	if err != nil {
		t.Fatal(err)
	}
	return o, idp
}

// begin runs the redirect step and returns the IdP URL and the flow cookies.
func begin(t *testing.T, o *auth.OIDC) (string, []*http.Cookie) {
	t.Helper()
	rec := httptest.NewRecorder()
	o.Begin(rec, httptest.NewRequest("GET", "/auth/login", nil))
	if rec.Code != http.StatusFound {
		t.Fatalf("want redirect, got %d", rec.Code)
	}
	cookies := rec.Result().Cookies()
	for _, c := range cookies {
		if !c.HttpOnly || !c.Secure || c.Path != "/auth/callback" || c.MaxAge <= 0 {
			t.Fatalf("flow cookie %q must be HttpOnly, Secure, scoped and short-lived: %+v", c.Name, c)
		}
	}
	return rec.Header().Get("Location"), cookies
}

func callback(o *auth.OIDC, code, state string, cookies []*http.Cookie) (*auth.Claims, error) {
	q := url.Values{"code": {code}, "state": {state}}
	req := httptest.NewRequest("GET", "/auth/callback?"+q.Encode(), nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	return o.Finish(httptest.NewRecorder(), req)
}

func TestOIDCLoginSucceeds(t *testing.T) {
	o, idp := newOIDC(t)
	loc, cookies := begin(t, o)
	code, state := idp.Authorize(t, loc, oidcfake.Identity{
		Subject: "u-1", Email: "ada@corp.com", EmailVerified: true, Name: "Ada", Groups: []string{"platform", "eng"},
	})
	claims, err := callback(o, code, state, cookies)
	if err != nil {
		t.Fatal(err)
	}
	if claims.Subject != "u-1" || claims.Email != "ada@corp.com" || !claims.EmailVerified || claims.Name != "Ada" || len(claims.Groups) != 2 {
		t.Fatalf("unexpected claims %+v", claims)
	}
}

func TestOIDCRejectsForgedState(t *testing.T) {
	o, idp := newOIDC(t)
	loc, cookies := begin(t, o)
	code, _ := idp.Authorize(t, loc, oidcfake.Identity{Subject: "u"})
	if _, err := callback(o, code, "attacker-state", cookies); !errors.Is(err, auth.ErrLoginFailed) {
		t.Fatalf("forged state must fail, got %v", err)
	}
}

func TestOIDCRejectsMissingFlowCookies(t *testing.T) {
	o, idp := newOIDC(t)
	loc, _ := begin(t, o)
	code, state := idp.Authorize(t, loc, oidcfake.Identity{Subject: "u"})
	// Login CSRF: a victim's browser never saw /auth/login, so it has no flow cookies.
	if _, err := callback(o, code, state, nil); !errors.Is(err, auth.ErrLoginFailed) {
		t.Fatalf("callback without flow cookies must fail, got %v", err)
	}
}

func TestOIDCRejectsWrongPKCEVerifier(t *testing.T) {
	o, idp := newOIDC(t)
	loc, cookies := begin(t, o)
	code, state := idp.Authorize(t, loc, oidcfake.Identity{Subject: "u"})
	for _, c := range cookies {
		if c.Name == "langdeck_oauth_pkce" {
			c.Value = "stolen-code-without-the-real-verifier"
		}
	}
	if _, err := callback(o, code, state, cookies); !errors.Is(err, auth.ErrLoginFailed) {
		t.Fatalf("wrong PKCE verifier must fail, got %v", err)
	}
}

func TestOIDCRejectsNonceMismatch(t *testing.T) {
	o, idp := newOIDC(t)
	loc, cookies := begin(t, o)
	code, state := idp.Authorize(t, loc, oidcfake.Identity{Subject: "u"})
	idp.TamperNonce = true
	if _, err := callback(o, code, state, cookies); !errors.Is(err, auth.ErrLoginFailed) {
		t.Fatalf("replayed ID token (nonce mismatch) must fail, got %v", err)
	}
}

func TestOIDCCodeIsSingleUse(t *testing.T) {
	o, idp := newOIDC(t)
	loc, cookies := begin(t, o)
	code, state := idp.Authorize(t, loc, oidcfake.Identity{Subject: "u"})
	if _, err := callback(o, code, state, cookies); err != nil {
		t.Fatal(err)
	}
	if _, err := callback(o, code, state, cookies); !errors.Is(err, auth.ErrLoginFailed) {
		t.Fatalf("reused code must fail, got %v", err)
	}
}

func TestOIDCProviderError(t *testing.T) {
	o, _ := newOIDC(t)
	_, cookies := begin(t, o)
	req := httptest.NewRequest("GET", "/auth/callback?error=access_denied", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	if _, err := o.Finish(httptest.NewRecorder(), req); !errors.Is(err, auth.ErrLoginFailed) {
		t.Fatalf("provider error must fail, got %v", err)
	}
}

func TestOIDCDiscoveryFailure(t *testing.T) {
	if _, err := auth.NewOIDC(context.Background(), "http://127.0.0.1:1", "x", "", "https://d", nil, "groups", true); err == nil {
		t.Fatal("unreachable issuer must fail at startup")
	}
}
