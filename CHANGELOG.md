# Changelog

All notable changes follow [Keep a Changelog](https://keepachangelog.com/) and
[Semantic Versioning](https://semver.org/).

## [Unreleased]

## [0.2.1] - 2026-11-10

### Fixed
- Opening a run with a long trace was very slow (about 4,200 DOM nodes and ~330 ms per frame for a
  480-event run). The trace now renders 100 events at a time with "Show more" / "Show all", rows
  are memoised, and timestamps use one shared formatter. About 290 nodes for the same run.
- Slow scrolling in the UI: the page background no longer repaints on every scroll frame
  (`background-attachment: fixed`), and cards and the header no longer use backdrop blur.

## [0.2.0] - 2026-11-10

### Added
- **Agent accesses and dependencies.** An agent can declare the resources it touches and how
  (`read`, `write`, `execute`, `delete`, `admin`) with `LangDeck(accesses=...)`, and the agents it
  calls with `depends_on=[...]`. The agent page has an Access tab, and a new Dependencies page
  (`GET /api/v1/agent-graph`) draws the graph across agents. Both are self-reported and not
  enforced. Migration `0003` adds two nullable columns to `agents`.
- **Agent pagination.** `GET /api/v1/agents` is keyset-paginated by name (`limit`, `after`,
  `next_after`) and returns `total`, with server-side search (`q`, name and description) and a
  `status` filter (`online`, `paused`, `offline`). The Agents page has a search box, status filter
  and Previous/Next pager, with filters kept in the URL.
- **Agent pictures.** Operators can upload a picture for an agent (PNG, JPEG, WebP or GIF, up to
  512 KB; the UI shrinks it first). It shows on the Agents list and the agent page. Stored in its
  own table (migration `0004`), served with `nosniff` and a locked-down CSP; SVG is refused.
  `PUT`/`GET`/`DELETE /api/v1/agents/{id}/image`.
- The agent page's Runs tab pages through history instead of stopping at 50.
- The Runs page pages through history (it previously stopped at the newest 100 runs).

### Changed
- **Faster with many runs.** Run lists no longer read or send each run's input and output (the
  detail view still does); `GET /api/v1/stats` is computed from a light agent query and reused for
  5 s across viewers; the UI coalesces live updates over 1.5 s instead of 0.4 s; and migration
  `0005` adds `(status, created_at)` and `(thread_id, created_at)` indexes so filtered run lists
  stay index-ordered.
- `GET /api/v1/agents` returns 24 agents by default; pass `limit` (up to 500) or follow
  `next_after` to get more.

## [0.1.2] - 2026-11-07

The Python SDK is now on PyPI:

```bash
pip install langdeck              # or: pip install "langdeck[langgraph]"
```

### Added
- `langdeck` published to PyPI with trusted publishing (OIDC from GitHub Actions, no API token).
- PyPI project links (source, issues, changelog, demo), Python version classifiers and a `py.typed`
  marker, so type checkers use the SDK's annotations.

### Security
- **Public demo no longer exposes visitors.** The demo asked for an email and made every visitor an
  admin, so anyone could list other visitors' emails under Users and their IP addresses in the
  audit log. A new demo mode (`LANGDECK_DEMO_MODE=true`) signs visitors in with one click under a
  random anonymous identity and the operator role, never stores IP addresses, and keeps visitors
  out of users, API keys and the audit log. It is refused in production.

### Changed
- CI builds the sdist and wheel and runs `twine check --strict` on every push.
- Releases refuse to publish the SDK when its version differs from the tag.
- GitHub Actions updated: checkout v7, setup-go v7, build-push-action v7, setup-qemu-action v4.

## [0.1.1] - 2026-11-06

The first public release of LangDeck, a self-hosted control plane for LangGraph agents.
`0.1.0` shipped container images only; `0.1.1` is the first full release, with binaries and the
Helm chart.

**Try it:** `docker run -p 8080:8080 rojinebrahimi/langdeck:0.1.1`, then sign in as
`admin` / `admin` (you will be asked to change the password). There's also a
[live demo](https://langdeck-demo.onrender.com) where any email signs in.

### Highlights

- **See every agent.** Agents register with name, version, labels and graph topology; heartbeats
  drive live online / paused / offline status.
- **Trace every run.** Node, LLM and tool steps with inputs, outputs, latency and token usage,
  streamed to the UI and replayed on the graph.
- **Steer from the UI.** Start runs, cancel them mid-flight, pause or resume agents, and push
  versioned runtime config with history and rollback, with no redeploy.
- **Keep a human in the loop.** `deck.request_approval(...)` pauses a node until an operator
  approves or rejects it in the Approvals inbox.
- **Get told in chat.** Approvals, failed runs and offline agents go to Slack, Mattermost,
  Microsoft Teams or Telegram, with ids and a deep link but never payloads.

### Added

**Server**
- Single Go binary with the UI embedded; in-memory store for trials, PostgreSQL with embedded
  migrations for production.
- Pull-based control channel: agents only make outbound HTTPS calls and long-poll for commands.
  At-least-once delivery with de-duplication, expiry and `SKIP LOCKED` claims.
- Stateless replicas kept in sync with PostgreSQL `LISTEN/NOTIFY`; Server-Sent Events to the UI.
- Fleet stats: runs per hour, failure rate, p50/p95 latency, token usage, busiest agents.
- Housekeeping jobs for retention, session cleanup and command expiry.
- Prometheus metrics on a separate port, plus `/healthz` and `/readyz`.
- Chat notifications for Slack, Mattermost, Microsoft Teams and Telegram (`LANGDECK_NOTIFY_*`),
  asynchronous with bounded retries.

**Security**
- Built-in accounts (PBKDF2-SHA256, 600k iterations) with a default `admin` / `admin` that must
  be changed on first login.
- OIDC single sign-on: Authorization Code + PKCE, `state` and `nonce`, group-to-role mapping,
  bootstrap admins and allowed email domains.
- Server-enforced RBAC (viewer, operator, admin) and agent-only API keys that can be bound to one
  agent, set to expire and revoked instantly.
- Sessions and keys stored only as SHA-256 hashes; `__Host-` cookies, CSRF synchronizer tokens,
  strict CSP, HSTS, rate limits, body and payload caps.
- Audit log of every state change with actor, target and IP.
- Production guard rails: dev sign-in, missing database or plain-HTTP public URLs are refused
  when `LANGDECK_ENV=production`.

**UI**
- React + Tailwind interface in purple: overview dashboard, agents with graph view, run traces with
  execution-path replay, approvals inbox, API keys, users and audit log.
- In-app user guide, GitHub link, and a logo that takes you home.

**Python SDK** (`langdeck` 0.1.0)
- `deck.instrument(graph)` traces any compiled `StateGraph` through a LangChain callback;
  sync and async.
- Remote runs (with `input_mapper` or `@deck.on_run`), cancellation at node boundaries,
  pause/resume, live config and `@deck.on_config`.
- `request_approval` / `arequest_approval` for human-in-the-loop decisions.
- Background event buffer: if LangDeck is unreachable, the agent keeps running.
- Secret redaction and an opt-out for capturing payloads.

**Deployment**
- Multi-arch (`linux/amd64`, `linux/arm64`) distroless, non-root image on Docker Hub and GHCR,
  cosign-signed with SBOM and provenance.
- Helm chart at `oci://ghcr.io/rojinebrahimi/charts/langdeck` with HPA, PDB, NetworkPolicy,
  ServiceMonitor and an optional evaluation PostgreSQL.
- Docker Compose stack with a demo agent, and a Render blueprint for a throwaway demo.
- Static binaries for Linux, macOS and Windows attached to this release.

**Release pipeline** (fixed since 0.1.0)
- The Helm chart is published to `oci://ghcr.io/rojinebrahimi/charts/langdeck` instead of the
  image repository, where it would have overwritten the image's version tag.
- The Docker Hub overview sync can no longer block a release, and syncs a Docker Hub-ready page.

**Docs and project**
- Architecture, configuration, HTTP API, security and user guides.
- Contribution guide, CLA with a signature bot, code of conduct and security policy.
- CI: Go tests with race detection against real PostgreSQL, UI and SDK tests, an end-to-end test,
  golangci-lint, CodeQL, govulncheck, Trivy and Helm validation.

## [0.1.0] - 2026-11-06

Container images only (`rojinebrahimi/langdeck:0.1.0`). The release pipeline stopped before
publishing the GitHub release and Helm chart; see 0.1.1, which contains the same features.

[Unreleased]: https://github.com/rojinebrahimi/langdeck/compare/v0.1.2...HEAD
[0.1.2]: https://github.com/rojinebrahimi/langdeck/releases/tag/v0.1.2
[0.1.1]: https://github.com/rojinebrahimi/langdeck/releases/tag/v0.1.1
[0.1.0]: https://github.com/rojinebrahimi/langdeck/compare/49e2b3f...v0.1.0
