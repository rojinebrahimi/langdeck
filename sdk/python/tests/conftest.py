"""A tiny in-process fake of the LangDeck agent API, built on httpx.MockTransport."""

from __future__ import annotations

import json
import threading

import httpx
import pytest

from langdeck import LangDeck

KEY = "ldk_" + "x" * 43


class FakeServer:
    def __init__(self) -> None:
        self.lock = threading.Lock()
        self.registrations: list[dict] = []
        self.events: list[dict] = []
        self.commands: list[dict] = []
        self.acks: dict[str, dict] = {}
        self.interrupts: dict[str, dict] = {}
        self.desired_state = "running"
        self.config: dict | None = None
        self.config_version = 0

    def handler(self, request: httpx.Request) -> httpx.Response:
        assert request.headers["authorization"] == f"Bearer {KEY}"
        path = request.url.path
        body = json.loads(request.content) if request.content else {}
        with self.lock:
            if path == "/api/v1/agent/register":
                self.registrations.append(body)
                return httpx.Response(
                    200,
                    json={
                        "agent": {"name": body["name"]},
                        "desired_state": self.desired_state,
                        "config": self.config,
                        "config_version": self.config_version,
                    },
                )
            if path == "/api/v1/agent/heartbeat":
                return httpx.Response(
                    200, json={"desired_state": self.desired_state, "config_version": self.config_version}
                )
            if path == "/api/v1/agent/events":
                self.events.extend(body["events"])
                return httpx.Response(202, json={"accepted": len(body["events"])})
            if path == "/api/v1/agent/commands":
                out, self.commands = self.commands, []
                return httpx.Response(200, json={"commands": out})
            if path.endswith("/ack"):
                self.acks[path.split("/")[-2]] = body
                return httpx.Response(204)
            if path == "/api/v1/agent/interrupts":
                self.interrupts[body["id"]] = {**body, "status": "pending"}
                return httpx.Response(201, json=self.interrupts[body["id"]])
            if path.startswith("/api/v1/agent/interrupts/"):
                return httpx.Response(200, json=self.interrupts[path.rsplit("/", 1)[1]])
        return httpx.Response(404, json={"error": "not found"})

    def types(self) -> list[str]:
        return [e["type"] for e in self.events]


@pytest.fixture
def server() -> FakeServer:
    return FakeServer()


@pytest.fixture
def deck(server: FakeServer):
    client = httpx.Client(base_url="http://deck.test", transport=httpx.MockTransport(server.handler))
    d = LangDeck(
        "test-agent",
        url="http://deck.test",
        api_key=KEY,
        http_client=client,
        heartbeat_interval=3600,
        poll_wait=0.05,
        flush_interval=0.05,
    )
    yield d
    d.stop()
