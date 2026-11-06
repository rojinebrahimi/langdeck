import type { RunEvent, RunStatus } from "../api/types";
import type { NodeState } from "../components/GraphView";

/** nodeStates derives graph highlighting from a run's event trace. */
export function nodeStates(events: RunEvent[], status: RunStatus): Record<string, NodeState> {
  const out: Record<string, NodeState> = {};
  for (const e of events) {
    if (!e.node) continue;
    if (e.type === "node_start") out[e.node] = "active";
    else if (e.type === "node_end") out[e.node] = "visited";
    else if (e.type === "node_error") out[e.node] = "error";
  }
  if (Object.keys(out).length) out["__start__"] = "visited";
  const finished = status === "succeeded" || status === "failed" || status === "cancelled";
  if (finished) {
    for (const k of Object.keys(out)) if (out[k] === "active") out[k] = status === "failed" ? "error" : "visited";
    if (status === "succeeded") out["__end__"] = "visited";
  }
  return out;
}

const labels: Record<string, string> = {
  run_start: "Run started",
  run_end: "Run finished",
  run_error: "Run failed",
  node_start: "Node started",
  node_end: "Node finished",
  node_error: "Node failed",
  llm_start: "LLM call",
  llm_end: "LLM response",
  tool_start: "Tool call",
  tool_end: "Tool result",
  interrupt: "Waiting for approval",
  log: "Log",
};

export const eventLabel = (t: string) => labels[t] ?? t;
