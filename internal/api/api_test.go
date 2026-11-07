package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rojinebrahimi/langdeck/internal/api"
	"github.com/rojinebrahimi/langdeck/internal/auth"
	"github.com/rojinebrahimi/langdeck/internal/config"
	"github.com/rojinebrahimi/langdeck/internal/domain"
	"github.com/rojinebrahimi/langdeck/internal/events"
	"github.com/rojinebrahimi/langdeck/internal/notify"
	"github.com/rojinebrahimi/langdeck/internal/store/memory"
)

type env struct {
	t     *testing.T
	srv   *httptest.Server
	store *memory.Store
}

func newEnv(t *testing.T) *env { return newEnvMode(t, "dev") }

func newEnvMode(t *testing.T, mode string) *env { return newEnvNotify(t, mode, nil) }

func newEnvNotify(t *testing.T, mode string, n *notify.Dispatcher) *env {
	t.Helper()
	return newEnvWith(t, mode, n, nil)
}

func newEnvWith(t *testing.T, mode string, n *notify.Dispatcher, tweak func(*config.Config)) *env {
	t.Helper()
	st := memory.New()
	cfg := &config.Config{
		Env: "development", AuthMode: mode, PublicURL: "http://localhost", SessionTTL: time.Hour,
		LoginRateLimit: 1000, APIRateLimit: 1000, MaxBodyBytes: 1 << 20, DefaultRole: "viewer",
		AdminUsername: "admin", AdminPassword: "admin", ForcePasswordChange: true,
	}
	if tweak != nil {
		tweak(cfg)
	}
	if mode == "local" {
		if _, err := api.EnsureLocalAdmin(context.Background(), st, "admin", "admin", true); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	a := &api.API{
		Store: st, Broker: events.NewLocal(), Cfg: cfg,
		Log:          slog.New(slog.NewTextHandler(io.Discard, nil)),
		Auth:         &auth.Authenticator{Store: st, Now: func() time.Time { return time.Now().UTC() }},
		PollInterval: 20 * time.Millisecond,
		Notify:       n,
	}
	if n != nil {
		go n.Run(ctx)
	}
	srv := httptest.NewServer(a.Handler(ctx))
	t.Cleanup(srv.Close)
	return &env{t: t, srv: srv, store: st}
}

// client is either a browser (cookie jar + CSRF token) or a bearer-token caller.
type client struct {
	e     *env
	http  *http.Client
	token string
	csrf  string
}

func (e *env) browser(email string) *client {
	jar, _ := cookiejar.New(nil)
	c := &client{e: e, http: &http.Client{Jar: jar}}
	c.do("POST", "/auth/dev-login", map[string]string{"email": email}, http.StatusOK, nil)
	var me struct {
		CSRF string `json:"csrf_token"`
	}
	c.do("GET", "/api/v1/me", nil, http.StatusOK, &me)
	c.csrf = me.CSRF
	return c
}

func (e *env) bearer(token string) *client {
	return &client{e: e, http: http.DefaultClient, token: token}
}

func (c *client) do(method, path string, body any, want int, out any) {
	c.e.t.Helper()
	if err := c.call(method, path, body, want, out); err != nil {
		c.e.t.Fatal(err)
	}
}

// call is the goroutine-safe form of do: it reports failures instead of calling t.Fatal.
func (c *client) call(method, path string, body any, want int, out any) error {
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.e.srv.URL+path, r)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if c.csrf != "" {
		req.Header.Set(auth.CSRFHeader, c.csrf)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != want {
		return fmt.Errorf("%s %s: want %d, got %d: %s", method, path, want, resp.StatusCode, raw)
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("decode %s: %w (%s)", path, err, raw)
		}
	}
	return nil
}

func (c *client) newKey(name string, role domain.Role, agent string) string {
	var out struct {
		Secret string `json:"secret"`
	}
	c.do("POST", "/api/v1/api-keys", map[string]any{"name": name, "role": role, "agent_name": agent}, http.StatusCreated, &out)
	return out.Secret
}

