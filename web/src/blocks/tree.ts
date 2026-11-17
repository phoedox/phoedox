// Trees from the knowledge graph: an entry and everything linked to it by
// one relation, laid out top-down – a family, people and teams, or a network
// (router → switches → devices). Each box shows the entry's live status and
// opens it.
import { t } from "../i18n";
import { registerBlock, type BlockProblem } from "./registry";
import { BLOCK_CSS, blockLines, esc, keyValue, liveStatus, openEntry, statusColor } from "./util";

export interface TreeSpec { root: string; relation: string; direction: "in" | "out"; depth: number; title: string }
export interface TreeNode { id: string; slug: string; name: string; kind: string; children: TreeNode[] }

const SLUG = /^@?([a-z0-9]+(?:-[a-z0-9]+)*)$/;

export function parseTree(src: string): { model?: TreeSpec; problems: BlockProblem[] } {
  const m: TreeSpec = { root: "", relation: "", direction: "in", depth: 4, title: "" };
  const problems: BlockProblem[] = [];
  for (const l of blockLines(src)) {
    const kv = keyValue(l, ["root", "wurzel", "relation", "beziehung", "direction", "richtung", "depth", "tiefe", "title", "titel"]);
    if (!kv) {
      // A bare "@slug" line is the root.
      const s = SLUG.exec(l);
      if (s && !m.root) { m.root = s[1]; continue; }
      problems.push({ key: "tree.err_line", params: { line: l } });
      continue;
    }
    const [k, v] = kv;
    if (k === "root" || k === "wurzel") {
      const s = SLUG.exec(v);
      if (s) m.root = s[1]; else problems.push({ key: "tree.err_root", params: { value: v } });
    } else if (k === "relation" || k === "beziehung") m.relation = v.trim();
    else if (k === "direction" || k === "richtung") {
      if (/^(in|ein|eingehend|up)$/i.test(v)) m.direction = "in";
      else if (/^(out|aus|ausgehend|down)$/i.test(v)) m.direction = "out";
      else problems.push({ key: "tree.err_direction", params: { value: v } });
    } else if (k === "depth" || k === "tiefe") m.depth = Math.min(8, Math.max(1, Number(v) || 4));
    else m.title = v;
  }
  if (!m.root) problems.push({ key: "tree.err_root", params: { value: "" } });
  return problems.length ? { problems } : { model: m, problems };
}

/** Builds the tree from traverse hops (each hop knows the entry it was reached from). */
export function buildTree(root: { id: string; slug: string; name: string; kind: string }, hops: { entity: { id: string; slug: string; name: string; kind: string }; parent?: string }[]): TreeNode {
  const top: TreeNode = { ...root, children: [] };
  const byId = new Map<string, TreeNode>([[root.id, top]]);
  for (const h of hops) {
    const n: TreeNode = { id: h.entity.id, slug: h.entity.slug, name: h.entity.name, kind: h.entity.kind, children: [] };
    byId.set(n.id, n);
  }
  for (const h of hops) (byId.get(h.parent ?? "") ?? top).children.push(byId.get(h.entity.id)!);
  const sort = (n: TreeNode) => { n.children.sort((a, b) => a.name.localeCompare(b.name)); n.children.forEach(sort); };
  sort(top);
  return top;
}

export interface TreeBox { node: TreeNode; x: number; y: number; w: number; h: number; parent?: TreeBox }

const BOX_W = 150, BOX_H = 44, GAP_X = 16, GAP_Y = 46;

/** Top-down layout: leaves side by side, parents centred over their children. */
export function layoutTree(root: TreeNode): { boxes: TreeBox[]; width: number; height: number } {
  const boxes: TreeBox[] = [];
  let next = 0;
  let maxDepth = 0;
  const place = (n: TreeNode, depth: number, parent?: TreeBox): TreeBox => {
    maxDepth = Math.max(maxDepth, depth);
    const b: TreeBox = { node: n, x: 0, y: depth * (BOX_H + GAP_Y), w: BOX_W, h: BOX_H, parent };
    boxes.push(b);
    if (!n.children.length) {
      b.x = next;
      next += BOX_W + GAP_X;
    } else {
      const kids = n.children.map((c) => place(c, depth + 1, b));
      b.x = (kids[0].x + kids[kids.length - 1].x) / 2;
    }
    return b;
  };
  place(root, 0);
  return { boxes, width: Math.max(BOX_W, next - GAP_X), height: (maxDepth + 1) * (BOX_H + GAP_Y) - GAP_Y };
}

