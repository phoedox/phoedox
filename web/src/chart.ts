// Charts in pages (```chart blocks) and on whiteboards: one JSON format and
// one renderer. Pure (no DOM), so it runs in tests and builds the same SVG
// everywhere. Colours are the whiteboard palette (--tc-* lines and markers,
// --tb-* areas and bands), so charts follow the theme.
//
// Besides everyday line, area, bar and scatter charts it draws annotated
// timelines: numbered points with a title and text (listed under the
// chart), phases as background bands, a time axis made of segments with
// their own scale (compressed and stretched), qualitative y ticks and notes.

export type ChartColor = "gray" | "red" | "orange" | "yellow" | "green" | "blue" | "purple" | "pink";
export const CHART_COLORS: ChartColor[] = ["gray", "red", "orange", "yellow", "green", "blue", "purple", "pink"];
export type XValue = string | number;

export interface ChartPoint { x: XValue; y: number | null; label?: string; title?: string; text?: string; color?: ChartColor }
export interface ChartSeries { name?: string; color?: ChartColor; curve?: "smooth" | "straight" | "step"; dashed?: boolean; markers?: boolean; points: ChartPoint[] }
export interface ChartBand { from: XValue; to: XValue; label?: string; color?: ChartColor }
export interface ChartSegment { from: XValue; to: XValue; weight?: number; label?: string }
export interface ChartNote { x: XValue; y: number; text: string; color?: ChartColor }
export interface ChartTick { value: number; label: string }
export interface ChartSpec {
  type?: "line" | "area" | "bar" | "scatter";
  title?: string;
  description?: string;
  height?: number;
  x?: { type?: "time" | "number" | "category"; label?: string; segments?: ChartSegment[] };
  y?: { label?: string; min?: number; max?: number; unit?: string; ticks?: (number | ChartTick)[] };
  bands?: ChartBand[];
  series?: ChartSeries[];
  /** Short form for one series. */
  points?: ChartPoint[];
  notes?: ChartNote[];
  /** The numbered list of points with a title or text (default: shown when there are any). */
  list?: boolean;
}

export const CHART_TYPES = ["line", "area", "bar", "scatter"];
const SERIES_COLORS: ChartColor[] = ["blue", "orange", "green", "purple", "red", "pink", "yellow", "gray"];
const DATE_RE = /^(\d{1,4})(?:-(\d{2})(?:-(\d{2}))?)?$/;

export interface ChartProblem { key: string; params?: Record<string, string | number>; detail?: string }

/** Parses the JSON of a chart block; problems are i18n keys with params. */
export function parseChart(src: string): { spec?: ChartSpec; problems: ChartProblem[] } {
  let raw: unknown;
  try {
    raw = JSON.parse(src);
  } catch (e) {
    return { problems: [{ key: "chart.err_json", detail: (e as Error).message }] };
  }
  if (!raw || typeof raw !== "object" || Array.isArray(raw)) return { problems: [{ key: "chart.err_json", detail: "not an object" }] };
  const spec = raw as ChartSpec;
  const problems: ChartProblem[] = [];
  if (spec.type && !CHART_TYPES.includes(spec.type)) problems.push({ key: "chart.err_type", params: { type: String(spec.type) } });
  const series = seriesOf(spec);
  if (!series.length || !series.some((s) => Array.isArray(s.points) && s.points.length)) problems.push({ key: "chart.err_points" });
  series.forEach((s, i) => {
    if (!Array.isArray(s.points)) return problems.push({ key: "chart.err_points" });
    s.points.forEach((p, j) => {
      if (!p || typeof p !== "object") problems.push({ key: "chart.err_point", params: { series: i + 1, point: j + 1 } });
      else if (p.y !== null && typeof p.y !== "number") problems.push({ key: "chart.err_y", params: { series: i + 1, point: j + 1 } });
      else if (p.x === undefined || p.x === null || p.x === "") problems.push({ key: "chart.err_x", params: { series: i + 1, point: j + 1 } });
    });
  });
  const colors = [...series.map((s) => s.color), ...series.flatMap((s) => (s.points ?? []).map((p) => p?.color)), ...(spec.bands ?? []).map((b) => b.color), ...(spec.notes ?? []).map((n) => n.color)];
  for (const c of colors) if (c && !CHART_COLORS.includes(c)) problems.push({ key: "chart.err_color", params: { color: String(c) } });
  if (!problems.length) {
    const kind = xKind(spec);
    if (kind !== "category") {
      for (const v of allX(spec)) if (xNum(v, kind) === null) { problems.push({ key: "chart.err_x_value", params: { value: String(v) } }); break; }
      for (const s of spec.x?.segments ?? []) {
        const a = xNum(s.from, kind), b = xNum(s.to, kind);
        if (a === null || b === null || b <= a) problems.push({ key: "chart.err_segment", params: { from: String(s.from), to: String(s.to) } });
      }
    }
  }
  return { spec: problems.length ? undefined : spec, problems };
}

