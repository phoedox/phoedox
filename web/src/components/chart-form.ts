// <phx-chart-form .spec=…>: a chart edited in fields instead of JSON –
// general settings, axes, series with their points, phases, notes, named
// y values and time segments. Fires "change" with the new spec.
import { LitElement, css, html, nothing, type TemplateResult } from "lit";
import { CHART_COLORS, CHART_TYPES, type ChartBand, type ChartColor, type ChartNote, type ChartPoint, type ChartSegment, type ChartSeries, type ChartSpec } from "../chart";
import { editableSeries, numFrom, withSeries, xFrom, yFrom } from "../chart-form-model";
import { t, type Key } from "../i18n";
import { icon } from "../icon";
import { base, foldCss } from "./shared";

type Row = Record<string, unknown>;

export class PhxChartForm extends LitElement {
  static properties = { spec: { attribute: false }, open: { state: true } };
  static styles = [base, foldCss, css`
    :host { display: block; }
    .grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(150px, 1fr)); gap: 10px; }
    label { display: grid; gap: 4px; font-size: 12.5px; color: var(--muted); }
    label.check { display: flex; gap: 6px; align-items: center; color: var(--fg); }
    label.check input { width: auto; }
    input, select { font-size: 13.5px; }
    .seg { display: inline-flex; border: 1px solid var(--line); border-radius: 8px; overflow: hidden; }
    .seg button { border: 0; border-radius: 0; background: transparent; color: var(--muted); padding: 5px 12px; font-size: 13px; }
    .seg button[aria-pressed="true"] { background: var(--accent-soft); color: var(--fg); }
    table { width: 100%; border-collapse: collapse; font-size: 13px; }
    th { text-align: left; font-weight: 500; color: var(--muted); font-size: 12px; padding: 0 4px 4px; }
    td { padding: 2px 4px; vertical-align: middle; }
    td input, td select { padding: 4px 6px; min-height: 0; }
    td.n { width: 1%; color: var(--muted); font-variant-numeric: tabular-nums; text-align: right; }
    td.x { width: 1%; }
    button.icon { padding: 4px; border: 0; background: transparent; color: var(--muted); }
    @media (hover: hover) and (pointer: fine) { button.icon:hover { color: var(--bad); } }
    .add { justify-self: start; display: inline-flex; gap: 5px; align-items: center; font-size: 13px; padding: 5px 10px; }
    .series { border: 1px solid var(--line); border-radius: 8px; padding: 10px; display: grid; gap: 10px; background: var(--bg); }
    .series header { display: flex; gap: 8px; align-items: center; }
    .series header input { flex: 1; }
    .dot { width: 12px; height: 12px; border-radius: 50%; display: inline-block; flex: none; }
    .muted { color: var(--muted); font-size: 12.5px; margin: 0; }
  `];
  spec: ChartSpec = {};

  private emit(spec: ChartSpec) {
    this.spec = spec;
    this.dispatchEvent(new CustomEvent("change", { detail: spec }));
  }

  private set(patch: Partial<ChartSpec>) {
    this.emit({ ...this.spec, ...patch });
  }

  private colorSelect(value: ChartColor | undefined, onChange: (c: ChartColor | undefined) => void, auto = true) {
    return html`<select aria-label=${t("chartf.color")} @change=${(e: Event) => onChange(((e.target as HTMLSelectElement).value || undefined) as ChartColor | undefined)}>
      ${auto ? html`<option value="" ?selected=${!value}>${t("chartf.auto")}</option>` : nothing}
      ${CHART_COLORS.map((c) => html`<option value=${c} ?selected=${value === c}>${t(`color.${c}` as Key)}</option>`)}
    </select>`;
  }

