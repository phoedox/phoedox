// Floor plan document (ADR-0034): rooms as polygons and devices as points in
// millimetres. Viewing links to the entries; editors draw rooms, place
// devices, add a background image and calibrate it.

import { LitElement, css, html, nothing, svg, unsafeCSS } from "lit";
import { ApiError, api, type Doc, type Entity } from "../api";

type DocResult = { applied: boolean; document?: Doc };
import { fileURL, uploadFile } from "../files";
import {
  ROOM_COLORS, areaM2, calibrate, distance, labelPoint, metres, newId, normalizePlan, orthogonal, roomAt, snap,
  type FloorDevice, type FloorRoom, type Floorplan, type Pt,
} from "../floorplan";
import { locale, t } from "../i18n";
import { icon } from "../icon";
import { findKind, kindName, loadModel, type Model } from "../model";
import { navigate } from "../state";
import { base } from "./shared";
import { toast } from "./toasts";

type Tool = "select" | "room" | "device" | "calibrate";
type Box = { x: number; y: number; w: number; h: number };
/** What the side panel asks for before something is added. */
type Pending = { kind: "room"; points: Pt[] } | { kind: "device"; at: Pt } | { kind: "calibrate"; a: Pt; b: Pt };
type Drag = { kind: "pan"; from: Pt; box: Box } | { kind: "device"; id: string; moved: boolean } | { kind: "vertex"; room: string; index: number };

const GRID = 100; // snap: 10 cm
const NEW = "__new__";

export class PhxFloorplan extends LitElement {
  static properties = {
    project: {}, doc: { attribute: false }, canEdit: { type: Boolean },
    plan: { state: true }, editing: { state: true }, tool: { state: true }, selected: { state: true }, draft: { state: true },
    cursor: { state: true }, pending: { state: true }, entities: { state: true }, model: { state: true }, box: { state: true },
    dirty: { state: true }, busy: { state: true }, error: { state: true },
  };
  static styles = [base, css`
    :host { display: block; }
    .wrap { display: grid; grid-template-columns: minmax(0, 1fr); gap: 12px; }
    .wrap.edit { grid-template-columns: minmax(0, 1fr) 280px; }
    .stage { position: relative; border: 1px solid var(--line); border-radius: 12px; background: var(--surface); overflow: hidden; }
    svg.plan { display: block; width: 100%; height: min(70vh, 640px); touch-action: none; user-select: none; }
    .edit svg.plan { cursor: crosshair; } .edit.t-select svg.plan { cursor: default; }
    .grid { stroke: var(--line); }
    .room { stroke: var(--fg); stroke-linejoin: round; vector-effect: non-scaling-stroke; stroke-width: 1.5; }
    ${unsafeCSS(ROOM_COLORS.map((c) => `.room.c-${c} { fill: var(--tb-${c}); }`).join("\n"))}
    .room.link { cursor: pointer; }
    .room.sel { stroke: var(--accent); stroke-width: 3; }
    @media (hover: hover) and (pointer: fine) { .room.link:hover { stroke: var(--accent); } .dev:hover circle { stroke: var(--fg); } }
    text { fill: var(--fg); font-family: var(--font-body); paint-order: stroke; stroke: var(--surface); stroke-linejoin: round; pointer-events: none; }
    text.area { fill: var(--muted); }
    .dev { cursor: pointer; color: var(--surface); }
    .dev circle { fill: var(--accent); stroke: var(--surface); vector-effect: non-scaling-stroke; stroke-width: 2; }
    .dev.sel circle { stroke: var(--fg); stroke-width: 3; }
    .draft { fill: color-mix(in srgb, var(--accent) 12%, transparent); stroke: var(--accent); stroke-dasharray: 6 4; vector-effect: non-scaling-stroke; stroke-width: 2; }
    .vertex { fill: var(--surface); stroke: var(--accent); vector-effect: non-scaling-stroke; stroke-width: 2; cursor: move; }
    .measure { stroke: var(--wire); stroke-width: 2; vector-effect: non-scaling-stroke; }
    .zoom { position: absolute; right: 8px; bottom: 8px; display: flex; gap: 4px; }
    .zoom button { padding: 4px 7px; }
    .bar { display: flex; flex-wrap: wrap; gap: 6px; align-items: center; margin-bottom: 8px; }
    .bar .tools { display: inline-flex; border: 1px solid var(--line); border-radius: 8px; overflow: hidden; }
    .bar .tools button { border: 0; border-radius: 0; padding: 6px 10px; background: var(--surface); color: var(--fg); }
    .bar .tools button[aria-pressed="true"] { background: var(--accent-soft); color: var(--accent); }
    .bar .spacer { flex: 1; }
    aside { display: grid; gap: 12px; align-content: start; font-size: 14px; }
    aside section { border: 1px solid var(--line); border-radius: 10px; padding: 10px 12px; display: grid; gap: 8px; background: var(--surface); }
    aside h3 { margin: 0; font-size: 14px; }
    .hint { font-size: 13px; color: var(--muted); margin: 0; }
    .swatches { display: flex; gap: 4px; flex-wrap: wrap; }
    .swatches button { width: 24px; height: 24px; padding: 0; border-radius: 6px; border: 1px solid var(--line); }
    .swatches button[aria-pressed="true"] { outline: 2px solid var(--accent); outline-offset: 1px; }
    ${unsafeCSS(ROOM_COLORS.map((c) => `.swatches .s-${c} { background: var(--tb-${c}); }`).join("\n"))}
    table { width: 100%; border-collapse: collapse; font-size: 14px; margin-top: 12px; }
    td, th { text-align: left; padding: 5px 8px 5px 0; border-bottom: 1px solid var(--line); vertical-align: top; }
    th { font-weight: 600; color: var(--muted); font-size: 12px; }
    td.num { white-space: nowrap; font-variant-numeric: tabular-nums; }
    .row { display: flex; gap: 6px; flex-wrap: wrap; }
    @media (max-width: 860px) { .wrap.edit { grid-template-columns: minmax(0, 1fr); } }
  `];