export function seriesOf(spec: ChartSpec): ChartSeries[] {
  if (Array.isArray(spec.series) && spec.series.length) return spec.series;
  return Array.isArray(spec.points) ? [{ points: spec.points }] : [];
}

function allX(spec: ChartSpec): XValue[] {
  return [...seriesOf(spec).flatMap((s) => (s.points ?? []).map((p) => p.x)), ...(spec.bands ?? []).flatMap((b) => [b.from, b.to]), ...(spec.notes ?? []).map((n) => n.x)];
}

/** time (dates like 1848, 1848-03, 1848-03-18), number or category. */
export function xKind(spec: ChartSpec): "time" | "number" | "category" {
  if (spec.x?.type) return spec.x.type;
  if (spec.type === "bar") {
    const xs = allX(spec);
    if (xs.every((v) => typeof v === "number")) return "number";
    return "category";
  }
  const xs = allX(spec);
  if (xs.every((v) => typeof v === "number")) return "number";
  if (xs.every((v) => typeof v === "string" && DATE_RE.test(v.trim()))) return "time";
  return "category";
}

/** A value on a time or number axis (time: milliseconds, UTC). */
export function xNum(v: XValue, kind: "time" | "number" | "category"): number | null {
  if (kind === "number") return typeof v === "number" ? v : Number.isFinite(Number(v)) && String(v).trim() !== "" ? Number(v) : null;
  if (kind !== "time") return null;
  if (typeof v === "number") return Date.UTC(v, 0, 1);
  const m = DATE_RE.exec(String(v).trim());
  if (!m) return null;
  const mo = m[2] ? Number(m[2]) - 1 : 0, d = m[3] ? Number(m[3]) : 1;
  if (mo < 0 || mo > 11 || d < 1 || d > 31) return null;
  const t = new Date(0);
  t.setUTCFullYear(Number(m[1]), mo, d);
  t.setUTCHours(0, 0, 0, 0);
  return t.getTime();
}

const esc = (s: unknown) => String(s ?? "").replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]!);
const r1 = (n: number) => Math.round(n * 10) / 10;

