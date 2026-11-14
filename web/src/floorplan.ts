// Floor plans (ADR-0034): data and geometry. All lengths in millimetres.

export type Pt = [number, number];
export interface FloorRoom { id: string; name: string; entity?: string; color?: string; points: Pt[] }
export interface FloorDevice { id: string; entity: string; x: number; y: number }
/** A straight wall along its centre line. */
export interface FloorWall { id: string; a: Pt; b: Pt; thickness: number; height?: number }
/** A door or window; offset is the distance from the wall start to its centre. */
export interface FloorOpening { id: string; wall: string; kind: "door" | "window"; offset: number; width: number }
export interface FloorPhoto { id: string; file: string; x: number; y: number; caption?: string }
/** A cable route: [x, y, height above this floor] in mm; from/to are entries at its ends. */
export type Pt3 = [number, number, number];
export type CableKind = "network" | "power" | "coax" | "other";
export interface FloorCable { id: string; kind: CableKind; label?: string; points: Pt3[]; from?: string; to?: string; entity?: string }
export const CABLE_KINDS: CableKind[] = ["network", "power", "coax", "other"];
export interface Floorplan {
  description?: string; floor?: string; image?: string; imageWidth?: number;
  width: number; height: number; rooms: FloorRoom[]; devices: FloorDevice[];
  walls: FloorWall[]; openings: FloorOpening[]; photos: FloorPhoto[]; cables: FloorCable[];
}

export const WALL_THICKNESS = 120;
export const DOOR_WIDTH = 900;
export const WINDOW_WIDTH = 1200;

export const ROOM_COLORS = ["gray", "red", "orange", "yellow", "green", "blue", "purple", "pink"] as const;

export function normalizePlan(c: Partial<Floorplan> | undefined): Floorplan {
  return {
    ...c, width: c?.width || 20000, height: c?.height || 15000,
    rooms: (c?.rooms ?? []).map((r) => ({ ...r, points: r.points.map((p) => [p[0], p[1]] as Pt) })),
    devices: (c?.devices ?? []).map((d) => ({ ...d })),
    walls: (c?.walls ?? []).map((w) => ({ ...w, a: [w.a[0], w.a[1]] as Pt, b: [w.b[0], w.b[1]] as Pt, thickness: w.thickness || WALL_THICKNESS })),
    openings: (c?.openings ?? []).map((o) => ({ ...o })),
    photos: (c?.photos ?? []).map((p) => ({ ...p })),
    cables: (c?.cables ?? []).map((k) => ({ ...k, kind: k.kind || "network", points: k.points.map((q) => [q[0], q[1], q[2] ?? 0] as Pt3) })),
  };
}

/** The plan as the server stores it: empty stage-2 lists are left out. */
export function planForSave(p: Floorplan): Partial<Floorplan> {
  const out: Partial<Floorplan> = { ...p };
  for (const k of ["walls", "openings", "photos", "cables"] as const) if (!p[k].length) delete out[k];
  return out;
}

// ---- walls ----------------------------------------------------------------

export const wallLength = (w: Pick<FloorWall, "a" | "b">) => distance(w.a, w.b);

/** Where a point falls on a wall: offset along it (clamped) and distance from it. */
export function onWall(w: Pick<FloorWall, "a" | "b">, p: Pt): { offset: number; dist: number; at: Pt } {
  const len = wallLength(w);
  const [dx, dy] = [(w.b[0] - w.a[0]) / len, (w.b[1] - w.a[1]) / len];
  const offset = Math.min(len, Math.max(0, (p[0] - w.a[0]) * dx + (p[1] - w.a[1]) * dy));
  const at: Pt = [w.a[0] + dx * offset, w.a[1] + dy * offset];
  return { offset, dist: distance(at, p), at };
}

/** The wall closest to a point within reach (mm, beyond its half thickness), if any. */
export function nearestWall(plan: Floorplan, p: Pt, reach: number): { wall: FloorWall; offset: number } | undefined {
  let best: { wall: FloorWall; offset: number } | undefined;
  let bestDist = Infinity;
  for (const w of plan.walls) {
    const r = onWall(w, p);
    if (r.dist <= reach + w.thickness / 2 && r.dist < bestDist) [best, bestDist] = [{ wall: w, offset: r.offset }, r.dist];
  }
  return best;
}

/** Keeps an opening inside its wall. */
export function clampOpening(len: number, offset: number, width: number): number {
  const half = Math.min(width, len) / 2;
  return Math.min(len - half, Math.max(half, offset));
}

/** Snaps to the nearest wall end within reach, so walls connect. */
export function snapToEnds(plan: Floorplan, p: Pt, reach: number, skip?: string): Pt | undefined {
  let best: Pt | undefined;
  let bestDist = reach;
  for (const w of plan.walls) {
    if (w.id === skip) continue;
    for (const e of [w.a, w.b]) {
      const d = distance(e, p);
      if (d < bestDist) [best, bestDist] = [e, d];
    }
  }
  return best;
}

