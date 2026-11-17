import { LitElement, css, html } from "lit";
import { t } from "../i18n";
import { icon } from "../icon";
import { base } from "./shared";

type Tool = "hide" | "mark" | "draw";
type Op = { tool: "hide" | "mark"; x: number; y: number; w: number; h: number } | { tool: "draw"; pts: number[] };

/** Largest side of an edited picture; bigger screenshots are scaled down. */
const MAX_SIDE = 2560;

/** Paints over a picture before it is sent: black boxes cover private
 * parts (burned into the result), red boxes and lines mark what it is about.
 * Fires "done" with the new PNG file or "cancel". */
export class PhxShotEditor extends LitElement {
  static properties = { file: { attribute: false }, tool: { state: true }, ops: { state: true } };
  static styles = [base, css`
    :host { position: fixed; inset: 0; z-index: 50; display: grid; grid-template-rows: auto 1fr auto; background: var(--bg); }
    header, footer { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; padding: 10px 12px; border-bottom: 1px solid var(--line); background: var(--surface); }
    footer { border-bottom: 0; border-top: 1px solid var(--line); justify-content: flex-end; }
    h2 { margin: 0; font-size: 16px; flex: 1; }
    .hint { margin: 0; color: var(--muted); font-size: 12.5px; flex-basis: 100%; }
    .tools button[aria-pressed=true] { background: var(--accent); color: var(--on-color); border-color: var(--accent); }
    .stage { overflow: auto; display: grid; place-items: center; padding: 12px; min-height: 0; }
    canvas { max-width: 100%; max-height: 100%; touch-action: none; cursor: crosshair; box-shadow: 0 2px 12px rgb(0 0 0 / 0.18); border-radius: 4px; }
  `];

  file?: Blob;
  tool: Tool = "hide";
  ops: Op[] = [];
  private img?: HTMLImageElement | ImageBitmap;
  private w = 0;
  private h = 0;
  private live?: Op;

  async firstUpdated() {
    if (!this.file) return;
    const bmp = await createImageBitmap(this.file);
    const k = Math.min(1, MAX_SIDE / Math.max(bmp.width, bmp.height));
    this.w = Math.round(bmp.width * k);
    this.h = Math.round(bmp.height * k);
    this.img = bmp;
    const c = this.canvas;
    c.width = this.w;
    c.height = this.h;
    this.paint();
  }

  private get canvas() {
    return this.renderRoot.querySelector("canvas")!;
  }

  private paint(target = this.canvas) {
    const g = target.getContext("2d")!;
    g.clearRect(0, 0, this.w, this.h);
    if (this.img) g.drawImage(this.img, 0, 0, this.w, this.h);
    const lw = Math.max(3, Math.round(Math.max(this.w, this.h) / 300));
    for (const op of this.live ? [...this.ops, this.live] : this.ops) {
      if (op.tool === "hide") {
        g.fillStyle = "#000";
        g.fillRect(op.x, op.y, op.w, op.h);
      } else if (op.tool === "mark") {
        g.strokeStyle = "#e5262a";
        g.lineWidth = lw;
        g.strokeRect(op.x, op.y, op.w, op.h);
      } else if (op.tool === "draw" && op.pts.length >= 4) {
        g.strokeStyle = "#e5262a";
        g.lineWidth = lw;
        g.lineCap = g.lineJoin = "round";
        g.beginPath();
        g.moveTo(op.pts[0], op.pts[1]);
        for (let i = 2; i < op.pts.length; i += 2) g.lineTo(op.pts[i], op.pts[i + 1]);
        g.stroke();
      }
    }
  }

  private at(e: PointerEvent) {
    const r = this.canvas.getBoundingClientRect();
    return [((e.clientX - r.left) / r.width) * this.w, ((e.clientY - r.top) / r.height) * this.h];
  }

  private down = (e: PointerEvent) => {
    if (!this.img) return;
    this.canvas.setPointerCapture(e.pointerId);
    const [x, y] = this.at(e);
    this.live = this.tool === "draw" ? { tool: "draw", pts: [x, y] } : { tool: this.tool, x, y, w: 0, h: 0 };
    this.start = [x, y];
  };
  private start = [0, 0];

  private move = (e: PointerEvent) => {
    const op = this.live;
    if (!op) return;
    const [x, y] = this.at(e);
    if (op.tool === "draw") op.pts.push(x, y);
    else {
      const [sx, sy] = this.start;
      Object.assign(op, { x: Math.min(sx, x), y: Math.min(sy, y), w: Math.abs(x - sx), h: Math.abs(y - sy) });
    }
    this.paint();
  };

  private up = () => {
    const op = this.live;
    this.live = undefined;
    if (op && (op.tool === "draw" ? op.pts.length >= 4 : op.w > 2 && op.h > 2)) this.ops = [...this.ops, op];
    this.paint();
  };

  private undo() {
    this.ops = this.ops.slice(0, -1);
    this.paint();
  }

  private done() {
    // Render into a fresh canvas so nothing but the flattened picture leaves.
    const out = document.createElement("canvas");
    out.width = this.w;
    out.height = this.h;
    this.paint(out);
    out.toBlob((b) => {
      if (!b) return;
      const name = this.file instanceof File && this.file.name ? this.file.name.replace(/\.[a-z0-9]+$/i, "") + ".png" : "screenshot.png";
      this.dispatchEvent(new CustomEvent("done", { detail: new File([b], name, { type: "image/png" }) }));
    }, "image/png");
  }

  render() {
    const tools: [Tool, string, string][] = [["hide", "EyeOff", t("fbw.ed_hide")], ["mark", "Square", t("fbw.ed_mark")], ["draw", "Pencil", t("fbw.ed_draw")]];
    return html`<header>
        <h2>${t("fbw.ed_title")}</h2>
        <div class="tools" role="group">${tools.map(([id, ic, label]) => html`<button type="button" class="ghost" aria-pressed=${this.tool === id} @click=${() => (this.tool = id)}>${icon(ic, 14)} ${label}</button>`)}</div>
        <button type="button" class="ghost" ?disabled=${!this.ops.length} @click=${this.undo}>${icon("Undo2", 14)} ${t("fbw.ed_undo")}</button>
        <p class="hint">${t("fbw.ed_hint")}</p>
      </header>
      <div class="stage"><canvas role="img" aria-label=${t("fbw.ed_canvas")} @pointerdown=${this.down} @pointermove=${this.move} @pointerup=${this.up} @pointercancel=${this.up}></canvas></div>
      <footer>
        <button type="button" class="ghost" @click=${() => this.dispatchEvent(new CustomEvent("cancel"))}>${t("fbw.ed_cancel")}</button>
        <button type="button" @click=${this.done}>${icon("Check", 14)} ${t("fbw.ed_done")}</button>
      </footer>`;
  }
}
customElements.get("phx-shot-editor") || customElements.define("phx-shot-editor", PhxShotEditor);
