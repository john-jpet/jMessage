import type { UserSummary } from "./types";

const TOKEN_KEY = "jmessage.token";
const USER_KEY = "jmessage.user";

export function getToken(): string | null {
  return localStorage.getItem(TOKEN_KEY);
}

export function getUser(): UserSummary | null {
  const raw = localStorage.getItem(USER_KEY);
  return raw ? (JSON.parse(raw) as UserSummary) : null;
}

export function setSession(token: string, user: UserSummary) {
  localStorage.setItem(TOKEN_KEY, token);
  localStorage.setItem(USER_KEY, JSON.stringify(user));
}

/** updateSessionUser refreshes the cached identity after profile edits. */
export function updateSessionUser(user: UserSummary) {
  localStorage.setItem(USER_KEY, JSON.stringify(user));
}

export function clearSession() {
  localStorage.removeItem(TOKEN_KEY);
  localStorage.removeItem(USER_KEY);
}

export class ApiError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

/** uploadBlob streams file bytes to the server in a single request and
 *  returns the attachment's render metadata. The attachment is Pending
 *  until a message references it. */
export async function uploadBlob(
  blob: Blob,
  filename: string,
): Promise<{ id: string; filename: string; mimeType: string; size: number }> {
  const resp = await fetch("/api/uploads", {
    method: "POST",
    headers: {
      Authorization: `Bearer ${getToken()}`,
      "X-Filename": filename,
      "Content-Type": blob.type || "application/octet-stream",
    },
    body: blob,
  });
  const data = await resp.json().catch(() => ({}));
  if (!resp.ok) {
    throw new ApiError(resp.status, (data as { error?: string }).error ?? resp.statusText);
  }
  return data as { id: string; filename: string; mimeType: string; size: number };
}

/** attachmentURL builds the byte URL; the token rides the query string
 *  because <img> cannot set headers. */
export function attachmentURL(id: string): string {
  return `/api/attachments/${id}?token=${encodeURIComponent(getToken() ?? "")}`;
}

/** JSON fetch wrapper; injects the Bearer token, maps errors, and
 *  bounces to login on 401. */
export async function api<T>(method: string, path: string, body?: unknown): Promise<T> {
  const headers: Record<string, string> = {};
  const token = getToken();
  if (token) headers.Authorization = `Bearer ${token}`;
  if (body !== undefined) headers["Content-Type"] = "application/json";

  const resp = await fetch(path, {
    method,
    headers,
    body: body !== undefined ? JSON.stringify(body) : undefined,
  });
  if (resp.status === 401 && token) {
    clearSession();
    location.reload();
  }
  const data = await resp.json().catch(() => ({}));
  if (!resp.ok) {
    throw new ApiError(resp.status, (data as { error?: string }).error ?? resp.statusText);
  }
  return data as T;
}
