package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rojinebrahimi/langdeck/internal/domain"
	"github.com/rojinebrahimi/langdeck/internal/notify"
)

// Approvals and failed runs reach the chat webhook; successful runs and payloads do not.
func TestChatNotifications(t *testing.T) {
	texts := make(chan string, 8)
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var m struct {
			Text string `json:"text"`
		}
		_ = json.NewDecoder(r.Body).Decode(&m)
		texts <- m.Text
	}))
	t.Cleanup(hook.Close)

	e := newEnvNotify(t, "dev", notify.New(notify.Options{SlackURL: hook.URL, PublicURL: "http://localhost"}))
	admin := e.browser("admin@example.com")
	agent := e.bearer(admin.newKey("bot", domain.RoleAgent, ""))
	agent.do("POST", "/api/v1/agent/register", map[string]any{"name": "billing"}, http.StatusOK, nil)

	now := time.Now().UTC()
	agent.do("POST", "/api/v1/agent/interrupts", map[string]any{"agent": "billing", "id": "int-9", "run_id": "run-9",
		"node": "refund", "question": "Refund $240?", "payload": map[string]string{"card": "4242-secret"}}, http.StatusCreated, nil)
	approval := next(t, texts)
	for _, want := range []string{"Refund $240?", "billing", "refund", "http://localhost/approvals"} {
		if !strings.Contains(approval, want) {
			t.Fatalf("approval message missing %q:\n%s", want, approval)
		}
	}
	if strings.Contains(approval, "4242-secret") {
		t.Fatal("payloads must never be sent to chat")
	}

	// A successful run is quiet; a failing one notifies exactly once.
	agent.do("POST", "/api/v1/agent/events", map[string]any{"agent": "billing", "events": []map[string]any{
		{"run_id": "ok-1", "type": "run_start", "ts": now},
		{"run_id": "ok-1", "type": "run_end", "ts": now},
		{"run_id": "bad-1", "type": "run_start", "ts": now},
		{"run_id": "bad-1", "type": "run_error", "ts": now, "payload": map[string]string{"error": "TimeoutError: upstream"}},
	}}, http.StatusAccepted, nil)
	failed := next(t, texts)
	if !strings.Contains(failed, "Run failed on billing") || !strings.Contains(failed, "TimeoutError") ||
		!strings.Contains(failed, "/runs/bad-1") {
		t.Fatalf("bad failure message:\n%s", failed)
	}
	agent.do("POST", "/api/v1/agent/events", map[string]any{"agent": "billing", "events": []map[string]any{
		{"run_id": "bad-1", "type": "log", "ts": now},
	}}, http.StatusAccepted, nil)
	select {
	case extra := <-texts:
		t.Fatalf("unexpected extra notification:\n%s", extra)
	case <-time.After(300 * time.Millisecond):
	}
}

func next(t *testing.T, ch <-chan string) string {
	t.Helper()
	select {
	case s := <-ch:
		return s
	case <-time.After(3 * time.Second):
		t.Fatal("no notification delivered")
		return ""
	}
}

// Channels can be configured from the UI: secrets are write-only, changes apply at once,
// and only admins can touch them.
func TestNotifySettingsFromUI(t *testing.T) {
	texts := make(chan string, 8)
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var m struct {
			Text string `json:"text"`
		}
		_ = json.NewDecoder(r.Body).Decode(&m)
		texts <- m.Text
	}))
	t.Cleanup(hook.Close)

	e := newEnvNotify(t, "dev", notify.New(notify.Options{}))
	admin := e.browser("admin@example.com")
	agent := e.bearer(admin.newKey("bot", domain.RoleAgent, ""))
	agent.do("POST", "/api/v1/agent/register", map[string]any{"name": "billing"}, http.StatusOK, nil)

	type view struct {
		Events   []string        `json:"events"`
		Channels map[string]bool `json:"channels"`
		Source   string          `json:"source"`
	}
	var v view
	admin.do("GET", "/api/v1/settings/notifications", nil, http.StatusOK, &v)
	if v.Source != "env" || v.Channels["slack"] {
		t.Fatalf("starts unconfigured: %+v", v)
	}
	admin.do("POST", "/api/v1/settings/notifications/test", nil, http.StatusBadRequest, nil)

	// Bad input is refused before anything is saved.
	admin.do("PUT", "/api/v1/settings/notifications", map[string]any{"slack_url": "http://evil.example.com/x"}, http.StatusBadRequest, nil)
	admin.do("PUT", "/api/v1/settings/notifications", map[string]any{"telegram_token": "123:abc"}, http.StatusBadRequest, nil)
	admin.do("PUT", "/api/v1/settings/notifications", map[string]any{"events": []string{"nope"}}, http.StatusBadRequest, nil)

	admin.do("PUT", "/api/v1/settings/notifications", map[string]any{"slack_url": hook.URL, "events": []string{"approval"}}, http.StatusOK, &v)
	if v.Source != "ui" || !v.Channels["slack"] || len(v.Events) != 1 {
		t.Fatalf("after save: %+v", v)
	}
	var raw map[string]any
	admin.do("GET", "/api/v1/settings/notifications", nil, http.StatusOK, &raw)
	if b, _ := json.Marshal(raw); strings.Contains(string(b), hook.URL) {
		t.Fatal("the webhook URL must never be returned")
	}

	// The test button reaches the channel; a real approval does too, without a restart.
	var tr struct {
		Results []notify.TestResult `json:"results"`
	}
	admin.do("POST", "/api/v1/settings/notifications/test", nil, http.StatusOK, &tr)
	if len(tr.Results) != 1 || !tr.Results[0].OK {
		t.Fatalf("test results: %+v", tr)
	}
	if got := next(t, texts); !strings.Contains(got, "test notification") {
		t.Fatalf("test message: %s", got)
	}
	agent.do("POST", "/api/v1/agent/interrupts", map[string]any{"agent": "billing", "id": "i1", "run_id": "r1",
		"node": "n", "question": "Refund?"}, http.StatusCreated, nil)
	if got := next(t, texts); !strings.Contains(got, "Refund?") {
		t.Fatalf("approval message: %s", got)
	}

	// Operators cannot read or change settings.
	op := e.bearer(admin.newKey("op", domain.RoleOperator, ""))
	op.do("GET", "/api/v1/settings/notifications", nil, http.StatusForbidden, nil)
	op.do("PUT", "/api/v1/settings/notifications", map[string]any{"clear": []string{"slack"}}, http.StatusForbidden, nil)

	admin.do("PUT", "/api/v1/settings/notifications", map[string]any{"clear": []string{"slack"}}, http.StatusOK, &v)
	if v.Channels["slack"] {
		t.Fatal("slack should be cleared")
	}
}
