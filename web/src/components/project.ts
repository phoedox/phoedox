import { Routes } from "@lit-labs/router";
import { LIST_KINDS, onProjectChange, stopWatching, watchProject } from "../project-live";
import { loadProjectPlugins } from "../plugin-host";
import { SignalWatcher } from "@lit-labs/signals";
import { LitElement, css, html } from "lit";
import { ApiError, api, hasFeature, type Doc, type ProjectFeature, type DraftInfo, type Favorite, type Project } from "../api";
import { applyProjectColor } from "../color";
import { t } from "../i18n";
import { icon } from "../icon";
import { contentLocale, currentProject, docsVersion, favorites, focusMode, navigate } from "../state";
import { toast } from "./toasts";
import { preferredLocale } from "../languages";
import { comboFor, comboLabel, onShortcut } from "../shortcuts";
import { base } from "./shared";
import "./import-view";
import "./topology-view";
import "./locations";
import "./entity-table";
import "./household";
import "./ipam";
import "./doc-view";
import "./doc-form";
import "./decisions";
import "./history";
import "./translate";
import "./trash";
import "./entity-view";
import "./entity-form";
import "./knowledge";
import type { KnowledgeView } from "./knowledge";
import "./project-files";
import "./suggestions";
import "./monitoring";
import "./insights";
import "./project-settings";
import "./create-dialog";
import "./ai-chat";
import type { PhxCreateDialog } from "./create-dialog";

const typeIcon: Record<Doc["type"], string> = { markdown: "FileText", adr: "Scale", link: "ExternalLink", board: "SquareKanban", floorplan: "LandPlot" };

