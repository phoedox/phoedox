// Whiteboards (ADR-0068): the element model shared with the server, and the
// geometry of the editor: bindings of arrow ends, hit tests, extents and
// the order of elements. Pure, so it can be tested without a browser.

export type Pt = [number, number];
export type ElementType = "rect" | "ellipse" | "diamond" | "line" | "arrow" | "pen" | "text" | "sticky" | "frame" | "image" | "embed" | "entity";
export type Color = "gray" | "red" | "orange" | "yellow" | "green" | "blue" | "purple" | "pink";
export const PALETTE: Color[] = ["gray", "red", "orange", "yellow", "green", "blue", "purple", "pink"];

/** An arrow end bound to an element: towards its centre, or (fx/fy, 0–1 of
 * its box) to a fixed point of its outline. */
export interface Binding { element: string; fx?: number; fy?: number }
export interface Preview { title?: string; description?: string; site?: string; image?: string; status?: string; frameable?: boolean }

export interface WBElement {
  id: string; type: ElementType;
  x?: number; y?: number; w?: number; h?: number;
  order?: string; points?: Pt[]; text?: string;
  stroke?: Color; fill?: Color; width?: number; dash?: "" | "dashed" | "dotted"; smooth?: boolean; opacity?: number;
  fontSize?: number; align?: "left" | "center" | "right";
  round?: boolean; start?: Binding; end?: Binding;
  frame?: string; groups?: string[]; locked?: boolean; seed?: number;
  entity?: string; doc?: string; file?: string;
  url?: string; kind?: "video" | "code" | "link" | "web"; preview?: Preview;
}

export interface WBReply { id: string; author?: string; name?: string; text: string; at?: string }
export interface WBComment {
  id: string; x: number; y: number; element?: string; author?: string; name?: string;
  text?: string; ink?: Pt[][]; at?: string; resolved?: boolean; replies?: WBReply[];
}
export type Background = "" | "plain" | "grid" | "dots";
export interface Whiteboard { version?: number; background?: Background; elements: WBElement[]; comments?: WBComment[] }

export const LINEAR = new Set<ElementType>(["line", "arrow"]);
export const BOXY = new Set<ElementType>(["rect", "ellipse", "diamond", "sticky", "entity", "image", "embed", "frame", "text"]);
/** Elements that hold a text a double-click edits. */
export const TEXTUAL = new Set<ElementType>(["rect", "ellipse", "diamond", "sticky", "text", "arrow", "line"]);

export function normalizeBoard(c: Partial<Whiteboard> | undefined): Whiteboard {
  return { version: 1, background: c?.background ?? "", elements: (c?.elements ?? []).map((e) => ({ ...e })), comments: (c?.comments ?? []).map((n) => ({ ...n })) };
}

// ---- order ------------------------------------------------------------------

/** Elements back to front. */
export function sorted(els: WBElement[]): WBElement[] {
  return [...els].sort((a, b) => ((a.order ?? "") < (b.order ?? "") ? -1 : (a.order ?? "") > (b.order ?? "") ? 1 : a.id < b.id ? -1 : 1));
}

/** An order key in front of all elements: the time in base 36 plus a short
 * random part, so inserts of several people sort by time and never tie. */
export function frontKey(els: WBElement[], now = Date.now()): string {
  let key = now.toString(36).padStart(9, "0") + Math.random().toString(36).slice(2, 5).padEnd(3, "0");
  const top = els.reduce((m, e) => ((e.order ?? "") > m ? e.order ?? "" : m), "");
  if (key <= top) key = top.slice(0, 36) + "1";
  return key.replace(/[^0-9a-z]/gi, "").slice(0, 40);
}

/** An order key behind all elements. */
export function backKey(els: WBElement[]): string {
  const low = els.reduce((m, e) => (m === null || (e.order ?? "") < m ? e.order ?? "" : m), null as string | null) ?? "";
  return ("0" + low).slice(0, 40);
}

export const newId = () => "e" + Math.random().toString(36).slice(2, 10);

// ---- geometry ---------------------------------------------------------------

export interface Box { x: number; y: number; w: number; h: number }
export const boxOf = (e: WBElement): Box => ({ x: e.x ?? 0, y: e.y ?? 0, w: e.w ?? 0, h: e.h ?? 0 });
export const center = (e: WBElement): Pt => [(e.x ?? 0) + (e.w ?? 0) / 2, (e.y ?? 0) + (e.h ?? 0) / 2];

