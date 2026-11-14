// Floor plan imports: DXF drawings, Apple RoomPlan scans (CapturedRoom JSON)
// and IFC building models. Everything is parsed in the browser into walls,
// openings and rooms in millimetres; nothing is executed or uploaded.

import { DOOR_WIDTH, WALL_THICKNESS, area, bounds, clampOpening, newId, onWall, wallLength, type FloorOpening, type FloorRoom, type FloorWall, type Floorplan, type Pt } from "./floorplan";

export interface ImportedWall { a: Pt; b: Pt; thickness?: number; height?: number }
export interface ImportedOpening { kind: "door" | "window"; center: Pt; width: number }
export interface ImportedRoom { name: string; points: Pt[] }
/** One floor of an import (IFC models can have several storeys). */
export interface ImportedLevel { name: string; walls: ImportedWall[]; openings: ImportedOpening[]; rooms: ImportedRoom[] }
export interface ImportResult { levels: ImportedLevel[]; skipped: number }

export class ImportError extends Error {
  constructor(public key: string) {
    super(key);
  }
}

// ---- DXF --------------------------------------------------------------------

const DXF_UNITS: Record<number, number> = { 1: 25.4, 2: 304.8, 4: 1, 5: 10, 6: 1000 };
const ROOM_LAYER = /room|raum|space|zone|fläche|flaeche|area/i;

/** Reads lines, polylines and texts of a DXF drawing. Closed polylines on
 * room layers become rooms (named by a text inside them), everything else
 * becomes walls. DWG files must be saved as DXF first. */
export function parseDXF(text: string): ImportResult {
  const lines = text.split(/\r?\n/);
  const pairs: [number, string][] = [];
  for (let i = 0; i + 1 < lines.length; i += 2) {
    const code = Number(lines[i].trim());
    if (!Number.isFinite(code)) throw new ImportError("plan.import_invalid");
    pairs.push([code, lines[i + 1].trim()]);
  }
  if (!pairs.some(([c, v]) => c === 0 && v === "SECTION")) throw new ImportError("plan.import_invalid");
  let unit = 0;
  const unitAt = pairs.findIndex(([c, v]) => c === 9 && v === "$INSUNITS");
  if (unitAt >= 0) unit = Number(pairs.find(([c], i) => i > unitAt && c === 70)?.[1] ?? 0);

  type Shape = { kind: string; layer: string; pts: Pt[]; closed: boolean; text?: string };
  const shapes: Shape[] = [];
  let skipped = 0;
  let inEntities = false;
  let cur: Shape | undefined;
  let poly: Shape | undefined; // POLYLINE collects following VERTEX entities
  const flush = () => {
    if (cur && cur.kind !== "VERTEX" && cur.kind !== "SEQEND" && cur.kind !== "POLYLINE") shapes.push(cur);
    cur = undefined;
  };
  for (let i = 0; i < pairs.length; i++) {
    const [code, v] = pairs[i];
    if (code === 0) {
      if (v === "SECTION") {
        const name = pairs[i + 1]?.[1];
        inEntities = name === "ENTITIES";
        continue;
      }
      if (v === "ENDSEC") {
        flush();
        inEntities = false;
        continue;
      }
      if (!inEntities) continue;
      flush();
      if (v === "SEQEND") {
        if (poly) shapes.push(poly);
        poly = undefined;
        continue;
      }
      if (v === "VERTEX" && poly) {
        cur = { kind: "VERTEX", layer: poly.layer, pts: [], closed: false };
        continue;
      }
      if (["LINE", "LWPOLYLINE", "POLYLINE", "TEXT", "MTEXT"].includes(v)) {
        cur = { kind: v, layer: "", pts: [], closed: false };
        if (v === "POLYLINE") poly = cur;
      } else skipped++;
      continue;
    }
    if (!cur) continue;
    if (code === 8) cur.layer = v;
    else if (code === 70 && (cur.kind === "LWPOLYLINE" || cur.kind === "POLYLINE")) cur.closed = (Number(v) & 1) === 1;
    else if (code === 1 && (cur.kind === "TEXT" || cur.kind === "MTEXT")) cur.text = v.replace(/\\P/g, " ").replace(/\\[A-Za-z][^;]*;/g, "").replace(/[{}]/g, "").trim();
    else if (code === 10 && cur.kind !== "POLYLINE") {
      const y = Number(pairs[i + 1]?.[0] === 20 ? pairs[i + 1][1] : 0);
      const pt: Pt = [Number(v), y];
      if (cur.kind === "VERTEX") poly?.pts.push(pt);
      else if (cur.kind === "LINE" || cur.kind === "TEXT" || cur.kind === "MTEXT") cur.pts[0] = pt;
      else cur.pts.push(pt);
    } else if (code === 11 && cur.kind === "LINE") {
      cur.pts[1] = [Number(v), Number(pairs[i + 1]?.[0] === 21 ? pairs[i + 1][1] : 0)];
    }
  }
  flush();

  // Unit: from the header; unitless drawings are guessed from their size.
  let scale = DXF_UNITS[unit];
  if (!scale) {
    const all = shapes.flatMap((s) => s.pts);
    const xs = all.map((p) => p[0]);
    const ys = all.map((p) => p[1]);
    const extent = all.length ? Math.max(Math.max(...xs) - Math.min(...xs), Math.max(...ys) - Math.min(...ys)) : 0;
    scale = extent > 0 && extent < 500 ? 1000 : 1;
  }
  // DXF y points up, plans point down.
  const mm = (p: Pt): Pt => [Math.round(p[0] * scale), Math.round(-p[1] * scale)];

  const level: ImportedLevel = { name: "", walls: [], openings: [], rooms: [] };
  const texts: { at: Pt; text: string }[] = [];
  for (const s of shapes) {
    const pts = s.pts.filter(Boolean).map(mm);
    if (s.kind === "TEXT" || s.kind === "MTEXT") {
      if (pts[0] && s.text) texts.push({ at: pts[0], text: s.text });
    } else if (s.kind === "LINE") {
      if (pts.length === 2) level.walls.push({ a: pts[0], b: pts[1] });
    } else if (s.closed && pts.length >= 3 && ROOM_LAYER.test(s.layer)) {
      level.rooms.push({ name: "", points: pts });
    } else {
      const ring = s.closed ? [...pts, pts[0]] : pts;
      for (let i = 0; i + 1 < ring.length; i++) level.walls.push({ a: ring[i], b: ring[i + 1] });
    }
  }
  nameRooms(level.rooms, texts);
  level.walls = level.walls.filter((w) => wallLength(w) > 0);
  if (!level.walls.length && !level.rooms.length) throw new ImportError("plan.import_empty");
  return { levels: [level], skipped };
}

