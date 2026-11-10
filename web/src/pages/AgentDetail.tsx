import { useRef, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate, useParams } from "react-router-dom";
import { Pause, Play, Rocket, Save, Trash2 } from "lucide-react";
import { useAction, useAgent, useAgentCommands, useAgentGraph, useAgentConfigs, useRunsPage } from "../api/hooks";
import { api, shrinkImage, uploadAgentImage } from "../api/client";
import type { Agent, Run } from "../api/types";
import { Link } from "react-router-dom";
import { AgentAvatar } from "../components/AgentAvatar";
import { Pager, useCursorPager } from "../components/Pager";
import { AccessModes } from "../components/AccessModes";
import { GraphView } from "../components/GraphView";
import { RunsTable } from "../components/RunsTable";
import { Can, ErrorBox, JsonView, Loading, Modal, PageHeader, StatusBadge, Tabs, useCan } from "../components/ui";
import { pretty, timeAgo } from "../lib/format";

type Tab = "graph" | "access" | "runs" | "config" | "commands";

function parseJSON(text: string): { ok: true; value: unknown } | { ok: false; error: string } {
  try {
    return { ok: true, value: text.trim() ? JSON.parse(text) : {} };
  } catch (e) {
    return { ok: false, error: (e as Error).message };
  }
}

function StartRunModal({ agentId, open, onClose }: { agentId: string; open: boolean; onClose: () => void }) {
  const nav = useNavigate();
  const [input, setInput] = useState('{\n  "messages": [{"role": "user", "content": "Hello"}]\n}');
  const [thread, setThread] = useState("");
  const start = useAction((body: { input: unknown; thread_id: string }) =>
    api<{ run: Run; warning?: string }>(`/api/v1/agents/${agentId}/runs`, { method: "POST", body }),
  );
  const parsed = parseJSON(input);
  return (
    <Modal open={open} onClose={onClose} title="Start a run">
      <form
        className="space-y-4"
        onSubmit={(e) => {
          e.preventDefault();
          if (!parsed.ok) return;
          start.mutate({ input: parsed.value, thread_id: thread }, { onSuccess: (r) => nav(`/runs/${r.run.id}`) });
        }}
      >
        <div>
          <label className="label" htmlFor="run-input">Input (JSON)</label>
          <textarea id="run-input" rows={8} className="input font-mono text-xs" value={input} onChange={(e) => setInput(e.target.value)} />
          {!parsed.ok && <p className="mt-1 text-xs text-rose-300">{parsed.error}</p>}
        </div>
        <div>
          <label className="label" htmlFor="thread">Thread id (optional)</label>
          <input id="thread" className="input" placeholder="conversation-42" value={thread} onChange={(e) => setThread(e.target.value)} />
        </div>
        <ErrorBox error={start.error} />
        <div className="flex justify-end gap-2">
          <button type="button" className="btn-ghost" onClick={onClose}>Cancel</button>
          <button type="submit" className="btn-primary" disabled={!parsed.ok || start.isPending}><Rocket className="h-4 w-4" />Start run</button>
        </div>
      </form>
    </Modal>
  );
}

function ConfigTab({ agentId, current }: { agentId: string; current?: Record<string, unknown> }) {
  const configs = useAgentConfigs(agentId);
  const canEdit = useCan("operator");
  const [text, setText] = useState(pretty(current ?? { model: "gpt-4o-mini", temperature: 0.2, system_prompt: "You are a helpful agent." }));
  const [comment, setComment] = useState("");
  const save = useAction((body: { config: unknown; comment: string }) => api(`/api/v1/agents/${agentId}/configs`, { method: "POST", body }));
  const parsed = parseJSON(text);
  const isObject = parsed.ok && typeof parsed.value === "object" && parsed.value !== null && !Array.isArray(parsed.value);

  return (
    <div className="grid grid-cols-1 gap-6 lg:grid-cols-5">
      <div className="card p-5 lg:col-span-3">
        <h3 className="font-semibold text-white">Runtime configuration</h3>
        <p className="mb-4 mt-1 text-sm text-ink-300">Pushed live to every instance of this agent; read it in your graph with <code className="text-brand-300">deck.config</code>. Each save is a new version.</p>
        <textarea aria-label="Configuration JSON" rows={14} className="input font-mono text-xs" value={text} onChange={(e) => setText(e.target.value)} readOnly={!canEdit} />
        {!isObject && <p className="mt-1 text-xs text-rose-300">{parsed.ok ? "Configuration must be a JSON object" : parsed.error}</p>}
        <Can role="operator">
          <div className="mt-3 flex gap-2">
            <input className="input" placeholder="What changed? (optional)" value={comment} onChange={(e) => setComment(e.target.value)} />
            <button className="btn-primary shrink-0" disabled={!isObject || save.isPending} onClick={() => parsed.ok && save.mutate({ config: parsed.value, comment }, { onSuccess: () => setComment("") })}>
              <Save className="h-4 w-4" />Save & push
            </button>
          </div>
          <div className="mt-3"><ErrorBox error={save.error} /></div>
        </Can>
      </div>
      <div className="card p-5 lg:col-span-2">
        <h3 className="mb-4 font-semibold text-white">History</h3>
        {!configs.data?.length && <p className="text-sm text-ink-400">No versions yet.</p>}
        <ol className="space-y-3">
          {configs.data?.map((c) => (
            <li key={c.version} className="rounded-xl border border-ink-700 p-3">
              <div className="flex items-center justify-between text-sm">
                <span className="font-medium text-white">v{c.version}</span>
                <button className="text-xs text-brand-300 hover:text-brand-200" onClick={() => setText(pretty(c.config))}>Load</button>
              </div>
              <div className="mt-1 text-xs text-ink-400">{c.created_by} · {timeAgo(c.created_at)}</div>
              {c.comment && <div className="mt-1 text-sm text-ink-200">{c.comment}</div>}
            </li>
          ))}
        </ol>
      </div>
    </div>
  );
}

