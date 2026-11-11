// Package postgres is the production Store, backed by PostgreSQL via pgx.
package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rojinebrahimi/langdeck/internal/domain"
	"github.com/rojinebrahimi/langdeck/internal/store"
	"github.com/rojinebrahimi/langdeck/internal/util"
)

// Store implements store.Store on a pgx connection pool.
type Store struct{ pool *pgxpool.Pool }

var _ store.Store = (*Store)(nil)

// Open connects, verifies the connection and runs migrations.
func Open(ctx context.Context, dsn string, maxConns int32) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	if maxConns > 0 {
		cfg.MaxConns = maxConns
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	if err := Migrate(ctx, pool); err != nil {
		pool.Close()
		return nil, err
	}
	return &Store{pool: pool}, nil
}

// Pool exposes the pool for components that need LISTEN/NOTIFY.
func (s *Store) Pool() *pgxpool.Pool { return s.pool }

func (s *Store) Ping(ctx context.Context) error { return s.pool.Ping(ctx) }
func (s *Store) Close()                         { s.pool.Close() }

// mapErr turns driver errors into domain errors.
func mapErr(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			return domain.ErrConflict
		case "23503", "22P02":
			return domain.ErrInvalid
		}
	}
	return err
}

// jb converts raw JSON to a value pgx sends as JSONB (nil stays SQL NULL).
func jb(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	return string(raw)
}

// jsonOrNil encodes v as JSON text, or returns SQL NULL when present is false.
func jsonOrNil(present bool, v any) any {
	if !present {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return string(b)
}

func raw(b []byte) json.RawMessage {
	if len(b) == 0 {
		return nil
	}
	return json.RawMessage(b)
}

func affected(tag pgconn.CommandTag, err error) error {
	if err != nil {
		return mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// ---- users ----

const userCols = `id, subject, email, name, role, created_at, last_login_at, password_hash, must_change_password`

func scanUser(row pgx.Row) (*domain.User, error) {
	var u domain.User
	if err := row.Scan(&u.ID, &u.Subject, &u.Email, &u.Name, &u.Role, &u.CreatedAt, &u.LastLoginAt,
		&u.PasswordHash, &u.MustChangePassword); err != nil {
		return nil, mapErr(err)
	}
	u.Local = strings.HasPrefix(u.Subject, "local|")
	return &u, nil
}

func (s *Store) CreateUser(ctx context.Context, u *domain.User) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO users (id, subject, email, name, role, created_at, password_hash, must_change_password)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, u.ID, u.Subject, u.Email, u.Name, u.Role, u.CreatedAt, u.PasswordHash, u.MustChangePassword)
	return mapErr(err)
}

func (s *Store) GetUserBySubject(ctx context.Context, subject string) (*domain.User, error) {
	return scanUser(s.pool.QueryRow(ctx, `SELECT `+userCols+` FROM users WHERE subject = $1`, subject))
}

func (s *Store) SetPassword(ctx context.Context, id, hash string, mustChange bool) error {
	return affected(s.pool.Exec(ctx, `UPDATE users SET password_hash = $2, must_change_password = $3 WHERE id = $1`, id, hash, mustChange))
}

func (s *Store) UpsertUserOnLogin(ctx context.Context, subject, email, name string, defaultRole domain.Role) (*domain.User, error) {
	return scanUser(s.pool.QueryRow(ctx, `
		INSERT INTO users (id, subject, email, name, role, last_login_at)
		VALUES ($1, $2, $3, $4, $5, now())
		ON CONFLICT (subject) DO UPDATE SET email = EXCLUDED.email, name = EXCLUDED.name, last_login_at = now()
		RETURNING `+userCols, util.NewID(), subject, email, name, defaultRole))
}

func (s *Store) GetUser(ctx context.Context, id string) (*domain.User, error) {
	return scanUser(s.pool.QueryRow(ctx, `SELECT `+userCols+` FROM users WHERE id = $1`, id))
}

func (s *Store) ListUsers(ctx context.Context) ([]domain.User, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+userCols+` FROM users ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	return collect(rows, scanUser)
}

func (s *Store) SetUserRole(ctx context.Context, id string, role domain.Role) error {
	return affected(s.pool.Exec(ctx, `UPDATE users SET role = $2 WHERE id = $1`, id, role))
}

// collect drains rows using scan, always closing them.
func collect[T any](rows pgx.Rows, scan func(pgx.Row) (*T, error)) ([]T, error) {
	defer rows.Close()
	out := []T{}
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}

// ---- sessions ----

func (s *Store) CreateSession(ctx context.Context, x *domain.Session) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO sessions (id, user_id, csrf_token, user_agent, ip, created_at, expires_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`, x.ID, x.UserID, x.CSRFToken, x.UserAgent, x.IP, x.CreatedAt, x.ExpiresAt)
	return mapErr(err)
}

