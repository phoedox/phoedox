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
  static properties = { slug: {}, docs: { state: true }, error: { state: true }, menuOpen: { state: true } };
  static styles = [base, css`
    :host { display: grid; grid-template-columns: 260px minmax(0, 1fr); min-height: calc(100vh - var(--header-h)); }
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
    .mbar, .scrim { display: none; }

    /* Phones and small tablets: the sidebar becomes a drawer from the left. */
    @media (max-width: 800px) {
      :host { grid-template-columns: 1fr; align-content: start; }
      main { padding: 18px 16px 48px; }
      .mbar { display: flex; align-items: center; gap: 10px; position: sticky; top: var(--header-h); z-index: 5;
        padding: 8px 12px; background: var(--bg); border-bottom: 1px solid var(--line); }
      .mbar button { padding: 6px 10px; }
      .mbar .where { color: var(--muted); font-size: 14px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; min-width: 0; }
      aside { position: fixed; inset: 0 auto 0 0; z-index: 30; width: min(300px, 86vw); overflow-y: auto;
        border-right: 1px solid var(--line); box-shadow: 0 0 40px rgb(0 0 0 / .18);
        transform: translateX(-100%); visibility: hidden;
        transition: transform var(--dur-drawer) var(--ease-drawer), visibility 0s linear var(--dur-drawer); }
      aside.open { transform: none; visibility: visible; transition: transform var(--dur-drawer) var(--ease-drawer), visibility 0s; }
      .scrim { display: block; position: fixed; inset: 0; z-index: 25; background: rgb(0 0 0 / .35); opacity: 0; pointer-events: none;
        transition: opacity var(--dur-drawer) var(--ease-out); }
      .scrim.open { opacity: 1; pointer-events: auto; }
    }
    @media (max-width: 800px) and (prefers-reduced-motion: reduce) {
      aside { transform: none; opacity: 0; transition: opacity var(--dur) ease, visibility 0s linear var(--dur); }
      aside.open { opacity: 1; transition: opacity var(--dur) ease, visibility 0s; }
    }
  `];
  slug = "";
  docs: Doc[] = [];
  error = "";
  /** Drawer state on small screens; ignored on wide screens. */
  menuOpen = false;
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

  connectedCallback() {
    super.connectedCallback();
    addEventListener("popstate", this.onNavigate);
    addEventListener("keydown", this.onKey);
  }

  disconnectedCallback() {
    super.disconnectedCallback();
    removeEventListener("popstate", this.onNavigate);
    removeEventListener("keydown", this.onKey);
    this.setMenu(false);
    delete document.documentElement.dataset.pc;
  }

  private onNavigate = () => this.setMenu(false);
  private onKey = (e: KeyboardEvent) => {
    if (e.key === "Escape" && this.menuOpen) this.setMenu(false, true);
  };

  /** Following a link inside the drawer closes it. */
  private onAsideClick = (e: Event) => {
    if ((e.target as HTMLElement).closest("a")) this.setMenu(false);
  };

  /** Opens or closes the drawer; locks page scroll while it is open. */
  private async setMenu(open: boolean, restoreFocus = false) {
    if (open === this.menuOpen) return;
    this.menuOpen = open;
    document.documentElement.style.overflow = open ? "hidden" : "";
    await this.updateComplete;
    if (open) this.renderRoot.querySelector<HTMLElement>("aside a")?.focus();
    else if (restoreFocus) this.renderRoot.querySelector<HTMLElement>(".mbar button")?.focus();
  }

  /** Label of the current page for the mobile bar. */
  private where(here: string) {
    const doc = this.docs.find((d) => here.startsWith(`/p/${this.slug}/d/${d.slug}`));
    if (doc) return doc.title;
    if (here.endsWith("/decisions")) return t("nav.decisions");
    if (here.endsWith("/suggestions")) return t("nav.suggestions");
    if (here.endsWith("/settings")) return t("nav.settings");
    return t("project.overview");
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
      <div class="mbar">
        <button class="ghost" aria-expanded=${this.menuOpen} aria-controls="nav" @click=${() => this.setMenu(!this.menuOpen)}>${icon("Menu", 16)} ${t("nav.menu")}</button>
        <span class="where">${this.where(here)}</span>
      </div>
      <div class=${this.menuOpen ? "scrim open" : "scrim"} @click=${() => this.setMenu(false, true)}></div>
      <aside id="nav" class=${this.menuOpen ? "open" : ""} aria-label=${t("nav.menu")} @click=${this.onAsideClick}>
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
