import { useState } from "react";
import { Copy, KeyRound, Plus } from "lucide-react";
import { useAction, useAPIKeys } from "../api/hooks";
import { api } from "../api/client";
import type { APIKey, Role } from "../api/types";
import { Empty, ErrorBox, Loading, Modal, PageHeader } from "../components/ui";
import { timeAgo } from "../lib/format";

function CreateKey({ open, onClose }: { open: boolean; onClose: () => void }) {
  const [name, setName] = useState("");
  const [role, setRole] = useState<Role>("agent");
  const [agent, setAgent] = useState("");
  const [days, setDays] = useState(90);
  const [secret, setSecret] = useState("");
  const create = useAction((body: object) => api<{ secret: string; api_key: APIKey }>("/api/v1/api-keys", { method: "POST", body }));
  const close = () => {
    setSecret("");
    setName("");
    setAgent("");
    create.reset();
    onClose();
  };
  return (
    <Modal open={open} onClose={close} title={secret ? "Copy your key now" : "New API key"}>
      {secret ? (
        <div className="space-y-4">
          <p className="text-sm text-ink-300">This is the only time the key is shown. LangDeck stores just a hash of it.</p>
          <div className="flex items-center gap-2 rounded-xl border border-brand-400/40 bg-ink-950 p-3">
            <code className="flex-1 break-all font-mono text-xs text-brand-200">{secret}</code>
            <button className="btn-ghost px-2" onClick={() => navigator.clipboard?.writeText(secret)} aria-label="Copy key"><Copy className="h-4 w-4" /></button>
          </div>
          <pre className="rounded-xl bg-ink-950 p-3 font-mono text-xs text-ink-300">{`export LANGDECK_URL=${window.location.origin}\nexport LANGDECK_API_KEY=${secret.slice(0, 12)}…`}</pre>
          <div className="flex justify-end"><button className="btn-primary" onClick={close}>Done</button></div>
        </div>
      ) : (
        <form
          className="space-y-4"
          onSubmit={(e) => {
            e.preventDefault();
            create.mutate({ name, role, agent_name: role === "agent" ? agent : "", expires_in_days: days }, { onSuccess: (r) => setSecret(r.secret) });
          }}
        >
          <div><label className="label" htmlFor="k-name">Name</label><input id="k-name" required className="input" placeholder="support-bot production" value={name} onChange={(e) => setName(e.target.value)} /></div>
          <div className="grid grid-cols-2 gap-3">
            <div>
              <label className="label" htmlFor="k-role">Role</label>
              <select id="k-role" className="input" value={role} onChange={(e) => setRole(e.target.value as Role)}>
                <option value="agent">agent (SDK)</option><option value="viewer">viewer</option><option value="operator">operator</option><option value="admin">admin</option>
              </select>
            </div>
            <div>
              <label className="label" htmlFor="k-exp">Expires in (days)</label>
              <input id="k-exp" type="number" min={0} max={3650} className="input" value={days} onChange={(e) => setDays(Number(e.target.value))} />
            </div>
          </div>
          {role === "agent" && (
            <div>
              <label className="label" htmlFor="k-agent">Restrict to agent (recommended)</label>
              <input id="k-agent" className="input" placeholder="support-bot" value={agent} onChange={(e) => setAgent(e.target.value)} />
            </div>
          )}
          <ErrorBox error={create.error} />
          <div className="flex justify-end gap-2"><button type="button" className="btn-ghost" onClick={close}>Cancel</button><button className="btn-primary" disabled={create.isPending}>Create key</button></div>
        </form>
      )}
    </Modal>
  );
}

export function APIKeysPage() {
  const { data, isLoading, error } = useAPIKeys();
  const revoke = useAction((id: string) => api(`/api/v1/api-keys/${id}`, { method: "DELETE" }));
  const [open, setOpen] = useState(false);
  return (
    <>
      <PageHeader title="API keys" subtitle="Credentials for agents (SDK) and automation. Keys are shown once and stored hashed." actions={<button className="btn-primary" onClick={() => setOpen(true)}><Plus className="h-4 w-4" />New key</button>} />
      <ErrorBox error={error ?? revoke.error} />
      <div className="card overflow-x-auto">
        {isLoading ? <Loading /> : !data?.length ? <Empty title="No API keys" icon={<KeyRound className="h-6 w-6" />} /> : (
          <table className="w-full min-w-[720px]">
            <thead className="border-b border-ink-800"><tr><th className="table-head">Name</th><th className="table-head">Key</th><th className="table-head">Role</th><th className="table-head">Last used</th><th className="table-head">Expires</th><th className="table-head" /></tr></thead>
            <tbody className="divide-y divide-ink-800">
              {data.map((k) => (
                <tr key={k.id} className={k.revoked_at ? "opacity-50" : ""}>
                  <td className="table-cell font-medium text-white">{k.name}<div className="text-xs font-normal text-ink-400">by {k.created_by}</div></td>
                  <td className="table-cell font-mono text-xs">{k.prefix}…</td>
                  <td className="table-cell">{k.role}{k.agent_name && <span className="text-ink-400"> → {k.agent_name}</span>}</td>
                  <td className="table-cell text-ink-300">{timeAgo(k.last_used_at)}</td>
                  <td className="table-cell text-ink-300">{k.expires_at ? new Date(k.expires_at).toLocaleDateString() : "never"}</td>
                  <td className="table-cell text-right">
                    {k.revoked_at ? <span className="text-xs text-ink-400">revoked</span> : (
                      <button className="btn-danger px-2.5 py-1 text-xs" onClick={() => confirm(`Revoke "${k.name}"? Agents using it disconnect immediately.`) && revoke.mutate(k.id)}>Revoke</button>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
      <CreateKey open={open} onClose={() => setOpen(false)} />
    </>
  );
}