func (s *Store) GetSession(ctx context.Context, id string) (*domain.Session, error) {
	var x domain.Session
	err := s.pool.QueryRow(ctx, `SELECT id, user_id, csrf_token, user_agent, ip, created_at, expires_at FROM sessions WHERE id = $1`, id).
		Scan(&x.ID, &x.UserID, &x.CSRFToken, &x.UserAgent, &x.IP, &x.CreatedAt, &x.ExpiresAt)
	if err != nil {
		return nil, mapErr(err)
	}
	return &x, nil
}

func (s *Store) DeleteSession(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE id = $1`, id)
	return err
}

func (s *Store) DeleteExpiredSessions(ctx context.Context, now time.Time) (int64, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE expires_at < $1`, now)
	return tag.RowsAffected(), err
}

// ---- api keys ----

const keyCols = `id, name, prefix, hash, role, agent_name, created_by, created_at, expires_at, last_used_at, revoked_at`

func scanKey(row pgx.Row) (*domain.APIKey, error) {
	var k domain.APIKey
	if err := row.Scan(&k.ID, &k.Name, &k.Prefix, &k.Hash, &k.Role, &k.AgentName, &k.CreatedBy, &k.CreatedAt,
		&k.ExpiresAt, &k.LastUsedAt, &k.RevokedAt); err != nil {
		return nil, mapErr(err)
	}
	return &k, nil
}

func (s *Store) CreateAPIKey(ctx context.Context, k *domain.APIKey) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO api_keys (`+keyCols+`) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		k.ID, k.Name, k.Prefix, k.Hash, k.Role, k.AgentName, k.CreatedBy, k.CreatedAt, k.ExpiresAt, k.LastUsedAt, k.RevokedAt)
	return mapErr(err)
}

func (s *Store) GetAPIKeyByHash(ctx context.Context, hash string) (*domain.APIKey, error) {
	return scanKey(s.pool.QueryRow(ctx, `SELECT `+keyCols+` FROM api_keys WHERE hash = $1`, hash))
}

