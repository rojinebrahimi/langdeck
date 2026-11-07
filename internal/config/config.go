// Package config loads LangDeck settings from environment variables (12-factor).
package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config is the full runtime configuration.
type Config struct {
	Env         string // "development" or "production"
	Addr        string
	MetricsAddr string
	PublicURL   string
	LogLevel    string
	LogFormat   string // "json" or "text"

	DatabaseURL string
	DBMaxConns  int32

	AuthMode string // "local", "oidc" or "dev"
	// DemoMode hardens dev sign-in for a public demo: visitors get an anonymous
	// identity (no email is collected), the operator role, and no IP is stored.
	DemoMode            bool
	AdminUsername       string
	AdminPassword       string
	ForcePasswordChange bool
	OIDCIssuer          string
	OIDCClientID        string
	OIDCClientSecret    string
	OIDCScopes          []string
	OIDCGroupsClaim     string
	AdminGroups         []string
	OperatorGroups      []string
	ViewerGroups        []string
	DefaultRole         string
	BootstrapAdmins     []string
	AllowedEmailDomains []string
	SessionTTL          time.Duration

	BootstrapAgentKey string // optional pre-shared agent key, for headless setups

	// Chat notifications. Empty endpoints are simply not used.
	NotifyEvents         []string // approval, run_failed, agent_offline, agent_online
	NotifySlackURL       string
	NotifyMattermostURL  string
	NotifyTeamsURL       string
	NotifyTelegramToken  string
	NotifyTelegramChatID string

	CORSOrigins       []string
	TrustProxyHeaders bool
	LoginRateLimit    float64 // requests/second per IP on auth endpoints
	APIRateLimit      float64 // requests/second per credential on the API
	MaxBodyBytes      int64
	RetentionDays     int
	CommandTTL        time.Duration
}

// Production reports whether production safeguards apply.
func (c *Config) Production() bool { return c.Env == "production" }

// SecureCookies is true when the public URL is HTTPS.
func (c *Config) SecureCookies() bool { return strings.HasPrefix(c.PublicURL, "https://") }

// Load reads the environment and validates the result.
func Load() (*Config, error) { return load(os.Getenv) }

