// The whiteboard editor (ADR-0068): an own canvas with a hand-drawn look
// (Rough.js), drawn together live and published automatically. Shapes,
// arrows and lines bound to shapes (drag or click mode, round or sharp
// corners, editable points), text in shapes, sticky notes, frames, images,
// embeds (videos, code links, link cards, websites), entries, comment pins
// with text or handwriting, a laser pointer and export.

import { LitElement, css, html, nothing } from "lit";
import rough from "roughjs";
import { ApiError, api, type Doc, type Entity } from "../api";
import { userColor } from "../collab";
import { ScreenPictures } from "../wb-screen-pictures";
import { uploadFile } from "../files";
import { blockDef, boardKinds } from "../blocks";
import { formatDate, locale, t } from "../i18n";
import { icon, iconSVG } from "../icon";
import { findKind, groupColor, kindName, loadModel, type Model } from "../model";
import { currentUser, navigate } from "../state";
import {
  BOXY, asBlock, LINEAR, PALETTE, isCustomColor, swatchCols, type WBColor, TEXTUAL, alignTo, backKey, bbox, bindTarget, bindingAt, boxOf, codeLabel, embedKind, extent, frontKey, hitTest, inFrame, linePts,
  newId, normalizeBoard, playerURL, removeElement, screenTextLook, sorted, wrapText, type Binding, type Color, type ElementType, type Preview, type Pt, type WBComment, type WBElement, type Whiteboard,
  alignEls, cloneEls, fromExcalidraw, slides, templateEls, toExcalidraw, TEMPLATES, type Template, type Box, distributeEls, groupOf, inBox, shiftEl, snapBox, CLIP_PREFIX, type AlignHow,
} from "../whiteboard";
import { connectBoard, type Live, type Peer, type Status } from "../whiteboard-live";
import type { Comp, Device, WBPrototype } from "../whiteboard";
import { COMPS, DEVICES, PreviewCache, actionsView, drawFlow, drawScreen, type FlowMode, inspector, newProtoId, protoById, screenOf, screensOf, settingsDialog, type InspectorCtx } from "./wb-proto";
import { base } from "./shared";
import { toast } from "./toasts";
import "./block-view";
import "./plugin-block";
import { activePluginBlocks, cachedPluginBlock, pluginBlock } from "../plugin-blocks";

/** A plugin block whose element runs on whiteboards (manifest "live"). */
const liveOnBoard = (kind: string) => {
  const b = pluginBlock(kind);
  return !!(b && b.live && b.element && !b.missing);
};

type Tool = "select" | "hand" | "rect" | "ellipse" | "diamond" | "arrow" | "line" | "pen" | "text" | "sticky" | "frame" | "image" | "embed" | "entity" | "comment" | "laser" | "eraser" | "screen" | "ui";
const TOOLS: (readonly [Tool, string, string] | "|")[] = [
  ["select", "MousePointer2", "V"], ["hand", "Hand", "H"], "|",
  ["rect", "Square", "R"], ["ellipse", "Circle", "O"], ["diamond", "Diamond", "D"], ["arrow", "MoveUpRight", "A"], ["line", "Minus", "L"], ["pen", "Pencil", "P"], "|",
  ["text", "Type", "T"], ["sticky", "StickyNote", "N"], ["frame", "Frame", "F"], ["image", "Image", "I"], ["embed", "Globe", "W"], ["entity", "Waypoints", "E"], "|",
  ["screen", "Smartphone", "S"], ["ui", "Component", "U"], "|",
  ["comment", "MessageSquarePlus", "C"], ["laser", "Wand", "K"], ["eraser", "Eraser", "X"],
];
const VIEW_TOOLS = new Set<Tool>(["select", "hand", "laser"]);
const NS = "http://www.w3.org/2000/svg";
/** Height of the title bar of an app on the board (the app runs below it). */
const WB_APP_BAR = 30;
const XH = "http://www.w3.org/1999/xhtml";
const tc = (c?: string) => (isCustomColor(c) ? c : `var(--tc-${c || "gray"})`);
const tb = (c?: string) => (isCustomColor(c) ? c : `var(--tb-${c || "gray"})`);
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
/** The context menu: where it opened (stage pixels and board point). */
interface Menu { x: number; y: number; wx: number; wy: number }
const MAC = typeof navigator !== "undefined" && /Mac|iP(hone|ad|od)/.test(navigator.platform);
const kbd = (k: string, shift = false) => [MAC ? "⌘" : t("wb.key_ctrl"), shift ? (MAC ? "⇧" : t("wb.key_shift")) : "", k].filter(Boolean).join(MAC ? "" : "+");
interface Pop { kind: "comment" | "newcomment" | "embed" | "entity"; x: number; y: number; id?: string; element?: string }

export class PhxWhiteboard extends LitElement {
  static properties = {
    project: {}, doc: { attribute: false }, canEdit: { type: Boolean }, still: { type: Boolean, reflect: true },
    maximized: { type: Boolean, reflect: true }, standalone: { type: Boolean, reflect: true },
    tool: { state: true }, selId: { state: true }, sel: { state: true }, snap: { state: true }, embedWeb: { state: true }, apps: { state: true }, cards: { state: true }, mermaid: { state: true }, chartEdit: { state: true }, present: { state: true }, status: { state: true }, saved: { state: true }, peers: { state: true }, zoom: { state: true },
    pop: { state: true }, menu: { state: true }, panel: { state: true }, arrowMode: { state: true }, roundDefault: { state: true }, trail: { state: true }, laserSize: { state: true },
    entities: { state: true }, query: { state: true }, busy: { state: true }, boardVersion: { state: true },
    uiComp: { state: true }, screenDevice: { state: true }, screenProto: { state: true }, protoDlg: { state: true }, playing: { state: true }, flows: { state: true },
  };

  static styles = [base, css`
    :host { display: block; position: relative; height: calc(100dvh - var(--header-h) - 150px); min-height: 460px; }
    :host([still]) { height: 320px; min-height: 0; }
    /* Maximized over the app (Esc restores), or its own page (/wb/…) filling the window. */
    :host([maximized]) { position: fixed; inset: 0; z-index: 60; height: auto; min-height: 0; }
    :host([standalone]) { height: 100dvh; min-height: 0; }
    :host([maximized]) .stage, :host([standalone]) .stage { border: 0; border-radius: 0; }
    .home { left: 10px; top: 10px; gap: 4px; padding: 4px 10px 4px 4px; font-size: 13px; max-width: min(320px, 28vw); }
    .home span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-weight: 600; }
    .bar a.ico { width: 34px; height: 34px; display: grid; place-items: center; color: var(--fg); border-radius: 7px; flex: none; }
    @media (hover: hover) and (pointer: fine) { .bar a.ico:hover { background: var(--surface2); } }
    .bar a.ico:focus-visible { outline: 2px solid var(--accent); }
    :host([still]) .stage { border: 0; border-radius: 0; }
    :host([still]) svg.board { pointer-events: none; }
    .stage { position: absolute; inset: 0; border: 1px solid var(--line); border-radius: 12px; overflow: hidden; background: var(--canvas, var(--bg)); touch-action: none; }
    dialog.wbdlg { border: 1px solid var(--line); border-radius: var(--radius-card, 12px); background: var(--surface); color: var(--fg); padding: 20px; width: min(560px, 94vw); box-shadow: 0 18px 50px rgb(0 0 0 / .2); }
    dialog.wbdlg::backdrop { background: rgb(0 0 0 / .35); }
    dialog.wbdlg h2 { font-size: 18px; margin: 0 0 6px; }
    dialog.wbdlg .two { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; margin: 12px 0; }
    dialog.wbdlg label { display: grid; gap: 4px; font-size: 13px; }
    dialog.wbdlg .cardlist { list-style: none; padding: 0; margin: 0 0 14px; display: grid; gap: 6px; max-height: 50vh; overflow: auto; }
    dialog.wbdlg .cardlist li { display: flex; gap: 8px; align-items: center; }
    dialog.wbdlg .cardlist input[type=checkbox] { width: auto; }
    dialog.wbdlg .cardlist input[type=text] { flex: 1; }
    dialog.wbdlg.chart { width: min(1040px, 96vw); }
    dialog.wbdlg textarea { width: 100%; min-height: 200px; box-sizing: border-box; font: 13px/1.5 var(--font-mono); margin: 10px 0 14px; }
    dialog.wbdlg .acts { display: flex; gap: 8px; justify-content: flex-end; }
    .apps { position: absolute; inset: 0; overflow: hidden; pointer-events: none; z-index: 1; }
    .apps .app { position: absolute; transform-origin: 0 0; pointer-events: auto; border-radius: 0 0 10px 10px; overflow: hidden; }
    .apps .app.live { border-radius: 10px; background: var(--surface); box-shadow: 0 0 0 1.5px var(--line); overflow: auto; }
    svg.board { position: absolute; inset: 0; width: 100%; height: 100%; display: block; user-select: none; -webkit-user-select: none; outline: none; }
    svg.board.t-hand, svg.board.panning { cursor: grab; }
    svg.board.panning { cursor: grabbing; }
    svg.board.t-draw { cursor: crosshair; }
    svg.board.t-laser { cursor: crosshair; }
    svg.board.t-comment { cursor: cell; }
    .grid { fill: var(--line); }
    .bar { position: absolute; z-index: 3; display: flex; gap: 2px; align-items: center; padding: 4px; background: var(--surface); border: 1px solid var(--line); border-radius: 10px; box-shadow: 0 6px 20px -12px rgb(0 0 0 / 0.35); }
    .tools { top: 10px; left: 50%; transform: translateX(-50%); max-width: calc(100% - 20px); overflow-x: auto; }
    .zoom button.flows { width: auto; display: inline-flex; align-items: center; gap: 6px; padding: 0 8px; font-size: 12px; white-space: nowrap; }
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
    .stylebar { position: absolute; z-index: 3; right: 10px; top: 56px; width: 244px; max-height: calc(100% - 120px); overflow: auto; padding: 10px; display: grid; gap: 8px;
      background: var(--surface); border: 1px solid var(--line); border-radius: 10px; box-shadow: 0 6px 20px -12px rgb(0 0 0 / 0.35); font-size: 12px; }
    .stylebar h4 { margin: 0; font: 600 11px var(--font-body); color: var(--muted); text-transform: uppercase; letter-spacing: 0.04em; }
    /* One row, or two full rows (swatchCols). */
    .swatches { display: grid; grid-template-columns: repeat(var(--cols, 9), auto); justify-content: start; gap: 3px; }
    .sw.custom { position: relative; display: grid; place-items: center; background: var(--surface2); color: var(--muted); cursor: pointer; overflow: hidden; }
    .sw.custom input { position: absolute; inset: 0; width: 100%; height: 100%; padding: 0; border: 0; opacity: 0; cursor: pointer; }
    .sw.custom.on { outline: 2px solid var(--accent); outline-offset: 1px; }
    .sw.custom:focus-within { outline: 2px solid var(--accent); outline-offset: 1px; }
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
    .menu { position: absolute; z-index: 6; width: 240px; padding: 4px; display: grid; background: var(--surface); border: 1px solid var(--line); border-radius: 10px;
      box-shadow: 0 14px 34px -14px rgb(0 0 0 / 0.45); font-size: 13px; transform-origin: var(--mo, top left); animation: menu-in 120ms var(--ease-out); }
    @keyframes menu-in { from { opacity: 0; transform: scale(0.97); } }
    @media (prefers-reduced-motion: reduce) { .menu { animation: menu-fade 120ms ease; } @keyframes menu-fade { from { opacity: 0; } } }
    .menu .mi { display: flex; align-items: center; gap: 10px; width: 100%; height: 30px; padding: 0 8px; border: 0; border-radius: 6px; background: transparent; color: var(--fg); font-size: 13px; text-align: left; }
    .menu .mi:focus-visible { outline: none; background: var(--surface2); }
    @media (hover: hover) and (pointer: fine) { .menu .mi:hover:not([disabled]) { background: var(--surface2); } }
    .menu .mi[disabled] { opacity: 0.45; }
    .menu .mi .ico { flex: none; color: var(--muted); }
    .menu .mi span { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .menu .mi kbd { flex: none; font: 11px var(--font-mono); color: var(--muted); }
    .menu .mi.danger, .menu .mi.danger .ico { color: var(--bad); }
    .menu .mi .tick { flex: none; display: grid; color: var(--accent); }
    .menu .mi .tick .ico { color: var(--accent); }
    .menu .fmt .sw.none { display: grid; place-items: center; color: var(--muted); background: transparent; }
    .menu hr { border: 0; border-top: 1px solid var(--line); margin: 4px 2px; }
    .menu .fmt { display: grid; gap: 6px; padding: 4px 8px 6px; }
    .menu .fmt .lbl { font-size: 11px; color: var(--muted); }
    .menu .fmt .swatches { gap: 3px; }
    .menu .fmt .sw { width: 19px; height: 19px; border-radius: 5px; }
    .menu .fmt .seg2 button { height: 24px; font-size: 11.5px; }
    .menu .icons { display: flex; gap: 2px; padding: 2px 4px; }
    .menu .icons button { width: 30px; height: 28px; padding: 0; display: grid; place-items: center; border: 0; border-radius: 6px; background: transparent; color: var(--fg); }
    @media (hover: hover) and (pointer: fine) { .menu .icons button:hover { background: var(--surface2); } }
    .menu .icons button:focus-visible { outline: none; background: var(--surface2); }
    .menu .note { padding: 2px 8px 4px; font-size: 11px; color: var(--muted); }
    :host([presenting]) .bar:not(.present), :host([presenting]) .stylebar, :host([presenting]) .panel, :host([presenting]) .pop { display: none !important; }
    .stylebar.proto { width: 252px; }
    .stylebar label { display: grid; gap: 3px; font-size: 12px; color: var(--muted); }
    .stylebar label.check { display: flex; align-items: center; gap: 6px; color: var(--fg); }
    .stylebar label.check input { width: auto; }
    .stylebar input, .stylebar select, .stylebar textarea { width: 100%; box-sizing: border-box; font-size: 12.5px; }
    .stylebar .two { display: grid; grid-template-columns: 1fr 1fr; gap: 6px; }
    .stylebar .act { display: grid; gap: 4px; padding: 6px; border: 1px solid var(--line); border-radius: 8px; position: relative; }
    .stylebar .act > button.danger { justify-self: end; height: 24px; }
    .opts.comps { max-width: calc(100% - 20px); overflow-x: auto; }
    .opts.comps button { width: 30px; height: 30px; }
    .opts select { height: 28px; font-size: 12px; }
    dialog.proto { width: min(640px, 94vw); max-height: 88vh; overflow: auto; }
    dialog.proto h3 { font-size: 14px; margin: 18px 0 2px; }
    dialog.proto p.muted { margin: 0 0 8px; font-size: 12.5px; }
    dialog.proto .plist { list-style: none; margin: 0 0 8px; padding: 0; display: grid; gap: 6px; }
    dialog.proto .plist li { display: grid; grid-template-columns: 1fr 110px 1fr auto; gap: 6px; align-items: center; }
    dialog.proto fieldset.pdata { border: 1px solid var(--line); border-radius: 10px; padding: 10px; margin: 0 0 8px; display: grid; gap: 8px; }
    dialog.proto fieldset.pdata textarea { min-height: 70px; margin: 0; }
    dialog.proto .acts { flex-wrap: wrap; margin-top: 16px; }
    dialog.proto .acts .btn { height: 34px; display: inline-flex; align-items: center; gap: 6px; }
    .bar.present { position: absolute; left: 50%; bottom: 16px; transform: translateX(-50%); display: flex; gap: 8px; align-items: center; font-size: 13px; }
    .bar.present span { padding: 0 6px; color: var(--muted); max-width: 40vw; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  `];

