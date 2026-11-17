// UI themes at runtime: the effective theme's tokens become CSS custom
// properties on :root (light and dark), its fonts and shape set their
// tokens, and its own CSS (when released for this version) goes into a
// stylesheet that every component's shadow root adopts.
import { LitElement } from "lit";
import { Signal, signal } from "@lit-labs/signals";
import { api } from "./api";

export interface ThemeFont {
  family: string;
  url?: string;
  license?: string;
}
export interface ThemeData {
  light: Record<string, string>;
  dark: Record<string, string>;
  body: ThemeFont;
  display: ThemeFont;
  mono: ThemeFont;
  radius: number;
  density: "" | "compact" | "normal" | "comfortable";
  logo?: string;
  cssOff?: boolean;
}
export interface ThemeView {
  id: string;
  scope: string;
  ownerId: string;
  name: string;
  css: string;
  cssReleased: string;
  data: ThemeData;
  ownerName?: string;
  canEdit: boolean;
  cssReleasedHere: boolean;
  updatedAt?: string;
}
export interface EffectiveTheme {
  theme: ThemeView | null;
  source: "forced" | "mine" | "project" | "org" | "instance" | "builtin" | "contrast";
  forced: boolean;
  cssWarning?: boolean;
}
export interface ThemeSummary {
  id: string;
  name: string;
  scope: string;
  ownerName?: string;
  owner?: string;
  canEdit: boolean;
  swatch?: string[];
  originId?: string;
  originVersion?: string;
}
export interface MyTheme {
  themeId: string;
  always: boolean;
  contrast: "" | "high" | "variant";
}
export interface ThemeCatalog {
  available: ThemeSummary[];
  mine: MyTheme;
  effective: EffectiveTheme;
  fonts: string[];
  tokens: string[];
  defaults: { light: Record<string, string>; dark: Record<string, string> };
  version: string;
  builtin: { phoedox: string[]; contrast: string[] };
  projectTheme?: string;
  instanceTheme?: string;
}
export interface ThemeProblem {
  mode: "light" | "dark";
  fg: string;
  bg: string;
  ratio: number;
  need: number;
}

export const emptyTheme = (): ThemeData => ({
  light: {}, dark: {}, body: { family: "" }, display: { family: "" }, mono: { family: "" }, radius: 0, density: "",
});

/** The theme in use now (null = Phoedox's own). */
export const activeTheme = signal<EffectiveTheme | null>(null);

/** Stylesheet with the theme's own CSS, adopted by every shadow root. */
export const themeSheet: CSSStyleSheet | null =
  typeof CSSStyleSheet !== "undefined" && "replaceSync" in CSSStyleSheet.prototype ? new CSSStyleSheet() : null;

if (themeSheet) {
  // Lit collects a component class's styles once; the theme sheet goes last
  // so the theme's CSS can override the component's own rules.
  const RE = LitElement as unknown as { finalizeStyles(s: unknown): unknown[] };
  const orig = RE.finalizeStyles;
  RE.finalizeStyles = function (this: unknown, styles: unknown) {
    return [...orig.call(this, styles), themeSheet];
  };
  if (typeof document !== "undefined") document.adoptedStyleSheets = [...document.adoptedStyleSheets, themeSheet];
}

