"""HTTP transport to the LangDeck control plane, plus a non-blocking event buffer."""

from __future__ import annotations

import logging
import threading
import time
from collections import deque
from typing import Any

import httpx

from ._version import __version__

log = logging.getLogger("langdeck")


class LangDeckError(Exception):
    """Raised when the control plane rejects a request."""


class Transport:
    """Thin, thread-safe wrapper over httpx with LangDeck auth and errors."""

    def __init__(self, url: str, api_key: str, *, timeout: float = 10.0, client: httpx.Client | None = None):
        if not api_key.startswith("ldk_"):
            raise ValueError("LangDeck API keys start with 'ldk_'; create one under Settings → API keys")
        self._client = client or httpx.Client(base_url=url.rstrip("/"), timeout=timeout)
        self._client.headers.update(
            {"Authorization": f"Bearer {api_key}", "User-Agent": f"langdeck-python/{__version__}"}
        )

    def request(
        self, method: str, path: str, *, json: Any = None, params: dict | None = None, timeout: float | None = None
    ) -> Any:
        kwargs: dict[str, Any] = {"json": json, "params": params}
        if timeout is not None:
            kwargs["timeout"] = timeout
        resp = self._client.request(method, path, **kwargs)
        if resp.status_code >= 400:
            try:
                msg = resp.json().get("error", resp.text)
            except ValueError:
                msg = resp.text
            raise LangDeckError(f"{method} {path}: {resp.status_code} {msg}")
        if resp.status_code == 204 or not resp.content:
            return None
        return resp.json()

    def close(self) -> None:
        self._client.close()


class EventBuffer:
    """Collects events in memory and ships them in batches from a background thread.

    Graph execution never waits on the network: if LangDeck is unreachable the
    buffer keeps the newest events (up to max_size) and retries with backoff.
    """

    def __init__(
        self,
        transport: Transport,
        agent: str,
        *,
        flush_interval: float = 1.0,
        batch_size: int = 200,
        max_size: int = 10_000,
    ):
        self._t = transport
        self._agent = agent
        self._interval = flush_interval
        self._batch = batch_size
        self._q: deque[dict] = deque(maxlen=max_size)
        self._lock = threading.Lock()
        self._wake = threading.Event()
        self._stop = threading.Event()
        self._thread: threading.Thread | None = None
        self.dropped = 0

    def put(self, event: dict) -> None:
        with self._lock:
            if len(self._q) == self._q.maxlen:
                self.dropped += 1  # deque drops the oldest event
            self._q.append(event)
            if len(self._q) >= self._batch:
                self._wake.set()

    def start(self) -> None:
        if self._thread is None:
            self._thread = threading.Thread(target=self._loop, name="langdeck-events", daemon=True)
            self._thread.start()

    def stop(self, timeout: float = 5.0) -> None:
        self._stop.set()
        self._wake.set()
        if self._thread:
            self._thread.join(timeout)
        self.flush()

    def flush(self) -> bool:
        """Send everything buffered. Returns False if a send failed (events are kept)."""
        while True:
            with self._lock:
                batch = [self._q.popleft() for _ in range(min(self._batch, len(self._q)))]
            if not batch:
                return True
            try:
                self._t.request("POST", "/api/v1/agent/events", json={"agent": self._agent, "events": batch})
            except LangDeckError as e:
                if " 400 " in str(e):  # a malformed batch will never succeed; drop it
                    log.warning("langdeck dropped an invalid event batch: %s", e)
                    continue
                self._requeue(batch)
                log.debug("langdeck event flush failed: %s", e)
                return False
            except httpx.HTTPError as e:
                self._requeue(batch)
                log.debug("langdeck event flush failed: %s", e)
                return False

    def _requeue(self, batch: list[dict]) -> None:
        with self._lock:
            for ev in reversed(batch):
                if len(self._q) == self._q.maxlen:
                    self.dropped += 1
                    break
                self._q.appendleft(ev)

    def _loop(self) -> None:
        backoff = self._interval
        while not self._stop.is_set():
            self._wake.wait(backoff)
            self._wake.clear()
            backoff = self._interval if self.flush() else min(backoff * 2, 30.0)


def now_iso() -> str:
    t = time.time()
    return time.strftime("%Y-%m-%dT%H:%M:%S", time.gmtime(t)) + f".{int((t % 1) * 1e6):06d}Z"
