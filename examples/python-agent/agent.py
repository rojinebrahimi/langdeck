"""A demo LangGraph agent wired to LangDeck.

It runs fully offline (no LLM key needed): "thinking" is simulated so you can
see the control plane end to end — live traces, runtime config, pause/resume,
cancel and human approval.

    LANGDECK_URL=http://localhost:8080 LANGDECK_API_KEY=ldk_... python agent.py
"""

from __future__ import annotations

import logging
import os
import random
import signal
import threading
import time
from typing import TypedDict

from langgraph.graph import END, START, StateGraph

from langdeck import LangDeck

logging.basicConfig(
    level=logging.INFO, format="%(asctime)s %(levelname)s %(name)s: %(message)s"
)
logging.getLogger("httpx").setLevel(logging.WARNING)
log = logging.getLogger("demo-agent")

deck = LangDeck(
    name=os.environ.get("AGENT_NAME", "research-assistant"),
    description="Plans a research task, gathers sources, asks a human before publishing, then answers.",
    version="1.2.0",
    labels={"team": "demo", "env": os.environ.get("AGENT_ENV", "local")},
)


class State(TypedDict, total=False):
    question: str
    background: bool
    plan: list[str]
    sources: list[dict]
    draft: str
    approved: bool
    answer: str


def think(seconds: float) -> None:
    time.sleep(seconds * random.uniform(0.6, 1.4))


def plan(state: State) -> dict:
    cfg = deck.config
    steps = int(cfg.get("max_steps", 3))
    think(0.6)
    deck.log("planning", model=cfg.get("model", "demo-model"), steps=steps)
    return {
        "plan": [f"step {i + 1}: look into '{state['question']}'" for i in range(steps)]
    }


def research(state: State) -> dict:
    sources = []
    for step in state["plan"]:
        think(0.4)
        sources.append(
            {"title": f"Source for {step}", "score": round(random.random(), 2)}
        )
    if deck.config.get("fail_rate", 0.08) > random.random():
        raise RuntimeError("search backend timed out")
    return {"sources": sources}


def draft(state: State) -> dict:
    think(0.5)
    tone = deck.config.get("tone", "neutral")
    best = max(state["sources"], key=lambda s: s["score"])
    return {
        "draft": f"[{tone}] Based on {len(state['sources'])} sources (best: {best['title']}), the answer is 42."
    }


def review(state: State) -> dict:
    if not deck.config.get("require_approval", True):
        return {"approved": True}
    # Background demo traffic only asks a human sometimes, so the inbox stays readable.
    if state.get("background") and random.random() > 0.3:
        return {"approved": True}
    decision = deck.request_approval(
        "Publish this answer?",
        {"question": state["question"], "draft": state["draft"]},
        timeout=float(deck.config.get("approval_timeout_s", 600)),
    )
    return {"approved": decision.approved}


def publish(state: State) -> dict:
    return {
        "answer": state["draft"]
        if state.get("approved")
        else "Draft was rejected by a reviewer."
    }


def route(state: State) -> str:
    return "publish"


builder = StateGraph(State)
for name, fn in [
    ("plan", plan),
    ("research", research),
    ("draft", draft),
    ("review", review),
    ("publish", publish),
]:
    builder.add_node(name, fn)
builder.add_edge(START, "plan")
builder.add_edge("plan", "research")
builder.add_edge("research", "draft")
builder.add_edge("draft", "review")
builder.add_conditional_edges("review", route, {"publish": "publish"})
builder.add_edge("publish", END)

# The UI sends free-form JSON; map it to this graph's state.
app = deck.instrument(
    builder.compile(),
    input_mapper=lambda inp: {
        "question": (inp or {}).get("question", "What is the meaning of life?")
    },
)


def background_run(question: str) -> None:
    try:
        app.invoke({"question": question, "background": True})
    except Exception as e:  # noqa: BLE001 - failures are visible in LangDeck
        log.warning("run failed: %s", e)


def main() -> None:
    deck.start()
    log.info("connected to LangDeck as %s; start runs from the UI", deck.name)
    stop = False

    def on_signal(*_):
        nonlocal stop
        stop = True

    signal.signal(signal.SIGTERM, on_signal)
    signal.signal(signal.SIGINT, on_signal)

    # Optionally generate background traffic so the dashboard has something to show.
    every = float(os.environ.get("DEMO_TRAFFIC_EVERY_S", "0"))
    last = 0.0
    while not stop:
        if every and time.monotonic() - last > every and not deck.paused:
            last = time.monotonic()
            question = random.choice(
                [
                    "Why is the sky blue?",
                    "How do tides work?",
                    "What is RAG?",
                    "Explain vector search",
                ]
            )
            threading.Thread(
                target=background_run, args=(question,), daemon=True
            ).start()
        time.sleep(0.5)
    deck.stop()


if __name__ == "__main__":
    main()
