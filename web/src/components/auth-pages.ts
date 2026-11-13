import { LitElement, html } from "lit";
import { ApiError, api, minPasswordLength, type Instance, type LinkInfo, type User } from "../api";
import { t } from "../i18n";
import { navigate, signedIn } from "../state";
import { authPage } from "./login";
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
      await api("POST", "/auth/reset-request", { email: new FormData(e.target as HTMLFormElement).get("email") });
      this.sent = true;
    } catch (err) {
      this.error = err instanceof ApiError ? err.text : t("error.unknown");
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
      ${this.error ? html`<p class="error" role="alert">${this.error}</p>` : ""}
      <button type="submit" ?disabled=${this.busy || !this.instance}>${t("forgot.submit")}</button>
      ${back}
    </form>`;
  }
}
customElements.define("phx-forgot", PhxForgot);

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
      this.error = err instanceof ApiError ? err.text : t("error.unknown");
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
