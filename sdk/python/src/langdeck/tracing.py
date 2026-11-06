"""LangChain callback handler that turns LangGraph execution into LangDeck events."""

from __future__ import annotations

import contextvars
import threading
import time
from collections.abc import Callable
from typing import Any
from uuid import UUID

from .redact import to_jsonable
from .transport import now_iso

try:  # LangChain is optional at import time so the SDK stays importable without it.
    from langchain_core.callbacks import BaseCallbackHandler
except ImportError:  # pragma: no cover - exercised only without langchain installed
    BaseCallbackHandler = object  # type: ignore[assignment,misc]

# The LangDeck run and graph node executing in the current context.
current_run: contextvars.ContextVar[str | None] = contextvars.ContextVar("langdeck_run", default=None)
current_node: contextvars.ContextVar[str | None] = contextvars.ContextVar("langdeck_node", default=None)


class RunCancelled(Exception):
    """Raised inside a graph when an operator cancels the run from LangDeck."""


class Tracer(BaseCallbackHandler):  # type: ignore[misc,valid-type]
    """Emits node, LLM and tool events for one LangDeck run.

    raise_error=True lets RunCancelled stop the graph at the next node boundary.
    """

    raise_error = True
    run_inline = True

    def __init__(
        self,
        run_id: str,
        thread_id: str | None,
        emit: Callable[[dict], None],
        is_cancelled: Callable[[str], bool],
        capture: bool = True,
    ):
        self.run_id = run_id
        self.thread_id = thread_id
        self._emit = emit
        self._cancelled = is_cancelled
        self._capture = capture
        self._open: dict[UUID, tuple[str, str, float]] = {}  # lc run id -> (kind, node, start)
        self._lock = threading.Lock()

    # ---- helpers ----

    def event(self, type_: str, node: str | None = None, payload: Any = None, latency_ms: int | None = None) -> None:
        ev: dict[str, Any] = {"run_id": self.run_id, "type": type_, "ts": now_iso()}
        if self.thread_id:
            ev["thread_id"] = self.thread_id
        if node:
            ev["node"] = node
        if payload is not None:
            ev["payload"] = to_jsonable(payload)
        if latency_ms is not None:
            ev["latency_ms"] = latency_ms
        self._emit(ev)

    def _data(self, value: Any) -> Any:
        return value if self._capture else None

    def _begin(self, lc_id: UUID, kind: str, node: str) -> None:
        with self._lock:
            self._open[lc_id] = (kind, node, time.monotonic())

    def _end(self, lc_id: UUID) -> tuple[str, str, int] | None:
        with self._lock:
            item = self._open.pop(lc_id, None)
        if item is None:
            return None
        kind, node, start = item
        return kind, node, int((time.monotonic() - start) * 1000)

    @staticmethod
    def _node_of(metadata: dict | None) -> str:
        return (metadata or {}).get("langgraph_node", "") or ""

    # ---- chains (graph nodes) ----

    def on_chain_start(
        self,
        serialized: dict | None,
        inputs: Any,
        *,
        run_id: UUID,
        parent_run_id: UUID | None = None,
        metadata: dict | None = None,
        **kwargs: Any,
    ) -> None:
        node = self._node_of(metadata)
        name = kwargs.get("name") or (serialized or {}).get("name")
        # LangGraph tags each node's own runnable with its name; nested chains share the metadata.
        if not node or name != node:
            return
        if self._cancelled(self.run_id):
            raise RunCancelled(f"run {self.run_id} was cancelled from LangDeck")
        current_node.set(node)
        self._begin(run_id, "node", node)
        self.event("node_start", node, {"input": self._data(inputs)} if self._capture else None)

    def on_chain_end(self, outputs: Any, *, run_id: UUID, **kwargs: Any) -> None:
        done = self._end(run_id)
        if done and done[0] == "node":
            self.event("node_end", done[1], {"output": self._data(outputs)} if self._capture else None, done[2])

    def on_chain_error(self, error: BaseException, *, run_id: UUID, **kwargs: Any) -> None:
        done = self._end(run_id)
        # GraphInterrupt / ParentCommand are control flow in LangGraph, not failures.
        if (
            done
            and done[0] == "node"
            and type(error).__name__ not in ("GraphInterrupt", "ParentCommand", "GraphBubbleUp")
        ):
            self.event("node_error", done[1], {"error": f"{type(error).__name__}: {error}"}, done[2])

    # ---- LLMs ----

    def on_chat_model_start(
        self, serialized: dict | None, messages: Any, *, run_id: UUID, metadata: dict | None = None, **kwargs: Any
    ) -> None:
        self._llm_start(serialized, messages, run_id, metadata, kwargs)

    def on_llm_start(
        self, serialized: dict | None, prompts: Any, *, run_id: UUID, metadata: dict | None = None, **kwargs: Any
    ) -> None:
        self._llm_start(serialized, prompts, run_id, metadata, kwargs)

    def _llm_start(self, serialized: dict | None, prompt: Any, run_id: UUID, metadata: dict | None, kw: dict) -> None:
        node = self._node_of(metadata)
        model = (metadata or {}).get("ls_model_name") or kw.get("name") or (serialized or {}).get("name") or "llm"
        self._begin(run_id, "llm", node)
        self.event("llm_start", node, {"model": model, "prompt": self._data(prompt)})

    def on_llm_end(self, response: Any, *, run_id: UUID, **kwargs: Any) -> None:
        done = self._end(run_id)
        node, latency = (done[1], done[2]) if done else ("", None)
        self.event("llm_end", node, {"usage": usage_of(response), "output": self._data(text_of(response))}, latency)

    def on_llm_error(self, error: BaseException, *, run_id: UUID, **kwargs: Any) -> None:
        done = self._end(run_id)
        self.event("log", done[1] if done else None, {"level": "error", "message": f"LLM error: {error}"})

    # ---- tools ----

    def on_tool_start(
        self, serialized: dict | None, input_str: str, *, run_id: UUID, metadata: dict | None = None, **kwargs: Any
    ) -> None:
        node = self._node_of(metadata)
        tool = kwargs.get("name") or (serialized or {}).get("name") or "tool"
        self._begin(run_id, "tool", node)
        self.event("tool_start", node, {"tool": tool, "input": self._data(kwargs.get("inputs") or input_str)})

    def on_tool_end(self, output: Any, *, run_id: UUID, **kwargs: Any) -> None:
        done = self._end(run_id)
        self.event("tool_end", done[1] if done else None, {"output": self._data(output)}, done[2] if done else None)

    def on_tool_error(self, error: BaseException, *, run_id: UUID, **kwargs: Any) -> None:
        done = self._end(run_id)
        self.event("log", done[1] if done else None, {"level": "error", "message": f"tool error: {error}"})


def usage_of(response: Any) -> dict[str, int]:
    """Extract token usage from an LLMResult across LangChain versions and providers."""
    prompt = completion = 0
    llm_output = getattr(response, "llm_output", None) or {}
    usage = llm_output.get("token_usage") or llm_output.get("usage") or {}
    if usage:
        prompt = int(usage.get("prompt_tokens") or usage.get("input_tokens") or 0)
        completion = int(usage.get("completion_tokens") or usage.get("output_tokens") or 0)
    if not (prompt or completion):
        for gens in getattr(response, "generations", None) or []:
            for g in gens:
                meta = getattr(getattr(g, "message", None), "usage_metadata", None) or {}
                prompt += int(meta.get("input_tokens", 0))
                completion += int(meta.get("output_tokens", 0))
    return {"prompt_tokens": prompt, "completion_tokens": completion}


def text_of(response: Any) -> Any:
    try:
        return [[getattr(g, "text", None) for g in gens] for gens in response.generations]
    except AttributeError:
        return None