func (s *Store) ListAPIKeys(ctx context.Context) ([]domain.APIKey, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+keyCols+` FROM api_keys ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	return collect(rows, scanKey)
}

func (s *Store) RevokeAPIKey(ctx context.Context, id string, at time.Time) error {
	return affected(s.pool.Exec(ctx, `UPDATE api_keys SET revoked_at = COALESCE(revoked_at, $2) WHERE id = $1`, id, at))
}

func (s *Store) TouchAPIKey(ctx context.Context, id string, at time.Time) error {
	_, err := s.pool.Exec(ctx, `UPDATE api_keys SET last_used_at = $2 WHERE id = $1`, id, at)
	return err
}

// ---- agents ----

const agentCols = `id, name, description, version, framework, sdk_version, graph, labels, accesses, depends_on, desired_state,
	instances, last_heartbeat, created_at, updated_at, image_updated_at`

func scanAgent(row pgx.Row) (*domain.Agent, error) {
	var a domain.Agent
	var graph, labels, accesses, dependsOn []byte
	if err := row.Scan(&a.ID, &a.Name, &a.Description, &a.Version, &a.Framework, &a.SDKVersion, &graph, &labels, &accesses, &dependsOn,
		&a.DesiredState, &a.Instances, &a.LastHeartbeat, &a.CreatedAt, &a.UpdatedAt, &a.ImageUpdatedAt); err != nil {
		return nil, mapErr(err)
	}
	a.Graph = raw(graph)
	a.Labels = map[string]string{}
	_ = json.Unmarshal(labels, &a.Labels)
	a.Accesses, a.DependsOn = []domain.Access{}, []string{}
	_ = json.Unmarshal(accesses, &a.Accesses)
	_ = json.Unmarshal(dependsOn, &a.DependsOn)
	return &a, nil
}

func (s *Store) RegisterAgent(ctx context.Context, reg store.AgentRegistration, now time.Time) (*domain.Agent, error) {
	labels, _ := json.Marshal(reg.Labels)
	if reg.Labels == nil {
		labels = []byte("{}")
	}
	return scanAgent(s.pool.QueryRow(ctx, `
		INSERT INTO agents (id, name, description, version, framework, sdk_version, graph, labels, accesses, depends_on, last_heartbeat, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$11,$11)
		ON CONFLICT (name) DO UPDATE SET
			description = EXCLUDED.description, version = EXCLUDED.version, framework = EXCLUDED.framework,
			sdk_version = EXCLUDED.sdk_version, graph = COALESCE(EXCLUDED.graph, agents.graph),
			accesses = COALESCE(EXCLUDED.accesses, agents.accesses), depends_on = COALESCE(EXCLUDED.depends_on, agents.depends_on),
			labels = EXCLUDED.labels, last_heartbeat = EXCLUDED.last_heartbeat, updated_at = EXCLUDED.updated_at
		RETURNING `+agentCols,
		util.NewID(), reg.Name, reg.Description, reg.Version, reg.Framework, reg.SDKVersion, jb(reg.Graph), string(labels),
		jsonOrNil(reg.Accesses != nil, reg.Accesses), jsonOrNil(reg.DependsOn != nil, reg.DependsOn), now))
}

func (s *Store) Heartbeat(ctx context.Context, id string, instances int, now time.Time) error {
	return affected(s.pool.Exec(ctx, `UPDATE agents SET last_heartbeat = $2,
		instances = CASE WHEN $3 > 0 THEN $3 ELSE instances END WHERE id = $1`, id, now, instances))
}

func (s *Store) GetAgent(ctx context.Context, id string) (*domain.Agent, error) {
	return scanAgent(s.pool.QueryRow(ctx, `SELECT `+agentCols+` FROM agents WHERE id = $1`, id))
}

func (s *Store) GetAgentByName(ctx context.Context, name string) (*domain.Agent, error) {
	return scanAgent(s.pool.QueryRow(ctx, `SELECT `+agentCols+` FROM agents WHERE name = $1`, name))
}

func (s *Store) ListAgents(ctx context.Context) ([]domain.Agent, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+agentCols+` FROM agents ORDER BY name`)
	if err != nil {
		return nil, err
	}
	return collect(rows, scanAgent)
}

func (s *Store) ListAgentsPage(ctx context.Context, f store.AgentFilter) (*store.AgentPage, error) {
	where, args := []string{"TRUE"}, []any{}
	arg := func(v any) string { args = append(args, v); return fmt.Sprintf("$%d", len(args)) }
	if q := strings.TrimSpace(f.Query); q != "" {
		p := arg("%" + likeEscape(q) + "%")
		where = append(where, "(name ILIKE "+p+" OR description ILIKE "+p+")")
	}
	if f.Status != "" {
		// Mirrors domain.Agent.ComputeStatus.
		fresh := "(last_heartbeat IS NOT NULL AND last_heartbeat >= " + arg(f.Now.Add(-domain.HeartbeatTTL)) + ")"
		switch f.Status {
		case domain.AgentOffline:
			where = append(where, "NOT "+fresh)
		case domain.AgentPaused:
			where = append(where, fresh+" AND desired_state = 'paused'")
		case domain.AgentOnline:
			where = append(where, fresh+" AND desired_state <> 'paused'")
		default:
			return nil, fmt.Errorf("%w: unknown agent status %q", domain.ErrInvalid, f.Status)
		}
	}
	cond := strings.Join(where, " AND ")
	page := &store.AgentPage{}
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM agents WHERE `+cond, args...).Scan(&page.Total); err != nil {
		return nil, err
	}
	pageCond := cond
	if f.After != "" {
		pageCond += " AND name > " + arg(f.After)
	}
	limit := arg(store.ClampLimit(f.Limit, 24, store.MaxAgentPage))
	rows, err := s.pool.Query(ctx, `SELECT `+agentCols+` FROM agents WHERE `+pageCond+` ORDER BY name LIMIT `+limit, args...)
	if err != nil {
		return nil, err
	}
	list, err := collect(rows, scanAgent)
	if err != nil {
		return nil, err
	}
	page.Agents = list
	return page, nil
}

