import { Link } from "react-router-dom";
import type { Run } from "../api/types";
import { compact, duration, timeAgo } from "../lib/format";
import { Empty, StatusBadge } from "./ui";

export function RunsTable({ runs, showAgent = true }: { runs?: Run[]; showAgent?: boolean }) {
  if (!runs?.length) return <Empty title="No runs match" hint="Start a run from an agent page, or run your instrumented graph." />;
  return (
    <div className="overflow-x-auto">
      <table className="w-full min-w-[640px]">
        <thead className="border-b border-ink-800">
          <tr>
            <th className="table-head">Run</th>
            {showAgent && <th className="table-head">Agent</th>}
            <th className="table-head">Status</th>
            <th className="table-head">Trigger</th>
            <th className="table-head">Tokens</th>
            <th className="table-head">Duration</th>
            <th className="table-head text-right">Started</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-ink-800">
          {runs.map((r) => (
            <tr key={r.id} className="hover:bg-ink-800/40">
              <td className="table-cell">
                <Link to={`/runs/${r.id}`} className="font-mono text-xs text-brand-300 hover:underline">{r.id.length > 14 ? r.id.slice(0, 14) + "…" : r.id}</Link>
                {r.thread_id && <div className="text-[11px] text-ink-400">thread {r.thread_id}</div>}
              </td>
              {showAgent && <td className="table-cell">{r.agent_name}</td>}
              <td className="table-cell"><StatusBadge status={r.status} /></td>
              <td className="table-cell text-ink-300">{r.trigger}</td>
              <td className="table-cell text-ink-300">{compact(r.prompt_tokens + r.completion_tokens)}</td>
              <td className="table-cell text-ink-300">{duration(r.started_at, r.ended_at)}</td>
              <td className="table-cell text-right text-ink-400">{timeAgo(r.created_at)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
