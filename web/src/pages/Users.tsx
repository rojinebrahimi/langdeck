import { useState } from "react";
import { UserPlus } from "lucide-react";
import { useAction, useMe, useUsers } from "../api/hooks";
import { api } from "../api/client";
import type { Role } from "../api/types";
import { ErrorBox, Loading, Modal, PageHeader } from "../components/ui";
import { timeAgo } from "../lib/format";

function NewUser({ open, onClose }: { open: boolean; onClose: () => void }) {
  const [username, setUsername] = useState("");
  const [role, setRole] = useState<Role>("viewer");
  const [password, setPassword] = useState("");
  const create = useAction((body: object) => api("/api/v1/users", { method: "POST", body }));
  return (
    <Modal open={open} onClose={onClose} title="New user">
      <form
        className="space-y-4"
        onSubmit={(e) => {
          e.preventDefault();
          create.mutate({ username, role, password }, { onSuccess: () => { setUsername(""); setPassword(""); onClose(); } });
        }}
      >
        <div><label className="label" htmlFor="u-name">Username</label><input id="u-name" required className="input" value={username} onChange={(e) => setUsername(e.target.value)} /></div>
        <div>
          <label className="label" htmlFor="u-role">Role</label>
          <select id="u-role" className="input" value={role} onChange={(e) => setRole(e.target.value as Role)}>
            <option value="viewer">viewer</option><option value="operator">operator</option><option value="admin">admin</option>
          </select>
        </div>
        <div>
          <label className="label" htmlFor="u-pass">Initial password</label>
          <input id="u-pass" type="password" minLength={10} required className="input" value={password} onChange={(e) => setPassword(e.target.value)} />
          <p className="mt-1 text-xs text-ink-400">They must choose their own password at first sign-in.</p>
        </div>
        <div className="flex justify-end gap-2"><button type="button" className="btn-ghost" onClick={onClose}>Cancel</button><button className="btn-primary" disabled={create.isPending}>Create user</button></div>
        <ErrorBox error={create.error} />
      </form>
    </Modal>
  );
}

export function UsersPage() {
  const { data, isLoading, error } = useUsers();
  const { data: me } = useMe();
  const [open, setOpen] = useState(false);
  const update = useAction(({ id, role }: { id: string; role: Role }) => api(`/api/v1/users/${id}`, { method: "PATCH", body: { role } }));
  return (
    <>
      <PageHeader
        title="Users"
        subtitle={me?.auth_mode === "local" ? "Username and password accounts." : "People who signed in through your identity provider. Group mappings override roles at each login."}
        actions={me?.auth_mode === "local" && <button className="btn-primary" onClick={() => setOpen(true)}><UserPlus className="h-4 w-4" />New user</button>}
      />
      <ErrorBox error={error ?? update.error} />
      <div className="card overflow-x-auto">
        {isLoading ? <Loading /> : (
          <table className="w-full min-w-[600px]">
            <thead className="border-b border-ink-800"><tr><th className="table-head">User</th><th className="table-head">Role</th><th className="table-head">Last login</th><th className="table-head">Joined</th></tr></thead>
            <tbody className="divide-y divide-ink-800">
              {data?.map((u) => (
                <tr key={u.id}>
                  <td className="table-cell"><div className="font-medium text-white">{u.name || u.email}</div><div className="text-xs text-ink-400">{u.local ? "local account" : u.email}</div></td>
                  <td className="table-cell">
                    <select aria-label={`Role for ${u.email}`} className="input w-auto py-1" value={u.role} disabled={u.id === me?.id} onChange={(e) => update.mutate({ id: u.id, role: e.target.value as Role })}>
                      <option value="viewer">viewer</option><option value="operator">operator</option><option value="admin">admin</option>
                    </select>
                  </td>
                  <td className="table-cell text-ink-300">{timeAgo(u.last_login_at)}</td>
                  <td className="table-cell text-ink-300">{new Date(u.created_at).toLocaleDateString()}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
      <NewUser open={open} onClose={() => setOpen(false)} />
    </>
  );
}
