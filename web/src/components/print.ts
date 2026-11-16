import { LitElement, css, html, unsafeCSS } from "lit";
import { unsafeHTML } from "lit/directives/unsafe-html.js";
import { ApiError, adrRef, api, type Doc, type Project } from "../api";
import { formatDate, t } from "../i18n";
import { icon } from "../icon";
import { renderMarkdown, withoutTitle } from "../md";
import { contentLocale, currentProject } from "../state";
import { prose } from "./doc-view";
import { base } from "./shared";

/** Print view for PDF (ADR: the browser's "Save as PDF"): one page, one
 * category or the whole project, with cover and contents, rendered like in
 * the app (diagrams, tables, whiteboards). /print/:project?doc=slug or
 * ?category=Name. */
export class PhxPrint extends LitElement {
  static properties = { project: {}, proj: { state: true }, docs: { state: true }, done: { state: true }, total: { state: true }, error: { state: true } };
  static styles = [base, unsafeCSS(prose), css`
    :host { display: block; background: var(--bg); color: var(--fg); }
    .bar { position: sticky; top: 0; z-index: 5; display: flex; gap: 10px; align-items: center; flex-wrap: wrap; padding: 10px 16px; background: var(--surface); border-bottom: 1px solid var(--line); }
    .bar .sp { flex: 1; }
    .bar .muted { font-size: 13px; }
    .paper { max-width: 820px; margin: 0 auto; padding: 32px 24px 80px; }
    .cover { min-height: 70vh; display: grid; align-content: center; gap: 14px; }
    .cover .tile { width: 64px; height: 64px; border-radius: 16px; display: grid; place-items: center; background: var(--pl, var(--accent)); color: var(--on-color); font: 700 24px var(--font-body); }
    .cover h1 { font-size: 40px; margin: 0; }
    .cover p { color: var(--muted); margin: 0; max-width: 60ch; }
    .toc h2 { margin-top: 0; }
    .toc h3 { font-size: 13px; text-transform: uppercase; letter-spacing: .05em; color: var(--muted); margin: 18px 0 6px; }
    .toc ol { margin: 0; padding-left: 0; list-style: none; display: grid; gap: 3px; }
    .toc a { color: var(--fg); text-decoration: none; display: flex; gap: 8px; }
    .toc a .n { color: var(--muted); font-variant-numeric: tabular-nums; min-width: 72px; }
    section.doc { padding-top: 8px; }
    section.doc + section.doc, .toc + section.doc { margin-top: 48px; }
    .doc > header { border-bottom: 1px solid var(--line); padding-bottom: 10px; margin-bottom: 16px; }
    .doc > header h1 { margin: 0 0 6px; font-size: 28px; }
    .meta { display: flex; gap: 6px; flex-wrap: wrap; font-size: 12.5px; color: var(--muted); }
    .meta span { border: 1px solid var(--line); border-radius: 999px; padding: 1px 8px; }
    .cat { font-size: 12px; text-transform: uppercase; letter-spacing: .05em; color: var(--muted); }
    .adr h2 { font-size: 17px; }
    .note { color: var(--muted); font-size: 14px; }
    @media print {
      :host, .paper { background: var(--surface); }
      .bar { display: none; }
      .paper { max-width: none; padding: 0; }
      .cover { min-height: 0; height: 90vh; break-after: page; }
      .toc { break-after: page; }
      section.doc { break-before: page; margin-top: 0 !important; }
      section.doc:first-of-type.single { break-before: auto; }
      h1, h2, h3 { break-after: avoid; }
      pre, table, figure, img, svg, phx-mermaid, phx-whiteboard-embed { break-inside: avoid; }
      a { color: inherit; }
    }
  `];

  project = "";
  proj?: Project;
  docs: Doc[] = [];
  done = 0;
  total = 0;
  error = "";
  private q = new URLSearchParams(location.search);
  private prevTheme?: string;
  private pageStyle?: HTMLStyleElement;

  connectedCallback() {
    super.connectedCallback();
    // Paper is white: the light theme while printing; page margins and numbers.
    this.prevTheme = document.documentElement.dataset.theme;
    document.documentElement.dataset.theme = "light";
    this.pageStyle = document.createElement("style");
    this.pageStyle.textContent = `@page { size: A4; margin: 18mm 16mm 20mm; @bottom-right { content: counter(page) " / " counter(pages); font: 9pt sans-serif; color: #777; } }
      @media print { body { background: var(--surface) !important; } header, phx-palette { display: none !important; } }`;
    document.head.append(this.pageStyle);
    void this.load();
  }

  disconnectedCallback() {
    super.disconnectedCallback();
    if (this.prevTheme === undefined) delete document.documentElement.dataset.theme;
    else document.documentElement.dataset.theme = this.prevTheme;
    this.pageStyle?.remove();
  }

