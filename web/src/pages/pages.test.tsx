import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { setCsrfToken } from "../api/client";
import { me, mockApi, renderPage } from "../test/render";
import { ApprovalsPage } from "./Approvals";
import { APIKeysPage } from "./APIKeys";
import { ChangePasswordForm } from "./ChangePassword";
import { AgentsPage } from "./Agents";
import { NotificationsPage } from "./Notifications";
import { GuidePage } from "./Guide";
import { DependenciesPage } from "./Dependencies";
import { AgentDetailPage } from "./AgentDetail";
import { Route, Routes } from "react-router-dom";

// React Flow needs layout APIs jsdom lacks; the graph itself is covered by GraphView.test.ts.
vi.mock("../components/GraphView", () => ({
  GraphView: ({ graph }: { graph?: { nodes: { id: string }[]; edges: { source: string; target: string }[] } }) => (
    <ul aria-label="graph">{graph?.edges.map((e) => <li key={e.source + e.target}>{`${e.source} -> ${e.target}`}</li>)}</ul>
  ),
}));

afterEach(() => vi.unstubAllGlobals());

const interrupt = {
  id: "int-1", run_id: "r1", agent_id: "a1", agent_name: "support-bot", node: "refund",
  question: "Refund $120?", payload: { order: 42 }, status: "pending", created_at: new Date().toISOString(),
};

describe("ApprovalsPage", () => {
  it("lets an operator approve with a note", async () => {
    const calls = mockApi([
      me("operator"),
      { match: /\/api\/v1\/interrupts\?status=pending/, body: { interrupts: [interrupt] } },
      { method: "POST", match: /\/interrupts\/int-1\/resolve$/, body: { ...interrupt, status: "approved" } },
    ]);
    setCsrfToken("csrf-1");
    renderPage(<ApprovalsPage />);
    expect(await screen.findByText("Refund $120?")).toBeInTheDocument();
    await userEvent.type(await screen.findByPlaceholderText(/note for the agent/i), "ok, VIP");
    await userEvent.click(screen.getByRole("button", { name: /approve/i }));
    await waitFor(() => expect(calls.some((c) => c.method === "POST")).toBe(true));
    const post = calls.find((c) => c.method === "POST")!;
    expect(post.body).toEqual({ decision: "approve", response: { note: "ok, VIP" } });
  });

  it("hides decision buttons from viewers", async () => {
    mockApi([me("viewer"), { match: /\/api\/v1\/interrupts/, body: { interrupts: [interrupt] } }]);
    renderPage(<ApprovalsPage />);
    expect(await screen.findByText("Refund $120?")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /approve/i })).not.toBeInTheDocument();
  });
});

describe("APIKeysPage", () => {
  it("shows a new secret exactly once", async () => {
    const secret = "ldk_" + "s".repeat(43);
    const calls = mockApi([
      me(),
      { match: /\/api\/v1\/api-keys$/, body: { api_keys: [] } },
      { method: "POST", match: /\/api\/v1\/api-keys$/, status: 201, body: { secret, api_key: { id: "k1" } } },
    ]);
    renderPage(<APIKeysPage />);
    await userEvent.click(await screen.findByRole("button", { name: /new key/i }));
    await userEvent.type(screen.getByLabelText("Name"), "support-bot prod");
    await userEvent.type(screen.getByLabelText(/restrict to agent/i), "support-bot");
    await userEvent.click(screen.getByRole("button", { name: /create key/i }));
    expect(await screen.findByText(secret)).toBeInTheDocument();
    expect(calls.find((c) => c.method === "POST")!.body).toMatchObject({ name: "support-bot prod", role: "agent", agent_name: "support-bot" });
    await userEvent.click(screen.getByRole("button", { name: "Done" }));
    expect(screen.queryByText(secret)).not.toBeInTheDocument();
  });
});

describe("ChangePasswordForm", () => {
  it("blocks mismatched confirmation and submits a valid change", async () => {
    const calls = mockApi([{ method: "POST", match: /\/me\/password$/, status: 204 }]);
    const done = vi.fn();
    renderPage(<ChangePasswordForm forced onDone={done} />);
    expect(screen.getByText(/initial password/i)).toBeInTheDocument();
    await userEvent.type(screen.getByLabelText("Current password"), "admin");
    await userEvent.type(screen.getByLabelText("New password"), "a-strong-password");
    await userEvent.type(screen.getByLabelText("Confirm new password"), "a-strong-passw0rd");
    expect(screen.getByText(/do not match/i)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /update password/i })).toBeDisabled();

    await userEvent.clear(screen.getByLabelText("Confirm new password"));
    await userEvent.type(screen.getByLabelText("Confirm new password"), "a-strong-password");
    await userEvent.click(screen.getByRole("button", { name: /update password/i }));
    await waitFor(() => expect(done).toHaveBeenCalled());
    expect(calls[0].body).toEqual({ current_password: "admin", new_password: "a-strong-password" });
  });

  it("shows server errors", async () => {
    mockApi([{ method: "POST", match: /\/me\/password$/, status: 403, body: { error: "current password is wrong" } }]);
    renderPage(<ChangePasswordForm />);
    await userEvent.type(screen.getByLabelText("Current password"), "nope");
    await userEvent.type(screen.getByLabelText("New password"), "a-strong-password");
    await userEvent.type(screen.getByLabelText("Confirm new password"), "a-strong-password");
    await userEvent.click(screen.getByRole("button", { name: /update password/i }));
    expect(await screen.findByRole("alert")).toHaveTextContent("current password is wrong");
  });
});

