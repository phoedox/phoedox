import { SignalWatcher } from "@lit-labs/signals";
import { LitElement, css, html } from "lit";
import { ApiError, api, type Org, type Project } from "../api";
import { continueTiles, matchProjects, relativeTime, type Tile } from "../home";
import { locale, t } from "../i18n";
import { icon } from "../icon";
import { appOrg, singleOrg, currentProject, currentUser, navigate, projectsPath, roleKey, setShowAllProjects, showAllProjects } from "../state";
import "./trashed-projects";
import { base } from "./shared";
import { projectBadge } from "../pictures";
import "./project-transfer";
import { recentHref, recentIcon, type RecentItem } from "../recent";

interface Template { id: string; name: string; description: string; icon: string }
interface Stats { documents: number; openTasks: number; failingChecks: number; suggestions: number; lastActivity: string | null }
interface Home { stats: Record<string, Stats>; pinned: string[]; dueTasks: number; suggestions: { project: string; name: string; count: number }[] }
type Layout = "grid" | "list";

const LAYOUT_KEY = "phx.projects.layout";

/** A number with an icon; the label (also for screen readers) says what it counts. */
const chip = (kind: string, ico: string, n: number, label: string) =>
  html`<span class=${`chip ${kind}`} title=${label} aria-label=${label}>${icon(ico, 12)} ${n}</span>`;
const readLayout = (): Layout => {
  try {
    return localStorage.getItem(LAYOUT_KEY) === "list" ? "list" : "grid";
  } catch {
    return "grid";
  }
};

/** Start page: what to continue with, then the projects with their numbers.
 * Creating and importing live in a dialog. */
