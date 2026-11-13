import { Router } from "@lit-labs/router";
import { SignalWatcher } from "@lit-labs/signals";
import { LitElement, css, html } from "lit";
import { ApiError, api, type User } from "../api";
import { t } from "../i18n";
import { icon } from "../icon";
import { currentProject, currentUser, navigate } from "../state";
import { base } from "./shared";
import "./login";
import "./projects";
import "./project";
import "./tokens";

/** Root element: top bar, session check and top-level routes. */
export class PhxApp extends SignalWatcher(LitElement) {
  static properties = { ready: { state: true } };
  static styles = [base, css`
    :host { display: block; min-height: 100vh; }
    header { display: flex; align-items: center; gap: 14px; padding: 10px 16px; border-bottom: 1px solid var(--line); background: var(--surface); position: sticky; top: 0; z-index: 10; }
    .brand { font-family: var(--font-display); font-weight: 700; font-size: 18px; color: var(--fg); text-decoration: none; display: flex; align-items: center; gap: 8px; }
    .brand img { width: 26px; height: 26px; }
    .crumb { color: var(--muted); text-decoration: none; display: flex; align-items: center; gap: 8px; }
    .pi { width: 22px; height: 22px; border-radius: 6px; display: grid; place-items: center; color: var(--surface); background: var(--accent); font: 600 10px var(--font-mono); }
    nav { margin-left: auto; display: flex; gap: 6px; align-items: center; }
    nav a, nav button { font-size: 13px; }
    nav a { color: var(--muted); text-decoration: none; display: inline-flex; gap: 6px; align-items: center; padding: 6px 8px; border-radius: 6px; }
    @media (hover: hover) and (pointer: fine) { nav a:hover { color: var(--fg); } }
  `];
  ready = false;

  private router = new Router(this, [
    { path: "/login", render: () => html`<phx-login></phx-login>` },
    { path: "/", render: () => html`<phx-projects></phx-projects>` },
    { path: "/settings/tokens", render: () => html`<phx-tokens></phx-tokens>` },
    { path: "/p/:project/*", render: ({ project }) => html`<phx-project .slug=${project!}></phx-project>` },
    { path: "/p/:project", render: ({ project }) => html`<phx-project .slug=${project!}></phx-project>` },
  ]);

  async connectedCallback() {
    super.connectedCallback();
    try {
      currentUser.set(await api<User>("GET", "/me"));
    } catch (e) {
      if (e instanceof ApiError && e.status === 401 && location.pathname !== "/login") navigate("/login");
    }
    this.ready = true;
  }

  private async logout() {
    await api("POST", "/auth/logout").catch(() => {});
    currentUser.set(null);
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
            <a class="brand" href="/"><img src="/logo.svg" alt="" />Phoedox</a>
            ${inProject && p ? html`<a class="crumb" href="/p/${p.slug}"><span class="pi">${p.short}</span>${p.name}</a>` : ""}
            <nav>
              <a href="/">${icon("FolderOpen", 15)} ${t("nav.projects")}</a>
              <a href="/settings/tokens">${icon("KeyRound", 15)} ${t("nav.tokens")}</a>
              <button class="ghost" @click=${this.logout}>${icon("LogOut", 15)} ${t("nav.logout")}</button>
            </nav>
          </header>`
        : ""}
      ${this.router.outlet()}
    `;
  }
}
customElements.define("phx-app", PhxApp);
