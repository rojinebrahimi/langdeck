// Package storetest is a contract suite every store.Store implementation must pass.
package storetest

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/rojinebrahimi/langdeck/internal/domain"
	"github.com/rojinebrahimi/langdeck/internal/store"
	"github.com/rojinebrahimi/langdeck/internal/util"
)

// Run executes the contract against fresh stores built by newStore.
func Run(t *testing.T, newStore func(t *testing.T) store.Store) {
	t.Run("users", func(t *testing.T) { testUsers(t, newStore(t)) })
	t.Run("sessions", func(t *testing.T) { testSessions(t, newStore(t)) })
	t.Run("api_keys", func(t *testing.T) { testKeys(t, newStore(t)) })
	t.Run("agents", func(t *testing.T) { testAgents(t, newStore(t)) })
	t.Run("configs", func(t *testing.T) { testConfigs(t, newStore(t)) })
	t.Run("runs_and_events", func(t *testing.T) { testRuns(t, newStore(t)) })
	t.Run("commands", func(t *testing.T) { testCommands(t, newStore(t)) })
	t.Run("interrupts", func(t *testing.T) { testInterrupts(t, newStore(t)) })
	t.Run("audit", func(t *testing.T) { testAudit(t, newStore(t)) })
}

var ctx = context.Background()

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func now() time.Time { return time.Now().UTC().Truncate(time.Microsecond) }

func agent(t *testing.T, s store.Store, name string) *domain.Agent {
	t.Helper()
	a, err := s.RegisterAgent(ctx, store.AgentRegistration{Name: name, Version: "1", Graph: json.RawMessage(`{"nodes":[]}`)}, now())
	must(t, err)
	return a
}

func testUsers(t *testing.T, s store.Store) {
	u, err := s.UpsertUserOnLogin(ctx, "sub-1", "a@x.io", "A", domain.RoleViewer)
	must(t, err)
	must(t, s.SetUserRole(ctx, u.ID, domain.RoleAdmin))
	again, err := s.UpsertUserOnLogin(ctx, "sub-1", "new@x.io", "A2", domain.RoleViewer)
	must(t, err)
	if again.ID != u.ID || again.Role != domain.RoleAdmin || again.Email != "new@x.io" {
		t.Fatalf("re-login must keep id and role and refresh profile: %+v", again)
	}
	if err := s.SetUserRole(ctx, util.NewID(), domain.RoleAdmin); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	local := &domain.User{ID: util.NewID(), Subject: "local|admin", Name: "admin", Role: domain.RoleAdmin,
		PasswordHash: "h1", MustChangePassword: true, CreatedAt: now()}
	must(t, s.CreateUser(ctx, local))
	if err := s.CreateUser(ctx, &domain.User{ID: util.NewID(), Subject: "local|admin", Role: domain.RoleViewer, CreatedAt: now()}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("duplicate subject must conflict, got %v", err)
	}
	must(t, s.SetPassword(ctx, local.ID, "h2", false))
	got, err := s.GetUserBySubject(ctx, "local|admin")
	must(t, err)
	if got.PasswordHash != "h2" || got.MustChangePassword || !got.Local {
		t.Fatalf("bad local user %+v", got)
	}
	list, err := s.ListUsers(ctx)
	must(t, err)
	if len(list) != 2 {
		t.Fatalf("want 2 users, got %d", len(list))
	}
}

