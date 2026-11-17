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
  url?: string; kind?: "video" | "code" | "link" | "web" | "app"; preview?: Preview;
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

// ---- several elements at once ------------------------------------------------

/** Moves an element by dx, dy (points of lines, else the box). */
export function shiftEl(e: WBElement, dx: number, dy: number) {
  if (e.points) e.points = e.points.map(([a, b]) => [a + dx, b + dy] as Pt);
  else {
    e.x = (e.x ?? 0) + dx;
    e.y = (e.y ?? 0) + dy;
  }
}

/** Elements whose bounds lie fully inside the box (frames only as a whole). */
export function inBox(els: WBElement[], b: Box, byId: (id: string) => WBElement | undefined): WBElement[] {
  return els.filter((e) => {
    const r = bbox(e, byId);
    return r.x >= b.x && r.y >= b.y && r.x + r.w <= b.x + b.w && r.y + r.h <= b.y + b.h;
  });
}

/** The elements of the same group(s) as e (its outermost group). */
export function groupOf(els: WBElement[], e: WBElement): WBElement[] {
  const g = e.groups?.[e.groups.length - 1];
  return g ? els.filter((x) => x.groups?.includes(g)) : [e];
}

export type AlignHow = "left" | "hcenter" | "right" | "top" | "vcenter" | "bottom";

/** Aligns the elements to the common bounds. */
export function alignEls(sel: WBElement[], how: AlignHow, byId: (id: string) => WBElement | undefined) {
  const boxes = sel.map((e) => bbox(e, byId));
  const minX = Math.min(...boxes.map((b) => b.x)), maxX = Math.max(...boxes.map((b) => b.x + b.w));
  const minY = Math.min(...boxes.map((b) => b.y)), maxY = Math.max(...boxes.map((b) => b.y + b.h));
  sel.forEach((e, i) => {
    const b = boxes[i];
    const dx = how === "left" ? minX - b.x : how === "right" ? maxX - (b.x + b.w) : how === "hcenter" ? (minX + maxX) / 2 - (b.x + b.w / 2) : 0;
    const dy = how === "top" ? minY - b.y : how === "bottom" ? maxY - (b.y + b.h) : how === "vcenter" ? (minY + maxY) / 2 - (b.y + b.h / 2) : 0;
    shiftEl(e, dx, dy);
  });
}

/** Spreads the elements evenly between the outermost ones. */
export function distributeEls(sel: WBElement[], axis: "x" | "y", byId: (id: string) => WBElement | undefined) {
  if (sel.length < 3) return;
  const items = sel.map((e) => ({ e, b: bbox(e, byId) })).sort((a, c) => (axis === "x" ? a.b.x - c.b.x : a.b.y - c.b.y));
  const size = (b: Box) => (axis === "x" ? b.w : b.h), pos = (b: Box) => (axis === "x" ? b.x : b.y);
  const first = items[0].b, last = items[items.length - 1].b;
  const total = pos(last) + size(last) - pos(first);
  const gap = (total - items.reduce((s, x) => s + size(x.b), 0)) / (items.length - 1);
  let at = pos(first) + size(first) + gap;
  for (const x of items.slice(1, -1)) {
    const d = at - pos(x.b);
    shiftEl(x.e, axis === "x" ? d : 0, axis === "y" ? d : 0);
    at += size(x.b) + gap;
  }
}

/** Copies of elements with new ids, moved by d; bindings and groups inside
 * the copy point to the copies, bindings to other elements are dropped. */
export function cloneEls(sel: WBElement[], d: number): WBElement[] {
  const ids = new Map(sel.map((e) => [e.id, newId()]));
  const groups = new Map<string, string>();
  return sel.map((e) => {
    const c: WBElement = JSON.parse(JSON.stringify(e));
    c.id = ids.get(e.id)!;
    delete c.seed;
    delete c.order;
    shiftEl(c, d, d);
    for (const end of ["start", "end"] as const) {
      const b = c[end];
      if (b) {
        if (ids.has(b.element)) c[end] = { ...b, element: ids.get(b.element)! };
        else delete c[end];
      }
    }
    if (c.groups) c.groups = c.groups.map((g) => groups.get(g) ?? (groups.set(g, newId()), groups.get(g)!));
    if (c.frame && !ids.has(c.frame)) delete c.frame;
    else if (c.frame) c.frame = ids.get(c.frame);
    return c;
  });
}

