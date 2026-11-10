# langdeck — Python SDK

Connect any [LangGraph](https://github.com/langchain-ai/langgraph) agent to
[LangDeck](https://github.com/rojinebrahimi/langdeck), the open-source control plane for agents.

```bash
pip install langdeck
export LANGDECK_URL=https://langdeck.example.com
export LANGDECK_API_KEY=ldk_...        # Settings → API keys → role "agent"
```

## Three lines

```python
from langdeck import LangDeck

deck = LangDeck(name="support-bot", version="1.4.0", labels={"team": "cx"})
app = deck.instrument(builder.compile())   # your compiled StateGraph
deck.start()                               # register, heartbeat, listen for commands

app.invoke({"messages": [("user", "hi")]})   # traced live in LangDeck
```

`instrument()` returns a drop-in wrapper: `invoke` and `ainvoke` are traced, everything
else passes straight through to your graph. Works with LangGraph 0.2+ and Python 3.10+.

## What you get

| Feature | How |
|---|---|
| Live traces | Node, LLM and tool events with latency and token usage, via LangChain callbacks. |
| Graph topology | Exported from `graph.get_graph()` at registration and drawn in the UI. |
| Start runs from the UI | Pass `input_mapper=` to turn the UI's JSON into your state, or handle runs yourself with `@deck.on_run`. |
| Cancel | Operators cancel; your graph raises `RunCancelled` at the next node boundary. |
| Pause / resume | While paused, `invoke` raises `AgentPaused` and UI runs are refused. Check `deck.paused` in loops. |
| Runtime config | `deck.config` always holds the latest config pushed from the UI; `@deck.on_config` notifies you. |
| Human approval | `deck.request_approval("Send email?", payload)` blocks the node until someone decides. |
| Logs | `deck.log("message", key=value)` attaches a line to the current run's trace. |

## Examples

### Runtime configuration

```python
def call_model(state):
    cfg = deck.config                       # e.g. {"model": "gpt-4o", "temperature": 0.2}
    llm = ChatOpenAI(model=cfg.get("model", "gpt-4o-mini"), temperature=cfg.get("temperature", 0))
    return {"messages": [llm.invoke(state["messages"])]}

@deck.on_config
def reload(cfg):
    print("new config", cfg)
```

### Human in the loop

```python
def send_refund(state):
    decision = deck.request_approval(
        "Refund this order?",
        {"order": state["order_id"], "amount": state["amount"]},
        timeout=900,                        # seconds; a timeout counts as "rejected"
    )
    if not decision:
        return {"status": "refund rejected", "note": decision.response}
    ...
```

### Runs started from the UI

```python
app = deck.instrument(graph, input_mapper=lambda inp: initial_state(question=inp["question"]))

# or take full control:
@deck.on_run
def handle(input, run_id, thread_id):
    return my_pipeline(input)               # traced as run `run_id`
```

### Existing run ids and threads

```python
app.invoke(state, run_id=request_id, thread_id=conversation_id)
```

## Safety

- **Never blocks your agent.** Events are buffered and shipped in the background; if
  LangDeck is unreachable, runs continue and the buffer retries with backoff.
- **Redaction.** Keys that look like secrets (`password`, `token`, `api_key`,
  `authorization`, ...) are replaced with `[REDACTED]`; long values are truncated.
  Use `LangDeck(..., capture_payloads=False)` to send no inputs or outputs at all.
- **Outbound only.** The SDK never opens a port; it long-polls LangDeck over HTTPS.

## Options

| Argument | Default | |
|---|---|---|
| `name` | required | Agent name (lowercase, digits, `-`, `_`, `.`) |
| `url`, `api_key` | `LANGDECK_URL`, `LANGDECK_API_KEY` | Where and how to connect |
| `description`, `version`, `labels` | | Shown in the UI |
| `accesses` | | Resources the agent touches, e.g. `{"postgres:orders": ["read", "write"], "s3:reports": "read"}`. Modes: `read`, `write`, `execute`, `delete`, `admin`. Shown on the agent's Access tab; self-reported, not enforced |
| `depends_on` | | Names of other agents this one calls or hands off to, e.g. `["billing"]`. Drawn on the Dependencies page |
| `capture_payloads` | `True` | Send inputs/outputs in traces |
| `max_concurrent_runs` | `4` | Worker threads for runs started from the UI |
| `heartbeat_interval` | `15` | Seconds |
| `poll_wait` | `25` | Long-poll duration in seconds |
