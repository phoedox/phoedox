// Floor plan document (ADR-0034): rooms as polygons, walls with doors and
// windows, devices and photos as points, all in millimetres. Viewing links to
// the entries; editing happens live in the shared draft (ADR-0027), so several
// people can draw at once, and publishing makes the draft the plan.

import { LitElement, css, html, nothing, svg, unsafeCSS } from "lit";
import { ApiError, api, type Doc, type DraftInfo, type Edge, type Entity } from "../api";
import type { PlanInput } from "./plan3d";
import { userColor } from "../collab";
import { fileURL, isImage, uploadFile, type FileInfo } from "../files";
import {
  CABLE_KINDS, DOOR_WIDTH, ROOM_COLORS, WALL_THICKNESS, WINDOW_WIDTH, areaM2, calibrate, clampOpening, distance, labelPoint, metres, nearestWall, newId,
  normalizePlan, onWall, openingSpan, orthogonal, planForSave, roomAt, snap, snapToEnds, wallLength, wallOutline,
  type FloorCable, type Pt3, type FloorDevice, type FloorOpening, type FloorRoom, type FloorWall, type Floorplan, type Pt,
} from "../floorplan";
import { ImportError, addImport, parseImport, type ImportResult } from "../floorplan-import";
import { connectPlan, type PlanLive, type PlanPeer, type Status } from "../floorplan-live";
import { locale, t } from "../i18n";
import { icon } from "../icon";
import { findKind, kindName, loadModel, type Model } from "../model";
import { currentUser, navigate } from "../state";
import { base } from "./shared";
import { toast } from "./toasts";

type Tool = "select" | "room" | "wall" | "opening" | "device" | "photo" | "cable" | "calibrate";
type Box = { x: number; y: number; w: number; h: number };
/** What the side panel asks for before something is added. */
type Pending =
  | { kind: "room"; points: Pt[] } | { kind: "device"; at: Pt } | { kind: "calibrate"; a: Pt; b: Pt }
  | { kind: "opening"; wall: string; offset: number } | { kind: "import"; result: ImportResult; name: string }
  | { kind: "cable"; points: Pt3[] };
type Drag =
  | { kind: "pan"; from: Pt; box: Box } | { kind: "device" | "photo"; id: string } | { kind: "vertex"; room: string; index: number }
  | { kind: "wallEnd"; wall: string; end: "a" | "b" } | { kind: "opening"; id: string } | { kind: "cvertex"; cable: string; index: number };

const GRID = 100; // snap: 10 cm
const NEW = "__new__";
const SNAP_PX = 12;

export class PhxFloorplan extends LitElement {
  static properties = {
    project: {}, doc: { attribute: false }, draft: { attribute: false }, canEdit: { type: Boolean },
    plan: { state: true }, editing: { state: true }, tool: { state: true }, selected: { state: true }, chain: { state: true },
    cursor: { state: true }, pending: { state: true }, entities: { state: true }, model: { state: true }, box: { state: true },
    busy: { state: true }, error: { state: true }, peers: { state: true }, status: { state: true }, saving: { state: true },
    photo: { state: true }, wallThickness: { state: true }, cableHeight: { state: true }, view3d: { state: true }, allFloors: { state: true }, cut: { state: true }, others: { state: true }, edges: { state: true },
  };
  static styles = [base, css`
    :host { display: block; }
    .wrap { display: grid; grid-template-columns: minmax(0, 1fr); gap: 12px; }
    .wrap.edit { grid-template-columns: minmax(0, 1fr) 280px; }
    .stage { position: relative; border: 1px solid var(--line); border-radius: 12px; background: var(--surface); overflow: hidden; }
    .stage.drop { outline: 2px dashed var(--accent); outline-offset: -4px; }
    svg.plan { display: block; width: 100%; height: min(70vh, 640px); touch-action: none; user-select: none; }
    .edit svg.plan { cursor: crosshair; } .edit.t-select svg.plan { cursor: default; }
    .grid { stroke: var(--line); }
    .room { stroke: var(--fg); stroke-linejoin: round; vector-effect: non-scaling-stroke; stroke-width: 1.5; }
    ${unsafeCSS(ROOM_COLORS.map((c) => `.room.c-${c} { fill: var(--tb-${c}); }`).join("\n"))}
    .walls .room { stroke-width: 0.75; stroke: var(--muted); }
    .room.link { cursor: pointer; }
    .room.sel { stroke: var(--accent); stroke-width: 3; }
    .wall { fill: var(--fg); stroke: var(--fg); stroke-width: 0.5; vector-effect: non-scaling-stroke; }
    .wall.sel { fill: var(--accent); stroke: var(--accent); }
    .gap { fill: var(--surface); stroke: none; }
    .leaf { fill: none; stroke: var(--fg); stroke-width: 1; vector-effect: non-scaling-stroke; }
    .win { stroke: var(--wire); stroke-width: 1.5; vector-effect: non-scaling-stroke; }
    .opening.sel .leaf, .opening.sel .win { stroke: var(--accent); stroke-width: 2.5; }
    .opening { cursor: pointer; }
    @media (hover: hover) and (pointer: fine) { .room.link:hover { stroke: var(--accent); } .dev:hover circle, .photo:hover circle { stroke: var(--fg); } }
    text { fill: var(--fg); font-family: var(--font-body); paint-order: stroke; stroke: var(--surface); stroke-linejoin: round; pointer-events: none; }
    text.area { fill: var(--muted); }
    .dev, .photo { cursor: pointer; color: var(--surface); }
    .dev circle { fill: var(--accent); stroke: var(--surface); vector-effect: non-scaling-stroke; stroke-width: 2; }
    .photo circle { fill: var(--wire); stroke: var(--surface); vector-effect: non-scaling-stroke; stroke-width: 2; }
    .dev.sel circle, .photo.sel circle { stroke: var(--fg); stroke-width: 3; }
    .draft { fill: color-mix(in srgb, var(--accent) 12%, transparent); stroke: var(--accent); stroke-dasharray: 6 4; vector-effect: non-scaling-stroke; stroke-width: 2; }
    .vertex { fill: var(--surface); stroke: var(--accent); vector-effect: non-scaling-stroke; stroke-width: 2; cursor: move; }
    .measure { stroke: var(--wire); stroke-width: 2; vector-effect: non-scaling-stroke; }
    .cable { fill: none; stroke-width: 3; vector-effect: non-scaling-stroke; stroke-linecap: round; stroke-linejoin: round; pointer-events: none; }
    .cable.network { stroke: var(--wire); } .cable.power { stroke: var(--bad); } .cable.coax { stroke: var(--signal); } .cable.other { stroke: var(--muted); }
    .cable.sel { stroke-width: 5; }
    .cable-hit { fill: none; stroke: transparent; stroke-width: 14; vector-effect: non-scaling-stroke; cursor: pointer; }
    .peer text { stroke-width: 0; paint-order: normal; fill: var(--surface); font-weight: 600; }
    .zoom { position: absolute; right: 8px; bottom: 8px; display: flex; gap: 4px; }
    .zoom button { padding: 4px 7px; }
    .bar { display: flex; flex-wrap: wrap; gap: 6px; align-items: center; margin-bottom: 8px; }
    .bar .tools { display: inline-flex; flex-wrap: wrap; border: 1px solid var(--line); border-radius: 8px; overflow: hidden; }
    .bar .tools button { border: 0; border-radius: 0; padding: 6px 10px; background: var(--surface); color: var(--fg); }
    .bar .tools button[aria-pressed="true"] { background: var(--accent-soft); color: var(--accent); }
    .bar .spacer { flex: 1; }
    .bar label.check { display: inline-flex; gap: 6px; align-items: center; font-size: 14px; }
    .bar label.check input { width: auto; }
    .bar .state { font-size: 13px; color: var(--muted); }
    .avatars { display: inline-flex; gap: 2px; }
    .avatars span { width: 24px; height: 24px; border-radius: 50%; display: inline-grid; place-items: center; font-size: 10px; font-weight: 700; color: var(--surface); }
    .notice { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; justify-content: space-between; border-left: 3px solid var(--accent); padding: 8px 12px; margin-bottom: 10px; background: var(--surface); border-radius: 0 8px 8px 0; font-size: 14px; }
    aside { display: grid; gap: 12px; align-content: start; font-size: 14px; }
    aside section { border: 1px solid var(--line); border-radius: 10px; padding: 10px 12px; display: grid; gap: 8px; background: var(--surface); }
    aside h3 { margin: 0; font-size: 14px; }
    aside img { width: 100%; border-radius: 8px; max-height: 180px; object-fit: cover; }
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
    dialog { border: 1px solid var(--line); border-radius: 12px; padding: 12px; max-width: min(92vw, 1100px); background: var(--surface); color: var(--fg); }
    dialog::backdrop { background: color-mix(in srgb, var(--fg) 45%, transparent); }
    dialog img { display: block; max-width: 100%; max-height: 78vh; border-radius: 8px; }
    dialog .row { justify-content: space-between; align-items: center; margin-top: 8px; }
    @media (max-width: 860px) { .wrap.edit { grid-template-columns: minmax(0, 1fr); } }
  `];

