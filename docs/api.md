# HTTP API

All endpoints live under `/api/v1` and speak JSON. Errors look like `{"error": "message"}`.

**Authentication:** browser sessions (cookie + `X-CSRF-Token` header on writes) or
`Authorization: Bearer ldk_…` API keys. Roles: `viewer` < `operator` < `admin`; `agent`
keys can only use the agent API.

## Management API

| Method | Path | Role | Description |
|---|---|---|---|
| GET | `/me` | viewer | Current principal and CSRF token |
| GET | `/stats` | viewer | Dashboard summary (last 24h) |
| GET | `/stream` | viewer | Server-Sent Events: `agent.updated`, `run.updated`, `interrupt.updated`, `command.created` |
| GET | `/agents?q=&status=&after=&limit=` | viewer | List agents by name with derived status. `q` searches name and description, `status` is `online`, `paused` or `offline`, `limit` defaults to 24 (max 500). Returns `total` and `next_after`, the cursor for the next page (`null` on the last page) |
| GET | `/agent-graph` | viewer | Declared dependency graph: `{"nodes":[{id,name,status,registered,accesses}],"edges":[{source,target}]}`; edge means source depends on target; unregistered targets have `registered:false`, `status:"unknown"` |
| GET | `/agents/{id}` | viewer | Agent with graph, accesses, dependencies and current config |
| GET | `/agents/{id}/configs` | viewer | Config versions, newest first |
| GET | `/agents/{id}/commands` | viewer | Recent commands and their delivery status |
| POST | `/agents/{id}/runs` | operator | Start a run: `{"input": {...}, "thread_id": "..."}` |
| POST | `/agents/{id}/pause` | operator | Pause: the agent refuses new runs |
| POST | `/agents/{id}/resume` | operator | Resume |
| POST | `/agents/{id}/configs` | operator | New config version: `{"config": {...}, "comment": "..."}` |
| GET | `/agents/{id}/image` | viewer | The agent's picture (404 if none). `image_updated_at` on the agent says whether one exists |
| PUT | `/agents/{id}/image` | operator | Upload a picture as the raw request body: PNG, JPEG, WebP or GIF, max 512 KB (type is sniffed, SVG refused) |
| DELETE | `/agents/{id}/image` | operator | Remove the picture |
| DELETE | `/agents/{id}` | admin | Delete an agent and all its data |
| GET | `/runs?agent_id=&status=&thread_id=&before=&limit=` | viewer | List runs, newest first; paginate with `next_before` |
| GET | `/runs/{id}` | viewer | Run with input and output |
| GET | `/runs/{id}/events?after=&limit=` | viewer | Trace events |
| POST | `/runs/{id}/cancel` | operator | Cancel a run |
| GET | `/interrupts?status=pending` | viewer | Approval requests |
| POST | `/interrupts/{id}/resolve` | operator | `{"decision": "approve" \| "reject", "response": {...}}` |
| GET/POST | `/api-keys` | admin | List / create (`{"name","role","agent_name","expires_in_days"}`); the secret is returned once |
| DELETE | `/api-keys/{id}` | admin | Revoke |
| GET | `/users` | admin | List users |
| PATCH | `/users/{id}` | admin | `{"role": "operator"}` |
| GET | `/audit?limit=&before=` | admin | Audit log, newest first |

## Agent API (used by the SDK)

| Method | Path | Description |
|---|---|---|
| POST | `/agent/register` | Upsert the agent (name, version, graph, labels, accesses, depends_on); returns desired state and latest config. `accesses` is `[{"resource","modes":["read"\|"write"\|"execute"\|"delete"\|"admin"],"description"}]` (max 64); `depends_on` is a list of agent names (max 64, not itself). Omit either to keep the stored value; send `[]` to clear it |
| POST | `/agent/heartbeat` | `{"agent","instances"}`; returns desired state and config version |
| POST | `/agent/events` | `{"agent","events":[{run_id,type,node,ts,latency_ms,payload,thread_id}]}` (max 500) |
| GET | `/agent/commands?agent=&wait=25s` | Long-poll for commands (max 30s) |
| POST | `/agent/commands/{id}/ack` | `{"agent","ok","result"}` |
| POST | `/agent/interrupts` | Create an approval request |
| GET | `/agent/interrupts/{id}?wait=25s` | Long-poll for the decision |

Event types: `run_start`, `run_end`, `run_error`, `node_start`, `node_end`, `node_error`,
`llm_start`, `llm_end` (payload `usage.prompt_tokens/completion_tokens` is summed into the
run), `tool_start`, `tool_end`, `interrupt`, `log`.
