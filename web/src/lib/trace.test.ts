import type { RunEvent } from "../api/types";
import { eventLabel, nodeStates } from "./trace";

const ev = (type: string, node?: string): RunEvent => ({ id: Math.random(), run_id: "r", type, node, ts: "2026-01-01T00:00:00Z" });

describe("nodeStates", () => {
  it("marks the running node active and finished ones visited", () => {
    const s = nodeStates([ev("node_start", "plan"), ev("node_end", "plan"), ev("node_start", "act")], "running");
    expect(s).toEqual({ __start__: "visited", plan: "visited", act: "active" });
  });
  it("closes the path when the run succeeds", () => {
    const s = nodeStates([ev("node_start", "plan")], "succeeded");
    expect(s.plan).toBe("visited");
    expect(s.__end__).toBe("visited");
  });
  it("marks the in-flight node as error when the run fails", () => {
    expect(nodeStates([ev("node_start", "act")], "failed").act).toBe("error");
    expect(nodeStates([ev("node_error", "x")], "running").x).toBe("error");
  });
  it("is empty without node events", () => {
    expect(nodeStates([ev("run_start")], "running")).toEqual({});
  });
  it("labels events", () => {
    expect(eventLabel("interrupt")).toBe("Waiting for approval");
    expect(eventLabel("custom")).toBe("custom");
  });
});