export class PhxProjects extends SignalWatcher(LitElement) {
  static properties = {
    recent: { state: true }, projects: { state: true }, orgs: { state: true }, customerOrgs: { state: true }, templates: { state: true }, error: { state: true },
    home: { state: true }, q: { state: true }, layout: { state: true }, tab: { state: true }, loaded: { state: true }, busy: { state: true },
  };
  static styles = [base, css`
    :host { display: block; max-width: 1080px; margin: 0 auto; padding: 28px 16px; }
    .top { display: flex; gap: 10px; align-items: center; flex-wrap: wrap; margin-bottom: 18px; }
    .top h1 { margin: 0; flex: 1 1 auto; }
    .search { flex: 1 1 220px; max-width: 340px; position: relative; display: block; }
    .search input[type="search"] { width: 100%; padding-left: 36px; }
    .search .sicon { position: absolute; left: 11px; pointer-events: none; top: 50%; transform: translateY(-50%); color: var(--muted); display: inline-flex; }
    .seg { display: inline-flex; border: 1px solid var(--line); border-radius: 8px; overflow: hidden; }
    .seg button { border: 0; border-radius: 0; background: none; padding: 6px 9px; color: var(--muted); }
    .seg button[aria-pressed="true"] { background: var(--accent-soft); color: var(--fg); }
    h2.sec { font-size: 12px; font-weight: 600; letter-spacing: .06em; text-transform: uppercase; color: var(--muted); display: flex; gap: 8px; align-items: center; margin: 22px 0 8px; }
    h2.sec a { color: inherit; text-decoration: none; }
    h2.sec .n { text-transform: none; letter-spacing: 0; font-weight: 500; }
    .tiles { display: grid; grid-template-columns: repeat(auto-fill, minmax(210px, 1fr)); gap: 8px; }
    /* Without a heading in between, the projects must not stick to the tiles. */
    .tiles + .grid, .tiles + .list { margin-top: 24px; }
    .tile { display: flex; gap: 10px; align-items: center; padding: 9px 12px; text-decoration: none; color: var(--fg); min-width: 0; }
    .tile > span { min-width: 0; }
    .tile b, .tile small { display: block; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
    .tile small { color: var(--muted); font-size: 12.5px; }
    .tile .ico { color: var(--muted); display: inline-flex; }
    .tile.due .ico { color: var(--signal); }
    .tile.suggestions .ico { color: var(--accent); }
    .grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(250px, 1fr)); gap: 12px; }
    .pcard { position: relative; display: grid; grid-template-rows: auto 1fr auto; gap: 10px; padding: 14px; min-width: 0;
      transition: border-color var(--dur-fast) ease; }
    @media (hover: hover) and (pointer: fine) { .pcard:hover { border-color: var(--muted); } }
    .pcard .head { display: flex; gap: 10px; align-items: center; min-width: 0; }
    .pcard .head > span { min-width: 0; flex: 1; }
    a.name { color: var(--fg); text-decoration: none; font-weight: 600; display: block; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    a.name::after { content: ""; position: absolute; inset: 0; border-radius: inherit; }
    a.name:focus-visible { outline: none; }
    .pcard:has(a.name:focus-visible) { outline: 2px solid var(--accent); outline-offset: 2px; }
    .role { font-size: 12px; color: var(--muted); }
    .role.admin { color: var(--signal); display: inline-flex; gap: 4px; align-items: center; }
    .allsw { display: inline-flex; gap: 6px; align-items: center; font-size: 13.5px; color: var(--muted); margin: -6px 0 14px; cursor: pointer; }
    .allsw input { width: auto; margin: 0; }
    .desc { margin: 0; font-size: 13.5px; color: var(--muted); display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden; min-height: 2.6em; }
    .desc a { position: relative; z-index: 1; }
    .stats { display: flex; gap: 6px; flex-wrap: wrap; border-top: 1px solid var(--line); padding-top: 10px; font-size: 12px; }
    .chip { display: inline-flex; gap: 4px; align-items: center; border-radius: 999px; padding: 1px 8px; background: var(--surface2); color: var(--muted); white-space: nowrap; }
    .chip.warn { background: var(--accent-soft); color: var(--fg); }
    .chip.bad { color: var(--bad); }
    .pin { position: relative; z-index: 1; background: none; border: 0; padding: 4px; color: var(--muted); display: inline-flex; border-radius: 6px; }
    .pin[aria-pressed="true"] { color: var(--accent); }
    .pin[aria-pressed="true"] svg { fill: currentColor; }
    .list { display: grid; border: 1px solid var(--line); border-radius: 10px; overflow: hidden; background: var(--surface); }
    .lrow { position: relative; display: grid; grid-template-columns: auto minmax(0, 1fr) auto auto; gap: 12px; align-items: center; padding: 9px 12px; border-top: 1px solid var(--line); }
    .lrow:first-child { border-top: 0; }
    .lrow .stats { border: 0; padding: 0; }
    .empty { display: grid; gap: 12px; justify-items: start; padding: 28px; border: 1px dashed var(--line); border-radius: 12px; }
    dialog { width: min(680px, calc(100vw - 32px)); border: 1px solid var(--line); border-radius: 14px; background: var(--surface); color: var(--fg); padding: 20px;
      box-shadow: 0 18px 48px rgb(0 0 0 / .25); }
    dialog::backdrop { background: rgb(0 0 0 / .45); }
    @media (max-width: 560px) { .tiles .tile:nth-child(n+3) { display: none; } }
    dialog .tabs { display: flex; gap: 4px; margin-bottom: 14px; box-shadow: inset 0 -1px var(--line); }
    dialog .tabs button { border: 0; border-radius: 0; background: none; padding: 8px 12px; color: var(--muted); }
    dialog .tabs button[aria-selected="true"] { color: var(--fg); box-shadow: inset 0 -2px var(--accent); }
    form.new { display: grid; gap: 12px; }
    fieldset.tpl { border: 0; padding: 0; margin: 0; display: grid; grid-template-columns: repeat(auto-fill, minmax(190px, 1fr)); gap: 8px; }
    fieldset.tpl legend { font-size: 13px; font-weight: 600; margin-bottom: 6px; }
    fieldset.tpl label { position: relative; cursor: pointer; }
    fieldset.tpl input { position: absolute; opacity: 0; }
    fieldset.tpl span { display: grid; gap: 2px; padding: 10px 12px; border: 1px solid var(--line); border-radius: 10px; background: var(--surface); height: 100%; box-sizing: border-box; font-size: 14px; }
    fieldset.tpl span .ico { color: var(--accent); }
    fieldset.tpl small { color: var(--muted); font-size: 12.5px; }
    fieldset.tpl input:checked + span { border-color: var(--accent); box-shadow: 0 0 0 1px var(--accent); }
    fieldset.tpl input:focus-visible + span { outline: 2px solid var(--accent); outline-offset: 2px; }
    .two { display: grid; grid-template-columns: repeat(auto-fit, minmax(200px, 1fr)); gap: 10px; }
    .actions { display: flex; gap: 8px; justify-content: flex-end; flex-wrap: wrap; }
    @media (max-width: 560px) { .lrow { grid-template-columns: auto minmax(0, 1fr) auto; } .lrow .stats { grid-column: 2 / -1; } }
  `];
  projects: Project[] = [];
  recent: RecentItem[] = [];
  orgs: Org[] = [];
  /** Customers where one of my organizations is service provider with the admin role (ADR-0073). */
  customerOrgs: { slug: string; name: string }[] = [];
  templates: Template[] = [];
  home?: Home;
  error = "";
  q = "";
  layout: Layout = readLayout();
  tab: "new" | "import" = "new";
  loaded = false;
  busy = false;

