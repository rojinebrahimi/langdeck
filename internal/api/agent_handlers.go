package api

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/rojinebrahimi/langdeck/internal/auth"
	"github.com/rojinebrahimi/langdeck/internal/domain"
	"github.com/rojinebrahimi/langdeck/internal/events"
	"github.com/rojinebrahimi/langdeck/internal/store"
)

const (
	maxEventsPerBatch = 500
	maxPayloadBytes   = 64 << 10
	maxGraphBytes     = 512 << 10
	maxPollWait       = 30 * time.Second
)

var (
	idRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

	eventsIngested = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "langdeck_events_ingested_total", Help: "Run events ingested, by type.",
	}, []string{"type"})
	commandsDelivered = promauto.NewCounter(prometheus.CounterOpts{
		Name: "langdeck_commands_delivered_total", Help: "Commands handed to agents.",
	})
)

// agentFor resolves the agent named in a request and checks that the API key
// is allowed to act for it.
func (a *API) agentFor(r *http.Request, name string) (*domain.Agent, error) {
	p := auth.FromContext(r.Context())
	if p.AgentName != "" && p.AgentName != name {
		return nil, domain.ErrForbidden
	}
	return a.Store.GetAgentByName(r.Context(), name)
}

type registerReq struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Version     string            `json:"version"`
	Framework   string            `json:"framework"`
	SDKVersion  string            `json:"sdk_version"`
	Graph       json.RawMessage   `json:"graph"`
	Labels      map[string]string `json:"labels"`
	Accesses    []domain.Access   `json:"accesses"`
	DependsOn   []string          `json:"depends_on"`
}

func (a *API) agentRegister(w http.ResponseWriter, r *http.Request) {
	var req registerReq
	if err := decode(r, &req); err != nil {
		a.fail(w, r, err)
		return
	}
	if !domain.ValidAgentName(req.Name) {
		writeError(w, http.StatusBadRequest, "name must be lowercase letters, digits, '-', '_' or '.', max 63 chars")
		return
	}
	if p := auth.FromContext(r.Context()); p.AgentName != "" && p.AgentName != req.Name {
		writeError(w, http.StatusForbidden, "this API key is bound to another agent")
		return
	}
	if len(req.Graph) > maxGraphBytes || (len(req.Graph) > 0 && !json.Valid(req.Graph)) {
		writeError(w, http.StatusBadRequest, "graph must be valid JSON under 512 KB")
		return
	}
	if len(req.Labels) > 32 || len(req.Description) > 2000 || len(req.Version) > 64 {
		writeError(w, http.StatusBadRequest, "too many labels or field too long")
		return
	}
	accesses, err := domain.NormalizeAccesses(req.Accesses)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	dependsOn, err := domain.NormalizeDependsOn(req.Name, req.DependsOn)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Framework == "" {
		req.Framework = "langgraph"
	}
	ag, err := a.Store.RegisterAgent(r.Context(), store.AgentRegistration{
		Name: req.Name, Description: req.Description, Version: req.Version, Framework: req.Framework,
		SDKVersion: req.SDKVersion, Graph: req.Graph, Labels: req.Labels, Accesses: accesses, DependsOn: dependsOn,
	}, a.Now())
	if err != nil {
		a.fail(w, r, err)
		return
	}
	ag.ComputeStatus(a.Now())
	a.publish(r, events.Event{Kind: events.AgentUpdated, AgentID: ag.ID})

	resp := map[string]any{"agent": ag, "desired_state": ag.DesiredState, "config": nil, "config_version": 0}
	if c, err := a.Store.LatestAgentConfig(r.Context(), ag.ID); err == nil {
		resp["config"], resp["config_version"] = c.Config, c.Version
	}
	writeJSON(w, 200, resp)
}

