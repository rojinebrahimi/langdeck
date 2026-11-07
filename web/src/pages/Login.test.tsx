import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { LoginPage } from "./Login";

function renderWith(mode: "dev" | "oidc" | "local", extra: object = {}) {
  vi.stubGlobal("fetch", vi.fn(async () => new Response(JSON.stringify({ mode, ...extra }), { status: 200 })));
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={qc}><LoginPage /></QueryClientProvider>);
}

afterEach(() => vi.unstubAllGlobals());

describe("LoginPage", () => {
  it("offers SSO in OIDC mode", async () => {
    renderWith("oidc");
    const link = await screen.findByRole("link", { name: /continue with sso/i });
    expect(link).toHaveAttribute("href", "/auth/login");
    expect(screen.getByRole("link", { name: /open source on github/i })).toHaveAttribute("href", "https://github.com/rojinebrahimi/langdeck");
  });

  it("offers username and password with the first-login hint in local mode", async () => {
    renderWith("local", { default_credentials: "admin / admin" });
    expect(await screen.findByLabelText("Username")).toBeInTheDocument();
    expect(screen.getByLabelText("Password")).toHaveAttribute("type", "password");
    expect(screen.getByText("admin / admin")).toBeInTheDocument();
  });

  it("offers the email form with a warning in dev mode", async () => {
    renderWith("dev");
    expect(await screen.findByLabelText("Email")).toBeInTheDocument();
    expect(screen.getByText(/development mode/i)).toBeInTheDocument();
  });

  it("never asks for an email on the public demo", async () => {
    const fetchMock = vi.fn(async (url: string, _init?: RequestInit) =>
      url.includes("/auth/info")
        ? new Response(JSON.stringify({ mode: "dev", demo: true }), { status: 200 })
        : new Response(JSON.stringify({ id: "u1" }), { status: 200 }),
    );
    vi.stubGlobal("fetch", fetchMock);
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(<QueryClientProvider client={qc}><LoginPage /></QueryClientProvider>);
    const button = await screen.findByRole("button", { name: /enter the demo/i });
    expect(screen.queryByLabelText("Email")).not.toBeInTheDocument();
    expect(screen.getByText(/anonymous visitor identity/i)).toBeInTheDocument();
    const assign = vi.fn();
    vi.stubGlobal("location", { ...window.location, assign, search: "" });
    button.click();
    await vi.waitFor(() => expect(fetchMock.mock.calls.some(([u]) => String(u).includes("/auth/dev-login"))).toBe(true));
    const [, init] = fetchMock.mock.calls.find(([u]) => String(u).includes("/auth/dev-login"))!;
    expect(String((init as RequestInit | undefined)?.body ?? "")).not.toContain("@");
  });
});
