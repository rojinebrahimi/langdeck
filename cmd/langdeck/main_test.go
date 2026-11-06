package main

import (
	"context"
	"testing"

	"github.com/rojinebrahimi/langdeck/internal/auth"
	"github.com/rojinebrahimi/langdeck/internal/domain"
	"github.com/rojinebrahimi/langdeck/internal/store/memory"
)

func TestPortOf(t *testing.T) {
	for in, want := range map[string]string{":8080": ":8080", "0.0.0.0:9000": ":9000", "8081": ":8081"} {
		if got := portOf(in); got != want {
			t.Errorf("portOf(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEnsureBootstrapKeyIsIdempotent(t *testing.T) {
	st := memory.New()
	secret := "ldk_bootstrap-secret-for-tests-000000000"
	for range 2 {
		if err := ensureBootstrapKey(context.Background(), st, secret); err != nil {
			t.Fatal(err)
		}
	}
	keys, _ := st.ListAPIKeys(context.Background())
	if len(keys) != 1 || keys[0].Role != domain.RoleAgent || keys[0].Hash != auth.HashSecret(secret) {
		t.Fatalf("want exactly one hashed agent key, got %+v", keys)
	}
	if err := ensureBootstrapKey(context.Background(), st, ""); err != nil {
		t.Fatal("empty bootstrap key is a no-op")
	}
}