func (a *API) agentHeartbeat(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Agent     string `json:"agent"`
		Instances int    `json:"instances"`
	}
	if err := decode(r, &req); err != nil {
		a.fail(w, r, err)
		return
	}
	ag, err := a.agentFor(r, req.Agent)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	wasOffline := ag.LastHeartbeat == nil || a.Now().Sub(*ag.LastHeartbeat) > domain.HeartbeatTTL
	if err := a.Store.Heartbeat(r.Context(), ag.ID, req.Instances, a.Now()); err != nil {
		a.fail(w, r, err)
		return
	}
	if wasOffline {
		a.publish(r, events.Event{Kind: events.AgentUpdated, AgentID: ag.ID})
	}
	version := 0
	if c, err := a.Store.LatestAgentConfig(r.Context(), ag.ID); err == nil {
		version = c.Version
	}
	writeJSON(w, 200, map[string]any{"desired_state": ag.DesiredState, "config_version": version})
}

// ingestEvent is the wire format the SDK sends.
type ingestEvent struct {
	RunID     string           `json:"run_id"`
	ThreadID  string           `json:"thread_id"`
	Type      domain.EventType `json:"type"`
	Node      string           `json:"node"`
	Payload   json.RawMessage  `json:"payload"`
	LatencyMS int64            `json:"latency_ms"`
	Timestamp time.Time        `json:"ts"`
}

func (a *API) agentEvents(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Agent  string        `json:"agent"`
		Events []ingestEvent `json:"events"`
	}
	if err := decode(r, &req); err != nil {
		a.fail(w, r, err)
		return
	}
	if len(req.Events) == 0 || len(req.Events) > maxEventsPerBatch {
		writeError(w, http.StatusBadRequest, "send between 1 and 500 events per batch")
		return
	}
	ag, err := a.agentFor(r, req.Agent)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	now := a.Now()
	for i, e := range req.Events {
		if !idRe.MatchString(e.RunID) || !e.Type.Valid() || len(e.Node) > 128 || len(e.ThreadID) > 128 {
			writeError(w, http.StatusBadRequest, "event "+strconv.Itoa(i)+": invalid run_id, type, node or thread_id")
			return
		}
		if len(e.Payload) > 0 && !json.Valid(e.Payload) {
			writeError(w, http.StatusBadRequest, "event "+strconv.Itoa(i)+": payload is not valid JSON")
			return
		}
	}

	// Group by run so each run is loaded and saved once per batch.
	order := []string{}
	byRun := map[string][]ingestEvent{}
	for _, e := range req.Events {
		if _, ok := byRun[e.RunID]; !ok {
			order = append(order, e.RunID)
		}
		byRun[e.RunID] = append(byRun[e.RunID], e)
	}

	for _, runID := range order {
		evs := byRun[runID]
		first := evs[0]
		ts := clampTS(first.Timestamp, now)
		run, err := a.Store.EnsureRun(r.Context(), &domain.Run{
			ID: runID, AgentID: ag.ID, AgentName: ag.Name, ThreadID: first.ThreadID, Status: domain.RunRunning,
			Trigger: "sdk", Input: inputOf(first), CreatedAt: ts, StartedAt: &ts,
		})
		if err != nil {
			a.fail(w, r, err)
			return
		}
		run.AgentName = ag.Name
		wasFailed := run.Status == domain.RunFailed
		stored := make([]domain.RunEvent, 0, len(evs))
		for _, e := range evs {
			ts := clampTS(e.Timestamp, now)
			applyEvent(run, e, ts)
			stored = append(stored, domain.RunEvent{RunID: runID, Type: e.Type, Node: e.Node,
				Payload: capPayload(e.Payload), LatencyMS: e.LatencyMS, Timestamp: ts})
			eventsIngested.WithLabelValues(string(e.Type)).Inc()
		}
		if err := a.Store.AppendEvents(r.Context(), stored); err != nil {
			a.fail(w, r, err)
			return
		}
		if err := a.Store.UpdateRun(r.Context(), run); err != nil {
			a.fail(w, r, err)
			return
		}
		a.publish(r, events.Event{Kind: events.RunUpdated, AgentID: ag.ID, RunID: runID})
		if !wasFailed && run.Status == domain.RunFailed {
			a.notifyRunFailed(ag, run)
		}
	}
	writeJSON(w, http.StatusAccepted, map[string]int{"accepted": len(req.Events)})
}

