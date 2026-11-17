// Elements behind the page building blocks of ADR-0031. They render inside
// pages (doc view, shared view, editor) and only load what they need.

import { LitElement, css, html, type PropertyValues } from "lit";
import { ApiError, api, type Doc } from "../api";
import { locale, t } from "../i18n";
import { icon, iconSVG } from "../icon";
import { attrLabel } from "../model";
import { base } from "./shared";
import "./chart-block";
import "./block-view";
import "./plugin-block";
import "./page-blocks";

/** Formula: KaTeX as MathML (no fonts or styles needed). */
export class PhxMath extends HTMLElement {
  private done = "";
  connectedCallback() {
    void this.render();
  }

  static get observedAttributes() {
    return ["data-tex"];
  }

  attributeChangedCallback() {
    if (this.isConnected) void this.render();
  }

  private async render() {
    const tex = this.dataset.tex ?? "";
    const display = this.dataset.display === "1";
    if (this.done === tex + display) return;
    this.done = tex + display;
    const root = this.shadowRoot ?? this.attachShadow({ mode: "open" });
    try {
      const katex = (await import("katex")).default;
      const mathml = katex.renderToString(tex, { output: "mathml", displayMode: display, throwOnError: false, trust: false });
      root.innerHTML = `<style>:host{display:${display ? "block" : "inline"};${display ? "margin:.6em 0;overflow-x:auto;" : ""}}math{font-size:1.1em}</style>${mathml}`;
    } catch {
      root.innerHTML = `<code>${tex.replace(/</g, "&lt;")}</code>`;
    }
  }
}
customElements.get("phx-math") || customElements.define("phx-math", PhxMath);

let mermaidSeq = 0;

/** ```mermaid: diagrams, rendered strictly (no scripts, no clicks) and loaded on demand. */
export class PhxMermaid extends HTMLElement {
  private done = "";
  connectedCallback() {
    void this.render();
  }

  static get observedAttributes() {
    return ["data-src"];
  }

  attributeChangedCallback() {
    if (this.isConnected) void this.render();
  }

  private async render() {
    // The source lives in the <pre> fallback: sanitizers drop attributes that
    // contain "-->", which is every Mermaid arrow.
    const src = this.dataset.src ?? this.querySelector("pre")?.textContent ?? "";
    if (!src.trim()) return;
    const dark = matchMedia("(prefers-color-scheme: dark)").matches && document.documentElement.dataset.theme !== "light" || document.documentElement.dataset.theme === "dark";
    const key = src + dark;
    if (this.done === key) return;
    this.done = key;
    const root = this.shadowRoot ?? this.attachShadow({ mode: "open" });
    const id = `phx-mermaid-${++mermaidSeq}`;
    try {
      const mermaid = (await import("mermaid")).default;
      // suppressErrorRendering: on a syntax error Mermaid would otherwise
      // draw its error picture at the end of the page body.
      mermaid.initialize({ startOnLoad: false, securityLevel: "strict", theme: dark ? "dark" : "neutral", fontFamily: "inherit", suppressErrorRendering: true });
      const { svg } = await mermaid.render(id, src);
      root.innerHTML = `<style>:host{display:block;margin:.8em 0;overflow-x:auto}svg{max-width:100%;height:auto}</style>${svg}`;
    } catch (e) {
      const msg = (e instanceof Error ? e.message : String(e)).replace(/</g, "&lt;");
      root.innerHTML = `<style>:host{display:block;margin:.8em 0}.err{border:1px solid var(--line);border-left:3px solid var(--bad);border-radius:8px;padding:8px 12px;background:var(--surface)}
        b{display:block;font-size:13px;color:var(--bad);margin-bottom:4px}pre{margin:0;white-space:pre-wrap;font:12px/1.45 var(--font-mono);color:var(--muted)}</style>
        <div class="err" role="alert"><b>${t("block.mermaid_error").replace(/</g, "&lt;")}</b><pre>${msg}</pre></div>`;
    } finally {
      // Mermaid measures in temporary elements in <body>; remove what is left.
      for (const sel of [`#${id}`, `#d${id}`, `#i${id}`]) document.querySelectorAll(sel).forEach((n) => { if (!this.shadowRoot?.contains(n)) n.remove(); });
    }
  }
}
customElements.get("phx-mermaid") || customElements.define("phx-mermaid", PhxMermaid);

/** Turns a link into an embeddable player URL for known providers. */
export function embedFor(raw: string): { src?: string; kind: "video" | "card" | "frame"; title: string } {
  let u: URL;
  try {
    u = new URL(raw.trim());
  } catch {
    return { kind: "card", title: raw };
  }
  if (u.protocol !== "https:") return { kind: "card", title: raw };
  const host = u.hostname.replace(/^www\./, "");
  const yt = host === "youtu.be" ? u.pathname.slice(1)
    : host.endsWith("youtube.com") ? (u.searchParams.get("v") ?? u.pathname.match(/^\/(?:shorts|embed|live)\/([\w-]+)/)?.[1] ?? "") : "";
  if (yt && /^[\w-]{6,20}$/.test(yt)) return { src: `https://www.youtube-nocookie.com/embed/${yt}`, kind: "video", title: "YouTube" };
  const vimeo = host === "vimeo.com" ? u.pathname.match(/^\/(\d+)/)?.[1] : undefined;
  if (vimeo) return { src: `https://player.vimeo.com/video/${vimeo}?dnt=1`, kind: "video", title: "Vimeo" };
  const loom = host === "loom.com" ? u.pathname.match(/^\/share\/([\w-]+)/)?.[1] : undefined;
  if (loom) return { src: `https://www.loom.com/embed/${loom}`, kind: "video", title: "Loom" };
  if (host === "github.com" || host === "gist.github.com") return { kind: "card", title: u.pathname.split("/").filter(Boolean).slice(0, 4).join(" / ") || "GitHub" };
  return { kind: "card", title: host };
}