// likeEscape makes user input literal inside a LIKE pattern.
func likeEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(s)
}

func (s *Store) SetAgentDesiredState(ctx context.Context, id string, st domain.DesiredState) error {
	return affected(s.pool.Exec(ctx, `UPDATE agents SET desired_state = $2, updated_at = now() WHERE id = $1`, id, st))
}

func (s *Store) SetAgentImage(ctx context.Context, id, contentType string, data []byte, now time.Time) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := affected(tx.Exec(ctx, `UPDATE agents SET image_updated_at = $2 WHERE id = $1`, id, now)); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO agent_images (agent_id, content_type, data) VALUES ($1, $2, $3)
		ON CONFLICT (agent_id) DO UPDATE SET content_type = EXCLUDED.content_type, data = EXCLUDED.data`, id, contentType, data); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) AgentImage(ctx context.Context, id string) (string, []byte, error) {
	var ct string
	var data []byte
	err := s.pool.QueryRow(ctx, `SELECT content_type, data FROM agent_images WHERE agent_id = $1`, id).Scan(&ct, &data)
	if err != nil {
		return "", nil, mapErr(err)
	}
	return ct, data, nil
}

func (s *Store) DeleteAgentImage(ctx context.Context, id string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := affected(tx.Exec(ctx, `UPDATE agents SET image_updated_at = NULL WHERE id = $1`, id)); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM agent_images WHERE agent_id = $1`, id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) GetSetting(ctx context.Context, key string) (json.RawMessage, error) {
	var v []byte
	if err := s.pool.QueryRow(ctx, `SELECT value FROM settings WHERE key = $1`, key).Scan(&v); err != nil {
		return nil, mapErr(err)
	}
	return v, nil
}

