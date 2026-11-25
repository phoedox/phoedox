// Prototypes on whiteboards in the editor: components drawn as they look
// while playing (the shared player), arrows for the flow between screens,
// the inspector for components, screens and interactions, the prototype
// settings and the play mode.

import { LitElement, css, html, nothing, type TemplateResult } from "lit";
import "../../player/proto-player.js";
import { ApiError, api } from "../api";
import { t } from "../i18n";
import { icon } from "../icon";
import { navigate } from "../state";
import {
  boxOf, flowRoute, type Act, type Comp, type Device, type Trigger, type WBAction, type WBComment, type WBData, type WBElement, type WBPrototype, type WBVar, type Whiteboard,
} from "../whiteboard";
import { toast } from "./toasts";
import { htmlScreenImage } from "../wb-html";

interface PlayerController { update(model: PlayerModel): void; restart(): void; escape(): boolean; destroy(): void }
interface PlayerModel { board: Whiteboard; proto: string; data: Record<string, unknown[]>; lang?: string; strings?: Record<string, string> }
interface PlayerHooks {
  onClose?: () => void; onComment?: (c: { screen: string; x: number; y: number; text: string }) => Promise<WBComment>;
  fileURL?: (id: string) => string; start?: string;
}
declare global {
  // eslint-disable-next-line no-var
  var PhxProto: {
    mount(root: HTMLElement, model: PlayerModel, hooks?: PlayerHooks): PlayerController;
    preview(host: HTMLElement, e: WBElement, opts: { look?: string; vars?: WBVar[]; data?: Record<string, unknown[]>; fileURL?: (id: string) => string }): void;
    icons: string[];
  };
}

/** Components with their toolbar icon and size when placed. */
export const COMPS: { comp: Comp; icon: string; w: number; h: number }[] = [
  { comp: "button", icon: "RectangleHorizontal", w: 160, h: 48 }, { comp: "link", icon: "Link", w: 140, h: 28 },
  { comp: "input", icon: "TextCursorInput", w: 300, h: 48 }, { comp: "textarea", icon: "LetterText", w: 300, h: 110 },
  { comp: "checkbox", icon: "SquareCheck", w: 240, h: 32 }, { comp: "switch", icon: "ToggleRight", w: 300, h: 36 },
  { comp: "radio", icon: "CircleDot", w: 240, h: 96 }, { comp: "select", icon: "ChevronsUpDown", w: 300, h: 48 },
  { comp: "slider", icon: "SlidersHorizontal", w: 300, h: 52 }, { comp: "tabs", icon: "PanelTop", w: 340, h: 40 },
  { comp: "navbar", icon: "PanelBottom", w: 390, h: 64 }, { comp: "list", icon: "List", w: 340, h: 224 },
  { comp: "card", icon: "SquareStack", w: 340, h: 120 }, { comp: "table", icon: "Table", w: 340, h: 200 },
  { comp: "toast", icon: "MessageSquareText", w: 300, h: 48 }, { comp: "icon", icon: "Smile", w: 32, h: 32 },
  { comp: "chart", icon: "ChartColumn", w: 340, h: 180 }, { comp: "avatar", icon: "CircleUser", w: 56, h: 56 },
  { comp: "badge", icon: "Tag", w: 96, h: 28 }, { comp: "progress", icon: "Loader", w: 300, h: 32 },
];

export const DEVICES: Record<Exclude<Device, "custom">, [number, number]> = { phone: [390, 844], tablet: [820, 1180], desktop: [1440, 900] };
const TRIGGERS: Trigger[] = ["click", "change", "hover", "open", "delay"];
const ACTS: Act[] = ["goto", "back", "overlay", "close", "set", "toggle", "inc", "add", "link", "toast"];
const ITEM_COMPS = new Set<Comp>(["tabs", "navbar", "radio", "select", "list", "table"]);

/** The screen an element is on: its frame, else the screen around its centre. */
export function screenOf(board: Whiteboard, e: WBElement): WBElement | undefined {
  if (e.screen) return e;
  const byId = new Map(board.elements.map((x) => [x.id, x]));
  for (let f = e.frame, n = 0; f && n < 20; f = byId.get(f)?.frame, n++) {
    const s = byId.get(f);
    if (s?.screen) return s;
  }
  const b = boxOf(e.points?.length ? { ...e, ...ptsBox(e) } : e), cx = b.x + b.w / 2, cy = b.y + b.h / 2;
  return board.elements.find((s) => s.screen && cx >= (s.x ?? 0) && cx <= (s.x ?? 0) + (s.w ?? 0) && cy >= (s.y ?? 0) && cy <= (s.y ?? 0) + (s.h ?? 0));
}
function ptsBox(e: WBElement) {
  const xs = e.points!.map((p) => p[0]), ys = e.points!.map((p) => p[1]);
  return { x: Math.min(...xs), y: Math.min(...ys), w: Math.max(...xs) - Math.min(...xs), h: Math.max(...ys) - Math.min(...ys) };
}
export const protoById = (board: Whiteboard, id?: string) => (board.prototypes ?? []).find((p) => p.id === id);
export const screensOf = (board: Whiteboard, proto: string) =>
  board.elements.filter((e) => e.screen?.proto === proto).sort((a, b) => (a.y ?? 0) - (b.y ?? 0) || (a.x ?? 0) - (b.x ?? 0));
