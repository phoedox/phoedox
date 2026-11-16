import { SignalWatcher } from "@lit-labs/signals";
import { LitElement, css, html } from "lit";
import { ApiError, api, minPasswordLength, type Instance, type LinkInfo, type User } from "../api";
import { locale, t } from "../i18n";
import { currentUser, navigate, signedIn } from "../state";
import { authPage } from "./login";
import "./botcheck";
import type { PhxBotcheck } from "./botcheck";
import { base } from "./shared";

const logo = html`<div class="logo"><img src="/logo.svg" alt="" />Phoedox</div>`;

/** "Forgot password": asks for the email and always answers the same way. */
export class PhxForgot extends LitElement {
  static properties = { instance: { state: true }, sent: { state: true }, error: { state: true }, busy: { state: true } };
  static styles = [base, authPage];
  instance?: Instance;
  sent = false;
  error = "";
  busy = false;

  async connectedCallback() {
    super.connectedCallback();
    this.instance = await api<Instance>("GET", "/instance").catch(() => undefined);
  }

  private async submit(e: SubmitEvent) {
    e.preventDefault();
    this.busy = true;
    this.error = "";
    try {
      const botToken = await this.renderRoot.querySelector<PhxBotcheck>("phx-botcheck")?.token();
      await api("POST", "/auth/reset-request", { email: new FormData(e.target as HTMLFormElement).get("email"), botToken });
      this.sent = true;
    } catch (err) {
      this.error = err instanceof ApiError ? err.text : err instanceof Error && err.message === t("bot.blocked") ? err.message : t("error.unknown");
    } finally {
      this.busy = false;
    }
  }

  render() {
    const back = html`<div class="links"><a href="/login">${t("forgot.back")}</a></div>`;
    if (this.instance && !this.instance.passwordReset) {
      return html`<div class="card">${logo}<h1 style="font-size:20px">${t("forgot.title")}</h1><p class="muted">${t("forgot.no_mail")}</p>${back}</div>`;
    }
    if (this.sent) {
      return html`<div class="card" role="status">${logo}<h1 style="font-size:20px">${t("forgot.title")}</h1><p>${t("forgot.sent")}</p>${back}</div>`;
    }
    return html`<form class="card" @submit=${this.submit}>
      ${logo}
      <h1 style="font-size:20px">${t("forgot.title")}</h1>
      <p class="muted">${t("forgot.intro")}</p>
      <label class="field">${t("login.email")}<input name="email" type="email" autocomplete="email" required /></label>
      <phx-botcheck place="reset"></phx-botcheck>
      ${this.error ? html`<p class="error" role="alert">${this.error}</p>` : ""}
      <button type="submit" ?disabled=${this.busy || !this.instance}>${t("forgot.submit")}</button>
      ${back}
    </form>`;
  }
}
customElements.define("phx-forgot", PhxForgot);

/** Self sign-up (ADR-0057): name and address; the account becomes active
 * through the confirmation link sent by mail. */
export class PhxSignup extends LitElement {
  static properties = { instance: { state: true }, sent: { state: true }, error: { state: true }, busy: { state: true } };
  static styles = [base, authPage, css`
    label.accept { display: grid; grid-template-columns: auto 1fr; gap: 8px; align-items: start; font-size: 14px; line-height: 1.45; }
    label.accept input { width: auto; margin-top: 3px; }
  `];
  instance?: Instance;
  sent = "";
  error = "";
  busy = false;

  async connectedCallback() {
    super.connectedCallback();
    this.instance = await api<Instance>("GET", "/instance").catch(() => undefined);
  }

  private async submit(e: SubmitEvent) {
    e.preventDefault();
    const f = new FormData(e.target as HTMLFormElement);
    this.busy = true;
    this.error = "";
    try {
      const email = String(f.get("email") ?? "");
      const botToken = await this.renderRoot.querySelector<PhxBotcheck>("phx-botcheck")?.token();
      await api("POST", "/auth/signup", { email, name: String(f.get("name") ?? ""), locale: locale(), accept: f.get("accept") === "on", botToken });
      this.sent = email;
    } catch (err) {
      this.error = err instanceof ApiError ? err.text : err instanceof Error && err.message === t("bot.blocked") ? err.message : t("error.unknown");
    } finally {
      this.busy = false;
    }
  }

