import { Link } from "react-router-dom";
import { Activity, AlertOctagon, Bot, Coins, Gauge, Hand } from "lucide-react";
import { useInterrupts, useRuns, useStats } from "../api/hooks";
import type { Stats } from "../api/types";
import { Empty, ErrorBox, Loading, PageHeader, Stat, StatusBadge } from "../components/ui";
import { compact, duration, formatMs, timeAgo } from "../lib/format";

function HourlyChart({ hourly }: { hourly: Stats["hourly"] }) {
  const max = Math.max(1, ...hourly.map((h) => h.succeeded + h.failed + h.other));
  return (
    <div>
      <div className="flex h-40 items-end gap-1" role="img" aria-label="Runs per hour, last 24 hours">
        {hourly.map((h) => {
          const total = h.succeeded + h.failed + h.other;
          const pct = (n: number) => `${(n / max) * 100}%`;
          return (
            <div key={h.hour} className="group relative flex h-full flex-1 flex-col justify-end" title={`${new Date(h.hour).getHours()}:00 — ${total} runs`}>
              <div className="flex flex-col justify-end overflow-hidden rounded-t-md" style={{ height: pct(total) }}>
                <div className="bg-rose-400/80" style={{ height: total ? `${(h.failed / total) * 100}%` : 0 }} />
                <div className="bg-ink-400/60" style={{ height: total ? `${(h.other / total) * 100}%` : 0 }} />
                <div className="flex-1 bg-gradient-to-t from-brand-600 to-brand-400" />
              </div>
              {total === 0 && <div className="h-0.5 rounded bg-ink-700" />}
            </div>
          );
        })}
      </div>
      <div className="mt-2 flex justify-between text-[11px] text-ink-400">
        <span>24h ago</span>
        <span>now</span>
      </div>
    </div>
  );
}

export function DashboardPage() {
  const stats = useStats();
  const runs = useRuns({ limit: 8 });
  const pending = useInterrupts("pending");

  if (stats.isLoading) return <Loading />;
  if (stats.error) return <ErrorBox error={stats.error} />;
  const s = stats.data!;

  return (
    <>
      <PageHeader title="Overview" subtitle="Health and activity of every agent, over the last 24 hours." />
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <Stat label="Agents online" value={`${s.agents_online}/${s.agents_total}`} icon={<Bot className="h-4 w-4" />} hint="Heartbeat within 45s" />
        <Stat label="Runs (24h)" value={compact(s.runs_last_24h)} icon={<Activity className="h-4 w-4" />} hint={`${s.runs_by_status.running ?? 0} running now`} />
        <Stat
          label="Failure rate"
          value={`${(s.failure_rate * 100).toFixed(1)}%`}
          icon={<AlertOctagon className="h-4 w-4" />}
          hint={`${s.runs_by_status.failed ?? 0} failed runs`}
        />
        <Stat label="Latency p50 / p95" value={<span className="text-2xl">{formatMs(s.p50_latency_ms)} / {formatMs(s.p95_latency_ms)}</span>} icon={<Gauge className="h-4 w-4" />} hint={<span className="inline-flex items-center gap-1"><Coins className="h-3 w-3" /> {compact(s.tokens_last_24h)} tokens</span>} />
      </div>

      <div className="mt-6 grid grid-cols-1 gap-6 xl:grid-cols-3">
        <div className="card p-5 xl:col-span-2">
          <div className="mb-4 flex items-center justify-between">
            <h2 className="font-semibold text-white">Runs per hour</h2>
            <div className="flex gap-3 text-xs text-ink-300">
              <span className="flex items-center gap-1.5"><span className="h-2 w-2 rounded-sm bg-brand-500" />succeeded</span>
              <span className="flex items-center gap-1.5"><span className="h-2 w-2 rounded-sm bg-rose-400" />failed</span>
              <span className="flex items-center gap-1.5"><span className="h-2 w-2 rounded-sm bg-ink-400" />other</span>
            </div>
          </div>
          <HourlyChart hourly={s.hourly} />
        </div>
        <div className="card p-5">
          <div className="mb-4 flex items-center justify-between">
            <h2 className="font-semibold text-white">Needs your approval</h2>
            <Link to="/approvals" className="text-xs text-brand-300 hover:text-brand-200">View all</Link>
          </div>
          {pending.data?.length ? (
            <ul className="space-y-2">
              {pending.data.slice(0, 5).map((i) => (
                <li key={i.id}>
                  <Link to="/approvals" className="block rounded-xl border border-amber-400/20 bg-amber-400/5 px-3 py-2 hover:border-amber-400/40">
                    <div className="flex items-center gap-2 text-sm text-amber-100"><Hand className="h-3.5 w-3.5" />{i.question || "Approval requested"}</div>
                    <div className="mt-0.5 text-xs text-ink-300">{i.agent_name} · {i.node} · {timeAgo(i.created_at)}</div>
                  </Link>
                </li>
              ))}
            </ul>
          ) : (
            <Empty title="All clear" hint="No run is waiting for a human." icon={<Hand className="h-5 w-5" />} />
          )}
        </div>
      </div>

      <div className="mt-6 grid grid-cols-1 gap-6 xl:grid-cols-3">
        <div className="card overflow-hidden xl:col-span-2">
          <div className="flex items-center justify-between px-5 pt-5">
            <h2 className="font-semibold text-white">Latest runs</h2>
            <Link to="/runs" className="text-xs text-brand-300 hover:text-brand-200">View all</Link>
          </div>
          <table className="mt-3 w-full">
            <tbody className="divide-y divide-ink-800">
              {runs.data?.map((r) => (
                <tr key={r.id} className="hover:bg-ink-800/40">
                  <td className="table-cell"><Link to={`/runs/${r.id}`} className="font-mono text-xs text-brand-300 hover:underline">{r.id.slice(0, 8)}</Link></td>
                  <td className="table-cell">{r.agent_name}</td>
                  <td className="table-cell"><StatusBadge status={r.status} /></td>
                  <td className="table-cell text-ink-300">{duration(r.started_at, r.ended_at)}</td>
                  <td className="table-cell text-right text-ink-400">{timeAgo(r.created_at)}</td>
                </tr>
              ))}
            </tbody>
          </table>
          {!runs.data?.length && <Empty title="No runs yet" hint="Runs appear here as soon as an instrumented agent executes." />}
        </div>
        <div className="card p-5">
          <h2 className="mb-4 font-semibold text-white">Busiest agents</h2>
          {s.top_agents.length ? (
            <ul className="space-y-3">
              {s.top_agents.map((a) => {
                const max = s.top_agents[0].runs || 1;
                return (
                  <li key={a.agent_name}>
                    <div className="mb-1 flex justify-between text-sm"><span className="text-ink-100">{a.agent_name}</span><span className="text-ink-300">{a.runs} runs{a.failed ? ` · ${a.failed} failed` : ""}</span></div>
                    <div className="h-1.5 overflow-hidden rounded-full bg-ink-800"><div className="h-full rounded-full bg-gradient-to-r from-brand-600 to-brand-400" style={{ width: `${(a.runs / max) * 100}%` }} /></div>
                  </li>
                );
              })}
            </ul>
          ) : (
            <Empty title="No activity" />
          )}
        </div>
      </div>
    </>
  );
}
