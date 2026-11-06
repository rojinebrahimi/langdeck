"""End-to-end: real LangDeck server + real LangGraph + this SDK.

Run a server first (CI does this):
    LANGDECK_BOOTSTRAP_AGENT_KEY=ldk_... langdeck serve
    LANGDECK_E2E_URL=http://localhost:8080 LANGDECK_E2E_KEY=ldk_... pytest tests/test_e2e.py
"""

from __future__ import annotations

import os
import threading
import time
import uuid
from typing import TypedDict

import httpx
import pytest

pytest.importorskip("langgraph")
from langgraph.graph import END, START, StateGraph  # noqa: E402

from langdeck import LangDeck  # noqa: E402

URL = os.environ.get("LANGDECK_E2E_URL")
KEY = os.environ.get("LANGDECK_E2E_KEY")
pytestmark = pytest.mark.skipif(not (URL and KEY), reason="LANGDECK_E2E_URL / LANGDECK_E2E_KEY not set")


class S(TypedDict, total=False):
    n: int
    approved: bool


def wait_for(fn, timeout=15.0):
    end = time.monotonic() + timeout
    while time.monotonic() < end:
        v = fn()
        if v:
            return v
        time.sleep(0.2)
    raise AssertionError("condition not met in time")


@pytest.fixture
def admin():
    """A browser-like admin session with its CSRF token (dev auth, or local admin password)."""
    c = httpx.Client(base_url=URL, timeout=10)
    r = c.post("/auth/dev-login", json={"email": "e2e@example.com"})
    if r.status_code == 404:
        r = c.post(
            "/auth/password-login",
            json={"username": "admin", "password": os.environ.get("LANGDECK_E2E_ADMIN_PASSWORD", "admin")},
        )
    r.raise_for_status()
    c.headers["X-CSRF-Token"] = c.get("/api/v1/me").json()["csrf_token"]
    yield c
    c.close()


def test_full_control_loop(admin):
    name = f"e2e-{uuid.uuid4().hex[:8]}"
    deck = LangDeck(name, url=URL, api_key=KEY, poll_wait=2, flush_interval=0.2, heartbeat_interval=1)

    def double(s: S) -> dict:
        return {"n": s["n"] * 2}

    def review(s: S) -> dict:
        return {"approved": deck.request_approval("Keep the result?", {"n": s["n"]}, timeout=30).approved}

    g = StateGraph(S)
    g.add_node("double", double)
    g.add_node("review", review)
    g.add_edge(START, "double")
    g.add_edge("double", "review")
    g.add_edge("review", END)
    deck.instrument(g.compile(), input_mapper=lambda inp: {"n": int(inp.get("n", 1))})
    deck.start()
    try:
        agent = wait_for(
            lambda: next((a for a in admin.get("/api/v1/agents").json()["agents"] if a["name"] == name), None)
        )
        assert agent["status"] == "online"
        detail = admin.get(f"/api/v1/agents/{agent['id']}").json()["agent"]
        assert {"double", "review"} <= {n["id"] for n in detail["graph"]["nodes"]}

        # Push a config and watch it arrive in the process.
        admin.post(f"/api/v1/agents/{agent['id']}/configs", json={"config": {"model": "e2e"}}).raise_for_status()
        wait_for(lambda: deck.config.get("model") == "e2e")

        # Start a run from the "UI".
        run = admin.post(f"/api/v1/agents/{agent['id']}/runs", json={"input": {"n": 21}}).json()["run"]
        interrupt = wait_for(
            lambda: next(
                (
                    i
                    for i in admin.get("/api/v1/interrupts?status=pending").json()["interrupts"]
                    if i["run_id"] == run["id"]
                ),
                None,
            )
        )
        assert interrupt["question"] == "Keep the result?" and interrupt["payload"] == {"n": 42}
        admin.post(f"/api/v1/interrupts/{interrupt['id']}/resolve", json={"decision": "approve"}).raise_for_status()

        final = wait_for(lambda: (r := admin.get(f"/api/v1/runs/{run['id']}").json())["status"] == "succeeded" and r)
        assert final["output"] == {"n": 42, "approved": True}
        events = admin.get(f"/api/v1/runs/{run['id']}/events").json()["events"]
        nodes = [(e["type"], e.get("node")) for e in events if e["type"].startswith("node_")]
        assert ("node_end", "double") in nodes and ("node_end", "review") in nodes

        # Pause from the UI; the SDK refuses new local runs.
        admin.post(f"/api/v1/agents/{agent['id']}/pause").raise_for_status()
        wait_for(lambda: deck.paused)
        admin.post(f"/api/v1/agents/{agent['id']}/resume").raise_for_status()
        wait_for(lambda: not deck.paused)
    finally:
        deck.stop()


def test_cancel_running_run(admin):
    name = f"e2e-cancel-{uuid.uuid4().hex[:6]}"
    deck = LangDeck(name, url=URL, api_key=KEY, poll_wait=1, flush_interval=0.2)
    gate = threading.Event()

    def slow(s: S) -> dict:
        gate.wait(10)
        return {"n": 1}

    def after(s: S) -> dict:
        return {"n": 2}

    g = StateGraph(S)
    g.add_node("slow", slow)
    g.add_node("after", after)
    g.add_edge(START, "slow")
    g.add_edge("slow", "after")
    g.add_edge("after", END)
    deck.instrument(g.compile())
    deck.start()
    try:
        agent = wait_for(
            lambda: next((a for a in admin.get("/api/v1/agents").json()["agents"] if a["name"] == name), None)
        )
        run = admin.post(f"/api/v1/agents/{agent['id']}/runs", json={"input": {"n": 0}}).json()["run"]
        wait_for(lambda: admin.get(f"/api/v1/runs/{run['id']}").json()["status"] == "running")
        admin.post(f"/api/v1/runs/{run['id']}/cancel").raise_for_status()
        wait_for(lambda: deck.is_cancelled(run["id"]))
        gate.set()
        final = wait_for(lambda: (r := admin.get(f"/api/v1/runs/{run['id']}").json())["status"] == "cancelled" and r)
        assert final["status"] == "cancelled"
    finally:
        deck.stop()