  render() {
    const back = html`<div class="links"><a href="/login">${t("signup.have_account")}</a></div>`;
    const i = this.instance;
    if (i && !i.signup) {
      return html`<div class="card">${logo}<h1 style="font-size:20px">${t("signup.title")}</h1><p class="muted">${t("login.signup_closed")}</p>${back}</div>`;
    }
    if (this.sent) {
      return html`<div class="card" role="status">${logo}<h1 style="font-size:20px">${t("signup.check_mail")}</h1>
        <p>${t("signup.sent", { email: this.sent })}</p><p class="muted">${t("signup.sent_hint")}</p>${back}</div>`;
    }
    const legal = i?.termsUrl || i?.privacyUrl;
    return html`<form class="card" @submit=${this.submit}>
      ${logo}
      <h1 style="font-size:20px">${t("signup.title")}</h1>
      <p class="muted">${t("signup.intro")}</p>
      <label class="field">${t("signup.name")}<input name="name" autocomplete="name" maxlength="100" /></label>
      <label class="field">${t("login.email")}<input name="email" type="email" autocomplete="email" required /></label>
      ${legal ? html`<label class="accept"><input type="checkbox" name="accept" required>
        <span>${t("signup.accept_prefix")} ${i?.termsUrl ? html`<a href=${i.termsUrl} target="_blank" rel="noopener">${t("signup.terms")}</a>` : ""}${i?.termsUrl && i?.privacyUrl ? ` ${t("signup.and")} ` : ""}${i?.privacyUrl ? html`<a href=${i.privacyUrl} target="_blank" rel="noopener">${t("signup.privacy")}</a>` : ""}.</span></label>` : ""}
      <phx-botcheck place="signup"></phx-botcheck>
      ${this.error ? html`<p class="error" role="alert">${this.error}</p>` : ""}
      <button type="submit" ?disabled=${this.busy || !i}>${t("signup.submit")}</button>
      ${back}
    </form>`;
  }
}
customElements.define("phx-signup", PhxSignup);

/** "Sign in with Phoedox" (ADR-0058): another Phoedox service, such as the
 * plugin store, asks who you are. It gets id, name and e-mail – nothing else. */
export class PhxIdentify extends SignalWatcher(LitElement) {
  static properties = { client: { state: true }, error: { state: true }, busy: { state: true } };
  static styles = [base, authPage, css`
    .who { display: flex; gap: 10px; align-items: center; padding: 10px 12px; border: 1px solid var(--line); border-radius: 10px; }
    .who b { display: block; }
    ul { margin: 0; padding-left: 20px; line-height: 1.6; }
  `];
  client?: { clientId: string; name: string; host?: string; app?: boolean };
  error = "";
  busy = false;
  private q = new URLSearchParams(location.search);

  async connectedCallback() {
    super.connectedCallback();
    try {
      this.client = await api("GET", `/identity/request?client_id=${encodeURIComponent(this.q.get("client_id") ?? "")}&redirect_uri=${encodeURIComponent(this.q.get("redirect_uri") ?? "")}`);
    } catch (err) {
      this.error = err instanceof ApiError ? err.text : err instanceof Error && err.message === t("bot.blocked") ? err.message : t("error.unknown");
    }
  }

  private async approve() {
    this.busy = true;
    try {
      const r = await api<{ redirect: string }>("POST", "/identity/approve", {
        client_id: this.q.get("client_id"), redirect_uri: this.q.get("redirect_uri"), state: this.q.get("state"), code_challenge: this.q.get("code_challenge"),
      });
      location.assign(r.redirect);
    } catch (err) {
      this.error = err instanceof ApiError ? err.text : err instanceof Error && err.message === t("bot.blocked") ? err.message : t("error.unknown");
      this.busy = false;
    }
  }

  private async other() {
    await api("POST", "/auth/logout").catch(() => {});
    location.assign(`/login?next=${encodeURIComponent(location.pathname + location.search)}`);
  }