export const sampleData = (p?: WBPrototype) => Object.fromEntries((p?.data ?? []).map((d) => [d.name, d.rows ?? []]));

export function newProtoId(board: Whiteboard) {
  let n = (board.prototypes ?? []).length + 1, id = `proto-${n}`;
  while ((board.prototypes ?? []).some((p) => p.id === id)) id = `proto-${++n}`;
  return id;
}

export function playerStrings(): Record<string, string> {
  return {
    restart: t("proto.restart"), screens: t("proto.screens"), comment: t("proto.comment"), close: t("proto.close"), send: t("proto.send"),
    cancel: t("doc.cancel"), place: t("proto.place"), saved: t("proto.saved"), failed: t("proto.failed"), empty: t("proto.empty"),
  };
}

// ---- drawing in the editor ------------------------------------------------------

const NS = "http://www.w3.org/2000/svg";

/** Components drawn by the player, kept while they do not change. */
export class PreviewCache {
  private nodes = new Map<string, { key: string; fo: SVGForeignObjectElement }>();

  draw(e: WBElement, board: Whiteboard): SVGForeignObjectElement {
    const s = screenOf(board, e), p = protoById(board, s?.screen?.proto);
    const { x, y, order, seed, ...rest } = e;
    void order; void seed;
    const key = JSON.stringify([rest, p?.look ?? "", p?.vars ?? [], p?.data ?? []]);
    let hit = this.nodes.get(e.id);
    if (!hit || hit.key !== key) {
      const fo = document.createElementNS(NS, "foreignObject");
      fo.style.pointerEvents = "none";
      fo.setAttribute("data-label", e.text || t(`proto.c.${e.ui?.comp ?? "button"}` as any));
      const host = document.createElement("div");
      host.style.cssText = "position:relative;width:100%;height:100%";
      fo.append(host);
      try {
        globalThis.PhxProto.preview(host, { ...e, x: 0, y: 0 }, { look: p?.look, vars: p?.vars, data: sampleData(p), fileURL: (id) => `/api/v1/files/${encodeURIComponent(id)}` });
      } catch { /* a broken component must not break the board */ }
      hit = { key, fo };
      this.nodes.set(e.id, hit);
    }
    for (const [k, v] of Object.entries({ x: x ?? 0, y: y ?? 0, width: Math.max(1, e.w ?? 1), height: Math.max(1, e.h ?? 1) })) hit.fo.setAttribute(k, String(v));
    return hit.fo;
  }

  prune(ids: Set<string>) {
    for (const id of this.nodes.keys()) if (!ids.has(id)) this.nodes.delete(id);
  }
}

