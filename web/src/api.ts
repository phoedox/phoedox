import { hasKey, t, type Key } from "./i18n";

/** Error returned by the API: translate via key + params, never show `error`. */
export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    readonly key: string,
    readonly params: Record<string, unknown> = {},
  ) {
    super(code);
  }

  /** The translated message for the current locale. */
  get text(): string {
    if (hasKey(this.key)) return t(this.key as Key, this.params);
    const generic = `error.${this.code}`;
    return hasKey(generic) ? t(generic as Key) : t("error.unknown");
  }
}

export async function api<T = unknown>(method: string, path: string, body?: unknown): Promise<T> {
  let res: Response;
  try {
    res = await fetch(`/api/v1${path}`, {
      method,
      credentials: "same-origin",
      headers: { "Content-Type": "application/json", "X-Phoedox": "1" },
      body: body === undefined ? undefined : JSON.stringify(body),
    });
  } catch {
    throw new ApiError(0, "network", "error.network");
  }
  if (res.status === 204) return undefined as T;
  const data = await res.json().catch(() => ({}));
  if (!res.ok) throw new ApiError(res.status, data.code ?? "unknown", data.key ?? "error.unknown", data.params ?? {});
  return data as T;
}

export interface User { id: string; email: string; name: string; isAdmin: boolean }
export interface Project {
  id: string; slug: string; name: string; short: string; description: string;
  primaryColor: string; aiWriteMode: "suggest" | "direct"; role: string;
}
export interface Doc {
  id: string; projectId: string; type: "markdown" | "adr" | "link"; slug: string; title: string;
  category: string; position: number; version: number; updatedAt: string; content?: any;
}
export interface Entity { id: string; kind: string; slug: string; name: string; attrs: Record<string, unknown> }
export interface Edge { id: string; from: string; rel: string; to: string; fromName: string; toName: string }
export interface Suggestion { id: string; actor: string; op: string; summary: string; status: string; createdAt: string }
export interface Token { id: string; name: string; prefix: string; createdAt: string; lastUsedAt: string }
