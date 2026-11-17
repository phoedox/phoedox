// UI themes (ui-themes-concept): the personal appearance page, the theme
// lists of projects, organizations and the instance, and the theme editor
// with live preview and contrast check.

import { LitElement, css, html, nothing } from "lit";
import { ApiError, api } from "../api";
import { contrast } from "../color";
import { t, type Key } from "../i18n";
import { icon } from "../icon";
import { setTheme, theme, type Theme } from "../state";
import {
  activeTheme, emptyTheme, loadTheme, previewTheme,
  type EffectiveTheme, type MyTheme, type ThemeCatalog, type ThemeData, type ThemeProblem, type ThemeSummary, type ThemeView,
} from "../theme-runtime";
import { pageHead, scard, settingsUI, srow, sseg, sswitch } from "./settings-ui";
import { base } from "./shared";
import { toast } from "./toasts";
import "./store-items";

const BASE_TOKENS = ["bg", "surface", "surface2", "line", "fg", "muted", "accent", "accent-soft", "signal", "bad", "good", "wire", "on-color"];
const COMPONENT_TOKENS = ["btn-bg", "btn-fg", "btn-ghost-bg", "btn-ghost-fg", "input-bg", "input-border", "sidebar-bg", "header-bg", "link", "focus"];
const FALLBACK: Record<string, string> = {
  "btn-bg": "accent", "btn-fg": "surface", "btn-ghost-bg": "surface", "btn-ghost-fg": "fg", "input-bg": "surface", "input-border": "line",
  "sidebar-bg": "bg", "header-bg": "surface", link: "accent", focus: "accent",
};

type Mode = "light" | "dark";
const tokenLabel = (k: string) => t(`theme.tok.${k}` as Key);
const scopeLabel = (s: string) => t(`theme.scope.${s}` as Key);
const sourceLabel = (s: string) => t(`theme.source.${s}` as Key);

/** The colours of one mode: Phoedox's defaults, the theme's values and the
 *  fallbacks of component tokens. */
export function paletteOf(defaults: Record<string, string>, set: Record<string, string>): Record<string, string> {
  const p: Record<string, string> = { ...defaults, ...set };
  for (const [k, b] of Object.entries(FALLBACK)) if (!p[k]) p[k] = p[b];
  return p;
}

const hx = (h: string) => [1, 3, 5].map((i) => parseInt(h.slice(i, i + 2), 16));
const toHex = (c: number[]) => "#" + c.map((v) => Math.round(Math.max(0, Math.min(255, v))).toString(16).padStart(2, "0")).join("");

/** Moves a text colour away from its background until it reaches need. */
export function fixContrast(fg: string, bg: string, need = 4.5): string {
  let c = hx(fg);
  const b = hx(bg);
  const darker = contrast([0, 0, 0], b) > contrast([255, 255, 255], b);
  for (let i = 0; i < 40 && contrast(c, b) < need; i++) c = darker ? c.map((v) => v * 0.92) : c.map((v) => v + (255 - v) * 0.1);
  return toHex(c);
}

/** A small preview of a theme from its swatch (bg, surface, fg, accent per mode). */
function swatchTile(sw: string[] | undefined, dark: boolean) {
  const s = sw ?? [];
  const o = dark ? 4 : 0;
  return html`<span class="sw-tile" aria-hidden="true" style="background:${s[o]};border-color:${s[o + 1]}">
    <i style="background:${s[o + 1]}"><b style="background:${s[o + 2]}"></b><b class="short" style="background:${s[o + 2]};opacity:.55"></b></i>
    <em style="background:${s[o + 3]}"></em></span>`;
}

const tileStyles = css`
  .tiles { display: grid; grid-template-columns: repeat(auto-fill, minmax(150px, 1fr)); gap: 10px; padding: 0 18px 16px; }
  .tile { display: grid; gap: 6px; padding: 8px; border: 1px solid var(--line); border-radius: var(--radius-card, 12px); background: var(--surface); color: var(--fg); cursor: pointer; text-align: left; font: inherit; font-weight: 400; }
  .tile[aria-pressed="true"] { border-color: var(--accent); box-shadow: inset 0 0 0 1px var(--accent); }
  .tile:disabled { cursor: not-allowed; }
  .tile .nm { display: flex; gap: 6px; align-items: center; font-size: 13.5px; font-weight: 500; overflow-wrap: anywhere; }
  .tile .own { color: var(--muted); font-size: 12px; }
  .sw-tile { display: grid; grid-template-columns: 1fr auto; align-items: end; gap: 6px; height: 58px; padding: 8px; border-radius: 8px; border: 1px solid; }
  .sw-tile i { display: grid; gap: 4px; padding: 6px; border-radius: 5px; align-content: start; height: 100%; box-sizing: border-box; }
  .sw-tile b { display: block; height: 4px; border-radius: 2px; }
  .sw-tile b.short { width: 60%; }
  .sw-tile em { width: 16px; height: 16px; border-radius: 50%; }
  .pair { display: grid; grid-template-columns: 1fr 1fr; gap: 4px; }
  @media (hover: hover) and (pointer: fine) { .tile:not([aria-pressed="true"]):not(:disabled):hover { border-color: var(--muted); } }
`;