  private toggleAll = async (e: Event) => {
    setShowAllProjects((e.target as HTMLInputElement).checked);
    this.projects = (await api<Project[]>("GET", projectsPath()).catch(() => [])) ?? [];
  };

  private async loadCustomers() {
    type L = { customerSlug: string; customerName: string; role: string; projects: string[]; revokedAt?: string; expiresAt?: string };
    const seen = new Set(this.orgs.map((o) => o.slug));
    const out: { slug: string; name: string }[] = [];
    for (const o of this.orgs) {
      const links = await api<{ customers: L[] }>("GET", `/orgs/${o.slug}/providers`).catch(() => undefined);
      for (const l of links?.customers ?? []) {
        const active = !l.revokedAt && (!l.expiresAt || Date.parse(l.expiresAt) > Date.now());
        if (active && l.role === "admin" && !l.projects.length && !seen.has(l.customerSlug)) {
          seen.add(l.customerSlug);
          out.push({ slug: l.customerSlug, name: l.customerName });
        }
      }
    }
    this.customerOrgs = out;
  }

  async connectedCallback() {
    super.connectedCallback();
    currentProject.set(null);
    const [projects, orgs, recent, home] = await Promise.all([
      api<Project[]>("GET", projectsPath()).catch(() => []),
      api<Org[]>("GET", "/orgs").catch(() => []),
      api<RecentItem[]>("GET", "/me/recent?limit=6").catch(() => []),
      api<Home>("GET", "/me/home").catch(() => undefined),
    ]);
    this.projects = projects ?? [];
    this.orgs = orgs ?? [];
    void this.loadCustomers();
    this.recent = recent ?? [];
    this.home = home;
    this.loaded = true;
    if (new URLSearchParams(location.search).get("new") === "1") this.openDialog();
  }

  private async loadTemplates() {
    if (!this.templates.length) this.templates = (await api<Template[]>("GET", `/templates?lang=${locale()}`).catch(() => [])) ?? [];
  }

  private openDialog(tab: "new" | "import" = "new") {
    this.tab = tab;
    this.error = "";
    void this.loadTemplates();
    this.updateComplete.then(() => this.renderRoot.querySelector<HTMLDialogElement>("dialog")?.showModal());
  }

  private closeDialog() {
    this.renderRoot.querySelector<HTMLDialogElement>("dialog")?.close();
  }