/** The four corners of a wall drawn with its thickness. */
export function wallOutline(w: FloorWall): Pt[] {
  const len = wallLength(w) || 1;
  const [nx, ny] = [(-(w.b[1] - w.a[1]) / len) * (w.thickness / 2), ((w.b[0] - w.a[0]) / len) * (w.thickness / 2)];
  return [[w.a[0] + nx, w.a[1] + ny], [w.b[0] + nx, w.b[1] + ny], [w.b[0] - nx, w.b[1] - ny], [w.a[0] - nx, w.a[1] - ny]];
}

/** Start and end of an opening on its wall's centre line. */
export function openingSpan(w: FloorWall, o: Pick<FloorOpening, "offset" | "width">): [Pt, Pt] {
  const len = wallLength(w) || 1;
  const [dx, dy] = [(w.b[0] - w.a[0]) / len, (w.b[1] - w.a[1]) / len];
  const [s, e] = [o.offset - o.width / 2, o.offset + o.width / 2];
  return [[w.a[0] + dx * s, w.a[1] + dy * s], [w.a[0] + dx * e, w.a[1] + dy * e]];
}

/** The extent of everything drawn (rooms, walls, devices, photos). */
export function bounds(p: Pick<Floorplan, "rooms" | "walls" | "devices" | "photos"> & { cables?: FloorCable[] }): { min: Pt; max: Pt } | undefined {
  const pts: Pt[] = [...p.rooms.flatMap((r) => r.points), ...p.walls.flatMap((w) => [w.a, w.b]),
    ...(p.cables ?? []).flatMap((c) => c.points.map((q) => [q[0], q[1]] as Pt)),
    ...p.devices.map((d) => [d.x, d.y] as Pt), ...p.photos.map((f) => [f.x, f.y] as Pt)];
  if (!pts.length) return undefined;
  const xs = pts.map((q) => q[0]);
  const ys = pts.map((q) => q[1]);
  return { min: [Math.min(...xs), Math.min(...ys)], max: [Math.max(...xs), Math.max(...ys)] };
}

// ---- live editing ----------------------------------------------------------
// In a live session every element is one entry of a shared map (by id), so
// two people changing different rooms or walls never overwrite each other.

export type PlanItem =
  | ({ t: "room" } & FloorRoom) | ({ t: "device" } & FloorDevice) | ({ t: "wall" } & FloorWall)
  | ({ t: "opening" } & FloorOpening) | ({ t: "photo" } & FloorPhoto) | ({ t: "cable" } & FloorCable);
export type PlanMeta = Omit<Floorplan, "rooms" | "devices" | "walls" | "openings" | "photos" | "cables">;

export function planItems(p: Floorplan): Map<string, PlanItem> {
  const m = new Map<string, PlanItem>();
  for (const r of p.rooms) m.set(r.id, { t: "room", ...r });
  for (const d of p.devices) m.set(d.id, { t: "device", ...d });
  for (const w of p.walls) m.set(w.id, { t: "wall", ...w });
  for (const o of p.openings) m.set(o.id, { t: "opening", ...o });
  for (const f of p.photos) m.set(f.id, { t: "photo", ...f });
  for (const c of p.cables) m.set(c.id, { t: "cable", ...c });
  return m;
}

export function planMeta(p: Floorplan): PlanMeta {
  const { rooms: _r, devices: _d, walls: _w, openings: _o, photos: _f, cables: _c, ...meta } = p;
  return meta;
}

/** Builds a plan from shared entries. Rooms are drawn largest first so small
 * rooms inside big ones stay clickable; the rest in a stable order. */
export function planFrom(meta: Partial<PlanMeta>, items: Iterable<PlanItem>): Floorplan {
  const p = normalizePlan(meta as Partial<Floorplan>);
  const strip = <T extends { t: string }>(x: T) => {
    const { t: _t, ...rest } = x;
    return rest;
  };
  for (const it of items) {
    if (it.t === "room") p.rooms.push(strip(it) as FloorRoom);
    else if (it.t === "device") p.devices.push(strip(it) as FloorDevice);
    else if (it.t === "wall") p.walls.push(strip(it) as FloorWall);
    else if (it.t === "opening") p.openings.push(strip(it) as FloorOpening);
    else if (it.t === "photo") p.photos.push(strip(it) as FloorPhoto);
    else if (it.t === "cable") p.cables.push(strip(it) as FloorCable);
  }
  const byId = (a: { id: string }, b: { id: string }) => (a.id < b.id ? -1 : a.id > b.id ? 1 : 0);
  p.rooms.sort((a, b) => area(b.points) - area(a.points) || byId(a, b));
  p.devices.sort(byId);
  p.walls.sort(byId);
  const walls = new Set(p.walls.map((w) => w.id));
  p.openings = p.openings.filter((o) => walls.has(o.wall)).sort(byId);
  p.photos.sort(byId);
  p.cables.sort(byId);
  return p;
}

/** What changed between two plans: entries to write and ids to delete. */
export function planDiff(before: Floorplan, after: Floorplan): { set: PlanItem[]; del: string[]; meta: boolean } {
  const a = planItems(before);
  const b = planItems(after);
  const set: PlanItem[] = [];
  for (const [id, it] of b) if (JSON.stringify(a.get(id)) !== JSON.stringify(it)) set.push(it);
  const del = [...a.keys()].filter((id) => !b.has(id));
  return { set, del, meta: JSON.stringify(planMeta(before)) !== JSON.stringify(planMeta(after)) };
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
