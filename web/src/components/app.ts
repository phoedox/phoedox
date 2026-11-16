import { Router } from "@lit-labs/router";
import { SignalWatcher } from "@lit-labs/signals";
import { LitElement, css, html } from "lit";
import { ApiError, api, type User } from "../api";
import { t } from "../i18n";
import { icon } from "../icon";
import { comboFor, comboLabel, installShortcuts, onShortcut } from "../shortcuts";
import "./usage";
import "./bot-admin";
import "./feedback-portal";
import "./feedback-settings";
import "./recent-history";
import "./secret-links";
import "./shortcut-settings";
import { appOrg, currentProject, currentUser, focusMode, setFocusMode, setWideReading, wideReading, isPublicPath, loginURL, navigate, signedIn } from "../state";
import "./audit-log";
import "./project-switcher";
import "./lang-switcher";
import { base } from "./shared";
import "./auth-pages";
import "./kiosk";
import "./print";
import "./login";
import "./profile";
import "./publications";
import "./security";
import "./account";
import "./notification-bell";
import "./notification-settings";
import "./my-tasks";
import "./shared-devices";
import "./consent";
import "./palette";
import type { PhxPalette } from "./palette";
import "./user-menu";
import "./users";
import "./admin-settings";
import "./admin-overview";
import "./settings-shell";
import "./projects";
import "./project";
import "./tokens";
import "./orgs";
import "./backups";
import "./plugins-admin";
import "./shared-view";

const projectView = (slug: string, root: boolean) => html`<phx-project .slug=${slug} .root=${root}></phx-project>`;

/** Root element: top bar, session check and top-level routes. */
/** A page of the personal settings or the administration in its frame. */
const me = (active: string, page: unknown = "") => html`<phx-settings-shell area="me" active=${active}>${page}</phx-settings-shell>`;
const adm = (active: string, page: unknown = "") => currentUser.get()?.isAdmin
  ? html`<phx-settings-shell area="admin" active=${active}>${page}</phx-settings-shell>`
  : html`<main class="notfound enter"><h1>${t("error.not_found")}</h1></main>`;

export class PhxApp extends SignalWatcher(LitElement) {
  static properties = { ready: { state: true } };
  static styles = [base, css`
    .notfound { max-width: 560px; margin: 60px auto; padding: 0 16px; display: grid; gap: 14px; }
    :host { display: block; min-height: 100vh; }
    header { display: flex; align-items: center; gap: 14px; height: var(--header-h); padding: 0 16px; border-bottom: 1px solid var(--line); background: var(--surface); position: sticky; top: 0; z-index: 10; }
    .brand { font-family: var(--font-display); font-weight: 700; font-size: 18px; color: var(--fg); text-decoration: none; display: flex; align-items: center; gap: 8px; }
    .brand img { width: 26px; height: 26px; }
    .crumb { color: var(--muted); text-decoration: none; display: flex; align-items: center; gap: 8px; }
    .pi { width: 22px; height: 22px; border-radius: 6px; display: grid; place-items: center; color: var(--surface); background: var(--accent); font: 600 10px var(--font-mono); }
    nav { margin-left: auto; display: flex; gap: 6px; align-items: center; }
    nav a, nav button { font-size: 13px; }
    nav a { color: var(--muted); text-decoration: none; display: inline-flex; gap: 6px; align-items: center; height: var(--header-ctl, 34px); box-sizing: border-box; padding: 0 8px; border-radius: 6px; }
    @media (hover: hover) and (pointer: fine) { nav a:hover { color: var(--fg); } }
    button.search { font-weight: 500; color: var(--muted); height: var(--header-ctl, 34px); box-sizing: border-box; padding: 0 10px; }
    kbd { font: 11px var(--font-mono); border: 1px solid var(--line); border-radius: 4px; padding: 1px 5px; color: var(--muted); }
    .crumb { min-width: 0; } .crumb .name { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    /* Phones: icons only in the bar, the labels stay as accessible names. */
    .unfocus { position: fixed; top: 10px; right: 12px; z-index: 20; padding: 5px 10px; font-size: 13px; background: var(--surface); }
    @media (max-width: 700px) {
      header { gap: 10px; padding: 0 12px; }
      .brand .word, nav .lbl { display: none; }
      nav { gap: 2px; }
    }
  `];
  ready = false;

