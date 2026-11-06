# Changelog

All notable changes follow [Keep a Changelog](https://keepachangelog.com/) and
[Semantic Versioning](https://semver.org/).

## [Unreleased]

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

[Unreleased]: https://github.com/rojinebrahimi/langdeck/compare/v0.1.1...HEAD
[0.1.1]: https://github.com/rojinebrahimi/langdeck/releases/tag/v0.1.1
[0.1.0]: https://github.com/rojinebrahimi/langdeck/compare/49e2b3f...v0.1.0