/** Places x values: segments side by side, each with its own linear scale. */
export class XScale {
  readonly segs: { a: number; b: number; x0: number; x1: number; label?: string }[] = [];
  readonly cats: string[] = [];
  constructor(readonly kind: "time" | "number" | "category", spec: ChartSpec, readonly left: number, readonly right: number) {
    if (kind === "category") {
      for (const v of allX(spec)) if (!this.cats.includes(String(v))) this.cats.push(String(v));
      return;
    }
    const given = (spec.x?.segments ?? []).map((s) => ({ a: xNum(s.from, kind)!, b: xNum(s.to, kind)!, w: s.weight && s.weight > 0 ? s.weight : 1, label: s.label }))
      .filter((s) => s.a !== null && s.b !== null && s.b > s.a).sort((p, q) => p.a - q.a);
    let parts = given;
    if (!parts.length) {
      const xs = allX(spec).map((v) => xNum(v, kind)).filter((n): n is number => n !== null);
      let a = Math.min(...xs), b = Math.max(...xs);
      if (!Number.isFinite(a)) [a, b] = [0, 1];
      if (a === b) [a, b] = kind === "time" ? [a - 15768e6, b + 15768e6] : [a - 1, b + 1];
      parts = [{ a, b, w: 1, label: undefined }];
    }
    const total = parts.reduce((s, p) => s + p.w, 0);
    let x = left;
    for (const p of parts) {
      const wpx = ((right - left) * p.w) / total;
      this.segs.push({ a: p.a, b: p.b, x0: x, x1: x + wpx, label: p.label });
      x += wpx;
    }
  }
  /** Width of one category. */
  get step() {
    return this.cats.length ? (this.right - this.left) / this.cats.length : 0;
  }
  at(v: XValue): number {
    if (this.kind === "category") {
      const i = this.cats.indexOf(String(v));
      return this.left + (Math.max(0, i) + 0.5) * this.step;
    }
    const n = xNum(v, this.kind);
    if (n === null) return this.left;
    return this.num(n);
  }
  num(n: number): number {
    const s = this.segs;
    if (n <= s[0].a) return s[0].x0;
    for (const g of s) if (n <= g.b) return n < g.a ? g.x0 : g.x0 + ((n - g.a) / (g.b - g.a)) * (g.x1 - g.x0);
    return s[s.length - 1].x1;
  }
}

const NICE = [1, 2, 2.5, 5, 10];
function niceStep(range: number, count: number): number {
  const raw = range / Math.max(1, count);
  const p = 10 ** Math.floor(Math.log10(raw || 1));
  for (const n of NICE) if (n * p >= raw) return n * p;
  return 10 * p;
}

const fmtNum = (n: number, locale: string) => new Intl.NumberFormat(locale, { maximumFractionDigits: 2 }).format(n);

/** Ticks of the x axis: per segment, as many as fit with at least `gap` pixels between them. */
export function xTicks(sc: XScale, locale = "en", gap = 54): { x: number; label: string }[] {
  if (sc.kind === "category") {
    const every = Math.max(1, Math.ceil(gap / Math.max(1, sc.step)));
    return sc.cats.map((c, i) => ({ x: sc.left + (i + 0.5) * sc.step, label: c })).filter((_, i) => i % every === 0);
  }
  const out: { x: number; label: string }[] = [];
  for (const g of sc.segs) {
    const fits = Math.max(1, Math.floor((g.x1 - g.x0) / gap));
    if (sc.kind === "number") {
      const st = niceStep(g.b - g.a, fits);
      for (let v = Math.ceil(g.a / st) * st; v <= g.b + st * 1e-9; v += st) out.push({ x: sc.num(v), label: fmtNum(Math.round(v * 1e6) / 1e6, locale) });
      continue;
    }
    const a = new Date(g.a), b = new Date(g.b);
    const months = (b.getUTCFullYear() - a.getUTCFullYear()) * 12 + b.getUTCMonth() - a.getUTCMonth();
    if (months < 3) {
      const days = (g.b - g.a) / 864e5;
      const st = [1, 2, 7, 14, 30].find((d) => days / d <= fits) ?? 30;
      const fmt = new Intl.DateTimeFormat(locale, { day: "numeric", month: "short", timeZone: "UTC" });
      for (let t = g.a; t <= g.b; t += st * 864e5) out.push({ x: sc.num(t), label: fmt.format(new Date(t)) });
      continue;
    }
    const st = [1, 2, 3, 6, 12, 24, 60, 120, 240, 600, 1200, 6000].find((m) => months / m <= fits) ?? 6000;
    const mon = new Intl.DateTimeFormat(locale, { month: "short", timeZone: "UTC" });
    let y = a.getUTCFullYear(), m = a.getUTCMonth();
    if (st >= 12) {
      const ys = st / 12;
      if (m > 0 || a.getUTCDate() > 1) y++;
      y = Math.ceil(y / ys) * ys;
      m = 0;
    } else {
      if (a.getUTCDate() > 1) m++;
      m = Math.ceil(m / st) * st;
    }
    let first = true;
    for (;;) {
      const d = new Date(0);
      d.setUTCFullYear(y + Math.floor(m / 12), m % 12, 1);
      const t = d.getTime();
      if (t > g.b) break;
      const label = st >= 12 ? String(d.getUTCFullYear()) : d.getUTCMonth() === 0 || first ? `${mon.format(d)} ${String(d.getUTCFullYear()).slice(-2)}` : mon.format(d);
      out.push({ x: sc.num(t), label });
      first = false;
      m += st;
    }
  }
  return out;
}

