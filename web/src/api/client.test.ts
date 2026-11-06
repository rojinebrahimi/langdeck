import { api, ApiError, setCsrfToken } from "./client";

afterEach(() => vi.unstubAllGlobals());

describe("api client", () => {
  it("sends the CSRF token on writes only", async () => {
    const fetchMock = vi.fn(async () => new Response("{}", { status: 200 }));
    vi.stubGlobal("fetch", fetchMock);
    setCsrfToken("tok");
    await api("/x");
    await api("/x", { method: "POST", body: { a: 1 } });
    const headers = (i: number) => (fetchMock.mock.calls[i] as unknown as [string, RequestInit])[1].headers as Record<string, string>;
    expect(headers(0)["X-CSRF-Token"]).toBeUndefined();
    expect(headers(1)["X-CSRF-Token"]).toBe("tok");
    expect(headers(1)["Content-Type"]).toBe("application/json");
  });

  it("raises ApiError with the server message", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => new Response(JSON.stringify({ error: "insufficient role" }), { status: 403 })));
    await expect(api("/x")).rejects.toEqual(new ApiError(403, "insufficient role"));
  });

  it("handles 204", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => new Response(null, { status: 204 })));
    await expect(api("/x", { method: "DELETE" })).resolves.toBeUndefined();
  });
});
