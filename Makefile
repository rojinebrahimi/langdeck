# LangDeck developer tasks. Run `make help` for the list.
SHELL := /bin/bash
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
IMAGE   ?= rojinebrahimi/langdeck
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT)

.PHONY: help
help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[35m%-14s\033[0m %s\n", $$1, $$2}'

.PHONY: dev
dev: ## Run the API (dev auth, in-memory store) and the UI dev server
	@trap 'kill 0' EXIT; \
	LANGDECK_LOG_FORMAT=text LANGDECK_AUTH_MODE=dev go run ./cmd/langdeck & \
	cd web && npm run dev

.PHONY: ui
ui: ## Build the UI into web/dist (embedded by the Go build)
	cd web && npm ci --no-audit --no-fund && npm run build

.PHONY: build
build: ui ## Build the single binary into bin/langdeck
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/langdeck ./cmd/langdeck

.PHONY: test
test: test-go test-ui test-sdk ## Run every test suite

.PHONY: test-go
test-go: ## Go tests with the race detector (set LANGDECK_TEST_DATABASE_URL for Postgres)
	go test -race -count=1 -coverprofile=coverage.out ./...

.PHONY: test-ui
test-ui: ## UI type-check and unit tests
	cd web && npm run lint && npm test

.PHONY: test-sdk
test-sdk: ## Python SDK tests
	cd sdk/python && python -m pytest -q

.PHONY: lint
lint: ## Static analysis
	go vet ./...
	@command -v golangci-lint >/dev/null && golangci-lint run || echo "golangci-lint not installed, skipped"
	cd sdk/python && ruff check src tests && ruff format --check src tests

.PHONY: docker
docker: ## Build the container image
	docker build --build-arg VERSION=$(VERSION) --build-arg COMMIT=$(COMMIT) -t $(IMAGE):$(VERSION) .

.PHONY: up
up: ## Start LangDeck + Postgres + demo agent with docker compose
	docker compose up --build

.PHONY: helm-lint
helm-lint: ## Lint and render the Helm chart
	helm lint deploy/helm/langdeck
	helm template t deploy/helm/langdeck >/dev/null
