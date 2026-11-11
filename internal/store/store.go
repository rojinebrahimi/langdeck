// Package store defines the persistence contract. Two implementations exist:
// memory (tests, local demo) and postgres (production).
package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/rojinebrahimi/langdeck/internal/domain"
)

// RunFilter narrows ListRuns. Zero values mean "no filter".
type RunFilter struct {
	AgentID  string
	Status   domain.RunStatus
	ThreadID string
	Before   time.Time // cursor: only runs created strictly before this time
	Limit    int
}

// AgentFilter narrows and pages ListAgentsPage. Zero values mean "no filter".
type AgentFilter struct {
	Query  string             // case-insensitive substring of the name or description
	Status domain.AgentStatus // online, paused or offline, evaluated at Now
	After  string             // cursor: only agents whose name sorts strictly after this
	Limit  int
	Now    time.Time // reference time for Status; required when Status is set
}

// MaxAgentPage caps a single ListAgentsPage call. It is one above the API's
// largest page (500) so the API can fetch a look-ahead row and know whether more exist.
const MaxAgentPage = 501

// AgentPage is one page of agents plus the number matching the filter across all pages.
type AgentPage struct {
	Agents []domain.Agent
	Total  int
}

// AgentRegistration is what an SDK instance reports when it starts.
type AgentRegistration struct {
	Name        string
	Description string
	Version     string
	Framework   string
	SDKVersion  string
	Graph       json.RawMessage
	Labels      map[string]string
	// Accesses and DependsOn keep the stored value when nil (an older SDK that does not
	// report them) and replace it when non-nil, so an empty slice clears it.
	Accesses  []domain.Access
	DependsOn []string
}

// RedeliverAfter is how long a delivered but un-acked command waits before
// it is handed out again (for example after an agent crash).
const RedeliverAfter = 2 * time.Minute

// Store is the full persistence contract used by the API layer.
type Store interface {
	Ping(ctx context.Context) error
	Close()

	// Users
	UpsertUserOnLogin(ctx context.Context, subject, email, name string, defaultRole domain.Role) (*domain.User, error)
	GetUser(ctx context.Context, id string) (*domain.User, error)
	ListUsers(ctx context.Context) ([]domain.User, error)
	SetUserRole(ctx context.Context, id string, role domain.Role) error
	CreateUser(ctx context.Context, u *domain.User) error
	GetUserBySubject(ctx context.Context, subject string) (*domain.User, error)
	SetPassword(ctx context.Context, id, hash string, mustChange bool) error

	// Sessions
	CreateSession(ctx context.Context, s *domain.Session) error
	GetSession(ctx context.Context, id string) (*domain.Session, error)
	DeleteSession(ctx context.Context, id string) error
	DeleteExpiredSessions(ctx context.Context, now time.Time) (int64, error)

	// API keys
	CreateAPIKey(ctx context.Context, k *domain.APIKey) error
	GetAPIKeyByHash(ctx context.Context, hash string) (*domain.APIKey, error)
	ListAPIKeys(ctx context.Context) ([]domain.APIKey, error)
	RevokeAPIKey(ctx context.Context, id string, at time.Time) error
	TouchAPIKey(ctx context.Context, id string, at time.Time) error

	// Agents
	RegisterAgent(ctx context.Context, reg AgentRegistration, now time.Time) (*domain.Agent, error)
	Heartbeat(ctx context.Context, agentID string, instances int, now time.Time) error
	GetAgent(ctx context.Context, id string) (*domain.Agent, error)
	GetAgentByName(ctx context.Context, name string) (*domain.Agent, error)
	ListAgents(ctx context.Context) ([]domain.Agent, error)
	// ListAgentsPage returns agents ordered by name, keyset-paginated by name.
	ListAgentsPage(ctx context.Context, f AgentFilter) (*AgentPage, error)
	SetAgentDesiredState(ctx context.Context, id string, st domain.DesiredState) error
	DeleteAgent(ctx context.Context, id string) error

	// GetSetting and SetSetting hold small JSON documents keyed by name (e.g. notification
	// channels). GetSetting returns ErrNotFound when the key was never set.
	GetSetting(ctx context.Context, key string) (json.RawMessage, error)
	SetSetting(ctx context.Context, key string, value json.RawMessage, now time.Time) error
	// SetAgentImage stores (or replaces) the agent's picture; DeleteAgentImage removes it.
	SetAgentImage(ctx context.Context, id, contentType string, data []byte, now time.Time) error
	AgentImage(ctx context.Context, id string) (contentType string, data []byte, err error)
	DeleteAgentImage(ctx context.Context, id string) error

	// Agent configs (append-only versions)
	CreateAgentConfig(ctx context.Context, c *domain.AgentConfig) error
	ListAgentConfigs(ctx context.Context, agentID string) ([]domain.AgentConfig, error)
	LatestAgentConfig(ctx context.Context, agentID string) (*domain.AgentConfig, error)

	// Runs and events
	CreateRun(ctx context.Context, r *domain.Run) error
	EnsureRun(ctx context.Context, r *domain.Run) (*domain.Run, error)
	GetRun(ctx context.Context, id string) (*domain.Run, error)
	ListRuns(ctx context.Context, f RunFilter) ([]domain.Run, error)
	UpdateRun(ctx context.Context, r *domain.Run) error
	AppendEvents(ctx context.Context, evs []domain.RunEvent) error
	ListEvents(ctx context.Context, runID string, afterID int64, limit int) ([]domain.RunEvent, error)
	PurgeRunsBefore(ctx context.Context, t time.Time) (int64, error)

	// Commands
	CreateCommand(ctx context.Context, c *domain.Command) error
	ClaimCommands(ctx context.Context, agentID string, limit int, now time.Time) ([]domain.Command, error)
	AckCommand(ctx context.Context, agentID, id string, ok bool, result string, now time.Time) error
	ListCommands(ctx context.Context, agentID string, limit int) ([]domain.Command, error)
	ExpireCommands(ctx context.Context, olderThan time.Time) (int64, error)

	// Human-in-the-loop interrupts
	CreateInterrupt(ctx context.Context, i *domain.Interrupt) error
	GetInterrupt(ctx context.Context, id string) (*domain.Interrupt, error)
	ListInterrupts(ctx context.Context, status domain.InterruptStatus, limit int) ([]domain.Interrupt, error)
	ResolveInterrupt(ctx context.Context, id string, st domain.InterruptStatus, response json.RawMessage, by string, now time.Time) (*domain.Interrupt, error)

	// Audit
	AppendAudit(ctx context.Context, e *domain.AuditEntry) error
	ListAudit(ctx context.Context, limit int, beforeID int64) ([]domain.AuditEntry, error)

	// Dashboard
	Stats(ctx context.Context, now time.Time) (*domain.Stats, error)
}

// ClampLimit applies a default and an upper bound to page sizes.
func ClampLimit(n, def, max int) int {
	if n <= 0 {
		return def
	}
	if n > max {
		return max
	}
	return n
}