func TestHealthAndSecurityHeaders(t *testing.T) {
	e := newEnv(t)
	resp, err := http.Get(e.srv.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("healthz: %d", resp.StatusCode)
	}
	for _, h := range []string{"Content-Security-Policy", "X-Frame-Options", "X-Content-Type-Options", "X-Request-ID"} {
		if resp.Header.Get(h) == "" {
			t.Errorf("missing header %s", h)
		}
	}
}

func TestManagementAPIRequiresAuth(t *testing.T) {
	e := newEnv(t)
	anon := &client{e: e, http: http.DefaultClient}
	anon.do("GET", "/api/v1/agents", nil, http.StatusUnauthorized, nil)
	e.bearer("ldk_not-a-real-key").do("GET", "/api/v1/agents", nil, http.StatusUnauthorized, nil)
	e.bearer("something-else").do("GET", "/api/v1/agents", nil, http.StatusUnauthorized, nil)
}

func TestCSRFIsEnforcedForCookieSessions(t *testing.T) {
	e := newEnv(t)
	admin := e.browser("admin@example.com")
	admin.csrf = ""
	admin.do("POST", "/api/v1/api-keys", map[string]any{"name": "x", "role": "agent"}, http.StatusForbidden, nil)
	admin.csrf = "wrong"
	admin.do("POST", "/api/v1/api-keys", map[string]any{"name": "x", "role": "agent"}, http.StatusForbidden, nil)
}

func TestRBAC(t *testing.T) {
	e := newEnv(t)
	admin := e.browser("admin@example.com")

	viewer := e.bearer(admin.newKey("viewer", domain.RoleViewer, ""))
	operator := e.bearer(admin.newKey("operator", domain.RoleOperator, ""))
	agentKey := admin.newKey("agent", domain.RoleAgent, "")
	agent := e.bearer(agentKey)

	var reg struct {
		Agent domain.Agent `json:"agent"`
	}
	agent.do("POST", "/api/v1/agent/register", map[string]any{"name": "support-bot"}, http.StatusOK, &reg)
	id := reg.Agent.ID

	viewer.do("GET", "/api/v1/agents", nil, http.StatusOK, nil)
	viewer.do("POST", "/api/v1/agents/"+id+"/pause", nil, http.StatusForbidden, nil)
	operator.do("POST", "/api/v1/agents/"+id+"/pause", nil, http.StatusOK, nil)
	operator.do("GET", "/api/v1/audit", nil, http.StatusForbidden, nil)
	operator.do("DELETE", "/api/v1/agents/"+id, nil, http.StatusForbidden, nil)

	// Agent keys cannot use the management API, and human keys cannot use the agent API.
	agent.do("GET", "/api/v1/agents", nil, http.StatusForbidden, nil)
	operator.do("POST", "/api/v1/agent/register", map[string]any{"name": "x"}, http.StatusForbidden, nil)

	admin.do("GET", "/api/v1/audit", nil, http.StatusOK, nil)
	admin.do("DELETE", "/api/v1/agents/"+id, nil, http.StatusNoContent, nil)
}

func TestAgentKeyBoundToOneAgent(t *testing.T) {
	e := newEnv(t)
	admin := e.browser("admin@example.com")
	bound := e.bearer(admin.newKey("bound", domain.RoleAgent, "alpha"))
	bound.do("POST", "/api/v1/agent/register", map[string]any{"name": "alpha"}, http.StatusOK, nil)
	bound.do("POST", "/api/v1/agent/register", map[string]any{"name": "beta"}, http.StatusForbidden, nil)

	other := e.bearer(admin.newKey("other", domain.RoleAgent, ""))
	other.do("POST", "/api/v1/agent/register", map[string]any{"name": "beta"}, http.StatusOK, nil)
	bound.do("GET", "/api/v1/agent/commands?agent=beta", nil, http.StatusForbidden, nil)
}