  private setLayout(l: Layout) {
    this.layout = l;
    try {
      localStorage.setItem(LAYOUT_KEY, l);
    } catch { /* only for this visit */ }
  }

  private async create(e: SubmitEvent) {
    e.preventDefault();
    const f = new FormData(e.target as HTMLFormElement);
    this.busy = true;
    try {
      const p = await api<Project>("POST", "/projects", { name: f.get("name"), description: f.get("description"), org: f.get("org") || undefined,
        template: f.get("template") || undefined, templateLocale: locale() });
      navigate(`/p/${p.slug}`);
    } catch (err) {
      this.error = err instanceof ApiError ? err.text : t("error.unknown");
    } finally {
      this.busy = false;
    }
  }

  private async togglePin(e: Event, p: Project) {
    e.preventDefault();
    e.stopPropagation();
    if (!this.home) return;
    const pinned = this.home.pinned.includes(p.id) ? this.home.pinned.filter((x) => x !== p.id) : [...this.home.pinned, p.id];
    this.home = { ...this.home, pinned };
    try {
      const res = await api<{ pinned: string[] }>("PUT", "/me/pinned", { projects: pinned });
      this.home = { ...this.home, pinned: res.pinned };
    } catch { /* keeps the local state */ }
  }

  /** Projects by organization; personal projects first. Without organizations no headings. */
  private groups(list: Project[]): [string, string, Project[]][] {
    const orgOf = new Map(this.orgs.map((o) => [o.id, o]));
    const by = new Map<string, Project[]>();
    for (const p of list) by.set(p.orgId ?? "", [...(by.get(p.orgId ?? "") ?? []), p]);
    if (by.size === 1 && by.has("")) return [["", "", list]];
    return [...by].sort(([a], [b]) => (a === "" ? -1 : b === "" ? 1 : 0)).map(([id, ps]) =>
      [id ? orgOf.get(id)?.name ?? ps[0].orgName ?? "" : t("projects.personal"), id ? orgOf.get(id)?.slug ?? "" : "", ps]);
  }

  private stats(p: Project) {
    const s = this.home?.stats[p.id];
    if (!s) return "";
    return html`<span class="stats">
      ${chip("", "FileText", s.documents, t("projects.stat_pages", { n: s.documents }))}
      ${s.openTasks ? chip("warn", "SquareKanban", s.openTasks, t("projects.stat_tasks", { n: s.openTasks })) : ""}
      ${s.failingChecks ? chip("bad", "CircleAlert", s.failingChecks, t("projects.stat_checks", { n: s.failingChecks })) : ""}
      ${s.suggestions ? chip("warn", "Bot", s.suggestions, t("projects.stat_suggestions", { n: s.suggestions })) : ""}
      ${s.lastActivity ? html`<span class="chip">${icon("Clock", 12)} ${relativeTime(s.lastActivity, Date.now(), locale())}</span>` : ""}
    </span>`;
  }

  private pinButton(p: Project) {
    if (!this.home) return "";
    const on = this.home.pinned.includes(p.id);
    return html`<button class="pin" aria-pressed=${on} aria-label=${t(on ? "projects.unpin" : "projects.pin", { name: p.name })} title=${t(on ? "projects.unpin" : "projects.pin", { name: p.name })}
      @click=${(e: Event) => this.togglePin(e, p)}>${icon("Star", 15)}</button>`;
  }

  private desc(p: Project) {
    if (p.description) return p.description;
    if (p.role === "owner" && !p.adminAccess) return html`<a href="/p/${p.slug}/settings">${t("projects.add_description")}</a>`;
    return "";
  }

  private card(p: Project) {
    return html`<article class="pcard card">
      <div class="head">${projectBadge(p, 36)}
        <span><a class="name" href="/p/${p.slug}">${p.name}</a><span class=${p.adminAccess ? "role admin" : "role"}>${p.adminAccess ? icon("ShieldCheck", 11) : ""}${t(roleKey(p) as any)}</span></span>
        ${this.pinButton(p)}</div>
      <p class="desc">${this.desc(p)}</p>
      ${this.stats(p)}
    </article>`;
  }

