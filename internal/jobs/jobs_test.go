package jobs

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rojinebrahimi/langdeck/internal/domain"
	"github.com/rojinebrahimi/langdeck/internal/notify"
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

func TestCheckAgentsNotifiesOnTransitions(t *testing.T) {
	ctx := context.Background()
	st := memory.New()
	now := time.Now().UTC()
	a, _ := st.RegisterAgent(ctx, store.AgentRegistration{Name: "billing"}, now)

	got := make(chan string, 8)
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var m struct {
			Text string `json:"text"`
		}
		_ = json.NewDecoder(r.Body).Decode(&m)
		got <- m.Text
	}))
	defer hook.Close()
	n := notify.New(notify.Options{SlackURL: hook.URL, Events: []string{"agent_offline", "agent_online"}})
	rctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go n.Run(rctx)

	j := &Runner{Store: st, Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Notify: n, PublicURL: "https://deck.example.com"}

	j.CheckAgents(ctx, now) // online: nothing to say
	late := now.Add(2 * domain.HeartbeatTTL)
	j.CheckAgents(ctx, late)
	j.CheckAgents(ctx, late.Add(time.Second)) // still offline: no repeat
	if msg := recv(t, got); !strings.Contains(msg, "Agent offline: billing") || !strings.Contains(msg, "/agents/"+a.ID) {
		t.Fatalf("bad offline message: %s", msg)
	}

	_ = st.Heartbeat(ctx, a.ID, 1, late)
	j.CheckAgents(ctx, late.Add(time.Second))
	if msg := recv(t, got); !strings.Contains(msg, "Agent back online: billing") {
		t.Fatalf("bad online message: %s", msg)
	}
	select {
	case extra := <-got:
		t.Fatalf("unexpected notification: %s", extra)
	case <-time.After(200 * time.Millisecond):
	}
}

func recv(t *testing.T, ch <-chan string) string {
	t.Helper()
	select {
	case s := <-ch:
		return s
	case <-time.After(3 * time.Second):
		t.Fatal("no notification delivered")
		return ""
	}
}