/** Personal settings → Appearance. */
export class PhxAppearance extends LitElement {
  static properties = { cat: { state: true }, busy: { state: true } };
  static styles = [base, settingsUI, tileStyles, css`
    :host { display: block; }
    .note { display: flex; gap: 10px; align-items: flex-start; margin: 0 18px 14px; padding: 10px 12px; border-radius: 10px; background: var(--surface2); font-size: 13.5px; }
    .note.warn { background: color-mix(in srgb, var(--signal) 14%, var(--surface)); }
    .slist .item .acts { display: flex; gap: 6px; }
    .empty { color: var(--muted); font-size: 13.5px; padding: 0 18px 16px; margin: 0; }
  `];

  cat?: ThemeCatalog;
  busy = false;

  connectedCallback() {
    super.connectedCallback();
    void this.load();
  }

  private async load() {
    this.cat = await api<ThemeCatalog>("GET", "/themes").catch(() => undefined);
  }

  private async save(mine: Partial<MyTheme>) {
    if (!this.cat) return;
    const next = { ...this.cat.mine, ...mine };
    this.busy = true;
    try {
      await api("PUT", "/me/theme", next);
      this.cat = { ...this.cat, mine: next };
      await loadTheme(activeProjectSlug(), true);
      void this.load();
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    } finally {
      this.busy = false;
    }
  }

  private async removeTheme(th: ThemeSummary) {
    if (!confirm(t("theme.delete_confirm", { name: th.name }))) return;
    try {
      await api("DELETE", `/themes/${th.id}`);
      toast(t("theme.deleted"));
      await loadTheme(activeProjectSlug(), true);
      void this.load();
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    }
  }

  render() {
    const c = this.cat;
    const dark = isDark();
    const eff = activeTheme.get() ?? c?.effective;
    const mine = c?.mine ?? { themeId: "", always: false, contrast: "" as const };
    const own = (c?.available ?? []).filter((x) => x.scope === "user");
    const choosable = c?.available ?? [];
    return html`${pageHead(t("theme.appearance"), t("theme.appearance_text"))}
      ${scard(t("theme.mode_title"), "", html`
        ${srow(t("sx.me.theme"), t("sx.me.theme_help"), sseg<Theme>(t("sx.me.theme"), theme.get(), [["system", t("sx.me.theme_system")], ["light", t("sx.me.theme_light")], ["dark", t("sx.me.theme_dark")]], (v) => { setTheme(v); this.requestUpdate(); }))}
        ${srow(t("theme.contrast"), t("theme.contrast_help"), sseg<MyTheme["contrast"]>(t("theme.contrast"), mine.contrast, [["", t("theme.contrast_off")], ["variant", t("theme.contrast_variant")], ["high", t("theme.contrast_high")]], (v) => this.save({ contrast: v })))}`)}
      ${c ? scard(t("theme.choose"), t("theme.choose_text"), html`
        ${eff?.forced ? html`<p class="note">${icon("Lock", 16)}<span>${t("theme.forced_note", { name: eff.theme?.name ?? "" })}</span></p>` : nothing}
        ${eff && !eff.forced && eff.source !== "builtin" && eff.source !== "mine" && eff.source !== "contrast" ? html`<p class="note">${icon("Info", 16)}<span>${t("theme.using", { name: eff.theme?.name ?? "", source: sourceLabel(eff.source) })}</span></p>` : nothing}
        ${eff?.cssWarning ? html`<p class="note warn">${icon("TriangleAlert", 16)}<span>${t("theme.css_warning")}</span></p>` : nothing}
        <div class="tiles" role="group" aria-label=${t("theme.choose")}>
          <button type="button" class="tile" aria-pressed=${mine.themeId === "" ? "true" : "false"} ?disabled=${this.busy} @click=${() => this.save({ themeId: "" })}>
            ${swatchTile(c.builtin.phoedox, dark)}<span class="nm">Phoedox</span><span class="own">${t("theme.builtin")}</span></button>
          ${choosable.map((x) => html`<button type="button" class="tile" aria-pressed=${mine.themeId === x.id ? "true" : "false"} ?disabled=${this.busy} @click=${() => this.save({ themeId: x.id })}>
            ${swatchTile(x.swatch, dark)}<span class="nm">${x.name}</span><span class="own">${scopeLabel(x.scope)}${x.ownerName && x.scope !== "user" ? ` · ${x.ownerName}` : ""}</span></button>`)}
        </div>
        ${srow(t("theme.always"), t("theme.always_help"), sswitch(mine.always, t("theme.always"), (on) => this.save({ always: on }), this.busy || !mine.themeId))}`) : nothing}
      ${scard(t("theme.mine"), t("theme.mine_text"), html`
        ${own.length ? html`<div class="slist">${own.map((x) => html`<div class="item">
          ${swatchTile(x.swatch, dark)}
          <div class="t"><b>${x.name}</b>${x.originId ? html`<span>${t("store.from_store", { version: x.originVersion ?? "" })}</span>` : nothing}</div>
          <div class="acts"><a class="btn ghost" href="/settings/themes/${x.id}">${icon("Pencil", 14)} ${t("doc.edit")}</a>
            <button class="danger" @click=${() => this.removeTheme(x)} aria-label=${t("doc.delete")}>${icon("Trash2", 14)}</button></div></div>`)}</div>`
          : html`<p class="empty">${t("theme.none_yet")}</p>`}
        <div class="body"><a class="btn" href="/settings/themes/new?scope=user">${icon("Plus", 14)} ${t("theme.new")}</a></div>`)}
      <phx-store-themes scope="user" @changed=${() => this.load()}></phx-store-themes>`;
  }
}

