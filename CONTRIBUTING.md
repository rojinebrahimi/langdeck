# Contributing to LangDeck

Thanks for helping build an open control plane for LangGraph agents! This guide gets you
from clone to merged pull request.

## Ground rules

- Be kind. We follow the [Code of Conduct](CODE_OF_CONDUCT.md).
- **Security issues go to [SECURITY.md](SECURITY.md)**, never to public issues.
- For anything bigger than a bug fix, open an issue first so we can agree on the approach
  before you invest the time.
- First-time contributors sign the [CLA](CLA.md) once (the bot explains how on your PR).

## Repository layout

| Path | What lives there |
|---|---|
| `cmd/langdeck` | Server entrypoint (`serve`, `healthcheck`, `version`) |
| `internal/api` | HTTP handlers: management API, agent API, SSE stream |
| `internal/auth` | Sessions, API keys, OIDC (PKCE), RBAC, CSRF |
| `internal/store` | Storage contract, `memory` and `postgres` implementations, shared contract tests |
| `internal/events` | Change notifications (in-process, or Postgres `LISTEN/NOTIFY` across replicas) |
| `internal/server` | HTTP middleware: security headers, rate limits, metrics, request ids |
| `web/` | React + TypeScript + Tailwind UI, embedded into the Go binary |
| `sdk/python` | `langdeck` Python SDK for LangGraph |
| `examples/` | Runnable demo agents |
| `deploy/helm` | Helm chart |
| `docs/` | Architecture, security, configuration and SDK docs |

## Development setup

Prerequisites: Go 1.26+, Node 24+, Python 3.10+, and optionally Docker and Helm.

```bash
git clone https://github.com/rojinebrahimi/langdeck && cd langdeck
(cd web && npm ci)
make dev            # API on :8080 (in-memory store, dev auth) + UI with hot reload on :5173
```

Open http://localhost:5173 and sign in with any email (dev auth makes you admin).

To connect an agent:

```bash
cd sdk/python && python -m venv .venv && . .venv/bin/activate && pip install -e ".[dev]"
# create an "agent" API key in the UI, then:
LANGDECK_URL=http://localhost:8080 LANGDECK_API_KEY=ldk_... python ../../examples/python-agent/agent.py
```

Prefer containers? `docker compose up --build` runs LangDeck, PostgreSQL and the demo agent.

## Tests

Every change needs tests. Run everything with `make test`, or one suite at a time:

```bash
make test-go     # Go, with -race
make test-ui     # tsc + vitest
make test-sdk    # pytest
```

PostgreSQL tests run when `LANGDECK_TEST_DATABASE_URL` is set, for example:

```bash
docker run -d --rm -p 5432:5432 -e POSTGRES_PASSWORD=pg postgres:17-alpine
LANGDECK_TEST_DATABASE_URL=postgres://postgres:pg@localhost:5432/postgres?sslmode=disable make test-go
```

New store behaviour goes into `internal/store/storetest`, so both implementations are
held to the same contract. The end-to-end test in `sdk/python/tests/test_e2e.py` drives
a real server with a real LangGraph graph; CI runs it on every pull request.

## Style

- **Go:** `gofmt`, `go vet` and `golangci-lint run` must pass. Return domain errors
  (`domain.ErrNotFound`, ...) from stores and let `api.fail` map them to HTTP codes.
  Never leak internal error text to clients.
- **TypeScript:** strict mode, no `any`. Keep server state in TanStack Query; use the
  shared components in `web/src/components/ui.tsx` and the purple theme tokens in
  `web/src/index.css`.
- **Python:** `ruff check` and `ruff format`. The SDK must never block or crash the host
  agent because LangDeck is unreachable.
- **Comments** explain *why*, not *what*.

## Database migrations

Add a new file `internal/store/postgres/migrations/NNNN_description.sql`. Never edit a
migration that has been released. Migrations run automatically at startup under an
advisory lock, so they must be safe to run while older replicas are still serving.

## Commits and pull requests

- Use [Conventional Commits](https://www.conventionalcommits.org/): `feat(api): ...`,
  `fix(sdk): ...`, `docs: ...`, `chore(deps): ...`.
- Keep PRs focused; one logical change per PR.
- Fill in the PR template, including how you tested.
- CI must be green: Go, UI, SDK (3.10–3.13), end-to-end, Helm, Docker build + Trivy scan,
  CodeQL and the CLA check.

## Releasing (maintainers)

1. Update `CHANGELOG.md`, `deploy/helm/langdeck/Chart.yaml` and `sdk/python/src/langdeck/_version.py`.
2. Tag: `git tag v0.2.0 && git push origin v0.2.0`.
3. The Release workflow publishes multi-arch images to Docker Hub and GHCR (signed with
   cosign, with SBOM and provenance), pushes the Helm chart to Docker Hub as an OCI
   artifact, attaches binaries to a GitHub release and, if enabled, publishes the SDK to PyPI.

Thank you for contributing!
