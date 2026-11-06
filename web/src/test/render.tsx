import type { ReactNode } from "react";
import { render } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";

type Route = { method?: string; match: RegExp; status?: number; body?: unknown };

/** mockApi stubs fetch with a tiny route table and records every call. */
export function mockApi(routes: Route[]) {
  const calls: { method: string; url: string; body?: unknown }[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const method = init?.method ?? "GET";
      calls.push({ method, url, body: init?.body ? JSON.parse(String(init.body)) : undefined });
      const r = routes.find((x) => (x.method ?? "GET") === method && x.match.test(url));
      if (!r) return new Response(JSON.stringify({ error: "unmocked " + url }), { status: 404 });
      return r.status === 204 ? new Response(null, { status: 204 }) : new Response(JSON.stringify(r.body ?? {}), { status: r.status ?? 200 });
    }),
  );
  return calls;
}

export const me = (role = "admin", extra: object = {}) => ({
  match: /\/api\/v1\/me$/,
  body: { kind: "user", id: "u1", name: "admin", email: "", role, csrf_token: "csrf-1", auth_mode: "local", must_change_password: false, ...extra },
});

export function renderPage(ui: ReactNode, path = "/") {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[path]}>{ui}</MemoryRouter>
    </QueryClientProvider>,
  );
}
