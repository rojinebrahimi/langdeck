import { useState, type FormEvent } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { KeyRound } from "lucide-react";
import { api } from "../api/client";
import { ErrorBox, Spinner } from "../components/ui";

export function ChangePasswordForm({ forced, onDone }: { forced?: boolean; onDone?: () => void }) {
  const qc = useQueryClient();
  const [current, setCurrent] = useState("");
  const [next, setNext] = useState("");
  const [confirm, setConfirm] = useState("");
  const [err, setErr] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);
  const mismatch = confirm.length > 0 && next !== confirm;

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (mismatch) return;
    setBusy(true);
    setErr(null);
    try {
      await api("/api/v1/me/password", { method: "POST", body: { current_password: current, new_password: next } });
      await qc.invalidateQueries();
      onDone?.();
    } catch (e) {
      setErr(e);
    } finally {
      setBusy(false);
    }
  };

  return (
    <form onSubmit={submit} className="space-y-4">
      {forced && (
        <div className="rounded-xl border border-amber-400/30 bg-amber-400/5 px-3 py-2 text-xs text-amber-200">
          You are using an initial password. Choose a new one to continue.
        </div>
      )}
      <div>
        <label className="label" htmlFor="pw-current">Current password</label>
        <input id="pw-current" type="password" autoComplete="current-password" required className="input" value={current} onChange={(e) => setCurrent(e.target.value)} />
      </div>
      <div>
        <label className="label" htmlFor="pw-new">New password</label>
        <input id="pw-new" type="password" autoComplete="new-password" minLength={10} required className="input" value={next} onChange={(e) => setNext(e.target.value)} />
        <p className="mt-1 text-xs text-ink-400">At least 10 characters.</p>
      </div>
      <div>
        <label className="label" htmlFor="pw-confirm">Confirm new password</label>
        <input id="pw-confirm" type="password" autoComplete="new-password" required className="input" value={confirm} onChange={(e) => setConfirm(e.target.value)} />
        {mismatch && <p className="mt-1 text-xs text-rose-300">Passwords do not match.</p>}
      </div>
      <ErrorBox error={err} />
      <button type="submit" className="btn-primary w-full" disabled={busy || mismatch}>
        {busy ? <Spinner className="text-white" /> : "Update password"}
      </button>
    </form>
  );
}

export function ForcedPasswordChangePage() {
  return (
    <div className="flex min-h-full items-center justify-center p-6">
      <div className="w-full max-w-sm">
        <div className="mb-6 flex flex-col items-center text-center">
          <div className="mb-3 rounded-2xl bg-brand-500/15 p-3 text-brand-300 ring-1 ring-brand-400/30"><KeyRound className="h-6 w-6" /></div>
          <h1 className="text-xl font-semibold text-white">Set your password</h1>
        </div>
        <div className="card p-6">
          <ChangePasswordForm forced />
        </div>
      </div>
    </div>
  );
}