/** A screen: an opaque device-shaped frame with its name above. */
export function drawScreen(e: WBElement, board: Whiteboard, g: SVGGElement) {
  const x = e.x ?? 0, y = e.y ?? 0, w = e.w ?? 0, h = e.h ?? 0;
  const dev = e.screen?.device ?? "phone";
  const rx = dev === "phone" ? 40 : dev === "tablet" ? 26 : dev === "desktop" ? 10 : 14;
  const r = document.createElementNS(NS, "rect");
  for (const [k, v] of Object.entries({ x, y, width: w, height: h, rx })) r.setAttribute(k, String(v));
  r.style.cssText = `fill:${e.fill ? `var(--tb-${e.fill})` : "var(--surface)"};stroke:var(--line);stroke-width:1.5;filter:drop-shadow(0 6px 16px rgb(0 0 0 / .12))`;
  if (e.screen?.overlay) r.style.strokeDasharray = "8 6";
  g.append(r);
  if (e.screen?.html && w > 0 && h > 0) {
    // An HTML screen: its design as a still picture (it runs only when played).
    const img = document.createElementNS(NS, "image");
    for (const [k, v] of Object.entries({ x, y, width: w, height: h, preserveAspectRatio: "xMinYMin slice" })) img.setAttribute(k, String(v));
    const p0 = protoById(board, e.screen.proto);
    const start = Object.fromEntries((p0?.vars ?? []).map((v) => [v.name, v.value]));
    img.setAttribute("href", htmlScreenImage(e.screen.html, w, h, start));
    img.style.clipPath = `inset(0 round ${rx}px)`;
    img.style.pointerEvents = "none";
    g.append(img);
  }
  const p = protoById(board, e.screen?.proto);
  const start = p && (p.start === e.id || (!p.start && screensOf(board, p.id).filter((s) => !s.screen?.overlay)[0]?.id === e.id));
  const label = document.createElementNS(NS, "text");
  label.setAttribute("x", String(x + 4));
  label.setAttribute("y", String(y - 10));
  label.style.cssText = "font:600 14px var(--font-body);fill:var(--muted)";
  label.textContent = `${start ? "▶ " : ""}${e.text || t("proto.screen")}${p ? ` · ${p.name}` : ""}${e.screen?.overlay ? ` · ${t(`proto.overlay_${e.screen.overlay}` as any)}` : ""}`;
  g.append(label);
  // "▶ from here": play the prototype starting on this screen.
  if (p && !e.screen?.overlay) {
    const text = t("proto.play_from_here"), cw = 30 + text.length * 7.2, cx = x + w - cw, cy = y - 34;
    const chip = document.createElementNS(NS, "g");
    chip.setAttribute("data-play", e.id);
    chip.setAttribute("data-proto", p.id);
    chip.style.cursor = "pointer";
    const bg = document.createElementNS(NS, "rect");
    for (const [k, v] of Object.entries({ x: cx, y: cy, width: cw, height: 26, rx: 13 })) bg.setAttribute(k, String(v));
    bg.style.cssText = "fill:var(--surface);stroke:var(--line);stroke-width:1.5";
    const tri = document.createElementNS(NS, "path");
    tri.setAttribute("d", `M${cx + 12} ${cy + 7} l10 6 l-10 6 z`);
    tri.style.cssText = "fill:var(--accent)";
    const tx = document.createElementNS(NS, "text");
    tx.setAttribute("x", String(cx + 27));
    tx.setAttribute("y", String(cy + 18));
    tx.style.cssText = "font:600 12px var(--font-body);fill:var(--fg)";
    tx.textContent = text;
    const tip = document.createElementNS(NS, "title");
    tip.textContent = t("proto.play_here");
    chip.append(tip, bg, tri, tx);
    g.append(chip);
  }
}

/** Which flow arrows the board shows (Jan, 2026-11-22): none, those of the selected screens, or all. */
export type FlowMode = "off" | "sel" | "all";

/**
 * Arrows from elements to the screens their interactions open. They run at
 * right angles through the gaps between screens (or in a lane above or
 * below the row when a screen is in the way) and end at the side of the
 * target, so no arrow crosses a screen's content. In "sel" mode only the
 * selected screens' links are drawn: outgoing ones strong, incoming faint.
 */
export function drawFlow(board: Whiteboard, z: number, mode: FlowMode = "all", sel: Set<string> = new Set()): SVGGElement {
  const g = document.createElementNS(NS, "g");
  g.style.pointerEvents = "none";
  if (mode === "off") return g;
  const byId = new Map(board.elements.map((e) => [e.id, e]));
  const frames = board.elements.filter((e) => e.screen);
  const entries = new Map<string, number>(); // arrows ending at a side of a screen, to fan them out
  const lanes = { above: 0, below: 0 };
  const next = (key: string) => { const n = entries.get(key) ?? 0; entries.set(key, n + 1); return n; };
  for (const e of board.elements) {
    for (const a of e.actions ?? []) {
      if (a.do !== "goto" && a.do !== "overlay") continue;
      const to = byId.get(a.target ?? "");
      if (!to || to.id === e.id) continue;
      const from = e.screen ? e : screenOf(board, e);
      if (from?.id === to.id) continue;
      let faint = false;
      if (mode === "sel") {
        const out = !!from && sel.has(from.id), inc = sel.has(to.id);
        if (!out && !inc) continue;
        faint = !out;
      }
      const sb = e.points?.length ? ptsBox(e) : boxOf(e), fb = from ? boxOf(from) : sb, tb = boxOf(to);
      const route = flowRoute(sb, fb, tb, frames.filter((f) => f.id !== from?.id && f.id !== to.id).map((f) => boxOf(f)), next, lanes, to.id);
      const path = document.createElementNS(NS, "path");
      path.setAttribute("d", "M" + route.pts.map((p) => `${p[0]} ${p[1]}`).join(" L"));
      const op = faint ? 0.3 : 0.85;
      path.style.cssText = `fill:none;stroke:var(--accent);stroke-width:${2 / z};stroke-dasharray:${6 / z} ${5 / z};stroke-linejoin:round;opacity:${op}`;
      const [ex, ey] = route.pts[route.pts.length - 1], [px, py] = route.pts[route.pts.length - 2];
      const ang = Math.atan2(ey - py, ex - px), L = 10 / z;
      const head = document.createElementNS(NS, "path");
      head.setAttribute("d", `M${ex - L * Math.cos(ang - 0.5)} ${ey - L * Math.sin(ang - 0.5)} L${ex} ${ey} L${ex - L * Math.cos(ang + 0.5)} ${ey - L * Math.sin(ang + 0.5)}`);
      head.style.cssText = `fill:none;stroke:var(--accent);stroke-width:${2 / z};opacity:${op}`;
      const dot = document.createElementNS(NS, "circle");
      dot.setAttribute("cx", String(route.pts[0][0]));
      dot.setAttribute("cy", String(route.pts[0][1]));
      dot.setAttribute("r", String(4 / z));
      dot.style.cssText = `fill:var(--accent);opacity:${op}`;
      g.append(path, head, dot);
    }
  }
  return g;
}