/** Where the ray from an element's centre towards (tx, ty) leaves its
 * outline (plus a small gap), by shape. */
export function edgePoint(e: WBElement, tx: number, ty: number, gap = 8): Pt {
  const [cx, cy] = center(e);
  const dx = tx - cx, dy = ty - cy;
  if (!dx && !dy) return [cx, cy];
  const hw = (e.w ?? 0) / 2 + gap, hh = (e.h ?? 0) / 2 + gap;
  let s: number;
  if (e.type === "ellipse") s = 1 / Math.sqrt((dx * dx) / (hw * hw) + (dy * dy) / (hh * hh));
  else if (e.type === "diamond") s = 1 / (Math.abs(dx) / hw + Math.abs(dy) / hh);
  else s = Math.min(hw / Math.abs(dx || 1e-9), hh / Math.abs(dy || 1e-9));
  return [cx + dx * s, cy + dy * s];
}

/** A fixed anchor: the point on the box, pushed out a little from the centre. */
export function anchorPoint(e: WBElement, b: Binding): Pt {
  const px = (e.x ?? 0) + (b.fx ?? 0.5) * (e.w ?? 0), py = (e.y ?? 0) + (b.fy ?? 0.5) * (e.h ?? 0);
  const [cx, cy] = center(e);
  const d = Math.hypot(px - cx, py - cy) || 1;
  return [px + ((px - cx) / d) * 6, py + ((py - cy) / d) * 6];
}

/** The points of a line or arrow with its bindings applied. */
export function linePts(e: WBElement, byId: (id: string) => WBElement | undefined): Pt[] {
  const pts = (e.points ?? []).map((p) => [p[0], p[1]] as Pt);
  const n = pts.length;
  if (n < 2) return pts;
  const target = (b?: Binding) => (b ? byId(b.element) : undefined);
  const s0 = target(e.start), s1 = target(e.end);
  const raw = (b: Binding | undefined, s: WBElement | undefined, fallback: Pt): Pt => (!b || !s ? fallback : b.fx !== undefined ? anchorPoint(s, b) : center(s));
  pts[0] = raw(e.start, s0, pts[0]);
  pts[n - 1] = raw(e.end, s1, pts[n - 1]);
  if (s0 && e.start && e.start.fx === undefined) pts[0] = edgePoint(s0, pts[1][0], pts[1][1]);
  if (s1 && e.end && e.end.fx === undefined) pts[n - 1] = edgePoint(s1, pts[n - 2][0], pts[n - 2][1]);
  return pts;
}

export function distSeg(p: Pt, a: Pt, b: Pt): number {
  const dx = b[0] - a[0], dy = b[1] - a[1], l = dx * dx + dy * dy;
  let t = l ? ((p[0] - a[0]) * dx + (p[1] - a[1]) * dy) / l : 0;
  t = Math.max(0, Math.min(1, t));
  return Math.hypot(p[0] - a[0] - t * dx, p[1] - a[1] - t * dy);
}

export function bbox(e: WBElement, byId: (id: string) => WBElement | undefined): Box {
  const pts = LINEAR.has(e.type) ? linePts(e, byId) : e.type === "pen" ? e.points ?? [] : null;
  if (pts) {
    if (!pts.length) return { x: 0, y: 0, w: 1, h: 1 };
    const xs = pts.map((p) => p[0]), ys = pts.map((p) => p[1]);
    const x = Math.min(...xs), y = Math.min(...ys);
    return { x, y, w: Math.max(...xs) - x || 1, h: Math.max(...ys) - y || 1 };
  }
  return boxOf(e);
}

export const inside = (b: Box, x: number, y: number, pad = 0) => x >= b.x - pad && x <= b.x + b.w + pad && y >= b.y - pad && y <= b.y + b.h + pad;