/** Names rooms after the smallest-room text inside them. */
function nameRooms(rooms: ImportedRoom[], texts: { at: Pt; text: string }[]) {
  const sorted = [...rooms].sort((a, b) => area(a.points) - area(b.points));
  for (const t of texts) {
    const r = sorted.find((x) => !x.name && insidePts(t.at, x.points));
    if (r) r.name = t.text.slice(0, 200);
  }
}

function insidePts([x, y]: Pt, pts: Pt[]): boolean {
  let hit = false;
  for (let i = 0, j = pts.length - 1; i < pts.length; j = i++) {
    const [xi, yi] = pts[i];
    const [xj, yj] = pts[j];
    if ((yi > y) !== (yj > y) && x < ((xj - xi) * (y - yi)) / (yj - yi) + xi) hit = !hit;
  }
  return hit;
}

// ---- Apple RoomPlan -----------------------------------------------------------

type Num3 = [number, number, number];
const vec3 = (v: unknown): Num3 | undefined => {
  if (Array.isArray(v) && v.length >= 3 && v.slice(0, 3).every((x) => typeof x === "number")) return [v[0], v[1], v[2]];
  if (v && typeof v === "object" && "x" in v) {
    const o = v as { x: number; y: number; z: number };
    return [o.x, o.y, o.z];
  }
  return undefined;
};
/** simd_float4x4 as RoomPlan encodes it: 16 numbers (column-major) or 4 columns. */
const mat4 = (v: unknown): number[] | undefined => {
  const flat = Array.isArray(v) ? (v as unknown[]).flat() : [];
  return flat.length === 16 && flat.every((x) => typeof x === "number") ? (flat as number[]) : undefined;
};

/** Reads a RoomPlan CapturedRoom (JSONEncoder output): walls with their
 * size and position, doors, windows and openings. Metres → millimetres; the
 * floor is the x/z plane seen from above. */