describe("AgentsPage", () => {
  it("shows onboarding help when no agent is connected", async () => {
    mockApi([me(), { match: /\/api\/v1\/agents\?/, body: { agents: [], total: 0, next_after: null } }]);
    renderPage(<AgentsPage />);
    expect(await screen.findByText(/no agents connected yet/i)).toBeInTheDocument();
    expect(screen.getByText(/deck.instrument/)).toBeInTheDocument();
  });

  it("lists agents with status", async () => {
    mockApi([
      me(),
      {
        match: /\/api\/v1\/agents\?/,
        body: { total: 1, next_after: null, agents: [{ id: "a1", name: "support-bot", description: "Answers tickets", version: "2.0", framework: "langgraph", labels: { team: "cx" }, desired_state: "running", status: "online", instances: 3, sdk_version: "0.1.0", last_heartbeat: new Date().toISOString(), created_at: "", updated_at: "" }] },
      },
    ]);
    renderPage(<AgentsPage />);
    expect(await screen.findByText("support-bot")).toBeInTheDocument();
    expect(screen.getByText("online")).toBeInTheDocument();
    expect(screen.getByText("3 instances")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /support-bot/ })).toHaveAttribute("href", "/agents/a1");
  });
});

describe("GuidePage", () => {
  it("walks through connecting an agent and links the repo guide", () => {
    renderPage(<GuidePage />);
    expect(screen.getByRole("heading", { name: /connect an agent/i })).toBeInTheDocument();
    expect(screen.getByText(/deck\.instrument\(builder\.compile\(\)\)/)).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: /roles, keys and accounts/i })).toBeInTheDocument();
    const link = screen.getByRole("link", { name: /full guide/i });
    expect(link).toHaveAttribute("href", expect.stringContaining("docs/user-guide.md"));
  });
});

const agentRow = (name: string, status = "online") => ({
  id: "id-" + name, name, description: "", version: "1", framework: "langgraph", labels: {}, desired_state: "running",
  status, instances: 1, sdk_version: "0.1.2", last_heartbeat: new Date().toISOString(), created_at: "", updated_at: "",
});

describe("AgentsPage pagination", () => {
  it("pages with the server cursor and goes back", async () => {
    const calls = mockApi([
      me(),
      { match: /\/api\/v1\/agents\?.*after=agent-b/, body: { agents: [agentRow("agent-c")], total: 3, next_after: null } },
      { match: /\/api\/v1\/agents\?/, body: { agents: [agentRow("agent-a"), agentRow("agent-b")], total: 3, next_after: "agent-b" } },
    ]);
    renderPage(<AgentsPage />);
    expect(await screen.findByText("agent-a")).toBeInTheDocument();
    expect(screen.getByText(/of 3 agents/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /previous page/i })).toBeDisabled();

    await userEvent.click(screen.getByRole("button", { name: /next page/i }));
    expect(await screen.findByText("agent-c")).toBeInTheDocument();
    expect(calls.some((c) => c.url.includes("after=agent-b"))).toBe(true);
    expect(screen.getByRole("button", { name: /next page/i })).toBeDisabled();

    await userEvent.click(screen.getByRole("button", { name: /previous page/i }));
    expect(await screen.findByText("agent-a")).toBeInTheDocument();
  });

  it("searches and filters by status on the server", async () => {
    const calls = mockApi([
      me(),
      { match: /\/api\/v1\/agents\?/, body: { agents: [agentRow("billing-bot", "paused")], total: 1, next_after: null } },
    ]);
    renderPage(<AgentsPage />);
    await screen.findByText("billing-bot");
    await userEvent.type(screen.getByPlaceholderText(/search by name/i), "billing");
    await waitFor(() => expect(calls.some((c) => c.url.includes("q=billing"))).toBe(true));
    await userEvent.click(screen.getByRole("button", { name: "Paused" }));
    await waitFor(() => expect(calls.some((c) => c.url.includes("status=paused") && c.url.includes("q=billing"))).toBe(true));
    expect(screen.getByRole("button", { name: "Paused" })).toHaveAttribute("aria-pressed", "true");
    // A single page shows a count instead of pager buttons.
    expect(screen.queryByRole("button", { name: /next page/i })).not.toBeInTheDocument();
    expect(screen.getByText("1 agent")).toBeInTheDocument();
  });

  it("says nothing matched instead of showing onboarding when filters are active", async () => {
    mockApi([me(), { match: /\/api\/v1\/agents\?/, body: { agents: [], total: 0, next_after: null } }]);
    renderPage(<AgentsPage />, "/agents?status=offline");
    expect(await screen.findByText(/no agents match/i)).toBeInTheDocument();
    expect(screen.queryByText(/no agents connected yet/i)).not.toBeInTheDocument();
  });
});

