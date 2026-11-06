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
