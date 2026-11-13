import { Routes } from "@lit-labs/router";
import { SignalWatcher } from "@lit-labs/signals";
import { LitElement, css, html } from "lit";
import { adrRef, api, type Doc, type Project } from "../api";
import { applyProjectColor } from "../color";
import { t } from "../i18n";
import { icon } from "../icon";
import { currentProject, docsVersion } from "../state";
import { base } from "./shared";
import "./doc-view";
import "./doc-form";
import "./decisions";
import "./entity-view";
import "./suggestions";
import "./project-settings";

const typeIcon: Record<Doc["type"], string> = { markdown: "FileText", adr: "Scale", link: "ExternalLink" };

/** Project layout: sidebar with documents by category + nested routes. */
export class PhxProject extends SignalWatcher(LitElement) {
  static properties = { slug: {}, docs: { state: true }, error: { state: true } };
  static styles = [base, css`
    :host { display: grid; grid-template-columns: 260px minmax(0, 1fr); min-height: calc(100vh - 53px); }
    aside { border-right: 1px solid var(--line); padding: 14px 10px; display: flex; flex-direction: column; gap: 14px; background: var(--bg); }
    .grp { display: flex; flex-direction: column; gap: 1px; }
    .lbl { font-size: 11px; letter-spacing: .08em; text-transform: uppercase; color: var(--muted); font-weight: 600; padding: 0 8px 4px; }
    aside a { display: flex; align-items: center; gap: 8px; padding: 6px 8px; border-radius: 6px; color: var(--muted); text-decoration: none; font-size: 14px;
      transition: background-color var(--dur-fast) ease, color var(--dur-fast) ease; }
    aside a.on { background: var(--accent-soft); color: var(--fg); }
    @media (hover: hover) and (pointer: fine) { aside a:hover { background: var(--surface2); color: var(--fg); } }
    aside a .ext { margin-left: auto; opacity: .7; }
    aside a .num { margin-left: auto; font-family: var(--font-mono); font-size: 11px; opacity: .8; }
    .tools { margin-top: auto; display: flex; flex-direction: column; gap: 1px; }
    main { padding: 24px 32px 60px; min-width: 0; }
    @media (max-width: 800px) { :host { grid-template-columns: 1fr; } aside { border-right: 0; border-bottom: 1px solid var(--line); } main { padding: 18px 16px; } }
  `];
  slug = "";
  docs: Doc[] = [];
  error = "";
  private loadedFor = "";
  private loadedVersion = -1;

  private routes = new Routes(this, [
    { path: "d/:doc/edit", render: ({ doc }) => html`<phx-doc-form .project=${this.slug} .docSlug=${doc!}></phx-doc-form>` },
    { path: "d/:doc", render: ({ doc }) => html`<phx-doc-view .project=${this.slug} .docSlug=${doc!}></phx-doc-view>` },
    { path: "new/:type", render: ({ type }) => html`<phx-doc-form .project=${this.slug} .type=${type!}></phx-doc-form>` },
    { path: "e/:entity", render: ({ entity }) => html`<phx-entity-view .project=${this.slug} .entity=${entity!}></phx-entity-view>` },
    { path: "decisions", render: () => html`<phx-decisions .project=${this.slug}></phx-decisions>` },
    { path: "suggestions", render: () => html`<phx-suggestions .project=${this.slug}></phx-suggestions>` },
    { path: "settings", render: () => html`<phx-project-settings .project=${this.slug}></phx-project-settings>` },
  ], { fallback: { render: () => this.overview() } });

  async willUpdate() {
    const v = docsVersion.get();
    if (this.slug && (this.slug !== this.loadedFor || v !== this.loadedVersion)) {
      this.loadedFor = this.slug;
      this.loadedVersion = v;
      try {
        const p = await api<Project>("GET", `/projects/${this.slug}`);
        currentProject.set(p);
        applyProjectColor(document.documentElement, p.primaryColor);
        this.docs = (await api<Doc[]>("GET", `/projects/${this.slug}/documents`)) ?? [];
      } catch {
        this.error = t("error.not_found");
      }
    }
  }

  disconnectedCallback() {
    super.disconnectedCallback();
    delete document.documentElement.dataset.pc;
  }

  private overview() {
    const p = currentProject.get();
    if (!p) return "";
    return html`<div class="enter">
      <h1>${p.name}</h1>
      ${p.description ? html`<p class="muted">${p.description}</p>` : ""}
      <p class="row"><span class="chip">${t("project.documents", { count: this.docs.length })}</span>
        ${p.aiWriteMode === "suggest" ? html`<span class="chip">${icon("Bot", 12)} ${t("project.suggest_hint")}</span>` : ""}</p>
      ${this.docs.length === 0 ? html`<p class="muted">${t("project.empty")}</p>` : ""}
    </div>`;
  }

  render() {
    if (this.error) return html`<main><p class="error">${this.error}</p></main>`;
    const groups = new Map<string, Doc[]>();
    for (const d of this.docs) {
      const c = d.category || "";
      groups.set(c, [...(groups.get(c) ?? []), d]);
    }
    const here = location.pathname;
    const canEdit = ["editor", "owner"].includes(currentProject.get()?.role ?? "");
    return html`
      <aside aria-label=${t("nav.projects")}>
        <div class="grp"><a href="/p/${this.slug}" class=${here === `/p/${this.slug}` ? "on" : ""}>${icon("House", 15)} ${t("project.overview")}</a>
          <a href="/p/${this.slug}/decisions" class=${here === `/p/${this.slug}/decisions` ? "on" : ""}>${icon("ListChecks", 15)} ${t("nav.decisions")}</a></div>
        ${[...groups].map(([cat, docs]) => html`<div class="grp">
          <div class="lbl">${cat || t("nav.uncategorized")}</div>
          ${docs.map((d) => d.type === "link" && d.content?.url
            ? html`<a href=${d.content.url} target="_blank" rel="noopener external" title=${t("nav.external")}>${icon("Link", 15)} ${d.title}<span class="ext">${icon("ExternalLink", 13)}</span></a>`
            : html`<a href="/p/${this.slug}/d/${d.slug}" class=${here.startsWith(`/p/${this.slug}/d/${d.slug}`) ? "on" : ""}>${icon(typeIcon[d.type], 15)} ${d.title}${d.number ? html`<span class="num" title=${adrRef(d.number)}>${String(d.number).padStart(4, "0")}</span>` : ""}</a>`)}
        </div>`)}
        <div class="tools">
          ${canEdit ? html`
            <a href="/p/${this.slug}/new/markdown">${icon("FilePlus", 15)} ${t("doc.type.markdown")}</a>
            <a href="/p/${this.slug}/new/adr">${icon("Scale", 15)} ${t("doc.type.adr")}</a>
            <a href="/p/${this.slug}/new/link">${icon("Link", 15)} ${t("doc.type.link")}</a>` : ""}
          <a href="/p/${this.slug}/suggestions" class=${here.endsWith("/suggestions") ? "on" : ""}>${icon("Bot", 15)} ${t("nav.suggestions")}</a>
          <a href="/p/${this.slug}/settings" class=${here.endsWith("/settings") ? "on" : ""}>${icon("Settings", 15)} ${t("nav.settings")}</a>
        </div>
      </aside>
      <main>${this.routes.outlet()}</main>
    `;
  }
}
customElements.define("phx-project", PhxProject);
