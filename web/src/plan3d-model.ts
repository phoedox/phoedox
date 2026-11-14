// 3D view of floor plans: turns plans (millimetres, y pointing down on paper)
// into simple solids in metres with y pointing up. Pure data so it can be
// tested without WebGL; components/plan3d.ts draws it with three.js.

import { area, labelPoint, wallLength, type Floorplan } from "./floorplan";

export const DOOR_HEIGHT = 2100;
export const WINDOW_SILL = 900;
export const WINDOW_TOP = 2100;
export const WALL_HEIGHT = 2500;
export const SLAB = 200; // between storeys
export const RACK_UNIT = 44.45;
const RACK_WIDTH = 600;

export type Solid = "wall" | "glass" | "rack" | "unit" | "device";
/** A box: centre and size in metres, turned by rot around the vertical axis. */
export interface Box {
  kind: Solid; x: number; y: number; z: number; w: number; h: number; d: number; rot: number;
  slug?: string; name?: string;
}
/** A room floor: polygon in the x/z plane at height y. */
export interface Slab { id: string; slug?: string; name: string; color: string; y: number; points: [number, number][] }
export interface Label { text: string; x: number; y: number; z: number; slug?: string; kind: "room" | "device" | "level" }
export interface Scene3D { boxes: Box[]; slabs: Slab[]; labels: Label[]; min: [number, number, number]; max: [number, number, number] }

export interface Level {
  plan: Floorplan;
  /** Height of the floor above the lowest floor (mm). */
  elevation: number;
  /** Default wall height (mm). */
  wallHeight: number;
  name?: string;
  /** Cut-away view: walls end at this height (mm). */
  cut?: number;
}

export const CUT_HEIGHT = 1200;

export interface EntryInfo { slug: string; name: string; kind: string; attrs: Record<string, unknown> }
export interface Mounted { slug: string; name: string; u?: number; height: number }

/** Stacks plans by the level of their floor entry; storeys are as high as
 * the floor's ceiling height (or their walls) plus a slab. */
export function stackLevels(plans: { plan: Floorplan; floor?: EntryInfo }[]): Level[] {
  const sorted = [...plans].sort((a, b) => (Number(a.floor?.attrs.level) || 0) - (Number(b.floor?.attrs.level) || 0));
  let elevation = 0;
  return sorted.map(({ plan, floor }) => {
    const ceiling = Number(floor?.attrs.ceiling_height) * 1000;
    const tallest = Math.max(0, ...plan.walls.map((w) => w.height ?? 0));
    const wallHeight = ceiling > 0 ? ceiling : tallest || WALL_HEIGHT;
    const level = { plan, elevation, wallHeight, name: floor?.name };
    elevation += Math.max(wallHeight, tallest) + SLAB;
    return level;
  });
}

const m = (mm: number) => mm / 1000;

/** Solid pieces of one wall: full height between openings, lintels above
 * doors and windows, parapets below windows, glass in windows. */
export function wallPieces(plan: Floorplan, wallId: string, height: number, cut?: number): { from: number; to: number; bottom: number; top: number; kind: "wall" | "glass" }[] {
  const w = plan.walls.find((x) => x.id === wallId);
  if (!w) return [];
  const len = wallLength(w);
  const h = w.height || height;
  const gaps = plan.openings.filter((o) => o.wall === w.id)
    .map((o) => ({ kind: o.kind, from: Math.max(0, o.offset - o.width / 2), to: Math.min(len, o.offset + o.width / 2) }))
    .sort((a, b) => a.from - b.from);
  const out: ReturnType<typeof wallPieces> = [];
  let at = 0;
  for (const g of gaps) {
    if (g.from > at) out.push({ from: at, to: g.from, bottom: 0, top: h, kind: "wall" });
    const from = Math.max(g.from, at);
    if (g.to <= from) continue;
    const top = Math.min(h, g.kind === "door" ? DOOR_HEIGHT : WINDOW_TOP);
    if (g.kind === "window") {
      const sill = Math.min(WINDOW_SILL, top);
      out.push({ from, to: g.to, bottom: 0, top: sill, kind: "wall" });
      if (top > sill) out.push({ from, to: g.to, bottom: sill, top, kind: "glass" });
    }
    if (h > top) out.push({ from, to: g.to, bottom: top, top: h, kind: "wall" });
    at = g.to;
  }
  if (at < len) out.push({ from: at, to: len, bottom: 0, top: h, kind: "wall" });
  if (cut) for (const p of out) p.top = Math.min(p.top, cut);
  return out.filter((p) => p.to - p.from > 1 && p.top - p.bottom > 1);
}

