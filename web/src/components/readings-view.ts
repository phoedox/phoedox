import { LitElement, css, html, svg } from "lit";
import { api } from "../api";
import { formatDate, locale, t } from "../i18n";
import { icon } from "../icon";
import { isBinary, nearest, plot, type Series } from "../readings";
import { base } from "./shared";

const RANGES = [24, 24 * 7, 24 * 30, 24 * 90];
const W = 640;
const PAD = { l: 44, r: 8, t: 8, b: 22 };

/** History of an entry's measured values: one small chart per metric
 * (each with its own axis), crosshair tooltip, table view (ADR-0051). */
export class PhxReadings extends LitElement {
  static properties = { project: {}, entity: {}, hours: { state: true }, series: { state: true }, hover: { state: true }, table: { state: true } };
  static styles = [base, css`
    :host { display: block; }
    section { margin: 20px 0; display: grid; gap: 12px; }
    .head { display: flex; gap: 10px; align-items: center; flex-wrap: wrap; }
    h3 { margin: 0; font-size: 15px; display: flex; gap: 8px; align-items: center; flex: 1; }
    .seg { display: inline-flex; border: 1px solid var(--line); border-radius: 8px; overflow: hidden; }
    .seg button { border: 0; border-radius: 0; background: none; padding: 4px 10px; font-size: 13px; color: var(--muted); }
    .seg button[aria-pressed="true"] { background: var(--accent-soft); color: var(--fg); font-weight: 600; }
    figure { margin: 0; display: grid; gap: 4px; }
    figcaption { display: flex; gap: 8px; align-items: baseline; flex-wrap: wrap; font-size: 13.5px; }
    figcaption b { font-weight: 600; }
    figcaption .now { font-variant-numeric: tabular-nums; }
    .wrap { position: relative; }
    svg.chart { width: 100%; height: auto; display: block; overflow: visible; }
    .grid { stroke: var(--line); stroke-width: 1; }
    .axis { fill: var(--muted); font-size: 11px; font-variant-numeric: tabular-nums; }
    .band { fill: var(--accent); opacity: 0.12; }
    .line { fill: none; stroke: var(--accent); stroke-width: 2; stroke-linejoin: round; stroke-linecap: round; }
    .cross { stroke: var(--muted); stroke-width: 1; }
    .dot { fill: var(--accent); stroke: var(--surface); stroke-width: 2; }
    .tip { position: absolute; top: 0; transform: translateX(-50%); pointer-events: none; background: var(--surface); border: 1px solid var(--line);
      border-radius: 8px; padding: 4px 8px; font-size: 12.5px; white-space: nowrap; box-shadow: 0 2px 8px rgb(0 0 0 / 0.08); font-variant-numeric: tabular-nums; }
    .hit { fill: transparent; cursor: crosshair; }
    table { width: 100%; font-size: 13px; border-collapse: collapse; }
    td, th { text-align: left; padding: 3px 6px; border-bottom: 1px solid var(--line); font-variant-numeric: tabular-nums; }
  `];
  project = "";
  entity = "";
  hours = 24;
  series?: Series[];
  hover?: { metric: string; t: number };
  table = false;

  updated(changed: Map<string, unknown>) {
    if (changed.has("entity") || changed.has("hours")) this.load();
  }

  private async load() {
    if (!this.entity) return;
    this.series = (await api<Series[]>("GET", `/projects/${this.project}/entities/${encodeURIComponent(this.entity)}/readings?hours=${this.hours}`).catch(() => [])) ?? [];
  }

  private fmt(v: number, unit: string, binary: boolean) {
    if (binary) return v >= 0.5 ? t("readings.on") : t("readings.off");
    const n = new Intl.NumberFormat(locale(), { maximumFractionDigits: Math.abs(v) >= 100 ? 0 : 2 }).format(v);
    return unit ? `${n} ${unit}` : n;
  }

  private time(ms: number) {
    const d = new Date(ms);
    return this.hours <= 24 ? d.toLocaleTimeString(locale(), { hour: "2-digit", minute: "2-digit" })
      : d.toLocaleString(locale(), { day: "numeric", month: "short", hour: "2-digit", minute: "2-digit" });
  }

  private onMove(e: PointerEvent, metric: string, from: number, to: number) {
    const r = (e.currentTarget as SVGRectElement).getBoundingClientRect();
    const x = ((e.clientX - r.left) / r.width) * (to - from) + from;
    this.hover = { metric, t: x };
  }

