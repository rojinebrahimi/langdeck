import { useSearchParams } from "react-router-dom";
import { useAgents, useRuns } from "../api/hooks";
import { RunsTable } from "../components/RunsTable";
import { ErrorBox, Loading, PageHeader } from "../components/ui";

const statuses = ["", "pending", "running", "interrupted", "succeeded", "failed", "cancelled"];

export function RunsPage() {
  const [params, setParams] = useSearchParams();
  const agent_id = params.get("agent_id") ?? "";
  const status = params.get("status") ?? "";
  const agents = useAgents();
  const runs = useRuns({ agent_id, status, limit: 100 });
  const set = (k: string, v: string) => {
    const next = new URLSearchParams(params);
    if (v) next.set(k, v);
    else next.delete(k);
    setParams(next, { replace: true });
  };
  return (
    <>
      <PageHeader title="Runs" subtitle="Every graph execution, live and historical." />
      <div className="mb-4 flex flex-wrap gap-3">
        <select aria-label="Filter by agent" className="input w-auto" value={agent_id} onChange={(e) => set("agent_id", e.target.value)}>
          <option value="">All agents</option>
          {agents.data?.map((a) => <option key={a.id} value={a.id}>{a.name}</option>)}
        </select>
        <select aria-label="Filter by status" className="input w-auto" value={status} onChange={(e) => set("status", e.target.value)}>
          {statuses.map((s) => <option key={s} value={s}>{s || "All statuses"}</option>)}
        </select>
      </div>
      <ErrorBox error={runs.error} />
      <div className="card overflow-hidden">{runs.isLoading ? <Loading /> : <RunsTable runs={runs.data} />}</div>
    </>
  );
}
