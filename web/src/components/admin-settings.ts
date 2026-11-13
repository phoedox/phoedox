import { SignalWatcher } from "@lit-labs/signals";
import { LitElement, css, html } from "lit";
import { ApiError, api } from "../api";
import { t } from "../i18n";
import { settingsNav, settingsNavStyles } from "./settings-nav";
import { base } from "./shared";
import { toast } from "./toasts";

interface Settings { aiEnabled: boolean }

/** Instance settings for system admins. */
export class PhxAdminSettings extends SignalWatcher(LitElement) {
  static properties = { s: { state: true } };
  static styles = [base, settingsNavStyles, css`
    :host { display: block; max-width: 760px; margin: 0 auto; padding: 28px 16px; }
    h1 { margin-bottom: 16px; }
    label.switch { display: flex; gap: 10px; align-items: center; font-weight: 600; font-size: 15px; }
    label.switch input { width: auto; }
    p { max-width: 620px; line-height: 1.5; }
  `];
  s?: Settings;

  async connectedCallback() {
    super.connectedCallback();
    this.s = await api<Settings>("GET", "/admin/settings").catch(() => undefined);
  }

  private async set(aiEnabled: boolean) {
    try {
      this.s = await api<Settings>("PUT", "/admin/settings", { aiEnabled });
      toast(t("admin.saved"));
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    }
  }

  render() {
    return html`<div class="enter">
      <h1>${t("admin.title")}</h1>
      ${settingsNav("instance")}
      ${this.s ? html`<label class="switch"><input type="checkbox" .checked=${this.s.aiEnabled} @change=${(e: Event) => this.set((e.target as HTMLInputElement).checked)} /> ${t("admin.ai")}</label>
        <p class="muted">${t("admin.ai_hint")}</p>` : html`<p class="muted">${t("app.loading")}</p>`}
    </div>`;
  }
}
customElements.define("phx-admin-settings", PhxAdminSettings);
