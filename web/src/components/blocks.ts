// Elements behind the page building blocks of ADR-0031. They render inside
// pages (doc view, shared view, editor) and only load what they need.

import { LitElement, css, html, type PropertyValues } from "lit";
import { api, type Doc } from "../api";
import { locale, t } from "../i18n";
import { icon } from "../icon";
import { base } from "./shared";

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
    const dark = matchMedia("(prefers-color-scheme: dark)").matches && document.documentElement.dataset.theme !== "light" || document.documentElement.dataset.theme === "dark";
    const key = src + dark;
    if (this.done === key) return;
    this.done = key;
    const root = this.shadowRoot ?? this.attachShadow({ mode: "open" });
    try {
      const mermaid = (await import("mermaid")).default;
      mermaid.initialize({ startOnLoad: false, securityLevel: "strict", theme: dark ? "dark" : "neutral", fontFamily: "inherit" });
      const { svg } = await mermaid.render(`phx-mermaid-${++mermaidSeq}`, src);
      root.innerHTML = `<style>:host{display:block;margin:.8em 0;overflow-x:auto}svg{max-width:100%;height:auto}</style>${svg}`;
    } catch (e) {
      const msg = (e instanceof Error ? e.message : String(e)).replace(/</g, "&lt;");
      root.innerHTML = `<style>:host{display:block}pre{white-space:pre-wrap;font-size:12px;color:var(--bad)}</style><pre>${msg}</pre><pre>${src.replace(/</g, "&lt;")}</pre>`;
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

/** ```embed (known players, other links as cards) and ```iframe (any https page in a sandbox). */
export class PhxEmbed extends LitElement {
  static properties = { url: { attribute: "data-url" }, kind: { attribute: "data-kind" } };
  static styles = [base, css`
    :host { display: block; margin: .8em 0; }
    .frame { position: relative; width: 100%; aspect-ratio: 16 / 9; border-radius: 10px; overflow: hidden; border: 1px solid var(--line); background: var(--surface2); }
    .frame.page { aspect-ratio: auto; height: 480px; resize: vertical; }
    iframe { position: absolute; inset: 0; width: 100%; height: 100%; border: 0; }
    a.card { display: flex; gap: 10px; align-items: center; padding: 10px 14px; border: 1px solid var(--line); border-radius: 10px; background: var(--surface);
      color: var(--fg); text-decoration: none; }
    @media (hover: hover) and (pointer: fine) { a.card:hover { border-color: var(--muted); } }
    a.card .u { color: var(--muted); font-size: 12.5px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    a.card > span { min-width: 0; display: flex; flex-direction: column; }
  `];
  url = "";
  kind = "embed";

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
      return html`<div class="frame page"><iframe src=${u.href} title=${u.host} loading="lazy" referrerpolicy="no-referrer"
        sandbox=${same ? "allow-popups" : "allow-scripts allow-same-origin allow-popups allow-forms"}></iframe></div>`;
    }
    const e = embedFor(u.href);
    if (e.kind === "video" && e.src) {
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

/** Styles for callouts and colors inside rendered pages. */
export const blockStyles = `
  .callout { display: flex; gap: 10px; padding: 10px 14px; border-radius: 10px; margin: .8em 0; background: var(--tb-gray); }
  .callout .ci { flex: none; padding-top: 2px; color: var(--tc-gray); }
  .callout .cc { min-width: 0; flex: 1; } .callout .cc > :first-child { margin-top: 0; } .callout .cc > :last-child { margin-bottom: 0; }
  ${["gray", "red", "orange", "yellow", "green", "blue", "purple", "pink"].map((c) => `
  .callout.cb-${c} { background: var(--tb-${c}); } .callout.cb-${c} .ci { color: var(--tc-${c}); }
  .ci.ic-${c} { color: var(--tc-${c}) !important; }
  .tc-${c} { color: var(--tc-${c}); } .tb-${c} { background: var(--tb-${c}); border-radius: 3px; padding: 0 2px; }`).join("")}
`;
