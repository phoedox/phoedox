// The whiteboard editor (ADR-0068): an own canvas with a hand-drawn look
// (Rough.js), drawn together live and published automatically. Shapes,
// arrows and lines bound to shapes (drag or click mode, round or sharp
// corners, editable points), text in shapes, sticky notes, frames, images,
// embeds (videos, code links, link cards, websites), entries, comment pins
// with text or handwriting, a laser pointer and export.

import { LitElement, css, html, nothing } from "lit";
import rough from "roughjs";
import { api, type Doc, type Entity } from "../api";
import { userColor } from "../collab";
import { uploadFile } from "../files";
import { formatDate, t } from "../i18n";
import { icon, iconSVG } from "../icon";
import { findKind, groupColor, kindName, loadModel, type Model } from "../model";
import { currentUser, navigate } from "../state";
import {
  BOXY, LINEAR, PALETTE, TEXTUAL, alignTo, backKey, bbox, bindTarget, bindingAt, boxOf, codeLabel, embedKind, extent, frontKey, hitTest, inFrame, linePts,
  newId, normalizeBoard, playerURL, removeElement, sorted, wrapText, type Binding, type Color, type ElementType, type Preview, type Pt, type WBComment, type WBElement, type Whiteboard,
  alignEls, cloneEls, fromExcalidraw, slides, templateEls, toExcalidraw, TEMPLATES, type Template, type Box, distributeEls, groupOf, inBox, shiftEl, snapBox, CLIP_PREFIX, type AlignHow,
} from "../whiteboard";
import { connectBoard, type Live, type Peer, type Status } from "../whiteboard-live";
import { base } from "./shared";
import { toast } from "./toasts";

type Tool = "select" | "hand" | "rect" | "ellipse" | "diamond" | "arrow" | "line" | "pen" | "text" | "sticky" | "frame" | "image" | "embed" | "entity" | "comment" | "laser" | "eraser";
const TOOLS: (readonly [Tool, string, string] | "|")[] = [
  ["select", "MousePointer2", "V"], ["hand", "Hand", "H"], "|",
  ["rect", "Square", "R"], ["ellipse", "Circle", "O"], ["diamond", "Diamond", "D"], ["arrow", "MoveUpRight", "A"], ["line", "Minus", "L"], ["pen", "Pencil", "P"], "|",
  ["text", "Type", "T"], ["sticky", "StickyNote", "N"], ["frame", "Frame", "F"], ["image", "Image", "I"], ["embed", "Globe", "W"], ["entity", "Waypoints", "E"], "|",
  ["comment", "MessageSquarePlus", "C"], ["laser", "Wand", "K"], ["eraser", "Eraser", "X"],
];
const VIEW_TOOLS = new Set<Tool>(["select", "hand", "laser"]);
const NS = "http://www.w3.org/2000/svg";
const XH = "http://www.w3.org/1999/xhtml";
const tc = (c?: string) => `var(--tc-${c || "gray"})`;
const tb = (c?: string) => `var(--tb-${c || "gray"})`;
const HAND = "var(--font-hand, var(--font-body))";
const reduced = () => matchMedia("(prefers-reduced-motion: reduce)").matches;

function mk<K extends keyof SVGElementTagNameMap>(tag: K, attrs: Record<string, string | number> = {}, parent?: Element): SVGElementTagNameMap[K] {
  const e = document.createElementNS(NS, tag);
  for (const k in attrs) e.setAttribute(k, String(attrs[k]));
  parent?.appendChild(e);
  return e;
}
/** HTML inside foreignObject, built without innerHTML for any user text. */
function hx(tag: string, style = "", children: (Node | string)[] = [], attrs: Record<string, string> = {}): HTMLElement {
  const e = document.createElementNS(XH, tag) as HTMLElement;
  if (style) e.setAttribute("style", style);
  for (const k in attrs) e.setAttribute(k, attrs[k]);
  for (const c of children) e.append(c);
  return e;
}
function ico(name: string, size = 14): HTMLElement {
  const s = hx("span", "display:inline-grid;place-items:center;flex:none");
  s.innerHTML = iconSVG(name, size); // our own icon markup, no user text
  return s;
}

interface Drag {
  mode: "pan" | "move" | "resize" | "point" | "pen" | "linedrag" | "create" | "erase" | "box" | "movemany";
  cx?: number; cy?: number; add?: string[];
  id?: string; sx?: number; sy?: number; vx?: number; vy?: number; lx?: number; ly?: number; moved?: boolean;
  h?: string; o?: { x: number; y: number; w: number; h: number }; i?: number; bind?: WBElement; ox?: number; oy?: number;
}
interface Pop { kind: "comment" | "newcomment" | "embed" | "entity"; x: number; y: number; id?: string; element?: string }

export class PhxWhiteboard extends LitElement {
  static properties = {
    project: {}, doc: { attribute: false }, canEdit: { type: Boolean },
    tool: { state: true }, selId: { state: true }, sel: { state: true }, snap: { state: true }, embedWeb: { state: true }, present: { state: true }, status: { state: true }, saved: { state: true }, peers: { state: true }, zoom: { state: true },
    pop: { state: true }, panel: { state: true }, arrowMode: { state: true }, roundDefault: { state: true }, trail: { state: true }, laserSize: { state: true },
    entities: { state: true }, query: { state: true }, busy: { state: true }, boardVersion: { state: true },
  };

  static styles = [base, css`
    :host { display: block; position: relative; height: calc(100dvh - var(--header-h) - 150px); min-height: 460px; }
    .stage { position: absolute; inset: 0; border: 1px solid var(--line); border-radius: 12px; overflow: hidden; background: var(--canvas, var(--bg)); touch-action: none; }
    svg.board { position: absolute; inset: 0; width: 100%; height: 100%; display: block; user-select: none; -webkit-user-select: none; outline: none; }
    svg.board.t-hand, svg.board.panning { cursor: grab; }
    svg.board.panning { cursor: grabbing; }
    svg.board.t-draw { cursor: crosshair; }
    svg.board.t-laser { cursor: crosshair; }
    svg.board.t-comment { cursor: cell; }
    .grid { fill: var(--line); }
    .bar { position: absolute; z-index: 3; display: flex; gap: 2px; align-items: center; padding: 4px; background: var(--surface); border: 1px solid var(--line); border-radius: 10px; box-shadow: 0 6px 20px -12px rgb(0 0 0 / 0.35); }
    .tools { top: 10px; left: 50%; transform: translateX(-50%); max-width: calc(100% - 20px); overflow-x: auto; }
    .bar button { position: relative; width: 34px; height: 34px; padding: 0; display: grid; place-items: center; border: 0; background: transparent; color: var(--fg); border-radius: 7px; }
    .bar button[aria-pressed="true"] { background: var(--accent-soft); color: var(--accent); }
    @media (hover: hover) and (pointer: fine) { .bar button:hover:not([aria-pressed="true"]) { background: var(--surface2); } }
    .bar button kbd { position: absolute; right: 2px; bottom: 1px; font: 9px var(--font-mono); color: var(--muted); }
    .sep { width: 1px; height: 22px; background: var(--line); margin: 0 3px; flex: none; }
    .opts { top: 60px; left: 50%; transform: translateX(-50%); gap: 6px; padding: 4px 8px; font-size: 12px; color: var(--muted); white-space: nowrap; }
    .opts .seg { display: inline-flex; border: 1px solid var(--line); border-radius: 7px; overflow: hidden; }
    .opts .seg button { width: auto; height: 26px; padding: 0 9px; border-radius: 0; font-size: 12px; }
    .opts .dot { display: block; border-radius: 50%; background: var(--laser, var(--bad)); }
    .zoom { left: 10px; bottom: 10px; }
    .zoom span:not(.sep) { min-width: 46px; text-align: center; font: 12px var(--font-mono); color: var(--muted); }
    .state { right: 10px; top: 10px; gap: 8px; padding: 4px 10px; font-size: 12px; color: var(--muted); }
    .state .live { width: 8px; height: 8px; border-radius: 50%; background: var(--good); }
    .state .live.off { background: var(--muted); }
    .state .live.saving { background: var(--signal); }
    .avatars { display: flex; }
    .avatars span { width: 22px; height: 22px; border-radius: 50%; margin-left: -6px; border: 2px solid var(--surface); display: grid; place-items: center; font: 600 9px var(--font-body); color: var(--on-color); }
    .side { right: 10px; bottom: 10px; }
    .count { position: absolute; top: 1px; right: 1px; min-width: 15px; height: 15px; border-radius: 8px; background: var(--accent); color: var(--on-color); font: 600 10px/15px var(--font-body); }
    .stylebar { position: absolute; z-index: 3; right: 10px; top: 56px; width: 212px; max-height: calc(100% - 120px); overflow: auto; padding: 10px; display: grid; gap: 8px;
      background: var(--surface); border: 1px solid var(--line); border-radius: 10px; box-shadow: 0 6px 20px -12px rgb(0 0 0 / 0.35); font-size: 12px; }
    .stylebar h4 { margin: 0; font: 600 11px var(--font-body); color: var(--muted); text-transform: uppercase; letter-spacing: 0.04em; }
    .swatches { display: flex; flex-wrap: wrap; gap: 4px; }
    .sw { width: 22px; height: 22px; padding: 0; border-radius: 6px; border: 1px solid var(--line); }
    .sw[aria-pressed="true"] { outline: 2px solid var(--accent); outline-offset: 1px; }
    .seg2 { display: flex; border: 1px solid var(--line); border-radius: 7px; overflow: hidden; }
    .seg2 button { flex: 1; height: 26px; padding: 0 6px; border: 0; border-radius: 0; background: var(--surface); color: var(--fg); font-size: 12px; }
    .seg2 button[aria-pressed="true"] { background: var(--accent-soft); color: var(--accent); }
    .row { display: flex; gap: 4px; flex-wrap: wrap; }
    .row button { height: 28px; padding: 0 8px; font-size: 12px; display: inline-flex; gap: 4px; align-items: center; }
    .editbox { position: absolute; z-index: 4; display: flex; align-items: center; justify-content: center; }
    .editbox.top { align-items: flex-start; justify-content: flex-start; }
    .editbox div { width: 100%; max-height: 100%; outline: none; text-align: center; font-family: var(--font-hand, var(--font-body)); line-height: 1.15; color: var(--fg); white-space: pre-wrap; word-break: break-word; caret-color: var(--accent); }
    .editbox.top div { text-align: left; }
    .pop { position: absolute; z-index: 5; width: 290px; padding: 12px; display: grid; gap: 8px; background: var(--surface); border: 1px solid var(--line); border-radius: 12px; box-shadow: 0 14px 34px -14px rgb(0 0 0 / 0.4);
      font-size: 13px; animation: rise var(--dur) var(--ease-out); }
    @keyframes rise { from { opacity: 0; transform: translateY(4px); } }
    @media (prefers-reduced-motion: reduce) { .pop { animation: none; } }
    .pop textarea, .pop input[type="url"], .pop input[type="search"] { width: 100%; box-sizing: border-box; }
    .pop .as { display: flex; align-items: center; justify-content: space-between; gap: 8px; font-size: 13px; color: var(--muted); }
    .pop textarea { min-height: 70px; resize: vertical; }
    .pop canvas { width: 100%; height: 110px; border: 1px dashed var(--line); border-radius: 8px; touch-action: none; background: var(--surface2); }
    .pop .who { display: flex; gap: 6px; align-items: center; font-size: 12px; color: var(--muted); }
    .pop .msg { white-space: pre-wrap; overflow-wrap: anywhere; }
    .pop .reply { border-left: 2px solid var(--line); padding-left: 8px; }
    .pop .acts { display: flex; gap: 6px; justify-content: flex-end; flex-wrap: wrap; }
    .pop ul { list-style: none; margin: 0; padding: 0; max-height: 200px; overflow: auto; display: grid; gap: 2px; }
    .pop li button { width: 100%; display: flex; gap: 8px; align-items: center; text-align: left; padding: 5px 6px; border: 0; background: transparent; color: var(--fg); border-radius: 6px; }
    @media (hover: hover) and (pointer: fine) { .pop li button:hover { background: var(--surface2); } }
    .pop .muted { color: var(--muted); font-size: 12px; }
    .panel { position: absolute; z-index: 4; right: 10px; top: 56px; bottom: 56px; width: min(300px, calc(100% - 20px)); display: flex; flex-direction: column; background: var(--surface); border: 1px solid var(--line); border-radius: 12px;
      box-shadow: 0 10px 28px -14px rgb(0 0 0 / 0.35); }
    .panel header { display: flex; justify-content: space-between; align-items: center; padding: 10px 12px; border-bottom: 1px solid var(--line); font-weight: 600; }
    .panel .list { flex: 1; overflow: auto; padding: 8px; display: grid; gap: 6px; align-content: start; }
    .panel .thread { padding: 8px; border: 1px solid var(--line); border-radius: 8px; font-size: 13px; display: grid; gap: 4px; text-align: left; background: var(--surface); color: var(--fg); }
    .panel .thread.done { opacity: 0.6; }
    .empty { position: absolute; inset: 0; display: grid; place-items: center; pointer-events: none; color: var(--muted); font-size: 14px; text-align: center; padding: 20px; }
    .empty .tpls { display: flex; flex-wrap: wrap; gap: 6px; justify-content: center; align-items: center; margin-top: 12px; pointer-events: auto; }
    .empty .tpls button { height: 30px; padding: 0 10px; font-size: 13px; }
    :host([presenting]) .bar:not(.present), :host([presenting]) .stylebar, :host([presenting]) .panel, :host([presenting]) .pop { display: none !important; }
    .bar.present { position: absolute; left: 50%; bottom: 16px; transform: translateX(-50%); display: flex; gap: 8px; align-items: center; font-size: 13px; }
    .bar.present span { padding: 0 6px; color: var(--muted); max-width: 40vw; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  `];

  project = "";
  doc?: Doc;
  canEdit = false;
  tool: Tool = "select";
  selId: string | null = null;
  /** Several selected elements (box, Shift-click, groups); selId is then null. */
  sel: string[] = [];
  snap = localStorage.getItem("wb.snap") === "1";
  /** Embed dialog: a live website instead of a preview card. */
  embedWeb = false;
  /** Presentation: index of the frame shown (-1 = off). */
  present = -1;
  status: Status = "connecting";
  saved: { state: "saving" | "saved" | "error"; at?: Date } | null = null;
  peers: Peer[] = [];
  zoom = 100;
  pop: Pop | null = null;
  panel = false;
  arrowMode: "drag" | "click" = (localStorage.getItem("wb.arrowMode") as "drag" | "click") || "drag";
  roundDefault = localStorage.getItem("wb.round") === "1";
  trail = localStorage.getItem("wb.trail") !== "dot";
  laserSize = Number(localStorage.getItem("wb.laserSize")) || 4.5;
  entities: Entity[] = [];
  query = "";
  busy = false;
  boardVersion = 0;

  private board: Whiteboard = { elements: [], comments: [] };
  private synced: Whiteboard = { elements: [], comments: [] };
  private live?: Live;
  private view = { x: 80, y: 80, z: 1 };
  private pen: { stroke: Color; smooth: boolean } = { stroke: "blue", smooth: false };
  private drag: Drag | null = null;
  private clickLine: string | null = null;
  /** Last copied elements, for paste without clipboard permission. */
  private clip = "";
  private hoverBind: string | null = null;
  private editing: string | null = null;
  private spaceDown = false;
  private model?: Model;
  private loaded = new Set<string>();
  private flushQueued = false;
  private painted = false;
  private lastDown = { t: 0, x: 0, y: 0, tool: "" };
  private resize?: ResizeObserver;
  private readOnly = true;

