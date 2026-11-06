import { useState } from "react";
import { Link } from "react-router-dom";
import { Check, Hand, X } from "lucide-react";
import { useAction, useInterrupts } from "../api/hooks";
import { api } from "../api/client";
import type { Interrupt } from "../api/types";
import { Can, Empty, ErrorBox, JsonView, Loading, PageHeader, Tabs } from "../components/ui";
import { timeAgo } from "../lib/format";

function Card({ i }: { i: Interrupt }) {
  const [note, setNote] = useState("");
  const resolve = useAction((decision: "approve" | "reject") =>
    api(`/api/v1/interrupts/${i.id}/resolve`, { method: "POST", body: { decision, response: note ? { note } : undefined } }),
  );
  return (
    <div className="card p-5">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <div className="flex items-center gap-2 text-lg font-semibold text-white"><Hand className="h-4 w-4 text-amber-300" />{i.question || "Approval requested"}</div>
          <div className="mt-1 text-sm text-ink-300">
            <Link to={`/agents/${i.agent_id}`} className="text-brand-300 hover:underline">{i.agent_name}</Link> · node <span className="font-mono">{i.node}</span> ·{" "}
            <Link to={`/runs/${i.run_id}`} className="text-brand-300 hover:underline">run</Link> · {timeAgo(i.created_at)}
          </div>
        </div>
        {i.status !== "pending" && (
          <span className={i.status === "approved" ? "text-emerald-300" : "text-rose-300"}>
            {i.status} by {i.resolved_by} {timeAgo(i.resolved_at)}
          </span>
        )}
      </div>
      {i.payload !== undefined && <div className="mt-4"><JsonView value={i.payload} /></div>}
      {i.status === "pending" && (
        <Can role="operator">
          <div className="mt-4 flex flex-col gap-2 sm:flex-row">
            <input className="input" placeholder="Note for the agent (optional)" value={note} onChange={(e) => setNote(e.target.value)} />
            <div className="flex gap-2">
              <button className="btn-danger" onClick={() => resolve.mutate("reject")} disabled={resolve.isPending}><X className="h-4 w-4" />Reject</button>
              <button className="btn-primary" onClick={() => resolve.mutate("approve")} disabled={resolve.isPending}><Check className="h-4 w-4" />Approve</button>
            </div>
          </div>
          <div className="mt-2"><ErrorBox error={resolve.error} /></div>
        </Can>
      )}
    </div>
  );
}

export function ApprovalsPage() {
  const [tab, setTab] = useState<"pending" | "">("pending");
  const { data, isLoading, error } = useInterrupts(tab);
  return (
    <>
      <PageHeader title="Approvals" subtitle="Human-in-the-loop checkpoints raised by your graphs." />
      <Tabs value={tab} onChange={setTab} tabs={[{ id: "pending", label: "Pending" }, { id: "", label: "History" }]} />
      <ErrorBox error={error} />
      {isLoading ? <Loading /> : !data?.length ? (
        <div className="card"><Empty title="Nothing waiting" hint={<>Call <code className="text-brand-300">deck.request_approval(...)</code> inside a node to pause a run until someone decides.</>} icon={<Hand className="h-6 w-6" />} /></div>
      ) : (
        <div className="space-y-4">{data.map((i) => <Card key={i.id} i={i} />)}</div>
      )}
    </>
  );
}
