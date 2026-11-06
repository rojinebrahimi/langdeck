import time

import pytest

from langdeck import AgentPaused, LangDeck, RunCancelled
from langdeck.tracing import current_run

from .conftest import KEY


def wait_for(cond, timeout=3.0):
    end = time.monotonic() + timeout
    while time.monotonic() < end:
        if cond():
            return True
        time.sleep(0.02)
    return False


def test_rejects_bad_api_key():
    with pytest.raises(ValueError):
        LangDeck("x", api_key="nope")


def test_registers_with_config(deck, server):
    server.config, server.config_version = {"model": "m1"}, 3
    seen = []
    deck.on_config(seen.append)
    deck.start()
    assert server.registrations[0]["name"] == "test-agent"
    assert deck.config == {"model": "m1"} and deck.config_version == 3
    assert seen == [{"model": "m1"}]


class FakeGraph:
    """Behaves like a compiled graph: invoke(input, config) and get_graph()."""

    def __init__(self, fn=None):
        self.fn = fn or (lambda x: {"answer": x["q"] * 2})
        self.configs = []

    def invoke(self, input, config=None, **kw):
        self.configs.append(config)
        return self.fn(input)

    def get_graph(self):
        class E:
            def __init__(self, s, t):
                self.source, self.target, self.conditional = s, t, False

        class G:
            nodes = {"__start__": 1, "work": 1, "__end__": 1}
            edges = [E("__start__", "work"), E("work", "__end__")]

        return G()


def test_instrumented_invoke_emits_run_events(deck, server):
    g = FakeGraph()
    app = deck.instrument(g)
    deck.start()
    assert server.registrations[0]["graph"]["nodes"][1] == {"id": "work"}
    assert app.invoke({"q": 2}, run_id="r-1", thread_id="t-1") == {"answer": 4}
    deck._events.flush()
    assert server.types() == ["run_start", "run_end"]
    assert server.events[0]["thread_id"] == "t-1"
    assert server.events[1]["payload"]["output"] == {"answer": 4}
    cfg = g.configs[0]
    assert cfg["configurable"]["thread_id"] == "t-1" and len(cfg["callbacks"]) == 1


def test_failed_run_is_reported(deck, server):
    def boom(_):
        raise RuntimeError("nope")

    app = deck.instrument(FakeGraph(boom))
    with pytest.raises(RuntimeError):
        app.invoke({}, run_id="r-2")
    deck._events.flush()
    assert server.events[-1]["type"] == "run_error"
    assert "RuntimeError: nope" in server.events[-1]["payload"]["error"]


def test_capture_payloads_off(server):
    import httpx

    client = httpx.Client(base_url="http://deck.test", transport=httpx.MockTransport(server.handler))
    d = LangDeck("quiet", api_key=KEY, http_client=client, capture_payloads=False)
    d.instrument(FakeGraph()).invoke({"q": 1, "ssn": "123"}, run_id="r")
    d._events.flush()
    assert server.events[0]["payload"] == {"input": None}
    d.stop()


def test_commands_pause_resume_config_and_dedupe(deck, server):
    deck.handle_command({"id": "c1", "type": "pause"})
    assert deck.paused
    with pytest.raises(AgentPaused):
        deck.instrument(FakeGraph()).invoke({"q": 1})
    deck.handle_command({"id": "c1", "type": "resume"})  # duplicate id: ignored
    assert deck.paused
    deck.handle_command({"id": "c2", "type": "resume"})
    assert not deck.paused
    deck.handle_command({"id": "c3", "type": "update_config", "payload": {"version": 2, "config": {"t": 0.5}}})
    deck.handle_command({"id": "c4", "type": "update_config", "payload": {"version": 1, "config": {"t": 9}}})
    assert deck.config == {"t": 0.5}, "older config versions must not win"
    deck.handle_command({"id": "c5", "type": "explode"})
    assert server.acks["c5"]["ok"] is False
    assert server.acks["c3"]["ok"] is True


