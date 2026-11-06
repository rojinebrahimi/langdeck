// Thin fetch wrapper. Cookies carry the session; the CSRF token from /me is
// attached to every state-changing request.

export class ApiError extends Error {
  constructor(public status: number, message: string) {
    super(message);
  }
}

let csrfToken = "";
export function setCsrfToken(token: string) {
  csrfToken = token;
}

export async function api<T>(path: string, init: { method?: string; body?: unknown } = {}): Promise<T> {
  const method = init.method ?? "GET";
  const headers: Record<string, string> = { Accept: "application/json" };
  if (init.body !== undefined) headers["Content-Type"] = "application/json";
  if (method !== "GET" && csrfToken) headers["X-CSRF-Token"] = csrfToken;

  const res = await fetch(path, {
    method,
    headers,
    credentials: "same-origin",
    body: init.body === undefined ? undefined : JSON.stringify(init.body),
  });
  if (res.status === 204) return undefined as T;
  const text = await res.text();
  let data: unknown = undefined;
  try {
    data = text ? JSON.parse(text) : undefined;
  } catch {
    /* non-JSON error page */
  }
  if (!res.ok) {
    const msg = (data as { error?: string } | undefined)?.error ?? res.statusText ?? "Request failed";
    throw new ApiError(res.status, msg);
  }
  return data as T;
}