  project = "";
  doc?: Doc;
  canEdit = false;
  /** Read-only picture for page embeds: the editor's own drawing, no live
   * connection and no chrome, sized to the board. */
  still = false;
  /** Fills the browser window over the app (Esc or the button restores). */
  maximized = false;
  /** Shown as a page of its own (/wb/project/doc): a way back instead of maximize. */
  standalone = false;

  private setMaximized(on: boolean) {
    this.maximized = on;
    document.documentElement.style.overflow = on ? "hidden" : "";
  }
  tool: Tool = "select";
  selId: string | null = null;
  /** Several selected elements (box, Shift-click, groups); selId is then null. */
  sel: string[] = [];
  snap = localStorage.getItem("wb.snap") === "1";
  /** Flow arrows between prototype screens: off, of the selected screens (default) or all. */
  flows: FlowMode = (() => { try { const v = localStorage.getItem("wb.flows"); return v === "off" || v === "all" ? v : "sel"; } catch { return "sel"; } })();
  /** Embed dialog: a live website instead of a preview card. */
  embedWeb = false;
  /** Apps of the project for the embed dialog. */
  apps: Doc[] = [];
  /** Sticky notes on their way to becoming cards. */
  cards?: { items: { id: string; title: string; description: string; on: boolean }[]; boards: Doc[]; board: string; columns: { id: string; name: string }[]; column: string };
  /** A Mermaid flowchart to turn into shapes. */
  mermaid?: string;
  /** The block being written in the dialog (a new one without id). */
  chartEdit?: { id?: string; kind: string; src: string };
  /** Live state of entries (slug → ok | warn | down | fail …). */
  private entStatus = new Map<string, string>();
  /** Which websites may be embedded live (instance and organization). */
  private embedRules?: { web: boolean; sites: string[] };
  private liveTimer = 0;
  /** Bring the next change from outside into view (a diagram drawn on request). */
  private showNext = false;
  /** Presentation: index of the frame shown (-1 = off). */
  present = -1;
  status: Status = "connecting";
  saved: { state: "saving" | "saved" | "error"; at?: Date } | null = null;
  peers: Peer[] = [];
  zoom = 100;
  pop: Pop | null = null;
  menu: Menu | null = null;
  panel = false;
  arrowMode: "drag" | "click" = (localStorage.getItem("wb.arrowMode") as "drag" | "click") || "drag";
  roundDefault = localStorage.getItem("wb.round") === "1";
  trail = localStorage.getItem("wb.trail") !== "dot";
  laserSize = Number(localStorage.getItem("wb.laserSize")) || 4.5;
  entities: Entity[] = [];
  query = "";
  busy = false;
  boardVersion = 0;
  /** Prototypes: the component the "ui" tool places, the device and
   * prototype of new screens, the open settings, the prototype playing. */
  uiComp: Comp = "button";
  screenDevice: Exclude<Device, "custom"> = "phone";
  screenProto = "";
  protoDlg = "";
  playing: { proto: string; start?: string } | null = null;
  private previews = new PreviewCache();

  private board: Whiteboard = { elements: [], comments: [] };
  private synced: Whiteboard = { elements: [], comments: [] };
  private live?: Live;
  private view = { x: 80, y: 80, z: 1 };
  private pen: { stroke: WBColor; smooth: boolean } = { stroke: "blue", smooth: false };
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
  private longPress?: { timer: number; x: number; y: number };
  /** A clicked point of the selected line or arrow (Delete removes it). */
  private selPt: { id: string; i: number } | null = null;
  private menuFocus = false;
  private resize?: ResizeObserver;
  private readOnly = true;
  /** Uploads pictures of HTML screens for server-side screenshots. */
  private pictures?: ScreenPictures;

  // ---- lifecycle -------------------------------------------------------------

  connectedCallback() {
    super.connectedCallback();
    window.addEventListener("keydown", this.onKey);
    window.addEventListener("keyup", this.onKeyUp);
    window.addEventListener("paste", this.onPaste);
    window.addEventListener("pointerdown", this.onOutside, true);
    window.addEventListener("blur", this.closeMenu);
    document.addEventListener("fullscreenchange", this.onFullscreen);
    requestAnimationFrame(this.laserFrame);
  }

  disconnectedCallback() {
    removeEventListener("phx-plugin-block-drawn", this.onPluginDrawn);
    super.disconnectedCallback();
    if (this.maximized) this.setMaximized(false); // leaving the page restores scrolling
    window.removeEventListener("keydown", this.onKey);
    window.removeEventListener("keyup", this.onKeyUp);
    window.removeEventListener("paste", this.onPaste);
    window.removeEventListener("pointerdown", this.onOutside, true);
    window.removeEventListener("blur", this.closeMenu);
    document.removeEventListener("fullscreenchange", this.onFullscreen);
    this.resize?.disconnect();
    window.clearInterval(this.liveTimer);
    void this.live?.destroy();
    this.live = undefined;
  }

  willUpdate(changed: Map<string, unknown>) {
    if (changed.has("doc") && this.doc && (changed.get("doc") as Doc | undefined)?.id !== this.doc.id) this.start();
  }

  firstUpdated() {
    this.resize = new ResizeObserver(() => (this.still ? this.fitStill() : this.paint()));
    this.resize.observe(this.svgEl);
  }

  private get svgEl() {
    return this.renderRoot.querySelector("svg.board") as SVGSVGElement;
  }

  private start() {
    void this.live?.destroy();
    this.readOnly = !this.canEdit;
    this.pictures?.stop();
    this.pictures = this.canEdit ? new ScreenPictures(this.project, this.doc!.id) : undefined;
    this.tool = "select";
    this.board = normalizeBoard(this.doc!.content as Partial<Whiteboard>);
    this.synced = this.clone(this.board);
    this.painted = false;
    loadModel(this.project).then((m) => { this.model = m; this.paint(); }).catch(() => {});
    addEventListener("phx-plugin-block-drawn", this.onPluginDrawn);
    // Entries on the board show their name and kind.
    this.entities = [];
    if (this.board.elements.some((e) => e.entity)) void this.loadEntities();
    void this.loadLive();
    window.clearInterval(this.liveTimer);
    this.liveTimer = window.setInterval(() => void this.loadLive(), 60_000);
    if (this.still) {
      this.readOnly = true;
      this.live = undefined;
      requestAnimationFrame(() => this.fitStill());
      return;
    }
    const u = currentUser.get();
    const proto = location.protocol === "https:" ? "wss:" : "ws:";
    this.live = connectBoard({
      url: `${proto}//${location.host}/rt/doc/${this.project}`, project: this.project, doc: this.doc!.id, initial: this.board, readOnly: this.readOnly,
      user: { name: u?.name ?? "?", color: userColor(u?.id ?? "") },
      onChange: (b) => this.remote(b),
      onPeers: (p) => { this.peers = p; this.paintOthers(); },
      onStatus: (s) => (this.status = s),
      onSaved: (state, at) => (this.saved = { state, at }),
      onReset: (by, ai, changed) => {
        void this.loadLive();
        if (!this.showNext) toast(by ? t(ai ? "wb.changed_by_ai" : "wb.changed_by", { name: by }) : t("wb.changed_outside"));
        if (ai) this.showAICursor(by, changed);
        // A diagram this person asked for (neighbourhood, Mermaid): show it.
        if (this.showNext && changed.length) {
          this.showNext = false;
          const b = extent(changed.map((id) => this.byId(id)).filter((e): e is WBElement => !!e), this.byId);
          if (b) this.fitBox(b, 60);
        }
      },
    });
    requestAnimationFrame(() => {
      this.fit();
      this.findFromURL();
      // A play link (?play=<prototype>) starts the prototype.
      const play = new URLSearchParams(location.search).get("play");
      if (play && protoById(this.board, play)) this.playing = { proto: play };
    });
  }

