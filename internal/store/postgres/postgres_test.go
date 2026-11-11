package postgres_test

import (
	"context"
	"os"
	"testing"

	"github.com/rojinebrahimi/langdeck/internal/store"
	"github.com/rojinebrahimi/langdeck/internal/store/postgres"
	"github.com/rojinebrahimi/langdeck/internal/store/storetest"
)

// Set LANGDECK_TEST_DATABASE_URL to run the contract against a real database,
// for example: postgres://langdeck:langdeck@localhost:5432/langdeck_test?sslmode=disable
// CI does this with a Postgres service container.
func TestContract(t *testing.T) {
	dsn := os.Getenv("LANGDECK_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("LANGDECK_TEST_DATABASE_URL not set")
	}
	storetest.Run(t, func(t *testing.T) store.Store {
		ctx := context.Background()
		s, err := postgres.Open(ctx, dsn, 5)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.Pool().Exec(ctx, `TRUNCATE users, sessions, api_keys, agents, agent_configs, runs,
			run_events, commands, interrupts, audit_log, settings RESTART IDENTITY CASCADE`); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(s.Close)
		return s
	})
}

func TestMigrateIsIdempotent(t *testing.T) {
	dsn := os.Getenv("LANGDECK_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("LANGDECK_TEST_DATABASE_URL not set")
	}
	for range 2 {
		s, err := postgres.Open(context.Background(), dsn, 2)
		if err != nil {
			t.Fatal(err)
		}
		s.Close()
	}
}