function isDark() {
  const d = document.documentElement.dataset.theme;
  return d === "dark" || (d !== "light" && matchMedia("(prefers-color-scheme: dark)").matches);
}

function activeProjectSlug() {
  const m = location.pathname.match(/^\/p\/([^/]+)/);
  return m ? decodeURIComponent(m[1]) : "";
}

/** The themes of a project, an organization or the instance, and which one
 *  is used there (project settings, organization page, administration). */
export class PhxThemeList extends LitElement {
  static properties = { scope: {}, owner: {}, cat: { state: true }, chosen: { state: true }, forced: { state: true }, busy: { state: true } };
  static styles = [base, settingsUI, tileStyles, css`
    :host { display: block; }
    .slist .item .acts { display: flex; gap: 6px; }
    .empty { color: var(--muted); font-size: 13.5px; padding: 0 18px 16px; margin: 0; }
  `];

  scope: "project" | "org" | "instance" = "project";
  owner = "";
  cat?: ThemeCatalog;
  chosen = "";
  forced = false;
  busy = false;

  connectedCallback() {
    super.connectedCallback();
    void this.load();
  }

  private async load() {
    this.cat = await api<ThemeCatalog>("GET", `/themes${this.scope === "project" ? `?project=${encodeURIComponent(this.owner)}` : ""}`).catch(() => undefined);
    if (this.scope === "project") {
      this.chosen = this.cat?.projectTheme ?? "";
    } else if (this.scope === "org") {
      const o = await api<{ themeId: string; forced: boolean }>("GET", `/orgs/${this.owner}/theme`).catch(() => ({ themeId: "", forced: false }));
      this.chosen = o.themeId;
      this.forced = o.forced;
    } else {
      this.chosen = this.cat?.instanceTheme ?? "";
    }
  }

  /** Themes that belong here, and themes the person may pick for here. */
  private lists() {
    const all = this.cat?.available ?? [];
    const mine = all.filter((x) => x.scope === this.scope && (this.scope === "instance" || x.owner === this.owner));
    const pick = this.scope === "instance" ? all.filter((x) => x.scope === "instance")
      : this.scope === "org" ? all.filter((x) => x.scope === "instance" || (x.scope === "org" && x.owner === this.owner))
      : all.filter((x) => x.scope === "instance" || x.scope === "org" || (x.scope === "project" && x.owner === this.owner));
    return { mine, pick };
  }

  private async choose(id: string, forced = this.forced) {
    this.busy = true;
    try {
      if (this.scope === "project") await api("PUT", `/projects/${this.owner}/theme`, { themeId: id });
      else if (this.scope === "org") await api("PUT", `/orgs/${this.owner}/theme`, { themeId: id, forced });
      else await api("PUT", "/admin/theme", { themeId: id });
      this.chosen = id;
      this.forced = forced && !!id;
      toast(t("doc.saved"));
      await loadTheme(activeProjectSlug(), true);
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    } finally {
      this.busy = false;
    }
  }

  private async removeTheme(th: ThemeSummary) {
    if (!confirm(t("theme.delete_confirm", { name: th.name }))) return;
    try {
      await api("DELETE", `/themes/${th.id}`);
      toast(t("theme.deleted"));
      void this.load();
      await loadTheme(activeProjectSlug(), true);
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    }
  }

  render() {
    if (!this.cat) return nothing;
    const dark = isDark();
    const { mine, pick } = this.lists();
    const ownerQ = this.scope === "instance" ? "" : `&owner=${encodeURIComponent(this.owner)}`;
    const help = t(`theme.use_help_${this.scope}` as Key);
    return html`
      ${scard(t(`theme.use_${this.scope}` as Key), help, html`
        <div class="tiles" role="group" aria-label=${t(`theme.use_${this.scope}` as Key)}>
          <button type="button" class="tile" aria-pressed=${this.chosen === "" ? "true" : "false"} ?disabled=${this.busy} @click=${() => this.choose("")}>
            ${swatchTile(this.cat.builtin.phoedox, dark)}<span class="nm">${this.scope === "project" ? t("theme.none_project") : "Phoedox"}</span></button>
          ${pick.map((x) => html`<button type="button" class="tile" aria-pressed=${this.chosen === x.id ? "true" : "false"} ?disabled=${this.busy} @click=${() => this.choose(x.id)}>
            ${swatchTile(x.swatch, dark)}<span class="nm">${x.name}</span><span class="own">${scopeLabel(x.scope)}</span></button>`)}
        </div>
        ${this.scope === "org" ? srow(t("theme.force"), t("theme.force_help"), sswitch(this.forced, t("theme.force"), (on) => this.choose(this.chosen, on), this.busy || !this.chosen)) : nothing}`)}
      ${scard(t(`theme.own_${this.scope}` as Key), t("theme.own_text"), html`
        ${mine.length ? html`<div class="slist">${mine.map((x) => html`<div class="item">
          ${swatchTile(x.swatch, dark)}<div class="t"><b>${x.name}</b>${x.originId ? html`<span>${t("store.from_store", { version: x.originVersion ?? "" })}</span>` : nothing}</div>
          ${x.canEdit ? html`<div class="acts"><a class="btn ghost" href="/settings/themes/${x.id}${this.owner ? `?owner=${encodeURIComponent(this.owner)}` : ""}">${icon("Pencil", 14)} ${t("doc.edit")}</a>
            <button class="danger" @click=${() => this.removeTheme(x)} aria-label=${t("doc.delete")}>${icon("Trash2", 14)}</button></div>` : html`<span></span>`}</div>`)}</div>`
          : html`<p class="empty">${t("theme.none_yet")}</p>`}
        <div class="body"><a class="btn" href="/settings/themes/new?scope=${this.scope}${ownerQ}">${icon("Plus", 14)} ${t("theme.new")}</a></div>`)}
      <phx-store-themes .scope=${this.scope} .owner=${this.owner} @changed=${() => this.load()}></phx-store-themes>`;
  }
}