  /** A search hit opens the board with ?find=…: select the first element
   * whose text matches and bring it into view. */
  private findFromURL() {
    const q = new URLSearchParams(location.search).get("find")?.trim().toLowerCase();
    if (!q) return;
    const words = q.split(/\s+/).filter(Boolean);
    const text = (e: WBElement) => [e.text, e.preview?.title, e.preview?.description, e.entity, e.url].filter(Boolean).join(" ").toLowerCase();
    const hit = sorted(this.els()).find((e) => words.every((w) => text(e).includes(w))) ?? sorted(this.els()).find((e) => words.some((w) => text(e).includes(w)));
    if (!hit) return;
    this.select(hit.id);
    const b = bbox(hit, this.byId);
    this.fitBox({ x: b.x - b.w, y: b.y - b.h, w: b.w * 3, h: b.h * 3 }, 40);
  }

  /** Live state on the board: entries (checks, path diagnosis), pull
   * requests and issues through the code connection, embed rules. */
  private async loadLive() {
    const p = this.project;
    if (!p) return;
    if (!this.embedRules) this.embedRules = await api<{ web: boolean; sites: string[] }>("GET", `/projects/${p}/embed-policy`).catch(() => ({ web: true, sites: [] }));
    const slugs = [...new Set(this.els().filter((e) => e.entity).map((e) => e.entity!))].slice(0, 100);
    if (slugs.length) {
      const q = new URLSearchParams();
      for (const sl of slugs) q.append("e", sl);
      const st = await api<{ entries: { slug: string; status: string }[] }>("GET", `/projects/${p}/status?${q}`).catch(() => undefined);
      if (st) this.entStatus = new Map(st.entries.map((h) => [h.slug, h.status]));
    }
    const code = this.els().filter((e) => e.type === "embed" && e.kind === "code" && e.url);
    if (code.length) {
      const res = await api<Record<string, { title: string; status: string }>>("POST", `/projects/${p}/whiteboards/code-status`, { urls: code.map((e) => e.url!) }).catch(() => ({}) as Record<string, { title: string; status: string }>);
      let changed = false;
      for (const e of code) {
        const r = res[e.url!];
        if (!r || (e.preview?.status === r.status && (!r.title || e.preview?.title === r.title))) continue;
        e.preview = { ...(e.preview ?? {}), status: r.status, title: r.title || e.preview?.title };
        changed = true;
      }
      if (changed && !this.readOnly) this.changed();
    }
    this.paint();
  }

