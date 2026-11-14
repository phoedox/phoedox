// Network plan layout: a tidy tree from the gateways (routers) down.
// An edge points from a station to the one it connects through.

export interface TopoNode { slug: string; name: string; kind: string; status: "ok" | "warn" | "down" | "unknown"; ip?: string }
export interface TopoEdge { from: string; to: string; rel: string }
export interface Placed extends TopoNode { x: number; y: number; depth: number }
export interface Layout { nodes: Placed[]; edges: (TopoEdge & { tree: boolean })[]; width: number; height: number }

export const COL = 150;
export const ROW = 120;

export function layoutTopology(nodes: TopoNode[], edges: TopoEdge[]): Layout {
  const bySlug = new Map(nodes.map((n) => [n.slug, n]));
  const valid = edges.filter((e) => bySlug.has(e.from) && bySlug.has(e.to) && e.from !== e.to);
  // Each station hangs below its first upstream station (uplink before wired_to before runs_on).
  const order = { part_of: -1, uplink: 0, wired_to: 1, runs_on: 2 } as Record<string, number>;
  const parent = new Map<string, string>();
  for (const e of [...valid].sort((a, b) => (order[a.rel] ?? 9) - (order[b.rel] ?? 9))) {
    if (!parent.has(e.from)) parent.set(e.from, e.to);
  }
  // Break cycles: walk up; a station that reaches itself loses its parent.
  for (const n of nodes) {
    const seen = new Set<string>([n.slug]);
    let cur = parent.get(n.slug);
    while (cur) {
      if (seen.has(cur)) {
        parent.delete(n.slug);
        break;
      }
      seen.add(cur);
      cur = parent.get(cur);
    }
  }
  const children = new Map<string, string[]>();
  for (const [c, p] of parent) children.set(p, [...(children.get(p) ?? []), c]);
  for (const list of children.values()) list.sort((a, b) => bySlug.get(a)!.name.localeCompare(bySlug.get(b)!.name));
  const gateway = (k: string) => k === "router" || k === "modem";
  const roots = nodes.filter((n) => !parent.has(n.slug))
    .sort((a, b) => Number(gateway(b.kind)) - Number(gateway(a.kind)) || (children.get(b.slug)?.length ?? 0) - (children.get(a.slug)?.length ?? 0) || a.name.localeCompare(b.name));

  const placed = new Map<string, Placed>();
  let cursor = 0;
  let maxDepth = 0;
  // Leaves take one column each; parents sit above the middle of their children.
  const place = (slug: string, depth: number): number => {
    maxDepth = Math.max(maxDepth, depth);
    const kids = children.get(slug) ?? [];
    let x: number;
    if (!kids.length) {
      x = cursor;
      cursor += 1;
    } else {
      const xs = kids.map((k) => place(k, depth + 1));
      x = (xs[0] + xs[xs.length - 1]) / 2;
    }
    placed.set(slug, { ...bySlug.get(slug)!, depth, x: x * COL + COL / 2, y: depth * ROW + ROW / 2 });
    return x;
  }
  for (const r of roots) {
    place(r.slug, 0);
    cursor += 0.5; // a little air between separate trees
  }
  return {
    nodes: [...placed.values()],
    edges: valid.map((e) => ({ ...e, tree: parent.get(e.from) === e.to })),
    width: Math.max(1, Math.ceil(cursor - 0.5)) * COL,
    height: (maxDepth + 1) * ROW,
  };
}