  /** A table of rows with add and remove; cells come from the caller. */
  private rows<R extends Row>(rows: R[], head: string[], cells: (r: R, i: number, set: (p: Partial<R>) => void) => TemplateResult[], change: (rows: R[]) => void, add: () => R, addLabel: string) {
    const setRow = (i: number) => (p: Partial<R>) => change(rows.map((r, k) => (k === i ? { ...r, ...p } : r)));
    return html`${rows.length ? html`<table>
        <thead><tr><th></th>${head.map((h) => html`<th>${h}</th>`)}<th></th></tr></thead>
        <tbody>${rows.map((r, i) => html`<tr><td class="n">${i + 1}</td>${cells(r, i, setRow(i)).map((c) => html`<td>${c}</td>`)}
          <td class="x"><button class="icon" aria-label=${t("chartf.remove")} title=${t("chartf.remove")} @click=${() => change(rows.filter((_, k) => k !== i))}>${icon("X", 14)}</button></td></tr>`)}</tbody>
      </table>` : nothing}
      <button class="ghost add" @click=${() => change([...rows, add()])}>${icon("Plus", 13)} ${addLabel}</button>`;
  }

  private text(value: unknown, onInput: (v: string) => void, label: string, placeholder = "") {
    return html`<input aria-label=${label} placeholder=${placeholder} .value=${value === undefined || value === null ? "" : String(value)} @change=${(e: Event) => onInput((e.target as HTMLInputElement).value)}>`;
  }

  private general() {
    const s = this.spec;
    return html`<details class="sec" open><summary><span class="chev">${icon("ChevronRight", 14)}</span>${icon("Settings2", 15)} ${t("chartf.general")}</summary><div class="body">
      <div class="grid">
        <label>${t("chartf.title")}${this.text(s.title, (v) => this.set({ title: v || undefined }), t("chartf.title"))}</label>
        <label>${t("chartf.height")}${this.text(s.height, (v) => this.set({ height: numFrom(v) }), t("chartf.height"), "320")}</label>
      </div>
      <label>${t("chartf.description")}${this.text(s.description, (v) => this.set({ description: v || undefined }), t("chartf.description"))}</label>
      <div class="seg" role="group" aria-label=${t("chartf.type")}>${CHART_TYPES.map((ty) => html`<button aria-pressed=${(s.type ?? "line") === ty ? "true" : "false"}
        @click=${() => this.set({ type: ty === "line" ? undefined : (ty as ChartSpec["type"]) })}>${t(`chartf.type_${ty}` as Key)}</button>`)}</div>
    </div></details>`;
  }

  private axes() {
    const s = this.spec, x = s.x ?? {}, y = s.y ?? {};
    return html`<details class="sec" open><summary><span class="chev">${icon("ChevronRight", 14)}</span>${icon("Ruler", 15)} ${t("chartf.axes")}</summary><div class="body">
      <div class="grid">
        <label>${t("chartf.x_label")}${this.text(x.label, (v) => this.set({ x: { ...x, label: v || undefined } }), t("chartf.x_label"))}</label>
        <label>${t("chartf.x_type")}<select @change=${(e: Event) => this.set({ x: { ...x, type: ((e.target as HTMLSelectElement).value || undefined) as "time" | "number" | "category" | undefined } })}>
          ${["", "time", "number", "category"].map((k) => html`<option value=${k} ?selected=${(x.type ?? "") === k}>${t(`chartf.x_${k || "auto"}` as Key)}</option>`)}</select></label>
      </div>
      <div class="grid">
        <label>${t("chartf.y_label")}${this.text(y.label, (v) => this.set({ y: { ...y, label: v || undefined } }), t("chartf.y_label"))}</label>
        <label>${t("chartf.y_unit")}${this.text(y.unit, (v) => this.set({ y: { ...y, unit: v || undefined } }), t("chartf.y_unit"), "%")}</label>
        <label>${t("chartf.y_min")}${this.text(y.min, (v) => this.set({ y: { ...y, min: numFrom(v) } }), t("chartf.y_min"), t("chartf.auto"))}</label>
        <label>${t("chartf.y_max")}${this.text(y.max, (v) => this.set({ y: { ...y, max: numFrom(v) } }), t("chartf.y_max"), t("chartf.auto"))}</label>
      </div>
    </div></details>`;
  }