  project = "";
  doc?: Doc;
  canEdit = false;
  plan: Floorplan = normalizePlan(undefined);
  editing = false;
  tool: Tool = "select";
  selected = "";
  draft: Pt[] = [];
  cursor?: Pt;
  pending?: Pending;
  entities: Entity[] = [];
  model?: Model;
  box: Box = { x: 0, y: 0, w: 20000, h: 15000 };
  dirty = false;
  busy = false;
  error = "";
  private drag?: Drag;
  private loadedFor = "";

  connectedCallback() {
    super.connectedCallback();
    window.addEventListener("beforeunload", this.onUnload);
  }

  disconnectedCallback() {
    super.disconnectedCallback();
    window.removeEventListener("beforeunload", this.onUnload);
  }

  private onUnload = (e: BeforeUnloadEvent) => {
    if (this.dirty) e.preventDefault();
  };

  willUpdate() {
    const key = `${this.doc?.id}@${this.doc?.version}`;
    if (this.doc && key !== this.loadedFor && !this.dirty) {
      this.loadedFor = key;
      this.plan = normalizePlan(this.doc.content as Partial<Floorplan>);
      this.fit();
      void this.loadEntities();
    }
  }

  private async loadEntities() {
    const [ents, model] = await Promise.all([
      api<Entity[]>("GET", `/projects/${this.project}/entities`).catch(() => [] as Entity[]),
      loadModel(this.project).catch(() => undefined),
    ]);
    this.entities = ents ?? [];
    this.model = model;
  }

  private entity(slug?: string) {
    return slug ? this.entities.find((e) => e.slug === slug) : undefined;
  }

  // ---- coordinates -------------------------------------------------------

  private get svgEl() {
    return this.renderRoot.querySelector<SVGSVGElement>("svg.plan");
  }

  /** Pointer position in plan millimetres. */
  private toPlan(e: PointerEvent | WheelEvent): Pt {
    const el = this.svgEl!;
    const m = el.getScreenCTM()!.inverse();
    const p = new DOMPoint(e.clientX, e.clientY).matrixTransform(m);
    return [p.x, p.y];
  }

  /** Millimetres per screen pixel at the current zoom. */
  private get mmPerPx() {
    const el = this.svgEl;
    if (!el) return this.box.w / 800;
    const r = el.getBoundingClientRect();
    return Math.max(this.box.w / r.width, this.box.h / r.height);
  }

  private fit() {
    const pad = Math.max(this.plan.width, this.plan.height) * 0.04;
    this.box = { x: -pad, y: -pad, w: this.plan.width + 2 * pad, h: this.plan.height + 2 * pad };
  }

