// Package memory is an in-process Store. It is used by tests and by the
// zero-dependency demo mode. Data is lost on restart.
package memory

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rojinebrahimi/langdeck/internal/domain"
	"github.com/rojinebrahimi/langdeck/internal/store"
	"github.com/rojinebrahimi/langdeck/internal/util"
)

// Store keeps everything in maps behind one mutex. Simplicity over speed.
type Store struct {
	mu         sync.Mutex
	users      map[string]*domain.User
	sessions   map[string]*domain.Session
	keys       map[string]*domain.APIKey
	agents     map[string]*domain.Agent
	configs    map[string][]domain.AgentConfig
	runs       map[string]*domain.Run
	events     map[string][]domain.RunEvent
	commands   []*domain.Command
	interrupts map[string]*domain.Interrupt
	audit      []domain.AuditEntry
	eventSeq   int64
	auditSeq   int64
}

var _ store.Store = (*Store)(nil)

// New returns an empty store.
func New() *Store {
	return &Store{
		users:      map[string]*domain.User{},
		sessions:   map[string]*domain.Session{},
		keys:       map[string]*domain.APIKey{},
		agents:     map[string]*domain.Agent{},
		configs:    map[string][]domain.AgentConfig{},
		runs:       map[string]*domain.Run{},
		events:     map[string][]domain.RunEvent{},
		interrupts: map[string]*domain.Interrupt{},
	}
}

func (s *Store) Ping(context.Context) error { return nil }
func (s *Store) Close()                     {}

func cp[T any](v *T) *T { c := *v; return &c }

// agentName mirrors the JOIN the SQL store does. Callers hold s.mu.
func (s *Store) agentName(id string) string {
	if a, ok := s.agents[id]; ok {
		return a.Name
	}
	return ""
}

func (s *Store) run(r *domain.Run) *domain.Run {
	c := cp(r)
	c.AgentName = s.agentName(r.AgentID)
	return c
}

func (s *Store) interrupt(i *domain.Interrupt) *domain.Interrupt {
	c := cp(i)
	c.AgentName = s.agentName(i.AgentID)
	return c
}

// ---- users ----

func (s *Store) UpsertUserOnLogin(_ context.Context, subject, email, name string, defaultRole domain.Role) (*domain.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	for _, u := range s.users {
		if u.Subject == subject {
			u.Email, u.Name, u.LastLoginAt = email, name, &now
			return cp(u), nil
		}
	}
	u := &domain.User{ID: util.NewID(), Subject: subject, Email: email, Name: name, Role: defaultRole, CreatedAt: now, LastLoginAt: &now}
	s.users[u.ID] = u
	return cp(u), nil
}

func (s *Store) GetUser(_ context.Context, id string) (*domain.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return cp(u), nil
}

func (s *Store) ListUsers(context.Context) ([]domain.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]domain.User, 0, len(s.users))
	for _, u := range s.users {
		out = append(out, *u)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (s *Store) SetUserRole(_ context.Context, id string, role domain.Role) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[id]
	if !ok {
		return domain.ErrNotFound
	}
	u.Role = role
	return nil
}

func (s *Store) CreateUser(_ context.Context, u *domain.User) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, x := range s.users {
		if x.Subject == u.Subject {
			return domain.ErrConflict
		}
	}
	u.Local = strings.HasPrefix(u.Subject, "local|")
	s.users[u.ID] = cp(u)
	return nil
}

func (s *Store) GetUserBySubject(_ context.Context, subject string) (*domain.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, u := range s.users {
		if u.Subject == subject {
			return cp(u), nil
		}
	}
	return nil, domain.ErrNotFound
}

func (s *Store) SetPassword(_ context.Context, id, hash string, mustChange bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[id]
	if !ok {
		return domain.ErrNotFound
	}
	u.PasswordHash, u.MustChangePassword = hash, mustChange
	return nil
}

// ---- sessions ----

func (s *Store) CreateSession(_ context.Context, sess *domain.Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[sess.ID] = cp(sess)
	return nil
}

func (s *Store) GetSession(_ context.Context, id string) (*domain.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return cp(sess), nil
}

func (s *Store) DeleteSession(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, id)
	return nil
}