func (s *Store) SetSetting(ctx context.Context, key string, value json.RawMessage, now time.Time) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO settings (key, value, updated_at) VALUES ($1, $2, $3)
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = EXCLUDED.updated_at`, key, []byte(value), now)
	return err
}

func (s *Store) DeleteAgent(ctx context.Context, id string) error {
	return affected(s.pool.Exec(ctx, `DELETE FROM agents WHERE id = $1`, id))
}

// ---- configs ----

func (s *Store) CreateAgentConfig(ctx context.Context, c *domain.AgentConfig) error {
	// The version is computed inside the INSERT; the primary key rejects a concurrent duplicate.
	err := s.pool.QueryRow(ctx, `
		INSERT INTO agent_configs (agent_id, version, config, comment, created_by, created_at)
		SELECT $1, COALESCE(MAX(version), 0) + 1, $2, $3, $4, $5 FROM agent_configs WHERE agent_id = $1
		RETURNING version`, c.AgentID, jb(c.Config), c.Comment, c.CreatedBy, c.CreatedAt).Scan(&c.Version)
	return mapErr(err)
}

func scanConfig(row pgx.Row) (*domain.AgentConfig, error) {
	var c domain.AgentConfig
	var cfg []byte
	if err := row.Scan(&c.AgentID, &c.Version, &cfg, &c.Comment, &c.CreatedBy, &c.CreatedAt); err != nil {
		return nil, mapErr(err)
	}
	c.Config = raw(cfg)
	return &c, nil
}

const configCols = `agent_id, version, config, comment, created_by, created_at`

func (s *Store) ListAgentConfigs(ctx context.Context, agentID string) ([]domain.AgentConfig, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+configCols+` FROM agent_configs WHERE agent_id = $1 ORDER BY version DESC LIMIT 100`, agentID)
	if err != nil {
		return nil, err
	}
	return collect(rows, scanConfig)
}

func (s *Store) LatestAgentConfig(ctx context.Context, agentID string) (*domain.AgentConfig, error) {
	return scanConfig(s.pool.QueryRow(ctx, `SELECT `+configCols+` FROM agent_configs WHERE agent_id = $1 ORDER BY version DESC LIMIT 1`, agentID))
}

// ---- runs ----

// runListCols is runCols without the (potentially large) input and output documents; list
// screens never show them, so they are not read from disk or sent over the wire.
const runListCols = `r.id, r.agent_id, a.name, r.thread_id, r.status, r.trigger, NULL::jsonb, NULL::jsonb, r.error,
	r.prompt_tokens, r.completion_tokens, r.created_by, r.created_at, r.started_at, r.ended_at`

const runCols = `r.id, r.agent_id, a.name, r.thread_id, r.status, r.trigger, r.input, r.output, r.error,
	r.prompt_tokens, r.completion_tokens, r.created_by, r.created_at, r.started_at, r.ended_at`

func scanRun(row pgx.Row) (*domain.Run, error) {
	var r domain.Run
	var in, out []byte
	if err := row.Scan(&r.ID, &r.AgentID, &r.AgentName, &r.ThreadID, &r.Status, &r.Trigger, &in, &out, &r.Error,
		&r.PromptTokens, &r.CompletionTokens, &r.CreatedBy, &r.CreatedAt, &r.StartedAt, &r.EndedAt); err != nil {
		return nil, mapErr(err)
	}
	r.Input, r.Output = raw(in), raw(out)
	return &r, nil
}

func (s *Store) CreateRun(ctx context.Context, r *domain.Run) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO runs (id, agent_id, thread_id, status, trigger, input, created_by, created_at, started_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`, r.ID, r.AgentID, r.ThreadID, r.Status, r.Trigger, jb(r.Input), r.CreatedBy, r.CreatedAt, r.StartedAt)
	return mapErr(err)
}

func (s *Store) EnsureRun(ctx context.Context, r *domain.Run) (*domain.Run, error) {
	_, err := s.pool.Exec(ctx, `INSERT INTO runs (id, agent_id, thread_id, status, trigger, input, created_by, created_at, started_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT (id) DO NOTHING`,
		r.ID, r.AgentID, r.ThreadID, r.Status, r.Trigger, jb(r.Input), r.CreatedBy, r.CreatedAt, r.StartedAt)
	if err != nil {
		return nil, mapErr(err)
	}
	got, err := s.GetRun(ctx, r.ID)
	if err != nil {
		return nil, err
	}
	if got.AgentID != r.AgentID {
		return nil, domain.ErrConflict // a run id owned by another agent
	}
	return got, nil
}

func (s *Store) GetRun(ctx context.Context, id string) (*domain.Run, error) {
	return scanRun(s.pool.QueryRow(ctx, `SELECT `+runCols+` FROM runs r JOIN agents a ON a.id = r.agent_id WHERE r.id = $1`, id))
}