  private zoom(factor: number, at?: Pt) {
    const [cx, cy] = at ?? [this.box.x + this.box.w / 2, this.box.y + this.box.h / 2];
    const w = Math.min(Math.max(this.box.w * factor, 500), this.plan.width * 6);
    const f = w / this.box.w;
    this.box = { x: cx - (cx - this.box.x) * f, y: cy - (cy - this.box.y) * f, w, h: this.box.h * f };
  }

  private onWheel = (e: WheelEvent) => {
    // In view mode the page scrolls; zoom needs Ctrl/⌘ (or a pinch, which sends ctrlKey).
    if (!this.editing && !e.ctrlKey && !e.metaKey) return;
    e.preventDefault();
    this.zoom(Math.exp(e.deltaY * 0.0015), this.toPlan(e));
  };

  // ---- editing -----------------------------------------------------------

  private change(fn: (p: Floorplan) => void) {
    const next = structuredClone(this.plan);
    fn(next);
    this.plan = next;
    this.dirty = true;
  }

  private snapped(e: PointerEvent): Pt {
    const [x, y] = this.toPlan(e);
    let p: Pt = e.altKey ? [Math.round(x), Math.round(y)] : [snap(x, GRID), snap(y, GRID)];
    const last = this.draft[this.draft.length - 1];
    if (e.shiftKey && last) p = orthogonal(last, p);
    return p;
  }

  private onDown(e: PointerEvent) {
    if (e.button !== 0) return;
    const target = e.target as Element;
    const devEl = target.closest<SVGGElement>("[data-dev]");
    const roomEl = target.closest<SVGPolygonElement>("[data-room]");
    const vertexEl = target.closest<SVGCircleElement>("[data-vertex]");
    if (!this.editing) {
      const slug = devEl ? this.plan.devices.find((d) => d.id === devEl.dataset.dev)?.entity
        : roomEl ? this.plan.rooms.find((r) => r.id === roomEl.dataset.room)?.entity : undefined;
      if (slug) navigate(`/p/${this.project}/e/${slug}`);
      else this.startPan(e);
      return;
    }
    if (this.pending) return;
    if (this.tool === "room") {
      const p = this.snapped(e);
      const first = this.draft[0];
      if (first && this.draft.length >= 3 && distance(first, p) < 10 * this.mmPerPx) this.closeRoom();
      else this.draft = [...this.draft, p];
      return;
    }
    if (this.tool === "device") {
      this.pending = { kind: "device", at: this.snapped(e) };
      return;
    }
    if (this.tool === "calibrate") {
      const p = this.toPlan(e);
      if (this.draft.length === 0) this.draft = [p];
      else {
        this.pending = { kind: "calibrate", a: this.draft[0], b: p };
        this.draft = [];
      }
      return;
    }
    // select
    (e.currentTarget as Element).setPointerCapture(e.pointerId);
    if (vertexEl) {
      this.drag = { kind: "vertex", room: vertexEl.dataset.vertex!, index: Number(vertexEl.dataset.index) };
    } else if (devEl) {
      this.selected = devEl.dataset.dev!;
      this.drag = { kind: "device", id: this.selected, moved: false };
    } else if (roomEl) {
      this.selected = roomEl.dataset.room!;
    } else {
      this.selected = "";
      this.startPan(e);
    }
  }

  private startPan(e: PointerEvent) {
    (e.currentTarget as Element)?.setPointerCapture?.(e.pointerId);
    this.drag = { kind: "pan", from: [e.clientX, e.clientY], box: { ...this.box } };
  }

  private onMove(e: PointerEvent) {
    if (this.editing && (this.tool === "room" || this.tool === "calibrate")) this.cursor = this.tool === "room" ? this.snapped(e) : this.toPlan(e);
    const d = this.drag;
    if (!d) return;
    if (d.kind === "pan") {
      const k = this.mmPerPx;
      this.box = { ...d.box, x: d.box.x - (e.clientX - d.from[0]) * k, y: d.box.y - (e.clientY - d.from[1]) * k };
    } else if (d.kind === "device") {
      const [x, y] = this.snapped(e);
      d.moved = true;
      this.change((p) => Object.assign(p.devices.find((v) => v.id === d.id)!, { x, y }));
    } else {
      const pt = this.snapped(e);
      this.change((p) => { p.rooms.find((r) => r.id === d.room)!.points[d.index] = pt; });
    }
  }

