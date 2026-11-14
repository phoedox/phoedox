import { LitElement, css, html } from "lit";
import { ApiError, api, type Instance, type User } from "../api";
import { t } from "../i18n";
import { icon } from "../icon";
import { hasKey, type Key } from "../i18n";
import { navigate, nextPath, signedIn } from "../state";
import { cancelled, credentialJSON, passkeysSupported, requestOptions } from "../webauthn";
import { base } from "./shared";

/** Centered card used by login, forgot-password and set-password pages. */
export const authPage = css`
    :host { display: grid; place-items: center; min-height: 100vh; padding: 16px; }
    /* Seen once per session: a soft entrance is fine here. */
    .card {
      width: min(380px, 100%); display: flex; flex-direction: column; gap: 14px; padding: 28px;
      transition: opacity var(--dur-enter) var(--ease-out), transform var(--dur-enter) var(--ease-out);
      @starting-style { opacity: 0; transform: translateY(8px) scale(0.98); }
    }
    @media (prefers-reduced-motion: reduce) {
      .card { transition: opacity var(--dur) ease; @starting-style { opacity: 0; transform: none; } }
    }
    .logo { display: flex; align-items: center; gap: 10px; font-family: var(--font-display); font-size: 24px; font-weight: 700; }
    .logo img { width: 40px; height: 40px; }
    .card p { margin: 0; line-height: 1.5; }
    .links { display: flex; justify-content: space-between; gap: 10px; font-size: 13px; }
`;

type LoginStep = { challenge: string; methods: string[] };

export class PhxLogin extends LitElement {
  static properties = { error: { state: true }, busy: { state: true }, step: { state: true }, mode: { state: true }, instance: { state: true } };
  static styles = [base, authPage, css`
    .sep { display: flex; align-items: center; gap: 10px; color: var(--muted); font-size: 12px; }
    .sep::before, .sep::after { content: ""; flex: 1; border-top: 1px solid var(--line); }
    .alt { display: flex; flex-direction: column; gap: 6px; font-size: 13px; }
    .alt button { background: none; border: 0; padding: 0; color: var(--accent); font-weight: 500; font-size: 13px; text-decoration: underline; }
    input.code { font-family: var(--font-mono); font-size: 20px; letter-spacing: .2em; text-align: center; }
  `];
  error = "";
  busy = false;
  /** Set after the password when a second factor is needed. */
  step?: LoginStep;
  mode: "totp" | "recovery" = "totp";
  instance?: Instance;

  async connectedCallback() {
    super.connectedCallback();
    // Errors from the SSO callback arrive as ?error=<translation key>.
    const key = new URLSearchParams(location.search).get("error");
    if (key && hasKey(key)) this.error = t(key as Key);
    this.instance = await api<Instance>("GET", "/instance").catch(() => undefined);
  }

  private fail(err: unknown) {
    if (cancelled(err)) return;
    this.error = err instanceof ApiError ? err.text : t("error.unknown");
  }

  private done(u: User) {
    signedIn(u);
    const next = nextPath();
    // Publications are served by the server, not by the app's router.
    if (next.startsWith("/pub/")) location.assign(next);
    else navigate(next);
  }

  private async run(fn: () => Promise<void>) {
    this.busy = true;
    this.error = "";
    try {
      await fn();
    } catch (err) {
      this.fail(err);
    } finally {
      this.busy = false;
    }
  }

  private submit(e: SubmitEvent) {
    e.preventDefault();
    const f = new FormData(e.target as HTMLFormElement);
    return this.run(async () => {
      const res = await api<User | LoginStep>("POST", "/auth/login", { email: f.get("email"), password: f.get("password") });
      if ("challenge" in res) {
        this.step = res;
        this.mode = "totp";
        await this.updateComplete;
        this.renderRoot.querySelector<HTMLInputElement>("input.code")?.focus();
      } else this.done(res);
    });
  }

  private submitCode(e: SubmitEvent) {
    e.preventDefault();
    const code = new FormData(e.target as HTMLFormElement).get("code");
    return this.run(async () => {
      try {
        this.done(await api<User>("POST", `/auth/login/${this.mode}`, { challenge: this.step!.challenge, code }));
      } catch (err) {
        // An expired or exhausted challenge sends the user back to the password.
        if (err instanceof ApiError && err.key === "validation.challenge_invalid") this.step = undefined;
        throw err;
      }
    });
  }

