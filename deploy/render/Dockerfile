# Public demo image for Render's free tier: the LangDeck server and the demo
# LangGraph agent in one container (free plans have no background workers).
# Not for production: it uses dev auth and the in-memory store.

ARG GO_VERSION=1.26
ARG NODE_VERSION=24

FROM node:${NODE_VERSION}-alpine AS ui
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

FROM golang:${GO_VERSION}-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=ui /src/web/dist ./web/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=demo" -o /out/langdeck ./cmd/langdeck

FROM python:3.12-slim
ENV PYTHONDONTWRITEBYTECODE=1 PYTHONUNBUFFERED=1
WORKDIR /app
COPY sdk/python /sdk
RUN pip install --no-cache-dir "langgraph>=0.2" /sdk && useradd -u 10001 -m demo
COPY --from=build /out/langdeck /usr/local/bin/langdeck
COPY examples/python-agent/agent.py /app/agent.py
COPY deploy/render/start.sh /app/start.sh
USER 10001
EXPOSE 10000
CMD ["sh", "/app/start.sh"]