  private onUp() {
    this.drag = undefined;
  }

  private onKey = (e: KeyboardEvent) => {
    if (!this.editing || (e.target as HTMLElement).closest("input, select, textarea")) return;
    if (e.key === "Escape") {
      if (this.draft.length) this.draft = [];
      else if (this.pending) this.pending = undefined;
      else this.selected = "";
    } else if (e.key === "Enter" && this.tool === "room" && this.draft.length >= 3) {
      this.closeRoom();
    } else if (e.key === "Backspace" && this.draft.length) {
      this.draft = this.draft.slice(0, -1);
    } else if ((e.key === "Delete" || e.key === "Backspace") && this.selected) {
      this.removeSelected();
    } else return;
    e.preventDefault();
  };

  private closeRoom() {
    this.pending = { kind: "room", points: this.draft };
    this.draft = [];
    this.cursor = undefined;
  }

  private setTool(tool: Tool) {
    this.tool = tool;
    this.draft = [];
    this.pending = undefined;
    this.cursor = undefined;
  }

  private removeSelected() {
    const id = this.selected;
    this.change((p) => {
      p.rooms = p.rooms.filter((r) => r.id !== id);
      p.devices = p.devices.filter((d) => d.id !== id);
    });
    this.selected = "";
  }

  /** Resolves the entry choice of a form: an existing slug or a new entry. */
  private async entryFor(choice: string, kind: string, name: string): Promise<string | undefined> {
    if (choice !== NEW) return choice || undefined;
    if (!name) return undefined;
    const r = await api<{ applied: boolean; entity?: Entity }>("POST", `/projects/${this.project}/entities`, { kind, name });
    if (!r.entity) return undefined;
    this.entities = [...this.entities, r.entity];
    return r.entity.slug;
  }

  private async submitPending(e: SubmitEvent) {
    e.preventDefault();
    const f = new FormData(e.target as HTMLFormElement);
    const s = (k: string) => String(f.get(k) ?? "").trim();
    const pend = this.pending!;
    this.busy = true;
    try {
      if (pend.kind === "room") {
        const entity = await this.entryFor(s("entity"), "room", s("name"));
        const room: FloorRoom = { id: newId("r"), name: s("name"), entity, color: s("color") || undefined, points: pend.points };
        this.change((p) => p.rooms.push(room));
        this.selected = room.id;
      } else if (pend.kind === "device") {
        const entity = await this.entryFor(s("entity"), s("kind") || "device", s("name"));
        if (!entity) return;
        const dev: FloorDevice = { id: newId("d"), entity, x: pend.at[0], y: pend.at[1] };
        this.change((p) => p.devices.push(dev));
      } else {
        const real = Number(s("metres").replace(",", ".")) * 1000;
        const measured = distance(pend.a, pend.b);
        this.change((p) => { p.imageWidth = calibrate(p.imageWidth || p.width, measured, real); });
        this.tool = "select";
      }
      this.pending = undefined;
    } catch (err) {
      toast(err instanceof ApiError ? err.text : t("error.unknown"), "error");
    } finally {
      this.busy = false;
    }
  }

  private async upload(e: Event) {
    const input = e.target as HTMLInputElement;
    const file = input.files?.[0];
    input.value = "";
    if (!file || !this.doc) return;
    this.busy = true;
    try {
      const info = await uploadFile(this.project, file, { document: this.doc.id });
      this.imageName = info.name;
      this.change((p) => { p.image = info.id; p.imageWidth = p.width; });
      this.tool = "calibrate";
      toast(t("plan.calibrate_hint"));
    } catch (err) {
      toast(err instanceof ApiError ? err.text : t("error.unknown"), "error");
    } finally {
      this.busy = false;
    }
  }
  private imageName = "plan";

  private async save() {
    if (!this.doc) return;
    this.busy = true;
    this.error = "";
    try {
      const res = await api<DocResult>("PATCH", `/projects/${this.project}/documents/${this.doc.slug}`, { floorplan: this.plan, expectVersion: this.doc.version });
      this.dirty = false;
      if (res.document) this.doc = res.document;
      toast(res.applied ? t("plan.saved") : t("suggestion.created"));
      this.editing = false;
      this.dispatchEvent(new CustomEvent("changed", { bubbles: true, composed: true }));
    } catch (err) {
      this.error = err instanceof ApiError ? err.text : t("error.unknown");
    } finally {
      this.busy = false;
    }
  }

