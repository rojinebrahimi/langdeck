// Package jobs runs periodic housekeeping: data retention, expired sessions and
// stale commands. Every job is idempotent, so running it on several replicas at
// once is safe.
package jobs

import (
	"context"
	"log/slog"
	"time"

	"github.com/rojinebrahimi/langdeck/internal/store"
)

// Runner owns the housekeeping loop.
type Runner struct {
	Store         store.Store
	Log           *slog.Logger
	RetentionDays int
	CommandTTL    time.Duration
	Interval      time.Duration
}

// Run blocks until ctx is cancelled.
func (j *Runner) Run(ctx context.Context) {
	if j.Interval == 0 {
		j.Interval = 5 * time.Minute
	}
	t := time.NewTicker(j.Interval)
	defer t.Stop()
	for {
		j.Once(ctx, time.Now().UTC())
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
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