/** Rounds a box to the grid (12 px). */
export function snapBox(e: WBElement, grid = 12) {
  if (e.points) return;
  const r = (v: number | undefined) => Math.round((v ?? 0) / grid) * grid;
  e.x = r(e.x);
  e.y = r(e.y);
  if (e.w) e.w = Math.max(grid, r(e.w));
  if (e.h) e.h = Math.max(grid, r(e.h));
}

/** Clipboard marker for elements copied from a whiteboard. */
export const CLIP_PREFIX = "phoedox-whiteboard:";

// ---- frames as slides -----------------------------------------------------------

/** Frames in reading order (top to bottom, then left to right). */
export function slides(els: WBElement[]): WBElement[] {
  return els.filter((e) => e.type === "frame").sort((a, b) => {
    const dy = (a.y ?? 0) - (b.y ?? 0);
    return Math.abs(dy) > 40 ? dy : (a.x ?? 0) - (b.x ?? 0);
  });
}

// ---- templates -------------------------------------------------------------------

export type Template = "retro" | "mindmap" | "network" | "storymap" | "c4";
export const TEMPLATES: Template[] = ["retro", "mindmap", "network", "storymap", "c4"];

/** Elements of a template; labels are the visible texts (translated by the caller). */
export function templateEls(kind: Template, L: Record<string, string>): WBElement[] {
  const out: WBElement[] = [];
  const add = (e: Omit<WBElement, "id">) => {
    const el = { id: newId(), ...e } as WBElement;
    out.push(el);
    return el;
  };
  const arrow = (a: WBElement, b: WBElement) => add({ type: "arrow", stroke: "gray", points: [[0, 0], [0, 0]], start: { element: a.id }, end: { element: b.id } });
  switch (kind) {
    case "retro": {
      const cols: [string, Color][] = [[L.well, "green"], [L.improve, "orange"], [L.actions, "blue"]];
      cols.forEach(([title, c], i) => {
        add({ type: "frame", x: i * 400, y: 0, w: 370, h: 560, text: title });
        add({ type: "sticky", x: i * 400 + 30, y: 60, w: 150, h: 110, fill: c === "green" ? "green" : c === "orange" ? "yellow" : "blue", text: "" });
      });
      break;
    }
    case "mindmap": {
      const mid = add({ type: "ellipse", x: 0, y: 0, w: 200, h: 100, text: L.topic, fill: "yellow", smooth: true });
      const spots: Pt[] = [[-320, -160], [320, -160], [-320, 160], [320, 160]];
      spots.forEach(([x, y], i) => arrow(mid, add({ type: "rect", x: x, y: y + 20, w: 180, h: 64, text: `${L.idea} ${i + 1}`, smooth: true })));
      break;
    }
    case "network": {
      const net = add({ type: "ellipse", x: 0, y: 0, w: 180, h: 80, text: "Internet", stroke: "blue" });
      const router = add({ type: "rect", x: 0, y: 160, w: 180, h: 70, text: "Router" });
      const sw = add({ type: "rect", x: 0, y: 310, w: 180, h: 70, text: "Switch" });
      arrow(net, router);
      arrow(router, sw);
      ["NAS", "Server", L.ap].forEach((n, i) => arrow(sw, add({ type: "rect", x: -240 + i * 240, y: 470, w: 180, h: 70, text: n })));
      break;
    }
    case "storymap": {
      for (let i = 0; i < 3; i++) {
        add({ type: "sticky", x: i * 220, y: 0, w: 180, h: 100, fill: "blue", text: `${L.activity} ${i + 1}` });
        for (let j = 0; j < 2; j++) add({ type: "sticky", x: i * 220, y: 160 + j * 130, w: 180, h: 100, fill: "yellow", text: "" });
      }
      add({ type: "line", stroke: "gray", dash: "dashed", points: [[-40, 135], [660, 135]] });
      break;
    }
    case "c4": {
      const person = add({ type: "ellipse", x: 120, y: 0, w: 200, h: 90, text: L.person, fill: "blue" });
      const sys = add({ type: "frame", x: -40, y: 160, w: 520, h: 300, text: L.system });
      const web = add({ type: "rect", x: 0, y: 220, w: 190, h: 80, text: L.web, fill: "blue" });
      const api = add({ type: "rect", x: 250, y: 220, w: 190, h: 80, text: "API", fill: "blue" });
      const db = add({ type: "rect", x: 250, y: 350, w: 190, h: 80, text: L.db, fill: "gray" });
      void sys;
      arrow(person, web);
      arrow(web, api);
      arrow(api, db);
      break;
    }
  }
  return out;
}

