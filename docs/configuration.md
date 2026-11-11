# Configuration

LangDeck is configured with environment variables. Invalid values stop the server at
startup with a message listing every problem.

| Variable | Default | Description |
|---|---|---|
| `LANGDECK_ENV` | `development` | `production` enables strict validation (OIDC, HTTPS, database required). |
| `LANGDECK_ADDR` | `:8080` | HTTP listen address (UI, API). |
| `LANGDECK_METRICS_ADDR` | `:9090` | Prometheus `/metrics` listen address. Keep it private. |
| `LANGDECK_PUBLIC_URL` | `http://localhost:8080` | External URL. Drives OIDC redirect and `Secure` cookies. |
| `LANGDECK_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error`. |
| `LANGDECK_LOG_FORMAT` | `json` | `json` or `text`. |
| `LANGDECK_DATABASE_URL` | *(empty)* | PostgreSQL URL. Empty uses the in-memory store (development only). |
| `LANGDECK_DB_MAX_CONNS` | `20` | Connection pool size per replica. |
| `LANGDECK_AUTH_MODE` | `local` | `local` (username/password), `oidc` (SSO) or `dev` (any email signs in as admin; refused in production). |
| `LANGDECK_DEMO_MODE` | `false` | For public demos, with `dev` auth: one-click anonymous visitors with the operator role; no emails or IP addresses are stored. Refused in production. |
| `LANGDECK_ADMIN_USERNAME` | `admin` | Initial admin account created in `local` mode if it does not exist. |
| `LANGDECK_ADMIN_PASSWORD` | `admin` | Its initial password. Only used when the account is created. |
| `LANGDECK_ADMIN_FORCE_PASSWORD_CHANGE` | `true` | Require choosing a new password at first sign-in. Production refuses `false` with the default password. |
| `LANGDECK_OIDC_ISSUER` | | Issuer URL of your OpenID Connect provider. |
| `LANGDECK_OIDC_CLIENT_ID` | | OAuth client id. |
| `LANGDECK_OIDC_CLIENT_SECRET` | | OAuth client secret. |
| `LANGDECK_OIDC_SCOPES` | `openid,profile,email` | Requested scopes. |
| `LANGDECK_OIDC_GROUPS_CLAIM` | `groups` | ID-token claim holding group names. |
| `LANGDECK_OIDC_ADMIN_GROUPS` | | Comma-separated groups mapped to `admin`. |
| `LANGDECK_OIDC_OPERATOR_GROUPS` | | Groups mapped to `operator`. |
| `LANGDECK_OIDC_VIEWER_GROUPS` | | Groups mapped to `viewer`. |
| `LANGDECK_DEFAULT_ROLE` | `viewer` | Role for new users when no group matches. |
| `LANGDECK_BOOTSTRAP_ADMINS` | | Verified emails that always become `admin` (first-time setup). |
| `LANGDECK_ALLOWED_EMAIL_DOMAINS` | | If set, only these email domains may sign in. |
| `LANGDECK_SESSION_TTL` | `12h` | Browser session lifetime. |
| `LANGDECK_BOOTSTRAP_AGENT_KEY` | | Pre-shared agent key (`ldk_…`, 36+ chars) created at startup, for GitOps and compose setups. |
| `LANGDECK_CORS_ORIGINS` | | Extra origins allowed to call the API from a browser. Not needed for the bundled UI. |
| `LANGDECK_TRUST_PROXY_HEADERS` | `false` | Trust `X-Forwarded-For` / `X-Real-IP` (only behind a proxy you control). |
| `LANGDECK_LOGIN_RATE_LIMIT` | `1` | Auth requests per second per IP (burst 10). |
| `LANGDECK_API_RATE_LIMIT` | `50` | API requests per second per credential. |
| `LANGDECK_MAX_BODY_BYTES` | `4194304` | Maximum request body size. |
| `LANGDECK_RETENTION_DAYS` | `30` | Finished runs older than this are deleted. `0` keeps everything. |
| `LANGDECK_COMMAND_TTL` | `1h` | Undelivered commands expire after this long. |

### Chat notifications

Optional. With no endpoint set, notifications are off and cost nothing.

**From the UI:** admins can connect Slack, Mattermost, Microsoft Teams and Telegram under
**Notifications** in the sidebar, choose which events to send, and press *Send test message* to see
which channel delivers. Changes apply immediately, with no restart. Webhook URLs and tokens are
write-only: they are stored in the database (use a database with encryption at rest and restricted
access) and never returned by the API. Once you save from the UI, the saved settings take
precedence over the environment variables below. Webhook URLs must use https.

**From the environment** (GitOps, Helm): the variables below set the initial channels.

| Variable | Default | Description |
|---|---|---|
| `LANGDECK_NOTIFY_EVENTS` | `approval,run_failed,agent_offline` | Comma-separated events to send: `approval`, `run_failed`, `agent_offline`, `agent_online`. |
| `LANGDECK_NOTIFY_SLACK_WEBHOOK_URL` | | Slack incoming webhook URL. |
| `LANGDECK_NOTIFY_MATTERMOST_WEBHOOK_URL` | | Mattermost incoming webhook URL. |
| `LANGDECK_NOTIFY_TEAMS_WEBHOOK_URL` | | Microsoft Teams incoming webhook or Workflows URL. |
| `LANGDECK_NOTIFY_TELEGRAM_BOT_TOKEN` | | Telegram bot token from @BotFather. Set together with the chat id. |
| `LANGDECK_NOTIFY_TELEGRAM_CHAT_ID` | | Telegram chat or channel id (e.g. `-1001234567890`). |

Delivery is asynchronous with three attempts and exponential backoff; a 4xx response is treated
as permanent and not retried. Messages contain ids, names and a deep link — never event payloads,
so prompts and tool outputs stay inside LangDeck. If the queue fills (a webhook is down), events
are dropped and counted in `langdeck_notifications_dropped_total` rather than slowing requests.

Agent online/offline notices are emitted by the housekeeping loop, which polls agent status
every 30 s when notifications are enabled; on multi-replica deployments each replica tracks the
transition independently, so run the sweep on one replica (e.g. by enabling notifications only
there) if duplicates are a concern.

## Endpoints

| Path | Purpose |
|---|---|
| `GET /healthz` | Liveness (process is up). |
| `GET /readyz` | Readiness (database reachable). |
| `:9090/metrics` | Prometheus metrics: `langdeck_http_requests_total`, `langdeck_http_request_duration_seconds`, `langdeck_events_ingested_total`, `langdeck_commands_delivered_total`, Go runtime. |

See [api.md](api.md) for the HTTP API.