/** The y range and ticks: given ones (possibly with words like "low"), or nice numbers. */
export function yScale(spec: ChartSpec, locale = "en"): { min: number; max: number; ticks: ChartTick[] } {
  const given = (spec.y?.ticks ?? []).map((t) => (typeof t === "number" ? { value: t, label: fmtNum(t, locale) } : { value: Number(t.value), label: String(t.label ?? t.value) }))
    .filter((t) => Number.isFinite(t.value));
  const ys = [...seriesOf(spec).flatMap((s) => (s.points ?? []).map((p) => p.y)), ...(spec.notes ?? []).map((n) => n.y), ...given.map((t) => t.value)]
    .filter((v): v is number => typeof v === "number" && Number.isFinite(v));
  let lo = spec.y?.min ?? Math.min(...ys), hi = spec.y?.max ?? Math.max(...ys);
  if (!Number.isFinite(lo)) lo = 0;
  if (!Number.isFinite(hi)) hi = 1;
  if (spec.y?.min === undefined && lo > 0 && (spec.type === "bar" || spec.type === "area" || lo < hi * 0.5)) lo = 0;
  if (hi === lo) hi = lo + 1;
  if (given.length) return { min: lo, max: hi, ticks: given };
  const st = niceStep(hi - lo, 5);
  if (spec.y?.min === undefined) lo = Math.floor(lo / st) * st;
  if (spec.y?.max === undefined) hi = Math.ceil(hi / st) * st;
  const ticks: ChartTick[] = [];
  const unit = spec.y?.unit ? ` ${spec.y.unit}` : "";
  for (let v = lo; v <= hi + st * 1e-9; v += st) ticks.push({ value: Math.round(v * 1e9) / 1e9, label: fmtNum(Math.round(v * 1e9) / 1e9, locale) + unit });
  return { min: lo, max: hi, ticks };
}

function linePath(p: [number, number][]): string {
  return p.map((q, i) => `${i ? "L" : "M"}${r1(q[0])},${r1(q[1])}`).join("");
}

function stepPath(p: [number, number][]): string {
  return p.map((q, i) => (i ? `H${r1(q[0])}V${r1(q[1])}` : `M${r1(q[0])},${r1(q[1])}`)).join("");
}

/** A smooth curve that never overshoots its points (monotone cubic, Fritsch–Carlson). */
export function smoothPath(p: [number, number][]): string {
  const n = p.length;
  if (n < 3) return linePath(p);
  for (let i = 0; i < n - 1; i++) if (p[i + 1][0] <= p[i][0]) return linePath(p);
  const dx: number[] = [], m: number[] = [];
  for (let i = 0; i < n - 1; i++) {
    dx.push(p[i + 1][0] - p[i][0]);
    m.push((p[i + 1][1] - p[i][1]) / dx[i]);
  }
  const t = new Array<number>(n);
  t[0] = m[0];
  t[n - 1] = m[n - 2];
  for (let i = 1; i < n - 1; i++) {
    t[i] = m[i - 1] * m[i] <= 0 ? 0 : (3 * (dx[i - 1] + dx[i])) / ((2 * dx[i] + dx[i - 1]) / m[i - 1] + (dx[i] + 2 * dx[i - 1]) / m[i]);
  }
  let d = `M${r1(p[0][0])},${r1(p[0][1])}`;
  for (let i = 0; i < n - 1; i++) {
    const h = dx[i] / 3;
    d += `C${r1(p[i][0] + h)},${r1(p[i][1] + t[i] * h)} ${r1(p[i + 1][0] - h)},${r1(p[i + 1][1] - t[i + 1] * h)} ${r1(p[i + 1][0])},${r1(p[i + 1][1])}`;
  }
  return d;
}

