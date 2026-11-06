package store

import (
	"math"
	"slices"
	"sort"
	"time"

	"github.com/rojinebrahimi/langdeck/internal/domain"
)

// ComputeStats builds the dashboard summary from agents and the last 24h of runs.
// Both store implementations share it so the numbers are always computed the same way.
func ComputeStats(agents []domain.Agent, runs []domain.Run, pendingInterrupts int, now time.Time) *domain.Stats {
	st := &domain.Stats{
		AgentsTotal:       len(agents),
		RunsLast24h:       len(runs),
		RunsByStatus:      map[domain.RunStatus]int{},
		PendingInterrupts: pendingInterrupts,
		Hourly:            make([]domain.HourlyRunBucket, 24),
		TopAgents:         []domain.AgentRunCounter{},
	}
	for i := range agents {
		agents[i].ComputeStatus(now)
		if agents[i].Status != domain.AgentOffline {
			st.AgentsOnline++
		}
	}

	start := now.Truncate(time.Hour).Add(-23 * time.Hour)
	for i := range st.Hourly {
		st.Hourly[i].Hour = start.Add(time.Duration(i) * time.Hour)
	}

	perAgent := map[string]*domain.AgentRunCounter{}
	var latencies []int64
	finished, failed := 0, 0
	for _, r := range runs {
		st.RunsByStatus[r.Status]++
		st.TokensLast24h += r.PromptTokens + r.CompletionTokens

		c := perAgent[r.AgentName]
		if c == nil {
			c = &domain.AgentRunCounter{AgentName: r.AgentName}
			perAgent[r.AgentName] = c
		}
		c.Runs++

		if idx := int(r.CreatedAt.Sub(start) / time.Hour); idx >= 0 && idx < 24 {
			switch r.Status {
			case domain.RunSucceeded:
				st.Hourly[idx].Succeeded++
			case domain.RunFailed:
				st.Hourly[idx].Failed++
			default:
				st.Hourly[idx].Other++
			}
		}
		if r.Status == domain.RunFailed {
			failed++
			c.Failed++
		}
		if r.Status.Terminal() {
			finished++
		}
		if r.StartedAt != nil && r.EndedAt != nil {
			latencies = append(latencies, r.EndedAt.Sub(*r.StartedAt).Milliseconds())
		}
	}
	if finished > 0 {
		st.FailureRate = float64(failed) / float64(finished)
	}
	st.P50LatencyMS = percentile(latencies, 0.50)
	st.P95LatencyMS = percentile(latencies, 0.95)

	for _, c := range perAgent {
		st.TopAgents = append(st.TopAgents, *c)
	}
	sort.Slice(st.TopAgents, func(i, j int) bool {
		if st.TopAgents[i].Runs != st.TopAgents[j].Runs {
			return st.TopAgents[i].Runs > st.TopAgents[j].Runs
		}
		return st.TopAgents[i].AgentName < st.TopAgents[j].AgentName
	})
	if len(st.TopAgents) > 5 {
		st.TopAgents = st.TopAgents[:5]
	}
	return st
}

// percentile uses the nearest-rank method on a copy of xs.
func percentile(xs []int64, p float64) int64 {
	if len(xs) == 0 {
		return 0
	}
	s := slices.Clone(xs)
	slices.Sort(s)
	idx := min(max(int(math.Ceil(float64(len(s))*p))-1, 0), len(s)-1)
	return s[idx]
}
