/** A force-directed layout (Fruchterman–Reingold), computed once without animation. */
export interface Pos { x: number; y: number }

export function layout(ids: string[], links: [string, string][], opts: { width?: number; height?: number; iterations?: number; seed?: number; k?: number } = {}): Map<string, Pos> {
  const n = ids.length;
  const W = opts.width ?? 1000, H = opts.height ?? 700;
  const pos = new Map<string, Pos>();
  if (n === 0) return pos;
  // Deterministic start on a spiral so the same graph looks the same each time.
  let seed = opts.seed ?? 7;
  const rnd = () => ((seed = (seed * 16807) % 2147483647) / 2147483647);
  ids.forEach((id, i) => {
    const a = i * 2.399963, r = 18 * Math.sqrt(i + 1);
    pos.set(id, { x: W / 2 + Math.cos(a) * r + rnd(), y: H / 2 + Math.sin(a) * r + rnd() });
  });
  if (n === 1) return pos;
  // Ideal edge length in layout units; the view is fitted to the result, so this sets node spacing.
  const k = opts.k ?? 90;
  const index = new Map(ids.map((id, i) => [id, i]));
  const P = ids.map((id) => pos.get(id)!);
  const E = links.map(([a, b]) => [index.get(a), index.get(b)]).filter(([a, b]) => a !== undefined && b !== undefined && a !== b) as [number, number][];
  const iters = opts.iterations ?? Math.max(60, Math.min(300, Math.round(60000 / n)));
  let temp = k * 2;
  const far2 = (3 * k) * (3 * k);
  const dx = new Float64Array(n), dy = new Float64Array(n);
  for (let it = 0; it < iters; it++) {
    dx.fill(0); dy.fill(0);
    for (let i = 0; i < n; i++) {
      for (let j = i + 1; j < n; j++) {
        let ddx = P[i].x - P[j].x, ddy = P[i].y - P[j].y;
        let d2 = ddx * ddx + ddy * ddy;
        if (d2 < 0.01) { ddx = rnd() - 0.5; ddy = rnd() - 0.5; d2 = 0.01; }
        if (d2 > far2) continue; // far apart: no repulsion, keeps components together
        const f = (k * k) / d2;
        dx[i] += ddx * f; dy[i] += ddy * f; dx[j] -= ddx * f; dy[j] -= ddy * f;
      }
    }
    for (const [a, b] of E) {
      const ddx = P[a].x - P[b].x, ddy = P[a].y - P[b].y;
      const d = Math.sqrt(ddx * ddx + ddy * ddy) || 0.01;
      const f = d / k;
      dx[a] -= ddx * f; dy[a] -= ddy * f; dx[b] += ddx * f; dy[b] += ddy * f;
    }
    for (let i = 0; i < n; i++) {
      // Gentle pull to the center keeps unconnected parts on screen.
      dx[i] += (W / 2 - P[i].x) * 0.05; dy[i] += (H / 2 - P[i].y) * 0.05;
      const d = Math.sqrt(dx[i] * dx[i] + dy[i] * dy[i]) || 1;
      const s = Math.min(d, temp) / d;
      P[i].x += dx[i] * s; P[i].y += dy[i] * s;
    }
    temp = Math.max(1, temp * 0.97);
  }
  return pos;
}

/** Bounding box of positions with padding. */
export function bounds(pos: Iterable<Pos>, pad = 60) {
  let x0 = Infinity, y0 = Infinity, x1 = -Infinity, y1 = -Infinity;
  for (const p of pos) { x0 = Math.min(x0, p.x); y0 = Math.min(y0, p.y); x1 = Math.max(x1, p.x); y1 = Math.max(y1, p.y); }
  if (!isFinite(x0)) return { x: 0, y: 0, w: 100, h: 100 };
  return { x: x0 - pad, y: y0 - pad, w: x1 - x0 + 2 * pad, h: y1 - y0 + 2 * pad };
}