func (s *Store) ListRuns(ctx context.Context, f store.RunFilter) ([]domain.Run, error) {
	q := `SELECT ` + runListCols + ` FROM runs r JOIN agents a ON a.id = r.agent_id WHERE true`
	args := []any{}
	add := func(cond string, v any) {
		args = append(args, v)
		q += fmt.Sprintf(" AND "+cond, len(args))
	}
	if f.AgentID != "" {
		add("r.agent_id = $%d", f.AgentID)
	}
	if f.Status != "" {
		add("r.status = $%d", f.Status)
	}
	if f.ThreadID != "" {
		add("r.thread_id = $%d", f.ThreadID)
	}
	if !f.Before.IsZero() {
		add("r.created_at < $%d", f.Before)
	}
	args = append(args, store.ClampLimit(f.Limit, 50, 500))
	q += fmt.Sprintf(" ORDER BY r.created_at DESC LIMIT $%d", len(args))
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	return collect(rows, scanRun)
}

func (s *Store) UpdateRun(ctx context.Context, r *domain.Run) error {
	return affected(s.pool.Exec(ctx, `UPDATE runs SET thread_id=$2, status=$3, output=$4, error=$5, prompt_tokens=$6,
		completion_tokens=$7, started_at=$8, ended_at=$9 WHERE id=$1`,
		r.ID, r.ThreadID, r.Status, jb(r.Output), r.Error, r.PromptTokens, r.CompletionTokens, r.StartedAt, r.EndedAt))
}

func (s *Store) AppendEvents(ctx context.Context, evs []domain.RunEvent) error {
	if len(evs) == 0 {
		return nil
	}
	batch := &pgx.Batch{}
	for _, e := range evs {
		batch.Queue(`INSERT INTO run_events (run_id, type, node, payload, latency_ms, ts) VALUES ($1,$2,$3,$4,$5,$6) RETURNING id`,
			e.RunID, e.Type, e.Node, jb(e.Payload), e.LatencyMS, e.Timestamp)
	}
	br := s.pool.SendBatch(ctx, batch)
	for i := range evs {
		if err := br.QueryRow().Scan(&evs[i].ID); err != nil {
			_ = br.Close()
			return mapErr(err)
		}
	}
	return br.Close()
}

func (s *Store) ListEvents(ctx context.Context, runID string, afterID int64, limit int) ([]domain.RunEvent, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, run_id, type, node, payload, latency_ms, ts FROM run_events
		WHERE run_id = $1 AND id > $2 ORDER BY id LIMIT $3`, runID, afterID, store.ClampLimit(limit, 1000, 5000))
	if err != nil {
		return nil, err
	}
	return collect(rows, func(row pgx.Row) (*domain.RunEvent, error) {
		var e domain.RunEvent
		var p []byte
		if err := row.Scan(&e.ID, &e.RunID, &e.Type, &e.Node, &p, &e.LatencyMS, &e.Timestamp); err != nil {
			return nil, err
		}
		e.Payload = raw(p)
		return &e, nil
	})
}

func (s *Store) PurgeRunsBefore(ctx context.Context, t time.Time) (int64, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM runs WHERE created_at < $1 AND status IN ('succeeded','failed','cancelled')`, t)
	return tag.RowsAffected(), err
}

// ---- commands ----

const cmdCols = `id, agent_id, run_id, type, payload, status, result, created_by, created_at, delivered_at, acked_at`

func scanCmd(row pgx.Row) (*domain.Command, error) {
	var c domain.Command
	var p []byte
	if err := row.Scan(&c.ID, &c.AgentID, &c.RunID, &c.Type, &p, &c.Status, &c.Result, &c.CreatedBy, &c.CreatedAt,
		&c.DeliveredAt, &c.AckedAt); err != nil {
		return nil, mapErr(err)
	}
	c.Payload = raw(p)
	return &c, nil
}

