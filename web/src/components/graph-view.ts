import "./duplicates";
import { LitElement, css, html, svg } from "lit";
import { ApiError, api, type Edge, type Entity } from "../api";
import { t } from "../i18n";
import { icon } from "../icon";
import { bounds, layout, type Pos } from "../graph-layout";
import { findKind, groupColor, kindName, loadModel, relLabel, type Model } from "../model";
import { navigate } from "../state";
import { base } from "./shared";

type Graph = { entities: Entity[]; edges: Edge[] };

/** The whole knowledge graph of a project: filter by kind, search, pan and zoom. */
export class PhxGraphView extends LitElement {
  static properties = {
    project: {}, g: { state: true }, model: { state: true }, off: { state: true }, query: { state: true },
    sel: { state: true }, view: { state: true }, error: { state: true },
  };
  static styles = [base, css`
    :host { display: block; }
    .head { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; margin-bottom: 12px; }
    .head h1 { margin-right: auto; }
    .bar { display: flex; gap: 8px; flex-wrap: wrap; align-items: center; margin-bottom: 10px; }
    .bar input { max-width: 260px; }
    .kf { all: unset; display: inline-flex; align-items: center; gap: 5px; font-size: 12px; padding: 3px 9px; border-radius: 999px; cursor: pointer;
      border: 1px solid var(--line); color: var(--fg); background: var(--surface); transition: opacity var(--dur-fast) ease; }
    .kf[aria-pressed="false"] { opacity: .45; }
    .kf:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
    .kf .dot { width: 8px; height: 8px; border-radius: 50%; background: var(--c); }
    .stage { position: relative; border: 1px solid var(--line); border-radius: 12px; background: var(--surface); overflow: hidden; }
    svg.g { display: block; width: 100%; height: min(70vh, 720px); touch-action: none; cursor: grab; user-select: none; }
    svg.g.panning { cursor: grabbing; }
    .nd { cursor: pointer; transition: opacity var(--dur-fast) ease; }
    .nd circle { fill: var(--surface); stroke: var(--c); stroke-width: 2; }
    .nd.sel circle { fill: color-mix(in srgb, var(--c) 18%, var(--surface)); stroke-width: 3; }
    .nd.hit circle { stroke: var(--accent); stroke-width: 3; }
    .nd .ico { color: var(--c); }
    .nd text { font-size: 11px; fill: var(--fg); text-anchor: middle; paint-order: stroke; stroke: var(--surface); stroke-width: 3px; pointer-events: none; }
    .nd:focus { outline: none; }
    .nd:focus-visible circle { stroke: var(--accent); stroke-width: 3.5; }
    .dim { opacity: .2; }
    line.ed { stroke: var(--line); stroke-width: 1.2; transition: opacity var(--dur-fast) ease; }
    line.ed.on { stroke: var(--muted); stroke-width: 1.8; }
    text.el { font-size: 10px; fill: var(--muted); text-anchor: middle; paint-order: stroke; stroke: var(--surface); stroke-width: 3px; pointer-events: none; }
    .zoom { position: absolute; right: 10px; top: 10px; display: flex; flex-direction: column; gap: 4px; }
    .zoom button { padding: 5px 8px; }
    .card { position: absolute; left: 10px; bottom: 10px; max-width: min(320px, calc(100% - 20px)); padding: 12px 14px; box-shadow: 0 6px 24px rgb(0 0 0 / .12);
      transition: opacity var(--dur) var(--ease-out), transform var(--dur) var(--ease-out);
      @starting-style { opacity: 0; transform: translateY(6px); } }
    .card h3 { font-size: 16px; margin-bottom: 4px; }
    .empty { padding: 40px 20px; text-align: center; }
    @media (prefers-reduced-motion: reduce) { .card { transition: opacity var(--dur) ease; @starting-style { opacity: 0; transform: none; } } }
  `];
  project = "";
  g?: Graph;
  model?: Model;
  /** Kinds switched off in the filter. */
  off = new Set<string>();
  query = "";
  sel = "";
  /** Pan and zoom: translate in layout units, then scale. */
  view = { k: 1, x: 0, y: 0 };
  error = "";
  private pos = new Map<string, Pos>();
  private box = { x: 0, y: 0, w: 1000, h: 700 };
  private loaded = "";
  private pan?: { id: number; x: number; y: number; vx: number; vy: number };

  async willUpdate() {
    if (this.loaded === this.project) return;
    this.loaded = this.project;
    try {
      const [g, m] = await Promise.all([api<Graph>("GET", `/projects/${this.project}/graph`), loadModel(this.project)]);
      g.entities ??= [];
      g.edges ??= [];
      this.pos = layout(g.entities.map((e) => e.id), g.edges.map((e) => [e.from, e.to]));
      const b = bounds(this.pos.values());
      // A few entries should not fill the screen: keep a minimum view size.
      const w = Math.max(b.w, 900), h = Math.max(b.h, 560);
      this.box = { x: b.x - (w - b.w) / 2, y: b.y - (h - b.h) / 2, w, h };
      this.model = m;
      this.g = g;
    } catch (e) {
      this.error = e instanceof ApiError ? e.text : t("error.unknown");
    }
  }

