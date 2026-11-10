package domain

import (
	"strings"
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

func TestNormalizeAccesses(t *testing.T) {
	got, err := NormalizeAccesses([]Access{{Resource: "  postgres:orders ", Modes: []AccessMode{AccessRead, AccessWrite, AccessRead}}})
	if err != nil || len(got) != 1 || got[0].Resource != "postgres:orders" || len(got[0].Modes) != 2 {
		t.Fatalf("trim and de-duplicate modes: %+v %v", got, err)
	}
	if got, err := NormalizeAccesses(nil); got != nil || err != nil {
		t.Fatalf("nil means not reported: %v %v", got, err)
	}
	if got, err := NormalizeAccesses([]Access{}); got == nil || err != nil {
		t.Fatalf("empty must stay non-nil so it can clear: %v %v", got, err)
	}
	bad := map[string][]Access{
		"no resource": {{Modes: []AccessMode{AccessRead}}},
		"no modes":    {{Resource: "s3:a"}},
		"bad mode":    {{Resource: "s3:a", Modes: []AccessMode{"sudo"}}},
		"duplicate":   {{Resource: "s3:a", Modes: []AccessMode{AccessRead}}, {Resource: "s3:a", Modes: []AccessMode{AccessWrite}}},
		"long":        {{Resource: strings.Repeat("x", MaxResourceLength+1), Modes: []AccessMode{AccessRead}}},
	}
	for name, in := range bad {
		if _, err := NormalizeAccesses(in); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}

func TestNormalizeDependsOn(t *testing.T) {
	got, err := NormalizeDependsOn("a", []string{"b", "c", "b"})
	if err != nil || len(got) != 2 {
		t.Fatalf("de-duplicate: %v %v", got, err)
	}
	for name, in := range map[string][]string{"self": {"a"}, "bad name": {"Not Valid"}} {
		if _, err := NormalizeDependsOn("a", in); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}
