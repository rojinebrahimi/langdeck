"""The LangDeck client: registration, tracing, remote control and human approval."""

from __future__ import annotations

import asyncio
import copy
import logging
import os
import threading
import time
import uuid
from collections import OrderedDict
from collections.abc import Callable
from concurrent.futures import ThreadPoolExecutor
from dataclasses import dataclass
from typing import Any

import httpx

from ._version import __version__
from .redact import to_jsonable
from .tracing import RunCancelled, Tracer, current_node, current_run
from .transport import EventBuffer, LangDeckError, Transport, now_iso

log = logging.getLogger("langdeck")


ACCESS_MODES = ("read", "write", "execute", "delete", "admin")


def normalize_accesses(accesses: Any) -> list[dict] | None:
    """Turn the ``accesses=`` argument into the list the API expects.

    Accepts a mapping ``{"postgres:orders": ["read", "write"], "s3:reports": "read"}``
    (a value may also be ``{"modes": [...], "description": "..."}``) or a list of
    ``{"resource": ..., "modes": [...], "description": ...}`` dicts. ``None`` means "do not report".
    """
    if accesses is None:
        return None
    if isinstance(accesses, dict):
        items = [{"resource": res, **(v if isinstance(v, dict) else {"modes": v})} for res, v in accesses.items()]
    else:
        items = [dict(a) for a in accesses]
    out = []
    for item in items:
        modes = item.get("modes") or []
        modes = [modes] if isinstance(modes, str) else list(modes)
        bad = [m for m in modes if m not in ACCESS_MODES]
        if not item.get("resource") or not modes or bad:
            raise ValueError(
                f"access {item.get('resource')!r} needs a resource and modes from {ACCESS_MODES}"
                + (f" (got {bad})" if bad else "")
            )
        entry = {"resource": item["resource"], "modes": modes}
        if item.get("description"):
            entry["description"] = item["description"]
        out.append(entry)
    return out


class AgentPaused(Exception):
    """Raised when a run is started while operators have paused the agent."""


@dataclass(frozen=True)
class Decision:
    """The outcome of a human approval request."""

    approved: bool
    status: str  # "approved" | "rejected" | "timeout"
    response: Any = None
    resolved_by: str | None = None

    def __bool__(self) -> bool:
        return self.approved


def graph_topology(graph: Any) -> dict | None:
    """Export a compiled LangGraph's nodes and edges as {"nodes": [...], "edges": [...]}."""
    try:
        g = graph.get_graph()
    except Exception:  # noqa: BLE001 - not a LangGraph, or drawing failed
        return None
    nodes = [{"id": str(n)} for n in g.nodes]
    edges = [
        {"source": str(e.source), "target": str(e.target), "conditional": bool(getattr(e, "conditional", False))}
        for e in g.edges
    ]
    return {"nodes": nodes, "edges": edges}


