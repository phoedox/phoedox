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

/** Materials: building parts, racks, and the parts of device models
 * (body = light casing or furniture, dark = device, lamp = light shade). */
export type Solid = "wall" | "glass" | "rack" | "unit" | "body" | "dark" | "lamp";
/** A box: centre and size in metres, turned by rot around the vertical axis.
 * level is the index of the storey it belongs to. */
export interface Box {
  kind: Solid; x: number; y: number; z: number; w: number; h: number; d: number; rot: number;
  slug?: string; name?: string; level: number;
}
/** A room floor: polygon in the x/z plane at height y; area in m². */
export interface Slab { id: string; slug?: string; name: string; color: string; y: number; points: [number, number][]; area: number; level: number }
export interface Label { text: string; x: number; y: number; z: number; slug?: string; kind: "room" | "level"; level: number }
/** A device marker above its model: a round badge with the kind's icon. */
export interface Pin { slug: string; name: string; kind: string; x: number; y: number; z: number; level: number }
/** A cable route in metres (y up), coloured by kind. */
export interface Cable3D { kind: string; slug?: string; name: string; points: [number, number, number][]; level: number }
export interface Scene3D { boxes: Box[]; slabs: Slab[]; labels: Label[]; pins: Pin[]; cables: Cable3D[]; min: [number, number, number]; max: [number, number, number] }

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

type Part = [kind: Solid, w: number, h: number, d: number, bottom: number, dx?: number, dz?: number];

/** A simple model of a device by its kind, in metres: parts stand on the
 * floor (bottom = height above it); ceiling devices hang from the ceiling.
 * Returns the parts and the height of the model's top (for the pin). */
export function deviceModel(kind: string, ceiling: number): { parts: Part[]; top: number } {
  const c = ceiling;
  switch (kind) {
    case "router":
    case "modem":
      // On a sideboard, with antennas.
      return { parts: [["body", 0.6, 0.7, 0.4, 0], ["dark", 0.3, 0.05, 0.2, 0.7], ...[-0.09, 0, 0.09].map((dx): Part => ["dark", 0.012, 0.16, 0.012, 0.75, dx, -0.07])], top: 0.91 };
    case "access_point":
      return { parts: [["body", 0.24, 0.04, 0.24, c - 0.04]], top: c };
    case "light":
      return { parts: [["dark", 0.01, 0.45, 0.01, c - 0.45], ["lamp", 0.32, 0.14, 0.32, c - 0.59]], top: c };
    case "lan_port":
    case "outlet":
    case "tae":
    case "smart_plug":
    case "repeater":
      // Low on the wall.
      return { parts: [["body", 0.09, 0.09, 0.04, 0.26]], top: 0.35 };
    case "client_device":
      // A screen on a low cabinet.
      return { parts: [["body", 0.9, 0.42, 0.35, 0], ["dark", 0.08, 0.06, 0.08, 0.42], ["dark", 1.0, 0.58, 0.04, 0.48]], top: 1.06 };
    case "phone":
      return { parts: [["body", 0.45, 0.7, 0.35, 0], ["dark", 0.18, 0.06, 0.2, 0.7]], top: 0.76 };
    case "switch":
    case "patch_panel":
      return { parts: [["body", 0.6, 0.7, 0.4, 0], ["dark", 0.44, 0.045, 0.3, 0.7]], top: 0.745 };
    case "server":
    case "nas":
    case "ups":
      return { parts: [["dark", 0.22, 0.38, 0.42, 0]], top: 0.38 };
    default:
      return { parts: [["body", 0.32, 0.28, 0.3, 0]], top: 0.28 };
  }
}

