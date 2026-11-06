"""Tracing against a real LangGraph graph (skipped if langgraph is not installed)."""

from typing import TypedDict

import pytest

pytest.importorskip("langgraph")
from langgraph.graph import END, START, StateGraph  # noqa: E402

from langdeck import RunCancelled  # noqa: E402


class State(TypedDict, total=False):
    n: int
    log: list


def build(on_plan=None):
    def plan(s: State) -> dict:
        if on_plan:
            on_plan()
        return {"n": s["n"] + 1, "log": [*s.get("log", []), "plan"]}

    def act(s: State) -> dict:
        return {"n": s["n"] * 10, "log": [*s.get("log", []), "act"]}

    def route(s: State) -> str:
        return "act" if s["n"] < 5 else END

    g = StateGraph(State)
    g.add_node("plan", plan)
    g.add_node("act", act)
    g.add_edge(START, "plan")
    g.add_conditional_edges("plan", route, {"act": "act", END: END})
    g.add_edge("act", END)
    return g.compile()


def test_node_events_and_topology(deck, server):
    app = deck.instrument(build())
    deck.start()
    topo = server.registrations[0]["graph"]
    ids = {n["id"] for n in topo["nodes"]}
    assert {"plan", "act", "__start__", "__end__"} <= ids
    assert any(e["source"] == "plan" and e["conditional"] for e in topo["edges"])

    out = app.invoke({"n": 1}, run_id="lg-1")
    assert out["n"] == 20
    deck._events.flush()
    nodes = [(e["type"], e.get("node")) for e in server.events if e["type"].startswith("node_")]
    assert nodes == [("node_start", "plan"), ("node_end", "plan"), ("node_start", "act"), ("node_end", "act")]
    ends = [e for e in server.events if e["type"] == "node_end"]
    assert all("latency_ms" in e for e in ends)


def test_cancel_stops_at_next_node(deck, server):
    app = deck.instrument(
        build(on_plan=lambda: deck.handle_command({"id": "c", "type": "cancel_run", "payload": {"run_id": "lg-2"}}))
    )
    with pytest.raises(RunCancelled):
        app.invoke({"n": 1}, run_id="lg-2")
    deck._events.flush()
    assert ("node_start", "act") not in [(e["type"], e.get("node")) for e in server.events]
    assert server.events[-1]["type"] == "run_error" and server.events[-1]["payload"]["cancelled"]


async def test_async_invoke(deck, server):
    app = deck.instrument(build())
    out = await app.ainvoke({"n": 7}, run_id="lg-3")
    assert out["n"] == 8
    deck._events.flush()
    assert [e["type"] for e in server.events][0] == "run_start"
    assert server.events[-1]["type"] == "run_end"
