import { LitElement, css, html } from "lit";
import { ApiError, api, type User } from "../api";
import { t } from "../i18n";
import { navigate, signedIn } from "../state";
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

export class PhxLogin extends LitElement {
  static properties = { error: { state: true }, busy: { state: true } };
  static styles = [base, authPage];
  error = "";
  busy = false;

  private async submit(e: SubmitEvent) {
    e.preventDefault();
    const f = new FormData(e.target as HTMLFormElement);
    this.busy = true;
    this.error = "";
    try {
      signedIn(await api<User>("POST", "/auth/login", { email: f.get("email"), password: f.get("password") }));
      navigate("/");
    } catch (err) {
      this.error = err instanceof ApiError ? err.text : t("error.unknown");
    } finally {
      this.busy = false;
    }
  }

  render() {
    return html`<form class="card" @submit=${this.submit}>
      <div class="logo"><img src="/logo.svg" alt="" />Phoedox</div>
      <h1 style="font-size:20px">${t("login.title")}</h1>
      <label class="field">${t("login.email")}<input name="email" type="email" autocomplete="username" required /></label>
      <label class="field">${t("login.password")}<input name="password" type="password" autocomplete="current-password" required /></label>
      ${this.error ? html`<p class="error" role="alert">${this.error}</p>` : ""}
      <button type="submit" ?disabled=${this.busy}>${t("login.submit")}</button>
      <div class="links"><a href="/forgot">${t("login.forgot")}</a></div>
    </form>`;
  }
}
customElements.define("phx-login", PhxLogin);
