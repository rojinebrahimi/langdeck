from dataclasses import dataclass

from langdeck.redact import MAX_ITEMS, MAX_STRING, REDACTED, to_jsonable


def test_secrets_are_redacted_recursively():
    out = to_jsonable({"api_key": "sk-1", "nested": {"Password": "p", "ok": 1}, "list": [{"auth_token": "t"}]})
    assert out == {"api_key": REDACTED, "nested": {"Password": REDACTED, "ok": 1}, "list": [{"auth_token": REDACTED}]}


def test_truncation_bounds_payloads():
    out = to_jsonable({"s": "a" * (MAX_STRING + 10), "l": list(range(MAX_ITEMS + 5))})
    assert out["s"].endswith("(+10 chars)")
    assert len(out["l"]) == MAX_ITEMS + 1


def test_objects_become_json():
    @dataclass
    class Point:
        x: int

    class Model:
        def model_dump(self):
            return {"token": "secret", "v": 2}

    assert to_jsonable(Point(1)) == {"x": 1}
    assert to_jsonable(Model()) == {"token": REDACTED, "v": 2}
    assert to_jsonable(b"abc") == "<3 bytes>"
    assert isinstance(to_jsonable(object()), str)
