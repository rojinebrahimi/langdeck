package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/rojinebrahimi/langdeck/internal/auth"
	"github.com/rojinebrahimi/langdeck/internal/domain"
	"github.com/rojinebrahimi/langdeck/internal/events"
	"github.com/rojinebrahimi/langdeck/internal/store"
	"github.com/rojinebrahimi/langdeck/internal/util"
)

// ---- dashboard ----

func (a *API) stats(w http.ResponseWriter, r *http.Request) {
	st, err := a.Store.Stats(r.Context(), a.Now())
	if err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, 200, st)
}

// ---- agents ----

// listAgents pages agents by name: ?q= searches name and description, ?status=
// is online|paused|offline, ?limit= (default 24, max 500) and ?after=<name> is the
// cursor from the previous page's next_after.
func (a *API) listAgents(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	now := a.Now()
	f := store.AgentFilter{Query: q.Get("q"), Status: domain.AgentStatus(q.Get("status")), After: q.Get("after"), Now: now}
	switch f.Status {
	case "", domain.AgentOnline, domain.AgentPaused, domain.AgentOffline:
	default:
		writeError(w, http.StatusBadRequest, "status must be online, paused or offline")
		return
	}
	if len(f.Query) > 128 || len(f.After) > 128 {
		writeError(w, http.StatusBadRequest, "q and after must be at most 128 characters")
		return
	}
	if l := q.Get("limit"); l != "" {
		n, err := strconv.Atoi(l)
		if err != nil || n < 1 {
			writeError(w, http.StatusBadRequest, "limit must be a positive integer")
			return
		}
		f.Limit = n
	}
	limit := store.ClampLimit(f.Limit, 24, 500)
	f.Limit = limit + 1 // one look-ahead row tells us whether another page exists
	page, err := a.Store.ListAgentsPage(r.Context(), f)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	more := len(page.Agents) > limit
	if more {
		page.Agents = page.Agents[:limit]
	}
	for i := range page.Agents {
		page.Agents[i].ComputeStatus(now)
		page.Agents[i].Graph = nil // the list view does not need topology; keeps responses small
	}
	resp := map[string]any{"agents": page.Agents, "total": page.Total, "next_after": nil}
	if more {
		resp["next_after"] = page.Agents[len(page.Agents)-1].Name
	}
	writeJSON(w, 200, resp)
}

func (a *API) loadAgent(w http.ResponseWriter, r *http.Request) (*domain.Agent, bool) {
	ag, err := a.Store.GetAgent(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		a.fail(w, r, err)
		return nil, false
	}
	ag.ComputeStatus(a.Now())
	return ag, true
}

func (a *API) getAgent(w http.ResponseWriter, r *http.Request) {
	ag, ok := a.loadAgent(w, r)
	if !ok {
		return
	}
	resp := map[string]any{"agent": ag, "config": nil}
	if c, err := a.Store.LatestAgentConfig(r.Context(), ag.ID); err == nil {
		resp["config"] = c
	}
	writeJSON(w, 200, resp)
}

func (a *API) enqueue(r *http.Request, agentID, runID string, typ domain.CommandType, payload any) (*domain.Command, error) {
	var raw json.RawMessage
	if payload != nil {
		raw = mustJSON(payload)
	}
	c := &domain.Command{ID: util.NewID(), AgentID: agentID, RunID: runID, Type: typ, Payload: raw,
		Status: domain.CmdPending, CreatedBy: auth.FromContext(r.Context()).Actor(), CreatedAt: a.Now()}
	if err := a.Store.CreateCommand(r.Context(), c); err != nil {
		return nil, err
	}
	a.publish(r, events.Event{Kind: events.CommandCreated, AgentID: agentID, ID: c.ID})
	return c, nil
}

func (a *API) setDesired(w http.ResponseWriter, r *http.Request, st domain.DesiredState, cmd domain.CommandType) {
	ag, ok := a.loadAgent(w, r)
	if !ok {
		return
	}
	if err := a.Store.SetAgentDesiredState(r.Context(), ag.ID, st); err != nil {
		a.fail(w, r, err)
		return
	}
	if _, err := a.enqueue(r, ag.ID, "", cmd, nil); err != nil {
		a.fail(w, r, err)
		return
	}
	a.audit(r, "agent."+string(cmd), "agent", ag.ID, map[string]string{"name": ag.Name})
	a.publish(r, events.Event{Kind: events.AgentUpdated, AgentID: ag.ID})
	ag.DesiredState = st
	ag.ComputeStatus(a.Now())
	writeJSON(w, 200, ag)
}