func (s *Store) DeleteExpiredSessions(_ context.Context, now time.Time) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var n int64
	for id, sess := range s.sessions {
		if now.After(sess.ExpiresAt) {
			delete(s.sessions, id)
			n++
		}
	}
	return n, nil
}

// ---- api keys ----

func (s *Store) CreateAPIKey(_ context.Context, k *domain.APIKey) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.keys[k.ID] = cp(k)
	return nil
}

func (s *Store) GetAPIKeyByHash(_ context.Context, hash string) (*domain.APIKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, k := range s.keys {
		if k.Hash == hash {
			return cp(k), nil
		}
	}
	return nil, domain.ErrNotFound
}

func (s *Store) ListAPIKeys(context.Context) ([]domain.APIKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]domain.APIKey, 0, len(s.keys))
	for _, k := range s.keys {
		out = append(out, *k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

func (s *Store) RevokeAPIKey(_ context.Context, id string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	k, ok := s.keys[id]
	if !ok {
		return domain.ErrNotFound
	}
	if k.RevokedAt == nil {
		k.RevokedAt = &at
	}
	return nil
}

func (s *Store) TouchAPIKey(_ context.Context, id string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if k, ok := s.keys[id]; ok {
		k.LastUsedAt = &at
	}
	return nil
}

// ---- agents ----

func (s *Store) RegisterAgent(_ context.Context, reg store.AgentRegistration, now time.Time) (*domain.Agent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	labels := reg.Labels
	if labels == nil {
		labels = map[string]string{}
	}
	for _, a := range s.agents {
		if a.Name == reg.Name {
			a.Description, a.Version, a.Framework, a.SDKVersion = reg.Description, reg.Version, reg.Framework, reg.SDKVersion
			if len(reg.Graph) > 0 {
				a.Graph = reg.Graph
			}
			a.Labels, a.LastHeartbeat, a.UpdatedAt = labels, &now, now
			if a.Instances < 1 {
				a.Instances = 1
			}
			return cp(a), nil
		}
	}
	a := &domain.Agent{
		ID: util.NewID(), Name: reg.Name, Description: reg.Description, Version: reg.Version,
		Framework: reg.Framework, SDKVersion: reg.SDKVersion, Graph: reg.Graph, Labels: labels,
		DesiredState: domain.DesiredRunning, Instances: 1, LastHeartbeat: &now, CreatedAt: now, UpdatedAt: now,
	}
	s.agents[a.ID] = a
	return cp(a), nil
}

func (s *Store) Heartbeat(_ context.Context, id string, instances int, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.agents[id]
	if !ok {
		return domain.ErrNotFound
	}
	a.LastHeartbeat = &now
	if instances > 0 {
		a.Instances = instances
	}
	return nil
}

func (s *Store) GetAgent(_ context.Context, id string) (*domain.Agent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.agents[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return cp(a), nil
}

func (s *Store) GetAgentByName(_ context.Context, name string) (*domain.Agent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, a := range s.agents {
		if a.Name == name {
			return cp(a), nil
		}
	}
	return nil, domain.ErrNotFound
}

func (s *Store) ListAgents(context.Context) ([]domain.Agent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]domain.Agent, 0, len(s.agents))
	for _, a := range s.agents {
		out = append(out, *a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (s *Store) ListAgentsPage(ctx context.Context, f store.AgentFilter) (*store.AgentPage, error) {
	all, err := s.ListAgents(ctx) // sorted by name
	if err != nil {
		return nil, err
	}
	q := strings.ToLower(strings.TrimSpace(f.Query))
	limit := store.ClampLimit(f.Limit, 24, store.MaxAgentPage)
	page := &store.AgentPage{Agents: []domain.Agent{}}
	for _, a := range all {
		if q != "" && !strings.Contains(strings.ToLower(a.Name), q) && !strings.Contains(strings.ToLower(a.Description), q) {
			continue
		}
		if f.Status != "" {
			a.ComputeStatus(f.Now)
			if a.Status != f.Status {
				continue
			}
		}
		page.Total++
		if a.Name > f.After && len(page.Agents) < limit {
			page.Agents = append(page.Agents, a)
		}
	}
	return page, nil
}

func (s *Store) SetAgentDesiredState(_ context.Context, id string, st domain.DesiredState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.agents[id]
	if !ok {
		return domain.ErrNotFound
	}
	a.DesiredState, a.UpdatedAt = st, time.Now().UTC()
	return nil
}

func (s *Store) DeleteAgent(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.agents[id]; !ok {
		return domain.ErrNotFound
	}
	delete(s.agents, id)
	delete(s.configs, id)
	for rid, r := range s.runs {
		if r.AgentID == id {
			delete(s.runs, rid)
			delete(s.events, rid)
		}
	}
	kept := s.commands[:0]
	for _, c := range s.commands {
		if c.AgentID != id {
			kept = append(kept, c)
		}
	}
	s.commands = kept
	for iid, i := range s.interrupts {
		if i.AgentID == id {
			delete(s.interrupts, iid)
		}
	}
	return nil
}

// ---- configs ----

func (s *Store) CreateAgentConfig(_ context.Context, c *domain.AgentConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	list := s.configs[c.AgentID]
	c.Version = len(list) + 1
	s.configs[c.AgentID] = append(list, *c)
	return nil
}

func (s *Store) ListAgentConfigs(_ context.Context, agentID string) ([]domain.AgentConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	list := s.configs[agentID]
	out := make([]domain.AgentConfig, len(list))
	for i := range list {
		out[len(list)-1-i] = list[i]
	}
	return out, nil
}

func (s *Store) LatestAgentConfig(_ context.Context, agentID string) (*domain.AgentConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	list := s.configs[agentID]
	if len(list) == 0 {
		return nil, domain.ErrNotFound
	}
	c := list[len(list)-1]
	return &c, nil
}

// ---- runs ----

func (s *Store) CreateRun(_ context.Context, r *domain.Run) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.runs[r.ID]; ok {
		return domain.ErrConflict
	}
	s.runs[r.ID] = cp(r)
	return nil
}

func (s *Store) EnsureRun(_ context.Context, r *domain.Run) (*domain.Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ex, ok := s.runs[r.ID]; ok {
		if ex.AgentID != r.AgentID {
			return nil, domain.ErrConflict
		}
		return s.run(ex), nil
	}
	s.runs[r.ID] = cp(r)
	return s.run(r), nil
}

func (s *Store) GetRun(_ context.Context, id string) (*domain.Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.runs[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return s.run(r), nil
}

func (s *Store) ListRuns(_ context.Context, f store.RunFilter) ([]domain.Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	limit := store.ClampLimit(f.Limit, 50, 500)
	out := []domain.Run{}
	for _, r := range s.runs {
		if (f.AgentID != "" && r.AgentID != f.AgentID) || (f.Status != "" && r.Status != f.Status) ||
			(f.ThreadID != "" && r.ThreadID != f.ThreadID) || (!f.Before.IsZero() && !r.CreatedAt.Before(f.Before)) {
			continue
		}
		out = append(out, *s.run(r))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *Store) UpdateRun(_ context.Context, r *domain.Run) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.runs[r.ID]; !ok {
		return domain.ErrNotFound
	}
	s.runs[r.ID] = cp(r)
	return nil
}

func (s *Store) AppendEvents(_ context.Context, evs []domain.RunEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range evs {
		s.eventSeq++
		evs[i].ID = s.eventSeq
		s.events[evs[i].RunID] = append(s.events[evs[i].RunID], evs[i])
	}
	return nil
}

func (s *Store) ListEvents(_ context.Context, runID string, afterID int64, limit int) ([]domain.RunEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	limit = store.ClampLimit(limit, 1000, 5000)
	out := []domain.RunEvent{}
	for _, e := range s.events[runID] {
		if e.ID > afterID {
			out = append(out, e)
			if len(out) == limit {
				break
			}
		}
	}
	return out, nil
}

func (s *Store) PurgeRunsBefore(_ context.Context, t time.Time) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var n int64
	for id, r := range s.runs {
		if r.CreatedAt.Before(t) && r.Status.Terminal() {
			delete(s.runs, id)
			delete(s.events, id)
			n++
		}
	}
	return n, nil
}

// ---- commands ----

func (s *Store) CreateCommand(_ context.Context, c *domain.Command) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.commands = append(s.commands, cp(c))
	return nil
}

func (s *Store) ClaimCommands(_ context.Context, agentID string, limit int, now time.Time) ([]domain.Command, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []domain.Command{}
	for _, c := range s.commands {
		if len(out) >= limit {
			break
		}
		if c.AgentID != agentID {
			continue
		}
		stale := c.Status == domain.CmdDelivered && c.DeliveredAt != nil && now.Sub(*c.DeliveredAt) > store.RedeliverAfter
		if c.Status == domain.CmdPending || stale {
			c.Status, c.DeliveredAt = domain.CmdDelivered, &now
			out = append(out, *c)
		}
	}
	return out, nil
}

func (s *Store) AckCommand(_ context.Context, agentID, id string, ok bool, result string, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.commands {
		if c.ID == id && c.AgentID == agentID {
			c.Status = domain.CmdAcked
			if !ok {
				c.Status = domain.CmdFailed
			}
			c.Result, c.AckedAt = result, &now
			return nil
		}
	}
	return domain.ErrNotFound
}

func (s *Store) ListCommands(_ context.Context, agentID string, limit int) ([]domain.Command, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	limit = store.ClampLimit(limit, 50, 500)
	out := []domain.Command{}
	for i := len(s.commands) - 1; i >= 0 && len(out) < limit; i-- {
		if c := s.commands[i]; agentID == "" || c.AgentID == agentID {
			out = append(out, *c)
		}
	}
	return out, nil
}

func (s *Store) ExpireCommands(_ context.Context, olderThan time.Time) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var n int64
	for _, c := range s.commands {
		if (c.Status == domain.CmdPending || c.Status == domain.CmdDelivered) && c.CreatedAt.Before(olderThan) {
			c.Status = domain.CmdExpired
			n++
		}
	}
	return n, nil
}

// ---- interrupts ----

func (s *Store) CreateInterrupt(_ context.Context, i *domain.Interrupt) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.interrupts[i.ID]; ok {
		return domain.ErrConflict
	}
	s.interrupts[i.ID] = cp(i)
	return nil
}