  private webAllowed(url: string) {
    const r = this.embedRules;
    if (!r) return true;
    let host = "";
    try { host = new URL(url).hostname.toLowerCase(); } catch { return false; }
    return r.web && (!r.sites.length || r.sites.some((s) => host === s || host.endsWith("." + s)));
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
    if (this.selPt?.id !== id) this.selPt = null;
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
    this.selPt = null;
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

  private pasteElements(json: string, at?: Pt) {
    let list: WBElement[];
    try {
      list = JSON.parse(json) as WBElement[];
    } catch {
      return;
    }
    if (!Array.isArray(list) || !list.length) return;
    const copies = cloneEls(list.filter((e) => e && typeof e.id === "string" && typeof e.type === "string"), 24);
    if (at) {
      // From the context menu: the copies' top left goes where it opened.
      const b = extent(copies, (id) => copies.find((c) => c.id === id));
      if (b) for (const c of copies) shiftEl(c, at[0] - b.x, at[1] - b.y);
    }
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

  /** Screens of the selected elements (flow arrows in "selection" mode). */
  private selScreens(): Set<string> {
    const out = new Set<string>();
    for (const id of this.sel) {
      const e = this.byId(id);
      const s = e && screenOf(this.board, e);
      if (s) out.add(s.id);
    }
    return out;
  }

  private cycleFlows() {
    this.flows = this.flows === "sel" ? "all" : this.flows === "all" ? "off" : "sel";
    try { localStorage.setItem("wb.flows", this.flows); } catch { /* storage blocked */ }
    this.paint();
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
    // Screens are opaque: always behind what is on them.
    const all = sorted(this.els());
    layer.replaceChildren(...[...all.filter((e) => e.screen), ...all.filter((e) => !e.screen)].map((e) => this.drawEl(svg, e)));
    if (!this.still && this.els().some((e) => e.actions?.length)) layer.append(drawFlow(this.board, this.view.z, this.flows, this.selScreens()));
    this.previews.prune(new Set(this.els().filter((e) => e.ui).map((e) => e.id)));
    this.syncApps();
    if (!this.readOnly && !this.still) this.pictures?.schedule(this.board);
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

  private textBlock(g: Element, text: string | undefined, cx: number, cy: number, maxW: number, o: { size?: number; color?: string; anchor?: string; top?: boolean; font?: string; weight?: number } = {}) {
    if (!text) return;
    const size = o.size ?? 20, lines = wrapText(text, maxW, size), lh = size * (o.font ? 1.3 : 1.15);
    const y0 = o.top ? cy : cy - ((lines.length - 1) * lh) / 2;
    const tx = mk("text", { x: cx, y: y0, "text-anchor": o.anchor ?? "middle", "dominant-baseline": o.top ? "hanging" : "central" }, g);
    tx.style.font = `${o.weight ?? 400} ${size}px/1.15 ${o.font ?? HAND}`;
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
      case "ui":
        g.append(this.previews.draw(e, this.board));
        break;
      case "frame": {
        if (e.screen) {
          drawScreen(e, this.board, g);
          break;
        }
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
        // Texts on a screen of a clean prototype use the app's fonts and text styles.
        const look = screenTextLook(e, this.board);
        this.textBlock(g, e.text, ax, y, Math.max(w, 60), { anchor, top: true, size: e.fontSize || 24, color: look?.color ?? tc(e.stroke), font: look?.font, weight: look?.weight });
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
      case "chart":
      case "block": this.drawBlock(e, g); break;
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
    const st = this.entStatus.get(e.entity ?? "");
    const kids: Node[] = [badge, hx("div", "min-width:0;display:grid;gap:2px;flex:1", [name, sub])];
    if (st && st !== "unknown" && st !== "") {
      const col = st === "ok" ? "var(--good)" : st === "warn" ? "var(--signal)" : "var(--bad)";
      kids.push(hx("span", `width:12px;height:12px;flex:none;border-radius:50%;background:${col};box-shadow:0 0 0 3px color-mix(in srgb, ${col} 25%, transparent)`, [], { title: t(`wb.status.${st === "ok" || st === "warn" ? st : "down"}` as any), role: "img", "aria-label": t(`wb.status.${st === "ok" || st === "warn" ? st : "down"}` as any) }));
    }
    fo.append(hx("div", "box-sizing:border-box;width:100%;height:100%;display:flex;gap:10px;align-items:center;padding:0 12px;color:var(--fg)", kids));
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
    if (e.kind === "app") {
      const bar = hx("div", `display:flex;gap:6px;align-items:center;height:${WB_APP_BAR}px;box-sizing:border-box;padding:0 10px;border-bottom:1px solid var(--line);background:var(--surface2);font-size:12px;color:var(--muted)`, [
        ico("AppWindow", 13), hx("span", "flex:1;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;color:var(--fg);font-weight:600", [pv.title || e.doc || ""]),
        hx("a", "color:var(--muted);text-decoration:none", [ico("ExternalLink", 12)], { href: `/p/${this.project}/d/${encodeURIComponent(e.doc ?? "")}`, "data-link": "1", title: t("wb.open_app") })]);
      // A running app lives in the overlay above the drawing (so it keeps
      // its state while the board repaints); here only its place.
      const body = loaded && !this.still
        ? hx("div", "flex:1;background:var(--surface)", [])
        : hx("div", "flex:1;display:grid;place-items:center;align-content:center;gap:6px;text-align:center;padding:12px;cursor:pointer;color:var(--muted);font-size:12px", [ico("Play", 22), hx("b", "color:var(--fg)", [pv.title || t("wb.app")]), t("wb.click_to_start_app")], { "data-load": e.id });
      fo.append(card(bar, body));
      return;
    }
    if (e.kind === "web" && this.webAllowed(e.url ?? "")) {
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

  /** Blocks (ADR-0085) are drawn as SVG (not HTML), so exports keep them;
   * live kinds (e.g. polls) run in the overlay above, like apps. */
  private drawBlock(e: WBElement, g: SVGGElement) {
    const b = asBlock(e);
    const x = b.x ?? 0, y = b.y ?? 0, w = Math.max(200, b.w ?? 0), h = Math.max(120, b.h ?? 0);
    const card = mk("rect", { x, y, width: w, height: h, rx: 10 }, g);
    card.style.cssText = "fill:var(--surface);stroke:var(--line);stroke-width:1.5";
    const def = blockDef(b.kind ?? "");
    const parsed = def?.parse(b.source ?? "");
    if (!def && pluginBlock(b.kind ?? "")) {
      // Live plugin blocks run as an overlay (syncLiveBlocks), like core ones.
      if (liveOnBoard(b.kind ?? "") && !this.still) return;
      // A plugin's block: the server draws it; the picture arrives later and repaints.
      const r = pluginBlock(b.kind!)!.missing ? undefined : cachedPluginBlock(this.project, b.kind!, b.source ?? "", w - 16, locale());
      if (!r?.svg) {
        const tx = mk("text", { x: x + 14, y: y + 26 }, g);
        tx.style.cssText = "font:13px var(--font-body);fill:var(--muted)";
        const info = pluginBlock(b.kind!)!;
        tx.textContent = `${info.label} · ${info.missing ? t("plugin_block.needs", { plugin: info.plugin }) : t(r ? "block.cannot_show" : "plugin_block.drawing")}`;
        return;
      }
      const pic = document.importNode(new DOMParser().parseFromString(r.svg, "image/svg+xml").documentElement, true) as unknown as SVGSVGElement;
      pic.setAttribute("x", String(x + 8));
      pic.setAttribute("y", String(y + 8));
      pic.setAttribute("width", String(w - 16));
      pic.setAttribute("height", String(h - 16));
      g.appendChild(pic);
      return;
    }
    if (!def || !parsed?.model) {
      const tx = mk("text", { x: x + 14, y: y + 26 }, g);
      tx.style.cssText = "font:13px var(--font-body);fill:var(--bad)";
      tx.textContent = `${b.kind ?? ""}: ${t("block.cannot_show")}`;
      return;
    }
    if (def.board?.live && !this.still) return; // the overlay shows it
    const markup = def.svg(parsed.model, { width: w - 16, height: h - 16, locale: locale() });
    const inner = document.importNode(new DOMParser().parseFromString(markup, "image/svg+xml").documentElement, true) as unknown as SVGSVGElement;
    inner.setAttribute("x", String(x + 8));
    inner.setAttribute("y", String(y + 8));
    inner.removeAttribute("style");
    inner.style.fontFamily = "var(--font-body)";
    g.appendChild(inner);
  }

  private onPluginDrawn = () => this.paint();

  /** Running apps: frames in an overlay, positioned over their cards. */
  private appFrames = new Map<string, HTMLElement>();

  private syncApps() {
    const host = this.renderRoot.querySelector<HTMLElement>(".apps");
    if (!host) return;
    const want = new Set(this.still ? [] : this.els().filter((e) => e.type === "embed" && e.kind === "app" && e.doc && this.loaded.has(e.id)).map((e) => e.id));
    for (const [id, el] of this.appFrames) if (!want.has(id)) { el.remove(); this.appFrames.delete(id); }
    const z = this.view.z;
    for (const id of want) {
      const e = this.byId(id)!;
      let el = this.appFrames.get(id);
      if (!el) {
        void import("./apps");
        el = document.createElement("div");
        el.className = "app";
        const embed = document.createElement("phx-wb-app") as HTMLElement & { project: string; app: string };
        embed.project = this.project;
        embed.app = e.doc!;
        el.append(embed);
        host.append(el);
        this.appFrames.set(id, el);
      }
      const [sx, sy] = this.toScreen(e.x ?? 0, (e.y ?? 0) + WB_APP_BAR);
      el.style.cssText = `left:${sx}px;top:${sy}px;width:${e.w ?? 0}px;height:${Math.max(0, (e.h ?? 0) - WB_APP_BAR)}px;transform:scale(${z})`;
    }
    this.syncLiveBlocks(host, z);
  }

  /** Live blocks (e.g. polls) run in the overlay, positioned over their card;
   * their state is kept per element of this whiteboard. */
  private liveBlocks = new Map<string, HTMLElement & { source?: string; render?: () => Promise<void> }>();

  private syncLiveBlocks(host: HTMLElement, z: number) {
    const want = new Set(this.still ? [] : this.els().filter((e) => e.type === "block" && (blockDef(e.kind ?? "")?.board?.live || liveOnBoard(e.kind ?? ""))).map((e) => e.id));
    for (const [id, el] of this.liveBlocks) if (!want.has(id)) { el.remove(); this.liveBlocks.delete(id); }
    for (const id of want) {
      const e = this.byId(id)!;
      let el = this.liveBlocks.get(id);
      if (!el) {
        // Core blocks and plugin blocks (their element runs inside <phx-plugin-block>).
        el = document.createElement(blockDef(e.kind ?? "") ? "phx-block" : "phx-plugin-block") as HTMLElement & { source?: string; render?: () => Promise<void> };
        el.className = "app live";
        el.setAttribute("data-board", "");
        el.dataset.project = this.project;
        el.dataset.doc = this.doc?.id ?? "";
        el.dataset.key = e.id;
        el.dataset.kind = e.kind ?? "";
        host.append(el);
        this.liveBlocks.set(id, el);
      }
      if (el.source !== e.source) {
        el.source = e.source;
        void el.render?.();
      }
      const [sx, sy] = this.toScreen(e.x ?? 0, e.y ?? 0);
      el.style.cssText = `left:${sx}px;top:${sy}px;width:${e.w ?? 0}px;height:${e.h ?? 0}px;transform:scale(${z})`;
    }
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
        if (this.selPt?.id === e.id && this.selPt.i === i) {
          // The chosen point: larger, with a ring.
          h.setAttribute("r", String(7 / this.view.z));
          h.style.fill = "var(--accent)";
          h.style.stroke = "var(--surface)";
          const ring = mk("circle", { cx: p[0], cy: p[1], r: 10 / this.view.z }, g);
          ring.style.cssText = `fill:none;stroke:var(--accent);stroke-width:${1.5 / this.view.z};pointer-events:none`;
        }
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
    if (this.still) return;
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

  /** The AI's own cursor: it visits what the AI just drew or changed. */
  private aiCursor?: { name: string; at: Pt; until: number };
  private aiTimer = 0;

  private showAICursor(by: string, changed: string[]) {
    const pts: Pt[] = changed.map((id) => this.byId(id)).filter((e): e is WBElement => !!e).slice(0, 12).map((e) => {
      const b = bbox(e, this.byId);
      return [b.x + b.w * 0.6, b.y + b.h * 0.6] as Pt;
    });
    if (!pts.length) return;
    const name = t("wb.ai_cursor", { name: by || "AI" });
    window.clearTimeout(this.aiTimer);
    const step = (i: number) => {
      this.aiCursor = { name, at: pts[i], until: Date.now() + 2500 };
      this.paintOthers();
      if (i + 1 < pts.length) this.aiTimer = window.setTimeout(() => step(i + 1), reduced() ? 0 : 380);
      else this.aiTimer = window.setTimeout(() => { this.aiCursor = undefined; this.paintOthers(); }, 2500);
    };
    step(reduced() ? pts.length - 1 : 0);
  }

  private paintOthers() {
    const layer = this.svgEl?.querySelector("g.others");
    if (!layer) return;
    layer.replaceChildren();
    const ai = this.aiCursor;
    const others = ai ? [...this.peers, { id: -1, name: ai.name, color: "var(--accent)", cursor: ai.at, ai: true }] : this.peers;
    for (const p of others as (Peer & { ai?: boolean })[]) {
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

  /** Still mode: the height follows the board's shape, the board fills it. */
  private fitStill() {
    const r = this.svgEl?.getBoundingClientRect(), b = extent(this.els(), this.byId);
    if (!r?.width || !b) return;
    const pad = 24, z = Math.min(1, (r.width - 2 * pad) / Math.max(b.w, 1));
    const h = Math.round(Math.min(560, Math.max(120, b.h * z + 2 * pad)));
    if (Math.abs(this.offsetHeight - h) > 1) this.style.height = `${h}px`;
    const zz = Math.min(z, (h - 2 * pad) / Math.max(b.h, 1));
    this.view = { z: zz, x: (r.width - b.w * zz) / 2 - b.x * zz, y: (h - b.h * zz) / 2 - b.y * zz };
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

  /** The middle button pans the board (onDown); without this the browser
   * starts scrolling the page instead (autoscroll). */
  private noAutoscroll = (ev: MouseEvent) => {
    if (ev.button === 1) ev.preventDefault();
  };

  /** The middle button over blocks, embeds and apps on the board pans it too. */
  private onStageDown = (ev: PointerEvent) => {
    if (ev.button !== 1 || this.svgEl.contains(ev.composedPath()[0] as Node)) return;
    this.onDown(ev);
  };

  private onDown = (ev: PointerEvent) => {
    const svg = this.svgEl;
    if (this.present >= 0) {
      if (ev.button === 0) this.presentAt(this.present + 1);
      return;
    }
    if (ev.button === 2) return; // the context menu opens on "contextmenu"
    this.menu = null;
    if (this.editing) {
      (this.renderRoot.querySelector(".editbox div") as HTMLElement | null)?.blur();
      return;
    }
    if (ev.pointerType === "touch") {
      // A long press opens the context menu (iOS fires no contextmenu event).
      clearTimeout(this.longPress?.timer);
      const { clientX, clientY } = ev;
      this.longPress = { x: clientX, y: clientY, timer: window.setTimeout(() => { this.longPress = undefined; this.drag = null; this.openMenu(clientX, clientY); }, 550) };
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
    const play = target.closest?.("[data-play]") as SVGElement | null;
    if (play) {
      // "▶ from here" on a screen title.
      this.playing = { proto: play.dataset.proto!, start: play.dataset.play };
      return;
    }
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
      void api<Doc[]>("GET", `/projects/${this.project}/documents?type=app`).then((l) => (this.apps = l ?? [])).catch(() => (this.apps = []));
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
        this.selPt = { id: e.id, i: Number(pEl.dataset.pt) };
        this.drag = { mode: "point", id: e.id, i: Number(pEl.dataset.pt) };
        this.paint();
        this.requestUpdate();
        return;
      }
      if (e && mEl) {
        const i = Number(mEl.dataset.mid), pts = linePts(e, this.byId);
        e.points = e.points!.map((p, k) => (k === 0 || k === e.points!.length - 1 ? pts[k] : p));
        e.points.splice(i, 0, [x, y]);
        this.selPt = { id: e.id, i };
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
    if (this.tool === "screen") {
      this.placeScreen(x, y);
      return;
    }
    if (this.tool === "ui") {
      this.placeComp(x, y);
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
    if (this.longPress && Math.hypot(ev.clientX - this.longPress.x, ev.clientY - this.longPress.y) > 8) {
      clearTimeout(this.longPress.timer);
      this.longPress = undefined;
    }
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
    clearTimeout(this.longPress?.timer);
    this.longPress = undefined;
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
    if (h?.type === "chart" || h?.type === "block") {
      this.select(h.id);
      void this.openChart(h);
      return;
    }
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
    this.menu = null;
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
    if (!this.isConnected || this.still || this.playing || this.typing(ev) || !this.svgEl) return;
    const k = ev.key.toLowerCase(), mod = ev.ctrlKey || ev.metaKey;
    if (this.present >= 0) {
      if (["arrowright", "arrowdown", " ", "pagedown", "enter"].includes(k)) { ev.preventDefault(); this.presentAt(this.present + 1); }
      else if (["arrowleft", "arrowup", "pageup", "backspace"].includes(k)) { ev.preventDefault(); this.presentAt(Math.max(0, this.present - 1)); }
      else if (k === "escape") { ev.preventDefault(); this.stopPresenting(); }
      return;
    }
    if (ev.key === "ContextMenu" || (ev.shiftKey && ev.key === "F10")) {
      ev.preventDefault();
      this.menuAtSelection();
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
      // Nothing else to cancel: leave the maximized view.
      if (this.maximized && !this.pop && !this.selId && this.tool === "select") { this.setMaximized(false); return; }
      this.pop = null;
      this.select(null);
      this.setTool("select");
      return;
    }
    if ((ev.key === "Delete" || ev.key === "Backspace") && this.selPt && this.selId === this.selPt.id && !this.readOnly) {
      ev.preventDefault();
      if (!this.removePoint()) this.removeMany(this.selection().filter((e) => !e.locked).map((e) => e.id));
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
    if (!this.isConnected || this.still || this.readOnly || this.typing(ev)) return;
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
    // Innermost var() first, so fallbacks like var(--canvas, var(--bg)) resolve too.
    const resolve = (s: string): string => {
      for (let i = 0; i < 6 && s.includes("var("); i++) {
        s = s.replace(/var\((--[\w-]+)(?:,\s*([^()]*))?\)/g, (_m, v: string, fb?: string) => cs.getPropertyValue(v).trim() || fb?.trim() || "#888");
      }
      return s;
    };
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
      tx.textContent = (fo.getAttribute("data-label") ?? fo.textContent ?? "").trim().slice(0, 60);
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

  /** The last swatch: a custom colour from the colour picker (#rrggbb). Its
   * picker starts at the current colour as the theme draws it. */
  private customSwatch(cur: string | undefined, token: "tc" | "tb", apply: (hex: WBColor) => void) {
    const on = isCustomColor(cur);
    const themed = getComputedStyle(this).getPropertyValue(`--${token}-${cur || "gray"}`).trim();
    const value = (on ? cur : isCustomColor(themed) ? themed : "#808080").toLowerCase();
    return html`<label class=${on ? "sw custom on" : "sw custom"} style=${on ? `background:${cur}` : ""} title=${t("wb.custom_color")}>
      ${on ? nothing : icon("Pipette", 12)}
      <input type="color" .value=${value} aria-label=${t("wb.custom_color")} @click=${(ev: Event) => ev.stopPropagation()}
        @input=${(ev: Event) => apply((ev.target as HTMLInputElement).value as WBColor)}></label>`;
  }

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
    if (e.ui || e.screen) return inspector(this.inspCtx(e));
    const onScreen = e.type !== "frame" && !!screenOf(this.board, e);
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
      ${strokeable ? html`<h4>${t("wb.stroke")}</h4><div class="swatches" style="--cols:${swatchCols(PALETTE.length + 1, 9)}">${PALETTE.map((c) => html`<button class="sw" style="background:${tc(c)}" aria-label=${t(`color.${c}` as any)} aria-pressed=${(e.stroke ?? "gray") === c ? "true" : "false"}
        @click=${() => { set({ stroke: c }); this.pen.stroke = c; }}></button>`)}${this.customSwatch(e.stroke ?? "gray", "tc", (hex) => { set({ stroke: hex }); this.pen.stroke = hex; })}</div>` : nothing}
      ${shape || e.type === "sticky" ? html`<h4>${t("wb.fill")}</h4><div class="swatches" style="--cols:${swatchCols(PALETTE.length + 1, 9)}">${PALETTE.map((c) => html`<button class="sw" style="background:${tb(c)}" aria-label=${t(`color.${c}` as any)} aria-pressed=${e.fill === c ? "true" : "false"}
        @click=${() => set({ fill: c })}></button>`)}${this.customSwatch(e.fill ?? (e.type === "sticky" ? "yellow" : "gray"), "tb", (hex) => set({ fill: hex }))}</div>
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
        ${lin && this.selPt?.id === e.id ? html`<button class="ghost danger" title=${t("wb.menu.delete_point")} aria-label=${t("wb.menu.delete_point")} ?disabled=${(e.points?.length ?? 0) <= 2 || e.locked} @click=${() => this.removePoint()}>${icon("CircleMinus", 14)}</button>` : nothing}
        <button class="ghost danger" title=${t("wb.delete")} aria-label=${t("wb.delete")} @click=${() => this.removeEl(e.id)}>${icon("Trash2", 14)}</button>
        ${e.entity ? html`<a class="btn ghost" style="height:28px;padding:0 8px;font-size:12px" href="/p/${this.project}/e/${e.entity}" @click=${(ev: MouseEvent) => { if (!ev.ctrlKey && !ev.metaKey && ev.button === 0) { ev.preventDefault(); navigate(`/p/${this.project}/e/${e.entity}`); } }}>${icon("ExternalLink", 13)} ${t("wb.open_entry")}</a>` : nothing}
      </div>
      ${onScreen ? html`<div @keydown=${(ev: Event) => ev.stopPropagation()} style="display:grid;gap:6px">${actionsView(this.inspCtx(e), false)}</div>` : nothing}
    </aside>`;
  }

  // ---- prototypes --------------------------------------------------------------

  private inspCtx(e: WBElement): InspectorCtx {
    return {
      e, board: this.board,
      set: (patch) => {
        const cur = this.byId(e.id);
        if (!cur) return;
        for (const [k, v] of Object.entries(patch)) {
          if (v === undefined) delete (cur as unknown as Record<string, unknown>)[k];
          else (cur as unknown as Record<string, unknown>)[k] = v;
        }
        this.changed();
        this.requestUpdate();
      },
      setProto: (id, patch) => this.setProto(id, patch),
      play: (proto, screen) => (this.playing = { proto, start: screen }),
      settings: (proto) => void this.openProto(proto),
    };
  }

  /** Changes a prototype (null deletes it; its screens become frames). */
  private setProto(id: string, patch: Partial<WBPrototype> | null) {
    const list = this.board.prototypes ?? [];
    if (patch === null) {
      this.board.prototypes = list.filter((p) => p.id !== id);
      for (const e of this.els()) if (e.screen?.proto === id) delete e.screen;
    } else {
      this.board.prototypes = list.map((p) => {
        if (p.id !== id) return p;
        const n = { ...p, ...patch } as Record<string, unknown>;
        for (const [k, v] of Object.entries(n)) if (v === undefined) delete n[k];
        return n as unknown as WBPrototype;
      });
    }
    this.changed();
    this.requestUpdate();
  }

  private async openProto(id: string) {
    this.protoDlg = id;
    await this.updateComplete;
    const d = this.renderRoot.querySelector<HTMLDialogElement>("dialog.proto");
    if (d && !d.open) d.showModal();
  }

  private closeProto = () => {
    this.renderRoot.querySelector<HTMLDialogElement>("dialog.proto")?.close();
    this.protoDlg = "";
  };

  /** A new screen of the device chosen, in the chosen (or a new) prototype. */
  private placeScreen(x: number, y: number) {
    let proto = this.screenProto === "+new" ? undefined : protoById(this.board, this.screenProto) ?? (this.board.prototypes ?? [])[0];
    if (!proto) {
      proto = { id: newProtoId(this.board), name: t("proto.default_name", { n: (this.board.prototypes ?? []).length + 1 }) };
      this.board.prototypes = [...(this.board.prototypes ?? []), proto];
      this.screenProto = proto.id;
    }
    const [w, h] = DEVICES[this.screenDevice];
    const n = screensOf(this.board, proto.id).length + 1;
    const e: WBElement = { id: newId(), type: "frame", x, y, w, h, text: t("proto.screen_n", { n }), screen: { proto: proto.id, device: this.screenDevice } };
    this.add(e, true);
    this.tool = "select";
    this.select(e.id);
    this.changed();
  }

  /** A component at a point; on a screen it fits the screen's width. */
  private placeComp(x: number, y: number) {
    const def = COMPS.find((c) => c.comp === this.uiComp) ?? COMPS[0];
    const probe: WBElement = { id: "", type: "ui", x, y, w: 1, h: 1 };
    const scr = screenOf(this.board, probe);
    let w = def.w, h = def.h;
    if (scr) w = Math.min(w, (scr.w ?? w) - 32);
    if (scr && def.comp === "navbar") {
      x = scr.x ?? x;
      y = (scr.y ?? y) + (scr.h ?? 0) - h;
      w = scr.w ?? w;
    }
    const opts = [1, 2, 3].map((n) => t("proto.option_n", { n }));
    const ui: WBElement["ui"] = { comp: def.comp };
    if (["radio", "select", "tabs", "navbar", "list"].includes(def.comp)) ui.options = opts;
    if (def.comp === "navbar") ui.fields = ["house", "search", "user"];
    if (def.comp === "icon") ui.icon = "star";
    const labelled = ["button", "link", "checkbox", "switch", "card", "toast", "badge", "slider", "avatar"].includes(def.comp);
    const e: WBElement = { id: newId(), type: "ui", x, y, w, h, ui, frame: scr?.id, text: labelled ? t(`proto.c.${def.comp}` as any) : undefined };
    this.add(e);
    this.tool = "select";
    this.select(e.id);
    this.changed();
  }

  private protoView() {
    const protos = this.board.prototypes ?? [];
    return html`
      ${this.protoDlg && protoById(this.board, this.protoDlg) ? settingsDialog({
        board: this.board, id: this.protoDlg, project: this.project, doc: this.doc!.id, canEdit: !this.readOnly,
        setProto: (id, patch) => this.setProto(id, patch), pick: (id) => (this.protoDlg = id),
        play: (proto) => (this.playing = { proto }), close: this.closeProto,
      }) : nothing}
      ${this.playing && protoById(this.board, this.playing.proto) ? html`<phx-proto-play .project=${this.project} .doc=${this.doc!.id} .board=${this.board} .version=${this.boardVersion}
        .proto=${this.playing.proto} .start=${this.playing.start ?? ""} ?canComment=${!this.still && protos.length > 0} @close=${() => (this.playing = null)}></phx-proto-play>` : nothing}`;
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
        ${this.apps.length ? html`<div class="as"><span>${t("wb.embed_app")}</span>
          <select aria-label=${t("wb.embed_app")} @keydown=${(e: KeyboardEvent) => e.stopPropagation()}>${this.apps.map((d) => html`<option value=${d.slug}>${d.title}</option>`)}</select>
          <button class="ghost" @click=${(e: Event) => this.placeApp((e.target as HTMLElement).closest(".as")!.querySelector("select")!.value, p.x, p.y)}>${icon("AppWindow", 14)} ${t("wb.embed_app_button")}</button></div>` : nothing}
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

  private placeApp(slug: string, x: number, y: number) {
    const d = this.apps.find((a) => a.slug === slug);
    if (!d) return;
    const e: WBElement = { id: newId(), type: "embed", kind: "app", doc: d.slug, preview: { title: d.title }, x, y, w: 420, h: 320 };
    this.add(e);
    this.pop = null;
    this.tool = "select";
    this.select(e.id);
    this.changed();
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

  // ---- context menu ----------------------------------------------------------------------

  private onContextMenu = (ev: MouseEvent) => {
    ev.preventDefault();
    if (this.present >= 0) return;
    const pt = (ev.composedPath()[0] as Element).closest?.("[data-pt]") as HTMLElement | null;
    const keep = pt && this.selId ? { id: this.selId, i: Number(pt.dataset.pt) } : null;
    this.openMenu(ev.clientX, ev.clientY);
    if (keep && this.selId === keep.id) {
      this.selPt = keep;
      this.paint();
    }
  };

  private onOutside = (ev: PointerEvent) => {
    if (!this.menu) return;
    const m = this.renderRoot.querySelector(".menu");
    if (m && !ev.composedPath().includes(m)) this.menu = null;
  };

  private closeMenu = () => {
    this.menu = null;
  };

  /** Opens the menu at a screen point; what lies there becomes the selection. */
  private openMenu(cx: number, cy: number) {
    const svg = this.svgEl;
    if (!svg) return;
    if (this.editing) (this.renderRoot.querySelector(".editbox div") as HTMLElement | null)?.blur();
    if (this.clickLine) {
      this.finishClickLine();
      this.tool = "select";
    }
    this.pop = null;
    this.drag = null;
    const [x, y] = this.toWorld(cx, cy);
    const h = hitTest(this.els(), x, y, 8 / this.view.z, this.byId);
    if (h && !this.sel.includes(h.id)) {
      const group = this.withGroups([h.id]);
      if (group.length > 1) this.selectMany(group); else this.select(h.id);
    } else if (!h) this.select(null);
    const r = svg.getBoundingClientRect();
    this.menu = { x: cx - r.left, y: cy - r.top, wx: x, wy: y };
    this.menuFocus = true;
  }

  /** Keyboard (menu key, Shift+F10): opens at the selection or the middle. */
  private menuAtSelection() {
    const r = this.svgEl.getBoundingClientRect(), sel = this.selection();
    let cx = r.left + r.width / 2, cy = r.top + r.height / 2;
    if (sel.length) {
      const b = extent(sel, this.byId);
      if (b) {
        const [sx, sy] = this.toScreen(b.x + b.w / 2, b.y + b.h / 2);
        cx = r.left + sx;
        cy = r.top + sy;
      }
    }
    const keep = [...this.sel];
    this.openMenu(cx, cy);
    if (keep.length) this.selectMany(keep);
  }

  /** Keeps the menu inside the board; it opens towards the free side. */
  private placeMenu() {
    const m = this.renderRoot.querySelector(".menu") as HTMLElement | null;
    if (!m || !this.menu) return;
    const st = this.renderRoot.querySelector(".stage") as HTMLElement, W = st.clientWidth, H = st.clientHeight, w = m.offsetWidth, h = m.offsetHeight;
    const left = this.menu.x + w + 8 > W ? Math.max(8, this.menu.x - w) : this.menu.x;
    const top = this.menu.y + h + 8 > H ? Math.max(8, this.menu.y - h) : this.menu.y;
    m.style.left = `${left}px`;
    m.style.top = `${top}px`;
    m.style.setProperty("--mo", `${left < this.menu.x ? "right" : "left"} ${top < this.menu.y ? "bottom" : "top"}`);
    if (this.menuFocus) {
      this.menuFocus = false;
      (m.querySelector("button:not([disabled])") as HTMLElement | null)?.focus();
    }
  }

  private menuKey(ev: KeyboardEvent) {
    ev.stopPropagation();
    const list = [...this.renderRoot.querySelectorAll<HTMLButtonElement>(".menu button:not([disabled])")];
    const i = list.indexOf((this.renderRoot as ShadowRoot).activeElement as HTMLButtonElement);
    const go = (n: number) => {
      ev.preventDefault();
      list[(n + list.length) % list.length]?.focus();
    };
    if (ev.key === "ArrowDown") go(i + 1);
    else if (ev.key === "ArrowUp") go(i < 0 ? list.length - 1 : i - 1);
    else if (ev.key === "Home") go(0);
    else if (ev.key === "End") go(list.length - 1);
    else if (ev.key === "Escape" || ev.key === "Tab") {
      ev.preventDefault();
      this.menu = null;
      this.svgEl.focus();
    }
  }

  /** Removes the chosen point of a line; false when only two points are left. */
  private removePoint(): boolean {
    const p = this.selPt, e = p ? this.byId(p.id) : undefined;
    if (!p || !e?.points || e.locked || e.points.length <= 2) return false;
    const n = e.points.length;
    // Bound ends: the end that loses its point also loses its binding.
    e.points = linePts(e, this.byId).map((q, k) => (k === 0 || k === n - 1 ? q : e.points![k]));
    if (p.i === 0) delete e.start;
    if (p.i === n - 1) delete e.end;
    e.points.splice(p.i, 1);
    this.selPt = { id: e.id, i: Math.min(p.i, e.points.length - 1) };
    this.changed();
    return true;
  }

  /** Applies a change to every selected element that is not locked. */
  private applySel(fn: (e: WBElement) => void) {
    for (const e of this.selection()) if (!e.locked) fn(e);
    this.changed();
  }

  /** Front/back for the whole selection, keeping its own order. */
  private orderSel(front: boolean) {
    const sel = sorted(this.selection());
    for (const e of front ? sel : sel.reverse()) e.order = front ? frontKey(this.els()) : backKey(this.els());
    this.changed();
  }

  private async pasteAt(at: Pt) {
    let text = "";
    try {
      text = (await navigator.clipboard?.readText()) ?? "";
    } catch {
      /* no permission: use the last copy from this board */
    }
    text = text.trim();
    if (text.startsWith(CLIP_PREFIX)) this.pasteElements(text.slice(CLIP_PREFIX.length), at);
    else if (/^https?:\/\/\S+$/.test(text)) void this.embed(text, at[0], at[1], false);
    else if (this.clip) this.pasteElements(this.clip.slice(CLIP_PREFIX.length), at);
  }

  private addAt(type: "sticky" | "text", x: number, y: number) {
    const e: WBElement = type === "text"
      ? { id: newId(), type, stroke: this.pen.stroke, smooth: this.pen.smooth, x, y: y - 14, w: 240, h: 34, text: "" }
      : { id: newId(), type, x: x - 80, y: y - 60, w: 160, h: 120, text: "", fill: "yellow" };
    this.add(e);
    this.setTool("select");
    this.select(e.id);
    this.changed();
    this.editText(e);
  }

  private async drawNeighbourhood(entity: string) {
    try {
      this.showNext = true;
      await api("POST", `/projects/${this.project}/documents/${this.doc!.id}/whiteboard/neighbourhood`, { entity });
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    }
  }

  private async openCards() {
    const items = this.selection().filter((e) => e.type === "sticky" && e.text?.trim()).map((e) => {
      const [first, ...rest] = e.text!.trim().split("\n");
      return { id: e.id, title: first.trim().slice(0, 200), description: rest.join("\n").trim(), on: true };
    });
    const boards = (await api<Doc[]>("GET", `/projects/${this.project}/documents?type=board`).catch(() => [])) ?? [];
    if (!boards.length) {
      toast(t("wb.cards_no_board"), "error");
      return;
    }
    this.cards = { items, boards, board: boards[0].slug, columns: [], column: "" };
    await this.loadColumns(boards[0].slug);
    await this.updateComplete;
    this.renderRoot.querySelector<HTMLDialogElement>("dialog.cards")?.showModal();
  }

  private async loadColumns(board: string) {
    if (!this.cards) return;
    const d = await api<Doc>("GET", `/projects/${this.project}/documents/${encodeURIComponent(board)}`).catch(() => undefined);
    const columns = ((d?.content as { columns?: { id: string; name: string; done?: boolean }[] } | undefined)?.columns ?? []).filter((c) => !c.done);
    this.cards = { ...this.cards, board, columns, column: columns[0]?.id ?? "" };
  }

  private async createCards() {
    const c = this.cards;
    if (!c) return;
    const list = c.items.filter((i) => i.on && i.title);
    this.busy = true;
    let n = 0;
    try {
      for (const i of list) {
        await api("POST", `/projects/${this.project}/boards/${encodeURIComponent(c.board)}/tasks`, { title: i.title, description: i.description, column: c.column, documents: [this.doc!.id] });
        n++;
      }
      toast(t("wb.cards_created", { count: n }));
      this.renderRoot.querySelector<HTMLDialogElement>("dialog.cards")?.close();
      this.cards = undefined;
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    } finally {
      this.busy = false;
    }
  }

  private cardsDialog() {
    const c = this.cards;
    if (!c) return nothing;
    const close = () => { this.renderRoot.querySelector<HTMLDialogElement>("dialog.cards")?.close(); this.cards = undefined; };
    const set = (i: number, patch: Partial<{ title: string; on: boolean }>) => { const items = [...c.items]; items[i] = { ...items[i], ...patch }; this.cards = { ...c, items }; };
    return html`<dialog class="cards wbdlg" aria-labelledby="wbct" @cancel=${(e: Event) => { e.preventDefault(); close(); }} @keydown=${(e: KeyboardEvent) => e.stopPropagation()}>
      <h2 id="wbct">${t("wb.cards_title")}</h2>
      <p class="muted">${t("wb.cards_text")}</p>
      <div class="two">
        <label>${t("wb.cards_board")}<select @change=${(e: Event) => void this.loadColumns((e.target as HTMLSelectElement).value)}>${c.boards.map((b) => html`<option value=${b.slug} ?selected=${b.slug === c.board}>${b.title}</option>`)}</select></label>
        <label>${t("wb.cards_column")}<select @change=${(e: Event) => (this.cards = { ...c, column: (e.target as HTMLSelectElement).value })}>${c.columns.map((col) => html`<option value=${col.id} ?selected=${col.id === c.column}>${col.name}</option>`)}</select></label>
      </div>
      <ul class="cardlist">${c.items.map((i, k) => html`<li><input type="checkbox" .checked=${i.on} aria-label=${t("wb.cards_take")} @change=${(e: Event) => set(k, { on: (e.target as HTMLInputElement).checked })}>
        <input type="text" .value=${i.title} aria-label=${t("wb.cards_card_title")} @input=${(e: Event) => set(k, { title: (e.target as HTMLInputElement).value })}></li>`)}</ul>
      <div class="acts"><button class="ghost" @click=${close}>${t("doc.cancel")}</button>
        <button ?disabled=${this.busy || !c.items.some((i) => i.on && i.title.trim())} @click=${() => void this.createCards()}>${icon("SquareKanban", 14)} ${t("wb.cards_create", { count: c.items.filter((i) => i.on && i.title.trim()).length })}</button></div>
    </dialog>`;
  }

  /** Edits a block (or adds one) in the full-screen block editor. */
  private async openChart(e?: WBElement, kind = "chart") {
    const b = e ? asBlock(e) : undefined;
    const k = b?.kind ?? kind;
    const { openBlockEditor } = await import("./block-editor");
    const kinds = b ? undefined : [
      ...boardKinds().map((d) => ({ kind: d.kind, label: t(d.label as any), example: d.example })),
      ...activePluginBlocks().filter((p) => p.board).map((p) => ({ kind: p.kind, label: p.label, example: p.example ?? "", plugin: true })),
    ];
    const r = await openBlockEditor({ kind: k, source: b?.source ?? blockDef(k)?.example ?? pluginBlock(k)?.example ?? "", title: b ? undefined : t("wb.block_title"),
      project: this.project, doc: this.doc?.id, kinds, plugin: !blockDef(k) });
    if (!r) return;
    this.chartEdit = { id: b?.id, kind: r.kind, src: r.source };
    this.saveChart();
  }

  private saveChart() {
    const c = this.chartEdit;
    if (!c) return;
    const def = blockDef(c.kind);
    const { model, problems } = def?.parse(c.src) ?? { problems: [{ key: "block.cannot_show" }] };
    if (!model) {
      toast(`${t("block.cannot_show")}: ${t(problems[0].key as any, problems[0].params ?? {})}`, "error");
      return;
    }
    const old = c.id ? this.byId(c.id) : undefined;
    if (old) {
      // Older chart elements become blocks when edited.
      Object.assign(old, { type: "block", kind: c.kind, source: c.src });
      delete old.chart;
    } else {
      const r = this.svgEl.getBoundingClientRect();
      const [cx, cy] = this.toWorld(r.left + r.width / 2, r.top + r.height / 2);
      const size = def?.board ?? pluginBlock(c.kind)?.size ?? { w: 560, h: 360 };
      const el: WBElement = { id: newId(), type: "block", kind: c.kind, source: c.src, x: cx - size.w / 2, y: cy - size.h / 2, w: size.w, h: size.h };
      this.add(el);
      this.select(el.id);
    }
    this.changed();
    this.chartEdit = undefined;
  }

  private async openMermaid() {
    this.mermaid = "flowchart LR\n  A[Idea] --> B{Decide}\n  B -->|yes| C[Build]\n  B -->|no| D[Drop]";
    await this.updateComplete;
    this.renderRoot.querySelector<HTMLDialogElement>("dialog.mermaid")?.showModal();
  }

  private async importMermaid() {
    const src = this.renderRoot.querySelector<HTMLTextAreaElement>("dialog.mermaid textarea")?.value ?? "";
    this.busy = true;
    try {
      this.showNext = true;
      await api("POST", `/projects/${this.project}/whiteboards/mermaid`, { source: src, document: this.doc!.id });
      this.renderRoot.querySelector<HTMLDialogElement>("dialog.mermaid")?.close();
      this.mermaid = undefined;
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    } finally {
      this.busy = false;
    }
  }

  private mermaidDialog() {
    if (this.mermaid === undefined) return nothing;
    const close = () => { this.renderRoot.querySelector<HTMLDialogElement>("dialog.mermaid")?.close(); this.mermaid = undefined; };
    return html`<dialog class="mermaid wbdlg" aria-labelledby="wbmt" @cancel=${(e: Event) => { e.preventDefault(); close(); }} @keydown=${(e: KeyboardEvent) => e.stopPropagation()}>
      <h2 id="wbmt">${t("wb.mermaid_title")}</h2>
      <p class="muted">${t("wb.mermaid_text")}</p>
      <textarea aria-label=${t("wb.mermaid_title")} .value=${this.mermaid} spellcheck="false"></textarea>
      <div class="acts"><button class="ghost" @click=${close}>${t("doc.cancel")}</button><button ?disabled=${this.busy} @click=${() => void this.importMermaid()}>${icon("Workflow", 14)} ${t("wb.mermaid_button")}</button></div>
    </dialog>`;
  }

  private menuView() {
    const m = this.menu;
    if (!m) return nothing;
    const run = (fn: () => void, keep = false) => () => {
      if (!keep) {
        this.menu = null;
        this.svgEl.focus();
      }
      fn();
    };
    const item = (ico: string, label: string, fn: () => void, o: { key?: string; danger?: boolean; dis?: boolean; checked?: boolean } = {}) => html`<button class="mi ${o.danger ? "danger" : ""}"
      role=${o.checked === undefined ? "menuitem" : "menuitemcheckbox"} aria-checked=${o.checked === undefined ? nothing : o.checked ? "true" : "false"} ?disabled=${o.dis}
      @click=${run(fn)}>${icon(ico, 15)}<span>${label}</span>${o.key ? html`<kbd>${o.key}</kbd>` : nothing}${o.checked ? html`<span class="tick">${icon("Check", 14)}</span>` : nothing}</button>`;
    const sep = html`<hr role="separator">`;
    const sel = this.selection(), ro = this.readOnly, at: Pt = [m.wx, m.wy];
    const one = sel.length === 1 ? sel[0] : undefined, free = sel.filter((e) => !e.locked);
    let body;
    if (!sel.length) {
      body = html`
        ${ro ? nothing : html`${item("ClipboardPaste", t("wb.menu.paste_here"), () => void this.pasteAt(at), { key: kbd("V") })}
        ${item("StickyNote", t("wb.menu.add_sticky"), () => this.addAt("sticky", m.wx, m.wy))}
        ${item("Type", t("wb.menu.add_text"), () => this.addAt("text", m.wx, m.wy))}`}
        ${item("MessageSquarePlus", t("wb.menu.add_comment"), () => (this.pop = { kind: "newcomment", x: m.wx, y: m.wy }))}
        ${sep}
        ${item("SquareDashedMousePointer", t("wb.menu.select_all"), () => this.selectMany(this.els().map((e) => e.id)), { key: kbd("A"), dis: !this.els().length })}
        ${item("Maximize", t("wb.fit"), () => this.fit())}
        ${ro ? nothing : html`${item("Grid3x3", t("wb.snap"), () => this.setSnap(!this.snap), { checked: this.snap })}
        ${item("CircleDashed", t("wb.menu.grid"), () => { this.board.background = this.board.background === "plain" ? "" : "plain"; this.changed(); this.requestUpdate(); }, { checked: this.board.background !== "plain" })}`}`;
    } else {
      const textual = one && (TEXTUAL.has(one.type) || one.type === "frame");
      const grouped = sel.some((e) => e.groups?.length);
      const allLocked = sel.every((e) => e.locked);
      body = html`
        ${ro ? nothing : item("Scissors", t("wb.menu.cut"), () => this.copySelection(true), { key: kbd("X"), dis: !free.length })}
        ${item("Copy", t("wb.menu.copy"), () => this.copySelection(false), { key: kbd("C") })}
        ${ro ? nothing : html`${item("ClipboardPaste", t("wb.menu.paste"), () => void this.pasteAt(at), { key: kbd("V") })}
        ${item("CopyPlus", t("wb.menu.duplicate"), () => this.duplicate(), { key: kbd("D") })}`}
        ${!ro && textual && !one!.locked ? item("Pencil", t("wb.menu.edit_text"), () => this.editText(one!), { key: "↵" }) : nothing}
        ${one?.entity ? item("ExternalLink", t("wb.open_entry"), () => navigate(`/p/${this.project}/e/${one.entity}`)) : nothing}
        ${!ro && one?.entity ? item("Network", t("wb.menu.neighbourhood"), () => void this.drawNeighbourhood(one.entity!)) : nothing}
        ${!ro && sel.some((e) => e.type === "sticky" && e.text?.trim()) ? item("SquareKanban", t("wb.menu.to_cards"), () => void this.openCards()) : nothing}
        ${ro ? nothing : html`${this.formatView(free)}
        ${sep}
        ${item("BringToFront", t("wb.front"), () => this.orderSel(true))}
        ${item("SendToBack", t("wb.back"), () => this.orderSel(false))}
        ${sel.length > 1 ? html`<div class="icons" role="group" aria-label=${t("wb.align")}>${([["left", "AlignStartVertical", "wb.align_left"], ["hcenter", "AlignCenterVertical", "wb.align_hcenter"], ["right", "AlignEndVertical", "wb.align_right"],
          ["top", "AlignStartHorizontal", "wb.align_top"], ["vcenter", "AlignCenterHorizontal", "wb.align_vcenter"], ["bottom", "AlignEndHorizontal", "wb.align_bottom"]] as const).map(([how, ic, label]) =>
          html`<button title=${t(label)} aria-label=${t(label)} @click=${run(() => this.alignSel(how), true)}>${icon(ic, 15)}</button>`)}</div>
          ${item("Group", t("wb.menu.group"), () => this.groupSel(true), { key: kbd("G") })}` : nothing}
        ${grouped ? item("Ungroup", t("wb.menu.ungroup"), () => this.groupSel(false), { key: kbd("G", true) }) : nothing}
        ${item(allLocked ? "LockOpen" : "Lock", t(allLocked ? "wb.unlock" : "wb.lock"), () => { for (const e of sel) e.locked = allLocked ? undefined : true; this.changed(); })}
        ${item("MessageSquarePlus", t("wb.menu.add_comment"), () => (this.pop = { kind: "newcomment", x: m.wx, y: m.wy, element: one && one.type !== "frame" ? one.id : undefined }))}
        ${sep}
        ${one && this.selPt?.id === one.id ? item("CircleMinus", t("wb.menu.delete_point"), () => this.removePoint(), { key: t("wb.key_del"), danger: true, dis: one.locked || (one.points?.length ?? 0) <= 2 }) : nothing}
        ${item("Trash2", one && this.selPt?.id === one.id ? t(one.type === "arrow" ? "wb.menu.delete_arrow" : "wb.menu.delete_line") : t("wb.menu.delete"), () => this.removeMany(free.map((e) => e.id)), { key: one && this.selPt?.id === one.id ? undefined : t("wb.key_del"), danger: true, dis: !free.length })}
        ${free.length < sel.length ? html`<div class="note">${t("wb.menu.locked")}</div>` : nothing}`}`;
    }
    return html`<div class="menu" role="menu" aria-label=${t("wb.menu")} style="left:${m.x}px;top:${m.y}px" @keydown=${(e: KeyboardEvent) => this.menuKey(e)} @contextmenu=${(e: Event) => e.preventDefault()}>${body}</div>`;
  }

  /** Formatting for the selection: colour, fill, line style, text size. */
  private formatView(free: WBElement[]) {
    if (!free.length) return nothing;
    const keep = (fn: () => void) => () => fn();
    const strokeable = free.filter((e) => !["frame", "image", "embed", "sticky"].includes(e.type));
    const stickies = free.filter((e) => e.type === "sticky");
    const shapes = free.filter((e) => ["rect", "ellipse", "diamond"].includes(e.type));
    const lined = strokeable.filter((e) => e.type !== "text" && e.type !== "entity");
    const texts = free.filter((e) => TEXTUAL.has(e.type) && !LINEAR.has(e.type));
    if (!strokeable.length && !stickies.length && !texts.length) return nothing;
    // One colour row: the stroke of drawn things, the paper of sticky notes.
    const cur = strokeable[0]?.stroke ?? (strokeable.length ? "gray" : stickies[0]?.fill ?? "yellow");
    const sw = (c: Color, bg: string, pressed: boolean, fn: () => void) => html`<button class="sw" style="background:${bg}" aria-label=${t(`color.${c}` as any)} aria-pressed=${pressed ? "true" : "false"} @click=${keep(fn)}></button>`;
    const sizeOf = (e: WBElement) => e.fontSize || (e.type === "text" ? 24 : e.type === "sticky" ? 18 : 20);
    return html`<hr role="separator"><div class="fmt">
      ${strokeable.length || stickies.length ? html`<span class="lbl">${t("wb.menu.color")}</span><div class="swatches" role="group" aria-label=${t("wb.menu.color")} style="--cols:${swatchCols(PALETTE.length + 1, 9)}">${PALETTE.map((c) => sw(c, strokeable.length ? tc(c) : tb(c), cur === c, () => this.applySel((e) => {
        if (e.type === "sticky") e.fill = c;
        else if (!["frame", "image", "embed"].includes(e.type)) e.stroke = c;
      })))}${this.customSwatch(cur, strokeable.length ? "tc" : "tb", (hex) => this.applySel((e) => {
        if (e.type === "sticky") e.fill = hex;
        else if (!["frame", "image", "embed"].includes(e.type)) e.stroke = hex;
      }))}</div>` : nothing}
      ${shapes.length ? html`<span class="lbl">${t("wb.fill")}</span><div class="swatches" role="group" aria-label=${t("wb.fill")} style="--cols:${swatchCols(PALETTE.length + 2, 9)}">${PALETTE.map((c) => sw(c, tb(c), shapes[0].fill === c, () => this.applySel((e) => { if (shapes.includes(e)) e.fill = c; })))}
        <button class="sw none" title=${t("wb.no_fill")} aria-label=${t("wb.no_fill")} aria-pressed=${!shapes[0].fill ? "true" : "false"} @click=${() => this.applySel((e) => { if (shapes.includes(e)) delete e.fill; })}>${icon("X", 11)}</button>${this.customSwatch(shapes[0].fill ?? "gray", "tb", (hex) => this.applySel((e) => { if (shapes.includes(e)) e.fill = hex; }))}</div>` : nothing}
      ${lined.length ? html`<div class="seg2" role="group" aria-label=${t("wb.width")}>${(["", "dashed", "dotted"] as const).map((d) => html`<button aria-pressed=${(lined[0].dash ?? "") === d ? "true" : "false"}
        @click=${() => this.applySel((e) => { if (lined.includes(e)) { if (d) e.dash = d; else delete e.dash; } })}>${t(`wb.dash_${d || "solid"}` as any)}</button>`)}</div>` : nothing}
      ${texts.length ? html`<div class="seg2" role="group" aria-label=${t("wb.font_size")}>${[16, 20, 28, 40].map((z, i) => html`<button aria-pressed=${sizeOf(texts[0]) === z ? "true" : "false"}
        @click=${() => this.applySel((e) => { if (texts.includes(e)) e.fontSize = z; })}>${["S", "M", "L", "XL"][i]}</button>`)}</div>` : nothing}
    </div>`;
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
    if (this.still) return html`<div class="stage"><svg class="board" role="img" aria-label=${this.doc?.title ?? t("wb.board")}>
      <g class="world"><g class="layer"></g><g class="pins"></g><g class="others"></g></g><g class="laser"></g></svg></div>`;
    const toolCls = this.tool === "hand" ? "t-hand" : this.tool === "laser" ? "t-laser" : this.tool === "comment" ? "t-comment" : this.tool === "select" ? "" : "t-draw";
    const open = this.openComments().length;
    // Prototype controls only while screens are on the board (a prototype may outlive its last screen).
    const hasScreens = this.els().some((e) => e.screen);
    return html`<div class="stage" @dragover=${(e: DragEvent) => !this.readOnly && e.preventDefault()} @drop=${this.onDrop} @mousedown=${this.noAutoscroll} @pointerdown=${this.onStageDown}>
      <svg class="board ${toolCls}" tabindex="0" role="application" aria-label=${this.doc?.title ?? t("wb.board")}
        @pointerdown=${this.onDown} @pointermove=${this.onMove} @pointerup=${this.onUp} @pointercancel=${this.onUp} @contextmenu=${this.onContextMenu}
        @pointerleave=${() => { this.live?.setCursor(undefined); if (this.laserDown) { this.laserDown = false; this.laserEnd("me"); } }} @wheel=${this.onWheel}>
        <defs><pattern id="wbgrid" width="24" height="24" patternUnits="userSpaceOnUse"><circle class="grid" cx="1" cy="1" r="1"></circle></pattern></defs>
        ${this.board.background === "plain" ? nothing : html`<rect width="100%" height="100%" fill="url(#wbgrid)" style="pointer-events:none"></rect>`}
        <g class="world"><g class="layer"></g><g class="pins"></g><g class="others"></g></g>
        <g class="laser" style="pointer-events:none"></g>
      </svg>
      <div class="apps"></div>
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
      ${this.tool === "screen" ? html`<div class="bar opts">
        <span class="seg" role="group" aria-label=${t("proto.device")}>${(["phone", "tablet", "desktop"] as const).map((d) => html`<button aria-pressed=${this.screenDevice === d ? "true" : "false"} @click=${() => (this.screenDevice = d)}>${t(`proto.device_${d}` as any)}</button>`)}</span>
        <select aria-label=${t("proto.prototype")} @change=${(e: Event) => (this.screenProto = (e.target as HTMLSelectElement).value)} @keydown=${(e: Event) => e.stopPropagation()}>
          ${(this.board.prototypes ?? []).map((p, i) => html`<option value=${p.id} ?selected=${this.screenProto === p.id || (i === 0 && !this.screenProto)}>${p.name}</option>`)}
          <option value="+new" ?selected=${this.screenProto === "+new" || !(this.board.prototypes ?? []).length}>${t("proto.new_prototype")}</option></select>
        <span>${t("proto.hint_screen")}</span></div>` : nothing}
      ${this.tool === "ui" ? html`<div class="bar opts comps" role="group" aria-label=${t("proto.component")}>
        ${COMPS.map((c) => html`<button aria-pressed=${this.uiComp === c.comp ? "true" : "false"} title=${t(`proto.c.${c.comp}` as any)} aria-label=${t(`proto.c.${c.comp}` as any)} @click=${() => (this.uiComp = c.comp)}>${icon(c.icon, 16)}</button>`)}
        <span>${t("proto.hint_ui")}</span></div>` : nothing}
      ${this.tool === "laser" ? html`<div class="bar opts">
        <span class="seg"><button aria-pressed=${this.trail ? "true" : "false"} @click=${() => { this.trail = true; try { localStorage.setItem("wb.trail", "trail"); } catch { /* ignore */ } }}>${t("wb.trail")}</button><button aria-pressed=${!this.trail ? "true" : "false"} @click=${() => { this.trail = false; try { localStorage.setItem("wb.trail", "dot"); } catch { /* ignore */ } }}>${t("wb.dot")}</button></span>
        <span class="seg" role="group" aria-label=${t("wb.thickness")}>${[3, 4.5, 7].map((s, i) => html`<button aria-pressed=${this.laserSize === s ? "true" : "false"} aria-label=${[t("wb.thin"), t("wb.medium"), t("wb.bold")][i]} title=${[t("wb.thin"), t("wb.medium"), t("wb.bold")][i]}
          @click=${() => { this.laserSize = s; try { localStorage.setItem("wb.laserSize", String(s)); } catch { /* ignore */ } }}><span class="dot" style="width:${s * 2}px;height:${s * 2}px"></span></button>`)}</span>
        <span>${t("wb.hint_laser")}</span></div>` : nothing}
      ${this.standalone && this.doc ? html`<div class="bar home"><a class="ico" href=${`/p/${this.project}/d/${this.doc.id}`} aria-label=${t("wb.back_to_page")} title=${t("wb.back_to_page")}>${icon("ArrowLeft", 16)}</a><span title=${this.doc.title}>${this.doc.title}</span></div>` : nothing}
      <div class="bar state" role="status">
        <span class="live ${this.readOnly || this.status !== "connected" ? "off" : this.saved?.state === "saving" ? "saving" : ""}"></span>${this.stateText()}
        ${this.peers.length ? html`<span class="avatars" title=${this.peers.map((p) => p.name).join(", ")}>${this.peers.map((p) => html`<span style="background:${p.color}">${p.name.slice(0, 2).toUpperCase()}</span>`)}</span>` : nothing}
      </div>
      <div class="bar zoom">
        <button aria-label=${t("wb.zoom_out")} title=${t("wb.zoom_out")} @click=${() => this.zoomBy(1 / 1.2)}>${icon("ZoomOut", 16)}</button><span>${this.zoom} %</span>
        <button aria-label=${t("wb.zoom_in")} title=${t("wb.zoom_in")} @click=${() => this.zoomBy(1.2)}>${icon("ZoomIn", 16)}</button>
        <button aria-label=${t("wb.fit")} title=${t("wb.fit")} @click=${() => this.fit()}>${icon("Maximize", 16)}</button>
        ${this.standalone || !this.doc ? nothing : html`<button aria-label=${t(this.maximized ? "wb.restore" : "wb.maximize")} title=${t(this.maximized ? "wb.restore" : "wb.maximize")} aria-pressed=${this.maximized ? "true" : "false"}
          @click=${() => this.setMaximized(!this.maximized)}>${icon(this.maximized ? "Minimize2" : "Maximize2", 16)}</button>
          <a class="ico" href=${`/wb/${this.project}/${this.doc.id}`} target="_blank" rel="noopener" aria-label=${t("wb.own_page")} title=${t("wb.own_page")}>${icon("ExternalLink", 16)}</a>`}
        ${hasScreens ? html`<button class="flows" aria-label=${t("wb.flows_title", { mode: t(`wb.flows_${this.flows}`) })} title=${t("wb.flows_title", { mode: t(`wb.flows_${this.flows}`) })}
          aria-pressed=${this.flows !== "off" ? "true" : "false"} @click=${() => this.cycleFlows()}>${icon("Workflow", 16)}<span>${t(`wb.flows_${this.flows}`)}</span></button>` : nothing}
        ${this.readOnly ? nothing : html`<button aria-label=${t("wb.snap")} title=${t("wb.snap")} aria-pressed=${this.snap ? "true" : "false"} @click=${() => this.setSnap(!this.snap)}>${icon("Grid3x3", 16)}</button>`}
        ${this.readOnly ? nothing : html`<span class="sep"></span>
        <button aria-label=${t("editor.undo")} title=${t("editor.undo")} @click=${() => this.live?.undo()}>${icon("Undo2", 16)}</button>
        <button aria-label=${t("editor.redo")} title=${t("editor.redo")} @click=${() => this.live?.redo()}>${icon("Redo2", 16)}</button>`}
      </div>
      <div class="bar side">
        <button aria-label=${t("wb.export_svg")} title=${t("wb.export_svg")} @click=${() => this.exportAs("svg")}>${icon("Download", 16)}</button>
        <button aria-label=${t("wb.export_png")} title=${t("wb.export_png")} @click=${() => this.exportAs("png")}>${icon("Image", 16)}</button>
        <button aria-label=${t("wb.export_excalidraw")} title=${t("wb.export_excalidraw")} @click=${() => this.exportExcalidraw()}>${icon("FileCode", 16)}</button>
        ${this.readOnly ? nothing : html`<button aria-label=${t("wb.import_excalidraw")} title=${t("wb.import_excalidraw")} @click=${() => this.pickExcalidraw()}>${icon("Upload", 16)}</button>
          <button aria-label=${t("wb.mermaid_title")} title=${t("wb.mermaid_title")} @click=${() => void this.openMermaid()}>${icon("Workflow", 16)}</button>
          <button aria-label=${t("wb.block_title")} title=${t("wb.block_title")} @click=${() => void this.openChart()}>${icon("Shapes", 16)}</button>`}
        <button aria-label=${t("wb.present")} title=${t("wb.present")} @click=${() => this.presentAt(0)}>${icon("Presentation", 16)}</button>
        ${hasScreens ? html`<button aria-label=${t("proto.prototypes")} title=${t("proto.prototypes")} @click=${() => {
          const list = this.board.prototypes ?? [];
          if (list.length === 1 && this.readOnly) this.playing = { proto: list[0].id };
          else void this.openProto(list[0].id);
        }}>${icon("Play", 16)}</button>` : nothing}
        <button aria-label=${t("wb.comments")} title=${t("wb.comments")} aria-pressed=${this.panel ? "true" : "false"} @click=${() => (this.panel = !this.panel)}>${icon("MessageSquare", 16)}${open ? html`<span class="count">${open}</span>` : nothing}</button>
      </div>
      ${this.panel ? this.commentPanel() : this.styleBar()}
      ${this.popoverView()}
      ${this.menuView()}
      ${this.cardsDialog()}
      ${this.mermaidDialog()}
      ${this.protoView()}
      ${this.present >= 0 ? html`<div class="bar present" role="toolbar" aria-label=${t("wb.present")}>
        <button aria-label=${t("wb.prev")} @click=${() => this.presentAt(Math.max(0, this.present - 1))}>${icon("ChevronLeft", 16)}</button>
        <span>${this.present + 1} / ${slides(this.els()).length} · ${slides(this.els())[this.present]?.text ?? ""}</span>
        <button aria-label=${t("wb.next")} @click=${() => this.presentAt(this.present + 1)}>${icon("ChevronRight", 16)}</button>
        <button aria-label=${t("wb.present_end")} @click=${() => this.stopPresenting()}>${icon("X", 16)}</button></div>` : nothing}
    </div>`;
  }

  updated() {
    this.toggleAttribute("presenting", this.present >= 0);
    this.placeMenu();
    if (!this.painted && this.doc) this.paint();
    if (this.pop) (this.renderRoot.querySelector(".pop [autofocus]") as HTMLElement | null)?.focus();
  }
}
customElements.define("phx-whiteboard", PhxWhiteboard);
