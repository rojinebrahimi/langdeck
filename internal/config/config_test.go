package config

import (
	"strings"
	"testing"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestDefaults(t *testing.T) {
	c, err := load(env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if c.Addr != ":8080" || c.AuthMode != "local" || c.AdminUsername != "admin" || !c.ForcePasswordChange || c.Production() || c.SecureCookies() {
		t.Fatalf("unexpected defaults %+v", c)
	}
}

func TestProductionGuards(t *testing.T) {
	_, err := load(env(map[string]string{"LANGDECK_ENV": "production", "LANGDECK_AUTH_MODE": "dev"}))
	if err == nil {
		t.Fatal("production with dev auth, no database and http must fail")
	}
	for _, want := range []string{"AUTH_MODE=dev", "DATABASE_URL", "https"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should mention %s: %v", want, err)
		}
	}

	c, err := load(env(map[string]string{
		"LANGDECK_ENV": "production", "LANGDECK_AUTH_MODE": "oidc", "LANGDECK_OIDC_ISSUER": "https://idp",
		"LANGDECK_OIDC_CLIENT_ID": "x", "LANGDECK_DATABASE_URL": "postgres://x", "LANGDECK_PUBLIC_URL": "https://deck.example.com/",
		"LANGDECK_OIDC_ADMIN_GROUPS": "ops, platform ",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !c.SecureCookies() || c.PublicURL != "https://deck.example.com" || len(c.AdminGroups) != 2 || c.AdminGroups[1] != "platform" {
		t.Fatalf("bad parsed config %+v", c)
	}
}

func TestLocalAdminInProduction(t *testing.T) {
	base := map[string]string{"LANGDECK_ENV": "production", "LANGDECK_DATABASE_URL": "postgres://x", "LANGDECK_PUBLIC_URL": "https://d"}
	if _, err := load(env(base)); err != nil {
		t.Fatalf("admin/admin with forced change is allowed: %v", err)
	}
	base["LANGDECK_ADMIN_FORCE_PASSWORD_CHANGE"] = "false"
	if _, err := load(env(base)); err == nil {
		t.Fatal("a permanent default password must be refused in production")
	}
	base["LANGDECK_ADMIN_PASSWORD"] = "a-long-unique-secret"
	if _, err := load(env(base)); err != nil {
		t.Fatal(err)
	}
}

func TestBadValues(t *testing.T) {
	cases := map[string]string{
		"LANGDECK_DB_MAX_CONNS":        "many",
		"LANGDECK_RETENTION_DAYS":      "1e9",
		"LANGDECK_SESSION_TTL":         "forever",
		"LANGDECK_DEFAULT_ROLE":        "root",
		"LANGDECK_AUTH_MODE":           "basic",
		"LANGDECK_BOOTSTRAP_AGENT_KEY": "short",
		"LANGDECK_LOG_FORMAT":          "xml",
		"LANGDECK_ADMIN_USERNAME":      "Bad Name",
	}
	for k, v := range cases {
		if _, err := load(env(map[string]string{k: v})); err == nil {
			t.Errorf("%s=%s should be rejected", k, v)
		}
	}
}

func TestDBMaxConnsBounds(t *testing.T) {
	for _, bad := range []string{"0", "-1", "1001", "4294967296"} {
		if _, err := load(env(map[string]string{"LANGDECK_DB_MAX_CONNS": bad})); err == nil {
			t.Errorf("LANGDECK_DB_MAX_CONNS=%s must be rejected", bad)
		}
	}
	c, err := load(env(map[string]string{"LANGDECK_DB_MAX_CONNS": "50"}))
	if err != nil || c.DBMaxConns != 50 {
		t.Fatalf("got %v %v", c, err)
	}
}
