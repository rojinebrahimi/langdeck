import { useAudit } from "../api/hooks";
import { ErrorBox, Loading, PageHeader } from "../components/ui";

export function AuditPage() {
  const { data, isLoading, error } = useAudit();
  return (
    <>
      <PageHeader title="Audit log" subtitle="Append-only record of every change made through LangDeck." />
      <ErrorBox error={error} />
      <div className="card overflow-x-auto">
        {isLoading ? <Loading /> : (
          <table className="w-full min-w-[760px]">
            <thead className="border-b border-ink-800"><tr><th className="table-head">When</th><th className="table-head">Actor</th><th className="table-head">Action</th><th className="table-head">Target</th><th className="table-head">Details</th><th className="table-head">IP</th></tr></thead>
            <tbody className="divide-y divide-ink-800">
              {data?.map((e) => (
                <tr key={e.id}>
                  <td className="table-cell whitespace-nowrap text-ink-300">{new Date(e.created_at).toLocaleString()}</td>
                  <td className="table-cell">{e.actor}</td>
                  <td className="table-cell"><span className="rounded-md bg-brand-500/15 px-2 py-0.5 font-mono text-xs text-brand-200">{e.action}</span></td>
                  <td className="table-cell font-mono text-xs text-ink-300">{e.target_type}/{e.target_id.slice(0, 12)}</td>
                  <td className="table-cell max-w-xs truncate font-mono text-xs text-ink-400">{e.details ? JSON.stringify(e.details) : ""}</td>
                  <td className="table-cell text-ink-400">{e.ip}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </>
  );
}