// ---- inspector --------------------------------------------------------------------

export interface InspectorCtx {
  e: WBElement;
  board: Whiteboard;
  set: (patch: Partial<WBElement>) => void;
  setProto: (id: string, patch: Partial<WBPrototype> | null) => void;
  play: (proto: string, screen?: string) => void;
  settings: (proto: string) => void;
}

const stop = (ev: Event) => ev.stopPropagation();
const val = (ev: Event) => (ev.target as HTMLInputElement).value;

export function actionsView(c: InspectorCtx, onScreen: boolean): TemplateResult {
  const { e, board } = c;
  const scr = screenOf(board, e), p = protoById(board, scr?.screen?.proto);
  if (!p) return html``;
  const screens = screensOf(board, p.id);
  const acts = e.actions ?? [];
  const put = (list: WBAction[]) => c.set({ actions: list.length ? list : undefined });
  const upd = (i: number, patch: Partial<WBAction>) => put(acts.map((a, k) => {
    if (k !== i) return a;
    const n = { ...a, ...patch } as WBAction;
    for (const [key, v] of Object.entries(n)) if (v === "" || v === undefined) delete (n as unknown as Record<string, unknown>)[key];
    return n;
  }));
  const triggers = onScreen ? (["open", "delay"] as Trigger[]) : TRIGGERS.filter((x) => x !== "open" && x !== "delay");
  const withItems = e.ui && ITEM_COMPS.has(e.ui.comp);
  return html`<h4>${t("proto.interactions")}</h4>
    ${acts.map((a, i) => html`<div class="act">
      <div class="two"><select aria-label=${t("proto.when")} @change=${(ev: Event) => upd(i, { on: val(ev) as Trigger })}>${triggers.map((x) => html`<option value=${x} ?selected=${a.on === x}>${t(`proto.on_${x}` as any)}</option>`)}</select>
        <select aria-label=${t("proto.then")} @change=${(ev: Event) => upd(i, { do: val(ev) as Act, target: undefined, value: undefined })}>${ACTS.map((x) => html`<option value=${x} ?selected=${a.do === x}>${t(`proto.do_${x}` as any)}</option>`)}</select></div>
      ${a.do === "goto" || a.do === "overlay" ? html`<select aria-label=${t("proto.target_screen")} @change=${(ev: Event) => upd(i, { target: val(ev) })}>
        <option value="">${t("proto.pick_screen")}</option>
        ${screens.filter((s) => (a.do === "overlay") === !!s.screen?.overlay).map((s) => html`<option value=${s.id} ?selected=${a.target === s.id}>${s.text || s.id}</option>`)}</select>` : nothing}
      ${a.do === "goto" ? html`<select aria-label=${t("proto.anim")} @change=${(ev: Event) => upd(i, { anim: val(ev) as WBAction["anim"] })}>${(["", "slide", "fade"] as const).map((x) => html`<option value=${x} ?selected=${(a.anim ?? "") === x}>${t(`proto.anim_${x || "none"}` as any)}</option>`)}</select>` : nothing}
      ${["set", "toggle", "inc", "add"].includes(a.do) ? html`<select aria-label=${t("proto.variable")} @change=${(ev: Event) => upd(i, { target: val(ev) })}>
        <option value="">${t("proto.pick_variable")}</option>${(p.vars ?? []).map((v) => html`<option value=${v.name} ?selected=${a.target === v.name}>${v.name}</option>`)}</select>` : nothing}
      ${["set", "inc", "add", "link", "toast"].includes(a.do) ? html`<input aria-label=${t("proto.value")} placeholder=${t(`proto.value_${a.do}` as any)} .value=${a.value ?? ""} @change=${(ev: Event) => upd(i, { value: val(ev) })}>` : nothing}
      ${a.on === "delay" ? html`<input type="number" min="0" step="100" aria-label=${t("proto.delay")} placeholder=${t("proto.delay")} .value=${String(a.delay ?? 1000)} @change=${(ev: Event) => upd(i, { delay: Number(val(ev)) || 0 })}>` : nothing}
      ${withItems ? html`<input aria-label=${t("proto.item")} placeholder=${t("proto.item")} .value=${a.item ?? ""} @change=${(ev: Event) => upd(i, { item: val(ev) })}>` : nothing}
      <button class="ghost danger" title=${t("proto.remove")} aria-label=${t("proto.remove")} @click=${() => put(acts.filter((_, k) => k !== i))}>${icon("Trash2", 13)}</button>
    </div>`)}
    <div class="row"><button class="ghost" @click=${() => put([...acts, { on: triggers[0], do: onScreen ? "toast" : "goto" }])}>${icon("Plus", 13)} ${t("proto.add_interaction")}</button></div>`;
}

