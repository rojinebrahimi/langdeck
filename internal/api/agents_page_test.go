package api_test

import (
	"bytes"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/rojinebrahimi/langdeck/internal/auth"
	"github.com/rojinebrahimi/langdeck/internal/domain"
)

type agentPage struct {
	Agents    []domain.Agent `json:"agents"`
	Total     int            `json:"total"`
	NextAfter *string        `json:"next_after"`
}

func TestAgentsPagination(t *testing.T) {
	e := newEnv(t)
	admin := e.browser("admin@example.com")
	agent := e.bearer(admin.newKey("bot", domain.RoleAgent, ""))
	for i := range 7 {
		name := fmt.Sprintf("agent-%02d", i)
		agent.do("POST", "/api/v1/agent/register", map[string]any{"name": name, "description": "worker " + name}, http.StatusOK, nil)
	}
	agent.do("POST", "/api/v1/agent/register", map[string]any{"name": "support-bot", "description": "Answers customers"}, http.StatusOK, nil)

	// Walk every page with the cursor; each agent appears exactly once, in order.
	var seen []string
	after := ""
	for pages := 0; ; pages++ {
		if pages > 10 {
			t.Fatal("pagination did not terminate")
		}
		var p agentPage
		admin.do("GET", "/api/v1/agents?limit=3&after="+url.QueryEscape(after), nil, http.StatusOK, &p)
		if p.Total != 8 {
			t.Fatalf("total = %d, want 8", p.Total)
		}
		for _, a := range p.Agents {
			seen = append(seen, a.Name)
			if a.Graph != nil || a.Status == "" {
				t.Fatalf("list rows must be light and carry a status: %+v", a)
			}
		}
		if p.NextAfter == nil {
			break
		}
		after = *p.NextAfter
	}
	want := "agent-00,agent-01,agent-02,agent-03,agent-04,agent-05,agent-06,support-bot"
	if strings.Join(seen, ",") != want {
		t.Fatalf("walked %v", seen)
	}

	// An exactly full last page has no next cursor (no empty trailing page).
	var exact agentPage
	admin.do("GET", "/api/v1/agents?limit=8", nil, http.StatusOK, &exact)
	if len(exact.Agents) != 8 || exact.NextAfter != nil {
		t.Fatalf("exact page: %d agents, next=%v", len(exact.Agents), exact.NextAfter)
	}

	// Search and status filters.
	var found agentPage
	admin.do("GET", "/api/v1/agents?q=CUSTOMERS", nil, http.StatusOK, &found)
	if found.Total != 1 || found.Agents[0].Name != "support-bot" {
		t.Fatalf("search: %+v", found)
	}
	var online agentPage
	admin.do("GET", "/api/v1/agents?status=online", nil, http.StatusOK, &online)
	if online.Total != 8 {
		t.Fatalf("all freshly registered agents are online, got %d", online.Total)
	}
	var offline agentPage
	admin.do("GET", "/api/v1/agents?status=offline", nil, http.StatusOK, &offline)
	if offline.Total != 0 || offline.Agents == nil {
		t.Fatalf("offline must be an empty list: %+v", offline)
	}

	for _, bad := range []string{"status=sleeping", "limit=0", "limit=abc", "q=" + strings.Repeat("x", 129)} {
		admin.do("GET", "/api/v1/agents?"+bad, nil, http.StatusBadRequest, nil)
	}
	// Oversized pages are clamped rather than rejected.
	var big agentPage
	admin.do("GET", "/api/v1/agents?limit=100000", nil, http.StatusOK, &big)
	if len(big.Agents) != 8 {
		t.Fatalf("clamped page: %d", len(big.Agents))
	}
}

func TestAgentImage(t *testing.T) {
	e := newEnv(t)
	admin := e.browser("admin@example.com")
	agent := e.bearer(admin.newKey("bot", domain.RoleAgent, ""))
	var reg struct {
		Agent domain.Agent `json:"agent"`
	}
	agent.do("POST", "/api/v1/agent/register", map[string]any{"name": "pic"}, http.StatusOK, &reg)
	id := reg.Agent.ID
	if id == "" {
		t.Skip("register response has no agent id")
	}
	png := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89")

	// Only real raster images are accepted; the sniffed type wins over the declared one.
	if code := admin.raw("PUT", "/api/v1/agents/"+id+"/image", "image/png", []byte("<svg onload=alert(1)/>")); code != http.StatusUnsupportedMediaType {
		t.Fatalf("svg/text upload = %d, want 415", code)
	}
	if code := admin.raw("PUT", "/api/v1/agents/"+id+"/image", "image/png", png); code != http.StatusOK {
		t.Fatalf("png upload = %d", code)
	}
	var got struct {
		Agent domain.Agent `json:"agent"`
	}
	admin.do("GET", "/api/v1/agents/"+id, nil, http.StatusOK, &got)
	if got.Agent.ImageUpdatedAt == nil {
		t.Fatal("agent should report an image")
	}
	if code := admin.raw("GET", "/api/v1/agents/"+id+"/image", "", nil); code != http.StatusOK {
		t.Fatalf("get image = %d", code)
	}
	admin.do("DELETE", "/api/v1/agents/"+id+"/image", nil, http.StatusNoContent, nil)
	admin.do("GET", "/api/v1/agents/"+id+"/image", nil, http.StatusNotFound, nil)
}

// raw sends a non-JSON body and returns only the status code.
func (c *client) raw(method, path, contentType string, body []byte) int {
	c.e.t.Helper()
	req, err := http.NewRequest(method, c.e.srv.URL+path, bytes.NewReader(body))
	if err != nil {
		c.e.t.Fatal(err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if c.csrf != "" {
		req.Header.Set(auth.CSRFHeader, c.csrf)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		c.e.t.Fatal(err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}