/** The colour of a series. */
export function seriesColor(spec: ChartSpec, i: number): ChartColor {
  const s = seriesOf(spec)[i];
  if (s?.color) return s.color;
  // With phases the points carry the colours; the line stays neutral.
  if (spec.bands?.length) return "gray";
  return SERIES_COLORS[i % SERIES_COLORS.length];
}

/** The colour of a point: its own, else the band it lies in, else its series'. */
export function pointColor(spec: ChartSpec, si: number, p: ChartPoint): ChartColor {
  if (p.color) return p.color;
  const kind = xKind(spec);
  if (kind !== "category" && spec.bands?.length) {
    const v = xNum(p.x, kind);
    if (v !== null) {
      const b = spec.bands.find((b) => {
        const a = xNum(b.from, kind), z = xNum(b.to, kind);
        return a !== null && z !== null && v >= a && v <= z;
      });
      if (b?.color) return b.color;
    }
  }
  return seriesColor(spec, si);
}

/** Points with a title or text, numbered: the list under the chart. */
export function chartItems(spec: ChartSpec): { key: string; n: string; title: string; text: string; color: ChartColor }[] {
  const out: { key: string; n: string; title: string; text: string; color: ChartColor }[] = [];
  let k = 0;
  seriesOf(spec).forEach((s, si) => (s.points ?? []).forEach((p, pi) => {
    if (!p.title && !p.text) return;
    k++;
    out.push({ key: `${si}:${pi}`, n: p.label ?? String(k), title: p.title ?? "", text: p.text ?? "", color: pointColor(spec, si, p) });
  }));
  return out;
}

export interface ChartSize { width: number; height?: number; locale?: string }