  private router = new Router(this, [
    { path: "/login", render: () => html`<phx-login></phx-login>` },
    { path: "/feedback/status/:token", render: ({ token }) => html`<phx-feedback-status .token=${token!}></phx-feedback-status>` },
    { path: "/feedback/:handle", render: ({ handle }) => html`<phx-feedback-portal .handle=${handle!}></phx-feedback-portal>` },
    { path: "/identify", render: () => html`<phx-identify></phx-identify>` },
    { path: "/kiosk/:project/:doc", render: ({ project, doc }) => html`<phx-kiosk .project=${project!} .doc=${doc!}></phx-kiosk>` },
    { path: "/print/:project", render: ({ project }) => html`<phx-print .project=${project!}></phx-print>` },
    // The website's "Start for free" links here; signed-in people go to their projects.
    { path: "/signup", render: () => {
      if (!currentUser.get()) return html`<phx-signup></phx-signup>`;
      queueMicrotask(() => navigate("/"));
      return html``;
    } },
    { path: "/", render: () => html`<phx-projects></phx-projects>` },
    { path: "/forgot", render: () => html`<phx-forgot></phx-forgot>` },
    { path: "/invite/:token", render: ({ token }) => html`<phx-set-password .token=${token!}></phx-set-password>` },
    { path: "/reset/:token", render: ({ token }) => html`<phx-set-password .token=${token!}></phx-set-password>` },
    { path: "/oauth/authorize", render: () => html`<phx-consent></phx-consent>` },
    { path: "/tasks", render: () => html`<phx-my-tasks></phx-my-tasks>` },
    { path: "/devices", render: () => html`<phx-shared-devices></phx-shared-devices>` },
    { path: "/history", render: () => html`<phx-recent-history></phx-recent-history>` },
    { path: "/secrets/new", render: () => html`<phx-secret-new></phx-secret-new>` },
    { path: "/secret/:id", render: ({ id }) => html`<phx-secret-open .id=${id!}></phx-secret-open>` },
    // Personal settings and administration share one frame (ADR-0070).
    { path: "/settings", render: () => me("") },
    { path: "/settings/profile", render: () => me("profile", html`<phx-profile></phx-profile>`) },
    { path: "/settings/security", render: () => me("security", html`<phx-security></phx-security>`) },
    { path: "/settings/notifications", render: () => me("notifications", html`<phx-notification-settings></phx-notification-settings>`) },
    { path: "/settings/shortcuts", render: () => me("shortcuts", html`<phx-shortcut-settings></phx-shortcut-settings>`) },
    { path: "/settings/tokens", render: () => me("tokens", html`<phx-tokens></phx-tokens>`) },
    { path: "/settings/publications", render: () => me("publications", html`<phx-publications></phx-publications>`) },
    { path: "/settings/account", render: () => me("account", html`<phx-account></phx-account>`) },
    { path: "/settings/usage", render: () => me("usage", html`<phx-usage level="me"></phx-usage>`) },
    { path: "/settings/feedback", render: () => me("feedback", html`<phx-my-feedback></phx-my-feedback>`) },
    { path: "/admin/usage", render: () => adm("usage", html`<phx-usage level="admin"></phx-usage>`) },
    { path: "/admin/bot-protection", render: () => adm("bot", html`<phx-bot-admin></phx-bot-admin>`) },
    { path: "/admin/orgs/:slug/usage", render: ({ slug }) => adm("orgs", html`<phx-usage level="org" .ref=${slug!}></phx-usage>`) },
    { path: "/orgs/:slug/usage", render: ({ slug }) => me(`org:${slug}`, html`<phx-usage level="org" .ref=${slug!}></phx-usage>`) },
    { path: "/admin", render: () => adm("") },
    { path: "/admin/overview", render: () => adm("overview", html`<phx-admin-overview></phx-admin-overview>`) },
    { path: "/admin/users", render: () => adm("users", html`<phx-users></phx-users>`) },
    { path: "/admin/signup", render: () => adm("signup", html`<phx-admin-settings section="signup"></phx-admin-settings>`) },
    { path: "/admin/ai", render: () => adm("ai", html`<phx-admin-settings section="ai"></phx-admin-settings>`) },
    { path: "/admin/storage", render: () => adm("storage", html`<phx-admin-settings section="storage"></phx-admin-settings>`) },
    { path: "/admin/security", render: () => adm("security", html`<phx-admin-settings section="security"></phx-admin-settings>`) },
    // Old address of the instance settings.
    { path: "/admin/settings", render: () => { queueMicrotask(() => { history.replaceState({}, "", "/admin/ai"); window.dispatchEvent(new PopStateEvent("popstate")); }); return html``; } },
    { path: "/admin/backups", render: () => adm("backups", html`<phx-backups></phx-backups>`) },
    { path: "/admin/plugins", render: () => adm("plugins", html`<phx-plugins-admin></phx-plugins-admin>`) },
    { path: "/admin/audit", render: () => adm("audit", html`<phx-audit-log page></phx-audit-log>`) },
    { path: "/admin/orgs", render: () => adm("orgs", html`<phx-orgs mode="list"></phx-orgs>`) },
    { path: "/admin/orgs/new", render: () => adm("org-new", html`<phx-orgs mode="new"></phx-orgs>`) },
    { path: "/admin/orgs/:slug", render: ({ slug }) => adm("orgs", html`<phx-org .slug=${slug!} inAdmin></phx-org>`) },
    { path: "/orgs", render: () => html`<phx-orgs></phx-orgs>` },
    // Owners manage their organisation within their personal settings.
    { path: "/orgs/:slug", render: ({ slug }) => me(`org:${slug}`, html`<phx-org .slug=${slug!}></phx-org>`) },
    { path: "/s/:token", render: ({ token }) => html`<phx-shared-view .token=${token!}></phx-shared-view>` },
    { path: "/s/:token/:doc", render: ({ token, doc }) => html`<phx-shared-view .token=${token!} .doc=${doc!}></phx-shared-view>` },
    // One template for both routes keeps the same element; root tells it to
    // show the overview (the root route has no tail for the child routes).
    { path: "/p/:project/*", render: ({ project }) => projectView(project!, false) },
    { path: "/p/:project", render: ({ project }) => projectView(project!, true) },
  ], { fallback: { render: () => html`<main class="notfound enter"><h1>${t("error.not_found")}</h1>
    <p><a class="btn" href="/">${icon("FolderOpen", 14)} ${t("nav.projects")}</a></p></main>` } });