func (s *Store) CreateCommand(ctx context.Context, c *domain.Command) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO commands (id, agent_id, run_id, type, payload, status, created_by, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, c.ID, c.AgentID, c.RunID, c.Type, jb(c.Payload), c.Status, c.CreatedBy, c.CreatedAt)
	return mapErr(err)
}

// ClaimCommands atomically marks commands delivered. SKIP LOCKED lets several
// replicas serve long-polls for the same agent without handing out duplicates.
func (s *Store) ClaimCommands(ctx context.Context, agentID string, limit int, now time.Time) ([]domain.Command, error) {
	rows, err := s.pool.Query(ctx, `
		UPDATE commands SET status = 'delivered', delivered_at = $2
		WHERE id IN (
			SELECT id FROM commands
			WHERE agent_id = $1 AND (status = 'pending' OR (status = 'delivered' AND delivered_at < $3))
			ORDER BY created_at LIMIT $4 FOR UPDATE SKIP LOCKED)
		RETURNING `+cmdCols, agentID, now, now.Add(-store.RedeliverAfter), limit)
	if err != nil {
		return nil, err
	}
	out, err := collect(rows, scanCmd)
	if err != nil {
		return nil, err
	}
	// RETURNING order is not guaranteed; agents expect creation order.
	sortCommands(out)
	return out, nil
}

func (s *Store) AckCommand(ctx context.Context, agentID, id string, ok bool, result string, now time.Time) error {
	st := domain.CmdAcked
	if !ok {
		st = domain.CmdFailed
	}
	return affected(s.pool.Exec(ctx, `UPDATE commands SET status=$3, result=$4, acked_at=$5 WHERE id=$1 AND agent_id=$2`,
		id, agentID, st, result, now))
}

func (s *Store) ListCommands(ctx context.Context, agentID string, limit int) ([]domain.Command, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+cmdCols+` FROM commands WHERE ($1 = '' OR agent_id::text = $1)
		ORDER BY created_at DESC LIMIT $2`, agentID, store.ClampLimit(limit, 50, 500))
	if err != nil {
		return nil, err
	}
	return collect(rows, scanCmd)
}

func (s *Store) ExpireCommands(ctx context.Context, olderThan time.Time) (int64, error) {
	tag, err := s.pool.Exec(ctx, `UPDATE commands SET status='expired' WHERE status IN ('pending','delivered') AND created_at < $1`, olderThan)
	return tag.RowsAffected(), err
}

// ---- interrupts ----

const intCols = `i.id, i.run_id, i.agent_id, a.name, i.node, i.question, i.payload, i.status, i.response,
	i.resolved_by, i.created_at, i.resolved_at`

func scanInt(row pgx.Row) (*domain.Interrupt, error) {
	var i domain.Interrupt
	var p, r []byte
	if err := row.Scan(&i.ID, &i.RunID, &i.AgentID, &i.AgentName, &i.Node, &i.Question, &p, &i.Status, &r,
		&i.ResolvedBy, &i.CreatedAt, &i.ResolvedAt); err != nil {
		return nil, mapErr(err)
	}
	i.Payload, i.Response = raw(p), raw(r)
	return &i, nil
}

func (s *Store) CreateInterrupt(ctx context.Context, i *domain.Interrupt) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO interrupts (id, run_id, agent_id, node, question, payload, status, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, i.ID, i.RunID, i.AgentID, i.Node, i.Question, jb(i.Payload), i.Status, i.CreatedAt)
	return mapErr(err)
}

func (s *Store) GetInterrupt(ctx context.Context, id string) (*domain.Interrupt, error) {
	return scanInt(s.pool.QueryRow(ctx, `SELECT `+intCols+` FROM interrupts i JOIN agents a ON a.id = i.agent_id WHERE i.id = $1`, id))
}