/** The chart as SVG markup (the plot with axes, bands, series, markers, notes). */
export function chartSVG(spec: ChartSpec, size: ChartSize): string {
  const W = Math.max(240, Math.round(size.width));
  const H = Math.max(140, Math.round(size.height ?? Math.min(800, Math.max(160, spec.height ?? 300))));
  const locale = size.locale ?? "en";
  const kind = xKind(spec);
  const ys = yScale(spec, locale);
  const longest = Math.max(...ys.ticks.map((t) => t.label.length), 1);
  // Room above for numbered markers at the top of the range.
  const L = Math.min(140, 14 + longest * 6.4 + (spec.y?.label ? 18 : 0)), R = 16, T = 18;
  const segLabels = (spec.x?.segments ?? []).some((s) => s.label);
  const B = 30 + (spec.x?.label ? 18 : 0) + (segLabels ? 16 : 0);
  const sc = new XScale(kind, spec, L, W - R);
  const plotB = H - B;
  const yAt = (v: number) => plotB - ((v - ys.min) / (ys.max - ys.min)) * (plotB - T);
  const parts: string[] = [];
  const stroke = (c: ChartColor) => `var(--tc-${c})`;
  const fill = (c: ChartColor) => `var(--tb-${c})`;

  // Bands (phases) behind everything, labelled at the bottom.
  for (const b of spec.bands ?? []) {
    if (kind === "category") break;
    const x0 = sc.at(b.from), x1 = sc.at(b.to);
    if (x1 - x0 < 0.5) continue;
    parts.push(`<rect x="${r1(x0)}" y="${T}" width="${r1(x1 - x0)}" height="${r1(plotB - T)}" style="fill:${fill(b.color ?? "gray")};opacity:.75"/>`);
    if (b.label && x1 - x0 > b.label.length * 5.6 + 8) {
      parts.push(`<text x="${r1((x0 + x1) / 2)}" y="${r1(plotB - 7)}" text-anchor="middle" style="font-size:10.5px;font-weight:600;fill:var(--muted)">${esc(b.label)}</text>`);
    }
  }
  // Grid and y axis.
  for (const t of ys.ticks) {
    const y = yAt(t.value);
    if (y < T - 0.5 || y > plotB + 0.5) continue;
    parts.push(`<line x1="${L}" x2="${W - R}" y1="${r1(y)}" y2="${r1(y)}" style="stroke:var(--line);stroke-width:1"/>`);
    parts.push(`<text x="${L - 6}" y="${r1(y + 3.5)}" text-anchor="end" style="font-size:10.5px;fill:var(--muted)">${esc(t.label)}</text>`);
  }
  if (spec.y?.label) {
    parts.push(`<text transform="translate(12 ${r1((T + plotB) / 2)}) rotate(-90)" text-anchor="middle" style="font-size:10.5px;fill:var(--muted)">${esc(spec.y.label)}</text>`);
  }
  // Borders between segments of the time axis.
  sc.segs.slice(1).forEach((g) => parts.push(`<line x1="${r1(g.x0)}" x2="${r1(g.x0)}" y1="${T}" y2="${plotB}" style="stroke:var(--muted);stroke-width:1;stroke-dasharray:2 3"/>`));
  // X axis.
  parts.push(`<line x1="${L}" x2="${W - R}" y1="${plotB}" y2="${plotB}" style="stroke:var(--muted);stroke-width:1"/>`);
  let lastX = -1e9;
  for (const t of xTicks(sc, locale)) {
    const w = t.label.length * 5.8;
    if (t.x - w / 2 < lastX + 4) continue;
    lastX = t.x + w / 2;
    parts.push(`<line x1="${r1(t.x)}" x2="${r1(t.x)}" y1="${plotB}" y2="${plotB + 4}" style="stroke:var(--muted)"/>`);
    parts.push(`<text x="${r1(t.x)}" y="${plotB + 16}" text-anchor="middle" style="font-size:10.5px;fill:var(--muted)">${esc(t.label)}</text>`);
  }
  let below = plotB + 30;
  if (segLabels) {
    for (const g of sc.segs) if (g.label) parts.push(`<text x="${r1((g.x0 + g.x1) / 2)}" y="${below}" text-anchor="middle" style="font-size:10px;font-style:italic;fill:var(--muted)">${esc(g.label)}</text>`);
    below += 16;
  }
  if (spec.x?.label) parts.push(`<text x="${r1((L + W - R) / 2)}" y="${below}" text-anchor="middle" style="font-size:10.5px;fill:var(--muted)">${esc(spec.x.label)}</text>`);

  const series = seriesOf(spec);
  const type = spec.type ?? "line";
  // Bars.
  if (type === "bar") {
    const n = series.length;
    const step = kind === "category" ? sc.step : Math.max(8, (W - L - R) / Math.max(1, Math.max(...series.map((s) => s.points.length))));
    const bw = Math.max(3, Math.min(40, (step * 0.7) / n));
    const base = yAt(Math.max(ys.min, Math.min(ys.max, 0)));
    series.forEach((s, si) => s.points.forEach((p, pi) => {
      if (typeof p.y !== "number") return;
      const cx = sc.at(p.x) + (si - (n - 1) / 2) * bw;
      const y = yAt(p.y);
      const c = p.color ?? seriesColor(spec, si);
      parts.push(`<g class="mk" tabindex="0" data-k="${si}:${pi}" role="img" aria-label="${esc(ariaOf(s, p))}"><title>${esc(ariaOf(s, p))}</title><rect x="${r1(cx - bw / 2)}" y="${r1(Math.min(y, base))}" width="${r1(bw - 1)}" height="${r1(Math.max(1, Math.abs(base - y)))}" rx="2" style="fill:${stroke(c)}"/></g>`);
    }));
  } else {
    // Lines and areas, then markers on top.
    series.forEach((s, si) => {
      const c = seriesColor(spec, si);
      const runs: [number, number][][] = [[]];
      for (const p of s.points) {
        if (typeof p.y !== "number") {
          if (runs[runs.length - 1].length) runs.push([]);
          continue;
        }
        runs[runs.length - 1].push([sc.at(p.x), yAt(p.y)]);
      }
      for (const run of runs) {
        if (run.length < 2 || type === "scatter") continue;
        const d = s.curve === "step" ? stepPath(run) : s.curve === "straight" ? linePath(run) : smoothPath(run);
        if (type === "area") parts.push(`<path d="${d}L${r1(run[run.length - 1][0])},${plotB}L${r1(run[0][0])},${plotB}Z" style="fill:${fill(c)};opacity:.8"/>`);
        parts.push(`<path d="${d}" style="fill:none;stroke:${stroke(c)};stroke-width:2;stroke-linejoin:round;stroke-linecap:round${s.dashed ? ";stroke-dasharray:6 5" : ""}"/>`);
      }
    });
    series.forEach((s, si) => s.points.forEach((p, pi) => {
      if (typeof p.y !== "number") return;
      const big = !!p.label;
      if (s.markers === false && !big && !p.title && !p.text) return;
      if (type === "line" || type === "area") if (!big && !p.title && !p.text && s.markers !== true) return;
      const x = sc.at(p.x), y = yAt(p.y), c = pointColor(spec, si, p), a = ariaOf(s, p);
      const r = big ? Math.min(10, 6 + p.label!.length * 2) : 4.5;
      parts.push(`<g class="mk" tabindex="0" data-k="${si}:${pi}" role="img" aria-label="${esc(a)}"><title>${esc(a)}</title><circle cx="${r1(x)}" cy="${r1(y)}" r="${r}" style="fill:${stroke(c)};stroke:var(--surface);stroke-width:1.5"/>${big ? `<text x="${r1(x)}" y="${r1(y + 3.5)}" text-anchor="middle" style="font-size:${p.label!.length > 2 ? 8.5 : 10}px;font-weight:700;fill:var(--surface);pointer-events:none">${esc(p.label)}</text>` : ""}</g>`);
    }));
  }
  // Notes on the plot.
  for (const n of spec.notes ?? []) {
    if (typeof n.y !== "number") continue;
    const x = sc.at(n.x), y = yAt(n.y);
    const anchor = x > W - R - 80 ? "end" : "start";
    parts.push(`<text x="${r1(anchor === "end" ? x - 4 : x + 4)}" y="${r1(y)}" text-anchor="${anchor}" style="font-size:10.5px;font-style:italic;fill:${n.color ? stroke(n.color) : "var(--good)"}">${esc(n.text)}</text>`);
  }
  const label = spec.title ?? "";
  return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${W} ${H}" width="${W}" height="${H}" role="img" aria-label="${esc(label)}" style="font-family:var(--font-body,system-ui,sans-serif);display:block;max-width:100%;height:auto">${parts.join("")}</svg>`;
}