export function parseRoomPlan(text: string): ImportResult {
  let room: Record<string, unknown>;
  try {
    room = JSON.parse(text);
  } catch {
    throw new ImportError("plan.import_invalid");
  }
  if (!room || !Array.isArray(room.walls)) throw new ImportError("plan.import_invalid");
  const surface = (s: Record<string, unknown>) => {
    const d = vec3(s.dimensions);
    const m = mat4(s.transform);
    if (!d || !m) return undefined;
    const center: Pt = [m[12] * 1000, m[14] * 1000];
    const len = Math.hypot(m[0], m[2]) || 1;
    const dir: Pt = [m[0] / len, m[2] / len];
    return { center, dir, width: d[0] * 1000, height: d[1] * 1000 };
  };
  const level: ImportedLevel = { name: "", walls: [], openings: [], rooms: [] };
  let skipped = 0;
  for (const w of room.walls as Record<string, unknown>[]) {
    const s = surface(w);
    if (!s) {
      skipped++;
      continue;
    }
    const h = s.width / 2;
    level.walls.push({
      a: [Math.round(s.center[0] - s.dir[0] * h), Math.round(s.center[1] - s.dir[1] * h)],
      b: [Math.round(s.center[0] + s.dir[0] * h), Math.round(s.center[1] + s.dir[1] * h)],
      height: Math.round(s.height),
    });
  }
  for (const [key, kind] of [["doors", "door"], ["openings", "door"], ["windows", "window"]] as const) {
    for (const o of (Array.isArray(room[key]) ? room[key] : []) as Record<string, unknown>[]) {
      const s = surface(o);
      if (s) level.openings.push({ kind, center: [Math.round(s.center[0]), Math.round(s.center[1])], width: Math.round(s.width) });
      else skipped++;
    }
  }
  if (Array.isArray(room.objects)) skipped += room.objects.length;
  if (!level.walls.length) throw new ImportError("plan.import_empty");
  return { levels: [level], skipped };
}

// ---- IFC (STEP) ---------------------------------------------------------------

type StepVal = number | string | { ref: number } | { enum: string } | { typed: string; value: StepVal } | StepVal[] | null;
interface StepEntity { type: string; args: StepVal[] }

/** Decodes \X2\…\X0\ (UTF-16 hex) and '' in STEP strings. */
function stepString(s: string): string {
  return s.replace(/''/g, "'").replace(/\\X2\\([0-9A-F]+)\\X0\\/gi, (_, hex: string) =>
    String.fromCharCode(...(hex.match(/.{4}/g) ?? []).map((h) => parseInt(h, 16)))).replace(/\\X\\([0-9A-F]{2})/gi, (_, h: string) => String.fromCharCode(parseInt(h, 16)));
}