  private row(p: Project) {
    return html`<div class="lrow">${projectBadge(p, 28)}
      <span style="min-width:0"><a class="name" href="/p/${p.slug}">${p.name}</a></span>
      ${this.stats(p)}${this.pinButton(p)}</div>`;
  }

  private section(title: unknown, ps: Project[]) {
    if (!ps.length) return "";
    return html`${title ? html`<h2 class="sec">${title} <span class="n">${ps.length}</span></h2>` : ""}
      ${this.layout === "grid" ? html`<div class="grid">${ps.map((p) => this.card(p))}</div>` : html`<div class="list">${ps.map((p) => this.row(p))}</div>`}`;
  }

  private continueRow() {
    const recent: Tile[] = this.recent.map((r) => ({ kind: "recent", href: recentHref(r), title: r.title, sub: r.projectName, icon: recentIcon(r) }));
    const tiles = continueTiles(this.home?.dueTasks ?? 0, this.home?.suggestions ?? [], recent, {
      due: (n) => t("projects.due_tasks", { n }), dueSub: t("mytasks.title"), sugg: (n) => t("projects.open_suggestions", { n }),
    });
    if (!tiles.length) return "";
    return html`<h2 class="sec">${t("projects.continue")} <a class="n" href="/history">${t("projects.history")}</a></h2>
      <div class="tiles">${tiles.map((x) => html`<a class=${`tile card ${x.kind}`} href=${x.href}>${icon(x.icon, 16)}<span><b>${x.title}</b><small>${x.sub}</small></span></a>`)}</div>`;
  }

  private dialog() {
    return html`<dialog aria-labelledby="new-title" @close=${() => (this.error = "")}>
      <h2 id="new-title" style="margin:0 0 10px;font-size:20px">${t("projects.new")}</h2>
      <div class="tabs" role="tablist">
        <button role="tab" aria-selected=${this.tab === "new"} @click=${() => (this.tab = "new")}>${icon("Plus", 14)} ${t("projects.tab_new")}</button>
        <button role="tab" aria-selected=${this.tab === "import"} @click=${() => (this.tab = "import")}>${icon("ArrowRightLeft", 14)} ${t("transfer.title")}</button>
      </div>
      ${this.tab === "new" ? html`<form class="new" @submit=${this.create}>
        <fieldset class="tpl"><legend>${t("projects.template_question")}</legend>
          ${this.templates.map((tp, i) => html`<label><input type="radio" name="template" value=${tp.id} ?checked=${i === 0}> <span>${icon(tp.icon, 18)}<b>${tp.name}</b><small>${tp.description}</small></span></label>`)}
          <label><input type="radio" name="template" value="" ?checked=${!this.templates.length}> <span>${icon("FilePlus", 18)}<b>${t("projects.template_empty")}</b><small>${t("projects.template_empty_desc")}</small></span></label>
        </fieldset>
        <div class="two">
          <label class="field">${t("projects.name")}<input name="name" required autocomplete="off"></label>
          ${this.orgs.length && !singleOrg.get() ? html`<label class="field">${t("projects.org")}<select name="org">
            <option value="">${t("projects.personal")}</option>
            ${this.orgs.map((o) => html`<option value=${o.slug} ?selected=${appOrg.get()?.slug === o.slug}>${o.name}</option>`)}
            ${this.customerOrgs.length ? html`<optgroup label=${t("prov.customers")}>${this.customerOrgs.map((o) => html`<option value=${o.slug}>${o.name}</option>`)}</optgroup>` : ""}</select></label>` : ""}
        </div>
        <label class="field">${t("projects.description_optional")}<input name="description" autocomplete="off"></label>
        ${this.error ? html`<p class="error" role="alert">${this.error}</p>` : ""}
        <div class="actions"><button type="button" class="ghost" @click=${this.closeDialog}>${t("doc.cancel")}</button><button ?disabled=${this.busy}>${t("projects.create")}</button></div>
      </form>` : html`<phx-project-import></phx-project-import>
        <div class="actions" style="margin-top:12px"><button type="button" class="ghost" @click=${this.closeDialog}>${t("board.close")}</button></div>`}
    </dialog>`;
  }

