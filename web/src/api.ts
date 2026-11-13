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

export interface User {
  id: string; email: string; name: string; isAdmin: boolean; disabled: boolean;
  locale: "" | "de" | "en"; hasPassword: boolean; createdAt: string; lastLoginAt: string | null;
}
export interface Instance { version: string; domain: string; passwordReset: boolean }
export interface LinkInfo { purpose: "invite" | "reset"; email: string; name: string; expiresAt: string }
export interface LinkResult { user: User; purpose: "invite" | "reset"; link: string; mailed: boolean; expiresAt: string }
/** Minimum password length, same as the server (auth.MinPasswordLen). */
export const minPasswordLength = 10;
export interface Project {
  id: string; slug: string; name: string; short: string; description: string;
  primaryColor: string; aiWriteMode: "suggest" | "direct"; role: string;
}
export interface Doc {
  id: string; projectId: string; type: "markdown" | "adr" | "link"; slug: string; title: string;
  category: string; position: number; version: number; updatedAt: string; content?: any;
  /** Decisions only: per-project number, never reused. */
  number?: number;
}
export type DecisionStatus = "proposed" | "accepted" | "rejected" | "deprecated" | "superseded";
export const decisionStatuses: readonly DecisionStatus[] = ["proposed", "accepted", "rejected", "deprecated", "superseded"];
export interface Decision {
  ref: string; number: number; slug: string; title: string; status: DecisionStatus; date?: string;
  category?: string; supersedes?: string; supersededBy?: string; version: number; updatedAt: string;
}
/** Formats a decision number like the server: ADR-0007. */
export const adrRef = (n: number) => `ADR-${String(n).padStart(4, "0")}`;
export interface Entity { id: string; kind: string; slug: string; name: string; attrs: Record<string, unknown> }
export interface Edge { id: string; from: string; rel: string; to: string; fromName: string; toName: string }
export interface Suggestion { id: string; actor: string; op: string; summary: string; status: string; createdAt: string }
export interface Token { id: string; name: string; prefix: string; createdAt: string; lastUsedAt: string | null }
