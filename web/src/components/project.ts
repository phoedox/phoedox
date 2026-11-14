import { Routes } from "@lit-labs/router";
import { loadProjectPlugins } from "../plugin-host";
import { SignalWatcher } from "@lit-labs/signals";
import { LitElement, css, html } from "lit";
import { api, hasFeature, type Doc, type ProjectFeature, type DraftInfo, type Favorite, type Project } from "../api";
import { applyProjectColor } from "../color";
import { t } from "../i18n";
import { icon } from "../icon";
import { contentLocale, currentProject, docsVersion, favorites } from "../state";
import { preferredLocale } from "../languages";
import { base } from "./shared";
import "./topology-view";
import "./doc-view";
import "./doc-form";
import "./decisions";
import "./history";
import "./translate";
import "./trash";
import "./entity-view";
import "./entity-form";
import "./data-model";
import "./graph-view";
import "./project-files";
import "./suggestions";
import "./project-settings";
import "./create-dialog";
import type { PhxCreateDialog } from "./create-dialog";

const typeIcon: Record<Doc["type"], string> = { markdown: "FileText", adr: "Scale", link: "ExternalLink", board: "SquareKanban", floorplan: "LandPlot" };

/** Project layout: sidebar with documents by category + nested routes. */
export class PhxProject extends SignalWatcher(LitElement) {
  static properties = { slug: {}, root: {}, docs: { state: true }, drafts: { state: true }, collapsed: { state: true }, newOpen: { state: true }, error: { state: true }, menuOpen: { state: true }, dropTarget: { state: true } };
  static styles = [base, css`
    :host { display: grid; grid-template-columns: 260px minmax(0, 1fr); min-height: calc(100vh - var(--header-h)); }
    /* The sidebar stays in place; only its page tree scrolls, the actions stay visible. */
    aside { border-right: 1px solid var(--line); display: flex; flex-direction: column; background: var(--bg);
      position: sticky; top: var(--header-h); height: calc(100dvh - var(--header-h)); }
    .scroll { flex: 1; min-height: 0; overflow-y: auto; padding: 14px 10px; display: flex; flex-direction: column; gap: 14px; scrollbar-width: thin; }
    .lbl { display: flex; align-items: center; gap: 4px; }
    .lbl .fold { all: unset; display: flex; align-items: center; gap: 4px; cursor: pointer; flex: 1; min-width: 0; border-radius: 4px; }
    .lbl .fold:focus-visible { outline: 2px solid var(--accent); }
    .lbl .fold .ico { transition: transform var(--dur-fast) var(--ease-out); flex: none; }
    .lbl .fold[aria-expanded="false"] .ico { transform: rotate(-90deg); }
    .lbl .add { all: unset; display: grid; place-items: center; width: 20px; height: 20px; border-radius: 5px; color: var(--muted); cursor: pointer; opacity: 0; }
    .lbl .add:focus-visible { opacity: 1; outline: 2px solid var(--accent); }
    @media (hover: hover) and (pointer: fine) { .grp:hover > .lbl .add { opacity: 1; } .lbl .add:hover { background: var(--surface2); color: var(--fg); } }
    @media (hover: none) { .lbl .add { opacity: 1; } }
    .sub { margin-left: 10px; padding-left: 6px; border-left: 1px solid var(--line); }
    .grp { display: flex; flex-direction: column; gap: 1px; }
    .lbl { font-size: 11px; letter-spacing: .08em; text-transform: uppercase; color: var(--muted); font-weight: 600; padding: 0 8px 4px; }
    aside a { display: flex; align-items: center; gap: 8px; padding: 6px 8px; border-radius: 6px; color: var(--muted); text-decoration: none; font-size: 14px;
      transition: background-color var(--dur-fast) ease, color var(--dur-fast) ease; }
    aside a.on { background: var(--accent-soft); color: var(--fg); }
    @media (hover: hover) and (pointer: fine) { aside a:hover { background: var(--surface2); color: var(--fg); } }
    aside a .ext { margin-left: auto; opacity: .7; }
    aside a .num { margin-left: auto; font-family: var(--font-mono); font-size: 11px; opacity: .8; }
    /* Favorites: a slightly highlighted group at the top. */
    .grp.favs { background: color-mix(in srgb, var(--accent-soft) 45%, transparent); border-radius: 8px; padding: 6px 0 4px; }
    aside a .mark { margin-left: auto; opacity: .6; }
    aside a[draggable="true"] { cursor: grab; }
    aside a.drop { box-shadow: inset 0 2px 0 var(--accent); }
    .tools { display: flex; flex-direction: column; gap: 1px; padding: 8px 10px 10px; border-top: 1px solid var(--line); background: var(--surface); }
    .tools .new { position: relative; }
    .tools .new > button { width: 100%; justify-content: flex-start; }
    .tools .menu { position: absolute; bottom: calc(100% + 4px); left: 0; right: 0; z-index: 10; display: flex; flex-direction: column; gap: 1px; padding: 4px;
      background: var(--bg); border: 1px solid var(--line); border-radius: 10px; box-shadow: 0 8px 24px rgb(0 0 0 / .12);
      transform-origin: bottom left; transition: opacity var(--dur-fast) var(--ease-out), transform var(--dur-fast) var(--ease-out);
      @starting-style { opacity: 0; transform: scale(.97); } }
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
      aside { position: fixed; inset: 0 auto 0 0; z-index: 30; width: min(300px, 86vw); height: 100dvh; top: 0;
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
  /** True on /p/:project itself: show the overview. */
  root = false;
  docs: Doc[] = [];
  /** Pages with an open draft (editors only). */
  drafts = new Set<string>();
  /** Collapsed category paths (remembered per browser and project). */
  collapsed = new Set<string>();
  newOpen = false;
  error = "";
  /** Drawer state on small screens; ignored on wide screens. */
  menuOpen = false;
  /** Favorite being dragged and the one it hovers. */
  private dragging?: Favorite;
  dropTarget = "";
  private loadedFor = "";
  private loadedVersion = -1;

  private routes = new Routes(this, [
    { path: "d/:doc/edit", render: ({ doc }) => html`<phx-doc-form .project=${this.slug} .docSlug=${doc!}></phx-doc-form>` },
    { path: "d/:doc/translate/:lang", render: ({ doc, lang }) => html`<phx-translate .project=${this.slug} .docSlug=${doc!} .lang=${lang!}></phx-translate>` },
    { path: "d/:doc/history", render: ({ doc }) => html`<phx-history .project=${this.slug} .docSlug=${doc!}></phx-history>` },
    { path: "trash", render: () => html`<phx-trash .project=${this.slug}></phx-trash>` },
    { path: "d/:doc", render: ({ doc }) => html`<phx-doc-view .project=${this.slug} .docSlug=${doc!}></phx-doc-view>` },
    { path: "new/:type", render: ({ type }) => html`<phx-doc-form .project=${this.slug} .type=${type!}></phx-doc-form>` },
    { path: "e/:entity/edit", render: ({ entity }) => html`<phx-entity-form .project=${this.slug} .entity=${entity!}></phx-entity-form>` },
    { path: "new-entity", render: () => html`<phx-entity-form .project=${this.slug}></phx-entity-form>` },
    { path: "model", render: () => this.gated("model", html`<phx-data-model .project=${this.slug}></phx-data-model>`) },
    { path: "files", render: () => this.gated("files", html`<phx-project-files .project=${this.slug}></phx-project-files>`) },
    { path: "graph", render: () => this.gated("graph", html`<phx-graph-view .project=${this.slug}></phx-graph-view>`) },
    { path: "topology", render: () => this.gated("graph", html`<phx-topology-view .project=${this.slug}></phx-topology-view>`) },
    { path: "e/:entity", render: ({ entity }) => html`<phx-entity-view .project=${this.slug} .entity=${entity!}></phx-entity-view>` },
    { path: "decisions", render: () => this.gated("decisions", html`<phx-decisions .project=${this.slug}></phx-decisions>`) },
    { path: "suggestions", render: () => html`<phx-suggestions .project=${this.slug}></phx-suggestions>` },
    { path: "settings", render: () => html`<phx-project-settings .project=${this.slug}></phx-project-settings>` },
  ], { fallback: { render: () => this.overview() } });

  async willUpdate(changed: Map<string, unknown>) {
    if (this.root && (changed.has("root") || changed.has("slug"))) await this.routes.goto("");
    const v = docsVersion.get();
    if (this.slug && (this.slug !== this.loadedFor || v !== this.loadedVersion)) {
      this.loadedFor = this.slug;
      this.loadedVersion = v;
      try {
        const p = await api<Project>("GET", `/projects/${this.slug}`);
        currentProject.set(p);
        contentLocale.set(p.mainLocale ? preferredLocale(p) : "");
        try {
          this.collapsed = new Set(JSON.parse(localStorage.getItem(`phx.collapsed.${p.slug}`) ?? "[]"));
        } catch {
          this.collapsed = new Set();
        }
        void loadProjectPlugins(p.slug);
        applyProjectColor(document.documentElement, p.primaryColor);
        this.docs = (await api<Doc[]>("GET", `/projects/${this.slug}/documents`)) ?? [];
        this.drafts = new Set(((await api<DraftInfo[]>("GET", `/projects/${this.slug}/drafts`).catch(() => [])) ?? []).map((d) => d.documentId));
        favorites.set((await api<Favorite[]>("GET", `/projects/${this.slug}/favorites`).catch(() => [])) ?? []);
      } catch {
        this.error = t("error.not_found");
      }
    }
  }

  connectedCallback() {
    super.connectedCallback();
    addEventListener("popstate", this.onNavigate);
    addEventListener("keydown", this.onKey);
    addEventListener("click", this.closeNew);
  }

  disconnectedCallback() {
    super.disconnectedCallback();
    removeEventListener("popstate", this.onNavigate);
    removeEventListener("keydown", this.onKey);
    removeEventListener("click", this.closeNew);
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

  /** Saves a new order of one scope (project or personal). */
  private async reorder(personal: boolean, ids: string[]) {
    const others = favorites.get().filter((f) => f.personal !== personal);
    const mine = ids.map((documentId, i) => ({ documentId, personal, position: i + 1 }));
    favorites.set(personal ? [...others, ...mine] : [...mine, ...others]);
    await api("PUT", `/projects/${this.slug}/favorites?scope=${personal ? "me" : "project"}`, { documents: ids }).catch(() => {});
  }

  private move(f: Favorite, to: Favorite) {
    const ids = favorites.get().filter((x) => x.personal === f.personal).map((x) => x.documentId).filter((id) => id !== f.documentId);
    ids.splice(ids.indexOf(to.documentId), 0, f.documentId);
    return this.reorder(f.personal, ids);
  }

  /** Alt+Arrow moves a favorite with the keyboard. */
  private onFavKey(e: KeyboardEvent, f: Favorite) {
    if (!e.altKey || (e.key !== "ArrowUp" && e.key !== "ArrowDown")) return;
    e.preventDefault();
    const ids = favorites.get().filter((x) => x.personal === f.personal).map((x) => x.documentId);
    const i = ids.indexOf(f.documentId);
    const j = e.key === "ArrowUp" ? i - 1 : i + 1;
    if (j < 0 || j >= ids.length) return;
    [ids[i], ids[j]] = [ids[j], ids[i]];
    this.reorder(f.personal, ids).then(() => this.updateComplete).then(() =>
      this.renderRoot.querySelector<HTMLElement>(`a[data-fav="${f.personal ? "me" : "p"}-${f.documentId}"]`)?.focus());
  }

  private favoritesGroup(here: string) {
    const byId = new Map(this.docs.map((d) => [d.id, d]));
    const canEdit = ["editor", "owner"].includes(currentProject.get()?.role ?? "");
    const favs = favorites.get().filter((f) => byId.has(f.documentId));
    if (!favs.length) return "";
    return html`<div class="grp favs">
      <div class="lbl">${t("nav.favorites")}</div>
      ${favs.map((f) => {
        const d = byId.get(f.documentId)!;
        const movable = f.personal || canEdit;
        return html`<a href="/p/${this.slug}/d/${d.slug}" data-fav="${f.personal ? "me" : "p"}-${d.id}"
          class=${[here.startsWith(`/p/${this.slug}/d/${d.slug}`) ? "on" : "", this.dropTarget === `${f.personal}-${d.id}` ? "drop" : ""].join(" ")}
          draggable=${movable ? "true" : "false"} title=${movable ? t("fav.reorder_hint") : ""}
          @keydown=${(e: KeyboardEvent) => movable && this.onFavKey(e, f)}
          @dragstart=${(e: DragEvent) => { this.dragging = f; e.dataTransfer!.effectAllowed = "move"; }}
          @dragover=${(e: DragEvent) => { if (this.dragging && this.dragging.personal === f.personal && this.dragging !== f) { e.preventDefault(); this.dropTarget = `${f.personal}-${d.id}`; } }}
          @dragleave=${() => (this.dropTarget = "")}
          @drop=${(e: DragEvent) => { e.preventDefault(); this.dropTarget = ""; if (this.dragging) this.move(this.dragging, f); this.dragging = undefined; }}
          @dragend=${() => { this.dragging = undefined; this.dropTarget = ""; }}>
          ${icon(typeIcon[d.type], 15)} ${d.title}
          <span class="mark" title=${t(f.personal ? "fav.personal" : "fav.project")}>${icon(f.personal ? "Star" : "Pin", 12)}</span></a>`;
      })}
    </div>`;
  }

  /** Label of the current page for the mobile bar. */
  private where(here: string) {
    const doc = this.docs.find((d) => here.startsWith(`/p/${this.slug}/d/${d.slug}`));
    if (doc) return doc.title;
    if (here.endsWith("/decisions")) return t("nav.decisions");
    if (here.endsWith("/trash")) return t("nav.trash");
    if (here.endsWith("/graph")) return t("nav.graph");
    if (here.endsWith("/model")) return t("nav.model");
    if (here.endsWith("/files")) return t("nav.files");
    if (here.endsWith("/new-entity")) return t("entity.new");
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

  /** Optional tabs (ADR-0032): switched off shows a note instead of the page. */
  private gated(f: ProjectFeature, page: unknown) {
    const p = currentProject.get();
    if (!p || hasFeature(p, f)) return page;
    return html`<div class="enter"><h1>${t(`feature.${f}` as any)}</h1>
      <p class="muted">${t("feature.off")}</p>
      ${p.role === "owner" ? html`<a class="btn" href="/p/${this.slug}/settings">${icon("Settings", 14)} ${t("feature.enable")}</a>` : ""}</div>`;
  }

  private closeNew = () => {
    if (this.newOpen) this.newOpen = false;
  };

  private toggleCategory(path: string) {
    const c = new Set(this.collapsed);
    if (c.has(path)) c.delete(path);
    else c.add(path);
    this.collapsed = c;
    try {
      localStorage.setItem(`phx.collapsed.${this.slug}`, JSON.stringify([...c]));
    } catch {
      /* not remembered */
    }
  }

  private create(category: string, step: "what" | "type" = "what") {
    this.newOpen = false;
    this.renderRoot.querySelector<PhxCreateDialog>("phx-create-dialog")!.open(category, step);
  }

  private docLink(d: Doc, here: string) {
    return d.type === "link" && d.content?.url
      ? html`<a href=${d.content.url} target="_blank" rel="noopener external" title=${t("nav.external")}>${icon("Link", 15)} ${d.title}<span class="ext">${icon("ExternalLink", 13)}</span></a>`
      : html`<a href="/p/${this.slug}/d/${d.slug}" class=${here.startsWith(`/p/${this.slug}/d/${d.slug}`) ? "on" : ""}>${icon(typeIcon[d.type], 15)} ${d.title}${this.drafts.has(d.id) ? html`<span class="ext" title=${t("draft.sidebar")} aria-label=${t("draft.sidebar")}>${icon("FilePen", 11)}</span>` : ""}${d.restricted ? html`<span class="ext" title=${t("share.restricted")}>${icon("Lock", 11)}</span>` : ""}</a>`;
  }

  /**
   * Pages by category as a tree: "Betrieb/Server" is the folder "Server"
   * inside "Betrieb". Decisions are not listed here; they have their own page.
   */
  private tree(here: string, canEdit: boolean) {
    type Node = { path: string; name: string; docs: Doc[]; children: Node[] };
    const root: Node = { path: "", name: "", docs: [], children: [] };
    const nodes = new Map<string, Node>([["", root]]);
    const node = (path: string): Node => {
      let n = nodes.get(path);
      if (n) return n;
      const i = path.lastIndexOf("/");
      const parent = node(i < 0 ? "" : path.slice(0, i));
      n = { path, name: path.slice(i + 1), docs: [], children: [] };
      parent.children.push(n);
      nodes.set(path, n);
      return n;
    };
    const uncategorized: Doc[] = [];
    for (const d of this.docs) {
      if (d.type === "adr" || d.parentId) continue; // boards made inside a page live in that page
      const c = (d.category || "").split("/").map((x) => x.trim()).filter(Boolean).join("/");
      if (!c) uncategorized.push(d);
      else node(c).docs.push(d);
    }
    const group = (n: Node, depth: number): unknown => {
      const open = !this.collapsed.has(n.path);
      const id = `cat-${n.path.replace(/[^a-z0-9]+/gi, "-")}`;
      return html`<div class=${depth ? "grp sub" : "grp"}>
        <div class="lbl">
          <button class="fold" aria-expanded=${open ? "true" : "false"} aria-controls=${id} @click=${() => this.toggleCategory(n.path)}>${icon("ChevronDown", 12)}<span>${n.name}</span></button>
          ${canEdit ? html`<button class="add" title=${t("create.here", { category: n.name })} aria-label=${t("create.here", { category: n.name })} @click=${() => this.create(n.path)}>${icon("Plus", 13)}</button>` : ""}
        </div>
        ${open ? html`<div id=${id} class="grp">${n.docs.map((d) => this.docLink(d, here))}${n.children.map((c) => group(c, depth + 1))}</div>` : ""}
      </div>`;
    };
    return html`${root.children.map((c) => group(c, 0))}
      ${uncategorized.length ? html`<div class="grp">
        <div class="lbl"><span style="flex:1">${t("nav.uncategorized")}</span>
          ${canEdit ? html`<button class="add" title=${t("create.here", { category: t("nav.uncategorized") })} aria-label=${t("create.here", { category: t("nav.uncategorized") })} @click=${() => this.create("")}>${icon("Plus", 13)}</button>` : ""}</div>
        ${uncategorized.map((d) => this.docLink(d, here))}</div>` : ""}`;
  }

  render() {
    if (this.error) return html`<main><p class="error">${this.error}</p></main>`;
    const decisions = this.docs.filter((d) => d.type === "adr").length;
    const proj = currentProject.get();
    const here = location.pathname;
    const canEdit = ["editor", "owner"].includes(currentProject.get()?.role ?? "");
    // Only single pages are shared with this person: no project-wide views.
    const docOnly = currentProject.get()?.role === "doc";
    return html`
      <div class="mbar">
        <button class="ghost" aria-expanded=${this.menuOpen} aria-controls="nav" @click=${() => this.setMenu(!this.menuOpen)}>${icon("Menu", 16)} ${t("nav.menu")}</button>
        <span class="where">${this.where(here)}</span>
      </div>
      <div class=${this.menuOpen ? "scrim open" : "scrim"} @click=${() => this.setMenu(false, true)}></div>
      <aside id="nav" class=${this.menuOpen ? "open" : ""} aria-label=${t("nav.menu")} @click=${this.onAsideClick}>
        <div class="scroll">
          ${this.favoritesGroup(here)}
          <div class="grp"><a href="/p/${this.slug}" class=${here === `/p/${this.slug}` ? "on" : ""}>${icon("House", 15)} ${t("project.overview")}</a>
            ${!hasFeature(proj, "decisions") ? "" : html`<a href="/p/${this.slug}/decisions" class=${here === `/p/${this.slug}/decisions` || this.docs.some((d) => d.type === "adr" && here.startsWith(`/p/${this.slug}/d/${d.slug}`)) ? "on" : ""}>${icon("ListChecks", 15)} ${t("nav.decisions")}
              ${decisions ? html`<span class="num">${decisions}</span>` : ""}</a>`}
            ${docOnly || !hasFeature(proj, "graph") ? "" : html`<a href="/p/${this.slug}/graph" class=${here === `/p/${this.slug}/graph` || here.startsWith(`/p/${this.slug}/e/`) ? "on" : ""}>${icon("Waypoints", 15)} ${t("nav.graph")}</a>
              <a href="/p/${this.slug}/topology" class=${here === `/p/${this.slug}/topology` ? "on" : ""}>${icon("Network", 15)} ${t("topo.title")}</a>`}
            ${docOnly || !hasFeature(proj, "model") ? "" : html`<a href="/p/${this.slug}/model" class=${here === `/p/${this.slug}/model` ? "on" : ""}>${icon("Shapes", 15)} ${t("nav.model")}</a>`}
            ${docOnly || !hasFeature(proj, "files") ? "" : html`<a href="/p/${this.slug}/files" class=${here === `/p/${this.slug}/files` ? "on" : ""}>${icon("Paperclip", 15)} ${t("nav.files")}</a>`}</div>
          ${this.tree(here, canEdit)}
        </div>
        <div class="tools">
          ${canEdit ? html`<div class="new">
            <button class="ghost" aria-expanded=${this.newOpen ? "true" : "false"} aria-haspopup="menu" @click=${(e: Event) => { e.stopPropagation(); this.newOpen = !this.newOpen; }}>${icon("Plus", 15)} ${t("create.new")}</button>
            ${this.newOpen ? html`<div class="menu" role="menu" @keydown=${(e: KeyboardEvent) => e.key === "Escape" && (this.newOpen = false)}>
              <a role="menuitem" href="#" @click=${(e: Event) => { e.preventDefault(); this.create("", "type"); }}>${icon("FilePlus", 15)} ${t("create.page")}</a>
              <a role="menuitem" href="#" @click=${(e: Event) => { e.preventDefault(); this.create(""); }}>${icon("FolderPlus", 15)} ${t("create.folder")}</a>
              ${hasFeature(proj, "decisions") ? html`<a role="menuitem" href="/p/${this.slug}/new/adr" @click=${() => (this.newOpen = false)}>${icon("Scale", 15)} ${t("doc.type.adr")}</a>` : ""}
              ${hasFeature(proj, "graph") || hasFeature(proj, "model") ? html`<a role="menuitem" href="/p/${this.slug}/new-entity" @click=${() => (this.newOpen = false)}>${icon("Plus", 15)} ${t("entity.new")}</a>` : ""}
            </div>` : ""}
          </div>
          <a href="/p/${this.slug}/trash" class=${here.endsWith("/trash") ? "on" : ""}>${icon("Trash2", 15)} ${t("nav.trash")}</a>` : ""}
          ${docOnly ? "" : html`<a href="/p/${this.slug}/suggestions" class=${here.endsWith("/suggestions") ? "on" : ""}>${icon("Bot", 15)} ${t("nav.suggestions")}</a>
          <a href="/p/${this.slug}/settings" class=${here.endsWith("/settings") ? "on" : ""}>${icon("Settings", 15)} ${t("nav.settings")}</a>`}
        </div>
        <phx-create-dialog .project=${this.slug}></phx-create-dialog>
      </aside>
      <main>${this.routes.outlet()}</main>
    `;
  }
}
customElements.define("phx-project", PhxProject);
