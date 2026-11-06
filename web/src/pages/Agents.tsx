import { Link } from "react-router-dom";
import { Bot, Cpu, Tag } from "lucide-react";
import { useAgents } from "../api/hooks";
import { Empty, ErrorBox, Loading, PageHeader, StatusBadge } from "../components/ui";
import { timeAgo } from "../lib/format";

export function AgentsPage() {
  const { data, isLoading, error } = useAgents();
  if (isLoading) return <Loading />;
  return (
    <>
      <PageHeader title="Agents" subtitle="Every LangGraph application connected through the LangDeck SDK." />
      <ErrorBox error={error} />
      {!data?.length ? (
        <div className="card">
          <Empty
            icon={<Bot className="h-6 w-6" />}
            title="No agents connected yet"
            hint={
              <>
                Create an <Link to="/settings/api-keys" className="text-brand-300 underline">agent API key</Link>, then add three lines to your graph:
                <pre className="mt-3 rounded-xl bg-ink-950 p-3 text-left font-mono text-xs text-ink-200">{`deck = LangDeck(name="my-agent")
app = deck.instrument(graph.compile())
deck.start()`}</pre>
              </>
            }
          />
        </div>
      ) : (
        <div className="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3">
          {data.map((a) => (
            <Link key={a.id} to={`/agents/${a.id}`} className="card group p-5 transition hover:border-brand-500/50 hover:shadow-lg hover:shadow-brand-900/20">
              <div className="flex items-start justify-between gap-3">
                <div className="flex min-w-0 items-center gap-3">
                  <div className="rounded-xl bg-brand-500/15 p-2.5 text-brand-300 ring-1 ring-brand-400/20 group-hover:bg-brand-500/25"><Bot className="h-5 w-5" /></div>
                  <div className="min-w-0">
                    <div className="truncate font-semibold text-white">{a.name}</div>
                    <div className="text-xs text-ink-400">{a.framework} {a.version && `· v${a.version}`}</div>
                  </div>
                </div>
                <StatusBadge kind="agent" status={a.status} />
              </div>
              <p className="mt-3 line-clamp-2 min-h-10 text-sm text-ink-300">{a.description || "No description."}</p>
              <div className="mt-4 flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-ink-400">
                <span className="flex items-center gap-1"><Cpu className="h-3 w-3" />{a.instances} instance{a.instances === 1 ? "" : "s"}</span>
                <span>heartbeat {timeAgo(a.last_heartbeat)}</span>
                {Object.entries(a.labels ?? {}).slice(0, 2).map(([k, v]) => (
                  <span key={k} className="flex items-center gap-1 rounded-md bg-ink-800 px-1.5 py-0.5"><Tag className="h-3 w-3" />{k}={v}</span>
                ))}
              </div>
            </Link>
          ))}
        </div>
      )}
    </>
  );
}
