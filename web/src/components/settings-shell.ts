// The frame of the settings areas (ADR-0070): a rail with groups and a
// search over the settings, and the page next to it. Personal settings and
// administration use it; project settings have their own rail in the
// project. On phones the rail is the first screen and a page opens with a
// back link.

import { SignalWatcher } from "@lit-labs/signals";
import { LitElement, css, html, nothing } from "lit";
import { api } from "../api";
import { hasKey, t } from "../i18n";
import { icon } from "../icon";
import { currentUser, navigate } from "../state";
import { base } from "./shared";

export type Area = "me" | "admin" | "project";
/** What the project area needs to know to show its pages. */
export interface ProjectCtx { slug: string; name: string; owner: boolean; canEdit: boolean; withModel: boolean }
export interface SettingsPage { id: string; href: string; icon: string; label: string; end?: string; warn?: boolean }
export interface SettingsGroup { label: string; pages: SettingsPage[] }

/** Words people search for, per page (both languages through i18n). */
function keywords(id: string): string {
  const k = `sx.kw.${id}`;
  return hasKey(k) ? t(k as any) : "";
}

interface OrgLite { slug: string; name: string; role?: string }

export function areaGroups(area: Area, orgs: OrgLite[] = [], ctx?: ProjectCtx): SettingsGroup[] {
  if (area === "project") {
    if (!ctx) return [];
    const base = `/p/${ctx.slug}/settings`;
    const pg = (id: string, ico: string, label: string, show = true) => (show ? [{ id, href: `${base}/${id}`, icon: ico, label }] : []);
    return [
      { label: t("sx.group.project"), pages: [...pg("general", "Settings", t("settings.general")), ...pg("people", "Users", t("sx.p.people"))] },
      { label: t("sx.group.workflow"), pages: [...pg("ai", "Bot", t("sx.p.ai")), ...pg("rules", "Zap", t("sx.p.auto"), ctx.canEdit), ...pg("model", "Shapes", t("kg.model"), ctx.withModel)] },
      { label: t("sx.group.connections"), pages: [...pg("env", "Braces", t("sx.p.env")), ...pg("code", "GitBranch", t("sx.p.code"), ctx.canEdit), ...pg("integrations", "Plug", t("sx.p.int"), ctx.owner)] },
      { label: t("sx.group.data"), pages: [...pg("data", "HardDrive", t("sx.p.data")), ...pg("usage", "Sigma", t("usage.nav_project"), ctx.owner), ...pg("activity", "ScrollText", t("audit.project_title"), ctx.owner), ...pg("transfer", "TriangleAlert", t("sx.p.transfer"), ctx.owner)] },
    ].filter((g) => g.pages.length);
  }
  if (area === "admin") {
    return [
      { label: t("sx.group.instance"), pages: [
        { id: "overview", href: "/admin/overview", icon: "Gauge", label: t("sx.admin.overview") },
        { id: "users", href: "/admin/users", icon: "Users", label: t("users.title") },
        { id: "signup", href: "/admin/signup", icon: "Mail", label: t("sx.admin.signup") },
        { id: "ai", href: "/admin/ai", icon: "Bot", label: t("sx.admin.ai") },
        { id: "usage", href: "/admin/usage", icon: "Sigma", label: t("usage.nav") },
        { id: "storage", href: "/admin/storage", icon: "HardDrive", label: t("sx.admin.storage") },
        { id: "security", href: "/admin/security", icon: "ShieldCheck", label: t("sx.admin.security") },
        { id: "bot", href: "/admin/bot-protection", icon: "Bot", label: t("botadmin.nav") },
        { id: "backups", href: "/admin/backups", icon: "Archive", label: t("backups.title") },
        { id: "plugins", href: "/admin/plugins", icon: "Puzzle", label: t("plugins.title") },
        { id: "audit", href: "/admin/audit", icon: "ScrollText", label: t("audit.title") },
      ] },
      { label: t("sx.group.orgs"), pages: [
        { id: "orgs", href: "/admin/orgs", icon: "Building2", label: t("sx.admin.orgs") },
        { id: "org-new", href: "/admin/orgs/new", icon: "Plus", label: t("sx.admin.org_new") },
      ] },
    ];
  }
  const groups: SettingsGroup[] = [
    { label: t("sx.group.you"), pages: [
      { id: "profile", href: "/settings/profile", icon: "UserRound", label: t("nav.profile") },
      { id: "security", href: "/settings/security", icon: "ShieldCheck", label: t("sx.me.security") },
      { id: "notifications", href: "/settings/notifications", icon: "Bell", label: t("notif.title") },
      { id: "shortcuts", href: "/settings/shortcuts", icon: "Keyboard", label: t("shortcut.title") },
      { id: "tokens", href: "/settings/tokens", icon: "KeyRound", label: t("sx.me.tokens") },
      { id: "usage", href: "/settings/usage", icon: "Sigma", label: t("usage.nav") },
      { id: "publications", href: "/settings/publications", icon: "Globe", label: t("pub.title_page") },
      { id: "account", href: "/settings/account", icon: "Archive", label: t("sx.me.account") },
    ] },
  ];
  // Owners of an organisation manage it here; system admins do it in the administration.
  const managed = orgs.filter((o) => o.role === "admin");
  if (managed.length && !currentUser.get()?.isAdmin) {
    groups.push({ label: t("sx.group.my_orgs"), pages: managed.map((o) => ({ id: `org:${o.slug}`, href: `/orgs/${o.slug}`, icon: "Building2", label: o.name })) });
  }
  return groups;
}