/** The theme editor: colours per mode, fonts, shape, own CSS, with a live
 *  preview and the contrast check. */
export class PhxThemeEditor extends LitElement {
  static properties = {
    themeId: {}, scope: {}, owner: {},
    cat: { state: true }, view: { state: true }, name: { state: true }, data: { state: true }, cssText: { state: true },
    mode: { state: true }, problems: { state: true }, cssError: { state: true }, busy: { state: true }, live: { state: true }, dirty: { state: true }, asking: { state: true },
  };
  static styles = [base, settingsUI, css`
    :host { display: block; max-width: 1240px; margin: 0 auto; padding: 24px 16px 80px; }
    .srow { grid-template-columns: 1fr; gap: 8px; }
    .srow > .c { justify-content: flex-start; }
    .srow > .c > .sw { margin-left: auto; }
    .srow:has(.sw) { grid-template-columns: minmax(0, 1fr) auto; }
    input[type=range] { accent-color: var(--accent); padding: 0; border: 0; background: transparent; }
    .top { display: flex; gap: 10px; align-items: center; flex-wrap: wrap; margin-bottom: 16px; }
    .font-slot { display: grid; gap: 6px; align-content: start; }
    .font-slot button { justify-self: start; }
    .top a.back { display: inline-flex; gap: 4px; align-items: center; color: var(--muted); text-decoration: none; font-size: 13.5px; }
    .top input.name { flex: 1 1 220px; font-size: 20px; font-weight: 600; font-family: var(--font-display); }
    .top .chip { white-space: nowrap; }
    dialog.ai { border: 1px solid var(--line); border-radius: var(--radius-card, 12px); background: var(--surface); color: var(--fg); padding: 20px; width: min(480px, 92vw); }
    dialog.ai::backdrop { background: rgb(0 0 0 / .35); }
    dialog.ai form { display: grid; gap: 12px; }
    dialog.ai textarea { min-height: 80px; font-family: var(--font-body); }
    .layout { display: grid; grid-template-columns: minmax(0, 1fr) minmax(0, 380px); gap: 22px; align-items: start; }
    .side { position: sticky; top: calc(var(--header-h) + 16px); display: grid; gap: 12px; }
    .colors { display: grid; grid-template-columns: repeat(auto-fill, minmax(200px, 1fr)); gap: 8px 14px; padding: 4px 18px 16px; }
    .color { display: grid; grid-template-columns: 34px 1fr auto; gap: 8px; align-items: center; font-size: 13.5px; }
    .color input[type=color] { width: 34px; height: 30px; padding: 2px; border-radius: 7px; cursor: pointer; }
    .color .lbl { display: grid; min-width: 0; }
    .color .lbl small { color: var(--muted); font: 11.5px var(--font-mono); }
    .color .lbl .set { color: var(--fg); }
    .color button.reset { padding: 4px; background: transparent; color: var(--muted); border: 0; visibility: hidden; }
    .color:has(.set) button.reset { visibility: visible; }
    details { border-top: 1px solid var(--line); }
    summary { cursor: pointer; padding: 12px 18px; font-weight: 500; font-size: 14px; }
    .grid2 { display: grid; grid-template-columns: repeat(auto-fill, minmax(200px, 1fr)); gap: 10px; padding: 0 18px 16px; }
    textarea.css { min-height: 180px; }
    .problems { display: grid; gap: 6px; padding: 0 18px 16px; margin: 0; list-style: none; }
    .problems li { display: flex; gap: 8px; align-items: center; justify-content: space-between; font-size: 13px; padding: 6px 10px; border-radius: 8px; background: color-mix(in srgb, var(--bad) 9%, var(--surface)); }
    .problems li button { padding: 3px 9px; font-size: 12.5px; }
    .ok { display: flex; gap: 6px; align-items: center; color: var(--good); font-size: 13.5px; padding: 0 18px 16px; margin: 0; }
    .rel { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; font-size: 13px; color: var(--muted); padding: 0 18px 14px; }
    .rel.warn { color: var(--signal); }
    .error { padding: 0 18px 12px; margin: 0; }
    /* The preview draws with the theme's tokens in its own box. */
    .pv { border: 1px solid var(--line); border-radius: var(--radius-card, 12px); overflow: hidden; background: var(--bg); color: var(--fg); font-family: var(--font-body); }
    .pv .ph { display: flex; gap: 8px; align-items: center; height: 38px; padding: 0 12px; background: var(--header-bg, var(--surface)); border-bottom: 1px solid var(--line); font: 600 13px var(--font-display); }
    .pv .ph i { width: 14px; height: 14px; border-radius: 4px; background: var(--accent); }
    .pv .pb { display: grid; grid-template-columns: 92px 1fr; min-height: 250px; }
    .pv nav { background: var(--sidebar-bg, var(--bg)); border-right: 1px solid var(--line); padding: 10px 8px; display: grid; gap: 4px; align-content: start; font-size: 12px; }
    .pv nav span { padding: 4px 6px; border-radius: 6px; color: var(--muted); }
    .pv nav span.on { background: var(--surface); color: var(--fg); box-shadow: inset 0 0 0 1px var(--line); }
    .pv .main { padding: 12px; display: grid; gap: 10px; align-content: start; }
    .pv .card { padding: 12px; display: grid; gap: 8px; }
    .pv h3 { font-size: 16px; }
    .pv p { margin: 0; font-size: 13px; }
    .pv .row { gap: 6px; }
    .pv button { font-size: 12.5px; }
    .pv input { font-size: 12.5px; }
    .pv .chips { display: flex; gap: 6px; flex-wrap: wrap; }
    .pv .good { color: var(--good); } .pv .bad { color: var(--bad); } .pv .sig { color: var(--signal); }
    .pv code { font: 12px var(--font-mono); background: var(--surface2); padding: 1px 5px; border-radius: 4px; }
    @media (max-width: 980px) { .layout { grid-template-columns: 1fr; } .side { position: static; } }
  `];

