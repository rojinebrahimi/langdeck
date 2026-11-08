import { useEffect, useState } from "react";
import { Link, useSearchParams } from "react-router-dom";
import clsx from "clsx";
import { Bot, Cpu, Search, Tag } from "lucide-react";
import { useAgentsPage } from "../api/hooks";
import { Empty, ErrorBox, Loading, PageHeader, StatusBadge } from "../components/ui";
import { Pager, useCursorPager } from "../components/Pager";
import { timeAgo } from "../lib/format";

export const AGENTS_PAGE_SIZE = 24;
const statusFilters = [
  { id: "", label: "All" },
  { id: "online", label: "Online" },
  { id: "paused", label: "Paused" },
  { id: "offline", label: "Offline" },
] as const;

export function AgentsPage() {
  const [params, setParams] = useSearchParams();
  const q = params.get("q") ?? "";
  const status = params.get("status") ?? "";
  const [search, setSearch] = useState(q);
  const pager = useCursorPager();
  const { reset } = pager;

  // Debounce typing into the URL; any filter change starts again at page 1.
  useEffect(() => {
    const id = setTimeout(() => {
      if (search.trim() === q) return;
      const next = new URLSearchParams(params);
      if (search.trim()) next.set("q", search.trim());
      else next.delete("q");
      setParams(next, { replace: true });
    }, 250);
    return () => clearTimeout(id);
  }, [search, q, params, setParams]);
  useEffect(() => reset(), [q, status, reset]);

  const setStatus = (s: string) => {
    const next = new URLSearchParams(params);
    if (s) next.set("status", s);
    else next.delete("status");
    setParams(next, { replace: true });
  };

  const { data: page, isLoading, isFetching, error } = useAgentsPage({ q, status, after: pager.cursor, limit: AGENTS_PAGE_SIZE });
  const agents = page?.agents ?? [];
  const filtered = !!(q || status);

  return (
    <>
      <PageHeader title="Agents" subtitle="Every LangGraph application connected through the LangDeck SDK." />
      <div className="mb-5 flex flex-wrap items-center gap-3">
        <label className="relative min-w-56 flex-1 sm:max-w-sm">
          <span className="sr-only">Search agents</span>
          <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-ink-400" />
          <input
            type="search"
            className="input pl-9"
            placeholder="Search by name or description"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
          />
        </label>
        <div role="group" aria-label="Filter by status" className="inline-flex rounded-xl border border-ink-700 bg-ink-850 p-1">
          {statusFilters.map((s) => (
            <button
              key={s.id}
              type="button"
              aria-pressed={status === s.id}
              onClick={() => setStatus(s.id)}
              className={clsx(
                "rounded-lg px-3 py-1.5 text-sm font-medium transition",
                status === s.id ? "bg-brand-500/20 text-white ring-1 ring-brand-400/30" : "text-ink-300 hover:text-white",
              )}
            >
              {s.label}
            </button>
          ))}
        </div>
      </div>
      <ErrorBox error={error} />
      {isLoading ? (
        <Loading />
      ) : !agents.length && filtered ? (
        <div className="card">
          <Empty icon={<Search className="h-6 w-6" />} title="No agents match" hint="Try another search or status filter." />
        </div>
      ) : !agents.length ? (
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
        <div className={clsx("grid grid-cols-1 gap-4 transition-opacity md:grid-cols-2 xl:grid-cols-3", isFetching && "opacity-70")}>
          {agents.map((a) => (
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
      {!isLoading && (
        <Pager
          page={pager.page}
          pageSize={AGENTS_PAGE_SIZE}
          shown={agents.length}
          total={page?.total}
          nextCursor={page?.next_after}
          onNext={(c) => {
            pager.next(c);
            window.scrollTo?.({ top: 0, behavior: "smooth" });
          }}
          onPrev={pager.prev}
          busy={isFetching}
          noun={page?.total === 1 ? "agent" : "agents"}
        />
      )}
    </>
  );
}