  private cancel() {
    if (this.dirty && !confirm(t("plan.discard"))) return;
    this.dirty = false;
    this.editing = false;
    this.loadedFor = "";
    this.setTool("select");
    this.selected = "";
    this.requestUpdate();
  }

  // ---- rendering ---------------------------------------------------------

  private drawGrid() {
    const lines = [];
    const step = 1000;
    for (let x = 0; x <= this.plan.width; x += step) lines.push(svg`<line class="grid" x1=${x} y1="0" x2=${x} y2=${this.plan.height} vector-effect="non-scaling-stroke"/>`);
    for (let y = 0; y <= this.plan.height; y += step) lines.push(svg`<line class="grid" x1="0" y1=${y} x2=${this.plan.width} y2=${y} vector-effect="non-scaling-stroke"/>`);
    return lines;
  }

  private drawPlan() {
    const p = this.plan;
    // Markers and labels keep about the same size on screen at every zoom.
    const unit = this.box.w / 42;
    const loc = locale();
    const pts = (ps: Pt[]) => ps.map((q) => q.join(",")).join(" ");
    const selRoom = this.editing ? p.rooms.find((r) => r.id === this.selected) : undefined;
    return svg`
      ${p.image && p.imageWidth ? svg`<image href=${fileURL({ id: p.image, name: this.imageName })} x="0" y="0" width=${p.imageWidth} opacity=".55"/>` : nothing}
      ${this.editing || !p.image ? this.drawGrid() : nothing}
      ${p.rooms.map((r) => svg`<polygon data-room=${r.id} class="room c-${r.color || "gray"} ${r.entity && !this.editing ? "link" : ""} ${r.id === this.selected && this.editing ? "sel" : ""}"
        points=${pts(r.points)}><title>${r.name}</title></polygon>`)}
      ${p.rooms.map((r) => {
        const [x, y] = labelPoint(r.points);
        return svg`<text x=${x} y=${y} font-size=${unit * 0.75} text-anchor="middle" stroke-width=${unit * 0.15}>${r.name}</text>
          <text class="area" x=${x} y=${y + unit * 0.85} font-size=${unit * 0.55} text-anchor="middle" stroke-width=${unit * 0.12}>${areaM2(r.points).toLocaleString(loc === "de" ? "de-DE" : "en-GB")} m²</text>`;
      })}
      ${p.devices.map((d) => {
        const e = this.entity(d.entity);
        const k = findKind(this.model, e?.kind ?? "");
        const r = unit * 0.55;
        return svg`<g data-dev=${d.id} class="dev ${d.id === this.selected && this.editing ? "sel" : ""}" role="link" aria-label=${e?.name ?? d.entity}>
          <title>${e?.name ?? d.entity}${e ? ` · ${kindName(k, e.kind)}` : ""}</title>
          <circle cx=${d.x} cy=${d.y} r=${r}/>
          <g transform="translate(${d.x - r * 0.62} ${d.y - r * 0.62})">${icon(k?.icon ?? "Cpu", r * 1.24)}</g>
          <text x=${d.x} y=${d.y + r + unit * 0.6} font-size=${unit * 0.5} text-anchor="middle" stroke-width=${unit * 0.12}>${e?.name ?? d.entity}</text></g>`;
      })}
      ${selRoom ? selRoom.points.map((q, i) => svg`<circle class="vertex" data-vertex=${selRoom.id} data-index=${i} cx=${q[0]} cy=${q[1]} r=${unit * 0.22}/>`) : nothing}
      ${this.draft.length && this.tool === "room" ? svg`<polygon class="draft" points=${pts(this.cursor ? [...this.draft, this.cursor] : this.draft)}/>
        ${this.draft.map((q) => svg`<circle class="vertex" cx=${q[0]} cy=${q[1]} r=${unit * 0.18}/>`)}` : nothing}
      ${this.tool === "calibrate" && this.editing && this.draft.length && this.cursor ? svg`<line class="measure" x1=${this.draft[0][0]} y1=${this.draft[0][1]} x2=${this.cursor[0]} y2=${this.cursor[1]}/>` : nothing}
      ${this.pending?.kind === "calibrate" ? svg`<line class="measure" x1=${this.pending.a[0]} y1=${this.pending.a[1]} x2=${this.pending.b[0]} y2=${this.pending.b[1]}/>` : nothing}
      ${this.pending?.kind === "room" ? svg`<polygon class="draft" points=${pts(this.pending.points)}/>` : nothing}
      ${this.pending?.kind === "device" ? svg`<circle class="vertex" cx=${this.pending.at[0]} cy=${this.pending.at[1]} r=${unit * 0.3}/>` : nothing}`;
  }