let orgCache: Promise<OrgLite[]> | undefined;

export class PhxSettingsShell extends SignalWatcher(LitElement) {
  static properties = { area: {}, active: {}, ctx: { attribute: false }, q: { state: true }, orgs: { state: true } };
  static styles = [base, css`
    :host { display: grid; grid-template-columns: 252px minmax(0, 1fr); gap: 40px; max-width: 1180px; margin: 0 auto; padding: 28px 16px 80px; align-items: start; }
    .rail { position: sticky; top: calc(var(--header-h) + 20px); display: grid; gap: 14px; max-height: calc(100dvh - var(--header-h) - 40px); overflow: auto; padding: 2px 4px 2px 2px; }
    .rail h1 { font-size: 24px; }
    .up { display: inline-flex; gap: 4px; align-items: center; color: var(--muted); text-decoration: none; font-size: 13.5px; margin-bottom: -8px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    @media (hover: hover) and (pointer: fine) { .up:hover { color: var(--fg); } }
    .tag { justify-self: start; display: inline-flex; gap: 5px; align-items: center; font-size: 12px; color: var(--signal); background: var(--surface2); border-radius: 999px; padding: 2px 9px; }
    .search { display: flex; gap: 8px; align-items: center; height: 36px; padding: 0 10px; border: 1px solid var(--line); border-radius: 9px; background: var(--surface); color: var(--muted); }
    .search input { border: 0; outline: 0; background: transparent; padding: 0; height: 100%; }
    .search:focus-within { border-color: var(--accent); }
    .search kbd { font: 11px var(--font-mono); border: 1px solid var(--line); border-radius: 5px; padding: 0 5px; }
    nav { display: grid; gap: 14px; }
    .grp { display: grid; gap: 1px; }
    .gl { font-size: 11.5px; font-weight: 600; letter-spacing: .05em; text-transform: uppercase; color: var(--muted); padding: 6px 10px; }
    nav a { display: flex; gap: 10px; align-items: center; padding: 7px 10px; border-radius: 8px; color: var(--fg); text-decoration: none; font-size: 14px; }
    nav a .ico { color: var(--muted); }
    nav a small { display: block; color: var(--muted); font-size: 12px; }
    nav a mark { background: var(--accent-soft); color: inherit; border-radius: 3px; }
    nav a[aria-current="page"] { background: var(--surface); box-shadow: inset 0 0 0 1px var(--line); font-weight: 500; }
    nav a[aria-current="page"] .ico { color: var(--accent); }
    @media (hover: hover) and (pointer: fine) { nav a:hover:not([aria-current="page"]) { background: var(--surface2); } }
    .none { color: var(--muted); font-size: 13px; padding: 0 10px; }
    main { min-width: 0; }
    .back { display: none; }
    /* The page's own frame (width, padding) gives way to the shell. */
    ::slotted(*) { margin: 0 !important; padding: 0 !important; max-width: none !important; }
    @media (max-width: 760px) {
      :host { grid-template-columns: 1fr; gap: 0; padding-top: 16px; }
      .rail { position: static; max-height: none; overflow: visible; }
      :host([active]:not([active=""])) .rail { display: none; }
      :host(:not([active])) main, :host([active=""]) main { display: none; }
      .back { display: inline-flex; gap: 4px; align-items: center; margin-bottom: 10px; color: var(--accent); text-decoration: none; font-weight: 500; }
    }
  `];