  private seriesSection() {
    const s = this.spec;
    const series = editableSeries(s);
    const change = (list: ChartSeries[]) => this.emit(withSeries(s, list));
    const setSeries = (i: number, p: Partial<ChartSeries>) => change(series.map((x, k) => (k === i ? { ...x, ...p } : x)));
    return html`<details class="sec" open><summary><span class="chev">${icon("ChevronRight", 14)}</span>${icon("ChartLine", 15)} ${t("chartf.series")}</summary><div class="body">
      <p class="muted">${t("chartf.points_hint")}</p>
      ${series.map((se, i) => html`<div class="series">
        <header>
          ${series.length > 1 || se.name ? html`<input aria-label=${t("chartf.series_name")} placeholder=${t("chartf.series_name")} .value=${se.name ?? ""} @change=${(e: Event) => setSeries(i, { name: (e.target as HTMLInputElement).value || undefined })}>` : html`<span style="flex:1"></span>`}
          ${this.colorSelect(se.color, (c) => setSeries(i, { color: c }))}
          <select aria-label=${t("chartf.curve")} @change=${(e: Event) => setSeries(i, { curve: ((e.target as HTMLSelectElement).value || undefined) as ChartSeries["curve"] })}>
            ${["", "straight", "step"].map((c) => html`<option value=${c} ?selected=${(se.curve ?? "") === c}>${t(`chartf.curve_${c || "smooth"}` as Key)}</option>`)}</select>
          <label class="check"><input type="checkbox" .checked=${!!se.dashed} @change=${(e: Event) => setSeries(i, { dashed: (e.target as HTMLInputElement).checked || undefined })}>${t("chartf.dashed")}</label>
          ${series.length > 1 ? html`<button class="icon" aria-label=${t("chartf.remove_series")} title=${t("chartf.remove_series")} @click=${() => change(series.filter((_, k) => k !== i))}>${icon("Trash2", 14)}</button>` : nothing}
        </header>
        ${this.rows<ChartPoint & Row>(se.points as (ChartPoint & Row)[], [t("chartf.x"), t("chartf.y"), t("chartf.point_title"), t("chartf.point_text")],
          (p, _i, set) => [
            this.text(p.x, (v) => set({ x: xFrom(v, s) }), t("chartf.x"), "2026-01"),
            this.text(p.y, (v) => set({ y: yFrom(v) }), t("chartf.y"), "0"),
            this.text(p.title, (v) => set({ title: v || undefined }), t("chartf.point_title")),
            this.text(p.text, (v) => set({ text: v || undefined }), t("chartf.point_text")),
          ],
          (pts) => setSeries(i, { points: pts }),
          () => ({ x: "", y: null }) as ChartPoint & Row, t("chartf.add_point"))}
      </div>`)}
      <button class="ghost add" @click=${() => change([...series, { name: t("chartf.series_n", { n: series.length + 1 }), points: [] }])}>${icon("Plus", 13)} ${t("chartf.add_series")}</button>
    </div></details>`;
  }

  private bands() {
    const s = this.spec;
    return html`<details class="sec" open><summary><span class="chev">${icon("ChevronRight", 14)}</span>${icon("Columns3", 15)} ${t("chartf.bands")}</summary><div class="body">
      <p class="muted">${t("chartf.bands_hint")}</p>
      ${this.rows((s.bands ?? []) as (Row & ChartBand)[], [t("chartf.from"), t("chartf.to"), t("chartf.label"), t("chartf.color")],
        (b, _i, set) => [
          this.text(b.from, (v) => set({ from: xFrom(v, s) }), t("chartf.from")),
          this.text(b.to, (v) => set({ to: xFrom(v, s) }), t("chartf.to")),
          this.text(b.label, (v) => set({ label: v || undefined }), t("chartf.label")),
          this.colorSelect(b.color as ChartColor | undefined, (c) => set({ color: c })),
        ],
        (rows) => this.set({ bands: rows as ChartSpec["bands"] }), () => ({ from: "", to: "" }), t("chartf.add_band"))}
    </div></details>`;
  }