  private entryOptions(filter: (e: Entity) => boolean, current?: string) {
    const list = this.entities.filter(filter).sort((a, b) => a.name.localeCompare(b.name));
    return html`${list.map((e) => html`<option value=${e.slug} ?selected=${e.slug === current}>${e.name}${e.kind !== "room" ? ` · ${kindName(findKind(this.model, e.kind), e.kind)}` : ""}</option>`)}`;
  }

  private colorPicker(current?: string, onPick?: (c: string) => void) {
    return html`<div class="swatches" role="radiogroup" aria-label=${t("plan.color")}>${ROOM_COLORS.map((c) => html`<button type="button" class="s-${c}" role="radio"
      aria-checked=${(current || "gray") === c ? "true" : "false"} aria-pressed=${(current || "gray") === c ? "true" : "false"} title=${t(`color.${c}` as any)}
      @click=${() => { onPick?.(c); this.requestUpdate(); }}></button>`)}</div>`;
  }

  private panel() {
    const p = this.plan;
    const pend = this.pending;
    if (pend?.kind === "room") {
      return html`<section><h3>${t("plan.new_room")}</h3>
        <form @submit=${this.submitPending} style="display:grid;gap:8px">
          <label class="field">${t("plan.room_name")}<input name="name" required autofocus></label>
          <label class="field">${t("plan.entry")}<select name="entity">
            <option value=${NEW}>${t("plan.new_entry")}</option><option value="">${t("plan.no_entry")}</option>${this.entryOptions((e) => e.kind === "room")}</select></label>
          ${this.colorPicker(this.pendingColor, (c) => (this.pendingColor = c))}<input type="hidden" name="color" .value=${this.pendingColor}>
          <p class="hint">${t("plan.area", { area: areaM2(pend.points) })}</p>
          <div class="row"><button ?disabled=${this.busy}>${t("plan.add")}</button><button type="button" class="ghost" @click=${() => (this.pending = undefined)}>${t("doc.cancel")}</button></div>
        </form></section>`;
    }
    if (pend?.kind === "device") {
      const room = roomAt(p, pend.at[0], pend.at[1]);
      return html`<section><h3>${t("plan.new_device")}</h3>
        <form @submit=${this.submitPending} style="display:grid;gap:8px">
          <label class="field">${t("plan.entry")}<select name="entity" autofocus @change=${(e: Event) => (this.newDevice = (e.target as HTMLSelectElement).value === NEW)}>
            ${this.entryOptions((e) => e.kind !== "room" && e.kind !== "floor")}<option value=${NEW}>${t("plan.new_entry")}</option></select></label>
          ${this.newDevice || !this.entities.some((e) => e.kind !== "room" && e.kind !== "floor") ? html`
            <label class="field">${t("plan.device_name")}<input name="name" required></label>
            <label class="field">${t("plan.kind")}<select name="kind">${(this.model?.kinds ?? []).filter((k) => !["room", "floor"].includes(k.id))
              .map((k) => html`<option value=${k.id} ?selected=${k.id === "device"}>${kindName(k)}</option>`)}</select></label>` : ""}
          <p class="hint">${room ? t("plan.in_room", { room: room.name }) : t("plan.no_room")}</p>
          <div class="row"><button ?disabled=${this.busy}>${t("plan.add")}</button><button type="button" class="ghost" @click=${() => (this.pending = undefined)}>${t("doc.cancel")}</button></div>
        </form></section>`;
    }
    if (pend?.kind === "calibrate") {
      return html`<section><h3>${t("plan.calibrate")}</h3>
        <form @submit=${this.submitPending} style="display:grid;gap:8px">
          <label class="field">${t("plan.real_distance")}<input name="metres" inputmode="decimal" required pattern="[0-9]+([.,][0-9]+)?" autofocus placeholder="4,25"></label>
          <div class="row"><button>${t("plan.apply")}</button><button type="button" class="ghost" @click=${() => (this.pending = undefined)}>${t("doc.cancel")}</button></div>
        </form></section>`;
    }
    const room = p.rooms.find((r) => r.id === this.selected);
    const dev = p.devices.find((d) => d.id === this.selected);
    if (room) {
      const set = (fn: (r: FloorRoom) => void) => this.change((q) => fn(q.rooms.find((r) => r.id === room.id)!));
      return html`<section><h3>${t("plan.room")}</h3>
        <label class="field">${t("plan.room_name")}<input .value=${room.name} @change=${(e: Event) => set((r) => (r.name = (e.target as HTMLInputElement).value.trim()))}></label>
        <label class="field">${t("plan.entry")}<select @change=${(e: Event) => set((r) => (r.entity = (e.target as HTMLSelectElement).value || undefined))}>
          <option value="">${t("plan.no_entry")}</option>${this.entryOptions((e) => e.kind === "room", room.entity)}</select></label>
        ${this.colorPicker(room.color, (c) => set((r) => (r.color = c === "gray" ? undefined : c)))}
        <p class="hint">${t("plan.area", { area: areaM2(room.points) })} · ${t("plan.vertex_hint")}</p>
        <div class="row"><button class="danger" @click=${this.removeSelected}>${icon("Trash2", 13)} ${t("plan.remove")}</button></div></section>`;
    }
    if (dev) {
      const e = this.entity(dev.entity);
      const r = roomAt(p, dev.x, dev.y);
      return html`<section><h3>${e?.name ?? dev.entity}</h3>
        <p class="hint">${r ? t("plan.in_room", { room: r.name }) : t("plan.no_room")}</p>
        <div class="row"><a class="btn ghost" href="/p/${this.project}/e/${dev.entity}">${icon("ExternalLink", 13)} ${t("plan.open_entry")}</a>
          <button class="danger" @click=${this.removeSelected}>${icon("Trash2", 13)} ${t("plan.remove")}</button></div></section>`;
    }
    const hints: Record<Tool, string> = { select: t("plan.hint_select"), room: t("plan.hint_room"), device: t("plan.hint_device"), calibrate: t("plan.hint_calibrate") };
    return html`<section><p class="hint">${hints[this.tool]}</p></section>
      <section><h3>${t("plan.settings")}</h3>
        <div class="row">
          <label class="field" style="flex:1">${t("plan.width")}<input type="number" min="1" max="1000" step="0.5" .value=${String(p.width / 1000)}
            @change=${(e: Event) => this.change((q) => (q.width = Math.round(Number((e.target as HTMLInputElement).value) * 1000) || q.width))}></label>
          <label class="field" style="flex:1">${t("plan.height")}<input type="number" min="1" max="1000" step="0.5" .value=${String(p.height / 1000)}
            @change=${(e: Event) => this.change((q) => (q.height = Math.round(Number((e.target as HTMLInputElement).value) * 1000) || q.height))}></label>
        </div>
        <label class="field">${t("plan.floor")}<select @change=${(e: Event) => this.change((q) => (q.floor = (e.target as HTMLSelectElement).value || undefined))}>
          <option value="">${t("plan.no_entry")}</option>${this.entryOptions((e) => e.kind === "floor", p.floor)}</select></label>
        <label class="btn ghost" style="justify-self:start">${icon("ImageUp", 14)} ${p.image ? t("plan.replace_image") : t("plan.add_image")}
          <input type="file" accept="image/png,image/jpeg,image/webp" hidden @change=${this.upload}></label>
        ${p.image ? html`<button class="ghost" style="justify-self:start" @click=${() => this.change((q) => { q.image = undefined; q.imageWidth = undefined; })}>${t("plan.remove_image")}</button>` : ""}
      </section>`;
  }
  private pendingColor = "gray";
  private newDevice = false;

