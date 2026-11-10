import { useMemo } from "react";
import { Link } from "react-router-dom";
import { Network } from "lucide-react";
import { useAgentGraph } from "../api/hooks";
import type { AgentGraph } from "../api/types";
import { GraphView, type NodeState } from "../components/GraphView";
import { Empty, ErrorBox, Loading, PageHeader } from "../components/ui";

/** Online agents are lit, offline ones red; paused and not-yet-registered ones stay neutral. */
const stateOf = (status: string): NodeState => (status === "online" ? "visited" : status === "offline" ? "error" : "idle");

export function DependenciesPage() {
  const { data, isLoading, error } = useAgentGraph();
  const { graph, states } = useMemo(() => {
    const nodes = data?.nodes ?? [];
    const linked = new Set((data?.edges ?? []).flatMap((e) => [e.source, e.target]));
    // Agents with no edges only add noise to the picture; they are still listed below.
    const graph: AgentGraph = {
      nodes: nodes.filter((n) => linked.has(n.name)).map((n) => ({ id: n.name })),
      edges: (data?.edges ?? []).map((e) => ({ source: e.source, target: e.target })),
    };
    return { graph, states: Object.fromEntries(nodes.map((n) => [n.name, stateOf(n.status)])) };
  }, [data]);

  if (isLoading) return <Loading />;
  if (error || !data) return <ErrorBox error={error ?? "Could not load the dependency graph"} />;
  const missing = data.nodes.filter((n) => !n.registered);

  return (
    <>
      <PageHeader title="Dependencies" subtitle="An arrow points from an agent to an agent it declares it calls or hands off to." />
      {data.edges.length === 0 ? (
        <Empty
          icon={<Network className="h-6 w-6" />}
          title="No dependencies declared"
          hint={<>Pass <code className="text-brand-300">depends_on=["billing"]</code> to <code className="text-brand-300">LangDeck(...)</code> and the graph appears here.</>}
        />
      ) : (
        <div className="card p-4"><GraphView graph={graph} states={states} height={480} /></div>
      )}
      {missing.length > 0 && (
        <p className="mt-4 text-sm text-amber-300">
          Declared but not registered: {missing.map((n) => n.name).join(", ")}. Start those agents or fix the names.
        </p>
      )}
      <div className="card mt-6 overflow-x-auto">
        <table className="w-full min-w-[480px]">
          <thead className="border-b border-ink-800"><tr><th className="table-head">Agent</th><th className="table-head">Depends on</th><th className="table-head text-right">Accesses</th></tr></thead>
          <tbody className="divide-y divide-ink-800">
            {data.nodes.filter((n) => n.registered).map((n) => (
              <tr key={n.name}>
                <td className="table-cell"><Link to={`/agents/${n.id}`} className="text-brand-300 hover:underline">{n.name}</Link></td>
                <td className="table-cell font-mono text-xs text-ink-300">{data.edges.filter((e) => e.source === n.name).map((e) => e.target).join(", ") || "—"}</td>
                <td className="table-cell text-right text-ink-300">{n.accesses}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </>
  );
}
