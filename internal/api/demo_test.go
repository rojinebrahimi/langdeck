package api_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/rojinebrahimi/langdeck/internal/config"
	"github.com/rojinebrahimi/langdeck/internal/domain"
)

// A public demo must never show one visitor another visitor's email or IP.
func TestDemoModeKeepsVisitorsAnonymous(t *testing.T) {
	e := newEnvWith(t, "dev", nil, func(c *config.Config) { c.DemoMode = true })

	var info struct {
		Mode string `json:"mode"`
		Demo bool   `json:"demo"`
	}
	e.bearer("").do("GET", "/api/v1/auth/info", nil, http.StatusOK, &info)
	if info.Mode != "dev" || !info.Demo {
		t.Fatalf("auth info must advertise demo mode: %+v", info)
	}

	// The email a visitor types is ignored entirely.
	alice := e.browser("alice.real@company.com")
	bob := e.browser("bob.real@company.com")
	var me domain.User
	alice.do("GET", "/api/v1/me", nil, http.StatusOK, &me)
	if strings.Contains(me.Email, "alice") || !strings.HasSuffix(me.Email, "@demo.invalid") ||
		!strings.HasPrefix(me.Name, "Visitor ") {
		t.Fatalf("demo identity must be anonymous, got %q / %q", me.Email, me.Name)
	}
	if me.Role != domain.RoleOperator {
		t.Fatalf("demo visitors must be operators, got %s", me.Role)
	}

	// Operators can use the product but cannot list people, keys or the audit trail.
	for _, path := range []string{"/api/v1/users", "/api/v1/audit", "/api/v1/api-keys"} {
		bob.do("GET", path, nil, http.StatusForbidden, nil)
	}
	alice.do("GET", "/api/v1/agents", nil, http.StatusOK, nil)

	// Nothing personal is stored either: no real email and no IP address.
	users, err := e.store.ListUsers(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range users {
		if strings.Contains(u.Email, "company.com") {
			t.Fatalf("a real email was stored: %s", u.Email)
		}
	}
	alice.do("POST", "/auth/logout", nil, http.StatusNoContent, nil) // writes an audit entry
	entries, err := e.store.ListAudit(t.Context(), 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("expected at least one audit entry to check")
	}
	for _, a := range entries {
		if strings.Contains(a.Actor, "company.com") {
			t.Fatalf("a real email reached the audit log: %s", a.Actor)
		}
		if a.IP != "" {
			t.Fatalf("demo audit entries must not record IPs: %+v", a)
		}
	}
}