const fontStack = (f: ThemeFont, fallback: string) => (f.family ? `"${f.family}", ${fallback}` : "");
const quote = (s: string) => s.replace(/["\\]/g, "");

const density: Record<string, [string, string]> = { compact: ["4px", "5px"], normal: ["7px", "8px"], comfortable: ["10px", "11px"] };

function block(tokens: Record<string, string>): string {
  return Object.entries(tokens)
    .filter(([k, v]) => /^[a-z0-9-]+$/.test(k) && /^#[0-9a-fA-F]{6}$/.test(v))
    .map(([k, v]) => `--${k}: ${v};`)
    .join(" ");
}

/** Accent tokens: a project's colour overrides weaker themes' accent. */
const ACCENT = new Set(["accent", "accent-soft"]);
const split = (t: Record<string, string>) => {
  const accent: Record<string, string> = {}, rest: Record<string, string> = {};
  for (const [k, v] of Object.entries(t || {})) (ACCENT.has(k) ? accent : rest)[k] = v;
  return { accent, rest };
};

/** CSS for a theme's tokens, fonts and shape. strong: the theme's accent
 *  wins over a project's colour (forced, "always mine", project themes). */
export function themeTokensCSS(d: ThemeData, strong: boolean): string {
  const out: string[] = [];
  const fonts = [d.body, d.display, d.mono].filter((f) => f?.url && f.family);
  for (const f of fonts) out.push(`@font-face { font-family: "${quote(f.family)}"; src: url("${encodeURI(f.url!)}"); font-display: swap; }`);
  const root: string[] = [];
  if (d.body?.family) root.push(`--font-body: ${fontStack({ family: quote(d.body.family) }, "system-ui, sans-serif")};`);
  if (d.display?.family) root.push(`--font-display: ${fontStack({ family: quote(d.display.family) }, "system-ui, sans-serif")};`);
  if (d.mono?.family) root.push(`--font-mono: ${fontStack({ family: quote(d.mono.family) }, "ui-monospace, monospace")};`);
  if (d.radius > 0) root.push(`--radius: ${d.radius}px; --radius-card: ${Math.min(d.radius + 4, 28)}px;`);
  if (d.density && density[d.density]) root.push(`--ctl-py: ${density[d.density][0]}; --input-py: ${density[d.density][1]};`);
  const l = split(d.light), k = split(d.dark);
  const acc = strong ? ":root, :root[data-pc]" : ":root:not([data-pc])";
  out.push(`:root { ${root.join(" ")} ${block(l.rest)} }`);
  if (Object.keys(l.accent).length) out.push(`${acc} { ${block(l.accent)} }`);
  const dark = block(k.rest), darkAcc = block(k.accent);
  if (dark || darkAcc) {
    const sel = (pre: string) => (strong ? `${pre}, ${pre}[data-pc]` : `${pre}:not([data-pc])`);
    out.push(`@media (prefers-color-scheme: dark) { :root:not([data-theme="light"]) { ${dark} } ${darkAcc ? `${sel(':root:not([data-theme="light"])')} { ${darkAcc} }` : ""} }`);
    out.push(`:root[data-theme="dark"] { ${dark} } ${darkAcc ? `${sel(':root[data-theme="dark"]')} { ${darkAcc} }` : ""}`);
  }
  return out.join("\n");
}

let tokenStyle: HTMLStyleElement | null = null;

/** Applies an effective theme (or Phoedox's own with null). */
export function applyEffectiveTheme(e: EffectiveTheme | null) {
  activeTheme.set(e);
  if (typeof document === "undefined") return;
  const t = e?.theme;
  if (!tokenStyle) {
    tokenStyle = document.createElement("style");
    tokenStyle.id = "phx-theme";
    document.head.append(tokenStyle);
  }
  const strong = !!e && ["forced", "mine", "project", "contrast"].includes(e.source);
  tokenStyle.textContent = t ? themeTokensCSS(t.data, strong) : "";
  const css = t && t.css && t.cssReleasedHere && !t.data.cssOff ? t.css : "";
  try {
    themeSheet?.replaceSync(css);
  } catch {
    themeSheet?.replaceSync("");
  }
  document.documentElement.dataset.themeSource = e?.source ?? "builtin";
}

let loaded = "";
let pending: Promise<void> | null = null;

/** Loads and applies the effective theme for a project ("" = outside). */
export function loadTheme(project = "", force = false): Promise<void> {
  if (!force && loaded === project && pending) return pending;
  loaded = project;
  pending = api<ThemeCatalog>("GET", `/themes${project ? `?project=${encodeURIComponent(project)}` : ""}`)
    .then((c) => {
      if (loaded === project) applyEffectiveTheme(c.effective);
    })
    .catch(() => {
      /* signed out or offline: Phoedox's own look */
    });
  return pending;
}

/** Previews a theme without saving (the editor); null ends the preview. */
export function previewTheme(t: ThemeView | null, fallback: EffectiveTheme | null) {
  applyEffectiveTheme(t ? { theme: t, source: "mine", forced: false } : fallback);
}

/** Keeps the theme in step with the signed-in person and the open project. */
export function syncTheme(key: () => { user: string; project: string }) {
  const c = new Signal.Computed(key);
  const w = new Signal.subtle.Watcher(() => queueMicrotask(run));
  let last = "";
  function run() {
    const k = c.get();
    w.watch();
    const id = `${k.user}|${k.project}`;
    if (id === last) return;
    last = id;
    if (!k.user) applyEffectiveTheme(null);
    else void loadTheme(k.project);
  }
  w.watch(c);
  run();
}