func load(get func(string) string) (*Config, error) {
	e := envReader{get: get}
	c := &Config{
		Env:                  e.str("LANGDECK_ENV", "development"),
		Addr:                 e.str("LANGDECK_ADDR", ":8080"),
		MetricsAddr:          e.str("LANGDECK_METRICS_ADDR", ":9090"),
		PublicURL:            strings.TrimRight(e.str("LANGDECK_PUBLIC_URL", "http://localhost:8080"), "/"),
		LogLevel:             e.str("LANGDECK_LOG_LEVEL", "info"),
		LogFormat:            e.str("LANGDECK_LOG_FORMAT", "json"),
		DatabaseURL:          e.str("LANGDECK_DATABASE_URL", ""),
		DBMaxConns:           e.int32Range("LANGDECK_DB_MAX_CONNS", 20, 1, 1000),
		AuthMode:             e.str("LANGDECK_AUTH_MODE", "local"),
		DemoMode:             e.bool("LANGDECK_DEMO_MODE", false),
		AdminUsername:        e.str("LANGDECK_ADMIN_USERNAME", "admin"),
		AdminPassword:        e.str("LANGDECK_ADMIN_PASSWORD", "admin"),
		ForcePasswordChange:  e.bool("LANGDECK_ADMIN_FORCE_PASSWORD_CHANGE", true),
		OIDCIssuer:           e.str("LANGDECK_OIDC_ISSUER", ""),
		OIDCClientID:         e.str("LANGDECK_OIDC_CLIENT_ID", ""),
		OIDCClientSecret:     e.str("LANGDECK_OIDC_CLIENT_SECRET", ""),
		OIDCScopes:           e.list("LANGDECK_OIDC_SCOPES", "openid,profile,email"),
		OIDCGroupsClaim:      e.str("LANGDECK_OIDC_GROUPS_CLAIM", "groups"),
		AdminGroups:          e.list("LANGDECK_OIDC_ADMIN_GROUPS", ""),
		OperatorGroups:       e.list("LANGDECK_OIDC_OPERATOR_GROUPS", ""),
		ViewerGroups:         e.list("LANGDECK_OIDC_VIEWER_GROUPS", ""),
		DefaultRole:          e.str("LANGDECK_DEFAULT_ROLE", "viewer"),
		BootstrapAdmins:      e.list("LANGDECK_BOOTSTRAP_ADMINS", ""),
		AllowedEmailDomains:  e.list("LANGDECK_ALLOWED_EMAIL_DOMAINS", ""),
		SessionTTL:           e.dur("LANGDECK_SESSION_TTL", 12*time.Hour),
		BootstrapAgentKey:    e.str("LANGDECK_BOOTSTRAP_AGENT_KEY", ""),
		NotifyEvents:         e.list("LANGDECK_NOTIFY_EVENTS", "approval,run_failed,agent_offline"),
		NotifySlackURL:       e.str("LANGDECK_NOTIFY_SLACK_WEBHOOK_URL", ""),
		NotifyMattermostURL:  e.str("LANGDECK_NOTIFY_MATTERMOST_WEBHOOK_URL", ""),
		NotifyTeamsURL:       e.str("LANGDECK_NOTIFY_TEAMS_WEBHOOK_URL", ""),
		NotifyTelegramToken:  e.str("LANGDECK_NOTIFY_TELEGRAM_BOT_TOKEN", ""),
		NotifyTelegramChatID: e.str("LANGDECK_NOTIFY_TELEGRAM_CHAT_ID", ""),
		CORSOrigins:          e.list("LANGDECK_CORS_ORIGINS", ""),
		TrustProxyHeaders:    e.bool("LANGDECK_TRUST_PROXY_HEADERS", false),
		LoginRateLimit:       e.float("LANGDECK_LOGIN_RATE_LIMIT", 1),
		APIRateLimit:         e.float("LANGDECK_API_RATE_LIMIT", 50),
		MaxBodyBytes:         int64(e.int("LANGDECK_MAX_BODY_BYTES", 4<<20)),
		RetentionDays:        e.int("LANGDECK_RETENTION_DAYS", 30),
		CommandTTL:           e.dur("LANGDECK_COMMAND_TTL", time.Hour),
	}
	if len(e.errs) > 0 {
		return nil, errors.Join(e.errs...)
	}
	return c, c.validate()
}

