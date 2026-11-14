/** Geometry for the history charts of an entry: one series per chart,
 * [unix ms, average, min, max] points. */
export type Point = [number, number, number, number];
export interface Series { metric: string; unit: string; last: number; lastAt: string; points: Point[] }

export interface Plot { line: string; band: string; yMin: number; yMax: number; ticks: number[]; binary: boolean }

/** Whether a series only holds 0 and 1 (switch states). */
export const isBinary = (pts: Point[]) => pts.length > 0 && pts.every((p) => (p[2] === 0 || p[2] === 1) && (p[3] === 0 || p[3] === 1));

/** "Nice" axis ticks covering [lo, hi]. */
export function niceTicks(lo: number, hi: number, count = 4): number[] {
  if (!(hi > lo)) {
    const pad = Math.abs(lo) * 0.1 || 1;
    lo -= pad;
    hi += pad;
  }
  const raw = (hi - lo) / count;
  const mag = 10 ** Math.floor(Math.log10(raw));
  const step = [1, 2, 2.5, 5, 10].map((m) => m * mag).find((s) => s >= raw) ?? raw;
  const start = Math.floor(lo / step) * step;
  const out: number[] = [];
  for (let v = start; v <= hi + step * 0.5; v += step) out.push(Number(v.toFixed(10)));
  return out;
}

/** SVG paths for a series inside w×h, time from `from` to `to` (ms). */
export function plot(pts: Point[], from: number, to: number, w: number, h: number): Plot {
  const binary = isBinary(pts);
  let lo = Math.min(...pts.map((p) => p[2]));
  let hi = Math.max(...pts.map((p) => p[3]));
  const ticks = binary ? [0, 1] : niceTicks(lo, hi);
  if (binary) [lo, hi] = [-0.1, 1.1];
  else [lo, hi] = [ticks[0], ticks[ticks.length - 1]];
  const x = (t: number) => ((t - from) / Math.max(1, to - from)) * w;
  const y = (v: number) => h - ((v - lo) / (hi - lo || 1)) * h;
  const f = (n: number) => n.toFixed(1);
  let line = "";
  let band = "";
  pts.forEach((p, i) => {
    const px = f(x(p[0]));
    if (binary && i > 0) line += `H${px}V${f(y(p[1]))}`; // steps for on/off
    else line += `${i ? "L" : "M"}${px},${f(y(p[1]))}`;
  });
  if (binary && pts.length) line += `H${f(x(to))}`;
  if (!binary && pts.some((p) => p[2] !== p[3])) {
    band = pts.map((p, i) => `${i ? "L" : "M"}${f(x(p[0]))},${f(y(p[3]))}`).join("") +
      [...pts].reverse().map((p) => `L${f(x(p[0]))},${f(y(p[2]))}`).join("") + "Z";
  }
  return { line, band, yMin: lo, yMax: hi, ticks, binary };
}

/** The point nearest to time t (binary search; points are sorted). */
export function nearest(pts: Point[], t: number): Point | undefined {
  if (!pts.length) return undefined;
  let a = 0;
  let b = pts.length - 1;
  while (b - a > 1) {
    const m = (a + b) >> 1;
    if (pts[m][0] < t) a = m;
    else b = m;
  }
  return Math.abs(pts[a][0] - t) <= Math.abs(pts[b][0] - t) ? pts[a] : pts[b];
}
