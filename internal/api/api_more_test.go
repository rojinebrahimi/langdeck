package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/rojinebrahimi/langdeck/internal/api"
	"github.com/rojinebrahimi/langdeck/internal/auth"
	"github.com/rojinebrahimi/langdeck/internal/config"
	"github.com/rojinebrahimi/langdeck/internal/domain"
	"github.com/rojinebrahimi/langdeck/internal/events"
	"github.com/rojinebrahimi/langdeck/internal/store/memory"
	"github.com/rojinebrahimi/langdeck/internal/testutil/oidcfake"
)

// ---- SSO through the real routes ----

type ssoEnv struct {
	srv *httptest.Server
	idp *oidcfake.Provider
}

func newSSOEnv(t *testing.T, mutate func(*config.Config)) *ssoEnv {
	t.Helper()
	idp := oidcfake.New(t, "langdeck")
	st := memory.New()
	cfg := &config.Config{
		Env: "development", AuthMode: "oidc", SessionTTL: time.Hour, LoginRateLimit: 1000, APIRateLimit: 1000,
		MaxBodyBytes: 1 << 20, DefaultRole: "viewer", OIDCGroupsClaim: "groups",
		AdminGroups: []string{"platform"}, OperatorGroups: []string{"sre"},
	}
	if mutate != nil {
		mutate(cfg)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	a := &api.API{Store: st, Broker: events.NewLocal(), Cfg: cfg, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Auth: &auth.Authenticator{Store: st, Now: func() time.Time { return time.Now().UTC() }}}
	srv := httptest.NewUnstartedServer(nil)
	srv.Start()
	t.Cleanup(srv.Close)
	cfg.PublicURL = srv.URL
	var err error
	a.OIDC, err = auth.NewOIDC(ctx, idp.URL, "langdeck", "s", srv.URL, []string{"openid"}, "groups", false)
	if err != nil {
		t.Fatal(err)
	}
	srv.Config.Handler = a.Handler(ctx)
	return &ssoEnv{srv: srv, idp: idp}
}

// login drives the browser through /auth/login -> IdP -> /auth/callback and
// returns the browser plus where the callback redirected it.
func (s *ssoEnv) login(t *testing.T, id oidcfake.Identity) (*http.Client, string) {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := c.Get(s.srv.URL + "/auth/login")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	code, state := s.idp.Authorize(t, resp.Header.Get("Location"), id)
	resp, err = c.Get(s.srv.URL + "/auth/callback?" + url.Values{"code": {code}, "state": {state}}.Encode())
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return c, resp.Header.Get("Location")
}

func me(t *testing.T, c *http.Client, base string) (int, map[string]any) {
	t.Helper()
	resp, err := c.Get(base + "/api/v1/me")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func TestSSOMapsGroupsToRoles(t *testing.T) {
	s := newSSOEnv(t, nil)
	cases := []struct {
		groups []string
		want   domain.Role
	}{
		{[]string{"platform"}, domain.RoleAdmin},
		{[]string{"sre", "eng"}, domain.RoleOperator},
		{[]string{"sales"}, domain.RoleViewer}, // default role
	}
	for i, c := range cases {
		browser, loc := s.login(t, oidcfake.Identity{Subject: fmt.Sprintf("u%d", i), Email: fmt.Sprintf("u%d@corp.com", i), Groups: c.groups})
		if loc != "/" {
			t.Fatalf("successful login must land on /, got %q", loc)
		}
		code, body := me(t, browser, s.srv.URL)
		if code != 200 || body["role"] != string(c.want) {
			t.Fatalf("groups %v: want role %s, got %d %v", c.groups, c.want, code, body["role"])
		}
	}
}

func TestSSORoleFollowsIdPOnEveryLogin(t *testing.T) {
	s := newSSOEnv(t, nil)
	s.login(t, oidcfake.Identity{Subject: "same", Groups: []string{"platform"}})
	browser, _ := s.login(t, oidcfake.Identity{Subject: "same", Groups: []string{"sre"}})
	if _, body := me(t, browser, s.srv.URL); body["role"] != "operator" {
		t.Fatalf("removing someone from the admin group must demote them at next login, got %v", body["role"])
	}
}

func TestSSODomainAllowList(t *testing.T) {
	s := newSSOEnv(t, func(c *config.Config) { c.AllowedEmailDomains = []string{"corp.com"} })
	browser, loc := s.login(t, oidcfake.Identity{Subject: "x", Email: "eve@evil.io"})
	if !strings.Contains(loc, "error=not_allowed") {
		t.Fatalf("outside domain must be rejected, got %q", loc)
	}
	if code, _ := me(t, browser, s.srv.URL); code != http.StatusUnauthorized {
		t.Fatalf("rejected login must not create a session, got %d", code)
	}
}

func TestSSOBootstrapAdminNeedsVerifiedEmail(t *testing.T) {
	s := newSSOEnv(t, func(c *config.Config) { c.BootstrapAdmins = []string{"root@corp.com"} })
	b1, _ := s.login(t, oidcfake.Identity{Subject: "a", Email: "root@corp.com", EmailVerified: false})
	if _, body := me(t, b1, s.srv.URL); body["role"] == "admin" {
		t.Fatal("an unverified email must not grant bootstrap admin")
	}
	b2, _ := s.login(t, oidcfake.Identity{Subject: "b", Email: "root@corp.com", EmailVerified: true})
	if _, body := me(t, b2, s.srv.URL); body["role"] != "admin" {
		t.Fatalf("verified bootstrap email must be admin, got %v", body["role"])
	}
}

func TestSSOFailedCallbackRedirectsWithError(t *testing.T) {
	s := newSSOEnv(t, nil)
	c := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := c.Get(s.srv.URL + "/auth/callback?code=x&state=y")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if loc := resp.Header.Get("Location"); loc != "/login?error=login_failed" {
		t.Fatalf("got %q", loc)
	}
}

func TestSSOModeDisablesOtherLogins(t *testing.T) {
	s := newSSOEnv(t, nil)
	for _, p := range []string{"/auth/dev-login", "/auth/password-login"} {
		resp, err := http.Post(s.srv.URL+p, "application/json", strings.NewReader(`{"email":"a@b.c","username":"admin","password":"admin"}`))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("%s must be disabled in oidc mode, got %d", p, resp.StatusCode)
		}
	}
}

// ---- read endpoints, pagination, limits ----

func TestReadEndpoints(t *testing.T) {
	e := newEnv(t)
	admin := e.browser("admin@example.com")
	agent := e.bearer(admin.newKey("agent", domain.RoleAgent, ""))
	var reg struct {
		Agent domain.Agent `json:"agent"`
	}
	agent.do("POST", "/api/v1/agent/register", map[string]any{"name": "reader", "graph": map[string]any{"nodes": []any{}, "edges": []any{}}}, http.StatusOK, &reg)
	id := reg.Agent.ID

	base := time.Now().UTC().Add(-time.Hour)
	for i := range 7 {
		ts := base.Add(time.Duration(i) * time.Minute)
		status := "run_end"
		if i%3 == 0 {
			status = "run_error"
		}
		agent.do("POST", "/api/v1/agent/events", map[string]any{"agent": "reader", "events": []map[string]any{
			{"run_id": fmt.Sprintf("r-%d", i), "thread_id": "t-1", "type": "run_start", "ts": ts},
			{"run_id": fmt.Sprintf("r-%d", i), "type": status, "ts": ts.Add(time.Second)},
		}}, http.StatusAccepted, nil)
	}

	// Pagination with next_before.
	var page struct {
		Runs []domain.Run `json:"runs"`
		Next *string      `json:"next_before"`
	}
	admin.do("GET", "/api/v1/runs?limit=3&agent_id="+id, nil, http.StatusOK, &page)
	if len(page.Runs) != 3 || page.Next == nil || page.Runs[0].ID != "r-6" {
		t.Fatalf("first page: %d runs, next=%v", len(page.Runs), page.Next)
	}
	seen := map[string]bool{}
	for _, r := range page.Runs {
		seen[r.ID] = true
		if r.Input != nil {
			t.Fatal("list rows must not include payloads")
		}
	}
	for page.Next != nil {
		admin.do("GET", "/api/v1/runs?limit=3&agent_id="+id+"&before="+url.QueryEscape(*page.Next), nil, http.StatusOK, &page)
		for _, r := range page.Runs {
			if seen[r.ID] {
				t.Fatalf("run %s returned twice", r.ID)
			}
			seen[r.ID] = true
		}
	}
	if len(seen) != 7 {
		t.Fatalf("paging must visit all 7 runs, saw %d", len(seen))
	}
	admin.do("GET", "/api/v1/runs?before=yesterday", nil, http.StatusBadRequest, nil)

	var failed struct {
		Runs []domain.Run `json:"runs"`
	}
	admin.do("GET", "/api/v1/runs?status=failed", nil, http.StatusOK, &failed)
	if len(failed.Runs) != 3 {
		t.Fatalf("status filter: want 3 failed, got %d", len(failed.Runs))
	}
	admin.do("GET", "/api/v1/runs?thread_id=t-1", nil, http.StatusOK, &failed)
	if len(failed.Runs) != 7 {
		t.Fatalf("thread filter: want 7, got %d", len(failed.Runs))
	}

	var detail struct {
		Agent  domain.Agent        `json:"agent"`
		Config *domain.AgentConfig `json:"config"`
	}
	admin.do("GET", "/api/v1/agents/"+id, nil, http.StatusOK, &detail)
	if detail.Agent.Name != "reader" || detail.Config != nil || len(detail.Agent.Graph) == 0 {
		t.Fatalf("bad agent detail %+v", detail)
	}
	var list struct {
		Agents []domain.Agent `json:"agents"`
	}
	admin.do("GET", "/api/v1/agents", nil, http.StatusOK, &list)
	if len(list.Agents) != 1 || list.Agents[0].Graph != nil || list.Agents[0].Status != domain.AgentOnline {
		t.Fatalf("agent list must be light and show status: %+v", list.Agents)
	}
	admin.do("GET", "/api/v1/agents/does-not-exist", nil, http.StatusNotFound, nil)
	admin.do("GET", "/api/v1/runs/nope", nil, http.StatusNotFound, nil)

	admin.do("POST", "/api/v1/agents/"+id+"/configs", map[string]any{"config": []int{1}}, http.StatusBadRequest, nil)
	admin.do("POST", "/api/v1/agents/"+id+"/configs", map[string]any{"config": map[string]int{"v": 1}}, http.StatusCreated, nil)
	admin.do("POST", "/api/v1/agents/"+id+"/configs", map[string]any{"config": map[string]int{"v": 2}}, http.StatusCreated, nil)
	var cfgs struct {
		Configs []domain.AgentConfig `json:"configs"`
	}
	admin.do("GET", "/api/v1/agents/"+id+"/configs", nil, http.StatusOK, &cfgs)
	if len(cfgs.Configs) != 2 || cfgs.Configs[0].Version != 2 {
		t.Fatalf("configs newest first: %+v", cfgs.Configs)
	}
	var cmds struct {
		Commands []domain.Command `json:"commands"`
	}
	admin.do("GET", "/api/v1/agents/"+id+"/commands", nil, http.StatusOK, &cmds)
	if len(cmds.Commands) != 2 || cmds.Commands[0].Type != domain.CmdUpdateConfig {
		t.Fatalf("commands: %+v", cmds.Commands)
	}

	admin.do("POST", "/api/v1/agents/"+id+"/pause", nil, http.StatusOK, nil)
	var resumed domain.Agent
	admin.do("POST", "/api/v1/agents/"+id+"/resume", nil, http.StatusOK, &resumed)
	if resumed.DesiredState != domain.DesiredRunning {
		t.Fatalf("resume: %+v", resumed)
	}

	var keys struct {
		Keys []domain.APIKey `json:"api_keys"`
	}
	admin.do("GET", "/api/v1/api-keys", nil, http.StatusOK, &keys)
	if len(keys.Keys) != 1 || keys.Keys[0].Prefix == "" {
		t.Fatalf("keys: %+v", keys.Keys)
	}
	raw, _ := json.Marshal(keys)
	if strings.Contains(string(raw), "hash") {
		t.Fatal("key hashes must never be serialized")
	}

	var audit struct {
		Entries []domain.AuditEntry `json:"entries"`
	}
	admin.do("GET", "/api/v1/audit", nil, http.StatusOK, &audit)
	actions := map[string]bool{}
	for _, a := range audit.Entries {
		actions[a.Action] = true
	}
	for _, want := range []string{"api_key.create", "config.update", "agent.pause", "agent.resume"} {
		if !actions[want] {
			t.Errorf("audit log is missing %s", want)
		}
	}

	var ints struct {
		Interrupts []domain.Interrupt `json:"interrupts"`
	}
	admin.do("GET", "/api/v1/interrupts?status=pending", nil, http.StatusOK, &ints)
	admin.do("POST", "/api/v1/interrupts/nope/resolve", map[string]string{"decision": "approve"}, http.StatusNotFound, nil)
	admin.do("POST", "/api/v1/interrupts/nope/resolve", map[string]string{"decision": "maybe"}, http.StatusBadRequest, nil)
}

func TestHealthReadyAndAuthInfo(t *testing.T) {
	e := newEnv(t)
	for path, want := range map[string]int{"/healthz": 200, "/readyz": 200, "/api/v1/auth/info": 200, "/auth/login": 404} {
		resp, err := http.Get(e.srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != want {
			t.Errorf("%s: want %d, got %d", path, want, resp.StatusCode)
		}
	}
}

func TestBodyLimitAndBadJSON(t *testing.T) {
	e := newEnv(t)
	admin := e.browser("admin@example.com")
	err := admin.call("POST", "/api/v1/api-keys", map[string]string{"name": strings.Repeat("x", 2<<20)}, http.StatusBadRequest, nil)
	if err != nil {
		t.Fatalf("oversized body must be a 400: %v", err)
	}
	req, _ := http.NewRequest("POST", e.srv.URL+"/api/v1/api-keys", strings.NewReader("{not json"))
	req.Header.Set(auth.CSRFHeader, admin.csrf)
	resp, err := admin.http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("malformed JSON: want 400, got %d", resp.StatusCode)
	}
}

func TestOversizedPayloadIsCapped(t *testing.T) {
	e := newEnv(t)
	admin := e.browser("admin@example.com")
	agent := e.bearer(admin.newKey("agent", domain.RoleAgent, ""))
	agent.do("POST", "/api/v1/agent/register", map[string]any{"name": "big"}, http.StatusOK, nil)
	agent.do("POST", "/api/v1/agent/events", map[string]any{"agent": "big", "events": []map[string]any{
		{"run_id": "r", "type": "node_end", "node": "n", "payload": map[string]string{"blob": strings.Repeat("a", 100<<10)}},
	}}, http.StatusAccepted, nil)
	var evs struct {
		Events []domain.RunEvent `json:"events"`
	}
	admin.do("GET", "/api/v1/runs/r/events", nil, http.StatusOK, &evs)
	if len(evs.Events) != 1 || !strings.Contains(string(evs.Events[0].Payload), "_truncated") {
		t.Fatalf("oversized payloads must be replaced by a marker: %.80s", evs.Events[0].Payload)
	}
}

func TestEventsForFutureClockAreClamped(t *testing.T) {
	e := newEnv(t)
	admin := e.browser("admin@example.com")
	agent := e.bearer(admin.newKey("agent", domain.RoleAgent, ""))
	agent.do("POST", "/api/v1/agent/register", map[string]any{"name": "clock"}, http.StatusOK, nil)
	future := time.Now().Add(48 * time.Hour)
	agent.do("POST", "/api/v1/agent/events", map[string]any{"agent": "clock", "events": []map[string]any{
		{"run_id": "r", "type": "run_start", "ts": future},
	}}, http.StatusAccepted, nil)
	var run domain.Run
	admin.do("GET", "/api/v1/runs/r", nil, http.StatusOK, &run)
	if run.CreatedAt.After(time.Now().Add(time.Minute)) {
		t.Fatalf("a skewed agent clock must not create runs in the future: %s", run.CreatedAt)
	}
}

func TestUnknownAgentIsNotFound(t *testing.T) {
	e := newEnv(t)
	admin := e.browser("admin@example.com")
	agent := e.bearer(admin.newKey("agent", domain.RoleAgent, ""))
	err := agent.call("POST", "/api/v1/agent/heartbeat", map[string]any{"agent": "ghost"}, http.StatusNotFound, nil)
	if err != nil {
		t.Fatal(err)
	}
	agent.do("GET", "/api/v1/agent/commands?agent=ghost", nil, http.StatusNotFound, nil)
}
