// Helpers shared by block definitions (DOM-free at import).
import type { BlockCtx } from "./registry";

export const esc = (s: unknown) => String(s ?? "").replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]!);

/** Lines of a block: trimmed, without empty lines and # comments. */
export function blockLines(src: string): string[] {
  return src.replace(/\r\n/g, "\n").split("\n").map((l) => l.trim()).filter((l) => l && !l.startsWith("#"));
}

/** "key: value" at the start of a line, for known keys only. */
export function keyValue(line: string, keys: string[]): [string, string] | undefined {
  const m = /^([A-Za-z]+)\s*:\s*(.*)$/.exec(line);
  if (!m || !keys.includes(m[1].toLowerCase())) return undefined;
  return [m[1].toLowerCase(), m[2].trim()];
}

export const yes = (v: string) => /^(true|yes|ja|1|on)$/i.test(v.trim());

/** The state of a block per reader (block_states); in published pages local to the browser. */
export function blockState(ctx: BlockCtx) {
  const local = `phx-block:${ctx.doc}:${ctx.key}`;
  const path = `/projects/${ctx.project}/documents/${encodeURIComponent(ctx.doc)}/blocks/${encodeURIComponent(ctx.key)}`;
  const remote = !!(ctx.project && ctx.doc);
  return {
    remote,
    async get<T>(): Promise<{ mine: T | null; votes?: Record<string, number>; voters?: number }> {
      if (!remote) {
        try { return { mine: JSON.parse(localStorage.getItem(local) ?? "null") }; } catch { return { mine: null }; }
      }
      const { api } = await import("../api");
      return api("GET", path);
    },
    async put<T>(data: T | null): Promise<{ mine: T | null; votes?: Record<string, number>; voters?: number }> {
      if (!remote) {
        try { if (data === null) localStorage.removeItem(local); else localStorage.setItem(local, JSON.stringify(data)); } catch { /* no storage */ }
        return { mine: data };
      }
      const { api } = await import("../api");
      return api("PUT", path, { data });
    },
  };
}

/** Base styles of interactive blocks (inside the block's shadow root). */
export const BLOCK_CSS = `
.blk { border: 1px solid var(--line); border-radius: 12px; background: var(--surface); padding: 14px 16px; color: var(--fg); font-family: var(--font-body, system-ui, sans-serif); font-size: 14px; }
:host([data-board]) .blk { border: 0; border-radius: 0; height: 100%; box-sizing: border-box; overflow: auto; }
.blk h3 { margin: 0 0 10px; font: 600 16px/1.3 var(--font-display, var(--font-body)); }
.muted { color: var(--muted); font-size: 12.5px; }
button { font: inherit; cursor: pointer; }
button.ghost { border: 1px solid var(--line); background: var(--surface); color: var(--fg); border-radius: 8px; padding: 5px 12px; font-size: 13px; }
button:focus-visible, input:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
@media (hover: hover) and (pointer: fine) { button.ghost:hover { background: var(--surface2); } }
`;