func (a *API) pauseAgent(w http.ResponseWriter, r *http.Request) {
	a.setDesired(w, r, domain.DesiredPaused, domain.CmdPause)
}

func (a *API) resumeAgent(w http.ResponseWriter, r *http.Request) {
	a.setDesired(w, r, domain.DesiredRunning, domain.CmdResume)
}

func (a *API) deleteAgent(w http.ResponseWriter, r *http.Request) {
	ag, ok := a.loadAgent(w, r)
	if !ok {
		return
	}
	if err := a.Store.DeleteAgent(r.Context(), ag.ID); err != nil {
		a.fail(w, r, err)
		return
	}
	a.audit(r, "agent.delete", "agent", ag.ID, map[string]string{"name": ag.Name})
	a.publish(r, events.Event{Kind: events.AgentUpdated, AgentID: ag.ID})
	w.WriteHeader(http.StatusNoContent)
}

// ---- runs ----

func (a *API) startRun(w http.ResponseWriter, r *http.Request) {
	ag, ok := a.loadAgent(w, r)
	if !ok {
		return
	}
	var req struct {
		Input    json.RawMessage `json:"input"`
		ThreadID string          `json:"thread_id"`
	}
	if err := decode(r, &req); err != nil {
		a.fail(w, r, err)
		return
	}
	if len(req.Input) == 0 {
		req.Input = json.RawMessage(`{}`)
	}
	if !json.Valid(req.Input) || len(req.Input) > maxPayloadBytes {
		writeError(w, http.StatusBadRequest, "input must be valid JSON under 64 KB")
		return
	}
	if req.ThreadID != "" && !idRe.MatchString(req.ThreadID) {
		writeError(w, http.StatusBadRequest, "invalid thread_id")
		return
	}
	if ag.DesiredState == domain.DesiredPaused {
		writeError(w, http.StatusConflict, "agent is paused; resume it before starting runs")
		return
	}
	run := &domain.Run{ID: util.NewID(), AgentID: ag.ID, AgentName: ag.Name, ThreadID: req.ThreadID,
		Status: domain.RunPending, Trigger: "ui", Input: req.Input,
		CreatedBy: auth.FromContext(r.Context()).Actor(), CreatedAt: a.Now()}
	if err := a.Store.CreateRun(r.Context(), run); err != nil {
		a.fail(w, r, err)
		return
	}
	if _, err := a.enqueue(r, ag.ID, run.ID, domain.CmdStartRun,
		map[string]any{"run_id": run.ID, "input": req.Input, "thread_id": req.ThreadID}); err != nil {
		a.fail(w, r, err)
		return
	}
	a.audit(r, "run.start", "run", run.ID, map[string]string{"agent": ag.Name})
	a.publish(r, events.Event{Kind: events.RunUpdated, AgentID: ag.ID, RunID: run.ID})
	resp := map[string]any{"run": run}
	if ag.Status == domain.AgentOffline {
		resp["warning"] = "agent is offline; the run starts when it reconnects (or expires)"
	}
	writeJSON(w, http.StatusAccepted, resp)
}

func (a *API) listRuns(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := store.RunFilter{AgentID: q.Get("agent_id"), Status: domain.RunStatus(q.Get("status")), ThreadID: q.Get("thread_id")}
	f.Limit, _ = strconv.Atoi(q.Get("limit"))
	if b := q.Get("before"); b != "" {
		t, err := time.Parse(time.RFC3339Nano, b)
		if err != nil {
			writeError(w, http.StatusBadRequest, "before must be an RFC 3339 timestamp")
			return
		}
		f.Before = t
	}
	runs, err := a.Store.ListRuns(r.Context(), f)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	for i := range runs { // list rows stay light; the detail view has full payloads
		runs[i].Input, runs[i].Output = nil, nil
	}
	resp := map[string]any{"runs": runs, "next_before": nil}
	if n := len(runs); n > 0 && n == store.ClampLimit(f.Limit, 50, 500) {
		resp["next_before"] = runs[n-1].CreatedAt.Format(time.RFC3339Nano)
	}
	writeJSON(w, 200, resp)
}

func (a *API) getRun(w http.ResponseWriter, r *http.Request) {
	run, err := a.Store.GetRun(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, 200, run)
}

