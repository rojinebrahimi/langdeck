-- LangDeck initial schema.

CREATE TABLE users (
    id            UUID PRIMARY KEY,
    subject       TEXT NOT NULL UNIQUE,
    email         TEXT NOT NULL DEFAULT '',
    name          TEXT NOT NULL DEFAULT '',
    role          TEXT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_login_at TIMESTAMPTZ
);

CREATE TABLE sessions (
    id         TEXT PRIMARY KEY,           -- SHA-256 of the cookie value, never the raw value
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    csrf_token TEXT NOT NULL,
    user_agent TEXT NOT NULL DEFAULT '',
    ip         TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX sessions_expires_idx ON sessions (expires_at);

CREATE TABLE api_keys (
    id           UUID PRIMARY KEY,
    name         TEXT NOT NULL,
    prefix       TEXT NOT NULL,
    hash         TEXT NOT NULL UNIQUE,
    role         TEXT NOT NULL,
    agent_name   TEXT NOT NULL DEFAULT '',
    created_by   TEXT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at   TIMESTAMPTZ,
    last_used_at TIMESTAMPTZ,
    revoked_at   TIMESTAMPTZ
);

CREATE TABLE agents (
    id             UUID PRIMARY KEY,
    name           TEXT NOT NULL UNIQUE,
    description    TEXT NOT NULL DEFAULT '',
    version        TEXT NOT NULL DEFAULT '',
    framework      TEXT NOT NULL DEFAULT '',
    sdk_version    TEXT NOT NULL DEFAULT '',
    graph          JSONB,
    labels         JSONB NOT NULL DEFAULT '{}',
    desired_state  TEXT NOT NULL DEFAULT 'running',
    instances      INT NOT NULL DEFAULT 1,
    last_heartbeat TIMESTAMPTZ,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE agent_configs (
    agent_id   UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    version    INT NOT NULL,
    config     JSONB NOT NULL,
    comment    TEXT NOT NULL DEFAULT '',
    created_by TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (agent_id, version)
);

CREATE TABLE runs (
    id                TEXT PRIMARY KEY,    -- SDKs may supply their own run id
    agent_id          UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    thread_id         TEXT NOT NULL DEFAULT '',
    status            TEXT NOT NULL,
    trigger           TEXT NOT NULL DEFAULT '',
    input             JSONB,
    output            JSONB,
    error             TEXT NOT NULL DEFAULT '',
    prompt_tokens     BIGINT NOT NULL DEFAULT 0,
    completion_tokens BIGINT NOT NULL DEFAULT 0,
    created_by        TEXT NOT NULL DEFAULT '',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at        TIMESTAMPTZ,
    ended_at          TIMESTAMPTZ
);
CREATE INDEX runs_created_idx ON runs (created_at DESC);
CREATE INDEX runs_agent_created_idx ON runs (agent_id, created_at DESC);
CREATE INDEX runs_status_idx ON runs (status) WHERE status IN ('pending', 'running', 'interrupted');

CREATE TABLE run_events (
    id         BIGSERIAL PRIMARY KEY,
    run_id     TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    type       TEXT NOT NULL,
    node       TEXT NOT NULL DEFAULT '',
    payload    JSONB,
    latency_ms BIGINT NOT NULL DEFAULT 0,
    ts         TIMESTAMPTZ NOT NULL
);
CREATE INDEX run_events_run_idx ON run_events (run_id, id);

CREATE TABLE commands (
    id           UUID PRIMARY KEY,
    agent_id     UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    run_id       TEXT NOT NULL DEFAULT '',
    type         TEXT NOT NULL,
    payload      JSONB,
    status       TEXT NOT NULL,
    result       TEXT NOT NULL DEFAULT '',
    created_by   TEXT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    delivered_at TIMESTAMPTZ,
    acked_at     TIMESTAMPTZ
);
CREATE INDEX commands_claim_idx ON commands (agent_id, created_at) WHERE status IN ('pending', 'delivered');

CREATE TABLE interrupts (
    id          TEXT PRIMARY KEY,
    run_id      TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    agent_id    UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    node        TEXT NOT NULL DEFAULT '',
    question    TEXT NOT NULL DEFAULT '',
    payload     JSONB,
    status      TEXT NOT NULL,
    response    JSONB,
    resolved_by TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at TIMESTAMPTZ
);
CREATE INDEX interrupts_status_idx ON interrupts (status, created_at DESC);

CREATE TABLE audit_log (
    id          BIGSERIAL PRIMARY KEY,
    actor       TEXT NOT NULL,
    action      TEXT NOT NULL,
    target_type TEXT NOT NULL DEFAULT '',
    target_id   TEXT NOT NULL DEFAULT '',
    details     JSONB,
    ip          TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
