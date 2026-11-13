import { LitElement, css, html, unsafeCSS } from "lit";
import { unsafeHTML } from "lit/directives/unsafe-html.js";
import { ApiError, api, type Doc } from "../api";
import { applyProjectColor } from "../color";
import { formatDate, t } from "../i18n";
import { icon } from "../icon";
import { renderMarkdown } from "../md";
import { prose } from "./doc-view";
import { base } from "./shared";

type View = { project: { name: string; primaryColor: string }; single: boolean; documents: Doc[] };

/**
 * Turns rendered Markdown into the read-only shared form: files load through
 * the share link, mentions and request blocks become plain content.
 */
export function sharedHTML(md: string, token: string): string {
  const tpl = document.createElement("template");
  tpl.innerHTML = renderMarkdown(md, "");
  for (const img of tpl.content.querySelectorAll("img")) {
    const m = img.getAttribute("src")?.match(/^\/api\/v1\/files\/([A-Za-z0-9]+)(\/.*)?$/);
    if (m) img.setAttribute("src", `/api/v1/shared/${encodeURIComponent(token)}/files/${m[1]}${m[2] ?? ""}`);
  }
  for (const a of tpl.content.querySelectorAll('a[href^="/p/"]')) a.replaceWith(...Array.from(a.childNodes));
  for (const rb of tpl.content.querySelectorAll("phx-request-block")) rb.replaceWith(...Array.from(rb.childNodes));
  return tpl.innerHTML;
}

/** Read-only view of a share link: one page, or the pages of a project. */
export class PhxSharedView extends LitElement {
  static properties = { token: {}, doc: {}, view: { state: true }, current: { state: true }, error: { state: true } };
  static styles = [base, unsafeCSS(prose), css`
    :host { display: block; min-height: 100vh; }
    header { display: flex; align-items: center; gap: 10px; padding: 0 16px; height: var(--header-h); border-bottom: 1px solid var(--line); background: var(--surface); }
    header .pi { width: 22px; height: 22px; border-radius: 6px; background: var(--accent); }
    header b { font-family: var(--font-display); font-size: 17px; }
    header .ro { margin-left: auto; }
    .wrap { display: grid; grid-template-columns: 240px minmax(0, 1fr); max-width: 1180px; margin: 0 auto; min-height: calc(100vh - var(--header-h)); }
    .wrap.single { grid-template-columns: minmax(0, 1fr); max-width: 860px; }
    nav { padding: 18px 10px; border-right: 1px solid var(--line); display: flex; flex-direction: column; gap: 2px; }
    nav a { display: flex; gap: 8px; align-items: center; padding: 6px 8px; border-radius: 6px; color: var(--muted); text-decoration: none; font-size: 14px; }
    nav a.on { background: var(--accent-soft); color: var(--fg); }
    main { padding: 24px 32px 60px; min-width: 0; }
    section h2 { font-size: 15px; text-transform: uppercase; letter-spacing: .06em; color: var(--muted); font-family: var(--font-body); margin: 18px 0 4px; }
    @media (max-width: 760px) { .wrap { grid-template-columns: 1fr; } nav { border-right: 0; border-bottom: 1px solid var(--line); flex-direction: row; overflow-x: auto; } main { padding: 18px 16px 48px; } }
  `];
  token = "";
  doc = "";
  view?: View;
  current?: Doc;
  error = "";
  private loaded = "";

  async willUpdate() {
    const key = `${this.token}/${this.doc}`;
    if (key === this.loaded) return;
    this.loaded = key;
    try {
      if (!this.view) {
        this.view = await api<View>("GET", `/shared/${this.token}`);
        applyProjectColor(document.documentElement, this.view.project.primaryColor);
        document.title = `${this.view.project.name} · Phoedox`;
      }
      const slug = this.doc || this.view.documents[0]?.slug;
      this.current = slug ? await api<Doc>("GET", `/shared/${this.token}/documents/${slug}`) : undefined;
    } catch (e) {
      this.error = e instanceof ApiError && e.status === 404 ? t("share.invalid") : t("error.unknown");
    }
  }

  private body(d: Doc) {
    const c = d.content ?? {};
    const md = (s: string) => html`<div class="prose">${unsafeHTML(sharedHTML(s ?? "", this.token))}</div>`;
    if (d.type === "markdown") return md(c.markdown);
    if (d.type === "link") return html`<p><a class="btn" href=${c.url} target="_blank" rel="noopener external">${icon("ExternalLink", 15)} ${t("doc.open_link")}</a></p>${c.description ? md(c.description) : ""}`;
    const part = (key: "adr.context" | "adr.decision" | "adr.consequences" | "adr.alternatives", s: string) => (s ? html`<section><h2>${t(key)}</h2>${md(s)}</section>` : "");
    return html`${part("adr.context", c.context)}${part("adr.decision", c.decision)}${part("adr.consequences", c.consequences)}${part("adr.alternatives", c.alternatives)}`;
  }

  render() {
    if (this.error) return html`<main style="max-width:600px;margin:60px auto;text-align:center">${icon("Lock", 32)}<h1 style="margin:12px 0">${t("share.invalid_title")}</h1><p class="muted">${this.error}</p></main>`;
    const v = this.view;
    if (!v) return html`<p class="muted" style="padding:24px">${t("app.loading")}</p>`;
    const d = this.current;
    return html`<header><span class="pi"></span><b>${v.project.name}</b><span class="chip ro">${icon("Globe", 12)} ${t("share.read_only")}</span></header>
      <div class=${v.single ? "wrap single" : "wrap"}>
        ${v.single ? "" : html`<nav aria-label=${t("nav.menu")}>${v.documents.map((x) => html`<a href="/s/${this.token}/${x.slug}" class=${x.id === d?.id ? "on" : ""}>${icon(x.type === "adr" ? "Scale" : x.type === "link" ? "Link" : "FileText", 14)} ${x.title}</a>`)}</nav>`}
        <main>${d ? html`<article class="enter"><h1>${d.title}</h1><p class="muted" style="font-size:13px">${t("doc.updated", { date: formatDate(d.updatedAt) })}</p>${this.body(d)}</article>` : html`<p class="muted">${t("share.empty")}</p>`}</main>
      </div>`;
  }
}
customElements.define("phx-shared-view", PhxSharedView);
