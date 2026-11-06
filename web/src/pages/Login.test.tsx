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
});