class LangDeck:
    """Connects one agent process to LangDeck.

    Typical use::

        deck = LangDeck(name="support-bot")          # reads LANGDECK_URL / LANGDECK_API_KEY
        app = deck.instrument(builder.compile())     # traces every invoke
        deck.start()                                 # register + listen for commands

        app.invoke({"messages": [...]})              # shows up live in the UI
    """

    def __init__(
        self,
        name: str,
        *,
        url: str | None = None,
        api_key: str | None = None,
        description: str = "",
        version: str = "",
        labels: dict[str, str] | None = None,
        accesses: dict[str, Any] | list[dict] | None = None,
        depends_on: list[str] | None = None,
        capture_payloads: bool = True,
        max_concurrent_runs: int = 4,
        heartbeat_interval: float = 15.0,
        poll_wait: float = 25.0,
        flush_interval: float = 1.0,
        timeout: float = 10.0,
        http_client: httpx.Client | None = None,
    ):
        url = url or os.environ.get("LANGDECK_URL", "http://localhost:8080")
        api_key = api_key or os.environ.get("LANGDECK_API_KEY", "")
        self.name = name
        self.description = description
        self.version = version or os.environ.get("LANGDECK_AGENT_VERSION", "")
        self.labels = labels or {}
        self.accesses = normalize_accesses(accesses)
        self.depends_on = list(depends_on) if depends_on is not None else None
        self.capture_payloads = capture_payloads
        self._t = Transport(url, api_key, timeout=timeout, client=http_client)
        self._events = EventBuffer(self._t, name, flush_interval=flush_interval)
        self._pool = ThreadPoolExecutor(max_workers=max_concurrent_runs, thread_name_prefix="langdeck-run")
        self._hb_interval = heartbeat_interval
        self._poll_wait = poll_wait

        self._graph: Any = None
        self._input_mapper: Callable[[Any], Any] | None = None
        self._config: dict[str, Any] = {}
        self._config_version = 0
        self._paused = False
        self._cancelled: set[str] = set()
        self._seen_commands: OrderedDict[str, None] = OrderedDict()
        self._config_listeners: list[Callable[[dict], None]] = []
        self._run_handler: Callable[[Any, str, str | None], Any] | None = None
        self._lock = threading.RLock()
        self._stop = threading.Event()
        self._threads: list[threading.Thread] = []
        self._started = False
        self._registered = False

    # ------------------------------------------------------------------ setup

    def instrument(self, graph: Any, *, input_mapper: Callable[[Any], Any] | None = None) -> InstrumentedGraph:
        """Wrap a compiled graph so every run is traced and remotely controllable.

        input_mapper turns the JSON input typed in the UI into your graph's state.
        """
        self._graph = graph
        self._input_mapper = input_mapper
        return InstrumentedGraph(self, graph)

    def on_run(self, fn: Callable[[Any, str, str | None], Any]) -> Callable[[Any, str, str | None], Any]:
        """Decorator: handle runs started from the UI yourself, as fn(input, run_id, thread_id)."""
        self._run_handler = fn
        return fn

    def on_config(self, fn: Callable[[dict], None]) -> Callable[[dict], None]:
        """Decorator: called with the new config whenever an operator pushes one."""
        self._config_listeners.append(fn)
        return fn

    def start(self) -> LangDeck:
        """Register with LangDeck and start the background threads. Safe to call twice."""
        if self._started:
            return self
        self._started = True
        self._stop.clear()
        try:
            self._register()
        except (LangDeckError, httpx.HTTPError) as e:
            # The agent keeps working without the control plane and retries in the background.
            log.warning("langdeck: registration failed, will retry: %s", e)
        self._events.start()
        for target, name in ((self._heartbeat_loop, "langdeck-heartbeat"), (self._command_loop, "langdeck-commands")):
            t = threading.Thread(target=target, name=name, daemon=True)
            t.start()
            self._threads.append(t)
        return self

    def stop(self, timeout: float = 5.0) -> None:
        """Flush pending events and stop background work."""
        self._stop.set()
        self._events.stop(timeout)
        self._pool.shutdown(wait=False, cancel_futures=True)
        self._t.close()
        self._started = False

    def __enter__(self) -> LangDeck:
        return self.start()

    def __exit__(self, *exc: object) -> None:
        self.stop()

    # ------------------------------------------------------------------ state

    @property
    def config(self) -> dict[str, Any]:
        """The latest runtime configuration pushed from LangDeck (a copy)."""
        with self._lock:
            return copy.deepcopy(self._config)

    @property
    def config_version(self) -> int:
        return self._config_version

    @property
    def paused(self) -> bool:
        return self._paused

    def is_cancelled(self, run_id: str) -> bool:
        with self._lock:
            return run_id in self._cancelled

    # ------------------------------------------------------------------ runs

    def tracer(self, run_id: str, thread_id: str | None = None) -> Tracer:
        return Tracer(run_id, thread_id, self._events.put, self.is_cancelled, self.capture_payloads)

    def _run(
        self, call: Callable[[dict], Any], input: Any, run_id: str | None, thread_id: str | None, config: dict | None
    ) -> Any:
        if self._paused:
            raise AgentPaused(f"agent {self.name} is paused in LangDeck")
        run_id = run_id or str(uuid.uuid4())
        tracer = self.tracer(run_id, thread_id)
        cfg = dict(config or {})
        cfg["callbacks"] = [*(cfg.get("callbacks") or []), tracer]
        if thread_id:
            cfg.setdefault("configurable", {}).setdefault("thread_id", thread_id)

        token = current_run.set(run_id)
        tracer.event("run_start", payload={"input": input if self.capture_payloads else None})
        try:
            out = call(cfg)
        except RunCancelled as e:
            tracer.event("run_error", payload={"error": str(e), "cancelled": True})
            raise
        except BaseException as e:
            tracer.event("run_error", payload={"error": f"{type(e).__name__}: {e}"})
            raise
        else:
            tracer.event("run_end", payload={"output": out if self.capture_payloads else None})
            return out
        finally:
            current_run.reset(token)
            with self._lock:
                self._cancelled.discard(run_id)

    async def _arun(
        self, call: Callable[[dict], Any], input: Any, run_id: str | None, thread_id: str | None, config: dict | None
    ) -> Any:
        if self._paused:
            raise AgentPaused(f"agent {self.name} is paused in LangDeck")
        run_id = run_id or str(uuid.uuid4())
        tracer = self.tracer(run_id, thread_id)
        cfg = dict(config or {})
        cfg["callbacks"] = [*(cfg.get("callbacks") or []), tracer]
        if thread_id:
            cfg.setdefault("configurable", {}).setdefault("thread_id", thread_id)
        token = current_run.set(run_id)
        tracer.event("run_start", payload={"input": input if self.capture_payloads else None})
        try:
            out = await call(cfg)
        except RunCancelled as e:
            tracer.event("run_error", payload={"error": str(e), "cancelled": True})
            raise
        except BaseException as e:
            tracer.event("run_error", payload={"error": f"{type(e).__name__}: {e}"})
            raise
        else:
            tracer.event("run_end", payload={"output": out if self.capture_payloads else None})
            return out
        finally:
            current_run.reset(token)
            with self._lock:
                self._cancelled.discard(run_id)

    # ------------------------------------------------------------------ human in the loop

    def request_approval(
        self,
        question: str,
        payload: Any = None,
        *,
        run_id: str | None = None,
        node: str | None = None,
        timeout: float | None = 3600.0,
    ) -> Decision:
        """Pause the current run until an operator approves or rejects it in LangDeck.

        Call this inside a graph node. It blocks the node (not the process).
        Returns a Decision; a timeout counts as rejected.
        """
        run_id = run_id or current_run.get()
        if not run_id:
            raise RuntimeError("request_approval needs a run: call it inside an instrumented graph or pass run_id")
        node = node or current_node.get() or ""
        interrupt_id = f"int-{uuid.uuid4().hex}"
        self._events.flush()  # make sure the trace up to this point is visible next to the question
        self._t.request(
            "POST",
            "/api/v1/agent/interrupts",
            json={
                "agent": self.name,
                "id": interrupt_id,
                "run_id": run_id,
                "node": node,
                "question": question,
                "payload": to_jsonable(payload) if payload is not None else None,
            },
        )
        deadline = None if timeout is None else time.monotonic() + timeout
        while True:
            if self.is_cancelled(run_id):
                raise RunCancelled(f"run {run_id} was cancelled while waiting for approval")
            remaining = None if deadline is None else deadline - time.monotonic()
            if remaining is not None and remaining <= 0:
                return Decision(False, "timeout")
            wait = min(self._poll_wait, remaining) if remaining is not None else self._poll_wait
            try:
                res = self._t.request(
                    "GET",
                    f"/api/v1/agent/interrupts/{interrupt_id}",
                    params={"wait": f"{max(int(wait), 0)}s"},
                    timeout=wait + 10,
                )
            except (LangDeckError, httpx.HTTPError) as e:
                log.warning("langdeck: waiting for approval failed, retrying: %s", e)
                time.sleep(2)
                continue
            if res["status"] != "pending":
                return Decision(res["status"] == "approved", res["status"], res.get("response"), res.get("resolved_by"))

    async def arequest_approval(self, question: str, payload: Any = None, **kwargs: Any) -> Decision:
        kwargs.setdefault("run_id", current_run.get())
        kwargs.setdefault("node", current_node.get())
        return await asyncio.to_thread(self.request_approval, question, payload, **kwargs)

    def log(self, message: str, *, level: str = "info", **fields: Any) -> None:
        """Attach a log line to the current run's trace."""
        run_id = current_run.get()
        if run_id:
            self._events.put(
                {
                    "run_id": run_id,
                    "type": "log",
                    "node": current_node.get() or "",
                    "ts": now_iso(),
                    "payload": to_jsonable({"level": level, "message": message, **fields}),
                }
            )

    # ------------------------------------------------------------------ control plane

    def _register(self) -> None:
        body: dict[str, Any] = {
            "name": self.name,
            "description": self.description,
            "version": self.version,
            "framework": "langgraph",
            "sdk_version": __version__,
            "labels": self.labels,
            "graph": graph_topology(self._graph) if self._graph is not None else None,
        }
        # Left out when not declared, so the server keeps what it already has.
        if self.accesses is not None:
            body["accesses"] = self.accesses
        if self.depends_on is not None:
            body["depends_on"] = self.depends_on
        res = self._t.request("POST", "/api/v1/agent/register", json=body)
        self._registered = True
        self._paused = res.get("desired_state") == "paused"
        if res.get("config") is not None:
            self._apply_config(res["config"], int(res.get("config_version") or 0))

    def _apply_config(self, cfg: dict, version: int) -> None:
        with self._lock:
            if version and version <= self._config_version:
                return
            self._config, self._config_version = dict(cfg or {}), version
        for fn in self._config_listeners:
            try:
                fn(self.config)
            except Exception:  # noqa: BLE001 - a listener bug must not break the agent
                log.exception("langdeck: config listener failed")

    def _heartbeat_loop(self) -> None:
        while not self._stop.wait(self._hb_interval):
            try:
                if not self._registered:
                    self._register()
                res = self._t.request("POST", "/api/v1/agent/heartbeat", json={"agent": self.name, "instances": 1})
                self._paused = res.get("desired_state") == "paused"
                if int(res.get("config_version") or 0) > self._config_version:
                    self._register()  # re-registering returns the latest config
            except LangDeckError as e:
                if " 404 " in str(e):
                    self._registered = False  # agent was deleted; register again
                log.debug("langdeck heartbeat failed: %s", e)
            except httpx.HTTPError as e:
                log.debug("langdeck heartbeat failed: %s", e)

    def _command_loop(self) -> None:
        backoff = 1.0
        while not self._stop.is_set():
            try:
                res = self._t.request(
                    "GET",
                    "/api/v1/agent/commands",
                    params={"agent": self.name, "wait": f"{int(self._poll_wait)}s"},
                    timeout=self._poll_wait + 10,
                )
                backoff = 1.0
            except (LangDeckError, httpx.HTTPError) as e:
                log.debug("langdeck command poll failed: %s", e)
                self._stop.wait(backoff)
                backoff = min(backoff * 2, 30.0)
                continue
            for cmd in res.get("commands") or []:
                self.handle_command(cmd)

    def handle_command(self, cmd: dict) -> None:
        """Apply one command from LangDeck. Duplicate deliveries are ignored."""
        cid, ctype, payload = cmd["id"], cmd["type"], cmd.get("payload") or {}
        with self._lock:
            if cid in self._seen_commands:
                return
            self._seen_commands[cid] = None
            while len(self._seen_commands) > 1000:
                self._seen_commands.popitem(last=False)
        ok, result = True, ""
        try:
            if ctype == "pause":
                self._paused = True
            elif ctype == "resume":
                self._paused = False
            elif ctype == "update_config":
                self._apply_config(payload.get("config") or {}, int(payload.get("version") or 0))
                result = f"config v{payload.get('version')} applied"
            elif ctype == "cancel_run":
                with self._lock:
                    self._cancelled.add(payload.get("run_id") or cmd.get("run_id"))
            elif ctype == "start_run":
                ok, result = self._start_remote_run(payload)
            else:
                ok, result = False, f"unsupported command {ctype}"
        except Exception as e:  # noqa: BLE001
            ok, result = False, f"{type(e).__name__}: {e}"
        try:
            self._t.request(
                "POST",
                f"/api/v1/agent/commands/{cid}/ack",
                json={"agent": self.name, "ok": ok, "result": result[:2000]},
            )
        except (LangDeckError, httpx.HTTPError) as e:
            log.debug("langdeck ack failed: %s", e)

    def _start_remote_run(self, payload: dict) -> tuple[bool, str]:
        run_id, thread_id, data = payload["run_id"], payload.get("thread_id") or None, payload.get("input")
        if self._paused:
            return False, "agent is paused"
        if self.is_cancelled(run_id):
            return False, "run was cancelled before it started"
        if self._run_handler is not None:
            handler = self._run_handler
            self._pool.submit(
                self._safe,
                lambda: self._run(lambda _cfg: handler(data, run_id, thread_id), data, run_id, thread_id, None),
            )
            return True, "started"
        if self._graph is None:
            return False, "no graph instrumented and no @deck.on_run handler"
        state = self._input_mapper(data) if self._input_mapper else data
        graph = self._graph
        self._pool.submit(
            self._safe, lambda: self._run(lambda cfg: graph.invoke(state, cfg), data, run_id, thread_id, None)
        )
        return True, "started"

    @staticmethod
    def _safe(fn: Callable[[], Any]) -> None:
        try:
            fn()
        except RunCancelled:
            log.info("langdeck: run cancelled by operator")
        except Exception:  # noqa: BLE001 - already reported as run_error
            log.exception("langdeck: remote run failed")


class InstrumentedGraph:
    """A compiled graph whose invoke/ainvoke are traced by LangDeck. Other attributes pass through."""

    def __init__(self, deck: LangDeck, graph: Any):
        self._deck = deck
        self._graph = graph

    def invoke(
        self,
        input: Any,
        config: dict | None = None,
        *,
        run_id: str | None = None,
        thread_id: str | None = None,
        **kwargs: Any,
    ) -> Any:
        return self._deck._run(lambda cfg: self._graph.invoke(input, cfg, **kwargs), input, run_id, thread_id, config)

    async def ainvoke(
        self,
        input: Any,
        config: dict | None = None,
        *,
        run_id: str | None = None,
        thread_id: str | None = None,
        **kwargs: Any,
    ) -> Any:
        return await self._deck._arun(
            lambda cfg: self._graph.ainvoke(input, cfg, **kwargs), input, run_id, thread_id, config
        )

    def __getattr__(self, item: str) -> Any:
        return getattr(self._graph, item)
