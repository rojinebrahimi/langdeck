import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Bell, CheckCircle2, Send, XCircle } from "lucide-react";
import clsx from "clsx";
import { useAction } from "../api/hooks";
import { api } from "../api/client";
import { ErrorBox, Loading, PageHeader } from "../components/ui";

interface View {
  events: string[];
  available_events: string[];
  channels: Record<string, boolean>;
  telegram_chat_id: string;
  source: "ui" | "env";
}
interface TestResult { channel: string; ok: boolean; error?: string }

const EVENT_LABEL: Record<string, string> = {
  approval: "A run is waiting for approval",
  run_failed: "A run fails",
  agent_offline: "An agent goes offline",
  agent_online: "An agent comes back online",
};

type Channel = { id: string; name: string; field: string; placeholder: string; help: string };
const URL_CHANNELS: Channel[] = [
  { id: "slack", name: "Slack", field: "slack_url", placeholder: "https://hooks.slack.com/services/…", help: "Incoming webhook URL (Slack app → Incoming Webhooks)." },
  { id: "mattermost", name: "Mattermost", field: "mattermost_url", placeholder: "https://mattermost.example.com/hooks/…", help: "Incoming webhook URL (Integrations → Incoming Webhooks)." },
  { id: "teams", name: "Microsoft Teams", field: "teams_url", placeholder: "https://…webhook.office.com/… or a Workflows URL", help: "Incoming webhook or Power Automate Workflows URL for the channel." },
];

function Badge({ on }: { on: boolean }) {
  return (
    <span className={clsx("rounded-full px-2 py-0.5 text-xs font-medium", on ? "bg-emerald-500/15 text-emerald-300" : "bg-ink-800 text-ink-400")}>
      {on ? "Connected" : "Not set"}
    </span>
  );
}

