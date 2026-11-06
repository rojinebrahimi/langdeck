package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rojinebrahimi/langdeck/internal/domain"
	"github.com/rojinebrahimi/langdeck/internal/store/memory"
)

func TestNewAPIKeyNeverStoresSecret(t *testing.T) {
	secret, k := NewAPIKey("k", domain.RoleAgent, "", "t", nil)
	if !strings.HasPrefix(secret, KeyPrefix) || len(secret) < 40 {
		t.Fatalf("weak secret %q", secret)
	}
	if k.Hash != HashSecret(secret) || strings.Contains(k.Hash, secret) || !strings.HasPrefix(secret, k.Prefix) {
		t.Fatal("key must store only the hash and a short prefix")
	}
	other, _ := NewAPIKey("k", domain.RoleAgent, "", "t", nil)
	if other == secret {
		t.Fatal("keys must be unique")
	}
}

func TestAuthenticateAPIKey(t *testing.T) {
	st := memory.New()
	a := &Authenticator{Store: st, Now: time.Now}
	secret, k := NewAPIKey("bot", domain.RoleOperator, "", "t", nil)
	_ = st.CreateAPIKey(context.Background(), k)

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+secret)
	p, err := a.Authenticate(req)
	if err != nil || p.Role != domain.RoleOperator || p.Kind != KindAPIKey {
		t.Fatalf("got %+v, %v", p, err)
	}

	past := time.Now().Add(-time.Second)
	expiredSecret, expired := NewAPIKey("old", domain.RoleAdmin, "", "t", &past)
	_ = st.CreateAPIKey(context.Background(), expired)
	req.Header.Set("Authorization", "Bearer "+expiredSecret)
	if _, err := a.Authenticate(req); err == nil {
		t.Fatal("expired key must be rejected")
	}
	req.Header.Set("Authorization", "Basic abc")
	if _, err := a.Authenticate(req); err == nil {
		t.Fatal("non-bearer auth must be rejected")
	}
}

func TestSessionLifecycle(t *testing.T) {
	st := memory.New()
	a := &Authenticator{Store: st, SecureCookies: true, Now: time.Now}
	u, _ := st.UpsertUserOnLogin(context.Background(), "s", "e@x.io", "E", domain.RoleViewer)

	rec := httptest.NewRecorder()
	if err := a.StartSession(rec, httptest.NewRequest("GET", "/", nil), u, time.Hour, "1.2.3.4"); err != nil {
		t.Fatal(err)
	}
	cookie := rec.Result().Cookies()[0]
	if cookie.Name != "__Host-langdeck_session" || !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("insecure cookie %+v", cookie)
	}
	if _, err := st.GetSession(context.Background(), cookie.Value); err == nil {
		t.Fatal("the raw cookie value must never be the stored session id")
	}

	req := httptest.NewRequest("POST", "/", nil)
	req.AddCookie(cookie)
	p, err := a.Authenticate(req)
	if err != nil || p.ID != u.ID || p.CSRFToken == "" {
		t.Fatalf("got %+v, %v", p, err)
	}
	if CheckCSRF(req, p) {
		t.Fatal("POST without CSRF header must fail")
	}
	req.Header.Set(CSRFHeader, p.CSRFToken)
	if !CheckCSRF(req, p) {
		t.Fatal("POST with the right CSRF token must pass")
	}
	if !CheckCSRF(httptest.NewRequest("GET", "/", nil), p) {
		t.Fatal("GET needs no CSRF token")
	}
	if !CheckCSRF(req, &Principal{Kind: KindAPIKey}) {
		t.Fatal("bearer requests are not subject to CSRF")
	}

	a.Now = func() time.Time { return time.Now().Add(2 * time.Hour) }
	if _, err := a.Authenticate(req); err == nil {
		t.Fatal("expired session must be rejected")
	}
}

func TestResolveRole(t *testing.T) {
	admins, ops, viewers := []string{"platform"}, []string{"sre"}, []string{"eng"}
	cases := []struct {
		email  string
		groups []string
		boot   []string
		want   domain.Role
	}{
		{"boss@x.io", nil, []string{"BOSS@x.io"}, domain.RoleAdmin},
		{"a@x.io", []string{"eng", "platform"}, nil, domain.RoleAdmin},
		{"a@x.io", []string{"sre"}, nil, domain.RoleOperator},
		{"a@x.io", []string{"eng"}, nil, domain.RoleViewer},
		{"a@x.io", []string{"sales"}, nil, ""},
	}
	for _, c := range cases {
		if got := ResolveRole(c.email, c.groups, c.boot, admins, ops, viewers); got != c.want {
			t.Errorf("%s %v: got %q want %q", c.email, c.groups, got, c.want)
		}
	}
}

func TestEmailAllowed(t *testing.T) {
	if !EmailAllowed("a@b.c", nil) {
		t.Fatal("empty allow-list allows everyone")
	}
	if !EmailAllowed("a@Corp.com", []string{"corp.com"}) || EmailAllowed("a@corp.com.evil.io", []string{"corp.com"}) || EmailAllowed("nope", []string{"corp.com"}) {
		t.Fatal("domain allow-list mismatch")
	}
}

func TestStringList(t *testing.T) {
	if got := stringList([]any{"a", 1, "b"}); len(got) != 2 {
		t.Fatalf("got %v", got)
	}
	if got := stringList("solo"); len(got) != 1 {
		t.Fatalf("got %v", got)
	}
	if stringList(nil) != nil {
		t.Fatal("nil claim gives nil")
	}
}

func TestPasswordHashing(t *testing.T) {
	h, err := HashPassword("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "pbkdf2-sha256$600000$") || strings.Contains(h, "correct") {
		t.Fatalf("bad encoding %q", h)
	}
	if !VerifyPassword(h, "correct horse battery") || VerifyPassword(h, "wrong") {
		t.Fatal("verify mismatch")
	}
	other, _ := HashPassword("correct horse battery")
	if other == h {
		t.Fatal("salts must differ")
	}
	for _, bad := range []string{"", "plain", "pbkdf2-sha256$x$y$z", "md5$1$a$b"} {
		if VerifyPassword(bad, "anything") {
			t.Fatalf("%q must never verify", bad)
		}
	}
	if ValidatePassword("short") == nil || ValidatePassword("long-enough-pw") != nil {
		t.Fatal("password policy")
	}
}
