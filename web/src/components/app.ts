import { Router } from "@lit-labs/router";
import { SignalWatcher } from "@lit-labs/signals";
import { LitElement, css, html } from "lit";
import { ApiError, api, type User } from "../api";
import { t } from "../i18n";
import { icon } from "../icon";
import { currentProject, currentUser, isPublicPath, loginURL, navigate, signedIn } from "../state";
import "./audit-log";
import "./project-switcher";
import "./lang-switcher";
import { base } from "./shared";
import "./auth-pages";
import "./login";
import "./profile";
import "./publications";
import "./security";
import "./notification-bell";
import "./notification-settings";
import "./consent";
import "./palette";
import type { PhxPalette } from "./palette";
import "./user-menu";
import "./users";
import "./admin-settings";
import "./projects";
import "./project";
import "./tokens";
import "./orgs";
import "./backups";
import "./plugins-admin";
import "./shared-view";

const projectView = (slug: string, root: boolean) => html`<phx-project .slug=${slug} .root=${root}></phx-project>`;

/** Root element: top bar, session check and top-level routes. */
export class PhxApp extends SignalWatcher(LitElement) {
  static properties = { ready: { state: true } };
  static styles = [base, css`
    :host { display: block; min-height: 100vh; }
    header { display: flex; align-items: center; gap: 14px; height: var(--header-h); padding: 0 16px; border-bottom: 1px solid var(--line); background: var(--surface); position: sticky; top: 0; z-index: 10; }
    .brand { font-family: var(--font-display); font-weight: 700; font-size: 18px; color: var(--fg); text-decoration: none; display: flex; align-items: center; gap: 8px; }
    .brand img { width: 26px; height: 26px; }
    .crumb { color: var(--muted); text-decoration: none; display: flex; align-items: center; gap: 8px; }
    .pi { width: 22px; height: 22px; border-radius: 6px; display: grid; place-items: center; color: var(--surface); background: var(--accent); font: 600 10px var(--font-mono); }
    nav { margin-left: auto; display: flex; gap: 6px; align-items: center; }
    nav a, nav button { font-size: 13px; }
    nav a { color: var(--muted); text-decoration: none; display: inline-flex; gap: 6px; align-items: center; padding: 6px 8px; border-radius: 6px; }
    @media (hover: hover) and (pointer: fine) { nav a:hover { color: var(--fg); } }
    button.search { font-weight: 500; color: var(--muted); padding: 5px 10px; }
    kbd { font: 11px var(--font-mono); border: 1px solid var(--line); border-radius: 4px; padding: 1px 5px; color: var(--muted); }
    .crumb { min-width: 0; } .crumb .name { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    /* Phones: icons only in the bar, the labels stay as accessible names. */
    @media (max-width: 700px) {
      header { gap: 10px; padding: 0 12px; }
      .brand .word, nav .lbl { display: none; }
      nav { gap: 2px; }
    }
  `];
  ready = false;

  private router = new Router(this, [
    { path: "/login", render: () => html`<phx-login></phx-login>` },
    { path: "/", render: () => html`<phx-projects></phx-projects>` },
    { path: "/forgot", render: () => html`<phx-forgot></phx-forgot>` },
    { path: "/invite/:token", render: ({ token }) => html`<phx-set-password .token=${token!}></phx-set-password>` },
    { path: "/reset/:token", render: ({ token }) => html`<phx-set-password .token=${token!}></phx-set-password>` },
    { path: "/settings/profile", render: () => html`<phx-profile></phx-profile>` },
    { path: "/oauth/authorize", render: () => html`<phx-consent></phx-consent>` },
    { path: "/settings/notifications", render: () => html`<phx-notification-settings></phx-notification-settings>` },
    { path: "/settings/security", render: () => html`<phx-security></phx-security>` },
    { path: "/settings/tokens", render: () => html`<phx-tokens></phx-tokens>` },
    { path: "/settings/publications", render: () => html`<phx-publications></phx-publications>` },
    { path: "/admin/users", render: () => html`<phx-users></phx-users>` },
    { path: "/admin/settings", render: () => html`<phx-admin-settings></phx-admin-settings>` },
    { path: "/admin/backups", render: () => html`<phx-backups></phx-backups>` },
    { path: "/admin/plugins", render: () => html`<phx-plugins-admin></phx-plugins-admin>` },
    { path: "/admin/audit", render: () => html`<phx-audit-log page></phx-audit-log>` },
    { path: "/orgs", render: () => html`<phx-orgs></phx-orgs>` },
    { path: "/orgs/:slug", render: ({ slug }) => html`<phx-org .slug=${slug!}></phx-org>` },
    { path: "/s/:token", render: ({ token }) => html`<phx-shared-view .token=${token!}></phx-shared-view>` },
    { path: "/s/:token/:doc", render: ({ token, doc }) => html`<phx-shared-view .token=${token!} .doc=${doc!}></phx-shared-view>` },
    // One template for both routes keeps the same element; root tells it to
    // show the overview (the root route has no tail for the child routes).
    { path: "/p/:project/*", render: ({ project }) => projectView(project!, false) },
    { path: "/p/:project", render: ({ project }) => projectView(project!, true) },
  ]);

  async connectedCallback() {
    super.connectedCallback();
    try {
      signedIn(await api<User>("GET", "/me"));
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

  render() {
    if (!this.ready) return html`<p class="muted" style="padding:24px">${t("app.loading")}</p>`;
    const user = currentUser.get();
    const shared = location.pathname.startsWith("/s/");
    return html`
      ${user && !shared
        ? html`<header>
            <a class="brand" href="/"><img src="/logo.svg" alt="" /><span class="word">Phoedox</span></a>
            <phx-project-switcher .path=${location.pathname}></phx-project-switcher>
            <phx-lang-switcher .path=${location.pathname}></phx-lang-switcher>
            <nav>
              <a href="/" aria-label=${t("nav.projects")}>${icon("FolderOpen", 15)} <span class="lbl">${t("nav.projects")}</span></a>
              <button class="ghost search" @click=${this.openPalette} aria-label=${t("palette.button")} aria-keyshortcuts="Control+K Meta+K">
                ${icon("Search", 15)}<span class="lbl">${t("palette.button")}</span><kbd class="lbl">${navigator.platform.startsWith("Mac") ? "⌘K" : t("palette.ctrl")}</kbd></button>
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