  private notes() {
    const s = this.spec;
    return html`<details class="sec" open><summary><span class="chev">${icon("ChevronRight", 14)}</span>${icon("StickyNote", 15)} ${t("chartf.notes")}</summary><div class="body">
      ${this.rows((s.notes ?? []) as (Row & ChartNote)[], [t("chartf.x"), t("chartf.y"), t("chartf.note_text"), t("chartf.color")],
        (n, _i, set) => [
          this.text(n.x, (v) => set({ x: xFrom(v, s) }), t("chartf.x")),
          this.text(n.y, (v) => set({ y: yFrom(v) ?? 0 }), t("chartf.y")),
          this.text(n.text, (v) => set({ text: v }), t("chartf.note_text")),
          this.colorSelect(n.color as ChartColor | undefined, (c) => set({ color: c })),
        ],
        (rows) => this.set({ notes: rows as ChartSpec["notes"] }), () => ({ x: "", y: 0, text: "" }), t("chartf.add_note"))}
    </div></details>`;
  }

  private advanced() {
    const s = this.spec, y = s.y ?? {}, x = s.x ?? {};
    const ticks = (y.ticks ?? []).map((tk) => (typeof tk === "number" ? { value: tk, label: String(tk) } : tk));
    return html`<details class="sec">
      <summary><span class="chev">${icon("ChevronRight", 14)}</span>${icon("SlidersHorizontal", 15)} ${t("chartf.advanced")}</summary>
      <div class="body">
        <h4 style="margin:0">${t("chartf.ticks")}</h4>
        <p class="muted">${t("chartf.ticks_hint")}</p>
        ${this.rows(ticks as (Row & { value: number; label: string })[], [t("chartf.tick_value"), t("chartf.label")],
          (tk, _i, set) => [this.text(tk.value, (v) => set({ value: numFrom(v) ?? 0 }), t("chartf.tick_value")), this.text(tk.label, (v) => set({ label: v }), t("chartf.label"))],
          (rows) => this.set({ y: { ...y, ticks: rows as { value: number; label: string }[] } }), () => ({ value: 0, label: "" }), t("chartf.add_tick"))}
        <h4 style="margin:6px 0 0">${t("chartf.segments")}</h4>
        <p class="muted">${t("chartf.segments_hint")}</p>
        ${this.rows((x.segments ?? []) as (Row & ChartSegment)[], [t("chartf.from"), t("chartf.to"), t("chartf.weight"), t("chartf.label")],
          (sg, _i, set) => [
            this.text(sg.from, (v) => set({ from: xFrom(v, s) }), t("chartf.from")),
            this.text(sg.to, (v) => set({ to: xFrom(v, s) }), t("chartf.to")),
            this.text(sg.weight, (v) => set({ weight: numFrom(v) }), t("chartf.weight"), "1"),
            this.text(sg.label, (v) => set({ label: v || undefined }), t("chartf.label")),
          ],
          (rows) => this.set({ x: { ...x, segments: rows as NonNullable<ChartSpec["x"]>["segments"] } }), () => ({ from: "", to: "" }), t("chartf.add_segment"))}
        <label class="check"><input type="checkbox" .checked=${s.list !== false} @change=${(e: Event) => this.set({ list: (e.target as HTMLInputElement).checked ? undefined : false })}>${t("chartf.list")}</label>
      </div>
    </details>`;
  }

  render() {
    return html`${this.general()}${this.seriesSection()}${this.axes()}${this.bands()}${this.notes()}${this.advanced()}`;
  }
}
customElements.get("phx-chart-form") || customElements.define("phx-chart-form", PhxChartForm);
