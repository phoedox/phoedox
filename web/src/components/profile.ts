import { SignalWatcher } from "@lit-labs/signals";
import { LitElement, css, html } from "lit";
import { ApiError, api, type User } from "../api";
import { locale, t } from "../i18n";
import { currentUser, setTheme, setWideReading, signedIn, theme, wideReading, type Theme } from "../state";
import { pageHead, scard, settingsUI, srow, sseg } from "./settings-ui";
import { base } from "./shared";
import { toast } from "./toasts";
import "./picture-picker";
import { userAvatar } from "../pictures";

/** Own picture, name, language and how the app looks (ADR-0070). */
export class PhxProfile extends SignalWatcher(LitElement) {
  static properties = { error: { state: true }, busy: { state: true } };
  static styles = [base, settingsUI, css`
    form .foot { display: flex; justify-content: flex-end; gap: 8px; padding: 12px 18px; border-top: 1px solid var(--line); }
  `];
  error = "";
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

  render() {
    const u = currentUser.get();
    if (!u) return "";
    return html`<div class="enter">
      ${pageHead(t("nav.profile"), t("sx.me.profile_text"))}
      <form @submit=${this.save}>
        ${scard(t("profile.title"), t("sx.me.profile_card"), html`
          ${srow(t("picture.profile"), t("sx.me.picture_help"), html`<phx-picture-picker .has=${!!u.avatar} @changed=${(e: CustomEvent<string>) => signedIn({ ...u, avatar: e.detail })}>${userAvatar(u, 48)}</phx-picture-picker>`)}
          ${srow(t("profile.name"), t("sx.me.name_help"), html`<input name="name" .value=${u.name} required autocomplete="name" aria-label=${t("profile.name")} />`)}
          ${srow(t("profile.email"), t("sx.me.email_help"), html`<input .value=${u.email} disabled aria-label=${t("profile.email")} />`)}
          ${srow(t("profile.language"), t("sx.me.language_help"), html`<select name="locale" aria-label=${t("profile.language")}>
            <option value="" ?selected=${!u.locale}>${t("profile.language_auto")}</option>
            <option value="de" ?selected=${u.locale === "de"} lang="de">Deutsch</option>
            <option value="en" ?selected=${u.locale === "en"} lang="en">English</option>
          </select>`)}
          ${this.error ? html`<p class="error" role="alert" style="padding:0 18px">${this.error}</p>` : ""}
          <div class="foot"><button type="submit" ?disabled=${this.busy}>${t("profile.save")}</button></div>`)}
      </form>
      ${scard(t("sx.me.appearance"), t("sx.me.appearance_text"), html`
        ${srow(t("sx.me.theme"), t("sx.me.theme_help"), sseg<Theme>(t("sx.me.theme"), theme.get(), [["system", t("sx.me.theme_system")], ["light", t("sx.me.theme_light")], ["dark", t("sx.me.theme_dark")]], setTheme))}
        ${srow(t("sx.me.width"), t("sx.me.width_help"), sseg(t("sx.me.width"), wideReading.get() ? "wide" : "column", [["column", t("sx.me.width_column")], ["wide", t("sx.me.width_wide")]], (v) => setWideReading(v === "wide")))}`)}
    </div>`;
  }
}
customElements.define("phx-profile", PhxProfile);