  // ---- lifecycle -------------------------------------------------------------

  connectedCallback() {
    super.connectedCallback();
    window.addEventListener("keydown", this.onKey);
    window.addEventListener("keyup", this.onKeyUp);
    window.addEventListener("paste", this.onPaste);
    document.addEventListener("fullscreenchange", this.onFullscreen);
    requestAnimationFrame(this.laserFrame);
  }

  disconnectedCallback() {
    super.disconnectedCallback();
    window.removeEventListener("keydown", this.onKey);
    window.removeEventListener("keyup", this.onKeyUp);
    window.removeEventListener("paste", this.onPaste);
    document.removeEventListener("fullscreenchange", this.onFullscreen);
    this.resize?.disconnect();
    void this.live?.destroy();
    this.live = undefined;
  }

  willUpdate(changed: Map<string, unknown>) {
    if (changed.has("doc") && this.doc && (changed.get("doc") as Doc | undefined)?.id !== this.doc.id) this.start();
  }

  firstUpdated() {
    this.resize = new ResizeObserver(() => this.paint());
    this.resize.observe(this.svgEl);
  }

  private get svgEl() {
    return this.renderRoot.querySelector("svg.board") as SVGSVGElement;
  }

  private start() {
    void this.live?.destroy();
    this.readOnly = !this.canEdit;
    this.tool = "select";
    this.board = normalizeBoard(this.doc!.content as Partial<Whiteboard>);
    this.synced = this.clone(this.board);
    this.painted = false;
    loadModel(this.project).then((m) => { this.model = m; this.paint(); }).catch(() => {});
    // Entries on the board show their name and kind.
    this.entities = [];
    if (this.board.elements.some((e) => e.entity)) void this.loadEntities();
    const u = currentUser.get();
    const proto = location.protocol === "https:" ? "wss:" : "ws:";
    this.live = connectBoard({
      url: `${proto}//${location.host}/rt/doc/${this.project}`, project: this.project, doc: this.doc!.id, initial: this.board, readOnly: this.readOnly,
      user: { name: u?.name ?? "?", color: userColor(u?.id ?? "") },
      onChange: (b) => this.remote(b),
      onPeers: (p) => { this.peers = p; this.paintOthers(); },
      onStatus: (s) => (this.status = s),
      onSaved: (state, at) => (this.saved = { state, at }),
      onReset: (by) => toast(by ? t("wb.changed_by", { name: by }) : t("wb.changed_outside")),
    });
    requestAnimationFrame(() => this.fit());
  }

  private clone(b: Whiteboard): Whiteboard {
    return JSON.parse(JSON.stringify(b)) as Whiteboard;
  }

  /** A change from someone else (or undo): adopt it, but keep what this
   * person is dragging right now. */
  private remote(b: Whiteboard) {
    const keep = this.drag?.id ? this.board.elements.find((e) => e.id === this.drag!.id) : undefined;
    this.synced = this.clone(b);
    if (keep) b.elements = b.elements.map((e) => (e.id === keep.id ? keep : e));
    this.board = b;
    if (this.selId && !this.byId(this.selId)) this.selId = null;
    this.boardVersion++;
    this.paint();
  }

  /** Records local changes: painted at once, sent to the others once per frame. */
  private changed() {
    this.boardVersion++;
    this.paint();
    if (this.flushQueued || this.readOnly) return;
    this.flushQueued = true;
    requestAnimationFrame(() => {
      this.flushQueued = false;
      const now = this.clone(this.board);
      this.live?.apply(this.synced, now);
      this.synced = now;
    });
  }

  // ---- helpers ---------------------------------------------------------------

  private byId = (id: string) => this.board.elements.find((e) => e.id === id);
  private els() {
    return this.board.elements;
  }
  private toWorld(cx: number, cy: number): Pt {
    const r = this.svgEl.getBoundingClientRect();
    return [(cx - r.left - this.view.x) / this.view.z, (cy - r.top - this.view.y) / this.view.z];
  }
  private toScreen(x: number, y: number): Pt {
    return [x * this.view.z + this.view.x, y * this.view.z + this.view.y];
  }
  private add(e: WBElement, back = false) {
    e.order = back ? backKey(this.els()) : frontKey(this.els());
    e.seed ??= 1 + Math.floor(Math.random() * 2 ** 30);
    this.board.elements.push(e);
  }
  private removeEl(id: string) {
    this.board.elements = removeElement(this.board.elements, id);
    this.board.comments = (this.board.comments ?? []).map((c) => (c.element === id ? { ...c, element: undefined } : c));
    if (this.selId === id) this.selId = null;
    this.changed();
  }
  private select(id: string | null) {
    this.selId = id;
    this.sel = id ? [id] : [];
    this.live?.setSelection(id ?? undefined);
    this.paint();
  }

  /** Selects several elements; one becomes the normal selection. */
  private selectMany(ids: string[]) {
    const uniq = [...new Set(ids)].filter((id) => this.byId(id));
    if (uniq.length <= 1) {
      this.select(uniq[0] ?? null);
      return;
    }
    this.selId = null;
    this.sel = uniq;
    this.live?.setSelection(uniq[0]);
    this.paint();
  }

  /** The selected elements (one or several). */
  private selection(): WBElement[] {
    return this.sel.map((id) => this.byId(id)).filter((e): e is WBElement => !!e);
  }

  /** With groups: clicking one element selects its whole group. */
  private withGroups(ids: string[]): string[] {
    const out = new Set<string>();
    for (const id of ids) {
      const e = this.byId(id);
      if (e) for (const g of groupOf(this.els(), e)) out.add(g.id);
    }
    return [...out];
  }

  private removeMany(ids: string[]) {
    for (const id of ids) {
      this.board.elements = removeElement(this.board.elements, id);
      this.board.comments = (this.board.comments ?? []).map((c) => (c.element === id ? { ...c, element: undefined } : c));
    }
    this.select(null);
    this.changed();
  }

  private copySelection(cut: boolean) {
    const sel = this.selection();
    if (!sel.length) return;
    const text = CLIP_PREFIX + JSON.stringify(sel);
    void navigator.clipboard?.writeText(text).catch(() => {});
    this.clip = text;
    if (cut && !this.readOnly) this.removeMany(sel.map((e) => e.id));
  }

  private pasteElements(json: string) {
    let list: WBElement[];
    try {
      list = JSON.parse(json) as WBElement[];
    } catch {
      return;
    }
    if (!Array.isArray(list) || !list.length) return;
    const copies = cloneEls(list.filter((e) => e && typeof e.id === "string" && typeof e.type === "string"), 24);
    for (const c of copies) this.add(c);
    this.selectMany(copies.map((c) => c.id));
    this.changed();
  }

  private alignSel(how: AlignHow | "dx" | "dy") {
    const sel = this.selection().filter((e) => !e.locked);
    if (how === "dx" || how === "dy") distributeEls(sel, how === "dx" ? "x" : "y", this.byId);
    else alignEls(sel, how, this.byId);
    this.changed();
  }

  private groupSel(on: boolean) {
    const sel = this.selection();
    if (on) {
      const g = newId();
      for (const e of sel) e.groups = [...(e.groups ?? []), g];
    } else {
      for (const e of sel) {
        e.groups = e.groups?.slice(0, -1);
        if (!e.groups?.length) delete e.groups;
      }
    }
    this.changed();
  }

  private setSnap(on: boolean) {
    this.snap = on;
    localStorage.setItem("wb.snap", on ? "1" : "0");
  }

  // ---- rendering -------------------------------------------------------------

  private paint() {
    const svg = this.svgEl;
    if (!svg) return;
    const world = svg.querySelector("g.world")!, layer = svg.querySelector("g.layer")!;
    world.setAttribute("transform", `translate(${this.view.x},${this.view.y}) scale(${this.view.z})`);
    const grid = svg.querySelector("pattern#wbgrid");
    grid?.setAttribute("patternTransform", `translate(${this.view.x % (24 * this.view.z)},${this.view.y % (24 * this.view.z)}) scale(${this.view.z})`);
    layer.replaceChildren(...sorted(this.els()).map((e) => this.drawEl(svg, e)));
    if (this.sel.length > 1) layer.append(this.drawMulti());
    const d = this.drag;
    if (d?.mode === "box") {
      const r = mk("rect", { x: Math.min(d.ox!, d.cx!), y: Math.min(d.oy!, d.cy!), width: Math.abs(d.cx! - d.ox!), height: Math.abs(d.cy! - d.oy!) });
      r.style.cssText = `fill:color-mix(in srgb, var(--accent) 8%, transparent);stroke:var(--accent);stroke-width:${1 / this.view.z};stroke-dasharray:${4 / this.view.z} ${3 / this.view.z}`;
      layer.append(r);
    }
    this.paintPins();
    this.paintOthers();
    this.zoom = Math.round(this.view.z * 100);
    if (!this.painted) this.painted = true;
  }

  private opts(e: WBElement) {
    const smooth = !!e.smooth;
    return {
      seed: e.seed || 1, roughness: smooth ? 0 : 1.1, bowing: smooth ? 0 : 1, stroke: tc(e.stroke), strokeWidth: e.width || 2,
      fill: e.fill ? tb(e.fill) : undefined, fillStyle: "solid", strokeLineDash: e.dash === "dashed" ? [10, 8] : e.dash === "dotted" ? [2, 6] : undefined,
      disableMultiStroke: smooth,
    };
  }

  /** Rough.js writes colours as attributes; CSS variables only work as styles. */
  private themed(g: SVGGElement): SVGGElement {
    g.querySelectorAll("path").forEach((p) => {
      for (const a of ["stroke", "fill"] as const) {
        const v = p.getAttribute(a);
        if (v?.startsWith("var(")) {
          p.style[a] = v;
          p.removeAttribute(a);
        }
      }
    });
    return g;
  }

  private textBlock(g: Element, text: string | undefined, cx: number, cy: number, maxW: number, o: { size?: number; color?: string; anchor?: string; top?: boolean; font?: string } = {}) {
    if (!text) return;
    const size = o.size ?? 20, lines = wrapText(text, maxW, size), lh = size * 1.15;
    const y0 = o.top ? cy : cy - ((lines.length - 1) * lh) / 2;
    const tx = mk("text", { x: cx, y: y0, "text-anchor": o.anchor ?? "middle", "dominant-baseline": o.top ? "hanging" : "central" }, g);
    tx.style.font = `${size}px/1.15 ${o.font ?? HAND}`;
    tx.style.fill = o.color ?? "var(--fg)";
    lines.forEach((l, i) => {
      const s = mk("tspan", { x: cx, dy: i ? lh : 0 }, tx);
      s.textContent = l;
    });
  }

  private drawEl(svg: SVGSVGElement, e: WBElement): SVGGElement {
    const rc = rough.svg(svg);
    const g = mk("g", { "data-id": e.id });
    if (e.opacity && e.opacity < 1) g.style.opacity = String(e.opacity);
    const x = e.x ?? 0, y = e.y ?? 0, w = e.w ?? 0, h = e.h ?? 0, size = e.fontSize || 20;
    switch (e.type) {
      case "frame": {
        const r = mk("rect", { x, y, width: w, height: h, rx: 14 }, g);
        r.style.cssText = "fill:none;stroke:var(--line);stroke-width:1.5;stroke-dasharray:6 6";
        const tx = mk("text", { x: x + 12, y: y + 18 }, g);
        tx.textContent = e.text || t("wb.frame");
        tx.style.cssText = "font:600 13px var(--font-body);fill:var(--muted)";
        break;
      }
      case "rect":
        g.appendChild(this.themed(rc.rectangle(x, y, w, h, this.opts(e))));
        this.textBlock(g, e.text, x + w / 2, y + h / 2, w - 16, { size });
        break;
      case "ellipse":
        g.appendChild(this.themed(rc.ellipse(x + w / 2, y + h / 2, w, h, this.opts(e))));
        this.textBlock(g, e.text, x + w / 2, y + h / 2, w * 0.7, { size });
        break;
      case "diamond": {
        const cx = x + w / 2, cy = y + h / 2;
        g.appendChild(this.themed(rc.polygon([[cx, y], [x + w, cy], [cx, y + h], [x, cy]], this.opts(e))));
        this.textBlock(g, e.text, cx, cy, w * 0.55, { size });
        break;
      }
      case "sticky": {
        const r = mk("rect", { x, y, width: w, height: h, rx: 4 }, g);
        r.style.cssText = `fill:${tb(e.fill || "yellow")};filter:drop-shadow(0 2px 3px rgb(0 0 0 / .14))`;
        this.textBlock(g, e.text, x + 12, y + 12, w - 24, { anchor: "start", top: true, size: e.fontSize || 18 });
        break;
      }
      case "text": {
        const anchor = e.align === "center" ? "middle" : e.align === "right" ? "end" : "start";
        const ax = e.align === "center" ? x + w / 2 : e.align === "right" ? x + w : x;
        this.textBlock(g, e.text, ax, y, Math.max(w, 60), { anchor, top: true, size: e.fontSize || 24, color: tc(e.stroke) });
        break;
      }
      case "entity": this.drawEntity(rc, e, g); break;
      case "image": {
        const r = mk("rect", { x, y, width: w, height: h, rx: 6 }, g);
        r.style.cssText = "fill:var(--surface2)";
        mk("image", { href: `/api/v1/files/${encodeURIComponent(e.file ?? "")}`, x, y, width: w, height: h, preserveAspectRatio: "xMidYMid meet" }, g);
        break;
      }
      case "embed": this.drawEmbed(e, g); break;
      case "arrow":
      case "line": {
        const pts = linePts(e, this.byId);
        if (pts.length < 2) break;
        const o = { ...this.opts(e), fill: undefined };
        g.appendChild(this.themed(pts.length === 2 ? rc.line(pts[0][0], pts[0][1], pts[1][0], pts[1][1], o) : e.round ? rc.curve(pts, o) : rc.linearPath(pts, o)));
        if (e.type === "arrow") {
          const n = pts.length;
          const a = e.round && n > 2 ? [pts[n - 1][0] - (pts[n - 1][0] - pts[n - 2][0]) * 0.25, pts[n - 1][1] - (pts[n - 1][1] - pts[n - 2][1]) * 0.25] : pts[n - 2];
          const p2 = pts[n - 1], ang = Math.atan2(p2[1] - a[1], p2[0] - a[0]), L = 14;
          g.appendChild(this.themed(rc.linearPath([[p2[0] - L * Math.cos(ang - 0.45), p2[1] - L * Math.sin(ang - 0.45)], p2, [p2[0] - L * Math.cos(ang + 0.45), p2[1] - L * Math.sin(ang + 0.45)]], o)));
        }
        if (e.text && this.editing !== e.id) {
          const k = Math.floor((pts.length - 1) / 2), m = [(pts[k][0] + pts[k + 1][0]) / 2, (pts[k][1] + pts[k + 1][1]) / 2];
          const lines = wrapText(e.text, 240, 18), wd = Math.min(250, Math.max(...lines.map((l) => l.length)) * 9.4 + 14);
          const bg = mk("rect", { x: m[0] - wd / 2, y: m[1] - (lines.length * 20.7) / 2 - 4, width: wd, height: lines.length * 20.7 + 8, rx: 6 }, g);
          bg.style.fill = "var(--canvas, var(--bg))";
          this.textBlock(g, e.text, m[0], m[1], 240, { size: 18, color: "var(--muted)" });
        }
        // A wide invisible stroke makes thin lines easy to grab.
        const hitPath = mk("polyline", { points: pts.map((p) => p.join(",")).join(" ") }, g);
        hitPath.style.cssText = `fill:none;stroke:transparent;stroke-width:${14 / this.view.z}`;
        break;
      }
      case "pen": {
        const pts = e.points ?? [];
        if (!pts.length) break;
        let d = `M${pts[0][0]},${pts[0][1]}`;
        for (let i = 1; i < pts.length - 1; i++) d += ` Q${pts[i][0]},${pts[i][1]} ${(pts[i][0] + pts[i + 1][0]) / 2},${(pts[i][1] + pts[i + 1][1]) / 2}`;
        if (pts.length > 1) d += ` L${pts[pts.length - 1][0]},${pts[pts.length - 1][1]}`;
        const p = mk("path", { d }, g);
        p.style.cssText = `fill:none;stroke:${tc(e.stroke)};stroke-width:${(e.width || 2) + 1.5};stroke-linecap:round;stroke-linejoin:round`;
        break;
      }
    }
    if (this.editing === e.id) g.querySelectorAll("text").forEach((t) => t.remove());
    if (this.hoverBind === e.id) {
      const r = mk("rect", { x: x - 6, y: y - 6, width: w + 12, height: h + 12, rx: 8 });
      r.style.cssText = `fill:var(--accent-soft);opacity:.45;stroke:var(--accent);stroke-width:${2 / this.view.z}`;
      g.insertBefore(r, g.firstChild);
    }
    if (this.selId === e.id) this.drawSelection(e, g);
    // Others' selections.
    for (const p of this.peers) {
      if (p.selection !== e.id) continue;
      const b = bbox(e, this.byId), pad = 9;
      const r = mk("rect", { x: b.x - pad, y: b.y - pad, width: b.w + 2 * pad, height: b.h + 2 * pad, rx: 6 }, g);
      r.style.cssText = `fill:none;stroke:${p.color};stroke-width:${1.5 / this.view.z};stroke-dasharray:${3 / this.view.z} ${3 / this.view.z}`;
    }
    return g;
  }

