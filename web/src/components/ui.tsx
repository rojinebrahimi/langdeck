import { useEffect, type ReactNode } from "react";
import clsx from "clsx";
import { AlertTriangle, Inbox, Loader2, X } from "lucide-react";
import type { AgentStatus, Role, RunStatus } from "../api/types";
import { useMe } from "../api/hooks";

export function PageHeader({ title, subtitle, actions }: { title: ReactNode; subtitle?: ReactNode; actions?: ReactNode }) {
  return (
    <div className="mb-6 flex flex-wrap items-end justify-between gap-4">
      <div className="min-w-0">
        <h1 className="truncate text-2xl font-semibold tracking-tight text-white">{title}</h1>
        {subtitle && <p className="mt-1 text-sm text-ink-300">{subtitle}</p>}
      </div>
      {actions && <div className="flex flex-wrap items-center gap-2">{actions}</div>}
    </div>
  );
}

const agentTone: Record<AgentStatus, string> = {
  online: "bg-emerald-400/10 text-emerald-300 ring-emerald-400/30",
  paused: "bg-amber-400/10 text-amber-300 ring-amber-400/30",
  offline: "bg-ink-600/30 text-ink-300 ring-ink-500/40",
};

const runTone: Record<RunStatus, string> = {
  pending: "bg-ink-600/30 text-ink-200 ring-ink-500/40",
  running: "bg-brand-500/15 text-brand-300 ring-brand-400/40",
  interrupted: "bg-amber-400/10 text-amber-300 ring-amber-400/30",
  succeeded: "bg-emerald-400/10 text-emerald-300 ring-emerald-400/30",
  failed: "bg-rose-500/10 text-rose-300 ring-rose-400/30",
  cancelled: "bg-ink-600/30 text-ink-300 ring-ink-500/40",
};

export function StatusBadge({ status, kind = "run" }: { status: string; kind?: "run" | "agent" }) {
  const tone = kind === "agent" ? agentTone[status as AgentStatus] : runTone[status as RunStatus];
  const live = status === "online" || status === "running";
  return (
    <span className={clsx("inline-flex items-center gap-1.5 rounded-full px-2.5 py-0.5 text-xs font-medium ring-1 ring-inset", tone ?? runTone.pending)}>
      <span className={clsx("h-1.5 w-1.5 rounded-full bg-current", live && "animate-pulse")} />
      {status}
    </span>
  );
}

export function Spinner({ className }: { className?: string }) {
  return <Loader2 className={clsx("h-4 w-4 animate-spin text-brand-400", className)} aria-label="Loading" />;
}

export function Loading() {
  return (
    <div className="flex items-center justify-center py-20">
      <Spinner className="h-6 w-6" />
    </div>
  );
}

export function ErrorBox({ error }: { error: unknown }) {
  if (!error) return null;
  return (
    <div role="alert" className="flex items-start gap-2 rounded-xl border border-rose-500/30 bg-rose-500/10 px-4 py-3 text-sm text-rose-200">
      <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" />
      <span>{error instanceof Error ? error.message : String(error)}</span>
    </div>
  );
}

export function Empty({ title, hint, icon }: { title: string; hint?: ReactNode; icon?: ReactNode }) {
  return (
    <div className="flex flex-col items-center justify-center px-6 py-16 text-center">
      <div className="mb-3 rounded-2xl bg-ink-800 p-3 text-brand-400">{icon ?? <Inbox className="h-6 w-6" />}</div>
      <p className="font-medium text-ink-100">{title}</p>
      {hint && <div className="mt-1 max-w-md text-sm text-ink-300">{hint}</div>}
    </div>
  );
}

export function Stat({ label, value, hint, icon }: { label: string; value: ReactNode; hint?: ReactNode; icon?: ReactNode }) {
  return (
    <div className="card p-5">
      <div className="flex items-center justify-between">
        <span className="text-xs font-medium uppercase tracking-wider text-ink-400">{label}</span>
        <span className="text-brand-400">{icon}</span>
      </div>
      <div className="mt-3 text-3xl font-semibold tracking-tight text-white">{value}</div>
      {hint && <div className="mt-1 text-xs text-ink-300">{hint}</div>}
    </div>
  );
}

export function JsonView({ value, className }: { value: unknown; className?: string }) {
  if (value === undefined || value === null) return <span className="text-sm text-ink-400">—</span>;
  return (
    <pre className={clsx("max-h-96 overflow-auto rounded-xl border border-ink-700 bg-ink-950/70 p-4 font-mono text-xs leading-relaxed text-ink-200", className)}>
      {JSON.stringify(value, null, 2)}
    </pre>
  );
}

export function Modal({ open, title, onClose, children }: { open: boolean; title: string; onClose: () => void; children: ReactNode }) {
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && onClose();
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [open, onClose]);
  if (!open) return null;
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-ink-950/70 p-4 backdrop-blur-sm" onMouseDown={onClose}>
      <div role="dialog" aria-modal="true" aria-label={title} className="card w-full max-w-lg p-6" onMouseDown={(e) => e.stopPropagation()}>
        <div className="mb-4 flex items-center justify-between">
          <h2 className="text-lg font-semibold text-white">{title}</h2>
          <button className="rounded-lg p-1 text-ink-300 hover:bg-ink-700 hover:text-white" onClick={onClose} aria-label="Close">
            <X className="h-4 w-4" />
          </button>
        </div>
        {children}
      </div>
    </div>
  );
}

const rank: Record<Role, number> = { viewer: 1, operator: 2, admin: 3, agent: 0 };

/** Can renders children only when the signed-in user has at least `role`. The server enforces the same rule. */
export function Can({ role, children }: { role: Role; children: ReactNode }) {
  const { data: me } = useMe();
  if (!me || rank[me.role] < rank[role]) return null;
  return <>{children}</>;
}

export function useCan(role: Role): boolean {
  const { data: me } = useMe();
  return !!me && rank[me.role] >= rank[role];
}

export function Tabs<T extends string>({ tabs, value, onChange }: { tabs: { id: T; label: ReactNode }[]; value: T; onChange: (v: T) => void }) {
  return (
    <div role="tablist" className="mb-5 inline-flex rounded-xl border border-ink-700 bg-ink-850 p-1">
      {tabs.map((t) => (
        <button
          key={t.id}
          role="tab"
          aria-selected={value === t.id}
          onClick={() => onChange(t.id)}
          className={clsx(
            "rounded-lg px-3.5 py-1.5 text-sm font-medium transition",
            value === t.id ? "bg-brand-500/20 text-white ring-1 ring-brand-400/40" : "text-ink-300 hover:text-white",
          )}
        >
          {t.label}
        </button>
      ))}
    </div>
  );
}