/** The inspector for screens and components (and the interactions of other elements on a screen). */
export function inspector(c: InspectorCtx): TemplateResult {
  const { e, board } = c;
  const scr = screenOf(board, e), p = protoById(board, scr?.screen?.proto);
  if (e.screen) {
    const s = e.screen;
    return html`<aside class="stylebar proto" aria-label=${t("proto.screen")} @keydown=${stop}>
      <h4>${t("proto.screen")}</h4>
      <label>${t("proto.name")}<input .value=${e.text ?? ""} @change=${(ev: Event) => c.set({ text: val(ev) })}></label>
      <label>${t("proto.prototype")}<select @change=${(ev: Event) => c.set({ screen: { ...s, proto: val(ev) } })}>${(board.prototypes ?? []).map((x) => html`<option value=${x.id} ?selected=${x.id === s.proto}>${x.name}</option>`)}</select></label>
      <label>${t("proto.device")}<select @change=${(ev: Event) => {
        const d = val(ev) as Device;
        const size = d === "custom" ? undefined : DEVICES[d];
        c.set({ screen: { ...s, device: d }, ...(size ? { w: size[0], h: size[1] } : {}) });
      }}>${(["phone", "tablet", "desktop", "custom"] as Device[]).map((d) => html`<option value=${d} ?selected=${(s.device ?? "phone") === d}>${t(`proto.device_${d}` as any)}</option>`)}</select></label>
      <label>${t("proto.overlay")}<select @change=${(ev: Event) => c.set({ screen: { ...s, overlay: (val(ev) || undefined) as "modal" | "sheet" | undefined } })}>${(["", "modal", "sheet"] as const).map((o) => html`<option value=${o} ?selected=${(s.overlay ?? "") === o}>${t(`proto.overlay_${o || "none"}` as any)}</option>`)}</select></label>
      ${p && !s.overlay ? html`<label class="check"><input type="checkbox" .checked=${p.start === e.id} @change=${(ev: Event) => c.setProto(p.id, { start: (ev.target as HTMLInputElement).checked ? e.id : undefined })}> ${t("proto.start_screen")}</label>` : nothing}
      ${actionsView(c, true)}
      <div class="row">
        ${p ? html`<button @click=${() => c.play(p.id, s.overlay ? undefined : e.id)}>${icon("Play", 13)} ${t("proto.play_here")}</button>
        <button class="ghost" @click=${() => c.settings(p.id)}>${icon("Settings2", 13)} ${t("proto.settings")}</button>` : nothing}
      </div>
    </aside>`;
  }
  if (!e.ui) return html``;
  const u = e.ui;
  const setUI = (patch: Partial<typeof u>) => {
    const n = { ...u, ...patch } as Record<string, unknown>;
    for (const [k, v] of Object.entries(n)) if (v === "" || v === undefined || (Array.isArray(v) && !v.length)) delete n[k];
    c.set({ ui: n as unknown as typeof u });
  };
  const has = (...comps: Comp[]) => comps.includes(u.comp);
  const lines = (ev: Event) => (ev.target as HTMLTextAreaElement).value.split("\n").map((x) => x.trim()).filter(Boolean);
  const csv = (ev: Event) => val(ev).split(",").map((x) => x.trim()).filter(Boolean);
  const vars = p?.vars ?? [], data = p?.data ?? [];
  return html`<aside class="stylebar proto" aria-label=${t("proto.component")} @keydown=${stop}>
    <h4>${t(`proto.c.${u.comp}` as any)}</h4>
    ${u.comp !== "icon" ? html`<label>${t("proto.label")}<input .value=${e.text ?? ""} @change=${(ev: Event) => c.set({ text: val(ev) || undefined })}></label>` : nothing}
    ${has("button") ? html`<div class="seg2">${["primary", "secondary", "ghost", "danger"].map((v) => html`<button aria-pressed=${(u.variant ?? "primary") === v ? "true" : "false"} @click=${() => setUI({ variant: v === "primary" ? undefined : v })}>${t(`proto.variant_${v}` as any)}</button>`)}</div>` : nothing}
    ${has("input", "textarea", "select") ? html`<label>${t("proto.placeholder")}<input .value=${u.placeholder ?? ""} @change=${(ev: Event) => setUI({ placeholder: val(ev) })}></label>` : nothing}
    ${has("input", "textarea", "card", "progress", "slider") ? html`<label>${t(u.comp === "card" ? "proto.body" : "proto.start_value")}<input .value=${u.value ?? ""} @change=${(ev: Event) => setUI({ value: val(ev) })}></label>` : nothing}
    ${has("radio", "select", "tabs", "navbar", "list") ? html`<label>${t("proto.options")}<textarea rows="3" .value=${(u.options ?? []).join("\n")} @change=${(ev: Event) => setUI({ options: lines(ev) })}></textarea></label>` : nothing}
    ${has("button", "icon", "list", "card", "toast") ? html`<label>${t("proto.icon")}<input list="phx-proto-icons" .value=${u.icon ?? ""} @change=${(ev: Event) => setUI({ icon: val(ev) })}>
      <datalist id="phx-proto-icons">${globalThis.PhxProto.icons.map((n) => html`<option value=${n}></option>`)}</datalist></label>` : nothing}
    ${has("list", "table", "chart", "tabs", "navbar", "select") ? html`<label>${t(has("tabs", "navbar") ? "proto.item_icons" : "proto.fields")}<input .value=${(u.fields ?? []).join(", ")} @change=${(ev: Event) => setUI({ fields: csv(ev) })}></label>` : nothing}
    ${has("input", "textarea", "checkbox", "switch", "radio", "select", "slider", "tabs", "navbar", "list", "progress") ? html`<label>${t("proto.bind")}<select @change=${(ev: Event) => setUI({ bind: val(ev) })}>
      <option value="">${t("proto.none")}</option>${vars.map((v) => html`<option value=${v.name} ?selected=${u.bind === v.name}>${v.name}</option>`)}</select></label>` : nothing}
    ${has("list", "table", "chart", "select") ? html`<label>${t("proto.data")}<select @change=${(ev: Event) => setUI({ data: val(ev) })}>
      <option value="">${t("proto.none")}</option>${data.map((d) => html`<option value=${d.name} ?selected=${u.data === d.name}>${d.name}</option>`)}</select></label>` : nothing}
    ${has("slider", "progress") ? html`<div class="two"><label>${t("proto.min")}<input type="number" .value=${u.min === undefined ? "" : String(u.min)} @change=${(ev: Event) => setUI({ min: val(ev) === "" ? undefined : Number(val(ev)) })}></label>
      <label>${t("proto.max")}<input type="number" .value=${u.max === undefined ? "" : String(u.max)} @change=${(ev: Event) => setUI({ max: val(ev) === "" ? undefined : Number(val(ev)) })}></label></div>` : nothing}
    <label>${t("proto.show_if")}<input placeholder=${t("proto.show_placeholder")} .value=${u.show ?? ""} @change=${(ev: Event) => setUI({ show: val(ev) })}></label>
    ${p ? nothing : html`<p class="muted" style="margin:0">${t("proto.off_screen")}</p>`}
    ${actionsView(c, false)}
  </aside>`;
}

