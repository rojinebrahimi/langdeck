import { useState, type ReactNode } from "react";
import { Link, NavLink } from "react-router-dom";
import clsx from "clsx";
import { useQueryClient } from "@tanstack/react-query";
import { BookOpen, Bot, Hand, Network, KeyRound, LayoutDashboard, Lock, Bell, LogOut, Menu, PlayCircle, ScrollText, Users, X } from "lucide-react";
import { useInterrupts, useLiveUpdates, useMe } from "../api/hooks";
import { api } from "../api/client";
import { Can, Modal } from "./ui";
import { ChangePasswordForm } from "../pages/ChangePassword";
import { GitHubLink } from "./GitHubLink";

const nav = [
  { to: "/", label: "Overview", icon: LayoutDashboard, end: true },
  { to: "/agents", label: "Agents", icon: Bot },
  { to: "/dependencies", label: "Dependencies", icon: Network },
  { to: "/runs", label: "Runs", icon: PlayCircle },
  { to: "/approvals", label: "Approvals", icon: Hand, badge: true },
  { to: "/guide", label: "User guide", icon: BookOpen },
];
const admin = [
  { to: "/settings/api-keys", label: "API keys", icon: KeyRound },
  { to: "/settings/users", label: "Users", icon: Users },
  { to: "/settings/notifications", label: "Notifications", icon: Bell },
  { to: "/settings/audit", label: "Audit log", icon: ScrollText },
];

function Item({ to, label, icon: Icon, end, badge, onClick }: { to: string; label: string; icon: typeof Bot; end?: boolean; badge?: number; onClick?: () => void }) {
  return (
    <NavLink
      to={to}
      end={end}
      onClick={onClick}
      className={({ isActive }) =>
        clsx(
          "group flex items-center gap-3 rounded-xl px-3 py-2 text-sm font-medium transition",
          isActive ? "bg-gradient-to-r from-brand-500/25 to-brand-500/5 text-white ring-1 ring-brand-400/30" : "text-ink-300 hover:bg-ink-800 hover:text-white",
        )
      }
    >
      <Icon className="h-4 w-4" />
      <span className="flex-1">{label}</span>
      {!!badge && <span className="rounded-full bg-amber-400/20 px-2 py-0.5 text-xs text-amber-200">{badge}</span>}
    </NavLink>
  );
}

export function Layout({ children }: { children: ReactNode }) {
  const { data: me } = useMe();
  const { data: pending } = useInterrupts("pending");
  const live = useLiveUpdates(!!me);
  const qc = useQueryClient();
  const [open, setOpen] = useState(false);
  const [pwOpen, setPwOpen] = useState(false);

  const logout = async () => {
    await api("/auth/logout", { method: "POST" }).catch(() => undefined);
    qc.clear();
    window.location.assign("/login");
  };

  const sidebar = (
    <nav className="flex h-full flex-col gap-1 p-4">
      <Link to="/" onClick={() => setOpen(false)} aria-label="LangDeck home" title="LangDeck home" className="mb-6 flex items-center gap-2.5 rounded-xl px-2 py-1 transition hover:bg-ink-800/60">
        <img src="/logo.svg" alt="" className="h-8 w-8" />
        <div>
          <div className="font-semibold tracking-tight text-white">LangDeck</div>
          <div className="text-[11px] text-ink-400">Agent control plane</div>
        </div>
      </Link>
      {nav.map((n) => (
        <Item key={n.to} {...n} badge={n.badge ? pending?.length : undefined} onClick={() => setOpen(false)} />
      ))}
      <Can role="admin">
        <div className="mb-1 mt-6 px-3 text-[11px] font-medium uppercase tracking-wider text-ink-500">Administration</div>
        {admin.map((n) => (
          <Item key={n.to} {...n} onClick={() => setOpen(false)} />
        ))}
      </Can>
      <div className="mt-auto mb-3 px-3 text-xs">
        <GitHubLink label="Star LangDeck on GitHub" />
      </div>
      <div className="rounded-xl border border-ink-700 bg-ink-850 p-3">
        <div className="flex items-center gap-3">
          <div className="flex h-8 w-8 items-center justify-center rounded-full bg-brand-500/20 text-sm font-semibold text-brand-300">
            {(me?.name || me?.email || "?").slice(0, 1).toUpperCase()}
          </div>
          <div className="min-w-0 flex-1">
            <div className="truncate text-sm font-medium text-white">{me?.name || me?.email}</div>
            <div className="text-xs capitalize text-ink-400">{me?.role}</div>
          </div>
          {me?.auth_mode === "local" && (
            <button onClick={() => setPwOpen(true)} className="rounded-lg p-1.5 text-ink-300 hover:bg-ink-700 hover:text-white" aria-label="Change password" title="Change password">
              <Lock className="h-4 w-4" />
            </button>
          )}
          <button onClick={logout} className="rounded-lg p-1.5 text-ink-300 hover:bg-ink-700 hover:text-white" aria-label="Sign out" title="Sign out">
            <LogOut className="h-4 w-4" />
          </button>
        </div>
      </div>
    </nav>
  );

  return (
    <div className="flex min-h-full">
      <aside className="sticky top-0 hidden h-screen w-64 shrink-0 border-r border-ink-800 bg-ink-900/60 lg:block">{sidebar}</aside>
      {open && (
        <div className="fixed inset-0 z-40 bg-ink-950/70 lg:hidden" onClick={() => setOpen(false)}>
          <aside className="h-full w-64 border-r border-ink-800 bg-ink-900" onClick={(e) => e.stopPropagation()}>
            {sidebar}
          </aside>
        </div>
      )}
      <div className="min-w-0 flex-1">
        <header className="sticky top-0 z-30 flex h-14 items-center gap-3 border-b border-ink-800 bg-ink-900/95 px-4 lg:px-8">
          <button className="rounded-lg p-1.5 text-ink-200 hover:bg-ink-800 lg:hidden" onClick={() => setOpen(!open)} aria-label="Toggle menu">
            {open ? <X className="h-5 w-5" /> : <Menu className="h-5 w-5" />}
          </button>
          <Link to="/" aria-label="LangDeck home" title="LangDeck home" className="flex items-center gap-2 rounded-lg px-1.5 py-1 transition hover:bg-ink-800 lg:hidden">
            <img src="/logo.svg" alt="" className="h-6 w-6" />
            <span className="text-sm font-semibold tracking-tight text-white">LangDeck</span>
          </Link>
          <div className="flex-1" />
          {me?.auth_mode === "dev" && (
            <span className="rounded-full bg-amber-400/10 px-2.5 py-1 text-xs font-medium text-amber-300 ring-1 ring-amber-400/30">dev auth</span>
          )}
          <GitHubLink className="text-xs" />
          <span className="flex items-center gap-2 text-xs text-ink-300" title={live ? "Receiving live updates" : "Reconnecting…"}>
            <span className={clsx("h-2 w-2 rounded-full", live ? "bg-emerald-400 shadow-[0_0_8px] shadow-emerald-400" : "bg-ink-500")} />
            {live ? "Live" : "Offline"}
          </span>
        </header>
        <main className="mx-auto max-w-7xl px-4 py-8 lg:px-8">{children}</main>
        <Modal open={pwOpen} title="Change password" onClose={() => setPwOpen(false)}>
          <ChangePasswordForm onDone={() => setPwOpen(false)} />
        </Modal>
      </div>
    </div>
  );
}