  private drawEntity(rc: ReturnType<typeof rough.svg>, e: WBElement, g: SVGGElement) {
    const x = e.x ?? 0, y = e.y ?? 0, w = e.w ?? 0, h = e.h ?? 0;
    const ent = this.entities.find((x) => x.slug === e.entity);
    const k = findKind(this.model, ent?.kind ?? "");
    g.appendChild(this.themed(rc.rectangle(x, y, w, h, { ...this.opts(e), fill: "var(--surface)", fillStyle: "solid" })));
    const fo = mk("foreignObject", { x, y, width: w, height: h }, g);
    const badge = hx("div", `width:38px;height:38px;flex:none;border-radius:10px;display:grid;place-items:center;background:${groupColor[k?.group ?? ""] ?? "var(--accent)"};color:var(--surface)`, [ico(k?.icon ?? "Waypoints", 20)]);
    const name = hx("div", "font:600 16px var(--font-body);white-space:nowrap;overflow:hidden;text-overflow:ellipsis", [e.text || ent?.name || e.entity || ""]);
    const sub = hx("div", "font:12px var(--font-mono);color:var(--muted);white-space:nowrap;overflow:hidden;text-overflow:ellipsis", [`@${e.entity ?? ""}${ent ? " · " + kindName(k, ent.kind) : ""}`]);
    fo.append(hx("div", "box-sizing:border-box;width:100%;height:100%;display:flex;gap:10px;align-items:center;padding:0 12px;color:var(--fg)", [badge, hx("div", "min-width:0;display:grid;gap:2px", [name, sub])]));
  }

  /** Embeds load nothing from third parties before a click (privacy, like
   * embeds in pages); code links and link cards show a preview the server
   * fetched. */
  private drawEmbed(e: WBElement, g: SVGGElement) {
    const x = e.x ?? 0, y = e.y ?? 0, w = e.w ?? 0, h = e.h ?? 0, pv: Preview = e.preview ?? {};
    const fo = mk("foreignObject", { x, y, width: w, height: h }, g);
    const card = (...kids: Node[]) => hx("div", "box-sizing:border-box;width:100%;height:100%;border:1px solid var(--line);border-radius:10px;background:var(--surface);overflow:hidden;display:flex;flex-direction:column;font:13px/1.35 var(--font-body);color:var(--fg)", kids);
    const foot = (iconName: string, text: string) => {
      const a = hx("a", "color:var(--muted);display:flex;gap:6px;align-items:center;min-width:0;text-decoration:none", [ico(iconName, 12), hx("span", "overflow:hidden;text-overflow:ellipsis;white-space:nowrap", [text])], { href: e.url ?? "#", target: "_blank", rel: "noopener noreferrer", "data-link": "1" });
      return hx("div", "display:flex;gap:6px;align-items:center;padding:6px 10px;border-top:1px solid var(--line);font-size:11px;white-space:nowrap;overflow:hidden", [a]);
    };
    const title = (s: string) => hx("div", "font-weight:600;white-space:nowrap;overflow:hidden;text-overflow:ellipsis", [s]);
    const loaded = this.loaded.has(e.id);
    if (e.kind === "video") {
      const player = playerURL(e.url ?? "");
      let body: HTMLElement;
      if (loaded && player) {
        body = hx("iframe", "flex:1;border:0;width:100%", [], { src: player, allow: "autoplay; encrypted-media; picture-in-picture; fullscreen", allowfullscreen: "", sandbox: "allow-scripts allow-same-origin allow-presentation allow-popups", referrerpolicy: "strict-origin-when-cross-origin", title: pv.title ?? "" });
      } else {
        body = hx("div", "flex:1;position:relative;cursor:pointer;background:linear-gradient(135deg,var(--tb-red),var(--tb-orange));display:grid;place-items:center", [
          hx("div", "width:60px;height:42px;border-radius:12px;background:var(--tc-red);color:var(--on-color);display:grid;place-items:center", [ico("Play", 22)]),
          hx("span", "position:absolute;left:10px;bottom:8px;right:10px;font-size:11px;background:var(--surface);border-radius:6px;padding:3px 7px;width:fit-content", [player ? t("wb.click_to_load") : t("wb.open_video")]),
        ], { "data-load": e.id });
      }
      fo.append(card(body, hx("div", "padding:6px 10px 0", [title(pv.title || e.url || "")]), foot("Video", pv.site || "")));
      return;
    }
    if (e.kind === "code") {
      const st = (pv.status ?? "").toLowerCase();
      const color = /merg/.test(st) ? "var(--tc-purple)" : /clos/.test(st) ? "var(--tc-red)" : "var(--good)";
      fo.append(card(hx("div", "flex:1;padding:10px 12px;display:grid;gap:6px;align-content:start", [
        hx("div", "display:flex;gap:8px;align-items:center;color:var(--muted);font:12px var(--font-mono)", [ico("GitPullRequest", 15), codeLabel(e.url ?? "")]),
        hx("div", "font-weight:600;font-size:14px;display:-webkit-box;-webkit-line-clamp:2;-webkit-box-orient:vertical;overflow:hidden", [pv.title || e.url || ""]),
        ...(st ? [hx("span", `width:fit-content;display:inline-flex;gap:4px;align-items:center;padding:1px 8px;border-radius:999px;background:var(--surface2);color:${color};font-size:12px`, [ico("Circle", 10), pv.status ?? ""])] : []),
      ]), foot("GitCommitHorizontal", pv.site || "")));
      return;
    }
    if (e.kind === "web") {
      const bar = hx("div", "display:flex;gap:6px;align-items:center;padding:6px 10px;border-bottom:1px solid var(--line);background:var(--surface2);font:11px var(--font-mono);color:var(--muted)", [
        hx("span", "flex:1;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;background:var(--surface);border-radius:6px;padding:2px 8px", [e.url ?? ""]), ico("Lock", 11)]);
      const body = loaded
        ? hx("iframe", "flex:1;border:0;width:100%;background:#fff", [], { src: e.url ?? "", sandbox: "allow-scripts allow-same-origin allow-forms allow-popups", referrerpolicy: "no-referrer", title: pv.title ?? "" })
        : hx("div", "flex:1;display:grid;place-items:center;align-content:center;gap:6px;text-align:center;padding:12px;cursor:pointer;color:var(--muted);font-size:12px", [ico("Globe", 22), hx("b", "color:var(--fg)", [pv.title || t("wb.website")]), t("wb.click_to_load_site")], { "data-load": e.id });
      fo.append(card(bar, body));
      return;
    }
    fo.append(card(hx("div", "flex:1;display:flex;gap:10px;padding:10px 12px;min-height:0", [
      hx("div", "width:48px;height:48px;flex:none;border-radius:8px;background:var(--tb-blue);color:var(--tc-blue);display:grid;place-items:center", [ico("BookOpen", 20)]),
      hx("div", "min-width:0;display:grid;gap:2px;align-content:start", [title(pv.title || e.url || ""),
        hx("div", "color:var(--muted);font-size:12px;display:-webkit-box;-webkit-line-clamp:2;-webkit-box-orient:vertical;overflow:hidden", [pv.description ?? ""])]),
    ]), foot("Link", pv.site || "")));
  }

  private handle(g: SVGGElement, x: number, y: number, attrs: Record<string, string | number>, round = false) {
    const r = 5 / this.view.z;
    const h = round ? mk("circle", { cx: x, cy: y, r: r * 1.15, ...attrs }, g) : mk("rect", { x: x - r, y: y - r, width: 2 * r, height: 2 * r, rx: 1.5 / this.view.z, ...attrs }, g);
    h.style.cssText = `fill:var(--surface);stroke:var(--accent);stroke-width:${1.5 / this.view.z};cursor:${attrs.cursor ?? "pointer"}`;
    return h;
  }

  private drawSelection(e: WBElement, g: SVGGElement) {
    if (this.readOnly) {
      const b = bbox(e, this.byId), pad = 6;
      const r = mk("rect", { x: b.x - pad, y: b.y - pad, width: b.w + 2 * pad, height: b.h + 2 * pad, rx: 4 }, g);
      r.style.cssText = `fill:none;stroke:var(--accent);stroke-width:${1.5 / this.view.z};stroke-dasharray:${4 / this.view.z} ${3 / this.view.z}`;
      return;
    }
    if (LINEAR.has(e.type)) {
      const pts = linePts(e, this.byId);
      // "+" between two points inserts one; points can be dragged.
      for (let i = 1; i < pts.length; i++) {
        const m = [(pts[i - 1][0] + pts[i][0]) / 2, (pts[i - 1][1] + pts[i][1]) / 2];
        const c = mk("g", { "data-mid": i, style: "cursor:copy" }, g);
        const cc = mk("circle", { cx: m[0], cy: m[1], r: 7 / this.view.z }, c);
        cc.style.cssText = `fill:var(--accent-soft);stroke:var(--accent);stroke-width:${1 / this.view.z};opacity:.9`;
        const tx = mk("text", { x: m[0], y: m[1], "text-anchor": "middle", "dominant-baseline": "central" }, c);
        tx.textContent = "+";
        tx.style.cssText = `font:600 ${11 / this.view.z}px var(--font-mono);fill:var(--accent);pointer-events:none`;
      }
      pts.forEach((p, i) => {
        const b = i === 0 ? e.start : i === pts.length - 1 ? e.end : undefined;
        const h = this.handle(g, p[0], p[1], { "data-pt": i, cursor: "move" }, true);
        if (b) h.style.fill = "var(--accent)";
      });
      return;
    }
    const b = bbox(e, this.byId), pad = 6;
    const r = mk("rect", { x: b.x - pad, y: b.y - pad, width: b.w + 2 * pad, height: b.h + 2 * pad, rx: 4 }, g);
    r.style.cssText = `fill:none;stroke:var(--accent);stroke-width:${1.5 / this.view.z};stroke-dasharray:${4 / this.view.z} ${3 / this.view.z}`;
    if (!BOXY.has(e.type) || e.locked) return;
    const X = [b.x - pad, b.x + b.w / 2, b.x + b.w + pad], Y = [b.y - pad, b.y + b.h / 2, b.y + b.h + pad];
    const H: Record<string, [number, number, string]> = { nw: [0, 0, "nwse-resize"], n: [1, 0, "ns-resize"], ne: [2, 0, "nesw-resize"], e: [2, 1, "ew-resize"], se: [2, 2, "nwse-resize"], s: [1, 2, "ns-resize"], sw: [0, 2, "nesw-resize"], w: [0, 1, "ew-resize"] };
    for (const k in H) this.handle(g, X[H[k][0]], Y[H[k][1]], { "data-h": k, cursor: H[k][2] });
  }

  /** Outline of several selected elements and their common bounds. */
  private drawMulti(): SVGGElement {
    const g = mk("g", { "data-multi": "1" });
    const boxes = this.selection().map((e) => bbox(e, this.byId)), pad = 6;
    for (const b of boxes) {
      const r = mk("rect", { x: b.x - 3, y: b.y - 3, width: b.w + 6, height: b.h + 6, rx: 3 }, g);
      r.style.cssText = `fill:none;stroke:var(--accent);stroke-width:${1 / this.view.z};opacity:.6`;
    }
    const x = Math.min(...boxes.map((b) => b.x)) - pad, y = Math.min(...boxes.map((b) => b.y)) - pad;
    const w = Math.max(...boxes.map((b) => b.x + b.w)) + pad - x, h = Math.max(...boxes.map((b) => b.y + b.h)) + pad - y;
    const r = mk("rect", { x, y, width: w, height: h, rx: 4 }, g);
    r.style.cssText = `fill:none;stroke:var(--accent);stroke-width:${1.5 / this.view.z};stroke-dasharray:${4 / this.view.z} ${3 / this.view.z}`;
    return g;
  }

  private openComments() {
    return (this.board.comments ?? []).filter((c) => !c.resolved);
  }

  private paintPins() {
    const pins = this.svgEl?.querySelector("g.pins");
    if (!pins) return;
    pins.replaceChildren();
    this.openComments().forEach((c, i) => {
      const g = mk("g", { transform: `translate(${c.x},${c.y})`, "data-pin": c.id, style: "cursor:pointer" }, pins);
      const inner = mk("g", { transform: `scale(${Math.min(1.6, Math.max(0.6, 1 / this.view.z))})` }, g);
      const path = mk("path", { d: "M0,0 L2,-14 A13,13 0 1 1 14,-2 Z" }, inner);
      path.style.cssText = `fill:${userColor(c.author ?? "")};stroke:var(--surface);stroke-width:2`;
      const tx = mk("text", { x: 14, y: -14, "text-anchor": "middle", "dominant-baseline": "central" }, inner);
      tx.textContent = String(i + 1);
      tx.style.cssText = "font:600 12px var(--font-mono);fill:var(--on-color);pointer-events:none";
    });
  }

  private paintOthers() {
    const layer = this.svgEl?.querySelector("g.others");
    if (!layer) return;
    layer.replaceChildren();
    for (const p of this.peers) {
      if (!p.cursor) continue;
      const g = mk("g", { transform: `translate(${p.cursor[0]},${p.cursor[1]}) scale(${1 / this.view.z})` }, layer);
      const arrow = mk("path", { d: "M0,0 L0,17 L4.5,13 L7.5,20 L10,19 L7,12 L13,12 Z" }, g);
      arrow.style.cssText = `fill:${p.color};stroke:var(--surface);stroke-width:1.2`;
      const label = mk("g", { transform: "translate(12,20)" }, g);
      const tw = p.name.length * 7 + 12;
      const bg = mk("rect", { width: tw, height: 18, rx: 9 }, label);
      bg.style.fill = p.color;
      const tx = mk("text", { x: 6, y: 13 }, label);
      tx.textContent = p.name;
      tx.style.cssText = "font:600 11px var(--font-body);fill:var(--on-color)";
    }
  }

  // ---- view --------------------------------------------------------------------