const EMBED_OK = "phx.embed.allow";
const embedAllowed = (): string[] => {
  try {
    const v = JSON.parse(localStorage.getItem(EMBED_OK) ?? "[]");
    return Array.isArray(v) ? v.filter((x) => typeof x === "string") : [];
  } catch {
    return [];
  }
};

/** ```embed (known players, other links as cards) and ```iframe (any https
 * page in a sandbox). Content from other sites loads only after a click,
 * because the provider then sees the reader's IP address; a provider can be
 * allowed for good in this browser (like map tiles). */
export class PhxEmbed extends LitElement {
  static properties = { url: { attribute: "data-url" }, kind: { attribute: "data-kind" }, on: { state: true } };
  static styles = [base, css`
    :host { display: block; margin: .8em 0; }
    .frame { position: relative; width: 100%; aspect-ratio: 16 / 9; border-radius: 10px; overflow: hidden; border: 1px solid var(--line); background: var(--surface2); }
    .frame.page { aspect-ratio: auto; height: 480px; resize: vertical; }
    iframe { position: absolute; inset: 0; width: 100%; height: 100%; border: 0; }
    .consent { position: absolute; inset: 0; display: grid; place-content: center; justify-items: center; gap: 10px; padding: 16px; text-align: center; font-size: 14px; }
    .consent p { margin: 0; max-width: 46ch; color: var(--muted); }
    .consent .row { display: flex; gap: 10px; align-items: center; flex-wrap: wrap; justify-content: center; }
    .consent label.always { display: inline-flex; flex-direction: row; gap: 6px; align-items: center; font-size: 13px; color: var(--muted); width: auto; }
    .consent label.always input { width: auto; margin: 0; }
    a.card { display: flex; gap: 10px; align-items: center; padding: 10px 14px; border: 1px solid var(--line); border-radius: 10px; background: var(--surface);
      color: var(--fg); text-decoration: none; }
    @media (hover: hover) and (pointer: fine) { a.card:hover { border-color: var(--muted); } }
    a.card .u { color: var(--muted); font-size: 12.5px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    a.card > span { min-width: 0; display: flex; flex-direction: column; }
  `];
  url = "";
  kind = "embed";
  on = false;

  private load(provider: string, e: Event) {
    const always = (e.target as HTMLElement).closest(".consent")?.querySelector<HTMLInputElement>("input[type=checkbox]")?.checked;
    if (always) {
      try {
        localStorage.setItem(EMBED_OK, JSON.stringify([...new Set([...embedAllowed(), provider])]));
      } catch { /* only this time then */ }
    }
    this.on = true;
  }

  /** Placeholder until the reader agrees to load content from provider. */
  private consent(provider: string, href: string, page: boolean) {
    return html`<div class="frame ${page ? "page" : ""}"><div class="consent">
      ${icon(page ? "Frame" : "SquarePlay", 28)}
      <b>${t("embed.title", { provider })}</b>
      <p>${t("embed.ip", { provider })}</p>
      <div class="row"><button @click=${(e: Event) => this.load(provider, e)}>${t("embed.load")}</button>
        <a href=${href} target="_blank" rel="noopener external">${t("embed.open")}</a></div>
      <label class="always"><input type="checkbox"> ${t("embed.always", { provider })}</label>
    </div></div>`;
  }

  render() {
    let u: URL | undefined;
    try {
      u = new URL(this.url.trim());
    } catch {
      /* shown as text */
    }
    if (!u || u.protocol !== "https:") return html`<p class="muted">${this.url}</p>`;
    if (this.kind === "iframe") {
      // Same-site pages must not run scripts with our origin.
      const same = u.host === location.host;
      if (!same && !this.on && !embedAllowed().includes(u.host)) return this.consent(u.host, u.href, true);
      return html`<div class="frame page"><iframe src=${u.href} title=${u.host} loading="lazy" referrerpolicy="no-referrer"
        sandbox=${same ? "allow-popups" : "allow-scripts allow-same-origin allow-popups allow-forms"}></iframe></div>`;
    }
    const e = embedFor(u.href);
    if (e.kind === "video" && e.src) {
      if (!this.on && !embedAllowed().includes(e.title)) return this.consent(e.title, u.href, false);
      return html`<div class="frame"><iframe src=${e.src} title=${e.title} loading="lazy" referrerpolicy="strict-origin-when-cross-origin"
        allow="encrypted-media; picture-in-picture; fullscreen" allowfullscreen sandbox="allow-scripts allow-same-origin allow-popups allow-presentation"></iframe></div>`;
    }
    const gh = u.hostname.endsWith("github.com");
    return html`<a class="card" href=${u.href} target="_blank" rel="noopener external">${icon(gh ? "GitBranch" : "ExternalLink", 18)}
      <span><strong>${e.title}</strong><span class="u">${u.href}</span></span></a>`;
  }
}
customElements.get("phx-embed") || customElements.define("phx-embed", PhxEmbed);

/** <time datetime> as date, time, date and time, or a live countdown. */
export class PhxTime extends LitElement {
  static properties = { datetime: {}, kind: { attribute: "data-kind" }, now: { state: true } };
  static styles = css`:host { display: inline; } .t { background: var(--surface2); border-radius: 5px; padding: 0 5px; font-variant-numeric: tabular-nums; white-space: nowrap; }`;
  datetime = "";
  kind = "date";
  now = Date.now();
  private timer?: number;

  connectedCallback() {
    super.connectedCallback();
    if (this.kind === "countdown") this.timer = window.setInterval(() => (this.now = Date.now()), 1000);
  }

