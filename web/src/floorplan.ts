// Floor plans (ADR-0034): data and geometry. All lengths in millimetres.

export type Pt = [number, number];
export interface FloorRoom { id: string; name: string; entity?: string; color?: string; points: Pt[] }
export interface FloorDevice { id: string; entity: string; x: number; y: number }
export interface Floorplan {
  description?: string; floor?: string; image?: string; imageWidth?: number;
  width: number; height: number; rooms: FloorRoom[]; devices: FloorDevice[];
}

export const ROOM_COLORS = ["gray", "red", "orange", "yellow", "green", "blue", "purple", "pink"] as const;

export function normalizePlan(c: Partial<Floorplan> | undefined): Floorplan {
  return {
    ...c, width: c?.width || 20000, height: c?.height || 15000,
    rooms: (c?.rooms ?? []).map((r) => ({ ...r, points: r.points.map((p) => [p[0], p[1]] as Pt) })),
    devices: (c?.devices ?? []).map((d) => ({ ...d })),
  };
}

export const newId = (prefix: string) => `${prefix}${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`;

/** Polygon area in mm² (shoelace). */
export function area(pts: Pt[]): number {
  let a = 0;
  for (let i = 0; i < pts.length; i++) {
    const j = (i + 1) % pts.length;
    a += pts[i][0] * pts[j][1] - pts[j][0] * pts[i][1];
  }
  return Math.abs(a) / 2;
}

/** Area in m² with one decimal, like the server stores it. */
export const areaM2 = (pts: Pt[]) => Math.round(area(pts) / 1e5) / 10;

export function inside(x: number, y: number, pts: Pt[]): boolean {
  let hit = false;
  for (let i = 0, j = pts.length - 1; i < pts.length; j = i++) {
    const [xi, yi] = pts[i];
    const [xj, yj] = pts[j];
    if ((yi > y) !== (yj > y) && x < ((xj - xi) * (y - yi)) / (yj - yi) + xi) hit = !hit;
  }
  return hit;
}

/** The smallest room containing the point (rooms may overlap). */
export function roomAt(plan: Floorplan, x: number, y: number): FloorRoom | undefined {
  let best: FloorRoom | undefined;
  let bestArea = Infinity;
  for (const r of plan.rooms) {
    if (r.points.length >= 3 && inside(x, y, r.points)) {
      const a = area(r.points);
      if (a < bestArea) [best, bestArea] = [r, a];
    }
  }
  return best;
}

/** A point well inside the polygon for labels: the centroid when it lies
 * inside, else the middle of the widest horizontal span. */
export function labelPoint(pts: Pt[]): Pt {
  let a = 0, cx = 0, cy = 0;
  for (let i = 0; i < pts.length; i++) {
    const [x0, y0] = pts[i];
    const [x1, y1] = pts[(i + 1) % pts.length];
    const f = x0 * y1 - x1 * y0;
    a += f; cx += (x0 + x1) * f; cy += (y0 + y1) * f;
  }
  if (Math.abs(a) > 1e-9 && inside(cx / (3 * a), cy / (3 * a), pts)) return [cx / (3 * a), cy / (3 * a)];
  const ys = pts.map((p) => p[1]);
  const [minY, maxY] = [Math.min(...ys), Math.max(...ys)];
  let best: Pt = pts[0];
  let bestW = -1;
  for (let k = 1; k < 20; k++) {
    const y = minY + ((maxY - minY) * k) / 20;
    const xs: number[] = [];
    for (let i = 0, j = pts.length - 1; i < pts.length; j = i++) {
      const [xi, yi] = pts[i];
      const [xj, yj] = pts[j];
      if ((yi > y) !== (yj > y)) xs.push(((xj - xi) * (y - yi)) / (yj - yi) + xi);
    }
    xs.sort((p, q) => p - q);
    for (let i = 0; i + 1 < xs.length; i += 2) {
      if (xs[i + 1] - xs[i] > bestW) [bestW, best] = [xs[i + 1] - xs[i], [(xs[i] + xs[i + 1]) / 2, y]];
    }
  }
  return best;
}

/** Snaps to a grid (mm); 0 = off. */
export const snap = (v: number, grid: number) => (grid > 0 ? Math.round(v / grid) * grid : Math.round(v));

/** Holding Shift keeps a new wall horizontal or vertical. */
export function orthogonal(from: Pt, to: Pt): Pt {
  return Math.abs(to[0] - from[0]) >= Math.abs(to[1] - from[1]) ? [to[0], from[1]] : [from[0], to[1]];
}

export const distance = (a: Pt, b: Pt) => Math.hypot(a[0] - b[0], a[1] - b[1]);

/** New image width after measuring a known distance on the image. */
export function calibrate(imageWidth: number, measured: number, realMm: number): number {
  if (!(measured > 0) || !(realMm > 0)) return imageWidth;
  return Math.round((imageWidth * realMm) / measured);
}

/** Formats millimetres as metres for people ("4,25 m"). */
export const metres = (mm: number, locale: string) =>
  `${(mm / 1000).toLocaleString(locale === "de" ? "de-DE" : "en-GB", { maximumFractionDigits: 2 })} m`;