  private zoomAt(cx: number, cy: number, f: number) {
    const r = this.svgEl.getBoundingClientRect(), z = Math.min(4, Math.max(0.15, this.view.z * f));
    const wx = (cx - r.left - this.view.x) / this.view.z, wy = (cy - r.top - this.view.y) / this.view.z;
    this.view = { z, x: cx - r.left - wx * z, y: cy - r.top - wy * z };
    this.paint();
    this.placeEditor();
  }

  private zoomBy(f: number) {
    const r = this.svgEl.getBoundingClientRect();
    this.zoomAt(r.left + r.width / 2, r.top + r.height / 2, f);
  }

  fit() {
    const r = this.svgEl?.getBoundingClientRect();
    if (!r?.width) return;
    const b = extent(this.els(), this.byId);
    if (!b) {
      this.view = { x: r.width / 2, y: r.height / 2, z: 1 };
    } else {
      const z = Math.min(1.3, Math.max(0.2, Math.min((r.width - 120) / Math.max(b.w, 1), (r.height - 160) / Math.max(b.h, 1))));
      this.view = { z, x: (r.width - b.w * z) / 2 - b.x * z, y: (r.height - b.h * z) / 2 - b.y * z + 20 };
    }
    this.paint();
  }

  /** Shows a box as large as possible. */
  private fitBox(b: Box, pad = 40) {
    const r = this.svgEl?.getBoundingClientRect();
    if (!r?.width) return;
    const z = Math.min(4, Math.max(0.1, Math.min((r.width - 2 * pad) / Math.max(b.w, 1), (r.height - 2 * pad) / Math.max(b.h, 1))));
    this.view = { z, x: (r.width - b.w * z) / 2 - b.x * z, y: (r.height - b.h * z) / 2 - b.y * z };
    this.paint();
  }

  /** Presentation: the frames are slides, shown one after the other. */
  private presentAt(i: number) {
    const list = slides(this.els());
    if (!list.length) {
      toast(t("wb.present_none"), "error");
      return;
    }
    if (i < 0 || i >= list.length) {
      this.stopPresenting();
      return;
    }
    if (this.present < 0) {
      this.select(null);
      this.pop = null;
      this.panel = false;
      void this.requestFullscreen?.().catch(() => {});
    }
    this.present = i;
    requestAnimationFrame(() => this.fitBox(boxOf(list[i]), 24));
  }

  /** Leaving full screen (Esc in the browser) ends the presentation too. */
  private onFullscreen = () => {
    if (!document.fullscreenElement && this.present >= 0) {
      this.present = -1;
      requestAnimationFrame(() => this.fit());
    } else if (this.present >= 0) {
      requestAnimationFrame(() => this.presentAt(this.present));
    }
  };

  private stopPresenting() {
    this.present = -1;
    if (document.fullscreenElement) void document.exitFullscreen().catch(() => {});
    requestAnimationFrame(() => this.fit());
  }

  private useTemplate(kind: Template) {
    const L: Record<string, string> = {};
    for (const k of ["well", "improve", "actions", "topic", "idea", "ap", "activity", "person", "system", "web", "db"]) L[k] = t(`wb.tpl.${k}` as any);
    for (const e of templateEls(kind, L)) this.add(e, e.type === "frame");
    this.changed();
    requestAnimationFrame(() => this.fit());
  }

  private exportExcalidraw() {
    const blob = new Blob([toExcalidraw(this.board)], { type: "application/json" });
    const a = document.createElement("a");
    a.href = URL.createObjectURL(blob);
    a.download = `${this.doc?.slug ?? "whiteboard"}.excalidraw`;
    a.click();
    URL.revokeObjectURL(a.href);
  }

  private async importExcalidraw(f: File) {
    try {
      const els = fromExcalidraw(await f.text());
      // Placed next to what is already there.
      const b = extent(this.els(), this.byId);
      const dx = b ? b.x + b.w + 80 : 0;
      const minX = Math.min(...els.map((e) => bbox(e, (id) => els.find((x) => x.id === id)).x));
      for (const e of els) {
        shiftEl(e, dx - (Number.isFinite(minX) ? minX : 0), 0);
        this.add(e, e.type === "frame");
      }
      this.changed();
      this.selectMany(els.map((e) => e.id));
      requestAnimationFrame(() => this.fit());
      toast(t("wb.imported", { count: els.length }));
    } catch {
      toast(t("wb.import_failed"), "error");
    }
  }

  private pickExcalidraw() {
    const input = document.createElement("input");
    input.type = "file";
    input.accept = ".excalidraw,application/json";
    input.onchange = () => input.files?.[0] && void this.importExcalidraw(input.files[0]);
    input.click();
  }

  // ---- tools --------------------------------------------------------------------

  private setTool(tool: Tool) {
    if (this.readOnly && !VIEW_TOOLS.has(tool)) return;
    this.finishClickLine();
    this.tool = tool;
    this.pop = null;
    if (tool !== "select") this.select(null);
    if (tool === "image") this.pickImage();
  }

  private setArrowMode(m: "drag" | "click") {
    this.arrowMode = m;
    try { localStorage.setItem("wb.arrowMode", m); } catch { /* private mode */ }
  }

  private finishClickLine(keepPreview = false) {
    if (!this.clickLine) return;
    const e = this.byId(this.clickLine);
    this.clickLine = null;
    this.hoverBind = null;
    if (!e) return;
    if (!keepPreview) e.points!.pop();
    const pts = e.points!;
    if (pts.length < 2 || (pts.length === 2 && Math.hypot(pts[1][0] - pts[0][0], pts[1][1] - pts[0][1]) < 4 && !e.end)) {
      this.board.elements = this.els().filter((x) => x.id !== e.id);
    } else this.selId = e.id;
    this.changed();
  }

  private clickPoint(x: number, y: number, ev: PointerEvent) {
    const ctrl = ev.ctrlKey || ev.metaKey, tol = 10 / this.view.z;
    if (!this.clickLine) {
      const s = bindTarget(this.els(), x, y, tol);
      const e: WBElement = { id: newId(), type: this.tool as ElementType, stroke: this.tool === "arrow" ? "gray" : this.pen.stroke, smooth: this.pen.smooth, round: this.roundDefault, points: [[x, y], [x, y]] };
      if (s) e.start = bindingAt(s, x, y, 14 / this.view.z);
      this.add(e);
      this.clickLine = e.id;
      this.changed();
      return;
    }
    const e = this.byId(this.clickLine)!, n = e.points!.length, p = alignTo([x, y], e.points![n - 2], ctrl);
    e.points![n - 1] = p;
    if (ev.shiftKey) {
      e.points!.push([p[0], p[1]]); // a waypoint; the line goes on
      this.changed();
      return;
    }
    const s = bindTarget(this.els(), p[0], p[1], tol, e.start?.element);
    if (s) e.end = bindingAt(s, p[0], p[1], 14 / this.view.z);
    this.finishClickLine(true);
    this.tool = "select";
    this.select(e.id);
  }

  // ---- pointer -------------------------------------------------------------------

  private isDouble(ev: PointerEvent) {
    const now = performance.now(), d = Math.hypot(ev.clientX - this.lastDown.x, ev.clientY - this.lastDown.y);
    const dbl = this.lastDown.tool === this.tool && now - this.lastDown.t < 380 && d < 8;
    this.lastDown = dbl ? { t: 0, x: 0, y: 0, tool: "" } : { t: now, x: ev.clientX, y: ev.clientY, tool: this.tool };
    return dbl;
  }

  private onDown = (ev: PointerEvent) => {
    const svg = this.svgEl;
    if (this.present >= 0) {
      if (ev.button === 0) this.presentAt(this.present + 1);
      return;
    }
    if (this.editing) {
      (this.renderRoot.querySelector(".editbox div") as HTMLElement | null)?.blur();
      return;
    }
    const target = ev.composedPath()[0] as Element;
    if (target.closest?.("[data-link]")) return; // links in embed cards open normally
    if (ev.button === 0 && this.isDouble(ev) && (this.tool === "select" || this.clickLine)) {
      ev.preventDefault();
      this.drag = null;
      this.onDouble(ev, target);
      return;
    }
    svg.setPointerCapture(ev.pointerId);
    const [x, y] = this.toWorld(ev.clientX, ev.clientY);
    const pin = target.closest?.("[data-pin]");
    if (pin) {
      const c = (this.board.comments ?? []).find((n) => n.id === (pin as HTMLElement).dataset.pin);
      if (c) this.pop = { kind: "comment", x: c.x, y: c.y, id: c.id };
      return;
    }
    this.pop = null;
    if (this.tool === "hand" || this.spaceDown || ev.button === 1) {
      this.drag = { mode: "pan", sx: ev.clientX, sy: ev.clientY, vx: this.view.x, vy: this.view.y };
      svg.classList.add("panning");
      return;
    }
    if (this.tool === "laser") {
      this.laserDown = true;
      this.laserStart("me");
      this.laserMove(ev.clientX, ev.clientY, "me");
      return;
    }
    const load = target.closest?.("[data-load]") as HTMLElement | null;
    if (load && this.tool === "select") {
      const e = this.byId(load.dataset.load!);
      if (e) {
        if (e.kind === "video" && !playerURL(e.url ?? "")) window.open(e.url, "_blank", "noopener");
        else this.loaded.add(e.id);
        this.select(e.id);
      }
      return;
    }
    if (this.readOnly) {
      const h = hitTest(this.els(), x, y, 8 / this.view.z, this.byId);
      this.select(h?.id ?? null);
      this.drag = { mode: "pan", sx: ev.clientX, sy: ev.clientY, vx: this.view.x, vy: this.view.y };
      return;
    }
    if (this.tool === "comment") {
      const h = hitTest(this.els(), x, y, 8 / this.view.z, this.byId);
      this.pop = { kind: "newcomment", x, y, element: h && h.type !== "frame" ? h.id : undefined };
      return;
    }
    if (this.tool === "embed") {
      this.embedWeb = false;
      this.pop = { kind: "embed", x, y };
      return;
    }
    if (this.tool === "entity") {
      this.pop = { kind: "entity", x, y };
      void this.loadEntities();
      return;
    }
    if (this.tool === "eraser") {
      const h = hitTest(this.els(), x, y, 8 / this.view.z, this.byId);
      if (h && h.type !== "frame") this.removeEl(h.id);
      this.drag = { mode: "erase" };
      return;
    }
    if (LINEAR.has(this.tool as ElementType) && this.arrowMode === "click") {
      this.clickPoint(x, y, ev);
      return;
    }
    if (this.tool === "select") {
      const e = this.selId ? this.byId(this.selId) : undefined;
      const hEl = target.closest?.("[data-h]") as HTMLElement | null, pEl = target.closest?.("[data-pt]") as HTMLElement | null, mEl = target.closest?.("[data-mid]") as HTMLElement | null;
      if (e && hEl) {
        this.drag = { mode: "resize", id: e.id, h: hEl.dataset.h, o: boxOf(e), sx: x, sy: y };
        return;
      }
      if (e && pEl) {
        this.drag = { mode: "point", id: e.id, i: Number(pEl.dataset.pt) };
        return;
      }
      if (e && mEl) {
        const i = Number(mEl.dataset.mid), pts = linePts(e, this.byId);
        e.points = e.points!.map((p, k) => (k === 0 || k === e.points!.length - 1 ? pts[k] : p));
        e.points.splice(i, 0, [x, y]);
        this.drag = { mode: "point", id: e.id, i };
        this.changed();
        return;
      }
      const h = hitTest(this.els(), x, y, 8 / this.view.z, this.byId);
      if (!h) {
        // Empty space: draw a box to select what lies in it.
        if (!ev.shiftKey) this.select(null);
        this.drag = { mode: "box", ox: x, oy: y, cx: x, cy: y, add: ev.shiftKey ? [...this.sel] : [] };
        return;
      }
      const group = this.withGroups([h.id]);
      if (ev.shiftKey) {
        const cur = new Set(this.sel);
        const all = group.every((id) => cur.has(id));
        for (const id of group) if (all) cur.delete(id); else cur.add(id);
        this.selectMany([...cur]);
        return;
      }
      if (this.sel.length > 1 && this.sel.includes(h.id)) {
        this.drag = { mode: "movemany", lx: x, ly: y, moved: false };
        return;
      }
      if (group.length > 1) {
        this.selectMany(group);
        this.drag = { mode: "movemany", lx: x, ly: y, moved: false };
        return;
      }
      this.select(h.id);
      if (!h.locked) this.drag = { mode: "move", id: h.id, lx: x, ly: y, moved: false };
      return;
    }
    const base: WBElement = { id: newId(), type: this.tool as ElementType, stroke: this.pen.stroke, smooth: this.pen.smooth };
    if (this.tool === "text" || this.tool === "sticky") {
      const e: WBElement = this.tool === "text" ? { ...base, x, y: y - 14, w: 240, h: 34, text: "" } : { ...base, stroke: undefined, x: x - 80, y: y - 60, w: 160, h: 120, text: "", fill: "yellow" };
      this.add(e);
      this.tool = "select";
      this.select(e.id);
      this.changed();
      this.editText(e);
      return;
    }
    if (this.tool === "pen") {
      const e = { ...base, points: [[x, y]] as Pt[] };
      this.add(e);
      this.drag = { mode: "pen", id: e.id };
      return;
    }
    if (LINEAR.has(this.tool as ElementType)) {
      const s = bindTarget(this.els(), x, y, 10 / this.view.z);
      const e: WBElement = { ...base, stroke: this.tool === "arrow" ? "gray" : this.pen.stroke, round: this.roundDefault, points: [[x, y], [x, y]] };
      if (s) e.start = bindingAt(s, x, y, 14 / this.view.z);
      this.add(e);
      this.drag = { mode: "linedrag", id: e.id };
      return;
    }
    const e: WBElement = { ...base, x, y, w: 1, h: 1, text: this.tool === "frame" ? t("wb.frame") : "" };
    if (this.tool === "frame") delete e.stroke;
    this.add(e, this.tool === "frame");
    this.drag = { mode: "create", id: e.id, ox: x, oy: y };
  };

