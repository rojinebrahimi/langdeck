package jobs

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/rojinebrahimi/langdeck/internal/domain"
	"github.com/rojinebrahimi/langdeck/internal/store"
	"github.com/rojinebrahimi/langdeck/internal/store/memory"
	"github.com/rojinebrahimi/langdeck/internal/util"
)

func TestOncePurgesAndExpires(t *testing.T) {
	ctx := context.Background()
	st := memory.New()
	now := time.Now().UTC()
	a, _ := st.RegisterAgent(ctx, store.AgentRegistration{Name: "a"}, now)
	old := now.AddDate(0, 0, -40)
	_ = st.CreateRun(ctx, &domain.Run{ID: "old", AgentID: a.ID, Status: domain.RunSucceeded, CreatedAt: old})
	_ = st.CreateRun(ctx, &domain.Run{ID: "old-running", AgentID: a.ID, Status: domain.RunRunning, CreatedAt: old})
	_ = st.CreateRun(ctx, &domain.Run{ID: "new", AgentID: a.ID, Status: domain.RunSucceeded, CreatedAt: now})
	_ = st.CreateCommand(ctx, &domain.Command{ID: util.NewID(), AgentID: a.ID, Type: domain.CmdPause, Status: domain.CmdPending, CreatedAt: now.Add(-2 * time.Hour)})

	j := &Runner{Store: st, Log: slog.New(slog.NewTextHandler(io.Discard, nil)), RetentionDays: 30, CommandTTL: time.Hour}
	j.Once(ctx, now)

	if _, err := st.GetRun(ctx, "old"); err == nil {
		t.Fatal("old finished run must be purged")
	}
	for _, id := range []string{"old-running", "new"} {
		if _, err := st.GetRun(ctx, id); err != nil {
			t.Fatalf("%s must be kept", id)
		}
	}
	cmds, _ := st.ListCommands(ctx, a.ID, 10)
	if cmds[0].Status != domain.CmdExpired {
		t.Fatalf("stale command must expire, got %s", cmds[0].Status)
	}
}

func TestRunStopsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		(&Runner{Store: memory.New(), Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Interval: time.Millisecond}).Run(ctx)
		close(done)
	}()
	time.Sleep(10 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run must return when its context is cancelled")
	}
}