// ---- Excalidraw ------------------------------------------------------------------

// The Excalidraw file format stores colours as hex values; these are its
// default palette entries closest to ours (only for files, not for styling).
const EX_STROKE: Record<Color, string> = { gray: "#1e1e1e", red: "#e03131", orange: "#f08c00", yellow: "#e8b800", green: "#2f9e44", blue: "#1971c2", purple: "#9c36b5", pink: "#c2255c" };
const EX_FILL: Record<Color, string> = { gray: "#e9ecef", red: "#ffc9c9", orange: "#ffd8a8", yellow: "#fff3bf", green: "#b2f2bb", blue: "#a5d8ff", purple: "#eebefa", pink: "#fcc2d7" };

function nearest(hex: string | undefined, table: Record<Color, string>): Color | undefined {
  if (!hex || !/^#[0-9a-f]{6}$/i.test(hex)) return undefined;
  const rgb = (h: string) => [1, 3, 5].map((i) => parseInt(h.slice(i, i + 2), 16));
  const [r, g, b] = rgb(hex);
  let best: Color | undefined, dist = Infinity;
  for (const [c, h] of Object.entries(table) as [Color, string][]) {
    const [x, y, z] = rgb(h);
    const d = (r - x) ** 2 + (g - y) ** 2 + (b - z) ** 2;
    if (d < dist) { dist = d; best = c; }
  }
  return best;
}

type ExEl = Record<string, any>;

/** The board as an .excalidraw file (shapes, text, lines, drawings, frames). */
export function toExcalidraw(b: Whiteboard): string {
  const byId = (id: string) => b.elements.find((e) => e.id === id);
  const out: ExEl[] = [];
  const base = (e: WBElement, type: string, box: Box): ExEl => ({
    id: e.id, type, x: box.x, y: box.y, width: box.w, height: box.h, angle: 0,
    strokeColor: EX_STROKE[(e.stroke as Color) ?? "gray"] ?? EX_STROKE.gray, backgroundColor: e.fill ? EX_FILL[e.fill as Color] ?? "transparent" : "transparent",
    fillStyle: "solid", strokeWidth: e.width || 2, strokeStyle: e.dash || "solid", roughness: e.smooth ? 0 : 1, opacity: Math.round((e.opacity ?? 1) * 100),
    groupIds: e.groups ?? [], frameId: null, roundness: null, seed: e.seed || 1, version: 1, versionNonce: 0, isDeleted: false,
    boundElements: [] as { id: string; type: string }[], updated: 0, link: null, locked: !!e.locked,
  });
  const text = (id: string, s: string, box: Box, container: string | null, size: number): ExEl => ({
    ...base({ id, type: "text" } as WBElement, "text", box), text: s, originalText: s, fontSize: size, fontFamily: 1,
    textAlign: container ? "center" : "left", verticalAlign: container ? "middle" : "top", containerId: container, lineHeight: 1.25, strokeColor: EX_STROKE.gray, backgroundColor: "transparent",
  });
  for (const e of sorted(b.elements)) {
    const box = bbox(e, byId);
    if (LINEAR.has(e.type) || e.type === "pen") {
      const pts = e.type === "pen" ? e.points ?? [] : linePts(e, byId);
      if (pts.length < 2) continue;
      const [x0, y0] = pts[0];
      const el = base(e, e.type === "pen" ? "freedraw" : e.type, box);
      Object.assign(el, { x: x0, y: y0, points: pts.map(([x, y]) => [x - x0, y - y0]), lastCommittedPoint: null });
      if (e.type === "arrow") Object.assign(el, { startArrowhead: null, endArrowhead: "arrow" });
      if (e.type !== "pen") {
        if (e.start) el.startBinding = { elementId: e.start.element, focus: 0, gap: 8 };
        if (e.end) el.endBinding = { elementId: e.end.element, focus: 0, gap: 8 };
      } else Object.assign(el, { pressures: [], simulatePressure: true });
      out.push(el);
      continue;
    }
    if (e.type === "text") {
      out.push(text(e.id, e.text ?? "", box, null, e.fontSize || 24));
      continue;
    }
    if (e.type === "frame") {
      out.push({ ...base(e, "frame", box), name: e.text ?? null });
      continue;
    }
    const type = e.type === "ellipse" ? "ellipse" : e.type === "diamond" ? "diamond" : "rectangle";
    const el = base(e, type, box);
    if (e.type === "sticky") Object.assign(el, { backgroundColor: EX_FILL[(e.fill as Color) ?? "yellow"] ?? EX_FILL.yellow, strokeColor: "transparent" });
    const label = e.text || (e.type === "entity" ? e.entity : e.type === "embed" ? e.url : "") || "";
    out.push(el);
    if (label) {
      const tid = e.id + "-t";
      el.boundElements.push({ id: tid, type: "text" });
      out.push(text(tid, label, { x: box.x + 8, y: box.y + box.h / 2 - 12, w: Math.max(10, box.w - 16), h: 24 }, e.id, e.fontSize || 20));
    }
  }
  for (const el of out) {
    for (const end of ["startBinding", "endBinding"]) {
      const target = el[end] && out.find((x) => x.id === el[end].elementId);
      if (target) target.boundElements.push({ id: el.id, type: "arrow" });
    }
  }
  return JSON.stringify({ type: "excalidraw", version: 2, source: "https://phoedox.com", elements: out, appState: { viewBackgroundColor: "#ffffff", gridSize: null }, files: {} }, null, 2);
}

/** Elements from an .excalidraw file; images are left out. */
export function fromExcalidraw(json: string): WBElement[] {
  const data = JSON.parse(json) as { type?: string; elements?: ExEl[] };
  if (data.type !== "excalidraw" || !Array.isArray(data.elements)) throw new Error("not an Excalidraw file");
  const els = data.elements.filter((x) => x && !x.isDeleted);
  const labels = new Map<string, ExEl>();
  for (const x of els) if (x.type === "text" && x.containerId) labels.set(x.containerId, x);
  const ids = new Map<string, string>();
  const id = (old: string) => ids.get(old) ?? (ids.set(old, newId()), ids.get(old)!);
  const out: WBElement[] = [];
  for (const x of els) {
    const common: Partial<WBElement> = {
      stroke: nearest(x.strokeColor, EX_STROKE), smooth: x.roughness === 0 ? true : undefined, width: x.strokeWidth && x.strokeWidth !== 2 ? Math.min(8, x.strokeWidth) : undefined,
      dash: x.strokeStyle === "dashed" || x.strokeStyle === "dotted" ? x.strokeStyle : undefined, opacity: x.opacity && x.opacity < 100 ? x.opacity / 100 : undefined,
      groups: Array.isArray(x.groupIds) && x.groupIds.length ? x.groupIds.map((g: string) => "g" + g) : undefined, locked: x.locked || undefined,
    };
    const box = { x: Number(x.x) || 0, y: Number(x.y) || 0, w: Math.abs(Number(x.width) || 0), h: Math.abs(Number(x.height) || 0) };
    switch (x.type) {
      case "rectangle": case "ellipse": case "diamond": {
        const fill = x.backgroundColor && x.backgroundColor !== "transparent" ? nearest(x.backgroundColor, EX_FILL) : undefined;
        out.push({ ...common, id: id(x.id), type: x.type === "rectangle" ? "rect" : x.type, ...box, fill, text: labels.get(x.id)?.text } as WBElement);
        break;
      }
      case "text":
        if (x.containerId && els.some((c) => c.id === x.containerId)) break;
        out.push({ ...common, id: id(x.id), type: "text", ...box, text: String(x.text ?? ""), fontSize: x.fontSize || undefined } as WBElement);
        break;
      case "frame": case "magicframe":
        out.push({ id: id(x.id), type: "frame", ...box, text: x.name ?? "" } as WBElement);
        break;
      case "arrow": case "line": case "freedraw": {
        const pts = (Array.isArray(x.points) ? x.points : []).map(([px, py]: number[]) => [box.x + px, box.y + py] as Pt);
        if (pts.length < 2) break;
        const e: WBElement = { ...common, id: id(x.id), type: x.type === "freedraw" ? "pen" : x.type, points: pts } as WBElement;
        if (x.type !== "freedraw") {
          if (x.startBinding?.elementId) e.start = { element: id(x.startBinding.elementId) };
          if (x.endBinding?.elementId) e.end = { element: id(x.endBinding.elementId) };
          const label = labels.get(x.id);
          if (label) e.text = label.text;
        }
        out.push(e);
        break;
      }
    }
  }
  // Bindings to elements that were not imported are dropped.
  const have = new Set(out.map((e) => e.id));
  for (const e of out) {
    if (e.start && !have.has(e.start.element)) delete e.start;
    if (e.end && !have.has(e.end.element)) delete e.end;
  }
  return out;
}
