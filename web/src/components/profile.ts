import { SignalWatcher } from "@lit-labs/signals";
import { LitElement, css, html } from "lit";
import { ApiError, api, minPasswordLength, type User } from "../api";
import { locale, t } from "../i18n";
import { currentUser, signedIn } from "../state";
import { settingsNav, settingsNavStyles } from "./settings-nav";
import { base } from "./shared";
import { toast } from "./toasts";
import "./picture-picker";
import { userAvatar } from "../pictures";

/** Own name, language and password. */
export class PhxProfile extends SignalWatcher(LitElement) {
  static properties = { error: { state: true }, pwError: { state: true }, busy: { state: true } };
  static styles = [base, settingsNavStyles, css`
    :host { display: block; max-width: 760px; margin: 0 auto; padding: 28px 16px; }
    h1 { margin-bottom: 16px; }
    form { display: grid; gap: 12px; max-width: 460px; }
    h2 { font-size: 17px; margin: 30px 0 12px; }
  `];
  error = "";
  pwError = "";
  busy = false;

  private async save(e: SubmitEvent) {
    e.preventDefault();
    const f = new FormData(e.target as HTMLFormElement);
    const before = locale();
    this.busy = true;
    this.error = "";
    try {
      const u = await api<User>("PATCH", "/me", { name: f.get("name"), locale: f.get("locale") });
      signedIn(u);
      toast(t("profile.saved"));
      // Texts are read at render time; a reload switches the whole app cleanly.
      if (locale() !== before) location.reload();
    } catch (err) {
      this.error = err instanceof ApiError ? err.text : t("error.unknown");
    } finally {
      this.busy = false;
    }
  }

  private async changePassword(e: SubmitEvent) {
    e.preventDefault();
    const form = e.target as HTMLFormElement;
    const f = new FormData(form);
    if (f.get("new") !== f.get("repeat")) {
      this.pwError = t("setpw.mismatch");
      return;
    }
    this.pwError = "";
    try {
      await api("POST", "/me/password", { current: f.get("current") ?? "", new: f.get("new") });
      form.reset();
      toast(t("profile.password_changed"));
    } catch (err) {
      this.pwError = err instanceof ApiError ? err.text : t("error.unknown");
    }
  }

  render() {
    const u = currentUser.get();
    if (!u) return "";
    return html`<div class="enter">
      <h1>${t("profile.title")}</h1>
      ${settingsNav("profile")}
      <h2 style="margin-top:0">${t("picture.profile")}</h2>
      <phx-picture-picker .has=${!!u.avatar} @changed=${(e: CustomEvent<string>) => signedIn({ ...u, avatar: e.detail })}>${userAvatar(u, 64)}</phx-picture-picker>
      <h2>${t("profile.title")}</h2>
      <form @submit=${this.save}>
        <label class="field">${t("profile.email")}<input .value=${u.email} disabled /></label>
        <label class="field">${t("profile.name")}<input name="name" .value=${u.name} required autocomplete="name" /></label>
        <label class="field">${t("profile.language")}
          <select name="locale">
            <option value="" ?selected=${!u.locale}>${t("profile.language_auto")}</option>
            <option value="de" ?selected=${u.locale === "de"} lang="de">Deutsch</option>
            <option value="en" ?selected=${u.locale === "en"} lang="en">English</option>
          </select>
        </label>
        ${this.error ? html`<p class="error" role="alert">${this.error}</p>` : ""}
        <div><button type="submit" ?disabled=${this.busy}>${t("profile.save")}</button></div>
      </form>

      <h2>${t("profile.password_title")}</h2>
      <form @submit=${this.changePassword}>
        <input type="email" name="username" autocomplete="username" .value=${u.email} hidden />
        ${u.hasPassword ? html`<label class="field">${t("profile.password_current")}<input name="current" type="password" autocomplete="current-password" required /></label>` : ""}
        <label class="field">${t("setpw.password")}<input name="new" type="password" autocomplete="new-password" minlength=${minPasswordLength} required /></label>
        <label class="field">${t("setpw.repeat")}<input name="repeat" type="password" autocomplete="new-password" required /></label>
        <span class="muted" style="font-size:12.5px">${t("setpw.hint", { min: minPasswordLength })}</span>
        ${this.pwError ? html`<p class="error" role="alert">${this.pwError}</p>` : ""}
        <div><button type="submit" class="ghost">${t("profile.password_submit")}</button></div>
      </form>
    </div>`;
  }
}
customElements.define("phx-profile", PhxProfile);