// ---- prototype settings -----------------------------------------------------------

export interface SettingsCtx {
  board: Whiteboard;
  id: string;
  project: string;
  doc: string;
  canEdit: boolean;
  setProto: (id: string, patch: Partial<WBPrototype> | null) => void;
  pick: (id: string) => void;
  play: (proto: string) => void;
  close: () => void;
}

function parseRows(text: string): WBData["rows"] {
  const s = text.trim();
  if (!s) return [];
  if (s.startsWith("[")) {
    const v = JSON.parse(s);
    if (!Array.isArray(v)) throw new Error("rows");
    return v;
  }
  // CSV with a header line (comma or semicolon).
  const lines = s.split(/\r?\n/).filter((l) => l.trim());
  const sep = lines[0].includes(";") ? ";" : ",";
  const head = lines[0].split(sep).map((h) => h.trim());
  return lines.slice(1).map((l) => {
    const cells = l.split(sep);
    return Object.fromEntries(head.map((h, i) => {
      const c = (cells[i] ?? "").trim();
      return [h, c !== "" && !isNaN(Number(c)) ? Number(c) : c];
    }));
  });
}

export function settingsDialog(c: SettingsCtx): TemplateResult {
  const p = protoById(c.board, c.id);
  if (!p) return html``;
  const ro = !c.canEdit;
  const set = (patch: Partial<WBPrototype>) => c.setProto(p.id, patch);
  const vars = p.vars ?? [], data = p.data ?? [];
  const setVar = (i: number, v: Partial<WBVar>) => set({ vars: vars.map((x, k) => (k === i ? { ...x, ...v } : x)) });
  const setData = (i: number, d: Partial<WBData>) => set({ data: data.map((x, k) => (k === i ? { ...x, ...d } : x)) });
  const typed = (type: WBVar["type"], raw: string): unknown => {
    if (type === "number") return Number(raw) || 0;
    if (type === "bool") return raw === "true" || raw === "1";
    if (type === "list") return raw.split(",").map((x) => x.trim()).filter(Boolean);
    return raw;
  };
  const shown = (v: WBVar) => (Array.isArray(v.value) ? v.value.join(", ") : v.value === undefined || v.value === null ? "" : String(v.value));
  const screens = screensOf(c.board, p.id).filter((s) => !s.screen?.overlay);
  const base = `/api/v1/projects/${c.project}/documents/${c.doc}/prototypes/${encodeURIComponent(p.id)}`;
  const makeApp = async () => {
    try {
      const r = await api<{ document?: { slug: string } }>("POST", `/projects/${c.project}/documents/${c.doc}/prototypes/${encodeURIComponent(p.id)}/app`, { lang: document.documentElement.lang });
      c.close();
      if (r.document) navigate(`/p/${c.project}/d/${r.document.slug}`);
      else toast(t("proto.app_suggested"));
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    }
  };
  return html`<dialog class="proto wbdlg" aria-labelledby="wbpt" @cancel=${(e: Event) => { e.preventDefault(); c.close(); }} @keydown=${stop}>
    <h2 id="wbpt">${t("proto.settings_title")}</h2>
    ${(c.board.prototypes ?? []).length > 1 ? html`<label>${t("proto.prototype")}<select @change=${(ev: Event) => c.pick(val(ev))}>${(c.board.prototypes ?? []).map((x) => html`<option value=${x.id} ?selected=${x.id === p.id}>${x.name}</option>`)}</select></label>` : nothing}
    <div class="two">
      <label>${t("proto.name")}<input .value=${p.name} ?disabled=${ro} @change=${(ev: Event) => set({ name: val(ev).trim() || p.name })}></label>
      <label>${t("proto.look")}<select ?disabled=${ro} @change=${(ev: Event) => set({ look: (val(ev) || undefined) as "sketch" | undefined })}>
        <option value="" ?selected=${!p.look}>${t("proto.look_theme")}</option><option value="sketch" ?selected=${p.look === "sketch"}>${t("proto.look_sketch")}</option></select></label>
    </div>
    <label>${t("proto.start_screen")}<select ?disabled=${ro} @change=${(ev: Event) => set({ start: val(ev) || undefined })}>
      <option value="">${t("proto.first_screen")}</option>${screens.map((s) => html`<option value=${s.id} ?selected=${p.start === s.id}>${s.text || s.id}</option>`)}</select></label>

    <h3>${t("proto.variables")}</h3>
    <p class="muted">${t("proto.variables_hint")}</p>
    <ul class="plist">${vars.map((v, i) => html`<li>
      <input aria-label=${t("proto.name")} .value=${v.name} ?disabled=${ro} pattern="[A-Za-z_][A-Za-z0-9_]*" @change=${(ev: Event) => setVar(i, { name: val(ev).replace(/[^A-Za-z0-9_]/g, "_") })}>
      <select aria-label=${t("proto.type")} ?disabled=${ro} @change=${(ev: Event) => { const type = val(ev) as WBVar["type"]; setVar(i, { type, value: typed(type, shown(v)) }); }}>${(["text", "number", "bool", "list"] as const).map((x) => html`<option value=${x} ?selected=${v.type === x}>${t(`proto.type_${x}` as any)}</option>`)}</select>
      <input aria-label=${t("proto.start_value")} placeholder=${t("proto.start_value")} .value=${shown(v)} ?disabled=${ro} @change=${(ev: Event) => setVar(i, { value: typed(v.type, val(ev)) })}>
      ${ro ? nothing : html`<button class="ghost danger" aria-label=${t("proto.remove")} @click=${() => set({ vars: vars.filter((_, k) => k !== i) })}>${icon("Trash2", 13)}</button>`}</li>`)}</ul>
    ${ro ? nothing : html`<button class="ghost" @click=${() => set({ vars: [...vars, { name: `var${vars.length + 1}`, type: "text", value: "" }] })}>${icon("Plus", 13)} ${t("proto.add_variable")}</button>`}

    <h3>${t("proto.data_sources")}</h3>
    <p class="muted">${t("proto.data_hint")}</p>
    ${data.map((d, i) => html`<fieldset class="pdata">
      <div class="two"><label>${t("proto.name")}<input .value=${d.name} ?disabled=${ro} @change=${(ev: Event) => setData(i, { name: val(ev).replace(/[^A-Za-z0-9_]/g, "_") })}></label>
        <label>${t("proto.source")}<select ?disabled=${ro} @change=${(ev: Event) => setData(i, { source: val(ev) as WBData["source"] })}>${(["sample", "query", "tasks", "checks"] as const).map((x) => html`<option value=${x} ?selected=${d.source === x}>${t(`proto.source_${x}` as any)}</option>`)}</select></label></div>
      ${d.source === "query" || d.source === "tasks" ? html`<label>${t(d.source === "query" ? "proto.ref_query" : "proto.ref_board")}<input .value=${d.ref ?? ""} ?disabled=${ro} @change=${(ev: Event) => setData(i, { ref: val(ev).trim() })}></label>` : nothing}
      <label>${t(d.source === "sample" ? "proto.rows" : "proto.rows_fallback")}<textarea rows="4" spellcheck="false" ?disabled=${ro} .value=${JSON.stringify(d.rows ?? [], null, 0)}
        @change=${(ev: Event) => { try { setData(i, { rows: parseRows((ev.target as HTMLTextAreaElement).value) }); } catch { toast(t("proto.rows_invalid"), "error"); } }}></textarea></label>
      ${ro ? nothing : html`<button class="ghost danger" @click=${() => set({ data: data.filter((_, k) => k !== i) })}>${icon("Trash2", 13)} ${t("proto.remove")}</button>`}
    </fieldset>`)}
    ${ro ? nothing : html`<button class="ghost" @click=${() => set({ data: [...data, { name: `data${data.length + 1}`, source: "sample", rows: [{ title: "Example", value: 1 }] }] })}>${icon("Plus", 13)} ${t("proto.add_data")}</button>`}

    <div class="acts">
      ${ro ? nothing : html`<button class="ghost danger" style="margin-right:auto" @click=${() => { if (confirm(t("proto.delete_confirm", { name: p.name }))) { c.setProto(p.id, null); c.close(); } }}>${icon("Trash2", 14)} ${t("proto.delete")}</button>`}
      <a class="btn ghost" href=${`${base}/export?lang=${document.documentElement.lang}`} download>${icon("Download", 14)} ${t("proto.export")}</a>
      ${ro ? nothing : html`<button class="ghost" @click=${() => void makeApp()}>${icon("AppWindow", 14)} ${t("proto.make_app")}</button>`}
      <button class="ghost" @click=${c.close}>${t("proto.close")}</button>
      <button @click=${() => { c.close(); c.play(p.id); }}>${icon("Play", 14)} ${t("proto.play")}</button>
    </div>
  </dialog>`;
}