function ariaOf(s: ChartSeries, p: ChartPoint): string {
  const head = [p.label, p.title].filter(Boolean).join(" ");
  const val = `${s.name ? s.name + ": " : ""}${p.x} → ${p.y}`;
  return [head, p.text, head || p.text ? "" : val].filter(Boolean).join(" – ") || val;
}

/** An example for new chart blocks (an annotated timeline). */
export const CHART_EXAMPLE = `{
  "title": "Load of the home server",
  "y": { "label": "Load", "ticks": [{ "value": 0, "label": "low" }, { "value": 5, "label": "medium" }, { "value": 10, "label": "high" }] },
  "bands": [
    { "from": "2026-01", "to": "2026-03", "label": "Old NAS", "color": "orange" },
    { "from": "2026-03", "to": "2026-06", "label": "New NAS", "color": "blue" }
  ],
  "points": [
    { "x": "2026-01", "y": 3, "label": "1", "title": "January", "text": "Backups run at night." },
    { "x": "2026-02-15", "y": 8, "label": "2", "title": "Photo import", "text": "20,000 pictures indexed." },
    { "x": "2026-04", "y": 4, "label": "3", "title": "New NAS", "text": "Faster disks, same jobs." },
    { "x": "2026-06", "y": 5 }
  ]
}`;