func (a *API) listEvents(w http.ResponseWriter, r *http.Request) {
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	evs, err := a.Store.ListEvents(r.Context(), chi.URLParam(r, "id"), after, limit)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"events": evs})
}

func (a *API) cancelRun(w http.ResponseWriter, r *http.Request) {
	run, err := a.Store.GetRun(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		a.fail(w, r, err)
		return
	}
	if run.Status.Terminal() {
		writeError(w, http.StatusConflict, "run already finished")
		return
	}
	if run.Status == domain.RunPending {
		// Never picked up: cancel right here. The agent skips start_run for cancelled runs.
		now := a.Now()
		run.Status, run.EndedAt, run.Error = domain.RunCancelled, &now, "cancelled before start"
		if err := a.Store.UpdateRun(r.Context(), run); err != nil {
			a.fail(w, r, err)
			return
		}
	}
	if _, err := a.enqueue(r, run.AgentID, run.ID, domain.CmdCancelRun, map[string]string{"run_id": run.ID}); err != nil {
		a.fail(w, r, err)
		return
	}
	a.audit(r, "run.cancel", "run", run.ID, nil)
	a.publish(r, events.Event{Kind: events.RunUpdated, AgentID: run.AgentID, RunID: run.ID})
	writeJSON(w, http.StatusAccepted, run)
}

// ---- interrupts ----

func (a *API) listInterrupts(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	list, err := a.Store.ListInterrupts(r.Context(), domain.InterruptStatus(r.URL.Query().Get("status")), limit)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"interrupts": list})
}

func (a *API) resolveInterrupt(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Decision string          `json:"decision"` // approve | reject
		Response json.RawMessage `json:"response"`
	}
	if err := decode(r, &req); err != nil {
		a.fail(w, r, err)
		return
	}
	st := map[string]domain.InterruptStatus{"approve": domain.InterruptApproved, "reject": domain.InterruptRejected}[req.Decision]
	if st == "" {
		writeError(w, http.StatusBadRequest, "decision must be approve or reject")
		return
	}
	if len(req.Response) > 0 && (!json.Valid(req.Response) || len(req.Response) > maxPayloadBytes) {
		writeError(w, http.StatusBadRequest, "response must be valid JSON under 64 KB")
		return
	}
	p := auth.FromContext(r.Context())
	in, err := a.Store.ResolveInterrupt(r.Context(), chi.URLParam(r, "id"), st, req.Response, p.Actor(), a.Now())
	if err != nil {
		a.fail(w, r, err)
		return
	}
	if run, err := a.Store.GetRun(r.Context(), in.RunID); err == nil && run.Status == domain.RunInterrupted {
		run.Status = domain.RunRunning
		_ = a.Store.UpdateRun(r.Context(), run)
	}
	_ = a.Store.AppendEvents(r.Context(), []domain.RunEvent{{RunID: in.RunID, Type: domain.EvLog, Node: in.Node,
		Payload:   mustJSON(map[string]any{"message": fmt.Sprintf("interrupt %s by %s", req.Decision, p.Actor()), "interrupt_id": in.ID}),
		Timestamp: a.Now()}})
	a.audit(r, "interrupt."+req.Decision, "interrupt", in.ID, map[string]string{"run_id": in.RunID})
	a.publish(r, events.Event{Kind: events.InterruptUpdated, AgentID: in.AgentID, RunID: in.RunID, ID: in.ID})
	a.publish(r, events.Event{Kind: events.RunUpdated, AgentID: in.AgentID, RunID: in.RunID})
	writeJSON(w, 200, in)
}

// ---- configs ----

func (a *API) listConfigs(w http.ResponseWriter, r *http.Request) {
	list, err := a.Store.ListAgentConfigs(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"configs": list})
}

func (a *API) createConfig(w http.ResponseWriter, r *http.Request) {
	ag, ok := a.loadAgent(w, r)
	if !ok {
		return
	}
	var req struct {
		Config  json.RawMessage `json:"config"`
		Comment string          `json:"comment"`
	}
	if err := decode(r, &req); err != nil {
		a.fail(w, r, err)
		return
	}
	var obj map[string]any
	if json.Unmarshal(req.Config, &obj) != nil || len(req.Config) > maxPayloadBytes {
		writeError(w, http.StatusBadRequest, "config must be a JSON object under 64 KB")
		return
	}
	c := &domain.AgentConfig{AgentID: ag.ID, Config: req.Config, Comment: truncate(req.Comment, 500),
		CreatedBy: auth.FromContext(r.Context()).Actor(), CreatedAt: a.Now()}
	if err := a.Store.CreateAgentConfig(r.Context(), c); err != nil {
		a.fail(w, r, err)
		return
	}
	if _, err := a.enqueue(r, ag.ID, "", domain.CmdUpdateConfig, map[string]any{"version": c.Version, "config": c.Config}); err != nil {
		a.fail(w, r, err)
		return
	}
	a.audit(r, "config.update", "agent", ag.ID, map[string]any{"version": c.Version, "comment": c.Comment})
	a.publish(r, events.Event{Kind: events.AgentUpdated, AgentID: ag.ID})
	writeJSON(w, http.StatusCreated, c)
}