  private legend() {
    const p = this.plan;
    if (!p.rooms.length && !p.devices.length) return html`<p class="muted">${this.canEdit ? t("plan.empty_edit") : t("plan.empty")}</p>`;
    const loc = locale();
    const devicesIn = (r: FloorRoom) => p.devices.filter((d) => roomAt(p, d.x, d.y)?.id === r.id);
    const outside = p.devices.filter((d) => !roomAt(p, d.x, d.y));
    const name = (d: FloorDevice) => this.entity(d.entity)?.name ?? d.entity;
    const link = (slug: string | undefined, text: string) => (slug ? html`<a href="/p/${this.project}/e/${slug}">${text}</a>` : text);
    return html`<table>
      <thead><tr><th>${t("plan.room")}</th><th>${t("plan.area_head")}</th><th>${t("plan.devices")}</th></tr></thead>
      <tbody>${p.rooms.map((r) => html`<tr><td>${link(r.entity, r.name || "–")}</td><td class="num">${areaM2(r.points).toLocaleString(loc === "de" ? "de-DE" : "en-GB")} m²</td>
        <td>${devicesIn(r).map((d, i) => html`${i ? ", " : ""}${link(d.entity, name(d))}`)}</td></tr>`)}
      ${outside.length ? html`<tr><td class="muted">${t("plan.no_room")}</td><td></td><td>${outside.map((d, i) => html`${i ? ", " : ""}${link(d.entity, name(d))}`)}</td></tr>` : ""}</tbody>
    </table>
    <p class="hint" style="margin-top:6px">${t("plan.size", { w: metres(p.width, loc), h: metres(p.height, loc) })}</p>`;
  }

