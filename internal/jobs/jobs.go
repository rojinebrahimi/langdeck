// Package jobs runs periodic housekeeping: data retention, expired sessions and
// stale commands. Every job is idempotent, so running it on several replicas at
// once is safe.
package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/rojinebrahimi/langdeck/internal/domain"
	"github.com/rojinebrahimi/langdeck/internal/notify"
	"github.com/rojinebrahimi/langdeck/internal/store"
)

// Runner owns the housekeeping loop.
type Runner struct {
	Store         store.Store
	Log           *slog.Logger
	RetentionDays int
	CommandTTL    time.Duration
	Interval      time.Duration

	// Notify raises agent online/offline chat notifications. Nil disables them.
	Notify    *notify.Dispatcher
	PublicURL string
	// AgentInterval is how often heartbeats are checked; defaults to 30s.
	AgentInterval time.Duration

	mu      sync.Mutex
	offline map[string]bool // agents already reported offline
}

// Run blocks until ctx is cancelled.
func (j *Runner) Run(ctx context.Context) {
	if j.Interval == 0 {
		j.Interval = 5 * time.Minute
	}
	t := time.NewTicker(j.Interval)
	defer t.Stop()
	var agents *time.Ticker
	if j.Notify.Enabled() {
		interval := j.AgentInterval
		if interval == 0 {
			interval = 30 * time.Second
		}
		agents = time.NewTicker(interval)
		defer agents.Stop()
	}
	for {
		j.Once(ctx, time.Now().UTC())
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-tick(agents):
			j.CheckAgents(ctx, time.Now().UTC())
		}
	}
}

// tick turns an optional ticker into a channel that never fires when the ticker is nil.
func tick(t *time.Ticker) <-chan time.Time {
	if t == nil {
		return nil
	}
	return t.C
}

// Once runs every job a single time.
func (j *Runner) Once(ctx context.Context, now time.Time) {
	if n, err := j.Store.DeleteExpiredSessions(ctx, now); err != nil {
		j.Log.Error("session cleanup failed", "err", err)
	} else if n > 0 {
		j.Log.Info("expired sessions removed", "count", n)
	}
	if j.CommandTTL > 0 {
		if n, err := j.Store.ExpireCommands(ctx, now.Add(-j.CommandTTL)); err != nil {
			j.Log.Error("command expiry failed", "err", err)
		} else if n > 0 {
			j.Log.Info("stale commands expired", "count", n)
		}
	}
	if j.RetentionDays > 0 {
		if n, err := j.Store.PurgeRunsBefore(ctx, now.AddDate(0, 0, -j.RetentionDays)); err != nil {
			j.Log.Error("retention purge failed", "err", err)
		} else if n > 0 {
			j.Log.Info("old runs purged", "count", n, "retention_days", j.RetentionDays)
		}
	}
}

// CheckAgents raises a chat notification when an agent crosses the online/offline
// boundary. The transition is remembered per process, so a flapping agent notifies once
// per crossing rather than on every sweep.
func (j *Runner) CheckAgents(ctx context.Context, now time.Time) {
	if !j.Notify.Enabled() {
		return
	}
	agents, err := j.Store.ListAgents(ctx)
	if err != nil {
		j.Log.Error("agent status sweep failed", "err", err)
		return
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.offline == nil {
		j.offline = map[string]bool{}
	}
	for i := range agents {
		ag := &agents[i]
		ag.ComputeStatus(now)
		wasOffline := j.offline[ag.ID]
		switch {
		case ag.Status == domain.AgentOffline && !wasOffline:
			j.offline[ag.ID] = true
			j.Notify.Publish(notify.Event{
				Kind:  notify.KindAgentOffline,
				Title: "Agent offline: " + ag.Name,
				Text:  fmt.Sprintf("No heartbeat for over %s.", domain.HeartbeatTTL),
				URL:   j.PublicURL + "/agents/" + ag.ID,
				Fields: [][2]string{
					{"Agent", ag.Name},
					{"Version", ag.Version},
					{"Last heartbeat", lastBeat(ag, now)},
				},
			})
		case ag.Status != domain.AgentOffline && wasOffline:
			delete(j.offline, ag.ID)
			j.Notify.Publish(notify.Event{
				Kind:  notify.KindAgentOnline,
				Title: "Agent back online: " + ag.Name,
				URL:   j.PublicURL + "/agents/" + ag.ID,
				Fields: [][2]string{
					{"Agent", ag.Name},
					{"Version", ag.Version},
				},
			})
		}
	}
}

func lastBeat(ag *domain.Agent, now time.Time) string {
	if ag.LastHeartbeat == nil {
		return "never"
	}
	return now.Sub(*ag.LastHeartbeat).Round(time.Second).String() + " ago"
}