/** The top-most element at a point (frames only by their title bar). */
export function hitTest(els: WBElement[], x: number, y: number, tol: number, byId: (id: string) => WBElement | undefined): WBElement | undefined {
  const list = sorted(els);
  for (let i = list.length - 1; i >= 0; i--) {
    const e = list[i];
    if (e.type === "frame") continue;
    if (LINEAR.has(e.type) || e.type === "pen") {
      const pts = LINEAR.has(e.type) ? linePts(e, byId) : e.points ?? [];
      for (let k = 1; k < pts.length; k++) if (distSeg([x, y], pts[k - 1], pts[k]) < tol) return e;
      continue;
    }
    if (inside(boxOf(e), x, y)) return e;
  }
  for (let i = list.length - 1; i >= 0; i--) {
    const e = list[i];
    if (e.type === "frame" && y >= (e.y ?? 0) - 4 && y <= (e.y ?? 0) + 26 && x >= (e.x ?? 0) && x <= (e.x ?? 0) + (e.w ?? 0)) return e;
  }
  return undefined;
}

/** An element an arrow end can bind to at a point (not lines, frames, text). */
export function bindTarget(els: WBElement[], x: number, y: number, tol: number, skip?: string): WBElement | undefined {
  const list = sorted(els);
  for (let i = list.length - 1; i >= 0; i--) {
    const e = list[i];
    if (BOXY.has(e.type) && e.type !== "frame" && e.type !== "text" && e.id !== skip && inside(boxOf(e), x, y, tol)) return e;
  }
  return undefined;
}

/** Near the outline the end stays at that point; inside it aims at the centre. */
export function bindingAt(s: WBElement, x: number, y: number, edgeTol: number): Binding {
  const b = boxOf(s);
  const nearX = Math.min(Math.abs(x - b.x), Math.abs(x - b.x - b.w)), nearY = Math.min(Math.abs(y - b.y), Math.abs(y - b.y - b.h));
  if (Math.min(nearX, nearY) <= edgeTol && b.w > 0 && b.h > 0) {
    let fx = Math.max(0, Math.min(1, (x - b.x) / b.w)), fy = Math.max(0, Math.min(1, (y - b.y) / b.h));
    if (nearX < nearY) fx = Math.round(fx);
    else fy = Math.round(fy);
    return { element: s.id, fx: +fx.toFixed(3), fy: +fy.toFixed(3) };
  }
  return { element: s.id };
}

/** Ctrl: put a point on the horizontal or vertical axis of its neighbour. */
export const alignTo = (p: Pt, ref: Pt | undefined, on: boolean): Pt =>
  !on || !ref ? p : Math.abs(p[0] - ref[0]) > Math.abs(p[1] - ref[1]) ? [p[0], ref[1]] : [ref[0], p[1]];

/** Extent of all elements (for "fit" and export). */
export function extent(els: WBElement[], byId: (id: string) => WBElement | undefined): Box | undefined {
  if (!els.length) return undefined;
  const bs = els.map((e) => bbox(e, byId));
  const x0 = Math.min(...bs.map((b) => b.x)), y0 = Math.min(...bs.map((b) => b.y));
  const x1 = Math.max(...bs.map((b) => b.x + b.w)), y1 = Math.max(...bs.map((b) => b.y + b.h));
  return { x: x0, y: y0, w: x1 - x0, h: y1 - y0 };
}

/** What deleting an element does to the rest: bound ends keep their last
 * position, unbound; members of a deleted frame stay where they are. */
export function removeElement(els: WBElement[], id: string): WBElement[] {
  const byId = (x: string) => els.find((e) => e.id === x);
  return els.filter((e) => e.id !== id).map((o) => {
    let next = o;
    if (LINEAR.has(o.type) && (o.start?.element === id || o.end?.element === id)) {
      const pts = linePts(o, byId);
      next = { ...o, points: (o.points ?? []).map((p) => [p[0], p[1]] as Pt) };
      if (o.start?.element === id) { next.points![0] = pts[0]; delete next.start; }
      if (o.end?.element === id) { next.points![next.points!.length - 1] = pts[pts.length - 1]; delete next.end; }
    }
    if (o.frame === id) next = { ...next, frame: undefined };
    return next;
  });
}

/** Elements that lie completely inside a frame. */
export function inFrame(els: WBElement[], frame: WBElement, byId: (id: string) => WBElement | undefined): WBElement[] {
  const f = boxOf(frame);
  return els.filter((o) => {
    if (o.id === frame.id || o.type === "frame") return false;
    if (o.frame === frame.id) return true;
    const b = bbox(o, byId);
    return b.x >= f.x && b.y >= f.y && b.x + b.w <= f.x + f.w && b.y + b.h <= f.y + f.h;
  });
}