export function NotificationsPage() {
  const qc = useQueryClient();
  const q = useQuery({ queryKey: ["settings", "notifications"], queryFn: () => api<View>("/api/v1/settings/notifications") });
  const [draft, setDraft] = useState<Record<string, string>>({});
  const [events, setEvents] = useState<string[] | null>(null);
  const [results, setResults] = useState<TestResult[] | null>(null);
  const saveReq = useAction((body: object) => api<View>("/api/v1/settings/notifications", { method: "PUT", body }));
  const save = {
    ...saveReq,
    mutate: (body: object) =>
      saveReq.mutate(body, {
        onSuccess: (v) => {
          qc.setQueryData(["settings", "notifications"], v);
          setDraft({});
          setEvents(null);
        },
      }),
  };
  const test = useAction(() => api<{ results: TestResult[] }>("/api/v1/settings/notifications/test", { method: "POST" }));

  if (q.isLoading) return <Loading />;
  if (q.error || !q.data) return <ErrorBox error={q.error ?? "Could not load settings"} />;
  const v = q.data;
  const selected = events ?? v.events;
  const dirty = Object.values(draft).some((x) => x.trim() !== "") || events !== null;
  const set = (k: string, val: string) => setDraft((d) => ({ ...d, [k]: val }));
  const submit = () => save.mutate({ ...draft, ...(events ? { events } : {}) });
  const clear = (id: string) => confirm(`Disconnect ${id}? Notifications to it stop immediately.`) && save.mutate({ clear: [id] });

  return (
    <>
      <PageHeader
        title="Notifications"
        subtitle="Send approvals, failed runs and agent outages to your chat. Changes apply immediately."
        actions={
          <button className="btn-ghost" disabled={!Object.values(v.channels).some(Boolean) || test.isPending}
            onClick={() => { setResults(null); test.mutate(undefined, { onSuccess: (r) => setResults(r.results) }); }}>
            <Send className="h-4 w-4" />{test.isPending ? "Sending…" : "Send test message"}
          </button>
        }
      />
      <ErrorBox error={save.error ?? test.error} />
      {v.source === "env" && Object.values(v.channels).some(Boolean) && (
        <p className="mb-4 rounded-xl border border-ink-700 bg-ink-850 p-3 text-sm text-ink-300">
          These channels come from <code className="font-mono">LANGDECK_NOTIFY_*</code> environment variables. Saving here overrides them.
        </p>
      )}
      {results && (
        <ul className="mb-4 space-y-1 rounded-xl border border-ink-700 bg-ink-850 p-3 text-sm" aria-label="Test results">
          {results.map((r) => (
            <li key={r.channel} className="flex items-center gap-2">
              {r.ok ? <CheckCircle2 className="h-4 w-4 text-emerald-400" /> : <XCircle className="h-4 w-4 text-rose-400" />}
              <span className="font-medium text-white">{r.channel}</span>
              <span className={r.ok ? "text-emerald-300" : "text-rose-300"}>{r.ok ? "delivered" : r.error}</span>
            </li>
          ))}
        </ul>
      )}

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        {URL_CHANNELS.map((c) => (
          <section key={c.id} className="card p-5">
            <div className="mb-3 flex items-center justify-between">
              <h2 className="flex items-center gap-2 font-semibold text-white"><Bell className="h-4 w-4 text-brand-300" />{c.name}</h2>
              <Badge on={v.channels[c.id]} />
            </div>
            <label className="label" htmlFor={`ch-${c.id}`}>{v.channels[c.id] ? "Replace webhook URL" : "Webhook URL"}</label>
            <input id={`ch-${c.id}`} type="url" autoComplete="off" className="input" placeholder={v.channels[c.id] ? "•••••••• (saved, hidden)" : c.placeholder}
              value={draft[c.field] ?? ""} onChange={(e) => set(c.field, e.target.value)} />
            <p className="mt-2 text-xs text-ink-400">{c.help}</p>
            {v.channels[c.id] && <button type="button" className="mt-3 text-xs text-rose-300 hover:underline" onClick={() => clear(c.id)}>Disconnect</button>}
          </section>
        ))}

        <section className="card p-5">
          <div className="mb-3 flex items-center justify-between">
            <h2 className="flex items-center gap-2 font-semibold text-white"><Bell className="h-4 w-4 text-brand-300" />Telegram</h2>
            <Badge on={v.channels.telegram} />
          </div>
          <label className="label" htmlFor="tg-token">{v.channels.telegram ? "Replace bot token" : "Bot token"}</label>
          <input id="tg-token" autoComplete="off" className="input font-mono" placeholder={v.channels.telegram ? "•••••••• (saved, hidden)" : "123456:ABC-DEF…"}
            value={draft.telegram_token ?? ""} onChange={(e) => set("telegram_token", e.target.value)} />
          <label className="label mt-3" htmlFor="tg-chat">Chat ID</label>
          <input id="tg-chat" autoComplete="off" className="input font-mono" placeholder={v.telegram_chat_id || "-1001234567890"}
            value={draft.telegram_chat_id ?? ""} onChange={(e) => set("telegram_chat_id", e.target.value)} />
          <p className="mt-2 text-xs text-ink-400">Create a bot with @BotFather, add it to the chat, and use the chat or channel id.</p>
          {v.channels.telegram && <button type="button" className="mt-3 text-xs text-rose-300 hover:underline" onClick={() => clear("telegram")}>Disconnect</button>}
        </section>
      </div>

      <section className="card mt-4 p-5">
        <h2 className="mb-3 font-semibold text-white">Notify me when…</h2>
        <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
          {v.available_events.map((k) => (
            <label key={k} className="flex items-center gap-2 text-sm text-ink-200">
              <input type="checkbox" className="h-4 w-4 accent-purple-500" checked={selected.includes(k)}
                onChange={(e) => setEvents(e.target.checked ? [...selected, k] : selected.filter((x) => x !== k))} />
              {EVENT_LABEL[k] ?? k}
            </label>
          ))}
        </div>
        <p className="mt-3 text-xs text-ink-400">Messages carry names, ids and a link, never prompts, inputs or outputs. Webhook URLs and tokens are stored on the server and never shown again.</p>
      </section>

      <div className="mt-4 flex justify-end gap-2">
        <button className="btn-ghost" disabled={!dirty} onClick={() => { setDraft({}); setEvents(null); }}>Discard</button>
        <button className="btn-primary" disabled={!dirty || save.isPending} onClick={submit}>{save.isPending ? "Saving…" : "Save changes"}</button>
      </div>
    </>
  );
}