function AccessTab({ agent }: { agent: Agent }) {
  const graph = useAgentGraph();
  const idOf = (name: string) => graph.data?.nodes.find((n) => n.name === name && n.registered)?.id;
  const usedBy = graph.data?.edges.filter((e) => e.target === agent.name).map((e) => e.source) ?? [];
  const agentLink = (name: string) => {
    const id = idOf(name);
    return id ? <Link to={`/agents/${id}`} className="text-brand-300 hover:underline">{name}</Link> : <span title="Not registered yet">{name}</span>;
  };
  const chips = (names: string[], empty: string) =>
    names.length ? (
      <ul className="flex flex-wrap gap-2">
        {names.map((n) => <li key={n} className="rounded-lg border border-ink-700 bg-ink-900 px-2.5 py-1 font-mono text-xs">{agentLink(n)}</li>)}
      </ul>
    ) : <p className="text-sm text-ink-400">{empty}</p>;
  return (
    <div className="space-y-6">
      <div className="card overflow-x-auto">
        <table className="w-full min-w-[560px]">
          <thead className="border-b border-ink-800"><tr><th className="table-head">Resource</th><th className="table-head">Access</th><th className="table-head">Why</th></tr></thead>
          <tbody className="divide-y divide-ink-800">
            {agent.accesses.map((x) => (
              <tr key={x.resource}>
                <td className="table-cell font-mono text-xs">{x.resource}</td>
                <td className="table-cell"><AccessModes modes={x.modes} /></td>
                <td className="table-cell text-ink-300">{x.description || "—"}</td>
              </tr>
            ))}
          </tbody>
        </table>
        {!agent.accesses.length && (
          <p className="p-6 text-sm text-ink-400">
            This agent has not declared any accesses. Pass <code className="text-brand-300">accesses=&#123;"postgres:orders": ["read", "write"]&#125;</code> to <code className="text-brand-300">LangDeck(...)</code>. They are self-reported, not enforced.
          </p>
        )}
      </div>
      <div className="grid gap-6 md:grid-cols-2">
        <div><h3 className="label">Depends on</h3>{chips(agent.depends_on, "Calls no other agents.")}</div>
        <div><h3 className="label">Used by</h3>{chips(usedBy, "No agent declares a dependency on this one.")}</div>
      </div>
      <Link to="/dependencies" className="inline-block text-sm text-brand-300 hover:underline">See the whole dependency graph →</Link>
    </div>
  );
}

const RUNS_TAB_PAGE = 25;

/** Avatar that operators can click to upload a new picture (admins and operators only). */
function AvatarEditor({ agent }: { agent: Agent }) {
  const qc = useQueryClient();
  const input = useRef<HTMLInputElement>(null);
  const [err, setErr] = useState("");
  const canEdit = useCan("operator");
  const upload = useMutation({
    mutationFn: async (file: File) => uploadAgentImage(agent.id, await shrinkImage(file)),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["agents"] }),
    onError: (e) => setErr(e instanceof Error ? e.message : "Upload failed"),
  });
  const remove = useAction(() => api(`/api/v1/agents/${agent.id}/image`, { method: "DELETE" }));
  return (
    <span className="relative inline-flex items-center gap-2 text-sm font-normal">
      <AgentAvatar agent={agent} className="h-12 w-12" />
      {canEdit && (
        <>
          <input
            ref={input}
            type="file"
            accept="image/png,image/jpeg,image/webp,image/gif"
            hidden
            aria-label="Upload agent picture"
            onChange={(e) => {
              const f = e.target.files?.[0];
              e.target.value = "";
              if (f) { setErr(""); upload.mutate(f); }
            }}
          />
          <button type="button" className="text-xs text-brand-300 hover:underline" onClick={() => input.current?.click()} disabled={upload.isPending}>
            {upload.isPending ? "Uploading…" : agent.image_updated_at ? "Change" : "Add picture"}
          </button>
          {agent.image_updated_at && <button type="button" className="text-xs text-ink-400 hover:underline" onClick={() => remove.mutate(undefined)}>Remove</button>}
        </>
      )}
      {err && <span role="alert" className="text-xs text-red-400">{err}</span>}
    </span>
  );
}