  private toggle(kind: string) {
    const h = new Set(this.off);
    if (h.has(kind)) h.delete(kind);
    else h.add(kind);
    this.off = h;
  }

  private zoomBy(f: number, cx = this.box.x + this.box.w / 2, cy = this.box.y + this.box.h / 2) {
    const k = Math.min(6, Math.max(0.2, this.view.k * f));
    const r = k / this.view.k;
    this.view = { k, x: cx - (cx - this.view.x) * r, y: cy - (cy - this.view.y) * r };
  }

  /** Converts a pointer position into layout coordinates (before pan/zoom). */
  private point(e: { clientX: number; clientY: number }) {
    const el = this.renderRoot.querySelector("svg.g") as SVGSVGElement;
    const m = el.getScreenCTM();
    if (!m) return { x: 0, y: 0 };
    const p = new DOMPoint(e.clientX, e.clientY).matrixTransform(m.inverse());
    return { x: p.x, y: p.y };
  }

  private onWheel = (e: WheelEvent) => {
    e.preventDefault();
    const p = this.point(e);
    this.zoomBy(Math.exp(-e.deltaY * 0.0015), p.x, p.y);
  };

  private onDown = (e: PointerEvent) => {
    if ((e.target as Element).closest(".nd")) return;
    (e.currentTarget as Element).setPointerCapture(e.pointerId);
    const p = this.point(e);
    this.pan = { id: e.pointerId, x: p.x, y: p.y, vx: this.view.x, vy: this.view.y };
    this.requestUpdate();
  };

  private onMove = (e: PointerEvent) => {
    if (!this.pan || e.pointerId !== this.pan.id) return;
    const p = this.point(e);
    this.view = { ...this.view, x: this.pan.vx + (p.x - this.pan.x), y: this.pan.vy + (p.y - this.pan.y) };
    // point() is relative to the viewBox, which does not move; panning shifts only the inner group.
    this.pan = { ...this.pan, x: p.x, y: p.y, vx: this.view.x, vy: this.view.y };
  };

  private onUp = (e: PointerEvent) => {
    if (this.pan?.id !== e.pointerId) return;
    const moved = Math.abs(this.view.x - this.pan.vx) + Math.abs(this.view.y - this.pan.vy);
    this.pan = undefined;
    if (moved < 1 && !(e.target as Element).closest(".nd")) this.sel = "";
    this.requestUpdate();
  };

  private open(e: Entity) {
    navigate(`/p/${this.project}/e/${e.slug}`);
  }

  private onSearchKey(e: KeyboardEvent, matches: Entity[]) {
    if (e.key === "Enter" && matches[0]) {
      this.sel = matches[0].id;
      const p = this.pos.get(this.sel)!;
      const cx = this.box.x + this.box.w / 2, cy = this.box.y + this.box.h / 2;
      this.view = { k: Math.max(this.view.k, 1.5), x: 0, y: 0 };
      this.view = { ...this.view, x: cx - p.x * this.view.k, y: cy - p.y * this.view.k };
    }
  }