  render() {
    const visible = matchProjects(this.projects, this.q);
    const pinned = new Set(this.home?.pinned ?? []);
    const pinnedList = (this.home?.pinned ?? []).map((id) => visible.find((p) => p.id === id)).filter((p): p is Project => !!p);
    const rest = visible.filter((p) => !pinned.has(p.id));
    const groups = this.groups(rest);
    const org = appOrg.get();
    const main = org && groups.length > 1 ? groups.filter((g) => g[1] === org.slug) : groups;
    const other = org && groups.length > 1 ? groups.filter((g) => g[1] !== org.slug) : [];
    return html`<div class="enter">
      <div class="top">
        <h1>${t("projects.title")}</h1>
        ${this.projects.length > 3 ? html`<label class="search"><span class="sicon">${icon("Search", 15)}</span>
          <input type="search" aria-label=${t("projects.search")} placeholder=${t("projects.search")} .value=${this.q} @input=${(e: Event) => (this.q = (e.target as HTMLInputElement).value)}></label>` : ""}
        ${this.projects.length ? html`<span class="seg" role="group" aria-label=${t("projects.layout")}>
          <button aria-pressed=${this.layout === "grid"} aria-label=${t("projects.layout_grid")} title=${t("projects.layout_grid")} @click=${() => this.setLayout("grid")}>${icon("LayoutGrid", 15)}</button>
          <button aria-pressed=${this.layout === "list"} aria-label=${t("projects.layout_list")} title=${t("projects.layout_list")} @click=${() => this.setLayout("list")}>${icon("List", 15)}</button></span>` : ""}
        <button @click=${() => this.openDialog()}>${icon("Plus", 15)} ${t("projects.new")}</button>
      </div>
      ${currentUser.get()?.isAdmin ? html`<label class="allsw" title=${t("projects.all_admin_hint")}><input type="checkbox" .checked=${showAllProjects()} @change=${this.toggleAll}> ${icon("ShieldCheck", 14)} ${t("projects.all_admin")}</label>` : ""}
      ${this.continueRow()}
      ${this.loaded && this.projects.length === 0 ? html`<div class="empty"><b>${t("projects.empty_title")}</b><span class="muted">${t("projects.empty")}</span>
        <div class="actions" style="justify-content:flex-start"><button @click=${() => this.openDialog()}>${icon("Plus", 15)} ${t("projects.new")}</button>
        <button class="ghost" @click=${() => this.openDialog("import")}>${icon("ArrowRightLeft", 15)} ${t("transfer.title")}</button></div></div>` : ""}
      ${this.q && visible.length === 0 ? html`<p class="muted">${t("projects.no_match")}</p>` : ""}
      ${this.section(pinnedList.length ? html`${icon("Star", 13)} ${t("projects.pinned")}` : "", pinnedList)}
      ${main.map(([name, slug, ps]) => this.section(name ? html`${icon(slug ? "Building2" : "UserRound", 13)} ${slug ? html`<a href="/orgs/${slug}">${name}</a>` : name}` : pinnedList.length || this.recent.length ? t("projects.all") : "", ps))}
      ${other.length ? html`<details style="margin-top:18px"><summary style="cursor:pointer;font-weight:600">${t("projects.other")}</summary>
        ${other.map(([name, slug, ps]) => this.section(html`${icon("Building2", 13)} <a href="/orgs/${slug}">${name}</a>`, ps))}</details>` : ""}
      <phx-trashed-projects></phx-trashed-projects>
      ${this.dialog()}
    </div>`;
  }
}
customElements.define("phx-projects", PhxProjects);