export function AgentDetailPage() {
  const { id = "" } = useParams();
  const nav = useNavigate();
  const { data, isLoading, error } = useAgent(id);
  const pager = useCursorPager();
  const runs = useRunsPage({ agent_id: id, limit: RUNS_TAB_PAGE, before: pager.cursor });
  const commands = useAgentCommands(id);
  const [tab, setTab] = useState<Tab>("graph");
  const [runOpen, setRunOpen] = useState(false);
  const toggle = useAction((action: "pause" | "resume") => api(`/api/v1/agents/${id}/${action}`, { method: "POST" }));
  const remove = useAction(() => api(`/api/v1/agents/${id}`, { method: "DELETE" }));

  if (isLoading) return <Loading />;
  if (error || !data) return <ErrorBox error={error ?? "Agent not found"} />;
  const a = data.agent;
  const paused = a.desired_state === "paused";

  return (
    <>
      <PageHeader
        title={<span className="flex items-center gap-3"><AvatarEditor agent={a} />{a.name}<StatusBadge kind="agent" status={a.status} /></span>}
        subtitle={`${a.framework}${a.version ? ` v${a.version}` : ""} · ${a.instances} instance(s) · SDK ${a.sdk_version || "?"} · heartbeat ${timeAgo(a.last_heartbeat)}`}
        actions={
          <Can role="operator">
            <button className="btn-ghost" onClick={() => toggle.mutate(paused ? "resume" : "pause")} disabled={toggle.isPending}>
              {paused ? <><Play className="h-4 w-4" />Resume</> : <><Pause className="h-4 w-4" />Pause</>}
            </button>
            <button className="btn-primary" onClick={() => setRunOpen(true)} disabled={paused}><Rocket className="h-4 w-4" />Start run</button>
            <Can role="admin">
              <button
                className="btn-danger"
                aria-label="Delete agent"
                onClick={() => confirm(`Delete ${a.name} and all of its runs? This cannot be undone.`) && remove.mutate(undefined, { onSuccess: () => nav("/agents") })}
              >
                <Trash2 className="h-4 w-4" />
              </button>
            </Can>
          </Can>
        }
      />
      {a.description && <p className="-mt-3 mb-6 max-w-3xl text-sm text-ink-300">{a.description}</p>}
      <ErrorBox error={toggle.error ?? remove.error} />

      <Tabs<Tab>
        value={tab}
        onChange={setTab}
        tabs={[
          { id: "graph", label: "Graph" },
          { id: "access", label: `Access${a.accesses.length ? ` (${a.accesses.length})` : ""}` },
          { id: "runs", label: "Runs" },
          { id: "config", label: "Configuration" },
          { id: "commands", label: "Commands" },
        ]}
      />
      {tab === "graph" && (
        <div className="card p-4">
          <GraphView graph={a.graph} height={360} />
        </div>
      )}
      {tab === "access" && <AccessTab agent={a} />}
      {tab === "runs" && (
        <>
          <div className="card overflow-hidden"><RunsTable runs={runs.data?.runs} showAgent={false} /></div>
          <Pager page={pager.page} pageSize={RUNS_TAB_PAGE} shown={runs.data?.runs.length ?? 0} nextCursor={runs.data?.next_before} onNext={pager.next} onPrev={pager.prev} busy={runs.isFetching} noun="runs" />
        </>
      )}
      {tab === "config" && <ConfigTab agentId={id} current={data.config?.config} />}
      {tab === "commands" && (
        <div className="card overflow-x-auto">
          <table className="w-full min-w-[600px]">
            <thead className="border-b border-ink-800"><tr><th className="table-head">Command</th><th className="table-head">Status</th><th className="table-head">By</th><th className="table-head">Result</th><th className="table-head text-right">Created</th></tr></thead>
            <tbody className="divide-y divide-ink-800">
              {commands.data?.map((c) => (
                <tr key={c.id}>
                  <td className="table-cell font-mono text-xs">{c.type}</td>
                  <td className="table-cell">{c.status}</td>
                  <td className="table-cell text-ink-300">{c.created_by}</td>
                  <td className="table-cell text-ink-300">{c.result || "—"}</td>
                  <td className="table-cell text-right text-ink-400">{timeAgo(c.created_at)}</td>
                </tr>
              ))}
            </tbody>
          </table>
          {!commands.data?.length && <p className="p-6 text-sm text-ink-400">No commands sent yet.</p>}
        </div>
      )}
      {a.labels && Object.keys(a.labels).length > 0 && (
        <div className="mt-6">
          <h3 className="label">Labels</h3>
          <JsonView value={a.labels} />
        </div>
      )}
      <StartRunModal agentId={id} open={runOpen} onClose={() => setRunOpen(false)} />
    </>
  );
}