  private async load() {
    try {
      this.proj = await api<Project>("GET", `/projects/${this.project}`);
      currentProject.set(this.proj);
      const lang = contentLocale.get();
      const one = this.q.get("doc");
      const category = this.q.get("category");
      let list: Doc[];
      if (one) {
        list = [await api<Doc>("GET", `/projects/${this.project}/documents/${encodeURIComponent(one)}${lang ? `?lang=${lang}` : ""}`)];
      } else {
        list = (await api<Doc[]>("GET", `/projects/${this.project}/documents${lang ? `?lang=${lang}` : ""}`))
          .filter((d) => !d.parentId && (category === null || (d.category || "") === category || (d.category || "").startsWith(category + "/")));
        list.sort((a, b) => (a.category || "").localeCompare(b.category || "") || (a.number ?? 0) - (b.number ?? 0) || a.position - b.position || a.title.localeCompare(b.title));
      }
      this.total = list.length;
      // Full content, a few at a time.
      const full: Doc[] = new Array(list.length);
      let next = 0;
      const worker = async () => {
        while (next < list.length) {
          const i = next++;
          full[i] = list[i].content ? list[i] : await api<Doc>("GET", `/projects/${this.project}/documents/${list[i].id}${lang ? `?lang=${lang}` : ""}`);
          this.done++;
        }
      };
      await Promise.all([worker(), worker(), worker(), worker()]);
      this.docs = full;
      document.title = one ? `${full[0]?.title} – ${this.proj.name}` : `${this.proj.name}${category ? ` – ${category}` : ""}`;
    } catch (e) {
      this.error = e instanceof ApiError ? e.text : t("error.unknown");
    }
  }

  /** A folder path in the chosen language (each part that has a name). */
  private folder(path: string) {
    const p = this.proj, lang = contentLocale.get();
    if (!path || !p || !lang || lang === p.mainLocale) return path;
    const parts = path.split("/");
    return parts.map((x, i) => p.categoryNames?.[parts.slice(0, i + 1).join("/")]?.[lang] || x).join(" / ");
  }

  private body(d: Doc) {
    const c = d.content ?? {};
    const md = (s?: string) => (s ? html`<div class="prose">${unsafeHTML(renderMarkdown(s, this.project, d.id))}</div>` : "");
    switch (d.type) {
      case "markdown":
        return md(withoutTitle(c.markdown ?? "", d.title));
      case "adr": {
        const sec = (key: string, text?: string) => (text ? html`<h2>${t(key as any)}</h2>${md(text)}` : "");
        return html`<div class="adr">${sec("adr.context", c.context)}
          ${c.options?.length && !c.decision ? html`<h2>${t("adr.options")}</h2>${c.options.map((o: { title: string; description?: string }) => html`<h3>${o.title}</h3>${md(o.description)}`)}` : ""}
          ${sec("adr.decision", c.decision)}${sec("adr.consequences", c.consequences)}${sec("adr.alternatives", c.alternatives)}</div>`;
      }
      case "link":
        return html`<p><a href=${c.url}>${c.url}</a></p>${md(c.description)}`;
      case "whiteboard":
        return html`<phx-whiteboard-embed data-board=${d.slug} data-project=${this.project}></phx-whiteboard-embed>`;
      default:
        return html`${md(c.description)}<p class="note">${t("print.open_app")}</p>`;
    }
  }

  private docHead(d: Doc) {
    const c = d.content ?? {};
    return html`<header>
      ${d.category ? html`<div class="cat">${this.folder(d.category)}</div>` : ""}
      <h1>${d.title}</h1>
      <div class="meta">
        ${d.number ? html`<span>${adrRef(d.number)}</span>` : ""}${d.extRef ? html`<span>${d.extRef}</span>` : ""}
        <span>${t(`doc.type.${d.type}` as any)}</span>
        ${d.type === "adr" && c.status ? html`<span>${t(`adr.status.${c.status}` as any)}</span>` : ""}${c.date ? html`<span>${c.date}</span>` : ""}
        <span>${t("doc.version", { version: d.version })}</span><span>${t("doc.updated", { date: formatDate(d.updatedAt) })}</span>
      </div></header>`;
  }

  render() {
    if (this.error) return html`<p class="error" style="padding:24px">${this.error}</p>`;
    const p = this.proj;
    const single = !!this.q.get("doc");
    const ready = this.docs.length === this.total && this.total > 0;
    const category = this.q.get("category");
    const back = single ? `/p/${this.project}/d/${this.q.get("doc")}` : `/p/${this.project}`;
    let cat: string | undefined;
    return html`<div class="bar">
        <a class="btn ghost" href=${back}>${icon("ArrowLeft", 14)} ${t("print.back")}</a>
        <span class="muted">${ready ? t("print.ready", { count: this.total }) : t("print.loading", { done: this.done, total: this.total || "…" })}</span>
        <span class="sp"></span>
        <span class="muted">${t("print.hint")}</span>
        <button ?disabled=${!ready} @click=${() => window.print()}>${icon("FileDown", 15)} ${t("print.save")}</button>
      </div>
      <div class="paper">
        ${p && !single ? html`<div class="cover">
            <div class="tile" style=${p.primaryColor ? `--pl:${p.primaryColor}` : ""}>${p.short || p.name.slice(0, 2).toUpperCase()}</div>
            <h1>${p.name}</h1>${category ? html`<p><b>${category}</b></p>` : ""}
            ${p.description ? html`<p>${p.description}</p>` : ""}
            <p>${t("print.as_of", { date: formatDate(new Date().toISOString()), count: this.total })}</p></div>
          <nav class="toc" aria-label=${t("print.contents")}><h2>${t("print.contents")}</h2>
            ${this.docs.map((d) => {
              const head = (d.category || "") !== cat ? ((cat = d.category || ""), html`<h3>${cat ? this.folder(cat) : t("print.no_category")}</h3>`) : "";
              return html`${head}<ol><li><a href="#d-${d.id}">${d.number ? html`<span class="n">${adrRef(d.number)}</span>` : ""}${d.title}</a></li></ol>`;
            })}</nav>` : ""}
        ${this.docs.map((d) => html`<section class="doc ${single ? "single" : ""}" id="d-${d.id}">${this.docHead(d)}${this.body(d)}</section>`)}
      </div>`;
  }
}
customElements.define("phx-print", PhxPrint);
