package store

import (
	"testing"
	"time"

	"github.com/rojinebrahimi/langdeck/internal/domain"
)

func TestComputeStats(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 30, 0, 0, time.UTC)
	hb := now.Add(-time.Second)
	agents := []domain.Agent{{Name: "a", LastHeartbeat: &hb, DesiredState: domain.DesiredRunning}, {Name: "b"}}
	mk := func(agent string, st domain.RunStatus, ago, dur time.Duration) domain.Run {
		start := now.Add(-ago)
		end := start.Add(dur)
		return domain.Run{AgentName: agent, Status: st, CreatedAt: start, StartedAt: &start, EndedAt: &end, PromptTokens: 10, CompletionTokens: 5}
	}
	runs := []domain.Run{
		mk("a", domain.RunSucceeded, time.Minute, 100*time.Millisecond),
		mk("a", domain.RunSucceeded, 2*time.Hour, 200*time.Millisecond),
		mk("a", domain.RunFailed, 3*time.Hour, 300*time.Millisecond),
		mk("b", domain.RunSucceeded, 4*time.Hour, 400*time.Millisecond),
	}
	st := ComputeStats(agents, runs, 2, now)
	if st.AgentsTotal != 2 || st.AgentsOnline != 1 || st.RunsLast24h != 4 || st.PendingInterrupts != 2 {
		t.Fatalf("counts %+v", st)
	}
	if st.FailureRate != 0.25 || st.TokensLast24h != 60 {
		t.Fatalf("rate %v tokens %d", st.FailureRate, st.TokensLast24h)
	}
	if st.P50LatencyMS != 200 || st.P95LatencyMS != 400 {
		t.Fatalf("p50 %d p95 %d", st.P50LatencyMS, st.P95LatencyMS)
	}
	if len(st.Hourly) != 24 || st.Hourly[23].Succeeded != 1 {
		t.Fatalf("hourly %+v", st.Hourly[23])
	}
	if st.TopAgents[0].AgentName != "a" || st.TopAgents[0].Failed != 1 {
		t.Fatalf("top %+v", st.TopAgents)
	}
	if empty := ComputeStats(nil, nil, 0, now); empty.FailureRate != 0 || empty.P95LatencyMS != 0 {
		t.Fatal("empty stats must be zero")
	}
}