  themeId = "";
  scope = "user";
  owner = "";
  cat?: ThemeCatalog;
  view?: ThemeView;
  name = "";
  data: ThemeData = emptyTheme();
  cssText = "";
  mode: Mode = isDark() ? "dark" : "light";
  problems: ThemeProblem[] = [];
  cssError = "";
  busy = false;
  live = false;
  dirty = false;
  private prevTheme: EffectiveTheme | null = null;
  private checkTimer = 0;

  connectedCallback() {
    super.connectedCallback();
    this.prevTheme = activeTheme.get();
    void this.load();
    window.addEventListener("beforeunload", this.onUnload);
  }

  disconnectedCallback() {
    super.disconnectedCallback();
    window.removeEventListener("beforeunload", this.onUnload);
    if (this.live) previewTheme(null, this.prevTheme);
  }

  private onUnload = (e: BeforeUnloadEvent) => {
    if (this.dirty) e.preventDefault();
  };

  private async load() {
    this.cat = await api<ThemeCatalog>("GET", "/themes").catch(() => undefined);
    if (this.themeId) {
      try {
        const v = await api<ThemeView>("GET", `/themes/${this.themeId}`);
        this.view = v;
        this.name = v.name;
        this.data = { ...emptyTheme(), ...v.data, light: { ...v.data.light }, dark: { ...v.data.dark } };
        this.cssText = v.css;
        this.scope = v.scope;
      } catch (e) {
        toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
      }
    } else {
      this.name = t("theme.new_name");
    }
    this.check();
  }

  private change(fn: (d: ThemeData) => void) {
    const d: ThemeData = { ...this.data, light: { ...this.data.light }, dark: { ...this.data.dark }, body: { ...this.data.body }, display: { ...this.data.display }, mono: { ...this.data.mono } };
    fn(d);
    this.data = d;
    this.dirty = true;
    this.check();
  }

  private check() {
    clearTimeout(this.checkTimer);
    this.checkTimer = window.setTimeout(async () => {
      const r = await api<{ problems: ThemeProblem[]; error?: string; params?: Record<string, unknown> }>("POST", "/themes/check", { scope: this.scope, name: this.name, data: this.data, css: this.cssText }).catch(() => undefined);
      if (!r) return;
      this.problems = r.problems ?? [];
      this.cssError = r.error ? t(r.error as Key, r.params) : "";
    }, 250);
    if (this.live) this.applyLive();
  }

  private applyLive() {
    previewTheme({ ...(this.view ?? ({} as ThemeView)), id: this.view?.id ?? "draft", name: this.name, css: this.cssText, cssReleasedHere: true, data: this.data, canEdit: true } as ThemeView, this.prevTheme);
  }

  private setLive(on: boolean) {
    this.live = on;
    if (on) this.applyLive();
    else previewTheme(null, this.prevTheme);
  }

  private palette(mode: Mode) {
    const defs = this.cat?.defaults[mode] ?? {};
    return paletteOf(defs, mode === "light" ? this.data.light : this.data.dark);
  }

  private fix(p: ThemeProblem) {
    const pal = this.palette(p.mode);
    this.change((d) => {
      const set = p.mode === "light" ? d.light : d.dark;
      set[p.fg] = fixContrast(pal[p.fg], pal[p.bg], p.need);
    });
  }

  private fixAll() {
    for (let i = 0; i < 3; i++) {
      for (const mode of ["light", "dark"] as Mode[]) {
        const pal = this.palette(mode);
        const set = mode === "light" ? this.data.light : this.data.dark;
        for (const p of this.problems.filter((x) => x.mode === mode)) {
          set[p.fg] = fixContrast(pal[p.fg], pal[p.bg], p.need);
          pal[p.fg] = set[p.fg];
        }
      }
    }
    this.change(() => undefined);
  }