export function buildScene(levels: Level[], entries: Map<string, EntryInfo>, mounted: Map<string, Mounted[]>): Scene3D {
  const boxes: Box[] = [];
  const slabs: Slab[] = [];
  const labels: Label[] = [];
  for (const lv of levels) {
    const p = lv.plan;
    const base = lv.elevation;
    for (const w of p.walls) {
      const len = wallLength(w) || 1;
      const [dx, dy] = [(w.b[0] - w.a[0]) / len, (w.b[1] - w.a[1]) / len];
      const rot = Math.atan2(-dy, dx);
      for (const piece of wallPieces(p, w.id, lv.wallHeight, lv.cut)) {
        const mid = (piece.from + piece.to) / 2;
        boxes.push({
          kind: piece.kind, rot,
          x: m(w.a[0] + dx * mid), z: m(w.a[1] + dy * mid), y: m(base + (piece.bottom + piece.top) / 2),
          w: m(piece.to - piece.from), h: m(piece.top - piece.bottom), d: m(piece.kind === "glass" ? Math.min(30, w.thickness) : w.thickness),
        });
      }
    }
    // Largest rooms first, slightly lower, so nested rooms stay visible.
    const rooms = [...p.rooms].sort((a, b) => area(b.points) - area(a.points));
    rooms.forEach((r, i) => {
      const e = r.entity ? entries.get(r.entity) : undefined;
      slabs.push({ id: r.id, slug: e?.slug, name: r.name, color: r.color || "gray", y: m(base) + i * 0.002, points: r.points.map(([x, y]) => [m(x), m(y)]) });
      const [lx, ly] = labelPoint(r.points);
      if (r.name) labels.push({ kind: "room", text: r.name, x: m(lx), y: m(base) + 0.05, z: m(ly), slug: e?.slug });
    });
    for (const d of p.devices) {
      const e = entries.get(d.entity);
      if (e?.kind === "rack") {
        const units = Math.max(1, Number(e.attrs.height_units) || 42);
        const depth = Number(e.attrs.depth) || 1000;
        const h = units * RACK_UNIT + 100;
        boxes.push({ kind: "rack", slug: e.slug, name: e.name, rot: 0, x: m(d.x), z: m(d.y), y: m(base + h / 2), w: m(RACK_WIDTH), h: m(h), d: m(depth) });
        for (const u of mounted.get(e.slug) ?? []) {
          if (!u.u || u.u < 1 || u.u + u.height - 1 > units) continue;
          const bottom = 50 + (u.u - 1) * RACK_UNIT;
          // Racks face south (+z on screen); units stand 1 cm out of the front so they show.
          const ud = depth * 0.9;
          boxes.push({ kind: "unit", slug: u.slug, name: u.name, rot: 0, x: m(d.x), z: m(d.y + depth / 2 + 10 - ud / 2), y: m(base + bottom + (u.height * RACK_UNIT) / 2),
            w: m(RACK_WIDTH - 120), h: m(u.height * RACK_UNIT - 4), d: m(ud) });
        }
        labels.push({ kind: "device", text: e.name, x: m(d.x), y: m(base + h) + 0.15, z: m(d.y), slug: e.slug });
      } else {
        boxes.push({ kind: "device", slug: e?.slug ?? d.entity, name: e?.name ?? d.entity, rot: 0, x: m(d.x), z: m(d.y), y: m(base) + 0.15, w: 0.3, h: 0.3, d: 0.3 });
        labels.push({ kind: "device", text: e?.name ?? d.entity, x: m(d.x), y: m(base) + 0.55, z: m(d.y), slug: e?.slug ?? d.entity });
      }
    }
    if (levels.length > 1 && lv.name) labels.push({ kind: "level", text: lv.name, x: -0.5, y: m(base) + 0.1, z: -0.5 });
  }
  // Extent, for the camera.
  const pts: [number, number, number][] = [
    ...boxes.flatMap((b) => [[b.x - b.w / 2, b.y - b.h / 2, b.z - b.d / 2], [b.x + b.w / 2, b.y + b.h / 2, b.z + b.d / 2]] as [number, number, number][]),
    ...slabs.flatMap((s) => s.points.map(([x, z]) => [x, s.y, z] as [number, number, number])),
  ];
  if (!pts.length) {
    const p = levels[0]?.plan;
    pts.push([0, 0, 0], [m(p?.width ?? 10000), m(WALL_HEIGHT), m(p?.height ?? 10000)]);
  }
  const min = [0, 1, 2].map((i) => Math.min(...pts.map((q) => q[i]))) as [number, number, number];
  const max = [0, 1, 2].map((i) => Math.max(...pts.map((q) => q[i]))) as [number, number, number];
  return { boxes, slabs, labels, min, max };
}

/** Devices mounted in racks (mounted_in edges with u and height). */
export function mountedByRack(edges: { rel: string; fromSlug?: string; fromName: string; toSlug?: string; attrs?: Record<string, unknown> }[]): Map<string, Mounted[]> {
  const out = new Map<string, Mounted[]>();
  for (const e of edges) {
    if (e.rel !== "mounted_in" || !e.toSlug || !e.fromSlug) continue;
    const list = out.get(e.toSlug) ?? [];
    list.push({ slug: e.fromSlug, name: e.fromName, u: Number(e.attrs?.u) || undefined, height: Math.max(1, Number(e.attrs?.height) || 1) });
    out.set(e.toSlug, list);
  }
  return out;
}