  private onMove = (ev: PointerEvent) => {
    const [x, y] = this.toWorld(ev.clientX, ev.clientY), ctrl = ev.ctrlKey || ev.metaKey, tol = 10 / this.view.z;
    this.live?.setCursor([x, y]);
    if (this.tool === "laser") {
      if (this.laserDown) this.laserMove(ev.clientX, ev.clientY, "me");
      return;
    }
    if (this.clickLine) {
      const e = this.byId(this.clickLine)!, n = e.points!.length;
      e.points![n - 1] = alignTo([x, y], e.points![n - 2], ctrl);
      this.hoverBind = bindTarget(this.els(), x, y, tol, e.start?.element)?.id ?? null;
      this.paint();
      return;
    }
    const d = this.drag;
    if (!d) {
      // While choosing where an arrow starts, show the shape it would bind to.
      if (LINEAR.has(this.tool as ElementType)) {
        const hb = bindTarget(this.els(), x, y, tol)?.id ?? null;
        if (hb !== this.hoverBind) {
          this.hoverBind = hb;
          this.paint();
        }
      }
      return;
    }
    if (d.mode === "pan") {
      this.view.x = d.vx! + ev.clientX - d.sx!;
      this.view.y = d.vy! + ev.clientY - d.sy!;
      this.paint();
      return;
    }
    if (d.mode === "erase") {
      const h = hitTest(this.els(), x, y, 8 / this.view.z, this.byId);
      if (h && h.type !== "frame") this.removeEl(h.id);
      return;
    }
    if (d.mode === "box") {
      d.cx = x;
      d.cy = y;
      this.paint();
      return;
    }
    if (d.mode === "movemany") {
      const dx = x - d.lx!, dy = y - d.ly!;
      d.lx = x;
      d.ly = y;
      d.moved = true;
      const moving = new Set(this.sel);
      for (const o of this.selection()) {
        if (o.locked) continue;
        if (o.type === "frame") for (const c of inFrame(this.els(), o, this.byId)) if (!moving.has(c.id)) { moving.add(c.id); shiftEl(c, dx, dy); }
        shiftEl(o, dx, dy);
      }
      this.changed();
      return;
    }
    const e = d.id ? this.byId(d.id) : undefined;
    if (!e) return;
    if (d.mode === "move") {
      const dx = x - d.lx!, dy = y - d.ly!;
      d.lx = x;
      d.ly = y;
      d.moved = true;
      const shift = (o: WBElement) => {
        if (o.points) o.points = o.points.map(([a, b]) => [a + dx, b + dy] as Pt);
        else {
          o.x = (o.x ?? 0) + dx;
          o.y = (o.y ?? 0) + dy;
        }
      };
      if (LINEAR.has(e.type) && (e.start || e.end)) {
        // Moving a bound arrow as a whole frees its ends.
        e.points = linePts(e, this.byId);
        delete e.start;
        delete e.end;
      }
      if (e.type === "frame") {
        // A frame takes what lies in it along; bound ends follow by themselves.
        for (const o of inFrame(this.els(), e, this.byId)) {
          if (LINEAR.has(o.type)) o.points = o.points!.map(([a, c], k) => ((k === 0 && o.start) || (k === o.points!.length - 1 && o.end) ? [a, c] : [a + dx, c + dy]) as Pt);
          else shift(o);
        }
      }
      shift(e);
      this.changed();
      return;
    }
    if (d.mode === "resize") {
      const o = d.o!, dx = x - d.sx!, dy = y - d.sy!, min = 24, h = d.h!;
      let nx = o.x, ny = o.y, nw = o.w, nh = o.h;
      if (h.includes("e")) nw = Math.max(min, o.w + dx);
      if (h.includes("s")) nh = Math.max(min, o.h + dy);
      if (h.includes("w")) { nw = Math.max(min, o.w - dx); nx = o.x + o.w - nw; }
      if (h.includes("n")) { nh = Math.max(min, o.h - dy); ny = o.y + o.h - nh; }
      if (ev.shiftKey && h.length === 2 && o.h > 0) {
        const r = o.w / o.h;
        if (nw / nh > r) nw = nh * r; else nh = nw / r;
        if (h.includes("w")) nx = o.x + o.w - nw;
        if (h.includes("n")) ny = o.y + o.h - nh;
      }
      Object.assign(e, { x: nx, y: ny, w: nw, h: nh });
      this.changed();
      return;
    }
    if (d.mode === "point") {
      const i = d.i!, n = e.points!.length, end = i === 0 || i === n - 1;
      const pts = linePts(e, this.byId), p = alignTo([x, y], pts[i === 0 ? 1 : i - 1], ctrl);
      e.points![i] = p;
      if (end) {
        // An end dropped onto a shape binds to it; until then it is free.
        if (i === 0) delete e.start; else delete e.end;
        const other = i === 0 ? e.end : e.start;
        d.bind = bindTarget(this.els(), p[0], p[1], tol, other?.element);
        this.hoverBind = d.bind?.id ?? null;
      }
      this.changed();
      return;
    }
    if (d.mode === "pen") {
      e.points!.push([x, y]);
      this.changed();
      return;
    }
    if (d.mode === "linedrag") {
      e.points![1] = alignTo([x, y], linePts(e, this.byId)[0], ctrl);
      this.hoverBind = bindTarget(this.els(), x, y, tol, e.start?.element)?.id ?? null;
      this.changed();
      return;
    }
    if (d.mode === "create") {
      e.x = Math.min(x, d.ox!);
      e.y = Math.min(y, d.oy!);
      e.w = Math.abs(x - d.ox!);
      e.h = Math.abs(y - d.oy!);
      this.changed();
    }
  };

  private onUp = () => {
    this.svgEl.classList.remove("panning");
    if (this.tool === "laser") {
      this.laserDown = false;
      this.laserEnd("me");
      return;
    }
    const d = this.drag;
    this.drag = null;
    if (!d) return;
    if (d.mode === "box") {
      const b = { x: Math.min(d.ox!, d.cx!), y: Math.min(d.oy!, d.cy!), w: Math.abs(d.cx! - d.ox!), h: Math.abs(d.cy! - d.oy!) };
      const hit = b.w > 3 || b.h > 3 ? inBox(this.els(), b, this.byId).map((e) => e.id) : [];
      this.selectMany(this.withGroups([...(d.add ?? []), ...hit]));
      return;
    }
    if (d.mode === "movemany") {
      if (d.moved) {
        if (this.snap) {
          // The first element snaps; the others keep their distance to it.
          const sel = this.selection().filter((o) => !o.locked && !o.points);
          if (sel[0]) {
            const before = { x: sel[0].x ?? 0, y: sel[0].y ?? 0 };
            const s0 = { ...sel[0] };
            snapBox(s0);
            const dx = (s0.x ?? 0) - before.x, dy = (s0.y ?? 0) - before.y;
            for (const o of this.selection()) if (!o.locked) shiftEl(o, dx, dy);
          }
        }
        this.changed();
      }
      return;
    }
    const e = d.id ? this.byId(d.id) : undefined;
    if (this.snap && e && !e.points && (d.mode === "create" || d.mode === "resize" || (d.mode === "move" && d.moved))) {
      if (d.mode === "move") {
        const s0 = { ...e };
        snapBox(s0);
        e.x = s0.x;
        e.y = s0.y;
      } else snapBox(e);
    }
    if (d.mode === "linedrag" && e) {
      const p = e.points![1], s = bindTarget(this.els(), p[0], p[1], 10 / this.view.z, e.start?.element);
      if (s) e.end = bindingAt(s, p[0], p[1], 14 / this.view.z);
      const pts = linePts(e, this.byId);
      if (Math.hypot(pts[1][0] - pts[0][0], pts[1][1] - pts[0][1]) < 8) this.board.elements = this.els().filter((x) => x.id !== e.id);
      else this.select(e.id);
      this.tool = "select";
    } else if (d.mode === "point" && e) {
      const i = d.i!, n = e.points!.length;
      if ((i === 0 || i === n - 1) && d.bind) {
        const p = e.points![i], b: Binding = bindingAt(d.bind, p[0], p[1], 14 / this.view.z);
        if (i === 0) e.start = b; else e.end = b;
      }
    } else if (d.mode === "create" && e) {
      if ((e.w ?? 0) < 8 || (e.h ?? 0) < 8) {
        e.w = e.type === "frame" ? 420 : 160;
        e.h = e.type === "frame" ? 280 : 80;
        e.x = (e.x ?? 0) - e.w / 2;
        e.y = (e.y ?? 0) - e.h / 2;
      }
      this.tool = "select";
      this.select(e.id);
    } else if (d.mode === "pen" && e && e.points!.length < 2) {
      this.board.elements = this.els().filter((x) => x.id !== e.id);
    }
    this.hoverBind = null;
    if (d.mode !== "pan" && (d.mode !== "move" || d.moved)) this.changed();
    else this.paint();
  };

  private onDouble(ev: PointerEvent, target: Element) {
    const [x, y] = this.toWorld(ev.clientX, ev.clientY);
    if (this.clickLine) {
      this.finishClickLine();
      this.tool = "select";
      return;
    }
    if (this.readOnly) return;
    const pEl = target.closest?.("[data-pt]") as HTMLElement | null, e = this.selId ? this.byId(this.selId) : undefined;
    if (pEl && e && LINEAR.has(e.type)) {
      const i = Number(pEl.dataset.pt);
      if (i > 0 && i < e.points!.length - 1) {
        e.points!.splice(i, 1);
        this.changed();
      }
      return;
    }
    const h = hitTest(this.els(), x, y, 8 / this.view.z, this.byId);
    if (h && (TEXTUAL.has(h.type) || h.type === "frame")) {
      this.select(h.id);
      this.editText(h);
      return;
    }
    if (!h) {
      // A double-click on the empty canvas starts a text there.
      const tx: WBElement = { id: newId(), type: "text", x, y: y - 14, w: 240, h: 34, text: "", stroke: "gray" };
      this.add(tx);
      this.select(tx.id);
      this.editText(tx);
    }
  }

  private onWheel = (ev: WheelEvent) => {
    ev.preventDefault();
    if (ev.ctrlKey || ev.metaKey) this.zoomAt(ev.clientX, ev.clientY, Math.exp(-ev.deltaY * 0.01));
    else {
      this.view.x -= ev.deltaX;
      this.view.y -= ev.deltaY;
      this.paint();
      this.placeEditor();
    }
  };

  // ---- keyboard and paste --------------------------------------------------------

  private typing(ev: Event) {
    const el = ev.composedPath()[0] as HTMLElement | undefined;
    return !!el?.closest?.("input, textarea, select, [contenteditable=true], [contenteditable='']") || el?.isContentEditable;
  }

  private onKey = (ev: KeyboardEvent) => {
    if (!this.isConnected || this.typing(ev) || !this.svgEl) return;
    const k = ev.key.toLowerCase(), mod = ev.ctrlKey || ev.metaKey;
    if (this.present >= 0) {
      if (["arrowright", "arrowdown", " ", "pagedown", "enter"].includes(k)) { ev.preventDefault(); this.presentAt(this.present + 1); }
      else if (["arrowleft", "arrowup", "pageup", "backspace"].includes(k)) { ev.preventDefault(); this.presentAt(Math.max(0, this.present - 1)); }
      else if (k === "escape") { ev.preventDefault(); this.stopPresenting(); }
      return;
    }
    if (mod && k === "z") {
      ev.preventDefault();
      if (ev.shiftKey) this.live?.redo(); else this.live?.undo();
      return;
    }
    if (mod && k === "y") {
      ev.preventDefault();
      this.live?.redo();
      return;
    }
    if (mod && k === "d" && this.sel.length && !this.readOnly) {
      ev.preventDefault();
      this.duplicate();
      return;
    }
    if (mod && k === "a") {
      ev.preventDefault();
      this.selectMany(this.els().map((e) => e.id));
      return;
    }
    if (mod && (k === "c" || k === "x") && this.sel.length) {
      ev.preventDefault();
      this.copySelection(k === "x");
      return;
    }
    if (mod && k === "g" && this.sel.length > 1 && !this.readOnly) {
      ev.preventDefault();
      this.groupSel(!ev.shiftKey);
      return;
    }
    if (ev.key === " " && !this.spaceDown) {
      this.spaceDown = true;
      this.svgEl.classList.add("t-hand");
      ev.preventDefault();
      return;
    }
    if (ev.key === "Escape") {
      if (this.clickLine) { this.finishClickLine(); this.tool = "select"; return; }
      this.pop = null;
      this.select(null);
      this.setTool("select");
      return;
    }
    if ((ev.key === "Delete" || ev.key === "Backspace") && this.sel.length && !this.readOnly) {
      ev.preventDefault();
      this.removeMany(this.selection().filter((e) => !e.locked).map((e) => e.id));
      return;
    }
    if (ev.key === "Enter" && this.selId && !this.readOnly) {
      const e = this.byId(this.selId);
      if (e && (TEXTUAL.has(e.type) || e.type === "frame")) {
        ev.preventDefault();
        this.editText(e);
      }
      return;
    }
    if (mod || ev.altKey) return;
    const tool = TOOLS.find((x) => x !== "|" && x[2].toLowerCase() === k) as readonly [Tool, string, string] | undefined;
    if (tool) this.setTool(tool[0]);
  };

  private onKeyUp = (ev: KeyboardEvent) => {
    if (ev.key === " " && this.spaceDown) {
      this.spaceDown = false;
      this.svgEl?.classList.remove("t-hand");
    }
  };

  /** A link pasted onto the board becomes an embed in the middle of the view. */
  private onPaste = (ev: ClipboardEvent) => {
    if (!this.isConnected || this.readOnly || this.typing(ev)) return;
    const text = (ev.clipboardData?.getData("text/plain") ?? "").trim();
    if (text.startsWith(CLIP_PREFIX)) {
      ev.preventDefault();
      this.pasteElements(text.slice(CLIP_PREFIX.length));
      return;
    }
    if (!text && this.clip) {
      ev.preventDefault();
      this.pasteElements(this.clip.slice(CLIP_PREFIX.length));
      return;
    }
    if (!/^https?:\/\/\S+$/.test(text)) return;
    ev.preventDefault();
    const r = this.svgEl.getBoundingClientRect(), [x, y] = this.toWorld(r.left + r.width / 2, r.top + r.height / 2);
    void this.embed(text, x - 160, y - 60, false);
  };

  private duplicate() {
    if (this.sel.length > 1) {
      const copies = cloneEls(this.selection(), 24);
      for (const c of copies) this.add(c);
      this.selectMany(copies.map((c) => c.id));
      this.changed();
      return;
    }
    const e = this.selId ? this.byId(this.selId) : undefined;
    if (!e) return;
    const c: WBElement = { ...JSON.parse(JSON.stringify(e)), id: newId(), seed: undefined };
    if (c.points) c.points = c.points.map(([a, b]) => [a + 24, b + 24] as Pt);
    else { c.x = (c.x ?? 0) + 24; c.y = (c.y ?? 0) + 24; }
    delete c.start;
    delete c.end;
    this.add(c);
    this.select(c.id);
    this.changed();
  }

  // ---- text editing ----------------------------------------------------------------

  private editBox?: { el: HTMLElement; box: { x: number; y: number; w: number; h: number }; size: number };

  private placeEditor() {
    if (!this.editBox) return;
    const { el, box, size } = this.editBox;
    const [sx, sy] = this.toScreen(box.x, box.y);
    Object.assign(el.style, { left: sx + "px", top: sy + "px", width: box.w * this.view.z + "px", height: box.h * this.view.z + "px" });
    (el.firstElementChild as HTMLElement).style.fontSize = size * this.view.z + "px";
  }