  disconnectedCallback() {
    super.disconnectedCallback();
    window.clearInterval(this.timer);
  }

  protected updated(changed: PropertyValues) {
    if (changed.has("kind") && this.kind === "countdown" && !this.timer) this.timer = window.setInterval(() => (this.now = Date.now()), 1000);
  }

  render() {
    const dateOnly = /^\d{4}-\d{2}-\d{2}$/.test(this.datetime);
    const d = new Date(dateOnly ? `${this.datetime}T00:00:00` : this.datetime);
    if (isNaN(d.getTime())) return html`<slot></slot>`;
    const loc = locale() === "de" ? "de-DE" : "en-GB";
    if (this.kind === "countdown") {
      let s = Math.round((d.getTime() - this.now) / 1000);
      if (s <= 0) return html`<span class="t" title=${d.toLocaleString(loc)}>${t("block.countdown_over")}</span>`;
      const days = Math.floor(s / 86400); s -= days * 86400;
      const h = Math.floor(s / 3600); s -= h * 3600;
      const m = Math.floor(s / 60); s -= m * 60;
      const parts = days ? t("block.countdown_days", { d: days, h, m }) : t("block.countdown_hms", { h, m: String(m).padStart(2, "0"), s: String(s).padStart(2, "0") });
      return html`<span class="t" title=${d.toLocaleString(loc)} role="timer">${icon("Hourglass", 12)} ${parts}</span>`;
    }
    const text = this.kind === "time" ? d.toLocaleTimeString(loc, { hour: "2-digit", minute: "2-digit" })
      : this.kind === "datetime" ? d.toLocaleString(loc, { dateStyle: "medium", timeStyle: "short" })
      : d.toLocaleDateString(loc, { dateStyle: "medium" });
    return html`<time class="t" datetime=${this.datetime}>${text}</time>`;
  }
}
customElements.get("phx-time") || customElements.define("phx-time", PhxTime);

/** ```board: a board of the project, live and usable inside a page. */
export class PhxBoardEmbed extends LitElement {
  static properties = { board: { attribute: "data-board" }, project: { attribute: "data-project" }, doc: { state: true }, missing: { state: true } };
  static styles = [base, css`
    :host { display: block; margin: 1em 0; padding: 10px 12px 4px; border: 1px solid var(--line); border-radius: 12px; background: var(--surface); }
    .head { display: flex; gap: 8px; align-items: center; font-weight: 600; }
    .head a { color: var(--fg); text-decoration: none; }
  `];
  board = "";
  project = "";
  doc?: Doc;
  missing = false;
  private loaded = "";

  async willUpdate() {
    const key = `${this.project}/${this.board}`;
    if (!this.project || !this.board || key === this.loaded) return;
    this.loaded = key;
    await import("./board");
    await this.load();
  }

  /** Loads the board (again when someone changed its columns). */
  private async load() {
    try {
      const d = await api<Doc>("GET", `/projects/${this.project}/documents/${encodeURIComponent(this.board.trim())}`);
      this.doc = d.type === "board" ? d : undefined;
      this.missing = d.type !== "board";
    } catch {
      this.missing = true;
    }
  }

  render() {
    if (this.missing) return html`<p class="muted">${icon("SquareKanban", 14)} ${t("block.board_missing", { board: this.board })}</p>`;
    if (!this.doc) return html`<p class="muted">${t("app.loading")}</p>`;
    return html`<div class="head">${icon("SquareKanban", 15)} <a href="/p/${this.project}/d/${this.doc.slug}">${this.doc.title}</a></div>
      <phx-board .project=${this.project} .doc=${this.doc} @changed=${() => this.load()}></phx-board>`;
  }
}
customElements.get("phx-board-embed") || customElements.define("phx-board-embed", PhxBoardEmbed);

/** ```whiteboard: a whiteboard of the project, drawn read-only, with a link to open it. */
export class PhxWhiteboardEmbed extends LitElement {
  static properties = { board: { attribute: "data-board" }, project: { attribute: "data-project" }, doc: { state: true }, missing: { state: true } };
  static styles = [base, css`
    :host { display: block; margin: 1em 0; border: 1px solid var(--line); border-radius: 12px; background: var(--surface); overflow: hidden; }
    .head { display: flex; gap: 8px; align-items: center; padding: 8px 12px; border-bottom: 1px solid var(--line); font-weight: 600; }
    .head a { color: var(--fg); text-decoration: none; margin-right: auto; }
    .pic { padding: 0; }
  `];
  board = "";
  project = "";
  doc?: Doc;
  missing = false;
  private loaded = "";

  async willUpdate() {
    const key = `${this.project}/${this.board}`;
    if (!this.project || !this.board || key === this.loaded) return;
    this.loaded = key;
    try {
      const d = await api<Doc>("GET", `/projects/${this.project}/documents/${encodeURIComponent(this.board.trim())}`);
      if (d.type !== "whiteboard") throw new Error("not a whiteboard");
      // The same drawing code as the editor, so the page shows what people drew.
      await import("./whiteboard");
      this.doc = d;
    } catch {
      this.missing = true;
    }
  }

  render() {
    if (this.missing) return html`<p class="muted" style="padding:8px 12px">${icon("Presentation", 14)} ${t("block.whiteboard_missing", { board: this.board })}</p>`;
    const href = `/p/${this.project}/d/${encodeURIComponent(this.board.trim())}`;
    return html`<div class="head">${icon("Presentation", 15)} <a href=${href}>${this.doc?.title ?? this.board}</a><a class="btn ghost" style="margin:0" href=${href}>${t("block.whiteboard_open")}</a></div>
      <div class="pic">${this.doc ? html`<phx-whiteboard still .project=${this.project} .doc=${this.doc}></phx-whiteboard>` : html`<span class="muted" style="display:block;padding:8px 12px">${t("app.loading")}</span>`}</div>`;
  }
}
customElements.get("phx-whiteboard-embed") || customElements.define("phx-whiteboard-embed", PhxWhiteboardEmbed);

