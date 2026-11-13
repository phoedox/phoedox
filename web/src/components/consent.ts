import { SignalWatcher } from "@lit-labs/signals";
import { LitElement, css, html } from "lit";
import { ApiError, api } from "../api";
import { t } from "../i18n";
import { icon } from "../icon";
import { currentUser, loginURL, navigate } from "../state";
import { authPage } from "./login";
import "./scope-picker";
import type { PhxScopePicker } from "./scope-picker";
import { base } from "./shared";

interface RequestInfo { clientName: string; clientUri?: string; redirectHost: string; resource: string }

/** OAuth consent screen for MCP clients (/oauth/authorize). */
export class PhxConsent extends SignalWatcher(LitElement) {
  static properties = { info: { state: true }, error: { state: true }, busy: { state: true } };
  static styles = [base, authPage, css`
    .who { display: flex; gap: 12px; align-items: center; }
    .who .app { width: 40px; height: 40px; border-radius: 10px; display: grid; place-items: center; background: var(--accent-soft); color: var(--fg); flex: none; }
    .scope { background: var(--surface2); border-radius: 8px; padding: 10px 12px; font-size: 14px; line-height: 1.5; }
    .warn { font-size: 13px; color: var(--muted); border-left: 3px solid var(--signal); padding-left: 10px; }
    .actions { display: flex; gap: 8px; } .actions button { flex: 1; justify-content: center; }
  `];
  info?: RequestInfo;
  error = "";
  busy = false;

  async connectedCallback() {
    super.connectedCallback();
    if (!currentUser.get()) {
      navigate(loginURL());
      return;
    }
    try {
      this.info = await api<RequestInfo>("GET", `/oauth/request${location.search}`);
    } catch (err) {
      this.error = err instanceof ApiError ? err.text : t("error.unknown");
    }
  }

  private async decide(approve: boolean) {
    this.busy = true;
    try {
      const params = Object.fromEntries(new URLSearchParams(location.search));
      const scope = this.renderRoot.querySelector<PhxScopePicker>("phx-scope-picker")?.value;
      if (approve && !scope) {
        this.error = t("scope.none_selected");
        this.busy = false;
        return;
      }
      const { redirect } = await api<{ redirect: string }>("POST", "/oauth/authorize", { ...params, ...(scope ?? {}), approve });
      location.href = redirect; // the client's redirect URI (may be an app scheme)
    } catch (err) {
      this.error = err instanceof ApiError ? err.text : t("error.unknown");
      this.busy = false;
    }
  }

  render() {
    const logo = html`<div class="logo"><img src="/logo.svg" alt="" />Phoedox</div>`;
    if (this.error) return html`<div class="card">${logo}<p class="error" role="alert">${this.error}</p></div>`;
    const i = this.info;
    if (!i) return html`<p class="muted">${t("app.loading")}</p>`;
    const u = currentUser.get();
    return html`<div class="card">
      ${logo}
      <h1 style="font-size:20px">${t("consent.title")}</h1>
      <div class="who"><span class="app" aria-hidden="true">${icon("Bot", 20)}</span>
        <p>${t("consent.request", { client: i.clientName, user: u?.name ?? "" })}</p></div>
      <p class="scope">${t("consent.scope")}</p>
      <div><b style="font-size:14px">${t("consent.access_title")}</b><phx-scope-picker style="display:block;margin-top:8px"></phx-scope-picker></div>
      <p class="muted" style="font-size:14px">${t("consent.redirect", { host: i.redirectHost })}</p>
      <p class="warn">${t("consent.warning")}</p>
      <div class="actions">
        <button class="ghost" ?disabled=${this.busy} @click=${() => this.decide(false)}>${t("consent.deny")}</button>
        <button ?disabled=${this.busy} @click=${() => this.decide(true)}>${this.busy ? t("consent.redirecting") : t("consent.allow")}</button>
      </div>
    </div>`;
  }
}
customElements.define("phx-consent", PhxConsent);