  /** A double-click puts the caret into the shape, where its text is drawn. */
  private editText(e: WBElement) {
    if (this.editing || this.readOnly) return;
    let box: { x: number; y: number; w: number; h: number }, top = false, size = e.fontSize || 20;
    const x = e.x ?? 0, y = e.y ?? 0, w = e.w ?? 0, h = e.h ?? 0;
    if (LINEAR.has(e.type)) {
      const pts = linePts(e, this.byId), k = Math.floor((pts.length - 1) / 2), m = [(pts[k][0] + pts[k + 1][0]) / 2, (pts[k][1] + pts[k + 1][1]) / 2];
      box = { x: m[0] - 130, y: m[1] - 22, w: 260, h: 44 };
      size = 18;
    } else if (e.type === "sticky") { box = { x: x + 12, y: y + 12, w: w - 24, h: h - 24 }; top = true; size = e.fontSize || 18; }
    else if (e.type === "text") { box = { x, y, w: Math.max(w, 240), h: Math.max(h, 40) }; top = true; size = e.fontSize || 24; }
    else if (e.type === "frame") { box = { x: x + 8, y: y + 2, w: Math.max(160, w - 16), h: 26 }; top = true; size = 13; }
    else {
      const inset = e.type === "diamond" ? 0.22 : e.type === "ellipse" ? 0.15 : 0.04;
      box = { x: x + w * inset, y: y + h * inset, w: w * (1 - 2 * inset), h: h * (1 - 2 * inset) };
    }
    const stage = this.renderRoot.querySelector(".stage")!;
    const wrap = document.createElement("div");
    wrap.className = "editbox" + (top ? " top" : "");
    const ed = document.createElement("div");
    ed.contentEditable = "true";
    ed.setAttribute("role", "textbox");
    ed.setAttribute("aria-multiline", "true");
    ed.setAttribute("aria-label", t("wb.text"));
    if (e.type === "text") ed.style.color = tc(e.stroke);
    if (e.type === "frame") ed.style.font = "600 13px var(--font-body)";
    ed.textContent = e.text ?? "";
    wrap.appendChild(ed);
    stage.appendChild(wrap);
    this.editBox = { el: wrap, box, size };
    this.placeEditor();
    this.editing = e.id;
    this.paint();
    // Focus after the pointer events of the double-click; caret at the end.
    const focusEnd = () => {
      ed.focus();
      const range = document.createRange();
      range.selectNodeContents(ed);
      range.collapse(false);
      const s = getSelection();
      s?.removeAllRanges();
      s?.addRange(range);
    };
    focusEnd();
    setTimeout(focusEnd, 0);
    const before = e.text ?? "";
    const done = (keep: boolean) => {
      if (!wrap.isConnected) return;
      const text = keep ? ed.innerText.replace(/\n$/, "").trim() : before;
      wrap.remove();
      this.editBox = undefined;
      this.editing = null;
      const cur = this.byId(e.id);
      if (cur && text !== before) cur.text = text;
      if (cur && !cur.text && cur.type === "text") this.board.elements = this.els().filter((x) => x.id !== cur.id);
      this.changed();
    };
    const since = performance.now();
    ed.addEventListener("blur", () => {
      // Ignore the blur caused by the double-click itself.
      if (performance.now() - since < 250) {
        setTimeout(focusEnd, 0);
        return;
      }
      done(true);
    });
    ed.addEventListener("keydown", (k) => {
      k.stopPropagation();
      if (k.key === "Escape" || (k.key === "Enter" && (k.ctrlKey || k.metaKey))) {
        k.preventDefault();
        ed.blur();
      }
    });
  }

  // ---- embeds, images, entries ---------------------------------------------------------

  private async embed(url: string, x: number, y: number, web: boolean) {
    let u: URL;
    try { u = new URL(url); } catch { toast(t("wb.bad_link"), "error"); return; }
    if (u.protocol !== "https:" && u.protocol !== "http:") { toast(t("wb.bad_link"), "error"); return; }
    this.busy = true;
    let kind = embedKind(url), preview: Preview = { title: url, site: u.hostname.replace(/^www\./, "") };
    try {
      const r = await api<{ kind: typeof kind; preview: Preview }>("GET", `/projects/${this.project}/embed-preview?url=${encodeURIComponent(url)}`);
      kind = r.kind;
      preview = r.preview;
    } catch { /* the card shows the address */ }
    this.busy = false;
    if (web && kind === "link") {
      if (preview.frameable) kind = "web";
      else toast(t("wb.not_frameable"));
    }
    const e: WBElement = { id: newId(), type: "embed", url, kind, preview, x, y, w: kind === "web" ? 480 : 320, h: kind === "video" ? 214 : kind === "web" ? 320 : kind === "code" ? 124 : 112 };
    this.add(e);
    this.pop = null;
    this.tool = "select";
    this.select(e.id);
    this.changed();
  }

  private pickImage() {
    const input = document.createElement("input");
    input.type = "file";
    input.accept = "image/*";
    input.onchange = () => {
      const f = input.files?.[0];
      if (f) {
        const r = this.svgEl.getBoundingClientRect(), [x, y] = this.toWorld(r.left + r.width / 2, r.top + r.height / 2);
        void this.addImage(f, x, y);
      }
      this.tool = "select";
    };
    input.click();
  }

  private async addImage(file: File, x: number, y: number) {
    if (!file.type.startsWith("image/")) return;
    this.busy = true;
    try {
      const size = await new Promise<[number, number]>((res) => {
        const img = new Image();
        img.onload = () => res([img.naturalWidth, img.naturalHeight]);
        img.onerror = () => res([400, 300]);
        img.src = URL.createObjectURL(file);
      });
      const info = await uploadFile(this.project, file, { document: this.doc!.id });
      const s = Math.min(1, 420 / Math.max(size[0], size[1], 1));
      const e: WBElement = { id: newId(), type: "image", file: info.id, x: x - (size[0] * s) / 2, y: y - (size[1] * s) / 2, w: size[0] * s, h: size[1] * s };
      this.add(e);
      this.select(e.id);
      this.changed();
    } catch {
      toast(t("wb.upload_failed"), "error");
    } finally {
      this.busy = false;
    }
  }

  private onDrop = (ev: DragEvent) => {
    if (this.readOnly) return;
    const ex = [...(ev.dataTransfer?.files ?? [])].find((x) => x.name.endsWith(".excalidraw"));
    if (ex) {
      ev.preventDefault();
      void this.importExcalidraw(ex);
      return;
    }
    const f = [...(ev.dataTransfer?.files ?? [])].find((x) => x.type.startsWith("image/"));
    if (!f) return;
    ev.preventDefault();
    const [x, y] = this.toWorld(ev.clientX, ev.clientY);
    void this.addImage(f, x, y);
  };

  private async loadEntities() {
    if (this.entities.length) return;
    this.entities = (await api<Entity[]>("GET", `/projects/${this.project}/entities`).catch(() => [])) ?? [];
    this.paint();
  }

  private placeEntity(ent: Entity, x: number, y: number) {
    const e: WBElement = { id: newId(), type: "entity", entity: ent.slug, x: x - 110, y: y - 36, w: 220, h: 72, stroke: "blue" };
    this.add(e);
    this.pop = null;
    this.tool = "select";
    this.select(e.id);
    this.changed();
  }

  // ---- comments -------------------------------------------------------------------------

  private ink: Pt[][] = [];

  private saveComment(text: string, ink: Pt[][]) {
    const p = this.pop!, u = currentUser.get();
    const c: WBComment = { id: "c" + Math.random().toString(36).slice(2, 9), x: p.x, y: p.y, element: p.element, author: u?.id, name: u?.name, at: new Date().toISOString() };
    if (text) c.text = text;
    if (ink.length) c.ink = ink.map((s) => s.map(([a, b]) => [+a.toFixed(1), +b.toFixed(1)] as Pt));
    this.board.comments = [...(this.board.comments ?? []), c];
    this.pop = null;
    this.tool = "select";
    this.changed();
    toast(t("wb.comment_saved"));
  }

  private updateComment(id: string, fn: (c: WBComment) => WBComment | null) {
    this.board.comments = (this.board.comments ?? []).flatMap((c) => {
      if (c.id !== id) return [c];
      const n = fn({ ...c });
      return n ? [n] : [];
    });
    this.changed();
  }

  private inkSVG(strokes: Pt[][], h = 46) {
    return html`<svg viewBox="0 0 110 40" style="width:100%;height:${h}px;display:block" aria-hidden="true">${strokes.map((s) =>
      html`<polyline points=${s.map((p) => p.join(",")).join(" ")} style="fill:none;stroke:var(--tc-red);stroke-width:2.4;stroke-linecap:round;stroke-linejoin:round"/>`)}</svg>`;
  }

  // ---- laser pointer --------------------------------------------------------------------

  // Only while the button is held. Each press is its own stroke; old strokes
  // fade on their own and are never joined to a new one.
  private laserDown = false;
  private trails: Record<string, { x: number; y: number; t: number }[][]> = { me: [] };
  private holding: Record<string, boolean> = {};

  private laserStart(who: string) {
    this.holding[who] = true;
    (this.trails[who] ??= []).push([]);
  }
  private laserEnd(who: string) {
    this.holding[who] = false;
    if (who === "me") this.live?.setLaser(undefined);
  }
  private laserMove(cx: number, cy: number, who: string) {
    const strokes = this.trails[who];
    if (!this.holding[who] || !strokes?.length) return;
    const r = this.svgEl.getBoundingClientRect(), tr = strokes[strokes.length - 1], prev = tr[tr.length - 1];
    let x = cx - r.left, y = cy - r.top;
    // A light low-pass filter takes the jitter out of mouse positions.
    if (prev && performance.now() - prev.t < 80) { x = prev.x + (x - prev.x) * 0.6; y = prev.y + (y - prev.y) * 0.6; }
    tr.push({ x, y, t: performance.now() });
    if (!this.trail || reduced()) tr.splice(0, tr.length - 1);
    if (who === "me") this.live?.setLaser(tr.map((p) => this.toWorld(p.x + r.left, p.y + r.top)));
  }

  /** Outline of a tapered band through the points (oldest first): a
   * Catmull-Rom curve over the whole stroke, cut where it has expired. */
  private ribbon(raw: { x: number; y: number; t: number }[], now: number, life: number, maxW: number): string {
    const dense: { x: number; y: number; t: number }[] = [], n0 = raw.length;
    const P = (i: number) => raw[Math.max(0, Math.min(n0 - 1, i))];
    dense.push(raw[0]);
    for (let i = 0; i < n0 - 1; i++) {
      const p0 = P(i - 1), p1 = P(i), p2 = P(i + 1), p3 = P(i + 2);
      const steps = Math.max(1, Math.ceil(Math.hypot(p2.x - p1.x, p2.y - p1.y) / 2.5));
      for (let k = 1; k <= steps; k++) {
        const s = k / steps, s2 = s * s, s3 = s2 * s;
        const cr = (a: number, b: number, c: number, d: number) => 0.5 * (2 * b + (-a + c) * s + (2 * a - 5 * b + 4 * c - d) * s2 + (-a + 3 * b - 3 * c + d) * s3);
        dense.push({ x: cr(p0.x, p1.x, p2.x, p3.x), y: cr(p0.y, p1.y, p2.y, p3.y), t: p1.t + (p2.t - p1.t) * s });
      }
    }
    let cut = 0;
    while (cut < dense.length - 1 && now - dense[cut].t >= life) cut++;
    const pts = dense.slice(Math.max(0, cut - 1));
    if (pts.length < 2) return "";
    const n = pts.length, L: Pt[] = [], R: Pt[] = [], headAge = Math.min(0.95, (now - raw[n0 - 1].t) / life);
    for (let i = 0; i < n; i++) {
      const a = pts[Math.max(0, i - 2)], b = pts[Math.min(n - 1, i + 2)];
      let dx = b.x - a.x, dy = b.y - a.y;
      const d = Math.hypot(dx, dy) || 1;
      dx /= d; dy /= d;
      const ageAbs = (now - pts[i].t) / life, rel = Math.min(1, Math.max(0, (ageAbs - headAge) / Math.max(0.05, 1 - headAge)));
      const w = (maxW / 2) * (0.35 + 0.65 * Math.pow(1 - rel, 0.7));
      L.push([pts[i].x - dy * w, pts[i].y + dx * w]);
      R.push([pts[i].x + dy * w, pts[i].y - dx * w]);
    }
    const smooth = (q: Pt[]) => {
      let d = "";
      for (let i = 1; i < q.length - 1; i++) d += `Q${q[i][0].toFixed(1)},${q[i][1].toFixed(1)} ${((q[i][0] + q[i + 1][0]) / 2).toFixed(1)},${((q[i][1] + q[i + 1][1]) / 2).toFixed(1)}`;
      return d;
    };
    const tw = Math.hypot(L[0][0] - R[0][0], L[0][1] - R[0][1]) / 2, r = R.reverse(), hw = maxW / 2;
    return `M${L[0][0].toFixed(1)},${L[0][1].toFixed(1)}${smooth(L)}L${L[n - 1][0].toFixed(1)},${L[n - 1][1].toFixed(1)}` +
      `A${hw},${hw} 0 0 0 ${r[0][0].toFixed(1)},${r[0][1].toFixed(1)}${smooth(r)}L${r[n - 1][0].toFixed(1)},${r[n - 1][1].toFixed(1)}A${tw},${tw} 0 0 0 ${L[0][0].toFixed(1)},${L[0][1].toFixed(1)}Z`;
  }

  private laserFrame = () => {
    if (!this.isConnected) return;
    const layer = this.svgEl?.querySelector("g.laser");
    if (layer) {
      layer.replaceChildren();
      const now = performance.now(), life = this.trail ? 650 : 120;
      // Others' laser: their points come as one stroke with a time stamp.
      for (const p of this.peers) {
        if (!p.laser?.length || !p.laserAt || Date.now() - p.laserAt > 2000) continue;
        const age = Date.now() - p.laserAt;
        const pts = p.laser.map(([x, y], i, a) => {
          const [sx, sy] = this.toScreen(x, y);
          return { x: sx, y: sy, t: now - age - (a.length - 1 - i) * 16 };
        });
        this.drawStroke(layer, pts, now, life, false, p.color);
      }
      for (const who of Object.keys(this.trails)) {
        const strokes = this.trails[who];
        for (let si = strokes.length - 1; si >= 0; si--) {
          const live = this.holding[who] && si === strokes.length - 1, pts = strokes[si];
          if (!pts.length || (!live && now - pts[pts.length - 1].t >= life)) {
            if (!live) strokes.splice(si, 1);
            continue;
          }
          while (pts.length > 4 && now - pts[2].t >= life) pts.shift();
          this.drawStroke(layer, pts, now, life, live, "var(--laser, var(--bad))");
        }
      }
    }
    requestAnimationFrame(this.laserFrame);
  };

  private drawStroke(layer: Element, pts: { x: number; y: number; t: number }[], now: number, life: number, live: boolean, color: string) {
    const last = pts[pts.length - 1], fade = live ? 1 : Math.max(0, 1 - (now - last.t) / life);
    if (fade <= 0) return;
    const g = mk("g", {}, layer);
    g.style.cssText = `fill:${color};opacity:${fade};filter:drop-shadow(0 0 5px ${color})`;
    const d = pts.length > 1 ? this.ribbon(pts, now, life, this.laserSize * 2) : "";
    if (d) mk("path", { d }, g);
    mk("circle", { cx: last.x, cy: last.y, r: this.laserSize }, g);
  }

  // ---- export ---------------------------------------------------------------------------------

