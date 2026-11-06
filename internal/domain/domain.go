// Package domain holds the core types shared by every LangDeck component.
// It has no dependencies on storage, transport or auth.
package domain

import (
	"encoding/json"
	"errors"
	"regexp"
	"time"
)

// Sentinel errors returned by stores and services. Handlers map them to HTTP codes.
var (
	ErrNotFound     = errors.New("not found")
	ErrConflict     = errors.New("conflict")
	ErrInvalid      = errors.New("invalid input")
	ErrUnauthorized = errors.New("unauthorized")
	ErrForbidden    = errors.New("forbidden")
)

// Role is a coarse RBAC role. Roles are ordered: each one includes the
// permissions of the roles before it (viewer < operator < admin).
// RoleAgent is separate: it is only allowed to use the agent-facing API.
type Role string

const (
	RoleViewer   Role = "viewer"
	RoleOperator Role = "operator"
	RoleAdmin    Role = "admin"
	RoleAgent    Role = "agent"
)

var roleRank = map[Role]int{RoleViewer: 1, RoleOperator: 2, RoleAdmin: 3}

// Valid reports whether r is a known role.
func (r Role) Valid() bool {
	_, ok := roleRank[r]
	return ok || r == RoleAgent
}

// AtLeast reports whether r grants at least the permissions of min.
// The agent role never satisfies a human role.
func (r Role) AtLeast(min Role) bool {
	if r == RoleAgent || min == RoleAgent {
		return r == min
	}
	return roleRank[r] >= roleRank[min]
}

// User is a human operator, created on first OIDC login.
type User struct {
	ID          string     `json:"id"`
	Subject     string     `json:"subject"`
	Email       string     `json:"email"`
	Name        string     `json:"name"`
	Role        Role       `json:"role"`
	CreatedAt   time.Time  `json:"created_at"`
	LastLoginAt *time.Time `json:"last_login_at,omitempty"`
	// Local (username/password) accounts only.
	PasswordHash       string `json:"-"`
	MustChangePassword bool   `json:"must_change_password"`
	Local              bool   `json:"local"`
}

// Session is a server-side browser session. Only the ID travels in a cookie.
type Session struct {
	ID        string    `json:"-"`
	UserID    string    `json:"user_id"`
	CSRFToken string    `json:"-"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
	UserAgent string    `json:"user_agent"`
	IP        string    `json:"ip"`
}

// APIKey authenticates agents and automation. Only a hash of the secret is stored.
type APIKey struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	Hash       string     `json:"-"`
	Role       Role       `json:"role"`
	AgentName  string     `json:"agent_name,omitempty"` // optional: restrict an agent key to one agent
	CreatedBy  string     `json:"created_by"`
	CreatedAt  time.Time  `json:"created_at"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
}

// Active reports whether the key can still be used at time now.
func (k *APIKey) Active(now time.Time) bool {
	if k.RevokedAt != nil {
		return false
	}
	return k.ExpiresAt == nil || now.Before(*k.ExpiresAt)
}

// DesiredState is what operators want an agent to do.
type DesiredState string

const (
	DesiredRunning DesiredState = "running"
	DesiredPaused  DesiredState = "paused"
)

// AgentStatus is derived from heartbeats and the desired state.
type AgentStatus string

const (
	AgentOnline  AgentStatus = "online"
	AgentOffline AgentStatus = "offline"
	AgentPaused  AgentStatus = "paused"
)

// Agent is a registered LangGraph application.
type Agent struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Description   string            `json:"description"`
	Version       string            `json:"version"`
	Framework     string            `json:"framework"`
	Graph         json.RawMessage   `json:"graph,omitempty"`
	Labels        map[string]string `json:"labels"`
	DesiredState  DesiredState      `json:"desired_state"`
	Status        AgentStatus       `json:"status"`
	Instances     int               `json:"instances"`
	SDKVersion    string            `json:"sdk_version"`
	LastHeartbeat *time.Time        `json:"last_heartbeat,omitempty"`
	CreatedAt     time.Time         `json:"created_at"`
	UpdatedAt     time.Time         `json:"updated_at"`
}