func (s *Store) ListInterrupts(ctx context.Context, st domain.InterruptStatus, limit int) ([]domain.Interrupt, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+intCols+` FROM interrupts i JOIN agents a ON a.id = i.agent_id
		WHERE ($1 = '' OR i.status = $1) ORDER BY i.created_at DESC LIMIT $2`, string(st), store.ClampLimit(limit, 50, 500))
	if err != nil {
		return nil, err
	}
	return collect(rows, scanInt)
}

func (s *Store) ResolveInterrupt(ctx context.Context, id string, st domain.InterruptStatus, resp json.RawMessage, by string, now time.Time) (*domain.Interrupt, error) {
	tag, err := s.pool.Exec(ctx, `UPDATE interrupts SET status=$2, response=$3, resolved_by=$4, resolved_at=$5
		WHERE id=$1 AND status='pending'`, id, st, jb(resp), by, now)
	if err != nil {
		return nil, mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		if _, err := s.GetInterrupt(ctx, id); err != nil {
			return nil, err
		}
		return nil, domain.ErrConflict // exists but already resolved
	}
	return s.GetInterrupt(ctx, id)
}

// ---- audit ----

func (s *Store) AppendAudit(ctx context.Context, e *domain.AuditEntry) error {
	return s.pool.QueryRow(ctx, `INSERT INTO audit_log (actor, action, target_type, target_id, details, ip, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id`, e.Actor, e.Action, e.TargetType, e.TargetID, jb(e.Details), e.IP, e.CreatedAt).Scan(&e.ID)
}

func (s *Store) ListAudit(ctx context.Context, limit int, beforeID int64) ([]domain.AuditEntry, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, actor, action, target_type, target_id, details, ip, created_at FROM audit_log
		WHERE ($1 = 0 OR id < $1) ORDER BY id DESC LIMIT $2`, beforeID, store.ClampLimit(limit, 100, 1000))
	if err != nil {
		return nil, err
	}
	return collect(rows, func(row pgx.Row) (*domain.AuditEntry, error) {
		var e domain.AuditEntry
		var d []byte
		if err := row.Scan(&e.ID, &e.Actor, &e.Action, &e.TargetType, &e.TargetID, &d, &e.IP, &e.CreatedAt); err != nil {
			return nil, err
		}
		e.Details = raw(d)
		return &e, nil
	})
}

// ---- stats ----

// statsRowCap bounds the rows Stats reads, so a burst of runs cannot make the dashboard slow.
const statsRowCap = 100_000

func (s *Store) Stats(ctx context.Context, now time.Time) (*domain.Stats, error) {
	// Only status inputs are needed, not the graph and other documents.
	arows, err := s.pool.Query(ctx, `SELECT desired_state, last_heartbeat FROM agents`)
	if err != nil {
		return nil, err
	}
	agents, err := collect(arows, func(row pgx.Row) (*domain.Agent, error) {
		var a domain.Agent
		err := row.Scan(&a.DesiredState, &a.LastHeartbeat)
		return &a, err
	})
	if err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT a.name, r.status, r.prompt_tokens, r.completion_tokens, r.created_at, r.started_at, r.ended_at
		FROM runs r JOIN agents a ON a.id = r.agent_id WHERE r.created_at > $1 ORDER BY r.created_at DESC LIMIT $2`,
		now.Add(-24*time.Hour), statsRowCap)
	if err != nil {
		return nil, err
	}
	runs, err := collect(rows, func(row pgx.Row) (*domain.Run, error) {
		var r domain.Run
		err := row.Scan(&r.AgentName, &r.Status, &r.PromptTokens, &r.CompletionTokens, &r.CreatedAt, &r.StartedAt, &r.EndedAt)
		return &r, err
	})
	if err != nil {
		return nil, err
	}
	var pending int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM interrupts WHERE status = 'pending'`).Scan(&pending); err != nil {
		return nil, err
	}
	return store.ComputeStats(agents, runs, pending, now), nil
}

func sortCommands(cs []domain.Command) {
	slices.SortStableFunc(cs, func(a, b domain.Command) int { return a.CreatedAt.Compare(b.CreatedAt) })
}
