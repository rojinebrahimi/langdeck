// Package notify delivers operational events to chat services — Slack, Mattermost,
// Microsoft Teams and Telegram — so operators learn about approvals and failures
// without staring at the console.
//
// Delivery is asynchronous and best-effort. A slow or broken webhook never blocks a
// request, never fails a run and never affects the agent API; failed deliveries are
// retried a few times and then logged.
package notify

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Event kinds. They double as the values accepted by LANGDECK_NOTIFY_EVENTS.
const (
	KindApproval     = "approval"
	KindRunFailed    = "run_failed"
	KindAgentOffline = "agent_offline"
	KindAgentOnline  = "agent_online"
)

// DefaultKinds is what a dispatcher watches when no event list is configured.
var DefaultKinds = []string{KindApproval, KindRunFailed, KindAgentOffline}

// Event is one notification. It is transport-neutral: senders render it in their own
// format, so adding a channel never touches the code that raises events.
type Event struct {
	Kind   string
	Title  string
	Text   string
	URL    string      // deep link into LangDeck
	Fields [][2]string // ordered key/value details
}

// Sender is one destination.
type Sender interface {
	Name() string
	Send(ctx context.Context, e Event) error
}

// Dispatcher fans events out to every configured sender in the background.
type Dispatcher struct {
	log     *slog.Logger
	senders []Sender
	kinds   map[string]struct{}
	queue   chan Event
	timeout time.Duration
	wg      sync.WaitGroup
	once    sync.Once
	stopped atomic.Bool

	dropped atomic.Int64
}

// Options builds a dispatcher from configuration. Empty endpoints are skipped, so
// callers can pass the whole config unconditionally.
type Options struct {
	SlackURL      string
	MattermostURL string
	TeamsURL      string
	TelegramToken string
	TelegramChat  string
	Events        []string
	PublicURL     string
	Log           *slog.Logger
	HTTPClient    *http.Client // optional; tests inject one
	Queue         int          // optional buffer size
	Timeout       time.Duration
}

// New builds a dispatcher. It returns a disabled dispatcher (Enabled() == false) when
// no channel is configured, which makes every call a cheap no-op.
func New(o Options) *Dispatcher {
	if o.Log == nil {
		o.Log = slog.Default()
	}
	if o.HTTPClient == nil {
		o.HTTPClient = &http.Client{Timeout: 10 * time.Second}
	}
	if o.Queue == 0 {
		o.Queue = 512
	}
	if o.Timeout == 0 {
		o.Timeout = 10 * time.Second
	}
	var senders []Sender
	if o.SlackURL != "" {
		senders = append(senders, &chatSender{name: "slack", url: o.SlackURL, client: o.HTTPClient, publicURL: o.PublicURL})
	}
	if o.MattermostURL != "" {
		senders = append(senders, &chatSender{name: "mattermost", url: o.MattermostURL, client: o.HTTPClient, publicURL: o.PublicURL})
	}
	if o.TeamsURL != "" {
		senders = append(senders, &teamsSender{url: o.TeamsURL, client: o.HTTPClient, publicURL: o.PublicURL})
	}
	if o.TelegramToken != "" && o.TelegramChat != "" {
		senders = append(senders, &telegramSender{token: o.TelegramToken, chat: o.TelegramChat, client: o.HTTPClient, publicURL: o.PublicURL})
	}
	kinds := make([]string, 0, len(o.Events))
	for _, k := range o.Events {
		if k = strings.TrimSpace(strings.ToLower(k)); k != "" {
			kinds = append(kinds, k)
		}
	}
	if len(kinds) == 0 {
		kinds = DefaultKinds
	}
	d := &Dispatcher{log: o.Log, senders: senders, kinds: map[string]struct{}{}, queue: make(chan Event, o.Queue), timeout: o.Timeout}
	for _, k := range kinds {
		d.kinds[k] = struct{}{}
	}
	return d
}

// Enabled reports whether at least one channel is configured.
func (d *Dispatcher) Enabled() bool { return d != nil && len(d.senders) > 0 }

// Wants reports whether this event kind is subscribed.
func (d *Dispatcher) Wants(kind string) bool {
	if d == nil {
		return false
	}
	_, ok := d.kinds[kind]
	return ok
}

// Publish queues an event. It never blocks: if the queue is full the event is dropped
// and counted, because a webhook outage must not become a request latency problem.
func (d *Dispatcher) Publish(e Event) {
	if !d.Enabled() || !d.Wants(e.Kind) || d.stopped.Load() {
		return
	}
	select {
	case d.queue <- e:
	default:
		d.dropped.Add(1)
		notifyDropped.WithLabelValues(e.Kind).Inc()
		d.log.Warn("notification queue full, dropping event", "kind", e.Kind, "title", e.Title)
	}
}

// Run starts one worker per sender and blocks until ctx is cancelled.
func (d *Dispatcher) Run(ctx context.Context) {
	if !d.Enabled() {
		return
	}
	for _, s := range d.senders {
		d.wg.Add(1)
		go d.worker(ctx, s)
	}
	d.log.Info("notifications enabled", "channels", d.channelNames(), "events", strings.Join(d.kindList(), ","))
	<-ctx.Done()
	d.wg.Wait()
}

func (d *Dispatcher) worker(ctx context.Context, s Sender) {
	defer d.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case e, ok := <-d.queue:
			if !ok {
				return
			}
			d.deliver(ctx, s, e)
		}
	}
}

// deliver retries transient failures with backoff. Bad requests (4xx) are permanent and
// are not retried: a malformed payload will never succeed.
func (d *Dispatcher) deliver(ctx context.Context, s Sender, e Event) {
	backoff := time.Second
	for attempt := 1; attempt <= 3; attempt++ {
		cctx, cancel := context.WithTimeout(ctx, d.timeout)
		err := s.Send(cctx, e)
		cancel()
		if err == nil {
			notifySent.WithLabelValues(s.Name(), e.Kind).Inc()
			return
		}
		if pe := (*permanentError)(nil); errors.As(err, &pe) {
			notifySent.WithLabelValues(s.Name(), e.Kind+"_permanent").Inc()
			d.log.Warn("notification rejected", "channel", s.Name(), "kind", e.Kind, "err", pe.err)
			return
		}
		if attempt < 3 && !sleep(ctx, backoff) {
			return
		}
		backoff *= 3
	}
	notifySent.WithLabelValues(s.Name(), e.Kind+"_failed").Inc()
	d.log.Warn("notification failed", "channel", s.Name(), "kind", e.Kind, "attempts", 3)
}

// Close stops accepting new events. Workers exit when the run context is cancelled;
// pending events are abandoned, because notifications are advisory and shutdown should
// stay fast.
func (d *Dispatcher) Close() {
	if d == nil {
		return
	}
	d.once.Do(func() { d.stopped.Store(true) })
}

func (d *Dispatcher) channelNames() []string {
	out := make([]string, 0, len(d.senders))
	for _, s := range d.senders {
		out = append(out, s.Name())
	}
	return out
}

func (d *Dispatcher) kindList() []string {
	out := make([]string, 0, len(d.kinds))
	for k := range d.kinds {
		out = append(out, k)
	}
	return out
}

func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// permanentError marks a response that retrying cannot fix.
type permanentError struct{ err error }

func (p *permanentError) Error() string { return p.err.Error() }