// ---- play mode --------------------------------------------------------------------

/** Plays a prototype full screen. In a project, live data comes with the
 * viewer's rights and comments land on the board; in share links the
 * sample data plays and there are no comments. */
export class PhxProtoPlay extends LitElement {
  static properties = { project: {}, doc: {}, board: { attribute: false }, version: { type: Number }, proto: {}, start: {}, token: {}, canComment: { type: Boolean } };
  static styles = css`
    :host { position: fixed; inset: 0; z-index: 1000; display: block; background: var(--canvas, var(--bg)); }
    .root { position: absolute; inset: 0; outline: none; }
  `;

  project = "";
  doc = "";
  board?: Whiteboard;
  /** Changes when the board changes (the board object itself may stay). */
  version = 0;
  proto = "";
  start = "";
  token = "";
  canComment = false;
  private ctl?: PlayerController;
  private data: Record<string, unknown[]> = {};
  private prevFocus: Element | null = null;
  /** Escape while the focus is outside the player (it handles its own). */
  private onKey = (ev: KeyboardEvent) => {
    if (ev.key === "Escape" && !ev.defaultPrevented && this.ctl?.escape()) ev.preventDefault();
  };

  connectedCallback() {
    super.connectedCallback();
    window.addEventListener("keydown", this.onKey);
  }

