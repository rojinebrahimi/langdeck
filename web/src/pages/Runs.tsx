import { useEffect } from "react";
import { useSearchParams } from "react-router-dom";
import clsx from "clsx";
import { useAgents, useRunsPage } from "../api/hooks";
import { RunsTable } from "../components/RunsTable";
import { Pager, useCursorPager } from "../components/Pager";
import { ErrorBox, Loading, PageHeader } from "../components/ui";

export const RUNS_PAGE_SIZE = 50;

const statuses = ["", "pending", "running", "interrupted", "succeeded", "failed", "cancelled"];

export function RunsPage() {
  const [params, setParams] = useSearchParams();
  const agent_id = params.get("agent_id") ?? "";
  const status = params.get("status") ?? "";
  const agents = useAgents();
  const pager = useCursorPager();
  const { reset } = pager;
  useEffect(() => reset(), [agent_id, status, reset]);
  const runs = useRunsPage({ agent_id, status, limit: RUNS_PAGE_SIZE, before: pager.cursor });
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
      <div className={clsx("card overflow-hidden transition-opacity", runs.isFetching && !runs.isLoading && "opacity-70")}>
        {runs.isLoading ? <Loading /> : <RunsTable runs={runs.data?.runs} />}
      </div>
      {!runs.isLoading && (
        <Pager
          page={pager.page}
          pageSize={RUNS_PAGE_SIZE}
          shown={runs.data?.runs.length ?? 0}
          nextCursor={runs.data?.next_before}
          onNext={(c) => {
            pager.next(c);
            window.scrollTo?.({ top: 0, behavior: "smooth" });
          }}
          onPrev={pager.prev}
          busy={runs.isFetching}
          noun="runs"
        />
      )}
    </>
  );
}