func testSessions(t *testing.T, s store.Store) {
	u, err := s.UpsertUserOnLogin(ctx, "sub", "e@x.io", "E", domain.RoleViewer)
	must(t, err)
	n := now()
	must(t, s.CreateSession(ctx, &domain.Session{ID: "h1", UserID: u.ID, CSRFToken: "c", CreatedAt: n, ExpiresAt: n.Add(time.Hour)}))
	must(t, s.CreateSession(ctx, &domain.Session{ID: "h2", UserID: u.ID, CSRFToken: "c", CreatedAt: n, ExpiresAt: n.Add(-time.Hour)}))
	got, err := s.GetSession(ctx, "h1")
	must(t, err)
	if got.UserID != u.ID || got.CSRFToken != "c" {
		t.Fatalf("bad session %+v", got)
	}
	removed, err := s.DeleteExpiredSessions(ctx, n)
	must(t, err)
	if removed != 1 {
		t.Fatalf("want 1 expired session removed, got %d", removed)
	}
	must(t, s.DeleteSession(ctx, "h1"))
	if _, err := s.GetSession(ctx, "h1"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func testKeys(t *testing.T, s store.Store) {
	k := &domain.APIKey{ID: util.NewID(), Name: "k", Prefix: "ldk_abc", Hash: "h", Role: domain.RoleAgent, CreatedBy: "t", CreatedAt: now()}
	must(t, s.CreateAPIKey(ctx, k))
	got, err := s.GetAPIKeyByHash(ctx, "h")
	must(t, err)
	if got.ID != k.ID || !got.Active(now()) {
		t.Fatalf("bad key %+v", got)
	}
	must(t, s.TouchAPIKey(ctx, k.ID, now()))
	must(t, s.RevokeAPIKey(ctx, k.ID, now()))
	got, _ = s.GetAPIKeyByHash(ctx, "h")
	if got.Active(now()) || got.LastUsedAt == nil {
		t.Fatalf("revoked key must be inactive and touched: %+v", got)
	}
}

func testAgents(t *testing.T, s store.Store) {
	a := agent(t, s, "alpha")
	if a.DesiredState != domain.DesiredRunning || a.LastHeartbeat == nil {
		t.Fatalf("bad new agent %+v", a)
	}
	// Re-registering without a graph keeps the old graph.
	b, err := s.RegisterAgent(ctx, store.AgentRegistration{Name: "alpha", Version: "2"}, now())
	must(t, err)
	if b.ID != a.ID || b.Version != "2" || len(b.Graph) == 0 {
		t.Fatalf("re-register must update in place and keep graph: %+v", b)
	}
	must(t, s.SetAgentDesiredState(ctx, a.ID, domain.DesiredPaused))
	must(t, s.Heartbeat(ctx, a.ID, 3, now()))
	got, err := s.GetAgentByName(ctx, "alpha")
	must(t, err)
	if got.DesiredState != domain.DesiredPaused || got.Instances != 3 {
		t.Fatalf("bad agent %+v", got)
	}
	must(t, s.DeleteAgent(ctx, a.ID))
	if _, err := s.GetAgent(ctx, a.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("want ErrNotFound after delete, got %v", err)
	}
}

func testConfigs(t *testing.T, s store.Store) {
	a := agent(t, s, "cfg")
	if _, err := s.LatestAgentConfig(ctx, a.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	for i := 1; i <= 3; i++ {
		c := &domain.AgentConfig{AgentID: a.ID, Config: json.RawMessage(`{"temperature":0.1}`), CreatedBy: "t", CreatedAt: now()}
		must(t, s.CreateAgentConfig(ctx, c))
		if c.Version != i {
			t.Fatalf("want version %d, got %d", i, c.Version)
		}
	}
	latest, err := s.LatestAgentConfig(ctx, a.ID)
	must(t, err)
	list, err := s.ListAgentConfigs(ctx, a.ID)
	must(t, err)
	if latest.Version != 3 || len(list) != 3 || list[0].Version != 3 {
		t.Fatalf("bad versions: latest=%d list=%d first=%d", latest.Version, len(list), list[0].Version)
	}
}

func testRuns(t *testing.T, s store.Store) {
	a := agent(t, s, "runner")
	other := agent(t, s, "other")
	base := now().Add(-time.Hour)
	for i := range 3 {
		ts := base.Add(time.Duration(i) * time.Minute)
		must(t, s.CreateRun(ctx, &domain.Run{ID: util.NewID(), AgentID: a.ID, Status: domain.RunSucceeded, CreatedAt: ts, StartedAt: &ts, EndedAt: &ts}))
	}
	r := &domain.Run{ID: "sdk-run-1", AgentID: a.ID, Status: domain.RunRunning, Trigger: "sdk", CreatedAt: now()}
	got, err := s.EnsureRun(ctx, r)
	must(t, err)
	if got.AgentName != "runner" {
		t.Fatalf("unexpected agent name %q", got.AgentName)
	}
	if _, err := s.EnsureRun(ctx, r); err != nil {
		t.Fatalf("EnsureRun must be idempotent: %v", err)
	}
	if _, err := s.EnsureRun(ctx, &domain.Run{ID: "sdk-run-1", AgentID: other.ID, Status: domain.RunRunning, CreatedAt: now()}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("a run id owned by another agent must conflict, got %v", err)
	}

	got.Status, got.PromptTokens, got.Output = domain.RunFailed, 42, json.RawMessage(`{"ok":false}`)
	must(t, s.UpdateRun(ctx, got))
	got, _ = s.GetRun(ctx, "sdk-run-1")
	if got.Status != domain.RunFailed || got.PromptTokens != 42 || string(got.Output) == "" {
		t.Fatalf("update not persisted: %+v", got)
	}

	page, err := s.ListRuns(ctx, store.RunFilter{AgentID: a.ID, Limit: 2})
	must(t, err)
	if len(page) != 2 || page[0].ID != "sdk-run-1" {
		t.Fatalf("want newest first, got %d runs", len(page))
	}
	next, err := s.ListRuns(ctx, store.RunFilter{AgentID: a.ID, Before: page[1].CreatedAt})
	must(t, err)
	if len(next) != 2 {
		t.Fatalf("cursor page: want 2, got %d", len(next))
	}
	failed, _ := s.ListRuns(ctx, store.RunFilter{Status: domain.RunFailed})
	if len(failed) != 1 {
		t.Fatalf("status filter: want 1, got %d", len(failed))
	}

	evs := []domain.RunEvent{
		{RunID: "sdk-run-1", Type: domain.EvNodeStart, Node: "a", Timestamp: now()},
		{RunID: "sdk-run-1", Type: domain.EvNodeEnd, Node: "a", Payload: json.RawMessage(`{"x":1}`), LatencyMS: 5, Timestamp: now()},
	}
	must(t, s.AppendEvents(ctx, evs))
	if evs[0].ID == 0 || evs[1].ID <= evs[0].ID {
		t.Fatalf("event ids must be assigned and increasing: %d %d", evs[0].ID, evs[1].ID)
	}
	list, err := s.ListEvents(ctx, "sdk-run-1", evs[0].ID, 0)
	must(t, err)
	if len(list) != 1 || list[0].Node != "a" || list[0].LatencyMS != 5 {
		t.Fatalf("after-cursor events: %+v", list)
	}

	n, err := s.PurgeRunsBefore(ctx, now().Add(time.Second))
	must(t, err)
	if n != 4 {
		t.Fatalf("want 4 terminal runs purged, got %d", n)
	}
	if left, _ := s.ListEvents(ctx, "sdk-run-1", 0, 0); len(left) != 0 {
		t.Fatalf("events must be purged with their run")
	}
}

func testCommands(t *testing.T, s store.Store) {
	a := agent(t, s, "cmd")
	b := agent(t, s, "cmd-b")
	base := now()
	for i := range 3 {
		must(t, s.CreateCommand(ctx, &domain.Command{ID: util.NewID(), AgentID: a.ID, Type: domain.CmdPause,
			Status: domain.CmdPending, CreatedBy: "t", CreatedAt: base.Add(time.Duration(i) * time.Millisecond)}))
	}
	must(t, s.CreateCommand(ctx, &domain.Command{ID: util.NewID(), AgentID: b.ID, Type: domain.CmdPause, Status: domain.CmdPending, CreatedBy: "t", CreatedAt: base}))

	first, err := s.ClaimCommands(ctx, a.ID, 2, base)
	must(t, err)
	if len(first) != 2 || !first[0].CreatedAt.Before(first[1].CreatedAt) {
		t.Fatalf("want 2 commands in creation order, got %d", len(first))
	}
	second, _ := s.ClaimCommands(ctx, a.ID, 10, base)
	if len(second) != 1 {
		t.Fatalf("already delivered commands must not be handed out again: got %d", len(second))
	}
	must(t, s.AckCommand(ctx, a.ID, first[0].ID, true, "done", base))
	if err := s.AckCommand(ctx, b.ID, first[1].ID, true, "", base); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("an agent must not ack another agent's command, got %v", err)
	}
	// Un-acked deliveries come back after RedeliverAfter.
	again, _ := s.ClaimCommands(ctx, a.ID, 10, base.Add(store.RedeliverAfter+time.Second))
	if len(again) != 2 {
		t.Fatalf("want 2 redelivered commands, got %d", len(again))
	}
	n, err := s.ExpireCommands(ctx, base.Add(time.Hour))
	must(t, err)
	if n != 3 { // two re-delivered for a, one pending for b
		t.Fatalf("want 3 expired, got %d", n)
	}
	list, _ := s.ListCommands(ctx, a.ID, 10)
	if len(list) != 3 {
		t.Fatalf("want 3 commands listed, got %d", len(list))
	}
}

func testInterrupts(t *testing.T, s store.Store) {
	a := agent(t, s, "hitl")
	must(t, s.CreateRun(ctx, &domain.Run{ID: "r1", AgentID: a.ID, Status: domain.RunRunning, CreatedAt: now()}))
	in := &domain.Interrupt{ID: "i1", RunID: "r1", AgentID: a.ID, Node: "review", Question: "ok?", Status: domain.InterruptPending, CreatedAt: now()}
	must(t, s.CreateInterrupt(ctx, in))
	if err := s.CreateInterrupt(ctx, in); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("duplicate interrupt id must conflict, got %v", err)
	}
	pending, _ := s.ListInterrupts(ctx, domain.InterruptPending, 0)
	if len(pending) != 1 || pending[0].AgentName != "hitl" {
		t.Fatalf("bad pending list %+v", pending)
	}
	got, err := s.ResolveInterrupt(ctx, "i1", domain.InterruptApproved, json.RawMessage(`{"note":"lgtm"}`), "user:x", now())
	must(t, err)
	if got.Status != domain.InterruptApproved || got.ResolvedBy != "user:x" || got.ResolvedAt == nil {
		t.Fatalf("bad resolution %+v", got)
	}
	if _, err := s.ResolveInterrupt(ctx, "i1", domain.InterruptRejected, nil, "user:y", now()); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("second resolution must conflict, got %v", err)
	}
	if _, err := s.ResolveInterrupt(ctx, "nope", domain.InterruptRejected, nil, "u", now()); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func testAudit(t *testing.T, s store.Store) {
	for i := range 5 {
		must(t, s.AppendAudit(ctx, &domain.AuditEntry{Actor: "u", Action: "a", TargetID: string(rune('a' + i)), CreatedAt: now()}))
	}
	page, err := s.ListAudit(ctx, 2, 0)
	must(t, err)
	if len(page) != 2 || page[0].ID <= page[1].ID {
		t.Fatalf("want newest first, got %+v", page)
	}
	rest, _ := s.ListAudit(ctx, 10, page[1].ID)
	if len(rest) != 3 {
		t.Fatalf("want 3 older entries, got %d", len(rest))
	}
}