  private chart(s: Series) {
    const to = Date.now();
    const from = to - this.hours * 3600_000;
    const H = isBinary(s.points) ? 76 : 140;
    const w = W - PAD.l - PAD.r;
    const h = H - PAD.t - PAD.b;
    const p = plot(s.points, from, to, w, h);
    const y = (v: number) => PAD.t + h - ((v - p.yMin) / (p.yMax - p.yMin || 1)) * h;
    const x = (ms: number) => PAD.l + ((ms - from) / (to - from)) * w;
    const hv = this.hover?.metric === s.metric ? nearest(s.points, this.hover.t) : undefined;
    const xTicks = [0, 1, 2, 3, 4].map((i) => from + ((to - from) * i) / 4);
    const label = `${s.metric}: ${t("readings.summary", { n: s.points.length, last: this.fmt(s.last, s.unit, p.binary) })}`;
    return html`<figure>
      <figcaption><b>${s.metric}</b><span class="now">${this.fmt(s.last, s.unit, p.binary)}</span><span class="muted">${formatDate(s.lastAt)}</span></figcaption>
      <div class="wrap">
        <svg class="chart" viewBox="0 0 ${W} ${H}" role="img" aria-label=${label}>
          ${p.ticks.map((v) => svg`<line class="grid" x1=${PAD.l} x2=${W - PAD.r} y1=${y(v)} y2=${y(v)}></line>
            <text class="axis" x=${PAD.l - 6} y=${y(v) + 4} text-anchor="end">${p.binary ? (v ? t("readings.on") : t("readings.off")) : this.fmt(v, "", false)}</text>`)}
          ${xTicks.map((ms, i) => svg`<text class="axis" x=${x(ms)} y=${H - 6} text-anchor=${i === 0 ? "start" : i === 4 ? "end" : "middle"}>${this.time(ms)}</text>`)}
          <g transform="translate(${PAD.l} ${PAD.t})">
            ${p.band ? svg`<path class="band" d=${p.band}></path>` : ""}
            <path class="line" d=${p.line}></path>
          </g>
          ${hv ? svg`<line class="cross" x1=${x(hv[0])} x2=${x(hv[0])} y1=${PAD.t} y2=${PAD.t + h}></line><circle class="dot" r="4" cx=${x(hv[0])} cy=${y(hv[1])}></circle>` : ""}
          <rect class="hit" x=${PAD.l} y="0" width=${w} height=${H}
            @pointermove=${(e: PointerEvent) => this.onMove(e, s.metric, from, to)} @pointerleave=${() => (this.hover = undefined)}></rect>
        </svg>
        ${hv ? html`<div class="tip" style="left:${(x(hv[0]) / W) * 100}%">${this.time(hv[0])} · <b>${this.fmt(hv[1], s.unit, p.binary)}</b>${!p.binary && hv[2] !== hv[3] ? html` <span class="muted">(${this.fmt(hv[2], "", false)}–${this.fmt(hv[3], "", false)})</span>` : ""}</div>` : ""}
      </div>
      ${this.table ? html`<table><thead><tr><th>${t("readings.time")}</th><th>${t("readings.value")}</th><th>${t("readings.range")}</th></tr></thead><tbody>
        ${s.points.slice(-50).reverse().map((pt) => html`<tr><td>${this.time(pt[0])}</td><td>${this.fmt(pt[1], s.unit, p.binary)}</td><td>${pt[2] !== pt[3] ? `${this.fmt(pt[2], "", false)}–${this.fmt(pt[3], "", false)}` : ""}</td></tr>`)}
      </tbody></table>` : ""}
    </figure>`;
  }

  render() {
    if (!this.series || (this.series.length === 0 && this.hours === 24)) return "";
    return html`<section aria-label=${t("readings.title")}>
      <div class="head">
        <h3>${icon("Gauge", 16)} ${t("readings.title")}</h3>
        <div class="seg" role="group" aria-label=${t("readings.period")}>
          ${RANGES.map((h) => html`<button aria-pressed=${h === this.hours} @click=${() => (this.hours = h)}>${h === 24 ? t("readings.24h") : t("readings.days", { n: h / 24 })}</button>`)}
        </div>
        <button class="ghost" aria-pressed=${this.table} @click=${() => (this.table = !this.table)}>${icon("Table", 14)} ${t("readings.table")}</button>
      </div>
      ${this.series.length === 0 ? html`<p class="muted">${t("readings.none")}</p>` : this.series.map((s) => (s.points.length ? this.chart(s) : html`<p class="muted">${s.metric}: ${t("readings.none")}</p>`))}
    </section>`;
  }
}
customElements.define("phx-readings", PhxReadings);
