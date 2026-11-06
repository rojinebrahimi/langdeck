# syntax=docker/dockerfile:1.7
# LangDeck: one static binary with the UI embedded, on a distroless non-root base.

ARG GO_VERSION=1.26
ARG NODE_VERSION=24

# ---- UI ----
FROM --platform=$BUILDPLATFORM node:${NODE_VERSION}-alpine AS ui
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

# ---- Go ----
FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-alpine AS build
ARG TARGETOS TARGETARCH
ARG VERSION=dev
ARG COMMIT=none
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
COPY --from=ui /src/web/dist ./web/dist
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" -o /out/langdeck ./cmd/langdeck

# ---- Runtime ----
FROM gcr.io/distroless/static-debian12:nonroot
ARG VERSION=dev
LABEL org.opencontainers.image.title="LangDeck" \
      org.opencontainers.image.description="Open-source control plane for LangGraph agents" \
      org.opencontainers.image.source="https://github.com/rojinebrahimi/langdeck" \
      org.opencontainers.image.licenses="Apache-2.0" \
      org.opencontainers.image.version="${VERSION}"
COPY --from=build /out/langdeck /langdeck
USER 65532:65532
EXPOSE 8080 9090
ENV LANGDECK_ADDR=:8080 LANGDECK_METRICS_ADDR=:9090
HEALTHCHECK --interval=15s --timeout=3s --start-period=10s --retries=3 CMD ["/langdeck", "healthcheck"]
ENTRYPOINT ["/langdeck"]
CMD ["serve"]
