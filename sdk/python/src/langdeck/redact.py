"""Turn arbitrary graph state into safe, bounded JSON before it leaves the process."""

from __future__ import annotations

import re
from typing import Any

DEFAULT_SECRET_KEYS = re.compile(
    r"(pass(word)?|secret|token|api[_-]?key|authorization|auth|cookie|credential|private[_-]?key|session)",
    re.IGNORECASE,
)
REDACTED = "[REDACTED]"
MAX_STRING = 8_000
MAX_ITEMS = 200
MAX_DEPTH = 12


def to_jsonable(value: Any, secret_keys: re.Pattern[str] = DEFAULT_SECRET_KEYS, _depth: int = 0) -> Any:
    """Convert value into JSON-safe data.

    - dict keys that look like secrets are replaced with "[REDACTED]"
    - long strings, long lists and deep nesting are truncated
    - pydantic models, dataclasses and LangChain messages are converted to dicts
    - anything else falls back to repr()
    """
    if _depth > MAX_DEPTH:
        return "…"
    if value is None or isinstance(value, (bool, int, float)):
        return value
    if isinstance(value, str):
        return value if len(value) <= MAX_STRING else value[:MAX_STRING] + f"… (+{len(value) - MAX_STRING} chars)"
    if isinstance(value, bytes):
        return f"<{len(value)} bytes>"
    if isinstance(value, dict):
        out: dict[str, Any] = {}
        for i, (k, v) in enumerate(value.items()):
            if i >= MAX_ITEMS:
                out["…"] = f"{len(value) - MAX_ITEMS} more keys"
                break
            key = str(k)
            out[key] = REDACTED if secret_keys.search(key) else to_jsonable(v, secret_keys, _depth + 1)
        return out
    if isinstance(value, (list, tuple, set, frozenset)):
        items = list(value)
        out_list = [to_jsonable(v, secret_keys, _depth + 1) for v in items[:MAX_ITEMS]]
        if len(items) > MAX_ITEMS:
            out_list.append(f"… {len(items) - MAX_ITEMS} more items")
        return out_list
    for attr in ("model_dump", "dict"):  # pydantic v2 / v1, LangChain messages
        fn = getattr(value, attr, None)
        if callable(fn):
            try:
                return to_jsonable(fn(), secret_keys, _depth + 1)
            except Exception:  # noqa: BLE001 - best effort
                break
    if hasattr(value, "__dataclass_fields__"):
        import dataclasses

        return to_jsonable(dataclasses.asdict(value), secret_keys, _depth + 1)
    return to_jsonable(repr(value), secret_keys, _depth + 1)