func (s *Store) GetInterrupt(_ context.Context, id string) (*domain.Interrupt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	i, ok := s.interrupts[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return s.interrupt(i), nil
}

func (s *Store) ListInterrupts(_ context.Context, st domain.InterruptStatus, limit int) ([]domain.Interrupt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	limit = store.ClampLimit(limit, 50, 500)
	out := []domain.Interrupt{}
	for _, i := range s.interrupts {
		if st == "" || i.Status == st {
			out = append(out, *s.interrupt(i))
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a].CreatedAt.After(out[b].CreatedAt) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *Store) ResolveInterrupt(_ context.Context, id string, st domain.InterruptStatus, resp json.RawMessage, by string, now time.Time) (*domain.Interrupt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	i, ok := s.interrupts[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	if i.Status != domain.InterruptPending {
		return nil, domain.ErrConflict
	}
	i.Status, i.Response, i.ResolvedBy, i.ResolvedAt = st, resp, by, &now
	return s.interrupt(i), nil
}

// ---- audit ----

func (s *Store) AppendAudit(_ context.Context, e *domain.AuditEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.auditSeq++
	e.ID = s.auditSeq
	s.audit = append(s.audit, *e)
	return nil
}

func (s *Store) ListAudit(_ context.Context, limit int, beforeID int64) ([]domain.AuditEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	limit = store.ClampLimit(limit, 100, 1000)
	out := []domain.AuditEntry{}
	for i := len(s.audit) - 1; i >= 0 && len(out) < limit; i-- {
		if beforeID == 0 || s.audit[i].ID < beforeID {
			out = append(out, s.audit[i])
		}
	}
	return out, nil
}

// ---- stats ----

func (s *Store) Stats(_ context.Context, now time.Time) (*domain.Stats, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	agents := make([]domain.Agent, 0, len(s.agents))
	for _, a := range s.agents {
		agents = append(agents, *a)
	}
	runs := []domain.Run{}
	since := now.Add(-24 * time.Hour)
	for _, r := range s.runs {
		if r.CreatedAt.After(since) {
			runs = append(runs, *s.run(r))
		}
	}
	pending := 0
	for _, i := range s.interrupts {
		if i.Status == domain.InterruptPending {
			pending++
		}
	}
	return store.ComputeStats(agents, runs, pending, now), nil
}
