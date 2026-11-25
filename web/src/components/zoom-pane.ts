// <phx-zoom-pane>: shows its content (a diagram, a chart) zoomable. The
// content keeps its layout at 100 % (the width of the pane) and is scaled
// with a transform; the pane scrolls. Ctrl/⌘ + wheel zooms at the pointer,
// dragging pans when zoomed in, the toolbar has −, 100 % and +.
import { LitElement, css, html } from "lit";
import { t } from "../i18n";
import { icon } from "../icon";
import { base } from "./shared";

import { ZOOM_MAX as MAX, ZOOM_MIN as MIN, zoomStep } from "../zoom";

const clamp = (z: number) => Math.min(MAX, Math.max(MIN, z));

export class PhxZoomPane extends LitElement {
  static properties = { z: { state: true }, w: { state: true }, h: { state: true } };
  static styles = [base, css`
    :host { display: grid; gap: 6px; }
    .view { overflow: auto; max-height: var(--zoom-max, 72vh); height: var(--zoom-height, auto); overscroll-behavior: contain; scrollbar-gutter: stable; }
    .view.pan { cursor: grab; }
    .view.panning { cursor: grabbing; user-select: none; }
    .sizer { position: relative; }
    .content { transform-origin: 0 0; position: absolute; inset: 0 auto auto 0; }
    .bar { justify-self: var(--zoom-bar-align, end); display: flex; align-items: center; background: var(--surface); border: 1px solid var(--line); border-radius: 8px; }
    .bar button { border: 0; background: transparent; color: var(--muted); padding: 4px 7px; min-height: 0; font: 12px var(--font-mono); display: inline-flex; align-items: center; }
    .bar .pct { min-width: 4.2em; justify-content: center; }
    @media (hover: hover) and (pointer: fine) { .bar button:hover { color: var(--fg); } }
  `];
  z = 1;
  w = 0;
  h = 0;
  private ro?: ResizeObserver;
  private drag?: { x: number; y: number; l: number; t: number; id: number };

  private get view() { return this.renderRoot.querySelector<HTMLElement>(".view"); }
  private get content() { return this.renderRoot.querySelector<HTMLElement>(".content"); }

  firstUpdated() {
    this.ro = new ResizeObserver(() => this.measure());
    this.ro.observe(this.view!);
    this.ro.observe(this.content!);
    this.measure();
  }

  disconnectedCallback() {
    super.disconnectedCallback();
    this.ro?.disconnect();
  }

  private measure() {
    const v = this.view, c = this.content;
    if (!v || !c) return;
    // The content is laid out at the inner width of the pane (as without zoom).
    const w = v.clientWidth;
    if (w !== this.w) this.w = w;
    const h = c.offsetHeight;
    if (h !== this.h) this.h = h;
  }

  /** Zooms to z, keeping the point (px, py) of the visible area in place. */
  zoomTo(z: number, px?: number, py?: number) {
    const v = this.view;
    const nz = clamp(z);
    if (!v || nz === this.z) return;
    const ax = px ?? v.clientWidth / 2, ay = py ?? v.clientHeight / 2;
    const cx = (v.scrollLeft + ax) / this.z, cy = (v.scrollTop + ay) / this.z;
    this.z = nz;
    void this.updateComplete.then(() => {
      v.scrollLeft = cx * nz - ax;
      v.scrollTop = cy * nz - ay;
    });
  }

  private onWheel = (e: WheelEvent) => {
    if (!e.ctrlKey && !e.metaKey) return;
    e.preventDefault();
    const r = this.view!.getBoundingClientRect();
    this.zoomTo(this.z * Math.exp(-e.deltaY * 0.0015), e.clientX - r.left, e.clientY - r.top);
  };

  private onKey = (e: KeyboardEvent) => {
    if (!(e.ctrlKey || e.metaKey) || e.altKey) return;
    if (e.key === "+" || e.key === "=") { e.preventDefault(); this.zoomTo(zoomStep(this.z, 1)); }
    else if (e.key === "-") { e.preventDefault(); this.zoomTo(zoomStep(this.z, -1)); }
    else if (e.key === "0") { e.preventDefault(); this.zoomTo(1); }
  };

  private onDown = (e: PointerEvent) => {
    const v = this.view!;
    const scrolls = v.scrollWidth > v.clientWidth || v.scrollHeight > v.clientHeight;
    if (e.button !== 0 || !scrolls || (e.target as HTMLElement).closest?.(".bar")) return;
    this.drag = { x: e.clientX, y: e.clientY, l: v.scrollLeft, t: v.scrollTop, id: e.pointerId };
    v.setPointerCapture(e.pointerId);
    v.classList.add("panning");
  };

  private onMove = (e: PointerEvent) => {
    const d = this.drag;
    if (!d || d.id !== e.pointerId) return;
    const v = this.view!;
    v.scrollLeft = d.l - (e.clientX - d.x);
    v.scrollTop = d.t - (e.clientY - d.y);
  };

  private onUp = (e: PointerEvent) => {
    if (this.drag?.id !== e.pointerId) return;
    this.drag = undefined;
    this.view?.classList.remove("panning");
  };

  render() {
    const z = this.z;
    const pan = z > 1;
    return html`<div class="bar" role="group" aria-label=${t("zoom.label")}>
        <button aria-label=${t("zoom.out")} title=${t("zoom.out")} ?disabled=${z <= MIN} @click=${() => this.zoomTo(zoomStep(z, -1))}>${icon("ZoomOut", 14)}</button>
        <button class="pct" aria-label=${t("zoom.reset")} title=${t("zoom.reset")} @click=${() => this.zoomTo(1)}>${Math.round(z * 100)} %</button>
        <button aria-label=${t("zoom.in")} title=${t("zoom.in")} ?disabled=${z >= MAX} @click=${() => this.zoomTo(zoomStep(z, 1))}>${icon("ZoomIn", 14)}</button>
      </div>
      <div class="view ${pan ? "pan" : ""}" tabindex="0" aria-label=${t("zoom.area")} @wheel=${this.onWheel} @keydown=${this.onKey}
        @pointerdown=${this.onDown} @pointermove=${this.onMove} @pointerup=${this.onUp} @pointercancel=${this.onUp}>
        <div class="sizer" style="width:${this.w ? `${this.w * z}px` : "100%"};height:${this.h * z}px">
          <div class="content" style="width:${this.w ? `${this.w}px` : "100%"};transform:scale(${z})"><slot></slot></div>
        </div>
      </div>`;
  }
}
customElements.get("phx-zoom-pane") || customElements.define("phx-zoom-pane", PhxZoomPane);
