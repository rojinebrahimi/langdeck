import { layout } from "./GraphView";

describe("layout", () => {
  it("places nodes left to right following edges", () => {
    const pos = layout({
      nodes: [{ id: "__start__" }, { id: "plan" }, { id: "act" }, { id: "__end__" }],
      edges: [
        { source: "__start__", target: "plan" },
        { source: "plan", target: "act" },
        { source: "act", target: "__end__" },
        { source: "act", target: "ghost" }, // dangling edges are ignored
      ],
    });
    expect(Object.keys(pos)).toHaveLength(4);
    expect(pos.__start__.x).toBeLessThan(pos.plan.x);
    expect(pos.plan.x).toBeLessThan(pos.act.x);
    expect(pos.act.x).toBeLessThan(pos.__end__.x);
  });
});

describe("layout direction", () => {
  it("stacks nodes vertically for TB", () => {
    const pos = layout({ nodes: [{ id: "a" }, { id: "b" }], edges: [{ source: "a", target: "b" }] }, "TB");
    expect(pos.a.y).toBeLessThan(pos.b.y);
  });
});