func TestRevokedKeyStopsWorking(t *testing.T) {
	e := newEnv(t)
	admin := e.browser("admin@example.com")
	var created struct {
		Secret string        `json:"secret"`
		Key    domain.APIKey `json:"api_key"`
	}
	admin.do("POST", "/api/v1/api-keys", map[string]any{"name": "tmp", "role": "viewer"}, http.StatusCreated, &created)
	e.bearer(created.Secret).do("GET", "/api/v1/agents", nil, http.StatusOK, nil)
	admin.do("DELETE", "/api/v1/api-keys/"+created.Key.ID, nil, http.StatusNoContent, nil)
	e.bearer(created.Secret).do("GET", "/api/v1/agents", nil, http.StatusUnauthorized, nil)
}

func TestInputValidation(t *testing.T) {
	e := newEnv(t)
	admin := e.browser("admin@example.com")
	agent := e.bearer(admin.newKey("a", domain.RoleAgent, ""))
	agent.do("POST", "/api/v1/agent/register", map[string]any{"name": "Bad Name!"}, http.StatusBadRequest, nil)
	agent.do("POST", "/api/v1/agent/register", map[string]any{"name": "ok"}, http.StatusOK, nil)
	agent.do("POST", "/api/v1/agent/events", map[string]any{"agent": "ok", "events": []any{}}, http.StatusBadRequest, nil)
	agent.do("POST", "/api/v1/agent/events", map[string]any{"agent": "ok", "events": []map[string]any{
		{"run_id": "../../etc", "type": "node_start"}}}, http.StatusBadRequest, nil)
	agent.do("POST", "/api/v1/agent/events", map[string]any{"agent": "ok", "events": []map[string]any{
		{"run_id": "r1", "type": "made_up"}}}, http.StatusBadRequest, nil)
	admin.do("POST", "/api/v1/api-keys", map[string]any{"name": "x", "role": "root"}, http.StatusBadRequest, nil)
}