function parseStepArgs(src: string): StepVal[] {
  let i = 0;
  const ws = () => { while (i < src.length && /\s/.test(src[i])) i++; };
  const value = (): StepVal => {
    ws();
    const c = src[i];
    if (c === "(") {
      i++;
      const list: StepVal[] = [];
      ws();
      if (src[i] === ")") { i++; return list; }
      for (;;) {
        list.push(value());
        ws();
        if (src[i] === ",") { i++; continue; }
        if (src[i] === ")") { i++; return list; }
        throw new ImportError("plan.import_invalid");
      }
    }
    if (c === "'") {
      let j = i + 1;
      let out = "";
      for (;;) {
        if (j >= src.length) throw new ImportError("plan.import_invalid");
        if (src[j] === "'" && src[j + 1] === "'") { out += "''"; j += 2; continue; }
        if (src[j] === "'") break;
        out += src[j++];
      }
      i = j + 1;
      return stepString(out);
    }
    if (c === "#") {
      const m = /^#(\d+)/.exec(src.slice(i));
      i += m![0].length;
      return { ref: Number(m![1]) };
    }
    if (c === ".") {
      const end = src.indexOf(".", i + 1);
      const e = src.slice(i + 1, end);
      i = end + 1;
      return { enum: e };
    }
    if (c === "$" || c === "*") { i++; return null; }
    const num = /^[-+]?(\d+\.?\d*([eE][-+]?\d+)?|\.\d+)/.exec(src.slice(i));
    if (num) { i += num[0].length; return Number(num[0]); }
    const typed = /^([A-Z0-9_]+)\s*\(/i.exec(src.slice(i));
    if (typed) {
      i += typed[1].length;
      const inner = value() as StepVal[];
      return { typed: typed[1].toUpperCase(), value: inner[0] ?? null };
    }
    throw new ImportError("plan.import_invalid");
  };
  const v = value();
  return Array.isArray(v) ? v : [v];
}

function parseStep(text: string): Map<number, StepEntity> {
  const data = text.indexOf("DATA;");
  if (!/ISO-10303-21/.test(text.slice(0, 200)) || data < 0) throw new ImportError("plan.import_invalid");
  const out = new Map<number, StepEntity>();
  // Statements end with ";" outside strings.
  let start = data + 5;
  let inStr = false;
  for (let i = start; i < text.length; i++) {
    const c = text[i];
    if (c === "'") inStr = !inStr;
    else if (c === ";" && !inStr) {
      const st = text.slice(start, i).trim();
      start = i + 1;
      const m = /^#(\d+)\s*=\s*([A-Z0-9_]+)\s*(\(.*\))$/is.exec(st);
      if (m) {
        try {
          out.set(Number(m[1]), { type: m[2].toUpperCase(), args: parseStepArgs(m[3]) });
        } catch {
          // One unreadable line does not stop the import.
        }
      }
      if (st === "ENDSEC") break;
    }
  }
  return out;
}

type Mat = number[]; // 4x4 row-major
const IDENT: Mat = [1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1];
const mul = (a: Mat, b: Mat): Mat => {
  const r = new Array(16).fill(0);
  for (let i = 0; i < 4; i++) for (let j = 0; j < 4; j++) for (let k = 0; k < 4; k++) r[i * 4 + j] += a[i * 4 + k] * b[k * 4 + j];
  return r;
};
const apply = (m: Mat, p: number[]): number[] => [0, 1, 2].map((i) => m[i * 4] * p[0] + m[i * 4 + 1] * p[1] + m[i * 4 + 2] * (p[2] ?? 0) + m[i * 4 + 3]);
const norm = (v: number[]) => { const l = Math.hypot(...v) || 1; return v.map((x) => x / l); };
const cross = (a: number[], b: number[]) => [a[1] * b[2] - a[2] * b[1], a[2] * b[0] - a[0] * b[2], a[0] * b[1] - a[1] * b[0]];

/** Reads walls, doors, windows and spaces of an IFC model per storey. Walls
 * use their axis (or the rectangle they are extruded from), spaces their
 * footprint; positions follow the placement chain. */
export function parseIFC(text: string): ImportResult {
  const ents = parseStep(text);
  const get = (v: StepVal | undefined) => (v && typeof v === "object" && "ref" in v ? ents.get(v.ref) : undefined);
  const nums = (v: StepVal | undefined): number[] => (Array.isArray(v) ? v.map((x) => (typeof x === "number" ? x : 0)) : []);
  const str = (v: StepVal | undefined) => (typeof v === "string" ? v : v && typeof v === "object" && "typed" in v && typeof v.value === "string" ? v.value : "");

  // Length unit → mm.
  let scale = 1000;
  for (const e of ents.values()) {
    if (e.type === "IFCSIUNIT" && (e.args[1] as { enum?: string })?.enum === "LENGTHUNIT") {
      const prefix = (e.args[2] as { enum?: string } | null)?.enum;
      scale = prefix === "MILLI" ? 1 : prefix === "CENTI" ? 10 : prefix === "DECI" ? 100 : 1000;
    }
  }

  const point = (v: StepVal | undefined) => nums(get(v)?.args[0]);
  const axis2 = (e: StepEntity | undefined): Mat => {
    if (!e) return IDENT;
    const o = point(e.args[0]);
    if (e.type === "IFCAXIS2PLACEMENT2D") {
      const x = get(e.args[1]) ? norm(nums(get(e.args[1])!.args[0])) : [1, 0];
      return [x[0], -x[1], 0, o[0] ?? 0, x[1], x[0], 0, o[1] ?? 0, 0, 0, 1, 0, 0, 0, 0, 1];
    }
    const z = get(e.args[1]) ? norm(nums(get(e.args[1])!.args[0])) : [0, 0, 1];
    let x = get(e.args[2]) ? norm(nums(get(e.args[2])!.args[0])) : [1, 0, 0];
    const y = norm(cross(z, x));
    x = cross(y, z);
    return [x[0], y[0], z[0], o[0] ?? 0, x[1], y[1], z[1], o[1] ?? 0, x[2], y[2], z[2], o[2] ?? 0, 0, 0, 0, 1];
  };
  const placements = new Map<number, Mat>();
  const placement = (v: StepVal | undefined, depth = 0): Mat => {
    const e = get(v);
    if (!e || e.type !== "IFCLOCALPLACEMENT" || depth > 50) return IDENT;
    const id = (v as { ref: number }).ref;
    const known = placements.get(id);
    if (known) return known;
    const m = mul(placement(e.args[0], depth + 1), axis2(get(e.args[1])));
    placements.set(id, m);
    return m;
  };
  const reps = (product: StepEntity) => {
    const shape = get(product.args[6]);
    const list = shape ? (shape.args[2] as StepVal[]) ?? [] : [];
    return list.map(get).filter((r): r is StepEntity => !!r).map((r) => ({ id: str(r.args[1]), items: ((r.args[3] as StepVal[]) ?? []).map(get).filter((x): x is StepEntity => !!x) }));
  };
  const polyline = (e: StepEntity | undefined): number[][] =>
    e?.type === "IFCPOLYLINE" ? ((e.args[0] as StepVal[]) ?? []).map((p) => nums(get(p)?.args[0])) : [];
  const toPlan = (m: Mat, p: number[]): Pt => {
    const w = apply(m, p);
    return [Math.round(w[0] * scale), Math.round(-w[1] * scale)];
  };
  /** Footprint of a profile (rectangle or polyline) in the product's frame. */
  const profile = (e: StepEntity | undefined): { pts: number[][]; frame: Mat; rect?: [number, number] } | undefined => {
    if (!e) return undefined;
    if (e.type === "IFCRECTANGLEPROFILEDEF") {
      const [xd, yd] = [Number(e.args[3]) || 0, Number(e.args[4]) || 0];
      return { pts: [[-xd / 2, -yd / 2], [xd / 2, -yd / 2], [xd / 2, yd / 2], [-xd / 2, yd / 2]], frame: axis2(get(e.args[2])), rect: [xd, yd] };
    }
    if (e.type === "IFCARBITRARYCLOSEDPROFILEDEF") return { pts: polyline(get(e.args[2])), frame: IDENT };
    return undefined;
  };

  const storeyOf = new Map<number, number>();
  for (const e of ents.values()) {
    if (e.type === "IFCRELCONTAINEDINSPATIALSTRUCTURE" || e.type === "IFCRELAGGREGATES") {
      const [kids, parent] = e.type === "IFCRELAGGREGATES" ? [e.args[5], e.args[4]] : [e.args[4], e.args[5]];
      const pid = (parent as { ref?: number })?.ref;
      if (pid && ents.get(pid)?.type === "IFCBUILDINGSTOREY") for (const k of (kids as StepVal[]) ?? []) if (k && typeof k === "object" && "ref" in k) storeyOf.set(k.ref, pid);
    }
  }
  const levels = new Map<number, ImportedLevel>();
  const levelFor = (id: number) => {
    const sid = storeyOf.get(id) ?? 0;
    let l = levels.get(sid);
    if (!l) {
      const s = ents.get(sid);
      l = { name: s ? str(s.args[2]) || str(s.args[7]) : "", walls: [], openings: [], rooms: [] };
      levels.set(sid, l);
    }
    return l;
  };

  let skipped = 0;
  for (const [id, e] of ents) {
    const t = e.type;
    if (t === "IFCWALL" || t === "IFCWALLSTANDARDCASE" || t === "IFCWALLELEMENTEDCASE") {
      const m = placement(e.args[5]);
      const rs = reps(e);
      const body = rs.find((r) => r.id === "Body")?.items.find((x) => x.type === "IFCEXTRUDEDAREASOLID");
      const prof = body ? profile(get(body.args[0])) : undefined;
      const thickness = prof?.rect ? Math.round(prof.rect[1] * scale) : undefined;
      const height = body && typeof body.args[3] === "number" ? Math.round(body.args[3] * scale) : undefined;
      const axis = polyline(rs.find((r) => r.id === "Axis")?.items[0]);
      if (axis.length >= 2) {
        const pts = axis.map((p) => toPlan(m, p));
        for (let i = 0; i + 1 < pts.length; i++) levelFor(id).walls.push({ a: pts[i], b: pts[i + 1], thickness, height });
      } else if (prof?.rect && body) {
        const frame = mul(mul(m, axis2(get(body.args[1]))), prof.frame);
        const half = prof.rect[0] / 2;
        levelFor(id).walls.push({ a: toPlan(frame, [-half, 0]), b: toPlan(frame, [half, 0]), thickness, height });
      } else skipped++;
    } else if (t === "IFCDOOR" || t === "IFCWINDOW") {
      const m = placement(e.args[5]);
      const width = Number(e.args[9]) || 0;
      if (width > 0) {
        // The local origin sits at one side of the opening.
        levelFor(id).openings.push({ kind: t === "IFCDOOR" ? "door" : "window", center: toPlan(m, [width / 2, 0, 0]), width: Math.round(width * scale) });
      } else skipped++;
    } else if (t === "IFCSPACE") {
      const m = placement(e.args[5]);
      const rs = reps(e);
      const name = str(e.args[7]) || str(e.args[2]);
      const foot = polyline(rs.find((r) => r.id === "FootPrint")?.items.find((x) => x.type === "IFCPOLYLINE"));
      let pts: Pt[] = foot.map((p) => toPlan(m, p));
      if (pts.length < 3) {
        const body = rs.find((r) => r.id === "Body")?.items.find((x) => x.type === "IFCEXTRUDEDAREASOLID");
        const prof = body ? profile(get(body.args[0])) : undefined;
        if (prof && body) {
          const frame = mul(mul(m, axis2(get(body.args[1]))), prof.frame);
          pts = prof.pts.map((p) => toPlan(frame, p));
        }
      }
      if (pts.length > 3 && pts[0][0] === pts[pts.length - 1][0] && pts[0][1] === pts[pts.length - 1][1]) pts = pts.slice(0, -1);
      if (pts.length >= 3) levelFor(id).rooms.push({ name, points: pts });
      else skipped++;
    }
  }
  const out = [...levels.values()].filter((l) => l.walls.length || l.rooms.length);
  for (const l of out) l.walls = l.walls.filter((w) => wallLength(w) > 0);
  if (!out.length) throw new ImportError("plan.import_empty");
  return { levels: out, skipped };
}

// ---- into the plan ----------------------------------------------------------

/** Picks the parser by file name and content. */
export function parseImport(name: string, text: string): ImportResult {
  const n = name.toLowerCase();
  if (n.endsWith(".dwg")) throw new ImportError("plan.import_dwg");
  if (n.endsWith(".dxf")) return parseDXF(text);
  if (n.endsWith(".ifc")) return parseIFC(text);
  if (n.endsWith(".json")) return parseRoomPlan(text);
  throw new ImportError("plan.import_format");
}

/** Adds an imported level to the plan: shifted next to the origin with a
 * one-metre margin, the plan grown to fit, openings put on their nearest wall. */
export function addImport(plan: Floorplan, level: ImportedLevel): { plan: Floorplan; walls: number; rooms: number; openings: number } {
  const next = structuredClone(plan);
  const walls: FloorWall[] = level.walls.map((w) => ({ id: newId("w"), a: w.a, b: w.b, thickness: w.thickness && w.thickness >= 10 && w.thickness <= 2000 ? w.thickness : WALL_THICKNESS, ...(w.height ? { height: w.height } : {}) }));
  const rooms: FloorRoom[] = level.rooms.map((r, i) => ({ id: newId("r"), name: r.name || `${i + 1}`, points: r.points }));
  const b = bounds({ rooms, walls, devices: [], photos: [] });
  if (!b) return { plan: next, walls: 0, rooms: 0, openings: 0 };
  const [dx, dy] = [1000 - b.min[0], 1000 - b.min[1]];
  const move = (p: Pt): Pt => [p[0] + dx, p[1] + dy];
  for (const w of walls) [w.a, w.b] = [move(w.a), move(w.b)];
  for (const r of rooms) r.points = r.points.map(move);
  const openings: FloorOpening[] = [];
  for (const o of level.openings) {
    const at = move(o.center);
    let best: { w: FloorWall; offset: number; dist: number } | undefined;
    for (const w of walls) {
      const r = onWall(w, at);
      if (r.dist <= w.thickness / 2 + 300 && (!best || r.dist < best.dist)) best = { w, offset: r.offset, dist: r.dist };
    }
    if (!best) continue;
    const len = wallLength(best.w);
    const width = Math.min(Math.max(o.width || DOOR_WIDTH, 100), len);
    openings.push({ id: newId("o"), wall: best.w.id, kind: o.kind, width, offset: clampOpening(len, best.offset, width) });
  }
  next.walls.push(...walls);
  next.rooms.push(...rooms);
  next.openings.push(...openings);
  next.width = Math.max(next.width, Math.ceil((b.max[0] + dx + 1000) / 1000) * 1000);
  next.height = Math.max(next.height, Math.ceil((b.max[1] + dy + 1000) / 1000) * 1000);
  return { plan: next, walls: walls.length, rooms: rooms.length, openings: openings.length };
}
