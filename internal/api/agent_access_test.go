package api_test

import (
	"net/http"
	"testing"

	"github.com/rojinebrahimi/langdeck/internal/domain"
)

func TestAgentAccessesAndDependencyGraph(t *testing.T) {
	e := newEnv(t)
	admin := e.browser("admin@example.com")
	agent := e.bearer(admin.newKey("bot", domain.RoleAgent, ""))

	agent.do("POST", "/api/v1/agent/register", map[string]any{
		"name": "support-bot",
		"accesses": []map[string]any{
			{"resource": "postgres:orders", "modes": []string{"read", "write"}, "description": "refund lookups"},
			{"resource": "s3:reports", "modes": []string{"read"}},
		},
		"depends_on": []string{"billing", "kb-search"},
	}, http.StatusOK, nil)
	agent.do("POST", "/api/v1/agent/register", map[string]any{"name": "billing"}, http.StatusOK, nil)

	var list agentPage
	admin.do("GET", "/api/v1/agents", nil, http.StatusOK, &list)
	var id string
	for _, a := range list.Agents {
		if a.Name == "support-bot" {
			id = a.ID
			if len(a.Accesses) != 2 || len(a.DependsOn) != 2 {
				t.Fatalf("list row lost accesses/dependencies: %+v", a)
			}
		}
		if a.Name == "billing" && (a.Accesses == nil || a.DependsOn == nil) {
			t.Fatalf("unreported lists must encode as [] not null: %+v", a)
		}
	}

	// Re-registering without the fields (an older SDK) keeps them.
	agent.do("POST", "/api/v1/agent/register", map[string]any{"name": "support-bot", "version": "2"}, http.StatusOK, nil)
	var detail struct {
		Agent domain.Agent `json:"agent"`
	}
	admin.do("GET", "/api/v1/agents/"+id, nil, http.StatusOK, &detail)
	if len(detail.Agent.Accesses) != 2 || detail.Agent.Accesses[0].Modes[1] != domain.AccessWrite {
		t.Fatalf("accesses not kept: %+v", detail.Agent.Accesses)
	}

	var g struct {
		Nodes []struct {
			Name       string `json:"name"`
			Status     string `json:"status"`
			Registered bool   `json:"registered"`
			Accesses   int    `json:"accesses"`
		} `json:"nodes"`
		Edges []struct{ Source, Target string } `json:"edges"`
	}
	admin.do("GET", "/api/v1/agent-graph", nil, http.StatusOK, &g)
	if len(g.Edges) != 2 || len(g.Nodes) != 3 {
		t.Fatalf("graph: %+v", g)
	}
	for _, n := range g.Nodes {
		if n.Name == "kb-search" && (n.Registered || n.Status != "unknown") {
			t.Fatalf("an unregistered dependency must show as unknown: %+v", n)
		}
		if n.Name == "support-bot" && n.Accesses != 2 {
			t.Fatalf("access count: %+v", n)
		}
	}

	// Validation.
	for name, body := range map[string]map[string]any{
		"bad mode": {"name": "x1", "accesses": []map[string]any{{"resource": "db", "modes": []string{"sudo"}}}},
		"no modes": {"name": "x2", "accesses": []map[string]any{{"resource": "db", "modes": []string{}}}},
		"self dep": {"name": "x3", "depends_on": []string{"x3"}},
		"bad dep":  {"name": "x4", "depends_on": []string{"Not Valid"}},
	} {
		t.Run(name, func(t *testing.T) { agent.do("POST", "/api/v1/agent/register", body, http.StatusBadRequest, nil) })
	}
}