/** Project layout: sidebar with documents by category + nested routes. */
export class PhxProject extends SignalWatcher(LitElement) {
  static properties = { slug: {}, root: {}, docs: { state: true }, drafts: { state: true }, collapsed: { state: true }, newOpen: { state: true }, error: { state: true }, menuOpen: { state: true }, dropTarget: { state: true }, ctx: { state: true }, sideWidth: { state: true }, sideHidden: { type: Boolean, reflect: true, attribute: "sidehidden" },
    aiChat: { state: true }, chatOpen: { type: Boolean, reflect: true, attribute: "chatopen" } };
  static styles = [base, css`
    :host { display: grid; grid-template-columns: var(--side-w, 260px) minmax(0, 1fr); min-height: calc(100vh - var(--header-h)); }
    /* Collapsed sidebar (desktop): the page uses the full width; a small
       button at the left edge brings the sidebar back. */
    :host([sidehidden]) { grid-template-columns: minmax(0, 1fr); }
    /* AI chat (ADR-0066): a third column on the right. */
    :host([chatopen]) { grid-template-columns: var(--side-w, 260px) minmax(0, 1fr) var(--chat-w, 380px); }
    :host([chatopen][sidehidden]) { grid-template-columns: minmax(0, 1fr) var(--chat-w, 380px); }
    phx-ai-chat { position: sticky; top: var(--header-h); height: calc(100vh - var(--header-h)); }
    .mbar .chatbtn { margin-left: auto; }
    :host([sidehidden]) aside { display: none; }
    .reopen { display: none; }
    :host([sidehidden]) .reopen { display: grid; place-items: center; position: fixed; left: 0; top: calc(var(--header-h) + 12px); z-index: 6;
      width: 26px; height: 34px; padding: 0; border: 1px solid var(--line); border-left: 0; border-radius: 0 8px 8px 0; background: var(--surface); color: var(--muted); cursor: pointer; }
    @media (hover: hover) and (pointer: fine) { :host([sidehidden]) .reopen:hover { color: var(--fg); border-color: var(--muted); } }
    /* Drag the right edge to resize; arrow keys work on the focused edge. */
    .resize { position: absolute; top: 0; right: -4px; width: 8px; height: 100%; cursor: col-resize; z-index: 2; touch-action: none; }
    .resize::after { content: ""; position: absolute; top: 0; bottom: 0; left: 3px; width: 2px; background: transparent; transition: background-color var(--dur-fast) ease; }
    .resize:focus-visible { outline: none; }
    .resize:focus-visible::after, .resize.dragging::after { background: var(--accent); }
    @media (hover: hover) and (pointer: fine) { .resize:hover::after { background: var(--line); } }
    .collapse { all: unset; display: flex; align-items: center; gap: 8px; padding: 6px 8px; border-radius: 6px; color: var(--muted); font-size: 14px; cursor: pointer; }
    .collapse:focus-visible { outline: 2px solid var(--accent); }
    @media (hover: hover) and (pointer: fine) { .collapse:hover { background: var(--surface2); color: var(--fg); } }
    .collapse kbd { margin-left: auto; font: 11px var(--font-mono); opacity: .7; }
    :host([focus]) { grid-template-columns: minmax(0, 1fr); }
    :host([focus]) aside, :host([focus]) .mbar { display: none; }
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
    .ctx { position: fixed; z-index: 50; min-width: 220px; max-width: 280px; display: flex; flex-direction: column; gap: 1px; padding: 4px;
      background: var(--bg); border: 1px solid var(--line); border-radius: 10px; box-shadow: 0 8px 24px rgb(0 0 0 / .14);
      transform-origin: top left; transition: opacity var(--dur-fast) var(--ease-out), transform var(--dur-fast) var(--ease-out);
      @starting-style { opacity: 0; transform: scale(.97); } }
    .ctx button { justify-content: flex-start; gap: 8px; background: none; border: 0; color: var(--fg); font-weight: 400; padding: 6px 8px; border-radius: 6px; text-align: left; }
    .ctx button:hover, .ctx button:focus-visible { background: var(--surface2); outline: none; }
    .ctx button.danger { color: var(--bad); }
    .ctx hr { border: 0; border-top: 1px solid var(--line); margin: 3px 4px; }
    @media (prefers-reduced-motion: reduce) { .ctx { transition: opacity var(--dur-fast) ease; @starting-style { transform: none; } } }
    main { padding: 24px 32px 60px; min-width: 0; }
    .mbar, .scrim { display: none; }

    /* Phones and small tablets: the sidebar becomes a drawer from the left. */
    @media (max-width: 800px) {
      :host, :host([sidehidden]) { grid-template-columns: 1fr; align-content: start; }
      :host([sidehidden]) aside { display: flex; }
      .resize, .collapse, :host([sidehidden]) .reopen { display: none; }
      main { padding: 18px 16px 48px; }
      :host([chatopen]), :host([chatopen][sidehidden]) { grid-template-columns: 1fr; }
      phx-ai-chat { position: fixed; inset: 0; z-index: 40; height: 100dvh; border-left: 0; }
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
  /** Sidebar width in px (desktop), remembered per browser. */
  sideWidth = readSide("phx.sidebar.width", 260);
  sideHidden = readSide("phx.sidebar.hidden", 0) === 1;
  private unshortcut?: () => void;
  /** Favorite being dragged and the one it hovers. */
  private dragging?: Favorite;
  /** Open context menu of a page or folder in the sidebar. */
  ctx: { x: number; y: number; doc?: Doc; folder?: string; opener?: HTMLElement } | null = null;
  dropTarget = "";
  private loadedFor = "";
  private loadedVersion = -1;
  private loadedLang = "";

  private routes = new Routes(this, [
    { path: "d/:doc/edit", render: ({ doc }) => html`<phx-doc-form .project=${this.slug} .docSlug=${doc!}></phx-doc-form>` },
    { path: "d/:doc/translate/:lang", render: ({ doc, lang }) => html`<phx-translate .project=${this.slug} .docSlug=${doc!} .lang=${lang!}></phx-translate>` },
    { path: "d/:doc/history", render: ({ doc }) => html`<phx-history .project=${this.slug} .docSlug=${doc!}></phx-history>` },
    { path: "trash", render: () => html`<phx-trash .project=${this.slug}></phx-trash>` },
    { path: "d/:doc", render: ({ doc }) => html`<phx-doc-view .project=${this.slug} .docSlug=${doc!}></phx-doc-view>` },
    { path: "new/:type", render: ({ type }) => html`<phx-doc-form .project=${this.slug} .type=${type!}></phx-doc-form>` },
    { path: "e/:entity/edit", render: ({ entity }) => html`<phx-entity-form .project=${this.slug} .entity=${entity!}></phx-entity-form>` },
    { path: "new-entity", render: () => html`<phx-entity-form .project=${this.slug}></phx-entity-form>` },
    // The data model moved to the project settings and the table into the knowledge graph area (ADR-0056).
    { path: "model", render: () => this.redirect("settings/model") },
    { path: "table", render: () => this.redirect("graph") },
    { path: "files", render: () => this.gated("files", html`<phx-project-files .project=${this.slug}></phx-project-files>`) },
    { path: "graph", render: () => this.knowledge("entries") },
    { path: "graph/map", render: () => this.knowledge("map") },
    { path: "import", render: () => html`<phx-import-view .project=${this.slug}></phx-import-view>` },
    { path: "topology", render: () => this.knowledge("topology") },
    { path: "ipam", render: () => this.knowledge("ipam") },
    { path: "household", render: () => this.gated("household", html`<phx-household .project=${this.slug}></phx-household>`) },
    { path: "locations", render: () => this.knowledge("locations") },
    { path: "e/:entity", render: ({ entity }) => html`<phx-entity-view .project=${this.slug} .entity=${entity!}></phx-entity-view>` },
    { path: "decisions", render: () => this.gated("decisions", html`<phx-decisions .project=${this.slug}></phx-decisions>`) },
    { path: "suggestions", render: () => html`<phx-suggestions .project=${this.slug}></phx-suggestions>` },
    { path: "insights", render: () => html`<phx-insights .project=${this.slug}></phx-insights>` },
    { path: "monitoring", render: () => html`<phx-monitoring .project=${this.slug}></phx-monitoring>` },
    { path: "settings", render: () => html`<phx-project-settings .project=${this.slug}></phx-project-settings>` },
    { path: "settings/model", render: () => html`<phx-project-settings .project=${this.slug} section="model"></phx-project-settings>` },
    { path: "settings/rules", render: () => html`<phx-project-settings .project=${this.slug} section="rules"></phx-project-settings>` },
    { path: "settings/code", render: () => html`<phx-project-settings .project=${this.slug} section="code"></phx-project-settings>` },
  ], { fallback: { render: () => this.overview() } });

  async willUpdate(changed: Map<string, unknown>) {
    if (this.root && (changed.has("root") || changed.has("slug"))) await this.routes.goto("");
    const v = docsVersion.get();
    const lang = contentLocale.get();
    if (this.slug && this.slug === this.loadedFor && v === this.loadedVersion && lang !== this.loadedLang) {
      // Another content language: only the titles in the sidebar change.
      this.loadedLang = lang;
      const p = currentProject.get();
      this.docs = (await api<Doc[]>("GET", `/projects/${this.slug}/documents${p?.mainLocale && lang ? `?lang=${lang}` : ""}`).catch(() => this.docs)) ?? this.docs;
    }
    if (this.slug && (this.slug !== this.loadedFor || v !== this.loadedVersion)) {
      this.loadedFor = this.slug;
      this.loadedVersion = v;
      watchProject(this.slug);
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
        this.loadedLang = contentLocale.get();
        this.docs = (await api<Doc[]>("GET", `/projects/${this.slug}/documents${p.mainLocale && this.loadedLang ? `?lang=${this.loadedLang}` : ""}`)) ?? [];
        this.drafts = new Set(((await api<DraftInfo[]>("GET", `/projects/${this.slug}/drafts`).catch(() => [])) ?? []).map((d) => d.documentId));
        favorites.set((await api<Favorite[]>("GET", `/projects/${this.slug}/favorites`).catch(() => [])) ?? []);
        const ai = await api<{ available: boolean; reason?: string }>("GET", `/projects/${this.slug}/ai/status`).catch(() => undefined);
        // Hidden where the chat is switched off; shown with a notice when a budget is used up.
        this.aiChat = !!ai && (ai.available || /budget$/.test(ai.reason ?? ""));
      } catch {
        this.error = t("error.not_found");
      }
    }
  }

  private unlive?: () => void;

  connectedCallback() {
    super.connectedCallback();
    // New, renamed or deleted pages and decisions of others appear at once.
    this.unlive = onProjectChange(LIST_KINDS, () => docsVersion.set(docsVersion.get() + 1));
    this.unshortcut = onShortcut("sidebar", () => this.setSideHidden(!this.sideHidden));
    this.style.setProperty("--side-w", `${this.sideWidth}px`);
    addEventListener("popstate", this.onNavigate);
    addEventListener("keydown", this.onKey);
    addEventListener("click", this.closeNew);
    addEventListener("pointerdown", this.ctxOutside, true);
    addEventListener("resize", this.ctxDismiss);
    addEventListener("blur", this.ctxDismiss);
  }

  private ctxOutside = (e: Event) => {
    if (this.ctx && !e.composedPath().some((n) => n instanceof HTMLElement && n.classList.contains("ctx"))) this.closeCtx();
  };
  private ctxDismiss = () => this.closeCtx();

  disconnectedCallback() {
    super.disconnectedCallback();
    this.unlive?.();
    this.unshortcut?.();
    stopWatching();
    removeEventListener("popstate", this.onNavigate);
    removeEventListener("keydown", this.onKey);
    removeEventListener("click", this.closeNew);
    removeEventListener("pointerdown", this.ctxOutside, true);
    removeEventListener("resize", this.ctxDismiss);
    removeEventListener("blur", this.ctxDismiss);
    this.setMenu(false);
    delete document.documentElement.dataset.pc;
  }

  private setSideHidden(hidden: boolean) {
    this.sideHidden = hidden;
    writeSide("phx.sidebar.hidden", hidden ? 1 : 0);
  }

  private setSideWidth(w: number, save = true) {
    this.sideWidth = Math.round(Math.min(SIDE_MAX, Math.max(SIDE_MIN, w)));
    this.style.setProperty("--side-w", `${this.sideWidth}px`);
    if (save) writeSide("phx.sidebar.width", this.sideWidth);
  }

  private startResize = (e: PointerEvent) => {
    if (e.button !== 0) return;
    const handle = e.currentTarget as HTMLElement;
    const left = (handle.parentElement as HTMLElement).getBoundingClientRect().left;
    handle.setPointerCapture(e.pointerId);
    handle.classList.add("dragging");
    document.documentElement.style.cursor = "col-resize";
    const move = (ev: PointerEvent) => this.setSideWidth(ev.clientX - left, false);
    const end = () => {
      handle.removeEventListener("pointermove", move);
      handle.classList.remove("dragging");
      document.documentElement.style.cursor = "";
      writeSide("phx.sidebar.width", this.sideWidth);
    };
    handle.addEventListener("pointermove", move);
    handle.addEventListener("pointerup", end, { once: true });
    handle.addEventListener("pointercancel", end, { once: true });
    e.preventDefault();
  };

  private resizeKey = (e: KeyboardEvent) => {
    const step = e.shiftKey ? 48 : 16;
    if (e.key === "ArrowLeft") this.setSideWidth(this.sideWidth - step);
    else if (e.key === "ArrowRight") this.setSideWidth(this.sideWidth + step);
    else if (e.key === "Home") this.setSideWidth(260);
    else if (e.key === "Enter") this.setSideHidden(true);
    else return;
    e.preventDefault();
  };

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
          @keydown=${(e: KeyboardEvent) => { if (movable) this.onFavKey(e, f); this.openCtx(e, { doc: d }); }}
          @contextmenu=${(e: MouseEvent) => this.openCtx(e, { doc: d })}
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

  /** The knowledge graph area: its views and the entry pages. */
  private inKnowledge(here: string) {
    const p = `/p/${this.slug}/`;
    return ["graph", "graph/map", "topology", "ipam", "locations", "new-entity"].some((x) => here === p + x) || here.startsWith(`${p}e/`);
  }

  private knowledge(view: KnowledgeView) {
    return this.gated("graph", html`<phx-knowledge .project=${this.slug} .view=${view}></phx-knowledge>`);
  }

  /** Replaces an old address by its new place, keeping the query. */
  private redirect(path: string) {
    const to = `/p/${this.slug}/${path}${location.search}`;
    queueMicrotask(() => {
      history.replaceState(history.state, "", to);
      window.dispatchEvent(new PopStateEvent("popstate"));
    });
    return html``;
  }

  /** Label of the current page for the mobile bar. */
  private where(here: string) {
    const doc = this.docs.find((d) => here.startsWith(`/p/${this.slug}/d/${d.slug}`));
    if (doc) return doc.title;
    if (here.endsWith("/decisions")) return t("nav.decisions");
    if (here.endsWith("/trash")) return t("nav.trash");
    if (this.inKnowledge(here)) return t("nav.graph");
    if (here.endsWith("/files")) return t("nav.files");
    if (here.endsWith("/new-entity")) return t("entity.new");
    if (here.endsWith("/suggestions")) return t("nav.suggestions");
    if (here.endsWith("/monitoring")) return t("monitoring.title");
    if (here.endsWith("/insights")) return t("insights.title");
    if (here.endsWith("/locations")) return t("loc.title");
    if (here.endsWith("/table")) return t("table.title");
    if (here.endsWith("/household")) return t("house.title");
    if (here.endsWith("/ipam")) return t("ipam.title");
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


  /** Right-click (or the context-menu key / Shift+F10) on a page or folder. */
  private openCtx(e: MouseEvent | KeyboardEvent, target: { doc?: Doc; folder?: string }) {
    if (e instanceof KeyboardEvent && !(e.key === "ContextMenu" || (e.key === "F10" && e.shiftKey))) return;
    e.preventDefault();
    const el = e.currentTarget as HTMLElement;
    let x: number, y: number;
    if (e instanceof MouseEvent && (e.clientX || e.clientY)) {
      x = e.clientX;
      y = e.clientY;
    } else {
      const r = el.getBoundingClientRect();
      x = r.left + 16;
      y = r.bottom;
    }
    this.ctx = { x, y, ...target, opener: el };
    this.updateComplete.then(() => {
      const m = this.renderRoot.querySelector<HTMLElement>(".ctx");
      if (!m) return;
      // Keep the menu inside the window.
      const r = m.getBoundingClientRect();
      if (r.right > innerWidth - 8) m.style.left = `${Math.max(8, innerWidth - r.width - 8)}px`;
      if (r.bottom > innerHeight - 8) m.style.top = `${Math.max(8, innerHeight - r.height - 8)}px`;
      m.querySelector<HTMLElement>("button")?.focus();
    });
  }

  private closeCtx = (restoreFocus = false) => {
    if (!this.ctx) return;
    const opener = this.ctx.opener;
    this.ctx = null;
    if (restoreFocus) opener?.focus();
  };

  private onCtxKey(e: KeyboardEvent) {
    const items = [...this.renderRoot.querySelectorAll<HTMLElement>(".ctx button")];
    const i = items.indexOf((this.renderRoot as ShadowRoot).activeElement as HTMLElement);
    if (e.key === "Escape" || e.key === "Tab") {
      e.preventDefault();
      this.closeCtx(true);
    } else if (e.key === "ArrowDown" || e.key === "ArrowUp") {
      e.preventDefault();
      items[(i + (e.key === "ArrowDown" ? 1 : items.length - 1)) % items.length]?.focus();
    } else if (e.key === "Home" || e.key === "End") {
      e.preventDefault();
      items[e.key === "Home" ? 0 : items.length - 1]?.focus();
    }
  }

  private async ctxRun(fn: () => Promise<unknown>, done?: string) {
    this.closeCtx(true);
    try {
      await fn();
      docsVersion.set(docsVersion.get() + 1);
      if (done) toast(done);
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    }
  }

  private renameDoc(d: Doc) {
    const title = prompt(t("ctx.rename_prompt"), d.title)?.trim();
    if (!title || title === d.title) return this.closeCtx(true);
    this.ctxRun(() => api("PATCH", `/projects/${this.slug}/documents/${d.slug}`, { title }), t("settings.saved"));
  }

  private moveDoc(d: Doc) {
    const v = prompt(t("ctx.move_prompt"), d.category ?? "");
    if (v === null) return this.closeCtx(true);
    const category = v.split("/").map((x) => x.trim()).filter(Boolean).join("/");
    if (category === (d.category ?? "")) return this.closeCtx(true);
    this.ctxRun(() => api("PATCH", `/projects/${this.slug}/documents/${d.slug}`, { category }), t("settings.saved"));
  }

  private renameFolder(path: string) {
    const name = path.slice(path.lastIndexOf("/") + 1);
    const v = prompt(t("ctx.rename_folder_prompt"), name)?.trim().replaceAll("/", "-");
    if (!v || v === name) return this.closeCtx(true);
    const to = path.includes("/") ? `${path.slice(0, path.lastIndexOf("/"))}/${v}` : v;
    const affected = this.docs.filter((d) => d.type !== "adr" && d.category && (d.category === path || d.category.startsWith(`${path}/`)));
    this.ctxRun(async () => {
      for (const d of affected) await api("PATCH", `/projects/${this.slug}/documents/${d.slug}`, { category: to + d.category!.slice(path.length) });
    }, t("settings.saved"));
  }

  private async trashDoc(d: Doc) {
    if (!confirm(`${d.title}: ${t("doc.delete_confirm")}`)) return this.closeCtx(true);
    const open = location.pathname.startsWith(`/p/${this.slug}/d/${d.slug}`);
    const slug = this.slug;
    await this.ctxRun(() => api("DELETE", `/projects/${slug}/documents/${d.slug}`));
    if (open) navigate(`/p/${slug}`);
    toast(t("doc.trashed", { title: d.title }), "ok", {
      label: t("doc.undo"),
      run: () => this.ctxRun(() => api("POST", `/projects/${slug}/trash/${d.id}/restore`)),
    });
  }

  private copyLink(d: Doc) {
    this.closeCtx(true);
    const url = `${location.origin}/p/${this.slug}/d/${d.slug}`;
    navigator.clipboard?.writeText(url).then(() => toast(t("ctx.link_copied")), () => toast(url));
  }

  private ctxMenu() {
    const c = this.ctx;
    if (!c) return "";
    const canEdit = ["editor", "owner"].includes(currentProject.get()?.role ?? "");
    const item = (ic: string, label: string, run: () => void, cls = "") =>
      html`<button role="menuitem" class=${cls} @click=${run}>${icon(ic as any, 15)} ${label}</button>`;
    let items: unknown;
    if (c.doc) {
      const d = c.doc;
      const favs = favorites.get();
      const mine = favs.some((f) => f.personal && f.documentId === d.id);
      const pinned = favs.some((f) => !f.personal && f.documentId === d.id);
      const fav = (personal: boolean, on: boolean) =>
        this.ctxRun(() => api(on ? "PUT" : "DELETE", `/projects/${this.slug}/favorites/${d.id}?scope=${personal ? "me" : "project"}`));
      const external = d.type === "link" && d.content?.url;
      items = html`
        ${item("ExternalLink", t("ctx.open_tab"), () => { this.closeCtx(); open(external ? d.content.url : `/p/${this.slug}/d/${d.slug}`, "_blank", "noopener"); })}
        ${external ? "" : item("Link", t("ctx.copy_link"), () => this.copyLink(d))}
        <hr>
        ${item("Star", t(mine ? "fav.remove_me" : "fav.add_me"), () => fav(true, !mine))}
        ${canEdit ? item("Pin", t(pinned ? "fav.remove_project" : "fav.add_project"), () => fav(false, !pinned)) : ""}
        ${canEdit ? html`<hr>
          ${item("Pencil", t("ctx.rename"), () => this.renameDoc(d))}
          ${external ? "" : item("SquarePen", t("ctx.edit"), () => { this.closeCtx(); navigate(`/p/${this.slug}/d/${d.slug}/edit`); })}
          ${item("FolderInput", t("ctx.move"), () => this.moveDoc(d))}
          <hr>
          ${item("Trash2", t("ctx.delete"), () => this.trashDoc(d), "danger")}` : ""}`;
    } else {
      const path = c.folder ?? "";
      const open = !this.collapsed.has(path);
      items = html`
        ${canEdit ? html`${item("FilePlus", t("ctx.new_page"), () => { this.closeCtx(); this.create(path, "type"); })}
          ${item("FolderPlus", t("ctx.new_folder"), () => { this.closeCtx(); this.create(path); })}` : ""}
        ${path ? item(open ? "ChevronsDownUp" : "ChevronsUpDown", t(open ? "ctx.collapse" : "ctx.expand"), () => { this.closeCtx(true); this.toggleCategory(path); }) : ""}
        ${canEdit && path ? html`<hr>${item("Pencil", t("ctx.rename_folder"), () => this.renameFolder(path))}` : ""}`;
    }
    return html`<div class="ctx" role="menu" aria-label=${c.doc ? c.doc.title : c.folder || t("nav.uncategorized")}
      style="left:${c.x}px;top:${c.y}px" @keydown=${this.onCtxKey} @contextmenu=${(e: Event) => e.preventDefault()}>${items}</div>`;
  }

  private docLink(d: Doc, here: string) {
    return d.type === "link" && d.content?.url
      ? html`<a href=${d.content.url} target="_blank" rel="noopener external" title=${t("nav.external")} @contextmenu=${(e: MouseEvent) => this.openCtx(e, { doc: d })} @keydown=${(e: KeyboardEvent) => this.openCtx(e, { doc: d })}>${icon("Link", 15)} ${d.title}<span class="ext">${icon("ExternalLink", 13)}</span></a>`
      : html`<a href="/p/${this.slug}/d/${d.slug}" class=${here.startsWith(`/p/${this.slug}/d/${d.slug}`) ? "on" : ""} @contextmenu=${(e: MouseEvent) => this.openCtx(e, { doc: d })} @keydown=${(e: KeyboardEvent) => this.openCtx(e, { doc: d })}>${icon(typeIcon[d.type], 15)} ${d.title}${this.drafts.has(d.id) ? html`<span class="ext" title=${t("draft.sidebar")} aria-label=${t("draft.sidebar")}>${icon("FilePen", 11)}</span>` : ""}${d.restricted ? html`<span class="ext" title=${t("share.restricted")}>${icon("Lock", 11)}</span>` : ""}</a>`;
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
          <button class="fold" aria-expanded=${open ? "true" : "false"} aria-controls=${id} @click=${() => this.toggleCategory(n.path)}
            @contextmenu=${(e: MouseEvent) => this.openCtx(e, { folder: n.path })} @keydown=${(e: KeyboardEvent) => this.openCtx(e, { folder: n.path })}>${icon("ChevronDown", 12)}<span>${n.name}</span></button>
          ${canEdit ? html`<button class="add" title=${t("create.here", { category: n.name })} aria-label=${t("create.here", { category: n.name })} @click=${() => this.create(n.path)}>${icon("Plus", 13)}</button>` : ""}
        </div>
        ${open ? html`<div id=${id} class="grp">${n.docs.map((d) => this.docLink(d, here))}${n.children.map((c) => group(c, depth + 1))}</div>` : ""}
      </div>`;
    };
    return html`${root.children.map((c) => group(c, 0))}
      ${uncategorized.length ? html`<div class="grp">
        <div class="lbl" @contextmenu=${(e: MouseEvent) => this.openCtx(e, { folder: "" })}><span style="flex:1">${t("nav.uncategorized")}</span>
          ${canEdit ? html`<button class="add" title=${t("create.here", { category: t("nav.uncategorized") })} aria-label=${t("create.here", { category: t("nav.uncategorized") })} @click=${() => this.create("")}>${icon("Plus", 13)}</button>` : ""}</div>
        ${uncategorized.map((d) => this.docLink(d, here))}</div>` : ""}`;
  }

  aiChat = false;
  chatOpen = readSide("phx.aichat", 0) === 1;

  private setChat(open: boolean) {
    this.chatOpen = open;
    writeSide("phx.aichat", open ? 1 : 0);
  }

  /** Slug of the page that is open (context for the AI chat). */
  private openDoc(here: string): string {
    const prefix = `/p/${this.slug}/d/`;
    return here.startsWith(prefix) ? decodeURIComponent(here.slice(prefix.length).split("/")[0]) : "";
  }

  render() {
    this.toggleAttribute("focus", focusMode.get());
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
        ${this.aiChat && !docOnly ? html`<button class="ghost chatbtn" aria-label=${t("ai_chat.title")} aria-pressed=${this.chatOpen ? "true" : "false"} @click=${() => this.setChat(!this.chatOpen)}>${icon("BotMessageSquare", 16)}</button>` : ""}
      </div>
      <div class=${this.menuOpen ? "scrim open" : "scrim"} @click=${() => this.setMenu(false, true)}></div>
      <button class="reopen" aria-controls="nav" aria-expanded="false" title=${`${t("nav.sidebar_show")}${comboFor("sidebar") ? ` (${comboLabel(comboFor("sidebar"))})` : ""}`}
        aria-label=${t("nav.sidebar_show")} @click=${() => this.setSideHidden(false)}>${icon("PanelLeftOpen", 16)}</button>
      <aside id="nav" class=${this.menuOpen ? "open" : ""} aria-label=${t("nav.menu")} @click=${this.onAsideClick}>
        <div class="resize" role="separator" aria-orientation="vertical" tabindex="0" aria-label=${t("nav.sidebar_resize")}
          aria-valuemin=${SIDE_MIN} aria-valuemax=${SIDE_MAX} aria-valuenow=${this.sideWidth}
          @pointerdown=${this.startResize} @keydown=${this.resizeKey} @dblclick=${() => this.setSideWidth(260)}></div>
        <div class="scroll" @scroll=${this.ctxDismiss}>
          ${this.favoritesGroup(here)}
          <div class="grp"><a href="/p/${this.slug}" class=${here === `/p/${this.slug}` ? "on" : ""}>${icon("House", 15)} ${t("project.overview")}</a>
            ${!hasFeature(proj, "decisions") ? "" : html`<a href="/p/${this.slug}/decisions" class=${here === `/p/${this.slug}/decisions` || this.docs.some((d) => d.type === "adr" && here.startsWith(`/p/${this.slug}/d/${d.slug}`)) ? "on" : ""}>${icon("ListChecks", 15)} ${t("nav.decisions")}
              ${decisions ? html`<span class="num">${decisions}</span>` : ""}</a>`}
            ${docOnly || !hasFeature(proj, "graph") ? "" : html`<a href="/p/${this.slug}/graph" class=${this.inKnowledge(here) ? "on" : ""}>${icon("Waypoints", 15)} ${t("nav.graph")}</a>`}
            ${docOnly || !hasFeature(proj, "files") ? "" : html`<a href="/p/${this.slug}/files" class=${here === `/p/${this.slug}/files` ? "on" : ""}>${icon("Paperclip", 15)} ${t("nav.files")}</a>`}</div>
          ${this.tree(here, canEdit)}
        </div>
        <div class="tools">
          ${canEdit ? html`<div class="new">
            <button class="ghost" aria-expanded=${this.newOpen ? "true" : "false"} aria-haspopup="menu" @click=${(e: Event) => { e.stopPropagation(); this.newOpen = !this.newOpen; }}>${icon("Plus", 15)} ${t("create.new")}</button>
            ${this.newOpen ? html`<div class="menu" role="menu" @keydown=${(e: KeyboardEvent) => e.key === "Escape" && (this.newOpen = false)}>
              <a role="menuitem" href="#" @click=${(e: Event) => { e.preventDefault(); this.create("", "type"); }}>${icon("FilePlus", 15)} ${t("create.page")}</a>
              <a role="menuitem" href="#" @click=${(e: Event) => { e.preventDefault(); this.create(""); }}>${icon("FolderPlus", 15)} ${t("create.folder")}</a>
              <a role="menuitem" href="/p/${this.slug}/import" @click=${() => (this.newOpen = false)}>${icon("Upload", 15)} ${t("import.title")}</a>
              ${hasFeature(proj, "decisions") ? html`<a role="menuitem" href="/p/${this.slug}/new/adr" @click=${() => (this.newOpen = false)}>${icon("Scale", 15)} ${t("doc.type.adr")}</a>` : ""}
              ${hasFeature(proj, "graph") || hasFeature(proj, "model") ? html`<a role="menuitem" href="/p/${this.slug}/new-entity" @click=${() => (this.newOpen = false)}>${icon("Plus", 15)} ${t("entity.new")}</a>` : ""}
            </div>` : ""}
          </div>
          <a href="/p/${this.slug}/trash" class=${here.endsWith("/trash") ? "on" : ""}>${icon("Trash2", 15)} ${t("nav.trash")}</a>` : ""}
          ${docOnly || !hasFeature(proj, "household") ? "" : html`<a href="/p/${this.slug}/household" class=${here.endsWith("/household") ? "on" : ""}>${icon("House", 15)} ${t("house.title")}</a>`}
          ${this.aiChat && !docOnly ? html`<button class="collapse" aria-pressed=${this.chatOpen ? "true" : "false"} @click=${() => this.setChat(!this.chatOpen)}>${icon("BotMessageSquare", 15)} ${t("ai_chat.title")}</button>` : ""}
          ${docOnly ? "" : html`<a href="/p/${this.slug}/insights" class=${here.endsWith("/insights") ? "on" : ""}>${icon("Sparkles", 15)} ${t("insights.title")}</a>
          <a href="/p/${this.slug}/monitoring" class=${here.endsWith("/monitoring") ? "on" : ""}>${icon("Activity", 15)} ${t("monitoring.title")}</a>
          <a href="/p/${this.slug}/suggestions" class=${here.endsWith("/suggestions") ? "on" : ""}>${icon("Bot", 15)} ${t("nav.suggestions")}</a>
          <a href="/p/${this.slug}/settings" class=${here.endsWith("/settings") ? "on" : ""}>${icon("Settings", 15)} ${t("nav.settings")}</a>`}
          <button class="collapse" aria-controls="nav" aria-expanded="true" @click=${() => this.setSideHidden(true)}>${icon("PanelLeftClose", 15)} ${t("nav.sidebar_hide")}
            ${comboFor("sidebar") ? html`<kbd>${comboLabel(comboFor("sidebar"))}</kbd>` : ""}</button>
        </div>
        <phx-create-dialog .project=${this.slug}></phx-create-dialog>
        ${this.ctxMenu()}
      </aside>
      <main>${this.routes.outlet()}</main>
      ${this.aiChat && this.chatOpen && !docOnly ? html`<phx-ai-chat .project=${this.slug} .doc=${this.openDoc(here)} @close=${() => this.setChat(false)}></phx-ai-chat>` : ""}
    `;
  }
}
customElements.define("phx-project", PhxProject);

const SIDE_MIN = 200;
const SIDE_MAX = 520;

function readSide(key: string, fallback: number): number {
  try {
    const n = Number(localStorage.getItem(key));
    return localStorage.getItem(key) !== null && Number.isFinite(n) ? n : fallback;
  } catch {
    return fallback;
  }
}

function writeSide(key: string, value: number) {
  try {
    localStorage.setItem(key, String(value));
  } catch { /* only for this page then */ }
}
