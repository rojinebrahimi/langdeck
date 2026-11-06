import { useMemo } from "react";
import { Background, Controls, MarkerType, Position, ReactFlow, type Edge, type Node } from "@xyflow/react";
import dagre from "@dagrejs/dagre";
import "@xyflow/react/dist/style.css";
import type { AgentGraph } from "../api/types";

const W = 132;
const H = 40;

export type Direction = "LR" | "TB";

export type NodeState = "idle" | "visited" | "active" | "error";

/** layout positions nodes with dagre, left-to-right or top-to-bottom. Exported for tests. */
export function layout(graph: AgentGraph, direction: Direction = "LR"): Record<string, { x: number; y: number }> {
  const g = new dagre.graphlib.Graph();
  g.setGraph({ rankdir: direction, nodesep: 28, ranksep: direction === "LR" ? 48 : 36 });
  g.setDefaultEdgeLabel(() => ({}));
  graph.nodes.forEach((n) => g.setNode(n.id, { width: W, height: H }));
  graph.edges.forEach((e) => g.hasNode(e.source) && g.hasNode(e.target) && g.setEdge(e.source, e.target));
  dagre.layout(g);
  const out: Record<string, { x: number; y: number }> = {};
  graph.nodes.forEach((n) => {
    const p = g.node(n.id);
    out[n.id] = { x: (p?.x ?? 0) - W / 2, y: (p?.y ?? 0) - H / 2 };
  });
  return out;
}

const isTerminal = (id: string) => id === "__start__" || id === "__end__";

function style(id: string, state: NodeState) {
  const base = {
    width: W, height: H, borderRadius: 14, fontSize: 13, fontWeight: 500,
    display: "flex", alignItems: "center", justifyContent: "center", color: "#ece7f8",
    border: "1px solid #342853", background: "#18102c",
  };
  if (isTerminal(id)) return { ...base, borderRadius: 999, background: "#120c22", color: "#a99cc9", fontSize: 12 };
  if (state === "active") return { ...base, border: "1px solid #c084fc", background: "#3b1d63", boxShadow: "0 0 0 4px rgb(168 85 247 / .25)" };
  if (state === "visited") return { ...base, border: "1px solid #9333ea", background: "#2a1748" };
  if (state === "error") return { ...base, border: "1px solid #fb7185", background: "#3a1426" };
  return base;
}

export function GraphView({ graph, states = {}, height = 460, direction = "LR" }: { graph?: AgentGraph | null; states?: Record<string, NodeState>; height?: number; direction?: Direction }) {
  const { nodes, edges } = useMemo(() => {
    if (!graph?.nodes?.length) return { nodes: [] as Node[], edges: [] as Edge[] };
    const pos = layout(graph, direction);
    const lr = direction === "LR";
    const nodes: Node[] = graph.nodes.map((n) => ({
      id: n.id,
      position: pos[n.id],
      data: { label: n.id === "__start__" ? "start" : n.id === "__end__" ? "end" : n.id },
      style: style(n.id, states[n.id] ?? "idle"),
      sourcePosition: lr ? Position.Right : Position.Bottom,
      targetPosition: lr ? Position.Left : Position.Top,
      draggable: true,
      connectable: false,
    }));
    const edges: Edge[] = graph.edges.map((e, i) => {
      const hot = states[e.source] && states[e.source] !== "idle" && states[e.target] && states[e.target] !== "idle";
      return {
        id: `${e.source}-${e.target}-${i}`,
        source: e.source,
        target: e.target,
        type: "smoothstep",
        animated: !!hot,
        style: { stroke: hot ? "#c084fc" : "#7d6fa3", strokeWidth: hot ? 2.25 : 1.5, strokeDasharray: e.conditional ? "6 4" : undefined },
        markerEnd: { type: MarkerType.ArrowClosed, color: hot ? "#c084fc" : "#7d6fa3" },
      };
    });
    return { nodes, edges };
  }, [graph, states, direction]);

  if (!nodes.length) {
    return <div className="flex h-40 items-center justify-center text-sm text-ink-400">This agent has not reported a graph yet.</div>;
  }
  return (
    <div style={{ height }} className="overflow-hidden rounded-xl border border-ink-700 bg-ink-950/50">
      <ReactFlow nodes={nodes} edges={edges} fitView fitViewOptions={{ padding: 0.15, maxZoom: 1.25 }} proOptions={{ hideAttribution: true }} nodesConnectable={false}>
        <Background color="#2b2145" gap={20} />
        <Controls showInteractive={false} />
      </ReactFlow>
    </div>
  );
}
