package api_test

import (
	"context"
	"testing"

	"github.com/rojinebrahimi/langdeck/internal/api"
	"github.com/rojinebrahimi/langdeck/internal/auth"
	"github.com/rojinebrahimi/langdeck/internal/store/memory"
)

func TestEnsureLocalAdminNeverOverwrites(t *testing.T) {
	ctx := context.Background()
	st := memory.New()
	created, err := api.EnsureLocalAdmin(ctx, st, "admin", "admin", true)
	if err != nil || !created {
		t.Fatalf("first start must create the admin: %v %v", created, err)
	}
	u, _ := st.GetUserBySubject(ctx, auth.LocalSubject("admin"))
	h, _ := auth.HashPassword("my-changed-password")
	_ = st.SetPassword(ctx, u.ID, h, false)

	created, err = api.EnsureLocalAdmin(ctx, st, "admin", "admin", true)
	if err != nil || created {
		t.Fatalf("restart must not recreate the admin: %v %v", created, err)
	}
	u, _ = st.GetUserBySubject(ctx, auth.LocalSubject("admin"))
	if !auth.VerifyPassword(u.PasswordHash, "my-changed-password") || u.MustChangePassword {
		t.Fatal("a restart must never reset a changed password back to the default")
	}
}
