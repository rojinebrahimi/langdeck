package events

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestLocalFanOut(t *testing.T) {
	b := NewLocal()
	s1, c1 := b.Subscribe()
	s2, c2 := b.Subscribe()
	defer c2()
	b.Publish(context.Background(), Event{Kind: RunUpdated, RunID: "r"})
	for _, s := range []<-chan Event{s1, s2} {
		if e := <-s; e.RunID != "r" {
			t.Fatalf("got %+v", e)
		}
	}
	c1()
	c1()                                                     // idempotent
	b.Publish(context.Background(), Event{Kind: RunUpdated}) // must not panic on closed subscriber
}

func TestLocalSlowSubscriberDoesNotBlock(t *testing.T) {
	b := NewLocal()
	_, cancel := b.Subscribe()
	defer cancel()
	done := make(chan struct{})
	go func() {
		for range 1000 {
			b.Publish(context.Background(), Event{Kind: RunEvent})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("publisher blocked on a slow subscriber")
	}
}

func TestPostgresAcrossReplicas(t *testing.T) {
	dsn := os.Getenv("LANGDECK_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("LANGDECK_TEST_DATABASE_URL not set")
	}
	ctx, cancel := context.WithCancel(context.Background())
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	poolA, _ := pgxpool.New(ctx, dsn)
	poolB, _ := pgxpool.New(ctx, dsn)
	// Stop the listeners before closing the pools: Close waits for every
	// acquired connection, including the one blocked in LISTEN.
	defer func() { cancel(); poolA.Close(); poolB.Close() }()
	replicaA, replicaB := NewPostgres(ctx, poolA, log), NewPostgres(ctx, poolB, log)
	sub, unsub := replicaB.Subscribe()
	defer unsub()

	deadline := time.After(5 * time.Second)
	tick := time.NewTicker(100 * time.Millisecond) // LISTEN starts asynchronously; publish until seen
	defer tick.Stop()
	for {
		select {
		case e := <-sub:
			if e.Kind == CommandCreated && e.AgentID == "a1" {
				return
			}
		case <-tick.C:
			replicaA.Publish(ctx, Event{Kind: CommandCreated, AgentID: "a1"})
		case <-deadline:
			t.Fatal("replica B never saw replica A's event")
		}
	}
}