  project = "";
  doc?: Doc;
  /** The open draft, if any (from the page view). */
  draft?: DraftInfo;
  canEdit = false;
  plan: Floorplan = normalizePlan(undefined);
  editing = false;
  tool: Tool = "select";
  selected = "";
  /** Points of the room or wall being drawn. */
  chain: Pt[] = [];
  cursor?: Pt;
  pending?: Pending;
  entities: Entity[] = [];
  model?: Model;
  box: Box = { x: 0, y: 0, w: 20000, h: 15000 };
  busy = false;
  error = "";
  peers: PlanPeer[] = [];
  status: Status = "connecting";
  saving = false;
  /** The photo shown large (view mode). */
  photo = "";
  wallThickness = WALL_THICKNESS;
  /** Height above the floor (mm) for new cable points. */
  cableHeight = 300;
  /** 3D view (three.js, loaded on demand). */
  view3d = false;
  allFloors = false;
  cut = false;
  /** The project's other floor plans, for the building view. */
  others?: PlanInput[];
  edges: Edge[] = [];
  private drag?: Drag;
  private loadedFor = "";
  private live?: PlanLive;
  private ended = false;
  private photoAt?: Pt;

  disconnectedCallback() {
    super.disconnectedCallback();
    void this.live?.destroy();
    this.live = undefined;
  }

