import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { setCsrfToken } from "../api/client";
import { me, mockApi, renderPage } from "../test/render";
import { ApprovalsPage } from "./Approvals";
import { APIKeysPage } from "./APIKeys";
import { ChangePasswordForm } from "./ChangePassword";
import { AgentsPage } from "./Agents";

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
    mockApi([me(), { match: /\/api\/v1\/agents$/, body: { agents: [] } }]);
    renderPage(<AgentsPage />);
    expect(await screen.findByText(/no agents connected yet/i)).toBeInTheDocument();
    expect(screen.getByText(/deck.instrument/)).toBeInTheDocument();
  });

  it("lists agents with status", async () => {
    mockApi([
      me(),
      {
        match: /\/api\/v1\/agents$/,
        body: { agents: [{ id: "a1", name: "support-bot", description: "Answers tickets", version: "2.0", framework: "langgraph", labels: { team: "cx" }, desired_state: "running", status: "online", instances: 3, sdk_version: "0.1.0", last_heartbeat: new Date().toISOString(), created_at: "", updated_at: "" }] },
      },
    ]);
    renderPage(<AgentsPage />);
    expect(await screen.findByText("support-bot")).toBeInTheDocument();
    expect(screen.getByText("online")).toBeInTheDocument();
    expect(screen.getByText("3 instances")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /support-bot/ })).toHaveAttribute("href", "/agents/a1");
  });
});
