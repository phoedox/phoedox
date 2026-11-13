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
  locale: "" | "de" | "en"; hasPassword: boolean; secondFactor?: boolean; createdAt: string; lastLoginAt: string | null;
}
export interface Instance { version: string; domain: string; passwordReset: boolean; oidc?: { name: string } }
export interface Grant { id: string; clientId: string; clientName: string; clientUri?: string; createdAt: string; lastUsedAt: string | null; access: "read" | "write"; projects: string[] | null }
export interface LinkInfo { purpose: "invite" | "reset"; email: string; name: string; expiresAt: string }
export interface LinkResult { user: User; purpose: "invite" | "reset"; link: string; mailed: boolean; expiresAt: string }
/** Minimum password length, same as the server (auth.MinPasswordLen). */
export const minPasswordLength = 10;
export interface Project {
  id: string; slug: string; name: string; short: string; description: string;
  primaryColor: string; aiWriteMode: "off" | "read" | "suggest" | "direct";
  /** Main language of the content ("" = not multilingual) and further languages (ADR-0028). */
  mainLocale: string; locales: string[];
  /** owner | editor | reader | doc (only single documents shared) */
  role: string;
  orgId?: string; orgName?: string;
}
export interface Org { id: string; slug: string; name: string; usageType: "commercial" | "noncommercial"; aiEnabled: boolean; quotaMb: number; createdAt: string; role?: "admin" | "member" }
export interface OrgMember { userId: string; email: string; name: string; role: string }
export interface Group { id: string; orgId: string; name: string; oidcGroup: string; members: OrgMember[] }
export interface OrgDetail extends Org { members: OrgMember[]; groups: Group[] }
export interface Member { userId: string; email: string; name: string; role: string; expiresAt: string | null }
export interface ProjectGroup { groupId: string; name: string; role: string }
export interface DocGrant { id: string; userId?: string; groupId?: string; name: string; email?: string; role: "reader" | "editor"; createdAt: string }
export interface DocSharing { restricted: boolean; grants: DocGrant[] }
export interface ShareLink { id: string; projectId: string; documentId?: string; label: string; createdBy: string; createdAt: string; expiresAt: string | null; lastUsedAt: string | null; token?: string }
/** Address of a share link (the token is only known right after creating it). */
export const shareURL = (token: string) => `${location.origin}/s/${token}`;
export interface Doc {
  id: string; projectId: string; type: "markdown" | "adr" | "link"; slug: string; title: string;
  category: string; position: number; version: number; updatedAt: string; content?: any;
  /** Decisions only: per-project number, never reused. */
  number?: number;
  /** Visible only to owners and people or groups it is shared with. */
  restricted?: boolean;
  /** The requesting user's role for this page. */
  role?: string;
  /** Multilingual projects: language shown, the shown translation (none = main language) and all translations. */
  locale?: string;
  translation?: TranslationMeta;
  translations?: TranslationMeta[];
}
export interface TranslationMeta { documentId: string; locale: string; title: string; sourceVersion: number; machine: boolean; updatedAt: string }
export interface TranslationInfo extends TranslationMeta { content?: any; outdated: boolean; mainVersion: number }
/** A draft of a Markdown page (ADR-0027); shared by everyone editing it. */
export interface DraftParticipant { actor: string; name: string; ai?: boolean }
export interface DraftInfo {
  documentId: string; slug?: string; baseVersion: number; title: string; category: string; markdown: string;
  startedAt: string; updatedAt: string; participants: DraftParticipant[]; startedBy: DraftParticipant; updatedBy: DraftParticipant;
  /** The published page changed after the draft started. */
  outdated: boolean;
  /** The caller may publish or cancel. */
  canEnd: boolean;
}
export type DecisionStatus = "proposed" | "accepted" | "rejected" | "deprecated" | "superseded";
export const decisionStatuses: readonly DecisionStatus[] = ["proposed", "accepted", "rejected", "deprecated", "superseded"];
export interface Decision {
  ref: string; number: number; slug: string; title: string; status: DecisionStatus; date?: string;
  category?: string; supersedes?: string; supersededBy?: string; version: number; updatedAt: string;
}
/** Formats a decision number like the server: ADR-0007. */
export const adrRef = (n: number) => `ADR-${String(n).padStart(4, "0")}`;
export interface Hit {
  type: "document" | "entity" | "file"; projectId: string; projectSlug: string; projectName: string;
  id: string; slug: string; title: string; kind: string; snippet?: string;
}
/** Snippet markers from the server around matches. */
export const MARK_START = "\uE000";
export const MARK_END = "\uE001";
export interface Favorite { documentId: string; personal: boolean; position: number }
export interface Entity { id: string; kind: string; slug: string; name: string; attrs: Record<string, unknown>; source?: string; updatedAt?: string }
export interface Edge {
  id: string; from: string; rel: string; to: string; fromName: string; toName: string;
  fromSlug?: string; toSlug?: string; fromKind?: string; toKind?: string;
}
export interface Suggestion { id: string; actor: string; op: string; summary: string; status: string; createdAt: string }
export interface Token { id: string; name: string; prefix: string; createdAt: string; lastUsedAt: string | null; access: "read" | "write"; projects: string[] | null }

export interface LicenseStatus { required: boolean; valid: boolean; plan?: string; expires?: string; trialEnd?: string; problem?: string; hint?: string }
export interface PluginInfo {
  id: string; version: string; name: string; sha256: string; size: number; source: "store" | "upload"; signedBy: string; signed: boolean;
  enabled: boolean; recall: "" | "warning" | "critical"; recallNote?: string; latest?: string; installedAt: string; updatedAt: string;
  description?: string; permissions: string[]; kinds: string[]; tools: string[]; events: string[]; secrets: string[];
  frontend: boolean; backend: boolean; criticalPath: boolean; license: LicenseStatus; projects: number; problem?: "manifest" | "incompatible"; needs?: string;
}
export interface CatalogEntry {
  id: string; name: string; version: string; summary: string; author: string; verified: boolean; icon?: string;
  permissions: string[]; sha256: string; recall?: string; recallNote?: string; installed?: string;
}
export interface ProjectPlugin {
  id: string; name: string; version: string; description?: string; enabled: boolean; permissions?: string[];
  kinds: string[]; tools: string[]; frontend?: string; secrets?: Record<string, boolean>; lastRun: string | null; lastError?: string;
  problem?: string; recall?: string;
}
export interface PluginLog { time: string; level: string; msg: string }