  area: Area = "me";
  ctx?: ProjectCtx;
  active = "";
  q = "";
  orgs: OrgLite[] = [];

  connectedCallback() {
    super.connectedCallback();
    window.addEventListener("keydown", this.onKey);
    if (this.area === "me") {
      orgCache ??= api<OrgLite[]>("GET", "/orgs").catch(() => []);
      void orgCache.then((o) => (this.orgs = o ?? []));
    }
  }

  disconnectedCallback() {
    super.disconnectedCallback();
    window.removeEventListener("keydown", this.onKey);
  }

  firstUpdated() {
    // Without a page: phones show the rail; elsewhere the first page opens.
    if (!this.active && !matchMedia("(max-width: 760px)").matches) {
      const first = areaGroups(this.area, this.orgs, this.ctx)[0]?.pages[0];
      if (!first) return;
      history.replaceState({}, "", first.href);
      window.dispatchEvent(new PopStateEvent("popstate"));
    }
  }

  private onKey = (e: KeyboardEvent) => {
    const el = e.composedPath()[0] as HTMLElement | undefined;
    if (e.key !== "/" || el?.closest?.("input, textarea, select, [contenteditable]") || el?.isContentEditable) return;
    e.preventDefault();
    this.renderRoot.querySelector<HTMLInputElement>(".search input")?.focus();
  };

  private found() {
    const term = this.q.trim().toLowerCase();
    return areaGroups(this.area, this.orgs, this.ctx).map((g) => ({
      ...g,
      pages: g.pages.map((p) => {
        if (!term || p.label.toLowerCase().includes(term)) return { p, hit: "" };
        const hit = keywords(p.id).split(",").map((w) => w.trim()).find((w) => w.toLowerCase().includes(term));
        return hit ? { p, hit } : undefined;
      }).filter((x): x is { p: SettingsPage; hit: string } => !!x),
    })).filter((g) => g.pages.length);
  }

  private go(e: Event, href: string) {
    const ev = e as MouseEvent;
    if (ev.button !== 0 || ev.ctrlKey || ev.metaKey || ev.shiftKey) return;
    e.preventDefault();
    this.q = "";
    navigate(href);
  }

  render() {
    const groups = this.found();
    const admin = this.area === "admin";
    const proj = this.area === "project" ? this.ctx : undefined;
    const root = proj ? `/p/${proj.slug}/settings` : admin ? "/admin" : "/settings";
    const title = t(proj ? "settings.title" : admin ? "sx.admin.title" : "sx.me.title");
    return html`
      <aside class="rail" aria-label=${title}>
        ${proj ? html`<a class="up" href="/p/${proj.slug}" @click=${(e: Event) => this.go(e, `/p/${proj.slug}`)}>${icon("ChevronLeft", 15)} ${proj.name}</a>` : nothing}
        <h1>${title}</h1>
        ${admin ? html`<span class="tag">${icon("Shield", 12)} ${t("sx.admin.only")}</span>` : nothing}
        <label class="search">${icon("Search", 15)}<input type="search" .value=${this.q} placeholder=${t("sx.search")} aria-label=${t("sx.search")} autocomplete="off"
          @input=${(e: Event) => (this.q = (e.target as HTMLInputElement).value)}
          @keydown=${(e: KeyboardEvent) => { if (e.key === "Enter") { const p = groups[0]?.pages[0]?.p; if (p) this.go(e, p.href); } if (e.key === "Escape") this.q = ""; }}><kbd>/</kbd></label>
        <nav>
          ${groups.length ? groups.map((g) => html`<div class="grp" role="group" aria-label=${g.label}><span class="gl">${g.label}</span>
            ${g.pages.map(({ p, hit }) => html`<a href=${p.href} aria-current=${p.id === this.active ? "page" : "false"} @click=${(e: Event) => this.go(e, p.href)}>
              ${icon(p.icon, 16)}<span>${p.label}${hit ? html`<small>… <mark>${hit}</mark></small>` : nothing}</span></a>`)}
          </div>`) : html`<p class="none">${t("sx.no_match", { q: this.q })}</p>`}
        </nav>
      </aside>
      <main>
        <a class="back" href=${root} @click=${(e: Event) => this.go(e, root)}>${icon("ChevronLeft", 15)} ${title}</a>
        <slot></slot>
      </main>`;
  }
}
customElements.define("phx-settings-shell", PhxSettingsShell);