  willUpdate() {
    const key = `${this.doc?.id}@${this.doc?.version}`;
    if (this.doc && key !== this.loadedFor && !this.editing) {
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
  private toPlan(e: { clientX: number; clientY: number }): Pt {
    const m = this.svgEl!.getScreenCTM()!.inverse();
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
    const w = Math.min(Math.max(this.box.w * factor, 500), Math.max(this.plan.width, this.plan.height) * 6);
    const f = w / this.box.w;
    this.box = { x: cx - (cx - this.box.x) * f, y: cy - (cy - this.box.y) * f, w, h: this.box.h * f };
  }

  private onWheel = (e: WheelEvent) => {
    // In view mode the page scrolls; zoom needs Ctrl/⌘ (or a pinch, which sends ctrlKey).
    if (!this.editing && !e.ctrlKey && !e.metaKey) return;
    e.preventDefault();
    this.zoom(Math.exp(e.deltaY * 0.0015), this.toPlan(e));
  };

  // ---- 3D --------------------------------------------------------------

  private async show3d(on: boolean) {
    if (!on) {
      this.view3d = false;
      return;
    }
    this.busy = true;
    try {
      await import("./plan3d");
      const graph = await api<{ edges: Edge[] }>("GET", `/projects/${this.project}/graph`).catch(() => ({ edges: [] as Edge[] }));
      this.edges = graph.edges ?? [];
      this.view3d = true;
      if (!this.others) void this.loadOthers();
    } catch {
      toast(t("plan3d.no_webgl"), "error");
    } finally {
      this.busy = false;
    }
  }

  private async loadOthers() {
    const list = (await api<Doc[]>("GET", `/projects/${this.project}/documents?type=floorplan`).catch(() => [] as Doc[])) ?? [];
    const docs = await Promise.all(list.filter((d) => d.id !== this.doc?.id).map((d) => api<Doc>("GET", `/projects/${this.project}/documents/${d.id}`).catch(() => undefined)));
    this.others = docs.filter((d): d is Doc => !!d).map((d) => {
      const plan = normalizePlan(d.content as Partial<Floorplan>);
      return { plan, floor: plan.floor };
    });
  }

  private plans3d(): PlanInput[] {
    const own = { plan: this.plan, floor: this.plan.floor };
    return this.allFloors && this.others?.length ? [own, ...this.others] : [own];
  }

  // ---- live session ------------------------------------------------------

  /** Opens or joins the shared draft and starts drawing. */
  async startEdit() {
    if (!this.doc || this.live) return;
    this.busy = true;
    this.error = "";
    this.view3d = false;
    try {
      const d = await api<DraftInfo>("POST", `/projects/${this.project}/documents/${this.doc.id}/draft`);
      this.draft = d;
      const initial = normalizePlan(JSON.parse(d.markdown || "{}") as Partial<Floorplan>);
      this.plan = initial;
      this.editing = true;
      this.ended = false;
      this.setTool("select");
      const u = currentUser.get()!;
      const proto = location.protocol === "https:" ? "wss:" : "ws:";
      this.live = connectPlan({
        url: `${proto}//${location.host}/rt/doc/${this.project}`, docId: this.doc.id, initial,
        user: { name: u.name, color: userColor(u.id) },
        onChange: (plan) => (this.plan = plan),
        onSave: (plan) => this.saveDraft(plan),
        onPeers: (p) => (this.peers = p),
        onStatus: (s) => (this.status = s),
        onSaving: (s) => (this.saving = s),
        onReset: (by) => {
          // Changed outside the live session (API, AI, restore): load the draft again,
          // unless it is gone meanwhile (then the session has ended).
          if (this.ended) return;
          void api<DraftInfo>("GET", `/projects/${this.project}/documents/${this.doc!.id}/draft`).then(() => {
            if (this.ended) return;
            toast(t("editor.reset", { by }));
            void this.restart();
          }, () => this.onEnded("", by));
        },
        onEnded: (reason, by) => this.onEnded(reason, by),
      });
    } catch (e) {
      this.error = e instanceof ApiError ? e.text : t("error.unknown");
    } finally {
      this.busy = false;
    }
  }

  private async restart() {
    const live = this.live;
    this.live = undefined;
    await live?.destroy();
    this.editing = false;
    await this.startEdit();
  }

  private async saveDraft(plan: Floorplan) {
    if (this.ended || !this.doc) return;
    try {
      this.draft = await api<DraftInfo>("PUT", `/projects/${this.project}/documents/${this.doc.id}/draft`, { floorplan: planForSave(plan) });
    } catch (e) {
      if (e instanceof ApiError && e.key === "validation.draft_ended") {
        this.onEnded("", "");
        return;
      }
      this.error = e instanceof ApiError ? e.text : t("error.unknown");
      throw e;
    }
  }

  /** Closes the editor and shows the page again (reloaded by the view). */
  private async close() {
    const live = this.live;
    this.live = undefined;
    await live?.destroy();
    this.editing = false;
    this.peers = [];
    this.pending = undefined;
    this.selected = "";
    this.loadedFor = "";
    this.dispatchEvent(new CustomEvent("changed", { bubbles: true, composed: true }));
  }

  private onEnded(reason: string, by: string) {
    if (this.ended) return;
    this.ended = true;
    this.draft = undefined;
    if (by && by !== currentUser.get()?.name) {
      const key = reason === "published" ? "draft.ended_published" : reason === "deleted" ? "draft.ended_deleted" : reason === "cancelled" ? "draft.ended_cancelled" : "draft.ended";
      toast(t(key, { by }));
    }
    void this.close();
  }

  private async publish() {
    if (!this.doc) return;
    if (this.peers.length && !confirm(t("plan.publish_confirm", { names: this.peers.map((p) => p.name).join(", ") }))) return;
    this.busy = true;
    try {
      await this.live?.flush();
      this.ended = true;
      await api("POST", `/projects/${this.project}/documents/${this.doc.id}/draft/publish`);
      this.draft = undefined;
      toast(t("plan.saved"));
      await this.close();
    } catch (e) {
      this.ended = false;
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    } finally {
      this.busy = false;
    }
  }

  private async discard() {
    if (!this.doc || !confirm(t("plan.discard_draft"))) return;
    this.busy = true;
    try {
      this.ended = true;
      await api("DELETE", `/projects/${this.project}/documents/${this.doc.id}/draft`);
      this.draft = undefined;
      toast(t("draft.cancelled_toast"));
      await this.close();
    } catch (e) {
      this.ended = false;
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    } finally {
      this.busy = false;
    }
  }

  // ---- editing -----------------------------------------------------------

  private change(fn: (p: Floorplan) => void) {
    const before = this.plan;
    const next = structuredClone(before);
    fn(next);
    this.plan = next;
    this.live?.apply(before, next);
  }

  private gridded(e: PointerEvent, from?: Pt): Pt {
    const [x, y] = this.toPlan(e);
    let p: Pt = e.altKey ? [Math.round(x), Math.round(y)] : [snap(x, GRID), snap(y, GRID)];
    if (e.shiftKey && from) p = orthogonal(from, p);
    return p;
  }

  /** For walls: the end of another wall nearby, else the grid. */
  private wallPoint(e: PointerEvent, from?: Pt, skip?: string): Pt {
    const end = e.altKey ? undefined : snapToEnds(this.plan, this.toPlan(e), SNAP_PX * this.mmPerPx, skip);
    return end ? [end[0], end[1]] : this.gridded(e, from);
  }

  /** A device near a point (for cable ends). */
  private deviceNear(p: Pt): FloorDevice | undefined {
    let best: FloorDevice | undefined;
    let bestD = SNAP_PX * this.mmPerPx;
    for (const d of this.plan.devices) {
      const dist = distance([d.x, d.y], p);
      if (dist < bestD) [best, bestD] = [d, dist];
    }
    return best;
  }

  private cablePoint(e: PointerEvent): Pt3 {
    const raw = this.toPlan(e);
    const dev = e.altKey ? undefined : this.deviceNear(raw);
    const p = dev ? ([dev.x, dev.y] as Pt) : this.gridded(e, this.chain[this.chain.length - 1]);
    return [p[0], p[1], this.cableHeight];
  }

  private cableChain: Pt3[] = [];

  private finishCable() {
    if (this.cableChain.length >= 2) this.pending = { kind: "cable", points: this.cableChain };
    this.cableChain = [];
    this.chain = [];
  }

  private onDown(e: PointerEvent) {
    if (e.button !== 0) return;
    const target = e.target as Element;
    const ds = (sel: string, key: string) => (target.closest(`[data-${sel}]`) as HTMLElement | SVGElement | null)?.dataset[key];
    const devId = ds("dev", "dev");
    const photoId = ds("photo", "photo");
    const roomId = ds("room", "room");
    const wallId = ds("wall", "wall");
    const openingId = ds("opening", "opening");
    if (!this.editing) {
      if (photoId) {
        this.photo = photoId;
        void this.updateComplete.then(() => this.renderRoot.querySelector<HTMLDialogElement>("dialog.photo")?.showModal());
        return;
      }
      const slug = devId ? this.plan.devices.find((d) => d.id === devId)?.entity
        : roomId ? this.plan.rooms.find((r) => r.id === roomId)?.entity : undefined;
      if (slug) navigate(`/p/${this.project}/e/${slug}`);
      else this.startPan(e);
      return;
    }
    if (this.pending) return;
    switch (this.tool) {
      case "room": {
        const p = this.gridded(e, this.chain[this.chain.length - 1]);
        const first = this.chain[0];
        if (first && this.chain.length >= 3 && distance(first, p) < 10 * this.mmPerPx) this.closeRoom();
        else this.chain = [...this.chain, p];
        return;
      }
      case "wall": {
        const last = this.chain[this.chain.length - 1];
        const p = this.wallPoint(e, last);
        if (last && distance(last, p) > 0) {
          const w: FloorWall = { id: newId("w"), a: last, b: p, thickness: this.wallThickness };
          this.change((q) => q.walls.push(w));
        }
        this.chain = [p];
        return;
      }
      case "opening": {
        const hit = nearestWall(this.plan, this.toPlan(e), SNAP_PX * this.mmPerPx);
        if (hit) this.pending = { kind: "opening", wall: hit.wall.id, offset: hit.offset };
        else toast(t("plan.hint_opening"));
        return;
      }
      case "device":
        this.pending = { kind: "device", at: this.gridded(e) };
        return;
      case "cable": {
        this.cableChain = [...this.cableChain, this.cablePoint(e)];
        this.chain = this.cableChain.map((q) => [q[0], q[1]] as Pt);
        return;
      }
      case "photo":
        this.photoAt = this.toPlan(e);
        this.renderRoot.querySelector<HTMLInputElement>("input.photo-file")?.click();
        return;
      case "calibrate": {
        const p = this.toPlan(e);
        if (this.chain.length === 0) this.chain = [p];
        else {
          this.pending = { kind: "calibrate", a: this.chain[0], b: p };
          this.chain = [];
        }
        return;
      }
    }
    // select
    (e.currentTarget as Element).setPointerCapture(e.pointerId);
    const cvertex = target.closest<SVGElement>("[data-cvertex]");
    const cableId = ds("cable", "cable");
    if (cvertex) {
      this.drag = { kind: "cvertex", cable: cvertex.dataset.cvertex!, index: Number(cvertex.dataset.index) };
      return;
    }
    if (cableId && !openingId && !devId && !photoId) {
      this.selected = cableId;
      return;
    }
    const vertex = target.closest<SVGElement>("[data-vertex]");
    const end = target.closest<SVGElement>("[data-end]");
    if (vertex) {
      this.drag = { kind: "vertex", room: vertex.dataset.vertex!, index: Number(vertex.dataset.index) };
    } else if (end) {
      this.drag = { kind: "wallEnd", wall: end.dataset.end!, end: end.dataset.which as "a" | "b" };
    } else if (openingId) {
      this.selected = openingId;
      this.drag = { kind: "opening", id: openingId };
    } else if (devId) {
      this.selected = devId;
      this.drag = { kind: "device", id: devId };
    } else if (photoId) {
      this.selected = photoId;
      this.drag = { kind: "photo", id: photoId };
    } else if (wallId) {
      this.selected = wallId;
    } else if (roomId) {
      this.selected = roomId;
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
    if (this.editing) {
      const at = this.toPlan(e);
      this.live?.setCursor(at);
      if (this.tool === "room") this.cursor = this.gridded(e, this.chain[this.chain.length - 1]);
      else if (this.tool === "wall") this.cursor = this.wallPoint(e, this.chain[this.chain.length - 1]);
      else if (this.tool === "cable") this.cursor = this.cablePoint(e).slice(0, 2) as Pt;
      else if (this.tool === "calibrate") this.cursor = at;
    }
    const d = this.drag;
    if (!d) return;
    if (d.kind === "pan") {
      const k = this.mmPerPx;
      this.box = { ...d.box, x: d.box.x - (e.clientX - d.from[0]) * k, y: d.box.y - (e.clientY - d.from[1]) * k };
    } else if (d.kind === "device") {
      const [x, y] = this.gridded(e);
      this.change((p) => Object.assign(p.devices.find((v) => v.id === d.id)!, { x, y }));
    } else if (d.kind === "photo") {
      const [x, y] = this.toPlan(e).map(Math.round);
      this.change((p) => Object.assign(p.photos.find((v) => v.id === d.id)!, { x, y }));
    } else if (d.kind === "vertex") {
      const pt = this.gridded(e);
      this.change((p) => { p.rooms.find((r) => r.id === d.room)!.points[d.index] = pt; });
    } else if (d.kind === "wallEnd") {
      const w = this.plan.walls.find((x) => x.id === d.wall);
      if (!w) return;
      const pt = this.wallPoint(e, d.end === "a" ? w.b : w.a, w.id);
      if (distance(pt, d.end === "a" ? w.b : w.a) < 100) return;
      this.change((p) => {
        const ww = p.walls.find((x) => x.id === d.wall)!;
        ww[d.end] = pt;
        // Openings stay inside the shorter or longer wall.
        const len = wallLength(ww);
        for (const o of p.openings) if (o.wall === ww.id) { o.width = Math.min(o.width, len); o.offset = clampOpening(len, o.offset, o.width); }
      });
    } else if (d.kind === "cvertex") {
      const p = this.cablePoint(e);
      this.change((q) => {
        const pts = q.cables.find((x) => x.id === d.cable)!.points;
        pts[d.index] = [p[0], p[1], pts[d.index][2]];
      });
    } else if (d.kind === "opening") {
      const o = this.plan.openings.find((x) => x.id === d.id);
      const w = o && this.plan.walls.find((x) => x.id === o.wall);
      if (!o || !w) return;
      const offset = clampOpening(wallLength(w), Math.round(onWall(w, this.toPlan(e)).offset / 10) * 10, o.width);
      this.change((p) => { p.openings.find((x) => x.id === d.id)!.offset = offset; });
    }
  }

  private onUp() {
    this.drag = undefined;
  }

  private onKey = (e: KeyboardEvent) => {
    if (!this.editing || (e.target as HTMLElement).closest("input, select, textarea")) return;
    const mod = e.ctrlKey || e.metaKey;
    if (mod && e.key.toLowerCase() === "z") {
      if (e.shiftKey) this.live?.redo();
      else this.live?.undo();
    } else if (mod && e.key.toLowerCase() === "y") {
      this.live?.redo();
    } else if (e.key === "Escape") {
      if (this.chain.length) {
        this.chain = [];
        this.cableChain = [];
      } else if (this.pending) this.pending = undefined;
      else this.selected = "";
    } else if (e.key === "Enter" && this.tool === "room" && this.chain.length >= 3) {
      this.closeRoom();
    } else if (e.key === "Enter" && this.tool === "wall") {
      this.chain = [];
    } else if (e.key === "Enter" && this.tool === "cable") {
      this.finishCable();
    } else if (e.key === "Backspace" && this.chain.length && this.tool === "room") {
      this.chain = this.chain.slice(0, -1);
    } else if ((e.key === "Delete" || e.key === "Backspace") && this.selected) {
      this.removeSelected();
    } else return;
    e.preventDefault();
  };

  private closeRoom() {
    this.pending = { kind: "room", points: this.chain };
    this.chain = [];
    this.cursor = undefined;
  }

  private setTool(tool: Tool) {
    this.tool = tool;
    this.chain = [];
    this.cableChain = [];
    this.pending = undefined;
    this.cursor = undefined;
  }

  private removeSelected() {
    const id = this.selected;
    this.change((p) => {
      p.rooms = p.rooms.filter((r) => r.id !== id);
      p.devices = p.devices.filter((d) => d.id !== id);
      p.walls = p.walls.filter((w) => w.id !== id);
      p.openings = p.openings.filter((o) => o.id !== id && o.wall !== id);
      p.photos = p.photos.filter((f) => f.id !== id);
      p.cables = p.cables.filter((c) => c.id !== id);
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
    const num = (k: string) => Number(s(k).replace(",", "."));
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
      } else if (pend.kind === "opening") {
        const w = this.plan.walls.find((x) => x.id === pend.wall);
        if (!w) return;
        const len = wallLength(w);
        const width = Math.min(len, Math.max(100, Math.round(num("width") * 10)));
        const o: FloorOpening = { id: newId("o"), wall: w.id, kind: s("kind") === "window" ? "window" : "door", width, offset: clampOpening(len, pend.offset, width) };
        this.change((p) => p.openings.push(o));
        this.selected = o.id;
      } else if (pend.kind === "cable") {
        const c: FloorCable = { id: newId("c"), kind: (s("kind") || "network") as FloorCable["kind"], label: s("label") || undefined,
          points: pend.points, from: s("from") || undefined, to: s("to") || undefined };
        this.change((p) => p.cables.push(c));
        this.selected = c.id;
      } else if (pend.kind === "import") {
        const level = pend.result.levels[Number(s("level")) || 0];
        this.applyImport(level, pend.result.skipped);
      } else {
        const real = num("metres") * 1000;
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

  // ---- files ---------------------------------------------------------------

  private async upload(e: Event) {
    const input = e.target as HTMLInputElement;
    const file = input.files?.[0];
    input.value = "";
    if (!file || !this.doc) return;
    this.busy = true;
    try {
      let info: FileInfo = await uploadFile(this.project, file, { document: this.doc.id });
      // PDFs become an image of their first page.
      if (info.mime === "application/pdf") info = await api<FileInfo>("POST", `/projects/${this.project}/files/${info.id}/plan-image`);
      this.change((p) => { p.image = info.id; p.imageWidth = p.width; });
      this.tool = "calibrate";
      toast(t("plan.calibrate_hint"));
    } catch (err) {
      toast(err instanceof ApiError ? err.text : t("error.unknown"), "error");
    } finally {
      this.busy = false;
    }
  }

  /** Uploads photos and places them at a point (several side by side). */
  private async addPhotos(files: File[], at: Pt) {
    const images = files.filter((f) => isImage(f.type));
    if (!images.length || !this.doc) return;
    this.busy = true;
    try {
      const step = 40 * this.mmPerPx;
      for (const [i, file] of images.entries()) {
        const info = await uploadFile(this.project, file, { document: this.doc.id });
        const ph = { id: newId("f"), file: info.id, x: Math.round(at[0] + i * step), y: Math.round(at[1]), caption: file.name.replace(/\.[^.]+$/, "") };
        this.change((p) => p.photos.push(ph));
        this.selected = ph.id;
      }
    } catch (err) {
      toast(err instanceof ApiError ? err.text : t("error.unknown"), "error");
    } finally {
      this.busy = false;
    }
  }

  private onPhotoFile(e: Event) {
    const input = e.target as HTMLInputElement;
    const files = Array.from(input.files ?? []);
    input.value = "";
    if (this.photoAt) void this.addPhotos(files, this.photoAt);
  }

  private dropping = false;
  private onDragOver(e: DragEvent) {
    if (!this.editing || !e.dataTransfer?.types.includes("Files")) return;
    e.preventDefault();
    if (!this.dropping) {
      this.dropping = true;
      this.requestUpdate();
    }
  }

  private onDragLeave() {
    this.dropping = false;
    this.requestUpdate();
  }

  private onDrop(e: DragEvent) {
    if (!this.editing) return;
    e.preventDefault();
    this.dropping = false;
    this.requestUpdate();
    void this.addPhotos(Array.from(e.dataTransfer?.files ?? []), this.toPlan(e));
  }

  private async importFile(e: Event) {
    const input = e.target as HTMLInputElement;
    const file = input.files?.[0];
    input.value = "";
    if (!file) return;
    try {
      if (file.size > 50 << 20) throw new ImportError("plan.import_too_large");
      const result = parseImport(file.name, await file.text());
      if (result.levels.length > 1) this.pending = { kind: "import", result, name: file.name };
      else this.applyImport(result.levels[0], result.skipped);
    } catch (err) {
      toast(err instanceof ImportError ? t(err.key as any) : t("plan.import_invalid"), "error");
    }
  }

  private applyImport(level: ImportResult["levels"][number], skipped: number) {
    const res = addImport(this.plan, level);
    if (res.plan.walls.length > 5000 || res.plan.rooms.length > 500) {
      toast(t("plan.import_too_large"), "error");
      return;
    }
    const before = this.plan;
    this.plan = res.plan;
    this.live?.apply(before, res.plan);
    this.fit();
    toast(t("plan.imported", { walls: res.walls, rooms: res.rooms, openings: res.openings }) + (skipped ? ` ${t("plan.import_skipped", { count: skipped })}` : ""));
  }

  // ---- rendering ---------------------------------------------------------

  private drawGrid() {
    const lines = [];
    const step = 1000;
    for (let x = 0; x <= this.plan.width; x += step) lines.push(svg`<line class="grid" x1=${x} y1="0" x2=${x} y2=${this.plan.height} vector-effect="non-scaling-stroke"/>`);
    for (let y = 0; y <= this.plan.height; y += step) lines.push(svg`<line class="grid" x1="0" y1=${y} x2=${this.plan.width} y2=${y} vector-effect="non-scaling-stroke"/>`);
    return lines;
  }

  private drawOpening(o: FloorOpening, w: FloorWall) {
    const pts = (ps: Pt[]) => ps.map((q) => q.join(",")).join(" ");
    const [s, e] = openingSpan(w, o);
    const gap = wallOutline({ ...w, a: s, b: e, thickness: w.thickness + 4 });
    const len = wallLength(w) || 1;
    const [dx, dy] = [(w.b[0] - w.a[0]) / len, (w.b[1] - w.a[1]) / len];
    const n: Pt = [-dy, dx];
    const sel = this.editing && this.selected === o.id ? "sel" : "";
    if (o.kind === "door") {
      const leaf: Pt = [s[0] + n[0] * o.width, s[1] + n[1] * o.width];
      return svg`<g class="opening ${sel}" data-opening=${o.id}><title>${t("plan.door")}</title><polygon class="gap" points=${pts(gap)}/>
        <path class="leaf" d="M ${s[0]} ${s[1]} L ${leaf[0]} ${leaf[1]} A ${o.width} ${o.width} 0 0 0 ${e[0]} ${e[1]}"/></g>`;
    }
    const q = w.thickness / 4;
    return svg`<g class="opening ${sel}" data-opening=${o.id}><title>${t("plan.window")}</title><polygon class="gap" points=${pts(gap)}/>
      <line class="win" x1=${s[0] + n[0] * q} y1=${s[1] + n[1] * q} x2=${e[0] + n[0] * q} y2=${e[1] + n[1] * q}/>
      <line class="win" x1=${s[0] - n[0] * q} y1=${s[1] - n[1] * q} x2=${e[0] - n[0] * q} y2=${e[1] - n[1] * q}/></g>`;
  }

  private drawPlan() {
    const p = this.plan;
    // Markers and labels keep about the same size on screen at every zoom.
    const unit = this.box.w / 42;
    const loc = locale();
    const pts = (ps: Pt[]) => ps.map((q) => q.join(",")).join(" ");
    const sel = (id: string) => (this.editing && id === this.selected ? "sel" : "");
    const selRoom = this.editing ? p.rooms.find((r) => r.id === this.selected) : undefined;
    const selWall = this.editing ? p.walls.find((w) => w.id === this.selected) : undefined;
    const walls = new Map(p.walls.map((w) => [w.id, w]));
    return svg`
      ${p.image && p.imageWidth ? svg`<image href=${fileURL({ id: p.image, name: "plan" })} x="0" y="0" width=${p.imageWidth} opacity=".55"/>` : nothing}
      ${this.editing || !p.image ? this.drawGrid() : nothing}
      <g class=${p.walls.length ? "walls" : ""}>${p.rooms.map((r) => svg`<polygon data-room=${r.id} class="room c-${r.color || "gray"} ${r.entity && !this.editing ? "link" : ""} ${sel(r.id)}"
        points=${pts(r.points)}><title>${r.name}</title></polygon>`)}</g>
      ${p.walls.map((w) => svg`<polygon data-wall=${w.id} class="wall ${sel(w.id)}" points=${pts(wallOutline(w))}><title>${t("plan.wall")} · ${metres(wallLength(w), loc)}</title></polygon>`)}
      ${p.openings.map((o) => (walls.get(o.wall) ? this.drawOpening(o, walls.get(o.wall)!) : nothing))}
      ${p.rooms.map((r) => {
        const [x, y] = labelPoint(r.points);
        return svg`<text x=${x} y=${y} font-size=${unit * 0.75} text-anchor="middle" stroke-width=${unit * 0.15}>${r.name}</text>
          <text class="area" x=${x} y=${y + unit * 0.85} font-size=${unit * 0.55} text-anchor="middle" stroke-width=${unit * 0.12}>${areaM2(r.points).toLocaleString(loc === "de" ? "de-DE" : "en-GB")} m²</text>`;
      })}
      ${p.devices.map((d) => {
        const e = this.entity(d.entity);
        const k = findKind(this.model, e?.kind ?? "");
        const r = unit * 0.55;
        return svg`<g data-dev=${d.id} class="dev ${sel(d.id)}" role="link" aria-label=${e?.name ?? d.entity}>
          <title>${e?.name ?? d.entity}${e ? ` · ${kindName(k, e.kind)}` : ""}</title>
          <circle cx=${d.x} cy=${d.y} r=${r}/>
          <g transform="translate(${d.x - r * 0.62} ${d.y - r * 0.62})">${icon(k?.icon ?? "Cpu", r * 1.24)}</g>
          <text x=${d.x} y=${d.y + r + unit * 0.6} font-size=${unit * 0.5} text-anchor="middle" stroke-width=${unit * 0.12}>${e?.name ?? d.entity}</text></g>`;
      })}
      ${p.photos.map((f) => {
        const r = unit * 0.45;
        return svg`<g data-photo=${f.id} class="photo ${sel(f.id)}" role="button" aria-label=${f.caption || t("plan.photo")}>
          <title>${f.caption || t("plan.photo")}</title>
          <circle cx=${f.x} cy=${f.y} r=${r}/>
          <g transform="translate(${f.x - r * 0.6} ${f.y - r * 0.6})">${icon("Camera", r * 1.2)}</g></g>`;
      })}
      ${p.cables.map((c) => {
        const d = c.points.map((q, i) => `${i ? "L" : "M"}${q[0]} ${q[1]}`).join(" ");
        return svg`<g data-cable=${c.id}><title>${c.label || t(`plan.cable.${c.kind}` as any)}</title>
          <path class="cable-hit" d=${d}/><path class="cable ${c.kind} ${sel(c.id)}" d=${d}/></g>`;
      })}
      ${this.editing && p.cables.some((c) => c.id === this.selected) ? p.cables.find((c) => c.id === this.selected)!.points.map((q, i) =>
        svg`<circle class="vertex" data-cvertex=${this.selected} data-index=${i} cx=${q[0]} cy=${q[1]} r=${unit * 0.2}/>`) : nothing}
      ${this.tool === "cable" && this.cableChain.length ? svg`<path class="cable network" stroke-dasharray="6 4"
        d=${[...this.cableChain.map((q) => [q[0], q[1]] as Pt), ...(this.cursor ? [this.cursor] : [])].map((q, i) => `${i ? "L" : "M"}${q[0]} ${q[1]}`).join(" ")}/>
        ${this.cableChain.map((q) => svg`<circle class="vertex" cx=${q[0]} cy=${q[1]} r=${unit * 0.16}/>`)}` : nothing}
      ${this.pending?.kind === "cable" ? svg`<path class="cable network" stroke-dasharray="6 4" d=${this.pending.points.map((q, i) => `${i ? "L" : "M"}${q[0]} ${q[1]}`).join(" ")}/>` : nothing}
      ${selRoom ? selRoom.points.map((q, i) => svg`<circle class="vertex" data-vertex=${selRoom.id} data-index=${i} cx=${q[0]} cy=${q[1]} r=${unit * 0.22}/>`) : nothing}
      ${selWall ? (["a", "b"] as const).map((k) => svg`<circle class="vertex" data-end=${selWall.id} data-which=${k} cx=${selWall[k][0]} cy=${selWall[k][1]} r=${unit * 0.22}/>`) : nothing}
      ${this.chain.length && this.tool === "room" ? svg`<polygon class="draft" points=${pts(this.cursor ? [...this.chain, this.cursor] : this.chain)}/>
        ${this.chain.map((q) => svg`<circle class="vertex" cx=${q[0]} cy=${q[1]} r=${unit * 0.18}/>`)}` : nothing}
      ${this.chain.length && this.tool === "wall" && this.cursor ? svg`<polygon class="draft" points=${pts(wallOutline({ id: "", a: this.chain[0], b: this.cursor, thickness: this.wallThickness }))}/>
        <text x=${(this.chain[0][0] + this.cursor[0]) / 2} y=${(this.chain[0][1] + this.cursor[1]) / 2 - unit * 0.4} font-size=${unit * 0.5} text-anchor="middle" stroke-width=${unit * 0.12}>${metres(distance(this.chain[0], this.cursor), loc)}</text>` : nothing}
      ${this.tool === "calibrate" && this.editing && this.chain.length && this.cursor ? svg`<line class="measure" x1=${this.chain[0][0]} y1=${this.chain[0][1]} x2=${this.cursor[0]} y2=${this.cursor[1]}/>` : nothing}
      ${this.pending?.kind === "calibrate" ? svg`<line class="measure" x1=${this.pending.a[0]} y1=${this.pending.a[1]} x2=${this.pending.b[0]} y2=${this.pending.b[1]}/>` : nothing}
      ${this.pending?.kind === "room" ? svg`<polygon class="draft" points=${pts(this.pending.points)}/>` : nothing}
      ${this.pending?.kind === "device" ? svg`<circle class="vertex" cx=${this.pending.at[0]} cy=${this.pending.at[1]} r=${unit * 0.3}/>` : nothing}
      ${this.pending?.kind === "opening" ? (() => {
        const w = walls.get(this.pending.wall);
        const at = w && onWall(w, [w.a[0] + ((w.b[0] - w.a[0]) * this.pending.offset) / (wallLength(w) || 1), w.a[1] + ((w.b[1] - w.a[1]) * this.pending.offset) / (wallLength(w) || 1)]).at;
        return at ? svg`<circle class="vertex" cx=${at[0]} cy=${at[1]} r=${unit * 0.3}/>` : nothing;
      })() : nothing}
      ${this.editing ? this.peers.filter((x) => x.cursor).map((x) => svg`<g class="peer">
        <circle cx=${x.cursor![0]} cy=${x.cursor![1]} r=${unit * 0.18} fill=${x.color}/>
        <rect x=${x.cursor![0] + unit * 0.3} y=${x.cursor![1] - unit * 0.35} width=${unit * (0.6 + x.name.length * 0.3)} height=${unit * 0.7} rx=${unit * 0.15} fill=${x.color}/>
        <text x=${x.cursor![0] + unit * 0.6} y=${x.cursor![1] + unit * 0.17} font-size=${unit * 0.5}>${x.name}</text></g>`) : nothing}`;
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

  private formButtons(label = t("plan.add")) {
    return html`<div class="row"><button ?disabled=${this.busy}>${label}</button><button type="button" class="ghost" @click=${() => (this.pending = undefined)}>${t("doc.cancel")}</button></div>`;
  }

  private panel() {
    const p = this.plan;
    const pend = this.pending;
    const loc = locale();
    if (pend?.kind === "room") {
      return html`<section><h3>${t("plan.new_room")}</h3>
        <form @submit=${this.submitPending} style="display:grid;gap:8px">
          <label class="field">${t("plan.room_name")}<input name="name" required autofocus></label>
          <label class="field">${t("plan.entry")}<select name="entity">
            <option value=${NEW}>${t("plan.new_entry")}</option><option value="">${t("plan.no_entry")}</option>${this.entryOptions((e) => e.kind === "room")}</select></label>
          ${this.colorPicker(this.pendingColor, (c) => (this.pendingColor = c))}<input type="hidden" name="color" .value=${this.pendingColor}>
          <p class="hint">${t("plan.area", { area: areaM2(pend.points) })}</p>
          ${this.formButtons()}
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
          ${this.formButtons()}
        </form></section>`;
    }
    if (pend?.kind === "opening") {
      return html`<section><h3>${t("plan.new_opening")}</h3>
        <form @submit=${this.submitPending} style="display:grid;gap:8px">
          <label class="field">${t("plan.opening_kind")}<select name="kind" autofocus @change=${(e: Event) => {
            const w = (e.target as HTMLSelectElement).form!.elements.namedItem("width") as HTMLInputElement;
            w.value = String(((e.target as HTMLSelectElement).value === "window" ? WINDOW_WIDTH : DOOR_WIDTH) / 10);
          }}><option value="door">${t("plan.door")}</option><option value="window">${t("plan.window")}</option></select></label>
          <label class="field">${t("plan.width_cm")}<input name="width" type="number" min="10" step="1" required .value=${String(DOOR_WIDTH / 10)}></label>
          ${this.formButtons()}
        </form></section>`;
    }
    if (pend?.kind === "cable") {
      const near = (q: Pt3) => this.deviceNear([q[0], q[1]])?.entity ?? "";
      const [from, to] = [near(pend.points[0]), near(pend.points[pend.points.length - 1])];
      const ends = (cur: string) => html`<option value="">${t("plan.no_entry")}</option>${this.entryOptions((e) => e.kind !== "room" && e.kind !== "floor", cur)}`;
      return html`<section><h3>${t("plan.new_cable")}</h3>
        <form @submit=${this.submitPending} style="display:grid;gap:8px">
          <label class="field">${t("plan.cable_kind")}<select name="kind" autofocus>${CABLE_KINDS.map((k) => html`<option value=${k}>${t(`plan.cable.${k}` as any)}</option>`)}</select></label>
          <label class="field">${t("plan.cable_label")}<input name="label" maxlength="200" placeholder="Patch 12"></label>
          <label class="field">${t("plan.cable_from")}<select name="from">${ends(from)}</select></label>
          <label class="field">${t("plan.cable_to")}<select name="to">${ends(to)}</select></label>
          <p class="hint">${t("plan.cable_link_hint")}</p>
          ${this.formButtons()}
        </form></section>`;
    }
    if (pend?.kind === "import") {
      return html`<section><h3>${t("plan.import")}</h3>
        <form @submit=${this.submitPending} style="display:grid;gap:8px">
          <p class="hint">${t("plan.import_levels", { name: pend.name })}</p>
          <label class="field">${t("plan.level")}<select name="level" autofocus>${pend.result.levels.map((l, i) => html`<option value=${i}>${l.name || t("plan.level_n", { n: i + 1 })} · ${t("plan.import_counts", { walls: l.walls.length, rooms: l.rooms.length })}</option>`)}</select></label>
          ${this.formButtons(t("plan.import"))}
        </form></section>`;
    }
    if (pend?.kind === "calibrate") {
      return html`<section><h3>${t("plan.calibrate")}</h3>
        <form @submit=${this.submitPending} style="display:grid;gap:8px">
          <label class="field">${t("plan.real_distance")}<input name="metres" inputmode="decimal" required pattern="[0-9]+([.,][0-9]+)?" autofocus placeholder="4,25"></label>
          ${this.formButtons(t("plan.apply"))}
        </form></section>`;
    }
    const room = p.rooms.find((r) => r.id === this.selected);
    const dev = p.devices.find((d) => d.id === this.selected);
    const wall = p.walls.find((w) => w.id === this.selected);
    const opening = p.openings.find((o) => o.id === this.selected);
    const photo = p.photos.find((f) => f.id === this.selected);
    const remove = html`<button class="danger" @click=${this.removeSelected}>${icon("Trash2", 13)} ${t("plan.remove")}</button>`;
    if (room) {
      const set = (fn: (r: FloorRoom) => void) => this.change((q) => fn(q.rooms.find((r) => r.id === room.id)!));
      return html`<section><h3>${t("plan.room")}</h3>
        <label class="field">${t("plan.room_name")}<input .value=${room.name} @change=${(e: Event) => set((r) => (r.name = (e.target as HTMLInputElement).value.trim()))}></label>
        <label class="field">${t("plan.entry")}<select @change=${(e: Event) => set((r) => (r.entity = (e.target as HTMLSelectElement).value || undefined))}>
          <option value="">${t("plan.no_entry")}</option>${this.entryOptions((e) => e.kind === "room", room.entity)}</select></label>
        ${this.colorPicker(room.color, (c) => set((r) => (r.color = c === "gray" ? undefined : c)))}
        <p class="hint">${t("plan.area", { area: areaM2(room.points) })} · ${t("plan.vertex_hint")}</p>
        <div class="row">${remove}</div></section>`;
    }
    if (wall) {
      const set = (fn: (w: FloorWall) => void) => this.change((q) => fn(q.walls.find((w) => w.id === wall.id)!));
      return html`<section><h3>${t("plan.wall")} · ${metres(wallLength(wall), loc)}</h3>
        <div class="row">
          <label class="field" style="flex:1">${t("plan.thickness_cm")}<input type="number" min="1" max="200" step="0.5" .value=${String(wall.thickness / 10)}
            @change=${(e: Event) => set((w) => (w.thickness = Math.min(2000, Math.max(10, Math.round(Number((e.target as HTMLInputElement).value) * 10) || w.thickness))))}></label>
          <label class="field" style="flex:1">${t("plan.height_m")}<input type="number" min="0" max="100" step="0.05" .value=${wall.height ? String(wall.height / 1000) : ""} placeholder="–"
            @change=${(e: Event) => set((w) => { const h = Math.round(Number((e.target as HTMLInputElement).value) * 1000); if (h > 0) w.height = h; else delete w.height; })}></label>
        </div>
        <p class="hint">${t("plan.wall_hint")}</p>
        <div class="row">${remove}</div></section>`;
    }
    if (opening) {
      const w = p.walls.find((x) => x.id === opening.wall);
      const set = (fn: (o: FloorOpening) => void) => this.change((q) => fn(q.openings.find((o) => o.id === opening.id)!));
      return html`<section><h3>${t(opening.kind === "door" ? "plan.door" : "plan.window")}</h3>
        <label class="field">${t("plan.opening_kind")}<select @change=${(e: Event) => set((o) => (o.kind = (e.target as HTMLSelectElement).value as "door" | "window"))}>
          <option value="door" ?selected=${opening.kind === "door"}>${t("plan.door")}</option><option value="window" ?selected=${opening.kind === "window"}>${t("plan.window")}</option></select></label>
        <label class="field">${t("plan.width_cm")}<input type="number" min="10" step="1" .value=${String(opening.width / 10)}
          @change=${(e: Event) => set((o) => {
            const len = w ? wallLength(w) : o.width;
            o.width = Math.min(len, Math.max(100, Math.round(Number((e.target as HTMLInputElement).value) * 10) || o.width));
            o.offset = clampOpening(len, o.offset, o.width);
          })}></label>
        <p class="hint">${t("plan.opening_hint")}</p>
        <div class="row">${remove}</div></section>`;
    }
    const cable = p.cables.find((c) => c.id === this.selected);
    if (cable) {
      const set = (fn: (c: FloorCable) => void) => this.change((q) => fn(q.cables.find((c) => c.id === cable.id)!));
      const ends = (cur?: string) => html`<option value="">${t("plan.no_entry")}</option>${this.entryOptions((e) => e.kind !== "room" && e.kind !== "floor", cur)}`;
      const len = cable.points.slice(1).reduce((sum, q, i) => sum + Math.hypot(q[0] - cable.points[i][0], q[1] - cable.points[i][1], q[2] - cable.points[i][2]), 0);
      return html`<section><h3>${cable.label || t(`plan.cable.${cable.kind}` as any)} · ${metres(len, loc)}</h3>
        <label class="field">${t("plan.cable_kind")}<select @change=${(e: Event) => set((c) => (c.kind = (e.target as HTMLSelectElement).value as FloorCable["kind"]))}>
          ${CABLE_KINDS.map((k) => html`<option value=${k} ?selected=${k === cable.kind}>${t(`plan.cable.${k}` as any)}</option>`)}</select></label>
        <label class="field">${t("plan.cable_label")}<input .value=${cable.label ?? ""} maxlength="200" @change=${(e: Event) => set((c) => (c.label = (e.target as HTMLInputElement).value.trim() || undefined))}></label>
        <label class="field">${t("plan.cable_from")}<select @change=${(e: Event) => set((c) => (c.from = (e.target as HTMLSelectElement).value || undefined))}>${ends(cable.from)}</select></label>
        <label class="field">${t("plan.cable_to")}<select @change=${(e: Event) => set((c) => (c.to = (e.target as HTMLSelectElement).value || undefined))}>${ends(cable.to)}</select></label>
        <table><thead><tr><th>#</th><th>${t("plan.cable_height")}</th></tr></thead><tbody>${cable.points.map((q, i) => html`<tr><td>${i + 1}</td>
          <td><input type="number" step="5" style="max-width:110px" .value=${String(q[2] / 10)} aria-label=${t("plan.cable_height")}
            @change=${(e: Event) => set((c) => { c.points[i] = [c.points[i][0], c.points[i][1], Math.round(Number((e.target as HTMLInputElement).value) * 10) || 0]; })}></td></tr>`)}</tbody></table>
        <p class="hint">${t("plan.cable_edit_hint")}</p>
        <div class="row">${remove}</div></section>`;
    }
    if (photo) {
      return html`<section><h3>${t("plan.photo")}</h3>
        <img src=${fileURL({ id: photo.file, name: "photo" })} alt=${photo.caption ?? ""}>
        <label class="field">${t("plan.caption")}<input .value=${photo.caption ?? ""} maxlength="500"
          @change=${(e: Event) => this.change((q) => { q.photos.find((f) => f.id === photo.id)!.caption = (e.target as HTMLInputElement).value.trim() || undefined; })}></label>
        <div class="row"><a class="btn ghost" href=${fileURL({ id: photo.file, name: "photo" })} target="_blank" rel="noopener">${icon("ExternalLink", 13)} ${t("plan.open_photo")}</a>${remove}</div></section>`;
    }
    if (dev) {
      const e = this.entity(dev.entity);
      const r = roomAt(p, dev.x, dev.y);
      return html`<section><h3>${e?.name ?? dev.entity}</h3>
        <p class="hint">${r ? t("plan.in_room", { room: r.name }) : t("plan.no_room")}</p>
        <div class="row"><a class="btn ghost" href="/p/${this.project}/e/${dev.entity}">${icon("ExternalLink", 13)} ${t("plan.open_entry")}</a>${remove}</div></section>`;
    }
    const hints: Record<Tool, string> = {
      select: t("plan.hint_select"), room: t("plan.hint_room"), wall: t("plan.hint_wall"), opening: t("plan.hint_opening"),
      device: t("plan.hint_device"), photo: t("plan.hint_photo"), cable: t("plan.hint_cable"), calibrate: t("plan.hint_calibrate"),
    };
    return html`<section><p class="hint">${hints[this.tool]}</p>
        ${this.tool === "wall" ? html`<label class="field">${t("plan.thickness_cm")}<input type="number" min="1" max="200" step="0.5" .value=${String(this.wallThickness / 10)}
          @change=${(e: Event) => (this.wallThickness = Math.min(2000, Math.max(10, Math.round(Number((e.target as HTMLInputElement).value) * 10) || WALL_THICKNESS)))}></label>` : ""}
        ${this.tool === "cable" ? html`<label class="field">${t("plan.cable_height")}<input type="number" step="5" min="-2000" max="10000" .value=${String(this.cableHeight / 10)}
          @change=${(e: Event) => (this.cableHeight = Math.round(Number((e.target as HTMLInputElement).value) * 10) || 0)}></label>
          <p class="hint">${t("plan.cable_height_hint")}</p>` : ""}</section>
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
          <input type="file" accept="image/png,image/jpeg,image/webp,application/pdf" hidden @change=${this.upload}></label>
        ${p.image ? html`<button class="ghost" style="justify-self:start" @click=${() => this.change((q) => { q.image = undefined; q.imageWidth = undefined; })}>${t("plan.remove_image")}</button>` : ""}
        <label class="btn ghost" style="justify-self:start">${icon("Import", 14)} ${t("plan.import")}
          <input type="file" accept=".dxf,.dwg,.ifc,.json" hidden @change=${this.importFile}></label>
        <p class="hint">${t("plan.import_hint")}</p>
      </section>`;
  }
  private pendingColor = "gray";
  private newDevice = false;

  private legend() {
    const p = this.plan;
    if (!p.rooms.length && !p.devices.length && !p.walls.length) return html`<p class="muted">${this.canEdit ? t("plan.empty_edit") : t("plan.empty")}</p>`;
    const loc = locale();
    const devicesIn = (r: FloorRoom) => p.devices.filter((d) => roomAt(p, d.x, d.y)?.id === r.id);
    const outside = p.devices.filter((d) => !roomAt(p, d.x, d.y));
    const name = (d: FloorDevice) => this.entity(d.entity)?.name ?? d.entity;
    const link = (slug: string | undefined, text: string) => (slug ? html`<a href="/p/${this.project}/e/${slug}">${text}</a>` : text);
    return html`${p.rooms.length || p.devices.length ? html`<table>
      <thead><tr><th>${t("plan.room")}</th><th>${t("plan.area_head")}</th><th>${t("plan.devices")}</th></tr></thead>
      <tbody>${p.rooms.map((r) => html`<tr><td>${link(r.entity, r.name || "–")}</td><td class="num">${areaM2(r.points).toLocaleString(loc === "de" ? "de-DE" : "en-GB")} m²</td>
        <td>${devicesIn(r).map((d, i) => html`${i ? ", " : ""}${link(d.entity, name(d))}`)}</td></tr>`)}
      ${outside.length ? html`<tr><td class="muted">${t("plan.no_room")}</td><td></td><td>${outside.map((d, i) => html`${i ? ", " : ""}${link(d.entity, name(d))}`)}</td></tr>` : ""}</tbody>
    </table>` : ""}
    <p class="hint" style="margin-top:6px">${t("plan.size", { w: metres(p.width, loc), h: metres(p.height, loc) })}${p.photos.length ? ` · ${t("plan.photo_count", { count: p.photos.length })}` : ""}</p>`;
  }

  private draftNote() {
    const d = this.draft;
    if (!d || this.editing || !this.canEdit) return "";
    return html`<div class="notice" role="note"><span>${icon("FilePen", 15)} ${t("plan.draft_open", { names: d.participants.map((p) => p.name).join(", ") })}</span>
      <button ?disabled=${this.busy} @click=${() => this.startEdit()}>${icon("PenTool", 14)} ${t("draft.continue")}</button></div>`;
  }

  private photoDialog() {
    const f = this.plan.photos.find((x) => x.id === this.photo);
    return html`<dialog class="photo" @close=${() => (this.photo = "")} @click=${(e: Event) => e.target === e.currentTarget && (e.currentTarget as HTMLDialogElement).close()}>
      ${f ? html`<img src=${fileURL({ id: f.file, name: "photo" })} alt=${f.caption ?? ""}>
        <div class="row"><span>${f.caption ?? ""}</span><button class="ghost" autofocus @click=${(e: Event) => (e.target as HTMLElement).closest("dialog")!.close()}>${t("plan.close")}</button></div>` : ""}
    </dialog>`;
  }

  render() {
    const b = this.box;
    const tools: [Tool, string, string][] = [
      ["select", "MousePointer2", t("plan.tool_select")], ["wall", "BrickWall", t("plan.tool_wall")], ["opening", "DoorOpen", t("plan.tool_opening")],
      ["room", "PenTool", t("plan.tool_room")], ["device", "MapPin", t("plan.tool_device")], ["photo", "Camera", t("plan.tool_photo")],
      ["cable", "Cable", t("plan.tool_cable")],
    ];
    if (this.plan.image) tools.push(["calibrate", "Ruler", t("plan.tool_calibrate")]);
    const state = this.status !== "connected" ? t(this.status === "connecting" ? "editor.connecting" : "editor.disconnected") : this.saving ? t("editor.saving") : t("editor.saved");
    return html`
      ${this.draftNote()}
      ${this.canEdit || !this.editing ? html`<div class="bar">
        ${this.editing ? html`<div class="tools" role="toolbar" aria-label=${t("plan.tools")}>${tools.map(([id, ic, label]) => html`<button aria-pressed=${this.tool === id ? "true" : "false"}
          title=${label} @click=${() => this.setTool(id)}>${icon(ic, 15)} ${label}</button>`)}</div>
          <button class="ghost icon" title=${t("editor.undo")} aria-label=${t("editor.undo")} @click=${() => this.live?.undo()}>${icon("Undo2", 15)}</button>
          <button class="ghost icon" title=${t("editor.redo")} aria-label=${t("editor.redo")} @click=${() => this.live?.redo()}>${icon("Redo2", 15)}</button>
          <span class="spacer"></span>
          ${this.peers.length ? html`<span class="avatars" title=${t("editor.peers", { names: this.peers.map((p) => p.name).join(", ") })}>
            ${this.peers.map((p) => html`<span style="background:${p.color}">${p.name.slice(0, 2).toUpperCase()}</span>`)}</span>` : ""}
          <span class="state" role="status">${state}</span>
          ${this.error ? html`<span class="error" role="alert">${this.error}</span>` : ""}
          <button class="ghost" ?disabled=${this.busy} @click=${() => this.close()} title=${t("plan.leave_hint")}>${t("plan.leave")}</button>
          ${this.draft?.canEnd !== false ? html`<button class="ghost danger" ?disabled=${this.busy} @click=${this.discard}>${t("plan.discard_button")}</button>
          <button ?disabled=${this.busy} @click=${this.publish}>${icon("Check", 14)} ${t("plan.publish")}</button>` : ""}`
        : html`<div class="tools" role="group" aria-label=${t("plan3d.view")}>
            <button aria-pressed=${this.view3d ? "false" : "true"} @click=${() => this.show3d(false)}>${icon("LandPlot", 15)} 2D</button>
            <button aria-pressed=${this.view3d ? "true" : "false"} ?disabled=${this.busy} @click=${() => this.show3d(true)}>${icon("Box", 15)} 3D</button></div>
          ${this.view3d && this.others?.length ? html`<label class="check"><input type="checkbox" .checked=${this.allFloors} @change=${(e: Event) => (this.allFloors = (e.target as HTMLInputElement).checked)}> ${t("plan3d.all_floors")}</label>` : ""}
          ${this.view3d ? html`<label class="check"><input type="checkbox" .checked=${this.cut} @change=${(e: Event) => (this.cut = (e.target as HTMLInputElement).checked)}> ${t("plan3d.cut")}</label>` : ""}
          ${this.view3d ? html`<button class="ghost icon" title=${t("plan.fit")} aria-label=${t("plan.fit")} @click=${() => this.renderRoot.querySelector<HTMLElement & { resetView(): void }>("phx-plan3d")?.resetView()}>${icon("Maximize", 15)}</button>` : ""}
          <span class="spacer"></span>${this.draft || !this.canEdit ? "" : html`<button class="ghost" ?disabled=${this.busy} @click=${() => this.startEdit()}>${icon("PenTool", 14)} ${t("plan.edit")}</button>`}`}
      </div>` : ""}
      ${this.view3d && !this.editing ? html`<phx-plan3d .project=${this.project} .plans=${this.plans3d()} .entities=${this.entities} .edges=${this.edges} ?cut=${this.cut}></phx-plan3d>` : html`
      <div class="wrap ${this.editing ? `edit t-${this.tool}` : ""}" @keydown=${this.onKey} tabindex="-1">
        <div class="stage ${this.dropping ? "drop" : ""}" @dragover=${this.onDragOver} @dragleave=${this.onDragLeave} @drop=${this.onDrop}>
          <svg class="plan" viewBox="${b.x} ${b.y} ${b.w} ${b.h}" role="img" aria-label=${this.doc?.title ?? ""}
            @pointerdown=${this.onDown} @pointermove=${this.onMove} @pointerup=${this.onUp} @pointercancel=${this.onUp}
            @pointerleave=${() => { this.cursor = undefined; this.live?.setCursor(); }}
            @dblclick=${() => { if (this.tool === "room" && this.chain.length >= 3) this.closeRoom(); else if (this.tool === "wall") this.chain = []; else if (this.tool === "cable") this.finishCable(); }}
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
      </div>`}
      <input class="photo-file" type="file" accept="image/*" multiple hidden @change=${this.onPhotoFile}>
      ${this.editing ? "" : this.legend()}
      ${this.photoDialog()}`;
  }
}
customElements.define("phx-floorplan", PhxFloorplan);