export function buildScene(levels: Level[], entries: Map<string, EntryInfo>, mounted: Map<string, Mounted[]>): Scene3D {
  const boxes: Box[] = [];
  const slabs: Slab[] = [];
  const labels: Label[] = [];
  const pins: Pin[] = [];
  const cables: Cable3D[] = [];
  levels.forEach((lv, level) => {
    const p = lv.plan;
    const base = lv.elevation;
    for (const w of p.walls) {
      const len = wallLength(w) || 1;
      const [dx, dy] = [(w.b[0] - w.a[0]) / len, (w.b[1] - w.a[1]) / len];
      const rot = Math.atan2(-dy, dx);
      for (const piece of wallPieces(p, w.id, lv.wallHeight, lv.cut)) {
        const mid = (piece.from + piece.to) / 2;
        boxes.push({
          kind: piece.kind, rot, level,
          x: m(w.a[0] + dx * mid), z: m(w.a[1] + dy * mid), y: m(base + (piece.bottom + piece.top) / 2),
          w: m(piece.to - piece.from), h: m(piece.top - piece.bottom), d: m(piece.kind === "glass" ? Math.min(30, w.thickness) : w.thickness),
        });
      }
    }
    // Largest rooms first, slightly lower, so nested rooms stay visible.
    const rooms = [...p.rooms].sort((a, b) => area(b.points) - area(a.points));
    rooms.forEach((r, i) => {
      const e = r.entity ? entries.get(r.entity) : undefined;
      slabs.push({ id: r.id, slug: e?.slug, name: r.name, color: r.color || "gray", y: m(base) + i * 0.002, points: r.points.map(([x, y]) => [m(x), m(y)]), area: area(r.points) / 1e6, level });
      const [lx, ly] = labelPoint(r.points);
      if (r.name) labels.push({ kind: "room", text: r.name, x: m(lx), y: m(base) + 0.05, z: m(ly), slug: e?.slug, level });
    });
    for (const d of p.devices) {
      const e = entries.get(d.entity);
      if (e?.kind === "rack") {
        const units = Math.max(1, Number(e.attrs.height_units) || 42);
        const depth = Number(e.attrs.depth) || 1000;
        const h = units * RACK_UNIT + 100;
        boxes.push({ kind: "rack", slug: e.slug, name: e.name, rot: 0, level, x: m(d.x), z: m(d.y), y: m(base + h / 2), w: m(RACK_WIDTH), h: m(h), d: m(depth) });
        for (const u of mounted.get(e.slug) ?? []) {
          if (!u.u || u.u < 1 || u.u + u.height - 1 > units) continue;
          const bottom = 50 + (u.u - 1) * RACK_UNIT;
          // Racks face south (+z on screen); units stand 1 cm out of the front so they show.
          const ud = depth * 0.9;
          boxes.push({ kind: "unit", slug: u.slug, name: u.name, rot: 0, level, x: m(d.x), z: m(d.y + depth / 2 + 10 - ud / 2), y: m(base + bottom + (u.height * RACK_UNIT) / 2),
            w: m(RACK_WIDTH - 120), h: m(u.height * RACK_UNIT - 4), d: m(ud) });
        }
        pins.push({ slug: e.slug, name: e.name, kind: e.kind, x: m(d.x), y: m(base + h) + 0.3, z: m(d.y), level });
      } else {
        const slug = e?.slug ?? d.entity, name = e?.name ?? d.entity;
        const model = deviceModel(e?.kind ?? "", m(lv.wallHeight));
        for (const [kind, w, h, dd, bottom, dx = 0, dz = 0] of model.parts) {
          boxes.push({ kind, slug, name, rot: 0, level, x: m(d.x) + dx, z: m(d.y) + dz, y: m(base) + bottom + h / 2, w, h, d: dd });
        }
        pins.push({ slug, name, kind: e?.kind ?? "", x: m(d.x), y: m(base) + model.top + 0.28, z: m(d.y), level });
      }
    }
    for (const c of p.cables ?? []) {
      // A cable opens the entry of its far end (or the cable entry itself).
      const slug = c.entity ?? c.to ?? c.from;
      cables.push({ kind: c.kind, slug, name: c.label || slug || c.kind, points: c.points.map(([x, y, h]) => [m(x), m(base + h), m(y)]), level });
    }
    if (levels.length > 1 && lv.name) labels.push({ kind: "level", text: lv.name, x: -0.5, y: m(base) + 0.1, z: -0.5, level });
  });
  // Extent, for the camera.
  const pts: [number, number, number][] = [
    ...boxes.flatMap((b) => [[b.x - b.w / 2, b.y - b.h / 2, b.z - b.d / 2], [b.x + b.w / 2, b.y + b.h / 2, b.z + b.d / 2]] as [number, number, number][]),
    ...slabs.flatMap((s) => s.points.map(([x, z]) => [x, s.y, z] as [number, number, number])),
    ...cables.flatMap((c) => c.points),
  ];
  if (!pts.length) {
    const p = levels[0]?.plan;
    pts.push([0, 0, 0], [m(p?.width ?? 10000), m(WALL_HEIGHT), m(p?.height ?? 10000)]);
  }
  const min = [0, 1, 2].map((i) => Math.min(...pts.map((q) => q[i]))) as [number, number, number];
  const max = [0, 1, 2].map((i) => Math.max(...pts.map((q) => q[i]))) as [number, number, number];
  return { boxes, slabs, labels, pins, cables, min, max };
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