  /** Passwordless sign-in with any passkey of this site. */
  private passkeyLogin() {
    return this.run(async () => {
      const { options, token } = await api<{ options: any; token: string }>("POST", "/auth/passkey/begin");
      const cred = (await navigator.credentials.get(requestOptions(options))) as PublicKeyCredential;
      this.done(await api<User>("POST", `/auth/passkey/finish?token=${encodeURIComponent(token)}`, credentialJSON(cred)));
    });
  }

  /** Second step with one of the user's passkeys. */
  private passkeySecondFactor() {
    return this.run(async () => {
      const { options, token } = await api<{ options: any; token: string }>("POST", "/auth/login/passkey/begin", { challenge: this.step!.challenge });
      const cred = (await navigator.credentials.get(requestOptions(options))) as PublicKeyCredential;
      this.done(await api<User>("POST", `/auth/login/passkey/finish?token=${encodeURIComponent(token)}`, credentialJSON(cred)));
    });
  }

  private secondStep(step: LoginStep) {
    const recovery = this.mode === "recovery";
    return html`<form class="card" @submit=${this.submitCode}>
      <div class="logo"><img src="/logo.svg" alt="" />Phoedox</div>
      <h1 style="font-size:20px">${t("login.2fa_title")}</h1>
      <p class="muted">${t(recovery ? "login.recovery_intro" : "login.2fa_intro")}</p>
      ${recovery
        ? html`<label class="field">${t("login.recovery_code")}<input class="code" name="code" autocomplete="off" autocapitalize="off" spellcheck="false" required /></label>`
        : html`<label class="field">${t("login.2fa_code")}<input class="code" name="code" inputmode="numeric" autocomplete="one-time-code" pattern="[0-9 ]{6,7}" maxlength="7" required /></label>`}
      ${this.error ? html`<p class="error" role="alert">${this.error}</p>` : ""}
      <button type="submit" ?disabled=${this.busy}>${t("login.2fa_submit")}</button>
      <div class="alt">
        ${step.methods.includes("passkey") && passkeysSupported() ? html`<button type="button" @click=${this.passkeySecondFactor}>${t("login.use_passkey")}</button>` : ""}
        <button type="button" @click=${() => { this.mode = recovery ? "totp" : "recovery"; this.error = ""; }}>${t(recovery ? "login.use_totp" : "login.use_recovery")}</button>
      </div>
      <div class="links"><a href="/login" @click=${(e: Event) => { e.preventDefault(); this.step = undefined; this.error = ""; }}>${t("forgot.back")}</a></div>
    </form>`;
  }

  render() {
    if (this.step) return this.secondStep(this.step);
    return html`<form class="card" @submit=${this.submit}>
      <div class="logo"><img src="/logo.svg" alt="" />Phoedox</div>
      ${this.instance?.org ? html`<p class="muted" style="margin:0 0 4px;font-weight:600">${this.instance.org.name}</p>` : ""}
      <h1 style="font-size:20px">${t("login.title")}</h1>
      <label class="field">${t("login.email")}<input name="email" type="email" autocomplete="username webauthn" required /></label>
      <label class="field">${t("login.password")}<input name="password" type="password" autocomplete="current-password" required /></label>
      ${this.error ? html`<p class="error" role="alert">${this.error}</p>` : ""}
      <button type="submit" ?disabled=${this.busy}>${t("login.submit")}</button>
      ${passkeysSupported() || this.instance?.oidc || this.instance?.homeassistant ? html`<div class="sep">${t("login.or")}</div>` : ""}
      ${this.instance?.homeassistant ? html`<a class="btn ghost" rel="external" style="justify-content:center" href=${`/api/v1/auth/ha/start?return=${encodeURIComponent(nextPath())}`}>${icon("House", 15)} ${t("login.homeassistant")}</a>` : ""}
      ${this.instance?.oidc ? html`<a class="btn ghost" rel="external" style="justify-content:center" href=${`/api/v1/auth/oidc/start?return=${encodeURIComponent(nextPath())}`}>${icon("LogIn", 15)} ${t("login.oidc", { name: this.instance.oidc.name })}</a>` : ""}
      ${passkeysSupported() ? html`<button type="button" class="ghost" style="justify-content:center" ?disabled=${this.busy} @click=${this.passkeyLogin}>${icon("KeyRound", 15)} ${t("login.passkey")}</button>` : ""}
      <div class="links"><a href="/forgot">${t("login.forgot")}</a></div>
    </form>`;
  }
}
customElements.define("phx-login", PhxLogin);