  private model(): PlayerModel {
    return { board: this.board!, proto: this.proto, data: this.data, lang: document.documentElement.lang, strings: playerStrings() };
  }

  firstUpdated() {
    const root = this.renderRoot.querySelector(".root") as HTMLElement;
    this.prevFocus = document.activeElement;
    this.data = sampleData(protoById(this.board!, this.proto));
    this.ctl = globalThis.PhxProto.mount(root, this.model(), {
      start: this.start || undefined,
      onClose: () => this.dispatchEvent(new CustomEvent("close")),
      fileURL: (id) => (this.token ? `/api/v1/shared/${this.token}/files/${encodeURIComponent(id)}` : `/api/v1/files/${encodeURIComponent(id)}`),
      onComment: this.canComment && !this.token
        ? (c) => api<WBComment>("POST", `/projects/${this.project}/documents/${this.doc}/whiteboard/comments`, c)
        : undefined,
    });
    root.focus();
    if (!this.token && this.project) {
      void api<Record<string, unknown[]>>("GET", `/projects/${this.project}/documents/${this.doc}/prototypes/${encodeURIComponent(this.proto)}/data`)
        .then((d) => { this.data = d ?? this.data; this.ctl?.update(this.model()); }).catch(() => {});
    }
  }

  updated(changed: Map<string, unknown>) {
    if ((changed.has("version") && changed.get("version") !== undefined) || (changed.has("board") && changed.get("board") !== undefined)) {
      if (this.ctl && this.board) this.ctl.update(this.model());
    }
  }

  disconnectedCallback() {
    super.disconnectedCallback();
    window.removeEventListener("keydown", this.onKey);
    this.ctl?.destroy();
    (this.prevFocus as HTMLElement | null)?.focus?.();
  }

  render() {
    return html`<div class="root" tabindex="-1" role="dialog" aria-modal="true" aria-label=${protoById(this.board!, this.proto)?.name ?? t("proto.play")}></div>`;
  }
}
customElements.get("phx-proto-play") || customElements.define("phx-proto-play", PhxProtoPlay);