func (c *Config) validate() error {
	var errs []error
	bad := func(format string, a ...any) { errs = append(errs, fmt.Errorf(format, a...)) }

	if c.Env != "development" && c.Env != "production" {
		bad("LANGDECK_ENV must be development or production")
	}
	if u, err := url.Parse(c.PublicURL); err != nil || u.Scheme == "" || u.Host == "" {
		bad("LANGDECK_PUBLIC_URL must be an absolute URL")
	}
	switch c.AuthMode {
	case "local":
		if c.Production() && !c.ForcePasswordChange && c.AdminPassword == "admin" {
			bad("the default admin password needs LANGDECK_ADMIN_FORCE_PASSWORD_CHANGE=true in production")
		}
		if !agentNameLike(c.AdminUsername) {
			bad("LANGDECK_ADMIN_USERNAME must be 1-63 lowercase letters, digits, '-', '_' or '.'")
		}
	case "dev":
		if c.Production() {
			bad("LANGDECK_AUTH_MODE=dev is not allowed when LANGDECK_ENV=production")
		}
	case "oidc":
		if c.OIDCIssuer == "" || c.OIDCClientID == "" {
			bad("OIDC mode needs LANGDECK_OIDC_ISSUER and LANGDECK_OIDC_CLIENT_ID")
		}
	default:
		bad("LANGDECK_AUTH_MODE must be local, oidc or dev")
	}
	if c.DemoMode {
		if c.AuthMode != "dev" {
			bad("LANGDECK_DEMO_MODE needs LANGDECK_AUTH_MODE=dev")
		}
		if c.Production() {
			bad("LANGDECK_DEMO_MODE is not allowed when LANGDECK_ENV=production")
		}
	}
	switch c.DefaultRole {
	case "viewer", "operator", "admin":
	default:
		bad("LANGDECK_DEFAULT_ROLE must be viewer, operator or admin")
	}
	if c.Production() {
		if c.DatabaseURL == "" {
			bad("LANGDECK_DATABASE_URL is required in production")
		}
		if !c.SecureCookies() {
			bad("LANGDECK_PUBLIC_URL must use https in production")
		}
	}
	if c.BootstrapAgentKey != "" && (len(c.BootstrapAgentKey) < 36 || !strings.HasPrefix(c.BootstrapAgentKey, "ldk_")) {
		bad("LANGDECK_BOOTSTRAP_AGENT_KEY must start with ldk_ and be at least 36 characters")
	}
	for env, endpoint := range map[string]string{
		"LANGDECK_NOTIFY_SLACK_WEBHOOK_URL":      c.NotifySlackURL,
		"LANGDECK_NOTIFY_MATTERMOST_WEBHOOK_URL": c.NotifyMattermostURL,
		"LANGDECK_NOTIFY_TEAMS_WEBHOOK_URL":      c.NotifyTeamsURL,
	} {
		if endpoint == "" {
			continue
		}
		if u, err := url.Parse(endpoint); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			bad("%s must be an absolute http(s) URL", env)
		}
	}
	if (c.NotifyTelegramToken == "") != (c.NotifyTelegramChatID == "") {
		bad("LANGDECK_NOTIFY_TELEGRAM_BOT_TOKEN and LANGDECK_NOTIFY_TELEGRAM_CHAT_ID must be set together")
	}
	for _, kind := range c.NotifyEvents {
		switch kind {
		case "approval", "run_failed", "agent_offline", "agent_online":
		default:
			bad("LANGDECK_NOTIFY_EVENTS must list approval, run_failed, agent_offline or agent_online (got %q)", kind)
		}
	}
	if c.LoginRateLimit <= 0 || c.APIRateLimit <= 0 {
		bad("rate limits must be positive")
	}
	if c.LogFormat != "json" && c.LogFormat != "text" {
		bad("LANGDECK_LOG_FORMAT must be json or text")
	}
	return errors.Join(errs...)
}

type envReader struct {
	get  func(string) string
	errs []error
}

func (e *envReader) str(k, def string) string {
	if v := strings.TrimSpace(e.get(k)); v != "" {
		return v
	}
	return def
}

func (e *envReader) list(k, def string) []string {
	out := []string{}
	for p := range strings.SplitSeq(e.str(k, def), ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func (e *envReader) int(k string, def int) int {
	v := e.str(k, "")
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		e.errs = append(e.errs, fmt.Errorf("%s: %w", k, err))
	}
	return n
}

func (e *envReader) float(k string, def float64) float64 {
	v := e.str(k, "")
	if v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		e.errs = append(e.errs, fmt.Errorf("%s: %w", k, err))
	}
	return f
}

func (e *envReader) bool(k string, def bool) bool {
	v := e.str(k, "")
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		e.errs = append(e.errs, fmt.Errorf("%s: %w", k, err))
	}
	return b
}

func (e *envReader) dur(k string, def time.Duration) time.Duration {
	v := e.str(k, "")
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		e.errs = append(e.errs, fmt.Errorf("%s: %w", k, err))
	}
	return d
}

func agentNameLike(s string) bool {
	if s == "" || len(s) > 63 {
		return false
	}
	for _, r := range s {
		lower, digit := r >= 'a' && r <= 'z', r >= '0' && r <= '9'
		if !lower && !digit && r != '-' && r != '_' && r != '.' {
			return false
		}
	}
	return true
}

// int32Range parses an integer that must lie in [lo, hi]; out-of-range values are an error.
func (e *envReader) int32Range(k string, def, lo, hi int32) int32 {
	v := e.str(k, "")
	if v == "" {
		return def
	}
	n, err := strconv.ParseInt(v, 10, 32) // bitSize 32 makes the int32 conversion below safe
	if err != nil || int32(n) < lo || int32(n) > hi {
		e.errs = append(e.errs, fmt.Errorf("%s must be an integer between %d and %d", k, lo, hi))
		return def
	}
	return int32(n)
}