// HeartbeatTTL is how long an agent stays "online" after its last heartbeat.
const HeartbeatTTL = 45 * time.Second

// ComputeStatus fills Status from the desired state and the heartbeat age.
func (a *Agent) ComputeStatus(now time.Time) {
	switch {
	case a.LastHeartbeat == nil || now.Sub(*a.LastHeartbeat) > HeartbeatTTL:
		a.Status = AgentOffline
	case a.DesiredState == DesiredPaused:
		a.Status = AgentPaused
	default:
		a.Status = AgentOnline
	}
}

var agentNameRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-_.]{0,61}[a-z0-9])?$`)

// ValidAgentName reports whether name is a safe, DNS-like identifier.
func ValidAgentName(name string) bool { return agentNameRe.MatchString(name) }

// AgentConfig is one version of an agent's runtime configuration.
type AgentConfig struct {
	AgentID   string          `json:"agent_id"`
	Version   int             `json:"version"`
	Config    json.RawMessage `json:"config"`
	Comment   string          `json:"comment"`
	CreatedBy string          `json:"created_by"`
	CreatedAt time.Time       `json:"created_at"`
}

// RunStatus is the lifecycle state of a graph run.
type RunStatus string

const (
	RunPending     RunStatus = "pending"
	RunRunning     RunStatus = "running"
	RunInterrupted RunStatus = "interrupted"
	RunSucceeded   RunStatus = "succeeded"
	RunFailed      RunStatus = "failed"
	RunCancelled   RunStatus = "cancelled"
)

// Terminal reports whether the run can no longer change state.
func (s RunStatus) Terminal() bool {
	return s == RunSucceeded || s == RunFailed || s == RunCancelled
}

// Run is a single execution of an agent graph.
type Run struct {
	ID               string          `json:"id"`
	AgentID          string          `json:"agent_id"`
	AgentName        string          `json:"agent_name"`
	ThreadID         string          `json:"thread_id,omitempty"`
	Status           RunStatus       `json:"status"`
	Trigger          string          `json:"trigger"`
	Input            json.RawMessage `json:"input,omitempty"`
	Output           json.RawMessage `json:"output,omitempty"`
	Error            string          `json:"error,omitempty"`
	PromptTokens     int64           `json:"prompt_tokens"`
	CompletionTokens int64           `json:"completion_tokens"`
	CreatedBy        string          `json:"created_by,omitempty"`
	CreatedAt        time.Time       `json:"created_at"`
	StartedAt        *time.Time      `json:"started_at,omitempty"`
	EndedAt          *time.Time      `json:"ended_at,omitempty"`
}

// EventType classifies a run event emitted by the SDK.
type EventType string

const (
	EvRunStart  EventType = "run_start"
	EvRunEnd    EventType = "run_end"
	EvRunError  EventType = "run_error"
	EvNodeStart EventType = "node_start"
	EvNodeEnd   EventType = "node_end"
	EvNodeError EventType = "node_error"
	EvLLMStart  EventType = "llm_start"
	EvLLMEnd    EventType = "llm_end"
	EvToolStart EventType = "tool_start"
	EvToolEnd   EventType = "tool_end"
	EvInterrupt EventType = "interrupt"
	EvLog       EventType = "log"
)

var knownEvents = map[EventType]bool{
	EvRunStart: true, EvRunEnd: true, EvRunError: true, EvNodeStart: true, EvNodeEnd: true,
	EvNodeError: true, EvLLMStart: true, EvLLMEnd: true, EvToolStart: true, EvToolEnd: true,
	EvInterrupt: true, EvLog: true,
}

// Valid reports whether t is a known event type.
func (t EventType) Valid() bool { return knownEvents[t] }

// RunEvent is one step in a run's trace.
type RunEvent struct {
	ID        int64           `json:"id"`
	RunID     string          `json:"run_id"`
	Type      EventType       `json:"type"`
	Node      string          `json:"node,omitempty"`
	Payload   json.RawMessage `json:"payload,omitempty"`
	LatencyMS int64           `json:"latency_ms,omitempty"`
	Timestamp time.Time       `json:"ts"`
}

// CommandType is an instruction sent from the control plane to an agent.
type CommandType string

const (
	CmdStartRun     CommandType = "start_run"
	CmdCancelRun    CommandType = "cancel_run"
	CmdPause        CommandType = "pause"
	CmdResume       CommandType = "resume"
	CmdUpdateConfig CommandType = "update_config"
)

// CommandStatus tracks delivery of a command.
type CommandStatus string

const (
	CmdPending   CommandStatus = "pending"
	CmdDelivered CommandStatus = "delivered"
	CmdAcked     CommandStatus = "acked"
	CmdFailed    CommandStatus = "failed"
	CmdExpired   CommandStatus = "expired"
)

// Command is queued for an agent and delivered over the pull-based channel.
type Command struct {
	ID          string          `json:"id"`
	AgentID     string          `json:"agent_id"`
	RunID       string          `json:"run_id,omitempty"`
	Type        CommandType     `json:"type"`
	Payload     json.RawMessage `json:"payload,omitempty"`
	Status      CommandStatus   `json:"status"`
	Result      string          `json:"result,omitempty"`
	CreatedBy   string          `json:"created_by"`
	CreatedAt   time.Time       `json:"created_at"`
	DeliveredAt *time.Time      `json:"delivered_at,omitempty"`
	AckedAt     *time.Time      `json:"acked_at,omitempty"`
}

// InterruptStatus is the state of a human-in-the-loop request.
type InterruptStatus string

const (
	InterruptPending  InterruptStatus = "pending"
	InterruptApproved InterruptStatus = "approved"
	InterruptRejected InterruptStatus = "rejected"
)

// Interrupt is a human-in-the-loop approval requested by a running graph.
type Interrupt struct {
	ID         string          `json:"id"`
	RunID      string          `json:"run_id"`
	AgentID    string          `json:"agent_id"`
	AgentName  string          `json:"agent_name"`
	Node       string          `json:"node"`
	Question   string          `json:"question"`
	Payload    json.RawMessage `json:"payload,omitempty"`
	Status     InterruptStatus `json:"status"`
	Response   json.RawMessage `json:"response,omitempty"`
	ResolvedBy string          `json:"resolved_by,omitempty"`
	CreatedAt  time.Time       `json:"created_at"`
	ResolvedAt *time.Time      `json:"resolved_at,omitempty"`
}

// AuditEntry records who changed what. Entries are append-only.
type AuditEntry struct {
	ID         int64           `json:"id"`
	Actor      string          `json:"actor"`
	Action     string          `json:"action"`
	TargetType string          `json:"target_type"`
	TargetID   string          `json:"target_id"`
	Details    json.RawMessage `json:"details,omitempty"`
	IP         string          `json:"ip"`
	CreatedAt  time.Time       `json:"created_at"`
}

// Stats is the dashboard summary.
type Stats struct {
	AgentsTotal       int               `json:"agents_total"`
	AgentsOnline      int               `json:"agents_online"`
	RunsLast24h       int               `json:"runs_last_24h"`
	RunsByStatus      map[RunStatus]int `json:"runs_by_status"`
	FailureRate       float64           `json:"failure_rate"`
	PendingInterrupts int               `json:"pending_interrupts"`
	TokensLast24h     int64             `json:"tokens_last_24h"`
	P50LatencyMS      int64             `json:"p50_latency_ms"`
	P95LatencyMS      int64             `json:"p95_latency_ms"`
	Hourly            []HourlyRunBucket `json:"hourly"`
	TopAgents         []AgentRunCounter `json:"top_agents"`
}

// HourlyRunBucket is one bar of the runs-per-hour chart.
type HourlyRunBucket struct {
	Hour      time.Time `json:"hour"`
	Succeeded int       `json:"succeeded"`
	Failed    int       `json:"failed"`
	Other     int       `json:"other"`
}

// AgentRunCounter counts runs per agent.
type AgentRunCounter struct {
	AgentName string `json:"agent_name"`
	Runs      int    `json:"runs"`
	Failed    int    `json:"failed"`
}