func (a *API) listCommands(w http.ResponseWriter, r *http.Request) {
	list, err := a.Store.ListCommands(r.Context(), chi.URLParam(r, "id"), 100)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"commands": list})
}

// ---- API keys ----

func (a *API) listKeys(w http.ResponseWriter, r *http.Request) {
	list, err := a.Store.ListAPIKeys(r.Context())
	if err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"api_keys": list})
}

func (a *API) createKey(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name          string      `json:"name"`
		Role          domain.Role `json:"role"`
		AgentName     string      `json:"agent_name"`
		ExpiresInDays int         `json:"expires_in_days"`
	}
	if err := decode(r, &req); err != nil {
		a.fail(w, r, err)
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || len(req.Name) > 100 {
		writeError(w, http.StatusBadRequest, "name is required (max 100 chars)")
		return
	}
	if !req.Role.Valid() {
		writeError(w, http.StatusBadRequest, "role must be agent, viewer, operator or admin")
		return
	}
	if req.AgentName != "" && (req.Role != domain.RoleAgent || !domain.ValidAgentName(req.AgentName)) {
		writeError(w, http.StatusBadRequest, "agent_name only applies to agent keys and must be a valid agent name")
		return
	}
	if req.ExpiresInDays < 0 || req.ExpiresInDays > 3650 {
		writeError(w, http.StatusBadRequest, "expires_in_days must be between 0 (never) and 3650")
		return
	}
	var exp *time.Time
	if req.ExpiresInDays > 0 {
		t := a.Now().Add(time.Duration(req.ExpiresInDays) * 24 * time.Hour)
		exp = &t
	}
	secret, key := auth.NewAPIKey(req.Name, req.Role, req.AgentName, auth.FromContext(r.Context()).Actor(), exp)
	if err := a.Store.CreateAPIKey(r.Context(), key); err != nil {
		a.fail(w, r, err)
		return
	}
	a.audit(r, "api_key.create", "api_key", key.ID, map[string]any{"name": key.Name, "role": key.Role, "agent": key.AgentName})
	writeJSON(w, http.StatusCreated, map[string]any{"api_key": key, "secret": secret})
}

func (a *API) revokeKey(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := a.Store.RevokeAPIKey(r.Context(), id, a.Now()); err != nil {
		a.fail(w, r, err)
		return
	}
	a.audit(r, "api_key.revoke", "api_key", id, nil)
	w.WriteHeader(http.StatusNoContent)
}

// ---- users ----

func (a *API) listUsers(w http.ResponseWriter, r *http.Request) {
	list, err := a.Store.ListUsers(r.Context())
	if err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"users": list})
}

func (a *API) updateUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Role domain.Role `json:"role"`
	}
	if err := decode(r, &req); err != nil {
		a.fail(w, r, err)
		return
	}
	if !req.Role.Valid() || req.Role == domain.RoleAgent {
		writeError(w, http.StatusBadRequest, "role must be viewer, operator or admin")
		return
	}
	id := chi.URLParam(r, "id")
	if p := auth.FromContext(r.Context()); p.Kind == auth.KindUser && p.ID == id && req.Role != domain.RoleAdmin {
		writeError(w, http.StatusBadRequest, "you cannot demote yourself; ask another admin")
		return
	}
	if err := a.Store.SetUserRole(r.Context(), id, req.Role); err != nil {
		a.fail(w, r, err)
		return
	}
	a.audit(r, "user.role", "user", id, map[string]any{"role": req.Role})
	u, err := a.Store.GetUser(r.Context(), id)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, 200, u)
}

// ---- audit ----

func (a *API) listAudit(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	before, _ := strconv.ParseInt(r.URL.Query().Get("before"), 10, 64)
	list, err := a.Store.ListAudit(r.Context(), limit, before)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"entries": list})
}
