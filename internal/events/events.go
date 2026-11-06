// Package events fans out small change notifications to SSE clients and to
// long-polling agents. Notifications carry ids only; consumers refetch state.
// That keeps payloads tiny (Postgres NOTIFY caps them at 8 KB) and means a
// missed notification is never a correctness problem, only a latency one.
package events

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Kinds of notification.
const (
	AgentUpdated     = "agent.updated"
	RunUpdated       = "run.updated"
	RunEvent         = "run.event"
	InterruptUpdated = "interrupt.updated"
	CommandCreated   = "command.created"
)

// Event is one notification.
type Event struct {
	Kind    string `json:"kind"`
	AgentID string `json:"agent_id,omitempty"`
	RunID   string `json:"run_id,omitempty"`
	ID      string `json:"id,omitempty"`
}

// Broker publishes and subscribes to events.
type Broker interface {
	Publish(ctx context.Context, e Event)
	Subscribe() (<-chan Event, func())
}

// Local is an in-process broker. Slow subscribers drop events rather than
// blocking publishers.
type Local struct {
	mu   sync.RWMutex
	subs map[chan Event]struct{}
}

// NewLocal returns an empty in-process broker.
func NewLocal() *Local { return &Local{subs: map[chan Event]struct{}{}} }

// Publish delivers e to every subscriber without blocking.
func (l *Local) Publish(_ context.Context, e Event) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	for ch := range l.subs {
		select {
		case ch <- e:
		default: // subscriber is behind; it will catch up on its next refetch
		}
	}
}

// Subscribe returns a channel of events and a function that ends the subscription.
func (l *Local) Subscribe() (<-chan Event, func()) {
	ch := make(chan Event, 64)
	l.mu.Lock()
	l.subs[ch] = struct{}{}
	l.mu.Unlock()
	var once sync.Once
	return ch, func() {
		once.Do(func() {
			l.mu.Lock()
			delete(l.subs, ch)
			l.mu.Unlock()
			close(ch)
		})
	}
}

const pgChannel = "langdeck_events"

// Postgres fans events out across replicas with LISTEN/NOTIFY.
// Publish sends a NOTIFY; a background listener feeds every replica's Local broker,
// including the publisher's own.
type Postgres struct {
	pool  *pgxpool.Pool
	local *Local
	log   *slog.Logger
}

// NewPostgres starts the listener loop; it stops when ctx is cancelled.
func NewPostgres(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger) *Postgres {
	p := &Postgres{pool: pool, local: NewLocal(), log: log}
	go p.listen(ctx)
	return p
}

// Publish sends e through Postgres. On failure it still delivers locally.
func (p *Postgres) Publish(ctx context.Context, e Event) {
	b, _ := json.Marshal(e)
	if _, err := p.pool.Exec(ctx, `SELECT pg_notify($1, $2)`, pgChannel, string(b)); err != nil {
		p.log.Warn("pg_notify failed, delivering locally only", "err", err)
		p.local.Publish(ctx, e)
	}
}

// Subscribe subscribes to events from every replica.
func (p *Postgres) Subscribe() (<-chan Event, func()) { return p.local.Subscribe() }

func (p *Postgres) listen(ctx context.Context) {
	backoff := time.Second
	for ctx.Err() == nil {
		err := p.listenOnce(ctx)
		if ctx.Err() != nil {
			return
		}
		p.log.Warn("event listener disconnected, retrying", "err", err, "in", backoff)
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, 30*time.Second)
	}
}

func (p *Postgres) listenOnce(ctx context.Context) error {
	conn, err := p.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "LISTEN "+pgChannel); err != nil {
		return err
	}
	for {
		n, err := conn.Conn().WaitForNotification(ctx)
		if err != nil {
			return err
		}
		var e Event
		if json.Unmarshal([]byte(n.Payload), &e) == nil {
			p.local.Publish(ctx, e)
		}
	}
}