  private async save() {
    this.busy = true;
    const body = { scope: this.scope, owner: this.owner, name: this.name, data: this.data, css: this.cssText };
    try {
      const v = this.view
        ? await api<ThemeView>("PUT", `/themes/${this.view.id}`, body)
        : await api<ThemeView>("POST", "/themes", body);
      this.dirty = false;
      toast(t("doc.saved"));
      if (!this.view) {
        // A new personal theme is chosen at once.
        if (v.scope === "user") await api("PUT", "/me/theme", { ...(this.cat?.mine ?? { always: false, contrast: "" }), themeId: v.id }).catch(() => undefined);
        this.view = v;
        this.themeId = v.id;
        history.replaceState({}, "", `/settings/themes/${v.id}`);
      } else this.view = v;
      if (this.live) this.setLive(false);
      await loadTheme(activeProjectSlug(), true);
      this.prevTheme = activeTheme.get();
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    } finally {
      this.busy = false;
    }
  }

  asking = false;

  private async generate(e: SubmitEvent) {
    e.preventDefault();
    const f = new FormData(e.target as HTMLFormElement);
    this.asking = true;
    try {
      const p = await api<{ name: string; data: ThemeData }>("POST", "/themes/generate", { description: String(f.get("description") ?? ""), url: String(f.get("url") ?? "").trim() });
      this.change((d) => Object.assign(d, emptyTheme(), p.data, { light: { ...p.data.light }, dark: { ...p.data.dark } }));
      if (p.name && (!this.view || this.name === t("theme.new_name"))) this.name = p.name;
      this.renderRoot.querySelector<HTMLDialogElement>("dialog.ai")?.close();
      toast(t("theme.ai_done"));
    } catch (err) {
      toast(err instanceof ApiError ? err.text : t("error.unknown"), "error");
    } finally {
      this.asking = false;
    }
  }

  private async release() {
    if (!this.view) return;
    try {
      this.view = await api<ThemeView>("POST", `/themes/${this.view.id}/release`);
      toast(t("theme.released", { version: this.cat?.version ?? "" }));
      await loadTheme(activeProjectSlug(), true);
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    }
  }

  private back() {
    return this.scope === "user" ? "/settings/appearance" : this.scope === "instance" ? "/admin/appearance" : this.scope === "org" ? `/orgs/${this.owner || ""}` : `/p/${this.owner}/settings/appearance`;
  }

  private colorRow(k: string) {
    const set = this.mode === "light" ? this.data.light : this.data.dark;
    const pal = this.palette(this.mode);
    const own = set[k];
    const fb = FALLBACK[k];
    return html`<label class="color">
      <input type="color" .value=${pal[k] ?? "#000000"} aria-label=${tokenLabel(k)}
        @input=${(e: Event) => this.change((d) => ((this.mode === "light" ? d.light : d.dark)[k] = (e.target as HTMLInputElement).value))}>
      <span class="lbl"><span>${tokenLabel(k)}</span><small class=${own ? "set" : ""}>${own ?? (fb ? t("theme.like", { token: tokenLabel(fb) }) : pal[k])}</small></span>
      <button type="button" class="reset" title=${t("theme.reset")} aria-label=${t("theme.reset")}
        @click=${(e: Event) => { e.preventDefault(); this.change((d) => delete (this.mode === "light" ? d.light : d.dark)[k]); }}>${icon("RotateCcw", 14)}</button>
    </label>`;
  }

  /** Uploads a theme asset (font: woff2, logo: PNG/JPEG/WebP) and returns its address. */
  private async uploadAsset(kind: "font" | "logo"): Promise<{ url: string; name: string } | undefined> {
    const input = document.createElement("input");
    input.type = "file";
    input.accept = kind === "font" ? ".woff2,font/woff2" : "image/png,image/jpeg,image/webp";
    const file = await new Promise<File | undefined>((res) => { input.onchange = () => res(input.files?.[0]); input.click(); });
    if (!file) return undefined;
    const body = new FormData();
    body.append("kind", kind);
    body.append("name", file.name);
    body.append("file", file, file.name);
    const res = await fetch("/api/v1/themes/assets", { method: "POST", body, headers: { "X-Phoedox": "1" }, credentials: "same-origin" });
    const json = await res.json().catch(() => ({}));
    if (!res.ok) {
      toast(new ApiError(res.status, json.code ?? "error", json.key ?? "error.unknown", json.params ?? {}).text, "error");
      return undefined;
    }
    return { url: json.url, name: file.name };
  }

