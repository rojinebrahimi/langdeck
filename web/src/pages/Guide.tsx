import { useEffect, useState, type ReactNode } from "react";
import { Link } from "react-router-dom";
import { BookOpen, Bot, ExternalLink, KeyRound, PlayCircle, ShieldCheck, SlidersHorizontal } from "lucide-react";
import { PageHeader } from "../components/ui";
import { REPO_URL } from "../components/GitHubLink";

type Entry = { id: string; title: string };

const toc: Entry[] = [
  { id: "tour", title: "A tour of the console" },
  { id: "connect", title: "Connect an agent" },
  { id: "runs", title: "Start and follow a run" },
  { id: "control", title: "Control an agent" },
  { id: "approvals", title: "Approve an action" },
  { id: "config", title: "Push configuration" },
  { id: "access", title: "Roles, keys and accounts" },
  { id: "troubleshooting", title: "Troubleshooting" },
];

function Section({ id, title, children }: { id: string; title: string; children: ReactNode }) {
  return (
    <section id={id} className="scroll-mt-24">
      <h2 className="mb-3 text-lg font-semibold tracking-tight text-white">{title}</h2>
      <div className="space-y-3 text-sm leading-relaxed text-ink-200">{children}</div>
    </section>
  );
}

function Code({ children }: { children: string }) {
  return (
    <pre className="overflow-x-auto rounded-xl border border-ink-700 bg-ink-950/80 p-4 text-xs leading-relaxed text-ink-200">
      <code>{children}</code>
    </pre>
  );
}

function Steps({ items }: { items: ReactNode[] }) {
  return (
    <ol className="space-y-2">
      {items.map((item, i) => (
        <li key={i} className="flex gap-3">
          <span className="mt-0.5 flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-brand-500/20 text-[11px] font-semibold text-brand-300 ring-1 ring-brand-400/30">
            {i + 1}
          </span>
          <span className="min-w-0 flex-1">{item}</span>
        </li>
      ))}
    </ol>
  );
}

function Callout({ children }: { children: ReactNode }) {
  return <div className="rounded-xl border border-brand-400/25 bg-brand-500/10 px-4 py-3 text-sm text-brand-100">{children}</div>;
}

