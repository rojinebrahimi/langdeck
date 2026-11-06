package notify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func event() Event {
	return Event{
		Kind:   KindApproval,
		Title:  "Approval needed: Refund $240?",
		Text:   "support-bot is waiting for a human decision.",
		URL:    "https://langdeck.example.com/approvals",
		Fields: [][2]string{{"Agent", "support-bot"}, {"Node", "refund"}},
	}
}

func TestSlackPayload(t *testing.T) {
	bodies := make(chan map[string]any, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("content type %q", ct)
		}
		var m map[string]any
		_ = json.NewDecoder(r.Body).Decode(&m)
		bodies <- m
	}))
	defer srv.Close()

	d := New(Options{SlackURL: srv.URL, PublicURL: "https://langdeck.example.com"})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.Run(ctx)
	d.Publish(event())

	got := receive(t, bodies)
	text, _ := got["text"].(string)
	for _, want := range []string{"Approval needed: Refund $240?", "support-bot", "Node", "Open in LangDeck"} {
		if !strings.Contains(text, want) {
			t.Fatalf("slack text missing %q:\n%s", want, text)
		}
	}
	if got["mrkdwn"] != true {
		t.Fatalf("slack payload should request mrkdwn: %v", got)
	}
}

func TestTeamsCard(t *testing.T) {
	cards := make(chan map[string]any, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var m map[string]any
		_ = json.NewDecoder(r.Body).Decode(&m)
		cards <- m
	}))
	defer srv.Close()

	d := New(Options{TeamsURL: srv.URL})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.Run(ctx)
	d.Publish(event())

	card := receive(t, cards)
	if card["@type"] != "MessageCard" || card["title"] != event().Title {
		t.Fatalf("bad card %v", card)
	}
	actions, _ := card["potentialAction"].([]any)
	if len(actions) != 1 {
		t.Fatalf("want an open action, got %v", card["potentialAction"])
	}
}

func TestTelegramUsesHTML(t *testing.T) {
	var form map[string][]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/sendMessage") {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		_ = r.ParseForm()
		form = r.PostForm
	}))
	defer srv.Close()

	e := event()
	e.Title = "Run failed: <support-bot>"
	client := srv.Client()
	client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		req.URL.Scheme, req.URL.Host = "http", strings.TrimPrefix(srv.URL, "http://")
		return http.DefaultTransport.RoundTrip(req)
	})
	tg := &telegramSender{token: "t", chat: "42", client: client, publicURL: "https://langdeck.example.com"}
	if err := tg.Send(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	if form["chat_id"][0] != "42" || form["parse_mode"][0] != "HTML" {
		t.Fatalf("bad form %v", form)
	}
	if !strings.Contains(form["text"][0], "&lt;support-bot&gt;") {
		t.Fatalf("telegram text must escape HTML: %s", form["text"][0])
	}
}

func TestPermanentFailureIsNotRetried(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.Error(w, "bad payload", http.StatusBadRequest)
	}))
	defer srv.Close()

	d := New(Options{SlackURL: srv.URL})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.Run(ctx)
	d.Publish(event())

	waitFor(t, func() bool { return hits.Load() > 0 })
	time.Sleep(1500 * time.Millisecond) // longer than the first retry backoff
	if n := hits.Load(); n != 1 {
		t.Fatalf("4xx must not be retried, got %d attempts", n)
	}
}

func TestRetriesServerErrors(t *testing.T) {
	var mu sync.Mutex
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits++
		n := hits
		mu.Unlock()
		if n < 2 {
			http.Error(w, "boom", http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	d := New(Options{SlackURL: srv.URL})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.Run(ctx)
	d.Publish(event())

	waitFor(t, func() bool { mu.Lock(); defer mu.Unlock(); return hits >= 2 })
}

func TestDisabledAndFiltered(t *testing.T) {
	if New(Options{}).Enabled() {
		t.Fatal("no channels must mean disabled")
	}
	d := New(Options{SlackURL: "https://example.com/hook", Events: []string{KindRunFailed}})
	if d.Wants(KindApproval) {
		t.Fatal("approval was not subscribed")
	}
	if !d.Wants(KindRunFailed) {
		t.Fatal("run_failed was subscribed")
	}
	if !d.Enabled() {
		t.Fatal("a configured channel means enabled")
	}
	// Publishing an unsubscribed kind must not queue anything.
	d.Publish(event())
	if len(d.queue) != 0 {
		t.Fatalf("filtered event was queued: %d", len(d.queue))
	}
}

func TestPublishNeverBlocks(t *testing.T) {
	d := New(Options{SlackURL: "https://example.com/hook", Queue: 1})
	done := make(chan struct{})
	go func() {
		for range 10 {
			d.Publish(event())
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Publish blocked on a full queue")
	}
	if d.dropped.Load() == 0 {
		t.Fatal("overflowing events should be counted as dropped")
	}
}

func receive(t *testing.T, ch <-chan map[string]any) map[string]any {
	t.Helper()
	select {
	case m := <-ch:
		return m
	case <-time.After(3 * time.Second):
		t.Fatal("no request received")
		return nil
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met in time")
}