  private async exportAs(kind: "svg" | "png") {
    const b = extent(this.els(), this.byId);
    if (!b) return;
    const pad = 40, cs = getComputedStyle(this);
    const resolve = (s: string): string => s.replace(/var\((--[\w-]+)(?:,\s*([^)]+))?\)/g, (_m, v: string, fb?: string) => cs.getPropertyValue(v).trim() || (fb ? resolve(fb) : "#888"));
    const layer = this.svgEl.querySelector("g.layer")!.cloneNode(true) as SVGGElement;
    // Selection handles and invisible hit lines are not part of the picture.
    layer.querySelectorAll("[data-h],[data-pt],[data-mid]").forEach((n) => n.remove());
    // Embeds and entries become simple cards (no HTML inside the file).
    for (const fo of [...layer.querySelectorAll("foreignObject")]) {
      const x = fo.getAttribute("x"), y = fo.getAttribute("y"), w = fo.getAttribute("width"), h = fo.getAttribute("height");
      const g = document.createElementNS(NS, "g");
      const r = mk("rect", { x: x!, y: y!, width: w!, height: h!, rx: 10, fill: resolve("var(--surface)"), stroke: resolve("var(--line)") }, g);
      r.setAttribute("stroke-width", "1.5");
      const tx = mk("text", { x: Number(x) + 14, y: Number(y) + 26, fill: resolve("var(--fg)"), "font-size": 14, "font-family": "sans-serif" }, g);
      tx.textContent = (fo.textContent ?? "").trim().slice(0, 60);
      fo.replaceWith(g);
    }
    for (const n of [layer, ...layer.querySelectorAll("*")]) {
      const st = n.getAttribute("style");
      if (st) n.setAttribute("style", resolve(st));
      for (const a of ["fill", "stroke"]) {
        const v = n.getAttribute(a);
        if (v?.includes("var(")) n.setAttribute(a, resolve(v));
      }
    }
    // Images inside an exported picture must be data URLs.
    for (const img of [...layer.querySelectorAll("image")]) {
      try {
        const blob = await (await fetch(img.getAttribute("href")!, { credentials: "same-origin" })).blob();
        const data = await new Promise<string>((res) => { const fr = new FileReader(); fr.onload = () => res(String(fr.result)); fr.readAsDataURL(blob); });
        img.setAttribute("href", data);
      } catch { img.remove(); }
    }
    const W = b.w + 2 * pad, H = b.h + 2 * pad;
    const bg = resolve("var(--canvas, var(--bg))");
    const svgText = `<svg xmlns="${NS}" viewBox="${b.x - pad} ${b.y - pad} ${W} ${H}" width="${Math.round(W)}" height="${Math.round(H)}"><rect x="${b.x - pad}" y="${b.y - pad}" width="${W}" height="${H}" fill="${bg}"/>${new XMLSerializer().serializeToString(layer)}</svg>`;
    const name = (this.doc?.slug ?? "whiteboard") + "." + kind;
    const save = (blob: Blob) => {
      const a = document.createElement("a");
      a.href = URL.createObjectURL(blob);
      a.download = name;
      a.click();
      setTimeout(() => URL.revokeObjectURL(a.href), 5000);
    };
    if (kind === "svg") {
      save(new Blob([svgText], { type: "image/svg+xml" }));
      return;
    }
    const img = new Image(), scale = 2;
    img.onload = () => {
      const c = document.createElement("canvas");
      c.width = Math.round(W * scale);
      c.height = Math.round(H * scale);
      const ctx = c.getContext("2d")!;
      ctx.scale(scale, scale);
      ctx.drawImage(img, 0, 0, W, H);
      c.toBlob((bl) => bl && save(bl), "image/png");
    };
    img.src = "data:image/svg+xml;charset=utf-8," + encodeURIComponent(svgText);
  }

  // ---- chrome --------------------------------------------------------------------------------

  private multiBar() {
    const n = this.sel.length;
    const grouped = this.selection().some((e) => e.groups?.length);
    const b = (how: AlignHow | "dx" | "dy", ico: string, label: string, dis = false) =>
      html`<button class="ghost" title=${label} aria-label=${label} ?disabled=${dis} @click=${() => this.alignSel(how)}>${icon(ico, 14)}</button>`;
    return html`<aside class="stylebar" aria-label=${t("wb.style")}>
      <h4>${t("wb.selected", { count: n })}</h4>
      <h4>${t("wb.align")}</h4>
      <div class="row">${b("left", "AlignStartVertical", t("wb.align_left"))}${b("hcenter", "AlignCenterVertical", t("wb.align_hcenter"))}${b("right", "AlignEndVertical", t("wb.align_right"))}</div>
      <div class="row">${b("top", "AlignStartHorizontal", t("wb.align_top"))}${b("vcenter", "AlignCenterHorizontal", t("wb.align_vcenter"))}${b("bottom", "AlignEndHorizontal", t("wb.align_bottom"))}</div>
      <h4>${t("wb.distribute")}</h4>
      <div class="row">${b("dx", "AlignHorizontalSpaceAround", t("wb.distribute_h"), n < 3)}${b("dy", "AlignVerticalSpaceAround", t("wb.distribute_v"), n < 3)}</div>
      <div class="row">
        <button class="ghost" title=${t("wb.group")} aria-label=${t("wb.group")} @click=${() => this.groupSel(true)}>${icon("Group", 14)}</button>
        <button class="ghost" title=${t("wb.ungroup")} aria-label=${t("wb.ungroup")} ?disabled=${!grouped} @click=${() => this.groupSel(false)}>${icon("Ungroup", 14)}</button>
        <button class="ghost" title=${t("wb.duplicate")} aria-label=${t("wb.duplicate")} @click=${() => this.duplicate()}>${icon("Copy", 14)}</button>
        <button class="ghost danger" title=${t("wb.delete")} aria-label=${t("wb.delete")} @click=${() => this.removeMany(this.selection().filter((e) => !e.locked).map((e) => e.id))}>${icon("Trash2", 14)}</button>
      </div>
    </aside>`;
  }

  private styleBar() {
    if (this.sel.length > 1 && !this.readOnly && !this.editing) return this.multiBar();
    const e = this.selId ? this.byId(this.selId) : undefined;
    if (!e || this.readOnly || this.editing) return nothing;
    const set = (patch: Partial<WBElement>) => {
      const cur = this.byId(e.id);
      if (!cur) return;
      for (const [k, v] of Object.entries(patch)) {
        if (v === undefined) delete (cur as unknown as Record<string, unknown>)[k];
        else (cur as unknown as Record<string, unknown>)[k] = v;
      }
      this.changed();
    };
    const lin = LINEAR.has(e.type), shape = ["rect", "ellipse", "diamond"].includes(e.type);
    const strokeable = !["frame", "image", "embed", "sticky"].includes(e.type);
    return html`<aside class="stylebar" aria-label=${t("wb.style")}>
      ${strokeable ? html`<h4>${t("wb.stroke")}</h4><div class="swatches">${PALETTE.map((c) => html`<button class="sw" style="background:${tc(c)}" aria-label=${t(`color.${c}` as any)} aria-pressed=${(e.stroke ?? "gray") === c ? "true" : "false"}
        @click=${() => { set({ stroke: c }); this.pen.stroke = c; }}></button>`)}</div>` : nothing}
      ${shape || e.type === "sticky" ? html`<h4>${t("wb.fill")}</h4><div class="swatches">${PALETTE.map((c) => html`<button class="sw" style="background:${tb(c)}" aria-label=${t(`color.${c}` as any)} aria-pressed=${e.fill === c ? "true" : "false"}
        @click=${() => set({ fill: c })}></button>`)}</div>
        ${shape ? html`<div class="seg2"><button aria-pressed=${!e.fill ? "true" : "false"} @click=${() => set({ fill: undefined })}>${t("wb.no_fill")}</button></div>` : nothing}` : nothing}
      ${lin ? html`<h4>${t("wb.kind")}</h4><div class="seg2"><button aria-pressed=${e.type === "arrow" ? "true" : "false"} @click=${() => set({ type: "arrow" })}>${t("wb.arrow")}</button><button aria-pressed=${e.type === "line" ? "true" : "false"} @click=${() => set({ type: "line" })}>${t("wb.line")}</button></div>
        <h4>${t("wb.corners")}</h4><div class="seg2"><button aria-pressed=${e.round ? "true" : "false"} @click=${() => { set({ round: true }); this.roundDefault = true; }}>${t("wb.round")}</button><button aria-pressed=${!e.round ? "true" : "false"} @click=${() => { set({ round: undefined }); this.roundDefault = false; }}>${t("wb.sharp")}</button></div>
        <div class="muted" style="font-size:11px">${t("wb.ends", { start: e.start ? (e.start.fx !== undefined ? t("wb.end_fixed") : t("wb.end_centre")) : t("wb.end_free"), end: e.end ? (e.end.fx !== undefined ? t("wb.end_fixed") : t("wb.end_centre")) : t("wb.end_free") })}</div>` : nothing}
      ${strokeable && e.type !== "text" && e.type !== "entity" ? html`<h4>${t("wb.width")}</h4><div class="seg2">${[1, 2, 4].map((w, i) => html`<button aria-pressed=${(e.width || 2) === w ? "true" : "false"} @click=${() => set({ width: w === 2 ? undefined : w })}>${[t("wb.thin"), t("wb.medium"), t("wb.bold")][i]}</button>`)}</div>
        <div class="seg2">${(["", "dashed", "dotted"] as const).map((d) => html`<button aria-pressed=${(e.dash ?? "") === d ? "true" : "false"} @click=${() => set({ dash: d || undefined })}>${t(`wb.dash_${d || "solid"}` as any)}</button>`)}</div>
        <h4>${t("wb.look")}</h4><div class="seg2"><button aria-pressed=${!e.smooth ? "true" : "false"} @click=${() => { set({ smooth: undefined }); this.pen.smooth = false; }}>${t("wb.hand_drawn")}</button><button aria-pressed=${e.smooth ? "true" : "false"} @click=${() => { set({ smooth: true }); this.pen.smooth = true; }}>${t("wb.clean")}</button></div>` : nothing}
      ${TEXTUAL.has(e.type) && !lin ? html`<h4>${t("wb.font_size")}</h4><div class="seg2">${[16, 20, 28, 40].map((s, i) => html`<button aria-pressed=${(e.fontSize || (e.type === "text" ? 24 : 20)) === s ? "true" : "false"} @click=${() => set({ fontSize: s })}>${["S", "M", "L", "XL"][i]}</button>`)}</div>` : nothing}
      <div class="row">
        <button class="ghost" title=${t("wb.front")} aria-label=${t("wb.front")} @click=${() => set({ order: frontKey(this.els()) })}>${icon("BringToFront", 14)}</button>
        <button class="ghost" title=${t("wb.back")} aria-label=${t("wb.back")} @click=${() => set({ order: backKey(this.els()) })}>${icon("SendToBack", 14)}</button>
        <button class="ghost" title=${t("wb.duplicate")} aria-label=${t("wb.duplicate")} @click=${() => this.duplicate()}>${icon("Copy", 14)}</button>
        <button class="ghost" title=${t(e.locked ? "wb.unlock" : "wb.lock")} aria-label=${t(e.locked ? "wb.unlock" : "wb.lock")} aria-pressed=${e.locked ? "true" : "false"} @click=${() => set({ locked: e.locked ? undefined : true })}>${icon("Lock", 14)}</button>
        <button class="ghost danger" title=${t("wb.delete")} aria-label=${t("wb.delete")} @click=${() => this.removeEl(e.id)}>${icon("Trash2", 14)}</button>
        ${e.entity ? html`<a class="btn ghost" style="height:28px;padding:0 8px;font-size:12px" href="/p/${this.project}/e/${e.entity}" @click=${(ev: MouseEvent) => { if (!ev.ctrlKey && !ev.metaKey && ev.button === 0) { ev.preventDefault(); navigate(`/p/${this.project}/e/${e.entity}`); } }}>${icon("ExternalLink", 13)} ${t("wb.open_entry")}</a>` : nothing}
      </div>
    </aside>`;
  }

  private popPos(x: number, y: number) {
    const r = this.svgEl?.getBoundingClientRect(), [sx, sy] = this.toScreen(x, y);
    const w = r?.width ?? 800, h = r?.height ?? 600;
    return `left:${Math.min(Math.max(12, sx + 18), w - 302)}px;top:${Math.min(Math.max(12, sy - 20), h - 300)}px`;
  }

  private popoverView() {
    const p = this.pop;
    if (!p) return nothing;
    const close = () => (this.pop = null);
    if (p.kind === "embed") {
      return html`<div class="pop" style=${this.popPos(p.x, p.y)} role="dialog" aria-label=${t("wb.embed")}>
        <strong>${t("wb.embed")}</strong>
        <input type="url" placeholder=${t("wb.embed_placeholder")} aria-label=${t("wb.link")} autofocus @keydown=${(e: KeyboardEvent) => { e.stopPropagation(); if (e.key === "Enter") this.submitEmbed(); if (e.key === "Escape") close(); }}>
        <div class="as" role="radiogroup" aria-label=${t("wb.embed_as")}><span>${t("wb.embed_as")}</span>
          <span class="seg2">${([false, true] as const).map((w) => html`<button type="button" role="radio" aria-checked=${this.embedWeb === w ? "true" : "false"} aria-pressed=${this.embedWeb === w ? "true" : "false"}
            @click=${() => (this.embedWeb = w)}>${t(w ? "wb.embed_as_web" : "wb.embed_as_card")}</button>`)}</span></div>
        <p class="muted" style="margin:0">${t("wb.embed_hint")}</p>
        <div class="acts"><button class="ghost" @click=${close}>${t("doc.cancel")}</button><button ?disabled=${this.busy} @click=${() => this.submitEmbed()}>${t("wb.embed_button")}</button></div>
      </div>`;
    }
    if (p.kind === "entity") {
      const q = this.query.toLowerCase();
      const list = this.entities.filter((e) => !q || e.name.toLowerCase().includes(q) || e.slug.includes(q)).slice(0, 40);
      return html`<div class="pop" style=${this.popPos(p.x, p.y)} role="dialog" aria-label=${t("wb.entity")}>
        <strong>${t("wb.entity")}</strong>
        <input type="search" placeholder=${t("wb.entity_search")} aria-label=${t("wb.entity_search")} autofocus .value=${this.query}
          @input=${(e: Event) => (this.query = (e.target as HTMLInputElement).value)} @keydown=${(e: KeyboardEvent) => { e.stopPropagation(); if (e.key === "Escape") close(); if (e.key === "Enter" && list[0]) this.placeEntity(list[0], p.x, p.y); }}>
        <ul>${list.map((ent) => { const k = findKind(this.model, ent.kind); return html`<li><button @click=${() => this.placeEntity(ent, p.x, p.y)}>${icon(k?.icon ?? "Waypoints", 14)}<span>${ent.name}</span><span class="muted">${kindName(k, ent.kind)}</span></button></li>`; })}</ul>
        ${!list.length ? html`<p class="muted" style="margin:0">${t("wb.entity_none")}</p>` : nothing}
      </div>`;
    }
    if (p.kind === "newcomment") {
      const target = p.element ? this.byId(p.element) : undefined;
      return html`<div class="pop" style=${this.popPos(p.x, p.y)} role="dialog" aria-label=${t("wb.new_comment")}>
        <div class="who">${t("wb.new_comment")}${target ? html` · ${t("wb.on", { name: target.text || t(`wb.t.${target.type}` as any) })}` : nothing}</div>
        <div class="seg2" role="group"><button aria-pressed="true" data-m="text" @click=${(e: Event) => this.commentMode(e, "text")}>${icon("Type", 13)} ${t("wb.text")}</button><button aria-pressed="false" data-m="hand" @click=${(e: Event) => this.commentMode(e, "hand")}>${icon("Pencil", 13)} ${t("wb.handwriting")}</button></div>
        <textarea placeholder=${t("wb.comment_placeholder")} aria-label=${t("wb.comment")} autofocus @keydown=${(e: KeyboardEvent) => { e.stopPropagation(); if (e.key === "Escape") close(); if (e.key === "Enter" && (e.ctrlKey || e.metaKey)) this.submitComment(); }}></textarea>
        <canvas hidden width="550" height="200" aria-label=${t("wb.handwriting")} @pointerdown=${this.inkDown} @pointermove=${this.inkMove} @pointerup=${() => (this.inking = null)}></canvas>
        <div class="acts"><span class="muted" style="margin-right:auto">${t("wb.ai_reads")}</span><button class="ghost" @click=${close}>${t("doc.cancel")}</button><button @click=${() => this.submitComment()}>${icon("Send", 13)} ${t("wb.comment_button")}</button></div>
      </div>`;
    }
    const c = (this.board.comments ?? []).find((x) => x.id === p.id);
    if (!c) return nothing;
    const target = c.element ? this.byId(c.element) : undefined, me = currentUser.get();
    return html`<div class="pop" style=${this.popPos(c.x, c.y)} role="dialog" aria-label=${t("wb.comment")}>
      <div class="who"><span class="avatars"><span style="background:${userColor(c.author ?? "")};margin:0">${(c.name ?? "?").slice(0, 2).toUpperCase()}</span></span>${c.name ?? ""}${target ? html` · ${t("wb.on", { name: target.text || t(`wb.t.${target.type}` as any) })}` : nothing}${c.at ? html` · ${formatDate(c.at)}` : nothing}</div>
      ${c.ink?.length ? this.inkSVG(c.ink, 70) : nothing}${c.text ? html`<div class="msg">${c.text}</div>` : nothing}
      ${(c.replies ?? []).map((r) => html`<div class="reply"><div class="who">${r.author?.startsWith("mcp:") || r.name?.startsWith("AI") ? icon("Bot", 12) : nothing}${r.name ?? ""}</div><div class="msg">${r.text}</div></div>`)}
      ${this.readOnly ? nothing : html`<textarea placeholder=${t("wb.reply_placeholder")} aria-label=${t("wb.reply")} style="min-height:44px" @keydown=${(e: KeyboardEvent) => { e.stopPropagation(); if (e.key === "Enter" && (e.ctrlKey || e.metaKey)) this.reply(c.id, e.target as HTMLTextAreaElement); if (e.key === "Escape") close(); }}></textarea>
      <div class="acts">
        ${c.author === me?.id || this.canEdit ? html`<button class="ghost danger" @click=${() => { this.updateComment(c.id, () => null); close(); }}>${icon("Trash2", 13)}</button>` : nothing}
        <button class="ghost" @click=${(e: Event) => this.reply(c.id, (e.target as HTMLElement).closest(".pop")!.querySelector("textarea")!)}>${icon("Reply", 13)} ${t("wb.reply")}</button>
        <button class="ghost" @click=${() => { this.updateComment(c.id, (x) => ({ ...x, resolved: true })); close(); }}>${icon("Check", 13)} ${t("wb.resolve")}</button>
      </div>`}
    </div>`;
  }

  private submitEmbed() {
    const pop = this.renderRoot.querySelector(".pop");
    const url = (pop?.querySelector("input[type=url]") as HTMLInputElement | null)?.value.trim() ?? "";
    const web = this.embedWeb;
    if (url && this.pop) void this.embed(url, this.pop.x, this.pop.y, web);
  }

  private commentMode(e: Event, mode: "text" | "hand") {
    const pop = (e.target as HTMLElement).closest(".pop")!;
    pop.querySelectorAll<HTMLButtonElement>("[data-m]").forEach((b) => b.setAttribute("aria-pressed", b.dataset.m === mode ? "true" : "false"));
    (pop.querySelector("textarea") as HTMLElement).hidden = mode !== "text";
    (pop.querySelector("canvas") as HTMLElement).hidden = mode !== "hand";
    this.ink = [];
  }

  private inking: Pt[] | null = null;
  private inkPoint(e: PointerEvent): Pt {
    const r = (e.target as HTMLCanvasElement).getBoundingClientRect();
    return [((e.clientX - r.left) / r.width) * 110, ((e.clientY - r.top) / r.height) * 40];
  }
  private inkDown = (e: PointerEvent) => {
    (e.target as HTMLCanvasElement).setPointerCapture(e.pointerId);
    this.inking = [this.inkPoint(e)];
    this.ink.push(this.inking);
  };
  private inkMove = (e: PointerEvent) => {
    if (!this.inking) return;
    this.inking.push(this.inkPoint(e));
    const cv = e.target as HTMLCanvasElement, ctx = cv.getContext("2d")!;
    ctx.clearRect(0, 0, cv.width, cv.height);
    ctx.strokeStyle = getComputedStyle(this).getPropertyValue("--tc-red").trim() || "#c33";
    ctx.lineWidth = 5;
    ctx.lineCap = ctx.lineJoin = "round";
    for (const s of this.ink) {
      ctx.beginPath();
      s.forEach(([a, b], i) => (i ? ctx.lineTo((a / 110) * cv.width, (b / 40) * cv.height) : ctx.moveTo((a / 110) * cv.width, (b / 40) * cv.height)));
      ctx.stroke();
    }
  };

  private submitComment() {
    const pop = this.renderRoot.querySelector(".pop")!;
    const ta = pop.querySelector("textarea") as HTMLTextAreaElement, hand = !(pop.querySelector("canvas") as HTMLElement).hidden;
    const text = hand ? "" : ta.value.trim();
    if (!text && !(hand && this.ink.length)) {
      ta.focus();
      return;
    }
    this.saveComment(text, hand ? this.ink : []);
    this.ink = [];
  }

  private reply(id: string, ta: HTMLTextAreaElement) {
    const text = ta.value.trim(), u = currentUser.get();
    if (!text) return;
    this.updateComment(id, (c) => ({ ...c, replies: [...(c.replies ?? []), { id: "r" + Math.random().toString(36).slice(2, 8), author: u?.id, name: u?.name, text, at: new Date().toISOString() }] }));
    ta.value = "";
  }

  private commentPanel() {
    if (!this.panel) return nothing;
    const all = this.board.comments ?? [];
    return html`<aside class="panel" aria-label=${t("wb.comments")}>
      <header>${t("wb.comments")}<button class="ghost icon" aria-label=${t("plan.close")} @click=${() => (this.panel = false)}>${icon("X", 15)}</button></header>
      <div class="list">${all.length ? all.map((c, i) => html`<button class="thread ${c.resolved ? "done" : ""}" @click=${() => this.focusComment(c)}>
        <span class="muted">${c.resolved ? "✓" : `${this.openComments().indexOf(c) + 1}`} · ${c.name ?? ""}${c.resolved ? ` · ${t("wb.resolved")}` : ""}</span>
        ${c.ink?.length ? this.inkSVG(c.ink) : nothing}${c.text ? html`<span>${c.text}</span>` : nothing}
        ${c.replies?.length ? html`<span class="muted">${t("wb.replies", { count: c.replies.length })}</span>` : nothing}
        ${i < 0 ? nothing : nothing}</button>`) : html`<p class="muted">${t("wb.no_comments")}</p>`}</div>
    </aside>`;
  }

  private focusComment(c: WBComment) {
    if (c.resolved) {
      if (!this.readOnly) this.updateComment(c.id, (x) => ({ ...x, resolved: undefined }));
      return;
    }
    const r = this.svgEl.getBoundingClientRect();
    this.view.x = r.width / 2 - c.x * this.view.z;
    this.view.y = r.height / 2 - c.y * this.view.z;
    this.paint();
    this.pop = { kind: "comment", x: c.x, y: c.y, id: c.id };
  }

  private stateText() {
    if (this.readOnly) return t("wb.read_only");
    if (this.status !== "connected") return t("wb.offline");
    if (this.saved?.state === "saving") return t("wb.saving");
    if (this.saved?.state === "error") return t("wb.save_failed");
    if (this.saved?.at) return t("wb.published_at", { time: this.saved.at.toLocaleTimeString(document.documentElement.lang || "en", { hour: "2-digit", minute: "2-digit" }) });
    return t("wb.live");
  }

  render() {
    const toolCls = this.tool === "hand" ? "t-hand" : this.tool === "laser" ? "t-laser" : this.tool === "comment" ? "t-comment" : this.tool === "select" ? "" : "t-draw";
    const open = this.openComments().length;
    return html`<div class="stage" @dragover=${(e: DragEvent) => !this.readOnly && e.preventDefault()} @drop=${this.onDrop}>
      <svg class="board ${toolCls}" tabindex="0" role="application" aria-label=${this.doc?.title ?? t("wb.board")}
        @pointerdown=${this.onDown} @pointermove=${this.onMove} @pointerup=${this.onUp} @pointercancel=${this.onUp}
        @pointerleave=${() => { this.live?.setCursor(undefined); if (this.laserDown) { this.laserDown = false; this.laserEnd("me"); } }} @wheel=${this.onWheel}>
        <defs><pattern id="wbgrid" width="24" height="24" patternUnits="userSpaceOnUse"><circle class="grid" cx="1" cy="1" r="1"></circle></pattern></defs>
        ${this.board.background === "plain" ? nothing : html`<rect width="100%" height="100%" fill="url(#wbgrid)" style="pointer-events:none"></rect>`}
        <g class="world"><g class="layer"></g><g class="pins"></g><g class="others"></g></g>
        <g class="laser" style="pointer-events:none"></g>
      </svg>
      ${this.els().length === 0 && this.painted ? html`<div class="empty"><div>${this.readOnly ? t("wb.empty_read") : t("wb.empty")}
        ${this.readOnly ? nothing : html`<div class="tpls"><span>${t("wb.tpl.start")}</span>${TEMPLATES.map((k) => html`<button class="ghost" @click=${() => this.useTemplate(k)}>${t(`wb.tpl.${k}` as any)}</button>`)}
          <button class="ghost" @click=${() => this.pickExcalidraw()}>${icon("Upload", 14)} ${t("wb.import_excalidraw")}</button></div>`}</div></div>` : nothing}
      <div class="bar tools" role="toolbar" aria-label=${t("wb.tools")}>
        ${TOOLS.filter((x) => x === "|" ? !this.readOnly : !this.readOnly || VIEW_TOOLS.has(x[0])).map((x) => x === "|" ? html`<span class="sep"></span>` : html`
          <button aria-pressed=${this.tool === x[0] ? "true" : "false"} title="${t(`wb.tool.${x[0]}` as any)} (${x[2]})" aria-label=${t(`wb.tool.${x[0]}` as any)} @click=${() => this.setTool(x[0])}>${icon(x[1], 18)}<kbd>${x[2]}</kbd></button>`)}
      </div>
      ${LINEAR.has(this.tool as ElementType) ? html`<div class="bar opts">
        <span class="seg"><button aria-pressed=${this.arrowMode === "drag" ? "true" : "false"} @click=${() => this.setArrowMode("drag")}>${t("wb.mode_drag")}</button><button aria-pressed=${this.arrowMode === "click" ? "true" : "false"} @click=${() => this.setArrowMode("click")}>${t("wb.mode_click")}</button></span>
        <span class="seg"><button aria-pressed=${this.roundDefault ? "true" : "false"} @click=${() => { this.roundDefault = true; try { localStorage.setItem("wb.round", "1"); } catch { /* ignore */ } }}>${t("wb.round")}</button><button aria-pressed=${!this.roundDefault ? "true" : "false"} @click=${() => { this.roundDefault = false; try { localStorage.setItem("wb.round", "0"); } catch { /* ignore */ } }}>${t("wb.sharp")}</button></span>
        <span>${this.arrowMode === "drag" ? t("wb.hint_drag") : t("wb.hint_click")}</span></div>` : nothing}
      ${this.tool === "laser" ? html`<div class="bar opts">
        <span class="seg"><button aria-pressed=${this.trail ? "true" : "false"} @click=${() => { this.trail = true; try { localStorage.setItem("wb.trail", "trail"); } catch { /* ignore */ } }}>${t("wb.trail")}</button><button aria-pressed=${!this.trail ? "true" : "false"} @click=${() => { this.trail = false; try { localStorage.setItem("wb.trail", "dot"); } catch { /* ignore */ } }}>${t("wb.dot")}</button></span>
        <span class="seg" role="group" aria-label=${t("wb.thickness")}>${[3, 4.5, 7].map((s, i) => html`<button aria-pressed=${this.laserSize === s ? "true" : "false"} aria-label=${[t("wb.thin"), t("wb.medium"), t("wb.bold")][i]} title=${[t("wb.thin"), t("wb.medium"), t("wb.bold")][i]}
          @click=${() => { this.laserSize = s; try { localStorage.setItem("wb.laserSize", String(s)); } catch { /* ignore */ } }}><span class="dot" style="width:${s * 2}px;height:${s * 2}px"></span></button>`)}</span>
        <span>${t("wb.hint_laser")}</span></div>` : nothing}
      <div class="bar state" role="status">
        <span class="live ${this.readOnly || this.status !== "connected" ? "off" : this.saved?.state === "saving" ? "saving" : ""}"></span>${this.stateText()}
        ${this.peers.length ? html`<span class="avatars" title=${this.peers.map((p) => p.name).join(", ")}>${this.peers.map((p) => html`<span style="background:${p.color}">${p.name.slice(0, 2).toUpperCase()}</span>`)}</span>` : nothing}
      </div>
      <div class="bar zoom">
        <button aria-label=${t("wb.zoom_out")} title=${t("wb.zoom_out")} @click=${() => this.zoomBy(1 / 1.2)}>${icon("ZoomOut", 16)}</button><span>${this.zoom} %</span>
        <button aria-label=${t("wb.zoom_in")} title=${t("wb.zoom_in")} @click=${() => this.zoomBy(1.2)}>${icon("ZoomIn", 16)}</button>
        <button aria-label=${t("wb.fit")} title=${t("wb.fit")} @click=${() => this.fit()}>${icon("Maximize", 16)}</button>
        ${this.readOnly ? nothing : html`<button aria-label=${t("wb.snap")} title=${t("wb.snap")} aria-pressed=${this.snap ? "true" : "false"} @click=${() => this.setSnap(!this.snap)}>${icon("Grid3x3", 16)}</button>`}
        ${this.readOnly ? nothing : html`<span class="sep"></span>
        <button aria-label=${t("editor.undo")} title=${t("editor.undo")} @click=${() => this.live?.undo()}>${icon("Undo2", 16)}</button>
        <button aria-label=${t("editor.redo")} title=${t("editor.redo")} @click=${() => this.live?.redo()}>${icon("Redo2", 16)}</button>`}
      </div>
      <div class="bar side">
        <button aria-label=${t("wb.export_svg")} title=${t("wb.export_svg")} @click=${() => this.exportAs("svg")}>${icon("Download", 16)}</button>
        <button aria-label=${t("wb.export_png")} title=${t("wb.export_png")} @click=${() => this.exportAs("png")}>${icon("Image", 16)}</button>
        <button aria-label=${t("wb.export_excalidraw")} title=${t("wb.export_excalidraw")} @click=${() => this.exportExcalidraw()}>${icon("FileCode", 16)}</button>
        ${this.readOnly ? nothing : html`<button aria-label=${t("wb.import_excalidraw")} title=${t("wb.import_excalidraw")} @click=${() => this.pickExcalidraw()}>${icon("Upload", 16)}</button>`}
        <button aria-label=${t("wb.present")} title=${t("wb.present")} @click=${() => this.presentAt(0)}>${icon("Presentation", 16)}</button>
        <button aria-label=${t("wb.comments")} title=${t("wb.comments")} aria-pressed=${this.panel ? "true" : "false"} @click=${() => (this.panel = !this.panel)}>${icon("MessageSquare", 16)}${open ? html`<span class="count">${open}</span>` : nothing}</button>
      </div>
      ${this.panel ? this.commentPanel() : this.styleBar()}
      ${this.popoverView()}
      ${this.present >= 0 ? html`<div class="bar present" role="toolbar" aria-label=${t("wb.present")}>
        <button aria-label=${t("wb.prev")} @click=${() => this.presentAt(Math.max(0, this.present - 1))}>${icon("ChevronLeft", 16)}</button>
        <span>${this.present + 1} / ${slides(this.els()).length} · ${slides(this.els())[this.present]?.text ?? ""}</span>
        <button aria-label=${t("wb.next")} @click=${() => this.presentAt(this.present + 1)}>${icon("ChevronRight", 16)}</button>
        <button aria-label=${t("wb.present_end")} @click=${() => this.stopPresenting()}>${icon("X", 16)}</button></div>` : nothing}
    </div>`;
  }

  updated() {
    this.toggleAttribute("presenting", this.present >= 0);
    if (!this.painted && this.doc) this.paint();
    if (this.pop) (this.renderRoot.querySelector(".pop [autofocus]") as HTMLElement | null)?.focus();
  }
}
customElements.define("phx-whiteboard", PhxWhiteboard);
