// The chart editor's model (phx-chart-form): editing a ChartSpec as series,
// phases, notes … and writing it back as tidy JSON. Pure, so it is tested.
import { xKind, type ChartSeries, type ChartSpec, type XValue } from "./chart";

/** The series to edit: the short form "points" becomes one series. */
export function editableSeries(spec: ChartSpec): ChartSeries[] {
  if (spec.series?.length) return spec.series;
  return [{ points: spec.points ?? [] }];
}

/** Writes the series back: one plain series stays the short form "points". */
export function withSeries(spec: ChartSpec, series: ChartSeries[]): ChartSpec {
  const out: ChartSpec = { ...spec };
  const [first] = series;
  const plain = series.length === 1 && !first.name && !first.color && !first.curve && !first.dashed && first.markers === undefined;
  if (plain && !spec.series) {
    out.points = first.points;
    delete out.series;
  } else {
    out.series = series;
    delete out.points;
  }
  return out;
}

/** An x value typed into a field: a number on a number axis, else text (dates stay text). */
export function xFrom(text: string, spec: ChartSpec): XValue {
  const s = text.trim();
  if (s !== "" && xKind(spec) === "number" && !Number.isNaN(Number(s))) return Number(s);
  return s;
}

/** A y value typed into a field: a number, or null for a gap. */
export function yFrom(text: string): number | null {
  const s = text.trim().replace(",", ".");
  if (s === "") return null;
  const n = Number(s);
  return Number.isNaN(n) ? null : n;
}

/** An optional number field: undefined when empty. */
export function numFrom(text: string): number | undefined {
  const s = text.trim().replace(",", ".");
  if (s === "") return undefined;
  const n = Number(s);
  return Number.isNaN(n) ? undefined : n;
}

/** Removes empty values (undefined, "", empty objects and lists) so the source stays short. */
export function tidy<T>(v: T): T {
  if (Array.isArray(v)) return v.map((x) => (x && typeof x === "object" ? tidy(x) : x)) as T;
  if (!v || typeof v !== "object") return v;
  const out: Record<string, unknown> = {};
  for (const [k, x] of Object.entries(v as Record<string, unknown>)) {
    if (x === undefined || x === "" || x === false && k !== "list") continue;
    const t = x && typeof x === "object" ? tidy(x) : x;
    if (t && typeof t === "object" && !Array.isArray(t) && Object.keys(t).length === 0) continue;
    if (Array.isArray(t) && t.length === 0) continue;
    out[k] = t;
  }
  return out as T;
}

/** The chart as source text (two-space JSON, empty values left out). */
export function chartSource(spec: ChartSpec): string {
  return JSON.stringify(tidy(spec), null, 2);
}
