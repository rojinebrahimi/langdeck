package domain

import (
	"testing"
	"time"
)

func TestRoleAtLeast(t *testing.T) {
	cases := []struct {
		r, min Role
		want   bool
	}{
		{RoleAdmin, RoleViewer, true},
		{RoleOperator, RoleOperator, true},
		{RoleViewer, RoleOperator, false},
		{RoleAgent, RoleViewer, false},
		{RoleAdmin, RoleAgent, false},
		{RoleAgent, RoleAgent, true},
		{Role("root"), RoleViewer, false},
	}
	for _, c := range cases {
		if got := c.r.AtLeast(c.min); got != c.want {
			t.Errorf("%s.AtLeast(%s) = %v, want %v", c.r, c.min, got, c.want)
		}
	}
}

func TestComputeStatus(t *testing.T) {
	now := time.Now()
	recent, old := now.Add(-5*time.Second), now.Add(-time.Hour)
	cases := []struct {
		hb      *time.Time
		desired DesiredState
		want    AgentStatus
	}{
		{nil, DesiredRunning, AgentOffline},
		{&old, DesiredPaused, AgentOffline},
		{&recent, DesiredRunning, AgentOnline},
		{&recent, DesiredPaused, AgentPaused},
	}
	for _, c := range cases {
		a := Agent{LastHeartbeat: c.hb, DesiredState: c.desired}
		a.ComputeStatus(now)
		if a.Status != c.want {
			t.Errorf("hb=%v desired=%s: got %s, want %s", c.hb, c.desired, a.Status, c.want)
		}
	}
}

func TestValidAgentName(t *testing.T) {
	for _, ok := range []string{"a", "support-bot", "agent_1.v2", "x0"} {
		if !ValidAgentName(ok) {
			t.Errorf("%q should be valid", ok)
		}
	}
	for _, bad := range []string{"", "Upper", "-lead", "trail-", "has space", "a/b", string(make([]byte, 70))} {
		if ValidAgentName(bad) {
			t.Errorf("%q should be invalid", bad)
		}
	}
}

func TestAPIKeyActive(t *testing.T) {
	now := time.Now()
	past, future := now.Add(-time.Minute), now.Add(time.Minute)
	if !(&APIKey{}).Active(now) || !(&APIKey{ExpiresAt: &future}).Active(now) {
		t.Fatal("unexpired key must be active")
	}
	if (&APIKey{ExpiresAt: &past}).Active(now) || (&APIKey{RevokedAt: &past}).Active(now) {
		t.Fatal("expired or revoked key must be inactive")
	}
}