  private async uploadFont(which: "body" | "display" | "mono") {
    const up = await this.uploadAsset("font");
    if (!up) return;
    const family = up.name.replace(/\.woff2$/i, "").replace(/[-_]+/g, " ").replace(/["';{}<>]/g, "").trim() || "Custom";
    this.change((d) => (d[which] = { family, url: up.url, license: "" }));
    toast(t("theme.font_uploaded"));
  }

  private fontSelect(which: "body" | "display" | "mono") {
    const fonts = this.cat?.fonts ?? [];
    const cur = this.data[which]?.family ?? "";
    const own = this.data[which]?.url;
    const list = which === "mono" ? fonts.filter((f) => /Mono|system-ui/.test(f)) : fonts.filter((f) => !/Mono/.test(f));
    return html`<div class="font-slot"><label class="field">${t(`theme.font_${which}` as Key)}
      <select @change=${(e: Event) => this.change((d) => (d[which] = { family: (e.target as HTMLSelectElement).value }))}>
        <option value="" ?selected=${cur === ""}>${t("theme.font_default")}</option>
        ${list.map((f) => html`<option value=${f} ?selected=${cur === f && !own} style="font-family:'${f}'">${f}</option>`)}
        ${cur && (own || !list.includes(cur)) ? html`<option value=${cur} selected>${cur}${own ? ` (${t("theme.font_own")})` : ""}</option>` : nothing}
      </select></label>
      <button type="button" class="ghost small" @click=${() => this.uploadFont(which)}>${icon("Upload", 13)} ${t("theme.font_upload")}</button>
      ${own ? html`<label class="field">${t("theme.font_family")}<input .value=${cur} maxlength="60"
          @change=${(e: Event) => this.change((d) => (d[which] = { ...d[which], family: (e.target as HTMLInputElement).value.replace(/["';{}<>]/g, "").trim() }))}></label>
        <label class="field">${t("theme.font_license")}<input .value=${this.data[which]?.license ?? ""} maxlength="200" required placeholder=${t("theme.font_license_ph")}
          @change=${(e: Event) => this.change((d) => (d[which] = { ...d[which], license: (e.target as HTMLInputElement).value.trim() }))}></label>` : nothing}</div>`;
  }

  private preview() {
    const pal = this.palette(this.mode);
    const d = this.data;
    const vars = Object.entries(pal).map(([k, v]) => `--${k}:${v}`);
    const font = (f: { family: string }, fb: string) => (f?.family ? `"${f.family}", ${fb}` : "");
    if (font(d.body, "system-ui")) vars.push(`--font-body:${font(d.body, "system-ui, sans-serif")}`);
    if (font(d.display, "system-ui")) vars.push(`--font-display:${font(d.display, "system-ui, sans-serif")}`);
    if (font(d.mono, "monospace")) vars.push(`--font-mono:${font(d.mono, "ui-monospace, monospace")}`);
    if (d.radius > 0) vars.push(`--radius:${d.radius}px`, `--radius-card:${Math.min(d.radius + 4, 28)}px`);
    const dens = { compact: ["4px", "5px"], normal: ["7px", "8px"], comfortable: ["10px", "11px"] }[d.density as "compact"];
    if (dens) vars.push(`--ctl-py:${dens[0]}`, `--input-py:${dens[1]}`);
    vars.push(`color-scheme:${this.mode}`);
    return html`<div class="pv" style=${vars.join(";")} aria-label=${t("theme.preview")}>
      <div class="ph"><i></i>${d.logo ? html`<img src=${d.logo} alt="" height="18">` : "Phoedox"}</div>
      <div class="pb">
        <nav><span class="on">${t("theme.pv_overview")}</span><span>${t("theme.pv_pages")}</span><span>${t("theme.pv_board")}</span></nav>
        <div class="main">
          <div class="card">
            <h3>${t("theme.pv_title")}</h3>
            <p>${t("theme.pv_text")} <a href="#" @click=${(e: Event) => e.preventDefault()}>${t("theme.pv_link")}</a>.</p>
            <p class="muted">${t("theme.pv_muted")} <code>vlan 20</code></p>
            <input placeholder=${t("theme.pv_input")}>
            <div class="row"><button type="button">${t("doc.save")}</button><button type="button" class="ghost">${t("doc.cancel")}</button></div>
          </div>
          <div class="chips"><span class="chip good">${icon("CircleCheck", 12)} ${t("theme.pv_ok")}</span><span class="chip sig">${icon("TriangleAlert", 12)} ${t("theme.pv_warn")}</span><span class="chip bad">${icon("CircleAlert", 12)} ${t("theme.pv_bad")}</span></div>
        </div>
      </div></div>`;
  }

  render() {
    if (!this.cat) return nothing;
    const v = this.view;
    const released = !this.cssText || (v && v.css === this.cssText && v.cssReleasedHere);
    const problems = this.problems;
    return html`
      <div class="top">
        <a class="back" href=${this.back()} @click=${(e: Event) => { if (this.dirty && !confirm(t("theme.leave_unsaved"))) e.preventDefault(); }}>${icon("ArrowLeft", 14)} ${t("print.back")}</a>
        <input class="name" .value=${this.name} aria-label=${t("theme.name")} maxlength="80" @input=${(e: Event) => { this.name = (e.target as HTMLInputElement).value; this.dirty = true; }}>
        <span class="chip">${scopeLabel(this.scope)}</span>
        <button class="ghost" @click=${() => this.renderRoot.querySelector<HTMLDialogElement>("dialog.ai")?.showModal()}>${icon("Sparkles", 14)} ${t("theme.ai")}</button>
        <button ?disabled=${this.busy || !this.name.trim()} @click=${this.save}>${icon("Save", 14)} ${t("doc.save")}</button>
      </div>
      <dialog class="ai" aria-labelledby="ait">
        <form method="dialog" @submit=${this.generate}>
          <h2 id="ait">${t("theme.ai_title")}</h2>
          <p class="muted">${t("theme.ai_text")}</p>
          <label class="field">${t("theme.ai_description")}<textarea name="description" rows="3" placeholder=${t("theme.ai_placeholder")}></textarea></label>
          <label class="field">${t("theme.ai_url")}<input name="url" type="url" placeholder="https://…"></label>
          <div class="row" style="justify-content:flex-end"><button type="button" class="ghost" @click=${() => this.renderRoot.querySelector<HTMLDialogElement>("dialog.ai")?.close()}>${t("doc.cancel")}</button>
            <button ?disabled=${this.asking}>${icon("Sparkles", 14)} ${this.asking ? t("theme.ai_busy") : t("theme.ai_go")}</button></div>
        </form>
      </dialog>
      <div class="layout">
        <div>
          ${scard(t("theme.colors"), t("theme.colors_text"), html`
            <div class="body">${sseg<Mode>(t("theme.mode"), this.mode, [["light", t("sx.me.theme_light")], ["dark", t("sx.me.theme_dark")]], (m) => (this.mode = m))}</div>
            <div class="colors">${BASE_TOKENS.map((k) => this.colorRow(k))}</div>
            <details><summary>${t("theme.components")}</summary><div class="colors">${COMPONENT_TOKENS.map((k) => this.colorRow(k))}</div></details>`)}
          ${scard(t("theme.contrast_check"), t("theme.contrast_check_text"), problems.length
            ? html`<ul class="problems">${problems.map((p) => html`<li><span>${t("theme.problem", { fg: tokenLabel(p.fg), bg: tokenLabel(p.bg), mode: t(p.mode === "light" ? "sx.me.theme_light" : "sx.me.theme_dark"), ratio: p.ratio.toFixed(2) })}</span>
                <button type="button" class="ghost" @click=${() => this.fix(p)}>${icon("WandSparkles", 13)} ${t("theme.fix")}</button></li>`)}</ul>
                <div class="body"><button type="button" class="ghost" @click=${this.fixAll}>${icon("WandSparkles", 14)} ${t("theme.fix_all")}</button></div>`
            : html`<p class="ok">${icon("CircleCheck", 16)} ${t("theme.contrast_ok")}</p>`)}
          ${scard(t("theme.type_shape"), "", html`
            <div class="grid2">${this.fontSelect("body")}${this.fontSelect("display")}${this.fontSelect("mono")}</div>
            ${srow(t("theme.radius"), t("theme.radius_help"), html`<input type="range" min="0" max="24" .value=${String(this.data.radius)} aria-label=${t("theme.radius")}
              @input=${(e: Event) => this.change((d) => (d.radius = Number((e.target as HTMLInputElement).value)))}><span class="mono">${this.data.radius ? `${this.data.radius}px` : t("theme.font_default")}</span>`)}
            ${srow(t("theme.density"), t("theme.density_help"), sseg(t("theme.density"), (this.data.density || "normal") as "compact" | "normal" | "comfortable", [["compact", t("theme.density_compact")], ["normal", t("theme.density_normal")], ["comfortable", t("theme.density_comfortable")]], (x) => this.change((d) => (d.density = x === "normal" ? "" : x))))}
            ${srow(t("theme.logo"), t("theme.logo_help"), html`<span style="display:flex;gap:6px;align-items:center;flex-wrap:wrap">
              <input type="url" placeholder="https://…" .value=${this.data.logo ?? ""} @change=${(e: Event) => this.change((d) => (d.logo = (e.target as HTMLInputElement).value.trim() || undefined))}>
              <button type="button" class="ghost small" @click=${async () => { const up = await this.uploadAsset("logo"); if (up) this.change((d) => (d.logo = up.url)); }}>${icon("Upload", 13)} ${t("theme.logo_upload")}</button></span>`)}`)}
          ${scard(t("theme.css"), t("theme.css_text"), html`
            <div class="body"><textarea class="css" spellcheck="false" placeholder=".card { border-width: 2px; }" .value=${this.cssText}
              @input=${(e: Event) => { this.cssText = (e.target as HTMLTextAreaElement).value; this.dirty = true; this.check(); }}></textarea></div>
            ${this.cssError ? html`<p class="error">${this.cssError}</p>` : nothing}
            ${this.cssText ? html`<div class="rel ${released ? "" : "warn"}">${icon(released ? "BadgeCheck" : "TriangleAlert", 14)}
              ${released ? t("theme.css_released", { version: this.cat.version }) : v && v.css === this.cssText ? t("theme.css_not_released", { version: this.cat.version }) : t("theme.css_released_on_save", { version: this.cat.version })}
              ${v && v.css === this.cssText && !v.cssReleasedHere ? html`<button type="button" class="ghost" @click=${this.release}>${t("theme.release")}</button>` : nothing}</div>` : nothing}
            ${this.cssText ? srow(t("theme.css_off"), t("theme.css_off_help"), sswitch(!!this.data.cssOff, t("theme.css_off"), (on) => this.change((d) => (d.cssOff = on || undefined)))) : nothing}`)}
        </div>
        <div class="side">
          ${this.preview()}
          ${srowPlain(t("theme.live"), sswitch(this.live, t("theme.live"), (on) => this.setLive(on)))}
        </div>
      </div>`;
  }
}

const srowPlain = (label: string, control: unknown) => html`<div class="row" style="justify-content:space-between;font-size:13.5px"><span>${label}</span>${control}</div>`;

/** Applies the effective theme again (after changes elsewhere). */
export const reloadTheme = () => loadTheme(activeProjectSlug(), true);

customElements.get("phx-appearance") || customElements.define("phx-appearance", PhxAppearance);
customElements.get("phx-theme-list") || customElements.define("phx-theme-list", PhxThemeList);
customElements.get("phx-theme-editor") || customElements.define("phx-theme-editor", PhxThemeEditor);