// TestRunLifecycle walks the main product flow end to end:
// UI starts a run -> agent receives the command -> agent streams events ->
// agent asks a human -> operator approves -> run succeeds.
func TestRunLifecycle(t *testing.T) {
	e := newEnv(t)
	admin := e.browser("admin@example.com")
	agent := e.bearer(admin.newKey("agent", domain.RoleAgent, "researcher"))

	var reg struct {
		Agent domain.Agent `json:"agent"`
	}
	agent.do("POST", "/api/v1/agent/register", map[string]any{
		"name": "researcher", "version": "1.0.0",
		"graph": map[string]any{"nodes": []map[string]string{{"id": "plan"}, {"id": "act"}}, "edges": []any{}},
	}, http.StatusOK, &reg)
	agentID := reg.Agent.ID

	// Push a config; it should arrive as a command.
	admin.do("POST", "/api/v1/agents/"+agentID+"/configs", map[string]any{"config": map[string]any{"model": "gpt-x"}, "comment": "first"}, http.StatusCreated, nil)

	var started struct {
		Run domain.Run `json:"run"`
	}
	admin.do("POST", "/api/v1/agents/"+agentID+"/runs", map[string]any{"input": map[string]string{"q": "hi"}}, http.StatusAccepted, &started)
	runID := started.Run.ID
	if started.Run.Status != domain.RunPending {
		t.Fatalf("new UI run must be pending, got %s", started.Run.Status)
	}

	var polled struct {
		Commands []domain.Command `json:"commands"`
	}
	agent.do("GET", "/api/v1/agent/commands?agent=researcher&wait=1s", nil, http.StatusOK, &polled)
	if len(polled.Commands) != 2 || polled.Commands[0].Type != domain.CmdUpdateConfig || polled.Commands[1].Type != domain.CmdStartRun {
		t.Fatalf("want [update_config start_run], got %+v", polled.Commands)
	}
	for _, c := range polled.Commands {
		agent.do("POST", "/api/v1/agent/commands/"+c.ID+"/ack", map[string]any{"agent": "researcher", "ok": true}, http.StatusNoContent, nil)
	}

	now := time.Now().UTC()
	agent.do("POST", "/api/v1/agent/events", map[string]any{"agent": "researcher", "events": []map[string]any{
		{"run_id": runID, "type": "run_start", "ts": now, "payload": map[string]any{"input": map[string]string{"q": "hi"}}},
		{"run_id": runID, "type": "node_start", "node": "plan", "ts": now},
		{"run_id": runID, "type": "llm_end", "node": "plan", "ts": now, "payload": map[string]any{"usage": map[string]int{"prompt_tokens": 10, "completion_tokens": 5}}},
		{"run_id": runID, "type": "node_end", "node": "plan", "ts": now, "latency_ms": 12},
	}}, http.StatusAccepted, nil)

	// Human in the loop.
	agent.do("POST", "/api/v1/agent/interrupts", map[string]any{"agent": "researcher", "id": "int-1", "run_id": runID,
		"node": "act", "question": "Send the email?", "payload": map[string]string{"to": "x@y.z"}}, http.StatusCreated, nil)
	var run domain.Run
	admin.do("GET", "/api/v1/runs/"+runID, nil, http.StatusOK, &run)
	if run.Status != domain.RunInterrupted {
		t.Fatalf("want interrupted, got %s", run.Status)
	}

	// The agent long-polls for the decision while an operator approves it.
	done := make(chan domain.Interrupt, 1)
	go func() {
		var in domain.Interrupt
		if err := agent.call("GET", "/api/v1/agent/interrupts/int-1?wait=5s", nil, http.StatusOK, &in); err != nil {
			in.Status = domain.InterruptStatus(err.Error())
		}
		done <- in
	}()
	time.Sleep(50 * time.Millisecond)
	admin.do("POST", "/api/v1/interrupts/int-1/resolve", map[string]any{"decision": "approve", "response": map[string]string{"note": "go"}}, http.StatusOK, nil)
	select {
	case in := <-done:
		if in.Status != domain.InterruptApproved {
			t.Fatalf("agent saw %s", in.Status)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("agent long-poll did not return after approval")
	}
	admin.do("POST", "/api/v1/interrupts/int-1/resolve", map[string]any{"decision": "reject"}, http.StatusConflict, nil)

	agent.do("POST", "/api/v1/agent/events", map[string]any{"agent": "researcher", "events": []map[string]any{
		{"run_id": runID, "type": "run_end", "ts": time.Now().UTC(), "payload": map[string]any{"output": map[string]string{"answer": "42"}}},
	}}, http.StatusAccepted, nil)

	admin.do("GET", "/api/v1/runs/"+runID, nil, http.StatusOK, &run)
	if run.Status != domain.RunSucceeded || run.PromptTokens != 10 || run.CompletionTokens != 5 || !strings.Contains(string(run.Output), "42") {
		t.Fatalf("bad final run %+v", run)
	}
	var evs struct {
		Events []domain.RunEvent `json:"events"`
	}
	admin.do("GET", "/api/v1/runs/"+runID+"/events", nil, http.StatusOK, &evs)
	if len(evs.Events) < 7 {
		t.Fatalf("want full trace, got %d events", len(evs.Events))
	}
	admin.do("POST", "/api/v1/runs/"+runID+"/cancel", nil, http.StatusConflict, nil)

	var stats domain.Stats
	admin.do("GET", "/api/v1/stats", nil, http.StatusOK, &stats)
	if stats.AgentsTotal != 1 || stats.RunsLast24h != 1 || stats.RunsByStatus[domain.RunSucceeded] != 1 {
		t.Fatalf("bad stats %+v", stats)
	}
}

func TestPausedAgentRejectsNewRuns(t *testing.T) {
	e := newEnv(t)
	admin := e.browser("admin@example.com")
	agent := e.bearer(admin.newKey("agent", domain.RoleAgent, ""))
	var reg struct {
		Agent domain.Agent `json:"agent"`
	}
	agent.do("POST", "/api/v1/agent/register", map[string]any{"name": "p"}, http.StatusOK, &reg)
	admin.do("POST", "/api/v1/agents/"+reg.Agent.ID+"/pause", nil, http.StatusOK, nil)
	admin.do("POST", "/api/v1/agents/"+reg.Agent.ID+"/runs", map[string]any{}, http.StatusConflict, nil)

	var hb struct {
		Desired domain.DesiredState `json:"desired_state"`
	}
	agent.do("POST", "/api/v1/agent/heartbeat", map[string]any{"agent": "p"}, http.StatusOK, &hb)
	if hb.Desired != domain.DesiredPaused {
		t.Fatalf("heartbeat must report paused, got %s", hb.Desired)
	}
}

func TestCancelPendingRunIsImmediate(t *testing.T) {
	e := newEnv(t)
	admin := e.browser("admin@example.com")
	agent := e.bearer(admin.newKey("agent", domain.RoleAgent, ""))
	var reg struct {
		Agent domain.Agent `json:"agent"`
	}
	agent.do("POST", "/api/v1/agent/register", map[string]any{"name": "c"}, http.StatusOK, &reg)
	var started struct {
		Run domain.Run `json:"run"`
	}
	admin.do("POST", "/api/v1/agents/"+reg.Agent.ID+"/runs", map[string]any{}, http.StatusAccepted, &started)
	var run domain.Run
	admin.do("POST", "/api/v1/runs/"+started.Run.ID+"/cancel", nil, http.StatusAccepted, &run)
	if run.Status != domain.RunCancelled {
		t.Fatalf("want cancelled, got %s", run.Status)
	}
}

func TestLongPollWakesOnNewCommand(t *testing.T) {
	e := newEnv(t)
	admin := e.browser("admin@example.com")
	agent := e.bearer(admin.newKey("agent", domain.RoleAgent, ""))
	var reg struct {
		Agent domain.Agent `json:"agent"`
	}
	agent.do("POST", "/api/v1/agent/register", map[string]any{"name": "w"}, http.StatusOK, &reg)

	got := make(chan int, 1)
	go func() {
		var polled struct {
			Commands []domain.Command `json:"commands"`
		}
		if err := agent.call("GET", "/api/v1/agent/commands?agent=w&wait=10s", nil, http.StatusOK, &polled); err != nil {
			got <- -1
			return
		}
		got <- len(polled.Commands)
	}()
	time.Sleep(50 * time.Millisecond)
	start := time.Now()
	admin.do("POST", "/api/v1/agents/"+reg.Agent.ID+"/pause", nil, http.StatusOK, nil)
	select {
	case n := <-got:
		if n != 1 {
			t.Fatalf("want 1 command, got %d", n)
		}
		if time.Since(start) > 2*time.Second {
			t.Fatal("long-poll should wake promptly on a new command")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("long-poll never returned")
	}
}

func TestUserRoleManagement(t *testing.T) {
	e := newEnv(t)
	admin := e.browser("admin@example.com")
	e.browser("bob@example.com")
	var users struct {
		Users []domain.User `json:"users"`
	}
	admin.do("GET", "/api/v1/users", nil, http.StatusOK, &users)
	var me, bob domain.User
	for _, u := range users.Users {
		if u.Email == "admin@example.com" {
			me = u
		} else {
			bob = u
		}
	}
	admin.do("PATCH", "/api/v1/users/"+bob.ID, map[string]string{"role": "viewer"}, http.StatusOK, nil)
	admin.do("PATCH", "/api/v1/users/"+me.ID, map[string]string{"role": "viewer"}, http.StatusBadRequest, nil)
	admin.do("PATCH", "/api/v1/users/"+bob.ID, map[string]string{"role": "agent"}, http.StatusBadRequest, nil)
}

func TestLogoutEndsSession(t *testing.T) {
	e := newEnv(t)
	u := e.browser("u@example.com")
	u.do("POST", "/auth/logout", nil, http.StatusNoContent, nil)
	u.do("GET", "/api/v1/me", nil, http.StatusUnauthorized, nil)
}

func TestStreamSendsEvents(t *testing.T) {
	e := newEnv(t)
	admin := e.browser("admin@example.com")
	agentKey := admin.newKey("agent", domain.RoleAgent, "")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", e.srv.URL+"/api/v1/stream", nil)
	resp, err := admin.http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content type %q", ct)
	}
	go func() { // no t.Fatal off the test goroutine: the stream assertion below catches failures
		req, _ := http.NewRequest("POST", e.srv.URL+"/api/v1/agent/register", strings.NewReader(`{"name":"s"}`))
		req.Header.Set("Authorization", "Bearer "+agentKey)
		if resp, err := http.DefaultClient.Do(req); err == nil {
			resp.Body.Close()
		}
	}()

	buf := make([]byte, 4096)
	var seen strings.Builder
	for !strings.Contains(seen.String(), "event: agent.updated") {
		n, err := resp.Body.Read(buf)
		if err != nil {
			t.Fatalf("stream ended before event: %v (%q)", err, seen.String())
		}
		seen.Write(buf[:n])
	}
}

func (e *env) passwordLogin(user, pass string, want int) *client {
	jar, _ := cookiejar.New(nil)
	c := &client{e: e, http: &http.Client{Jar: jar}}
	c.do("POST", "/auth/password-login", map[string]string{"username": user, "password": pass}, want, nil)
	if want == http.StatusOK {
		var me struct {
			CSRF string `json:"csrf_token"`
		}
		c.do("GET", "/api/v1/me", nil, http.StatusOK, &me)
		c.csrf = me.CSRF
	}
	return c
}

func TestDefaultAdminMustChangePassword(t *testing.T) {
	e := newEnvMode(t, "local")
	e.passwordLogin("admin", "wrong", http.StatusUnauthorized)
	e.passwordLogin("ghost", "admin", http.StatusUnauthorized)
	e.newEnvDevLoginDisabled()

	admin := e.passwordLogin("ADMIN", "admin", http.StatusOK)
	var me struct {
		Must bool        `json:"must_change_password"`
		Role domain.Role `json:"role"`
	}
	admin.do("GET", "/api/v1/me", nil, http.StatusOK, &me)
	if !me.Must || me.Role != domain.RoleAdmin {
		t.Fatalf("default admin must be forced to change password: %+v", me)
	}
	admin.do("GET", "/api/v1/agents", nil, http.StatusForbidden, nil)
	admin.do("POST", "/api/v1/api-keys", map[string]any{"name": "x", "role": "agent"}, http.StatusForbidden, nil)

	admin.do("POST", "/api/v1/me/password", map[string]string{"current_password": "nope", "new_password": "a-much-better-password"}, http.StatusForbidden, nil)
	admin.do("POST", "/api/v1/me/password", map[string]string{"current_password": "admin", "new_password": "short"}, http.StatusBadRequest, nil)
	admin.do("POST", "/api/v1/me/password", map[string]string{"current_password": "admin", "new_password": "a-much-better-password"}, http.StatusNoContent, nil)
	admin.do("GET", "/api/v1/agents", nil, http.StatusOK, nil)

	e.passwordLogin("admin", "admin", http.StatusUnauthorized)
	e.passwordLogin("admin", "a-much-better-password", http.StatusOK)

	// Admins add users; new users must also pick their own password.
	admin.do("POST", "/api/v1/users", map[string]string{"username": "ops-1", "role": "operator", "password": "temporary-pass-1"}, http.StatusCreated, nil)
	admin.do("POST", "/api/v1/users", map[string]string{"username": "ops-1", "role": "operator", "password": "temporary-pass-1"}, http.StatusConflict, nil)
	admin.do("POST", "/api/v1/users", map[string]string{"username": "x", "role": "operator", "password": "short"}, http.StatusBadRequest, nil)
	ops := e.passwordLogin("ops-1", "temporary-pass-1", http.StatusOK)
	ops.do("GET", "/api/v1/runs", nil, http.StatusForbidden, nil)
}

// newEnvDevLoginDisabled checks that the email-only dev login is off outside dev mode.
func (e *env) newEnvDevLoginDisabled() {
	c := &client{e: e, http: http.DefaultClient}
	c.do("POST", "/auth/dev-login", map[string]string{"email": "a@b.c"}, http.StatusNotFound, nil)
}

func TestPasswordLoginDisabledInDevMode(t *testing.T) {
	e := newEnv(t)
	e.passwordLogin("admin", "admin", http.StatusNotFound)
}