def test_remote_start_run_uses_input_mapper(deck, server):
    g = FakeGraph()
    deck.instrument(g, input_mapper=lambda inp: {"q": inp["n"] + 1})
    deck.handle_command({"id": "c1", "type": "start_run", "payload": {"run_id": "remote-1", "input": {"n": 1}}})
    assert wait_for(lambda: deck._events.flush() and "run_end" in server.types())
    end = [e for e in server.events if e["type"] == "run_end"][0]
    assert end["run_id"] == "remote-1" and end["payload"]["output"] == {"answer": 4}
    assert server.acks["c1"] == {"agent": "test-agent", "ok": True, "result": "started"}


def test_remote_start_run_with_custom_handler(deck, server):
    calls = []

    @deck.on_run
    def handle(inp, run_id, thread_id):
        calls.append((inp, run_id, thread_id, current_run.get()))
        return {"done": True}

    deck.handle_command(
        {"id": "c1", "type": "start_run", "payload": {"run_id": "r9", "input": {"a": 1}, "thread_id": "t"}}
    )
    assert wait_for(lambda: calls)
    assert calls[0] == ({"a": 1}, "r9", "t", "r9")


def test_start_run_rejected_without_graph(deck, server):
    deck.handle_command({"id": "c1", "type": "start_run", "payload": {"run_id": "r", "input": {}}})
    assert server.acks["c1"]["ok"] is False


def test_request_approval_round_trip(deck, server):
    decision = {}

    def node(_):
        decision["d"] = deck.request_approval("Ship it?", {"diff": "+1"}, node="review", timeout=5)
        return {"ok": True}

    deck.instrument(FakeGraph(node))
    import threading

    t = threading.Thread(target=lambda: deck.instrument(FakeGraph(node)).invoke({}, run_id="r-hitl"))
    t.start()
    assert wait_for(lambda: server.interrupts)
    ((iid, req),) = server.interrupts.items()
    assert req["question"] == "Ship it?" and req["run_id"] == "r-hitl" and req["node"] == "review"
    with server.lock:
        server.interrupts[iid].update(status="approved", response={"note": "go"}, resolved_by="user:a")
    t.join(5)
    d = decision["d"]
    assert d.approved and bool(d) and d.response == {"note": "go"} and d.resolved_by == "user:a"


def test_request_approval_timeout(deck, server):
    def node(_):
        return deck.request_approval("?", timeout=0.1)

    out = deck.instrument(FakeGraph(node)).invoke({}, run_id="r-t")
    assert out.status == "timeout" and not out


def test_request_approval_outside_run():
    import httpx

    d = LangDeck(
        "x", api_key=KEY, http_client=httpx.Client(transport=httpx.MockTransport(lambda r: httpx.Response(200)))
    )
    with pytest.raises(RuntimeError):
        d.request_approval("?")


def test_cancel_while_waiting_for_approval(deck, server):
    def node(_):
        return deck.request_approval("?", timeout=5)

    import threading

    err = {}

    def run():
        try:
            deck.instrument(FakeGraph(node)).invoke({}, run_id="r-c")
        except RunCancelled as e:
            err["e"] = e

    t = threading.Thread(target=run)
    t.start()
    assert wait_for(lambda: server.interrupts)
    deck.handle_command({"id": "cx", "type": "cancel_run", "payload": {"run_id": "r-c"}})
    t.join(5)
    assert "e" in err
    deck._events.flush()
    assert server.events[-1]["payload"]["cancelled"] is True


def test_event_buffer_survives_outage(server):
    import httpx

    down = {"v": True}

    def flaky(req):
        if down["v"]:
            raise httpx.ConnectError("down")
        return server.handler(req)

    d = LangDeck("x", api_key=KEY, http_client=httpx.Client(base_url="http://d", transport=httpx.MockTransport(flaky)))
    d.instrument(FakeGraph()).invoke({"q": 1}, run_id="r")
    assert d._events.flush() is False
    down["v"] = False
    assert d._events.flush() is True
    assert server.types() == ["run_start", "run_end"]
    d.stop()