// applyEvent moves the run's state machine forward. Terminal runs only collect events.
func applyEvent(run *domain.Run, e ingestEvent, ts time.Time) {
	var p struct {
		Output    json.RawMessage `json:"output"`
		Error     string          `json:"error"`
		Cancelled bool            `json:"cancelled"`
		Usage     struct {
			PromptTokens     int64 `json:"prompt_tokens"`
			CompletionTokens int64 `json:"completion_tokens"`
		} `json:"usage"`
	}
	_ = json.Unmarshal(e.Payload, &p)
	if e.ThreadID != "" && run.ThreadID == "" {
		run.ThreadID = e.ThreadID
	}
	if e.Type == domain.EvLLMEnd {
		run.PromptTokens += max(p.Usage.PromptTokens, 0)
		run.CompletionTokens += max(p.Usage.CompletionTokens, 0)
	}
	if run.Status.Terminal() {
		return
	}
	switch e.Type {
	case domain.EvRunStart:
		if run.StartedAt == nil || run.Status == domain.RunPending {
			run.StartedAt = &ts
		}
		run.Status = domain.RunRunning
	case domain.EvNodeStart:
		run.Status = domain.RunRunning
	case domain.EvInterrupt:
		run.Status = domain.RunInterrupted
	case domain.EvRunEnd:
		run.Status, run.EndedAt = domain.RunSucceeded, &ts
		if len(p.Output) > 0 {
			run.Output = capPayload(p.Output)
		}
	case domain.EvRunError:
		run.Status, run.EndedAt, run.Error = domain.RunFailed, &ts, truncate(p.Error, 4000)
		if p.Cancelled {
			run.Status = domain.RunCancelled
		}
	}
}

func inputOf(e ingestEvent) json.RawMessage {
	if e.Type != domain.EvRunStart {
		return nil
	}
	var p struct {
		Input json.RawMessage `json:"input"`
	}
	_ = json.Unmarshal(e.Payload, &p)
	return capPayload(p.Input)
}

// clampTS keeps agent clocks from writing events far in the future or the zero time.
func clampTS(ts, now time.Time) time.Time {
	if ts.IsZero() || ts.After(now.Add(time.Minute)) {
		return now
	}
	return ts.UTC()
}