// ---- embeds -------------------------------------------------------------------

export type EmbedKind = "video" | "code" | "link" | "web";

/** The kind of an embed by its address (mirrors core.EmbedKind). */
export function embedKind(raw: string): EmbedKind {
  let u: URL;
  try { u = new URL(raw); } catch { return "link"; }
  const host = u.hostname.toLowerCase().replace(/^www\./, "");
  if (["youtube.com", "youtu.be", "m.youtube.com", "vimeo.com", "loom.com"].includes(host) || host.endsWith(".loom.com")) return "video";
  if ((host === "github.com" || host.includes("gitea") || host.startsWith("git.") || host.includes("codeberg")) && /\/(pull|pulls|issues|commit|releases)\//.test(u.pathname)) return "code";
  return "link";
}

/** The privacy-friendly player address of a video, or "" if unknown. */
export function playerURL(raw: string): string {
  let u: URL;
  try { u = new URL(raw); } catch { return ""; }
  const host = u.hostname.toLowerCase().replace(/^www\./, "");
  let id = "";
  if (host === "youtu.be") id = u.pathname.slice(1);
  else if (host.endsWith("youtube.com")) id = u.searchParams.get("v") ?? (u.pathname.match(/^\/(?:embed|shorts|live)\/([^/]+)/)?.[1] ?? "");
  if (id && /^[\w-]{6,20}$/.test(id)) return `https://www.youtube-nocookie.com/embed/${id}`;
  if (host === "vimeo.com") {
    const v = u.pathname.match(/^\/(\d+)/)?.[1];
    if (v) return `https://player.vimeo.com/video/${v}?dnt=1`;
  }
  const loom = u.pathname.match(/^\/share\/([0-9a-f]{16,})/)?.[1];
  if (host.endsWith("loom.com") && loom) return `https://www.loom.com/embed/${loom}`;
  return "";
}

/** A code link's label, e.g. "owner/repo #42". */
export function codeLabel(raw: string): string {
  try {
    const u = new URL(raw);
    const m = u.pathname.match(/^\/([^/]+)\/([^/]+)\/(pull|pulls|issues|commit|releases)\/([^/]+)/);
    if (!m) return u.hostname;
    return m[3] === "commit" ? `${m[1]}/${m[2]} @${m[4].slice(0, 7)}` : m[3] === "releases" ? `${m[1]}/${m[2]} ${m[4]}` : `${m[1]}/${m[2]} #${m[4]}`;
  } catch {
    return raw;
  }
}

// ---- text -------------------------------------------------------------------

/** Wraps text into lines that fit maxW at a font size (rough estimate). */
export function wrapText(text: string, maxW: number, size: number): string[] {
  const out: string[] = [];
  const cw = size * 0.52;
  for (const para of (text || "").split("\n")) {
    let cur = "";
    for (const w of para.split(/\s+/)) {
      const next = cur ? cur + " " + w : w;
      if (next.length * cw > maxW && cur) {
        out.push(cur);
        cur = w;
      } else cur = next;
    }
    out.push(cur);
  }
  return out;
}

// ---- live diff ----------------------------------------------------------------

/** What changed between two boards, element by element (for the live map). */
export function diffBoards(a: Whiteboard, b: Whiteboard): { set: WBElement[]; del: string[]; notes: WBComment[]; delNotes: string[]; bg: boolean } {
  const before = new Map(a.elements.map((e) => [e.id, JSON.stringify(e)]));
  const after = new Set(b.elements.map((e) => e.id));
  const set = b.elements.filter((e) => before.get(e.id) !== JSON.stringify(e));
  const del = a.elements.filter((e) => !after.has(e.id)).map((e) => e.id);
  const nb = new Map((a.comments ?? []).map((c) => [c.id, JSON.stringify(c)]));
  const na = new Set((b.comments ?? []).map((c) => c.id));
  const notes = (b.comments ?? []).filter((c) => nb.get(c.id) !== JSON.stringify(c));
  const delNotes = (a.comments ?? []).filter((c) => !na.has(c.id)).map((c) => c.id);
  return { set, del, notes, delNotes, bg: (a.background ?? "") !== (b.background ?? "") };
}
