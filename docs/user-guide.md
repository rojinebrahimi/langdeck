# LangDeck user guide

This guide is for the people who *use* LangDeck — operators, SREs and engineers who run
LangGraph agents in production and need to see them, steer them and approve what they do. It
assumes LangDeck is already running (see the [README](../README.md) or the
[Helm chart](../deploy/helm/langdeck/values.yaml) for installation).

If you are wiring up an agent for the first time, read
[Connecting an agent](#3-connecting-an-agent); the rest of the guide is about the day-to-day.

- [1. Signing in](#1-signing-in)
- [2. Concepts](#2-concepts)
- [3. Connecting an agent](#3-connecting-an-agent)
- [4. Watching a run](#4-watching-a-run)
- [5. Controlling agents](#5-controlling-agents)
- [6. Approvals](#6-approvals)
- [7. Runtime configuration](#7-runtime-configuration)
- [8. Keys, users and roles](#8-keys-users-and-roles)
- [9. Dashboard and metrics](#9-dashboard-and-metrics)
- [10. Audit log](#10-audit-log)
- [11. Chat notifications](#11-chat-notifications)
- [12. Troubleshooting](#12-troubleshooting)

## 1. Signing in

LangDeck supports two authentication modes, chosen by the administrator.

**Built-in accounts** (`AUTH_MODE=local`, the default). Sign in with a username and password.
A fresh installation ships with `admin` / `admin`, and the first login is forced through a
password change before anything else is reachable. Passwords are hashed with PBKDF2-SHA256
(600k iterations) and sessions live in `__Host-` `HttpOnly` cookies.

**OIDC SSO** (`AUTH_MODE=oidc`). You are redirected to your identity provider and come back
with a session. Your role comes from the groups in your token — the administrator maps group
names to `viewer`, `operator` or `admin` (see [docs/security.md](security.md#oidc-setup)). No
LangDeck account is created by hand.

**Dev mode** (`AUTH_MODE=dev`) is for local trials: any email signs in as an administrator. It is
refused outright when `LANGDECK_ENV=production`, and the UI shows a `dev auth` badge so you can
never mistake it for a real deployment.

**Demo mode** (`LANGDECK_DEMO_MODE=true`, on top of dev mode) is what the public demo runs. Visitors
click *Enter the demo* and get a random anonymous identity (`visitor-…@demo.invalid`) with the
**operator** role. No email is collected, no IP address is stored, and visitors cannot open users,
API keys or the audit log, so they never see each other or break the demo agent.

Signing out drops the session server-side; API keys are unaffected.

## 2. Concepts

| Term | Meaning |
|---|---|
| **Agent** | One logical agent, e.g. `support-bot`. Many processes can register under the same name; `instances` counts them. |
| **Instance** | One running process of an agent. It heartbeats every 15 s; three missed beats (45 s) mark it offline. |
| **Run** | One execution of the graph, started by the SDK or from the UI. Runs carry status, trigger, latency, token usage and output. |
| **Event** | A step inside a run: node start/end, LLM call, tool call, log line. Events are what the trace view renders. |
| **Command** | A control instruction (pause, resume, cancel run, start run, update config) delivered to the agent by long-poll. |
| **Interrupt** | A question from the agent that needs a human decision — the human-in-the-loop inbox. |
| **Config** | Versioned JSON the agent reads at runtime. Pushing a new version is instant; rollback is another push. |
| **API key** | A `ldk_…` credential for agents or scripts, stored only as a SHA-256 hash. |

Statuses you will see:

- Agent: **online** (heartbeat fresh), **paused** (operator paused it), **offline** (no heartbeat).
- Run: **pending** (queued), **running**, **interrupted** (waiting for approval), **succeeded**, **failed**, **cancelled**.

## 3. Connecting an agent

1. **Create a key.** *Settings → API keys → New key*, role **agent**. Binding it to an agent
   name limits the key to that agent. Copy it once — the value is never shown again.
2. **Install the SDK** in the agent's environment: `pip install langdeck`.
3. **Configure** two environment variables:

   ```bash
   export LANGDECK_URL=https://langdeck.example.com
   export LANGDECK_API_KEY=ldk_...
   ```

4. **Instrument your graph**:

   ```python
   from langdeck import LangDeck

   deck = LangDeck(name="support-bot", version="1.0.0")
   app = deck.instrument(builder.compile())
   deck.start()
   ```

5. **Check the Agents page.** The agent should appear within a second or two with status
   **online** and its graph rendered. If it does not, see [Troubleshooting](#11-troubleshooting).

Agents only make outbound HTTPS requests, so they work behind NAT, in private subnets and
inside a VPC with no ingress rules.

### Declaring accesses and dependencies

Tell LangDeck what the agent can touch and which other agents it calls:

```python
deck = LangDeck(
    name="support-bot",
    accesses={
        "postgres:orders": ["read", "write"],
        "s3:reports": "read",
        "zendesk-api": {"modes": ["execute"], "description": "close tickets"},
    },
    depends_on=["billing", "kb-search"],
)
```

- **Accesses** show on the agent's **Access** tab. Modes are `read`, `write`, `execute`, `delete`
  and `admin`; resource names are free text, so pick a convention such as `kind:name`.
- **Dependencies** are drawn on the **Dependencies** page, with each agent's online/offline state.
  A dependency nobody has registered yet is listed as "declared but not registered".
- Both are **self-reported** by the agent. LangDeck displays them; it does not enforce them, so
  they document intent rather than act as a permission boundary. Leave them out and the last
  declared values are kept; pass an empty value (`accesses={}`, `depends_on=[]`) to clear them.

## 4. Watching a run

The **Runs** page lists everything the fleet ran, newest first, filterable by agent and status.
Each row shows the agent, trigger (`sdk` or `ui`), status, duration and token counts.

Click a run to open the trace:

- **Graph panel** — the agent's topology with the executed path highlighted. Nodes that ran are
  marked, conditional edges show which branch was taken.
- **Event timeline** — node, LLM and tool steps in order. Expand a step to see its input, output,
  latency and token usage.
- **Live updates** — a running run streams events as they arrive over SSE; the page does not need
  refreshing.

The trace is restricted to what the SDK captures. Set `capture_payloads=False` in the SDK if you
do not want prompts and outputs stored (the SDK also redacts common secret patterns).

## 5. Controlling agents

All controls require the **operator** role and are recorded in the audit log.

| Action | Where | Effect |
|---|---|---|
| **Start a run** | Agent page → Start run | Sends a `start_run` command; the SDK invokes your graph with the JSON you typed (translated by `input_mapper` if you set one). |
| **Cancel a run** | Run page → Cancel | Marks the run cancelled and tells the agent; the SDK raises `RunCancelled` at the next node boundary. |
| **Pause / resume** | Agent page | Paused agents refuse new runs but keep heartbeating. In-flight runs continue. |
| **Delete an agent** | Agent page (admin) | Removes the agent and its runs. The SDK re-registers itself on its next heartbeat if it is still running. |

Commands are delivered at-least-once with de-duplication, so a flaky network cannot double-run
a command. If an agent is offline, commands queue and are applied when it returns, subject to
the command TTL.

## 6. Approvals

When an agent calls `deck.request_approval(...)`, the run pauses at that node and the question
appears in **Approvals** (and as a badge count in the sidebar). The card shows the agent, node,
run and the payload you passed.

- **Approve** returns `True` to the agent; **Reject** returns `False`.
- The optional note is passed back as `decision.response`.
- Requests have a timeout (default one hour); a timed-out request behaves as rejected, so a
  forgotten approval can never wedge a run forever.

Approvals are the recommended way to gate irreversible actions — refunds, emails, deploys,
writes to a production database.

## 7. Runtime configuration

The **Config** panel on an agent page holds arbitrary JSON, versioned. Push a new version and
every connected instance picks it up within one heartbeat; older versions are ignored, so
out-of-order delivery cannot regress config.

Use it for the things you would otherwise redeploy to change: model name, temperature, prompt
text, feature flags, thresholds.

```python
model = deck.config.get("model", "gpt-4o-mini")
```

History is kept per agent with the author and timestamp, and any earlier version can be restored
from the UI — rollback is just another push.

## 8. Keys, users and roles

Roles are hierarchical: **viewer** < **operator** < **admin**.

| Capability | viewer | operator | admin |
|---|:--:|:--:|:--:|
| See dashboard, agents, runs, traces | ✅ | ✅ | ✅ |
| Start / cancel runs, pause, push config, approve | | ✅ | ✅ |
| Manage API keys, users, delete agents, read audit log | | | ✅ |

API keys have a role of their own: **agent** keys can only call the agent API
(register, heartbeat, events, commands, interrupts) and cannot read the management API.

- Set an expiry when creating a key if it is for a temporary environment.
- Revoke a key from the same page; it stops working immediately.
- `Last used` tells you which keys are live and which are safe to delete.

To remove a person, revoke their session from *Settings → Users* (OIDC users lose access at
their next request; local users can also be deleted).

## 9. Dashboard and metrics

The overview answers "is the fleet healthy right now":

- **Runs per hour** for the last 24 hours, split by status.
- **Success rate** and **p50/p95 latency** over the selected window.
- **Token usage** for prompt and completion.
- **Busiest agents** — where the load actually is.
- **Agent health** — online / paused / offline counts.

For alerting, scrape Prometheus metrics at `/metrics` (separate port, default `:9090` on the
Helm chart) — request counts, latencies and SSE subscriber gauges are exported there.

## 10. Audit log

**Settings → Audit log** lists every state change with actor, action, target and IP: run starts
and cancels, config pushes, approvals, key creation and revocation, user and agent changes. It
is append-only and paginated. Administrators can export it with the API
(`GET /api/v1/audit`) for compliance retention.

## 11. Chat notifications

LangDeck can tell a channel when something needs a human, so nobody has to watch the
Approvals inbox. Configure any combination of Slack, Mattermost, Microsoft Teams and Telegram
from **Notifications** in the sidebar (admins), where you can also pick the events and press
*Send test message*, or with environment variables (see
[configuration](configuration.md#chat-notifications)). With nothing configured, the feature is off.

| Event | Sent when | Default |
|---|---|:--:|
| `approval` | An agent calls `request_approval` | ✅ |
| `run_failed` | A run ends with an error (cancelled runs are not failures) | ✅ |
| `agent_offline` | An agent misses heartbeats for 45 s | ✅ |
| `agent_online` | An agent that was reported offline comes back | |

Each message carries the agent, node or run id and a link straight to the right page — the
decision itself is still made in LangDeck, where it is authenticated and audited. Event
payloads (prompts, tool inputs, approval payloads) are never sent to chat.

| Service | What to create |
|---|---|
| Slack | An app with *Incoming Webhooks* → copy the `https://hooks.slack.com/services/…` URL |
| Mattermost | *Integrations → Incoming Webhooks* → copy the hook URL |
| Microsoft Teams | A channel *Incoming Webhook* connector, or a Workflows "post to channel when a webhook request is received" URL |
| Telegram | A bot from @BotFather (token), added to the group or channel; use its chat id (e.g. `-1001234567890`) |

Treat webhook URLs and bot tokens as secrets: anyone holding them can post to your channel.
The Helm chart reads them from the LangDeck Secret (`notify-*` keys).

## 12. Troubleshooting

**The agent shows offline.** Check that the process is running and can reach `LANGDECK_URL`
(`curl -H "Authorization: Bearer $LANGDECK_API_KEY" $LANGDECK_URL/api/v1/agent/commands?agent=NAME&wait=0`).
A `401` means the key is wrong, revoked or expired; a `403` means it is not an `agent` key or is
bound to a different agent name. Heartbeats every 15 s keep it online — if the network is flaky,
the agent will flap between online and offline.

**The graph is missing.** The graph is exported at registration. `instrument()` must receive the
*compiled* graph; if you register before instrumenting, re-register by restarting the process.

**Runs started from the UI do nothing.** The SDK must be running with either an instrumented graph
or an `@deck.on_run` handler. Check the command result on the agent page — an unsupported command
is acknowledged with an error rather than silently dropped.

**The UI says "Offline" in the header.** That is the SSE connection, not the server. It
reconnects automatically; a proxy that buffers responses (or strips `text/event-stream`) will
keep it offline — disable buffering for `/api/v1/stream`.

**403 on every write.** Your session or key lacks the role. Approving, starting runs and pushing
config need **operator**; keys and users need **admin**. Browser writes also need the CSRF token
that the UI sends automatically — scripted clients using a session cookie must send
`X-CSRF-Token` too.

**Approvals never arrive.** `request_approval` must run inside an instrumented run (it uses the
current run id) or be given an explicit `run_id`. It blocks that run only; other runs continue.

Still stuck? Open an issue with the LangDeck server logs (`LANGDECK_LOG_LEVEL=debug`) and the
run id — see [CONTRIBUTING.md](../CONTRIBUTING.md).