  render() {
    const u = currentUser.get();
    if (this.error) return html`<div class="card">${logo}<h1 style="font-size:20px">${t("identify.title")}</h1><p class="error" role="alert">${this.error}</p></div>`;
    if (!this.client || !u) return html`<div class="card">${logo}<p class="muted">${t("app.loading")}</p></div>`;
    if (this.client.app) return html`<div class="card">
      ${logo}
      <h1 style="font-size:20px">${t("appsignin.title")}</h1>
      <p class="muted">${t("appsignin.intro")}</p>
      <div class="who"><span><b>${u.name}</b><span class="muted">${u.email}</span></span></div>
      <ul class="muted"><li>${t("appsignin.gets")}</li><li>${t("appsignin.revoke")}</li></ul>
      <button ?disabled=${this.busy} @click=${this.approve}>${t("appsignin.allow")}</button>
      <div class="links"><a href="#" @click=${(e: Event) => { e.preventDefault(); void this.other(); }}>${t("identify.other")}</a></div>
    </div>`;
    return html`<div class="card">
      ${logo}
      <h1 style="font-size:20px">${t("identify.title_for", { name: this.client.name })}</h1>
      <p class="muted">${t("identify.intro", { name: this.client.name, host: this.client.host })}</p>
      <div class="who"><span><b>${u.name}</b><span class="muted">${u.email}</span></span></div>
      <ul class="muted"><li>${t("identify.gets")}</li><li>${t("identify.not")}</li></ul>
      <button ?disabled=${this.busy} @click=${this.approve}>${t("identify.continue", { name: u.name })}</button>
      <div class="links"><a href="#" @click=${(e: Event) => { e.preventDefault(); void this.other(); }}>${t("identify.other")}</a></div>
    </div>`;
  }
}
customElements.define("phx-identify", PhxIdentify);

/** Sets a password from an invitation or reset link and signs in. */
export class PhxSetPassword extends LitElement {
  static properties = { token: {}, info: { state: true }, error: { state: true }, invalid: { state: true }, busy: { state: true } };
  static styles = [base, authPage];
  token = "";
  info?: LinkInfo;
  error = "";
  /** The link itself is unusable (expired, used, unknown). */
  invalid = "";
  busy = false;
  private loaded = "";

  async willUpdate() {
    if (this.token && this.token !== this.loaded) {
      this.loaded = this.token;
      try {
        this.info = await api<LinkInfo>("GET", `/auth/links/${encodeURIComponent(this.token)}`);
      } catch (err) {
        this.invalid = err instanceof ApiError ? err.text : t("error.unknown");
      }
    }
  }

  private async submit(e: SubmitEvent) {
    e.preventDefault();
    const f = new FormData(e.target as HTMLFormElement);
    if (f.get("password") !== f.get("repeat")) {
      this.error = t("setpw.mismatch");
      return;
    }
    this.busy = true;
    this.error = "";
    try {
      signedIn(await api<User>("POST", `/auth/links/${encodeURIComponent(this.token)}`, { password: f.get("password") }));
      navigate("/");
    } catch (err) {
      this.error = err instanceof ApiError ? err.text : err instanceof Error && err.message === t("bot.blocked") ? err.message : t("error.unknown");
    } finally {
      this.busy = false;
    }
  }

  render() {
    if (this.invalid) {
      return html`<div class="card">${logo}<p class="error" role="alert">${this.invalid}</p><div class="links"><a href="/login">${t("forgot.back")}</a><a href="/forgot">${t("login.forgot")}</a></div></div>`;
    }
    const i = this.info;
    if (!i) return html`<p class="muted">${t("app.loading")}</p>`;
    const invite = i.purpose === "invite";
    return html`<form class="card" @submit=${this.submit}>
      ${logo}
      <h1 style="font-size:20px">${t(invite ? "setpw.invite_title" : "setpw.reset_title")}</h1>
      <p class="muted">${t(invite ? "setpw.invite_intro" : "setpw.reset_intro", { name: i.name, email: i.email })}</p>
      <input type="email" name="username" autocomplete="username" .value=${i.email} hidden />
      <label class="field">${t("setpw.password")}<input name="password" type="password" autocomplete="new-password" minlength=${minPasswordLength} required aria-describedby="pwhint" /></label>
      <span id="pwhint" class="muted" style="font-size:12.5px;margin-top:-8px">${t("setpw.hint", { min: minPasswordLength })}</span>
      <label class="field">${t("setpw.repeat")}<input name="repeat" type="password" autocomplete="new-password" required /></label>
      ${this.error ? html`<p class="error" role="alert">${this.error}</p>` : ""}
      <button type="submit" ?disabled=${this.busy}>${t("setpw.submit")}</button>
    </form>`;
  }
}
customElements.define("phx-set-password", PhxSetPassword);
