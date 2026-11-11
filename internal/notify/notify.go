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
	"net/url"
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

// lane is one sender with its own queue and worker, so a slow Teams webhook never delays
// Slack, and every event reaches every channel.
type lane struct {
	s      Sender
	queue  chan Event
	cancel context.CancelFunc
}

// Dispatcher fans events out to every configured sender in the background. Its
// configuration can be replaced at runtime with Reconfigure (the settings page does).
type Dispatcher struct {
	log     *slog.Logger
	client  *http.Client
	timeout time.Duration
	qsize   int
	public  string

	mu     sync.RWMutex
	lanes  []*lane
	kinds  map[string]struct{}
	runCtx context.Context // nil until Run
	wg     sync.WaitGroup

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

// New builds a dispatcher. It is disabled (Enabled() == false) when no channel is
// configured, which makes every call a cheap no-op.
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
	d := &Dispatcher{log: o.Log, client: o.HTTPClient, timeout: o.Timeout, qsize: o.Queue, public: o.PublicURL}
	d.lanes, d.kinds = d.build(o)
	return d
}

// build turns options into lanes (not yet running) and the subscribed event kinds.
func (d *Dispatcher) build(o Options) ([]*lane, map[string]struct{}) {
	var senders []Sender
	if o.SlackURL != "" {
		senders = append(senders, &chatSender{name: "slack", url: o.SlackURL, client: d.client, publicURL: d.public})
	}
	if o.MattermostURL != "" {
		senders = append(senders, &chatSender{name: "mattermost", url: o.MattermostURL, client: d.client, publicURL: d.public})
	}
	if o.TeamsURL != "" {
		senders = append(senders, &teamsSender{url: o.TeamsURL, client: d.client, publicURL: d.public})
	}
	if o.TelegramToken != "" && o.TelegramChat != "" {
		senders = append(senders, &telegramSender{token: o.TelegramToken, chat: o.TelegramChat, client: d.client, publicURL: d.public})
	}
	lanes := make([]*lane, 0, len(senders))
	for _, s := range senders {
		lanes = append(lanes, &lane{s: s, queue: make(chan Event, d.qsize)})
	}
	kinds := map[string]struct{}{}
	for _, k := range o.Events {
		if k = strings.TrimSpace(strings.ToLower(k)); k != "" {
			kinds[k] = struct{}{}
		}
	}
	if len(kinds) == 0 {
		for _, k := range DefaultKinds {
			kinds[k] = struct{}{}
		}
	}
	return lanes, kinds
}

// Reconfigure swaps in new channels and event kinds without a restart. Events already
// queued for a removed channel are abandoned, like at shutdown.
func (d *Dispatcher) Reconfigure(o Options) {
	if d == nil {
		return
	}
	d.mu.Lock()
	if o.PublicURL != "" {
		d.public = o.PublicURL
	}
	lanes, kinds := d.build(o)
	old := d.lanes
	d.lanes, d.kinds = lanes, kinds
	if d.runCtx != nil {
		d.startLocked(lanes)
	}
	d.mu.Unlock()
	for _, l := range old {
		if l.cancel != nil {
			l.cancel()
		}
	}
	d.log.Info("notification settings applied", "channels", d.ChannelNames(), "events", strings.Join(d.kindList(), ","))
}

// Enabled reports whether at least one channel is configured.
func (d *Dispatcher) Enabled() bool {
	if d == nil {
		return false
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	return len(d.lanes) > 0
}

// Wants reports whether this event kind is subscribed.
func (d *Dispatcher) Wants(kind string) bool {
	if d == nil {
		return false
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	_, ok := d.kinds[kind]
	return ok
}

// Publish queues an event on every channel. It never blocks: if a queue is full the
// event is dropped and counted, because a webhook outage must not become a request
// latency problem.
func (d *Dispatcher) Publish(e Event) {
	if d == nil || d.stopped.Load() {
		return
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	if _, ok := d.kinds[e.Kind]; !ok {
		return
	}
	for _, l := range d.lanes {
		select {
		case l.queue <- e:
		default:
			d.dropped.Add(1)
			notifyDropped.WithLabelValues(e.Kind).Inc()
			d.log.Warn("notification queue full, dropping event", "channel", l.s.Name(), "kind", e.Kind, "title", e.Title)
		}
	}
}

// Run starts the workers and blocks until ctx is cancelled.
func (d *Dispatcher) Run(ctx context.Context) {
	d.mu.Lock()
	d.runCtx = ctx
	d.startLocked(d.lanes)
	d.mu.Unlock()
	if d.Enabled() {
		d.log.Info("notifications enabled", "channels", d.ChannelNames(), "events", strings.Join(d.kindList(), ","))
	}
	<-ctx.Done()
	d.wg.Wait()
}

func (d *Dispatcher) startLocked(lanes []*lane) {
	for _, l := range lanes {
		wctx, cancel := context.WithCancel(d.runCtx)
		l.cancel = cancel
		d.wg.Add(1)
		go d.worker(wctx, l)
	}
}

func (d *Dispatcher) worker(ctx context.Context, l *lane) {
	defer d.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case e := <-l.queue:
			d.deliver(ctx, l.s, e)
		}
	}
}

// TestResult is the outcome of one channel in SendTest.
type TestResult struct {
	Channel string `json:"channel"`
	OK      bool   `json:"ok"`
	Error   string `json:"error,omitempty"`
}

// SendTest delivers a message to every configured channel right now, once and without
// retries, so the settings page can show exactly which channel works.
func (d *Dispatcher) SendTest(ctx context.Context, e Event) []TestResult {
	d.mu.RLock()
	lanes := append([]*lane(nil), d.lanes...)
	d.mu.RUnlock()
	out := make([]TestResult, 0, len(lanes))
	for _, l := range lanes {
		cctx, cancel := context.WithTimeout(ctx, d.timeout)
		err := l.s.Send(cctx, e)
		cancel()
		r := TestResult{Channel: l.s.Name(), OK: err == nil}
		if err != nil {
			r.Error = sanitizeErr(err)
		}
		out = append(out, r)
	}
	return out
}

// sanitizeErr keeps webhook URLs and bot tokens (which appear in transport errors) out of
// responses and logs.
func sanitizeErr(err error) string {
	var pe *permanentError
	if errors.As(err, &pe) {
		return pe.err.Error()
	}
	var ue *url.Error
	if errors.As(err, &ue) {
		return "request failed: " + ue.Err.Error()
	}
	return "request failed"
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

// ChannelNames lists the configured channels.
func (d *Dispatcher) ChannelNames() []string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	out := make([]string, 0, len(d.lanes))
	for _, l := range d.lanes {
		out = append(out, l.s.Name())
	}
	return out
}

func (d *Dispatcher) kindList() []string {
	d.mu.RLock()
	defer d.mu.RUnlock()
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
