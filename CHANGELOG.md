# Changelog

All notable changes follow [Keep a Changelog](https://keepachangelog.com/) and
[Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added
- Control plane server in Go: agent registry, run tracing, live updates (SSE), pull-based command channel.
- Remote control: start, cancel, pause/resume, versioned runtime configuration.
- Human-in-the-loop approvals.
- OIDC login (Authorization Code + PKCE), RBAC (viewer/operator/admin), API keys, CSRF protection, audit log.
- PostgreSQL store with embedded migrations and multi-replica fan-out via LISTEN/NOTIFY.
- React + Tailwind UI with graph visualization and execution-path replay.
- Python SDK for LangGraph (sync and async).
- Docker image, docker compose stack and Helm chart.
