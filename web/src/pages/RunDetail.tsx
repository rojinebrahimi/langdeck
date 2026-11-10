import { memo, useCallback, useMemo, useState } from "react";
import { Link, useParams } from "react-router-dom";
import clsx from "clsx";
import { Ban, ChevronRight, Clock, Coins } from "lucide-react";
import { useAction, useAgent, useRun, useRunEvents } from "../api/hooks";
import { api } from "../api/client";
import type { RunEvent } from "../api/types";
import { GraphView } from "../components/GraphView";
import { Can, ErrorBox, JsonView, Loading, PageHeader, StatusBadge } from "../components/ui";
import { compact, duration, formatMs } from "../lib/format";
import { eventLabel, nodeStates } from "../lib/trace";

const dot: Record<string, string> = {
  run_start: "bg-brand-400", run_end: "bg-emerald-400", run_error: "bg-rose-400",
  node_start: "bg-brand-500", node_end: "bg-brand-300", node_error: "bg-rose-400",
  llm_start: "bg-sky-400", llm_end: "bg-sky-300", tool_start: "bg-teal-400", tool_end: "bg-teal-300",
  interrupt: "bg-amber-400", log: "bg-ink-400",
};

// One shared formatter: calling toLocaleTimeString per row is surprisingly slow with hundreds of rows.
const timeFmt = new Intl.DateTimeFormat(undefined, { timeStyle: "medium" });
// Long traces render in slices so opening a run with thousands of events stays responsive.
const TRACE_SLICE = 100;

const EventRow = memo(function EventRow({ e, selected, onSelect }: { e: RunEvent; selected: boolean; onSelect: (id: number) => void }) {
  return (
    <li>
      <button onClick={() => onSelect(e.id)} className={clsx("flex w-full items-center gap-3 rounded-lg px-3 py-2 text-left text-sm transition", selected ? "bg-brand-500/15 ring-1 ring-brand-400/30" : "hover:bg-ink-800/60")}>
        <span className={clsx("h-2 w-2 shrink-0 rounded-full", dot[e.type] ?? "bg-ink-400")} />
        <span className="flex-1 truncate text-ink-100">
          {eventLabel(e.type)}
          {e.node && <span className="ml-2 rounded-md bg-ink-800 px-1.5 py-0.5 font-mono text-xs text-brand-200">{e.node}</span>}
        </span>
        {!!e.latency_ms && <span className="text-xs text-ink-400">{formatMs(e.latency_ms)}</span>}
        <span className="font-mono text-[11px] text-ink-500">{timeFmt.format(new Date(e.ts))}</span>
        <ChevronRight className="h-3.5 w-3.5 text-ink-500" />
      </button>
    </li>
  );
});

export function RunDetailPage() {
  const { id = "" } = useParams();
  const run = useRun(id);
  const events = useRunEvents(id);
  const agent = useAgent(run.data?.agent_id ?? "");
  const [selected, setSelected] = useState<number | null>(null);
  const [shown, setShown] = useState(TRACE_SLICE);
  const toggleSelected = useCallback((eid: number) => setSelected((cur) => (cur === eid ? null : eid)), []);
  const cancel = useAction(() => api(`/api/v1/runs/${id}/cancel`, { method: "POST" }));
  const states = useMemo(() => nodeStates(events.data ?? [], run.data?.status ?? "pending"), [events.data, run.data?.status]);

  if (run.isLoading) return <Loading />;
  if (run.error || !run.data) return <ErrorBox error={run.error ?? "Run not found"} />;
  const r = run.data;
  const active = r.status === "pending" || r.status === "running" || r.status === "interrupted";
  const sel = events.data?.find((e) => e.id === selected);

  return (
    <>
      <PageHeader
        title={<span className="flex items-center gap-3"><span className="font-mono text-xl">{r.id.length > 18 ? r.id.slice(0, 18) + "…" : r.id}</span><StatusBadge status={r.status} /></span>}
        subtitle={
          <span className="flex flex-wrap items-center gap-x-4 gap-y-1">
            <Link to={`/agents/${r.agent_id}`} className="text-brand-300 hover:underline">{r.agent_name}</Link>
            <span className="flex items-center gap-1"><Clock className="h-3.5 w-3.5" />{duration(r.started_at, r.ended_at)}</span>
            <span className="flex items-center gap-1"><Coins className="h-3.5 w-3.5" />{compact(r.prompt_tokens)} in · {compact(r.completion_tokens)} out</span>
            <span>trigger: {r.trigger}</span>
            {r.thread_id && <span>thread: {r.thread_id}</span>}
          </span>
        }
        actions={active && <Can role="operator"><button className="btn-danger" onClick={() => cancel.mutate(undefined)} disabled={cancel.isPending}><Ban className="h-4 w-4" />Cancel run</button></Can>}
      />
      <ErrorBox error={cancel.error} />
      {r.error && <div className="mb-6"><ErrorBox error={r.error} /></div>}

      <div className="grid grid-cols-1 gap-6 xl:grid-cols-2">
        <div className="card p-4">
          <h2 className="mb-3 px-1 font-semibold text-white">Execution path</h2>
          <GraphView graph={agent.data?.agent.graph} states={states} height={440} direction="TB" />
        </div>
        <div className="card flex flex-col p-4">
          <h2 className="mb-3 px-1 font-semibold text-white">Trace <span className="text-sm font-normal text-ink-400">({events.data?.length ?? 0} events)</span></h2>
          <ol className="max-h-[440px] flex-1 space-y-0.5 overflow-auto pr-1">
            {events.data?.slice(0, shown).map((e) => <EventRow key={e.id} e={e} selected={e.id === selected} onSelect={toggleSelected} />)}
            {events.data && events.data.length > shown && (
              <li className="flex items-center justify-center gap-2 py-2">
                <button className="btn-ghost" onClick={() => setShown((n) => n + 200)}>Show {Math.min(200, events.data.length - shown)} more</button>
                <button className="btn-ghost" onClick={() => setShown(events.data!.length)}>Show all ({events.data.length})</button>
              </li>
            )}
          </ol>
        </div>
      </div>

      {sel && (
        <div className="card mt-6 p-5">
          <h3 className="mb-3 font-semibold text-white">{eventLabel(sel.type)} {sel.node && <span className="font-mono text-brand-300">· {sel.node}</span>}</h3>
          <JsonView value={sel.payload} />
        </div>
      )}

      <div className="mt-6 grid grid-cols-1 gap-6 lg:grid-cols-2">
        <div className="card p-5"><h3 className="label">Input</h3><JsonView value={r.input} /></div>
        <div className="card p-5"><h3 className="label">Output</h3><JsonView value={r.output} /></div>
      </div>
    </>
  );
}
