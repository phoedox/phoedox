import { Router } from "@lit-labs/router";
import { SignalWatcher } from "@lit-labs/signals";
import { LitElement, css, html } from "lit";
import { ApiError, api, type User } from "../api";
import { t } from "../i18n";
import { icon } from "../icon";
import { currentProject, currentUser, isPublicPath, navigate, signedIn } from "../state";
import { base } from "./shared";
import "./auth-pages";
import "./login";
import "./profile";
import "./user-menu";
import "./users";
import "./projects";
import "./project";
import "./tokens";

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
    { path: "/settings/tokens", render: () => html`<phx-tokens></phx-tokens>` },
    { path: "/admin/users", render: () => html`<phx-users></phx-users>` },
    { path: "/p/:project/*", render: ({ project }) => html`<phx-project .slug=${project!}></phx-project>` },
    { path: "/p/:project", render: ({ project }) => html`<phx-project .slug=${project!}></phx-project>` },
  ]);

  async connectedCallback() {
    super.connectedCallback();
    try {
      signedIn(await api<User>("GET", "/me"));
    } catch (e) {
      if (e instanceof ApiError && e.status === 401 && !isPublicPath(location.pathname)) navigate("/login");
    }
    this.ready = true;
  }

  private async logout() {
    await api("POST", "/auth/logout").catch(() => {});
    signedIn(null);
    navigate("/login");
  }

  render() {
    if (!this.ready) return html`<p class="muted" style="padding:24px">${t("app.loading")}</p>`;
    const user = currentUser.get();
    const p = currentProject.get();
    const inProject = location.pathname.startsWith("/p/");
    return html`
      ${user
        ? html`<header>
            <a class="brand" href="/"><img src="/logo.svg" alt="" /><span class="word">Phoedox</span></a>
            ${inProject && p ? html`<a class="crumb" href="/p/${p.slug}"><span class="pi">${p.short}</span><span class="name">${p.name}</span></a>` : ""}
            <nav>
              <a href="/" aria-label=${t("nav.projects")}>${icon("FolderOpen", 15)} <span class="lbl">${t("nav.projects")}</span></a>
              <phx-user-menu @logout=${this.logout}></phx-user-menu>
            </nav>
          </header>`
        : ""}
      ${this.router.outlet()}
    `;
  }
}
customElements.define("phx-app", PhxApp);