export function GuidePage() {
  const [active, setActive] = useState(toc[0].id);

  useEffect(() => {
    if (typeof IntersectionObserver === "undefined") return;
    const observer = new IntersectionObserver(
      (entries) => {
        const visible = entries.filter((e) => e.isIntersecting).sort((a, b) => a.boundingClientRect.top - b.boundingClientRect.top);
        if (visible[0]) setActive(visible[0].target.id);
      },
      { rootMargin: "-96px 0px -65% 0px", threshold: 0 },
    );
    for (const t of toc) {
      const el = document.getElementById(t.id);
      if (el) observer.observe(el);
    }
    return () => observer.disconnect();
  }, []);

  return (
    <div>
      <PageHeader
        title={<span className="flex items-center gap-2"><BookOpen className="h-5 w-5 text-brand-300" />User guide</span>}
        subtitle="What every screen does, and how to run your agents from it."
        actions={
          <a href={`${REPO_URL}/blob/main/docs/user-guide.md`} target="_blank" rel="noreferrer" className="btn-ghost">
            Full guide <ExternalLink className="h-4 w-4" />
          </a>
        }
      />

      <div className="grid gap-8 lg:grid-cols-[220px_minmax(0,1fr)]">
        <nav className="hidden lg:block">
          <div className="sticky top-24 space-y-1">
            {toc.map((t) => (
              <a
                key={t.id}
                href={`#${t.id}`}
                onClick={() => setActive(t.id)}
                className={
                  active === t.id
                    ? "block rounded-lg bg-brand-500/15 px-3 py-1.5 text-sm text-brand-200 ring-1 ring-brand-400/30"
                    : "block rounded-lg px-3 py-1.5 text-sm text-ink-300 transition hover:bg-ink-800 hover:text-white"
                }
              >
                {t.title}
              </a>
            ))}
          </div>
        </nav>

        <div className="card max-w-3xl space-y-10 p-6 sm:p-8">
          <Section id="tour" title="A tour of the console">
            <p>Five screens cover the whole workflow:</p>
            <ul className="space-y-2">
              <li className="flex gap-2"><Bot className="mt-0.5 h-4 w-4 shrink-0 text-brand-300" /><span><Link to="/agents" className="text-brand-300 hover:underline">Agents</Link> — every agent that has registered, its graph, live status, config and controls.</span></li>
              <li className="flex gap-2"><PlayCircle className="mt-0.5 h-4 w-4 shrink-0 text-brand-300" /><span><Link to="/runs" className="text-brand-300 hover:underline">Runs</Link> — every execution, with a trace of nodes, LLM calls and tools. Open one to replay it on the graph.</span></li>
              <li className="flex gap-2"><ShieldCheck className="mt-0.5 h-4 w-4 shrink-0 text-brand-300" /><span><Link to="/approvals" className="text-brand-300 hover:underline">Approvals</Link> — questions from agents waiting on a human. This is your inbox.</span></li>
              <li className="flex gap-2"><SlidersHorizontal className="mt-0.5 h-4 w-4 shrink-0 text-brand-300" /><span><Link to="/" className="text-brand-300 hover:underline">Overview</Link> — health of the fleet over the last 24 hours.</span></li>
              <li className="flex gap-2"><KeyRound className="mt-0.5 h-4 w-4 shrink-0 text-brand-300" /><span><Link to="/settings/api-keys" className="text-brand-300 hover:underline">Settings</Link> — API keys, users and the audit log (admins only).</span></li>
            </ul>
          </Section>

          <Section id="connect" title="Connect an agent">
            <Steps
              items={[
                <>Create an <b>agent</b> key under Settings → API keys. It is shown once — copy it.</>,
                <>Set <span className="font-mono text-xs">LANGDECK_URL</span> and <span className="font-mono text-xs">LANGDECK_API_KEY</span> in the agent's environment.</>,
                <>Wrap your compiled graph and start the client — the agent appears here within seconds, status <b>online</b>.</>,
              ]}
            />
            <Code>{`from langdeck import LangDeck

deck = LangDeck(name="support-bot", version="1.0.0")
app = deck.instrument(builder.compile())   # your existing StateGraph
deck.start()                               # register + listen for commands

app.invoke({"messages": [("user", "Where is my order?")]})`}</Code>
            <Callout>Agents only make outbound HTTPS calls. Nothing needs an inbound port, so they work behind NAT, private subnets and firewalls.</Callout>
          </Section>

          <Section id="runs" title="Start and follow a run">
            <p>
              Open an agent and choose <b>Start run</b>. The JSON you type is handed to your graph (or to
              your <span className="font-mono text-xs">input_mapper</span>). The new run opens immediately and
              fills in live: the graph panel highlights the executed path and the timeline shows each node,
              LLM call and tool call with latency and token usage.
            </p>
            <p>
              Runs your code starts itself show up the same way — no extra work, the SDK traces every
              instrumented <span className="font-mono text-xs">invoke</span>.
            </p>
          </Section>

          <Section id="control" title="Control an agent">
            <ul className="space-y-2">
              <li><b>Cancel a run</b> — from the run page. The agent stops at its next node boundary; the run is recorded as cancelled.</li>
              <li><b>Pause / resume</b> — from the agent page. Paused agents refuse new runs but stay registered and keep their graph and config.</li>
              <li><b>Delete</b> — removes the agent and its runs. A process that is still running re-registers on its next heartbeat.</li>
            </ul>
            <p>Commands are delivered at-least-once and de-duplicated by id, so a flaky network cannot double-run anything.</p>
          </Section>

          <Section id="approvals" title="Approve an action">
            <p>
              When a node calls <span className="font-mono text-xs">deck.request_approval("Refund $240?", state)</span> the
              run pauses there and the question lands in Approvals with the payload attached. Approve returns{" "}
              <b>true</b>, Reject returns <b>false</b>, and a note is passed back to the agent. Requests time out
              after an hour and a timeout counts as a rejection, so a run can never wait forever.
            </p>
            <Code>{`def refund(state):
    if not deck.request_approval("Refund $%s?" % state["amount"], payload=state):
        return {"reply": "Escalated to a human."}
    return {"reply": "Refunded."}`}</Code>
          </Section>

          <Section id="config" title="Push configuration">
            <p>
              The Config panel holds arbitrary JSON for an agent, versioned with history and one-click rollback.
              Push a new version and every instance picks it up on its next heartbeat — no redeploy, no restart.
              Typical contents: model name, temperature, prompts, thresholds, feature flags.
            </p>
            <Code>{`model = deck.config.get("model", "gpt-4o-mini")
temperature = float(deck.config.get("temperature", 0.2))`}</Code>
          </Section>

          <Section id="access" title="Roles, keys and accounts">
            <div className="overflow-x-auto">
              <table className="w-full text-left text-sm">
                <thead className="text-xs uppercase tracking-wide text-ink-400">
                  <tr>
                    <th className="py-2 pr-4">Capability</th>
                    <th className="px-2">viewer</th>
                    <th className="px-2">operator</th>
                    <th className="px-2">admin</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-ink-800">
                  <tr><td className="py-2 pr-4">Dashboard, agents, runs, traces</td><td className="px-2">✅</td><td className="px-2">✅</td><td className="px-2">✅</td></tr>
                  <tr><td className="py-2 pr-4">Start / cancel runs, pause, config, approvals</td><td className="px-2">—</td><td className="px-2">✅</td><td className="px-2">✅</td></tr>
                  <tr><td className="py-2 pr-4">API keys, users, audit log, delete agents</td><td className="px-2">—</td><td className="px-2">—</td><td className="px-2">✅</td></tr>
                </tbody>
              </table>
            </div>
            <p>
              Keys have their own role: <b>agent</b> keys can only call the agent API, never the management API, and
              can be bound to a single agent name and given an expiry. Revoking a key is instant.
            </p>
          </Section>

          <Section id="troubleshooting" title="Troubleshooting">
            <dl className="space-y-3">
              <div>
                <dt className="font-medium text-white">Agent shows offline</dt>
                <dd className="text-ink-300">It heartbeats every 15 s and is marked offline after 45 s of silence. Check the URL is reachable from the agent's network and the key is a valid <b>agent</b> key (401 = bad key, 403 = wrong role or wrong agent binding).</dd>
              </div>
              <div>
                <dt className="font-medium text-white">Graph panel is empty</dt>
                <dd className="text-ink-300">The topology is captured at registration — make sure <span className="font-mono text-xs">instrument()</span> receives the compiled graph, then restart the process.</dd>
              </div>
              <div>
                <dt className="font-medium text-white">Start run does nothing</dt>
                <dd className="text-ink-300">The agent needs an instrumented graph or an <span className="font-mono text-xs">@deck.on_run</span> handler. The command's acknowledgement on the agent page shows the reason.</dd>
              </div>
              <div>
                <dt className="font-medium text-white">Header says Offline</dt>
                <dd className="text-ink-300">That is the live-update stream, not the server. A proxy buffering <span className="font-mono text-xs">text/event-stream</span> keeps it offline — disable buffering on <span className="font-mono text-xs">/api/v1/stream</span>.</dd>
              </div>
            </dl>
          </Section>

          <p className="border-t border-ink-800 pt-4 text-xs text-ink-400">
            The full guide lives in <a href={`${REPO_URL}/blob/main/docs/user-guide.md`} target="_blank" rel="noreferrer" className="text-brand-300 hover:underline">docs/user-guide.md</a>.
          </p>
        </div>
      </div>
    </div>
  );
}
