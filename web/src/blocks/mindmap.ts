// Mind maps: an indented outline (spaces, tabs or "- " lists) becomes a map
// with the first line in the middle and its branches to both sides. Readers
// collapse branches; the map can be copied onto a whiteboard as shapes.
import { t } from "../i18n";
import { registerBlock, type BlockProblem } from "./registry";
import { BLOCK_CSS, esc } from "./util";

export interface MindNode { text: string; children: MindNode[]; id: string }

const MAX_NODES = 400;

export function parseMindmap(src: string): { model?: MindNode; problems: BlockProblem[] } {
  const lines = src.replace(/\r\n/g, "\n").split("\n").filter((l) => l.trim() && !l.trim().startsWith("#"));
  if (!lines.length) return { problems: [{ key: "mindmap.err_empty" }] };
  const indent = (l: string) => l.replace(/\t/g, "  ").match(/^ */)![0].length;
  const clean = (l: string) => l.trim().replace(/^[-*+]\s+/, "");
  const root: MindNode = { text: clean(lines[0]), children: [], id: "0" };
  const stack: { node: MindNode; ind: number }[] = [{ node: root, ind: -1 }];
  const problems: BlockProblem[] = [];
  for (const l of lines.slice(1)) {
    const ind = indent(l);
    while (stack.length > 1 && stack[stack.length - 1].ind >= ind) stack.pop();
    const parent = stack[stack.length - 1].node;
    const node: MindNode = { text: clean(l), children: [], id: `${parent.id}.${parent.children.length}` };
    parent.children.push(node);
    stack.push({ node, ind });
  }
  if (count(root) > MAX_NODES) problems.push({ key: "mindmap.err_too_many", params: { max: MAX_NODES } });
  return problems.length ? { problems } : { model: root, problems };
}

function count(n: MindNode): number {
  return 1 + n.children.reduce((s, c) => s + count(c), 0);
}

const BRANCH = ["blue", "green", "orange", "purple", "pink", "red", "yellow"];
const FONT = 13, ROW = 30, GAP_X = 36;