  render() {
    const b = this.box;
    const tools: [Tool, string, string][] = [["select", "MousePointer2", t("plan.tool_select")], ["room", "PenTool", t("plan.tool_room")], ["device", "MapPin", t("plan.tool_device")]];
    if (this.plan.image) tools.push(["calibrate", "Ruler", t("plan.tool_calibrate")]);
    return html`
      ${this.canEdit ? html`<div class="bar">
        ${this.editing ? html`<div class="tools" role="toolbar" aria-label=${t("plan.tools")}>${tools.map(([id, ic, label]) => html`<button aria-pressed=${this.tool === id ? "true" : "false"}
          title=${label} @click=${() => this.setTool(id)}>${icon(ic, 15)} ${label}</button>`)}</div>
          <span class="spacer"></span>
          ${this.error ? html`<span class="error" role="alert">${this.error}</span>` : ""}
          <button class="ghost" @click=${this.cancel}>${t("doc.cancel")}</button>
          <button ?disabled=${this.busy || !this.dirty} @click=${this.save}>${icon("Check", 14)} ${t("doc.save")}</button>`
        : html`<span class="spacer"></span><button class="ghost" @click=${() => { this.editing = true; this.tool = "select"; }}>${icon("PenTool", 14)} ${t("plan.edit")}</button>`}
      </div>` : ""}
      <div class="wrap ${this.editing ? `edit t-${this.tool}` : ""}" @keydown=${this.onKey} tabindex="-1">
        <div class="stage">
          <svg class="plan" viewBox="${b.x} ${b.y} ${b.w} ${b.h}" role="img" aria-label=${this.doc?.title ?? ""}
            @pointerdown=${this.onDown} @pointermove=${this.onMove} @pointerup=${this.onUp} @pointercancel=${this.onUp}
            @pointerleave=${() => (this.cursor = undefined)} @dblclick=${() => this.tool === "room" && this.draft.length >= 3 && this.closeRoom()}
            @wheel=${this.onWheel}>
            <rect x="0" y="0" width=${this.plan.width} height=${this.plan.height} fill="none" stroke="var(--line)" vector-effect="non-scaling-stroke"/>
            ${this.drawPlan()}
          </svg>
          <div class="zoom">
            <button class="ghost" title=${t("plan.zoom_in")} aria-label=${t("plan.zoom_in")} @click=${() => this.zoom(0.8)}>${icon("ZoomIn", 15)}</button>
            <button class="ghost" title=${t("plan.zoom_out")} aria-label=${t("plan.zoom_out")} @click=${() => this.zoom(1.25)}>${icon("ZoomOut", 15)}</button>
            <button class="ghost" title=${t("plan.fit")} aria-label=${t("plan.fit")} @click=${this.fit}>${icon("Maximize", 15)}</button>
          </div>
        </div>
        ${this.editing ? html`<aside>${this.panel()}</aside>` : ""}
      </div>
      ${this.editing ? "" : this.legend()}`;
  }
}
customElements.define("phx-floorplan", PhxFloorplan);
