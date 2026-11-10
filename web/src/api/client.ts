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

/** Uploads or replaces an agent's picture (raw bytes; the server sniffs the type). */
export async function uploadAgentImage(agentId: string, blob: Blob): Promise<void> {
  const res = await fetch(`/api/v1/agents/${agentId}/image`, {
    method: "PUT",
    headers: { "Content-Type": blob.type || "application/octet-stream", ...(csrfToken ? { "X-CSRF-Token": csrfToken } : {}) },
    credentials: "same-origin",
    body: blob,
  });
  if (!res.ok) {
    const msg = ((await res.json().catch(() => ({}))) as { error?: string }).error ?? res.statusText;
    throw new ApiError(res.status, msg);
  }
}

/** Shrinks a picked image to at most `max` px on its longest side and re-encodes it, so uploads stay small. */
export async function shrinkImage(file: File, max = 256): Promise<Blob> {
  if (file.type === "image/gif") return file; // keep animation; the server caps the size
  const bmp = await createImageBitmap(file);
  const scale = Math.min(1, max / Math.max(bmp.width, bmp.height));
  const canvas = document.createElement("canvas");
  canvas.width = Math.max(1, Math.round(bmp.width * scale));
  canvas.height = Math.max(1, Math.round(bmp.height * scale));
  canvas.getContext("2d")?.drawImage(bmp, 0, 0, canvas.width, canvas.height);
  bmp.close();
  const out = await new Promise<Blob | null>((res) => canvas.toBlob(res, "image/webp", 0.9));
  if (!out) throw new Error("Could not process the image");
  return out;
}