const depGraph = {
  nodes: [
    { id: "a1", name: "support-bot", status: "online", registered: true, accesses: 2 },
    { id: "a2", name: "billing", status: "offline", registered: true, accesses: 0 },
    { name: "kb-search", status: "unknown", registered: false, accesses: 0 },
  ],
  edges: [{ source: "support-bot", target: "billing" }, { source: "support-bot", target: "kb-search" }],
};

describe("DependenciesPage", () => {
  it("draws declared edges and flags dependencies nobody registered", async () => {
    mockApi([me("viewer"), { match: /\/api\/v1\/agent-graph$/, body: depGraph }]);
    renderPage(<DependenciesPage />);
    expect(await screen.findByText("support-bot -> billing")).toBeInTheDocument();
    expect(screen.getByText("support-bot -> kb-search")).toBeInTheDocument();
    expect(screen.getByText(/Declared but not registered: kb-search/)).toBeInTheDocument();
  });

  it("explains how to declare dependencies when there are none", async () => {
    mockApi([me("viewer"), { match: /\/api\/v1\/agent-graph$/, body: { nodes: [], edges: [] } }]);
    renderPage(<DependenciesPage />);
    expect(await screen.findByText("No dependencies declared")).toBeInTheDocument();
  });
});

describe("AgentDetailPage access tab", () => {
  const agent = {
    id: "a1", name: "support-bot", description: "", version: "1", framework: "langgraph", labels: {}, desired_state: "running",
    status: "online", instances: 1, sdk_version: "0.1", created_at: new Date().toISOString(), updated_at: new Date().toISOString(),
    accesses: [
      { resource: "postgres:orders", modes: ["read", "write"], description: "refund lookups" },
      { resource: "s3:reports", modes: ["read"] },
    ],
    depends_on: ["billing"],
  };
  const routes = (a: object) => [
    me("viewer"),
    { match: /\/api\/v1\/agents\/a1$/, body: { agent: a, config: null } },
    { match: /\/api\/v1\/agents\/a1\/commands$/, body: { commands: [] } },
    { match: /\/api\/v1\/runs/, body: { runs: [] } },
    { match: /\/api\/v1\/agent-graph$/, body: depGraph },
  ];
  const open = () =>
    renderPage(<Routes><Route path="/agents/:id" element={<AgentDetailPage />} /></Routes>, "/agents/a1");

  it("lists each resource with its modes, and what the agent depends on", async () => {
    mockApi(routes(agent));
    open();
    await userEvent.click(await screen.findByRole("tab", { name: /^Access \(2\)/ }));
    expect(screen.getByText("postgres:orders")).toBeInTheDocument();
    expect(screen.getByText("refund lookups")).toBeInTheDocument();
    expect(screen.getAllByText("read")).toHaveLength(2);
    expect(screen.getByText("write")).toBeInTheDocument();
    expect(await screen.findByRole("link", { name: "billing" })).toHaveAttribute("href", "/agents/a2");
  });

  it("tells the developer how to declare accesses when there are none", async () => {
    mockApi(routes({ ...agent, accesses: [], depends_on: [] }));
    open();
    await userEvent.click(await screen.findByRole("tab", { name: /^Access/ }));
    expect(screen.getByText(/has not declared any accesses/)).toBeInTheDocument();
    expect(screen.getByText("Calls no other agents.")).toBeInTheDocument();
  });
});

describe("NotificationsPage", () => {
  const view = { events: ["approval"], available_events: ["approval", "run_failed", "agent_offline", "agent_online"],
    channels: { slack: false, mattermost: false, teams: false, telegram: false }, telegram_chat_id: "", source: "env" };

  it("saves a webhook without ever showing a stored secret", async () => {
    const calls = mockApi([
      me(),
      { method: "GET", match: /\/api\/v1\/settings\/notifications$/, body: view },
      { method: "PUT", match: /\/api\/v1\/settings\/notifications$/, body: { ...view, source: "ui", channels: { ...view.channels, slack: true } } },
    ]);
    setCsrfToken("csrf-1");
    renderPage(<NotificationsPage />);
    await userEvent.type(await screen.findByLabelText("Webhook URL", { selector: "#ch-slack" }), "https://hooks.slack.com/services/T/B/x");
    await userEvent.click(screen.getByRole("button", { name: /save changes/i }));
    await waitFor(() => expect(calls.some((c) => c.method === "PUT")).toBe(true));
    expect(calls.find((c) => c.method === "PUT")!.body).toEqual({ slack_url: "https://hooks.slack.com/services/T/B/x" });
    expect(await screen.findByText("Connected")).toBeInTheDocument();
    expect(screen.queryByDisplayValue(/hooks\.slack\.com/)).not.toBeInTheDocument();
  });
});