export function treeSVG(root: TreeNode, project: string, status: Map<string, { status: string }> = new Map(), interactive = false): string {
  const { boxes, width, height } = layoutTree(root);
  const pad = 8;
  const links = boxes.filter((b) => b.parent).map((b) => {
    const p = b.parent!;
    const x1 = p.x + p.w / 2, y1 = p.y + p.h, x2 = b.x + b.w / 2, y2 = b.y, my = (y1 + y2) / 2;
    return `<path d="M${x1} ${y1} V${my} H${x2} V${y2}" fill="none" style="stroke:var(--muted)" stroke-width="1.3"/>`;
  }).join("");
  const nodes = boxes.map((b) => {
    const st = status.get(b.node.slug)?.status;
    const name = esc(b.node.name.length > 20 ? b.node.name.slice(0, 19) + "…" : b.node.name);
    const inner = `<rect x="${b.x}" y="${b.y}" width="${b.w}" height="${b.h}" rx="8" style="fill:var(--surface);stroke:var(--line)"/>` +
      `<rect x="${b.x}" y="${b.y + 6}" width="4" height="${b.h - 12}" rx="2" style="fill:${statusColor(st)}"/>` +
      `<text x="${b.x + b.w / 2}" y="${b.y + 19}" text-anchor="middle" style="font-size:13px;font-weight:600;fill:var(--fg)">${name}</text>` +
      `<text x="${b.x + b.w / 2}" y="${b.y + 35}" text-anchor="middle" style="font-size:11px;fill:var(--muted)">${esc(b.node.kind)}${st && st !== "unknown" ? ` · ${esc(t(`diag.status.${st}` as never))}` : ""}</text>`;
    return interactive && project
      ? `<a href="/p/${esc(project)}/e/${esc(b.node.slug)}" data-entity aria-label="${esc(b.node.name)}">${inner}</a>`
      : `<g>${inner}</g>`;
  }).join("");
  return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="${-pad} ${-pad} ${width + 2 * pad} ${height + 2 * pad}" width="${width + 2 * pad}" height="${height + 2 * pad}" role="img" aria-label="${esc(root.name)}" style="font-family:var(--font-body,system-ui,sans-serif);max-width:none">${links}${nodes}</svg>`;
}

/** Placeholder picture before the data is loaded (whiteboards, exports). */
function treeSketch(s: TreeSpec, width: number): string {
  return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${width} 70" width="${width}" height="70" style="font-family:var(--font-body,system-ui,sans-serif)"><text x="6" y="24" style="font-size:15px;font-weight:600;fill:var(--fg)">${esc(s.title || "@" + s.root)}</text><text x="6" y="48" style="font-size:12px;fill:var(--muted)">${esc(t("tree.sketch", { root: s.root, relation: s.relation || "*" }))}</text></svg>`;
}

registerBlock<TreeSpec>({
  kind: "tree",
  icon: "ListTree",
  label: "slash.tree",
  hint: "block.tree_hint",
  example: "root: @router\nrelation: wired_to\ndirection: in\ndepth: 4",
  parse: parseTree,
  svg: (s, size) => treeSketch(s, size.width),
  board: { w: 640, h: 360, live: true },
  async mount(host, s, ctx) {
    const style = document.createElement("style");
    style.textContent = `${BLOCK_CSS}
      .tree { overflow: auto; max-height: 70vh; }
      .tree a[data-entity] rect:first-child { transition: stroke var(--dur, 150ms) var(--ease-out, ease-out); }
      @media (hover: hover) and (pointer: fine) { .tree a[data-entity]:hover rect:first-child { stroke: var(--accent); } }
      .tree a[data-entity]:focus-visible { outline: none; }
      .tree a[data-entity]:focus-visible rect:first-child { stroke: var(--accent); stroke-width: 2.5px; }
      .sr { position: absolute; width: 1px; height: 1px; overflow: hidden; clip: rect(0 0 0 0); }`;
    const box = document.createElement("div");
    box.className = "blk";
    host.replaceChildren(style, box);
    box.innerHTML = `${s.title ? `<h3>${esc(s.title)}</h3>` : ""}<p class="muted">${esc(t("tree.loading"))}</p>`;
    if (!ctx.project) return;
    const { api, ApiError } = await import("../api");
    let timer = 0;
    const load = async () => {
      try {
        const d = await api<{ entity: { id: string; slug: string; name: string; kind: string } }>("GET", `/projects/${ctx.project}/entities/${encodeURIComponent(s.root)}`);
        const q = new URLSearchParams({ dir: s.direction, depth: String(s.depth) });
        if (s.relation) q.set("rel", s.relation);
        const hops = await api<{ entity: { id: string; slug: string; name: string; kind: string }; parent?: string }[]>("GET", `/projects/${ctx.project}/entities/${encodeURIComponent(s.root)}/traverse?${q}`);
        const root = buildTree(d.entity, hops ?? []);
        const status = await liveStatus(ctx.project, [root.slug, ...(hops ?? []).map((h) => h.entity.slug)]);
        // A nested list for screen readers; the picture is aria-hidden.
        const list = (n: TreeNode): string => `<li>${esc(n.name)} (${esc(n.kind)}${status.get(n.slug) ? `, ${esc(t(`diag.status.${status.get(n.slug)!.status}` as never))}` : ""})${n.children.length ? `<ul>${n.children.map(list).join("")}</ul>` : ""}</li>`;
        box.innerHTML = `${s.title ? `<h3>${esc(s.title)}</h3>` : ""}<div class="tree">${treeSVG(root, ctx.project, status, true)}</div>
          ${!hops?.length ? `<p class="muted">${esc(t("tree.empty", { relation: s.relation || "*" }))}</p>` : ""}<ul class="sr">${list(root)}</ul>`;
        box.querySelector("svg")?.setAttribute("aria-hidden", "true");
      } catch (e) {
        box.innerHTML = `${s.title ? `<h3>${esc(s.title)}</h3>` : ""}<p class="muted">${esc(e instanceof ApiError && e.status === 404 ? t("tree.missing", { root: s.root }) : t("tree.failed"))}</p>`;
      }
    };
    box.addEventListener("click", (e) => {
      const a = (e.target as Element).closest<SVGAElement>("a[data-entity]");
      if (a) void openEntry(e, a.getAttribute("href")!);
    });
    await load();
    // Status changes: look again every minute while the page is visible.
    timer = window.setInterval(() => document.visibilityState === "visible" && void load(), 60_000);
    return () => clearInterval(timer);
  },
});