  async connectedCallback() {
    super.connectedCallback();
    // On an organization's own domain the app carries its name.
    void api<{ org?: { id: string; slug: string; name: string } }>("GET", "/instance").then((i) => {
      if (i?.org) {
        appOrg.set(i.org);
        document.title = i.org.name;
      }
    }).catch(() => {});
    try {
      signedIn(await api<User>("GET", "/me"));
      void installShortcuts();
    } catch (e) {
      if (e instanceof ApiError && e.status === 401 && !isPublicPath(location.pathname)) navigate(loginURL());
    }
    this.ready = true;
  }

  private openPalette() {
    this.renderRoot.querySelector<PhxPalette>("phx-palette")?.open();
  }

  private async logout() {
    await api("POST", "/auth/logout").catch(() => {});
    signedIn(null);
    navigate("/login");
  }

  private offs: (() => void)[] = [];

  firstUpdated() {
    this.offs = [
      onShortcut("focus", () => setFocusMode(!focusMode.get())),
      onShortcut("history", () => navigate("/history")),
      onShortcut("tasks", () => navigate("/tasks")),
      onShortcut("projects", () => navigate("/")),
      onShortcut("wide", () => setWideReading(!wideReading.get())),
      onShortcut("newPage", () => {
        const p = currentProject.get();
        if (p && (p.role === "editor" || p.role === "owner") && location.pathname.startsWith(`/p/${p.slug}`)) navigate(`/p/${p.slug}/new/markdown`);
      }),
    ];
    // Esc leaves focus mode when nothing else uses it.
    addEventListener("keydown", this.onEsc);
  }

  disconnectedCallback() {
    super.disconnectedCallback();
    this.offs.forEach((f) => f());
    removeEventListener("keydown", this.onEsc);
  }

  private onEsc = (e: KeyboardEvent) => {
    if (e.key === "Escape" && focusMode.get() && !e.defaultPrevented && !document.querySelector("dialog[open]")) setFocusMode(false);
  };

  render() {
    if (!this.ready) return html`<p class="muted" style="padding:24px">${t("app.loading")}</p>`;
    const user = currentUser.get();
    // Shared pages and the wall display show no app header.
    const shared = location.pathname.startsWith("/s/") || location.pathname.startsWith("/kiosk/") || location.pathname.startsWith("/print/") || location.pathname.startsWith("/feedback/");
    return html`
      ${user && !shared && focusMode.get()
        ? html`<button class="ghost unfocus" @click=${() => setFocusMode(false)} title=${t("focus.leave_hint", { key: comboLabel(comboFor("focus")) })}>${icon("Minimize2", 14)} ${t("focus.leave")}</button>`
        : ""}
      ${user && !shared && !focusMode.get()
        ? html`<header>
            <a class="brand" href="/"><img src="/logo.svg" alt="" /><span class="word">${appOrg.get()?.name ?? "Phoedox"}</span></a>
            <phx-project-switcher .path=${location.pathname}></phx-project-switcher>
            <phx-lang-switcher .path=${location.pathname}></phx-lang-switcher>
            <nav>
              <a href="/" aria-label=${t("nav.projects")}>${icon("FolderOpen", 15)} <span class="lbl">${t("nav.projects")}</span></a>
              <button class="ghost search" @click=${this.openPalette} aria-label=${t("palette.button")} >
                ${icon("Search", 15)}<span class="lbl">${t("palette.button")}</span>${comboFor("palette") ? html`<kbd class="lbl">${comboLabel(comboFor("palette"))}</kbd>` : ""}</button>
              <phx-notification-bell></phx-notification-bell>
              <phx-user-menu @logout=${this.logout}></phx-user-menu>
            </nav>
          </header>`
        : ""}
      ${this.router.outlet()}
      ${user ? html`<phx-palette></phx-palette>` : ""}
    `;
  }
}
customElements.define("phx-app", PhxApp);
