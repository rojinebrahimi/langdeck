// Command langdeck runs the LangDeck control plane.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/rojinebrahimi/langdeck/internal/api"
	"github.com/rojinebrahimi/langdeck/internal/auth"
	"github.com/rojinebrahimi/langdeck/internal/config"
	"github.com/rojinebrahimi/langdeck/internal/domain"
	"github.com/rojinebrahimi/langdeck/internal/events"
	"github.com/rojinebrahimi/langdeck/internal/jobs"
	"github.com/rojinebrahimi/langdeck/internal/store"
	"github.com/rojinebrahimi/langdeck/internal/store/memory"
	"github.com/rojinebrahimi/langdeck/internal/store/postgres"
	"github.com/rojinebrahimi/langdeck/internal/util"
	"github.com/rojinebrahimi/langdeck/web"
)

// Set at build time with -ldflags "-X main.version=... -X main.commit=...".
var (
	version = "dev"
	commit  = "none"
)

func main() {
	cmd := "serve"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	switch cmd {
	case "serve":
		if err := serve(); err != nil {
			fmt.Fprintln(os.Stderr, "langdeck:", err)
			os.Exit(1)
		}
	case "healthcheck": // used by the container HEALTHCHECK; the image has no shell or curl
		os.Exit(healthcheck())
	case "version", "--version", "-v":
		fmt.Printf("langdeck %s (%s)\n", version, commit)
	default:
		fmt.Fprintf(os.Stderr, "usage: langdeck [serve|healthcheck|version]\n")
		os.Exit(2)
	}
}

func healthcheck() int {
	addr := os.Getenv("LANGDECK_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	port, err := strconv.ParseUint(strings.TrimPrefix(portOf(addr), ":"), 10, 16)
	if err != nil {
		return 1
	}
	// The host is fixed to loopback; only a validated numeric port comes from config.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/healthz", port), nil) //nolint:gosec // G704: loopback only
	if err != nil {
		return 1
	}
	resp, err := http.DefaultClient.Do(req) //nolint:gosec // G704: loopback only
	if err != nil || resp.StatusCode != http.StatusOK {
		return 1
	}
	_ = resp.Body.Close()
	return 0
}

func portOf(addr string) string {
	for i := len(addr) - 1; i >= 0; i-- {
		if addr[i] == ':' {
			return addr[i:]
		}
	}
	return ":" + addr
}

func newLogger(cfg *config.Config) *slog.Logger {
	var level slog.Level
	_ = level.UnmarshalText([]byte(cfg.LogLevel))
	opts := &slog.HandlerOptions{Level: level}
	if cfg.LogFormat == "text" {
		return slog.New(slog.NewTextHandler(os.Stdout, opts))
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, opts))
}

func serve() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("invalid configuration:\n%w", err)
	}
	log := newLogger(cfg)
	log.Info("starting langdeck", "version", version, "commit", commit, "env", cfg.Env, "auth_mode", cfg.AuthMode)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var (
		st     store.Store
		broker events.Broker
	)
	if cfg.DatabaseURL != "" {
		pg, err := postgres.Open(ctx, cfg.DatabaseURL, cfg.DBMaxConns)
		if err != nil {
			return err
		}
		st, broker = pg, events.NewPostgres(ctx, pg.Pool(), log)
		log.Info("using postgres store")
	} else {
		st, broker = memory.New(), events.NewLocal()
		log.Warn("using in-memory store: data is lost on restart and replicas do not share state")
	}
	// Cancel background work (the LISTEN connection) before closing the pool,
	// otherwise Close waits forever for that connection to be released.
	defer func() { stop(); st.Close() }()

	if cfg.AuthMode == "dev" {
		log.Warn("DEV AUTH MODE: anyone who can reach this server can sign in as admin. Never expose it.")
	}
	if cfg.AuthMode == "local" {
		created, err := api.EnsureLocalAdmin(ctx, st, cfg.AdminUsername, cfg.AdminPassword, cfg.ForcePasswordChange)
		if err != nil {
			return fmt.Errorf("create admin account: %w", err)
		}
		if created && cfg.AdminPassword == "admin" {
			log.Warn("created admin account with the default password; it must be changed at first sign-in", "username", cfg.AdminUsername)
		}
	}
	if err := ensureBootstrapKey(ctx, st, cfg.BootstrapAgentKey); err != nil {
		return err
	}

	a := &api.API{
		Store: st, Broker: broker, Cfg: cfg, Log: log, UI: web.Handler(),
		Auth: &auth.Authenticator{Store: st, SecureCookies: cfg.SecureCookies(), Now: func() time.Time { return time.Now().UTC() }},
	}
	if cfg.AuthMode == "oidc" {
		a.OIDC, err = auth.NewOIDC(ctx, cfg.OIDCIssuer, cfg.OIDCClientID, cfg.OIDCClientSecret, cfg.PublicURL,
			cfg.OIDCScopes, cfg.OIDCGroupsClaim, cfg.SecureCookies())
		if err != nil {
			return err
		}
	}

	go (&jobs.Runner{Store: st, Log: log, RetentionDays: cfg.RetentionDays, CommandTTL: cfg.CommandTTL}).Run(ctx)

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           a.Handler(ctx),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      45 * time.Second, // above the 30s long-poll; SSE clears its own deadline
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	// Metrics live on their own port so they are never exposed through the public ingress.
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	metrics := &http.Server{Addr: cfg.MetricsAddr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}

	errs := make(chan error, 2)
	go func() { errs <- srv.ListenAndServe() }()
	go func() { errs <- metrics.ListenAndServe() }()
	log.Info("listening", "addr", cfg.Addr, "metrics_addr", cfg.MetricsAddr, "public_url", cfg.PublicURL)

	select {
	case err := <-errs:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
	}
	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	_ = metrics.Shutdown(shutdownCtx)
	return srv.Shutdown(shutdownCtx)
}

// ensureBootstrapKey provisions a pre-shared agent key, for setups where agents
// must start before anyone opens the UI (docker compose, GitOps).
func ensureBootstrapKey(ctx context.Context, st store.Store, secret string) error {
	if secret == "" {
		return nil
	}
	if _, err := st.GetAPIKeyByHash(ctx, auth.HashSecret(secret)); err == nil {
		return nil
	}
	prefix := secret
	if len(prefix) > 12 {
		prefix = prefix[:12]
	}
	return st.CreateAPIKey(ctx, &domain.APIKey{
		ID: util.NewID(), Name: "bootstrap", Prefix: prefix, Hash: auth.HashSecret(secret),
		Role: domain.RoleAgent, CreatedBy: "system", CreatedAt: time.Now().UTC(),
	})
}