// capPayload replaces oversized payloads with a marker so one noisy node cannot bloat the database.
func capPayload(p json.RawMessage) json.RawMessage {
	if len(p) <= maxPayloadBytes {
		return p
	}
	b, _ := json.Marshal(map[string]any{"_truncated": true, "_size_bytes": len(p)})
	return b
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

// agentPollCommands is the pull-based control channel. Agents long-poll here, so
// they never need to accept inbound connections: they work behind NAT,
// firewalls and service meshes with outbound HTTPS only.
func (a *API) agentPollCommands(w http.ResponseWriter, r *http.Request) {
	ag, err := a.agentFor(r, r.URL.Query().Get("agent"))
	if err != nil {
		a.fail(w, r, err)
		return
	}
	wait, _ := time.ParseDuration(r.URL.Query().Get("wait"))
	wait = min(max(wait, 0), maxPollWait)
	_ = a.Store.Heartbeat(r.Context(), ag.ID, 0, a.Now()) // polling proves liveness

	sub, cancel := a.Broker.Subscribe()
	defer cancel()
	deadline := time.NewTimer(wait)
	defer deadline.Stop()
	tick := time.NewTicker(a.PollInterval)
	defer tick.Stop()

	for {
		cmds, err := a.Store.ClaimCommands(r.Context(), ag.ID, 20, a.Now())
		if err != nil {
			a.fail(w, r, err)
			return
		}
		if len(cmds) > 0 || wait == 0 {
			commandsDelivered.Add(float64(len(cmds)))
			writeJSON(w, 200, map[string]any{"commands": cmds})
			return
		}
	waitLoop:
		for {
			select {
			case <-r.Context().Done():
				return
			case <-deadline.C:
				writeJSON(w, 200, map[string]any{"commands": []domain.Command{}})
				return
			case <-tick.C:
				break waitLoop
			case e := <-sub:
				if e.Kind == events.CommandCreated && e.AgentID == ag.ID {
					break waitLoop
				}
			}
		}
	}
}

func (a *API) agentAckCommand(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Agent  string `json:"agent"`
		OK     bool   `json:"ok"`
		Result string `json:"result"`
	}
	if err := decode(r, &req); err != nil {
		a.fail(w, r, err)
		return
	}
	ag, err := a.agentFor(r, req.Agent)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	if err := a.Store.AckCommand(r.Context(), ag.ID, chi.URLParam(r, "id"), req.OK, truncate(req.Result, 2000), a.Now()); err != nil {
		a.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) agentCreateInterrupt(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Agent    string          `json:"agent"`
		ID       string          `json:"id"`
		RunID    string          `json:"run_id"`
		Node     string          `json:"node"`
		Question string          `json:"question"`
		Payload  json.RawMessage `json:"payload"`
	}
	if err := decode(r, &req); err != nil {
		a.fail(w, r, err)
		return
	}
	if !idRe.MatchString(req.ID) || !idRe.MatchString(req.RunID) || len(req.Node) > 128 || len(req.Question) > 4000 {
		writeError(w, http.StatusBadRequest, "invalid id, run_id, node or question")
		return
	}
	if len(req.Payload) > 0 && !json.Valid(req.Payload) {
		writeError(w, http.StatusBadRequest, "payload is not valid JSON")
		return
	}
	ag, err := a.agentFor(r, req.Agent)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	now := a.Now()
	run, err := a.Store.EnsureRun(r.Context(), &domain.Run{ID: req.RunID, AgentID: ag.ID, Status: domain.RunRunning,
		Trigger: "sdk", CreatedAt: now, StartedAt: &now})
	if err != nil {
		a.fail(w, r, err)
		return
	}
	in := &domain.Interrupt{ID: req.ID, RunID: req.RunID, AgentID: ag.ID, AgentName: ag.Name, Node: req.Node,
		Question: req.Question, Payload: capPayload(req.Payload), Status: domain.InterruptPending, CreatedAt: now}
	if err := a.Store.CreateInterrupt(r.Context(), in); err != nil {
		a.fail(w, r, err)
		return
	}
	if !run.Status.Terminal() {
		run.Status = domain.RunInterrupted
		_ = a.Store.UpdateRun(r.Context(), run)
	}
	_ = a.Store.AppendEvents(r.Context(), []domain.RunEvent{{RunID: run.ID, Type: domain.EvInterrupt, Node: req.Node,
		Payload: mustJSON(map[string]any{"interrupt_id": in.ID, "question": in.Question}), Timestamp: now}})
	a.publish(r, events.Event{Kind: events.InterruptUpdated, AgentID: ag.ID, RunID: run.ID, ID: in.ID})
	a.publish(r, events.Event{Kind: events.RunUpdated, AgentID: ag.ID, RunID: run.ID})
	a.notifyApproval(ag, in)
	writeJSON(w, http.StatusCreated, in)
}

// agentWaitInterrupt long-polls until a human resolves the interrupt or the wait elapses.
func (a *API) agentWaitInterrupt(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	wait, _ := time.ParseDuration(r.URL.Query().Get("wait"))
	wait = min(max(wait, 0), maxPollWait)

	sub, cancel := a.Broker.Subscribe()
	defer cancel()
	deadline := time.After(wait)
	tick := time.NewTicker(a.PollInterval)
	defer tick.Stop()
	for {
		in, err := a.Store.GetInterrupt(r.Context(), id)
		if err != nil {
			a.fail(w, r, err)
			return
		}
		if _, err := a.agentFor(r, in.AgentName); err != nil {
			a.fail(w, r, err)
			return
		}
		if in.Status != domain.InterruptPending || wait == 0 {
			writeJSON(w, 200, in)
			return
		}
	waitLoop:
		for {
			select {
			case <-r.Context().Done():
				return
			case <-deadline:
				writeJSON(w, 200, in)
				return
			case <-tick.C:
				break waitLoop
			case e := <-sub:
				if e.Kind == events.InterruptUpdated && e.ID == id {
					break waitLoop
				}
			}
		}
	}
}

func mustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}