/** A rough text width (no DOM): wide letters count more. */
export function textWidth(s: string, size = FONT): number {
  let w = 0;
  for (const ch of s) w += /[MWmw@%]/.test(ch) ? 0.9 : /[A-ZÄÖÜ0-9]/.test(ch) ? 0.68 : /[iljtf.,:;'|!]/.test(ch) ? 0.3 : 0.55;
  return w * size;
}

export interface Placed { node: MindNode; x: number; y: number; w: number; h: number; side: -1 | 0 | 1; color: string; depth: number; parent?: Placed; hidden: number }

/** Lays the map out: the root in the middle, branches split by size to the left and right. */
export function layoutMindmap(root: MindNode, collapsed: Set<string> = new Set()): { nodes: Placed[]; width: number; height: number } {
  const kids = (n: MindNode) => (collapsed.has(n.id) ? [] : n.children);
  const leaves = (n: MindNode): number => (kids(n).length ? kids(n).reduce((s, c) => s + leaves(c), 0) : 1);
  const branches = root.children;
  // Balance: fill the right side up to half of all leaves, the rest goes left.
  const total = branches.reduce((s, b) => s + leaves(b), 0);
  const right: MindNode[] = [], left: MindNode[] = [];
  let acc = 0;
  for (const b of branches) {
    if (acc < total / 2 || !right.length) { right.push(b); acc += leaves(b); } else left.push(b);
  }
  const boxW = (n: MindNode, d: number) => Math.min(240, textWidth(n.text, d === 0 ? 16 : FONT) + (d === 0 ? 32 : 20));
  // Column widths per depth and side.
  const colW: Record<string, number> = {};
  const measure = (n: MindNode, d: number, side: number) => {
    const k = `${side}:${d}`;
    colW[k] = Math.max(colW[k] ?? 0, boxW(n, d));
    for (const c of kids(n)) measure(c, d + 1, side);
  };
  right.forEach((b) => measure(b, 1, 1));
  left.forEach((b) => measure(b, 1, -1));
  const rootW = boxW(root, 0), rootH = 40;
  const colX = (side: number, d: number) => {
    let x = rootW / 2 + GAP_X;
    for (let i = 1; i < d; i++) x += (colW[`${side}:${i}`] ?? 0) + GAP_X;
    return x;
  };
  const nodes: Placed[] = [];
  const place = (n: MindNode, d: number, side: -1 | 1, top: number, color: string, parent: Placed): number => {
    const h = leaves(n) * ROW;
    const w = boxW(n, d);
    const off = colX(side, d);
    const p: Placed = { node: n, x: side === 1 ? off : -off - w, y: top + h / 2 - 12, w, h: 24, side, color, depth: d, parent, hidden: collapsed.has(n.id) ? count(n) - 1 : 0 };
    nodes.push(p);
    let y = top;
    for (const c of kids(n)) y += place(c, d + 1, side, y, color, p);
    return h;
  };
  const rp: Placed = { node: root, x: -rootW / 2, y: -rootH / 2, w: rootW, h: rootH, side: 0, color: "gray", depth: 0, hidden: 0 };
  nodes.push(rp);
  for (const [list, side] of [[right, 1], [left, -1]] as const) {
    const h = list.reduce((s, b) => s + leaves(b) * ROW, 0);
    let y = -h / 2;
    list.forEach((b) => {
      const color = BRANCH[branches.indexOf(b) % BRANCH.length];
      y += place(b, 1, side, y, color, rp);
    });
  }
  // Shift into positive coordinates with a margin.
  const minX = Math.min(...nodes.map((n) => n.x)) - 8, minY = Math.min(...nodes.map((n) => n.y)) - 8;
  const maxX = Math.max(...nodes.map((n) => n.x + n.w)) + 8, maxY = Math.max(...nodes.map((n) => n.y + n.h)) + 8;
  for (const n of nodes) { n.x -= minX; n.y -= minY; }
  return { nodes, width: maxX - minX, height: maxY - minY };
}

/** The map as SVG; interactive adds focusable toggles for branches with children. */
export function mindmapSVG(root: MindNode, opts: { width?: number; collapsed?: Set<string>; interactive?: boolean } = {}): string {
  const { nodes, width, height } = layoutMindmap(root, opts.collapsed);
  const scale = opts.width && opts.width < width ? opts.width / width : 1;
  const links = nodes.filter((n) => n.parent).map((n) => {
    const p = n.parent!;
    const x1 = n.side === 1 ? p.x + p.w : p.x, y1 = p.y + p.h / 2;
    const x2 = n.side === 1 ? n.x : n.x + n.w, y2 = n.y + n.h / 2;
    const mx = (x1 + x2) / 2;
    return `<path d="M${x1} ${y1} C${mx} ${y1} ${mx} ${y2} ${x2} ${y2}" fill="none" style="stroke:var(--tc-${n.color})" stroke-width="${n.depth === 1 ? 2.5 : 1.5}"/>`;
  }).join("");
  const boxes = nodes.map((n) => {
    const toggle = opts.interactive && n.depth > 0 && n.node.children.length;
    const root = n.depth === 0;
    const label = esc(n.node.text);
    const attrs = toggle
      ? ` role="button" tabindex="0" data-id="${esc(n.node.id)}" aria-expanded="${!n.hidden}" aria-label="${label}${n.hidden ? ` (${esc(t("mindmap.hidden", { n: n.hidden }))})` : ""}" style="cursor:pointer"`
      : "";
    const fill = root ? "var(--surface2)" : n.depth === 1 ? `var(--tb-${n.color})` : "var(--surface)";
    const stroke = root ? "var(--line)" : `var(--tc-${n.color})`;
    return `<g${attrs}><rect x="${n.x}" y="${n.y}" width="${n.w}" height="${n.h}" rx="${root ? 12 : 8}" style="fill:${fill};stroke:${stroke}" stroke-width="${root ? 1.5 : 1}"/>` +
      `<text x="${n.x + n.w / 2}" y="${n.y + n.h / 2 + (root ? 5.5 : 4.5)}" text-anchor="middle" style="font-size:${root ? 16 : FONT}px;${root || n.depth === 1 ? "font-weight:600;" : ""}fill:var(--fg)">${label.length > 60 ? label.slice(0, 58) + "…" : label}</text>` +
      (n.hidden ? `<g><circle cx="${n.side === -1 ? n.x - 10 : n.x + n.w + 10}" cy="${n.y + n.h / 2}" r="9" style="fill:var(--tc-${n.color})"/><text x="${n.side === -1 ? n.x - 10 : n.x + n.w + 10}" y="${n.y + n.h / 2 + 3.5}" text-anchor="middle" style="font-size:10px;font-weight:600;fill:var(--bg)">${n.hidden > 99 ? "99+" : n.hidden}</text></g>` : "") +
      `</g>`;
  }).join("");
  // 20px each side for the "+n" badges of collapsed branches.
  const vw = Math.ceil(width) + 40, vh = Math.ceil(height);
  return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="-20 0 ${vw} ${vh}" width="${Math.ceil(vw * scale)}" height="${Math.ceil(vh * scale)}" role="img" aria-label="${esc(root.text)}" style="font-family:var(--font-body,system-ui,sans-serif);max-width:100%;height:auto">${links}${boxes}</svg>`;
}

/** The map as whiteboard elements: rounded boxes and bound lines (copy and paste onto a board). */
export function mindmapElements(root: MindNode, collapsed: Set<string> = new Set()) {
  const { nodes } = layoutMindmap(root, collapsed);
  const uid = Math.random().toString(36).slice(2, 8);
  const id = (n: Placed) => `mm-${uid}-${n.node.id.replace(/\./g, "-")}`;
  const els: Record<string, unknown>[] = [];
  for (const n of nodes) {
    els.push({ id: id(n), type: n.depth === 0 ? "ellipse" : "rect", x: Math.round(n.x * 1.4), y: Math.round(n.y * 1.6), w: Math.round(n.w * 1.25), h: n.depth === 0 ? 56 : 36, text: n.node.text, stroke: n.depth === 0 ? "gray" : n.color, fill: n.depth === 1 ? n.color : undefined, round: true, fontSize: n.depth === 0 ? 20 : 16 });
  }
  for (const n of nodes) {
    if (!n.parent) continue;
    const a = els.find((e) => e.id === id(n.parent!))!, b = els.find((e) => e.id === id(n))!;
    const cx = (e: Record<string, unknown>) => (e.x as number) + (e.w as number) / 2, cy = (e: Record<string, unknown>) => (e.y as number) + (e.h as number) / 2;
    els.push({ id: `${id(n)}-l`, type: "line", points: [[cx(a), cy(a)], [cx(b), cy(b)]], stroke: n.color, start: { element: a.id }, end: { element: b.id }, smooth: true });
  }
  return els;
}

registerBlock<MindNode>({
  kind: "mindmap",
  icon: "Network",
  label: "slash.mindmap",
  hint: "block.mindmap_hint",
  example: "Umzug\n  Wohnung\n    Kündigen\n    Übergabe mit Protokoll\n  Internet\n    Vertrag umziehen\n    Router mitnehmen\n  Kartons\n    Küche\n    Keller",
  parse: parseMindmap,
  svg: (m, s) => mindmapSVG(m, { width: s.width }),
  board: { w: 640, h: 380 },
  async mount(host, m) {
    const collapsed = new Set<string>();
    const style = document.createElement("style");
    style.textContent = `${BLOCK_CSS}
      .map { overflow-x: auto; text-align: center; }
      .map svg { max-width: none; }
      .map g[role=button]:focus-visible rect { stroke: var(--accent); stroke-width: 2.5px; }
      @media (hover: hover) and (pointer: fine) { .map g[role=button]:hover rect { filter: brightness(0.97); } }
      .foot { display: flex; gap: 8px; margin-top: 8px; flex-wrap: wrap; align-items: center; }`;
    const box = document.createElement("div");
    box.className = "blk";
    host.replaceChildren(style, box);
    const draw = (focus?: string) => {
      box.innerHTML = `<div class="map">${mindmapSVG(m, { collapsed, interactive: true })}</div>
        <div class="foot">${collapsed.size ? `<button type="button" class="ghost" data-all>${esc(t("mindmap.expand_all"))}</button>` : ""}
        <button type="button" class="ghost" data-copy>${esc(t("mindmap.copy_board"))}</button><span class="muted" data-note aria-live="polite"></span></div>`;
      if (focus) box.querySelector<SVGGElement>(`g[data-id="${CSS.escape(focus)}"]`)?.focus();
    };
    const toggle = (id: string) => {
      if (collapsed.has(id)) collapsed.delete(id);
      else collapsed.add(id);
      draw(id);
    };
    box.addEventListener("click", async (e) => {
      const el = e.target as Element;
      const g = el.closest<SVGGElement>("g[data-id]");
      if (g) return toggle(g.dataset.id!);
      if (el.closest("[data-all]")) { collapsed.clear(); draw(); return; }
      if (el.closest("[data-copy]")) {
        const { CLIP_PREFIX } = await import("../whiteboard");
        const text = CLIP_PREFIX + JSON.stringify(mindmapElements(m, collapsed));
        const note = box.querySelector<HTMLElement>("[data-note]")!;
        try {
          await navigator.clipboard.writeText(text);
          note.textContent = t("mindmap.copied");
        } catch {
          note.textContent = t("mindmap.copy_failed");
        }
      }
    });
    box.addEventListener("keydown", (e) => {
      const g = (e.target as Element).closest<SVGGElement>("g[data-id]");
      if (g && (e.key === "Enter" || e.key === " ")) { e.preventDefault(); toggle(g.dataset.id!); }
    });
    draw();
  },
});
