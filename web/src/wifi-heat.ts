// Wi-Fi heat map and speed per room (ADR-0047): pure functions over the
// measurements of a floor plan, so they can be tested without a browser.
import { inside, type FloorMeasurement, type FloorRoom } from "./floorplan";

export type Metric = "rssi" | "down";
export interface Cell { x: number; y: number; w: number; h: number; v: number }

const value = (m: FloorMeasurement, metric: Metric) => (metric === "rssi" ? m.rssi : m.down) || undefined;

/** Inverse distance weighting over a grid of cells (power 2). Cells farther
 * than reach (mm) from every measurement stay empty. */
export function heatGrid(ms: FloorMeasurement[], metric: Metric, box: { x: number; y: number; w: number; h: number }, cells = 40, reach = 8000): Cell[] {
  const pts = ms.filter((m) => value(m, metric) !== undefined);
  if (!pts.length || box.w <= 0 || box.h <= 0) return [];
  const size = Math.max(box.w, box.h) / cells;
  const out: Cell[] = [];
  for (let y = box.y; y < box.y + box.h; y += size) {
    for (let x = box.x; x < box.x + box.w; x += size) {
      const cx = x + size / 2, cy = y + size / 2;
      let num = 0, den = 0, near = Infinity;
      for (const m of pts) {
        const d2 = (m.x - cx) ** 2 + (m.y - cy) ** 2;
        near = Math.min(near, d2);
        if (d2 < 1) { num = value(m, metric)!; den = 1; break; }
        num += value(m, metric)! / d2;
        den += 1 / d2;
      }
      if (near <= reach * reach) out.push({ x, y, w: size, h: size, v: num / den });
    }
  }
  return out;
}

/** 0 (bad) … 1 (good): signal from -90 to -50 dBm, download from 5 to 300 Mbit/s (log). */
export function quality(metric: Metric, v: number): number {
  if (metric === "rssi") return Math.min(1, Math.max(0, (v + 90) / 40));
  if (v <= 0) return 0;
  return Math.min(1, Math.max(0, Math.log(v / 5) / Math.log(300 / 5)));
}

/** A colour token for a quality (tokens live in tokens.css). */
export const heatColor = (q: number) => (q >= 0.75 ? "var(--good)" : q >= 0.5 ? "var(--signal)" : q >= 0.25 ? "var(--accent)" : "var(--bad)");

export interface RoomStats { room: string; name: string; count: number; rssi?: number; down?: number; up?: number; ping?: number }

const avg = (xs: (number | undefined)[]) => {
  const v = xs.filter((x): x is number => typeof x === "number" && x !== 0);
  return v.length ? Math.round((v.reduce((a, b) => a + b, 0) / v.length) * 10) / 10 : undefined;
};

/** Averages of the measurements inside each room. */
export function roomStats(rooms: FloorRoom[], ms: FloorMeasurement[]): RoomStats[] {
  const out: RoomStats[] = [];
  for (const r of rooms) {
    const inRoom = ms.filter((m) => inside(m.x, m.y, r.points));
    if (!inRoom.length) continue;
    out.push({ room: r.id, name: r.name, count: inRoom.length, rssi: avg(inRoom.map((m) => m.rssi)), down: avg(inRoom.map((m) => m.down)),
      up: avg(inRoom.map((m) => m.up)), ping: avg(inRoom.map((m) => m.ping)) });
  }
  return out.sort((a, b) => (a.down ?? a.rssi ?? 0) - (b.down ?? b.rssi ?? 0));
}