  render() {
    if (this.error) return html`<p class="error">${this.error}</p>`;
    if (!this.g || !this.model) return html`<p class="muted">${t("app.loading")}</p>`;
    const { entities, edges } = this.g;
    const head = html`<div class="head"><h1>${t("graph.title")}</h1>
      <a class="btn ghost" href="/p/${this.project}/model">${icon("Shapes", 14)} ${t("nav.model")}</a></div>`;
    if (!entities.length) {
      return html`<div class="enter">${head}<div class="stage empty"><p class="muted">${t("graph.empty")}</p>
        <a class="btn" href="/p/${this.project}/new-entity">${icon("Plus", 14)} ${t("entity.new")}</a></div></div>`;
    }
    const kinds = [...new Set(entities.map((e) => e.kind))];
    const shown = entities.filter((e) => !this.off.has(e.kind));
    const shownIds = new Set(shown.map((e) => e.id));
    const vis = edges.filter((x) => shownIds.has(x.from) && shownIds.has(x.to));
    const q = this.query.trim().toLowerCase();
    const matches = q ? shown.filter((e) => e.name.toLowerCase().includes(q) || e.slug.includes(q)) : [];
    const matchIds = new Set(matches.map((e) => e.id));
    const near = new Set<string>(this.sel ? [this.sel] : []);
    for (const x of vis) if (x.from === this.sel || x.to === this.sel) { near.add(x.from); near.add(x.to); }
    const focus = this.sel ? near : q ? matchIds : undefined;
    const labels = shown.length <= 80 || this.view.k >= 1.4;
    const selEntity = entities.find((e) => e.id === this.sel);
    const P = (id: string) => this.pos.get(id)!;
    const { k, x, y } = this.view;
    return html`<div class="enter">
      ${head}<phx-duplicates .project=${this.project} @changed=${() => { this.loaded = ""; this.requestUpdate(); }}></phx-duplicates>
      <div class="bar">
        <input type="search" placeholder=${t("graph.search")} aria-label=${t("graph.search")} .value=${this.query}
          @input=${(e: Event) => (this.query = (e.target as HTMLInputElement).value)} @keydown=${(e: KeyboardEvent) => this.onSearchKey(e, matches)}>
        ${kinds.map((id) => {
          const kd = findKind(this.model, id);
          return html`<button class="kf" aria-pressed=${!this.off.has(id)} style="--c:${groupColor[kd?.group ?? "custom"]}" @click=${() => this.toggle(id)}>
            <span class="dot"></span>${kindName(kd, id)} <span class="muted">${entities.filter((e) => e.kind === id).length}</span></button>`;
        })}
      </div>
      <div class="stage">
        <svg class=${this.pan ? "g panning" : "g"} viewBox="${this.box.x} ${this.box.y} ${this.box.w} ${this.box.h}" preserveAspectRatio="xMidYMid meet"
          role="group" aria-label=${t("graph.label", { count: shown.length })}
          @wheel=${this.onWheel} @pointerdown=${this.onDown} @pointermove=${this.onMove} @pointerup=${this.onUp} @pointercancel=${this.onUp}>
          <rect x=${this.box.x} y=${this.box.y} width=${this.box.w} height=${this.box.h} fill="transparent"></rect>
          <g transform="translate(${x} ${y}) scale(${k})">
            ${vis.map((ed) => {
              const a = P(ed.from), b = P(ed.to);
              const on = !!this.sel && (ed.from === this.sel || ed.to === this.sel);
              const dim = focus && !(focus.has(ed.from) && focus.has(ed.to));
              return svg`<line class=${`ed${on ? " on" : ""}${dim ? " dim" : ""}`} x1=${a.x} y1=${a.y} x2=${b.x} y2=${b.y}></line>
                ${on ? svg`<text class="el" x=${(a.x + b.x) / 2} y=${(a.y + b.y) / 2 + 3}>${relLabel(ed.rel)}</text>` : ""}`;
            })}
            ${shown.map((e) => {
              const p = P(e.id);
              const kd = findKind(this.model, e.kind);
              const cls = ["nd", e.id === this.sel ? "sel" : "", matchIds.has(e.id) ? "hit" : "", focus && !focus.has(e.id) ? "dim" : ""].join(" ");
              const label = labels || (focus?.has(e.id) ?? false);
              return svg`<g class=${cls} transform="translate(${p.x} ${p.y})" style="--c:${groupColor[kd?.group ?? "custom"]}"
                tabindex=${shown.length <= 200 ? 0 : -1} role="button" aria-label="${e.name}, ${kindName(kd, e.kind)}"
                @click=${() => (this.sel = e.id)} @dblclick=${() => this.open(e)}
                @keydown=${(ev: KeyboardEvent) => { if (ev.key === "Enter") this.sel === e.id ? this.open(e) : (this.sel = e.id); }}>
                <circle r="15"></circle><g transform="translate(-8 -8)">${icon(kd?.icon ?? "CircleAlert", 16)}</g>
                ${label ? svg`<text y="29">${e.name.length > 24 ? `${e.name.slice(0, 23)}…` : e.name}</text>` : ""}</g>`;
            })}
          </g>
        </svg>
        <div class="zoom">
          <button class="ghost" aria-label=${t("graph.zoom_in")} @click=${() => this.zoomBy(1.3)}>${icon("Plus", 14)}</button>
          <button class="ghost" aria-label=${t("graph.zoom_out")} @click=${() => this.zoomBy(1 / 1.3)}>−</button>
          <button class="ghost" aria-label=${t("graph.fit")} title=${t("graph.fit")} @click=${() => (this.view = { k: 1, x: 0, y: 0 })}>${icon("Maximize2", 14)}</button>
        </div>
        ${selEntity ? html`<div class="card">
          <h3>${selEntity.name}</h3>
          <p class="muted" style="margin:0 0 8px">${kindName(findKind(this.model, selEntity.kind), selEntity.kind)} · ${t("graph.relations", { count: near.size - 1 })}</p>
          <a class="btn ghost" href="/p/${this.project}/e/${selEntity.slug}">${icon("ArrowRight", 14)} ${t("graph.open")}</a>
        </div>` : ""}
      </div>
      <p class="muted" style="font-size:12px">${t("graph.hint")}</p>
    </div>`;
  }
}
customElements.define("phx-graph-view", PhxGraphView);
