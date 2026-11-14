import { api } from "./api";

/** A monitoring check: a reading request block that runs on a schedule. */
export interface MonitorCheck {
  id: string;
  projectId: string;
  documentId: string;
  documentSlug: string;
  documentTitle: string;
  blockIndex: number;
  entityId?: string;
  entitySlug?: string;
  entityName?: string;
  name: string;
  intervalSec: number;
  enabled: boolean;
  /** Shown on the public status page. */
  public: boolean;
  lastStatus: "unknown" | "ok" | "fail";
  lastRun: string | null;
  lastChange: string | null;
  lastMs: number;
  lastMessage: string;
  lastDetail: string;
  failCount: number;
}

/** Interval choices offered in the UI (seconds). */
export const checkIntervals = [60, 300, 900, 3600, 21600, 86400];

/** The checks of one page, fetched once for all request blocks on it. */
const byDoc = new Map<string, Promise<MonitorCheck[]>>();

export function pageChecks(project: string, doc: string, fresh = false): Promise<MonitorCheck[]> {
  const key = `${project}/${doc}`;
  if (fresh || !byDoc.has(key)) {
    byDoc.set(key, api<MonitorCheck[]>("GET", `/projects/${project}/checks?document=${encodeURIComponent(doc)}`).then((x) => x ?? []).catch(() => []));
  }
  return byDoc.get(key)!;
}

export function forgetPageChecks(project: string, doc: string) {
  byDoc.delete(`${project}/${doc}`);
}

/** Status shown for a check: switched-off checks are "paused". */
export function checkState(c: MonitorCheck): "ok" | "fail" | "unknown" | "paused" {
  return c.enabled ? c.lastStatus : "paused";
}
