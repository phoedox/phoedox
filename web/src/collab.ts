// Shared helpers for live collaboration (text editor and floor plans).

/** Stable caret color per user from the collaboration tokens. */
export function userColor(id: string): string {
  let h = 0;
  for (const c of id) h = (h * 31 + c.charCodeAt(0)) >>> 0;
  const v = getComputedStyle(document.documentElement).getPropertyValue(`--collab-${(h % 8) + 1}`).trim();
  return v || "#d9480f";
}

/** Phoedox messages on the y-websocket connection (see internal/realtime). */
export const MSG_CONTROL = 100;
export const MSG_SNAPSHOT = 101;

export interface Control { type: string; upto?: number; by?: string; reason?: string }