/** Styles for callouts and colors inside rendered pages. */
/** ```app <slug>: an app inside a page, as tall as its content. */
export class PhxAppEmbed extends LitElement {
  static properties = { app: { attribute: "data-app" }, project: { attribute: "data-project" }, info: { state: true }, title: { state: true }, missing: { state: true } };
  static styles = [base, css`
    :host { display: block; margin: 1em 0; border: 1px solid var(--line); border-radius: 12px; background: var(--surface); overflow: hidden; }
    .head { display: flex; gap: 8px; align-items: center; padding: 8px 12px; border-bottom: 1px solid var(--line); font-weight: 600; }
    .head a { color: var(--fg); text-decoration: none; margin-right: auto; }
  `];
  app = "";
  project = "";
  info?: import("./apps").AppFrameInfo;
  title = "";
  missing = false;
  private loaded = "";

  async willUpdate() {
    const key = `${this.project}/${this.app}`;
    if (!this.project || !this.app || key === this.loaded) return;
    this.loaded = key;
    try {
      const ref = encodeURIComponent(this.app.trim());
      const d = await api<Doc>("GET", `/projects/${this.project}/documents/${ref}`);
      if (d.type !== "app") throw new Error("not an app");
      await import("./apps");
      this.title = d.title;
      this.info = await api("POST", `/projects/${this.project}/apps/${d.id}/open`);
      this.app = d.id;
    } catch {
      this.missing = true;
    }
  }

  render() {
    if (this.missing) return html`<p class="muted" style="padding:8px 12px">${icon("AppWindow", 14)} ${t("block.app_missing", { app: this.app })}</p>`;
    const href = `/p/${this.project}/d/${encodeURIComponent(this.app.trim())}`;
    return html`<div class="head">${icon("AppWindow", 15)} <a href=${href}>${this.title || this.app}</a><a class="btn ghost" style="margin:0" href=${href}>${t("block.whiteboard_open")}</a></div>
      ${this.info ? html`<phx-app-frame auto-height .project=${this.project} .doc=${this.app} .info=${this.info}></phx-app-frame>` : html`<span class="muted" style="display:block;padding:8px 12px">${t("app.loading")}</span>`}`;
  }
}
customElements.get("phx-app-embed") || customElements.define("phx-app-embed", PhxAppEmbed);

export const blockStyles = `
  phx-include { display: block; border-left: 3px solid var(--line); padding-left: 14px; margin: 1em 0; }
  phx-include .include-src { font-size: 12.5px; margin: .3em 0 0; }
  .callout { display: flex; gap: 10px; padding: 10px 14px; border-radius: 10px; margin: .8em 0; background: var(--tb-gray); }
  .callout .ci { flex: none; padding-top: 2px; color: var(--tc-gray); }
  .callout .cc { min-width: 0; flex: 1; } .callout .cc > :first-child { margin-top: 0; } .callout .cc > :last-child { margin-bottom: 0; }
  .callout .ct { font-weight: 600; margin-bottom: .3em; }
  details.callout.fold { display: block; padding: 0; }
  details.callout.fold > summary { display: flex; gap: 10px; align-items: center; padding: 10px 14px; cursor: pointer; list-style: none; border-radius: 10px; }
  details.callout.fold > summary::-webkit-details-marker { display: none; }
  details.callout.fold > summary::after { content: ""; margin-left: auto; width: 8px; height: 8px; border-right: 2px solid currentColor; border-bottom: 2px solid currentColor; transform: rotate(-45deg); transition: transform var(--dur-fast, 150ms) var(--ease-out, ease-out); opacity: .6; }
  details.callout.fold[open] > summary::after { transform: rotate(45deg); }
  details.callout.fold > summary .ct { margin: 0; }
  details.callout.fold > summary .ci { padding-top: 0; }
  details.callout.fold > summary:focus-visible { outline: 2px solid var(--accent); outline-offset: -2px; }
  details.callout.fold > .cc { padding: 0 14px 12px 42px; }
  @media (prefers-reduced-motion: reduce) { details.callout.fold > summary::after { transition: none; } }
  phx-tabs { display: block; }
  phx-tabs > section.tab { padding: 2px 0; }
  phx-tabs:defined > section.tab > .tab-h { display: none; }
  phx-tabs > section.tab > .tab-h { margin: .8em 0 .3em; font-size: 1em; }
  phx-tabs > section.tab:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; border-radius: 4px; }
  @media print { phx-tabs > section.tab { display: block !important; } phx-tabs > section.tab > .tab-h { display: block !important; } }
  phx-steps { display: block; }
  phx-steps .step-progress { display: flex; align-items: center; gap: 10px; font-size: 12.5px; color: var(--muted); margin-bottom: .4em; }
  phx-steps .step-progress .bar { flex: 1; max-width: 220px; height: 6px; border-radius: 3px; background: color-mix(in srgb, var(--fg) 10%, transparent); overflow: hidden; }
  phx-steps .step-progress .bar > span { display: block; height: 100%; background: var(--good); transition: width var(--dur, 200ms) var(--ease-out, ease-out); }
  phx-steps ol.steps { list-style: none; padding-left: 0; counter-reset: step; }
  phx-steps ol.steps > li { position: relative; padding-left: 36px; margin: .5em 0; counter-increment: step; }
  phx-steps ol.steps > li > .step-check { position: absolute; left: 0; top: 0; width: 24px; height: 24px; border-radius: 50%; border: 2px solid var(--tc-blue); background: var(--surface); cursor: pointer; padding: 0; display: grid; place-items: center; font: 600 11px var(--font-body); color: var(--tc-blue); }
  phx-steps ol.steps > li > .step-check::before { content: counter(step); }
  phx-steps ol.steps > li.done > .step-check { background: var(--good); border-color: var(--good); color: var(--on-color, #fff); }
  phx-steps ol.steps > li.done > .step-check::before { content: "✓"; }
  phx-steps ol.steps > li.done { color: var(--muted); }
  phx-steps ol.steps > li > .step-check:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
  @media (prefers-reduced-motion: reduce) { phx-steps .step-progress .bar > span { transition: none; } }
  ${["gray", "red", "orange", "yellow", "green", "blue", "purple", "pink"].map((c) => `
  .callout.cb-${c} { background: var(--tb-${c}); } .callout.cb-${c} .ci { color: var(--tc-${c}); }
  .ci.ic-${c} { color: var(--tc-${c}) !important; }
  .tc-${c} { color: var(--tc-${c}); } .tb-${c} { background: var(--tb-${c}); border-radius: 3px; padding: 0 2px; }`).join("")}
`;

interface SchemaBlock { documentId: string; slug: string; title: string; sql: string }
const schemas = new Map<string, { at: number; p: Promise<SchemaBlock[]> }>();

/** All dbmodel blocks of a project (cached for a minute per page view). */
function projectSchema(project: string): Promise<SchemaBlock[]> {
  const hit = schemas.get(project);
  if (hit && Date.now() - hit.at < 60_000) return hit.p;
  const p = api<SchemaBlock[]>("GET", `/projects/${project}/dbschema`).catch(() => []);
  schemas.set(project, { at: Date.now(), p });
  return p;
}

/** ```dbmodel: SQL CREATE TABLE statements shown as an ER diagram with a table list. */
export class PhxDbModel extends HTMLElement {
  private done = "";
  connectedCallback() {
    void this.render();
  }

  static get observedAttributes() {
    return ["data-src"];
  }

  attributeChangedCallback() {
    if (this.isConnected) void this.render();
  }

  private async render() {
    let src = this.dataset.src ?? this.querySelector("pre")?.textContent ?? "";
    if (src === this.done) return;
    this.done = src;
    const { parseDDL, toMermaid, shownTables, withSchema, repoSource } = await import("../erd");
    // "from owner/repo path": the schema the repository's migrations build.
    const fromRepo = repoSource(src);
    let repoNote = "";
    if (fromRepo && this.dataset.project) {
      const escText = (x: string) => x.replace(/[&<>"]/g, (c) => `&#${c.charCodeAt(0)};`);
      try {
        const r = await api<{ sql: string }>("GET", `/projects/${this.dataset.project}/code/dbmodel?repo=${encodeURIComponent(fromRepo.repo)}&path=${encodeURIComponent(fromRepo.path)}`);
        src = r.sql + "\n" + src.replace(/^\s*from\s+\S+\s+\S+\s*$/im, "");
        repoNote = `<p class="note">${escText(t("block.dbmodel_from_repo", { repo: fromRepo.repo, path: fromRepo.path }))}</p>`;
      } catch (e) {
        const root = this.shadowRoot ?? this.attachShadow({ mode: "open" });
        root.innerHTML = `<p style="color:var(--bad)">${escText(e instanceof ApiError ? e.text : t("error.unknown"))}</p>`;
        return;
      }
    }
    const own = parseDDL(src);
    const shown = shownTables(src);
    // References to tables of other blocks (and "-- tables:") use the
    // project's schema: all dbmodel blocks of its pages.
    const project = this.dataset.project ?? "";
    const needSchema = project && (shown.length > 0 || own.some((tb) => tb.columns.some((c) => c.ref && !own.some((o) => o.name.toLowerCase() === c.ref!.table.toLowerCase()))));
    const schema = needSchema ? await projectSchema(project) : [];
    const { tables, refs, missing } = withSchema(own, shown, schema.flatMap((b) => parseDDL(b.sql)));
    const root = this.shadowRoot ?? this.attachShadow({ mode: "open" });
    const esc = (s: string) => s.replace(/[&<>"]/g, (c) => `&#${c.charCodeAt(0)};`);
    if (!tables.length) {
      root.innerHTML = `<style>:host{display:block}pre{white-space:pre-wrap;font-size:12.5px}</style><p>${esc(t("block.dbmodel_empty"))}</p><pre>${esc(src)}</pre>`;
      return;
    }
    const rows = tables.map((tb) => `<details><summary><b>${esc(tb.name)}</b> <span class="m">${tables.length ? tb.columns.length : 0} ${esc(t("block.dbmodel_columns"))}</span></summary>
      <table><thead><tr><th>${esc(t("block.dbmodel_column"))}</th><th>${esc(t("block.dbmodel_type"))}</th><th></th></tr></thead><tbody>${tb.columns.map((c) =>
        `<tr><td>${esc(c.name)}</td><td class="m">${esc(c.type)}${c.notNull ? " NOT NULL" : ""}</td><td>${c.pk ? "PK " : ""}${c.ref ? `→ ${esc(c.ref.table)}.${esc(c.ref.column)}` : ""}</td></tr>`).join("")}</tbody></table></details>`).join("");
    const from = (name: string) => schema.find((b) => parseDDL(b.sql).some((tb) => tb.name.toLowerCase() === name.toLowerCase()));
    const link = (name: string) => {
      const b = from(name);
      return b ? `<a href="/p/${encodeURIComponent(project)}/d/${encodeURIComponent(b.documentId)}">${esc(name)}</a>` : esc(name);
    };
    const notes = [
      repoNote,
      refs.length ? `<p class="note">${esc(t("block.dbmodel_refs"))} ${refs.map((r) => link(r.name)).join(", ")}</p>` : "",
      missing.length ? `<p class="note warn">${esc(t("block.dbmodel_missing", { tables: missing.join(", ") }))}</p>` : "",
    ].join("");
    root.innerHTML = `<style>:host{display:block;margin:.8em 0}details{border-top:1px solid var(--line);padding:6px 0}summary{cursor:pointer}
      .m{color:var(--muted);font-family:var(--font-mono);font-size:12px}table{border-collapse:collapse;margin:6px 0;font-size:13px}td,th{border:1px solid var(--line);padding:3px 8px;text-align:left}
      .note{color:var(--muted);font-size:13px;margin:4px 0}.note a{color:var(--accent)}.warn{color:var(--bad)}</style>
      ${notes}${rows}`;
    // The source must be set before the element connects (else Mermaid renders "").
    const diagram = document.createElement("phx-mermaid");
    diagram.setAttribute("data-src", toMermaid([...tables, ...refs]));
    root.insertBefore(diagram, root.querySelector("details"));
  }
}
customElements.get("phx-dbmodel") || customElements.define("phx-dbmodel", PhxDbModel);

interface Hop { slug: string; name: string; kind: string; status: string; reason: string; params?: Record<string, unknown> }
interface StatusCheck { id: string; name: string; enabled: boolean; lastStatus: string; lastRun: string | null; documentSlug: string; lastDetail: string }

/** ```status: live tiles for entries (@slug lines) and checks (check: name, or "checks" for all). */
export class PhxStatus extends LitElement {
  static properties = { src: { attribute: "data-src" }, project: { attribute: "data-project" }, board: { state: true } };
  static styles = [base, css`
    :host { display: block; margin: 1em 0; }
    .grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(180px, 1fr)); gap: 8px; }
    a.tile { display: grid; gap: 2px; padding: 10px 12px; border: 1px solid var(--line); border-radius: 10px; background: var(--surface); color: var(--fg); text-decoration: none;
      border-left: 4px solid var(--muted); }
    a.tile.ok { border-left-color: var(--good); } a.tile.warn { border-left-color: var(--signal); } a.tile.down, a.tile.fail { border-left-color: var(--bad); }
    .n { font-weight: 600; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .r { font-size: 12.5px; color: var(--muted); }
    .foot { font-size: 12px; color: var(--muted); margin-top: 6px; }
  `];
  src = "";
  project = "";
  board?: { entries: Hop[]; checks: StatusCheck[] };
  private timer?: number;
  private loaded = "";

  connectedCallback() {
    super.connectedCallback();
    // Dashboards stay current while open.
    this.timer = window.setInterval(() => document.visibilityState === "visible" && void this.load(), 60_000);
  }

  disconnectedCallback() {
    super.disconnectedCallback();
    window.clearInterval(this.timer);
  }

  willUpdate() {
    const src = this.src || this.querySelector("pre")?.textContent || "";
    if (this.project && src !== this.loaded) {
      this.loaded = src;
      void this.load();
    }
  }

  private async load() {
    const q = new URLSearchParams();
    for (const line of this.loaded.split("\n").map((l) => l.trim()).filter(Boolean)) {
      const check = /^checks?\s*:?\s*(.*)$/i.exec(line);
      if (check) q.append("check", check[1].trim() || "*");
      else for (const m of line.matchAll(/@?([a-z0-9][a-z0-9-]*)/gi)) q.append("e", m[1]);
    }
    this.board = await api<{ entries: Hop[]; checks: StatusCheck[] }>("GET", `/projects/${this.project}/status?${q}`).catch(() => this.board);
  }

  render() {
    const b = this.board;
    if (!b) return html`<p class="muted">${t("app.loading")}</p>`;
    if (!b.entries.length && !b.checks.length) return html`<p class="muted">${t("block.status_empty")}</p>`;
    return html`<div class="grid">
      ${b.entries.map((h) => html`<a class="tile ${h.status}" href="/p/${this.project}/e/${h.slug}">
        <span class="n">${h.name}</span><span class="r">${t(`diag.status.${h.status}` as any)} · ${t(h.reason as any, h.params as any)}</span></a>`)}
      ${b.checks.map((c) => html`<a class="tile ${c.enabled ? c.lastStatus : ""}" href="/p/${this.project}/d/${c.documentSlug}">
        <span class="n">${icon("Activity", 13)} ${c.name}</span><span class="r">${t(`check.state.${c.enabled ? c.lastStatus : "paused"}` as any)}${c.lastDetail ? ` · ${c.lastDetail}` : ""}</span></a>`)}
    </div><p class="foot">${t("block.status_live")}</p>`;
  }
}
customElements.get("phx-status") || customElements.define("phx-status", PhxStatus);

interface EntityDetail { entity: { slug: string; name: string; kind: string; attrs: Record<string, unknown> }; edges: { from: string; rel: string; to: string; fromName: string; toName: string; fromSlug?: string; toSlug?: string }[] }

/** ```entity: a card for one entry (component pages): attributes and relations, live. */
export class PhxEntityCard extends LitElement {
  static properties = { src: { attribute: "data-src" }, project: { attribute: "data-project" }, d: { state: true }, missing: { state: true } };
  static styles = [base, css`
    :host { display: block; margin: 1em 0; padding: 12px 14px; border: 1px solid var(--line); border-radius: 12px; background: var(--surface); }
    .head { display: flex; gap: 8px; align-items: baseline; flex-wrap: wrap; }
    .head a { font-weight: 700; font-size: 16px; color: var(--fg); text-decoration: none; }
    dl { display: grid; grid-template-columns: max-content 1fr; gap: 2px 14px; margin: 8px 0 0; font-size: 13.5px; }
    dt { color: var(--muted); } dd { margin: 0; overflow-wrap: anywhere; }
    ul { margin: 8px 0 0; padding-left: 18px; font-size: 13.5px; }
  `];
  src = "";
  project = "";
  d?: EntityDetail;
  missing = false;
  private loaded = "";

  async willUpdate() {
    const slug = (this.src || this.querySelector("pre")?.textContent || "").trim().replace(/^@/, "");
    if (!this.project || !slug || slug === this.loaded) return;
    this.loaded = slug;
    try {
      this.d = await api<EntityDetail>("GET", `/projects/${this.project}/entities/${encodeURIComponent(slug)}`);
      this.missing = false;
    } catch {
      this.missing = true;
    }
  }

  render() {
    if (this.missing) return html`<p class="muted">${t("block.entity_missing", { entity: this.loaded })}</p>`;
    const d = this.d;
    if (!d) return html`<p class="muted">${t("app.loading")}</p>`;
    const e = d.entity;
    const attrs = Object.entries(e.attrs ?? {}).filter(([, v]) => v !== "" && v !== null && typeof v !== "object").slice(0, 12);
    const self = (s?: string, n?: string) => s === e.slug || n === e.name;
    return html`<div class="head">${icon("Box", 15)}<a href="/p/${this.project}/e/${e.slug}">${e.name}</a><span class="chip">${e.kind}</span></div>
      ${attrs.length ? html`<dl>${attrs.map(([k, v]) => html`<dt>${k}</dt><dd>${String(v)}</dd>`)}</dl>` : ""}
      ${d.edges.length ? html`<ul>${d.edges.slice(0, 20).map((x) => self(x.fromSlug, x.fromName)
        ? html`<li>${x.rel} → <a href="/p/${this.project}/e/${x.toSlug ?? x.to}">${x.toName}</a></li>`
        : html`<li><a href="/p/${this.project}/e/${x.fromSlug ?? x.from}">${x.fromName}</a> → ${x.rel}</li>`)}</ul>` : ""}`;
  }
}
customElements.get("phx-entity-card") || customElements.define("phx-entity-card", PhxEntityCard);

/** ```include: the content of another page, shown in place (reusable blocks).
 * Rendered in the light DOM so the page's text styles apply; includes inside
 * an included page show as links only, so pages cannot include each other
 * endlessly. */
export class PhxInclude extends HTMLElement {
  private loaded = "";
  connectedCallback() {
    void this.render();
  }

  private async render() {
    const project = this.dataset.project ?? "";
    const slug = (this.dataset.page ?? "").trim();
    const key = `${project}/${slug}`;
    if (!project || !slug || key === this.loaded) return;
    this.loaded = key;
    const link = (title: string) => {
      const a = document.createElement("a");
      a.href = `/p/${project}/d/${encodeURIComponent(slug)}`;
      a.textContent = title;
      return a;
    };
    if (this.dataset.nested) {
      this.replaceChildren(link(slug));
      return;
    }
    try {
      const d = await api<Doc>("GET", `/projects/${project}/documents/${encodeURIComponent(slug)}`);
      if (d.type !== "markdown") throw new Error("not a page");
      const { renderMarkdown, withoutTitle } = await import("../md");
      const body = renderMarkdown(withoutTitle(d.content?.markdown ?? "", d.title), project, d.id).replace(/<phx-include /g, '<phx-include data-nested="1" ');
      const box = document.createElement("div");
      box.className = "include-body";
      box.innerHTML = body;
      const src = document.createElement("p");
      src.className = "include-src muted";
      src.append(t("block.include_from") + " ", link(d.title));
      this.replaceChildren(box, src);
    } catch {
      const p = document.createElement("p");
      p.className = "muted";
      p.textContent = t("block.include_missing", { page: slug });
      this.replaceChildren(p);
    }
  }
}
customElements.get("phx-include") || customElements.define("phx-include", PhxInclude);

interface QueryResult { kind: string; columns: string[]; rows: { slug: string; name: string; attrs: Record<string, unknown> }[]; total: number }

/** ```query: the result of a saved query as a live table. */
export class PhxQueryBlock extends LitElement {
  static properties = { query: { attribute: "data-query" }, project: { attribute: "data-project" }, r: { state: true }, missing: { state: true } };
  static styles = [base, css`
    :host { display: block; margin: 1em 0; }
    .wrap { overflow: auto; border: 1px solid var(--line); border-radius: 12px; }
    table { border-collapse: collapse; width: 100%; font-size: 13.5px; }
    th, td { border-bottom: 1px solid var(--line); padding: 4px 10px; text-align: left; white-space: nowrap; }
    th { background: var(--surface); }
    .foot { font-size: 12.5px; margin-top: 4px; }
  `];
  query = "";
  project = "";
  r?: QueryResult;
  missing = false;
  private loaded = "";

  async willUpdate() {
    const q = this.query.trim();
    if (!this.project || !q || q === this.loaded) return;
    this.loaded = q;
    try {
      this.r = await api<QueryResult>("GET", `/projects/${this.project}/queries/${encodeURIComponent(q)}/run`);
    } catch {
      this.missing = true;
    }
  }

  render() {
    if (this.missing) return html`<p class="muted">${t("block.query_missing", { query: this.query })}</p>`;
    const r = this.r;
    if (!r) return html`<p class="muted">${t("app.loading")}</p>`;
    const cols = r.columns.slice(0, 8);
    const cell = (v: unknown) => (v === undefined || v === null ? "" : typeof v === "object" ? JSON.stringify(v) : String(v));
    return html`<div class="wrap"><table>
      <thead><tr><th>${t("entity.name")}</th>${cols.map((c) => html`<th>${attrLabel({ name: c, type: "text" })}</th>`)}</tr></thead>
      <tbody>${r.rows.slice(0, 200).map((x) => html`<tr><td><a href="/p/${this.project}/e/${x.slug}">${x.name}</a></td>${cols.map((c) => html`<td>${cell(x.attrs[c])}</td>`)}</tr>`)}</tbody>
    </table></div>
    <p class="foot muted">${icon("Table", 12)} ${t("block.query_count", { n: r.total })} · <a href="/p/${this.project}/graph?query=${encodeURIComponent(this.query.trim())}">${t("block.query_open")}</a></p>`;
  }
}
customElements.get("phx-query-block") || customElements.define("phx-query-block", PhxQueryBlock);

const MAP_OK = "phx.map.tiles";
const mapAllowed = () => {
  try {
    return localStorage.getItem(MAP_OK) === "1";
  } catch {
    return false;
  }
};

/** ```map: markers, tracks and GPX files on an OpenStreetMap map. Tiles load
 * only after the person agreed (remembered per browser, like the Locations page). */
export class PhxMapBlock extends LitElement {
  static properties = { src: { attribute: "data-src" }, project: { attribute: "data-project" }, on: { state: true }, note: { state: true } };
  static styles = [base, css`
    :host { display: block; margin: 1em 0; }
    .map { height: 360px; border-radius: 12px; border: 1px solid var(--line); z-index: 0; }
    .consent { border: 1px dashed var(--line); border-radius: 12px; padding: 14px; display: grid; gap: 8px; justify-items: start; font-size: 14px; }
    .trk { stroke: var(--accent); stroke-width: 4px; fill: none; }
    .pin { color: var(--accent); }
    .note { font-size: 12.5px; }
  `];
  src = "";
  project = "";
  on = mapAllowed();
  note = "";
  private map?: import("leaflet").Map;

  disconnectedCallback() {
    super.disconnectedCallback();
    this.map?.remove();
    this.map = undefined;
  }

  private allow() {
    try {
      localStorage.setItem(MAP_OK, "1");
    } catch { /* only for this page then */ }
    this.on = true;
  }

  async updated() {
    if (!this.on || this.map) return;
    const el = this.renderRoot.querySelector<HTMLElement>(".map");
    if (!el) return;
    const [L, { parseMap, gpxPoints }, cssMod] = await Promise.all([import("leaflet"), import("../map-block"), import("leaflet/dist/leaflet.css?inline")]);
    if (!this.renderRoot.querySelector("style[data-leaflet]")) {
      const st = document.createElement("style");
      st.dataset.leaflet = "1";
      st.textContent = cssMod.default;
      this.renderRoot.appendChild(st);
    }
    const spec = parseMap(this.src || this.querySelector("pre")?.textContent || "");
    const map = L.map(el);
    this.map = map;
    L.tileLayer("https://tile.openstreetmap.org/{z}/{x}/{y}.png", { maxZoom: 19, attribution: "© OpenStreetMap" }).addTo(map);
    const pin = L.divIcon({ className: "pin", html: iconSVG("MapPin", 28), iconSize: [28, 28], iconAnchor: [14, 28] });
    const pts: [number, number][] = [];
    for (const m of spec.markers) {
      let { lat, lng, label } = m;
      if ((lat === undefined || lng === undefined) && m.entity) {
        const d = await api<{ entity: { name: string; attrs: Record<string, unknown> } }>("GET", `/projects/${this.project}/entities/${encodeURIComponent(m.entity)}`).catch(() => undefined);
        const a = d?.entity.attrs ?? {};
        if (a.lat == null || a.lng == null || a.lat === "" || a.lng === "") continue;
        lat = Number(a.lat);
        lng = Number(a.lng);
        label ||= d?.entity.name ?? m.entity;
        if (!Number.isFinite(lat) || !Number.isFinite(lng)) continue;
      }
      if (lat === undefined || lng === undefined) continue;
      pts.push([lat, lng]);
      const mk = L.marker([lat, lng], { icon: pin, title: label || m.entity || "", keyboard: true }).addTo(map);
      if (label) mk.bindTooltip(label);
      if (m.entity) mk.on("click", () => location.assign(`/p/${this.project}/e/${m.entity}`));
    }
    const line = (points: [number, number][], label: string) => {
      pts.push(...points);
      const pl = L.polyline(points, { className: "trk" }).addTo(map);
      if (label) pl.bindTooltip(label, { sticky: true });
    };
    for (const tr of spec.tracks) line(tr.points, tr.label);
    for (const g of spec.gpx) {
      const res = await fetch(`/api/v1/files/${g.file}`, { credentials: "same-origin" }).catch(() => undefined);
      if (res?.ok) line(gpxPoints(await res.text()), g.label);
    }
    if (spec.view) map.setView([spec.view.lat, spec.view.lng], spec.view.zoom);
    else if (pts.length > 1) map.fitBounds(L.latLngBounds(pts), { padding: [30, 30] });
    else if (pts.length === 1) map.setView(pts[0], 14);
    else map.setView([51, 10], 4);
    if (spec.errors.length) this.note = t("block.map_errors", { lines: spec.errors.join(", ") });
  }

  render() {
    if (!this.on) return html`<div class="consent"><span>${t("loc.map_consent")}</span><button class="ghost" @click=${this.allow}>${icon("MapPin", 14)} ${t("loc.show_map")}</button></div>`;
    return html`<div class="map" role="region" aria-label=${t("block.map")}></div>${this.note ? html`<p class="note muted">${this.note}</p>` : ""}`;
  }
}
customElements.get("phx-map-block") || customElements.define("phx-map-block", PhxMapBlock);
